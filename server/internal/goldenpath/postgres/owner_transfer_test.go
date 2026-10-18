package postgres

import (
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"testing"
	"time"
)

func TestOwnerRecentAuthenticationInclusiveBoundary(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		age   time.Duration
		valid bool
	}{{authn.RecentAuthentication - time.Nanosecond, true}, {authn.RecentAuthentication, true}, {authn.RecentAuthentication + time.Nanosecond, false}} {
		if got := ownerRecentlyAuthenticated(now.Add(-test.age), now); got != test.valid {
			t.Fatal(test.age, got)
		}
	}
}
