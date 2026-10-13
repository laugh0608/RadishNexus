//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"reflect"
	"testing"
	"time"
)

func TestTicketStatusHistoryRetryAndReadEntrypoints(t *testing.T) {
	ctx, pool, core, relations, _, component := ticketComponentFixture(t)
	ticket, thread := componentTicket(t, ctx, core, "chn_restricted", "status-main")
	store := goldenpostgres.New(pool)
	s := goldenpath.NewTicketStatusService(store, goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	ref := entityref.Ref{Type: "ticket", ID: ticket.ID}
	if _, e := core.GetNexusView(ctx, inv.Principal, entityref.Ref{Type: "thread", ID: thread}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("private source readable", e)
	}
	cid := component("status-component")
	if _, e := relations.WriteTicketComponent(ctx, inv, "ticket.component.link", goldenpath.TicketComponentInput{TicketID: ticket.ID, ComponentID: cid, ClientOperationID: "link", Confirmed: true}); e != nil {
		t.Fatal(e)
	}
	view, e := core.GetNexusView(ctx, inv.Principal, ref)
	if e != nil || view.Current.StatusRevision != 1 || view.TicketCapabilities == nil || !view.TicketCapabilities.ChangeStatus {
		t.Fatal(view, e)
	}
	var decision string
	if e = pool.QueryRow(ctx, `SELECT target_id FROM radishnexus.collaboration_command_receipts WHERE command_kind='ticket.create' AND result_id=$1`, ticket.ID).Scan(&decision); e != nil {
		t.Fatal(e)
	}
	first := goldenpath.TicketStatusInput{TicketID: ticket.ID, ClientOperationID: "state-0", BaseStatusRevision: 1, ToStatus: "in-progress", Confirmed: true}
	before := ticket.UpdatedAt
	for i, status := range []string{"in-progress", "done", "open", "canceled", "open"} {
		input := first
		input.ClientOperationID = fmt.Sprintf("state-%d", i)
		input.BaseStatusRevision = i + 1
		input.ToStatus = status
		result, e := s.ChangeTicketStatus(ctx, inv, input)
		if e != nil || result.AppliedStatus != status || result.AppliedStatusRevision != i+2 {
			t.Fatal(result, e)
		}
		view, e = core.GetNexusView(ctx, inv.Principal, ref)
		if e != nil || view.Current.Status != status || view.Current.StatusRevision != i+2 || !view.Current.UpdatedAt.After(before) {
			t.Fatal(view, e)
		}
		before = view.Current.UpdatedAt
		page, e := goldenpath.NewDiscoveryService(store).ListProjectTickets(ctx, inv.Principal, ticket.GoverningProjectID, goldenpath.DiscoveryPageInput{Limit: 25})
		if e != nil || len(page.Items) != 1 || page.Items[0].Status != status {
			t.Fatal(page, e)
		}
		reverse, e := relations.ListTicketComponents(ctx, inv.Principal, entityref.Ref{Type: "component", ID: cid}, goldenpath.DiscoveryPageInput{Limit: 25})
		if e != nil || len(reverse.Links) != 1 || reverse.Links[0].Ticket.Status != status {
			t.Fatal(reverse, e)
		}
		retry, e := core.CreateTicketFromDecision(ctx, tcInvocation("usr_decider"), goldenpath.CreateTicketInput{DecisionID: decision, ClientOperationID: "status-main", Title: "Work status-main"})
		if e != nil || retry.Created || retry.Ticket.ID != ticket.ID || retry.Ticket.Status != status || retry.Ticket.StatusRevision != i+2 {
			t.Fatal(retry, e)
		}
	}
	result, e := s.ChangeTicketStatus(ctx, inv, first)
	if e != nil || result.AppliedStatus != "in-progress" || result.AppliedStatusRevision != 2 {
		t.Fatal("old receipt", result, e)
	}
	for _, mutate := range []func(*goldenpath.TicketStatusInput){func(i *goldenpath.TicketStatusInput) { i.ClientOperationID = "aba" }, func(i *goldenpath.TicketStatusInput) { i.ToStatus = "done" }, func(i *goldenpath.TicketStatusInput) { i.BaseStatusRevision = 6 }} {
		bad := first
		mutate(&bad)
		if _, e = s.ChangeTicketStatus(ctx, inv, bad); !errors.Is(e, authz.ErrConflict) {
			t.Fatal("stale/different request accepted", e)
		}
	}
	view, e = core.GetNexusView(ctx, inv.Principal, ref)
	if e != nil {
		t.Fatal(e)
	}
	original := view.Timeline
	count := 0
	for _, a := range original {
		if a.ActivityType == "ticket.status-changed" {
			count++
			if len(a.Subjects) != 0 || len(a.SafeFacts) != 4 {
				t.Fatal(a)
			}
		}
		if a.ActivityType == "ticket.created" && a.SafeFacts["status"] != "open" {
			t.Fatal(a)
		}
	}
	if count != 5 {
		t.Fatal(count)
	}
	if _, e = store.RebuildActivityProjection(ctx); e != nil {
		t.Fatal(e)
	}
	view, e = core.GetNexusView(ctx, inv.Principal, ref)
	if e != nil || !reflect.DeepEqual(original, view.Timeline) {
		t.Fatal("rebuild drift", e)
	}
	for _, tc := range []struct {
		set, restore string
		want         error
	}{
		{`UPDATE radishnexus.project_memberships SET role='viewer' WHERE user_id='usr_contributor'`, `UPDATE radishnexus.project_memberships SET role='contributor' WHERE user_id='usr_contributor'`, authz.ErrForbidden},
		{`UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`, `UPDATE radishnexus.projects SET status='active' WHERE id='prj_auth'`, authz.ErrConflict},
		{`DELETE FROM radishnexus.project_memberships WHERE user_id='usr_contributor' AND project_id='prj_auth'`, `INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) VALUES('wrk_main','prj_auth','usr_contributor','contributor')`, authz.ErrForbidden},
		{`UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE user_id='usr_contributor'`, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE user_id='usr_contributor'`, authz.ErrNotFound},
	} {
		if _, e = pool.Exec(ctx, tc.set); e != nil {
			t.Fatal(e)
		}
		if _, e = s.ChangeTicketStatus(ctx, inv, first); !errors.Is(e, tc.want) {
			t.Fatal("retry authorization", tc.want, e)
		}
		if _, e = pool.Exec(ctx, tc.restore); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.ChangeTicketStatus(ctx, inv, first); e != nil {
		t.Fatal("restored rights retry", e)
	}
}

func TestTicketStatusConcurrentAndAtomic(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "concurrent")
	s := goldenpath.NewTicketStatusService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	inv := tcInvocation("usr_contributor")
	first := goldenpath.TicketStatusInput{TicketID: ticket.ID, ClientOperationID: "one", BaseStatusRevision: 1, ToStatus: "done", Confirmed: true}
	start := make(chan struct{})
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			<-start
			in := first
			in.ClientOperationID = fmt.Sprintf("op-%d", i)
			_, e := s.ChangeTicketStatus(ctx, inv, in)
			results <- e
		}(i)
	}
	close(start)
	success := 0
	for i := 0; i < 8; i++ {
		e := <-results
		if e == nil {
			success++
		} else if !errors.Is(e, authz.ErrConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	first.ClientOperationID = "same"
	first.BaseStatusRevision = 2
	first.ToStatus = "open"
	for i := 0; i < 8; i++ {
		go func() { _, e := s.ChangeTicketStatus(ctx, inv, first); results <- e }()
	}
	for i := 0; i < 8; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	first.ClientOperationID = "rollback"
	first.BaseStatusRevision = 3
	first.ToStatus = "canceled"
	for _, table := range []string{"collaboration_command_receipts", "domain_events", "outbox_deliveries", "activity_items"} {
		var before string
		query := `SELECT jsonb_build_array((SELECT jsonb_agg(t) FROM radishnexus.tickets t),(SELECT count(*) FROM radishnexus.collaboration_command_receipts),(SELECT count(*) FROM radishnexus.domain_events),(SELECT count(*) FROM radishnexus.outbox_deliveries),(SELECT count(*) FROM radishnexus.activity_items))::text`
		if e := pool.QueryRow(ctx, query).Scan(&before); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `CREATE FUNCTION radishnexus.fail_status() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected status failure'; END $$; CREATE TRIGGER fail_status BEFORE INSERT ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_status()`); e != nil {
			t.Fatal(e)
		}
		if _, e := s.ChangeTicketStatus(ctx, inv, first); e == nil {
			t.Fatal("injected failure succeeded", table)
		}
		var after string
		if e := pool.QueryRow(ctx, query).Scan(&after); e != nil || before != after {
			t.Fatal("partial commit", table, e)
		}
		if _, e := pool.Exec(ctx, `DROP TRIGGER fail_status ON radishnexus.`+table+`; DROP FUNCTION radishnexus.fail_status()`); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.ChangeTicketStatus(ctx, inv, first); e != nil {
		t.Fatal("rollback consumed operation", e)
	}
	// Exercise the finite integer boundary; this fixture change is not a product transition.
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.tickets SET status_revision=2147483647 WHERE id=$1`, ticket.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ChangeTicketStatus(ctx, inv, first); e != nil {
		t.Fatal("max revision blocks old receipt", e)
	}
	first.ClientOperationID = "overflow"
	first.BaseStatusRevision = goldenpath.MaxTicketStatusRevision
	first.ToStatus = "open"
	if _, e := s.ChangeTicketStatus(ctx, inv, first); !errors.Is(e, authz.ErrConflict) {
		t.Fatal(e)
	}
	view, e := core.GetNexusView(ctx, inv.Principal, entityref.Ref{Type: "ticket", ID: ticket.ID})
	if e != nil || view.TicketCapabilities.ChangeStatus {
		t.Fatal("overflow capability", e)
	}
}

func TestTicketStatusPermissionSerialization(t *testing.T) {
	for _, change := range []struct {
		name, sql, pattern string
		want               error
	}{
		{"archive", `UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`, "%FROM radishnexus.projects%", authz.ErrConflict},
		{"role", `UPDATE radishnexus.project_memberships SET role='viewer' WHERE user_id='usr_contributor' AND project_id='prj_auth'`, "%FROM radishnexus.project_memberships%", authz.ErrForbidden},
		{"membership", `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE user_id='usr_contributor' AND workspace_id='wrk_main'`, "%FROM radishnexus.workspace_memberships%", authz.ErrNotFound},
		{"account", `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id='usr_contributor'`, "%FROM radishnexus.user_accounts%", authz.ErrNotFound},
	} {
		for _, writeFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/write-first-%v", change.name, writeFirst), func(t *testing.T) {
				ctx, pool, core, _, _, _ := ticketComponentFixture(t)
				s := goldenpath.NewTicketStatusService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
				ticket, _ := componentTicket(t, ctx, core, "chn_project", "permission")
				in := goldenpath.TicketStatusInput{TicketID: ticket.ID, BaseStatusRevision: 1, ToStatus: "done", ClientOperationID: "permission", Confirmed: true}
				call := func() error {
					_, e := s.ChangeTicketStatus(ctx, tcInvocation("usr_contributor"), in)
					return e
				}
				wait := func(pattern string) {
					t.Helper()
					timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					for {
						var waiting bool
						if e := pool.QueryRow(timeout, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1)`, pattern).Scan(&waiting); e != nil {
							t.Fatal(e)
						}
						if waiting {
							return
						}
						select {
						case <-timeout.Done():
							t.Fatal("permission lock barrier", timeout.Err())
						case <-time.After(5 * time.Millisecond):
						}
					}
				}
				if !writeFirst {
					tx, e := pool.Begin(ctx)
					if e != nil {
						t.Fatal(e)
					}
					defer tx.Rollback(ctx)
					if _, e = tx.Exec(ctx, change.sql); e != nil {
						t.Fatal(e)
					}
					done := make(chan error, 1)
					go func() { done <- call() }()
					wait(change.pattern)
					if e = tx.Commit(ctx); e != nil {
						t.Fatal(e)
					}
					if e = <-done; !errors.Is(e, change.want) {
						t.Fatal("write crossed permission change", e)
					}
				} else {
					if _, e := pool.Exec(ctx, `CREATE FUNCTION radishnexus.status_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(39,1); RETURN NEW; END $$; CREATE TRIGGER status_barrier BEFORE UPDATE ON radishnexus.tickets FOR EACH ROW EXECUTE FUNCTION radishnexus.status_barrier()`); e != nil {
						t.Fatal(e)
					}
					gate, e := pool.Begin(ctx)
					if e != nil {
						t.Fatal(e)
					}
					defer gate.Rollback(ctx)
					if _, e = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(39,1)`); e != nil {
						t.Fatal(e)
					}
					done, changed := make(chan error, 1), make(chan error, 1)
					go func() { done <- call() }()
					wait("%UPDATE radishnexus.tickets%")
					go func() { _, e := pool.Exec(ctx, change.sql); changed <- e }()
					wait(change.sql)
					if e = gate.Commit(ctx); e != nil {
						t.Fatal(e)
					}
					if e = <-done; e != nil {
						t.Fatal("authorized write failed", e)
					}
					if e = <-changed; e != nil {
						t.Fatal("permission change failed", e)
					}
				}
				if e := call(); !errors.Is(e, change.want) {
					t.Fatal("retry bypassed current permission", e)
				}
				var count int
				if e := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.domain_events WHERE event_type='ticket.status-changed' AND primary_entity_id=$1`, ticket.ID).Scan(&count); e != nil {
					t.Fatal(e)
				}
				want := 0
				if writeFirst {
					want = 1
				}
				if count != want {
					t.Fatal("wrong serial outcome", count, want)
				}
			})
		}
	}
}

func TestTicketStatusPermissionsAndInvalidEdges(t *testing.T) {
	ctx, pool, core, _, _, _ := ticketComponentFixture(t)
	ticket, _ := componentTicket(t, ctx, core, "chn_restricted", "permissions")
	service := goldenpath.NewTicketStatusService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	input := goldenpath.TicketStatusInput{TicketID: ticket.ID, ClientOperationID: "permission", BaseStatusRevision: 1, ToStatus: "done", Confirmed: true}
	exec := func(sql string) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	for _, user := range []string{"usr_reader", "usr_admin", "usr_decider"} {
		if user != "usr_reader" {
			exec(`DELETE FROM radishnexus.project_memberships WHERE user_id='` + user + `' AND project_id='prj_auth'`)
		}
		// Public Project baseline remains readable; neither owner nor creator gains writes.
		if _, e := service.ChangeTicketStatus(ctx, tcInvocation(user), input); !errors.Is(e, authz.ErrForbidden) {
			t.Fatal(user, e)
		}
		view, e := core.GetNexusView(ctx, principal(user), entityref.Ref{Type: "ticket", ID: ticket.ID})
		if e != nil || view.TicketCapabilities.ChangeStatus {
			t.Fatal("readonly capability", user, e)
		}
	}
	exec(`UPDATE radishnexus.projects SET visibility='restricted' WHERE id='prj_auth'`)
	if _, e := service.ChangeTicketStatus(ctx, tcInvocation("usr_admin"), input); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("owner bypass", e)
	}
	if _, e := service.ChangeTicketStatus(ctx, tcInvocation("usr_decider"), input); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("creator bypass", e)
	}
	if _, e := service.ChangeTicketStatus(ctx, tcInvocation("usr_contributor"), input); e != nil {
		t.Fatal(e)
	}
	for _, to := range []string{"done", "canceled", "in-progress"} {
		bad := input
		bad.ClientOperationID = "invalid-" + to
		bad.BaseStatusRevision = 2
		bad.ToStatus = to
		if _, e := service.ChangeTicketStatus(ctx, tcInvocation("usr_contributor"), bad); !errors.Is(e, authz.ErrConflict) {
			t.Fatal("terminal edge", to, e)
		}
	}
	// Receipt/event mismatch must fail at commit even if individual FKs are valid.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `INSERT INTO radishnexus.collaboration_command_receipts(workspace_id,actor_id,command_kind,target_type,target_id,client_operation_id,payload_sha256,result_type,result_id,event_id,created_at,result_revision)
 SELECT workspace_id,actor_id,command_kind,target_type,target_id,'corrupt-revision',payload_sha256,result_type,result_id,event_id,created_at,result_revision+1 FROM radishnexus.collaboration_command_receipts WHERE command_kind='ticket.status.change' AND target_id=$1`, ticket.ID)
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e == nil {
		t.Fatal("corrupt receipt committed")
	}
	view, e := core.GetNexusView(ctx, principal("usr_contributor"), entityref.Ref{Type: "ticket", ID: ticket.ID})
	if e != nil || view.Current.StatusRevision != 2 || view.Current.Status != "done" {
		t.Fatal("failure changed current", e)
	}
}
