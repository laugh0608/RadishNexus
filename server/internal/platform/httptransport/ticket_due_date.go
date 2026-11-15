package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/calendardate"
)

const ticketDueDatePattern = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/due-date"

type TicketDueDateApplication interface {
	ReadTicketDueDate(context.Context, authz.Principal, string) (goldenpath.TicketDueDate, error)
	SetTicketDueDate(context.Context, goldenpath.Invocation, goldenpath.TicketDueDateInput) (goldenpath.TicketDueDateResult, error)
}

func RegisterTicketDueDateRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle(ticketDueDatePattern, handler)
}
func NewTicketDueDateHandler(sessions MessagingSessionService, app TicketDueDateApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(ticketDueDatePattern, func(w http.ResponseWriter, r *http.Request) {
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
			writeIdentityError(w, r, errors.New("due date scope mismatch"))
			return
		}
		id := r.PathValue("ticket_id")
		if !validScopedID(id, "tkt_") || r.URL.RawQuery != "" {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		var data any
		if r.Method == "GET" {
			var result goldenpath.TicketDueDate
			result, err = app.ReadTicketDueDate(r.Context(), p, id)
			if err == nil && (!calendardate.Valid(result.DueDate) || !goldenpath.ValidTicketStatusRevision(result.DueDateRevision) || !goldenpath.ValidTicketStatusRevision(result.StatusRevision) || (result.CanSetDueDate && result.DueDateRevision == goldenpath.MaxTicketStatusRevision)) {
				err = errors.New("invalid due date projection")
			}
			data = result
		} else {
			in := goldenpath.TicketDueDateInput{TicketID: id}
			var raw map[string]json.RawMessage
			raw, err = decodeStrictObject(w, r, []string{"client_operation_id", "base_due_date_revision", "base_status_revision", "due_date", "confirmed"}, 8*1024)
			if err == nil {
				for name, target := range map[string]any{"client_operation_id": &in.ClientOperationID, "base_due_date_revision": &in.BaseDueDateRevision, "base_status_revision": &in.BaseStatusRevision, "due_date": &in.DueDate, "confirmed": &in.Confirmed} {
					if (name != "due_date" && string(raw[name]) == "null") || json.Unmarshal(raw[name], target) != nil {
						err = authz.ErrInvalid
						break
					}
				}
			}
			if err == nil {
				err = goldenpath.ValidateTicketDueDateInput(in)
			}
			if err == nil {
				var result goldenpath.TicketDueDateResult
				result, err = app.SetTicketDueDate(r.Context(), webInvocation(p, r), in)
				if err == nil && (!result.Applied || in.BaseDueDateRevision == goldenpath.MaxTicketStatusRevision || result.DueDateRevision != in.BaseDueDateRevision+1 || !calendardate.Equal(result.DueDate, in.DueDate)) {
					err = errors.New("invalid due date application result")
				}
				data = struct {
					Ticket entityRefDTO `json:"ticket"`
					goldenpath.TicketDueDateResult
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
