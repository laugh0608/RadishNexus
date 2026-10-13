package goldenpath

import (
	"context"
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"strings"
	"testing"
)

type ticketStatusRecorder struct {
	command TicketStatusCommand
	calls   int
}

func (s *ticketStatusRecorder) ChangeTicketStatus(_ context.Context, c TicketStatusCommand) (TicketStatusResult, error) {
	s.command = c
	s.calls++
	return TicketStatusResult{}, nil
}

func TestTicketStatusMatrix(t *testing.T) {
	allowed := map[string]bool{"open/in-progress": true, "open/done": true, "open/canceled": true, "in-progress/open": true, "in-progress/done": true, "in-progress/canceled": true, "done/open": true, "canceled/open": true}
	for _, from := range []string{"open", "in-progress", "done", "canceled", "unknown"} {
		for _, to := range []string{"open", "in-progress", "done", "canceled", "unknown"} {
			if got := CanTransitionTicketStatus(from, to); got != allowed[from+"/"+to] {
				t.Fatalf("%s -> %s: %v", from, to, got)
			}
		}
	}
	for _, r := range []int{0, -1, MaxTicketStatusRevision, MaxTicketStatusRevision + 1} {
		if (TicketStatusTransition{"open", "done", r, r + 1}).Valid() {
			t.Fatal("invalid transition revision", r)
		}
	}
}
func TestTicketStatusCommandDigestAndBoundary(t *testing.T) {
	store := &ticketStatusRecorder{}
	s := NewTicketStatusService(store, CryptoIDGenerator{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_member", WorkspaceID: "wrk_main"}, SourceKind: "web", CorrelationID: "req_status"}
	in := TicketStatusInput{TicketID: "tkt_main", ClientOperationID: "status", BaseStatusRevision: 1, ToStatus: "done", Confirmed: true}
	call := func(in TicketStatusInput) error {
		_, e := s.ChangeTicketStatus(context.Background(), inv, in)
		return e
	}
	if e := call(in); e != nil {
		t.Fatal(e)
	}
	first := store.command
	if e := call(in); e != nil || first.PayloadSHA256 != store.command.PayloadSHA256 || first.EventID == store.command.EventID {
		t.Fatal("unstable digest", e)
	}
	for _, change := range []func(*TicketStatusInput){func(i *TicketStatusInput) { i.BaseStatusRevision = 2 }, func(i *TicketStatusInput) { i.ToStatus = "in-progress" }} {
		next := in
		change(&next)
		if e := call(next); e != nil || store.command.PayloadSHA256 == first.PayloadSHA256 {
			t.Fatal("missing digest field", e)
		}
	}
	for _, change := range []func(*TicketStatusInput){func(i *TicketStatusInput) { i.BaseStatusRevision = 0 }, func(i *TicketStatusInput) { i.BaseStatusRevision = MaxTicketStatusRevision + 1 }, func(i *TicketStatusInput) { i.ToStatus = "unknown" }, func(i *TicketStatusInput) { i.Confirmed = false }, func(i *TicketStatusInput) { i.TicketID = "dec_other" }, func(i *TicketStatusInput) { i.ClientOperationID = "bad\n" }, func(i *TicketStatusInput) { i.ClientOperationID = strings.Repeat("x", 129) }} {
		bad := in
		change(&bad)
		calls := store.calls
		if e := call(bad); !errors.Is(e, authz.ErrInvalid) || store.calls != calls {
			t.Fatal("invalid reached store", e)
		}
	}
	inv.SourceKind = "api"
	if e := call(in); !errors.Is(e, authz.ErrInvalid) {
		t.Fatal(e)
	}
}
