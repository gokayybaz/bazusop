// Package ratelimit provides a minimal, process-local sliding-window rate
// limiter for brute-force protection on unauthenticated, high-value
// endpoints (login, MFA confirmation, invite consumption — see the design
// spec's "Rate limiting" requirement). It is deliberately NOT synchronized
// across hub replicas: see docs/OPERATIONS.md for why this is an accepted
// v1 trade-off rather than a full distributed limiter.
package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
	now      func() time.Time
}

type Option func(*Limiter)

func WithClock(clock func() time.Time) Option { return func(limiter *Limiter) { limiter.now = clock } }

func New(max int, window time.Duration, options ...Option) *Limiter {
	limiter := &Limiter{attempts: make(map[string][]time.Time), max: max, window: window, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(limiter)
	}
	return limiter
}

// Allow reports whether key may proceed. Expired attempts for key are
// always pruned first; a new attempt is recorded only when this call
// returns true, so a caller spamming a blocked key does not keep resetting
// its own window.
func (limiter *Limiter) Allow(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	cutoff := now.Add(-limiter.window)
	kept := limiter.attempts[key][:0]
	for _, at := range limiter.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= limiter.max {
		limiter.attempts[key] = kept
		return false
	}
	limiter.attempts[key] = append(kept, now)
	return true
}
