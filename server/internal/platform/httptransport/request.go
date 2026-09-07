package httptransport

import (
	"context"
	"log"
	"net/http"
)

type requestIDContextKey struct{}

// WithRequestID replaces any caller-provided request ID with a server-generated
// value before the request reaches public handlers.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request.Header.Del("X-Request-ID")
		requestID, err := NewRequestID()
		if err != nil {
			log.Printf("generate public request ID: %v", err)
			response.Header().Set("Cache-Control", "no-store")
			http.Error(response, "internal server error", http.StatusInternalServerError)
			return
		}
		response.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(request.Context(), requestIDContextKey{}, requestID)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}
