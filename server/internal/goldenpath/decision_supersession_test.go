package goldenpath

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestSupersessionValidationAndCanonicalPayload(t *testing.T) {
	store := &recordingStore{}
	ids := &sequenceIDs{values: []string{"lnk_one", "evt_one", "lnk_two", "evt_two"}}
	s := NewService(store, ids, fixedClock{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_one", WorkspaceID: "wrk_one"}, SourceKind: "web", CorrelationID: "cor_one"}
	in := SupersedeDecisionInput{DecisionID: "dec_old", ReplacementID: "dec_new", ClientOperationID: "same-operation", BaseDecisionRevision: 2, BaseReplacementRevision: 2, SupersessionReason: " Reason "}
	for _, mutate := range []func(*SupersedeDecisionInput){func(v *SupersedeDecisionInput) { v.BaseDecisionRevision = 0 }, func(v *SupersedeDecisionInput) { v.BaseReplacementRevision = MaxDecisionRevision + 1 }, func(v *SupersedeDecisionInput) { v.ReplacementID = "tkt_wrong" }, func(v *SupersedeDecisionInput) { v.SupersessionReason = "\u0085" }, func(v *SupersedeDecisionInput) { v.SupersessionReason = "bad\x00" }, func(v *SupersedeDecisionInput) { v.SupersessionReason = strings.Repeat("x", 32769) }} {
		bad := in
		mutate(&bad)
		if _, e := s.SupersedeDecision(context.Background(), inv, bad); !errors.Is(e, authz.ErrInvalid) {
			t.Fatal("invalid supersession", e)
		}
	}
	if _, e := s.SupersedeDecision(context.Background(), inv, in); e != nil {
		t.Fatal(e)
	}
	first := store.supersedeDecisionCommand
	in.SupersessionReason = "Reason"
	if _, e := s.SupersedeDecision(context.Background(), inv, in); e != nil {
		t.Fatal(e)
	}
	second := store.supersedeDecisionCommand
	if first.PayloadSHA256 != second.PayloadSHA256 || second.SupersessionReason != "Reason" || second.LinkID != "lnk_two" {
		t.Fatal("canonical payload changed")
	}
}
