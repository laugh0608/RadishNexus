package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const commandDecisionReview = "decision.review"

func (s *Store) ReviewDecision(ctx context.Context, c goldenpath.ReviewDecisionCommand) (out goldenpath.DecisionReviewRecord, applied bool, err error) {
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return out, false, fmt.Errorf("begin Decision review: %w", err)
	}
	defer rollback(ctx, tx, &err)

	project, access, err := decisionProjectAccess(ctx, tx, c.Principal, c.DecisionID)
	if err != nil {
		return out, false, err
	}
	if !access.active {
		return out, false, authz.ErrConflict
	}
	if err = authz.RequireDecisionAccept(access.role); err != nil {
		return out, false, err
	}

	var status string
	var revision int
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT status, decision_revision, updated_at FROM radishnexus.decisions
		WHERE workspace_id=$1 AND id=$2 FOR UPDATE
	`, c.Principal.WorkspaceID, c.DecisionID).Scan(&status, &revision, &updatedAt)
	if err != nil {
		return out, false, fmt.Errorf("lock Decision for review: %w", err)
	}
	threadIDs, err := readableDecisionEvidence(ctx, tx, c.Principal, c.DecisionID)
	if err != nil {
		return out, false, err
	}
	var extraID *string
	if ref := c.Facts.AdditionalEvidence; ref != nil {
		scope, _, err := readableThread(ctx, tx, c.Principal, ref.ID)
		if errors.Is(err, authz.ErrNotFound) {
			return out, false, authz.ErrForbidden
		}
		if err != nil {
			return out, false, err
		}
		if scope != project || slices.Contains(threadIDs, ref.ID) {
			return out, false, authz.ErrConflict
		}
		extraID = &ref.ID
	}

	var at time.Time
	err = tx.QueryRow(ctx, `
		SELECT GREATEST(clock_timestamp(), $3::timestamptz + interval '1 microsecond',
			(SELECT max(created_at) + interval '1 microsecond' FROM radishnexus.threads
			 WHERE workspace_id=$1 AND (id=ANY($4::text[]) OR id=$5)),
			radishnexus.last_decision_review($1, ARRAY[$2]::text[]) + interval '1 microsecond')
	`, c.Principal.WorkspaceID, c.DecisionID, updatedAt, threadIDs, extraID).Scan(&at)
	if err != nil {
		return out, false, fmt.Errorf("read Decision review time: %w", err)
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, commandDecisionReview,
		"decision", c.DecisionID, c.ClientOperationID, c.PayloadSHA256, "decision", c.DecisionID, c.EventID, at)
	if err != nil {
		return out, false, err
	}
	eventID := c.EventID
	if duplicate {
		eventID = receipt.eventID
		var raw []byte
		err = tx.QueryRow(ctx, `
			SELECT e.occurred_at, e.payload
			FROM radishnexus.domain_events e JOIN radishnexus.collaboration_command_receipts r
			ON r.workspace_id=e.workspace_id AND r.event_id=e.event_id
			WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.event_type='decision.reviewed' AND e.schema_version=1
			AND e.primary_entity_type='decision' AND e.primary_entity_id=$3 AND e.project_id=$4
			AND e.actor_kind='user' AND e.actor_id=$5 AND e.source_kind='web'
			AND r.command_kind='decision.review' AND r.client_operation_id=$6 AND r.actor_id=$5
			AND r.target_type='decision' AND r.target_id=$3 AND r.result_type='decision'
			AND r.result_id=$3 AND r.result_revision IS NULL AND r.created_at=e.occurred_at
		`, c.Principal.WorkspaceID, eventID, c.DecisionID, project, c.Principal.ID, c.ClientOperationID).Scan(&at, &raw)
		if err != nil {
			return out, false, fmt.Errorf("verify Decision review receipt: %w", err)
		}
		facts, err := cp.DecodeDecisionReview(raw)
		if err != nil || string(cp.Encode(facts)) != string(cp.Encode(c.Facts)) {
			return out, false, errors.New("invalid Decision review receipt provenance")
		}
	} else {
		if status != "accepted" || revision != c.Facts.DecisionRevision {
			return out, false, authz.ErrConflict
		}
		if err = insertEvent(ctx, tx, eventRecord{
			ID: c.EventID, Type: "decision.reviewed", WorkspaceID: c.Principal.WorkspaceID,
			ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID,
			PrimaryType: "decision", PrimaryID: c.DecisionID, ProjectID: project,
			CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: at,
			Payload: map[string]any{"decision_revision": revision, "trigger": c.Facts.Trigger,
				"conclusion": c.Facts.Conclusion, "reason": c.Facts.Reason, "additional_evidence": c.Facts.AdditionalEvidence},
		}); err != nil {
			return out, false, err
		}
		if err = insertOutbox(ctx, tx, c.EventID); err != nil {
			return out, false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, false, mapDatabaseError("commit Decision review", err)
	}
	return goldenpath.DecisionReviewRecord{
		EventID: eventID, DecisionID: c.DecisionID, Actor: goldenpath.ActorRef{Kind: "user", ID: c.Principal.ID},
		OccurredAt: at, Facts: c.Facts, OriginalEvidence: reviewEvidenceRefs(threadIDs),
	}, !duplicate, nil
}

func reviewEvidenceRefs(threadIDs []string) []entityref.Ref {
	refs := make([]entityref.Ref, 0, len(threadIDs))
	for _, id := range threadIDs {
		refs = append(refs, entityref.Ref{Type: "thread", ID: id})
	}
	return refs
}

func (s *Store) ListDecisionReviews(ctx context.Context, p authz.Principal, id string, input goldenpath.DecisionReviewPageInput) (out goldenpath.DecisionReviewPage, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, fmt.Errorf("begin Decision review read: %w", err)
	}
	defer rollback(ctx, tx, &err)
	project, _, err := decisionProjectAccess(ctx, tx, p, id)
	if err != nil {
		return out, err
	}

	out.Items = make([]goldenpath.DecisionReviewRecord, 0, input.Limit+1)
	threads, err := readableDecisionEvidence(ctx, tx, p, id)
	if errors.Is(err, authz.ErrForbidden) {
		return out, tx.Commit(ctx)
	}
	if err != nil {
		return out, err
	}
	sources := reviewEvidenceRefs(threads)
	var before *time.Time
	if !input.AfterTime.IsZero() {
		before = &input.AfterTime
	}
	beforeID := input.AfterID
	batchSize := goldenpath.MaxDecisionReviewPageSize + 1
	// Scan in bounded batches. The public cursor only advances to a visible record.
	for len(out.Items) <= input.Limit {
		rows, err := tx.Query(ctx, `
			SELECT a.event_id, a.occurred_at, COALESCE(h.original_actor,e.actor_id),
				COALESCE(h.original_facts,e.payload), h.event_id IS NOT NULL
			FROM radishnexus.activity_items a
			JOIN radishnexus.domain_events e ON e.workspace_id=a.workspace_id AND e.event_id=a.event_id
			LEFT JOIN radishnexus.context_import_history h ON h.workspace_id=e.workspace_id AND h.event_id=e.event_id
			WHERE a.workspace_id=$1 AND a.target_type='decision' AND a.target_id=$2
				AND a.projection_version=$3 AND a.activity_type='decision.reviewed'
				AND ($4::timestamptz IS NULL OR (a.occurred_at,a.event_id)<($4,$5))
			ORDER BY a.occurred_at DESC,a.event_id DESC LIMIT $6
		`, p.WorkspaceID, id, goldenpath.ActivityProjectionVersion, before, beforeID, batchSize)
		if err != nil {
			return out, fmt.Errorf("read Decision review candidates: %w", err)
		}
		batch, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (goldenpath.DecisionReviewRecord, error) {
			record := goldenpath.DecisionReviewRecord{DecisionID: id, Actor: goldenpath.ActorRef{Kind: "user"}, OriginalEvidence: sources}
			var facts []byte
			if err := row.Scan(&record.EventID, &record.OccurredAt, &record.Actor.ID, &facts, &record.Imported); err != nil {
				return record, err
			}
			record.Facts, err = cp.DecodeDecisionReview(facts)
			return record, err
		})
		if err != nil {
			return out, fmt.Errorf("load Decision review facts: %w", err)
		}
		for _, record := range batch {
			if ref := record.Facts.AdditionalEvidence; ref != nil {
				scope, _, err := readableThread(ctx, tx, p, ref.ID)
				if errors.Is(err, authz.ErrNotFound) {
					continue
				}
				if err != nil {
					return out, err
				}
				if scope != project {
					return out, errors.New("invalid Decision review evidence scope")
				}
			}
			out.Items = append(out.Items, record)
			if len(out.Items) > input.Limit {
				out.Items = out.Items[:input.Limit]
				out.HasMore = true
				break
			}
		}
		if out.HasMore || len(batch) < batchSize {
			break
		}
		last := batch[len(batch)-1]
		before, beforeID = &last.OccurredAt, last.EventID
	}
	if err = tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("commit Decision review read: %w", err)
	}
	return out, nil
}
