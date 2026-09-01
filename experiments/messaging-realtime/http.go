package messagingrealtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	experimentUserHeader = "X-Experiment-User"
	maxCommandBytes      = MaxMessageBodyBytes + 1024
)

type HTTPHandler struct {
	hub       *Hub
	heartbeat time.Duration
	mux       *http.ServeMux
}

func NewHTTPHandler(hub *Hub, heartbeat time.Duration) (*HTTPHandler, error) {
	if hub == nil || heartbeat <= 0 {
		return nil, fmt.Errorf("%w: hub and heartbeat are required", ErrInvalid)
	}
	handler := &HTTPHandler{
		hub:       hub,
		heartbeat: heartbeat,
		mux:       http.NewServeMux(),
	}
	handler.mux.HandleFunc("POST /channels/{channel_id}/messages", handler.postMessage)
	handler.mux.HandleFunc("GET /channels/{channel_id}/events", handler.streamEvents)
	return handler, nil
}

func (handler *HTTPHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.mux.ServeHTTP(response, request)
}

type postMessageRequest struct {
	ClientOperationID string `json:"client_operation_id"`
	Body              string `json:"body"`
}

func (handler *HTTPHandler) postMessage(response http.ResponseWriter, request *http.Request) {
	userID := request.Header.Get(experimentUserHeader)
	channelID := request.PathValue("channel_id")
	if !validExperimentPrincipal(userID) || !validExperimentChannel(channelID) {
		writeProblem(response, http.StatusBadRequest, "invalid_request")
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxCommandBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var command postMessageRequest
	if err := decoder.Decode(&command); err != nil {
		writeProblem(response, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeProblem(response, http.StatusBadRequest, "invalid_request")
		return
	}

	message, created, err := handler.hub.Publish(PublishInput{
		ChannelID:         channelID,
		AuthorID:          userID,
		ClientOperationID: command.ClientOperationID,
		Body:              command.Body,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeProblem(response, http.StatusNotFound, "not_found")
		case errors.Is(err, ErrForbidden):
			writeProblem(response, http.StatusForbidden, "forbidden")
		case errors.Is(err, ErrConflict):
			writeProblem(response, http.StatusConflict, "idempotency_conflict")
		default:
			writeProblem(response, http.StatusBadRequest, "invalid_request")
		}
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(response, status, struct {
		Message Message `json:"message"`
		Created bool    `json:"created"`
	}{Message: message, Created: created})
}

func (handler *HTTPHandler) streamEvents(response http.ResponseWriter, request *http.Request) {
	userID := request.Header.Get(experimentUserHeader)
	channelID := request.PathValue("channel_id")
	if !validExperimentPrincipal(userID) || !validExperimentChannel(channelID) {
		writeProblem(response, http.StatusBadRequest, "invalid_request")
		return
	}
	if !handler.hub.CanRead(userID, channelID) {
		writeProblem(response, http.StatusNotFound, "not_found")
		return
	}

	eventUpdates, cancelEvents := handler.hub.Watch(channelID)
	defer cancelEvents()
	accessUpdates, cancelAccess := handler.hub.WatchAccess(channelID)
	defer cancelAccess()

	cursor := request.Header.Get("Last-Event-ID")
	initial := cursor == ""
	var pending []MessageEvent
	var err error
	if initial {
		cursor = handler.hub.CurrentCursor(channelID)
	} else {
		pending, err = handler.hub.Replay(userID, channelID, cursor)
		if err != nil && !errors.Is(err, ErrCursorResync) {
			if errors.Is(err, ErrNotFound) {
				writeProblem(response, http.StatusNotFound, "not_found")
			} else {
				writeProblem(response, http.StatusBadRequest, "invalid_cursor")
			}
			return
		}
	}
	if !handler.hub.CanRead(userID, channelID) {
		writeProblem(response, http.StatusNotFound, "not_found")
		return
	}

	flusher, ok := response.(http.Flusher)
	if !ok {
		writeProblem(response, http.StatusInternalServerError, "streaming_unsupported")
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)

	if errors.Is(err, ErrCursorResync) {
		_ = writeSSE(response, "", "resync-required", struct{}{})
		flusher.Flush()
		return
	}
	if initial {
		if err := writeSSE(response, cursor, "ready", struct{}{}); err != nil {
			return
		}
		flusher.Flush()
	}
	if nextCursor, ok := handler.writeEvents(response, flusher, userID, channelID, pending); !ok {
		return
	} else if nextCursor != "" {
		cursor = nextCursor
	}

	heartbeat := time.NewTicker(handler.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-accessUpdates:
			if !handler.hub.CanRead(userID, channelID) {
				_ = writeSSE(response, "", "access-revoked", struct{}{})
				flusher.Flush()
				return
			}
		case <-eventUpdates:
			events, replayErr := handler.hub.Replay(userID, channelID, cursor)
			if replayErr != nil {
				eventType := "resync-required"
				if errors.Is(replayErr, ErrNotFound) {
					eventType = "access-revoked"
				}
				_ = writeSSE(response, "", eventType, struct{}{})
				flusher.Flush()
				return
			}
			nextCursor, open := handler.writeEvents(response, flusher, userID, channelID, events)
			if !open {
				return
			}
			if nextCursor != "" {
				cursor = nextCursor
			}
		case <-heartbeat.C:
			if !handler.hub.CanRead(userID, channelID) {
				_ = writeSSE(response, "", "access-revoked", struct{}{})
				flusher.Flush()
				return
			}
			if _, err := io.WriteString(response, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (handler *HTTPHandler) writeEvents(
	response http.ResponseWriter,
	flusher http.Flusher,
	userID string,
	channelID string,
	events []MessageEvent,
) (string, bool) {
	cursor := ""
	for _, event := range events {
		if !handler.hub.CanRead(userID, channelID) {
			_ = writeSSE(response, "", "access-revoked", struct{}{})
			flusher.Flush()
			return "", false
		}
		if err := writeSSE(response, event.Cursor, event.Type, event.Message); err != nil {
			return "", false
		}
		flusher.Flush()
		cursor = event.Cursor
	}
	return cursor, true
}

func writeSSE(response io.Writer, id, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if id != "" {
		if _, err := fmt.Fprintf(response, "id: %s\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(response, "event: %s\ndata: %s\n\n", eventType, encoded); err != nil {
		return err
	}
	return nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func validExperimentPrincipal(userID string) bool {
	return validOpaqueASCII(userID, 128) && strings.HasPrefix(userID, "usr_")
}

func validExperimentChannel(channelID string) bool {
	return validOpaqueASCII(channelID, 128) && strings.HasPrefix(channelID, "chn_")
}

func writeProblem(response http.ResponseWriter, status int, code string) {
	writeJSON(response, status, struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}{Error: struct {
		Code string `json:"code"`
	}{Code: code}})
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(payload)
}
