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
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// PEPMetrics collects performance metrics for the PEP.
// It uses lock-free atomic operations for high-performance metric collection.
type PEPMetrics struct {
	// checksTotal is the total number of checks performed
	checksTotal atomic.Int64

	// checksAllowed is the number of checks that were allowed
	checksAllowed atomic.Int64

	// checksDenied is the number of checks that were denied
	checksDenied atomic.Int64

	// checkLatencySum is the sum of all check latencies in nanoseconds
	checkLatencySum atomic.Int64

	// cacheHits is the number of cache hits
	cacheHits atomic.Int64

	// cacheMisses is the number of cache misses
	cacheMisses atomic.Int64

	// policyUpdates is the number of policy updates received
	policyUpdates atomic.Int64

	// syncErrors is the number of sync errors
	syncErrors atomic.Int64

	// lastSyncTime is the timestamp of the last successful sync
	lastSyncTime atomic.Int64

	// Latency histogram for percentile calculations
	latencyHistogram *LatencyHistogram
}

// LatencyHistogram tracks latency values for percentile calculations.
type LatencyHistogram struct {
	// samples stores recent latency samples in nanoseconds
	samples []int64

	// maxSamples is the maximum number of samples to keep
	maxSamples int

	// mu protects access to samples
	mu sync.RWMutex
}

// NewLatencyHistogram creates a new latency histogram.
func NewLatencyHistogram(maxSamples int) *LatencyHistogram {
	return &LatencyHistogram{
		samples:    make([]int64, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// Add adds a latency sample to the histogram.
func (h *LatencyHistogram) Add(latencyNs int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// If at capacity, remove oldest sample
	if len(h.samples) >= h.maxSamples {
		// Shift elements left to remove first element
		copy(h.samples, h.samples[1:])
		h.samples = h.samples[:len(h.samples)-1]
	}

	h.samples = append(h.samples, latencyNs)
}

// Percentile calculates the specified percentile (0.0-1.0).
func (h *LatencyHistogram) Percentile(p float64) int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.samples) == 0 {
		return 0
	}

	// Create a sorted copy
	sorted := make([]int64, len(h.samples))
	copy(sorted, h.samples)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Calculate percentile index
	index := int(float64(len(sorted)-1) * p)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// Clear clears all samples from the histogram.
func (h *LatencyHistogram) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.samples = h.samples[:0]
}

// Size returns the number of samples in the histogram.
func (h *LatencyHistogram) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.samples)
}

// NewPEPMetrics creates a new PEP metrics collector.
func NewPEPMetrics() *PEPMetrics {
	return &PEPMetrics{
		latencyHistogram: NewLatencyHistogram(10000), // Keep last 10k samples
	}
}

// RecordCheck records a completed authorization check.
func (m *PEPMetrics) RecordCheck(duration time.Duration, allowed bool) {
	latencyNs := duration.Nanoseconds()

	m.checksTotal.Add(1)
	m.checkLatencySum.Add(latencyNs)

	if allowed {
		m.checksAllowed.Add(1)
	} else {
		m.checksDenied.Add(1)
	}

	// Add to histogram for percentile calculations
	m.latencyHistogram.Add(latencyNs)
}

// RecordCacheHit records a cache hit.
func (m *PEPMetrics) RecordCacheHit() {
	m.cacheHits.Add(1)
}

// RecordCacheMiss records a cache miss.
func (m *PEPMetrics) RecordCacheMiss() {
	m.cacheMisses.Add(1)
}

// RecordPolicyUpdate records a policy update.
func (m *PEPMetrics) RecordPolicyUpdate() {
	m.policyUpdates.Add(1)
}

// RecordSync records a policy sync operation.
func (m *PEPMetrics) RecordSync(err error) {
	if err != nil {
		m.syncErrors.Add(1)
	} else {
		m.lastSyncTime.Store(time.Now().Unix())
	}
}

// GetStats returns current PEP statistics.
func (m *PEPMetrics) GetStats() *PEPStats {
	checksTotal := m.checksTotal.Load()
	checksAllowed := m.checksAllowed.Load()
	checksDenied := m.checksDenied.Load()
	checkLatencySum := m.checkLatencySum.Load()
	cacheHits := m.cacheHits.Load()
	cacheMisses := m.cacheMisses.Load()
	policyUpdates := m.policyUpdates.Load()
	syncErrors := m.syncErrors.Load()
	lastSyncTimestamp := m.lastSyncTime.Load()

	// Calculate average latency
	var averageLatencyNs int64
	if checksTotal > 0 {
		averageLatencyNs = checkLatencySum / checksTotal
	}

	// Calculate cache hit rate
	var cacheHitRate float64
	totalCacheOps := cacheHits + cacheMisses
	if totalCacheOps > 0 {
		cacheHitRate = float64(cacheHits) / float64(totalCacheOps)
	}

	// Get percentiles from histogram
	p50 := m.latencyHistogram.Percentile(0.50)
	p95 := m.latencyHistogram.Percentile(0.95)
	p99 := m.latencyHistogram.Percentile(0.99)

	// Convert last sync timestamp
	var lastSyncTime time.Time
	if lastSyncTimestamp > 0 {
		lastSyncTime = time.Unix(lastSyncTimestamp, 0)
	}

	return &PEPStats{
		ChecksTotal:      checksTotal,
		ChecksAllowed:    checksAllowed,
		ChecksDenied:     checksDenied,
		AverageLatencyNs: averageLatencyNs,
		P50LatencyNs:     p50,
		P95LatencyNs:     p95,
		P99LatencyNs:     p99,
		CacheHits:        cacheHits,
		CacheMisses:      cacheMisses,
		CacheHitRate:     cacheHitRate,
		PolicyUpdates:    policyUpdates,
		SyncErrors:       syncErrors,
		LastSyncTime:     lastSyncTime,
		CachedPolicies:   0, // Will be set by PEP
	}
}

// Reset resets all metrics to zero.
func (m *PEPMetrics) Reset() {
	m.checksTotal.Store(0)
	m.checksAllowed.Store(0)
	m.checksDenied.Store(0)
	m.checkLatencySum.Store(0)
	m.cacheHits.Store(0)
	m.cacheMisses.Store(0)
	m.policyUpdates.Store(0)
	m.syncErrors.Store(0)
	m.lastSyncTime.Store(0)
	m.latencyHistogram.Clear()
}

// GetChecksTotal returns the total number of checks performed.
func (m *PEPMetrics) GetChecksTotal() int64 {
	return m.checksTotal.Load()
}

// GetCacheHitRate returns the cache hit rate (0.0-1.0).
func (m *PEPMetrics) GetCacheHitRate() float64 {
	hits := m.cacheHits.Load()
	misses := m.cacheMisses.Load()
	total := hits + misses

	if total == 0 {
		return 0.0
	}

	return float64(hits) / float64(total)
}

// GetAverageLatency returns the average check latency.
func (m *PEPMetrics) GetAverageLatency() time.Duration {
	total := m.checksTotal.Load()
	if total == 0 {
		return 0
	}

	sum := m.checkLatencySum.Load()
	return time.Duration(sum / total)
}
