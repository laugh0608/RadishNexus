package httptransport

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var deploymentWebPathPattern = regexp.MustCompile(`^/workspaces/[^/]+/deployments/[^/]+/?$`)

type WebAppHandler struct {
	root      string
	indexHTML []byte
}

func NewWebAppHandler(root string) (http.Handler, error) {
	if root == "" {
		return nil, errors.New("web root is required")
	}
	if !filepath.IsAbs(root) {
		return nil, errors.New("web root must be an absolute path")
	}
	cleanRoot := filepath.Clean(root)
	info, err := os.Stat(cleanRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect web root: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("web root must be a directory")
	}
	indexHTML, err := os.ReadFile(filepath.Join(cleanRoot, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("read web index: %w", err)
	}
	if len(indexHTML) == 0 {
		return nil, errors.New("web index must not be empty")
	}
	return &WebAppHandler{root: cleanRoot, indexHTML: indexHTML}, nil
}

func (handler *WebAppHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	setWebSecurityHeaders(response)
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if strings.HasPrefix(request.URL.Path, "/assets/") {
		handler.serveAsset(response, request)
		return
	}
	if request.URL.Path == "/" || request.URL.Path == "/prototype/nexus-view" ||
		deploymentWebPathPattern.MatchString(request.URL.Path) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "no-cache")
		response.WriteHeader(http.StatusOK)
		if request.Method == http.MethodGet {
			_, _ = response.Write(handler.indexHTML)
		}
		return
	}

	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNotFound)
}

func (handler *WebAppHandler) serveAsset(response http.ResponseWriter, request *http.Request) {
	relativePath := strings.TrimPrefix(request.URL.Path, "/")
	if !fs.ValidPath(relativePath) || path.Clean(relativePath) != relativePath {
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNotFound)
		return
	}
	file, err := os.OpenInRoot(handler.root, relativePath)
	if err != nil {
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNotFound)
		return
	}
	response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(response, request, path.Base(relativePath), info.ModTime(), file)
}

func setWebSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("X-Frame-Options", "DENY")
}
