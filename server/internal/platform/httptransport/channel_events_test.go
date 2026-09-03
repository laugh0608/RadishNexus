package httptransport

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/realtime"
)

type streamSessions struct {
	mu       sync.Mutex
	verified authn.VerifiedUser
	err      error
}

func (sessions *streamSessions) ResolveWorkspace(
	_ context.Context,
	_ string,
	_ string,
) (authn.VerifiedUser, error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	return sessions.verified, sessions.err
}

func (sessions *streamSessions) setError(err error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.err = err
}

type streamApplication struct {
	mu           sync.Mutex
	authorizeErr error
	messages     map[string]goldenpath.MessageProjection
	messageErrs  map[string]error
}

func (application *streamApplication) AuthorizeChannelRead(
	_ context.Context,
	_ authz.Principal,
	_ string,
) error {
	application.mu.Lock()
	defer application.mu.Unlock()
	return application.authorizeErr
}

func (application *streamApplication) GetChannelMessage(
	_ context.Context,
	_ authz.Principal,
	_ string,
	messageID string,
) (goldenpath.MessageProjection, error) {
	application.mu.Lock()
	defer application.mu.Unlock()
	if err := application.messageErrs[messageID]; err != nil {
		return goldenpath.MessageProjection{}, err
	}
	message, exists := application.messages[messageID]
	if !exists {
		return goldenpath.MessageProjection{}, authz.ErrNotFound
	}
	return message, nil
}

func (application *streamApplication) setMessage(message goldenpath.MessageProjection) {
	application.mu.Lock()
	defer application.mu.Unlock()
	application.messages[message.ID] = message
}

func TestChannelEventsStreamsReadyVisibleMessageAndAccessRevocation(t *testing.T) {
	t.Parallel()
	hub := testRealtimeHub(t, realtime.Config{
		Generation: "http-test-generation", ReplayLimit: 8,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	defer hub.Shutdown()
	sessions := &streamSessions{verified: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"}}
	application := &streamApplication{
		messages:    make(map[string]goldenpath.MessageProjection),
		messageErrs: make(map[string]error),
	}
	server := testChannelEventsServer(t, sessions, application, hub, time.Hour)
	defer server.Close()

	response := openChannelEvents(t, server, "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != "text/event-stream; charset=utf-8" ||
		response.Header.Get("Cache-Control") != "private, no-store" ||
		response.Header.Get("Vary") != "Cookie" ||
		response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("stream response = %d, headers %#v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	ready := readSSEEvent(t, reader)
	if ready.event != "ready" || ready.id == "" || ready.data != "{}" {
		t.Fatalf("ready event = %#v", ready)
	}

	createdAt := time.Date(2026, 9, 3, 2, 3, 4, 0, time.UTC)
	application.setMessage(goldenpath.MessageProjection{
		ID: "msg_visible", ChannelID: "chn_main", AuthorID: "usr_writer",
		Body: "visible body", CreatedAt: createdAt,
	})
	hub.NotifyMessageCreated(realtime.MessageNotification{
		WorkspaceID: "wrk_main", ChannelID: "chn_main", MessageID: "msg_visible",
	})
	message := readSSEEvent(t, reader)
	if message.event != "message.created" || message.id == "" ||
		!strings.Contains(message.data, `"body":"visible body"`) ||
		strings.Contains(message.data, "client_operation") {
		t.Fatalf("Message event = %#v", message)
	}

	sessions.setError(authz.ErrForbidden)
	hub.NotifyChannelAccessChanged("wrk_main", "chn_main")
	revoked := readSSEEvent(t, reader)
	if revoked.event != "access-revoked" || revoked.id != "" || revoked.data != "{}" {
		t.Fatalf("access event = %#v", revoked)
	}
}

func TestChannelEventsSkipsRestrictedMessageAndReplaysFromLastEventID(t *testing.T) {
	t.Parallel()
	hub := testRealtimeHub(t, realtime.Config{
		Generation: "replay-test-generation", ReplayLimit: 8,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	defer hub.Shutdown()
	sessions := &streamSessions{verified: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"}}
	application := &streamApplication{
		messages: map[string]goldenpath.MessageProjection{
			"msg_visible": {
				ID: "msg_visible", ChannelID: "chn_main", AuthorID: "usr_writer",
				Body: "allowed", CreatedAt: time.Date(2026, 9, 3, 3, 0, 0, 0, time.UTC),
			},
		},
		messageErrs: map[string]error{"msg_hidden": authz.ErrNotFound},
	}
	server := testChannelEventsServer(t, sessions, application, hub, time.Hour)
	defer server.Close()

	first := openChannelEvents(t, server, "")
	reader := bufio.NewReader(first.Body)
	ready := readSSEEvent(t, reader)
	first.Body.Close()

	hub.NotifyMessageCreated(realtime.MessageNotification{WorkspaceID: "wrk_main", ChannelID: "chn_main", MessageID: "msg_hidden"})
	hub.NotifyMessageCreated(realtime.MessageNotification{WorkspaceID: "wrk_main", ChannelID: "chn_main", MessageID: "msg_visible"})
	replay := openChannelEvents(t, server, ready.id)
	defer replay.Body.Close()
	replayReader := bufio.NewReader(replay.Body)
	replayReady := readSSEEvent(t, replayReader)
	visible := readSSEEvent(t, replayReader)
	if replayReady.event != "ready" || replayReady.id != ready.id ||
		visible.event != "message.created" || !strings.Contains(visible.data, "allowed") ||
		strings.Contains(visible.data, "msg_hidden") {
		t.Fatalf("replay events = ready %#v, visible %#v", replayReady, visible)
	}
}

func TestChannelEventsInvalidCursorRequiresDataFreeResync(t *testing.T) {
	t.Parallel()
	hub := testRealtimeHub(t, realtime.Config{
		Generation: "resync-test-generation", ReplayLimit: 8,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	defer hub.Shutdown()
	server := testChannelEventsServer(
		t,
		&streamSessions{verified: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"}},
		&streamApplication{messages: make(map[string]goldenpath.MessageProjection), messageErrs: make(map[string]error)},
		hub,
		time.Hour,
	)
	defer server.Close()
	response := openChannelEvents(t, server, "invalid-cursor")
	defer response.Body.Close()
	event := readSSEEvent(t, bufio.NewReader(response.Body))
	if response.StatusCode != http.StatusOK || event.event != "resync-required" || event.id != "" || event.data != "{}" {
		t.Fatalf("resync response = %d, %#v", response.StatusCode, event)
	}
	remainder, err := io.ReadAll(response.Body)
	if err != nil || len(remainder) != 0 {
		t.Fatalf("resync remainder = %q, %v", remainder, err)
	}
}

func TestChannelEventsHeartbeatRechecksSessionWithoutWake(t *testing.T) {
	t.Parallel()
	hub := testRealtimeHub(t, realtime.Config{
		Generation: "heartbeat-test-generation", ReplayLimit: 8,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	defer hub.Shutdown()
	sessions := &streamSessions{verified: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"}}
	server := testChannelEventsServer(
		t,
		sessions,
		&streamApplication{messages: make(map[string]goldenpath.MessageProjection), messageErrs: make(map[string]error)},
		hub,
		10*time.Millisecond,
	)
	defer server.Close()
	response := openChannelEvents(t, server, "")
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	_ = readSSEEvent(t, reader)
	sessions.setError(authz.ErrForbidden)
	event := readSSEEvent(t, reader)
	if event.event != "access-revoked" || event.data != "{}" {
		t.Fatalf("heartbeat revocation = %#v", event)
	}
}

func TestChannelEventsEscapesServerWriteTimeoutForLongLivedStream(t *testing.T) {
	t.Parallel()
	hub := testRealtimeHub(t, realtime.Config{
		Generation: "write-timeout-generation", ReplayLimit: 8,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	defer hub.Shutdown()
	application := &streamApplication{
		messages:    make(map[string]goldenpath.MessageProjection),
		messageErrs: make(map[string]error),
	}
	server := testChannelEventsServer(
		t,
		&streamSessions{verified: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"}},
		application,
		hub,
		10*time.Millisecond,
	)
	defer server.Close()
	response := openChannelEvents(t, server, "")
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	_ = readSSEEvent(t, reader)
	time.Sleep(75 * time.Millisecond)
	application.setMessage(goldenpath.MessageProjection{
		ID: "msg_after_timeout", ChannelID: "chn_main", AuthorID: "usr_writer",
		Body: "still connected", CreatedAt: time.Date(2026, 9, 3, 4, 0, 0, 0, time.UTC),
	})
	hub.NotifyMessageCreated(realtime.MessageNotification{
		WorkspaceID: "wrk_main", ChannelID: "chn_main", MessageID: "msg_after_timeout",
	})
	event := readSSEEvent(t, reader)
	if event.event != "message.created" || !strings.Contains(event.data, "still connected") {
		t.Fatalf("post-timeout Message event = %#v", event)
	}
}

func TestChannelEventsRejectsQueryMethodAndUnknownPathBeforeStreaming(t *testing.T) {
	t.Parallel()
	hub := testRealtimeHub(t, realtime.Config{
		Generation: "boundary-generation", ReplayLimit: 8,
		ConnectionLimit: 8, UserConnectionLimit: 4, ChannelConnectionLimit: 4,
	})
	defer hub.Shutdown()
	session, err := NewBrowserSessionPolicy("https://nexus.example.test")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewTrustedProxyPolicy("127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	handler := WithRequestID(newChannelEventsHandler(
		&streamSessions{verified: authn.VerifiedUser{UserID: "usr_reader", WorkspaceID: "wrk_main"}},
		&streamApplication{messages: make(map[string]goldenpath.MessageProjection), messageErrs: make(map[string]error)},
		hub,
		session,
		proxy,
		time.Hour,
		time.Second,
	))
	for _, test := range []struct {
		method     string
		path       string
		wantStatus int
		wantAllow  string
	}{
		{method: http.MethodGet, path: "/api/v1/workspaces/wrk_main/channels/chn_main/events?cursor=bad", wantStatus: http.StatusBadRequest},
		{method: http.MethodPost, path: "/api/v1/workspaces/wrk_main/channels/chn_main/events", wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodGet},
		{method: http.MethodGet, path: "/api/v1/workspaces/wrk_main/channels/chn_main/events/nested", wantStatus: http.StatusNotFound},
	} {
		request := httptest.NewRequest(test.method, "https://nexus.example.test"+test.path, nil)
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: transportToken(1)})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.wantStatus || response.Header().Get("Allow") != test.wantAllow ||
			!strings.Contains(response.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s %s response = %d, headers %#v, body %q", test.method, test.path, response.Code, response.Header(), response.Body.String())
		}
	}
}

type parsedSSEEvent struct {
	id    string
	event string
	data  string
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) parsedSSEEvent {
	t.Helper()
	result := make(chan struct {
		event parsedSSEEvent
		err   error
	}, 1)
	go func() {
		var event parsedSSEEvent
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				result <- struct {
					event parsedSSEEvent
					err   error
				}{err: err}
				return
			}
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			if line == "" && event.event != "" {
				result <- struct {
					event parsedSSEEvent
					err   error
				}{event: event}
				return
			}
			switch {
			case strings.HasPrefix(line, "id: "):
				event.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				event.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				event.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	select {
	case outcome := <-result:
		if outcome.err != nil {
			t.Fatalf("read SSE event: %v", outcome.err)
		}
		return outcome.event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out reading SSE event")
		return parsedSSEEvent{}
	}
}

func testChannelEventsServer(
	t *testing.T,
	sessions WorkspaceSessionResolver,
	application ChannelEventsApplication,
	hub *realtime.Hub,
	heartbeat time.Duration,
) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	origin := "https://" + server.Listener.Addr().String()
	session, err := NewBrowserSessionPolicy(origin)
	if err != nil {
		t.Fatalf("NewBrowserSessionPolicy() error = %v", err)
	}
	proxy, err := NewTrustedProxyPolicy("127.0.0.1/32")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	server.Config.Handler = WithRequestID(newChannelEventsHandler(
		sessions, application, hub, session, proxy, heartbeat, time.Second,
	))
	server.Config.WriteTimeout = 25 * time.Millisecond
	server.StartTLS()
	return server
}

func openChannelEvents(t *testing.T, server *httptest.Server, lastEventID string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/v1/workspaces/wrk_main/channels/chn_main/events",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: transportToken(1)})
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open Channel events: %v", err)
	}
	return response
}

func testRealtimeHub(t *testing.T, config realtime.Config) *realtime.Hub {
	t.Helper()
	hub, err := realtime.NewHub(config)
	if err != nil {
		t.Fatalf("NewHub() error = %v", err)
	}
	return hub
}

func TestRealtimeCapacityErrorMapping(t *testing.T) {
	t.Parallel()
	mapping := MapApplicationError(ErrRealtimeCapacity)
	if mapping.StatusCode != http.StatusTooManyRequests || mapping.Code != "rate_limited" {
		t.Fatalf("capacity mapping = %#v", mapping)
	}
	if !errors.Is(ErrRealtimeCapacity, ErrRealtimeCapacity) {
		t.Fatal("capacity error identity changed")
	}
}
