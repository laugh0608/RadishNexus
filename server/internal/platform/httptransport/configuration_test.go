package httptransport

import (
	"context"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testConfiguration struct {
	input      goldenpath.ConfigurationInput
	invocation goldenpath.Invocation
	calls      int
	result     goldenpath.ConfigurationResult
	err        error
	page       goldenpath.ConfigurationPage
	query      goldenpath.ConfigurationQuery
}

func (s *testConfiguration) Configure(_ context.Context, i goldenpath.Invocation, input goldenpath.ConfigurationInput) (goldenpath.ConfigurationResult, error) {
	s.calls++
	s.input = input
	s.invocation = i
	return s.result, s.err
}
func (s *testConfiguration) ReadConfiguration(context.Context, authz.Principal, string, string) (goldenpath.ConfigurationObject, error) {
	return s.result.Object, s.err
}
func (s *testConfiguration) ListConfiguration(_ context.Context, _ authz.Principal, q goldenpath.ConfigurationQuery) (goldenpath.ConfigurationPage, error) {
	s.query = q
	return s.page, s.err
}
func configurationTestHandler(t *testing.T, app *testConfiguration) http.Handler {
	t.Helper()
	policy, e := NewBrowserSessionPolicy("https://nexus.example.test")
	if e != nil {
		t.Fatal(e)
	}
	proxy, e := NewTrustedProxyPolicy("10.0.0.0/8")
	if e != nil {
		t.Fatal(e)
	}
	return WithRequestID(NewConfigurationHandler(validMessagingSessions(), app, policy, proxy))
}
func TestConfigurationStrictCommandsAndAuthority(t *testing.T) {
	app := &testConfiguration{result: goldenpath.ConfigurationResult{Created: true, Object: goldenpath.ConfigurationObject{ID: "chn_created", Kind: "channel", Name: "Channel", ProjectID: "prj_parent", Visibility: "project", Status: "active"}}}
	h := configurationTestHandler(t, app)
	body := `{"client_operation_id":"channel-1","name":"Channel","visibility":"project","member_user_ids":[]}`
	path := "/api/v1/workspaces/wrk_main/projects/prj_parent/channels"
	response := httptest.NewRecorder()
	h.ServeHTTP(response, messagingWriteRequest("POST", path, body))
	if response.Code != 201 || app.calls != 1 || app.input.Kind != "channel.create" || app.input.ScopeID != "prj_parent" || app.invocation.Principal.ID != "usr_reader" || app.invocation.SourceKind != "web" {
		t.Fatal(response.Code, response.Body.String(), app)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(response.Body.String(), "channel-1") {
		t.Fatal("unsafe response")
	}
	for _, invalid := range []string{
		`{"client_operation_id":"a","name":"Channel","visibility":"project"}`,
		`{"client_operation_id":"a","name":"Channel","visibility":"project","member_user_ids":[],"actor":"usr_owner"}`,
		`{"client_operation_id":"a","name":"Channel","name":"Other","visibility":"project","member_user_ids":[]}`,
		`{"client_operation_id":"a","name":null,"visibility":"project","member_user_ids":[]}`,
		`null`, body + body,
	} {
		response = httptest.NewRecorder()
		h.ServeHTTP(response, messagingWriteRequest("POST", path, invalid))
		if response.Code != 400 || app.calls != 1 {
			t.Fatal(response.Code, response.Body.String(), invalid)
		}
	}
	response = httptest.NewRecorder()
	request := messagingWriteRequest("POST", path, body)
	request.Header.Del("X-CSRF-Token")
	h.ServeHTTP(response, request)
	if response.Code != 403 || app.calls != 1 {
		t.Fatal("missing CSRF accepted", response.Code)
	}
	response = httptest.NewRecorder()
	h.ServeHTTP(response, messagingWriteRequest("POST", path+"?role=admin", body))
	if response.Code != 400 || app.calls != 1 {
		t.Fatal("query accepted")
	}
	response = httptest.NewRecorder()
	h.ServeHTTP(response, messagingRequest("PATCH", path, body))
	if response.Code != 405 || response.Header().Get("Allow") != "POST" {
		t.Fatal("method boundary", response.Code)
	}
}
func TestConfigurationProjectionDriftFailsClosed(t *testing.T) {
	app := &testConfiguration{result: goldenpath.ConfigurationResult{Object: goldenpath.ConfigurationObject{ID: "prj_wrong", Kind: "project", Name: "Secret", Visibility: "restricted", Status: "active", Key: "test"}}}
	h := configurationTestHandler(t, app)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, messagingRequest("GET", "/api/v1/workspaces/wrk_main/projects/prj_expected/configuration", ""))
	if response.Code != 500 || strings.Contains(response.Body.String(), "Secret") {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestCreatedChannelCannotDriftToAnotherProject(t *testing.T) {
	app := &testConfiguration{result: goldenpath.ConfigurationResult{Created: true, Object: goldenpath.ConfigurationObject{ID: "chn_created", Kind: "channel", Name: "Secret", ProjectID: "prj_wrong", Visibility: "project", Status: "active"}}}
	response := httptest.NewRecorder()
	configurationTestHandler(t, app).ServeHTTP(response, messagingWriteRequest("POST", "/api/v1/workspaces/wrk_main/projects/prj_expected/channels", `{"client_operation_id":"create","name":"Channel","visibility":"project","member_user_ids":[]}`))
	if response.Code != 500 || strings.Contains(response.Body.String(), "Secret") {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestLegacyMemberMutationsCannotBypassPlans(t *testing.T) {
	app := &testConfiguration{}
	h := configurationTestHandler(t, app)
	for _, scope := range []string{"projects/prj_main", "channels/chn_main"} {
		for _, method := range []string{"PUT", "DELETE"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, messagingWriteRequest(method, "/api/v1/workspaces/wrk_main/"+scope+"/members/usr_target", `{"client_operation_id":"old"}`))
			if w.Code != 404 || app.calls != 0 {
				t.Fatal("legacy management bypass", w.Code)
			}
		}
	}
}
