//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
)

// This is a CLI test over an aged, synthetic on-disk receipt. It makes no
// network request and cannot bypass the production retention clock.
func TestWorkerCleanupCLIRequiresConfirmationAndKeepsReceipt(t *testing.T) {
	root := t.TempDir()
	c := jenkins.SpoolConfig{Version: 1, SenderFile: filepath.Join(root, "unprovisioned.json"), InputDir: filepath.Join(root, "input"), StateDir: filepath.Join(root, "state"), AckDir: filepath.Join(root, "ack"),
		Binding: jenkins.SpoolBinding{Version: 1, ReceiverID: "receiver", JenkinsID: "jenkins", Origin: "https://nexus.invalid", SourceID: "source", WorkspaceID: "wrk_test", ComponentID: "cmp_test", Job: "example/build", FirstBuild: 1}}
	write := func(path string, value any) {
		t.Helper()
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Mkdir(c.InputDir, 0700); e != nil {
		t.Fatal(e)
	}
	config := filepath.Join(root, "config.json")
	write(config, c)
	var out bytes.Buffer
	if e := run(context.Background(), []string{"init", "-config", config}, &out); e != nil {
		t.Fatal(e)
	}
	created := time.Now().UTC().Add(-9 * 24 * time.Hour)
	payload := []byte(fmt.Sprintf(`{"version":1,"job_full_name":"example/build","build_number":1,"building":false,"in_progress":false,"result":"SUCCESS","started_at":null,"completed_at":%q}`, created.Add(-time.Minute).Truncate(time.Millisecond).Format(time.RFC3339Nano)))
	bound, _ := json.Marshal(c.Binding)
	bindingHash := sha256.Sum256(bound)
	payloadHash := sha256.Sum256(payload)
	digest := hex.EncodeToString(payloadHash[:])
	recordPath := filepath.Join(c.StateDir, "build-1.json")
	write(recordPath, map[string]any{"version": 1, "binding_sha256": hex.EncodeToString(bindingHash[:]), "build_number": 1, "payload": payload, "payload_sha256": digest,
		"state": "delivered", "reason": "", "disposition": "", "last_http_status": 0, "rounds": 1, "first_attempt": created, "next_attempt": created,
		"created_at": created, "updated_at": created, "delivered_at": created, "ci_run_id": "cir_cleanup_cli", "manual_retries": []string{}})
	ack := fmt.Sprintf(`{"version":1,"source_id":"source","build_number":1,"payload_sha256":%q}`, digest)
	if e := os.WriteFile(filepath.Join(c.AckDir, "build-1.json"), []byte(ack), 0600); e != nil {
		t.Fatal(e)
	}
	proof := filepath.Join(root, "collector")
	if e := os.Mkdir(proof, 0700); e != nil {
		t.Fatal(e)
	}
	write(filepath.Join(proof, "manifest.json"), c.Binding)
	handoff := created.Add(time.Minute)
	retired := handoff.Add(7 * 24 * time.Hour)
	write(filepath.Join(proof, "checkpoint.json"), map[string]any{"version": 2, "binding": c.Binding, "prefix": 1, "high": 1, "scan_high": 1, "cursor": 1, "observed_at": retired, "last_sweep_at": handoff, "lifecycle": "stopped", "error": "",
		"entries": map[string]any{"1": map[string]any{"kind": "retired", "digest": digest, "handoff_at": handoff, "retired_at": retired}}})
	before, _ := os.ReadFile(recordPath)
	args := []string{"cleanup", "-config", config, "-build", "1", "-collector-state", proof}
	out.Reset()
	if e := run(context.Background(), args, &out); e == nil || out.Len() != 0 {
		t.Fatal("unconfirmed cleanup accepted", e)
	}
	args[0] = "cleanup-plan"
	if e := run(context.Background(), args, &out); e != nil || !bytes.Contains(out.Bytes(), []byte(`"eligible":true`)) {
		t.Fatal(out.String(), e)
	}
	after, _ := os.ReadFile(recordPath)
	if !bytes.Equal(before, after) {
		t.Fatal("preview changed receipt")
	}
	args[0] = "cleanup"
	args = append(args, "-confirmed")
	for attempt := 0; attempt < 2; attempt++ {
		out.Reset()
		if e := run(context.Background(), args, &out); e != nil {
			t.Fatal(e)
		}
		if !bytes.Contains(out.Bytes(), []byte(`"state":"compacted"`)) || bytes.Contains(out.Bytes(), []byte(root)) {
			t.Fatal("invalid cleanup output")
		}
	}
	after, _ = os.ReadFile(recordPath)
	if bytes.Contains(after, []byte(`"payload":`)) || !bytes.Contains(after, []byte("cir_cleanup_cli")) {
		t.Fatal("compaction lost identity or retained payload")
	}
}
