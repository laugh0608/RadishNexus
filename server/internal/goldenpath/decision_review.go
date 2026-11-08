package goldenpath

import (
	"context"
	"fmt"
	"strings"
	"time"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const MaxDecisionReviewPageSize = 50

type ReviewDecisionInput struct {
	DecisionID, ClientOperationID, Conclusion, Reason string
	BaseDecisionRevision                              int
	AdditionalEvidence                                *entityref.Ref
}

type ReviewDecisionCommand struct {
	Invocation
	DecisionID, ClientOperationID, PayloadSHA256, EventID string
	Facts                                                 cp.DecisionReview
}

type DecisionReviewRecord struct {
	EventID, DecisionID string
	Actor               ActorRef
	OccurredAt          time.Time
	Facts               cp.DecisionReview
	OriginalEvidence    []entityref.Ref
	Imported            bool
}

type DecisionReviewPageInput struct {
	Limit     int
	AfterTime time.Time
	AfterID   string
}

type DecisionReviewPage struct {
	Items   []DecisionReviewRecord
	HasMore bool
}

func (s *Service) ReviewDecision(ctx context.Context, inv Invocation, input ReviewDecisionInput) (DecisionReviewRecord, bool, error) {
	if err := validateInvocation(inv); err != nil {
		return DecisionReviewRecord{}, false, err
	}
	if inv.SourceKind != "web" || !cp.ValidID(input.DecisionID, "dec_") ||
		!validClientOperationID(input.ClientOperationID) || !cp.ValidReviewReason(input.Reason) ||
		!cp.ValidReviewConclusion(input.Conclusion) ||
		input.BaseDecisionRevision < 1 || input.BaseDecisionRevision > MaxDecisionRevision {
		return DecisionReviewRecord{}, false, fmt.Errorf("%w: invalid Decision review", authz.ErrInvalid)
	}
	var evidence *cp.Ref
	if ref := input.AdditionalEvidence; ref != nil {
		if ref.Type != "thread" || !cp.ValidID(ref.ID, "thr_") {
			return DecisionReviewRecord{}, false, fmt.Errorf("%w: invalid review evidence", authz.ErrInvalid)
		}
		evidence = &cp.Ref{Type: ref.Type, ID: ref.ID}
	}
	facts := cp.DecisionReview{
		DecisionRevision: input.BaseDecisionRevision, Trigger: "manual", Conclusion: input.Conclusion,
		Reason: strings.TrimSpace(input.Reason), AdditionalEvidence: evidence,
	}
	digest := collaborationPayloadDigest(struct {
		Base               int     `json:"base_decision_revision"`
		Conclusion         string  `json:"conclusion"`
		Reason             string  `json:"reason"`
		AdditionalEvidence *cp.Ref `json:"additional_evidence"`
	}{facts.DecisionRevision, facts.Conclusion, facts.Reason, evidence})
	event, err := s.ids.NewID("evt_")
	if err != nil {
		return DecisionReviewRecord{}, false, fmt.Errorf("generate Decision review event: %w", err)
	}
	return s.store.ReviewDecision(ctx, ReviewDecisionCommand{
		Invocation: inv, DecisionID: input.DecisionID, ClientOperationID: input.ClientOperationID,
		PayloadSHA256: digest, EventID: event, Facts: facts,
	})
}

func (s *Service) ListDecisionReviews(ctx context.Context, p authz.Principal, decisionID string, input DecisionReviewPageInput) (DecisionReviewPage, error) {
	if err := p.ValidateUser(); err != nil {
		return DecisionReviewPage{}, err
	}
	if !cp.ValidID(decisionID, "dec_") || input.Limit < 1 || input.Limit > MaxDecisionReviewPageSize ||
		(input.AfterID == "") != input.AfterTime.IsZero() ||
		(input.AfterID != "" && !cp.ValidID(input.AfterID, "evt_")) {
		return DecisionReviewPage{}, fmt.Errorf("%w: invalid Decision review page", authz.ErrInvalid)
	}
	return s.store.ListDecisionReviews(ctx, p, decisionID, input)
}
