package httptransport

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const ticketTimelineKind = "ticket-activity"

func ticketTimelineSelection(include string) goldenpath.TicketTimelineSelection {
	return goldenpath.TicketTimelineSelection{
		DueDate:        strings.Contains(include, "due_date"),
		Execution:      strings.Contains(include, "execution_context"),
		Classification: strings.Contains(include, "classification"),
	}
}

func (h *CollaborationHandler) getTicketTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, r)
		return
	}
	workspace, ticket := r.PathValue("workspace_id"), r.PathValue("ticket_id")
	p, err := h.authenticate(r, workspace, false)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if _, err = validateCollaborationPath(workspace, "ticket", ticket); err != nil {
		h.writeError(w, r, err)
		return
	}
	scope := discoveryCursor{Version: 1, WorkspaceID: workspace, ActorID: p.ID, TicketID: ticket, Kind: ticketTimelineKind}
	input, scope, err := parseTicketTimelineQuery(r.URL.RawQuery, scope)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	page, err := h.collaboration.ListTicketTimeline(r.Context(), p, ticket, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	dto, err := publicTicketTimelinePage(page, input, scope)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, map[string]any{"data": dto})
}

func parseTicketTimelineQuery(raw string, scope discoveryCursor) (input goldenpath.TicketTimelineInput, _ discoveryCursor, err error) {
	if len(raw) > 2048 {
		return input, scope, authz.ErrInvalid
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return input, scope, authz.ErrInvalid
	}
	if values, ok := q["include"]; ok {
		if len(values) != 1 || !validTicketViewInclude(values[0]) {
			return input, scope, authz.ErrInvalid
		}
		scope.Include = values[0]
		q.Del("include")
	}
	page, err := parseDiscoveryQuery(q.Encode(), scope)
	input = goldenpath.TicketTimelineInput{TicketTimelineSelection: ticketTimelineSelection(scope.Include), Limit: page.Limit, AfterEventID: page.AfterID}
	return input, scope, err
}

func publicTicketTimelinePage(page goldenpath.TicketTimelinePage, input goldenpath.TicketTimelineInput, scope discoveryCursor) (any, error) {
	invalid := errors.New("invalid Ticket timeline page")
	if page.Items == nil || len(page.Items) > input.Limit {
		return nil, invalid
	}
	types := input.EventTypes()
	seen := make(map[string]bool, len(page.Items))
	for i, item := range page.Items {
		if !slices.Contains(types, item.ActivityType) || seen[item.EventID] || item.EventID == input.AfterEventID {
			return nil, invalid
		}
		seen[item.EventID] = true
		if i > 0 && item.OccurredAt.After(page.Items[i-1].OccurredAt) {
			return nil, invalid
		}
		if (item.ActivityType == goldenpath.DiscussionLinked || item.ActivityType == goldenpath.DiscussionUnlinked) &&
			(len(item.Subjects) != 1 || item.Subjects[0].State != goldenpath.ProjectionVisible) {
			return nil, invalid
		}
	}
	if page.NextEventID != "" && (len(page.Items) != input.Limit || page.NextEventID != page.Items[len(page.Items)-1].EventID) {
		return nil, invalid
	}
	items, err := publicCollaborationTimeline(page.Items)
	if err != nil {
		return nil, err
	}
	next, err := nextDiscoveryCursor(scope, page.NextEventID)
	return map[string]any{"items": items, "next_cursor": next}, err
}
