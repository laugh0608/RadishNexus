package goldenpath

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	MaxMessageBodyBytes       = 16 * 1024
	MaxClientOperationIDBytes = 128
	MaxMessagePageSize        = 100
)

type Message struct {
	ID                string
	WorkspaceID       string
	ChannelID         string
	ThreadID          *string
	AuthorID          string
	Body              string
	ClientOperationID string
	CreatedAt         time.Time
}

type CreateMessageResult struct {
	Message Message
	Created bool
}

// MessageProjection is the canonical readable Message shape. It deliberately
// excludes client_operation_id because idempotency state is private to the
// authoring command boundary.
type MessageProjection struct {
	ID        string
	ChannelID string
	ThreadID  *string
	AuthorID  string
	Body      string
	CreatedAt time.Time
}

// MessagePageCursor is an internal exclusive keyset boundary. A future
// transport must encode it as an opaque token rather than exposing its fields
// as a public cursor contract.
type MessagePageCursor struct {
	CreatedAt time.Time
	MessageID string
}

type MessagePage struct {
	Messages    []MessageProjection
	OlderCursor *MessagePageCursor
}

type Thread struct {
	ID                 string
	WorkspaceID        string
	GoverningProjectID string
	OriginChannelID    *string
	Title              string
	Visibility         string
	CreatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateMessageInput struct {
	ChannelID         string
	ThreadID          string
	ClientOperationID string
	Body              string
}

type ListChannelMessagesInput struct {
	ChannelID string
	Before    *MessagePageCursor
	Limit     int
}

type StartThreadFromMessageInput struct {
	ChannelID  string
	MessageID  string
	Title      string
	Visibility string
}

type CreateMessageCommand struct {
	Invocation
	MessageID         string
	EventID           string
	ChannelID         string
	ThreadID          string
	ClientOperationID string
	Body              string
	OccurredAt        time.Time
}

type StartThreadFromMessageCommand struct {
	Invocation
	ThreadID   string
	LinkID     string
	EventID    string
	ChannelID  string
	MessageID  string
	Title      string
	Visibility string
	OccurredAt time.Time
}

func (service *Service) CreateMessage(
	ctx context.Context,
	invocation Invocation,
	input CreateMessageInput,
) (CreateMessageResult, error) {
	if err := validateInvocation(invocation); err != nil {
		return CreateMessageResult{}, err
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{
		Type: "channel",
		ID:   input.ChannelID,
	}); err != nil {
		return CreateMessageResult{}, fmt.Errorf("%w: Channel reference: %v", authz.ErrInvalid, err)
	}
	if input.ThreadID != "" {
		if err := entityref.M0Registry().Validate(entityref.Ref{
			Type: "thread",
			ID:   input.ThreadID,
		}); err != nil {
			return CreateMessageResult{}, fmt.Errorf("%w: Thread reference: %v", authz.ErrInvalid, err)
		}
	}
	if !validClientOperationID(input.ClientOperationID) {
		return CreateMessageResult{}, fmt.Errorf(
			"%w: canonical client operation ID is required",
			authz.ErrInvalid,
		)
	}
	if !utf8.ValidString(input.Body) || strings.ContainsRune(input.Body, '\x00') ||
		strings.TrimSpace(input.Body) == "" || len(input.Body) > MaxMessageBodyBytes {
		return CreateMessageResult{}, fmt.Errorf(
			"%w: message body must be non-empty UTF-8 within %d bytes",
			authz.ErrInvalid,
			MaxMessageBodyBytes,
		)
	}

	messageID, err := service.ids.NewID("msg_")
	if err != nil {
		return CreateMessageResult{}, fmt.Errorf("generate Message ID: %w", err)
	}
	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return CreateMessageResult{}, fmt.Errorf("generate Message event ID: %w", err)
	}

	return service.store.CreateMessage(ctx, CreateMessageCommand{
		Invocation:        invocation,
		MessageID:         messageID,
		EventID:           eventID,
		ChannelID:         input.ChannelID,
		ThreadID:          input.ThreadID,
		ClientOperationID: input.ClientOperationID,
		Body:              input.Body,
		OccurredAt:        service.clock.Now().UTC(),
	})
}

func (service *Service) ListChannelMessages(
	ctx context.Context,
	principal authz.Principal,
	input ListChannelMessagesInput,
) (MessagePage, error) {
	if err := principal.ValidateUser(); err != nil {
		return MessagePage{}, err
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{
		Type: "channel",
		ID:   input.ChannelID,
	}); err != nil {
		return MessagePage{}, fmt.Errorf("%w: Channel reference: %v", authz.ErrInvalid, err)
	}
	if input.Limit < 1 || input.Limit > MaxMessagePageSize {
		return MessagePage{}, fmt.Errorf(
			"%w: Message page size must be between 1 and %d",
			authz.ErrInvalid,
			MaxMessagePageSize,
		)
	}
	if input.Before != nil {
		if input.Before.CreatedAt.IsZero() {
			return MessagePage{}, fmt.Errorf("%w: Message page cursor time is required", authz.ErrInvalid)
		}
		if err := entityref.M0Registry().Validate(entityref.Ref{
			Type: "message",
			ID:   input.Before.MessageID,
		}); err != nil {
			return MessagePage{}, fmt.Errorf("%w: Message page cursor ID: %v", authz.ErrInvalid, err)
		}
		cursor := *input.Before
		cursor.CreatedAt = cursor.CreatedAt.UTC()
		input.Before = &cursor
	}
	return service.store.ListChannelMessages(ctx, principal, input)
}

func (service *Service) StartThreadFromMessage(
	ctx context.Context,
	invocation Invocation,
	input StartThreadFromMessageInput,
) (Thread, error) {
	if err := validateInvocation(invocation); err != nil {
		return Thread{}, err
	}
	title := strings.TrimSpace(input.Title)
	if err := entityref.M0Registry().Validate(entityref.Ref{
		Type: "channel",
		ID:   input.ChannelID,
	}); err != nil {
		return Thread{}, fmt.Errorf("%w: Channel reference: %v", authz.ErrInvalid, err)
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{
		Type: "message",
		ID:   input.MessageID,
	}); err != nil {
		return Thread{}, fmt.Errorf("%w: Message reference: %v", authz.ErrInvalid, err)
	}
	if !utf8.ValidString(input.Title) || strings.ContainsRune(input.Title, '\x00') || title == "" ||
		(input.Visibility != "project" && input.Visibility != "restricted") {
		return Thread{}, fmt.Errorf(
			"%w: title and project or restricted visibility are required",
			authz.ErrInvalid,
		)
	}

	threadID, err := service.ids.NewID("thr_")
	if err != nil {
		return Thread{}, fmt.Errorf("generate Thread ID: %w", err)
	}
	linkID, err := service.ids.NewID("lnk_")
	if err != nil {
		return Thread{}, fmt.Errorf("generate Thread source link ID: %w", err)
	}
	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return Thread{}, fmt.Errorf("generate Thread event ID: %w", err)
	}

	return service.store.StartThreadFromMessage(ctx, StartThreadFromMessageCommand{
		Invocation: invocation,
		ThreadID:   threadID,
		LinkID:     linkID,
		EventID:    eventID,
		ChannelID:  input.ChannelID,
		MessageID:  input.MessageID,
		Title:      title,
		Visibility: input.Visibility,
		OccurredAt: service.clock.Now().UTC(),
	})
}

func validClientOperationID(value string) bool {
	if value == "" || len(value) > MaxClientOperationIDBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
