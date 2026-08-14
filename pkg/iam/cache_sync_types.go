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

package iam

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// EnhancedPolicyChangeEvent represents a policy change event for distributed cache synchronization.
// This extends the base PolicyChangeEvent with additional metadata for enhanced sync.
type EnhancedPolicyChangeEvent struct {
	// EventID is a unique identifier for this event.
	EventID string `json:"event_id"`

	// Type is the change type (snapshot, created, updated, deleted, batch).
	Type PolicyUpdateType `json:"type"`

	// TenantID is the tenant this policy belongs to.
	TenantID string `json:"tenant_id"`

	// PolicyName is the policy name/identifier.
	PolicyName string `json:"policy_name"`

	// Policy contains the full policy object for create/update events.
	Policy *Policy `json:"policy,omitempty"`

	// CompiledPolicy contains the compiled policy for optimization.
	CompiledPolicy *CompiledPolicy `json:"compiled_policy,omitempty"`

	// Version is the policy version for ordering events.
	Version int64 `json:"version"`

	// Timestamp is when the change occurred.
	Timestamp time.Time `json:"timestamp"`

	// NodeID identifies the originating node.
	NodeID string `json:"node_id"`

	// Metadata contains additional metadata for the event.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MarshalBinary implements encoding.BinaryMarshaler for efficient serialization.
func (e *EnhancedPolicyChangeEvent) MarshalBinary() ([]byte, error) {
	return json.Marshal(e)
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler for efficient deserialization.
func (e *EnhancedPolicyChangeEvent) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, e)
}

// EnhancedCacheVersionTracker tracks cache versions for synchronization with per-policy granularity.
type EnhancedCacheVersionTracker struct {
	*CacheVersionTracker

	// policyVersions tracks per-policy versions using sync.Map for lock-free access.
	policyVersions sync.Map // map[string]int64 (key: "tenantID:policyName")
}

// NewEnhancedCacheVersionTracker creates a new enhanced cache version tracker.
func NewEnhancedCacheVersionTracker() *EnhancedCacheVersionTracker {
	return &EnhancedCacheVersionTracker{
		CacheVersionTracker: NewCacheVersionTracker(),
	}
}

// GetPolicyVersion returns the version for a specific policy.
func (t *EnhancedCacheVersionTracker) GetPolicyVersion(tenantID, policyName string) int64 {
	key := t.makePolicyKey(tenantID, policyName)
	if v, ok := t.policyVersions.Load(key); ok {
		if version, ok := v.(int64); ok {
			return version
		}
	}
	return 0
}

// SetPolicyVersion sets the version for a specific policy.
func (t *EnhancedCacheVersionTracker) SetPolicyVersion(tenantID, policyName string, version int64) {
	key := t.makePolicyKey(tenantID, policyName)
	t.policyVersions.Store(key, version)

	// Update tenant version if this version is higher
	currentTenantVersion := t.GetTenantVersion(tenantID)
	if version > currentTenantVersion {
		t.SetTenantVersion(tenantID, version)
	}
}

// makePolicyKey creates a unique key for policy version tracking.
func (t *EnhancedCacheVersionTracker) makePolicyKey(tenantID, policyName string) string {
	return tenantID + ":" + policyName
}

// EnhancedCacheSyncConfig extends CacheSyncConfig with additional options for enhanced sync.
type EnhancedCacheSyncConfig struct {
	*CacheSyncConfig

	// BatchSize is the maximum number of events to batch together.
	BatchSize int

	// BatchInterval is the maximum time to wait before flushing a batch.
	BatchInterval time.Duration

	// EnableCompression enables gzip compression for policy data.
	EnableCompression bool

	// CompressionLevel is the gzip compression level (1-9, 0 for default).
	CompressionLevel int

	// DeltaLogMaxEntries is the maximum number of delta log entries to keep.
	DeltaLogMaxEntries int

	// EnableIncrementalSync enables incremental sync support.
	EnableIncrementalSync bool
}

// DefaultEnhancedCacheSyncConfig returns a default enhanced configuration.
func DefaultEnhancedCacheSyncConfig(nodeID, tenantID string) *EnhancedCacheSyncConfig {
	return &EnhancedCacheSyncConfig{
		CacheSyncConfig:       DefaultCacheSyncConfig(nodeID, tenantID),
		BatchSize:             100,
		BatchInterval:         5 * time.Second,
		EnableCompression:     true,
		CompressionLevel:      6, // gzip.DefaultCompression
		DeltaLogMaxEntries:    10000,
		EnableIncrementalSync: true,
	}
}

// Validate validates the enhanced configuration.
func (c *EnhancedCacheSyncConfig) Validate() error {
	if err := c.CacheSyncConfig.Validate(); err != nil {
		return err
	}
	if c.BatchSize <= 0 {
		return &InvalidSyncConfigError{Reason: "batch size must be positive"}
	}
	if c.BatchInterval <= 0 {
		return &InvalidSyncConfigError{Reason: "batch interval must be positive"}
	}
	if c.CompressionLevel < 0 || c.CompressionLevel > 9 {
		return &InvalidSyncConfigError{Reason: "compression level must be between 0 and 9"}
	}
	if c.DeltaLogMaxEntries <= 0 {
		return &InvalidSyncConfigError{Reason: "delta log max entries must be positive"}
	}
	return nil
}

// EnhancedCacheSyncMetrics extends CacheSyncMetrics with additional metrics.
type EnhancedCacheSyncMetrics struct {
	*CacheSyncMetrics

	// batchesSent tracks the number of batches sent.
	batchesSent atomic.Int64

	// batchesReceived tracks the number of batches received.
	batchesReceived atomic.Int64

	// compressionRatio tracks the average compression ratio.
	compressionRatio atomic.Int64 // stored as ratio * 1000 for precision

	// deltaLogSize tracks the current delta log size.
	deltaLogSize atomic.Int64

	// incrementalSyncs tracks the number of incremental syncs performed.
	incrementalSyncs atomic.Int64

	// fullSyncs tracks the number of full syncs performed.
	fullSyncs atomic.Int64
}

// NewEnhancedCacheSyncMetrics creates a new enhanced cache sync metrics instance.
func NewEnhancedCacheSyncMetrics() *EnhancedCacheSyncMetrics {
	return &EnhancedCacheSyncMetrics{
		CacheSyncMetrics: initCacheSyncMetrics(),
	}
}

// RecordBatchSent increments the batches sent counter.
func (m *EnhancedCacheSyncMetrics) RecordBatchSent() {
	m.batchesSent.Add(1)
}

// RecordBatchReceived increments the batches received counter.
func (m *EnhancedCacheSyncMetrics) RecordBatchReceived() {
	m.batchesReceived.Add(1)
}

// RecordCompressionRatio records a compression ratio (uncompressed / compressed).
func (m *EnhancedCacheSyncMetrics) RecordCompressionRatio(ratio float64) {
	m.compressionRatio.Store(int64(ratio * 1000))
}

// GetCompressionRatio returns the current compression ratio.
func (m *EnhancedCacheSyncMetrics) GetCompressionRatio() float64 {
	return float64(m.compressionRatio.Load()) / 1000.0
}

// RecordDeltaLogSize records the current delta log size.
func (m *EnhancedCacheSyncMetrics) RecordDeltaLogSize(size int64) {
	m.deltaLogSize.Store(size)
}

// RecordIncrementalSync increments the incremental sync counter.
func (m *EnhancedCacheSyncMetrics) RecordIncrementalSync() {
	m.incrementalSyncs.Add(1)
}

// RecordFullSync increments the full sync counter.
func (m *EnhancedCacheSyncMetrics) RecordFullSync() {
	m.fullSyncs.Add(1)
}

// GetStats returns enhanced sync statistics.
func (m *EnhancedCacheSyncMetrics) GetStats() *EnhancedSyncStats {
	return &EnhancedSyncStats{
		BatchesSent:      m.batchesSent.Load(),
		BatchesReceived:  m.batchesReceived.Load(),
		CompressionRatio: m.GetCompressionRatio(),
		DeltaLogSize:     m.deltaLogSize.Load(),
		IncrementalSyncs: m.incrementalSyncs.Load(),
		FullSyncs:        m.fullSyncs.Load(),
		EventsProcessed:  m.eventsProcessed.Load(),
		SyncErrors:       m.syncErrorCount.Load(),
	}
}

// EnhancedSyncStats represents enhanced synchronization statistics.
type EnhancedSyncStats struct {
	// BatchesSent is the number of batches sent.
	BatchesSent int64 `json:"batches_sent"`

	// BatchesReceived is the number of batches received.
	BatchesReceived int64 `json:"batches_received"`

	// CompressionRatio is the average compression ratio.
	CompressionRatio float64 `json:"compression_ratio"`

	// DeltaLogSize is the current delta log size.
	DeltaLogSize int64 `json:"delta_log_size"`

	// IncrementalSyncs is the number of incremental syncs performed.
	IncrementalSyncs int64 `json:"incremental_syncs"`

	// FullSyncs is the number of full syncs performed.
	FullSyncs int64 `json:"full_syncs"`

	// EventsProcessed is the total number of events processed.
	EventsProcessed int64 `json:"events_processed"`

	// SyncErrors is the number of sync errors.
	SyncErrors int64 `json:"sync_errors"`
}

// EnhancedPolicyCacheSyncer extends PolicyCacheSyncer with incremental sync support.
type EnhancedPolicyCacheSyncer interface {
	PolicyCacheSyncer

	// SyncIncremental performs an incremental sync from a specific version.
	SyncIncremental(ctx context.Context, tenantID string, sinceVersion int64) error

	// GetNodeVersion returns the current cache version for a node.
	GetNodeVersion(tenantID string) int64

	// GetEnhancedStats returns enhanced synchronization statistics.
	GetEnhancedStats() *EnhancedSyncStats
}

// EnhancedPolicyChangeHandler is a callback function for handling enhanced policy change events.
type EnhancedPolicyChangeHandler func(ctx context.Context, event *EnhancedPolicyChangeEvent) error
