//go:build integration

package postgres_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestDecisionSupersessionChainRetryAndPortableHistory(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	inv := contextInvocation(f.principal)
	if _, e := f.core.AcceptDecision(f.ctx, inv, goldenpath.AcceptDecisionInput{DecisionID: f.proposed, ClientOperationID: "supersession-accept-b", Outcome: "New conclusion", Rationale: "New evidence"}); e != nil {
		t.Fatal(e)
	}
	input := goldenpath.SupersedeDecisionInput{DecisionID: f.decision, ReplacementID: f.proposed, ClientOperationID: "supersession-a-b", BaseDecisionRevision: 2, BaseReplacementRevision: 2, SupersessionReason: "  Preserve the old conclusion and its execution.  "}
	result, e := f.core.SupersedeDecision(f.ctx, inv, input)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.core.SupersedeDecision(f.ctx, inv, input)
	if e != nil || !reflect.DeepEqual(again, result) {
		t.Fatal("exact supersession retry", e)
	}
	for _, change := range []func(*goldenpath.SupersedeDecisionInput){
		func(v *goldenpath.SupersedeDecisionInput) { v.SupersessionReason = "Different reason" },
		func(v *goldenpath.SupersedeDecisionInput) { v.BaseDecisionRevision++ },
		func(v *goldenpath.SupersedeDecisionInput) { v.BaseReplacementRevision++ },
	} {
		changed := input
		change(&changed)
		if _, err := f.core.SupersedeDecision(f.ctx, inv, changed); !errors.Is(err, authz.ErrConflict) {
			t.Fatal("changed original operation", err)
		}
	}
	proposal, e := f.core.CreateDecisionFromThread(f.ctx, inv, goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: "source-proposal", Question: "Which historical choice?"})
	if e != nil || proposal.Created || proposal.Decision.ID != f.decision || proposal.Decision.Status != "superseded" || proposal.Decision.DecisionRevision != 3 {
		t.Fatal("original proposal current result", e)
	}
	for _, id := range []string{f.decision, f.proposed} {
		view, e := f.core.GetNexusView(f.ctx, f.principal, entityref.Ref{Type: "decision", ID: id})
		if e != nil || view.Current.DecisionRevision != 3 || len(view.Timeline) != 3 {
			t.Fatal("supersession view", e, view)
		}
		if id == f.decision && (view.Current.Status != "superseded" || view.DecisionCapabilities.CreateTicket) {
			t.Fatal("old decision remained executable")
		}
	}
	accepted, e := f.core.AcceptDecision(f.ctx, inv, goldenpath.AcceptDecisionInput{DecisionID: f.decision, ClientOperationID: "source-acceptance", Outcome: "Accept synthetic option", Rationale: "Preserve evidence"})
	if e != nil || accepted.Decision.Status != "superseded" || accepted.Accepted {
		t.Fatal("original acceptance retry", e)
	}
	ticket, e := f.core.CreateTicketFromDecision(f.ctx, inv, goldenpath.CreateTicketInput{DecisionID: f.decision, ClientOperationID: "source-ticket", Title: "Historical work"})
	if e != nil || ticket.Created || ticket.Ticket.ID != f.ticket {
		t.Fatal("original Ticket retry", e)
	}
	if _, e = f.core.CreateTicketFromDecision(f.ctx, inv, goldenpath.CreateTicketInput{DecisionID: f.decision, ClientOperationID: "after-supersession-ticket", Title: "Must fail"}); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("new Ticket accepted superseded source", e)
	}
	c, e := f.core.CreateDecisionFromThread(f.ctx, inv, goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: "supersession-proposal-c", Question: "Third conclusion?"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.core.AcceptDecision(f.ctx, inv, goldenpath.AcceptDecisionInput{DecisionID: c.Decision.ID, ClientOperationID: "supersession-accept-c", Outcome: "Third", Rationale: "Next evidence"}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.core.SupersedeDecision(f.ctx, inv, goldenpath.SupersedeDecisionInput{DecisionID: f.proposed, ReplacementID: c.Decision.ID, ClientOperationID: "supersession-b-c", BaseDecisionRevision: 3, BaseReplacementRevision: 2, SupersessionReason: "Continue without rewriting history"}); e != nil {
		t.Fatal(e)
	}
	if again, e = f.core.SupersedeDecision(f.ctx, inv, input); e != nil || !reflect.DeepEqual(again, result) {
		t.Fatal("original supersession after chain extension", e)
	}
	before, e := f.core.GetNexusView(f.ctx, f.principal, entityref.Ref{Type: "decision", ID: f.proposed})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.RebuildActivityProjection(f.ctx); e != nil {
		t.Fatal(e)
	}
	after, e := f.core.GetNexusView(f.ctx, f.principal, entityref.Ref{Type: "decision", ID: f.proposed})
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("projection rebuild", e)
	}
	b := f.bundle(t)
	for round := 0; round < 2; round++ {
		target := contextDatabase(t)
		plan := target.plan(t, b)
		r, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, plan, plan.PlanDigest)
		if e != nil || r.Status != "applied" {
			var pgerr *pgconn.PgError
			if errors.As(e, &pgerr) {
				t.Fatalf("portable supersession %d: %s %s %s", round, pgerr.Code, pgerr.Message, pgerr.Where)
			}
			t.Fatal("portable supersession", round, e)
		}
		b, e = target.service.Export(target.ctx, target.principal, target.owner.Token, r.ProjectID)
		if e != nil {
			t.Fatal("re-export supersession", round, e)
		}
	}
}
