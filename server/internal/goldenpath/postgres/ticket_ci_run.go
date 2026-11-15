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

func requireTraceRead(ctx context.Context, tx pgx.Tx, p authz.Principal, kind, id string) error {
	_, readable, err := entityAccess(ctx, tx, p, entityref.Ref{Type: kind, ID: id})
	if err != nil {
		return err
	}
	if !readable {
		return authz.ErrNotFound
	}
	return nil
}

func (s *Store) WriteTicketCIRun(ctx context.Context, c goldenpath.TicketCIRunCommand) (result goldenpath.TicketCIRunResult, err error) {
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return result, err
	}
	defer rollback(ctx, tx, &err)
	p := c.Principal
	project, access, err := ticketProjectAccess(ctx, tx, p, c.TicketID)
	if err != nil {
		return result, err
	}
	var created time.Time
	if err = tx.QueryRow(ctx, `SELECT created_at FROM radishnexus.tickets
	    WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, p.WorkspaceID, c.TicketID).Scan(&created); err != nil {
		return result, fmt.Errorf("lock Ticket for CI Run relation: %w", err)
	}

	runID := c.RunID
	unlink := c.Kind == goldenpath.TicketCIRunUnlink
	if unlink {
		err = tx.QueryRow(ctx, `SELECT to_id FROM radishnexus.entity_links
		    WHERE workspace_id=$1 AND id=$2 AND from_id=$3 AND relation_type='tracks-build'`,
			p.WorkspaceID, c.LinkID, c.TicketID).Scan(&runID)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, authz.ErrNotFound
		}
		if err != nil {
			return result, fmt.Errorf("locate Ticket CI Run relation: %w", err)
		}
	}
	if err = requireTraceRead(ctx, tx, p, "ci-run", runID); err != nil {
		return result, err
	}
	if !access.active || !access.role.CanContribute() {
		return result, authz.ErrForbidden
	}
	var status string
	var completed *time.Time
	err = tx.QueryRow(ctx, `SELECT r.status,r.completed_at FROM radishnexus.ci_runs r
	    JOIN radishnexus.components cmp ON (cmp.workspace_id,cmp.id)=(r.workspace_id,r.component_id)
	    WHERE r.workspace_id=$1 AND r.id=$2 FOR SHARE OF r,cmp`, p.WorkspaceID, runID).Scan(&status, &completed)
	if err != nil {
		return result, fmt.Errorf("read CI Run relation target: %w", err)
	}
	if !goldenpath.ValidBuildTerminalStatus(status) || completed == nil {
		return result, authz.ErrConflict
	}

	var currentState, cause string
	eventKind, state := goldenpath.TicketCIRunLinked, "active"
	if unlink {
		err = tx.QueryRow(ctx, `SELECT state,source_event_id FROM radishnexus.entity_links
		    WHERE workspace_id=$1 AND id=$2 AND from_id=$3 AND to_id=$4 AND relation_type='tracks-build' FOR UPDATE`,
			p.WorkspaceID, c.LinkID, c.TicketID, runID).Scan(&currentState, &cause)
		if err != nil {
			return result, fmt.Errorf("lock Ticket CI Run relation: %w", err)
		}
		eventKind, state = goldenpath.TicketCIRunUnlinked, "removed"
	}
	var at time.Time
	err = tx.QueryRow(ctx, `SELECT greatest(clock_timestamp(),$3::timestamptz,$4::timestamptz,
	    (SELECT max(updated_at)+interval '1 microsecond' FROM radishnexus.entity_links
	     WHERE workspace_id=$1 AND from_id=$2 AND relation_type='tracks-build'))`,
		p.WorkspaceID, c.TicketID, created, *completed).Scan(&at)
	if err != nil {
		return result, fmt.Errorf("determine Ticket CI Run relation time: %w", err)
	}

	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, c.Kind, "ticket", c.TicketID,
		c.ClientOperationID, c.Digest, "entity-link", c.ResultID, c.EventID, at)
	if err != nil {
		return result, err
	}
	if duplicate {
		var valid bool
		err = tx.QueryRow(ctx, `SELECT radishnexus.ticket_ci_run_operation_matches($1,$2) AND EXISTS(
		    SELECT 1 FROM radishnexus.entity_links WHERE workspace_id=$1 AND id=$3
		    AND relation_type='tracks-build' AND from_id=$4 AND to_id=$5)`,
			p.WorkspaceID, receipt.eventID, receipt.resultID, c.TicketID, runID).Scan(&valid)
		if err != nil {
			return result, fmt.Errorf("verify Ticket CI Run receipt: %w", err)
		}
		if !valid || (unlink && receipt.resultID != c.LinkID) {
			return result, errors.New("invalid Ticket CI Run receipt")
		}
		return goldenpath.TicketCIRunResult{LinkID: receipt.resultID}, tx.Commit(ctx)
	}
	if unlink && currentState != "active" {
		return result, authz.ErrConflict
	}
	facts := goldenpath.CIRunLinkFacts{
		Run:   entityref.Ref{Type: "ci-run", ID: runID},
		Link:  entityref.Ref{Type: "entity-link", ID: c.ResultID},
		State: state,
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: eventKind, WorkspaceID: p.WorkspaceID,
		ActorKind: "user", ActorID: p.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, ProjectID: project,
		PrimaryType: "ticket", PrimaryID: c.TicketID, CorrelationID: c.CorrelationID, CausationID: cause, OccurredAt: at, Payload: map[string]any{"ci_run": facts.Run, "link": facts.Link, "state": facts.State}}); err != nil {
		return result, err
	}
	if unlink {
		_, err = tx.Exec(ctx, `UPDATE radishnexus.entity_links SET state='removed',
		    removed_by_kind='user',removed_by_id=$3,removed_at=$4,updated_at=$4,removal_reason='ticket-ci-run-unlinked'
		    WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, c.LinkID, p.ID, at)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.entity_links
		    (id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,created_at,updated_at,source_event_id)
		    VALUES($1,$2,'ticket',$3,'tracks-build','ci-run',$4,'asserted','user','user',$5,$6,$6,$7)`,
			c.ResultID, p.WorkspaceID, c.TicketID, runID, p.ID, at, c.EventID)
	}
	if err != nil {
		return result, mapDatabaseError("change Ticket CI Run relation", err)
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit Ticket CI Run relation: %w", err)
	}
	return goldenpath.TicketCIRunResult{LinkID: c.ResultID, Created: !unlink}, nil
}

func (s *Store) ListCIRunTrace(ctx context.Context, p authz.Principal, kind, source string, input goldenpath.CIRunTraceInput) (page goldenpath.CIRunTracePage, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer rollback(ctx, tx, &err)
	switch kind {
	case goldenpath.TraceComponentRuns:
		err = requireTraceRead(ctx, tx, p, "component", source)
	case goldenpath.TraceTicketRuns, goldenpath.TraceTicketRunHistory:
		var access projectAccess
		_, access, err = ticketProjectAccess(ctx, tx, p, source)
		page.CanLink = access.active && access.role.CanContribute()
	case goldenpath.TraceRunTickets, goldenpath.TraceRunDeployments:
		err = requireTraceRead(ctx, tx, p, "ci-run", source)
	}
	if err != nil {
		return page, err
	}

	rows, err := queryCIRunTrace(ctx, tx, p, kind, source, input)
	if err != nil {
		return page, fmt.Errorf("query CI Run trace: %w", err)
	}
	defer rows.Close()
	page.Items = make([]goldenpath.CIRunTraceItem, 0, input.Limit)
	for rows.Next() {
		if len(page.Items) == input.Limit {
			last := page.Items[input.Limit-1]
			page.NextID = last.Ref.ID
			if last.OccurredAt != nil {
				page.NextTime = *last.OccurredAt
			}
			rows.Close()
			break
		}
		item, scanErr := scanCIRunTrace(rows, kind, page.CanLink)
		if scanErr != nil {
			return page, fmt.Errorf("scan CI Run trace: %w", scanErr)
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return page, fmt.Errorf("iterate CI Run trace: %w", err)
	}
	return page, tx.Commit(ctx)
}

// Component and Environment reads require active Workspace membership, checked
// by beginAdmission. The reverse list also applies the current Project predicate
// in SQL so hidden Tickets cannot fill a page or determine its cursor.
func queryCIRunTrace(ctx context.Context, tx pgx.Tx, p authz.Principal, kind, source string, in goldenpath.CIRunTraceInput) (pgx.Rows, error) {
	ws, after, limit := p.WorkspaceID, in.AfterID, in.Limit+1
	switch kind {
	case goldenpath.TraceComponentRuns:
		return tx.Query(ctx, `SELECT r.id,r.status,r.completed_at,c.id,c.name FROM radishnexus.ci_runs r
		 JOIN radishnexus.components c ON (c.workspace_id,c.id)=(r.workspace_id,r.component_id)
		 WHERE r.workspace_id=$1 AND r.component_id=$2 AND r.id>$3 AND r.status IN ('succeeded','failed','canceled')
		 ORDER BY r.id LIMIT $4`, ws, source, after, limit)
	case goldenpath.TraceTicketRuns:
		return tx.Query(ctx, `SELECT r.id,r.status,r.completed_at,c.id,c.name,l.id FROM radishnexus.entity_links l
		 JOIN radishnexus.ci_runs r ON (r.workspace_id,r.id)=(l.workspace_id,l.to_id)
		 JOIN radishnexus.components c ON (c.workspace_id,c.id)=(r.workspace_id,r.component_id)
		 WHERE l.workspace_id=$1 AND l.from_id=$2 AND l.to_id>$3 AND l.relation_type='tracks-build' AND l.state='active'
		 ORDER BY l.to_id LIMIT $4`, ws, source, after, limit)
	case goldenpath.TraceRunTickets:
		return tx.Query(ctx, `SELECT t.id,t.title,t.status,project.id FROM radishnexus.entity_links l
		 JOIN radishnexus.tickets t ON (t.workspace_id,t.id)=(l.workspace_id,l.from_id)
		 JOIN radishnexus.projects project ON (project.workspace_id,project.id)=(t.workspace_id,t.governing_project_id)
		 WHERE l.workspace_id=$1 AND l.to_id=$3 AND l.from_id>$4 AND l.relation_type='tracks-build' AND l.state='active'
		 AND `+readableProjectPredicate+` ORDER BY l.from_id LIMIT $5`, ws, p.ID, source, after, limit)
	case goldenpath.TraceRunDeployments:
		return tx.Query(ctx, `SELECT d.id,d.status,d.completed_at,e.id,e.name FROM radishnexus.deployments d
		 JOIN radishnexus.environments e ON (e.workspace_id,e.id)=(d.workspace_id,d.environment_id)
		 WHERE d.workspace_id=$1 AND d.ci_run_id=$2 AND d.id>$3 AND e.classification='staging'
		 ORDER BY d.id LIMIT $4`, ws, source, after, limit)
	case goldenpath.TraceTicketRunHistory:
		var before *time.Time
		if after != "" {
			before = &in.AfterTime
		}
		return tx.Query(ctx, `SELECT a.event_id,a.actor_id,a.occurred_at,r.id,a.safe_facts->>'link_id',a.safe_facts->>'state'
		 FROM radishnexus.activity_items a
		 JOIN radishnexus.ci_runs r ON r.workspace_id=a.workspace_id AND r.id=a.safe_facts->>'run_id'
		 JOIN radishnexus.components c ON (c.workspace_id,c.id)=(r.workspace_id,r.component_id)
		 WHERE a.workspace_id=$1 AND a.target_id=$2 AND a.target_type='ticket' AND a.projection_version=$3
		 AND a.activity_type IN ('ticket.ci-run-linked','ticket.ci-run-unlinked')
		 AND ($4::timestamptz IS NULL OR (a.occurred_at,a.event_id)<($4,$5))
		 ORDER BY a.occurred_at DESC,a.event_id DESC LIMIT $6`, ws, source, goldenpath.ActivityProjectionVersion, before, after, limit)
	default:
		return nil, authz.ErrInvalid
	}
}

func scanCIRunTrace(rows pgx.Rows, kind string, canLink bool) (item goldenpath.CIRunTraceItem, err error) {
	switch kind {
	case goldenpath.TraceComponentRuns, goldenpath.TraceTicketRuns:
		item.Ref.Type = "ci-run"
		item.Component = &goldenpath.TraceEntity{Ref: entityref.Ref{Type: "component"}}
		targets := []any{&item.Ref.ID, &item.Status, &item.CompletedAt, &item.Component.Ref.ID, &item.Component.Name}
		if kind == goldenpath.TraceTicketRuns {
			item.Link = &entityref.Ref{Type: "entity-link"}
			item.CanUnlink = &canLink
			targets = append(targets, &item.Link.ID)
		}
		err = rows.Scan(targets...)
	case goldenpath.TraceRunTickets:
		item.Ref.Type = "ticket"
		item.Project = &entityref.Ref{Type: "project"}
		err = rows.Scan(&item.Ref.ID, &item.Title, &item.Status, &item.Project.ID)
	case goldenpath.TraceRunDeployments:
		item.Ref.Type = "deployment"
		item.Environment = &goldenpath.TraceEntity{Ref: entityref.Ref{Type: "environment"}}
		err = rows.Scan(&item.Ref.ID, &item.Status, &item.CompletedAt, &item.Environment.Ref.ID, &item.Environment.Name)
	case goldenpath.TraceTicketRunHistory:
		item.Ref.Type = "event"
		item.Actor = &entityref.Ref{Type: "user"}
		item.Link = &entityref.Ref{Type: "entity-link"}
		item.Run = &entityref.Ref{Type: "ci-run"}
		err = rows.Scan(&item.Ref.ID, &item.Actor.ID, &item.OccurredAt, &item.Run.ID, &item.Link.ID, &item.RelationState)
	}
	if item.CompletedAt != nil {
		at := item.CompletedAt.UTC()
		item.CompletedAt = &at
	}
	if item.OccurredAt != nil {
		at := item.OccurredAt.UTC()
		item.OccurredAt = &at
	}

	return
}
