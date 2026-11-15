package postgres

import (
	"context"
	"errors"
	"fmt"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func executionMessageAccess(ctx context.Context, tx pgx.Tx, p authz.Principal, channelID, messageID string) (goldenpath.MessageLocation, channelAccess, bool, error) {
	loc := goldenpath.MessageLocation{MessageID: messageID, ChannelID: channelID}
	facts, err := readableMessage(ctx, tx, p, messageID)
	if err != nil {
		return loc, channelAccess{}, false, err
	}
	if facts.channelID != channelID {
		return loc, channelAccess{}, false, authz.ErrNotFound
	}
	loc.ThreadID = facts.threadID
	channel, err := readableChannel(ctx, tx, p, channelID)
	if err != nil {
		return loc, channel, false, err
	}
	projectVisible := channel.visibility == "project"
	if loc.ThreadID != nil {
		var visibility string
		err = tx.QueryRow(ctx, `SELECT visibility FROM radishnexus.threads WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, *loc.ThreadID).Scan(&visibility)
		projectVisible = projectVisible && visibility == "project"
	}
	return loc, channel, projectVisible, err
}

func (s *Store) WriteTicketExecution(ctx context.Context, c goldenpath.TicketExecutionCommand) (out goldenpath.TicketExecutionResult, err error) {
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	if c.Kind == goldenpath.TicketFromMessage {
		out, err = writeTicketFromMessage(ctx, tx, c)
	} else {
		out, err = writeTicketDiscussion(ctx, tx, c)
	}
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, mapDatabaseError("commit Ticket execution", err)
	}
	return out, nil
}

func writeTicketFromMessage(ctx context.Context, tx pgx.Tx, c goldenpath.TicketExecutionCommand) (out goldenpath.TicketExecutionResult, err error) {
	p := c.Principal
	location, channel, projectVisible, err := executionMessageAccess(ctx, tx, p, c.ChannelID, c.MessageID)
	if err != nil {
		return out, err
	}
	if !channel.project.active || !channel.project.role.CanContribute() {
		return out, authz.ErrForbidden
	}
	if channel.status != "active" || !projectVisible {
		return out, authz.ErrConflict
	}
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT greatest(clock_timestamp(),created_at) FROM radishnexus.messages WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, c.MessageID).Scan(&at); err != nil {
		return out, err
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, c.Kind, "message", c.MessageID, c.ClientOperationID, c.Digest, "ticket", c.ResultID, c.EventID, at)
	if err != nil {
		return out, err
	}
	out.Source = location
	if duplicate {
		if err = verifyExecutionReceipt(ctx, tx, c, receipt.eventID, receipt.resultID, c.MessageID, "ticket.created"); err != nil {
			return out, err
		}
		if err = verifyInitialClassification(ctx, tx, c.Invocation, channel.projectID, receipt.resultID, receipt.eventID, c.Classification); err != nil {
			return out, err
		}
		out.Ticket, err = loadTicket(ctx, tx, p.WorkspaceID, receipt.resultID)
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO radishnexus.tickets(id,workspace_id,governing_project_id,title,status,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,'open',$5,$6,$6)`, c.ResultID, p.WorkspaceID, channel.projectID, c.Title, p.ID, at); err != nil {
		return out, mapDatabaseError("insert Message Ticket", err)
	}
	ref := entityref.Ref{Type: "message", ID: c.MessageID}
	if err = insertExecutionEvent(ctx, tx, c, channel.projectID, c.ResultID, "ticket.created", 2, at, map[string]any{"status": "open", "message": ref}); err != nil {
		return out, err
	}
	if err = insertExecutionLink(ctx, tx, c, c.RelationID, c.ResultID, "started-from", ref, at); err != nil {
		return out, err
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return out, err
	}
	if c.Classification != nil {
		if err = applyTicketClassification(ctx, tx, c.Invocation, channel.projectID, c.ResultID, c.ClassificationEventID, c.EventID,
			cp.EmptyClassification(), *c.Classification, 1, at.Add(time.Microsecond)); err != nil {
			return out, err
		}
	}
	out.Ticket, err = loadTicket(ctx, tx, p.WorkspaceID, c.ResultID)
	out.Created = true
	return out, err
}

func executionThreadAccess(ctx context.Context, tx pgx.Tx, p authz.Principal, thread string) (string, channelAccess, error) {
	project, _, err := readableThread(ctx, tx, p, thread)
	if err != nil {
		return "", channelAccess{}, err
	}
	var channelID *string
	if err = tx.QueryRow(ctx, `SELECT origin_channel_id FROM radishnexus.threads WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, thread).Scan(&channelID); err != nil {
		return "", channelAccess{}, err
	}
	if channelID == nil {
		return "", channelAccess{}, authz.ErrNotFound
	}
	channel, err := readableChannel(ctx, tx, p, *channelID)
	if err == nil && channel.projectID != project {
		return "", channelAccess{}, authz.ErrNotFound
	}
	return *channelID, channel, err
}

func writeTicketDiscussion(ctx context.Context, tx pgx.Tx, c goldenpath.TicketExecutionCommand) (out goldenpath.TicketExecutionResult, err error) {
	p := c.Principal
	project, err := projectEntityScope(ctx, tx, p, "ticket", c.TicketID)
	if err != nil {
		return out, err
	}
	access, readable, err := readProjectAccess(ctx, tx, p, project)
	if err != nil {
		return out, err
	}
	if !readable {
		return out, authz.ErrNotFound
	}
	thread := c.ThreadID
	state := ""
	event := goldenpath.DiscussionLinked
	if c.Kind == goldenpath.TicketDiscussionUnlink {
		event = goldenpath.DiscussionUnlinked
		err = tx.QueryRow(ctx, `SELECT to_id FROM radishnexus.entity_links WHERE workspace_id=$1 AND from_type='ticket' AND from_id=$2 AND relation_type='relates-to' AND to_type='thread' AND id=$3`, p.WorkspaceID, c.TicketID, c.LinkID).Scan(&thread)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, authz.ErrNotFound
		}
		if err != nil {
			return out, err
		}
	}
	_, channel, err := executionThreadAccess(ctx, tx, p, thread)
	if err != nil {
		return out, err
	}
	if channel.projectID != project {
		return out, authz.ErrNotFound
	}
	if !access.active || !access.role.CanContribute() {
		return out, authz.ErrForbidden
	}
	if c.Kind == goldenpath.TicketDiscussionLink && channel.status != "active" {
		return out, authz.ErrConflict
	}
	// Serialize removal and replacement with other discussion writes on this Ticket.
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT greatest(clock_timestamp(),created_at) FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, p.WorkspaceID, c.TicketID).Scan(&at); err != nil {
		return out, err
	}
	// Obtain time after taking the lock, including on a host whose clock moved backwards.
	if err = tx.QueryRow(ctx, `
		SELECT greatest(clock_timestamp(),$3::timestamptz,
		  coalesce((SELECT max(updated_at)+interval '1 microsecond' FROM radishnexus.entity_links
		    WHERE workspace_id=$1 AND from_type='ticket' AND from_id=$2 AND relation_type='relates-to' AND to_type='thread'),$3),
		  (SELECT created_at FROM radishnexus.threads WHERE workspace_id=$1 AND id=$4))
	`, p.WorkspaceID, c.TicketID, at, thread).Scan(&at); err != nil {
		return out, err
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, c.Kind, "ticket", c.TicketID, c.ClientOperationID, c.Digest, "entity-link", c.ResultID, c.EventID, at)
	if err != nil {
		return out, err
	}
	if duplicate {
		if err = verifyExecutionReceipt(ctx, tx, c, receipt.eventID, receipt.resultID, thread, event); err != nil {
			return out, err
		}
		return goldenpath.TicketExecutionResult{LinkID: receipt.resultID}, nil
	}
	ref := entityref.Ref{Type: "thread", ID: thread}
	if err = insertExecutionEvent(ctx, tx, c, project, c.TicketID, event, 1, at, map[string]any{"thread": ref, "relation": entityref.Ref{Type: "entity-link", ID: c.ResultID}}); err != nil {
		return out, err
	}
	if c.Kind == goldenpath.TicketDiscussionLink {
		err = insertExecutionLink(ctx, tx, c, c.ResultID, c.TicketID, "relates-to", ref, at)
	} else {
		err = tx.QueryRow(ctx, `UPDATE radishnexus.entity_links SET state='removed',removed_by_kind='user',removed_by_id=$4,removed_at=$5,updated_at=$5,removal_reason='ticket-discussion-unlinked' WHERE workspace_id=$1 AND from_id=$2 AND id=$3 AND state='active' RETURNING state`, p.WorkspaceID, c.TicketID, c.LinkID, p.ID, at).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, authz.ErrConflict
		}
	}
	if err != nil {
		return out, mapDatabaseError("change Ticket discussion", err)
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return out, err
	}
	return goldenpath.TicketExecutionResult{LinkID: c.ResultID, Created: c.Kind == goldenpath.TicketDiscussionLink}, nil
}

func verifyExecutionReceipt(ctx context.Context, tx pgx.Tx, c goldenpath.TicketExecutionCommand, eventID, resultID, subject, eventType string) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
	  SELECT 1 FROM radishnexus.domain_events e JOIN radishnexus.entity_links l
	    ON l.workspace_id=e.workspace_id AND l.from_type='ticket' AND l.from_id=e.primary_entity_id
	  WHERE e.workspace_id=$1 AND e.event_id=$2 AND e.actor_kind='user' AND e.actor_id=$3
	    AND e.source_kind='web' AND e.event_type=$4 AND l.to_id=$5
	    AND (($4='ticket.created' AND e.schema_version=2 AND l.source_event_id=e.event_id
	        AND l.relation_type='started-from' AND l.to_type='message' AND l.from_id=$6 AND l.state='active')
	      OR ($4<>'ticket.created' AND e.schema_version=1 AND l.id=$6 AND l.to_type='thread' AND l.relation_type='relates-to'
	        AND e.payload->'relation'->>'id'=l.id AND e.payload->'thread'->>'id'=l.to_id
	        AND (($4='ticket.discussion-linked' AND l.source_event_id=e.event_id)
	          OR ($4='ticket.discussion-unlinked' AND l.removed_at=e.occurred_at AND l.removed_by_id=e.actor_id))))
	)`, c.Principal.WorkspaceID, eventID, c.Principal.ID, eventType, subject, resultID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("invalid Ticket execution receipt provenance")
	}
	return nil
}

func insertExecutionLink(ctx context.Context, tx pgx.Tx, c goldenpath.TicketExecutionCommand, id, ticket, relation string, to entityref.Ref, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,created_at,updated_at,source_event_id)
	 VALUES($1,$2,'ticket',$3,$4,$5,$6,'asserted','user','user',$7,$8,$8,$9)`, id, c.Principal.WorkspaceID, ticket, relation, to.Type, to.ID, c.Principal.ID, at, c.EventID)
	if err != nil {
		return mapDatabaseError("insert Ticket execution link", err)
	}
	return nil
}

func insertExecutionEvent(ctx context.Context, tx pgx.Tx, c goldenpath.TicketExecutionCommand, project, ticket, kind string, version int, at time.Time, payload map[string]any) error {
	return insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: kind, SchemaVersion: version, WorkspaceID: c.Principal.WorkspaceID, ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "ticket", PrimaryID: ticket, ProjectID: project, CorrelationID: c.CorrelationID, OccurredAt: at, Payload: payload})
}

func (s *Store) ReadExecutionMessage(ctx context.Context, p authz.Principal, channel, message string) (out goldenpath.Message, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	if _, _, _, err = executionMessageAccess(ctx, tx, p, channel, message); err != nil {
		return out, err
	}
	out, err = scanMessageRow(tx.QueryRow(ctx, `SELECT id,workspace_id,channel_id,thread_id,author_id,body,client_operation_id,created_at FROM radishnexus.messages WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, message))
	if err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return
}

func (s *Store) ListTicketExecution(ctx context.Context, p authz.Principal, source entityref.Ref, channelID string, in goldenpath.DiscoveryPageInput) (page goldenpath.ExecutionPage, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer rollback(ctx, tx, &err)
	var projectID string
	var query string
	switch source.Type {
	case "message":
		_, ch, visible, e := executionMessageAccess(ctx, tx, p, channelID, source.ID)
		if e != nil {
			return page, e
		}
		projectID = ch.projectID
		page.CanWrite = visible && ch.status == "active" && ch.project.active && ch.project.role.CanContribute()
		query = `SELECT l.from_id,t.title,t.status,''::text
		 FROM radishnexus.entity_links l JOIN radishnexus.tickets t ON t.workspace_id=l.workspace_id AND t.id=l.from_id AND t.governing_project_id=$3
		 WHERE l.workspace_id=$1 AND l.to_id=$2 AND l.from_type='ticket' AND l.to_type='message' AND l.relation_type='started-from' AND l.state='active' AND l.from_id>$4 ORDER BY l.from_id LIMIT $5`
	case "thread":
		_, ch, e := executionThreadAccess(ctx, tx, p, source.ID)
		if e != nil {
			return page, e
		}
		projectID = ch.projectID
		page.CanWrite = ch.status == "active" && ch.project.active && ch.project.role.CanContribute()
		query = `SELECT l.from_id,t.title,t.status,l.id
		 FROM radishnexus.entity_links l JOIN radishnexus.tickets t ON t.workspace_id=l.workspace_id AND t.id=l.from_id AND t.governing_project_id=$3
		 WHERE l.workspace_id=$1 AND l.to_id=$2 AND l.from_type='ticket' AND l.to_type='thread' AND l.relation_type='relates-to' AND l.state='active' AND l.from_id>$4 ORDER BY l.from_id LIMIT $5`
	case "ticket":
		t, w, e := ticketComponentAccess(ctx, tx, p, source.ID)
		if e != nil {
			return page, e
		}
		projectID = t.GoverningProjectID
		page.CanWrite = w
		// Same Project access was established for the Ticket. Apply both
		// remaining ACLs before limiting, so a hidden Thread never affects a cursor.
		query = `SELECT l.to_id,t.title,c.id,l.id
		 FROM radishnexus.entity_links l
		 JOIN radishnexus.threads t ON t.workspace_id=l.workspace_id AND t.id=l.to_id AND t.governing_project_id=$3
		 JOIN radishnexus.channels c ON c.workspace_id=t.workspace_id AND c.id=t.origin_channel_id AND c.governing_project_id=$3
		 WHERE l.workspace_id=$1 AND l.from_id=$2 AND l.from_type='ticket' AND l.to_type='thread' AND l.relation_type='relates-to' AND l.state='active' AND l.to_id>$4
		   AND (t.visibility='project' OR EXISTS(SELECT 1 FROM radishnexus.thread_memberships m WHERE m.workspace_id=t.workspace_id AND m.thread_id=t.id AND m.user_id=$6))
		   AND (c.visibility='project' OR EXISTS(SELECT 1 FROM radishnexus.channel_memberships m WHERE m.workspace_id=c.workspace_id AND m.channel_id=c.id AND m.user_id=$6))
		 ORDER BY l.to_id LIMIT $5`
	default:
		return page, authz.ErrInvalid
	}
	args := []any{p.WorkspaceID, source.ID, projectID, in.AfterID, in.Limit + 1}
	if source.Type == "ticket" {
		args = append(args, p.ID)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return page, err
	}
	page.Rows = []goldenpath.ExecutionRow{}
	for rows.Next() {
		row := goldenpath.ExecutionRow{Ref: entityref.Ref{Type: "ticket"}, ProjectID: projectID}
		extra := &row.Status
		if source.Type == "ticket" {
			row.Ref.Type = "thread"
			extra = &row.ChannelID
		}
		if err = rows.Scan(&row.Ref.ID, &row.Title, extra, &row.LinkID); err != nil {
			rows.Close()
			return page, err
		}
		page.Rows = append(page.Rows, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Rows) > in.Limit {
		page.Rows = page.Rows[:in.Limit]
		page.NextID = page.Rows[len(page.Rows)-1].Ref.ID
	}
	for i := range page.Rows {
		if source.Type == "ticket" {
			page.Rows[i].CanUnlink = page.CanWrite
		}
		if source.Type == "thread" {
			_, page.Rows[i].CanUnlink, err = ticketComponentAccess(ctx, tx, p, page.Rows[i].Ref.ID)
			if err != nil {
				return page, err
			}
		}
	}
	err = tx.Commit(ctx)
	return
}
