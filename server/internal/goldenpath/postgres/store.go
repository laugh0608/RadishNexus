// Package postgres implements the Golden Path Store with native pgx transactions.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type projectAccess struct {
	role   authz.ProjectRole
	active bool
}

func (store *Store) CreateDecisionFromThread(
	ctx context.Context,
	command goldenpath.CreateDecisionCommand,
) (result goldenpath.CreateDecisionResult, err error) {
	if err := command.Principal.ValidateUser(); err != nil {
		return result, err
	}

	tx, err := store.beginAdmission(ctx, command.Principal, pgx.TxOptions{})
	if err != nil {
		return result, fmt.Errorf("begin create Decision transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	projectID, access, err := readableThread(ctx, tx, command.Principal, command.ThreadID)
	if err != nil {
		return result, err
	}
	if !access.active {
		return result, fmt.Errorf("%w: archived Project does not accept new Decisions", authz.ErrConflict)
	}
	if err := authz.RequireContribute(access.role); err != nil {
		return result, err
	}

	receipt, duplicate, err := claimCollaborationCommand(
		ctx,
		tx,
		command.Invocation,
		commandDecisionPropose,
		"thread",
		command.ThreadID,
		command.ClientOperationID,
		command.PayloadSHA256,
		"decision",
		command.DecisionID,
		command.EventID,
		command.OccurredAt,
	)
	if err != nil {
		return result, err
	}
	if duplicate {
		result.Decision, err = loadDecision(
			ctx,
			tx,
			command.Principal.WorkspaceID,
			receipt.resultID,
		)
		if err != nil {
			return result, err
		}
		if err := tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("commit duplicate Decision proposal lookup: %w", err)
		}
		return result, nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.decisions (
			id, workspace_id, governing_project_id, question, status,
			proposer_id, created_by_kind, created_by_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'proposed', $5, 'user', $5, $6, $6)
	`, command.DecisionID, command.Principal.WorkspaceID, projectID, command.Question, command.Principal.ID, command.OccurredAt)
	if err != nil {
		return result, mapDatabaseError("insert proposed Decision", err)
	}

	if err := insertEvent(ctx, tx, eventRecord{
		ID:            command.EventID,
		Type:          "decision.proposed",
		WorkspaceID:   command.Principal.WorkspaceID,
		ActorKind:     "user",
		ActorID:       command.Principal.ID,
		SourceKind:    command.SourceKind,
		SourceID:      command.SourceID,
		PrimaryType:   "decision",
		PrimaryID:     command.DecisionID,
		ProjectID:     projectID,
		CorrelationID: command.CorrelationID,
		CausationID:   command.CausationID,
		OccurredAt:    command.OccurredAt,
		Payload: map[string]any{
			"status":   "proposed",
			"evidence": map[string]string{"type": "thread", "id": command.ThreadID},
		},
	}); err != nil {
		return result, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.entity_links (
			id, workspace_id, from_type, from_id, relation_type, to_type, to_id,
			assertion, origin, created_by_kind, created_by_id, created_at, updated_at, source_event_id
		) VALUES ($1, $2, 'decision', $3, 'derived-from', 'thread', $4,
			'asserted', 'user', 'user', $5, $6, $6, $7)
	`, command.LinkID, command.Principal.WorkspaceID, command.DecisionID, command.ThreadID,
		command.Principal.ID, command.OccurredAt, command.EventID)
	if err != nil {
		return result, mapDatabaseError("insert Decision evidence link", err)
	}
	if err := insertOutbox(ctx, tx, command.EventID); err != nil {
		return result, err
	}

	if err := tx.Commit(ctx); err != nil {
		return result, mapDatabaseError("commit proposed Decision", err)
	}

	result.Decision = goldenpath.Decision{
		DecisionRevision:   1,
		ID:                 command.DecisionID,
		WorkspaceID:        command.Principal.WorkspaceID,
		GoverningProjectID: projectID,
		Question:           command.Question,
		Status:             "proposed",
		ProposerID:         command.Principal.ID,
		DeciderIDs:         []string{},
		CreatedAt:          command.OccurredAt,
		UpdatedAt:          command.OccurredAt,
	}
	result.Created = true
	return result, nil
}

func (store *Store) CreateTicketFromDecision(
	ctx context.Context,
	command goldenpath.CreateTicketCommand,
) (result goldenpath.CreateTicketResult, err error) {
	if err := command.Principal.ValidateUser(); err != nil {
		return result, err
	}

	tx, err := store.beginAdmission(ctx, command.Principal, pgx.TxOptions{})
	if err != nil {
		return result, fmt.Errorf("begin create Ticket transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	projectID, access, err := decisionProjectAccess(ctx, tx, command.Principal, command.DecisionID)
	if err != nil {
		return result, err
	}
	if !access.active {
		return result, fmt.Errorf("%w: archived Project does not accept new Tickets", authz.ErrConflict)
	}
	if err := authz.RequireContribute(access.role); err != nil {
		return result, err
	}
	var decisionStatus string
	var decisionUpdated time.Time
	if err = tx.QueryRow(ctx, `SELECT status,updated_at FROM radishnexus.decisions WHERE workspace_id=$1 AND id=$2 FOR SHARE`, command.Principal.WorkspaceID, command.DecisionID).Scan(&decisionStatus, &decisionUpdated); err != nil {
		return result, fmt.Errorf("lock Decision for Ticket: %w", err)
	}
	if err = tx.QueryRow(ctx, `SELECT GREATEST(clock_timestamp(),$1::timestamptz+interval '1 microsecond')`, decisionUpdated).Scan(&command.OccurredAt); err != nil {
		return result, fmt.Errorf("read Ticket creation time: %w", err)
	}

	receipt, duplicate, err := claimCollaborationCommand(
		ctx,
		tx,
		command.Invocation,
		commandTicketCreate,
		"decision",
		command.DecisionID,
		command.ClientOperationID,
		command.PayloadSHA256,
		"ticket",
		command.TicketID,
		command.EventID,
		command.OccurredAt,
	)
	if err != nil {
		return result, err
	}
	if duplicate {
		result.Ticket, err = loadTicket(
			ctx,
			tx,
			command.Principal.WorkspaceID,
			receipt.resultID,
		)
		if err != nil {
			return result, err
		}
		var exact bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.domain_events e JOIN radishnexus.entity_links l ON l.workspace_id=e.workspace_id AND l.source_event_id=e.event_id JOIN radishnexus.collaboration_command_receipts r ON r.workspace_id=e.workspace_id AND r.event_id=e.event_id WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.event_type='ticket.created' AND e.primary_entity_type='ticket' AND e.primary_entity_id=$3 AND e.project_id=$4 AND e.actor_kind='user' AND e.actor_id=$5 AND e.payload=jsonb_build_object('status','open','decision',jsonb_build_object('type','decision','id',$6::text)) AND l.from_type='ticket' AND l.from_id=$3 AND l.to_type='decision' AND l.to_id=$6 AND l.relation_type='implements' AND e.occurred_at=l.created_at AND e.occurred_at=r.created_at AND r.command_kind='ticket.create' AND r.actor_id=$5 AND r.target_id=$6 AND r.result_id=$3 AND r.client_operation_id=$7)`, command.Principal.WorkspaceID, receipt.eventID, receipt.resultID, projectID, command.Principal.ID, command.DecisionID, command.ClientOperationID).Scan(&exact)
		if err != nil {
			return result, fmt.Errorf("verify Ticket creation receipt: %w", err)
		}
		if !exact {
			return result, fmt.Errorf("invalid Ticket creation receipt provenance")
		}
		if err := tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("commit duplicate Ticket creation lookup: %w", err)
		}
		return result, nil
	}

	if decisionStatus != "accepted" {
		return result, fmt.Errorf("%w: Ticket requires an accepted Decision", authz.ErrConflict)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.tickets (
			id, workspace_id, governing_project_id, title, status,
			created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'open', $5, $6, $6)
	`, command.TicketID, command.Principal.WorkspaceID, projectID, command.Title,
		command.Principal.ID, command.OccurredAt)
	if err != nil {
		return result, mapDatabaseError("insert Ticket", err)
	}

	if err := insertEvent(ctx, tx, eventRecord{
		ID:            command.EventID,
		Type:          "ticket.created",
		WorkspaceID:   command.Principal.WorkspaceID,
		ActorKind:     "user",
		ActorID:       command.Principal.ID,
		SourceKind:    command.SourceKind,
		SourceID:      command.SourceID,
		PrimaryType:   "ticket",
		PrimaryID:     command.TicketID,
		ProjectID:     projectID,
		CorrelationID: command.CorrelationID,
		CausationID:   command.CausationID,
		OccurredAt:    command.OccurredAt,
		Payload: map[string]any{
			"status":   "open",
			"decision": map[string]string{"type": "decision", "id": command.DecisionID},
		},
	}); err != nil {
		return result, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.entity_links (
			id, workspace_id, from_type, from_id, relation_type, to_type, to_id,
			assertion, origin, created_by_kind, created_by_id, created_at, updated_at, source_event_id
		) VALUES ($1, $2, 'ticket', $3, 'implements', 'decision', $4,
			'asserted', 'user', 'user', $5, $6, $6, $7)
	`, command.LinkID, command.Principal.WorkspaceID, command.TicketID, command.DecisionID,
		command.Principal.ID, command.OccurredAt, command.EventID)
	if err != nil {
		return result, mapDatabaseError("insert Ticket implementation link", err)
	}
	if err := insertOutbox(ctx, tx, command.EventID); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, mapDatabaseError("commit Ticket", err)
	}

	result.Ticket = goldenpath.Ticket{
		ID:                 command.TicketID,
		WorkspaceID:        command.Principal.WorkspaceID,
		GoverningProjectID: projectID,
		Title:              command.Title,
		Status:             "open",
		StatusRevision:     1,
		CreatedBy:          command.Principal.ID,
		CreatedAt:          command.OccurredAt,
		UpdatedAt:          command.OccurredAt,
	}
	result.Created = true
	return result, nil
}

func rollback(ctx context.Context, tx pgx.Tx, returnedError *error) {
	err := tx.Rollback(ctx)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) && *returnedError == nil {
		*returnedError = fmt.Errorf("rollback transaction: %w", err)
	}
}

type eventRecord struct {
	SchemaVersion int
	ID            string
	Type          string
	WorkspaceID   string
	ActorKind     string
	ActorID       string
	SourceKind    string
	SourceID      string
	PrimaryType   string
	PrimaryID     string
	ProjectID     string
	CorrelationID string
	CausationID   string
	OccurredAt    time.Time
	Payload       map[string]any
}

func insertEvent(ctx context.Context, tx pgx.Tx, event eventRecord) error {
	version := event.SchemaVersion
	if version == 0 {
		version = 1
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("encode %s event payload: %w", event.Type, err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.domain_events (
			event_id, event_type, schema_version, workspace_id,
			actor_kind, actor_id, source_kind, source_id,
			primary_entity_type, primary_entity_id, project_id,
			correlation_id, causation_id, occurred_at, payload
		) VALUES ($1, $2, $15, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14)
	`, event.ID, event.Type, event.WorkspaceID, event.ActorKind, nullable(event.ActorID),
		event.SourceKind, nullable(event.SourceID), event.PrimaryType, event.PrimaryID,
		nullable(event.ProjectID), event.CorrelationID, nullable(event.CausationID), event.OccurredAt, payload, version)
	if err != nil {
		return mapDatabaseError("insert "+event.Type+" domain event", err)
	}
	return nil
}

func insertOutbox(ctx context.Context, tx pgx.Tx, eventID string) error {
	if err := insertOutboxFor(ctx, tx, eventID, "activity-projector"); err != nil {
		return err
	}
	return projectActivityDelivery(ctx, tx, eventID)
}

func insertOutboxFor(ctx context.Context, tx pgx.Tx, eventID string, consumer string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO radishnexus.outbox_deliveries (event_id, consumer)
		VALUES ($1, $2)
	`, eventID, consumer)
	if err != nil {
		return mapDatabaseError("insert Outbox delivery", err)
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mapDatabaseError(operation string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "23505":
			return fmt.Errorf("%s: %w", operation, authz.ErrConflict)
		case "23503", "23514", "22P02":
			return fmt.Errorf("%s: %w", operation, authz.ErrInvalid)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
