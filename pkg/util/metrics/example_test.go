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

package metrics_test

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// Example demonstrates basic counter usage
func ExampleNewCounter() {
	counter := metrics.NewCounter(metrics.CounterOpts{
		Namespace: "myapp",
		Subsystem: "requests",
		Name:      "total",
		Help:      "Total number of requests",
	})

	counter.Inc()
	counter.Add(5)

	fmt.Println("Counter incremented")
	// Output: Counter incremented
}

// Example demonstrates gauge usage
func ExampleNewGauge() {
	gauge := metrics.NewGauge(metrics.GaugeOpts{
		Namespace: "myapp",
		Name:      "connections",
		Help:      "Current number of active connections",
	})

	gauge.Set(10)
	gauge.Inc()
	gauge.Dec()

	fmt.Println("Gauge updated")
	// Output: Gauge updated
}

// Example demonstrates histogram usage for timing operations
func ExampleNewHistogram() {
	histogram := metrics.NewHistogram(metrics.HistogramOpts{
		Namespace: "myapp",
		Name:      "request_duration_seconds",
		Help:      "Request duration in seconds",
		Buckets:   metrics.Buckets.Milliseconds(),
	})

	histogram.Observe(0.125) // 125ms
	histogram.Observe(0.250) // 250ms

	fmt.Println("Request durations recorded")
	// Output: Request durations recorded
}

// Example demonstrates counter vector with labels
func ExampleNewCounterVec() {
	counterVec := metrics.NewCounterVec(
		metrics.CounterOpts{
			Namespace: "myapp",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests by method and status",
		},
		[]string{"method", "status"},
	)

	// Using variadic With method
	counterVec.With("GET", "200").Inc()
	counterVec.With("POST", "201").Inc()

	// Using WithLabels method
	counterVec.WithLabels(prometheus.Labels{
		"method": "GET",
		"status": "404",
	}).Inc()

	fmt.Println("HTTP requests recorded with labels")
	// Output: HTTP requests recorded with labels
}

// Example demonstrates using the Collector for common application metrics
func ExampleNewCollector() {
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Namespace:       "myapp",
		Subsystem:       "api",
		DurationBuckets: metrics.Buckets.Milliseconds(),
	})

	// Record a request
	collector.RecordRequest("GET", "200")
	collector.RecordRequestDuration("GET", "200", 150*time.Millisecond)

	// Track errors
	collector.RecordError("timeout", "database_query")

	// Track connections
	collector.IncActiveConnections()
	defer collector.DecActiveConnections()

	fmt.Println("Application metrics recorded")
	// Output: Application metrics recorded
}

// Example demonstrates using RequestTimer for automatic request tracking
func ExampleCollector_StartRequestTimer() {
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Namespace: "timer_example",
		Subsystem: "requests",
	})

	// Start timing a request
	timer := collector.StartRequestTimer("POST")

	// Simulate some work
	time.Sleep(50 * time.Millisecond)

	// Done will automatically:
	// - Record the request duration
	// - Increment the request counter
	// - Decrement the in-flight requests gauge
	timer.Done("201")

	fmt.Println("Request automatically timed and recorded")
	// Output: Request automatically timed and recorded
}

// Example demonstrates handling errors with RequestTimer
func ExampleRequestTimer_DoneWithError() {
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Namespace: "error_example",
	})

	timer := collector.StartRequestTimer("GET")

	// Simulate work that results in an error
	time.Sleep(25 * time.Millisecond)

	// DoneWithError records both request metrics and error metrics
	timer.DoneWithError("500", "internal_error")

	fmt.Println("Error request recorded with all metrics")
	// Output: Error request recorded with all metrics
}

// Example demonstrates using Timer for custom operations
func ExampleNewTimer() {
	histogram := metrics.NewHistogram(metrics.HistogramOpts{
		Namespace: "myapp",
		Name:      "operation_duration_seconds",
		Help:      "Duration of custom operation",
	})

	timer := metrics.NewTimer(histogram)

	// Perform some operation
	time.Sleep(10 * time.Millisecond)

	duration := timer.ObserveDuration()
	// Duration will be at least 10ms due to the sleep
	if duration >= 10*time.Millisecond {
		fmt.Println("Operation completed and timed")
	}
	// Output: Operation completed and timed
}

// Example demonstrates using different latency bucket configurations
func ExampleLatencyBuckets() {
	// For very fast operations (microseconds)
	microHistogram := metrics.NewHistogram(metrics.HistogramOpts{
		Namespace: "myapp",
		Name:      "cache_lookup_duration",
		Help:      "Cache lookup duration",
		Buckets:   metrics.Buckets.Microseconds(),
	})
	microHistogram.Observe(0.000050) // 50μs

	// For fast operations (milliseconds)
	milliHistogram := metrics.NewHistogram(metrics.HistogramOpts{
		Namespace: "myapp",
		Name:      "api_request_duration",
		Help:      "API request duration",
		Buckets:   metrics.Buckets.Milliseconds(),
	})
	milliHistogram.Observe(0.125) // 125ms

	// For slow operations (seconds)
	secondHistogram := metrics.NewHistogram(metrics.HistogramOpts{
		Namespace: "myapp",
		Name:      "batch_job_duration",
		Help:      "Batch job duration",
		Buckets:   metrics.Buckets.Seconds(),
	})
	secondHistogram.Observe(5.5) // 5.5s

	// Custom buckets
	customHistogram := metrics.NewHistogram(metrics.HistogramOpts{
		Namespace: "myapp",
		Name:      "custom_duration",
		Help:      "Custom operation duration",
		Buckets:   metrics.Buckets.Custom(0.01, 0.1, 1.0, 10.0, 100.0),
	})
	customHistogram.Observe(0.5)

	fmt.Println("Different bucket configurations used")
	// Output: Different bucket configurations used
}

// Example demonstrates managing multiple registries
func ExampleSetRegistry() {
	// Create a custom registry
	customRegistry := prometheus.NewRegistry()
	metrics.SetRegistry(customRegistry)

	// All new metrics will use this registry
	counter := metrics.NewCounter(metrics.CounterOpts{
		Namespace: "custom",
		Name:      "counter",
		Help:      "A counter in custom registry",
	})
	counter.Inc()

	// Get the current registry
	registry := metrics.Registry()
	metricFamilies, _ := registry.Gather()

	fmt.Printf("Metrics in custom registry: %d\n", len(metricFamilies))
	// Output: Metrics in custom registry: 1
}

// Example demonstrates a complete HTTP request handler pattern
func ExampleCollector_httpHandler() {
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Namespace: "http_example",
		Subsystem: "handler",
	})

	// Simulate HTTP request handler
	handleRequest := func(method string) {
		// Track connection
		collector.IncActiveConnections()
		defer collector.DecActiveConnections()

		// Start request timer
		timer := collector.StartRequestTimer(method)

		// Simulate request processing
		time.Sleep(100 * time.Millisecond)

		// Determine status (simulate success)
		status := "200"
		timer.Done(status)
	}

	handleRequest("GET")
	handleRequest("POST")

	fmt.Println("HTTP requests handled with metrics")
	// Output: HTTP requests handled with metrics
}

// Example demonstrates error handling with metrics
func ExampleCollector_errorHandling() {
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Namespace: "db_example",
		Subsystem: "queries",
	})

	// Simulate database operation
	executeQuery := func() error {
		timer := collector.StartRequestTimer("query")

		// Simulate query execution
		time.Sleep(50 * time.Millisecond)

		// Simulate error
		err := &dragonerrors.MessageError{Message: "connection timeout"}
		if err != nil {
			timer.DoneWithError("500", "timeout")
			return err
		}

		timer.Done("200")
		return nil
	}

	err := executeQuery()
	if err != nil {
		fmt.Println("Query failed with metrics recorded")
	}
	// Output: Query failed with metrics recorded
}
