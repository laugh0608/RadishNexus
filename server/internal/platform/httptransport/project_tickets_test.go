package httptransport

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestProjectTicketsRejectsInvalidQueriesAndForeignCursors(t *testing.T) {
	path := "/api/v1/workspaces/wrk_main/projects/prj_main/tickets"
	scope := discoveryCursor{Version: 2, ActorID: "usr_reader", WorkspaceID: "wrk_main", ProjectID: "prj_main", Kind: "ticket", AfterID: "tkt_previous"}
	queries := []string{"limit=0", "limit=51", "limit=01", "limit=1&limit=2", "after=", "after=a&after=b", "status=unknown", "status=", "status=open&status=done", "assignee=usr_reader", "assignee=all", "priority=", "priority=highest", "sort=priority", "q=private", "after=bad", "limit=%zz", strings.Repeat("a", 2049)}
	for _, change := range []func(*discoveryCursor){
		func(c *discoveryCursor) { c.Version++ },
		func(c *discoveryCursor) { c.Version = 1 },
		func(c *discoveryCursor) { c.ActorID = "usr_other" },
		func(c *discoveryCursor) { c.Status = "open" },
		func(c *discoveryCursor) { c.Assignee = "me" },
		func(c *discoveryCursor) { c.Priority = "high" },
		func(c *discoveryCursor) { c.WorkspaceID = "wrk_other" },
		func(c *discoveryCursor) { c.ProjectID = "prj_other" },
		func(c *discoveryCursor) { c.Kind = "component-tickets" },
		func(c *discoveryCursor) { c.Kind = "channel" },
		func(c *discoveryCursor) { c.AfterID = "dec_wrong" },
	} {
		cursor := scope
		change(&cursor)
		body, _ := json.Marshal(cursor)
		queries = append(queries, "after="+base64.RawURLEncoding.EncodeToString(body))
	}
	for _, body := range []string{
		`{"v":1,"v":1,"workspace_id":"wrk_main","kind":"ticket","project_id":"prj_main","after_id":"tkt_previous"}`,
		`{"v":1,"workspace_id":"wrk_main","kind":"ticket","project_id":"prj_main","after_id":"tkt_previous","extra":true}`,
	} {
		queries = append(queries, "after="+base64.RawURLEncoding.EncodeToString([]byte(body)))
	}
	for _, query := range queries {
		reader := &fakeDiscoveryReader{}
		w := httptest.NewRecorder()
		discoveryHandlerForTest(t, validMessagingSessions(), reader).ServeHTTP(w, messagingRequest("GET", path+"?"+query, ""))
		if w.Code != 400 || reader.calls != 0 {
			t.Fatalf("query %q: %d %s", query, w.Code, w.Body.String())
		}
	}
}

func TestProjectTicketsTransportAndSafeFailure(t *testing.T) {
	path := "/api/v1/workspaces/wrk_main/projects/prj_main/tickets"
	for _, tc := range []struct {
		name                string
		change              func(*http.Request)
		sessionErr, readErr error
		status              int
	}{
		{"anonymous", func(r *http.Request) { r.Header.Del("Cookie") }, nil, nil, 401},
		{"host", func(r *http.Request) { r.Host = "evil.test" }, nil, nil, 400},
		{"proxy", func(r *http.Request) {
			r.TLS = nil
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
		}, nil, nil, 400},
		{"expired", nil, authn.ErrInvalidSession, nil, 401},
		{"workspace revoked", nil, authz.ErrForbidden, nil, 404},
		{"project revoked", nil, nil, authz.ErrNotFound, 404},
		{"invalid id", func(r *http.Request) { r.URL.Path = strings.ReplaceAll(path, "prj_main", "tkt_wrong") }, nil, nil, 400},
		{"unknown path", func(r *http.Request) { r.URL.Path += "/unknown" }, nil, nil, 404},
		{"POST", func(r *http.Request) { r.Method = "POST" }, nil, nil, 405},
		{"HEAD", func(r *http.Request) { r.Method = "HEAD" }, nil, nil, 405},
		{"database error", nil, nil, errors.New("private-source-thread"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions := validMessagingSessions()
			sessions.resolveErr = tc.sessionErr
			reader := &fakeDiscoveryReader{err: tc.readErr}
			r := messagingRequest("GET", path, "")
			if tc.change != nil {
				tc.change(r)
			}
			w := httptest.NewRecorder()
			discoveryHandlerForTest(t, sessions, reader).ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "Cookie" || strings.Contains(w.Body.String(), "private-source-thread") {
				t.Fatal(w.Code, w.Body.String(), w.Header())
			}
			if tc.status == 405 && w.Header().Get("Allow") != "GET" {
				t.Fatal(w.Header())
			}
		})
	}
}

func TestProjectTicketsRejectsProjectionDriftWithoutPartialResults(t *testing.T) {
	item := goldenpath.DiscoveryItem{Ref: entityref.Ref{Type: "ticket", ID: "tkt_one"}, Title: "Visible work", Status: "open", Priority: "unset"}
	for _, change := range []func(*goldenpath.DiscoveryItem){
		func(i *goldenpath.DiscoveryItem) { i.Status = "unknown" },
		func(i *goldenpath.DiscoveryItem) { i.Priority = "unknown" },
		func(i *goldenpath.DiscoveryItem) { i.Ref.Type = "decision" },
		func(i *goldenpath.DiscoveryItem) { i.Ref.ID = "dec_wrong" },
		func(i *goldenpath.DiscoveryItem) { i.Title = "\x00private" },
		func(i *goldenpath.DiscoveryItem) { i.Title = " " },
	} {
		bad := item
		bad.Ref.ID = "tkt_two"
		change(&bad)
		reader := &fakeDiscoveryReader{page: goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{item, bad}}}
		w := httptest.NewRecorder()
		discoveryHandlerForTest(t, validMessagingSessions(), reader).ServeHTTP(w, messagingRequest("GET", "/api/v1/workspaces/wrk_main/projects/prj_main/tickets", ""))
		if w.Code != 500 || strings.Contains(w.Body.String(), item.Title) || strings.Contains(w.Body.String(), "private") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	scope := discoveryCursor{Version: 2, ActorID: "usr_reader", WorkspaceID: "wrk_main", ProjectID: "prj_main", Kind: "ticket"}
	if _, e := publicDiscoveryPage(goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{item}}, goldenpath.DiscoveryPageInput{Limit: 25, AfterID: "tkt_one"}, scope); e == nil {
		t.Fatal("non-exclusive boundary accepted")
	}
}

func TestProjectTicketFiltersBindCursorAndPrincipal(t *testing.T) {
	path := "/api/v1/workspaces/wrk_main/projects/prj_main/tickets"
	filters := goldenpath.TicketFilters{Status: "active", Assignee: "me", Priority: "high"}
	reader := &fakeDiscoveryReader{page: goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{{Ref: entityref.Ref{Type: "ticket", ID: "tkt_one"}, Title: "My work", Status: "in-progress", Priority: "high", Assignee: &goldenpath.TicketAssignee{ID: "usr_reader", DisplayName: "Reader"}}}, NextID: "tkt_one"}}
	handler := discoveryHandlerForTest(t, validMessagingSessions(), reader)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, messagingRequest("GET", path+"?limit=1&status=active&assignee=me&priority=high", ""))
	if w.Code != 200 || reader.input.TicketFilters != filters {
		t.Fatal(w.Code, w.Body.String(), reader.input)
	}
	var body struct {
		Data discoveryPageDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Data.NextCursor == nil {
		t.Fatal(err)
	}
	cursor := *body.Data.NextCursor
	scope := discoveryCursor{Version: 2, WorkspaceID: "wrk_main", ProjectID: "prj_main", Kind: "ticket", ActorID: "usr_reader"}
	in, err := parseDiscoveryQuery("limit=50&priority=high&assignee=me&status=active&after="+cursor, scope)
	if err != nil || in.AfterID != "tkt_one" || in.Limit != 50 || in.TicketFilters != filters {
		t.Fatal(in, err)
	}
	for _, q := range []string{"status=open&assignee=me&priority=high", "status=active&assignee=unassigned&priority=high", "status=active&assignee=me&priority=normal", ""} {
		if _, err := parseDiscoveryQuery(q+"&after="+cursor, scope); err == nil {
			t.Fatal("foreign filters accepted", q)
		}
	}
	scope.ActorID = "usr_other"
	if _, err := parseDiscoveryQuery("status=active&assignee=me&priority=high&after="+cursor, scope); err == nil {
		t.Fatal("foreign actor accepted")
	}
	for _, mutate := range []func(*goldenpath.DiscoveryItem){func(v *goldenpath.DiscoveryItem) { v.Priority = "normal" }, func(v *goldenpath.DiscoveryItem) { v.Status = "done" }, func(v *goldenpath.DiscoveryItem) { v.Assignee = nil }} {
		original := reader.page.Items[0]
		mutate(&reader.page.Items[0])
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, messagingRequest("GET", path+"?limit=1&status=active&assignee=me&priority=high", ""))
		if w.Code != 500 {
			t.Fatal("filter drift accepted", w.Code)
		}
		reader.page.Items[0] = original
	}
}
