// Package authn adapts successful authentication results into the
// transport-independent principals consumed by application services.
package authn

import (
	"fmt"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// VerifiedUser is the minimal output expected from a future local-session or
// OIDC verifier. It intentionally contains no token, cookie, claim set, or
// transport metadata.
type VerifiedUser struct {
	UserID      string
	WorkspaceID string
}

// UserPrincipal converts a verified user into the only principal kind that
// user-facing application services currently accept. Authentication
// mechanisms must verify credentials before constructing VerifiedUser.
func UserPrincipal(user VerifiedUser) (authz.Principal, error) {
	principal := authz.Principal{
		Kind:        authz.PrincipalUser,
		ID:          user.UserID,
		WorkspaceID: user.WorkspaceID,
	}
	if err := principal.ValidateUser(); err != nil {
		return authz.Principal{}, fmt.Errorf("adapt verified user: %w", err)
	}
	return principal, nil
}
