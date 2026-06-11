// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package dpop

import (
	"sync"
	"time"
)

// Clock is the time source used by the verifier and replay cache. Production
// uses SystemClock; tests inject a deterministic clock so iat/skew and TTL
// expiry are exercised without sleeping.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
}

// SystemClock is the default wall-clock implementation of Clock.
type SystemClock struct{}

// Now returns time.Now().
func (SystemClock) Now() time.Time { return time.Now() }

// ReplayCache records DPoP proof jti values for the duration of a proof's
// validity so a captured proof cannot be replayed. Implementations must be
// safe for concurrent use. The interface allows swapping the in-memory default
// for a distributed (e.g. Raft- or Redis-backed) cache in clustered
// deployments without touching the verifier.
type ReplayCache interface {
	// SeenBefore atomically records jti as seen until expiresAt and reports
	// whether it had already been recorded (and not yet expired). A true
	// return means the proof is a replay and must be rejected.
	SeenBefore(jti string, expiresAt time.Time) bool
}

// DefaultReplayCacheMaxEntries bounds the replay window when no explicit cap is
// supplied. Expired jti entries are already swept on every access, but within a
// single proof-lifetime an attacker could flood distinct jtis; the cap keeps the
// map bounded even then.
const DefaultReplayCacheMaxEntries = 100000

// MemoryReplayCache is an in-memory, TTL- and size-bounded ReplayCache. Every
// access sweeps all expired entries, and inserts that would exceed the size cap
// trigger an opportunistic eviction, so the map cannot grow without limit under
// sustained load.
type MemoryReplayCache struct {
	clock      Clock
	maxEntries int
	mu         sync.Mutex
	seen       map[string]time.Time
}

// MemoryReplayCacheOption customizes a MemoryReplayCache at construction.
type MemoryReplayCacheOption func(*MemoryReplayCache)

// WithReplayCacheMaxEntries caps the number of recorded jti entries. A value
// <= 0 is ignored and the default cap applies.
func WithReplayCacheMaxEntries(max int) MemoryReplayCacheOption {
	return func(c *MemoryReplayCache) {
		if max > 0 {
			c.maxEntries = max
		}
	}
}

// NewMemoryReplayCache constructs an in-memory replay cache using the supplied
// clock. A nil clock defaults to SystemClock. The cache is bounded to
// DefaultReplayCacheMaxEntries unless overridden via WithReplayCacheMaxEntries.
func NewMemoryReplayCache(clock Clock, opts ...MemoryReplayCacheOption) *MemoryReplayCache {
	if clock == nil {
		clock = SystemClock{}
	}
	c := &MemoryReplayCache{
		clock:      clock,
		maxEntries: DefaultReplayCacheMaxEntries,
		seen:       make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SeenBefore records jti and reports whether it was already present and still
// within its lifetime. Expired entries are treated as not-seen and refreshed.
func (c *MemoryReplayCache) SeenBefore(jti string, expiresAt time.Time) bool {
	now := c.clock.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.pruneLocked(now)

	if exp, ok := c.seen[jti]; ok && now.Before(exp) {
		return true
	}
	c.evictForInsertLocked(jti)
	c.seen[jti] = expiresAt
	return false
}

// evictForInsertLocked makes room for a new jti when the cache is at capacity.
// pruneLocked has already removed every expired entry by the time this runs, so
// if the cache is still full it evicts the entries expiring soonest until one
// slot is free. Recording an existing (live) jti needs no eviction. The caller
// must hold c.mu and must have called pruneLocked first.
func (c *MemoryReplayCache) evictForInsertLocked(jti string) {
	if _, exists := c.seen[jti]; exists {
		return
	}
	for len(c.seen) >= c.maxEntries {
		var oldestJTI string
		var oldestExpiry time.Time
		first := true
		for k, expiry := range c.seen {
			if first || expiry.Before(oldestExpiry) {
				oldestJTI = k
				oldestExpiry = expiry
				first = false
			}
		}
		if first {
			break
		}
		delete(c.seen, oldestJTI)
	}
}

// pruneLocked removes expired entries. The caller must hold c.mu.
func (c *MemoryReplayCache) pruneLocked(now time.Time) {
	for jti, exp := range c.seen {
		if !now.Before(exp) {
			delete(c.seen, jti)
		}
	}
}

// Len returns the number of live entries. Intended for tests and metrics.
func (c *MemoryReplayCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(c.clock.Now())
	return len(c.seen)
}
