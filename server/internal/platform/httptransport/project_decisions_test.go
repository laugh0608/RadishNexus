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

func TestProjectDecisionsRejectsInvalidQueriesAndForeignCursors(t *testing.T) {
	path := "/api/v1/workspaces/wrk_main/projects/prj_main/decisions"
	scope := discoveryCursor{Version: 1, WorkspaceID: "wrk_main", ProjectID: "prj_main", Kind: "decision", AfterID: "dec_previous"}
	queries := []string{"limit=0", "limit=51", "limit=-1", "limit=01", "limit=+1", "limit=1.0", "limit=", "limit=1&limit=2", "after=", "after=a&after=b", "status=open", "q=private", "after=bad", "limit=%zz", "limit=1;after=x", "after=" + strings.Repeat("a", 1025), strings.Repeat("a", 2049)}
	for _, change := range []func(*discoveryCursor){
		func(c *discoveryCursor) { c.Version++ },
		func(c *discoveryCursor) { c.WorkspaceID = "wrk_other" },
		func(c *discoveryCursor) { c.ProjectID = "prj_other" },
		func(c *discoveryCursor) { c.Kind = "ticket"; c.AfterID = "tkt_previous" },
		func(c *discoveryCursor) { c.Kind = "document"; c.AfterID = "doc_previous" },
		func(c *discoveryCursor) { c.Kind = "project"; c.ProjectID = ""; c.AfterID = "prj_previous" },
		func(c *discoveryCursor) { c.CIRunID = "cir_extra" },
		func(c *discoveryCursor) { c.Kind = "channel" },
		func(c *discoveryCursor) { c.AfterID = "tkt_wrong" },
	} {
		cursor := scope
		change(&cursor)
		body, _ := json.Marshal(cursor)
		queries = append(queries, "after="+base64.RawURLEncoding.EncodeToString(body))
	}
	for _, body := range []string{
		`{"v":1,"v":1,"workspace_id":"wrk_main","kind":"decision","project_id":"prj_main","after_id":"dec_previous"}`,
		`{"v":1,"workspace_id":"wrk_main","kind":"decision","project_id":"prj_main","after_id":"dec_previous","extra":true}`,
		`{"v":1,"workspace_id":"wrk_main","kind":"decision","project_id":"prj_main","after_id":"dec_previous"} `,
		`{"workspace_id":"wrk_main","v":1,"kind":"decision","project_id":"prj_main","after_id":"dec_previous"}`,
	} {
		queries = append(queries, "after="+base64.RawURLEncoding.EncodeToString([]byte(body)))
		queries = append(queries, "after="+base64.URLEncoding.EncodeToString([]byte(body)))
	}
	for _, query := range queries {
		reader := &fakeDiscoveryReader{}
		w := httptest.NewRecorder()
		discoveryHandlerForTest(t, validMessagingSessions(), reader).ServeHTTP(w, messagingRequest("GET", path+"?"+query, ""))
		if w.Code != 400 || reader.calls != 0 || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "Cookie" || w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("query %q: %d %s", query, w.Code, w.Body.String())
		}
	}
}

func TestProjectDecisionsTransportAndSafeFailure(t *testing.T) {
	path := "/api/v1/workspaces/wrk_main/projects/prj_main/decisions"
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
		{"invalid id", func(r *http.Request) { r.URL.Path = strings.ReplaceAll(path, "prj_main", "dec_wrong") }, nil, nil, 400},
		{"unknown path", func(r *http.Request) { r.URL.Path += "/unknown" }, nil, nil, 404},
		{"POST", func(r *http.Request) { r.Method = "POST" }, nil, nil, 405},
		{"PUT", func(r *http.Request) { r.Method = "PUT" }, nil, nil, 405},
		{"DELETE", func(r *http.Request) { r.Method = "DELETE" }, nil, nil, 405},
		{"OPTIONS", func(r *http.Request) { r.Method = "OPTIONS" }, nil, nil, 405},
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

func TestProjectDecisionsRejectsProjectionDriftWithoutPartialResults(t *testing.T) {
	item := goldenpath.DiscoveryItem{Ref: entityref.Ref{Type: "decision", ID: "dec_one"}, Title: "Visible question", Status: "proposed"}
	for _, change := range []func(*goldenpath.DiscoveryItem){
		func(i *goldenpath.DiscoveryItem) { i.Status = "unknown" },
		func(i *goldenpath.DiscoveryItem) { i.Status = "rejected" },
		func(i *goldenpath.DiscoveryItem) { i.Status = "superseded" },
		func(i *goldenpath.DiscoveryItem) { i.Status = "active" },
		func(i *goldenpath.DiscoveryItem) { i.Ref.ID = "dec_one" },
		func(i *goldenpath.DiscoveryItem) { i.Ref.ID = "dec_aaa" },
		func(i *goldenpath.DiscoveryItem) { i.Title = string([]byte{0xff}) },
		func(i *goldenpath.DiscoveryItem) { i.Ref.Type = "ticket" },
		func(i *goldenpath.DiscoveryItem) { i.Ref.ID = "tkt_wrong" },
		func(i *goldenpath.DiscoveryItem) { i.Title = "\x00private" },
		func(i *goldenpath.DiscoveryItem) { i.Title = " " },
	} {
		bad := item
		bad.Ref.ID = "dec_two"
		change(&bad)
		reader := &fakeDiscoveryReader{page: goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{item, bad}}}
		w := httptest.NewRecorder()
		discoveryHandlerForTest(t, validMessagingSessions(), reader).ServeHTTP(w, messagingRequest("GET", "/api/v1/workspaces/wrk_main/projects/prj_main/decisions", ""))
		if w.Code != 500 || strings.Contains(w.Body.String(), item.Title) || strings.Contains(w.Body.String(), "private") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	scope := discoveryCursor{Version: 1, WorkspaceID: "wrk_main", ProjectID: "prj_main", Kind: "decision"}
	if _, e := publicDiscoveryPage(goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{item}}, goldenpath.DiscoveryPageInput{Limit: 25, AfterID: "dec_one"}, scope); e == nil {
		t.Fatal("non-exclusive boundary accepted")
	}
}

func TestProjectDecisionsReturnsExactQuestionAndOnlyMinimalFields(t *testing.T) {
	question := "是否采用 <script>alert(1)</script> **方案**？ " + strings.Repeat("长问题", 500)
	reader := &fakeDiscoveryReader{page: goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{
		{Ref: entityref.Ref{Type: "decision", ID: "dec_a"}, Title: question, Status: "proposed"},
		{Ref: entityref.Ref{Type: "decision", ID: "dec_b"}, Title: "已确认的问题", Status: "accepted"},
	}, NextID: "dec_b"}}
	sessions := validMessagingSessions()
	w := httptest.NewRecorder()
	discoveryHandlerForTest(t, sessions, reader).ServeHTTP(w, messagingRequest("GET", "/api/v1/workspaces/wrk_main/projects/prj_main/decisions?limit=2", ""))
	if w.Code != 200 || reader.kind != "decision" || reader.project != "prj_main" || reader.input.Limit != 2 || sessions.verifyCalls != 0 {
		t.Fatal(w.Code, w.Body.String(), reader, sessions)
	}
	var payload struct {
		Data struct {
			Items      []map[string]json.RawMessage `json:"items"`
			NextCursor string                       `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Items) != 2 {
		t.Fatal(payload)
	}
	for _, item := range payload.Data.Items {
		if len(item) != 3 || item["ref"] == nil || item["title"] == nil || item["status"] == nil {
			t.Fatal(item)
		}
		var ref map[string]string
		if err := json.Unmarshal(item["ref"], &ref); err != nil || len(ref) != 2 || ref["type"] != "decision" {
			t.Fatal(ref, err)
		}
	}
	var title string
	if err := json.Unmarshal(payload.Data.Items[0]["title"], &title); err != nil || title != question {
		t.Fatal(title, err)
	}
	input, err := parseDiscoveryQuery("after="+payload.Data.NextCursor, discoveryCursor{Version: 1, WorkspaceID: "wrk_main", Kind: "decision", ProjectID: "prj_main"})
	if err != nil || input.AfterID != "dec_b" || input.Limit != 25 {
		t.Fatal(input, err)
	}
	reader.page = goldenpath.DiscoveryPage{}
	w = httptest.NewRecorder()
	discoveryHandlerForTest(t, sessions, reader).ServeHTTP(w, messagingRequest("GET", "/api/v1/workspaces/wrk_main/projects/prj_main/decisions?after="+payload.Data.NextCursor, ""))
	if w.Code != 200 || w.Body.String() != "{\"data\":{\"items\":[],\"next_cursor\":null}}\n" || reader.calls != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestProjectDecisionsRejectsInvalidPaginationProjection(t *testing.T) {
	one := goldenpath.DiscoveryItem{Ref: entityref.Ref{Type: "decision", ID: "dec_one"}, Title: "Visible question", Status: "proposed"}
	two := one
	two.Ref.ID = "dec_two"
	for _, page := range []goldenpath.DiscoveryPage{
		{Items: []goldenpath.DiscoveryItem{one, two}},
		{Items: []goldenpath.DiscoveryItem{one}, NextID: "dec_two"},
		{NextID: "dec_hidden"},
	} {
		reader := &fakeDiscoveryReader{page: page}
		w := httptest.NewRecorder()
		discoveryHandlerForTest(t, validMessagingSessions(), reader).ServeHTTP(w, messagingRequest("GET", "/api/v1/workspaces/wrk_main/projects/prj_main/decisions?limit=1", ""))
		if w.Code != 500 || strings.Contains(w.Body.String(), one.Title) || strings.Contains(w.Body.String(), "dec_") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// A next cursor must describe a full returned page, even when its ID exists.
	if _, err := publicDiscoveryPage(goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{one}, NextID: "dec_one"}, goldenpath.DiscoveryPageInput{Limit: 2}, discoveryCursor{Kind: "decision"}); err == nil {
		t.Fatal("short page with next cursor accepted")
	}
}
