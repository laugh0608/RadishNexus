package httptransport

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebAppHandlerServesOnlyKnownShellRoutesAndImmutableAssets(t *testing.T) {
	t.Parallel()
	root := testWebRoot(t)
	handler, err := NewWebAppHandler(root)
	if err != nil {
		t.Fatalf("NewWebAppHandler() error = %v", err)
	}

	tests := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantCache   string
		wantBody    string
		wantContent string
	}{
		{name: "root shell", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantCache: "no-cache", wantBody: "authenticated shell", wantContent: "text/html; charset=utf-8"},
		{name: "Deployment shell", method: http.MethodGet, path: "/workspaces/wrk_main/deployments/dpl_release", wantStatus: http.StatusOK, wantCache: "no-cache", wantBody: "authenticated shell", wantContent: "text/html; charset=utf-8"},
		{name: "Channel shell", method: http.MethodGet, path: "/workspaces/wrk_main/channels/chn_project", wantStatus: http.StatusOK, wantCache: "no-cache", wantBody: "authenticated shell", wantContent: "text/html; charset=utf-8"},
		{name: "Thread shell", method: http.MethodGet, path: "/workspaces/wrk_main/threads/thr_discussion", wantStatus: http.StatusOK, wantCache: "no-cache", wantBody: "authenticated shell", wantContent: "text/html; charset=utf-8"},
		{name: "Decision shell", method: http.MethodGet, path: "/workspaces/wrk_main/decisions/dec_choice", wantStatus: http.StatusOK, wantCache: "no-cache", wantBody: "authenticated shell", wantContent: "text/html; charset=utf-8"},
		{name: "Ticket shell", method: http.MethodHead, path: "/workspaces/wrk_main/tickets/tkt_work", wantStatus: http.StatusOK, wantCache: "no-cache", wantContent: "text/html; charset=utf-8"},
		{name: "prototype shell", method: http.MethodHead, path: "/prototype/nexus-view", wantStatus: http.StatusOK, wantCache: "no-cache", wantContent: "text/html; charset=utf-8"},
		{name: "immutable asset", method: http.MethodGet, path: "/assets/index-abc123.js", wantStatus: http.StatusOK, wantCache: "public, max-age=31536000, immutable", wantBody: "console.log", wantContent: "text/javascript; charset=utf-8"},
		{name: "unknown route", method: http.MethodGet, path: "/unknown", wantStatus: http.StatusNotFound, wantCache: "no-store"},
		{name: "unknown nested Channel route", method: http.MethodGet, path: "/workspaces/wrk_main/channels/chn_project/settings", wantStatus: http.StatusNotFound, wantCache: "no-store"},
		{name: "unknown nested Decision route", method: http.MethodGet, path: "/workspaces/wrk_main/decisions/dec_choice/settings", wantStatus: http.StatusNotFound, wantCache: "no-store"},
		{name: "missing asset", method: http.MethodGet, path: "/assets/missing.js", wantStatus: http.StatusNotFound, wantCache: "no-store"},
		{name: "wrong method", method: http.MethodPost, path: "/", wantStatus: http.StatusMethodNotAllowed, wantCache: "no-store"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "https://nexus.example.test"+test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || response.Header().Get("Cache-Control") != test.wantCache {
				t.Fatalf("response = status %d, headers %#v", response.Code, response.Header())
			}
			if test.wantBody != "" && !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("body = %q, want %q", response.Body.String(), test.wantBody)
			}
			if test.method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatalf("HEAD body = %q", response.Body.String())
			}
			if test.wantContent != "" && response.Header().Get("Content-Type") != test.wantContent {
				t.Fatalf("Content-Type = %q, want %q", response.Header().Get("Content-Type"), test.wantContent)
			}
			if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("security headers = %#v", response.Header())
			}
		})
	}
}

func TestWebAppHandlerRejectsAssetSymlinkEscapingBuildRoot(t *testing.T) {
	t.Parallel()
	root := testWebRoot(t)
	outside := filepath.Join(t.TempDir(), "outside.js")
	if err := os.WriteFile(outside, []byte("must not be served"), 0o600); err != nil {
		t.Fatalf("write outside asset: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "assets", "escaped.js")); err != nil {
		t.Fatalf("create escaped asset symlink: %v", err)
	}
	handler, err := NewWebAppHandler(root)
	if err != nil {
		t.Fatalf("NewWebAppHandler() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "https://nexus.example.test/assets/escaped.js", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Body.Len() != 0 ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("escaped asset response = status %d, headers %#v, body %q", response.Code, response.Header(), response.Body.String())
	}
}

func TestNewWebAppHandlerRejectsUnsafeOrIncompleteRoots(t *testing.T) {
	t.Parallel()
	emptyRoot := t.TempDir()
	fileRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(fileRoot, []byte("file"), 0o600); err != nil {
		t.Fatalf("write file root: %v", err)
	}
	for _, root := range []string{"", "relative/web", filepath.Join(t.TempDir(), "missing"), emptyRoot, fileRoot} {
		if _, err := NewWebAppHandler(root); err == nil {
			t.Fatalf("NewWebAppHandler(%q) error = nil", root)
		}
	}
}

func testWebRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o700); err != nil {
		t.Fatalf("create assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html><title>authenticated shell</title>"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "index-abc123.js"), []byte("console.log('asset')"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	return root
}
