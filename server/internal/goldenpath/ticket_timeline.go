package goldenpath

import (
	"context"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type TicketTimelineSelection struct {
	DueDate, Execution, Classification bool
}

func (s TicketTimelineSelection) EventTypes() []string {
	types := []string{"ticket.created", "ticket.status-changed", "ticket.assignee-changed", "ticket.priority-changed", "ticket.component-linked", "ticket.component-unlinked"}
	if s.DueDate {
		types = append(types, TicketDueDateChanged)
	}
	if s.Execution {
		types = append(types, DiscussionLinked, DiscussionUnlinked)
	}
	if s.Classification {
		types = append(types, cp.TicketClassificationChanged)
	}
	return types
}

type TicketTimelineInput struct {
	TicketTimelineSelection
	Limit        int
	AfterEventID string
}

type TicketTimelinePage struct {
	Items       []TimelineItem
	NextEventID string
}

func (s *Service) GetTicketWithoutTimeline(ctx context.Context, p authz.Principal, ticket string, summary bool) (NexusView, error) {
	if err := p.ValidateUser(); err != nil {
		return NexusView{}, err
	}
	if !cp.ValidID(ticket, "tkt_") {
		return NexusView{}, authz.ErrInvalid
	}
	return s.store.GetTicketWithoutTimeline(ctx, p, ticket, summary)
}

func (s *Service) ListTicketTimeline(ctx context.Context, p authz.Principal, ticket string, input TicketTimelineInput) (TicketTimelinePage, error) {
	if err := p.ValidateUser(); err != nil {
		return TicketTimelinePage{}, err
	}
	if !cp.ValidID(ticket, "tkt_") || input.Limit < 1 || input.Limit > MaxDiscoveryPageSize || (input.AfterEventID != "" && !cp.ValidID(input.AfterEventID, "evt_")) {
		return TicketTimelinePage{}, authz.ErrInvalid
	}
	return s.store.ListTicketTimeline(ctx, p, ticket, input)
}
