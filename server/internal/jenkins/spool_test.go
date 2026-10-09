//go:build linux || darwin

package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func spoolFixture(t *testing.T) (SpoolConfig, *Spool) {
	t.Helper()
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c := SpoolConfig{Version: 1, SenderFile: filepath.Join(dir, "sender.json"), InputDir: filepath.Join(dir, "input"), StateDir: filepath.Join(dir, "state"), AckDir: filepath.Join(dir, "acks"), Binding: SpoolBinding{Version: 1, ReceiverID: "nexus_test", JenkinsID: "jenkins_test", Origin: "https://nexus.example", SourceID: "source_a", WorkspaceID: "wrk_main", ComponentID: "cmp_auth", Job: "auth-service/main", FirstBuild: 1}}
	// Keep the fixture in sync with the existing protocol payload's real job.
	p, e := ParsePayload([]byte(testPayload), testNow)
	if e != nil {
		t.Fatal(e)
	}
	c.Binding.Job = p.Job
	if e = os.Mkdir(c.InputDir, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := OpenSpool(c, "init")
	if e != nil {
		t.Fatal(e)
	}
	s.now = func() time.Time { return testNow }
	t.Cleanup(func() { s.Close() })
	return c, s
}
func putSpool(t *testing.T, c SpoolConfig, n int64, body string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(c.InputDir, spoolName(n)), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}
func nextPayload(n int) string {
	return strings.Replace(testPayload, `"build_number":42`, fmt.Sprintf(`"build_number":%d`, n), 1)
}
func reopenSpool(t *testing.T, c SpoolConfig, s *Spool) *Spool {
	t.Helper()
	s.Close()
	q, e := OpenSpool(c, "run")
	if e != nil {
		t.Fatal(e)
	}
	q.now = s.now
	t.Cleanup(func() { q.Close() })
	return q
}
func requireRecord(t *testing.T, s *Spool, n int64, state string, rounds int) spoolRecord {
	t.Helper()
	r, e := s.read(n)
	if e != nil || r.State != state || r.Rounds != rounds {
		t.Fatalf("record %d: state=%s rounds=%d error=%v", n, r.State, r.Rounds, e)
	}
	return r
}

func TestSpoolDurableImportAndSuccessRecovery(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	if e := s.Import(); e != nil {
		t.Fatal(e)
	}
	requireRecord(t, s, 42, "pending", 0)
	s = reopenSpool(t, c, s)
	calls := 0
	worked, e := s.tick(context.Background(), func(_ context.Context, b []byte) (SendResult, error) {
		calls++
		if string(b) != testPayload {
			t.Fatal("payload changed")
		}
		return SendResult{CIRunID: "cir_test"}, nil
	})
	if !worked || e != nil || calls != 1 {
		t.Fatal(worked, e, calls)
	}
	requireRecord(t, s, 42, "delivered", 1)
	s = reopenSpool(t, c, s)
	if worked, e = s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		t.Fatal("delivered twice")
		return SendResult{}, nil
	}); worked || e != nil {
		t.Fatal(worked, e)
	}
	if e = os.Remove(filepath.Join(c.AckDir, spoolName(42))); e != nil {
		t.Fatal(e)
	}
	if _, e = s.tick(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if _, e = spoolRead(filepath.Join(c.AckDir, spoolName(42)), 1024); e != nil {
		t.Fatal("missing regenerated ack", e)
	}
	status, e := s.Status(42)
	if e != nil || status.Counts["delivered"] != 1 || status.Item.CIRunID != "cir_test" || status.CleanupEnabled || status.Collector != "not_implemented_in_slice_a" {
		t.Fatal(status, e)
	}
}

func TestSpoolTransientRetriesPersistBudgetAndDoNotStarve(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	putSpool(t, c, 43, nextPayload(43))
	now := testNow
	s.now = func() time.Time { return now }
	failed := func(context.Context, []byte) (SendResult, error) { return SendResult{}, ErrDelivery }
	if _, e := s.tick(context.Background(), failed); e != nil {
		t.Fatal(e)
	}
	r := requireRecord(t, s, 42, "retry_wait", 1)
	if r.Next.Sub(now) < time.Minute || r.Next.Sub(now) > 66*time.Second {
		t.Fatal("bad backoff")
	}
	s = reopenSpool(t, c, s)
	if _, e := s.tick(context.Background(), func(_ context.Context, b []byte) (SendResult, error) {
		p, _ := ParsePayload(b, now)
		if p.Number != 43 {
			t.Fatal("starved other input")
		}
		return SendResult{CIRunID: "cir_second"}, nil
	}); e != nil {
		t.Fatal(e)
	}
	for round := 2; round <= 12; round++ {
		r, _ = s.read(42)
		now = r.Next
		if _, e := s.tick(context.Background(), failed); e != nil {
			t.Fatal(e)
		}
		if round < 12 {
			requireRecord(t, s, 42, "retry_wait", round)
		}
		s = reopenSpool(t, c, s)
	}
	r = requireRecord(t, s, 42, "blocked", 12)
	if r.Reason != "retry_exhausted" {
		t.Fatal(r.Reason)
	}
	if e := s.Retry(42); e != nil {
		t.Fatal(e)
	}
	r = requireRecord(t, s, 42, "pending", 0)
	if len(r.Resets) != 1 || r.Digest != digestBytes([]byte(testPayload)) {
		t.Fatal("manual retry lost identity/history")
	}
}

func TestSpoolUnknownResultReservationAndDayLimit(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	if e := s.Import(); e != nil {
		t.Fatal(e)
	}
	r, _ := s.read(42)
	first := testNow
	r.State = "in_flight"
	r.First = &first
	r.Rounds = 1
	if e := s.save(r); e != nil {
		t.Fatal(e)
	}
	s = reopenSpool(t, c, s)
	if _, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		return SendResult{CIRunID: "cir_original", Duplicate: true}, nil
	}); e != nil {
		t.Fatal(e)
	}
	requireRecord(t, s, 42, "delivered", 2)
	putSpool(t, c, 43, nextPayload(43))
	if _, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) { return SendResult{}, ErrResponse }); e != nil {
		t.Fatal(e)
	}
	s.now = func() time.Time { return testNow.Add(24 * time.Hour) }
	if _, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		t.Fatal("day budget exceeded")
		return SendResult{}, nil
	}); e != nil {
		t.Fatal(e)
	}
	requireRecord(t, s, 43, "blocked", 1)
}

func TestSpoolSourcePauseAndPermanentIsolation(t *testing.T) {
	for _, tc := range []struct{ code, disposition string }{{"unauthenticated", PauseSource}, {"tls_verification_failed", PauseSource}, {"conflict", BlockDelivery}, {"delivery_failed", BlockDelivery}} {
		t.Run(tc.code+tc.disposition, func(t *testing.T) {
			c, s := spoolFixture(t)
			putSpool(t, c, 42, testPayload)
			putSpool(t, c, 43, nextPayload(43))
			_, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
				return SendResult{}, &DeliveryFailure{Code: tc.code, Disposition: tc.disposition}
			})
			if tc.disposition == PauseSource && e != ErrSpoolPaused {
				t.Fatal(e)
			}
			r := requireRecord(t, s, 42, "blocked", 1)
			if r.Reason != tc.code {
				t.Fatal(r.Reason)
			}
			s = reopenSpool(t, c, s)
			calls := 0
			_, e = s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
				calls++
				return SendResult{CIRunID: "cir_next"}, nil
			})
			if tc.disposition == PauseSource {
				if e != ErrSpoolPaused || calls != 0 {
					t.Fatal(e, calls)
				}
			} else if e != nil || calls != 1 {
				t.Fatal(e, calls)
			}
			if tc.code == "conflict" {
				if e = s.Retry(42); e != ErrSpoolState {
					t.Fatal("conflict reset", e)
				}
			} else if e = s.Retry(42); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestSpoolImmutableInputAndBinding(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	if e := s.Import(); e != nil {
		t.Fatal(e)
	}
	putSpool(t, c, 42, testPayload+" ")
	if e := s.Import(); e != ErrSpoolConflict {
		t.Fatal(e)
	}
	if _, e := s.Status(0); e != ErrSpoolConflict {
		t.Fatal("conflict invisible", e)
	}
	r := requireRecord(t, s, 42, "pending", 0)
	if string(r.Payload) != testPayload {
		t.Fatal("input overwritten")
	}
	s.Close()
	c.Binding.ComponentID = "cmp_other"
	if q, e := OpenSpool(c, "run"); e != ErrSpoolBinding {
		if q != nil {
			q.Close()
		}
		t.Fatal(e)
	}
}

func TestSpoolDiskFailureNeverConfirmsOrSends(t *testing.T) {
	for _, stage := range []string{"write", "file_sync", "rename", "directory_sync"} {
		t.Run(stage, func(t *testing.T) {
			c, s := spoolFixture(t)
			putSpool(t, c, 42, testPayload)
			if e := s.Import(); e != nil {
				t.Fatal(e)
			}
			s.fault = func(got string) error {
				if got == stage {
					return errors.New("synthetic private IO detail")
				}
				return nil
			}
			calls := 0
			if _, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) { calls++; return SendResult{}, nil }); e != ErrSpool || calls != 0 {
				t.Fatal(e, calls)
			}
			if _, e := os.Stat(filepath.Join(c.AckDir, spoolName(42))); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("ack after failure", e)
			}
			s = reopenSpool(t, c, s)
			if _, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) { return SendResult{CIRunID: "cir_recovered"}, nil }); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestSpoolResponseBeforeLocalCommitReplaysSameFact(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	_, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		s.fault = func(string) error { return ErrSpool }
		return SendResult{CIRunID: "cir_committed"}, nil
	})
	if e != ErrSpool {
		t.Fatal(e)
	}
	requireRecord(t, s, 42, "in_flight", 1)
	s = reopenSpool(t, c, s)
	if _, e = s.tick(context.Background(), func(_ context.Context, b []byte) (SendResult, error) {
		if string(b) != testPayload {
			t.Fatal("new identity")
		}
		return SendResult{CIRunID: "cir_committed", Duplicate: true}, nil
	}); e != nil {
		t.Fatal(e)
	}
	r := requireRecord(t, s, 42, "delivered", 2)
	if r.CIRunID != "cir_committed" {
		t.Fatal("wrong fact")
	}
}

func TestSpoolRejectsUnsafeFilesAndCorruption(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "permissions", "oversized", "unknown-file", "unknown-version", "duplicate-key", "bad-digest", "orphan-ack", "manifest"} {
		t.Run(kind, func(t *testing.T) {
			c, s := spoolFixture(t)
			putSpool(t, c, 42, testPayload)
			if e := s.Import(); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(c.StateDir, spoolName(42))
			raw, _ := os.ReadFile(path)
			switch kind {
			case "symlink":
				os.Remove(path)
				os.Symlink(filepath.Join(c.InputDir, spoolName(42)), path)
			case "hardlink":
				os.Remove(path)
				os.Link(filepath.Join(c.InputDir, spoolName(42)), path)
			case "directory":
				os.Remove(path)
				os.Mkdir(path, 0700)
			case "permissions":
				os.Chmod(path, 0644)
			case "oversized":
				os.WriteFile(path, []byte(strings.Repeat("x", MaxSpoolRecord+1)), 0600)
			case "unknown-file":
				os.WriteFile(filepath.Join(c.StateDir, "secret"), []byte("private"), 0600)
			case "unknown-version":
				os.WriteFile(path, []byte(strings.Replace(string(raw), `"version":1`, `"version":2`, 1)), 0600)
			case "duplicate-key":
				os.WriteFile(path, []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1)), 0600)
			case "bad-digest":
				os.WriteFile(path, []byte(strings.Replace(string(raw), digestBytes([]byte(testPayload)), strings.Repeat("a", 64), 1)), 0600)
			case "orphan-ack":
				os.WriteFile(filepath.Join(c.AckDir, spoolName(43)), []byte("{}"), 0600)
			case "manifest":
				os.WriteFile(filepath.Join(c.StateDir, "manifest.json"), []byte("{}"), 0600)
			}
			if _, e := s.Status(0); e == nil {
				t.Fatal("unsafe state accepted")
			}
			if _, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
				t.Fatal("sent unsafe state")
				return SendResult{}, nil
			}); e == nil {
				t.Fatal("unsafe send accepted")
			}
		})
	}
}

func TestSpoolCapacityAndImportBatch(t *testing.T) {
	c, s := spoolFixture(t)
	for i := 1; i <= 101; i++ {
		putSpool(t, c, int64(i), nextPayload(i))
	}
	if e := s.Import(); e != nil {
		t.Fatal(e)
	}
	status, e := s.Status(0)
	if e != nil || status.Counts["pending"] != 100 {
		t.Fatal(status, e)
	}
	if e = s.Import(); e != nil {
		t.Fatal(e)
	}
	status, e = s.Status(0)
	if e != nil || status.Counts["pending"] != 101 {
		t.Fatal(status, e)
	}
	f, e := os.OpenFile(filepath.Join(c.StateDir, ".pending-full"), os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	e = f.Truncate(MaxSpoolBytes)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Status(0); e != ErrSpoolCapacity {
		t.Fatal("quota not enforced", e)
	}
}

func TestSpoolUnsupportedResultIsNotFabricated(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, strings.Replace(testPayload, "SUCCESS", "UNSTABLE", 1))
	putSpool(t, c, 43, nextPayload(43))
	if _, e := s.tick(context.Background(), func(_ context.Context, b []byte) (SendResult, error) {
		p, _ := ParsePayload(b, testNow)
		if p.Number != 43 {
			t.Fatal("unsupported sent")
		}
		return SendResult{CIRunID: "cir_next"}, nil
	}); e != nil {
		t.Fatal(e)
	}
	r := requireRecord(t, s, 42, "blocked", 0)
	if r.Reason != "unsupported_result" {
		t.Fatal(r.Reason)
	}
}

func TestSpoolProcessHelper(t *testing.T) {
	path := os.Getenv("RADISHNEXUS_SPOOL_HELPER_CONFIG")
	if path == "" {
		return
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		os.Exit(31)
	}
	c, e := LoadSpoolConfig(raw)
	if e != nil {
		os.Exit(32)
	}
	s, e := OpenSpool(c, "run")
	if e != nil {
		os.Exit(33)
	}
	defer s.Close()
	s.now = func() time.Time { return testNow }
	_, e = s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) { os.Exit(23); return SendResult{}, nil })
	if e != nil {
		os.Exit(34)
	}
	os.Exit(35)
}

func TestSpoolRealProcessExitReleasesLockAndPreservesInflight(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	if q, e := OpenSpool(c, "run"); e != ErrSpoolLocked {
		if q != nil {
			q.Close()
		}
		t.Fatal("second writer accepted", e)
	}
	reader, e := OpenSpool(c, "inspect")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = reader.Status(0); e != nil {
		t.Fatal(e)
	}
	reader.Close()
	s.Close()
	config := filepath.Join(filepath.Dir(c.StateDir), "worker.json")
	raw, _ := json.Marshal(c)
	os.WriteFile(config, raw, 0600)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpoolProcessHelper$")
	cmd.Env = append(os.Environ(), "RADISHNEXUS_SPOOL_HELPER_CONFIG="+config)
	e = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 23 {
		t.Fatal("child exit", e)
	}
	s = reopenSpool(t, c, s)
	requireRecord(t, s, 42, "in_flight", 1)
	if _, e = s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) { return SendResult{CIRunID: "cir_resumed"}, nil }); e != nil {
		t.Fatal(e)
	}
	requireRecord(t, s, 42, "delivered", 2)
}

func TestDeliveryFailureClassificationPreservesUnknown4xx(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 409, 418, 422, 307} {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"private-details"}}`))}, nil
		})}
		_, e := send(context.Background(), senderFixture(t, "https://nexus.example"), []byte(testPayload), client, func() time.Time { return testNow }, wait)
		f := ClassifyDelivery(e)
		want := BlockDelivery
		if code == 401 {
			want = PauseSource
		}
		if f.Disposition != want || f.HTTPStatus != code || strings.Contains(f.Code, "private") {
			t.Fatal(f, e)
		}
	}
}

func TestSpoolAcknowledgementFailureKeepsCommittedState(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	writes := 0
	_, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		s.fault = func(stage string) error {
			if stage == "write" {
				writes++
				if writes == 2 {
					return ErrSpool
				}
			}
			return nil
		}
		return SendResult{CIRunID: "cir_committed"}, nil
	})
	if e != ErrSpool {
		t.Fatal(e)
	}
	requireRecord(t, s, 42, "delivered", 1)
	s = reopenSpool(t, c, s)
	if _, e = s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		t.Fatal("ack repair resent")
		return SendResult{}, nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestSpoolMissingManifestDoesNotReinitializeState(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	if e := s.Import(); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if e := os.Remove(filepath.Join(c.StateDir, "manifest.json")); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"run", "init", "inspect"} {
		q, e := OpenSpool(c, mode)
		if e == nil {
			q.Close()
			t.Fatal("adopted unbound state", mode)
		}
	}
}

func TestSpoolConfigAndKeyRotation(t *testing.T) {
	c, s := spoolFixture(t)
	dir := filepath.Dir(c.StateDir)
	key := filepath.Join(dir, "key")
	if e := os.WriteFile(key, []byte(strings.Repeat("ab", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	for _, keyID := range []string{"key_a", "key_b"} {
		raw := []byte(fmt.Sprintf(`{"version":1,"origin":%q,"source_id":%q,"key_id":%q,"secret_file":%q}`, c.Binding.Origin, c.Binding.SourceID, keyID, key))
		if e := os.WriteFile(c.SenderFile, raw, 0600); e != nil {
			t.Fatal(e)
		}
		if sender, e := c.Sender(); e != nil || sender.KeyID != keyID {
			t.Fatal("rotation rejected", e)
		}
		s = reopenSpool(t, c, s)
	}
	raw, _ := json.Marshal(c)
	for _, bad := range []string{strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(raw), c.AckDir, c.StateDir, 1), strings.Replace(string(raw), `"first_build_number":1`, `"first_build_number":0`, 1)} {
		if _, e := LoadSpoolConfig([]byte(bad)); e == nil {
			t.Fatal("invalid config accepted")
		}
	}
	// Credentials may not be placed in any spooled/backup directory.
	keyInside := filepath.Join(c.StateDir, ".pending-key")
	if e := os.WriteFile(keyInside, []byte(strings.Repeat("ab", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(c.SenderFile, []byte(fmt.Sprintf(`{"version":1,"origin":%q,"source_id":%q,"key_id":"key_b","secret_file":%q}`, c.Binding.Origin, c.Binding.SourceID, keyInside)), 0600)
	if _, e := c.Sender(); e == nil {
		t.Fatal("credential in spool accepted")
	}
}

func TestSpoolMalformedInputDoesNotPauseOtherBuilds(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, `{"version":1}`)
	putSpool(t, c, 43, nextPayload(43))
	calls := 0
	if _, e := s.tick(context.Background(), func(_ context.Context, body []byte) (SendResult, error) {
		calls++
		if string(body) != nextPayload(43) {
			t.Fatal("malformed input sent")
		}
		return SendResult{CIRunID: "cir_valid"}, nil
	}); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	r := requireRecord(t, s, 42, "blocked", 0)
	if r.Reason != "invalid_request" || r.Disposition != BlockDelivery {
		t.Fatal(r.Reason, r.Disposition)
	}
	s = reopenSpool(t, c, s)
	requireRecord(t, s, 43, "delivered", 1)
}

func TestSpoolOfflineCopyPreservesBudgetAndRejectsRebinding(t *testing.T) {
	c, s := spoolFixture(t)
	putSpool(t, c, 42, testPayload)
	if _, e := s.tick(context.Background(), func(context.Context, []byte) (SendResult, error) {
		return SendResult{}, ErrResponse
	}); e != nil {
		t.Fatal(e)
	}
	original := requireRecord(t, s, 42, "retry_wait", 1)
	s.Close() // Copy only while both producer and worker are stopped.
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	restored := c
	restored.InputDir = filepath.Join(root, "input")
	restored.StateDir = filepath.Join(root, "state")
	restored.AckDir = filepath.Join(root, "acks")
	restored.SenderFile = filepath.Join(root, "not-provisioned.json")
	for source, destination := range map[string]string{c.InputDir: restored.InputDir, c.StateDir: restored.StateDir, c.AckDir: restored.AckDir} {
		if e = os.Mkdir(destination, 0700); e != nil {
			t.Fatal(e)
		}
		entries, e := os.ReadDir(source)
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			raw, e := os.ReadFile(filepath.Join(source, entry.Name()))
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(destination, entry.Name()), raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	q, e := OpenSpool(restored, "inspect") // No credentials or network required.
	if e != nil {
		t.Fatal(e)
	}
	q.now = s.now
	r := requireRecord(t, q, 42, "retry_wait", 1)
	if r.Digest != original.Digest || !r.First.Equal(*original.First) || !r.Next.Equal(original.Next) || string(r.Payload) != testPayload {
		t.Fatal("offline restore changed identity or budget")
	}
	q.Close()
	restored.Binding.ReceiverID = "different_receiver"
	if q, e = OpenSpool(restored, "run"); e != ErrSpoolBinding {
		if q != nil {
			q.Close()
		}
		t.Fatal("restored state rebound", e)
	}
}
