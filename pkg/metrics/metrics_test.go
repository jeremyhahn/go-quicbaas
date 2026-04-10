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
	"fmt"
	"strings"
	"testing"

	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMetrics(t *testing.T) {
	// Reset metrics before test
	Reset()

	metrics := InitMetrics()
	require.NotNil(t, metrics, "Metrics should be initialized")

	// Verify all metrics are registered
	assert.NotNil(t, metrics.RequestDuration, "RequestDuration should be initialized")
	assert.NotNil(t, metrics.RequestSize, "RequestSize should be initialized")
	assert.NotNil(t, metrics.ConcurrentRequests, "ConcurrentRequests should be initialized")
	assert.NotNil(t, metrics.TransactionDuration, "TransactionDuration should be initialized")
	assert.NotNil(t, metrics.TransactionSize, "TransactionSize should be initialized")
	assert.NotNil(t, metrics.TransactionConflicts, "TransactionConflicts should be initialized")
	assert.NotNil(t, metrics.RaftProposalLatency, "RaftProposalLatency should be initialized")
	assert.NotNil(t, metrics.RaftSnapshotSize, "RaftSnapshotSize should be initialized")
}

func TestInitMetrics_Singleton(t *testing.T) {
	// Reset metrics before test
	Reset()

	metrics1 := InitMetrics()
	metrics2 := InitMetrics()

	// Should return the same instance
	assert.Equal(t, metrics1, metrics2, "InitMetrics should return singleton instance")
}

func TestGetMetrics(t *testing.T) {
	// Reset metrics before test
	Reset()

	metrics := GetMetrics()
	require.NotNil(t, metrics, "GetMetrics should return initialized metrics")

	// Calling again should return same instance
	metrics2 := GetMetrics()
	assert.Equal(t, metrics, metrics2, "GetMetrics should return singleton instance")
}

func TestRecordRequestDuration(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	tests := []struct {
		name      string
		operation string
		status    string
		duration  float64
	}{
		{
			name:      "put operation success",
			operation: "put",
			status:    "success",
			duration:  0.015,
		},
		{
			name:      "get operation success",
			operation: "get",
			status:    "success",
			duration:  0.005,
		},
		{
			name:      "delete operation failure",
			operation: "delete",
			status:    "failure",
			duration:  0.025,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordRequestDuration(tt.operation, tt.status, tt.duration)

			// Verify metric was recorded
			count := testutil.CollectAndCount(m.RequestDuration)
			assert.Greater(t, count, 0, "RequestDuration should have recorded values")
		})
	}
}

func TestRecordRequestSize(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	tests := []struct {
		name      string
		operation string
		size      float64
	}{
		{
			name:      "small request",
			operation: "put",
			size:      100,
		},
		{
			name:      "medium request",
			operation: "put",
			size:      10000,
		},
		{
			name:      "large request",
			operation: "put",
			size:      1000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordRequestSize(tt.operation, tt.size)

			// Verify metric was recorded
			count := testutil.CollectAndCount(m.RequestSize)
			assert.Greater(t, count, 0, "RequestSize should have recorded values")
		})
	}
}

func TestConcurrentRequests(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	// Initial value should be 0
	value := testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 0.0, value, "ConcurrentRequests should start at 0")

	// Increment
	m.IncrementConcurrentRequests()
	value = testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 1.0, value, "ConcurrentRequests should be 1 after increment")

	// Increment again
	m.IncrementConcurrentRequests()
	value = testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 2.0, value, "ConcurrentRequests should be 2 after second increment")

	// Decrement
	m.DecrementConcurrentRequests()
	value = testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 1.0, value, "ConcurrentRequests should be 1 after decrement")

	// Decrement to zero
	m.DecrementConcurrentRequests()
	value = testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 0.0, value, "ConcurrentRequests should be 0 after second decrement")
}

func TestRecordTransactionDuration(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	tests := []struct {
		name     string
		duration float64
	}{
		{
			name:     "fast transaction",
			duration: 0.001,
		},
		{
			name:     "medium transaction",
			duration: 0.1,
		},
		{
			name:     "slow transaction",
			duration: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordTransactionDuration(tt.duration)

			// Verify metric was recorded
			count := testutil.CollectAndCount(m.TransactionDuration)
			assert.Greater(t, count, 0, "TransactionDuration should have recorded values")
		})
	}
}

func TestRecordTransactionSize(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	tests := []struct {
		name string
		size float64
	}{
		{
			name: "single operation",
			size: 1,
		},
		{
			name: "small batch",
			size: 10,
		},
		{
			name: "large batch",
			size: 100,
		},
		{
			name: "very large batch",
			size: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordTransactionSize(tt.size)

			// Verify metric was recorded
			count := testutil.CollectAndCount(m.TransactionSize)
			assert.Greater(t, count, 0, "TransactionSize should have recorded values")
		})
	}
}

func TestIncrementTransactionConflicts(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	// Initial value should be 0
	value := testutil.ToFloat64(m.TransactionConflicts)
	assert.Equal(t, 0.0, value, "TransactionConflicts should start at 0")

	// Increment
	m.IncrementTransactionConflicts()
	value = testutil.ToFloat64(m.TransactionConflicts)
	assert.Equal(t, 1.0, value, "TransactionConflicts should be 1 after increment")

	// Increment again
	m.IncrementTransactionConflicts()
	value = testutil.ToFloat64(m.TransactionConflicts)
	assert.Equal(t, 2.0, value, "TransactionConflicts should be 2 after second increment")
}

func TestRecordRaftProposalLatency(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	tests := []struct {
		name    string
		shard   string
		latency float64
	}{
		{
			name:    "shard 1 fast proposal",
			shard:   "1",
			latency: 0.001,
		},
		{
			name:    "shard 1 slow proposal",
			shard:   "1",
			latency: 0.1,
		},
		{
			name:    "shard 2 fast proposal",
			shard:   "2",
			latency: 0.005,
		},
		{
			name:    "shard 2 medium proposal",
			shard:   "2",
			latency: 0.05,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordRaftProposalLatency(tt.shard, tt.latency)

			// Verify metric was recorded
			count := testutil.CollectAndCount(m.RaftProposalLatency)
			assert.Greater(t, count, 0, "RaftProposalLatency should have recorded values")
		})
	}
}

func TestSetRaftSnapshotSize(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	tests := []struct {
		name  string
		shard string
		size  float64
	}{
		{
			name:  "shard 1 small snapshot",
			shard: "1",
			size:  1024,
		},
		{
			name:  "shard 1 medium snapshot",
			shard: "1",
			size:  1048576, // 1MB
		},
		{
			name:  "shard 2 large snapshot",
			shard: "2",
			size:  10485760, // 10MB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetRaftSnapshotSize(tt.shard, tt.size)

			// Verify metric was set
			labels := prometheus.Labels{"shard": tt.shard}
			value := testutil.ToFloat64(m.RaftSnapshotSize.With(labels))
			assert.Equal(t, tt.size, value, "RaftSnapshotSize should be set correctly")
		})
	}
}

func TestMetricsLabels(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	// Test RequestDuration labels
	m.RecordRequestDuration("get", "success", 0.01)
	m.RecordRequestDuration("put", "failure", 0.02)

	// Test RaftProposalLatency labels
	m.RecordRaftProposalLatency("1", 0.005)
	m.RecordRaftProposalLatency("2", 0.010)

	// Test RaftSnapshotSize labels
	m.SetRaftSnapshotSize("1", 1000)
	m.SetRaftSnapshotSize("2", 2000)

	// Verify labels are working by checking we can retrieve different values
	shard1Size := testutil.ToFloat64(m.RaftSnapshotSize.With(prometheus.Labels{"shard": "1"}))
	shard2Size := testutil.ToFloat64(m.RaftSnapshotSize.With(prometheus.Labels{"shard": "2"}))

	assert.Equal(t, 1000.0, shard1Size, "Shard 1 size should be 1000")
	assert.Equal(t, 2000.0, shard2Size, "Shard 2 size should be 2000")
}

func TestMetricsBuckets(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	// Test RequestDuration buckets
	t.Run("RequestDuration buckets", func(t *testing.T) {
		m.RecordRequestDuration("get", "success", 0.001)
		m.RecordRequestDuration("get", "success", 0.01)
		m.RecordRequestDuration("get", "success", 0.1)
		m.RecordRequestDuration("get", "success", 1.0)

		count := testutil.CollectAndCount(m.RequestDuration)
		assert.Greater(t, count, 0, "RequestDuration should have buckets")
	})

	// Test RequestSize buckets (exponential)
	t.Run("RequestSize exponential buckets", func(t *testing.T) {
		m.RecordRequestSize("put", 100)
		m.RecordRequestSize("put", 1000)
		m.RecordRequestSize("put", 10000)
		m.RecordRequestSize("put", 100000)

		count := testutil.CollectAndCount(m.RequestSize)
		assert.Greater(t, count, 0, "RequestSize should have exponential buckets")
	})

	// Test TransactionSize buckets
	t.Run("TransactionSize buckets", func(t *testing.T) {
		m.RecordTransactionSize(1)
		m.RecordTransactionSize(10)
		m.RecordTransactionSize(50)
		m.RecordTransactionSize(100)
		m.RecordTransactionSize(500)

		count := testutil.CollectAndCount(m.TransactionSize)
		assert.Greater(t, count, 0, "TransactionSize should have buckets")
	})
}

func TestMetricsRegistration(t *testing.T) {
	// Reset metrics before test
	Reset()

	// Initialize metrics
	m := InitMetrics()

	// Record some values to ensure metrics appear in registry
	// Histograms and histogram vectors don't appear until they have data
	m.RecordRequestDuration("test", "success", 0.01)
	m.RecordRequestSize("test", 1000)
	m.IncrementConcurrentRequests()
	m.RecordTransactionDuration(0.1)
	m.RecordTransactionSize(10)
	m.IncrementTransactionConflicts()
	m.RecordRaftProposalLatency("1", 0.005)
	m.SetRaftSnapshotSize("1", 1024)

	// Gather metrics from registry
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err, "Should be able to gather metrics")

	// Define expected metrics
	expectedMetrics := []string{
		"quicbaas_request_duration_seconds",
		"quicbaas_request_size_bytes",
		"quicbaas_concurrent_requests",
		"quicbaas_transaction_duration_seconds",
		"quicbaas_transaction_operations",
		"quicbaas_transaction_conflicts_total",
		"quicbaas_raft_proposal_latency_seconds",
		"quicbaas_raft_snapshot_size_bytes",
	}

	// Build map of registered metric names
	registeredMetrics := make(map[string]bool)
	for _, mf := range metricFamilies {
		registeredMetrics[mf.GetName()] = true
	}

	// Verify all expected metrics are registered
	for _, expected := range expectedMetrics {
		assert.True(t, registeredMetrics[expected], "Metric %s should be registered", expected)
	}
}

func TestMetricsHelpText(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	// Record values to ensure metrics appear in registry
	m.RecordRequestDuration("test", "success", 0.01)
	m.RecordRequestSize("test", 1000)
	m.IncrementConcurrentRequests()
	m.RecordTransactionDuration(0.1)
	m.RecordTransactionSize(10)
	m.IncrementTransactionConflicts()
	m.RecordRaftProposalLatency("1", 0.005)
	m.SetRaftSnapshotSize("1", 1024)

	metricFamilies, err := Registry.Gather()
	require.NoError(t, err, "Should be able to gather metrics")

	expectedHelp := map[string]string{
		"quicbaas_request_duration_seconds":      "Request latency by operation and status",
		"quicbaas_request_size_bytes":            "Request payload size in bytes",
		"quicbaas_concurrent_requests":           "Number of requests currently being processed",
		"quicbaas_transaction_duration_seconds":  "Transaction execution time",
		"quicbaas_transaction_operations":        "Number of operations per transaction",
		"quicbaas_transaction_conflicts_total":   "Number of transaction conflicts (optimistic locking failures)",
		"quicbaas_raft_proposal_latency_seconds": "Time from proposal to commit by shard",
		"quicbaas_raft_snapshot_size_bytes":      "Current snapshot size by shard",
	}

	for _, mf := range metricFamilies {
		name := mf.GetName()
		if expectedHelp[name] != "" {
			assert.Equal(t, expectedHelp[name], mf.GetHelp(),
				"Help text for %s should match", name)
		}
	}
}

func TestReset(t *testing.T) {
	// Initialize metrics
	m1 := InitMetrics()
	require.NotNil(t, m1)

	// Record some values
	m1.IncrementConcurrentRequests()
	m1.IncrementTransactionConflicts()

	// Reset metrics
	Reset()

	// Get new instance
	m2 := GetMetrics()
	require.NotNil(t, m2)

	// Should be a different instance
	assert.NotEqual(t, m1, m2, "Reset should create new instance")

	// Values should be reset
	value := testutil.ToFloat64(m2.ConcurrentRequests)
	assert.Equal(t, 0.0, value, "ConcurrentRequests should be reset to 0")

	conflictsValue := testutil.ToFloat64(m2.TransactionConflicts)
	assert.Equal(t, 0.0, conflictsValue, "TransactionConflicts should be reset to 0")
}

func TestConcurrentAccess(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	// Test concurrent access to metrics
	done := make(chan bool)
	iterations := 100

	// Concurrent increments
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				m.IncrementConcurrentRequests()
				m.IncrementTransactionConflicts()
				m.RecordRequestDuration("get", "success", 0.01)
				m.RecordRaftProposalLatency("1", 0.005)
				m.DecrementConcurrentRequests()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify metrics were recorded (should not panic or corrupt data)
	conflicts := testutil.ToFloat64(m.TransactionConflicts)
	assert.Equal(t, float64(10*iterations), conflicts, "Should record all conflicts")
}

func TestMetricsExport(t *testing.T) {
	// Reset metrics before test
	Reset()

	m := InitMetrics()

	// Record various metrics
	m.RecordRequestDuration("get", "success", 0.005)
	m.RecordRequestSize("put", 1024)
	m.IncrementConcurrentRequests()
	m.RecordTransactionDuration(0.1)
	m.RecordTransactionSize(5)
	m.IncrementTransactionConflicts()
	m.RecordRaftProposalLatency("1", 0.002)
	m.SetRaftSnapshotSize("1", 2048)

	// Gather all metrics
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err, "Should be able to gather metrics")
	assert.NotEmpty(t, metricFamilies, "Should have metric families")

	// Verify we can export metrics (basic smoke test)
	for _, mf := range metricFamilies {
		assert.NotEmpty(t, mf.GetName(), "Metric should have a name")
		if strings.HasPrefix(mf.GetName(), "quicbaas_") {
			assert.NotEmpty(t, mf.GetMetric(), "QuicBaaS metric should have values")
		}
	}
}

func BenchmarkRecordRequestDuration(b *testing.B) {
	Reset()
	m := InitMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordRequestDuration("get", "success", 0.01)
	}
}

func BenchmarkIncrementConcurrentRequests(b *testing.B) {
	Reset()
	m := InitMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.IncrementConcurrentRequests()
	}
}

func BenchmarkRecordTransactionMetrics(b *testing.B) {
	Reset()
	m := InitMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordTransactionDuration(0.1)
		m.RecordTransactionSize(10)
		m.IncrementTransactionConflicts()
	}
}

func BenchmarkRecordRaftMetrics(b *testing.B) {
	Reset()
	m := InitMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordRaftProposalLatency(fmt.Sprintf("%d", i%10), 0.005)
		m.SetRaftSnapshotSize(fmt.Sprintf("%d", i%10), 1024)
	}
}

// Vector multi-tenant metrics tests

func TestVectorMetricsInitialization(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Verify all vector metrics are initialized
	assert.NotNil(t, m.VectorOperationsTotal, "VectorOperationsTotal should be initialized")
	assert.NotNil(t, m.VectorOperationDuration, "VectorOperationDuration should be initialized")
	assert.NotNil(t, m.VectorStorageBytes, "VectorStorageBytes should be initialized")
	assert.NotNil(t, m.VectorCount, "VectorCount should be initialized")
	assert.NotNil(t, m.VectorSearchLatency, "VectorSearchLatency should be initialized")
	assert.NotNil(t, m.VectorBatchSize, "VectorBatchSize should be initialized")
	assert.NotNil(t, m.VectorQuotaUsage, "VectorQuotaUsage should be initialized")
	assert.NotNil(t, m.VectorNamespaceCount, "VectorNamespaceCount should be initialized")
}

func TestRecordVectorOperation(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		tenantID  string
		namespace string
		operation string
		status    string
	}{
		{
			name:      "upsert success",
			tenantID:  "tenant-1",
			namespace: "embeddings",
			operation: "upsert",
			status:    "success",
		},
		{
			name:      "search success",
			tenantID:  "tenant-1",
			namespace: "embeddings",
			operation: "search",
			status:    "success",
		},
		{
			name:      "delete failure",
			tenantID:  "tenant-2",
			namespace: "documents",
			operation: "delete",
			status:    "failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordVectorOperation(tt.tenantID, tt.namespace, tt.operation, tt.status)

			// Verify metric was recorded
			labels := prometheus.Labels{
				"tenant_id": tt.tenantID,
				"namespace": tt.namespace,
				"operation": tt.operation,
				"status":    tt.status,
			}
			value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
			assert.Equal(t, 1.0, value, "VectorOperationsTotal should be 1")
		})
	}
}

func TestRecordVectorOperationDuration(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		tenantID  string
		namespace string
		operation string
		duration  float64
	}{
		{
			name:      "fast upsert",
			tenantID:  "tenant-1",
			namespace: "embeddings",
			operation: "upsert",
			duration:  0.001,
		},
		{
			name:      "slow search",
			tenantID:  "tenant-1",
			namespace: "embeddings",
			operation: "search",
			duration:  0.5,
		},
		{
			name:      "batch upsert",
			tenantID:  "tenant-2",
			namespace: "documents",
			operation: "batch_upsert",
			duration:  2.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordVectorOperationDuration(tt.tenantID, tt.namespace, tt.operation, tt.duration)

			// Verify metric was recorded
			count := testutil.CollectAndCount(m.VectorOperationDuration)
			assert.Greater(t, count, 0, "VectorOperationDuration should have recorded values")
		})
	}
}

func TestSetVectorStorageBytes(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		tenantID  string
		namespace string
		bytes     float64
	}{
		{
			name:      "small namespace",
			tenantID:  "tenant-1",
			namespace: "embeddings",
			bytes:     1048576, // 1MB
		},
		{
			name:      "medium namespace",
			tenantID:  "tenant-1",
			namespace: "documents",
			bytes:     104857600, // 100MB
		},
		{
			name:      "large namespace",
			tenantID:  "tenant-2",
			namespace: "vectors",
			bytes:     1073741824, // 1GB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetVectorStorageBytes(tt.tenantID, tt.namespace, tt.bytes)

			labels := prometheus.Labels{
				"tenant_id": tt.tenantID,
				"namespace": tt.namespace,
			}
			value := testutil.ToFloat64(m.VectorStorageBytes.With(labels))
			assert.Equal(t, tt.bytes, value, "VectorStorageBytes should match")
		})
	}
}

func TestSetVectorCount(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		tenantID  string
		namespace string
		count     float64
	}{
		{
			name:      "small namespace",
			tenantID:  "tenant-1",
			namespace: "embeddings",
			count:     1000,
		},
		{
			name:      "medium namespace",
			tenantID:  "tenant-1",
			namespace: "documents",
			count:     100000,
		},
		{
			name:      "large namespace",
			tenantID:  "tenant-2",
			namespace: "vectors",
			count:     10000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetVectorCount(tt.tenantID, tt.namespace, tt.count)

			labels := prometheus.Labels{
				"tenant_id": tt.tenantID,
				"namespace": tt.namespace,
			}
			value := testutil.ToFloat64(m.VectorCount.With(labels))
			assert.Equal(t, tt.count, value, "VectorCount should match")
		})
	}
}

func TestRecordVectorSearchLatency(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name       string
		tenantID   string
		namespace  string
		searchType string
		latency    float64
	}{
		{
			name:       "similarity search",
			tenantID:   "tenant-1",
			namespace:  "embeddings",
			searchType: "similarity",
			latency:    0.005,
		},
		{
			name:       "hybrid search",
			tenantID:   "tenant-1",
			namespace:  "documents",
			searchType: "hybrid",
			latency:    0.025,
		},
		{
			name:       "metadata search",
			tenantID:   "tenant-2",
			namespace:  "vectors",
			searchType: "metadata",
			latency:    0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordVectorSearchLatency(tt.tenantID, tt.namespace, tt.searchType, tt.latency)

			// Verify metric was recorded
			count := testutil.CollectAndCount(m.VectorSearchLatency)
			assert.Greater(t, count, 0, "VectorSearchLatency should have recorded values")
		})
	}
}

func TestRecordVectorBatchSize(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		tenantID  string
		namespace string
		operation string
		size      float64
	}{
		{
			name:      "small batch upsert",
			tenantID:  "tenant-1",
			namespace: "embeddings",
			operation: "upsert",
			size:      10,
		},
		{
			name:      "medium batch upsert",
			tenantID:  "tenant-1",
			namespace: "documents",
			operation: "upsert",
			size:      100,
		},
		{
			name:      "large batch delete",
			tenantID:  "tenant-2",
			namespace: "vectors",
			operation: "delete",
			size:      1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.RecordVectorBatchSize(tt.tenantID, tt.namespace, tt.operation, tt.size)

			// Verify metric was recorded
			count := testutil.CollectAndCount(m.VectorBatchSize)
			assert.Greater(t, count, 0, "VectorBatchSize should have recorded values")
		})
	}
}

func TestSetVectorQuotaUsage(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name      string
		tenantID  string
		namespace string
		quotaType string
		ratio     float64
	}{
		{
			name:      "low vector usage",
			tenantID:  "tenant-1",
			namespace: "embeddings",
			quotaType: "vectors",
			ratio:     0.1,
		},
		{
			name:      "medium storage usage",
			tenantID:  "tenant-1",
			namespace: "documents",
			quotaType: "storage",
			ratio:     0.5,
		},
		{
			name:      "high vector usage",
			tenantID:  "tenant-2",
			namespace: "vectors",
			quotaType: "vectors",
			ratio:     0.9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetVectorQuotaUsage(tt.tenantID, tt.namespace, tt.quotaType, tt.ratio)

			labels := prometheus.Labels{
				"tenant_id":  tt.tenantID,
				"namespace":  tt.namespace,
				"quota_type": tt.quotaType,
			}
			value := testutil.ToFloat64(m.VectorQuotaUsage.With(labels))
			assert.Equal(t, tt.ratio, value, "VectorQuotaUsage should match")
		})
	}
}

func TestSetVectorNamespaceCount(t *testing.T) {
	Reset()
	m := InitMetrics()

	tests := []struct {
		name     string
		tenantID string
		count    float64
	}{
		{
			name:     "single namespace",
			tenantID: "tenant-1",
			count:    1,
		},
		{
			name:     "multiple namespaces",
			tenantID: "tenant-2",
			count:    10,
		},
		{
			name:     "many namespaces",
			tenantID: "tenant-3",
			count:    100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetVectorNamespaceCount(tt.tenantID, tt.count)

			labels := prometheus.Labels{"tenant_id": tt.tenantID}
			value := testutil.ToFloat64(m.VectorNamespaceCount.With(labels))
			assert.Equal(t, tt.count, value, "VectorNamespaceCount should match")
		})
	}
}

func TestVectorMetricsMultiTenant(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Record metrics for multiple tenants
	tenants := []string{"tenant-1", "tenant-2", "tenant-3"}
	namespaces := []string{"embeddings", "documents"}

	for _, tenant := range tenants {
		for _, ns := range namespaces {
			m.RecordVectorOperation(tenant, ns, "upsert", "success")
			m.SetVectorStorageBytes(tenant, ns, 1048576)
			m.SetVectorCount(tenant, ns, 1000)
		}
		m.SetVectorNamespaceCount(tenant, float64(len(namespaces)))
	}

	// Verify each tenant's metrics are isolated
	for _, tenant := range tenants {
		nsCountLabels := prometheus.Labels{"tenant_id": tenant}
		nsCount := testutil.ToFloat64(m.VectorNamespaceCount.With(nsCountLabels))
		assert.Equal(t, float64(len(namespaces)), nsCount,
			"Tenant %s should have correct namespace count", tenant)

		for _, ns := range namespaces {
			labels := prometheus.Labels{"tenant_id": tenant, "namespace": ns}
			count := testutil.ToFloat64(m.VectorCount.With(labels))
			assert.Equal(t, 1000.0, count,
				"Tenant %s namespace %s should have correct vector count", tenant, ns)
		}
	}
}

func TestVectorMetricsRegistration(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Record some vector metrics
	m.RecordVectorOperation("tenant-1", "embeddings", "upsert", "success")
	m.RecordVectorOperationDuration("tenant-1", "embeddings", "upsert", 0.01)
	m.SetVectorStorageBytes("tenant-1", "embeddings", 1048576)
	m.SetVectorCount("tenant-1", "embeddings", 1000)
	m.RecordVectorSearchLatency("tenant-1", "embeddings", "similarity", 0.005)
	m.RecordVectorBatchSize("tenant-1", "embeddings", "upsert", 100)
	m.SetVectorQuotaUsage("tenant-1", "embeddings", "vectors", 0.5)
	m.SetVectorNamespaceCount("tenant-1", 5)

	// Gather metrics from registry
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err, "Should be able to gather metrics")

	// Expected vector metrics
	expectedVectorMetrics := []string{
		"quicbaas_vector_operations_total",
		"quicbaas_vector_operation_duration_seconds",
		"quicbaas_vector_storage_bytes",
		"quicbaas_vector_count",
		"quicbaas_vector_search_latency_seconds",
		"quicbaas_vector_batch_size",
		"quicbaas_vector_quota_usage_ratio",
		"quicbaas_vector_namespace_count",
	}

	// Build map of registered metric names
	registeredMetrics := make(map[string]bool)
	for _, mf := range metricFamilies {
		registeredMetrics[mf.GetName()] = true
	}

	// Verify all expected vector metrics are registered
	for _, expected := range expectedVectorMetrics {
		assert.True(t, registeredMetrics[expected],
			"Vector metric %s should be registered", expected)
	}
}

func BenchmarkRecordVectorOperation(b *testing.B) {
	Reset()
	m := InitMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordVectorOperation("tenant-1", "embeddings", "upsert", "success")
	}
}

func BenchmarkRecordVectorSearchLatency(b *testing.B) {
	Reset()
	m := InitMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordVectorSearchLatency("tenant-1", "embeddings", "similarity", 0.005)
	}
}

func BenchmarkVectorMetricsMultiTenant(b *testing.B) {
	Reset()
	m := InitMetrics()
	tenants := []string{"tenant-1", "tenant-2", "tenant-3", "tenant-4", "tenant-5"}
	namespaces := []string{"embeddings", "documents"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tenant := tenants[i%len(tenants)]
		ns := namespaces[i%len(namespaces)]
		m.RecordVectorOperation(tenant, ns, "upsert", "success")
		m.RecordVectorOperationDuration(tenant, ns, "upsert", 0.01)
	}
}

// =============================================================================
// Consolidated from metrics_coverage_test.go
// =============================================================================

// TestConcurrency_MultipleGoroutinesRecordingMetrics tests that multiple goroutines
// can safely record metrics simultaneously without data races or corruption.
func TestConcurrency_MultipleGoroutinesRecordingMetrics(t *testing.T) {
	Reset()
	m := InitMetrics()

	const (
		numGoroutines = 100
		iterations    = 1000
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Track operations for verification
	var (
		requestDurationOps   int64
		requestSizeOps       int64
		concurrentIncOps     int64
		concurrentDecOps     int64
		transactionDurOps    int64
		transactionSizeOps   int64
		transactionConflicts int64
		raftProposalOps      int64
		raftSnapshotOps      int64
	)

	// Launch multiple goroutines recording different metrics
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				// Test request metrics
				m.RecordRequestDuration("get", "success", 0.001)
				atomic.AddInt64(&requestDurationOps, 1)

				m.RecordRequestSize("put", 1024)
				atomic.AddInt64(&requestSizeOps, 1)

				// Test concurrent requests gauge
				m.IncrementConcurrentRequests()
				atomic.AddInt64(&concurrentIncOps, 1)
				m.DecrementConcurrentRequests()
				atomic.AddInt64(&concurrentDecOps, 1)

				// Test transaction metrics
				m.RecordTransactionDuration(0.05)
				atomic.AddInt64(&transactionDurOps, 1)

				m.RecordTransactionSize(10)
				atomic.AddInt64(&transactionSizeOps, 1)

				m.IncrementTransactionConflicts()
				atomic.AddInt64(&transactionConflicts, 1)

				// Test Raft metrics with varying shards
				shard := fmt.Sprintf("%d", id%10)
				m.RecordRaftProposalLatency(shard, 0.002)
				atomic.AddInt64(&raftProposalOps, 1)

				m.SetRaftSnapshotSize(shard, float64(id*1024))
				atomic.AddInt64(&raftSnapshotOps, 1)
			}
		}(i)
	}

	wg.Wait()

	// Verify all operations completed
	assert.Equal(t, int64(numGoroutines*iterations), requestDurationOps)
	assert.Equal(t, int64(numGoroutines*iterations), requestSizeOps)
	assert.Equal(t, int64(numGoroutines*iterations), concurrentIncOps)
	assert.Equal(t, int64(numGoroutines*iterations), concurrentDecOps)

	// Verify concurrent requests gauge is back to 0 (all inc/dec balanced)
	concurrentValue := testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 0.0, concurrentValue, "Concurrent requests should be balanced")

	// Verify transaction conflicts counter
	conflictsValue := testutil.ToFloat64(m.TransactionConflicts)
	assert.Equal(t, float64(numGoroutines*iterations), conflictsValue)

	// Verify metrics were collected without corruption
	count := testutil.CollectAndCount(m.RequestDuration)
	assert.Greater(t, count, 0, "Request duration metrics should be collected")
}

// TestConcurrency_RaceConditionDetection runs with -race flag to detect data races.
func TestConcurrency_RaceConditionDetection(t *testing.T) {
	Reset()
	m := InitMetrics()

	const numWorkers = 50

	var wg sync.WaitGroup
	wg.Add(numWorkers * 3) // 3 different types of workers

	// Worker 1: Record request metrics
	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.RecordRequestDuration(fmt.Sprintf("op_%d", id%5), "success", float64(j)*0.001)
			}
		}(i)
	}

	// Worker 2: Manipulate concurrent requests gauge
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.IncrementConcurrentRequests()
				time.Sleep(time.Microsecond)
				m.DecrementConcurrentRequests()
			}
		}()
	}

	// Worker 3: Record Raft metrics with different shards
	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			defer wg.Done()
			shard := fmt.Sprintf("shard_%d", id%10)
			for j := 0; j < 100; j++ {
				m.RecordRaftProposalLatency(shard, 0.005)
				m.SetRaftSnapshotSize(shard, float64(j*1024))
			}
		}(i)
	}

	wg.Wait()
	// If we reach here without race detector complaints, test passes
}

// TestEdgeCase_NegativeValues tests recording negative values where they make sense.
func TestEdgeCase_NegativeValues(t *testing.T) {
	Reset()
	m := InitMetrics()

	t.Run("negative duration values", func(t *testing.T) {
		// Negative durations don't make sense but Prometheus allows them
		// The system should handle them without panicking
		m.RecordRequestDuration("test", "success", -0.5)
		m.RecordTransactionDuration(-1.0)
		m.RecordRaftProposalLatency("1", -0.1)

		// Should not panic and metrics should be recorded
		count := testutil.CollectAndCount(m.RequestDuration)
		assert.Greater(t, count, 0)
	})

	t.Run("negative size values", func(t *testing.T) {
		// Negative sizes don't make sense but should be handled
		m.RecordRequestSize("test", -1024)
		m.RecordTransactionSize(-10)
		m.SetRaftSnapshotSize("1", -2048)

		// Should not panic
		count := testutil.CollectAndCount(m.RequestSize)
		assert.Greater(t, count, 0)
	})

	t.Run("gauge can go negative", func(t *testing.T) {
		// Gauges can legitimately go negative
		m.DecrementConcurrentRequests()
		m.DecrementConcurrentRequests()

		value := testutil.ToFloat64(m.ConcurrentRequests)
		assert.Equal(t, -2.0, value, "Gauge should handle negative values")
	})
}

// TestEdgeCase_ZeroValues tests recording zero values.
func TestEdgeCase_ZeroValues(t *testing.T) {
	Reset()
	m := InitMetrics()

	t.Run("zero durations", func(t *testing.T) {
		m.RecordRequestDuration("test", "success", 0)
		m.RecordTransactionDuration(0)
		m.RecordRaftProposalLatency("1", 0)

		count := testutil.CollectAndCount(m.RequestDuration)
		assert.Greater(t, count, 0)
	})

	t.Run("zero sizes", func(t *testing.T) {
		m.RecordRequestSize("test", 0)
		m.RecordTransactionSize(0)
		m.SetRaftSnapshotSize("1", 0)

		count := testutil.CollectAndCount(m.RequestSize)
		assert.Greater(t, count, 0)
	})
}

// TestEdgeCase_ExtremeValues tests recording extremely large values.
func TestEdgeCase_ExtremeValues(t *testing.T) {
	Reset()
	m := InitMetrics()

	t.Run("very large durations", func(t *testing.T) {
		m.RecordRequestDuration("test", "success", 1000000.0) // 1M seconds
		m.RecordTransactionDuration(999999.999)
		m.RecordRaftProposalLatency("1", 86400.0) // 1 day in seconds

		count := testutil.CollectAndCount(m.RequestDuration)
		assert.Greater(t, count, 0)
	})

	t.Run("very large sizes", func(t *testing.T) {
		m.RecordRequestSize("test", 1e15) // Petabyte scale
		m.RecordTransactionSize(1e9)      // Billion operations
		m.SetRaftSnapshotSize("1", 1e12)  // Terabyte

		count := testutil.CollectAndCount(m.RequestSize)
		assert.Greater(t, count, 0)
	})

	t.Run("max float64 values", func(t *testing.T) {
		m.RecordRequestDuration("test", "success", math.MaxFloat64)
		m.RecordRequestSize("test", math.MaxFloat64)

		// Should handle without overflow
		count := testutil.CollectAndCount(m.RequestDuration)
		assert.Greater(t, count, 0)
	})
}

// TestEdgeCase_EmptyLabels tests metrics with empty label values.
func TestEdgeCase_EmptyLabels(t *testing.T) {
	Reset()
	m := InitMetrics()

	t.Run("empty operation and status labels", func(t *testing.T) {
		m.RecordRequestDuration("", "", 0.01)
		m.RecordRequestSize("", 1024)

		count := testutil.CollectAndCount(m.RequestDuration)
		assert.Greater(t, count, 0)
	})

	t.Run("empty shard label", func(t *testing.T) {
		m.RecordRaftProposalLatency("", 0.005)
		m.SetRaftSnapshotSize("", 2048)

		count := testutil.CollectAndCount(m.RaftProposalLatency)
		assert.Greater(t, count, 0)
	})
}

// TestEdgeCase_SpecialCharactersInLabels tests labels with special characters.
func TestEdgeCase_SpecialCharactersInLabels(t *testing.T) {
	Reset()
	m := InitMetrics()

	specialLabels := []string{
		"test-with-dashes",
		"test_with_underscores",
		"test.with.dots",
		"test/with/slashes",
		"test:with:colons",
		"test with spaces",
		"测试中文",
		"test\nwith\nnewlines",
	}

	for _, label := range specialLabels {
		t.Run(fmt.Sprintf("label_%s", label), func(t *testing.T) {
			m.RecordRequestDuration(label, "success", 0.01)
			m.RecordRequestSize(label, 1024)
			m.RecordRaftProposalLatency(label, 0.005)
			m.SetRaftSnapshotSize(label, 2048)

			// Should not panic
			count := testutil.CollectAndCount(m.RequestDuration)
			assert.Greater(t, count, 0)
		})
	}
}

// TestEdgeCase_LargeLabelCardinality tests many unique label values.
func TestEdgeCase_LargeLabelCardinality(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Create high cardinality by using many unique label values
	const numUniqueLabels = 1000

	for i := 0; i < numUniqueLabels; i++ {
		operation := fmt.Sprintf("operation_%d", i)
		status := fmt.Sprintf("status_%d", i%10)
		shard := fmt.Sprintf("shard_%d", i)

		m.RecordRequestDuration(operation, status, 0.01)
		m.RecordRequestSize(operation, 1024)
		m.RecordRaftProposalLatency(shard, 0.005)
		m.SetRaftSnapshotSize(shard, float64(i*1024))
	}

	// Verify metrics were recorded
	count := testutil.CollectAndCount(m.RequestDuration)
	assert.Greater(t, count, 0)

	// Verify we can still gather metrics without issues
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err)
	assert.NotEmpty(t, metricFamilies)
}

// TestCollector_EmptyRegistry tests collecting from empty registry.
func TestCollector_EmptyRegistry(t *testing.T) {
	// Create a new empty registry
	emptyRegistry := prometheus.NewRegistry()

	metricFamilies, err := emptyRegistry.Gather()
	require.NoError(t, err, "Should be able to gather from empty registry")
	assert.Empty(t, metricFamilies, "Empty registry should return no metrics")
}

// TestCollector_AfterReset tests collecting metrics after reset.
func TestCollector_AfterReset(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Record some metrics
	m.RecordRequestDuration("get", "success", 0.01)
	m.IncrementConcurrentRequests()
	m.IncrementTransactionConflicts()

	// Verify metrics exist
	beforeValue := testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 1.0, beforeValue)

	// Reset
	Reset()

	// Get new metrics instance
	m2 := GetMetrics()

	// Old instance should still have values (they're just disconnected)
	// New instance should have fresh values
	afterValue := testutil.ToFloat64(m2.ConcurrentRequests)
	assert.Equal(t, 0.0, afterValue, "New instance should have reset values")
}

// TestCollector_NoMetricsRegistered tests gathering when no QuicBaaS metrics registered.
func TestCollector_NoMetricsRegistered(t *testing.T) {
	Reset()

	// Don't initialize QuicBaaS metrics, just gather what's in the registry
	// (should only have Go runtime and process collectors from init())
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err)

	// Should have at least the default collectors
	assert.NotEmpty(t, metricFamilies, "Should have default collectors")

	// Verify no QuicBaaS metrics
	for _, mf := range metricFamilies {
		name := mf.GetName()
		assert.NotContains(t, name, "quicbaas_", "Should not have QuicBaaS metrics yet")
	}
}

// TestReset_VerifyMetricsCleared tests that reset truly clears all metrics.
func TestReset_VerifyMetricsCleared(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Record various metrics with significant values
	for i := 0; i < 100; i++ {
		m.RecordRequestDuration("get", "success", 0.01)
		m.RecordRequestSize("put", 1024)
		m.IncrementConcurrentRequests()
		m.RecordTransactionDuration(0.1)
		m.RecordTransactionSize(10)
		m.IncrementTransactionConflicts()
		m.RecordRaftProposalLatency("1", 0.005)
		m.SetRaftSnapshotSize("1", 2048)
	}

	// Verify metrics have values
	beforeConflicts := testutil.ToFloat64(m.TransactionConflicts)
	assert.Equal(t, 100.0, beforeConflicts)

	beforeConcurrent := testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 100.0, beforeConcurrent)

	// Reset
	Reset()
	m2 := GetMetrics()

	// All metrics should be at zero/empty
	afterConflicts := testutil.ToFloat64(m2.TransactionConflicts)
	assert.Equal(t, 0.0, afterConflicts, "Counter should reset to 0")

	afterConcurrent := testutil.ToFloat64(m2.ConcurrentRequests)
	assert.Equal(t, 0.0, afterConcurrent, "Gauge should reset to 0")

	// Histograms should have no observations
	count := testutil.CollectAndCount(m2.RequestDuration)
	// Count includes the descriptor, so it's > 0, but no observations yet
	assert.Greater(t, count, -1, "Histogram should be initialized")
}

// TestReset_HistogramsCleared tests that histograms clear observations after reset.
func TestReset_HistogramsCleared(t *testing.T) {
	Reset()
	m1 := InitMetrics()

	// Record many observations
	for i := 0; i < 1000; i++ {
		m1.RecordRequestDuration("get", "success", float64(i)*0.001)
		m1.RecordRequestSize("put", float64(i*100))
		m1.RecordTransactionDuration(float64(i) * 0.01)
		m1.RecordTransactionSize(float64(i))
		m1.RecordRaftProposalLatency("1", float64(i)*0.002)
	}

	// Reset
	Reset()
	m2 := GetMetrics()

	// New histograms should have no observations
	// Record one new observation
	m2.RecordRequestDuration("get", "success", 0.5)

	// Gather and check that we don't have 1000+ observations
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err)

	for _, mf := range metricFamilies {
		if mf.GetName() == "quicbaas_request_duration_seconds" {
			for _, metric := range mf.GetMetric() {
				if metric.Histogram != nil {
					// Should have only 1 observation, not 1000+
					count := metric.Histogram.GetSampleCount()
					assert.Equal(t, uint64(1), count, "Histogram should have only new observations")
				}
			}
		}
	}
}

// TestReset_MultipleResets tests calling Reset multiple times.
func TestReset_MultipleResets(t *testing.T) {
	for i := 0; i < 5; i++ {
		Reset()
		m := InitMetrics()
		require.NotNil(t, m, "Should initialize metrics after reset %d", i)

		// Record some values
		m.IncrementConcurrentRequests()
		value := testutil.ToFloat64(m.ConcurrentRequests)
		assert.Equal(t, 1.0, value)
	}
}

// TestConcurrency_InitMetricsMultipleTimes tests concurrent initialization.
func TestConcurrency_InitMetricsMultipleTimes(t *testing.T) {
	Reset()

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	instances := make([]*Metrics, numGoroutines)

	// Try to initialize metrics concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			instances[idx] = InitMetrics()
		}(i)
	}

	wg.Wait()

	// All instances should be the same (singleton)
	for i := 1; i < numGoroutines; i++ {
		assert.Equal(t, instances[0], instances[i],
			"All instances should be the same singleton")
	}
}

// TestConcurrency_GetMetricsMultipleTimes tests concurrent GetMetrics calls.
func TestConcurrency_GetMetricsMultipleTimes(t *testing.T) {
	Reset()

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	instances := make([]*Metrics, numGoroutines)

	// Get metrics concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			instances[idx] = GetMetrics()
		}(i)
	}

	wg.Wait()

	// All should be the same instance
	for i := 1; i < numGoroutines; i++ {
		assert.Equal(t, instances[0], instances[i])
	}
}

// TestHistogramBucketDistribution verifies observations fall into correct buckets.
func TestHistogramBucketDistribution(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Record values in different bucket ranges
	m.RecordRequestDuration("get", "success", 0.0005) // Below first bucket
	m.RecordRequestDuration("get", "success", 0.001)  // First bucket
	m.RecordRequestDuration("get", "success", 0.005)
	m.RecordRequestDuration("get", "success", 0.01)
	m.RecordRequestDuration("get", "success", 0.1)
	m.RecordRequestDuration("get", "success", 1.0)
	m.RecordRequestDuration("get", "success", 10.0)
	m.RecordRequestDuration("get", "success", 100.0) // Above last bucket

	// Gather and verify histogram has observations
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "quicbaas_request_duration_seconds" {
			found = true
			for _, metric := range mf.GetMetric() {
				if metric.Histogram != nil {
					assert.Equal(t, uint64(8), metric.Histogram.GetSampleCount())
				}
			}
		}
	}
	assert.True(t, found, "Should find request duration histogram")
}

// TestGaugeSetToSpecificValue tests setting gauge to specific values.
func TestGaugeSetToSpecificValue(t *testing.T) {
	Reset()
	m := InitMetrics()

	testValues := []float64{0, 1, 100, 999, 1000000, -50, -999.999}

	for _, val := range testValues {
		t.Run(fmt.Sprintf("set_to_%.2f", val), func(t *testing.T) {
			m.SetRaftSnapshotSize("test", val)

			labels := prometheus.Labels{"shard": "test"}
			actualValue := testutil.ToFloat64(m.RaftSnapshotSize.With(labels))
			assert.Equal(t, val, actualValue)
		})
	}
}

// TestCounterMonotonicity verifies counters only increase.
func TestCounterMonotonicity(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Counters should only increase
	for i := 0; i < 100; i++ {
		m.IncrementTransactionConflicts()
	}

	value := testutil.ToFloat64(m.TransactionConflicts)
	assert.Equal(t, 100.0, value)

	// Continue incrementing
	for i := 0; i < 50; i++ {
		m.IncrementTransactionConflicts()
	}

	value = testutil.ToFloat64(m.TransactionConflicts)
	assert.Equal(t, 150.0, value, "Counter should be monotonically increasing")
}

// TestMetricsTypes verifies correct metric types are registered.
func TestMetricsTypes(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Record at least one value for each metric to ensure they appear
	m.RecordRequestDuration("test", "success", 0.01)
	m.RecordRequestSize("test", 1024)
	m.IncrementConcurrentRequests()
	m.RecordTransactionDuration(0.1)
	m.RecordTransactionSize(10)
	m.IncrementTransactionConflicts()
	m.RecordRaftProposalLatency("1", 0.005)
	m.SetRaftSnapshotSize("1", 2048)

	metricFamilies, err := Registry.Gather()
	require.NoError(t, err)

	expectedTypes := map[string]string{
		"quicbaas_request_duration_seconds":      "histogram",
		"quicbaas_request_size_bytes":            "histogram",
		"quicbaas_concurrent_requests":           "gauge",
		"quicbaas_transaction_duration_seconds":  "histogram",
		"quicbaas_transaction_operations":        "histogram",
		"quicbaas_transaction_conflicts_total":   "counter",
		"quicbaas_raft_proposal_latency_seconds": "histogram",
		"quicbaas_raft_snapshot_size_bytes":      "gauge",
	}

	for _, mf := range metricFamilies {
		name := mf.GetName()
		if expectedType, ok := expectedTypes[name]; ok {
			actualType := mf.GetType().String()
			assert.Contains(t, strings.ToLower(actualType), expectedType,
				"Metric %s should be of type %s", name, expectedType)
		}
	}
}

// TestGetMetrics_BeforeInit tests GetMetrics before InitMetrics is called.
func TestGetMetrics_BeforeInit(t *testing.T) {
	Reset()

	// Call GetMetrics without calling InitMetrics first
	m := GetMetrics()
	require.NotNil(t, m, "GetMetrics should initialize if not already done")

	// Verify it's properly initialized
	assert.NotNil(t, m.RequestDuration)
	assert.NotNil(t, m.ConcurrentRequests)
	assert.NotNil(t, m.TransactionConflicts)

	// Should be able to use it
	m.IncrementConcurrentRequests()
	value := testutil.ToFloat64(m.ConcurrentRequests)
	assert.Equal(t, 1.0, value)
}

// TestConcurrency_MixedOperations tests concurrent mixed operations.
func TestConcurrency_MixedOperations(t *testing.T) {
	Reset()
	m := InitMetrics()

	const numWorkers = 50
	var wg sync.WaitGroup
	wg.Add(numWorkers * 4)

	// Different operation patterns
	operations := []string{"get", "put", "delete", "scan", "batch"}
	statuses := []string{"success", "failure", "timeout", "error"}

	// Worker set 1: Request metrics
	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				op := operations[j%len(operations)]
				status := statuses[j%len(statuses)]
				m.RecordRequestDuration(op, status, float64(j)*0.001)
			}
		}(i)
	}

	// Worker set 2: Transaction metrics
	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.RecordTransactionDuration(float64(j) * 0.01)
				m.RecordTransactionSize(float64(j))
				if j%10 == 0 {
					m.IncrementTransactionConflicts()
				}
			}
		}(i)
	}

	// Worker set 3: Concurrent requests
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.IncrementConcurrentRequests()
				if j%2 == 0 {
					m.DecrementConcurrentRequests()
				}
			}
		}()
	}

	// Worker set 4: Raft metrics
	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			defer wg.Done()
			shard := fmt.Sprintf("%d", id%20)
			for j := 0; j < 100; j++ {
				m.RecordRaftProposalLatency(shard, float64(j)*0.001)
				m.SetRaftSnapshotSize(shard, float64(j*1024))
			}
		}(i)
	}

	wg.Wait()

	// Verify we can still gather metrics without errors
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err)
	assert.NotEmpty(t, metricFamilies)
}
