package authn

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type SetupStore interface {
	HasAccounts(context.Context) (bool, error)
}
type Bootstrapper interface {
	Bootstrap(context.Context, BootstrapInput) (BootstrapResult, error)
}

// SetupService retains only a digest of the deployment proof. Account existence,
// never the lifetime of this process or its configured code, closes setup.
type SetupService struct {
	bootstrap  Bootstrapper
	store      SetupStore
	configured bool
	digest     [sha256.Size]byte
}

func NewSetupService(bootstrap Bootstrapper, store SetupStore, code string) (*SetupService, error) {
	if code != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(code)
		if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != code {
			return nil, errors.New("setup code must encode 32 random bytes as canonical unpadded base64url")
		}
	}
	return &SetupService{bootstrap: bootstrap, store: store, configured: code != "", digest: sha256.Sum256([]byte(code))}, nil
}

func (s *SetupService) Status(ctx context.Context) (string, error) {
	exists, err := s.store.HasAccounts(ctx)
	if err != nil {
		return "", err
	}
	if exists {
		return "complete", nil
	}
	if !s.configured {
		return "unavailable", nil
	}
	return "required", nil
}

func (s *SetupService) Complete(ctx context.Context, code string, input BootstrapInput) error {
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if status == "complete" {
		return ErrAlreadyBootstrapped
	}
	actual := sha256.Sum256([]byte(code))
	if !s.configured || len(code) != 43 || subtle.ConstantTimeCompare(actual[:], s.digest[:]) != 1 {
		return authz.ErrForbidden
	}
	// Bootstrap serializes CLI and Web contenders in the same database transaction.
	_, err = s.bootstrap.Bootstrap(ctx, input)
	return err
}
