package httptransport

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
)

func repositoryConfigurationDTO(o goldenpath.ConfigurationObject, detail bool) (any, error) {
	invalid := errors.New("invalid Repository configuration projection")
	if o.Kind != "repository" || !validScopedID(o.ID, "rep_") || o.Repository == nil || !utf8.ValidString(o.Name) || strings.TrimSpace(o.Name) == "" || strings.ContainsFunc(o.Name, unicode.IsControl) || len(o.Name) > 480 || utf8.RuneCountInString(o.Name) > 120 || o.CanManage || o.CanGrant || o.CanRevoke || o.CanLinkRepository {
		return nil, invalid
	}
	m, ok := goldenpath.NormalizeRepositoryMetadata(*o.Repository)
	if !ok || m != *o.Repository {
		return nil, invalid
	}
	data := map[string]any{
		"ref": entityRefDTO{Type: "repository", ID: o.ID}, "name": o.Name,
		"provider": m.Provider, "provider_origin": m.ProviderOrigin,
		"external_id": m.ExternalID, "web_url": m.WebURL, "default_branch": m.DefaultBranch,
	}
	if detail {
		data["capabilities"] = map[string]bool{}
	}
	return data, nil
}
