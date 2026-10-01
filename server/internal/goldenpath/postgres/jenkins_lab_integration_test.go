//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

// Opt-in evidence path: consumes only controller-finalized snapshots collected
// by experiments/jenkins-lab. Ordinary CI never synthesizes those files.
func assertRealJenkinsLabSnapshots(t *testing.T, ctx context.Context, pool *pgxpool.Pool, auth *authn.Service, sessionToken string) {
	t.Helper()
	directory := os.Getenv("RADISHNEXUS_JENKINS_LAB_SNAPSHOTS")
	if directory == "" {
		return
	}
	if !filepath.IsAbs(directory) {
		t.Fatal("Jenkins lab snapshot directory must be absolute")
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal("generate lab signing key")
	}
	readKey := func(string, int64) ([]byte, error) { return []byte(hex.EncodeToString(key[:])), nil }
	sources, err := jenkins.LoadSources([]byte(`{"version":1,"sources":[{"source_id":"real_jenkins_lab","workspace_id":"wrk_main","component_id":"cmp_auth","job_full_name":"nexus-ci-probe","keys":[{"key_id":"lab_key","secret_file":"/synthetic/in-memory-key"}]}]}`), readKey)
	if err != nil {
		t.Fatal(err)
	}
	service := goldenpath.NewService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	var receiver http.Handler
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { receiver.ServeHTTP(w, r) }))
	defer server.Close()
	session, _ := httptransport.NewBrowserSessionPolicy(server.URL)
	proxy, _ := httptransport.NewTrustedProxyPolicy("127.0.0.1/32")
	receiver = httptransport.WithRequestID(httptransport.NewJenkinsDeliveryHandler(sources, service, session, proxy, time.Now, func(httptransport.JenkinsDeliveryLog) {}))
	sender, err := jenkins.LoadSender([]byte(fmt.Sprintf(`{"version":1,"origin":%q,"source_id":"real_jenkins_lab","key_id":"lab_key","secret_file":"/synthetic/in-memory-key"}`, server.URL)), readKey)
	if err != nil {
		t.Fatal(err)
	}
	var deploymentsBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.deployments`).Scan(&deploymentsBefore); err != nil {
		t.Fatal(err)
	}
	viewPolicy, _ := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	view := httptransport.WithRequestID(httptransport.NewCIRunNexusViewHandler(auth, service, viewPolicy, proxy))
	for index, want := range []string{"SUCCESS", "FAILURE", "ABORTED"} {
		raw, err := jenkins.ReadFile(filepath.Join(directory, fmt.Sprintf("build-%d.json", index+1)), jenkins.MaxBody)
		if err != nil {
			t.Fatal("missing or oversized finalized snapshot")
		}
		payload, err := jenkins.ParsePayload(raw, time.Now())
		if err != nil || payload.Result != want || payload.Job != "nexus-ci-probe" || payload.Number != int64(index+1) || payload.StartedAt == nil {
			t.Fatal("finalized Jenkins snapshot does not match expected probe")
		}
		first, err := jenkins.Send(ctx, sender, raw, server.Client())
		if err != nil || first.Duplicate {
			t.Fatal("real Jenkins delivery failed", err)
		}
		duplicate, err := jenkins.Send(ctx, sender, raw, server.Client())
		if err != nil || duplicate != (jenkins.SendResult{CIRunID: first.CIRunID, Duplicate: true}) {
			t.Fatal("real Jenkins duplicate recovery failed", err)
		}
		r := httptest.NewRequest("GET", "https://nexus.example.test/api/v1/workspaces/wrk_main/ci-runs/"+first.CIRunID+"/nexus-view", nil)
		r.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: sessionToken})
		w := httptest.NewRecorder()
		view.ServeHTTP(w, r)
		status := map[string]string{"SUCCESS": "succeeded", "FAILURE": "failed", "ABORTED": "canceled"}[want]
		var document struct {
			Data struct {
				Current struct {
					Status string `json:"status"`
				} `json:"current"`
				Timeline []json.RawMessage `json:"timeline"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &document) != nil || w.Code != 200 || document.Data.Current.Status != status || len(document.Data.Timeline) != 1 || strings.Contains(w.Body.String(), "real_jenkins_lab") {
			t.Fatal("real Jenkins Session read mismatch")
		}
		t.Logf("real Jenkins finalized build %d (%s) -> %s, duplicate and Session read verified", payload.Number, want, first.CIRunID)
	}
	for _, query := range []string{
		`SELECT count(*) FROM radishnexus.ci_runs WHERE source_id='real_jenkins_lab'`,
		`SELECT count(*) FROM radishnexus.inbound_deliveries WHERE source_id='real_jenkins_lab'`,
		`SELECT count(*) FROM radishnexus.domain_events WHERE source_id='real_jenkins_lab'`,
		`SELECT count(*) FROM radishnexus.activity_items WHERE event_id IN (SELECT event_id FROM radishnexus.inbound_deliveries WHERE source_id='real_jenkins_lab')`,
	} {
		var count int
		if err := pool.QueryRow(ctx, query).Scan(&count); err != nil || count != 3 {
			t.Fatal("real Jenkins fact count mismatch", count, err)
		}
	}
	var deploymentsAfter int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.deployments`).Scan(&deploymentsAfter); err != nil || deploymentsAfter != deploymentsBefore {
		t.Fatal("real Jenkins build created Deployment")
	}
}
