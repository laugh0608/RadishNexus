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

// Acceptance and rejection share one authorization, lock and receipt boundary.
type decisionCommand struct {
	goldenpath.Invocation
	id, operation, digest, event, kind, status string
	outcome, rationale, reason                 string
}

func (s *Store) AcceptDecision(ctx context.Context, c goldenpath.AcceptDecisionCommand) (goldenpath.AcceptDecisionResult, error) {
	d, applied, err := s.decide(ctx, decisionCommand{Invocation: c.Invocation, id: c.DecisionID, operation: c.ClientOperationID,
		digest: c.PayloadSHA256, event: c.EventID, kind: commandDecisionAccept, status: "accepted", outcome: c.Outcome, rationale: c.Rationale})
	return goldenpath.AcceptDecisionResult{Decision: d, Accepted: applied}, err
}

func (s *Store) RejectDecision(ctx context.Context, c goldenpath.RejectDecisionCommand) (goldenpath.RejectDecisionResult, error) {
	d, applied, err := s.decide(ctx, decisionCommand{Invocation: c.Invocation, id: c.DecisionID, operation: c.ClientOperationID,
		digest: c.PayloadSHA256, event: c.EventID, kind: commandDecisionReject, status: "rejected", reason: c.RejectionReason})
	return goldenpath.RejectDecisionResult{Decision: d, Rejected: applied}, err
}

func decisionProjectAccess(ctx context.Context, tx pgx.Tx, p authz.Principal, id string) (string, projectAccess, error) {
	if _, err := configurationActor(ctx, tx, p); err != nil {
		return "", projectAccess{}, err
	}
	project, err := projectEntityScope(ctx, tx, p, "decision", id)
	if err != nil {
		return "", projectAccess{}, err
	}
	access, readable, err := readProjectAccess(ctx, tx, p, project)
	if err != nil {
		return "", projectAccess{}, err
	}
	if !readable {
		return "", projectAccess{}, authz.ErrNotFound
	}
	return project, access, nil
}

func (s *Store) decide(ctx context.Context, c decisionCommand) (d goldenpath.Decision, applied bool, err error) {
	if err = c.Principal.ValidateUser(); err != nil {
		return d, false, err
	}
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return d, false, fmt.Errorf("begin Decision outcome: %w", err)
	}
	defer rollback(ctx, tx, &err)
	project, access, err := decisionProjectAccess(ctx, tx, c.Principal, c.id)
	if err != nil {
		return d, false, err
	}
	if !access.active {
		return d, false, authz.ErrConflict
	}
	if err = authz.RequireDecisionAccept(access.role); err != nil {
		return d, false, err
	}
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM radishnexus.decisions WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Principal.WorkspaceID, c.id).Scan(&locked); err != nil {
		return d, false, fmt.Errorf("lock Decision outcome: %w", err)
	}
	if err = requireReadableDecisionEvidence(ctx, tx, c.Principal, c.id); err != nil {
		return d, false, err
	}
	d, err = loadDecision(ctx, tx, c.Principal.WorkspaceID, c.id)
	if err != nil {
		return d, false, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return d, false, fmt.Errorf("read Decision time: %w", err)
	}
	now = now.UTC().Truncate(time.Microsecond)
	if !now.After(d.UpdatedAt) {
		now = d.UpdatedAt.Add(time.Microsecond)
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, c.kind, "decision", c.id, c.operation, c.digest, "decision", c.id, c.event, now)
	if err != nil {
		return d, false, err
	}
	if duplicate {
		var exact bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.domain_events e
 JOIN radishnexus.collaboration_command_receipts r ON r.workspace_id=e.workspace_id AND r.event_id=e.event_id
 WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.primary_entity_type='decision' AND e.primary_entity_id=$3
 AND e.project_id=$4 AND e.actor_kind='user' AND e.actor_id=$5 AND ($8='decision.accept' OR e.source_kind='web') AND e.schema_version=1
 AND e.event_type=$6 AND e.payload=jsonb_build_object('status',$7::text)
 AND r.command_kind=$8 AND r.target_type='decision' AND r.target_id=$3 AND r.result_type='decision' AND r.result_id=$3
 AND r.result_revision IS NULL AND r.actor_id=$5 AND r.client_operation_id=$9 AND e.occurred_at=r.created_at
 AND e.occurred_at=$10)`, c.Principal.WorkspaceID, receipt.eventID, c.id, project, c.Principal.ID,
			"decision."+c.status, c.status, c.kind, c.operation, d.DecidedAt).Scan(&exact)
		if err != nil {
			return d, false, fmt.Errorf("verify Decision outcome receipt: %w", err)
		}
		if !exact || receipt.resultID != c.id || receipt.resultRevision != nil || (d.Status != c.status && !(c.status == "accepted" && d.Status == "superseded")) || d.Outcome != c.outcome || d.Rationale != c.rationale || d.RejectionReason != c.reason || len(d.DeciderIDs) != 1 || d.DeciderIDs[0] != c.Principal.ID {
			return d, false, errors.New("invalid Decision outcome receipt provenance")
		}
		if err = tx.Commit(ctx); err != nil {
			return d, false, fmt.Errorf("commit Decision outcome retry: %w", err)
		}
		return d, false, nil
	}
	if d.Status != "proposed" {
		return d, false, authz.ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE radishnexus.decisions SET status=$3, outcome=NULLIF($4,''), rationale=NULLIF($5,''),
 rejection_reason=NULLIF($6,''), decider_ids=ARRAY[$7]::text[], decided_at=$8, updated_at=$8, decision_revision=decision_revision+1
 WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, c.id, c.status, c.outcome, c.rationale, c.reason, c.Principal.ID, now)
	if err != nil {
		return d, false, fmt.Errorf("update Decision outcome: %w", err)
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.event, Type: "decision." + c.status, WorkspaceID: c.Principal.WorkspaceID,
		ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "decision", PrimaryID: c.id,
		ProjectID: project, CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: now, Payload: map[string]any{"status": c.status}}); err != nil {
		return d, false, err
	}
	if err = insertOutbox(ctx, tx, c.event); err != nil {
		return d, false, err
	}
	d, err = loadDecision(ctx, tx, c.Principal.WorkspaceID, c.id)
	if err != nil {
		return d, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return d, false, mapDatabaseError("commit Decision outcome", err)
	}
	return d, true, nil
}
