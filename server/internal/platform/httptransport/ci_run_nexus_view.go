package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const ciRunNexusViewPattern = "/api/v1/workspaces/{workspace_id}/ci-runs/{ci_run_id}/nexus-view"

func RegisterCIRunRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle(ciRunNexusViewPattern, handler)
}

func NewCIRunNexusViewHandler(sessions MessagingSessionService, views NexusViewReader, session BrowserSessionPolicy, proxy TrustedProxyPolicy) http.Handler {
	transport := &CollaborationHandler{sessions: sessions, session: session, proxy: proxy}
	mux := http.NewServeMux()
	mux.HandleFunc(ciRunNexusViewPattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			transport.writeError(w, r, ErrMethodNotAllowed)
			return
		}
		principal, err := authenticateWorkspaceRequest(r, r.PathValue("workspace_id"), false, sessions, session, proxy)
		if err != nil {
			transport.writeError(w, r, err)
			return
		}
		target := entityref.Ref{Type: "ci-run", ID: r.PathValue("ci_run_id")}
		if err := entityref.M0Registry().Validate(target); err != nil {
			transport.writeError(w, r, authz.ErrInvalid)
			return
		}
		view, err := views.GetNexusView(r.Context(), principal, target)
		if err != nil {
			transport.writeError(w, r, err)
			return
		}
		dto, err := ciRunNexusViewFromApplication(target, view)
		if err != nil {
			transport.writeError(w, r, err)
			return
		}
		transport.writeJSON(w, r, http.StatusOK, struct {
			Data ciRunNexusViewDTO `json:"data"`
		}{dto})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { transport.writeError(w, r, authz.ErrNotFound) })
	return privateNoStore(mux)
}

type ciRunCurrentDTO struct {
	Ref         entityRefDTO     `json:"ref"`
	Status      string           `json:"status"`
	StartedAt   *string          `json:"started_at"`
	CompletedAt string           `json:"completed_at"`
	RecordedAt  string           `json:"recorded_at"`
	UpdatedAt   string           `json:"updated_at"`
	Component   visibleEntityDTO `json:"component"`
}
type ciRunActorDTO struct {
	Kind string `json:"kind"`
}
type ciRunTimelineDTO struct {
	ID           string                 `json:"id"`
	ActivityType string                 `json:"activity_type"`
	Actor        ciRunActorDTO          `json:"actor"`
	OccurredAt   string                 `json:"occurred_at"`
	Status       string                 `json:"status"`
	Subjects     []deploymentSubjectDTO `json:"subjects"`
}
type ciRunNexusViewDTO struct {
	Current   ciRunCurrentDTO    `json:"current"`
	Relations []json.RawMessage  `json:"relations"`
	Timeline  []ciRunTimelineDTO `json:"timeline"`
}

func ciRunNexusViewFromApplication(target entityref.Ref, view goldenpath.NexusView) (ciRunNexusViewDTO, error) {
	fail := func(reason string) (ciRunNexusViewDTO, error) {
		return ciRunNexusViewDTO{}, fmt.Errorf("invalid CI Run Nexus View projection: %s", reason)
	}
	c := view.Current
	if c.Ref != target || target.Type != "ci-run" || entityref.M0Registry().Validate(target) != nil || c.Component == nil || c.CompletedAt == nil || c.RecordedAt == nil {
		return fail("required identity or fields")
	}
	if c.Status != "succeeded" && c.Status != "failed" && c.Status != "canceled" {
		return fail("unsupported status")
	}
	if c.CompletedAt.IsZero() || c.RecordedAt.IsZero() || !c.UpdatedAt.Equal(*c.RecordedAt) || (c.StartedAt != nil && (c.StartedAt.IsZero() || c.StartedAt.After(*c.CompletedAt))) {
		return fail("inconsistent current times")
	}
	if c.Title != "" || c.GoverningProjectID != "" || c.Visibility != "" || c.CreatedBy != (goldenpath.ActorRef{}) || !c.CreatedAt.IsZero() || c.Outcome != "" || c.Rationale != "" || c.ProposerID != "" || len(c.DeciderIDs) != 0 || c.DecidedAt != nil || c.OriginChannel != nil || c.Environment != nil || c.CIRun != nil {
		return fail("unexpected current fields")
	}
	component, err := requiredVisibleEntity(*c.Component, "component")
	if err != nil {
		return fail("unreadable Component")
	}
	if len(view.Relations) != 0 || len(view.Timeline) != 1 {
		return fail("unexpected relation or timeline count")
	}
	item := view.Timeline[0]
	if item.ActivityType != "ci-run.recorded" || item.Actor != (goldenpath.ActorRef{Kind: "plugin"}) || !validScopedID(item.EventID, "evt_") || item.ProjectionVersion != goldenpath.ActivityProjectionVersion || !item.OccurredAt.Equal(*c.CompletedAt) || len(item.SafeFacts) != 1 || item.SafeFacts["status"] != c.Status || len(item.Subjects) != 1 || item.Subjects[0] != *c.Component {
		return fail("inconsistent or unsafe timeline")
	}
	var started *string
	if c.StartedAt != nil {
		s := publicTime(*c.StartedAt)
		started = &s
	}
	return ciRunNexusViewDTO{
		Current: ciRunCurrentDTO{
			Ref: publicRef(target), Status: c.Status, StartedAt: started,
			CompletedAt: publicTime(*c.CompletedAt), RecordedAt: publicTime(*c.RecordedAt),
			UpdatedAt: publicTime(c.UpdatedAt), Component: component,
		},
		Relations: []json.RawMessage{},
		Timeline: []ciRunTimelineDTO{{
			ID: item.EventID, ActivityType: item.ActivityType, Actor: ciRunActorDTO{Kind: "plugin"},
			OccurredAt: publicTime(item.OccurredAt), Status: c.Status,
			Subjects: []deploymentSubjectDTO{{Visibility: "readable", Entity: &component}},
		}},
	}, nil
}
