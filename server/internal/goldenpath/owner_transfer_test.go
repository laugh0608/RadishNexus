package goldenpath

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type ownerCommandStore struct {
	command OwnerTransferCommand
	calls   int
}

func (*ownerCommandStore) ReadOwnership(context.Context, OwnerAccess) (Ownership, error) {
	return Ownership{}, nil
}
func (*ownerCommandStore) ListOwnerCandidates(context.Context, OwnerAccess, string, int) (OwnerCandidatePage, error) {
	return OwnerCandidatePage{}, nil
}
func (*ownerCommandStore) ReadOwnerTransfer(context.Context, OwnerAccess, string) (OwnerTransferResult, error) {
	return OwnerTransferResult{}, nil
}
func (s *ownerCommandStore) TransferOwner(_ context.Context, c OwnerTransferCommand) (OwnerTransferResult, error) {
	s.command = c
	s.calls++
	return OwnerTransferResult{}, nil
}
func TestOwnerTransferRevisionAndOperationValidation(t *testing.T) {
	for _, bad := range []string{"", "0", "-1", "+1", "01", "1.0", " 1", "9223372036854775808"} {
		if _, e := ParseRoleRevision(bad); !errors.Is(e, authz.ErrInvalid) {
			t.Fatal(bad, e)
		}
	}
	for _, good := range []string{"1", "9007199254740993", "9223372036854775807"} {
		if _, e := ParseRoleRevision(good); e != nil {
			t.Fatal(good, e)
		}
	}
	store := &ownerCommandStore{}
	s := NewOwnerTransferService(store, CryptoIDGenerator{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_owner", WorkspaceID: "wrk_main"}}
	input := OwnerTransferInput{ClientOperationID: "12345678-1234-4234-8234-123456789abc", TargetUserID: "usr_member", ExpectedActorRoleRevision: "1", ExpectedTargetRoleRevision: "1", Confirmed: true}
	for _, change := range []func(*OwnerTransferInput){func(i *OwnerTransferInput) { i.Confirmed = false }, func(i *OwnerTransferInput) { i.TargetUserID = "usr_owner" }, func(i *OwnerTransferInput) { i.TargetUserID = "wrk_other" }, func(i *OwnerTransferInput) { i.ClientOperationID = "12345678-1234-5234-8234-123456789abc" }, func(i *OwnerTransferInput) { i.ExpectedActorRoleRevision = "01" }} {
		bad := input
		change(&bad)
		if _, e := s.Transfer(context.Background(), inv, "test session", bad); !errors.Is(e, authz.ErrInvalid) {
			t.Fatal(e)
		}
		if store.calls != 0 {
			t.Fatal("invalid write reached store")
		}
	}
	if _, e := s.Transfer(context.Background(), inv, "test session", input); e != nil {
		t.Fatal(e)
	}
	first := store.command
	if first.Access.SessionDigest != sha256.Sum256([]byte("test session")) {
		t.Fatal("missing independent session evidence")
	}
	if _, e := s.Transfer(context.Background(), inv, "another session", input); e != nil {
		t.Fatal(e)
	}
	if store.command.PayloadSHA256 != first.PayloadSHA256 || store.command.Access.SessionDigest == first.Access.SessionDigest {
		t.Fatal("digest bound to ephemeral session")
	}
	changed := input
	changed.TargetUserID = "usr_other"
	_, _ = s.Transfer(context.Background(), inv, "test session", changed)
	if store.command.PayloadSHA256 == first.PayloadSHA256 {
		t.Fatal("target not bound")
	}
	changed = input
	changed.ExpectedActorRoleRevision = "2"
	_, _ = s.Transfer(context.Background(), inv, "test session", changed)
	if store.command.PayloadSHA256 == first.PayloadSHA256 {
		t.Fatal("revision not bound")
	}
	inv.Principal.WorkspaceID = "wrk_other"
	_, _ = s.Transfer(context.Background(), inv, "test session", input)
	if store.command.PayloadSHA256 == first.PayloadSHA256 {
		t.Fatal("workspace not bound")
	}
	inv.Principal.ID = "usr_other"
	_, _ = s.Transfer(context.Background(), inv, "test session", input)
	if store.command.PayloadSHA256 == first.PayloadSHA256 {
		t.Fatal("actor not bound")
	}
}
