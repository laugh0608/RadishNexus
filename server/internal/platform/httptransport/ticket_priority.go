package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const ticketPriorityPattern = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/priority"

type TicketPriorityApplication interface {
	ReadTicketPriority(context.Context, authz.Principal, string) (goldenpath.TicketPriority, error)
	SetTicketPriority(context.Context, goldenpath.Invocation, goldenpath.TicketPriorityInput) (goldenpath.TicketPriorityResult, error)
}

func RegisterTicketPriorityRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle(ticketPriorityPattern, handler)
}
func NewTicketPriorityHandler(sessions MessagingSessionService, app TicketPriorityApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(ticketPriorityPattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "POST" {
			w.Header().Set("Allow", "GET, POST")
			writeIdentityError(w, r, ErrMethodNotAllowed)
			return
		}
		p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), r.Method == "POST", sessions, session, proxy)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if p.WorkspaceID != r.PathValue("workspace_id") {
			writeIdentityError(w, r, errors.New("priority scope mismatch"))
			return
		}
		id := r.PathValue("ticket_id")
		if !validScopedID(id, "tkt_") || r.URL.RawQuery != "" {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		var data any
		if r.Method == "GET" {
			var result goldenpath.TicketPriority
			result, err = app.ReadTicketPriority(r.Context(), p, id)
			if err == nil && (!goldenpath.ValidTicketPriority(result.Priority) || !goldenpath.ValidTicketStatusRevision(result.PriorityRevision) || !goldenpath.ValidTicketStatusRevision(result.StatusRevision) || (result.CanSetPriority && result.PriorityRevision == goldenpath.MaxTicketStatusRevision)) {
				err = errors.New("invalid priority projection")
			}
			data = result
		} else {
			in := goldenpath.TicketPriorityInput{TicketID: id}
			var raw map[string]json.RawMessage
			raw, err = decodeStrictObject(w, r, []string{"client_operation_id", "base_priority_revision", "base_status_revision", "priority", "confirmed"}, 8*1024)
			if err == nil {
				for name, target := range map[string]any{"client_operation_id": &in.ClientOperationID, "base_priority_revision": &in.BasePriorityRevision, "base_status_revision": &in.BaseStatusRevision, "priority": &in.Priority, "confirmed": &in.Confirmed} {
					if string(raw[name]) == "null" || json.Unmarshal(raw[name], target) != nil {
						err = authz.ErrInvalid
						break
					}
				}
			}
			if err == nil {
				err = goldenpath.ValidateTicketPriorityInput(in)
			}
			if err == nil {
				var result goldenpath.TicketPriorityResult
				result, err = app.SetTicketPriority(r.Context(), webInvocation(p, r), in)
				if err == nil && (!result.Applied || in.BasePriorityRevision == goldenpath.MaxTicketStatusRevision || result.PriorityRevision != in.BasePriorityRevision+1 || result.Priority != in.Priority) {
					err = errors.New("invalid priority application result")
				}
				data = struct {
					Ticket entityRefDTO `json:"ticket"`
					goldenpath.TicketPriorityResult
				}{entityRefDTO{Type: "ticket", ID: id}, result}
			}
		}
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		writeIdentityJSON(w, r, http.StatusOK, map[string]any{"data": data})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}
