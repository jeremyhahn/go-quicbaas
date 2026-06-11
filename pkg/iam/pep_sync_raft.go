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
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// RaftProposer defines the interface for proposing changes via Raft.
type RaftProposer interface {
	// Propose submits a proposal to the Raft cluster.
	Propose(ctx context.Context, shardID uint64, data []byte) error

	// Read performs a linearizable read from the Raft cluster.
	Read(ctx context.Context, shardID uint64, query interface{}) (interface{}, error)

	// IsLeader returns true if this node is the leader for the shard.
	IsLeader(shardID uint64) bool

	// GetLeaderID returns the leader node ID for the shard.
	GetLeaderID(shardID uint64) (uint64, bool)
}

// RaftCacheSyncer implements PolicyCacheSyncer using Raft for distributed coordination.
type RaftCacheSyncer struct {
	config   *CacheSyncConfig
	raft     RaftProposer
	cache    CacheSyncInterface
	compiler *PolicyCompiler
	tracker  *CacheVersionTracker
	metrics  *CacheSyncMetrics

	// handler is the current policy change handler.
	handler PolicyChangeHandler

	// subscribed indicates if we are subscribed to changes.
	subscribed atomic.Bool

	// closed indicates if the syncer is closed.
	closed atomic.Bool

	// stopCh signals the background goroutine to stop.
	stopCh chan struct{}

	// eventCh receives policy change events from Raft log.
	eventCh chan PolicyChangeEvent

	// wg tracks running goroutines.
	wg sync.WaitGroup

	// mu protects handler updates.
	mu sync.RWMutex

	// shardID is the Raft shard for policy sync.
	shardID uint64

	// leaderNodeID caches the current leader node ID.
	leaderNodeID atomic.Value // string

	// lastSyncTime is the timestamp of the last successful sync.
	lastSyncTime atomic.Int64

	// syncLatencySum tracks total sync latency for averaging.
	syncLatencySum atomic.Int64

	// syncCount tracks number of syncs for averaging.
	syncCount atomic.Int64
}

// RaftSyncerOption is a functional option for configuring RaftCacheSyncer.
type RaftSyncerOption func(*RaftCacheSyncer)

// WithShardID sets the Raft shard ID for policy synchronization.
func WithShardID(shardID uint64) RaftSyncerOption {
	return func(s *RaftCacheSyncer) {
		s.shardID = shardID
	}
}

// WithCompiler sets the policy compiler for deserializing policies.
func WithCompiler(compiler *PolicyCompiler) RaftSyncerOption {
	return func(s *RaftCacheSyncer) {
		s.compiler = compiler
	}
}

// WithVersionTracker sets a custom version tracker.
func WithVersionTracker(tracker *CacheVersionTracker) RaftSyncerOption {
	return func(s *RaftCacheSyncer) {
		s.tracker = tracker
	}
}

// NewRaftCacheSyncer creates a new Raft-based cache syncer.
func NewRaftCacheSyncer(
	config *CacheSyncConfig,
	raft RaftProposer,
	cache CacheSyncInterface,
	opts ...RaftSyncerOption,
) (*RaftCacheSyncer, error) {
	if config == nil {
		return nil, &InvalidSyncConfigError{Reason: "config is nil"}
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if raft == nil {
		return nil, &InvalidSyncConfigError{Reason: "raft proposer is nil"}
	}
	if cache == nil {
		return nil, &InvalidSyncConfigError{Reason: "cache is nil"}
	}

	syncer := &RaftCacheSyncer{
		config:   config,
		raft:     raft,
		cache:    cache,
		compiler: NewPolicyCompiler(),
		tracker:  NewCacheVersionTracker(),
		stopCh:   make(chan struct{}),
		eventCh:  make(chan PolicyChangeEvent, config.BufferSize),
		shardID:  1, // Default shard ID for IAM
	}

	// Apply options
	for _, opt := range opts {
		opt(syncer)
	}

	// Initialize metrics if enabled
	if config.EnableMetrics {
		syncer.metrics = initCacheSyncMetrics()
	}

	// Register invalidation callback with tracker
	syncer.tracker.RegisterInvalidationCallback(syncer.handleInvalidation)

	return syncer, nil
}

// NotifyPolicyChange broadcasts a policy change to all nodes via Raft.
func (s *RaftCacheSyncer) NotifyPolicyChange(ctx context.Context, event PolicyChangeEvent) error {
	if s.closed.Load() {
		return ErrSyncerClosed
	}

	startTime := time.Now()

	// Set node ID and timestamp if not set
	if event.NodeID == "" {
		event.NodeID = s.config.NodeID
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Serialize the event
	data, err := event.MarshalBinary()
	if err != nil {
		s.recordError("notify")
		return &SyncFailedError{
			Operation: "serialize event",
			Cause:     err,
		}
	}

	// Wrap in a sync command envelope
	cmd := &syncCommand{
		Type: syncCmdPolicyChange,
		Data: data,
	}

	cmdData, err := json.Marshal(cmd)
	if err != nil {
		s.recordError("notify")
		return &SyncFailedError{
			Operation: "serialize command",
			Cause:     err,
		}
	}

	// Propose via Raft with retries
	var lastErr error
	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.config.RetryInterval):
			}
		}

		if err := s.raft.Propose(ctx, s.shardID, cmdData); err != nil {
			lastErr = err
			continue
		}

		// Success
		s.recordLatency("notify", time.Since(startTime))
		s.recordEventSent(event.Type)
		return nil
	}

	s.recordError("notify")
	return &SyncFailedError{
		Operation: "propose via raft",
		Cause:     lastErr,
	}
}

// Subscribe starts listening for policy changes from other nodes.
func (s *RaftCacheSyncer) Subscribe(ctx context.Context, handler PolicyChangeHandler) error {
	if s.closed.Load() {
		return ErrSyncerClosed
	}
	if handler == nil {
		return &InvalidSyncConfigError{Reason: "handler is nil"}
	}
	if !s.subscribed.CompareAndSwap(false, true) {
		return ErrAlreadySubscribed
	}

	s.mu.Lock()
	s.handler = handler
	s.mu.Unlock()

	// Start event processing goroutine
	s.wg.Add(1)
	go s.processEvents(ctx)

	// Start periodic sync goroutine
	s.wg.Add(1)
	go s.periodicSync(ctx)

	if s.metrics != nil {
		s.metrics.subscriberCount.Inc()
	}

	return nil
}

// Unsubscribe stops listening for policy changes.
func (s *RaftCacheSyncer) Unsubscribe(ctx context.Context) error {
	if s.closed.Load() {
		return ErrSyncerClosed
	}
	if !s.subscribed.CompareAndSwap(true, false) {
		return ErrNotSubscribed
	}

	s.mu.Lock()
	s.handler = nil
	s.mu.Unlock()

	if s.metrics != nil {
		s.metrics.subscriberCount.Dec()
	}

	return nil
}

// SyncFromLeader performs a full sync from the leader node.
func (s *RaftCacheSyncer) SyncFromLeader(ctx context.Context) error {
	if s.closed.Load() {
		return ErrSyncerClosed
	}

	startTime := time.Now()

	// Query leader for all policies
	query := &syncQuery{
		Type:     syncQueryFullSnapshot,
		TenantID: s.config.TenantID,
		Version:  s.tracker.GetTenantVersion(s.config.TenantID),
	}

	result, err := s.raft.Read(ctx, s.shardID, query)
	if err != nil {
		s.recordError("sync_from_leader")
		return &SyncFailedError{
			Operation: "read from leader",
			Cause:     err,
		}
	}

	// Process the snapshot response
	snapshot, ok := result.(*syncSnapshot)
	if !ok {
		s.recordError("sync_from_leader")
		return &SyncFailedError{
			Operation: "process snapshot",
			Cause:     &UnexpectedResultTypeError{ExpectedType: "*syncSnapshot", ActualType: "unknown"},
		}
	}

	// Apply the snapshot to the cache
	if err := s.applySnapshot(ctx, snapshot); err != nil {
		s.recordError("sync_from_leader")
		return err
	}

	// Update tracking
	s.lastSyncTime.Store(time.Now().Unix())
	s.tracker.SetTenantVersion(s.config.TenantID, snapshot.Version)

	// Update leader node ID
	if leaderID, ok := s.raft.GetLeaderID(s.shardID); ok {
		s.leaderNodeID.Store(strconv.FormatUint(leaderID, 10))
	}

	s.recordLatency("sync_from_leader", time.Since(startTime))
	return nil
}

// GetSyncStatus returns the current synchronization status.
func (s *RaftCacheSyncer) GetSyncStatus() *SyncStatus {
	var leaderID string
	if v := s.leaderNodeID.Load(); v != nil {
		leaderID = v.(string) //nolint:errcheck // type assertion guaranteed by atomic.Value contract
	}

	var avgLatency int64
	if count := s.syncCount.Load(); count > 0 {
		avgLatency = s.syncLatencySum.Load() / count
	}

	return &SyncStatus{
		IsSubscribed:    s.subscribed.Load(),
		LastSyncTime:    time.Unix(s.lastSyncTime.Load(), 0),
		LastSyncVersion: s.tracker.GetTenantVersion(s.config.TenantID),
		SyncErrors:      s.getSyncErrors(),
		EventsProcessed: s.getEventsProcessed(),
		LeaderNodeID:    leaderID,
		SyncLatencyNs:   avgLatency,
	}
}

// Close closes the syncer and releases resources.
func (s *RaftCacheSyncer) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	// Signal goroutines to stop
	close(s.stopCh)

	// Unsubscribe if subscribed
	if s.subscribed.Load() {
		s.subscribed.Store(false)
		s.mu.Lock()
		s.handler = nil
		s.mu.Unlock()
		if s.metrics != nil {
			s.metrics.subscriberCount.Dec()
		}
	}

	// Wait for goroutines to finish
	s.wg.Wait()

	// Clear callbacks
	s.tracker.ClearCallbacks()

	// Close event channel
	close(s.eventCh)

	return nil
}

// DeliverEvent delivers an event from the Raft log to the syncer.
// This is called by the state machine when it commits a policy change.
func (s *RaftCacheSyncer) DeliverEvent(event PolicyChangeEvent) {
	if s.closed.Load() || !s.subscribed.Load() {
		return
	}

	select {
	case s.eventCh <- event:
		// Event delivered
	default:
		// Buffer full, drop event (will be recovered on next sync)
		s.recordError("deliver_event")
	}
}

// processEvents processes policy change events from the event channel.
func (s *RaftCacheSyncer) processEvents(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-s.stopCh:
			return

		case <-ctx.Done():
			return

		case event, ok := <-s.eventCh:
			if !ok {
				return
			}

			// Skip events from this node (we already applied them locally)
			if event.NodeID == s.config.NodeID {
				continue
			}

			// Skip events for other tenants
			if event.TenantID != s.config.TenantID {
				continue
			}

			// Process the event
			if err := s.processEvent(ctx, event); err != nil {
				s.recordError("process_event")
				// Continue processing other events
			}
		}
	}
}

// processEvent processes a single policy change event.
func (s *RaftCacheSyncer) processEvent(ctx context.Context, event PolicyChangeEvent) error {
	startTime := time.Now()

	// Apply to local cache first
	if err := s.applyEventToCache(event); err != nil {
		return err
	}

	// Invoke handler if set
	s.mu.RLock()
	handler := s.handler
	s.mu.RUnlock()

	if handler != nil {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}

	// Update tracking
	s.tracker.SetTenantVersion(event.TenantID, event.Version)
	s.recordLatency("process_event", time.Since(startTime))
	s.recordEventReceived(event.Type)

	return nil
}

// applyEventToCache applies a policy change event to the local cache.
func (s *RaftCacheSyncer) applyEventToCache(event PolicyChangeEvent) error {
	switch event.Type {
	case PolicyChangeCreated, PolicyChangeUpdated:
		if len(event.Data) == 0 {
			return &SyncFailedError{
				Operation: "apply event",
				Cause:     &MissingPolicyDataError{EventType: string(event.Type.String())},
			}
		}

		// Deserialize the policy
		var policy Policy
		if err := json.Unmarshal(event.Data, &policy); err != nil {
			return &SyncFailedError{
				Operation: "deserialize policy",
				Cause:     err,
			}
		}

		// Compile the policy
		compiled, err := s.compiler.Compile(&policy)
		if err != nil {
			return &SyncFailedError{
				Operation: "compile policy",
				Cause:     err,
			}
		}

		// Store in cache
		s.cache.Set(event.TenantID, compiled)

	case PolicyChangeDeleted:
		// Invalidate the policy
		s.cache.Invalidate(event.TenantID, event.PolicyID)

	case PolicyChangeSnapshot:
		// Snapshot events are handled separately via SyncFromLeader
		// For now, just invalidate all and let next sync restore
		s.cache.InvalidateAll(event.TenantID)
	}

	// Notify tracker
	s.tracker.NotifyInvalidation(event.TenantID, event.PolicyID, event.Version)

	return nil
}

// applySnapshot applies a full snapshot to the cache.
func (s *RaftCacheSyncer) applySnapshot(ctx context.Context, snapshot *syncSnapshot) error {
	if snapshot == nil {
		return &SyncFailedError{
			Operation: "apply snapshot",
			Cause:     &NilSnapshotError{},
		}
	}

	// Clear existing cache for tenant
	s.cache.InvalidateAll(s.config.TenantID)

	// Apply all policies from snapshot
	for _, policyData := range snapshot.Policies {
		var policy Policy
		if err := json.Unmarshal(policyData, &policy); err != nil {
			continue // Skip invalid policies
		}

		compiled, err := s.compiler.Compile(&policy)
		if err != nil {
			continue // Skip policies that fail to compile
		}

		s.cache.Set(s.config.TenantID, compiled)
	}

	return nil
}

// periodicSync performs periodic synchronization from the leader.
func (s *RaftCacheSyncer) periodicSync(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return

		case <-ctx.Done():
			return

		case <-ticker.C:
			// Only sync if we're not the leader
			if s.raft.IsLeader(s.shardID) {
				continue
			}

			// Perform sync with timeout
			syncCtx, cancel := context.WithTimeout(ctx, s.config.SyncInterval/2)
			if err := s.SyncFromLeader(syncCtx); err != nil {
				s.recordError("periodic_sync")
			}
			cancel()
		}
	}
}

// handleInvalidation is called when a cache entry should be invalidated.
func (s *RaftCacheSyncer) handleInvalidation(tenantID, policyID string, version int64) {
	if s.metrics != nil {
		s.metrics.cacheVersion.Set(float64(version))
	}
}

// recordLatency records sync operation latency.
func (s *RaftCacheSyncer) recordLatency(operation string, duration time.Duration) {
	latencyNs := duration.Nanoseconds()
	s.syncLatencySum.Add(latencyNs)
	s.syncCount.Add(1)

	if s.metrics != nil {
		s.metrics.syncLatency.With(s.config.TenantID, operation).Observe(duration.Seconds())
	}
}

// recordError records a sync error.
func (s *RaftCacheSyncer) recordError(operation string) {
	if s.metrics != nil {
		s.metrics.syncErrors.With(s.config.TenantID, operation).Inc()
		s.metrics.syncErrorCount.Add(1)
	}
}

// recordEventReceived records a received event.
func (s *RaftCacheSyncer) recordEventReceived(eventType PolicyChangeType) {
	if s.metrics != nil {
		s.metrics.eventsReceived.With(s.config.TenantID, eventType.String()).Inc()
		s.metrics.eventsProcessed.Add(1)
	}
}

// recordEventSent records a sent event.
func (s *RaftCacheSyncer) recordEventSent(eventType PolicyChangeType) {
	if s.metrics != nil {
		s.metrics.eventsSent.With(s.config.TenantID, eventType.String()).Inc()
	}
}

// getSyncErrors returns the sync error count.
func (s *RaftCacheSyncer) getSyncErrors() int64 {
	if s.metrics != nil {
		return s.metrics.syncErrorCount.Load()
	}
	return 0
}

// getEventsProcessed returns the events processed count.
func (s *RaftCacheSyncer) getEventsProcessed() int64 {
	if s.metrics != nil {
		return s.metrics.eventsProcessed.Load()
	}
	return 0
}

// syncCommandType represents the type of sync command.
type syncCommandType int

const (
	syncCmdPolicyChange syncCommandType = iota
	syncCmdFullSync
)

// syncCommand wraps a sync operation for Raft proposal.
type syncCommand struct {
	Type syncCommandType `json:"type"`
	Data []byte          `json:"data"`
}

// syncQueryType represents the type of sync query.
type syncQueryType int

const (
	syncQueryFullSnapshot syncQueryType = iota
	syncQueryDelta
)

// syncQuery represents a sync query to the leader.
type syncQuery struct {
	Type     syncQueryType `json:"type"`
	TenantID string        `json:"tenant_id"`
	Version  int64         `json:"version"` // For delta queries
}

// syncSnapshot represents a full snapshot response.
type syncSnapshot struct {
	TenantID string   `json:"tenant_id"`
	Version  int64    `json:"version"`
	Policies [][]byte `json:"policies"` // Serialized policies
}

// Verify RaftCacheSyncer implements PolicyCacheSyncer.
var _ PolicyCacheSyncer = (*RaftCacheSyncer)(nil)
