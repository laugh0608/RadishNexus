package httptransport

import (
	"errors"
	"net/http"
	"slices"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const (
	threadDeliveryTicketsPattern     = "/api/v1/workspaces/{workspace_id}/threads/{thread_id}/delivery-tickets"
	threadDeliveryRunsPattern        = threadDeliveryTicketsPattern + "/{ticket_id}/ci-runs"
	threadDeliveryDeploymentsPattern = threadDeliveryRunsPattern + "/{ci_run_id}/deployments"
)

var threadDeliveryPatterns = [...]string{threadDeliveryTicketsPattern, threadDeliveryRunsPattern, threadDeliveryDeploymentsPattern}

func RegisterThreadDeliveryRoutes(mux *http.ServeMux, handler http.Handler) {
	for _, pattern := range threadDeliveryPatterns {
		mux.Handle(pattern, handler)
	}
}

func NewThreadDeliveryHandler(sessions MessagingSessionService, app *goldenpath.ThreadDeliveryService, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeIdentityError(w, r, ErrMethodNotAllowed)
			return
		}
		workspace := r.PathValue("workspace_id")
		p, err := authenticateWorkspaceRequest(r, workspace, false, sessions, session, proxy)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if p.WorkspaceID != workspace {
			writeIdentityError(w, r, errors.New("Thread delivery Workspace scope mismatch"))
			return
		}
		data, err := readThreadDelivery(r, p, app)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		writeIdentityJSON(w, r, http.StatusOK, map[string]any{"data": data})
	}
	for _, pattern := range threadDeliveryPatterns {
		mux.HandleFunc(pattern, handler)
	}
	return privateNoStore(mux)
}

func readThreadDelivery(r *http.Request, p authz.Principal, app *goldenpath.ThreadDeliveryService) (any, error) {
	path := goldenpath.ThreadDeliveryScope{
		ThreadID: r.PathValue("thread_id"), TicketID: r.PathValue("ticket_id"), RunID: r.PathValue("ci_run_id"),
	}
	scope := discoveryCursor{
		Version: 1, WorkspaceID: p.WorkspaceID, ActorID: p.ID, Kind: path.Kind(),
		SourceID: path.ThreadID, TicketID: path.TicketID, CIRunID: path.RunID,
	}
	input, err := parseDiscoveryQuery(r.URL.RawQuery, scope)
	if err != nil {
		return nil, err
	}
	page, err := app.List(r.Context(), p, path, input)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid Thread delivery page")
	if page.Items == nil || len(page.Items) > input.Limit {
		return nil, invalid
	}
	items := make([]map[string]any, 0, len(page.Items))
	previous := input.AfterID
	for _, item := range page.Items {
		body, ok := publicThreadDeliveryItem(scope.Kind, item)
		if !ok || item.Ref.ID <= previous {
			return nil, invalid
		}
		items = append(items, body)
		previous = item.Ref.ID
	}
	if page.NextID != "" && (page.NextID != previous || len(items) != input.Limit) {
		return nil, invalid
	}
	next, err := nextDiscoveryCursor(scope, page.NextID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "next_cursor": next}, nil
}

func publicThreadDeliveryItem(kind string, item goldenpath.ThreadDeliveryItem) (map[string]any, bool) {
	body := map[string]any{"ref": item.Ref, "status": item.Status}
	if kind == goldenpath.ThreadDeliveryTickets {
		if !traceRef(&item.Ref, "ticket", "tkt_") || !validDiscoveryTitle(item.Title) ||
			!goldenpath.ValidTicketStatus(item.Status) || !validDeliveryOrigins(item.Origins) {
			return nil, false
		}
		body["title"], body["origins"] = item.Title, item.Origins
		return body, true
	}

	fact := goldenpath.CIRunTraceItem{
		Ref: item.Ref, Status: item.Status, CompletedAt: item.CompletedAt,
		Component: item.Component, Environment: item.Environment,
	}
	switch kind {
	case goldenpath.ThreadDeliveryRuns:
		if !validCIRunTraceItem(goldenpath.TraceComponentRuns, fact, false) {
			return nil, false
		}
		body["component"] = item.Component
	case goldenpath.ThreadDeliveryDeployments:
		if !validCIRunTraceItem(goldenpath.TraceRunDeployments, fact, false) {
			return nil, false
		}
		body["environment"] = item.Environment
	default:
		return nil, false
	}
	body["completed_at"] = publicTime(*item.CompletedAt)
	return body, true
}

func validDeliveryOrigins(origins []string) bool {
	order := [...]string{goldenpath.DeliveryDecisionOrigin, goldenpath.DeliveryMessageOrigin, goldenpath.DeliveryDiscussion}
	if len(origins) == 0 || len(origins) > 2 || (len(origins) == 2 && origins[1] != goldenpath.DeliveryDiscussion) {
		return false
	}
	previous := -1
	for _, origin := range origins {
		position := slices.Index(order[:], origin)
		if position <= previous {
			return false
		}
		previous = position
	}
	return true
}
