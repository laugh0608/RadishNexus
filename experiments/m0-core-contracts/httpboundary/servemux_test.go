package httpboundary

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeMuxSupportsMethodAndEntityPathSegments(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v0/entities/{type}/{id}", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte(request.PathValue("type") + "/" + request.PathValue("id")))
	})

	request := httptest.NewRequest(http.MethodGet, "/v0/entities/decision/dec_01K4EXAMPLE", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "decision/dec_01K4EXAMPLE" {
		t.Fatalf("GET body = %q", got)
	}

	request = httptest.NewRequest(http.MethodPost, "/v0/entities/decision/dec_01K4EXAMPLE", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
