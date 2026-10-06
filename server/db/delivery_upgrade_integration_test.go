//go:build integration

package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestDeliveryUpgradePreservesHistoryAndLegacyReceipts(t *testing.T) {
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
	name := fmt.Sprintf("nexus_delivery_upgrade_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); e != nil {
			t.Error(e)
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
	for _, m := range migrations[:11] {
		if err = applyMigration(ctx, conn, m); err != nil {
			t.Fatal(err)
		}
	}
	// These are historical facts in the old schema, not new configuration actions.
	if _, err = conn.Exec(ctx, `
INSERT INTO radishnexus.users(id,display_name) VALUES('usr_owner','Owner');
INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_owner','active',now());
INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_main','Main');
INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_main','usr_owner','active','owner');
INSERT INTO radishnexus.teams(id,workspace_id,name) VALUES('tem_old','wrk_main','Team');
INSERT INTO radishnexus.components(id,workspace_id,key,name,type,owner_team_id,lifecycle,created_by_kind,created_by_id) VALUES('cmp_old','wrk_main','OLD','Old','service','tem_old','active','user','usr_owner');
INSERT INTO radishnexus.environments(id,workspace_id,key,name,classification,owner_team_id,status,created_by_kind,created_by_id) VALUES('env_old','wrk_main','OLD','Old','staging','tem_old','active','user','usr_owner');
INSERT INTO radishnexus.environment_deployment_authorizations(id,workspace_id,environment_id,user_id,granted_by) VALUES('dpa_old','wrk_main','env_old','usr_owner','usr_owner');
INSERT INTO radishnexus.ci_runs(id,workspace_id,component_id,source_kind,source_id,external_run_key,status,completed_at) VALUES('cir_old','wrk_main','cmp_old','jenkins','source','1','succeeded',now());
INSERT INTO radishnexus.deployments(id,workspace_id,environment_id,ci_run_id,authorization_id,status,completed_at,recorded_by,source_kind,recorded_at) VALUES('dpl_old','wrk_main','env_old','cir_old','dpa_old','succeeded',now(),'usr_owner','web',now());
UPDATE radishnexus.environment_deployment_authorizations SET status='revoked',revoked_by='usr_owner',revoked_at=now() WHERE id='dpa_old';
INSERT INTO radishnexus.domain_events(event_id,event_type,schema_version,workspace_id,actor_kind,actor_id,source_kind,primary_entity_type,primary_entity_id,correlation_id,occurred_at,payload) VALUES('evt_old','deployment.recorded',1,'wrk_main','user','usr_owner','web','deployment','dpl_old','upgrade',now(),'{"status":"succeeded","environment":{"type":"environment","id":"env_old"},"ci_run":{"type":"ci-run","id":"cir_old"}}');
INSERT INTO radishnexus.activity_items(workspace_id,target_type,target_id,event_id,activity_type,actor_kind,actor_id,occurred_at,projection_version,safe_facts) VALUES('wrk_main','deployment','dpl_old','evt_old','deployment.recorded','user','usr_owner',now(),2,'{"status":"succeeded"}');
INSERT INTO radishnexus.workspace_configuration_audit(id,workspace_id,actor_id,command_kind,scope_id,subject_id,result_id,before_state,after_state,changed,request_id,occurred_at) VALUES('cfa_old','wrk_main','usr_owner','team.create','wrk_main','','tem_old','','',true,'upgrade',now());
`); err != nil {
		t.Fatal(err)
	}
	legacyJSON := `{"Kind":"team.create","ScopeID":"wrk_main","UserID":"","ClientOperationID":"old","Name":"Team","Key":"","OwnerTeamID":"","Visibility":"","InitialAdminUserID":"","MemberUserIDs":null,"Role":"","ExpectedRole":null,"ExpectedMember":false}`
	digest := sha256.Sum256([]byte(legacyJSON))
	if _, err = conn.Exec(ctx, `INSERT INTO radishnexus.workspace_configuration_receipts(workspace_id,actor_id,command_kind,scope_id,subject_id,client_operation_id,payload_sha256,audit_id,created_at) VALUES('wrk_main','usr_owner','team.create','wrk_main','','old',$1,'cfa_old',now())`, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	var before string
	if err = conn.QueryRow(ctx, `SELECT to_jsonb(a)::text FROM radishnexus.environment_deployment_authorizations a WHERE id='dpa_old'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// First establish the exact previous release schema, then prove 012 -> 013
	// preserves its authoritative configuration evidence and rejects old binaries.
	if err = applyMigration(ctx, conn, migrations[11]); err != nil {
		t.Fatal(err)
	}
	oldChecker := &ReadinessChecker{database: conn, expected: migrations[:12]}
	if err = oldChecker.CheckReady(ctx); err != nil {
		t.Fatal("012 not ready", err)
	}
	newChecker, err := NewReadinessChecker(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err = newChecker.CheckReady(ctx); err == nil {
		t.Fatal("new binary accepted 012")
	}
	var oldAudit, oldReceipt, oldActivity string
	if err = conn.QueryRow(ctx, `SELECT to_jsonb(a)::text FROM radishnexus.workspace_configuration_audit a WHERE id='cfa_old'`).Scan(&oldAudit); err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM radishnexus.workspace_configuration_receipts r WHERE audit_id='cfa_old'`).Scan(&oldReceipt); err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(ctx, `SELECT (to_jsonb(a)-'projection_version')::text FROM radishnexus.activity_items a WHERE event_id='evt_old' AND projection_version=3`).Scan(&oldActivity); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err = newChecker.CheckReady(ctx); err != nil {
		t.Fatal("013 not ready", err)
	}
	if err = oldChecker.CheckReady(ctx); err == nil {
		t.Fatal("old binary accepted 013")
	}
	for _, item := range []struct{ sql, want string }{
		{`SELECT to_jsonb(a)::text FROM radishnexus.workspace_configuration_audit a WHERE id='cfa_old'`, oldAudit},
		{`SELECT to_jsonb(r)::text FROM radishnexus.workspace_configuration_receipts r WHERE audit_id='cfa_old'`, oldReceipt},
		{`SELECT (to_jsonb(a)-'projection_version')::text FROM radishnexus.activity_items a WHERE event_id='evt_old'`, oldActivity},
	} {
		var got string
		if err = conn.QueryRow(ctx, item.sql).Scan(&got); err != nil || got != item.want {
			t.Fatal("013 rewrote legacy evidence", err)
		}
	}
	var mapped int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM radishnexus.repositories`).Scan(&mapped); err != nil || mapped != 0 {
		t.Fatal("upgrade invented repository mappings", err, mapped)
	}
	var after string
	var generation, version int
	if err = conn.QueryRow(ctx, `SELECT (to_jsonb(a)-'generation')::text,generation FROM radishnexus.environment_deployment_authorizations a WHERE id='dpa_old'`).Scan(&after, &generation); err != nil || before != after || generation != 1 {
		t.Fatal("upgrade rewrote grant provenance", err)
	}
	if err = conn.QueryRow(ctx, `SELECT projection_version FROM radishnexus.activity_items WHERE event_id='evt_old'`).Scan(&version); err != nil || version != goldenpath.ActivityProjectionVersion {
		t.Fatal("historical Timeline lost after upgrade", err, version)
	}
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := goldenpath.NewConfigurationService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	inv := goldenpath.Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_owner", WorkspaceID: "wrk_main"}, SourceKind: "web", CorrelationID: "upgrade"}
	r, err := s.Configure(ctx, inv, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: "wrk_main", ClientOperationID: "old", Name: "Team"})
	if err != nil || r.Created || r.Object.ID != "tem_old" {
		t.Fatal("legacy receipt stopped retrying", err, r)
	}
	_, err = s.Configure(ctx, inv, goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: "env_old", UserID: "usr_owner", ClientOperationID: "regrant", Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true, ExpectedAuthorization: &goldenpath.ConfigurationAuthorization{ID: "dpa_old", Status: "revoked"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(ctx, `SELECT generation FROM radishnexus.environment_deployment_authorizations WHERE status='active'`).Scan(&generation); err != nil || generation != 2 {
		t.Fatal("new generation", err)
	}
	var original string
	if err = conn.QueryRow(ctx, `SELECT authorization_id FROM radishnexus.deployments WHERE id='dpl_old'`).Scan(&original); err != nil || original != "dpa_old" {
		t.Fatal("historical Deployment changed", err)
	}
}
