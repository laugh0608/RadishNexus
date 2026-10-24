package httptransport

import (
	"bytes"
	"context"
	"errors"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"io"
	"mime"
	"net/http"
)

type ContextApplication interface {
	Export(context.Context, authz.Principal, string, string) (cp.Bundle, error)
	Prepare(context.Context, authz.Principal, string, cp.Bundle, string) (cp.Plan, error)
	Apply(context.Context, authz.Principal, string, cp.Bundle, cp.Plan, string) (cp.Result, error)
	Result(context.Context, authz.Principal, string, string) (cp.Result, error)
	Source(context.Context, authz.Principal, entityref.Ref) (goldenpath.ContextSource, error)
}

const contextBase = "/api/v1/workspaces/{workspace_id}"

var contextRoutes = map[string]string{
	contextBase + "/projects/{project_id}/context-export":     "POST",
	contextBase + "/context-imports/prepare":                  "POST",
	contextBase + "/context-imports/apply":                    "POST",
	contextBase + "/context-imports/{operation_id}":           "GET",
	contextBase + "/context-source/{entity_type}/{entity_id}": "GET",
}

func RegisterContextRoutes(mux *http.ServeMux, h http.Handler) {
	for p := range contextRoutes {
		mux.Handle(p, h)
	}
}
func NewContextHandler(sessions MessagingSessionService, app ContextApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	for path, method := range contextRoutes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != method {
				w.Header().Set("Allow", method)
				writeIdentityError(w, r, ErrMethodNotAllowed)
				return
			}
			p, e := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), method == "POST", sessions, session, proxy)
			if e != nil {
				writeIdentityError(w, r, e)
				return
			}
			if p.WorkspaceID != r.PathValue("workspace_id") {
				writeIdentityError(w, r, errors.New("context scope mismatch"))
				return
			}
			if r.URL.RawQuery != "" {
				writeIdentityError(w, r, authz.ErrInvalid)
				return
			}
			token, e := session.SessionToken(r)
			if e != nil {
				writeIdentityError(w, r, e)
				return
			}
			var result any
			switch path {
			case contextBase + "/projects/{project_id}/context-export":
				var empty struct{}
				e = decodeContextBody(w, r, &empty, 1024)
				if e == nil {
					result, e = app.Export(r.Context(), p, token, r.PathValue("project_id"))
				}
			case contextBase + "/context-imports/prepare":
				var in struct {
					Bundle      cp.Bundle `json:"bundle"`
					OperationID string    `json:"operation_id"`
				}
				e = decodeContextBody(w, r, &in, 96<<20)
				if e == nil {
					result, e = app.Prepare(r.Context(), p, token, in.Bundle, in.OperationID)
				}
			case contextBase + "/context-imports/apply":
				var in struct {
					Bundle    cp.Bundle `json:"bundle"`
					Plan      cp.Plan   `json:"plan"`
					Confirmed string    `json:"confirmed_digest"`
				}
				e = decodeContextBody(w, r, &in, 96<<20)
				if e == nil {
					result, e = app.Apply(r.Context(), p, token, in.Bundle, in.Plan, in.Confirmed)
				}
			case contextBase + "/context-imports/{operation_id}":
				result, e = app.Result(r.Context(), p, token, r.PathValue("operation_id"))
			default:
				result, e = app.Source(r.Context(), p, entityref.Ref{Type: r.PathValue("entity_type"), ID: r.PathValue("entity_id")})
			}
			if e != nil {
				switch {
				case errors.Is(e, cp.ErrIncomplete):
					e = authz.ErrNotFound
				case errors.Is(e, cp.ErrInvalid):
					e = authz.ErrInvalid
				}
				writeIdentityError(w, r, e)
				return
			}
			writeIdentityJSON(w, r, http.StatusOK, map[string]any{"data": result})
		})
	}
	return privateNoStore(mux)
}
func decodeContextBody(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		return ErrUnsupportedMediaType
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			return ErrPayloadTooLarge
		}
		return authz.ErrInvalid
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		return authz.ErrInvalid
	}
	if e = cp.Decode(raw, target); e != nil {
		return authz.ErrInvalid
	}
	return nil
}
