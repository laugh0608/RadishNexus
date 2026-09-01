// Package goldenpath contains the first formal Thread to Decision to Ticket
// application slice. It is transport-independent and receives explicit principals.
package goldenpath

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type Invocation struct {
	Principal     authz.Principal
	SourceKind    string
	SourceID      string
	CorrelationID string
	CausationID   string
}

type Decision struct {
	ID                 string
	WorkspaceID        string
	GoverningProjectID string
	Question           string
	Outcome            string
	Rationale          string
	Status             string
	ProposerID         string
	DeciderIDs         []string
	DecidedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Ticket struct {
	ID                 string
	WorkspaceID        string
	GoverningProjectID string
	Title              string
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ProjectionState string

const (
	ProjectionVisible    ProjectionState = "visible"
	ProjectionRestricted ProjectionState = "restricted"
	ProjectionHidden     ProjectionState = "hidden"
)

// RelationProjection deliberately leaves all target fields empty when State
// is restricted. Hidden relations are omitted by the Store.
type RelationProjection struct {
	State        ProjectionState
	RelationType string
	Target       entityref.Ref
	Title        string
}

type CreateDecisionInput struct {
	ThreadID string
	Question string
}

type AcceptDecisionInput struct {
	DecisionID string
	Outcome    string
	Rationale  string
}

type CreateTicketInput struct {
	DecisionID string
	Title      string
}

type CreateDecisionCommand struct {
	Invocation
	DecisionID string
	LinkID     string
	EventID    string
	ThreadID   string
	Question   string
	OccurredAt time.Time
}

type AcceptDecisionCommand struct {
	Invocation
	EventID    string
	DecisionID string
	Outcome    string
	Rationale  string
	OccurredAt time.Time
}

type CreateTicketCommand struct {
	Invocation
	TicketID   string
	LinkID     string
	EventID    string
	DecisionID string
	Title      string
	OccurredAt time.Time
}

// Store owns the database transaction for each command so permission facts,
// business state, domain events, links, and Outbox state commit atomically.
type Store interface {
	CreateMessage(context.Context, CreateMessageCommand) (CreateMessageResult, error)
	StartThreadFromMessage(context.Context, StartThreadFromMessageCommand) (Thread, error)
	CreateDecisionFromThread(context.Context, CreateDecisionCommand) (Decision, error)
	AcceptDecision(context.Context, AcceptDecisionCommand) (Decision, error)
	CreateTicketFromDecision(context.Context, CreateTicketCommand) (Ticket, error)
	RecordCompletedCIRun(context.Context, RecordCompletedCIRunCommand) (CIRunReceipt, error)
	RecordStagingDeployment(context.Context, RecordStagingDeploymentCommand) (Deployment, error)
	ListRelations(context.Context, authz.Principal, entityref.Ref) ([]RelationProjection, error)
	GetNexusView(context.Context, authz.Principal, entityref.Ref) (NexusView, error)
}

type IDGenerator interface {
	NewID(prefix string) (string, error)
}

type Clock interface {
	Now() time.Time
}

type Service struct {
	store Store
	ids   IDGenerator
	clock Clock
}

func NewService(store Store, ids IDGenerator, clock Clock) *Service {
	return &Service{store: store, ids: ids, clock: clock}
}

func (service *Service) CreateDecisionFromThread(
	ctx context.Context,
	invocation Invocation,
	input CreateDecisionInput,
) (Decision, error) {
	if err := validateInvocation(invocation); err != nil {
		return Decision{}, err
	}
	question := strings.TrimSpace(input.Question)
	if input.ThreadID == "" || question == "" {
		return Decision{}, fmt.Errorf("%w: thread ID and question are required", authz.ErrInvalid)
	}

	decisionID, err := service.ids.NewID("dec_")
	if err != nil {
		return Decision{}, fmt.Errorf("generate decision ID: %w", err)
	}
	linkID, err := service.ids.NewID("lnk_")
	if err != nil {
		return Decision{}, fmt.Errorf("generate evidence link ID: %w", err)
	}
	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return Decision{}, fmt.Errorf("generate decision event ID: %w", err)
	}

	return service.store.CreateDecisionFromThread(ctx, CreateDecisionCommand{
		Invocation: invocation,
		DecisionID: decisionID,
		LinkID:     linkID,
		EventID:    eventID,
		ThreadID:   input.ThreadID,
		Question:   question,
		OccurredAt: service.clock.Now().UTC(),
	})
}

func (service *Service) AcceptDecision(
	ctx context.Context,
	invocation Invocation,
	input AcceptDecisionInput,
) (Decision, error) {
	if err := validateInvocation(invocation); err != nil {
		return Decision{}, err
	}
	outcome := strings.TrimSpace(input.Outcome)
	rationale := strings.TrimSpace(input.Rationale)
	if input.DecisionID == "" || outcome == "" || rationale == "" {
		return Decision{}, fmt.Errorf("%w: decision ID, outcome, and rationale are required", authz.ErrInvalid)
	}

	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return Decision{}, fmt.Errorf("generate acceptance event ID: %w", err)
	}

	return service.store.AcceptDecision(ctx, AcceptDecisionCommand{
		Invocation: invocation,
		EventID:    eventID,
		DecisionID: input.DecisionID,
		Outcome:    outcome,
		Rationale:  rationale,
		OccurredAt: service.clock.Now().UTC(),
	})
}

func (service *Service) CreateTicketFromDecision(
	ctx context.Context,
	invocation Invocation,
	input CreateTicketInput,
) (Ticket, error) {
	if err := validateInvocation(invocation); err != nil {
		return Ticket{}, err
	}
	title := strings.TrimSpace(input.Title)
	if input.DecisionID == "" || title == "" {
		return Ticket{}, fmt.Errorf("%w: decision ID and title are required", authz.ErrInvalid)
	}

	ticketID, err := service.ids.NewID("tkt_")
	if err != nil {
		return Ticket{}, fmt.Errorf("generate ticket ID: %w", err)
	}
	linkID, err := service.ids.NewID("lnk_")
	if err != nil {
		return Ticket{}, fmt.Errorf("generate implementation link ID: %w", err)
	}
	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return Ticket{}, fmt.Errorf("generate ticket event ID: %w", err)
	}

	return service.store.CreateTicketFromDecision(ctx, CreateTicketCommand{
		Invocation: invocation,
		TicketID:   ticketID,
		LinkID:     linkID,
		EventID:    eventID,
		DecisionID: input.DecisionID,
		Title:      title,
		OccurredAt: service.clock.Now().UTC(),
	})
}

func (service *Service) ListRelations(
	ctx context.Context,
	principal authz.Principal,
	source entityref.Ref,
) ([]RelationProjection, error) {
	if err := principal.ValidateUser(); err != nil {
		return nil, err
	}
	if err := entityref.M0Registry().Validate(source); err != nil {
		return nil, fmt.Errorf("%w: source reference: %v", authz.ErrInvalid, err)
	}
	return service.store.ListRelations(ctx, principal, source)
}

func validateInvocation(invocation Invocation) error {
	if err := invocation.Principal.ValidateUser(); err != nil {
		return err
	}
	if invocation.SourceKind != "web" && invocation.SourceKind != "api" {
		return fmt.Errorf("%w: source kind must be web or api for a user invocation", authz.ErrInvalid)
	}
	if invocation.CorrelationID == "" {
		return fmt.Errorf("%w: correlation ID is required", authz.ErrInvalid)
	}
	return nil
}

type CryptoIDGenerator struct{}

func (CryptoIDGenerator) NewID(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random), nil
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
