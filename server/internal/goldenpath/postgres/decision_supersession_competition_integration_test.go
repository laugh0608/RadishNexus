//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func supersessionFixture(t *testing.T) (*contextFixture, []string) {
	t.Helper()
	f := contextDatabase(t)
	f.seed(t)
	ids := []string{f.decision, f.proposed}
	inv := contextInvocation(f.principal)
	if _, e := f.core.AcceptDecision(f.ctx, inv, goldenpath.AcceptDecisionInput{DecisionID: f.proposed, ClientOperationID: "competition-accept-b", Outcome: "B", Rationale: "B evidence"}); e != nil {
		t.Fatal(e)
	}
	c, e := f.core.CreateDecisionFromThread(f.ctx, inv, goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: "competition-propose-c", Question: "C conclusion?"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.core.AcceptDecision(f.ctx, inv, goldenpath.AcceptDecisionInput{DecisionID: c.Decision.ID, ClientOperationID: "competition-accept-c", Outcome: "C", Rationale: "C evidence"}); e != nil {
		t.Fatal(e)
	}
	return f, append(ids, c.Decision.ID)
}
func supersessionInput(old, new, op string) goldenpath.SupersedeDecisionInput {
	return goldenpath.SupersedeDecisionInput{DecisionID: old, ReplacementID: new, ClientOperationID: op, BaseDecisionRevision: 2, BaseReplacementRevision: 2, SupersessionReason: "Explicit conclusion replacement"}
}
func waitSupersessionDecisionLock(t *testing.T, f *contextFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	waitProjectionLock(t, ctx, f.pool, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM radishnexus.decisions%' AND query NOT LIKE '%pg_stat_activity%')`)
}
func TestDecisionSupersessionDeterministicCompetition(t *testing.T) {
	for _, test := range []struct {
		name       string
		a, b, c, d int
		duplicate  bool
	}{
		{"two replacements", 0, 1, 0, 2, false}, {"two replacements reversed", 0, 2, 0, 1, false}, {"one replacement", 0, 2, 1, 2, false}, {"one replacement reversed", 1, 2, 0, 2, false},
		{"mutual", 0, 1, 1, 0, false}, {"mutual reversed", 1, 0, 0, 1, false}, {"chain forward", 0, 1, 1, 2, false}, {"chain reverse", 1, 2, 0, 1, false},
		{"same operation", 0, 1, 0, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, ids := supersessionFixture(t)
			first := supersessionInput(ids[test.a], ids[test.b], "ordered-first")
			second := supersessionInput(ids[test.c], ids[test.d], "ordered-second")
			if test.duplicate {
				second = first
			}
			release := contextGate(t, f, "collaboration_command_receipts")
			one, two := make(chan error, 1), make(chan error, 1)
			go func() { _, e := f.core.SupersedeDecision(f.ctx, contextInvocation(f.principal), first); one <- e }()
			contextWaitGate(t, f)
			go func() { _, e := f.core.SupersedeDecision(f.ctx, contextInvocation(f.principal), second); two <- e }()
			waitSupersessionDecisionLock(t, f)
			release()
			if e := <-one; e != nil {
				t.Fatal("first outcome", e)
			}
			if e := <-two; (test.duplicate && e != nil) || (!test.duplicate && !errors.Is(e, authz.ErrConflict)) {
				t.Fatal("second outcome", e)
			}
			var count int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.domain_events WHERE event_type='decision.superseded'`).Scan(&count); e != nil || count != 1 {
				t.Fatal("competition produced extra facts", count, e)
			}
			if test.name == "chain forward" {
				second.BaseDecisionRevision = 3
				if _, e := f.core.SupersedeDecision(f.ctx, contextInvocation(f.principal), second); e != nil {
					t.Fatal("fresh chain extension", e)
				}
			}
		})
	}
}
func TestDecisionSupersessionTicketCreationOrdering(t *testing.T) {
	for _, ticketFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(ticketFirst), func(t *testing.T) {
			f, ids := supersessionFixture(t)
			inv := contextInvocation(f.principal)
			create := func() error {
				_, e := f.core.CreateTicketFromDecision(f.ctx, inv, goldenpath.CreateTicketInput{DecisionID: ids[0], ClientOperationID: "ordered-ticket", Title: "Created before replacement only"})
				return e
			}
			replace := func() error {
				_, e := f.core.SupersedeDecision(f.ctx, inv, supersessionInput(ids[0], ids[1], "ordered-replacement"))
				return e
			}
			first, second := replace, create
			if ticketFirst {
				first, second = create, replace
			}
			release := contextGate(t, f, "collaboration_command_receipts")
			one, two := make(chan error, 1), make(chan error, 1)
			go func() { one <- first() }()
			contextWaitGate(t, f)
			go func() { two <- second() }()
			waitSupersessionDecisionLock(t, f)
			release()
			if e := <-one; e != nil {
				t.Fatal("first writer", e)
			}
			if e := <-two; (ticketFirst && e != nil) || (!ticketFirst && !errors.Is(e, authz.ErrConflict)) {
				t.Fatal("second writer", e)
			}
			var count int
			var valid bool
			e := f.pool.QueryRow(f.ctx, `SELECT count(*),COALESCE(bool_and(t.created_at<d.updated_at),true) FROM radishnexus.tickets t JOIN radishnexus.entity_links l ON l.workspace_id=t.workspace_id AND l.from_id=t.id AND l.relation_type='implements' JOIN radishnexus.decisions d ON d.workspace_id=l.workspace_id AND d.id=l.to_id WHERE d.id=$1`, ids[0]).Scan(&count, &valid)
			expected := 1
			if ticketFirst {
				expected = 2
			}
			if e != nil || count != expected || !valid {
				t.Fatal("Ticket/source chronology", count, valid, e)
			}
		})
	}
}

func TestDecisionSupersessionEvidenceRevocationOrdering(t *testing.T) {
	for _, writerFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("writer_first_%t", writerFirst), func(t *testing.T) {
			f, ids := supersessionFixture(t)
			input := supersessionInput(ids[0], ids[1], "supersession-revoke-race")
			write := func() error {
				_, e := f.core.SupersedeDecision(f.ctx, contextInvocation(f.principal), input)
				return e
			}
			wait := func() {
				ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
				defer cancel()
				// Deletion also advances the Thread management revision, so a
				// later reader waits on the parent before reaching membership.
				waitProjectionLock(t, ctx, f.pool, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND (query LIKE '%radishnexus.thread_memberships%' OR query LIKE '%radishnexus.threads%'))`)
			}
			remove := `DELETE FROM radishnexus.thread_memberships WHERE workspace_id=$1 AND thread_id=$2 AND user_id=$3`
			done := make(chan error, 1)
			if writerFirst {
				release := contextGate(t, f, "collaboration_command_receipts")
				go func() { done <- write() }()
				contextWaitGate(t, f)
				revoked := make(chan error, 1)
				go func() {
					_, e := f.pool.Exec(f.ctx, remove, f.workspace, f.thread, f.principal.ID)
					revoked <- e
				}()
				wait()
				release()
				if e := <-done; e != nil {
					t.Fatal("authorized writer", e)
				}
				if e := <-revoked; e != nil {
					t.Fatal("later revocation", e)
				}
			} else {
				tx, e := f.pool.Begin(f.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback(f.ctx)
				if _, e = tx.Exec(f.ctx, remove, f.workspace, f.thread, f.principal.ID); e != nil {
					t.Fatal(e)
				}
				go func() { done <- write() }()
				wait()
				if e = tx.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
				if e = <-done; !errors.Is(e, authz.ErrForbidden) {
					t.Fatal("revoked writer", e)
				}
			}
			if e := write(); !errors.Is(e, authz.ErrForbidden) {
				t.Fatal("retry bypassed current evidence", e)
			}
			var status string
			var events int
			if e := f.pool.QueryRow(f.ctx, `SELECT status,(SELECT count(*) FROM radishnexus.domain_events WHERE primary_entity_id=d.id AND event_type='decision.superseded') FROM radishnexus.decisions d WHERE id=$1`, ids[0]).Scan(&status, &events); e != nil {
				t.Fatal(e)
			}
			if (writerFirst && (status != "superseded" || events != 1)) || (!writerFirst && (status != "accepted" || events != 0)) {
				t.Fatal("revocation order outcome", status, events)
			}
		})
	}
}
