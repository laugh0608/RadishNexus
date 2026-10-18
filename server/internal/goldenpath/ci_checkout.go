package goldenpath

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const JenkinsCheckoutProfile = "jenkins-git-single-checkout-v1"

// CICheckoutBinding is a deployment-controlled capability, not user input or
// proof that a provider independently authenticated the repository identity.
type CICheckoutBinding struct {
	BindingID          string `json:"binding_id"`
	RepositoryID       string `json:"repository_id"`
	Provider           string `json:"provider"`
	ProviderOrigin     string `json:"provider_origin"`
	ExternalID         string `json:"external_id"`
	Profile            string `json:"profile"`
	EffectiveFromBuild int64  `json:"effective_from_build"`
}

type GitRevision struct {
	Algorithm string `json:"algorithm"`
	OID       string `json:"oid"`
}

type CICheckout struct {
	BindingID string       `json:"binding_id"`
	State     string       `json:"state"`
	Reason    *string      `json:"reason"`
	Revision  *GitRevision `json:"revision"`
}

var checkoutBindingID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
var checkoutOID = regexp.MustCompile(`^[0-9a-f]{40}$`)

func ValidGitRevision(r GitRevision) bool {
	return r.Algorithm == "sha1" && checkoutOID.MatchString(r.OID) && r.OID != strings.Repeat("0", 40)
}

func ValidCheckoutReason(reason string) bool {
	return reason == "checkout_not_observed" || reason == "unsupported_checkout" || reason == "binding_changed_during_run"
}

func NormalizeCICheckoutBinding(b CICheckoutBinding) (CICheckoutBinding, bool) {
	origin, ok := NormalizeRepositoryIdentity(b.Provider, b.ProviderOrigin, b.ExternalID)
	b.ProviderOrigin = origin
	return b, ok && checkoutBindingID.MatchString(b.BindingID) && ValidConfigurationID(b.RepositoryID, "rep_") && b.Profile == JenkinsCheckoutProfile && b.EffectiveFromBuild > 0 && b.EffectiveFromBuild <= 2147483647
}

func ValidCICheckout(c CICheckout) bool {
	if !checkoutBindingID.MatchString(c.BindingID) {
		return false
	}
	switch c.State {
	case "observed":
		return c.Reason == nil && c.Revision != nil && ValidGitRevision(*c.Revision)
	case "unavailable":
		return c.Revision == nil && c.Reason != nil && ValidCheckoutReason(*c.Reason)
	default:
		return false
	}
}

func validateCICheckout(d VerifiedJenkinsDelivery, in RecordCompletedCIRunInput) error {
	if d.CheckoutBinding == nil {
		if in.Checkout != nil {
			return fmt.Errorf("%w: checkout requires a bound source", authz.ErrInvalid)
		}
		return nil
	}
	b, ok := NormalizeCICheckoutBinding(*d.CheckoutBinding)
	n, err := strconv.ParseInt(in.ExternalRunKey, 10, 64)
	if !ok || b != *d.CheckoutBinding || d.Job == "" || len(d.Job) > 255 || strings.TrimSpace(d.Job) != d.Job || err != nil || n < 1 || n > 2147483647 || strconv.FormatInt(n, 10) != in.ExternalRunKey || d.DeliveryID != "build-"+in.ExternalRunKey {
		return fmt.Errorf("%w: invalid checkout source binding", authz.ErrInvalid)
	}
	if (n >= b.EffectiveFromBuild) != (in.Checkout != nil) || (in.Checkout != nil && (!ValidCICheckout(*in.Checkout) || in.Checkout.BindingID != b.BindingID)) {
		return fmt.Errorf("%w: checkout version or binding mismatch", authz.ErrInvalid)
	}
	return nil
}

// CICheckoutProjection carries only current-authorized business facts. A
// restricted projection has no other fields, including no state or reason.
type CICheckoutProjection struct {
	Visibility ProjectionState
	State      string
	Reason     string
	Repository *SubjectProjection
	Revision   *GitRevision
}

type RepositoryCIRunItem struct {
	Ref         entityref.Ref
	Status      string
	CompletedAt time.Time
	Revision    GitRevision
}
type RepositoryCIRunPage struct {
	Items  []RepositoryCIRunItem
	NextID string
}

func (s *Service) ListRepositoryCIRuns(ctx context.Context, p authz.Principal, repository string, in DiscoveryPageInput) (RepositoryCIRunPage, error) {
	if err := validateDiscoveryInput(p, "ci-run", in); err != nil {
		return RepositoryCIRunPage{}, err
	}
	if !ValidConfigurationID(repository, "rep_") {
		return RepositoryCIRunPage{}, authz.ErrInvalid
	}
	return s.store.ListRepositoryCIRuns(ctx, p, repository, in)
}
