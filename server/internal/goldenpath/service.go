// Package goldenpath contains the first formal Thread to Decision to Ticket
// application slice. It is transport-independent and receives explicit principals.
package goldenpath

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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

type CreateDecisionResult struct {
	Decision Decision
	Created  bool
}

type AcceptDecisionResult struct {
	Decision Decision
	Accepted bool
}

type Ticket struct {
	ID                 string
	WorkspaceID        string
	GoverningProjectID string
	Title              string
	Status             string
	CreatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateTicketResult struct {
	Ticket  Ticket
	Created bool
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
	ThreadID          string
	ClientOperationID string
	Question          string
}

type AcceptDecisionInput struct {
	DecisionID        string
	ClientOperationID string
	Outcome           string
	Rationale         string
}

type CreateTicketInput struct {
	DecisionID        string
	ClientOperationID string
	Title             string
}

type CreateDecisionCommand struct {
	Invocation
	DecisionID        string
	LinkID            string
	EventID           string
	ThreadID          string
	ClientOperationID string
	PayloadSHA256     string
	Question          string
	OccurredAt        time.Time
}

type AcceptDecisionCommand struct {
	Invocation
	EventID           string
	DecisionID        string
	ClientOperationID string
	PayloadSHA256     string
	Outcome           string
	Rationale         string
	OccurredAt        time.Time
}

type CreateTicketCommand struct {
	Invocation
	TicketID          string
	LinkID            string
	EventID           string
	DecisionID        string
	ClientOperationID string
	PayloadSHA256     string
	Title             string
	OccurredAt        time.Time
}

// Store owns the database transaction for each command so permission facts,
// business state, domain events, links, and Outbox state commit atomically.
type Store interface {
	CreateMessage(context.Context, CreateMessageCommand) (CreateMessageResult, error)
	ListChannelMessages(context.Context, authz.Principal, ListChannelMessagesInput) (MessagePage, error)
	AuthorizeChannelRead(context.Context, authz.Principal, string) error
	GetChannelMessage(context.Context, authz.Principal, string, string) (MessageProjection, error)
	StartThreadFromMessage(context.Context, StartThreadFromMessageCommand) (Thread, error)
	CreateDecisionFromThread(context.Context, CreateDecisionCommand) (CreateDecisionResult, error)
	AcceptDecision(context.Context, AcceptDecisionCommand) (AcceptDecisionResult, error)
	CreateTicketFromDecision(context.Context, CreateTicketCommand) (CreateTicketResult, error)
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
	store                  Store
	ids                    IDGenerator
	clock                  Clock
	messageCreatedNotifier MessageCreatedNotifier
}

type ServiceOption func(*Service)

func WithMessageCreatedNotifier(notifier MessageCreatedNotifier) ServiceOption {
	return func(service *Service) {
		service.messageCreatedNotifier = notifier
	}
}

func NewService(store Store, ids IDGenerator, clock Clock, options ...ServiceOption) *Service {
	service := &Service{store: store, ids: ids, clock: clock}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func (service *Service) CreateDecisionFromThread(
	ctx context.Context,
	invocation Invocation,
	input CreateDecisionInput,
) (CreateDecisionResult, error) {
	if err := validateInvocation(invocation); err != nil {
		return CreateDecisionResult{}, err
	}
	question := strings.TrimSpace(input.Question)
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "thread", ID: input.ThreadID}); err != nil {
		return CreateDecisionResult{}, fmt.Errorf("%w: Thread reference: %v", authz.ErrInvalid, err)
	}
	if !validClientOperationID(input.ClientOperationID) || !validCollaborationText(input.Question, question) {
		return CreateDecisionResult{}, fmt.Errorf("%w: client operation ID and question are required", authz.ErrInvalid)
	}
	payloadSHA256 := collaborationPayloadDigest(struct {
		Question string `json:"question"`
	}{Question: question})

	decisionID, err := service.ids.NewID("dec_")
	if err != nil {
		return CreateDecisionResult{}, fmt.Errorf("generate decision ID: %w", err)
	}
	linkID, err := service.ids.NewID("lnk_")
	if err != nil {
		return CreateDecisionResult{}, fmt.Errorf("generate evidence link ID: %w", err)
	}
	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return CreateDecisionResult{}, fmt.Errorf("generate decision event ID: %w", err)
	}

	return service.store.CreateDecisionFromThread(ctx, CreateDecisionCommand{
		Invocation:        invocation,
		DecisionID:        decisionID,
		LinkID:            linkID,
		EventID:           eventID,
		ThreadID:          input.ThreadID,
		ClientOperationID: input.ClientOperationID,
		PayloadSHA256:     payloadSHA256,
		Question:          question,
		OccurredAt:        service.clock.Now().UTC(),
	})
}

func (service *Service) AcceptDecision(
	ctx context.Context,
	invocation Invocation,
	input AcceptDecisionInput,
) (AcceptDecisionResult, error) {
	if err := validateInvocation(invocation); err != nil {
		return AcceptDecisionResult{}, err
	}
	outcome := strings.TrimSpace(input.Outcome)
	rationale := strings.TrimSpace(input.Rationale)
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "decision", ID: input.DecisionID}); err != nil {
		return AcceptDecisionResult{}, fmt.Errorf("%w: Decision reference: %v", authz.ErrInvalid, err)
	}
	if !validClientOperationID(input.ClientOperationID) ||
		!validCollaborationText(input.Outcome, outcome) || !validCollaborationText(input.Rationale, rationale) {
		return AcceptDecisionResult{}, fmt.Errorf("%w: client operation ID, outcome, and rationale are required", authz.ErrInvalid)
	}
	payloadSHA256 := collaborationPayloadDigest(struct {
		Outcome   string `json:"outcome"`
		Rationale string `json:"rationale"`
	}{Outcome: outcome, Rationale: rationale})

	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return AcceptDecisionResult{}, fmt.Errorf("generate acceptance event ID: %w", err)
	}

	return service.store.AcceptDecision(ctx, AcceptDecisionCommand{
		Invocation:        invocation,
		EventID:           eventID,
		DecisionID:        input.DecisionID,
		ClientOperationID: input.ClientOperationID,
		PayloadSHA256:     payloadSHA256,
		Outcome:           outcome,
		Rationale:         rationale,
		OccurredAt:        service.clock.Now().UTC(),
	})
}

func (service *Service) CreateTicketFromDecision(
	ctx context.Context,
	invocation Invocation,
	input CreateTicketInput,
) (CreateTicketResult, error) {
	if err := validateInvocation(invocation); err != nil {
		return CreateTicketResult{}, err
	}
	title := strings.TrimSpace(input.Title)
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "decision", ID: input.DecisionID}); err != nil {
		return CreateTicketResult{}, fmt.Errorf("%w: Decision reference: %v", authz.ErrInvalid, err)
	}
	if !validClientOperationID(input.ClientOperationID) || !validCollaborationText(input.Title, title) {
		return CreateTicketResult{}, fmt.Errorf("%w: client operation ID and title are required", authz.ErrInvalid)
	}
	payloadSHA256 := collaborationPayloadDigest(struct {
		Title string `json:"title"`
	}{Title: title})

	ticketID, err := service.ids.NewID("tkt_")
	if err != nil {
		return CreateTicketResult{}, fmt.Errorf("generate ticket ID: %w", err)
	}
	linkID, err := service.ids.NewID("lnk_")
	if err != nil {
		return CreateTicketResult{}, fmt.Errorf("generate implementation link ID: %w", err)
	}
	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return CreateTicketResult{}, fmt.Errorf("generate ticket event ID: %w", err)
	}

	return service.store.CreateTicketFromDecision(ctx, CreateTicketCommand{
		Invocation:        invocation,
		TicketID:          ticketID,
		LinkID:            linkID,
		EventID:           eventID,
		DecisionID:        input.DecisionID,
		ClientOperationID: input.ClientOperationID,
		PayloadSHA256:     payloadSHA256,
		Title:             title,
		OccurredAt:        service.clock.Now().UTC(),
	})
}

func validCollaborationText(raw string, normalized string) bool {
	return utf8.ValidString(raw) && !strings.ContainsRune(raw, '\x00') && normalized != ""
}

func collaborationPayloadDigest(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode fixed collaboration command payload: %v", err))
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
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
