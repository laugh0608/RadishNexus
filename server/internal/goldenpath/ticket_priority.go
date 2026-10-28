package goldenpath

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const TicketPriorityCommandKind = "ticket.priority.set"

func ValidTicketPriority(value string) bool {
	switch value {
	case "unset", "low", "normal", "high", "urgent":
		return true
	}
	return false
}

type TicketPriority struct {
	Priority         string `json:"priority"`
	PriorityRevision int    `json:"priority_revision"`
	StatusRevision   int    `json:"status_revision"`
	CanSetPriority   bool   `json:"can_set_priority"`
}
type TicketPriorityInput struct {
	TicketID             string `json:"-"`
	ClientOperationID    string `json:"client_operation_id"`
	BasePriorityRevision int    `json:"base_priority_revision"`
	BaseStatusRevision   int    `json:"base_status_revision"`
	Priority             string `json:"priority"`
	Confirmed            bool   `json:"confirmed"`
}
type TicketPriorityChange struct {
	From         string `json:"from"`
	To           string `json:"to"`
	FromRevision int    `json:"from_revision"`
	ToRevision   int    `json:"to_revision"`
}

func (v TicketPriorityChange) Valid() bool {
	return ValidTicketPriority(v.From) && ValidTicketPriority(v.To) && v.From != v.To && ValidTicketStatusRevision(v.FromRevision) && v.FromRevision < MaxTicketStatusRevision && v.ToRevision == v.FromRevision+1
}
func DecodeTicketPriority(payload []byte) (TicketPriorityChange, error) {
	var v struct {
		Priority TicketPriorityChange `json:"priority"`
	}
	var raw, fields map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil || len(raw) != 1 || json.Unmarshal(raw["priority"], &fields) != nil || len(fields) != 4 || fields["from"] == nil || fields["to"] == nil || fields["from_revision"] == nil || fields["to_revision"] == nil || json.Unmarshal(payload, &v) != nil || !v.Priority.Valid() {
		return v.Priority, errors.New("invalid Ticket priority event")
	}
	return v.Priority, nil
}

type TicketPriorityCommand struct {
	Invocation
	TicketPriorityInput
	EventID, PayloadSHA256 string
}
type TicketPriorityResult struct {
	Applied          bool   `json:"applied"`
	Priority         string `json:"applied_priority"`
	PriorityRevision int    `json:"applied_priority_revision"`
}
type TicketPriorityStore interface {
	ReadTicketPriority(context.Context, authz.Principal, string) (TicketPriority, error)
	SetTicketPriority(context.Context, TicketPriorityCommand) (TicketPriorityResult, error)
}
type TicketPriorityService struct {
	store TicketPriorityStore
	ids   IDGenerator
}

func NewTicketPriorityService(store TicketPriorityStore, ids IDGenerator) *TicketPriorityService {
	return &TicketPriorityService{store, ids}
}
func (s *TicketPriorityService) ReadTicketPriority(ctx context.Context, p authz.Principal, id string) (TicketPriority, error) {
	if err := p.ValidateUser(); err != nil {
		return TicketPriority{}, err
	}
	if entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: id}) != nil {
		return TicketPriority{}, authz.ErrInvalid
	}
	return s.store.ReadTicketPriority(ctx, p, id)
}
func ValidateTicketPriorityInput(in TicketPriorityInput) error {
	if entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: in.TicketID}) != nil || !validClientOperationID(in.ClientOperationID) || !in.Confirmed || !ValidTicketStatusRevision(in.BasePriorityRevision) || !ValidTicketStatusRevision(in.BaseStatusRevision) || !ValidTicketPriority(in.Priority) {
		return authz.ErrInvalid
	}
	return nil
}
func (s *TicketPriorityService) SetTicketPriority(ctx context.Context, inv Invocation, in TicketPriorityInput) (TicketPriorityResult, error) {
	if err := validateInvocation(inv); err != nil {
		return TicketPriorityResult{}, err
	}
	if inv.SourceKind != "web" {
		return TicketPriorityResult{}, authz.ErrInvalid
	}
	if err := ValidateTicketPriorityInput(in); err != nil {
		return TicketPriorityResult{}, err
	}
	event, err := s.ids.NewID("evt_")
	if err != nil {
		return TicketPriorityResult{}, err
	}
	payload := struct {
		BasePriorityRevision int    `json:"base_priority_revision"`
		BaseStatusRevision   int    `json:"base_status_revision"`
		Priority             string `json:"priority"`
	}{in.BasePriorityRevision, in.BaseStatusRevision, in.Priority}
	return s.store.SetTicketPriority(ctx, TicketPriorityCommand{Invocation: inv, TicketPriorityInput: in, EventID: event, PayloadSHA256: collaborationPayloadDigest(payload)})
}
