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

// Package ha provides high availability features for QuicBaaS including
// automated failover, split-brain prevention, and network partition handling.
package ha

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// FailoverManager manages automated failover for Raft shards.
// It monitors cluster health and triggers failover when leaders become unavailable.
type FailoverManager struct {
	raftNode interfaces.RaftNode
	config   *FailoverConfig
	logger   interfaces.Logger

	// failover state
	failoverHistory []FailoverEvent
	historyMu       sync.RWMutex

	// metrics
	metrics *failoverMetrics

	// lifecycle
	ctx      context.Context
	cancel   context.CancelFunc
	stopChan chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// FailoverConfig configures automated failover behavior.
type FailoverConfig struct {
	// HealthCheckInterval is how often to check cluster health
	HealthCheckInterval time.Duration

	// FailoverTimeout is the maximum time to wait for failover
	FailoverTimeout time.Duration

	// MaxFailoversPerHour limits failover frequency to prevent flapping
	MaxFailoversPerHour int

	// Shards is the list of shard IDs to monitor
	Shards []uint64

	// LeaderTimeout is how long to wait for a leader before triggering failover
	LeaderTimeout time.Duration

	// HealthCheckTimeout is the timeout for individual health checks
	HealthCheckTimeout time.Duration
}

// FailoverEvent records a failover occurrence.
type FailoverEvent struct {
	Timestamp      time.Time
	ShardID        uint64
	OldLeaderID    uint64
	NewLeaderID    uint64
	Reason         string
	Success        bool
	Duration       time.Duration
	RecoveryAction string
}

// FailoverStatus represents the current failover state.
type FailoverStatus struct {
	Healthy           bool
	LastCheck         time.Time
	UnhealthyShards   []uint64
	FailoversLastHour int
	RecentEvents      []FailoverEvent
}

type failoverMetrics struct {
	failoversTotal    *metrics.CounterVec
	failoverDuration  *metrics.HistogramVec
	healthChecksTotal *metrics.CounterVec
	unhealthyShards   *metrics.GaugeVec
	leaderChanges     *metrics.CounterVec
	failoverAttempts  *metrics.CounterVec
}

// NewFailoverManager creates a new failover manager.
func NewFailoverManager(raftNode interfaces.RaftNode, config *FailoverConfig, logger interfaces.Logger) (*FailoverManager, error) {
	if raftNode == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ha.NewFailoverManager",
			Err:  &errors.MessageError{Message: "raft node cannot be nil"},
		}
	}
	if config == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ha.NewFailoverManager",
			Err:  &errors.MessageError{Message: "config cannot be nil"},
		}
	}
	if logger == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ha.NewFailoverManager",
			Err:  &errors.MessageError{Message: "logger cannot be nil"},
		}
	}

	// Apply defaults
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 5 * time.Second
	}
	if config.FailoverTimeout == 0 {
		config.FailoverTimeout = 30 * time.Second
	}
	if config.MaxFailoversPerHour == 0 {
		config.MaxFailoversPerHour = 10
	}
	if config.LeaderTimeout == 0 {
		config.LeaderTimeout = 10 * time.Second
	}
	if config.HealthCheckTimeout == 0 {
		config.HealthCheckTimeout = 5 * time.Second
	}

	// The manager owns this context for its whole lifetime; cancel is retained
	// on the struct and called by Stop. gosec cannot follow it across the
	// struct field.
	ctx, cancel := context.WithCancel(context.Background()) // #nosec G118 -- cancel is stored on fm and invoked in Stop.

	fm := &FailoverManager{
		raftNode:        raftNode,
		config:          config,
		logger:          logger.WithField("component", "failover-manager"),
		failoverHistory: make([]FailoverEvent, 0, 100),
		ctx:             ctx,
		cancel:          cancel,
		stopChan:        make(chan struct{}),
	}

	fm.initMetrics()

	return fm, nil
}

// initMetrics initializes Prometheus metrics.
func (fm *FailoverManager) initMetrics() {
	fm.metrics = &failoverMetrics{
		failoversTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "failovers_total",
			Help:      "Total number of failovers by shard and status",
		}, []string{"shard", "status"}),

		failoverDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "failover_duration_seconds",
			Help:      "Duration of failover operations",
			Buckets:   []float64{.1, .5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"shard"}),

		healthChecksTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "health_checks_total",
			Help:      "Total number of health checks by status",
		}, []string{"status"}),

		unhealthyShards: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "unhealthy_shards",
			Help:      "Number of unhealthy shards",
		}, []string{"shard"}),

		leaderChanges: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "leader_changes_total",
			Help:      "Total number of leader changes detected",
		}, []string{"shard"}),

		failoverAttempts: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "failover_attempts_total",
			Help:      "Total number of failover attempts",
		}, []string{"shard", "reason"}),
	}
}

// Start begins monitoring cluster health and handling failovers.
func (fm *FailoverManager) Start() error {
	fm.logger.Info("Starting failover manager")

	fm.wg.Add(1)
	go fm.monitorLoop()

	return nil
}

// Stop gracefully stops the failover manager.
func (fm *FailoverManager) Stop() error {
	var stopErr error

	fm.stopOnce.Do(func() {
		fm.logger.Info("Stopping failover manager")
		fm.cancel()
		close(fm.stopChan)
		fm.wg.Wait()
		fm.logger.Info("Failover manager stopped")
	})

	return stopErr
}

// monitorLoop continuously monitors cluster health and triggers failovers.
func (fm *FailoverManager) monitorLoop() {
	defer fm.wg.Done()

	ticker := time.NewTicker(fm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := fm.performHealthCheck(); err != nil {
				fm.logger.WithError(err).Error("Health check failed")
			}

		case <-fm.stopChan:
			return

		case <-fm.ctx.Done():
			return
		}
	}
}

// performHealthCheck checks the health of all monitored shards.
func (fm *FailoverManager) performHealthCheck() error {
	ctx, cancel := context.WithTimeout(fm.ctx, fm.config.HealthCheckTimeout)
	defer cancel()

	unhealthyCount := 0

	for _, shardID := range fm.config.Shards {
		if err := fm.checkShardHealth(ctx, shardID); err != nil {
			fm.logger.WithFields(map[string]interface{}{
				"shard_id": shardID,
				"error":    err,
			}).Warn("Shard health check failed")

			fm.metrics.unhealthyShards.With(fmt.Sprintf("%d", shardID)).Set(1)
			unhealthyCount++

			// Trigger failover if needed
			if err := fm.triggerFailover(ctx, shardID, err.Error()); err != nil {
				fm.logger.WithError(err).Error("Failover failed")
			}
		} else {
			fm.metrics.unhealthyShards.With(fmt.Sprintf("%d", shardID)).Set(0)
		}
	}

	if unhealthyCount == 0 {
		fm.metrics.healthChecksTotal.With("success").Inc()
	} else {
		fm.metrics.healthChecksTotal.With("degraded").Inc()
	}

	return nil
}

// checkShardHealth verifies that a shard has a healthy leader.
func (fm *FailoverManager) checkShardHealth(ctx context.Context, shardID uint64) error {
	// Check if shard has a leader
	leaderID, _, err := fm.raftNode.GetLeaderID(shardID)
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrLeaderNotAvailable,
			Op:   "ha.checkShardHealth",
			Err: &errors.WrappedMessageError{
				Message: "failed to get leader for shard " + strconv.FormatUint(shardID, 10),
				Cause:   err,
			},
		}
	}

	if leaderID == 0 {
		return &errors.QBError{
			Code: errors.ErrLeaderNotAvailable,
			Op:   "ha.checkShardHealth",
			Err:  &errors.MessageError{Message: "no leader available for shard " + strconv.FormatUint(shardID, 10)},
		}
	}

	// Leader exists and is valid
	return nil
}

// triggerFailover initiates an automated failover for the given shard.
func (fm *FailoverManager) triggerFailover(ctx context.Context, shardID uint64, reason string) error {
	start := time.Now()

	fm.logger.WithFields(map[string]interface{}{
		"shard_id": shardID,
		"reason":   reason,
	}).Info("Triggering failover")

	fm.metrics.failoverAttempts.With(fmt.Sprintf("%d", shardID), reason).Inc()

	// Check if we've exceeded failover rate limit
	if fm.exceedsFailoverLimit() {
		fm.logger.Warn("Failover rate limit exceeded, skipping")
		return &errors.QBError{
			Code: errors.ErrRateLimited,
			Op:   "ha.triggerFailover",
			Err:  &errors.MessageError{Message: "failover rate limit exceeded"},
		}
	}

	// Get current leader (may be 0 if unavailable during failover)
	oldLeaderID, _, leaderErr := fm.raftNode.GetLeaderID(shardID)
	if leaderErr != nil {
		fm.logger.WithError(leaderErr).WithField("shard_id", shardID).Warn("Could not determine current leader during failover")
	}

	// Wait for Raft to elect a new leader
	newLeaderID, err := fm.waitForNewLeader(ctx, shardID, oldLeaderID)
	success := err == nil

	duration := time.Since(start)

	// Record failover event
	event := FailoverEvent{
		Timestamp:      start,
		ShardID:        shardID,
		OldLeaderID:    oldLeaderID,
		NewLeaderID:    newLeaderID,
		Reason:         reason,
		Success:        success,
		Duration:       duration,
		RecoveryAction: "raft_election",
	}

	fm.recordFailoverEvent(event)

	// Update metrics
	status := "success"
	if !success {
		status = "failure"
	}
	fm.metrics.failoversTotal.With(fmt.Sprintf("%d", shardID), status).Inc()
	fm.metrics.failoverDuration.With(fmt.Sprintf("%d", shardID)).Observe(duration.Seconds())

	if success {
		fm.metrics.leaderChanges.With(fmt.Sprintf("%d", shardID)).Inc()
		fm.logger.WithFields(map[string]interface{}{
			"shard_id":      shardID,
			"old_leader_id": oldLeaderID,
			"new_leader_id": newLeaderID,
			"duration":      duration,
		}).Info("Failover completed successfully")
	} else {
		fm.logger.WithError(err).WithFields(map[string]interface{}{
			"shard_id": shardID,
			"duration": duration,
		}).Error("Failover failed")
	}

	return err
}

// waitForNewLeader waits for a new leader to be elected for the shard.
func (fm *FailoverManager) waitForNewLeader(ctx context.Context, shardID, oldLeaderID uint64) (uint64, error) {
	timeout := fm.config.FailoverTimeout
	deadline := time.Now().Add(timeout)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()

		case <-ticker.C:
			if time.Now().After(deadline) {
				return 0, &errors.QBError{
					Code: errors.ErrTimeout,
					Op:   "ha.waitForNewLeader",
					Err:  &errors.MessageError{Message: "timeout waiting for new leader for shard " + strconv.FormatUint(shardID, 10)},
				}
			}

			leaderID, _, err := fm.raftNode.GetLeaderID(shardID)
			if err != nil {
				continue
			}

			// New leader elected
			if leaderID != 0 && leaderID != oldLeaderID {
				return leaderID, nil
			}

			// If old leader is 0, any leader is considered new
			if oldLeaderID == 0 && leaderID != 0 {
				return leaderID, nil
			}
		}
	}
}

// exceedsFailoverLimit checks if we've exceeded the failover rate limit.
func (fm *FailoverManager) exceedsFailoverLimit() bool {
	fm.historyMu.RLock()
	defer fm.historyMu.RUnlock()

	oneHourAgo := time.Now().Add(-1 * time.Hour)
	count := 0

	for i := len(fm.failoverHistory) - 1; i >= 0; i-- {
		event := fm.failoverHistory[i]
		if event.Timestamp.Before(oneHourAgo) {
			break
		}
		count++
	}

	return count >= fm.config.MaxFailoversPerHour
}

// recordFailoverEvent records a failover event in the history.
func (fm *FailoverManager) recordFailoverEvent(event FailoverEvent) {
	fm.historyMu.Lock()
	defer fm.historyMu.Unlock()

	fm.failoverHistory = append(fm.failoverHistory, event)

	// Keep only last 100 events
	if len(fm.failoverHistory) > 100 {
		fm.failoverHistory = fm.failoverHistory[len(fm.failoverHistory)-100:]
	}
}

// GetStatus returns the current failover status.
func (fm *FailoverManager) GetStatus() FailoverStatus {
	fm.historyMu.RLock()
	defer fm.historyMu.RUnlock()

	oneHourAgo := time.Now().Add(-1 * time.Hour)
	failoversLastHour := 0

	for i := len(fm.failoverHistory) - 1; i >= 0; i-- {
		event := fm.failoverHistory[i]
		if event.Timestamp.Before(oneHourAgo) {
			break
		}
		failoversLastHour++
	}

	// Get recent events (last 10)
	recentEvents := make([]FailoverEvent, 0, 10)
	start := len(fm.failoverHistory) - 10
	if start < 0 {
		start = 0
	}
	for i := start; i < len(fm.failoverHistory); i++ {
		recentEvents = append(recentEvents, fm.failoverHistory[i])
	}

	// Check current health
	unhealthyShards := make([]uint64, 0)
	for _, shardID := range fm.config.Shards {
		if _, _, err := fm.raftNode.GetLeaderID(shardID); err != nil {
			unhealthyShards = append(unhealthyShards, shardID)
		}
	}

	return FailoverStatus{
		Healthy:           len(unhealthyShards) == 0,
		LastCheck:         time.Now(),
		UnhealthyShards:   unhealthyShards,
		FailoversLastHour: failoversLastHour,
		RecentEvents:      recentEvents,
	}
}

// GetFailoverHistory returns the complete failover history.
func (fm *FailoverManager) GetFailoverHistory() []FailoverEvent {
	fm.historyMu.RLock()
	defer fm.historyMu.RUnlock()

	// Return a copy
	history := make([]FailoverEvent, len(fm.failoverHistory))
	copy(history, fm.failoverHistory)
	return history
}
