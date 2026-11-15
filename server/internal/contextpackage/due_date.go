package contextpackage

import "encoding/json"

func DecodeDueDateChange(payload []byte) (DueDateChange, error) {
	var event struct {
		DueDate DueDateChange `json:"due_date"`
	}
	var raw, fields map[string]json.RawMessage
	if Decode(payload, &raw) != nil || len(raw) != 1 ||
		Decode(raw["due_date"], &fields) != nil || len(fields) != 4 ||
		fields["from"] == nil || fields["to"] == nil || fields["from_revision"] == nil || fields["to_revision"] == nil ||
		Decode(payload, &event) != nil || !event.DueDate.Valid() {
		return event.DueDate, invalid("Ticket due date event")
	}
	return event.DueDate, nil
}
