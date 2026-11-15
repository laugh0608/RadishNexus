package goldenpath

import (
	"context"
	"slices"
	"strings"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const TicketClassificationSet = "ticket.classification.set"

type TicketLabel struct {
	Ref    entityref.Ref `json:"ref"`
	Name   string        `json:"name"`
	Status string        `json:"status"`
}

type TicketLabelEntry struct {
	TicketLabel
	Revision int `json:"revision"`
}

type TicketLabelPage struct {
	Items     []TicketLabelEntry
	NextID    string
	CanManage bool
}

type TicketClassificationSummary struct {
	Type   string        `json:"ticket_type"`
	Labels []TicketLabel `json:"labels"`
}

type TicketClassification struct {
	TicketClassificationSummary
	Revision int  `json:"classification_revision"`
	CanSet   bool `json:"can_set"`
}

type ClassificationInput struct {
	TicketID string `json:"-"`
	cp.Classification
	BaseRevision      int    `json:"base_classification_revision"`
	ClientOperationID string `json:"client_operation_id"`
	Confirmed         bool   `json:"confirmed"`
}

type ClassificationCommand struct {
	Invocation
	ClassificationInput
	EventID, Digest string
}

type LabelInput struct {
	ProjectID, LabelID, Action string
	Name                       string `json:"name"`
	BaseRevision               int    `json:"base_revision"`
	ClientOperationID          string `json:"client_operation_id"`
	Confirmed                  bool   `json:"confirmed"`
}

type LabelCommand struct {
	Invocation
	LabelInput
	EventID, AuditID, Digest string
}

type TicketClassificationStore interface {
	ReadTicketClassification(context.Context, authz.Principal, string) (TicketClassification, error)
	SetTicketClassification(context.Context, ClassificationCommand) (int, error)
	ListTicketLabels(context.Context, authz.Principal, string, string, DiscoveryPageInput) (TicketLabelPage, error)
	WriteTicketLabel(context.Context, LabelCommand) (TicketLabelEntry, error)
}

type TicketClassificationService struct {
	TicketClassificationStore
	ids IDGenerator
}

func NewTicketClassificationService(store TicketClassificationStore, ids IDGenerator) *TicketClassificationService {
	return &TicketClassificationService{TicketClassificationStore: store, ids: ids}
}

func NormalizeClassification(c cp.Classification) (cp.Classification, error) {
	c.LabelIDs = slices.Clone(c.LabelIDs)
	slices.Sort(c.LabelIDs)
	if !c.Valid() {
		return c, authz.ErrInvalid
	}
	return c, nil
}

func initialClassification(value *cp.Classification, ids IDGenerator) (*cp.Classification, string, error) {
	if value == nil {
		return nil, "", nil
	}
	c, err := NormalizeClassification(*value)
	if err != nil || c.Equal(cp.EmptyClassification()) {
		return nil, "", err
	}
	event, err := ids.NewID("evt_")
	return &c, event, err
}

type ticketCreationPayload struct {
	Title          string             `json:"title"`
	Classification *cp.Classification `json:"classification,omitempty"`
}

func (s *TicketClassificationService) Set(ctx context.Context, inv Invocation, in ClassificationInput) (int, error) {
	if err := validateInvocation(inv); err != nil {
		return 0, err
	}
	if inv.SourceKind != "web" || !in.Confirmed || !validClientOperationID(in.ClientOperationID) ||
		!cp.ValidID(in.TicketID, "tkt_") || !ValidTicketStatusRevision(in.BaseRevision) {
		return 0, authz.ErrInvalid
	}
	c, err := NormalizeClassification(in.Classification)
	if err != nil {
		return 0, err
	}
	in.Classification = c
	event, err := s.ids.NewID("evt_")
	if err != nil {
		return 0, err
	}
	return s.SetTicketClassification(ctx, ClassificationCommand{Invocation: inv, ClassificationInput: in, EventID: event,
		Digest: collaborationPayloadDigest(struct {
			cp.Classification
			BaseRevision int `json:"base_classification_revision"`
		}{c, in.BaseRevision})})
}

func (s *TicketClassificationService) WriteLabel(ctx context.Context, inv Invocation, in LabelInput) (TicketLabelEntry, error) {
	var out TicketLabelEntry
	if err := validateInvocation(inv); err != nil {
		return out, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if inv.SourceKind != "web" || !in.Confirmed || !validClientOperationID(in.ClientOperationID) || !ValidConfigurationID(in.ProjectID, "prj_") {
		return out, authz.ErrInvalid
	}
	switch in.Action {
	case "create":
		if in.LabelID != "" || in.BaseRevision != 0 || !cp.ValidTicketLabelName(in.Name) {
			return out, authz.ErrInvalid
		}
		var err error
		in.LabelID, err = s.ids.NewID("lbl_")
		if err != nil {
			return out, err
		}
	case "rename", "archive":
		if !cp.ValidID(in.LabelID, "lbl_") || !ValidTicketStatusRevision(in.BaseRevision) ||
			(in.Action == "rename" && !cp.ValidTicketLabelName(in.Name)) || (in.Action == "archive" && in.Name != "") {
			return out, authz.ErrInvalid
		}
	default:
		return out, authz.ErrInvalid
	}
	event, err := s.ids.NewID("evt_")
	if err != nil {
		return out, err
	}
	audit, err := s.ids.NewID("cfa_")
	if err != nil {
		return out, err
	}
	return s.WriteTicketLabel(ctx, LabelCommand{Invocation: inv, LabelInput: in, EventID: event, AuditID: audit,
		Digest: collaborationPayloadDigest(struct {
			Name         string `json:"name"`
			BaseRevision int    `json:"base_revision"`
		}{in.Name, in.BaseRevision})})
}
