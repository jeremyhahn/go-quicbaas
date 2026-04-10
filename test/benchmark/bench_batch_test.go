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

// BenchmarkBatchWrite measures batch write performance with different batch sizes.
func BenchmarkBatchWrite(b *testing.B) {
	batchSizes := []int{10, 50, 100, 500, 1000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize-%d", batchSize), func(b *testing.B) {
			benchmarkBatchWrite(b, batchSize)
		})
	}
}

func benchmarkBatchWrite(b *testing.B, batchSize int) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	testData := generateTestData(1024)
	tracker := NewThroughputTracker()

	b.ResetTimer()
	b.ReportAllocs()

	numBatches := b.N / batchSize
	if numBatches == 0 {
		numBatches = 1
	}

	for batchNum := 0; batchNum < numBatches; batchNum++ {
		// Prepare batch
		keys := make([]string, batchSize)
		values := make([][]byte, batchSize)

		for i := 0; i < batchSize; i++ {
			keys[i] = fmt.Sprintf("bench-batch-write-%d-%d", batchNum, i)
			values[i] = testData
		}

		// Execute BatchPut operation
		if err := server.BatchPut(ctx, keys, values); err != nil {
			b.Fatalf("BatchPut failed: %v", err)
		}

		tracker.RecordOp(batchSize*len(testData), true)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)

	// Calculate effective ops/sec (individual operations, not batches)
	effectiveOps := float64(numBatches*batchSize) / stats.Duration.Seconds()
	b.ReportMetric(effectiveOps, "effective-ops/sec")
}

// BenchmarkBatchRead measures batch read performance with different batch sizes.
func BenchmarkBatchRead(b *testing.B) {
	batchSizes := []int{10, 50, 100, 500, 1000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize-%d", batchSize), func(b *testing.B) {
			benchmarkBatchRead(b, batchSize)
		})
	}
}

func benchmarkBatchRead(b *testing.B, batchSize int) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)
	allKeys := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		allKeys[i] = fmt.Sprintf("bench-batch-read-%d", i)
		if err := server.Put(ctx, allKeys[i], testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewThroughputTracker()

	b.ResetTimer()

	numBatches := b.N / batchSize
	if numBatches == 0 {
		numBatches = 1
	}

	for batchNum := 0; batchNum < numBatches; batchNum++ {
		// Prepare batch of keys to read
		keys := make([]string, batchSize)
		for i := 0; i < batchSize; i++ {
			keyIdx := (batchNum*batchSize + i) % numKeys
			keys[i] = allKeys[keyIdx]
		}

		// Execute BatchGet operation
		if _, err := server.BatchGet(ctx, keys); err != nil {
			b.Fatalf("BatchGet failed: %v", err)
		}

		tracker.RecordOp(batchSize*1024, true)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)

	// Calculate effective ops/sec
	effectiveOps := float64(numBatches*batchSize) / stats.Duration.Seconds()
	b.ReportMetric(effectiveOps, "effective-ops/sec")
}

// BenchmarkBatchDelete measures batch delete performance.
func BenchmarkBatchDelete(b *testing.B) {
	batchSizes := []int{10, 50, 100, 500}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize-%d", batchSize), func(b *testing.B) {
			benchmarkBatchDelete(b, batchSize)
		})
	}
}

func benchmarkBatchDelete(b *testing.B, batchSize int) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	numBatches := b.N / batchSize
	if numBatches == 0 {
		numBatches = 1
	}

	// Pre-populate database with keys to delete
	totalKeys := numBatches * batchSize
	testData := generateTestData(1024)

	for i := 0; i < totalKeys; i++ {
		key := fmt.Sprintf("bench-batch-delete-%d", i)
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewThroughputTracker()

	b.ResetTimer()

	for batchNum := 0; batchNum < numBatches; batchNum++ {
		// Prepare batch of keys to delete
		keys := make([]string, batchSize)
		for i := 0; i < batchSize; i++ {
			keys[i] = fmt.Sprintf("bench-batch-delete-%d", batchNum*batchSize+i)
		}

		// Execute BatchDelete operation
		if err := server.BatchDelete(ctx, keys); err != nil {
			b.Fatalf("BatchDelete failed: %v", err)
		}

		tracker.RecordOp(batchSize*1024, true)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)

	effectiveOps := float64(numBatches*batchSize) / stats.Duration.Seconds()
	b.ReportMetric(effectiveOps, "effective-ops/sec")
}

// BenchmarkOptimalBatchSize finds the optimal batch size for maximum throughput.
func BenchmarkOptimalBatchSize(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	batchSizes := []int{1, 5, 10, 20, 50, 100, 200, 500, 1000, 2000}
	results := make(map[int]float64)

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize-%d", batchSize), func(b *testing.B) {
			testData := generateTestData(1024)
			tracker := NewThroughputTracker()

			b.ResetTimer()

			numBatches := b.N / batchSize
			if numBatches == 0 {
				numBatches = 1
			}

			for batchNum := 0; batchNum < numBatches; batchNum++ {
				keys := make([]string, batchSize)
				values := make([][]byte, batchSize)

				for i := 0; i < batchSize; i++ {
					keys[i] = fmt.Sprintf("bench-optimal-%d-%d", batchNum, i)
					values[i] = testData
				}

				// Execute BatchPut operation
				if err := server.BatchPut(ctx, keys, values); err != nil {
					b.Fatalf("BatchPut failed: %v", err)
				}

				tracker.RecordOp(batchSize*len(testData), true)
			}

			b.StopTimer()

			stats := tracker.GetStats()
			effectiveOps := float64(numBatches*batchSize) / stats.Duration.Seconds()

			results[batchSize] = effectiveOps
			b.ReportMetric(effectiveOps, "ops/sec")
		})
	}

	// Find and report optimal batch size
	maxThroughput := 0.0
	optimalSize := 0
	for size, throughput := range results {
		if throughput > maxThroughput {
			maxThroughput = throughput
			optimalSize = size
		}
	}

	b.Logf("Optimal batch size: %d (%.2f ops/sec)", optimalSize, maxThroughput)
}

// BenchmarkBatchVsSingle compares batch vs single operation performance.
func BenchmarkBatchVsSingle(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	testData := generateTestData(1024)
	batchSize := 100

	b.Run("Single", func(b *testing.B) {
		tracker := NewThroughputTracker()

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("bench-single-%d", i)

			// Execute single Put operation
			if err := server.Put(ctx, key, testData); err != nil {
				b.Fatalf("Put failed: %v", err)
			}

			tracker.RecordOp(len(testData), true)
		}

		b.StopTimer()

		stats := tracker.GetStats()
		reportThroughputStats(b, stats)
	})

	b.Run("Batch", func(b *testing.B) {
		tracker := NewThroughputTracker()

		b.ResetTimer()

		numBatches := b.N / batchSize
		if numBatches == 0 {
			numBatches = 1
		}

		for batchNum := 0; batchNum < numBatches; batchNum++ {
			keys := make([]string, batchSize)
			values := make([][]byte, batchSize)

			for i := 0; i < batchSize; i++ {
				keys[i] = fmt.Sprintf("bench-batch-%d-%d", batchNum, i)
				values[i] = testData
			}

			// Execute BatchPut operation
			if err := server.BatchPut(ctx, keys, values); err != nil {
				b.Fatalf("BatchPut failed: %v", err)
			}

			tracker.RecordOp(batchSize*len(testData), true)
		}

		b.StopTimer()

		stats := tracker.GetStats()
		effectiveOps := float64(numBatches*batchSize) / stats.Duration.Seconds()

		b.ReportMetric(effectiveOps, "ops/sec")
		b.Logf("Batch speedup: %.2fx", effectiveOps/stats.OpsPerSec)
	})
}

// BenchmarkBatchSizeVsLatency measures latency impact of batch size.
func BenchmarkBatchSizeVsLatency(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	batchSizes := []int{1, 10, 50, 100, 500, 1000}
	testData := generateTestData(1024)

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize-%d", batchSize), func(b *testing.B) {
			latencyTracker := NewLatencyTracker()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				keys := make([]string, batchSize)
				values := make([][]byte, batchSize)

				for j := 0; j < batchSize; j++ {
					keys[j] = fmt.Sprintf("bench-latency-%d-%d", i, j)
					values[j] = testData
				}

				start := time.Now()

				// Execute BatchPut operation
				if err := server.BatchPut(ctx, keys, values); err != nil {
					b.Fatalf("BatchPut failed: %v", err)
				}

				latency := time.Since(start)
				latencyTracker.Record(latency)
			}

			b.StopTimer()

			stats := latencyTracker.GetStats()
			reportLatencyStats(b, stats)

			// Report latency per operation in batch
			avgPerOp := stats.Avg / time.Duration(batchSize)
			b.ReportMetric(float64(avgPerOp.Microseconds()), "avg-per-op-µs")
		})
	}
}

// BenchmarkBatchMixed measures mixed batch operations (read + write).
func BenchmarkBatchMixed(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-mixed-batch-%d", i)
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	batchSize := 100
	tracker := NewThroughputTracker()

	b.ResetTimer()

	numBatches := b.N / batchSize
	if numBatches == 0 {
		numBatches = 1
	}

	for batchNum := 0; batchNum < numBatches; batchNum++ {
		// Create mixed batch: 70% reads, 30% writes
		readKeys := make([]string, 70)
		for i := 0; i < 70; i++ {
			readKeys[i] = fmt.Sprintf("bench-mixed-batch-%d", (batchNum*70+i)%numKeys)
		}

		writeKeys := make([]string, 30)
		writeValues := make([][]byte, 30)
		for i := 0; i < 30; i++ {
			writeKeys[i] = fmt.Sprintf("bench-mixed-batch-new-%d-%d", batchNum, i)
			writeValues[i] = testData
		}

		// Execute BatchGet operation
		if _, err := server.BatchGet(ctx, readKeys); err != nil {
			b.Fatalf("BatchGet failed: %v", err)
		}

		// Execute BatchPut operation
		if err := server.BatchPut(ctx, writeKeys, writeValues); err != nil {
			b.Fatalf("BatchPut failed: %v", err)
		}

		tracker.RecordOp(100*1024, true)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)
}
