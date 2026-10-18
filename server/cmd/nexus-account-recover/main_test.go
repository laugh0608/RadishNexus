package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
)

func validApply() string {
	return `{"user_id":"usr_test","expected_email":"private@example.test","expected_password_changed_at":"2026-10-18T00:00:00.123456Z","operation_id":"12345678-1234-4234-8234-123456789abc","operator_ref":"ops-42","reason_code":"lost_local_password","new_password":"private new password marker","confirmed":true}`
}
func TestRequestStrictnessAndPrivateErrors(t *testing.T) {
	good := validApply()
	for _, body := range []string{"", `null`, `[]`, `{}`, good + `{}`, strings.Replace(good, `"confirmed":true`, `"confirmed":null`, 1), strings.Replace(good, `"confirmed":true`, `"confirmed":"true"`, 1), strings.Replace(good, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(good, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), strings.Replace(good, `"confirmed":true`, `"unknown":"marker", "confirmed":true`, 1), strings.Replace(good, `"new_password":"private new password marker"`, `"new_password":null`, 1), strings.Replace(good, `"user_id":"usr_test"`, `"user_id":123`, 1), good + strings.Repeat(" ", 8192), "{\xff}", strings.Replace(good, `"new_password":"private new password marker",`, "", 1)} {
		_, err := readRequest("apply", strings.NewReader(body))
		if !errors.Is(err, authn.ErrRecoveryInput) {
			t.Fatal("accepted malformed request")
		}
		if strings.Contains(safeError(err), "marker") {
			t.Fatal("leaked input")
		}
	}
	for command, body := range map[string]string{"apply": good, "inspect": `{"email":"private@example.test"}`, "status": `{"user_id":"usr_test","operation_id":"12345678-1234-4234-8234-123456789abc"}`} {
		if _, err := readRequest(command, strings.NewReader(body)); err != nil {
			t.Fatal(command, err)
		}
	}
	for _, args := range [][]string{{}, {"apply"}, {"apply", "--password", "marker"}, {"apply", "--request-stdin", "marker"}} {
		var out bytes.Buffer
		err := run(args, strings.NewReader(good), &out)
		if !errors.Is(err, authn.ErrRecoveryInput) || out.Len() != 0 {
			t.Fatal("accepted arguments")
		}
	}
	t.Setenv("RADISHNEXUS_RECOVERY_PASSWORD", "marker")
	if _, err := readRequest("apply", strings.NewReader(strings.Replace(good, `"new_password":"private new password marker",`, "", 1))); err == nil {
		t.Fatal("read password environment")
	}
	for _, err := range []error{errors.New("SQL Detail private@example.test marker DSN /secret/file"), io.ErrClosedPipe, errors.Join(authn.ErrRecoveryUnconfirmed, errors.New("private marker"))} {
		message := safeError(err)
		if strings.Contains(message, "private") || strings.Contains(message, "marker") {
			t.Fatal("private output", message)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("private output error") }

type cliStore struct {
	applied   bool
	candidate authn.RecoveryCandidate
}

func (s *cliStore) InspectRecovery(context.Context, string) (authn.RecoveryCandidate, error) {
	return s.candidate, nil
}
func (s *cliStore) FindRecoveryCandidate(context.Context, string) (authn.RecoveryCandidate, error) {
	return s.candidate, nil
}
func (s *cliStore) RecoveryOperationExists(context.Context, string) (bool, error) {
	return s.applied, nil
}
func (s *cliStore) ApplyRecovery(_ context.Context, r authn.RecoveryRecord) (authn.RecoveryResult, error) {
	s.applied = true
	return authn.RecoveryResult{Result: "applied", UserID: r.Candidate.UserID, OperationID: r.OperationID, AuditID: r.AuditID, PasswordChangedAt: time.Now().Format(authn.RecoveryTimeLayout)}, nil
}
func (s *cliStore) RecoveryStatus(context.Context, string, string) (authn.RecoveryResult, error) {
	return authn.RecoveryResult{Result: "applied"}, nil
}
func TestOutputFailureKeepsCommitAndDirectsStatus(t *testing.T) {
	h := authn.NewArgon2idHasher()
	hash, err := h.Hash("original password marker")
	if err != nil {
		t.Fatal(err)
	}
	r, err := readRequest("apply", strings.NewReader(validApply()))
	if err != nil {
		t.Fatal(err)
	}
	version, _ := time.Parse(authn.RecoveryTimeLayout, r.apply.ExpectedPasswordChangedAt)
	s := &cliStore{candidate: authn.RecoveryCandidate{UserID: r.apply.UserID, Email: r.apply.ExpectedEmail, AccountStatus: "active", CredentialStatus: "active", PasswordHash: hash, PasswordChangedAt: version}}
	service := authn.NewRecoveryService(s, h, authn.CryptoSecretGenerator{})
	if err := execute(context.Background(), "apply", r, failingWriter{}, service); !errors.Is(err, authn.ErrRecoveryUnconfirmed) || !s.applied {
		t.Fatal("lost success acknowledgement", err)
	}
	if err := execute(context.Background(), "apply", r, io.Discard, service); !errors.Is(err, authn.ErrRecoveryRecorded) {
		t.Fatal("reapplied", err)
	}
	var out bytes.Buffer
	if err := execute(context.Background(), "inspect", recoveryRequest{email: s.candidate.Email}, &out, service); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal(out.Bytes(), &result) != nil || len(result) != 5 {
		t.Fatal("inspection shape")
	}
	if strings.Contains(out.String(), s.candidate.Email) || strings.Contains(out.String(), hash) {
		t.Fatal("inspection leaked identity material")
	}
}

func TestRequestDoesNotReplaceLoneSurrogates(t *testing.T) {
	for _, suffix := range []string{`\ud800`, `\udfff`, `\ud800x`, `\ud800\u0041`} {
		body := strings.Replace(validApply(), "private new password marker", "private new password marker"+suffix, 1)
		if _, err := readRequest("apply", strings.NewReader(body)); err == nil {
			t.Fatal("silently repaired password")
		}
	}
	for _, suffix := range []string{`\ud83d\ude00`, `\\ud800`, `\ufffd`} {
		body := strings.Replace(validApply(), "private new password marker", "private new password marker"+suffix, 1)
		if _, err := readRequest("apply", strings.NewReader(body)); err != nil {
			t.Fatal("valid unicode rejected", err)
		}
	}
}
