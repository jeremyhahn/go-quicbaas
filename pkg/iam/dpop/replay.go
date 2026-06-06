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

// MemoryReplayCache is an in-memory, TTL-bounded ReplayCache. Entries are
// pruned lazily on access and on a bounded sweep so the map cannot grow
// without limit under sustained load.
type MemoryReplayCache struct {
	clock Clock
	mu    sync.Mutex
	seen  map[string]time.Time
}

// NewMemoryReplayCache constructs an in-memory replay cache using the supplied
// clock. A nil clock defaults to SystemClock.
func NewMemoryReplayCache(clock Clock) *MemoryReplayCache {
	if clock == nil {
		clock = SystemClock{}
	}
	return &MemoryReplayCache{
		clock: clock,
		seen:  make(map[string]time.Time),
	}
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
	c.seen[jti] = expiresAt
	return false
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
