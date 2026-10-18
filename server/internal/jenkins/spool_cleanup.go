package jenkins

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const cleanupRetention = 7 * 24 * time.Hour
const maxCollectorCheckpoint = 4 * 1024 * 1024

var ErrCleanup = errors.New("spool_cleanup_not_eligible")
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Compact records remain in the identity inventory for the source's lifetime.
// They are not retryable and never lose the receipt's original CI Run identity.
type compactRecord struct {
	Version   int       `json:"version"`
	Binding   string    `json:"binding_sha256"`
	SourceID  string    `json:"source_id"`
	Number    int64     `json:"build_number"`
	Digest    string    `json:"payload_sha256"`
	CIRunID   string    `json:"ci_run_id"`
	Delivered time.Time `json:"delivered_at"`
	Compacted time.Time `json:"compacted_at"`
}

func (s *Spool) readCompact(raw []byte, number int64) (spoolRecord, error) {
	var c compactRecord
	if _, e := object(raw, "version", "binding_sha256", "source_id", "build_number", "payload_sha256", "ci_run_id", "delivered_at", "compacted_at"); e != nil || json.Unmarshal(raw, &c) != nil {
		return spoolRecord{}, ErrSpoolState
	}
	if c.Version != 2 || c.Binding != s.binding || c.SourceID != s.config.Binding.SourceID || c.Number != number || !sha256Pattern.MatchString(c.Digest) || !ciIDPattern.MatchString(c.CIRunID) || c.Delivered.IsZero() || c.Compacted.Before(c.Delivered.Add(cleanupRetention)) {
		return spoolRecord{}, ErrSpoolState
	}
	return spoolRecord{Version: 2, Binding: c.Binding, Number: c.Number, Digest: c.Digest, State: "compacted", CIRunID: c.CIRunID, Delivered: &c.Delivered, Updated: c.Compacted}, nil
}

type collectorEntry struct {
	Kind      string     `json:"kind"`
	Digest    string     `json:"digest"`
	Handoff   *time.Time `json:"handoff_at"`
	RetiredAt *time.Time `json:"retired_at"`
}

// A dynamic object needs the same duplicate-key rejection as fixed protocol
// objects, but its keys are bounded build numbers rather than a static schema.
func collectorEntries(raw []byte, version int, first, high int64) (map[int64]collectorEntry, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, ErrSpoolState
	}
	entries := map[int64]collectorEntry{}
	for d.More() {
		token, e := d.Token()
		key, ok := token.(string)
		if e != nil || !ok || !numberPattern.MatchString(key) || len(entries) >= MaxSpoolEntries {
			return nil, ErrSpoolState
		}
		n, e := strconv.ParseInt(key, 10, 64)
		if e != nil || n < first || n > high {
			return nil, ErrSpoolState
		}
		if _, exists := entries[n]; exists {
			return nil, ErrSpoolState
		}
		var body json.RawMessage
		var entry collectorEntry
		if d.Decode(&body) != nil || json.Unmarshal(body, &entry) != nil {
			return nil, ErrSpoolState
		}
		fields := []string{"kind", "digest", "handoff_at"}
		if entry.Kind == "retired" && version == 2 {
			fields = append(fields, "retired_at")
		}
		values, e := object(body, fields...)
		if e != nil || decode(values["kind"], &entry.Kind) != nil || decode(values["digest"], &entry.Digest) != nil {
			return nil, ErrSpoolState
		}
		switch entry.Kind {
		case "published", "retired":
			if !sha256Pattern.MatchString(entry.Digest) || (entry.Handoff != nil && entry.Handoff.IsZero()) {
				return nil, ErrSpoolState
			}
			if entry.Kind == "retired" && (version != 2 || entry.Handoff == nil || entry.RetiredAt == nil || entry.RetiredAt.Before(entry.Handoff.Add(cleanupRetention))) {
				return nil, ErrSpoolState
			}
		case "running", "missing", "unreadable":
			if entry.Digest != "" || entry.Handoff != nil {
				return nil, ErrSpoolState
			}
		default:
			return nil, ErrSpoolState
		}
		entries[n] = entry
	}
	if _, e = d.Token(); e != nil {
		return nil, ErrSpoolState
	}
	if _, e = d.Token(); !errors.Is(e, io.EOF) {
		return nil, ErrSpoolState
	}
	return entries, nil
}

func (s *Spool) collectorProof(dir string, number int64) (collectorEntry, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return collectorEntry{}, ErrConfiguration
	}
	for _, own := range []string{s.config.InputDir, s.config.StateDir, s.config.AckDir} {
		if dir == own || strings.HasPrefix(dir, own+string(filepath.Separator)) || strings.HasPrefix(own, dir+string(filepath.Separator)) {
			return collectorEntry{}, ErrConfiguration
		}
	}
	if e := spoolDirectory(dir); e != nil {
		return collectorEntry{}, e
	}
	manifest, e := spoolRead(filepath.Join(dir, "manifest.json"), MaxConfig)
	if e != nil {
		return collectorEntry{}, e
	}
	b, e := parseBinding(manifest)
	if e != nil || b != s.config.Binding {
		return collectorEntry{}, ErrSpoolBinding
	}
	raw, e := spoolRead(filepath.Join(dir, "checkpoint.json"), maxCollectorCheckpoint)
	if e != nil {
		return collectorEntry{}, e
	}
	f, e := object(raw, "version", "binding", "prefix", "high", "scan_high", "cursor", "observed_at", "last_sweep_at", "lifecycle", "error", "entries")
	if e != nil {
		return collectorEntry{}, ErrSpoolState
	}
	var checkpoint struct {
		Version   int        `json:"version"`
		Prefix    int64      `json:"prefix"`
		High      int64      `json:"high"`
		ScanHigh  int64      `json:"scan_high"`
		Cursor    int64      `json:"cursor"`
		Observed  time.Time  `json:"observed_at"`
		LastSweep *time.Time `json:"last_sweep_at"`
		Lifecycle string     `json:"lifecycle"`
		Error     string     `json:"error"`
	}
	if json.Unmarshal(raw, &checkpoint) != nil {
		return collectorEntry{}, ErrSpoolState
	}
	for key, target := range map[string]any{"version": &checkpoint.Version, "prefix": &checkpoint.Prefix, "high": &checkpoint.High, "scan_high": &checkpoint.ScanHigh, "cursor": &checkpoint.Cursor, "lifecycle": &checkpoint.Lifecycle, "error": &checkpoint.Error} {
		if decode(f[key], target) != nil {
			return collectorEntry{}, ErrSpoolState
		}
	}
	b, e = parseBinding(f["binding"])
	if e != nil || b != s.config.Binding {
		return collectorEntry{}, ErrSpoolBinding
	}
	c := checkpoint
	first := b.FirstBuild
	if (c.Version != 1 && c.Version != 2) || c.High < first-1 || c.High > 2147483647 || c.High-first >= MaxSpoolEntries || c.ScanHigh < first-1 || c.ScanHigh > c.High || c.Cursor < first || c.Cursor > c.ScanHigh+1 || c.Observed.IsZero() || c.Observed.After(s.now()) || (c.LastSweep != nil && (c.LastSweep.IsZero() || c.LastSweep.After(c.Observed))) || (c.Lifecycle != "running" && c.Lifecycle != "stopped" && c.Lifecycle != "paused") {
		return collectorEntry{}, ErrSpoolState
	}
	entries, e := collectorEntries(f["entries"], c.Version, first, c.High)
	if e != nil {
		return collectorEntry{}, e
	}
	for _, entry := range entries {
		if (entry.Handoff != nil && entry.Handoff.After(c.Observed)) || (entry.RetiredAt != nil && entry.RetiredAt.After(c.Observed)) {
			return collectorEntry{}, ErrSpoolState
		}
	}
	prefix := first - 1
	for {
		entry, ok := entries[prefix+1]
		if !ok || (entry.Kind != "published" && entry.Kind != "retired") {
			break
		}
		prefix++
	}
	if prefix != c.Prefix {
		return collectorEntry{}, ErrSpoolState
	}
	entry, ok := entries[number]
	if !ok || entry.Kind != "retired" || c.Error != "" || c.Lifecycle != "stopped" {
		return collectorEntry{}, ErrCleanup
	}
	if entry.RetiredAt.After(s.now()) {
		return collectorEntry{}, ErrSpoolState
	}
	return entry, nil
}

type CleanupPlan struct {
	SourceID         string `json:"source_id"`
	Number           int64  `json:"build_number"`
	Eligible         bool   `json:"eligible"`
	AlreadyCompacted bool   `json:"already_compacted"`
	ReclaimBytes     int64  `json:"payload_bytes_to_drop"`
	Automatic        bool   `json:"automatic_cleanup"`
	Reason           string `json:"reason"`
	checkedAt        time.Time
}

// CleanupPlan reads both sides but writes neither, and loads no credentials.
// collectorDir is an explicit read-only mount, not an implicit path discovery.
func (s *Spool) CleanupPlan(number int64, collectorDir string) (CleanupPlan, error) {
	p := CleanupPlan{SourceID: s.config.Binding.SourceID, Number: number}
	if number < s.config.Binding.FirstBuild || number > 2147483647 {
		return p, ErrConfiguration
	}
	status, e := s.Status(number)
	if e != nil {
		return p, e
	}
	if status.SourcePaused {
		return p, ErrSpoolPaused
	}
	r, e := s.read(number)
	if e != nil {
		return p, e
	}
	if (r.State != "delivered" && r.State != "compacted") || r.Delivered == nil {
		p.Reason = "delivery_not_completed"
		return p, nil
	}
	if r.Updated.After(s.now()) {
		return p, ErrSpoolState
	}
	if r.Delivered.Add(cleanupRetention).After(s.now()) {
		p.Reason = "retention_not_met"
		return p, nil
	}
	entry, e := s.collectorProof(collectorDir, number)
	if e != nil {
		if errors.Is(e, ErrCleanup) {
			p.Reason = "collector_not_ready"
			return p, nil
		}
		return p, e
	}
	if entry.Digest != r.Digest || entry.Handoff.Before(*r.Delivered) {
		return p, ErrSpoolConflict
	}
	// A retired intent can survive an interrupted collector unlink. Finish that
	// explicit operation before discarding the worker's full payload copy.
	if _, e = spoolRead(filepath.Join(s.config.InputDir, spoolName(number)), MaxBody); e == nil {
		p.Reason = "collector_input_present"
		return p, nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return p, e
	}
	ack, e := spoolRead(filepath.Join(s.config.AckDir, spoolName(number)), 1024)
	want, _ := json.Marshal(spoolAck{1, s.config.Binding.SourceID, number, r.Digest})
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			p.Reason = "ack_required"
			return p, nil
		}
		return p, e
	}
	if !bytes.Equal(ack, want) {
		return p, ErrSpoolConflict
	}
	p.Eligible = true
	p.checkedAt = s.now()
	p.AlreadyCompacted = r.State == "compacted"
	if !p.AlreadyCompacted {
		p.ReclaimBytes = int64(len(r.Payload))
	}
	return p, nil
}

// Cleanup atomically replaces only one acknowledged record with a tombstone.
// It never writes collector input/state, drops identity, or sends a request.
func (s *Spool) Cleanup(number int64, collectorDir string) (CleanupPlan, error) {
	if !s.writeable || s.failed || s.lock == nil {
		return CleanupPlan{}, ErrSpool
	}
	p, e := s.CleanupPlan(number, collectorDir)
	if e != nil || p.AlreadyCompacted {
		return p, e
	}
	if !p.Eligible {
		return p, ErrCleanup
	}
	r, e := s.read(number)
	if e != nil {
		return p, e
	}
	compactedAt := s.now()
	if compactedAt.Before(p.checkedAt) || compactedAt.Before(r.Delivered.Add(cleanupRetention)) || compactedAt.Before(r.Updated) {
		return p, ErrSpoolState
	}
	index := compactRecord{2, s.binding, s.config.Binding.SourceID, number, r.Digest, r.CIRunID, *r.Delivered, compactedAt}
	raw, _ := json.Marshal(index)
	_, size, e := s.inventory()
	if e != nil {
		return p, e
	}
	if size+int64(len(raw)) > MaxSpoolBytes {
		return p, ErrSpoolCapacity
	}
	if e = atomicSpoolWrite(filepath.Join(s.config.StateDir, spoolName(number)), raw, s.fault); e != nil {
		s.failed = true
		return p, e
	}
	return p, nil
}
