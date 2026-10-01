package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type stagingApp struct {
	calls     int
	duplicate bool
	err       error
	input     goldenpath.RecordStagingDeploymentInput
	inv       goldenpath.Invocation
	page      goldenpath.StagingTargetPage
}

func (a *stagingApp) RecordStagingDeployment(_ context.Context, inv goldenpath.Invocation, in goldenpath.RecordStagingDeploymentInput) (goldenpath.Deployment, error) {
	a.calls++
	a.input = in
	a.inv = inv
	return goldenpath.Deployment{ID: "dpl_recorded", Duplicate: a.duplicate}, a.err
}
func (a *stagingApp) ListStagingTargets(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.StagingTargetPage, error) {
	a.calls++
	return a.page, a.err
}

const stagingBody = `{"client_operation_id":"record-1","environment_id":"env_stage","status":"failed","started_at":null,"completed_at":"2026-09-26T01:00:00Z","confirmed":true}`

func TestStagingTransportBoundary(t *testing.T) {
	session, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	for _, tt := range []struct {
		name, body string
		mutate     func(*http.Request)
		err        error
		duplicate  bool
		want       int
	}{
		{name: "created", body: stagingBody, want: 201}, {name: "duplicate", body: stagingBody, duplicate: true, want: 200},
		{name: "not confirmed", body: strings.Replace(stagingBody, `true`, `false`, 1), want: 400},
		{name: "missing null", body: strings.Replace(stagingBody, `"started_at":null,`, "", 1), want: 400},
		{name: "duplicate key", body: strings.Replace(stagingBody, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), want: 400},
		{name: "unknown", body: strings.Replace(stagingBody, `"confirmed":true`, `"confirmed":true,"authorization_id":"private"`, 1), want: 400},
		{name: "invalid UTF8", body: strings.Replace(stagingBody, `record-1`, string([]byte{255}), 1), want: 400},
		{name: "trailing", body: stagingBody + `{}`, want: 400},
		{name: "oversize", body: strings.Repeat(" ", 8192) + stagingBody, want: 413},
		{name: "precision", body: strings.Replace(stagingBody, "01:00:00Z", "01:00:00.0001Z", 1), want: 400},
		{name: "timezone", body: strings.Replace(stagingBody, "01:00:00Z", "01:00:00+01:00", 1), want: 400},
		{name: "csrf", body: stagingBody, mutate: func(r *http.Request) { r.Header.Del(CSRFHeaderName) }, want: 403},
		{name: "origin", body: stagingBody, mutate: func(r *http.Request) { r.Header.Set("Origin", "https://wrong.test") }, want: 403},
		{name: "cookie", body: stagingBody, mutate: func(r *http.Request) { r.Header.Del("Cookie") }, want: 401},
		{name: "query", body: stagingBody, mutate: func(r *http.Request) { r.URL.RawQuery = "x=1" }, want: 400},
		{name: "method", body: stagingBody, mutate: func(r *http.Request) { r.Method = "PUT" }, want: 405},
		{name: "unreadable", body: stagingBody, err: authz.ErrNotFound, want: 404},
		{name: "authorization", body: stagingBody, err: authz.ErrForbidden, want: 403},
		{name: "conflict", body: stagingBody, err: authz.ErrConflict, want: 409},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &stagingApp{err: tt.err, duplicate: tt.duplicate}
			h := WithRequestID(NewStagingDeploymentHandler(validMessagingSessions(), a, session, proxy))
			r := messagingWriteRequest("POST", "/api/v1/workspaces/wrk_main/ci-runs/cir_build/staging-deployments", tt.body)
			if tt.mutate != nil {
				tt.mutate(r)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "Cookie" {
				t.Fatal("cacheable")
			}
			if tt.want < 300 {
				if a.input.CIRunID != "cir_build" || a.input.StartedAt != nil || a.inv.SourceKind != "web" || a.inv.SourceID != "" || a.inv.CorrelationID == "" {
					t.Fatal("wrong invocation")
				}
				for _, s := range []string{"source_id", "authorization", "receipt", "record-1"} {
					if strings.Contains(w.Body.String(), s) {
						t.Fatal("leaked", s)
					}
				}
			} else if tt.err == nil && a.calls != 0 {
				t.Fatal("invalid request reached application")
			}
		})
	}
}
func TestStagingTargetsCursorAndProjection(t *testing.T) {
	scope := discoveryCursor{Version: 1, WorkspaceID: "wrk_main", Kind: "staging-environment", CIRunID: "cir_one"}
	page := goldenpath.StagingTargetPage{Items: []goldenpath.StagingTarget{{Ref: entityref.Ref{Type: "environment", ID: "env_stage"}, Name: "Staging", Key: "STAGE"}}, NextID: "env_stage"}
	dto, err := publicStagingTargets(page, goldenpath.DiscoveryPageInput{Limit: 1}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = parseDiscoveryQuery("after="+*dto.NextCursor, scope); err != nil {
		t.Fatal(err)
	}
	scope.CIRunID = "cir_two"
	if _, err = parseDiscoveryQuery("after="+*dto.NextCursor, scope); err == nil {
		t.Fatal("cross CI cursor")
	}
	body, _ := json.Marshal(dto)
	if strings.Contains(string(body), "authorization") {
		t.Fatal("authorization leaked")
	}
	page.Items = append(page.Items, page.Items[0])
	if _, err = publicStagingTargets(page, goldenpath.DiscoveryPageInput{Limit: 2}, scope); err == nil {
		t.Fatal("duplicate target accepted")
	}
}
