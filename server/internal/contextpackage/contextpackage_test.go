package contextpackage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) Bundle {
	t.Helper()
	at := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	later := at.Add(time.Minute)
	d := Data{Bodies: map[string]string{"documents/doc_a/1.md": "# Original\n\n[link](https://example.invalid/never-fetch)", "documents/doc_a/2.md": "# Revised\n\nentity://decision/dec_a"}, Actors: []Actor{{ID: "usr_a", Lineage: []Origin{}}}}
	object := func(kind, id string, updated time.Time, data any) {
		actor := "usr_a"
		if kind == "team" {
			actor = ""
		}
		d.Objects = append(d.Objects, Object{Type: kind, ID: id, Actor: actor, CreatedAt: at, UpdatedAt: updated, Data: Encode(data), Lineage: []Origin{}})
	}
	object("team", "tem_a", at, Team{Name: "Team"})
	object("project", "prj_a", at, Project{Key: "DEMO", Name: "Project", Summary: "", TeamID: "tem_a", Visibility: "workspace", Status: "active"})
	object("channel", "chn_a", at, Channel{Name: "General", Visibility: "restricted", Status: "active"})
	object("message", "msg_a", at, Message{ChannelID: "chn_a", Body: "Source"})
	channel := "chn_a"
	object("thread", "thr_a", at, Thread{Title: "Discussion", Visibility: "restricted", ChannelID: &channel})
	outcome, rationale := "Adopt", "Reason"
	object("decision", "dec_a", at, Decision{Question: "Question?", Outcome: &outcome, Rationale: &rationale, Status: "accepted", Proposer: "usr_a", Deciders: []string{"usr_a"}, DecidedAt: &at, Alternatives: json.RawMessage(`[]`)})
	object("ticket", "tkt_a", later, Ticket{Title: "Work", Status: "in-progress", StatusRevision: 2})
	object("document", "doc_a", later, Document{Revision: 2, Visibility: "project"})
	ref := func(kind, id string) *Ref { return &Ref{kind, id} }
	event := func(id, kind, entity string, when time.Time, f Facts) {
		d.History = append(d.History, History{ID: id, Type: kind, Version: 1, Entity: Ref{strings.Split(kind, ".")[0], entity}, Actor: "usr_a", Source: "web", OccurredAt: when, CorrelationID: "cor_a", Facts: Encode(f), Lineage: []Origin{}})
	}
	event("evt_project", "project.created", "prj_a", at, Facts{Status: "active"})
	event("evt_channel", "channel.created", "chn_a", at, Facts{Status: "active"})
	event("evt_message", "message.created", "msg_a", at, Facts{Channel: ref("channel", "chn_a")})
	event("evt_thread", "thread.started", "thr_a", at, Facts{Channel: ref("channel", "chn_a"), Message: ref("message", "msg_a")})
	event("evt_propose", "decision.proposed", "dec_a", at, Facts{Status: "proposed", Evidence: ref("thread", "thr_a")})
	event("evt_accept", "decision.accepted", "dec_a", at, Facts{Status: "accepted"})
	event("evt_ticket", "ticket.created", "tkt_a", at, Facts{Status: "open", Decision: ref("decision", "dec_a")})
	event("evt_status", "ticket.status-changed", "tkt_a", later, Facts{FromStatus: "open", ToStatus: "in-progress", FromRevision: 1, ToRevision: 2})
	event("evt_document", "document.created", "doc_a", at, Facts{Revision: 1, Ticket: ref("ticket", "tkt_a")})
	event("evt_revision", "document.revised", "doc_a", later, Facts{Revision: 2})
	for _, v := range []struct {
		id              string
		from, to        Ref
		relation, event string
	}{{"lnk_thread", *ref("thread", "thr_a"), *ref("message", "msg_a"), "started-from", "evt_thread"}, {"lnk_decision", *ref("decision", "dec_a"), *ref("thread", "thr_a"), "derived-from", "evt_propose"}, {"lnk_ticket", *ref("ticket", "tkt_a"), *ref("decision", "dec_a"), "implements", "evt_ticket"}, {"lnk_document", *ref("ticket", "tkt_a"), *ref("document", "doc_a"), "relates-to", "evt_document"}} {
		d.Links = append(d.Links, Link{ID: v.id, From: v.from, To: v.to, Relation: v.relation, Assertion: "asserted", Origin: "user", Actor: "usr_a", CreatedAt: at, UpdatedAt: at, EventID: v.event, State: "active", Lineage: []Origin{}})
	}
	d.Revisions = []Revision{{DocumentID: "doc_a", Revision: 1, Title: "Original", Format: "nexus-markdown-v1", Actor: "usr_a", CreatedAt: at, EventID: "evt_document", BodyFile: BodyPath("doc_a", 1)}, {DocumentID: "doc_a", Revision: 2, Title: "Revised", Format: "nexus-markdown-v1", Actor: "usr_a", CreatedAt: later, EventID: "evt_revision", BodyFile: BodyPath("doc_a", 2)}}
	b, e := Pack(Manifest{PackageID: "pkg_a", CreatedAt: later, SourceVersion: "test", WorkspaceID: "wrk_a", ProjectID: "prj_a"}, d)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestPackageRoundTripAndPlanBinding(t *testing.T) {
	b := fixture(t)
	dir := filepath.Join(t.TempDir(), "project.nexus")
	if e := WriteDirectory(dir, b); e != nil {
		t.Fatal(e)
	}
	loaded, e := ReadDirectory(dir)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, loaded) {
		t.Fatal("package changed during directory round trip")
	}
	if e = WriteDirectory(dir, b); e == nil {
		t.Fatal("overwrote existing output")
	}
	for _, p := range []string{dir, filepath.Join(dir, "manifest.json")} {
		s, e := os.Stat(p)
		if e != nil || s.Mode().Perm()&0077 != 0 {
			t.Fatal("output not private")
		}
	}
	d, _, e := Unpack(b)
	if e != nil {
		t.Fatal(e)
	}
	operation := "00000000-0000-4000-8000-000000000001"
	plan, e := BuildPlan(b.Manifest, d, "wrk_target", Hash([]byte("target")), "usr_owner", operation)
	if e != nil {
		t.Fatal(e)
	}
	second, e := BuildPlan(b.Manifest, d, "wrk_target", Hash([]byte("target")), "usr_owner", operation)
	if e != nil || !reflect.DeepEqual(plan, second) {
		t.Fatal("plan not deterministic")
	}
	for _, change := range []struct{ workspace, identity, actor, operation string }{{"wrk_other", plan.TargetIdentity, plan.ActorID, operation}, {plan.WorkspaceID, Hash([]byte("other")), plan.ActorID, operation}, {plan.WorkspaceID, plan.TargetIdentity, "usr_other", operation}, {plan.WorkspaceID, plan.TargetIdentity, plan.ActorID, "00000000-0000-4000-8000-000000000002"}} {
		other, e := BuildPlan(b.Manifest, d, change.workspace, change.identity, change.actor, change.operation)
		if e != nil || other.PlanDigest == plan.PlanDigest || other.Mappings[0].TargetID == plan.Mappings[0].TargetID {
			t.Fatal("plan did not bind target, actor or operation")
		}
	}
	seen := map[string]bool{}
	for _, m := range plan.Mappings {
		if m.TargetID == m.SourceID || seen[m.TargetID] {
			t.Fatal("mapping reused identity")
		}
		seen[m.TargetID] = true
	}
}
func TestPackageRejectsBrokenHistoryAndClosure(t *testing.T) {
	tests := map[string]func(*Data){
		"missing document relation":   func(d *Data) { d.Links = d.Links[:3] },
		"duplicate document relation": func(d *Data) { link := d.Links[3]; link.ID = "lnk_duplicate"; d.Links = append(d.Links, link) },
		"premature ticket": func(d *Data) {
			var dec Decision
			_ = json.Unmarshal(d.Objects[5].Data, &dec)
			at := d.Objects[5].CreatedAt.Add(time.Second)
			dec.DecidedAt = &at
			d.Objects[5].UpdatedAt = at
			d.Objects[5].Data = Encode(dec)
			d.History[5].OccurredAt = at
		},
		"missing source":        func(d *Data) { d.Objects = d.Objects[1:] },
		"missing author":        func(d *Data) { d.Actors = nil },
		"identity directory":    func(d *Data) { d.Actors = append(d.Actors, Actor{ID: "usr_unreferenced", Lineage: []Origin{}}) },
		"duplicate object":      func(d *Data) { d.Objects = append(d.Objects, d.Objects[1]) },
		"unknown object fields": func(d *Data) { d.Objects[0].Data = Encode(map[string]string{"name": "Team", "secret": "forbidden"}) },
		"unknown history facts": func(d *Data) {
			d.History[0].Facts = Encode(map[string]string{"status": "active", "email": "forbidden"})
		},
		"missing accepted event": func(d *Data) { d.History = append(d.History[:5], d.History[6:]...) },
		"wrong decider": func(d *Data) {
			var v Decision
			_ = json.Unmarshal(d.Objects[5].Data, &v)
			v.Deciders = []string{"usr_missing"}
			d.Objects[5].Data = Encode(v)
		},
		"status gap": func(d *Data) {
			d.History[7].Facts = Encode(Facts{FromStatus: "open", ToStatus: "in-progress", FromRevision: 2, ToRevision: 3})
		},
		"missing revision":        func(d *Data) { d.Revisions = d.Revisions[1:] },
		"revision body mismatch":  func(d *Data) { delete(d.Bodies, BodyPath("doc_a", 1)) },
		"extra body":              func(d *Data) { d.Bodies[BodyPath("doc_a", 3)] = "hidden" },
		"missing link":            func(d *Data) { d.Links = d.Links[1:] },
		"wrong relation evidence": func(d *Data) { d.Links[0].EventID = "evt_message" },
		"unknown link":            func(d *Data) { d.Links[0].Relation = "custom" },
		"causation cycle":         func(d *Data) { id := d.History[0].ID; d.History[0].CausationID = &id },
		"lineage missing":         func(d *Data) { d.Objects[0].Lineage = nil },
		"lineage too deep": func(d *Data) {
			for i := 0; i <= MaxLineage; i++ {
				d.Objects[0].Lineage = append(d.Objects[0].Lineage, Origin{"pkg_a", Hash([]byte("a")), "source"})
			}
		},
		"body exceeds native limit": func(d *Data) { d.Bodies[BodyPath("doc_a", 1)] = strings.Repeat("x", 262145) },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			d, _, e := Unpack(fixture(t))
			if e != nil {
				t.Fatal(e)
			}
			change(&d)
			if _, e = Validate(d); !errors.Is(e, ErrInvalid) {
				t.Fatalf("accepted broken package: %v", e)
			}
		})
	}
}
func TestStrictJSON(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{"unknown":1}`, `{} {}`, `{"a":"` + string([]byte{0xff}) + `"}`, `{"a":` + strings.Repeat("[", 60) + `0` + strings.Repeat("]", 60) + `}`} {
		var dst struct {
			A any `json:"a"`
		}
		if e := Decode([]byte(raw), &dst); !errors.Is(e, ErrInvalid) {
			t.Fatal("accepted malformed JSON")
		}
	}
}
func TestBundleTampering(t *testing.T) {
	for name, mutate := range map[string]func(*Bundle){
		"unknown version": func(b *Bundle) { b.Manifest.Version++ }, "unknown profile": func(b *Bundle) { b.Manifest.Profile = "unknown" }, "duplicate file": func(b *Bundle) { b.Files[1] = b.Files[0] }, "hash mismatch": func(b *Bundle) { b.Files[0].Content = append(b.Files[0].Content, ' ') }, "wrong source project": func(b *Bundle) { b.Manifest.ProjectID = "prj_missing" }, "wrong line count": func(b *Bundle) { b.Manifest.Files[0].Lines++ }, "path traversal": func(b *Bundle) { b.Files[0].Path = "../outside" }, "absolute path": func(b *Bundle) { b.Files[0].Path = "/tmp/outside" }, "unknown file": func(b *Bundle) { b.Files[0].Path = "credentials.json" },
	} {
		t.Run(name, func(t *testing.T) {
			b := fixture(t)
			mutate(&b)
			if _, _, e := Unpack(b); e == nil {
				t.Fatal("accepted tampered bundle")
			}
		})
	}
}
func TestDirectoryRejectsLinksAndUnlistedFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "unlisted", "directory symlink", "root symlink", "fifo", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "project.nexus")
			b := fixture(t)
			if e := WriteDirectory(dir, b); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "objects.jsonl")
			var e error
			switch kind {
			case "unlisted":
				e = os.WriteFile(filepath.Join(dir, "extra"), []byte("x"), 0600)
			case "root symlink":
				link := filepath.Join(parent, "alias.nexus")
				e = os.Symlink(dir, link)
				dir = link
			case "directory symlink":
				e = os.Symlink(filepath.Join(dir, "documents"), filepath.Join(dir, "alias"))
			case "truncated":
				e = os.WriteFile(path, []byte(`{"type":"project"}`), 0600)
			default:
				original := filepath.Join(parent, "original")
				e = os.Rename(path, original)
				if e == nil {
					switch kind {
					case "symlink":
						e = os.Symlink(original, path)
					case "hardlink":
						e = os.Link(original, path)
					case "fifo":
						e = makeFIFO(path)
					}
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = ReadDirectory(dir); e == nil {
				t.Fatal("accepted invalid package directory")
			}
		})
	}
}

func TestPrepareRejectsExhaustedLineage(t *testing.T) {
	b := fixture(t)
	d, _, e := Unpack(b)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < MaxLineage; i++ {
		d.Objects[0].Lineage = append(d.Objects[0].Lineage, Origin{PackageID: "pkg_" + strings.Repeat("a", i+1), Digest: Hash([]byte{byte(i)}), SourceID: "tem_old"})
	}
	if _, e = Validate(d); e != nil {
		t.Fatal("maximum exported lineage should remain inspectable", e)
	}
	if _, e = BuildPlan(b.Manifest, d, "wrk_target", Hash([]byte("target")), "usr_owner", "00000000-0000-4000-8000-000000000001"); !errors.Is(e, ErrInvalid) {
		t.Fatal("prepare accepted ninth import hop", e)
	}
}
func TestDocumentTitleLimitBeforeApply(t *testing.T) {
	b := fixture(t)
	d, _, e := Unpack(b)
	if e != nil {
		t.Fatal(e)
	}
	for _, title := range []string{strings.Repeat("a", 201), "title\u2028break"} {
		d.Revisions[0].Title = title
		if _, e = Validate(d); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid document title escaped prepare", e)
		}
	}
}
