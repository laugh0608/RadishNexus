package contextpackage

import (
	"encoding/json"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const TicketClassificationChanged = "ticket.classification-changed"

type Classification struct {
	Type     string   `json:"ticket_type"`
	LabelIDs []string `json:"label_ids"`
}

func EmptyClassification() Classification {
	return Classification{Type: "unset", LabelIDs: []string{}}
}

func ValidTicketType(value string) bool {
	return oneOf(value, "unset", "requirement", "task", "defect")
}

func ASCIIKey(value string) string {
	buf := []byte(value)
	for i, b := range buf {
		if b >= 'A' && b <= 'Z' {
			buf[i] += 'a' - 'A'
		}
	}
	return string(buf)
}

func ValidTicketLabelName(name string) bool {
	return utf8.ValidString(name) && strings.TrimSpace(name) == name && utf8.RuneCountInString(name) >= 1 &&
		utf8.RuneCountInString(name) <= 40 && strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
	}) < 0
}

func (c Classification) Valid() bool {
	if !ValidTicketType(c.Type) || c.LabelIDs == nil || !slices.IsSorted(c.LabelIDs) {
		return false
	}
	for i, id := range c.LabelIDs {
		if !ValidID(id, "lbl_") || (i > 0 && id == c.LabelIDs[i-1]) {
			return false
		}
	}
	return true
}

func (c Classification) Equal(other Classification) bool {
	return c.Type == other.Type && slices.Equal(c.LabelIDs, other.LabelIDs)
}

type LabelVersion struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Revision int    `json:"revision"`
}

type ClassificationChange struct {
	From         Classification `json:"from"`
	To           Classification `json:"to"`
	FromRevision int            `json:"from_revision"`
	ToRevision   int            `json:"to_revision"`
	Labels       []LabelVersion `json:"labels"`
}

type LabelState struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Revision int    `json:"revision"`
}

type LabelChange struct {
	From *LabelState `json:"from"`
	To   LabelState  `json:"to"`
}

func (state LabelState) valid() bool {
	return ValidTicketLabelName(state.Name) && oneOf(state.Status, "active", "archived") &&
		state.Revision > 0 && state.Revision <= math.MaxInt32
}

func DecodeLabelChange(raw []byte, kind string) (LabelChange, error) {
	var envelope struct {
		Change LabelChange `json:"label"`
	}
	if !exactClassificationFacts(raw, &envelope) {
		return LabelChange{}, invalid("Ticket label event fields")
	}
	c := envelope.Change
	if !c.To.valid() {
		return c, invalid("Ticket label target state")
	}
	if kind == "ticket-label.created" && c.From == nil && c.To.Revision == 1 && c.To.Status == "active" {
		return c, nil
	}
	if c.From == nil || !c.From.valid() || c.From.Status != "active" || c.To.Revision != c.From.Revision+1 {
		return c, invalid("Ticket label lifecycle")
	}
	if (kind == "ticket-label.renamed" && c.To.Status == "active" && c.To.Name != c.From.Name) ||
		(kind == "ticket-label.archived" && c.To.Status == "archived" && c.To.Name == c.From.Name) {
		return c, nil
	}
	return c, invalid("Ticket label lifecycle")
}

func DecodeClassificationChange(raw []byte) (ClassificationChange, error) {
	var envelope struct {
		Change ClassificationChange `json:"classification"`
	}
	if !exactClassificationFacts(raw, &envelope) {
		return ClassificationChange{}, invalid("Ticket classification event fields")
	}
	c := envelope.Change
	if !c.From.Valid() || !c.To.Valid() || c.From.Equal(c.To) || c.FromRevision < 1 ||
		c.ToRevision != c.FromRevision+1 || c.ToRevision > math.MaxInt32 || c.Labels == nil {
		return c, invalid("Ticket classification transition")
	}
	ids := append(slices.Clone(c.From.LabelIDs), c.To.LabelIDs...)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) != len(c.Labels) {
		return c, invalid("Ticket classification label snapshots")
	}
	for i, label := range c.Labels {
		if label.ID != ids[i] || !ValidTicketLabelName(label.Name) || label.Revision < 1 || label.Revision > math.MaxInt32 {
			return c, invalid("Ticket classification label snapshot")
		}
	}
	return c, nil
}

func exactClassificationFacts(raw []byte, target any) bool {
	if Decode(raw, target) != nil {
		return false
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		return false
	}
	return fieldsPresent(raw, encoded)
}

// Decode has already rejected unknown fields and duplicate keys. Compare object
// keys recursively so missing nullable fields are rejected too.
func fieldsPresent(raw, encoded []byte) bool {
	var actual, expected map[string]json.RawMessage
	if json.Unmarshal(encoded, &expected) != nil || expected == nil {
		return true
	}
	if json.Unmarshal(raw, &actual) != nil || len(actual) != len(expected) {
		return false
	}
	for key, value := range expected {
		if actual[key] == nil || !fieldsPresent(actual[key], value) {
			return false
		}
	}
	return true
}

type TicketLabelObject struct {
	LabelState
	UpdatedBy string `json:"updated_by"`
}

type labelHistoryEntry struct {
	state LabelState
	at    time.Time
}

func validateLabelCatalog(d Data, x *Index) error {
	events := make([]History, 0)
	for _, h := range d.History {
		if h.Entity.Type == "ticket-label" {
			events = append(events, h)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].OccurredAt.Equal(events[j].OccurredAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].OccurredAt.Before(events[j].OccurredAt)
	})
	keys := map[string]string{}
	versions := map[string][]labelHistoryEntry{}
	for _, h := range events {
		change := x.Facts[h.ID].Label
		line := versions[h.Entity.ID]
		if change.From != nil {
			if len(line) == 0 || *change.From != line[len(line)-1].state || !h.OccurredAt.After(line[len(line)-1].at) {
				return invalid("Ticket label history sequence")
			}
			delete(keys, ASCIIKey(change.From.Name))
		} else if len(line) > 0 {
			return invalid("duplicate Ticket label creation")
		}
		key := ASCIIKey(change.To.Name)
		if keys[key] != "" {
			return invalid("duplicate Ticket label name")
		}
		keys[key] = h.Entity.ID
		versions[h.Entity.ID] = append(line, labelHistoryEntry{change.To, h.OccurredAt})
	}
	for _, h := range d.History {
		change := x.Facts[h.ID].Classification
		if change == nil {
			continue
		}
		if h.CausationID != nil {
			cause := x.History[*h.CausationID]
			if cause.Type != "ticket.created" || cause.Actor != h.Actor || cause.Entity != h.Entity ||
				change.FromRevision != 1 || !change.From.Equal(EmptyClassification()) {
				return invalid("initial Ticket classification source")
			}
		}
		for _, label := range change.Labels {
			line := versions[label.ID]
			i := sort.Search(len(line), func(i int) bool { return !line[i].at.Before(h.OccurredAt) })
			if i == 0 || (i < len(line) && line[i].at.Equal(h.OccurredAt)) {
				return invalid("Ticket label history time")
			}
			current := line[i-1].state
			if current.Revision != label.Revision || current.Name != label.Name ||
				(!slices.Contains(change.From.LabelIDs, label.ID) && current.Status != "active") {
				return invalid("Ticket classification label provenance")
			}
		}
	}
	return nil
}
