package contextpackage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDueDateProfileAndHistoryReplay(t *testing.T) {
	b := fixture(t)
	b.Manifest.Profile = ProfileV6
	if _, _, err := Unpack(b); err == nil {
		t.Fatal("v6 accepted v7 Ticket fields")
	}
	stripTicketFields(t, &b, "due_date", "due_date_revision")
	before := Digest(b.Manifest)
	data, index, err := Unpack(b)
	if err != nil || Digest(b.Manifest) != before {
		t.Fatal("legacy normalization changed digest", err)
	}
	ref := Ref{"ticket", "tkt_a"}
	row := index.Decoded[ref].(Ticket)
	if row.DueDate != nil || row.DueDateRevision != 1 {
		t.Fatal(row)
	}

	day := "2032-02-29"
	b.Manifest.CreatedAt = b.Manifest.CreatedAt.Add(time.Second)
	change := History{ID: "evt_due", Type: "ticket.due-date-changed", Version: 1, Entity: ref,
		Actor: "usr_a", Source: "web", OccurredAt: b.Manifest.CreatedAt, CorrelationID: "cor_due", Lineage: []Origin{}}
	change.Facts = Encode(Facts{DueDate: &DueDateChange{To: &day, FromRevision: 1, ToRevision: 2}})
	row.DueDate, row.DueDateRevision = &day, 2
	for i := range data.Objects {
		if data.Objects[i].ID == ref.ID {
			data.Objects[i].Data = Encode(row)
			data.Objects[i].UpdatedAt = change.OccurredAt
		}
	}
	data.History = append(data.History, change)
	current, err := Pack(b.Manifest, data)
	if err != nil || current.Manifest.Profile != Profile {
		t.Fatal("v7 due date rejected", err)
	}

	for _, facts := range []string{
		`{"due_date":{"to":"2032-02-29","from_revision":1,"to_revision":2}}`,
		`{"due_date":{"from":null,"to":"2031-02-29","from_revision":1,"to_revision":2}}`,
		`{"due_date":{"from":null,"to":"2032-02-28","from_revision":1,"to_revision":2}}`,
		`{"due_date":{"from":"2032-02-27","to":"2032-02-29","from_revision":1,"to_revision":2}}`,
		`{"due_date":{"from":null,"to":"2032-02-29","from_revision":2,"to_revision":3}}`,
	} {
		data.History[len(data.History)-1].Facts = json.RawMessage(facts)
		if _, err := Pack(b.Manifest, data); err == nil {
			t.Fatal("damaged history accepted", facts)
		}
	}

	current.Manifest.Profile = ProfileV6
	stripTicketFields(t, &current, "due_date", "due_date_revision")
	if _, _, err := Unpack(current); err == nil {
		t.Fatal("v6 accepted due date history")
	}
}
