package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// readableThread has already authorized the Project, Thread and optional origin
// Channel. Decision and Ticket use the same Project ACL. A reply additionally
// needs the exact Thread and its origin Channel, both checked by the join.
const threadDeliveryPaths = `WITH delivery_paths AS (
    SELECT implementation.from_id AS ticket_id, 'decision-origin' AS origin
    FROM radishnexus.entity_links evidence
    JOIN radishnexus.decisions d
      ON d.workspace_id=evidence.workspace_id AND d.id=evidence.from_id AND d.governing_project_id=$3 AND d.status IN ('accepted','superseded')
    JOIN radishnexus.entity_links implementation
      ON implementation.workspace_id=d.workspace_id AND implementation.to_id=d.id
     AND implementation.to_type='decision' AND implementation.from_type='ticket'
     AND implementation.relation_type='implements' AND implementation.state='active'
    WHERE evidence.workspace_id=$1 AND evidence.to_id=$2 AND evidence.to_type='thread'
      AND evidence.from_type='decision' AND evidence.relation_type='derived-from' AND evidence.state='active'
      AND implementation.from_id >= $4
    UNION ALL
    SELECT creation.from_id, 'message-origin'
    FROM radishnexus.threads thread
    JOIN radishnexus.messages reply
      ON reply.workspace_id=thread.workspace_id AND reply.thread_id=thread.id AND reply.channel_id=thread.origin_channel_id
    JOIN radishnexus.entity_links creation
      ON creation.workspace_id=reply.workspace_id AND creation.to_id=reply.id
     AND creation.to_type='message' AND creation.from_type='ticket'
     AND creation.relation_type='started-from' AND creation.state='active'
    WHERE thread.workspace_id=$1 AND thread.id=$2 AND creation.from_id >= $4
    UNION ALL
    SELECT discussion.from_id, 'execution-discussion'
    FROM radishnexus.threads thread
    JOIN radishnexus.entity_links discussion
      ON discussion.workspace_id=thread.workspace_id AND discussion.to_id=thread.id AND discussion.to_type='thread'
     AND discussion.from_type='ticket' AND discussion.relation_type='relates-to' AND discussion.state='active'
    WHERE thread.workspace_id=$1 AND thread.id=$2 AND thread.origin_channel_id IS NOT NULL AND discussion.from_id >= $4
)`

const listThreadDeliveryTicketsSQL = threadDeliveryPaths + `
    SELECT t.id,t.title,t.status,array_remove(ARRAY[
        CASE WHEN bool_or(p.origin='decision-origin') THEN 'decision-origin' END,
        CASE WHEN bool_or(p.origin='message-origin') THEN 'message-origin' END,
        CASE WHEN bool_or(p.origin='execution-discussion') THEN 'execution-discussion' END
    ], NULL)
    FROM delivery_paths p
    JOIN radishnexus.tickets t ON t.workspace_id=$1 AND t.id=p.ticket_id AND t.governing_project_id=$3
    WHERE t.id>$4
    GROUP BY t.id
    ORDER BY t.id LIMIT $5`

func (s *Store) ListThreadDelivery(ctx context.Context, p authz.Principal, scope goldenpath.ThreadDeliveryScope, input goldenpath.DiscoveryPageInput) (page goldenpath.ThreadDeliveryPage, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer rollback(ctx, tx, &err)
	project, _, err := readableThread(ctx, tx, p, scope.ThreadID)
	if err != nil {
		return page, err
	}
	if scope.TicketID == "" {
		page, err = listThreadDeliveryTickets(ctx, tx, p.WorkspaceID, project, scope.ThreadID, input)
	} else {
		page, err = listThreadDeliveryFacts(ctx, tx, p, project, scope, input)
	}
	if err != nil {
		return page, err
	}
	return page, tx.Commit(ctx)
}

func listThreadDeliveryTickets(ctx context.Context, tx pgx.Tx, workspace, project, thread string, input goldenpath.DiscoveryPageInput) (page goldenpath.ThreadDeliveryPage, err error) {
	rows, err := tx.Query(ctx, listThreadDeliveryTicketsSQL, workspace, thread, project, input.AfterID, input.Limit+1)
	if err != nil {
		return page, fmt.Errorf("list Thread delivery Tickets: %w", err)
	}
	defer rows.Close()
	page.Items = make([]goldenpath.ThreadDeliveryItem, 0, input.Limit)
	for rows.Next() {
		var item goldenpath.ThreadDeliveryItem
		item.Ref.Type = "ticket"
		if err = rows.Scan(&item.Ref.ID, &item.Title, &item.Status, &item.Origins); err != nil {
			return page, fmt.Errorf("scan Thread delivery Ticket: %w", err)
		}
		if len(page.Items) == input.Limit {
			page.NextID = page.Items[input.Limit-1].Ref.ID
			break
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return page, fmt.Errorf("iterate Thread delivery Tickets: %w", err)
	}
	return page, nil
}

func listThreadDeliveryFacts(ctx context.Context, tx pgx.Tx, p authz.Principal, project string, scope goldenpath.ThreadDeliveryScope, input goldenpath.DiscoveryPageInput) (page goldenpath.ThreadDeliveryPage, err error) {
	var reachable bool
	err = tx.QueryRow(ctx, threadDeliveryPaths+`
        SELECT EXISTS(
            SELECT 1 FROM delivery_paths path
            JOIN radishnexus.tickets t ON t.workspace_id=$1 AND t.id=path.ticket_id AND t.governing_project_id=$3
            WHERE t.id=$4
        )`, p.WorkspaceID, scope.ThreadID, project, scope.TicketID).Scan(&reachable)
	if err != nil {
		return page, fmt.Errorf("check Ticket path from Thread: %w", err)
	}
	if !reachable {
		return page, authz.ErrNotFound
	}

	kind, source := goldenpath.TraceTicketRuns, scope.TicketID
	if scope.RunID != "" {
		if err = requireTraceRead(ctx, tx, p, "ci-run", scope.RunID); err != nil {
			return page, err
		}
		err = tx.QueryRow(ctx, `SELECT EXISTS(
            SELECT 1 FROM radishnexus.entity_links
            WHERE workspace_id=$1 AND from_id=$2 AND from_type='ticket' AND to_id=$3 AND to_type='ci-run'
              AND relation_type='tracks-build' AND state='active'
        )`, p.WorkspaceID, scope.TicketID, scope.RunID).Scan(&reachable)
		if err != nil {
			return page, fmt.Errorf("check CI Run path from Ticket: %w", err)
		}
		if !reachable {
			return page, authz.ErrNotFound
		}

		kind, source = goldenpath.TraceRunDeployments, scope.RunID
	}
	trace, err := readCIRunTracePage(ctx, tx, p, kind, source, goldenpath.CIRunTraceInput{DiscoveryPageInput: input}, false)
	if err != nil {
		return page, err
	}
	page.Items = make([]goldenpath.ThreadDeliveryItem, 0, len(trace.Items))
	for _, fact := range trace.Items {
		page.Items = append(page.Items, goldenpath.ThreadDeliveryItem{
			Ref: fact.Ref, Status: fact.Status, CompletedAt: fact.CompletedAt,
			Component: fact.Component, Environment: fact.Environment,
		})
	}
	page.NextID = trace.NextID
	return page, nil
}
