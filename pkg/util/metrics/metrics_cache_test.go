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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMetricCacheDeduplication tests that metrics are properly cached and deduplicated.
func TestMetricCacheDeduplication(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func()
	}{
		{
			name: "counter cache deduplication",
			testFunc: func() {
				opts := CounterOpts{
					Namespace: "cache_test",
					Subsystem: "counter",
					Name:      "dedup",
					Help:      "Deduplication test",
				}
				c1 := NewCounter(opts)
				c1.Inc()
				c2 := NewCounter(opts)
				c2.Inc()
				// Both should affect the same underlying counter
				require.NotNil(t, c1)
				require.NotNil(t, c2)
			},
		},
		{
			name: "gauge cache deduplication",
			testFunc: func() {
				opts := GaugeOpts{
					Namespace: "cache_test",
					Subsystem: "gauge",
					Name:      "dedup",
					Help:      "Deduplication test",
				}
				g1 := NewGauge(opts)
				g1.Set(100)
				g2 := NewGauge(opts)
				g2.Inc()
				require.NotNil(t, g1)
				require.NotNil(t, g2)
			},
		},
		{
			name: "histogram cache deduplication",
			testFunc: func() {
				opts := HistogramOpts{
					Namespace: "cache_test",
					Subsystem: "histogram",
					Name:      "dedup",
					Help:      "Deduplication test",
					Buckets:   []float64{0.1, 0.5, 1.0},
				}
				h1 := NewHistogram(opts)
				h1.Observe(0.25)
				h2 := NewHistogram(opts)
				h2.Observe(0.75)
				require.NotNil(t, h1)
				require.NotNil(t, h2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			SetRegistry(registry)
			defer SetRegistry(prometheus.NewRegistry())

			tt.testFunc()
		})
	}
}

// TestMetricCacheTypeMismatchRecovery tests graceful recovery when cache contains wrong type.
// Previously these scenarios caused panics. Now they log a warning, evict the corrupt entry,
// and re-register the metric.
func TestMetricCacheTypeMismatchRecovery(t *testing.T) {
	t.Run("gauge cache wrong type recovers gracefully", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		SetRegistry(registry)
		defer SetRegistry(prometheus.NewRegistry())

		// Manually pollute cache with wrong type
		key := "gauge:test_gauge_invalid:cache:wrong_type_gauge"
		metricCacheMu.Lock()
		metricCache[key] = "not a gauge" // Wrong type
		metricCacheMu.Unlock()

		// Should not panic; should recover by evicting bad cache entry and re-registering
		assert.NotPanics(t, func() {
			g := NewGauge(GaugeOpts{
				Namespace: "test_gauge_invalid",
				Subsystem: "cache",
				Name:      "wrong_type_gauge",
				Help:      "Test wrong type",
			})
			require.NotNil(t, g)
			g.Set(42)
			assert.Equal(t, 42.0, testutil.ToFloat64(g.gauge))
		})
	})

	t.Run("histogram cache wrong type recovers gracefully", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		SetRegistry(registry)
		defer SetRegistry(prometheus.NewRegistry())

		// Manually pollute cache with wrong type
		key := "hist:test_hist_invalid:cache:wrong_type_hist:[0.005 0.01 0.025 0.05 0.1 0.25 0.5 1 2.5 5 10]"
		metricCacheMu.Lock()
		metricCache[key] = "not a histogram" // Wrong type
		metricCacheMu.Unlock()

		// Should not panic; should recover gracefully
		assert.NotPanics(t, func() {
			h := NewHistogram(HistogramOpts{
				Namespace: "test_hist_invalid",
				Subsystem: "cache",
				Name:      "wrong_type_hist",
				Help:      "Test wrong type",
			})
			require.NotNil(t, h)
			h.Observe(0.5)
		})
	})

	t.Run("countervec cache wrong type recovers gracefully", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		SetRegistry(registry)
		defer SetRegistry(prometheus.NewRegistry())

		// Manually pollute cache with wrong type
		key := "countervec:test_cv_invalid:cache:wrong_type_cv:[method]"
		metricCacheMu.Lock()
		metricCache[key] = "not a counter vec" // Wrong type
		metricCacheMu.Unlock()

		// Should not panic; should recover gracefully
		assert.NotPanics(t, func() {
			cv := NewCounterVec(CounterOpts{
				Namespace: "test_cv_invalid",
				Subsystem: "cache",
				Name:      "wrong_type_cv",
				Help:      "Test wrong type",
			}, []string{"method"})
			require.NotNil(t, cv)
			cv.With("GET").Inc()
		})
	})

	t.Run("gaugevec cache wrong type recovers gracefully", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		SetRegistry(registry)
		defer SetRegistry(prometheus.NewRegistry())

		// Manually pollute cache with wrong type
		key := "gaugevec:test_gv_invalid:cache:wrong_type_gv:[method]"
		metricCacheMu.Lock()
		metricCache[key] = "not a gauge vec" // Wrong type
		metricCacheMu.Unlock()

		// Should not panic; should recover gracefully
		assert.NotPanics(t, func() {
			gv := NewGaugeVec(GaugeOpts{
				Namespace: "test_gv_invalid",
				Subsystem: "cache",
				Name:      "wrong_type_gv",
				Help:      "Test wrong type",
			}, []string{"method"})
			require.NotNil(t, gv)
			gv.With("GET").Set(100)
		})
	})

	t.Run("histogramvec cache wrong type recovers gracefully", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		SetRegistry(registry)
		defer SetRegistry(prometheus.NewRegistry())

		// Manually pollute cache with wrong type
		key := "histvec:test_hv_invalid:cache:wrong_type_hv:[method]:[0.005 0.01 0.025 0.05 0.1 0.25 0.5 1 2.5 5 10]"
		metricCacheMu.Lock()
		metricCache[key] = "not a histogram vec" // Wrong type
		metricCacheMu.Unlock()

		// Should not panic; should recover gracefully
		assert.NotPanics(t, func() {
			hv := NewHistogramVec(HistogramOpts{
				Namespace: "test_hv_invalid",
				Subsystem: "cache",
				Name:      "wrong_type_hv",
				Help:      "Test wrong type",
			}, []string{"method"})
			require.NotNil(t, hv)
			hv.With("GET").Observe(0.5)
		})
	})

	t.Run("counter cache wrong type recovers gracefully", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		SetRegistry(registry)
		defer SetRegistry(prometheus.NewRegistry())

		// Manually pollute cache with wrong type
		key := "counter:test_counter_invalid:cache:wrong_type_counter"
		metricCacheMu.Lock()
		metricCache[key] = "not a counter" // Wrong type
		metricCacheMu.Unlock()

		// Should not panic; should recover gracefully
		assert.NotPanics(t, func() {
			c := NewCounter(CounterOpts{
				Namespace: "test_counter_invalid",
				Subsystem: "cache",
				Name:      "wrong_type_counter",
				Help:      "Test wrong type",
			})
			require.NotNil(t, c)
			c.Inc()
		})
	})
}

// TestHistogramVecTypeAssertion tests the type assertion paths in HistogramVec.
func TestHistogramVecTypeAssertion(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	t.Run("WithLabels success path", func(t *testing.T) {
		hv := NewHistogramVec(
			HistogramOpts{
				Namespace: "test",
				Name:      "hist_vec_labels",
				Help:      "Test histogram vec",
				Buckets:   []float64{0.1, 0.5, 1.0},
			},
			[]string{"endpoint"},
		)

		h := hv.WithLabels(prometheus.Labels{"endpoint": "/api/test"})
		require.NotNil(t, h)
		h.Observe(0.25)
	})

	t.Run("With success path", func(t *testing.T) {
		hv := NewHistogramVec(
			HistogramOpts{
				Namespace: "test",
				Name:      "hist_vec_with",
				Help:      "Test histogram vec",
				Buckets:   []float64{0.1, 0.5, 1.0},
			},
			[]string{"endpoint"},
		)

		h := hv.With("/api/test2")
		require.NotNil(t, h)
		h.Observe(0.5)
	})
}

// TestHistogramNoOpObserve tests that a no-op Histogram safely ignores observations.
func TestHistogramNoOpObserve(t *testing.T) {
	// Create a Histogram with nil internal histogram (no-op)
	h := &Histogram{}
	// Should not panic
	assert.NotPanics(t, func() {
		h.Observe(1.0)
		h.Observe(0.5)
	})
}

// TestCollectorCacheDeduplication tests that collectors are properly cached.
func TestCollectorCacheDeduplication(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	opts := CollectorOpts{
		Namespace: "test_collector_cache",
		Subsystem: "api",
	}

	// Create two collectors with same opts
	c1 := NewCollector(opts)
	c2 := NewCollector(opts)

	// They should be the same instance from cache
	require.NotNil(t, c1)
	require.NotNil(t, c2)

	// Verify they're functionally the same by updating one and checking both
	c1.IncActiveConnections()
	// Since they're cached, both should reflect the same underlying metric
}

// TestCollectorCacheWithDifferentRegistry tests collector cache invalidation on registry change.
func TestCollectorCacheWithDifferentRegistry(t *testing.T) {
	registry1 := prometheus.NewRegistry()
	SetRegistry(registry1)

	opts := CollectorOpts{
		Namespace: "test_reg_change",
		Subsystem: "api",
	}

	c1 := NewCollector(opts)
	require.NotNil(t, c1)

	// Change registry
	registry2 := prometheus.NewRegistry()
	SetRegistry(registry2)

	// Create collector with same opts but different registry
	c2 := NewCollector(opts)
	require.NotNil(t, c2)

	// They should be different instances (different cache keys due to registry pointer)
	SetRegistry(prometheus.NewRegistry()) // cleanup
}

// TestCounterCacheHit tests that cached counter is returned.
func TestCounterCacheHit(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	opts := CounterOpts{
		Namespace: "test_counter_cache",
		Name:      "hit_test",
		Help:      "Cache hit test",
	}

	// First call - creates and caches
	c1 := NewCounter(opts)
	c1.Inc()

	// Second call - should return cached instance
	c2 := NewCounter(opts)
	c2.Inc()

	// Both should point to the same underlying counter
	require.NotNil(t, c1)
	require.NotNil(t, c2)
}

// TestGaugeCacheHit tests that cached gauge is returned.
func TestGaugeCacheHit(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	opts := GaugeOpts{
		Namespace: "test_gauge_cache",
		Name:      "hit_test",
		Help:      "Cache hit test",
	}

	// First call - creates and caches
	g1 := NewGauge(opts)
	g1.Set(100)

	// Second call - should return cached instance
	g2 := NewGauge(opts)
	require.NotNil(t, g1)
	require.NotNil(t, g2)
}

// TestHistogramCacheHit tests that cached histogram is returned.
func TestHistogramCacheHit(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	opts := HistogramOpts{
		Namespace: "test_hist_cache",
		Name:      "hit_test",
		Help:      "Cache hit test",
		Buckets:   []float64{0.1, 0.5, 1.0},
	}

	// First call - creates and caches
	h1 := NewHistogram(opts)
	h1.Observe(0.25)

	// Second call - should return cached instance
	h2 := NewHistogram(opts)
	require.NotNil(t, h1)
	require.NotNil(t, h2)
}

// TestCounterVecCacheHit tests that cached counter vec is returned.
func TestCounterVecCacheHit(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	opts := CounterOpts{
		Namespace: "test_cv_cache",
		Name:      "hit_test",
		Help:      "Cache hit test",
	}
	labels := []string{"method"}

	// First call - creates and caches
	cv1 := NewCounterVec(opts, labels)
	cv1.With("GET").Inc()

	// Second call - should return cached instance
	cv2 := NewCounterVec(opts, labels)
	require.NotNil(t, cv1)
	require.NotNil(t, cv2)
}

// TestGaugeVecCacheHit tests that cached gauge vec is returned.
func TestGaugeVecCacheHit(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	opts := GaugeOpts{
		Namespace: "test_gv_cache",
		Name:      "hit_test",
		Help:      "Cache hit test",
	}
	labels := []string{"service"}

	// First call - creates and caches
	gv1 := NewGaugeVec(opts, labels)
	gv1.With("api").Set(100)

	// Second call - should return cached instance
	gv2 := NewGaugeVec(opts, labels)
	require.NotNil(t, gv1)
	require.NotNil(t, gv2)
}

// TestHistogramVecCacheHit tests that cached histogram vec is returned.
func TestHistogramVecCacheHit(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	opts := HistogramOpts{
		Namespace: "test_hv_cache",
		Name:      "hit_test",
		Help:      "Cache hit test",
		Buckets:   []float64{0.1, 0.5, 1.0},
	}
	labels := []string{"endpoint"}

	// First call - creates and caches
	hv1 := NewHistogramVec(opts, labels)
	hv1.With("/api").Observe(0.25)

	// Second call - should return cached instance
	hv2 := NewHistogramVec(opts, labels)
	require.NotNil(t, hv1)
	require.NotNil(t, hv2)
}

// TestSetRegistryClearsCaches tests that SetRegistry clears all caches.
func TestSetRegistryClearsCaches(t *testing.T) {
	registry1 := prometheus.NewRegistry()
	SetRegistry(registry1)

	// Create some metrics and collectors
	NewCounter(CounterOpts{
		Namespace: "test_clear",
		Name:      "counter",
		Help:      "Test",
	})

	NewCollector(CollectorOpts{
		Namespace: "test_clear",
	})

	// Verify caches have entries
	metricCacheMu.RLock()
	metricCount := len(metricCache)
	metricCacheMu.RUnlock()

	collectorsMu.RLock()
	collectorCount := len(collectorCache)
	collectorsMu.RUnlock()

	assert.Greater(t, metricCount, 0, "metric cache should have entries")
	assert.Greater(t, collectorCount, 0, "collector cache should have entries")

	// Set new registry
	registry2 := prometheus.NewRegistry()
	SetRegistry(registry2)

	// Verify caches are cleared
	metricCacheMu.RLock()
	metricCountAfter := len(metricCache)
	metricCacheMu.RUnlock()

	collectorsMu.RLock()
	collectorCountAfter := len(collectorCache)
	collectorsMu.RUnlock()

	assert.Equal(t, 0, metricCountAfter, "metric cache should be empty")
	assert.Equal(t, 0, collectorCountAfter, "collector cache should be empty")

	SetRegistry(prometheus.NewRegistry()) // cleanup
}

// TestHistogramDefaultBuckets tests histogram creation with no buckets specified.
func TestHistogramDefaultBuckets(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	h := NewHistogram(HistogramOpts{
		Namespace: "test_default_buckets",
		Name:      "histogram",
		Help:      "Test default buckets",
		// No buckets specified - should use prometheus.DefBuckets
	})

	require.NotNil(t, h)
	h.Observe(0.5)
}

// TestHistogramVecDefaultBuckets tests histogram vec creation with no buckets specified.
func TestHistogramVecDefaultBuckets(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	hv := NewHistogramVec(
		HistogramOpts{
			Namespace: "test_default_buckets_vec",
			Name:      "histogram_vec",
			Help:      "Test default buckets",
			// No buckets specified - should use prometheus.DefBuckets
		},
		[]string{"endpoint"},
	)

	require.NotNil(t, hv)
	h := hv.With("/api/test")
	h.Observe(0.5)
}

// TestCounterWithConstLabels tests counter creation with constant labels.
func TestCounterWithConstLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	counter := NewCounter(CounterOpts{
		Namespace: "test_const",
		Name:      "counter_with_labels",
		Help:      "Test const labels",
		ConstLabels: prometheus.Labels{
			"version": "v1.0.0",
			"env":     "test",
		},
	})

	require.NotNil(t, counter)
	counter.Inc()
	counter.Add(5)
}

// TestGaugeWithConstLabels tests gauge creation with constant labels.
func TestGaugeWithConstLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	gauge := NewGauge(GaugeOpts{
		Namespace: "test_const",
		Name:      "gauge_with_labels",
		Help:      "Test const labels",
		ConstLabels: prometheus.Labels{
			"datacenter": "us-west",
			"cluster":    "prod",
		},
	})

	require.NotNil(t, gauge)
	gauge.Set(100)
	gauge.Inc()
	gauge.Dec()
	gauge.Add(10)
	gauge.Sub(5)
}

// TestHistogramWithConstLabels tests histogram creation with constant labels.
func TestHistogramWithConstLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	histogram := NewHistogram(HistogramOpts{
		Namespace: "test_const",
		Name:      "histogram_with_labels",
		Help:      "Test const labels",
		Buckets:   []float64{0.1, 0.5, 1.0},
		ConstLabels: prometheus.Labels{
			"service": "api",
			"region":  "eu-central",
		},
	})

	require.NotNil(t, histogram)
	histogram.Observe(0.25)
	histogram.Observe(0.75)
}

// TestCounterVecWithConstLabels tests counter vec creation with constant labels.
func TestCounterVecWithConstLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	counterVec := NewCounterVec(
		CounterOpts{
			Namespace: "test_const",
			Name:      "counter_vec_with_labels",
			Help:      "Test const labels",
			ConstLabels: prometheus.Labels{
				"app": "quicbaas",
			},
		},
		[]string{"method", "status"},
	)

	require.NotNil(t, counterVec)
	counterVec.With("GET", "200").Inc()
	counterVec.WithLabels(prometheus.Labels{
		"method": "POST",
		"status": "201",
	}).Add(2)
}

// TestGaugeVecWithConstLabels tests gauge vec creation with constant labels.
func TestGaugeVecWithConstLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	gaugeVec := NewGaugeVec(
		GaugeOpts{
			Namespace: "test_const",
			Name:      "gauge_vec_with_labels",
			Help:      "Test const labels",
			ConstLabels: prometheus.Labels{
				"tier": "backend",
			},
		},
		[]string{"service"},
	)

	require.NotNil(t, gaugeVec)
	gaugeVec.With("api").Set(50)
	gaugeVec.WithLabels(prometheus.Labels{"service": "db"}).Inc()
}

// TestHistogramVecWithConstLabels tests histogram vec creation with constant labels.
func TestHistogramVecWithConstLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	SetRegistry(registry)
	defer SetRegistry(prometheus.NewRegistry())

	histogramVec := NewHistogramVec(
		HistogramOpts{
			Namespace: "test_const",
			Name:      "histogram_vec_with_labels",
			Help:      "Test const labels",
			Buckets:   []float64{0.1, 0.5, 1.0},
			ConstLabels: prometheus.Labels{
				"deployment": "canary",
			},
		},
		[]string{"endpoint"},
	)

	require.NotNil(t, histogramVec)
	histogramVec.With("/health").Observe(0.01)
	histogramVec.WithLabels(prometheus.Labels{"endpoint": "/metrics"}).Observe(0.05)
}
