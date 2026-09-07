package httptransport

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
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
			want: ErrorMapping{StatusCode: http.StatusUnauthorized, Code: "unauthenticated", Message: "authentication required"},
		},
		{
			name: "invalid credentials",
			err:  authn.ErrInvalidCredentials,
			want: ErrorMapping{StatusCode: http.StatusUnauthorized, Code: "invalid_credentials", Message: "invalid credentials"},
		},
		{
			name: "rate limited",
			err:  ErrLoginRateLimited,
			want: ErrorMapping{StatusCode: http.StatusTooManyRequests, Code: "rate_limited", Message: "too many login attempts"},
		},
		{
			name: "method not allowed",
			err:  ErrMethodNotAllowed,
			want: ErrorMapping{StatusCode: http.StatusMethodNotAllowed, Code: "method_not_allowed", Message: "method not allowed"},
		},
		{
			name: "csrf",
			err:  authn.ErrInvalidCSRFToken,
			want: ErrorMapping{StatusCode: http.StatusForbidden, Code: "csrf_failed", Message: "csrf validation failed"},
		},
		{
			name: "forbidden",
			err:  authz.ErrForbidden,
			want: ErrorMapping{StatusCode: http.StatusForbidden, Code: "forbidden", Message: "access denied"},
		},
		{
			name: "not found",
			err:  authz.ErrNotFound,
			want: ErrorMapping{StatusCode: http.StatusNotFound, Code: "not_found", Message: "resource not found"},
		},
		{
			name: "conflict",
			err:  authz.ErrConflict,
			want: ErrorMapping{StatusCode: http.StatusConflict, Code: "conflict", Message: "request conflicts with current state"},
		},
		{
			name: "invalid",
			err:  authz.ErrInvalid,
			want: ErrorMapping{StatusCode: http.StatusBadRequest, Code: "invalid", Message: "invalid request"},
		},
		{
			name: "unknown",
			err:  unknown,
			want: ErrorMapping{StatusCode: http.StatusInternalServerError, Code: "internal", Message: "internal server error"},
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

func TestWriteErrorUsesVersionedSafeEnvelope(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	internal := errors.New("password hash parse detail must stay private")
	requestID := "req_00000000000000000000000000000000"
	if err := WriteError(response, requestID, internal); err != nil {
		t.Fatalf("WriteError() error = %v", err)
	}
	if response.Code != http.StatusInternalServerError ||
		response.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("X-Request-ID") != requestID {
		t.Fatalf("response = status %d, headers %#v", response.Code, response.Header())
	}
	want := "{\"error\":{\"code\":\"internal\",\"message\":\"internal server error\",\"request_id\":\"" + requestID + "\"}}\n"
	if response.Body.String() != want || strings.Contains(response.Body.String(), internal.Error()) {
		t.Fatalf("response body = %q", response.Body.String())
	}
}

func TestWriteErrorPreservesCallerNoStoreScope(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	response.Header().Set("Cache-Control", "private, no-store")
	requestID := "req_00000000000000000000000000000000"
	if err := WriteError(response, requestID, authz.ErrNotFound); err != nil {
		t.Fatalf("WriteError() error = %v", err)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestWriteErrorRejectsMissingRequestIDBeforeWriting(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	if err := WriteError(response, "", authz.ErrInvalid); err == nil {
		t.Fatal("WriteError() error = nil")
	}
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("response = status %d, body %q", response.Code, response.Body.String())
	}
}

func TestNewRequestIDUsesStableOpaqueFormat(t *testing.T) {
	t.Parallel()
	first, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID() first error = %v", err)
	}
	second, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID() second error = %v", err)
	}
	if len(first) != 36 || !strings.HasPrefix(first, "req_") || first == second {
		t.Fatalf("request IDs = %q / %q", first, second)
	}
}
