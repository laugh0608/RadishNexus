//go:build integration

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestRecoveryUpgradeFrom016(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := fmt.Sprintf("nexus_recovery_upgrade_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
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
	if _, err = conn.Exec(ctx, `CREATE TABLE public.radishnexus_schema_migrations(sequence integer PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:16] {
		if err = applyMigration(ctx, conn, m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = conn.Exec(ctx, `INSERT INTO radishnexus.users(id,display_name) VALUES('usr_old','Old'); INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_old','active',clock_timestamp()); INSERT INTO radishnexus.identity_audit(id,action,actor_id,occurred_at) VALUES('ida_old','local.account.created','usr_old',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	var before string
	if err = conn.QueryRow(ctx, `SELECT to_jsonb(a)::text FROM radishnexus.identity_audit a WHERE id='ida_old'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	old := &ReadinessChecker{database: conn, expected: migrations[:16]}
	fresh, err := NewReadinessChecker(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err = old.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err = fresh.CheckReady(ctx); err == nil {
		t.Fatal("new accepted 016")
	}
	if err = Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err = old.CheckReady(ctx); err == nil {
		t.Fatal("old accepted 017")
	}
	if err = fresh.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	var after string
	var shape bool
	if err = conn.QueryRow(ctx, `SELECT (to_jsonb(a)-'actor_kind'-'operator_ref'-'target_user_id'-'operation_id'-'reason_code'-'previous_password_changed_at'-'applied_password_changed_at')::text,actor_kind='user' AND operator_ref IS NULL AND target_user_id IS NULL AND operation_id IS NULL FROM radishnexus.identity_audit a WHERE id='ida_old'`).Scan(&after, &shape); err != nil || before != after || !shape {
		t.Fatal("old audit changed", err)
	}
	// Existing callers still use the original five-column INSERT shape.
	if _, err = conn.Exec(ctx, `INSERT INTO radishnexus.identity_audit(id,action,actor_id,occurred_at) VALUES('ida_compatible','external.login','usr_old',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`UPDATE radishnexus.identity_audit SET actor_id=NULL WHERE id='ida_old'`,
		`UPDATE radishnexus.identity_audit SET actor_kind='operator' WHERE id='ida_old'`,
		`UPDATE radishnexus.identity_audit SET action='local.password.recovered',actor_kind='operator',actor_id=NULL,operator_ref='ops',target_user_id='usr_old',operation_id='12345678-1234-4234-8234-123456789abc',reason_code='lost_local_password',previous_password_changed_at=occurred_at-interval '1 second',applied_password_changed_at=occurred_at WHERE id='ida_old'`,
		`INSERT INTO radishnexus.identity_audit(id,action,actor_kind,occurred_at) VALUES('ida_invalid','local.password.recovered','operator',clock_timestamp())`,
	} {
		if _, err = conn.Exec(ctx, sql); err == nil {
			t.Fatal("invalid audit accepted")
		}
	}
}
