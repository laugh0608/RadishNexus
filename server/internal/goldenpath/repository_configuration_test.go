package goldenpath

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func repositoryTestMetadata() RepositoryMetadata {
	return RepositoryMetadata{Provider: "gitea", ProviderOrigin: "https://git.example.test", ExternalID: "123", WebURL: "https://git.example.test/team/service", DefaultBranch: "main"}
}

func TestRepositoryMappingCanonicalizationAndUnsafeInput(t *testing.T) {
	m := repositoryTestMetadata()
	m.ProviderOrigin, m.WebURL = "HTTPS://GIT.example.test:443/", "https://GIT.example.test:443/team/%73ervice"
	normalized, ok := NormalizeRepositoryMetadata(m)
	if !ok || normalized != repositoryTestMetadata() {
		t.Fatalf("canonical mapping = %#v, %v", normalized, ok)
	}
	for _, field := range []string{"origin", "url"} {
		for _, value := range []string{"http://git.example.test/team/service", "https://user:pass@git.example.test/team/service", "https://git.example.test/team/service?token=x", "https://git.example.test/team/service#x", "https://git.example.test/team/service?", "https://git.example.test/team/service#", "//git.example.test/team/service", "https://git.example.test.:443/team/service", "https://git.example.test:0/team/service", "https://git.example.test:65536/team/service", "https://git.example.test:0443/team/service", "https://git.example.test:/team/service", "https://git.example.test/te\nam/service", "https://git.example.test/team\\service", "https://例子.test/team/service"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				in := repositoryTestMetadata()
				if field == "origin" {
					in.ProviderOrigin = value
				} else {
					in.WebURL = value
				}
				if _, ok := NormalizeRepositoryMetadata(in); ok {
					t.Fatal("unsafe URL accepted")
				}
			})
		}
	}
	for _, path := range []string{"/", "/team//service", "/team/../service", "/team/%2e%2e/service", "/team%2fservice", "/team%5cservice", "/team/%252e/service", "/team/%00service", "/team/%20service", "/team/%3fservice"} {
		in := repositoryTestMetadata()
		in.WebURL = in.ProviderOrigin + path
		if _, ok := NormalizeRepositoryMetadata(in); ok {
			t.Errorf("unsafe path accepted: %s", path)
		}
	}
	for _, branch := range []string{"", "refs//heads/main", "topic..next", "topic/.hidden", "topic.lock", "topic/next.", "-main", "main/", "main@{1}", "中文", strings.Repeat("a", 256)} {
		in := repositoryTestMetadata()
		in.DefaultBranch = branch
		if _, ok := NormalizeRepositoryMetadata(in); ok {
			t.Errorf("invalid branch accepted: %s", branch)
		}
	}
	for _, change := range []func(*RepositoryMetadata){
		func(m *RepositoryMetadata) { m.Provider = "unknown" },
		func(m *RepositoryMetadata) { m.ExternalID = " 123" },
		func(m *RepositoryMetadata) { m.ExternalID = "12\u20033" },
		func(m *RepositoryMetadata) { m.ExternalID = "" },
		func(m *RepositoryMetadata) { m.ExternalID = strings.Repeat("x", 256) },
		func(m *RepositoryMetadata) { m.ProviderOrigin += "/instance" },
		func(m *RepositoryMetadata) { m.WebURL = "https://other.example.test/team/service" },
		func(m *RepositoryMetadata) { m.WebURL = "https://git.example.test:8443/team/service" },
	} {
		in := repositoryTestMetadata()
		change(&in)
		if _, ok := NormalizeRepositoryMetadata(in); ok {
			t.Errorf("invalid mapping accepted: %#v", in)
		}
	}
	for _, branch := range []string{"main", "release/2026.10", "fix_a-1"} {
		in := repositoryTestMetadata()
		in.DefaultBranch = branch
		if _, ok := NormalizeRepositoryMetadata(in); !ok {
			t.Errorf("valid branch rejected: %s", branch)
		}
	}
	for _, host := range []string{"127.1", "2130706433", "0x7f000001", "0177.0.0.1", "[git.example.test]"} {
		in := repositoryTestMetadata()
		in.ProviderOrigin = "https://" + host
		in.WebURL = in.ProviderOrigin + "/team/service"
		if _, ok := NormalizeRepositoryMetadata(in); ok {
			t.Errorf("ambiguous browser host accepted: %s", host)
		}
	}
	// Opaque external identities remain case-sensitive and retain leading zeros.
	for _, identity := range []string{"00123", "ABC", "abc"} {
		in := repositoryTestMetadata()
		in.ExternalID = identity
		if got, ok := NormalizeRepositoryMetadata(in); !ok || got.ExternalID != identity {
			t.Fatal("identity rewritten")
		}
	}
}

func TestRepositoryCommandsPreserveIdentityAndRequireConfirmation(t *testing.T) {
	store := &configurationRecorder{}
	s := NewConfigurationService(store, CryptoIDGenerator{}, SystemClock{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_owner", WorkspaceID: "wrk_main"}, SourceKind: "web"}
	in := ConfigurationInput{Kind: "repository.create", ScopeID: "wrk_main", ClientOperationID: "create", Name: " Service ", Repository: &RepositoryConfigurationInput{RepositoryMetadata: repositoryTestMetadata()}}
	in.Repository.ProviderOrigin = "HTTPS://GIT.example.test:443/"
	if _, err := s.Configure(context.Background(), inv, in); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(store.command.ID, "rep_") || store.command.EventID == "" || store.command.SubjectID() != "" {
		t.Fatal("mapping identity", store.command)
	}
	if in.Repository.ProviderOrigin != "HTTPS://GIT.example.test:443/" {
		t.Fatal("caller input mutated")
	}
	digest := store.command.PayloadSHA256
	in.Repository.ProviderOrigin, in.Name = "https://git.example.test", "Service"
	if _, err := s.Configure(context.Background(), inv, in); err != nil || store.command.PayloadSHA256 != digest {
		t.Fatal("canonical digest", err)
	}
	link := ConfigurationInput{Kind: "component.repository.link", ScopeID: "cmp_main", ClientOperationID: "link", Repository: &RepositoryConfigurationInput{RepositoryID: "rep_main", Confirmed: true}}
	if _, err := s.Configure(context.Background(), inv, link); err != nil || !strings.HasPrefix(store.command.ID, "lnk_") || store.command.SubjectID() != "rep_main" || store.command.EventID == "" {
		t.Fatal("link command", err)
	}
	unlink := ConfigurationInput{Kind: "component.repository.unlink", ScopeID: "cmp_main", ClientOperationID: "unlink", Repository: &RepositoryConfigurationInput{LinkID: "lnk_old", Confirmed: true}}
	if _, err := s.Configure(context.Background(), inv, unlink); err != nil || store.command.ID != "" || store.command.SubjectID() != "lnk_old" || store.command.EventID == "" {
		t.Fatal("unlink command", err)
	}
	for _, change := range []func(*ConfigurationInput){
		func(i *ConfigurationInput) { i.Repository = nil },
		func(i *ConfigurationInput) { i.Repository.Confirmed = false },
		func(i *ConfigurationInput) { i.Repository.LinkID = "lnk_wrong" },
		func(i *ConfigurationInput) { i.Repository.Provider = "github" },
		func(i *ConfigurationInput) { i.Repository.RepositoryID = "cmp_wrong" },
		func(i *ConfigurationInput) { i.UserID = "usr_injected" },
		func(i *ConfigurationInput) { i.Delivery = &DeliveryConfigurationInput{} },
		func(i *ConfigurationInput) { i.ScopeID = "prj_wrong" },
		func(i *ConfigurationInput) { i.Name = "hidden" },
	} {
		bad := link
		r := *link.Repository
		bad.Repository = &r
		change(&bad)
		if _, err := s.Configure(context.Background(), inv, bad); !errors.Is(err, authz.ErrInvalid) {
			t.Fatal("invalid command accepted", err)
		}
	}
	if store.calls != 4 {
		t.Fatal("invalid input reached store", store.calls)
	}
	inv.Principal.Kind = "plugin"
	if _, err := s.Configure(context.Background(), inv, in); err == nil {
		t.Fatal("plugin created repository")
	}
}

func TestRepositoryAdditionKeepsAllExistingCommandDigestEncodings(t *testing.T) {
	// This is the exact pre-ADR-0033 input wire shape, kept independent of the
	// current struct so adding a zero-valued field cannot rewrite old receipts.
	type legacyInput struct {
		Kind, ScopeID, UserID, ClientOperationID, Name, Key, OwnerTeamID, Visibility, InitialAdminUserID string
		MemberUserIDs                                                                                    []string
		Role                                                                                             string
		ExpectedRole                                                                                     *string
		ExpectedMember                                                                                   bool
		Delivery                                                                                         *DeliveryConfigurationInput `json:",omitempty"`
	}
	inputs := []ConfigurationInput{
		{Kind: "team.create", ScopeID: "wrk_main", Name: "Team"},
		{Kind: "project.create", ScopeID: "wrk_main", Name: "Project", Key: "project", OwnerTeamID: "tem_main", Visibility: "workspace", InitialAdminUserID: "usr_owner"},
		{Kind: "channel.create", ScopeID: "prj_main", Name: "Channel", Visibility: "project", MemberUserIDs: []string{}},
		{Kind: "project.member.set", ScopeID: "prj_main", UserID: "usr_member", Role: "viewer"},
		{Kind: "project.member.remove", ScopeID: "prj_main", UserID: "usr_member"},
		{Kind: "channel.member.add", ScopeID: "chn_main", UserID: "usr_member"},
		{Kind: "channel.member.remove", ScopeID: "chn_main", UserID: "usr_member", ExpectedMember: true},
		{Kind: "component.create", ScopeID: "wrk_main", Name: "Service", Key: "service", OwnerTeamID: "tem_main", Delivery: &DeliveryConfigurationInput{Type: "service"}},
		{Kind: "environment.create", ScopeID: "wrk_main", Name: "Stage", Key: "stage", OwnerTeamID: "tem_main", Delivery: &DeliveryConfigurationInput{Classification: "staging"}},
		{Kind: "environment.authorization.grant", ScopeID: "env_stage", UserID: "usr_member", Delivery: &DeliveryConfigurationInput{Confirmed: true}},
		{Kind: "environment.authorization.revoke", ScopeID: "env_stage", UserID: "usr_member", Delivery: &DeliveryConfigurationInput{Confirmed: true, ExpectedAuthorization: &ConfigurationAuthorization{ID: "dpa_old", Status: "active"}}},
	}
	store := &configurationRecorder{}
	s := NewConfigurationService(store, CryptoIDGenerator{}, SystemClock{})
	inv := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_owner", WorkspaceID: "wrk_main"}}
	for _, in := range inputs {
		in.ClientOperationID = "legacy"
		old := legacyInput{in.Kind, in.ScopeID, in.UserID, in.ClientOperationID, in.Name, in.Key, in.OwnerTeamID, in.Visibility, in.InitialAdminUserID, in.MemberUserIDs, in.Role, in.ExpectedRole, in.ExpectedMember, in.Delivery}
		body, err := json.Marshal(old)
		if err != nil {
			t.Fatal(err)
		}
		want := sha256.Sum256(body)
		if _, err := s.Configure(context.Background(), inv, in); err != nil || store.command.PayloadSHA256 != hex.EncodeToString(want[:]) {
			t.Fatal(in.Kind, "old digest drift", err)
		}
	}
}
