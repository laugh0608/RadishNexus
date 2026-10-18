package authn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type recoveryMemory struct {
	candidate RecoveryCandidate
	used      bool
	writes    int
	record    RecoveryRecord
}

func (s *recoveryMemory) InspectRecovery(context.Context, string) (RecoveryCandidate, error) {
	return s.candidate, nil
}
func (s *recoveryMemory) FindRecoveryCandidate(context.Context, string) (RecoveryCandidate, error) {
	return s.candidate, nil
}
func (s *recoveryMemory) RecoveryOperationExists(context.Context, string) (bool, error) {
	return s.used, nil
}
func (s *recoveryMemory) RecoveryStatus(context.Context, string, string) (RecoveryResult, error) {
	return RecoveryResult{Result: "not_observed"}, nil
}
func (s *recoveryMemory) ApplyRecovery(_ context.Context, r RecoveryRecord) (RecoveryResult, error) {
	s.writes++
	s.record = r
	return RecoveryResult{Result: "applied"}, nil
}

type recoveryFailHasher struct {
	PasswordHasher
	verifyErr, hashErr error
}

func (h recoveryFailHasher) Verify(a, b string) (bool, error) {
	if h.verifyErr != nil {
		return false, h.verifyErr
	}
	return h.PasswordHasher.Verify(a, b)
}
func (h recoveryFailHasher) Hash(p string) (string, error) {
	if h.hashErr != nil {
		return "", h.hashErr
	}
	return h.PasswordHasher.Hash(p)
}
func TestRecoveryValidationAndPreparation(t *testing.T) {
	ctx := context.Background()
	hasher := NewArgon2idHasher()
	old := "original password for recovery"
	hash, err := hasher.Hash(old)
	if err != nil {
		t.Fatal(err)
	}
	c := RecoveryCandidate{UserID: "usr_recovery", Email: "recover@example.test", AccountStatus: "active", CredentialStatus: "active", PasswordHash: hash, PasswordChangedAt: time.Now().UTC().Truncate(time.Microsecond)}
	input := RecoveryInput{UserID: c.UserID, ExpectedEmail: c.Email, ExpectedPasswordChangedAt: c.PasswordChangedAt.Format(RecoveryTimeLayout), OperationID: "12345678-1234-4234-8234-123456789abc", OperatorRef: "ops-42", ReasonCode: "lost_local_password", NewPassword: "中文恢复密码保持原文与空白  ", Confirmed: true}
	for _, tt := range []struct {
		name   string
		mutate func(*RecoveryInput)
		want   error
	}{
		{"success", func(*RecoveryInput) {}, nil},
		{"same password", func(i *RecoveryInput) { i.NewPassword = old }, ErrRecoverySamePassword},
		{"false", func(i *RecoveryInput) { i.Confirmed = false }, ErrRecoveryInput},
		{"email mismatch", func(i *RecoveryInput) { i.ExpectedEmail = "other@example.test" }, ErrRecoveryConflict},
		{"version mismatch", func(i *RecoveryInput) {
			i.ExpectedPasswordChangedAt = c.PasswordChangedAt.Add(-time.Second).Format(RecoveryTimeLayout)
		}, ErrRecoveryConflict},
		{"bad time", func(i *RecoveryInput) { i.ExpectedPasswordChangedAt = "2026-10-18T00:00:00Z" }, ErrRecoveryInput},
		{"bad user", func(i *RecoveryInput) { i.UserID = "usr_x?secret" }, ErrRecoveryInput},
		{"bad uuid", func(i *RecoveryInput) { i.OperationID = strings.ToUpper(i.OperationID) }, ErrRecoveryInput},
		{"bad operator", func(i *RecoveryInput) { i.OperatorRef = "name@example.test" }, ErrRecoveryInput},
		{"bad reason", func(i *RecoveryInput) { i.ReasonCode = "other" }, ErrRecoveryInput},
		{"14", func(i *RecoveryInput) { i.NewPassword = strings.Repeat("a", 14) }, ErrRecoveryInput},
		{"15", func(i *RecoveryInput) { i.NewPassword = strings.Repeat("a", 15) }, nil},
		{"128", func(i *RecoveryInput) { i.NewPassword = strings.Repeat("文", 128) }, nil},
		{"129", func(i *RecoveryInput) { i.NewPassword = strings.Repeat("a", 129) }, ErrRecoveryInput},
		{"invalid utf8", func(i *RecoveryInput) { i.NewPassword = strings.Repeat("a", 15) + "\xff" }, ErrRecoveryInput},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &recoveryMemory{candidate: c}
			service := NewRecoveryService(s, hasher, CryptoSecretGenerator{})
			i := input
			tt.mutate(&i)
			_, err := service.Apply(ctx, i)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want %v", err, tt.want)
			}
			if err != nil && s.writes != 0 {
				t.Fatal("rejected write")
			}
			if err == nil {
				ok, e := hasher.Verify(s.record.PasswordHash, i.NewPassword)
				if e != nil || !ok || s.record.PasswordHash == hash {
					t.Fatal("password altered or salt reused")
				}
			}
		})
	}
	for _, mode := range []string{"disabled account", "disabled credential", "unmapped", "used", "corrupt", "random"} {
		t.Run(mode, func(t *testing.T) {
			s := &recoveryMemory{candidate: c}
			var h PasswordHasher = hasher
			switch mode {
			case "disabled account":
				s.candidate.AccountStatus = "disabled"
			case "disabled credential":
				s.candidate.CredentialStatus = "disabled"
			case "unmapped":
				s.candidate.Email = ""
			case "used":
				s.used = true
			case "corrupt":
				s.candidate.PasswordHash = "damaged"
			case "random":
				h = recoveryFailHasher{PasswordHasher: hasher, hashErr: errors.New("random unavailable")}
			}
			_, err := NewRecoveryService(s, h, CryptoSecretGenerator{}).Apply(ctx, input)
			if err == nil || s.writes != 0 {
				t.Fatal("unsafe preparation", err)
			}
		})
	}
}
