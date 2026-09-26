package goldenpath

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type RecordStagingDeploymentInput struct {
	ClientOperationID string
	Confirmed         bool
	EnvironmentID     string
	CIRunID           string
	Status            string
	StartedAt         *time.Time
	CompletedAt       time.Time
}

type Deployment struct {
	Duplicate     bool
	ID            string
	WorkspaceID   string
	EnvironmentID string
	CIRunID       string
	Status        string
	StartedAt     *time.Time
	CompletedAt   time.Time
	RecordedBy    string
	SourceKind    string
	SourceID      string
	RecordedAt    time.Time
}

type RecordStagingDeploymentCommand struct {
	ClientOperationID string
	PayloadSHA256     string
	Invocation
	DeploymentID  string
	LinkID        string
	EventID       string
	EnvironmentID string
	CIRunID       string
	Status        string
	StartedAt     *time.Time
	CompletedAt   time.Time
	RecordedAt    time.Time
}

// RecordStagingDeployment records an explicitly authorized terminal staging
// fact. It never executes a deployment and is not called by CI Run recording.
func (service *Service) RecordStagingDeployment(
	ctx context.Context,
	invocation Invocation,
	input RecordStagingDeploymentInput,
) (Deployment, error) {
	if err := validateInvocation(invocation); err != nil {
		return Deployment{}, err
	}
	if err := validateStagingDeploymentInput(input); err != nil {
		return Deployment{}, err
	}

	if input.CompletedAt.After(service.clock.Now().Add(300 * time.Second)) {
		return Deployment{}, authz.ErrInvalid
	}
	payload, err := json.Marshal(struct {
		Environment, CIRun, Status string
		Started                    *time.Time
		Completed                  time.Time
		Confirmed                  bool
	}{input.EnvironmentID, input.CIRunID, input.Status, utcTimePointer(input.StartedAt), input.CompletedAt.UTC(), input.Confirmed})
	if err != nil {
		return Deployment{}, fmt.Errorf("encode deployment receipt: %w", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	deploymentID, err := service.ids.NewID("dpl_")
	if err != nil {
		return Deployment{}, fmt.Errorf("generate Deployment ID: %w", err)
	}
	linkID, err := service.ids.NewID("lnk_")
	if err != nil {
		return Deployment{}, fmt.Errorf("generate Deployment CI Run link ID: %w", err)
	}
	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return Deployment{}, fmt.Errorf("generate Deployment event ID: %w", err)
	}

	return service.store.RecordStagingDeployment(ctx, RecordStagingDeploymentCommand{
		Invocation:        invocation,
		ClientOperationID: input.ClientOperationID,
		PayloadSHA256:     digest,
		DeploymentID:      deploymentID,
		LinkID:            linkID,
		EventID:           eventID,
		EnvironmentID:     input.EnvironmentID,
		CIRunID:           input.CIRunID,
		Status:            input.Status,
		StartedAt:         utcTimePointer(input.StartedAt),
		CompletedAt:       input.CompletedAt.UTC(),
		RecordedAt:        service.clock.Now().UTC(),
	})
}

func validateStagingDeploymentInput(input RecordStagingDeploymentInput) error {
	if !validClientOperationID(input.ClientOperationID) || !input.Confirmed {
		return authz.ErrInvalid
	}
	for _, ref := range []entityref.Ref{{Type: "environment", ID: input.EnvironmentID}, {Type: "ci-run", ID: input.CIRunID}} {
		if err := entityref.M0Registry().Validate(ref); err != nil {
			return authz.ErrInvalid
		}
	}
	if input.CompletedAt.Nanosecond()%1000000 != 0 || (input.StartedAt != nil && (input.StartedAt.IsZero() || input.StartedAt.Nanosecond()%1000000 != 0)) {
		return authz.ErrInvalid
	}
	if input.Status != "succeeded" && input.Status != "failed" && input.Status != "canceled" {
		return fmt.Errorf("%w: completed Deployment status must be succeeded, failed, or canceled", authz.ErrInvalid)
	}
	if input.CompletedAt.IsZero() {
		return fmt.Errorf("%w: Deployment completed time is required", authz.ErrInvalid)
	}
	if input.StartedAt != nil && input.StartedAt.After(input.CompletedAt) {
		return fmt.Errorf("%w: Deployment cannot complete before it starts", authz.ErrInvalid)
	}
	return nil
}

type StagingTarget struct {
	Ref       entityref.Ref
	Name, Key string
}
type StagingTargetPage struct {
	Items  []StagingTarget
	NextID string
}

func (s *Service) ListStagingTargets(ctx context.Context, p authz.Principal, ciRun string, in DiscoveryPageInput) (StagingTargetPage, error) {
	if err := p.ValidateUser(); err != nil {
		return StagingTargetPage{}, err
	}
	if !ValidConfigurationID(p.WorkspaceID, "wrk_") || entityref.M0Registry().Validate(entityref.Ref{Type: "ci-run", ID: ciRun}) != nil || in.Limit < 1 || in.Limit > MaxDiscoveryPageSize || (in.AfterID != "" && entityref.M0Registry().Validate(entityref.Ref{Type: "environment", ID: in.AfterID}) != nil) {
		return StagingTargetPage{}, authz.ErrInvalid
	}
	return s.store.ListStagingTargets(ctx, p, ciRun, in)
}
