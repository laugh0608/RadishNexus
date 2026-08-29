package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type activityEvent struct {
	eventID       string
	eventType     string
	schemaVersion int
	workspaceID   string
	actorKind     string
	actorID       *string
	target        entityref.Ref
	occurredAt    time.Time
	payload       []byte
}

type activityEventPayload struct {
	Status   string         `json:"status"`
	Evidence *entityref.Ref `json:"evidence"`
	Decision *entityref.Ref `json:"decision"`
}

type activityRecord struct {
	activityEvent
	subjects  []entityref.Ref
	safeFacts map[string]string
}

// RebuildActivityProjection atomically replaces projection version 1 from
// immutable domain event facts. It deliberately does not read Outbox delivery
// state, so delivery cleanup cannot remove the source needed for a rebuild.
func (store *Store) RebuildActivityProjection(ctx context.Context) (projected int, err error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return 0, fmt.Errorf("begin Activity rebuild transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	rows, err := tx.Query(ctx, `
		SELECT event_id, event_type, schema_version, workspace_id,
			actor_kind, actor_id, primary_entity_type, primary_entity_id,
			occurred_at, payload
		FROM radishnexus.domain_events
		WHERE event_type IN ('decision.proposed', 'decision.accepted', 'ticket.created')
		ORDER BY occurred_at, event_id
	`)
	if err != nil {
		return 0, fmt.Errorf("read Activity source events: %w", err)
	}

	records := make([]activityRecord, 0)
	for rows.Next() {
		var event activityEvent
		if err := rows.Scan(
			&event.eventID,
			&event.eventType,
			&event.schemaVersion,
			&event.workspaceID,
			&event.actorKind,
			&event.actorID,
			&event.target.Type,
			&event.target.ID,
			&event.occurredAt,
			&event.payload,
		); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan Activity source event: %w", err)
		}
		record, err := projectActivityEvent(event)
		if err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate Activity source events: %w", err)
	}
	rows.Close()

	if _, err := tx.Exec(ctx, `
		DELETE FROM radishnexus.activity_items
		WHERE projection_version = $1
	`, goldenpath.ActivityProjectionVersion); err != nil {
		return 0, fmt.Errorf("clear Activity projection version %d: %w", goldenpath.ActivityProjectionVersion, err)
	}

	for _, record := range records {
		subjects, err := json.Marshal(record.subjects)
		if err != nil {
			return 0, fmt.Errorf("encode Activity subjects for event %s: %w", record.eventID, err)
		}
		safeFacts, err := json.Marshal(record.safeFacts)
		if err != nil {
			return 0, fmt.Errorf("encode Activity safe facts for event %s: %w", record.eventID, err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO radishnexus.activity_items (
				workspace_id, target_type, target_id, event_id, activity_type,
				actor_kind, actor_id, occurred_at, subject_refs,
				projection_version, safe_facts
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, record.workspaceID, record.target.Type, record.target.ID,
			record.eventID, record.eventType, record.actorKind, record.actorID,
			record.occurredAt, subjects, goldenpath.ActivityProjectionVersion, safeFacts)
		if err != nil {
			return 0, fmt.Errorf("insert Activity projection for event %s: %w", record.eventID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit Activity rebuild: %w", err)
	}
	return len(records), nil
}

func projectActivityEvent(event activityEvent) (activityRecord, error) {
	if event.schemaVersion != 1 {
		return activityRecord{}, fmt.Errorf(
			"project Activity event %s: unsupported %s schema version %d",
			event.eventID,
			event.eventType,
			event.schemaVersion,
		)
	}
	if err := entityref.M0Registry().Validate(event.target); err != nil {
		return activityRecord{}, fmt.Errorf("project Activity event %s target: %w", event.eventID, err)
	}

	var payload activityEventPayload
	if err := json.Unmarshal(event.payload, &payload); err != nil {
		return activityRecord{}, fmt.Errorf("decode Activity event %s payload: %w", event.eventID, err)
	}
	if payload.Status == "" {
		return activityRecord{}, fmt.Errorf("project Activity event %s: status is required", event.eventID)
	}

	record := activityRecord{
		activityEvent: event,
		subjects:      make([]entityref.Ref, 0),
		safeFacts:     map[string]string{"status": payload.Status},
	}
	switch event.eventType {
	case "decision.proposed":
		if event.target.Type != "decision" || payload.Status != "proposed" || payload.Evidence == nil {
			return activityRecord{}, fmt.Errorf("project Activity event %s: invalid decision.proposed facts", event.eventID)
		}
		record.subjects = []entityref.Ref{*payload.Evidence}
	case "decision.accepted":
		if event.target.Type != "decision" || payload.Status != "accepted" {
			return activityRecord{}, fmt.Errorf("project Activity event %s: invalid decision.accepted facts", event.eventID)
		}
	case "ticket.created":
		if event.target.Type != "ticket" || payload.Status != "open" || payload.Decision == nil {
			return activityRecord{}, fmt.Errorf("project Activity event %s: invalid ticket.created facts", event.eventID)
		}
		record.subjects = []entityref.Ref{*payload.Decision}
	default:
		return activityRecord{}, fmt.Errorf("project Activity event %s: event type is not allowed", event.eventID)
	}

	for _, subject := range record.subjects {
		if err := entityref.M0Registry().Validate(subject); err != nil {
			return activityRecord{}, fmt.Errorf("project Activity event %s subject: %w", event.eventID, err)
		}
	}
	return record, nil
}
