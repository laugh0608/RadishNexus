package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeReadinessChecker struct{ err error }

func (pinger fakeReadinessChecker) CheckReady(context.Context) error { return pinger.err }

type readinessFunc func(context.Context) error

func (check readinessFunc) CheckReady(ctx context.Context) error { return check(ctx) }

func TestHandlerRoutesDiscoveryBeforeWorkspaceFallback(t *testing.T) {
	t.Parallel()
	discovery := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusAccepted) })
	handler := newHandler(fakeReadinessChecker{}, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), discovery, http.NotFoundHandler())
	for _, path := range []string{"/api/v1/workspaces/wrk_main/projects", "/api/v1/workspaces/wrk_main/projects/prj_main/channels"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusAccepted {
			t.Fatal(path, response.Code)
		}
	}
}

func TestReadinessIsBoundedUncachedAndDoesNotExposeDatabaseDetails(t *testing.T) {
	t.Parallel()
	calls := 0
	check := readinessFunc(func(ctx context.Context) error {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("readiness has no bounded deadline")
		}
		return errors.New("private database detail / migration checksum")
	})
	handler := newHandler(check, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler())
	for range 2 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		if response.Code != http.StatusServiceUnavailable || response.Body.String() != "not ready\n" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unexpected readiness response: %d %q %v", response.Code, response.Body.String(), response.Header())
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Code != http.StatusNoContent || calls != 2 {
		t.Fatal("liveness depended on schema or readiness reused stale state")
	}
}

func TestHealthRoutesUseMethodPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pinger     fakeReadinessChecker
		method     string
		path       string
		wantStatus int
	}{
		{name: "live", method: http.MethodGet, path: "/health/live", wantStatus: http.StatusNoContent},
		{name: "ready", method: http.MethodGet, path: "/health/ready", wantStatus: http.StatusNoContent},
		{name: "database unavailable", pinger: fakeReadinessChecker{err: errors.New("offline")}, method: http.MethodGet, path: "/health/ready", wantStatus: http.StatusServiceUnavailable},
		{name: "wrong method", method: http.MethodPost, path: "/health/live", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			newHandler(test.pinger, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler()).ServeHTTP(response, request)
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

	newHandler(fakeReadinessChecker{}, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler()).ServeHTTP(response, request)

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
		fakeReadinessChecker{},
		http.NotFoundHandler(),
		channelMessages,
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		deploymentFallback,
		http.NotFoundHandler(),
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
		fakeReadinessChecker{},
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		channelEvents,
		http.NotFoundHandler(),
		deploymentFallback,
		http.NotFoundHandler(),
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
		fakeReadinessChecker{},
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		collaboration,
		deploymentFallback,
		http.NotFoundHandler(),
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
