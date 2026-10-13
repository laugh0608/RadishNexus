package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type checkoutReader struct {
	calls int
	scope string
	input goldenpath.DiscoveryPageInput
	err   error
}

func (s *checkoutReader) ListRepositoryCIRuns(_ context.Context, p authz.Principal, r string, in goldenpath.DiscoveryPageInput) (goldenpath.RepositoryCIRunPage, error) {
	s.calls++
	s.scope = p.WorkspaceID + ":" + r
	s.input = in
	return goldenpath.RepositoryCIRunPage{Items: []goldenpath.RepositoryCIRunItem{{Ref: entityref.Ref{Type: "ci-run", ID: "cir_one"}, Status: "failed", CompletedAt: time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC), Revision: goldenpath.GitRevision{Algorithm: "sha1", OID: strings.Repeat("a", 40)}}}, NextID: "cir_one"}, s.err
}
func TestRepositoryCIRunsSessionCursorAndSafeProjection(t *testing.T) {
	session, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	reader := &checkoutReader{}
	h := WithRequestID(NewRepositoryCIRunsHandler(validMessagingSessions(), reader, session, proxy))
	path := "/api/v1/workspaces/wrk_main/repositories/rep_source/ci-runs"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest("GET", path+"?limit=1", ""))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" || reader.scope != "wrk_main:rep_source" {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Next string `json:"next_cursor"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data.Next == "" {
		t.Fatal(e)
	}
	for _, leak := range []string{"source_id", "binding", "receipt", "total", "external_run"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Fatal(leak)
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest("GET", path+"?limit=1&after="+out.Data.Next, ""))
	if w.Code != 500 || reader.input.AfterID != "cir_one" {
		t.Fatal("repeated row must be rejected", w.Code)
	}
	calls := reader.calls
	for _, url := range []string{strings.Replace(path, "rep_source", "rep_other", 1) + "?after=" + out.Data.Next, path + "?limit=1&extra=yes", path + "?after=malformed"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, messagingRequest("GET", url, ""))
		if w.Code != 400 || reader.calls != calls {
			t.Fatal("invalid cursor reached core", w.Code)
		}
	}
	reader.err = authz.ErrNotFound
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest("GET", path, ""))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
