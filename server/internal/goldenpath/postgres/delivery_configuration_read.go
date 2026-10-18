package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func listDeliveryConfiguration(ctx context.Context, tx pgx.Tx, p authz.Principal, q goldenpath.ConfigurationQuery, owner bool) (page goldenpath.ConfigurationPage, err error) {
	if q.Kind == "components" || q.Kind == "environments" {
		kind := "component"
		query := `SELECT id,key,name,type,lifecycle,COALESCE(owner_team_id,'') FROM radishnexus.components WHERE workspace_id=$1 AND id>$2 ORDER BY id LIMIT $3`
		if q.Kind == "environments" {
			kind = "environment"
			query = `SELECT id,key,name,classification,status,owner_team_id FROM radishnexus.environments WHERE workspace_id=$1 AND id>$2 ORDER BY id LIMIT $3`
		}
		rows, e := tx.Query(ctx, query, p.WorkspaceID, q.AfterID, q.Limit+1)
		if e != nil {
			return page, fmt.Errorf("list delivery objects: %w", e)
		}
		defer rows.Close()
		page.Objects = []goldenpath.ConfigurationObject{}
		for rows.Next() {
			o := goldenpath.ConfigurationObject{Kind: kind}
			var category string
			if e = rows.Scan(&o.ID, &o.Key, &o.Name, &category, &o.Status, &o.OwnerTeamID); e != nil {
				return page, fmt.Errorf("scan delivery object: %w", e)
			}
			if kind == "component" {
				o.Type = category
			} else {
				o.Classification = category
			}
			page.Objects = append(page.Objects, o)
		}
		if err = rows.Err(); err != nil {
			return page, fmt.Errorf("iterate delivery objects: %w", err)
		}
		if len(page.Objects) > q.Limit {
			page.Objects = page.Objects[:q.Limit]
			page.NextID = page.Objects[q.Limit-1].ID
		}
		return page, nil
	}
	if _, err = deliveryConfigurationObject(ctx, tx, p.WorkspaceID, "environment", q.ScopeID, owner, false); err != nil {
		return page, err
	}
	if !owner {
		return page, authz.ErrForbidden
	}
	// One latest generation per subject. Inactive subjects remain visible for
	// cleanup; the ordinary member selector continues to expose only active ones.
	query := `SELECT u.id,u.display_name,(w.status='active' AND COALESCE(a.status='active',false)),g.id,g.status
	FROM radishnexus.workspace_memberships w JOIN radishnexus.users u ON u.id=w.user_id
	LEFT JOIN radishnexus.user_accounts a ON a.user_id=w.user_id
	LEFT JOIN LATERAL (SELECT id,status FROM radishnexus.environment_deployment_authorizations
	 WHERE workspace_id=w.workspace_id AND environment_id=$2 AND user_id=w.user_id ORDER BY generation DESC LIMIT 1) g ON true
	WHERE w.workspace_id=$1 AND u.id>$3 AND g.id IS NOT NULL ORDER BY u.id LIMIT $4`
	if q.Kind == "environment-authorization" {
		query = `SELECT u.id,u.display_name,(w.status='active' AND COALESCE(a.status='active',false)),g.id,g.status
		FROM radishnexus.workspace_memberships w JOIN radishnexus.users u ON u.id=w.user_id
		LEFT JOIN radishnexus.user_accounts a ON a.user_id=w.user_id
		LEFT JOIN LATERAL (SELECT id,status FROM radishnexus.environment_deployment_authorizations
		 WHERE workspace_id=w.workspace_id AND environment_id=$2 AND user_id=w.user_id ORDER BY generation DESC LIMIT 1) g ON true
		WHERE w.workspace_id=$1 AND u.id=$3 AND (g.id IS NOT NULL OR (w.status='active' AND a.status='active')) LIMIT $4`
	}
	after := q.AfterID
	if q.Kind == "environment-authorization" {
		after = q.UserID
	}
	rows, e := tx.Query(ctx, query, p.WorkspaceID, q.ScopeID, after, q.Limit+1)
	if e != nil {
		return page, fmt.Errorf("list environment authorizations: %w", e)
	}
	defer rows.Close()
	page.Members = []goldenpath.ConfigurationMember{}
	for rows.Next() {
		var m goldenpath.ConfigurationMember
		var id, status *string
		if e = rows.Scan(&m.ID, &m.Name, &m.Eligible, &id, &status); e != nil {
			return page, fmt.Errorf("scan environment authorization: %w", e)
		}
		if id != nil && status != nil {
			m.Authorization = &goldenpath.ConfigurationAuthorization{ID: *id, Status: *status}
		}
		page.Members = append(page.Members, m)
	}
	if err = rows.Err(); err != nil {
		return page, fmt.Errorf("iterate environment authorizations: %w", err)
	}
	if q.Kind == "environment-authorization" && len(page.Members) == 0 {
		return page, authz.ErrNotFound
	}
	if len(page.Members) > q.Limit {
		page.Members = page.Members[:q.Limit]
		page.NextID = page.Members[q.Limit-1].ID
	}
	return page, nil
}
