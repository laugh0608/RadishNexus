package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (s *Store) ReadConfiguration(ctx context.Context, p authz.Principal, kind, id string) (o goldenpath.ConfigurationObject, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return o, err
	}
	defer rollback(ctx, tx, &err)
	if _, err = configurationActor(ctx, tx, p); err != nil {
		return o, err
	}
	if kind == "project" {
		o, err = configurationProject(ctx, tx, p, id, false)
	} else {
		o, err = configurationChannel(ctx, tx, p, id, false)
	}
	if err != nil {
		return o, err
	}
	err = tx.Commit(ctx)
	return
}
func (s *Store) ListConfiguration(ctx context.Context, p authz.Principal, q goldenpath.ConfigurationQuery) (page goldenpath.ConfigurationPage, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer rollback(ctx, tx, &err)
	owner, err := configurationActor(ctx, tx, p)
	if err != nil {
		return page, err
	}
	switch q.Kind {
	case "teams":
		if !owner {
			return page, authz.ErrForbidden
		}
	case "members":
		if !owner {
			var admin bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.project_memberships m JOIN radishnexus.projects p ON p.workspace_id=m.workspace_id AND p.id=m.project_id WHERE m.workspace_id=$1 AND m.user_id=$2 AND m.role='admin' AND p.status='active')`, p.WorkspaceID, p.ID).Scan(&admin)
			if err != nil {
				return page, err
			}
			if !admin {
				return page, authz.ErrForbidden
			}
		}
	case "project-members":
		var o goldenpath.ConfigurationObject
		o, err = configurationProject(ctx, tx, p, q.ScopeID, false)
		if err != nil {
			return page, err
		}
		if !o.CanManage {
			return page, authz.ErrForbidden
		}
	case "channel-members":
		var o goldenpath.ConfigurationObject
		o, err = configurationChannel(ctx, tx, p, q.ScopeID, false)
		if err != nil {
			return page, err
		}
		if !o.CanManage {
			return page, authz.ErrForbidden
		}
	}
	page.Teams = []goldenpath.ConfigurationObject{}
	page.Members = []goldenpath.ConfigurationMember{}
	if q.Kind == "teams" {
		rows, e := tx.Query(ctx, `SELECT id,name FROM radishnexus.teams WHERE workspace_id=$1 AND id>$2 ORDER BY id LIMIT $3`, p.WorkspaceID, q.AfterID, q.Limit+1)
		if e != nil {
			return page, e
		}
		for rows.Next() {
			o := goldenpath.ConfigurationObject{Kind: "team"}
			if err = rows.Scan(&o.ID, &o.Name); err != nil {
				rows.Close()
				return page, err
			}
			page.Teams = append(page.Teams, o)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, err
		}
		if len(page.Teams) > q.Limit {
			page.Teams = page.Teams[:q.Limit]
			page.NextID = page.Teams[q.Limit-1].ID
		}
	} else {
		query := `SELECT u.id,u.display_name,'' AS role,true AS eligible FROM radishnexus.users u JOIN radishnexus.workspace_memberships w ON w.user_id=u.id JOIN radishnexus.user_accounts a ON a.user_id=u.id WHERE w.workspace_id=$1 AND u.id>$2 AND w.status='active' AND a.status='active' AND $4::text=$1 ORDER BY u.id LIMIT $3`
		if q.Kind == "project-members" {
			query = `SELECT u.id,u.display_name,m.role,(w.status='active' AND COALESCE(a.status='active',false)) FROM radishnexus.project_memberships m JOIN radishnexus.users u ON u.id=m.user_id JOIN radishnexus.workspace_memberships w ON w.workspace_id=m.workspace_id AND w.user_id=u.id LEFT JOIN radishnexus.user_accounts a ON a.user_id=u.id WHERE m.workspace_id=$1 AND u.id>$2 AND m.project_id=$4 ORDER BY u.id LIMIT $3`
		}
		if q.Kind == "channel-members" {
			query = `SELECT u.id,u.display_name,'' AS role,(w.status='active' AND COALESCE(a.status='active',false) AND pm.user_id IS NOT NULL) FROM radishnexus.channel_memberships m JOIN radishnexus.channels c ON c.workspace_id=m.workspace_id AND c.id=m.channel_id JOIN radishnexus.users u ON u.id=m.user_id JOIN radishnexus.workspace_memberships w ON w.workspace_id=m.workspace_id AND w.user_id=u.id LEFT JOIN radishnexus.user_accounts a ON a.user_id=u.id LEFT JOIN radishnexus.project_memberships pm ON pm.workspace_id=m.workspace_id AND pm.project_id=c.governing_project_id AND pm.user_id=u.id WHERE m.workspace_id=$1 AND u.id>$2 AND m.channel_id=$4 ORDER BY u.id LIMIT $3`
		}
		rows, e := tx.Query(ctx, query, p.WorkspaceID, q.AfterID, q.Limit+1, q.ScopeID)
		if e != nil {
			return page, e
		}
		for rows.Next() {
			m := goldenpath.ConfigurationMember{}
			if err = rows.Scan(&m.ID, &m.Name, &m.Role, &m.Eligible); err != nil {
				rows.Close()
				return page, err
			}
			page.Members = append(page.Members, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, err
		}
		if len(page.Members) > q.Limit {
			page.Members = page.Members[:q.Limit]
			page.NextID = page.Members[q.Limit-1].ID
		}
	}
	err = tx.Commit(ctx)
	return page, err
}
