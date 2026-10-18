//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
)

func TestRecoveryCLIRealDatabase(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := fmt.Sprintf("nexus_recovery_cli_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// ConnString includes only the isolated test credentials, never real secrets.
	parsedURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsedURL.Path = "/" + name
	cliDSN := parsedURL.String()
	t.Setenv("DATABASE_URL", cliDSN)
	t.Setenv("RADISHNEXUS_DATABASE_PASSWORD_FILE", "")
	var out bytes.Buffer
	if err = run([]string{"inspect", "--request-stdin"}, strings.NewReader(`{"email":"private@example.test"}`), &out); err == nil || out.Len() != 0 {
		t.Fatal("missing schema accepted")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Migrate(ctx, conn.Conn())
	conn.Release()
	if err != nil {
		t.Fatal(err)
	}
	h := authn.NewArgon2idHasher()
	auth := authn.NewService(authpostgres.New(pool), h, authn.CryptoSecretGenerator{}, authn.SystemClock{})
	oldPassword := "old private CLI password marker"
	newPassword := "new private CLI password marker"
	email := "private@example.test"
	owner, err := auth.Bootstrap(ctx, authn.BootstrapInput{Email: email, Password: oldPassword, DisplayName: "CLI Owner", WorkspaceName: "CLI Workspace"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login(ctx, authn.LoginInput{Email: email, Password: oldPassword})
	if err != nil {
		t.Fatal(err)
	}
	call := func(command string, request any) (string, error) {
		t.Helper()
		body, e := json.Marshal(request)
		if e != nil {
			t.Fatal(e)
		}
		var output bytes.Buffer
		err := run([]string{command, "--request-stdin"}, bytes.NewReader(body), &output)
		text := output.String() + safeError(err)
		for _, private := range []string{email, oldPassword, newPassword, "postgres://", "SQLSTATE", "password_hash"} {
			if strings.Contains(text, private) {
				t.Fatal("CLI disclosed sensitive value")
			}
		}
		return output.String(), err
	}
	inspection, err := call("inspect", map[string]any{"email": email})
	if err != nil {
		t.Fatal(err)
	}
	var inspected authn.RecoveryInspection
	if json.Unmarshal([]byte(inspection), &inspected) != nil || !inspected.Eligible {
		t.Fatal("inspection")
	}
	input := authn.RecoveryInput{UserID: owner.UserID, ExpectedEmail: email, ExpectedPasswordChangedAt: inspected.PasswordChangedAt, OperationID: "abcdefab-1234-4234-8234-123456789abc", OperatorRef: "ops-cli", ReasonCode: "lost_local_password", NewPassword: newPassword, Confirmed: true}
	applied, err := call("apply", input)
	if err != nil {
		t.Fatal(err)
	}
	var result authn.RecoveryResult
	if json.Unmarshal([]byte(applied), &result) != nil || result.Result != "applied" {
		t.Fatal("application result")
	}
	status, err := call("status", map[string]string{"user_id": owner.UserID, "operation_id": input.OperationID})
	if err != nil || status != applied {
		t.Fatal("status", err)
	}
	if _, err = call("apply", input); !errors.Is(err, authn.ErrRecoveryRecorded) {
		t.Fatal("reused operation", err)
	}
	if _, err = auth.ResolveSession(ctx, session.Token); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatal("session survived")
	}
	if _, err = auth.Login(ctx, authn.LoginInput{Email: email, Password: oldPassword}); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatal("old password worked")
	}
	if _, err = auth.Login(ctx, authn.LoginInput{Email: email, Password: newPassword}); err != nil {
		t.Fatal(err)
	}
	// Drift / old history rejected before any identity query or mutation.
	var history string
	if err = pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY sequence)::text FROM public.radishnexus_schema_migrations m`).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE public.radishnexus_schema_migrations SET checksum=repeat('0',64) WHERE sequence=17`); err != nil {
		t.Fatal(err)
	}
	if _, err = call("status", map[string]string{"user_id": owner.UserID, "operation_id": input.OperationID}); err == nil {
		t.Fatal("drift accepted")
	}
	// Restore only this test database's history for subsequent permissions probes.
	var records []struct {
		Sequence int    `json:"sequence"`
		Checksum string `json:"checksum"`
	}
	if json.Unmarshal([]byte(history), &records) != nil {
		t.Fatal("history")
	}
	if _, err = pool.Exec(ctx, `UPDATE public.radishnexus_schema_migrations SET checksum=$1 WHERE sequence=17`, records[16].Checksum); err != nil {
		t.Fatal(err)
	}
	after, err := call("inspect", map[string]string{"email": email})
	if err != nil {
		t.Fatal(err)
	}
	var current authn.RecoveryInspection
	_ = json.Unmarshal([]byte(after), &current)
	input.ExpectedPasswordChangedAt = current.PasswordChangedAt
	input.OperationID = "abcdefab-1234-4234-8234-123456789abd"
	input.NewPassword = oldPassword
	t.Setenv("DATABASE_URL", cliDSN+"&default_transaction_read_only=on")
	if _, err = call("apply", input); err == nil {
		t.Fatal("read-only DB applied")
	}
	t.Setenv("DATABASE_URL", cliDSN)
	finalInspection, err := call("inspect", map[string]string{"email": email})
	if err != nil || !reflect.DeepEqual(finalInspection, after) {
		t.Fatal("read-only changed credential")
	}
	t.Setenv("RADISHNEXUS_DATABASE_PASSWORD_FILE", "/nonexistent/private-secret-marker")
	_, err = call("inspect", map[string]string{"email": email})
	if err == nil || strings.Contains(safeError(err), "private-secret-marker") {
		t.Fatal("secret-file error leak")
	}
}
