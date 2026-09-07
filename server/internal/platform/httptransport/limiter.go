package httptransport

import (
	"net/netip"
	"sync"
	"time"
)

type loginWindow struct {
	startedAt time.Time
	attempts  int
}

type LoginGuard struct {
	mu             sync.Mutex
	windows        map[netip.Addr]loginWindow
	attemptLimit   int
	windowDuration time.Duration
	maxClients     int
	passwordSlots  chan struct{}
}

func NewLoginGuard(
	attemptLimit int,
	windowDuration time.Duration,
	maxClients int,
	maxConcurrentPasswords int,
) *LoginGuard {
	if attemptLimit < 1 || windowDuration <= 0 || maxClients < 1 || maxConcurrentPasswords < 1 {
		panic("login guard limits must be positive")
	}
	return &LoginGuard{
		windows:        make(map[netip.Addr]loginWindow),
		attemptLimit:   attemptLimit,
		windowDuration: windowDuration,
		maxClients:     maxClients,
		passwordSlots:  make(chan struct{}, maxConcurrentPasswords),
	}
}

// Begin records the per-client attempt before reserving password work. The
// returned release function must be called after Login finishes.
func (guard *LoginGuard) Begin(clientIP netip.Addr, now time.Time) (func(), time.Duration, error) {
	guard.mu.Lock()
	window, exists := guard.windows[clientIP]
	if exists && !now.Before(window.startedAt.Add(guard.windowDuration)) {
		delete(guard.windows, clientIP)
		exists = false
	}
	if !exists && len(guard.windows) >= guard.maxClients {
		guard.removeExpired(now)
		if len(guard.windows) >= guard.maxClients {
			guard.mu.Unlock()
			return nil, guard.windowDuration, ErrLoginRateLimited
		}
	}
	if !exists {
		window = loginWindow{startedAt: now}
	}
	if window.attempts >= guard.attemptLimit {
		retryAfter := window.startedAt.Add(guard.windowDuration).Sub(now)
		guard.mu.Unlock()
		return nil, retryAfter, ErrLoginRateLimited
	}
	window.attempts++
	guard.windows[clientIP] = window
	guard.mu.Unlock()

	select {
	case guard.passwordSlots <- struct{}{}:
		return func() { <-guard.passwordSlots }, 0, nil
	default:
		return nil, time.Second, ErrLoginRateLimited
	}
}

func (guard *LoginGuard) removeExpired(now time.Time) {
	for address, window := range guard.windows {
		if !now.Before(window.startedAt.Add(guard.windowDuration)) {
			delete(guard.windows, address)
		}
	}
}
