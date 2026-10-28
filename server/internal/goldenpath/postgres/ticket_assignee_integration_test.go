//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
)

func assignmentInput(t *testing.T, ctx context.Context, s *goldenpath.TicketAssignmentService, inv goldenpath.Invocation, ticket, target, operation string) goldenpath.TicketAssignmentInput {
	t.Helper()
	v, err := s.ReadTicketAssignment(ctx, inv.Principal, ticket)
	if err != nil {
		t.Fatal(err)
	}
	in := goldenpath.TicketAssignmentInput{TicketID: ticket, ClientOperationID: operation, BaseAssignmentRevision: v.AssignmentRevision, BaseStatusRevision: v.StatusRevision, ProjectManagementRevision: v.ProjectManagementRevision, TargetLifecycleRevision: "0", Confirmed: true}
	if target != "" {
		q, err := s.ListAssigneeCandidates(ctx, inv.Principal, ticket, goldenpath.DiscoveryPageInput{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range q.Items {
			if c.ID == target {
				in.AssigneeUserID = &target
				in.TargetLifecycleRevision = c.LifecycleRevision
				return in
			}
		}
		t.Fatal("missing candidate", target)
	}
	return in
}
func TestTicketAssignmentHistoryEligibilityAndRetry(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, thread := componentTicket(t, ctx, core, "chn_restricted", "assignee-main")
	store := goldenpostgres.New(pool)
	s := goldenpath.NewTicketAssignmentService(store, goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	if _, err := core.GetNexusView(ctx, inv.Principal, entityref.Ref{Type: "thread", ID: thread}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("private source exposed", err)
	}
	initial, err := s.ReadTicketAssignment(ctx, inv.Principal, ticket.ID)
	if err != nil || initial.Assignee != nil || initial.AssignmentRevision != 1 || !initial.CanAssign {
		t.Fatal(initial, err)
	}
	page, err := s.ListAssigneeCandidates(ctx, inv.Principal, ticket.ID, goldenpath.DiscoveryPageInput{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextID == "" {
		t.Fatal(page, err)
	}
	next, err := s.ListAssigneeCandidates(ctx, inv.Principal, ticket.ID, goldenpath.DiscoveryPageInput{Limit: 50, AfterID: page.NextID})
	if err != nil || len(next.Items) == 0 || next.Items[0].ID <= page.NextID {
		t.Fatal(next, err)
	}
	for _, c := range append(page.Items, next.Items...) {
		if c.ID == "usr_viewer" || c.ID == "usr_owner" {
			t.Fatal("ineligible candidate", c)
		}
	}
	first := assignmentInput(t, ctx, s, inv, ticket.ID, "usr_contributor", "first")
	if v, err := s.SetTicketAssignee(ctx, inv, first); err != nil || v.AssignmentRevision != 2 {
		t.Fatal(v, err)
	}
	for i, target := range []string{"usr_decider", "", "usr_contributor"} {
		in := assignmentInput(t, ctx, s, inv, ticket.ID, target, []string{"reassign", "clear", "self"}[i])
		if _, err := s.SetTicketAssignee(ctx, inv, in); err != nil {
			t.Fatal(err)
		}
	}
	stale := first
	stale.ClientOperationID = "aba"
	if _, err = s.SetTicketAssignee(ctx, inv, stale); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("ABA accepted", err)
	}
	same := assignmentInput(t, ctx, s, inv, ticket.ID, "usr_contributor", "noop")
	if _, err = s.SetTicketAssignee(ctx, inv, same); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("no-op accepted", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.project_memberships SET role='viewer' WHERE user_id='usr_contributor' AND project_id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	current, err := s.ReadTicketAssignment(ctx, tcInvocation("usr_decider").Principal, ticket.ID)
	if err != nil || current.Assignee == nil || current.Assignee.Eligible || current.Assignee.ID != "usr_contributor" {
		t.Fatal(current, err)
	}
	if _, err = s.ListAssigneeCandidates(ctx, inv.Principal, ticket.ID, goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("viewer candidates", err)
	}
	if _, err = s.SetTicketAssignee(ctx, inv, first); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("receipt restored authority", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.project_memberships SET role='contributor' WHERE user_id='usr_contributor' AND project_id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	status := goldenpath.NewTicketStatusService(store, goldenpath.CryptoIDGenerator{})
	if _, err = status.ChangeTicketStatus(ctx, inv, goldenpath.TicketStatusInput{TicketID: ticket.ID, ClientOperationID: "done", BaseStatusRevision: 1, ToStatus: "done", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if v, err := s.SetTicketAssignee(ctx, inv, first); err != nil || v.AssignmentRevision != 2 {
		t.Fatal("exact historical retry", v, err)
	}
	current, err = s.ReadTicketAssignment(ctx, inv.Principal, ticket.ID)
	if err != nil || current.CanAssign || current.AssignmentRevision != 5 || current.StatusRevision != 2 {
		t.Fatal(current, err)
	}
	discovery, err := store.ListProjectTickets(ctx, inv.Principal, ticket.GoverningProjectID, goldenpath.DiscoveryPageInput{Limit: 25})
	if err != nil || len(discovery.Items) != 1 || !reflect.DeepEqual(discovery.Items[0].Assignee, current.Assignee) {
		t.Fatal("list/detail drift", discovery, err)
	}
	ref := entityref.Ref{Type: "ticket", ID: ticket.ID}
	view, err := core.GetNexusView(ctx, inv.Principal, ref)
	if err != nil {
		t.Fatal(err)
	}
	before := view.Timeline
	count := 0
	for _, item := range before {
		if item.ActivityType == "ticket.assignee-changed" {
			count++
			if len(item.SafeFacts) != 4 || len(item.Subjects) != 0 {
				t.Fatal(item)
			}
		}
	}
	if count != 4 {
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
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM radishnexus.collaboration_command_receipts WHERE command_kind='ticket.assignee.set'),(SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.assignee-changed'),(SELECT count(*) FROM radishnexus.outbox_deliveries o JOIN radishnexus.domain_events e USING(event_id) WHERE e.event_type='ticket.assignee-changed' AND o.state='delivered')`).Scan(&receipts, &events, &outbox); err != nil || receipts != 4 || events != 4 || outbox != 4 {
		t.Fatal(receipts, events, outbox, err)
	}
}
func TestTicketAssignmentStaleMembershipAndConcurrentWrites(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "assignee-race")
	store := goldenpostgres.New(pool)
	s := goldenpath.NewTicketAssignmentService(store, goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	old := assignmentInput(t, ctx, s, inv, ticket.ID, "usr_decider", "old")
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id='wrk_main' AND user_id='usr_decider'; UPDATE radishnexus.workspace_memberships SET status='active' WHERE workspace_id='wrk_main' AND user_id='usr_decider'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTicketAssignee(ctx, inv, old); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("old lifecycle confirmed", err)
	}
	old = assignmentInput(t, ctx, s, inv, ticket.ID, "usr_decider", "role-aba")
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.project_memberships SET role='viewer' WHERE project_id='prj_auth' AND user_id='usr_decider'; UPDATE radishnexus.project_memberships SET role='decider' WHERE project_id='prj_auth' AND user_id='usr_decider'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTicketAssignee(ctx, inv, old); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("old role confirmed", err)
	}
	one := assignmentInput(t, ctx, s, inv, ticket.ID, "usr_decider", "one")
	two := assignmentInput(t, ctx, s, inv, ticket.ID, "usr_contributor", "two")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, in := range []goldenpath.TicketAssignmentInput{one, two} {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := s.SetTicketAssignee(ctx, inv, in); results <- err }()
	}
	close(start)
	wg.Wait()
	close(results)
	passed, conflict := 0, 0
	for err := range results {
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

func TestTicketAssignmentPortableHistoryAndNativeContinuation(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	inv := contextInvocation(source.principal)
	statuses := goldenpath.NewTicketStatusService(source.store, goldenpath.CryptoIDGenerator{})
	if _, err := statuses.ChangeTicketStatus(source.ctx, inv, goldenpath.TicketStatusInput{TicketID: source.ticket, ClientOperationID: "reopen-assign", BaseStatusRevision: 5, ToStatus: "open", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	service := goldenpath.NewTicketAssignmentService(source.store, goldenpath.CryptoIDGenerator{})
	for i, target := range []string{source.member.ID, source.principal.ID, "", source.member.ID} {
		in := assignmentInput(t, source.ctx, service, inv, source.ticket, target, []string{"member", "owner", "clear", "member-again"}[i])
		if _, err := service.SetTicketAssignee(source.ctx, inv, in); err != nil {
			t.Fatal(err)
		}
	}
	bundle := source.bundle(t)
	target := contextDatabase(t)
	plan := target.plan(t, bundle)
	result, err := target.service.Apply(target.ctx, target.principal, target.owner.Token, bundle, plan, plan.PlanDigest)
	if err != nil {
		t.Fatal(err)
	}
	ticket := contextMapping(t, plan, "ticket", source.ticket)
	historical := contextMapping(t, plan, "actor", source.member.ID)
	service = goldenpath.NewTicketAssignmentService(target.store, goldenpath.CryptoIDGenerator{})
	v, err := service.ReadTicketAssignment(target.ctx, target.principal, ticket)
	if err != nil || v.Assignee == nil || v.Assignee.ID != historical || !v.Assignee.Imported || v.Assignee.Eligible || v.AssignmentRevision != 5 {
		t.Fatal(v, err)
	}
	q, err := service.ListAssigneeCandidates(target.ctx, target.principal, ticket, goldenpath.DiscoveryPageInput{Limit: 50})
	if err != nil || len(q.Items) != 1 || q.Items[0].ID != target.principal.ID {
		t.Fatal("historical identity candidate", q, err)
	}
	in := assignmentInput(t, target.ctx, service, contextInvocation(target.principal), ticket, target.principal.ID, "native")
	if _, err = service.SetTicketAssignee(target.ctx, contextInvocation(target.principal), in); err != nil {
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
	service = goldenpath.NewTicketAssignmentService(third.store, goldenpath.CryptoIDGenerator{})
	v, err = service.ReadTicketAssignment(third.ctx, third.principal, ticket2)
	if err != nil || v.Assignee == nil || v.Assignee.ID != contextMapping(t, plan2, "actor", target.principal.ID) || v.AssignmentRevision != 6 || !v.Assignee.Imported {
		t.Fatal(v, err)
	}
	view, err := third.core.GetNexusView(third.ctx, third.principal, entityref.Ref{Type: "ticket", ID: ticket2})
	if err != nil {
		t.Fatal(err)
	}
	before := view.Timeline
	if _, err = third.store.RebuildActivityProjection(third.ctx); err != nil {
		t.Fatal(err)
	}
	view, err = third.core.GetNexusView(third.ctx, third.principal, entityref.Ref{Type: "ticket", ID: ticket2})
	if err != nil || !reflect.DeepEqual(before, view.Timeline) {
		t.Fatal("imported assignment rebuild drift", err)
	}
}

func TestTicketAssignmentFailureAtomicityAndCommitLoss(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "assignment-failures")
	s := goldenpath.NewTicketAssignmentService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	in := assignmentInput(t, ctx, s, inv, ticket.ID, "usr_decider", "fault")
	query := `SELECT jsonb_build_array((SELECT jsonb_agg(t) FROM radishnexus.tickets t),(SELECT count(*) FROM radishnexus.collaboration_command_receipts),(SELECT count(*) FROM radishnexus.domain_events),(SELECT count(*) FROM radishnexus.outbox_deliveries),(SELECT count(*) FROM radishnexus.activity_items))::text`
	for _, table := range []string{"collaboration_command_receipts", "domain_events", "outbox_deliveries", "activity_items"} {
		var before, after string
		if err := pool.QueryRow(ctx, query).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `CREATE FUNCTION radishnexus.fail_assignment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected assignment failure'; END $$; CREATE TRIGGER fail_assignment BEFORE INSERT ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_assignment()`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetTicketAssignee(ctx, inv, in); err == nil {
			t.Fatal("fault succeeded", table)
		}
		if err := pool.QueryRow(ctx, query).Scan(&after); err != nil || after != before {
			t.Fatal("partial transaction", table, err)
		}
		if _, err := pool.Exec(ctx, `DROP TRIGGER fail_assignment ON radishnexus.`+table+`;DROP FUNCTION radishnexus.fail_assignment()`); err != nil {
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
	_, err = goldenpath.NewTicketAssignmentService(goldenpostgres.New(faulty), goldenpath.CryptoIDGenerator{}).SetTicketAssignee(ctx, inv, in)
	faulty.Close()
	finish()
	if err == nil {
		t.Fatal("lost acknowledgement reported success")
	}
	result, err := s.SetTicketAssignee(ctx, inv, in)
	if err != nil || result.AssignmentRevision != 2 {
		t.Fatal("exact retry lost committed fact", result, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.assignee-changed'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("retry duplicated event", count, err)
	}
}

func TestTicketAssignmentFormalMembershipLifecycle(t *testing.T) {
	f := contextDatabase(t)
	f.seed(t)
	m := &membershipFixture{contextFixture: f, management: goldenpath.NewManagementService(f.store, goldenpath.CryptoIDGenerator{})}
	inv := contextInvocation(f.principal)
	status := goldenpath.NewTicketStatusService(f.store, goldenpath.CryptoIDGenerator{})
	if _, err := status.ChangeTicketStatus(f.ctx, inv, goldenpath.TicketStatusInput{TicketID: f.ticket, ClientOperationID: "lifecycle-reopen", BaseStatusRevision: 5, ToStatus: "open", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	s := goldenpath.NewTicketAssignmentService(f.store, goldenpath.CryptoIDGenerator{})
	first := assignmentInput(t, f.ctx, s, inv, f.ticket, f.member.ID, "lifecycle-assign")
	if _, err := s.SetTicketAssignee(f.ctx, inv, first); err != nil {
		t.Fatal(err)
	}
	check := func(eligible bool) {
		t.Helper()
		v, err := s.ReadTicketAssignment(f.ctx, f.principal, f.ticket)
		if err != nil || v.Assignee == nil || v.Assignee.ID != f.member.ID || v.Assignee.Eligible != eligible || v.AssignmentRevision != 2 {
			t.Fatal("responsibility changed with eligibility", v, err)
		}
		page, err := s.ListAssigneeCandidates(f.ctx, f.principal, f.ticket, goldenpath.DiscoveryPageInput{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range page.Items {
			found = found || c.ID == f.member.ID
		}
		if found != eligible {
			t.Fatal("candidate/read drift")
		}
	}
	check(true)
	m.mustChange(t, f.owner, proposal("workspace", f.workspace, "suspend", f.member.ID, ""))
	check(false)
	if v, err := s.SetTicketAssignee(f.ctx, inv, first); err != nil || v.AssignmentRevision != 2 {
		t.Fatal("retry depended on target eligibility", v, err)
	}
	m.mustChange(t, f.owner, proposal("workspace", f.workspace, "resume", f.member.ID, ""))
	check(true)
	m.mustChange(t, f.owner, proposal("workspace", f.workspace, "remove", f.member.ID, ""))
	check(false)
	session, err := f.auth.Login(f.ctx, authn.LoginInput{Email: "context.member@example.test", Password: contextTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	invitation := m.mustChange(t, f.owner, proposal("workspace", f.workspace, "reinvite", f.member.ID, ""))
	if invitation.Invitation == nil {
		t.Fatal("missing reinvitation")
	}
	if _, err = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invitation.Invitation.Token, SessionToken: session.Token}); err != nil {
		t.Fatal(err)
	}
	check(false)
	m.mustChange(t, f.owner, proposal("project", f.project, "add", f.member.ID, "contributor"))
	check(true)
	m.mustChange(t, f.owner, proposal("project", f.project, "role", f.member.ID, "viewer"))
	check(false)
	m.mustChange(t, f.owner, proposal("project", f.project, "role", f.member.ID, "contributor"))
	check(true)
	contextSQL(t, f, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, f.member.ID)
	check(false)
	var count int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.assignee-changed'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("lifecycle rewrote assignment history", count, err)
	}
}
