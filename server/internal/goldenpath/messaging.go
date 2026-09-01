package goldenpath

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const (
	MaxMessageBodyBytes       = 16 * 1024
	MaxClientOperationIDBytes = 128
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

type StartThreadFromMessageInput struct {
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
	if input.ChannelID == "" || !validClientOperationID(input.ClientOperationID) {
		return CreateMessageResult{}, fmt.Errorf(
			"%w: channel ID and canonical client operation ID are required",
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

func (service *Service) StartThreadFromMessage(
	ctx context.Context,
	invocation Invocation,
	input StartThreadFromMessageInput,
) (Thread, error) {
	if err := validateInvocation(invocation); err != nil {
		return Thread{}, err
	}
	title := strings.TrimSpace(input.Title)
	if input.MessageID == "" || title == "" ||
		(input.Visibility != "project" && input.Visibility != "restricted") {
		return Thread{}, fmt.Errorf(
			"%w: message ID, title, and project or restricted visibility are required",
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
