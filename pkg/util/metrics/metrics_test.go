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
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCounter(t *testing.T) {
	// Use a fresh registry for each test
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	tests := []struct {
		name string
		opts CounterOpts
	}{
		{
			name: "simple counter",
			opts: CounterOpts{
				Namespace: "test",
				Name:      "simple_counter",
				Help:      "A simple test counter",
			},
		},
		{
			name: "counter with subsystem",
			opts: CounterOpts{
				Namespace: "test",
				Subsystem: "metrics",
				Name:      "subsystem_counter",
				Help:      "Counter with subsystem",
			},
		},
		{
			name: "counter with const labels",
			opts: CounterOpts{
				Namespace: "test",
				Name:      "labeled_counter",
				Help:      "Counter with const labels",
				ConstLabels: prometheus.Labels{
					"version": "1.0",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := NewCounter(tt.opts)
			require.NotNil(t, counter)

			// Test Inc
			counter.Inc()
			counter.Inc()

			// Verify metric value
			fqName := buildFQName(tt.opts.Namespace, tt.opts.Subsystem, tt.opts.Name)
			value := testutil.ToFloat64(counter.counter)
			assert.Equal(t, 2.0, value, "counter should be 2")

			// Test Add
			counter.Add(3.5)
			value = testutil.ToFloat64(counter.counter)
			assert.Equal(t, 5.5, value, "counter should be 5.5")

			// Verify metric is registered
			count, err := testutil.GatherAndCount(registry, fqName)
			require.NoError(t, err)
			assert.Equal(t, 1, count, "should have 1 metric")
		})
	}
}

func TestNewGauge(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	tests := []struct {
		name string
		opts GaugeOpts
	}{
		{
			name: "simple gauge",
			opts: GaugeOpts{
				Namespace: "test",
				Name:      "simple_gauge",
				Help:      "A simple test gauge",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gauge := NewGauge(tt.opts)
			require.NotNil(t, gauge)

			// Test Set
			gauge.Set(42.5)
			value := testutil.ToFloat64(gauge.gauge)
			assert.Equal(t, 42.5, value)

			// Test Inc
			gauge.Inc()
			value = testutil.ToFloat64(gauge.gauge)
			assert.Equal(t, 43.5, value)

			// Test Dec
			gauge.Dec()
			value = testutil.ToFloat64(gauge.gauge)
			assert.Equal(t, 42.5, value)

			// Test Add
			gauge.Add(10)
			value = testutil.ToFloat64(gauge.gauge)
			assert.Equal(t, 52.5, value)

			// Test Sub
			gauge.Sub(2.5)
			value = testutil.ToFloat64(gauge.gauge)
			assert.Equal(t, 50.0, value)
		})
	}
}

func TestNewHistogram(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	tests := []struct {
		name    string
		opts    HistogramOpts
		buckets []float64
	}{
		{
			name: "histogram with default buckets",
			opts: HistogramOpts{
				Namespace: "test",
				Name:      "default_histogram",
				Help:      "Histogram with default buckets",
			},
			buckets: prometheus.DefBuckets,
		},
		{
			name: "histogram with custom buckets",
			opts: HistogramOpts{
				Namespace: "test",
				Name:      "custom_histogram",
				Help:      "Histogram with custom buckets",
				Buckets:   []float64{0.1, 0.5, 1.0, 5.0},
			},
			buckets: []float64{0.1, 0.5, 1.0, 5.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			histogram := NewHistogram(tt.opts)
			require.NotNil(t, histogram)

			// Test Observe
			histogram.Observe(0.25)
			histogram.Observe(0.75)
			histogram.Observe(2.5)

			// Verify observations were recorded
			fqName := buildFQName(tt.opts.Namespace, tt.opts.Subsystem, tt.opts.Name)
			count, err := testutil.GatherAndCount(registry, fqName)
			require.NoError(t, err)
			assert.Equal(t, 1, count)
		})
	}
}

func TestCounterVec(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	counterVec := NewCounterVec(
		CounterOpts{
			Namespace: "test",
			Name:      "vector_counter",
			Help:      "A test counter vector",
		},
		[]string{"method", "status"},
	)

	// Test WithLabels
	counter1 := counterVec.WithLabels(prometheus.Labels{
		"method": "GET",
		"status": "200",
	})
	counter1.Inc()
	counter1.Add(2)

	// Test With (variadic)
	counter2 := counterVec.With("POST", "201")
	counter2.Inc()

	// Verify both counters
	assert.Equal(t, 3.0, testutil.ToFloat64(counter1.counter))
	assert.Equal(t, 1.0, testutil.ToFloat64(counter2.counter))
}

func TestGaugeVec(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	gaugeVec := NewGaugeVec(
		GaugeOpts{
			Namespace: "test",
			Name:      "vector_gauge",
			Help:      "A test gauge vector",
		},
		[]string{"service"},
	)

	// Test WithLabels
	gauge1 := gaugeVec.WithLabels(prometheus.Labels{"service": "api"})
	gauge1.Set(100)

	// Test With (variadic)
	gauge2 := gaugeVec.With("db")
	gauge2.Set(50)
	gauge2.Inc()

	assert.Equal(t, 100.0, testutil.ToFloat64(gauge1.gauge))
	assert.Equal(t, 51.0, testutil.ToFloat64(gauge2.gauge))
}

func TestHistogramVec(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	histogramVec := NewHistogramVec(
		HistogramOpts{
			Namespace: "test",
			Name:      "vector_histogram",
			Help:      "A test histogram vector",
			Buckets:   []float64{0.1, 0.5, 1.0},
		},
		[]string{"endpoint"},
	)

	// Test WithLabels
	hist1 := histogramVec.WithLabels(prometheus.Labels{"endpoint": "/api/users"})
	hist1.Observe(0.25)
	hist1.Observe(0.75)

	// Test With (variadic)
	hist2 := histogramVec.With("/api/posts")
	hist2.Observe(0.05)

	// Histograms should be registered (2 unique endpoints)
	count, err := testutil.GatherAndCount(registry, "test_vector_histogram")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestRegistryManagement(t *testing.T) {
	// Save original registry
	originalRegistry := Registry()
	defer SetRegistry(originalRegistry)

	// Create and set a new registry
	newRegistry := prometheus.NewRegistry()
	SetRegistry(newRegistry)

	assert.Equal(t, newRegistry, Registry())

	// Create a metric with the new registry
	counter := NewCounter(CounterOpts{
		Namespace: "test",
		Name:      "registry_test",
		Help:      "Test registry management",
	})
	counter.Inc()

	// Verify metric is in the new registry
	count, err := testutil.GatherAndCount(newRegistry, "test_registry_test")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify metric is NOT in the original registry
	count, err = testutil.GatherAndCount(originalRegistry, "test_registry_test")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestMustRegister(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "custom_counter",
		Help: "A custom counter",
	})

	// Should not panic
	require.NotPanics(t, func() {
		MustRegister(collector)
	})

	// Verify registration
	count, err := testutil.GatherAndCount(registry, "custom_counter")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestRegister(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "register_test",
		Help: "Register test counter",
	})

	err := Register(collector)
	require.NoError(t, err)

	// Verify registration
	count, err := testutil.GatherAndCount(registry, "register_test")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Registering again should fail
	err = Register(collector)
	assert.Error(t, err)
}

func TestUnregister(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	collector := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "unregister_test",
		Help: "Unregister test counter",
	})

	err := Register(collector)
	require.NoError(t, err)

	// Unregister should return true
	ok := Unregister(collector)
	assert.True(t, ok)

	// Verify unregistration
	count, err := testutil.GatherAndCount(registry, "unregister_test")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Unregistering again should return false
	ok = Unregister(collector)
	assert.False(t, ok)
}

func TestPrometheusExposition(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	// Create some metrics
	counter := NewCounter(CounterOpts{
		Namespace: "app",
		Subsystem: "requests",
		Name:      "total",
		Help:      "Total requests",
	})
	counter.Add(100)

	gauge := NewGauge(GaugeOpts{
		Namespace: "app",
		Subsystem: "connections",
		Name:      "active",
		Help:      "Active connections",
	})
	gauge.Set(42)

	histogram := NewHistogram(HistogramOpts{
		Namespace: "app",
		Subsystem: "request",
		Name:      "duration_seconds",
		Help:      "Request duration",
		Buckets:   []float64{0.1, 0.5, 1.0},
	})
	histogram.Observe(0.25)
	histogram.Observe(0.75)

	// Gather metrics
	metrics, err := registry.Gather()
	require.NoError(t, err)
	assert.Len(t, metrics, 3)

	// Verify exposition format
	expected := `
		# HELP app_requests_total Total requests
		# TYPE app_requests_total counter
		app_requests_total 100
	`
	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "app_requests_total")
	require.NoError(t, err)
}

func TestLabelHandling(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	// Test counter with labels
	counterVec := NewCounterVec(
		CounterOpts{
			Namespace: "test",
			Name:      "labeled_requests",
			Help:      "Requests with labels",
		},
		[]string{"method", "path", "status"},
	)

	// Different label combinations
	counterVec.With("GET", "/api/users", "200").Inc()
	counterVec.With("POST", "/api/users", "201").Add(2)
	counterVec.With("GET", "/api/users", "404").Inc()

	// Verify each label combination
	c1 := counterVec.With("GET", "/api/users", "200")
	assert.Equal(t, 1.0, testutil.ToFloat64(c1.counter))

	c2 := counterVec.With("POST", "/api/users", "201")
	assert.Equal(t, 2.0, testutil.ToFloat64(c2.counter))

	c3 := counterVec.With("GET", "/api/users", "404")
	assert.Equal(t, 1.0, testutil.ToFloat64(c3.counter))
}

// Helper function to build fully qualified metric name
func buildFQName(namespace, subsystem, name string) string {
	if namespace == "" {
		return name
	}
	if subsystem == "" {
		return namespace + "_" + name
	}
	return namespace + "_" + subsystem + "_" + name
}

// Benchmark tests
func BenchmarkCounterInc(b *testing.B) {
	counter := NewCounter(CounterOpts{
		Namespace: "bench",
		Name:      "counter_inc",
		Help:      "Benchmark counter inc",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.Inc()
	}
}

func BenchmarkCounterVecWith(b *testing.B) {
	counterVec := NewCounterVec(
		CounterOpts{
			Namespace: "bench",
			Name:      "counter_vec",
			Help:      "Benchmark counter vec",
		},
		[]string{"method", "status"},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counterVec.With("GET", "200").Inc()
	}
}

func BenchmarkHistogramObserve(b *testing.B) {
	histogram := NewHistogram(HistogramOpts{
		Namespace: "bench",
		Name:      "histogram_observe",
		Help:      "Benchmark histogram observe",
		Buckets:   defaultDurationBuckets(),
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		histogram.Observe(0.123)
	}
}

func BenchmarkTimerObserveDuration(b *testing.B) {
	histogram := NewHistogram(HistogramOpts{
		Namespace: "bench",
		Name:      "timer_duration",
		Help:      "Benchmark timer",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		timer := NewTimer(histogram)
		time.Sleep(1 * time.Microsecond)
		timer.ObserveDuration()
	}
}
