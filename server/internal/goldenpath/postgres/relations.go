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

type relationFact struct {
	relationType string
	target       entityref.Ref
}

func (store *Store) ListRelations(
	ctx context.Context,
	principal authz.Principal,
	source entityref.Ref,
) (projections []goldenpath.RelationProjection, err error) {
	if err := principal.ValidateUser(); err != nil {
		return nil, err
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin relation projection transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	exists, canRead, err := entityAccess(ctx, tx, principal, source)
	if err != nil {
		return nil, err
	}
	if !exists || !canRead {
		return nil, authz.ErrNotFound
	}

	projections, err = listRelationProjections(ctx, tx, principal, source)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit relation projection transaction: %w", err)
	}
	return projections, nil
}

func listRelationProjections(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	source entityref.Ref,
) ([]goldenpath.RelationProjection, error) {
	rows, err := tx.Query(ctx, `
		SELECT relation_type, to_type, to_id
		FROM radishnexus.entity_links
		WHERE workspace_id = $1 AND from_type = $2 AND from_id = $3 AND state = 'active'
		ORDER BY created_at, id
	`, principal.WorkspaceID, source.Type, source.ID)
	if err != nil {
		return nil, fmt.Errorf("list relation facts: %w", err)
	}
	var facts []relationFact
	for rows.Next() {
		var fact relationFact
		if err := rows.Scan(&fact.relationType, &fact.target.Type, &fact.target.ID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan relation fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate relation facts: %w", err)
	}
	rows.Close()

	projections := make([]goldenpath.RelationProjection, 0, len(facts))
	for _, fact := range facts {
		targetExists, targetReadable, err := entityAccess(ctx, tx, principal, fact.target)
		if err != nil {
			return nil, err
		}
		if !targetExists {
			continue
		}
		if !targetReadable {
			projections = append(projections, goldenpath.RelationProjection{
				State: goldenpath.ProjectionRestricted,
			})
			continue
		}

		title, err := entityTitle(ctx, tx, principal.WorkspaceID, fact.target)
		if err != nil {
			return nil, err
		}
		projections = append(projections, goldenpath.RelationProjection{
			State:        goldenpath.ProjectionVisible,
			RelationType: fact.relationType,
			Target:       fact.target,
			Title:        title,
		})
	}
	return projections, nil
}

func entityAccess(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	ref entityref.Ref,
) (exists bool, canRead bool, err error) {
	switch ref.Type {
	case "thread":
		_, _, err := readableThread(ctx, tx, principal, ref.ID)
		if errors.Is(err, authz.ErrNotFound) {
			var targetExists bool
			lookupErr := tx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM radishnexus.threads
					WHERE workspace_id = $1 AND id = $2
				)
			`, principal.WorkspaceID, ref.ID).Scan(&targetExists)
			if lookupErr != nil {
				return false, false, fmt.Errorf("check restricted Thread existence: %w", lookupErr)
			}
			return targetExists, false, nil
		}
		if err != nil {
			return false, false, err
		}
		return true, true, nil
	case "decision", "ticket":
		var projectID string
		table := "radishnexus.decisions"
		if ref.Type == "ticket" {
			table = "radishnexus.tickets"
		}
		query := "SELECT governing_project_id FROM " + table + " WHERE workspace_id = $1 AND id = $2 FOR SHARE"
		err := tx.QueryRow(ctx, query, principal.WorkspaceID, ref.ID).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		if err != nil {
			return false, false, fmt.Errorf("load %s access facts: %w", ref.Type, err)
		}
		_, readable, err := readProjectAccess(ctx, tx, principal, projectID)
		return true, readable, err
	case "project":
		_, readable, err := readProjectAccess(ctx, tx, principal, ref.ID)
		if err != nil {
			return false, false, err
		}
		var exists bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM radishnexus.projects
				WHERE workspace_id = $1 AND id = $2
			)
		`, principal.WorkspaceID, ref.ID).Scan(&exists)
		return exists, readable, err
	default:
		return false, false, nil
	}
}

func entityTitle(ctx context.Context, tx pgx.Tx, workspaceID string, ref entityref.Ref) (string, error) {
	var query string
	switch ref.Type {
	case "thread", "ticket":
		query = "SELECT title FROM radishnexus." + ref.Type + "s WHERE workspace_id = $1 AND id = $2"
	case "decision":
		query = "SELECT question FROM radishnexus.decisions WHERE workspace_id = $1 AND id = $2"
	case "project":
		query = "SELECT name FROM radishnexus.projects WHERE workspace_id = $1 AND id = $2"
	default:
		return "", fmt.Errorf("unsupported visible relation target type %q", ref.Type)
	}

	var title string
	if err := tx.QueryRow(ctx, query, workspaceID, ref.ID).Scan(&title); err != nil {
		return "", fmt.Errorf("load visible %s title: %w", ref.Type, err)
	}
	return title, nil
}
