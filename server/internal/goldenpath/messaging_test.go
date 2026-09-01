package goldenpath

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestCreateMessageBuildsAtomicCommandWithoutNormalizingBody(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	now := time.Date(2026, 9, 1, 8, 30, 0, 0, time.FixedZone("source", 8*60*60))
	service := NewService(
		store,
		&sequenceIDs{values: []string{"msg_1", "evt_1"}},
		fixedClock{value: now},
	)
	invocation := validMessagingInvocation()
	body := "  exact body\n"

	result, err := service.CreateMessage(context.Background(), invocation, CreateMessageInput{
		ChannelID:         "chn_1",
		ThreadID:          "thr_1",
		ClientOperationID: "web-01:message-1",
		Body:              body,
	})
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	command := store.createMessageCommand
	if !result.Created || result.Message.ID != "msg_1" || command.EventID != "evt_1" {
		t.Fatalf("generated Message command = %#v, result = %#v", command, result)
	}
	if command.Body != body {
		t.Fatalf("Message body = %q, want exact input %q", command.Body, body)
	}
	if command.ChannelID != "chn_1" || command.ThreadID != "thr_1" ||
		command.ClientOperationID != "web-01:message-1" || command.Invocation != invocation {
		t.Fatalf("CreateMessage command = %#v", command)
	}
	if !command.OccurredAt.Equal(now.UTC()) {
		t.Fatalf("OccurredAt = %v, want %v", command.OccurredAt, now.UTC())
	}
}

func TestCreateMessageRejectsInvalidFactsBeforeStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CreateMessageInput
	}{
		{name: "missing channel", input: CreateMessageInput{ClientOperationID: "op-1", Body: "body"}},
		{name: "space in operation", input: CreateMessageInput{ChannelID: "chn_1", ClientOperationID: "op 1", Body: "body"}},
		{name: "blank body", input: CreateMessageInput{ChannelID: "chn_1", ClientOperationID: "op-1", Body: " \n\t"}},
		{name: "nul body", input: CreateMessageInput{ChannelID: "chn_1", ClientOperationID: "op-1", Body: "a\x00b"}},
		{name: "oversized body", input: CreateMessageInput{ChannelID: "chn_1", ClientOperationID: "op-1", Body: strings.Repeat("a", MaxMessageBodyBytes+1)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &recordingStore{}
			service := NewService(store, &sequenceIDs{values: []string{"msg_1", "evt_1"}}, fixedClock{})
			_, err := service.CreateMessage(context.Background(), validMessagingInvocation(), test.input)
			if !errors.Is(err, authz.ErrInvalid) {
				t.Fatalf("CreateMessage() error = %v, want invalid", err)
			}
			if store.createMessageCommand.MessageID != "" {
				t.Fatalf("invalid input reached Store: %#v", store.createMessageCommand)
			}
		})
	}
}

func TestListChannelMessagesValidatesAndNormalizesCursor(t *testing.T) {
	t.Parallel()

	principal := authz.Principal{
		Kind: authz.PrincipalUser, ID: "usr_1", WorkspaceID: "wrk_1",
	}
	localTime := time.Date(2026, 9, 1, 18, 30, 0, 123000000, time.FixedZone("source", 8*60*60))
	want := MessagePage{Messages: []MessageProjection{{ID: "msg_1", Body: "body"}}}
	store := &recordingStore{messagePage: want}
	service := NewService(store, &sequenceIDs{}, fixedClock{})

	got, err := service.ListChannelMessages(context.Background(), principal, ListChannelMessagesInput{
		ChannelID: "chn_1",
		Before: &MessagePageCursor{
			CreatedAt: localTime,
			MessageID: "msg_cursor",
		},
		Limit: 25,
	})
	if err != nil {
		t.Fatalf("ListChannelMessages() error = %v", err)
	}
	if !store.listMessagesCalled || store.listMessagesPrincipal != principal ||
		store.listMessagesInput.ChannelID != "chn_1" || store.listMessagesInput.Limit != 25 {
		t.Fatalf("ListChannelMessages Store input = %#v, principal = %#v", store.listMessagesInput, store.listMessagesPrincipal)
	}
	if store.listMessagesInput.Before == nil ||
		!store.listMessagesInput.Before.CreatedAt.Equal(localTime.UTC()) ||
		store.listMessagesInput.Before.CreatedAt.Location() != time.UTC ||
		store.listMessagesInput.Before.MessageID != "msg_cursor" {
		t.Fatalf("normalized Message cursor = %#v", store.listMessagesInput.Before)
	}
	if len(got.Messages) != 1 || got.Messages[0].ID != "msg_1" {
		t.Fatalf("ListChannelMessages() = %#v", got)
	}
}

func TestListChannelMessagesRejectsInvalidBoundaryBeforeStore(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		principal authz.Principal
		input     ListChannelMessagesInput
	}{
		{
			name:      "system principal",
			principal: authz.Principal{Kind: authz.PrincipalSystem, ID: "system", WorkspaceID: "wrk_1"},
			input:     ListChannelMessagesInput{ChannelID: "chn_1", Limit: 1},
		},
		{
			name:      "invalid channel",
			principal: validMessagingInvocation().Principal,
			input:     ListChannelMessagesInput{ChannelID: "channel_1", Limit: 1},
		},
		{
			name:      "zero limit",
			principal: validMessagingInvocation().Principal,
			input:     ListChannelMessagesInput{ChannelID: "chn_1", Limit: 0},
		},
		{
			name:      "oversized limit",
			principal: validMessagingInvocation().Principal,
			input:     ListChannelMessagesInput{ChannelID: "chn_1", Limit: MaxMessagePageSize + 1},
		},
		{
			name:      "zero cursor time",
			principal: validMessagingInvocation().Principal,
			input: ListChannelMessagesInput{
				ChannelID: "chn_1", Limit: 1,
				Before: &MessagePageCursor{MessageID: "msg_cursor"},
			},
		},
		{
			name:      "invalid cursor ID",
			principal: validMessagingInvocation().Principal,
			input: ListChannelMessagesInput{
				ChannelID: "chn_1", Limit: 1,
				Before: &MessagePageCursor{CreatedAt: now, MessageID: "message_cursor"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &recordingStore{}
			service := NewService(store, &sequenceIDs{}, fixedClock{})
			_, err := service.ListChannelMessages(context.Background(), test.principal, test.input)
			if !errors.Is(err, authz.ErrInvalid) && !errors.Is(err, authz.ErrUnauthenticated) {
				t.Fatalf("ListChannelMessages() error = %v, want invalid or unauthenticated", err)
			}
			if store.listMessagesCalled {
				t.Fatalf("invalid input reached Store: %#v", store.listMessagesInput)
			}
		})
	}
}

func TestStartThreadFromMessageBuildsAtomicCommand(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.FixedZone("source", 8*60*60))
	service := NewService(
		store,
		&sequenceIDs{values: []string{"thr_1", "lnk_1", "evt_1"}},
		fixedClock{value: now},
	)

	thread, err := service.StartThreadFromMessage(
		context.Background(),
		validMessagingInvocation(),
		StartThreadFromMessageInput{
			MessageID:  "msg_1",
			Title:      "  Investigate latency  ",
			Visibility: "restricted",
		},
	)
	if err != nil {
		t.Fatalf("StartThreadFromMessage() error = %v", err)
	}
	command := store.startThreadCommand
	if thread.ID != "thr_1" || command.LinkID != "lnk_1" || command.EventID != "evt_1" {
		t.Fatalf("generated Thread command = %#v, result = %#v", command, thread)
	}
	if command.MessageID != "msg_1" || command.Title != "Investigate latency" ||
		command.Visibility != "restricted" {
		t.Fatalf("StartThread command = %#v", command)
	}
	if !command.OccurredAt.Equal(now.UTC()) {
		t.Fatalf("OccurredAt = %v, want %v", command.OccurredAt, now.UTC())
	}
}

func TestStartThreadFromMessageRejectsInvalidFactsBeforeStore(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	service := NewService(
		store,
		&sequenceIDs{values: []string{"thr_1", "lnk_1", "evt_1"}},
		fixedClock{},
	)
	_, err := service.StartThreadFromMessage(
		context.Background(),
		validMessagingInvocation(),
		StartThreadFromMessageInput{MessageID: "msg_1", Title: "title", Visibility: "workspace"},
	)
	if !errors.Is(err, authz.ErrInvalid) {
		t.Fatalf("StartThreadFromMessage() error = %v, want invalid", err)
	}
	if store.startThreadCommand.ThreadID != "" {
		t.Fatalf("invalid input reached Store: %#v", store.startThreadCommand)
	}
}

func validMessagingInvocation() Invocation {
	return Invocation{
		Principal:     authz.Principal{Kind: authz.PrincipalUser, ID: "usr_1", WorkspaceID: "wrk_1"},
		SourceKind:    "api",
		SourceID:      "web",
		CorrelationID: "cor_1",
	}
}
