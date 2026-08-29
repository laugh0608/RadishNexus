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
	recordCIRunCommand    RecordCompletedCIRunCommand
	nexusPrincipal        authz.Principal
	nexusTarget           entityref.Ref
	nexusView             NexusView
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

func (store *recordingStore) RecordCompletedCIRun(
	_ context.Context,
	command RecordCompletedCIRunCommand,
) (CIRunReceipt, error) {
	store.recordCIRunCommand = command
	return CIRunReceipt{CIRun: CIRun{ID: command.CIRunID}}, nil
}

func (*recordingStore) ListRelations(context.Context, authz.Principal, entityref.Ref) ([]RelationProjection, error) {
	return nil, nil
}

func (store *recordingStore) GetNexusView(
	_ context.Context,
	principal authz.Principal,
	target entityref.Ref,
) (NexusView, error) {
	store.nexusPrincipal = principal
	store.nexusTarget = target
	return store.nexusView, nil
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

func TestRecordCompletedJenkinsRunBuildsVerifiedAtomicCommand(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	startedAt := time.Date(2026, 8, 29, 10, 0, 0, 0, time.FixedZone("source", 8*60*60))
	completedAt := startedAt.Add(7 * time.Minute)
	recordedAt := time.Date(2026, 8, 29, 2, 8, 0, 0, time.UTC)
	service := NewService(
		store,
		&sequenceIDs{values: []string{"cir_1", "evt_1", "cor_1"}},
		fixedClock{value: recordedAt},
	)

	receipt, err := service.RecordCompletedJenkinsRun(
		context.Background(),
		VerifiedJenkinsDelivery{
			WorkspaceID:   "wrk_1",
			SourceID:      "jenkins-main",
			DeliveryID:    "delivery-1",
			PayloadSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		RecordCompletedCIRunInput{
			ComponentID:    "cmp_1",
			ExternalRunKey: "auth-service/42",
			Status:         "succeeded",
			StartedAt:      &startedAt,
			CompletedAt:    completedAt,
		},
	)
	if err != nil {
		t.Fatalf("RecordCompletedJenkinsRun() error = %v", err)
	}
	command := store.recordCIRunCommand
	if receipt.CIRun.ID != "cir_1" || command.EventID != "evt_1" || command.CorrelationID != "cor_1" {
		t.Fatalf("generated identifiers were not kept in one command: %#v", command)
	}
	if command.StartedAt == nil || !command.StartedAt.Equal(startedAt.UTC()) ||
		!command.CompletedAt.Equal(completedAt.UTC()) || !command.RecordedAt.Equal(recordedAt) {
		t.Fatalf("command times were not normalized to UTC: %#v", command)
	}
}

func TestRecordCompletedJenkinsRunRejectsUnverifiedFactsBeforeStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		delivery VerifiedJenkinsDelivery
		input    RecordCompletedCIRunInput
	}{
		{
			name: "uppercase digest",
			delivery: VerifiedJenkinsDelivery{
				WorkspaceID: "wrk_1", SourceID: "jenkins-main", DeliveryID: "delivery-1",
				PayloadSHA256: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			},
			input: validCompletedCIRunInput(),
		},
		{
			name:     "nonterminal status",
			delivery: validVerifiedJenkinsDelivery(),
			input: func() RecordCompletedCIRunInput {
				input := validCompletedCIRunInput()
				input.Status = "running"
				return input
			}(),
		},
		{
			name:     "completion before start",
			delivery: validVerifiedJenkinsDelivery(),
			input: func() RecordCompletedCIRunInput {
				input := validCompletedCIRunInput()
				startedAt := input.CompletedAt.Add(time.Minute)
				input.StartedAt = &startedAt
				return input
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &recordingStore{}
			service := NewService(store, &sequenceIDs{values: []string{"cir_1", "evt_1", "cor_1"}}, fixedClock{})
			_, err := service.RecordCompletedJenkinsRun(context.Background(), test.delivery, test.input)
			if !errors.Is(err, authz.ErrInvalid) {
				t.Fatalf("RecordCompletedJenkinsRun() error = %v, want invalid", err)
			}
			if store.recordCIRunCommand.CIRunID != "" {
				t.Fatalf("invalid input reached Store: %#v", store.recordCIRunCommand)
			}
		})
	}
}

func validVerifiedJenkinsDelivery() VerifiedJenkinsDelivery {
	return VerifiedJenkinsDelivery{
		WorkspaceID:   "wrk_1",
		SourceID:      "jenkins-main",
		DeliveryID:    "delivery-1",
		PayloadSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
}

func validCompletedCIRunInput() RecordCompletedCIRunInput {
	return RecordCompletedCIRunInput{
		ComponentID:    "cmp_1",
		ExternalRunKey: "auth-service/42",
		Status:         "succeeded",
		CompletedAt:    time.Date(2026, 8, 29, 2, 7, 0, 0, time.UTC),
	}
}

func TestGetNexusViewValidatesTargetAndForwardsPrincipal(t *testing.T) {
	t.Parallel()

	principal := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_1", WorkspaceID: "wrk_1"}
	target := entityref.Ref{Type: "decision", ID: "dec_1"}
	want := NexusView{Current: CurrentProjection{Ref: target, Status: "accepted"}}
	store := &recordingStore{nexusView: want}
	service := NewService(store, &sequenceIDs{}, fixedClock{})

	got, err := service.GetNexusView(context.Background(), principal, target)
	if err != nil {
		t.Fatalf("GetNexusView() error = %v", err)
	}
	if got.Current.Status != "accepted" || store.nexusPrincipal != principal || store.nexusTarget != target {
		t.Fatalf("GetNexusView() = %#v, store principal = %#v, target = %#v", got, store.nexusPrincipal, store.nexusTarget)
	}

	ciRunTarget := entityref.Ref{Type: "ci-run", ID: "cir_1"}
	_, err = service.GetNexusView(context.Background(), principal, ciRunTarget)
	if err != nil || store.nexusTarget != ciRunTarget {
		t.Fatalf("CI Run GetNexusView() error = %v, target = %#v", err, store.nexusTarget)
	}

	_, err = service.GetNexusView(
		context.Background(),
		principal,
		entityref.Ref{Type: "thread", ID: "thr_1"},
	)
	if !errors.Is(err, authz.ErrInvalid) {
		t.Fatalf("Thread GetNexusView() error = %v, want invalid", err)
	}
}
