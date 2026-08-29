package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (store *Store) RecordStagingDeployment(
	ctx context.Context,
	command goldenpath.RecordStagingDeploymentCommand,
) (deployment goldenpath.Deployment, err error) {
	if err := command.Principal.ValidateUser(); err != nil {
		return deployment, err
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return deployment, fmt.Errorf("begin record staging Deployment transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	member, err := activeWorkspaceMember(ctx, tx, command.Principal)
	if err != nil {
		return deployment, err
	}
	if !member {
		return deployment, authz.ErrNotFound
	}

	var environmentClassification string
	var environmentStatus string
	err = tx.QueryRow(ctx, `
		SELECT classification, status
		FROM radishnexus.environments
		WHERE workspace_id = $1 AND id = $2
		FOR SHARE
	`, command.Principal.WorkspaceID, command.EnvironmentID).Scan(
		&environmentClassification,
		&environmentStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return deployment, authz.ErrNotFound
	}
	if err != nil {
		return deployment, fmt.Errorf("load staging Environment: %w", err)
	}
	if environmentClassification != "staging" || environmentStatus != "active" {
		return deployment, fmt.Errorf("%w: Deployment target must be an active staging Environment", authz.ErrConflict)
	}

	var authorizationID string
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM radishnexus.environment_deployment_authorizations
		WHERE workspace_id = $1
		  AND environment_id = $2
		  AND user_id = $3
		  AND status = 'active'
		FOR SHARE
	`, command.Principal.WorkspaceID, command.EnvironmentID, command.Principal.ID).Scan(
		&authorizationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return deployment, fmt.Errorf("%w: explicit staging Deployment authorization is required", authz.ErrForbidden)
	}
	if err != nil {
		return deployment, fmt.Errorf("load staging Deployment authorization: %w", err)
	}

	var ciRunStatus string
	err = tx.QueryRow(ctx, `
		SELECT status
		FROM radishnexus.ci_runs
		WHERE workspace_id = $1 AND id = $2
		FOR SHARE
	`, command.Principal.WorkspaceID, command.CIRunID).Scan(&ciRunStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return deployment, authz.ErrNotFound
	}
	if err != nil {
		return deployment, fmt.Errorf("load CI Run for staging Deployment: %w", err)
	}
	if ciRunStatus != "succeeded" {
		return deployment, fmt.Errorf("%w: staging Deployment requires a succeeded CI Run", authz.ErrConflict)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.deployments (
			id, workspace_id, environment_id, ci_run_id, authorization_id,
			status, started_at, completed_at, recorded_by,
			source_kind, source_id, recorded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, command.DeploymentID, command.Principal.WorkspaceID, command.EnvironmentID,
		command.CIRunID, authorizationID, command.Status, command.StartedAt,
		command.CompletedAt, command.Principal.ID, command.SourceKind,
		nullable(command.SourceID), command.RecordedAt)
	if err != nil {
		return deployment, mapDatabaseError("insert staging Deployment", err)
	}

	if err := insertEvent(ctx, tx, eventRecord{
		ID:            command.EventID,
		Type:          "deployment.recorded",
		WorkspaceID:   command.Principal.WorkspaceID,
		ActorKind:     "user",
		ActorID:       command.Principal.ID,
		SourceKind:    command.SourceKind,
		SourceID:      command.SourceID,
		PrimaryType:   "deployment",
		PrimaryID:     command.DeploymentID,
		CorrelationID: command.CorrelationID,
		CausationID:   command.CausationID,
		OccurredAt:    command.CompletedAt,
		Payload: map[string]any{
			"status":      command.Status,
			"environment": map[string]string{"type": "environment", "id": command.EnvironmentID},
			"ci_run":      map[string]string{"type": "ci-run", "id": command.CIRunID},
		},
	}); err != nil {
		return deployment, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.entity_links (
			id, workspace_id, from_type, from_id, relation_type, to_type, to_id,
			assertion, origin, created_by_kind, created_by_id,
			created_at, updated_at, source_event_id
		) VALUES ($1, $2, 'deployment', $3, 'deploys', 'ci-run', $4,
			'asserted', 'user', 'user', $5, $6, $6, $7)
	`, command.LinkID, command.Principal.WorkspaceID, command.DeploymentID,
		command.CIRunID, command.Principal.ID, command.RecordedAt, command.EventID)
	if err != nil {
		return deployment, mapDatabaseError("insert Deployment CI Run link", err)
	}
	if err := insertOutbox(ctx, tx, command.EventID); err != nil {
		return deployment, err
	}

	if err := tx.Commit(ctx); err != nil {
		return deployment, mapDatabaseError("commit staging Deployment", err)
	}

	return goldenpath.Deployment{
		ID:            command.DeploymentID,
		WorkspaceID:   command.Principal.WorkspaceID,
		EnvironmentID: command.EnvironmentID,
		CIRunID:       command.CIRunID,
		Status:        command.Status,
		StartedAt:     command.StartedAt,
		CompletedAt:   command.CompletedAt,
		RecordedBy:    command.Principal.ID,
		SourceKind:    command.SourceKind,
		SourceID:      command.SourceID,
		RecordedAt:    command.RecordedAt,
	}, nil
}
