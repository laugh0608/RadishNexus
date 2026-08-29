package postgres

import (
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestProjectActivityEventUsesOnlyAllowedSafeFacts(t *testing.T) {
	t.Parallel()

	event := activityEvent{
		eventID:       "evt_1",
		eventType:     "decision.proposed",
		schemaVersion: 1,
		workspaceID:   "wrk_1",
		actorKind:     "user",
		target:        entityref.Ref{Type: "decision", ID: "dec_1"},
		occurredAt:    time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC),
		payload: []byte(`{
			"status":"proposed",
			"evidence":{"type":"thread","id":"thr_1"},
			"untrusted_title":"must not enter safe facts"
		}`),
	}

	record, err := projectActivityEvent(event)
	if err != nil {
		t.Fatalf("projectActivityEvent() error = %v", err)
	}
	if len(record.safeFacts) != 1 || record.safeFacts["status"] != "proposed" {
		t.Fatalf("safeFacts = %#v", record.safeFacts)
	}
	if len(record.subjects) != 1 || record.subjects[0] != (entityref.Ref{Type: "thread", ID: "thr_1"}) {
		t.Fatalf("subjects = %#v", record.subjects)
	}
}

func TestProjectActivityEventRejectsUnsupportedSchema(t *testing.T) {
	t.Parallel()

	_, err := projectActivityEvent(activityEvent{
		eventID:       "evt_1",
		eventType:     "ticket.created",
		schemaVersion: 2,
		target:        entityref.Ref{Type: "ticket", ID: "tkt_1"},
		payload:       []byte(`{"status":"open","decision":{"type":"decision","id":"dec_1"}}`),
	})
	if err == nil {
		t.Fatal("projectActivityEvent() error = nil, want unsupported schema error")
	}
}

func TestProjectActivityEventAcceptsCompletedCIRunFacts(t *testing.T) {
	t.Parallel()

	record, err := projectActivityEvent(activityEvent{
		eventID:       "evt_ci_1",
		eventType:     "ci-run.recorded",
		schemaVersion: 1,
		workspaceID:   "wrk_1",
		actorKind:     "plugin",
		target:        entityref.Ref{Type: "ci-run", ID: "cir_1"},
		occurredAt:    time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC),
		payload:       []byte(`{"status":"failed","component":{"type":"component","id":"cmp_1"}}`),
	})
	if err != nil {
		t.Fatalf("projectActivityEvent() error = %v", err)
	}
	if len(record.safeFacts) != 1 || record.safeFacts["status"] != "failed" {
		t.Fatalf("safeFacts = %#v", record.safeFacts)
	}
	if len(record.subjects) != 1 || record.subjects[0] != (entityref.Ref{Type: "component", ID: "cmp_1"}) {
		t.Fatalf("subjects = %#v", record.subjects)
	}
}
