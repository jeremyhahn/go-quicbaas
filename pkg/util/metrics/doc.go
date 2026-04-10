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

// Package metrics provides a convenient wrapper around Prometheus client_golang
// for collecting and exposing application metrics.
//
// This package simplifies the creation and management of Prometheus metrics by
// providing easy-to-use constructors, automatic registration, and pre-built
// collectors for common application patterns.
//
// # Basic Usage
//
// Create metrics using the New* functions:
//
//	counter := metrics.NewCounter(metrics.CounterOpts{
//	    Namespace: "myapp",
//	    Name:      "requests_total",
//	    Help:      "Total number of requests",
//	})
//	counter.Inc()
//
//	gauge := metrics.NewGauge(metrics.GaugeOpts{
//	    Namespace: "myapp",
//	    Name:      "connections",
//	    Help:      "Active connections",
//	})
//	gauge.Set(42)
//
//	histogram := metrics.NewHistogram(metrics.HistogramOpts{
//	    Namespace: "myapp",
//	    Name:      "request_duration_seconds",
//	    Help:      "Request duration",
//	    Buckets:   metrics.Buckets.Milliseconds(),
//	})
//	histogram.Observe(0.125)
//
// # Using Labels
//
// Create metric vectors for metrics with labels:
//
//	counterVec := metrics.NewCounterVec(
//	    metrics.CounterOpts{
//	        Namespace: "myapp",
//	        Name:      "http_requests_total",
//	        Help:      "HTTP requests",
//	    },
//	    []string{"method", "status"},
//	)
//
//	// Variadic style
//	counterVec.With("GET", "200").Inc()
//
//	// Label map style
//	counterVec.WithLabels(prometheus.Labels{
//	    "method": "POST",
//	    "status": "201",
//	}).Inc()
//
// # Using the Collector
//
// The Collector type provides a convenient interface for common application
// metrics including request duration, request count, in-flight requests,
// error counts, and active connections:
//
//	collector := metrics.NewCollector(metrics.CollectorOpts{
//	    Namespace: "myapp",
//	    Subsystem: "api",
//	})
//
//	// Manual tracking
//	collector.RecordRequest("GET", "200")
//	collector.RecordRequestDuration("GET", "200", 150*time.Millisecond)
//
//	// Automatic tracking with timer
//	timer := collector.StartRequestTimer("POST")
//	defer timer.Done("201")
//
// # Request Timers
//
// RequestTimer automatically tracks request duration, count, and in-flight
// requests:
//
//	func handleRequest(collector *metrics.Collector) {
//	    timer := collector.StartRequestTimer("GET")
//	    defer timer.Done("200")
//
//	    // Your request handling logic
//	}
//
// For error cases:
//
//	timer := collector.StartRequestTimer("GET")
//	if err := process(); err != nil {
//	    timer.DoneWithError("500", "internal_error")
//	    return
//	}
//	timer.Done("200")
//
// # Histogram Buckets
//
// The package provides pre-configured bucket sets for different latency ranges:
//
//	// For microsecond-level operations (1μs to 1ms)
//	Buckets.Microseconds()
//
//	// For millisecond-level operations (1ms to 1s)
//	Buckets.Milliseconds()
//
//	// For second-level operations (100ms to 60s)
//	Buckets.Seconds()
//
//	// Default buckets (1ms to 10s)
//	Buckets.Default()
//
//	// Custom buckets
//	Buckets.Custom(0.001, 0.01, 0.1, 1.0)
//
// # Registry Management
//
// By default, the package uses its own Prometheus registry. You can access or
// replace it:
//
//	registry := metrics.Registry()
//
//	customRegistry := prometheus.NewRegistry()
//	metrics.SetRegistry(customRegistry)
//
// # Exposing Metrics
//
// To expose metrics via HTTP:
//
//	import "github.com/prometheus/client_golang/prometheus/promhttp"
//
//	http.Handle("/metrics", promhttp.HandlerFor(
//	    metrics.Registry(),
//	    promhttp.HandlerOpts{},
//	))
//
// # Thread Safety
//
// All operations are thread-safe and can be safely called from multiple
// goroutines concurrently.
//
// # Performance
//
// The package is optimized for low overhead with minimal allocations in hot
// paths. Typical operation costs:
//   - Counter increment: ~20-30 ns/op
//   - Histogram observation: ~150-200 ns/op
//   - Timer with observation: ~500-600 ns/op
package metrics
