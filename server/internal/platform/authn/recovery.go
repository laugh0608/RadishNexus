package authn

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrRecoveryInput        = errors.New("invalid_request")
	ErrRecoveryIneligible   = errors.New("account_ineligible")
	ErrRecoveryConflict     = errors.New("credential_conflict")
	ErrRecoveryRecorded     = errors.New("operation_already_recorded")
	ErrRecoverySamePassword = errors.New("password_unchanged")
	ErrRecoveryClock        = errors.New("clock_not_advanced")
	ErrRecoveryUnconfirmed  = errors.New("result_unconfirmed; use status with the original user_id and operation_id")
)

// RecoveryTimeLayout is the exact UTC microsecond representation used by inspect.
const RecoveryTimeLayout = "2006-01-02T15:04:05.000000Z"

var recoveryOperationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var recoveryOperatorPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

type RecoveryCandidate struct {
	UserID            string
	Email             string
	AccountStatus     string
	CredentialStatus  string
	PasswordHash      string
	PasswordChangedAt time.Time
}

func (c RecoveryCandidate) Eligible() bool {
	return c.AccountStatus == "active" && c.CredentialStatus == "active" && c.Email != ""
}

type RecoveryInspection struct {
	UserID            string `json:"user_id"`
	AccountStatus     string `json:"account_status"`
	CredentialStatus  string `json:"credential_status"`
	PasswordChangedAt string `json:"password_changed_at"`
	Eligible          bool   `json:"eligible"`
}
type RecoveryInput struct {
	UserID                    string `json:"user_id"`
	ExpectedEmail             string `json:"expected_email"`
	ExpectedPasswordChangedAt string `json:"expected_password_changed_at"`
	OperationID               string `json:"operation_id"`
	OperatorRef               string `json:"operator_ref"`
	ReasonCode                string `json:"reason_code"`
	NewPassword               string `json:"new_password"`
	Confirmed                 bool   `json:"confirmed"`
}
type RecoveryRecord struct {
	Candidate    RecoveryCandidate
	OperationID  string
	OperatorRef  string
	ReasonCode   string
	AuditID      string
	PasswordHash string
}
type RecoveryResult struct {
	Result            string `json:"result"`
	UserID            string `json:"user_id"`
	OperationID       string `json:"operation_id"`
	AuditID           string `json:"audit_id,omitempty"`
	PasswordChangedAt string `json:"password_changed_at,omitempty"`
}
type RecoveryStore interface {
	InspectRecovery(context.Context, string) (RecoveryCandidate, error)
	FindRecoveryCandidate(context.Context, string) (RecoveryCandidate, error)
	RecoveryOperationExists(context.Context, string) (bool, error)
	ApplyRecovery(context.Context, RecoveryRecord) (RecoveryResult, error)
	RecoveryStatus(context.Context, string, string) (RecoveryResult, error)
}
type RecoveryService struct {
	store     RecoveryStore
	passwords PasswordHasher
	secrets   SecretGenerator
}

func NewRecoveryService(store RecoveryStore, passwords PasswordHasher, secrets SecretGenerator) *RecoveryService {
	return &RecoveryService{store: store, passwords: passwords, secrets: secrets}
}
func ValidateRecoveryIdentity(userID, operationID string) error {
	if len(userID) <= 4 || len(userID) > 128 || !strings.HasPrefix(userID, "usr_") || !recoveryOperationPattern.MatchString(operationID) {
		return ErrRecoveryInput
	}
	for _, r := range userID {
		if r > 0x7f || r <= 0x20 || strings.ContainsRune("/?#", r) {
			return ErrRecoveryInput
		}
	}
	return nil
}
func ValidateRecoveryInput(input RecoveryInput) error {
	if ValidateRecoveryIdentity(input.UserID, input.OperationID) != nil || !input.Confirmed || !recoveryOperatorPattern.MatchString(input.OperatorRef) || input.ReasonCode != "lost_local_password" {
		return ErrRecoveryInput
	}
	if _, err := NormalizeEmail(input.ExpectedEmail); err != nil {
		return ErrRecoveryInput
	}
	version, err := time.Parse(RecoveryTimeLayout, input.ExpectedPasswordChangedAt)
	if err != nil || version.Format(RecoveryTimeLayout) != input.ExpectedPasswordChangedAt {
		return ErrRecoveryInput
	}
	if validateNewPassword(input.NewPassword) != nil {
		return ErrRecoveryInput
	}
	return nil
}
func (s *RecoveryService) Inspect(ctx context.Context, email string) (RecoveryInspection, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return RecoveryInspection{}, ErrRecoveryInput
	}
	c, err := s.store.InspectRecovery(ctx, email)
	if err != nil {
		return RecoveryInspection{}, err
	}
	return RecoveryInspection{c.UserID, c.AccountStatus, c.CredentialStatus, c.PasswordChangedAt.UTC().Format(RecoveryTimeLayout), c.Eligible()}, nil
}
func (s *RecoveryService) Status(ctx context.Context, userID, operationID string) (RecoveryResult, error) {
	if err := ValidateRecoveryIdentity(userID, operationID); err != nil {
		return RecoveryResult{}, err
	}
	return s.store.RecoveryStatus(ctx, userID, operationID)
}
func (s *RecoveryService) Apply(ctx context.Context, input RecoveryInput) (RecoveryResult, error) {
	if err := ValidateRecoveryInput(input); err != nil {
		return RecoveryResult{}, err
	}
	used, err := s.store.RecoveryOperationExists(ctx, input.OperationID)
	if err != nil {
		return RecoveryResult{}, err
	}
	if used {
		return RecoveryResult{}, ErrRecoveryRecorded
	}
	c, err := s.store.FindRecoveryCandidate(ctx, input.UserID)
	if err != nil {
		return RecoveryResult{}, err
	}
	if !c.Eligible() {
		return RecoveryResult{}, ErrRecoveryIneligible
	}
	email, _ := NormalizeEmail(input.ExpectedEmail)
	if c.Email != email || c.PasswordChangedAt.UTC().Format(RecoveryTimeLayout) != input.ExpectedPasswordChangedAt {
		return RecoveryResult{}, ErrRecoveryConflict
	}
	same, err := s.passwords.Verify(c.PasswordHash, input.NewPassword)
	if err != nil {
		return RecoveryResult{}, err
	}
	if same {
		return RecoveryResult{}, ErrRecoverySamePassword
	}
	hash, err := s.passwords.Hash(input.NewPassword)
	if err != nil {
		return RecoveryResult{}, err
	}
	auditID, err := s.secrets.NewID("ida_")
	if err != nil {
		return RecoveryResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RecoveryResult{}, err
	}
	return s.store.ApplyRecovery(ctx, RecoveryRecord{Candidate: c, OperationID: input.OperationID, OperatorRef: input.OperatorRef, ReasonCode: input.ReasonCode, AuditID: auditID, PasswordHash: hash})
}
