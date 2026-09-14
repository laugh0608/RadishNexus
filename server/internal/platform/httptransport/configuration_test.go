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
func (s *testConfiguration) ListConfiguration(context.Context, authz.Principal, goldenpath.ConfigurationQuery) (goldenpath.ConfigurationPage, error) {
	return goldenpath.ConfigurationPage{}, s.err
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
	app := &testConfiguration{result: goldenpath.ConfigurationResult{UserID: "usr_target"}}
	h := configurationTestHandler(t, app)
	body := `{"client_operation_id":"role-1","expected_role":null,"role":"contributor"}`
	path := "/api/v1/workspaces/wrk_main/projects/prj_parent/members/usr_target"
	response := httptest.NewRecorder()
	h.ServeHTTP(response, messagingWriteRequest("PUT", path, body))
	if response.Code != 200 || app.calls != 1 || app.input.Kind != "project.member.set" || app.input.UserID != "usr_target" || app.invocation.Principal.ID != "usr_reader" || app.invocation.SourceKind != "web" || app.input.ExpectedRole != nil {
		t.Fatal(response.Code, response.Body.String(), app)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(response.Body.String(), "role-1") {
		t.Fatal("unsafe response")
	}
	for _, invalid := range []string{
		`{"client_operation_id":"a","role":"viewer"}`,
		`{"client_operation_id":"a","expected_role":null,"role":"viewer","actor":"usr_owner"}`,
		`{"client_operation_id":"a","expected_role":null,"role":"viewer","role":"contributor"}`,
		`{"client_operation_id":"a","expected_role":null,"role":null}`,
		`null`, body + body,
	} {
		response = httptest.NewRecorder()
		h.ServeHTTP(response, messagingWriteRequest("PUT", path, invalid))
		if response.Code != 400 || app.calls != 1 {
			t.Fatal(response.Code, response.Body.String(), invalid)
		}
	}
	response = httptest.NewRecorder()
	request := messagingWriteRequest("PUT", path, body)
	request.Header.Del("X-CSRF-Token")
	h.ServeHTTP(response, request)
	if response.Code != 403 || app.calls != 1 {
		t.Fatal("missing CSRF accepted", response.Code)
	}
	response = httptest.NewRecorder()
	h.ServeHTTP(response, messagingWriteRequest("PUT", path+"?role=admin", body))
	if response.Code != 400 || app.calls != 1 {
		t.Fatal("query accepted")
	}
	response = httptest.NewRecorder()
	h.ServeHTTP(response, messagingRequest("PATCH", path, body))
	if response.Code != 405 || response.Header().Get("Allow") != "PUT, DELETE" {
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
