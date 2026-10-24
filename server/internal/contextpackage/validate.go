package contextpackage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/laugh0608/RadishNexus/server/internal/markdown"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Index struct {
	Objects   map[Ref]Object
	Actors    map[string]Actor
	History   map[string]History
	Facts     map[string]Facts
	Decoded   map[Ref]any
	Revisions map[string][]Revision
}

func Validate(d Data) (*Index, error) {
	if len(d.Objects) < 2 || len(d.Objects) > MaxObjects || len(d.History) > MaxHistory || len(d.Revisions) > MaxHistory || len(d.Actors) > MaxObjects || len(d.Links) > MaxHistory {
		return nil, invalid("record limits")
	}
	x := &Index{Objects: map[Ref]Object{}, Actors: map[string]Actor{}, History: map[string]History{}, Facts: map[string]Facts{}, Decoded: map[Ref]any{}, Revisions: map[string][]Revision{}}
	for _, a := range d.Actors {
		if !ValidID(a.ID, "usr_") || !validateLineage(a.Lineage) {
			return nil, invalid("actor")
		}
		if _, ok := x.Actors[a.ID]; ok {
			return nil, invalid("duplicate actor")
		}
		x.Actors[a.ID] = a
	}
	counts := map[string]int{}
	for _, o := range d.Objects {
		ref := Ref{o.Type, o.ID}
		if !oneOf(o.Type, "team", "project", "channel", "message", "thread", "decision", "ticket", "document") || !ValidID(o.ID, Prefix(o.Type)) || !validTime(o.CreatedAt) || !validTime(o.UpdatedAt) || o.UpdatedAt.Before(o.CreatedAt) || !validateLineage(o.Lineage) {
			return nil, invalid("object envelope")
		}
		if _, exists := x.Objects[ref]; exists {
			return nil, invalid("duplicate object")
		}
		if o.Type != "team" {
			if _, ok := x.Actors[o.Actor]; !ok {
				return nil, invalid("missing actor")
			}
		} else if o.Actor != "" {
			return nil, invalid("Team actor")
		}
		v, err := decodeObject(o)
		if err != nil {
			return nil, err
		}
		x.Objects[ref] = o
		x.Decoded[ref] = v
		counts[o.Type]++
	}
	if counts["team"] != 1 || counts["project"] != 1 {
		return nil, invalid("single Project scope")
	}
	exists := func(r Ref) bool { _, ok := x.Objects[r]; return ok }
	for ref, v := range x.Decoded {
		switch value := v.(type) {
		case Project:
			if !exists(Ref{"team", value.TeamID}) {
				return nil, invalid("Project Team")
			}
		case Message:
			if !exists(Ref{"channel", value.ChannelID}) {
				return nil, invalid("Message Channel")
			}
			if value.ThreadID != nil {
				t, ok := x.Decoded[Ref{"thread", *value.ThreadID}].(Thread)
				if !ok || t.ChannelID == nil || *t.ChannelID != value.ChannelID {
					return nil, invalid("Message Thread")
				}
			}
		case Thread:
			if value.ChannelID != nil && !exists(Ref{"channel", *value.ChannelID}) {
				return nil, invalid("Thread Channel")
			}
		case Decision:
			if _, ok := x.Actors[value.Proposer]; !ok {
				return nil, invalid("Decision proposer")
			}
			seen := map[string]bool{}
			for _, id := range value.Deciders {
				if _, ok := x.Actors[id]; !ok || seen[id] {
					return nil, invalid("Decision decider")
				}
				seen[id] = true
			}
		case Document:
			if value.Revision > MaxHistory {
				return nil, invalid("Document versions")
			}
		}
		_ = ref
	}
	for _, h := range d.History {
		if !ValidID(h.ID, "evt_") || h.Version != 1 || !exists(h.Entity) || !validTime(h.OccurredAt) || !ValidID(h.CorrelationID, "cor_") || !oneOf(h.Source, "web", "api", "system", "import") || !validateLineage(h.Lineage) {
			return nil, invalid("history envelope")
		}
		if _, ok := x.Actors[h.Actor]; !ok {
			return nil, invalid("history actor")
		}
		if _, ok := x.History[h.ID]; ok {
			return nil, invalid("duplicate history")
		}
		f, err := ValidateFacts(h)
		if err != nil {
			return nil, err
		}
		for _, r := range []*Ref{f.Evidence, f.Decision, f.Ticket, f.Channel, f.Thread, f.Message} {
			if r != nil && (!exists(*r) || x.Objects[*r].CreatedAt.After(h.OccurredAt)) {
				return nil, invalid("history dependency")
			}
		}
		x.History[h.ID] = h
		x.Facts[h.ID] = f
	}
	// An event's causation must resolve without cycles, and may not point forward
	// in source time. Correlation is checked rather than regenerated as evidence.
	for _, h := range d.History {
		seen := map[string]bool{h.ID: true}
		at := h
		for at.CausationID != nil {
			cause, ok := x.History[*at.CausationID]
			if !ok || seen[cause.ID] || cause.CorrelationID != at.CorrelationID || cause.OccurredAt.After(at.OccurredAt) {
				return nil, invalid("history causation")
			}
			seen[cause.ID] = true
			at = cause
			if len(seen) > 256 {
				return nil, invalid("history cycle")
			}
		}
	}
	seenLinks := map[string]bool{}
	relations := map[Ref]map[string][]Link{}
	for _, l := range d.Links {
		if !ValidID(l.ID, "lnk_") || seenLinks[l.ID] || !exists(l.From) || !exists(l.To) || l.Assertion != "asserted" || !oneOf(l.Origin, "user", "import") || !validTime(l.CreatedAt) || !validTime(l.UpdatedAt) || l.UpdatedAt.Before(l.CreatedAt) || !validateLineage(l.Lineage) {
			return nil, invalid("relation")
		}
		if _, ok := x.Actors[l.Actor]; !ok {
			return nil, invalid("relation actor")
		}
		seenLinks[l.ID] = true
		if l.State == "active" {
			if l.RemovedActor != nil || l.RemovedAt != nil || l.RemovalReason != nil {
				return nil, invalid("active relation removal")
			}
		} else if l.State == "removed" {
			if l.RemovedActor == nil || l.RemovedAt == nil || !validTime(*l.RemovedAt) || l.RemovedAt.Before(l.CreatedAt) || l.RemovalReason == nil || !textValue(*l.RemovalReason, 4096, true) {
				return nil, invalid("relation removal")
			}
			if _, ok := x.Actors[*l.RemovedActor]; !ok {
				return nil, invalid("removal actor")
			}
		} else {
			return nil, invalid("relation state")
		}
		h, ok := x.History[l.EventID]
		if !ok || h.Actor != l.Actor || !h.OccurredAt.Equal(l.CreatedAt) {
			return nil, invalid("relation source")
		}
		f := x.Facts[h.ID]
		var wanted *Ref
		var event string
		var primary Ref
		switch l.From.Type + "/" + l.Relation + "/" + l.To.Type {
		case "thread/started-from/message":
			wanted = f.Message
			event = "thread.started"
			primary = l.From
			if l.State != "active" {
				return nil, invalid("Thread source state")
			}
		case "decision/derived-from/thread":
			wanted = f.Evidence
			event = "decision.proposed"
			primary = l.From
		case "ticket/implements/decision":
			wanted = f.Decision
			event = "ticket.created"
			primary = l.From
		case "ticket/relates-to/document":
			wanted = f.Ticket
			event = "document.created"
			primary = l.To
		default:
			return nil, invalid("unsupported relation")
		}
		target := l.To
		if l.Relation == "relates-to" {
			target = l.From
		}
		if wanted == nil || *wanted != target || h.Type != event || h.Entity != primary {
			return nil, invalid("relation provenance")
		}
		if relations[l.From] == nil {
			relations[l.From] = map[string][]Link{}
		}
		relations[l.From][l.Relation] = append(relations[l.From][l.Relation], l)
	}
	usedBodies := map[string]bool{}
	for _, r := range d.Revisions {
		if !exists(Ref{"document", r.DocumentID}) || r.Revision < 1 || r.Revision > MaxHistory || !label(r.Title, 800) || utf8.RuneCountInString(r.Title) > 200 || strings.ContainsAny(r.Title, "\u2028\u2029") || r.Format != "nexus-markdown-v1" || !validTime(r.CreatedAt) || r.BodyFile != BodyPath(r.DocumentID, r.Revision) || usedBodies[r.BodyFile] {
			return nil, invalid("Document revision")
		}
		body, ok := d.Bodies[r.BodyFile]
		if !ok || !textValue(body, 262144, false) || strings.ContainsRune(body, '\r') {
			return nil, invalid("Document body")
		}
		if _, err := markdown.Parse(body, r.Format); err != nil {
			return nil, invalid("Document Markdown contract")
		}
		usedBodies[r.BodyFile] = true
		h, ok := x.History[r.EventID]
		f := x.Facts[r.EventID]
		want := "document.revised"
		if r.Revision == 1 {
			want = "document.created"
		}
		if !ok || h.Type != want || h.Entity != (Ref{"document", r.DocumentID}) || h.Actor != r.Actor || !h.OccurredAt.Equal(r.CreatedAt) || f.Revision != r.Revision || !sameInt(f.RestoredFrom, r.RestoredFrom) {
			return nil, invalid("Document history")
		}
		x.Revisions[r.DocumentID] = append(x.Revisions[r.DocumentID], r)
	}
	if len(usedBodies) != len(d.Bodies) {
		return nil, invalid("unreferenced body")
	}
	historiesByEntity := map[Ref][]History{}
	for _, h := range d.History {
		historiesByEntity[h.Entity] = append(historiesByEntity[h.Entity], h)
	}
	for ref, o := range x.Objects {
		if o.Type == "team" {
			continue
		}
		events := historiesByEntity[ref]
		if err := validateObjectHistory(o, x.Decoded[ref], events, x, relations); err != nil {
			return nil, err
		}
	}
	// Unused actors are not a portable membership directory.
	usedActors := map[string]bool{}
	for _, o := range d.Objects {
		usedActors[o.Actor] = true
		if dec, ok := x.Decoded[Ref{o.Type, o.ID}].(Decision); ok {
			usedActors[dec.Proposer] = true
			for _, id := range dec.Deciders {
				usedActors[id] = true
			}
		}
	}
	for _, h := range d.History {
		usedActors[h.Actor] = true
	}
	for _, l := range d.Links {
		usedActors[l.Actor] = true
		if l.RemovedActor != nil {
			usedActors[*l.RemovedActor] = true
		}
	}
	for id := range x.Actors {
		if !usedActors[id] {
			return nil, invalid("unreferenced actor")
		}
	}
	return x, nil
}

func decodeObject(o Object) (any, error) {
	var target any
	switch o.Type {
	case "team":
		target = &Team{}
	case "project":
		target = &Project{}
	case "channel":
		target = &Channel{}
	case "message":
		target = &Message{}
	case "thread":
		target = &Thread{}
	case "decision":
		target = &Decision{}
	case "ticket":
		target = &Ticket{}
	case "document":
		target = &Document{}
	default:
		return nil, invalid("object type")
	}
	if err := Decode(o.Data, target); err != nil {
		return nil, err
	}
	switch p := target.(type) {
	case *Team:
		if !label(p.Name, 800) {
			return nil, invalid("Team fields")
		}
		return *p, nil
	case *Project:
		if !label(p.Name, 800) || !textValue(p.Summary, 16384, false) || !tokenPattern.MatchString(p.Key) || len(p.Key) > 64 || !oneOf(p.Visibility, "workspace", "restricted") || !oneOf(p.Status, "active", "archived") {
			return nil, invalid("Project fields")
		}
		return *p, nil
	case *Channel:
		if !label(p.Name, 800) || !oneOf(p.Visibility, "project", "restricted") || !oneOf(p.Status, "active", "archived") {
			return nil, invalid("Channel fields")
		}
		return *p, nil
	case *Message:
		if !textValue(p.Body, 16384, true) {
			return nil, invalid("Message fields")
		}
		return *p, nil
	case *Thread:
		if !label(p.Title, 800) || !oneOf(p.Visibility, "project", "restricted") {
			return nil, invalid("Thread fields")
		}
		return *p, nil
	case *Decision:
		if !textValue(p.Question, 16384, true) || !oneOf(p.Status, "proposed", "accepted") || p.Deciders == nil || !bytes.Equal(bytes.TrimSpace(p.Alternatives), []byte("[]")) || p.Consequences != nil || p.ReviewAt != nil {
			return nil, invalid("unsupported Decision fields")
		}
		if p.Status == "accepted" {
			if p.Outcome == nil || p.Rationale == nil || !textValue(*p.Outcome, 32768, true) || !textValue(*p.Rationale, 32768, true) || len(p.Deciders) != 1 || p.DecidedAt == nil || !validTime(*p.DecidedAt) {
				return nil, invalid("accepted Decision")
			}
		} else if p.Outcome != nil || p.Rationale != nil || len(p.Deciders) != 0 || p.DecidedAt != nil {
			return nil, invalid("proposed Decision")
		}
		return *p, nil
	case *Ticket:
		if !label(p.Title, 800) || !oneOf(p.Status, "open", "in-progress", "done", "canceled") || p.StatusRevision < 1 || p.StatusRevision > MaxHistory {
			return nil, invalid("Ticket fields")
		}
		return *p, nil
	case *Document:
		if p.Visibility != "project" || p.Revision < 1 {
			return nil, invalid("Document fields")
		}
		return *p, nil
	}
	return nil, ErrInvalid
}

func ValidateFacts(h History) (Facts, error) {
	var f Facts
	if err := Decode(h.Facts, &f); err != nil {
		return f, err
	}
	var fields map[string]json.RawMessage
	if err := Decode(h.Facts, &fields); err != nil {
		return f, err
	}
	var expected []string
	kind := ""
	switch h.Type {
	case "project.created", "channel.created":
		kind = strings.TrimSuffix(h.Type, ".created")
		expected = []string{"status"}
		if f.Status != "active" {
			return f, invalid("creation state")
		}
	case "message.created":
		kind = "message"
		expected = []string{"channel"}
		if f.Thread != nil {
			expected = append(expected, "thread")
		}
		if !refType(f.Channel, "channel") || (f.Thread != nil && !refType(f.Thread, "thread")) {
			return f, invalid("Message history")
		}
	case "thread.started":
		kind = "thread"
		expected = []string{"channel", "message"}
		if !refType(f.Channel, "channel") || !refType(f.Message, "message") {
			return f, invalid("Thread history")
		}
	case "decision.proposed":
		kind = "decision"
		expected = []string{"status", "evidence"}
		if f.Status != "proposed" || !refType(f.Evidence, "thread") {
			return f, invalid("proposal history")
		}
	case "decision.accepted":
		kind = "decision"
		expected = []string{"status"}
		if f.Status != "accepted" {
			return f, invalid("acceptance history")
		}
	case "ticket.created":
		kind = "ticket"
		expected = []string{"status", "decision"}
		if f.Status != "open" || !refType(f.Decision, "decision") {
			return f, invalid("Ticket creation history")
		}
	case "ticket.status-changed":
		kind = "ticket"
		expected = []string{"from_status", "to_status", "from_status_revision", "to_status_revision"}
		if !ValidTransition(f.FromStatus, f.ToStatus) || f.FromRevision < 1 || f.ToRevision != f.FromRevision+1 {
			return f, invalid("Ticket transition")
		}
	case "document.created":
		kind = "document"
		expected = []string{"revision", "ticket"}
		if f.Revision != 1 || !refType(f.Ticket, "ticket") {
			return f, invalid("Document creation history")
		}
	case "document.revised":
		kind = "document"
		expected = []string{"revision"}
		if f.RestoredFrom != nil {
			expected = append(expected, "restored_from_revision")
		}
		if f.Revision < 2 || (f.RestoredFrom != nil && (*f.RestoredFrom < 1 || *f.RestoredFrom >= f.Revision)) {
			return f, invalid("Document revision history")
		}
	default:
		return f, invalid("history type")
	}
	if h.Entity.Type != kind || len(fields) != len(expected) {
		return f, invalid("history fields")
	}
	for _, name := range expected {
		if v, ok := fields[name]; !ok || bytes.Equal(v, []byte("null")) {
			return f, invalid("history fields")
		}
	}
	return f, nil
}
func ValidTransition(a, b string) bool {
	return oneOf(a, "open", "in-progress", "done", "canceled") && oneOf(b, "open", "in-progress", "done", "canceled") && a != b && (oneOf(a, "open", "in-progress") || b == "open")
}
func refType(r *Ref, kind string) bool {
	return r != nil && r.Type == kind && ValidID(r.ID, Prefix(kind))
}
func sameInt(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
func BodyPath(id string, revision int) string {
	return "documents/" + id + "/" + strconv.Itoa(revision) + ".md"
}

func validateObjectHistory(o Object, v any, events []History, x *Index, relations map[Ref]map[string][]Link) error {
	ref := Ref{o.Type, o.ID}
	create := o.Type + ".created"
	if o.Type == "decision" {
		create = "decision.proposed"
	}
	if o.Type == "thread" {
		create = "thread.started"
	}
	var first *History
	counts := map[string]int{}
	for _, h := range events {
		counts[h.Type]++
		if h.Type == create {
			copy := h
			first = &copy
		}
		if h.OccurredAt.Before(o.CreatedAt) || h.OccurredAt.After(o.UpdatedAt) {
			return invalid("object history time")
		}
	}
	if first == nil || counts[create] != 1 || first.Actor != o.Actor || !first.OccurredAt.Equal(o.CreatedAt) {
		return invalid("object creation history")
	}
	f := x.Facts[first.ID]
	oneRelation := func(relation string, target Ref) bool {
		n := 0
		for _, l := range relations[ref][relation] {
			if l.To == target {
				n++
			}
		}
		return n == 1
	}
	switch p := v.(type) {
	case Project, Channel:
		if len(events) != 1 {
			return invalid("unsupported configuration history")
		}
	case Message:
		if len(events) != 1 || f.Channel.ID != p.ChannelID || (p.ThreadID == nil) != (f.Thread == nil) || (p.ThreadID != nil && *p.ThreadID != f.Thread.ID) {
			return invalid("Message source")
		}
	case Thread:
		if len(events) != 1 || p.ChannelID == nil || *p.ChannelID != f.Channel.ID || !oneRelation("started-from", *f.Message) {
			return invalid("Thread source")
		}
		m := x.Decoded[*f.Message].(Message)
		if m.ChannelID != *p.ChannelID {
			return invalid("Thread source Channel")
		}
	case Decision:
		if p.Proposer != o.Actor || !oneRelation("derived-from", *f.Evidence) {
			return invalid("Decision source")
		}
		n := 1
		if p.Status == "accepted" {
			n = 2
			if counts["decision.accepted"] != 1 || !p.DecidedAt.Equal(o.UpdatedAt) {
				return invalid("acceptance history count")
			}
			for _, h := range events {
				if h.Type == "decision.accepted" && (h.Actor != p.Deciders[0] || !h.OccurredAt.Equal(*p.DecidedAt)) {
					return invalid("acceptance actor/time")
				}
			}
		}
		if len(events) != n {
			return invalid("Decision history count")
		}
	case Ticket:
		decision := x.Decoded[*f.Decision].(Decision)
		if decision.Status != "accepted" || decision.DecidedAt.After(o.CreatedAt) {
			return invalid("Ticket accepted source")
		}
		if !oneRelation("implements", *f.Decision) || len(events) != p.StatusRevision {
			return invalid("Ticket history count")
		}
		changes := []History{}
		for _, h := range events {
			if h.Type == "ticket.status-changed" {
				changes = append(changes, h)
			}
		}
		sort.Slice(changes, func(i, j int) bool { return x.Facts[changes[i].ID].ToRevision < x.Facts[changes[j].ID].ToRevision })
		status := "open"
		rev := 1
		last := o.CreatedAt
		for _, h := range changes {
			f := x.Facts[h.ID]
			if f.FromRevision != rev || f.FromStatus != status || h.OccurredAt.Before(last) {
				return invalid("Ticket history sequence")
			}
			status = f.ToStatus
			rev = f.ToRevision
			last = h.OccurredAt
		}
		if status != p.Status || rev != p.StatusRevision || !last.Equal(o.UpdatedAt) {
			return invalid("Ticket current state")
		}
	case Document:
		sourceLinks := 0
		for _, link := range relations[*f.Ticket]["relates-to"] {
			if link.To == ref {
				sourceLinks++
			}
		}
		if sourceLinks != 1 {
			return invalid("Document source relation")
		}
		revisions := x.Revisions[o.ID]
		sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision < revisions[j].Revision })
		if len(revisions) != p.Revision || len(events) != p.Revision {
			return invalid("Document revision count")
		}
		last := o.CreatedAt
		for i, r := range revisions {
			if r.Revision != i+1 || r.CreatedAt.Before(last) {
				return invalid("Document sequence")
			}
			last = r.CreatedAt
		}
		if !last.Equal(o.UpdatedAt) {
			return invalid("Document current time")
		}
		x.Revisions[o.ID] = revisions
	}
	return nil
}

func BuildPlan(m Manifest, d Data, workspace, identity, actor, operation string) (Plan, error) {
	if !ValidOperation(operation) || !ValidID(workspace, "wrk_") || !ValidID(actor, "usr_") || !digestPattern.MatchString(identity) {
		return Plan{}, invalid("plan target")
	}
	for _, o := range d.Objects {
		if len(o.Lineage) >= MaxLineage {
			return Plan{}, invalid("import lineage limit")
		}
	}
	for _, a := range d.Actors {
		if len(a.Lineage) >= MaxLineage {
			return Plan{}, invalid("import lineage limit")
		}
	}
	for _, l := range d.Links {
		if len(l.Lineage) >= MaxLineage {
			return Plan{}, invalid("import lineage limit")
		}
	}
	for _, h := range d.History {
		if len(h.Lineage) >= MaxLineage {
			return Plan{}, invalid("import lineage limit")
		}
	}
	p := Plan{OperationID: operation, PackageID: m.PackageID, PackageDigest: Digest(m), WorkspaceID: workspace, TargetIdentity: identity, ActorID: actor, Policy: PermissionPolicy, Objects: len(d.Objects), History: len(d.History), Revisions: len(d.Revisions), HistoricalActors: len(d.Actors)}
	seed := Hash(Encode(p))
	p.Mappings = []Mapping{}
	add := func(kind, id string) {
		p.Mappings = append(p.Mappings, Mapping{kind, id, Prefix(kind) + Hash([]byte(seed + "/" + kind + "/" + id))[:32]})
	}
	for _, o := range d.Objects {
		add(o.Type, o.ID)
	}
	for _, a := range d.Actors {
		add("actor", a.ID)
	}
	for _, l := range d.Links {
		add("entity-link", l.ID)
	}
	for _, h := range d.History {
		add("event", h.ID)
	}
	sort.Slice(p.Mappings, func(i, j int) bool {
		a, b := p.Mappings[i], p.Mappings[j]
		return a.Kind+"/"+a.SourceID < b.Kind+"/"+b.SourceID
	})
	p.PlanDigest = Hash(Encode(p))
	return p, nil
}
func MappingKey(kind, id string) string { return fmt.Sprintf("%s/%s", kind, id) }
