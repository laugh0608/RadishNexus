package contextpackage

import (
	"encoding/json"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/platform/jsontext"
)

const MaxReviewReasonBytes = 4 * 1024

type DecisionReview struct {
	DecisionRevision   int    `json:"decision_revision"`
	Trigger            string `json:"trigger"`
	Conclusion         string `json:"conclusion"`
	Reason             string `json:"reason"`
	AdditionalEvidence *Ref   `json:"additional_evidence"`
}

func ValidReviewReason(s string) bool {
	return textValue(s, MaxReviewReasonBytes, true)
}

func ValidReviewConclusion(s string) bool {
	return oneOf(s, "still_applicable", "reconsider")
}

func DecodeDecisionReview(raw []byte) (DecisionReview, error) {
	var review DecisionReview
	var fields map[string]json.RawMessage
	if Decode(raw, &fields) != nil || len(fields) != 5 || fields["decision_revision"] == nil ||
		fields["trigger"] == nil || fields["conclusion"] == nil || fields["reason"] == nil || fields["additional_evidence"] == nil ||
		Decode(raw, &review) != nil || jsontext.Decode(fields["reason"], &review.Reason) != nil || (review.DecisionRevision != 2 && review.DecisionRevision != 3) ||
		review.Trigger != "manual" || !ValidReviewConclusion(review.Conclusion) ||
		!ValidReviewReason(review.Reason) || strings.TrimSpace(review.Reason) != review.Reason ||
		(review.AdditionalEvidence != nil && !validReviewEvidence(fields["additional_evidence"], review.AdditionalEvidence)) {
		return review, invalid("Decision review facts")
	}
	return review, nil
}

func validReviewEvidence(raw []byte, ref *Ref) bool {
	var fields map[string]json.RawMessage
	return Decode(raw, &fields) == nil && len(fields) == 2 && fields["type"] != nil &&
		fields["id"] != nil && refType(ref, "thread")
}
