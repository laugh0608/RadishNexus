package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const (
	SessionLifetime   = 24 * time.Hour
	LoginFailureLimit = 5
	LoginLockDuration = 15 * time.Minute
	secretBytes       = 32
)

var (
	ErrAlreadyBootstrapped = errors.New("local identity already bootstrapped")
	ErrAccountNotFound     = errors.New("local account not found")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidSession      = errors.New("invalid session")
	ErrInvalidCSRFToken    = errors.New("invalid csrf token")

	loginNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
)

type PasswordHasher interface {
	Hash(string) (string, error)
	Verify(string, string) (bool, error)
	VerifyDummy(string) error
}

type SecretGenerator interface {
	NewID(string) (string, error)
	NewToken() (string, error)
}

type Clock interface {
	Now() time.Time
}

type Store interface {
	Bootstrap(context.Context, BootstrapRecord) error
	FindLocalAccount(context.Context, string) (LocalAccount, error)
	RecordFailedLogin(context.Context, string, time.Time, time.Time, int) error
	CreateSession(context.Context, SessionRecord) error
	ResolveSession(context.Context, []byte, time.Time) (ResolvedSession, error)
	ResolveWorkspaceSession(context.Context, []byte, string, time.Time) (VerifiedUser, error)
	RevokeSession(context.Context, []byte, time.Time) error
}

type BootstrapInput struct {
	LoginName     string
	DisplayName   string
	WorkspaceName string
	Password      string
}

type BootstrapRecord struct {
	UserID        string
	WorkspaceID   string
	LoginName     string
	DisplayName   string
	WorkspaceName string
	PasswordHash  string
	CreatedAt     time.Time
}

type BootstrapResult struct {
	UserID      string
	WorkspaceID string
	LoginName   string
}

type LocalAccount struct {
	UserID       string
	PasswordHash string
	Status       string
	LockedUntil  *time.Time
}

type LoginInput struct {
	LoginName string
	Password  string
}

type SessionRecord struct {
	ID              string
	UserID          string
	PasswordHash    string
	TokenDigest     []byte
	CSRFTokenDigest []byte
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type Workspace struct {
	ID   string
	Name string
	Role string
}

type User struct {
	ID          string
	DisplayName string
}

type SessionAccount struct {
	User       User
	Workspaces []Workspace
	ExpiresAt  time.Time
}

type ResolvedSession struct {
	Account         SessionAccount
	CSRFTokenDigest []byte
}

type Session struct {
	Token     string
	CSRFToken string
	Account   SessionAccount
}

type Service struct {
	store     Store
	passwords PasswordHasher
	secrets   SecretGenerator
	clock     Clock
}

func NewService(store Store, passwords PasswordHasher, secrets SecretGenerator, clock Clock) *Service {
	return &Service{store: store, passwords: passwords, secrets: secrets, clock: clock}
}

func (service *Service) Bootstrap(
	ctx context.Context,
	input BootstrapInput,
) (BootstrapResult, error) {
	loginName, err := normalizeLoginName(input.LoginName)
	if err != nil {
		return BootstrapResult{}, err
	}
	displayName, err := normalizeName("display name", input.DisplayName)
	if err != nil {
		return BootstrapResult{}, err
	}
	workspaceName, err := normalizeName("workspace name", input.WorkspaceName)
	if err != nil {
		return BootstrapResult{}, err
	}
	if err := validateNewPassword(input.Password); err != nil {
		return BootstrapResult{}, err
	}

	passwordHash, err := service.passwords.Hash(input.Password)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("hash bootstrap password: %w", err)
	}
	userID, err := service.secrets.NewID("usr_")
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("generate bootstrap user ID: %w", err)
	}
	workspaceID, err := service.secrets.NewID("wrk_")
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("generate bootstrap workspace ID: %w", err)
	}
	createdAt := service.clock.Now().UTC()
	record := BootstrapRecord{
		UserID:        userID,
		WorkspaceID:   workspaceID,
		LoginName:     loginName,
		DisplayName:   displayName,
		WorkspaceName: workspaceName,
		PasswordHash:  passwordHash,
		CreatedAt:     createdAt,
	}
	if err := service.store.Bootstrap(ctx, record); err != nil {
		return BootstrapResult{}, err
	}
	return BootstrapResult{UserID: userID, WorkspaceID: workspaceID, LoginName: loginName}, nil
}

func (service *Service) Login(ctx context.Context, input LoginInput) (Session, error) {
	loginName, err := normalizeLoginName(input.LoginName)
	if err != nil || input.Password == "" || len(input.Password) > 1024 || !utf8.ValidString(input.Password) {
		if dummyErr := service.passwords.VerifyDummy("invalid credential input"); dummyErr != nil {
			return Session{}, fmt.Errorf("verify dummy password: %w", dummyErr)
		}
		return Session{}, ErrInvalidCredentials
	}

	account, err := service.store.FindLocalAccount(ctx, loginName)
	if errors.Is(err, ErrAccountNotFound) {
		if dummyErr := service.passwords.VerifyDummy(input.Password); dummyErr != nil {
			return Session{}, fmt.Errorf("verify dummy password: %w", dummyErr)
		}
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}

	passwordMatches, err := service.passwords.Verify(account.PasswordHash, input.Password)
	if err != nil {
		return Session{}, fmt.Errorf("verify local account password: %w", err)
	}
	now := service.clock.Now().UTC()
	locked := account.LockedUntil != nil && account.LockedUntil.After(now)
	if account.Status != "active" || locked || !passwordMatches {
		if account.Status == "active" && !locked && !passwordMatches {
			if err := service.store.RecordFailedLogin(
				ctx,
				account.UserID,
				now,
				now.Add(LoginLockDuration),
				LoginFailureLimit,
			); err != nil {
				return Session{}, err
			}
		}
		return Session{}, ErrInvalidCredentials
	}

	sessionID, err := service.secrets.NewID("ses_")
	if err != nil {
		return Session{}, fmt.Errorf("generate session ID: %w", err)
	}
	token, err := service.secrets.NewToken()
	if err != nil {
		return Session{}, fmt.Errorf("generate session token: %w", err)
	}
	csrfToken, err := service.secrets.NewToken()
	if err != nil {
		return Session{}, fmt.Errorf("generate csrf token: %w", err)
	}
	record := SessionRecord{
		ID:              sessionID,
		UserID:          account.UserID,
		PasswordHash:    account.PasswordHash,
		TokenDigest:     digestToken(token),
		CSRFTokenDigest: digestToken(csrfToken),
		CreatedAt:       now,
		ExpiresAt:       now.Add(SessionLifetime),
	}
	if err := service.store.CreateSession(ctx, record); err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, err
	}
	resolved, err := service.store.ResolveSession(ctx, record.TokenDigest, now)
	if err != nil {
		return Session{}, fmt.Errorf("resolve newly created session: %w", err)
	}
	return Session{Token: token, CSRFToken: csrfToken, Account: resolved.Account}, nil
}

func (service *Service) ResolveSession(
	ctx context.Context,
	token string,
) (SessionAccount, error) {
	if !validTokenEncoding(token) {
		return SessionAccount{}, ErrInvalidSession
	}
	resolved, err := service.store.ResolveSession(ctx, digestToken(token), service.clock.Now().UTC())
	if errors.Is(err, ErrInvalidSession) {
		return SessionAccount{}, ErrInvalidSession
	}
	return resolved.Account, err
}

func (service *Service) ResolveWorkspace(
	ctx context.Context,
	token string,
	workspaceID string,
) (VerifiedUser, error) {
	if !validTokenEncoding(token) || workspaceID == "" {
		return VerifiedUser{}, authz.ErrUnauthenticated
	}
	verified, err := service.store.ResolveWorkspaceSession(
		ctx,
		digestToken(token),
		workspaceID,
		service.clock.Now().UTC(),
	)
	if errors.Is(err, ErrInvalidSession) {
		return VerifiedUser{}, authz.ErrUnauthenticated
	}
	return verified, err
}

func (service *Service) VerifyCSRF(
	ctx context.Context,
	token string,
	csrfToken string,
) error {
	if !validTokenEncoding(token) || !validTokenEncoding(csrfToken) {
		return ErrInvalidCSRFToken
	}
	resolved, err := service.store.ResolveSession(ctx, digestToken(token), service.clock.Now().UTC())
	if errors.Is(err, ErrInvalidSession) {
		return ErrInvalidSession
	}
	if err != nil {
		return err
	}
	actual := digestToken(csrfToken)
	if len(resolved.CSRFTokenDigest) != sha256.Size ||
		subtle.ConstantTimeCompare(actual, resolved.CSRFTokenDigest) != 1 {
		return ErrInvalidCSRFToken
	}
	return nil
}

func (service *Service) RevokeSession(
	ctx context.Context,
	token string,
	csrfToken string,
) error {
	if err := service.VerifyCSRF(ctx, token, csrfToken); err != nil {
		return err
	}
	if err := service.store.RevokeSession(ctx, digestToken(token), service.clock.Now().UTC()); err != nil {
		if errors.Is(err, ErrInvalidSession) {
			return ErrInvalidSession
		}
		return err
	}
	return nil
}

func normalizeLoginName(value string) (string, error) {
	canonical := strings.ToLower(strings.TrimSpace(value))
	if !loginNamePattern.MatchString(canonical) {
		return "", fmt.Errorf("%w: login name must be 3-64 lowercase ASCII letters, digits, dots, underscores, or hyphens", authz.ErrInvalid)
	}
	return canonical, nil
}

func normalizeName(field string, value string) (string, error) {
	canonical := strings.TrimSpace(value)
	length := utf8.RuneCountInString(canonical)
	if !utf8.ValidString(canonical) || length < 1 || length > 100 {
		return "", fmt.Errorf("%w: %s must be 1-100 Unicode characters", authz.ErrInvalid, field)
	}
	return canonical, nil
}

func validateNewPassword(password string) error {
	length := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || length < 15 || length > 128 || len(password) > 1024 {
		return fmt.Errorf("%w: password must be 15-128 Unicode characters and at most 1024 bytes", authz.ErrInvalid)
	}
	return nil
}

func digestToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

func validTokenEncoding(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == secretBytes
}

type CryptoSecretGenerator struct{}

func (CryptoSecretGenerator) NewID(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random), nil
}

func (CryptoSecretGenerator) NewToken() (string, error) {
	random := make([]byte, secretBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
