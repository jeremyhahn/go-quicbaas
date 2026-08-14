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

	"github.com/google/uuid"
)

// DistributedPEP wraps a PEP instance with distributed cache synchronization.
// It provides automatic policy synchronization across a cluster using Raft.
type DistributedPEP struct {
	// pep is the underlying Policy Enforcement Point.
	pep *PEP

	// syncer handles distributed cache synchronization.
	syncer EnhancedPolicyCacheSyncer

	// config holds the distributed PEP configuration.
	config *DistributedPEPConfig

	// closed indicates if the distributed PEP is closed.
	closed atomic.Bool

	// mu protects concurrent access to PEP state.
	mu sync.RWMutex

	// syncInProgress tracks if a sync operation is in progress.
	syncInProgress atomic.Bool

	// lastSyncError tracks the last sync error.
	lastSyncError atomic.Value // error
}

// DistributedPEPConfig holds configuration for a distributed PEP.
type DistributedPEPConfig struct {
	// PEPConfig is the base PEP configuration.
	*PEPConfig

	// SyncConfig is the cache synchronization configuration.
	SyncConfig *EnhancedCacheSyncConfig

	// AutoSync enables automatic synchronization with the cluster.
	AutoSync bool

	// SyncOnLoad enables synchronization when loading policies.
	SyncOnLoad bool

	// ConflictResolution determines how to handle version conflicts.
	ConflictResolution ConflictResolutionStrategy
}

// DefaultDistributedPEPConfig returns a distributed PEP configuration with sensible defaults.
func DefaultDistributedPEPConfig(nodeID, tenantID string) *DistributedPEPConfig {
	return &DistributedPEPConfig{
		PEPConfig:          DefaultPEPConfig(tenantID),
		SyncConfig:         DefaultEnhancedCacheSyncConfig(nodeID, tenantID),
		AutoSync:           true,
		SyncOnLoad:         true,
		ConflictResolution: ConflictResolutionHighestVersion,
	}
}

// NewDistributedPEP creates a new distributed Policy Enforcement Point.
func NewDistributedPEP(config *DistributedPEPConfig, raft RaftProposer) (*DistributedPEP, error) {
	if config == nil {
		config = DefaultDistributedPEPConfig("", "")
	}

	if raft == nil {
		return nil, &RaftProposerRequiredError{}
	}

	// Create the base PEP
	pep := NewPEP(config.PEPConfig)

	// Create the enhanced syncer
	syncerOpts := []EnhancedRaftSyncerOption{
		WithConflictResolution(config.ConflictResolution),
	}

	syncer, err := NewEnhancedRaftCacheSyncer(
		config.SyncConfig,
		raft,
		pep.cache,
		syncerOpts...,
	)
	if err != nil {
		return nil, &SyncerCreationError{Cause: err}
	}

	dpep := &DistributedPEP{
		pep:    pep,
		syncer: syncer,
		config: config,
	}

	return dpep, nil
}

// LoadPolicy loads a policy into the PEP cache and broadcasts the change to the cluster.
func (d *DistributedPEP) LoadPolicy(ctx context.Context, policy *Policy) error {
	if d.closed.Load() {
		return ErrPEPClosed
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Load the policy into the local PEP
	if err := d.pep.LoadPolicy(ctx, policy); err != nil {
		return err
	}

	// Broadcast the change to the cluster
	event := d.createPolicyChangeEvent(policy, PolicyUpdateCreated)

	if err := d.broadcastChange(ctx, event); err != nil {
		// Log error but don't fail the load operation
		// The policy is already loaded locally
		d.lastSyncError.Store(err)
	}

	// Sync with cluster if enabled
	if d.config.SyncOnLoad && d.config.AutoSync {
		go func() { // #nosec G118 -- cluster sync runs on its own 30s budget and must outlive the caller.
			syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = d.SyncWithCluster(syncCtx)
		}()
	}

	return nil
}

// RemovePolicy removes a policy from the PEP cache and broadcasts the change to the cluster.
func (d *DistributedPEP) RemovePolicy(ctx context.Context, policyName string) error {
	if d.closed.Load() {
		return ErrPEPClosed
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Remove the policy from the local PEP
	d.pep.RemovePolicy(policyName)

	// Broadcast the change to the cluster
	event := &EnhancedPolicyChangeEvent{
		EventID:    uuid.New().String(),
		Type:       PolicyUpdateDeleted,
		TenantID:   d.config.TenantID,
		PolicyName: policyName,
		Version:    time.Now().UnixNano(),
		Timestamp:  time.Now(),
		NodeID:     d.config.SyncConfig.NodeID,
	}

	if err := d.broadcastChange(ctx, event); err != nil {
		d.lastSyncError.Store(err)
	}

	return nil
}

// SyncWithCluster performs a full synchronization with the cluster.
func (d *DistributedPEP) SyncWithCluster(ctx context.Context) error {
	if d.closed.Load() {
		return ErrPEPClosed
	}

	// Check if sync is already in progress
	if !d.syncInProgress.CompareAndSwap(false, true) {
		return &SyncInProgressError{}
	}
	defer d.syncInProgress.Store(false)

	// Perform full sync from leader
	if err := d.syncer.SyncFromLeader(ctx); err != nil {
		d.lastSyncError.Store(err)
		return err
	}

	// Clear last sync error
	d.lastSyncError.Store(nil)

	return nil
}

// Check performs a single authorization check using the underlying PEP.
func (d *DistributedPEP) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	if d.closed.Load() {
		return nil, ErrPEPClosed
	}

	return d.pep.Check(ctx, req)
}

// CheckBatch performs batch authorization checks using the underlying PEP.
func (d *DistributedPEP) CheckBatch(ctx context.Context, req *BatchCheckRequest) (*BatchCheckResponse, error) {
	if d.closed.Load() {
		return nil, ErrPEPClosed
	}

	return d.pep.CheckBatch(ctx, req)
}

// Subscribe subscribes to policy updates from the cluster.
func (d *DistributedPEP) Subscribe(ctx context.Context, patterns []string) (<-chan PolicyUpdate, error) {
	if d.closed.Load() {
		return nil, ErrPEPClosed
	}

	// Subscribe to syncer events
	if err := d.syncer.Subscribe(ctx, d.handlePolicyChange); err != nil {
		return nil, err
	}

	// Also subscribe to the base PEP if it has a client
	if d.pep.config.Client != nil {
		return d.pep.Subscribe(ctx, patterns)
	}

	// Create a channel for policy updates
	updateCh := make(chan PolicyUpdate, 100)

	// The updates will be handled by handlePolicyChange
	// For now, return an empty channel
	go func() {
		<-ctx.Done()
		close(updateCh)
	}()

	return updateCh, nil
}

// GetStats returns current PEP statistics including sync stats.
func (d *DistributedPEP) GetStats() *DistributedPEPStats {
	baseStats := d.pep.GetStats()
	syncStatus := d.syncer.GetSyncStatus()
	enhancedStats := d.syncer.GetEnhancedStats()

	var lastSyncError string
	if err := d.lastSyncError.Load(); err != nil {
		if e, ok := err.(error); ok {
			lastSyncError = e.Error()
		}
	}

	return &DistributedPEPStats{
		PEPStats:       *baseStats,
		SyncStatus:     *syncStatus,
		EnhancedStats:  *enhancedStats,
		SyncInProgress: d.syncInProgress.Load(),
		LastSyncError:  lastSyncError,
	}
}

// Start starts the distributed PEP and begins background sync if enabled.
func (d *DistributedPEP) Start(ctx context.Context) error {
	if d.closed.Load() {
		return ErrPEPClosed
	}

	// Start the base PEP
	if err := d.pep.Start(ctx); err != nil {
		return err
	}

	// Subscribe to cluster changes if auto-sync is enabled
	if d.config.AutoSync {
		if err := d.syncer.Subscribe(ctx, d.handlePolicyChange); err != nil {
			return &SubscribeFailedError{Cause: err}
		}

		// Perform initial sync
		syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := d.SyncWithCluster(syncCtx); err != nil {
			// Log error but don't fail startup
			d.lastSyncError.Store(err)
		}
	}

	return nil
}

// Close closes the distributed PEP and stops background sync.
func (d *DistributedPEP) Close() error {
	if !d.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Close the syncer
	if err := d.syncer.Close(); err != nil {
		return err
	}

	// Close the base PEP
	return d.pep.Close()
}

// handlePolicyChange handles incoming policy changes from the cluster.
func (d *DistributedPEP) handlePolicyChange(ctx context.Context, event PolicyChangeEvent) error {
	// Skip events from this node (we already applied them locally)
	if event.NodeID == d.config.SyncConfig.NodeID {
		return nil
	}

	// Apply the change to the local cache
	d.mu.Lock()
	defer d.mu.Unlock()

	switch event.Type {
	case PolicyChangeCreated, PolicyChangeUpdated:
		if len(event.Data) == 0 {
			return &MissingPolicyDataError{EventType: event.Type.String()}
		}

		var policy Policy
		if err := json.Unmarshal(event.Data, &policy); err != nil {
			return &PolicyUnmarshalError{Cause: err}
		}

		// Load the policy into the local PEP
		return d.pep.LoadPolicy(ctx, &policy)

	case PolicyChangeDeleted:
		// Remove the policy from the local PEP
		d.pep.RemovePolicy(event.PolicyID)
		return nil

	case PolicyChangeSnapshot:
		// Trigger a full sync
		go func() { // #nosec G118 -- cluster sync runs on its own 30s budget and must outlive the caller.
			syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = d.SyncWithCluster(syncCtx)
		}()
		return nil
	}

	return nil
}

// broadcastChange broadcasts a policy change to the cluster.
func (d *DistributedPEP) broadcastChange(ctx context.Context, event *EnhancedPolicyChangeEvent) error {
	// Serialize the policy data
	var data []byte
	var err error

	if event.Policy != nil {
		data, err = json.Marshal(event.Policy)
		if err != nil {
			return &PolicyMarshalError{Cause: err}
		}
	}

	// Convert to base PolicyChangeEvent
	baseEvent := PolicyChangeEvent{
		Type:      d.convertToBaseType(event.Type),
		PolicyID:  event.PolicyName,
		TenantID:  event.TenantID,
		Version:   event.Version,
		Timestamp: event.Timestamp,
		Data:      data,
		NodeID:    event.NodeID,
	}

	// Notify via syncer
	return d.syncer.NotifyPolicyChange(ctx, baseEvent)
}

// createPolicyChangeEvent creates an enhanced policy change event.
func (d *DistributedPEP) createPolicyChangeEvent(policy *Policy, updateType PolicyUpdateType) *EnhancedPolicyChangeEvent {
	return &EnhancedPolicyChangeEvent{
		EventID:    uuid.New().String(),
		Type:       updateType,
		TenantID:   d.config.TenantID,
		PolicyName: policy.Metadata.Name,
		Policy:     policy,
		Version:    time.Now().UnixNano(),
		Timestamp:  time.Now(),
		NodeID:     d.config.SyncConfig.NodeID,
	}
}

// convertToBaseType converts a PolicyUpdateType to PolicyChangeType.
func (d *DistributedPEP) convertToBaseType(t PolicyUpdateType) PolicyChangeType {
	switch t {
	case PolicyUpdateCreated:
		return PolicyChangeCreated
	case PolicyUpdateUpdated:
		return PolicyChangeUpdated
	case PolicyUpdateDeleted:
		return PolicyChangeDeleted
	case PolicyUpdateSnapshot:
		return PolicyChangeSnapshot
	default:
		return PolicyChangeUpdated
	}
}

// DistributedPEPStats represents distributed PEP statistics.
type DistributedPEPStats struct {
	// PEPStats are the base PEP statistics.
	PEPStats

	// SyncStatus is the current synchronization status.
	SyncStatus SyncStatus `json:"sync_status"`

	// EnhancedStats are enhanced synchronization statistics.
	EnhancedStats EnhancedSyncStats `json:"enhanced_stats"`

	// SyncInProgress indicates if a sync is currently in progress.
	SyncInProgress bool `json:"sync_in_progress"`

	// LastSyncError is the last sync error message (if any).
	LastSyncError string `json:"last_sync_error,omitempty"`
}
