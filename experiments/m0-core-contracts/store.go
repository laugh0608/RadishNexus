package corecontracts

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_core_contracts.sql
var migrationSQL string

// Migrate applies the experiment schema atomically to an empty database.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, migrationSQL); err != nil {
		return fmt.Errorf("apply schema migration: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}

// ResetSchema removes only the experiment-owned schema.
func ResetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS m0_core CASCADE"); err != nil {
		return fmt.Errorf("drop experiment schema: %w", err)
	}
	return nil
}

// Store exposes the two narrow transaction experiments needed by M0.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type ProposeDecisionCommand struct {
	WorkspaceID   string
	ProjectID     string
	ThreadID      string
	DecisionID    string
	LinkID        string
	DecisionEvent string
	LinkEvent     string
	CorrelationID string
	ProposerID    string
	Question      string
	OccurredAt    time.Time
}

// ProposeDecision proves that the business record, evidence link, immutable
// events, event targets, and Outbox intents can commit as one transaction.
func (store *Store) ProposeDecision(ctx context.Context, command ProposeDecisionCommand) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin propose Decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	occurredAt := command.OccurredAt.UTC()
	if _, err := tx.Exec(ctx, `
		INSERT INTO m0_core.decisions (
			id,
			workspace_id,
			question,
			status,
			proposer_id,
			created_by_kind,
			created_by_id,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, 'proposed', $4, 'user', $4, $5, $5)
	`, command.DecisionID, command.WorkspaceID, command.Question, command.ProposerID, occurredAt); err != nil {
		return fmt.Errorf("insert proposed Decision: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO m0_core.entity_links (
			id,
			workspace_id,
			from_type,
			from_id,
			relation_type,
			to_type,
			to_id,
			assertion,
			origin,
			created_by_kind,
			created_by_id,
			created_at,
			updated_at
		) VALUES (
			$1, $2, 'decision', $3, 'derived-from', 'thread', $4,
			'asserted', 'user', 'user', $5, $6, $6
		)
	`, command.LinkID, command.WorkspaceID, command.DecisionID, command.ThreadID, command.ProposerID, occurredAt); err != nil {
		return fmt.Errorf("insert Decision evidence link: %w", err)
	}

	decisionPayload, err := json.Marshal(map[string]string{"status": "proposed"})
	if err != nil {
		return fmt.Errorf("encode Decision event payload: %w", err)
	}
	if err := insertEvent(ctx, tx, eventRecord{
		ID:                command.DecisionEvent,
		Type:              "decision.proposed",
		WorkspaceID:       command.WorkspaceID,
		ActorKind:         "user",
		ActorID:           command.ProposerID,
		SourceKind:        "web",
		PrimaryEntityType: "decision",
		PrimaryEntityID:   command.DecisionID,
		ProjectID:         command.ProjectID,
		CorrelationID:     command.CorrelationID,
		OccurredAt:        occurredAt,
		Payload:           decisionPayload,
	}); err != nil {
		return err
	}
	if err := insertTargets(ctx, tx, command.DecisionEvent, command.WorkspaceID, []eventTarget{
		{Type: "decision", ID: command.DecisionID, Role: "primary"},
		{Type: "thread", ID: command.ThreadID, Role: "related"},
	}); err != nil {
		return err
	}

	linkPayload, err := json.Marshal(map[string]string{"state": "active"})
	if err != nil {
		return fmt.Errorf("encode EntityLink event payload: %w", err)
	}
	if err := insertEvent(ctx, tx, eventRecord{
		ID:                command.LinkEvent,
		Type:              "entity-link.created",
		WorkspaceID:       command.WorkspaceID,
		ActorKind:         "user",
		ActorID:           command.ProposerID,
		SourceKind:        "web",
		PrimaryEntityType: "entity-link",
		PrimaryEntityID:   command.LinkID,
		ProjectID:         command.ProjectID,
		CorrelationID:     command.CorrelationID,
		CausationID:       command.DecisionEvent,
		OccurredAt:        occurredAt,
		Payload:           linkPayload,
	}); err != nil {
		return err
	}
	if err := insertTargets(ctx, tx, command.LinkEvent, command.WorkspaceID, []eventTarget{
		{Type: "entity-link", ID: command.LinkID, Role: "primary"},
		{Type: "decision", ID: command.DecisionID, Role: "related"},
		{Type: "thread", ID: command.ThreadID, Role: "related"},
	}); err != nil {
		return err
	}

	if err := insertOutboxIntent(ctx, tx, command.DecisionEvent, "activity"); err != nil {
		return err
	}
	if err := insertOutboxIntent(ctx, tx, command.LinkEvent, "activity"); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit proposed Decision: %w", err)
	}
	return nil
}

type RecordCIRunCommand struct {
	WorkspaceID    string
	IntegrationID  string
	DeliveryID     string
	ExternalRunKey string
	CIRunID        string
	EventID        string
	CorrelationID  string
	Status         string
	OccurredAt     time.Time
}

// RecordCIRun records the first webhook delivery and ignores later deliveries
// with the same external identity before any business side effect occurs.
func (store *Store) RecordCIRun(ctx context.Context, command RecordCIRunCommand) (bool, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin CI Run delivery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	occurredAt := command.OccurredAt.UTC()
	result, err := tx.Exec(ctx, `
		INSERT INTO m0_core.inbound_deliveries (
			workspace_id,
			source_kind,
			source_id,
			delivery_id,
			received_at
		) VALUES ($1, 'jenkins', $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, command.WorkspaceID, command.IntegrationID, command.DeliveryID, occurredAt)
	if err != nil {
		return false, fmt.Errorf("claim CI Run delivery: %w", err)
	}
	if result.RowsAffected() == 0 {
		if err := tx.Rollback(ctx); err != nil {
			return false, fmt.Errorf("rollback duplicate CI Run delivery: %w", err)
		}
		return false, nil
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO m0_core.ci_runs (
			id,
			workspace_id,
			integration_id,
			external_run_key,
			status,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, command.CIRunID, command.WorkspaceID, command.IntegrationID, command.ExternalRunKey, command.Status, occurredAt); err != nil {
		return false, fmt.Errorf("insert CI Run: %w", err)
	}

	payload, err := json.Marshal(map[string]string{"status": command.Status})
	if err != nil {
		return false, fmt.Errorf("encode CI Run event payload: %w", err)
	}
	if err := insertEvent(ctx, tx, eventRecord{
		ID:                command.EventID,
		Type:              "ci-run.recorded",
		WorkspaceID:       command.WorkspaceID,
		ActorKind:         "plugin",
		ActorID:           command.IntegrationID,
		SourceKind:        "plugin",
		SourceID:          command.IntegrationID,
		PrimaryEntityType: "ci-run",
		PrimaryEntityID:   command.CIRunID,
		CorrelationID:     command.CorrelationID,
		OccurredAt:        occurredAt,
		Payload:           payload,
	}); err != nil {
		return false, err
	}
	if err := insertTargets(ctx, tx, command.EventID, command.WorkspaceID, []eventTarget{
		{Type: "ci-run", ID: command.CIRunID, Role: "primary"},
	}); err != nil {
		return false, err
	}
	if err := insertOutboxIntent(ctx, tx, command.EventID, "activity"); err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE m0_core.inbound_deliveries
		SET state = 'processed', processed_at = $1, processed_event_id = $2
		WHERE workspace_id = $3
		  AND source_kind = 'jenkins'
		  AND source_id = $4
		  AND delivery_id = $5
	`, occurredAt, command.EventID, command.WorkspaceID, command.IntegrationID, command.DeliveryID); err != nil {
		return false, fmt.Errorf("finish CI Run delivery: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit CI Run delivery: %w", err)
	}
	return true, nil
}

// RebuildActivities atomically replaces one projection version.
func (store *Store) RebuildActivities(ctx context.Context, projectionVersion int) (int, error) {
	var count int
	if err := store.pool.QueryRow(
		ctx,
		"SELECT m0_core.rebuild_activities($1)",
		projectionVersion,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("rebuild Activity projection: %w", err)
	}
	return count, nil
}

type eventRecord struct {
	ID                string
	Type              string
	WorkspaceID       string
	ActorKind         string
	ActorID           string
	SourceKind        string
	SourceID          string
	PrimaryEntityType string
	PrimaryEntityID   string
	ProjectID         string
	CorrelationID     string
	CausationID       string
	OccurredAt        time.Time
	Payload           []byte
}

func insertEvent(ctx context.Context, tx pgx.Tx, event eventRecord) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO m0_core.domain_events (
			event_id,
			event_type,
			schema_version,
			workspace_id,
			actor_kind,
			actor_id,
			source_kind,
			source_id,
			primary_entity_type,
			primary_entity_id,
			project_id,
			correlation_id,
			causation_id,
			occurred_at,
			payload
		) VALUES (
			$1, $2, 1, $3, $4, NULLIF($5, ''), $6, NULLIF($7, ''),
			$8, $9, NULLIF($10, ''), $11, NULLIF($12, ''), $13, $14::jsonb
		)
	`,
		event.ID,
		event.Type,
		event.WorkspaceID,
		event.ActorKind,
		event.ActorID,
		event.SourceKind,
		event.SourceID,
		event.PrimaryEntityType,
		event.PrimaryEntityID,
		event.ProjectID,
		event.CorrelationID,
		event.CausationID,
		event.OccurredAt,
		string(event.Payload),
	); err != nil {
		return fmt.Errorf("insert domain event %s: %w", event.ID, err)
	}
	return nil
}

type eventTarget struct {
	Type string
	ID   string
	Role string
}

func insertTargets(
	ctx context.Context,
	tx pgx.Tx,
	eventID string,
	workspaceID string,
	targets []eventTarget,
) error {
	for _, target := range targets {
		if _, err := tx.Exec(ctx, `
			INSERT INTO m0_core.domain_event_targets (
				event_id,
				workspace_id,
				target_type,
				target_id,
				role
			) VALUES ($1, $2, $3, $4, $5)
		`, eventID, workspaceID, target.Type, target.ID, target.Role); err != nil {
			return fmt.Errorf("insert event target %s/%s: %w", target.Type, target.ID, err)
		}
	}
	return nil
}

func insertOutboxIntent(ctx context.Context, tx pgx.Tx, eventID string, consumer string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO m0_core.outbox_deliveries (event_id, consumer)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, eventID, consumer); err != nil {
		return fmt.Errorf("insert Outbox intent for event %s: %w", eventID, err)
	}
	return nil
}
