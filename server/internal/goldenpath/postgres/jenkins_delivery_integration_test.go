//go:build integration

package postgres_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func assertJenkinsDeliveryHTTP(t *testing.T, ctx context.Context, pool *pgxpool.Pool, auth *authn.Service, sessionToken string) {
	t.Helper()
	store := goldenpostgres.New(pool)
	service := goldenpath.NewService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	readKey := func(string, int64) ([]byte, error) { return []byte(strings.Repeat("cd", 32)), nil }
	loadSources := func(component string) []jenkins.Source {
		t.Helper()
		raw := fmt.Sprintf(`{"version":1,"sources":[{"source_id":"http_jenkins","workspace_id":"wrk_main","component_id":%q,"job_full_name":"example/build","keys":[{"key_id":"key_a","secret_file":"/synthetic/key"}]}]}`, component)
		sources, e := jenkins.LoadSources([]byte(raw), readKey)
		if e != nil {
			t.Fatal(e)
		}
		return sources
	}
	session, _ := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := httptransport.NewTrustedProxyPolicy("10.0.0.0/8")
	makeHandler := func(app httptransport.JenkinsRecorder, component string) http.Handler {
		return httptransport.WithRequestID(httptransport.NewJenkinsDeliveryHandler(loadSources(component), app, session, proxy, time.Now, func(httptransport.JenkinsDeliveryLog) {}))
	}
	handler := makeHandler(service, "cmp_auth")
	body := func(number int, result string) string {
		return fmt.Sprintf(`{"version":1,"job_full_name":"example/build","build_number":%d,"building":false,"in_progress":false,"result":%q,"started_at":null,"completed_at":"2026-08-28T12:00:00Z"}`, number, result)
	}
	request := func(raw string, number int) *http.Request {
		r := httptest.NewRequest("POST", "https://nexus.example.test"+jenkins.Path("http_jenkins"), strings.NewReader(raw))
		stamp := fmt.Sprint(time.Now().Unix())
		delivery := fmt.Sprintf("build-%d", number)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Nexus-Key-ID", "key_a")
		r.Header.Set("X-Nexus-Delivery-ID", delivery)
		r.Header.Set("X-Nexus-Timestamp", stamp)
		sum := sha256.Sum256([]byte(raw))
		key, _ := hex.DecodeString(strings.Repeat("cd", 32))
		mac := hmac.New(sha256.New, key)
		fmt.Fprint(mac, strings.Join([]string{"radishnexus-jenkins-v1", "POST", r.URL.Path, "key_a", delivery, stamp, hex.EncodeToString(sum[:])}, "\n"))
		r.Header.Set("X-Nexus-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
		return r
	}
	deliver := func(h http.Handler, raw string, number int) (int, jenkins.SendResult) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request(raw, number))
		var data struct {
			Data jenkins.SendResult `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &data)
		return w.Code, data.Data
	}
	var deploymentsBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.deployments`).Scan(&deploymentsBefore); err != nil {
		t.Fatal(err)
	}
	code, first := deliver(handler, body(42, "SUCCESS"), 42)
	if code != 201 || first.CIRunID == "" || first.Duplicate {
		t.Fatal(code, first)
	}
	code, duplicate := deliver(handler, "  "+body(42, "SUCCESS")+"\n", 42)
	if code != 200 || duplicate.CIRunID != first.CIRunID || !duplicate.Duplicate {
		t.Fatal(code, duplicate)
	}
	code, _ = deliver(handler, body(42, "FAILURE"), 42)
	if code != 409 {
		t.Fatal("content conflict", code)
	}
	// Concurrent identical requests still produce one CI Run and one Activity.
	var group sync.WaitGroup
	codes := make(chan int, 2)
	ids := make(chan string, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			code, result := deliver(handler, body(43, "SUCCESS"), 43)
			codes <- code
			ids <- result.CIRunID
		}()
	}
	group.Wait()
	a, b := <-codes, <-codes
	if a+b != 401 {
		t.Fatal("concurrent delivery", a, b)
	}
	if <-ids != <-ids {
		t.Fatal("concurrent duplicate identity")
	}
	// A missing or cross-workspace binding never leaves a receipt.
	if err := store.ValidateJenkinsBinding(ctx, "wrk_main", "cmp_auth"); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateJenkinsBinding(ctx, "wrk_other", "cmp_auth"); err == nil {
		t.Fatal("startup cross-workspace binding accepted")
	}
	bad := loadSources("cmp_auth")
	bad[0].WorkspaceID = "wrk_other"
	cross := httptransport.WithRequestID(httptransport.NewJenkinsDeliveryHandler(bad, service, session, proxy, time.Now, func(httptransport.JenkinsDeliveryLog) {}))
	if code, _ := deliver(cross, body(42, "SUCCESS"), 42); code != 404 {
		t.Fatal("cross-workspace", code)
	}
	if code, _ := deliver(makeHandler(service, "cmp_missing"), body(50, "SUCCESS"), 50); code != 404 {
		t.Fatal("missing target", code)
	}
	for _, result := range []string{"UNSTABLE", "NOT_BUILT"} {
		if code, _ := deliver(handler, body(50, result), 50); code != 422 {
			t.Fatal(result, code)
		}
	}
	badRequest := request(body(50, "SUCCESS"), 50)
	badRequest.Header.Set("X-Nexus-Signature", "v1=invalid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, badRequest)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	// Event insertion failure rolls back receipt, CI Run, outbox and Activity.
	var eventID string
	if err := pool.QueryRow(ctx, `SELECT event_id FROM radishnexus.inbound_deliveries WHERE source_id='http_jenkins' AND delivery_id='build-42'`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	broken := goldenpath.NewService(store, &fixedIDs{values: []string{"cir_jenkins_rollback", eventID, "cor_jenkins_rollback"}}, goldenpath.SystemClock{})
	if code, _ := deliver(makeHandler(broken, "cmp_auth"), body(50, "SUCCESS"), 50); code != 409 {
		t.Fatal("atomic failure", code)
	}
	var residue int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM radishnexus.inbound_deliveries WHERE source_id='http_jenkins' AND delivery_id='build-50')+(SELECT count(*) FROM radishnexus.ci_runs WHERE id='cir_jenkins_rollback')`).Scan(&residue); err != nil || residue != 0 {
		t.Fatal("failed transaction residue", residue, err)
	}
	if code, _ := deliver(handler, body(50, "SUCCESS"), 50); code != 201 {
		t.Fatal("recovery", code)
	}
	// After commit, lose the actual TLS response. Sender retries the same identity.
	var tlsHandler http.Handler
	var attempts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured := httptest.NewRecorder()
		tlsHandler.ServeHTTP(captured, r)
		if attempts.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		for key, values := range captured.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(captured.Code)
		_, _ = w.Write(captured.Body.Bytes())
	}))
	defer server.Close()
	tlsPolicy, _ := httptransport.NewBrowserSessionPolicy(server.URL)
	tlsHandler = httptransport.WithRequestID(httptransport.NewJenkinsDeliveryHandler(loadSources("cmp_auth"), service, tlsPolicy, proxy, time.Now, func(httptransport.JenkinsDeliveryLog) {}))
	sender, err := jenkins.LoadSender([]byte(fmt.Sprintf(`{"version":1,"origin":%q,"source_id":"http_jenkins","key_id":"key_a","secret_file":"/synthetic/key"}`, server.URL)), readKey)
	if err != nil {
		t.Fatal(err)
	}
	lost, err := jenkins.Send(ctx, sender, []byte(body(44, "SUCCESS")), server.Client())
	if err != nil || !lost.Duplicate || attempts.Load() != 2 {
		t.Fatal("lost response recovery", lost, err, attempts.Load())
	}
	for n, result := range map[int]string{45: "FAILURE", 46: "ABORTED"} {
		if code, _ := deliver(handler, body(n, result), n); code != 201 {
			t.Fatal(result, code)
		}
	}
	// Normal transaction projection is readable immediately, without rebuild.
	view := httptransport.WithRequestID(httptransport.NewCIRunNexusViewHandler(auth, service, session, proxy))
	r := httptest.NewRequest("GET", "https://nexus.example.test/api/v1/workspaces/wrk_main/ci-runs/"+first.CIRunID+"/nexus-view", nil)
	r.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: sessionToken})
	w = httptest.NewRecorder()
	view.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"activity_type":"ci-run.recorded"`) || strings.Contains(w.Body.String(), "http_jenkins") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, query := range []string{
		`SELECT count(*) FROM radishnexus.ci_runs WHERE source_id='http_jenkins'`,
		`SELECT count(*) FROM radishnexus.inbound_deliveries WHERE source_id='http_jenkins'`,
		`SELECT count(*) FROM radishnexus.domain_events WHERE source_id='http_jenkins'`,
		`SELECT count(*) FROM radishnexus.activity_items WHERE event_id IN (SELECT event_id FROM radishnexus.inbound_deliveries WHERE source_id='http_jenkins')`,
		`SELECT count(*) FROM radishnexus.outbox_deliveries WHERE event_id IN (SELECT event_id FROM radishnexus.inbound_deliveries WHERE source_id='http_jenkins')`,
	} {
		var count int
		if err := pool.QueryRow(ctx, query).Scan(&count); err != nil || count != 6 {
			t.Fatal(query, count, err)
		}
	}
	assertRealJenkinsLabSnapshots(t, ctx, pool, auth, sessionToken)
	var deploymentsAfter int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.deployments`).Scan(&deploymentsAfter); err != nil || deploymentsAfter != deploymentsBefore {
		t.Fatal("unexpected deployment", deploymentsAfter, err)
	}
}
