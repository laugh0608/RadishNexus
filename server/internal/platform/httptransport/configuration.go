package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type ConfigurationApplication interface {
	Configure(context.Context, goldenpath.Invocation, goldenpath.ConfigurationInput) (goldenpath.ConfigurationResult, error)
	ReadConfiguration(context.Context, authz.Principal, string, string) (goldenpath.ConfigurationObject, error)
	ListConfiguration(context.Context, authz.Principal, goldenpath.ConfigurationQuery) (goldenpath.ConfigurationPage, error)
}
type ConfigurationHandler struct {
	sessions    MessagingSessionService
	application ConfigurationApplication
	session     BrowserSessionPolicy
	proxy       TrustedProxyPolicy
}

const configurationBase = "/api/v1/workspaces/{workspace_id}"

// RegisterConfigurationRoutes is shared by the production server and the
// isolated browser fixture so configuration never falls through to GET-only discovery.
func RegisterConfigurationRoutes(mux *http.ServeMux, discovery, configuration http.Handler) {
	combined := privateNoStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			discovery.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodPost {
			configuration.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Allow", "GET, POST")
		writeIdentityError(w, r, ErrMethodNotAllowed)
	}))
	mux.Handle(configurationBase+"/projects", combined)
	mux.Handle(configurationBase+"/projects/{project_id}/channels", combined)
	for _, suffix := range []string{"/teams", "/members", "/projects/{project_id}/configuration", "/projects/{project_id}/members", "/projects/{project_id}/members/{user_id}", "/channels/{channel_id}/configuration", "/channels/{channel_id}/members", "/channels/{channel_id}/members/{user_id}"} {
		mux.Handle(configurationBase+suffix, configuration)
	}
}

func NewConfigurationHandler(sessions MessagingSessionService, application ConfigurationApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	h := &ConfigurationHandler{sessions, application, session, proxy}
	mux := http.NewServeMux()
	for _, route := range []struct{ path, methods, kind string }{
		{"/teams", "GET, POST", "teams"}, {"/members", "GET", "members"},
		{"/projects", "POST", "project.create"},
		{"/projects/{project_id}/configuration", "GET", "project"},
		{"/projects/{project_id}/members", "GET", "project-members"},
		{"/projects/{project_id}/members/{user_id}", "PUT, DELETE", "project.member"},
		{"/projects/{project_id}/channels", "POST", "channel.create"},
		{"/channels/{channel_id}/configuration", "GET", "channel"},
		{"/channels/{channel_id}/members", "GET", "channel-members"},
		{"/channels/{channel_id}/members/{user_id}", "PUT, DELETE", "channel.member"},
	} {
		mux.HandleFunc(configurationBase+route.path, func(w http.ResponseWriter, r *http.Request) {
			allowed := false
			for _, method := range strings.Split(route.methods, ", ") {
				if r.Method == method {
					allowed = true
				}
			}
			if !allowed {
				w.Header().Set("Allow", route.methods)
				writeIdentityError(w, r, ErrMethodNotAllowed)
				return
			}
			h.serve(w, r, route.kind)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}
func (h *ConfigurationHandler) serve(w http.ResponseWriter, r *http.Request, kind string) {
	p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), r.Method != "GET", h.sessions, h.session, h.proxy)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	scope := p.WorkspaceID
	if id := r.PathValue("project_id"); id != "" {
		scope = id
		if !validScopedID(id, "prj_") {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
	}
	if id := r.PathValue("channel_id"); id != "" {
		scope = id
		if !validScopedID(id, "chn_") {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
	}
	if r.Method == "GET" {
		h.read(w, r, p, kind, scope)
		return
	}
	if r.URL.RawQuery != "" {
		writeIdentityError(w, r, authz.ErrInvalid)
		return
	}
	input := goldenpath.ConfigurationInput{Kind: kind, ScopeID: scope, UserID: r.PathValue("user_id")}
	fields := []string{"client_operation_id"}
	switch kind {
	case "teams":
		input.Kind = "team.create"
		fields = append(fields, "name")
	case "project.create":
		fields = append(fields, "key", "name", "owner_team_id", "visibility", "initial_admin_user_id")
	case "channel.create":
		fields = append(fields, "name", "visibility", "member_user_ids")
	case "project.member":
		input.Kind = "project.member.remove"
		fields = append(fields, "expected_role")
		if r.Method == "PUT" {
			input.Kind = "project.member.set"
			fields = append(fields, "role")
		}
	case "channel.member":
		input.Kind = "channel.member.remove"
		fields = append(fields, "expected_member")
		if r.Method == "PUT" {
			input.Kind = "channel.member.add"
		}
	}
	body, err := decodeStrictObject(w, r, fields, 32*1024)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	for key, value := range body {
		var target any
		switch key {
		case "client_operation_id":
			target = &input.ClientOperationID
		case "name":
			target = &input.Name
		case "key":
			target = &input.Key
		case "owner_team_id":
			target = &input.OwnerTeamID
		case "visibility":
			target = &input.Visibility
		case "initial_admin_user_id":
			target = &input.InitialAdminUserID
		case "member_user_ids":
			target = &input.MemberUserIDs
		case "role":
			target = &input.Role
		case "expected_role":
			target = &input.ExpectedRole
		case "expected_member":
			target = &input.ExpectedMember
		}
		if key != "expected_role" && string(value) == "null" {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		if err = json.Unmarshal(value, target); err != nil {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
	}
	result, err := h.application.Configure(r.Context(), webInvocation(p, r), input)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	var data any
	if input.UserID != "" {
		if result.UserID != input.UserID || result.Created || result.Object.ID != "" {
			writeIdentityError(w, r, errors.New("invalid configuration member result"))
			return
		}
		data = struct {
			UserID  string `json:"user_id"`
			Applied bool   `json:"applied"`
		}{result.UserID, true}
	} else {
		if input.Kind == "channel.create" && result.Object.ProjectID != input.ScopeID {
			writeIdentityError(w, r, errors.New("invalid configuration channel scope"))
			return
		}
		if result.Object.Kind != strings.TrimSuffix(input.Kind, ".create") {
			writeIdentityError(w, r, errors.New("invalid configuration object kind"))
			return
		}
		data, err = configurationObjectDTO(result.Object)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
	}
	writeIdentityJSON(w, r, status, struct {
		Data any `json:"data"`
	}{data})
}

func configurationObjectDTO(o goldenpath.ConfigurationObject) (any, error) {
	invalid := errors.New("invalid configuration object projection")
	if !utf8.ValidString(o.Name) || strings.TrimSpace(o.Name) == "" || strings.ContainsRune(o.Name, '\x00') {
		return nil, invalid
	}
	if o.Kind == "team" {
		if !validScopedID(o.ID, "tem_") {
			return nil, invalid
		}
		return struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{o.ID, o.Name}, nil
	}
	if o.Kind != "project" && o.Kind != "channel" {
		return nil, invalid
	}
	if !validScopedID(o.ID, discoveryPrefix(o.Kind)) || (o.Status != "active" && o.Status != "archived") {
		return nil, invalid
	}
	if (o.Kind == "project" && o.Visibility != "workspace" && o.Visibility != "restricted") || (o.Kind == "channel" && o.Visibility != "project" && o.Visibility != "restricted") {
		return nil, invalid
	}
	if o.Kind == "channel" && !validScopedID(o.ProjectID, "prj_") {
		return nil, invalid
	}
	if o.Kind == "project" && o.Key == "" {
		return nil, invalid
	}
	data := map[string]any{"ref": entityRefDTO{Type: o.Kind, ID: o.ID}, "name": o.Name, "visibility": o.Visibility, "status": o.Status, "capabilities": map[string]bool{"manage": o.CanManage}}
	if o.Kind == "project" {
		data["key"] = o.Key
	} else {
		data["project"] = entityRefDTO{Type: "project", ID: o.ProjectID}
	}
	return data, nil
}

func (h *ConfigurationHandler) read(w http.ResponseWriter, r *http.Request, p authz.Principal, kind, scope string) {
	if kind == "project" || kind == "channel" {
		if r.URL.RawQuery != "" {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		o, err := h.application.ReadConfiguration(r.Context(), p, kind, scope)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		if o.ID != scope || o.Kind != kind {
			writeIdentityError(w, r, errors.New("configuration scope mismatch"))
			return
		}
		data, err := configurationObjectDTO(o)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		writeIdentityJSON(w, r, 200, struct {
			Data any `json:"data"`
		}{data})
		return
	}
	cursorScope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, Kind: kind, ProjectID: scope}
	input, err := parseDiscoveryQuery(r.URL.RawQuery, cursorScope)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	page, err := h.application.ListConfiguration(r.Context(), p, goldenpath.ConfigurationQuery{Kind: kind, ScopeID: scope, AfterID: input.AfterID, Limit: input.Limit})
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	items := []any{}
	last := input.AfterID
	invalid := errors.New("invalid configuration list projection")
	if kind == "teams" {
		for _, o := range page.Teams {
			if o.Kind != "team" || o.ID <= last {
				writeIdentityError(w, r, invalid)
				return
			}
			dto, e := configurationObjectDTO(o)
			if e != nil {
				writeIdentityError(w, r, e)
				return
			}
			items = append(items, dto)
			last = o.ID
		}
	} else {
		for _, m := range page.Members {
			if !validScopedID(m.ID, "usr_") || m.ID <= last || !utf8.ValidString(m.Name) || strings.TrimSpace(m.Name) == "" || strings.ContainsRune(m.Name, '\x00') {
				writeIdentityError(w, r, invalid)
				return
			}
			user := map[string]string{"id": m.ID, "display_name": m.Name}
			if kind == "members" {
				items = append(items, user)
			} else {
				row := map[string]any{"user": user, "eligible": m.Eligible}
				if kind == "project-members" {
					if m.Role != "viewer" && m.Role != "contributor" && m.Role != "decider" && m.Role != "admin" {
						writeIdentityError(w, r, invalid)
						return
					}
					row["role"] = m.Role
				}
				items = append(items, row)
			}
			last = m.ID
		}
	}
	if len(items) > input.Limit {
		writeIdentityError(w, r, invalid)
		return
	}
	var next *string
	if page.NextID != "" {
		if len(items) != input.Limit || page.NextID != last {
			writeIdentityError(w, r, invalid)
			return
		}
		cursorScope.AfterID = last
		b, e := json.Marshal(cursorScope)
		if e != nil {
			writeIdentityError(w, r, e)
			return
		}
		encoded := base64.RawURLEncoding.EncodeToString(b)
		next = &encoded
	}
	writeIdentityJSON(w, r, 200, struct {
		Data any `json:"data"`
	}{map[string]any{"items": items, "next_cursor": next}})
}
