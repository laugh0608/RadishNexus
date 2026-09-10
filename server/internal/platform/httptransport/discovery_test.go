package httptransport

import (
	"context"
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

type fakeDiscoveryReader struct {
	page      goldenpath.DiscoveryPage
	err       error
	calls     int
	principal authz.Principal
	project   string
	input     goldenpath.DiscoveryPageInput
}

func (reader *fakeDiscoveryReader) ListProjects(_ context.Context, principal authz.Principal, input goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error) {
	reader.calls++
	reader.principal = principal
	reader.input = input
	return reader.page, reader.err
}
func (reader *fakeDiscoveryReader) ListProjectChannels(ctx context.Context, principal authz.Principal, project string, input goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error) {
	reader.project = project
	return reader.ListProjects(ctx, principal, input)
}

func discoveryHandlerForTest(t *testing.T, sessions WorkspaceSessionResolver, reader DiscoveryReader) http.Handler {
	t.Helper()
	session, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	return WithRequestID(NewDiscoveryHandler(sessions, reader, session, proxy))
}

func TestDiscoveryRoutesReturnOnlySafeItemsAndScopedPagination(t *testing.T) {
	for _, kind := range []string{"project", "channel"} {
		t.Run(kind, func(t *testing.T) {
			project := ""
			path := "/api/v1/workspaces/wrk_main/projects"
			if kind == "channel" {
				project = "prj_parent"
				path += "/prj_parent/channels"
			}
			id := discoveryPrefix(kind) + "visible"
			reader := &fakeDiscoveryReader{page: goldenpath.DiscoveryPage{Items: []goldenpath.DiscoveryItem{{Ref: entityref.Ref{Type: kind, ID: id}, Title: "Visible title", Status: "archived"}}, NextID: id}}
			sessions := validMessagingSessions()
			handler := discoveryHandlerForTest(t, sessions, reader)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, messagingRequest(http.MethodGet, path+"?limit=1", ""))
			if response.Code != 200 || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("Vary") != "Cookie" {
				t.Fatal(response.Code, response.Body.String(), response.Header())
			}
			var payload struct {
				Data discoveryPageDTO `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Data.Items) != 1 || payload.Data.NextCursor == nil || payload.Data.Items[0].Ref.ID != id || reader.project != project || reader.principal != (authz.Principal{Kind: authz.PrincipalUser, ID: "usr_reader", WorkspaceID: "wrk_main"}) || sessions.verifyCalls != 0 {
				t.Fatal(payload, reader, sessions)
			}
			input, err := parseDiscoveryQuery("limit=1&after="+*payload.Data.NextCursor, discoveryCursor{Version: 1, WorkspaceID: "wrk_main", Kind: kind, ProjectID: project})
			if err != nil || input.AfterID != id {
				t.Fatal(input, err)
			}
			for _, forbidden := range []string{"membership", "owner", "total", "summary", "password", "permission"} {
				if strings.Contains(response.Body.String(), forbidden) {
					t.Fatal("private metadata leaked")
				}
			}
			reader.page = goldenpath.DiscoveryPage{}
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, messagingRequest(http.MethodGet, path+"?after="+*payload.Data.NextCursor, ""))
			if response.Code != 200 || !strings.Contains(response.Body.String(), `"items":[]`) {
				t.Fatal(response.Body.String())
			}
		})
	}
}

func TestDiscoveryRejectsAmbiguousQueriesAndCrossScopeCursors(t *testing.T) {
	scope := discoveryCursor{Version: 1, WorkspaceID: "wrk_main", Kind: "channel", ProjectID: "prj_parent"}
	queries := []string{"limit=0", "limit=51", "limit=01", "limit=+1", "limit=1&limit=2", "limit=", "after=", "after=bad", "after=a&after=b", "unknown=1", "limit=%zz", "limit=1;after=x"}
	encode := func(cursor discoveryCursor) string {
		body, _ := json.Marshal(cursor)
		return base64.RawURLEncoding.EncodeToString(body)
	}
	for _, mutate := range []func(*discoveryCursor){func(c *discoveryCursor) { c.Version = 2 }, func(c *discoveryCursor) { c.WorkspaceID = "wrk_other" }, func(c *discoveryCursor) { c.ProjectID = "prj_other" }, func(c *discoveryCursor) { c.Kind = "project" }, func(c *discoveryCursor) { c.AfterID = "prj_wrong" }} {
		cursor := scope
		cursor.AfterID = "chn_last"
		mutate(&cursor)
		queries = append(queries, "after="+encode(cursor))
	}
	queries = append(queries, "after="+base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"v":1,"workspace_id":"wrk_main","kind":"channel","project_id":"prj_parent","after_id":"chn_last"}`)))
	for _, query := range queries {
		reader := &fakeDiscoveryReader{}
		response := httptest.NewRecorder()
		discoveryHandlerForTest(t, validMessagingSessions(), reader).ServeHTTP(response, messagingRequest(http.MethodGet, "/api/v1/workspaces/wrk_main/projects/prj_parent/channels?"+query, ""))
		if response.Code != 400 || reader.calls != 0 {
			t.Fatalf("query %q: %d %s calls=%d", query, response.Code, response.Body.String(), reader.calls)
		}
	}
}

func TestDiscoveryEnforcesTransportAndCurrentMembership(t *testing.T) {
	for _, test := range []struct {
		name       string
		change     func(*http.Request)
		resolveErr error
		status     int
	}{
		{"anonymous", func(r *http.Request) { r.Header.Del("Cookie") }, nil, 401},
		{"wrong host", func(r *http.Request) { r.Host = "other.example.test" }, nil, 400},
		{"insecure untrusted forwarding", func(r *http.Request) {
			r.TLS = nil
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
		}, nil, 400},
		{"expired", func(*http.Request) {}, authn.ErrInvalidSession, 401},
		{"membership revoked", func(*http.Request) {}, authz.ErrForbidden, 404},
		{"wrong method", func(r *http.Request) { r.Method = http.MethodPost }, nil, 405},
		{"head", func(r *http.Request) { r.Method = http.MethodHead }, nil, 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			sessions := validMessagingSessions()
			sessions.resolveErr = test.resolveErr
			reader := &fakeDiscoveryReader{}
			request := messagingRequest(http.MethodGet, "/api/v1/workspaces/wrk_main/projects", "")
			test.change(request)
			response := httptest.NewRecorder()
			discoveryHandlerForTest(t, sessions, reader).ServeHTTP(response, request)
			if response.Code != test.status || reader.calls != 0 {
				t.Fatal(response.Code, response.Body.String(), reader.calls)
			}
		})
	}
	reader := &fakeDiscoveryReader{err: errors.New("private database error")}
	response := httptest.NewRecorder()
	discoveryHandlerForTest(t, validMessagingSessions(), reader).ServeHTTP(response, messagingRequest(http.MethodGet, "/api/v1/workspaces/wrk_main/projects", ""))
	if response.Code != 500 || strings.Contains(response.Body.String(), "private database error") {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestDiscoveryRejectsMalformedApplicationPages(t *testing.T) {
	item := goldenpath.DiscoveryItem{Ref: entityref.Ref{Type: "project", ID: "prj_visible"}, Title: "Visible", Status: "active"}
	for _, page := range []goldenpath.DiscoveryPage{
		{Items: []goldenpath.DiscoveryItem{item, item}},
		{Items: []goldenpath.DiscoveryItem{item}, NextID: "prj_hidden"},
		{NextID: "prj_hidden"},
		{Items: []goldenpath.DiscoveryItem{{Ref: entityref.Ref{Type: "channel", ID: "chn_wrong"}, Title: "Wrong", Status: "active"}}},
	} {
		if _, err := publicDiscoveryPage(page, goldenpath.DiscoveryPageInput{Limit: 2}, discoveryCursor{Version: 1, WorkspaceID: "wrk_main", Kind: "project"}); err == nil {
			t.Fatal("invalid page accepted", page)
		}
	}
}
