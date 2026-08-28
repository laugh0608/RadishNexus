//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type prefixedSequenceIDs struct{ next int }

func (ids *prefixedSequenceIDs) NewID(prefix string) (string, error) {
	ids.next++
	return fmt.Sprintf("%stest_%02d", prefix, ids.next), nil
}

type fixedIDs struct{ values []string }

func (ids *fixedIDs) NewID(_ string) (string, error) {
	if len(ids.values) == 0 {
		return "", errors.New("fixed ID sequence exhausted")
	}
	value := ids.values[0]
	ids.values = ids.values[1:]
	return value, nil
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestGoldenPathPermissionsAndAtomicity(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for integration tests")
	}
	ctx := context.Background()

	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgx.Connect() error = %v", err)
	}
	if err := db.Migrate(ctx, connection); err != nil {
		t.Fatalf("db.Migrate() first run error = %v", err)
	}
	if err := db.Migrate(ctx, connection); err != nil {
		t.Fatalf("db.Migrate() idempotent run error = %v", err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatalf("migration connection Close() error = %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	seedGoldenPath(t, ctx, pool)

	store := goldenpostgres.New(pool)
	clock := fixedClock{now: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}
	service := goldenpath.NewService(store, &prefixedSequenceIDs{}, clock)
	contributor := principal("usr_contributor")
	decider := principal("usr_decider")
	reader := principal("usr_reader")
	admin := principal("usr_admin")

	_, err = service.CreateDecisionFromThread(ctx, invocation(reader, "cor_reader_denied"), goldenpath.CreateDecisionInput{
		ThreadID: "thr_private",
		Question: "Should not discover this thread?",
	})
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("reader CreateDecisionFromThread() error = %v, want not found", err)
	}

	decision, err := service.CreateDecisionFromThread(ctx, invocation(contributor, "cor_propose"), goldenpath.CreateDecisionInput{
		ThreadID: "thr_private",
		Question: "Should the login API add rate limiting?",
	})
	if err != nil {
		t.Fatalf("CreateDecisionFromThread() error = %v", err)
	}
	if decision.Status != "proposed" || decision.GoverningProjectID != "prj_auth" {
		t.Fatalf("created Decision = %#v", decision)
	}
	assertCounts(t, ctx, pool, 1, 1, 1, 1)

	_, err = service.AcceptDecision(ctx, invocation(contributor, "cor_contributor_accept"), goldenpath.AcceptDecisionInput{
		DecisionID: decision.ID,
		Outcome:    "Use a token bucket.",
		Rationale:  "Bound abuse while keeping bursts usable.",
	})
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("contributor AcceptDecision() error = %v, want forbidden", err)
	}
	assertDecisionStatus(t, ctx, pool, decision.ID, "proposed")
	assertCounts(t, ctx, pool, 1, 1, 1, 1)

	_, err = service.AcceptDecision(ctx, invocation(admin, "cor_admin_accept"), goldenpath.AcceptDecisionInput{
		DecisionID: decision.ID,
		Outcome:    "Use a token bucket.",
		Rationale:  "An admin without evidence access must not confirm this.",
	})
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("admin without evidence AcceptDecision() error = %v, want forbidden", err)
	}
	assertDecisionStatus(t, ctx, pool, decision.ID, "proposed")
	assertCounts(t, ctx, pool, 1, 1, 1, 1)

	decision, err = service.AcceptDecision(ctx, invocation(decider, "cor_accept"), goldenpath.AcceptDecisionInput{
		DecisionID: decision.ID,
		Outcome:    "Use a token bucket.",
		Rationale:  "Bound abuse while keeping bursts usable.",
	})
	if err != nil {
		t.Fatalf("decider AcceptDecision() error = %v", err)
	}
	if decision.Status != "accepted" || len(decision.DeciderIDs) != 1 || decision.DeciderIDs[0] != decider.ID {
		t.Fatalf("accepted Decision = %#v", decision)
	}

	ticket, err := service.CreateTicketFromDecision(ctx, invocation(contributor, "cor_ticket"), goldenpath.CreateTicketInput{
		DecisionID: decision.ID,
		Title:      "Implement login token bucket",
	})
	if err != nil {
		t.Fatalf("CreateTicketFromDecision() error = %v", err)
	}
	if ticket.Status != "open" || ticket.GoverningProjectID != "prj_auth" {
		t.Fatalf("created Ticket = %#v", ticket)
	}
	assertCounts(t, ctx, pool, 1, 2, 3, 3)

	visibleRelations, err := service.ListRelations(ctx, decider, entityref.Ref{Type: "decision", ID: decision.ID})
	if err != nil {
		t.Fatalf("decider ListRelations() error = %v", err)
	}
	if len(visibleRelations) != 1 || visibleRelations[0].State != goldenpath.ProjectionVisible ||
		visibleRelations[0].Target != (entityref.Ref{Type: "thread", ID: "thr_private"}) ||
		visibleRelations[0].Title != "Private rate-limit discussion" {
		t.Fatalf("visible relations = %#v", visibleRelations)
	}

	for _, testPrincipal := range []authz.Principal{reader, admin} {
		restrictedRelations, err := service.ListRelations(ctx, testPrincipal, entityref.Ref{Type: "decision", ID: decision.ID})
		if err != nil {
			t.Fatalf("%s ListRelations() error = %v", testPrincipal.ID, err)
		}
		if len(restrictedRelations) != 1 {
			t.Fatalf("%s restricted relations = %#v", testPrincipal.ID, restrictedRelations)
		}
		restricted := restrictedRelations[0]
		if restricted.State != goldenpath.ProjectionRestricted || restricted.RelationType != "" ||
			restricted.Target != (entityref.Ref{}) || restricted.Title != "" {
			t.Fatalf("%s restricted projection leaks fields: %#v", testPrincipal.ID, restricted)
		}
	}

	_, err = service.ListRelations(ctx, reader, entityref.Ref{Type: "thread", ID: "thr_private"})
	if !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("direct restricted Thread ListRelations() error = %v, want not found", err)
	}

	ticketRelations, err := service.ListRelations(ctx, reader, entityref.Ref{Type: "ticket", ID: ticket.ID})
	if err != nil {
		t.Fatalf("reader Ticket ListRelations() error = %v", err)
	}
	if len(ticketRelations) != 1 || ticketRelations[0].State != goldenpath.ProjectionVisible ||
		ticketRelations[0].Target.ID != decision.ID || ticketRelations[0].RelationType != "implements" {
		t.Fatalf("Ticket relations = %#v", ticketRelations)
	}

	atomicFailureService := goldenpath.NewService(store, &fixedIDs{values: []string{
		"dec_atomic_failure",
		"lnk_atomic_failure",
		"evt_test_06",
	}}, clock)
	_, err = atomicFailureService.CreateDecisionFromThread(ctx, invocation(contributor, "cor_atomic_failure"), goldenpath.CreateDecisionInput{
		ThreadID: "thr_private",
		Question: "This transaction must roll back.",
	})
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate event CreateDecisionFromThread() error = %v, want conflict", err)
	}
	assertAbsent(t, ctx, pool, "radishnexus.decisions", "dec_atomic_failure")
	assertAbsent(t, ctx, pool, "radishnexus.entity_links", "lnk_atomic_failure")
	assertCounts(t, ctx, pool, 1, 2, 3, 3)

	assertDatabaseConstraints(t, ctx, pool, decision.ID)
}

func principal(userID string) authz.Principal {
	return authz.Principal{Kind: authz.PrincipalUser, ID: userID, WorkspaceID: "wrk_main"}
}

func invocation(principal authz.Principal, correlationID string) goldenpath.Invocation {
	return goldenpath.Invocation{
		Principal:     principal,
		SourceKind:    "api",
		CorrelationID: correlationID,
	}
}

func seedGoldenPath(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.users (id, display_name) VALUES
			('usr_contributor', 'Contributor'),
			('usr_decider', 'Decider'),
			('usr_reader', 'Reader'),
			('usr_admin', 'Admin');
		INSERT INTO radishnexus.workspaces (id, name) VALUES
			('wrk_main', 'Main'),
			('wrk_other', 'Other');
		INSERT INTO radishnexus.workspace_memberships (workspace_id, user_id) VALUES
			('wrk_main', 'usr_contributor'),
			('wrk_main', 'usr_decider'),
			('wrk_main', 'usr_reader'),
			('wrk_main', 'usr_admin'),
			('wrk_other', 'usr_contributor');
		INSERT INTO radishnexus.teams (id, workspace_id, name) VALUES
			('tem_main', 'wrk_main', 'Main Team'),
			('tem_other', 'wrk_other', 'Other Team');
		INSERT INTO radishnexus.projects (
			id, workspace_id, key, name, owner_team_id, visibility, status,
			created_by_kind, created_by_id
		) VALUES
			('prj_auth', 'wrk_main', 'AUTH', 'Authentication', 'tem_main', 'workspace', 'active', 'user', 'usr_contributor'),
			('prj_other', 'wrk_other', 'OTHER', 'Other', 'tem_other', 'workspace', 'active', 'user', 'usr_contributor');
		INSERT INTO radishnexus.project_memberships (workspace_id, project_id, user_id, role) VALUES
			('wrk_main', 'prj_auth', 'usr_contributor', 'contributor'),
			('wrk_main', 'prj_auth', 'usr_decider', 'decider'),
			('wrk_main', 'prj_auth', 'usr_reader', 'viewer'),
			('wrk_main', 'prj_auth', 'usr_admin', 'admin');
		INSERT INTO radishnexus.threads (
			id, workspace_id, governing_project_id, title, visibility, created_by
		) VALUES
			('thr_private', 'wrk_main', 'prj_auth', 'Private rate-limit discussion', 'restricted', 'usr_contributor'),
			('thr_other', 'wrk_other', 'prj_other', 'Other Workspace thread', 'project', 'usr_contributor');
		INSERT INTO radishnexus.thread_memberships (workspace_id, thread_id, user_id) VALUES
			('wrk_main', 'thr_private', 'usr_contributor'),
			('wrk_main', 'thr_private', 'usr_decider');
	`)
	if err != nil {
		t.Fatalf("seed Golden Path error = %v", err)
	}
}

func assertCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, decisions, links, events, outbox int) {
	t.Helper()
	for table, want := range map[string]int{
		"radishnexus.decisions":         decisions,
		"radishnexus.entity_links":      links,
		"radishnexus.domain_events":     events,
		"radishnexus.outbox_deliveries": outbox,
	} {
		var got int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s error = %v", table, err)
		}
		if got != want {
			t.Fatalf("count %s = %d, want %d", table, got, want)
		}
	}
}

func assertDecisionStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, decisionID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM radishnexus.decisions WHERE id = $1
	`, decisionID).Scan(&got); err != nil {
		t.Fatalf("load Decision status error = %v", err)
	}
	if got != want {
		t.Fatalf("Decision status = %q, want %q", got, want)
	}
}

func assertAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, id string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+table+" WHERE id = $1)", id).Scan(&exists); err != nil {
		t.Fatalf("check %s absence error = %v", table, err)
	}
	if exists {
		t.Fatalf("%s unexpectedly contains %s", table, id)
	}
}

func assertDatabaseConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool, decisionID string) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.decisions (
			id, workspace_id, governing_project_id, question, status,
			proposer_id, created_by_kind, created_by_id
		) VALUES (
			'dec_without_evidence', 'wrk_main', 'prj_auth', 'Missing evidence?', 'proposed',
			'usr_contributor', 'user', 'usr_contributor'
		)
	`)
	assertPGCode(t, err, "23514", "Decision evidence constraint")

	_, err = pool.Exec(ctx, `
		INSERT INTO radishnexus.entity_links (
			id, workspace_id, from_type, from_id, relation_type, to_type, to_id,
			assertion, origin, created_by_kind, created_by_id
		) VALUES (
			'lnk_cross_workspace', 'wrk_main', 'decision', $1, 'derived-from', 'thread', 'thr_other',
			'asserted', 'user', 'user', 'usr_contributor'
		)
	`, decisionID)
	assertPGCode(t, err, "23514", "cross-Workspace EntityLink constraint")

	_, err = pool.Exec(ctx, `
		UPDATE radishnexus.decisions
		SET status = 'superseded', updated_at = clock_timestamp()
		WHERE id = $1
	`, decisionID)
	assertPGCode(t, err, "23514", "superseded Decision replacement constraint")

	_, err = pool.Exec(ctx, `
		UPDATE radishnexus.domain_events
		SET payload = '{"status":"tampered"}'::jsonb
		WHERE event_id = 'evt_test_06'
	`)
	assertPGCode(t, err, "23514", "immutable domain event constraint")
}

func assertPGCode(t *testing.T, err error, wantCode, operation string) {
	t.Helper()
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != wantCode {
		t.Fatalf("%s error = %v, want PostgreSQL code %s", operation, err, wantCode)
	}
}
