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

func TestOwnerTransferUpgradeFrom017(t *testing.T) {
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
	name := fmt.Sprintf("nexus_owner_upgrade_%d", time.Now().UnixNano())
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
	for _, m := range migrations[:17] {
		if err = applyMigration(ctx, conn, m); err != nil {
			t.Fatal(err)
		}
	}

	if _, err = conn.Exec(ctx, `INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_upgrade','Upgrade'),('wrk_zero','No owner');
 INSERT INTO radishnexus.users(id,display_name) VALUES('usr_active','Active'),('usr_disabled','Disabled'),('usr_member','Member');
 INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_active','active',clock_timestamp()),('usr_disabled','disabled',clock_timestamp()),('usr_member','active',clock_timestamp());
 INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_upgrade','usr_active','active','owner'),('wrk_upgrade','usr_disabled','active','owner'),('wrk_upgrade','usr_member','active','member'),('wrk_zero','usr_member','active','member');
 INSERT INTO radishnexus.identity_invitations(id,token_digest,workspace_id,created_by,created_at,expires_at,consumed_at) VALUES
 ('inv_1',decode(repeat('11',32),'hex'),'wrk_upgrade','usr_active',clock_timestamp()-interval '1 hour',clock_timestamp()+interval '1 day',NULL),
 ('inv_2',decode(repeat('22',32),'hex'),'wrk_upgrade','usr_disabled',clock_timestamp()-interval '1 hour',clock_timestamp()+interval '1 day',NULL),
 ('inv_3',decode(repeat('33',32),'hex'),'wrk_upgrade','usr_member',clock_timestamp()-interval '1 hour',clock_timestamp()+interval '1 day',NULL),
 ('inv_4',decode(repeat('44',32),'hex'),'wrk_upgrade','usr_active',clock_timestamp()-interval '1 day',clock_timestamp()-interval '1 hour',NULL),
 ('inv_5',decode(repeat('55',32),'hex'),'wrk_upgrade','usr_active',clock_timestamp()-interval '1 hour',clock_timestamp()+interval '1 day',clock_timestamp());
 INSERT INTO radishnexus.workspace_configuration_audit(id,workspace_id,actor_id,command_kind,scope_id,subject_id,result_id,before_state,after_state,changed,request_id,occurred_at) VALUES('cfa_old','wrk_upgrade','usr_active','team.create','wrk_upgrade','','tem_old','','',true,'upgrade',clock_timestamp());
 INSERT INTO radishnexus.workspace_configuration_receipts(workspace_id,actor_id,command_kind,scope_id,subject_id,client_operation_id,payload_sha256,audit_id,created_at) VALUES('wrk_upgrade','usr_active','team.create','wrk_upgrade','','old',repeat('a',64),'cfa_old',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	snapshot := func(sql string) string {
		t.Helper()
		var out string
		if e := conn.QueryRow(ctx, sql).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	memberships := snapshot(`SELECT jsonb_agg(to_jsonb(m) ORDER BY workspace_id,user_id)::text FROM radishnexus.workspace_memberships m`)
	invitations := snapshot(`SELECT jsonb_agg(to_jsonb(i) ORDER BY token_digest)::text FROM radishnexus.identity_invitations i`)
	audit := snapshot(`SELECT to_jsonb(a)::text FROM radishnexus.workspace_configuration_audit a`)
	receipt := snapshot(`SELECT to_jsonb(r)::text FROM radishnexus.workspace_configuration_receipts r`)
	old := &ReadinessChecker{database: conn, expected: migrations[:17]}
	fresh, err := NewReadinessChecker(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err = old.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err = fresh.CheckReady(ctx); err == nil {
		t.Fatal("018 accepted 017")
	}
	if err = Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err = old.CheckReady(ctx); err == nil {
		t.Fatal("017 accepted 018")
	}
	if err = fresh.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	if memberships != snapshot(`SELECT jsonb_agg(to_jsonb(m)-'role_revision'-'lifecycle_revision'-'session_valid_after' ORDER BY workspace_id,user_id)::text FROM radishnexus.workspace_memberships m`) {
		t.Fatal("membership rewritten")
	}
	if invitations != snapshot(`SELECT jsonb_agg(to_jsonb(i)-'creator_role_revision'-'creator_lifecycle_revision'-'target_user_id'-'target_lifecycle_revision' ORDER BY token_digest)::text FROM radishnexus.identity_invitations i`) {
		t.Fatal("invitation rewritten")
	}
	if audit != snapshot(`SELECT (to_jsonb(a)-'actor_before_role'-'actor_after_role'-'actor_before_role_revision'-'actor_after_role_revision'-'subject_before_role_revision'-'subject_after_role_revision'-'management_plan_digest'-'management_before_revision'-'management_after_revision'-'management_actor_after_role'-'revoked_authorization_ids')::text FROM radishnexus.workspace_configuration_audit a`) {
		t.Fatal("audit rewritten")
	}
	if receipt != snapshot(`SELECT to_jsonb(r)::text FROM radishnexus.workspace_configuration_receipts r`) {
		t.Fatal("receipt rewritten")
	}
	var valid bool
	if err = conn.QueryRow(ctx, `SELECT bool_and(creator_role_revision=CASE WHEN token_digest=decode(repeat('11',32),'hex') THEN 1 ELSE 0 END) FROM radishnexus.identity_invitations`).Scan(&valid); err != nil || !valid {
		t.Fatal("legacy invitation tenure", err)
	}
	if err = conn.QueryRow(ctx, `SELECT bool_and(role_revision=1) FROM radishnexus.workspace_memberships`).Scan(&valid); err != nil || !valid {
		t.Fatal("initial revisions", err)
	}
	if err = conn.QueryRow(ctx, `SELECT actor_before_role IS NULL AND actor_after_role IS NULL AND actor_before_role_revision IS NULL AND actor_after_role_revision IS NULL AND subject_before_role_revision IS NULL AND subject_after_role_revision IS NULL FROM radishnexus.workspace_configuration_audit`).Scan(&valid); err != nil || !valid {
		t.Fatal("legacy audit extensions", err)
	}
}
