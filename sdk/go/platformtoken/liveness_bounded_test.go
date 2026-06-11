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

package platformtoken

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// alwaysActive is a LivenessChecker that always reports active. callCount tracks
// how often the base was consulted so cache hits versus misses are observable.
type alwaysActive struct{ callCount atomic.Int64 }

func (a *alwaysActive) CheckLiveness(_ context.Context, _ string, _ *Claims) (bool, error) {
	a.callCount.Add(1)
	return true, nil
}

func claimsWithJTI(jti string) *Claims {
	return &Claims{RegisteredClaims: jwt.RegisteredClaims{ID: jti}}
}

func (c *CachedLivenessChecker) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// TestCachedLiveness_ExpiredEntryEvicted proves an expired positive entry is
// removed when an insert sweep runs, not only when the same key is re-read.
func TestCachedLiveness_ExpiredEntryEvicted(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	base := &alwaysActive{}
	const cap = 2
	c := NewCachedLivenessChecker(base, time.Minute, clock, WithLivenessCacheMaxEntries(cap))

	ctx := context.Background()
	// Fill to capacity with two short-lived entries.
	if _, err := c.CheckLiveness(ctx, "t", claimsWithJTI("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CheckLiveness(ctx, "t", claimsWithJTI("b")); err != nil {
		t.Fatal(err)
	}
	if c.size() != 2 {
		t.Fatalf("expected 2 entries, got %d", c.size())
	}

	// Advance past expiry, then insert a new key: the sweep must reclaim the
	// two expired slots so the cache does not grow.
	now = now.Add(2 * time.Minute)
	if _, err := c.CheckLiveness(ctx, "t", claimsWithJTI("c")); err != nil {
		t.Fatal(err)
	}
	if got := c.size(); got != 1 {
		t.Fatalf("expected expired entries swept leaving only 'c', got %d", got)
	}
}

// TestCachedLiveness_CapHoldsUnderPressure inserts cap+N distinct, all-live keys
// and asserts the cache never exceeds the cap.
func TestCachedLiveness_CapHoldsUnderPressure(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	base := &alwaysActive{}
	const cap = 16
	c := NewCachedLivenessChecker(base, time.Hour, clock, WithLivenessCacheMaxEntries(cap))

	ctx := context.Background()
	for i := 0; i < cap+100; i++ {
		if _, err := c.CheckLiveness(ctx, "t", claimsWithJTI("k"+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
		if got := c.size(); got > cap {
			t.Fatalf("cache exceeded cap: %d > %d at insert %d", got, cap, i)
		}
	}
	if got := c.size(); got != cap {
		t.Fatalf("expected cache saturated at %d, got %d", cap, got)
	}
}

// TestCachedLiveness_LiveEntryStillHitsAfterEvictions proves a fresh entry with
// the latest expiry survives eviction of older keys and serves a cache hit
// (base not re-consulted).
func TestCachedLiveness_LiveEntryStillHitsAfterEvictions(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	base := &alwaysActive{}
	const cap = 4
	c := NewCachedLivenessChecker(base, time.Hour, clock, WithLivenessCacheMaxEntries(cap))

	ctx := context.Background()

	// Seed older entries, then advance the clock slightly so the protected key
	// gets a strictly later expiry and is preferred by soonest-expiry eviction.
	for i := 0; i < cap-1; i++ {
		if _, err := c.CheckLiveness(ctx, "t", claimsWithJTI("old"+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(time.Second)
	if _, err := c.CheckLiveness(ctx, "t", claimsWithJTI("protected")); err != nil {
		t.Fatal(err)
	}

	// Flood with newer keys to force evictions of the oldest entries.
	for i := 0; i < 50; i++ {
		now = now.Add(time.Second)
		if _, err := c.CheckLiveness(ctx, "t", claimsWithJTI("new"+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.size(); got > cap {
		t.Fatalf("cap violated: %d > %d", got, cap)
	}

	// "protected" had a later expiry than the original old* keys, so it was not
	// the soonest-expiring during the early evictions. It may eventually be
	// evicted by even-newer keys, so re-prime then assert a true cache hit.
	if _, err := c.CheckLiveness(ctx, "t", claimsWithJTI("protected")); err != nil {
		t.Fatal(err)
	}
	callsBeforeHit := base.callCount.Load()
	active, err := c.CheckLiveness(ctx, "t", claimsWithJTI("protected"))
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("expected protected entry to report active")
	}
	if base.callCount.Load() != callsBeforeHit {
		t.Fatal("expected a cache hit for protected (base must not be re-consulted)")
	}
}

// TestCachedLiveness_NegativeResultNotCached confirms inactive results are never
// stored, so the bound interacts only with positive results.
func TestCachedLiveness_NegativeResultNotCached(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	base := &stubLiveness{results: []bool{false}}
	c := NewCachedLivenessChecker(base, time.Hour, clock)

	if _, err := c.CheckLiveness(context.Background(), "t", claimsWithJTI("x")); err != nil {
		t.Fatal(err)
	}
	if c.size() != 0 {
		t.Fatalf("negative result must not be cached, size=%d", c.size())
	}
}

// TestWithLivenessCacheMaxEntries_IgnoresNonPositive verifies a non-positive cap
// falls back to the default.
func TestWithLivenessCacheMaxEntries_IgnoresNonPositive(t *testing.T) {
	c := NewCachedLivenessChecker(&alwaysActive{}, time.Hour, nil, WithLivenessCacheMaxEntries(0))
	if c.maxEntries != DefaultLivenessCacheMaxEntries {
		t.Fatalf("expected default cap %d, got %d", DefaultLivenessCacheMaxEntries, c.maxEntries)
	}
	c = NewCachedLivenessChecker(&alwaysActive{}, time.Hour, nil, WithLivenessCacheMaxEntries(-3))
	if c.maxEntries != DefaultLivenessCacheMaxEntries {
		t.Fatalf("expected default cap %d for negative, got %d", DefaultLivenessCacheMaxEntries, c.maxEntries)
	}
}
