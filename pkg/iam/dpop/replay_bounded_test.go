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
	"strconv"
	"testing"
	"time"
)

// TestMemoryReplayCache_ExpiredEntriesSwept proves expired jti entries are
// removed once the clock advances past their expiry, even when a different key
// triggers the sweep (not just same-key re-read).
func TestMemoryReplayCache_ExpiredEntriesSwept(t *testing.T) {
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	cache := NewMemoryReplayCache(clock)

	exp := clock.t.Add(time.Minute)
	for i := 0; i < 100; i++ {
		if cache.SeenBefore("old-"+strconv.Itoa(i), exp) {
			t.Fatalf("entry old-%d unexpectedly reported as replay on first insert", i)
		}
	}
	if got := cache.Len(); got != 100 {
		t.Fatalf("expected 100 live entries, got %d", got)
	}

	// Advance past expiry; touching an unrelated key must sweep all 100.
	clock.t = clock.t.Add(2 * time.Minute)
	if cache.SeenBefore("fresh", clock.t.Add(time.Minute)) {
		t.Fatal("fresh key should not be a replay")
	}
	if got := cache.Len(); got != 1 {
		t.Fatalf("expected only the fresh entry after sweep, got %d", got)
	}
}

// TestMemoryReplayCache_CapHoldsUnderPressure inserts cap+N distinct, all-live
// jtis and asserts the map never exceeds the cap.
func TestMemoryReplayCache_CapHoldsUnderPressure(t *testing.T) {
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	const cap = 8
	cache := NewMemoryReplayCache(clock, WithReplayCacheMaxEntries(cap))

	// All entries share a far-future expiry so none are sweepable: the cap
	// must be enforced by eviction, not by TTL pruning.
	exp := clock.t.Add(time.Hour)
	for i := 0; i < cap+50; i++ {
		cache.SeenBefore("jti-"+strconv.Itoa(i), exp)
		if got := cache.Len(); got > cap {
			t.Fatalf("cache exceeded cap: size %d > cap %d after insert %d", got, cap, i)
		}
	}
	if got := cache.Len(); got != cap {
		t.Fatalf("expected cache saturated at cap %d, got %d", cap, got)
	}
}

// TestMemoryReplayCache_ReplayDetectedAfterOtherEvictions proves a recently
// recorded jti is still detected as a replay even after evictions of OTHER keys.
// The most-recently inserted key has the latest expiry, so soonest-expiry
// eviction keeps it alive.
func TestMemoryReplayCache_ReplayDetectedAfterOtherEvictions(t *testing.T) {
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	const cap = 4
	cache := NewMemoryReplayCache(clock, WithReplayCacheMaxEntries(cap))

	// Record the protected jti with the latest expiry of the set.
	protectedExp := clock.t.Add(10 * time.Minute)
	if cache.SeenBefore("protected", protectedExp) {
		t.Fatal("protected jti should not be a replay on first record")
	}

	// Flood with other keys whose expiries are sooner, forcing evictions.
	for i := 0; i < 100; i++ {
		cache.SeenBefore("flood-"+strconv.Itoa(i), clock.t.Add(time.Minute))
	}

	if got := cache.Len(); got > cap {
		t.Fatalf("cap violated: %d > %d", got, cap)
	}
	if !cache.SeenBefore("protected", protectedExp) {
		t.Fatal("protected jti within its window must still be detected as a replay after other-key evictions")
	}
}

// TestMemoryReplayCache_ReplayWindowAfterFloodSweep confirms the original
// regression concern: a live jti recorded before a flood is still flagged as a
// replay within its window once the flood entries expire and are swept.
func TestMemoryReplayCache_ReplayWindowAfterFloodSweep(t *testing.T) {
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	cache := NewMemoryReplayCache(clock)

	longExp := clock.t.Add(5 * time.Minute)
	if cache.SeenBefore("keep", longExp) {
		t.Fatal("keep should not be a replay on first record")
	}
	for i := 0; i < 50; i++ {
		cache.SeenBefore("short-"+strconv.Itoa(i), clock.t.Add(time.Minute))
	}

	// Advance past the short entries' expiry but within "keep"'s window.
	clock.t = clock.t.Add(2 * time.Minute)
	if !cache.SeenBefore("keep", longExp) {
		t.Fatal("keep within its window must still be a replay after flood sweep")
	}
	if got := cache.Len(); got != 1 {
		t.Fatalf("expected only keep to survive the sweep, got %d", got)
	}
}

// TestWithReplayCacheMaxEntries_IgnoresNonPositive verifies a non-positive cap
// falls back to the default rather than producing a zero-capacity cache.
func TestWithReplayCacheMaxEntries_IgnoresNonPositive(t *testing.T) {
	cache := NewMemoryReplayCache(nil, WithReplayCacheMaxEntries(0))
	if cache.maxEntries != DefaultReplayCacheMaxEntries {
		t.Fatalf("expected default cap %d, got %d", DefaultReplayCacheMaxEntries, cache.maxEntries)
	}
	cache = NewMemoryReplayCache(nil, WithReplayCacheMaxEntries(-5))
	if cache.maxEntries != DefaultReplayCacheMaxEntries {
		t.Fatalf("expected default cap %d for negative, got %d", DefaultReplayCacheMaxEntries, cache.maxEntries)
	}
}
