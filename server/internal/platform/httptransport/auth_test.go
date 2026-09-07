package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
)

type fakeSessionService struct {
	login          func(context.Context, authn.LoginInput) (authn.Session, error)
	resolveSession func(context.Context, string) (authn.SessionAccount, error)
	revokeSession  func(context.Context, string, string) error
}

func (service *fakeSessionService) Login(ctx context.Context, input authn.LoginInput) (authn.Session, error) {
	return service.login(ctx, input)
}

func (service *fakeSessionService) ResolveSession(ctx context.Context, token string) (authn.SessionAccount, error) {
	return service.resolveSession(ctx, token)
}

func (service *fakeSessionService) RevokeSession(ctx context.Context, token string, csrfToken string) error {
	return service.revokeSession(ctx, token, csrfToken)
}

func TestAuthHandlerLoginSetsSecureCookiesAndReturnsOnlySafeContext(t *testing.T) {
	t.Parallel()
	expiresAt := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	service := &fakeSessionService{
		login: func(_ context.Context, input authn.LoginInput) (authn.Session, error) {
			if input != (authn.LoginInput{LoginName: "admin", Password: "correct horse battery staple"}) {
				t.Fatalf("Login() input = %#v", input)
			}
			return authn.Session{
				Token:     transportToken(1),
				CSRFToken: transportToken(2),
				Account:   testSessionAccount(expiresAt),
			}, nil
		},
	}
	handler := testAuthHandler(t, service, NewLoginGuard(5, time.Minute, 8, 2))
	request := secureJSONRequest(http.MethodPost, "/api/v1/auth/sessions", `{"login_name":"admin","password":"correct horse battery staple"}`)
	request.Header.Set("Origin", "https://nexus.example.test")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = status %d, headers %#v", response.Code, response.Header())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 || !cookies[0].Secure || !cookies[0].HttpOnly || !cookies[1].Secure || cookies[1].HttpOnly {
		t.Fatalf("cookies = %#v", cookies)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"display_name":"First Admin"`) ||
		!strings.Contains(body, `"role":"owner"`) ||
		strings.Contains(body, transportToken(1)) || strings.Contains(body, transportToken(2)) ||
		strings.Contains(body, "correct horse") {
		t.Fatalf("response body = %q", body)
	}
	if requestID := response.Header().Get("X-Request-ID"); len(requestID) != 36 || strings.Contains(response.Body.String(), requestID) {
		t.Fatalf("request ID = %q, body = %q", requestID, body)
	}
}

func TestAuthHandlerRejectsUnsafeLoginBeforeCredentialVerification(t *testing.T) {
	t.Parallel()
	loginCalls := 0
	service := &fakeSessionService{login: func(context.Context, authn.LoginInput) (authn.Session, error) {
		loginCalls++
		return authn.Session{}, nil
	}}
	tests := []struct {
		name       string
		request    func() *http.Request
		wantStatus int
		wantCode   string
	}{
		{
			name: "plaintext untrusted",
			request: func() *http.Request {
				request := httptest.NewRequest(http.MethodPost, "http://nexus.example.test/api/v1/auth/sessions", strings.NewReader(`{"login_name":"admin","password":"password"}`))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Origin", "https://nexus.example.test")
				return request
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "secure_transport_required",
		},
		{
			name: "wrong origin",
			request: func() *http.Request {
				request := secureJSONRequest(http.MethodPost, "/api/v1/auth/sessions", `{"login_name":"admin","password":"password"}`)
				request.Header.Set("Origin", "https://evil.example.test")
				return request
			},
			wantStatus: http.StatusForbidden,
			wantCode:   "csrf_failed",
		},
		{
			name: "wrong content type",
			request: func() *http.Request {
				request := secureJSONRequest(http.MethodPost, "/api/v1/auth/sessions", `{}`)
				request.Header.Set("Origin", "https://nexus.example.test")
				request.Header.Set("Content-Type", "text/plain")
				return request
			},
			wantStatus: http.StatusUnsupportedMediaType,
			wantCode:   "unsupported_media_type",
		},
		{
			name: "oversized body",
			request: func() *http.Request {
				request := secureJSONRequest(http.MethodPost, "/api/v1/auth/sessions", `{"login_name":"admin","password":"`+strings.Repeat("a", int(MaxLoginBodyBytes))+`"}`)
				request.Header.Set("Origin", "https://nexus.example.test")
				return request
			},
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "payload_too_large",
		},
		{
			name: "unknown field",
			request: func() *http.Request {
				request := secureJSONRequest(http.MethodPost, "/api/v1/auth/sessions", `{"login_name":"admin","password":"password","remember":true}`)
				request.Header.Set("Origin", "https://nexus.example.test")
				return request
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid",
		},
		{
			name: "multiple JSON values",
			request: func() *http.Request {
				request := secureJSONRequest(http.MethodPost, "/api/v1/auth/sessions", `{"login_name":"admin","password":"password"} {}`)
				request.Header.Set("Origin", "https://nexus.example.test")
				return request
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testAuthHandler(t, service, NewLoginGuard(5, time.Minute, 8, 2)).ServeHTTP(response, test.request())
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("response = status %d, body %q", response.Code, response.Body.String())
			}
		})
	}
	if loginCalls != 0 {
		t.Fatalf("Login() calls = %d", loginCalls)
	}
}

func TestAuthHandlerRateLimitsBeforeRepeatedCredentialVerification(t *testing.T) {
	t.Parallel()
	loginCalls := 0
	service := &fakeSessionService{login: func(context.Context, authn.LoginInput) (authn.Session, error) {
		loginCalls++
		return authn.Session{}, authn.ErrInvalidCredentials
	}}
	handler := testAuthHandler(t, service, NewLoginGuard(1, time.Minute, 8, 1))
	for attempt, wantStatus := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		request := secureJSONRequest(http.MethodPost, "/api/v1/auth/sessions", `{"login_name":"admin","password":"wrong password"}`)
		request.Header.Set("Origin", "https://nexus.example.test")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != wantStatus {
			t.Fatalf("attempt %d status = %d, body %q", attempt+1, response.Code, response.Body.String())
		}
		if attempt == 1 && response.Header().Get("Retry-After") == "" {
			t.Fatal("rate-limited response is missing Retry-After")
		}
	}
	if loginCalls != 1 {
		t.Fatalf("Login() calls = %d", loginCalls)
	}
}

func TestAuthHandlerResolvesAndRevokesSessionWithDatabaseCSRFCheck(t *testing.T) {
	t.Parallel()
	token := transportToken(1)
	csrfToken := transportToken(2)
	expiresAt := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	revoked := false
	service := &fakeSessionService{
		resolveSession: func(_ context.Context, gotToken string) (authn.SessionAccount, error) {
			if gotToken != token {
				t.Fatalf("ResolveSession() token = %q", gotToken)
			}
			return testSessionAccount(expiresAt), nil
		},
		revokeSession: func(_ context.Context, gotToken string, gotCSRF string) error {
			if gotToken != token || gotCSRF != csrfToken {
				t.Fatalf("RevokeSession() = %q, %q", gotToken, gotCSRF)
			}
			revoked = true
			return nil
		},
	}
	handler := testAuthHandler(t, service, NewLoginGuard(5, time.Minute, 8, 2))

	getRequest := httptest.NewRequest(http.MethodGet, "https://nexus.example.test/api/v1/auth/session", nil)
	getRequest.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"id":"usr_1"`) {
		t.Fatalf("GET session = status %d, body %q", getResponse.Code, getResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "https://nexus.example.test/api/v1/auth/session", nil)
	deleteRequest.Header.Set("Origin", "https://nexus.example.test")
	deleteRequest.Header.Set(CSRFHeaderName, csrfToken)
	deleteRequest.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	deleteRequest.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfToken})
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || !revoked || len(deleteResponse.Result().Cookies()) != 2 {
		t.Fatalf("DELETE session = status %d, revoked %t, cookies %#v", deleteResponse.Code, revoked, deleteResponse.Result().Cookies())
	}
}

func TestAuthHandlerRejectsCSRFBeforeRevocation(t *testing.T) {
	t.Parallel()
	service := &fakeSessionService{revokeSession: func(context.Context, string, string) error {
		return errors.New("must not be called")
	}}
	handler := testAuthHandler(t, service, NewLoginGuard(5, time.Minute, 8, 2))
	request := httptest.NewRequest(http.MethodDelete, "https://nexus.example.test/api/v1/auth/session", nil)
	request.Header.Set("Origin", "https://nexus.example.test")
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: transportToken(1)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("response = status %d, body %q", response.Code, response.Body.String())
	}
}

func TestAuthHandlerUsesVersionedNoStoreErrorsForMethodAndPath(t *testing.T) {
	t.Parallel()
	service := &fakeSessionService{}
	handler := testAuthHandler(t, service, NewLoginGuard(5, time.Minute, 8, 2))
	tests := []struct {
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{method: http.MethodHead, path: "/api/v1/auth/session", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{method: http.MethodPatch, path: "/api/v1/auth/sessions", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{method: http.MethodGet, path: "/api/v1/auth/unknown", wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{method: http.MethodGet, path: "/api/v1/auth", wantStatus: http.StatusNotFound, wantCode: "not_found"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, "https://nexus.example.test"+test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.wantStatus || response.Header().Get("Cache-Control") != "no-store" ||
			!strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) ||
			!strings.Contains(response.Body.String(), `"request_id":"req_`) {
			t.Fatalf("%s %s = status %d, headers %#v, body %q", test.method, test.path, response.Code, response.Header(), response.Body.String())
		}
		if test.wantStatus == http.StatusMethodNotAllowed && response.Header().Get("Allow") == "" {
			t.Fatalf("%s %s is missing Allow", test.method, test.path)
		}
	}
}

func testAuthHandler(t *testing.T, service SessionService, guard *LoginGuard) http.Handler {
	t.Helper()
	session, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	proxy, err := NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	return WithRequestID(NewAuthHandler(service, session, proxy, guard))
}

func secureJSONRequest(method string, path string, body string) *http.Request {
	request := httptest.NewRequest(method, "https://nexus.example.test"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func testSessionAccount(expiresAt time.Time) authn.SessionAccount {
	return authn.SessionAccount{
		User:       authn.User{ID: "usr_1", DisplayName: "First Admin"},
		Workspaces: []authn.Workspace{{ID: "wrk_1", Name: "First Workspace", Role: "owner"}},
		ExpiresAt:  expiresAt,
	}
}
