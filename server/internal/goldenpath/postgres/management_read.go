package postgres

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (s *Store) ListManagementResponsibilities(ctx context.Context, a goldenpath.OwnerAccess, after string, limit int) (out goldenpath.ManagementResponsibilityPage, err error) {
	out.Items = []goldenpath.ManagementResponsibility{}
	err = s.managementTransaction(ctx, a, false, false, func(tx pgx.Tx, session ownerSessionState) (bool, error) {
		rows, e := tx.Query(ctx, `SELECT id,kind,name,role FROM (
 SELECT p.id,'project' kind,p.name,m.role FROM radishnexus.projects p JOIN radishnexus.project_memberships m ON m.workspace_id=p.workspace_id AND m.project_id=p.id WHERE p.workspace_id=$1 AND m.user_id=$2 AND m.role='admin'
 UNION ALL SELECT c.id,'channel',c.name,CASE WHEN pm.role='admin' THEN 'admin' ELSE cm.management_role END FROM radishnexus.channels c JOIN radishnexus.channel_memberships cm ON cm.workspace_id=c.workspace_id AND cm.channel_id=c.id JOIN radishnexus.project_memberships pm ON pm.workspace_id=c.workspace_id AND pm.project_id=c.governing_project_id AND pm.user_id=cm.user_id WHERE c.workspace_id=$1 AND cm.user_id=$2 AND c.visibility='restricted' AND (pm.role='admin' OR cm.management_role='manager')
 UNION ALL SELECT t.id,'thread',t.title,CASE WHEN pm.role='admin' THEN 'admin' ELSE tm.management_role END FROM radishnexus.threads t JOIN radishnexus.thread_memberships tm ON tm.workspace_id=t.workspace_id AND tm.thread_id=t.id JOIN radishnexus.project_memberships pm ON pm.workspace_id=t.workspace_id AND pm.project_id=t.governing_project_id AND pm.user_id=tm.user_id WHERE t.workspace_id=$1 AND tm.user_id=$2 AND t.visibility='restricted' AND (pm.role='admin' OR tm.management_role='manager') AND (t.origin_channel_id IS NULL OR EXISTS(SELECT 1 FROM radishnexus.channels c WHERE c.workspace_id=t.workspace_id AND c.id=t.origin_channel_id AND (c.visibility='project' OR EXISTS(SELECT 1 FROM radishnexus.channel_memberships cm WHERE cm.workspace_id=c.workspace_id AND cm.channel_id=c.id AND cm.user_id=tm.user_id))))
 ) readable_responsibilities WHERE id>$3 ORDER BY id LIMIT $4`, a.Principal.WorkspaceID, a.Principal.ID, after, limit+1)
		if e != nil {
			return false, e
		}
		out.Items, e = pgx.CollectRows(rows, func(r pgx.CollectableRow) (v goldenpath.ManagementResponsibility, e error) {
			e = r.Scan(&v.ID, &v.Kind, &v.Name, &v.Role)
			return
		})
		if e != nil {
			return false, e
		}
		if len(out.Items) > limit {
			out.Items = out.Items[:limit]
			out.NextID = out.Items[limit-1].ID
		}
		return false, nil
	})
	return
}

func (s *Store) ListManagementMembers(ctx context.Context, a goldenpath.OwnerAccess, q goldenpath.ManagementQuery) (out goldenpath.ManagementPage, err error) {
	out.Items = []goldenpath.ManagementMember{}
	err = s.managementTransaction(ctx, a, false, false, func(tx pgx.Tx, session ownerSessionState) (bool, error) {
		o, e := readManagementScope(ctx, tx, a.Principal, q.ScopeKind, q.ScopeID)
		if e != nil {
			return false, e
		}
		out.CanManage, out.SelfRole, out.ScopeRevision = o.canManage, o.actorRole, strconv.FormatInt(o.revision, 10)
		if q.Candidates && !o.canManage {
			return false, authz.ErrForbidden
		}
		// Non-managers receive only their own membership for the leave flow.
		self := ""
		if !o.canManage {
			self = a.Principal.ID
		}
		role, join, filter := "m.role", "", "true"
		eligible := "a.status='active' AND m.status='active'"
		if o.kind == "project" {
			join = ` LEFT JOIN radishnexus.project_memberships pm ON pm.workspace_id=m.workspace_id AND pm.user_id=m.user_id AND pm.project_id=$5 `
			role = "coalesce(pm.role,'')"
			filter = "pm.user_id IS NOT NULL"
		}
		if o.kind == "channel" || o.kind == "thread" {
			table, key := "channel_memberships", "channel_id"
			if o.kind == "thread" {
				table, key = "thread_memberships", "thread_id"
			}
			join = ` LEFT JOIN radishnexus.project_memberships pm ON pm.workspace_id=m.workspace_id AND pm.user_id=m.user_id AND pm.project_id=$5 LEFT JOIN radishnexus.` + table + ` lm ON lm.workspace_id=m.workspace_id AND lm.user_id=m.user_id AND lm.` + key + `=$6 `
			role = "coalesce(lm.management_role,'')"
			filter = "lm.user_id IS NOT NULL"
			eligible += ` AND pm.user_id IS NOT NULL AND ($7='' OR EXISTS(SELECT 1 FROM radishnexus.channels c WHERE c.workspace_id=m.workspace_id AND c.id=$7 AND (c.visibility='project' OR EXISTS(SELECT 1 FROM radishnexus.channel_memberships cm WHERE cm.workspace_id=c.workspace_id AND cm.channel_id=c.id AND cm.user_id=m.user_id))))`
			if q.Candidates {
				filter = "lm.user_id IS NULL AND " + eligible + " AND $8='active'"
			}
		}
		if q.Candidates && (o.kind == "workspace" || o.kind == "project") {
			filter = eligible
			if o.kind == "project" {
				filter += " AND pm.user_id IS NULL AND $8='active'"
			}
		}
		query := `SELECT u.id,u.display_name,m.status,` + role + `,m.lifecycle_revision::text,(` + eligible + `) FROM radishnexus.workspace_memberships m JOIN radishnexus.users u ON u.id=m.user_id JOIN radishnexus.user_accounts a ON a.user_id=u.id ` + join + ` WHERE m.workspace_id=$1 AND u.id>$2 AND ($4='' OR u.id=$4) AND (` + filter + `) AND NOT EXISTS(SELECT 1 FROM radishnexus.context_import_mappings im WHERE im.kind='actor' AND im.target_id=u.id) `
		// Keep a single typed argument contract for the four static SQL branches.
		query += ` AND $5::text=$5 AND $6::text=$6 AND $7::text=$7 AND $8::text=$8 ORDER BY u.id LIMIT $3`
		rows, e := tx.Query(ctx, query, a.Principal.WorkspaceID, q.AfterID, q.Limit+1, self, o.project, o.id, o.channel, o.status)
		if e != nil {
			return false, e
		}
		out.Items, e = pgx.CollectRows(rows, func(r pgx.CollectableRow) (v goldenpath.ManagementMember, e error) {
			e = r.Scan(&v.ID, &v.DisplayName, &v.Status, &v.Role, &v.LifecycleRevision, &v.Eligible)
			return
		})
		if e != nil {
			return false, e
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			out.NextID = out.Items[q.Limit-1].ID
		}
		return false, nil
	})
	return
}

func (s *Store) ListManagementAudit(ctx context.Context, a goldenpath.OwnerAccess, q goldenpath.ManagementQuery) (out goldenpath.ManagementAuditPage, err error) {
	out.Items = []goldenpath.ManagementAudit{}
	err = s.managementTransaction(ctx, a, false, false, func(tx pgx.Tx, session ownerSessionState) (bool, error) {
		o, e := readManagementScope(ctx, tx, a.Principal, q.ScopeKind, q.ScopeID)
		if e != nil {
			return false, e
		}
		if !o.canManage {
			return false, authz.ErrForbidden
		}
		// Exact current scope authorization precedes pagination; no total or
		// descendant audit query can disclose a private scope's existence.
		rows, e := tx.Query(ctx, `SELECT id,actor_id,subject_id,command_kind,before_state,after_state,occurred_at FROM radishnexus.workspace_configuration_audit WHERE workspace_id=$1 AND scope_id=$2 AND command_kind LIKE $3 AND id>$4 ORDER BY id LIMIT $5`, a.Principal.WorkspaceID, o.id, "management."+o.kind+".%", q.AfterID, q.Limit+1)
		if e != nil {
			return false, e
		}
		out.Items, e = pgx.CollectRows(rows, func(r pgx.CollectableRow) (v goldenpath.ManagementAudit, e error) {
			e = r.Scan(&v.ID, &v.ActorID, &v.TargetUserID, &v.Action, &v.Before, &v.After, &v.OccurredAt)
			return
		})
		if e != nil {
			return false, e
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			out.NextID = out.Items[q.Limit-1].ID
		}
		return false, nil
	})
	return
}
