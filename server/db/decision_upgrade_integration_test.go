//go:build integration

package db

import (
	"context"
	"testing"
)

const legacyDecisionBaselineSQL = `BEGIN;
 INSERT INTO radishnexus.users(id,display_name) VALUES('usr_old','Old');
 INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_old','active',clock_timestamp());
 INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_old','Old');
 INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_old','usr_old','active','owner');
 INSERT INTO radishnexus.teams(id,workspace_id,name) VALUES('tem_old','wrk_old','Old');
 INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind,created_by_id) VALUES('prj_old','wrk_old','old','Old','tem_old','workspace','active','user','usr_old');
 INSERT INTO radishnexus.threads(id,workspace_id,governing_project_id,title,visibility,created_by) VALUES('thr_old','wrk_old','prj_old','Old source','project','usr_old');
 INSERT INTO radishnexus.decisions(id,workspace_id,governing_project_id,question,status,proposer_id,created_by_kind,created_by_id,created_at,updated_at,outcome,rationale,decider_ids,decided_at)
 VALUES('dec_old','wrk_old','prj_old','Legacy accepted?','accepted','usr_old','user','usr_old','2026-10-28T00:00:00Z','2026-10-28T00:00:00Z','Yes','Reason',ARRAY['usr_old'],'2026-10-28T00:00:00Z');
 INSERT INTO radishnexus.domain_events(event_id,event_type,schema_version,workspace_id,actor_kind,actor_id,source_kind,primary_entity_type,primary_entity_id,project_id,correlation_id,payload,occurred_at)
 VALUES('evt_proposed','decision.proposed',1,'wrk_old','user','usr_old','web','decision','dec_old','prj_old','cor_old','{"status":"proposed","evidence":{"type":"thread","id":"thr_old"}}','2026-10-28T00:00:00Z'),
 ('evt_accepted','decision.accepted',1,'wrk_old','user','usr_old','web','decision','dec_old','prj_old','cor_old','{"status":"accepted"}','2026-10-28T00:00:00Z');
 INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,source_event_id)
 VALUES('lnk_old','wrk_old','decision','dec_old','derived-from','thread','thr_old','asserted','user','user','usr_old','evt_proposed');
 INSERT INTO radishnexus.collaboration_command_receipts(workspace_id,actor_id,command_kind,target_type,target_id,client_operation_id,payload_sha256,result_type,result_id,event_id,created_at)
 VALUES('wrk_old','usr_old','decision.accept','decision','dec_old','legacy-accept',repeat('a',64),'decision','dec_old','evt_accepted','2026-10-28T00:00:00Z');
 COMMIT;`

func TestDecisionUpgradeFrom022PreservesHistoryAndRejectsReservedState(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "supported", true: "reserved-rejected"}[invalid], func(t *testing.T) {
			conn, _ := upgradeFailureDatabase(t, 22)
			ctx := context.Background()
			// This is a labelled legacy schema fixture, not evidence of a new command.
			upgradeSQL(t, conn, legacyDecisionBaselineSQL)
			if invalid {
				upgradeSQL(t, conn, `UPDATE radishnexus.decisions SET status='rejected',rejection_reason='reserved probe'`)
			}
			var before, after string
			query := `SELECT jsonb_build_object('decisions',(SELECT jsonb_agg(to_jsonb(d)-'decision_revision') FROM radishnexus.decisions d),'receipts',(SELECT jsonb_agg(to_jsonb(r)) FROM radishnexus.collaboration_command_receipts r),'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY event_id) FROM radishnexus.domain_events e))::text`
			if e := conn.QueryRow(ctx, query).Scan(&before); e != nil {
				t.Fatal(e)
			}
			oldHistory := upgradeHistory(t, conn)
			e := Migrate(ctx, conn)
			if invalid {
				if e == nil {
					t.Fatal("unsupported baseline migrated")
				}
				if upgradeHistory(t, conn) != oldHistory {
					t.Fatal("failed migration changed history")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = conn.QueryRow(ctx, query).Scan(&after); e != nil || before != after {
				t.Fatal("legacy fact or receipt rewritten", e)
			}
			migrations, e := loadMigrations(migrationFiles)
			if e != nil {
				t.Fatal(e)
			}
			if e = (&ReadinessChecker{database: conn, expected: migrations[:22]}).CheckReady(ctx); e == nil {
				t.Fatal("old application accepted schema 023")
			}
			if e = (&ReadinessChecker{database: conn, expected: migrations}).CheckReady(ctx); e != nil {
				t.Fatal(e)
			}
		})
	}
}
