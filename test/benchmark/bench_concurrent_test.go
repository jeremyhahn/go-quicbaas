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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkConcurrentReads measures read scalability with multiple clients.
func BenchmarkConcurrentReads(b *testing.B) {
	concurrencyLevels := []int{1, 10, 50, 100, 500, 1000}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Clients-%d", concurrency), func(b *testing.B) {
			benchmarkConcurrentReads(b, concurrency)
		})
	}
}

func benchmarkConcurrentReads(b *testing.B, concurrency int) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-concurrent-read-%d", i)
		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx
	}

	runner := NewConcurrentRunner(concurrency, b.N/concurrency)
	tracker := NewThroughputTracker()
	latencyTracker := NewLatencyTracker()

	b.ResetTimer()

	err := runner.Run(func(workerID, iteration int) error {
		keyIdx := (workerID*10000 + iteration) % numKeys
		key := fmt.Sprintf("bench-concurrent-read-%d", keyIdx)

		start := time.Now()

		// TODO: Implement Get operation
		_ = key
		_ = ctx

		latency := time.Since(start)
		latencyTracker.Record(latency)
		tracker.RecordOp(1024, true)
		return nil
	})

	b.StopTimer()

	if err != nil {
		b.Fatalf("concurrent execution failed: %v", err)
	}

	stats := tracker.GetStats()
	latencyStats := latencyTracker.GetStats()

	reportThroughputStats(b, stats)
	reportLatencyStats(b, latencyStats)

	// Calculate scalability metric
	b.ReportMetric(float64(concurrency), "concurrency")
	b.Logf("Concurrent reads (%d clients): %.2f ops/sec, P99: %v",
		concurrency, stats.OpsPerSec, latencyStats.P99)
}

// BenchmarkConcurrentWrites measures write scalability with multiple clients.
func BenchmarkConcurrentWrites(b *testing.B) {
	concurrencyLevels := []int{1, 10, 50, 100, 500}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Clients-%d", concurrency), func(b *testing.B) {
			benchmarkConcurrentWrites(b, concurrency)
		})
	}
}

func benchmarkConcurrentWrites(b *testing.B, concurrency int) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	testData := generateTestData(1024)
	runner := NewConcurrentRunner(concurrency, b.N/concurrency)
	tracker := NewThroughputTracker()
	latencyTracker := NewLatencyTracker()

	b.ResetTimer()

	err := runner.Run(func(workerID, iteration int) error {
		key := fmt.Sprintf("bench-concurrent-write-%d-%d", workerID, iteration)

		start := time.Now()

		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx

		latency := time.Since(start)
		latencyTracker.Record(latency)
		tracker.RecordOp(len(testData), true)
		return nil
	})

	b.StopTimer()

	if err != nil {
		b.Fatalf("concurrent execution failed: %v", err)
	}

	stats := tracker.GetStats()
	latencyStats := latencyTracker.GetStats()

	reportThroughputStats(b, stats)
	reportLatencyStats(b, latencyStats)

	b.ReportMetric(float64(concurrency), "concurrency")
	b.Logf("Concurrent writes (%d clients): %.2f ops/sec, P99: %v",
		concurrency, stats.OpsPerSec, latencyStats.P99)
}

// BenchmarkConcurrentMixed measures mixed read/write scalability.
func BenchmarkConcurrentMixed(b *testing.B) {
	configurations := []struct {
		concurrency int
		readPct     int
	}{
		{10, 70},
		{50, 70},
		{100, 70},
		{10, 90},
		{50, 90},
		{100, 90},
	}

	for _, config := range configurations {
		b.Run(fmt.Sprintf("C%d-R%d", config.concurrency, config.readPct), func(b *testing.B) {
			benchmarkConcurrentMixed(b, config.concurrency, config.readPct)
		})
	}
}

func benchmarkConcurrentMixed(b *testing.B, concurrency, readPercentage int) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-mixed-%d", i)
		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx
	}

	runner := NewConcurrentRunner(concurrency, b.N/concurrency)
	tracker := NewThroughputTracker()
	var readCount, writeCount atomic.Int64

	b.ResetTimer()

	err := runner.Run(func(workerID, iteration int) error {
		isRead := (iteration % 100) < readPercentage

		if isRead {
			keyIdx := (workerID*10000 + iteration) % numKeys
			key := fmt.Sprintf("bench-mixed-%d", keyIdx)

			// TODO: Implement Get operation
			_ = key
			_ = ctx

			readCount.Add(1)
		} else {
			key := fmt.Sprintf("bench-mixed-write-%d-%d", workerID, iteration)

			// TODO: Implement Put operation
			_ = key
			_ = testData
			_ = ctx

			writeCount.Add(1)
		}

		tracker.RecordOp(1024, true)
		return nil
	})

	b.StopTimer()

	if err != nil {
		b.Fatalf("concurrent execution failed: %v", err)
	}

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)

	b.ReportMetric(float64(concurrency), "concurrency")
	b.ReportMetric(float64(readCount.Load()), "reads")
	b.ReportMetric(float64(writeCount.Load()), "writes")
}

// BenchmarkScalability measures throughput scaling with client count.
func BenchmarkScalability(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-scale-%d", i)
		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx
	}

	concurrencyLevels := []int{1, 2, 4, 8, 16, 32, 64, 128}
	baselineOps := 0.0

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("C%d", concurrency), func(b *testing.B) {
			runner := NewConcurrentRunner(concurrency, b.N/concurrency)
			tracker := NewThroughputTracker()

			b.ResetTimer()

			err := runner.Run(func(workerID, iteration int) error {
				keyIdx := (workerID*10000 + iteration) % numKeys
				key := fmt.Sprintf("bench-scale-%d", keyIdx)

				// TODO: Implement Get operation
				_ = key
				_ = ctx

				tracker.RecordOp(1024, true)
				return nil
			})

			b.StopTimer()

			if err != nil {
				b.Fatalf("concurrent execution failed: %v", err)
			}

			stats := tracker.GetStats()
			reportThroughputStats(b, stats)

			// Calculate scalability factor
			if concurrency == 1 {
				baselineOps = stats.OpsPerSec
			}
			scaleFactor := stats.OpsPerSec / baselineOps
			efficiency := scaleFactor / float64(concurrency) * 100

			b.ReportMetric(scaleFactor, "scale-factor")
			b.ReportMetric(efficiency, "efficiency-%")

			b.Logf("Concurrency %d: %.2f ops/sec, %.2fx scaling, %.1f%% efficiency",
				concurrency, stats.OpsPerSec, scaleFactor, efficiency)
		})
	}
}

// BenchmarkContentionLevels measures performance under different contention levels.
func BenchmarkContentionLevels(b *testing.B) {
	contentionLevels := []struct {
		name        string
		keySpace    int
		concurrency int
	}{
		{"NoContention", 100000, 100},
		{"LowContention", 10000, 100},
		{"MediumContention", 1000, 100},
		{"HighContention", 100, 100},
		{"ExtremeContention", 10, 100},
	}

	for _, level := range contentionLevels {
		b.Run(level.name, func(b *testing.B) {
			benchmarkContentionLevel(b, level.keySpace, level.concurrency)
		})
	}
}

func benchmarkContentionLevel(b *testing.B, keySpace, concurrency int) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate key space
	testData := generateTestData(1024)
	for i := 0; i < keySpace; i++ {
		key := fmt.Sprintf("bench-contention-%d", i)
		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx
	}

	runner := NewConcurrentRunner(concurrency, b.N/concurrency)
	tracker := NewThroughputTracker()
	latencyTracker := NewLatencyTracker()
	var conflicts atomic.Int64

	b.ResetTimer()

	err := runner.Run(func(workerID, iteration int) error {
		keyIdx := (workerID*iteration + iteration) % keySpace
		key := fmt.Sprintf("bench-contention-%d", keyIdx)

		start := time.Now()

		// TODO: Implement Put operation with optimistic locking
		// Track conflicts
		_ = key
		_ = testData
		_ = ctx

		latency := time.Since(start)
		latencyTracker.Record(latency)
		tracker.RecordOp(len(testData), true)
		return nil
	})

	b.StopTimer()

	if err != nil {
		b.Fatalf("concurrent execution failed: %v", err)
	}

	stats := tracker.GetStats()
	latencyStats := latencyTracker.GetStats()

	reportThroughputStats(b, stats)
	reportLatencyStats(b, latencyStats)

	conflictRate := float64(conflicts.Load()) / float64(b.N) * 100
	b.ReportMetric(float64(conflicts.Load()), "conflicts")
	b.ReportMetric(conflictRate, "conflict-rate-%")

	b.Logf("Key space: %d, Conflicts: %d (%.2f%%)",
		keySpace, conflicts.Load(), conflictRate)
}

// BenchmarkLockContention measures lock contention impact.
func BenchmarkLockContention(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Use small key space to force contention
	numKeys := 10
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-lock-%d", i)
		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx
	}

	concurrencyLevels := []int{1, 10, 50, 100}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("C%d", concurrency), func(b *testing.B) {
			runner := NewConcurrentRunner(concurrency, b.N/concurrency)
			latencyTracker := NewLatencyTracker()

			b.ResetTimer()

			err := runner.Run(func(workerID, iteration int) error {
				keyIdx := iteration % numKeys
				key := fmt.Sprintf("bench-lock-%d", keyIdx)

				start := time.Now()

				// TODO: Implement Put operation (forces lock contention)
				_ = key
				_ = testData
				_ = ctx

				latency := time.Since(start)
				latencyTracker.Record(latency)
				return nil
			})

			b.StopTimer()

			if err != nil {
				b.Fatalf("concurrent execution failed: %v", err)
			}

			stats := latencyTracker.GetStats()
			reportLatencyStats(b, stats)

			b.Logf("Concurrency %d under high contention: P99=%v",
				concurrency, stats.P99)
		})
	}
}

// BenchmarkParallelism measures CPU parallelism efficiency.
func BenchmarkParallelism(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-parallel-%d", i)
		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			keyIdx := i % numKeys
			key := fmt.Sprintf("bench-parallel-%d", keyIdx)

			// TODO: Implement Get operation
			_ = key
			_ = ctx

			i++
		}
	})
}

// BenchmarkConcurrentBatch measures concurrent batch operation performance.
func BenchmarkConcurrentBatch(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	concurrency := 50
	batchSize := 100
	testData := generateTestData(1024)

	runner := NewConcurrentRunner(concurrency, b.N/(concurrency*batchSize))
	tracker := NewThroughputTracker()

	b.ResetTimer()

	err := runner.Run(func(workerID, iteration int) error {
		// Prepare batch
		keys := make([]string, batchSize)
		values := make([][]byte, batchSize)

		for i := 0; i < batchSize; i++ {
			keys[i] = fmt.Sprintf("bench-concurrent-batch-%d-%d-%d", workerID, iteration, i)
			values[i] = testData
		}

		// TODO: Implement BatchPut operation
		_ = keys
		_ = values
		_ = ctx

		tracker.RecordOp(batchSize*len(testData), true)
		return nil
	})

	b.StopTimer()

	if err != nil {
		b.Fatalf("concurrent execution failed: %v", err)
	}

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)
}

// BenchmarkFairness measures operation fairness under load.
func BenchmarkFairness(b *testing.B) {
	_ = setupBenchmarkServer(b) // TODO: use server
	ctx := context.Background()

	// Pre-populate database
	numKeys := 1000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-fairness-%d", i)
		// TODO: Implement Put operation
		_ = key
		_ = testData
		_ = ctx
	}

	concurrency := 10
	workerLatencies := make([][]time.Duration, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	opsPerWorker := b.N / concurrency

	b.ResetTimer()

	wg.Add(concurrency)
	for w := 0; w < concurrency; w++ {
		go func(workerID int) {
			defer wg.Done()

			latencies := make([]time.Duration, 0, opsPerWorker)

			for i := 0; i < opsPerWorker; i++ {
				keyIdx := (workerID*opsPerWorker + i) % numKeys
				key := fmt.Sprintf("bench-fairness-%d", keyIdx)

				start := time.Now()

				// TODO: Implement Get operation
				_ = key
				_ = ctx

				latency := time.Since(start)
				latencies = append(latencies, latency)
			}

			mu.Lock()
			workerLatencies[workerID] = latencies
			mu.Unlock()
		}(w)
	}

	wg.Wait()
	b.StopTimer()

	// Analyze fairness: compare P99 latencies across workers
	for workerID, latencies := range workerLatencies {
		stats := calculateLatencyStats(latencies)
		b.Logf("Worker %d: P50=%v, P99=%v", workerID, stats.P50, stats.P99)
	}

	// Calculate fairness metric (coefficient of variation of P99 latencies)
	var p99Values []time.Duration
	for _, latencies := range workerLatencies {
		stats := calculateLatencyStats(latencies)
		p99Values = append(p99Values, stats.P99)
	}

	fairnessStats := calculateLatencyStats(p99Values)
	fairnessCV := float64(fairnessStats.Avg) / float64(fairnessStats.P50)
	b.ReportMetric(fairnessCV, "fairness-cv")

	b.Logf("Fairness coefficient of variation: %.2f (lower is more fair)", fairnessCV)
}
