package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct{ err error }

func (pinger fakePinger) Ping(context.Context) error { return pinger.err }

func TestHealthRoutesUseMethodPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pinger     fakePinger
		method     string
		path       string
		wantStatus int
	}{
		{name: "live", method: http.MethodGet, path: "/health/live", wantStatus: http.StatusNoContent},
		{name: "ready", method: http.MethodGet, path: "/health/ready", wantStatus: http.StatusNoContent},
		{name: "database unavailable", pinger: fakePinger{err: errors.New("offline")}, method: http.MethodGet, path: "/health/ready", wantStatus: http.StatusServiceUnavailable},
		{name: "wrong method", method: http.MethodPost, path: "/health/live", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			newHandler(test.pinger, http.NotFoundHandler(), http.NotFoundHandler()).ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			requestID := response.Header().Get("X-Request-ID")
			if len(requestID) != 36 || requestID[:4] != "req_" {
				t.Fatalf("X-Request-ID = %q", requestID)
			}
		})
	}
}

func TestHandlerReplacesCallerRequestID(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	request.Header.Set("X-Request-ID", "caller-controlled")
	response := httptest.NewRecorder()

	newHandler(fakePinger{}, http.NotFoundHandler(), http.NotFoundHandler()).ServeHTTP(response, request)

	if requestID := response.Header().Get("X-Request-ID"); requestID == "caller-controlled" || len(requestID) != 36 {
		t.Fatalf("X-Request-ID = %q", requestID)
	}
	if request.Header.Get("X-Request-ID") != "" {
		t.Fatalf("inbound X-Request-ID = %q", request.Header.Get("X-Request-ID"))
	}
}
