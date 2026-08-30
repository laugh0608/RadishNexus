//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func assertStagingDeploymentSlice(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *goldenpostgres.Store,
	service *goldenpath.Service,
	ciRun goldenpath.CIRun,
) {
	t.Helper()
	seedDeploymentTargets(t, ctx, pool)

	startedAt := time.Date(2026, 8, 28, 11, 56, 0, 0, time.UTC)
	completedAt := time.Date(2026, 8, 28, 11, 59, 0, 0, time.UTC)
	input := goldenpath.RecordStagingDeploymentInput{
		EnvironmentID: "env_staging",
		CIRunID:       ciRun.ID,
		Status:        "succeeded",
		StartedAt:     &startedAt,
		CompletedAt:   completedAt,
	}

	_, err := service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_reader"), "cor_unauthorized_deployment"),
		input,
	)
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("unauthorized RecordStagingDeployment() error = %v, want forbidden", err)
	}
	assertDeploymentCounts(t, ctx, pool, 0, 2, 5, 2)

	productionInput := input
	productionInput.EnvironmentID = "env_production"
	_, err = service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_production_denied"),
		productionInput,
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("production RecordStagingDeployment() error = %v, want conflict", err)
	}
	assertDeploymentCounts(t, ctx, pool, 0, 2, 5, 2)

	deployment, err := service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_staging_deployment"),
		input,
	)
	if err != nil {
		t.Fatalf("RecordStagingDeployment() error = %v", err)
	}
	if deployment.ID == "" || deployment.WorkspaceID != "wrk_main" ||
		deployment.EnvironmentID != "env_staging" || deployment.CIRunID != ciRun.ID ||
		deployment.Status != "succeeded" || deployment.RecordedBy != "usr_contributor" ||
		deployment.SourceKind != "api" || !deployment.CompletedAt.Equal(completedAt) {
		t.Fatalf("staging Deployment = %#v", deployment)
	}
	assertDeploymentCounts(t, ctx, pool, 1, 3, 6, 3)
	assertDeploymentAuditFields(t, ctx, pool, deployment.ID)

	_, err = service.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_duplicate_deployment"),
		input,
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate staging Deployment error = %v, want conflict", err)
	}
	assertDeploymentCounts(t, ctx, pool, 1, 3, 6, 3)

	otherCIRunID := loadOtherSucceededCIRunID(t, ctx, pool, ciRun.ID)
	existingEventID := loadDomainEventID(t, ctx, pool, "deployment.recorded", deployment.ID)
	atomicFailureService := goldenpath.NewService(store, &fixedIDs{values: []string{
		"dpl_atomic_failure", "lnk_atomic_deployment", existingEventID,
	}}, fixedClock{now: time.Date(2026, 8, 28, 12, 1, 0, 0, time.UTC)})
	atomicFailureInput := input
	atomicFailureInput.CIRunID = otherCIRunID
	_, err = atomicFailureService.RecordStagingDeployment(
		ctx,
		invocation(principal("usr_contributor"), "cor_atomic_deployment"),
		atomicFailureInput,
	)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate event staging Deployment error = %v, want conflict", err)
	}
	assertAbsent(t, ctx, pool, "radishnexus.deployments", "dpl_atomic_failure")
	assertAbsent(t, ctx, pool, "radishnexus.entity_links", "lnk_atomic_deployment")
	assertDeploymentCounts(t, ctx, pool, 1, 3, 6, 3)

	assertDeploymentDatabaseConstraints(t, ctx, pool, deployment, otherCIRunID)

	projected, err := store.RebuildActivityProjection(ctx)
	if err != nil {
		t.Fatalf("RebuildActivityProjection() with Deployment error = %v", err)
	}
	if projected != 6 {
		t.Fatalf("RebuildActivityProjection() projected = %d, want 6", projected)
	}
	assertTableCount(t, ctx, pool, "radishnexus.activity_items", 6)
	assertDeploymentActivityFacts(t, ctx, pool, deployment.ID, ciRun.ID)
}

func seedDeploymentTargets(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.environments (
			id, workspace_id, key, name, classification, owner_team_id,
			status, created_by_kind, created_by_id
		) VALUES
			('env_staging', 'wrk_main', 'STAGING', 'Staging', 'staging',
			 'tem_main', 'active', 'user', 'usr_admin'),
			('env_production', 'wrk_main', 'PRODUCTION', 'Production', 'production',
			 'tem_main', 'active', 'user', 'usr_admin');
		INSERT INTO radishnexus.environment_deployment_authorizations (
			id, workspace_id, environment_id, user_id, granted_by
		) VALUES
			('dpa_staging_contributor', 'wrk_main', 'env_staging', 'usr_contributor', 'usr_admin'),
			('dpa_production_contributor', 'wrk_main', 'env_production', 'usr_contributor', 'usr_admin');
	`)
	if err != nil {
		t.Fatalf("seed Deployment targets error = %v", err)
	}
}

func assertDeploymentCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	deployments int,
	links int,
	events int,
	outbox int,
) {
	t.Helper()
	for table, want := range map[string]int{
		"radishnexus.deployments":       deployments,
		"radishnexus.entity_links":      links,
		"radishnexus.domain_events":     events,
		"radishnexus.outbox_deliveries": outbox,
	} {
		assertTableCount(t, ctx, pool, table, want)
	}
}

func assertDeploymentAuditFields(t *testing.T, ctx context.Context, pool *pgxpool.Pool, deploymentID string) {
	t.Helper()
	var authorizationID string
	var recordedBy string
	var sourceKind string
	var sourceID *string
	if err := pool.QueryRow(ctx, `
		SELECT authorization_id, recorded_by, source_kind, source_id
		FROM radishnexus.deployments
		WHERE id = $1
	`, deploymentID).Scan(&authorizationID, &recordedBy, &sourceKind, &sourceID); err != nil {
		t.Fatalf("load Deployment audit fields error = %v", err)
	}
	if authorizationID != "dpa_staging_contributor" || recordedBy != "usr_contributor" ||
		sourceKind != "api" || sourceID != nil {
		t.Fatalf("Deployment audit fields = authorization %q, actor %q, source %q/%v",
			authorizationID, recordedBy, sourceKind, sourceID)
	}
}

func loadOtherSucceededCIRunID(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	excludedID string,
) string {
	t.Helper()
	var ciRunID string
	if err := pool.QueryRow(ctx, `
		SELECT id
		FROM radishnexus.ci_runs
		WHERE workspace_id = 'wrk_main' AND status = 'succeeded' AND id <> $1
		ORDER BY id
		LIMIT 1
	`, excludedID).Scan(&ciRunID); err != nil {
		t.Fatalf("load another succeeded CI Run error = %v", err)
	}
	return ciRunID
}

func assertDeploymentDatabaseConstraints(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	deployment goldenpath.Deployment,
	otherCIRunID string,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		UPDATE radishnexus.deployments SET status = 'failed' WHERE id = $1
	`, deployment.ID)
	assertPGCode(t, err, "23514", "immutable Deployment fact")

	_, err = pool.Exec(ctx, `
		UPDATE radishnexus.environments
		SET classification = 'production'
		WHERE id = 'env_staging'
	`)
	assertPGCode(t, err, "23514", "immutable Environment classification")

	_, err = pool.Exec(ctx, `
		INSERT INTO radishnexus.deployments (
			id, workspace_id, environment_id, ci_run_id, authorization_id,
			status, completed_at, recorded_by, source_kind, recorded_at
		) VALUES (
			'dpl_production_rejected', 'wrk_main', 'env_production', $1,
			'dpa_production_contributor', 'succeeded', clock_timestamp(),
			'usr_contributor', 'api', clock_timestamp()
		)
	`, otherCIRunID)
	assertPGCode(t, err, "23514", "database staging-only Deployment constraint")
}

func assertDeploymentActivityFacts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	deploymentID string,
	ciRunID string,
) {
	t.Helper()
	var subjects []byte
	var safeFacts []byte
	var actorKind string
	var actorID string
	if err := pool.QueryRow(ctx, `
		SELECT subject_refs, safe_facts, actor_kind, actor_id
		FROM radishnexus.activity_items
		WHERE target_type = 'deployment' AND target_id = $1
	`, deploymentID).Scan(&subjects, &safeFacts, &actorKind, &actorID); err != nil {
		t.Fatalf("load Deployment Activity facts error = %v", err)
	}
	var decodedSubjects []map[string]string
	if err := json.Unmarshal(subjects, &decodedSubjects); err != nil {
		t.Fatalf("decode Deployment Activity subjects error = %v", err)
	}
	var decodedFacts map[string]string
	if err := json.Unmarshal(safeFacts, &decodedFacts); err != nil {
		t.Fatalf("decode Deployment Activity safe facts error = %v", err)
	}
	if len(decodedSubjects) != 2 || decodedSubjects[0]["type"] != "environment" ||
		decodedSubjects[0]["id"] != "env_staging" || decodedSubjects[1]["type"] != "ci-run" ||
		decodedSubjects[1]["id"] != ciRunID || len(decodedFacts) != 1 ||
		decodedFacts["status"] != "succeeded" || actorKind != "user" || actorID != "usr_contributor" {
		t.Fatalf("Deployment Activity subjects = %#v, facts = %#v, actor = %s/%s",
			decodedSubjects, decodedFacts, actorKind, actorID)
	}
}
