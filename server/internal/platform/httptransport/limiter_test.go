package httptransport

import (
	"errors"
	"net/netip"
	"testing"
	"time"
)

func TestLoginGuardLimitsAttemptsPerClientWindow(t *testing.T) {
	t.Parallel()
	guard := NewLoginGuard(2, time.Minute, 8, 2)
	address := netip.MustParseAddr("203.0.113.7")
	now := time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)
	for attempt := 0; attempt < 2; attempt++ {
		release, _, err := guard.Begin(address, now)
		if err != nil {
			t.Fatalf("Begin() attempt %d error = %v", attempt+1, err)
		}
		release()
	}
	if _, retryAfter, err := guard.Begin(address, now); !errors.Is(err, ErrLoginRateLimited) || retryAfter != time.Minute {
		t.Fatalf("Begin() limited = retry %s, error %v", retryAfter, err)
	}
	release, _, err := guard.Begin(address, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Begin() next window error = %v", err)
	}
	release()
}

func TestLoginGuardCapsConcurrentPasswordWork(t *testing.T) {
	t.Parallel()
	guard := NewLoginGuard(5, time.Minute, 8, 1)
	now := time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)
	release, _, err := guard.Begin(netip.MustParseAddr("203.0.113.1"), now)
	if err != nil {
		t.Fatalf("Begin() first error = %v", err)
	}
	if _, retryAfter, err := guard.Begin(netip.MustParseAddr("203.0.113.2"), now); !errors.Is(err, ErrLoginRateLimited) || retryAfter != time.Second {
		t.Fatalf("Begin() concurrent = retry %s, error %v", retryAfter, err)
	}
	release()
}

func TestLoginGuardBoundsTrackedClientsAndExpiresEntries(t *testing.T) {
	t.Parallel()
	guard := NewLoginGuard(5, time.Minute, 1, 1)
	now := time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)
	release, _, err := guard.Begin(netip.MustParseAddr("203.0.113.1"), now)
	if err != nil {
		t.Fatalf("Begin() first error = %v", err)
	}
	release()
	if _, _, err := guard.Begin(netip.MustParseAddr("203.0.113.2"), now); !errors.Is(err, ErrLoginRateLimited) {
		t.Fatalf("Begin() full error = %v", err)
	}
	release, _, err = guard.Begin(netip.MustParseAddr("203.0.113.2"), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Begin() after expiry error = %v", err)
	}
	release()
}
