//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
)

func TestDecisionRejectionPermissionsRetryAndRollback(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	input := goldenpath.RejectDecisionInput{DecisionID: f.proposed, ClientOperationID: "reject-proposal", RejectionReason: "  Reason is public to Decision readers.  "}
	if _, e := f.core.RejectDecision(f.ctx, contextInvocation(f.member), input); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("contributor rejected", e)
	}
	contextSQL(t, f, `UPDATE radishnexus.project_memberships SET role='decider' WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3`, f.workspace, f.project, f.member.ID)
	if _, e := f.core.RejectDecision(f.ctx, contextInvocation(f.member), input); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("missing private evidence", e)
	}
	v, e := f.core.GetNexusView(f.ctx, f.member, entityref.Ref{Type: "decision", ID: f.proposed})
	if e != nil || v.DecisionCapabilities == nil || v.DecisionCapabilities.Reject || v.DecisionCapabilities.Accept {
		t.Fatal("read-only evidence capability", e)
	}
	beforeTickets, beforeDocuments := contextCount(t, f, "tickets"), contextCount(t, f, "documents")
	r, e := f.core.RejectDecision(f.ctx, contextInvocation(f.principal), input)
	if e != nil || !r.Rejected || r.Decision.Status != "rejected" || r.Decision.RejectionReason != "Reason is public to Decision readers." {
		t.Fatal("reject", e, r)
	}
	if r.Decision.DecidedAt == nil || !r.Decision.DecidedAt.After(r.Decision.CreatedAt) || !r.Decision.DecidedAt.Equal(r.Decision.UpdatedAt) {
		t.Fatal("decision clock")
	}
	again, e := f.core.RejectDecision(f.ctx, contextInvocation(f.principal), input)
	if e != nil || again.Rejected || !reflect.DeepEqual(again.Decision, r.Decision) {
		t.Fatal("exact retry", e)
	}
	changed := input
	changed.RejectionReason = "Changed reason"
	if _, e = f.core.RejectDecision(f.ctx, contextInvocation(f.principal), changed); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("changed replay", e)
	}
	changed = input
	changed.ClientOperationID = "new-reject"
	if _, e = f.core.RejectDecision(f.ctx, contextInvocation(f.principal), changed); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("different operation", e)
	}
	if _, e = f.core.AcceptDecision(f.ctx, contextInvocation(f.principal), goldenpath.AcceptDecisionInput{DecisionID: f.proposed, ClientOperationID: "late-accept", Outcome: "late", Rationale: "late"}); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("stale acceptance", e)
	}
	if _, e = f.core.CreateTicketFromDecision(f.ctx, contextInvocation(f.principal), goldenpath.CreateTicketInput{DecisionID: f.proposed, ClientOperationID: "rejected-ticket", Title: "Must fail"}); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("rejected execution source", e)
	}
	proposal, e := f.core.CreateDecisionFromThread(f.ctx, contextInvocation(f.principal), goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: "source-second-proposal", Question: "Still a proposal?"})
	if e != nil || proposal.Created || proposal.Decision.ID != f.proposed || proposal.Decision.Status != "rejected" {
		t.Fatal("proposal retry", e)
	}
	if contextCount(t, f, "tickets") != beforeTickets || contextCount(t, f, "documents") != beforeDocuments {
		t.Fatal("execution objects changed")
	}
	v, e = f.core.GetNexusView(f.ctx, f.principal, entityref.Ref{Type: "decision", ID: f.proposed})
	if e != nil || v.DecisionCapabilities.Reject || v.DecisionCapabilities.CreateTicket || len(v.Timeline) != 2 {
		t.Fatal("rejected view", e, v)
	}
	if _, e = f.store.RebuildActivityProjection(f.ctx); e != nil {
		t.Fatal(e)
	}
	rebuilt, e := f.core.GetNexusView(f.ctx, f.principal, entityref.Ref{Type: "decision", ID: f.proposed})
	if e != nil || !reflect.DeepEqual(v, rebuilt) {
		t.Fatal("projection changed", e)
	}
	contextSQL(t, f, `DELETE FROM radishnexus.thread_memberships WHERE workspace_id=$1 AND thread_id=$2 AND user_id=$3`, f.workspace, f.thread, f.principal.ID)
	if _, e = f.core.RejectDecision(f.ctx, contextInvocation(f.principal), input); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("retry bypassed evidence revocation", e)
	}
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'manager')`, f.workspace, f.thread, f.principal.ID)
	if _, e = f.core.RejectDecision(f.ctx, contextInvocation(f.principal), input); e != nil {
		t.Fatal("restored exact retry", e)
	}
	newDecision, e := f.core.CreateDecisionFromThread(f.ctx, contextInvocation(f.principal), goldenpath.CreateDecisionInput{ThreadID: f.thread, ClientOperationID: "fault-proposal", Question: "Atomic rejection?"})
	if e != nil {
		t.Fatal(e)
	}
	before := contextCount(t, f, "domain_events")
	contextSQL(t, f, `CREATE FUNCTION public.reject_activity_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic projection failure'; END $$; CREATE TRIGGER reject_activity_fault BEFORE INSERT ON radishnexus.activity_items FOR EACH STATEMENT EXECUTE FUNCTION public.reject_activity_fault()`)
	fault := goldenpath.RejectDecisionInput{DecisionID: newDecision.Decision.ID, ClientOperationID: "fault-reject", RejectionReason: "Only if atomic"}
	if _, e = f.core.RejectDecision(f.ctx, contextInvocation(f.principal), fault); e == nil {
		t.Fatal("projection fault ignored")
	}
	contextSQL(t, f, `DROP TRIGGER reject_activity_fault ON radishnexus.activity_items; DROP FUNCTION public.reject_activity_fault()`)
	if contextCount(t, f, "domain_events") != before {
		t.Fatal("partial event committed")
	}
	var status string
	if e = f.pool.QueryRow(f.ctx, `SELECT status FROM radishnexus.decisions WHERE id=$1`, fault.DecisionID).Scan(&status); e != nil || status != "proposed" {
		t.Fatal("partial decision committed", e)
	}
	if _, e = f.core.RejectDecision(f.ctx, contextInvocation(f.principal), fault); e != nil {
		t.Fatal("same operation after rollback", e)
	}
}

func TestDecisionAcceptRejectOrderedCompetition(t *testing.T) {
	for _, test := range []struct {
		first, second string
		duplicate     bool
	}{
		{"accepted", "rejected", false}, {"rejected", "accepted", false},
		{"accepted", "accepted", false}, {"rejected", "rejected", false},
		{"accepted", "accepted", true}, {"rejected", "rejected", true},
	} {
		t.Run(fmt.Sprintf("%s_%s_duplicate_%t", test.first, test.second, test.duplicate), func(t *testing.T) {
			f := contextDatabase(t)
			f.seed(t)
			block, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer block.Rollback(f.ctx)
			if _, e = block.Exec(f.ctx, `SELECT id FROM radishnexus.decisions WHERE id=$1 FOR UPDATE`, f.proposed); e != nil {
				t.Fatal(e)
			}
			command := func(status, operation string) error {
				if status == "accepted" {
					_, e := f.core.AcceptDecision(f.ctx, contextInvocation(f.principal), goldenpath.AcceptDecisionInput{DecisionID: f.proposed, ClientOperationID: operation, Outcome: "Chosen", Rationale: "Chosen reason"})
					return e
				}
				_, e := f.core.RejectDecision(f.ctx, contextInvocation(f.principal), goldenpath.RejectDecisionInput{DecisionID: f.proposed, ClientOperationID: operation, RejectionReason: "Not chosen"})
				return e
			}
			a, b := make(chan error, 1), make(chan error, 1)
			wait := func(want int) {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					var n int
					e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM radishnexus.decisions%'`).Scan(&n)
					if e != nil {
						t.Fatal(e)
					}
					if n >= want {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("decision writers did not reach lock")
			}
			go func() { a <- command(test.first, "competition-one") }()
			wait(1)
			operation := "competition-two"
			if test.duplicate {
				operation = "competition-one"
			}
			go func() { b <- command(test.second, operation) }()
			wait(2)
			if e = block.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			if e = <-a; e != nil {
				t.Fatal("first writer", e)
			}
			if e = <-b; (test.duplicate && e != nil) || (!test.duplicate && !errors.Is(e, authz.ErrConflict)) {
				t.Fatal("second writer", e)
			}
			var status string
			var n int
			if e = f.pool.QueryRow(f.ctx, `SELECT status,(SELECT count(*) FROM radishnexus.domain_events WHERE primary_entity_id=d.id AND event_type IN ('decision.accepted','decision.rejected')) FROM radishnexus.decisions d WHERE id=$1`, f.proposed).Scan(&status, &n); e != nil || status != test.first || n != 1 {
				t.Fatal("competition outcome", status, n, e)
			}
		})
	}
}

func TestDecisionRejectionEvidenceRevocationOrdering(t *testing.T) {
	for _, writerFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("writer_first_%t", writerFirst), func(t *testing.T) {
			f := contextDatabase(t)
			f.seed(t)
			input := goldenpath.RejectDecisionInput{DecisionID: f.proposed, ClientOperationID: "revoke-race", RejectionReason: "Only with current evidence access"}
			write := func() error {
				_, e := f.core.RejectDecision(f.ctx, contextInvocation(f.principal), input)
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
			if e := f.pool.QueryRow(f.ctx, `SELECT status,(SELECT count(*) FROM radishnexus.domain_events WHERE primary_entity_id=d.id AND event_type='decision.rejected') FROM radishnexus.decisions d WHERE id=$1`, f.proposed).Scan(&status, &events); e != nil {
				t.Fatal(e)
			}
			if (writerFirst && (status != "rejected" || events != 1)) || (!writerFirst && (status != "proposed" || events != 0)) {
				t.Fatal("revocation order outcome", status, events)
			}
		})
	}
}

func TestDecisionRejectionContextTwoRoundTrips(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	if _, e := f.core.RejectDecision(f.ctx, contextInvocation(f.principal), goldenpath.RejectDecisionInput{DecisionID: f.proposed, ClientOperationID: "portable-rejection", RejectionReason: "Keep original rejected reasoning"}); e != nil {
		t.Fatal(e)
	}
	b := f.bundle(t)
	oldID := f.proposed
	if b.Manifest.Profile != cp.Profile {
		t.Fatal("wrong profile")
	}
	for round := 0; round < 2; round++ {
		target := contextDatabase(t)
		p := target.plan(t, b)
		result, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, b, p, p.PlanDigest)
		if e != nil || result.Status != "applied" {
			t.Fatal("import", round, e)
		}
		id := contextMapping(t, p, "decision", oldID)
		v, e := target.core.GetNexusView(target.ctx, target.principal, entityref.Ref{Type: "decision", ID: id})
		if e != nil || v.Current.Status != "rejected" || v.Current.RejectionReason != "Keep original rejected reasoning" {
			t.Fatal("imported rejection", round, e)
		}
		if _, e = target.store.RebuildActivityProjection(target.ctx); e != nil {
			t.Fatal(e)
		}
		if round == 0 {
			proposed, e := target.core.CreateDecisionFromThread(target.ctx, contextInvocation(target.principal), goldenpath.CreateDecisionInput{ThreadID: contextMapping(t, p, "thread", f.thread), ClientOperationID: "target-proposal", Question: "Target new proposal"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = target.core.RejectDecision(target.ctx, contextInvocation(target.principal), goldenpath.RejectDecisionInput{DecisionID: proposed.Decision.ID, ClientOperationID: "target-reject", RejectionReason: "Current member explicitly rejected"}); e != nil {
				t.Fatal(e)
			}
		}
		b, e = target.service.Export(target.ctx, target.principal, target.owner.Token, result.ProjectID)
		if e != nil {
			t.Fatal("re-export", round, e)
		}
		oldID = id
		t.Log(fmt.Sprintf("round %d preserved rejected reason, source and execution objects", round+1))
	}
}

func TestDecisionRejectionFivePointRollbackAndLostCommit(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	input := goldenpath.RejectDecisionInput{DecisionID: f.proposed, ClientOperationID: "five-point-rejection", RejectionReason: "Atomic reason"}
	tables := []string{"decisions", "collaboration_command_receipts", "domain_events", "outbox_deliveries", "activity_items"}
	baseline := map[string]int{}
	for _, table := range tables {
		baseline[table] = contextCount(t, f, table)
	}
	for _, table := range tables {
		event := "INSERT"
		if table == "decisions" {
			event = "UPDATE"
		}
		contextSQL(t, f, `CREATE FUNCTION public.rejection_write_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'isolated rejection write failure'; END $$; CREATE TRIGGER rejection_write_fault BEFORE `+event+` ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION public.rejection_write_fault()`)
		if _, e := f.core.RejectDecision(f.ctx, contextInvocation(f.principal), input); e == nil {
			t.Fatal("write fault ignored", table)
		}
		contextSQL(t, f, `DROP TRIGGER rejection_write_fault ON radishnexus.`+table+`; DROP FUNCTION public.rejection_write_fault()`)
		for _, check := range tables {
			if contextCount(t, f, check) != baseline[check] {
				t.Fatal("partial write", table, check)
			}
		}
		var status string
		if e := f.pool.QueryRow(f.ctx, `SELECT status FROM radishnexus.decisions WHERE id=$1`, f.proposed).Scan(&status); e != nil || status != "proposed" {
			t.Fatal("partial outcome", table, status, e)
		}
	}
	config := f.pool.Config().Copy()
	proxy, finish := pgfault.LoseCommit(t, config.ConnConfig)
	config.ConnConfig = proxy
	config.MaxConns = 1
	faulty, e := pgxpool.NewWithConfig(f.ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	core := goldenpath.NewService(goldenpostgres.New(faulty), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	_, e = core.RejectDecision(f.ctx, contextInvocation(f.principal), input)
	faulty.Close()
	finish()
	if e == nil {
		t.Fatal("lost confirmation reported success")
	}
	retry, e := f.core.RejectDecision(f.ctx, contextInvocation(f.principal), input)
	if e != nil || retry.Rejected || retry.Decision.Status != "rejected" {
		t.Fatal("committed outcome retry", e)
	}
	if _, e = f.pool.Exec(f.ctx, `UPDATE radishnexus.decisions SET rejection_reason='Changed' WHERE id=$1`, f.proposed); e == nil {
		t.Fatal("rejected reason mutable")
	}
	var count int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.domain_events WHERE primary_entity_id=$1 AND event_type='decision.rejected'`, f.proposed).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate rejected event", count, e)
	}
	if _, e = f.pool.Exec(f.ctx, `INSERT INTO radishnexus.domain_events(event_id,event_type,schema_version,workspace_id,actor_kind,actor_id,source_kind,primary_entity_type,primary_entity_id,project_id,correlation_id,payload,occurred_at) VALUES('evt_forged_rejection','decision.rejected',1,$1,'user',$2,'web','decision','dec_forged',$3,'cor_forged','{"status":"rejected"}',clock_timestamp())`, f.workspace, f.principal.ID, f.project); e == nil {
		t.Fatal("forged native rejection event")
	}
}

func TestDecisionRejectionCurrentAdmissionAndRoles(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	// These SQL changes are explicit permission/failure fixtures; Decisions still use formal commands.
	contextSQL(t, f, `INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) VALUES($1,$2,$3,'member')`, f.workspace, f.thread, f.member.ID)
	input := goldenpath.RejectDecisionInput{DecisionID: f.proposed, ClientOperationID: "admission-reject", RejectionReason: "Only a current decider may reject"}
	check := func(want error) {
		t.Helper()
		_, e := f.core.RejectDecision(f.ctx, contextInvocation(f.member), input)
		if !errors.Is(e, want) {
			t.Fatal("admission boundary", want, e)
		}
	}
	for _, role := range []string{"viewer", "contributor"} {
		contextSQL(t, f, `UPDATE radishnexus.project_memberships SET role=$1 WHERE project_id=$2 AND user_id=$3`, role, f.project, f.member.ID)
		check(authz.ErrForbidden)
	}
	contextSQL(t, f, `UPDATE radishnexus.workspace_memberships SET role='owner',role_revision=role_revision+1 WHERE workspace_id=$1 AND user_id=$2`, f.workspace, f.member.ID)
	check(authz.ErrForbidden)
	contextSQL(t, f, `UPDATE radishnexus.workspace_memberships SET role='member',role_revision=role_revision+1 WHERE workspace_id=$1 AND user_id=$2`, f.workspace, f.member.ID)
	contextSQL(t, f, `UPDATE radishnexus.project_memberships SET role='decider' WHERE project_id=$1 AND user_id=$2`, f.project, f.member.ID)
	contextSQL(t, f, `UPDATE radishnexus.projects SET status='archived' WHERE id=$1`, f.project)
	check(authz.ErrConflict)
	contextSQL(t, f, `UPDATE radishnexus.projects SET status='active' WHERE id=$1`, f.project)
	for _, status := range []string{"suspended", "removed"} {
		contextSQL(t, f, `UPDATE radishnexus.workspace_memberships SET status=$1 WHERE workspace_id=$2 AND user_id=$3`, status, f.workspace, f.member.ID)
		check(authz.ErrNotFound)
		contextSQL(t, f, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE workspace_id=$1 AND user_id=$2`, f.workspace, f.member.ID)
	}
	contextSQL(t, f, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, f.member.ID)
	check(authn.ErrInvalidSession)
	contextSQL(t, f, `UPDATE radishnexus.user_accounts SET status='active' WHERE user_id=$1`, f.member.ID)
	other := f.member
	other.WorkspaceID = "wrk_other"
	if _, e := f.core.RejectDecision(f.ctx, contextInvocation(other), input); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("cross Workspace", e)
	}
	contextSQL(t, f, `DELETE FROM radishnexus.project_memberships WHERE project_id=$1 AND user_id=$2`, f.project, f.member.ID)
	check(authz.ErrNotFound)
	contextSQL(t, f, `UPDATE radishnexus.projects SET visibility='workspace' WHERE id=$1`, f.project)
	check(authz.ErrForbidden)
	var events int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.domain_events WHERE event_type='decision.rejected'`).Scan(&events); e != nil || events != 0 {
		t.Fatal("denied operation created fact", e)
	}
}
