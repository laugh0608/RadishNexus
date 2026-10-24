package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type ManagementApplication interface {
	Responsibilities(context.Context, authz.Principal, string, string, int) (goldenpath.ManagementResponsibilityPage, error)
	Preview(context.Context, authz.Principal, string, goldenpath.ManagementProposal) (goldenpath.ManagementPlan, error)
	Apply(context.Context, goldenpath.Invocation, string, goldenpath.ManagementInput) (goldenpath.ManagementResult, error)
	Result(context.Context, authz.Principal, string, string) (goldenpath.ManagementResult, error)
	Members(context.Context, authz.Principal, string, goldenpath.ManagementQuery) (goldenpath.ManagementPage, error)
	Audit(context.Context, authz.Principal, string, goldenpath.ManagementQuery) (goldenpath.ManagementAuditPage, error)
}
type ManagementSessions interface {
	MessagingSessionService
	ResolveSession(context.Context, string) (authn.SessionAccount, error)
}
type managementRoute struct {
	kind, base, idField string
	actions             []string
}

var managementRoutes = []managementRoute{
	{"workspace", configurationBase + "/management", "workspace_id", []string{"suspend", "resume", "remove", "leave", "reinvite"}},
	{"project", configurationBase + "/projects/{project_id}/management", "project_id", []string{"add", "role", "remove", "handoff", "leave"}},
	{"channel", configurationBase + "/channels/{channel_id}/management", "channel_id", []string{"add", "role", "remove", "handoff", "leave"}},
	{"thread", configurationBase + "/threads/{thread_id}/management", "thread_id", []string{"add", "role", "remove", "handoff", "leave"}},
}

const managementResultPath = "/api/v1/auth/management-results/{workspace_id}/{client_operation_id}"

func RegisterManagementRoutes(mux *http.ServeMux, handler http.Handler) {
	for _, route := range managementRoutes {
		for _, view := range []string{"members", "candidates", "audit", "responsibilities"} {
			if view == "responsibilities" && route.kind != "workspace" {
				continue
			}
			mux.Handle(route.base+"/"+view, handler)
		}
		for _, action := range route.actions {
			mux.Handle(route.base+"/"+action, handler)
			mux.Handle(route.base+"/"+action+"/preview", handler)
		}
	}
	mux.Handle(managementResultPath, handler)
}
func NewManagementHandler(sessions ManagementSessions, app ManagementApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	for _, route := range managementRoutes {
		for _, view := range []string{"members", "candidates", "audit", "responsibilities"} {
			if view == "responsibilities" && route.kind != "workspace" {
				continue
			}
			mux.HandleFunc(route.base+"/"+view, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					w.Header().Set("Allow", "GET")
					writeIdentityError(w, r, ErrMethodNotAllowed)
					return
				}
				p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), false, sessions, session, proxy)
				if err != nil {
					writeIdentityError(w, r, err)
					return
				}
				token, err := session.SessionToken(r)
				if err != nil {
					writeIdentityError(w, r, err)
					return
				}
				scope := discoveryCursor{Version: 1, ActorID: p.ID, WorkspaceID: p.WorkspaceID, Kind: "management-" + view + "-" + route.kind, ProjectID: r.PathValue(route.idField)}
				input, err := parseDiscoveryQuery(r.URL.RawQuery, scope)
				if err != nil {
					writeIdentityError(w, r, err)
					return
				}
				q := goldenpath.ManagementQuery{ScopeKind: route.kind, ScopeID: scope.ProjectID, AfterID: input.AfterID, Limit: input.Limit, Candidates: view == "candidates"}
				var data any
				var next string
				if view == "responsibilities" {
					if route.kind != "workspace" {
						writeIdentityError(w, r, authz.ErrNotFound)
						return
					}
					var page goldenpath.ManagementResponsibilityPage
					page, err = app.Responsibilities(r.Context(), p, token, input.AfterID, input.Limit)
					next = page.NextID
					data = map[string]any{"items": page.Items, "next_cursor": managementCursor(scope, next)}
				} else if view == "audit" {
					var page goldenpath.ManagementAuditPage
					page, err = app.Audit(r.Context(), p, token, q)
					next = page.NextID
					for i := range page.Items {
						page.Items[i].OccurredAt = page.Items[i].OccurredAt.UTC()
					}
					data = map[string]any{"items": page.Items, "next_cursor": managementCursor(scope, next)}
				} else {
					var page goldenpath.ManagementPage
					page, err = app.Members(r.Context(), p, token, q)
					next = page.NextID
					data = map[string]any{"items": page.Items, "next_cursor": managementCursor(scope, next), "can_manage": page.CanManage, "self_role": page.SelfRole, "scope_revision": page.ScopeRevision}
				}
				if err != nil {
					writeIdentityError(w, r, err)
					return
				}
				writeIdentityJSON(w, r, 200, map[string]any{"data": data})
			})
		}
		for _, action := range route.actions {
			for _, preview := range []bool{false, true} {
				path := route.base + "/" + action
				if preview {
					path += "/preview"
				}
				mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "POST" {
						w.Header().Set("Allow", "POST")
						writeIdentityError(w, r, ErrMethodNotAllowed)
						return
					}
					// Preview is a POST with CSRF/Origin checks, although it only reads.
					p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), true, sessions, session, proxy)
					if err != nil {
						writeIdentityError(w, r, err)
						return
					}
					token, err := session.SessionToken(r)
					if err != nil {
						writeIdentityError(w, r, err)
						return
					}
					if r.URL.RawQuery != "" {
						writeIdentityError(w, r, authz.ErrInvalid)
						return
					}
					in := goldenpath.ManagementInput{ManagementProposal: goldenpath.ManagementProposal{ScopeKind: route.kind, ScopeID: r.PathValue(route.idField), Action: action}}
					fields := []string{"target_user_id", "role", "actor_after_role"}
					targets := map[string]any{"target_user_id": &in.TargetUserID, "role": &in.Role, "actor_after_role": &in.ActorAfterRole}
					if !preview {
						fields = append(fields, "client_operation_id", "plan_digest", "confirmed")
						targets["client_operation_id"] = &in.ClientOperationID
						targets["plan_digest"] = &in.PlanDigest
						targets["confirmed"] = &in.Confirmed
					}
					raw, err := decodeStrictObject(w, r, fields, 8*1024)
					if err == nil {
						for key, target := range targets {
							if string(raw[key]) == "null" || json.Unmarshal(raw[key], target) != nil {
								err = authz.ErrInvalid
								break
							}
						}
					}
					if err != nil {
						writeIdentityError(w, r, err)
						return
					}
					if preview {
						err = goldenpath.ValidateManagementProposal(p, in.ManagementProposal)
					} else {
						err = goldenpath.ValidateManagementInput(p, in)
					}
					if err != nil {
						writeIdentityError(w, r, err)
						return
					}
					var data any
					if preview {
						var plan goldenpath.ManagementPlan
						plan, err = app.Preview(r.Context(), p, token, in.ManagementProposal)
						if err == nil {
							expected, e := plan.SHA256(p.WorkspaceID)
							if e != nil || plan.ManagementProposal != in.ManagementProposal || plan.ActorID != p.ID || plan.Digest != expected || plan.Policy != "membership-management-v1" {
								err = errors.New("invalid management plan projection")
							}
						}
						data = plan
					} else {
						var result goldenpath.ManagementResult
						result, err = app.Apply(r.Context(), webInvocation(p, r), token, in)
						if err == nil {
							err = validateManagementResult(result, p.WorkspaceID, in.ClientOperationID, true)
						}
						if err == nil && (result.Action != in.Kind() || result.ScopeID != in.ScopeID || result.TargetUserID != in.TargetUserID) {
							err = errors.New("management result coordinates changed")
						}
						data = managementResultUTC(result)
					}
					if err != nil {
						writeIdentityError(w, r, err)
						return
					}
					writeIdentityJSON(w, r, 200, map[string]any{"data": data})
				})
			}
		}
	}
	mux.HandleFunc(managementResultPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			writeIdentityError(w, r, ErrMethodNotAllowed)
			return
		}
		if _, err := proxy.ClientIP(r); err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if err := session.ValidateHost(r); err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if r.URL.RawQuery != "" || !validScopedID(r.PathValue("workspace_id"), "wrk_") {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		token, err := session.SessionToken(r)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		account, err := sessions.ResolveSession(r.Context(), token)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		p := authz.Principal{Kind: authz.PrincipalUser, ID: account.User.ID, WorkspaceID: r.PathValue("workspace_id")}
		out, err := app.Result(r.Context(), p, token, r.PathValue("client_operation_id"))
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if validateManagementResult(out, p.WorkspaceID, r.PathValue("client_operation_id"), false) != nil {
			writeIdentityError(w, r, errors.New("invalid management receipt projection"))
			return
		}
		writeIdentityJSON(w, r, 200, map[string]any{"data": managementResultUTC(out)})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}
func managementCursor(scope discoveryCursor, next string) any {
	if next == "" {
		return nil
	}
	scope.AfterID = next
	body, _ := json.Marshal(scope)
	return base64.RawURLEncoding.EncodeToString(body)
}
func managementCursorPrefix(kind string) string {
	if strings.HasPrefix(kind, "management-audit-") {
		return "cfa_"
	}
	return "usr_"
}

func validateManagementResult(out goldenpath.ManagementResult, workspace, operation string, write bool) error {
	invalid := errors.New("invalid management result projection")
	if out.WorkspaceID != workspace || out.ClientOperationID != operation {
		return invalid
	}
	if out.Result == "not_observed" && !write {
		if out.Action != "" || out.ScopeID != "" || out.TargetUserID != "" || out.OccurredAt != nil || out.Invitation != nil {
			return invalid
		}
		return nil
	}
	parts := strings.Split(out.Action, ".")
	if out.Result != "applied" || len(parts) != 3 || parts[0] != "management" || !goldenpath.ValidManagementScope(parts[1], out.ScopeID, workspace) || !validScopedID(out.TargetUserID, "usr_") || out.OccurredAt == nil || out.OccurredAt.IsZero() {
		return invalid
	}
	known := false
	for _, route := range managementRoutes {
		if route.kind == parts[1] {
			for _, action := range route.actions {
				if action == parts[2] {
					known = true
				}
			}
		}
	}
	if !known {
		return invalid
	}
	if out.Invitation != nil {
		if !write || out.Action != "management.workspace.reinvite" || out.Invitation.ExpiresAt.IsZero() {
			return invalid
		}
		raw, e := base64.RawURLEncoding.DecodeString(out.Invitation.Token)
		if e != nil || len(raw) != 32 {
			return invalid
		}
	}
	return nil
}

func managementResultUTC(out goldenpath.ManagementResult) goldenpath.ManagementResult {
	if out.OccurredAt != nil {
		value := out.OccurredAt.UTC()
		out.OccurredAt = &value
	}
	if out.Invitation != nil {
		value := *out.Invitation
		value.ExpiresAt = value.ExpiresAt.UTC()
		out.Invitation = &value
	}
	return out
}
