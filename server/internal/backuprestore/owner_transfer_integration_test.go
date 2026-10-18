//go:build integration

package backuprestore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestBackupRestoreOwnerTransfer(t *testing.T) {
	sourceURL, targetURL := os.Getenv("SOURCE_DATABASE_URL"), os.Getenv("TARGET_DATABASE_URL")
	if sourceURL == "" || targetURL == "" {
		t.Skip("source and target required")
	}
	ctx := context.Background()
	sourceURL = recoveryBackupDatabase(t, ctx, sourceURL)
	targetURL = recoveryBackupDatabase(t, ctx, targetURL)
	source := connectIntegrationDatabase(t, ctx, sourceURL)
	defer source.Close(ctx)
	target := connectIntegrationDatabase(t, ctx, targetURL)
	defer target.Close(ctx)
	if e := db.Migrate(ctx, source); e != nil {
		t.Fatal(e)
	}
	pool := connectIntegrationPool(t, ctx, sourceURL)
	defer pool.Close()
	store := authpostgres.New(pool)
	auth := authn.NewService(store, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	password := "owner transfer backup fixture password"
	owner, e := auth.Bootstrap(ctx, authn.BootstrapInput{Email: "owner@example.test", Password: password, DisplayName: "Owner", WorkspaceName: "Handoff"})
	if e != nil {
		t.Fatal(e)
	}
	a, e := auth.Login(ctx, authn.LoginInput{Email: "owner@example.test", Password: password})
	if e != nil {
		t.Fatal(e)
	}
	identity := authn.NewIdentityService(store, auth, "")
	invite, e := identity.CreateInvitation(ctx, a.Token, owner.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	b, e := identity.AcceptInvitation(ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: "member@example.test", Password: password, DisplayName: "Member"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = identity.CreateInvitation(ctx, a.Token, owner.WorkspaceID); e != nil {
		t.Fatal(e)
	}
	transfer := goldenpath.NewOwnerTransferService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	principal := authz.Principal{Kind: authz.PrincipalUser, ID: owner.UserID, WorkspaceID: owner.WorkspaceID}
	input := goldenpath.OwnerTransferInput{ClientOperationID: "12345678-1234-4234-8234-123456789abc", TargetUserID: b.Account.User.ID, ExpectedActorRoleRevision: "1", ExpectedTargetRoleRevision: "1", Confirmed: true}
	result, e := transfer.Transfer(ctx, goldenpath.Invocation{Principal: principal}, a.Token, input)
	if e != nil {
		t.Fatal(e)
	}
	baseline := snapshotIncludedTables(t, ctx, pool)
	path := filepath.Join(t.TempDir(), "owner-transfer")
	service := NewService()
	if _, e = service.Backup(ctx, source, sourceURL, path); e != nil {
		t.Fatal(e)
	}
	if _, e = service.Restore(ctx, target, targetURL, path); e != nil {
		t.Fatal(e)
	}
	restoredPool := connectIntegrationPool(t, ctx, targetURL)
	defer restoredPool.Close()
	if !reflect.DeepEqual(baseline, snapshotIncludedTables(t, ctx, restoredPool)) {
		t.Fatal("authoritative role/revision/history backup drift")
	}
	for _, table := range []string{"user_sessions", "identity_invitations", "oidc_transactions"} {
		if snapshotTable(t, ctx, restoredPool, "radishnexus."+table) != "[]" {
			t.Fatal("ephemeral identity restored", table)
		}
	}
	restoredStore := authpostgres.New(restoredPool)
	restored := authn.NewService(restoredStore, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	if _, e = restored.ResolveSession(ctx, a.Token); !errors.Is(e, authn.ErrInvalidSession) {
		t.Fatal("original session restored", e)
	}
	fresh, e := restored.Login(ctx, authn.LoginInput{Email: "owner@example.test", Password: password})
	if e != nil || fresh.Account.Workspaces[0].Role != "member" {
		t.Fatal("old owner role returned", e)
	}
	newOwner, e := restored.Login(ctx, authn.LoginInput{Email: "member@example.test", Password: password})
	if e != nil || newOwner.Account.Workspaces[0].Role != "owner" {
		t.Fatal("new owner missing", e)
	}
	restoredTransfer := goldenpath.NewOwnerTransferService(goldenpostgres.New(restoredPool), goldenpath.CryptoIDGenerator{})
	status, e := restoredTransfer.Status(ctx, principal, fresh.Token, input.ClientOperationID)
	if e != nil || !reflect.DeepEqual(status, result) {
		t.Fatal("historical receipt missing", e, status, result)
	}
	retry, e := restoredTransfer.Transfer(ctx, goldenpath.Invocation{Principal: principal}, fresh.Token, input)
	if e != nil || retry.AuditID != result.AuditID {
		t.Fatal("restored history retry", e)
	}
	setup, e := authn.NewSetupService(restored, restoredStore, "")
	if e != nil {
		t.Fatal(e)
	}
	if status, e := setup.Status(ctx); e != nil || status != "complete" {
		t.Fatal("setup reopened", e)
	}
	for _, sql := range []string{`DELETE FROM radishnexus.workspace_configuration_audit`, `DELETE FROM radishnexus.workspace_configuration_receipts`, `UPDATE radishnexus.workspace_memberships SET role='owner' WHERE role='member'`} {
		if _, e = restoredPool.Exec(ctx, sql); e == nil {
			t.Fatal("restore lost protection")
		}
	}
}

// The optional runner builds a real local 017 commit, not a reconstructed dump
// or modified current manifest. New and old artifact boundaries are exercised.
func TestBackupRestoreOwnerTransferLegacy(t *testing.T) {
	legacy := os.Getenv("RADISHNEXUS_OWNER_TRANSFER_LEGACY_BIN")
	sourceURL, targetURL := os.Getenv("SOURCE_DATABASE_URL"), os.Getenv("TARGET_DATABASE_URL")
	if legacy == "" || sourceURL == "" || targetURL == "" {
		t.Skip("explicit local 017 artifact and isolated source/target required")
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
			t.Fatalf("old artifact %s failed: %v (%s)", command, e, string(output))
		}
	}
	run(sourceURL, "nexus-migrate", "")
	source := connectIntegrationDatabase(t, ctx, sourceURL)
	defer source.Close(ctx)
	target := connectIntegrationDatabase(t, ctx, targetURL)
	defer target.Close(ctx)
	var migrations int
	if e := source.QueryRow(ctx, `SELECT count(*) FROM public.radishnexus_schema_migrations`).Scan(&migrations); e != nil || migrations != 17 {
		t.Fatal("legacy baseline must be schema 017", e, migrations)
	}
	run(sourceURL, "nexus-bootstrap", `{"email":"legacy-owner@example.test","password":"legacy handoff fixture password"}`, "--display-name", "Legacy owner", "--workspace-name", "Legacy workspace", "--credentials-stdin")
	backup := filepath.Join(t.TempDir(), "schema017")
	run(sourceURL, "nexus-backup", "", "--output", backup)
	if e := db.Migrate(ctx, source); e != nil {
		t.Fatal(e)
	}
	pool := connectIntegrationPool(t, ctx, sourceURL)
	defer pool.Close()
	auth := authn.NewService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	login := authn.LoginInput{Email: "legacy-owner@example.test", Password: "legacy handoff fixture password"}
	a, e := auth.Login(ctx, login)
	if e != nil {
		t.Fatal(e)
	}
	workspace := a.Account.Workspaces[0].ID
	identity := authn.NewIdentityService(authpostgres.New(pool), auth, "")
	invite, e := identity.CreateInvitation(ctx, a.Token, workspace)
	if e != nil {
		t.Fatal(e)
	}
	b, e := identity.AcceptInvitation(ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: "legacy-member@example.test", Password: login.Password, DisplayName: "New owner"})
	if e != nil {
		t.Fatal(e)
	}
	principal := authz.Principal{Kind: authz.PrincipalUser, ID: a.Account.User.ID, WorkspaceID: workspace}
	input := goldenpath.OwnerTransferInput{ClientOperationID: "12345678-1234-4234-8234-123456789abc", TargetUserID: b.Account.User.ID, ExpectedActorRoleRevision: "1", ExpectedTargetRoleRevision: "1", Confirmed: true}
	service := goldenpath.NewOwnerTransferService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	if _, e = service.Transfer(ctx, goldenpath.Invocation{Principal: principal}, a.Token, input); e != nil {
		t.Fatal(e)
	}
	if _, e = NewService().Restore(ctx, target, targetURL, backup); e == nil {
		t.Fatal("018 restore accepted 017 backup")
	}
	run(targetURL, "nexus-restore", "", "--input", backup)
	ready, e := db.NewReadinessChecker(target)
	if e != nil {
		t.Fatal(e)
	}
	if e = ready.CheckReady(ctx); e == nil {
		t.Fatal("018 ready on old restore")
	}
	if e = db.Migrate(ctx, target); e != nil {
		t.Fatal(e)
	}
	if e = ready.CheckReady(ctx); e != nil {
		t.Fatal(e)
	}
	targetPool := connectIntegrationPool(t, ctx, targetURL)
	defer targetPool.Close()
	targetStore := authpostgres.New(targetPool)
	targetAuth := authn.NewService(targetStore, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	restored, e := targetAuth.Login(ctx, login)
	if e != nil || restored.Account.Workspaces[0].Role != "owner" {
		t.Fatal("017 snapshot did not restore historical owner", e)
	}
	targetTransfer := goldenpath.NewOwnerTransferService(goldenpostgres.New(targetPool), goldenpath.CryptoIDGenerator{})
	ownership, e := targetTransfer.Ownership(ctx, principal, restored.Token)
	if e != nil || ownership.RoleRevision != 1 {
		t.Fatal("old backup replayed new revision", e)
	}
	status, e := targetTransfer.Status(ctx, principal, restored.Token, input.ClientOperationID)
	if e != nil || status.Result != "not_observed" {
		t.Fatal("old backup invented later handoff", e)
	}
	setup, e := authn.NewSetupService(targetAuth, targetStore, "")
	if e != nil {
		t.Fatal(e)
	}
	if state, e := setup.Status(ctx); e != nil || state != "complete" {
		t.Fatal("old restored setup reopened", e)
	}
	var sourceRole string
	if e = pool.QueryRow(ctx, `SELECT role FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, workspace, principal.ID).Scan(&sourceRole); e != nil || sourceRole != "member" {
		t.Fatal("restore touched source", e)
	}
}
