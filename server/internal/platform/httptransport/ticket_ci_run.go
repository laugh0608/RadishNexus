package httptransport

import (
	"errors"
	"net/http"
	"strings"
	"time"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	componentRunsPattern    = "/api/v1/workspaces/{workspace_id}/components/{component_id}/ci-runs"
	ticketRunsPattern       = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/ci-runs"
	ticketRunHistoryPattern = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/ci-run-link-history"
	ticketRunLinkPattern    = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/ci-run-links/{link_id}"
	runTicketsPattern       = "/api/v1/workspaces/{workspace_id}/ci-runs/{ci_run_id}/tickets"
	runDeploymentsPattern   = "/api/v1/workspaces/{workspace_id}/ci-runs/{ci_run_id}/deployments"
)

type ciRunTraceRoute struct{ kind, source, methods string }

var ciRunTraceRoutes = map[string]ciRunTraceRoute{
	componentRunsPattern:    {goldenpath.TraceComponentRuns, "component_id", "GET"},
	ticketRunsPattern:       {goldenpath.TraceTicketRuns, "ticket_id", "GET, POST"},
	ticketRunHistoryPattern: {goldenpath.TraceTicketRunHistory, "ticket_id", "GET"},
	ticketRunLinkPattern:    {"", "ticket_id", "DELETE"},
	runTicketsPattern:       {goldenpath.TraceRunTickets, "ci_run_id", "GET"},
	runDeploymentsPattern:   {goldenpath.TraceRunDeployments, "ci_run_id", "GET"},
}

func RegisterTicketCIRunRoutes(mux *http.ServeMux, handler http.Handler) {
	for pattern := range ciRunTraceRoutes {
		mux.Handle(pattern, handler)
	}
}

func NewTicketCIRunHandler(sessions MessagingSessionService, app *goldenpath.TicketCIRunService, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	for pattern, route := range ciRunTraceRoutes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			method := r.Method
			switch method {
			case "GET":
				if route.kind == "" {
					method = ""
				}
			case "POST":
				if route.kind != goldenpath.TraceTicketRuns {
					method = ""
				}
			case "DELETE":
				if route.kind != "" {
					method = ""
				}
			default:
				method = ""
			}
			if method == "" {
				w.Header().Set("Allow", route.methods)
				writeIdentityError(w, r, ErrMethodNotAllowed)
				return
			}
			p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), method != "GET", sessions, session, proxy)
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			if p.WorkspaceID != r.PathValue("workspace_id") {
				writeIdentityError(w, r, errors.New("CI Run trace Workspace scope mismatch"))
				return
			}
			source := r.PathValue(route.source)
			var data any
			status := http.StatusOK
			if method == "GET" {
				data, err = readCIRunTrace(r, p, app, route.kind, source)
			} else {
				data, status, err = writeTicketCIRun(w, r, p, app, source)
			}
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			writeIdentityJSON(w, r, status, map[string]any{"data": data})
		})
	}
	return privateNoStore(mux)
}

func writeTicketCIRun(w http.ResponseWriter, r *http.Request, p authz.Principal, app *goldenpath.TicketCIRunService, ticket string) (any, int, error) {
	if r.URL.RawQuery != "" {
		return nil, 0, authz.ErrInvalid
	}
	in := goldenpath.TicketCIRunInput{TicketID: ticket}
	fields := []string{"client_operation_id", "confirmed"}
	targets := map[string]any{"client_operation_id": &in.ClientOperationID, "confirmed": &in.Confirmed}
	kind := goldenpath.TicketCIRunUnlink
	if r.Method == "POST" {
		kind = goldenpath.TicketCIRunLink
		fields = append(fields, "ci_run_id")
		targets["ci_run_id"] = &in.RunID
	} else {
		in.LinkID = r.PathValue("link_id")
	}
	raw, err := decodeStrictObject(w, r, fields, 8*1024)
	if err != nil {
		return nil, 0, err
	}
	if err = decodeCommandFields(raw, targets); err != nil {
		return nil, 0, err
	}
	result, err := app.Write(r.Context(), webInvocation(p, r), kind, in)
	if err != nil {
		return nil, 0, err
	}
	if !cp.ValidID(result.LinkID, "lnk_") || (kind == goldenpath.TicketCIRunUnlink && (result.Created || result.LinkID != in.LinkID)) {
		return nil, 0, errors.New("invalid Ticket CI Run result")
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	return map[string]any{"link_id": result.LinkID, "applied": true}, status, nil
}

func readCIRunTrace(r *http.Request, p authz.Principal, app *goldenpath.TicketCIRunService, kind, source string) (any, error) {
	scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, ActorID: p.ID, Kind: kind, SourceID: source}
	q, err := parseDiscoveryQuery(r.URL.RawQuery, scope)
	if err != nil {
		return nil, err
	}
	in := goldenpath.CIRunTraceInput{DiscoveryPageInput: q}
	if kind == goldenpath.TraceTicketRunHistory && q.AfterID != "" {
		in.AfterID, in.AfterTime, err = parseCIRunHistoryBoundary(q.AfterID)
		if err != nil {
			return nil, err
		}
	}
	page, err := app.List(r.Context(), p, kind, source, in)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid CI Run trace page")
	if page.Items == nil || len(page.Items) > in.Limit {
		return nil, invalid
	}
	previous := in.AfterID
	previousTime := in.AfterTime
	for _, item := range page.Items {
		if !validCIRunTraceItem(kind, item, page.CanLink) {
			return nil, invalid
		}
		if kind == goldenpath.TraceTicketRunHistory {
			if !previousTime.IsZero() && (item.OccurredAt.After(previousTime) || (item.OccurredAt.Equal(previousTime) && item.Ref.ID >= previous)) {
				return nil, invalid
			}
			previousTime = *item.OccurredAt
		} else if item.Ref.ID <= previous {
			return nil, invalid
		}
		previous = item.Ref.ID
	}
	boundary := page.NextID
	if boundary != "" {
		if len(page.Items) != in.Limit || boundary != previous {
			return nil, invalid
		}
		if kind == goldenpath.TraceTicketRunHistory {
			if !page.NextTime.Equal(previousTime) {
				return nil, invalid
			}
			boundary += "," + page.NextTime.UTC().Format(time.RFC3339Nano)
		}
	}
	next, err := nextDiscoveryCursor(scope, boundary)
	if err != nil {
		return nil, err
	}
	data := map[string]any{"items": page.Items, "next_cursor": next}
	if kind == goldenpath.TraceTicketRuns {
		data["capabilities"] = map[string]bool{"can_link": page.CanLink}
	}
	return data, nil
}

func parseCIRunHistoryBoundary(value string) (string, time.Time, error) {
	id, stamp, ok := strings.Cut(value, ",")
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if !ok || !cp.ValidID(id, "evt_") || err != nil || at.IsZero() || stamp != at.UTC().Format(time.RFC3339Nano) {
		return "", time.Time{}, authz.ErrInvalid
	}
	return id, at, nil
}

func traceRef(ref *entityref.Ref, kind, prefix string) bool {
	return ref != nil && ref.Type == kind && cp.ValidID(ref.ID, prefix)
}

func validCIRunTraceItem(kind string, item goldenpath.CIRunTraceItem, canLink bool) bool {
	terminal := goldenpath.ValidBuildTerminalStatus(item.Status) && item.CompletedAt != nil && !item.CompletedAt.IsZero()
	ref := &item.Ref
	switch kind {
	case goldenpath.TraceComponentRuns, goldenpath.TraceTicketRuns:
		return terminal && traceRef(ref, "ci-run", "cir_") && item.Component != nil &&
			traceRef(&item.Component.Ref, "component", "cmp_") && validDiscoveryTitle(item.Component.Name) &&
			(kind == goldenpath.TraceComponentRuns || (traceRef(item.Link, "entity-link", "lnk_") && item.CanUnlink != nil && *item.CanUnlink == canLink))
	case goldenpath.TraceRunTickets:
		return traceRef(ref, "ticket", "tkt_") && validDiscoveryTitle(item.Title) &&
			goldenpath.ValidTicketStatus(item.Status) && traceRef(item.Project, "project", "prj_")
	case goldenpath.TraceRunDeployments:
		return terminal && traceRef(ref, "deployment", "dpl_") && item.Environment != nil &&
			traceRef(&item.Environment.Ref, "environment", "env_") && validDiscoveryTitle(item.Environment.Name)
	case goldenpath.TraceTicketRunHistory:
		return traceRef(ref, "event", "evt_") && traceRef(item.Actor, "user", "usr_") &&
			traceRef(item.Run, "ci-run", "cir_") && traceRef(item.Link, "entity-link", "lnk_") &&
			item.OccurredAt != nil && !item.OccurredAt.IsZero() && (item.RelationState == "active" || item.RelationState == "removed")
	}
	return false
}
