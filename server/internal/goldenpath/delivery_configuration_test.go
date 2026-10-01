package goldenpath

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestConfigurationPreservesLegacyReceiptDigest(t *testing.T) {
	store := &configurationRecorder{}
	s := NewConfigurationService(store, CryptoIDGenerator{}, SystemClock{})
	in := ConfigurationInput{Kind: "team.create", ScopeID: "wrk_main", ClientOperationID: "old", Name: "Team"}
	_, err := s.Configure(context.Background(), Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_owner", WorkspaceID: "wrk_main"}, SourceKind: "web"}, in)
	if err != nil {
		t.Fatal(err)
	}
	// Golden JSON from ADR-0026, before Delivery was added to the input type.
	old := `{"Kind":"team.create","ScopeID":"wrk_main","UserID":"","ClientOperationID":"old","Name":"Team","Key":"","OwnerTeamID":"","Visibility":"","InitialAdminUserID":"","MemberUserIDs":null,"Role":"","ExpectedRole":null,"ExpectedMember":false}`
	want := sha256.Sum256([]byte(old))
	if store.command.PayloadSHA256 != hex.EncodeToString(want[:]) {
		t.Fatal("legacy receipt digest changed")
	}
}

func TestDeliveryConfigurationRequiresExplicitNarrowAuthority(t *testing.T) {
	store := &configurationRecorder{}
	s := NewConfigurationService(store, CryptoIDGenerator{}, SystemClock{})
	i := Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: "usr_owner", WorkspaceID: "wrk_main"}, SourceKind: "web"}
	valid := ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: "env_stage", UserID: "usr_owner", ClientOperationID: "grant", Delivery: &DeliveryConfigurationInput{Confirmed: true}}
	if _, err := s.Configure(context.Background(), i, valid); err != nil || store.command.AuthorizationID == "" || store.command.ID != "" {
		t.Fatal("explicit self grant", err)
	}
	digest := store.command.PayloadSHA256
	valid.Delivery.ExpectedAuthorization = &ConfigurationAuthorization{ID: "dpa_previous", Status: "revoked"}
	if _, err := s.Configure(context.Background(), i, valid); err != nil || store.command.PayloadSHA256 == digest {
		t.Fatal("expected authorization missing from digest", err)
	}
	for _, change := range []func(*ConfigurationInput){
		func(in *ConfigurationInput) { in.Delivery = nil },
		func(in *ConfigurationInput) { in.Delivery.Confirmed = false },
		func(in *ConfigurationInput) { in.ScopeID = "prj_main" },
		func(in *ConfigurationInput) { in.UserID = "plugin_main" },
		func(in *ConfigurationInput) {
			in.Delivery.ExpectedAuthorization = &ConfigurationAuthorization{ID: "dpa_old", Status: "pending"}
		},
		func(in *ConfigurationInput) { in.Role = "admin" },
	} {
		in := valid
		d := *valid.Delivery
		in.Delivery = &d
		change(&in)
		if _, err := s.Configure(context.Background(), i, in); !errors.Is(err, authz.ErrInvalid) {
			t.Fatal("invalid input accepted", err)
		}
	}
	for _, classification := range []string{"production", "development", "other", ""} {
		in := ConfigurationInput{Kind: "environment.create", ScopeID: "wrk_main", ClientOperationID: "create", Name: "Environment", Key: "stage", OwnerTeamID: "tem_main", Delivery: &DeliveryConfigurationInput{Classification: classification}}
		if _, err := s.Configure(context.Background(), i, in); !errors.Is(err, authz.ErrInvalid) {
			t.Fatal("non-staging creation", classification, err)
		}
	}
	i.Principal.Kind = "plugin"
	if _, err := s.Configure(context.Background(), i, valid); err == nil {
		t.Fatal("plugin configuration accepted")
	}
}
