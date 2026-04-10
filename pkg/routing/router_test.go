// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package routing

import (
	"testing"
)

func TestRouter_EntityPinning(t *testing.T) {
	r := NewWithShards([]uint64{1, 2, 3})
	r.SetEntityShard("users", 3)

	if got := r.Route("users", 0); got != 3 {
		t.Fatalf("expected shard 3, got %d", got)
	}
	if got := r.Route("users", 42); got != 3 {
		t.Fatalf("expected shard 3 for id=42, got %d", got)
	}
}

func TestRouter_ModuloFallback(t *testing.T) {
	r := NewWithShards([]uint64{10, 20, 30, 40})
	// No pin for entity type
	cases := []struct {
		id   uint64
		want uint64
	}{
		{1, 20},
		{2, 30},
		{3, 40},
		{4, 10},
		{8, 10},
	}
	for _, c := range cases {
		if got := r.Route("events", c.id); got != c.want {
			t.Fatalf("id=%d expected %d, got %d", c.id, c.want, got)
		}
	}
}

func TestRouter_DefaultShardWhenNoID(t *testing.T) {
	r := NewWithShards([]uint64{5, 6})
	if got := r.Route("metrics", 0); got != 5 {
		t.Fatalf("expected default first shard 5, got %d", got)
	}
}

func TestRouter_DefaultsWhenNoShardsConfigured(t *testing.T) {
	r := New()
	if got := r.Route("any", 0); got != 1 {
		t.Fatalf("expected default shard 1, got %d", got)
	}
	r2 := NewWithShards(nil)
	if got := r2.Route("any", 123); got != 1 {
		t.Fatalf("expected default shard 1 when shards empty, got %d", got)
	}
}

func TestRouter_SetShardsReplacesList(t *testing.T) {
	r := NewWithShards([]uint64{1, 2, 3})
	if got := r.Route("evt", 1); got != 2 { // idx 1 => shard 2
		t.Fatalf("unexpected shard: %d", got)
	}
	r.SetShards([]uint64{10, 20})
	if got := r.Route("evt", 1); got != 20 { // idx 1 => 20
		t.Fatalf("expected shard 20 after reset, got %d", got)
	}
	// Empty resets to [1]
	r.SetShards(nil)
	if got := r.Route("evt", 1); got != 1 {
		t.Fatalf("expected shard 1 after empty reset, got %d", got)
	}
}

func TestRouter_MultiplePins(t *testing.T) {
	r := NewWithShards([]uint64{1, 2, 3})
	r.SetEntityShard("users", 2)
	r.SetEntityShard("orders", 3)
	if got := r.Route("users", 999); got != 2 {
		t.Fatalf("users pinned to 2, got %d", got)
	}
	if got := r.Route("orders", 1); got != 3 {
		t.Fatalf("orders pinned to 3, got %d", got)
	}
	if got := r.Route("invoices", 4); got != 2 {
		t.Fatalf("fallback modulo expected 2, got %d", got)
	}
}

func TestRouter_DeterminismAcrossCalls(t *testing.T) {
	r := NewWithShards([]uint64{1, 2, 3})
	ids := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9}
	want := make([]uint64, len(ids))
	for i, id := range ids {
		want[i] = r.Route("evt", id)
	}
	// Repeat and ensure stable mapping
	for i, id := range ids {
		if got := r.Route("evt", id); got != want[i] {
			t.Fatalf("non-deterministic mapping for id=%d: got %d want %d", id, got, want[i])
		}
	}
}
