package contextpackage

import (
	"encoding/json"
	"testing"
	"time"
)

func stripPriorityFields(t *testing.T, b *Bundle) {
	t.Helper()
	rewritePackageFile(t, b, "objects.jsonl", func(raw []byte) []byte {
		var result []byte
		for _, o := range mustUnpackObjects(t, raw) {
			if o.Type == "ticket" {
				var v map[string]any
				if err := json.Unmarshal(o.Data, &v); err != nil {
					t.Fatal(err)
				}
				delete(v, "priority")
				delete(v, "priority_revision")
				o.Data = Encode(v)
			}
			result = append(result, Encode(o)...)
			result = append(result, '\n')
		}
		return result
	})
}
func TestPriorityV2ReadIsStrictAndPreservesManifest(t *testing.T) {
	b := fixture(t)
	b.Manifest.Profile = ProfileV2
	if _, _, err := Unpack(b); err == nil {
		t.Fatal("v2 accepted v3 fields")
	}
	stripPriorityFields(t, &b)
	stripDecisionReason(t, &b)
	before := Digest(b.Manifest)
	d, x, err := Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	v := x.Decoded[Ref{"ticket", "tkt_a"}].(Ticket)
	if v.Priority != "unset" || v.PriorityRevision != 1 || Digest(b.Manifest) != before {
		t.Fatal("legacy normalization drift")
	}
	plan, err := BuildPlan(b.Manifest, d, "wrk_target", Hash([]byte("identity")), "usr_owner", "00000000-0000-4000-8000-000000000001")
	if err != nil || plan.PackageDigest != before {
		t.Fatal("original digest lost", err)
	}
	b.Manifest.Profile = Profile
	if _, _, err = Unpack(b); err == nil {
		t.Fatal("v3 accepted missing fields")
	}
}
func priorityHistoryFixture(t *testing.T) (Manifest, Data) {
	t.Helper()
	b := fixture(t)
	d, _, err := Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	when := b.Manifest.CreatedAt.Add(time.Second)
	b.Manifest.CreatedAt = when
	for i, o := range d.Objects {
		if o.Type == "ticket" {
			var v Ticket
			if err = json.Unmarshal(o.Data, &v); err != nil {
				t.Fatal(err)
			}
			v.Priority = "high"
			v.PriorityRevision = 2
			d.Objects[i].Data = Encode(v)
			d.Objects[i].UpdatedAt = when
		}
	}
	d.History = append(d.History, History{ID: "evt_priority", Type: "ticket.priority-changed", Version: 1, Entity: Ref{"ticket", "tkt_a"}, Actor: "usr_a", Source: "web", OccurredAt: when, CorrelationID: "cor_priority", Facts: Encode(Facts{Priority: &Priority{From: "unset", To: "high", FromRevision: 1, ToRevision: 2}}), Lineage: []Origin{}})
	return b.Manifest, d
}
func TestPriorityHistorySequenceAndProfileBoundaries(t *testing.T) {
	m, d := priorityHistoryFixture(t)
	b, err := Pack(m, d)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{ProfileV1, ProfileV2} {
		legacy := b
		legacy.Files = append([]File(nil), b.Files...)
		legacy.Manifest.Files = append([]Descriptor(nil), b.Manifest.Files...)
		legacy.Manifest.Profile = profile
		stripPriorityFields(t, &legacy)
		if profile == ProfileV1 {
			rewritePackageFile(t, &legacy, "objects.jsonl", func(raw []byte) []byte {
				var result []byte
				for _, o := range mustUnpackObjects(t, raw) {
					if o.Type == "ticket" {
						var v map[string]any
						_ = json.Unmarshal(o.Data, &v)
						delete(v, "assignee_user_id")
						delete(v, "assignment_revision")
						o.Data = Encode(v)
					}
					result = append(result, Encode(o)...)
					result = append(result, '\n')
				}
				return result
			})
		}
		if _, _, err := Unpack(legacy); err == nil {
			t.Fatal("legacy accepted priority history", profile)
		}
	}
	for _, change := range []Priority{{From: "normal", To: "high", FromRevision: 1, ToRevision: 2}, {From: "unset", To: "high", FromRevision: 2, ToRevision: 3}, {From: "unset", To: "normal", FromRevision: 1, ToRevision: 2}, {From: "high", To: "high", FromRevision: 1, ToRevision: 2}} {
		_, d := priorityHistoryFixture(t)
		d.History[len(d.History)-1].Facts = Encode(Facts{Priority: &change})
		if _, err := Validate(d); err == nil {
			t.Fatal("inconsistent priority accepted", change)
		}
	}
	for _, missing := range []string{"priority", "priority_revision", "title", "status", "status_revision", "assignee_user_id", "assignment_revision"} {
		_, d := priorityHistoryFixture(t)
		for i, o := range d.Objects {
			if o.Type == "ticket" {
				var raw map[string]json.RawMessage
				_ = json.Unmarshal(o.Data, &raw)
				delete(raw, missing)
				d.Objects[i].Data = Encode(raw)
			}
		}
		if _, err := Validate(d); err == nil {
			t.Fatal("missing field accepted", missing)
		}
	}
	_, d = priorityHistoryFixture(t)
	for i := range d.History {
		if d.History[i].Type == "ticket.status-changed" {
			var f Facts
			_ = json.Unmarshal(d.History[i].Facts, &f)
			f.ToStatus = "done"
			d.History[i].Facts = Encode(f)
		}
	}
	for i, o := range d.Objects {
		if o.Type == "ticket" {
			var v Ticket
			_ = json.Unmarshal(o.Data, &v)
			v.Status = "done"
			d.Objects[i].Data = Encode(v)
		}
	}
	if _, err := Validate(d); err == nil {
		t.Fatal("priority after terminal accepted")
	}
}
func TestPriorityFactsRequireExactFields(t *testing.T) {
	h := History{Type: "ticket.priority-changed", Entity: Ref{"ticket", "tkt_a"}, Facts: json.RawMessage(`{"priority":{"from":"unset","to":"high","from_revision":1,"to_revision":2}}`)}
	if _, err := ValidateFacts(h); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"from", "to", "from_revision", "to_revision"} {
		var raw map[string]map[string]json.RawMessage
		_ = json.Unmarshal(h.Facts, &raw)
		delete(raw["priority"], field)
		broken := h
		broken.Facts = Encode(raw)
		if _, err := ValidateFacts(broken); err == nil {
			t.Fatal("missing field accepted", field)
		}
	}
	for _, raw := range []string{`{"priority":{"from":"unset","to":"high","from_revision":1,"to_revision":2,"extra":false}}`, `{"priority":{"from":"unset","from":"low","to":"high","from_revision":1,"to_revision":2}}`, `{"priority":{"from":"unset","to":"high","from_revision":"1","to_revision":2}}`} {
		h.Facts = json.RawMessage(raw)
		if _, err := ValidateFacts(h); err == nil {
			t.Fatal("malformed priority accepted", raw)
		}
	}
}
