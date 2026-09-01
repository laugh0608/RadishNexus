package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type channelAccess struct {
	projectID  string
	visibility string
	status     string
	project    projectAccess
}

type messageAccessFacts struct {
	channelID string
	threadID  *string
}

func readableChannel(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	channelID string,
) (channelAccess, error) {
	var access channelAccess
	err := tx.QueryRow(ctx, `
		SELECT governing_project_id, visibility, status
		FROM radishnexus.channels
		WHERE workspace_id = $1 AND id = $2
		FOR SHARE
	`, principal.WorkspaceID, channelID).Scan(
		&access.projectID,
		&access.visibility,
		&access.status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return channelAccess{}, authz.ErrNotFound
	}
	if err != nil {
		return channelAccess{}, fmt.Errorf("load Channel access facts: %w", err)
	}

	project, canReadProject, err := readProjectAccess(ctx, tx, principal, access.projectID)
	if err != nil {
		return channelAccess{}, err
	}
	if !canReadProject {
		return channelAccess{}, authz.ErrNotFound
	}
	access.project = project
	if access.visibility == "restricted" {
		var allowed bool
		err = tx.QueryRow(ctx, `
			SELECT true
			FROM radishnexus.channel_memberships
			WHERE workspace_id = $1 AND channel_id = $2 AND user_id = $3
			FOR SHARE
		`, principal.WorkspaceID, channelID, principal.ID).Scan(&allowed)
		if errors.Is(err, pgx.ErrNoRows) {
			return channelAccess{}, authz.ErrNotFound
		}
		if err != nil {
			return channelAccess{}, fmt.Errorf("load restricted Channel membership: %w", err)
		}
	}
	return access, nil
}

func readableMessage(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	messageID string,
) (messageAccessFacts, error) {
	var facts messageAccessFacts
	var threadID sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT channel_id, thread_id
		FROM radishnexus.messages
		WHERE workspace_id = $1 AND id = $2
		FOR SHARE
	`, principal.WorkspaceID, messageID).Scan(&facts.channelID, &threadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return messageAccessFacts{}, authz.ErrNotFound
	}
	if err != nil {
		return messageAccessFacts{}, fmt.Errorf("load Message access facts: %w", err)
	}
	if _, err := readableChannel(ctx, tx, principal, facts.channelID); err != nil {
		return messageAccessFacts{}, err
	}
	if threadID.Valid {
		facts.threadID = &threadID.String
		if err := requireReadableThreadInChannel(
			ctx,
			tx,
			principal,
			threadID.String,
			facts.channelID,
		); err != nil {
			return messageAccessFacts{}, err
		}
	}
	return facts, nil
}

func readProjectAccess(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	projectID string,
) (projectAccess, bool, error) {
	var visibility string
	var status string
	err := tx.QueryRow(ctx, `
		SELECT visibility, status
		FROM radishnexus.projects
		WHERE workspace_id = $1 AND id = $2
		FOR SHARE
	`, principal.WorkspaceID, projectID).Scan(&visibility, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectAccess{}, false, nil
	}
	if err != nil {
		return projectAccess{}, false, fmt.Errorf("load Project access facts: %w", err)
	}

	activeMember, err := activeWorkspaceMember(ctx, tx, principal)
	if err != nil {
		return projectAccess{}, false, err
	}
	if !activeMember {
		return projectAccess{active: status == "active"}, false, nil
	}

	var role authz.ProjectRole
	err = tx.QueryRow(ctx, `
		SELECT role
		FROM radishnexus.project_memberships
		WHERE workspace_id = $1 AND project_id = $2 AND user_id = $3
		FOR SHARE
	`, principal.WorkspaceID, projectID, principal.ID).Scan(&role)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return projectAccess{}, false, fmt.Errorf("load Project membership: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		role = authz.RoleNone
	}

	canRead := visibility == "workspace" || role != authz.RoleNone
	return projectAccess{role: role, active: status == "active"}, canRead, nil
}

func activeWorkspaceMember(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
) (bool, error) {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status
		FROM radishnexus.workspace_memberships
		WHERE workspace_id = $1 AND user_id = $2
		FOR SHARE
	`, principal.WorkspaceID, principal.ID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load Workspace membership: %w", err)
	}
	return status == "active", nil
}

func readableThread(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	threadID string,
) (string, projectAccess, error) {
	var projectID string
	var visibility string
	var originChannelID sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT governing_project_id, visibility, origin_channel_id
		FROM radishnexus.threads
		WHERE workspace_id = $1 AND id = $2
		FOR SHARE
	`, principal.WorkspaceID, threadID).Scan(&projectID, &visibility, &originChannelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", projectAccess{}, authz.ErrNotFound
	}
	if err != nil {
		return "", projectAccess{}, fmt.Errorf("load Thread access facts: %w", err)
	}

	access, canReadProject, err := readProjectAccess(ctx, tx, principal, projectID)
	if err != nil {
		return "", projectAccess{}, err
	}
	if !canReadProject {
		return "", projectAccess{}, authz.ErrNotFound
	}
	if originChannelID.Valid {
		channel, err := readableChannel(ctx, tx, principal, originChannelID.String)
		if err != nil {
			return "", projectAccess{}, err
		}
		if channel.projectID != projectID {
			return "", projectAccess{}, fmt.Errorf(
				"Thread %s origin Channel has a different governing Project",
				threadID,
			)
		}
	}
	if visibility == "restricted" {
		var allowed bool
		err = tx.QueryRow(ctx, `
			SELECT true
			FROM radishnexus.thread_memberships
			WHERE workspace_id = $1 AND thread_id = $2 AND user_id = $3
			FOR SHARE
		`, principal.WorkspaceID, threadID, principal.ID).Scan(&allowed)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", projectAccess{}, authz.ErrNotFound
		}
		if err != nil {
			return "", projectAccess{}, fmt.Errorf("load restricted Thread membership: %w", err)
		}
	}
	return projectID, access, nil
}

func requireReadableThreadInChannel(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	threadID string,
	channelID string,
) error {
	if _, _, err := readableThread(ctx, tx, principal, threadID); err != nil {
		return err
	}
	var originChannelID sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT origin_channel_id
		FROM radishnexus.threads
		WHERE workspace_id = $1 AND id = $2
		FOR SHARE
	`, principal.WorkspaceID, threadID).Scan(&originChannelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load Thread origin Channel: %w", err)
	}
	if !originChannelID.Valid || originChannelID.String != channelID {
		return authz.ErrNotFound
	}
	return nil
}

func requireReadableDecisionEvidence(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	decisionID string,
) error {
	rows, err := tx.Query(ctx, `
		SELECT to_id
		FROM radishnexus.entity_links
		WHERE workspace_id = $1
		  AND from_type = 'decision'
		  AND from_id = $2
		  AND relation_type = 'derived-from'
		  AND to_type = 'thread'
		  AND state = 'active'
		ORDER BY id
	`, principal.WorkspaceID, decisionID)
	if err != nil {
		return fmt.Errorf("load Decision evidence access facts: %w", err)
	}
	var threadIDs []string
	for rows.Next() {
		var threadID string
		if err := rows.Scan(&threadID); err != nil {
			rows.Close()
			return fmt.Errorf("scan Decision evidence access facts: %w", err)
		}
		threadIDs = append(threadIDs, threadID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate Decision evidence access facts: %w", err)
	}
	rows.Close()
	if len(threadIDs) == 0 {
		return fmt.Errorf("%w: Decision has no readable evidence", authz.ErrConflict)
	}

	for _, threadID := range threadIDs {
		if _, _, err := readableThread(ctx, tx, principal, threadID); err != nil {
			if errors.Is(err, authz.ErrNotFound) {
				return fmt.Errorf("%w: Decision evidence is not readable", authz.ErrForbidden)
			}
			return err
		}
	}
	return nil
}
