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

// Package metrics provides comprehensive Prometheus metrics for QuicBaaS Phase 4.
//
// This package implements three categories of metrics following Prometheus best practices:
//
// 1. Request-level metrics - Track request performance and concurrency
// 2. Transaction metrics - Monitor transaction behavior and conflicts
// 3. Raft-specific metrics - Observe Raft consensus performance
//
// # Usage
//
// Initialize metrics at application startup:
//
//	m := metrics.InitMetrics()
//
// Record request metrics:
//
//	start := time.Now()
//	m.IncrementConcurrentRequests()
//	defer m.DecrementConcurrentRequests()
//
//	// ... perform operation ...
//
//	duration := time.Since(start).Seconds()
//	m.RecordRequestDuration("get", "success", duration)
//	m.RecordRequestSize("get", float64(len(payload)))
//
// Record transaction metrics:
//
//	start := time.Now()
//	// ... execute transaction ...
//	duration := time.Since(start).Seconds()
//	m.RecordTransactionDuration(duration)
//	m.RecordTransactionSize(float64(numOperations))
//
//	// On conflict:
//	if err == ErrConflict {
//	    m.IncrementTransactionConflicts()
//	}
//
// Record Raft metrics:
//
//	start := time.Now()
//	// ... submit Raft proposal ...
//	duration := time.Since(start).Seconds()
//	m.RecordRaftProposalLatency("1", duration)
//
//	// Update snapshot size:
//	m.SetRaftSnapshotSize("1", float64(snapshotBytes))
//
// # Prometheus Integration
//
// Expose metrics via HTTP endpoint:
//
//	import "github.com/prometheus/client_golang/prometheus/promhttp"
//
//	http.Handle("/metrics", promhttp.HandlerFor(
//	    metrics.Registry,
//	    promhttp.HandlerOpts{},
//	))
//	http.ListenAndServe(":9090", nil)
//
// # Metric Specifications
//
// Request Metrics:
//   - quicbaas_request_duration_seconds: Histogram with operation and status labels
//     Buckets: 1ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s
//   - quicbaas_request_size_bytes: Histogram with operation label
//     Buckets: Exponential from 100 bytes to 100MB
//   - quicbaas_concurrent_requests: Gauge tracking current concurrent requests
//
// Transaction Metrics:
//   - quicbaas_transaction_duration_seconds: Histogram
//     Buckets: 1ms, 10ms, 100ms, 1s, 10s
//   - quicbaas_transaction_operations: Histogram
//     Buckets: 1, 5, 10, 25, 50, 100, 250, 500
//   - quicbaas_transaction_conflicts_total: Counter
//
// Raft Metrics:
//   - quicbaas_raft_proposal_latency_seconds: Histogram with shard label
//     Buckets: 1ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms
//   - quicbaas_raft_snapshot_size_bytes: Gauge with shard label
//
// # Best Practices
//
// 1. Use consistent label values (e.g., operation names should be lowercase)
// 2. Increment/decrement concurrent requests using defer for guaranteed cleanup
// 3. Record durations in seconds for consistency with Prometheus conventions
// 4. Record sizes in bytes for consistency with Prometheus conventions
// 5. Use string shard IDs consistently across all Raft metrics
//
// # Performance
//
// All metrics operations are highly optimized:
//   - Request duration recording: ~42 ns/op
//   - Concurrent requests increment: ~9 ns/op
//   - Transaction metrics: ~40 ns/op
//   - Raft metrics: ~114 ns/op
//
// All operations are zero-allocation and thread-safe.
package metrics
