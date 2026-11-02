package goldenpath

import (
	"context"
	"fmt"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const MaxRejectionReasonBytes = 32 * 1024

func ValidRejectionReason(raw string) bool {
	return len(raw) <= MaxRejectionReasonBytes && validCollaborationText(raw, strings.TrimSpace(raw))
}

func (s *Service) RejectDecision(ctx context.Context, invocation Invocation, input RejectDecisionInput) (RejectDecisionResult, error) {
	if err := validateInvocation(invocation); err != nil {
		return RejectDecisionResult{}, err
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "decision", ID: input.DecisionID}); err != nil {
		return RejectDecisionResult{}, fmt.Errorf("%w: Decision reference: %v", authz.ErrInvalid, err)
	}
	if !validClientOperationID(input.ClientOperationID) || !ValidRejectionReason(input.RejectionReason) {
		return RejectDecisionResult{}, fmt.Errorf("%w: operation ID and rejection reason are required", authz.ErrInvalid)
	}
	reason := strings.TrimSpace(input.RejectionReason)
	digest := collaborationPayloadDigest(struct {
		Reason string `json:"rejection_reason"`
	}{reason})
	event, err := s.ids.NewID("evt_")
	if err != nil {
		return RejectDecisionResult{}, fmt.Errorf("generate rejection event ID: %w", err)
	}
	return s.store.RejectDecision(ctx, RejectDecisionCommand{Invocation: invocation, DecisionID: input.DecisionID,
		ClientOperationID: input.ClientOperationID, RejectionReason: reason, PayloadSHA256: digest, EventID: event})
}
