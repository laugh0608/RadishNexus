//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func assertActivityFailureRollback(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *goldenpostgres.Store, service *goldenpath.Service, acceptedID string) {
	t.Helper()
	proposed, err := service.CreateDecisionFromThread(ctx, invocation(principal("usr_contributor"), "cor_projection_setup"), goldenpath.CreateDecisionInput{
		ThreadID: "thr_private", ClientOperationID: "projection:setup", Question: "Projection rollback boundary",
	})
	if err != nil {
		t.Fatal(err)
	}
	before := loadNexusView(t, ctx, service, principal("usr_decider"), entityref.Ref{Type: "decision", ID: proposed.Decision.ID})
	var countsBefore, countsAfter []int64
	const counts = `SELECT ARRAY[(SELECT count(*) FROM radishnexus.decisions), (SELECT count(*) FROM radishnexus.tickets),
		(SELECT count(*) FROM radishnexus.entity_links), (SELECT count(*) FROM radishnexus.domain_events),
		(SELECT count(*) FROM radishnexus.outbox_deliveries), (SELECT count(*) FROM radishnexus.collaboration_command_receipts),
		(SELECT count(*) FROM radishnexus.activity_items)]`
	if err := pool.QueryRow(ctx, counts).Scan(&countsBefore); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		CREATE FUNCTION radishnexus.test_reject_activity() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'test projection failure'; END; $$;
		CREATE TRIGGER test_reject_activity BEFORE INSERT ON radishnexus.activity_items
		FOR EACH ROW EXECUTE FUNCTION radishnexus.test_reject_activity();`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `DROP TRIGGER test_reject_activity ON radishnexus.activity_items; DROP FUNCTION radishnexus.test_reject_activity()`); err != nil {
			t.Error(err)
		}
	}()
	_, err = service.CreateDecisionFromThread(ctx, invocation(principal("usr_contributor"), "cor_projection_propose"), goldenpath.CreateDecisionInput{
		ThreadID: "thr_private", ClientOperationID: "projection:propose", Question: "Must roll back",
	})
	if err == nil {
		t.Fatal("proposal reported success after projection failure")
	}
	_, err = service.AcceptDecision(ctx, invocation(principal("usr_decider"), "cor_projection_accept"), goldenpath.AcceptDecisionInput{
		DecisionID: proposed.Decision.ID, ClientOperationID: "projection:accept", Outcome: "Must roll back", Rationale: "Projection unavailable",
	})
	if err == nil {
		t.Fatal("acceptance reported success after projection failure")
	}
	_, err = service.CreateTicketFromDecision(ctx, invocation(principal("usr_contributor"), "cor_projection_ticket"), goldenpath.CreateTicketInput{
		DecisionID: acceptedID, ClientOperationID: "projection:ticket", Title: "Must roll back",
	})
	if err == nil {
		t.Fatal("Ticket reported success after projection failure")
	}
	if _, err := store.RebuildActivityProjection(ctx); err == nil {
		t.Fatal("rebuild reported success after projection failure")
	}
	if err := pool.QueryRow(ctx, counts).Scan(&countsAfter); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(countsBefore, countsAfter) {
		t.Fatalf("partial writes after projection failures: %v -> %v", countsBefore, countsAfter)
	}
	after := loadNexusView(t, ctx, service, principal("usr_decider"), before.Current.Ref)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed projection changed Current/Timeline: %#v", after)
	}
}

func assertActivityRebuildConcurrency(t *testing.T, parent context.Context, pool *pgxpool.Pool, store *goldenpostgres.Store, service *goldenpath.Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	// Pause a real command after INSERT has acquired its Activity table lock.
	// The rebuild must wait, then take a fresh source snapshot after it commits.
	_, err := pool.Exec(ctx, `CREATE FUNCTION radishnexus.test_pause_activity() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_advisory_xact_lock(902210); RETURN NEW; END; $$;
		CREATE TRIGGER test_pause_activity BEFORE INSERT ON radishnexus.activity_items
		FOR EACH ROW EXECUTE FUNCTION radishnexus.test_pause_activity();`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(parent, `DROP TRIGGER test_pause_activity ON radishnexus.activity_items; DROP FUNCTION radishnexus.test_pause_activity()`); err != nil {
			t.Error(err)
		}
	}()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(parent)
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(902210)`); err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	var result goldenpath.CreateDecisionResult
	go func() {
		var err error
		result, err = service.CreateDecisionFromThread(ctx, invocation(principal("usr_contributor"), "cor_rebuild_concurrent"), goldenpath.CreateDecisionInput{
			ThreadID: "thr_private", ClientOperationID: "projection:concurrent", Question: "Committed while rebuild waits",
		})
		written <- err
	}()
	waitProjectionLock(t, ctx, pool, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=902210 AND NOT granted)`)
	rebuilt := make(chan error, 1)
	go func() { _, err := store.RebuildActivityProjection(ctx); rebuilt <- err }()
	waitProjectionLock(t, ctx, pool, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='radishnexus.activity_items'::regclass AND mode='ShareRowExclusiveLock' AND NOT granted)`)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := <-rebuilt; err != nil {
		t.Fatal(err)
	}
	view := loadNexusView(t, ctx, service, principal("usr_contributor"), entityref.Ref{Type: "decision", ID: result.Decision.ID})
	if len(view.Timeline) != 1 {
		t.Fatalf("rebuild lost concurrent command: %#v", view.Timeline)
	}
	var incomplete int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.outbox_deliveries WHERE consumer='activity-projector' AND state <> 'delivered'`).Scan(&incomplete); err != nil {
		t.Fatal(err)
	}
	if incomplete != 0 {
		t.Fatalf("synchronous projector left %d pending deliveries", incomplete)
	}
}

func waitProjectionLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, query).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertIncomingPermissionBoundary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, service *goldenpath.Service, decisionID string) {
	t.Helper()
	// All currently creatable reverse targets share the source Project. Verify
	// withdrawal at the authoritative object boundary; no cached count survives.
	ref := entityref.Ref{Type: "thread", ID: "thr_private"}
	view := loadNexusView(t, ctx, service, principal("usr_contributor"), ref)
	if len(view.Relations) == 0 || view.Relations[0].Direction != "incoming" || view.Relations[0].Target.ID != decisionID {
		t.Fatalf("Thread did not discover its Decision: %#v", view.Relations)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM radishnexus.thread_memberships WHERE thread_id='thr_private' AND user_id='usr_contributor'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetNexusView(ctx, principal("usr_contributor"), ref); err == nil {
		t.Fatal("revoked Thread remained discoverable")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO radishnexus.thread_memberships (workspace_id, thread_id, user_id) VALUES ('wrk_main','thr_private','usr_contributor')`); err != nil {
		t.Fatal(err)
	}
	// A separate hostile-read fixture exercises links from a different Project.
	// It is not evidence of a supported cross-Project creation command.
	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.projects (id, workspace_id, key, name, owner_team_id, visibility, status, created_by_kind, created_by_id)
		VALUES ('prj_incoming_private','wrk_main','PRIVATE','Private incoming','tem_main','restricted','active','user','usr_admin');
		INSERT INTO radishnexus.decisions (id,workspace_id,governing_project_id,question,status,proposer_id,created_by_kind,created_by_id)
		VALUES ('dec_incoming_private','wrk_main','prj_incoming_private','Hidden decision title','proposed','usr_admin','user','usr_admin');
		INSERT INTO radishnexus.tickets (id,workspace_id,governing_project_id,title,status,created_by)
		VALUES ('tkt_incoming_private','wrk_main','prj_incoming_private','Hidden ticket title','open','usr_admin');
		INSERT INTO radishnexus.entity_links (id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id)
		VALUES ('lnk_private_decision','wrk_main','decision','dec_incoming_private','derived-from','thread','thr_private','asserted','user','user','usr_admin');`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO radishnexus.entity_links (id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id)
		VALUES ('lnk_private_ticket','wrk_main','ticket','tkt_incoming_private','implements','decision',$1,'asserted','user','user','usr_admin')`, decisionID)
	if err != nil {
		t.Fatal(err)
	}
	decisionRef := entityref.Ref{Type: "decision", ID: decisionID}
	for _, target := range []entityref.Ref{ref, decisionRef} {
		before := loadNexusView(t, ctx, service, principal("usr_contributor"), target)
		if _, err := pool.Exec(ctx, `INSERT INTO radishnexus.project_memberships (workspace_id,project_id,user_id,role)
			VALUES ('wrk_main','prj_incoming_private','usr_contributor','viewer')`); err != nil {
			t.Fatal(err)
		}
		granted := loadNexusView(t, ctx, service, principal("usr_contributor"), target)
		if len(granted.Relations) != len(before.Relations)+1 {
			t.Fatalf("grant did not expose exactly one incoming result: %#v", granted.Relations)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM radishnexus.project_memberships WHERE project_id='prj_incoming_private' AND user_id='usr_contributor'`); err != nil {
			t.Fatal(err)
		}
		revoked := loadNexusView(t, ctx, service, principal("usr_contributor"), target)
		if !reflect.DeepEqual(before, revoked) {
			t.Fatalf("revoked incoming target left identity/count/timestamp residue: %#v", revoked)
		}
	}
}

// Count real database round trips without a permission cache or a mocked Store.
type relationQueryCounter struct{ queries atomic.Int64 }

func (counter *relationQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	counter.queries.Add(1)
	return ctx
}
func (*relationQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func assertIncomingRelationsScale(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO radishnexus.threads (id,workspace_id,governing_project_id,title,visibility,created_by)
		VALUES ('thr_relation_scale','wrk_main','prj_auth','Relation scale fixture','project','usr_contributor')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		writer := goldenpath.NewService(goldenpostgres.New(pool), &fixedIDs{values: []string{
			fmt.Sprintf("dec_scale_%03d", i), fmt.Sprintf("lnk_scale_%03d", i), fmt.Sprintf("evt_scale_%03d", i),
		}}, fixedClock{now: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)})
		if _, err := writer.CreateDecisionFromThread(ctx, invocation(principal("usr_contributor"), "cor_relation_scale"), goldenpath.CreateDecisionInput{
			ThreadID: "thr_relation_scale", ClientOperationID: fmt.Sprintf("scale:%03d", i), Question: fmt.Sprintf("Decision %03d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	counter := &relationQueryCounter{}
	config := pool.Config()
	config.ConnConfig.Tracer = counter
	measured, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	if err := measured.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	counter.queries.Store(0)
	start := time.Now()
	view, err := goldenpostgres.New(measured).GetNexusView(ctx, principal("usr_contributor"), entityref.Ref{Type: "thread", ID: "thr_relation_scale"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Relations) != 100 {
		t.Fatalf("incoming results truncated: %d", len(view.Relations))
	}
	for i, relation := range view.Relations {
		if relation.Direction != "incoming" || relation.Title != fmt.Sprintf("Decision %03d", i) {
			t.Fatalf("unstable incoming order: %#v", relation)
		}
	}
	t.Logf("incoming scale: 100 readable relations, %d SQL calls, %s", counter.queries.Load(), elapsed)
}
