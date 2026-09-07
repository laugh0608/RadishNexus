package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	channelMessagesPattern    = "/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages"
	messageThreadPattern      = "/api/v1/workspaces/{workspace_id}/channels/{channel_id}/messages/{message_id}/threads"
	defaultMessagePageSize    = 50
	maxPublicMessageCursorLen = 512
	maxCreateMessageBodyBytes = 128 * 1024
	maxStartThreadBodyBytes   = 8 * 1024
)

type MessagingSessionService interface {
	WorkspaceSessionResolver
	VerifyCSRF(context.Context, string, string) error
}

type ChannelMessagingApplication interface {
	ListChannelMessages(context.Context, authz.Principal, goldenpath.ListChannelMessagesInput) (goldenpath.MessagePage, error)
	CreateMessage(context.Context, goldenpath.Invocation, goldenpath.CreateMessageInput) (goldenpath.CreateMessageResult, error)
	StartThreadFromMessage(context.Context, goldenpath.Invocation, goldenpath.StartThreadFromMessageInput) (goldenpath.Thread, error)
}

type ChannelMessagesHandler struct {
	sessions  MessagingSessionService
	messaging ChannelMessagingApplication
	session   BrowserSessionPolicy
	proxy     TrustedProxyPolicy
}

type createMessageRequest struct {
	ClientOperationID string  `json:"client_operation_id"`
	Body              string  `json:"body"`
	ThreadID          *string `json:"thread_id"`
}

type startThreadRequest struct {
	Title      string `json:"title"`
	Visibility string `json:"visibility"`
}

type messageResponse struct {
	Data messageDTO `json:"data"`
}

type messagePageResponse struct {
	Data messagePageDTO `json:"data"`
}

type messagePageDTO struct {
	Messages    []messageDTO `json:"messages"`
	OlderCursor *string      `json:"older_cursor"`
}

type messageDTO struct {
	Ref       entityRefDTO       `json:"ref"`
	Channel   entityRefDTO       `json:"channel"`
	Thread    *entityRefDTO      `json:"thread"`
	Author    deploymentActorDTO `json:"author"`
	Body      string             `json:"body"`
	CreatedAt string             `json:"created_at"`
}

type threadResponse struct {
	Data threadDTO `json:"data"`
}

type threadDTO struct {
	Ref           entityRefDTO `json:"ref"`
	Channel       entityRefDTO `json:"channel"`
	SourceMessage entityRefDTO `json:"source_message"`
	Title         string       `json:"title"`
	Visibility    string       `json:"visibility"`
	CreatedAt     string       `json:"created_at"`
}

type publicMessageCursor struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"created_at"`
	MessageID string `json:"message_id"`
}

func NewChannelMessagesHandler(
	sessions MessagingSessionService,
	messaging ChannelMessagingApplication,
	session BrowserSessionPolicy,
	proxy TrustedProxyPolicy,
) http.Handler {
	handler := &ChannelMessagesHandler{
		sessions:  sessions,
		messaging: messaging,
		session:   session,
		proxy:     proxy,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+channelMessagesPattern, handler.list)
	mux.HandleFunc("POST "+channelMessagesPattern, handler.createMessage)
	mux.HandleFunc("POST "+messageThreadPattern, handler.startThread)
	mux.HandleFunc(channelMessagesPattern, handler.methodNotAllowed)
	mux.HandleFunc(messageThreadPattern, handler.methodNotAllowed)
	mux.HandleFunc("/api/v1/workspaces/", handler.notFound)
	mux.HandleFunc("/api/v1/workspaces", handler.notFound)
	return privateNoStore(mux)
}

func (handler *ChannelMessagesHandler) list(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		handler.methodNotAllowed(response, request)
		return
	}
	workspaceID := request.PathValue("workspace_id")
	channelID := request.PathValue("channel_id")
	principal, err := handler.authenticate(request, workspaceID, false)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := validateMessagingPath(workspaceID, channelID, ""); err != nil {
		handler.writeError(response, request, err)
		return
	}
	input, err := parseMessagePageQuery(request.URL.RawQuery, channelID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	page, err := handler.messaging.ListChannelMessages(request.Context(), principal, input)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	dto, err := publicMessagePage(channelID, input.Limit, page)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	handler.writeJSON(response, request, http.StatusOK, messagePageResponse{Data: dto})
}

func (handler *ChannelMessagesHandler) createMessage(response http.ResponseWriter, request *http.Request) {
	workspaceID := request.PathValue("workspace_id")
	channelID := request.PathValue("channel_id")
	principal, err := handler.authenticate(request, workspaceID, true)
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
	var body createMessageRequest
	if err := decodeJSON(response, request, &body, maxCreateMessageBodyBytes); err != nil {
		handler.writeError(response, request, err)
		return
	}
	threadID := ""
	if body.ThreadID != nil {
		threadID = *body.ThreadID
	}
	result, err := handler.messaging.CreateMessage(request.Context(), webInvocation(principal, request), goldenpath.CreateMessageInput{
		ChannelID:         channelID,
		ThreadID:          threadID,
		ClientOperationID: body.ClientOperationID,
		Body:              body.Body,
	})
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	dto, err := publicCreatedMessage(workspaceID, channelID, principal, body, result.Message)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	handler.writeJSON(response, request, status, messageResponse{Data: dto})
}

func (handler *ChannelMessagesHandler) startThread(response http.ResponseWriter, request *http.Request) {
	workspaceID := request.PathValue("workspace_id")
	channelID := request.PathValue("channel_id")
	messageID := request.PathValue("message_id")
	principal, err := handler.authenticate(request, workspaceID, true)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := validateMessagingPath(workspaceID, channelID, messageID); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if request.URL.RawQuery != "" {
		handler.writeError(response, request, fmt.Errorf("%w: query parameters are not supported", authz.ErrInvalid))
		return
	}
	var body startThreadRequest
	if err := decodeJSON(response, request, &body, maxStartThreadBodyBytes); err != nil {
		handler.writeError(response, request, err)
		return
	}
	thread, err := handler.messaging.StartThreadFromMessage(
		request.Context(),
		webInvocation(principal, request),
		goldenpath.StartThreadFromMessageInput{
			ChannelID:  channelID,
			MessageID:  messageID,
			Title:      body.Title,
			Visibility: body.Visibility,
		},
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	dto, err := publicStartedThread(workspaceID, channelID, messageID, principal, body, thread)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	handler.writeJSON(response, request, http.StatusCreated, threadResponse{Data: dto})
}

func (handler *ChannelMessagesHandler) authenticate(
	request *http.Request,
	workspaceID string,
	write bool,
) (authz.Principal, error) {
	if _, err := handler.proxy.ClientIP(request); err != nil {
		return authz.Principal{}, err
	}
	if err := handler.session.ValidateHost(request); err != nil {
		return authz.Principal{}, err
	}
	token, err := handler.session.SessionToken(request)
	if err != nil {
		return authz.Principal{}, err
	}
	if write {
		csrfToken, err := handler.session.ValidateCSRF(request)
		if err != nil {
			return authz.Principal{}, err
		}
		if err := handler.sessions.VerifyCSRF(request.Context(), token, csrfToken); err != nil {
			return authz.Principal{}, err
		}
	}
	if !validScopedID(workspaceID, "wrk_") {
		return authz.Principal{}, fmt.Errorf("%w: invalid Workspace ID", authz.ErrInvalid)
	}
	verified, err := handler.sessions.ResolveWorkspace(request.Context(), token, workspaceID)
	if err != nil {
		if errors.Is(err, authz.ErrForbidden) {
			err = authz.ErrNotFound
		}
		return authz.Principal{}, err
	}
	return authn.UserPrincipal(verified)
}

func validateMessagingPath(workspaceID string, channelID string, messageID string) error {
	if !validScopedID(workspaceID, "wrk_") {
		return fmt.Errorf("%w: invalid Workspace ID", authz.ErrInvalid)
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "channel", ID: channelID}); err != nil {
		return fmt.Errorf("%w: invalid Channel ID", authz.ErrInvalid)
	}
	if messageID != "" {
		if err := entityref.M0Registry().Validate(entityref.Ref{Type: "message", ID: messageID}); err != nil {
			return fmt.Errorf("%w: invalid Message ID", authz.ErrInvalid)
		}
	}
	return nil
}

func parseMessagePageQuery(rawQuery string, channelID string) (goldenpath.ListChannelMessagesInput, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return goldenpath.ListChannelMessagesInput{}, fmt.Errorf("%w: malformed query", authz.ErrInvalid)
	}
	for key := range query {
		if key != "limit" && key != "before" {
			return goldenpath.ListChannelMessagesInput{}, fmt.Errorf("%w: unsupported query parameter", authz.ErrInvalid)
		}
	}
	input := goldenpath.ListChannelMessagesInput{ChannelID: channelID, Limit: defaultMessagePageSize}
	if values, exists := query["limit"]; exists {
		if len(values) != 1 || values[0] == "" {
			return goldenpath.ListChannelMessagesInput{}, fmt.Errorf("%w: one Message page limit is required", authz.ErrInvalid)
		}
		limit, err := strconv.Atoi(values[0])
		if err != nil || strconv.Itoa(limit) != values[0] || limit < 1 || limit > goldenpath.MaxMessagePageSize {
			return goldenpath.ListChannelMessagesInput{}, fmt.Errorf("%w: invalid Message page limit", authz.ErrInvalid)
		}
		input.Limit = limit
	}
	if values, exists := query["before"]; exists {
		if len(values) != 1 || values[0] == "" {
			return goldenpath.ListChannelMessagesInput{}, fmt.Errorf("%w: one Message cursor is required", authz.ErrInvalid)
		}
		cursor, err := decodePublicMessageCursor(values[0])
		if err != nil {
			return goldenpath.ListChannelMessagesInput{}, err
		}
		input.Before = &cursor
	}
	return input, nil
}

func encodePublicMessageCursor(cursor goldenpath.MessagePageCursor) (string, error) {
	if cursor.CreatedAt.IsZero() {
		return "", fmt.Errorf("invalid Message cursor time")
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "message", ID: cursor.MessageID}); err != nil {
		return "", fmt.Errorf("invalid Message cursor ID: %w", err)
	}
	body, err := json.Marshal(publicMessageCursor{
		Version:   1,
		CreatedAt: publicTime(cursor.CreatedAt),
		MessageID: cursor.MessageID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal Message cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func decodePublicMessageCursor(value string) (goldenpath.MessagePageCursor, error) {
	invalid := func() (goldenpath.MessagePageCursor, error) {
		return goldenpath.MessagePageCursor{}, fmt.Errorf("%w: invalid Message cursor", authz.ErrInvalid)
	}
	if len(value) > maxPublicMessageCursorLen {
		return invalid()
	}
	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return invalid()
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	var public publicMessageCursor
	if err := decoder.Decode(&public); err != nil {
		return invalid()
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return invalid()
	}
	createdAt, err := time.Parse(time.RFC3339Nano, public.CreatedAt)
	if err != nil || public.Version != 1 || publicTime(createdAt) != public.CreatedAt {
		return invalid()
	}
	cursor := goldenpath.MessagePageCursor{CreatedAt: createdAt.UTC(), MessageID: public.MessageID}
	canonical, err := encodePublicMessageCursor(cursor)
	if err != nil || canonical != value {
		return invalid()
	}
	return cursor, nil
}

func publicMessagePage(channelID string, limit int, page goldenpath.MessagePage) (messagePageDTO, error) {
	if len(page.Messages) > limit {
		return messagePageDTO{}, fmt.Errorf("Message page exceeds requested limit")
	}
	dto := messagePageDTO{Messages: make([]messageDTO, 0, len(page.Messages))}
	var previous goldenpath.MessageProjection
	hasPrevious := false
	for index := range page.Messages {
		message := page.Messages[index]
		item, err := publicMessageProjection(channelID, message)
		if err != nil {
			return messagePageDTO{}, err
		}
		if hasPrevious && (message.CreatedAt.Before(previous.CreatedAt) ||
			(message.CreatedAt.Equal(previous.CreatedAt) && message.ID <= previous.ID)) {
			return messagePageDTO{}, fmt.Errorf("invalid Message page order")
		}
		dto.Messages = append(dto.Messages, item)
		previous = message
		hasPrevious = true
	}
	if page.OlderCursor != nil {
		if len(page.Messages) == 0 || !page.OlderCursor.CreatedAt.Equal(page.Messages[0].CreatedAt) ||
			page.OlderCursor.MessageID != page.Messages[0].ID {
			return messagePageDTO{}, fmt.Errorf("Message page cursor does not match oldest Message")
		}
		cursor, err := encodePublicMessageCursor(*page.OlderCursor)
		if err != nil {
			return messagePageDTO{}, err
		}
		dto.OlderCursor = &cursor
	}
	return dto, nil
}

func publicMessageProjection(channelID string, message goldenpath.MessageProjection) (messageDTO, error) {
	if message.ChannelID != channelID || message.CreatedAt.IsZero() || !validPublicMessageBody(message.Body) {
		return messageDTO{}, fmt.Errorf("invalid public Message projection")
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "message", ID: message.ID}); err != nil {
		return messageDTO{}, fmt.Errorf("invalid public Message ID: %w", err)
	}
	if !validScopedID(message.AuthorID, "usr_") {
		return messageDTO{}, fmt.Errorf("invalid public Message author")
	}
	var thread *entityRefDTO
	if message.ThreadID != nil {
		if err := entityref.M0Registry().Validate(entityref.Ref{Type: "thread", ID: *message.ThreadID}); err != nil {
			return messageDTO{}, fmt.Errorf("invalid public Message Thread: %w", err)
		}
		ref := publicRef(entityref.Ref{Type: "thread", ID: *message.ThreadID})
		thread = &ref
	}
	return messageDTO{
		Ref:       publicRef(entityref.Ref{Type: "message", ID: message.ID}),
		Channel:   publicRef(entityref.Ref{Type: "channel", ID: message.ChannelID}),
		Thread:    thread,
		Author:    deploymentActorDTO{Kind: "user", ID: message.AuthorID},
		Body:      message.Body,
		CreatedAt: publicTime(message.CreatedAt),
	}, nil
}

func publicCreatedMessage(
	workspaceID string,
	channelID string,
	principal authz.Principal,
	request createMessageRequest,
	message goldenpath.Message,
) (messageDTO, error) {
	threadID := ""
	if request.ThreadID != nil {
		threadID = *request.ThreadID
	}
	if message.WorkspaceID != workspaceID || message.ChannelID != channelID || message.AuthorID != principal.ID ||
		message.ClientOperationID != request.ClientOperationID || message.Body != request.Body || message.CreatedAt.IsZero() {
		return messageDTO{}, fmt.Errorf("created Message does not match request scope")
	}
	if (message.ThreadID == nil && threadID != "") ||
		(message.ThreadID != nil && *message.ThreadID != threadID) {
		return messageDTO{}, fmt.Errorf("created Message Thread does not match request")
	}
	return publicMessageProjection(channelID, goldenpath.MessageProjection{
		ID:        message.ID,
		ChannelID: message.ChannelID,
		ThreadID:  message.ThreadID,
		AuthorID:  message.AuthorID,
		Body:      message.Body,
		CreatedAt: message.CreatedAt,
	})
}

func publicStartedThread(
	workspaceID string,
	channelID string,
	messageID string,
	principal authz.Principal,
	request startThreadRequest,
	thread goldenpath.Thread,
) (threadDTO, error) {
	title := strings.TrimSpace(request.Title)
	if thread.WorkspaceID != workspaceID || thread.OriginChannelID == nil || *thread.OriginChannelID != channelID ||
		thread.CreatedBy != principal.ID || title == "" || thread.Title != title ||
		!utf8.ValidString(thread.Title) || strings.ContainsRune(thread.Title, '\x00') ||
		thread.Visibility != request.Visibility || thread.CreatedAt.IsZero() ||
		!thread.UpdatedAt.Equal(thread.CreatedAt) ||
		(thread.Visibility != "project" && thread.Visibility != "restricted") {
		return threadDTO{}, fmt.Errorf("created Thread does not match request scope")
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "project", ID: thread.GoverningProjectID}); err != nil {
		return threadDTO{}, fmt.Errorf("invalid created Thread Project: %w", err)
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "thread", ID: thread.ID}); err != nil {
		return threadDTO{}, fmt.Errorf("invalid created Thread ID: %w", err)
	}
	return threadDTO{
		Ref:           publicRef(entityref.Ref{Type: "thread", ID: thread.ID}),
		Channel:       publicRef(entityref.Ref{Type: "channel", ID: channelID}),
		SourceMessage: publicRef(entityref.Ref{Type: "message", ID: messageID}),
		Title:         thread.Title,
		Visibility:    thread.Visibility,
		CreatedAt:     publicTime(thread.CreatedAt),
	}, nil
}

func validPublicMessageBody(body string) bool {
	return utf8.ValidString(body) && !strings.ContainsRune(body, '\x00') &&
		strings.TrimSpace(body) != "" && len(body) <= goldenpath.MaxMessageBodyBytes
}

func webInvocation(principal authz.Principal, request *http.Request) goldenpath.Invocation {
	return goldenpath.Invocation{
		Principal:     principal,
		SourceKind:    "web",
		CorrelationID: RequestID(request.Context()),
	}
}

func (handler *ChannelMessagesHandler) methodNotAllowed(response http.ResponseWriter, request *http.Request) {
	if request.PathValue("message_id") == "" {
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	} else {
		response.Header().Set("Allow", http.MethodPost)
	}
	handler.writeError(response, request, ErrMethodNotAllowed)
}

func (handler *ChannelMessagesHandler) notFound(response http.ResponseWriter, request *http.Request) {
	handler.writeError(response, request, authz.ErrNotFound)
}

func (handler *ChannelMessagesHandler) writeJSON(
	response http.ResponseWriter,
	request *http.Request,
	status int,
	value any,
) {
	body, err := json.Marshal(value)
	if err != nil {
		handler.writeError(response, request, fmt.Errorf("marshal public Channel Message response: %w", err))
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if _, err := response.Write(append(body, '\n')); err != nil {
		log.Printf("write public Channel Message response request_id=%s: %v", RequestID(request.Context()), err)
	}
}

func (handler *ChannelMessagesHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	if MapApplicationError(err).StatusCode == http.StatusInternalServerError {
		log.Printf("public Channel Message request failed request_id=%s: %v", RequestID(request.Context()), err)
	}
	if writeErr := WriteError(response, RequestID(request.Context()), err); writeErr != nil {
		log.Printf("write public Channel Message error: %v", writeErr)
	}
}
