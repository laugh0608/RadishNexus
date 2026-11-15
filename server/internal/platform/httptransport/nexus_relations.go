package httptransport

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type collaborationReadOptions struct {
	summary, dueDate, execution, classification bool
}

func parseCollaborationReadOptions(raw, entityType string) (options collaborationReadOptions, err error) {
	query, err := url.ParseQuery(raw)
	if err != nil || (raw != "" && len(query) == 0) {
		return options, authz.ErrInvalid
	}
	for key, values := range query {
		if len(values) != 1 {
			return options, authz.ErrInvalid
		}
		switch key {
		case "relations":
			if values[0] != "summary" {
				return options, authz.ErrInvalid
			}
			options.summary = true
		case "include":
			if entityType != "ticket" || !validTicketViewInclude(values[0]) {
				return options, authz.ErrInvalid
			}
			options.dueDate = strings.Contains(values[0], "due_date")
			options.execution = strings.Contains(values[0], "execution_context")
			options.classification = strings.Contains(values[0], "classification")
		default:
			return options, authz.ErrInvalid
		}
	}
	return options, nil
}

type nexusRelationCursor struct {
	Version    int           `json:"v"`
	Kind       string        `json:"kind"`
	Workspace  string        `json:"workspace_id"`
	Actor      string        `json:"actor_id"`
	Source     entityref.Ref `json:"source"`
	Target     entityref.Ref `json:"target"`
	Generation string        `json:"generation"`
}

func (handler *CollaborationHandler) registerNexusRelations(mux *http.ServeMux) {
	routes := [...]struct{ pattern, kind string }{
		{threadViewPattern, "thread"},
		{decisionViewPattern, "decision"},
		{ticketViewPattern, "ticket"},
	}
	for _, route := range routes {
		path := route.pattern + "/relations"
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				handler.methodNotAllowed(w, r)
				return
			}
			handler.getNexusRelations(w, r, route.kind)
		})
		mux.HandleFunc(path, handler.methodNotAllowed)
	}
}

func (handler *CollaborationHandler) getNexusRelations(w http.ResponseWriter, r *http.Request, kind string) {
	workspace := r.PathValue("workspace_id")
	p, err := handler.authenticate(r, workspace, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	source, err := validateCollaborationPath(workspace, kind, r.PathValue(kind+"_id"))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	scope := nexusRelationCursor{Version: 1, Kind: "nexus-view-relations", Workspace: p.WorkspaceID, Actor: p.ID, Source: source}
	input, err := parseNexusRelationPage(r.URL.RawQuery, scope)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	page, err := handler.collaboration.ListNexusRelations(r.Context(), p, source, input)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items, next, err := publicNexusRelationPage(page, scope, input.Limit)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.writeJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{"items": items, "next_cursor": next}})
}

func parseNexusRelationPage(raw string, scope nexusRelationCursor) (input goldenpath.NexusRelationPageInput, err error) {
	input.Limit = 25
	if len(raw) > 2048 {
		return input, authz.ErrInvalid
	}
	query, err := url.ParseQuery(raw)
	if err != nil {
		return input, authz.ErrInvalid
	}
	for key, values := range query {
		if (key != "limit" && key != "after") || len(values) != 1 || values[0] == "" {
			return input, authz.ErrInvalid
		}
	}
	if rawLimit := query.Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || strconv.Itoa(limit) != rawLimit || limit < 1 || limit > goldenpath.MaxDiscoveryPageSize {
			return input, authz.ErrInvalid
		}
		input.Limit = limit
	}
	if token := query.Get("after"); token != "" {
		if len(token) > 1024 {
			return input, authz.ErrInvalid
		}
		raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil {
			return input, authz.ErrInvalid
		}
		var decoded nexusRelationCursor
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return input, authz.ErrInvalid
		}
		canonical, err := encodeNexusRelationCursor(decoded)
		if err != nil || canonical != token {
			return input, authz.ErrInvalid
		}
		boundary := goldenpath.NexusRelationBoundary{Target: decoded.Target, Generation: decoded.Generation}
		decoded.Target, decoded.Generation = entityref.Ref{}, ""
		if decoded != scope || !goldenpath.ValidNexusRelationBoundary(scope.Source.Type, boundary) {
			return input, authz.ErrInvalid
		}
		input.After = &boundary
	}
	return input, nil
}

func encodeNexusRelationCursor(cursor nexusRelationCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func publicNexusRelationPage(page goldenpath.NexusRelationPage, scope nexusRelationCursor, limit int) ([]collaborationRelationDTO, *string, error) {
	invalid := errors.New("invalid Nexus relations page")
	if page.Items == nil || len(page.Items) > limit {
		return nil, nil, invalid
	}
	seen := make(map[entityref.Ref]bool, len(page.Items))
	for _, item := range page.Items {
		direction, relation := goldenpath.NexusRelationDirection(scope.Source.Type, item.Target.Type)
		if direction == "" || item.State != goldenpath.ProjectionVisible ||
			item.Direction != direction || item.RelationType != relation || seen[item.Target] {
			return nil, nil, invalid
		}
		seen[item.Target] = true
	}
	items, err := publicCollaborationRelations(page.Items)
	if err != nil || page.Next == nil {
		return items, nil, err
	}
	if len(items) != limit || page.Next.Target != page.Items[len(items)-1].Target || !goldenpath.ValidNexusRelationBoundary(scope.Source.Type, *page.Next) {
		return nil, nil, invalid
	}
	scope.Target, scope.Generation = page.Next.Target, page.Next.Generation
	next, err := encodeNexusRelationCursor(scope)
	return items, &next, err
}
