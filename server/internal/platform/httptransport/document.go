package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/markdown"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const documentPrefix = "/api/v1/workspaces/{workspace_id}"

var documentRoutes = map[string]string{
	documentPrefix + "/projects/{project_id}/documents":              "GET",
	documentPrefix + "/projects/{project_id}/document-preview":       "POST",
	documentPrefix + "/tickets/{ticket_id}/documents":                "POST",
	documentPrefix + "/documents/{document_id}/nexus-view":           "GET",
	documentPrefix + "/documents/{document_id}/revisions":            "GET, POST",
	documentPrefix + "/documents/{document_id}/revisions/{revision}": "GET",
	documentPrefix + "/documents/{document_id}/restorations":         "POST",
}

type DocumentApplication interface {
	WriteDocument(context.Context, goldenpath.Invocation, string, goldenpath.DocumentInput) (goldenpath.DocumentResult, error)
	ReadDocument(context.Context, authz.Principal, string, int) (goldenpath.DocumentView, error)
	ListDocuments(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.DocumentPage, error)
	ListDocumentRevisions(context.Context, authz.Principal, string, int, int) (goldenpath.DocumentPage, error)
	PreviewDocument(context.Context, authz.Principal, string, string, string) (markdown.View, error)
}
type DocumentHandler struct {
	transport *CollaborationHandler
	app       DocumentApplication
}

func NewDocumentHandler(sessions MessagingSessionService, app DocumentApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	h := &DocumentHandler{transport: &CollaborationHandler{sessions: sessions, session: session, proxy: proxy}, app: app}
	mux := http.NewServeMux()
	for path, methods := range documentRoutes {
		for _, method := range strings.Split(methods, ", ") {
			mux.HandleFunc(method+" "+path, h.handle)
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", methods)
			h.fail(w, r, ErrMethodNotAllowed)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { h.fail(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}
func RegisterDocumentRoutes(mux *http.ServeMux, handler http.Handler) {
	for path := range documentRoutes {
		mux.Handle(path, handler)
	}
}

type documentRevisionDTO struct {
	Ref              entityRefDTO         `json:"ref"`
	Project          entityRefDTO         `json:"project"`
	Revision         int                  `json:"revision"`
	Title            string               `json:"title"`
	Body             string               `json:"body_markdown"`
	Format           string               `json:"format_version"`
	CreatedBy        deploymentActorDTO   `json:"created_by"`
	CreatedAt        string               `json:"created_at"`
	RestoredFrom     *int                 `json:"restored_from_revision"`
	View             *markdown.View       `json:"view"`
	RenderingFailure *markdown.Diagnostic `json:"rendering_failure"`
}

func documentRevision(v goldenpath.DocumentRevision) documentRevisionDTO {
	return documentRevisionDTO{Ref: publicRef(v.Ref), Project: publicRef(v.Project), Revision: v.Revision, Title: v.Title, Body: v.BodyMarkdown, Format: v.FormatVersion, CreatedBy: deploymentActorDTO{Kind: "user", ID: v.CreatedBy}, CreatedAt: publicTime(v.CreatedAt), RestoredFrom: v.RestoredFromRevision, View: v.View, RenderingFailure: v.RenderingFailure}
}

type documentListItemDTO struct {
	Ref       entityRefDTO `json:"ref"`
	Title     string       `json:"title"`
	Revision  int          `json:"revision"`
	CreatedAt string       `json:"created_at"`
	UpdatedAt string       `json:"updated_at"`
}
type documentHistoryItemDTO struct {
	Revision     int                `json:"revision"`
	Title        string             `json:"title"`
	CreatedBy    deploymentActorDTO `json:"created_by"`
	CreatedAt    string             `json:"created_at"`
	RestoredFrom *int               `json:"restored_from_revision"`
}
type documentTimelineDTO struct {
	ID           string                    `json:"id"`
	ActivityType string                    `json:"activity_type"`
	Actor        deploymentActorDTO        `json:"actor"`
	OccurredAt   string                    `json:"occurred_at"`
	Revision     int                       `json:"revision"`
	RestoredFrom *int                      `json:"restored_from_revision"`
	Subjects     []collaborationSubjectDTO `json:"subjects"`
}

func documentTimeline(items []goldenpath.TimelineItem) ([]documentTimelineDTO, error) {
	out := []documentTimelineDTO{}
	for _, item := range items {
		rev, err := strconv.Atoi(item.SafeFacts["revision"])
		if err != nil || rev < 1 || item.ProjectionVersion != goldenpath.ActivityProjectionVersion || (item.ActivityType != "document.created" && item.ActivityType != "document.revised") {
			return nil, errors.New("invalid Document Timeline")
		}
		entry := documentTimelineDTO{ID: item.EventID, ActivityType: item.ActivityType, Actor: deploymentActorDTO{Kind: item.Actor.Kind, ID: item.Actor.ID}, OccurredAt: publicTime(item.OccurredAt), Revision: rev, Subjects: []collaborationSubjectDTO{}}
		if value := item.SafeFacts["restored_from_revision"]; value != "" {
			r, e := strconv.Atoi(value)
			if e != nil || r < 1 || r >= rev {
				return nil, errors.New("invalid restore fact")
			}
			entry.RestoredFrom = &r
		}
		for _, subject := range item.Subjects {
			if subject.State == goldenpath.ProjectionRestricted {
				entry.Subjects = append(entry.Subjects, collaborationSubjectDTO{Visibility: "restricted"})
			} else if subject.State == goldenpath.ProjectionVisible {
				entity, e := requiredVisibleEntity(subject, "")
				if e != nil {
					return nil, e
				}
				entry.Subjects = append(entry.Subjects, collaborationSubjectDTO{Visibility: "readable", Entity: &entity})
			} else {
				return nil, errors.New("invalid Document subject")
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

func (h *DocumentHandler) handle(w http.ResponseWriter, r *http.Request) {
	workspace, id, project, ticket := r.PathValue("workspace_id"), r.PathValue("document_id"), r.PathValue("project_id"), r.PathValue("ticket_id")
	write := r.Method == http.MethodPost
	p, err := h.transport.authenticate(r, workspace, write)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	kind, target := "document", id
	if project != "" {
		kind, target = "project", project
	}
	if ticket != "" {
		kind, target = "ticket", ticket
	}
	if _, err = validateCollaborationPath(workspace, kind, target); err != nil {
		h.fail(w, r, err)
		return
	}
	if write {
		h.write(w, r, p, id, project, ticket)
		return
	}
	if project != "" || (strings.HasSuffix(r.URL.Path, "/revisions") && id != "") {
		h.list(w, r, p, id, project)
		return
	}
	if r.URL.RawQuery != "" {
		h.fail(w, r, authz.ErrInvalid)
		return
	}
	revision := 0
	if raw := r.PathValue("revision"); raw != "" {
		revision, err = strconv.Atoi(raw)
		if err != nil || revision < 1 || revision > 2147483647 || strconv.Itoa(revision) != raw {
			h.fail(w, r, authz.ErrInvalid)
			return
		}
	}
	view, err := h.app.ReadDocument(r.Context(), p, id, revision)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	current := documentRevision(view.Current)
	if revision != 0 {
		h.ok(w, r, http.StatusOK, current)
		return
	}
	relations, err := publicCollaborationRelations(view.Relations)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	timeline, err := documentTimeline(view.Timeline)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.ok(w, r, http.StatusOK, struct {
		Current   documentRevisionDTO        `json:"current"`
		Relations []collaborationRelationDTO `json:"relations"`
		Timeline  []documentTimelineDTO      `json:"timeline"`
	}{current, relations, timeline})
}

func (h *DocumentHandler) write(w http.ResponseWriter, r *http.Request, p authz.Principal, id, project, ticket string) {
	if r.URL.RawQuery != "" {
		h.fail(w, r, authz.ErrInvalid)
		return
	}
	fields := []string{"client_operation_id", "title", "body_markdown", "format_version"}
	command := "document.create"
	if id != "" {
		command = "document.save"
		fields = append(fields, "base_revision")
	}
	if strings.HasSuffix(r.URL.Path, "/restorations") {
		command = "document.restore"
		fields = []string{"client_operation_id", "base_revision", "restore_revision", "confirmed"}
	}
	if project != "" {
		fields = []string{"body_markdown", "format_version"}
	}
	raw, err := decodeStrictObject(w, r, fields, 2<<20)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in goldenpath.DocumentInput
	stringsTo := map[string]*string{"client_operation_id": &in.ClientOperationID, "title": &in.Title, "body_markdown": &in.BodyMarkdown, "format_version": &in.FormatVersion}
	for key, value := range raw {
		if string(value) == "null" {
			h.fail(w, r, authz.ErrInvalid)
			return
		}
		if dest, ok := stringsTo[key]; ok {
			err = decodeDocumentString(value, dest)
		} else if key == "base_revision" {
			err = json.Unmarshal(value, &in.BaseRevision)
		} else if key == "restore_revision" {
			err = json.Unmarshal(value, &in.RestoreRevision)
		} else {
			var confirmed bool
			err = json.Unmarshal(value, &confirmed)
			if !confirmed {
				err = authz.ErrInvalid
			}
		}
		if err != nil {
			h.fail(w, r, authz.ErrInvalid)
			return
		}
	}
	if project != "" {
		view, err := h.app.PreviewDocument(r.Context(), p, project, in.BodyMarkdown, in.FormatVersion)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		h.ok(w, r, http.StatusOK, view)
		return
	}
	in.TargetID = id
	if ticket != "" {
		in.TargetID = ticket
	}
	result, err := h.app.WriteDocument(r.Context(), webInvocation(p, r), command, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	h.ok(w, r, status, struct {
		Ref             entityRefDTO `json:"ref"`
		AppliedRevision int          `json:"applied_revision"`
	}{publicRef(result.Ref), result.AppliedRevision})
}

func (h *DocumentHandler) list(w http.ResponseWriter, r *http.Request, p authz.Principal, id, project string) {
	scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, Kind: "document", ProjectID: project}
	if id != "" {
		scope.Kind = "document-revision"
		scope.ProjectID = id
	}
	input, err := parseDiscoveryQuery(r.URL.RawQuery, scope)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !r.URL.Query().Has("limit") {
		input.Limit = 20
	}
	var page goldenpath.DocumentPage
	if id == "" {
		page, err = h.app.ListDocuments(r.Context(), p, project, input)
	} else {
		before := 0
		if input.AfterID != "" {
			before, err = strconv.Atoi(strings.TrimPrefix(input.AfterID, "rev_"))
			if err != nil || before < 1 || before > 2147483647 || "rev_"+strconv.Itoa(before) != input.AfterID {
				h.fail(w, r, authz.ErrInvalid)
				return
			}
		}
		page, err = h.app.ListDocumentRevisions(r.Context(), p, id, before, input.Limit)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var cursor *string
	if page.NextID != "" || page.NextRevision > 0 {
		scope.AfterID = page.NextID
		if page.NextRevision > 0 {
			scope.AfterID = "rev_" + strconv.Itoa(page.NextRevision)
		}
		body, e := json.Marshal(scope)
		if e != nil {
			h.fail(w, r, e)
			return
		}
		encoded := base64.RawURLEncoding.EncodeToString(body)
		cursor = &encoded
	}
	if id == "" {
		items := []documentListItemDTO{}
		for _, v := range page.Items {
			items = append(items, documentListItemDTO{publicRef(v.Ref), v.Title, v.Revision, publicTime(v.DocumentCreatedAt), publicTime(v.CreatedAt)})
		}
		h.ok(w, r, 200, struct {
			Items []documentListItemDTO `json:"items"`
			Next  *string               `json:"next_cursor"`
		}{items, cursor})
	} else {
		items := []documentHistoryItemDTO{}
		for _, v := range page.Items {
			items = append(items, documentHistoryItemDTO{v.Revision, v.Title, deploymentActorDTO{Kind: "user", ID: v.CreatedBy}, publicTime(v.CreatedAt), v.RestoredFromRevision})
		}
		h.ok(w, r, 200, struct {
			Items []documentHistoryItemDTO `json:"items"`
			Next  *string                  `json:"next_cursor"`
		}{items, cursor})
	}
}
func (h *DocumentHandler) ok(w http.ResponseWriter, r *http.Request, status int, value any) {
	h.transport.writeJSON(w, r, status, struct {
		Data any `json:"data"`
	}{value})
}
func (h *DocumentHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var conflict *goldenpath.RevisionConflict
	var diagnostic *markdown.Diagnostic
	if errors.As(err, &conflict) || errors.As(err, &diagnostic) {
		mapping := MapApplicationError(err)
		var rev *int
		if conflict != nil {
			rev = &conflict.CurrentRevision
		}
		h.transport.writeJSON(w, r, mapping.StatusCode, struct {
			Error struct {
				ErrorObject
				CurrentRevision *int                 `json:"current_revision,omitempty"`
				Diagnostic      *markdown.Diagnostic `json:"diagnostic,omitempty"`
			} `json:"error"`
		}{Error: struct {
			ErrorObject
			CurrentRevision *int                 `json:"current_revision,omitempty"`
			Diagnostic      *markdown.Diagnostic `json:"diagnostic,omitempty"`
		}{ErrorObject: ErrorObject{Code: mapping.Code, Message: mapping.Message, RequestID: RequestID(r.Context())}, CurrentRevision: rev, Diagnostic: diagnostic}})
		return
	}
	h.transport.writeError(w, r, err)
}

// encoding/json replaces malformed Unicode with U+FFFD. Authoritative Markdown
// must reject that lossy conversion, including unpaired JSON surrogate escapes.
func decodeDocumentString(raw []byte, dest *string) error {
	if !utf8.Valid(raw) {
		return authz.ErrInvalid
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return authz.ErrInvalid
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return authz.ErrInvalid
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return authz.ErrInvalid
		}
		i += 4
		if code >= 0xDC00 && code <= 0xDFFF {
			return authz.ErrInvalid
		}
		if code >= 0xD800 && code <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return authz.ErrInvalid
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return authz.ErrInvalid
			}
			i += 6
		}
	}
	return json.Unmarshal(raw, dest)
}
