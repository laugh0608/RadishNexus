package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type OwnerTransferApplication interface {
	Ownership(context.Context, authz.Principal, string) (goldenpath.Ownership, error)
	Candidates(context.Context, authz.Principal, string, string, int) (goldenpath.OwnerCandidatePage, error)
	Transfer(context.Context, goldenpath.Invocation, string, goldenpath.OwnerTransferInput) (goldenpath.OwnerTransferResult, error)
	Status(context.Context, authz.Principal, string, string) (goldenpath.OwnerTransferResult, error)
}

func RegisterOwnerTransferRoutes(mux *http.ServeMux, handler http.Handler) {
	for _, suffix := range []string{"/ownership", "/owner-transfer-candidates", "/owner-transfers", "/owner-transfers/{client_operation_id}"} {
		mux.Handle(configurationBase+suffix, handler)
	}
}
func NewOwnerTransferHandler(sessions MessagingSessionService, app OwnerTransferApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	for _, route := range []struct{ path, method string }{{"/ownership", "GET"}, {"/owner-transfer-candidates", "GET"}, {"/owner-transfers", "POST"}, {"/owner-transfers/{client_operation_id}", "GET"}} {
		mux.HandleFunc(configurationBase+route.path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != route.method {
				w.Header().Set("Allow", route.method)
				writeIdentityError(w, r, ErrMethodNotAllowed)
				return
			}
			p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), r.Method == "POST", sessions, session, proxy)
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			if p.WorkspaceID != r.PathValue("workspace_id") {
				writeIdentityError(w, r, errors.New("owner transfer scope mismatch"))
				return
			}
			token, err := session.SessionToken(r)
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			if route.path != "/owner-transfer-candidates" && r.URL.RawQuery != "" {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
			var data any
			switch route.path {
			case "/ownership":
				var result goldenpath.Ownership
				result, err = app.Ownership(r.Context(), p, token)
				if err == nil {
					if result.UserID != p.ID || result.RoleRevision < 1 || (result.Role != "owner" && result.Role != "member") {
						err = errors.New("invalid ownership projection")
					} else {
						data = map[string]any{"self": map[string]any{"id": result.UserID, "role": result.Role, "role_revision": strconv.FormatInt(result.RoleRevision, 10)}, "can_transfer": result.Role == "owner", "recent_authentication": result.RecentlyAuthenticated}
					}
				}
			case "/owner-transfer-candidates":
				scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, ActorID: p.ID, Kind: "owner-transfer-candidates"}
				var input goldenpath.DiscoveryPageInput
				input, err = parseDiscoveryQuery(r.URL.RawQuery, scope)
				if err == nil {
					var page goldenpath.OwnerCandidatePage
					page, err = app.Candidates(r.Context(), p, token, input.AfterID, input.Limit)
					if err == nil {
						data, err = ownerCandidatesDTO(page, input, scope)
					}
				}
			default:
				var result goldenpath.OwnerTransferResult
				operation := r.PathValue("client_operation_id")
				var in goldenpath.OwnerTransferInput
				if r.Method == "POST" {
					var raw map[string]json.RawMessage
					raw, err = decodeStrictObject(w, r, []string{"client_operation_id", "target_user_id", "expected_actor_role_revision", "expected_target_role_revision", "confirmed"}, 8*1024)
					if err == nil {
						for key, target := range map[string]any{"client_operation_id": &in.ClientOperationID, "target_user_id": &in.TargetUserID, "expected_actor_role_revision": &in.ExpectedActorRoleRevision, "expected_target_role_revision": &in.ExpectedTargetRoleRevision, "confirmed": &in.Confirmed} {
							if string(raw[key]) == "null" || json.Unmarshal(raw[key], target) != nil {
								err = authz.ErrInvalid
								break
							}
						}
					}
					if err == nil {
						_, aerr := goldenpath.ParseRoleRevision(in.ExpectedActorRoleRevision)
						_, berr := goldenpath.ParseRoleRevision(in.ExpectedTargetRoleRevision)
						if !in.Confirmed || !goldenpath.ValidOwnerOperationID(in.ClientOperationID) || !validScopedID(in.TargetUserID, "usr_") || in.TargetUserID == p.ID || aerr != nil || berr != nil {
							err = authz.ErrInvalid
						}
					}
					operation = in.ClientOperationID
					if err == nil {
						result, err = app.Transfer(r.Context(), webInvocation(p, r), token, in)
					}
				} else {
					if !goldenpath.ValidOwnerOperationID(operation) {
						err = authz.ErrInvalid
					} else {
						result, err = app.Status(r.Context(), p, token, operation)
					}
				}
				if err == nil {
					data, err = ownerTransferDTO(result, p, operation, r.Method == "POST")
				}
				if err == nil && r.Method == "POST" {
					ar, _ := goldenpath.ParseRoleRevision(in.ExpectedActorRoleRevision)
					tr, _ := goldenpath.ParseRoleRevision(in.ExpectedTargetRoleRevision)
					if result.TargetUserID != in.TargetUserID || result.ActorRoleRevision <= ar || result.ActorRoleRevision-ar != 1 || result.TargetRoleRevision <= tr || result.TargetRoleRevision-tr != 1 {
						err = errors.New("owner transfer result version mismatch")
					}
				}
			}
			if err != nil {
				writeIdentityError(w, r, err)
				return
			}
			writeIdentityJSON(w, r, 200, map[string]any{"data": data})
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}
func ownerCandidatesDTO(page goldenpath.OwnerCandidatePage, input goldenpath.DiscoveryPageInput, scope discoveryCursor) (any, error) {
	invalid := errors.New("invalid owner candidates projection")
	if len(page.Items) > input.Limit {
		return nil, invalid
	}
	rows := []any{}
	last := input.AfterID
	for _, item := range page.Items {
		if !validScopedID(item.ID, "usr_") || item.ID == scope.ActorID || item.ID <= last || item.RoleRevision <= 0 || !utf8.ValidString(item.DisplayName) || strings.TrimSpace(item.DisplayName) == "" || strings.ContainsRune(item.DisplayName, 0) {
			return nil, invalid
		}
		rows = append(rows, map[string]any{"id": item.ID, "display_name": item.DisplayName, "role_revision": strconv.FormatInt(item.RoleRevision, 10)})
		last = item.ID
	}
	var next *string
	if page.NextID != "" {
		if len(rows) != input.Limit || page.NextID != last {
			return nil, invalid
		}
		scope.AfterID = last
		body, err := json.Marshal(scope)
		if err != nil {
			return nil, err
		}
		encoded := base64.RawURLEncoding.EncodeToString(body)
		next = &encoded
	}
	return map[string]any{"items": rows, "next_cursor": next}, nil
}
func ownerTransferDTO(result goldenpath.OwnerTransferResult, p authz.Principal, operation string, write bool) (any, error) {
	invalid := errors.New("invalid owner transfer result")
	if result.WorkspaceID != p.WorkspaceID || result.ClientOperationID != operation {
		return nil, invalid
	}
	data := map[string]any{"result": result.Result, "workspace_id": result.WorkspaceID, "client_operation_id": result.ClientOperationID}
	if result.Result == "not_observed" && !write {
		if result.ActorUserID != "" || result.TargetUserID != "" || result.AuditID != "" || !result.OccurredAt.IsZero() || result.ActorRoleRevision != 0 || result.TargetRoleRevision != 0 {
			return nil, invalid
		}
		return data, nil
	}
	if result.Result != "applied" || result.ActorUserID != p.ID || !validScopedID(result.TargetUserID, "usr_") || result.TargetUserID == p.ID || !validScopedID(result.AuditID, "cfa_") || result.OccurredAt.IsZero() || result.ActorRoleRevision < 2 || result.TargetRoleRevision < 2 {
		return nil, invalid
	}
	data["actor_user_id"] = result.ActorUserID
	data["target_user_id"] = result.TargetUserID
	data["audit_id"] = result.AuditID
	data["occurred_at"] = result.OccurredAt.UTC().Format(time.RFC3339Nano)
	data["actor_role_revision"] = strconv.FormatInt(result.ActorRoleRevision, 10)
	data["target_role_revision"] = strconv.FormatInt(result.TargetRoleRevision, 10)
	return data, nil
}
