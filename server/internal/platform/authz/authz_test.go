package authz

import (
	"errors"
	"testing"
)

func TestDecisionAcceptanceIsHumanAndRoleBound(t *testing.T) {
	t.Parallel()

	if err := (Principal{Kind: PrincipalSystem, ID: "system", WorkspaceID: "wrk_1"}).ValidateUser(); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("system principal validation error = %v", err)
	}
	if err := RequireDecisionAccept(RoleContributor); !errors.Is(err, ErrForbidden) {
		t.Fatalf("contributor acceptance error = %v", err)
	}
	if err := RequireDecisionAccept(RoleDecider); err != nil {
		t.Fatalf("decider acceptance error = %v", err)
	}
}
