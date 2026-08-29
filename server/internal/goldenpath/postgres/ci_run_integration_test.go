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

const (
	jenkinsDigestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	jenkinsDigestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func assertJenkinsCIRunSlice(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *goldenpostgres.Store,
	service *goldenpath.Service,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.components (
			id, workspace_id, key, name, type, owner_team_id, lifecycle,
			created_by_kind, created_by_id
		) VALUES (
			'cmp_auth', 'wrk_main', 'AUTH-SERVICE', 'Authentication Service',
			'service', 'tem_main', 'active', 'user', 'usr_contributor'
		)
	`)
	if err != nil {
		t.Fatalf("seed Component error = %v", err)
	}

	delivery := goldenpath.VerifiedJenkinsDelivery{
		WorkspaceID:   "wrk_main",
		SourceID:      "jenkins-main",
		DeliveryID:    "delivery-42",
		PayloadSHA256: jenkinsDigestA,
	}
	input := goldenpath.RecordCompletedCIRunInput{
		ComponentID:    "cmp_auth",
		ExternalRunKey: "auth-service/main/42",
		Status:         "succeeded",
		CompletedAt:    time.Date(2026, 8, 29, 11, 55, 0, 0, time.UTC),
	}

	first, err := service.RecordCompletedJenkinsRun(ctx, delivery, input)
	if err != nil {
		t.Fatalf("RecordCompletedJenkinsRun() first delivery error = %v", err)
	}
	if first.Duplicate || first.CIRun.ID == "" || first.CIRun.Status != "succeeded" ||
		first.CIRun.ComponentID != "cmp_auth" || first.CIRun.SourceKind != "jenkins" {
		t.Fatalf("first CI Run receipt = %#v", first)
	}
	assertCIRunCounts(t, ctx, pool, 1, 1, 4, 1)

	duplicate, err := service.RecordCompletedJenkinsRun(ctx, delivery, input)
	if err != nil {
		t.Fatalf("RecordCompletedJenkinsRun() duplicate error = %v", err)
	}
	if !duplicate.Duplicate || duplicate.CIRun.ID != first.CIRun.ID {
		t.Fatalf("duplicate CI Run receipt = %#v, first = %#v", duplicate, first)
	}
	assertCIRunCounts(t, ctx, pool, 1, 1, 4, 1)

	changedPayload := delivery
	changedPayload.PayloadSHA256 = jenkinsDigestB
	_, err = service.RecordCompletedJenkinsRun(ctx, changedPayload, input)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("changed digest replay error = %v, want conflict", err)
	}
	assertCIRunCounts(t, ctx, pool, 1, 1, 4, 1)

	differentDelivery := delivery
	differentDelivery.DeliveryID = "delivery-43"
	_, err = service.RecordCompletedJenkinsRun(ctx, differentDelivery, input)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate external run error = %v, want conflict", err)
	}
	assertDeliveryAbsent(t, ctx, pool, differentDelivery.DeliveryID)
	assertCIRunCounts(t, ctx, pool, 1, 1, 4, 1)
	assertConcurrentDuplicateDelivery(t, ctx, pool, store, delivery, input)
	assertCIRunCounts(t, ctx, pool, 2, 2, 5, 2)

	existingEventID := loadDomainEventID(t, ctx, pool, "ci-run.recorded", first.CIRun.ID)
	atomicFailureService := goldenpath.NewService(store, &fixedIDs{values: []string{
		"cir_atomic_failure",
		existingEventID,
		"cor_atomic_failure",
	}}, fixedClock{now: time.Date(2026, 8, 29, 12, 5, 0, 0, time.UTC)})
	atomicFailureDelivery := delivery
	atomicFailureDelivery.DeliveryID = "delivery-atomic-failure"
	atomicFailureInput := input
	atomicFailureInput.ExternalRunKey = "auth-service/main/43"
	_, err = atomicFailureService.RecordCompletedJenkinsRun(ctx, atomicFailureDelivery, atomicFailureInput)
	if !errors.Is(err, authz.ErrConflict) {
		t.Fatalf("duplicate event atomic failure error = %v, want conflict", err)
	}
	assertAbsent(t, ctx, pool, "radishnexus.ci_runs", "cir_atomic_failure")
	assertDeliveryAbsent(t, ctx, pool, atomicFailureDelivery.DeliveryID)
	assertCIRunCounts(t, ctx, pool, 2, 2, 5, 2)

	projected, err := store.RebuildActivityProjection(ctx)
	if err != nil {
		t.Fatalf("RebuildActivityProjection() with CI Run error = %v", err)
	}
	if projected != 5 {
		t.Fatalf("RebuildActivityProjection() projected = %d, want 5", projected)
	}
	assertTableCount(t, ctx, pool, "radishnexus.activity_items", 5)
	assertCIRunActivityFacts(t, ctx, pool, first.CIRun.ID)
	assertNoDeploymentEvent(t, ctx, pool)
	assertInboundReceiptImmutable(t, ctx, pool, delivery.DeliveryID)
}

func assertConcurrentDuplicateDelivery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *goldenpostgres.Store,
	delivery goldenpath.VerifiedJenkinsDelivery,
	input goldenpath.RecordCompletedCIRunInput,
) {
	t.Helper()
	delivery.DeliveryID = "delivery-concurrent"
	input.ExternalRunKey = "auth-service/main/44"

	type outcome struct {
		receipt goldenpath.CIRunReceipt
		err     error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	services := []*goldenpath.Service{
		goldenpath.NewService(store, &fixedIDs{values: []string{
			"cir_concurrent_a", "evt_concurrent_a", "cor_concurrent_a",
		}}, fixedClock{now: time.Date(2026, 8, 29, 12, 1, 0, 0, time.UTC)}),
		goldenpath.NewService(store, &fixedIDs{values: []string{
			"cir_concurrent_b", "evt_concurrent_b", "cor_concurrent_b",
		}}, fixedClock{now: time.Date(2026, 8, 29, 12, 1, 0, 0, time.UTC)}),
	}
	for _, service := range services {
		go func(service *goldenpath.Service) {
			<-start
			receipt, err := service.RecordCompletedJenkinsRun(ctx, delivery, input)
			results <- outcome{receipt: receipt, err: err}
		}(service)
	}
	close(start)
	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent duplicate errors = (%v, %v)", first.err, second.err)
	}
	if first.receipt.CIRun.ID != second.receipt.CIRun.ID ||
		first.receipt.Duplicate == second.receipt.Duplicate {
		t.Fatalf("concurrent duplicate receipts = (%#v, %#v)", first.receipt, second.receipt)
	}

	var receipts int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM radishnexus.inbound_deliveries
		WHERE delivery_id = $1
	`, delivery.DeliveryID).Scan(&receipts); err != nil {
		t.Fatalf("count concurrent delivery receipts error = %v", err)
	}
	if receipts != 1 {
		t.Fatalf("concurrent delivery receipt count = %d, want 1", receipts)
	}
}

func assertCIRunCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	ciRuns int,
	deliveries int,
	events int,
	outbox int,
) {
	t.Helper()
	for table, want := range map[string]int{
		"radishnexus.ci_runs":            ciRuns,
		"radishnexus.inbound_deliveries": deliveries,
		"radishnexus.domain_events":      events,
		"radishnexus.outbox_deliveries":  outbox,
	} {
		assertTableCount(t, ctx, pool, table, want)
	}
}

func assertDeliveryAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, deliveryID string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM radishnexus.inbound_deliveries WHERE delivery_id = $1
		)
	`, deliveryID).Scan(&exists); err != nil {
		t.Fatalf("check delivery receipt absence error = %v", err)
	}
	if exists {
		t.Fatalf("delivery receipt %s exists after rolled back transaction", deliveryID)
	}
}

func assertCIRunActivityFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ciRunID string) {
	t.Helper()
	var subjects []byte
	var safeFacts []byte
	if err := pool.QueryRow(ctx, `
		SELECT subject_refs, safe_facts
		FROM radishnexus.activity_items
		WHERE target_type = 'ci-run' AND target_id = $1
	`, ciRunID).Scan(&subjects, &safeFacts); err != nil {
		t.Fatalf("load CI Run Activity facts error = %v", err)
	}
	var decodedSubjects []map[string]string
	if err := json.Unmarshal(subjects, &decodedSubjects); err != nil {
		t.Fatalf("decode CI Run Activity subjects error = %v", err)
	}
	var decodedFacts map[string]string
	if err := json.Unmarshal(safeFacts, &decodedFacts); err != nil {
		t.Fatalf("decode CI Run Activity safe facts error = %v", err)
	}
	if len(decodedSubjects) != 1 || decodedSubjects[0]["type"] != "component" ||
		decodedSubjects[0]["id"] != "cmp_auth" || len(decodedFacts) != 1 ||
		decodedFacts["status"] != "succeeded" {
		t.Fatalf("CI Run Activity subjects = %#v, safe facts = %#v", decodedSubjects, decodedFacts)
	}
}

func assertNoDeploymentEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM radishnexus.domain_events WHERE event_type LIKE 'deployment.%'
	`).Scan(&count); err != nil {
		t.Fatalf("count Deployment events error = %v", err)
	}
	if count != 0 {
		t.Fatalf("Deployment event count = %d, want 0", count)
	}
}

func assertInboundReceiptImmutable(t *testing.T, ctx context.Context, pool *pgxpool.Pool, deliveryID string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		UPDATE radishnexus.inbound_deliveries
		SET payload_sha256 = $2
		WHERE delivery_id = $1
	`, deliveryID, jenkinsDigestB)
	assertPGCode(t, err, "23514", "immutable inbound delivery receipt")
}
