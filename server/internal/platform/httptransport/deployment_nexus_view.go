package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const deploymentNexusViewPattern = "/api/v1/workspaces/{workspace_id}/deployments/{deployment_id}/nexus-view"

type WorkspaceSessionResolver interface {
	ResolveWorkspace(context.Context, string, string) (authn.VerifiedUser, error)
}

type NexusViewReader interface {
	GetNexusView(context.Context, authz.Principal, entityref.Ref) (goldenpath.NexusView, error)
}

type DeploymentNexusViewHandler struct {
	sessions WorkspaceSessionResolver
	views    NexusViewReader
	session  BrowserSessionPolicy
	proxy    TrustedProxyPolicy
}

type deploymentNexusViewResponse struct {
	Data deploymentNexusViewDTO `json:"data"`
}

type deploymentNexusViewDTO struct {
	Current   deploymentCurrentDTO    `json:"current"`
	Relations []deploymentRelationDTO `json:"relations"`
	Timeline  []deploymentTimelineDTO `json:"timeline"`
}

type deploymentCurrentDTO struct {
	Ref         entityRefDTO     `json:"ref"`
	Status      string           `json:"status"`
	StartedAt   *string          `json:"started_at"`
	CompletedAt string           `json:"completed_at"`
	RecordedAt  string           `json:"recorded_at"`
	Environment visibleEntityDTO `json:"environment"`
	CIRun       visibleEntityDTO `json:"ci_run"`
}

type entityRefDTO struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type visibleEntityDTO struct {
	Ref   entityRefDTO `json:"ref"`
	Title string       `json:"title"`
}

type deploymentRelationDTO struct {
	Visibility   string            `json:"visibility"`
	RelationType string            `json:"relation_type,omitempty"`
	Target       *visibleEntityDTO `json:"target,omitempty"`
}

type deploymentTimelineDTO struct {
	ID           string                 `json:"id"`
	ActivityType string                 `json:"activity_type"`
	Actor        deploymentActorDTO     `json:"actor"`
	OccurredAt   string                 `json:"occurred_at"`
	Status       string                 `json:"status"`
	Subjects     []deploymentSubjectDTO `json:"subjects"`
}

type deploymentActorDTO struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type deploymentSubjectDTO struct {
	Visibility string            `json:"visibility"`
	Entity     *visibleEntityDTO `json:"entity,omitempty"`
}

func NewDeploymentNexusViewHandler(
	sessions WorkspaceSessionResolver,
	views NexusViewReader,
	session BrowserSessionPolicy,
	proxy TrustedProxyPolicy,
) http.Handler {
	handler := &DeploymentNexusViewHandler{
		sessions: sessions,
		views:    views,
		session:  session,
		proxy:    proxy,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+deploymentNexusViewPattern, handler.get)
	mux.HandleFunc(deploymentNexusViewPattern, handler.methodNotAllowed)
	mux.HandleFunc("/api/v1/workspaces/", handler.notFound)
	mux.HandleFunc("/api/v1/workspaces", handler.notFound)
	return privateNoStore(mux)
}

func (handler *DeploymentNexusViewHandler) get(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		handler.methodNotAllowed(response, request)
		return
	}
	if _, err := handler.proxy.ClientIP(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if err := handler.session.ValidateHost(request); err != nil {
		handler.writeError(response, request, err)
		return
	}
	token, err := handler.session.SessionToken(request)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}

	workspaceID := request.PathValue("workspace_id")
	deploymentID := request.PathValue("deployment_id")
	target := entityref.Ref{Type: "deployment", ID: deploymentID}
	if !validScopedID(workspaceID, "wrk_") {
		handler.writeError(response, request, fmt.Errorf("%w: invalid Workspace ID", authz.ErrInvalid))
		return
	}
	if err := entityref.M0Registry().Validate(target); err != nil {
		handler.writeError(response, request, fmt.Errorf("%w: invalid Deployment ID", authz.ErrInvalid))
		return
	}

	verified, err := handler.sessions.ResolveWorkspace(request.Context(), token, workspaceID)
	if err != nil {
		if errors.Is(err, authz.ErrForbidden) {
			err = authz.ErrNotFound
		}
		handler.writeError(response, request, err)
		return
	}
	principal, err := authn.UserPrincipal(verified)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	view, err := handler.views.GetNexusView(request.Context(), principal, target)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	body, err := marshalDeploymentNexusView(target, view)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(append(body, '\n')); err != nil {
		log.Printf("write Deployment Nexus View request_id=%s: %v", RequestID(request.Context()), err)
	}
}

func (handler *DeploymentNexusViewHandler) methodNotAllowed(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Allow", http.MethodGet)
	handler.writeError(response, request, ErrMethodNotAllowed)
}

func (handler *DeploymentNexusViewHandler) notFound(response http.ResponseWriter, request *http.Request) {
	handler.writeError(response, request, authz.ErrNotFound)
}

func (handler *DeploymentNexusViewHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	if MapApplicationError(err).StatusCode == http.StatusInternalServerError {
		log.Printf("public Deployment Nexus View failed request_id=%s: %v", RequestID(request.Context()), err)
	}
	if writeErr := WriteError(response, RequestID(request.Context()), err); writeErr != nil {
		log.Printf("write public Deployment Nexus View error: %v", writeErr)
	}
}

func privateNoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "private, no-store")
		response.Header().Set("Vary", "Cookie")
		next.ServeHTTP(response, request)
	})
}

func validScopedID(value string, prefix string) bool {
	if len(value) <= len(prefix) || len(value) > 128 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, char := range value {
		if char > 0x7f || char <= 0x20 || char == '/' || char == '?' || char == '#' {
			return false
		}
	}
	return true
}

func marshalDeploymentNexusView(target entityref.Ref, view goldenpath.NexusView) ([]byte, error) {
	dto, err := deploymentNexusViewFromApplication(target, view)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(deploymentNexusViewResponse{Data: dto})
	if err != nil {
		return nil, fmt.Errorf("marshal public Deployment Nexus View: %w", err)
	}
	return body, nil
}

func deploymentNexusViewFromApplication(
	target entityref.Ref,
	view goldenpath.NexusView,
) (deploymentNexusViewDTO, error) {
	current := view.Current
	if current.Ref != target || current.Status == "" || current.CompletedAt == nil ||
		current.RecordedAt == nil || current.Environment == nil || current.CIRun == nil {
		return deploymentNexusViewDTO{}, fmt.Errorf("invalid Deployment Nexus View Current projection")
	}
	if current.Title != "" || current.GoverningProjectID != "" || current.Component != nil ||
		!current.UpdatedAt.Equal(*current.RecordedAt) {
		return deploymentNexusViewDTO{}, fmt.Errorf("unexpected fields in Deployment Current projection")
	}
	if current.Status != "succeeded" && current.Status != "failed" && current.Status != "canceled" {
		return deploymentNexusViewDTO{}, fmt.Errorf("invalid public Deployment status %q", current.Status)
	}
	if len(view.Relations) != 1 || len(view.Timeline) != 1 {
		return deploymentNexusViewDTO{}, fmt.Errorf("Deployment Nexus View requires one relation and one Timeline item")
	}
	environment, err := requiredVisibleEntity(*current.Environment, "environment")
	if err != nil {
		return deploymentNexusViewDTO{}, err
	}
	ciRun, err := requiredVisibleEntity(*current.CIRun, "ci-run")
	if err != nil {
		return deploymentNexusViewDTO{}, err
	}
	var startedAt *string
	if current.StartedAt != nil {
		formatted := publicTime(*current.StartedAt)
		startedAt = &formatted
	}
	dto := deploymentNexusViewDTO{
		Current: deploymentCurrentDTO{
			Ref:         publicRef(target),
			Status:      current.Status,
			StartedAt:   startedAt,
			CompletedAt: publicTime(*current.CompletedAt),
			RecordedAt:  publicTime(*current.RecordedAt),
			Environment: environment,
			CIRun:       ciRun,
		},
		Relations: make([]deploymentRelationDTO, 0, len(view.Relations)),
		Timeline:  make([]deploymentTimelineDTO, 0, len(view.Timeline)),
	}
	for _, relation := range view.Relations {
		switch relation.State {
		case goldenpath.ProjectionRestricted:
			dto.Relations = append(dto.Relations, deploymentRelationDTO{Visibility: "restricted"})
		case goldenpath.ProjectionVisible:
			if relation.RelationType != "deploys" {
				return deploymentNexusViewDTO{}, fmt.Errorf("unexpected Deployment relation type %q", relation.RelationType)
			}
			targetEntity, err := requiredVisibleEntity(goldenpath.SubjectProjection{
				State: relation.State,
				Ref:   relation.Target,
				Title: relation.Title,
			}, "ci-run")
			if err != nil {
				return deploymentNexusViewDTO{}, err
			}
			if relation.Target != current.CIRun.Ref || relation.Title != current.CIRun.Title {
				return deploymentNexusViewDTO{}, fmt.Errorf("Deployment relation does not match Current CI Run")
			}
			dto.Relations = append(dto.Relations, deploymentRelationDTO{
				Visibility:   "readable",
				RelationType: relation.RelationType,
				Target:       &targetEntity,
			})
		default:
			return deploymentNexusViewDTO{}, fmt.Errorf("unexpected Deployment relation projection state %q", relation.State)
		}
	}
	for _, item := range view.Timeline {
		if item.ActivityType != "deployment.recorded" || item.Actor.Kind != "user" ||
			!validScopedID(item.Actor.ID, "usr_") || item.ProjectionVersion != goldenpath.ActivityProjectionVersion ||
			item.OccurredAt.IsZero() || len(item.SafeFacts) != 1 || item.SafeFacts["status"] != current.Status {
			return deploymentNexusViewDTO{}, fmt.Errorf("invalid Deployment Timeline projection")
		}
		if !validScopedID(item.EventID, "evt_") {
			return deploymentNexusViewDTO{}, fmt.Errorf("invalid Deployment Timeline event ID")
		}
		if len(item.Subjects) != 2 || item.Subjects[0].Ref != current.Environment.Ref ||
			item.Subjects[1].Ref != current.CIRun.Ref {
			return deploymentNexusViewDTO{}, fmt.Errorf("Deployment Timeline subjects do not match Current")
		}
		timeline := deploymentTimelineDTO{
			ID:           item.EventID,
			ActivityType: item.ActivityType,
			Actor:        deploymentActorDTO{Kind: item.Actor.Kind, ID: item.Actor.ID},
			OccurredAt:   publicTime(item.OccurredAt),
			Status:       current.Status,
			Subjects:     make([]deploymentSubjectDTO, 0, len(item.Subjects)),
		}
		expectedSubjects := []goldenpath.SubjectProjection{*current.Environment, *current.CIRun}
		for index, subject := range item.Subjects {
			switch subject.State {
			case goldenpath.ProjectionRestricted:
				timeline.Subjects = append(timeline.Subjects, deploymentSubjectDTO{Visibility: "restricted"})
			case goldenpath.ProjectionVisible:
				entity, err := requiredVisibleEntity(subject, "")
				if err != nil {
					return deploymentNexusViewDTO{}, err
				}
				if subject.Title != expectedSubjects[index].Title {
					return deploymentNexusViewDTO{}, fmt.Errorf("Deployment Timeline subject does not match Current")
				}
				timeline.Subjects = append(timeline.Subjects, deploymentSubjectDTO{
					Visibility: "readable",
					Entity:     &entity,
				})
			default:
				return deploymentNexusViewDTO{}, fmt.Errorf("unexpected Deployment subject projection state %q", subject.State)
			}
		}
		dto.Timeline = append(dto.Timeline, timeline)
	}
	return dto, nil
}

func requiredVisibleEntity(subject goldenpath.SubjectProjection, expectedType string) (visibleEntityDTO, error) {
	if subject.State != goldenpath.ProjectionVisible || subject.Title == "" {
		return visibleEntityDTO{}, fmt.Errorf("required readable Deployment subject is unavailable")
	}
	if err := entityref.M0Registry().Validate(subject.Ref); err != nil {
		return visibleEntityDTO{}, fmt.Errorf("invalid readable Deployment subject: %w", err)
	}
	if expectedType != "" && subject.Ref.Type != expectedType {
		return visibleEntityDTO{}, fmt.Errorf("expected %s subject, got %s", expectedType, subject.Ref.Type)
	}
	return visibleEntityDTO{Ref: publicRef(subject.Ref), Title: subject.Title}, nil
}

func publicRef(ref entityref.Ref) entityRefDTO {
	return entityRefDTO{Type: ref.Type, ID: ref.ID}
}

func publicTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
