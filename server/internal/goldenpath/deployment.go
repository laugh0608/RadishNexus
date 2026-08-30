package goldenpath

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type RecordStagingDeploymentInput struct {
	EnvironmentID string
	CIRunID       string
	Status        string
	StartedAt     *time.Time
	CompletedAt   time.Time
}

type Deployment struct {
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
		Invocation:    invocation,
		DeploymentID:  deploymentID,
		LinkID:        linkID,
		EventID:       eventID,
		EnvironmentID: input.EnvironmentID,
		CIRunID:       input.CIRunID,
		Status:        input.Status,
		StartedAt:     utcTimePointer(input.StartedAt),
		CompletedAt:   input.CompletedAt.UTC(),
		RecordedAt:    service.clock.Now().UTC(),
	})
}

func validateStagingDeploymentInput(input RecordStagingDeploymentInput) error {
	for name, value := range map[string]string{
		"Environment ID": input.EnvironmentID,
		"CI Run ID":      input.CIRunID,
	} {
		if value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s must be non-empty and canonical", authz.ErrInvalid, name)
		}
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
