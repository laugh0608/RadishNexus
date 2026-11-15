//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
)

func TestTicketDueDateUpgradeFrom025(t *testing.T) {
	conn, _ := upgradeFailureDatabase(t, 25)
	ctx := context.Background()
	legacy := strings.Replace(legacyDecisionBaselineSQL, "outcome,rationale,decider_ids,decided_at)", "outcome,rationale,decider_ids,decided_at,decision_revision)", 1)
	legacy = strings.Replace(legacy, "ARRAY['usr_old'],'2026-10-28T00:00:00Z');", "ARRAY['usr_old'],'2026-10-28T00:00:00Z',2);", 1)
	upgradeSQL(t, conn, legacy)
	upgradeSQL(t, conn, `INSERT INTO radishnexus.tickets(id,workspace_id,governing_project_id,title,status,created_by,updated_at)
		VALUES('tkt_old','wrk_old','prj_old','Old work','done','usr_old','2026-11-01T00:00:00Z');
		INSERT INTO radishnexus.activity_items(workspace_id,target_type,target_id,event_id,activity_type,actor_kind,actor_id,occurred_at,projection_version,safe_facts)
		SELECT workspace_id,primary_entity_type,primary_entity_id,event_id,event_type,actor_kind,actor_id,occurred_at,13,payload
		FROM radishnexus.domain_events WHERE event_id='evt_accepted'`)
	const snapshot = `SELECT jsonb_build_array(
		(SELECT to_jsonb(t)-'due_date'-'due_date_revision'-'ticket_type'-'classification_revision' FROM radishnexus.tickets t),
		(SELECT jsonb_agg(e ORDER BY e.event_id) FROM radishnexus.domain_events e),
		(SELECT jsonb_agg(r) FROM radishnexus.collaboration_command_receipts r),
		(SELECT to_jsonb(a)-'projection_version' FROM radishnexus.activity_items a))::text`
	var before, after string
	if err := conn.QueryRow(ctx, snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, snapshot).Scan(&after); err != nil || after != before {
		t.Fatal("migration changed old facts", err)
	}
	var revision, projection int
	var date *string
	if err := conn.QueryRow(ctx, `SELECT t.due_date::text,t.due_date_revision,a.projection_version FROM radishnexus.tickets t CROSS JOIN radishnexus.activity_items a`).Scan(&date, &revision, &projection); err != nil || date != nil || revision != 1 || projection != 16 {
		t.Fatal(date, revision, projection, err)
	}
	requireUpgradePrefix(t, conn, 28)

	for payload, want := range map[string]bool{
		`{"due_date":{"from":null,"to":"2032-02-29","from_revision":1,"to_revision":2}}`:   true,
		`{"due_date":{"from":"0001-01-01","to":null,"from_revision":1,"to_revision":2}}`:   true,
		`{"due_date":{"to":"2032-02-29","from_revision":1,"to_revision":2}}`:               false,
		`{"due_date":{"from":null,"to":"2031-02-29","from_revision":1,"to_revision":2}}`:   false,
		`{"due_date":{"from":null,"to":"0000-01-01","from_revision":1,"to_revision":2}}`:   false,
		`{"due_date":{"from":null,"to":"2032-02-29","from_revision":"1","to_revision":2}}`: false,
	} {
		var got bool
		if err := conn.QueryRow(ctx, `SELECT radishnexus.valid_ticket_due_date_change($1::jsonb)`, payload).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: %v, %v", payload, got, err)
		}
	}
}
