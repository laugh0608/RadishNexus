package goldenpath

import (
	"context"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/calendardate"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	TicketDueDateCommandKind = "ticket.due-date.set"
	TicketDueDateChanged     = "ticket.due-date-changed"
)

type TicketDueDate struct {
	DueDate         *string `json:"due_date"`
	DueDateRevision int     `json:"due_date_revision"`
	StatusRevision  int     `json:"status_revision"`
	CanSetDueDate   bool    `json:"can_set_due_date"`
}
type TicketDueDateInput struct {
	TicketID            string  `json:"-"`
	ClientOperationID   string  `json:"client_operation_id"`
	BaseDueDateRevision int     `json:"base_due_date_revision"`
	BaseStatusRevision  int     `json:"base_status_revision"`
	DueDate             *string `json:"due_date"`
	Confirmed           bool    `json:"confirmed"`
}
type TicketDueDateCommand struct {
	Invocation
	TicketDueDateInput
	EventID, PayloadSHA256 string
}
type TicketDueDateResult struct {
	Applied         bool    `json:"applied"`
	DueDate         *string `json:"applied_due_date"`
	DueDateRevision int     `json:"applied_due_date_revision"`
}
type TicketDueDateStore interface {
	ReadTicketDueDate(context.Context, authz.Principal, string) (TicketDueDate, error)
	SetTicketDueDate(context.Context, TicketDueDateCommand) (TicketDueDateResult, error)
}
type TicketDueDateService struct {
	store TicketDueDateStore
	ids   IDGenerator
}

func NewTicketDueDateService(store TicketDueDateStore, ids IDGenerator) *TicketDueDateService {
	return &TicketDueDateService{store, ids}
}
func (s *TicketDueDateService) ReadTicketDueDate(ctx context.Context, p authz.Principal, id string) (TicketDueDate, error) {
	if err := p.ValidateUser(); err != nil {
		return TicketDueDate{}, err
	}
	if entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: id}) != nil {
		return TicketDueDate{}, authz.ErrInvalid
	}
	return s.store.ReadTicketDueDate(ctx, p, id)
}
func ValidateTicketDueDateInput(in TicketDueDateInput) error {
	if entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: in.TicketID}) != nil || !validClientOperationID(in.ClientOperationID) || !in.Confirmed || !ValidTicketStatusRevision(in.BaseDueDateRevision) || !ValidTicketStatusRevision(in.BaseStatusRevision) || !calendardate.Valid(in.DueDate) {
		return authz.ErrInvalid
	}
	return nil
}
func (s *TicketDueDateService) SetTicketDueDate(ctx context.Context, inv Invocation, in TicketDueDateInput) (TicketDueDateResult, error) {
	if err := validateInvocation(inv); err != nil {
		return TicketDueDateResult{}, err
	}
	if inv.SourceKind != "web" {
		return TicketDueDateResult{}, authz.ErrInvalid
	}
	if err := ValidateTicketDueDateInput(in); err != nil {
		return TicketDueDateResult{}, err
	}
	event, err := s.ids.NewID("evt_")
	if err != nil {
		return TicketDueDateResult{}, err
	}
	payload := struct {
		BaseDueDateRevision int     `json:"base_due_date_revision"`
		BaseStatusRevision  int     `json:"base_status_revision"`
		DueDate             *string `json:"due_date"`
	}{in.BaseDueDateRevision, in.BaseStatusRevision, in.DueDate}
	return s.store.SetTicketDueDate(ctx, TicketDueDateCommand{Invocation: inv, TicketDueDateInput: in, EventID: event, PayloadSHA256: collaborationPayloadDigest(payload)})
}
