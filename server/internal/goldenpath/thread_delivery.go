package goldenpath

import (
	"context"
	"time"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const (
	ThreadDeliveryTickets     = "thread-delivery-tickets"
	ThreadDeliveryRuns        = "thread-delivery-runs"
	ThreadDeliveryDeployments = "thread-delivery-deployments"

	DeliveryDecisionOrigin = "decision-origin"
	DeliveryMessageOrigin  = "message-origin"
	DeliveryDiscussion     = "execution-discussion"
)

type ThreadDeliveryScope struct {
	ThreadID string
	TicketID string
	RunID    string
}

func (scope ThreadDeliveryScope) Kind() string {
	switch {
	case scope.RunID != "":
		return ThreadDeliveryDeployments
	case scope.TicketID != "":
		return ThreadDeliveryRuns
	default:
		return ThreadDeliveryTickets
	}
}

type ThreadDeliveryItem struct {
	Ref         entityref.Ref
	Title       string
	Status      string
	Origins     []string
	CompletedAt *time.Time
	Component   *TraceEntity
	Environment *TraceEntity
}

type ThreadDeliveryPage struct {
	Items  []ThreadDeliveryItem
	NextID string
}

type ThreadDeliveryStore interface {
	ListThreadDelivery(context.Context, authz.Principal, ThreadDeliveryScope, DiscoveryPageInput) (ThreadDeliveryPage, error)
}

type ThreadDeliveryService struct {
	store ThreadDeliveryStore
}

func NewThreadDeliveryService(store ThreadDeliveryStore) *ThreadDeliveryService {
	return &ThreadDeliveryService{store: store}
}

func (s *ThreadDeliveryService) List(ctx context.Context, p authz.Principal, scope ThreadDeliveryScope, in DiscoveryPageInput) (ThreadDeliveryPage, error) {
	if !cp.ValidID(scope.ThreadID, "thr_") || (scope.TicketID != "" && !cp.ValidID(scope.TicketID, "tkt_")) ||
		(scope.RunID != "" && (scope.TicketID == "" || !cp.ValidID(scope.RunID, "cir_"))) ||
		in.TicketFilters != (TicketFilters{}) || in.IncludeDueDate || in.IncludeClassification {
		return ThreadDeliveryPage{}, authz.ErrInvalid
	}
	target, prefix := "ticket", "tkt_"
	switch scope.Kind() {
	case ThreadDeliveryRuns:
		target, prefix = "ci-run", "cir_"
	case ThreadDeliveryDeployments:
		target, prefix = "deployment", "dpl_"
	}
	if err := validateDiscoveryInput(p, target, in); err != nil {
		return ThreadDeliveryPage{}, err
	}
	if in.AfterID != "" && !cp.ValidID(in.AfterID, prefix) {
		return ThreadDeliveryPage{}, authz.ErrInvalid
	}
	return s.store.ListThreadDelivery(ctx, p, scope, in)
}
