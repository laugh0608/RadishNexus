package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
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

	for _, ref := range []entityref.Ref{{Type: "ci-run", ID: command.CIRunID}, {Type: "environment", ID: command.EnvironmentID}} {
		exists, readable, e := entityAccess(ctx, tx, command.Principal, ref)
		if e != nil {
			return deployment, e
		}
		if !exists || !readable {
			return deployment, authz.ErrNotFound
		}
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

	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, command.Invocation, "deployment.record", "ci-run", command.CIRunID, command.ClientOperationID, command.PayloadSHA256, "deployment", command.DeploymentID, command.EventID, command.RecordedAt)
	if err != nil {
		return deployment, err
	}
	if duplicate {
		deployment, err = loadDeployment(ctx, tx, command.Principal.WorkspaceID, receipt.resultID)
		if err != nil {
			return deployment, err
		}
		if err = tx.Commit(ctx); err != nil {
			return deployment, fmt.Errorf("commit Deployment retry: %w", err)
		}
		deployment.Duplicate = true
		return deployment, nil
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

func loadDeployment(ctx context.Context, tx pgx.Tx, workspace, id string) (d goldenpath.Deployment, err error) {
	err = tx.QueryRow(ctx, `SELECT id,workspace_id,environment_id,ci_run_id,status,started_at,completed_at,recorded_by,source_kind,COALESCE(source_id,''),recorded_at FROM radishnexus.deployments WHERE workspace_id=$1 AND id=$2`, workspace, id).Scan(&d.ID, &d.WorkspaceID, &d.EnvironmentID, &d.CIRunID, &d.Status, &d.StartedAt, &d.CompletedAt, &d.RecordedBy, &d.SourceKind, &d.SourceID, &d.RecordedAt)
	if err != nil {
		return d, fmt.Errorf("load Deployment receipt result: %w", err)
	}
	return d, nil
}

func (s *Store) ListStagingTargets(ctx context.Context, p authz.Principal, ciRun string, in goldenpath.DiscoveryPageInput) (page goldenpath.StagingTargetPage, err error) {
	if err = p.ValidateUser(); err != nil {
		return page, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, fmt.Errorf("begin staging targets: %w", err)
	}
	defer rollback(ctx, tx, &err)
	member, err := activeWorkspaceMember(ctx, tx, p)
	if err != nil {
		return page, err
	}
	if !member {
		return page, authz.ErrNotFound
	}
	exists, readable, err := entityAccess(ctx, tx, p, entityref.Ref{Type: "ci-run", ID: ciRun})
	if err != nil {
		return page, err
	}
	if !exists || !readable {
		return page, authz.ErrNotFound
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM radishnexus.ci_runs WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, ciRun).Scan(&status); err != nil {
		return page, fmt.Errorf("load target source: %w", err)
	}
	if status != "succeeded" {
		return page, authz.ErrConflict
	}
	// Environment read access is active Workspace membership (ADR-0011).
	// Authorization narrows this list to permitted writes, never grants read access.
	rows, err := tx.Query(ctx, `SELECT e.id,e.name,e.key FROM radishnexus.environments e
 WHERE e.workspace_id=$1 AND e.id>$3 AND e.status='active' AND e.classification='staging'
 AND EXISTS (SELECT 1 FROM radishnexus.environment_deployment_authorizations a WHERE a.workspace_id=e.workspace_id AND a.environment_id=e.id AND a.user_id=$2 AND a.status='active')
 ORDER BY e.id LIMIT $4`, p.WorkspaceID, p.ID, in.AfterID, in.Limit+1)
	if err != nil {
		return page, fmt.Errorf("query staging targets: %w", err)
	}
	page.Items = []goldenpath.StagingTarget{}
	for rows.Next() {
		v := goldenpath.StagingTarget{Ref: entityref.Ref{Type: "environment"}}
		if err = rows.Scan(&v.Ref.ID, &v.Name, &v.Key); err != nil {
			rows.Close()
			return page, fmt.Errorf("scan staging target: %w", err)
		}
		page.Items = append(page.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, fmt.Errorf("read staging targets: %w", err)
	}
	if len(page.Items) > in.Limit {
		page.Items = page.Items[:in.Limit]
		page.NextID = page.Items[len(page.Items)-1].Ref.ID
	}
	if err = tx.Commit(ctx); err != nil {
		return page, fmt.Errorf("commit staging targets: %w", err)
	}
	return page, nil
}
