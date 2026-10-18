// nexus-account-recover is an explicit, operator-authorized local recovery tool.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/runtimeconfig"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, safeError(err))
		os.Exit(1)
	}
}
func safeError(err error) string {
	for _, known := range []error{authn.ErrRecoveryInput, authn.ErrRecoveryIneligible, authn.ErrRecoveryConflict, authn.ErrRecoveryRecorded, authn.ErrRecoverySamePassword, authn.ErrRecoveryClock, authn.ErrRecoveryUnconfirmed} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "operation_timed_out_or_canceled"
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "55P03":
			return "lock_timeout"
		case "40P01":
			return "transaction_deadlock; inspect again before retry"
		case "42501", "25006":
			return "database_write_not_authorized"
		case "57014":
			return "operation_timed_out_or_canceled"
		}
	}
	// Never print a driver/configuration/parser/writer error or its wrapped cause.
	return "operation_failed; check deployment configuration and schema readiness"
}

type recoveryRequest struct {
	email       string
	userID      string
	operationID string
	apply       authn.RecoveryInput
}

func readRequest(command string, input io.Reader) (recoveryRequest, error) {
	var request recoveryRequest
	body, err := io.ReadAll(io.LimitReader(input, 8193))
	if err != nil || len(body) > 8192 || !utf8.Valid(body) {
		return request, authn.ErrRecoveryInput
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return request, authn.ErrRecoveryInput
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return request, authn.ErrRecoveryInput
		}
		key, ok := token.(string)
		if !ok {
			return request, authn.ErrRecoveryInput
		}
		if _, seen := fields[key]; seen {
			return request, authn.ErrRecoveryInput
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return request, authn.ErrRecoveryInput
		}
		fields[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return request, authn.ErrRecoveryInput
	}
	if decoder.Decode(new(any)) != io.EOF {
		return request, authn.ErrRecoveryInput
	}
	expected := map[string]*string{}
	switch command {
	case "inspect":
		expected["email"] = &request.email
	case "status":
		expected["user_id"] = &request.userID
		expected["operation_id"] = &request.operationID
	case "apply":
		expected = map[string]*string{"user_id": &request.apply.UserID, "expected_email": &request.apply.ExpectedEmail, "expected_password_changed_at": &request.apply.ExpectedPasswordChangedAt, "operation_id": &request.apply.OperationID, "operator_ref": &request.apply.OperatorRef, "reason_code": &request.apply.ReasonCode, "new_password": &request.apply.NewPassword}
		confirmed, ok := fields["confirmed"]
		if !ok || json.Unmarshal(confirmed, &request.apply.Confirmed) != nil {
			return request, authn.ErrRecoveryInput
		}
		delete(fields, "confirmed")
	default:
		return request, authn.ErrRecoveryInput
	}
	if len(fields) != len(expected) {
		return request, authn.ErrRecoveryInput
	}
	for key, target := range expected {
		value, ok := fields[key]
		if !ok || !validJSONStringUnicode(value) || json.Unmarshal(value, target) != nil {
			return request, authn.ErrRecoveryInput
		}
	}
	switch command {
	case "inspect":
		_, err = authn.NormalizeEmail(request.email)
	case "status":
		err = authn.ValidateRecoveryIdentity(request.userID, request.operationID)
	case "apply":
		err = authn.ValidateRecoveryInput(request.apply)
	}
	if err != nil {
		return request, authn.ErrRecoveryInput
	}
	return request, nil
}

// encoding/json replaces lone UTF-16 surrogates with U+FFFD. Reject them rather
// than silently changing the password supplied by the operator.
func validJSONStringUnicode(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		value, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value >= 0xd800 && value <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) != 2 || args[1] != "--request-stdin" || (args[0] != "inspect" && args[0] != "apply" && args[0] != "status") {
		return authn.ErrRecoveryInput
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Includes a blocked stdin in the command budget. main exits on timeout.
	type readResult struct {
		request recoveryRequest
		err     error
	}
	reads := make(chan readResult, 1)
	go func() { request, err := readRequest(args[0], input); reads <- readResult{request, err} }()
	var request recoveryRequest
	select {
	case r := <-reads:
		if r.err != nil {
			return r.err
		}
		request = r.request
	case <-ctx.Done():
		return ctx.Err()
	}
	databaseURL, err := runtimeconfig.DatabaseURL(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return err
	}
	config.ConnConfig.RuntimeParams["application_name"] = "radishnexus-account-recover"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	checker, err := db.NewReadinessChecker(pool)
	if err != nil {
		return err
	}
	if err = checker.CheckReady(ctx); err != nil {
		return err
	}
	service := authn.NewRecoveryService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{})
	return execute(ctx, args[0], request, output, service)
}
func execute(ctx context.Context, command string, request recoveryRequest, output io.Writer, service *authn.RecoveryService) error {
	var result any
	var err error
	switch command {
	case "inspect":
		result, err = service.Inspect(ctx, request.email)
	case "apply":
		result, err = service.Apply(ctx, request.apply)
	case "status":
		result, err = service.Status(ctx, request.userID, request.operationID)
	default:
		return authn.ErrRecoveryInput
	}
	if err != nil {
		return err
	}
	if err = json.NewEncoder(output).Encode(result); err != nil {
		if command == "apply" {
			return authn.ErrRecoveryUnconfirmed
		}
		return errors.New("output_failed")
	}
	return nil
}
