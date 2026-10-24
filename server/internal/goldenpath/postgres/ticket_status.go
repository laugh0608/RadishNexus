package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (s *Store) ChangeTicketStatus(ctx context.Context, c goldenpath.TicketStatusCommand) (result goldenpath.TicketStatusResult, err error) {
	if err = c.Principal.ValidateUser(); err != nil {
		return result, err
	}
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return result, fmt.Errorf("begin Ticket status change: %w", err)
	}
	defer rollback(ctx, tx, &err)
	if _, err = configurationActor(ctx, tx, c.Principal); err != nil {
		return result, err
	}
	project, err := projectEntityScope(ctx, tx, c.Principal, "ticket", c.TicketID)
	if err != nil {
		return result, err
	}
	access, readable, err := readProjectAccess(ctx, tx, c.Principal, project)
	if err != nil {
		return result, err
	}
	if !readable {
		return result, authz.ErrNotFound
	}
	if err = authz.RequireContribute(access.role); err != nil {
		return result, err
	}
	if !access.active {
		return result, authz.ErrConflict
	}
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Principal.WorkspaceID, c.TicketID).Scan(&locked); err != nil {
		return result, fmt.Errorf("lock Ticket status: %w", err)
	}
	ticket, err := loadTicket(ctx, tx, c.Principal.WorkspaceID, c.TicketID)
	if err != nil {
		return result, err
	}
	if !goldenpath.ValidTicketStatus(ticket.Status) || !goldenpath.ValidTicketStatusRevision(ticket.StatusRevision) {
		return result, errors.New("invalid stored Ticket state")
	}
	// Take the authoritative time only after the row lock. Round to the stored
	// precision, and keep each transition later than this Ticket's prior state.
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return result, fmt.Errorf("read Ticket transition time: %w", err)
	}
	now = now.UTC().Truncate(time.Microsecond)
	if !now.After(ticket.UpdatedAt) {
		now = ticket.UpdatedAt.Add(time.Microsecond)
	}
	claimRevision := c.BaseStatusRevision
	if claimRevision < goldenpath.MaxTicketStatusRevision {
		claimRevision++
	}
	// A claim for a new invalid base/edge is rolled back below. Capping this
	// provisional value lets exact retry lookup precede the overflow check.
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, goldenpath.TicketStatusCommandKind, "ticket", c.TicketID,
		c.ClientOperationID, c.PayloadSHA256, "ticket", c.TicketID, c.EventID, now, claimRevision)
	if err != nil {
		return result, err
	}
	if duplicate {
		result, err = ticketStatusReceiptResult(ctx, tx, c, receipt, ticket)
		if err != nil {
			return result, err
		}
		if err = tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("commit Ticket status retry: %w", err)
		}
		return result, nil
	}
	transition := goldenpath.TicketStatusTransition{FromStatus: ticket.Status, ToStatus: c.ToStatus, FromRevision: ticket.StatusRevision, ToRevision: claimRevision}
	if c.BaseStatusRevision != ticket.StatusRevision || !transition.Valid() {
		return result, authz.ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE radishnexus.tickets SET status=$3,status_revision=$4,updated_at=$5 WHERE workspace_id=$1 AND id=$2`,
		c.Principal.WorkspaceID, c.TicketID, c.ToStatus, transition.ToRevision, now); err != nil {
		return result, fmt.Errorf("update Ticket status: %w", err)
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: "ticket.status-changed", WorkspaceID: c.Principal.WorkspaceID,
		ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "ticket", PrimaryID: c.TicketID,
		ProjectID: project, CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: now,
		Payload: map[string]any{"from_status": transition.FromStatus, "to_status": transition.ToStatus, "from_status_revision": transition.FromRevision, "to_status_revision": transition.ToRevision}}); err != nil {
		return result, err
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, mapDatabaseError("commit Ticket status change", err)
	}
	return goldenpath.TicketStatusResult{TicketID: c.TicketID, AppliedStatus: c.ToStatus, AppliedStatusRevision: transition.ToRevision}, nil
}

func ticketStatusReceiptResult(ctx context.Context, tx pgx.Tx, c goldenpath.TicketStatusCommand, receipt collaborationCommandReceipt, ticket goldenpath.Ticket) (goldenpath.TicketStatusResult, error) {
	invalid := errors.New("invalid Ticket status receipt provenance")
	if receipt.resultID != c.TicketID || receipt.resultRevision == nil {
		return goldenpath.TicketStatusResult{}, invalid
	}
	var payload []byte
	err := tx.QueryRow(ctx, `SELECT e.payload FROM radishnexus.domain_events e
        JOIN radishnexus.collaboration_command_receipts r ON r.workspace_id=e.workspace_id AND r.event_id=e.event_id
        WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.primary_entity_type='ticket' AND e.primary_entity_id=$3
        AND e.event_type='ticket.status-changed' AND e.schema_version=1 AND e.project_id=$4
        AND e.actor_kind='user' AND e.actor_id=$5 AND e.source_kind='web'
        AND r.command_kind='ticket.status.change' AND r.target_type='ticket' AND r.target_id=$3
        AND r.actor_id=$5 AND r.client_operation_id=$6 AND e.occurred_at=r.created_at`,
		c.Principal.WorkspaceID, receipt.eventID, c.TicketID, ticket.GoverningProjectID, c.Principal.ID, c.ClientOperationID).Scan(&payload)
	if err != nil {
		return goldenpath.TicketStatusResult{}, fmt.Errorf("read Ticket status receipt event: %w", err)
	}
	transition, err := decodeTicketStatusTransition(payload)
	if err != nil || transition.FromRevision != c.BaseStatusRevision || transition.ToStatus != c.ToStatus || transition.ToRevision != *receipt.resultRevision || transition.ToRevision > ticket.StatusRevision {
		return goldenpath.TicketStatusResult{}, invalid
	}
	return goldenpath.TicketStatusResult{TicketID: c.TicketID, AppliedStatus: transition.ToStatus, AppliedStatusRevision: transition.ToRevision}, nil
}

func decodeTicketStatusTransition(payload []byte) (goldenpath.TicketStatusTransition, error) {
	var shape map[string]json.RawMessage
	var transition goldenpath.TicketStatusTransition
	if json.Unmarshal(payload, &shape) != nil || len(shape) != 4 || json.Unmarshal(payload, &transition) != nil || !transition.Valid() {
		return transition, errors.New("invalid Ticket status event facts")
	}
	for _, field := range []string{"from_status", "to_status", "from_status_revision", "to_status_revision"} {
		if shape[field] == nil {
			return transition, errors.New("missing Ticket status event fact")
		}
	}
	return transition, nil
}
