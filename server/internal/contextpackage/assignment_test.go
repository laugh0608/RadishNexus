package contextpackage

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func rewritePackageFile(t *testing.T, b *Bundle, path string, transform func([]byte) []byte) {
	t.Helper()
	for i := range b.Files {
		if b.Files[i].Path != path {
			continue
		}
		b.Files[i].Content = transform(b.Files[i].Content)
		for j := range b.Manifest.Files {
			if b.Manifest.Files[j].Path == path {
				b.Manifest.Files[j].Bytes = int64(len(b.Files[i].Content))
				b.Manifest.Files[j].SHA256 = Hash(b.Files[i].Content)
			}
		}
		return
	}
	t.Fatal("file missing")
}
func TestAssignmentV1ReadIsStrictAndPreservesManifest(t *testing.T) {
	b := fixture(t)
	b.Manifest.Profile = ProfileV1
	if _, _, err := Unpack(b); err == nil {
		t.Fatal("v1 accepted new fields")
	}
	rewritePackageFile(t, &b, "objects.jsonl", func(raw []byte) []byte {
		var result []byte
		for _, o := range mustUnpackObjects(t, raw) {
			if o.Type == "ticket" {
				var v map[string]any
				if err := json.Unmarshal(o.Data, &v); err != nil {
					t.Fatal(err)
				}
				delete(v, "assignee_user_id")
				delete(v, "assignment_revision")
				delete(v, "priority")
				delete(v, "priority_revision")
				o.Data = Encode(v)
			}
			result = append(result, Encode(o)...)
			result = append(result, '\n')
		}
		return result
	})
	stripDecisionReason(t, &b)
	before := Digest(b.Manifest)
	d, x, err := Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	v := x.Decoded[Ref{"ticket", "tkt_a"}].(Ticket)
	if v.AssigneeUserID != nil || v.AssignmentRevision != 1 || Digest(b.Manifest) != before {
		t.Fatal("v1 normalization drift")
	}
	p, err := BuildPlan(b.Manifest, d, "wrk_target", Hash([]byte("identity")), "usr_owner", "00000000-0000-4000-8000-000000000001")
	if err != nil || p.PackageDigest != before {
		t.Fatal("plan lost original digest", err)
	}
	b.Manifest.Profile = Profile
	if _, _, err := Unpack(b); err == nil {
		t.Fatal("v3 accepted missing fields")
	}
}
func mustUnpackObjects(t *testing.T, raw []byte) []Object {
	t.Helper()
	var out []Object
	if err := readLines(raw, bytes.Count(raw, []byte{'\n'}), &out, MaxObjects); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestAssignmentHistoryClosureAndCurrentState(t *testing.T) {
	b := fixture(t)
	d, _, err := Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	actor := "usr_target"
	d.Actors = append(d.Actors, Actor{ID: actor, Lineage: []Origin{}})
	when := b.Manifest.CreatedAt.Add(time.Second)
	b.Manifest.CreatedAt = when
	for i, o := range d.Objects {
		if o.Type == "ticket" {
			var v Ticket
			if err = json.Unmarshal(o.Data, &v); err != nil {
				t.Fatal(err)
			}
			v.AssigneeUserID = &actor
			v.AssignmentRevision = 2
			d.Objects[i].Data = Encode(v)
			d.Objects[i].UpdatedAt = when
		}
	}
	d.History = append(d.History, History{ID: "evt_assign", Type: "ticket.assignee-changed", Version: 1, Entity: Ref{"ticket", "tkt_a"}, Actor: "usr_a", Source: "web", OccurredAt: when, CorrelationID: "cor_a", Facts: Encode(Facts{Assignment: &Assignment{ToUserID: &actor, FromRevision: 1, ToRevision: 2}}), Lineage: []Origin{}})
	if _, err = Pack(b.Manifest, d); err != nil {
		t.Fatal(err)
	}
	original := d.History[len(d.History)-1]
	for _, change := range []Assignment{{ToUserID: &actor, FromRevision: 2, ToRevision: 3}, {FromUserID: &actor, ToUserID: &actor, FromRevision: 1, ToRevision: 2}, {FromUserID: &actor, FromRevision: 1, ToRevision: 2}} {
		d.History[len(d.History)-1].Facts = Encode(Facts{Assignment: &change})
		if _, err = Validate(d); err == nil {
			t.Fatal("inconsistent history accepted")
		}
	}
	d.History[len(d.History)-1] = original
	d.Actors = d.Actors[:1]
	if _, err = Validate(d); err == nil {
		t.Fatal("missing assignee actor accepted")
	}
}

func TestAssignmentHistoryRequiresExplicitNullableFields(t *testing.T) {
	h := History{Type: "ticket.assignee-changed", Entity: Ref{"ticket", "tkt_a"}, Facts: json.RawMessage(`{"assignment":{"from_user_id":null,"to_user_id":"usr_a","from_revision":1,"to_revision":2}}`)}
	if _, err := ValidateFacts(h); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"from_user_id", "to_user_id", "from_revision", "to_revision"} {
		var outer map[string]map[string]json.RawMessage
		if err := json.Unmarshal(h.Facts, &outer); err != nil {
			t.Fatal(err)
		}
		delete(outer["assignment"], field)
		broken := h
		broken.Facts = Encode(outer)
		if _, err := ValidateFacts(broken); err == nil {
			t.Fatalf("accepted missing %s", field)
		}
	}
}

func TestAssignmentContinuesLegacyEqualTimeStatusHistory(t *testing.T) {
	b := fixture(t)
	d, _, err := Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	var created time.Time
	for _, o := range d.Objects {
		if o.Type == "ticket" {
			created = o.CreatedAt
		}
	}
	for i := range d.History {
		if d.History[i].Type == "ticket.status-changed" {
			d.History[i].OccurredAt = created
		}
	}
	target := "usr_a"
	when := created.Add(time.Second)
	for i := range d.Objects {
		if d.Objects[i].Type == "ticket" {
			var v Ticket
			if err = json.Unmarshal(d.Objects[i].Data, &v); err != nil {
				t.Fatal(err)
			}
			v.AssignmentRevision = 2
			v.AssigneeUserID = &target
			d.Objects[i].Data = Encode(v)
			d.Objects[i].UpdatedAt = when
		}
	}
	d.History = append(d.History, History{ID: "evt_assign", Type: "ticket.assignee-changed", Version: 1, Entity: Ref{"ticket", "tkt_a"}, Actor: target, Source: "web", OccurredAt: when, CorrelationID: "cor_a", Facts: Encode(Facts{Assignment: &Assignment{ToUserID: &target, FromRevision: 1, ToRevision: 2}}), Lineage: []Origin{}})
	if _, err = Validate(d); err != nil {
		t.Fatal(err)
	}
	d.History[len(d.History)-1].OccurredAt = created
	if _, err = Validate(d); err == nil {
		t.Fatal("accepted ambiguous assignment ordering")
	}
}
