package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// Lock the account before membership, matching identity operations. Project
// locks then serialize all configuration of its roles and narrow grants.
func configurationActor(ctx context.Context, tx pgx.Tx, p authz.Principal) (bool, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id=$1 FOR SHARE`, p.ID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return false, authz.ErrNotFound
	}
	if err != nil {
		return false, err
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT status,role FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, p.WorkspaceID, p.ID).Scan(&status, &role)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return false, authz.ErrNotFound
	}
	return role == "owner", err
}

func configurationProject(ctx context.Context, tx pgx.Tx, p authz.Principal, id string, write bool) (goldenpath.ConfigurationObject, error) {
	o := goldenpath.ConfigurationObject{ID: id, Kind: "project"}
	lock := "FOR SHARE"
	if write {
		lock = "FOR UPDATE"
	}
	err := tx.QueryRow(ctx, `SELECT name,key,visibility,status FROM radishnexus.projects WHERE workspace_id=$1 AND id=$2 `+lock, p.WorkspaceID, id).Scan(&o.Name, &o.Key, &o.Visibility, &o.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, authz.ErrNotFound
	}
	if err != nil {
		return o, err
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM radishnexus.project_memberships WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3 FOR SHARE`, p.WorkspaceID, id, p.ID).Scan(&role)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return o, err
	}
	if o.Visibility == "restricted" && role == "" {
		return o, authz.ErrNotFound
	}
	o.CanManage = role == "admin" && o.Status == "active"
	if write && role != "admin" {
		return o, authz.ErrForbidden
	}
	if write && o.Status != "active" {
		return o, authz.ErrConflict
	}
	return o, nil
}

func configurationChannel(ctx context.Context, tx pgx.Tx, p authz.Principal, id string, write bool) (goldenpath.ConfigurationObject, error) {
	o := goldenpath.ConfigurationObject{ID: id, Kind: "channel"}
	err := tx.QueryRow(ctx, `SELECT name,governing_project_id,visibility,status FROM radishnexus.channels WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, id).Scan(&o.Name, &o.ProjectID, &o.Visibility, &o.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, authz.ErrNotFound
	}
	if err != nil {
		return o, err
	}
	project, err := configurationProject(ctx, tx, p, o.ProjectID, false)
	if err != nil {
		return o, err
	}
	if o.Visibility == "restricted" {
		var member bool
		err = tx.QueryRow(ctx, `SELECT true FROM radishnexus.channel_memberships WHERE workspace_id=$1 AND channel_id=$2 AND user_id=$3 FOR SHARE`, p.WorkspaceID, id, p.ID).Scan(&member)
		if errors.Is(err, pgx.ErrNoRows) {
			return o, authz.ErrNotFound
		}
		if err != nil {
			return o, err
		}
	}
	o.CanManage = project.CanManage && o.Status == "active" && o.Visibility == "restricted"
	if write {
		if !project.CanManage && project.Status == "active" {
			return o, authz.ErrForbidden
		}
		if project.Status != "active" || o.Status != "active" {
			return o, authz.ErrConflict
		}
		if o.Visibility != "restricted" {
			return o, authz.ErrInvalid
		}
	}
	return o, nil
}

func (s *Store) Configure(ctx context.Context, c goldenpath.ConfigurationCommand) (result goldenpath.ConfigurationResult, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer rollback(ctx, tx, &err)
	owner, err := configurationActor(ctx, tx, c.Principal)
	if err != nil {
		return result, err
	}
	projectID := ""
	if c.Kind == "team.create" || c.Kind == "project.create" {
		if !owner {
			return result, authz.ErrForbidden
		}
		// Lock the workspace once for creation and unique receipt resolution.
		if _, err = tx.Exec(ctx, `SELECT id FROM radishnexus.workspaces WHERE id=$1 FOR UPDATE`, c.Principal.WorkspaceID); err != nil {
			return result, err
		}
	} else {
		projectID = c.ScopeID
		if strings.HasPrefix(c.Kind, "channel.member.") {
			err = tx.QueryRow(ctx, `SELECT governing_project_id FROM radishnexus.channels WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, c.ScopeID).Scan(&projectID)
			if errors.Is(err, pgx.ErrNoRows) {
				return result, authz.ErrNotFound
			}
			if err != nil {
				return result, err
			}
		}
		// Acquire the exclusive project lock before reading membership to avoid a
		// shared-to-exclusive upgrade deadlock between concurrent configurations.
		if _, err = configurationProject(ctx, tx, c.Principal, projectID, true); err != nil {
			return result, err
		}
		if strings.HasPrefix(c.Kind, "channel.member.") {
			if _, err = configurationChannel(ctx, tx, c.Principal, c.ScopeID, true); err != nil {
				return result, err
			}
		}
	}
	var digest, resultID string
	err = tx.QueryRow(ctx, `SELECT r.payload_sha256,a.result_id FROM radishnexus.workspace_configuration_receipts r JOIN radishnexus.workspace_configuration_audit a ON a.workspace_id=r.workspace_id AND a.id=r.audit_id WHERE r.workspace_id=$1 AND r.actor_id=$2 AND r.command_kind=$3 AND r.scope_id=$4 AND r.subject_id=$5 AND r.client_operation_id=$6`, c.Principal.WorkspaceID, c.Principal.ID, c.Kind, c.ScopeID, c.UserID, c.ClientOperationID).Scan(&digest, &resultID)
	if err == nil {
		if digest != c.PayloadSHA256 {
			return result, authz.ErrConflict
		}
		switch c.Kind {
		case "team.create":
			result.Object = goldenpath.ConfigurationObject{ID: resultID, Kind: "team"}
			err = tx.QueryRow(ctx, `SELECT name FROM radishnexus.teams WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, resultID).Scan(&result.Object.Name)
		case "project.create":
			result.Object, err = configurationProject(ctx, tx, c.Principal, resultID, false)
		case "channel.create":
			result.Object, err = configurationChannel(ctx, tx, c.Principal, resultID, false)
		default:
			result.UserID = resultID
		}
		if err != nil {
			return result, err
		}
		if result.Object.Status == "archived" {
			return result, authz.ErrConflict
		}
		err = tx.Commit(ctx)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	before, after := "", ""
	changed := true
	removedChannels, removedThreads := []string{}, []string{}
	grantedUsers := []string{}
	switch c.Kind {
	case "team.create":
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.teams(id,workspace_id,name,created_at) VALUES($1,$2,$3,$4)`, c.ID, c.Principal.WorkspaceID, c.Name, c.OccurredAt)
		result.Object = goldenpath.ConfigurationObject{ID: c.ID, Kind: "team", Name: c.Name}
	case "project.create":
		var team string
		err = tx.QueryRow(ctx, `SELECT id FROM radishnexus.teams WHERE workspace_id=$1 AND id=$2 FOR SHARE`, c.Principal.WorkspaceID, c.OwnerTeamID).Scan(&team)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, authz.ErrNotFound
		}
		if err != nil {
			return result, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'active','user',$7,$8,$8)`, c.ID, c.Principal.WorkspaceID, c.Key, c.Name, c.OwnerTeamID, c.Visibility, c.Principal.ID, c.OccurredAt)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role,created_at) VALUES($1,$2,$3,'admin',$4)`, c.Principal.WorkspaceID, c.ID, c.Principal.ID, c.OccurredAt)
		}
		projectID = c.ID
		after = "admin"
		grantedUsers = []string{c.Principal.ID}
		result.Object = goldenpath.ConfigurationObject{ID: c.ID, Kind: "project", Name: c.Name, Key: c.Key, Visibility: c.Visibility, Status: "active", CanManage: true}
	case "channel.create":
		grantedUsers = append(grantedUsers, c.MemberUserIDs...)
		for _, id := range c.MemberUserIDs {
			if err = eligibleConfigurationMember(ctx, tx, c.Principal.WorkspaceID, projectID, id); err != nil {
				return result, err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.channels(id,workspace_id,governing_project_id,name,visibility,status,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'active','user',$6,$7,$7)`, c.ID, c.Principal.WorkspaceID, projectID, c.Name, c.Visibility, c.Principal.ID, c.OccurredAt)
		if err == nil {
			for _, id := range c.MemberUserIDs {
				_, err = tx.Exec(ctx, `INSERT INTO radishnexus.channel_memberships(workspace_id,channel_id,user_id,created_at) VALUES($1,$2,$3,$4)`, c.Principal.WorkspaceID, c.ID, id, c.OccurredAt)
				if err != nil {
					break
				}
			}
		}
		result.Object = goldenpath.ConfigurationObject{ID: c.ID, Kind: "channel", Name: c.Name, ProjectID: projectID, Visibility: c.Visibility, Status: "active", CanManage: c.Visibility == "restricted"}
	default:
		before, after, removedChannels, removedThreads, err = configureMember(ctx, tx, c, projectID)
		changed = before != after || len(removedChannels) > 0 || len(removedThreads) > 0
		result.UserID = c.UserID
	}
	if err != nil {
		return result, mapDatabaseError("configure workspace", err)
	}
	resultID = c.ID
	if resultID == "" {
		resultID = c.UserID
	}
	_, err = tx.Exec(ctx, `INSERT INTO radishnexus.workspace_configuration_audit(id,workspace_id,actor_id,command_kind,scope_id,subject_id,result_id,before_state,after_state,changed,removed_channel_ids,removed_thread_ids,request_id,occurred_at,granted_user_ids) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, c.AuditID, c.Principal.WorkspaceID, c.Principal.ID, c.Kind, c.ScopeID, c.UserID, resultID, before, after, changed, removedChannels, removedThreads, c.CorrelationID, c.OccurredAt, grantedUsers)
	if err != nil {
		return result, fmt.Errorf("record configuration audit: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO radishnexus.workspace_configuration_receipts(workspace_id,actor_id,command_kind,scope_id,subject_id,client_operation_id,payload_sha256,audit_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, c.Principal.WorkspaceID, c.Principal.ID, c.Kind, c.ScopeID, c.UserID, c.ClientOperationID, c.PayloadSHA256, c.AuditID, c.OccurredAt)
	if err != nil {
		return result, fmt.Errorf("record configuration receipt: %w", err)
	}
	if c.EventID != "" {
		err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: result.Object.Kind + ".created", WorkspaceID: c.Principal.WorkspaceID, ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, PrimaryType: result.Object.Kind, PrimaryID: c.ID, ProjectID: projectID, CorrelationID: c.CorrelationID, OccurredAt: c.OccurredAt, Payload: map[string]any{"status": "active"}})
		if err != nil {
			return result, err
		}
		if err = insertOutbox(ctx, tx, c.EventID); err != nil {
			return result, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return result, mapDatabaseError("commit configuration", err)
	}
	result.Created = c.ID != ""
	return result, nil
}

func eligibleConfigurationMember(ctx context.Context, tx pgx.Tx, workspace, project, user string) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id=$1 FOR SHARE`, user).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return authz.ErrNotFound
	}
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT status FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, workspace, user).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return authz.ErrNotFound
	}
	if err != nil {
		return err
	}
	if project != "" {
		var role string
		err = tx.QueryRow(ctx, `SELECT role FROM radishnexus.project_memberships WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3 FOR SHARE`, workspace, project, user).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return authz.ErrNotFound
		}
	}
	return err
}

func configureMember(ctx context.Context, tx pgx.Tx, c goldenpath.ConfigurationCommand, project string) (before, after string, channels, threads []string, err error) {
	channels, threads = []string{}, []string{}
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM radishnexus.project_memberships WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3 FOR UPDATE`, c.Principal.WorkspaceID, project, c.UserID).Scan(&role)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return
	}
	err = nil
	if (role == "admin" || c.UserID == c.Principal.ID) && c.Kind != "channel.member.add" {
		err = authz.ErrForbidden
		return
	}
	projectCommand := strings.HasPrefix(c.Kind, "project.")
	if projectCommand {
		before = role
		expected := ""
		if c.ExpectedRole != nil {
			expected = *c.ExpectedRole
		}
		if expected != before {
			err = authz.ErrConflict
			return
		}
		if c.Kind == "project.member.set" {
			if err = eligibleConfigurationMember(ctx, tx, c.Principal.WorkspaceID, "", c.UserID); err != nil {
				return
			}
			after = c.Role
			_, err = tx.Exec(ctx, `INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role,created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(workspace_id,project_id,user_id) DO UPDATE SET role=EXCLUDED.role`, c.Principal.WorkspaceID, project, c.UserID, c.Role, c.OccurredAt)
			return
		}
	} else {
		var member bool
		err = tx.QueryRow(ctx, `SELECT true FROM radishnexus.channel_memberships WHERE workspace_id=$1 AND channel_id=$2 AND user_id=$3 FOR UPDATE`, c.Principal.WorkspaceID, c.ScopeID, c.UserID).Scan(&member)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return
		}
		err = nil
		if member {
			before = "member"
		}
		if member != c.ExpectedMember {
			err = authz.ErrConflict
			return
		}
		if c.Kind == "channel.member.add" {
			if err = eligibleConfigurationMember(ctx, tx, c.Principal.WorkspaceID, project, c.UserID); err != nil {
				return
			}
			after = "member"
			_, err = tx.Exec(ctx, `INSERT INTO radishnexus.channel_memberships(workspace_id,channel_id,user_id,created_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, c.Principal.WorkspaceID, c.ScopeID, c.UserID, c.OccurredAt)
			return
		}
	}
	rows, queryErr := tx.Query(ctx, `DELETE FROM radishnexus.thread_memberships m USING radishnexus.threads t WHERE m.workspace_id=$1 AND m.user_id=$2 AND t.workspace_id=m.workspace_id AND t.id=m.thread_id AND t.governing_project_id=$3 AND ($4='' OR t.origin_channel_id=$4) RETURNING m.thread_id`, c.Principal.WorkspaceID, c.UserID, project, func() string {
		if projectCommand {
			return ""
		}
		return c.ScopeID
	}())
	if queryErr != nil {
		err = queryErr
		return
	}
	threads, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return
	}
	rows, err = tx.Query(ctx, `DELETE FROM radishnexus.channel_memberships m USING radishnexus.channels ch WHERE m.workspace_id=$1 AND m.user_id=$2 AND ch.workspace_id=m.workspace_id AND ch.id=m.channel_id AND ch.governing_project_id=$3 AND ($4='' OR ch.id=$4) RETURNING m.channel_id`, c.Principal.WorkspaceID, c.UserID, project, func() string {
		if projectCommand {
			return ""
		}
		return c.ScopeID
	}())
	if err != nil {
		return
	}
	channels, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return
	}
	if projectCommand {
		_, err = tx.Exec(ctx, `DELETE FROM radishnexus.project_memberships WHERE workspace_id=$1 AND project_id=$2 AND user_id=$3`, c.Principal.WorkspaceID, project, c.UserID)
	}
	return
}
