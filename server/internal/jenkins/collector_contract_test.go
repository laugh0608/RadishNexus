//go:build linux || darwin

package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in cross-language check, consuming actual bytes exported by the Groovy
// collector test. These are synthetic Runs, never real Jenkins build evidence.
func TestCollectorWorkerContract(t *testing.T) {
	dir := os.Getenv("RADISHNEXUS_COLLECTOR_CONTRACT")
	if dir == "" {
		t.Skip("set RADISHNEXUS_COLLECTOR_CONTRACT to the Groovy test export")
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("collector contract directory must be absolute")
	}
	c, s := spoolFixture(t)
	s.Close()
	// The receiver binding is unchanged; this is a new fixture spool for the
	// exact collector job. Do not mutate an existing manifest to adopt it.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.InputDir, c.StateDir, c.AckDir = filepath.Join(base, "input"), filepath.Join(base, "state"), filepath.Join(base, "ack")
	c.Binding.Job = "collector-probe"
	if err := os.Mkdir(c.InputDir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSpool(c, "init")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	results := []string{"SUCCESS", "FAILURE", "ABORTED", "UNSTABLE", "NOT_BUILT"}
	for index, result := range results {
		n := int64(index + 1)
		body, err := ReadFile(filepath.Join(dir, spoolName(n)), MaxBody)
		if err != nil {
			t.Fatal("missing collector export", n)
		}
		p, err := ParsePayload(body, time.Now())
		if (index < 3 && err != nil) || (index >= 3 && !errors.Is(err, ErrUnsupportedResult)) || p.Number != n || p.Result != result || p.Job != c.Binding.Job {
			t.Fatal("collector payload contract drift", n, err)
		}
		putSpool(t, c, n, string(body))
	}
	sent := map[int64]bool{}
	for i := 0; i < 4; i++ {
		_, err = s.tick(context.Background(), func(_ context.Context, raw []byte) (SendResult, error) {
			p, err := ParsePayload(raw, time.Now())
			if err != nil || sent[p.Number] {
				t.Fatal("unexpected or duplicate send", err)
			}
			sent[p.Number] = true
			return SendResult{CIRunID: fmt.Sprintf("cir_collector%d", p.Number)}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(sent) != 3 {
		t.Fatal("wrong terminal send count", len(sent))
	}
	for n := int64(1); n <= 5; n++ {
		if n > 3 {
			r := requireRecord(t, s, n, "blocked", 0)
			if r.Reason != "unsupported_result" {
				t.Fatal(r.Reason)
			}
			continue
		}
		r := requireRecord(t, s, n, "delivered", 1)
		raw, err := spoolRead(filepath.Join(c.AckDir, spoolName(n)), 1024)
		var ack spoolAck
		if err != nil || json.Unmarshal(raw, &ack) != nil || ack.Digest != r.Digest || ack.Number != n || ack.SourceID != c.Binding.SourceID {
			t.Fatal("collector handoff contract drift", n, err)
		}
	}
}
