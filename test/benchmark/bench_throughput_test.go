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
	"sync/atomic"
	"testing"
	"time"
)

// Performance targets for throughput.
const (
	targetSequentialReads  = 10000.0 // ops/sec
	targetSequentialWrites = 5000.0  // ops/sec
	targetMixedWorkload    = 8000.0  // ops/sec (70% read, 30% write)
)

// BenchmarkSequentialReads measures maximum sequential read throughput.
func BenchmarkSequentialReads(b *testing.B) {
	valueSizes := []int{256, 1024, 4096}

	for _, size := range valueSizes {
		b.Run(fmt.Sprintf("ValueSize-%d", size), func(b *testing.B) {
			benchmarkSequentialReads(b, size)
		})
	}
}

func benchmarkSequentialReads(b *testing.B, valueSize int) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(valueSize)
	keys := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("bench-seq-read-%d", i)
		if err := server.Put(ctx, keys[i], testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewThroughputTracker()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := keys[i%numKeys]

		// Execute Get operation
		if _, err := server.Get(ctx, key); err != nil {
			b.Fatalf("Get failed: %v", err)
		}

		tracker.RecordOp(valueSize, true)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)

	// Check against target
	if stats.OpsPerSec >= targetSequentialReads {
		b.Logf("SUCCESS: Sequential read throughput target met: %.2f >= %.2f ops/sec",
			stats.OpsPerSec, targetSequentialReads)
	} else {
		b.Logf("WARNING: Sequential read throughput below target: %.2f < %.2f ops/sec",
			stats.OpsPerSec, targetSequentialReads)
	}
}

// BenchmarkSequentialWrites measures maximum sequential write throughput.
func BenchmarkSequentialWrites(b *testing.B) {
	valueSizes := []int{256, 1024, 4096}

	for _, size := range valueSizes {
		b.Run(fmt.Sprintf("ValueSize-%d", size), func(b *testing.B) {
			benchmarkSequentialWrites(b, size)
		})
	}
}

func benchmarkSequentialWrites(b *testing.B, valueSize int) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	testData := generateTestData(valueSize)
	tracker := NewThroughputTracker()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-seq-write-%d", i)

		// Execute Put operation
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed: %v", err)
		}

		tracker.RecordOp(valueSize, true)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)

	// Check against target
	if stats.OpsPerSec >= targetSequentialWrites {
		b.Logf("SUCCESS: Sequential write throughput target met: %.2f >= %.2f ops/sec",
			stats.OpsPerSec, targetSequentialWrites)
	} else {
		b.Logf("WARNING: Sequential write throughput below target: %.2f < %.2f ops/sec",
			stats.OpsPerSec, targetSequentialWrites)
	}
}

// BenchmarkMixedWorkload measures throughput with mixed read/write workload.
func BenchmarkMixedWorkload(b *testing.B) {
	readPercentages := []int{50, 70, 90, 95}

	for _, readPct := range readPercentages {
		b.Run(fmt.Sprintf("Read%d", readPct), func(b *testing.B) {
			benchmarkMixedWorkload(b, readPct)
		})
	}
}

func benchmarkMixedWorkload(b *testing.B, readPercentage int) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)
	keys := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("bench-mixed-%d", i)
		if err := server.Put(ctx, keys[i], testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewThroughputTracker()
	var readCount, writeCount atomic.Int64

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Determine if this should be a read or write based on percentage
		isRead := (i % 100) < readPercentage

		if isRead {
			// Perform read
			key := keys[i%numKeys]
			if _, err := server.Get(ctx, key); err != nil {
				b.Fatalf("Get failed: %v", err)
			}
			readCount.Add(1)
			tracker.RecordOp(1024, true)
		} else {
			// Perform write
			key := keys[i%numKeys]
			if err := server.Put(ctx, key, testData); err != nil {
				b.Fatalf("Put failed: %v", err)
			}
			writeCount.Add(1)
			tracker.RecordOp(1024, true)
		}
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)

	b.ReportMetric(float64(readCount.Load()), "reads")
	b.ReportMetric(float64(writeCount.Load()), "writes")

	// Check against target for 70% read workload
	if readPercentage == 70 {
		if stats.OpsPerSec >= targetMixedWorkload {
			b.Logf("SUCCESS: Mixed workload throughput target met: %.2f >= %.2f ops/sec",
				stats.OpsPerSec, targetMixedWorkload)
		} else {
			b.Logf("WARNING: Mixed workload throughput below target: %.2f < %.2f ops/sec",
				stats.OpsPerSec, targetMixedWorkload)
		}
	}
}

// BenchmarkThroughputScaling measures throughput scaling with data size.
func BenchmarkThroughputScaling(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	dataSizes := []int{100, 1000, 10000, 100000}

	for _, dataSize := range dataSizes {
		b.Run(fmt.Sprintf("Keys-%d", dataSize), func(b *testing.B) {
			// Pre-populate database
			testData := generateTestData(1024)
			keys := make([]string, dataSize)

			for i := 0; i < dataSize; i++ {
				keys[i] = fmt.Sprintf("bench-scale-%d-%d", dataSize, i)
				if err := server.Put(ctx, keys[i], testData); err != nil {
					b.Fatalf("Put failed during setup: %v", err)
				}
			}

			tracker := NewThroughputTracker()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				key := keys[i%dataSize]

				// Execute Get operation
				if _, err := server.Get(ctx, key); err != nil {
					b.Fatalf("Get failed: %v", err)
				}

				tracker.RecordOp(1024, true)
			}

			b.StopTimer()

			stats := tracker.GetStats()
			reportThroughputStats(b, stats)
		})
	}
}

// BenchmarkSustainedThroughput measures sustained throughput over longer duration.
func BenchmarkSustainedThroughput(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)
	keys := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("bench-sustained-%d", i)
		if err := server.Put(ctx, keys[i], testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	// Track throughput in time windows
	windowSize := 5 * time.Second
	tracker := NewThroughputTracker()
	lastReport := time.Now()
	windowStart := time.Now()
	var windowOps atomic.Int64

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Mix of reads and writes (70% read, 30% write)
		isRead := (i % 10) < 7
		key := keys[i%numKeys]

		if isRead {
			if _, err := server.Get(ctx, key); err != nil {
				b.Fatalf("Get failed: %v", err)
			}
		} else {
			if err := server.Put(ctx, key, testData); err != nil {
				b.Fatalf("Put failed: %v", err)
			}
		}

		tracker.RecordOp(1024, true)
		windowOps.Add(1)

		// Report throughput every window
		if time.Since(lastReport) >= windowSize {
			windowDuration := time.Since(windowStart)
			windowThroughput := float64(windowOps.Load()) / windowDuration.Seconds()
			b.Logf("Window throughput: %.2f ops/sec", windowThroughput)

			lastReport = time.Now()
			windowStart = time.Now()
			windowOps.Store(0)
		}
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)
}

// BenchmarkBurstThroughput measures peak burst throughput.
func BenchmarkBurstThroughput(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 1000
	testData := generateTestData(256) // Smaller values for faster bursts
	keys := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("bench-burst-%d", i)
		if err := server.Put(ctx, keys[i], testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewThroughputTracker()

	b.ResetTimer()

	// Perform burst of operations as fast as possible
	for i := 0; i < b.N; i++ {
		key := keys[i%numKeys]

		// Execute Get operation
		if _, err := server.Get(ctx, key); err != nil {
			b.Fatalf("Get failed: %v", err)
		}

		tracker.RecordOp(256, true)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)

	b.Logf("Peak burst throughput: %.2f ops/sec", stats.OpsPerSec)
}

// BenchmarkThroughputWithMetrics measures throughput impact of metrics collection.
func BenchmarkThroughputWithMetrics(b *testing.B) {
	metricsSettings := []bool{false, true}

	for _, metricsEnabled := range metricsSettings {
		name := "MetricsDisabled"
		if metricsEnabled {
			name = "MetricsEnabled"
		}

		b.Run(name, func(b *testing.B) {
			server := setupBenchmarkServer(b)
			// Configure metrics (would set server.Config.Extended.Metrics.Enabled in real implementation)
			server.Config.Extended.Metrics.Enabled = metricsEnabled

			ctx := context.Background()

			// Pre-populate database
			numKeys := 1000
			testData := generateTestData(1024)
			keys := make([]string, numKeys)

			for i := 0; i < numKeys; i++ {
				keys[i] = fmt.Sprintf("bench-metrics-%d", i)
				if err := server.Put(ctx, keys[i], testData); err != nil {
					b.Fatalf("Put failed during setup: %v", err)
				}
			}

			tracker := NewThroughputTracker()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				key := keys[i%numKeys]

				// Execute Get operation
				if _, err := server.Get(ctx, key); err != nil {
					b.Fatalf("Get failed: %v", err)
				}

				tracker.RecordOp(1024, true)
			}

			b.StopTimer()

			stats := tracker.GetStats()
			reportThroughputStats(b, stats)
		})
	}
}

// BenchmarkWriteThroughputByMode measures write throughput across sync modes.
func BenchmarkWriteThroughputByMode(b *testing.B) {
	syncModes := []string{"Async", "Batched", "Sync"}

	for _, mode := range syncModes {
		b.Run(mode, func(b *testing.B) {
			server := setupBenchmarkServer(b)
			ctx := context.Background()

			// Configure sync mode
			switch mode {
			case "Sync":
				server.Config.Storage.SyncWrites = true
				server.Config.Raft.WriteMode = "sync"
			case "Async":
				server.Config.Storage.SyncWrites = false
				server.Config.Raft.WriteMode = "wal"
			case "Batched":
				server.Config.Storage.SyncWrites = false
				server.Config.Raft.WriteMode = "batched"
			}

			testData := generateTestData(1024)
			tracker := NewThroughputTracker()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("bench-mode-%s-%d", mode, i)

				// Execute Put operation
				if err := server.Put(ctx, key, testData); err != nil {
					b.Fatalf("Put failed: %v", err)
				}

				tracker.RecordOp(1024, true)
			}

			b.StopTimer()

			stats := tracker.GetStats()
			reportThroughputStats(b, stats)

			b.Logf("%s mode write throughput: %.2f ops/sec", mode, stats.OpsPerSec)
		})
	}
}
