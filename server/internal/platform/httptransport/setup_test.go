package httptransport

import (
	"context"
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type setupFake struct {
	status string
	err    error
	writes int
	input  authn.BootstrapInput
}

func (s *setupFake) Status(context.Context) (string, error) { return s.status, s.err }
func (s *setupFake) Complete(_ context.Context, _ string, input authn.BootstrapInput) error {
	s.writes++
	s.input = input
	return s.err
}

type setupReady struct{ err error }

func (s setupReady) CheckReady(context.Context) error { return s.err }

const setupBody = `{"setup_code":"synthetic-code","email":"admin@example.test","display_name":"Owner","password":"synthetic long password","workspace_name":"Workspace"}`

func setupHandlerTest(t *testing.T, app *setupFake, ready error, limit int) http.Handler {
	t.Helper()
	policy, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	return WithRequestID(NewSetupHandler(app, setupReady{ready}, policy, proxy, NewLoginGuard(limit, time.Minute, 10, 2)))
}
func TestSetupHTTPBoundary(t *testing.T) {
	app := &setupFake{status: "required"}
	h := setupHandlerTest(t, app, nil, 30)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/v1/setup", "", 200}, {"POST", "/api/v1/setup", setupBody, 201},
		{"POST", "/api/v1/setup?setup_code=no", setupBody, 400}, {"PUT", "/api/v1/setup", setupBody, 405},
		{"POST", "/api/v1/setup", strings.Replace(setupBody, `"email":`, `"email":"duplicate","email":`, 1), 400},
		{"POST", "/api/v1/setup", strings.Replace(setupBody, `"display_name":"Owner",`, "", 1), 400},
		{"POST", "/api/v1/setup", strings.Replace(setupBody, `"display_name":"Owner"`, `"display_name":null`, 1), 400},
		{"POST", "/api/v1/setup", strings.Replace(setupBody, "Owner", strings.Repeat("x", 4096), 1), 413},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(tc.method, tc.path, tc.body))
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "synthetic") {
			t.Fatal(tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	if app.writes != 1 || app.input.WorkspaceName != "Workspace" {
		t.Fatal(app)
	}
	for _, alter := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Origin") }, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, func(r *http.Request) { r.Host = "evil.example" }} {
		r := messagingWriteRequest("POST", "/api/v1/setup", setupBody)
		alter(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 || app.writes != 1 {
			t.Fatal("unsafe request accepted")
		}
	}
	app.err = authn.ErrAlreadyBootstrapped
	w := httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest("POST", "/api/v1/setup", setupBody))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "setup_complete") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSetupReadinessAndLimiter(t *testing.T) {
	app := &setupFake{status: "required"}
	w := httptest.NewRecorder()
	setupHandlerTest(t, app, errors.New("schema mismatch"), 1).ServeHTTP(w, messagingWriteRequest("POST", "/api/v1/setup", setupBody))
	if w.Code != 500 || app.writes != 0 || strings.Contains(w.Body.String(), "schema mismatch") {
		t.Fatal(w.Code)
	}
	h := setupHandlerTest(t, app, nil, 1)
	h.ServeHTTP(httptest.NewRecorder(), messagingWriteRequest("POST", "/api/v1/setup", setupBody))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest("POST", "/api/v1/setup", setupBody))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || app.writes != 1 {
		t.Fatal(w.Code)
	}
}
