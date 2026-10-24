//go:build integration

package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestMembershipUpgradeFrom019(t *testing.T) {
	conn, _ := upgradeFailureDatabase(t, 19)
	ctx := context.Background()
	upgradeSQL(t, conn, `INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_upgrade','Membership upgrade');
 INSERT INTO radishnexus.users(id,display_name) VALUES('usr_active','Active'),('usr_disabled','Disabled'),('usr_paused','Paused'),('usr_orphan','No parent');
 INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_active','active',clock_timestamp()),('usr_disabled','disabled',clock_timestamp()),('usr_paused','active',clock_timestamp()),('usr_orphan','active',clock_timestamp());
 INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_upgrade','usr_active','active','owner'),('wrk_upgrade','usr_disabled','active','member'),('wrk_upgrade','usr_paused','suspended','member'),('wrk_upgrade','usr_orphan','active','member');
 INSERT INTO radishnexus.teams(id,workspace_id,name) VALUES('tem_upgrade','wrk_upgrade','Team');
 INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind) VALUES('prj_upgrade','wrk_upgrade','upgrade','Project','tem_upgrade','restricted','archived','system');
 INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) VALUES('wrk_upgrade','prj_upgrade','usr_active','admin'),('wrk_upgrade','prj_upgrade','usr_disabled','viewer'),('wrk_upgrade','prj_upgrade','usr_paused','viewer');
 INSERT INTO radishnexus.threads(id,workspace_id,governing_project_id,title,visibility,created_by) SELECT 'thr_'||user_id,'wrk_upgrade','prj_upgrade','Retained title','restricted',user_id FROM radishnexus.user_accounts;
 INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id) SELECT 'wrk_upgrade','thr_'||user_id,user_id FROM radishnexus.user_accounts;
 INSERT INTO radishnexus.identity_invitations(id,token_digest,workspace_id,created_by,created_at,expires_at,creator_role_revision) VALUES('inv_valid',decode(repeat('11',32),'hex'),'wrk_upgrade','usr_active',clock_timestamp(),clock_timestamp()+interval '1 day',1),('inv_old',decode(repeat('22',32),'hex'),'wrk_upgrade','usr_active',clock_timestamp()-interval '2 days',clock_timestamp()-interval '1 day',1);`)
	var before string
	if e := conn.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM radishnexus.threads t`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	migrations, e := loadMigrations(migrationFiles)
	if e != nil {
		t.Fatal(e)
	}
	old := &ReadinessChecker{database: conn, expected: migrations[:19]}
	fresh, e := NewReadinessChecker(conn)
	if e != nil {
		t.Fatal(e)
	}
	if e = fresh.CheckReady(ctx); e == nil {
		t.Fatal("020 accepted 019")
	}
	if e = Migrate(ctx, conn); e != nil {
		t.Fatal(e)
	}
	if e = old.CheckReady(ctx); e == nil {
		t.Fatal("019 accepted 020")
	}
	if e = fresh.CheckReady(ctx); e != nil {
		t.Fatal(e)
	}
	var after string
	if e = conn.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(t)-'management_revision' ORDER BY id)::text FROM radishnexus.threads t`).Scan(&after); e != nil || after != before {
		t.Fatal("creator/history rewritten", e)
	}
	for user, want := range map[string]string{"usr_active": "manager", "usr_disabled": "member", "usr_paused": "member", "usr_orphan": "member"} {
		var got string
		if e = conn.QueryRow(ctx, `SELECT management_role FROM radishnexus.thread_memberships WHERE user_id=$1`, user).Scan(&got); e != nil || got != want {
			t.Fatal("migration silently granted authority", user, got, e)
		}
	}
	var life int64
	var untouched bool
	if e = conn.QueryRow(ctx, `SELECT lifecycle_revision,session_valid_after='-infinity' FROM radishnexus.workspace_memberships WHERE user_id='usr_active'`).Scan(&life, &untouched); e != nil || life != 1 || !untouched {
		t.Fatal("migration invalidated unchanged sessions", e)
	}
	for id, want := range map[string]int64{"inv_valid": 1, "inv_old": 0} {
		var got int64
		if e = conn.QueryRow(ctx, `SELECT creator_lifecycle_revision FROM radishnexus.identity_invitations WHERE id=$1`, id).Scan(&got); e != nil || got != want {
			t.Fatal("invitation resurrected", id, got, e)
		}
	}
	upgradeSQL(t, conn, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE user_id='usr_paused'`)
	if e = conn.QueryRow(ctx, `SELECT lifecycle_revision,session_valid_after>'-infinity' FROM radishnexus.workspace_memberships WHERE user_id='usr_paused'`).Scan(&life, &untouched); e != nil || life != 2 || !untouched {
		t.Fatal("transition did not invalidate old sessions", life, e)
	}
	for _, sql := range []string{`UPDATE radishnexus.workspace_memberships SET lifecycle_revision=1 WHERE user_id='usr_paused'`, `UPDATE radishnexus.workspace_memberships SET session_valid_after='-infinity' WHERE user_id='usr_paused'`, `UPDATE radishnexus.workspace_memberships SET status='removed' WHERE user_id='usr_active'`, `UPDATE radishnexus.thread_memberships SET management_role='owner' WHERE user_id='usr_active'`} {
		_, e = conn.Exec(ctx, sql)
		var pgerr *pgconn.PgError
		if !errors.As(e, &pgerr) || pgerr.Code != "23514" {
			t.Fatal("invalid migration state accepted", e)
		}
	}
}
