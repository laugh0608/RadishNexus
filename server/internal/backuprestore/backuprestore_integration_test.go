//go:build integration

package backuprestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestBackupRestoreGoldenPath(t *testing.T) {
	sourceURL := os.Getenv("SOURCE_DATABASE_URL")
	targetURL := os.Getenv("TARGET_DATABASE_URL")
	if sourceURL == "" || targetURL == "" {
		t.Skip("SOURCE_DATABASE_URL and TARGET_DATABASE_URL are required")
	}
	ctx := context.Background()
	sourceConnection := connectIntegrationDatabase(t, ctx, sourceURL)
	defer sourceConnection.Close(ctx)
	targetConnection := connectIntegrationDatabase(t, ctx, targetURL)
	defer targetConnection.Close(ctx)
	if err := db.Migrate(ctx, sourceConnection); err != nil {
		t.Fatalf("migrate source database: %v", err)
	}

	sourcePool := connectIntegrationPool(t, ctx, sourceURL)
	defer sourcePool.Close()
	seedBackupGoldenPath(t, ctx, sourcePool)
	sourceStore := goldenpostgres.New(sourcePool)
	projected, err := sourceStore.RebuildActivityProjection(ctx)
	if err != nil {
		t.Fatalf("rebuild source Activity projection: %v", err)
	}
	if projected != 5 {
		t.Fatalf("source Activity rows = %d, want 5", projected)
	}
	sourceSnapshot := snapshotIncludedTables(t, ctx, sourcePool)
	sourceActivity := snapshotTable(t, ctx, sourcePool, "radishnexus.activity_items")

	artifactRoot := t.TempDir()
	backupPath := filepath.Join(artifactRoot, "golden-path-backup")
	service := NewService()
	manifest, err := service.Backup(ctx, sourceConnection, sourceURL, backupPath)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if !reflect.DeepEqual(manifest.ExcludedDataTables, []string{"radishnexus.activity_items"}) {
		t.Fatalf("backup exclusions = %#v", manifest.ExcludedDataTables)
	}
	if _, err := sourceConnection.Exec(ctx, `CREATE TABLE public.unclassified_backup_data (value text)`); err != nil {
		t.Fatalf("create unclassified source table: %v", err)
	}
	unclassifiedOutput := filepath.Join(artifactRoot, "unclassified-output")
	if _, err := service.Backup(ctx, sourceConnection, sourceURL, unclassifiedOutput); err == nil ||
		!strings.Contains(err.Error(), "not fully classified") {
		t.Fatalf("Backup() unclassified relation error = %v", err)
	}
	if _, err := os.Lstat(unclassifiedOutput); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected backup output exists or cannot be inspected: %v", err)
	}
	if _, err := sourceConnection.Exec(ctx, `DROP TABLE public.unclassified_backup_data`); err != nil {
		t.Fatalf("drop unclassified source table fixture: %v", err)
	}

	checksumDriftPath := copyBackupDirectory(t, backupPath, filepath.Join(artifactRoot, "checksum-drift"))
	checksumDriftManifest := readIntegrationManifest(t, checksumDriftPath)
	checksumDriftManifest.Migrations[0].Checksum = strings.Repeat("0", 64)
	writeIntegrationManifest(t, checksumDriftPath, checksumDriftManifest)
	if _, err := service.Restore(ctx, targetConnection, targetURL, checksumDriftPath); err == nil ||
		!strings.Contains(err.Error(), "migration history") {
		t.Fatalf("Restore() migration drift error = %v", err)
	}
	assertIntegrationTargetEmpty(t, ctx, targetConnection)

	corruptPath := copyBackupDirectory(t, backupPath, filepath.Join(artifactRoot, "corrupt-dump"))
	corruptDump := filepath.Join(corruptPath, dumpFilename)
	file, err := os.OpenFile(corruptDump, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open corrupt dump fixture: %v", err)
	}
	if _, err := file.WriteString("corrupt"); err != nil {
		file.Close()
		t.Fatalf("append corrupt dump fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close corrupt dump fixture: %v", err)
	}
	if _, err := service.Restore(ctx, targetConnection, targetURL, corruptPath); err == nil ||
		!strings.Contains(err.Error(), "checksum or size") {
		t.Fatalf("Restore() corrupt dump error = %v", err)
	}
	assertIntegrationTargetEmpty(t, ctx, targetConnection)

	if _, err := service.Restore(ctx, targetConnection, targetURL, backupPath); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	targetPool := connectIntegrationPool(t, ctx, targetURL)
	defer targetPool.Close()
	if got := snapshotTable(t, ctx, targetPool, "radishnexus.activity_items"); got != "[]" {
		t.Fatalf("restored Activity before rebuild = %s, want []", got)
	}
	targetSnapshot := snapshotIncludedTables(t, ctx, targetPool)
	if !reflect.DeepEqual(targetSnapshot, sourceSnapshot) {
		t.Fatalf("restored authoritative data differs\nsource: %#v\ntarget: %#v", sourceSnapshot, targetSnapshot)
	}
	targetStore := goldenpostgres.New(targetPool)
	projected, err = targetStore.RebuildActivityProjection(ctx)
	if err != nil {
		t.Fatalf("rebuild target Activity projection: %v", err)
	}
	if projected != 5 {
		t.Fatalf("target Activity rows = %d, want 5", projected)
	}
	if targetActivity := snapshotTable(t, ctx, targetPool, "radishnexus.activity_items"); targetActivity != sourceActivity {
		t.Fatalf("rebuilt Activity differs\nsource: %s\ntarget: %s", sourceActivity, targetActivity)
	}

	beforeConflict := snapshotIncludedTables(t, ctx, targetPool)
	if _, err := service.Restore(ctx, targetConnection, targetURL, backupPath); err == nil ||
		!strings.Contains(err.Error(), "not empty") {
		t.Fatalf("Restore() non-empty target error = %v", err)
	}
	afterConflict := snapshotIncludedTables(t, ctx, targetPool)
	if !reflect.DeepEqual(afterConflict, beforeConflict) {
		t.Fatalf("non-empty target changed after rejected restore")
	}
}

type deterministicIDs struct {
	next map[string]int
}

func (ids *deterministicIDs) NewID(prefix string) (string, error) {
	if ids.next == nil {
		ids.next = make(map[string]int)
	}
	ids.next[prefix]++
	return fmt.Sprintf("%sbackup_%02d", prefix, ids.next[prefix]), nil
}

type integrationClock struct {
	now time.Time
}

func (clock integrationClock) Now() time.Time { return clock.now }

func seedBackupGoldenPath(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO radishnexus.users (id, display_name, created_at) VALUES
			('usr_admin', 'Admin', '2026-08-30T01:00:00Z'),
			('usr_contributor', 'Contributor', '2026-08-30T01:00:00Z'),
			('usr_decider', 'Decider', '2026-08-30T01:00:00Z');
		INSERT INTO radishnexus.workspaces (id, name, created_at)
		VALUES ('wrk_backup', 'Backup Workspace', '2026-08-30T01:00:00Z');
		INSERT INTO radishnexus.workspace_memberships (
			workspace_id, user_id, status, created_at
		) VALUES
			('wrk_backup', 'usr_admin', 'active', '2026-08-30T01:00:00Z'),
			('wrk_backup', 'usr_contributor', 'active', '2026-08-30T01:00:00Z'),
			('wrk_backup', 'usr_decider', 'active', '2026-08-30T01:00:00Z');
		INSERT INTO radishnexus.teams (id, workspace_id, name, created_at)
		VALUES ('tem_backup', 'wrk_backup', 'Backup Team', '2026-08-30T01:00:00Z');
		INSERT INTO radishnexus.projects (
			id, workspace_id, key, name, owner_team_id, visibility, status,
			created_by_kind, created_by_id, created_at, updated_at
		) VALUES (
			'prj_backup', 'wrk_backup', 'BACKUP', 'Backup Project', 'tem_backup',
			'workspace', 'active', 'user', 'usr_admin',
			'2026-08-30T01:00:00Z', '2026-08-30T01:00:00Z'
		);
		INSERT INTO radishnexus.project_memberships (
			workspace_id, project_id, user_id, role, created_at
		) VALUES
			('wrk_backup', 'prj_backup', 'usr_contributor', 'contributor', '2026-08-30T01:00:00Z'),
			('wrk_backup', 'prj_backup', 'usr_decider', 'decider', '2026-08-30T01:00:00Z');
		INSERT INTO radishnexus.threads (
			id, workspace_id, governing_project_id, title, visibility, created_by,
			created_at, updated_at
		) VALUES (
			'thr_backup', 'wrk_backup', 'prj_backup', 'Backup contract discussion',
			'restricted', 'usr_contributor',
			'2026-08-30T01:00:00Z', '2026-08-30T01:00:00Z'
		);
		INSERT INTO radishnexus.thread_memberships (
			workspace_id, thread_id, user_id, created_at
		) VALUES
			('wrk_backup', 'thr_backup', 'usr_contributor', '2026-08-30T01:00:00Z'),
			('wrk_backup', 'thr_backup', 'usr_decider', '2026-08-30T01:00:00Z');
		INSERT INTO radishnexus.components (
			id, workspace_id, key, name, type, owner_team_id, lifecycle,
			created_by_kind, created_by_id, created_at, updated_at
		) VALUES (
			'cmp_backup', 'wrk_backup', 'AUTH', 'Authentication Service', 'service',
			'tem_backup', 'active', 'user', 'usr_contributor',
			'2026-08-30T01:00:00Z', '2026-08-30T01:00:00Z'
		);
		INSERT INTO radishnexus.environments (
			id, workspace_id, key, name, classification, owner_team_id, status,
			created_by_kind, created_by_id, created_at, updated_at
		) VALUES (
			'env_backup_staging', 'wrk_backup', 'STAGING', 'Staging', 'staging',
			'tem_backup', 'active', 'user', 'usr_admin',
			'2026-08-30T01:00:00Z', '2026-08-30T01:00:00Z'
		);
		INSERT INTO radishnexus.environment_deployment_authorizations (
			id, workspace_id, environment_id, user_id, status, granted_by, granted_at
		) VALUES (
			'dpa_backup', 'wrk_backup', 'env_backup_staging', 'usr_contributor',
			'active', 'usr_admin', '2026-08-30T01:00:00Z'
		);
	`)
	if err != nil {
		t.Fatalf("seed backup base data: %v", err)
	}

	clock := integrationClock{now: time.Date(2026, 8, 30, 1, 30, 0, 0, time.UTC)}
	service := goldenpath.NewService(goldenpostgres.New(pool), &deterministicIDs{}, clock)
	contributor := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_contributor", WorkspaceID: "wrk_backup"}
	decider := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_decider", WorkspaceID: "wrk_backup"}
	invocation := func(principal authz.Principal, correlationID string) goldenpath.Invocation {
		return goldenpath.Invocation{
			Principal:     principal,
			SourceKind:    "api",
			CorrelationID: correlationID,
		}
	}
	decision, err := service.CreateDecisionFromThread(
		ctx,
		invocation(contributor, "cor_backup_decision"),
		goldenpath.CreateDecisionInput{
			ThreadID: "thr_backup",
			Question: "How should the backup contract preserve context?",
		},
	)
	if err != nil {
		t.Fatalf("create backup Decision: %v", err)
	}
	decision, err = service.AcceptDecision(
		ctx,
		invocation(decider, "cor_backup_accept"),
		goldenpath.AcceptDecisionInput{
			DecisionID: decision.ID,
			Outcome:    "Preserve authoritative facts and rebuild Activity.",
			Rationale:  "Derived projections must not become a second source of truth.",
		},
	)
	if err != nil {
		t.Fatalf("accept backup Decision: %v", err)
	}
	if _, err := service.CreateTicketFromDecision(
		ctx,
		invocation(contributor, "cor_backup_ticket"),
		goldenpath.CreateTicketInput{
			DecisionID: decision.ID,
			Title:      "Implement verified backup restore",
		},
	); err != nil {
		t.Fatalf("create backup Ticket: %v", err)
	}
	startedAt := time.Date(2026, 8, 30, 1, 20, 0, 0, time.UTC)
	ciReceipt, err := service.RecordCompletedJenkinsRun(
		ctx,
		goldenpath.VerifiedJenkinsDelivery{
			WorkspaceID:   "wrk_backup",
			SourceID:      "jenkins-backup",
			DeliveryID:    "delivery-backup-42",
			PayloadSHA256: strings.Repeat("a", 64),
		},
		goldenpath.RecordCompletedCIRunInput{
			ComponentID:    "cmp_backup",
			ExternalRunKey: "auth/main/42",
			Status:         "succeeded",
			StartedAt:      &startedAt,
			CompletedAt:    time.Date(2026, 8, 30, 1, 25, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("record backup CI Run: %v", err)
	}
	if _, err := service.RecordStagingDeployment(
		ctx,
		invocation(contributor, "cor_backup_deployment"),
		goldenpath.RecordStagingDeploymentInput{
			EnvironmentID: "env_backup_staging",
			CIRunID:       ciReceipt.CIRun.ID,
			Status:        "succeeded",
			StartedAt:     &startedAt,
			CompletedAt:   time.Date(2026, 8, 30, 1, 29, 0, 0, time.UTC),
		},
	); err != nil {
		t.Fatalf("record backup Deployment: %v", err)
	}
}

func connectIntegrationDatabase(t *testing.T, ctx context.Context, databaseURL string) *pgx.Conn {
	t.Helper()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect integration database: %v", err)
	}
	return connection
}

func connectIntegrationPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect integration database pool: %v", err)
	}
	return pool
}

func snapshotIncludedTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	snapshot := make(map[string]string, len(includedDataTables))
	for _, table := range includedDataTables {
		snapshot[table] = snapshotTable(t, ctx, pool, table)
	}
	return snapshot
}

func snapshotTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) string {
	t.Helper()
	allowed := append(append([]string{}, includedDataTables...), excludedDataTables...)
	if !slicesContains(allowed, table) {
		t.Fatalf("snapshot table %q is not part of the backup contract", table)
	}
	query := fmt.Sprintf(`
		SELECT COALESCE(
			jsonb_agg(to_jsonb(snapshot_row) ORDER BY to_jsonb(snapshot_row)::text),
			'[]'::jsonb
		)::text
		FROM %s AS snapshot_row
	`, table)
	var snapshot string
	if err := pool.QueryRow(ctx, query).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot %s: %v", table, err)
	}
	return snapshot
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func copyBackupDirectory(t *testing.T, source string, destination string) string {
	t.Helper()
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatalf("create backup copy directory: %v", err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("read backup directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unexpected directory in backup artifact: %s", entry.Name())
		}
		body, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatalf("read backup artifact file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(destination, entry.Name()), body, 0o600); err != nil {
			t.Fatalf("write backup artifact copy: %v", err)
		}
	}
	return destination
}

func readIntegrationManifest(t *testing.T, directory string) Manifest {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(directory, manifestFilename))
	if err != nil {
		t.Fatalf("read integration manifest: %v", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("decode integration manifest: %v", err)
	}
	return manifest
}

func writeIntegrationManifest(t *testing.T, directory string, manifest Manifest) {
	t.Helper()
	if err := writeManifest(filepath.Join(directory, manifestFilename), manifest); err != nil {
		t.Fatalf("write integration manifest: %v", err)
	}
}

func assertIntegrationTargetEmpty(t *testing.T, ctx context.Context, connection *pgx.Conn) {
	t.Helper()
	if err := requireEmptyTarget(ctx, connection); err != nil {
		t.Fatalf("target changed after rejected restore: %v", err)
	}
}
