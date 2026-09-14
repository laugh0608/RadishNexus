package authn

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"testing"
)

type setupTestStore struct {
	exists bool
	err    error
	calls  int
}

func (s *setupTestStore) HasAccounts(context.Context) (bool, error) { return s.exists, s.err }
func (s *setupTestStore) Bootstrap(context.Context, BootstrapInput) (BootstrapResult, error) {
	s.calls++
	s.exists = true
	return BootstrapResult{}, s.err
}
func TestSetupProofAndPersistentClosure(t *testing.T) {
	ctx := context.Background()
	code := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	store := &setupTestStore{}
	s, err := NewSetupService(store, store, code)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := s.Status(ctx); err != nil || status != "required" {
		t.Fatal(status, err)
	}
	if err := s.Complete(ctx, "incorrect", BootstrapInput{}); !errors.Is(err, authz.ErrForbidden) || store.calls != 0 {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, code, BootstrapInput{}); err != nil || store.calls != 1 {
		t.Fatal(err)
	}
	restarted, _ := NewSetupService(store, store, "")
	if status, err := restarted.Status(ctx); err != nil || status != "complete" {
		t.Fatal(status, err)
	}
	if err := s.Complete(ctx, code, BootstrapInput{}); !errors.Is(err, ErrAlreadyBootstrapped) || store.calls != 1 {
		t.Fatal(err)
	}
	store.exists = false
	if status, _ := restarted.Status(ctx); status != "unavailable" {
		t.Fatal(status)
	}
	if err := restarted.Complete(ctx, "", BootstrapInput{}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal(err)
	}
	store.err = errors.New("database failure")
	if status, err := s.Status(ctx); err == nil || status != "" {
		t.Fatal("failure treated as empty")
	}
	if err := s.Complete(ctx, code, BootstrapInput{}); err == nil || store.calls != 1 {
		t.Fatal("failure reached bootstrap")
	}
	for _, invalid := range []string{"default", code + "=", code[:42] + "B"} {
		if _, err := NewSetupService(store, store, invalid); err == nil {
			t.Fatal("accepted invalid encoding")
		}
	}
}
