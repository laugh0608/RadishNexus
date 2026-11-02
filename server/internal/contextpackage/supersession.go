package contextpackage

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Supersession is the single event contract for both affected Decisions.
type Supersession struct {
	Status                  string `json:"status"`
	Replacement             Ref    `json:"replacement"`
	Relation                Ref    `json:"relation"`
	FromRevision            int    `json:"from_revision"`
	ToRevision              int    `json:"to_revision"`
	ReplacementFromRevision int    `json:"replacement_from_revision"`
	ReplacementToRevision   int    `json:"replacement_to_revision"`
}

func DecodeSupersession(raw []byte) (Supersession, error) {
	var f Supersession
	var fields map[string]json.RawMessage
	if Decode(raw, &fields) != nil || len(fields) != 7 || Decode(raw, &f) != nil || f.Status != "superseded" || f.Replacement.Type != "decision" || !ValidID(f.Replacement.ID, "dec_") || f.Relation.Type != "entity-link" || !ValidID(f.Relation.ID, "lnk_") || (f.FromRevision != 2 && f.FromRevision != 3) || f.ToRevision != f.FromRevision+1 || f.ReplacementFromRevision != 2 || f.ReplacementToRevision != 3 {
		return f, invalid("supersession facts")
	}
	return f, nil
}

func validateSupersessionHistory(d Data, x *Index) error {
	type state struct {
		status   string
		revision int
		at       time.Time
	}
	states := map[Ref]state{}
	events := []History{}
	for _, h := range d.History {
		if h.Entity.Type == "decision" {
			events = append(events, h)
		}
	}
	priority := func(t string) int {
		if t == "decision.proposed" {
			return 0
		}
		if t == "decision.superseded" {
			return 2
		}
		return 1
	}
	sort.Slice(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.OccurredAt.Equal(b.OccurredAt) {
			if priority(a.Type) != priority(b.Type) {
				return priority(a.Type) < priority(b.Type)
			}
			return a.ID < b.ID
		}
		return a.OccurredAt.Before(b.OccurredAt)
	})
	links := map[string]Link{}
	for _, l := range d.Links {
		if l.Relation == "supersedes" {
			links[l.ID] = l
		}
	}
	outgoing, incoming := map[Ref]bool{}, map[Ref]bool{}
	for _, h := range events {
		dec, ok := x.Decoded[h.Entity].(Decision)
		if !ok {
			return invalid("Decision history target")
		}
		current, exists := states[h.Entity]
		switch h.Type {
		case "decision.proposed":
			if exists {
				return invalid("duplicate proposal")
			}
			states[h.Entity] = state{"proposed", 1, h.OccurredAt}
		case "decision.accepted", "decision.rejected":
			if !exists || current.status != "proposed" || len(dec.Deciders) != 1 || h.Actor != dec.Deciders[0] || dec.DecidedAt == nil || !h.OccurredAt.Equal(*dec.DecidedAt) || h.OccurredAt.Before(current.at) {
				return invalid("Decision terminal history")
			}
			states[h.Entity] = state{strings.TrimPrefix(h.Type, "decision."), 2, h.OccurredAt}
		case "decision.superseded":
			f, err := DecodeSupersession(h.Facts)
			if err != nil {
				return err
			}
			replacement, present := states[f.Replacement]
			l, found := links[f.Relation.ID]
			if !exists || !present || current.status != "accepted" || replacement.status != "accepted" || h.Entity == f.Replacement || outgoing[f.Replacement] || incoming[h.Entity] || current.revision != f.FromRevision || replacement.revision != f.ReplacementFromRevision || !h.OccurredAt.After(current.at) || !h.OccurredAt.After(replacement.at) || !found || l.From != f.Replacement || l.To != h.Entity || l.EventID != h.ID || l.Actor != h.Actor || (l.Origin == "user" && h.Source != "web") || !l.CreatedAt.Equal(h.OccurredAt) || !l.UpdatedAt.Equal(h.OccurredAt) {
				return invalid("supersession history")
			}
			delete(links, f.Relation.ID)
			outgoing[f.Replacement] = true
			incoming[h.Entity] = true
			states[h.Entity] = state{"superseded", f.ToRevision, h.OccurredAt}
			states[f.Replacement] = state{"accepted", f.ReplacementToRevision, h.OccurredAt}
			x.SupersededAt[h.Entity] = h.OccurredAt
		default:
			return invalid("unsupported Decision history")
		}
	}
	if len(links) != 0 {
		return invalid("supersession without event")
	}
	for ref, v := range x.Decoded {
		dec, ok := v.(Decision)
		if !ok {
			continue
		}
		s, exists := states[ref]
		if !exists || dec.Status != s.status || dec.DecisionRevision != s.revision || ((dec.Status == "proposed" && x.Objects[ref].UpdatedAt.Before(s.at)) || (dec.Status != "proposed" && !x.Objects[ref].UpdatedAt.Equal(s.at))) {
			return invalid("Decision final revision or history")
		}
	}
	return nil
}
