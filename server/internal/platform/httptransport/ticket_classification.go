package httptransport

import (
	"encoding/json"
	"net/http"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const (
	ticketClassificationPattern = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/classification"
	ticketLabelPattern          = "/api/v1/workspaces/{workspace_id}/projects/{project_id}/ticket-labels"
	ticketLabelRenamePattern    = ticketLabelPattern + "/{label_id}/rename"
	ticketLabelArchivePattern   = ticketLabelPattern + "/{label_id}/archive"
)

func RegisterTicketClassificationRoutes(mux *http.ServeMux, handler http.Handler) {
	for _, pattern := range []string{ticketClassificationPattern, ticketLabelPattern, ticketLabelRenamePattern, ticketLabelArchivePattern} {
		mux.Handle(pattern, handler)
	}
}

func NewTicketClassificationHandler(sessions MessagingSessionService, app *goldenpath.TicketClassificationService, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	for path, allowGet := range map[string]bool{ticketClassificationPattern: true, ticketLabelPattern: true, ticketLabelRenamePattern: false, ticketLabelArchivePattern: false} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" && !(allowGet && r.Method == "GET") {
				allow := "POST"
				if allowGet {
					allow = "GET, POST"
				}
				w.Header().Set("Allow", allow)
				writeIdentityError(w, r, ErrMethodNotAllowed)
				return
			}
			p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), r.Method == "POST", sessions, session, proxy)
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			var result any
			if path == ticketClassificationPattern {
				result, err = serveClassification(w, r, p, app)
			} else {
				result, err = serveLabels(w, r, p, app, path)
			}
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			writeIdentityJSON(w, r, http.StatusOK, map[string]any{"data": result})
		})
	}
	return privateNoStore(mux)
}

func serveClassification(w http.ResponseWriter, r *http.Request, p authz.Principal, app *goldenpath.TicketClassificationService) (any, error) {
	ticket := r.PathValue("ticket_id")
	if !validScopedID(ticket, "tkt_") || r.URL.RawQuery != "" {
		return nil, authz.ErrInvalid
	}
	if r.Method == "GET" {
		return app.ReadTicketClassification(r.Context(), p, ticket)
	}
	in := goldenpath.ClassificationInput{TicketID: ticket}
	raw, err := decodeStrictObject(w, r, []string{"ticket_type", "label_ids", "base_classification_revision", "client_operation_id", "confirmed"}, 8*1024)
	if err != nil {
		return nil, err
	}
	if err = decodeCommandFields(raw, map[string]any{"ticket_type": &in.Type, "label_ids": &in.LabelIDs, "base_classification_revision": &in.BaseRevision,
		"client_operation_id": &in.ClientOperationID, "confirmed": &in.Confirmed}); err != nil {
		return nil, err
	}
	revision, err := app.Set(r.Context(), webInvocation(p, r), in)
	return map[string]any{"applied": true, "classification_revision": revision}, err
}

func serveLabels(w http.ResponseWriter, r *http.Request, p authz.Principal, app *goldenpath.TicketClassificationService, path string) (any, error) {
	project, label := r.PathValue("project_id"), r.PathValue("label_id")
	if !validScopedID(project, "prj_") || (label != "" && !validScopedID(label, "lbl_")) {
		return nil, authz.ErrInvalid
	}
	if r.Method == "GET" {
		input, scope, status, err := parseLabelDirectoryQuery(r.URL.RawQuery, p, project)
		if err != nil {
			return nil, err
		}
		page, err := app.ListTicketLabels(r.Context(), p, project, status, input)
		if err != nil {
			return nil, err
		}
		next, err := nextDiscoveryCursor(scope, page.NextID)
		return map[string]any{"items": page.Items, "next_cursor": next, "can_manage": page.CanManage}, err
	}
	if r.URL.RawQuery != "" {
		return nil, authz.ErrInvalid
	}
	action := "create"
	if path == ticketLabelRenamePattern {
		action = "rename"
	}
	if path == ticketLabelArchivePattern {
		action = "archive"
	}
	in := goldenpath.LabelInput{ProjectID: project, LabelID: label, Action: action}
	fields := []string{"client_operation_id", "confirmed"}
	targets := map[string]any{"client_operation_id": &in.ClientOperationID, "confirmed": &in.Confirmed}
	if action != "create" {
		fields = append(fields, "base_revision")
		targets["base_revision"] = &in.BaseRevision
	}
	if action != "archive" {
		fields = append(fields, "name")
		targets["name"] = &in.Name
	}
	raw, err := decodeStrictObject(w, r, fields, 8*1024)
	if err != nil {
		return nil, err
	}
	if err = decodeCommandFields(raw, targets); err != nil {
		return nil, err
	}
	return app.WriteLabel(r.Context(), webInvocation(p, r), in)
}

func decodeCommandFields(raw map[string]json.RawMessage, targets map[string]any) error {
	for name, target := range targets {
		value := raw[name]
		if string(value) == "null" || json.Unmarshal(value, target) != nil {
			return authz.ErrInvalid
		}
	}
	return nil
}

func decodeInitialClassification(raw json.RawMessage) (*cp.Classification, error) {
	if raw == nil {
		return nil, nil
	}
	fields, err := decodeStrictObjectBytes(raw, []string{"ticket_type", "label_ids"})
	if err != nil {
		return nil, err
	}
	value := &cp.Classification{}
	if err = decodeCommandFields(fields, map[string]any{"ticket_type": &value.Type, "label_ids": &value.LabelIDs}); err != nil {
		return nil, err
	}
	return value, nil
}
