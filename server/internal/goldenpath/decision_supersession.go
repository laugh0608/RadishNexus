package goldenpath

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const MaxDecisionRevision = 2147483647

type SupersedeDecisionInput struct {
	DecisionID, ReplacementID, ClientOperationID, SupersessionReason string
	BaseDecisionRevision, BaseReplacementRevision                    int
}
type SupersedeDecisionCommand struct {
	Invocation
	SupersedeDecisionInput
	PayloadSHA256, LinkID, EventID string
}
type SupersedeDecisionResult struct {
	DecisionID, ReplacementID, LinkID, SupersessionReason                    string
	FromRevision, ToRevision, ReplacementFromRevision, ReplacementToRevision int
	OccurredAt                                                               time.Time
}

func (s *Service) SupersedeDecision(ctx context.Context, inv Invocation, in SupersedeDecisionInput) (SupersedeDecisionResult, error) {
	if inv.SourceKind != "web" {
		return SupersedeDecisionResult{}, fmt.Errorf("%w: supersession requires the Web command boundary", authz.ErrInvalid)
	}
	if err := validateInvocation(inv); err != nil {
		return SupersedeDecisionResult{}, err
	}
	for _, id := range []string{in.DecisionID, in.ReplacementID} {
		if err := entityref.M0Registry().Validate(entityref.Ref{Type: "decision", ID: id}); err != nil {
			return SupersedeDecisionResult{}, fmt.Errorf("%w: Decision reference", authz.ErrInvalid)
		}
	}
	if !validClientOperationID(in.ClientOperationID) || !ValidRejectionReason(in.SupersessionReason) || in.BaseDecisionRevision < 1 || in.BaseDecisionRevision > MaxDecisionRevision || in.BaseReplacementRevision < 1 || in.BaseReplacementRevision > MaxDecisionRevision {
		return SupersedeDecisionResult{}, fmt.Errorf("%w: operation, revisions and supersession reason required", authz.ErrInvalid)
	}
	in.SupersessionReason = strings.TrimSpace(in.SupersessionReason)
	digest := collaborationPayloadDigest(struct {
		Replacement     string `json:"replacement_decision_id"`
		Base            int    `json:"base_decision_revision"`
		ReplacementBase int    `json:"base_replacement_revision"`
		Reason          string `json:"supersession_reason"`
	}{in.ReplacementID, in.BaseDecisionRevision, in.BaseReplacementRevision, in.SupersessionReason})
	link, err := s.ids.NewID("lnk_")
	if err != nil {
		return SupersedeDecisionResult{}, fmt.Errorf("generate supersession link: %w", err)
	}
	event, err := s.ids.NewID("evt_")
	if err != nil {
		return SupersedeDecisionResult{}, fmt.Errorf("generate supersession event: %w", err)
	}
	return s.store.SupersedeDecision(ctx, SupersedeDecisionCommand{Invocation: inv, SupersedeDecisionInput: in, PayloadSHA256: digest, LinkID: link, EventID: event})
}
