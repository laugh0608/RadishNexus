package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type TicketComponentApplication interface {
	WriteTicketComponent(context.Context, goldenpath.Invocation, string, goldenpath.TicketComponentInput) (goldenpath.TicketComponentResult, error)
	ListTicketComponents(context.Context, authz.Principal, entityref.Ref, goldenpath.DiscoveryPageInput) (goldenpath.TicketComponentPage, error)
}

var ticketComponentRoutes = map[string]string{
	"/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/components":                "GET, POST",
	"/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/component-links/{link_id}": "DELETE",
	"/api/v1/workspaces/{workspace_id}/components/{component_id}/tickets":             "GET",
}

func NewTicketComponentHandler(sessions MessagingSessionService, app TicketComponentApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	handle := func(w http.ResponseWriter, r *http.Request) {
		p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), r.Method != "GET", sessions, session, proxy)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if p.WorkspaceID != r.PathValue("workspace_id") {
			writeIdentityError(w, r, errors.New("Ticket Component identity scope mismatch"))
			return
		}
		source := entityref.Ref{Type: "ticket", ID: r.PathValue("ticket_id")}
		if r.PathValue("component_id") != "" {
			source = entityref.Ref{Type: "component", ID: r.PathValue("component_id")}
		}
		if entityref.M0Registry().Validate(source) != nil {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		if r.Method == "GET" {
			scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, Kind: "ticket-components", ProjectID: source.ID}
			if source.Type == "component" {
				scope.Kind = "component-tickets"
			}
			in, e := parseDiscoveryQuery(r.URL.RawQuery, scope)
			if e != nil {
				writeIdentityError(w, r, e)
				return
			}
			page, e := app.ListTicketComponents(r.Context(), p, source, in)
			if e != nil {
				writeIdentityError(w, r, e)
				return
			}
			data, e := publicTicketComponentPage(page, in, scope)
			if e != nil {
				writeIdentityError(w, r, e)
				return
			}
			writeIdentityJSON(w, r, 200, map[string]any{"data": data})
			return
		}
		if r.URL.RawQuery != "" {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		in := goldenpath.TicketComponentInput{TicketID: source.ID, LinkID: r.PathValue("link_id")}
		command := "ticket.component.link"
		fields := []string{"client_operation_id", "confirmed", "component_id"}
		if r.Method == "DELETE" {
			command = "ticket.component.unlink"
			fields = []string{"client_operation_id", "confirmed"}
			if !validScopedID(in.LinkID, "lnk_") {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
		}
		raw, e := decodeStrictObject(w, r, fields, 8*1024)
		if e != nil {
			writeIdentityError(w, r, e)
			return
		}
		for key, value := range raw {
			if string(value) == "null" {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
			var target any
			switch key {
			case "client_operation_id":
				target = &in.ClientOperationID
			case "confirmed":
				target = &in.Confirmed
			case "component_id":
				target = &in.ComponentID
			}
			if e = json.Unmarshal(value, target); e != nil {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
		}
		if !in.Confirmed {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		result, e := app.WriteTicketComponent(r.Context(), webInvocation(p, r), command, in)
		if e != nil {
			writeIdentityError(w, r, e)
			return
		}
		if !validScopedID(result.LinkID, "lnk_") || (command == "ticket.component.unlink" && (result.LinkID != in.LinkID || result.Created)) {
			writeIdentityError(w, r, errors.New("invalid Ticket Component result"))
			return
		}
		status := 200
		if result.Created {
			status = 201
		}
		writeIdentityJSON(w, r, status, map[string]any{"data": map[string]any{"link_id": result.LinkID, "applied": true}})
	}
	for path, methods := range ticketComponentRoutes {
		for _, method := range strings.Split(methods, ", ") {
			mux.HandleFunc(method+" "+path, handle)
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", methods)
			writeIdentityError(w, r, ErrMethodNotAllowed)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}
func RegisterTicketComponentRoutes(mux *http.ServeMux, handler http.Handler) {
	for path := range ticketComponentRoutes {
		mux.Handle(path, handler)
	}
}

func publicTicketComponentPage(page goldenpath.TicketComponentPage, in goldenpath.DiscoveryPageInput, scope discoveryCursor) (any, error) {
	invalid := errors.New("invalid Ticket Component page")
	if len(page.Links) > in.Limit || (scope.Kind == "component-tickets" && page.CanLink) {
		return nil, invalid
	}
	items := []any{}
	seen := map[string]bool{}
	last := in.AfterID
	for _, link := range page.Links {
		if !validScopedID(link.ID, "lnk_") || seen[link.ID] {
			return nil, invalid
		}
		seen[link.ID] = true
		var target any
		var id string
		if scope.Kind == "ticket-components" {
			if link.Component == nil || link.Ticket != nil || link.Component.Kind != "component" || link.CanUnlink != page.CanLink {
				return nil, invalid
			}
			var err error
			target, err = deliveryConfigurationDTO(*link.Component, false)
			if err != nil {
				return nil, err
			}
			id = link.Component.ID
		} else {
			if link.Ticket == nil || link.Component != nil {
				return nil, invalid
			}
			t, err := publicTicket(*link.Ticket)
			if err != nil {
				return nil, err
			}
			target = map[string]any{"ref": t.Ref, "title": t.Title, "status": t.Status, "project": t.Project}
			id = t.Ref.ID
		}
		if id <= last {
			return nil, invalid
		}
		last = id
		items = append(items, map[string]any{"link_id": link.ID, "target": target, "can_unlink": link.CanUnlink})
	}
	var next *string
	if page.NextID != "" {
		if len(items) != in.Limit || page.NextID != last {
			return nil, invalid
		}
		scope.AfterID = last
		b, err := json.Marshal(scope)
		if err != nil {
			return nil, err
		}
		encoded := base64.RawURLEncoding.EncodeToString(b)
		next = &encoded
	}
	data := map[string]any{"items": items, "next_cursor": next}
	if scope.Kind == "ticket-components" {
		data["capabilities"] = map[string]bool{"can_link": page.CanLink}
	}
	return data, nil
}
