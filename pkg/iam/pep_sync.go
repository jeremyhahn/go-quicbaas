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

package iam

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// PolicyChangeType represents the type of policy change.
type PolicyChangeType int

const (
	// PolicyChangeCreated indicates a new policy was created.
	PolicyChangeCreated PolicyChangeType = iota

	// PolicyChangeUpdated indicates an existing policy was updated.
	PolicyChangeUpdated

	// PolicyChangeDeleted indicates a policy was deleted.
	PolicyChangeDeleted

	// PolicyChangeSnapshot indicates a full policy snapshot sync.
	PolicyChangeSnapshot
)

// String returns the string representation of the change type.
func (t PolicyChangeType) String() string {
	switch t {
	case PolicyChangeCreated:
		return "created"
	case PolicyChangeUpdated:
		return "updated"
	case PolicyChangeDeleted:
		return "deleted"
	case PolicyChangeSnapshot:
		return "snapshot"
	default:
		return "unknown"
	}
}

// PolicyChangeEvent represents a policy change event for cache synchronization.
type PolicyChangeEvent struct {
	// Type is the change type (created, updated, deleted, snapshot).
	Type PolicyChangeType `json:"type"`

	// PolicyID is the unique policy identifier.
	PolicyID string `json:"policy_id"`

	// TenantID is the tenant this policy belongs to.
	TenantID string `json:"tenant_id"`

	// Version is the policy version for ordering events.
	Version int64 `json:"version"`

	// Timestamp is when the change occurred.
	Timestamp time.Time `json:"timestamp"`

	// Data contains the serialized policy for create/update events.
	Data []byte `json:"data,omitempty"`

	// NodeID identifies the originating node.
	NodeID string `json:"node_id,omitempty"`
}

// MarshalBinary implements encoding.BinaryMarshaler for efficient serialization.
func (e *PolicyChangeEvent) MarshalBinary() ([]byte, error) {
	return json.Marshal(e)
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler for efficient deserialization.
func (e *PolicyChangeEvent) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, e)
}

// PolicyChangeHandler is a callback function for handling policy change events.
type PolicyChangeHandler func(ctx context.Context, event PolicyChangeEvent) error

// PolicyCacheSyncer defines the interface for distributed cache synchronization.
type PolicyCacheSyncer interface {
	// NotifyPolicyChange broadcasts a policy change to all nodes.
	NotifyPolicyChange(ctx context.Context, event PolicyChangeEvent) error

	// Subscribe listens for policy changes from other nodes.
	Subscribe(ctx context.Context, handler PolicyChangeHandler) error

	// Unsubscribe stops listening for policy changes.
	Unsubscribe(ctx context.Context) error

	// SyncFromLeader performs a full sync from the leader node.
	SyncFromLeader(ctx context.Context) error

	// GetSyncStatus returns the current synchronization status.
	GetSyncStatus() *SyncStatus

	// Close closes the syncer and releases resources.
	Close() error
}

// SyncStatus represents the current synchronization status.
type SyncStatus struct {
	// IsSubscribed indicates if actively receiving updates.
	IsSubscribed bool `json:"is_subscribed"`

	// LastSyncTime is the timestamp of the last successful sync.
	LastSyncTime time.Time `json:"last_sync_time"`

	// LastSyncVersion is the version from the last sync.
	LastSyncVersion int64 `json:"last_sync_version"`

	// SyncErrors is the number of sync errors encountered.
	SyncErrors int64 `json:"sync_errors"`

	// EventsProcessed is the total number of events processed.
	EventsProcessed int64 `json:"events_processed"`

	// LeaderNodeID is the current leader node ID (if known).
	LeaderNodeID string `json:"leader_node_id,omitempty"`

	// SyncLatencyNs is the average sync latency in nanoseconds.
	SyncLatencyNs int64 `json:"sync_latency_ns"`
}

// CacheSyncConfig holds configuration for cache synchronization.
type CacheSyncConfig struct {
	// NodeID is this node's unique identifier.
	NodeID string

	// TenantID is the tenant for which to sync policies.
	TenantID string

	// SyncInterval is the interval for periodic sync operations.
	SyncInterval time.Duration

	// RetryInterval is the interval between retry attempts.
	RetryInterval time.Duration

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int

	// BufferSize is the event channel buffer size.
	BufferSize int

	// EnableMetrics enables Prometheus metrics collection.
	EnableMetrics bool
}

// DefaultCacheSyncConfig returns a default configuration.
func DefaultCacheSyncConfig(nodeID, tenantID string) *CacheSyncConfig {
	return &CacheSyncConfig{
		NodeID:        nodeID,
		TenantID:      tenantID,
		SyncInterval:  30 * time.Second,
		RetryInterval: 5 * time.Second,
		MaxRetries:    3,
		BufferSize:    1000,
		EnableMetrics: true,
	}
}

// Validate validates the configuration.
func (c *CacheSyncConfig) Validate() error {
	if c.NodeID == "" {
		return &InvalidSyncConfigError{Reason: "node ID is required"}
	}
	if c.TenantID == "" {
		return &InvalidSyncConfigError{Reason: "tenant ID is required"}
	}
	if c.SyncInterval <= 0 {
		return &InvalidSyncConfigError{Reason: "sync interval must be positive"}
	}
	if c.RetryInterval <= 0 {
		return &InvalidSyncConfigError{Reason: "retry interval must be positive"}
	}
	if c.MaxRetries < 0 {
		return &InvalidSyncConfigError{Reason: "max retries cannot be negative"}
	}
	if c.BufferSize <= 0 {
		return &InvalidSyncConfigError{Reason: "buffer size must be positive"}
	}
	return nil
}

// SyncFailedError represents a synchronization failure error.
type SyncFailedError struct {
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *SyncFailedError) Error() string {
	if e.Cause != nil {
		return "sync failed during " + e.Operation + ": " + e.Cause.Error()
	}
	return "sync failed during " + e.Operation
}

// Unwrap returns the underlying error.
func (e *SyncFailedError) Unwrap() error {
	return e.Cause
}

// ErrNotSubscribed is returned when operations require an active subscription.
var ErrNotSubscribed = &dberrors.DragonError{
	Code: dberrors.ErrForbidden,
	Op:   "iam.pep.sync",
	Err:  &IAMMessageError{Message: "not subscribed to policy changes"},
}

// ErrAlreadySubscribed is returned when trying to subscribe while already subscribed.
var ErrAlreadySubscribed = &dberrors.DragonError{
	Code: dberrors.ErrAlreadyExists,
	Op:   "iam.pep.sync",
	Err:  &IAMMessageError{Message: "already subscribed to policy changes"},
}

// ErrSyncerClosed is returned when operating on a closed syncer.
var ErrSyncerClosed = &dberrors.DragonError{
	Code: dberrors.ErrForbidden,
	Op:   "iam.pep.sync",
	Err:  &IAMMessageError{Message: "syncer is closed"},
}

// CacheSyncMetrics holds Prometheus metrics for cache synchronization.
type CacheSyncMetrics struct {
	eventsReceived  *metrics.CounterVec
	eventsSent      *metrics.CounterVec
	syncLatency     *metrics.HistogramVec
	syncErrors      *metrics.CounterVec
	cacheVersion    *metrics.Gauge
	subscriberCount *metrics.Gauge
	eventsProcessed atomic.Int64
	syncErrorCount  atomic.Int64
}

var (
	globalCacheSyncMetrics     *CacheSyncMetrics
	globalCacheSyncMetricsOnce sync.Once
)

// initCacheSyncMetrics initializes the global cache sync metrics.
func initCacheSyncMetrics() *CacheSyncMetrics {
	globalCacheSyncMetricsOnce.Do(func() {
		globalCacheSyncMetrics = &CacheSyncMetrics{
			eventsReceived: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "iam_cache_sync",
				Name:      "events_received_total",
				Help:      "Total number of cache sync events received",
			}, []string{"tenant", "type"}),

			eventsSent: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "iam_cache_sync",
				Name:      "events_sent_total",
				Help:      "Total number of cache sync events sent",
			}, []string{"tenant", "type"}),

			syncLatency: metrics.NewHistogramVec(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "iam_cache_sync",
				Name:      "latency_seconds",
				Help:      "Cache sync latency in seconds",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
			}, []string{"tenant", "operation"}),

			syncErrors: metrics.NewCounterVec(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "iam_cache_sync",
				Name:      "errors_total",
				Help:      "Total number of cache sync errors",
			}, []string{"tenant", "operation"}),

			cacheVersion: metrics.NewGauge(metrics.GaugeOpts{
				Namespace: "quicbaas",
				Subsystem: "iam_cache_sync",
				Name:      "cache_version",
				Help:      "Current cache version",
			}),

			subscriberCount: metrics.NewGauge(metrics.GaugeOpts{
				Namespace: "quicbaas",
				Subsystem: "iam_cache_sync",
				Name:      "subscribers",
				Help:      "Number of active subscribers",
			}),
		}
	})
	return globalCacheSyncMetrics
}

// InvalidationCallback is called when a cache entry should be invalidated.
type InvalidationCallback func(tenantID, policyID string, version int64)

// CacheVersionTracker tracks cache versions for synchronization.
type CacheVersionTracker struct {
	// version is the current cache version (atomic for lock-free access).
	version atomic.Int64

	// tenantVersions tracks per-tenant versions.
	tenantVersions sync.Map // map[string]int64

	// invalidationCallbacks holds registered invalidation callbacks.
	invalidationCallbacks []InvalidationCallback

	// callbackMu protects the callbacks slice.
	callbackMu sync.RWMutex
}

// NewCacheVersionTracker creates a new cache version tracker.
func NewCacheVersionTracker() *CacheVersionTracker {
	return &CacheVersionTracker{
		invalidationCallbacks: make([]InvalidationCallback, 0),
	}
}

// GetVersion returns the current global cache version.
func (t *CacheVersionTracker) GetVersion() int64 {
	return t.version.Load()
}

// IncrementVersion increments and returns the new global version.
func (t *CacheVersionTracker) IncrementVersion() int64 {
	return t.version.Add(1)
}

// GetTenantVersion returns the version for a specific tenant.
func (t *CacheVersionTracker) GetTenantVersion(tenantID string) int64 {
	if v, ok := t.tenantVersions.Load(tenantID); ok {
		return v.(int64) //nolint:errcheck // type assertion guaranteed by sync.Map contract
	}
	return 0
}

// SetTenantVersion sets the version for a specific tenant.
func (t *CacheVersionTracker) SetTenantVersion(tenantID string, version int64) {
	t.tenantVersions.Store(tenantID, version)
	// Update global version if tenant version is higher
	for {
		current := t.version.Load()
		if version <= current {
			break
		}
		if t.version.CompareAndSwap(current, version) {
			break
		}
	}
}

// RegisterInvalidationCallback registers a callback for cache invalidation.
func (t *CacheVersionTracker) RegisterInvalidationCallback(callback InvalidationCallback) {
	t.callbackMu.Lock()
	defer t.callbackMu.Unlock()
	t.invalidationCallbacks = append(t.invalidationCallbacks, callback)
}

// NotifyInvalidation notifies all registered callbacks of an invalidation.
func (t *CacheVersionTracker) NotifyInvalidation(tenantID, policyID string, version int64) {
	t.callbackMu.RLock()
	callbacks := make([]InvalidationCallback, len(t.invalidationCallbacks))
	copy(callbacks, t.invalidationCallbacks)
	t.callbackMu.RUnlock()

	for _, callback := range callbacks {
		callback(tenantID, policyID, version)
	}
}

// ClearCallbacks removes all registered invalidation callbacks.
func (t *CacheVersionTracker) ClearCallbacks() {
	t.callbackMu.Lock()
	defer t.callbackMu.Unlock()
	t.invalidationCallbacks = t.invalidationCallbacks[:0]
}

// CacheSyncInterface defines the interface that a cache must implement for synchronization.
type CacheSyncInterface interface {
	// GetVersion returns the current cache version.
	GetVersion() int64

	// Set stores a compiled policy in the cache.
	Set(tenantID string, policy *CompiledPolicy)

	// Invalidate removes a specific policy from the cache.
	Invalidate(tenantID, name string)

	// InvalidateAll removes all policies for a tenant from the cache.
	InvalidateAll(tenantID string)

	// GetAll returns all cached policies for a tenant.
	GetAll(tenantID string) []*CompiledPolicy

	// Size returns the number of cached policies.
	Size() int

	// Clear removes all entries from the cache.
	Clear()
}

// Verify PolicyCache implements CacheSyncInterface.
var _ CacheSyncInterface = (*PolicyCache)(nil)
