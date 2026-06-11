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

package iam

import (
	"testing"
	"time"
)

func TestNewPEPMetrics(t *testing.T) {
	metrics := NewPEPMetrics()

	if metrics == nil {
		t.Fatal("NewPEPMetrics returned nil")
	}

	if metrics.latencyHistogram == nil {
		t.Error("latency histogram not initialized")
	}

	if metrics.GetChecksTotal() != 0 {
		t.Error("initial checks total should be 0")
	}
}

func TestPEPMetrics_RecordCheck(t *testing.T) {
	metrics := NewPEPMetrics()

	// Record allowed check
	metrics.RecordCheck(1*time.Microsecond, true)

	if metrics.GetChecksTotal() != 1 {
		t.Errorf("checks total = %d, want 1", metrics.GetChecksTotal())
	}

	stats := metrics.GetStats()
	if stats.ChecksAllowed != 1 {
		t.Errorf("checks allowed = %d, want 1", stats.ChecksAllowed)
	}

	if stats.ChecksDenied != 0 {
		t.Errorf("checks denied = %d, want 0", stats.ChecksDenied)
	}

	// Record denied check
	metrics.RecordCheck(2*time.Microsecond, false)

	if metrics.GetChecksTotal() != 2 {
		t.Errorf("checks total = %d, want 2", metrics.GetChecksTotal())
	}

	stats = metrics.GetStats()
	if stats.ChecksAllowed != 1 {
		t.Errorf("checks allowed = %d, want 1", stats.ChecksAllowed)
	}

	if stats.ChecksDenied != 1 {
		t.Errorf("checks denied = %d, want 1", stats.ChecksDenied)
	}
}

func TestPEPMetrics_RecordCacheHit(t *testing.T) {
	metrics := NewPEPMetrics()

	metrics.RecordCacheHit()

	stats := metrics.GetStats()
	if stats.CacheHits != 1 {
		t.Errorf("cache hits = %d, want 1", stats.CacheHits)
	}

	// Record multiple hits
	for i := 0; i < 5; i++ {
		metrics.RecordCacheHit()
	}

	stats = metrics.GetStats()
	if stats.CacheHits != 6 {
		t.Errorf("cache hits = %d, want 6", stats.CacheHits)
	}
}

func TestPEPMetrics_RecordCacheMiss(t *testing.T) {
	metrics := NewPEPMetrics()

	metrics.RecordCacheMiss()

	stats := metrics.GetStats()
	if stats.CacheMisses != 1 {
		t.Errorf("cache misses = %d, want 1", stats.CacheMisses)
	}

	// Record multiple misses
	for i := 0; i < 3; i++ {
		metrics.RecordCacheMiss()
	}

	stats = metrics.GetStats()
	if stats.CacheMisses != 4 {
		t.Errorf("cache misses = %d, want 4", stats.CacheMisses)
	}
}

func TestPEPMetrics_CacheHitRate(t *testing.T) {
	metrics := NewPEPMetrics()

	// Record 7 hits and 3 misses (70% hit rate)
	for i := 0; i < 7; i++ {
		metrics.RecordCacheHit()
	}
	for i := 0; i < 3; i++ {
		metrics.RecordCacheMiss()
	}

	stats := metrics.GetStats()
	if stats.CacheHitRate < 0.69 || stats.CacheHitRate > 0.71 {
		t.Errorf("cache hit rate = %.2f, want ~0.70", stats.CacheHitRate)
	}

	// Test GetCacheHitRate method
	rate := metrics.GetCacheHitRate()
	if rate < 0.69 || rate > 0.71 {
		t.Errorf("GetCacheHitRate() = %.2f, want ~0.70", rate)
	}
}

func TestPEPMetrics_RecordPolicyUpdate(t *testing.T) {
	metrics := NewPEPMetrics()

	metrics.RecordPolicyUpdate()

	stats := metrics.GetStats()
	if stats.PolicyUpdates != 1 {
		t.Errorf("policy updates = %d, want 1", stats.PolicyUpdates)
	}

	// Record multiple updates
	for i := 0; i < 5; i++ {
		metrics.RecordPolicyUpdate()
	}

	stats = metrics.GetStats()
	if stats.PolicyUpdates != 6 {
		t.Errorf("policy updates = %d, want 6", stats.PolicyUpdates)
	}
}

func TestPEPMetrics_RecordSync(t *testing.T) {
	metrics := NewPEPMetrics()

	// Record successful sync
	metrics.RecordSync(nil)

	stats := metrics.GetStats()
	if stats.SyncErrors != 0 {
		t.Errorf("sync errors = %d, want 0", stats.SyncErrors)
	}

	if stats.LastSyncTime.IsZero() {
		t.Error("last sync time should be set")
	}

	// Record failed sync
	metrics.RecordSync(NewPolicySyncError("test-tenant", nil))

	stats = metrics.GetStats()
	if stats.SyncErrors != 1 {
		t.Errorf("sync errors = %d, want 1", stats.SyncErrors)
	}
}

func TestPEPMetrics_AverageLatency(t *testing.T) {
	metrics := NewPEPMetrics()

	// Record checks with known latencies
	metrics.RecordCheck(1*time.Microsecond, true)
	metrics.RecordCheck(2*time.Microsecond, true)
	metrics.RecordCheck(3*time.Microsecond, true)

	// Average should be 2μs
	avgLatency := metrics.GetAverageLatency()
	if avgLatency != 2*time.Microsecond {
		t.Errorf("average latency = %v, want 2μs", avgLatency)
	}

	stats := metrics.GetStats()
	if stats.AverageLatencyNs != 2000 {
		t.Errorf("average latency ns = %d, want 2000", stats.AverageLatencyNs)
	}
}

func TestPEPMetrics_Percentiles(t *testing.T) {
	metrics := NewPEPMetrics()

	// Record checks with varied latencies
	latencies := []time.Duration{
		1 * time.Microsecond,
		2 * time.Microsecond,
		3 * time.Microsecond,
		4 * time.Microsecond,
		5 * time.Microsecond,
		6 * time.Microsecond,
		7 * time.Microsecond,
		8 * time.Microsecond,
		9 * time.Microsecond,
		10 * time.Microsecond,
	}

	for _, latency := range latencies {
		metrics.RecordCheck(latency, true)
	}

	stats := metrics.GetStats()

	// P50 should be around 5-6μs
	if stats.P50LatencyNs < 4000 || stats.P50LatencyNs > 6000 {
		t.Errorf("P50 latency = %d ns, want ~5000ns", stats.P50LatencyNs)
	}

	// P95 should be around 9-10μs
	if stats.P95LatencyNs < 8000 || stats.P95LatencyNs > 10000 {
		t.Errorf("P95 latency = %d ns, want ~9000ns", stats.P95LatencyNs)
	}

	// P99 should be around 10μs
	if stats.P99LatencyNs < 9000 || stats.P99LatencyNs > 10000 {
		t.Errorf("P99 latency = %d ns, want ~10000ns", stats.P99LatencyNs)
	}
}

func TestPEPMetrics_Reset(t *testing.T) {
	metrics := NewPEPMetrics()

	// Record some metrics
	metrics.RecordCheck(1*time.Microsecond, true)
	metrics.RecordCheck(2*time.Microsecond, false)
	metrics.RecordCacheHit()
	metrics.RecordCacheMiss()
	metrics.RecordPolicyUpdate()

	// Verify metrics are not zero
	if metrics.GetChecksTotal() == 0 {
		t.Error("checks total should not be 0 before reset")
	}

	// Reset
	metrics.Reset()

	// Verify all metrics are zero
	stats := metrics.GetStats()
	if stats.ChecksTotal != 0 {
		t.Errorf("checks total = %d after reset, want 0", stats.ChecksTotal)
	}
	if stats.ChecksAllowed != 0 {
		t.Errorf("checks allowed = %d after reset, want 0", stats.ChecksAllowed)
	}
	if stats.ChecksDenied != 0 {
		t.Errorf("checks denied = %d after reset, want 0", stats.ChecksDenied)
	}
	if stats.CacheHits != 0 {
		t.Errorf("cache hits = %d after reset, want 0", stats.CacheHits)
	}
	if stats.CacheMisses != 0 {
		t.Errorf("cache misses = %d after reset, want 0", stats.CacheMisses)
	}
	if stats.PolicyUpdates != 0 {
		t.Errorf("policy updates = %d after reset, want 0", stats.PolicyUpdates)
	}
}

func TestLatencyHistogram_AddPercentile(t *testing.T) {
	histogram := NewLatencyHistogram(100)

	// Add some values
	values := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for _, v := range values {
		histogram.Add(v)
	}

	// P50 should be 5
	p50 := histogram.Percentile(0.5)
	if p50 != 5 {
		t.Errorf("P50 = %d, want 5", p50)
	}

	// P95 should be 9 or 10
	p95 := histogram.Percentile(0.95)
	if p95 < 9 || p95 > 10 {
		t.Errorf("P95 = %d, want 9-10", p95)
	}

	// P0 should be 1
	p0 := histogram.Percentile(0.0)
	if p0 != 1 {
		t.Errorf("P0 = %d, want 1", p0)
	}

	// P100 should be 10
	p100 := histogram.Percentile(1.0)
	if p100 != 10 {
		t.Errorf("P100 = %d, want 10", p100)
	}
}

func TestLatencyHistogram_Size(t *testing.T) {
	histogram := NewLatencyHistogram(10)

	if histogram.Size() != 0 {
		t.Errorf("initial size = %d, want 0", histogram.Size())
	}

	// Add values
	for i := 0; i < 5; i++ {
		histogram.Add(int64(i))
	}

	if histogram.Size() != 5 {
		t.Errorf("size = %d, want 5", histogram.Size())
	}
}

func TestLatencyHistogram_MaxSamples(t *testing.T) {
	maxSamples := 10
	histogram := NewLatencyHistogram(maxSamples)

	// Add more samples than max
	for i := 0; i < 20; i++ {
		histogram.Add(int64(i))
	}

	// Should not exceed max samples
	if histogram.Size() > maxSamples {
		t.Errorf("size = %d, want <= %d", histogram.Size(), maxSamples)
	}

	// Latest samples should be kept (10-19)
	p0 := histogram.Percentile(0.0)
	if p0 < 10 {
		t.Errorf("P0 = %d, should be >= 10 (oldest samples should be evicted)", p0)
	}
}

func TestLatencyHistogram_Clear(t *testing.T) {
	histogram := NewLatencyHistogram(100)

	// Add values
	for i := 0; i < 10; i++ {
		histogram.Add(int64(i))
	}

	if histogram.Size() != 10 {
		t.Errorf("size = %d, want 10", histogram.Size())
	}

	// Clear
	histogram.Clear()

	if histogram.Size() != 0 {
		t.Errorf("size = %d after clear, want 0", histogram.Size())
	}

	// Percentile on empty histogram should return 0
	p50 := histogram.Percentile(0.5)
	if p50 != 0 {
		t.Errorf("P50 on empty histogram = %d, want 0", p50)
	}
}

// Benchmark tests

func BenchmarkPEPMetrics_RecordCheck(b *testing.B) {
	metrics := NewPEPMetrics()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		metrics.RecordCheck(1*time.Microsecond, true)
	}
}

func BenchmarkPEPMetrics_GetStats(b *testing.B) {
	metrics := NewPEPMetrics()

	// Add some data
	for i := 0; i < 1000; i++ {
		metrics.RecordCheck(time.Duration(i)*time.Nanosecond, i%2 == 0)
		metrics.RecordCacheHit()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = metrics.GetStats()
	}
}

func BenchmarkLatencyHistogram_Add(b *testing.B) {
	histogram := NewLatencyHistogram(10000)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		histogram.Add(int64(i % 10000))
	}
}

func BenchmarkLatencyHistogram_Percentile(b *testing.B) {
	histogram := NewLatencyHistogram(10000)

	// Fill with data
	for i := 0; i < 10000; i++ {
		histogram.Add(int64(i))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = histogram.Percentile(0.95)
	}
}
