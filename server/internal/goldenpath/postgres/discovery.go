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
	tx, err := store.beginAdmission(ctx, principal, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
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
	tx, err := store.beginAdmission(ctx, principal, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
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

func (store *Store) ListProjectTickets(ctx context.Context, principal authz.Principal, projectID string, input goldenpath.DiscoveryPageInput) (page goldenpath.DiscoveryPage, err error) {
	if err := principal.ValidateUser(); err != nil {
		return page, err
	}
	tx, err := store.beginAdmission(ctx, principal, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, fmt.Errorf("begin Ticket discovery: %w", err)
	}
	defer rollback(ctx, tx, &err)
	// Ticket access is its governing Project access, independent of evidence
	// Thread permissions. Keep this equivalent to entityAccess's Ticket branch.
	access, readable, err := readProjectAccess(ctx, tx, principal, projectID)
	if err != nil {
		return page, err
	}
	if !readable {
		return page, authz.ErrNotFound
	}
	rows, err := tx.Query(ctx, `
		SELECT t.id,t.title,t.status,t.priority, CASE WHEN t.assignee_user_id IS NULL THEN NULL ELSE jsonb_build_object('id',u.id,'display_name',u.display_name,'eligible',COALESCE(`+assigneeEligibility+`,false) AND $5,'imported',EXISTS(SELECT 1 FROM radishnexus.context_import_mappings im WHERE im.kind='actor' AND im.target_id=u.id)) END
        FROM radishnexus.tickets t
        LEFT JOIN radishnexus.users u ON u.id=t.assignee_user_id
        LEFT JOIN radishnexus.workspace_memberships m ON m.workspace_id=t.workspace_id AND m.user_id=u.id
        LEFT JOIN radishnexus.user_accounts a ON a.user_id=u.id
        LEFT JOIN radishnexus.project_memberships pm ON pm.workspace_id=t.workspace_id AND pm.project_id=t.governing_project_id AND pm.user_id=u.id
        WHERE t.workspace_id=$1 AND t.governing_project_id=$2 AND t.id>$3
 AND ($6='' OR ($6='active' AND t.status IN ('open','in-progress')) OR t.status=$6)
 AND ($7='' OR ($7='me' AND t.assignee_user_id=$8) OR ($7='unassigned' AND t.assignee_user_id IS NULL))
 AND ($9='' OR t.priority=$9)
 ORDER BY t.id LIMIT $4
	`, principal.WorkspaceID, projectID, input.AfterID, input.Limit+1, access.active, input.TicketFilters.Status, input.TicketFilters.Assignee, principal.ID, input.TicketFilters.Priority)
	if err != nil {
		return page, fmt.Errorf("query readable Tickets: %w", err)
	}
	page, err = scanDiscoveryPage(rows, "ticket", input.Limit)
	if err != nil {
		return page, err
	}
	if err := tx.Commit(ctx); err != nil {
		return page, fmt.Errorf("commit Ticket discovery: %w", err)
	}
	return page, nil
}

func (store *Store) ListProjectDecisions(ctx context.Context, principal authz.Principal, projectID string, input goldenpath.DiscoveryPageInput) (page goldenpath.DiscoveryPage, err error) {
	if err := principal.ValidateUser(); err != nil {
		return page, err
	}
	tx, err := store.beginAdmission(ctx, principal, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, fmt.Errorf("begin Decision discovery: %w", err)
	}
	defer rollback(ctx, tx, &err)
	// Decision readability follows its governing Project, independently of its
	// evidence Thread or origin Channel. Authorize and read in one snapshot.
	_, readable, err := readProjectAccess(ctx, tx, principal, projectID)
	if err != nil {
		return page, err
	}
	if !readable {
		return page, authz.ErrNotFound
	}
	rows, err := tx.Query(ctx, `
		SELECT id, question, status FROM radishnexus.decisions
		WHERE workspace_id = $1 AND governing_project_id = $2 AND id > $3
		ORDER BY id LIMIT $4
	`, principal.WorkspaceID, projectID, input.AfterID, input.Limit+1)
	if err != nil {
		return page, fmt.Errorf("query readable Decisions: %w", err)
	}
	page, err = scanDiscoveryPage(rows, "decision", input.Limit)
	if err != nil {
		return page, err
	}
	if err := tx.Commit(ctx); err != nil {
		return page, fmt.Errorf("commit Decision discovery: %w", err)
	}
	return page, nil
}

func scanDiscoveryPage(rows pgx.Rows, kind string, limit int) (goldenpath.DiscoveryPage, error) {
	defer rows.Close()
	page := goldenpath.DiscoveryPage{Items: make([]goldenpath.DiscoveryItem, 0, limit+1)}
	for rows.Next() {
		item := goldenpath.DiscoveryItem{}
		item.Ref.Type = kind
		targets := []any{&item.Ref.ID, &item.Title, &item.Status}
		if kind == "ticket" {
			targets = append(targets, &item.Priority, &item.Assignee)
		}
		if err := rows.Scan(targets...); err != nil {
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
