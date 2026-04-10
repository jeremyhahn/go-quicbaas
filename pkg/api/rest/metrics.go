//go:build rest

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

package rest

import (
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// API-specific metrics for detailed REST observability.
var (
	apiMetricsOnce sync.Once
	apiMetrics     *restAPIMetrics
)

// restAPIMetrics tracks detailed REST API performance.
type restAPIMetrics struct {
	// Request metrics
	requestDuration *metrics.HistogramVec // Latency by endpoint
	requestSize     *metrics.HistogramVec // Request payload size
	responseSize    *metrics.HistogramVec // Response payload size

	// Error metrics
	errorsByType *metrics.CounterVec // Errors by error code

	// Connection metrics
	activeConnections *metrics.Gauge // Current active connections
	totalConnections  *metrics.Counter

	// Operation metrics
	putOperations    *metrics.Counter
	getOperations    *metrics.Counter
	deleteOperations *metrics.Counter
	queryOperations  *metrics.Counter
	listOperations   *metrics.Counter

	// Success/failure rates
	successfulOps *metrics.CounterVec
	failedOps     *metrics.CounterVec

	// Batch operation metrics
	batchSize    *metrics.Histogram // Batch operation sizes
	batchLatency *metrics.Histogram // Batch operation latency
}

// initAPIMetrics initializes REST API-specific metrics.
func initAPIMetrics() {
	apiMetricsOnce.Do(func() {
		apiMetrics = &restAPIMetrics{
			requestDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "request_duration_seconds",
				Help:      "REST API request duration by endpoint",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
			}, []string{"endpoint", "method"}),

			requestSize: metrics.NewHistogramVec(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "request_size_bytes",
				Help:      "REST API request payload size",
				Buckets:   []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576},
			}, []string{"endpoint"}),

			responseSize: metrics.NewHistogramVec(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "response_size_bytes",
				Help:      "REST API response payload size",
				Buckets:   []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576},
			}, []string{"endpoint"}),

			errorsByType: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "errors_total",
				Help:      "Total REST API errors by type",
			}, []string{"error_code", "endpoint"}),

			activeConnections: metrics.NewGauge(metrics.GaugeOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "active_connections",
				Help:      "Number of active REST API connections",
			}),

			totalConnections: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "connections_total",
				Help:      "Total number of REST API connections",
			}),

			putOperations: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "put_operations_total",
				Help:      "Total number of PUT operations",
			}),

			getOperations: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "get_operations_total",
				Help:      "Total number of GET operations",
			}),

			deleteOperations: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "delete_operations_total",
				Help:      "Total number of DELETE operations",
			}),

			queryOperations: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "query_operations_total",
				Help:      "Total number of QUERY operations",
			}),

			listOperations: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "list_operations_total",
				Help:      "Total number of LIST operations",
			}),

			successfulOps: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "successful_operations_total",
				Help:      "Total successful operations by type",
			}, []string{"operation"}),

			failedOps: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "failed_operations_total",
				Help:      "Total failed operations by type",
			}, []string{"operation", "reason"}),

			batchSize: metrics.NewHistogram(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "batch_operation_size",
				Help:      "Number of items in batch operations",
				Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
			}),

			batchLatency: metrics.NewHistogram(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "rest_api",
				Name:      "batch_operation_duration_seconds",
				Help:      "Batch operation duration",
				Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10},
			}),
		}
	})
}

// getAPIMetrics returns the API metrics singleton.
func getAPIMetrics() *restAPIMetrics {
	initAPIMetrics()
	return apiMetrics
}
