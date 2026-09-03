// Package httptransport owns the versioned public HTTP error boundary.
package httptransport

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

var (
	ErrSecureTransportRequired = errors.New("secure transport required")
	ErrInvalidPublicOrigin     = errors.New("invalid public origin")
	ErrInvalidProxyChain       = errors.New("invalid trusted proxy chain")
	ErrLoginRateLimited        = errors.New("login rate limited")
	ErrPayloadTooLarge         = errors.New("request payload too large")
	ErrUnsupportedMediaType    = errors.New("unsupported media type")
	ErrMethodNotAllowed        = errors.New("method not allowed")
	ErrRealtimeCapacity        = errors.New("realtime connection capacity reached")
)

type ErrorMapping struct {
	StatusCode int
	Code       string
	Message    string
}

type ErrorObject struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type ErrorResponse struct {
	Error ErrorObject `json:"error"`
}

// MapApplicationError preserves errors.Is semantics for wrapped application
// errors. Unknown failures become a generic internal error and must be logged
// separately without exposing their cause to the caller.
func MapApplicationError(err error) ErrorMapping {
	switch {
	case errors.Is(err, ErrSecureTransportRequired):
		return ErrorMapping{
			StatusCode: http.StatusBadRequest,
			Code:       "secure_transport_required",
			Message:    "secure transport required",
		}
	case errors.Is(err, ErrInvalidPublicOrigin):
		return ErrorMapping{
			StatusCode: http.StatusBadRequest,
			Code:       "invalid_origin",
			Message:    "invalid public origin",
		}
	case errors.Is(err, ErrInvalidProxyChain):
		return ErrorMapping{
			StatusCode: http.StatusBadRequest,
			Code:       "invalid_proxy_chain",
			Message:    "invalid proxy chain",
		}
	case errors.Is(err, ErrLoginRateLimited):
		return ErrorMapping{
			StatusCode: http.StatusTooManyRequests,
			Code:       "rate_limited",
			Message:    "too many login attempts",
		}
	case errors.Is(err, ErrRealtimeCapacity):
		return ErrorMapping{
			StatusCode: http.StatusTooManyRequests,
			Code:       "rate_limited",
			Message:    "too many active streams",
		}
	case errors.Is(err, ErrPayloadTooLarge):
		return ErrorMapping{
			StatusCode: http.StatusRequestEntityTooLarge,
			Code:       "payload_too_large",
			Message:    "request payload too large",
		}
	case errors.Is(err, ErrUnsupportedMediaType):
		return ErrorMapping{
			StatusCode: http.StatusUnsupportedMediaType,
			Code:       "unsupported_media_type",
			Message:    "application/json is required",
		}
	case errors.Is(err, ErrMethodNotAllowed):
		return ErrorMapping{
			StatusCode: http.StatusMethodNotAllowed,
			Code:       "method_not_allowed",
			Message:    "method not allowed",
		}
	case errors.Is(err, authn.ErrInvalidCredentials):
		return ErrorMapping{
			StatusCode: http.StatusUnauthorized,
			Code:       "invalid_credentials",
			Message:    "invalid credentials",
		}
	case errors.Is(err, authn.ErrInvalidSession), errors.Is(err, authz.ErrUnauthenticated):
		return ErrorMapping{
			StatusCode: http.StatusUnauthorized,
			Code:       "unauthenticated",
			Message:    "authentication required",
		}
	case errors.Is(err, authn.ErrInvalidCSRFToken):
		return ErrorMapping{
			StatusCode: http.StatusForbidden,
			Code:       "csrf_failed",
			Message:    "csrf validation failed",
		}
	case errors.Is(err, authz.ErrForbidden):
		return ErrorMapping{StatusCode: http.StatusForbidden, Code: "forbidden", Message: "access denied"}
	case errors.Is(err, authz.ErrNotFound):
		return ErrorMapping{StatusCode: http.StatusNotFound, Code: "not_found", Message: "resource not found"}
	case errors.Is(err, authz.ErrConflict):
		return ErrorMapping{
			StatusCode: http.StatusConflict,
			Code:       "conflict",
			Message:    "request conflicts with current state",
		}
	case errors.Is(err, authz.ErrInvalid):
		return ErrorMapping{StatusCode: http.StatusBadRequest, Code: "invalid", Message: "invalid request"}
	default:
		return ErrorMapping{
			StatusCode: http.StatusInternalServerError,
			Code:       "internal",
			Message:    "internal server error",
		}
	}
}

func NewRequestID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate request ID: %w", err)
	}
	return "req_" + hex.EncodeToString(random), nil
}

func WriteError(response http.ResponseWriter, requestID string, err error) error {
	if !validRequestID(requestID) {
		return fmt.Errorf("invalid request ID")
	}
	mapping := MapApplicationError(err)
	body, marshalErr := json.Marshal(ErrorResponse{Error: ErrorObject{
		Code:      mapping.Code,
		Message:   mapping.Message,
		RequestID: requestID,
	}})
	if marshalErr != nil {
		return fmt.Errorf("marshal public error response: %w", marshalErr)
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	if response.Header().Get("Cache-Control") == "" {
		response.Header().Set("Cache-Control", "no-store")
	}
	response.Header().Set("X-Request-ID", requestID)
	response.WriteHeader(mapping.StatusCode)
	if _, writeErr := response.Write(append(body, '\n')); writeErr != nil {
		return fmt.Errorf("write public error response: %w", writeErr)
	}
	return nil
}

func validRequestID(requestID string) bool {
	if len(requestID) != 36 || requestID[:4] != "req_" {
		return false
	}
	decoded, err := hex.DecodeString(requestID[4:])
	return err == nil && len(decoded) == 16
}
