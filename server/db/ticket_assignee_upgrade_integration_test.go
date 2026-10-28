//go:build integration

package db

import (
	"context"
	"testing"
)

func TestTicketAssignmentUpgradeFrom020(t *testing.T) {
	conn, _ := upgradeFailureDatabase(t, 20)
	ctx := context.Background()
	upgradeSQL(t, conn, `INSERT INTO radishnexus.users(id,display_name) VALUES('usr_old','Old');
 INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_old','active',clock_timestamp());
 INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_old','Old');
 INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_old','usr_old','active','owner');
 INSERT INTO radishnexus.teams(id,workspace_id,name) VALUES('tem_old','wrk_old','Old');
 INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind,created_by_id) VALUES('prj_old','wrk_old','old','Old','tem_old','workspace','active','user','usr_old');
 INSERT INTO radishnexus.tickets(id,workspace_id,governing_project_id,title,status,created_by) VALUES('tkt_old','wrk_old','prj_old','Old work','done','usr_old');`)
	var before, after string
	if err := conn.QueryRow(ctx, `SELECT to_jsonb(t)::text FROM radishnexus.tickets t`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	old := &ReadinessChecker{database: conn, expected: migrations[:20]}
	fresh, err := NewReadinessChecker(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err = fresh.CheckReady(ctx); err == nil {
		t.Fatal("new binary accepted old schema")
	}
	if err = Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err = old.CheckReady(ctx); err == nil {
		t.Fatal("old binary accepted new schema")
	}
	if err = fresh.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	var target *string
	var rev, events int
	if err = conn.QueryRow(ctx, `SELECT (to_jsonb(t)-'assignee_user_id'-'assignment_revision')::text,assignee_user_id,assignment_revision FROM radishnexus.tickets t`).Scan(&after, &target, &rev); err != nil || after != before || target != nil || rev != 1 {
		t.Fatal("old responsibility inferred", after, err)
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM radishnexus.domain_events`).Scan(&events); err != nil || events != 0 {
		t.Fatal("invented history", err)
	}
	upgradeSQL(t, conn, `UPDATE radishnexus.tickets SET assignee_user_id='usr_old'`)
	if _, err = conn.Exec(ctx, `UPDATE radishnexus.tickets SET assignee_user_id='usr_unknown'`); err == nil {
		t.Fatal("unknown identity accepted")
	}
	if _, err = conn.Exec(ctx, `UPDATE radishnexus.tickets SET assignment_revision=0`); err == nil {
		t.Fatal("invalid revision accepted")
	}
	for _, payload := range []string{`{"assignment":{"from_user_id":null,"to_user_id":"usr_old","from_revision":1,"to_revision":2}}`, `{"assignment":{"from_user_id":null,"to_user_id":"usr_old","from_revision":"1","to_revision":2}}`, `{"assignment":{"from_user_id":null,"to_user_id":"usr_old","from_revision":2147483647,"to_revision":2147483648}}`} {
		var valid bool
		if err = conn.QueryRow(ctx, `SELECT radishnexus.valid_ticket_assignment($1::jsonb)`, payload).Scan(&valid); err != nil {
			t.Fatal(err)
		}
		if valid != (payload == `{"assignment":{"from_user_id":null,"to_user_id":"usr_old","from_revision":1,"to_revision":2}}`) {
			t.Fatal("SQL assignment validator drift")
		}
	}
}
