package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type ciCheckoutDTO struct {
	Visibility string                  `json:"visibility"`
	State      string                  `json:"state,omitempty"`
	Reason     string                  `json:"reason,omitempty"`
	Repository *visibleEntityDTO       `json:"repository,omitempty"`
	Revision   *goldenpath.GitRevision `json:"revision,omitempty"`
}

func publicCICheckout(c *goldenpath.CICheckoutProjection) (ciCheckoutDTO, error) {
	invalid := errors.New("invalid CI checkout projection")
	if c == nil {
		return ciCheckoutDTO{}, invalid
	}
	if c.Visibility == goldenpath.ProjectionRestricted {
		if c.State != "" || c.Reason != "" || c.Repository != nil || c.Revision != nil {
			return ciCheckoutDTO{}, invalid
		}
		return ciCheckoutDTO{Visibility: "restricted"}, nil
	}
	if c.Visibility != goldenpath.ProjectionVisible {
		return ciCheckoutDTO{}, invalid
	}
	d := ciCheckoutDTO{Visibility: "readable", State: c.State, Reason: c.Reason, Revision: c.Revision}
	if c.State == "not_collected" {
		if c.Reason != "" || c.Repository != nil || c.Revision != nil {
			return ciCheckoutDTO{}, invalid
		}
		return d, nil
	}
	if c.Repository == nil {
		return ciCheckoutDTO{}, invalid
	}
	r, err := requiredVisibleEntity(*c.Repository, "repository")
	if err != nil {
		return ciCheckoutDTO{}, err
	}
	d.Repository = &r
	if c.State == "observed" && c.Reason == "" && c.Revision != nil && goldenpath.ValidGitRevision(*c.Revision) {
		return d, nil
	}
	if c.State == "unavailable" && goldenpath.ValidCheckoutReason(c.Reason) && c.Revision == nil {
		return d, nil
	}
	return ciCheckoutDTO{}, invalid
}

func ciCheckoutContext(c goldenpath.CurrentProjection, relations []goldenpath.RelationProjection, item goldenpath.TimelineItem) ([]collaborationRelationDTO, []deploymentSubjectDTO, error) {
	invalid := errors.New("inconsistent CI checkout context")
	d, err := publicCICheckout(c.Checkout)
	if err != nil {
		return nil, nil, err
	}
	wantSubjects := []goldenpath.SubjectProjection{*c.Component}
	wantFacts := map[string]string{"status": c.Status}
	result := []collaborationRelationDTO{}
	if d.Visibility == "restricted" {
		wantSubjects = append(wantSubjects, goldenpath.SubjectProjection{State: goldenpath.ProjectionRestricted})
		if len(relations) != 1 || relations[0] != (goldenpath.RelationProjection{State: goldenpath.ProjectionRestricted}) {
			return nil, nil, invalid
		}
		result = append(result, collaborationRelationDTO{Visibility: "restricted"})
	} else if d.State != "not_collected" {
		wantSubjects = append(wantSubjects, *c.Checkout.Repository)
		wantFacts["checkout_state"] = d.State
		if d.Reason != "" {
			wantFacts["checkout_reason"] = d.Reason
		}
		if d.State == "observed" {
			if len(relations) != 1 || relations[0] != (goldenpath.RelationProjection{State: goldenpath.ProjectionVisible, Direction: "outgoing", RelationType: "built-from", Target: c.Checkout.Repository.Ref, Title: c.Checkout.Repository.Title}) {
				return nil, nil, invalid
			}
			result = append(result, collaborationRelationDTO{Visibility: "readable", Direction: "outgoing", RelationType: "built-from", Target: d.Repository})
		} else if len(relations) != 0 {
			return nil, nil, invalid
		}
	} else if len(relations) != 0 {
		return nil, nil, invalid
	}
	if len(item.Subjects) != len(wantSubjects) || len(item.SafeFacts) != len(wantFacts) {
		return nil, nil, invalid
	}
	for k, v := range wantFacts {
		if item.SafeFacts[k] != v {
			return nil, nil, invalid
		}
	}
	subjects := []deploymentSubjectDTO{}
	for i, s := range wantSubjects {
		if item.Subjects[i] != s {
			return nil, nil, invalid
		}
		if s.State == goldenpath.ProjectionRestricted {
			subjects = append(subjects, deploymentSubjectDTO{Visibility: "restricted"})
			continue
		}
		v, e := requiredVisibleEntity(s, "")
		if e != nil {
			return nil, nil, e
		}
		subjects = append(subjects, deploymentSubjectDTO{Visibility: "readable", Entity: &v})
	}
	return result, subjects, nil
}

const repositoryCIRunsPattern = "/api/v1/workspaces/{workspace_id}/repositories/{repository_id}/ci-runs"

type RepositoryCIRunReader interface {
	ListRepositoryCIRuns(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.RepositoryCIRunPage, error)
}

func RegisterRepositoryCIRunRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle(repositoryCIRunsPattern, handler)
}
func NewRepositoryCIRunsHandler(sessions MessagingSessionService, reader RepositoryCIRunReader, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(repositoryCIRunsPattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeIdentityError(w, r, ErrMethodNotAllowed)
			return
		}
		p, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), false, sessions, session, proxy)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		repository := r.PathValue("repository_id")
		if !validScopedID(repository, "rep_") {
			writeIdentityError(w, r, authz.ErrInvalid)
			return
		}
		scope := discoveryCursor{Version: 1, WorkspaceID: p.WorkspaceID, Kind: "repository-ci-runs", ProjectID: repository}
		in, err := parseDiscoveryQuery(r.URL.RawQuery, scope)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		page, err := reader.ListRepositoryCIRuns(r.Context(), p, repository, in)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		dto, err := publicRepositoryCIRuns(page, in, scope)
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		writeIdentityJSON(w, r, 200, map[string]any{"data": dto})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeIdentityError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}
func publicRepositoryCIRuns(page goldenpath.RepositoryCIRunPage, in goldenpath.DiscoveryPageInput, scope discoveryCursor) (any, error) {
	invalid := errors.New("invalid Repository CI Run page")
	if len(page.Items) > in.Limit {
		return nil, invalid
	}
	items := []any{}
	last := in.AfterID
	for _, item := range page.Items {
		if item.Ref.Type != "ci-run" || entityref.M0Registry().Validate(item.Ref) != nil || item.Ref.ID <= last || (item.Status != "succeeded" && item.Status != "failed" && item.Status != "canceled") || item.CompletedAt.IsZero() || !goldenpath.ValidGitRevision(item.Revision) {
			return nil, invalid
		}
		last = item.Ref.ID
		items = append(items, map[string]any{"ref": publicRef(item.Ref), "status": item.Status, "completed_at": publicTime(item.CompletedAt), "revision": item.Revision})
	}
	var next *string
	if page.NextID != "" {
		if len(items) != in.Limit || page.NextID != last {
			return nil, invalid
		}
		scope.AfterID = last
		body, err := json.Marshal(scope)
		if err != nil {
			return nil, err
		}
		s := base64.RawURLEncoding.EncodeToString(body)
		next = &s
	}
	return map[string]any{"items": items, "next_cursor": next}, nil
}
