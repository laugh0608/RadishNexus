package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/realtime"
)

const (
	channelEventsPattern = "/api/v1/workspaces/{workspace_id}/channels/{channel_id}/events"
	streamHeartbeat      = 15 * time.Second
	streamWriteTimeout   = 5 * time.Second
)

type ChannelEventsApplication interface {
	AuthorizeChannelRead(context.Context, authz.Principal, string) error
	GetChannelMessage(context.Context, authz.Principal, string, string) (goldenpath.MessageProjection, error)
}

type ChannelEventsHandler struct {
	sessions     WorkspaceSessionResolver
	messages     ChannelEventsApplication
	hub          *realtime.Hub
	session      BrowserSessionPolicy
	proxy        TrustedProxyPolicy
	heartbeat    time.Duration
	writeTimeout time.Duration
}

func NewChannelEventsHandler(
	sessions WorkspaceSessionResolver,
	messages ChannelEventsApplication,
	hub *realtime.Hub,
	session BrowserSessionPolicy,
	proxy TrustedProxyPolicy,
) http.Handler {
	return newChannelEventsHandler(sessions, messages, hub, session, proxy, streamHeartbeat, streamWriteTimeout)
}

func newChannelEventsHandler(
	sessions WorkspaceSessionResolver,
	messages ChannelEventsApplication,
	hub *realtime.Hub,
	session BrowserSessionPolicy,
	proxy TrustedProxyPolicy,
	heartbeat time.Duration,
	writeTimeout time.Duration,
) http.Handler {
	handler := &ChannelEventsHandler{
		sessions: sessions, messages: messages, hub: hub, session: session, proxy: proxy,
		heartbeat: heartbeat, writeTimeout: writeTimeout,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+channelEventsPattern, handler.get)
	mux.HandleFunc(channelEventsPattern, handler.methodNotAllowed)
	mux.HandleFunc("/api/v1/workspaces/", handler.notFound)
	mux.HandleFunc("/api/v1/workspaces", handler.notFound)
	return privateNoStore(mux)
}

func (handler *ChannelEventsHandler) get(response http.ResponseWriter, request *http.Request) {
	workspaceID := request.PathValue("workspace_id")
	channelID := request.PathValue("channel_id")
	if _, err := handler.proxy.ClientIP(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := handler.session.ValidateHost(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	token, err := handler.session.SessionToken(request)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := validateMessagingPath(workspaceID, channelID, ""); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if request.URL.RawQuery != "" {
		handler.writeError(response, request, fmt.Errorf("%w: query parameters are not supported", authz.ErrInvalid))
		return
	}
	principal, err := handler.authorize(request.Context(), token, workspaceID, channelID, "")
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	lastEventID, valid := singleLastEventID(request)
	if !valid {
		handler.writeResync(response, request)
		return
	}
	subscription, readyCursor, err := handler.hub.Subscribe(
		workspaceID, channelID, principal.ID, lastEventID,
	)
	if errors.Is(err, realtime.ErrResyncRequired) {
		handler.writeResync(response, request)
		return
	}
	if errors.Is(err, realtime.ErrCapacity) {
		handler.writeError(response, request, ErrRealtimeCapacity)
		return
	}
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	defer subscription.Close()

	controller := http.NewResponseController(response)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		handler.writeError(response, request, fmt.Errorf("disable Message event stream write deadline: %w", err))
		return
	}
	setEventStreamHeaders(response)
	response.WriteHeader(http.StatusOK)
	if err := handler.writeEvent(controller, response, "ready", readyCursor, struct{}{}); err != nil {
		return
	}
	if !handler.drain(request, controller, response, subscription, token, workspaceID, channelID, principal.ID) {
		return
	}

	heartbeat := time.NewTicker(handler.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-subscription.Wake():
			if !handler.drain(request, controller, response, subscription, token, workspaceID, channelID, principal.ID) {
				return
			}
		case <-heartbeat.C:
			if _, err := handler.authorize(request.Context(), token, workspaceID, channelID, principal.ID); err != nil {
				handler.closeForAuthorization(response, request, controller, err)
				return
			}
			if err := handler.writeHeartbeat(controller, response); err != nil {
				return
			}
		}
	}
}

func (handler *ChannelEventsHandler) drain(
	request *http.Request,
	controller *http.ResponseController,
	response http.ResponseWriter,
	subscription *realtime.Subscription,
	token string,
	workspaceID string,
	channelID string,
	userID string,
) bool {
	if _, err := handler.authorize(request.Context(), token, workspaceID, channelID, userID); err != nil {
		handler.closeForAuthorization(response, request, controller, err)
		return false
	}
	events, err := subscription.Drain()
	if errors.Is(err, realtime.ErrResyncRequired) {
		_ = handler.writeEvent(controller, response, "resync-required", "", struct{}{})
		return false
	}
	if errors.Is(err, realtime.ErrClosed) {
		return false
	}
	if err != nil {
		log.Printf("drain Message event stream request_id=%s: %v", RequestID(request.Context()), err)
		return false
	}
	for _, event := range events {
		principal, err := handler.authorize(request.Context(), token, workspaceID, channelID, userID)
		if err != nil {
			handler.closeForAuthorization(response, request, controller, err)
			return false
		}
		message, err := handler.messages.GetChannelMessage(request.Context(), principal, channelID, event.MessageID)
		if errors.Is(err, authz.ErrNotFound) {
			if _, accessErr := handler.authorize(request.Context(), token, workspaceID, channelID, userID); accessErr != nil {
				handler.closeForAuthorization(response, request, controller, accessErr)
				return false
			}
			continue
		}
		if err != nil {
			log.Printf("load Message event projection request_id=%s: %v", RequestID(request.Context()), err)
			return false
		}
		dto, err := publicMessageProjection(channelID, message)
		if err != nil {
			log.Printf("validate Message event projection request_id=%s: %v", RequestID(request.Context()), err)
			return false
		}
		if err := handler.writeEvent(controller, response, "message.created", event.Cursor, messageResponse{Data: dto}); err != nil {
			return false
		}
	}
	return true
}

func (handler *ChannelEventsHandler) authorize(
	ctx context.Context,
	token string,
	workspaceID string,
	channelID string,
	expectedUserID string,
) (authz.Principal, error) {
	verified, err := handler.sessions.ResolveWorkspace(ctx, token, workspaceID)
	if err != nil {
		if errors.Is(err, authz.ErrForbidden) {
			err = authz.ErrNotFound
		}
		return authz.Principal{}, err
	}
	principal, err := authn.UserPrincipal(verified)
	if err != nil {
		return authz.Principal{}, err
	}
	if expectedUserID != "" && principal.ID != expectedUserID {
		return authz.Principal{}, authz.ErrUnauthenticated
	}
	if err := handler.messages.AuthorizeChannelRead(ctx, principal, channelID); err != nil {
		return authz.Principal{}, err
	}
	return principal, nil
}

func (handler *ChannelEventsHandler) closeForAuthorization(
	response http.ResponseWriter,
	request *http.Request,
	controller *http.ResponseController,
	err error,
) {
	if isAccessRevoked(err) {
		_ = handler.writeEvent(controller, response, "access-revoked", "", struct{}{})
		return
	}
	log.Printf("authorize Message event stream request_id=%s: %v", RequestID(request.Context()), err)
}

func (handler *ChannelEventsHandler) writeResync(response http.ResponseWriter, request *http.Request) {
	controller := http.NewResponseController(response)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		handler.writeError(response, request, fmt.Errorf("disable Message resync write deadline: %w", err))
		return
	}
	setEventStreamHeaders(response)
	response.WriteHeader(http.StatusOK)
	_ = handler.writeEvent(controller, response, "resync-required", "", struct{}{})
}

func (handler *ChannelEventsHandler) writeEvent(
	controller *http.ResponseController,
	response http.ResponseWriter,
	event string,
	id string,
	data any,
) error {
	if strings.ContainsAny(event, "\r\n") || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("invalid Message event framing")
	}
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal Message event: %w", err)
	}
	if err := controller.SetWriteDeadline(time.Now().Add(handler.writeTimeout)); err != nil {
		return err
	}
	if id != "" {
		if _, err := fmt.Fprintf(response, "id: %s\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event, body); err != nil {
		return err
	}
	if err := controller.Flush(); err != nil {
		return err
	}
	return controller.SetWriteDeadline(time.Time{})
}

func (handler *ChannelEventsHandler) writeHeartbeat(
	controller *http.ResponseController,
	response http.ResponseWriter,
) error {
	if err := controller.SetWriteDeadline(time.Now().Add(handler.writeTimeout)); err != nil {
		return err
	}
	if _, err := fmt.Fprint(response, ": heartbeat\n\n"); err != nil {
		return err
	}
	if err := controller.Flush(); err != nil {
		return err
	}
	return controller.SetWriteDeadline(time.Time{})
}

func (handler *ChannelEventsHandler) methodNotAllowed(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Allow", http.MethodGet)
	handler.writeError(response, request, ErrMethodNotAllowed)
}

func (handler *ChannelEventsHandler) notFound(response http.ResponseWriter, request *http.Request) {
	handler.writeError(response, request, authz.ErrNotFound)
}

func (handler *ChannelEventsHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	if MapApplicationError(err).StatusCode == http.StatusInternalServerError {
		log.Printf("public Channel event stream failed request_id=%s: %v", RequestID(request.Context()), err)
	}
	if writeErr := WriteError(response, RequestID(request.Context()), err); writeErr != nil {
		log.Printf("write public Channel event stream error: %v", writeErr)
	}
}

func singleLastEventID(request *http.Request) (string, bool) {
	values := request.Header.Values("Last-Event-ID")
	if len(values) == 0 {
		return "", true
	}
	if len(values) != 1 || values[0] == "" || len(values[0]) > realtime.MaxCursorBytes {
		return "", false
	}
	return values[0], true
}

func setEventStreamHeaders(response http.ResponseWriter) {
	response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	response.Header().Set("Cache-Control", "private, no-store")
	response.Header().Set("Vary", "Cookie")
	response.Header().Set("X-Content-Type-Options", "nosniff")
}

func isAccessRevoked(err error) bool {
	return errors.Is(err, authz.ErrUnauthenticated) || errors.Is(err, authz.ErrForbidden) ||
		errors.Is(err, authz.ErrNotFound) || errors.Is(err, authn.ErrInvalidSession)
}
