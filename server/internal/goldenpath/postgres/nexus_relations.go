package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const nexusThreadRelations = `
	SELECT link.id,link.created_at,'decision'::text AS target_type,target.id AS target_id,target.question AS title
	FROM radishnexus.entity_links link
	JOIN radishnexus.decisions target ON (target.workspace_id,target.id)=(link.workspace_id,link.from_id)
	JOIN radishnexus.projects project ON (project.workspace_id,project.id)=(target.workspace_id,target.governing_project_id)
	WHERE link.workspace_id=$1 AND link.to_id=$3 AND link.state='active'
	  AND link.to_type='thread' AND link.from_type='decision' AND link.relation_type='derived-from'
	  AND ` + readableProjectPredicate

const nexusDecisionRelations = `
	SELECT link.id,link.created_at,'ticket'::text AS target_type,target.id AS target_id,target.title
	FROM radishnexus.entity_links link
	JOIN radishnexus.tickets target ON (target.workspace_id,target.id)=(link.workspace_id,link.from_id)
	JOIN radishnexus.projects project ON (project.workspace_id,project.id)=(target.workspace_id,target.governing_project_id)
	WHERE link.workspace_id=$1 AND link.to_id=$3 AND link.state='active'
	  AND link.to_type='decision' AND link.from_type='ticket' AND link.relation_type='implements'
	  AND ` + readableProjectPredicate

const nexusTicketRelations = `
	SELECT link.id,link.created_at,'document'::text AS target_type,target.id AS target_id,revision.title
	FROM radishnexus.entity_links link
	JOIN radishnexus.documents target ON (target.workspace_id,target.id)=(link.workspace_id,link.to_id)
	JOIN radishnexus.document_revisions revision
	  ON (revision.workspace_id,revision.document_id,revision.revision)=(target.workspace_id,target.id,target.current_revision)
	JOIN radishnexus.projects project ON (project.workspace_id,project.id)=(target.workspace_id,target.governing_project_id)
	WHERE link.workspace_id=$1 AND link.from_id=$3 AND link.state='active'
	  AND link.from_type='ticket' AND link.to_type='document' AND link.relation_type='relates-to'
	  AND ` + readableProjectPredicate + `
	UNION ALL
	SELECT link.id,link.created_at,'component'::text,target.id,target.name
	FROM radishnexus.entity_links link
	JOIN radishnexus.components target ON (target.workspace_id,target.id)=(link.workspace_id,link.to_id)
	WHERE link.workspace_id=$1 AND link.from_id=$3 AND link.state='active'
	  AND link.from_type='ticket' AND link.to_type='component' AND link.relation_type='affects'`

func (s *Store) ListNexusRelations(ctx context.Context, p authz.Principal, source entityref.Ref, input goldenpath.NexusRelationPageInput) (page goldenpath.NexusRelationPage, err error) {
	var query string
	switch source.Type {
	case "thread":
		query = nexusThreadRelations
	case "decision":
		query = nexusDecisionRelations
	case "ticket":
		query = nexusTicketRelations
	default:
		return page, authz.ErrInvalid
	}
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer rollback(ctx, tx, &err)
	_, readable, err := entityAccess(ctx, tx, p, source)
	if err != nil {
		return page, err
	}
	if !readable {
		return page, authz.ErrNotFound
	}

	page, err = readNexusRelationPage(ctx, tx, p, source, query, input)
	if err != nil {
		return page, err
	}
	return page, tx.Commit(ctx)
}

func readNexusRelationPage(ctx context.Context, tx pgx.Tx, p authz.Principal, source entityref.Ref, query string, input goldenpath.NexusRelationPageInput) (page goldenpath.NexusRelationPage, err error) {
	args := []any{p.WorkspaceID, p.ID, source.ID}
	query = `SELECT id,created_at,target_type,target_id,title FROM (` + query + `) visible`
	var afterTime time.Time
	var afterID string
	if boundary := input.After; boundary != nil {
		err = tx.QueryRow(ctx, query+` WHERE target_type=$4 AND target_id=$5 ORDER BY created_at,id LIMIT 1`,
			p.WorkspaceID, p.ID, source.ID, boundary.Target.Type, boundary.Target.ID).
			Scan(&afterID, &afterTime, nil, nil, nil)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && nexusRelationGeneration(p, source, afterID) != boundary.Generation) {
			return page, authz.ErrConflict
		}
		if err != nil {
			return page, fmt.Errorf("resolve readable Nexus relation boundary: %w", err)
		}
		query += ` WHERE (created_at,id)>($5,$6)`
	}
	args = append(args, input.Limit+1)
	if input.After != nil {
		args = append(args, afterTime, afterID)
	}
	rows, err := tx.Query(ctx, query+` ORDER BY created_at,id LIMIT $4`, args...)
	if err != nil {
		return page, fmt.Errorf("list Nexus relation page: %w", err)
	}
	defer rows.Close()
	page.Items = make([]goldenpath.RelationProjection, 0, input.Limit)
	var lastID string
	for rows.Next() {
		if len(page.Items) == input.Limit {
			last := page.Items[len(page.Items)-1]
			page.Next = &goldenpath.NexusRelationBoundary{Target: last.Target, Generation: nexusRelationGeneration(p, source, lastID)}
			rows.Close()
			break
		}
		var row goldenpath.RelationProjection
		if err := rows.Scan(&lastID, nil, &row.Target.Type, &row.Target.ID, &row.Title); err != nil {
			return page, fmt.Errorf("scan Nexus relation page: %w", err)
		}
		row.State = goldenpath.ProjectionVisible
		row.Direction, row.RelationType = goldenpath.NexusRelationDirection(source.Type, row.Target.Type)
		page.Items = append(page.Items, row)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("iterate Nexus relation page: %w", err)
	}
	return page, nil
}

func nexusRelationGeneration(p authz.Principal, source entityref.Ref, linkID string) string {
	return cp.Hash(cp.Encode([]string{"nexus-relations-v1", p.WorkspaceID, p.ID, source.Type, source.ID, linkID}))
}
