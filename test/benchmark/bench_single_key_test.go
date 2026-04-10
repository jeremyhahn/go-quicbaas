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

package benchmark

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Performance targets for single-key operations.
const (
	targetReadP99   = 10 * time.Millisecond
	targetWriteP99  = 25 * time.Millisecond
	targetDeleteP99 = 15 * time.Millisecond
)

// BenchmarkReadLatency measures read operation latency with various value sizes.
func BenchmarkReadLatency(b *testing.B) {
	valueSizes := []int{64, 256, 1024, 4096, 16384}

	for _, size := range valueSizes {
		b.Run(fmt.Sprintf("ValueSize-%d", size), func(b *testing.B) {
			benchmarkReadLatency(b, size)
		})
	}
}

func benchmarkReadLatency(b *testing.B, valueSize int) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database with test data
	numKeys := 1000
	testData := generateTestData(valueSize)
	keys := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("bench-read-%d", i)
		// TODO: Implement Put operation when server API is ready
		_ = testData
		_ = ctx
	}

	// Warm up
	// TODO: warmupServer(b, bs)

	// Track latencies
	tracker := NewLatencyTracker()

	b.ResetTimer()
	b.ReportAllocs()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		key := keys[i%numKeys]
		start := time.Now()

		// TODO: Implement Get operation when server API is ready
		_ = key

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	// Calculate and report statistics
	stats := tracker.GetStats()
	reportLatencyStats(b, stats)

	// Check against target
	assertPerformanceTarget(b, stats.P99, targetReadP99, "Read P99")

	// Report memory usage
	measureMemory(b)
}

// BenchmarkReadLatencyConsistency measures read latency across different consistency levels.
func BenchmarkReadLatencyConsistency(b *testing.B) {
	consistencyLevels := []string{"Strong", "Linearizable", "Eventual"}

	for _, level := range consistencyLevels {
		b.Run(level, func(b *testing.B) {
			benchmarkReadLatencyConsistency(b, level)
		})
	}
}

func benchmarkReadLatencyConsistency(b *testing.B, consistencyLevel string) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 100
	testData := generateTestData(1024)
	keys := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("bench-consistency-%d", i)
		// TODO: Implement Put operation
		_ = testData
		_ = ctx
	}

	tracker := NewLatencyTracker()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := keys[i%numKeys]
		start := time.Now()

		// TODO: Implement Get with consistency level
		_ = key
		_ = consistencyLevel

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)
}

// BenchmarkWriteLatency measures write operation latency with various value sizes.
func BenchmarkWriteLatency(b *testing.B) {
	valueSizes := []int{64, 256, 1024, 4096, 16384}

	for _, size := range valueSizes {
		b.Run(fmt.Sprintf("ValueSize-%d", size), func(b *testing.B) {
			benchmarkWriteLatency(b, size)
		})
	}
}

func benchmarkWriteLatency(b *testing.B, valueSize int) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	testData := generateTestData(valueSize)
	tracker := NewLatencyTracker()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-write-%d", i)
		start := time.Now()

		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)

	// Check against target
	assertPerformanceTarget(b, stats.P99, targetWriteP99, "Write P99")

	measureMemory(b)
}

// BenchmarkWriteLatencySyncModes measures write latency across different sync modes.
func BenchmarkWriteLatencySyncModes(b *testing.B) {
	syncModes := []string{"Async", "Sync", "Batched"}

	for _, mode := range syncModes {
		b.Run(mode, func(b *testing.B) {
			benchmarkWriteLatencySyncMode(b, mode)
		})
	}
}

func benchmarkWriteLatencySyncMode(b *testing.B, syncMode string) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Adjust config based on sync mode
	switch syncMode {
	case "Sync":
		// TODO: bs.Config.Storage.SyncWrites = true
		// TODO: bs.Config.Raft.WriteMode = "sync"
	case "Async":
		// TODO: bs.Config.Storage.SyncWrites = false
		// TODO: bs.Config.Raft.WriteMode = "wal"
	case "Batched":
		// TODO: bs.Config.Storage.SyncWrites = false
		// TODO: bs.Config.Raft.WriteMode = "batched"
	}

	testData := generateTestData(1024)
	tracker := NewLatencyTracker()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-sync-%s-%d", syncMode, i)
		start := time.Now()

		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)
}

// BenchmarkDeleteLatency measures delete operation latency.
func BenchmarkDeleteLatency(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database with keys to delete
	testData := generateTestData(1024)
	keys := make([]string, b.N)

	for i := 0; i < b.N; i++ {
		keys[i] = fmt.Sprintf("bench-delete-%d", i)
		// TODO: Implement Put operation
		_ = testData
		_ = ctx
	}

	tracker := NewLatencyTracker()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := keys[i]
		start := time.Now()

		// TODO: Implement Delete operation
		_ = key
		_ = ctx

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)

	// Check against target
	assertPerformanceTarget(b, stats.P99, targetDeleteP99, "Delete P99")

	measureMemory(b)
}

// BenchmarkUpdateLatency measures update (overwrite) operation latency.
func BenchmarkUpdateLatency(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 1000
	testData := generateTestData(1024)
	keys := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("bench-update-%d", i)
		// TODO: Implement Put operation
		_ = testData
		_ = ctx
	}

	// New data for updates
	updateData := generateTestData(1024)
	tracker := NewLatencyTracker()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := keys[i%numKeys]
		start := time.Now()

		// TODO: Implement Put operation (update existing key)
		_ = key
		_ = updateData
		_ = ctx

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)
}

// BenchmarkColdCacheRead measures read latency with cold cache.
func BenchmarkColdCacheRead(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-cold-%d", i)
		// TODO: Implement Put operation
		_ = testData
		_ = key
		_ = ctx
	}

	tracker := NewLatencyTracker()

	b.ResetTimer()

	// Read different keys each time to avoid cache hits
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-cold-%d", i%numKeys)
		start := time.Now()

		// TODO: Implement Get operation
		_ = key
		_ = ctx

		latency := time.Since(start)
		tracker.Record(latency)

		// Clear cache between reads to measure cold cache performance
		// TODO: Implement cache clear operation
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)
}

// BenchmarkWarmCacheRead measures read latency with warm cache.
func BenchmarkWarmCacheRead(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 100 // Small set to fit in cache
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-warm-%d", i)
		// TODO: Implement Put operation
		_ = testData
		_ = key
		_ = ctx
	}

	// Warm up cache by reading all keys
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-warm-%d", i)
		// TODO: Implement Get operation
		_ = key
		_ = ctx
	}

	tracker := NewLatencyTracker()

	b.ResetTimer()

	// Read same keys repeatedly to maximize cache hits
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-warm-%d", i%numKeys)
		start := time.Now()

		// TODO: Implement Get operation
		_ = key
		_ = ctx

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)

	// Warm cache should have much lower latency
	b.Logf("Warm cache P99: %v (should be <1ms for cached reads)", stats.P99)
}
