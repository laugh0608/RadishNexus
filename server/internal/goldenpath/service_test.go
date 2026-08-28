package goldenpath

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type recordingStore struct {
	createDecisionCommand CreateDecisionCommand
}

func (store *recordingStore) CreateDecisionFromThread(_ context.Context, command CreateDecisionCommand) (Decision, error) {
	store.createDecisionCommand = command
	return Decision{ID: command.DecisionID, Question: command.Question}, nil
}

func (*recordingStore) AcceptDecision(context.Context, AcceptDecisionCommand) (Decision, error) {
	return Decision{}, nil
}

func (*recordingStore) CreateTicketFromDecision(context.Context, CreateTicketCommand) (Ticket, error) {
	return Ticket{}, nil
}

func (*recordingStore) ListRelations(context.Context, authz.Principal, entityref.Ref) ([]RelationProjection, error) {
	return nil, nil
}

type sequenceIDs struct {
	values []string
}

func (ids *sequenceIDs) NewID(_ string) (string, error) {
	if len(ids.values) == 0 {
		return "", errors.New("ID sequence exhausted")
	}
	value := ids.values[0]
	ids.values = ids.values[1:]
	return value, nil
}

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

func TestCreateDecisionBuildsExplicitAtomicCommand(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	service := NewService(store, &sequenceIDs{values: []string{"dec_1", "lnk_1", "evt_1"}}, fixedClock{value: now})
	invocation := Invocation{
		Principal:     authz.Principal{Kind: authz.PrincipalUser, ID: "usr_1", WorkspaceID: "wrk_1"},
		SourceKind:    "api",
		CorrelationID: "cor_1",
	}

	decision, err := service.CreateDecisionFromThread(
		context.Background(),
		invocation,
		CreateDecisionInput{ThreadID: "thr_1", Question: "  Use rate limiting?  "},
	)
	if err != nil {
		t.Fatalf("CreateDecisionFromThread() error = %v", err)
	}
	if decision.ID != "dec_1" || store.createDecisionCommand.LinkID != "lnk_1" || store.createDecisionCommand.EventID != "evt_1" {
		t.Fatalf("generated identifiers were not kept in one command: %#v", store.createDecisionCommand)
	}
	if store.createDecisionCommand.Question != "Use rate limiting?" {
		t.Fatalf("question = %q", store.createDecisionCommand.Question)
	}
	if !store.createDecisionCommand.OccurredAt.Equal(now.UTC()) {
		t.Fatalf("OccurredAt = %v, want %v", store.createDecisionCommand.OccurredAt, now.UTC())
	}
}

func TestAcceptDecisionRejectsSystemPrincipalBeforeStore(t *testing.T) {
	t.Parallel()

	service := NewService(&recordingStore{}, &sequenceIDs{values: []string{"evt_1"}}, fixedClock{})
	_, err := service.AcceptDecision(context.Background(), Invocation{
		Principal:     authz.Principal{Kind: authz.PrincipalSystem, ID: "system", WorkspaceID: "wrk_1"},
		SourceKind:    "api",
		CorrelationID: "cor_1",
	}, AcceptDecisionInput{DecisionID: "dec_1", Outcome: "yes", Rationale: "because"})
	if !errors.Is(err, authz.ErrUnauthenticated) {
		t.Fatalf("AcceptDecision() error = %v, want unauthenticated", err)
	}
}
