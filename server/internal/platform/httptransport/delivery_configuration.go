package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func parseExpectedAuthorization(raw json.RawMessage) (*goldenpath.ConfigurationAuthorization, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	fields, err := decodeStrictObjectBytes(raw, []string{"id", "status"})
	if err != nil {
		return nil, err
	}
	a := &goldenpath.ConfigurationAuthorization{}
	if json.Unmarshal(fields["id"], &a.ID) != nil || json.Unmarshal(fields["status"], &a.Status) != nil || !validScopedID(a.ID, "dpa_") || (a.Status != "active" && a.Status != "revoked") {
		return nil, authz.ErrInvalid
	}
	return a, nil
}

func deliveryConfigurationDTO(o goldenpath.ConfigurationObject, detail bool) (any, error) {
	invalid := errors.New("invalid delivery configuration projection")
	if !utf8.ValidString(o.Name) || strings.TrimSpace(o.Name) == "" || strings.ContainsRune(o.Name, '\x00') || !utf8.ValidString(o.Key) || strings.TrimSpace(o.Key) == "" || strings.ContainsRune(o.Key, '\x00') {
		return nil, invalid
	}
	if o.OwnerTeamID != "" && !validScopedID(o.OwnerTeamID, "tem_") {
		return nil, invalid
	}
	var owner *string
	if o.OwnerTeamID != "" {
		owner = &o.OwnerTeamID
	}
	data := map[string]any{"ref": entityRefDTO{Type: o.Kind, ID: o.ID}, "key": o.Key, "name": o.Name, "owner_team_id": owner}
	if o.Kind == "component" {
		if !validScopedID(o.ID, "cmp_") || !goldenpath.ValidComponentType(o.Type) || (o.Status != "planned" && o.Status != "active" && o.Status != "deprecated" && o.Status != "retired") || (o.Status != "planned" && owner == nil) || o.CanManage || o.CanGrant || o.CanRevoke {
			return nil, invalid
		}
		data["type"], data["lifecycle"] = o.Type, o.Status
		if detail {
			data["capabilities"] = map[string]bool{}
		}
	} else if o.Kind == "environment" {
		if !validScopedID(o.ID, "env_") || owner == nil || (o.Status != "active" && o.Status != "archived") || (o.Classification != "development" && o.Classification != "staging" && o.Classification != "production" && o.Classification != "other") {
			return nil, invalid
		}
		if o.CanManage && o.Classification != "staging" || o.CanGrant != (o.CanManage && o.Status == "active") || o.CanRevoke != o.CanManage {
			return nil, invalid
		}
		data["classification"], data["status"] = o.Classification, o.Status
		if detail {
			data["capabilities"] = map[string]bool{"can_manage_authorizations": o.CanManage, "can_grant": o.CanGrant, "can_revoke": o.CanRevoke}
		}
	} else {
		return nil, invalid
	}
	return data, nil
}

func authorizationMemberDTO(m goldenpath.ConfigurationMember, allowEmpty bool) (any, error) {
	invalid := errors.New("invalid authorization member projection")
	if !validScopedID(m.ID, "usr_") || !utf8.ValidString(m.Name) || strings.TrimSpace(m.Name) == "" || strings.ContainsRune(m.Name, '\x00') {
		return nil, invalid
	}
	if a := m.Authorization; a != nil {
		if !validScopedID(a.ID, "dpa_") || (a.Status != "active" && a.Status != "revoked") {
			return nil, invalid
		}
	} else if !allowEmpty || !m.Eligible {
		return nil, invalid
	}
	return map[string]any{"user": map[string]string{"id": m.ID, "display_name": m.Name}, "eligible": m.Eligible, "authorization": m.Authorization}, nil
}

func (h *ConfigurationHandler) readAuthorization(w http.ResponseWriter, r *http.Request, p authz.Principal, scope string) {
	user := r.PathValue("user_id")
	if r.URL.RawQuery != "" || !validScopedID(user, "usr_") {
		writeIdentityError(w, r, authz.ErrInvalid)
		return
	}
	page, err := h.application.ListConfiguration(r.Context(), p, goldenpath.ConfigurationQuery{Kind: "environment-authorization", ScopeID: scope, UserID: user, Limit: 1})
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	if len(page.Members) != 1 || page.Members[0].ID != user || page.NextID != "" {
		writeIdentityError(w, r, errors.New("invalid authorization subject projection"))
		return
	}
	data, err := authorizationMemberDTO(page.Members[0], true)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeIdentityJSON(w, r, 200, struct {
		Data any `json:"data"`
	}{data})
}
