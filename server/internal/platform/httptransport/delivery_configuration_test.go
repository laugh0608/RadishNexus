package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
)

func TestAuthorizationHTTPStrictExpectedStateAndConfirmation(t *testing.T) {
	app := &testConfiguration{result: goldenpath.ConfigurationResult{UserID: "usr_target"}}
	h := configurationTestHandler(t, app)
	path := "/api/v1/workspaces/wrk_main/environments/env_stage/deployment-authorizations/usr_target"
	good := `{"client_operation_id":"grant","expected_authorization":{"id":"dpa_old","status":"revoked"},"confirmed":true}`
	r := httptest.NewRecorder()
	h.ServeHTTP(r, messagingWriteRequest("PUT", path, good))
	if r.Code != 200 || app.input.Kind != "environment.authorization.grant" || app.input.ScopeID != "env_stage" || app.input.Delivery.ExpectedAuthorization.ID != "dpa_old" {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, body := range []string{
		strings.Replace(good, `"confirmed":true`, `"confirmed":false`, 1),
		strings.Replace(good, `"confirmed":true`, `"confirmed":null`, 1),
		strings.Replace(good, `"id":"dpa_old"`, `"id":"dpa_old","id":"dpa_new"`, 1),
		strings.Replace(good, `"status":"revoked"`, `"status":"revoked","actor":"usr_owner"`, 1),
		strings.Replace(good, `"status":"revoked"`, `"status":null`, 1),
		strings.Replace(good, `"expected_authorization":{"id":"dpa_old","status":"revoked"},`, "", 1),
		strings.Replace(good, `"confirmed":true`, `"confirmed":true,"granted_by":"usr_owner"`, 1),
	} {
		r = httptest.NewRecorder()
		h.ServeHTTP(r, messagingWriteRequest("PUT", path, body))
		if r.Code != 400 || app.calls != 1 {
			t.Fatal("invalid reached application", r.Code, body)
		}
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingWriteRequest("DELETE", path, `{"client_operation_id":"revoke","expected_authorization":null,"confirmed":true}`))
	if r.Code != 200 || app.input.Kind != "environment.authorization.revoke" || app.input.Delivery.ExpectedAuthorization != nil {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		func(r *http.Request) { r.Header.Set("Origin", "https://other.example.test") },
	} {
		req := messagingWriteRequest("PUT", path, good)
		mutate(req)
		r = httptest.NewRecorder()
		h.ServeHTTP(r, req)
		if r.Code != 403 || app.calls != 2 {
			t.Fatal("security boundary", r.Code)
		}
	}
}

func TestDeliveryDiscoveryAndOwnerAuthorizationProjection(t *testing.T) {
	app := &testConfiguration{page: goldenpath.ConfigurationPage{Objects: []goldenpath.ConfigurationObject{{ID: "env_a", Kind: "environment", Key: "STAGE", Name: "Staging", Status: "active", Classification: "staging", OwnerTeamID: "tem_main"}}, NextID: "env_a"}}
	h := configurationTestHandler(t, app)
	path := "/api/v1/workspaces/wrk_main/environments"
	r := httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path+"?limit=1", ""))
	if r.Code != 200 || strings.Contains(r.Body.String(), "authorization") || strings.Contains(r.Body.String(), "capabilities") {
		t.Fatal(r.Code, r.Body.String())
	}
	var body struct {
		Data struct {
			Cursor string `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	app.page = goldenpath.ConfigurationPage{}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path+"?after="+body.Data.Cursor, ""))
	if r.Code != 200 || app.query.AfterID != "env_a" {
		t.Fatal("environment cursor", r.Code)
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", "/api/v1/workspaces/wrk_main/components?after="+body.Data.Cursor, ""))
	if r.Code != 400 {
		t.Fatal("cross-kind cursor accepted", r.Code)
	}
	app.page = goldenpath.ConfigurationPage{Members: []goldenpath.ConfigurationMember{{ID: "usr_target", Name: "Member", Eligible: true}}}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path+"/env_a/deployment-authorizations/usr_target", ""))
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"authorization":null`) || app.query.UserID != "usr_target" {
		t.Fatal(r.Code, r.Body.String())
	}
	app.page.Members[0].Eligible = false
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path+"/env_a/deployment-authorizations/usr_target", ""))
	if r.Code != 500 {
		t.Fatal("invalid candidate projection accepted", r.Code)
	}
}

func TestDeliveryConfigurationRoutesRegisteredInProductionMux(t *testing.T) {
	app := &testConfiguration{result: goldenpath.ConfigurationResult{Object: goldenpath.ConfigurationObject{ID: "cmp_a", Kind: "component", Name: "Service", Key: "service", Type: "service", Status: "active", OwnerTeamID: "tem_main"}, Created: true}}
	mux := http.NewServeMux()
	RegisterConfigurationRoutes(mux, http.NotFoundHandler(), configurationTestHandler(t, app))
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, messagingWriteRequest("POST", "/api/v1/workspaces/wrk_main/components", `{"client_operation_id":"create","key":"service","name":"Service","type":"service","owner_team_id":"tem_main"}`))
	if r.Code != 201 || app.input.Kind != "component.create" || app.input.Delivery.Type != "service" {
		t.Fatal(r.Code, r.Body.String())
	}
}
