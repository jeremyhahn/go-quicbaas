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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsCreation(t *testing.T) {
	m := NewMetrics("test_pipeline")
	assert.NotNil(t, m)

	stats := m.GetStats()
	assert.Equal(t, int64(0), stats.TotalQueries)
	assert.Equal(t, int64(0), stats.CachedQueries)
	assert.Equal(t, int64(0), stats.FailedQueries)
}

func TestMetricsRecordQuery(t *testing.T) {
	m := NewMetrics("test_pipeline")

	response := &Response{
		Query:             "test query",
		ProcessingTime:    150 * time.Millisecond,
		VectorSearchTime:  50 * time.Millisecond,
		KeywordSearchTime: 30 * time.Millisecond,
		RerankTime:        40 * time.Millisecond,
		ContextBuildTime:  10 * time.Millisecond,
		CacheHit:          false,
		Results:           make([]interface{}, 5),
		AvgRelevance:      0.85,
	}

	m.RecordQuery(response)

	stats := m.GetStats()
	assert.Equal(t, int64(1), stats.TotalQueries)
	assert.Equal(t, int64(0), stats.CachedQueries)
	assert.Equal(t, int64(0), stats.FailedQueries)
	assert.Greater(t, stats.AvgLatencyMs, 0.0)
}

func TestMetricsRecordCachedQuery(t *testing.T) {
	m := NewMetrics("test_pipeline")

	response := &Response{
		Query:          "cached query",
		ProcessingTime: 1 * time.Millisecond,
		CacheHit:       true,
		Results:        make([]interface{}, 5),
	}

	m.RecordQuery(response)

	stats := m.GetStats()
	assert.Equal(t, int64(1), stats.TotalQueries)
	assert.Equal(t, int64(1), stats.CachedQueries)
	assert.Equal(t, 1.0, stats.CacheHitRate)
}

func TestMetricsRecordQueryError(t *testing.T) {
	m := NewMetrics("test_pipeline")

	m.RecordQueryError(ErrQueryTimeout)
	m.RecordQueryError(NewPipelineError(ErrVectorSearchFailed, "vector_search", "search", nil))

	stats := m.GetStats()
	assert.Equal(t, int64(0), stats.TotalQueries)
	assert.Equal(t, int64(2), stats.FailedQueries)
}

func TestMetricsPercentileCalculation(t *testing.T) {
	m := NewMetrics("test_pipeline")

	// Record queries with known latencies
	latencies := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		150 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		500 * time.Millisecond,
	}

	for _, latency := range latencies {
		m.RecordQuery(&Response{
			ProcessingTime: latency,
			Results:        make([]interface{}, 1),
		})
	}

	stats := m.GetStats()
	assert.Equal(t, int64(10), stats.TotalQueries)

	// Check percentiles are reasonable
	assert.Greater(t, stats.P50LatencyMs, 0.0)
	assert.Greater(t, stats.P95LatencyMs, stats.P50LatencyMs)
	assert.GreaterOrEqual(t, stats.P99LatencyMs, stats.P95LatencyMs) // Can be equal in small datasets

	// P50 should be around 50ms
	assert.InDelta(t, 50.0, stats.P50LatencyMs, 50.0)

	// P95 should be around 300-500ms
	assert.Greater(t, stats.P95LatencyMs, 200.0)

	// Average should be reasonable
	assert.Greater(t, stats.AvgLatencyMs, 0.0)
}

func TestMetricsCacheHitRate(t *testing.T) {
	m := NewMetrics("test_pipeline")

	// Record 7 cache hits and 3 misses
	for i := 0; i < 10; i++ {
		m.RecordQuery(&Response{
			ProcessingTime: 10 * time.Millisecond,
			CacheHit:       i < 7, // First 7 are hits
			Results:        make([]interface{}, 1),
		})
	}

	stats := m.GetStats()
	assert.Equal(t, int64(10), stats.TotalQueries)
	assert.Equal(t, int64(7), stats.CachedQueries)
	assert.InDelta(t, 0.7, stats.CacheHitRate, 0.01)
}

func TestMetricsConcurrentUpdates(t *testing.T) {
	m := NewMetrics("test_pipeline")

	// Record metrics concurrently
	const goroutines = 50
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			if id%2 == 0 {
				m.RecordQuery(&Response{
					ProcessingTime: 10 * time.Millisecond,
					Results:        make([]interface{}, 1),
				})
			} else {
				m.RecordQueryError(ErrQueryTimeout)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}

	stats := m.GetStats()
	assert.Equal(t, int64(25), stats.TotalQueries)
	assert.Equal(t, int64(25), stats.FailedQueries)
}

func TestMetricsReset(t *testing.T) {
	m := NewMetrics("test_pipeline")

	// Record some metrics
	m.RecordQuery(&Response{
		ProcessingTime: 100 * time.Millisecond,
		Results:        make([]interface{}, 1),
	})
	m.RecordQueryError(ErrQueryTimeout)

	stats := m.GetStats()
	require.Greater(t, stats.TotalQueries, int64(0))

	// Reset metrics
	m.Reset()

	stats = m.GetStats()
	assert.Equal(t, int64(0), stats.TotalQueries)
	assert.Equal(t, int64(0), stats.CachedQueries)
	assert.Equal(t, int64(0), stats.FailedQueries)
}

func TestMetricsLatencySliceTruncation(t *testing.T) {
	m := NewMetrics("test_pipeline")

	// Record more than 10000 queries to trigger truncation
	for i := 0; i < 11000; i++ {
		m.RecordQuery(&Response{
			ProcessingTime: time.Duration(i) * time.Millisecond,
			Results:        make([]interface{}, 1),
		})
	}

	stats := m.GetStats()
	assert.Equal(t, int64(11000), stats.TotalQueries)

	// Latency slice should be truncated to last 10000
	// We can't directly verify slice length, but percentiles should still work
	assert.Greater(t, stats.P95LatencyMs, 0.0)
}

func TestMetricsZeroQueries(t *testing.T) {
	m := NewMetrics("test_pipeline")

	stats := m.GetStats()
	assert.Equal(t, int64(0), stats.TotalQueries)
	assert.Equal(t, 0.0, stats.CacheHitRate)
	assert.Equal(t, 0.0, stats.AvgLatencyMs)
	assert.Equal(t, 0.0, stats.P50LatencyMs)
	assert.Equal(t, 0.0, stats.P95LatencyMs)
	assert.Equal(t, 0.0, stats.P99LatencyMs)
}

func TestMetricsComponentLatencies(t *testing.T) {
	m := NewMetrics("test_pipeline")

	response := &Response{
		ProcessingTime:    200 * time.Millisecond,
		VectorSearchTime:  80 * time.Millisecond,
		KeywordSearchTime: 40 * time.Millisecond,
		RerankTime:        50 * time.Millisecond,
		ContextBuildTime:  10 * time.Millisecond,
		Results:           make([]interface{}, 5),
	}

	m.RecordQuery(response)

	stats := m.GetStats()
	assert.Greater(t, stats.VectorSearchAvgMs, 0.0)
	assert.Greater(t, stats.KeywordSearchAvgMs, 0.0)
	assert.Greater(t, stats.RerankAvgMs, 0.0)
	assert.Greater(t, stats.ContextBuildAvgMs, 0.0)

	// Verify component latencies are reasonable
	assert.InDelta(t, 80.0, stats.VectorSearchAvgMs, 1.0)
	assert.InDelta(t, 40.0, stats.KeywordSearchAvgMs, 1.0)
	assert.InDelta(t, 50.0, stats.RerankAvgMs, 1.0)
	assert.InDelta(t, 10.0, stats.ContextBuildAvgMs, 1.0)
}
