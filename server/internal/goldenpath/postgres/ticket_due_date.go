package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/calendardate"
)

func (s *Store) ReadTicketDueDate(ctx context.Context, p authz.Principal, id string) (out goldenpath.TicketDueDate, err error) {
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
	if err = tx.QueryRow(ctx, `SELECT to_char(due_date::timestamp, 'YYYY-MM-DD'),due_date_revision,status_revision,status FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, id).Scan(&out.DueDate, &out.DueDateRevision, &out.StatusRevision, &status); err != nil {
		return out, err
	}
	if !calendardate.Valid(out.DueDate) || !goldenpath.ValidTicketStatusRevision(out.DueDateRevision) || !goldenpath.ValidTicketStatusRevision(out.StatusRevision) || !goldenpath.ValidTicketStatus(status) {
		return out, errors.New("invalid stored Ticket due date state")
	}
	out.CanSetDueDate = access.active && authz.RequireContribute(access.role) == nil && (status == "open" || status == "in-progress") && out.DueDateRevision < goldenpath.MaxTicketStatusRevision
	err = tx.Commit(ctx)
	return
}

func (s *Store) SetTicketDueDate(ctx context.Context, c goldenpath.TicketDueDateCommand) (out goldenpath.TicketDueDateResult, err error) {
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
	var current *string
	var rev, statusRev int
	var status string
	var updated time.Time
	if err = tx.QueryRow(ctx, `SELECT to_char(due_date::timestamp, 'YYYY-MM-DD'),due_date_revision,status_revision,status,updated_at FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Principal.WorkspaceID, c.TicketID).Scan(&current, &rev, &statusRev, &status, &updated); err != nil {
		return out, err
	}
	if !calendardate.Valid(current) || !goldenpath.ValidTicketStatusRevision(rev) || !goldenpath.ValidTicketStatusRevision(statusRev) || !goldenpath.ValidTicketStatus(status) {
		return out, errors.New("invalid stored Ticket due date state")
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return out, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if !now.After(updated) {
		now = updated.Add(time.Microsecond)
	}
	claim := c.BaseDueDateRevision
	if claim < goldenpath.MaxTicketStatusRevision {
		claim++
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, goldenpath.TicketDueDateCommandKind, "ticket", c.TicketID, c.ClientOperationID, c.PayloadSHA256, "ticket", c.TicketID, c.EventID, now, claim)
	if err != nil {
		return out, err
	}
	if duplicate {
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT e.payload FROM radishnexus.domain_events e JOIN radishnexus.collaboration_command_receipts r ON r.workspace_id=e.workspace_id AND r.event_id=e.event_id WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.event_type='ticket.due-date-changed' AND e.primary_entity_type='ticket' AND e.primary_entity_id=$3 AND e.actor_id=$4 AND e.actor_kind='user' AND e.source_kind='web' AND e.schema_version=1 AND e.project_id=$5 AND r.command_kind='ticket.due-date.set' AND r.target_type='ticket' AND r.target_id=$3 AND r.actor_id=$4 AND r.client_operation_id=$6 AND e.occurred_at=r.created_at`, c.Principal.WorkspaceID, receipt.eventID, c.TicketID, c.Principal.ID, project, c.ClientOperationID).Scan(&payload)
		if err != nil {
			return out, err
		}
		change, e := cp.DecodeDueDateChange(payload)
		if e != nil || receipt.resultID != c.TicketID || receipt.resultRevision == nil || change.ToRevision != *receipt.resultRevision || change.ToRevision > rev || change.FromRevision != c.BaseDueDateRevision || !calendardate.Equal(change.To, c.DueDate) {
			return out, errors.New("invalid due date receipt provenance")
		}
		out = goldenpath.TicketDueDateResult{Applied: true, DueDateRevision: change.ToRevision, DueDate: change.To}
		err = tx.Commit(ctx)
		return
	}
	if rev != c.BaseDueDateRevision || statusRev != c.BaseStatusRevision || (status != "open" && status != "in-progress") || rev == goldenpath.MaxTicketStatusRevision || calendardate.Equal(current, c.DueDate) {
		return out, authz.ErrConflict
	}
	change := cp.DueDateChange{From: current, To: c.DueDate, FromRevision: rev, ToRevision: rev + 1}
	if _, err = tx.Exec(ctx, `UPDATE radishnexus.tickets SET due_date=$3::text::date,due_date_revision=$4,updated_at=$5 WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, c.TicketID, c.DueDate, rev+1, now); err != nil {
		return out, err
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: goldenpath.TicketDueDateChanged, WorkspaceID: c.Principal.WorkspaceID, ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "ticket", PrimaryID: c.TicketID, ProjectID: project, CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: now, Payload: map[string]any{"due_date": change}}); err != nil {
		return out, err
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, mapDatabaseError("commit Ticket due date", err)
	}
	return goldenpath.TicketDueDateResult{Applied: true, DueDateRevision: rev + 1, DueDate: c.DueDate}, nil
}
