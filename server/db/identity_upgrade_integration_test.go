//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
)

func TestIdentityUpgradePreservesLegacyUsersAndRequiresReviewedEmails(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := fmt.Sprintf("nexus_identity_upgrade_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	}()
	config := admin.Config().Copy()
	config.Database = name
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `CREATE TABLE public.radishnexus_schema_migrations(sequence integer PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:7] {
		if err := applyMigration(ctx, conn, migration); err != nil {
			t.Fatal(err)
		}
	}
	hash, err := authn.NewArgon2idHasher().Hash("legacy account password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `
        INSERT INTO radishnexus.users(id,display_name) VALUES ('usr_legacy','Legacy'),('usr_disabled','Disabled');
        INSERT INTO radishnexus.workspaces(id,name) VALUES ('wrk_legacy','Legacy Workspace');
        INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES ('wrk_legacy','usr_legacy','active','owner');
    `); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO radishnexus.local_accounts(user_id,login_name,password_hash,status,created_at,password_changed_at) VALUES ('usr_legacy','legacy',$1,'active',now(),now()),('usr_disabled','disabled',$1,'disabled',now(),now())`, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO radishnexus.user_sessions(id,user_id,token_digest,csrf_token_digest,created_at,expires_at) VALUES ('ses_legacy','usr_legacy',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),now(),now()+interval '24 hours')`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var storedHash, status string
	var email *string
	if err := conn.QueryRow(ctx, `SELECT credential.password_hash,credential.email,account.status FROM radishnexus.local_credentials AS credential JOIN radishnexus.user_accounts AS account USING(user_id) WHERE credential.user_id='usr_disabled'`).Scan(&storedHash, &email, &status); err != nil {
		t.Fatal(err)
	}
	if storedHash != hash || email != nil || status != "disabled" {
		t.Fatal("upgrade changed credential or reactivated account")
	}
	var activeSessions, refs int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM radishnexus.user_sessions WHERE revoked_at IS NULL`).Scan(&activeSessions); err != nil || activeSessions != 0 {
		t.Fatal("upgrade retained old sessions", err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM radishnexus.workspace_memberships WHERE user_id='usr_legacy' AND workspace_id='wrk_legacy' AND role='owner'`).Scan(&refs); err != nil || refs != 1 {
		t.Fatal("upgrade changed business membership", err)
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := authpostgres.New(pool)
	auth := authn.NewService(store, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	if _, err := auth.Login(ctx, authn.LoginInput{Email: "legacy", Password: "legacy account password"}); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatal("legacy username still accepted", err)
	}
	if err := store.MapLegacyEmails(ctx, []authpostgres.EmailMapping{{UserID: "usr_legacy", Email: "same@example.test"}, {UserID: "usr_disabled", Email: "SAME@example.test"}}); err == nil {
		t.Fatal("duplicate mapping accepted")
	}
	if err := store.MapLegacyEmails(ctx, []authpostgres.EmailMapping{{UserID: "usr_legacy", Email: "legacy@example.test"}, {UserID: "usr_missing", Email: "missing@example.test"}}); err == nil {
		t.Fatal("partial mapping accepted")
	}
	if err := conn.QueryRow(ctx, `SELECT email FROM radishnexus.local_credentials WHERE user_id='usr_legacy'`).Scan(&email); err != nil || email != nil {
		t.Fatal("failed mapping partially committed", err)
	}
	if err := store.MapLegacyEmails(ctx, []authpostgres.EmailMapping{{UserID: "usr_legacy", Email: "Legacy@Example.Test"}, {UserID: "usr_disabled", Email: "disabled@example.test"}}); err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login(ctx, authn.LoginInput{Email: "legacy@example.test", Password: "legacy account password"})
	if err != nil || session.Account.User.ID != "usr_legacy" {
		t.Fatal("mapped login lost original identity", err)
	}
	if _, err := auth.Login(ctx, authn.LoginInput{Email: "disabled@example.test", Password: "legacy account password"}); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatal("mapping activated disabled account", err)
	}
	if err := store.MapLegacyEmails(ctx, []authpostgres.EmailMapping{{UserID: "usr_legacy", Email: "replace@example.test"}}); err == nil {
		t.Fatal("mapped email silently replaced")
	}
}
