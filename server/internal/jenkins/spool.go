package jenkins

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrSpoolPaused = errors.New("spool_source_paused")
var ErrSpoolConflict = errors.New("spool_input_conflict")

type spoolRecord struct {
	Version     int         `json:"version"`
	Binding     string      `json:"binding_sha256"`
	Number      int64       `json:"build_number"`
	Payload     []byte      `json:"payload"`
	Digest      string      `json:"payload_sha256"`
	State       string      `json:"state"`
	Reason      string      `json:"reason"`
	Disposition string      `json:"disposition"`
	HTTPStatus  int         `json:"last_http_status"`
	Rounds      int         `json:"rounds"`
	First       *time.Time  `json:"first_attempt"`
	Next        time.Time   `json:"next_attempt"`
	Created     time.Time   `json:"created_at"`
	Updated     time.Time   `json:"updated_at"`
	Delivered   *time.Time  `json:"delivered_at"`
	CIRunID     string      `json:"ci_run_id"`
	Resets      []time.Time `json:"manual_retries"`
}

type spoolAck struct {
	Version  int    `json:"version"`
	SourceID string `json:"source_id"`
	Number   int64  `json:"build_number"`
	Digest   string `json:"payload_sha256"`
}

// Spool is single-owner and not safe for concurrent method calls. Inspection
// opens a separate read-only view; atomic files make each record consistent.
type Spool struct {
	config    SpoolConfig
	lock      *os.File
	binding   string
	now       func() time.Time
	fault     func(string) error
	writeable bool
	failed    bool
	slots     int
}

func digestBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func spoolName(number int64) string { return "build-" + strconv.FormatInt(number, 10) + ".json" }
func spoolNumber(name string) (int64, bool) {
	if !strings.HasPrefix(name, "build-") || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(name, "build-"), ".json")
	n, e := strconv.ParseInt(raw, 10, 64)
	return n, e == nil && n > 0 && n <= 2147483647 && strconv.FormatInt(n, 10) == raw
}

// init is explicit and offline. It never adopts a nonempty state or ack folder.
// run and inspect never recreate a missing manifest, even when inputs exist.
func OpenSpool(c SpoolConfig, mode string) (*Spool, error) {
	if mode != "init" && mode != "run" && mode != "inspect" {
		return nil, ErrConfiguration
	}
	raw, e := json.Marshal(c)
	if e != nil {
		return nil, ErrConfiguration
	}
	if _, e = LoadSpoolConfig(raw); e != nil {
		return nil, e
	}
	if e = spoolDirectory(c.InputDir); e != nil {
		return nil, e
	}
	for _, dir := range []string{c.StateDir, c.AckDir} {
		if mode == "init" {
			if e = os.Mkdir(dir, 0700); e != nil && !errors.Is(e, os.ErrExist) {
				return nil, ErrSpool
			}
		}
		if e = spoolDirectory(dir); e != nil {
			return nil, e
		}
	}
	bindingRaw, _ := json.Marshal(c.Binding)
	s := &Spool{config: c, binding: digestBytes(bindingRaw), now: func() time.Time { return time.Now().UTC() }, writeable: mode != "inspect"}
	if s.writeable {
		f, e := spoolOpen(filepath.Join(c.StateDir, ".lock"), os.O_CREATE|os.O_RDWR)
		if e != nil {
			return nil, ErrSpool
		}
		s.lock = f
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !spoolOwned(info) {
			s.Close()
			return nil, ErrSpoolState
		}
		if e = spoolLock(f); e != nil {
			s.Close()
			return nil, e
		}
	}
	fail := func(e error) (*Spool, error) { s.Close(); return nil, e }
	path := filepath.Join(c.StateDir, "manifest.json")
	manifest, e := spoolRead(path, MaxConfig)
	if errors.Is(e, os.ErrNotExist) && mode == "init" {
		names, _, e := boundedNames(c.StateDir)
		if e != nil || len(names) != 1 || names[0] != ".lock" {
			return fail(ErrSpoolState)
		}
		names, _, e = boundedNames(c.AckDir)
		if e != nil || len(names) != 0 {
			return fail(ErrSpoolState)
		}
		if e = atomicSpoolWrite(path, bindingRaw, nil); e != nil {
			return fail(e)
		}
		// Persist newly created directories in their parents as well.
		for _, dir := range []string{c.StateDir, c.AckDir} {
			p, e := os.Open(filepath.Dir(dir))
			if e != nil {
				return fail(ErrSpool)
			}
			e = p.Sync()
			p.Close()
			if e != nil {
				return fail(ErrSpool)
			}
		}
	} else {
		if e != nil {
			return fail(ErrSpoolState)
		}
		b, e := parseBinding(manifest)
		if e != nil || b != c.Binding {
			return fail(ErrSpoolBinding)
		}
	}
	if _, _, e = s.inventory(); e != nil {
		return fail(e)
	}
	return s, nil
}

func (s *Spool) Close() error {
	if s.lock != nil {
		e := s.lock.Close()
		s.lock = nil
		return e
	}
	return nil
}

func (s *Spool) inventory() ([]int64, int64, error) {
	raw, err := spoolRead(filepath.Join(s.config.StateDir, "manifest.json"), MaxConfig)
	if err != nil {
		return nil, 0, ErrSpoolState
	}
	b, err := parseBinding(raw)
	if err != nil || b != s.config.Binding {
		return nil, 0, ErrSpoolBinding
	}
	var numbers []int64
	var total int64
	identities := map[int64]bool{}
	temporary := 0
	for index, dir := range []string{s.config.InputDir, s.config.StateDir, s.config.AckDir} {
		if e := spoolDirectory(dir); e != nil {
			return nil, 0, e
		}
		names, size, e := boundedNames(dir)
		if e != nil {
			return nil, 0, e
		}
		total += size
		for _, name := range names {
			if strings.HasPrefix(name, ".pending-") {
				temporary++
				continue
			}
			if index == 1 && (name == ".lock" || name == "manifest.json") {
				continue
			}
			if index == 0 && strings.HasSuffix(name, ".json.tmp") {
				if _, ok := spoolNumber(strings.TrimSuffix(name, ".tmp")); ok {
					temporary++
					continue
				}
			}
			n, ok := spoolNumber(name)
			if !ok || n < s.config.Binding.FirstBuild {
				return nil, 0, ErrSpoolState
			}
			identities[n] = true
			if index == 1 {
				numbers = append(numbers, n)
			}
			if index == 2 {
				r, e := s.read(n)
				if e != nil || r.State != "delivered" {
					return nil, 0, ErrSpoolState
				}
				want, _ := json.Marshal(spoolAck{1, s.config.Binding.SourceID, n, r.Digest})
				ack, e := spoolRead(filepath.Join(dir, name), 1024)
				if e != nil || !bytes.Equal(ack, want) {
					return nil, 0, ErrSpoolState
				}
			}
		}
	}
	if len(identities)+temporary > MaxSpoolEntries || total > MaxSpoolBytes {
		return nil, 0, ErrSpoolCapacity
	}
	s.slots = len(identities) + temporary
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	return numbers, total, nil
}

func (s *Spool) read(number int64) (spoolRecord, error) {
	var r spoolRecord
	raw, e := spoolRead(filepath.Join(s.config.StateDir, spoolName(number)), MaxSpoolRecord)
	if e != nil {
		return r, e
	}
	if _, e = object(raw, "version", "binding_sha256", "build_number", "payload", "payload_sha256", "state", "reason", "disposition", "last_http_status", "rounds", "first_attempt", "next_attempt", "created_at", "updated_at", "delivered_at", "ci_run_id", "manual_retries"); e != nil || json.Unmarshal(raw, &r) != nil {
		return r, ErrSpoolState
	}
	if r.HTTPStatus != 0 && (r.HTTPStatus < 100 || r.HTTPStatus > 599) {
		return r, ErrSpoolState
	}
	if r.Version != 1 || r.Binding != s.binding || r.Number != number || len(r.Payload) > MaxBody || r.Digest != digestBytes(r.Payload) || r.Rounds < 0 || r.Rounds > 12 || len(r.Resets) > 128 || r.Created.IsZero() || r.Updated.Before(r.Created) || r.Next.IsZero() {
		return r, ErrSpoolState
	}
	if (r.First == nil) != (r.Rounds == 0) || (r.First != nil && (r.First.IsZero() || r.First.Before(r.Created))) {
		return r, ErrSpoolState
	}
	switch r.State {
	case "pending":
		if r.Rounds != 0 || r.Reason != "" || r.Disposition != "" {
			return r, ErrSpoolState
		}
	case "in_flight":
		if r.Rounds < 1 || r.Reason != "" || r.Disposition != "" {
			return r, ErrSpoolState
		}
	case "retry_wait":
		if r.Rounds < 1 || r.Disposition != RetryDelivery || !safeSpoolReason(r.Reason) {
			return r, ErrSpoolState
		}
	case "blocked":
		if !safeSpoolReason(r.Reason) || (r.Disposition != BlockDelivery && r.Disposition != PauseSource) {
			return r, ErrSpoolState
		}
	case "delivered":
		if r.Rounds < 1 || r.Delivered == nil || r.Delivered.Before(r.Created) || !ciIDPattern.MatchString(r.CIRunID) || r.Reason != "" || r.Disposition != "" {
			return r, ErrSpoolState
		}
	default:
		return r, ErrSpoolState
	}
	if r.State != "delivered" && (r.Delivered != nil || r.CIRunID != "") {
		return r, ErrSpoolState
	}
	if r.State != "blocked" {
		p, e := ParsePayload(r.Payload, s.now())
		if e != nil || p.Job != s.config.Binding.Job || p.Number != r.Number {
			return r, ErrSpoolState
		}
	}
	return r, nil
}

func safeSpoolReason(code string) bool {
	switch code {
	case "delivery_failed", "invalid_delivery_response", "tls_verification_failed", "invalid_configuration", "invalid_request", "unsupported_result", "run_not_completed", "source_binding_mismatch", "conflict", "not_found", "unauthenticated", "secure_transport_required", "invalid_origin", "invalid_proxy_chain", "method_not_allowed", "payload_too_large", "unsupported_media_type", "retry_exhausted", "input_conflict":
		return true
	}
	return false
}

func (s *Spool) save(r spoolRecord) error {
	if !s.writeable || s.lock == nil || s.failed {
		return ErrSpool
	}
	_, size, e := s.inventory()
	if e != nil {
		s.failed = true
		return e
	}
	if size+MaxSpoolRecord > MaxSpoolBytes {
		s.failed = true
		return ErrSpoolCapacity
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return ErrSpoolState
	}
	if e = atomicSpoolWrite(filepath.Join(s.config.StateDir, spoolName(r.Number)), raw, s.fault); e != nil {
		s.failed = true
		return e
	}
	return nil
}

func (s *Spool) acknowledge(r spoolRecord) error {
	want := spoolAck{1, s.config.Binding.SourceID, r.Number, r.Digest}
	raw, _ := json.Marshal(want)
	path := filepath.Join(s.config.AckDir, spoolName(r.Number))
	existing, e := spoolRead(path, 1024)
	if e == nil {
		if !bytes.Equal(existing, raw) {
			return ErrSpoolState
		}
		return nil
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if !s.writeable || s.failed {
		return ErrSpool
	}
	_, size, e := s.inventory()
	if e != nil {
		return e
	}
	if size+int64(len(raw)) > MaxSpoolBytes {
		return ErrSpoolCapacity
	}
	if e = atomicSpoolWrite(path, raw, s.fault); e != nil {
		s.failed = true
		return e
	}
	return nil
}
