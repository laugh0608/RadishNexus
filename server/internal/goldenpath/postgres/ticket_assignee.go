package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// Both discovery and writes use this exact eligibility predicate. The Workspace
// admission gate serializes membership/role changes; writes also lock accounts.
const assigneeEligibility = `a.status='active' AND m.status='active' AND pm.role IN ('contributor','decider','admin') AND NOT EXISTS(SELECT 1 FROM radishnexus.context_import_mappings im WHERE im.kind='actor' AND im.target_id=u.id)`

func readTicketAssignee(ctx context.Context, tx pgx.Tx, w, project, id string, active bool) (*goldenpath.TicketAssignee, error) {
	if id == "" {
		return nil, nil
	}
	var v goldenpath.TicketAssignee
	err := tx.QueryRow(ctx, `SELECT u.id,u.display_name,COALESCE(`+assigneeEligibility+`,false) AND $4,EXISTS(SELECT 1 FROM radishnexus.context_import_mappings im WHERE im.kind='actor' AND im.target_id=u.id) FROM radishnexus.users u JOIN radishnexus.workspace_memberships m ON m.user_id=u.id AND m.workspace_id=$1 JOIN radishnexus.user_accounts a ON a.user_id=u.id LEFT JOIN radishnexus.project_memberships pm ON pm.workspace_id=$1 AND pm.project_id=$2 AND pm.user_id=u.id WHERE u.id=$3`, w, project, id, active).Scan(&v.ID, &v.DisplayName, &v.Eligible, &v.Imported)
	if err != nil {
		return nil, fmt.Errorf("read Ticket assignee: %w", err)
	}
	return &v, nil
}
func assignmentScope(ctx context.Context, tx pgx.Tx, p authz.Principal, id string) (string, projectAccess, error) {
	project, err := projectEntityScope(ctx, tx, p, "ticket", id)
	if err != nil {
		return "", projectAccess{}, err
	}
	access, readable, err := readProjectAccess(ctx, tx, p, project)
	if err != nil {
		return "", access, err
	}
	if !readable {
		return "", access, authz.ErrNotFound
	}
	return project, access, nil
}
func (s *Store) ReadTicketAssignment(ctx context.Context, p authz.Principal, id string) (out goldenpath.TicketAssignment, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	project, access, err := assignmentScope(ctx, tx, p, id)
	if err != nil {
		return out, err
	}
	var assignee *string
	var status string
	if err = tx.QueryRow(ctx, `SELECT assignee_user_id,assignment_revision,status_revision,status FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, id).Scan(&assignee, &out.AssignmentRevision, &out.StatusRevision, &status); err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, `SELECT management_revision::text FROM radishnexus.projects WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, project).Scan(&out.ProjectManagementRevision); err != nil {
		return out, err
	}
	if assignee != nil {
		out.Assignee, err = readTicketAssignee(ctx, tx, p.WorkspaceID, project, *assignee, access.active)
		if err != nil {
			return out, err
		}
	}
	out.CanAssign = access.active && authz.RequireContribute(access.role) == nil && (status == "open" || status == "in-progress") && out.AssignmentRevision < goldenpath.MaxTicketStatusRevision
	err = tx.Commit(ctx)
	return
}
func (s *Store) ListAssigneeCandidates(ctx context.Context, p authz.Principal, id string, q goldenpath.DiscoveryPageInput) (out goldenpath.AssigneeCandidates, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	project, access, err := assignmentScope(ctx, tx, p, id)
	if err != nil {
		return out, err
	}
	if err = authz.RequireContribute(access.role); err != nil {
		return out, err
	}
	var status string
	var revision int
	if err = tx.QueryRow(ctx, `SELECT status,assignment_revision FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, id).Scan(&status, &revision); err != nil {
		return out, err
	}
	if !access.active || (status != "open" && status != "in-progress") || revision == goldenpath.MaxTicketStatusRevision {
		return out, authz.ErrConflict
	}
	if err = tx.QueryRow(ctx, `SELECT management_revision::text FROM radishnexus.projects WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, project).Scan(&out.ProjectManagementRevision); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT u.id,u.display_name,m.lifecycle_revision::text FROM radishnexus.users u JOIN radishnexus.workspace_memberships m ON m.user_id=u.id AND m.workspace_id=$1 JOIN radishnexus.user_accounts a ON a.user_id=u.id JOIN radishnexus.project_memberships pm ON pm.workspace_id=$1 AND pm.project_id=$2 AND pm.user_id=u.id WHERE `+assigneeEligibility+` AND u.id>$3 ORDER BY u.id LIMIT $4`, p.WorkspaceID, project, q.AfterID, q.Limit+1)
	if err != nil {
		return out, err
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (v goldenpath.AssigneeCandidate, e error) {
		e = r.Scan(&v.ID, &v.DisplayName, &v.LifecycleRevision)
		return
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > q.Limit {
		out.Items = out.Items[:q.Limit]
		out.NextID = out.Items[q.Limit-1].ID
	}
	out.SelfID = p.ID
	err = tx.Commit(ctx)
	return
}
func (s *Store) SetTicketAssignee(ctx context.Context, c goldenpath.TicketAssignmentCommand) (out goldenpath.TicketAssignmentResult, err error) {
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	project, access, err := assignmentScope(ctx, tx, c.Principal, c.TicketID)
	if err != nil {
		return out, err
	}
	if err = authz.RequireContribute(access.role); err != nil {
		return out, err
	}
	if !access.active {
		return out, authz.ErrConflict
	}
	var current *string
	var rev, statusRev int
	var status string
	var updated time.Time
	if err = tx.QueryRow(ctx, `SELECT assignee_user_id,assignment_revision,status_revision,status,updated_at FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Principal.WorkspaceID, c.TicketID).Scan(&current, &rev, &statusRev, &status, &updated); err != nil {
		return out, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return out, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if !now.After(updated) {
		now = updated.Add(time.Microsecond)
	}
	claim := c.BaseAssignmentRevision
	if claim < goldenpath.MaxTicketStatusRevision {
		claim++
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, goldenpath.TicketAssigneeCommandKind, "ticket", c.TicketID, c.ClientOperationID, c.PayloadSHA256, "ticket", c.TicketID, c.EventID, now, claim)
	if err != nil {
		return out, err
	}
	if duplicate {
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT e.payload FROM radishnexus.domain_events e JOIN radishnexus.collaboration_command_receipts r ON r.workspace_id=e.workspace_id AND r.event_id=e.event_id WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.event_type='ticket.assignee-changed' AND e.primary_entity_type='ticket' AND e.primary_entity_id=$3 AND e.actor_id=$4 AND e.actor_kind='user' AND e.source_kind='web' AND e.schema_version=1 AND e.project_id=$5 AND r.command_kind='ticket.assignee.set' AND r.target_id=$3 AND r.actor_id=$4 AND r.client_operation_id=$6 AND e.occurred_at=r.created_at`, c.Principal.WorkspaceID, receipt.eventID, c.TicketID, c.Principal.ID, project, c.ClientOperationID).Scan(&payload)
		if err != nil {
			return out, err
		}
		change, e := goldenpath.DecodeTicketAssignment(payload)
		if e != nil || receipt.resultID != c.TicketID || receipt.resultRevision == nil || change.ToRevision != *receipt.resultRevision || change.ToRevision > rev || change.FromRevision != c.BaseAssignmentRevision || !goldenpath.SameAssignee(change.ToUserID, c.AssigneeUserID) {
			return out, errors.New("invalid assignment receipt provenance")
		}
		out = goldenpath.TicketAssignmentResult{Applied: true, AssignmentRevision: change.ToRevision, AssigneeUserID: change.ToUserID}
		err = tx.Commit(ctx)
		return
	}
	if rev != c.BaseAssignmentRevision || statusRev != c.BaseStatusRevision || (status != "open" && status != "in-progress") || rev == goldenpath.MaxTicketStatusRevision || goldenpath.SameAssignee(current, c.AssigneeUserID) {
		return out, authz.ErrConflict
	}
	var projectRevision string
	if err = tx.QueryRow(ctx, `SELECT management_revision::text FROM radishnexus.projects WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, project).Scan(&projectRevision); err != nil {
		return out, err
	}
	if projectRevision != c.ProjectManagementRevision {
		return out, authz.ErrConflict
	}
	if c.AssigneeUserID != nil {
		var account string
		err = tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id=$1 FOR SHARE`, *c.AssigneeUserID).Scan(&account)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, authz.ErrConflict
		}
		if err != nil {
			return out, err
		}
		if account != "active" {
			return out, authz.ErrConflict
		}
		var lifecycle int64
		err = tx.QueryRow(ctx, `SELECT lifecycle_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, c.Principal.WorkspaceID, *c.AssigneeUserID).Scan(&lifecycle)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, authz.ErrConflict
		}
		if err != nil {
			return out, err
		}
		candidate, e := readTicketAssignee(ctx, tx, c.Principal.WorkspaceID, project, *c.AssigneeUserID, true)
		if e != nil {
			return out, e
		}
		if !candidate.Eligible || strconv.FormatInt(lifecycle, 10) != c.TargetLifecycleRevision {
			return out, authz.ErrConflict
		}
	}
	change := goldenpath.TicketAssignmentChange{FromUserID: current, ToUserID: c.AssigneeUserID, FromRevision: rev, ToRevision: rev + 1}
	if _, err = tx.Exec(ctx, `UPDATE radishnexus.tickets SET assignee_user_id=$3,assignment_revision=$4,updated_at=$5 WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, c.TicketID, c.AssigneeUserID, rev+1, now); err != nil {
		return out, err
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: "ticket.assignee-changed", WorkspaceID: c.Principal.WorkspaceID, ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "ticket", PrimaryID: c.TicketID, ProjectID: project, CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: now, Payload: map[string]any{"assignment": change}}); err != nil {
		return out, err
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, mapDatabaseError("commit Ticket assignment", err)
	}
	return goldenpath.TicketAssignmentResult{Applied: true, AssignmentRevision: rev + 1, AssigneeUserID: c.AssigneeUserID}, nil
}

func assignmentFactString(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}
