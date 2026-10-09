package jenkins

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Import performs at most 100 new imports per call. Existing identities are
// checked against the original bytes, including already-delivered records.
func (s *Spool) Import() error {
	if !s.writeable || s.failed || s.lock == nil {
		return ErrSpool
	}
	numbers, _, e := s.inventory()
	if e != nil {
		return e
	}
	exists := make(map[int64]bool, len(numbers))
	for _, n := range numbers {
		exists[n] = true
	}
	names, _, e := boundedNames(s.config.InputDir)
	if e != nil {
		return e
	}
	sort.Strings(names)
	added := 0
	for _, name := range names {
		n, ok := spoolNumber(name)
		if !ok {
			continue
		}
		if !exists[n] && added >= 100 {
			continue
		}
		body, e := spoolRead(filepath.Join(s.config.InputDir, name), MaxBody)
		if e != nil {
			return e
		}
		if exists[n] {
			r, e := s.read(n)
			if e != nil {
				return e
			}
			if r.Digest != digestBytes(body) {
				// Preserve success facts but refuse all further sending until the
				// source conflict is resolved; never replace authoritative bytes.
				return ErrSpoolConflict
			}
			continue
		}
		if len(numbers)+added >= MaxSpoolEntries {
			return ErrSpoolCapacity
		}
		now := s.now()
		r := spoolRecord{Version: 1, Binding: s.binding, Number: n, Payload: body, Digest: digestBytes(body), State: "pending", Created: now, Updated: now, Next: now, Resets: []time.Time{}}
		p, parseErr := ParsePayload(body, now)
		if parseErr != nil {
			f := ClassifyDelivery(parseErr)
			r.State = "blocked"
			r.Reason = f.Code
			r.Disposition = f.Disposition
		}
		// An invalid document can leave partially decoded identity fields. Do
		// not reinterpret those as a proven source mismatch and pause the job.
		identityKnown := parseErr == nil || errors.Is(parseErr, ErrUnsupportedResult) || errors.Is(parseErr, ErrRunNotCompleted)
		if identityKnown && (p.Number != n || p.Job != s.config.Binding.Job) {
			r.State = "blocked"
			r.Reason = "source_binding_mismatch"
			r.Disposition = PauseSource
		}
		if e = s.save(r); e != nil {
			return e
		}
		added++
	}
	return nil
}

// Tick handles a single eligible delivery round; failure of a different item
// cannot starve an older due item. Send is injectable only through this private
// method in tests; the public path always uses the existing authenticated sender.
func (s *Spool) Tick(ctx context.Context, sender SenderConfig, client *http.Client) (bool, error) {
	if sender.SourceID != s.config.Binding.SourceID || sender.Origin != s.config.Binding.Origin {
		return false, ErrSpoolBinding
	}
	return s.tick(ctx, func(ctx context.Context, body []byte) (SendResult, error) { return Send(ctx, sender, body, client) })
}

func (s *Spool) tick(ctx context.Context, sendOne func(context.Context, []byte) (SendResult, error)) (bool, error) {
	if !s.writeable || s.failed || s.lock == nil {
		return false, ErrSpool
	}
	if e := s.Import(); e != nil {
		s.failed = true
		return false, e
	}
	numbers, _, e := s.inventory()
	if e != nil {
		return false, e
	}
	now := s.now()
	var chosen *spoolRecord
	paused := false
	for _, n := range numbers {
		r, e := s.read(n)
		if e != nil {
			s.failed = true
			return false, e
		}
		if r.State == "delivered" {
			if e = s.acknowledge(r); e != nil {
				return false, e
			}
			continue
		}
		if now.Before(r.Updated) {
			return false, ErrSpoolState
		}
		if r.State == "blocked" {
			if r.Disposition == PauseSource {
				paused = true
			}
			continue
		}
		if r.Rounds >= 12 || (r.First != nil && !now.Before(r.First.Add(24*time.Hour))) {
			r.State = "blocked"
			r.Reason = "retry_exhausted"
			r.Disposition = BlockDelivery
			r.Updated = now
			if e = s.save(r); e != nil {
				return false, e
			}
			continue
		}
		if r.Next.After(now) {
			continue
		}
		if chosen == nil || r.Next.Before(chosen.Next) {
			copy := r
			chosen = &copy
		}
	}
	if paused {
		return false, ErrSpoolPaused
	}
	if chosen == nil {
		return false, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	r := *chosen
	if r.First == nil {
		r.First = &now
	}
	r.Rounds++
	r.State = "in_flight"
	r.Reason = ""
	r.Disposition = ""
	r.HTTPStatus = 0
	r.Updated = now
	// A crash consumes this reserved round and cannot evade the persistent cap.
	if e = s.save(r); e != nil {
		return false, e
	}
	result, sendErr := sendOne(ctx, r.Payload)
	now = s.now()
	r.Updated = now
	if sendErr == nil {
		if !ciIDPattern.MatchString(result.CIRunID) {
			sendErr = ErrResponse
		} else {
			r.State = "delivered"
			r.CIRunID = result.CIRunID
			r.Delivered = &now
			if e = s.save(r); e != nil {
				return true, e
			}
			return true, s.acknowledge(r)
		}
	}
	f := ClassifyDelivery(sendErr)
	r.Reason = f.Code
	r.Disposition = f.Disposition
	r.HTTPStatus = f.HTTPStatus
	if f.Disposition == RetryDelivery && r.Rounds < 12 && now.Before(r.First.Add(24*time.Hour)) {
		r.State = "retry_wait"
		r.Next = now.Add(spoolDelay(r))
	} else {
		r.State = "blocked"
		if f.Disposition == RetryDelivery {
			r.Reason = "retry_exhausted"
			r.Disposition = BlockDelivery
		}
	}
	if e = s.save(r); e != nil {
		return true, e
	}
	if r.Disposition == PauseSource {
		return true, ErrSpoolPaused
	}
	return true, nil
}

func spoolDelay(r spoolRecord) time.Duration {
	base := time.Minute * time.Duration(1<<min(r.Rounds-1, 6))
	if base > time.Hour {
		base = time.Hour
	}
	// Stable per-input jitter survives restart; the final delay never exceeds 1h.
	sum := sha256.Sum256([]byte(r.Digest + string(rune(r.Rounds))))
	jitter := time.Duration(binary.BigEndian.Uint16(sum[:2])%1001) * base / 10000
	return min(base+jitter, time.Hour)
}

// Retry changes only the selected blocked item's budget, preserving identity
// and payload. Append-only bounded timestamps retain every manual reset.
func (s *Spool) Retry(number int64) error {
	if !s.writeable || s.failed || s.lock == nil {
		return ErrSpool
	}
	r, e := s.read(number)
	if e != nil {
		return e
	}
	if r.State != "blocked" || len(r.Resets) >= 128 {
		return ErrSpoolState
	}
	if r.Reason == "conflict" || r.Reason == "input_conflict" || r.Reason == "source_binding_mismatch" || r.Reason == "invalid_request" || r.Reason == "unsupported_result" || r.Reason == "run_not_completed" {
		return ErrSpoolState
	}
	if e = s.Import(); e != nil {
		return e
	}
	now := s.now()
	r.Resets = append(r.Resets, now)
	r.HTTPStatus = 0
	r.State = "pending"
	r.Reason = ""
	r.Disposition = ""
	r.Rounds = 0
	r.First = nil
	r.Next = now
	r.Updated = now
	return s.save(r)
}

type SpoolItem struct {
	BuildNumber   int64     `json:"build_number"`
	State         string    `json:"state"`
	Reason        string    `json:"reason"`
	Rounds        int       `json:"rounds"`
	ManualRetries int       `json:"manual_retries"`
	NextAttempt   time.Time `json:"next_attempt"`
	CIRunID       string    `json:"ci_run_id,omitempty"`
	HTTPStatus    int       `json:"last_http_status"`
}
type SpoolStatus struct {
	SourceID          string         `json:"source_id"`
	ObservedAt        time.Time      `json:"observed_at"`
	Counts            map[string]int `json:"counts"`
	Bytes             int64          `json:"bytes"`
	RemainingBytes    int64          `json:"remaining_bytes"`
	RemainingRecords  int            `json:"remaining_records"`
	OldestPending     *time.Time     `json:"oldest_pending_at"`
	LastDelivered     *time.Time     `json:"last_delivered_at"`
	SourcePaused      bool           `json:"source_paused"`
	Collector         string         `json:"collector"`
	CleanupCandidates int            `json:"cleanup_candidates"`
	CleanupEnabled    bool           `json:"cleanup_enabled"`
	Item              *SpoolItem     `json:"item,omitempty"`
}

// Status never loads credentials and never mutates state or acknowledges files.
// It is a bounded observation, not a transaction spanning the live worker.
func (s *Spool) Status(number int64) (SpoolStatus, error) {
	numbers, size, e := s.inventory()
	if e != nil {
		return SpoolStatus{}, e
	}
	now := s.now()
	status := SpoolStatus{SourceID: s.config.Binding.SourceID, ObservedAt: now, Counts: map[string]int{"pending": 0, "in_flight": 0, "retry_wait": 0, "blocked": 0, "delivered": 0}, Bytes: size, RemainingBytes: MaxSpoolBytes - size, RemainingRecords: MaxSpoolEntries - s.slots, Collector: "external_status_required"}
	for _, n := range numbers {
		r, e := s.read(n)
		if e != nil {
			return SpoolStatus{}, e
		}
		input, e := spoolRead(filepath.Join(s.config.InputDir, spoolName(n)), MaxBody)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return SpoolStatus{}, e
		}
		if e == nil && digestBytes(input) != r.Digest {
			return SpoolStatus{}, ErrSpoolConflict
		}
		status.Counts[r.State]++
		if r.State == "blocked" && r.Disposition == PauseSource {
			status.SourcePaused = true
		}
		if r.State != "delivered" && (status.OldestPending == nil || r.Created.Before(*status.OldestPending)) {
			copy := r.Created
			status.OldestPending = &copy
		}
		if r.Delivered != nil {
			if status.LastDelivered == nil || r.Delivered.After(*status.LastDelivered) {
				copy := *r.Delivered
				status.LastDelivered = &copy
			}
			if !r.Delivered.Add(7 * 24 * time.Hour).After(now) {
				status.CleanupCandidates++
			}
		}
		if n == number {
			status.Item = &SpoolItem{n, r.State, r.Reason, r.Rounds, len(r.Resets), r.Next, r.CIRunID, r.HTTPStatus}
		}
	}
	if number != 0 && status.Item == nil {
		return SpoolStatus{}, os.ErrNotExist
	}
	return status, nil
}

// SafeSpoolError deliberately excludes filesystem paths and secret references.
func SafeSpoolError(err error) string {
	for _, known := range []error{ErrConfiguration, ErrSpool, ErrSpoolState, ErrSpoolCapacity, ErrSpoolLocked, ErrSpoolBinding, ErrSpoolPaused, ErrSpoolConflict} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return "spool_item_not_found"
	}
	if errors.Is(err, context.Canceled) {
		return "worker_stopped"
	}
	return "spool_operation_failed"
}
