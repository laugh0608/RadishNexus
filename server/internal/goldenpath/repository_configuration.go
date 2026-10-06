package goldenpath

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RepositoryMetadata describes a user-declared external mapping, never a
// verified provider connection or a source of authority.
type RepositoryMetadata struct {
	Provider       string
	ProviderOrigin string
	ExternalID     string
	WebURL         string
	DefaultBranch  string
}

// Kept in an omitempty branch so existing configuration receipts keep their
// exact digest representation, including the pre-Repository commands.
type RepositoryConfigurationInput struct {
	RepositoryMetadata
	RepositoryID string
	LinkID       string
	Confirmed    bool
}

type RepositoryLink struct {
	ID        string
	Target    ConfigurationObject
	CanUnlink bool
}

func IsRepositoryConfiguration(kind string) bool {
	return kind == "repository.create" || kind == "component.repository.link" || kind == "component.repository.unlink"
}

// SubjectID preserves the original UserID input field/digest for membership
// commands while giving non-user commands an explicit typed subject.
func (input ConfigurationInput) SubjectID() string {
	if input.Repository != nil {
		if input.Kind == "component.repository.link" {
			return input.Repository.RepositoryID
		}
		if input.Kind == "component.repository.unlink" {
			return input.Repository.LinkID
		}
	}
	return input.UserID
}

func validateRepositoryConfiguration(in ConfigurationInput, workspace string) (ConfigurationInput, string, bool) {
	if in.Repository == nil || in.Delivery != nil || in.UserID != "" || in.Key != "" || in.OwnerTeamID != "" || in.Visibility != "" || in.InitialAdminUserID != "" || in.MemberUserIDs != nil || in.Role != "" || in.ExpectedRole != nil || in.ExpectedMember {
		return in, "", false
	}
	r := *in.Repository
	in.Repository = &r
	if in.Kind == "repository.create" {
		if in.ScopeID != workspace || r.RepositoryID != "" || r.LinkID != "" || r.Confirmed {
			return in, "", false
		}
		metadata, ok := NormalizeRepositoryMetadata(r.RepositoryMetadata)
		r.RepositoryMetadata = metadata
		return in, "rep_", ok
	}
	if !ValidConfigurationID(in.ScopeID, "cmp_") || in.Name != "" || !r.Confirmed || r.RepositoryMetadata != (RepositoryMetadata{}) {
		return in, "", false
	}
	if in.Kind == "component.repository.link" {
		return in, "lnk_", ValidConfigurationID(r.RepositoryID, "rep_") && r.LinkID == ""
	}
	return in, "", ValidConfigurationID(r.LinkID, "lnk_") && r.RepositoryID == ""
}

var repositoryBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
var repositoryHostLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizeRepositoryMetadata uses the same rules for input, digest and safe
// projection. It never resolves a host, follows a URL or executes Git.
func NormalizeRepositoryMetadata(m RepositoryMetadata) (RepositoryMetadata, bool) {
	if m.Provider != "github" && m.Provider != "gitlab" && m.Provider != "gitea" {
		return m, false
	}
	if len(m.ExternalID) < 1 || len(m.ExternalID) > 255 || !utf8.ValidString(m.ExternalID) || strings.ContainsFunc(m.ExternalID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return m, false
	}
	origin, ok := repositoryURL(m.ProviderOrigin, true)
	if !ok {
		return m, false
	}
	web, ok := repositoryURL(m.WebURL, false)
	if !ok || web.Host != origin.Host {
		return m, false
	}
	if !repositoryBranch.MatchString(m.DefaultBranch) || strings.Contains(m.DefaultBranch, "..") {
		return m, false
	}
	for part := range strings.SplitSeq(m.DefaultBranch, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return m, false
		}
	}
	m.ProviderOrigin, m.WebURL = origin.String(), web.String()
	return m, true
}

func repositoryURL(raw string, origin bool) (*url.URL, bool) {
	maxLength := 2048
	if origin {
		maxLength = 512
	}
	if len(raw) == 0 || len(raw) > maxLength || !utf8.ValidString(raw) || strings.ContainsAny(raw, `\?#`) || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Opaque != "" || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return nil, false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || len(host) > 253 || strings.HasSuffix(host, ".") || strings.HasSuffix(u.Host, ":") {
		return nil, false
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		// Browsers interpret shortened, octal and hexadecimal IPv4 hostnames.
		// Reject those ambiguous forms rather than storing a different identity
		// from the origin the browser will actually open.
		labels := strings.Split(host, ".")
		last := labels[len(labels)-1]
		if strings.ContainsAny(u.Host, "[]") || strings.HasPrefix(last, "0x") || strings.Trim(last, "0123456789") == "" {
			return nil, false
		}
		for label := range strings.SplitSeq(host, ".") {
			if !repositoryHostLabel.MatchString(label) {
				return nil, false
			}
		}
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return nil, false
		}
		if port == "443" {
			port = ""
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u.Scheme, u.Host = "https", host
	if port != "" {
		u.Host += ":" + port
	}
	if origin {
		if u.Path != "" && u.Path != "/" || u.RawPath != "" {
			return nil, false
		}
		u.Path = ""
		return u, true
	}
	if !strings.HasPrefix(u.Path, "/") || u.Path == "/" || strings.Contains(u.Path, "//") || strings.ContainsAny(u.Path, `\%?#`) || strings.ContainsFunc(u.Path, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return nil, false
	}
	// Detect encoded separators before canonicalizing RawPath. A single decode
	// of %25 is rejected above, so double-encoded paths cannot bypass this check.
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") {
		return nil, false
	}
	for part := range strings.SplitSeq(u.Path, "/") {
		if part == "." || part == ".." {
			return nil, false
		}
	}
	u.RawPath = ""
	if len(u.String()) > maxLength {
		return nil, false
	}
	return u, true
}
