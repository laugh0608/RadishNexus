package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type fakeWorkspaceSessionResolver struct {
	result      authn.VerifiedUser
	err         error
	token       string
	workspaceID string
	calls       int
}

func (resolver *fakeWorkspaceSessionResolver) ResolveWorkspace(
	_ context.Context,
	token string,
	workspaceID string,
) (authn.VerifiedUser, error) {
	resolver.calls++
	resolver.token = token
	resolver.workspaceID = workspaceID
	return resolver.result, resolver.err
}

type fakeNexusViewReader struct {
	view      goldenpath.NexusView
	err       error
	principal authz.Principal
	target    entityref.Ref
	calls     int
}

func (reader *fakeNexusViewReader) GetNexusView(
	_ context.Context,
	principal authz.Principal,
	target entityref.Ref,
) (goldenpath.NexusView, error) {
	reader.calls++
	reader.principal = principal
	reader.target = target
	return reader.view, reader.err
}

func TestDeploymentNexusViewHandlerUsesVerifiedWorkspacePrincipalAndSafeDTO(t *testing.T) {
	t.Parallel()
	resolver := &fakeWorkspaceSessionResolver{
		result: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"},
	}
	target := entityref.Ref{Type: "deployment", ID: "dpl_release_1"}
	reader := &fakeNexusViewReader{view: testDeploymentNexusView(target, true)}
	handler := testDeploymentNexusViewHandler(t, resolver, reader)
	request := deploymentNexusViewRequest(http.MethodGet, "wrk_main", target.ID)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("Vary") != "Cookie" {
		t.Fatalf("response = status %d, headers %#v", response.Code, response.Header())
	}
	if resolver.calls != 1 || resolver.token != transportToken(1) || resolver.workspaceID != "wrk_main" {
		t.Fatalf("ResolveWorkspace() = calls %d, token %q, Workspace %q", resolver.calls, resolver.token, resolver.workspaceID)
	}
	wantPrincipal := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_reader", WorkspaceID: "wrk_main"}
	if reader.calls != 1 || reader.principal != wantPrincipal || reader.target != target {
		t.Fatalf("GetNexusView() = calls %d, principal %#v, target %#v", reader.calls, reader.principal, reader.target)
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	encoded := response.Body.String()
	for _, required := range []string{
		`"type":"deployment"`, `"id":"dpl_release_1"`, `"status":"succeeded"`,
		`"type":"environment"`, `"id":"env_staging"`, `"type":"ci-run"`,
		`"relation_type":"deploys"`, `"activity_type":"deployment.recorded"`,
		`"kind":"user"`, `"id":"usr_contributor"`,
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("response body is missing %q: %s", required, encoded)
		}
	}
	for _, forbidden := range []string{"governing_project", "updated_at", "projection_version", "safe_facts", "authorization", "jenkins", "secret"} {
		if strings.Contains(strings.ToLower(encoded), forbidden) {
			t.Fatalf("response body contains forbidden field %q: %s", forbidden, encoded)
		}
	}
}

func TestDeploymentNexusViewHandlerPreservesNullableStartTime(t *testing.T) {
	t.Parallel()
	target := entityref.Ref{Type: "deployment", ID: "dpl_release_1"}
	view := testDeploymentNexusView(target, false)
	body, err := marshalDeploymentNexusView(target, view)
	if err != nil {
		t.Fatalf("marshalDeploymentNexusView() error = %v", err)
	}
	if !strings.Contains(string(body), `"started_at":null`) {
		t.Fatalf("body = %s", body)
	}
}

func TestDeploymentNexusViewHandlerFailsClosedOnProjectionDrift(t *testing.T) {
	t.Parallel()
	target := entityref.Ref{Type: "deployment", ID: "dpl_release_1"}
	tests := []struct {
		name      string
		forbidden string
		mutate    func(*goldenpath.NexusView)
	}{
		{
			name:      "unexpected Current field",
			forbidden: "prj_must_not_cross_transport",
			mutate: func(view *goldenpath.NexusView) {
				view.Current.GoverningProjectID = "prj_must_not_cross_transport"
			},
		},
		{
			name:      "relation title differs from Current",
			forbidden: "drifted relation title",
			mutate: func(view *goldenpath.NexusView) {
				view.Relations[0].Title = "drifted relation title"
			},
		},
		{
			name:      "Timeline subject title differs from Current",
			forbidden: "drifted subject title",
			mutate: func(view *goldenpath.NexusView) {
				view.Timeline[0].Subjects[0].Title = "drifted subject title"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view := testDeploymentNexusView(target, true)
			test.mutate(&view)
			resolver := &fakeWorkspaceSessionResolver{result: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"}}
			reader := &fakeNexusViewReader{view: view}
			response := httptest.NewRecorder()
			testDeploymentNexusViewHandler(t, resolver, reader).ServeHTTP(
				response,
				deploymentNexusViewRequest(http.MethodGet, "wrk_main", target.ID),
			)
			if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal"`) ||
				strings.Contains(response.Body.String(), test.forbidden) {
				t.Fatalf("response = status %d, body %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestDeploymentNexusViewHandlerRejectsUnauthenticatedInvalidAndUnreadableRequests(t *testing.T) {
	t.Parallel()
	target := entityref.Ref{Type: "deployment", ID: "dpl_release_1"}
	tests := []struct {
		name         string
		workspaceID  string
		deploymentID string
		cookie       bool
		resolverErr  error
		readerErr    error
		wantStatus   int
		wantCode     string
	}{
		{name: "missing session", workspaceID: "wrk_main", deploymentID: target.ID, wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated"},
		{name: "invalid Workspace", workspaceID: "invalid", deploymentID: target.ID, cookie: true, wantStatus: http.StatusBadRequest, wantCode: "invalid"},
		{name: "invalid Deployment", workspaceID: "wrk_main", deploymentID: "not-a-deployment", cookie: true, wantStatus: http.StatusBadRequest, wantCode: "invalid"},
		{name: "not a Workspace member", workspaceID: "wrk_other", deploymentID: target.ID, cookie: true, resolverErr: authz.ErrForbidden, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "Deployment unreadable", workspaceID: "wrk_main", deploymentID: target.ID, cookie: true, readerErr: authz.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeWorkspaceSessionResolver{
				result: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: test.workspaceID},
				err:    test.resolverErr,
			}
			reader := &fakeNexusViewReader{view: testDeploymentNexusView(target, true), err: test.readerErr}
			request := deploymentNexusViewRequest(http.MethodGet, test.workspaceID, test.deploymentID)
			if !test.cookie {
				request.Header.Del("Cookie")
			}
			response := httptest.NewRecorder()
			testDeploymentNexusViewHandler(t, resolver, reader).ServeHTTP(response, request)
			if response.Code != test.wantStatus || response.Header().Get("Cache-Control") != "private, no-store" ||
				!strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("response = status %d, headers %#v, body %q", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}

func TestDeploymentNexusViewHandlerUsesVersionedMethodAndPathErrors(t *testing.T) {
	t.Parallel()
	resolver := &fakeWorkspaceSessionResolver{}
	reader := &fakeNexusViewReader{}
	handler := testDeploymentNexusViewHandler(t, resolver, reader)
	tests := []struct {
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{method: http.MethodHead, path: "/api/v1/workspaces/wrk_main/deployments/dpl_release_1/nexus-view", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{method: http.MethodPost, path: "/api/v1/workspaces/wrk_main/deployments/dpl_release_1/nexus-view", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{method: http.MethodGet, path: "/api/v1/workspaces/wrk_main/deployments/dpl_release_1/unknown", wantStatus: http.StatusNotFound, wantCode: "not_found"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, "https://nexus.example.test"+test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
			t.Fatalf("%s %s = status %d, body %q", test.method, test.path, response.Code, response.Body.String())
		}
		if test.wantStatus == http.StatusMethodNotAllowed && response.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("Allow = %q", response.Header().Get("Allow"))
		}
	}
}

func TestDeploymentNexusViewHandlerRequiresSecureTransport(t *testing.T) {
	t.Parallel()
	resolver := &fakeWorkspaceSessionResolver{}
	reader := &fakeNexusViewReader{}
	request := httptest.NewRequest(
		http.MethodGet,
		"http://nexus.example.test/api/v1/workspaces/wrk_main/deployments/dpl_release_1/nexus-view",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: transportToken(1)})
	response := httptest.NewRecorder()
	testDeploymentNexusViewHandler(t, resolver, reader).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"secure_transport_required"`) {
		t.Fatalf("response = status %d, body %q", response.Code, response.Body.String())
	}
}

func testDeploymentNexusViewHandler(
	t *testing.T,
	resolver WorkspaceSessionResolver,
	reader NexusViewReader,
) http.Handler {
	t.Helper()
	session, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	proxy, err := NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	return WithRequestID(NewDeploymentNexusViewHandler(resolver, reader, session, proxy))
}

func deploymentNexusViewRequest(method string, workspaceID string, deploymentID string) *http.Request {
	request := httptest.NewRequest(
		method,
		"https://nexus.example.test/api/v1/workspaces/"+workspaceID+"/deployments/"+deploymentID+"/nexus-view",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: transportToken(1)})
	return request
}

func testDeploymentNexusView(target entityref.Ref, withStart bool) goldenpath.NexusView {
	startedAt := time.Date(2026, 8, 30, 8, 0, 0, 123000000, time.UTC)
	completedAt := time.Date(2026, 8, 30, 8, 5, 0, 456000000, time.UTC)
	recordedAt := time.Date(2026, 8, 30, 8, 5, 1, 789000000, time.UTC)
	environment := goldenpath.SubjectProjection{
		State: goldenpath.ProjectionVisible,
		Ref:   entityref.Ref{Type: "environment", ID: "env_staging"},
		Title: "Staging",
	}
	ciRun := goldenpath.SubjectProjection{
		State: goldenpath.ProjectionVisible,
		Ref:   entityref.Ref{Type: "ci-run", ID: "cir_source_1"},
		Title: "CI Run",
	}
	view := goldenpath.NexusView{
		Current: goldenpath.CurrentProjection{
			Ref:         target,
			Status:      "succeeded",
			UpdatedAt:   recordedAt,
			Environment: &environment,
			CIRun:       &ciRun,
			CompletedAt: &completedAt,
			RecordedAt:  &recordedAt,
		},
		Relations: []goldenpath.RelationProjection{{
			State:        goldenpath.ProjectionVisible,
			RelationType: "deploys",
			Target:       ciRun.Ref,
			Title:        ciRun.Title,
		}},
		Timeline: []goldenpath.TimelineItem{{
			EventID:           "evt_deployment_recorded_1",
			ActivityType:      "deployment.recorded",
			Actor:             goldenpath.ActorRef{Kind: "user", ID: "usr_contributor"},
			OccurredAt:        completedAt,
			Subjects:          []goldenpath.SubjectProjection{environment, ciRun},
			ProjectionVersion: goldenpath.ActivityProjectionVersion,
			SafeFacts:         map[string]string{"status": "succeeded"},
		}},
	}
	if withStart {
		view.Current.StartedAt = &startedAt
	}
	return view
}
