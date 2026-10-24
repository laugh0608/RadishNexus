package httptransport

import (
	"context"
	"errors"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type contextTestApplication struct {
	calls     int
	err       error
	principal authz.Principal
	token     string
}

func (a *contextTestApplication) Export(_ context.Context, p authz.Principal, token, project string) (cp.Bundle, error) {
	a.calls++
	a.principal = p
	a.token = token
	return cp.Bundle{}, a.err
}
func (a *contextTestApplication) Prepare(context.Context, authz.Principal, string, cp.Bundle, string) (cp.Plan, error) {
	a.calls++
	return cp.Plan{}, a.err
}
func (a *contextTestApplication) Apply(context.Context, authz.Principal, string, cp.Bundle, cp.Plan, string) (cp.Result, error) {
	a.calls++
	return cp.Result{}, a.err
}
func (a *contextTestApplication) Result(context.Context, authz.Principal, string, string) (cp.Result, error) {
	a.calls++
	return cp.Result{Status: "not_observed"}, a.err
}
func (a *contextTestApplication) Source(context.Context, authz.Principal, entityref.Ref) (goldenpath.ContextSource, error) {
	a.calls++
	return goldenpath.ContextSource{}, a.err
}
func contextHTTPHandler(t *testing.T, a *contextTestApplication) http.Handler {
	t.Helper()
	policy, e := NewBrowserSessionPolicy("https://nexus.example.test")
	if e != nil {
		t.Fatal(e)
	}
	proxy, e := NewTrustedProxyPolicy("10.0.0.0/8")
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	RegisterContextRoutes(mux, NewContextHandler(validMessagingSessions(), a, policy, proxy))
	return WithRequestID(mux)
}
func TestContextHTTPAuthorizationAndStrictInputs(t *testing.T) {
	a := &contextTestApplication{}
	h := contextHTTPHandler(t, a)
	path := "/api/v1/workspaces/wrk_main/projects/prj_main/context-export"
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(method, path, body))
		return w
	}
	if w := request("POST", path, "{}"); w.Code != 200 || a.calls != 1 || a.principal.ID != "usr_reader" || a.token == "" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`null`, `[]`, `{} {}`, `{"actor_id":"usr_other"}`, `{"actor_id":"a","actor_id":"b"}`} {
		w := request("POST", path, body)
		if w.Code != 400 || a.calls != 1 {
			t.Fatal("invalid body reached exporter", w.Code)
		}
	}
	if w := request("POST", path, strings.Repeat(" ", 1025)+`{}`); w.Code != 413 || a.calls != 1 {
		t.Fatal("unbounded export input", w.Code)
	}
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Del("Cookie") }, func(r *http.Request) { r.Header.Set("Origin", "https://other.example.test") }, func(r *http.Request) { r.Host = "other.example.test" }} {
		r := messagingWriteRequest("POST", path, "{}")
		change(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 || a.calls != 1 {
			t.Fatal("unsafe request reached exporter", w.Code)
		}
	}
	if w := request("POST", path+"?actor=other", "{}"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request("GET", path, ""); w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/api/v1/workspaces/wrk_main/context-imports/prepare", "/api/v1/workspaces/wrk_main/context-imports/apply"} {
		if w := request("POST", path, `{"unexpected":true}`); w.Code != 400 || a.calls != 1 {
			t.Fatal("unknown import field reached service", w.Code)
		}
	}
}
func TestContextHTTPSourceAndUnconfirmedErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{cp.ErrIncomplete, 404, "not_found"}, {cp.ErrInvalid, 400, "invalid"}, {cp.ErrUnconfirmed, 503, "result_unconfirmed"}, {authz.ErrConflict, 409, "conflict"}, {errors.New("synthetic private body and credentials"), 500, "internal"}} {
		a := &contextTestApplication{err: tc.err}
		h := contextHTTPHandler(t, a)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest("POST", "/api/v1/workspaces/wrk_main/projects/prj_main/context-export", "{}"))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "credentials") || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	a := &contextTestApplication{}
	h := contextHTTPHandler(t, a)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest("GET", "/api/v1/workspaces/wrk_main/context-source/document/doc_main", ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"imported":false`) || a.calls != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}
