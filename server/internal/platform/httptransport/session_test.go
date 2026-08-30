package httptransport

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
)

func TestBrowserSessionPolicyRequiresExactHTTPSOrigin(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{
		"http://nexus.example.test",
		"https://nexus.example.test/",
		"https://nexus.example.test/path",
		"https://user@nexus.example.test",
	} {
		if _, err := NewBrowserSessionPolicy(invalid); err == nil {
			t.Fatalf("NewBrowserSessionPolicy(%q) error = nil", invalid)
		}
	}
	policy, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://nexus.example.test/api/v1/auth/sessions", nil)
	request.Header.Set("Origin", "https://nexus.example.test")
	if err := policy.ValidateOrigin(request); err != nil {
		t.Fatalf("ValidateOrigin() error = %v", err)
	}
	request.Header.Set("Origin", "https://evil.example.test")
	if err := policy.ValidateOrigin(request); !errors.Is(err, authn.ErrInvalidCSRFToken) {
		t.Fatalf("ValidateOrigin() mismatch error = %v", err)
	}
}

func TestSessionCookiesUseHostSecureStrictContract(t *testing.T) {
	t.Parallel()
	expiresAt := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	cookies, err := SessionCookies(transportToken(1), transportToken(2), expiresAt)
	if err != nil {
		t.Fatalf("SessionCookies() error = %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("SessionCookies() length = %d", len(cookies))
	}
	session, csrf := cookies[0], cookies[1]
	if session.Name != SessionCookieName || session.Path != "/" || !session.Secure ||
		!session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Domain != "" ||
		!session.Expires.Equal(expiresAt) {
		t.Fatalf("session cookie = %#v", session)
	}
	if csrf.Name != CSRFCookieName || csrf.Path != "/" || !csrf.Secure || csrf.HttpOnly ||
		csrf.SameSite != http.SameSiteStrictMode || csrf.Domain != "" || !csrf.Expires.Equal(expiresAt) {
		t.Fatalf("csrf cookie = %#v", csrf)
	}
}

func TestValidateCSRFRequiresOriginCookieAndMatchingHeader(t *testing.T) {
	t.Parallel()
	policy, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	csrfToken := transportToken(2)
	request := httptest.NewRequest(http.MethodDelete, "https://nexus.example.test/api/v1/auth/session", nil)
	request.Header.Set("Origin", "https://nexus.example.test")
	request.Header.Set(CSRFHeaderName, csrfToken)
	request.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfToken})
	if got, err := policy.ValidateCSRF(request); err != nil || got != csrfToken {
		t.Fatalf("ValidateCSRF() = %q, %v", got, err)
	}
	request.Header.Set(CSRFHeaderName, transportToken(3))
	if _, err := policy.ValidateCSRF(request); !errors.Is(err, authn.ErrInvalidCSRFToken) {
		t.Fatalf("ValidateCSRF() mismatch error = %v", err)
	}
}

func TestSessionTokenRejectsMissingOrMalformedCookie(t *testing.T) {
	t.Parallel()
	policy, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://nexus.example.test/api/v1/auth/session", nil)
	if _, err := policy.SessionToken(request); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatalf("SessionToken() missing error = %v", err)
	}
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "not-a-session"})
	if _, err := policy.SessionToken(request); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatalf("SessionToken() malformed error = %v", err)
	}
}

func transportToken(value byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
}
