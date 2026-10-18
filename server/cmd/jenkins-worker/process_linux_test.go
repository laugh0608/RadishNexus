//go:build linux

package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
)

func TestWorkerForegroundChild(t *testing.T) {
	config := os.Getenv("RADISHNEXUS_FOREGROUND_TEST_CONFIG")
	if config == "" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	e := run(ctx, []string{"run", "-config", config}, os.Stdout)
	if e != nil && e != context.Canceled {
		os.Exit(31)
	}
	os.Exit(0)
}

func TestWorkerForegroundTLSAndGracefulStop(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, raw []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if e := os.WriteFile(p, raw, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	key := write("key", []byte(strings.Repeat("ac", 32)))
	sources, e := jenkins.LoadSources([]byte(fmt.Sprintf(`{"version":1,"sources":[{"source_id":"source","workspace_id":"wrk_test","component_id":"cmp_test","job_full_name":"example/build","keys":[{"key_id":"current","secret_file":%q}]}]}`, key)), jenkins.ReadFile)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte(`{"version":1,"job_full_name":"example/build","build_number":1,"building":false,"in_progress":false,"result":"SUCCESS","started_at":null,"completed_at":"2026-09-26T02:01:00Z"}`)
		if !sources[0].Verify(r.Header.Get("X-Nexus-Key-ID"), r.Header.Get("X-Nexus-Delivery-ID"), r.Header.Get("X-Nexus-Timestamp"), r.Header.Get("X-Nexus-Signature"), body, time.Now()) {
			t.Error("bad signature")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"data":{"ci_run_id":"cir_cli","duplicate":false}}`))
	}))
	defer server.Close()
	ca := write("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	sender := write("sender.json", []byte(fmt.Sprintf(`{"version":1,"origin":%q,"source_id":"source","key_id":"current","secret_file":%q}`, server.URL, key)))
	c := jenkins.SpoolConfig{Version: 1, SenderFile: sender, InputDir: filepath.Join(dir, "input"), StateDir: filepath.Join(dir, "state"), AckDir: filepath.Join(dir, "acks"), Binding: jenkins.SpoolBinding{Version: 1, ReceiverID: "receiver", JenkinsID: "jenkins", Origin: server.URL, SourceID: "source", WorkspaceID: "wrk_test", ComponentID: "cmp_test", Job: "example/build", FirstBuild: 1}}
	os.Mkdir(c.InputDir, 0700)
	s, e := jenkins.OpenSpool(c, "init")
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	if e = os.WriteFile(filepath.Join(c.InputDir, "build-1.json"), []byte(`{"version":1,"job_full_name":"example/build","build_number":1,"building":false,"in_progress":false,"result":"SUCCESS","started_at":null,"completed_at":"2026-09-26T02:01:00Z"}`), 0600); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c)
	config := write("worker.json", raw)
	command := exec.Command(os.Args[0], "-test.run=^TestWorkerForegroundChild$")
	command.Env = append(os.Environ(), "RADISHNEXUS_FOREGROUND_TEST_CONFIG="+config, "SSL_CERT_FILE="+ca, "SSL_CERT_DIR="+filepath.Join(dir, "no-extra-roots"))
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	defer command.Process.Kill()
	reader, e := jenkins.OpenSpool(c, "inspect")
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, e := reader.Status(1)
		if e == nil && status.Item.State == "delivered" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("CLI did not deliver", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e = command.Process.Signal(syscall.SIGTERM); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CLI ignored cancellation")
	}
	s, e = jenkins.OpenSpool(c, "run")
	if e != nil {
		t.Fatal("lock not released", e)
	}
	s.Close()
}
