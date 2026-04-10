//go:build rag

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

package pipeline

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics collects pipeline metrics.
type Metrics struct {
	namespace string

	// Atomic counters for concurrent updates
	totalQueries  atomic.Int64
	cachedQueries atomic.Int64
	failedQueries atomic.Int64

	// Component-level latency tracking
	vectorSearchTotal  atomic.Int64
	keywordSearchTotal atomic.Int64
	rerankTotal        atomic.Int64
	contextBuildTotal  atomic.Int64

	// In-memory statistics with mutex protection
	mu        sync.RWMutex
	latencies []float64 // For percentile calculation
}

// Stats represents pipeline statistics.
type Stats struct {
	// Query statistics
	TotalQueries  int64
	CachedQueries int64
	FailedQueries int64

	// Performance statistics
	AvgLatencyMs float64
	P50LatencyMs float64
	P95LatencyMs float64
	P99LatencyMs float64

	// Cache statistics
	CacheHitRate      float64
	CacheSize         int
	CacheEvictions    int64
	ZeroResultQueries int64

	// Quality statistics
	AvgResultsReturned float64
	AvgRelevanceScore  float64

	// Component statistics
	VectorSearchAvgMs  float64
	KeywordSearchAvgMs float64
	RerankAvgMs        float64
	ContextBuildAvgMs  float64
}

// Response represents a query response for metrics.
type Response struct {
	Query             string
	Results           []interface{}
	ProcessingTime    time.Duration
	VectorSearchTime  time.Duration
	KeywordSearchTime time.Duration
	RerankTime        time.Duration
	ContextBuildTime  time.Duration
	CacheHit          bool
	AvgRelevance      float32
}

// NewMetrics creates a new metrics collector.
func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		namespace: namespace,
		latencies: make([]float64, 0, 10000),
	}
}

// RecordQuery records a successful query.
func (m *Metrics) RecordQuery(response *Response) {
	// Increment counters
	m.totalQueries.Add(1)

	if response.CacheHit {
		m.cachedQueries.Add(1)
	}

	// Record component latencies (convert to nanoseconds for atomic operations)
	if response.VectorSearchTime > 0 {
		m.vectorSearchTotal.Add(response.VectorSearchTime.Nanoseconds())
	}
	if response.KeywordSearchTime > 0 {
		m.keywordSearchTotal.Add(response.KeywordSearchTime.Nanoseconds())
	}
	if response.RerankTime > 0 {
		m.rerankTotal.Add(response.RerankTime.Nanoseconds())
	}
	if response.ContextBuildTime > 0 {
		m.contextBuildTotal.Add(response.ContextBuildTime.Nanoseconds())
	}

	// Record latency for percentile calculation
	m.mu.Lock()
	defer m.mu.Unlock()

	latencySeconds := response.ProcessingTime.Seconds()
	m.latencies = append(m.latencies, latencySeconds)

	// Limit latency slice size to prevent unbounded growth
	if len(m.latencies) > 10000 {
		m.latencies = m.latencies[len(m.latencies)-10000:]
	}
}

// RecordQueryError records a failed query.
func (m *Metrics) RecordQueryError(err error) {
	m.failedQueries.Add(1)
}

// GetStats returns current statistics.
func (m *Metrics) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalQueries := m.totalQueries.Load()
	cachedQueries := m.cachedQueries.Load()
	failedQueries := m.failedQueries.Load()

	stats := Stats{
		TotalQueries:  totalQueries,
		CachedQueries: cachedQueries,
		FailedQueries: failedQueries,
	}

	// Calculate cache hit rate
	if totalQueries > 0 {
		stats.CacheHitRate = float64(cachedQueries) / float64(totalQueries)
	}

	// Calculate percentiles from latencies
	if len(m.latencies) > 0 {
		sorted := make([]float64, len(m.latencies))
		copy(sorted, m.latencies)
		sort.Float64s(sorted)

		stats.P50LatencyMs = sorted[len(sorted)/2] * 1000
		stats.P95LatencyMs = sorted[int(float64(len(sorted))*0.95)] * 1000
		stats.P99LatencyMs = sorted[int(float64(len(sorted))*0.99)] * 1000

		// Calculate average latency
		var sum float64
		for _, l := range m.latencies {
			sum += l
		}
		stats.AvgLatencyMs = (sum / float64(len(m.latencies))) * 1000
	}

	// Calculate component averages
	if totalQueries > 0 {
		vectorTotal := m.vectorSearchTotal.Load()
		keywordTotal := m.keywordSearchTotal.Load()
		rerankTotal := m.rerankTotal.Load()
		contextTotal := m.contextBuildTotal.Load()

		if vectorTotal > 0 {
			stats.VectorSearchAvgMs = float64(vectorTotal) / float64(totalQueries) / 1e6
		}
		if keywordTotal > 0 {
			stats.KeywordSearchAvgMs = float64(keywordTotal) / float64(totalQueries) / 1e6
		}
		if rerankTotal > 0 {
			stats.RerankAvgMs = float64(rerankTotal) / float64(totalQueries) / 1e6
		}
		if contextTotal > 0 {
			stats.ContextBuildAvgMs = float64(contextTotal) / float64(totalQueries) / 1e6
		}
	}

	return stats
}

// Reset resets all metrics.
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalQueries.Store(0)
	m.cachedQueries.Store(0)
	m.failedQueries.Store(0)
	m.vectorSearchTotal.Store(0)
	m.keywordSearchTotal.Store(0)
	m.rerankTotal.Store(0)
	m.contextBuildTotal.Store(0)
	m.latencies = make([]float64, 0, 10000)
}
