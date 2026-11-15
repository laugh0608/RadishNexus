//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
)

func TestDecisionReviewUpgradeFrom024(t *testing.T) {
	conn, _ := upgradeFailureDatabase(t, 24)
	ctx := context.Background()
	legacy := strings.Replace(legacyDecisionBaselineSQL, "outcome,rationale,decider_ids,decided_at)", "outcome,rationale,decider_ids,decided_at,decision_revision,review_at)", 1)
	legacy = strings.Replace(legacy, "ARRAY['usr_old'],'2026-10-28T00:00:00Z');", "ARRAY['usr_old'],'2026-10-28T00:00:00Z',2,'2027-01-02T00:00:00Z');", 1)
	upgradeSQL(t, conn, legacy)
	upgradeSQL(t, conn, `INSERT INTO radishnexus.activity_items(workspace_id,target_type,target_id,event_id,activity_type,actor_kind,actor_id,occurred_at,projection_version,safe_facts)
		SELECT workspace_id,primary_entity_type,primary_entity_id,event_id,event_type,actor_kind,actor_id,occurred_at,12,payload
		FROM radishnexus.domain_events WHERE event_id='evt_accepted'`)

	const query = `SELECT jsonb_build_object(
		'decision',(SELECT to_jsonb(d) FROM radishnexus.decisions d WHERE id='dec_old'),
		'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY event_id) FROM radishnexus.domain_events e),
		'receipts',(SELECT jsonb_agg(to_jsonb(r)) FROM radishnexus.collaboration_command_receipts r),
		'activity',(SELECT to_jsonb(a)-'projection_version' FROM radishnexus.activity_items a))::text`
	var before, after string
	if err := conn.QueryRow(ctx, query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, query).Scan(&after); err != nil || before != after {
		t.Fatalf("migration changed pre-existing facts: %v", err)
	}
	var version int
	if err := conn.QueryRow(ctx, `SELECT projection_version FROM radishnexus.activity_items`).Scan(&version); err != nil || version != 14 {
		t.Fatalf("Activity version = %d: %v", version, err)
	}
	requireUpgradePrefix(t, conn, latestUpgradeSequence(t))
}
