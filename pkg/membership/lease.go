//go:build raft

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

package membership

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

const (
	// MembershipPrefix is the key prefix for cluster membership.
	MembershipPrefix = "/_cluster/members/"

	// DefaultLeaseTTL is the default TTL for member registrations (10 seconds).
	DefaultLeaseTTL = 10 * time.Second

	// DefaultKeepAliveInterval is how often to renew the lease (5 seconds).
	DefaultKeepAliveInterval = 5 * time.Second

	// DefaultHealthCheckInterval is how often to check member health.
	DefaultHealthCheckInterval = 15 * time.Second

	// DefaultMaxRetries is the default maximum retries for Raft operations.
	DefaultMaxRetries = 3

	// DefaultRetryBaseDelay is the base delay for exponential backoff.
	DefaultRetryBaseDelay = 100 * time.Millisecond
)

// MembershipError represents a membership validation or operational error.
type MembershipError struct {
	Message string
}

// Error implements the error interface.
func (e *MembershipError) Error() string {
	return e.Message
}

// MembershipCauseError represents a membership error with an underlying cause.
type MembershipCauseError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *MembershipCauseError) Error() string {
	return e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *MembershipCauseError) Unwrap() error {
	return e.Cause
}

// Member represents a cluster member.
type Member struct {
	// NodeID is the unique identifier for this node
	NodeID uint64 `json:"node_id"`

	// Address is the host:port for this node's Raft communication
	Address string `json:"address"`

	// RaftAddr is an alias for Address (for compatibility)
	RaftAddr string `json:"raft_addr"`

	// RegisteredAt is when this member registered
	RegisteredAt time.Time `json:"registered_at"`

	// LastSeen is when this member last renewed their lease
	LastSeen time.Time `json:"last_seen"`

	// Metadata contains optional key-value pairs about this member
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Manager manages cluster membership using watch-based lease registration.
type Manager struct {
	store  storage.Storage
	watch  *watch.Manager
	logger *logger.Logger
	self   Member

	// Raft integration (optional)
	raftNode RaftNode
	shardID  uint64
	enabled  bool // Raft integration enabled

	// Configuration
	leaseTTL            time.Duration
	keepAliveInterval   time.Duration
	healthCheckInterval time.Duration
	maxRetries          int
	retryBaseDelay      time.Duration

	// State
	members      map[uint64]Member
	membersMu    sync.RWMutex
	leaseCtx     context.Context
	leaseCancel  context.CancelFunc
	watchCtx     context.Context
	watchCancel  context.CancelFunc
	healthCtx    context.Context
	healthCancel context.CancelFunc
	closeChan    chan struct{}
	closeOnce    sync.Once
	closed       bool
	mu           sync.RWMutex
	watchStarted bool

	// Metrics
	metrics *membershipMetrics
}

// membershipMetrics tracks membership operations.
type membershipMetrics struct {
	memberAdd      *metrics.CounterVec
	memberRemove   *metrics.CounterVec
	memberCount    *metrics.Gauge
	leaseRenewals  *metrics.CounterVec
	healthChecks   *metrics.CounterVec
	raftOperations *metrics.CounterVec
	raftOpDuration *metrics.HistogramVec
}

// Config contains configuration for the membership manager.
type Config struct {
	// LeaseTTL is how long a member registration lasts without renewal
	LeaseTTL time.Duration

	// KeepAliveInterval is how often to renew the lease
	KeepAliveInterval time.Duration

	// HealthCheckInterval is how often to check member health
	HealthCheckInterval time.Duration

	// MaxRetries is the maximum number of retries for Raft operations
	MaxRetries int

	// RetryBaseDelay is the base delay for exponential backoff
	RetryBaseDelay time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		LeaseTTL:            DefaultLeaseTTL,
		KeepAliveInterval:   DefaultKeepAliveInterval,
		HealthCheckInterval: DefaultHealthCheckInterval,
		MaxRetries:          DefaultMaxRetries,
		RetryBaseDelay:      DefaultRetryBaseDelay,
	}
}

// Option is a functional option for configuring the Manager.
type Option func(*Manager)

// WithRaftIntegration enables Raft dynamic membership integration.
func WithRaftIntegration(raftNode RaftNode, shardID uint64) Option {
	return func(m *Manager) {
		m.raftNode = raftNode
		m.shardID = shardID
		m.enabled = true
	}
}

// NewManager creates a new membership manager with optional Raft integration.
func NewManager(
	store storage.Storage,
	watchMgr *watch.Manager,
	self discovery.Peer,
	config *Config,
	log *logger.Logger,
	opts ...Option,
) (*Manager, error) {
	if store == nil {
		return nil, &MembershipError{Message: "storage cannot be nil"}
	}

	if watchMgr == nil {
		return nil, &MembershipError{Message: "watch manager cannot be nil"}
	}

	if log == nil {
		return nil, &MembershipError{Message: "logger cannot be nil"}
	}

	if config == nil {
		config = DefaultConfig()
	}

	// Validate configuration
	if config.LeaseTTL < time.Second {
		return nil, &MembershipError{Message: "lease TTL must be at least 1 second"}
	}

	if config.KeepAliveInterval < time.Second {
		return nil, &MembershipError{Message: "keep-alive interval must be at least 1 second"}
	}

	if config.KeepAliveInterval >= config.LeaseTTL {
		return nil, &MembershipError{Message: "keep-alive interval must be less than lease TTL"}
	}

	if config.HealthCheckInterval < time.Second {
		return nil, &MembershipError{Message: "health check interval must be at least 1 second"}
	}

	if config.MaxRetries < 1 {
		return nil, &MembershipError{Message: "max retries must be at least 1"}
	}

	if config.RetryBaseDelay < time.Millisecond {
		return nil, &MembershipError{Message: "retry base delay must be at least 1 millisecond"}
	}

	// Convert discovery.Peer to Member
	member := Member{
		NodeID:       self.NodeID,
		Address:      self.Address,
		RaftAddr:     self.RaftAddr,
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
		Metadata:     self.Metadata,
	}

	if member.RaftAddr == "" {
		member.RaftAddr = member.Address
	}

	m := &Manager{
		store:               store,
		watch:               watchMgr,
		logger:              log.WithField("component", "membership"),
		self:                member,
		leaseTTL:            config.LeaseTTL,
		keepAliveInterval:   config.KeepAliveInterval,
		healthCheckInterval: config.HealthCheckInterval,
		maxRetries:          config.MaxRetries,
		retryBaseDelay:      config.RetryBaseDelay,
		members:             make(map[uint64]Member),
		closeChan:           make(chan struct{}),
	}

	// Apply functional options
	for _, opt := range opts {
		opt(m)
	}

	// Initialize metrics
	m.initMetrics()

	if m.enabled {
		m.logger.WithFields(map[string]interface{}{
			"shard_id": m.shardID,
			"node_id":  m.self.NodeID,
		}).Info("Membership manager initialized with Raft integration")
	} else {
		m.logger.Info("Membership manager initialized without Raft integration")
	}

	return m, nil
}

// initMetrics initializes Prometheus metrics for membership operations.
func (m *Manager) initMetrics() {
	m.metrics = &membershipMetrics{
		memberAdd: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "membership",
			Name:      "member_add_total",
			Help:      "Total number of member add operations",
		}, []string{"shard", "status"}),

		memberRemove: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "membership",
			Name:      "member_remove_total",
			Help:      "Total number of member remove operations",
		}, []string{"shard", "status"}),

		memberCount: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "membership",
			Name:      "member_count",
			Help:      "Current number of registered members",
		}),

		leaseRenewals: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "membership",
			Name:      "lease_renewals_total",
			Help:      "Total number of lease renewal operations",
		}, []string{"status"}),

		healthChecks: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "membership",
			Name:      "health_checks_total",
			Help:      "Total number of health check operations",
		}, []string{"status"}),

		raftOperations: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "membership",
			Name:      "raft_operations_total",
			Help:      "Total number of Raft membership operations",
		}, []string{"shard", "operation", "status"}),

		raftOpDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "membership",
			Name:      "raft_operation_duration_seconds",
			Help:      "Duration of Raft membership operations",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"shard", "operation"}),
	}
}

// Register registers this node in the cluster membership
// This starts the lease keep-alive goroutine.
func (m *Manager) Register(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return &MembershipError{Message: "manager is closed"}
	}

	// Serialize member info
	data, err := json.Marshal(m.self)
	if err != nil {
		return &MembershipCauseError{Message: "failed to marshal member", Cause: err}
	}

	// Construct membership key
	key := fmt.Sprintf("%s%d", MembershipPrefix, m.self.NodeID)

	// Store member registration
	// Note: TTL/lease management is handled by the higher-level lease management layer
	// For now, we rely on periodic updates (keep-alive) and watch-based detection
	err = m.store.Set([]byte(key), data)
	if err != nil {
		return &MembershipCauseError{Message: "failed to register member", Cause: err}
	}

	m.logger.Infof("Registered member %d at %s", m.self.NodeID, m.self.Address)

	// Start lease keep-alive goroutine
	m.leaseCtx, m.leaseCancel = context.WithCancel(context.Background())
	go m.keepAlive()

	// Start health check goroutine if Raft integration is enabled
	if m.enabled {
		m.healthCtx, m.healthCancel = context.WithCancel(context.Background())
		go m.healthCheckLoop()
	}

	return nil
}

// keepAlive periodically renews the lease to keep this node registered.
func (m *Manager) keepAlive() {
	ticker := time.NewTicker(m.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.RLock()
			if m.closed {
				m.mu.RUnlock()
				return
			}
			m.mu.RUnlock()

			// Update LastSeen
			m.self.LastSeen = time.Now()

			// Serialize member info
			data, err := json.Marshal(m.self)
			if err != nil {
				m.logger.Errorf("Failed to marshal member for keep-alive: %v", err)
				m.metrics.leaseRenewals.With("error").Inc()
				continue
			}

			// Renew the lease by updating the entry
			key := fmt.Sprintf("%s%d", MembershipPrefix, m.self.NodeID)
			err = m.store.Set([]byte(key), data)

			if err != nil {
				m.logger.Errorf("Failed to renew lease: %v", err)
				m.metrics.leaseRenewals.With("error").Inc()
			} else {
				m.logger.Debugf("Renewed lease for member %d", m.self.NodeID)
				m.metrics.leaseRenewals.With("success").Inc()
			}

		case <-m.leaseCtx.Done():
			return

		case <-m.closeChan:
			return
		}
	}
}

// healthCheckLoop periodically checks member health and removes stale members.
func (m *Manager) healthCheckLoop() {
	ticker := time.NewTicker(m.healthCheckInterval)
	defer ticker.Stop()

	m.logger.Info("Health check loop started")

	for {
		select {
		case <-ticker.C:
			m.performHealthCheck()

		case <-m.healthCtx.Done():
			m.logger.Info("Health check loop stopped")
			return

		case <-m.closeChan:
			m.logger.Info("Health check loop stopped due to close")
			return
		}
	}
}

// performHealthCheck checks member health and removes stale members.
func (m *Manager) performHealthCheck() {
	m.membersMu.RLock()
	membersSnapshot := make([]Member, 0, len(m.members))
	for _, member := range m.members {
		membersSnapshot = append(membersSnapshot, member)
	}
	m.membersMu.RUnlock()

	now := time.Now()
	staleThreshold := now.Add(-m.leaseTTL * 2) // Consider stale if not seen in 2x TTL

	for _, member := range membersSnapshot {
		// Skip self
		if member.NodeID == m.self.NodeID {
			continue
		}

		// Check if member is stale
		if member.LastSeen.Before(staleThreshold) {
			m.logger.WithFields(map[string]interface{}{
				"node_id":   member.NodeID,
				"last_seen": member.LastSeen,
			}).Warn("Detected stale member, removing from membership")

			// Remove stale member from local cache
			m.membersMu.Lock()
			delete(m.members, member.NodeID)
			m.membersMu.Unlock()

			m.metrics.healthChecks.With("stale_removed").Inc()
		} else {
			m.metrics.healthChecks.With("healthy").Inc()
		}
	}
}

// StartWatching starts watching for membership changes
// This uses the watch system to automatically track member add/remove events.
func (m *Manager) StartWatching(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return &MembershipError{Message: "manager is closed"}
	}

	if m.watchStarted {
		return &MembershipError{Message: "watch already started"}
	}

	m.watchCtx, m.watchCancel = context.WithCancel(context.Background())
	m.watchStarted = true

	// Start watch goroutine
	go m.watchMembership()

	m.logger.Info("Started watching cluster membership")
	return nil
}

// watchMembership watches for membership changes using the watch system.
func (m *Manager) watchMembership() {
	// Create a watch for the membership prefix
	watchChan, err := m.watch.Watch(m.watchCtx, MembershipPrefix, watch.WithPrefix())
	if err != nil {
		m.logger.Errorf("Failed to create membership watch: %v", err)
		return
	}

	m.logger.Info("Membership watch started")

	for {
		select {
		case event, ok := <-watchChan:
			if !ok {
				m.logger.Info("Membership watch channel closed")
				return
			}

			m.handleWatchEvent(event)

		case <-m.watchCtx.Done():
			m.logger.Info("Membership watch context canceled")
			return

		case <-m.closeChan:
			m.logger.Info("Membership watch stopping due to close")
			return
		}
	}
}

// handleWatchEvent processes a watch event (member add/remove).
func (m *Manager) handleWatchEvent(event watch.Event) {
	// Deserialize member
	var member Member
	if err := json.Unmarshal(event.Value, &member); err != nil {
		m.logger.Errorf("Failed to unmarshal member from watch event: %v", err)
		return
	}

	switch event.Type {
	case watch.EventTypePut:
		m.handleMemberAdded(member)

	case watch.EventTypeDelete:
		m.handleMemberRemoved(member)
	}
}

// handleMemberAdded processes a member addition event.
func (m *Manager) handleMemberAdded(member Member) {
	// Check if this is a new member (not just an update)
	m.membersMu.RLock()
	_, existed := m.members[member.NodeID]
	m.membersMu.RUnlock()

	// Update local cache
	m.membersMu.Lock()
	m.members[member.NodeID] = member
	count := len(m.members)
	m.membersMu.Unlock()

	m.metrics.memberCount.Set(float64(count))

	if existed {
		m.logger.Debugf("Member %d updated: %s", member.NodeID, member.Address)
		return
	}

	m.logger.Infof("Member %d joined: %s", member.NodeID, member.Address)

	// Skip Raft integration if not enabled or if this is self
	if !m.enabled || member.NodeID == m.self.NodeID {
		return
	}

	// Check if we are the leader before triggering Raft operations
	if !m.isLeader() {
		m.logger.Debugf("Not leader, skipping Raft AddNode for member %d", member.NodeID)
		return
	}

	// Trigger Raft AddNode operation with retry
	go m.addNodeWithRetry(member)
}

// handleMemberRemoved processes a member removal event.
func (m *Manager) handleMemberRemoved(member Member) {
	// Remove from local cache
	m.membersMu.Lock()
	delete(m.members, member.NodeID)
	count := len(m.members)
	m.membersMu.Unlock()

	m.metrics.memberCount.Set(float64(count))

	m.logger.Infof("Member %d left: %s", member.NodeID, member.Address)

	// Skip Raft integration if not enabled or if this is self
	if !m.enabled || member.NodeID == m.self.NodeID {
		return
	}

	// Check if we are the leader before triggering Raft operations
	if !m.isLeader() {
		m.logger.Debugf("Not leader, skipping Raft RemoveNode for member %d", member.NodeID)
		return
	}

	// Trigger Raft RemoveNode operation with retry
	go m.removeNodeWithRetry(member)
}

// isLeader checks if this node is the Raft leader for the managed shard.
func (m *Manager) isLeader() bool {
	if m.raftNode == nil {
		return false
	}

	leaderID, _, err := m.raftNode.GetLeaderID(m.shardID)
	if err != nil {
		m.logger.Debugf("Failed to get leader ID: %v", err)
		return false
	}

	return leaderID == m.self.NodeID
}

// addNodeWithRetry attempts to add a node to Raft with exponential backoff.
func (m *Manager) addNodeWithRetry(member Member) {
	shardStr := fmt.Sprintf("%d", m.shardID)
	start := time.Now()

	for attempt := 1; attempt <= m.maxRetries; attempt++ {
		m.logger.WithFields(map[string]interface{}{
			"shard_id": m.shardID,
			"node_id":  member.NodeID,
			"address":  member.Address,
			"attempt":  attempt,
		}).Info("Adding node to Raft group")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := m.raftNode.RequestAddNode(ctx, m.shardID, member.NodeID, member.Address, 0)
		cancel()
		duration := time.Since(start).Seconds()
		m.metrics.raftOpDuration.With(shardStr, "add").Observe(duration)

		if err == nil {
			m.metrics.raftOperations.With(shardStr, "add", "success").Inc()
			m.metrics.memberAdd.With(shardStr, "success").Inc()
			m.logger.WithFields(map[string]interface{}{
				"shard_id": m.shardID,
				"node_id":  member.NodeID,
				"duration": duration,
			}).Info("Successfully added node to Raft group")
			return
		}

		m.logger.WithFields(map[string]interface{}{
			"shard_id": m.shardID,
			"node_id":  member.NodeID,
			"attempt":  attempt,
			"error":    err,
		}).Warn("Failed to add node to Raft group")

		// Exit if we've exhausted retries
		if attempt >= m.maxRetries {
			m.metrics.raftOperations.With(shardStr, "add", "error").Inc()
			m.metrics.memberAdd.With(shardStr, "error").Inc()
			m.logger.WithFields(map[string]interface{}{
				"shard_id": m.shardID,
				"node_id":  member.NodeID,
				"error":    err,
			}).Error("Failed to add node to Raft group after all retries")
			return
		}

		// Exponential backoff
		//nolint:gosec // G115: attempt is a small counter (1-maxRetries), safe to convert to uint
		delay := m.retryBaseDelay * time.Duration(1<<uint(attempt-1))
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}

		m.logger.Debugf("Retrying AddNode in %v", delay)
		select {
		case <-time.After(delay):
		case <-m.closeChan:
			return
		}
	}
}

// removeNodeWithRetry attempts to remove a node from Raft with exponential backoff.
func (m *Manager) removeNodeWithRetry(member Member) {
	shardStr := fmt.Sprintf("%d", m.shardID)
	start := time.Now()

	for attempt := 1; attempt <= m.maxRetries; attempt++ {
		m.logger.WithFields(map[string]interface{}{
			"shard_id": m.shardID,
			"node_id":  member.NodeID,
			"attempt":  attempt,
		}).Info("Removing node from Raft group")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := m.raftNode.RequestRemoveNode(ctx, m.shardID, member.NodeID, 0)
		cancel()
		duration := time.Since(start).Seconds()
		m.metrics.raftOpDuration.With(shardStr, "remove").Observe(duration)

		if err == nil {
			m.metrics.raftOperations.With(shardStr, "remove", "success").Inc()
			m.metrics.memberRemove.With(shardStr, "success").Inc()
			m.logger.WithFields(map[string]interface{}{
				"shard_id": m.shardID,
				"node_id":  member.NodeID,
				"duration": duration,
			}).Info("Successfully removed node from Raft group")
			return
		}

		m.logger.WithFields(map[string]interface{}{
			"shard_id": m.shardID,
			"node_id":  member.NodeID,
			"attempt":  attempt,
			"error":    err,
		}).Warn("Failed to remove node from Raft group")

		// Exit if we've exhausted retries
		if attempt >= m.maxRetries {
			m.metrics.raftOperations.With(shardStr, "remove", "error").Inc()
			m.metrics.memberRemove.With(shardStr, "error").Inc()
			m.logger.WithFields(map[string]interface{}{
				"shard_id": m.shardID,
				"node_id":  member.NodeID,
				"error":    err,
			}).Error("Failed to remove node from Raft group after all retries")
			return
		}

		// Exponential backoff
		//nolint:gosec // G115: attempt is a small counter (1-maxRetries), safe to convert to uint
		delay := m.retryBaseDelay * time.Duration(1<<uint(attempt-1))
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}

		m.logger.Debugf("Retrying RemoveNode in %v", delay)
		select {
		case <-time.After(delay):
		case <-m.closeChan:
			return
		}
	}
}

// GetMembers returns the current list of cluster members.
func (m *Manager) GetMembers() []Member {
	m.membersMu.RLock()
	defer m.membersMu.RUnlock()

	members := make([]Member, 0, len(m.members))
	for _, member := range m.members {
		members = append(members, member)
	}
	return members
}

// GetMember returns a specific member by node ID.
func (m *Manager) GetMember(nodeID uint64) (Member, bool) {
	m.membersMu.RLock()
	defer m.membersMu.RUnlock()

	member, ok := m.members[nodeID]
	return member, ok
}

// GetSelf returns this node's member information.
func (m *Manager) GetSelf() Member {
	return m.self
}

// IsRaftEnabled returns whether Raft integration is enabled.
func (m *Manager) IsRaftEnabled() bool {
	return m.enabled
}

// Close gracefully shuts down the membership manager.
func (m *Manager) Close() error {
	var closeErr error

	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()

		close(m.closeChan)

		// Cancel keep-alive
		if m.leaseCancel != nil {
			m.leaseCancel()
		}

		// Cancel watch
		if m.watchCancel != nil {
			m.watchCancel()
		}

		// Cancel health check
		if m.healthCancel != nil {
			m.healthCancel()
		}

		// Deregister from cluster
		key := fmt.Sprintf("%s%d", MembershipPrefix, m.self.NodeID)
		if err := m.store.Delete([]byte(key)); err != nil {
			m.logger.Warnf("Failed to deregister member: %v", err)
		} else {
			m.logger.Infof("Deregistered member %d", m.self.NodeID)
		}

		// Reset member count metric
		m.metrics.memberCount.Set(0)

		m.logger.Info("Membership manager closed")
	})

	return closeErr
}
