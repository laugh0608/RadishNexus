package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

// The same visible set resolves a continuation and supplies its page. Historical
// discussion visibility follows the Thread and its origin Channel even after unlink.
const visibleTicketEvents = `
 FROM radishnexus.activity_items a
 LEFT JOIN LATERAL (
   SELECT t.title FROM radishnexus.threads t
   JOIN radishnexus.projects project ON (project.workspace_id,project.id)=(t.workspace_id,t.governing_project_id)
   WHERE t.workspace_id=a.workspace_id AND t.id=a.subject_refs->0->>'id'
     AND a.subject_refs->0->>'type'='thread' AND jsonb_array_length(a.subject_refs)=1
     AND ` + readableProjectPredicate + `
     AND (t.visibility='project' OR EXISTS (
        SELECT 1 FROM radishnexus.thread_memberships m
        WHERE (m.workspace_id,m.thread_id,m.user_id)=(t.workspace_id,t.id,$2)))
     AND (t.origin_channel_id IS NULL OR EXISTS (
        SELECT 1 FROM radishnexus.channels c
        WHERE (c.workspace_id,c.id,c.governing_project_id)=(t.workspace_id,t.origin_channel_id,t.governing_project_id)
          AND (c.visibility='project' OR EXISTS (SELECT 1 FROM radishnexus.channel_memberships m
               WHERE (m.workspace_id,m.channel_id,m.user_id)=(c.workspace_id,c.id,$2)))))
 ) discussion ON a.activity_type IN ('ticket.discussion-linked','ticket.discussion-unlinked')
 WHERE a.workspace_id=$1 AND a.target_type='ticket' AND a.target_id=$3 AND a.projection_version=$4
   AND a.activity_type=ANY($5::text[])
   AND (a.activity_type NOT IN ('ticket.discussion-linked','ticket.discussion-unlinked') OR discussion.title IS NOT NULL)`

func (s *Store) ListTicketTimeline(ctx context.Context, p authz.Principal, ticket string, input goldenpath.TicketTimelineInput) (page goldenpath.TicketTimelinePage, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer rollback(ctx, tx, &err)
	if _, err = configurationActor(ctx, tx, p); err != nil {
		return page, err
	}
	_, readable, err := entityAccess(ctx, tx, p, entityref.Ref{Type: "ticket", ID: ticket})
	if err != nil {
		return page, err
	}
	if !readable {
		return page, authz.ErrNotFound
	}
	if !input.Execution {
		var messageSource bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.entity_links
		  WHERE workspace_id=$1 AND from_id=$2 AND from_type='ticket' AND relation_type='started-from' AND to_type='message' AND state='active')`, p.WorkspaceID, ticket).Scan(&messageSource)
		if err != nil {
			return page, fmt.Errorf("check Ticket history source: %w", err)
		}
		if messageSource {
			return page, authz.ErrConflict
		}
	}

	args := []any{p.WorkspaceID, p.ID, ticket, goldenpath.ActivityProjectionVersion, input.EventTypes()}
	query := `SELECT a.event_id,a.activity_type,a.actor_kind,a.actor_id,a.occurred_at,a.subject_refs,a.safe_facts,
	  discussion.title,(SELECT c.name FROM radishnexus.components c WHERE c.workspace_id=a.workspace_id
	    AND a.subject_refs->0->>'type'='component' AND c.id=a.subject_refs->0->>'id')` + visibleTicketEvents
	if input.AfterEventID != "" {
		var at time.Time
		err = tx.QueryRow(ctx, "SELECT a.occurred_at"+visibleTicketEvents+" AND a.event_id=$6", append(args, input.AfterEventID)...).Scan(&at)
		if errors.Is(err, pgx.ErrNoRows) {
			return page, authz.ErrConflict
		}
		if err != nil {
			return page, fmt.Errorf("resolve readable Ticket history boundary: %w", err)
		}
		query += " AND (a.occurred_at,a.event_id)<($7,$8)"
		args = append(args, input.Limit+1, at, input.AfterEventID)
	} else {
		args = append(args, input.Limit+1)
	}

	rows, err := tx.Query(ctx, query+" ORDER BY a.occurred_at DESC,a.event_id DESC LIMIT $6", args...)
	if err != nil {
		return page, fmt.Errorf("list Ticket history: %w", err)
	}
	defer rows.Close()
	page.Items = make([]goldenpath.TimelineItem, 0, input.Limit)
	// There is at most one creation event. Resolve its Decision or Message
	// using the original subject reader, including Message-in-Thread access.
	var sourceRefs []entityref.Ref
	var createdIndex int
	for rows.Next() {
		if len(page.Items) == input.Limit {
			page.NextEventID = page.Items[len(page.Items)-1].EventID
			break
		}
		var item goldenpath.TimelineItem
		var refs []entityref.Ref
		var threadTitle, componentTitle *string
		err = rows.Scan(&item.EventID, &item.ActivityType, &item.Actor.Kind, &item.Actor.ID, &item.OccurredAt, &refs, &item.SafeFacts, &threadTitle, &componentTitle)
		if err != nil {
			return page, fmt.Errorf("scan Ticket history: %w", err)
		}
		item.ProjectionVersion = goldenpath.ActivityProjectionVersion
		item.Subjects = []goldenpath.SubjectProjection{}
		switch item.ActivityType {
		case "ticket.created":
			if len(refs) != 1 || sourceRefs != nil {
				return page, errors.New("invalid Ticket creation history")
			}
			sourceRefs, createdIndex = refs, len(page.Items)
		case goldenpath.DiscussionLinked, goldenpath.DiscussionUnlinked:
			if len(refs) != 1 || threadTitle == nil {
				return page, errors.New("invalid Ticket discussion history")
			}
			item.Subjects = append(item.Subjects, goldenpath.SubjectProjection{State: goldenpath.ProjectionVisible, Ref: refs[0], Title: *threadTitle})
		case "ticket.component-linked", "ticket.component-unlinked":
			// Components are readable to every admitted Workspace member.
			if len(refs) != 1 || componentTitle == nil {
				return page, errors.New("invalid Ticket component history")
			}
			item.Subjects = append(item.Subjects, goldenpath.SubjectProjection{State: goldenpath.ProjectionVisible, Ref: refs[0], Title: *componentTitle})
		default:
			if len(refs) != 0 {
				return page, errors.New("unexpected Ticket history subjects")
			}
		}
		page.Items = append(page.Items, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return page, fmt.Errorf("iterate Ticket history: %w", err)
	}
	if sourceRefs != nil {
		page.Items[createdIndex].Subjects, err = projectActivitySubjects(ctx, tx, p, sourceRefs)
		if err != nil {
			return page, err
		}
	}
	return page, tx.Commit(ctx)
}
