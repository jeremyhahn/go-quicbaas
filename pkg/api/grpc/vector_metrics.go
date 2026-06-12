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
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// VectorMetricsRecorder provides methods for recording vector operation metrics
// with multi-tenant context.
type VectorMetricsRecorder struct {
	metrics *metrics.Metrics
}

// NewVectorMetricsRecorder creates a new vector metrics recorder.
func NewVectorMetricsRecorder() *VectorMetricsRecorder {
	return &VectorMetricsRecorder{
		metrics: metrics.GetMetrics(),
	}
}

// defaultTenantID is the fallback tenant ID for non-multi-tenant
// deployments: the control-plane system tenant that qrdb always provisions.
const defaultTenantID = qrdbsdk.SystemTenantID

// getTenantID extracts the tenant ID from context or returns the fallback.
func getTenantID(ctx context.Context) string {
	tenantID := iam.TenantFromContext(ctx)
	if tenantID == "" {
		return defaultTenantID
	}
	return tenantID
}

// RecordUpsert records an upsert operation with timing.
func (r *VectorMetricsRecorder) RecordUpsert(ctx context.Context, namespace string, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "upsert", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "upsert", duration)
}

// RecordBatchUpsert records a batch upsert operation with timing and batch size.
func (r *VectorMetricsRecorder) RecordBatchUpsert(ctx context.Context, namespace string, batchSize int, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "batch_upsert", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "batch_upsert", duration)
	r.metrics.RecordVectorBatchSize(tenantID, namespace, "upsert", float64(batchSize))
}

// RecordGet records a get operation with timing.
func (r *VectorMetricsRecorder) RecordGet(ctx context.Context, namespace string, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "get", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "get", duration)
}

// RecordDelete records a delete operation with timing.
func (r *VectorMetricsRecorder) RecordDelete(ctx context.Context, namespace string, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "delete", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "delete", duration)
}

// RecordBatchDelete records a batch delete operation with timing.
func (r *VectorMetricsRecorder) RecordBatchDelete(ctx context.Context, namespace string, batchSize int, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "batch_delete", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "batch_delete", duration)
	r.metrics.RecordVectorBatchSize(tenantID, namespace, "delete", float64(batchSize))
}

// RecordSearch records a search operation with timing and search type.
func (r *VectorMetricsRecorder) RecordSearch(ctx context.Context, namespace, searchType string, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, searchType, status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, searchType, duration)
	r.metrics.RecordVectorSearchLatency(tenantID, namespace, searchType, duration)
}

// RecordNamespaceCreate records a namespace creation operation.
func (r *VectorMetricsRecorder) RecordNamespaceCreate(ctx context.Context, namespace string, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "create_namespace", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "create_namespace", duration)
}

// RecordNamespaceDelete records a namespace deletion operation.
func (r *VectorMetricsRecorder) RecordNamespaceDelete(ctx context.Context, namespace string, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "delete_namespace", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "delete_namespace", duration)
}

// UpdateNamespaceStats updates the namespace-level statistics (storage and vector count).
func (r *VectorMetricsRecorder) UpdateNamespaceStats(ctx context.Context, namespace string, vectorCount int64, storageBytes int64) {
	tenantID := getTenantID(ctx)

	r.metrics.SetVectorCount(tenantID, namespace, float64(vectorCount))
	r.metrics.SetVectorStorageBytes(tenantID, namespace, float64(storageBytes))
}

// UpdateQuotaUsage updates the quota usage ratio for a namespace.
// quotaType should be "vectors" or "storage".
// ratio is a value between 0 and 1 representing percentage of quota used.
func (r *VectorMetricsRecorder) UpdateQuotaUsage(ctx context.Context, namespace, quotaType string, ratio float64) {
	tenantID := getTenantID(ctx)
	r.metrics.SetVectorQuotaUsage(tenantID, namespace, quotaType, ratio)
}

// UpdateTenantNamespaceCount updates the count of namespaces for a tenant.
func (r *VectorMetricsRecorder) UpdateTenantNamespaceCount(ctx context.Context, count int) {
	tenantID := getTenantID(ctx)
	r.metrics.SetVectorNamespaceCount(tenantID, float64(count))
}

// RecordExport records an export operation.
func (r *VectorMetricsRecorder) RecordExport(ctx context.Context, namespace string, vectorCount int, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "export", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "export", duration)
	r.metrics.RecordVectorBatchSize(tenantID, namespace, "export", float64(vectorCount))
}

// RecordImport records an import operation.
func (r *VectorMetricsRecorder) RecordImport(ctx context.Context, namespace string, vectorCount int, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "import", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "import", duration)
	r.metrics.RecordVectorBatchSize(tenantID, namespace, "import", float64(vectorCount))
}

// RecordIndexRebuild records an index rebuild operation.
func (r *VectorMetricsRecorder) RecordIndexRebuild(ctx context.Context, namespace string, startTime time.Time, success bool) {
	tenantID := getTenantID(ctx)
	status := "success"
	if !success {
		status = "failure"
	}
	duration := time.Since(startTime).Seconds()

	r.metrics.RecordVectorOperation(tenantID, namespace, "rebuild_index", status)
	r.metrics.RecordVectorOperationDuration(tenantID, namespace, "rebuild_index", duration)
}
