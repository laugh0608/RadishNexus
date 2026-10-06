package goldenpath

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type ticketComponentRecorder struct {
	command TicketComponentCommand
	calls   int
}

func (s *ticketComponentRecorder) WriteTicketComponent(_ context.Context, c TicketComponentCommand) (TicketComponentResult, error) {
	s.command = c
	s.calls++
	return TicketComponentResult{LinkID: c.ResultID}, nil
}
func (s *ticketComponentRecorder) ListTicketComponents(context.Context, authz.Principal, entityref.Ref, DiscoveryPageInput) (TicketComponentPage, error) {
	s.calls++
	return TicketComponentPage{}, nil
}
func TestTicketComponentCommandBoundaryAndDigest(t *testing.T) {
	store := &ticketComponentRecorder{}
	s := NewTicketComponentService(store, CryptoIDGenerator{}, SystemClock{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_member", WorkspaceID: "wrk_main"}, SourceKind: "web", CorrelationID: "req_ticket_component_test"}
	in := TicketComponentInput{TicketID: "tkt_main", ComponentID: "cmp_main", ClientOperationID: "link", Confirmed: true}
	call := func(kind string, in TicketComponentInput) error {
		_, e := s.WriteTicketComponent(context.Background(), inv, kind, in)
		return e
	}
	if e := call("ticket.component.link", in); e != nil {
		t.Fatal(e)
	}
	first := store.command
	if first.OccurredAt.IsZero() || !strings.HasPrefix(first.ResultID, "lnk_") || !strings.HasPrefix(first.EventID, "evt_") {
		t.Fatal(first)
	}
	if e := call("ticket.component.link", in); e != nil || store.command.PayloadSHA256 != first.PayloadSHA256 || store.command.ResultID == first.ResultID {
		t.Fatal("unstable digest or reused candidate", e)
	}
	in.ComponentID = "cmp_other"
	if e := call("ticket.component.link", in); e != nil || store.command.PayloadSHA256 == first.PayloadSHA256 {
		t.Fatal("component missing from digest", e)
	}
	for _, change := range []func(*TicketComponentInput){func(i *TicketComponentInput) { i.Confirmed = false }, func(i *TicketComponentInput) { i.ComponentID = "tkt_wrong" }, func(i *TicketComponentInput) { i.TicketID = "cmp_wrong" }, func(i *TicketComponentInput) { i.LinkID = "lnk_unexpected" }, func(i *TicketComponentInput) { i.ClientOperationID = "bad\n" }, func(i *TicketComponentInput) { i.ClientOperationID = strings.Repeat("x", 129) }} {
		bad := in
		change(&bad)
		before := store.calls
		if e := call("ticket.component.link", bad); !errors.Is(e, authz.ErrInvalid) || store.calls != before {
			t.Fatal("invalid input reached store", e)
		}
	}
	unlink := TicketComponentInput{TicketID: in.TicketID, LinkID: "lnk_old", ClientOperationID: "unlink", Confirmed: true}
	if e := call("ticket.component.unlink", unlink); e != nil || store.command.ResultID != "lnk_old" {
		t.Fatal("exact unlink", e)
	}
	digest := store.command.PayloadSHA256
	unlink.LinkID = "lnk_new"
	if e := call("ticket.component.unlink", unlink); e != nil || store.command.PayloadSHA256 == digest {
		t.Fatal("unlink identity missing from digest", e)
	}
	inv.SourceKind = "api"
	if e := call("ticket.component.link", in); !errors.Is(e, authz.ErrInvalid) {
		t.Fatal("nonweb command", e)
	}
	inv.SourceKind = "web"
	inv.Principal.Kind = "plugin"
	if e := call("ticket.component.link", in); e == nil {
		t.Fatal("plugin accepted")
	}
}
