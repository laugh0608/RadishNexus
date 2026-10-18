package goldenpath

import (
	"context"
	"fmt"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const MaxTicketStatusRevision = 2147483647
const TicketStatusCommandKind = "ticket.status.change"

func ValidTicketStatus(status string) bool {
	return status == "open" || status == "in-progress" || status == "done" || status == "canceled"
}

func ValidTicketStatusRevision(revision int) bool {
	return revision >= 1 && revision <= MaxTicketStatusRevision
}

func CanTransitionTicketStatus(from, to string) bool {
	if !ValidTicketStatus(from) || !ValidTicketStatus(to) || from == to {
		return false
	}
	return from == "open" || from == "in-progress" || to == "open"
}

type TicketStatusTransition struct {
	FromStatus   string `json:"from_status"`
	ToStatus     string `json:"to_status"`
	FromRevision int    `json:"from_status_revision"`
	ToRevision   int    `json:"to_status_revision"`
}

func (t TicketStatusTransition) Valid() bool {
	return CanTransitionTicketStatus(t.FromStatus, t.ToStatus) && ValidTicketStatusRevision(t.FromRevision) &&
		ValidTicketStatusRevision(t.ToRevision) && t.FromRevision < MaxTicketStatusRevision && t.ToRevision == t.FromRevision+1
}

type TicketStatusInput struct {
	TicketID, ClientOperationID, ToStatus string
	BaseStatusRevision                    int
	Confirmed                             bool
}

type TicketStatusCommand struct {
	Invocation
	TicketStatusInput
	EventID, PayloadSHA256 string
}

type TicketStatusResult struct {
	TicketID, AppliedStatus string
	AppliedStatusRevision   int
}

type TicketStatusStore interface {
	ChangeTicketStatus(context.Context, TicketStatusCommand) (TicketStatusResult, error)
}

type TicketStatusService struct {
	store TicketStatusStore
	ids   IDGenerator
}

func NewTicketStatusService(store TicketStatusStore, ids IDGenerator) *TicketStatusService {
	return &TicketStatusService{store: store, ids: ids}
}

func (s *TicketStatusService) ChangeTicketStatus(ctx context.Context, inv Invocation, in TicketStatusInput) (TicketStatusResult, error) {
	if err := validateInvocation(inv); err != nil {
		return TicketStatusResult{}, err
	}
	if inv.SourceKind != "web" || entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: in.TicketID}) != nil || !validClientOperationID(in.ClientOperationID) ||
		!in.Confirmed || !ValidTicketStatus(in.ToStatus) || !ValidTicketStatusRevision(in.BaseStatusRevision) {
		return TicketStatusResult{}, authz.ErrInvalid
	}
	eventID, err := s.ids.NewID("evt_")
	if err != nil {
		return TicketStatusResult{}, fmt.Errorf("generate Ticket status event: %w", err)
	}
	digest := collaborationPayloadDigest(struct {
		Base int    `json:"base_status_revision"`
		To   string `json:"to_status"`
	}{in.BaseStatusRevision, in.ToStatus})
	return s.store.ChangeTicketStatus(ctx, TicketStatusCommand{Invocation: inv, TicketStatusInput: in, EventID: eventID, PayloadSHA256: digest})
}
