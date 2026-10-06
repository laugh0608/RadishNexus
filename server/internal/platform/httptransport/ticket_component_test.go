package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type testTicketComponent struct {
	calls  int
	kind   string
	in     goldenpath.TicketComponentInput
	inv    goldenpath.Invocation
	page   goldenpath.TicketComponentPage
	result goldenpath.TicketComponentResult
	source entityref.Ref
	query  goldenpath.DiscoveryPageInput
}

func (s *testTicketComponent) WriteTicketComponent(_ context.Context, inv goldenpath.Invocation, kind string, in goldenpath.TicketComponentInput) (goldenpath.TicketComponentResult, error) {
	s.calls++
	s.in = in
	s.kind = kind
	s.inv = inv
	return s.result, nil
}
func (s *testTicketComponent) ListTicketComponents(_ context.Context, _ authz.Principal, source entityref.Ref, in goldenpath.DiscoveryPageInput) (goldenpath.TicketComponentPage, error) {
	s.calls++
	s.source = source
	s.query = in
	return s.page, nil
}
func ticketComponentTestHandler(t *testing.T, app *testTicketComponent) http.Handler {
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
	RegisterTicketComponentRoutes(mux, NewTicketComponentHandler(validMessagingSessions(), app, policy, proxy))
	return WithRequestID(mux)
}
func TestTicketComponentHTTPStrictBoundary(t *testing.T) {
	app := &testTicketComponent{result: goldenpath.TicketComponentResult{LinkID: "lnk_one", Created: true}}
	h := ticketComponentTestHandler(t, app)
	path := "/api/v1/workspaces/wrk_main/tickets/tkt_one/components"
	body := `{"client_operation_id":"link","component_id":"cmp_one","confirmed":true}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest("POST", path, body))
	if w.Code != 201 || app.kind != "ticket.component.link" || app.in.TicketID != "tkt_one" || app.in.ComponentID != "cmp_one" || app.inv.Principal.ID != "usr_reader" || app.inv.SourceKind != "web" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Body.String(), app)
	}
	for _, bad := range []string{strings.Replace(body, "true", "false", 1), strings.Replace(body, `,"confirmed":true`, "", 1), strings.Replace(body, `"component_id":"cmp_one"`, `"component_id":null`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"actor_id":"usr_admin"`, 1), body + body, strings.Repeat(" ", 8192) + body} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest("POST", path, bad))
		if w.Code != 400 && w.Code != 413 {
			t.Fatal("body accepted", w.Code, w.Body.String())
		}
		if app.calls != 1 {
			t.Fatal("bad body reached app")
		}
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://other.example.test") }} {
		r := messagingWriteRequest("POST", path, body)
		mutate(r)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || app.calls != 1 {
			t.Fatal("CSRF/origin", w.Code)
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest("POST", path+"?extra=1", body))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	app.result.Created = false
	path = "/api/v1/workspaces/wrk_main/tickets/tkt_one/component-links/lnk_one"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest("DELETE", path, `{"client_operation_id":"unlink","confirmed":true}`))
	if w.Code != 200 || app.kind != "ticket.component.unlink" || app.in.LinkID != "lnk_one" {
		t.Fatal(w.Code, w.Body.String())
	}
	app.result.LinkID = "lnk_wrong"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest("DELETE", path, `{"client_operation_id":"unlink","confirmed":true}`))
	if w.Code != 500 || strings.Contains(w.Body.String(), "lnk_wrong") {
		t.Fatal("invalid result", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest("GET", path, ""))
	if w.Code != 405 || w.Header().Get("Allow") != "DELETE" {
		t.Fatal("method", w.Code)
	}
}
func TestTicketComponentHTTPCursorAndDTO(t *testing.T) {
	c := goldenpath.ConfigurationObject{ID: "cmp_one", Kind: "component", Key: "service", Name: "Service", Type: "service", Status: "active", OwnerTeamID: "tem_main"}
	app := &testTicketComponent{page: goldenpath.TicketComponentPage{CanLink: true, Links: []goldenpath.TicketComponentLink{{ID: "lnk_one", Component: &c, CanUnlink: true}}, NextID: "cmp_one"}}
	h := ticketComponentTestHandler(t, app)
	path := "/api/v1/workspaces/wrk_main/tickets/tkt_one/components"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest("GET", path+"?limit=1", ""))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var payload struct {
		Data struct {
			Next string `json:"next_cursor"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &payload); e != nil {
		t.Fatal(e)
	}
	app.page = goldenpath.TicketComponentPage{}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest("GET", path+"?after="+payload.Data.Next, ""))
	if w.Code != 200 || app.query.AfterID != "cmp_one" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, other := range []string{strings.Replace(path, "tkt_one", "tkt_other", 1), "/api/v1/workspaces/wrk_main/components/cmp_one/tickets", strings.Replace(path, "wrk_main", "wrk_other", 1)} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, messagingRequest("GET", other+"?after="+payload.Data.Next, ""))
		if w.Code == 200 {
			t.Fatal("cross scope", other)
		}
	}
	app.page = goldenpath.TicketComponentPage{Links: []goldenpath.TicketComponentLink{{ID: "lnk_one", Component: &c}}, NextID: "cmp_hidden"}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest("GET", path+"?limit=1", ""))
	if w.Code != 500 || strings.Contains(w.Body.String(), "cmp_hidden") {
		t.Fatal("invalid cursor leaked", w.Code)
	}
}
