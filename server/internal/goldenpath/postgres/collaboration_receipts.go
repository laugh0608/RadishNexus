package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const (
	commandDecisionPropose = "decision.propose"
	commandDecisionAccept  = "decision.accept"
	commandTicketCreate    = "ticket.create"
)

type collaborationCommandReceipt struct {
	payloadSHA256 string
	resultType    string
	resultID      string
	eventID       string
}

func claimCollaborationCommand(
	ctx context.Context,
	tx pgx.Tx,
	invocation goldenpath.Invocation,
	commandKind string,
	targetType string,
	targetID string,
	clientOperationID string,
	payloadSHA256 string,
	resultType string,
	resultID string,
	eventID string,
	createdAt time.Time,
) (collaborationCommandReceipt, bool, error) {
	result, err := tx.Exec(ctx, `
		INSERT INTO radishnexus.collaboration_command_receipts (
			workspace_id, actor_id, command_kind, target_type, target_id,
			client_operation_id, payload_sha256, result_type, result_id,
			event_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (
			workspace_id, actor_id, command_kind, target_type, target_id,
			client_operation_id
		) DO NOTHING
	`, invocation.Principal.WorkspaceID, invocation.Principal.ID, commandKind,
		targetType, targetID, clientOperationID, payloadSHA256, resultType,
		resultID, eventID, createdAt)
	if err != nil {
		return collaborationCommandReceipt{}, false, mapDatabaseError("claim collaboration command", err)
	}
	if result.RowsAffected() == 1 {
		return collaborationCommandReceipt{
			payloadSHA256: payloadSHA256,
			resultType:    resultType,
			resultID:      resultID,
			eventID:       eventID,
		}, false, nil
	}

	var existing collaborationCommandReceipt
	err = tx.QueryRow(ctx, `
		SELECT payload_sha256, result_type, result_id, event_id
		FROM radishnexus.collaboration_command_receipts
		WHERE workspace_id = $1
		  AND actor_id = $2
		  AND command_kind = $3
		  AND target_type = $4
		  AND target_id = $5
		  AND client_operation_id = $6
	`, invocation.Principal.WorkspaceID, invocation.Principal.ID, commandKind,
		targetType, targetID, clientOperationID).Scan(
		&existing.payloadSHA256,
		&existing.resultType,
		&existing.resultID,
		&existing.eventID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return collaborationCommandReceipt{}, false, errors.New("concurrent collaboration command receipt winner is missing")
	}
	if err != nil {
		return collaborationCommandReceipt{}, false, fmt.Errorf("load collaboration command receipt: %w", err)
	}
	if existing.payloadSHA256 != payloadSHA256 || existing.resultType != resultType {
		return collaborationCommandReceipt{}, false, fmt.Errorf(
			"collaboration command replay changed payload: %w",
			authz.ErrConflict,
		)
	}
	return existing, true, nil
}

func loadDecision(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID string,
	decisionID string,
) (decision goldenpath.Decision, err error) {
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, governing_project_id, question,
			COALESCE(outcome, ''), COALESCE(rationale, ''), status,
			proposer_id, decider_ids, decided_at, created_at, updated_at
		FROM radishnexus.decisions
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, decisionID).Scan(
		&decision.ID,
		&decision.WorkspaceID,
		&decision.GoverningProjectID,
		&decision.Question,
		&decision.Outcome,
		&decision.Rationale,
		&decision.Status,
		&decision.ProposerID,
		&decision.DeciderIDs,
		&decision.DecidedAt,
		&decision.CreatedAt,
		&decision.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return goldenpath.Decision{}, errors.New("collaboration command receipt Decision is missing")
	}
	if err != nil {
		return goldenpath.Decision{}, fmt.Errorf("load collaboration command Decision: %w", err)
	}
	return decision, nil
}

func loadTicket(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID string,
	ticketID string,
) (ticket goldenpath.Ticket, err error) {
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, governing_project_id, title, status, created_by,
			created_at, updated_at
		FROM radishnexus.tickets
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, ticketID).Scan(
		&ticket.ID,
		&ticket.WorkspaceID,
		&ticket.GoverningProjectID,
		&ticket.Title,
		&ticket.Status,
		&ticket.CreatedBy,
		&ticket.CreatedAt,
		&ticket.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return goldenpath.Ticket{}, errors.New("collaboration command receipt Ticket is missing")
	}
	if err != nil {
		return goldenpath.Ticket{}, fmt.Errorf("load collaboration command Ticket: %w", err)
	}
	return ticket, nil
}
