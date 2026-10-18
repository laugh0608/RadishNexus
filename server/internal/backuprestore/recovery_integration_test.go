//go:build integration

package backuprestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
)

func recoveryBackupDatabase(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	admin := connectIntegrationDatabase(t, ctx, dsn)
	name := fmt.Sprintf("nexus_recovery_backup_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close(ctx)
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}
func TestBackupRestoreAccountRecovery(t *testing.T) {
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
	if err := db.Migrate(ctx, source); err != nil {
		t.Fatal(err)
	}
	pool := connectIntegrationPool(t, ctx, sourceURL)
	defer pool.Close()
	store := authpostgres.New(pool)
	auth := authn.NewService(store, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	oldPassword, newPassword := "backup old recovery password", "backup new recovery password"
	email := "restore-owner@example.test"
	owner, err := auth.Bootstrap(ctx, authn.BootstrapInput{Email: email, Password: oldPassword, DisplayName: "Restore Owner", WorkspaceName: "Restore Workspace"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login(ctx, authn.LoginInput{Email: email, Password: oldPassword})
	if err != nil {
		t.Fatal(err)
	}
	identity := authn.NewIdentityService(store, auth, "")
	if _, err = identity.CreateInvitation(ctx, session.Token, owner.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err = store.BeginOIDC(ctx, authn.OIDCTransaction{StateDigest: bytesOf(1), BrowserDigest: bytesOf(2), NonceDigest: bytesOf(3), ProviderDigest: bytesOf(4), InitiatingSessionDigest: sessionDigest(session.Token), CreatedAt: now, ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	recovery := authn.NewRecoveryService(store, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{})
	inspection, err := recovery.Inspect(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	input := authn.RecoveryInput{UserID: owner.UserID, ExpectedEmail: email, ExpectedPasswordChangedAt: inspection.PasswordChangedAt, OperationID: "12345678-1234-4234-8234-123456789abc", OperatorRef: "ops-backup", ReasonCode: "lost_local_password", NewPassword: newPassword, Confirmed: true}
	result, err := recovery.Apply(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	baseline := snapshotIncludedTables(t, ctx, pool)
	path := filepath.Join(t.TempDir(), "recovered-account")
	service := NewService()
	if _, err = service.Backup(ctx, source, sourceURL, path); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Restore(ctx, target, targetURL, path); err != nil {
		t.Fatal(err)
	}
	targetPool := connectIntegrationPool(t, ctx, targetURL)
	defer targetPool.Close()
	if !reflect.DeepEqual(baseline, snapshotIncludedTables(t, ctx, targetPool)) {
		t.Fatal("authoritative backup drift")
	}
	for _, table := range []string{"user_sessions", "identity_invitations", "oidc_transactions"} {
		if snapshotTable(t, ctx, targetPool, "radishnexus."+table) != "[]" {
			t.Fatal("ephemeral data restored", table)
		}
	}
	restoredStore := authpostgres.New(targetPool)
	restored := authn.NewService(restoredStore, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	restoredRecovery := authn.NewRecoveryService(restoredStore, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{})
	status, err := restoredRecovery.Status(ctx, owner.UserID, input.OperationID)
	if err != nil || status != result {
		t.Fatal("recovery audit missing", err)
	}
	if snapshotTable(t, ctx, targetPool, "radishnexus.user_sessions") != "[]" {
		t.Fatal("status created session")
	}
	if _, err = restored.Login(ctx, authn.LoginInput{Email: email, Password: oldPassword}); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatal("old password restored", err)
	}
	fresh, err := restored.Login(ctx, authn.LoginInput{Email: email, Password: newPassword})
	if err != nil || fresh.Account.User.ID != owner.UserID || fresh.Account.Workspaces[0].Role != "owner" {
		t.Fatal("new password login", err)
	}
	if _, err = restored.ResolveSession(ctx, session.Token); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatal("old session restored")
	}
	setup, err := authn.NewSetupService(restored, restoredStore, "")
	if err != nil {
		t.Fatal(err)
	}
	if state, err := setup.Status(ctx); err != nil || state != "complete" {
		t.Fatal("setup reopened")
	}
	if _, err = targetPool.Exec(ctx, `DELETE FROM radishnexus.identity_audit WHERE operation_id=$1`, input.OperationID); err == nil {
		t.Fatal("restore lost audit immutability")
	}
}
func bytesOf(value byte) []byte         { return bytes.Repeat([]byte{value}, 32) }
func sessionDigest(token string) []byte { value := sha256.Sum256([]byte(token)); return value[:] }
