package httptransport

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
)

func httpTestRepository() goldenpath.ConfigurationObject {
	return goldenpath.ConfigurationObject{ID: "rep_a", Kind: "repository", Name: "Service", Repository: &goldenpath.RepositoryMetadata{Provider: "gitea", ProviderOrigin: "https://git.example.test", ExternalID: "123", WebURL: "https://git.example.test/team/service", DefaultBranch: "main"}}
}

func TestRepositoryRoutesStrictCommandsAndRetryProjection(t *testing.T) {
	app := &testConfiguration{result: goldenpath.ConfigurationResult{Object: httpTestRepository(), Created: true}}
	mux := http.NewServeMux()
	RegisterConfigurationRoutes(mux, http.NotFoundHandler(), configurationTestHandler(t, app))
	create := `{"client_operation_id":"create","name":"Service","provider":"gitea","provider_origin":"https://git.example.test","external_id":"123","web_url":"https://git.example.test/team/service","default_branch":"main"}`
	path := "/api/v1/workspaces/wrk_main/repositories"
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, messagingWriteRequest("POST", path, create))
	if r.Code != 201 || app.input.Kind != "repository.create" || app.input.Repository.ExternalID != "123" || app.input.ScopeID != "wrk_main" || strings.Contains(r.Body.String(), "capabilities") {
		t.Fatal(r.Code, r.Body.String(), app.input)
	}
	if r.Header().Get("Cache-Control") != "private, no-store" || r.Header().Get("Vary") != "Cookie" {
		t.Fatal("cache boundary")
	}
	for _, body := range []string{
		strings.Replace(create, `"name":"Service"`, `"name":"Service","name":"Other"`, 1),
		strings.Replace(create, `"name":"Service"`, `"name":"Service","secret":"not-allowed"`, 1),
		strings.Replace(create, `"external_id":"123"`, `"external_id":null`, 1),
		strings.Replace(create, `"default_branch":"main"`, `"default_branch":5`, 1),
		strings.Replace(create, `,"external_id":"123"`, "", 1),
		create + create,
	} {
		r = httptest.NewRecorder()
		mux.ServeHTTP(r, messagingWriteRequest("POST", path, body))
		if r.Code != 400 || app.calls != 1 {
			t.Fatal("unsafe body reached application", r.Code, body)
		}
	}
	app.result = goldenpath.ConfigurationResult{LinkID: "lnk_a", Created: true}
	path = "/api/v1/workspaces/wrk_main/components/cmp_a/repositories"
	body := `{"client_operation_id":"link","repository_id":"rep_a","confirmed":true}`
	r = httptest.NewRecorder()
	mux.ServeHTTP(r, messagingWriteRequest("POST", path, body))
	if r.Code != 201 || app.input.Kind != "component.repository.link" || app.input.SubjectID() != "rep_a" || !strings.Contains(r.Body.String(), `"applied":true`) {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, req := range []*http.Request{
		messagingWriteRequest("POST", path, strings.Replace(body, "true", "false", 1)),
		messagingWriteRequest("POST", path+"?x=y", body),
		messagingWriteRequest("POST", path, strings.Replace(body, `,"confirmed":true`, "", 1)),
	} {
		r = httptest.NewRecorder()
		mux.ServeHTTP(r, req)
		if r.Code != 400 || app.calls != 2 {
			t.Fatal("invalid relation body", r.Code, r.Body.String())
		}
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://other.example.test") }} {
		req := messagingWriteRequest("POST", path, body)
		mutate(req)
		r = httptest.NewRecorder()
		mux.ServeHTTP(r, req)
		if r.Code != 403 || app.calls != 2 {
			t.Fatal("write security boundary", r.Code)
		}
	}
	app.result.Created = false
	path = "/api/v1/workspaces/wrk_main/components/cmp_a/repository-links/lnk_a"
	r = httptest.NewRecorder()
	mux.ServeHTTP(r, messagingWriteRequest("DELETE", path, `{"client_operation_id":"unlink","confirmed":true}`))
	if r.Code != 200 || app.input.Kind != "component.repository.unlink" || app.input.Repository.LinkID != "lnk_a" {
		t.Fatal(r.Code, r.Body.String())
	}
	app.result.LinkID = "lnk_wrong"
	r = httptest.NewRecorder()
	mux.ServeHTTP(r, messagingWriteRequest("DELETE", path, `{"client_operation_id":"unlink","confirmed":true}`))
	if r.Code != 500 || strings.Contains(r.Body.String(), "lnk_wrong") {
		t.Fatal("wrong relation projection accepted", r.Code)
	}
	r = httptest.NewRecorder()
	mux.ServeHTTP(r, messagingRequest("GET", path, ""))
	if r.Code != 405 || r.Header().Get("Allow") != "DELETE" {
		t.Fatal("method boundary", r.Code)
	}
}

func TestRepositoryDiscoveryScopesAndProjectionSafety(t *testing.T) {
	app := &testConfiguration{page: goldenpath.ConfigurationPage{Links: []goldenpath.RepositoryLink{{ID: "lnk_a", Target: httpTestRepository(), CanUnlink: true}}, NextID: "rep_a"}}
	h := configurationTestHandler(t, app)
	path := "/api/v1/workspaces/wrk_main/components/cmp_a/repositories"
	r := httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path+"?limit=1", ""))
	if r.Code != 200 || app.query.Kind != "component-repositories" || app.query.ScopeID != "cmp_a" {
		t.Fatal(r.Code, r.Body.String())
	}
	var payload struct {
		Data struct {
			Cursor string `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	app.page = goldenpath.ConfigurationPage{}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path+"?after="+payload.Data.Cursor, ""))
	if r.Code != 200 || app.query.AfterID != "rep_a" {
		t.Fatal("valid cursor", r.Code)
	}
	for _, other := range []string{"/api/v1/workspaces/wrk_main/repositories", "/api/v1/workspaces/wrk_main/components/cmp_b/repositories", "/api/v1/workspaces/wrk_main/repositories/rep_a/components"} {
		r = httptest.NewRecorder()
		h.ServeHTTP(r, messagingRequest("GET", other+"?after="+payload.Data.Cursor, ""))
		if r.Code == 200 {
			t.Fatal("cross-scope cursor accepted", other)
		}
	}
	encoded, err := json.Marshal(discoveryCursor{Version: 1, WorkspaceID: "wrk_other", Kind: "component-repositories", ProjectID: "cmp_a", AfterID: "rep_a"})
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path+"?after="+base64.RawURLEncoding.EncodeToString(encoded), ""))
	if r.Code != 400 {
		t.Fatal("cross-workspace cursor accepted", r.Code)
	}
	app.result.Object = httpTestRepository()
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", "/api/v1/workspaces/wrk_main/repositories/rep_a/configuration", ""))
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"capabilities":{}`) {
		t.Fatal(r.Code, r.Body.String())
	}
	app.result.Object.Repository.WebURL = "javascript:alert(1)"
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", "/api/v1/workspaces/wrk_main/repositories/rep_a/configuration", ""))
	if r.Code != 500 || strings.Contains(r.Body.String(), "javascript") {
		t.Fatal("unsafe URL escaped projection", r.Code)
	}
	app.result.Object = httpTestRepository()
	app.result.Object.ID = "rep_wrong"
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", "/api/v1/workspaces/wrk_main/repositories/rep_a/configuration", ""))
	if r.Code != 500 {
		t.Fatal("wrong repository identity", r.Code)
	}
}
