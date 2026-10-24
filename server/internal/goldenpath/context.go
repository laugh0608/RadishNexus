package goldenpath

import (
	"context"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type ContextSource struct {
	Imported  bool   `json:"imported"`
	AppliedAt string `json:"applied_at,omitempty"`
}
type ContextStore interface {
	ExportContext(context.Context, OwnerAccess, string) (cp.Bundle, error)
	PrepareContext(context.Context, OwnerAccess, cp.Bundle, string) (cp.Plan, error)
	ApplyContext(context.Context, OwnerAccess, cp.Bundle, cp.Plan, string) (cp.Result, error)
	ContextResult(context.Context, OwnerAccess, string) (cp.Result, error)
	ReadContextSource(context.Context, authz.Principal, entityref.Ref) (ContextSource, error)
}
type ContextService struct{ store ContextStore }

func NewContextService(s ContextStore) *ContextService { return &ContextService{s} }
func (s *ContextService) Export(ctx context.Context, p authz.Principal, token, project string) (cp.Bundle, error) {
	a, e := ownerAccess(p, token)
	if e != nil {
		return cp.Bundle{}, e
	}
	if !cp.ValidID(project, "prj_") {
		return cp.Bundle{}, authz.ErrNotFound
	}
	return s.store.ExportContext(ctx, a, project)
}
func (s *ContextService) Prepare(ctx context.Context, p authz.Principal, token string, b cp.Bundle, op string) (cp.Plan, error) {
	a, e := ownerAccess(p, token)
	if e != nil {
		return cp.Plan{}, e
	}
	return s.store.PrepareContext(ctx, a, b, op)
}
func (s *ContextService) Apply(ctx context.Context, p authz.Principal, token string, b cp.Bundle, plan cp.Plan, confirmed string) (cp.Result, error) {
	a, e := ownerAccess(p, token)
	if e != nil {
		return cp.Result{}, e
	}
	if confirmed == "" || confirmed != plan.PlanDigest {
		return cp.Result{}, authz.ErrConflict
	}
	return s.store.ApplyContext(ctx, a, b, plan, confirmed)
}
func (s *ContextService) Result(ctx context.Context, p authz.Principal, token, op string) (cp.Result, error) {
	a, e := ownerAccess(p, token)
	if e != nil {
		return cp.Result{}, e
	}
	if !cp.ValidOperation(op) {
		return cp.Result{}, authz.ErrNotFound
	}
	return s.store.ContextResult(ctx, a, op)
}
func (s *ContextService) Source(ctx context.Context, p authz.Principal, ref entityref.Ref) (ContextSource, error) {
	if e := p.ValidateUser(); e != nil {
		return ContextSource{}, e
	}
	if e := entityref.M0Registry().Validate(ref); e != nil {
		return ContextSource{}, authz.ErrNotFound
	}
	return s.store.ReadContextSource(ctx, p, ref)
}
