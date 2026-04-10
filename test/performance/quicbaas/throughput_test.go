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

// Package quicbaas provides comprehensive performance tests for QuicBaaS.
package quicbaas

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/hash"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/test/performance/internal"
)

// BenchmarkPutThroughput measures maximum write throughput.
func BenchmarkPutThroughput(b *testing.B) {
	tests := []struct {
		name      string
		workers   int
		valueSize int
	}{
		{"1worker_100b", 1, 100},
		{"10workers_100b", 10, 100},
		{"100workers_100b", 100, 100},
		{"10workers_1kb", 10, 1024},
		{"100workers_1kb", 100, 1024},
		{"10workers_10kb", 10, 10240},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			stats := runPutBenchmark(b, tt.workers, tt.valueSize, b.N)

			// Report metrics
			snapshot := stats.Snapshot()
			duration := stats.Duration()
			b.ReportMetric(snapshot.Throughput(duration), "req/sec")
			b.ReportMetric(snapshot.AvgLatencyMs, "ms/op")

			percentiles := stats.CalculatePercentiles()
			b.ReportMetric(percentiles.P99, "p99_ms")
		})
	}
}

// BenchmarkBatchPutThroughput measures batch write performance.
func BenchmarkBatchPutThroughput(b *testing.B) {
	tests := []struct {
		name      string
		batchSize int
		workers   int
	}{
		{"batch10_10workers", 10, 10},
		{"batch100_10workers", 100, 10},
		{"batch1000_10workers", 1000, 10},
		{"batch100_1worker", 100, 1},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			stats := runBatchPutBenchmark(b, tt.batchSize, tt.workers, b.N)

			snapshot := stats.Snapshot()
			duration := stats.Duration()
			b.ReportMetric(snapshot.Throughput(duration), "req/sec")
			b.ReportMetric(snapshot.AvgLatencyMs, "ms/batch")
		})
	}
}

// BenchmarkGetThroughput measures read throughput with different consistency levels.
func BenchmarkGetThroughput(b *testing.B) {
	tests := []struct {
		name        string
		workers     int
		consistency string
	}{
		{"10workers_linearizable", 10, "linearizable"},
		{"100workers_linearizable", 100, "linearizable"},
		{"10workers_eventual", 10, "eventual"},
		{"100workers_eventual", 100, "eventual"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			// Pre-populate data
			client := setupClient(b)
			prepopulateData(b, client, 10000)
			client.Close()

			stats := runGetBenchmark(b, tt.workers, tt.consistency, b.N)

			snapshot := stats.Snapshot()
			duration := stats.Duration()
			b.ReportMetric(snapshot.Throughput(duration), "req/sec")
			b.ReportMetric(snapshot.AvgLatencyMs, "ms/op")
		})
	}
}

// BenchmarkMixedWorkload measures performance under mixed read/write workloads.
func BenchmarkMixedWorkload(b *testing.B) {
	tests := []struct {
		name    string
		readPct int // percentage of reads
		workers int
	}{
		{"50read_50write_10workers", 50, 10},
		{"90read_10write_10workers", 90, 10},
		{"10read_90write_10workers", 10, 10},
		{"50read_50write_100workers", 50, 100},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			// Pre-populate for reads
			client := setupClient(b)
			prepopulateData(b, client, 10000)
			client.Close()

			stats := runMixedBenchmark(b, tt.readPct, tt.workers, b.N)

			snapshot := stats.Snapshot()
			duration := stats.Duration()
			b.ReportMetric(snapshot.Throughput(duration), "req/sec")
			b.ReportMetric(snapshot.AvgLatencyMs, "ms/op")
		})
	}
}

// Helper functions

func setupClient(b *testing.B) quicbaas.Client {
	b.Helper()

	client, err := quicbaas.NewClient("localhost:50051", quicbaas.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	// Test connectivity with health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Health().Readiness(ctx)
	if err != nil {
		client.Close()
		b.Skipf("QuicBaaS server not available: %v", err)
	}

	return client
}

func runPutBenchmark(b *testing.B, workers, valueSize, totalOps int) *internal.Stats {
	b.Helper()

	client := setupClient(b)
	defer client.Close()

	stats := internal.NewStats()
	value := make([]byte, valueSize)
	for i := range value {
		value[i] = byte('a' + (i % 26))
	}

	opsPerWorker := totalOps / workers

	b.ResetTimer()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			ctx := context.Background()
			for i := 0; i < opsPerWorker; i++ {
				key := fmt.Sprintf("bench-w%d-k%d-%d", workerID, i, rand.Int63())

				start := time.Now()
				_, err := client.KV().Put(ctx, &service.PutRequest{
					EntityType: "benchmark",
					ID:         hash.FNV1a(key),
					Data:       value,
				})
				latency := time.Since(start)

				if err != nil {
					stats.RecordFailure()
				} else {
					stats.RecordSuccess(latency)
				}
			}
		}(w)
	}

	wg.Wait()
	stats.Stop()

	return stats
}

func runBatchPutBenchmark(b *testing.B, batchSize, workers, totalBatches int) *internal.Stats {
	b.Helper()

	client := setupClient(b)
	defer client.Close()

	stats := internal.NewStats()
	batchesPerWorker := totalBatches / workers

	b.ResetTimer()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			ctx := context.Background()
			for batch := 0; batch < batchesPerWorker; batch++ {
				// Build batch of requests - execute individually as SDK doesn't have batch API yet
				start := time.Now()
				successCount := 0
				for i := 0; i < batchSize; i++ {
					key := fmt.Sprintf("batch-w%d-b%d-k%d", workerID, batch, i)
					_, err := client.KV().Put(ctx, &service.PutRequest{
						EntityType: "benchmark",
						ID:         hash.FNV1a(key),
						Data:       []byte(fmt.Sprintf("value-%d", i)),
					})
					if err == nil {
						successCount++
					}
				}
				latency := time.Since(start)

				if successCount == batchSize {
					stats.RecordSuccess(latency)
				} else {
					stats.RecordFailure()
				}
			}
		}(w)
	}

	wg.Wait()
	stats.Stop()

	return stats
}

func prepopulateData(b *testing.B, client quicbaas.Client, count int) {
	b.Helper()

	ctx := context.Background()
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("prepop-key-%d", i)
		value := []byte(fmt.Sprintf("prepop-value-%d", i))
		if _, err := client.KV().Put(ctx, &service.PutRequest{
			EntityType: "benchmark",
			ID:         hash.FNV1a(key),
			Data:       value,
		}); err != nil {
			b.Fatalf("Failed to prepopulate data: %v", err)
		}
	}
}

func runGetBenchmark(b *testing.B, workers int, consistency string, totalOps int) *internal.Stats {
	b.Helper()

	client := setupClient(b)
	defer client.Close()

	stats := internal.NewStats()
	opsPerWorker := totalOps / workers

	// Determine consistency level
	consistencyLevel := service.ConsistencyLevelLinearizable
	if consistency == "eventual" {
		consistencyLevel = service.ConsistencyLevelEventual
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			ctx := context.Background()
			for i := 0; i < opsPerWorker; i++ {
				key := fmt.Sprintf("prepop-key-%d", rand.Intn(10000))

				start := time.Now()
				_, err := client.KV().Get(ctx, &service.GetRequest{
					EntityType:  "benchmark",
					ID:          hash.FNV1a(key),
					Consistency: consistencyLevel,
				})
				latency := time.Since(start)

				if err != nil {
					stats.RecordFailure()
				} else {
					stats.RecordSuccess(latency)
				}
			}
		}(w)
	}

	wg.Wait()
	stats.Stop()

	return stats
}

func runMixedBenchmark(b *testing.B, readPct, workers, totalOps int) *internal.Stats {
	b.Helper()

	client := setupClient(b)
	defer client.Close()

	stats := internal.NewStats()
	opsPerWorker := totalOps / workers

	b.ResetTimer()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			ctx := context.Background()
			for i := 0; i < opsPerWorker; i++ {
				start := time.Now()
				var err error

				// Determine operation type
				if rand.Intn(100) < readPct {
					// Read
					key := fmt.Sprintf("prepop-key-%d", rand.Intn(10000))
					_, err = client.KV().Get(ctx, &service.GetRequest{
						EntityType: "benchmark",
						ID:         hash.FNV1a(key),
					})
				} else {
					// Write
					key := fmt.Sprintf("mixed-w%d-k%d-%d", workerID, i, rand.Int63())
					value := []byte(fmt.Sprintf("mixed-value-%d", i))
					_, err = client.KV().Put(ctx, &service.PutRequest{
						EntityType: "benchmark",
						ID:         hash.FNV1a(key),
						Data:       value,
					})
				}

				latency := time.Since(start)

				if err != nil {
					stats.RecordFailure()
				} else {
					stats.RecordSuccess(latency)
				}
			}
		}(w)
	}

	wg.Wait()
	stats.Stop()

	return stats
}
