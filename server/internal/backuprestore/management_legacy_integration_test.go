//go:build integration

package backuprestore

import (
	"context"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupRestoreMembershipLegacy(t *testing.T) {
	legacy := os.Getenv("RADISHNEXUS_MEMBERSHIP_LEGACY_BIN")
	sourceURL, targetURL := os.Getenv("SOURCE_DATABASE_URL"), os.Getenv("TARGET_DATABASE_URL")
	if legacy == "" || sourceURL == "" || targetURL == "" {
		t.Skip("explicit local schema 019 artifact required")
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
	if e := source.QueryRow(ctx, `SELECT count(*) FROM public.radishnexus_schema_migrations`).Scan(&count); e != nil || count != 19 {
		t.Fatal("require actual 019 artifact", count, e)
	}
	run(sourceURL, "nexus-bootstrap", `{"email":"legacy-membership@example.test","password":"legacy membership synthetic password"}`, "--display-name", "Legacy membership owner", "--workspace-name", "Legacy workspace", "--credentials-stdin")
	snapshot := filepath.Join(t.TempDir(), "schema019")
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
		t.Fatal("new readiness accepted schema 019")
	}
	if e = db.Migrate(ctx, target); e != nil {
		t.Fatal(e)
	}
	if e = ready.CheckReady(ctx); e != nil {
		t.Fatal(e)
	}
	pool := connectIntegrationPool(t, ctx, targetURL)
	defer pool.Close()
	auth := authn.NewService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	session, e := auth.Login(ctx, authn.LoginInput{Email: "legacy-membership@example.test", Password: "legacy membership synthetic password"})
	if e != nil {
		t.Fatal(e)
	}
	verified, e := auth.ResolveWorkspace(ctx, session.Token, session.Account.Workspaces[0].ID)
	if e != nil || verified.MembershipRevision != 1 || session.Account.Workspaces[0].Role != "owner" {
		t.Fatal("upgraded restored admission", e)
	}
	var oldSessions, oldInvitations int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM radishnexus.user_sessions WHERE id<>$1),(SELECT count(*) FROM radishnexus.identity_invitations)`, verified.SessionID).Scan(&oldSessions, &oldInvitations); e != nil || oldSessions != 0 || oldInvitations != 0 {
		t.Fatal("restore retained authorization material", oldSessions, oldInvitations, e)
	}
	if e = source.QueryRow(ctx, `SELECT count(*) FROM public.radishnexus_schema_migrations`).Scan(&count); e != nil || count != 19 {
		t.Fatal("restore changed source", count, e)
	}
}
