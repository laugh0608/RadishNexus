package goldenpath

import (
	"context"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	TicketFromMessage      = "ticket.create-from-message"
	TicketDiscussionLink   = "ticket.discussion.link"
	TicketDiscussionUnlink = "ticket.discussion.unlink"
	DiscussionLinked       = "ticket.discussion-linked"
	DiscussionUnlinked     = "ticket.discussion-unlinked"
)

type MessageLocation struct {
	MessageID string
	ChannelID string
	ThreadID  *string
}

type TicketExecutionInput struct {
	ChannelID, MessageID, TicketID, ThreadID, LinkID, Title, ClientOperationID string
	Confirmed                                                                  bool
}

type TicketExecutionCommand struct {
	Invocation
	TicketExecutionInput
	Kind, EventID, ResultID, RelationID, Digest string
}

type TicketExecutionResult struct {
	Ticket  Ticket
	Source  MessageLocation
	LinkID  string
	Created bool
}

type ExecutionRow struct {
	Ref                                         entityref.Ref
	Title, ProjectID, Status, ChannelID, LinkID string
	CanUnlink                                   bool
}

type ExecutionPage struct {
	Rows     []ExecutionRow
	NextID   string
	CanWrite bool
}

type TicketExecutionStore interface {
	WriteTicketExecution(context.Context, TicketExecutionCommand) (TicketExecutionResult, error)
	ListTicketExecution(context.Context, authz.Principal, entityref.Ref, string, DiscoveryPageInput) (ExecutionPage, error)
	ReadExecutionMessage(context.Context, authz.Principal, string, string) (Message, error)
	ListExecutionReplies(context.Context, authz.Principal, string, ListChannelMessagesInput) (MessagePage, string, bool, error)
}

type TicketExecutionService struct {
	TicketExecutionStore
	ids IDGenerator
}

func NewTicketExecutionService(store TicketExecutionStore, ids IDGenerator) *TicketExecutionService {
	return &TicketExecutionService{TicketExecutionStore: store, ids: ids}
}

func (s *TicketExecutionService) Write(ctx context.Context, inv Invocation, kind string, in TicketExecutionInput) (TicketExecutionResult, error) {
	var result TicketExecutionResult
	if err := validateInvocation(inv); err != nil {
		return result, err
	}
	if inv.SourceKind != "web" || !validClientOperationID(in.ClientOperationID) || !in.Confirmed {
		return result, authz.ErrInvalid
	}

	cmd := TicketExecutionCommand{Invocation: inv, TicketExecutionInput: in, Kind: kind}
	resultPrefix := "lnk_"
	var canonical any
	switch kind {
	case TicketFromMessage:
		title := strings.TrimSpace(in.Title)
		if entityref.M0Registry().Validate(entityref.Ref{Type: "message", ID: in.MessageID}) != nil || !ValidConfigurationID(in.ChannelID, "chn_") || !validCollaborationText(in.Title, title) {
			return result, authz.ErrInvalid
		}
		cmd.Title = title
		resultPrefix = "tkt_"
		canonical = struct {
			Title string `json:"title"`
		}{title}
	case TicketDiscussionLink:
		if entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: in.TicketID}) != nil || entityref.M0Registry().Validate(entityref.Ref{Type: "thread", ID: in.ThreadID}) != nil {
			return result, authz.ErrInvalid
		}
		canonical = struct {
			ThreadID string `json:"thread_id"`
		}{in.ThreadID}
	case TicketDiscussionUnlink:
		if entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: in.TicketID}) != nil || !ValidConfigurationID(in.LinkID, "lnk_") {
			return result, authz.ErrInvalid
		}
		canonical = struct {
			LinkID string `json:"link_id"`
		}{in.LinkID}
	default:
		return result, authz.ErrInvalid
	}

	var err error
	cmd.EventID, err = s.ids.NewID("evt_")
	if err != nil {
		return result, err
	}
	cmd.ResultID = in.LinkID
	if kind != TicketDiscussionUnlink {
		cmd.ResultID, err = s.ids.NewID(resultPrefix)
		if err != nil {
			return result, err
		}
	}
	if kind == TicketFromMessage {
		cmd.RelationID, err = s.ids.NewID("lnk_")
		if err != nil {
			return result, err
		}
	}
	cmd.Digest = collaborationPayloadDigest(canonical)
	return s.WriteTicketExecution(ctx, cmd)
}
