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
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func (store *Store) GetNexusView(
	ctx context.Context,
	principal authz.Principal,
	target entityref.Ref,
) (view goldenpath.NexusView, err error) {
	if err := principal.ValidateUser(); err != nil {
		return view, err
	}

	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead,
	})
	if err != nil {
		return view, fmt.Errorf("begin Nexus View transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	exists, canRead, err := entityAccess(ctx, tx, principal, target)
	if err != nil {
		return view, err
	}
	if !exists || !canRead {
		return view, authz.ErrNotFound
	}

	view.Current, err = loadCurrentProjection(ctx, tx, principal, target)
	if err != nil {
		return view, err
	}
	view.Relations, err = listRelationProjections(ctx, tx, principal, target)
	if err != nil {
		return view, err
	}
	view.Timeline, err = listTimeline(ctx, tx, principal, target)
	if err != nil {
		return view, err
	}

	if err := tx.Commit(ctx); err != nil {
		return view, fmt.Errorf("commit Nexus View transaction: %w", err)
	}
	return view, nil
}

func loadCurrentProjection(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	target entityref.Ref,
) (current goldenpath.CurrentProjection, err error) {
	current.Ref = target
	switch target.Type {
	case "decision":
		err = tx.QueryRow(ctx, `
			SELECT governing_project_id, question, status, updated_at
			FROM radishnexus.decisions
			WHERE workspace_id = $1 AND id = $2
		`, principal.WorkspaceID, target.ID).Scan(
			&current.GoverningProjectID,
			&current.Title,
			&current.Status,
			&current.UpdatedAt,
		)
	case "ticket":
		err = tx.QueryRow(ctx, `
			SELECT governing_project_id, title, status, updated_at
			FROM radishnexus.tickets
			WHERE workspace_id = $1 AND id = $2
		`, principal.WorkspaceID, target.ID).Scan(
			&current.GoverningProjectID,
			&current.Title,
			&current.Status,
			&current.UpdatedAt,
		)
	case "ci-run":
		var componentID string
		var recordedAt time.Time
		err = tx.QueryRow(ctx, `
			SELECT component_id, status, started_at, completed_at, created_at, updated_at
			FROM radishnexus.ci_runs
			WHERE workspace_id = $1 AND id = $2
		`, principal.WorkspaceID, target.ID).Scan(
			&componentID,
			&current.Status,
			&current.StartedAt,
			&current.CompletedAt,
			&recordedAt,
			&current.UpdatedAt,
		)
		if err == nil {
			componentRef := entityref.Ref{Type: "component", ID: componentID}
			exists, canRead, accessErr := entityAccess(ctx, tx, principal, componentRef)
			if accessErr != nil {
				return goldenpath.CurrentProjection{}, accessErr
			}
			if !exists || !canRead {
				return goldenpath.CurrentProjection{}, authz.ErrNotFound
			}
			componentTitle, titleErr := entityTitle(ctx, tx, principal.WorkspaceID, componentRef)
			if titleErr != nil {
				return goldenpath.CurrentProjection{}, titleErr
			}
			current.Component = &goldenpath.SubjectProjection{
				State: goldenpath.ProjectionVisible,
				Ref:   componentRef,
				Title: componentTitle,
			}
			current.RecordedAt = &recordedAt
		}
	default:
		return current, fmt.Errorf("load Current projection: unsupported target type %q", target.Type)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return goldenpath.CurrentProjection{}, authz.ErrNotFound
	}
	if err != nil {
		return goldenpath.CurrentProjection{}, fmt.Errorf("load %s Current projection: %w", target.Type, err)
	}
	return current, nil
}

func listTimeline(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	target entityref.Ref,
) ([]goldenpath.TimelineItem, error) {
	type timelineFact struct {
		item     goldenpath.TimelineItem
		subjects []entityref.Ref
	}

	rows, err := tx.Query(ctx, `
		SELECT event_id, activity_type, actor_kind, actor_id, occurred_at,
			subject_refs, projection_version, safe_facts
		FROM radishnexus.activity_items
		WHERE workspace_id = $1
		  AND target_type = $2
		  AND target_id = $3
		  AND projection_version = $4
		ORDER BY occurred_at, event_id
	`, principal.WorkspaceID, target.Type, target.ID, goldenpath.ActivityProjectionVersion)
	if err != nil {
		return nil, fmt.Errorf("list Timeline facts: %w", err)
	}

	facts := make([]timelineFact, 0)
	for rows.Next() {
		var fact timelineFact
		var actorID *string
		var subjectJSON []byte
		var safeFactsJSON []byte
		if err := rows.Scan(
			&fact.item.EventID,
			&fact.item.ActivityType,
			&fact.item.Actor.Kind,
			&actorID,
			&fact.item.OccurredAt,
			&subjectJSON,
			&fact.item.ProjectionVersion,
			&safeFactsJSON,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan Timeline fact: %w", err)
		}
		if actorID != nil && fact.item.ActivityType != "ci-run.recorded" {
			fact.item.Actor.ID = *actorID
		}

		if err := json.Unmarshal(subjectJSON, &fact.subjects); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode Timeline subjects for event %s: %w", fact.item.EventID, err)
		}
		if err := json.Unmarshal(safeFactsJSON, &fact.item.SafeFacts); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode Timeline safe facts for event %s: %w", fact.item.EventID, err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate Timeline facts: %w", err)
	}
	rows.Close()

	timeline := make([]goldenpath.TimelineItem, 0, len(facts))
	for _, fact := range facts {
		fact.item.Subjects, err = projectActivitySubjects(ctx, tx, principal, fact.subjects)
		if err != nil {
			return nil, fmt.Errorf("project Timeline subjects for event %s: %w", fact.item.EventID, err)
		}
		timeline = append(timeline, fact.item)
	}
	return timeline, nil
}

func projectActivitySubjects(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	subjects []entityref.Ref,
) ([]goldenpath.SubjectProjection, error) {
	projections := make([]goldenpath.SubjectProjection, 0, len(subjects))
	for _, subject := range subjects {
		exists, canRead, err := entityAccess(ctx, tx, principal, subject)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if !canRead {
			projections = append(projections, goldenpath.SubjectProjection{
				State: goldenpath.ProjectionRestricted,
			})
			continue
		}
		title, err := entityTitle(ctx, tx, principal.WorkspaceID, subject)
		if err != nil {
			return nil, err
		}
		projections = append(projections, goldenpath.SubjectProjection{
			State: goldenpath.ProjectionVisible,
			Ref:   subject,
			Title: title,
		})
	}
	return projections, nil
}
