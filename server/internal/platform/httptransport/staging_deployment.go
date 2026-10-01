package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const stagingTargetsPattern = "/api/v1/workspaces/{workspace_id}/ci-runs/{ci_run_id}/staging-targets"
const stagingDeploymentPattern = "/api/v1/workspaces/{workspace_id}/ci-runs/{ci_run_id}/staging-deployments"

type StagingDeploymentApplication interface {
	ListStagingTargets(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.StagingTargetPage, error)
	RecordStagingDeployment(context.Context, goldenpath.Invocation, goldenpath.RecordStagingDeploymentInput) (goldenpath.Deployment, error)
}

func RegisterStagingDeploymentRoutes(mux *http.ServeMux, h http.Handler) {
	mux.Handle(stagingTargetsPattern, h)
	mux.Handle(stagingDeploymentPattern, h)
}
func NewStagingDeploymentHandler(sessions MessagingSessionService, app StagingDeploymentApplication, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	transport := &CollaborationHandler{sessions: sessions, session: session, proxy: proxy}
	mux := http.NewServeMux()
	for path, method := range map[string]string{stagingTargetsPattern: "GET", stagingDeploymentPattern: "POST"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			fail := func(err error) { transport.writeError(w, r, err) }
			if r.Method != method {
				w.Header().Set("Allow", method)
				fail(ErrMethodNotAllowed)
				return
			}
			p, err := transport.authenticate(r, r.PathValue("workspace_id"), method == "POST")
			if err != nil {
				fail(err)
				return
			}
			ciRun := r.PathValue("ci_run_id")
			if !validScopedID(ciRun, "cir_") || r.URL.RawPath != "" || r.URL.ForceQuery {
				fail(authz.ErrInvalid)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			if method == "GET" {
				scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, Kind: "staging-environment", CIRunID: ciRun}
				in, err := parseDiscoveryQuery(r.URL.RawQuery, scope)
				if err != nil {
					fail(err)
					return
				}
				page, err := app.ListStagingTargets(ctx, p, ciRun, in)
				if err != nil {
					fail(err)
					return
				}
				dto, err := publicStagingTargets(page, in, scope)
				if err != nil {
					fail(err)
					return
				}
				transport.writeJSON(w, r, 200, struct {
					Data stagingTargetsDTO `json:"data"`
				}{dto})
				return
			}
			if r.URL.RawQuery != "" {
				fail(authz.ErrInvalid)
				return
			}
			in, err := decodeStagingDeployment(w, r)
			if err != nil {
				fail(err)
				return
			}
			in.CIRunID = ciRun
			result, err := app.RecordStagingDeployment(ctx, webInvocation(p, r), in)
			if err != nil {
				fail(err)
				return
			}
			if !validScopedID(result.ID, "dpl_") {
				fail(errors.New("invalid Deployment result identity"))
				return
			}
			status := http.StatusCreated
			if result.Duplicate {
				status = http.StatusOK
			}
			transport.writeJSON(w, r, status, struct {
				Data struct {
					Deployment entityRefDTO `json:"deployment"`
					Duplicate  bool         `json:"duplicate"`
				} `json:"data"`
			}{Data: struct {
				Deployment entityRefDTO `json:"deployment"`
				Duplicate  bool         `json:"duplicate"`
			}{entityRefDTO{Type: "deployment", ID: result.ID}, result.Duplicate}})
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { transport.writeError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}

var stagingTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?Z$`)

func decodeStagingDeployment(w http.ResponseWriter, r *http.Request) (in goldenpath.RecordStagingDeploymentInput, err error) {
	fields := []string{"client_operation_id", "environment_id", "status", "started_at", "completed_at", "confirmed"}
	raw, err := decodeStrictObject(w, r, fields, 8<<10)
	if err != nil {
		return in, err
	}
	for key, dest := range map[string]*string{"client_operation_id": &in.ClientOperationID, "environment_id": &in.EnvironmentID, "status": &in.Status} {
		if string(raw[key]) == "null" || !utf8.Valid(raw[key]) || json.Unmarshal(raw[key], dest) != nil {
			return in, authz.ErrInvalid
		}
	}
	if json.Unmarshal(raw["confirmed"], &in.Confirmed) != nil || !in.Confirmed {
		return in, authz.ErrInvalid
	}
	for _, key := range []string{"started_at", "completed_at"} {
		if key == "started_at" && string(raw[key]) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw[key], &value) != nil || !stagingTimePattern.MatchString(value) {
			return in, authz.ErrInvalid
		}
		parsed, e := time.Parse(time.RFC3339Nano, value)
		if e != nil {
			return in, authz.ErrInvalid
		}
		if key == "started_at" {
			in.StartedAt = &parsed
		} else {
			in.CompletedAt = parsed
		}
	}
	return in, nil
}

type stagingTargetDTO struct {
	Ref            entityRefDTO `json:"ref"`
	Name           string       `json:"name"`
	Key            string       `json:"key"`
	Classification string       `json:"classification"`
}
type stagingTargetsDTO struct {
	Items      []stagingTargetDTO `json:"items"`
	NextCursor *string            `json:"next_cursor"`
}

func publicStagingTargets(page goldenpath.StagingTargetPage, in goldenpath.DiscoveryPageInput, scope discoveryCursor) (out stagingTargetsDTO, err error) {
	out.Items = []stagingTargetDTO{}
	invalid := errors.New("invalid staging target projection")
	if len(page.Items) > in.Limit {
		return out, invalid
	}
	previous := in.AfterID
	for _, v := range page.Items {
		if v.Ref.Type != "environment" || !validScopedID(v.Ref.ID, "env_") || v.Ref.ID <= previous || strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Key) == "" || !utf8.ValidString(v.Name) || !utf8.ValidString(v.Key) {
			return out, invalid
		}
		previous = v.Ref.ID
		out.Items = append(out.Items, stagingTargetDTO{publicRef(v.Ref), v.Name, v.Key, "staging"})
	}
	if page.NextID != "" {
		if len(page.Items) != in.Limit || previous != page.NextID {
			return out, invalid
		}
		scope.AfterID = page.NextID
		body, e := json.Marshal(scope)
		if e != nil {
			return out, e
		}
		cursor := base64.RawURLEncoding.EncodeToString(body)
		out.NextCursor = &cursor
	}
	return out, nil
}
