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
	"fmt"
	"testing"
)

// BenchmarkRoute measures per-request routing lookup throughput using
// modulo fallback (the hottest path, called on every request).
func BenchmarkRoute(b *testing.B) {
	shardCounts := []int{3, 10, 64, 256}

	for _, n := range shardCounts {
		b.Run(fmt.Sprintf("Shards_%d", n), func(b *testing.B) {
			shards := make([]uint64, n)
			for i := range shards {
				shards[i] = uint64(i + 1)
			}
			r := NewWithShards(shards)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				r.Route("events", uint64(i))
			}
		})
	}
}

// BenchmarkRouteEntityPinning measures pinned entity routing versus
// modulo fallback to quantify the fast-path advantage.
func BenchmarkRouteEntityPinning(b *testing.B) {
	shards := make([]uint64, 64)
	for i := range shards {
		shards[i] = uint64(i + 1)
	}

	b.Run("Pinned", func(b *testing.B) {
		r := NewWithShards(shards)
		r.SetEntityShard("users", 42)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			r.Route("users", uint64(i))
		}
	})

	b.Run("ModuloFallback", func(b *testing.B) {
		r := NewWithShards(shards)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			r.Route("events", uint64(i))
		}
	})
}

// BenchmarkRouteConcurrent measures parallel routing under contention using
// b.RunParallel for realistic multi-goroutine access patterns.
func BenchmarkRouteConcurrent(b *testing.B) {
	shardCounts := []int{3, 64, 256}

	for _, n := range shardCounts {
		b.Run(fmt.Sprintf("Shards_%d", n), func(b *testing.B) {
			shards := make([]uint64, n)
			for i := range shards {
				shards[i] = uint64(i + 1)
			}
			r := NewWithShards(shards)

			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				id := uint64(0)
				for pb.Next() {
					r.Route("events", id)
					id++
				}
			})
		})
	}

	b.Run("MixedPinnedAndFallback", func(b *testing.B) {
		shards := make([]uint64, 64)
		for i := range shards {
			shards[i] = uint64(i + 1)
		}
		r := NewWithShards(shards)
		r.SetEntityShard("users", 10)
		r.SetEntityShard("orders", 20)

		entities := []string{"users", "orders", "events", "metrics", "logs"}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			id := uint64(0)
			for pb.Next() {
				entity := entities[id%uint64(len(entities))]
				r.Route(entity, id)
				id++
			}
		})
	})
}

// BenchmarkSetShards measures shard reconfiguration overhead to ensure
// dynamic topology changes do not block the routing hot path.
func BenchmarkSetShards(b *testing.B) {
	shardConfigs := []int{3, 10, 64, 256}

	for _, n := range shardConfigs {
		b.Run(fmt.Sprintf("ShardCount_%d", n), func(b *testing.B) {
			r := New()
			shards := make([]uint64, n)
			for i := range shards {
				shards[i] = uint64(i + 1)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				r.SetShards(shards)
			}
		})
	}
}

// BenchmarkSetShardsUnderLoad measures SetShards while concurrent Route
// calls are in flight, simulating a live reconfiguration scenario.
func BenchmarkSetShardsUnderLoad(b *testing.B) {
	shards := make([]uint64, 64)
	for i := range shards {
		shards[i] = uint64(i + 1)
	}
	r := NewWithShards(shards)

	// Pre-populate some entity pins to exercise both code paths.
	r.SetEntityShard("users", 10)
	r.SetEntityShard("orders", 20)

	done := make(chan struct{})

	// Background goroutine performing continuous routing lookups.
	go func() {
		id := uint64(0)
		for {
			select {
			case <-done:
				return
			default:
				r.Route("events", id)
				id++
			}
		}
	}()

	newShards := make([]uint64, 32)
	for i := range newShards {
		newShards[i] = uint64(i + 100)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			r.SetShards(shards)
		} else {
			r.SetShards(newShards)
		}
	}

	b.StopTimer()
	close(done)
}

// BenchmarkRouteZeroID measures the zero-ID path that returns the first
// shard deterministically (common for operations without a natural key).
func BenchmarkRouteZeroID(b *testing.B) {
	shards := make([]uint64, 64)
	for i := range shards {
		shards[i] = uint64(i + 1)
	}
	r := NewWithShards(shards)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r.Route("metrics", 0)
	}
}
