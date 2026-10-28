package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (s *Store) ReadTicketPriority(ctx context.Context, p authz.Principal, id string) (out goldenpath.TicketPriority, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	_, access, err := ticketProjectAccess(ctx, tx, p, id)
	if err != nil {
		return out, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT priority,priority_revision,status_revision,status FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, id).Scan(&out.Priority, &out.PriorityRevision, &out.StatusRevision, &status); err != nil {
		return out, err
	}
	if !goldenpath.ValidTicketPriority(out.Priority) || !goldenpath.ValidTicketStatusRevision(out.PriorityRevision) || !goldenpath.ValidTicketStatusRevision(out.StatusRevision) || !goldenpath.ValidTicketStatus(status) {
		return out, errors.New("invalid stored Ticket priority state")
	}
	out.CanSetPriority = access.active && authz.RequireContribute(access.role) == nil && (status == "open" || status == "in-progress") && out.PriorityRevision < goldenpath.MaxTicketStatusRevision
	err = tx.Commit(ctx)
	return
}

func (s *Store) SetTicketPriority(ctx context.Context, c goldenpath.TicketPriorityCommand) (out goldenpath.TicketPriorityResult, err error) {
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	project, access, err := ticketProjectAccess(ctx, tx, c.Principal, c.TicketID)
	if err != nil {
		return out, err
	}
	if err = authz.RequireContribute(access.role); err != nil {
		return out, err
	}
	if !access.active {
		return out, authz.ErrConflict
	}
	var current string
	var rev, statusRev int
	var status string
	var updated time.Time
	if err = tx.QueryRow(ctx, `SELECT priority,priority_revision,status_revision,status,updated_at FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Principal.WorkspaceID, c.TicketID).Scan(&current, &rev, &statusRev, &status, &updated); err != nil {
		return out, err
	}
	if !goldenpath.ValidTicketPriority(current) || !goldenpath.ValidTicketStatusRevision(rev) || !goldenpath.ValidTicketStatusRevision(statusRev) || !goldenpath.ValidTicketStatus(status) {
		return out, errors.New("invalid stored Ticket priority state")
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return out, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if !now.After(updated) {
		now = updated.Add(time.Microsecond)
	}
	claim := c.BasePriorityRevision
	if claim < goldenpath.MaxTicketStatusRevision {
		claim++
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, goldenpath.TicketPriorityCommandKind, "ticket", c.TicketID, c.ClientOperationID, c.PayloadSHA256, "ticket", c.TicketID, c.EventID, now, claim)
	if err != nil {
		return out, err
	}
	if duplicate {
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT e.payload FROM radishnexus.domain_events e JOIN radishnexus.collaboration_command_receipts r ON r.workspace_id=e.workspace_id AND r.event_id=e.event_id WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.event_type='ticket.priority-changed' AND e.primary_entity_type='ticket' AND e.primary_entity_id=$3 AND e.actor_id=$4 AND e.actor_kind='user' AND e.source_kind='web' AND e.schema_version=1 AND e.project_id=$5 AND r.command_kind='ticket.priority.set' AND r.target_type='ticket' AND r.target_id=$3 AND r.actor_id=$4 AND r.client_operation_id=$6 AND e.occurred_at=r.created_at`, c.Principal.WorkspaceID, receipt.eventID, c.TicketID, c.Principal.ID, project, c.ClientOperationID).Scan(&payload)
		if err != nil {
			return out, err
		}
		change, e := goldenpath.DecodeTicketPriority(payload)
		if e != nil || receipt.resultID != c.TicketID || receipt.resultRevision == nil || change.ToRevision != *receipt.resultRevision || change.ToRevision > rev || change.FromRevision != c.BasePriorityRevision || change.To != c.Priority {
			return out, errors.New("invalid priority receipt provenance")
		}
		out = goldenpath.TicketPriorityResult{Applied: true, PriorityRevision: change.ToRevision, Priority: change.To}
		err = tx.Commit(ctx)
		return
	}
	if rev != c.BasePriorityRevision || statusRev != c.BaseStatusRevision || (status != "open" && status != "in-progress") || rev == goldenpath.MaxTicketStatusRevision || current == c.Priority {
		return out, authz.ErrConflict
	}
	change := goldenpath.TicketPriorityChange{From: current, To: c.Priority, FromRevision: rev, ToRevision: rev + 1}
	if _, err = tx.Exec(ctx, `UPDATE radishnexus.tickets SET priority=$3,priority_revision=$4,updated_at=$5 WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, c.TicketID, c.Priority, rev+1, now); err != nil {
		return out, err
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: "ticket.priority-changed", WorkspaceID: c.Principal.WorkspaceID, ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "ticket", PrimaryID: c.TicketID, ProjectID: project, CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: now, Payload: map[string]any{"priority": change}}); err != nil {
		return out, err
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, mapDatabaseError("commit Ticket priority", err)
	}
	return goldenpath.TicketPriorityResult{Applied: true, PriorityRevision: rev + 1, Priority: c.Priority}, nil
}
