//go:build integration

package db

import (
	"context"
	"testing"
)

func TestDecisionSupersessionUpgradeFrom023(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "supported", true: "reserved-supersedes"}[invalid], func(t *testing.T) {
			conn, _ := upgradeFailureDatabase(t, 23)
			ctx := context.Background()
			// A labelled historical schema fixture; business commands are tested separately.
			upgradeSQL(t, conn, legacyDecisionBaselineSQL)
			upgradeSQL(t, conn, `BEGIN;
   INSERT INTO radishnexus.decisions(id,workspace_id,governing_project_id,question,status,proposer_id,created_by_kind,created_by_id,created_at,updated_at)
   VALUES('dec_proposal','wrk_old','prj_old','Old proposed?','proposed','usr_old','user','usr_old','2026-10-28T00:00:00Z','2026-10-28T00:00:00.000001Z');
   INSERT INTO radishnexus.decisions(id,workspace_id,governing_project_id,question,status,proposer_id,created_by_kind,created_by_id,created_at,updated_at,rejection_reason,decider_ids,decided_at)
   VALUES('dec_rejected','wrk_old','prj_old','Old rejected?','rejected','usr_old','user','usr_old','2026-10-28T00:00:00Z','2026-10-28T00:00:01Z','Original reason',ARRAY['usr_old'],'2026-10-28T00:00:01Z');
   INSERT INTO radishnexus.domain_events(event_id,event_type,schema_version,workspace_id,actor_kind,actor_id,source_kind,primary_entity_type,primary_entity_id,project_id,correlation_id,payload,occurred_at)
   VALUES('evt_proposal_old','decision.proposed',1,'wrk_old','user','usr_old','web','decision','dec_proposal','prj_old','cor_old','{"status":"proposed","evidence":{"type":"thread","id":"thr_old"}}','2026-10-28T00:00:00Z'),
   ('evt_rejected_proposal_old','decision.proposed',1,'wrk_old','user','usr_old','web','decision','dec_rejected','prj_old','cor_old','{"status":"proposed","evidence":{"type":"thread","id":"thr_old"}}','2026-10-28T00:00:00Z'),
   ('evt_rejected_old','decision.rejected',1,'wrk_old','user','usr_old','web','decision','dec_rejected','prj_old','cor_old','{"status":"rejected"}','2026-10-28T00:00:01Z');
   INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,source_event_id)
   VALUES('lnk_proposal_old','wrk_old','decision','dec_proposal','derived-from','thread','thr_old','asserted','user','user','usr_old','evt_proposal_old'),
   ('lnk_rejected_old','wrk_old','decision','dec_rejected','derived-from','thread','thr_old','asserted','user','user','usr_old','evt_rejected_proposal_old');
   INSERT INTO radishnexus.collaboration_command_receipts(workspace_id,actor_id,command_kind,target_type,target_id,client_operation_id,payload_sha256,result_type,result_id,event_id,created_at)
   VALUES('wrk_old','usr_old','decision.reject','decision','dec_rejected','legacy-reject',repeat('b',64),'decision','dec_rejected','evt_rejected_old','2026-10-28T00:00:01Z');
   COMMIT;`)
			if invalid {
				upgradeSQL(t, conn, `BEGIN; INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id) VALUES('lnk_reserved','wrk_old','decision','dec_old','supersedes','decision','dec_old','asserted','user','user','usr_old'); UPDATE radishnexus.decisions SET status='superseded' WHERE id='dec_old'; COMMIT;`)
			}
			query := `SELECT jsonb_build_object('decisions',(SELECT jsonb_agg(to_jsonb(d)-'decision_revision' ORDER BY id) FROM radishnexus.decisions d),'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY event_id) FROM radishnexus.domain_events e),'links',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM radishnexus.entity_links l),'receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY command_kind) FROM radishnexus.collaboration_command_receipts r))::text`
			var before, after string
			if e := conn.QueryRow(ctx, query).Scan(&before); e != nil {
				t.Fatal(e)
			}
			history := upgradeHistory(t, conn)
			e := Migrate(ctx, conn)
			if invalid {
				if e == nil || history != upgradeHistory(t, conn) {
					t.Fatal("unsupported baseline migrated", e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = conn.QueryRow(ctx, query).Scan(&after); e != nil || before != after {
				t.Fatal("legacy facts changed", e)
			}
			var valid bool
			if e = conn.QueryRow(ctx, `SELECT bool_and(decision_revision=CASE WHEN status='proposed' THEN 1 ELSE 2 END) FROM radishnexus.decisions`).Scan(&valid); e != nil || !valid {
				t.Fatal("revision backfill", e)
			}
			migrations, e := loadMigrations(migrationFiles)
			if e != nil {
				t.Fatal(e)
			}
			if e = (&ReadinessChecker{database: conn, expected: migrations[:23]}).CheckReady(ctx); e == nil {
				t.Fatal("old artifact accepted 024")
			}
			if e = (&ReadinessChecker{database: conn, expected: migrations}).CheckReady(ctx); e != nil {
				t.Fatal(e)
			}
		})
	}
}
