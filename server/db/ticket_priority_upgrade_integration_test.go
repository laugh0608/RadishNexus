//go:build integration

package db

import (
	"context"
	"testing"
)

func TestTicketPriorityUpgradeFrom021(t *testing.T) {
	conn, _ := upgradeFailureDatabase(t, 21)
	ctx := context.Background()
	upgradeSQL(t, conn, `INSERT INTO radishnexus.users(id,display_name) VALUES('usr_old','Old');
 INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_old','active',clock_timestamp());
 INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_old','Old');
 INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_old','usr_old','active','owner');
 INSERT INTO radishnexus.teams(id,workspace_id,name) VALUES('tem_old','wrk_old','Old');
 INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind,created_by_id) VALUES('prj_old','wrk_old','old','Old','tem_old','workspace','active','user','usr_old');
 INSERT INTO radishnexus.tickets(id,workspace_id,governing_project_id,title,status,created_by,assignee_user_id) VALUES('tkt_old','wrk_old','prj_old','Old work','done','usr_old','usr_old');`)
	var before, after string
	if err := conn.QueryRow(ctx, `SELECT to_jsonb(t)::text FROM radishnexus.tickets t`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	old := &ReadinessChecker{database: conn, expected: migrations[:21]}
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
	var priority string
	var rev, events int
	if err = conn.QueryRow(ctx, `SELECT (to_jsonb(t)-'priority'-'priority_revision')::text,priority,priority_revision FROM radishnexus.tickets t`).Scan(&after, &priority, &rev); err != nil || after != before || priority != "unset" || rev != 1 {
		t.Fatal("old priority inferred or history changed", after, priority, rev, err)
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM radishnexus.domain_events`).Scan(&events); err != nil || events != 0 {
		t.Fatal("invented history", err)
	}
	for _, sql := range []string{`UPDATE radishnexus.tickets SET priority='p0'`, `UPDATE radishnexus.tickets SET priority=NULL`, `UPDATE radishnexus.tickets SET priority_revision=0`} {
		if _, err = conn.Exec(ctx, sql); err == nil {
			t.Fatal("invalid priority accepted", sql)
		}
	}
	for i, payload := range []string{`{"priority":{"from":"unset","to":"high","from_revision":1,"to_revision":2}}`, `{"priority":{"from":"unset","to":"high","from_revision":"1","to_revision":2}}`, `{"priority":{"from":"high","to":"high","from_revision":1,"to_revision":2}}`, `{"priority":{"from":"p0","to":"high","from_revision":1,"to_revision":2}}`, `{"priority":{"from":"unset","to":"high","from_revision":2147483647,"to_revision":2147483648}}`, `{"priority":{"from":"unset","to":"high","from_revision":1,"to_revision":2},"extra":true}`} {
		var valid bool
		if err = conn.QueryRow(ctx, `SELECT radishnexus.valid_ticket_priority($1::jsonb)`, payload).Scan(&valid); err != nil || valid != (i == 0) {
			t.Fatal("SQL validator drift", valid, err)
		}
	}
}
