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
			newHandler(test.pinger, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler()).ServeHTTP(response, request)
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

	newHandler(fakePinger{}, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler()).ServeHTTP(response, request)

	if requestID := response.Header().Get("X-Request-ID"); requestID == "caller-controlled" || len(requestID) != 36 {
		t.Fatalf("X-Request-ID = %q", requestID)
	}
	if request.Header.Get("X-Request-ID") != "" {
		t.Fatalf("inbound X-Request-ID = %q", request.Header.Get("X-Request-ID"))
	}
}

func TestHandlerRoutesChannelMessagesBeforeWorkspaceFallback(t *testing.T) {
	t.Parallel()
	channelMessages := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusAccepted)
	})
	deploymentFallback := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTeapot)
	})
	handler := newHandler(
		fakePinger{},
		http.NotFoundHandler(),
		channelMessages,
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		deploymentFallback,
		http.NotFoundHandler(),
	)

	for _, path := range []string{
		"/api/v1/workspaces/wrk_main/channels/chn_main/messages",
		"/api/v1/workspaces/wrk_main/channels/chn_main/messages/msg_1/threads",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusAccepted)
		}
	}
}

func TestHandlerRoutesChannelEventsBeforeWorkspaceFallback(t *testing.T) {
	t.Parallel()
	channelEvents := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusAccepted)
	})
	deploymentFallback := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTeapot)
	})
	handler := newHandler(
		fakePinger{},
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		channelEvents,
		http.NotFoundHandler(),
		deploymentFallback,
		http.NotFoundHandler(),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/wrk_main/channels/chn_main/events", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("Channel events status = %d, want %d", response.Code, http.StatusAccepted)
	}
}

func TestHandlerRoutesCollaborationBeforeWorkspaceFallback(t *testing.T) {
	t.Parallel()
	collaboration := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusAccepted)
	})
	deploymentFallback := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTeapot)
	})
	handler := newHandler(
		fakePinger{},
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		collaboration,
		deploymentFallback,
		http.NotFoundHandler(),
	)

	for _, path := range []string{
		"/api/v1/workspaces/wrk_main/threads/thr_main/nexus-view",
		"/api/v1/workspaces/wrk_main/threads/thr_main/decisions",
		"/api/v1/workspaces/wrk_main/decisions/dec_main/nexus-view",
		"/api/v1/workspaces/wrk_main/decisions/dec_main/acceptance",
		"/api/v1/workspaces/wrk_main/decisions/dec_main/tickets",
		"/api/v1/workspaces/wrk_main/tickets/tkt_main/nexus-view",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusAccepted)
		}
	}
}
