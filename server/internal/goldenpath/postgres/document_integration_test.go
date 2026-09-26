//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/markdown"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func documentDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("nexus_document_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
		if err != nil {
			t.Error(err)
		}
		admin.Close(context.Background())
	})
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Migrate(ctx, conn.Conn())
	conn.Release()
	if err != nil {
		t.Fatal(err)
	}
	seedGoldenPath(t, ctx, pool)
	if _, err = pool.Exec(ctx, `INSERT INTO radishnexus.user_accounts(user_id,status,created_at) SELECT id,'active',created_at FROM radishnexus.users`); err != nil {
		t.Fatal(err)
	}
	return ctx, pool
}
func documentTicket(t *testing.T, ctx context.Context, s *goldenpath.Service) string {
	t.Helper()
	d, err := s.CreateDecisionFromThread(ctx, invocation(principal("usr_contributor"), "document-propose"), goldenpath.CreateDecisionInput{ThreadID: "thr_private", ClientOperationID: "document-propose", Question: "设计文档"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AcceptDecision(ctx, invocation(principal("usr_decider"), "document-accept"), goldenpath.AcceptDecisionInput{DecisionID: d.Decision.ID, ClientOperationID: "document-accept", Outcome: "采用", Rationale: "明确来源"})
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := s.CreateTicketFromDecision(ctx, invocation(principal("usr_contributor"), "document-ticket"), goldenpath.CreateTicketInput{DecisionID: d.Decision.ID, ClientOperationID: "document-ticket", Title: "实现"})
	if err != nil {
		t.Fatal(err)
	}
	return ticket.Ticket.ID
}
func TestDocumentTransactionsAndCurrentPermissions(t *testing.T) {
	ctx, pool := documentDatabase(t)
	store := goldenpostgres.New(pool)
	core := goldenpath.NewService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	service := goldenpath.NewDocumentService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	ticket := documentTicket(t, ctx, core)
	p := principal("usr_contributor")
	inv := invocation(p, "document-test")
	create := goldenpath.DocumentInput{TargetID: ticket, ClientOperationID: "create", Title: " 中文😀 ", BodyMarkdown: "# 正文\r\n\r\na  \r\nb", FormatVersion: markdown.Format}
	doc, err := service.WriteDocument(ctx, inv, "document.create", create)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Created || doc.AppliedRevision != 1 {
		t.Fatal(doc)
	}
	v, err := service.ReadDocument(ctx, p, doc.Ref.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v.Current.Title != "中文😀" || v.Current.BodyMarkdown != "# 正文\n\na  \nb" || len(v.Relations) != 1 || v.Relations[0].Target.ID != ticket || len(v.Timeline) != 1 {
		t.Fatalf("incomplete creation: %#v", v)
	}
	ticketView, err := core.GetNexusView(ctx, p, entityref.Ref{Type: "ticket", ID: ticket})
	if err != nil {
		t.Fatal(err)
	}
	if len(ticketView.Relations) != 2 {
		t.Fatal("missing outgoing Document", ticketView.Relations)
	}
	duplicate, err := service.WriteDocument(ctx, inv, "document.create", create)
	if err != nil || duplicate.Created || duplicate.Ref != doc.Ref {
		t.Fatal("creation retry", duplicate, err)
	}
	changed := create
	changed.Title = "变化"
	if _, err = service.WriteDocument(ctx, inv, "document.create", changed); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("changed replay", err)
	}
	save := goldenpath.DocumentInput{TargetID: doc.Ref.ID, ClientOperationID: "save-a", Title: "第二版", BodyMarkdown: "保存内容", FormatVersion: markdown.Format, BaseRevision: 1}
	other := save
	other.ClientOperationID = "save-b"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	winners := make(chan goldenpath.DocumentInput, 2)
	for _, input := range []goldenpath.DocumentInput{save, other} {
		wg.Go(func() {
			_, e := service.WriteDocument(ctx, inv, "document.save", input)
			results <- e
			if e == nil {
				winners <- input
			}
		})
	}
	wg.Wait()
	close(results)
	close(winners)
	success, conflicts := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else {
			var conflict *goldenpath.RevisionConflict
			if !errors.As(e, &conflict) || conflict.CurrentRevision != 2 {
				t.Fatal(e)
			}
			conflicts++
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	winner := <-winners
	restore := goldenpath.DocumentInput{TargetID: doc.Ref.ID, ClientOperationID: "restore", BaseRevision: 2, RestoreRevision: 1}
	restored, err := service.WriteDocument(ctx, inv, "document.restore", restore)
	if err != nil || restored.AppliedRevision != 3 {
		t.Fatal(restored, err)
	}
	retry, err := service.WriteDocument(ctx, inv, "document.save", winner)
	if err != nil || retry.AppliedRevision != 2 {
		t.Fatal("old receipt must stay at version 2", retry, err)
	}
	v, err = service.ReadDocument(ctx, p, doc.Ref.ID, 0)
	if err != nil || v.Current.Revision != 3 || v.Current.BodyMarkdown != "# 正文\n\na  \nb" || v.Current.RestoredFromRevision == nil || *v.Current.RestoredFromRevision != 1 || len(v.Timeline) != 3 {
		t.Fatal("restore/current", v, err)
	}
	before := v.Timeline
	if _, err = store.RebuildActivityProjection(ctx); err != nil {
		t.Fatal(err)
	}
	v, err = service.ReadDocument(ctx, p, doc.Ref.ID, 0)
	if err != nil || !reflect.DeepEqual(before, v.Timeline) {
		t.Fatal("rebuild mismatch", err)
	}
	history, err := service.ListDocumentRevisions(ctx, p, doc.Ref.ID, 0, 2)
	if err != nil || len(history.Items) != 2 || history.NextRevision != 2 {
		t.Fatal(history, err)
	}
	history, err = service.ListDocumentRevisions(ctx, p, doc.Ref.ID, history.NextRevision, 2)
	if err != nil || len(history.Items) != 1 || history.Items[0].Revision != 1 {
		t.Fatal(history, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.document_revisions SET title='overwrite' WHERE document_id=$1`, doc.Ref.ID); err == nil {
		t.Fatal("mutable revision")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM radishnexus.document_revisions WHERE document_id=$1`, doc.Ref.ID); err == nil {
		t.Fatal("deletable revision")
	}
	invalid := save
	invalid.BaseRevision = 3
	invalid.ClientOperationID = "unsafe"
	invalid.BodyMarkdown = "<script>bad</script>"
	if _, err = service.WriteDocument(ctx, inv, "document.save", invalid); !errors.Is(err, authz.ErrInvalid) {
		t.Fatal("unsafe save", err)
	}
	var receipts int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.collaboration_command_receipts WHERE client_operation_id='unsafe'`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("failed receipt persisted", err)
	}
	// Projection failure must roll back the revision, pointer, receipt and event.
	_, err = pool.Exec(ctx, `CREATE FUNCTION radishnexus.fail_document_activity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.target_type='document' THEN RAISE EXCEPTION 'injected failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_document_activity BEFORE INSERT ON radishnexus.activity_items FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_document_activity()`)
	if err != nil {
		t.Fatal(err)
	}
	invalid.BodyMarkdown = "valid"
	if _, err = service.WriteDocument(ctx, inv, "document.save", invalid); err == nil {
		t.Fatal("projection failure hidden")
	}
	v, err = service.ReadDocument(ctx, p, doc.Ref.ID, 0)
	if err != nil || v.Current.Revision != 3 {
		t.Fatal("partial write", err)
	}
	if _, err = pool.Exec(ctx, `DROP TRIGGER fail_document_activity ON radishnexus.activity_items`); err != nil {
		t.Fatal(err)
	}
	reader := principal("usr_reader")
	if _, err = service.ReadDocument(ctx, reader, doc.Ref.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = service.PreviewDocument(ctx, reader, "prj_auth", "x", markdown.Format); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("viewer preview", err)
	}
	cross := p
	cross.WorkspaceID = "wrk_other"
	if _, err = service.ReadDocument(ctx, cross, doc.Ref.ID, 1); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("cross workspace", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`); err != nil {
		t.Fatal(err)
	}
	if _, err = service.WriteDocument(ctx, inv, "document.save", winner); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("archived retry", err)
	}
	if _, err = service.ReadDocument(ctx, p, doc.Ref.ID, 1); err != nil {
		t.Fatal("archived history", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id='wrk_main' AND user_id='usr_contributor'`); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReadDocument(ctx, p, doc.Ref.ID, 1); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("revoked history", err)
	}
	if _, err = service.WriteDocument(ctx, inv, "document.create", create); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("revoked creation retry", err)
	}
}

func TestDocumentSaveWaitsForArchiveAndRevocation(t *testing.T) {
	ctx, pool := documentDatabase(t)
	store := goldenpostgres.New(pool)
	core := goldenpath.NewService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	service := goldenpath.NewDocumentService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	ticket := documentTicket(t, ctx, core)
	inv := invocation(principal("usr_contributor"), "race")
	doc, err := service.WriteDocument(ctx, inv, "document.create", goldenpath.DocumentInput{TargetID: ticket, ClientOperationID: "race-create", Title: "Race", FormatVersion: markdown.Format})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, sql string
		want      error
	}{
		{"archive", `UPDATE radishnexus.projects SET status='archived' WHERE id='prj_auth'`, authz.ErrForbidden},
		{"revoke", `UPDATE radishnexus.projects SET status='active',visibility='restricted' WHERE id='prj_auth'; DELETE FROM radishnexus.project_memberships WHERE project_id='prj_auth' AND user_id='usr_contributor'`, authz.ErrNotFound},
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, scenario.sql); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := service.WriteDocument(ctx, inv, "document.save", goldenpath.DocumentInput{TargetID: doc.Ref.ID, ClientOperationID: scenario.name, Title: "Denied", FormatVersion: markdown.Format, BaseRevision: 1})
			done <- err
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var waiting bool
			err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%FROM radishnexus.projects%')`).Scan(&waiting)
			if err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			if time.Now().After(deadline) {
				_ = tx.Rollback(ctx)
				t.Fatal("save did not wait for Project lock")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err = <-done; !errors.Is(err, scenario.want) {
			t.Fatalf("%s returned %v", scenario.name, err)
		}
	}
	var revision int
	if err = pool.QueryRow(ctx, `SELECT current_revision FROM radishnexus.documents WHERE id=$1`, doc.Ref.ID).Scan(&revision); err != nil || revision != 1 {
		t.Fatal("revocation crossed write", revision, err)
	}
}
