package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/calendardate"
)

const (
	channelThreadListPattern   = "/api/v1/workspaces/{workspace_id}/channels/{channel_id}/threads"
	projectListPattern         = "/api/v1/workspaces/{workspace_id}/projects"
	projectChannelListPattern  = projectListPattern + "/{project_id}/channels"
	projectTicketListPattern   = projectListPattern + "/{project_id}/tickets"
	projectDecisionListPattern = projectListPattern + "/{project_id}/decisions"
	myTicketsListPattern       = "/api/v1/workspaces/{workspace_id}/my-tickets"
)

type DiscoveryReader interface {
	ListProjects(context.Context, authz.Principal, goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error)
	ListProjectChannels(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error)
	ListChannelThreads(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error)
	ListProjectTickets(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error)
	ListMyTickets(context.Context, authz.Principal, goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error)
	ListProjectDecisions(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.DiscoveryPage, error)
}

type DiscoveryHandler struct {
	sessions WorkspaceSessionResolver
	reader   DiscoveryReader
	session  BrowserSessionPolicy
	proxy    TrustedProxyPolicy
}

type discoveryCursor struct {
	ChannelID    string `json:"channel_id,omitempty"`
	Include      string `json:"include,omitempty"`
	SourceID     string `json:"source_id,omitempty"`
	FilterDigest string `json:"filter_digest,omitempty"`
	Status       string `json:"status,omitempty"`
	Assignee     string `json:"assignee,omitempty"`
	Priority     string `json:"priority,omitempty"`
	ActorID      string `json:"actor_id,omitempty"`
	CIRunID      string `json:"ci_run_id,omitempty"`
	TicketID     string `json:"ticket_id,omitempty"`
	Version      int    `json:"v"`
	WorkspaceID  string `json:"workspace_id"`
	Kind         string `json:"kind"`
	ProjectID    string `json:"project_id"`
	AfterID      string `json:"after_id"`
}

// Ticket cursor v2 requires even empty filters; v1 encodings for all other
// discovery kinds stay byte-for-byte compatible.
func (c discoveryCursor) MarshalJSON() ([]byte, error) {
	if c.Version == 2 {
		return json.Marshal(struct {
			Version     int    `json:"v"`
			WorkspaceID string `json:"workspace_id"`
			ProjectID   string `json:"project_id"`
			Kind        string `json:"kind"`
			ActorID     string `json:"actor_id"`
			Status      string `json:"status"`
			Assignee    string `json:"assignee"`
			Priority    string `json:"priority"`
			AfterID     string `json:"after_id"`
		}{c.Version, c.WorkspaceID, c.ProjectID, c.Kind, c.ActorID, c.Status, c.Assignee, c.Priority, c.AfterID})
	}
	type plain discoveryCursor
	return json.Marshal(plain(c))
}

type discoveryItemDTO struct {
	Classification *goldenpath.TicketClassificationSummary `json:"classification,omitempty"`
	DueDate        **string                                `json:"due_date,omitempty"`
	Project        *goldenpath.DiscoveryProject            `json:"project,omitempty"`
	Priority       string                                  `json:"priority,omitempty"`
	Assignee       **goldenpath.TicketAssignee             `json:"assignee,omitempty"`
	Ref            entityRefDTO                            `json:"ref"`
	Title          string                                  `json:"title"`
	Status         string                                  `json:"status,omitempty"`
}

type discoveryPageDTO struct {
	Items      []discoveryItemDTO `json:"items"`
	NextCursor *string            `json:"next_cursor"`
}

func NewDiscoveryHandler(sessions WorkspaceSessionResolver, reader DiscoveryReader, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	handler := &DiscoveryHandler{sessions: sessions, reader: reader, session: session, proxy: proxy}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+projectListPattern, handler.list)
	mux.HandleFunc("GET "+channelThreadListPattern, handler.list)
	mux.HandleFunc("GET "+projectChannelListPattern, handler.list)
	mux.HandleFunc("GET "+projectTicketListPattern, handler.list)
	mux.HandleFunc("GET "+projectDecisionListPattern, handler.list)
	mux.HandleFunc("GET "+myTicketsListPattern, handler.list)
	wrongMethod := func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Allow", http.MethodGet)
		handler.writeError(response, request, ErrMethodNotAllowed)
	}
	mux.HandleFunc(projectListPattern, wrongMethod)
	mux.HandleFunc(channelThreadListPattern, wrongMethod)
	mux.HandleFunc(projectChannelListPattern, wrongMethod)
	mux.HandleFunc(projectTicketListPattern, wrongMethod)
	mux.HandleFunc(projectDecisionListPattern, wrongMethod)
	mux.HandleFunc(myTicketsListPattern, wrongMethod)
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
	channelID := request.PathValue("channel_id")
	if (channelID != "" && !validScopedID(channelID, "chn_")) || !validScopedID(workspaceID, "wrk_") || (projectID != "" && !validScopedID(projectID, "prj_")) {
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
	switch request.Pattern {
	case "GET " + channelThreadListPattern:
		kind = "thread"
	case "GET " + projectChannelListPattern:
		kind = "channel"
	case "GET " + projectTicketListPattern:
		kind = "ticket"
	case "GET " + projectDecisionListPattern:
		kind = "decision"
	case "GET " + myTicketsListPattern:
		kind = "my-tickets"
	}
	scope := discoveryCursor{Version: 1, WorkspaceID: workspaceID, Kind: kind, ProjectID: projectID, ChannelID: channelID}
	if kind == "ticket" {
		scope.Version = 2
	}
	if kind == "thread" || kind == "ticket" || kind == "my-tickets" {
		scope.ActorID = principal.ID
	}
	input, err := parseDiscoveryQuery(request.URL.RawQuery, scope)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	scope = ticketDiscoveryScope(scope, input.TicketFilters)
	var page goldenpath.DiscoveryPage
	switch kind {
	case "thread":
		page, err = handler.reader.ListChannelThreads(request.Context(), principal, channelID, input)
	case "project":
		page, err = handler.reader.ListProjects(request.Context(), principal, input)
	case "ticket":
		page, err = handler.reader.ListProjectTickets(request.Context(), principal, projectID, input)
	case "my-tickets":
		page, err = handler.reader.ListMyTickets(request.Context(), principal, input)
	case "decision":
		page, err = handler.reader.ListProjectDecisions(request.Context(), principal, projectID, input)
	default:
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
		if name == "include" {
			if (scope.Kind != "ticket" && scope.Kind != "my-tickets") || len(values) != 1 || (values[0] != "due_date" && values[0] != "classification" && values[0] != "due_date,classification") {
				return input, invalid
			}
			input.IncludeDueDate = strings.Contains(values[0], "due_date")
			input.IncludeClassification = strings.Contains(values[0], "classification")
			continue
		}
		if (name != "limit" && name != "after" && !(scope.Version == 2 && scope.Kind == "ticket" && (name == "status" || name == "assignee" || name == "priority" || name == "type" || name == "labels" || name == "q")) && !(scope.Kind == "ticket-label" && name == "status")) || len(values) != 1 || values[0] == "" {
			return input, invalid
		}
	}
	if scope.Version == 2 && scope.Kind == "ticket" {
		input.TicketFilters = goldenpath.TicketFilters{Status: values.Get("status"), Assignee: values.Get("assignee"), Priority: values.Get("priority"), Type: values.Get("type"), Labels: values.Get("labels"), Title: values.Get("q")}
		input.TicketFilters.Normalize()
		if !input.TicketFilters.Valid() || (values.Has("q") && input.TicketFilters.Title == "") {
			return input, invalid
		}
		scope = ticketDiscoveryScope(scope, input.TicketFilters)
	}
	if scope.Kind == "ticket-label" {
		scope.Status = labelDirectoryStatus(values)
		if scope.Status != "active" && scope.Status != "archived" && scope.Status != "all" {
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
		if cursor != scope || !validDiscoveryAfterID(after, scope.Kind) {
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
	previousID := input.AfterID
	refKind := scope.Kind
	if scope.Kind == "my-tickets" {
		refKind = "ticket"
	}
	for _, item := range page.Items {
		validStatus := item.Status == "active" || item.Status == "archived"
		if scope.Kind == "ticket" {
			validStatus = goldenpath.ValidTicketStatus(item.Status)
		} else if scope.Kind == "my-tickets" {
			validStatus = item.Status == "open" || item.Status == "in-progress"
		} else if scope.Kind == "thread" {
			validStatus = item.Status == ""
		} else if scope.Kind == "decision" {
			validStatus = item.Status == "proposed" || item.Status == "accepted" || item.Status == "rejected" || item.Status == "superseded"
		}
		if item.Ref.Type != refKind || !validScopedID(item.Ref.ID, discoveryPrefix(refKind)) || seen[item.Ref.ID] || !validDiscoveryTitle(item.Title) || !validStatus || ((refKind == "ticket" || refKind == "decision" || refKind == "thread") && item.Ref.ID <= previousID) {
			return dto, invalid
		}
		previousID = item.Ref.ID
		seen[item.Ref.ID] = true
		if scope.Kind == "ticket" {
			if !goldenpath.ValidTicketPriority(item.Priority) || (input.TicketFilters.Priority != "" && item.Priority != input.TicketFilters.Priority) || (input.TicketFilters.Status == "active" && item.Status != "open" && item.Status != "in-progress") || (input.TicketFilters.Status != "" && input.TicketFilters.Status != "active" && item.Status != input.TicketFilters.Status) || (input.TicketFilters.Assignee == "unassigned" && item.Assignee != nil) || (input.TicketFilters.Assignee == "me" && (item.Assignee == nil || item.Assignee.ID != scope.ActorID)) {
				return dto, invalid
			}
		} else if scope.Kind == "my-tickets" {
			if !goldenpath.ValidTicketPriority(item.Priority) || item.Project == nil || !validScopedID(item.Project.ID, "prj_") || !validDiscoveryTitle(item.Project.Name) {
				return dto, invalid
			}
		} else if item.Priority != "" {
			return dto, invalid
		}
		if !calendardate.Valid(item.DueDate) || (refKind != "ticket" && item.DueDate != nil) {
			return dto, invalid
		}
		row := discoveryItemDTO{Priority: item.Priority, Ref: entityRefDTO{Type: item.Ref.Type, ID: item.Ref.ID}, Title: item.Title, Status: item.Status}
		if input.IncludeClassification && refKind == "ticket" {
			if !validClassificationSummary(item.Classification) {
				return dto, invalid
			}
			row.Classification = item.Classification
		}
		if input.IncludeDueDate && refKind == "ticket" {
			row.DueDate = &item.DueDate
		}
		if scope.Kind == "my-tickets" {
			row.Project = item.Project
		} else if item.Project != nil {
			return dto, invalid
		}
		if scope.Kind == "ticket" {
			if !validAssignee(item.Assignee) {
				return dto, invalid
			}
			row.Assignee = &item.Assignee
		} else if item.Assignee != nil {
			return dto, invalid
		}
		dto.Items = append(dto.Items, row)
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

func validDiscoveryTitle(title string) bool {
	return utf8.ValidString(title) && strings.TrimSpace(title) != "" && !strings.ContainsRune(title, '\x00')
}

func discoveryPrefix(kind string) string {
	if kind == "ticket-label" {
		return "lbl_"
	}
	switch kind {
	case "message-execution", "thread-execution", goldenpath.ThreadDeliveryTickets:
		return "tkt_"
	case "ticket-execution", "thread":
		return "thr_"
	}
	if strings.HasPrefix(kind, "management-") {
		return managementCursorPrefix(kind)
	}
	if kind == "owner-transfer-candidates" || kind == "ticket-assignee-candidates" {
		return "usr_"
	}
	if kind == "decision" {
		return "dec_"
	}
	if kind == "repository-ci-runs" || kind == goldenpath.TraceComponentRuns || kind == goldenpath.TraceTicketRuns || kind == goldenpath.ThreadDeliveryRuns {
		return "cir_"
	}
	if kind == goldenpath.TraceRunDeployments || kind == goldenpath.ThreadDeliveryDeployments {
		return "dpl_"
	}
	if kind == goldenpath.TraceRunTickets || kind == "component-tickets" || kind == "ticket" || kind == "my-tickets" {
		return "tkt_"
	}
	if kind == "repositories" || kind == "component-repositories" {
		return "rep_"
	}
	if kind == "components" || kind == "repository-components" || kind == "ticket-components" {
		return "cmp_"
	}
	if kind == "environment-authorizations" {
		return "usr_"
	}
	if kind == "staging-environment" || kind == "environments" {
		return "env_"
	}
	if kind == "document" {
		return "doc_"
	}
	if kind == "document-revision" {
		return "rev_"
	}
	if kind == "teams" {
		return "tem_"
	}
	if kind == "members" || kind == "project-members" || kind == "channel-members" {
		return "usr_"
	}
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

func validDiscoveryAfterID(id, kind string) bool {
	if kind == ticketTimelineKind {
		return validScopedID(id, "evt_")
	}
	if kind == goldenpath.TraceTicketRunHistory {
		_, _, err := parseCIRunHistoryBoundary(id)
		return err == nil
	}
	if kind == "management-responsibilities-workspace" {
		return validScopedID(id, "prj_") || validScopedID(id, "chn_") || validScopedID(id, "thr_")
	}
	return validScopedID(id, discoveryPrefix(kind))
}

func validClassificationSummary(summary *goldenpath.TicketClassificationSummary) bool {
	if summary == nil || summary.Labels == nil || !cp.ValidTicketType(summary.Type) {
		return false
	}
	previous := ""
	for _, label := range summary.Labels {
		if label.Ref.Type != "ticket-label" || !validScopedID(label.Ref.ID, "lbl_") || label.Ref.ID <= previous ||
			!cp.ValidTicketLabelName(label.Name) || (label.Status != "active" && label.Status != "archived") {
			return false
		}
		previous = label.Ref.ID
	}
	return true
}

func ticketDiscoveryScope(scope discoveryCursor, f goldenpath.TicketFilters) discoveryCursor {
	scope.Status, scope.Assignee, scope.Priority = f.Status, f.Assignee, f.Priority
	if f.HasClassificationFilters() {
		scope.Version = 3
		scope.FilterDigest = cp.Hash(cp.Encode(f))
	}
	return scope
}

func labelDirectoryStatus(values url.Values) string {
	if status := values.Get("status"); status != "" {
		return status
	}
	return "active"
}

func parseLabelDirectoryQuery(raw string, p authz.Principal, project string) (goldenpath.DiscoveryPageInput, discoveryCursor, string, error) {
	scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, ActorID: p.ID, ProjectID: project, Kind: "ticket-label"}
	input, err := parseDiscoveryQuery(raw, scope)
	if err != nil {
		return input, scope, "", err
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return input, scope, "", err
	}
	scope.Status = labelDirectoryStatus(values)
	return input, scope, scope.Status, nil
}

func nextDiscoveryCursor(scope discoveryCursor, id string) (*string, error) {
	if id == "" {
		return nil, nil
	}
	scope.AfterID = id
	body, err := json.Marshal(scope)
	if err != nil {
		return nil, err
	}
	cursor := base64.RawURLEncoding.EncodeToString(body)
	return &cursor, nil
}
