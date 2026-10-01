package jenkins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testPayload = `{"version":1,"job_full_name":"example/build","build_number":42,"building":false,"in_progress":false,"result":"SUCCESS","started_at":"2026-09-26T02:00:00Z","completed_at":"2026-09-26T02:01:00Z"}`

var testNow = time.Date(2026, 9, 26, 2, 2, 0, 0, time.UTC)

const testConfig = `{"version":1,"sources":[{"source_id":"source_a","workspace_id":"wrk_main","component_id":"cmp_auth","job_full_name":"example/build","keys":[{"key_id":"key_a","secret_file":"/synthetic/key"}]}]}`

func testRead(_ string, _ int64) ([]byte, error) { return []byte(strings.Repeat("ab", 32)), nil }
func testSource(t *testing.T) Source {
	t.Helper()
	s, e := LoadSources([]byte(testConfig), testRead)
	if e != nil {
		t.Fatal(e)
	}
	return s[0]
}
func TestPayloadFailureBoundaries(t *testing.T) {
	cases := []struct {
		name, body string
		want       error
	}{
		{"unknown", strings.Replace(testPayload, `"version":1`, `"extra":true,"version":1`, 1), ErrInvalid},
		{"duplicate", strings.Replace(testPayload, `"version":1`, `"version":1,"version":1`, 1), ErrInvalid},
		{"missing", strings.Replace(testPayload, `"building":false,`, "", 1), ErrInvalid},
		{"null bool", strings.Replace(testPayload, `"building":false`, `"building":null`, 1), ErrInvalid},
		{"case", strings.Replace(testPayload, `"version"`, `"Version"`, 1), ErrInvalid},
		{"trailing", testPayload + `{}`, ErrInvalid},
		{"utf8", strings.Replace(testPayload, "example", string([]byte{0xff}), 1), ErrInvalid},
		{"surrogate", strings.Replace(testPayload, "example", `\ud800`, 1), ErrInvalid},
		{"job whitespace", strings.Replace(testPayload, "example/build", " example/build", 1), ErrInvalid},
		{"exponent", strings.Replace(testPayload, ":42,", ":4.2e1,", 1), ErrInvalid},
		{"fraction", strings.Replace(testPayload, ":42,", ":42.0,", 1), ErrInvalid},
		{"overflow", strings.Replace(testPayload, ":42,", ":2147483648,", 1), ErrInvalid},
		{"zero", strings.Replace(testPayload, ":42,", ":0,", 1), ErrInvalid},
		{"running", strings.Replace(testPayload, `"building":false`, `"building":true`, 1), ErrRunNotCompleted},
		{"post production", strings.Replace(testPayload, `"in_progress":false`, `"in_progress":true`, 1), ErrRunNotCompleted},
		{"unstable", strings.Replace(testPayload, "SUCCESS", "UNSTABLE", 1), ErrUnsupportedResult},
		{"not built", strings.Replace(testPayload, "SUCCESS", "NOT_BUILT", 1), ErrUnsupportedResult},
		{"null result", strings.Replace(testPayload, `"SUCCESS"`, `null`, 1), ErrUnsupportedResult},
		{"unknown result", strings.Replace(testPayload, "SUCCESS", "UNKNOWN", 1), ErrUnsupportedResult},
		{"offset", strings.ReplaceAll(testPayload, "Z", "+00:00"), ErrInvalid},
		{"calendar", strings.ReplaceAll(testPayload, "09-26", "02-30"), ErrInvalid},
		{"precision", strings.Replace(testPayload, "02:00:00Z", "02:00:00.0001Z", 1), ErrInvalid},
		{"future", strings.Replace(testPayload, "02:01:00Z", "02:08:00Z", 1), ErrInvalid},
		{"reversed", strings.Replace(testPayload, "02:00:00Z", "02:02:00Z", 1), ErrInvalid},
		{"oversize", testPayload + strings.Repeat(" ", MaxBody), ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParsePayload([]byte(tc.body), testNow); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}
func TestMappingAndCanonicalReceipt(t *testing.T) {
	source := testSource(t)
	parse := func(body string) Payload {
		t.Helper()
		p, e := ParsePayload([]byte(body), testNow)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	p := parse(testPayload)
	delivery, input, err := source.Map(p, "build-42")
	if err != nil || input.Status != "succeeded" || input.ExternalRunKey != "42" || delivery.WorkspaceID != "wrk_main" {
		t.Fatal(delivery, input, err)
	}
	variant := strings.ReplaceAll(testPayload, "Z", ".000Z")
	var formatted bytes.Buffer
	_ = json.Indent(&formatted, []byte(variant), "", "  ")
	same, _, _ := source.Map(parse(formatted.String()), "build-42")
	if same != delivery {
		t.Fatal("equivalent facts changed digest")
	}
	for result, want := range map[string]string{"FAILURE": "failed", "ABORTED": "canceled"} {
		other, actual, e := source.Map(parse(strings.Replace(testPayload, "SUCCESS", result, 1)), "build-42")
		if e != nil || actual.Status != want || other.PayloadSHA256 == delivery.PayloadSHA256 {
			t.Fatal(result)
		}
	}
	nullable := parse(strings.Replace(testPayload, `"2026-09-26T02:00:00Z"`, `null`, 1))
	if nullable.StartedAt != nil {
		t.Fatal("nullable start")
	}
	if _, _, e := source.Map(p, "build-43"); e != ErrInvalid {
		t.Fatal(e)
	}
	source.Job = "other"
	if _, _, e := source.Map(p, "build-42"); e != ErrBinding {
		t.Fatal(e)
	}
	source = testSource(t)
	source.ComponentID = "cmp_other"
	changed, _, _ := source.Map(p, "build-42")
	if changed.PayloadSHA256 == delivery.PayloadSHA256 {
		t.Fatal("binding excluded from digest")
	}
}
func TestSignatureWindowAndScope(t *testing.T) {
	s := testSource(t)
	body := []byte(testPayload)
	for _, delta := range []int64{-301, -300, 0, 300, 301} {
		stamp := fmt.Sprint(testNow.Unix() + delta)
		sig := signature(s.keys["key_a"], s.ID, "key_a", "build-42", stamp, body)
		if s.Verify("key_a", "build-42", stamp, sig, body, testNow) != (delta >= -300 && delta <= 300) {
			t.Fatal(delta)
		}
	}
	stamp := fmt.Sprint(testNow.Unix())
	sig := signature(s.keys["key_a"], s.ID, "key_a", "build-42", stamp, body)
	for _, tc := range []struct {
		key, delivery, stamp, sig string
		body                      []byte
	}{
		{"unknown", "build-42", stamp, sig, body}, {"key_a", "build-43", stamp, sig, body},
		{"key_a", "build-42", "0" + stamp, sig, body}, {"key_a", "build-42", stamp, strings.ToUpper(sig), body},
		{"key_a", "build-42", stamp, sig, append(body, ' ')},
	} {
		if s.Verify(tc.key, tc.delivery, tc.stamp, tc.sig, tc.body, testNow) {
			t.Fatal("accepted altered request")
		}
	}
	s.ID = "source_b"
	if s.Verify("key_a", "build-42", stamp, sig, body, testNow) {
		t.Fatal("cross source signature")
	}
}
func TestConfigurationFailsClosedAndRedacts(t *testing.T) {
	for _, body := range []string{
		strings.Replace(testConfig, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(testConfig, `"key_id":"key_a"`, `"key_id":"key_a","key_id":"key_b"`, 1),
		strings.Replace(testConfig, `"source_id"`, `"Source_ID"`, 1),
		strings.Replace(testConfig, `"keys":[`, `"extra":true,"keys":[`, 1),
		strings.Replace(testConfig, "/synthetic/key", "relative", 1),
		strings.Replace(testConfig, "wrk_main", "cmp_main", 1),
		strings.Replace(testConfig, "cmp_auth", "wrk_auth", 1),
		strings.Replace(testConfig, "source_a", "bad/source", 1),
		strings.Replace(testConfig, `"version":1`, `"version":2`, 1),
		`{"version":1,"sources":[]}`, `{"version":1,"sources":null}`,
		testConfig + strings.Repeat(" ", MaxConfig),
	} {
		if _, e := LoadSources([]byte(body), testRead); e != ErrConfiguration {
			t.Fatal("accepted bad config", e)
		}
	}
	for _, value := range []string{"sensitive-invalid-key", strings.Repeat("AB", 32), strings.Repeat("ab", 32) + "\n\n", strings.Repeat("ab", 32) + "\r"} {
		_, e := LoadSources([]byte(testConfig), func(string, int64) ([]byte, error) { return []byte(value), nil })
		if e != ErrConfiguration || strings.Contains(e.Error(), value) {
			t.Fatal("key validation/redaction")
		}
	}
	rotation := strings.Replace(testConfig, `"secret_file":"/synthetic/key"}`, `"secret_file":"/synthetic/key"},{"key_id":"key_b","secret_file":"/synthetic/new"}`, 1)
	sources, e := LoadSources([]byte(rotation), testRead)
	if e != nil || len(sources[0].keys) != 2 {
		t.Fatal(e)
	}
	duplicate := strings.Replace(rotation, `"key_b"`, `"key_a"`, 1)
	if _, e := LoadSources([]byte(duplicate), testRead); e != ErrConfiguration {
		t.Fatal(e)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", sources[0], sources[0]), "171") {
		t.Fatal("secret in formatter")
	}
}
func TestBoundedFileAndSenderConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if e := os.WriteFile(path, []byte("12345"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadFile(path, 4); e != ErrConfiguration {
		t.Fatal(e)
	}
	if _, e := ReadFile("relative", 4); e != ErrConfiguration {
		t.Fatal(e)
	}
	config := `{"version":1,"origin":"https://nexus.example","source_id":"source_a","key_id":"key_a","secret_file":"/synthetic/key"}`
	if _, e := LoadSender([]byte(config), testRead); e != nil {
		t.Fatal(e)
	}
	for _, origin := range []string{"http://nexus.example", "https://user:secret@nexus.example", "https://nexus.example/", "https://nexus.example?", "https://nexus.example/#x"} {
		if _, e := LoadSender([]byte(strings.Replace(config, "https://nexus.example", origin, 1)), testRead); e != ErrConfiguration {
			t.Fatal(origin, e)
		}
	}
}
