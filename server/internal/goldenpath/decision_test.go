package goldenpath

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestRejectDecisionValidationAndCanonicalRetryDigest(t *testing.T) {
	store := &recordingStore{}
	s := NewService(store, &sequenceIDs{values: []string{"evt_one", "evt_two"}}, fixedClock{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_one", WorkspaceID: "wrk_one"}, SourceKind: "web", CorrelationID: "cor_one"}
	for _, reason := range []string{"", " \n ", "bad\x00", string([]byte{0xff}), strings.Repeat("x", 32769)} {
		if _, e := s.RejectDecision(context.Background(), inv, RejectDecisionInput{DecisionID: "dec_one", ClientOperationID: "op", RejectionReason: reason}); !errors.Is(e, authz.ErrInvalid) {
			t.Fatal("invalid reason", e)
		}
	}
	for _, reason := range []string{"  Reason  ", "Reason"} {
		if _, e := s.RejectDecision(context.Background(), inv, RejectDecisionInput{DecisionID: "dec_one", ClientOperationID: "op", RejectionReason: reason}); e != nil {
			t.Fatal(e)
		}
		if store.rejectDecisionCommand.RejectionReason != "Reason" {
			t.Fatal("normalization")
		}
	}
	if store.rejectDecisionCommand.PayloadSHA256 != collaborationPayloadDigest(struct {
		Reason string `json:"rejection_reason"`
	}{"Reason"}) {
		t.Fatal("digest changed")
	}
}
