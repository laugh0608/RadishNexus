package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type activityEvent struct {
	imported      bool
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
	Lifecycle            string         `json:"lifecycle"`
	Classification       string         `json:"classification"`
	Revision             int            `json:"revision"`
	RestoredFromRevision *int           `json:"restored_from_revision"`
	Ticket               *entityref.Ref `json:"ticket"`
	Status               string         `json:"status"`
	Evidence             *entityref.Ref `json:"evidence"`
	Decision             *entityref.Ref `json:"decision"`
	Component            *entityref.Ref `json:"component"`
	Environment          *entityref.Ref `json:"environment"`
	CIRun                *entityref.Ref `json:"ci_run"`
	Repository           *entityref.Ref `json:"repository"`
	LinkID               string         `json:"link_id"`
	State                string         `json:"state"`
	CheckoutState        string         `json:"checkout_state"`
	CheckoutReason       *string        `json:"checkout_reason"`
}

type activityRecord struct {
	activityEvent
	subjects  []entityref.Ref
	safeFacts map[string]string
}

// RebuildActivityProjection atomically replaces the current projection version from
// immutable domain event facts. It deliberately does not read Outbox delivery
// state, so delivery cleanup cannot remove the source needed for a rebuild.
func (store *Store) RebuildActivityProjection(ctx context.Context) (projected int, err error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin Activity rebuild transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	// Block projection writers before taking the source snapshot. READ COMMITTED
	// ensures a writer that committed while we waited is included. Writers that
	// have not committed will project after this transaction releases the lock.
	if _, err := tx.Exec(ctx, `LOCK TABLE radishnexus.activity_items IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return 0, fmt.Errorf("lock Activity projection for rebuild: %w", err)
	}

	rows, err := tx.Query(ctx, `
 SELECT e.event_id, COALESCE(h.original_type,e.event_type), e.schema_version, e.workspace_id,
 e.actor_kind, COALESCE(h.original_actor,e.actor_id), e.primary_entity_type,e.primary_entity_id,
 COALESCE(h.original_time,e.occurred_at),COALESCE(h.original_facts,e.payload),h.event_id IS NOT NULL
 FROM radishnexus.domain_events e LEFT JOIN radishnexus.context_import_history h ON h.workspace_id=e.workspace_id AND h.event_id=e.event_id
 WHERE COALESCE(h.original_type,e.event_type) IN (
 'decision.proposed','decision.accepted','ticket.created','ticket.status-changed',
 'ci-run.recorded','deployment.recorded','project.created','channel.created','component.created','environment.created','document.created','document.revised',
 'repository.created','component.repository-linked','component.repository-unlinked','ticket.component-linked','ticket.component-unlinked')
 ORDER BY COALESCE(h.original_time,e.occurred_at),e.event_id
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
			&event.imported,
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
		if err := insertActivityRecord(ctx, tx, record); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit Activity rebuild: %w", err)
	}
	return len(records), nil
}

// projectActivityDelivery runs inside the business transaction. The delivery is
// complete only when the projection and the original business facts commit.
func projectActivityDelivery(ctx context.Context, tx pgx.Tx, eventID string) error {
	var event activityEvent
	err := tx.QueryRow(ctx, `
		SELECT event_id, event_type, schema_version, workspace_id,
			actor_kind, actor_id, primary_entity_type, primary_entity_id,
			occurred_at, payload
		FROM radishnexus.domain_events WHERE event_id = $1
	`, eventID).Scan(&event.eventID, &event.eventType, &event.schemaVersion,
		&event.workspaceID, &event.actorKind, &event.actorID,
		&event.target.Type, &event.target.ID, &event.occurredAt, &event.payload)
	if err != nil {
		return fmt.Errorf("read Activity delivery event %s: %w", eventID, err)
	}
	record, err := projectActivityEvent(event)
	if err != nil {
		return err
	}
	if err := insertActivityRecord(ctx, tx, record); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE radishnexus.outbox_deliveries
		SET state = 'delivered', attempt_count = attempt_count + 1,
			delivered_at = clock_timestamp(), locked_until = NULL, last_error = NULL
		WHERE event_id = $1 AND consumer = 'activity-projector'
	`, eventID)
	if err != nil {
		return fmt.Errorf("complete Activity delivery %s: %w", eventID, err)
	}
	return nil
}

func insertActivityRecord(ctx context.Context, tx pgx.Tx, record activityRecord) error {
	subjects, err := json.Marshal(record.subjects)
	if err != nil {
		return fmt.Errorf("encode Activity subjects for event %s: %w", record.eventID, err)
	}
	safeFacts, err := json.Marshal(record.safeFacts)
	if err != nil {
		return fmt.Errorf("encode Activity safe facts for event %s: %w", record.eventID, err)
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
		return fmt.Errorf("insert Activity projection for event %s: %w", record.eventID, err)
	}
	return nil
}

func projectActivityEvent(event activityEvent) (activityRecord, error) {
	if event.schemaVersion != 1 && !(event.eventType == "ci-run.recorded" && event.schemaVersion == 2) {
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
	repositoryEvent := event.eventType == "repository.created" || event.eventType == "component.repository-linked" || event.eventType == "component.repository-unlinked"
	ticketRelation := event.eventType == "ticket.component-linked" || event.eventType == "ticket.component-unlinked"
	if payload.Status == "" && event.target.Type != "document" && event.eventType != "component.created" && !repositoryEvent && !ticketRelation && event.eventType != "ticket.status-changed" {
		return activityRecord{}, fmt.Errorf("project Activity event %s: status is required", event.eventID)
	}

	record := activityRecord{
		activityEvent: event,
		subjects:      make([]entityref.Ref, 0),
		safeFacts:     map[string]string{"status": payload.Status},
	}
	switch event.eventType {
	case "ticket.status-changed":
		transition, err := decodeTicketStatusTransition(event.payload)
		if err != nil || event.target.Type != "ticket" || event.actorKind != "user" || event.actorID == nil {
			return activityRecord{}, fmt.Errorf("invalid Ticket status event %s", event.eventID)
		}
		record.safeFacts = map[string]string{"from_status": transition.FromStatus, "to_status": transition.ToStatus,
			"from_status_revision": strconv.Itoa(transition.FromRevision), "to_status_revision": strconv.Itoa(transition.ToRevision)}
	case "ticket.component-linked", "ticket.component-unlinked":
		state := "active"
		if event.eventType == "ticket.component-unlinked" {
			state = "removed"
		}
		if event.target.Type != "ticket" || event.actorKind != "user" || event.actorID == nil || payload.Component == nil || payload.Component.Type != "component" || !goldenpath.ValidConfigurationID(payload.LinkID, "lnk_") || payload.State != state {
			return activityRecord{}, fmt.Errorf("invalid Ticket Component relation event %s", event.eventID)
		}
		record.subjects = []entityref.Ref{*payload.Component}
		record.safeFacts = map[string]string{"relation_state": state}
	case "repository.created":
		if event.target.Type != "repository" || event.actorKind != "user" || event.actorID == nil || string(event.payload) != "{}" {
			return activityRecord{}, fmt.Errorf("invalid Repository creation %s", event.eventID)
		}
		record.safeFacts = map[string]string{}
	case "component.repository-linked", "component.repository-unlinked":
		state := "active"
		if event.eventType == "component.repository-unlinked" {
			state = "removed"
		}
		if event.target.Type != "component" || event.actorKind != "user" || event.actorID == nil || payload.Repository == nil || payload.Repository.Type != "repository" || !goldenpath.ValidConfigurationID(payload.LinkID, "lnk_") || payload.State != state {
			return activityRecord{}, fmt.Errorf("invalid Repository relation event %s", event.eventID)
		}
		record.subjects = []entityref.Ref{*payload.Repository}
		record.safeFacts = map[string]string{"state": state}
	case "component.created":
		if event.target.Type != "component" || payload.Lifecycle != "active" || event.actorKind != "user" || event.actorID == nil {
			return activityRecord{}, fmt.Errorf("invalid Component creation %s", event.eventID)
		}
		record.safeFacts = map[string]string{"lifecycle": payload.Lifecycle}
	case "environment.created":
		if event.target.Type != "environment" || payload.Status != "active" || payload.Classification != "staging" || event.actorKind != "user" || event.actorID == nil {
			return activityRecord{}, fmt.Errorf("invalid Environment creation %s", event.eventID)
		}
		record.safeFacts = map[string]string{"status": payload.Status, "classification": payload.Classification}
	case "document.created", "document.revised":
		if event.target.Type != "document" || payload.Revision < 1 || event.actorKind != "user" || event.actorID == nil {
			return activityRecord{}, fmt.Errorf("invalid Document event %s", event.eventID)
		}
		record.safeFacts = map[string]string{"revision": strconv.Itoa(payload.Revision)}
		if event.eventType == "document.created" {
			if payload.Revision != 1 || payload.Ticket == nil || payload.Ticket.Type != "ticket" || payload.RestoredFromRevision != nil {
				return activityRecord{}, fmt.Errorf("invalid Document creation %s", event.eventID)
			}
			record.subjects = []entityref.Ref{*payload.Ticket}
		} else if payload.Revision < 2 || payload.Ticket != nil {
			return activityRecord{}, fmt.Errorf("invalid Document revision %s", event.eventID)
		}
		if payload.RestoredFromRevision != nil {
			if *payload.RestoredFromRevision < 1 || *payload.RestoredFromRevision >= payload.Revision {
				return activityRecord{}, fmt.Errorf("invalid Document restore %s", event.eventID)
			}
			record.safeFacts["restored_from_revision"] = strconv.Itoa(*payload.RestoredFromRevision)
		}
	case "project.created", "channel.created":
		if event.eventType != event.target.Type+".created" || payload.Status != "active" || event.actorKind != "user" || event.actorID == nil {
			return activityRecord{}, fmt.Errorf("project Activity event %s: invalid configuration creation facts", event.eventID)
		}
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
	case "ci-run.recorded":
		if event.target.Type != "ci-run" || event.actorKind != "plugin" || event.actorID == nil ||
			!isTerminalCIRunStatus(payload.Status) || payload.Component == nil {
			return activityRecord{}, fmt.Errorf("project Activity event %s: invalid ci-run.recorded facts", event.eventID)
		}
		record.subjects = []entityref.Ref{*payload.Component}
		if event.schemaVersion == 2 {
			var shape map[string]json.RawMessage
			if err := json.Unmarshal(event.payload, &shape); err != nil || len(shape) != 6 || shape["checkout_reason"] == nil || shape["link_id"] == nil || payload.Repository == nil || payload.Repository.Type != "repository" || payload.Component.Type != "component" {
				return activityRecord{}, fmt.Errorf("invalid CI checkout event %s", event.eventID)
			}
			if (payload.CheckoutState == "observed" && (payload.CheckoutReason != nil || !goldenpath.ValidConfigurationID(payload.LinkID, "lnk_"))) ||
				(payload.CheckoutState == "unavailable" && (payload.CheckoutReason == nil || !goldenpath.ValidCheckoutReason(*payload.CheckoutReason) || string(shape["link_id"]) != "null")) ||
				(payload.CheckoutState != "observed" && payload.CheckoutState != "unavailable") {
				return activityRecord{}, fmt.Errorf("invalid CI checkout state in event %s", event.eventID)
			}
			record.subjects = append(record.subjects, *payload.Repository)
			record.safeFacts["checkout_state"] = payload.CheckoutState
			if payload.CheckoutReason != nil {
				record.safeFacts["checkout_reason"] = *payload.CheckoutReason
			}
		} else if payload.CheckoutState != "" || payload.CheckoutReason != nil || payload.Repository != nil || payload.LinkID != "" {
			return activityRecord{}, fmt.Errorf("legacy CI Run contains checkout in event %s", event.eventID)
		}
	case "deployment.recorded":
		if event.target.Type != "deployment" || event.actorKind != "user" || event.actorID == nil ||
			!isTerminalDeploymentStatus(payload.Status) || payload.Environment == nil || payload.CIRun == nil {
			return activityRecord{}, fmt.Errorf("project Activity event %s: invalid deployment.recorded facts", event.eventID)
		}
		record.subjects = []entityref.Ref{*payload.Environment, *payload.CIRun}
	default:
		return activityRecord{}, fmt.Errorf("project Activity event %s: event type is not allowed", event.eventID)
	}

	for _, subject := range record.subjects {
		if err := entityref.M0Registry().Validate(subject); err != nil {
			return activityRecord{}, fmt.Errorf("project Activity event %s subject: %w", event.eventID, err)
		}
	}
	if event.imported {
		record.safeFacts["imported"] = "true"
	}
	return record, nil
}

func isTerminalCIRunStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "canceled"
}

func isTerminalDeploymentStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "canceled"
}
