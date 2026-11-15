package postgres

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func (s *Store) ReadTicketClassification(ctx context.Context, p authz.Principal, id string) (out goldenpath.TicketClassification, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	project, access, err := ticketProjectAccess(ctx, tx, p, id)
	if err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, `SELECT t.ticket_type,t.classification_revision,`+ticketLabelSummarySQL+`
		FROM radishnexus.tickets t WHERE t.workspace_id=$1 AND t.governing_project_id=$2 AND t.id=$3`, p.WorkspaceID, project, id).
		Scan(&out.Type, &out.Revision, &out.Labels); err != nil {
		return out, err
	}
	out.CanSet = access.active && access.role.CanContribute() && out.Revision < goldenpath.MaxTicketStatusRevision
	err = tx.Commit(ctx)
	return
}

// Used after scope authorization in the Ticket read and in bounded list projections.
const ticketLabelSummarySQL = `COALESCE((SELECT jsonb_agg(jsonb_build_object(
	'ref',jsonb_build_object('type','ticket-label','id',l.id),'name',l.name,'status',l.status) ORDER BY l.id COLLATE "C")
	FROM radishnexus.ticket_label_assignments a JOIN radishnexus.ticket_labels l
	  ON (l.workspace_id,l.project_id,l.id)=(a.workspace_id,a.project_id,a.label_id)
	WHERE (a.workspace_id,a.project_id,a.ticket_id)=(t.workspace_id,t.governing_project_id,t.id)), '[]'::jsonb)`

func (s *Store) SetTicketClassification(ctx context.Context, c goldenpath.ClassificationCommand) (revision int, err error) {
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer rollback(ctx, tx, &err)
	project, access, err := ticketProjectAccess(ctx, tx, c.Principal, c.TicketID)
	if err != nil {
		return 0, err
	}
	if err = authz.RequireContribute(access.role); err != nil {
		return 0, err
	}
	if !access.active {
		return 0, authz.ErrConflict
	}
	current, currentRev, at, err := lockTicketClassification(ctx, tx, c.Principal.WorkspaceID, project, c.TicketID)
	if err != nil {
		return 0, err
	}
	next := c.BaseRevision
	if next < goldenpath.MaxTicketStatusRevision {
		next++
	}
	if err = lockClassificationLabels(ctx, tx, c.Principal.WorkspaceID, project, current, c.Classification, &at, nil); err != nil {
		return 0, err
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, goldenpath.TicketClassificationSet, "ticket", c.TicketID, c.ClientOperationID, c.Digest, "ticket", c.TicketID, c.EventID, at, next)
	if err != nil {
		return 0, err
	}
	if duplicate {
		change, err := readClassificationEvent(ctx, tx, c.Invocation, receipt.eventID, project, c.TicketID, "")
		if err != nil {
			return 0, err
		}
		if receipt.resultID != c.TicketID || receipt.resultRevision == nil || *receipt.resultRevision != change.ToRevision ||
			change.ToRevision > currentRev || change.FromRevision != c.BaseRevision || !change.To.Equal(c.Classification) {
			return 0, errors.New("invalid Ticket classification receipt")
		}
		return change.ToRevision, tx.Commit(ctx)
	}
	if currentRev != c.BaseRevision || currentRev == goldenpath.MaxTicketStatusRevision || current.Equal(c.Classification) {
		return 0, authz.ErrConflict
	}
	if err = applyTicketClassification(ctx, tx, c.Invocation, project, c.TicketID, c.EventID, "", current, c.Classification, currentRev, at); err != nil {
		return 0, err
	}
	return next, tx.Commit(ctx)
}

func lockTicketClassification(ctx context.Context, tx pgx.Tx, ws, project, ticket string) (current cp.Classification, rev int, next time.Time, err error) {
	err = tx.QueryRow(ctx, `SELECT t.ticket_type,t.classification_revision,t.updated_at FROM radishnexus.tickets t
		WHERE (t.workspace_id,t.governing_project_id,t.id)=($1,$2,$3) FOR UPDATE`, ws, project, ticket).Scan(&current.Type, &rev, &next)
	if err != nil {
		return
	}
	err = tx.QueryRow(ctx, `SELECT ARRAY(SELECT label_id FROM radishnexus.ticket_label_assignments
		WHERE (workspace_id,project_id,ticket_id)=($1,$2,$3) ORDER BY label_id COLLATE "C"),
		greatest(clock_timestamp(),$4::timestamptz+interval '1 microsecond')`, ws, project, ticket, next).Scan(&current.LabelIDs, &next)
	return
}

// Locks the union in canonical ID order. Including removed labels preserves their
// names at the time of the change. A nil snapshots target allows receipt lookup
// to precede the active-label rule while keeping a single timestamp.
func lockClassificationLabels(ctx context.Context, tx pgx.Tx, ws, project string, before, after cp.Classification, at *time.Time, snapshots *[]cp.LabelVersion) error {
	ids := append(slices.Clone(before.LabelIDs), after.LabelIDs...)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	rows, err := tx.Query(ctx, `SELECT id,name,revision,status,updated_at FROM radishnexus.ticket_labels
		WHERE workspace_id=$1 AND project_id=$2 AND id=ANY($3) ORDER BY id COLLATE "C" FOR SHARE`, ws, project, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var snapshot cp.LabelVersion
		var state string
		var updated time.Time
		if err = rows.Scan(&snapshot.ID, &snapshot.Name, &snapshot.Revision, &state, &updated); err != nil {
			return err
		}
		count++
		if !at.After(updated) {
			*at = updated.Add(time.Microsecond)
		}
		if snapshots != nil {
			if state == "archived" && !slices.Contains(before.LabelIDs, snapshot.ID) {
				return authz.ErrConflict
			}
			*snapshots = append(*snapshots, snapshot)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count != len(ids) {
		return authz.ErrInvalid
	}
	return nil
}

func applyTicketClassification(ctx context.Context, tx pgx.Tx, inv goldenpath.Invocation, project, ticket, event, cause string, before, after cp.Classification, rev int, at time.Time) error {
	ws := inv.Principal.WorkspaceID
	snapshots := make([]cp.LabelVersion, 0)
	if err := lockClassificationLabels(ctx, tx, ws, project, before, after, &at, &snapshots); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE radishnexus.tickets SET ticket_type=$4,classification_revision=$5,updated_at=$6
		 WHERE (workspace_id,governing_project_id,id)=($1,$2,$3)`, ws, project, ticket, after.Type, rev+1, at); err != nil {
		return mapDatabaseError("update Ticket classification", err)
	}
	if err := storeTicketLabelAssignments(ctx, tx, ws, project, ticket, after.LabelIDs); err != nil {
		return err
	}
	return writeClassificationEvent(ctx, tx, inv, project, "ticket", ticket, event, cause, cp.TicketClassificationChanged, at,
		map[string]any{"classification": cp.ClassificationChange{From: before, To: after, FromRevision: rev, ToRevision: rev + 1, Labels: snapshots}})
}

func storeTicketLabelAssignments(ctx context.Context, tx pgx.Tx, ws, project, ticket string, ids []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM radishnexus.ticket_label_assignments WHERE (workspace_id,project_id,ticket_id)=($1,$2,$3) AND NOT(label_id=ANY($4))`, ws, project, ticket, ids); err != nil {
		return mapDatabaseError("remove Ticket label assignments", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.ticket_label_assignments(workspace_id,project_id,ticket_id,label_id)
		SELECT $1,$2,$3,unnest($4::text[]) ON CONFLICT DO NOTHING`, ws, project, ticket, ids); err != nil {
		return mapDatabaseError("write Ticket label assignments", err)
	}
	return nil
}

func writeClassificationEvent(ctx context.Context, tx pgx.Tx, inv goldenpath.Invocation, project, targetType, target, event, cause, kind string, at time.Time, facts map[string]any) error {
	if err := insertEvent(ctx, tx, eventRecord{
		ID: event, Type: kind, WorkspaceID: inv.Principal.WorkspaceID, ProjectID: project,
		PrimaryType: targetType, PrimaryID: target, ActorKind: "user", ActorID: inv.Principal.ID,
		SourceKind: inv.SourceKind, SourceID: inv.SourceID, CorrelationID: inv.CorrelationID, CausationID: cause,
		OccurredAt: at, Payload: facts,
	}); err != nil {
		return err
	}
	return insertOutbox(ctx, tx, event)
}

func readClassificationEvent(ctx context.Context, tx pgx.Tx, inv goldenpath.Invocation, event, project, ticket, creation string) (cp.ClassificationChange, error) {
	var payload []byte
	err := tx.QueryRow(ctx, `SELECT payload FROM radishnexus.domain_events WHERE workspace_id=$1 AND project_id=$2
		AND primary_entity_type='ticket' AND primary_entity_id=$3 AND actor_kind='user' AND actor_id=$4
		AND event_type='ticket.classification-changed' AND schema_version=1
		AND (($5<>'' AND event_id=$5) OR ($6<>'' AND causation_id=$6))`,
		inv.Principal.WorkspaceID, project, ticket, inv.Principal.ID, event, creation).Scan(&payload)
	if err != nil {
		return cp.ClassificationChange{}, err
	}
	return cp.DecodeClassificationChange(payload)
}

func verifyInitialClassification(ctx context.Context, tx pgx.Tx, inv goldenpath.Invocation, project, ticket, created string, expected *cp.Classification) error {
	if expected == nil {
		return nil
	}
	change, err := readClassificationEvent(ctx, tx, inv, "", project, ticket, created)
	if err != nil {
		return err
	}
	if change.FromRevision != 1 || !change.From.Equal(cp.EmptyClassification()) || !change.To.Equal(*expected) {
		return errors.New("invalid initial Ticket classification receipt")
	}
	return nil
}

func (s *Store) ListTicketLabels(ctx context.Context, p authz.Principal, project, status string, input goldenpath.DiscoveryPageInput) (out goldenpath.TicketLabelPage, err error) {
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	access, readable, err := readProjectAccess(ctx, tx, p, project)
	if err != nil {
		return out, err
	}
	if !readable {
		return out, authz.ErrNotFound
	}
	rows, err := tx.Query(ctx, `SELECT id,name,status,revision FROM radishnexus.ticket_labels
		WHERE workspace_id=$1 AND project_id=$2 AND ($3='all' OR status=$3) AND id COLLATE "C">$4
		ORDER BY id COLLATE "C" LIMIT $5`, p.WorkspaceID, project, status, input.AfterID, input.Limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Items = make([]goldenpath.TicketLabelEntry, 0)
	out.CanManage = access.active && access.role == authz.RoleAdmin
	for rows.Next() {
		var id string
		var state cp.LabelState
		if err = rows.Scan(&id, &state.Name, &state.Status, &state.Revision); err != nil {
			return out, err
		}
		out.Items = append(out.Items, ticketLabelEntry(id, state))
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > input.Limit {
		out.Items = out.Items[:input.Limit]
		out.NextID = out.Items[len(out.Items)-1].Ref.ID
	}
	return out, tx.Commit(ctx)
}

func ticketLabelEntry(id string, state cp.LabelState) goldenpath.TicketLabelEntry {
	return goldenpath.TicketLabelEntry{
		TicketLabel: goldenpath.TicketLabel{Ref: entityref.Ref{Type: "ticket-label", ID: id}, Name: state.Name, Status: state.Status},
		Revision:    state.Revision,
	}
}

func (s *Store) WriteTicketLabel(ctx context.Context, c goldenpath.LabelCommand) (out goldenpath.TicketLabelEntry, err error) {
	tx, err := s.beginAdmission(ctx, c.Principal, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer rollback(ctx, tx, &err)
	ws, project := c.Principal.WorkspaceID, c.ProjectID
	// Acquire the write lock before readProjectAccess takes its shared Project lock.
	var projectID string
	err = tx.QueryRow(ctx, `SELECT id FROM radishnexus.projects WHERE workspace_id=$1 AND id=$2 FOR NO KEY UPDATE`, ws, project).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, authz.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	access, readable, err := readProjectAccess(ctx, tx, c.Principal, projectID)
	if err != nil {
		return out, err
	}
	if !readable {
		return out, authz.ErrNotFound
	}
	if access.role != authz.RoleAdmin {
		return out, authz.ErrForbidden
	}
	if !access.active {
		return out, authz.ErrConflict
	}

	kind, eventType := "ticket-label."+c.Action, "ticket-label.created"
	targetType, target := "project", project
	state := cp.LabelState{Name: c.Name, Status: "active", Revision: 1}
	var prev *cp.LabelState
	var updated *time.Time
	if c.Action != "create" {
		targetType, target = "ticket-label", c.LabelID
		prev = &cp.LabelState{}
		err = tx.QueryRow(ctx, `SELECT name,status,revision,updated_at FROM radishnexus.ticket_labels
			WHERE (workspace_id,project_id,id)=($1,$2,$3) FOR UPDATE`, ws, project, c.LabelID).Scan(&prev.Name, &prev.Status, &prev.Revision, &updated)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, authz.ErrNotFound
		}
		if err != nil {
			return out, err
		}
		state.Revision = c.BaseRevision
		if state.Revision < goldenpath.MaxTicketStatusRevision {
			state.Revision++
		}
		eventType = "ticket-label.renamed"
		if c.Action == "archive" {
			state.Status, state.Name, eventType = "archived", prev.Name, "ticket-label.archived"
		}
	}
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT greatest(clock_timestamp(),$1::timestamptz+interval '1 microsecond',
		(SELECT max(occurred_at)+interval '1 microsecond' FROM radishnexus.domain_events
		 WHERE workspace_id=$2 AND project_id=$3 AND event_type IN ('ticket-label.created','ticket-label.renamed','ticket-label.archived','ticket.classification-changed') AND actor_kind='user'))`, updated, ws, project).Scan(&at); err != nil {
		return out, err
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, kind, targetType, target, c.ClientOperationID, c.Digest, "ticket-label", c.LabelID, c.EventID, at, state.Revision)
	if err != nil {
		return out, err
	}
	if duplicate {
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT payload FROM radishnexus.domain_events WHERE workspace_id=$1 AND project_id=$2
			AND event_id=$3 AND event_type=$4 AND schema_version=1 AND actor_kind='user' AND actor_id=$5
			AND primary_entity_type='ticket-label' AND primary_entity_id=$6`, ws, project, receipt.eventID, eventType, c.Principal.ID, receipt.resultID).Scan(&payload)
		if err != nil {
			return out, err
		}
		change, err := cp.DecodeLabelChange(payload, eventType)
		if err != nil || receipt.resultRevision == nil || *receipt.resultRevision != change.To.Revision ||
			(prev != nil && (receipt.resultID != c.LabelID || change.From.Revision != c.BaseRevision)) ||
			(c.Action != "archive" && change.To.Name != c.Name) {
			return out, errors.New("invalid Ticket label receipt")
		}
		return ticketLabelEntry(receipt.resultID, change.To), tx.Commit(ctx)
	}
	if prev != nil && (prev.Revision != c.BaseRevision || prev.Status != "active" || prev.Revision == state.Revision || *prev == (cp.LabelState{Name: state.Name, Status: state.Status, Revision: prev.Revision})) {
		return out, authz.ErrConflict
	}
	if prev == nil {
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.ticket_labels(id,workspace_id,project_id,name,status,revision,created_by,updated_by,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$8)`, c.LabelID, ws, project, state.Name, state.Status, state.Revision, c.Principal.ID, at)
	} else {
		_, err = tx.Exec(ctx, `UPDATE radishnexus.ticket_labels SET name=$4,status=$5,revision=$6,updated_by=$7,updated_at=$8
			WHERE (id,workspace_id,project_id)=($1,$2,$3)`, c.LabelID, ws, project, state.Name, state.Status, state.Revision, c.Principal.ID, at)
	}
	if err != nil {
		return out, mapDatabaseError("write Ticket label", err)
	}
	if err = writeClassificationEvent(ctx, tx, c.Invocation, project, "ticket-label", c.LabelID, c.EventID, "", eventType, at,
		map[string]any{"label": cp.LabelChange{From: prev, To: state}}); err != nil {
		return out, err
	}
	beforeStatus := ""
	if prev != nil {
		beforeStatus = prev.Status
	}
	if _, err = tx.Exec(ctx, `INSERT INTO radishnexus.workspace_configuration_audit
        (id,workspace_id,actor_id,command_kind,scope_id,subject_id,result_id,before_state,after_state,changed,request_id,occurred_at)
        VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,true,$9,$10)`, c.AuditID, ws, c.Principal.ID, kind, project, c.LabelID, beforeStatus, state.Status, c.CorrelationID, at); err != nil {
		return out, mapDatabaseError("audit Ticket label", err)
	}
	return ticketLabelEntry(c.LabelID, state), tx.Commit(ctx)
}
