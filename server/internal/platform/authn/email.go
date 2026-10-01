package authn

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

var emailLocalPattern = regexp.MustCompile("^[a-z0-9!#$%&'*+/=?^_`{|}~.-]+$")
var emailDomainLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizeEmail defines the single credential identifier policy. It does not
// assert deliverability or verified ownership and must never merge identities.
func NormalizeEmail(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	canonical := strings.ToLower(trimmed)
	invalid := func() (string, error) {
		return "", fmt.Errorf("%w: email does not match the supported ASCII mailbox policy", authz.ErrInvalid)
	}
	for _, char := range trimmed {
		if char > 127 {
			return invalid()
		}
	}
	if len(canonical) > 254 || strings.Count(canonical, "@") != 1 {
		return invalid()
	}
	parts := strings.SplitN(canonical, "@", 2)
	local, domain := parts[0], parts[1]
	if len(local) == 0 || len(local) > 64 || !emailLocalPattern.MatchString(local) || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return invalid()
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return invalid()
	}
	for _, label := range labels {
		if !emailDomainLabelPattern.MatchString(label) {
			return invalid()
		}
	}
	return canonical, nil
}
