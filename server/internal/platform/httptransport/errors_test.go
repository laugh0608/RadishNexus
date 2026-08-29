package httptransport

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestMapApplicationError(t *testing.T) {
	t.Parallel()

	unknown := errors.New("database connection detail must stay internal")
	tests := []struct {
		name string
		err  error
		want ErrorMapping
	}{
		{
			name: "wrapped unauthenticated",
			err:  fmt.Errorf("authenticate request: %w", authz.ErrUnauthenticated),
			want: ErrorMapping{StatusCode: http.StatusUnauthorized, Code: "unauthenticated"},
		},
		{
			name: "forbidden",
			err:  authz.ErrForbidden,
			want: ErrorMapping{StatusCode: http.StatusForbidden, Code: "forbidden"},
		},
		{
			name: "not found",
			err:  authz.ErrNotFound,
			want: ErrorMapping{StatusCode: http.StatusNotFound, Code: "not_found"},
		},
		{
			name: "conflict",
			err:  authz.ErrConflict,
			want: ErrorMapping{StatusCode: http.StatusConflict, Code: "conflict"},
		},
		{
			name: "invalid",
			err:  authz.ErrInvalid,
			want: ErrorMapping{StatusCode: http.StatusBadRequest, Code: "invalid"},
		},
		{
			name: "unknown",
			err:  unknown,
			want: ErrorMapping{StatusCode: http.StatusInternalServerError, Code: "internal"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := MapApplicationError(test.err); got != test.want {
				t.Fatalf("MapApplicationError() = %#v, want %#v", got, test.want)
			}
		})
	}
}
