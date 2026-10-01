package goldenpath

import (
	"context"
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"testing"
)

type configurationRecorder struct {
	command ConfigurationCommand
	calls   int
}

func (s *configurationRecorder) Configure(_ context.Context, c ConfigurationCommand) (ConfigurationResult, error) {
	s.command = c
	s.calls++
	return ConfigurationResult{}, nil
}
func (s *configurationRecorder) ReadConfiguration(context.Context, authz.Principal, string, string) (ConfigurationObject, error) {
	return ConfigurationObject{}, nil
}
func (s *configurationRecorder) ListConfiguration(context.Context, authz.Principal, ConfigurationQuery) (ConfigurationPage, error) {
	return ConfigurationPage{}, nil
}
func TestConfigurationCanonicalMembersAndExplicitAuthority(t *testing.T) {
	store := &configurationRecorder{}
	s := NewConfigurationService(store, CryptoIDGenerator{}, SystemClock{})
	invocation := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_owner", WorkspaceID: "wrk_main"}, SourceKind: "web"}
	input := ConfigurationInput{Kind: "channel.create", ScopeID: "prj_main", ClientOperationID: "create", Name: " Private ", Visibility: "restricted", MemberUserIDs: []string{"usr_owner", "usr_member"}}
	if _, e := s.Configure(context.Background(), invocation, input); e != nil {
		t.Fatal(e)
	}
	digest := store.command.PayloadSHA256
	input.MemberUserIDs = []string{"usr_member", "usr_owner"}
	input.Name = "Private"
	if _, e := s.Configure(context.Background(), invocation, input); e != nil || digest != store.command.PayloadSHA256 {
		t.Fatal("noncanonical member digest", e)
	}
	invalid := []ConfigurationInput{
		{Kind: "channel.create", ScopeID: "prj_main", ClientOperationID: "a", Name: "Private", Visibility: "restricted", MemberUserIDs: []string{"usr_member"}},
		{Kind: "channel.create", ScopeID: "prj_main", ClientOperationID: "a", Name: "Private", Visibility: "restricted", MemberUserIDs: []string{"usr_owner", "usr_owner"}},
		{Kind: "project.create", ScopeID: "wrk_main", ClientOperationID: "a", Name: "Project", Key: "project", OwnerTeamID: "tem_main", Visibility: "restricted"},
		{Kind: "project.member.set", ScopeID: "prj_main", UserID: "usr_member", ClientOperationID: "a", Role: "admin"},
	}
	for _, in := range invalid {
		if _, e := s.Configure(context.Background(), invocation, in); !errors.Is(e, authz.ErrInvalid) {
			t.Fatal("invalid command accepted", in.Kind, e)
		}
	}
	if store.calls != 2 {
		t.Fatal("invalid input reached store")
	}
}
