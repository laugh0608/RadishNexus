package goldenpath

import (
	"context"
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"testing"
)

type assignmentRecorder struct {
	TicketAssignmentStore
	command TicketAssignmentCommand
	calls   int
}

func (r *assignmentRecorder) SetTicketAssignee(_ context.Context, c TicketAssignmentCommand) (TicketAssignmentResult, error) {
	r.command = c
	r.calls++
	return TicketAssignmentResult{}, nil
}
func TestAssignmentValidationAndExactDigest(t *testing.T) {
	r := &assignmentRecorder{}
	s := NewTicketAssignmentService(r, CryptoIDGenerator{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_actor", WorkspaceID: "wrk_main"}, SourceKind: "web", CorrelationID: "cor_assignment"}
	target := "usr_target"
	in := TicketAssignmentInput{TicketID: "tkt_one", ClientOperationID: "assign", BaseAssignmentRevision: 1, BaseStatusRevision: 1, AssigneeUserID: &target, TargetLifecycleRevision: "9223372036854775807", ProjectManagementRevision: "1", Confirmed: true}
	call := func(v TicketAssignmentInput) error {
		_, err := s.SetTicketAssignee(context.Background(), inv, v)
		return err
	}
	if err := call(in); err != nil {
		t.Fatal(err)
	}
	first := r.command
	if err := call(in); err != nil || first.PayloadSHA256 != r.command.PayloadSHA256 || first.EventID == r.command.EventID {
		t.Fatal("unstable retry", err)
	}
	for _, mutate := range []func(*TicketAssignmentInput){func(v *TicketAssignmentInput) { v.BaseAssignmentRevision++ }, func(v *TicketAssignmentInput) { v.BaseStatusRevision++ }, func(v *TicketAssignmentInput) { v.TargetLifecycleRevision = "1" }, func(v *TicketAssignmentInput) { v.ProjectManagementRevision = "2" }, func(v *TicketAssignmentInput) { v.AssigneeUserID = nil; v.TargetLifecycleRevision = "0" }} {
		v := in
		mutate(&v)
		if err := call(v); err != nil || r.command.PayloadSHA256 == first.PayloadSHA256 {
			t.Fatal("unbound receipt field", err)
		}
	}
	for _, mutate := range []func(*TicketAssignmentInput){func(v *TicketAssignmentInput) { v.BaseAssignmentRevision = 0 }, func(v *TicketAssignmentInput) { v.BaseStatusRevision = MaxTicketStatusRevision + 1 }, func(v *TicketAssignmentInput) { v.Confirmed = false }, func(v *TicketAssignmentInput) { v.AssigneeUserID = nil }, func(v *TicketAssignmentInput) { v.TargetLifecycleRevision = "0" }, func(v *TicketAssignmentInput) { v.TargetLifecycleRevision = "01" }, func(v *TicketAssignmentInput) { v.ProjectManagementRevision = "+1" }, func(v *TicketAssignmentInput) { v.ProjectManagementRevision = "9223372036854775808" }, func(v *TicketAssignmentInput) { v.ClientOperationID = "" }} {
		v := in
		mutate(&v)
		before := r.calls
		if err := call(v); !errors.Is(err, authz.ErrInvalid) || r.calls != before {
			t.Fatal("invalid input reached store", err)
		}
	}
	inv.SourceKind = "api"
	if err := call(in); !errors.Is(err, authz.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestAssignmentEventStrictShape(t *testing.T) {
	for _, body := range []string{`{"assignment":{"from_user_id":null,"to_user_id":"usr_a","from_revision":1,"to_revision":2}}`, `{"assignment":{"from_user_id":"usr_a","to_user_id":null,"from_revision":2,"to_revision":3}}`} {
		if _, err := DecodeTicketAssignment([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{"assignment":{"to_user_id":"usr_a","from_revision":1,"to_revision":2}}`, `{"assignment":{"from_user_id":null,"to_user_id":null,"from_revision":1,"to_revision":2}}`, `{"assignment":{"from_user_id":null,"to_user_id":"usr_a","from_revision":2147483647,"to_revision":2147483648}}`, `{"assignment":{"from_user_id":"usr_a","to_user_id":"usr_a","from_revision":1,"to_revision":2}}`, `{"assignment":{"from_user_id":null,"to_user_id":"usr_a","from_revision":1,"to_revision":3}}`, `{"assignment":{"from_user_id":null,"to_user_id":"usr_a","from_revision":1,"to_revision":2,"email":"private"}}`} {
		if _, err := DecodeTicketAssignment([]byte(body)); err == nil {
			t.Fatal("invalid event accepted", body)
		}
	}
}
