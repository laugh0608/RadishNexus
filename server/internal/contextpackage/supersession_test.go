package contextpackage

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func stripSupersessionFields(t *testing.T, b *Bundle) {
	t.Helper()
	rewritePackageFile(t, b, "objects.jsonl", func(raw []byte) []byte {
		var out []byte
		for _, o := range mustUnpackObjects(t, raw) {
			if o.Type == "decision" {
				var fields map[string]json.RawMessage
				if e := Decode(o.Data, &fields); e != nil {
					t.Fatal(e)
				}
				delete(fields, "decision_revision")
				o.Data = Encode(fields)
			}
			out = append(out, Encode(o)...)
			out = append(out, '\n')
		}
		return out
	})
	rewritePackageFile(t, b, "links.jsonl", func(raw []byte) []byte {
		var out []byte
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var fields map[string]json.RawMessage
			if e := Decode(line, &fields); e != nil {
				t.Fatal(e)
			}
			delete(fields, "supersession_reason")
			out = append(out, Encode(fields)...)
			out = append(out, '\n')
		}
		return out
	})
}
func TestSupersessionLegacyV4AndHistoricalClosure(t *testing.T) {
	legacy := fixture(t)
	legacy.Manifest.Profile = ProfileV4
	if _, _, e := Unpack(legacy); e == nil {
		t.Fatal("v4 accepted new fields")
	}
	stripSupersessionFields(t, &legacy)
	digest := Digest(legacy.Manifest)
	if _, x, e := Unpack(legacy); e != nil || x.Decoded[Ref{"decision", "dec_a"}].(Decision).DecisionRevision != 2 || Digest(legacy.Manifest) != digest {
		t.Fatal("v4 conversion", e)
	}
	b := fixture(t)
	d, _, e := Unpack(b)
	if e != nil {
		t.Fatal(e)
	}
	at := d.Objects[0].CreatedAt.Add(2 * time.Minute)
	originalCount := len(d.Objects)
	for i := 0; i < originalCount; i++ {
		o := d.Objects[i]
		if o.Type != "decision" {
			continue
		}
		var dec Decision
		if e = Decode(o.Data, &dec); e != nil {
			t.Fatal(e)
		}
		next := o
		next.ID = "dec_b"
		next.UpdatedAt = at
		dec.DecisionRevision = 3
		dec.Question = "Replacement?"
		next.Data = Encode(dec)
		d.Objects = append(d.Objects, next)
		dec.Question = "Question?"
		dec.Status = "superseded"
		o.UpdatedAt = at
		o.Data = Encode(dec)
		d.Objects[i] = o
	}
	for _, h := range append([]History{}, d.History...) {
		if h.Entity.Type == "decision" {
			h.ID += "_b"
			h.Entity.ID = "dec_b"
			d.History = append(d.History, h)
		}
	}
	for _, l := range append([]Link{}, d.Links...) {
		if l.From.Type == "decision" {
			l.ID += "_b"
			l.From.ID = "dec_b"
			l.EventID += "_b"
			d.Links = append(d.Links, l)
		}
	}
	facts := Supersession{Status: "superseded", Replacement: Ref{"decision", "dec_b"}, Relation: Ref{"entity-link", "lnk_supersession"}, FromRevision: 2, ToRevision: 3, ReplacementFromRevision: 2, ReplacementToRevision: 3}
	d.History = append(d.History, History{ID: "evt_supersession", Type: "decision.superseded", Version: 1, Entity: Ref{"decision", "dec_a"}, Actor: "usr_a", Source: "web", OccurredAt: at, CorrelationID: "cor_a", Facts: Encode(facts), Lineage: []Origin{}})
	reason := "Keep the historical execution source"
	d.Links = append(d.Links, Link{ID: "lnk_supersession", From: facts.Replacement, To: Ref{"decision", "dec_a"}, Relation: "supersedes", Assertion: "asserted", Origin: "user", Actor: "usr_a", CreatedAt: at, UpdatedAt: at, EventID: "evt_supersession", State: "active", Lineage: []Origin{}, SupersessionReason: &reason})
	b.Manifest.CreatedAt = at.Add(time.Minute)
	packed, e := Pack(b.Manifest, d)
	if e != nil {
		t.Fatal("valid supersession", e)
	}
	for _, mutate := range []func(*Data){
		func(v *Data) { v.Links = v.Links[:len(v.Links)-1] },
		func(v *Data) { v.History = v.History[:len(v.History)-1] },
		func(v *Data) {
			for i, o := range v.Objects {
				if o.ID == "dec_b" {
					var dec Decision
					_ = Decode(o.Data, &dec)
					dec.DecisionRevision = 2
					v.Objects[i].Data = Encode(dec)
				}
			}
		},
		func(v *Data) { last := &v.Links[len(v.Links)-1]; value := "\u0085"; last.SupersessionReason = &value },
		func(v *Data) {
			last := &v.History[len(v.History)-1]
			bad := facts
			bad.Replacement = last.Entity
			last.Facts = Encode(bad)
		},
		func(v *Data) {
			v.Actors = append(v.Actors, Actor{ID: "usr_b", Lineage: []Origin{}})
			v.History[len(v.History)-1].Actor = "usr_b"
		},
		func(v *Data) {
			later := at.Add(time.Minute)
			v.Objects = append(v.Objects, Object{Type: "ticket", ID: "tkt_late", Actor: "usr_a", CreatedAt: later, UpdatedAt: later, Data: Encode(Ticket{Title: "After replacement", Status: "open", StatusRevision: 1, AssignmentRevision: 1, Priority: "unset", PriorityRevision: 1}), Lineage: []Origin{}})
			v.History = append(v.History, History{ID: "evt_late", Type: "ticket.created", Version: 1, Entity: Ref{"ticket", "tkt_late"}, Actor: "usr_a", Source: "web", OccurredAt: later, CorrelationID: "cor_a", Facts: Encode(Facts{Status: "open", Decision: &Ref{"decision", "dec_a"}}), Lineage: []Origin{}})
			v.Links = append(v.Links, Link{ID: "lnk_late", From: Ref{"ticket", "tkt_late"}, To: Ref{"decision", "dec_a"}, Relation: "implements", Assertion: "asserted", Origin: "user", Actor: "usr_a", CreatedAt: later, UpdatedAt: later, EventID: "evt_late", State: "active", Lineage: []Origin{}})
		},
	} {
		copy, _, err := Unpack(packed)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&copy)
		if _, err = Validate(copy); err == nil {
			t.Fatal("damaged supersession accepted")
		}
	}
	packed.Manifest.Profile = ProfileV4
	stripSupersessionFields(t, &packed)
	if _, _, e = Unpack(packed); e == nil {
		t.Fatal("v4 accepted supersession")
	}
}

func TestSupersessionPreservesLegacyProposedTimestamp(t *testing.T) {
	b := fixture(t)
	d, _, e := Unpack(b)
	if e != nil {
		t.Fatal(e)
	}
	for _, o := range append([]Object{}, d.Objects...) {
		if o.Type == "decision" {
			var dec Decision
			_ = Decode(o.Data, &dec)
			dec.Status = "proposed"
			dec.DecisionRevision = 1
			dec.Outcome = nil
			dec.Rationale = nil
			dec.Deciders = []string{}
			dec.DecidedAt = nil
			o.ID = "dec_legacy_proposal"
			o.UpdatedAt = o.CreatedAt.Add(30 * time.Second)
			o.Data = Encode(dec)
			d.Objects = append(d.Objects, o)
		}
	}
	for _, h := range append([]History{}, d.History...) {
		if h.Type == "decision.proposed" {
			h.ID += "_legacy_proposal"
			h.Entity.ID = "dec_legacy_proposal"
			d.History = append(d.History, h)
		}
	}
	for _, l := range append([]Link{}, d.Links...) {
		if l.Relation == "derived-from" {
			l.ID += "_legacy_proposal"
			l.From.ID = "dec_legacy_proposal"
			l.EventID += "_legacy_proposal"
			d.Links = append(d.Links, l)
		}
	}
	packed, e := Pack(b.Manifest, d)
	if e != nil {
		t.Fatal(e)
	}
	packed.Manifest.Profile = ProfileV4
	stripSupersessionFields(t, &packed)
	digest := Digest(packed.Manifest)
	converted, x, e := Unpack(packed)
	if e != nil {
		t.Fatal(e)
	}
	if Digest(packed.Manifest) != digest || x.Decoded[Ref{"decision", "dec_legacy_proposal"}].(Decision).DecisionRevision != 1 {
		t.Fatal("legacy proposal changed")
	}
	for _, o := range converted.Objects {
		if o.ID == "dec_legacy_proposal" && !o.UpdatedAt.Equal(o.CreatedAt.Add(30*time.Second)) {
			t.Fatal("legacy updated_at rewritten")
		}
	}
}
