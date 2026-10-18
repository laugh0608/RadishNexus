package goldenpath

import (
	"context"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type TicketComponentInput struct {
	TicketID, ComponentID, LinkID, ClientOperationID string
	Confirmed                                        bool
}
type TicketComponentCommand struct {
	Invocation
	TicketComponentInput
	Kind, ResultID, EventID, PayloadSHA256 string
	OccurredAt                             time.Time
}
type TicketComponentResult struct {
	LinkID  string
	Created bool
}
type TicketComponentLink struct {
	ID        string
	Component *ConfigurationObject
	Ticket    *Ticket
	CanUnlink bool
}
type TicketComponentPage struct {
	Links   []TicketComponentLink
	NextID  string
	CanLink bool
}
type TicketComponentStore interface {
	WriteTicketComponent(context.Context, TicketComponentCommand) (TicketComponentResult, error)
	ListTicketComponents(context.Context, authz.Principal, entityref.Ref, DiscoveryPageInput) (TicketComponentPage, error)
}
type TicketComponentService struct {
	store TicketComponentStore
	ids   IDGenerator
	clock Clock
}

func NewTicketComponentService(store TicketComponentStore, ids IDGenerator, clock Clock) *TicketComponentService {
	return &TicketComponentService{store, ids, clock}
}
func (s *TicketComponentService) WriteTicketComponent(ctx context.Context, inv Invocation, kind string, in TicketComponentInput) (TicketComponentResult, error) {
	if err := validateInvocation(inv); err != nil {
		return TicketComponentResult{}, err
	}
	if inv.SourceKind != "web" || !validClientOperationID(in.ClientOperationID) || !in.Confirmed || entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: in.TicketID}) != nil {
		return TicketComponentResult{}, authz.ErrInvalid
	}
	resultID := in.LinkID
	switch kind {
	case "ticket.component.link":
		if !ValidConfigurationID(in.ComponentID, "cmp_") || in.LinkID != "" {
			return TicketComponentResult{}, authz.ErrInvalid
		}
		var err error
		resultID, err = s.ids.NewID("lnk_")
		if err != nil {
			return TicketComponentResult{}, err
		}
	case "ticket.component.unlink":
		if !ValidConfigurationID(in.LinkID, "lnk_") || in.ComponentID != "" {
			return TicketComponentResult{}, authz.ErrInvalid
		}
	default:
		return TicketComponentResult{}, authz.ErrInvalid
	}
	eventID, err := s.ids.NewID("evt_")
	if err != nil {
		return TicketComponentResult{}, err
	}
	digest := collaborationPayloadDigest(struct {
		ComponentID string `json:"component_id"`
		LinkID      string `json:"link_id"`
		Confirmed   bool   `json:"confirmed"`
	}{in.ComponentID, in.LinkID, in.Confirmed})
	return s.store.WriteTicketComponent(ctx, TicketComponentCommand{Invocation: inv, TicketComponentInput: in, Kind: kind, ResultID: resultID, EventID: eventID, PayloadSHA256: digest, OccurredAt: s.clock.Now().UTC()})
}
func (s *TicketComponentService) ListTicketComponents(ctx context.Context, p authz.Principal, source entityref.Ref, in DiscoveryPageInput) (TicketComponentPage, error) {
	target := "component"
	if source.Type == "component" {
		target = "ticket"
	} else if source.Type != "ticket" {
		return TicketComponentPage{}, authz.ErrInvalid
	}
	if err := entityref.M0Registry().Validate(source); err != nil {
		return TicketComponentPage{}, authz.ErrInvalid
	}
	if err := validateDiscoveryInput(p, target, in); err != nil {
		return TicketComponentPage{}, err
	}
	return s.store.ListTicketComponents(ctx, p, source, in)
}
