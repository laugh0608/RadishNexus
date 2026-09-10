package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const (
	projectListPattern        = "/api/v1/workspaces/{workspace_id}/projects"
	projectChannelListPattern = projectListPattern + "/{project_id}/channels"
)

type DiscoveryReader interface {
	ListProjects(context.Context, authz.Principal, goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error)
	ListProjectChannels(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error)
}

type DiscoveryHandler struct {
	sessions WorkspaceSessionResolver
	reader   DiscoveryReader
	session  BrowserSessionPolicy
	proxy    TrustedProxyPolicy
}

type discoveryCursor struct {
	Version     int    `json:"v"`
	WorkspaceID string `json:"workspace_id"`
	Kind        string `json:"kind"`
	ProjectID   string `json:"project_id"`
	AfterID     string `json:"after_id"`
}

type discoveryItemDTO struct {
	Ref    entityRefDTO `json:"ref"`
	Title  string       `json:"title"`
	Status string       `json:"status"`
}

type discoveryPageDTO struct {
	Items      []discoveryItemDTO `json:"items"`
	NextCursor *string            `json:"next_cursor"`
}

func NewDiscoveryHandler(sessions WorkspaceSessionResolver, reader DiscoveryReader, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	handler := &DiscoveryHandler{sessions: sessions, reader: reader, session: session, proxy: proxy}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+projectListPattern, handler.list)
	mux.HandleFunc("GET "+projectChannelListPattern, handler.list)
	wrongMethod := func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Allow", http.MethodGet)
		handler.writeError(response, request, ErrMethodNotAllowed)
	}
	mux.HandleFunc(projectListPattern, wrongMethod)
	mux.HandleFunc(projectChannelListPattern, wrongMethod)
	mux.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		handler.writeError(response, request, authz.ErrNotFound)
	})
	return privateNoStore(mux)
}

func (handler *DiscoveryHandler) list(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		handler.writeError(response, request, ErrMethodNotAllowed)
		return
	}
	workspaceID, projectID := request.PathValue("workspace_id"), request.PathValue("project_id")
	if _, err := handler.proxy.ClientIP(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := handler.session.ValidateHost(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	token, err := handler.session.SessionToken(request)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if !validScopedID(workspaceID, "wrk_") || (projectID != "" && !validScopedID(projectID, "prj_")) {
		handler.writeError(response, request, fmt.Errorf("%w: invalid discovery scope", authz.ErrInvalid))
		return
	}
	verified, err := handler.sessions.ResolveWorkspace(request.Context(), token, workspaceID)
	if err != nil {
		if errors.Is(err, authz.ErrForbidden) {
			err = authz.ErrNotFound
		}
		handler.writeError(response, request, err)
		return
	}
	principal, err := authn.UserPrincipal(verified)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	kind := "project"
	if projectID != "" {
		kind = "channel"
	}
	scope := discoveryCursor{Version: 1, WorkspaceID: workspaceID, Kind: kind, ProjectID: projectID}
	input, err := parseDiscoveryQuery(request.URL.RawQuery, scope)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	var page goldenpath.DiscoveryPage
	if kind == "project" {
		page, err = handler.reader.ListProjects(request.Context(), principal, input)
	} else {
		page, err = handler.reader.ListProjectChannels(request.Context(), principal, projectID, input)
	}
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	dto, err := publicDiscoveryPage(page, input, scope)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	body, err := json.Marshal(struct {
		Data discoveryPageDTO `json:"data"`
	}{Data: dto})
	if err != nil {
		handler.writeError(response, request, fmt.Errorf("marshal discovery response: %w", err))
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(append(body, '\n')); err != nil {
		log.Printf("write discovery response request_id=%s: %v", RequestID(request.Context()), err)
	}
}

func parseDiscoveryQuery(raw string, scope discoveryCursor) (goldenpath.DiscoveryPageInput, error) {
	input := goldenpath.DiscoveryPageInput{Limit: 25}
	invalid := fmt.Errorf("%w: invalid discovery query or cursor", authz.ErrInvalid)
	if len(raw) > 2048 {
		return input, invalid
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return input, invalid
	}
	for name, values := range values {
		if (name != "limit" && name != "after") || len(values) != 1 || values[0] == "" {
			return input, invalid
		}
	}
	if value := values.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || strconv.Itoa(limit) != value || limit < 1 || limit > goldenpath.MaxDiscoveryPageSize {
			return input, invalid
		}
		input.Limit = limit
	}
	if value := values.Get("after"); value != "" {
		if len(value) > 1024 {
			return input, invalid
		}
		body, err := base64.RawURLEncoding.Strict().DecodeString(value)
		if err != nil {
			return input, invalid
		}
		var cursor discoveryCursor
		if err := json.Unmarshal(body, &cursor); err != nil {
			return input, invalid
		}
		// Only our canonical encoding is valid: rejects unknown/duplicate fields,
		// alternate encodings and trailing JSON without treating the cursor as auth.
		canonical, err := json.Marshal(cursor)
		if err != nil || base64.RawURLEncoding.EncodeToString(canonical) != value {
			return input, invalid
		}
		after := cursor.AfterID
		cursor.AfterID = ""
		if cursor != scope || !validScopedID(after, discoveryPrefix(scope.Kind)) {
			return input, invalid
		}
		input.AfterID = after
	}
	return input, nil
}

func publicDiscoveryPage(page goldenpath.DiscoveryPage, input goldenpath.DiscoveryPageInput, scope discoveryCursor) (discoveryPageDTO, error) {
	dto := discoveryPageDTO{Items: make([]discoveryItemDTO, 0, len(page.Items))}
	invalid := errors.New("discovery application returned an invalid page")
	if len(page.Items) > input.Limit {
		return dto, invalid
	}
	seen := make(map[string]bool, len(page.Items))
	for _, item := range page.Items {
		if item.Ref.Type != scope.Kind || !validScopedID(item.Ref.ID, discoveryPrefix(scope.Kind)) || seen[item.Ref.ID] || !utf8.ValidString(item.Title) || strings.TrimSpace(item.Title) == "" || strings.ContainsRune(item.Title, '\x00') || (item.Status != "active" && item.Status != "archived") {
			return dto, invalid
		}
		seen[item.Ref.ID] = true
		dto.Items = append(dto.Items, discoveryItemDTO{Ref: entityRefDTO{Type: item.Ref.Type, ID: item.Ref.ID}, Title: item.Title, Status: item.Status})
	}
	if page.NextID != "" {
		if len(page.Items) != input.Limit || page.NextID != page.Items[len(page.Items)-1].Ref.ID || page.NextID == input.AfterID {
			return dto, invalid
		}
		scope.AfterID = page.NextID
		body, err := json.Marshal(scope)
		if err != nil {
			return dto, err
		}
		cursor := base64.RawURLEncoding.EncodeToString(body)
		dto.NextCursor = &cursor
	}
	return dto, nil
}

func discoveryPrefix(kind string) string {
	if kind == "project" {
		return "prj_"
	}
	return "chn_"
}

func (handler *DiscoveryHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	if MapApplicationError(err).StatusCode == http.StatusInternalServerError {
		log.Printf("discovery failed request_id=%s: %v", RequestID(request.Context()), err)
	}
	if err := WriteError(response, RequestID(request.Context()), err); err != nil {
		log.Printf("write discovery error: %v", err)
	}
}
