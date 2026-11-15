//go:build integration

package backuprestore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestBackupRestoreMembershipLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_MEMBERSHIP_LEGACY_BIN", 19)
}
func TestBackupRestoreAssignmentLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_ASSIGNMENT_LEGACY_BIN", 20)
}

func TestBackupRestoreSupersessionLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_SUPERSESSION_LEGACY_BIN", 23)
}

func TestBackupRestoreReviewLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_REVIEW_LEGACY_BIN", 24)
}

func TestBackupRestoreDecisionLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_DECISION_LEGACY_BIN", 22)
}

func TestBackupRestorePriorityLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_PRIORITY_LEGACY_BIN", 21)
}

func TestBackupRestoreExecutionLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_EXECUTION_LEGACY_BIN", 26)
}

func TestBackupRestoreClassificationLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_CLASSIFICATION_LEGACY_BIN", 27)
}

func TestBackupRestoreCIRunTraceLegacy(t *testing.T) {
	testBackupRestoreLegacy(t, "RADISHNEXUS_CI_RUN_TRACE_LEGACY_BIN", 28)
}

func testBackupRestoreLegacy(t *testing.T, variable string, schema int) {
	testBackupRestoreLegacyWithFixture(t, variable, schema, nil)
}

type legacyUpgradeFixture func(context.Context, *pgxpool.Pool, authz.Principal) func(*pgxpool.Pool, authz.Principal)

func testBackupRestoreLegacyWithFixture(t *testing.T, variable string, schema int, seed legacyUpgradeFixture) {
	legacy := os.Getenv(variable)
	sourceURL, targetURL := os.Getenv("SOURCE_DATABASE_URL"), os.Getenv("TARGET_DATABASE_URL")
	if legacy == "" || sourceURL == "" || targetURL == "" {
		t.Skipf("explicit local schema %03d artifact required", schema)
	}
	ctx := context.Background()
	sourceURL = recoveryBackupDatabase(t, ctx, sourceURL)
	targetURL = recoveryBackupDatabase(t, ctx, targetURL)
	run := func(database, command, stdin string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(legacy, command), args...)
		cmd.Env = append(os.Environ(), "DATABASE_URL="+database)
		cmd.Stdin = strings.NewReader(stdin)
		if output, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("legacy %s: %v (%s)", command, e, output)
		}
	}
	run(sourceURL, "nexus-migrate", "")
	source := connectIntegrationDatabase(t, ctx, sourceURL)
	defer source.Close(ctx)
	target := connectIntegrationDatabase(t, ctx, targetURL)
	defer target.Close(ctx)
	var count int
	if e := source.QueryRow(ctx, `SELECT count(*) FROM public.radishnexus_schema_migrations`).Scan(&count); e != nil || count != schema {
		t.Fatal("require actual matching legacy artifact", count, schema, e)
	}
	run(sourceURL, "nexus-bootstrap", `{"email":"legacy-membership@example.test","password":"legacy membership synthetic password"}`, "--display-name", "Legacy membership owner", "--workspace-name", "Legacy workspace", "--credentials-stdin")
	var verifyFixture func(*pgxpool.Pool, authz.Principal)
	if seed != nil {
		pool := connectIntegrationPool(t, ctx, sourceURL)
		defer pool.Close()
		verifyFixture = seed(ctx, pool, loginLegacyOwner(t, ctx, pool))
	}
	snapshot := filepath.Join(t.TempDir(), fmt.Sprintf("schema%03d", schema))
	run(sourceURL, "nexus-backup", "", "--output", snapshot)
	if _, e := NewService().Restore(ctx, target, targetURL, snapshot); e == nil {
		t.Fatal("new restore accepted mismatched schema")
	}
	run(targetURL, "nexus-restore", "", "--input", snapshot)
	ready, e := db.NewReadinessChecker(target)
	if e != nil {
		t.Fatal(e)
	}
	if e = ready.CheckReady(ctx); e == nil {
		t.Fatal("new readiness accepted old schema")
	}
	if e = db.Migrate(ctx, target); e != nil {
		t.Fatal(e)
	}
	if e = ready.CheckReady(ctx); e != nil {
		t.Fatal(e)
	}
	pool := connectIntegrationPool(t, ctx, targetURL)
	defer pool.Close()
	owner := loginLegacyOwner(t, ctx, pool)
	if verifyFixture != nil {
		verifyFixture(pool, owner)
	}
	var oldSessions, oldInvitations int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM radishnexus.user_sessions WHERE id<>$1),(SELECT count(*) FROM radishnexus.identity_invitations)`, owner.SessionID).Scan(&oldSessions, &oldInvitations); e != nil || oldSessions != 0 || oldInvitations != 0 {
		t.Fatal("restore retained authorization material", oldSessions, oldInvitations, e)
	}
	if e = source.QueryRow(ctx, `SELECT count(*) FROM public.radishnexus_schema_migrations`).Scan(&count); e != nil || count != schema {
		t.Fatal("restore changed source", count, e)
	}
}

func loginLegacyOwner(t *testing.T, ctx context.Context, pool *pgxpool.Pool) authz.Principal {
	t.Helper()
	auth := authn.NewService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	session, e := auth.Login(ctx, authn.LoginInput{Email: "legacy-membership@example.test", Password: "legacy membership synthetic password"})
	if e != nil {
		t.Fatal(e)
	}
	verified, e := auth.ResolveWorkspace(ctx, session.Token, session.Account.Workspaces[0].ID)
	if e != nil || verified.MembershipRevision != 1 || session.Account.Workspaces[0].Role != "owner" {
		t.Fatal("upgraded restored admission", e)
	}
	p, e := authn.UserPrincipal(verified)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
