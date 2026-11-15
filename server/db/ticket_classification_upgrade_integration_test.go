//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
)

func TestTicketClassificationUpgradeFrom027(t *testing.T) {
	conn, _ := upgradeFailureDatabase(t, 27)
	ctx := context.Background()
	legacy := strings.Replace(legacyDecisionBaselineSQL, "outcome,rationale,decider_ids,decided_at)", "outcome,rationale,decider_ids,decided_at,decision_revision)", 1)
	legacy = strings.Replace(legacy, "ARRAY['usr_old'],'2026-10-28T00:00:00Z');", "ARRAY['usr_old'],'2026-10-28T00:00:00Z',2);", 1)
	upgradeSQL(t, conn, legacy)
	upgradeSQL(t, conn, `INSERT INTO radishnexus.tickets(id,workspace_id,governing_project_id,title,status,created_by,updated_at)
		VALUES('tkt_old','wrk_old','prj_old','Old work','done','usr_old','2026-11-01T00:00:00Z');
		INSERT INTO radishnexus.activity_items(workspace_id,target_type,target_id,event_id,activity_type,actor_kind,actor_id,occurred_at,projection_version,safe_facts)
		SELECT workspace_id,primary_entity_type,primary_entity_id,event_id,event_type,actor_kind,actor_id,occurred_at,15,payload
		FROM radishnexus.domain_events WHERE event_id='evt_accepted'`)
	const snapshot = `SELECT jsonb_build_array(
		(SELECT to_jsonb(t)-'ticket_type'-'classification_revision' FROM radishnexus.tickets t),
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
		t.Fatal("028 migration rewrote existing facts", err)
	}
	var kind string
	var revision, projection, assignments int
	if err := conn.QueryRow(ctx, `SELECT t.ticket_type,t.classification_revision,a.projection_version,
		(SELECT count(*) FROM radishnexus.ticket_label_assignments)
		FROM radishnexus.tickets t CROSS JOIN radishnexus.activity_items a`).Scan(&kind, &revision, &projection, &assignments); err != nil ||
		kind != "unset" || revision != 1 || projection != 16 || assignments != 0 {
		t.Fatal(kind, revision, projection, assignments, err)
	}
	requireUpgradePrefix(t, conn, 28)
}
