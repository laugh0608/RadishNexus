package goldenpath

import (
	"context"
	"errors"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type priorityRecorder struct {
	TicketPriorityStore
	command TicketPriorityCommand
	calls   int
}

func (r *priorityRecorder) SetTicketPriority(_ context.Context, c TicketPriorityCommand) (TicketPriorityResult, error) {
	r.command = c
	r.calls++
	return TicketPriorityResult{}, nil
}
func TestPriorityValidationAndExactDigest(t *testing.T) {
	r := &priorityRecorder{}
	s := NewTicketPriorityService(r, CryptoIDGenerator{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_actor", WorkspaceID: "wrk_main"}, SourceKind: "web", CorrelationID: "cor_priority"}
	in := TicketPriorityInput{TicketID: "tkt_one", ClientOperationID: "priority", BasePriorityRevision: 1, BaseStatusRevision: 1, Priority: "high", Confirmed: true}
	call := func(v TicketPriorityInput) error {
		_, err := s.SetTicketPriority(context.Background(), inv, v)
		return err
	}
	if err := call(in); err != nil {
		t.Fatal(err)
	}
	first := r.command
	if err := call(in); err != nil || first.PayloadSHA256 != r.command.PayloadSHA256 || first.EventID == r.command.EventID {
		t.Fatal("unstable retry", err)
	}
	for _, mutate := range []func(*TicketPriorityInput){func(v *TicketPriorityInput) { v.BasePriorityRevision++ }, func(v *TicketPriorityInput) { v.BaseStatusRevision++ }, func(v *TicketPriorityInput) { v.Priority = "normal" }} {
		v := in
		mutate(&v)
		if err := call(v); err != nil || r.command.PayloadSHA256 == first.PayloadSHA256 {
			t.Fatal("unbound receipt field", err)
		}
	}
	v := in
	v.ClientOperationID = "other"
	if err := call(v); err != nil || r.command.PayloadSHA256 != first.PayloadSHA256 {
		t.Fatal("operation ID entered payload digest", err)
	}
	for _, mutate := range []func(*TicketPriorityInput){func(v *TicketPriorityInput) { v.BasePriorityRevision = 0 }, func(v *TicketPriorityInput) { v.BaseStatusRevision = MaxTicketStatusRevision + 1 }, func(v *TicketPriorityInput) { v.Confirmed = false }, func(v *TicketPriorityInput) { v.Priority = "" }, func(v *TicketPriorityInput) { v.Priority = "p0" }, func(v *TicketPriorityInput) { v.ClientOperationID = "" }, func(v *TicketPriorityInput) { v.TicketID = "dec_other" }} {
		v := in
		mutate(&v)
		before := r.calls
		if err := call(v); !errors.Is(err, authz.ErrInvalid) || r.calls != before {
			t.Fatal("invalid reached store", err)
		}
	}
	inv.SourceKind = "api"
	if err := call(in); !errors.Is(err, authz.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestPriorityEventStrictShape(t *testing.T) {
	for _, body := range []string{`{"priority":{"from":"unset","to":"high","from_revision":1,"to_revision":2}}`, `{"priority":{"from":"high","to":"unset","from_revision":2,"to_revision":3}}`} {
		if _, err := DecodeTicketPriority([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{"priority":{"to":"high","from_revision":1,"to_revision":2}}`, `{"priority":{"from":"unset","to":"unset","from_revision":1,"to_revision":2}}`, `{"priority":{"from":"unset","to":"high","from_revision":2147483647,"to_revision":2147483648}}`, `{"priority":{"from":"unset","to":"high","from_revision":1,"to_revision":3}}`, `{"priority":{"from":"unset","to":"high","from_revision":1,"to_revision":2,"email":"private"}}`, `{"priority":{"from":"p0","to":"high","from_revision":1,"to_revision":2}}`} {
		if _, err := DecodeTicketPriority([]byte(body)); err == nil {
			t.Fatal("invalid accepted", body)
		}
	}
}
