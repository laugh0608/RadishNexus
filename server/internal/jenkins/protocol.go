// Package jenkins implements the narrow ADR-0030 delivery protocol. It does not
// fetch Jenkins URLs or execute builds; authentication precedes core commands.
package jenkins

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
)

const MaxBody = 16 * 1024
const PathPrefix = "/api/v1/integrations/jenkins/"

var tokenPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
var ciIDPattern = regexp.MustCompile(`^cir_[A-Za-z0-9_-]+$`)
var numberPattern = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
var utcPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,3})?Z$`)
var ErrInvalid = errors.New("invalid_request")
var ErrUnsupportedResult = errors.New("unsupported_result")
var ErrRunNotCompleted = errors.New("run_not_completed")
var ErrBinding = errors.New("source_binding_mismatch")

func Path(sourceID string) string { return PathPrefix + sourceID + "/deliveries" }

// object preserves explicit null, rejects unknown, missing and duplicate keys.
// All protocol and configuration objects are decoded through this function.
func object(body []byte, fields ...string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(body) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, ErrInvalid
	}
	values := make(map[string]json.RawMessage, len(fields))
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		key, ok := tok.(string)
		if !ok {
			return nil, ErrInvalid
		}
		if _, exists := values[key]; exists {
			return nil, ErrInvalid
		}
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, ErrInvalid
		}
		values[key] = v
	}
	if _, err := d.Token(); err != nil {
		return nil, ErrInvalid
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}
	if len(values) != len(fields) {
		return nil, ErrInvalid
	}
	for _, field := range fields {
		if _, ok := values[field]; !ok {
			return nil, ErrInvalid
		}
	}
	return values, nil
}
func decode(raw json.RawMessage, target any) error {
	if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, target) != nil {
		return ErrInvalid
	}
	return nil
}
func validJob(job string) bool {
	if job == "" || len(job) > 255 || strings.TrimSpace(job) != job {
		return false
	}
	for _, c := range job {
		if unicode.IsControl(c) || c == unicode.ReplacementChar {
			return false
		}
	}
	return true
}

type Payload struct {
	Version     int        `json:"version"`
	Job         string     `json:"job_full_name"`
	Number      int64      `json:"build_number"`
	Building    bool       `json:"building"`
	InProgress  bool       `json:"in_progress"`
	Result      string     `json:"result"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt time.Time  `json:"completed_at"`
}

func (p Payload) DeliveryID() string { return "build-" + strconv.FormatInt(p.Number, 10) }
func ParsePayload(body []byte, now time.Time) (Payload, error) {
	var p Payload
	if len(body) > MaxBody {
		return p, ErrInvalid
	}
	fields, err := object(body, "version", "job_full_name", "build_number", "building", "in_progress", "result", "started_at", "completed_at")
	if err != nil {
		return p, err
	}
	for key, target := range map[string]any{"version": &p.Version, "job_full_name": &p.Job, "build_number": &p.Number, "building": &p.Building, "in_progress": &p.InProgress} {
		if decode(fields[key], target) != nil {
			return p, ErrInvalid
		}
	}
	if p.Version != 1 || !validJob(p.Job) || !numberPattern.Match(fields["build_number"]) || p.Number > 2147483647 {
		return p, ErrInvalid
	}
	if p.Building || p.InProgress {
		return p, ErrRunNotCompleted
	}
	if decode(fields["result"], &p.Result) != nil {
		if bytes.Equal(fields["result"], []byte("null")) {
			return p, ErrUnsupportedResult
		}
		return p, ErrInvalid
	}
	if status(p.Result) == "" {
		return p, ErrUnsupportedResult
	}
	parseTime := func(raw json.RawMessage) (time.Time, error) {
		var v string
		if decode(raw, &v) != nil || !utcPattern.MatchString(v) {
			return time.Time{}, ErrInvalid
		}
		t, e := time.Parse(time.RFC3339Nano, v)
		if e != nil || t.IsZero() {
			return time.Time{}, ErrInvalid
		}
		return t, nil
	}
	p.CompletedAt, err = parseTime(fields["completed_at"])
	if err != nil || p.CompletedAt.After(now.Add(300*time.Second)) {
		return p, ErrInvalid
	}
	if !bytes.Equal(fields["started_at"], []byte("null")) {
		start, e := parseTime(fields["started_at"])
		if e != nil || start.After(p.CompletedAt) {
			return p, ErrInvalid
		}
		p.StartedAt = &start
	}
	return p, nil
}
func status(result string) string {
	switch result {
	case "SUCCESS":
		return "succeeded"
	case "FAILURE":
		return "failed"
	case "ABORTED":
		return "canceled"
	}
	return ""
}

// Map is called only after source signature verification and payload validation.
func (s Source) Map(p Payload, deliveryID string) (goldenpath.VerifiedJenkinsDelivery, goldenpath.RecordCompletedCIRunInput, error) {
	if p.Job != s.Job {
		return goldenpath.VerifiedJenkinsDelivery{}, goldenpath.RecordCompletedCIRunInput{}, ErrBinding
	}
	if p.DeliveryID() != deliveryID {
		return goldenpath.VerifiedJenkinsDelivery{}, goldenpath.RecordCompletedCIRunInput{}, ErrInvalid
	}
	canonical, _ := json.Marshal(struct {
		Version   int        `json:"version"`
		Workspace string     `json:"workspace_id"`
		Component string     `json:"component_id"`
		Source    string     `json:"source_id"`
		Job       string     `json:"job_full_name"`
		Number    int64      `json:"build_number"`
		Result    string     `json:"result"`
		Started   *time.Time `json:"started_at"`
		Completed time.Time  `json:"completed_at"`
	}{p.Version, s.WorkspaceID, s.ComponentID, s.ID, p.Job, p.Number, p.Result, p.StartedAt, p.CompletedAt})
	sum := sha256.Sum256(canonical)
	return goldenpath.VerifiedJenkinsDelivery{WorkspaceID: s.WorkspaceID, SourceID: s.ID, DeliveryID: deliveryID, PayloadSHA256: hex.EncodeToString(sum[:])}, goldenpath.RecordCompletedCIRunInput{ComponentID: s.ComponentID, ExternalRunKey: strconv.FormatInt(p.Number, 10), Status: status(p.Result), StartedAt: p.StartedAt, CompletedAt: p.CompletedAt}, nil
}
func signature(secret []byte, sourceID, keyID, deliveryID, timestamp string, body []byte) string {
	sum := sha256.Sum256(body)
	input := strings.Join([]string{"radishnexus-jenkins-v1", "POST", Path(sourceID), keyID, deliveryID, timestamp, hex.EncodeToString(sum[:])}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(input))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}
func (s Source) Verify(keyID, deliveryID, timestamp, sig string, body []byte, now time.Time) bool {
	key, ok := s.keys[keyID]
	if !ok {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || ts <= 0 || strconv.FormatInt(ts, 10) != timestamp || ts < now.Unix()-300 || ts > now.Unix()+300 {
		return false
	}
	number := strings.TrimPrefix(deliveryID, "build-")
	if deliveryID == number || !numberPattern.MatchString(number) {
		return false
	}
	expected := signature(key, s.ID, keyID, deliveryID, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(sig))
}
