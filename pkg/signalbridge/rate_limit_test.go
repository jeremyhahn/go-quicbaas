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
	"testing"
	"time"
)

// TestNewTokenBucket_Defaults clamps non-positive rate and capacity up to
// 1 so a misconfigured limiter still allows a single immediate take.
func TestNewTokenBucket_Defaults(t *testing.T) {
	b := newTokenBucket(0, 0)
	if b.capacity != 1 || b.refillRate != 1 {
		t.Fatalf("defaults not applied: cap=%v rate=%v", b.capacity, b.refillRate)
	}
	if !b.take() {
		t.Fatalf("first take should succeed with defaulted capacity")
	}
	if b.take() {
		t.Fatalf("second immediate take should fail")
	}
}

// TestNewTokenBucket_NegativeInputs also clamps negatives.
func TestNewTokenBucket_NegativeInputs(t *testing.T) {
	b := newTokenBucket(-5, -3)
	if b.capacity != 1 || b.refillRate != 1 {
		t.Fatalf("negatives not clamped: cap=%v rate=%v", b.capacity, b.refillRate)
	}
}

// TestTokenBucket_NowDefault exercises the time.Now path (nowFn nil).
func TestTokenBucket_NowDefault(t *testing.T) {
	b := newTokenBucket(10, 5)
	got := b.now()
	if time.Since(got) > time.Second {
		t.Fatalf("now() returned a stale time: %v", got)
	}
}

// TestTokenBucket_CapacityClamp ensures refilled tokens never exceed
// capacity, even after a long idle period.
func TestTokenBucket_CapacityClamp(t *testing.T) {
	base := time.Now()
	b := newTokenBucket(100, 2) // capacity 2
	b.nowFn = func() time.Time { return base }
	b.last = base
	// Drain both tokens.
	if !b.take() || !b.take() {
		t.Fatalf("should be able to take capacity tokens")
	}
	if b.take() {
		t.Fatalf("should be empty now")
	}
	// Idle a long time; refill must clamp at capacity (2), so exactly two
	// takes succeed, not more.
	b.nowFn = func() time.Time { return base.Add(time.Hour) }
	if !b.take() || !b.take() {
		t.Fatalf("two takes should succeed after long idle")
	}
	if b.take() {
		t.Fatalf("third take should fail — tokens clamped to capacity")
	}
}
