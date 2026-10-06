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

func repositoryConfigurationObject(ctx context.Context, tx pgx.Tx, p authz.Principal, id string) (o goldenpath.ConfigurationObject, err error) {
	exists, readable, err := entityAccess(ctx, tx, p, entityref.Ref{Type: "repository", ID: id})
	if err != nil {
		return o, err
	}
	if !exists || !readable {
		return o, authz.ErrNotFound
	}
	o.ID, o.Kind = id, "repository"
	m := &goldenpath.RepositoryMetadata{}
	err = tx.QueryRow(ctx, `SELECT name,provider,provider_origin,external_id,web_url,default_branch FROM radishnexus.repositories WHERE workspace_id=$1 AND id=$2 FOR SHARE`, p.WorkspaceID, id).Scan(&o.Name, &m.Provider, &m.ProviderOrigin, &m.ExternalID, &m.WebURL, &m.DefaultBranch)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, authz.ErrNotFound
	}
	if err != nil {
		return o, fmt.Errorf("read Repository configuration: %w", err)
	}
	o.Repository = m
	return o, nil
}

// Called after the account and membership locks. State conflicts are deliberately
// checked after receipt resolution, so historical exact retries cannot mutate links.
func lockRepositoryConfiguration(ctx context.Context, tx pgx.Tx, c goldenpath.ConfigurationCommand, owner bool) (repository, state string, err error) {
	if c.Kind == "repository.create" && !owner {
		return "", "", authz.ErrForbidden
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM radishnexus.workspaces WHERE id=$1 FOR UPDATE`, c.Principal.WorkspaceID); err != nil {
		return "", "", fmt.Errorf("lock Repository configuration workspace: %w", err)
	}
	if c.Kind == "repository.create" {
		return "", "", nil
	}
	exists, readable, err := entityAccess(ctx, tx, c.Principal, entityref.Ref{Type: "component", ID: c.ScopeID})
	if err != nil {
		return "", "", err
	}
	if !exists || !readable {
		return "", "", authz.ErrNotFound
	}
	component, err := deliveryConfigurationObject(ctx, tx, c.Principal.WorkspaceID, "component", c.ScopeID, owner, true)
	if err != nil {
		return "", "", err
	}
	repository = c.Repository.RepositoryID
	if c.Kind == "component.repository.unlink" {
		// Read only immutable endpoint identity before acquiring its locks.
		err = tx.QueryRow(ctx, `SELECT to_id FROM radishnexus.entity_links WHERE workspace_id=$1 AND id=$2 AND from_type='component' AND from_id=$3 AND to_type='repository' AND relation_type='source-repository'`, c.Principal.WorkspaceID, c.Repository.LinkID, c.ScopeID).Scan(&repository)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", authz.ErrNotFound
		}
		if err != nil {
			return "", "", fmt.Errorf("locate Repository link endpoint: %w", err)
		}
	}
	if _, err = repositoryConfigurationObject(ctx, tx, c.Principal, repository); err != nil {
		return "", "", err
	}
	if !owner {
		return "", "", authz.ErrForbidden
	}
	if c.Kind == "component.repository.link" {
		if component.Status != "active" {
			return "", "", authz.ErrConflict
		}
		return repository, "", nil
	}
	err = tx.QueryRow(ctx, `SELECT state FROM radishnexus.entity_links WHERE workspace_id=$1 AND id=$2 AND from_id=$3 AND to_id=$4 AND from_type='component' AND to_type='repository' AND relation_type='source-repository' FOR UPDATE`, c.Principal.WorkspaceID, c.Repository.LinkID, c.ScopeID, repository).Scan(&state)
	if err != nil {
		return "", "", fmt.Errorf("lock Repository link: %w", err)
	}
	return repository, state, nil
}

func createRepositoryConfiguration(ctx context.Context, tx pgx.Tx, c goldenpath.ConfigurationCommand) (goldenpath.ConfigurationObject, error) {
	m := c.Repository.RepositoryMetadata
	_, err := tx.Exec(ctx, `INSERT INTO radishnexus.repositories(id,workspace_id,name,provider,provider_origin,external_id,web_url,default_branch,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'user',$9,$10,$10)`, c.ID, c.Principal.WorkspaceID, c.Name, m.Provider, m.ProviderOrigin, m.ExternalID, m.WebURL, m.DefaultBranch, c.Principal.ID, c.OccurredAt)
	if err != nil {
		return goldenpath.ConfigurationObject{}, mapDatabaseError("create Repository mapping", err)
	}
	return repositoryConfigurationObject(ctx, tx, c.Principal, c.ID)
}

func changeRepositoryLink(ctx context.Context, tx pgx.Tx, c goldenpath.ConfigurationCommand, repository, state string) (id, before, after string, err error) {
	if c.Kind == "component.repository.link" {
		id, after = c.ID, "active"
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,'component',$3,'source-repository','repository',$4,'asserted','user','user',$5,$6,$6)`, id, c.Principal.WorkspaceID, c.ScopeID, repository, c.Principal.ID, c.OccurredAt)
	} else {
		if state != "active" {
			return "", "", "", authz.ErrConflict
		}
		id, before, after = c.Repository.LinkID, "active", "removed"
		_, err = tx.Exec(ctx, `UPDATE radishnexus.entity_links SET state='removed',removed_by_kind='user',removed_by_id=$1,removed_at=$2,updated_at=$2,removal_reason='owner-unlinked' WHERE workspace_id=$3 AND id=$4`, c.Principal.ID, c.OccurredAt, c.Principal.WorkspaceID, id)
	}
	if err != nil {
		err = mapDatabaseError("change Repository link", err)
	}
	return
}

func listRepositoryConfiguration(ctx context.Context, tx pgx.Tx, p authz.Principal, q goldenpath.ConfigurationQuery, owner bool) (page goldenpath.ConfigurationPage, err error) {
	page.Objects, page.Links = []goldenpath.ConfigurationObject{}, []goldenpath.RepositoryLink{}
	targetKind := "repository"
	query := `SELECT id,''::text FROM radishnexus.repositories WHERE workspace_id=$1 AND id>$2 AND workspace_id=$4 ORDER BY id LIMIT $3`
	if q.Kind != "repositories" {
		sourceKind := "component"
		query = `SELECT to_id,id FROM radishnexus.entity_links WHERE workspace_id=$1 AND to_id>$2 AND from_id=$4 AND from_type='component' AND to_type='repository' AND relation_type='source-repository' AND state='active' ORDER BY to_id LIMIT $3`
		if q.Kind == "repository-components" {
			sourceKind, targetKind = "repository", "component"
			query = `SELECT from_id,id FROM radishnexus.entity_links WHERE workspace_id=$1 AND from_id>$2 AND to_id=$4 AND from_type='component' AND to_type='repository' AND relation_type='source-repository' AND state='active' ORDER BY from_id LIMIT $3`
		}
		exists, readable, e := entityAccess(ctx, tx, p, entityref.Ref{Type: sourceKind, ID: q.ScopeID})
		if e != nil {
			return page, e
		}
		if !exists || !readable {
			return page, authz.ErrNotFound
		}
	}
	// Scan bounded batches until we have a page of authorized targets. Filtering
	// never exposes an unreadable ID through a cursor, count or placeholder.
	after := q.AfterID
	for len(page.Objects)+len(page.Links) <= q.Limit {
		rows, e := tx.Query(ctx, query, p.WorkspaceID, after, q.Limit+1, q.ScopeID)
		if e != nil {
			return page, fmt.Errorf("list Repository configuration: %w", e)
		}
		type fact struct{ target, link string }
		facts := []fact{}
		for rows.Next() {
			var f fact
			if e = rows.Scan(&f.target, &f.link); e != nil {
				rows.Close()
				return page, fmt.Errorf("scan Repository configuration: %w", e)
			}
			facts = append(facts, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return page, fmt.Errorf("iterate Repository configuration: %w", e)
		}
		for _, f := range facts {
			after = f.target
			exists, readable, e := entityAccess(ctx, tx, p, entityref.Ref{Type: targetKind, ID: f.target})
			if e != nil {
				return page, e
			}
			if !exists || !readable {
				continue
			}
			var object goldenpath.ConfigurationObject
			if targetKind == "repository" {
				object, e = repositoryConfigurationObject(ctx, tx, p, f.target)
			} else {
				object, e = deliveryConfigurationObject(ctx, tx, p.WorkspaceID, targetKind, f.target, owner, false)
			}
			if e != nil {
				return page, e
			}
			if q.Kind == "repositories" {
				page.Objects = append(page.Objects, object)
			} else {
				page.Links = append(page.Links, goldenpath.RepositoryLink{ID: f.link, Target: object, CanUnlink: owner})
			}
			if len(page.Objects)+len(page.Links) > q.Limit {
				if q.Kind == "repositories" {
					page.Objects = page.Objects[:q.Limit]
					page.NextID = page.Objects[q.Limit-1].ID
				} else {
					page.Links = page.Links[:q.Limit]
					page.NextID = page.Links[q.Limit-1].Target.ID
				}
				return page, nil
			}
		}
		if len(facts) < q.Limit+1 {
			return page, nil
		}
	}
	return page, nil
}
