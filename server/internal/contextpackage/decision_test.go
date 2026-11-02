package contextpackage

import (
	"bytes"
	"encoding/json"
	"testing"
)

func stripDecisionReason(t *testing.T, b *Bundle) {
	t.Helper()
	rewritePackageFile(t, b, "links.jsonl", func(raw []byte) []byte {
		var out []byte
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(line, &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, "supersession_reason")
			out = append(out, Encode(fields)...)
			out = append(out, '\n')
		}
		return out
	})
	rewritePackageFile(t, b, "objects.jsonl", func(raw []byte) []byte {
		var out []byte
		for _, o := range mustUnpackObjects(t, raw) {
			if o.Type == "decision" {
				var f map[string]any
				if err := json.Unmarshal(o.Data, &f); err != nil {
					t.Fatal(err)
				}
				delete(f, "rejection_reason")
				delete(f, "decision_revision")
				o.Data = Encode(f)
			}
			out = append(out, Encode(o)...)
			out = append(out, '\n')
		}
		return out
	})
}

func TestDecisionLegacyProfilesAndRejectedHistory(t *testing.T) {
	for _, profile := range []string{ProfileV1, ProfileV2, ProfileV3} {
		t.Run(profile, func(t *testing.T) {
			b := fixture(t)
			b.Manifest.Profile = profile
			if profile != ProfileV3 {
				stripPriorityFields(t, &b)
			}
			if profile == ProfileV1 {
				rewritePackageFile(t, &b, "objects.jsonl", func(raw []byte) []byte {
					var out []byte
					for _, o := range mustUnpackObjects(t, raw) {
						if o.Type == "ticket" {
							var f map[string]any
							_ = json.Unmarshal(o.Data, &f)
							delete(f, "assignee_user_id")
							delete(f, "assignment_revision")
							o.Data = Encode(f)
						}
						out = append(out, Encode(o)...)
						out = append(out, '\n')
					}
					return out
				})
			}
			if _, _, e := Unpack(b); e == nil {
				t.Fatal("old profile accepted new reason field")
			}
			stripDecisionReason(t, &b)
			digest := Digest(b.Manifest)
			if _, x, e := Unpack(b); e != nil || x.Decoded[Ref{"decision", "dec_a"}].(Decision).RejectionReason != nil || Digest(b.Manifest) != digest {
				t.Fatal("legacy conversion", e)
			}
			rewritePackageFile(t, &b, "objects.jsonl", func(raw []byte) []byte {
				return bytes.ReplaceAll(raw, []byte(`"accepted"`), []byte(`"\u0061ccepted"`))
			})
			if _, _, e := Unpack(b); e != nil {
				t.Fatal("valid escaped legacy status", e)
			}
		})
	}
	b := fixture(t)
	d, _, err := Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	// Preserve the accepted execution chain and add an independently rejected proposal.
	for _, o := range append([]Object{}, d.Objects...) {
		if o.Type == "decision" {
			var v Decision
			_ = json.Unmarshal(o.Data, &v)
			v.Status = "rejected"
			v.Outcome = nil
			v.Rationale = nil
			reason := "Reason kept as history"
			v.RejectionReason = &reason
			o.ID = "dec_rejected"
			o.Data = Encode(v)
			d.Objects = append(d.Objects, o)
		}
	}
	for _, h := range append([]History{}, d.History...) {
		if h.Entity.Type == "decision" {
			h.Entity.ID = "dec_rejected"
			h.ID += "_rejected"
			if h.Type == "decision.accepted" {
				h.Type = "decision.rejected"
				h.Facts = Encode(Facts{Status: "rejected"})
			}
			d.History = append(d.History, h)
		}
	}
	for _, l := range append([]Link{}, d.Links...) {
		if l.From.Type == "decision" {
			l.ID += "_rejected"
			l.From.ID = "dec_rejected"
			l.EventID += "_rejected"
			d.Links = append(d.Links, l)
		}
	}
	packed, e := Pack(b.Manifest, d)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = Unpack(packed); e != nil {
		t.Fatal(e)
	}
	for _, profile := range []string{ProfileV1, ProfileV2, ProfileV3} {
		bad := packed
		bad.Manifest.Profile = profile
		if _, _, e = Unpack(bad); e == nil {
			t.Fatal("legacy profile accepted rejection")
		}
	}
	for _, mutate := range []func(*Data){
		func(v *Data) {
			for i, o := range v.Objects {
				if o.ID == "dec_rejected" {
					var dec Decision
					_ = json.Unmarshal(o.Data, &dec)
					dec.RejectionReason = nil
					v.Objects[i].Data = Encode(dec)
				}
			}
		},
		func(v *Data) { v.History = v.History[:len(v.History)-1] },
		func(v *Data) {
			for i, h := range v.History {
				if h.Entity.ID == "dec_rejected" && h.Type == "decision.rejected" {
					v.History[i].Actor = "usr_missing"
				}
			}
		},
		func(v *Data) {
			for i, h := range v.History {
				if h.Type == "ticket.created" {
					v.History[i].Facts = Encode(Facts{Status: "open", Decision: &Ref{"decision", "dec_rejected"}})
				}
			}
		},
	} {
		copyData, _, e := Unpack(packed)
		if e != nil {
			t.Fatal(e)
		}
		mutate(&copyData)
		if _, e = Validate(copyData); e == nil {
			t.Fatal("damaged rejection accepted")
		}
	}
}
