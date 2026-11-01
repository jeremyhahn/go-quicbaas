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
	"testing"

	"math"
	"sync"

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
	tests := []struct {
		name string
		get  func() *Metrics
	}{
		{name: "InitMetrics", get: InitMetrics},
		{name: "GetMetrics before InitMetrics", get: GetMetrics},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Reset()

			const numGoroutines = 100
			instances := make([]*Metrics, numGoroutines)
			var wg sync.WaitGroup
			wg.Add(numGoroutines)
			for i := 0; i < numGoroutines; i++ {
				go func(idx int) {
					defer wg.Done()
					instances[idx] = tt.get()
				}(i)
			}
			wg.Wait()

			require.NotNil(t, instances[0])
			assert.NotNil(t, instances[0].RequestDuration)
			for i := 1; i < numGoroutines; i++ {
				assert.Same(t, instances[0], instances[i], "every caller should get the singleton")
			}
			assert.Same(t, instances[0], InitMetrics())
			assert.Same(t, instances[0], GetMetrics())
		})
	}
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

func TestMetricsRegistration(t *testing.T) {
	Reset()
	m := InitMetrics()

	// Histograms and vectors appear in the registry only once they hold data.
	m.RecordRequestDuration("test", "success", 0.01)
	m.RecordRequestSize("test", 1000)
	m.IncrementConcurrentRequests()
	m.RecordTransactionDuration(0.1)
	m.RecordTransactionSize(10)
	m.IncrementTransactionConflicts()
	m.RecordRaftProposalLatency("1", 0.005)
	m.SetRaftSnapshotSize("1", 1024)
	m.RecordVectorOperation("tenant-1", "embeddings", "upsert", "success")
	m.RecordVectorOperationDuration("tenant-1", "embeddings", "upsert", 0.01)
	m.SetVectorStorageBytes("tenant-1", "embeddings", 1048576)
	m.SetVectorCount("tenant-1", "embeddings", 1000)
	m.RecordVectorSearchLatency("tenant-1", "embeddings", "similarity", 0.005)
	m.RecordVectorBatchSize("tenant-1", "embeddings", "upsert", 100)
	m.SetVectorQuotaUsage("tenant-1", "embeddings", "vectors", 0.5)
	m.SetVectorNamespaceCount("tenant-1", 5)

	metricFamilies, err := Registry.Gather()
	require.NoError(t, err, "Should be able to gather metrics")

	help := make(map[string]string)
	for _, mf := range metricFamilies {
		help[mf.GetName()] = mf.GetHelp()
	}

	// An empty want leaves the help text unchecked.
	tests := []struct {
		name string
		want string
	}{
		{name: "quicbaas_request_duration_seconds", want: "Request latency by operation and status"},
		{name: "quicbaas_request_size_bytes", want: "Request payload size in bytes"},
		{name: "quicbaas_concurrent_requests", want: "Number of requests currently being processed"},
		{name: "quicbaas_transaction_duration_seconds", want: "Transaction execution time"},
		{name: "quicbaas_transaction_operations", want: "Number of operations per transaction"},
		{name: "quicbaas_transaction_conflicts_total", want: "Number of transaction conflicts (optimistic locking failures)"},
		{name: "quicbaas_raft_proposal_latency_seconds", want: "Time from proposal to commit by shard"},
		{name: "quicbaas_raft_snapshot_size_bytes", want: "Current snapshot size by shard"},
		{name: "quicbaas_vector_operations_total"},
		{name: "quicbaas_vector_operation_duration_seconds"},
		{name: "quicbaas_vector_storage_bytes"},
		{name: "quicbaas_vector_count"},
		{name: "quicbaas_vector_search_latency_seconds"},
		{name: "quicbaas_vector_batch_size"},
		{name: "quicbaas_vector_quota_usage_ratio"},
		{name: "quicbaas_vector_namespace_count"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := help[tt.name]
			require.True(t, ok, "Metric %s should be registered", tt.name)
			if tt.want != "" {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestReset(t *testing.T) {
	tests := []struct {
		name   string
		record func(m *Metrics)
		count  func(m *Metrics) float64
	}{
		{
			name:   "counter",
			record: func(m *Metrics) { m.IncrementTransactionConflicts() },
			count:  func(m *Metrics) float64 { return testutil.ToFloat64(m.TransactionConflicts) },
		},
		{
			name:   "gauge",
			record: func(m *Metrics) { m.IncrementConcurrentRequests() },
			count:  func(m *Metrics) float64 { return testutil.ToFloat64(m.ConcurrentRequests) },
		},
		{
			name:   "histogram",
			record: func(m *Metrics) { m.RecordRequestDuration("get", "success", 0.01) },
			count:  func(m *Metrics) float64 { return histogramSampleCount(t, "quicbaas_request_duration_seconds") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Repeated resets must each yield a fresh, working instance.
			for round := 0; round < 3; round++ {
				Reset()
				m1 := InitMetrics()
				require.NotNil(t, m1)
				for i := 0; i < 100; i++ {
					tt.record(m1)
				}
				assert.Equal(t, 100.0, tt.count(m1), "round %d: values before reset", round)

				Reset()
				m2 := GetMetrics()
				require.NotNil(t, m2)
				assert.NotSame(t, m1, m2, "Reset should create a new instance")

				tt.record(m2)
				assert.Equal(t, 1.0, tt.count(m2), "round %d: only post-reset values", round)
			}
		})
	}
}

// histogramSampleCount returns the total number of observations, across all
// label values, of the named histogram family in the current Registry.
func histogramSampleCount(t *testing.T, family string) float64 {
	t.Helper()
	metricFamilies, err := Registry.Gather()
	require.NoError(t, err)

	var count uint64
	for _, mf := range metricFamilies {
		if mf.GetName() != family {
			continue
		}
		for _, metric := range mf.GetMetric() {
			count += metric.GetHistogram().GetSampleCount()
		}
	}
	return float64(count)
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

// TestConcurrency records each workload from many goroutines, alone and all
// at once, and checks that no update was lost.
func TestConcurrency(t *testing.T) {
	const (
		numGoroutines = 50
		iterations    = 200
		total         = numGoroutines * iterations
		numShards     = 10
	)
	operations := []string{"get", "put", "delete", "scan", "batch"}

	type workload struct {
		name   string
		record func(m *Metrics, id, j int)
		check  func(t *testing.T, m *Metrics)
	}
	workloads := []workload{
		{
			name: "request metrics",
			record: func(m *Metrics, id, j int) {
				m.RecordRequestDuration(operations[j%len(operations)], "success", float64(j)*0.001)
				m.RecordRequestSize("put", 1024)
			},
			check: func(t *testing.T, m *Metrics) {
				assert.Equal(t, float64(total), histogramSampleCount(t, "quicbaas_request_duration_seconds"))
				assert.Equal(t, float64(total), histogramSampleCount(t, "quicbaas_request_size_bytes"))
			},
		},
		{
			name: "concurrent requests gauge",
			record: func(m *Metrics, id, j int) {
				m.IncrementConcurrentRequests()
				if j%2 == 0 {
					m.DecrementConcurrentRequests()
				}
			},
			check: func(t *testing.T, m *Metrics) {
				assert.Equal(t, float64(total/2), testutil.ToFloat64(m.ConcurrentRequests))
			},
		},
		{
			name: "transaction metrics",
			record: func(m *Metrics, id, j int) {
				m.RecordTransactionDuration(float64(j) * 0.01)
				m.RecordTransactionSize(float64(j))
				m.IncrementTransactionConflicts()
			},
			check: func(t *testing.T, m *Metrics) {
				assert.Equal(t, float64(total), histogramSampleCount(t, "quicbaas_transaction_duration_seconds"))
				assert.Equal(t, float64(total), histogramSampleCount(t, "quicbaas_transaction_operations"))
				assert.Equal(t, float64(total), testutil.ToFloat64(m.TransactionConflicts))
			},
		},
		{
			name: "raft metrics",
			record: func(m *Metrics, id, j int) {
				shard := fmt.Sprintf("%d", id%numShards)
				m.RecordRaftProposalLatency(shard, float64(j)*0.001)
				m.SetRaftSnapshotSize(shard, float64(j*1024))
			},
			check: func(t *testing.T, m *Metrics) {
				assert.Equal(t, float64(total), histogramSampleCount(t, "quicbaas_raft_proposal_latency_seconds"))
				assert.Equal(t, numShards, testutil.CollectAndCount(m.RaftSnapshotSize))
			},
		},
	}

	// run starts numGoroutines workers for each workload at once and waits
	// for all of them; the race detector checks the shared metrics.
	run := func(m *Metrics, selected []workload) {
		var wg sync.WaitGroup
		for _, w := range selected {
			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(w workload, id int) {
					defer wg.Done()
					for j := 0; j < iterations; j++ {
						w.record(m, id, j)
					}
				}(w, i)
			}
		}
		wg.Wait()
	}

	for _, w := range workloads {
		t.Run(w.name, func(t *testing.T) {
			Reset()
			m := InitMetrics()
			run(m, []workload{w})
			w.check(t, m)
		})
	}

	t.Run("mixed workloads", func(t *testing.T) {
		Reset()
		m := InitMetrics()
		run(m, workloads)
		for _, w := range workloads {
			w.check(t, m)
		}
	})
}

// TestEdgeCases records boundary values and unusual label values on every
// recording method.
func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		label string
		value float64
	}{
		{name: "negative value", label: "test", value: -1024},
		{name: "zero value", label: "test", value: 0},
		{name: "large value", label: "test", value: 1e15},
		{name: "max float64", label: "test", value: math.MaxFloat64},
		{name: "empty label", label: "", value: 0.01},
		{name: "dashes in label", label: "test-with-dashes", value: 0.01},
		{name: "dots in label", label: "test.with.dots", value: 0.01},
		{name: "slashes in label", label: "test/with/slashes", value: 0.01},
		{name: "colons in label", label: "test:with:colons", value: 0.01},
		{name: "spaces in label", label: "test with spaces", value: 0.01},
		{name: "unicode label", label: "测试中文", value: 0.01},
		{name: "newlines in label", label: "test\nwith\nnewlines", value: 0.01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Reset()
			m := InitMetrics()

			m.RecordRequestDuration(tt.label, "success", tt.value)
			m.RecordRequestSize(tt.label, tt.value)
			m.RecordTransactionDuration(tt.value)
			m.RecordTransactionSize(tt.value)
			m.RecordRaftProposalLatency(tt.label, tt.value)
			m.SetRaftSnapshotSize(tt.label, tt.value)

			assert.Equal(t, 1, testutil.CollectAndCount(m.RequestDuration))
			assert.Equal(t, 1, testutil.CollectAndCount(m.RequestSize))
			assert.Equal(t, 1, testutil.CollectAndCount(m.RaftProposalLatency))
			assert.Equal(t, tt.value, testutil.ToFloat64(m.RaftSnapshotSize.WithLabelValues(tt.label)))
		})
	}

	t.Run("gauge can go negative", func(t *testing.T) {
		Reset()
		m := InitMetrics()
		m.DecrementConcurrentRequests()
		m.DecrementConcurrentRequests()
		assert.Equal(t, -2.0, testutil.ToFloat64(m.ConcurrentRequests))
	})

	t.Run("large label cardinality", func(t *testing.T) {
		Reset()
		m := InitMetrics()
		const numUniqueLabels = 1000
		for i := 0; i < numUniqueLabels; i++ {
			shard := fmt.Sprintf("shard_%d", i)
			m.RecordRequestDuration(fmt.Sprintf("operation_%d", i), "success", 0.01)
			m.RecordRaftProposalLatency(shard, 0.005)
			m.SetRaftSnapshotSize(shard, float64(i))
		}
		assert.Equal(t, numUniqueLabels, testutil.CollectAndCount(m.RequestDuration))
		assert.Equal(t, numUniqueLabels, testutil.CollectAndCount(m.RaftSnapshotSize))
	})
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
