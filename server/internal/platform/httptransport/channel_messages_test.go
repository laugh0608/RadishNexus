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
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type fakeMessagingSessions struct {
	verified         authn.VerifiedUser
	resolveErr       error
	verifyErr        error
	resolveCalls     int
	verifyCalls      int
	resolveToken     string
	resolveWorkspace string
	verifyToken      string
	verifyCSRFToken  string
}

func (sessions *fakeMessagingSessions) ResolveWorkspace(
	_ context.Context,
	token string,
	workspaceID string,
) (authn.VerifiedUser, error) {
	sessions.resolveCalls++
	sessions.resolveToken = token
	sessions.resolveWorkspace = workspaceID
	return sessions.verified, sessions.resolveErr
}

func (sessions *fakeMessagingSessions) VerifyCSRF(
	_ context.Context,
	token string,
	csrfToken string,
) error {
	sessions.verifyCalls++
	sessions.verifyToken = token
	sessions.verifyCSRFToken = csrfToken
	return sessions.verifyErr
}

type fakeChannelMessagingApplication struct {
	page             goldenpath.MessagePage
	messageResult    goldenpath.CreateMessageResult
	thread           goldenpath.Thread
	listErr          error
	createErr        error
	threadErr        error
	listCalls        int
	createCalls      int
	threadCalls      int
	listPrincipal    authz.Principal
	listInput        goldenpath.ListChannelMessagesInput
	createInvocation goldenpath.Invocation
	createInput      goldenpath.CreateMessageInput
	threadInvocation goldenpath.Invocation
	threadInput      goldenpath.StartThreadFromMessageInput
}

func (application *fakeChannelMessagingApplication) ListChannelMessages(
	_ context.Context,
	principal authz.Principal,
	input goldenpath.ListChannelMessagesInput,
) (goldenpath.MessagePage, error) {
	application.listCalls++
	application.listPrincipal = principal
	application.listInput = input
	return application.page, application.listErr
}

func (application *fakeChannelMessagingApplication) CreateMessage(
	_ context.Context,
	invocation goldenpath.Invocation,
	input goldenpath.CreateMessageInput,
) (goldenpath.CreateMessageResult, error) {
	application.createCalls++
	application.createInvocation = invocation
	application.createInput = input
	return application.messageResult, application.createErr
}

func (application *fakeChannelMessagingApplication) StartThreadFromMessage(
	_ context.Context,
	invocation goldenpath.Invocation,
	input goldenpath.StartThreadFromMessageInput,
) (goldenpath.Thread, error) {
	application.threadCalls++
	application.threadInvocation = invocation
	application.threadInput = input
	return application.thread, application.threadErr
}

func TestChannelMessagesHandlerListsCanonicalPageWithOpaqueCursor(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 9, 1, 2, 3, 4, 123000000, time.UTC)
	older := goldenpath.MessagePageCursor{CreatedAt: createdAt, MessageID: "msg_older"}
	application := &fakeChannelMessagingApplication{page: goldenpath.MessagePage{
		Messages: []goldenpath.MessageProjection{
			{ID: "msg_older", ChannelID: "chn_main", AuthorID: "usr_reader", Body: "older body", CreatedAt: createdAt},
			{ID: "msg_newer", ChannelID: "chn_main", AuthorID: "usr_writer", Body: "newer body", CreatedAt: createdAt.Add(time.Second)},
		},
		OlderCursor: &older,
	}}
	sessions := validMessagingSessions()
	request := messagingRequest(http.MethodGet, "/api/v1/workspaces/wrk_main/channels/chn_main/messages?limit=2", "")
	response := httptest.NewRecorder()
	testChannelMessagesHandler(t, sessions, application).ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Header().Get("Vary") != "Cookie" || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("response = status %d, headers %#v, body %q", response.Code, response.Header(), response.Body.String())
	}
	wantPrincipal := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_reader", WorkspaceID: "wrk_main"}
	if sessions.resolveCalls != 1 || sessions.verifyCalls != 0 || application.listCalls != 1 ||
		application.listPrincipal != wantPrincipal || application.listInput.ChannelID != "chn_main" ||
		application.listInput.Limit != 2 || application.listInput.Before != nil {
		t.Fatalf("list boundary = sessions %#v, application %#v", sessions, application)
	}
	var payload messagePageResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data.Messages) != 2 || payload.Data.Messages[0].Body != "older body" ||
		payload.Data.Messages[0].Thread != nil || payload.Data.OlderCursor == nil {
		t.Fatalf("Message page DTO = %#v", payload)
	}
	cursor, err := decodePublicMessageCursor(*payload.Data.OlderCursor)
	if err != nil || cursor != older {
		t.Fatalf("public cursor = %#v, %v, want %#v", cursor, err, older)
	}
	if strings.Contains(response.Body.String(), "client_operation") {
		t.Fatalf("response leaks idempotency state: %s", response.Body.String())
	}
}

func TestChannelMessagesHandlerCreatesMessageWithVerifiedWebInvocation(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 9, 1, 2, 4, 0, 0, time.UTC)
	threadID := "thr_focus"
	application := &fakeChannelMessagingApplication{messageResult: goldenpath.CreateMessageResult{
		Created: true,
		Message: goldenpath.Message{
			ID: "msg_created", WorkspaceID: "wrk_main", ChannelID: "chn_main", ThreadID: &threadID,
			AuthorID: "usr_reader", Body: "  exact body\n", ClientOperationID: "browser-tab:7", CreatedAt: createdAt,
		},
	}}
	sessions := validMessagingSessions()
	request := messagingWriteRequest(
		http.MethodPost,
		"/api/v1/workspaces/wrk_main/channels/chn_main/messages",
		`{"client_operation_id":"browser-tab:7","body":"  exact body\n","thread_id":"thr_focus"}`,
	)
	response := httptest.NewRecorder()
	testChannelMessagesHandler(t, sessions, application).ServeHTTP(response, request)

	if response.Code != http.StatusCreated || sessions.verifyCalls != 1 || application.createCalls != 1 {
		t.Fatalf("response = %d %q, sessions %#v, calls %d", response.Code, response.Body.String(), sessions, application.createCalls)
	}
	if application.createInput != (goldenpath.CreateMessageInput{
		ChannelID: "chn_main", ThreadID: "thr_focus", ClientOperationID: "browser-tab:7", Body: "  exact body\n",
	}) {
		t.Fatalf("CreateMessage input = %#v", application.createInput)
	}
	invocation := application.createInvocation
	if invocation.Principal.ID != "usr_reader" || invocation.Principal.WorkspaceID != "wrk_main" ||
		invocation.SourceKind != "web" || len(invocation.CorrelationID) != 36 || invocation.CorrelationID[:4] != "req_" ||
		invocation.SourceID != "" || invocation.CausationID != "" {
		t.Fatalf("CreateMessage invocation = %#v", invocation)
	}
	if !strings.Contains(response.Body.String(), `"type":"message"`) ||
		!strings.Contains(response.Body.String(), `"thread":{"type":"thread","id":"thr_focus"}`) ||
		strings.Contains(response.Body.String(), "client_operation") {
		t.Fatalf("response body = %s", response.Body.String())
	}
}

func TestChannelMessagesHandlerReturnsOKForExactMessageRetry(t *testing.T) {
	t.Parallel()
	application := &fakeChannelMessagingApplication{messageResult: goldenpath.CreateMessageResult{
		Message: goldenpath.Message{
			ID: "msg_existing", WorkspaceID: "wrk_main", ChannelID: "chn_main", AuthorID: "usr_reader",
			Body: "same body", ClientOperationID: "browser-tab:8", CreatedAt: time.Date(2026, 9, 1, 2, 5, 0, 0, time.UTC),
		},
	}}
	response := httptest.NewRecorder()
	testChannelMessagesHandler(t, validMessagingSessions(), application).ServeHTTP(
		response,
		messagingWriteRequest(http.MethodPost, "/api/v1/workspaces/wrk_main/channels/chn_main/messages", `{"client_operation_id":"browser-tab:8","body":"same body"}`),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestChannelMessagesHandlerStartsThreadWithinPathChannel(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 9, 1, 2, 6, 0, 0, time.UTC)
	channelID := "chn_main"
	application := &fakeChannelMessagingApplication{thread: goldenpath.Thread{
		ID: "thr_created", WorkspaceID: "wrk_main", GoverningProjectID: "prj_main", OriginChannelID: &channelID,
		Title: "Investigate latency", Visibility: "restricted", CreatedBy: "usr_reader", CreatedAt: createdAt, UpdatedAt: createdAt,
	}}
	response := httptest.NewRecorder()
	testChannelMessagesHandler(t, validMessagingSessions(), application).ServeHTTP(
		response,
		messagingWriteRequest(
			http.MethodPost,
			"/api/v1/workspaces/wrk_main/channels/chn_main/messages/msg_source/threads",
			`{"title":"  Investigate latency  ","visibility":"restricted"}`,
		),
	)
	if response.Code != http.StatusCreated || application.threadCalls != 1 {
		t.Fatalf("response = %d %q, calls = %d", response.Code, response.Body.String(), application.threadCalls)
	}
	if application.threadInput != (goldenpath.StartThreadFromMessageInput{
		ChannelID: "chn_main", MessageID: "msg_source", Title: "  Investigate latency  ", Visibility: "restricted",
	}) || application.threadInvocation.SourceKind != "web" {
		t.Fatalf("StartThread boundary = input %#v, invocation %#v", application.threadInput, application.threadInvocation)
	}
	for _, required := range []string{`"type":"thread"`, `"id":"thr_created"`, `"source_message":{"type":"message","id":"msg_source"}`, `"title":"Investigate latency"`} {
		if !strings.Contains(response.Body.String(), required) {
			t.Fatalf("response is missing %q: %s", required, response.Body.String())
		}
	}
}

func TestChannelMessagesHandlerEnforcesSessionCSRFAndUnfindability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		prepare    func(*http.Request, *fakeMessagingSessions)
		wantStatus int
		wantCode   string
	}{
		{
			name: "read missing session", method: http.MethodGet,
			path: "/api/v1/workspaces/wrk_main/channels/chn_main/messages",
			prepare: func(request *http.Request, _ *fakeMessagingSessions) {
				request.Header.Del("Cookie")
			},
			wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated",
		},
		{
			name: "membership unavailable is hidden", method: http.MethodGet,
			path: "/api/v1/workspaces/wrk_main/channels/chn_main/messages",
			prepare: func(_ *http.Request, sessions *fakeMessagingSessions) {
				sessions.resolveErr = authz.ErrForbidden
			},
			wantStatus: http.StatusNotFound, wantCode: "not_found",
		},
		{
			name: "write missing Origin", method: http.MethodPost,
			path: "/api/v1/workspaces/wrk_main/channels/chn_main/messages", body: `{"client_operation_id":"op-1","body":"body"}`,
			prepare: func(request *http.Request, _ *fakeMessagingSessions) {
				request.Header.Del("Origin")
			},
			wantStatus: http.StatusForbidden, wantCode: "csrf_failed",
		},
		{
			name: "write wrong stored digest", method: http.MethodPost,
			path: "/api/v1/workspaces/wrk_main/channels/chn_main/messages", body: `{"client_operation_id":"op-1","body":"body"}`,
			prepare: func(_ *http.Request, sessions *fakeMessagingSessions) {
				sessions.verifyErr = authn.ErrInvalidCSRFToken
			},
			wantStatus: http.StatusForbidden, wantCode: "csrf_failed",
		},
		{
			name: "invalid cursor", method: http.MethodGet,
			path:       "/api/v1/workspaces/wrk_main/channels/chn_main/messages?before=not-a-cursor",
			wantStatus: http.StatusBadRequest, wantCode: "invalid",
		},
		{
			name: "unsupported query", method: http.MethodGet,
			path:       "/api/v1/workspaces/wrk_main/channels/chn_main/messages?offset=1",
			wantStatus: http.StatusBadRequest, wantCode: "invalid",
		},
		{
			name: "wrong method", method: http.MethodHead,
			path:       "/api/v1/workspaces/wrk_main/channels/chn_main/messages",
			wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sessions := validMessagingSessions()
			application := &fakeChannelMessagingApplication{}
			request := messagingRequest(test.method, test.path, test.body)
			if test.method == http.MethodPost {
				addMessagingWriteCredentials(request)
			}
			if test.prepare != nil {
				test.prepare(request, sessions)
			}
			response := httptest.NewRecorder()
			testChannelMessagesHandler(t, sessions, application).ServeHTTP(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) ||
				response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("Vary") != "Cookie" {
				t.Fatalf("response = %d, headers %#v, body %q", response.Code, response.Header(), response.Body.String())
			}
			if application.listCalls+application.createCalls+application.threadCalls != 0 {
				t.Fatalf("rejected request reached application: %#v", application)
			}
		})
	}
}

func TestChannelMessagesHandlerRejectsStrictWriteBodiesAndProjectionDrift(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		body        string
		contentType string
		application *fakeChannelMessagingApplication
		wantStatus  int
		wantCode    string
	}{
		{
			name: "unknown JSON field", body: `{"client_operation_id":"op-1","body":"body","actor_id":"usr_attacker"}`,
			wantStatus: http.StatusBadRequest, wantCode: "invalid",
		},
		{
			name: "wrong media type", body: `{"client_operation_id":"op-1","body":"body"}`, contentType: "text/plain",
			wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type",
		},
		{
			name: "application projection mismatch", body: `{"client_operation_id":"op-1","body":"body"}`,
			application: &fakeChannelMessagingApplication{messageResult: goldenpath.CreateMessageResult{
				Created: true,
				Message: goldenpath.Message{
					ID: "msg_wrong", WorkspaceID: "wrk_other", ChannelID: "chn_main", AuthorID: "usr_reader",
					Body: "body", ClientOperationID: "op-1", CreatedAt: time.Now().UTC(),
				},
			}},
			wantStatus: http.StatusInternalServerError, wantCode: "internal",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := test.application
			if application == nil {
				application = &fakeChannelMessagingApplication{}
			}
			request := messagingWriteRequest(http.MethodPost, "/api/v1/workspaces/wrk_main/channels/chn_main/messages", test.body)
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			testChannelMessagesHandler(t, validMessagingSessions(), application).ServeHTTP(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) ||
				strings.Contains(response.Body.String(), "wrk_other") {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestChannelMessagesHandlerFailsClosedOnMessagePageDrift(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 1, 4, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		page goldenpath.MessagePage
	}{
		{
			name: "wrong Channel",
			page: goldenpath.MessagePage{Messages: []goldenpath.MessageProjection{{
				ID: "msg_1", ChannelID: "chn_other", AuthorID: "usr_reader", Body: "body", CreatedAt: now,
			}}},
		},
		{
			name: "unstable order",
			page: goldenpath.MessagePage{Messages: []goldenpath.MessageProjection{
				{ID: "msg_2", ChannelID: "chn_main", AuthorID: "usr_reader", Body: "new", CreatedAt: now.Add(time.Second)},
				{ID: "msg_1", ChannelID: "chn_main", AuthorID: "usr_reader", Body: "old", CreatedAt: now},
			}},
		},
		{
			name: "cursor mismatch",
			page: goldenpath.MessagePage{
				Messages: []goldenpath.MessageProjection{{
					ID: "msg_1", ChannelID: "chn_main", AuthorID: "usr_reader", Body: "body", CreatedAt: now,
				}},
				OlderCursor: &goldenpath.MessagePageCursor{CreatedAt: now, MessageID: "msg_other"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testChannelMessagesHandler(t, validMessagingSessions(), &fakeChannelMessagingApplication{page: test.page}).ServeHTTP(
				response,
				messagingRequest(http.MethodGet, "/api/v1/workspaces/wrk_main/channels/chn_main/messages?limit=2", ""),
			)
			if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal"`) ||
				strings.Contains(response.Body.String(), "chn_other") {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestPublicMessageCursorRejectsNonCanonicalAndUnknownVersions(t *testing.T) {
	t.Parallel()
	cursor := goldenpath.MessagePageCursor{
		CreatedAt: time.Date(2026, 9, 1, 3, 0, 0, 123000000, time.UTC),
		MessageID: "msg_cursor",
	}
	encoded, err := encodePublicMessageCursor(cursor)
	if err != nil {
		t.Fatalf("encodePublicMessageCursor() error = %v", err)
	}
	decoded, err := decodePublicMessageCursor(encoded)
	if err != nil || decoded != cursor {
		t.Fatalf("decodePublicMessageCursor() = %#v, %v", decoded, err)
	}
	for _, invalid := range []string{
		"",
		"not-base64",
		base64Cursor(t, `{"v":2,"created_at":"2026-09-01T03:00:00Z","message_id":"msg_cursor"}`),
		base64Cursor(t, `{"v":1,"created_at":"2026-09-01T03:00:00+00:00","message_id":"msg_cursor"}`),
		base64Cursor(t, `{"v":1,"created_at":"2026-09-01T03:00:00Z","message_id":"msg_cursor","extra":true}`),
	} {
		if _, err := decodePublicMessageCursor(invalid); !errors.Is(err, authz.ErrInvalid) {
			t.Fatalf("decodePublicMessageCursor(%q) error = %v, want invalid", invalid, err)
		}
	}
}

func validMessagingSessions() *fakeMessagingSessions {
	return &fakeMessagingSessions{verified: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"}}
}

func testChannelMessagesHandler(
	t *testing.T,
	sessions MessagingSessionService,
	application ChannelMessagingApplication,
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
	return WithRequestID(NewChannelMessagesHandler(sessions, application, session, proxy))
}

func messagingRequest(method string, path string, body string) *http.Request {
	request := httptest.NewRequest(method, "https://nexus.example.test"+path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: transportToken(1)})
	return request
}

func messagingWriteRequest(method string, path string, body string) *http.Request {
	request := messagingRequest(method, path, body)
	addMessagingWriteCredentials(request)
	return request
}

func addMessagingWriteCredentials(request *http.Request) {
	csrfToken := transportToken(2)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://nexus.example.test")
	request.Header.Set(CSRFHeaderName, csrfToken)
	request.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfToken})
}

func base64Cursor(t *testing.T, value string) string {
	t.Helper()
	var raw json.RawMessage = []byte(value)
	if !json.Valid(raw) {
		t.Fatalf("invalid cursor fixture JSON: %s", value)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
