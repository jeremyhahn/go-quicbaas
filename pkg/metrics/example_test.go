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

	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

func Example() {
	// Initialize metrics
	m := metrics.InitMetrics()

	// Record a request
	start := time.Now()
	m.IncrementConcurrentRequests()
	defer m.DecrementConcurrentRequests()

	// Simulate request processing
	time.Sleep(10 * time.Millisecond)

	// Record request metrics
	duration := time.Since(start).Seconds()
	m.RecordRequestDuration("get", "success", duration)
	m.RecordRequestSize("get", 1024)

	fmt.Println("Request metrics recorded")

	// Output: Request metrics recorded
}

func ExampleMetrics_recordRequestDuration() {
	m := metrics.GetMetrics()

	// Record request duration with operation and status labels
	m.RecordRequestDuration("get", "success", 0.015)
	m.RecordRequestDuration("put", "success", 0.025)
	m.RecordRequestDuration("delete", "failure", 0.050)

	fmt.Println("Request durations recorded")

	// Output: Request durations recorded
}

func Example_transactionMetrics() {
	m := metrics.GetMetrics()

	// Record transaction metrics
	start := time.Now()

	// Simulate transaction execution
	numOperations := 10
	time.Sleep(100 * time.Millisecond)

	duration := time.Since(start).Seconds()
	m.RecordTransactionDuration(duration)
	m.RecordTransactionSize(float64(numOperations))

	// Record conflict if one occurred
	conflictOccurred := false
	if conflictOccurred {
		m.IncrementTransactionConflicts()
	}

	fmt.Println("Transaction metrics recorded")

	// Output: Transaction metrics recorded
}

func Example_raftMetrics() {
	m := metrics.GetMetrics()

	// Record Raft proposal latency for shard 1
	shardID := "1"
	proposalLatency := 0.005 // 5ms
	m.RecordRaftProposalLatency(shardID, proposalLatency)

	// Update snapshot size for shard 1
	snapshotSize := float64(10 * 1024 * 1024) // 10MB
	m.SetRaftSnapshotSize(shardID, snapshotSize)

	fmt.Println("Raft metrics recorded")

	// Output: Raft metrics recorded
}

func Example_concurrentRequests() {
	m := metrics.GetMetrics()

	// Track concurrent requests
	m.IncrementConcurrentRequests()
	defer m.DecrementConcurrentRequests()

	// Process request
	fmt.Println("Processing request with concurrent tracking")

	// Output: Processing request with concurrent tracking
}

// Example showing how to use metrics with error handling.
func Example_withErrorHandling() {
	m := metrics.GetMetrics()

	start := time.Now()
	m.IncrementConcurrentRequests()
	defer m.DecrementConcurrentRequests()

	// Simulate operation that might fail
	err := performOperation()

	duration := time.Since(start).Seconds()

	if err != nil {
		m.RecordRequestDuration("put", "failure", duration)
		fmt.Println("Request failed")
	} else {
		m.RecordRequestDuration("put", "success", duration)
		fmt.Println("Request succeeded")
	}

	// Output: Request succeeded
}

// Example showing middleware pattern for automatic metrics.
func Example_middleware() {
	m := metrics.GetMetrics()

	// Middleware wraps a handler function
	handler := func(operation string, fn func() error) error {
		start := time.Now()
		m.IncrementConcurrentRequests()
		defer m.DecrementConcurrentRequests()

		err := fn()

		duration := time.Since(start).Seconds()
		status := "success"
		if err != nil {
			status = "failure"
		}
		m.RecordRequestDuration(operation, status, duration)

		return err
	}

	// Use the middleware
	err := handler("get", func() error {
		// Perform operation
		return nil
	})

	if err == nil {
		fmt.Println("Middleware pattern works")
	}

	// Output: Middleware pattern works
}

// Example showing transaction monitoring.
func Example_transactionMonitoring() {
	m := metrics.GetMetrics()

	// Begin transaction
	start := time.Now()
	operations := []string{"put:key1", "put:key2", "delete:key3"}

	// Execute operations
	for range operations {
		// Simulate operation
		time.Sleep(1 * time.Millisecond)
	}

	// Commit transaction
	duration := time.Since(start).Seconds()
	m.RecordTransactionDuration(duration)
	m.RecordTransactionSize(float64(len(operations)))

	fmt.Printf("Transaction with %d operations completed\n", len(operations))

	// Output: Transaction with 3 operations completed
}

// Example showing Raft consensus monitoring.
func Example_raftMonitoring() {
	m := metrics.GetMetrics()

	// Monitor Raft proposals for multiple shards
	shards := []string{"1", "2", "3"}

	for _, shard := range shards {
		// Simulate Raft proposal
		start := time.Now()
		time.Sleep(5 * time.Millisecond)
		latency := time.Since(start).Seconds()

		m.RecordRaftProposalLatency(shard, latency)

		// Update snapshot size
		snapshotSize := float64(1024 * 1024 * (len(shard))) // 1MB per shard number
		m.SetRaftSnapshotSize(shard, snapshotSize)
	}

	fmt.Println("Raft metrics recorded for 3 shards")

	// Output: Raft metrics recorded for 3 shards
}

// Helper function for examples.
func performOperation() error {
	// Simulate successful operation
	return nil
}
