package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
)

type identityTransportStub struct {
	calls         int
	csrfErr       error
	oidcInput     authn.OIDCStartInput
	callbackCalls int
}

func (stub *identityTransportStub) VerifyCSRF(context.Context, string, string) error {
	return stub.csrfErr
}
func (stub *identityTransportStub) Account(context.Context, string) (authn.AccountDetails, error) {
	stub.calls++
	return authn.AccountDetails{User: authn.User{ID: "usr_1", DisplayName: "Member"}, HasPassword: true, RadishLinked: true, RecentlyAuthenticated: true}, nil
}
func (stub *identityTransportStub) CreateInvitation(context.Context, string, string) (authn.Invitation, error) {
	stub.calls++
	return authn.Invitation{ID: "inv_1", Token: transportToken(9), ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (stub *identityTransportStub) AcceptInvitation(context.Context, authn.AcceptInvitationInput) (authn.Session, error) {
	stub.calls++
	return authn.Session{Token: transportToken(1), CSRFToken: transportToken(2), Account: testSessionAccount(time.Now().Add(time.Hour))}, nil
}
func (stub *identityTransportStub) Unlink(context.Context, string) error { stub.calls++; return nil }
func (stub *identityTransportStub) Start(_ context.Context, input authn.OIDCStartInput) (authn.OIDCStart, error) {
	stub.calls++
	stub.oidcInput = input
	return authn.OIDCStart{AuthorizationURL: "https://radish.example.test/connect/authorize", BrowserToken: transportToken(3), ExpiresAt: time.Now().Add(5 * time.Minute)}, nil
}
func (stub *identityTransportStub) Callback(context.Context, string, string, string, bool) (authn.Session, bool, error) {
	stub.callbackCalls++
	return authn.Session{Token: transportToken(1), CSRFToken: transportToken(2), Account: testSessionAccount(time.Now().Add(time.Hour))}, false, nil
}

func identityTransport(t *testing.T, stub *identityTransportStub, oidc bool) http.Handler {
	t.Helper()
	session, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	var provider OIDCService
	if oidc {
		provider = stub
	}
	return WithRequestID(NewIdentityHandler(stub, stub, provider, session, proxy, NewLoginGuard(100, time.Minute, 16, 4)))
}
func identityRequest(method, path, body string) *http.Request {
	request := secureJSONRequest(method, path, body)
	request.Header.Set("Origin", "https://nexus.example.test")
	return request
}
func withIdentityCookies(request *http.Request) {
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: transportToken(1)})
	request.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: transportToken(2)})
	request.Header.Set(CSRFHeaderName, transportToken(2))
}

func TestIdentityMutationsRequireCurrentSessionCSRFAndExactOrigin(t *testing.T) {
	for _, path := range []string{"/api/v1/workspaces/wrk_1/invitations", "/api/v1/auth/external-identity", "/api/v1/auth/oidc/start"} {
		for _, failure := range []string{"missing-session", "wrong-origin", "wrong-csrf", "revoked-session"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				stub := &identityTransportStub{}
				handler := identityTransport(t, stub, true)
				method, body := http.MethodPost, `{}`
				if strings.HasSuffix(path, "external-identity") {
					method = http.MethodDelete
				}
				if strings.HasSuffix(path, "start") {
					body = `{"mode":"link"}`
				}
				request := identityRequest(method, path, body)
				if failure != "missing-session" {
					withIdentityCookies(request)
				}
				switch failure {
				case "wrong-origin":
					request.Header.Set("Origin", "https://evil.example.test")
				case "wrong-csrf":
					request.Header.Set(CSRFHeaderName, transportToken(7))
				case "revoked-session":
					stub.csrfErr = authn.ErrInvalidSession
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code < 400 || stub.calls != 0 {
					t.Fatalf("mutation bypass: status=%d calls=%d", response.Code, stub.calls)
				}
			})
		}
	}
}

func TestIdentityMethodsAndAccountOmitPrivateMetadata(t *testing.T) {
	stub := &identityTransportStub{}
	handler := identityTransport(t, stub, false)
	for _, path := range []string{"/api/v1/auth/methods", "/api/v1/auth/account"} {
		request := identityRequest(http.MethodGet, path, "")
		withIdentityCookies(request)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("read response", response.Code)
		}
		for _, private := range []string{"issuer", "subject", "email", "secret", "credential", "password_hash", transportToken(1)} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatal("private field in account response", private)
			}
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, identityRequest(http.MethodPost, "/api/v1/auth/oidc/start", `{"mode":"login"}`))
	if response.Code != http.StatusServiceUnavailable || stub.calls != 1 {
		t.Fatal("unconfigured OIDC not rejected")
	}
}

func TestOIDCStartSetsSeparateLaxCookieAndDoesNotIssueSession(t *testing.T) {
	stub := &identityTransportStub{}
	handler := identityTransport(t, stub, true)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, identityRequest(http.MethodPost, "/api/v1/auth/oidc/start", `{"mode":"login"}`))
	cookies := response.Result().Cookies()
	if response.Code != 200 || len(cookies) != 1 || cookies[0].Name != oidcCookieName || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Domain != "" {
		t.Fatal("invalid OIDC start cookie policy")
	}
	if strings.Contains(response.Body.String(), transportToken(3)) {
		t.Fatal("browser binding leaked into body")
	}
}

func TestOIDCCallbackRejectsAmbiguousInputBeforeProviderAndHidesCodes(t *testing.T) {
	for _, query := range []string{"state=x&state=y&code=private", "state=x&code=private&error=denied", "state=x&code=private&extra=x", "state=x&code=%invalid"} {
		stub := &identityTransportStub{}
		handler := identityTransport(t, stub, true)
		request := identityRequest(http.MethodGet, "/api/v1/auth/oidc/callback?"+query, "")
		request.AddCookie(&http.Cookie{Name: oidcCookieName, Value: transportToken(3)})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if stub.callbackCalls != 0 || response.Code != 303 || strings.Contains(response.Header().Get("Location"), "private") || strings.Contains(response.Body.String(), "private") {
			t.Fatal("ambiguous callback reached provider or leaked code")
		}
	}
	stub := &identityTransportStub{}
	handler := identityTransport(t, stub, true)
	request := identityRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state="+transportToken(5)+"&code=private", "")
	request.Header.Del("Origin")
	request.AddCookie(&http.Cookie{Name: oidcCookieName, Value: transportToken(3)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 303 || stub.callbackCalls != 1 || response.Header().Get("Location") != "/auth/complete?result=success" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("valid callback did not complete")
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == SessionCookieName && (!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode) {
			t.Fatal("callback weakened Session cookie")
		}
	}
	finish := httptest.NewRecorder()
	handler.ServeHTTP(finish, identityRequest(http.MethodGet, "/auth/complete?result=%3Cscript%3Eprivate%3C/script%3E", ""))
	if strings.Contains(finish.Body.String(), "private") || !strings.Contains(finish.Body.String(), `href="/"`) {
		t.Fatal("completion reflected callback input")
	}
}

func TestInvitationAcceptanceRejectsStaleCookieInsteadOfCreatingNewAccount(t *testing.T) {
	stub := &identityTransportStub{csrfErr: authn.ErrInvalidSession}
	handler := identityTransport(t, stub, false)
	request := identityRequest(http.MethodPost, "/api/v1/auth/invitations/accept", `{"invitation_token":"token"}`)
	withIdentityCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 401 || stub.calls != 0 {
		t.Fatal("stale session became anonymous registration")
	}
}
