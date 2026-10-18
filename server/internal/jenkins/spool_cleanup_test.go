//go:build linux || darwin

package jenkins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cleanupFixture(t *testing.T) (SpoolConfig, *Spool, string, map[string]any) {
	t.Helper()
	c, s := spoolFixture(t)
	putSpool(t, c, 1, nextPayload(1))
	if _, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) { return SendResult{CIRunID: "cir_cleanup"}, nil }); e != nil {
		t.Fatal(e)
	}
	proof, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(proof, 0700); e != nil {
		t.Fatal(e)
	}
	r := requireRecord(t, s, 1, "delivered", 1)
	handoff := testNow.Add(time.Minute)
	retired := handoff.Add(cleanupRetention)
	s.now = func() time.Time { return testNow.Add(8 * 24 * time.Hour) }
	checkpoint := map[string]any{"version": 2, "binding": c.Binding, "prefix": 1, "high": 1, "scan_high": 1, "cursor": 1,
		"observed_at": retired, "last_sweep_at": handoff, "lifecycle": "stopped", "error": "", "entries": map[string]any{
			"1": map[string]any{"kind": "retired", "digest": r.Digest, "handoff_at": handoff, "retired_at": retired}}}
	writeCleanupJSON(t, filepath.Join(proof, "manifest.json"), c.Binding)
	writeCleanupJSON(t, filepath.Join(proof, "checkpoint.json"), checkpoint)
	return c, s, proof, checkpoint
}
func writeCleanupJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestSpoolCleanupRetainsIdentityAndDoesNotResendRestoredInput(t *testing.T) {
	c, s, proof, _ := cleanupFixture(t)
	before, _ := os.ReadFile(filepath.Join(c.StateDir, spoolName(1)))
	p, e := s.CleanupPlan(1, proof)
	if e != nil || p.Eligible || p.Reason != "collector_input_present" {
		t.Fatal(p, e)
	}
	after, _ := os.ReadFile(filepath.Join(c.StateDir, spoolName(1)))
	if !bytes.Equal(before, after) {
		t.Fatal("preview mutated record")
	}
	if _, e = s.Cleanup(1, proof); !errors.Is(e, ErrCleanup) {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(c.InputDir, spoolName(1))); e != nil {
		t.Fatal(e)
	}
	p, e = s.CleanupPlan(1, proof)
	if e != nil || !p.Eligible || p.Automatic || p.ReclaimBytes == 0 {
		t.Fatal(p, e)
	}
	if _, e = s.Cleanup(1, proof); e != nil {
		t.Fatal(e)
	}
	r := requireRecord(t, s, 1, "compacted", 0)
	if r.CIRunID != "cir_cleanup" || len(r.Payload) != 0 || !r.Delivered.Equal(testNow) {
		t.Fatal("receipt identity lost")
	}
	after, _ = os.ReadFile(filepath.Join(c.StateDir, spoolName(1)))
	if bytes.Contains(after, []byte(`"payload":`)) || bytes.Contains(after, []byte("job_full_name")) || len(after) >= len(before) {
		t.Fatal("payload not reclaimed")
	}
	s = reopenSpool(t, c, s)
	if p, e = s.Cleanup(1, proof); e != nil || !p.AlreadyCompacted {
		t.Fatal(p, e)
	}
	if e = s.Retry(1); !errors.Is(e, ErrSpoolState) {
		t.Fatal("compacted receipt retryable", e)
	}
	// An old backup's input can reappear without reintroducing a send.
	putSpool(t, c, 1, nextPayload(1))
	if e = os.Remove(filepath.Join(c.AckDir, spoolName(1))); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		t.Fatal("compacted receipt resent")
		return SendResult{}, nil
	}); e != nil || worked {
		t.Fatal(worked, e)
	}
	if _, e = os.Stat(filepath.Join(c.AckDir, spoolName(1))); e != nil {
		t.Fatal("ack not repaired", e)
	}
	status, e := s.Status(1)
	if e != nil || status.Counts["compacted"] != 1 || status.CleanupCandidates != 0 || status.OldestPending != nil || status.RemainingRecords != MaxSpoolEntries-1 {
		t.Fatal(status, e)
	}
	itemJSON, _ := json.Marshal(status.Item)
	if bytes.Contains(itemJSON, []byte("next_attempt")) || bytes.Contains(itemJSON, []byte("rounds")) || !bytes.Contains(itemJSON, []byte(`"history_compacted":true`)) {
		t.Fatal("discarded history reported as zero", string(itemJSON))
	}
	putSpool(t, c, 1, strings.Replace(nextPayload(1), "SUCCESS", "FAILURE", 1))
	if e = s.Import(); !errors.Is(e, ErrSpoolConflict) {
		t.Fatal("conflicting restored input accepted", e)
	}
}

func TestSpoolCleanupRejectsUnprovenOrUnretainedRecords(t *testing.T) {
	tests := []struct {
		name   string
		change func(SpoolConfig, *Spool, string, map[string]any)
		want   error
		reason string
	}{
		{"age", func(c SpoolConfig, s *Spool, p string, m map[string]any) {
			s.now = func() time.Time { return testNow.Add(time.Hour) }
		}, nil, "retention_not_met"},
		{"not retired", func(c SpoolConfig, s *Spool, p string, m map[string]any) {
			m["version"] = 1
			entry := m["entries"].(map[string]any)["1"].(map[string]any)
			entry["kind"] = "published"
			delete(entry, "retired_at")
		}, nil, "collector_not_ready"},
		{"other binding", func(c SpoolConfig, s *Spool, p string, m map[string]any) {
			b := c.Binding
			b.ComponentID = "cmp_other"
			m["binding"] = b
		}, ErrSpoolBinding, ""},
		{"other digest", func(c SpoolConfig, s *Spool, p string, m map[string]any) {
			m["entries"].(map[string]any)["1"].(map[string]any)["digest"] = strings.Repeat("0", 64)
		}, ErrSpoolConflict, ""},
		{"premature retirement", func(c SpoolConfig, s *Spool, p string, m map[string]any) {
			m["entries"].(map[string]any)["1"].(map[string]any)["retired_at"] = testNow.Add(time.Hour)
		}, ErrSpoolState, ""},
		{"missing ack", func(c SpoolConfig, s *Spool, p string, m map[string]any) {
			if e := os.Remove(filepath.Join(c.AckDir, spoolName(1))); e != nil {
				t.Fatal(e)
			}
		}, nil, "ack_required"},
		{"future checkpoint", func(c SpoolConfig, s *Spool, p string, m map[string]any) { m["observed_at"] = s.now().Add(time.Hour) }, ErrSpoolState, ""},
		{"paused collector", func(c SpoolConfig, s *Spool, p string, m map[string]any) {
			m["lifecycle"] = "paused"
			m["error"] = "collector_input_conflict"
		}, nil, "collector_not_ready"},
		{"running collector", func(c SpoolConfig, s *Spool, p string, m map[string]any) { m["lifecycle"] = "running" }, nil, "collector_not_ready"},
		{"null prefix", func(c SpoolConfig, s *Spool, p string, m map[string]any) { m["prefix"] = nil }, ErrSpoolState, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, s, p, m := cleanupFixture(t)
			if e := os.Remove(filepath.Join(c.InputDir, spoolName(1))); e != nil {
				t.Fatal(e)
			}
			tt.change(c, s, p, m)
			writeCleanupJSON(t, filepath.Join(p, "checkpoint.json"), m)
			before, _ := os.ReadFile(filepath.Join(c.StateDir, spoolName(1)))
			plan, e := s.CleanupPlan(1, p)
			if !errors.Is(e, tt.want) || plan.Eligible || plan.Reason != tt.reason {
				t.Fatal(plan, e)
			}
			if _, e = s.Cleanup(1, p); e == nil {
				t.Fatal("unproven cleanup accepted")
			}
			after, _ := os.ReadFile(filepath.Join(c.StateDir, spoolName(1)))
			if !bytes.Equal(before, after) {
				t.Fatal("rejected cleanup changed record")
			}
		})
	}
}

func TestSpoolCleanupAtomicFaultsAndResume(t *testing.T) {
	for _, stage := range []string{"write", "file_sync", "rename", "directory_sync"} {
		t.Run(stage, func(t *testing.T) {
			c, s, p, _ := cleanupFixture(t)
			if e := os.Remove(filepath.Join(c.InputDir, spoolName(1))); e != nil {
				t.Fatal(e)
			}
			s.fault = func(at string) error {
				if at == stage {
					return errors.New("synthetic")
				}
				return nil
			}
			if _, e := s.Cleanup(1, p); !errors.Is(e, ErrSpool) {
				t.Fatal(e)
			}
			if _, e := s.Cleanup(1, p); !errors.Is(e, ErrSpool) {
				t.Fatal("failed process kept writing", e)
			}
			s = reopenSpool(t, c, s)
			if _, e := s.Cleanup(1, p); e != nil {
				t.Fatal(e)
			}
			if worked, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
				t.Fatal("cleanup recovery resent")
				return SendResult{}, nil
			}); worked || e != nil {
				t.Fatal(worked, e)
			}
		})
	}
}

func TestSpoolCleanupRejectsCorruptProofAndCompactIndex(t *testing.T) {
	c, s, p, _ := cleanupFixture(t)
	raw, _ := os.ReadFile(filepath.Join(p, "checkpoint.json"))
	// Duplicate dynamic build keys must not be silently replaced by json.Unmarshal.
	raw = bytes.Replace(raw, []byte(`"entries":{`), []byte(`"entries":{"1":{"kind":"missing","digest":"","handoff_at":null},`), 1)
	if e := os.WriteFile(filepath.Join(p, "checkpoint.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CleanupPlan(1, p); !errors.Is(e, ErrSpoolState) {
		t.Fatal(e)
	}
	c, s, p, _ = cleanupFixture(t)
	if e := os.Remove(filepath.Join(c.InputDir, spoolName(1))); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Cleanup(1, p); e != nil {
		t.Fatal(e)
	}
	raw, _ = os.ReadFile(filepath.Join(c.StateDir, spoolName(1)))
	raw = bytes.Replace(raw, []byte(`"source_id":"source_a"`), []byte(`"source_id":"other_source"`), 1)
	if e := os.WriteFile(filepath.Join(c.StateDir, spoolName(1)), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Status(1); !errors.Is(e, ErrSpoolState) {
		t.Fatal("rebound compact index accepted", e)
	}
}

func TestSpoolCleanupProcessDeathAndOfflineRestore(t *testing.T) {
	if config := os.Getenv("RADISHNEXUS_CLEANUP_CRASH_CONFIG"); config != "" {
		raw, e := os.ReadFile(config)
		if e != nil {
			t.Fatal(e)
		}
		c, e := LoadSpoolConfig(raw)
		if e != nil {
			t.Fatal(e)
		}
		s, e := OpenSpool(c, "run")
		if e != nil {
			t.Fatal(e)
		}
		s.now = func() time.Time { return testNow.Add(8 * 24 * time.Hour) }
		s.fault = func(stage string) error {
			if stage == "directory_sync" {
				os.Exit(25)
			}
			return nil
		}
		_, e = s.Cleanup(1, os.Getenv("RADISHNEXUS_CLEANUP_CRASH_PROOF"))
		t.Fatal("crash point missed", e)
	}
	c, s, p, _ := cleanupFixture(t)
	if e := os.Remove(filepath.Join(c.InputDir, spoolName(1))); e != nil {
		t.Fatal(e)
	}
	s.Close()
	config := filepath.Join(filepath.Dir(c.StateDir), "cleanup-worker.json")
	writeCleanupJSON(t, config, c)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpoolCleanupProcessDeathAndOfflineRestore$")
	cmd.Env = append(os.Environ(), "RADISHNEXUS_CLEANUP_CRASH_CONFIG="+config, "RADISHNEXUS_CLEANUP_CRASH_PROOF="+p)
	output, e := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 25 {
		t.Fatalf("crash helper failed: %v %s", e, output)
	}
	s = reopenSpool(t, c, s)
	requireRecord(t, s, 1, "compacted", 0)
	if _, e = s.Cleanup(1, p); e != nil {
		t.Fatal(e)
	}
	s.Close()
	base, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	restored := c
	restored.InputDir = filepath.Join(base, "input")
	restored.StateDir = filepath.Join(base, "state")
	restored.AckDir = filepath.Join(base, "ack")
	proofCopy := filepath.Join(base, "collector")
	for from, to := range map[string]string{c.InputDir: restored.InputDir, c.StateDir: restored.StateDir, c.AckDir: restored.AckDir, p: proofCopy} {
		if e = os.Mkdir(to, 0700); e != nil {
			t.Fatal(e)
		}
		entries, e := os.ReadDir(from)
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			raw, e := os.ReadFile(filepath.Join(from, entry.Name()))
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(to, entry.Name()), raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	q, e := OpenSpool(restored, "run")
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	q.now = s.now
	if plan, e := q.Cleanup(1, proofCopy); e != nil || !plan.AlreadyCompacted {
		t.Fatal(plan, e)
	}
	if worked, e := q.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		t.Fatal("restored compact receipt resent")
		return SendResult{}, nil
	}); worked || e != nil {
		t.Fatal(worked, e)
	}
}

func TestSpoolCleanupNeverCompactsPendingOrBlocked(t *testing.T) {
	for _, raw := range []string{nextPayload(1), strings.Replace(nextPayload(1), "SUCCESS", "UNSTABLE", 1)} {
		c, s := spoolFixture(t)
		putSpool(t, c, 1, raw)
		if e := s.Import(); e != nil {
			t.Fatal(e)
		}
		s.now = func() time.Time { return testNow.Add(30 * 24 * time.Hour) }
		plan, e := s.CleanupPlan(1, "/proof-not-needed-for-ineligible-record")
		if e != nil || plan.Eligible || plan.Reason != "delivery_not_completed" {
			t.Fatal(plan, e)
		}
		if _, e = s.Cleanup(1, "/proof-not-needed-for-ineligible-record"); !errors.Is(e, ErrCleanup) {
			t.Fatal(e)
		}
	}
}
