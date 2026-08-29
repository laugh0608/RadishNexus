// Package httptransport owns internal HTTP adapter semantics that are not yet
// a public API contract.
package httptransport

import (
	"errors"
	"net/http"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// ErrorMapping contains only the status and safe machine code needed by a
// future handler. It deliberately excludes the original error text and does
// not define a public JSON error schema.
type ErrorMapping struct {
	StatusCode int
	Code       string
}

// MapApplicationError preserves errors.Is semantics for wrapped application
// errors. Unknown failures become a generic internal error and must be logged
// separately without exposing their cause to the caller.
func MapApplicationError(err error) ErrorMapping {
	switch {
	case errors.Is(err, authz.ErrUnauthenticated):
		return ErrorMapping{StatusCode: http.StatusUnauthorized, Code: "unauthenticated"}
	case errors.Is(err, authz.ErrForbidden):
		return ErrorMapping{StatusCode: http.StatusForbidden, Code: "forbidden"}
	case errors.Is(err, authz.ErrNotFound):
		return ErrorMapping{StatusCode: http.StatusNotFound, Code: "not_found"}
	case errors.Is(err, authz.ErrConflict):
		return ErrorMapping{StatusCode: http.StatusConflict, Code: "conflict"}
	case errors.Is(err, authz.ErrInvalid):
		return ErrorMapping{StatusCode: http.StatusBadRequest, Code: "invalid"}
	default:
		return ErrorMapping{StatusCode: http.StatusInternalServerError, Code: "internal"}
	}
}
