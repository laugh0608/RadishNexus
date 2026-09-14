package httptransport

import (
	"errors"
	"fmt"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"net/http"
)

func authenticateWorkspaceRequest(request *http.Request, workspaceID string, write bool, sessions MessagingSessionService, session BrowserSessionPolicy, proxy TrustedProxyPolicy) (authz.Principal, error) {
	if _, err := proxy.ClientIP(request); err != nil {
		return authz.Principal{}, err
	}
	if err := session.ValidateHost(request); err != nil {
		return authz.Principal{}, err
	}
	token, err := session.SessionToken(request)
	if err != nil {
		return authz.Principal{}, err
	}
	if write {
		csrfToken, err := session.ValidateCSRF(request)
		if err != nil {
			return authz.Principal{}, err
		}
		if err := sessions.VerifyCSRF(request.Context(), token, csrfToken); err != nil {
			return authz.Principal{}, err
		}
	}
	if !validScopedID(workspaceID, "wrk_") {
		return authz.Principal{}, fmt.Errorf("%w: invalid Workspace ID", authz.ErrInvalid)
	}
	verified, err := sessions.ResolveWorkspace(request.Context(), token, workspaceID)
	if err != nil {
		if errors.Is(err, authz.ErrForbidden) {
			err = authz.ErrNotFound
		}
		return authz.Principal{}, err
	}
	return authn.UserPrincipal(verified)
}
