package goldenpath

import (
	"context"
	"encoding/hex"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type NexusRelationBoundary struct {
	Target     entityref.Ref
	Generation string
}

type NexusRelationPageInput struct {
	Limit int
	After *NexusRelationBoundary
}

type NexusRelationPage struct {
	Items []RelationProjection
	Next  *NexusRelationBoundary
}

func NexusRelationDirection(sourceKind, targetKind string) (direction, relation string) {
	switch {
	case sourceKind == "thread" && targetKind == "decision":
		return "incoming", "derived-from"
	case sourceKind == "decision" && targetKind == "ticket":
		return "incoming", "implements"
	case sourceKind == "ticket" && targetKind == "document":
		return "outgoing", "relates-to"
	case sourceKind == "ticket" && targetKind == "component":
		return "outgoing", "affects"
	default:
		return "", ""
	}
}

func ValidNexusRelationSource(source entityref.Ref) bool {
	switch source.Type {
	case "thread":
		return cp.ValidID(source.ID, "thr_")
	case "decision":
		return cp.ValidID(source.ID, "dec_")
	case "ticket":
		return cp.ValidID(source.ID, "tkt_")
	default:
		return false
	}
}

func ValidNexusRelationBoundary(sourceKind string, after NexusRelationBoundary) bool {
	direction, _ := NexusRelationDirection(sourceKind, after.Target.Type)
	if direction == "" || entityref.M0Registry().Validate(after.Target) != nil {
		return false
	}
	digest, err := hex.DecodeString(after.Generation)
	return len(digest) == 32 && err == nil && hex.EncodeToString(digest) == after.Generation
}

func (service *Service) GetNexusSummary(ctx context.Context, p authz.Principal, target entityref.Ref) (NexusView, error) {
	if err := p.ValidateUser(); err != nil {
		return NexusView{}, err
	}
	if !ValidNexusRelationSource(target) {
		return NexusView{}, authz.ErrInvalid
	}
	return service.store.GetNexusSummary(ctx, p, target)
}

func (service *Service) ListNexusRelations(ctx context.Context, p authz.Principal, target entityref.Ref, input NexusRelationPageInput) (NexusRelationPage, error) {
	if err := p.ValidateUser(); err != nil {
		return NexusRelationPage{}, err
	}
	if !ValidNexusRelationSource(target) || input.Limit < 1 || input.Limit > MaxDiscoveryPageSize ||
		(input.After != nil && !ValidNexusRelationBoundary(target.Type, *input.After)) {
		return NexusRelationPage{}, authz.ErrInvalid
	}
	return service.store.ListNexusRelations(ctx, p, target, input)
}
