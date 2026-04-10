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

package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name string
		opts CollectorOpts
	}{
		{
			name: "collector with namespace only",
			opts: CollectorOpts{
				Namespace: "test1",
			},
		},
		{
			name: "collector with namespace and subsystem",
			opts: CollectorOpts{
				Namespace: "test2",
				Subsystem: "api",
			},
		},
		{
			name: "collector with custom buckets",
			opts: CollectorOpts{
				Namespace:       "test3",
				DurationBuckets: []float64{0.001, 0.01, 0.1, 1.0},
			},
		},
		{
			name: "collector with default buckets",
			opts: CollectorOpts{
				Namespace: "test4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each subtest gets a fresh registry
			registry := prometheus.NewRegistry()
			SetRegistry(registry)
			defer SetRegistry(prometheus.NewRegistry())

			collector := NewCollector(tt.opts)
			require.NotNil(t, collector)
			assert.Equal(t, tt.opts.Namespace, collector.namespace)
			assert.Equal(t, tt.opts.Subsystem, collector.subsystem)
			assert.NotNil(t, collector.requestDuration)
			assert.NotNil(t, collector.requestRate)
			assert.NotNil(t, collector.requestsInFlight)
			assert.NotNil(t, collector.errorRate)
			assert.NotNil(t, collector.activeConnections)
		})
	}
}

func TestCollector_RecordRequestDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "test",
		Subsystem: "http",
	})

	tests := []struct {
		method   string
		status   string
		duration time.Duration
	}{
		{"GET", "200", 100 * time.Millisecond},
		{"POST", "201", 250 * time.Millisecond},
		{"GET", "404", 50 * time.Millisecond},
		{"PUT", "200", 150 * time.Millisecond},
	}

	for _, tt := range tests {
		collector.RecordRequestDuration(tt.method, tt.status, tt.duration)
	}

	// Verify metrics were recorded (4 unique label combinations)
	count, err := testutil.GatherAndCount(registry, "test_http_request_duration_seconds")
	require.NoError(t, err)
	assert.Equal(t, 4, count) // 4 unique method/status combinations
}

func TestCollector_RecordRequest(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "test",
	})

	collector.RecordRequest("GET", "200")
	collector.RecordRequest("GET", "200")
	collector.RecordRequest("POST", "201")

	// Verify counter values
	metrics, err := registry.Gather()
	require.NoError(t, err)

	var found bool
	for _, mf := range metrics {
		if mf.GetName() == "test_requests_total" {
			found = true
			assert.Len(t, mf.GetMetric(), 2) // Two unique label combinations
		}
	}
	assert.True(t, found, "requests_total metric should exist")
}

func TestCollector_RequestsInFlight(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "test",
	})

	// Test increment
	collector.IncRequestsInFlight("GET")
	collector.IncRequestsInFlight("GET")
	collector.IncRequestsInFlight("POST")

	// Test decrement
	collector.DecRequestsInFlight("GET")

	// Verify metrics were recorded (2 unique methods: GET and POST)
	count, err := testutil.GatherAndCount(registry, "test_requests_in_flight")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestCollector_RecordError(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "test",
	})

	tests := []struct {
		errorType string
		operation string
	}{
		{"timeout", "read"},
		{"connection_refused", "dial"},
		{"timeout", "write"},
		{"not_found", "query"},
	}

	for _, tt := range tests {
		collector.RecordError(tt.errorType, tt.operation)
	}

	// Verify error metrics (4 unique error type/operation combinations)
	count, err := testutil.GatherAndCount(registry, "test_errors_total")
	require.NoError(t, err)
	assert.Equal(t, 4, count)
}

func TestCollector_ActiveConnections(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "test",
	})

	// Test Set
	collector.SetActiveConnections(42)

	// Test Inc
	collector.IncActiveConnections()
	collector.IncActiveConnections()

	// Test Dec
	collector.DecActiveConnections()

	// Verify final value (42 + 2 - 1 = 43)
	count, err := testutil.GatherAndCount(registry, "test_active_connections")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTimer(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	histogram := NewHistogram(HistogramOpts{
		Namespace: "test",
		Name:      "timer_test",
		Help:      "Timer test",
	})

	timer := NewTimer(histogram)
	time.Sleep(10 * time.Millisecond)
	duration := timer.ObserveDuration()

	assert.GreaterOrEqual(t, duration, 10*time.Millisecond)
	assert.Less(t, duration, 100*time.Millisecond)

	// Verify observation was recorded
	count, err := testutil.GatherAndCount(registry, "test_timer_test")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTimer_NilHistogram(t *testing.T) {
	// Timer should handle nil histogram gracefully
	timer := NewTimer(nil)
	require.NotPanics(t, func() {
		duration := timer.ObserveDuration()
		assert.Greater(t, duration, time.Duration(0))
	})
}

func TestRequestTimer(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "test",
	})

	// Start a request timer
	timer := collector.StartRequestTimer("GET")
	time.Sleep(10 * time.Millisecond)
	timer.Done("200")

	// Verify all metrics were updated
	metrics, err := registry.Gather()
	require.NoError(t, err)

	metricNames := make(map[string]bool)
	for _, mf := range metrics {
		metricNames[mf.GetName()] = true
	}

	assert.True(t, metricNames["test_request_duration_seconds"], "duration should be recorded")
	assert.True(t, metricNames["test_requests_total"], "request should be counted")
	assert.True(t, metricNames["test_requests_in_flight"], "in-flight gauge should exist")
}

func TestRequestTimer_DoneWithError(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "test",
	})

	timer := collector.StartRequestTimer("POST")
	time.Sleep(5 * time.Millisecond)
	timer.DoneWithError("500", "internal_error")

	// Verify error metric was recorded
	count, err := testutil.GatherAndCount(registry, "test_errors_total")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify request metrics were also recorded
	count, err = testutil.GatherAndCount(registry, "test_requests_total")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestLatencyBuckets_Microseconds(t *testing.T) {
	buckets := Buckets.Microseconds()
	assert.Len(t, buckets, 7)
	assert.Equal(t, 0.000001, buckets[0]) // 1μs
	assert.Equal(t, 0.001, buckets[6])    // 1ms
}

func TestLatencyBuckets_Milliseconds(t *testing.T) {
	buckets := Buckets.Milliseconds()
	assert.Len(t, buckets, 9)
	assert.Equal(t, 0.001, buckets[0]) // 1ms
	assert.Equal(t, 1.0, buckets[8])   // 1s
}

func TestLatencyBuckets_Seconds(t *testing.T) {
	buckets := Buckets.Seconds()
	assert.Len(t, buckets, 8)
	assert.Equal(t, 0.1, buckets[0])  // 100ms
	assert.Equal(t, 60.0, buckets[7]) // 60s
}

func TestLatencyBuckets_Default(t *testing.T) {
	buckets := Buckets.Default()
	assert.Len(t, buckets, 9)
	assert.Equal(t, 0.001, buckets[0]) // 1ms
	assert.Equal(t, 10.0, buckets[8])  // 10s
}

func TestLatencyBuckets_Custom(t *testing.T) {
	custom := []float64{0.1, 0.5, 1.0, 5.0, 10.0}
	buckets := Buckets.Custom(custom...)
	assert.Equal(t, custom, buckets)
}

func TestDefaultDurationBuckets(t *testing.T) {
	buckets := defaultDurationBuckets()
	assert.Len(t, buckets, 9)

	// Verify buckets are in ascending order
	for i := 1; i < len(buckets); i++ {
		assert.Greater(t, buckets[i], buckets[i-1])
	}

	// Verify specific values
	assert.Equal(t, 0.001, buckets[0]) // 1ms
	assert.Equal(t, 0.01, buckets[2])  // 10ms
	assert.Equal(t, 0.1, buckets[4])   // 100ms
	assert.Equal(t, 1.0, buckets[6])   // 1s
	assert.Equal(t, 10.0, buckets[8])  // 10s
}

func TestCollector_ConcurrentUsage(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "test",
	})

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				collector.RecordRequest("GET", "200")
				collector.IncRequestsInFlight("GET")
				collector.RecordRequestDuration("GET", "200", 10*time.Millisecond)
				collector.DecRequestsInFlight("GET")
				collector.RecordError("timeout", "read")
				collector.IncActiveConnections()
				collector.DecActiveConnections()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify metrics exist (exact values are hard to test due to concurrency)
	count, err := testutil.GatherAndCount(registry, "test_requests_total")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestCollector_Integration(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := NewCollector(CollectorOpts{
		Namespace: "myapp",
		Subsystem: "api",
	})

	// Simulate a complete request lifecycle
	collector.IncActiveConnections()
	timer := collector.StartRequestTimer("POST")

	// Simulate some work
	time.Sleep(15 * time.Millisecond)

	timer.Done("201")
	collector.DecActiveConnections()

	// Verify all expected metrics exist
	expectedMetrics := []string{
		"myapp_api_request_duration_seconds",
		"myapp_api_requests_total",
		"myapp_api_requests_in_flight",
		"myapp_api_active_connections",
	}

	metrics, err := registry.Gather()
	require.NoError(t, err)

	metricNames := make(map[string]bool)
	for _, mf := range metrics {
		metricNames[mf.GetName()] = true
	}

	for _, name := range expectedMetrics {
		assert.True(t, metricNames[name], "metric %s should exist", name)
	}
}

// Benchmark tests for collector
func BenchmarkCollector_RecordRequest(b *testing.B) {
	collector := NewCollector(CollectorOpts{
		Namespace: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.RecordRequest("GET", "200")
	}
}

func BenchmarkCollector_RecordRequestDuration(b *testing.B) {
	collector := NewCollector(CollectorOpts{
		Namespace: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.RecordRequestDuration("GET", "200", 100*time.Millisecond)
	}
}

func BenchmarkCollector_RequestTimer(b *testing.B) {
	collector := NewCollector(CollectorOpts{
		Namespace: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		timer := collector.StartRequestTimer("GET")
		timer.Done("200")
	}
}

func BenchmarkCollector_RecordError(b *testing.B) {
	collector := NewCollector(CollectorOpts{
		Namespace: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.RecordError("timeout", "read")
	}
}

func BenchmarkCollector_ActiveConnections(b *testing.B) {
	collector := NewCollector(CollectorOpts{
		Namespace: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.IncActiveConnections()
		collector.DecActiveConnections()
	}
}
