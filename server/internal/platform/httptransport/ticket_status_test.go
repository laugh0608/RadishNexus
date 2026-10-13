package httptransport

import (
	"context"
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type statusApplication struct {
	calls  int
	in     goldenpath.TicketStatusInput
	inv    goldenpath.Invocation
	result goldenpath.TicketStatusResult
	err    error
}

func (s *statusApplication) ChangeTicketStatus(_ context.Context, inv goldenpath.Invocation, in goldenpath.TicketStatusInput) (goldenpath.TicketStatusResult, error) {
	s.calls++
	s.in = in
	s.inv = inv
	return s.result, s.err
}
func TestTicketStatusHTTP(t *testing.T) {
	app := &statusApplication{result: goldenpath.TicketStatusResult{TicketID: "tkt_one", AppliedStatus: "done", AppliedStatusRevision: 2}}
	policy, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	h := WithRequestID(NewTicketStatusHandler(validMessagingSessions(), app, policy, proxy))
	path := "/api/v1/workspaces/wrk_main/tickets/tkt_one/status-transitions"
	body := `{"client_operation_id":"status","base_status_revision":1,"to_status":"done","confirmed":true}`
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(method, path, body))
		return w
	}
	w := request("POST", path, body)
	if w.Code != 200 || app.calls != 1 || app.inv.SourceKind != "web" || app.inv.Principal.ID != "usr_reader" || app.in.BaseStatusRevision != 1 || w.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(w.Body.String(), `"applied_status_revision":2`) {
		t.Fatal(w.Code, w.Body.String(), app)
	}
	for _, bad := range []string{strings.Replace(body, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(body, `"base_status_revision":1`, `"base_status_revision":null`, 1), strings.Replace(body, `"base_status_revision":1`, `"base_status_revision":1.5`, 1), strings.Replace(body, `"base_status_revision":1`, `"base_status_revision":2147483648`, 1), strings.Replace(body, `"to_status":"done"`, `"to_status":"unknown"`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"actor":"usr_admin"`, 1), body + body, strings.Repeat(" ", 8192) + body} {
		w = request("POST", path, bad)
		if (w.Code != 400 && w.Code != 413) || app.calls != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://other.example.test") }, func(r *http.Request) { r.Host = "other.example.test" }, func(r *http.Request) { r.Header.Del("Cookie") }} {
		r := messagingWriteRequest("POST", path, body)
		mutate(r)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 || app.calls != 1 {
			t.Fatal(w.Code)
		}
	}
	if w = request("POST", path+"?x=1", body); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w = request("GET", path, ""); w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal(w.Code)
	}
	for _, tc := range []struct {
		err  error
		code int
	}{{authz.ErrNotFound, 404}, {authz.ErrForbidden, 403}, {authz.ErrConflict, 409}, {errors.New("private database detail"), 500}} {
		app.err = tc.err
		w = request("POST", path, body)
		if w.Code != tc.code || strings.Contains(w.Body.String(), "private database") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	app.err = nil
	app.result.AppliedStatusRevision = 3
	w = request("POST", path, body)
	if w.Code != 500 {
		t.Fatal("drift accepted", w.Code)
	}
}
