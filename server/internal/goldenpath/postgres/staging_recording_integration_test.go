//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func stagingDatabase(t *testing.T) (context.Context, *pgxpool.Pool, *goldenpath.Service, func(string) string) {
	// Reuse the isolated, migrated Golden Path database fixture.
	ctx, pool := documentDatabase(t)
	seedDeploymentTargets(t, ctx, pool)
	_, err := pool.Exec(ctx, `INSERT INTO radishnexus.components(id,workspace_id,key,name,type,owner_team_id,lifecycle,created_by_kind,created_by_id) VALUES('cmp_stage','wrk_main','STAGE','Stage service','service','tem_main','active','user','usr_admin')`)
	if err != nil {
		t.Fatal(err)
	}
	s := goldenpath.NewService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	number := 0
	run := func(status string) string {
		t.Helper()
		number++
		key := fmt.Sprintf("stage-%d", number)
		v, e := s.RecordCompletedJenkinsRun(ctx, goldenpath.VerifiedJenkinsDelivery{WorkspaceID: "wrk_main", SourceID: "staging-tests", DeliveryID: key, PayloadSHA256: strings.Repeat("a", 64)}, goldenpath.RecordCompletedCIRunInput{ComponentID: "cmp_stage", ExternalRunKey: key, Status: status, CompletedAt: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)})
		if e != nil {
			t.Fatal(e)
		}
		return v.CIRun.ID
	}
	return ctx, pool, s, run
}
func stagingInput(ci string) goldenpath.RecordStagingDeploymentInput {
	return goldenpath.RecordStagingDeploymentInput{ClientOperationID: "staging-operation", Confirmed: true, EnvironmentID: "env_staging", CIRunID: ci, Status: "succeeded", CompletedAt: time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)}
}
func TestStagingRecordingHTTPAndConcurrentRetry(t *testing.T) {
	ctx, pool, s, run := stagingDatabase(t)
	inv := invocation(principal("usr_contributor"), "record-staging")
	// Real Session/CSRF validation against the migrated database.
	token, csrf := deploymentHTTPToken(31), deploymentHTTPToken(32)
	td, cd := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(csrf))
	now := time.Now()
	if _, err := pool.Exec(ctx, `INSERT INTO radishnexus.user_sessions(id,user_id,token_digest,csrf_token_digest,created_at,expires_at) VALUES('ses_stage','usr_contributor',$1,$2,$3,$4)`, td[:], cd[:], now, now.Add(authn.SessionLifetime)); err != nil {
		t.Fatal(err)
	}
	auth := authn.NewService(authpostgres.New(pool), nil, nil, fixedClock{now: now})
	policy, _ := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := httptransport.NewTrustedProxyPolicy("127.0.0.1/32")
	h := httptransport.WithRequestID(httptransport.NewStagingDeploymentHandler(auth, s, policy, proxy))
	request := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, "https://nexus.example.test/api/v1/workspaces/wrk_main/ci-runs/"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: token})
		r.AddCookie(&http.Cookie{Name: httptransport.CSRFCookieName, Value: csrf})
		r.Header.Set(httptransport.CSRFHeaderName, csrf)
		r.Header.Set("Origin", "https://nexus.example.test")
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	for _, status := range []string{"succeeded", "failed", "canceled"} {
		id := run("succeeded")
		body := fmt.Sprintf(`{"client_operation_id":"http-%s","environment_id":"env_staging","status":%q,"started_at":null,"completed_at":"2026-09-26T01:00:00Z","confirmed":true}`, status, status)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request("GET", id+"/staging-targets?limit=1", ""))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "env_staging") || strings.Contains(w.Body.String(), "env_production") {
			t.Fatal("targets", w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, request("POST", id+"/staging-deployments", body))
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				Deployment entityref.Ref `json:"deployment"`
				Duplicate  bool          `json:"duplicate"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		// Discard the first response and recover the authoritative result with the same request.
		again := httptest.NewRecorder()
		h.ServeHTTP(again, request("POST", id+"/staging-deployments", body))
		if again.Code != 200 || !strings.Contains(again.Body.String(), out.Data.Deployment.ID) || !strings.Contains(again.Body.String(), `"duplicate":true`) {
			t.Fatal("recovery", again.Code, again.Body.String())
		}
		view := httptransport.WithRequestID(httptransport.NewDeploymentNexusViewHandler(auth, s, policy, proxy))
		read := httptest.NewRecorder()
		view.ServeHTTP(read, deploymentHTTPRequest("wrk_main", out.Data.Deployment.ID, token))
		if read.Code != 200 || !strings.Contains(read.Body.String(), `"status":"`+status+`"`) {
			t.Fatal("read", read.Code, read.Body.String())
		}
		v, err := s.GetNexusView(ctx, inv.Principal, out.Data.Deployment)
		if err != nil || len(v.Timeline) != 1 || len(v.Relations) != 1 {
			t.Fatal("activity", v, err)
		}
	}
	input := stagingInput(run("succeeded"))
	var wg sync.WaitGroup
	results := make(chan goldenpath.Deployment, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); d, e := s.RecordStagingDeployment(ctx, inv, input); results <- d; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	created := 0
	id := ""
	for d := range results {
		if !d.Duplicate {
			created++
		}
		if id != "" && id != d.ID {
			t.Fatal("duplicate identities")
		}
		id = d.ID
	}
	if created != 1 {
		t.Fatal("new writes", created)
	}
	changed := input
	changed.Status = "failed"
	if _, err := s.RecordStagingDeployment(ctx, inv, changed); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("changed payload", err)
	}
	changed = input
	changed.ClientOperationID = "other-op"
	if _, err := s.RecordStagingDeployment(ctx, inv, changed); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("different operation", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO radishnexus.environment_deployment_authorizations(id,workspace_id,environment_id,user_id,granted_by) VALUES('dpa_reader','wrk_main','env_staging','usr_reader','usr_admin')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordStagingDeployment(ctx, invocation(principal("usr_reader"), "reader"), input); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("different actor", err)
	}
	for _, status := range []string{"failed", "canceled"} {
		v := stagingInput(run(status))
		if _, err := s.RecordStagingDeployment(ctx, inv, v); !errors.Is(err, authz.ErrConflict) {
			t.Fatal("invalid source", err)
		}
	}
	for _, query := range []string{`SELECT count(*) FROM radishnexus.deployments`, `SELECT count(*) FROM radishnexus.collaboration_command_receipts WHERE command_kind='deployment.record'`, `SELECT count(*) FROM radishnexus.domain_events WHERE event_type='deployment.recorded'`, `SELECT count(*) FROM radishnexus.activity_items WHERE activity_type='deployment.recorded'`} {
		var n int
		if err := pool.QueryRow(ctx, query).Scan(&n); err != nil || n != 4 {
			t.Fatal("unique facts", n, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.environment_deployment_authorizations SET status='revoked',revoked_by='usr_admin',revoked_at=now() WHERE id='dpa_staging_contributor'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordStagingDeployment(ctx, inv, input); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("revoked replay", err)
	}
	if page, err := s.ListStagingTargets(ctx, inv.Principal, input.CIRunID, goldenpath.DiscoveryPageInput{Limit: 1}); err != nil || len(page.Items) != 0 {
		t.Fatal("revoked targets", page, err)
	}
	if _, err := s.GetNexusView(ctx, inv.Principal, entityref.Ref{Type: "deployment", ID: id}); err != nil {
		t.Fatal("revocation removed history", err)
	}
}
func TestStagingRecordingWaitsForRevocationAndArchive(t *testing.T) {
	for _, scenario := range []struct {
		name, sql, wait string
		want            error
	}{
		{"authorization", `UPDATE radishnexus.environment_deployment_authorizations SET status='revoked',revoked_by='usr_admin',revoked_at=now() WHERE id='dpa_staging_contributor'`, "%FROM radishnexus.environment_deployment_authorizations%", authz.ErrForbidden},
		{"archive", `UPDATE radishnexus.environments SET status='archived' WHERE id='env_staging'`, "%FROM radishnexus.environments%", authz.ErrConflict},
		{"membership", `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id='wrk_main' AND user_id='usr_contributor'`, "%FROM radishnexus.workspace_memberships%", authz.ErrNotFound},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, pool, s, run := stagingDatabase(t)
			input := stagingInput(run("succeeded"))
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, scenario.sql); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, e := s.RecordStagingDeployment(ctx, invocation(principal("usr_contributor"), "race"), input)
				done <- e
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting bool
				err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1)`, scenario.wait).Scan(&waiting)
				if err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("command did not wait")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-done; !errors.Is(err, scenario.want) {
				t.Fatal("race crossed authorization", err)
			}
			assertTableCount(t, ctx, pool, "radishnexus.deployments", 0)
			assertTableCount(t, ctx, pool, "radishnexus.collaboration_command_receipts", 0)
		})
	}
}

func TestStagingTargetsPaginationAndScope(t *testing.T) {
	ctx, pool, s, run := stagingDatabase(t)
	id := run("succeeded")
	p := principal("usr_contributor")
	if _, err := pool.Exec(ctx, `INSERT INTO radishnexus.environments(id,workspace_id,key,name,classification,owner_team_id,status,created_by_kind,created_by_id) VALUES
 ('env_a','wrk_main','A','Stage A','staging','tem_main','active','user','usr_admin'),
 ('env_b','wrk_main','B','Stage B','staging','tem_main','active','user','usr_admin'),
 ('env_hidden','wrk_main','H','No authorization','staging','tem_main','active','user','usr_admin');
 INSERT INTO radishnexus.environment_deployment_authorizations(id,workspace_id,environment_id,user_id,granted_by) VALUES
 ('dpa_a','wrk_main','env_a','usr_contributor','usr_admin'),('dpa_b','wrk_main','env_b','usr_contributor','usr_admin');`); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListStagingTargets(ctx, p, id, goldenpath.DiscoveryPageInput{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].Ref.ID != "env_a" || first.NextID != "env_a" {
		t.Fatal(first, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.environment_deployment_authorizations SET status='revoked',revoked_by='usr_admin',revoked_at=now() WHERE id='dpa_b'`); err != nil {
		t.Fatal(err)
	}
	second, err := s.ListStagingTargets(ctx, p, id, goldenpath.DiscoveryPageInput{Limit: 1, AfterID: first.NextID})
	if err != nil || len(second.Items) != 1 || second.Items[0].Ref.ID != "env_staging" || second.NextID != "" {
		t.Fatal(second, err)
	}
	empty, err := s.ListStagingTargets(ctx, principal("usr_admin"), id, goldenpath.DiscoveryPageInput{Limit: 50})
	if err != nil || len(empty.Items) != 0 {
		t.Fatal("admin implicitly authorized", empty, err)
	}
	other := p
	other.WorkspaceID = "wrk_other"
	if _, err = s.ListStagingTargets(ctx, other, id, goldenpath.DiscoveryPageInput{Limit: 1}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("cross workspace", err)
	}
	if _, err = s.ListStagingTargets(ctx, p, run("failed"), goldenpath.DiscoveryPageInput{Limit: 1}); !errors.Is(err, authz.ErrConflict) {
		t.Fatal("failed source targets", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id='wrk_main' AND user_id='usr_contributor'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ListStagingTargets(ctx, p, id, goldenpath.DiscoveryPageInput{Limit: 1}); !errors.Is(err, authz.ErrNotFound) {
		t.Fatal("suspended", err)
	}
}
