package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/workspaceauth"
)

func (s *Store) managementTransaction(ctx context.Context, a goldenpath.OwnerAccess, write, history bool, fn func(pgx.Tx, ownerSessionState) (bool, error)) (err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ownerDBError("begin management", err)
	}
	defer func() { err = errors.Join(err, workspaceauth.Rollback(ctx, tx)) }()
	if history {
		err = workspaceauth.SchemaLock(ctx, tx)
	} else {
		err = workspaceauth.Lock(ctx, tx, a.Principal.WorkspaceID, write)
	}
	if err != nil {
		return ownerDBError("lock management scope", err)
	}
	// Global account recovery does not take a Workspace gate. Lock the accounts
	// used by eligibility and last-manager checks in stable order for writes.
	// Shared locks allow other Workspaces to read and govern the same accounts.
	if write {
		rows, e := tx.Query(ctx, `SELECT a.user_id FROM radishnexus.user_accounts a JOIN radishnexus.workspace_memberships m ON m.user_id=a.user_id WHERE m.workspace_id=$1 ORDER BY a.user_id FOR SHARE OF a`, a.Principal.WorkspaceID)
		if e != nil {
			return e
		}
		if _, e = pgx.CollectRows(rows, pgx.RowTo[string]); e != nil {
			return e
		}
	}
	active, err := lockOwnerAccount(ctx, tx, a.Principal.ID)
	if err != nil {
		return err
	}
	if !active {
		return authn.ErrInvalidSession
	}
	session, err := lockOwnerSession(ctx, tx, a)
	if err != nil {
		return err
	}
	if _, err = ownerSessionNow(ctx, tx, session, false); err != nil {
		return err
	}
	if !history {
		if err = workspaceauth.Check(ctx, tx, a.Principal); err != nil {
			return err
		}
	}
	changed, err := fn(tx, session)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		var pgerr *pgconn.PgError
		if changed && !errors.Is(err, pgx.ErrTxCommitRollback) && !(errors.As(err, &pgerr) && pgerr.Severity == "ERROR") {
			return errors.Join(goldenpath.ErrManagementUnconfirmed, ownerDBError("management commit acknowledgement", err))
		}
		return ownerDBError("commit management", err)
	}
	return nil
}

type managementScope struct {
	kind, id, project, channel, status, visibility, actorRole string
	revision, projectRevision, channelRevision                int64
	canManage                                                 bool
}

// Readability is checked before management. Workspace ownership never bypasses
// explicit Project and private memberships, including archived objects.
func readManagementScope(ctx context.Context, tx pgx.Tx, p authz.Principal, kind, id string) (o managementScope, err error) {
	o.kind, o.id = kind, id
	if kind == "workspace" {
		err = tx.QueryRow(ctx, `SELECT role,lifecycle_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2 AND status='active'`, p.WorkspaceID, p.ID).Scan(&o.actorRole, &o.revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return o, authz.ErrNotFound
		}
		o.canManage = o.actorRole == "owner"
		return
	}
	o.project = id
	if kind == "channel" {
		err = tx.QueryRow(ctx, `SELECT governing_project_id,status,visibility,management_revision FROM radishnexus.channels WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, id).Scan(&o.project, &o.status, &o.visibility, &o.revision)
	}
	if kind == "thread" {
		err = tx.QueryRow(ctx, `SELECT governing_project_id,coalesce(origin_channel_id,''),'active'::text,visibility,management_revision FROM radishnexus.threads WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, id).Scan(&o.project, &o.channel, &o.status, &o.visibility, &o.revision)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return o, authz.ErrNotFound
	}
	if err != nil {
		return o, err
	}
	var projectStatus, projectVisibility, role string
	err = tx.QueryRow(ctx, `SELECT p.status,p.visibility,p.management_revision,coalesce(m.role,'') FROM radishnexus.projects p LEFT JOIN radishnexus.project_memberships m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.user_id=$3 WHERE p.workspace_id=$1 AND p.id=$2`, p.WorkspaceID, o.project, p.ID).Scan(&projectStatus, &projectVisibility, &o.projectRevision, &role)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && projectVisibility == "restricted" && role == "" {
		return o, authz.ErrNotFound
	}
	if err != nil {
		return o, err
	}
	if kind == "project" {
		o.status, o.visibility, o.actorRole, o.revision = projectStatus, projectVisibility, role, o.projectRevision
		o.canManage = role == "admin"
		return o, nil
	}
	if projectStatus != "active" {
		o.status = "archived"
	}
	if o.channel != "" {
		var channelStatus, visibility string
		var member bool
		err = tx.QueryRow(ctx, `SELECT c.status,c.visibility,c.management_revision,EXISTS(SELECT 1 FROM radishnexus.channel_memberships m WHERE m.workspace_id=c.workspace_id AND m.channel_id=c.id AND m.user_id=$3) FROM radishnexus.channels c WHERE c.workspace_id=$1 AND c.id=$2`, p.WorkspaceID, o.channel, p.ID).Scan(&channelStatus, &visibility, &o.channelRevision, &member)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && visibility == "restricted" && !member {
			return o, authz.ErrNotFound
		}
		if err != nil {
			return o, err
		}
		if channelStatus != "active" {
			o.status = "archived"
		}
	}
	if o.visibility != "restricted" {
		return o, authz.ErrInvalid
	}
	table, key := "channel_memberships", "channel_id"
	if kind == "thread" {
		table, key = "thread_memberships", "thread_id"
	}
	err = tx.QueryRow(ctx, `SELECT management_role FROM radishnexus.`+table+` WHERE workspace_id=$1 AND `+key+`=$2 AND user_id=$3`, p.WorkspaceID, id, p.ID).Scan(&o.actorRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, authz.ErrNotFound
	}
	if err != nil {
		return o, err
	}
	o.canManage = role != "" && (role == "admin" || o.actorRole == "manager")
	return o, nil
}

type managementTarget struct {
	name, status, workspaceRole, role string
	life, roleRevision                int64
	eligible                          bool
}

func readManagementTarget(ctx context.Context, tx pgx.Tx, w, user string, o managementScope) (t managementTarget, err error) {
	err = tx.QueryRow(ctx, `SELECT u.display_name,m.status,m.role,m.lifecycle_revision,m.role_revision,a.status='active' FROM radishnexus.workspace_memberships m JOIN radishnexus.users u ON u.id=m.user_id JOIN radishnexus.user_accounts a ON a.user_id=u.id WHERE m.workspace_id=$1 AND m.user_id=$2 AND NOT EXISTS(SELECT 1 FROM radishnexus.context_import_mappings im WHERE im.kind='actor' AND im.target_id=u.id)`, w, user).Scan(&t.name, &t.status, &t.workspaceRole, &t.life, &t.roleRevision, &t.eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, authz.ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if o.kind == "workspace" {
		t.role = t.status
		return t, nil
	}
	var projectRole string
	err = tx.QueryRow(ctx, `SELECT coalesce((SELECT role FROM radishnexus.project_memberships WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3),'')`, w, o.project, user).Scan(&projectRole)
	if err != nil {
		return t, err
	}
	t.eligible = t.eligible && t.status == "active"
	if o.kind == "project" {
		t.role = projectRole
		return t, nil
	}
	t.eligible = t.eligible && projectRole != ""
	table, key := "channel_memberships", "channel_id"
	if o.kind == "thread" {
		table, key = "thread_memberships", "thread_id"
	}
	err = tx.QueryRow(ctx, `SELECT coalesce((SELECT management_role FROM radishnexus.`+table+` WHERE workspace_id=$1 AND `+key+`=$2 AND user_id=$3),'')`, w, o.id, user).Scan(&t.role)
	if err != nil {
		return t, err
	}
	if o.channel != "" {
		var parentReadable bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.channels c WHERE c.workspace_id=$1 AND c.id=$2 AND (c.visibility='project' OR EXISTS(SELECT 1 FROM radishnexus.channel_memberships m WHERE m.workspace_id=c.workspace_id AND m.channel_id=c.id AND m.user_id=$3)))`, w, o.channel, user).Scan(&parentReadable)
		t.eligible = t.eligible && parentReadable
	}
	return t, err
}

func prepareManagement(ctx context.Context, tx pgx.Tx, a goldenpath.OwnerAccess, in goldenpath.ManagementProposal) (plan goldenpath.ManagementPlan, o managementScope, err error) {
	p := a.Principal
	o, err = readManagementScope(ctx, tx, p, in.ScopeKind, in.ScopeID)
	if err != nil {
		return
	}
	if in.Action != "leave" && !o.canManage {
		err = authz.ErrForbidden
		return
	}
	t, err := readManagementTarget(ctx, tx, p.WorkspaceID, in.TargetUserID, o)
	if err != nil {
		return
	}
	var actorLife, actorRoleRevision int64
	err = tx.QueryRow(ctx, `SELECT lifecycle_revision,role_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, p.WorkspaceID, p.ID).Scan(&actorLife, &actorRoleRevision)
	if err != nil {
		return
	}
	after := t.role
	if in.ScopeKind == "workspace" {
		if !t.eligible && in.Action != "remove" {
			err = authz.ErrConflict
			return
		}
		switch in.Action {
		case "suspend":
			if t.status != "active" {
				err = authz.ErrConflict
			}
			after = "suspended"
		case "reinvite":
			if t.status != "removed" {
				err = authz.ErrConflict
			}
			after = "removed"
		case "resume":
			if t.status != "suspended" {
				err = authz.ErrConflict
			}
			after = "active"
		case "remove", "leave":
			if (t.status != "active" && t.status != "suspended") || t.workspaceRole == "owner" {
				err = authz.ErrConflict
			}
			after = "removed"
		}
		o.revision = t.life
	} else {
		switch in.Action {
		case "add":
			if !t.eligible || t.role != "" || o.status != "active" {
				err = authz.ErrConflict
			}
			after = "member"
			if o.kind == "project" {
				after = in.Role
			}
		case "role":
			if t.role == "" || in.Role == t.role {
				err = authz.ErrConflict
			}
			if (in.Role == "admin" || in.Role == "manager") && !t.eligible {
				err = authz.ErrConflict
			}
			after = in.Role
		case "handoff":
			if t.role == "" || !t.eligible {
				err = authz.ErrConflict
			}
			after = "manager"
			if o.kind == "project" {
				after = "admin"
				if o.actorRole != "admin" {
					err = authz.ErrForbidden
				}
			}
		case "remove", "leave":
			if t.role == "" {
				err = authz.ErrConflict
			}
			after = ""
		}
	}
	if err != nil {
		return
	}
	plan = goldenpath.ManagementPlan{ManagementProposal: in, ActorID: p.ID, TargetName: t.name, Before: t.role, After: after, ActorRole: o.actorRole, TargetWorkspaceRole: t.workspaceRole, TargetStatus: t.status, ScopeRevision: strconv.FormatInt(o.revision, 10), ActorLifecycleRevision: strconv.FormatInt(actorLife, 10), TargetLifecycleRevision: strconv.FormatInt(t.life, 10), ActorRoleRevision: strconv.FormatInt(actorRoleRevision, 10), TargetRoleRevision: strconv.FormatInt(t.roleRevision, 10), Policy: "membership-management-v1"}
	plan.ParentProjectRevision = strconv.FormatInt(o.projectRevision, 10)
	plan.ParentChannelRevision = strconv.FormatInt(o.channelRevision, 10)
	plan.Digest, err = plan.SHA256(p.WorkspaceID)
	return
}

func (s *Store) PreviewManagement(ctx context.Context, a goldenpath.OwnerAccess, in goldenpath.ManagementProposal) (out goldenpath.ManagementPlan, err error) {
	err = s.managementTransaction(ctx, a, false, false, func(tx pgx.Tx, session ownerSessionState) (bool, error) {
		var e error
		out, _, e = prepareManagement(ctx, tx, a, in)
		return false, e
	})
	return
}
func managementReceipt(ctx context.Context, tx pgx.Tx, a goldenpath.OwnerAccess, operation string) (out goldenpath.ManagementResult, digest string, err error) {
	out = goldenpath.ManagementResult{Result: "not_observed", WorkspaceID: a.Principal.WorkspaceID, ClientOperationID: operation}
	var occurred time.Time
	err = tx.QueryRow(ctx, `SELECT r.payload_sha256,a.command_kind,a.scope_id,a.subject_id,a.occurred_at FROM radishnexus.workspace_configuration_receipts r JOIN radishnexus.workspace_configuration_audit a ON a.workspace_id=r.workspace_id AND a.id=r.audit_id WHERE r.workspace_id=$1 AND r.actor_id=$2 AND r.client_operation_id=$3 AND r.command_kind LIKE 'management.%'`, a.Principal.WorkspaceID, a.Principal.ID, operation).Scan(&digest, &out.Action, &out.ScopeID, &out.TargetUserID, &occurred)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, "", nil
	}
	if err != nil {
		return out, "", err
	}
	out.Result = "applied"
	out.OccurredAt = &occurred
	return
}
func (s *Store) ReadManagementResult(ctx context.Context, a goldenpath.OwnerAccess, operation string) (out goldenpath.ManagementResult, err error) {
	// This dedicated path requires a live global session, but deliberately does
	// not re-admit the actor to a Workspace they have left. Only their receipt.
	err = s.managementTransaction(ctx, a, false, true, func(tx pgx.Tx, session ownerSessionState) (bool, error) {
		var e error
		out, _, e = managementReceipt(ctx, tx, a, operation)
		return false, e
	})
	return
}
func (s *Store) ApplyManagement(ctx context.Context, c goldenpath.ManagementCommand) (out goldenpath.ManagementResult, err error) {
	a, in := c.Access, c.Input
	err = s.managementTransaction(ctx, a, true, false, func(tx pgx.Tx, session ownerSessionState) (bool, error) {
		previous, digest, e := managementReceipt(ctx, tx, a, in.ClientOperationID)
		if e != nil {
			return false, e
		}
		if previous.Result == "applied" {
			if digest != c.PayloadSHA256 {
				return false, authz.ErrConflict
			}
			out = previous
			return false, nil
		}
		plan, o, e := prepareManagement(ctx, tx, a, in.ManagementProposal)
		if e != nil {
			return false, e
		}
		if plan.Digest != in.PlanDigest {
			return false, authz.ErrConflict
		}
		now, e := ownerSessionNow(ctx, tx, session, true)
		if e != nil {
			return false, e
		}
		users := []string{in.TargetUserID}
		if in.Action == "handoff" {
			users = append(users, a.Principal.ID)
		}
		var responsibilities []managementResponsibility
		if in.Action != "suspend" && in.Action != "resume" && in.Action != "add" && in.Action != "reinvite" {
			responsibilities, e = affectedManagementResponsibilities(ctx, tx, a.Principal.WorkspaceID, users, o)
			if e != nil {
				return false, e
			}
		}
		if in.ScopeKind == "workspace" && in.Action == "suspend" {
			var owners int
			e = tx.QueryRow(ctx, `SELECT count(*) FROM radishnexus.workspace_memberships m JOIN radishnexus.user_accounts u ON u.user_id=m.user_id WHERE m.workspace_id=$1 AND m.user_id<>$2 AND m.role='owner' AND m.status='active' AND u.status='active'`, a.Principal.WorkspaceID, in.TargetUserID).Scan(&owners)
			if e != nil {
				return false, e
			}
			if owners == 0 {
				return false, authz.ErrConflict
			}
		}
		revoked, e := applyManagementChange(ctx, tx, c, o, plan, now)
		if e != nil {
			return false, e
		}
		if e = checkManagementResponsibilities(ctx, tx, a.Principal.WorkspaceID, responsibilities); e != nil {
			return false, e
		}
		var revision int64
		if o.kind == "workspace" {
			e = tx.QueryRow(ctx, `SELECT lifecycle_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, a.Principal.WorkspaceID, in.TargetUserID).Scan(&revision)
		} else {
			e = tx.QueryRow(ctx, `SELECT management_revision FROM radishnexus.`+managementObjectTable(o.kind)+` WHERE workspace_id=$1 AND id=$2`, a.Principal.WorkspaceID, o.id).Scan(&revision)
		}
		if e != nil {
			return false, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO radishnexus.workspace_configuration_audit(id,workspace_id,actor_id,command_kind,scope_id,subject_id,result_id,before_state,after_state,changed,request_id,occurred_at,management_plan_digest,management_before_revision,management_after_revision,management_actor_after_role,revoked_authorization_ids) VALUES($1,$2,$3,$4,$5,$6,$6,$7,$8,true,$9,$10,$11,$12,$13,$14,$15)`, c.AuditID, a.Principal.WorkspaceID, a.Principal.ID, in.Kind(), in.ScopeID, in.TargetUserID, plan.Before, plan.After, c.RequestID, now, in.PlanDigest, o.revision, revision, in.ActorAfterRole, revoked)
		if e != nil {
			return false, ownerDBError("write management audit", e)
		}
		_, e = tx.Exec(ctx, `INSERT INTO radishnexus.workspace_configuration_receipts(workspace_id,actor_id,command_kind,scope_id,subject_id,client_operation_id,payload_sha256,audit_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.Principal.WorkspaceID, a.Principal.ID, in.Kind(), in.ScopeID, in.TargetUserID, in.ClientOperationID, c.PayloadSHA256, c.AuditID, now)
		if e != nil {
			return false, ownerDBError("write management receipt", e)
		}
		out = goldenpath.ManagementResult{Result: "applied", WorkspaceID: a.Principal.WorkspaceID, ClientOperationID: in.ClientOperationID, Action: in.Kind(), ScopeID: in.ScopeID, TargetUserID: in.TargetUserID, OccurredAt: &now}
		if in.Action == "reinvite" {
			out.Invitation = &goldenpath.ManagementInvitation{Token: c.InvitationToken, ExpiresAt: now.Add(authn.InvitationLifetime)}
		}
		return true, nil
	})
	return
}
func managementObjectTable(kind string) string {
	switch kind {
	case "project":
		return "projects"
	case "channel":
		return "channels"
	case "thread":
		return "threads"
	}
	panic("validated management scope required")
}

func applyManagementChange(ctx context.Context, tx pgx.Tx, c goldenpath.ManagementCommand, o managementScope, p goldenpath.ManagementPlan, now time.Time) (revoked []string, err error) {
	w, in := c.Access.Principal.WorkspaceID, c.Input
	revoked = []string{}
	if o.kind == "workspace" {
		if in.Action == "reinvite" {
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM radishnexus.identity_invitations WHERE workspace_id=$1 AND created_by=$2 AND consumed_at IS NULL AND expires_at>$3`, w, c.Access.Principal.ID, now).Scan(&count); err != nil {
				return
			}
			if count >= 20 {
				return nil, authz.ErrConflict
			}
			_, err = tx.Exec(ctx, `INSERT INTO radishnexus.identity_invitations(id,token_digest,workspace_id,created_by,created_at,expires_at,creator_role_revision,creator_lifecycle_revision,target_user_id,target_lifecycle_revision) SELECT $1,$2,$3,$4,$5,$6,role_revision,lifecycle_revision,$7,$8 FROM radishnexus.workspace_memberships WHERE workspace_id=$3 AND user_id=$4`, c.InvitationID, c.InvitationDigest, w, c.Access.Principal.ID, now, now.Add(authn.InvitationLifetime), in.TargetUserID, p.TargetLifecycleRevision)
			return
		}
		if in.Action == "suspend" || in.Action == "remove" || in.Action == "leave" {
			rows, e := tx.Query(ctx, `UPDATE radishnexus.environment_deployment_authorizations SET status='revoked',revoked_by=$3,revoked_at=greatest($4,granted_at) WHERE workspace_id=$1 AND user_id=$2 AND status='active' RETURNING id`, w, in.TargetUserID, c.Access.Principal.ID, now)
			if e != nil {
				return nil, e
			}
			revoked, err = pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return nil, err
			}
		}
		if p.After == "removed" {
			if err = deleteManagementGrants(ctx, tx, w, in.TargetUserID, o); err != nil {
				return nil, err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status=$3 WHERE workspace_id=$1 AND user_id=$2`, w, in.TargetUserID, p.After)
		return
	}
	if in.Action == "remove" || in.Action == "leave" {
		err = deleteManagementGrants(ctx, tx, w, in.TargetUserID, o)
		return
	}
	if in.Action == "add" {
		if o.kind == "project" {
			_, err = tx.Exec(ctx, `INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role,created_at) VALUES($1,$2,$3,$4,$5)`, w, o.id, in.TargetUserID, in.Role, now)
			return
		}
		table, key := "channel_memberships", "channel_id"
		if o.kind == "thread" {
			table, key = "thread_memberships", "thread_id"
		}
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.`+table+`(workspace_id,`+key+`,user_id,management_role,created_at) VALUES($1,$2,$3,'member',$4)`, w, o.id, in.TargetUserID, now)
		return
	}
	if err = setManagementRole(ctx, tx, w, o, in.TargetUserID, p.After); err != nil {
		return
	}
	if in.Action == "handoff" {
		if in.ActorAfterRole == "" {
			err = deleteManagementGrants(ctx, tx, w, c.Access.Principal.ID, o)
		} else {
			err = setManagementRole(ctx, tx, w, o, c.Access.Principal.ID, in.ActorAfterRole)
		}
	}
	return
}
func setManagementRole(ctx context.Context, tx pgx.Tx, w string, o managementScope, user, role string) error {
	table, key, column := "project_memberships", "project_id", "role"
	if o.kind == "channel" {
		table, key, column = "channel_memberships", "channel_id", "management_role"
	}
	if o.kind == "thread" {
		table, key, column = "thread_memberships", "thread_id", "management_role"
	}
	tag, err := tx.Exec(ctx, `UPDATE radishnexus.`+table+` SET `+column+`=$4 WHERE workspace_id=$1 AND `+key+`=$2 AND user_id=$3`, w, o.id, user, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return authz.ErrConflict
	}
	return nil
}
func deleteManagementGrants(ctx context.Context, tx pgx.Tx, w, user string, o managementScope) error {
	// Descendants first. Explicit private grants cannot survive a parent exit.
	if _, err := tx.Exec(ctx, `DELETE FROM radishnexus.thread_memberships m USING radishnexus.threads t WHERE m.workspace_id=$1 AND m.user_id=$2 AND t.workspace_id=m.workspace_id AND t.id=m.thread_id AND ($3='workspace' OR ($3='project' AND t.governing_project_id=$4) OR ($3='channel' AND t.origin_channel_id=$4) OR ($3='thread' AND t.id=$4))`, w, user, o.kind, o.id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM radishnexus.channel_memberships m USING radishnexus.channels c WHERE m.workspace_id=$1 AND m.user_id=$2 AND c.workspace_id=m.workspace_id AND c.id=m.channel_id AND ($3='workspace' OR ($3='project' AND c.governing_project_id=$4) OR ($3='channel' AND c.id=$4))`, w, user, o.kind, o.id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM radishnexus.project_memberships WHERE workspace_id=$1 AND user_id=$2 AND ($3='workspace' OR ($3='project' AND project_id=$4))`, w, user, o.kind, o.id)
	return err
}

type managementResponsibility struct{ kind, id string }

func affectedManagementResponsibilities(ctx context.Context, tx pgx.Tx, w string, users []string, o managementScope) ([]managementResponsibility, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT kind,id FROM (
 SELECT 'project' kind,p.project_id id FROM radishnexus.project_memberships p WHERE p.workspace_id=$1 AND p.user_id=ANY($2) AND p.role='admin' AND ($3='workspace' OR ($3='project' AND p.project_id=$4))
 UNION ALL SELECT 'channel',c.id FROM radishnexus.channels c JOIN radishnexus.channel_memberships m ON m.workspace_id=c.workspace_id AND m.channel_id=c.id LEFT JOIN radishnexus.project_memberships p ON p.workspace_id=m.workspace_id AND p.project_id=c.governing_project_id AND p.user_id=m.user_id WHERE c.workspace_id=$1 AND m.user_id=ANY($2) AND (m.management_role='manager' OR p.role='admin') AND ($3='workspace' OR ($3='project' AND c.governing_project_id=$4) OR ($3='channel' AND c.id=$4))
 UNION ALL SELECT 'thread',t.id FROM radishnexus.threads t JOIN radishnexus.thread_memberships m ON m.workspace_id=t.workspace_id AND m.thread_id=t.id LEFT JOIN radishnexus.project_memberships p ON p.workspace_id=m.workspace_id AND p.project_id=t.governing_project_id AND p.user_id=m.user_id WHERE t.workspace_id=$1 AND m.user_id=ANY($2) AND (m.management_role='manager' OR p.role='admin') AND ($3='workspace' OR ($3='project' AND t.governing_project_id=$4) OR ($3='channel' AND t.origin_channel_id=$4) OR ($3='thread' AND t.id=$4))
 ) responsibilities ORDER BY kind,id`, w, users, o.kind, o.id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (v managementResponsibility, e error) { e = r.Scan(&v.kind, &v.id); return })
}
func checkManagementResponsibilities(ctx context.Context, tx pgx.Tx, w string, items []managementResponsibility) error {
	scopes := map[string][]string{"project": {}, "channel": {}, "thread": {}}
	for _, item := range items {
		scopes[item.kind] = append(scopes[item.kind], item.id)
	}
	// A bounded number of database round trips, independent of descendant count.
	for _, kind := range []string{"project", "channel", "thread"} {
		if len(scopes[kind]) == 0 {
			continue
		}
		var query string
		switch kind {
		case "project":
			query = `SELECT NOT EXISTS(SELECT 1 FROM unnest($2::text[]) AS affected(id) WHERE NOT EXISTS(SELECT 1 FROM radishnexus.project_memberships p JOIN radishnexus.workspace_memberships m USING(workspace_id,user_id) JOIN radishnexus.user_accounts a ON a.user_id=p.user_id WHERE p.workspace_id=$1 AND p.project_id=affected.id AND p.role='admin' AND m.status='active' AND a.status='active'))`
		case "channel":
			query = `SELECT NOT EXISTS(SELECT 1 FROM unnest($2::text[]) AS affected(id) WHERE NOT EXISTS(SELECT 1 FROM radishnexus.channels c JOIN radishnexus.channel_memberships cm ON cm.workspace_id=c.workspace_id AND cm.channel_id=c.id JOIN radishnexus.project_memberships p ON p.workspace_id=c.workspace_id AND p.project_id=c.governing_project_id AND p.user_id=cm.user_id JOIN radishnexus.workspace_memberships m ON m.workspace_id=cm.workspace_id AND m.user_id=cm.user_id JOIN radishnexus.user_accounts a ON a.user_id=cm.user_id WHERE c.workspace_id=$1 AND c.id=affected.id AND (cm.management_role='manager' OR p.role='admin') AND m.status='active' AND a.status='active'))`
		case "thread":
			query = `SELECT NOT EXISTS(SELECT 1 FROM unnest($2::text[]) AS affected(id) WHERE NOT EXISTS(SELECT 1 FROM radishnexus.threads t JOIN radishnexus.thread_memberships tm ON tm.workspace_id=t.workspace_id AND tm.thread_id=t.id JOIN radishnexus.project_memberships p ON p.workspace_id=t.workspace_id AND p.project_id=t.governing_project_id AND p.user_id=tm.user_id JOIN radishnexus.workspace_memberships m ON m.workspace_id=tm.workspace_id AND m.user_id=tm.user_id JOIN radishnexus.user_accounts a ON a.user_id=tm.user_id WHERE t.workspace_id=$1 AND t.id=affected.id AND (tm.management_role='manager' OR p.role='admin') AND m.status='active' AND a.status='active' AND (t.origin_channel_id IS NULL OR EXISTS(SELECT 1 FROM radishnexus.channels c WHERE c.workspace_id=t.workspace_id AND c.id=t.origin_channel_id AND (c.visibility='project' OR EXISTS(SELECT 1 FROM radishnexus.channel_memberships cm WHERE cm.workspace_id=c.workspace_id AND cm.channel_id=c.id AND cm.user_id=tm.user_id))))))`
		}
		var valid bool
		if err := tx.QueryRow(ctx, query, w, scopes[kind]).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return authz.ErrConflict
		}
	}
	return nil
}
