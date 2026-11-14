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

package cache

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetrics_AllOperations(t *testing.T) {
	m := NewMetrics()

	t.Run("increment and add", func(t *testing.T) {
		m.Increment("l1_hits")
		assert.Equal(t, int64(1), m.Get("l1_hits"))

		m.Add("l1_hits", 5)
		assert.Equal(t, int64(6), m.Get("l1_hits"))

		m.Add("l1_misses", 10)
		assert.Equal(t, int64(10), m.Get("l1_misses"))
	})

	t.Run("all metric types", func(t *testing.T) {
		m.Reset()

		// Test all metric names
		metricNames := []string{
			"l1_hits", "l1_misses", "l1_sets", "l1_deletes", "l1_expired",
			"l2_hits", "l2_misses", "l2_sets", "l2_deletes", "l2_errors",
			"cache_misses", "cache_clears",
		}

		for i, name := range metricNames {
			m.Add(name, int64(i+1))
			assert.Equal(t, int64(i+1), m.Get(name), "metric %s should be %d", name, i+1)
		}
	})

	t.Run("L2 metric accessors", func(t *testing.T) {
		m.Reset()

		m.Add("l2_hits", 100)
		m.Add("l2_misses", 50)

		assert.Equal(t, int64(100), m.L2Hits())
		assert.Equal(t, int64(50), m.L2Misses())
	})

	t.Run("L1 metric accessors", func(t *testing.T) {
		m.Reset()

		m.Add("l1_hits", 200)
		m.Add("l1_misses", 25)

		assert.Equal(t, int64(200), m.L1Hits())
		assert.Equal(t, int64(25), m.L1Misses())
	})

	t.Run("reset all metrics", func(t *testing.T) {
		// Set various metrics
		m.Add("l1_hits", 100)
		m.Add("l1_misses", 50)
		m.Add("l2_hits", 200)
		m.Add("cache_clears", 5)

		// Reset
		m.Reset()

		// All should be zero
		assert.Equal(t, int64(0), m.L1Hits())
		assert.Equal(t, int64(0), m.L1Misses())
		assert.Equal(t, int64(0), m.L2Hits())
		assert.Equal(t, int64(0), m.Get("cache_clears"))
	})

	t.Run("copy creates independent snapshot", func(t *testing.T) {
		m.Reset()
		m.Add("l1_hits", 100)
		m.Add("l1_misses", 50)

		// Create copy
		copy := m.Copy()

		// Modify original
		m.Add("l1_hits", 50)

		// Copy should not be affected
		assert.Equal(t, int64(100), copy.L1Hits())
		assert.Equal(t, int64(150), m.L1Hits())
	})

	t.Run("ToMap returns all metrics", func(t *testing.T) {
		m.Reset()
		m.Add("l1_hits", 10)
		m.Add("l1_misses", 5)
		m.Add("l2_hits", 20)
		m.Add("l2_misses", 3)
		m.Add("l1_sets", 15)
		m.Add("l1_deletes", 2)
		m.Add("l1_expired", 1)
		m.Add("l2_sets", 10)
		m.Add("l2_deletes", 1)
		m.Add("l2_errors", 0)
		m.Add("cache_misses", 8)
		m.Add("cache_clears", 1)

		metricsMap := m.ToMap()

		assert.Equal(t, int64(10), metricsMap["l1_hits"])
		assert.Equal(t, int64(5), metricsMap["l1_misses"])
		assert.Equal(t, int64(20), metricsMap["l2_hits"])
		assert.Equal(t, int64(3), metricsMap["l2_misses"])
		assert.Equal(t, int64(15), metricsMap["l1_sets"])
		assert.Equal(t, int64(2), metricsMap["l1_deletes"])
		assert.Equal(t, int64(1), metricsMap["l1_expired"])
		assert.Equal(t, int64(10), metricsMap["l2_sets"])
		assert.Equal(t, int64(1), metricsMap["l2_deletes"])
		assert.Equal(t, int64(0), metricsMap["l2_errors"])
		assert.Equal(t, int64(8), metricsMap["cache_misses"])
		assert.Equal(t, int64(1), metricsMap["cache_clears"])
		assert.Equal(t, int64(0), metricsMap["writeback_success"])
		assert.Equal(t, int64(0), metricsMap["writeback_failures"])

		// Should have all 14 metrics
		assert.Equal(t, 14, len(metricsMap))
	})

	t.Run("concurrent operations", func(t *testing.T) {
		m.Reset()

		const numGoroutines = 100
		const incrementsPerGoroutine = 100

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < incrementsPerGoroutine; j++ {
					m.Increment("l1_hits")
					m.Add("l1_misses", 2)
				}
			}()
		}

		wg.Wait()

		expectedHits := int64(numGoroutines * incrementsPerGoroutine)
		expectedMisses := int64(numGoroutines * incrementsPerGoroutine * 2)

		assert.Equal(t, expectedHits, m.L1Hits())
		assert.Equal(t, expectedMisses, m.L1Misses())
	})

	t.Run("add with unknown metric name is safe", func(t *testing.T) {
		m.Reset()

		// Should not panic
		m.Add("unknown_metric", 100)
		m.Increment("unknown_metric")

		// Unknown metric should return 0
		assert.Equal(t, int64(0), m.Get("unknown_metric"))
	})
}

func TestMetricsSnapshot_Operations(t *testing.T) {
	m := NewMetrics()
	m.Add("l1_hits", 100)
	m.Add("l1_misses", 50)
	m.Add("l2_hits", 200)
	m.Add("cache_clears", 5)

	t.Run("create snapshot", func(t *testing.T) {
		snapshot := NewMetricsSnapshot(m)
		assert.NotNil(t, snapshot)

		// Verify snapshot values
		assert.Equal(t, int64(100), snapshot.Get("l1_hits"))
		assert.Equal(t, int64(50), snapshot.Get("l1_misses"))
		assert.Equal(t, int64(200), snapshot.Get("l2_hits"))
		assert.Equal(t, int64(5), snapshot.Get("cache_clears"))
	})

	t.Run("snapshot is independent of original", func(t *testing.T) {
		snapshot := NewMetricsSnapshot(m)

		// Modify original
		m.Add("l1_hits", 50)

		// Snapshot should remain unchanged
		assert.Equal(t, int64(100), snapshot.Get("l1_hits"))
		assert.Equal(t, int64(150), m.Get("l1_hits"))
	})

	t.Run("get unknown metric from snapshot returns zero", func(t *testing.T) {
		snapshot := NewMetricsSnapshot(m)
		val := snapshot.Get("unknown_metric")
		assert.Equal(t, int64(0), val)
	})

	t.Run("all returns copy of all metrics", func(t *testing.T) {
		snapshot := NewMetricsSnapshot(m)
		allMetrics := snapshot.All()

		assert.Equal(t, 14, len(allMetrics)) // All metric types (including writeback_success, writeback_failures)
		assert.Equal(t, int64(150), allMetrics["l1_hits"])
		assert.Equal(t, int64(50), allMetrics["l1_misses"])

		// Modify returned map should not affect snapshot
		allMetrics["l1_hits"] = 999

		// Get again should return original value
		allMetrics2 := snapshot.All()
		assert.Equal(t, int64(150), allMetrics2["l1_hits"])
	})

	t.Run("concurrent reads from snapshot", func(t *testing.T) {
		snapshot := NewMetricsSnapshot(m)

		const numGoroutines = 50
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					snapshot.Get("l1_hits")
					snapshot.All()
				}
			}()
		}

		wg.Wait()

		// Should not panic and values should be consistent
		assert.Equal(t, int64(150), snapshot.Get("l1_hits"))
	})
}

func TestMetrics_EdgeCases(t *testing.T) {
	t.Run("add negative value", func(t *testing.T) {
		m := NewMetrics()
		m.Add("l1_hits", 100)
		m.Add("l1_hits", -50)
		assert.Equal(t, int64(50), m.Get("l1_hits"))
	})
}

func BenchmarkMetrics_Increment(b *testing.B) {
	m := NewMetrics()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.Increment("l1_hits")
		}
	})
}

func BenchmarkMetrics_Add(b *testing.B) {
	m := NewMetrics()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.Add("l1_hits", 5)
		}
	})
}

func BenchmarkMetrics_Get(b *testing.B) {
	m := NewMetrics()
	m.Add("l1_hits", 100)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.Get("l1_hits")
		}
	})
}

func BenchmarkMetrics_Copy(b *testing.B) {
	m := NewMetrics()
	m.Add("l1_hits", 100)
	m.Add("l1_misses", 50)
	m.Add("l2_hits", 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Copy()
	}
}

func BenchmarkMetrics_ToMap(b *testing.B) {
	m := NewMetrics()
	m.Add("l1_hits", 100)
	m.Add("l1_misses", 50)
	m.Add("l2_hits", 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ToMap()
	}
}
