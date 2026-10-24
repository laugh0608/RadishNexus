//go:build integration

package backuprestore

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBackupRestoreImportedContext(t *testing.T) {
	sourceURL, targetURL := os.Getenv("SOURCE_DATABASE_URL"), os.Getenv("TARGET_DATABASE_URL")
	if sourceURL == "" || targetURL == "" {
		t.Skip("source and target required")
	}
	ctx := context.Background()
	password := "context backup synthetic password"
	schema := func(ctx context.Context, tx pgx.Tx) error {
		checker, e := db.NewReadinessChecker(tx)
		if e != nil {
			return e
		}
		return checker.CheckReady(ctx)
	}
	initialize := func(base string) (string, *pgx.Conn, *pgxpool.Pool, authn.Session, authz.Principal, *goldenpath.ContextService) {
		uri := recoveryBackupDatabase(t, ctx, base)
		conn := connectIntegrationDatabase(t, ctx, uri)
		t.Cleanup(func() { conn.Close(ctx) })
		if e := db.Migrate(ctx, conn); e != nil {
			t.Fatal(e)
		}
		pool := connectIntegrationPool(t, ctx, uri)
		t.Cleanup(pool.Close)
		auth := authn.NewService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
		boot, e := auth.Bootstrap(ctx, authn.BootstrapInput{Email: "context-backup@example.test", Password: password, DisplayName: "Context backup", WorkspaceName: "Context backup"})
		if e != nil {
			t.Fatal(e)
		}
		session, e := auth.Login(ctx, authn.LoginInput{Email: "context-backup@example.test", Password: password})
		if e != nil {
			t.Fatal(e)
		}
		principal := authz.Principal{Kind: authz.PrincipalUser, ID: boot.UserID, WorkspaceID: boot.WorkspaceID}
		return uri, conn, pool, session, principal, goldenpath.NewContextService(goldenpostgres.NewContextStore(pool, schema))
	}
	_, _, original, session, principal, exporter := initialize(sourceURL)
	configuration := goldenpath.NewConfigurationService(goldenpostgres.New(original), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	invocation := goldenpath.Invocation{Principal: principal, SourceKind: "web", CorrelationID: "cor_context_backup"}
	team, e := configuration.Configure(ctx, invocation, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: principal.WorkspaceID, ClientOperationID: "context-backup-team", Name: "Context team"})
	if e != nil {
		t.Fatal(e)
	}
	project, e := configuration.Configure(ctx, invocation, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: principal.WorkspaceID, ClientOperationID: "context-backup-project", Name: "Context project", Key: "context", OwnerTeamID: team.Object.ID, InitialAdminUserID: principal.ID, Visibility: "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	bundle, e := exporter.Export(ctx, principal, session.Token, project.Object.ID)
	if e != nil {
		t.Fatal(e)
	}
	importedURL, importedConn, imported, owner, actor, importer := initialize(sourceURL)
	plan, e := importer.Prepare(ctx, actor, owner.Token, bundle, "00000000-0000-4000-8000-000000000044")
	if e != nil {
		t.Fatal(e)
	}
	result, e := importer.Apply(ctx, actor, owner.Token, bundle, plan, plan.PlanDigest)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = importer.Export(ctx, actor, owner.Token, result.ProjectID); e != nil {
		t.Fatal(e)
	}
	baseline := snapshotIncludedTables(t, ctx, imported)
	activity := snapshotTable(t, ctx, imported, "radishnexus.activity_items")
	restoredURL := recoveryBackupDatabase(t, ctx, targetURL)
	restoredConn := connectIntegrationDatabase(t, ctx, restoredURL)
	defer restoredConn.Close(ctx)
	path := filepath.Join(t.TempDir(), "imported-context")
	service := NewService()
	if _, e = service.Backup(ctx, importedConn, importedURL, path); e != nil {
		t.Fatal(e)
	}
	if _, e = service.Restore(ctx, restoredConn, restoredURL, path); e != nil {
		t.Fatal(e)
	}
	restored := connectIntegrationPool(t, ctx, restoredURL)
	defer restored.Close()
	if !reflect.DeepEqual(baseline, snapshotIncludedTables(t, ctx, restored)) {
		t.Fatal("import provenance restore drifted")
	}
	if snapshotTable(t, ctx, restored, "radishnexus.activity_items") != "[]" {
		t.Fatal("backup restored derived projection")
	}
	if _, e = goldenpostgres.New(restored).RebuildActivityProjection(ctx); e != nil {
		t.Fatal(e)
	}
	if activity != snapshotTable(t, ctx, restored, "radishnexus.activity_items") {
		t.Fatal("rebuilt import projection drifted")
	}
	auth := authn.NewService(authpostgres.New(restored), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	fresh, e := auth.Login(ctx, authn.LoginInput{Email: "context-backup@example.test", Password: password})
	if e != nil {
		t.Fatal(e)
	}
	restoredContext := goldenpath.NewContextService(goldenpostgres.NewContextStore(restored, schema))
	status, e := restoredContext.Result(ctx, actor, fresh.Token, plan.OperationID)
	if e != nil || !reflect.DeepEqual(status, result) {
		t.Fatal("restored import receipt changed", e)
	}
	if _, e = restored.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='active' WHERE user_id IN (SELECT target_id FROM radishnexus.context_import_mappings WHERE kind='actor')`); e == nil {
		t.Fatal("restored historical actor can activate")
	}
}
