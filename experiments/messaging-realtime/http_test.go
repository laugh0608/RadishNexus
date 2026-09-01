package messagingrealtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testChannel = "chn_general"
	testWriter  = "usr_writer"
	testReader  = "usr_reader"
)

type testFixture struct {
	gate   *AccessGate
	hub    *Hub
	server *httptest.Server
}

func newTestFixture(t *testing.T, maxReplay int) *testFixture {
	t.Helper()
	gate := NewAccessGate()
	hub, err := NewHub("process-a", maxReplay, gate)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	handler, err := NewHTTPHandler(hub, time.Hour)
	if err != nil {
		t.Fatalf("NewHTTPHandler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &testFixture{gate: gate, hub: hub, server: server}
}

type postMessageResponse struct {
	Message Message `json:"message"`
	Created bool    `json:"created"`
}

func (fixture *testFixture) postMessage(
	t *testing.T,
	userID string,
	channelID string,
	operationID string,
	body string,
) (int, postMessageResponse) {
	t.Helper()
	payload, err := json.Marshal(postMessageRequest{
		ClientOperationID: operationID,
		Body:              body,
	})
	if err != nil {
		t.Fatalf("marshal post body: %v", err)
	}
	request, err := http.NewRequest(
		http.MethodPost,
		fixture.server.URL+"/channels/"+channelID+"/messages",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("new post request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(experimentUserHeader, userID)
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatalf("post message: %v", err)
	}
	defer response.Body.Close()
	var result postMessageResponse
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated {
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatalf("decode post response: %v", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, response.Body)
	}
	return response.StatusCode, result
}

type sseEvent struct {
	ID   string
	Type string
	Data string
}

type testStream struct {
	response *http.Response
	reader   *bufio.Reader
	cancel   context.CancelFunc
}

func (fixture *testFixture) openStream(
	t *testing.T,
	userID string,
	channelID string,
	lastEventID string,
) (*testStream, int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fixture.server.URL+"/channels/"+channelID+"/events",
		nil,
	)
	if err != nil {
		cancel()
		t.Fatalf("new stream request: %v", err)
	}
	request.Header.Set(experimentUserHeader, userID)
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		cancel()
		t.Fatalf("open stream: %v", err)
	}
	stream := &testStream{
		response: response,
		reader:   bufio.NewReader(response.Body),
		cancel:   cancel,
	}
	t.Cleanup(stream.Close)
	return stream, response.StatusCode
}

func (stream *testStream) Close() {
	stream.cancel()
	_ = stream.response.Body.Close()
}

func (stream *testStream) readEvent(t *testing.T) sseEvent {
	t.Helper()
	type result struct {
		event sseEvent
		err   error
	}
	resultChannel := make(chan result, 1)
	go func() {
		event, err := readSSEEvent(stream.reader)
		resultChannel <- result{event: event, err: err}
	}()
	select {
	case got := <-resultChannel:
		if got.err != nil {
			t.Fatalf("read SSE event: %v", got.err)
		}
		return got.event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE event")
		return sseEvent{}
	}
}

func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var event sseEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return sseEvent{}, err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if event.Type != "" || event.Data != "" || event.ID != "" {
				return event, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			event.ID = value
		case "event":
			event.Type = value
		case "data":
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += value
		}
	}
}

func TestMessageCommandIdempotency(t *testing.T) {
	fixture := newTestFixture(t, 16)
	fixture.gate.Set(testWriter, testChannel, AccessDecision{Read: true, Post: true})

	status, first := fixture.postMessage(t, testWriter, testChannel, "send-1", "hello")
	if status != http.StatusCreated || !first.Created {
		t.Fatalf("first post = status %d, created %t; want 201, true", status, first.Created)
	}
	status, duplicate := fixture.postMessage(t, testWriter, testChannel, "send-1", "hello")
	if status != http.StatusOK || duplicate.Created || duplicate.Message.ID != first.Message.ID {
		t.Fatalf("duplicate = status %d, created %t, id %q; want 200, false, %q", status, duplicate.Created, duplicate.Message.ID, first.Message.ID)
	}
	status, _ = fixture.postMessage(t, testWriter, testChannel, "send-1", "changed")
	if status != http.StatusConflict {
		t.Fatalf("changed idempotent payload status = %d; want 409", status)
	}

	fixture.gate.Set(testReader, testChannel, AccessDecision{Read: true, Post: true})
	status, secondAuthor := fixture.postMessage(t, testReader, testChannel, "send-1", "hello")
	if status != http.StatusCreated || secondAuthor.Message.ID == first.Message.ID {
		t.Fatalf("second author = status %d, id %q; want 201 and a distinct ID", status, secondAuthor.Message.ID)
	}

	secondChannel := "chn_private"
	fixture.gate.Set(testWriter, secondChannel, AccessDecision{Read: true, Post: true})
	status, secondChannelMessage := fixture.postMessage(t, testWriter, secondChannel, "send-1", "hello")
	if status != http.StatusCreated || secondChannelMessage.Message.ID == first.Message.ID {
		t.Fatalf("second channel = status %d, id %q; want 201 and a globally distinct ID", status, secondChannelMessage.Message.ID)
	}

	fixture.gate.Set(testWriter, testChannel, AccessDecision{})
	status, _ = fixture.postMessage(t, testWriter, testChannel, "send-1", "hello")
	if status != http.StatusNotFound {
		t.Fatalf("retry after access revocation status = %d; want 404", status)
	}
}

func TestSSEReconnectReplaysFromLastEventID(t *testing.T) {
	fixture := newTestFixture(t, 16)
	fixture.gate.Set(testWriter, testChannel, AccessDecision{Read: true, Post: true})

	stream, status := fixture.openStream(t, testWriter, testChannel, "")
	if status != http.StatusOK {
		t.Fatalf("initial stream status = %d; want 200", status)
	}
	ready := stream.readEvent(t)
	if ready.Type != "ready" || ready.ID != "process-a:0" {
		t.Fatalf("ready event = %#v; want ready at process-a:0", ready)
	}

	status, _ = fixture.postMessage(t, testWriter, testChannel, "send-1", "first")
	if status != http.StatusCreated {
		t.Fatalf("first message status = %d; want 201", status)
	}
	firstEvent := stream.readEvent(t)
	if firstEvent.Type != "message.created" || firstEvent.ID != "process-a:1" {
		t.Fatalf("first event = %#v; want message.created at process-a:1", firstEvent)
	}
	if strings.Contains(firstEvent.Data, "client_operation_id") || strings.Contains(firstEvent.Data, "cursor") {
		t.Fatalf("realtime projection leaked command or cursor fields: %s", firstEvent.Data)
	}
	stream.Close()

	status, second := fixture.postMessage(t, testWriter, testChannel, "send-2", "second")
	if status != http.StatusCreated {
		t.Fatalf("second message status = %d; want 201", status)
	}
	reconnected, status := fixture.openStream(t, testWriter, testChannel, firstEvent.ID)
	if status != http.StatusOK {
		t.Fatalf("reconnected stream status = %d; want 200", status)
	}
	replayed := reconnected.readEvent(t)
	if replayed.Type != "message.created" || replayed.ID != "process-a:2" {
		t.Fatalf("replayed event = %#v; want second message at process-a:2", replayed)
	}
	var message Message
	if err := json.Unmarshal([]byte(replayed.Data), &message); err != nil {
		t.Fatalf("decode replayed message: %v", err)
	}
	if message.ID != second.Message.ID || message.Body != "second" {
		t.Fatalf("replayed message = %#v; want %#v", message, second.Message)
	}
}

func TestPermissionRevocationClosesIdleStreamWithoutLeaking(t *testing.T) {
	fixture := newTestFixture(t, 16)
	fixture.gate.Set(testWriter, testChannel, AccessDecision{Read: true, Post: true})
	fixture.gate.Set(testReader, testChannel, AccessDecision{Read: true})

	stream, status := fixture.openStream(t, testReader, testChannel, "")
	if status != http.StatusOK {
		t.Fatalf("reader stream status = %d; want 200", status)
	}
	_ = stream.readEvent(t)
	fixture.gate.Set(testReader, testChannel, AccessDecision{})
	revoked := stream.readEvent(t)
	if revoked.Type != "access-revoked" || revoked.ID != "" || revoked.Data != "{}" {
		t.Fatalf("revocation event = %#v; want content-free access-revoked", revoked)
	}

	status, _ = fixture.postMessage(t, testWriter, testChannel, "send-secret", "secret after revocation")
	if status != http.StatusCreated {
		t.Fatalf("post after revocation status = %d; want 201", status)
	}
	reconnect, status := fixture.openStream(t, testReader, testChannel, "process-a:0")
	defer reconnect.Close()
	if status != http.StatusNotFound {
		t.Fatalf("revoked reconnect status = %d; want 404", status)
	}
}

func TestExpiredCursorRequiresCanonicalResync(t *testing.T) {
	fixture := newTestFixture(t, 2)
	fixture.gate.Set(testWriter, testChannel, AccessDecision{Read: true, Post: true})

	stream, status := fixture.openStream(t, testWriter, testChannel, "")
	if status != http.StatusOK {
		t.Fatalf("initial stream status = %d; want 200", status)
	}
	ready := stream.readEvent(t)
	stream.Close()
	for index := 1; index <= 3; index++ {
		status, _ = fixture.postMessage(
			t,
			testWriter,
			testChannel,
			"send-"+strconv.Itoa(index),
			fmt.Sprintf("message %d", index),
		)
		if status != http.StatusCreated {
			t.Fatalf("message %d status = %d; want 201", index, status)
		}
	}

	reconnected, status := fixture.openStream(t, testWriter, testChannel, ready.ID)
	if status != http.StatusOK {
		t.Fatalf("expired reconnect status = %d; want 200 with a control event", status)
	}
	control := reconnected.readEvent(t)
	if control.Type != "resync-required" || control.ID != "" || control.Data != "{}" {
		t.Fatalf("resync event = %#v; want content-free resync-required", control)
	}
}

func TestFutureCursorRequiresCanonicalResync(t *testing.T) {
	fixture := newTestFixture(t, 2)
	fixture.gate.Set(testWriter, testChannel, AccessDecision{Read: true, Post: true})

	stream, status := fixture.openStream(t, testWriter, testChannel, "process-a:1")
	if status != http.StatusOK {
		t.Fatalf("future cursor status = %d; want 200 with a control event", status)
	}
	control := stream.readEvent(t)
	if control.Type != "resync-required" || control.ID != "" || control.Data != "{}" {
		t.Fatalf("future cursor control = %#v; want content-free resync-required", control)
	}
}

func TestStaleProcessGenerationRequiresCanonicalResync(t *testing.T) {
	fixture := newTestFixture(t, 2)
	fixture.gate.Set(testWriter, testChannel, AccessDecision{Read: true, Post: true})

	stream, status := fixture.openStream(t, testWriter, testChannel, "process-before-restart:0")
	if status != http.StatusOK {
		t.Fatalf("stale generation status = %d; want 200 with a control event", status)
	}
	control := stream.readEvent(t)
	if control.Type != "resync-required" || control.ID != "" || control.Data != "{}" {
		t.Fatalf("stale generation control = %#v; want content-free resync-required", control)
	}
}

func TestSlowSubscriptionDoesNotBlockPublish(t *testing.T) {
	gate := NewAccessGate()
	gate.Set(testWriter, testChannel, AccessDecision{Read: true, Post: true})
	hub, err := NewHub("process-a", 8, gate)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	_, cancel := hub.Watch(testChannel)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		for index := 0; index < 1000; index++ {
			_, _, publishErr := hub.Publish(PublishInput{
				ChannelID:         testChannel,
				AuthorID:          testWriter,
				ClientOperationID: "send-" + strconv.Itoa(index),
				Body:              "message",
			})
			if publishErr != nil {
				done <- publishErr
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("publish with slow subscriber: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked behind a slow subscriber")
	}
}
