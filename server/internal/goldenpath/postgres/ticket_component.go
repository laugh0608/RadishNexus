package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

// The caller locks account and membership first. Locate the immutable Project
// before taking locks, then lock Project, role and Ticket in that order.
func ticketComponentAccess(ctx context.Context, tx pgx.Tx, p authz.Principal, id string) (ticket goldenpath.Ticket, writable bool, err error) {
	project, err := projectEntityScope(ctx, tx, p, "ticket", id)
	if err != nil {
		return ticket, false, err
	}
	access, readable, err := readProjectAccess(ctx, tx, p, project)
	if err != nil {
		return ticket, false, err
	}
	if !readable {
		return ticket, false, authz.ErrNotFound
	}
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM radishnexus.tickets WHERE workspace_id=$1 AND id=$2 FOR SHARE`, p.WorkspaceID, id).Scan(&locked); err != nil {
		return ticket, false, fmt.Errorf("lock Ticket relation source: %w", err)
	}
	ticket, err = loadTicket(ctx, tx, p.WorkspaceID, id)
	return ticket, access.active && authz.RequireContribute(access.role) == nil, err
}

func (s *Store) WriteTicketComponent(ctx context.Context, c goldenpath.TicketComponentCommand) (result goldenpath.TicketComponentResult, err error) {
	if err = c.Principal.ValidateUser(); err != nil {
		return result, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin Ticket Component command: %w", err)
	}
	defer rollback(ctx, tx, &err)
	if _, err = configurationActor(ctx, tx, c.Principal); err != nil {
		return result, err
	}
	ticket, writable, err := ticketComponentAccess(ctx, tx, c.Principal, c.TicketID)
	if err != nil {
		return result, err
	}
	componentID := c.ComponentID
	if c.Kind == "ticket.component.unlink" {
		err = tx.QueryRow(ctx, `SELECT to_id FROM radishnexus.entity_links WHERE workspace_id=$1 AND id=$2 AND from_type='ticket' AND from_id=$3 AND relation_type='affects' AND to_type='component'`, c.Principal.WorkspaceID, c.LinkID, c.TicketID).Scan(&componentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, authz.ErrNotFound
		}
		if err != nil {
			return result, fmt.Errorf("locate Ticket Component relation: %w", err)
		}
	}
	component, err := deliveryConfigurationObject(ctx, tx, c.Principal.WorkspaceID, "component", componentID, false, true)
	if err != nil {
		return result, err
	}
	if !writable {
		return result, authz.ErrForbidden
	}
	if c.Kind == "ticket.component.link" && component.Status == "retired" {
		return result, authz.ErrConflict
	}
	state := ""
	if c.Kind == "ticket.component.unlink" {
		err = tx.QueryRow(ctx, `SELECT state FROM radishnexus.entity_links WHERE workspace_id=$1 AND id=$2 AND from_type='ticket' AND from_id=$3 AND to_type='component' AND to_id=$4 AND relation_type='affects' FOR UPDATE`, c.Principal.WorkspaceID, c.LinkID, c.TicketID, componentID).Scan(&state)
		if err != nil {
			return result, fmt.Errorf("lock Ticket Component relation: %w", err)
		}
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, c.Kind, "ticket", c.TicketID, c.ClientOperationID, c.PayloadSHA256, "entity-link", c.ResultID, c.EventID, c.OccurredAt)
	if err != nil {
		return result, err
	}
	if duplicate {
		result.LinkID = receipt.resultID
		err = tx.Commit(ctx)
		return result, err
	}
	eventType, after := "ticket.component-linked", "active"
	if c.Kind == "ticket.component.link" {
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,'ticket',$3,'affects','component',$4,'asserted','user','user',$5,$6,$6)`, c.ResultID, c.Principal.WorkspaceID, c.TicketID, componentID, c.Principal.ID, c.OccurredAt)
	} else {
		if state != "active" {
			return result, authz.ErrConflict
		}
		eventType, after = "ticket.component-unlinked", "removed"
		_, err = tx.Exec(ctx, `UPDATE radishnexus.entity_links SET state='removed',removed_by_kind='user',removed_by_id=$1,removed_at=$2,updated_at=$2,removal_reason='ticket-component-unlinked' WHERE workspace_id=$3 AND id=$4`, c.Principal.ID, c.OccurredAt, c.Principal.WorkspaceID, c.ResultID)
	}
	if err != nil {
		return result, mapDatabaseError("change Ticket Component relation", err)
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: eventType, WorkspaceID: c.Principal.WorkspaceID, ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "ticket", PrimaryID: c.TicketID, ProjectID: ticket.GoverningProjectID, CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: c.OccurredAt, Payload: map[string]any{"component": entityref.Ref{Type: "component", ID: componentID}, "link_id": c.ResultID, "state": after}}); err != nil {
		return result, err
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, mapDatabaseError("commit Ticket Component relation", err)
	}
	return goldenpath.TicketComponentResult{LinkID: c.ResultID, Created: c.Kind == "ticket.component.link"}, nil
}

func (s *Store) ListTicketComponents(ctx context.Context, p authz.Principal, source entityref.Ref, in goldenpath.DiscoveryPageInput) (page goldenpath.TicketComponentPage, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, fmt.Errorf("begin Ticket Component list: %w", err)
	}
	defer rollback(ctx, tx, &err)
	if _, err = configurationActor(ctx, tx, p); err != nil {
		return page, err
	}
	if source.Type == "ticket" {
		_, page.CanLink, err = ticketComponentAccess(ctx, tx, p, source.ID)
		if err != nil {
			return page, err
		}
	} else {
		// Do not lock Component before the per-Ticket Project locks: writers acquire
		// these in the opposite order. Shared membership defines Component access.
		exists, e := entityExists(ctx, tx, "radishnexus.components", p.WorkspaceID, source.ID)
		if e != nil {
			return page, e
		}
		if !exists {
			return page, authz.ErrNotFound
		}
	}
	query := `SELECT to_id,id FROM radishnexus.entity_links WHERE workspace_id=$1 AND from_type='ticket' AND from_id=$2 AND relation_type='affects' AND to_type='component' AND state='active' AND to_id>$3 ORDER BY to_id LIMIT $4`
	if source.Type == "component" {
		query = `SELECT from_id,id FROM radishnexus.entity_links WHERE workspace_id=$1 AND to_type='component' AND to_id=$2 AND relation_type='affects' AND from_type='ticket' AND state='active' AND from_id>$3 ORDER BY from_id LIMIT $4`
	}
	page.Links = []goldenpath.TicketComponentLink{}
	after := in.AfterID
	for {
		rows, e := tx.Query(ctx, query, p.WorkspaceID, source.ID, after, in.Limit+1)
		if e != nil {
			return page, fmt.Errorf("list Ticket Component facts: %w", e)
		}
		type fact struct{ target, id string }
		facts := []fact{}
		for rows.Next() {
			var f fact
			if e = rows.Scan(&f.target, &f.id); e != nil {
				rows.Close()
				return page, fmt.Errorf("scan Ticket Component fact: %w", e)
			}
			facts = append(facts, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return page, fmt.Errorf("iterate Ticket Component facts: %w", e)
		}
		for _, f := range facts {
			after = f.target
			link := goldenpath.TicketComponentLink{ID: f.id, CanUnlink: page.CanLink}
			if source.Type == "ticket" {
				o, e := deliveryConfigurationObject(ctx, tx, p.WorkspaceID, "component", f.target, false, false)
				if errors.Is(e, authz.ErrNotFound) {
					continue
				}
				if e != nil {
					return page, e
				}
				link.Component = &o
			} else {
				t, canWrite, e := ticketComponentAccess(ctx, tx, p, f.target)
				if errors.Is(e, authz.ErrNotFound) {
					continue
				}
				if e != nil {
					return page, e
				}
				link.Ticket, link.CanUnlink = &t, canWrite
			}
			page.Links = append(page.Links, link)
			if len(page.Links) > in.Limit {
				page.Links = page.Links[:in.Limit]
				last := page.Links[in.Limit-1]
				if last.Component != nil {
					page.NextID = last.Component.ID
				} else {
					page.NextID = last.Ticket.ID
				}
				err = tx.Commit(ctx)
				return page, err
			}
		}
		if len(facts) < in.Limit+1 {
			err = tx.Commit(ctx)
			return page, err
		}
	}
}
