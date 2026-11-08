package contextpackage

import (
	"strings"
	"testing"
	"time"
)

func TestDecisionReviewPackageProfileAndHistoricalVersion(t *testing.T) {
	bundle := fixture(t)
	bundle.Manifest.Profile = ProfileV5
	digest := Digest(bundle.Manifest)
	data, _, err := Unpack(bundle)
	if err != nil || Digest(bundle.Manifest) != digest {
		t.Fatalf("legacy v5: %v", err)
	}

	review := History{
		ID: "evt_review", Type: "decision.reviewed", Version: 1, Entity: Ref{"decision", "dec_a"},
		Actor: "usr_a", Source: "web", OccurredAt: bundle.Manifest.CreatedAt.Add(-time.Second),
		CorrelationID: "cor_review", Lineage: []Origin{},
		Facts: Encode(DecisionReview{DecisionRevision: 2, Trigger: "manual", Conclusion: "still_applicable", Reason: "Evidence reviewed."}),
	}
	data.History = append(data.History, review)
	current, err := Pack(bundle.Manifest, data)
	if err != nil || current.Manifest.Profile != Profile {
		t.Fatalf("v6 review history: %v", err)
	}
	current.Manifest.Profile = ProfileV5
	if _, _, err := Unpack(current); err == nil {
		t.Fatal("v5 accepted a v6 review fact")
	}
	for _, mutation := range []struct{ from, to string }{
		{`"decision_revision":2`, `"decision_revision":3`},
		{`"trigger"`, `"Trigger"`},
		{`"Evidence reviewed."`, `"\ud800"`},
		{`"additional_evidence":null`, `"additional_evidence":{"type":"thread","id":"thr_missing"}`},
		{`"additional_evidence":null`, `"additional_evidence":{"type":"thread","id":"thr_a"}`},
	} {
		bad := review
		bad.Facts = []byte(strings.Replace(string(review.Facts), mutation.from, mutation.to, 1))
		data.History[len(data.History)-1] = bad
		if _, err := Pack(bundle.Manifest, data); err == nil {
			t.Fatalf("invalid review accepted: %s", mutation.to)
		}
	}
}
