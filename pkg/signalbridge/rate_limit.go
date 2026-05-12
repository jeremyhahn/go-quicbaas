// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"sync"
	"time"
)

// tokenBucket is a simple thread-safe rate limiter. Not sufficient for
// cluster-wide quota enforcement but fine for per-conversation
// outbound throttling inside a single bot process.
type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second
	last       time.Time
	// nowFn is a seam for tests. When non-nil it replaces time.Now so
	// tests can drive the clock without time.Sleep.
	nowFn func() time.Time
}

// newTokenBucket constructs a bucket with the given refill rate and
// capacity (both in tokens; capacity in absolute count, rate in
// tokens/second).
func newTokenBucket(rate, capacity float64) *tokenBucket {
	if capacity <= 0 {
		capacity = 1
	}
	if rate <= 0 {
		rate = 1
	}
	return &tokenBucket{
		tokens:     capacity,
		capacity:   capacity,
		refillRate: rate,
		last:       time.Now(),
	}
}

// take atomically attempts to remove one token. Returns true on
// success, false when empty.
func (b *tokenBucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// now returns the current time, deferring to nowFn when set.
func (b *tokenBucket) now() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}
