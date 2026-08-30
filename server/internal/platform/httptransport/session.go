package httptransport

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
)

const (
	SessionCookieName = "__Host-radishnexus-session"
	CSRFCookieName    = "__Host-radishnexus-csrf"
	CSRFHeaderName    = "X-CSRF-Token"
)

type BrowserSessionPolicy struct {
	publicOrigin string
}

func NewBrowserSessionPolicy(publicOrigin string) (BrowserSessionPolicy, error) {
	parsed, err := url.Parse(publicOrigin)
	if err != nil {
		return BrowserSessionPolicy{}, fmt.Errorf("parse public origin: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Opaque != "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" ||
		parsed.String() != publicOrigin {
		return BrowserSessionPolicy{}, fmt.Errorf("public origin must be an exact https origin")
	}
	return BrowserSessionPolicy{publicOrigin: publicOrigin}, nil
}

func (policy BrowserSessionPolicy) ValidateOrigin(request *http.Request) error {
	if request.Header.Get("Origin") != policy.publicOrigin {
		return authn.ErrInvalidCSRFToken
	}
	return nil
}

func (policy BrowserSessionPolicy) SessionToken(request *http.Request) (string, error) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || !validOpaqueToken(cookie.Value) {
		return "", authn.ErrInvalidSession
	}
	return cookie.Value, nil
}

func (policy BrowserSessionPolicy) ValidateCSRF(request *http.Request) (string, error) {
	if err := policy.ValidateOrigin(request); err != nil {
		return "", err
	}
	cookie, err := request.Cookie(CSRFCookieName)
	if err != nil || !validOpaqueToken(cookie.Value) {
		return "", authn.ErrInvalidCSRFToken
	}
	header := request.Header.Get(CSRFHeaderName)
	if !validOpaqueToken(header) || len(header) != len(cookie.Value) ||
		subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
		return "", authn.ErrInvalidCSRFToken
	}
	return header, nil
}

func SessionCookies(token string, csrfToken string, expiresAt time.Time) ([]*http.Cookie, error) {
	if !validOpaqueToken(token) || !validOpaqueToken(csrfToken) || expiresAt.IsZero() {
		return nil, fmt.Errorf("%w: valid session secrets and expiry are required", authn.ErrInvalidSession)
	}
	return []*http.Cookie{
		{
			Name:     SessionCookieName,
			Value:    token,
			Path:     "/",
			Expires:  expiresAt.UTC(),
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		},
		{
			Name:     CSRFCookieName,
			Value:    csrfToken,
			Path:     "/",
			Expires:  expiresAt.UTC(),
			Secure:   true,
			HttpOnly: false,
			SameSite: http.SameSiteStrictMode,
		},
	}, nil
}

func ExpiredSessionCookies() []*http.Cookie {
	expiredAt := time.Unix(1, 0).UTC()
	return []*http.Cookie{
		{
			Name:     SessionCookieName,
			Path:     "/",
			Expires:  expiredAt,
			MaxAge:   -1,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		},
		{
			Name:     CSRFCookieName,
			Path:     "/",
			Expires:  expiredAt,
			MaxAge:   -1,
			Secure:   true,
			HttpOnly: false,
			SameSite: http.SameSiteStrictMode,
		},
	}
}

func validOpaqueToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32
}
