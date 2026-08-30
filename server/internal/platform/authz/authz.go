// Package authz defines transport-independent principals and M0 policy facts.
package authz

import (
	"errors"
	"fmt"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrInvalid         = errors.New("invalid input")
)

type PrincipalKind string

const (
	PrincipalUser   PrincipalKind = "user"
	PrincipalSystem PrincipalKind = "system"
)

// Principal is produced by a future authentication adapter. It deliberately
// contains no token, session, or HTTP details.
type Principal struct {
	Kind        PrincipalKind
	ID          string
	WorkspaceID string
}

func (principal Principal) ValidateUser() error {
	if principal.Kind != PrincipalUser || principal.ID == "" || principal.WorkspaceID == "" {
		return ErrUnauthenticated
	}
	return nil
}

type ProjectRole string

const (
	RoleNone        ProjectRole = ""
	RoleViewer      ProjectRole = "viewer"
	RoleContributor ProjectRole = "contributor"
	RoleDecider     ProjectRole = "decider"
	RoleAdmin       ProjectRole = "admin"
)

func (role ProjectRole) CanContribute() bool {
	return role == RoleContributor || role == RoleDecider || role == RoleAdmin
}

func (role ProjectRole) CanAcceptDecision() bool {
	return role == RoleDecider || role == RoleAdmin
}

func RequireContribute(role ProjectRole) error {
	if !role.CanContribute() {
		return fmt.Errorf("%w: project contribution requires contributor, decider, or admin", ErrForbidden)
	}
	return nil
}

func RequireDecisionAccept(role ProjectRole) error {
	if !role.CanAcceptDecision() {
		return fmt.Errorf("%w: accepting a decision requires decider or admin", ErrForbidden)
	}
	return nil
}
