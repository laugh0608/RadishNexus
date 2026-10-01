package authn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const OIDCTransactionLifetime = 5 * time.Minute

type ExternalIdentity struct {
	Issuer  string
	Subject string
}

type OIDCProvider interface {
	Issuer() string
	Fingerprint() []byte
	AuthorizationURL(context.Context, string, string, string) (string, error)
	Exchange(context.Context, string, string, []byte) (ExternalIdentity, error)
}

type OIDCTransaction struct {
	StateDigest             []byte
	BrowserDigest           []byte
	NonceDigest             []byte
	ProviderDigest          []byte
	InitiatingSessionDigest []byte
	InvitationDigest        []byte
	DisplayName             string
	CreatedAt               time.Time
	ExpiresAt               time.Time
}

type OIDCCompletion struct {
	Transaction OIDCTransaction
	Identity    ExternalIdentity
	NewUserID   string
	Session     SessionRecord
	AuditID     string
}

type OIDCStore interface {
	BeginOIDC(context.Context, OIDCTransaction) error
	ConsumeOIDC(context.Context, []byte, []byte, time.Time) (OIDCTransaction, error)
	CompleteOIDC(context.Context, OIDCCompletion) error
}

type OIDCStartInput struct {
	Mode            string
	SessionToken    string
	InvitationToken string
	DisplayName     string
}

type OIDCStart struct {
	AuthorizationURL string
	BrowserToken     string
	ExpiresAt        time.Time
}

type OIDCService struct {
	store    OIDCStore
	identity *IdentityService
	provider OIDCProvider
}

func NewOIDCService(store OIDCStore, identity *IdentityService, provider OIDCProvider) *OIDCService {
	return &OIDCService{store: store, identity: identity, provider: provider}
}

func (service *OIDCService) Start(ctx context.Context, input OIDCStartInput) (OIDCStart, error) {
	if service.provider == nil {
		return OIDCStart{}, ErrOIDCUnavailable
	}
	record := OIDCTransaction{ProviderDigest: service.provider.Fingerprint()}
	switch input.Mode {
	case "login":
		if input.SessionToken != "" {
			return OIDCStart{}, authz.ErrInvalid
		}
		if input.InvitationToken != "" {
			if !validTokenEncoding(input.InvitationToken) {
				return OIDCStart{}, ErrInvitationInvalid
			}
			name, err := normalizeName("display name", input.DisplayName)
			if err != nil {
				return OIDCStart{}, err
			}
			record.InvitationDigest, record.DisplayName = digestToken(input.InvitationToken), name
		} else if input.DisplayName != "" {
			return OIDCStart{}, authz.ErrInvalid
		}
	case "link":
		if input.InvitationToken != "" || input.DisplayName != "" {
			return OIDCStart{}, authz.ErrInvalid
		}
		account, err := service.identity.Account(ctx, input.SessionToken)
		if err != nil {
			return OIDCStart{}, err
		}
		if !account.RecentlyAuthenticated {
			return OIDCStart{}, ErrRecentAuthentication
		}
		if account.RadishLinked {
			return OIDCStart{}, ErrIdentityConflict
		}
		record.InitiatingSessionDigest = digestToken(input.SessionToken)
	default:
		return OIDCStart{}, authz.ErrInvalid
	}
	secrets := service.identity.auth.secrets
	state, err := secrets.NewToken()
	if err != nil {
		return OIDCStart{}, fmt.Errorf("generate OIDC state: %w", err)
	}
	browser, err := secrets.NewToken()
	if err != nil {
		return OIDCStart{}, fmt.Errorf("generate OIDC browser binding: %w", err)
	}
	nonce, err := secrets.NewToken()
	if err != nil {
		return OIDCStart{}, fmt.Errorf("generate OIDC nonce: %w", err)
	}
	authorizationURL, err := service.provider.AuthorizationURL(ctx, state, nonce, oidcVerifier(browser, state))
	if err != nil {
		return OIDCStart{}, err
	}
	record.StateDigest, record.BrowserDigest, record.NonceDigest = digestToken(state), digestToken(browser), digestToken(nonce)
	record.CreatedAt = service.identity.auth.clock.Now().UTC()
	record.ExpiresAt = record.CreatedAt.Add(OIDCTransactionLifetime)
	if err := service.store.BeginOIDC(ctx, record); err != nil {
		return OIDCStart{}, err
	}
	return OIDCStart{AuthorizationURL: authorizationURL, BrowserToken: browser, ExpiresAt: record.ExpiresAt}, nil
}

// Callback consumes the transaction before any token request. Callback denial
// also consumes it, so neither a code nor a failed attempt can be replayed.
func (service *OIDCService) Callback(ctx context.Context, state, browser, code string, denied bool) (Session, bool, error) {
	if service.provider == nil {
		return Session{}, false, ErrOIDCUnavailable
	}
	if !validTokenEncoding(state) || !validTokenEncoding(browser) {
		return Session{}, false, ErrOIDCInvalid
	}
	now := service.identity.auth.clock.Now().UTC()
	transaction, err := service.store.ConsumeOIDC(ctx, digestToken(state), digestToken(browser), now)
	if err != nil {
		return Session{}, false, err
	}
	if denied || code == "" || len(code) > 8192 || strings.IndexFunc(code, unicode.IsControl) >= 0 || subtle.ConstantTimeCompare(transaction.ProviderDigest, service.provider.Fingerprint()) != 1 {
		return Session{}, false, ErrOIDCInvalid
	}
	identity, err := service.provider.Exchange(ctx, code, oidcVerifier(browser, state), transaction.NonceDigest)
	if err != nil {
		return Session{}, false, err
	}
	if identity.Issuer != service.provider.Issuer() || !validSubject(identity.Subject) {
		return Session{}, false, ErrOIDCInvalid
	}
	session, record, err := service.identity.auth.newSession("")
	if err != nil {
		return Session{}, false, err
	}
	completion := OIDCCompletion{Transaction: transaction, Identity: identity, Session: record}
	completion.NewUserID, err = service.identity.auth.secrets.NewID("usr_")
	if err != nil {
		return Session{}, false, err
	}
	completion.AuditID, err = service.identity.auth.secrets.NewID("ida_")
	if err != nil {
		return Session{}, false, err
	}
	if err := service.store.CompleteOIDC(ctx, completion); err != nil {
		return Session{}, false, err
	}
	if len(transaction.InitiatingSessionDigest) != 0 {
		return Session{}, true, nil
	}
	resolved, err := service.identity.auth.store.ResolveSession(ctx, record.TokenDigest, record.CreatedAt)
	if err != nil {
		return Session{}, false, err
	}
	session.Account = resolved.Account
	return session, false, nil
}

func oidcVerifier(browser, state string) string {
	mac := hmac.New(sha256.New, []byte(browser))
	mac.Write([]byte("radishnexus-oidc-pkce-v1:" + state))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validSubject(subject string) bool {
	return utf8.ValidString(subject) && len(subject) > 0 && len(subject) <= 255 && strings.TrimSpace(subject) == subject && strings.IndexFunc(subject, unicode.IsControl) < 0
}

// External provider errors preserve their cause for diagnostic inspection, but
// logging the error itself cannot print codes, tokens, URLs or claims.
type ProviderError struct {
	Stage string
	Cause error
}

func (err ProviderError) Error() string        { return "Radish provider failed during " + err.Stage }
func (err ProviderError) Unwrap() error        { return err.Cause }
func (err ProviderError) Is(target error) bool { return errors.Is(ErrOIDCUnavailable, target) }
