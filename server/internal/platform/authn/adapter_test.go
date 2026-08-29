package authn

import (
	"errors"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestUserPrincipalAdaptsVerifiedIdentity(t *testing.T) {
	t.Parallel()

	principal, err := UserPrincipal(VerifiedUser{
		UserID:      "usr_1",
		WorkspaceID: "wrk_1",
	})
	if err != nil {
		t.Fatalf("UserPrincipal() error = %v", err)
	}
	want := authz.Principal{
		Kind:        authz.PrincipalUser,
		ID:          "usr_1",
		WorkspaceID: "wrk_1",
	}
	if principal != want {
		t.Fatalf("UserPrincipal() = %#v, want %#v", principal, want)
	}
}

func TestUserPrincipalRejectsIncompleteAuthenticationResult(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		user VerifiedUser
	}{
		{name: "missing user", user: VerifiedUser{WorkspaceID: "wrk_1"}},
		{name: "missing Workspace", user: VerifiedUser{UserID: "usr_1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			principal, err := UserPrincipal(test.user)
			if !errors.Is(err, authz.ErrUnauthenticated) {
				t.Fatalf("UserPrincipal() error = %v, want unauthenticated", err)
			}
			if principal != (authz.Principal{}) {
				t.Fatalf("UserPrincipal() principal = %#v, want zero value", principal)
			}
		})
	}
}
