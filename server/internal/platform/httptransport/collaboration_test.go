package httptransport

import (
	"context"
	"encoding/json"
	"errors"
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

type fakeCollaborationApplication struct {
	view             goldenpath.NexusView
	decisionResult   goldenpath.CreateDecisionResult
	acceptanceResult goldenpath.AcceptDecisionResult
	ticketResult     goldenpath.CreateTicketResult
	viewErr          error
	decisionErr      error
	acceptanceErr    error
	ticketErr        error
	viewCalls        int
	decisionCalls    int
	acceptanceCalls  int
	ticketCalls      int
	principal        authz.Principal
	target           entityref.Ref
	invocation       goldenpath.Invocation
	decisionInput    goldenpath.CreateDecisionInput
	acceptanceInput  goldenpath.AcceptDecisionInput
	ticketInput      goldenpath.CreateTicketInput
}

func (application *fakeCollaborationApplication) GetNexusView(
	_ context.Context,
	principal authz.Principal,
	target entityref.Ref,
) (goldenpath.NexusView, error) {
	application.viewCalls++
	application.principal = principal
	application.target = target
	return application.view, application.viewErr
}

func (application *fakeCollaborationApplication) CreateDecisionFromThread(
	_ context.Context,
	invocation goldenpath.Invocation,
	input goldenpath.CreateDecisionInput,
) (goldenpath.CreateDecisionResult, error) {
	application.decisionCalls++
	application.invocation = invocation
	application.decisionInput = input
	return application.decisionResult, application.decisionErr
}

func (application *fakeCollaborationApplication) AcceptDecision(
	_ context.Context,
	invocation goldenpath.Invocation,
	input goldenpath.AcceptDecisionInput,
) (goldenpath.AcceptDecisionResult, error) {
	application.acceptanceCalls++
	application.invocation = invocation
	application.acceptanceInput = input
	return application.acceptanceResult, application.acceptanceErr
}

func (application *fakeCollaborationApplication) CreateTicketFromDecision(
	_ context.Context,
	invocation goldenpath.Invocation,
	input goldenpath.CreateTicketInput,
) (goldenpath.CreateTicketResult, error) {
	application.ticketCalls++
	application.invocation = invocation
	application.ticketInput = input
	return application.ticketResult, application.ticketErr
}

func TestCollaborationHandlerReadsMessagingOriginThreadWithoutMessageBody(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 1, 2, 3, 0, time.UTC)
	threadRef := entityref.Ref{Type: "thread", ID: "thr_source"}
	application := &fakeCollaborationApplication{view: goldenpath.NexusView{
		Current: goldenpath.CurrentProjection{
			Ref: threadRef, GoverningProjectID: "prj_main", Title: "Choose auth boundary",
			Visibility: "restricted", CreatedBy: goldenpath.ActorRef{Kind: "user", ID: "usr_reader"},
			CreatedAt: now, UpdatedAt: now,
			OriginChannel: &goldenpath.SubjectProjection{
				State: goldenpath.ProjectionVisible,
				Ref:   entityref.Ref{Type: "channel", ID: "chn_main"},
				Title: "Project Channel",
			},
		},
		Relations: []goldenpath.RelationProjection{{
			State: goldenpath.ProjectionVisible, RelationType: "started-from",
			Target: entityref.Ref{Type: "message", ID: "msg_source"}, Title: "Message",
		}},
	}}
	request := messagingRequest(
		http.MethodGet,
		"/api/v1/workspaces/wrk_main/threads/thr_source/nexus-view",
		"",
	)
	response := httptest.NewRecorder()
	testCollaborationHandler(t, validMessagingSessions(), application).ServeHTTP(response, request)

	if response.Code != http.StatusOK || application.viewCalls != 1 ||
		application.target != threadRef || application.principal.ID != "usr_reader" {
		t.Fatalf("response = %d %q, application %#v", response.Code, response.Body.String(), application)
	}
	for _, required := range []string{
		`"type":"thread","id":"thr_source"`,
		`"origin_channel":{"ref":{"type":"channel","id":"chn_main"},"title":"Project Channel"}`,
		`"relation_type":"started-from"`,
		`"type":"message","id":"msg_source"`,
	} {
		if !strings.Contains(response.Body.String(), required) {
			t.Fatalf("response is missing %q: %s", required, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), "authoritative source body") ||
		response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("Vary") != "Cookie" {
		t.Fatalf("unsafe Thread response: headers %#v, body %s", response.Header(), response.Body.String())
	}
}

func TestCollaborationHandlerProposesDecisionWithIdempotentWebCommand(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 1, 3, 0, 0, time.UTC)
	application := &fakeCollaborationApplication{decisionResult: goldenpath.CreateDecisionResult{
		Created: true,
		Decision: goldenpath.Decision{
			ID: "dec_created", WorkspaceID: "wrk_main", GoverningProjectID: "prj_main",
			Question: "Adopt Session collaboration?", Status: "proposed", ProposerID: "usr_reader",
			DeciderIDs: []string{}, CreatedAt: now, UpdatedAt: now,
		},
	}}
	request := messagingWriteRequest(
		http.MethodPost,
		"/api/v1/workspaces/wrk_main/threads/thr_source/decisions",
		`{"client_operation_id":"browser:decision:1","question":"  Adopt Session collaboration?  "}`,
	)
	response := httptest.NewRecorder()
	testCollaborationHandler(t, validMessagingSessions(), application).ServeHTTP(response, request)

	if response.Code != http.StatusCreated || application.decisionCalls != 1 ||
		application.decisionInput != (goldenpath.CreateDecisionInput{
			ThreadID: "thr_source", ClientOperationID: "browser:decision:1", Question: "  Adopt Session collaboration?  ",
		}) || application.invocation.SourceKind != "web" || application.invocation.Principal.ID != "usr_reader" ||
		len(application.invocation.CorrelationID) != 36 {
		t.Fatalf("response = %d %q, application %#v", response.Code, response.Body.String(), application)
	}
	if !strings.Contains(response.Body.String(), `"source_thread":{"type":"thread","id":"thr_source"}`) ||
		strings.Contains(response.Body.String(), "client_operation") {
		t.Fatalf("Decision response = %s", response.Body.String())
	}
}

func TestCollaborationHandlerReturnsExistingDecisionForExactRetry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 1, 4, 0, 0, time.UTC)
	application := &fakeCollaborationApplication{decisionResult: goldenpath.CreateDecisionResult{
		Decision: goldenpath.Decision{
			ID: "dec_existing", WorkspaceID: "wrk_main", GoverningProjectID: "prj_main",
			Question: "Adopt Session collaboration?", Outcome: "Yes", Rationale: "It preserves authority.",
			Status: "accepted", ProposerID: "usr_reader", DeciderIDs: []string{"usr_reader"},
			DecidedAt: &now, CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
		},
	}}
	response := httptest.NewRecorder()
	testCollaborationHandler(t, validMessagingSessions(), application).ServeHTTP(
		response,
		messagingWriteRequest(
			http.MethodPost,
			"/api/v1/workspaces/wrk_main/threads/thr_source/decisions",
			`{"client_operation_id":"browser:decision:1","question":"Adopt Session collaboration?"}`,
		),
	)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestCollaborationHandlerRequiresHumanConfirmationAndAcceptsDecision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 1, 5, 0, 0, time.UTC)
	application := &fakeCollaborationApplication{acceptanceResult: goldenpath.AcceptDecisionResult{
		Accepted: true,
		Decision: goldenpath.Decision{
			ID: "dec_target", WorkspaceID: "wrk_main", GoverningProjectID: "prj_main",
			Question: "Adopt Session collaboration?", Outcome: "Yes", Rationale: "Keeps one authority.",
			Status: "accepted", ProposerID: "usr_writer", DeciderIDs: []string{"usr_reader"},
			DecidedAt: &now, CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
		},
	}}
	handler := testCollaborationHandler(t, validMessagingSessions(), application)

	unconfirmed := httptest.NewRecorder()
	handler.ServeHTTP(
		unconfirmed,
		messagingWriteRequest(
			http.MethodPost,
			"/api/v1/workspaces/wrk_main/decisions/dec_target/acceptance",
			`{"client_operation_id":"browser:accept:1","outcome":"Yes","rationale":"Keeps one authority.","confirmed":false}`,
		),
	)
	if unconfirmed.Code != http.StatusBadRequest || application.acceptanceCalls != 0 {
		t.Fatalf("unconfirmed response = %d %q, calls = %d", unconfirmed.Code, unconfirmed.Body.String(), application.acceptanceCalls)
	}

	confirmed := httptest.NewRecorder()
	handler.ServeHTTP(
		confirmed,
		messagingWriteRequest(
			http.MethodPost,
			"/api/v1/workspaces/wrk_main/decisions/dec_target/acceptance",
			`{"client_operation_id":"browser:accept:1","outcome":" Yes ","rationale":" Keeps one authority. ","confirmed":true}`,
		),
	)
	if confirmed.Code != http.StatusOK || application.acceptanceCalls != 1 ||
		application.acceptanceInput.ClientOperationID != "browser:accept:1" ||
		!strings.Contains(confirmed.Body.String(), `"outcome":"Yes"`) ||
		strings.Contains(confirmed.Body.String(), "client_operation") {
		t.Fatalf("confirmed response = %d %q, application %#v", confirmed.Code, confirmed.Body.String(), application)
	}
}

func TestCollaborationHandlerCreatesTicketWithStructuredDecisionSource(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 1, 6, 0, 0, time.UTC)
	application := &fakeCollaborationApplication{ticketResult: goldenpath.CreateTicketResult{
		Created: true,
		Ticket: goldenpath.Ticket{
			ID: "tkt_created", WorkspaceID: "wrk_main", GoverningProjectID: "prj_main",
			Title: "Implement collaboration transport", Status: "open", CreatedBy: "usr_reader",
			CreatedAt: now, UpdatedAt: now,
		},
	}}
	response := httptest.NewRecorder()
	testCollaborationHandler(t, validMessagingSessions(), application).ServeHTTP(
		response,
		messagingWriteRequest(
			http.MethodPost,
			"/api/v1/workspaces/wrk_main/decisions/dec_target/tickets",
			`{"client_operation_id":"browser:ticket:1","title":" Implement collaboration transport "}`,
		),
	)
	if response.Code != http.StatusCreated || application.ticketCalls != 1 ||
		application.ticketInput.DecisionID != "dec_target" ||
		!strings.Contains(response.Body.String(), `"source_decision":{"type":"decision","id":"dec_target"}`) ||
		strings.Contains(response.Body.String(), "client_operation") {
		t.Fatalf("response = %d %q, application %#v", response.Code, response.Body.String(), application)
	}
}

func TestCollaborationHandlerKeepsRestrictedDecisionEvidenceOpaque(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 1, 7, 0, 0, time.UTC)
	application := &fakeCollaborationApplication{view: goldenpath.NexusView{
		Current: goldenpath.CurrentProjection{
			Ref: entityref.Ref{Type: "decision", ID: "dec_target"}, GoverningProjectID: "prj_main",
			Title: "Adopt Session collaboration?", Status: "proposed", ProposerID: "usr_writer",
			DeciderIDs: []string{}, CreatedAt: now, UpdatedAt: now,
		},
		Relations: []goldenpath.RelationProjection{{State: goldenpath.ProjectionRestricted}},
		Timeline: []goldenpath.TimelineItem{{
			EventID: "evt_proposed", ActivityType: "decision.proposed",
			Actor: goldenpath.ActorRef{Kind: "user", ID: "usr_writer"}, OccurredAt: now,
			Subjects:          []goldenpath.SubjectProjection{{State: goldenpath.ProjectionRestricted}},
			ProjectionVersion: goldenpath.ActivityProjectionVersion,
			SafeFacts:         map[string]string{"status": "proposed"},
		}},
	}}
	response := httptest.NewRecorder()
	testCollaborationHandler(t, validMessagingSessions(), application).ServeHTTP(
		response,
		messagingRequest(http.MethodGet, "/api/v1/workspaces/wrk_main/decisions/dec_target/nexus-view", ""),
	)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "thr_private") ||
		strings.Count(response.Body.String(), `"visibility":"restricted"`) != 2 {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestCollaborationHandlerEnforcesSessionCSRFMethodsAndStrictBodies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		prepare    func(*http.Request, *fakeMessagingSessions, *fakeCollaborationApplication)
		wantStatus int
		wantCode   string
		wantAllow  string
	}{
		{
			name: "missing Session", method: http.MethodGet,
			path: "/api/v1/workspaces/wrk_main/threads/thr_source/nexus-view",
			prepare: func(request *http.Request, _ *fakeMessagingSessions, _ *fakeCollaborationApplication) {
				request.Header.Del("Cookie")
			},
			wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated",
		},
		{
			name: "membership unavailable is hidden", method: http.MethodGet,
			path: "/api/v1/workspaces/wrk_main/threads/thr_source/nexus-view",
			prepare: func(_ *http.Request, sessions *fakeMessagingSessions, _ *fakeCollaborationApplication) {
				sessions.resolveErr = authz.ErrForbidden
			},
			wantStatus: http.StatusNotFound, wantCode: "not_found",
		},
		{
			name: "wrong stored CSRF digest", method: http.MethodPost,
			path: "/api/v1/workspaces/wrk_main/threads/thr_source/decisions",
			body: `{"client_operation_id":"op-1","question":"Question?"}`,
			prepare: func(_ *http.Request, sessions *fakeMessagingSessions, _ *fakeCollaborationApplication) {
				sessions.verifyErr = authn.ErrInvalidCSRFToken
			},
			wantStatus: http.StatusForbidden, wantCode: "csrf_failed",
		},
		{
			name: "unknown JSON field", method: http.MethodPost,
			path:       "/api/v1/workspaces/wrk_main/threads/thr_source/decisions",
			body:       `{"client_operation_id":"op-1","question":"Question?","actor_id":"usr_attacker"}`,
			wantStatus: http.StatusBadRequest, wantCode: "invalid",
		},
		{
			name: "unsupported query", method: http.MethodGet,
			path:       "/api/v1/workspaces/wrk_main/threads/thr_source/nexus-view?expand=body",
			wantStatus: http.StatusBadRequest, wantCode: "invalid",
		},
		{
			name: "wrong read method", method: http.MethodPost,
			path:       "/api/v1/workspaces/wrk_main/threads/thr_source/nexus-view",
			body:       `{}`,
			wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed", wantAllow: http.MethodGet,
		},
		{
			name: "unknown nested path", method: http.MethodGet,
			path:       "/api/v1/workspaces/wrk_main/threads/thr_source/body",
			wantStatus: http.StatusNotFound, wantCode: "not_found",
		},
		{
			name: "application forbidden", method: http.MethodPost,
			path: "/api/v1/workspaces/wrk_main/decisions/dec_target/acceptance",
			body: `{"client_operation_id":"op-1","outcome":"Yes","rationale":"Reason","confirmed":true}`,
			prepare: func(_ *http.Request, _ *fakeMessagingSessions, application *fakeCollaborationApplication) {
				application.acceptanceErr = authz.ErrForbidden
			},
			wantStatus: http.StatusForbidden, wantCode: "forbidden",
		},
		{
			name: "changed idempotent replay", method: http.MethodPost,
			path: "/api/v1/workspaces/wrk_main/decisions/dec_target/tickets",
			body: `{"client_operation_id":"op-1","title":"Ticket"}`,
			prepare: func(_ *http.Request, _ *fakeMessagingSessions, application *fakeCollaborationApplication) {
				application.ticketErr = authz.ErrConflict
			},
			wantStatus: http.StatusConflict, wantCode: "conflict",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sessions := validMessagingSessions()
			application := &fakeCollaborationApplication{}
			request := messagingRequest(test.method, test.path, test.body)
			if test.method == http.MethodPost {
				addMessagingWriteCredentials(request)
			}
			if test.prepare != nil {
				test.prepare(request, sessions, application)
			}
			response := httptest.NewRecorder()
			testCollaborationHandler(t, sessions, application).ServeHTTP(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) ||
				response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("Vary") != "Cookie" ||
				response.Header().Get("Allow") != test.wantAllow {
				t.Fatalf("response = %d, headers %#v, body %q", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}

func TestPublicCollaborationViewFailsClosedOnProjectionDrift(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 1, 8, 0, 0, time.UTC)
	base := goldenpath.NexusView{
		Current: goldenpath.CurrentProjection{
			Ref: entityref.Ref{Type: "ticket", ID: "tkt_target"}, GoverningProjectID: "prj_main",
			Title: "Implement transport", Status: "open",
			CreatedBy: goldenpath.ActorRef{Kind: "user", ID: "usr_reader"}, CreatedAt: now, UpdatedAt: now,
		},
		Relations: []goldenpath.RelationProjection{{
			State: goldenpath.ProjectionVisible, RelationType: "implements",
			Target: entityref.Ref{Type: "decision", ID: "dec_target"}, Title: "Decision",
		}},
	}
	for _, mutate := range []func(*goldenpath.NexusView){
		func(view *goldenpath.NexusView) { view.Current.Ref.ID = "tkt_other" },
		func(view *goldenpath.NexusView) { view.Relations[0].RelationType = "derived-from" },
		func(view *goldenpath.NexusView) { view.Relations[0].Target.Type = "thread" },
		func(view *goldenpath.NexusView) { view.Current.CreatedBy.Kind = "plugin" },
		func(view *goldenpath.NexusView) { view.Current.CreatedBy.ID = "" },
	} {
		view := base
		view.Relations = append([]goldenpath.RelationProjection(nil), base.Relations...)
		mutate(&view)
		if _, err := publicCollaborationView(entityref.Ref{Type: "ticket", ID: "tkt_target"}, view); err == nil {
			t.Fatalf("publicCollaborationView(%#v) error = nil", view)
		}
	}
}

func testCollaborationHandler(
	t *testing.T,
	sessions *fakeMessagingSessions,
	application CollaborationApplication,
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
	return WithRequestID(NewCollaborationHandler(sessions, application, session, proxy))
}

func TestCollaborationHandlerMapsApplicationErrors(t *testing.T) {
	t.Parallel()
	application := &fakeCollaborationApplication{viewErr: errors.New("database unavailable")}
	response := httptest.NewRecorder()
	testCollaborationHandler(t, validMessagingSessions(), application).ServeHTTP(
		response,
		messagingRequest(http.MethodGet, "/api/v1/workspaces/wrk_main/tickets/tkt_target/nexus-view", ""),
	)
	var payload ErrorResponse
	if response.Code != http.StatusInternalServerError || json.Unmarshal(response.Body.Bytes(), &payload) != nil ||
		payload.Error.Code != "internal" || payload.Error.Message != "internal server error" ||
		strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}
