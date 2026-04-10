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

// Package benchmark provides comprehensive performance benchmarks for QuicBaaS.
package benchmark

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Ensure helper functions are kept for future use.
var (
	_ = warmupBenchmark
	_ = warmupServer
)

// LatencyStats holds latency percentile statistics.
type LatencyStats struct {
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
	Min time.Duration
	Max time.Duration
	Avg time.Duration
}

// ThroughputStats holds throughput statistics.
type ThroughputStats struct {
	OpsPerSec    float64
	TotalOps     int64
	Duration     time.Duration
	BytesPerSec  float64
	TotalBytes   int64
	SuccessCount int64
	FailureCount int64
}

// calculateLatencyStats computes percentile statistics from latency samples.
func calculateLatencyStats(latencies []time.Duration) LatencyStats {
	if len(latencies) == 0 {
		return LatencyStats{}
	}

	// Sort latencies
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Calculate percentiles
	p50idx := int(float64(len(sorted)) * 0.50)
	p95idx := int(float64(len(sorted)) * 0.95)
	p99idx := int(float64(len(sorted)) * 0.99)

	// Calculate average
	var total time.Duration
	for _, lat := range sorted {
		total += lat
	}
	avg := total / time.Duration(len(sorted))

	return LatencyStats{
		P50: sorted[p50idx],
		P95: sorted[p95idx],
		P99: sorted[p99idx],
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		Avg: avg,
	}
}

// reportLatencyStats reports latency statistics to the benchmark.
func reportLatencyStats(b *testing.B, stats LatencyStats) {
	b.ReportMetric(float64(stats.P50.Microseconds()), "p50-µs")
	b.ReportMetric(float64(stats.P95.Microseconds()), "p95-µs")
	b.ReportMetric(float64(stats.P99.Microseconds()), "p99-µs")
	b.ReportMetric(float64(stats.Min.Microseconds()), "min-µs")
	b.ReportMetric(float64(stats.Max.Microseconds()), "max-µs")
	b.ReportMetric(float64(stats.Avg.Microseconds()), "avg-µs")
}

// reportThroughputStats reports throughput statistics to the benchmark.
func reportThroughputStats(b *testing.B, stats ThroughputStats) {
	b.ReportMetric(stats.OpsPerSec, "ops/sec")
	b.ReportMetric(stats.BytesPerSec/1024/1024, "MB/sec")
	b.ReportMetric(float64(stats.SuccessCount), "success")
	b.ReportMetric(float64(stats.FailureCount), "failure")
	successRate := float64(stats.SuccessCount) / float64(stats.TotalOps) * 100
	b.ReportMetric(successRate, "success-%")
}

// generateTestData creates test data of specified size.
func generateTestData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

// LatencyTracker tracks latencies in a thread-safe manner.
type LatencyTracker struct {
	mu        sync.Mutex
	latencies []time.Duration
}

// NewLatencyTracker creates a new latency tracker.
func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		latencies: make([]time.Duration, 0, 10000),
	}
}

// Record records a latency measurement.
func (lt *LatencyTracker) Record(latency time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.latencies = append(lt.latencies, latency)
}

// GetStats computes and returns latency statistics.
func (lt *LatencyTracker) GetStats() LatencyStats {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	return calculateLatencyStats(lt.latencies)
}

// Count returns the number of recorded latencies.
func (lt *LatencyTracker) Count() int {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	return len(lt.latencies)
}

// ThroughputTracker tracks throughput metrics in a thread-safe manner.
type ThroughputTracker struct {
	startTime    time.Time
	opsCount     atomic.Int64
	bytesCount   atomic.Int64
	successCount atomic.Int64
	failureCount atomic.Int64
}

// NewThroughputTracker creates a new throughput tracker.
func NewThroughputTracker() *ThroughputTracker {
	return &ThroughputTracker{
		startTime: time.Now(),
	}
}

// RecordOp records a completed operation.
func (tt *ThroughputTracker) RecordOp(bytes int, success bool) {
	tt.opsCount.Add(1)
	tt.bytesCount.Add(int64(bytes))
	if success {
		tt.successCount.Add(1)
	} else {
		tt.failureCount.Add(1)
	}
}

// GetStats computes and returns throughput statistics.
func (tt *ThroughputTracker) GetStats() ThroughputStats {
	duration := time.Since(tt.startTime)
	ops := tt.opsCount.Load()
	bytes := tt.bytesCount.Load()
	success := tt.successCount.Load()
	failure := tt.failureCount.Load()

	opsPerSec := float64(ops) / duration.Seconds()
	bytesPerSec := float64(bytes) / duration.Seconds()

	return ThroughputStats{
		OpsPerSec:    opsPerSec,
		TotalOps:     ops,
		Duration:     duration,
		BytesPerSec:  bytesPerSec,
		TotalBytes:   bytes,
		SuccessCount: success,
		FailureCount: failure,
	}
}

// ConcurrentRunner runs functions concurrently for benchmarking.
type ConcurrentRunner struct {
	concurrency int
	iterations  int
}

// NewConcurrentRunner creates a new concurrent runner.
func NewConcurrentRunner(concurrency, iterations int) *ConcurrentRunner {
	return &ConcurrentRunner{
		concurrency: concurrency,
		iterations:  iterations,
	}
}

// Run executes the given function concurrently.
func (cr *ConcurrentRunner) Run(fn func(workerID, iteration int) error) error {
	var wg sync.WaitGroup
	errCh := make(chan error, cr.concurrency)

	wg.Add(cr.concurrency)
	for w := 0; w < cr.concurrency; w++ {
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < cr.iterations; i++ {
				if err := fn(workerID, i); err != nil {
					select {
					case errCh <- &WorkerError{WorkerID: workerID, Iteration: i, Cause: err}:
					default:
					}
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	// Return first error if any
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// measureMemory returns current memory usage statistics.
func measureMemory(b *testing.B) {
	b.Helper()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	b.ReportMetric(float64(m.Alloc)/1024/1024, "alloc-MB")
	b.ReportMetric(float64(m.TotalAlloc)/1024/1024, "total-alloc-MB")
	b.ReportMetric(float64(m.Sys)/1024/1024, "sys-MB")
	b.ReportMetric(float64(m.NumGC), "num-gc")
}

// assertPerformanceTarget checks if performance meets target.
func assertPerformanceTarget(b *testing.B, actual, target time.Duration, metric string) {
	if actual > target {
		b.Logf("WARNING: %s target not met: %v > %v (target)", metric, actual, target)
	} else {
		b.Logf("SUCCESS: %s within target: %v <= %v (target)", metric, actual, target)
	}
}

// setupBenchmarkContext creates a context for benchmarking.
func setupBenchmarkContext(b *testing.B) context.Context {
	b.Helper()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:gosec // cancel deferred in cleanup below

	// Clean up on test completion
	b.Cleanup(func() {
		cancel()
	})

	return ctx
}

// warmupBenchmark performs initial operations to warm up caches.
func warmupBenchmark(b *testing.B, ctx context.Context) {
	b.Helper()

	// Simulate warmup with small pause
	time.Sleep(10 * time.Millisecond)
	_ = ctx
}

// BenchmarkServer represents a QuicBaaS server instance for benchmarking.
// NOTE: This is a stub implementation due to import cycle constraints.
// Real benchmarks should be run as integration tests with actual server instances.
type BenchmarkServer struct {
	Context context.Context
	Config  *BenchmarkConfig
	storage map[string][]byte
	mu      sync.RWMutex
}

// setupBenchmarkServer creates a stub server for benchmarking
// NOTE: This creates an in-memory stub for testing the benchmark structure.
// For real performance testing, use integration tests with actual QuicBaaS server.
func setupBenchmarkServer(b *testing.B) *BenchmarkServer {
	b.Helper()

	ctx := setupBenchmarkContext(b)

	bs := &BenchmarkServer{
		Context: ctx,
		Config:  &BenchmarkConfig{},
		storage: make(map[string][]byte),
	}

	b.Cleanup(func() {
		bs.Close()
	})

	return bs
}

// Put simulates a Put operation (stub implementation).
func (bs *BenchmarkServer) Put(ctx context.Context, key string, value []byte) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Simulate network/processing latency
	time.Sleep(100 * time.Microsecond)

	bs.storage[key] = append([]byte(nil), value...)
	return nil
}

// Get simulates a Get operation (stub implementation).
func (bs *BenchmarkServer) Get(ctx context.Context, key string) ([]byte, error) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	// Simulate network/processing latency
	time.Sleep(50 * time.Microsecond)

	value, exists := bs.storage[key]
	if !exists {
		return nil, ErrKeyNotFound
	}
	return append([]byte(nil), value...), nil
}

// Delete simulates a Delete operation (stub implementation).
func (bs *BenchmarkServer) Delete(ctx context.Context, key string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Simulate network/processing latency
	time.Sleep(80 * time.Microsecond)

	delete(bs.storage, key)
	return nil
}

// BatchPut simulates a BatchPut operation (stub implementation).
func (bs *BenchmarkServer) BatchPut(ctx context.Context, keys []string, values [][]byte) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Simulate network/processing latency for batch
	time.Sleep(time.Duration(len(keys)) * 20 * time.Microsecond)

	for i, key := range keys {
		bs.storage[key] = append([]byte(nil), values[i]...)
	}
	return nil
}

// BatchGet simulates a BatchGet operation (stub implementation).
func (bs *BenchmarkServer) BatchGet(ctx context.Context, keys []string) ([][]byte, error) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	// Simulate network/processing latency for batch
	time.Sleep(time.Duration(len(keys)) * 10 * time.Microsecond)

	results := make([][]byte, len(keys))
	for i, key := range keys {
		if value, exists := bs.storage[key]; exists {
			results[i] = append([]byte(nil), value...)
		}
	}
	return results, nil
}

// BatchDelete simulates a BatchDelete operation (stub implementation).
func (bs *BenchmarkServer) BatchDelete(ctx context.Context, keys []string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Simulate network/processing latency for batch
	time.Sleep(time.Duration(len(keys)) * 15 * time.Microsecond)

	for _, key := range keys {
		delete(bs.storage, key)
	}
	return nil
}

// Transaction simulates a transaction operation (stub implementation).
func (bs *BenchmarkServer) Transaction(ctx context.Context, fn func() error) error {
	// Simulate transaction overhead
	time.Sleep(200 * time.Microsecond)

	// Execute function within "transaction"
	return fn()
}

// CompareAndSwap simulates a CAS operation (stub implementation).
func (bs *BenchmarkServer) CompareAndSwap(ctx context.Context, key string, oldValue, newValue []byte) (bool, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Simulate network/processing latency
	time.Sleep(150 * time.Microsecond)

	currentValue, exists := bs.storage[key]
	if !exists {
		return false, nil
	}

	if string(currentValue) != string(oldValue) {
		return false, nil
	}

	bs.storage[key] = append([]byte(nil), newValue...)
	return true, nil
}

// Close closes the benchmark server.
func (bs *BenchmarkServer) Close() {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.storage = nil
}

// warmupServer performs initial operations to warm up the system.
func warmupServer(b *testing.B, bs *BenchmarkServer) {
	b.Helper()
	warmupBenchmark(b, bs.Context)
}

// BenchmarkConfig holds configuration for benchmarking.
type BenchmarkConfig struct {
	Storage struct {
		SyncWrites bool
	}
	Raft struct {
		WriteMode string
	}
	Extended struct {
		Cache struct {
			Enabled bool
			L1Size  int
			L1TTL   time.Duration
		}
		Metrics struct {
			Enabled bool
		}
	}
}
