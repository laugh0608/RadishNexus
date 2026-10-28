package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const ticketAssigneePattern = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/assignee"

type TicketAssignmentApplication interface {
	ReadTicketAssignment(context.Context, authz.Principal, string) (goldenpath.TicketAssignment, error)
	ListAssigneeCandidates(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.AssigneeCandidates, error)
	SetTicketAssignee(context.Context, goldenpath.Invocation, goldenpath.TicketAssignmentInput) (goldenpath.TicketAssignmentResult, error)
}

func RegisterTicketAssignmentRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle(ticketAssigneePattern, handler)
	mux.Handle(ticketAssigneePattern+"/candidates", handler)
}
func NewTicketAssignmentHandler(sessions MessagingSessionService, app TicketAssignmentApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	for _, path := range []string{ticketAssigneePattern, ticketAssigneePattern + "/candidates"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			candidates := path == ticketAssigneePattern+"/candidates"
			if r.Method != "GET" && (r.Method != "POST" || candidates) {
				allow := "GET, POST"
				if candidates {
					allow = "GET"
				}
				w.Header().Set("Allow", allow)
				writeIdentityError(w, r, ErrMethodNotAllowed)
				return
			}
			p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), r.Method == "POST", sessions, session, proxy)
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			if p.WorkspaceID != r.PathValue("workspace_id") {
				writeIdentityError(w, r, errors.New("assignment scope mismatch"))
				return
			}
			id := r.PathValue("ticket_id")
			if !validScopedID(id, "tkt_") || (!candidates && r.URL.RawQuery != "") {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
			var data any
			if candidates {
				scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, ActorID: p.ID, ProjectID: id, Kind: "ticket-assignee-candidates"}
				var q goldenpath.DiscoveryPageInput
				q, err = parseDiscoveryQuery(r.URL.RawQuery, scope)
				if err == nil {
					var page goldenpath.AssigneeCandidates
					page, err = app.ListAssigneeCandidates(r.Context(), p, id, q)
					if err == nil {
						data, err = publicAssigneeCandidates(page, q, scope)
					}
				}
			} else if r.Method == "GET" {
				var result goldenpath.TicketAssignment
				result, err = app.ReadTicketAssignment(r.Context(), p, id)
				if err == nil && (!goldenpath.ValidTicketStatusRevision(result.AssignmentRevision) || !goldenpath.ValidTicketStatusRevision(result.StatusRevision) || !validAssignmentVersion(result.ProjectManagementRevision) || !validAssignee(result.Assignee) || (result.CanAssign && result.AssignmentRevision == goldenpath.MaxTicketStatusRevision)) {
					err = errors.New("invalid assignment projection")
				}
				data = result
			} else {
				in := goldenpath.TicketAssignmentInput{TicketID: id}
				var raw map[string]json.RawMessage
				raw, err = decodeStrictObject(w, r, []string{"client_operation_id", "base_assignment_revision", "base_status_revision", "assignee_user_id", "target_lifecycle_revision", "project_management_revision", "confirmed"}, 8*1024)
				if err == nil {
					for name, target := range map[string]any{"client_operation_id": &in.ClientOperationID, "base_assignment_revision": &in.BaseAssignmentRevision, "base_status_revision": &in.BaseStatusRevision, "assignee_user_id": &in.AssigneeUserID, "target_lifecycle_revision": &in.TargetLifecycleRevision, "project_management_revision": &in.ProjectManagementRevision, "confirmed": &in.Confirmed} {
						if (name != "assignee_user_id" && string(raw[name]) == "null") || json.Unmarshal(raw[name], target) != nil {
							err = authz.ErrInvalid
							break
						}
					}
				}
				if err == nil {
					err = goldenpath.ValidateTicketAssignmentInput(in)
				}
				if err == nil {
					var result goldenpath.TicketAssignmentResult
					result, err = app.SetTicketAssignee(r.Context(), webInvocation(p, r), in)
					if err == nil && (!result.Applied || in.BaseAssignmentRevision == goldenpath.MaxTicketStatusRevision || result.AssignmentRevision != in.BaseAssignmentRevision+1 || !goldenpath.SameAssignee(result.AssigneeUserID, in.AssigneeUserID)) {
						err = errors.New("invalid assignment application result")
					}
					data = result
				}
			}
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			writeIdentityJSON(w, r, http.StatusOK, map[string]any{"data": data})
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}
func validAssignmentVersion(v string) bool {
	n, e := strconv.ParseInt(v, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == v
}
func validAssignee(v *goldenpath.TicketAssignee) bool {
	return v == nil || validScopedID(v.ID, "usr_") && utf8.ValidString(v.DisplayName) && strings.TrimSpace(v.DisplayName) != "" && !strings.ContainsRune(v.DisplayName, 0) && !(v.Imported && v.Eligible)
}
func publicAssigneeCandidates(page goldenpath.AssigneeCandidates, q goldenpath.DiscoveryPageInput, scope discoveryCursor) (any, error) {
	invalid := errors.New("invalid assignment candidates")
	if page.Items == nil || len(page.Items) > q.Limit || page.SelfID != scope.ActorID || !validAssignmentVersion(page.ProjectManagementRevision) {
		return nil, invalid
	}
	previous := q.AfterID
	for _, item := range page.Items {
		if item.ID <= previous || !validAssignee(&goldenpath.TicketAssignee{ID: item.ID, DisplayName: item.DisplayName}) || !validAssignmentVersion(item.LifecycleRevision) {
			return nil, invalid
		}
		previous = item.ID
	}
	var cursor *string
	if page.NextID != "" {
		if len(page.Items) != q.Limit || page.NextID != previous {
			return nil, invalid
		}
		scope.AfterID = page.NextID
		raw, e := json.Marshal(scope)
		if e != nil {
			return nil, e
		}
		v := base64.RawURLEncoding.EncodeToString(raw)
		cursor = &v
	}
	return struct {
		Items                     []goldenpath.AssigneeCandidate `json:"items"`
		NextCursor                *string                        `json:"next_cursor"`
		ProjectManagementRevision string                         `json:"project_management_revision"`
		SelfID                    string                         `json:"self_id"`
	}{page.Items, cursor, page.ProjectManagementRevision, page.SelfID}, nil
}
