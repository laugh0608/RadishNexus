package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const ticketStatusPattern = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/status-transitions"

type TicketStatusApplication interface {
	ChangeTicketStatus(context.Context, goldenpath.Invocation, goldenpath.TicketStatusInput) (goldenpath.TicketStatusResult, error)
}

func NewTicketStatusHandler(sessions MessagingSessionService, app TicketStatusApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+ticketStatusPattern, func(w http.ResponseWriter, r *http.Request) {
		p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), true, sessions, session, proxy)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if p.WorkspaceID != r.PathValue("workspace_id") {
			writeIdentityError(w, r, errors.New("Ticket status identity scope mismatch"))
			return
		}
		in := goldenpath.TicketStatusInput{TicketID: r.PathValue("ticket_id")}
		if !validScopedID(in.TicketID, "tkt_") || r.URL.RawQuery != "" {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		raw, err := decodeStrictObject(w, r, []string{"client_operation_id", "base_status_revision", "to_status", "confirmed"}, 8*1024)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		for name, target := range map[string]any{"client_operation_id": &in.ClientOperationID, "base_status_revision": &in.BaseStatusRevision, "to_status": &in.ToStatus, "confirmed": &in.Confirmed} {
			if string(raw[name]) == "null" || json.Unmarshal(raw[name], target) != nil {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
		}
		if !in.Confirmed || !goldenpath.ValidTicketStatus(in.ToStatus) || !goldenpath.ValidTicketStatusRevision(in.BaseStatusRevision) {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		result, err := app.ChangeTicketStatus(r.Context(), webInvocation(p, r), in)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if result.TicketID != in.TicketID || result.AppliedStatus != in.ToStatus || !goldenpath.ValidTicketStatusRevision(result.AppliedStatusRevision) || in.BaseStatusRevision == goldenpath.MaxTicketStatusRevision || result.AppliedStatusRevision != in.BaseStatusRevision+1 {
			writeIdentityError(w, r, errors.New("invalid Ticket status application result"))
			return
		}
		writeIdentityJSON(w, r, http.StatusOK, map[string]any{"data": map[string]any{"ticket": entityRefDTO{Type: "ticket", ID: result.TicketID}, "applied": true,
			"applied_status": result.AppliedStatus, "applied_status_revision": result.AppliedStatusRevision}})
	})
	mux.HandleFunc(ticketStatusPattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", http.MethodPost)
		writeIdentityError(w, r, ErrMethodNotAllowed)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}

func RegisterTicketStatusRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle(ticketStatusPattern, handler)
}
