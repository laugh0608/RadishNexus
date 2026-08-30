package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (store *Store) RecordCompletedCIRun(
	ctx context.Context,
	command goldenpath.RecordCompletedCIRunCommand,
) (receipt goldenpath.CIRunReceipt, err error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return receipt, fmt.Errorf("begin record CI Run transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	result, err := tx.Exec(ctx, `
		INSERT INTO radishnexus.inbound_deliveries (
			workspace_id, source_kind, source_id, delivery_id,
			payload_sha256, ci_run_id, event_id, recorded_at
		) VALUES ($1, 'jenkins', $2, $3, $4, $5, $6, $7)
		ON CONFLICT (workspace_id, source_kind, source_id, delivery_id) DO NOTHING
	`, command.WorkspaceID, command.SourceID, command.DeliveryID, command.PayloadSHA256,
		command.CIRunID, command.EventID, command.RecordedAt)
	if err != nil {
		return receipt, mapDatabaseError("claim Jenkins delivery", err)
	}
	if result.RowsAffected() == 0 {
		var existingDigest string
		var existingCIRunID string
		err := tx.QueryRow(ctx, `
			SELECT payload_sha256, ci_run_id
			FROM radishnexus.inbound_deliveries
			WHERE workspace_id = $1
			  AND source_kind = 'jenkins'
			  AND source_id = $2
			  AND delivery_id = $3
		`, command.WorkspaceID, command.SourceID, command.DeliveryID).Scan(
			&existingDigest,
			&existingCIRunID,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return receipt, fmt.Errorf("load duplicate Jenkins delivery receipt: %w", authz.ErrConflict)
		}
		if err != nil {
			return receipt, fmt.Errorf("load duplicate Jenkins delivery receipt: %w", err)
		}
		if existingDigest != command.PayloadSHA256 {
			return receipt, fmt.Errorf(
				"Jenkins delivery replay changed payload digest: %w",
				authz.ErrConflict,
			)
		}
		existing, err := loadCIRun(ctx, tx, command.WorkspaceID, existingCIRunID)
		if err != nil {
			return receipt, err
		}
		return goldenpath.CIRunReceipt{CIRun: existing, Duplicate: true}, nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.ci_runs (
			id, workspace_id, component_id, source_kind, source_id,
			external_run_key, status, started_at, completed_at,
			created_at, updated_at
		) VALUES ($1, $2, $3, 'jenkins', $4, $5, $6, $7, $8, $9, $9)
	`, command.CIRunID, command.WorkspaceID, command.ComponentID, command.SourceID,
		command.ExternalRunKey, command.Status, command.StartedAt, command.CompletedAt,
		command.RecordedAt)
	if err != nil {
		return receipt, mapDatabaseError("insert completed CI Run", err)
	}

	if err := insertEvent(ctx, tx, eventRecord{
		ID:            command.EventID,
		Type:          "ci-run.recorded",
		WorkspaceID:   command.WorkspaceID,
		ActorKind:     "plugin",
		ActorID:       command.SourceID,
		SourceKind:    "plugin",
		SourceID:      command.SourceID,
		PrimaryType:   "ci-run",
		PrimaryID:     command.CIRunID,
		CorrelationID: command.CorrelationID,
		OccurredAt:    command.CompletedAt,
		Payload: map[string]any{
			"status":    command.Status,
			"component": map[string]string{"type": "component", "id": command.ComponentID},
		},
	}); err != nil {
		return receipt, err
	}
	if err := insertOutbox(ctx, tx, command.EventID); err != nil {
		return receipt, err
	}
	if err := tx.Commit(ctx); err != nil {
		return receipt, mapDatabaseError("commit completed CI Run", err)
	}

	completedAt := command.CompletedAt
	return goldenpath.CIRunReceipt{CIRun: goldenpath.CIRun{
		ID:             command.CIRunID,
		WorkspaceID:    command.WorkspaceID,
		ComponentID:    command.ComponentID,
		SourceKind:     "jenkins",
		SourceID:       command.SourceID,
		ExternalRunKey: command.ExternalRunKey,
		Status:         command.Status,
		StartedAt:      command.StartedAt,
		CompletedAt:    &completedAt,
		CreatedAt:      command.RecordedAt,
		UpdatedAt:      command.RecordedAt,
	}}, nil
}

func loadCIRun(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID string,
	ciRunID string,
) (ciRun goldenpath.CIRun, err error) {
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, component_id, source_kind, source_id,
			external_run_key, status, started_at, completed_at, created_at, updated_at
		FROM radishnexus.ci_runs
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, ciRunID).Scan(
		&ciRun.ID,
		&ciRun.WorkspaceID,
		&ciRun.ComponentID,
		&ciRun.SourceKind,
		&ciRun.SourceID,
		&ciRun.ExternalRunKey,
		&ciRun.Status,
		&ciRun.StartedAt,
		&ciRun.CompletedAt,
		&ciRun.CreatedAt,
		&ciRun.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ciRun, fmt.Errorf("load CI Run for delivery receipt: %w", authz.ErrConflict)
	}
	if err != nil {
		return ciRun, fmt.Errorf("load CI Run for delivery receipt: %w", err)
	}
	return ciRun, nil
}
