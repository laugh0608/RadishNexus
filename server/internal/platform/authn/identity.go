package authn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const RecentAuthentication = 10 * time.Minute
const InvitationLifetime = 24 * time.Hour

var (
	ErrInvitationInvalid    = errors.New("invitation unavailable")
	ErrRecentAuthentication = errors.New("recent authentication required")
	ErrLastLoginMethod      = errors.New("cannot remove last login method")
	ErrIdentityConflict     = errors.New("identity operation conflicts with existing account")
	ErrOIDCUnavailable      = errors.New("Radish login unavailable")
	ErrOIDCInvalid          = errors.New("Radish login failed")
)

type AccountDetails struct {
	User                  User
	HasPassword           bool
	RadishLinked          bool
	RecentlyAuthenticated bool
}

type InvitationRecord struct {
	ID          string
	TokenDigest []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
	AuditID     string
}

type Invitation struct {
	ID        string
	Token     string
	ExpiresAt time.Time
}

type AcceptInvitationInput struct {
	InvitationToken string
	SessionToken    string
	Email           string
	Password        string
	DisplayName     string
}

type InvitationAcceptance struct {
	InvitationDigest      []byte
	ExistingSessionDigest []byte
	Email                 string
	DisplayName           string
	UserID                string
	PasswordHash          string
	Session               SessionRecord
	AuditID               string
}

type IdentityStore interface {
	AccountDetails(context.Context, []byte, string, time.Time) (AccountDetails, error)
	CreateInvitation(context.Context, []byte, string, InvitationRecord) error
	AcceptInvitation(context.Context, InvitationAcceptance) (string, error)
	UnlinkExternal(context.Context, []byte, string, string, time.Time) error
}

// IdentityService owns account admission and login-method management. The
// existing authenticator remains the one Session and Workspace resolver.
type IdentityService struct {
	store  IdentityStore
	auth   *Service
	issuer string
}

func NewIdentityService(store IdentityStore, auth *Service, issuer string) *IdentityService {
	return &IdentityService{store: store, auth: auth, issuer: issuer}
}

func (service *IdentityService) Account(ctx context.Context, token string) (AccountDetails, error) {
	if !validTokenEncoding(token) {
		return AccountDetails{}, ErrInvalidSession
	}
	return service.store.AccountDetails(ctx, digestToken(token), service.issuer, service.auth.clock.Now().UTC())
}

func (service *IdentityService) CreateInvitation(ctx context.Context, token, workspaceID string) (Invitation, error) {
	if !validTokenEncoding(token) {
		return Invitation{}, ErrInvalidSession
	}
	id, err := service.auth.secrets.NewID("inv_")
	if err != nil {
		return Invitation{}, fmt.Errorf("generate invitation ID: %w", err)
	}
	secret, err := service.auth.secrets.NewToken()
	if err != nil {
		return Invitation{}, fmt.Errorf("generate invitation secret: %w", err)
	}
	auditID, err := service.auth.secrets.NewID("ida_")
	if err != nil {
		return Invitation{}, err
	}
	now := service.auth.clock.Now().UTC()
	record := InvitationRecord{ID: id, TokenDigest: digestToken(secret), CreatedAt: now, ExpiresAt: now.Add(InvitationLifetime), AuditID: auditID}
	if err := service.store.CreateInvitation(ctx, digestToken(token), workspaceID, record); err != nil {
		return Invitation{}, err
	}
	return Invitation{ID: id, Token: secret, ExpiresAt: record.ExpiresAt}, nil
}

func (service *IdentityService) AcceptInvitation(ctx context.Context, input AcceptInvitationInput) (Session, error) {
	if !validTokenEncoding(input.InvitationToken) {
		return Session{}, ErrInvitationInvalid
	}
	record := InvitationAcceptance{InvitationDigest: digestToken(input.InvitationToken)}
	if input.SessionToken != "" {
		if !validTokenEncoding(input.SessionToken) {
			return Session{}, ErrInvalidSession
		}
		if input.Email != "" || input.Password != "" || input.DisplayName != "" {
			return Session{}, authz.ErrInvalid
		}
		record.ExistingSessionDigest = digestToken(input.SessionToken)
	} else {
		var err error
		record.Email, err = NormalizeEmail(input.Email)
		if err != nil {
			return Session{}, err
		}
		record.DisplayName, err = normalizeName("display name", input.DisplayName)
		if err != nil {
			return Session{}, err
		}
		if err := validateNewPassword(input.Password); err != nil {
			return Session{}, err
		}
		record.PasswordHash, err = service.auth.passwords.Hash(input.Password)
		if err != nil {
			return Session{}, fmt.Errorf("hash invited account password: %w", err)
		}
		record.UserID, err = service.auth.secrets.NewID("usr_")
		if err != nil {
			return Session{}, err
		}
	}
	var session Session
	var sessionRecord SessionRecord
	var err error
	if input.SessionToken == "" {
		session, sessionRecord, err = service.auth.newSession(record.UserID)
		if err != nil {
			return Session{}, err
		}
	} else {
		sessionRecord.CreatedAt = service.auth.clock.Now().UTC()
	}
	record.Session = sessionRecord
	record.AuditID, err = service.auth.secrets.NewID("ida_")
	if err != nil {
		return Session{}, err
	}
	userID, err := service.store.AcceptInvitation(ctx, record)
	if err != nil {
		return Session{}, err
	}
	if input.SessionToken != "" {
		// Admission does not silently renew an existing session.
		account, err := service.auth.ResolveSession(ctx, input.SessionToken)
		return Session{Account: account}, err
	}
	sessionRecord.UserID = userID
	resolved, err := service.auth.store.ResolveSession(ctx, sessionRecord.TokenDigest, sessionRecord.CreatedAt)
	if err != nil {
		return Session{}, err
	}
	session.Account = resolved.Account
	return session, nil
}

func (service *IdentityService) Unlink(ctx context.Context, token string) error {
	if !validTokenEncoding(token) {
		return ErrInvalidSession
	}
	if service.issuer == "" {
		return ErrOIDCUnavailable
	}
	auditID, err := service.auth.secrets.NewID("ida_")
	if err != nil {
		return err
	}
	return service.store.UnlinkExternal(ctx, digestToken(token), service.issuer, auditID, service.auth.clock.Now().UTC())
}

func (service *Service) newSession(userID string) (Session, SessionRecord, error) {
	id, err := service.secrets.NewID("ses_")
	if err != nil {
		return Session{}, SessionRecord{}, fmt.Errorf("generate session ID: %w", err)
	}
	token, err := service.secrets.NewToken()
	if err != nil {
		return Session{}, SessionRecord{}, fmt.Errorf("generate session token: %w", err)
	}
	csrf, err := service.secrets.NewToken()
	if err != nil {
		return Session{}, SessionRecord{}, fmt.Errorf("generate CSRF token: %w", err)
	}
	now := service.clock.Now().UTC()
	return Session{Token: token, CSRFToken: csrf}, SessionRecord{ID: id, UserID: userID, TokenDigest: digestToken(token), CSRFTokenDigest: digestToken(csrf), CreatedAt: now, ExpiresAt: now.Add(SessionLifetime)}, nil
}
