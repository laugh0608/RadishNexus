package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (store *Store) ListProjects(ctx context.Context, principal authz.Principal, input goldenpath.DiscoveryPageInput) (page goldenpath.DiscoveryPage, err error) {
	if err := principal.ValidateUser(); err != nil {
		return page, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, fmt.Errorf("begin Project discovery: %w", err)
	}
	defer rollback(ctx, tx, &err)
	active, err := activeWorkspaceMember(ctx, tx, principal)
	if err != nil {
		return page, err
	}
	if !active {
		return page, authz.ErrNotFound
	}
	// Match readProjectAccess: Workspace visibility or an explicit Project
	// membership, always inside the active Workspace membership boundary.
	rows, err := tx.Query(ctx, `
		SELECT project.id, project.name, project.status
		FROM radishnexus.projects AS project
		WHERE project.workspace_id = $1 AND project.id > $3
		  AND (project.visibility = 'workspace' OR EXISTS (
			SELECT 1 FROM radishnexus.project_memberships AS membership
			WHERE membership.workspace_id = project.workspace_id
			  AND membership.project_id = project.id AND membership.user_id = $2
		  ))
		ORDER BY project.id LIMIT $4
	`, principal.WorkspaceID, principal.ID, input.AfterID, input.Limit+1)
	if err != nil {
		return page, fmt.Errorf("query readable Projects: %w", err)
	}
	page, err = scanDiscoveryPage(rows, "project", input.Limit)
	if err != nil {
		return page, err
	}
	if err := tx.Commit(ctx); err != nil {
		return page, fmt.Errorf("commit Project discovery: %w", err)
	}
	return page, nil
}

func (store *Store) ListProjectChannels(ctx context.Context, principal authz.Principal, projectID string, input goldenpath.DiscoveryPageInput) (page goldenpath.DiscoveryPage, err error) {
	if err := principal.ValidateUser(); err != nil {
		return page, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, fmt.Errorf("begin Channel discovery: %w", err)
	}
	defer rollback(ctx, tx, &err)
	_, readable, err := readProjectAccess(ctx, tx, principal, projectID)
	if err != nil {
		return page, err
	}
	if !readable {
		return page, authz.ErrNotFound
	}
	// Match readableChannel's narrower membership rule. No role overrides it.
	rows, err := tx.Query(ctx, `
		SELECT channel.id, channel.name, channel.status
		FROM radishnexus.channels AS channel
		WHERE channel.workspace_id = $1 AND channel.governing_project_id = $2
		  AND channel.id > $4
		  AND (channel.visibility = 'project' OR EXISTS (
			SELECT 1 FROM radishnexus.channel_memberships AS membership
			WHERE membership.workspace_id = channel.workspace_id
			  AND membership.channel_id = channel.id AND membership.user_id = $3
		  ))
		ORDER BY channel.id LIMIT $5
	`, principal.WorkspaceID, projectID, principal.ID, input.AfterID, input.Limit+1)
	if err != nil {
		return page, fmt.Errorf("query readable Channels: %w", err)
	}
	page, err = scanDiscoveryPage(rows, "channel", input.Limit)
	if err != nil {
		return page, err
	}
	if err := tx.Commit(ctx); err != nil {
		return page, fmt.Errorf("commit Channel discovery: %w", err)
	}
	return page, nil
}

func scanDiscoveryPage(rows pgx.Rows, kind string, limit int) (goldenpath.DiscoveryPage, error) {
	defer rows.Close()
	page := goldenpath.DiscoveryPage{Items: make([]goldenpath.DiscoveryItem, 0, limit+1)}
	for rows.Next() {
		item := goldenpath.DiscoveryItem{}
		item.Ref.Type = kind
		if err := rows.Scan(&item.Ref.ID, &item.Title, &item.Status); err != nil {
			return goldenpath.DiscoveryPage{}, fmt.Errorf("scan discovery item: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return goldenpath.DiscoveryPage{}, fmt.Errorf("iterate discovery page: %w", err)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextID = page.Items[limit-1].Ref.ID
	}
	return page, nil
}
