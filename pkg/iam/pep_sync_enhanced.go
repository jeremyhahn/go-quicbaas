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
	"time"

	"github.com/google/uuid"
)

// EnhancedRaftCacheSyncer extends RaftCacheSyncer with incremental sync and batch processing.
type EnhancedRaftCacheSyncer struct {
	// Base syncer for core functionality
	*RaftCacheSyncer

	// config is the enhanced sync configuration.
	enhancedConfig *EnhancedCacheSyncConfig

	// deltaLog tracks incremental changes.
	deltaLog *DeltaLog

	// batchProcessor batches changes for efficient propagation.
	batchProcessor *BatchProcessor

	// versionTracker tracks versions with per-policy granularity.
	versionTracker *EnhancedCacheVersionTracker

	// enhancedMetrics collects enhanced metrics.
	enhancedMetrics *EnhancedCacheSyncMetrics

	// conflictResolutionStrategy determines how to handle version conflicts.
	conflictResolutionStrategy ConflictResolutionStrategy

	// mu protects concurrent access to syncer state.
	enhancedMu sync.RWMutex
}

// ConflictResolutionStrategy defines how to handle version conflicts.
type ConflictResolutionStrategy int

const (
	// ConflictResolutionLastWrite uses last-write-wins strategy.
	ConflictResolutionLastWrite ConflictResolutionStrategy = iota

	// ConflictResolutionHighestVersion uses highest version wins.
	ConflictResolutionHighestVersion

	// ConflictResolutionReject rejects conflicting updates.
	ConflictResolutionReject
)

// EnhancedRaftSyncerOption is a functional option for configuring EnhancedRaftCacheSyncer.
type EnhancedRaftSyncerOption func(*EnhancedRaftCacheSyncer)

// WithConflictResolution sets the conflict resolution strategy.
func WithConflictResolution(strategy ConflictResolutionStrategy) EnhancedRaftSyncerOption {
	return func(s *EnhancedRaftCacheSyncer) {
		s.conflictResolutionStrategy = strategy
	}
}

// WithEnhancedVersionTracker sets a custom enhanced version tracker.
func WithEnhancedVersionTracker(tracker *EnhancedCacheVersionTracker) EnhancedRaftSyncerOption {
	return func(s *EnhancedRaftCacheSyncer) {
		s.versionTracker = tracker
	}
}

// NewEnhancedRaftCacheSyncer creates a new enhanced Raft-based cache syncer.
func NewEnhancedRaftCacheSyncer(
	config *EnhancedCacheSyncConfig,
	raft RaftProposer,
	cache CacheSyncInterface,
	opts ...EnhancedRaftSyncerOption,
) (*EnhancedRaftCacheSyncer, error) {
	if config == nil {
		return nil, &InvalidSyncConfigError{Reason: "config is nil"}
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Create base syncer with standard options
	baseOpts := []RaftSyncerOption{
		WithShardID(1), // Default IAM shard
		WithCompiler(NewPolicyCompiler()),
	}

	baseSyncer, err := NewRaftCacheSyncer(config.CacheSyncConfig, raft, cache, baseOpts...)
	if err != nil {
		return nil, &BaseSyncerCreationError{Cause: err}
	}

	syncer := &EnhancedRaftCacheSyncer{
		RaftCacheSyncer:            baseSyncer,
		enhancedConfig:             config,
		deltaLog:                   NewDeltaLog(config.DeltaLogMaxEntries),
		versionTracker:             NewEnhancedCacheVersionTracker(),
		enhancedMetrics:            NewEnhancedCacheSyncMetrics(),
		conflictResolutionStrategy: ConflictResolutionHighestVersion,
	}

	// Apply options
	for _, opt := range opts {
		opt(syncer)
	}

	// Create batch processor if batching is enabled
	if config.BatchSize > 0 {
		syncer.batchProcessor, err = NewBatchProcessor(config, syncer)
		if err != nil {
			return nil, &BatchProcessorCreationError{Cause: err}
		}
	}

	return syncer, nil
}

// NotifyPolicyChange broadcasts a policy change using batch processing if enabled.
func (s *EnhancedRaftCacheSyncer) NotifyPolicyChange(ctx context.Context, event PolicyChangeEvent) error {
	// Convert to enhanced event
	enhancedEvent := &EnhancedPolicyChangeEvent{
		EventID:    uuid.New().String(),
		Type:       s.convertToEnhancedType(event.Type),
		TenantID:   event.TenantID,
		PolicyName: event.PolicyID,
		Version:    event.Version,
		Timestamp:  event.Timestamp,
		NodeID:     event.NodeID,
	}

	// Deserialize policy data if present
	if len(event.Data) > 0 {
		var policy Policy
		if err := json.Unmarshal(event.Data, &policy); err == nil {
			enhancedEvent.Policy = &policy
		}
	}

	// Add to delta log
	if err := s.deltaLog.Append(enhancedEvent); err != nil {
		return &DeltaLogAppendError{Cause: err}
	}

	// Update version tracker
	s.versionTracker.SetPolicyVersion(event.TenantID, event.PolicyID, event.Version)

	// If batch processing is enabled, add to batch
	if s.batchProcessor != nil && s.enhancedConfig.BatchSize > 1 {
		return s.batchProcessor.Add(enhancedEvent)
	}

	// Otherwise, use base syncer to notify immediately
	return s.RaftCacheSyncer.NotifyPolicyChange(ctx, event)
}

// SyncIncremental performs an incremental sync from a specific version.
func (s *EnhancedRaftCacheSyncer) SyncIncremental(ctx context.Context, tenantID string, sinceVersion int64) error {
	if s.closed.Load() {
		return ErrSyncerClosed
	}

	if !s.enhancedConfig.EnableIncrementalSync {
		return &IncrementalSyncDisabledError{}
	}

	startTime := time.Now()

	// Get changes since the specified version
	events, err := s.deltaLog.GetSince(tenantID, sinceVersion)
	if err != nil {
		// If we can't fulfill the incremental sync, fall back to full sync
		s.recordError("incremental_sync_fallback")
		return s.SyncFromLeader(ctx)
	}

	if len(events) == 0 {
		// No changes to sync
		return nil
	}

	// Apply each change
	for _, event := range events {
		if err := s.applyChange(ctx, event); err != nil {
			s.recordError("apply_change")
			// Continue with other changes
		}
	}

	// Update tracking
	if len(events) > 0 {
		lastEvent := events[len(events)-1]
		s.versionTracker.SetTenantVersion(tenantID, lastEvent.Version)
		s.lastSyncTime.Store(time.Now().Unix())
	}

	// Record metrics
	s.recordLatency("incremental_sync", time.Since(startTime))
	s.enhancedMetrics.RecordIncrementalSync()

	return nil
}

// applyChange applies a single policy change to the local cache.
func (s *EnhancedRaftCacheSyncer) applyChange(ctx context.Context, event *EnhancedPolicyChangeEvent) error {
	if event == nil {
		return &NilEventError{}
	}

	// Check for version conflicts
	currentVersion := s.versionTracker.GetPolicyVersion(event.TenantID, event.PolicyName)
	if currentVersion > 0 && event.Version <= currentVersion {
		// Handle conflict based on strategy
		switch s.conflictResolutionStrategy {
		case ConflictResolutionLastWrite:
			// Accept the change regardless of version
		case ConflictResolutionHighestVersion:
			// Skip if current version is higher
			if event.Version < currentVersion {
				return nil
			}
		case ConflictResolutionReject:
			// Reject the change
			return &VersionConflictError{Current: currentVersion, Incoming: event.Version}
		}
	}

	switch event.Type {
	case PolicyUpdateCreated, PolicyUpdateUpdated:
		if event.Policy == nil && event.CompiledPolicy == nil {
			return &MissingPolicyDataError{EventType: event.Type.String()}
		}

		var compiled *CompiledPolicy
		var err error

		// Use compiled policy if available, otherwise compile
		if event.CompiledPolicy != nil {
			compiled = event.CompiledPolicy
		} else if event.Policy != nil {
			compiled, err = s.compiler.Compile(event.Policy)
			if err != nil {
				return &PolicyCompileFailedError{Cause: err}
			}
		}

		// Store in cache
		s.cache.Set(event.TenantID, compiled)

	case PolicyUpdateDeleted:
		// Invalidate the policy
		s.cache.Invalidate(event.TenantID, event.PolicyName)

	case PolicyUpdateSnapshot:
		// Full snapshot handled by base syncer
		return nil

	case PolicyUpdateBatch:
		// Batch updates are handled separately
		return nil
	}

	// Update version tracker
	s.versionTracker.SetPolicyVersion(event.TenantID, event.PolicyName, event.Version)

	return nil
}

// SyncFromLeader performs a full sync from the leader node.
func (s *EnhancedRaftCacheSyncer) SyncFromLeader(ctx context.Context) error {
	// Use base syncer for full sync
	if err := s.RaftCacheSyncer.SyncFromLeader(ctx); err != nil {
		return err
	}

	// Record metrics
	s.enhancedMetrics.RecordFullSync()

	return nil
}

// GetNodeVersion returns the current cache version for a node.
func (s *EnhancedRaftCacheSyncer) GetNodeVersion(tenantID string) int64 {
	return s.versionTracker.GetTenantVersion(tenantID)
}

// GetEnhancedStats returns enhanced synchronization statistics.
func (s *EnhancedRaftCacheSyncer) GetEnhancedStats() *EnhancedSyncStats {
	stats := s.enhancedMetrics.GetStats()
	stats.DeltaLogSize = int64(s.deltaLog.Size())
	return stats
}

// Close closes the enhanced syncer and releases resources.
func (s *EnhancedRaftCacheSyncer) Close() error {
	// Close batch processor if present
	if s.batchProcessor != nil {
		if err := s.batchProcessor.Close(); err != nil {
			return err
		}
	}

	// Close base syncer
	return s.RaftCacheSyncer.Close()
}

// convertToEnhancedType converts a PolicyChangeType to PolicyUpdateType.
func (s *EnhancedRaftCacheSyncer) convertToEnhancedType(t PolicyChangeType) PolicyUpdateType {
	switch t {
	case PolicyChangeCreated:
		return PolicyUpdateCreated
	case PolicyChangeUpdated:
		return PolicyUpdateUpdated
	case PolicyChangeDeleted:
		return PolicyUpdateDeleted
	case PolicyChangeSnapshot:
		return PolicyUpdateSnapshot
	default:
		return PolicyUpdateUpdated
	}
}

// Verify EnhancedRaftCacheSyncer implements EnhancedPolicyCacheSyncer.
var _ EnhancedPolicyCacheSyncer = (*EnhancedRaftCacheSyncer)(nil)
