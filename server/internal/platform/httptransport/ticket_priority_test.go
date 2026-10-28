package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type priorityApplication struct {
	calls  int
	in     goldenpath.TicketPriorityInput
	inv    goldenpath.Invocation
	result goldenpath.TicketPriorityResult
	read   goldenpath.TicketPriority
	err    error
}

func (s *priorityApplication) SetTicketPriority(_ context.Context, inv goldenpath.Invocation, in goldenpath.TicketPriorityInput) (goldenpath.TicketPriorityResult, error) {
	s.calls++
	s.in = in
	s.inv = inv
	return s.result, s.err
}
func (s *priorityApplication) ReadTicketPriority(context.Context, authz.Principal, string) (goldenpath.TicketPriority, error) {
	s.calls++
	return s.read, s.err
}
func TestTicketPriorityHTTPBoundary(t *testing.T) {
	app := &priorityApplication{result: goldenpath.TicketPriorityResult{Applied: true, PriorityRevision: 2, Priority: "high"}, read: goldenpath.TicketPriority{Priority: "unset", PriorityRevision: 1, StatusRevision: 1, CanSetPriority: true}}
	policy, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	h := WithRequestID(NewTicketPriorityHandler(validMessagingSessions(), app, policy, proxy))
	path := "/api/v1/workspaces/wrk_main/tickets/tkt_one/priority"
	body := `{"client_operation_id":"priority","base_priority_revision":1,"base_status_revision":1,"priority":"high","confirmed":true}`
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(method, path, body))
		return w
	}
	w := request("POST", path, body)
	if w.Code != 200 || app.calls != 1 || app.inv.Principal.ID != "usr_reader" || app.inv.SourceKind != "web" || app.in.TicketID != "tkt_one" || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "Cookie" || !strings.Contains(w.Body.String(), `"ticket":{"type":"ticket","id":"tkt_one"}`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, bad := range []string{strings.Replace(body, `"priority":"high",`, "", 1), strings.Replace(body, `"priority":"high"`, `"priority":null`, 1), strings.Replace(body, `"priority":"high"`, `"priority":"highest"`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(body, `"base_priority_revision":1`, `"base_priority_revision":null`, 1), strings.Replace(body, `"base_priority_revision":1`, `"base_priority_revision":1.5`, 1), strings.Replace(body, `"base_status_revision":1`, `"base_status_revision":2147483648`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"actor":"usr_admin"`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), body + body} {
		w = request("POST", path, bad)
		if w.Code != 400 || app.calls != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://other.example.test") }, func(r *http.Request) { r.Header.Del("Cookie") }, func(r *http.Request) { r.Host = "evil.test" }, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }} {
		r := messagingWriteRequest("POST", path, body)
		mutate(r)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 || app.calls != 1 {
			t.Fatal(w.Code)
		}
	}
	if w = request("POST", path, strings.Repeat(" ", 8192)+body); w.Code != 413 {
		t.Fatal(w.Code)
	}
	if w = request("GET", path, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"priority":"unset"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, method := range []string{"PUT", "DELETE", "HEAD"} {
		w = request(method, path, "")
		if w.Code != 405 || w.Header().Get("Allow") != "GET, POST" {
			t.Fatal(w.Code)
		}
	}
	for _, method := range []string{"GET", "POST"} {
		if w = request(method, path+"?priority=high", body); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	if w = request("GET", path+"/unknown", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	app.result.PriorityRevision = 3
	if w = request("POST", path, body); w.Code != 500 {
		t.Fatal("result mismatch", w.Code)
	}
	app.read.Priority = "invalid"
	if w = request("GET", path, ""); w.Code != 500 {
		t.Fatal("invalid priority", w.Code)
	}
	app.err = errors.New("private database detail")
	w = request("GET", path, "")
	if w.Code != 500 || strings.Contains(w.Body.String(), "private database detail") {
		t.Fatal(w.Code, w.Body.String())
	}
}
