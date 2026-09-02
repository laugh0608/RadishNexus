package goldenpath

import (
	"context"
	"fmt"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const ActivityProjectionVersion = 1

type CurrentProjection struct {
	Ref                entityref.Ref
	GoverningProjectID string
	Title              string
	Status             string
	Visibility         string
	CreatedBy          ActorRef
	CreatedAt          time.Time
	Outcome            string
	Rationale          string
	ProposerID         string
	DeciderIDs         []string
	DecidedAt          *time.Time
	OriginChannel      *SubjectProjection
	UpdatedAt          time.Time
	Component          *SubjectProjection
	Environment        *SubjectProjection
	CIRun              *SubjectProjection
	StartedAt          *time.Time
	CompletedAt        *time.Time
	RecordedAt         *time.Time
}

type ActorRef struct {
	Kind string
	ID   string
}

// SubjectProjection leaves Ref and Title empty for a restricted subject.
// Hidden subjects are omitted from TimelineItem.Subjects.
type SubjectProjection struct {
	State ProjectionState
	Ref   entityref.Ref
	Title string
}

type TimelineItem struct {
	EventID           string
	ActivityType      string
	Actor             ActorRef
	OccurredAt        time.Time
	Subjects          []SubjectProjection
	ProjectionVersion int
	SafeFacts         map[string]string
}

type NexusView struct {
	Current   CurrentProjection
	Relations []RelationProjection
	Timeline  []TimelineItem
}

func (service *Service) GetNexusView(
	ctx context.Context,
	principal authz.Principal,
	target entityref.Ref,
) (NexusView, error) {
	if err := principal.ValidateUser(); err != nil {
		return NexusView{}, err
	}
	if err := entityref.M0Registry().Validate(target); err != nil {
		return NexusView{}, fmt.Errorf("%w: target reference: %v", authz.ErrInvalid, err)
	}
	if target.Type != "thread" && target.Type != "decision" && target.Type != "ticket" &&
		target.Type != "ci-run" && target.Type != "deployment" {
		return NexusView{}, fmt.Errorf(
			"%w: Nexus View currently supports Thread, Decision, Ticket, CI Run, and Deployment",
			authz.ErrInvalid,
		)
	}
	return service.store.GetNexusView(ctx, principal, target)
}
