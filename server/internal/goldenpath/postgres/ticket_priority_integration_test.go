//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
)

func priorityInput(t *testing.T, ctx context.Context, s *goldenpath.TicketPriorityService, inv goldenpath.Invocation, ticket, priority, operation string) goldenpath.TicketPriorityInput {
	t.Helper()
	v, err := s.ReadTicketPriority(ctx, inv.Principal, ticket)
	if err != nil {
		t.Fatal(err)
	}
	return goldenpath.TicketPriorityInput{TicketID: ticket, ClientOperationID: operation, BasePriorityRevision: v.PriorityRevision, BaseStatusRevision: v.StatusRevision, Priority: priority, Confirmed: true}
}
func TestTicketPriorityHistoryPermissionsAndRetry(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, thread := componentTicket(t, ctx, core, "chn_restricted", "priority-main")
	store := goldenpostgres.New(pool)
	s := goldenpath.NewTicketPriorityService(store, goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	if _, err := core.GetNexusView(ctx, inv.Principal, entityref.Ref{Type: "thread", ID: thread}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("private source exposed", err)
	}
	v, err := s.ReadTicketPriority(ctx, inv.Principal, ticket.ID)
	if err != nil || v.Priority != "unset" || v.PriorityRevision != 1 || !v.CanSetPriority {
		t.Fatal(v, err)
	}
	first := priorityInput(t, ctx, s, inv, ticket.ID, "high", "first")
	if v, err := s.SetTicketPriority(ctx, inv, first); err != nil || v.PriorityRevision != 2 {
		t.Fatal(v, err)
	}
	for i, target := range []string{"normal", "unset", "high"} {
		if _, err = s.SetTicketPriority(ctx, inv, priorityInput(t, ctx, s, inv, ticket.ID, target, fmt.Sprintf("change-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	stale := first
	stale.ClientOperationID = "aba"
	if _, err = s.SetTicketPriority(ctx, inv, stale); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("priority ABA accepted", err)
	}
	if _, err = s.SetTicketPriority(ctx, inv, priorityInput(t, ctx, s, inv, ticket.ID, "high", "noop")); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("no-op accepted", err)
	}
	changed := first
	changed.Priority = "urgent"
	if _, err = s.SetTicketPriority(ctx, inv, changed); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("reused operation with new payload", err)
	}
	// Assignment is a separate dimension and does not invalidate priority confirmation.
	independent := priorityInput(t, ctx, s, inv, ticket.ID, "low", "independent")
	assignments := goldenpath.NewTicketAssignmentService(store, goldenpath.CryptoIDGenerator{})
	if _, err = assignments.SetTicketAssignee(ctx, inv, assignmentInput(t, ctx, assignments, inv, ticket.ID, "usr_contributor", "self")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetTicketPriority(ctx, inv, independent); err != nil {
		t.Fatal("assignment invalidated priority", err)
	}
	statuses := goldenpath.NewTicketStatusService(store, goldenpath.CryptoIDGenerator{})
	stale = priorityInput(t, ctx, s, inv, ticket.ID, "urgent", "status-aba")
	for i, state := range []string{"done", "open"} {
		if _, err = statuses.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{TicketID: ticket.ID, ClientOperationID: fmt.Sprintf("state-%d", i), BaseStatusRevision: i + 1, ToStatus: state, Confirmed: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.SetTicketPriority(ctx, inv, stale); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("status ABA accepted", err)
	}
	if _, err = statuses.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{TicketID: ticket.ID, ClientOperationID: "terminal", BaseStatusRevision: 3, ToStatus: "canceled", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	v, err = s.ReadTicketPriority(ctx, inv.Principal, ticket.ID)
	if err != nil || v.CanSetPriority || v.Priority != "low" || v.PriorityRevision != 6 {
		t.Fatal(v, err)
	}
	if _, err = s.SetTicketPriority(ctx, inv, priorityInput(t, ctx, s, inv, ticket.ID, "urgent", "terminal-priority")); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("terminal mutated", err)
	}
	if v, err := s.SetTicketPriority(ctx, inv, first); err != nil || v.Priority != "high" || v.PriorityRevision != 2 {
		t.Fatal("historical retry", v, err)
	}
	for _, who := range []string{"usr_reader", "usr_unknown"} {
		if _, err = s.SetTicketPriority(ctx, tcInvocation(who), first); err == nil {
			t.Fatal("non-writer changed priority", who)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.project_memberships SET role='viewer' WHERE user_id='usr_contributor' AND project_id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetTicketPriority(ctx, inv, first); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("receipt restored write authority", err)
	}
	// me is responsibility, including terminal records, rather than current eligibility.
	page, err := store.ListProjectTickets(ctx, inv.Principal, ticket.GoverningProjectID, goldenpath.DiscoveryPageInput{Limit: 25, TicketFilters: goldenpath.TicketFilters{Status: "canceled", Assignee: "me", Priority: "low"}})
	if err != nil || len(page.Items) != 1 || page.Items[0].Assignee.Eligible || page.Items[0].Priority != v.Priority {
		t.Fatal("me filter changed responsibility", page, err)
	}
	ref := entityref.Ref{Type: "ticket", ID: ticket.ID}
	view, err := core.GetNexusView(ctx, inv.Principal, ref)
	if err != nil {
		t.Fatal(err)
	}
	before := view.Timeline
	count := 0
	for _, item := range before {
		if item.ActivityType == "ticket.priority-changed" {
			count++
			if len(item.SafeFacts) != 4 || len(item.Subjects) != 0 {
				t.Fatal(item)
			}
		}
	}
	if count != 5 {
		t.Fatal(count)
	}
	if _, err = store.RebuildActivityProjection(ctx); err != nil {
		t.Fatal(err)
	}
	view, err = core.GetNexusView(ctx, inv.Principal, ref)
	if err != nil || !reflect.DeepEqual(before, view.Timeline) {
		t.Fatal("rebuild drift", err)
	}
	var receipts, events, outbox int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM radishnexus.collaboration_command_receipts WHERE command_kind='ticket.priority.set'),(SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.priority-changed'),(SELECT count(*) FROM radishnexus.outbox_deliveries o JOIN radishnexus.domain_events e USING(event_id) WHERE e.event_type='ticket.priority-changed')`).Scan(&receipts, &events, &outbox); err != nil || receipts != 5 || events != 5 || outbox != 5 {
		t.Fatal(receipts, events, outbox, err)
	}
}

func TestTicketPriorityConcurrentWrites(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "priority-race")
	s := goldenpath.NewTicketPriorityService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	one := priorityInput(t, ctx, s, inv, ticket.ID, "high", "one")
	two := one
	two.ClientOperationID = "two"
	two.Priority = "low"
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, in := range []goldenpath.TicketPriorityInput{one, two} {
		go func() { <-start; _, err := s.SetTicketPriority(ctx, inv, in); results <- err }()
	}
	close(start)
	passed, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			passed++
		} else if errors.Is(err, authz.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || conflict != 1 {
		t.Fatal(passed, conflict)
	}
}

func TestTicketPriorityPortableHistoryAndNativeContinuation(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	inv := contextInvocation(source.principal)
	statuses := goldenpath.NewTicketStatusService(source.store, goldenpath.CryptoIDGenerator{})
	if _, err := statuses.ChangeTicketStatus(source.ctx, inv, goldenpath.TicketStatusInput{TicketID: source.ticket, ClientOperationID: "priority-reopen", BaseStatusRevision: 5, ToStatus: "open", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	s := goldenpath.NewTicketPriorityService(source.store, goldenpath.CryptoIDGenerator{})
	for i, target := range []string{"urgent", "normal", "unset", "high"} {
		if _, err := s.SetTicketPriority(source.ctx, inv, priorityInput(t, source.ctx, s, inv, source.ticket, target, fmt.Sprintf("portable-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	assignments := goldenpath.NewTicketAssignmentService(source.store, goldenpath.CryptoIDGenerator{})
	if _, err := assignments.SetTicketAssignee(source.ctx, inv, assignmentInput(t, source.ctx, assignments, inv, source.ticket, source.member.ID, "portable-assignee")); err != nil {
		t.Fatal(err)
	}
	bundle := source.bundle(t)
	if bundle.Manifest.Profile != "project-collaboration-v4" {
		t.Fatal(bundle.Manifest.Profile)
	}
	target := contextDatabase(t)
	plan := target.plan(t, bundle)
	result, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, bundle, plan, plan.PlanDigest)
	if err != nil {
		t.Fatal(err)
	}
	ticket := contextMapping(t, plan, "ticket", source.ticket)
	s = goldenpath.NewTicketPriorityService(target.store, goldenpath.CryptoIDGenerator{})
	v, err := s.ReadTicketPriority(target.ctx, target.principal, ticket)
	if err != nil || v.Priority != "high" || v.PriorityRevision != 5 || !v.CanSetPriority {
		t.Fatal(v, err)
	}
	discovery := goldenpath.NewDiscoveryService(target.store)
	for _, who := range []string{"", "me", "unassigned"} {
		page, err := discovery.ListProjectTickets(target.ctx, target.principal, result.ProjectID, goldenpath.DiscoveryPageInput{Limit: 25, TicketFilters: goldenpath.TicketFilters{Assignee: who, Priority: "high"}})
		if err != nil {
			t.Fatal(err)
		}
		if who == "" {
			if len(page.Items) != 1 || page.Items[0].Assignee == nil || !page.Items[0].Assignee.Imported || page.Items[0].Assignee.Eligible {
				t.Fatal("historical responsibility lost", page)
			}
		} else if len(page.Items) != 0 {
			t.Fatal("historical actor matched live or unassigned filter", who, page)
		}
	}
	native := priorityInput(t, target.ctx, s, contextInvocation(target.principal), ticket, "low", "native")
	if _, err = s.SetTicketPriority(target.ctx, contextInvocation(target.principal), native); err != nil {
		t.Fatal(err)
	}
	second, err := target.service.Export(target.ctx, target.principal, target.owner.Token, result.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	third := contextDatabase(t)
	plan2 := third.plan(t, second)
	if _, err = third.service.Apply(third.ctx, third.principal, third.owner.Token, second, plan2, plan2.PlanDigest); err != nil {
		t.Fatal(err)
	}
	ticket2 := contextMapping(t, plan2, "ticket", ticket)
	s = goldenpath.NewTicketPriorityService(third.store, goldenpath.CryptoIDGenerator{})
	v, err = s.ReadTicketPriority(third.ctx, third.principal, ticket2)
	if err != nil || v.Priority != "low" || v.PriorityRevision != 6 {
		t.Fatal(v, err)
	}
	ref := entityref.Ref{Type: "ticket", ID: ticket2}
	view, err := third.core.GetNexusView(third.ctx, third.principal, ref)
	if err != nil {
		t.Fatal(err)
	}
	before := view.Timeline
	count := 0
	for _, item := range before {
		if item.ActivityType == "ticket.priority-changed" {
			count++
			if item.SafeFacts["imported"] != "true" {
				t.Fatal(item)
			}
		}
	}
	if count != 5 {
		t.Fatal(count)
	}
	if _, err = third.store.RebuildActivityProjection(third.ctx); err != nil {
		t.Fatal(err)
	}
	view, err = third.core.GetNexusView(third.ctx, third.principal, ref)
	if err != nil || !reflect.DeepEqual(before, view.Timeline) {
		t.Fatal("imported priority rebuild drift", err)
	}
}

func TestTicketPriorityFailureAtomicityAndCommitLoss(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "priority-failures")
	s := goldenpath.NewTicketPriorityService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	in := priorityInput(t, ctx, s, inv, ticket.ID, "high", "fault")
	query := `SELECT jsonb_build_array((SELECT jsonb_agg(t) FROM radishnexus.tickets t),(SELECT count(*) FROM radishnexus.collaboration_command_receipts),(SELECT count(*) FROM radishnexus.domain_events),(SELECT count(*) FROM radishnexus.outbox_deliveries),(SELECT count(*) FROM radishnexus.activity_items))::text`
	for _, table := range []string{"collaboration_command_receipts", "domain_events", "outbox_deliveries", "activity_items"} {
		var before, after string
		if err := pool.QueryRow(ctx, query).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `CREATE FUNCTION radishnexus.fail_priority() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected priority failure'; END $$; CREATE TRIGGER fail_priority BEFORE INSERT ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_priority()`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetTicketPriority(ctx, inv, in); err == nil {
			t.Fatal("fault succeeded", table)
		}
		if err := pool.QueryRow(ctx, query).Scan(&after); err != nil || after != before {
			t.Fatal("partial transaction", table, err)
		}
		if _, err := pool.Exec(ctx, `DROP TRIGGER fail_priority ON radishnexus.`+table+`;DROP FUNCTION radishnexus.fail_priority()`); err != nil {
			t.Fatal(err)
		}
	}
	config := pool.Config().Copy()
	proxied, finish := pgfault.LoseCommit(t, config.ConnConfig)
	config.ConnConfig = proxied
	config.MaxConns = 1
	faulty, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = goldenpath.NewTicketPriorityService(goldenpostgres.New(faulty), goldenpath.CryptoIDGenerator{}).SetTicketPriority(ctx, inv, in)
	faulty.Close()
	finish()
	if err == nil {
		t.Fatal("lost acknowledgement reported success")
	}
	result, err := s.SetTicketPriority(ctx, inv, in)
	if err != nil || result.PriorityRevision != 2 {
		t.Fatal("exact retry lost committed fact", result, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.priority-changed'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("retry duplicated event", count, err)
	}
}

func TestTicketPriorityLimitsAndReadBoundary(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "priority-limits")
	s := goldenpath.NewTicketPriorityService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	original := priorityInput(t, ctx, s, inv, ticket.ID, "high", "first")
	if _, err := s.SetTicketPriority(ctx, inv, original); err != nil {
		t.Fatal(err)
	}
	// Only this overflow probe alters stored revision; it is not export evidence.
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.tickets SET priority_revision=2147483647 WHERE id=$1`, ticket.ID); err != nil {
		t.Fatal(err)
	}
	v, err := s.ReadTicketPriority(ctx, inv.Principal, ticket.ID)
	if err != nil || v.CanSetPriority {
		t.Fatal(v, err)
	}
	if _, err = s.SetTicketPriority(ctx, inv, priorityInput(t, ctx, s, inv, ticket.ID, "normal", "overflow")); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("overflow accepted", err)
	}
	if r, err := s.SetTicketPriority(ctx, inv, original); err != nil || r.PriorityRevision != 2 {
		t.Fatal("overflow prevented exact retry", r, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.tickets SET priority_revision=2 WHERE id=$1`, ticket.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadTicketPriority(ctx, inv.Principal, "tkt_missing"); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal(err)
	}
	cross := inv.Principal
	cross.WorkspaceID = "wrk_other"
	if _, err = s.ReadTicketPriority(ctx, cross, ticket.ID); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("cross Workspace", err)
	}
	viewer := tcInvocation("usr_reader")
	v, err = s.ReadTicketPriority(ctx, viewer.Principal, ticket.ID)
	if err != nil || v.CanSetPriority {
		t.Fatal("viewer read/write boundary", v, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	v, err = s.ReadTicketPriority(ctx, inv.Principal, ticket.ID)
	if err != nil || v.CanSetPriority {
		t.Fatal("archive read", v, err)
	}
	if _, err = s.SetTicketPriority(ctx, inv, original); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("archive receipt write", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.projects SET status='active',visibility='restricted' WHERE id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM radishnexus.project_memberships WHERE project_id='prj_auth' AND user_id='usr_admin'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadTicketPriority(ctx, tcInvocation("usr_admin").Principal, ticket.ID); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("owner bypassed restricted Project", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id='wrk_main' AND user_id='usr_contributor'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadTicketPriority(ctx, inv.Principal, ticket.ID); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("suspended read", err)
	}
}
