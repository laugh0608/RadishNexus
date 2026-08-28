//go:build integration

package corecontracts

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgreSQLCoreContracts(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for the PostgreSQL contract experiment")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if err := ResetSchema(ctx, pool); err != nil {
		t.Fatalf("ResetSchema() error = %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	seedExperiment(t, ctx, pool)

	store := NewStore(pool)
	occurredAt := time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)
	decisionCommand := ProposeDecisionCommand{
		WorkspaceID:   "wrk_ALPHA",
		ProjectID:     "prj_ALPHA",
		ThreadID:      "prototype-thread-private-alpha",
		DecisionID:    "dec_RATE_LIMIT",
		LinkID:        "lnk_DECISION_THREAD",
		DecisionEvent: "evt_DECISION_PROPOSED",
		LinkEvent:     "evt_LINK_CREATED",
		CorrelationID: "cor_RATE_LIMIT",
		ProposerID:    "usr_OWNER",
		Question:      "登录接口是否增加速率限制？",
		OccurredAt:    occurredAt,
	}
	if err := store.ProposeDecision(ctx, decisionCommand); err != nil {
		t.Fatalf("ProposeDecision() error = %v", err)
	}

	assertCount(t, ctx, pool, "m0_core.decisions", 1)
	assertCount(t, ctx, pool, "m0_core.entity_links", 1)
	assertCount(t, ctx, pool, "m0_core.domain_events", 2)
	assertCount(t, ctx, pool, "m0_core.outbox_deliveries", 2)

	t.Run("stable identity survives rename", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
			UPDATE m0_core.projects
			SET key = 'alpha-renamed', name = 'Renamed Alpha Project', updated_at = clock_timestamp()
			WHERE workspace_id = 'wrk_ALPHA' AND id = 'prj_ALPHA'
		`); err != nil {
			t.Fatalf("rename Project error = %v", err)
		}

		var projectID string
		if err := pool.QueryRow(ctx, `
			SELECT id FROM m0_core.projects
			WHERE workspace_id = 'wrk_ALPHA' AND key = 'alpha-renamed'
		`).Scan(&projectID); err != nil {
			t.Fatalf("read renamed Project error = %v", err)
		}
		if projectID != "prj_ALPHA" {
			t.Fatalf("renamed Project ID = %q", projectID)
		}

		if _, err := pool.Exec(ctx, `
			UPDATE m0_core.projects SET id = 'prj_REPLACED' WHERE id = 'prj_ALPHA'
		`); err == nil {
			t.Fatal("stable Project ID update succeeded")
		}
	})

	t.Run("domain event facts are immutable", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
			UPDATE m0_core.domain_events
			SET payload = '{"status":"accepted"}'::jsonb
			WHERE event_id = 'evt_DECISION_PROPOSED'
		`); err == nil {
			t.Fatal("domain event payload update succeeded")
		}
	})

	t.Run("Decision requires evidence at commit", func(t *testing.T) {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("BeginTx() error = %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		_, err = tx.Exec(ctx, `
			INSERT INTO m0_core.decisions (
				id,
				workspace_id,
				question,
				status,
				proposer_id,
				created_by_kind,
				created_by_id
			) VALUES (
				'dec_WITHOUT_EVIDENCE',
				'wrk_ALPHA',
				'这个草案没有证据吗？',
				'proposed',
				'usr_OWNER',
				'user',
				'usr_OWNER'
			)
		`)
		if err != nil {
			t.Fatalf("insert evidence-free Decision error = %v", err)
		}
		if err := tx.Commit(ctx); err == nil {
			t.Fatal("Commit() succeeded for Decision without evidence")
		}
	})

	t.Run("cross Workspace EntityLink is rejected", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
			INSERT INTO m0_core.entity_links (
				id,
				workspace_id,
				from_type,
				from_id,
				relation_type,
				to_type,
				to_id,
				assertion,
				origin,
				created_by_kind,
				created_by_id
			) VALUES (
				'lnk_CROSS_WORKSPACE',
				'wrk_ALPHA',
				'decision',
				'dec_RATE_LIMIT',
				'derived-from',
				'thread',
				'prototype-thread-private-beta',
				'asserted',
				'user',
				'user',
				'usr_OWNER'
			)
		`)
		if err == nil {
			t.Fatal("cross Workspace EntityLink insert succeeded")
		}
		if !strings.Contains(err.Error(), "another Workspace") {
			t.Fatalf("cross Workspace error = %v", err)
		}
	})

	projected, err := store.RebuildActivities(ctx, 1)
	if err != nil {
		t.Fatalf("RebuildActivities() error = %v", err)
	}
	if projected != 5 {
		t.Fatalf("RebuildActivities() count = %d, want 5", projected)
	}
	assertCount(t, ctx, pool, "m0_core.activities", 5)

	projected, err = store.RebuildActivities(ctx, 1)
	if err != nil {
		t.Fatalf("second RebuildActivities() error = %v", err)
	}
	if projected != 5 {
		t.Fatalf("second RebuildActivities() count = %d, want 5", projected)
	}
	assertCount(t, ctx, pool, "m0_core.activities", 5)

	if _, err := pool.Exec(ctx, `
		UPDATE m0_core.outbox_deliveries
		SET state = 'delivered', delivered_at = clock_timestamp()
	`); err != nil {
		t.Fatalf("mark Outbox delivered error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM m0_core.outbox_deliveries WHERE state = 'delivered'
	`); err != nil {
		t.Fatalf("prune Outbox delivery state error = %v", err)
	}
	assertCount(t, ctx, pool, "m0_core.outbox_deliveries", 0)
	assertCount(t, ctx, pool, "m0_core.domain_events", 2)

	if _, err := pool.Exec(ctx, "DELETE FROM m0_core.activities"); err != nil {
		t.Fatalf("clear Activity projection error = %v", err)
	}
	projected, err = store.RebuildActivities(ctx, 1)
	if err != nil {
		t.Fatalf("rebuild after Outbox pruning error = %v", err)
	}
	if projected != 5 {
		t.Fatalf("rebuild after Outbox pruning count = %d, want 5", projected)
	}

	ciRunCommand := RecordCIRunCommand{
		WorkspaceID:    "wrk_ALPHA",
		IntegrationID:  "jenkins_PRIMARY",
		DeliveryID:     "delivery-42",
		ExternalRunKey: "auth-service/main/42",
		CIRunID:        "prototype-ci-run-42",
		EventID:        "evt_CI_RUN_RECORDED",
		CorrelationID:  "cor_CI_RUN_42",
		Status:         "succeeded",
		OccurredAt:     occurredAt.Add(time.Minute),
	}
	recorded, err := store.RecordCIRun(ctx, ciRunCommand)
	if err != nil {
		t.Fatalf("RecordCIRun() error = %v", err)
	}
	if !recorded {
		t.Fatal("RecordCIRun() first delivery was not recorded")
	}

	duplicateCommand := ciRunCommand
	duplicateCommand.CIRunID = "prototype-ci-run-duplicate"
	duplicateCommand.EventID = "evt_CI_RUN_DUPLICATE"
	recorded, err = store.RecordCIRun(ctx, duplicateCommand)
	if err != nil {
		t.Fatalf("RecordCIRun() duplicate error = %v", err)
	}
	if recorded {
		t.Fatal("RecordCIRun() recorded a duplicate delivery")
	}
	assertCount(t, ctx, pool, "m0_core.inbound_deliveries", 1)
	assertCount(t, ctx, pool, "m0_core.ci_runs", 1)
	assertCount(t, ctx, pool, "m0_core.domain_events", 3)

	projected, err = store.RebuildActivities(ctx, 1)
	if err != nil {
		t.Fatalf("final RebuildActivities() error = %v", err)
	}
	if projected != 6 {
		t.Fatalf("final RebuildActivities() count = %d, want 6", projected)
	}
	assertCount(t, ctx, pool, "m0_core.activities", 6)
}

func seedExperiment(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	statements := []string{
		`INSERT INTO m0_core.workspaces (id, name) VALUES
			('wrk_ALPHA', 'Alpha'),
			('wrk_BETA', 'Beta')`,
		`INSERT INTO m0_core.projects (
			id,
			workspace_id,
			key,
			name,
			owner_team_id,
			visibility,
			status,
			created_by_kind,
			created_by_id
		) VALUES (
			'prj_ALPHA',
			'wrk_ALPHA',
			'alpha',
			'Alpha Project',
			'team_ALPHA',
			'workspace',
			'active',
			'user',
			'usr_OWNER'
		)`,
		`INSERT INTO m0_core.threads (id, workspace_id, visibility, title) VALUES
			('prototype-thread-private-alpha', 'wrk_ALPHA', 'restricted', 'Private Alpha Thread'),
			('prototype-thread-private-beta', 'wrk_BETA', 'restricted', 'Private Beta Thread')`,
	}

	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed statement error = %v", err)
		}
	}
}

func assertCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	want int,
) {
	t.Helper()

	allowed := map[string]bool{
		"m0_core.activities":         true,
		"m0_core.ci_runs":            true,
		"m0_core.decisions":          true,
		"m0_core.domain_events":      true,
		"m0_core.entity_links":       true,
		"m0_core.inbound_deliveries": true,
		"m0_core.outbox_deliveries":  true,
	}
	if !allowed[table] {
		t.Fatalf("assertCount() disallowed table %q", table)
	}

	var got int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s error = %v", table, err)
	}
	if got != want {
		t.Fatalf("count %s = %d, want %d", table, got, want)
	}
}
