package goldenpath

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

var sha256DigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// VerifiedJenkinsDelivery is the trust boundary after source authentication,
// replay headers, and payload mapping have succeeded. It intentionally carries
// only an opaque digest and mapped facts, never a Secret or raw webhook body.
type VerifiedJenkinsDelivery struct {
	WorkspaceID   string
	SourceID      string
	DeliveryID    string
	PayloadSHA256 string
}

type RecordCompletedCIRunInput struct {
	ComponentID    string
	ExternalRunKey string
	Status         string
	StartedAt      *time.Time
	CompletedAt    time.Time
}

type CIRun struct {
	ID             string
	WorkspaceID    string
	ComponentID    string
	SourceKind     string
	SourceID       string
	ExternalRunKey string
	Status         string
	StartedAt      *time.Time
	CompletedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CIRunReceipt struct {
	CIRun     CIRun
	Duplicate bool
}

type RecordCompletedCIRunCommand struct {
	WorkspaceID    string
	SourceID       string
	DeliveryID     string
	PayloadSHA256  string
	ComponentID    string
	ExternalRunKey string
	Status         string
	StartedAt      *time.Time
	CompletedAt    time.Time
	CIRunID        string
	EventID        string
	CorrelationID  string
	RecordedAt     time.Time
}

// RecordCompletedJenkinsRun records a terminal Jenkins fact. HTTP routing and
// signature verification deliberately live outside this application boundary.
func (service *Service) RecordCompletedJenkinsRun(
	ctx context.Context,
	delivery VerifiedJenkinsDelivery,
	input RecordCompletedCIRunInput,
) (CIRunReceipt, error) {
	if err := validateVerifiedJenkinsDelivery(delivery); err != nil {
		return CIRunReceipt{}, err
	}
	if err := validateCompletedCIRunInput(input); err != nil {
		return CIRunReceipt{}, err
	}

	ciRunID, err := service.ids.NewID("cir_")
	if err != nil {
		return CIRunReceipt{}, fmt.Errorf("generate CI Run ID: %w", err)
	}
	eventID, err := service.ids.NewID("evt_")
	if err != nil {
		return CIRunReceipt{}, fmt.Errorf("generate CI Run event ID: %w", err)
	}
	correlationID, err := service.ids.NewID("cor_")
	if err != nil {
		return CIRunReceipt{}, fmt.Errorf("generate CI Run correlation ID: %w", err)
	}

	startedAt := utcTimePointer(input.StartedAt)
	return service.store.RecordCompletedCIRun(ctx, RecordCompletedCIRunCommand{
		WorkspaceID:    delivery.WorkspaceID,
		SourceID:       delivery.SourceID,
		DeliveryID:     delivery.DeliveryID,
		PayloadSHA256:  delivery.PayloadSHA256,
		ComponentID:    input.ComponentID,
		ExternalRunKey: input.ExternalRunKey,
		Status:         input.Status,
		StartedAt:      startedAt,
		CompletedAt:    input.CompletedAt.UTC(),
		CIRunID:        ciRunID,
		EventID:        eventID,
		CorrelationID:  correlationID,
		RecordedAt:     service.clock.Now().UTC(),
	})
}

func validateVerifiedJenkinsDelivery(delivery VerifiedJenkinsDelivery) error {
	for name, value := range map[string]string{
		"Workspace ID": delivery.WorkspaceID,
		"source ID":    delivery.SourceID,
		"delivery ID":  delivery.DeliveryID,
	} {
		if value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s must be non-empty and canonical", authz.ErrInvalid, name)
		}
	}
	if len(delivery.SourceID) > 255 || len(delivery.DeliveryID) > 512 {
		return fmt.Errorf("%w: Jenkins source or delivery ID exceeds the supported boundary", authz.ErrInvalid)
	}
	if !sha256DigestPattern.MatchString(delivery.PayloadSHA256) {
		return fmt.Errorf("%w: payload SHA-256 must be 64 lowercase hexadecimal characters", authz.ErrInvalid)
	}
	return nil
}

func validateCompletedCIRunInput(input RecordCompletedCIRunInput) error {
	for name, value := range map[string]string{
		"Component ID":     input.ComponentID,
		"external run key": input.ExternalRunKey,
	} {
		if value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s must be non-empty and canonical", authz.ErrInvalid, name)
		}
	}
	if len(input.ExternalRunKey) > 512 {
		return fmt.Errorf("%w: external run key exceeds the supported boundary", authz.ErrInvalid)
	}
	if input.Status != "succeeded" && input.Status != "failed" && input.Status != "canceled" {
		return fmt.Errorf("%w: completed CI Run status must be succeeded, failed, or canceled", authz.ErrInvalid)
	}
	if input.CompletedAt.IsZero() {
		return fmt.Errorf("%w: completed time is required", authz.ErrInvalid)
	}
	if input.StartedAt != nil && input.StartedAt.After(input.CompletedAt) {
		return fmt.Errorf("%w: CI Run cannot complete before it starts", authz.ErrInvalid)
	}
	return nil
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
