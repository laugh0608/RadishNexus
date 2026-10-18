//go:build integration && (linux || darwin)

package postgres_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func TestJenkinsWorkerCrashChild(t *testing.T) {
	path := os.Getenv("RADISHNEXUS_WORKER_CRASH_CONFIG")
	if path == "" {
		return
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		os.Exit(31)
	}
	c, e := jenkins.LoadSpoolConfig(raw)
	if e != nil {
		os.Exit(32)
	}
	s, e := jenkins.OpenSpool(c, "run")
	if e != nil {
		os.Exit(33)
	}
	defer s.Close()
	sender, e := c.Sender()
	if e != nil {
		os.Exit(34)
	}
	cert, e := os.ReadFile(os.Getenv("RADISHNEXUS_WORKER_CRASH_CA"))
	if e != nil {
		os.Exit(35)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		os.Exit(36)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, e = s.Tick(ctx, sender, client); e != nil {
		os.Exit(37)
	}
	os.Exit(38) // Parent must kill us after DB commit and before any response.
}

func TestJenkinsWorkerPostgresCrashRecovery(t *testing.T) {
	ctx, pool, core, _, _, component := ticketComponentFixture(t)
	cid := component("worker-crash")
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	write := func(name string, raw []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if e := os.WriteFile(p, raw, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	key := write("key", []byte(strings.Repeat("ab", 32)))
	sources, e := jenkins.LoadSources([]byte(fmt.Sprintf(`{"version":1,"sources":[{"source_id":"durable_test","workspace_id":"wrk_main","component_id":%q,"job_full_name":"example/build","keys":[{"key_id":"key_a","secret_file":%q}]}]}`, cid, key)), jenkins.ReadFile)
	if e != nil {
		t.Fatal(e)
	}
	var handler http.Handler
	var killFirst atomic.Bool
	killFirst.Store(true)
	child := make(chan *os.Process, 1)
	committed := make(chan string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured := httptest.NewRecorder()
		handler.ServeHTTP(captured, r)
		if captured.Code == 201 && killFirst.CompareAndSwap(true, false) {
			var body struct {
				Data jenkins.SendResult `json:"data"`
			}
			if json.Unmarshal(captured.Body.Bytes(), &body) != nil {
				t.Error("bad receiver response")
				return
			}
			committed <- body.Data.CIRunID
			process := <-child
			if e := process.Kill(); e != nil {
				t.Error("failed to kill test worker")
			}
			return
		}
		for k, v := range captured.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(captured.Code)
		_, _ = w.Write(captured.Body.Bytes())
	}))
	defer server.Close()
	policy, _ := httptransport.NewBrowserSessionPolicy(server.URL)
	proxy, _ := httptransport.NewTrustedProxyPolicy("127.0.0.1/32")
	handler = httptransport.WithRequestID(httptransport.NewJenkinsDeliveryHandler(sources, core, policy, proxy, time.Now, func(httptransport.JenkinsDeliveryLog) {}))
	senderFile := write("sender.json", []byte(fmt.Sprintf(`{"version":1,"origin":%q,"source_id":"durable_test","key_id":"key_a","secret_file":%q}`, server.URL, key)))
	c := jenkins.SpoolConfig{Version: 1, SenderFile: senderFile, InputDir: filepath.Join(dir, "input"), StateDir: filepath.Join(dir, "state"), AckDir: filepath.Join(dir, "acks"), Binding: jenkins.SpoolBinding{Version: 1, ReceiverID: "isolated_receiver", JenkinsID: "synthetic_controller", Origin: server.URL, SourceID: "durable_test", WorkspaceID: "wrk_main", ComponentID: cid, Job: "example/build", FirstBuild: 1}}
	if e = os.Mkdir(c.InputDir, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := jenkins.OpenSpool(c, "init")
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	payload := func(n int, result string) []byte {
		return []byte(fmt.Sprintf(`{"version":1,"job_full_name":"example/build","build_number":%d,"building":false,"in_progress":false,"result":%q,"started_at":null,"completed_at":"2026-09-26T02:01:00Z"}`, n, result))
	}
	if e = os.WriteFile(filepath.Join(c.InputDir, "build-1.json"), payload(1, "SUCCESS"), 0600); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c)
	config := write("worker.json", raw)
	ca := write("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	command := exec.Command(os.Args[0], "-test.run=^TestJenkinsWorkerCrashChild$")
	command.Env = append(os.Environ(), "RADISHNEXUS_WORKER_CRASH_CONFIG="+config, "RADISHNEXUS_WORKER_CRASH_CA="+ca)
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	child <- command.Process
	if e = command.Wait(); e == nil {
		t.Fatal("worker was not interrupted")
	}
	var original string
	select {
	case original = <-committed:
	case <-time.After(time.Second):
		t.Fatal("worker did not reach real commit")
	}
	s, e = jenkins.OpenSpool(c, "run")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	state, e := s.Status(1)
	if e != nil || state.Item.State != "in_flight" || state.Item.Rounds != 1 {
		t.Fatal(state, e)
	}
	sender, e := c.Sender()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Tick(ctx, sender, server.Client()); e != nil {
		t.Fatal(e)
	}
	state, e = s.Status(1)
	if e != nil || state.Item.State != "delivered" || state.Item.Rounds != 2 || state.Item.CIRunID != original {
		t.Fatal(state, e)
	}
	for index, result := range []string{"FAILURE", "ABORTED"} {
		n := index + 2
		if e = os.WriteFile(filepath.Join(c.InputDir, fmt.Sprintf("build-%d.json", n)), payload(n, result), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Tick(ctx, sender, server.Client()); e != nil {
			t.Fatal(e)
		}
	}
	for _, query := range []string{
		`SELECT count(*) FROM radishnexus.ci_runs WHERE source_id='durable_test'`,
		`SELECT count(*) FROM radishnexus.inbound_deliveries WHERE source_id='durable_test'`,
		`SELECT count(*) FROM radishnexus.domain_events WHERE source_id='durable_test'`,
		`SELECT count(*) FROM radishnexus.activity_items WHERE event_id IN (SELECT event_id FROM radishnexus.inbound_deliveries WHERE source_id='durable_test')`,
		`SELECT count(*) FROM radishnexus.outbox_deliveries WHERE event_id IN (SELECT event_id FROM radishnexus.inbound_deliveries WHERE source_id='durable_test')`,
	} {
		var count int
		if e = pool.QueryRow(ctx, query).Scan(&count); e != nil || count != 3 {
			t.Fatal("nonunique facts", count, e)
		}
	}
	var deployments int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.deployments`).Scan(&deployments); e != nil || deployments != 0 {
		t.Fatal("worker created Deployment", e)
	}
	if _, e = core.GetNexusView(ctx, principal("usr_contributor"), entityref.Ref{Type: "ci-run", ID: original}); e != nil {
		t.Fatal("fact unreadable", e)
	}
}
