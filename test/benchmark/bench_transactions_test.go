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
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkTransactionLatency measures transaction latency with varying operation counts.
func BenchmarkTransactionLatency(b *testing.B) {
	operationCounts := []int{2, 5, 10, 20}

	for _, opCount := range operationCounts {
		b.Run(fmt.Sprintf("Ops-%d", opCount), func(b *testing.B) {
			benchmarkTransactionLatency(b, opCount)
		})
	}
}

func benchmarkTransactionLatency(b *testing.B, operationCount int) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 1000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-txn-lat-%d", i)
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewLatencyTracker()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		start := time.Now()

		// Execute transaction with multiple operations
		err := server.Transaction(ctx, func() error {
			for j := 0; j < operationCount; j++ {
				keyIdx := (i*operationCount + j) % numKeys
				key := fmt.Sprintf("bench-txn-lat-%d", keyIdx)

				// Read-modify-write pattern
				value, err := server.Get(ctx, key)
				if err != nil {
					return err
				}

				// Modify value
				if err := server.Put(ctx, key, value); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("Transaction failed: %v", err)
		}

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)

	// Report per-operation latency
	avgPerOp := stats.Avg / time.Duration(operationCount)
	b.ReportMetric(float64(avgPerOp.Microseconds()), "avg-per-op-µs")
}

// BenchmarkTransactionConflicts measures transaction performance under contention.
func BenchmarkTransactionConflicts(b *testing.B) {
	conflictLevels := []string{"None", "Low", "Medium", "High"}

	for _, level := range conflictLevels {
		b.Run(level, func(b *testing.B) {
			benchmarkTransactionConflicts(b, level)
		})
	}
}

func benchmarkTransactionConflicts(b *testing.B, conflictLevel string) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Determine key space size based on conflict level
	var keySpaceSize int
	switch conflictLevel {
	case "None":
		keySpaceSize = b.N // Unique key per transaction
	case "Low":
		keySpaceSize = b.N / 10 // 10 transactions per key
	case "Medium":
		keySpaceSize = b.N / 100 // 100 transactions per key
	case "High":
		keySpaceSize = 100 // Fixed small key space
	}

	if keySpaceSize == 0 {
		keySpaceSize = 1
	}

	// Pre-populate keys
	testData := generateTestData(1024)
	for i := 0; i < keySpaceSize; i++ {
		key := fmt.Sprintf("bench-conflict-%s-%d", conflictLevel, i)
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	var successCount, conflictCount atomic.Int64
	tracker := NewLatencyTracker()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		keyIdx := i % keySpaceSize
		key := fmt.Sprintf("bench-conflict-%s-%d", conflictLevel, keyIdx)

		start := time.Now()

		// Execute compare-and-swap operation
		oldValue, err := server.Get(ctx, key)
		if err == nil {
			success, err := server.CompareAndSwap(ctx, key, oldValue, testData)
			if err == nil && success {
				successCount.Add(1)
			} else {
				conflictCount.Add(1)
			}
		} else {
			conflictCount.Add(1)
		}

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)

	totalOps := successCount.Load() + conflictCount.Load()
	conflictRate := float64(conflictCount.Load()) / float64(totalOps) * 100

	b.ReportMetric(float64(successCount.Load()), "success")
	b.ReportMetric(float64(conflictCount.Load()), "conflicts")
	b.ReportMetric(conflictRate, "conflict-rate-%")

	b.Logf("Conflict rate: %.2f%% (%d conflicts / %d total)",
		conflictRate, conflictCount.Load(), totalOps)
}

// BenchmarkTransactionThroughput measures transaction throughput.
func BenchmarkTransactionThroughput(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-txn-throughput-%d", i)
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewThroughputTracker()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		keyIdx := i % numKeys
		key := fmt.Sprintf("bench-txn-throughput-%d", keyIdx)

		// Execute transaction
		err := server.Transaction(ctx, func() error {
			return server.Put(ctx, key, testData)
		})
		if err != nil {
			b.Fatalf("Transaction failed: %v", err)
		}

		tracker.RecordOp(1024, true)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)
}

// BenchmarkTransactionComplexity measures latency vs transaction complexity.
func BenchmarkTransactionComplexity(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	complexities := []struct {
		name       string
		conditions int
		operations int
	}{
		{"Simple", 1, 1},
		{"Medium", 3, 5},
		{"Complex", 5, 10},
		{"VeryComplex", 10, 20},
	}

	for _, complexity := range complexities {
		b.Run(complexity.name, func(b *testing.B) {
			// Pre-populate keys
			testData := generateTestData(1024)
			for i := 0; i < 1000; i++ {
				key := fmt.Sprintf("bench-complexity-%s-%d", complexity.name, i)
				if err := server.Put(ctx, key, testData); err != nil {
					b.Fatalf("Put failed during setup: %v", err)
				}
			}

			tracker := NewLatencyTracker()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				start := time.Now()

				// Execute transaction with specified complexity
				err := server.Transaction(ctx, func() error {
					// Perform condition checks (reads)
					for j := 0; j < complexity.conditions; j++ {
						key := fmt.Sprintf("bench-complexity-%s-%d", complexity.name, (i*complexity.conditions+j)%1000)
						if _, err := server.Get(ctx, key); err != nil {
							return err
						}
					}

					// Perform operations (writes)
					for j := 0; j < complexity.operations; j++ {
						key := fmt.Sprintf("bench-complexity-%s-%d", complexity.name, (i*complexity.operations+j)%1000)
						if err := server.Put(ctx, key, testData); err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					b.Fatalf("Transaction failed: %v", err)
				}

				latency := time.Since(start)
				tracker.Record(latency)
			}

			b.StopTimer()

			stats := tracker.GetStats()
			reportLatencyStats(b, stats)
		})
	}
}

// BenchmarkTransactionConcurrent measures concurrent transaction performance.
func BenchmarkTransactionConcurrent(b *testing.B) {
	concurrencyLevels := []int{1, 10, 50, 100}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency-%d", concurrency), func(b *testing.B) {
			benchmarkTransactionConcurrent(b, concurrency)
		})
	}
}

func benchmarkTransactionConcurrent(b *testing.B, concurrency int) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 10000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-txn-concurrent-%d", i)
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	var wg sync.WaitGroup
	tracker := NewThroughputTracker()
	opsPerWorker := b.N / concurrency

	b.ResetTimer()

	wg.Add(concurrency)
	for w := 0; w < concurrency; w++ {
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < opsPerWorker; i++ {
				keyIdx := (workerID*opsPerWorker + i) % numKeys
				key := fmt.Sprintf("bench-txn-concurrent-%d", keyIdx)

				// Execute transaction
				err := server.Transaction(ctx, func() error {
					return server.Put(ctx, key, testData)
				})
				if err != nil {
					b.Errorf("Transaction failed: %v", err)
					return
				}

				tracker.RecordOp(1024, true)
			}
		}(w)
	}

	wg.Wait()
	b.StopTimer()

	stats := tracker.GetStats()
	reportThroughputStats(b, stats)
}

// BenchmarkTransactionReadModifyWrite measures read-modify-write transaction pattern.
func BenchmarkTransactionReadModifyWrite(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Initialize counters
	numCounters := 100
	initialCounter := make([]byte, 8)
	binary.LittleEndian.PutUint64(initialCounter, 0)

	for i := 0; i < numCounters; i++ {
		key := fmt.Sprintf("counter-%d", i)
		if err := server.Put(ctx, key, initialCounter); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewLatencyTracker()
	var successCount, retryCount atomic.Int64

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		counterIdx := i % numCounters
		key := fmt.Sprintf("counter-%d", counterIdx)

		start := time.Now()

		// Atomic increment transaction with retry on conflict
		maxRetries := 10
		for retry := 0; retry < maxRetries; retry++ {
			// Read current value
			currentBytes, err := server.Get(ctx, key)
			if err != nil {
				b.Fatalf("Get failed: %v", err)
			}

			// Increment value
			currentValue := binary.LittleEndian.Uint64(currentBytes)
			newValue := currentValue + 1
			newBytes := make([]byte, 8)
			binary.LittleEndian.PutUint64(newBytes, newValue)

			// Compare-and-swap
			success, err := server.CompareAndSwap(ctx, key, currentBytes, newBytes)
			if err != nil {
				b.Fatalf("CAS failed: %v", err)
			}

			if success {
				successCount.Add(1)
				break
			}

			// Conflict - retry
			retryCount.Add(1)
		}

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)

	b.ReportMetric(float64(successCount.Load()), "success")
	b.ReportMetric(float64(retryCount.Load()), "retries")

	if successCount.Load() > 0 {
		avgRetries := float64(retryCount.Load()) / float64(successCount.Load())
		b.ReportMetric(avgRetries, "avg-retries")
	}
}

// BenchmarkTransactionOptimisticLocking measures optimistic locking performance.
func BenchmarkTransactionOptimisticLocking(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	numKeys := 1000
	testData := generateTestData(1024)

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("bench-opt-lock-%d", i)
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewLatencyTracker()
	var versionMismatches atomic.Int64

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		keyIdx := i % numKeys
		key := fmt.Sprintf("bench-opt-lock-%d", keyIdx)

		start := time.Now()

		// Optimistic locking pattern
		// 1. Read with version
		oldValue, err := server.Get(ctx, key)
		if err != nil {
			b.Fatalf("Get failed: %v", err)
		}

		// 2. Modify locally
		modifiedValue := append([]byte("modified-"), oldValue[:100]...)

		// 3. Write with version check
		success, err := server.CompareAndSwap(ctx, key, oldValue, modifiedValue)
		if err != nil {
			b.Fatalf("CAS failed: %v", err)
		}

		if !success {
			versionMismatches.Add(1)
		}

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)

	mismatchRate := float64(versionMismatches.Load()) / float64(b.N) * 100
	b.ReportMetric(float64(versionMismatches.Load()), "version-mismatches")
	b.ReportMetric(mismatchRate, "mismatch-rate-%")
}

// BenchmarkTransactionRollback measures transaction rollback performance.
func BenchmarkTransactionRollback(b *testing.B) {
	server := setupBenchmarkServer(b)
	ctx := context.Background()

	// Pre-populate database
	testData := generateTestData(1024)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("bench-rollback-%d", i)
		if err := server.Put(ctx, key, testData); err != nil {
			b.Fatalf("Put failed during setup: %v", err)
		}
	}

	tracker := NewLatencyTracker()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()

		// Execute transaction that simulates rollback scenario
		key := fmt.Sprintf("bench-rollback-%d", i%1000)

		// Read current value
		currentValue, err := server.Get(ctx, key)
		if err != nil {
			b.Fatalf("Get failed: %v", err)
		}

		// Try to update with CAS (will fail if value changed)
		wrongOldValue := append([]byte("wrong-"), currentValue[:100]...)
		newValue := append([]byte("new-"), testData[:100]...)

		// This will fail, simulating rollback
		_, err = server.CompareAndSwap(ctx, key, wrongOldValue, newValue)
		// Ignore error as we expect failure

		latency := time.Since(start)
		tracker.Record(latency)
	}

	b.StopTimer()

	stats := tracker.GetStats()
	reportLatencyStats(b, stats)
}
