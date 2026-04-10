//go:build iam

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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

func TestNewVectorMetricsRecorder(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	assert.NotNil(t, recorder, "VectorMetricsRecorder should be created")
	assert.NotNil(t, recorder.metrics, "Metrics should be initialized")
}

func TestGetTenantID(t *testing.T) {
	tests := []struct {
		name       string
		tenantID   string
		wantResult string
	}{
		{
			name:       "with tenant ID",
			tenantID:   "tenant-123",
			wantResult: "tenant-123",
		},
		{
			name:       "without tenant ID",
			tenantID:   "",
			wantResult: defaultTenantID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.tenantID != "" {
				ctx = iam.ContextWithTenant(ctx, tt.tenantID)
			}
			result := getTenantID(ctx)
			assert.Equal(t, tt.wantResult, result)
		})
	}
}

func TestRecordUpsert(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now().Add(-10 * time.Millisecond) // Simulate operation took 10ms

	recorder.RecordUpsert(ctx, "embeddings", startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "embeddings",
		"operation": "upsert",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 upsert operation")
}

func TestRecordUpsert_Failure(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now()

	recorder.RecordUpsert(ctx, "embeddings", startTime, false)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "embeddings",
		"operation": "upsert",
		"status":    "failure",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 failed upsert operation")
}

func TestRecordBatchUpsert(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-2")
	startTime := time.Now().Add(-50 * time.Millisecond)

	recorder.RecordBatchUpsert(ctx, "documents", 100, startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-2",
		"namespace": "documents",
		"operation": "batch_upsert",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 batch upsert operation")

	// Check batch size was recorded
	count := testutil.CollectAndCount(m.VectorBatchSize)
	assert.Greater(t, count, 0, "VectorBatchSize should have recorded values")
}

func TestRecordGet(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now().Add(-5 * time.Millisecond)

	recorder.RecordGet(ctx, "embeddings", startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "embeddings",
		"operation": "get",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 get operation")
}

func TestRecordDelete(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-3")
	startTime := time.Now().Add(-3 * time.Millisecond)

	recorder.RecordDelete(ctx, "vectors", startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-3",
		"namespace": "vectors",
		"operation": "delete",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 delete operation")
}

func TestRecordBatchDelete(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now().Add(-20 * time.Millisecond)

	recorder.RecordBatchDelete(ctx, "embeddings", 50, startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "embeddings",
		"operation": "batch_delete",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 batch delete operation")
}

func TestRecordSearch_Similarity(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now().Add(-15 * time.Millisecond)

	recorder.RecordSearch(ctx, "embeddings", "similarity", startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "embeddings",
		"operation": "similarity",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 similarity search operation")

	// Check search latency was recorded
	count := testutil.CollectAndCount(m.VectorSearchLatency)
	assert.Greater(t, count, 0, "VectorSearchLatency should have recorded values")
}

func TestRecordSearch_Hybrid(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-2")
	startTime := time.Now().Add(-25 * time.Millisecond)

	recorder.RecordSearch(ctx, "documents", "hybrid", startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-2",
		"namespace": "documents",
		"operation": "hybrid",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 hybrid search operation")
}

func TestRecordNamespaceCreate(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now().Add(-100 * time.Millisecond)

	recorder.RecordNamespaceCreate(ctx, "new-namespace", startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "new-namespace",
		"operation": "create_namespace",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 namespace create operation")
}

func TestRecordNamespaceDelete(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now().Add(-50 * time.Millisecond)

	recorder.RecordNamespaceDelete(ctx, "old-namespace", startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "old-namespace",
		"operation": "delete_namespace",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 namespace delete operation")
}

func TestUpdateNamespaceStats(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")

	recorder.UpdateNamespaceStats(ctx, "embeddings", 10000, 1073741824) // 1GB

	// Check vector count
	countLabels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "embeddings",
	}
	vectorCount := testutil.ToFloat64(m.VectorCount.With(countLabels))
	assert.Equal(t, 10000.0, vectorCount, "Should record correct vector count")

	// Check storage bytes
	storageBytes := testutil.ToFloat64(m.VectorStorageBytes.With(countLabels))
	assert.Equal(t, 1073741824.0, storageBytes, "Should record correct storage bytes")
}

func TestUpdateQuotaUsage(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")

	// Update vector quota usage
	recorder.UpdateQuotaUsage(ctx, "embeddings", "vectors", 0.75)

	labels := prometheus.Labels{
		"tenant_id":  "tenant-1",
		"namespace":  "embeddings",
		"quota_type": "vectors",
	}
	value := testutil.ToFloat64(m.VectorQuotaUsage.With(labels))
	assert.Equal(t, 0.75, value, "Should record 75% vector quota usage")

	// Update storage quota usage
	recorder.UpdateQuotaUsage(ctx, "embeddings", "storage", 0.50)

	storageLabels := prometheus.Labels{
		"tenant_id":  "tenant-1",
		"namespace":  "embeddings",
		"quota_type": "storage",
	}
	storageValue := testutil.ToFloat64(m.VectorQuotaUsage.With(storageLabels))
	assert.Equal(t, 0.50, storageValue, "Should record 50% storage quota usage")
}

func TestUpdateTenantNamespaceCount(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")

	recorder.UpdateTenantNamespaceCount(ctx, 5)

	labels := prometheus.Labels{"tenant_id": "tenant-1"}
	value := testutil.ToFloat64(m.VectorNamespaceCount.With(labels))
	assert.Equal(t, 5.0, value, "Should record 5 namespaces for tenant")
}

func TestRecordExport(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now().Add(-500 * time.Millisecond)

	recorder.RecordExport(ctx, "embeddings", 5000, startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "embeddings",
		"operation": "export",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 export operation")
}

func TestRecordImport(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-2")
	startTime := time.Now().Add(-1 * time.Second)

	recorder.RecordImport(ctx, "documents", 10000, startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-2",
		"namespace": "documents",
		"operation": "import",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 import operation")
}

func TestRecordIndexRebuild(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now().Add(-2 * time.Second)

	recorder.RecordIndexRebuild(ctx, "embeddings", startTime, true)

	labels := prometheus.Labels{
		"tenant_id": "tenant-1",
		"namespace": "embeddings",
		"operation": "rebuild_index",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record 1 index rebuild operation")
}

func TestVectorMetrics_DefaultTenant(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	// Context without tenant ID
	ctx := context.Background()
	startTime := time.Now()

	recorder.RecordUpsert(ctx, "embeddings", startTime, true)

	// Should use default tenant ID
	labels := prometheus.Labels{
		"tenant_id": defaultTenantID,
		"namespace": "embeddings",
		"operation": "upsert",
		"status":    "success",
	}
	value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
	assert.Equal(t, 1.0, value, "Should record operation with default tenant")
}

func TestVectorMetrics_MultipleTenantsIsolation(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	m := metrics.GetMetrics()

	tenants := []string{"tenant-1", "tenant-2", "tenant-3"}
	namespace := "embeddings"

	// Record operations for each tenant
	for _, tenantID := range tenants {
		ctx := iam.ContextWithTenant(context.Background(), tenantID)
		startTime := time.Now()
		recorder.RecordUpsert(ctx, namespace, startTime, true)
	}

	// Verify each tenant has exactly 1 operation
	for _, tenantID := range tenants {
		labels := prometheus.Labels{
			"tenant_id": tenantID,
			"namespace": namespace,
			"operation": "upsert",
			"status":    "success",
		}
		value := testutil.ToFloat64(m.VectorOperationsTotal.With(labels))
		assert.Equal(t, 1.0, value, "Tenant %s should have 1 operation", tenantID)
	}
}

func TestVectorMetrics_MetricsRegistration(t *testing.T) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	startTime := time.Now()

	// Record various metrics
	recorder.RecordUpsert(ctx, "embeddings", startTime, true)
	recorder.RecordSearch(ctx, "embeddings", "similarity", startTime, true)
	recorder.UpdateNamespaceStats(ctx, "embeddings", 1000, 1048576)
	recorder.UpdateQuotaUsage(ctx, "embeddings", "vectors", 0.5)
	recorder.UpdateTenantNamespaceCount(ctx, 3)

	// Gather metrics from registry
	metricFamilies, err := metrics.Registry.Gather()
	require.NoError(t, err, "Should be able to gather metrics")

	// Build map of registered metric names
	registeredMetrics := make(map[string]bool)
	for _, mf := range metricFamilies {
		registeredMetrics[mf.GetName()] = true
	}

	// Verify vector metrics are registered
	expectedMetrics := []string{
		"quicbaas_vector_operations_total",
		"quicbaas_vector_operation_duration_seconds",
		"quicbaas_vector_storage_bytes",
		"quicbaas_vector_count",
		"quicbaas_vector_search_latency_seconds",
		"quicbaas_vector_quota_usage_ratio",
		"quicbaas_vector_namespace_count",
	}

	for _, expected := range expectedMetrics {
		assert.True(t, registeredMetrics[expected],
			"Vector metric %s should be registered", expected)
	}
}

func BenchmarkVectorMetricsRecorder_RecordUpsert(b *testing.B) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		startTime := time.Now()
		recorder.RecordUpsert(ctx, "embeddings", startTime, true)
	}
}

func BenchmarkVectorMetricsRecorder_RecordSearch(b *testing.B) {
	metrics.Reset()
	recorder := NewVectorMetricsRecorder()
	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		startTime := time.Now()
		recorder.RecordSearch(ctx, "embeddings", "similarity", startTime, true)
	}
}
