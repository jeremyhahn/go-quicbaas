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
// This package implements request-level, transaction, and Raft-specific metrics
// following Prometheus best practices.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Registry is the Prometheus registry used for all QuicBaaS metrics.
	Registry = prometheus.NewRegistry()

	// metricsOnce ensures metrics are initialized only once.
	metricsOnce sync.Once

	// metricsMu protects defaultMetrics pointer access.
	metricsMu sync.RWMutex
)

// Metrics holds all QuicBaaS metrics.
type Metrics struct {
	// Request-level metrics
	RequestDuration    *prometheus.HistogramVec
	RequestSize        *prometheus.HistogramVec
	ConcurrentRequests prometheus.Gauge

	// Transaction metrics
	TransactionDuration  prometheus.Histogram
	TransactionSize      prometheus.Histogram
	TransactionConflicts prometheus.Counter

	// Raft-specific metrics
	RaftProposalLatency *prometheus.HistogramVec
	RaftSnapshotSize    *prometheus.GaugeVec

	// Vector multi-tenant metrics
	VectorOperationsTotal   *prometheus.CounterVec
	VectorOperationDuration *prometheus.HistogramVec
	VectorStorageBytes      *prometheus.GaugeVec
	VectorCount             *prometheus.GaugeVec
	VectorSearchLatency     *prometheus.HistogramVec
	VectorBatchSize         *prometheus.HistogramVec
	VectorQuotaUsage        *prometheus.GaugeVec
	VectorNamespaceCount    *prometheus.GaugeVec

	// Orchestration metrics
	OrchPeerMessagesTotal    *prometheus.CounterVec
	OrchPeerInboxSize        *prometheus.GaugeVec
	OrchFileLocksActive      *prometheus.GaugeVec
	OrchFileLockWaitDuration *prometheus.HistogramVec
	OrchAgentRestarts        *prometheus.CounterVec

	// FactoryTokensTotal counts the tokens every recorded turn consumed, by
	// tenant, project, kind (the job's runner), provider, backend, model and
	// direction (input, output, cache_read, cache_write). EVERY LABEL IS
	// BOUNDED (go-quicbaas#1058, ruling F7): model by the price table plus
	// "other", provider by the price table plus "unknown", backend by the
	// configured engines, project by the project count. The host and the job
	// are never labels; they are on the ledger entry and in the API.
	FactoryTokensTotal *prometheus.CounterVec

	// FactoryCostMicrosTotal counts the money every priced turn cost, in
	// millionths of a US dollar, under the same labels as FactoryTokensTotal
	// with basis "equivalent" (list price) or "billed" (what the bill paid).
	// A turn with no known figure adds nothing to that basis.
	FactoryCostMicrosTotal *prometheus.CounterVec

	// FactorySpendDeferrals counts admission deferring a project because its
	// own spend limits, or its tenant's, are spent, by axis
	// ("project_spend" or "tenant_spend"). Counted when a deferral starts or
	// changes reason, not on every sweep.
	FactorySpendDeferrals *prometheus.CounterVec

	// OrchAccountPoolExhausted counts account resolutions that found every
	// pooled account of a provider unavailable, by why: "cooling_down" (all on
	// a provider limit hold) or "saturated" (all at their session cap).
	OrchAccountPoolExhausted *prometheus.CounterVec

	// OrchAccountLimitHolds counts pooled accounts taken out of rotation
	// because the provider refused them for a rate or usage limit.
	OrchAccountLimitHolds *prometheus.CounterVec

	// OrchAccountRefreshStampFailures counts account-record writes that
	// failed to stamp a token-refresh outcome (status, expiry, last_error).
	OrchAccountRefreshStampFailures *prometheus.CounterVec

	// Indexing metrics
	IndexFilesIndexedTotal *prometheus.CounterVec
	IndexVectorsTotal      *prometheus.GaugeVec
	IndexEmbedDuration     *prometheus.HistogramVec
	IndexChunkDuration     *prometheus.HistogramVec
}

// defaultMetrics holds the singleton instance of all metrics.
var defaultMetrics *Metrics

// init registers Go runtime and process collectors.
func init() {
	Registry.MustRegister(collectors.NewGoCollector())
	Registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// InitMetrics initializes all QuicBaaS metrics and registers them with Prometheus.
// This function is safe to call multiple times - initialization only happens once.
func InitMetrics() *Metrics {
	metricsOnce.Do(func() {
		metricsMu.Lock()
		defer metricsMu.Unlock()

		defaultMetrics = &Metrics{
			// Request-level metrics
			RequestDuration: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_request_duration_seconds",
					Help:    "Request latency by operation and status",
					Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
				},
				[]string{"operation", "status"},
			),

			RequestSize: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_request_size_bytes",
					Help:    "Request payload size in bytes",
					Buckets: prometheus.ExponentialBuckets(100, 10, 7), // 100, 1000, 10000, 100000, 1000000, 10000000, 100000000
				},
				[]string{"operation"},
			),

			ConcurrentRequests: promauto.With(Registry).NewGauge(
				prometheus.GaugeOpts{
					Name: "quicbaas_concurrent_requests",
					Help: "Number of requests currently being processed",
				},
			),

			// Transaction metrics
			TransactionDuration: promauto.With(Registry).NewHistogram(
				prometheus.HistogramOpts{
					Name:    "quicbaas_transaction_duration_seconds",
					Help:    "Transaction execution time",
					Buckets: []float64{.001, .01, .1, 1, 10},
				},
			),

			TransactionSize: promauto.With(Registry).NewHistogram(
				prometheus.HistogramOpts{
					Name:    "quicbaas_transaction_operations",
					Help:    "Number of operations per transaction",
					Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500},
				},
			),

			TransactionConflicts: promauto.With(Registry).NewCounter(
				prometheus.CounterOpts{
					Name: "quicbaas_transaction_conflicts_total",
					Help: "Number of transaction conflicts (optimistic locking failures)",
				},
			),

			// Raft-specific metrics
			RaftProposalLatency: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_raft_proposal_latency_seconds",
					Help:    "Time from proposal to commit by shard",
					Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5},
				},
				[]string{"shard"},
			),

			RaftSnapshotSize: promauto.With(Registry).NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "quicbaas_raft_snapshot_size_bytes",
					Help: "Current snapshot size by shard",
				},
				[]string{"shard"},
			),

			// Vector multi-tenant metrics
			VectorOperationsTotal: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_vector_operations_total",
					Help: "Total vector operations by tenant, namespace, operation type, and status",
				},
				[]string{"tenant_id", "namespace", "operation", "status"},
			),

			VectorOperationDuration: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_vector_operation_duration_seconds",
					Help:    "Vector operation latency by tenant, namespace, and operation type",
					Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
				},
				[]string{"tenant_id", "namespace", "operation"},
			),

			VectorStorageBytes: promauto.With(Registry).NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "quicbaas_vector_storage_bytes",
					Help: "Storage used by vectors by tenant and namespace",
				},
				[]string{"tenant_id", "namespace"},
			),

			VectorCount: promauto.With(Registry).NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "quicbaas_vector_count",
					Help: "Number of vectors stored by tenant and namespace",
				},
				[]string{"tenant_id", "namespace"},
			),

			VectorSearchLatency: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_vector_search_latency_seconds",
					Help:    "Vector search latency by tenant, namespace, and search type",
					Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
				},
				[]string{"tenant_id", "namespace", "search_type"},
			),

			VectorBatchSize: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_vector_batch_size",
					Help:    "Size of vector batch operations by tenant, namespace, and operation",
					Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
				},
				[]string{"tenant_id", "namespace", "operation"},
			),

			VectorQuotaUsage: promauto.With(Registry).NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "quicbaas_vector_quota_usage_ratio",
					Help: "Quota usage ratio (0-1) by tenant, namespace, and quota type (vectors, storage)",
				},
				[]string{"tenant_id", "namespace", "quota_type"},
			),

			VectorNamespaceCount: promauto.With(Registry).NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "quicbaas_vector_namespace_count",
					Help: "Number of vector namespaces by tenant",
				},
				[]string{"tenant_id"},
			),

			// Orchestration metrics
			OrchPeerMessagesTotal: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_orch_peer_messages_total",
					Help: "Total peer messages sent or received by action and tenant",
				},
				[]string{"action", "tenant"},
			),

			OrchPeerInboxSize: promauto.With(Registry).NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "quicbaas_orch_peer_inbox_size",
					Help: "Current number of messages in an agent's peer inbox",
				},
				[]string{"agent_id", "tenant"},
			),

			OrchFileLocksActive: promauto.With(Registry).NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "quicbaas_orch_file_locks_active",
					Help: "Number of active file locks by project and tenant",
				},
				[]string{"project_id", "tenant"},
			),

			OrchFileLockWaitDuration: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_orch_file_lock_wait_duration_seconds",
					Help:    "Time spent waiting to acquire a file lock",
					Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
				},
				[]string{"project_id"},
			),

			FactoryTokensTotal: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_factory_tokens_total",
					Help: "Tokens consumed by recorded turns, by tenant, project, kind, provider, backend, model and direction",
				},
				factoryTurnLabelNames("direction"),
			),

			FactoryCostMicrosTotal: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_factory_cost_micros_total",
					Help: "Cost of priced turns in millionths of a US dollar, by tenant, project, kind, provider, backend, model and basis (equivalent or billed)",
				},
				factoryTurnLabelNames("basis"),
			),

			FactorySpendDeferrals: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_factory_spend_deferrals_total",
					Help: "Admission deferrals of a project whose own or whose tenant's spend limits are spent, by axis",
				},
				[]string{"tenant", "project", "axis"},
			),

			OrchAgentRestarts: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_orch_agent_restarts_total",
					Help: "Total number of agent restarts by agent and tenant",
				},
				[]string{"agent_id", "tenant"},
			),

			OrchAccountPoolExhausted: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_orch_account_pool_exhausted_total",
					Help: "Account resolutions that found every pooled account of a provider unavailable, by reason",
				},
				[]string{"tenant", "provider", "reason"},
			),

			OrchAccountLimitHolds: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_orch_account_limit_holds_total",
					Help: "Pooled accounts held out of rotation after a provider rate or usage limit",
				},
				[]string{"tenant", "provider"},
			),

			OrchAccountRefreshStampFailures: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_orch_account_refresh_stamp_failures_total",
					Help: "Total account-record writes that failed to stamp a token-refresh outcome, by tenant and outcome",
				},
				[]string{"tenant", "outcome"},
			),

			// Indexing metrics
			IndexFilesIndexedTotal: promauto.With(Registry).NewCounterVec(
				prometheus.CounterOpts{
					Name: "quicbaas_index_files_indexed_total",
					Help: "Total files indexed by project and language",
				},
				[]string{"project_id", "language"},
			),

			IndexVectorsTotal: promauto.With(Registry).NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "quicbaas_index_vectors_total",
					Help: "Total vectors stored in the index by project and backend",
				},
				[]string{"project_id", "backend"},
			),

			IndexEmbedDuration: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_index_embed_duration_seconds",
					Help:    "Duration of embedding generation by provider",
					Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
				},
				[]string{"provider"},
			),

			IndexChunkDuration: promauto.With(Registry).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "quicbaas_index_chunk_duration_seconds",
					Help:    "Duration of file chunking by language",
					Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
				},
				[]string{"language"},
			),
		}
	})

	metricsMu.RLock()
	defer metricsMu.RUnlock()
	return defaultMetrics
}

// GetMetrics returns the singleton instance of QuicBaaS metrics.
// If metrics haven't been initialized, this will initialize them.
func GetMetrics() *Metrics {
	metricsMu.RLock()
	if defaultMetrics != nil {
		defer metricsMu.RUnlock()
		return defaultMetrics
	}
	metricsMu.RUnlock()

	// Not initialized, so initialize
	return InitMetrics()
}

// Reset clears all metrics. This is primarily useful for testing.
func Reset() {
	metricsMu.Lock()
	defer metricsMu.Unlock()

	metricsOnce = sync.Once{}
	defaultMetrics = nil
	Registry = prometheus.NewRegistry()
	Registry.MustRegister(collectors.NewGoCollector())
	Registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// RecordRequestDuration records the duration of a request with operation and status labels.
func (m *Metrics) RecordRequestDuration(operation, status string, duration float64) {
	m.RequestDuration.WithLabelValues(operation, status).Observe(duration)
}

// RecordRequestSize records the size of a request payload.
func (m *Metrics) RecordRequestSize(operation string, size float64) {
	m.RequestSize.WithLabelValues(operation).Observe(size)
}

// IncrementConcurrentRequests increments the concurrent requests gauge.
func (m *Metrics) IncrementConcurrentRequests() {
	m.ConcurrentRequests.Inc()
}

// DecrementConcurrentRequests decrements the concurrent requests gauge.
func (m *Metrics) DecrementConcurrentRequests() {
	m.ConcurrentRequests.Dec()
}

// RecordTransactionDuration records the duration of a transaction.
func (m *Metrics) RecordTransactionDuration(duration float64) {
	m.TransactionDuration.Observe(duration)
}

// RecordTransactionSize records the number of operations in a transaction.
func (m *Metrics) RecordTransactionSize(size float64) {
	m.TransactionSize.Observe(size)
}

// IncrementTransactionConflicts increments the transaction conflicts counter.
func (m *Metrics) IncrementTransactionConflicts() {
	m.TransactionConflicts.Inc()
}

// RecordRaftProposalLatency records the latency of a Raft proposal for a specific shard.
func (m *Metrics) RecordRaftProposalLatency(shard string, latency float64) {
	m.RaftProposalLatency.WithLabelValues(shard).Observe(latency)
}

// SetRaftSnapshotSize sets the snapshot size for a specific shard.
func (m *Metrics) SetRaftSnapshotSize(shard string, size float64) {
	m.RaftSnapshotSize.WithLabelValues(shard).Set(size)
}

// RecordVectorOperation records a vector operation with tenant context.
func (m *Metrics) RecordVectorOperation(tenantID, namespace, operation, status string) {
	m.VectorOperationsTotal.WithLabelValues(tenantID, namespace, operation, status).Inc()
}

// RecordVectorOperationDuration records the duration of a vector operation.
func (m *Metrics) RecordVectorOperationDuration(tenantID, namespace, operation string, duration float64) {
	m.VectorOperationDuration.WithLabelValues(tenantID, namespace, operation).Observe(duration)
}

// SetVectorStorageBytes sets the storage usage for a tenant's namespace.
func (m *Metrics) SetVectorStorageBytes(tenantID, namespace string, bytes float64) {
	m.VectorStorageBytes.WithLabelValues(tenantID, namespace).Set(bytes)
}

// SetVectorCount sets the vector count for a tenant's namespace.
func (m *Metrics) SetVectorCount(tenantID, namespace string, count float64) {
	m.VectorCount.WithLabelValues(tenantID, namespace).Set(count)
}

// RecordVectorSearchLatency records vector search latency by type.
func (m *Metrics) RecordVectorSearchLatency(tenantID, namespace, searchType string, latency float64) {
	m.VectorSearchLatency.WithLabelValues(tenantID, namespace, searchType).Observe(latency)
}

// RecordVectorBatchSize records the size of a batch operation.
func (m *Metrics) RecordVectorBatchSize(tenantID, namespace, operation string, size float64) {
	m.VectorBatchSize.WithLabelValues(tenantID, namespace, operation).Observe(size)
}

// SetVectorQuotaUsage sets the quota usage ratio (0-1) for a tenant's namespace.
func (m *Metrics) SetVectorQuotaUsage(tenantID, namespace, quotaType string, ratio float64) {
	m.VectorQuotaUsage.WithLabelValues(tenantID, namespace, quotaType).Set(ratio)
}

// SetVectorNamespaceCount sets the namespace count for a tenant.
func (m *Metrics) SetVectorNamespaceCount(tenantID string, count float64) {
	m.VectorNamespaceCount.WithLabelValues(tenantID).Set(count)
}

// RecordOrchPeerMessage increments the peer messages counter.
func (m *Metrics) RecordOrchPeerMessage(action, tenant string) {
	m.OrchPeerMessagesTotal.WithLabelValues(action, tenant).Inc()
}

// SetOrchPeerInboxSize sets the current inbox size for an agent.
func (m *Metrics) SetOrchPeerInboxSize(agentID, tenant string, size float64) {
	m.OrchPeerInboxSize.WithLabelValues(agentID, tenant).Set(size)
}

// SetOrchFileLocksActive sets the number of active file locks for a project.
func (m *Metrics) SetOrchFileLocksActive(projectID, tenant string, count float64) {
	m.OrchFileLocksActive.WithLabelValues(projectID, tenant).Set(count)
}

// RecordOrchFileLockWaitDuration records the time spent waiting for a file lock.
func (m *Metrics) RecordOrchFileLockWaitDuration(projectID string, duration float64) {
	m.OrchFileLockWaitDuration.WithLabelValues(projectID).Observe(duration)
}

// FactoryTurnLabels are the bounded labels one recorded turn is counted
// under. The caller bounds them: see FactoryTokensTotal.
type FactoryTurnLabels struct {
	Tenant   string
	Project  string
	Kind     string
	Provider string
	Backend  string
	Model    string
}

// factoryTurnLabelNames is the label set of a factory turn series, with its
// last label (direction or basis).
func factoryTurnLabelNames(last string) []string {
	return []string{"tenant", "project", "kind", "provider", "backend", "model", last}
}

// values renders the labels in factoryTurnLabelNames order.
func (l FactoryTurnLabels) values(last string) []string {
	return []string{l.Tenant, l.Project, l.Kind, l.Provider, l.Backend, l.Model, last}
}

// RecordFactoryTokens adds a turn's tokens in one direction.
func (m *Metrics) RecordFactoryTokens(labels FactoryTurnLabels, direction string, tokens float64) {
	m.FactoryTokensTotal.WithLabelValues(labels.values(direction)...).Add(tokens)
}

// RecordFactoryCostMicros adds a turn's cost on one basis.
func (m *Metrics) RecordFactoryCostMicros(labels FactoryTurnLabels, basis string, micros float64) {
	m.FactoryCostMicrosTotal.WithLabelValues(labels.values(basis)...).Add(micros)
}

// RecordFactorySpendDeferral counts one admission deferral for spend.
func (m *Metrics) RecordFactorySpendDeferral(tenant, project, axis string) {
	m.FactorySpendDeferrals.WithLabelValues(tenant, project, axis).Inc()
}

// RecordOrchAccountPoolExhausted counts one resolution that found a provider's
// whole pool unavailable. reason is "cooling_down", "saturated" or "spent".
func (m *Metrics) RecordOrchAccountPoolExhausted(tenant, provider, reason string) {
	m.OrchAccountPoolExhausted.WithLabelValues(tenant, provider, reason).Inc()
}

// RecordOrchAccountLimitHold counts one account held after a provider limit.
func (m *Metrics) RecordOrchAccountLimitHold(tenant, provider string) {
	m.OrchAccountLimitHolds.WithLabelValues(tenant, provider).Inc()
}

// RecordOrchAgentRestart increments the agent restart counter.
func (m *Metrics) RecordOrchAgentRestart(agentID, tenant string) {
	m.OrchAgentRestarts.WithLabelValues(agentID, tenant).Inc()
}

// RecordOrchAccountRefreshStampFailure increments the refresh-stamp failure
// counter. outcome is the refresh outcome the lost write was recording
// ("success" or "failure").
func (m *Metrics) RecordOrchAccountRefreshStampFailure(tenant, outcome string) {
	m.OrchAccountRefreshStampFailures.WithLabelValues(tenant, outcome).Inc()
}

// RecordIndexFileIndexed increments the files indexed counter.
func (m *Metrics) RecordIndexFileIndexed(projectID, language string) {
	m.IndexFilesIndexedTotal.WithLabelValues(projectID, language).Inc()
}

// SetIndexVectorsTotal sets the total vectors count for a project and backend.
func (m *Metrics) SetIndexVectorsTotal(projectID, backend string, count float64) {
	m.IndexVectorsTotal.WithLabelValues(projectID, backend).Set(count)
}

// RecordIndexEmbedDuration records the duration of an embedding generation.
func (m *Metrics) RecordIndexEmbedDuration(provider string, duration float64) {
	m.IndexEmbedDuration.WithLabelValues(provider).Observe(duration)
}

// RecordIndexChunkDuration records the duration of a file chunking operation.
func (m *Metrics) RecordIndexChunkDuration(language string, duration float64) {
	m.IndexChunkDuration.WithLabelValues(language).Observe(duration)
}
