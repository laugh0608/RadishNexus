package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (s *Store) SupersedeDecision(ctx context.Context, c goldenpath.SupersedeDecisionCommand) (result goldenpath.SupersedeDecisionResult, err error) {
	if err = c.Principal.ValidateUser(); err != nil {
		return result, err
	}
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return result, fmt.Errorf("begin supersession: %w", err)
	}
	defer rollback(ctx, tx, &err)
	project, access, err := decisionProjectAccess(ctx, tx, c.Principal, c.DecisionID)
	if err != nil {
		return result, err
	}
	other, _, err := decisionProjectAccess(ctx, tx, c.Principal, c.ReplacementID)
	if err != nil {
		return result, err
	}
	if !access.active {
		return result, authz.ErrConflict
	}
	if err = authz.RequireDecisionAccept(access.role); err != nil {
		return result, err
	}
	if project != other || c.DecisionID == c.ReplacementID {
		return result, authz.ErrConflict
	}
	ids := []string{c.DecisionID, c.ReplacementID}
	sort.Strings(ids)
	for _, id := range ids {
		var locked string
		if err = tx.QueryRow(ctx, `SELECT id FROM radishnexus.decisions WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Principal.WorkspaceID, id).Scan(&locked); err != nil {
			return result, fmt.Errorf("lock supersession Decision: %w", err)
		}
	}
	for _, id := range ids {
		if err = requireReadableDecisionEvidence(ctx, tx, c.Principal, id); err != nil {
			return result, err
		}
	}
	old, err := loadDecision(ctx, tx, c.Principal.WorkspaceID, c.DecisionID)
	if err != nil {
		return result, err
	}
	newDecision, err := loadDecision(ctx, tx, c.Principal.WorkspaceID, c.ReplacementID)
	if err != nil {
		return result, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT GREATEST(clock_timestamp(),$3::timestamptz+interval '1 microsecond',$4::timestamptz+interval '1 microsecond',COALESCE(max(t.created_at)+interval '1 microsecond','-infinity'::timestamptz)) FROM radishnexus.tickets t JOIN radishnexus.entity_links l ON l.workspace_id=t.workspace_id AND l.from_type='ticket' AND l.from_id=t.id AND l.relation_type='implements' AND l.to_type='decision' WHERE l.workspace_id=$1 AND l.to_id=$2`, c.Principal.WorkspaceID, c.DecisionID, old.UpdatedAt, newDecision.UpdatedAt).Scan(&now); err != nil {
		return result, fmt.Errorf("read supersession time: %w", err)
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, "decision.supersede", "decision", c.DecisionID, c.ClientOperationID, c.PayloadSHA256, "entity-link", c.LinkID, c.EventID, now)
	if err != nil {
		return result, err
	}
	if duplicate {
		result, err = loadSupersessionResult(ctx, tx, c, receipt, project)
		if err != nil {
			return result, err
		}
		if err = tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("commit supersession retry: %w", err)
		}
		return result, nil
	}
	if old.Status != "accepted" || newDecision.Status != "accepted" || old.DecisionRevision != c.BaseDecisionRevision || newDecision.DecisionRevision != c.BaseReplacementRevision || old.DecisionRevision >= goldenpath.MaxDecisionRevision || newDecision.DecisionRevision >= goldenpath.MaxDecisionRevision {
		return result, authz.ErrConflict
	}
	var unavailable bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.entity_links WHERE workspace_id=$1 AND relation_type='supersedes' AND (from_id=$2 OR to_id=$3))`, c.Principal.WorkspaceID, c.ReplacementID, c.DecisionID).Scan(&unavailable); err != nil {
		return result, fmt.Errorf("check supersession endpoints: %w", err)
	}
	if unavailable {
		return result, authz.ErrConflict
	}
	for _, id := range ids {
		status := "accepted"
		if id == c.DecisionID {
			status = "superseded"
		}
		if _, err = tx.Exec(ctx, `UPDATE radishnexus.decisions SET status=$3,decision_revision=decision_revision+1,updated_at=$4 WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, id, status, now); err != nil {
			return result, fmt.Errorf("advance supersession Decision: %w", err)
		}
	}
	f := cp.Supersession{Status: "superseded", Replacement: cp.Ref{Type: "decision", ID: c.ReplacementID}, Relation: cp.Ref{Type: "entity-link", ID: c.LinkID}, FromRevision: old.DecisionRevision, ToRevision: old.DecisionRevision + 1, ReplacementFromRevision: newDecision.DecisionRevision, ReplacementToRevision: newDecision.DecisionRevision + 1}
	var payload map[string]any
	if err = json.Unmarshal(cp.Encode(f), &payload); err != nil {
		return result, fmt.Errorf("encode supersession event: %w", err)
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: "decision.superseded", WorkspaceID: c.Principal.WorkspaceID, ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "decision", PrimaryID: c.DecisionID, ProjectID: project, CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: now, Payload: payload}); err != nil {
		return result, err
	}
	metadata := cp.Encode(map[string]string{"supersession_reason": c.SupersessionReason})
	if _, err = tx.Exec(ctx, `INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,created_at,updated_at,source_event_id,metadata) VALUES($1,$2,'decision',$3,'supersedes','decision',$4,'asserted','user','user',$5,$6,$6,$7,$8)`, c.LinkID, c.Principal.WorkspaceID, c.ReplacementID, c.DecisionID, c.Principal.ID, now, c.EventID, metadata); err != nil {
		return result, mapDatabaseError("insert supersession relation", err)
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return result, err
	}
	result = supersessionResult(c.DecisionID, f, c.SupersessionReason, now)
	if err = tx.Commit(ctx); err != nil {
		return result, mapDatabaseError("commit supersession", err)
	}
	return result, nil
}

func supersessionResult(old string, f cp.Supersession, reason string, at time.Time) goldenpath.SupersedeDecisionResult {
	return goldenpath.SupersedeDecisionResult{DecisionID: old, ReplacementID: f.Replacement.ID, LinkID: f.Relation.ID, SupersessionReason: reason, FromRevision: f.FromRevision, ToRevision: f.ToRevision, ReplacementFromRevision: f.ReplacementFromRevision, ReplacementToRevision: f.ReplacementToRevision, OccurredAt: at}
}
func loadSupersessionResult(ctx context.Context, tx pgx.Tx, c goldenpath.SupersedeDecisionCommand, r collaborationCommandReceipt, project string) (goldenpath.SupersedeDecisionResult, error) {
	var raw []byte
	var reason string
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT e.payload,l.metadata->>'supersession_reason',e.occurred_at
 FROM radishnexus.domain_events e JOIN radishnexus.entity_links l ON l.workspace_id=e.workspace_id AND l.source_event_id=e.event_id
 JOIN radishnexus.collaboration_command_receipts r ON r.workspace_id=e.workspace_id AND r.event_id=e.event_id
 WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.event_type='decision.superseded' AND e.schema_version=1 AND e.primary_entity_type='decision' AND e.primary_entity_id=$3
 AND e.project_id=$4 AND e.actor_kind='user' AND e.actor_id=$5 AND e.source_kind='web'
 AND l.id=$6 AND l.from_type='decision' AND l.from_id=$7 AND l.to_type='decision' AND l.to_id=$3 AND l.relation_type='supersedes' AND l.state='active' AND l.origin='user' AND l.created_by_id=$5 AND l.created_at=e.occurred_at AND l.updated_at=e.occurred_at
 AND r.command_kind='decision.supersede' AND r.target_type='decision' AND r.target_id=$3 AND r.result_type='entity-link' AND r.result_id=$6 AND r.result_revision IS NULL AND r.actor_id=$5 AND r.client_operation_id=$8 AND r.created_at=e.occurred_at`, c.Principal.WorkspaceID, r.eventID, c.DecisionID, project, c.Principal.ID, r.resultID, c.ReplacementID, c.ClientOperationID).Scan(&raw, &reason, &at)
	if err != nil {
		return goldenpath.SupersedeDecisionResult{}, fmt.Errorf("verify supersession receipt: %w", err)
	}
	f, err := cp.DecodeSupersession(raw)
	if err != nil || r.resultRevision != nil || f.Replacement.ID != c.ReplacementID || f.Relation.ID != r.resultID || f.FromRevision != c.BaseDecisionRevision || f.ReplacementFromRevision != c.BaseReplacementRevision || reason != c.SupersessionReason {
		return goldenpath.SupersedeDecisionResult{}, errors.New("invalid supersession receipt provenance")
	}
	if _, err = tx.Exec(ctx, `SELECT radishnexus.validate_decision_supersession_link($1,$2)`, c.Principal.WorkspaceID, r.resultID); err != nil {
		return goldenpath.SupersedeDecisionResult{}, fmt.Errorf("validate supersession history: %w", err)
	}
	return supersessionResult(c.DecisionID, f, reason, at), nil
}
