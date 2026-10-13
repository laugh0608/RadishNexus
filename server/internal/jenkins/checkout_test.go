package jenkins

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const checkoutJSON = `{"binding_id":"checkout_a","state":"observed","reason":null,"revision":{"algorithm":"sha1","oid":"1234567890abcdef1234567890abcdef12345678"}}`
const bindingJSON = `{"binding_id":"checkout_a","repository_id":"rep_source","provider":"gitea","provider_origin":"https://git.example.test","external_id":"123","profile":"jenkins-git-single-checkout-v1","effective_from_build":42}`

func checkoutPayload() string {
	return strings.TrimSuffix(strings.Replace(testPayload, `"version":1`, `"version":2`, 1), "}") + `,"checkout":` + checkoutJSON + `}`
}
func checkoutSource(t *testing.T) Source {
	t.Helper()
	body := strings.Replace(strings.Replace(testConfig, `"version":1`, `"version":2`, 1), `"keys":`, `"checkout_binding":`+bindingJSON+`,"keys":`, 1)
	sources, err := LoadSources([]byte(body), testRead)
	if err != nil {
		t.Fatal(err)
	}
	return sources[0]
}
func TestCheckoutClosedSchemaAndVersionBoundary(t *testing.T) {
	s := checkoutSource(t)
	p, e := ParsePayload([]byte(checkoutPayload()), testNow)
	if e != nil {
		t.Fatal(e)
	}
	d, in, e := s.Map(p, "build-42")
	if e != nil || in.Checkout.State != "observed" {
		t.Fatal(e)
	}
	var pretty bytes.Buffer
	_ = json.Indent(&pretty, []byte(checkoutPayload()), "", "  ")
	q, e := ParsePayload(pretty.Bytes(), testNow)
	if e != nil {
		t.Fatal(e)
	}
	same, _, e := s.Map(q, "build-42")
	if e != nil || same.PayloadSHA256 != d.PayloadSHA256 {
		t.Fatal("canonical whitespace", e)
	}
	old, _ := ParsePayload([]byte(testPayload), testNow)
	if _, _, e = s.Map(old, "build-42"); e != ErrInvalid {
		t.Fatal("v1 at boundary", e)
	}
	old.Number = 41
	legacy, _, e := testSource(t).Map(old, "build-41")
	if e != nil {
		t.Fatal(e)
	}
	upgraded, _, e := s.Map(old, "build-41")
	if e != nil || legacy.PayloadSHA256 != upgraded.PayloadSHA256 {
		t.Fatal("legacy digest changed", e)
	}
	p.Number = 41
	if _, _, e = s.Map(p, "build-41"); e != ErrInvalid {
		t.Fatal("v2 before boundary", e)
	}
	p.Number = 42
	p.Checkout.BindingID = "other"
	if _, _, e = s.Map(p, "build-42"); e != ErrBinding {
		t.Fatal(e)
	}
	if _, _, e = testSource(t).Map(q, "build-42"); e != ErrInvalid {
		t.Fatal("v1 source accepted v2", e)
	}
	for _, bad := range []string{
		strings.Replace(checkoutPayload(), `"reason":null,`, "", 1),
		strings.Replace(checkoutPayload(), `"state":"observed"`, `"state":"observed","state":"observed"`, 1),
		strings.Replace(checkoutPayload(), `"algorithm":"sha1"`, `"algorithm":"sha256"`, 1),
		strings.Replace(checkoutPayload(), `1234567890abcdef1234567890abcdef12345678`, strings.Repeat("0", 40), 1),
		strings.Replace(checkoutPayload(), `1234567890abcdef1234567890abcdef12345678`, "abcdef", 1),
		strings.Replace(checkoutPayload(), `"reason":null`, `"reason":"checkout_not_observed"`, 1),
		strings.Replace(checkoutPayload(), `"state":"observed"`, `"state":"unavailable"`, 1),
		strings.Replace(checkoutPayload(), `"revision":{`, `"extra":true,"revision":{`, 1),
	} {
		if _, e := ParsePayload([]byte(bad), testNow); e != ErrInvalid {
			t.Fatal("accepted malformed checkout", e)
		}
	}
	for _, reason := range []string{"checkout_not_observed", "unsupported_checkout", "binding_changed_during_run"} {
		body := strings.Replace(checkoutPayload(), checkoutJSON, `{"binding_id":"checkout_a","state":"unavailable","reason":"`+reason+`","revision":null}`, 1)
		p, e := ParsePayload([]byte(body), testNow)
		if e != nil {
			t.Fatal(e)
		}
		other, _, e := s.Map(p, "build-42")
		if e != nil || other.PayloadSHA256 == d.PayloadSHA256 {
			t.Fatal("missing state digest", e)
		}
	}
}

func TestLegacyCheckoutDigestGolden(t *testing.T) {
	p, e := ParsePayload([]byte(testPayload), testNow)
	if e != nil {
		t.Fatal(e)
	}
	d, _, e := testSource(t).Map(p, "build-42")
	if e != nil || d.PayloadSHA256 != "259303a5227348709f1a4fda1b6ee0601606f69cbaee267bc0e915817fb7bae2" {
		t.Fatal("v1 canonical bytes changed", e, d.PayloadSHA256)
	}
}
