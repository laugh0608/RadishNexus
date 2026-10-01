package goldenpath

import (
	"context"
	"fmt"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const MaxDiscoveryPageSize = 50

type DiscoveryPageInput struct {
	AfterID string
	Limit   int
}

type DiscoveryItem struct {
	Ref    entityref.Ref
	Title  string
	Status string
}

type DiscoveryPage struct {
	Items  []DiscoveryItem
	NextID string
}

type DiscoveryStore interface {
	ListProjects(context.Context, authz.Principal, DiscoveryPageInput) (DiscoveryPage, error)
	ListProjectChannels(context.Context, authz.Principal, string, DiscoveryPageInput) (DiscoveryPage, error)
}

type DiscoveryService struct{ store DiscoveryStore }

func NewDiscoveryService(store DiscoveryStore) *DiscoveryService {
	return &DiscoveryService{store: store}
}

func (service *DiscoveryService) ListProjects(ctx context.Context, principal authz.Principal, input DiscoveryPageInput) (DiscoveryPage, error) {
	if err := validateDiscoveryInput(principal, "project", input); err != nil {
		return DiscoveryPage{}, err
	}
	return service.store.ListProjects(ctx, principal, input)
}

func (service *DiscoveryService) ListProjectChannels(ctx context.Context, principal authz.Principal, projectID string, input DiscoveryPageInput) (DiscoveryPage, error) {
	if err := validateDiscoveryInput(principal, "channel", input); err != nil {
		return DiscoveryPage{}, err
	}
	if err := entityref.M0Registry().Validate(entityref.Ref{Type: "project", ID: projectID}); err != nil {
		return DiscoveryPage{}, fmt.Errorf("%w: invalid Project reference", authz.ErrInvalid)
	}
	return service.store.ListProjectChannels(ctx, principal, projectID, input)
}

func validateDiscoveryInput(principal authz.Principal, kind string, input DiscoveryPageInput) error {
	if err := principal.ValidateUser(); err != nil {
		return err
	}
	if input.Limit < 1 || input.Limit > MaxDiscoveryPageSize {
		return fmt.Errorf("%w: invalid discovery page size", authz.ErrInvalid)
	}
	if input.AfterID != "" {
		if err := entityref.M0Registry().Validate(entityref.Ref{Type: kind, ID: input.AfterID}); err != nil {
			return fmt.Errorf("%w: invalid discovery cursor ID", authz.ErrInvalid)
		}
	}
	return nil
}
