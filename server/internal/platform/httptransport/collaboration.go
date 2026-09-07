package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	threadViewPattern         = "/api/v1/workspaces/{workspace_id}/threads/{thread_id}/nexus-view"
	threadDecisionsPattern    = "/api/v1/workspaces/{workspace_id}/threads/{thread_id}/decisions"
	decisionViewPattern       = "/api/v1/workspaces/{workspace_id}/decisions/{decision_id}/nexus-view"
	decisionAcceptancePattern = "/api/v1/workspaces/{workspace_id}/decisions/{decision_id}/acceptance"
	decisionTicketsPattern    = "/api/v1/workspaces/{workspace_id}/decisions/{decision_id}/tickets"
	ticketViewPattern         = "/api/v1/workspaces/{workspace_id}/tickets/{ticket_id}/nexus-view"

	maxProposeDecisionBodyBytes = 32 * 1024
	maxAcceptDecisionBodyBytes  = 64 * 1024
	maxCreateTicketBodyBytes    = 16 * 1024
)

type CollaborationApplication interface {
	GetNexusView(context.Context, authz.Principal, entityref.Ref) (goldenpath.NexusView, error)
	CreateDecisionFromThread(context.Context, goldenpath.Invocation, goldenpath.CreateDecisionInput) (goldenpath.CreateDecisionResult, error)
	AcceptDecision(context.Context, goldenpath.Invocation, goldenpath.AcceptDecisionInput) (goldenpath.AcceptDecisionResult, error)
	CreateTicketFromDecision(context.Context, goldenpath.Invocation, goldenpath.CreateTicketInput) (goldenpath.CreateTicketResult, error)
}

type CollaborationHandler struct {
	sessions      MessagingSessionService
	collaboration CollaborationApplication
	session       BrowserSessionPolicy
	proxy         TrustedProxyPolicy
}

type proposeDecisionRequest struct {
	ClientOperationID string `json:"client_operation_id"`
	Question          string `json:"question"`
}

type acceptDecisionRequest struct {
	ClientOperationID string `json:"client_operation_id"`
	Outcome           string `json:"outcome"`
	Rationale         string `json:"rationale"`
	Confirmed         bool   `json:"confirmed"`
}

type createTicketRequest struct {
	ClientOperationID string `json:"client_operation_id"`
	Title             string `json:"title"`
}

type collaborationViewResponse struct {
	Data collaborationViewDTO `json:"data"`
}

type collaborationViewDTO struct {
	Current   any                        `json:"current"`
	Relations []collaborationRelationDTO `json:"relations"`
	Timeline  []collaborationTimelineDTO `json:"timeline"`
}

type threadCurrentDTO struct {
	Ref           entityRefDTO       `json:"ref"`
	Project       entityRefDTO       `json:"project"`
	OriginChannel *visibleEntityDTO  `json:"origin_channel"`
	Title         string             `json:"title"`
	Visibility    string             `json:"visibility"`
	CreatedBy     deploymentActorDTO `json:"created_by"`
	CreatedAt     string             `json:"created_at"`
	UpdatedAt     string             `json:"updated_at"`
}

type decisionCurrentDTO struct {
	Ref       entityRefDTO         `json:"ref"`
	Project   entityRefDTO         `json:"project"`
	Question  string               `json:"question"`
	Status    string               `json:"status"`
	Outcome   *string              `json:"outcome"`
	Rationale *string              `json:"rationale"`
	Proposer  deploymentActorDTO   `json:"proposer"`
	Deciders  []deploymentActorDTO `json:"deciders"`
	DecidedAt *string              `json:"decided_at"`
	CreatedAt string               `json:"created_at"`
	UpdatedAt string               `json:"updated_at"`
}

type ticketCurrentDTO struct {
	Ref       entityRefDTO       `json:"ref"`
	Project   entityRefDTO       `json:"project"`
	Title     string             `json:"title"`
	Status    string             `json:"status"`
	CreatedBy deploymentActorDTO `json:"created_by"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
}

type collaborationRelationDTO struct {
	Visibility   string            `json:"visibility"`
	RelationType string            `json:"relation_type,omitempty"`
	Target       *visibleEntityDTO `json:"target,omitempty"`
}

type collaborationTimelineDTO struct {
	ID           string                    `json:"id"`
	ActivityType string                    `json:"activity_type"`
	Actor        deploymentActorDTO        `json:"actor"`
	OccurredAt   string                    `json:"occurred_at"`
	Status       string                    `json:"status"`
	Subjects     []collaborationSubjectDTO `json:"subjects"`
}

type collaborationSubjectDTO struct {
	Visibility string            `json:"visibility"`
	Entity     *visibleEntityDTO `json:"entity,omitempty"`
}

type decisionResponse struct {
	Data decisionCurrentDTO `json:"data"`
}

type proposedDecisionResponse struct {
	Data proposedDecisionDTO `json:"data"`
}

type proposedDecisionDTO struct {
	Decision     decisionCurrentDTO `json:"decision"`
	SourceThread entityRefDTO       `json:"source_thread"`
}

type ticketResponse struct {
	Data createdTicketDTO `json:"data"`
}

type createdTicketDTO struct {
	Ticket         ticketCurrentDTO `json:"ticket"`
	SourceDecision entityRefDTO     `json:"source_decision"`
}

func NewCollaborationHandler(
	sessions MessagingSessionService,
	collaboration CollaborationApplication,
	session BrowserSessionPolicy,
	proxy TrustedProxyPolicy,
) http.Handler {
	handler := &CollaborationHandler{
		sessions:      sessions,
		collaboration: collaboration,
		session:       session,
		proxy:         proxy,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+threadViewPattern, handler.getThread)
	mux.HandleFunc("POST "+threadDecisionsPattern, handler.proposeDecision)
	mux.HandleFunc("GET "+decisionViewPattern, handler.getDecision)
	mux.HandleFunc("POST "+decisionAcceptancePattern, handler.acceptDecision)
	mux.HandleFunc("POST "+decisionTicketsPattern, handler.createTicket)
	mux.HandleFunc("GET "+ticketViewPattern, handler.getTicket)
	for _, pattern := range []string{
		threadViewPattern,
		threadDecisionsPattern,
		decisionViewPattern,
		decisionAcceptancePattern,
		decisionTicketsPattern,
		ticketViewPattern,
	} {
		mux.HandleFunc(pattern, handler.methodNotAllowed)
	}
	mux.HandleFunc("/api/v1/workspaces/", handler.notFound)
	mux.HandleFunc("/api/v1/workspaces", handler.notFound)
	return privateNoStore(mux)
}

func (handler *CollaborationHandler) getThread(response http.ResponseWriter, request *http.Request) {
	handler.getView(response, request, "thread", request.PathValue("thread_id"))
}

func (handler *CollaborationHandler) getDecision(response http.ResponseWriter, request *http.Request) {
	handler.getView(response, request, "decision", request.PathValue("decision_id"))
}

func (handler *CollaborationHandler) getTicket(response http.ResponseWriter, request *http.Request) {
	handler.getView(response, request, "ticket", request.PathValue("ticket_id"))
}

func (handler *CollaborationHandler) getView(
	response http.ResponseWriter,
	request *http.Request,
	entityType string,
	entityID string,
) {
	workspaceID := request.PathValue("workspace_id")
	principal, err := handler.authenticate(request, workspaceID, false)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	target, err := validateCollaborationPath(workspaceID, entityType, entityID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if request.URL.RawQuery != "" {
		handler.writeError(response, request, fmt.Errorf("%w: query parameters are not supported", authz.ErrInvalid))
		return
	}
	view, err := handler.collaboration.GetNexusView(request.Context(), principal, target)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	dto, err := publicCollaborationView(target, view)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	handler.writeJSON(response, request, http.StatusOK, collaborationViewResponse{Data: dto})
}

func (handler *CollaborationHandler) proposeDecision(response http.ResponseWriter, request *http.Request) {
	workspaceID := request.PathValue("workspace_id")
	threadID := request.PathValue("thread_id")
	principal, err := handler.authenticate(request, workspaceID, true)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	threadRef, err := validateCollaborationPath(workspaceID, "thread", threadID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if request.URL.RawQuery != "" {
		handler.writeError(response, request, fmt.Errorf("%w: query parameters are not supported", authz.ErrInvalid))
		return
	}
	var body proposeDecisionRequest
	if err := decodeJSON(response, request, &body, maxProposeDecisionBodyBytes); err != nil {
		handler.writeError(response, request, err)
		return
	}
	result, err := handler.collaboration.CreateDecisionFromThread(
		request.Context(),
		webInvocation(principal, request),
		goldenpath.CreateDecisionInput{
			ThreadID:          threadID,
			ClientOperationID: body.ClientOperationID,
			Question:          body.Question,
		},
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	decision, err := publicDecision(result.Decision)
	if err != nil {
		handler.writeError(response, request, fmt.Errorf("invalid proposed Decision application result: %w", err))
		return
	}
	if result.Decision.WorkspaceID != workspaceID ||
		result.Decision.Question != strings.TrimSpace(body.Question) ||
		result.Decision.ProposerID != principal.ID {
		handler.writeError(response, request, errors.New("invalid proposed Decision application result"))
		return
	}
	status := http.StatusOK
	if result.Created {
		if result.Decision.Status != "proposed" {
			handler.writeError(response, request, errors.New("new Decision is not proposed"))
			return
		}
		status = http.StatusCreated
	}
	handler.writeJSON(response, request, status, proposedDecisionResponse{Data: proposedDecisionDTO{
		Decision:     decision,
		SourceThread: publicRef(threadRef),
	}})
}

func (handler *CollaborationHandler) acceptDecision(response http.ResponseWriter, request *http.Request) {
	workspaceID := request.PathValue("workspace_id")
	decisionID := request.PathValue("decision_id")
	principal, err := handler.authenticate(request, workspaceID, true)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if _, err := validateCollaborationPath(workspaceID, "decision", decisionID); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if request.URL.RawQuery != "" {
		handler.writeError(response, request, fmt.Errorf("%w: query parameters are not supported", authz.ErrInvalid))
		return
	}
	var body acceptDecisionRequest
	if err := decodeJSON(response, request, &body, maxAcceptDecisionBodyBytes); err != nil {
		handler.writeError(response, request, err)
		return
	}
	if !body.Confirmed {
		handler.writeError(response, request, fmt.Errorf("%w: explicit confirmation is required", authz.ErrInvalid))
		return
	}
	result, err := handler.collaboration.AcceptDecision(
		request.Context(),
		webInvocation(principal, request),
		goldenpath.AcceptDecisionInput{
			DecisionID:        decisionID,
			ClientOperationID: body.ClientOperationID,
			Outcome:           body.Outcome,
			Rationale:         body.Rationale,
		},
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	decision, err := publicDecision(result.Decision)
	if err != nil {
		handler.writeError(response, request, fmt.Errorf("invalid accepted Decision application result: %w", err))
		return
	}
	if result.Decision.WorkspaceID != workspaceID || result.Decision.ID != decisionID ||
		result.Decision.Status != "accepted" ||
		result.Decision.Outcome != strings.TrimSpace(body.Outcome) ||
		result.Decision.Rationale != strings.TrimSpace(body.Rationale) ||
		len(result.Decision.DeciderIDs) != 1 || result.Decision.DeciderIDs[0] != principal.ID {
		handler.writeError(response, request, errors.New("invalid accepted Decision application result"))
		return
	}
	handler.writeJSON(response, request, http.StatusOK, decisionResponse{Data: decision})
}

func (handler *CollaborationHandler) createTicket(response http.ResponseWriter, request *http.Request) {
	workspaceID := request.PathValue("workspace_id")
	decisionID := request.PathValue("decision_id")
	principal, err := handler.authenticate(request, workspaceID, true)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	decisionRef, err := validateCollaborationPath(workspaceID, "decision", decisionID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	if request.URL.RawQuery != "" {
		handler.writeError(response, request, fmt.Errorf("%w: query parameters are not supported", authz.ErrInvalid))
		return
	}
	var body createTicketRequest
	if err := decodeJSON(response, request, &body, maxCreateTicketBodyBytes); err != nil {
		handler.writeError(response, request, err)
		return
	}
	result, err := handler.collaboration.CreateTicketFromDecision(
		request.Context(),
		webInvocation(principal, request),
		goldenpath.CreateTicketInput{
			DecisionID:        decisionID,
			ClientOperationID: body.ClientOperationID,
			Title:             body.Title,
		},
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	ticket, err := publicTicket(result.Ticket)
	if err != nil {
		handler.writeError(response, request, fmt.Errorf("invalid created Ticket application result: %w", err))
		return
	}
	if result.Ticket.WorkspaceID != workspaceID ||
		result.Ticket.Title != strings.TrimSpace(body.Title) || result.Ticket.Status != "open" {
		handler.writeError(response, request, errors.New("invalid created Ticket application result"))
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	handler.writeJSON(response, request, status, ticketResponse{Data: createdTicketDTO{
		Ticket:         ticket,
		SourceDecision: publicRef(decisionRef),
	}})
}

func (handler *CollaborationHandler) authenticate(
	request *http.Request,
	workspaceID string,
	write bool,
) (authz.Principal, error) {
	if _, err := handler.proxy.ClientIP(request); err != nil {
		return authz.Principal{}, err
	}
	if err := handler.session.ValidateHost(request); err != nil {
		return authz.Principal{}, err
	}
	token, err := handler.session.SessionToken(request)
	if err != nil {
		return authz.Principal{}, err
	}
	if write {
		csrfToken, err := handler.session.ValidateCSRF(request)
		if err != nil {
			return authz.Principal{}, err
		}
		if err := handler.sessions.VerifyCSRF(request.Context(), token, csrfToken); err != nil {
			return authz.Principal{}, err
		}
	}
	if !validScopedID(workspaceID, "wrk_") {
		return authz.Principal{}, fmt.Errorf("%w: invalid Workspace ID", authz.ErrInvalid)
	}
	verified, err := handler.sessions.ResolveWorkspace(request.Context(), token, workspaceID)
	if err != nil {
		if errors.Is(err, authz.ErrForbidden) {
			err = authz.ErrNotFound
		}
		return authz.Principal{}, err
	}
	return authn.UserPrincipal(verified)
}

func validateCollaborationPath(workspaceID string, entityType string, entityID string) (entityref.Ref, error) {
	if !validScopedID(workspaceID, "wrk_") {
		return entityref.Ref{}, fmt.Errorf("%w: invalid Workspace ID", authz.ErrInvalid)
	}
	target := entityref.Ref{Type: entityType, ID: entityID}
	if err := entityref.M0Registry().Validate(target); err != nil {
		return entityref.Ref{}, fmt.Errorf("%w: invalid %s ID", authz.ErrInvalid, entityType)
	}
	return target, nil
}

func publicCollaborationView(target entityref.Ref, view goldenpath.NexusView) (collaborationViewDTO, error) {
	if view.Current.Ref != target {
		return collaborationViewDTO{}, errors.New("collaboration Current projection does not match target")
	}
	relations, err := publicCollaborationRelations(view.Relations)
	if err != nil {
		return collaborationViewDTO{}, err
	}
	timeline, err := publicCollaborationTimeline(view.Timeline)
	if err != nil {
		return collaborationViewDTO{}, err
	}
	dto := collaborationViewDTO{Relations: relations, Timeline: timeline}
	switch target.Type {
	case "thread":
		current, err := publicThreadCurrent(view.Current, view.Relations, view.Timeline)
		if err != nil {
			return collaborationViewDTO{}, err
		}
		dto.Current = current
	case "decision":
		current, err := publicDecisionProjection(view.Current, view.Relations, view.Timeline)
		if err != nil {
			return collaborationViewDTO{}, err
		}
		dto.Current = current
	case "ticket":
		current, err := publicTicketProjection(view.Current, view.Relations, view.Timeline)
		if err != nil {
			return collaborationViewDTO{}, err
		}
		dto.Current = current
	default:
		return collaborationViewDTO{}, fmt.Errorf("unsupported collaboration target type %q", target.Type)
	}
	return dto, nil
}

func publicThreadCurrent(
	current goldenpath.CurrentProjection,
	relations []goldenpath.RelationProjection,
	timeline []goldenpath.TimelineItem,
) (threadCurrentDTO, error) {
	if current.GoverningProjectID == "" || current.Title == "" ||
		(current.Visibility != "project" && current.Visibility != "restricted") ||
		current.CreatedBy.Kind != "user" || !validScopedID(current.CreatedBy.ID, "usr_") ||
		current.CreatedAt.IsZero() || current.UpdatedAt.IsZero() || current.UpdatedAt.Before(current.CreatedAt) ||
		current.Status != "" || current.Outcome != "" || current.Rationale != "" ||
		current.ProposerID != "" || len(current.DeciderIDs) != 0 || current.DecidedAt != nil ||
		len(timeline) != 0 {
		return threadCurrentDTO{}, errors.New("invalid Thread Nexus View Current projection")
	}
	project := entityref.Ref{Type: "project", ID: current.GoverningProjectID}
	if err := entityref.M0Registry().Validate(project); err != nil {
		return threadCurrentDTO{}, fmt.Errorf("invalid Thread governing Project: %w", err)
	}
	dto := threadCurrentDTO{
		Ref:        publicRef(current.Ref),
		Project:    publicRef(project),
		Title:      current.Title,
		Visibility: current.Visibility,
		CreatedBy:  deploymentActorDTO{Kind: current.CreatedBy.Kind, ID: current.CreatedBy.ID},
		CreatedAt:  publicTime(current.CreatedAt),
		UpdatedAt:  publicTime(current.UpdatedAt),
	}
	if current.OriginChannel == nil {
		if len(relations) != 0 {
			return threadCurrentDTO{}, errors.New("project-origin Thread has unexpected source relation")
		}
		return dto, nil
	}
	channel, err := requiredVisibleEntity(*current.OriginChannel, "channel")
	if err != nil {
		return threadCurrentDTO{}, err
	}
	if len(relations) != 1 || relations[0].State != goldenpath.ProjectionVisible ||
		relations[0].RelationType != "started-from" || relations[0].Target.Type != "message" ||
		relations[0].Title != "Message" {
		return threadCurrentDTO{}, errors.New("messaging-origin Thread requires one readable started-from Message")
	}
	dto.OriginChannel = &channel
	return dto, nil
}

func publicDecisionProjection(
	current goldenpath.CurrentProjection,
	relations []goldenpath.RelationProjection,
	timeline []goldenpath.TimelineItem,
) (decisionCurrentDTO, error) {
	if current.Visibility != "" || current.CreatedBy != (goldenpath.ActorRef{}) ||
		current.OriginChannel != nil || current.Component != nil || current.Environment != nil ||
		current.CIRun != nil || current.StartedAt != nil || current.CompletedAt != nil ||
		current.RecordedAt != nil || len(relations) != 1 {
		return decisionCurrentDTO{}, errors.New("unexpected fields in Decision Nexus View")
	}
	if relations[0].State == goldenpath.ProjectionVisible &&
		(relations[0].RelationType != "derived-from" || relations[0].Target.Type != "thread") {
		return decisionCurrentDTO{}, errors.New("Decision requires a derived-from Thread relation")
	}
	for _, item := range timeline {
		if item.ActivityType != "decision.proposed" && item.ActivityType != "decision.accepted" {
			return decisionCurrentDTO{}, errors.New("unexpected Decision Timeline item")
		}
	}
	return publicDecision(goldenpath.Decision{
		ID:                 current.Ref.ID,
		WorkspaceID:        "projection",
		GoverningProjectID: current.GoverningProjectID,
		Question:           current.Title,
		Outcome:            current.Outcome,
		Rationale:          current.Rationale,
		Status:             current.Status,
		ProposerID:         current.ProposerID,
		DeciderIDs:         current.DeciderIDs,
		DecidedAt:          current.DecidedAt,
		CreatedAt:          current.CreatedAt,
		UpdatedAt:          current.UpdatedAt,
	})
}

func publicTicketProjection(
	current goldenpath.CurrentProjection,
	relations []goldenpath.RelationProjection,
	timeline []goldenpath.TimelineItem,
) (ticketCurrentDTO, error) {
	if current.Visibility != "" || current.CreatedBy.Kind != "user" || current.OriginChannel != nil || current.Outcome != "" ||
		current.Rationale != "" || current.ProposerID != "" || len(current.DeciderIDs) != 0 ||
		current.DecidedAt != nil || current.Component != nil || current.Environment != nil ||
		current.CIRun != nil || current.StartedAt != nil || current.CompletedAt != nil ||
		current.RecordedAt != nil || len(relations) != 1 ||
		relations[0].State != goldenpath.ProjectionVisible || relations[0].RelationType != "implements" ||
		relations[0].Target.Type != "decision" {
		return ticketCurrentDTO{}, errors.New("invalid Ticket Nexus View")
	}
	for _, item := range timeline {
		if item.ActivityType != "ticket.created" {
			return ticketCurrentDTO{}, errors.New("unexpected Ticket Timeline item")
		}
	}
	return publicTicket(goldenpath.Ticket{
		ID:                 current.Ref.ID,
		WorkspaceID:        "projection",
		GoverningProjectID: current.GoverningProjectID,
		Title:              current.Title,
		Status:             current.Status,
		CreatedBy:          current.CreatedBy.ID,
		CreatedAt:          current.CreatedAt,
		UpdatedAt:          current.UpdatedAt,
	})
}

func publicDecision(decision goldenpath.Decision) (decisionCurrentDTO, error) {
	ref := entityref.Ref{Type: "decision", ID: decision.ID}
	project := entityref.Ref{Type: "project", ID: decision.GoverningProjectID}
	if err := entityref.M0Registry().Validate(ref); err != nil {
		return decisionCurrentDTO{}, fmt.Errorf("invalid Decision reference: %w", err)
	}
	if err := entityref.M0Registry().Validate(project); err != nil {
		return decisionCurrentDTO{}, fmt.Errorf("invalid Decision governing Project: %w", err)
	}
	if decision.Question == "" || !validScopedID(decision.ProposerID, "usr_") ||
		decision.CreatedAt.IsZero() || decision.UpdatedAt.IsZero() || decision.UpdatedAt.Before(decision.CreatedAt) {
		return decisionCurrentDTO{}, errors.New("invalid Decision application projection")
	}
	dto := decisionCurrentDTO{
		Ref:       publicRef(ref),
		Project:   publicRef(project),
		Question:  decision.Question,
		Status:    decision.Status,
		Proposer:  deploymentActorDTO{Kind: "user", ID: decision.ProposerID},
		Deciders:  make([]deploymentActorDTO, 0, len(decision.DeciderIDs)),
		CreatedAt: publicTime(decision.CreatedAt),
		UpdatedAt: publicTime(decision.UpdatedAt),
	}
	switch decision.Status {
	case "proposed":
		if decision.Outcome != "" || decision.Rationale != "" || len(decision.DeciderIDs) != 0 || decision.DecidedAt != nil {
			return decisionCurrentDTO{}, errors.New("proposed Decision has accepted fields")
		}
	case "accepted":
		if decision.Outcome == "" || decision.Rationale == "" || len(decision.DeciderIDs) == 0 || decision.DecidedAt == nil {
			return decisionCurrentDTO{}, errors.New("accepted Decision is missing confirmation fields")
		}
		outcome := decision.Outcome
		rationale := decision.Rationale
		decidedAt := publicTime(*decision.DecidedAt)
		dto.Outcome = &outcome
		dto.Rationale = &rationale
		dto.DecidedAt = &decidedAt
		for _, id := range decision.DeciderIDs {
			if !validScopedID(id, "usr_") {
				return decisionCurrentDTO{}, errors.New("invalid Decision decider")
			}
			dto.Deciders = append(dto.Deciders, deploymentActorDTO{Kind: "user", ID: id})
		}
	default:
		return decisionCurrentDTO{}, fmt.Errorf("unsupported public Decision status %q", decision.Status)
	}
	return dto, nil
}

func publicTicket(ticket goldenpath.Ticket) (ticketCurrentDTO, error) {
	ref := entityref.Ref{Type: "ticket", ID: ticket.ID}
	project := entityref.Ref{Type: "project", ID: ticket.GoverningProjectID}
	if err := entityref.M0Registry().Validate(ref); err != nil {
		return ticketCurrentDTO{}, fmt.Errorf("invalid Ticket reference: %w", err)
	}
	if err := entityref.M0Registry().Validate(project); err != nil {
		return ticketCurrentDTO{}, fmt.Errorf("invalid Ticket governing Project: %w", err)
	}
	if ticket.Title == "" || ticket.Status != "open" || !validScopedID(ticket.CreatedBy, "usr_") || ticket.CreatedAt.IsZero() ||
		ticket.UpdatedAt.IsZero() || ticket.UpdatedAt.Before(ticket.CreatedAt) {
		return ticketCurrentDTO{}, errors.New("invalid Ticket application projection")
	}
	return ticketCurrentDTO{
		Ref:       publicRef(ref),
		Project:   publicRef(project),
		Title:     ticket.Title,
		Status:    ticket.Status,
		CreatedBy: deploymentActorDTO{Kind: "user", ID: ticket.CreatedBy},
		CreatedAt: publicTime(ticket.CreatedAt),
		UpdatedAt: publicTime(ticket.UpdatedAt),
	}, nil
}

func publicCollaborationRelations(relations []goldenpath.RelationProjection) ([]collaborationRelationDTO, error) {
	dto := make([]collaborationRelationDTO, 0, len(relations))
	for _, relation := range relations {
		switch relation.State {
		case goldenpath.ProjectionRestricted:
			if relation.RelationType != "" || relation.Target != (entityref.Ref{}) || relation.Title != "" {
				return nil, errors.New("restricted relation leaks fields")
			}
			dto = append(dto, collaborationRelationDTO{Visibility: "restricted"})
		case goldenpath.ProjectionVisible:
			entity, err := requiredVisibleEntity(goldenpath.SubjectProjection{
				State: relation.State,
				Ref:   relation.Target,
				Title: relation.Title,
			}, "")
			if err != nil {
				return nil, err
			}
			if relation.RelationType != "started-from" && relation.RelationType != "derived-from" &&
				relation.RelationType != "implements" {
				return nil, fmt.Errorf("unsupported collaboration relation %q", relation.RelationType)
			}
			dto = append(dto, collaborationRelationDTO{
				Visibility:   "readable",
				RelationType: relation.RelationType,
				Target:       &entity,
			})
		default:
			return nil, fmt.Errorf("invalid collaboration relation state %q", relation.State)
		}
	}
	return dto, nil
}

func publicCollaborationTimeline(items []goldenpath.TimelineItem) ([]collaborationTimelineDTO, error) {
	dto := make([]collaborationTimelineDTO, 0, len(items))
	for _, item := range items {
		if !validScopedID(item.EventID, "evt_") || item.Actor.Kind != "user" ||
			!validScopedID(item.Actor.ID, "usr_") || item.OccurredAt.IsZero() ||
			item.ProjectionVersion != goldenpath.ActivityProjectionVersion || len(item.SafeFacts) != 1 ||
			item.SafeFacts["status"] == "" {
			return nil, errors.New("invalid collaboration Timeline item")
		}
		if item.ActivityType != "decision.proposed" && item.ActivityType != "decision.accepted" &&
			item.ActivityType != "ticket.created" {
			return nil, fmt.Errorf("unsupported collaboration Activity type %q", item.ActivityType)
		}
		entry := collaborationTimelineDTO{
			ID:           item.EventID,
			ActivityType: item.ActivityType,
			Actor:        deploymentActorDTO{Kind: item.Actor.Kind, ID: item.Actor.ID},
			OccurredAt:   publicTime(item.OccurredAt),
			Status:       item.SafeFacts["status"],
			Subjects:     make([]collaborationSubjectDTO, 0, len(item.Subjects)),
		}
		for _, subject := range item.Subjects {
			switch subject.State {
			case goldenpath.ProjectionRestricted:
				if subject.Ref != (entityref.Ref{}) || subject.Title != "" {
					return nil, errors.New("restricted Timeline subject leaks fields")
				}
				entry.Subjects = append(entry.Subjects, collaborationSubjectDTO{Visibility: "restricted"})
			case goldenpath.ProjectionVisible:
				entity, err := requiredVisibleEntity(subject, "")
				if err != nil {
					return nil, err
				}
				entry.Subjects = append(entry.Subjects, collaborationSubjectDTO{Visibility: "readable", Entity: &entity})
			default:
				return nil, fmt.Errorf("invalid collaboration subject state %q", subject.State)
			}
		}
		dto = append(dto, entry)
	}
	return dto, nil
}

func (handler *CollaborationHandler) methodNotAllowed(response http.ResponseWriter, request *http.Request) {
	if strings.HasSuffix(request.URL.Path, "/nexus-view") {
		response.Header().Set("Allow", http.MethodGet)
	} else {
		response.Header().Set("Allow", http.MethodPost)
	}
	handler.writeError(response, request, ErrMethodNotAllowed)
}

func (handler *CollaborationHandler) notFound(response http.ResponseWriter, request *http.Request) {
	handler.writeError(response, request, authz.ErrNotFound)
}

func (handler *CollaborationHandler) writeJSON(
	response http.ResponseWriter,
	request *http.Request,
	status int,
	value any,
) {
	body, err := json.Marshal(value)
	if err != nil {
		handler.writeError(response, request, fmt.Errorf("marshal public collaboration response: %w", err))
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if _, err := response.Write(append(body, '\n')); err != nil {
		log.Printf("write public collaboration response request_id=%s: %v", RequestID(request.Context()), err)
	}
}

func (handler *CollaborationHandler) writeError(response http.ResponseWriter, request *http.Request, err error) {
	if MapApplicationError(err).StatusCode == http.StatusInternalServerError {
		log.Printf("public collaboration request failed request_id=%s: %v", RequestID(request.Context()), err)
	}
	if writeErr := WriteError(response, RequestID(request.Context()), err); writeErr != nil {
		log.Printf("write public collaboration error: %v", writeErr)
	}
}
