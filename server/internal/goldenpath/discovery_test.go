package goldenpath

import (
	"context"
	"errors"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type discoveryStoreStub struct{ calls int }

func (store *discoveryStoreStub) ListProjects(context.Context, authz.Principal, DiscoveryPageInput) (DiscoveryPage, error) {
	store.calls++
	return DiscoveryPage{}, nil
}
func (store *discoveryStoreStub) ListProjectChannels(context.Context, authz.Principal, string, DiscoveryPageInput) (DiscoveryPage, error) {
	store.calls++
	return DiscoveryPage{}, nil
}

func (store *discoveryStoreStub) ListProjectTickets(context.Context, authz.Principal, string, DiscoveryPageInput) (DiscoveryPage, error) {
	store.calls++
	return DiscoveryPage{}, nil
}

func TestDiscoveryValidatesBeforeReading(t *testing.T) {
	principal := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_reader", WorkspaceID: "wrk_main"}
	store := &discoveryStoreStub{}
	service := NewDiscoveryService(store)
	for _, input := range []DiscoveryPageInput{{Limit: 0}, {Limit: 51}, {Limit: 25, AfterID: "chn_other"}} {
		if _, err := service.ListProjects(context.Background(), principal, input); !errors.Is(err, authz.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := service.ListProjects(context.Background(), authz.Principal{}, DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := service.ListProjectChannels(context.Background(), principal, "chn_wrong", DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := service.ListProjectChannels(context.Background(), principal, "prj_main", DiscoveryPageInput{Limit: 25, AfterID: "prj_wrong"}); !errors.Is(err, authz.ErrInvalid) {
		t.Fatal(err)
	}
	if store.calls != 0 {
		t.Fatal("invalid request reached store")
	}
	for _, input := range []DiscoveryPageInput{{Limit: 0}, {Limit: 51}, {Limit: 25, AfterID: "chn_other"}} {
		if _, err := service.ListProjectTickets(context.Background(), principal, "prj_main", input); !errors.Is(err, authz.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := service.ListProjectTickets(context.Background(), principal, "tkt_wrong", DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := service.ListProjectTickets(context.Background(), authz.Principal{}, "prj_main", DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if store.calls != 0 {
		t.Fatal("invalid Ticket request reached store")
	}
}

func (store *discoveryStoreStub) ListProjectDecisions(context.Context, authz.Principal, string, DiscoveryPageInput) (DiscoveryPage, error) {
	store.calls++
	return DiscoveryPage{}, nil
}

func TestDecisionDiscoveryValidatesBeforeReading(t *testing.T) {
	principal := authz.Principal{Kind: authz.PrincipalUser, ID: "usr_reader", WorkspaceID: "wrk_main"}
	store := &discoveryStoreStub{}
	service := NewDiscoveryService(store)
	for _, input := range []DiscoveryPageInput{{Limit: -1}, {Limit: 0}, {Limit: 51}, {Limit: 25, AfterID: "chn_other"}, {Limit: 25, AfterID: "dec_invalid/path"}} {
		if _, err := service.ListProjectDecisions(context.Background(), principal, "prj_main", input); !errors.Is(err, authz.ErrInvalid) {
			t.Fatal(input, err)
		}
	}
	for _, project := range []string{"", "tkt_wrong", "prj_invalid/path"} {
		if _, err := service.ListProjectDecisions(context.Background(), principal, project, DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrInvalid) {
			t.Fatal(project, err)
		}
	}
	if _, err := service.ListProjectDecisions(context.Background(), authz.Principal{}, "prj_main", DiscoveryPageInput{Limit: 25}); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if store.calls != 0 {
		t.Fatal("invalid Decision request reached store")
	}
	for _, input := range []DiscoveryPageInput{{Limit: 1}, {Limit: 50, AfterID: "dec_missing"}} {
		if _, err := service.ListProjectDecisions(context.Background(), principal, "prj_main", input); err != nil {
			t.Fatal(input, err)
		}
	}
	if store.calls != 2 {
		t.Fatal("valid Decision request did not reach store")
	}
}
