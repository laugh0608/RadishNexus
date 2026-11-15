package goldenpath

import (
	"context"
	"time"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	TicketCIRunLink       = "ticket.ci-run.link"
	TicketCIRunUnlink     = "ticket.ci-run.unlink"
	TicketCIRunLinked     = "ticket.ci-run-linked"
	TicketCIRunUnlinked   = "ticket.ci-run-unlinked"
	TraceComponentRuns    = "component-ci-runs"
	TraceTicketRuns       = "ticket-ci-runs"
	TraceTicketRunHistory = "ticket-ci-run-history"
	TraceRunTickets       = "ci-run-tickets"
	TraceRunDeployments   = "ci-run-deployments"
)

type TicketCIRunInput struct {
	TicketID, RunID, LinkID, ClientOperationID string
	Confirmed                                  bool
}

type TicketCIRunCommand struct {
	Invocation
	TicketCIRunInput
	Kind, ResultID, EventID, Digest string
}

type TicketCIRunResult struct {
	LinkID  string
	Created bool
}

type CIRunLinkFacts struct {
	Run   entityref.Ref `json:"ci_run"`
	Link  entityref.Ref `json:"link"`
	State string        `json:"state"`
}

func DecodeCIRunLinkFacts(raw []byte, event string) (CIRunLinkFacts, error) {
	var facts CIRunLinkFacts
	if err := cp.Decode(raw, &facts); err != nil {
		return facts, err
	}
	if facts.Run.Type != "ci-run" || facts.Link.Type != "entity-link" ||
		!cp.ValidID(facts.Run.ID, "cir_") || !cp.ValidID(facts.Link.ID, "lnk_") ||
		!((event == TicketCIRunLinked && facts.State == "active") || (event == TicketCIRunUnlinked && facts.State == "removed")) {
		return facts, authz.ErrInvalid
	}
	return facts, nil
}

type CIRunTraceInput struct {
	DiscoveryPageInput
	AfterTime time.Time
}

type TraceEntity struct {
	Ref  entityref.Ref `json:"ref"`
	Name string        `json:"name"`
}

type CIRunTraceItem struct {
	Ref           entityref.Ref  `json:"ref"`
	Status        string         `json:"status,omitempty"`
	CompletedAt   *time.Time     `json:"completed_at,omitempty"`
	Component     *TraceEntity   `json:"component,omitempty"`
	Environment   *TraceEntity   `json:"environment,omitempty"`
	Link          *entityref.Ref `json:"link,omitempty"`
	CanUnlink     *bool          `json:"can_unlink,omitempty"`
	Title         string         `json:"title,omitempty"`
	Project       *entityref.Ref `json:"project,omitempty"`
	Run           *entityref.Ref `json:"ci_run,omitempty"`
	Actor         *entityref.Ref `json:"actor,omitempty"`
	OccurredAt    *time.Time     `json:"occurred_at,omitempty"`
	RelationState string         `json:"relation_state,omitempty"`
}

type CIRunTracePage struct {
	Items    []CIRunTraceItem
	NextID   string
	NextTime time.Time
	CanLink  bool
}

type TicketCIRunStore interface {
	WriteTicketCIRun(context.Context, TicketCIRunCommand) (TicketCIRunResult, error)
	ListCIRunTrace(context.Context, authz.Principal, string, string, CIRunTraceInput) (CIRunTracePage, error)
}

type TicketCIRunService struct {
	store TicketCIRunStore
	ids   IDGenerator
}

func NewTicketCIRunService(store TicketCIRunStore, ids IDGenerator) *TicketCIRunService {
	return &TicketCIRunService{store: store, ids: ids}
}

func (s *TicketCIRunService) Write(ctx context.Context, inv Invocation, kind string, in TicketCIRunInput) (TicketCIRunResult, error) {
	var out TicketCIRunResult
	if err := validateInvocation(inv); err != nil {
		return out, err
	}
	if inv.SourceKind != "web" || !in.Confirmed || !validClientOperationID(in.ClientOperationID) || !cp.ValidID(in.TicketID, "tkt_") {
		return out, authz.ErrInvalid
	}
	command := TicketCIRunCommand{Invocation: inv, TicketCIRunInput: in, Kind: kind}
	var err error
	switch kind {
	case TicketCIRunLink:
		if !cp.ValidID(in.RunID, "cir_") || in.LinkID != "" {
			return out, authz.ErrInvalid
		}
		command.ResultID, err = s.ids.NewID("lnk_")
		command.Digest = collaborationPayloadDigest(struct {
			Run string `json:"ci_run_id"`
		}{in.RunID})
	case TicketCIRunUnlink:
		if !cp.ValidID(in.LinkID, "lnk_") || in.RunID != "" {
			return out, authz.ErrInvalid
		}
		command.ResultID = in.LinkID
		command.Digest = collaborationPayloadDigest(struct {
			Link string `json:"link_id"`
		}{in.LinkID})
	default:
		return out, authz.ErrInvalid
	}
	if err != nil {
		return out, err
	}
	command.EventID, err = s.ids.NewID("evt_")
	if err != nil {
		return out, err
	}
	return s.store.WriteTicketCIRun(ctx, command)
}

func (s *TicketCIRunService) List(ctx context.Context, p authz.Principal, kind, source string, in CIRunTraceInput) (CIRunTracePage, error) {
	var sourcePrefix, afterPrefix string
	switch kind {
	case TraceComponentRuns:
		sourcePrefix, afterPrefix = "cmp_", "cir_"
	case TraceTicketRuns:
		sourcePrefix, afterPrefix = "tkt_", "cir_"
	case TraceTicketRunHistory:
		sourcePrefix, afterPrefix = "tkt_", "evt_"
	case TraceRunTickets:
		sourcePrefix, afterPrefix = "cir_", "tkt_"
	case TraceRunDeployments:
		sourcePrefix, afterPrefix = "cir_", "dpl_"
	default:
		return CIRunTracePage{}, authz.ErrInvalid
	}
	if err := p.ValidateUser(); err != nil {
		return CIRunTracePage{}, err
	}
	timeBoundary := kind == TraceTicketRunHistory && in.AfterID != ""
	if !cp.ValidID(source, sourcePrefix) || in.Limit < 1 || in.Limit > MaxDiscoveryPageSize ||
		(in.AfterID != "" && !cp.ValidID(in.AfterID, afterPrefix)) || timeBoundary == in.AfterTime.IsZero() {
		return CIRunTracePage{}, authz.ErrInvalid
	}
	return s.store.ListCIRunTrace(ctx, p, kind, source, in)
}

func ValidBuildTerminalStatus(value string) bool {
	return value == "succeeded" || value == "failed" || value == "canceled"
}
