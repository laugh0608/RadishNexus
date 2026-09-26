package goldenpath

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/markdown"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type DocumentInput struct {
	TargetID          string
	ClientOperationID string
	Title             string
	BodyMarkdown      string
	FormatVersion     string
	BaseRevision      int
	RestoreRevision   int
}
type DocumentCommand struct {
	Invocation
	DocumentInput
	Kind          string
	DocumentID    string
	LinkID        string
	EventID       string
	PayloadSHA256 string
	OccurredAt    time.Time
}
type DocumentResult struct {
	Ref             entityref.Ref
	AppliedRevision int
	Created         bool
}
type RevisionConflict struct{ CurrentRevision int }

func (e *RevisionConflict) Error() string { return "Document base revision is stale" }
func (e *RevisionConflict) Unwrap() error { return authz.ErrConflict }

type DocumentRevision struct {
	Ref                  entityref.Ref
	Project              entityref.Ref
	Revision             int
	Title                string
	BodyMarkdown         string
	FormatVersion        string
	CreatedBy            string
	CreatedAt            time.Time
	DocumentCreatedAt    time.Time
	RestoredFromRevision *int
	View                 *markdown.View
	RenderingFailure     *markdown.Diagnostic
}
type DocumentView struct {
	Current   DocumentRevision
	Relations []RelationProjection
	Timeline  []TimelineItem
}
type DocumentPage struct {
	Items        []DocumentRevision
	NextID       string
	NextRevision int
}
type DocumentStore interface {
	WriteDocument(context.Context, DocumentCommand) (DocumentResult, error)
	ReadDocument(context.Context, authz.Principal, string, int) (DocumentView, error)
	ListDocuments(context.Context, authz.Principal, string, DiscoveryPageInput) (DocumentPage, error)
	ListDocumentRevisions(context.Context, authz.Principal, string, int, int) (DocumentPage, error)
	PreviewDocument(context.Context, authz.Principal, string, string, string) (markdown.View, error)
}
type DocumentService struct {
	store DocumentStore
	ids   IDGenerator
	clock Clock
}

func NewDocumentService(store DocumentStore, ids IDGenerator, clock Clock) *DocumentService {
	return &DocumentService{store, ids, clock}
}

func (s *DocumentService) WriteDocument(ctx context.Context, inv Invocation, kind string, in DocumentInput) (DocumentResult, error) {
	if err := validateInvocation(inv); err != nil {
		return DocumentResult{}, err
	}
	targetType := "document"
	if kind == "document.create" {
		targetType = "ticket"
	} else if kind != "document.save" && kind != "document.restore" {
		return DocumentResult{}, authz.ErrInvalid
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: targetType, ID: in.TargetID}); err != nil {
		return DocumentResult{}, authz.ErrInvalid
	}
	if !validClientOperationID(in.ClientOperationID) || (kind != "document.create" && (in.BaseRevision < 1 || in.BaseRevision > 2147483647)) {
		return DocumentResult{}, authz.ErrInvalid
	}
	if kind == "document.create" && (in.BaseRevision != 0 || in.RestoreRevision != 0) {
		return DocumentResult{}, authz.ErrInvalid
	}
	if kind == "document.restore" {
		if in.RestoreRevision < 1 || in.RestoreRevision > 2147483647 || in.Title != "" || in.BodyMarkdown != "" || in.FormatVersion != "" {
			return DocumentResult{}, authz.ErrInvalid
		}
	} else {
		if !utf8.ValidString(in.Title) || strings.IndexFunc(in.Title, unicode.IsControl) >= 0 || strings.ContainsAny(in.Title, "\u2028\u2029") {
			return DocumentResult{}, authz.ErrInvalid
		}
		in.Title = strings.TrimSpace(in.Title)
		if utf8.RuneCountInString(in.Title) < 1 || utf8.RuneCountInString(in.Title) > 200 || in.RestoreRevision != 0 {
			return DocumentResult{}, authz.ErrInvalid
		}
		normalized, err := markdown.Normalize(in.BodyMarkdown, in.FormatVersion)
		if err != nil {
			return DocumentResult{}, err
		}
		in.BodyMarkdown = normalized
	}
	// Markdown parsing happens inside the currently authorized store operation,
	// after exact receipt lookup, so an old retry never becomes a new save.
	digest := collaborationPayloadDigest(struct {
		Title, Body, Format string
		Base, Restore       int
	}{in.Title, in.BodyMarkdown, in.FormatVersion, in.BaseRevision, in.RestoreRevision})
	docID := in.TargetID
	var err error
	if kind == "document.create" {
		docID, err = s.ids.NewID("doc_")
		if err != nil {
			return DocumentResult{}, fmt.Errorf("generate Document ID: %w", err)
		}
	}
	eventID, err := s.ids.NewID("evt_")
	if err != nil {
		return DocumentResult{}, err
	}
	linkID := ""
	if kind == "document.create" {
		linkID, err = s.ids.NewID("lnk_")
		if err != nil {
			return DocumentResult{}, err
		}
	}
	return s.store.WriteDocument(ctx, DocumentCommand{Invocation: inv, DocumentInput: in, Kind: kind, DocumentID: docID, LinkID: linkID, EventID: eventID, PayloadSHA256: digest, OccurredAt: s.clock.Now().UTC()})
}
func validateDocumentRead(p authz.Principal, kind, id string) error {
	if err := p.ValidateUser(); err != nil {
		return err
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: kind, ID: id}); err != nil {
		return authz.ErrInvalid
	}
	return nil
}
func (s *DocumentService) ReadDocument(ctx context.Context, p authz.Principal, id string, revision int) (DocumentView, error) {
	if err := validateDocumentRead(p, "document", id); err != nil {
		return DocumentView{}, err
	}
	if revision < 0 || revision > 2147483647 {
		return DocumentView{}, authz.ErrInvalid
	}
	return s.store.ReadDocument(ctx, p, id, revision)
}
func (s *DocumentService) ListDocuments(ctx context.Context, p authz.Principal, project string, in DiscoveryPageInput) (DocumentPage, error) {
	if err := validateDocumentRead(p, "project", project); err != nil {
		return DocumentPage{}, err
	}
	if err := validateDiscoveryInput(p, "document", in); err != nil {
		return DocumentPage{}, err
	}
	return s.store.ListDocuments(ctx, p, project, in)
}
func (s *DocumentService) ListDocumentRevisions(ctx context.Context, p authz.Principal, id string, before, limit int) (DocumentPage, error) {
	if err := validateDocumentRead(p, "document", id); err != nil {
		return DocumentPage{}, err
	}
	if before < 0 || before > 2147483647 || limit < 1 || limit > 50 {
		return DocumentPage{}, authz.ErrInvalid
	}
	return s.store.ListDocumentRevisions(ctx, p, id, before, limit)
}
func (s *DocumentService) PreviewDocument(ctx context.Context, p authz.Principal, project, body, format string) (markdown.View, error) {
	if err := validateDocumentRead(p, "project", project); err != nil {
		return markdown.View{}, err
	}
	return s.store.PreviewDocument(ctx, p, project, body, format)
}
