package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/markdown"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

// Lock account and Workspace membership before Project, then role, then
// Document. Configuration writes lock the same Project before changing roles.
func documentProject(ctx context.Context, tx pgx.Tx, p authz.Principal, project string, write bool) error {
	if _, err := configurationActor(ctx, tx, p); err != nil {
		return err
	}
	access, readable, err := readProjectAccess(ctx, tx, p, project)
	if err != nil {
		return err
	}
	if !readable {
		return authz.ErrNotFound
	}
	if write {
		if !access.active {
			return authz.ErrForbidden
		}
		return authz.RequireContribute(access.role)
	}
	return nil
}
func documentScope(ctx context.Context, tx pgx.Tx, p authz.Principal, kind, id string) (string, error) {
	table := "radishnexus.documents"
	if kind == "ticket" {
		table = "radishnexus.tickets"
	}
	var project string
	err := tx.QueryRow(ctx, "SELECT governing_project_id FROM "+table+" WHERE workspace_id=$1 AND id=$2", p.WorkspaceID, id).Scan(&project)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", authz.ErrNotFound
	}
	return project, err
}

func (s *Store) WriteDocument(ctx context.Context, c goldenpath.DocumentCommand) (result goldenpath.DocumentResult, err error) {
	if err = c.Principal.ValidateUser(); err != nil {
		return result, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollback(ctx, tx, &err)
	targetType := "document"
	if c.Kind == "document.create" {
		targetType = "ticket"
	}
	project, err := documentScope(ctx, tx, c.Principal, targetType, c.TargetID)
	if err != nil {
		return result, err
	}
	if err = documentProject(ctx, tx, c.Principal, project, true); err != nil {
		return result, err
	}
	// Creation receipts serialize before creating a new identity. Existing
	// Documents serialize before checking either receipt or base revision.
	current := 0
	if targetType == "document" {
		err = tx.QueryRow(ctx, `SELECT current_revision FROM radishnexus.documents WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Principal.WorkspaceID, c.TargetID).Scan(&current)
		if err != nil {
			return result, err
		}
	}
	receipt, duplicate, err := claimCollaborationCommand(ctx, tx, c.Invocation, c.Kind, targetType, c.TargetID, c.ClientOperationID, c.PayloadSHA256, "document", c.DocumentID, c.EventID, c.OccurredAt, min(current+1, 2147483647))
	if err != nil {
		return result, err
	}
	if duplicate {
		result = goldenpath.DocumentResult{Ref: entityref.Ref{Type: "document", ID: receipt.resultID}, AppliedRevision: *receipt.resultRevision}
		err = tx.Commit(ctx)
		return result, err
	}
	if targetType == "document" && c.BaseRevision != current {
		return result, &goldenpath.RevisionConflict{CurrentRevision: current}
	}
	if current == 2147483647 {
		return result, authz.ErrConflict
	}
	var restored *int
	if c.Kind == "document.restore" {
		prior, loadErr := loadDocumentRevision(ctx, tx, c.Principal.WorkspaceID, c.TargetID, c.RestoreRevision)
		if loadErr != nil {
			return result, loadErr
		}
		c.Title = prior.Title
		c.BodyMarkdown = prior.BodyMarkdown
		c.FormatVersion = prior.FormatVersion
		restored = &c.RestoreRevision
	}
	if _, err = markdown.Parse(c.BodyMarkdown, c.FormatVersion); err != nil {
		return result, err
	}
	revision := current + 1
	if current == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.documents(id,workspace_id,governing_project_id,current_revision,created_by,created_at) VALUES($1,$2,$3,1,$4,$5)`, c.DocumentID, c.Principal.WorkspaceID, project, c.Principal.ID, c.OccurredAt)
	} else {
		_, err = tx.Exec(ctx, `UPDATE radishnexus.documents SET current_revision=$3 WHERE workspace_id=$1 AND id=$2`, c.Principal.WorkspaceID, c.DocumentID, revision)
	}
	if err != nil {
		return result, mapDatabaseError("write Document", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO radishnexus.document_revisions(workspace_id,document_id,revision,title,body_markdown,format_version,created_by,created_at,source_event_id,restored_from_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.Principal.WorkspaceID, c.DocumentID, revision, c.Title, c.BodyMarkdown, c.FormatVersion, c.Principal.ID, c.OccurredAt, c.EventID, restored)
	if err != nil {
		return result, mapDatabaseError("append Document revision", err)
	}
	eventType := "document.revised"
	payload := map[string]any{"revision": revision}
	if current == 0 {
		eventType = "document.created"
		payload["ticket"] = entityref.Ref{Type: "ticket", ID: c.TargetID}
	}
	if restored != nil {
		payload["restored_from_revision"] = *restored
	}
	if err = insertEvent(ctx, tx, eventRecord{ID: c.EventID, Type: eventType, WorkspaceID: c.Principal.WorkspaceID, ActorKind: "user", ActorID: c.Principal.ID, SourceKind: c.SourceKind, SourceID: c.SourceID, PrimaryType: "document", PrimaryID: c.DocumentID, ProjectID: project, CorrelationID: c.CorrelationID, CausationID: c.CausationID, OccurredAt: c.OccurredAt, Payload: payload}); err != nil {
		return result, err
	}
	if current == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,created_by_kind,created_by_id,created_at,updated_at,source_event_id) VALUES($1,$2,'ticket',$3,'relates-to','document',$4,'asserted','user','user',$5,$6,$6,$7)`, c.LinkID, c.Principal.WorkspaceID, c.TargetID, c.DocumentID, c.Principal.ID, c.OccurredAt, c.EventID)
		if err != nil {
			return result, mapDatabaseError("link Ticket to Document", err)
		}
	}
	if err = insertOutbox(ctx, tx, c.EventID); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, mapDatabaseError("commit Document", err)
	}
	return goldenpath.DocumentResult{Ref: entityref.Ref{Type: "document", ID: c.DocumentID}, AppliedRevision: revision, Created: current == 0}, nil
}

func loadDocumentRevision(ctx context.Context, tx pgx.Tx, workspace, id string, revision int) (v goldenpath.DocumentRevision, err error) {
	v.Ref = entityref.Ref{Type: "document", ID: id}
	v.Project.Type = "project"
	err = tx.QueryRow(ctx, `SELECT d.governing_project_id,r.revision,r.title,r.body_markdown,r.format_version,r.created_by,r.created_at,r.restored_from_revision,d.created_at FROM radishnexus.documents d JOIN radishnexus.document_revisions r ON r.workspace_id=d.workspace_id AND r.document_id=d.id AND r.revision=CASE WHEN $3::integer=0 THEN d.current_revision ELSE $3 END WHERE d.workspace_id=$1 AND d.id=$2`, workspace, id, revision).Scan(&v.Project.ID, &v.Revision, &v.Title, &v.BodyMarkdown, &v.FormatVersion, &v.CreatedBy, &v.CreatedAt, &v.RestoredFromRevision, &v.DocumentCreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, authz.ErrNotFound
	}
	if err != nil {
		return v, fmt.Errorf("load Document revision: %w", err)
	}
	return v, nil
}
func (s *Store) ReadDocument(ctx context.Context, p authz.Principal, id string, revision int) (view goldenpath.DocumentView, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return view, err
	}
	defer rollback(ctx, tx, &err)
	project, err := documentScope(ctx, tx, p, "document", id)
	if err != nil {
		return view, err
	}
	if err = documentProject(ctx, tx, p, project, false); err != nil {
		return view, err
	}
	view.Current, err = loadDocumentRevision(ctx, tx, p.WorkspaceID, id, revision)
	if err != nil {
		return view, err
	}
	projection, parseErr := markdown.Parse(view.Current.BodyMarkdown, view.Current.FormatVersion)
	if parseErr != nil {
		var diagnostic *markdown.Diagnostic
		if !errors.As(parseErr, &diagnostic) {
			return view, parseErr
		}
		view.Current.RenderingFailure = diagnostic
	} else {
		view.Current.View = &projection
	}
	if revision == 0 {
		view.Relations, err = listRelationProjections(ctx, tx, p, view.Current.Ref)
		if err != nil {
			return view, err
		}
		view.Timeline, err = listTimeline(ctx, tx, p, view.Current.Ref)
		if err != nil {
			return view, err
		}
	}
	err = tx.Commit(ctx)
	return view, err
}
func (s *Store) PreviewDocument(ctx context.Context, p authz.Principal, project, body, format string) (view markdown.View, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return view, err
	}
	defer rollback(ctx, tx, &err)
	if err = documentProject(ctx, tx, p, project, true); err != nil {
		return view, err
	}
	view, err = markdown.Parse(body, format)
	if err != nil {
		return view, err
	}
	err = tx.Commit(ctx)
	return view, err
}

func (s *Store) ListDocuments(ctx context.Context, p authz.Principal, project string, in goldenpath.DiscoveryPageInput) (page goldenpath.DocumentPage, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer rollback(ctx, tx, &err)
	if err = documentProject(ctx, tx, p, project, false); err != nil {
		return page, err
	}
	rows, err := tx.Query(ctx, `SELECT d.id,r.title,r.revision,d.created_at,r.created_at FROM radishnexus.documents d JOIN radishnexus.document_revisions r ON r.workspace_id=d.workspace_id AND r.document_id=d.id AND r.revision=d.current_revision WHERE d.workspace_id=$1 AND d.governing_project_id=$2 AND d.id>$3 ORDER BY d.id LIMIT $4`, p.WorkspaceID, project, in.AfterID, in.Limit+1)
	if err != nil {
		return page, err
	}
	page.Items = []goldenpath.DocumentRevision{}
	for rows.Next() {
		v := goldenpath.DocumentRevision{Ref: entityref.Ref{Type: "document"}, Project: entityref.Ref{Type: "project", ID: project}}
		if err = rows.Scan(&v.Ref.ID, &v.Title, &v.Revision, &v.DocumentCreatedAt, &v.CreatedAt); err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > in.Limit {
		page.Items = page.Items[:in.Limit]
		page.NextID = page.Items[in.Limit-1].Ref.ID
	}
	err = tx.Commit(ctx)
	return page, err
}
func (s *Store) ListDocumentRevisions(ctx context.Context, p authz.Principal, id string, before, limit int) (page goldenpath.DocumentPage, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, err
	}
	defer rollback(ctx, tx, &err)
	project, err := documentScope(ctx, tx, p, "document", id)
	if err != nil {
		return page, err
	}
	if err = documentProject(ctx, tx, p, project, false); err != nil {
		return page, err
	}
	rows, err := tx.Query(ctx, `SELECT revision,title,created_by,created_at,restored_from_revision FROM radishnexus.document_revisions WHERE workspace_id=$1 AND document_id=$2 AND ($3::integer=0 OR revision<$3) ORDER BY revision DESC LIMIT $4`, p.WorkspaceID, id, before, limit+1)
	if err != nil {
		return page, err
	}
	page.Items = []goldenpath.DocumentRevision{}
	for rows.Next() {
		v := goldenpath.DocumentRevision{Ref: entityref.Ref{Type: "document", ID: id}}
		if err = rows.Scan(&v.Revision, &v.Title, &v.CreatedBy, &v.CreatedAt, &v.RestoredFromRevision); err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextRevision = page.Items[limit-1].Revision
	}
	err = tx.Commit(ctx)
	return page, err
}
