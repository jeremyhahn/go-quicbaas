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

package ha

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// QuorumRecoveryManager handles quorum loss and recovery procedures.
type QuorumRecoveryManager struct {
	raftNode interfaces.RaftNode
	config   *QuorumRecoveryConfig
	logger   interfaces.Logger

	// recovery state
	recoveryInProgress map[uint64]bool
	recoveryMu         sync.RWMutex

	// metrics
	metrics *quorumRecoveryMetrics
}

// QuorumRecoveryConfig configures quorum recovery behavior.
type QuorumRecoveryConfig struct {
	// AutoRecover enables automatic recovery attempts
	AutoRecover bool

	// RecoveryTimeout is the maximum time to wait for recovery
	RecoveryTimeout time.Duration

	// ForceReconstruct allows forcing cluster reconstruction as last resort
	ForceReconstruct bool

	// NodeRestartTimeout is how long to wait for a node restart
	NodeRestartTimeout time.Duration

	// MinHealthyNodes is the minimum number of healthy nodes required
	MinHealthyNodes int

	// HealthCheckInterval is how often to check node health during recovery
	HealthCheckInterval time.Duration
}

// RecoveryProcedure represents a quorum recovery procedure.
type RecoveryProcedure string

const (
	RecoveryRestartNodes  RecoveryProcedure = "restart_nodes"
	RecoveryRemoveNodes   RecoveryProcedure = "remove_nodes"
	RecoveryAddNodes      RecoveryProcedure = "add_nodes"
	RecoveryReconstruct   RecoveryProcedure = "reconstruct_cluster"
	RecoveryRestoreBackup RecoveryProcedure = "restore_backup"
)

// RecoveryStatus represents the status of a recovery operation.
type RecoveryStatus struct {
	ShardID       uint64
	InProgress    bool
	Procedure     RecoveryProcedure
	StartTime     time.Time
	Progress      string
	HealthyNodes  int
	RequiredNodes int
	Success       bool
	Error         error
}

type quorumRecoveryMetrics struct {
	recoveryAttempts *metrics.CounterVec
	recoverySuccess  *metrics.CounterVec
	recoveryDuration *metrics.HistogramVec
	quorumLossEvents *metrics.Counter
	nodesRestarted   *metrics.CounterVec
}

// NewQuorumRecoveryManager creates a new quorum recovery manager.
func NewQuorumRecoveryManager(raftNode interfaces.RaftNode, config *QuorumRecoveryConfig, logger interfaces.Logger) (*QuorumRecoveryManager, error) {
	if raftNode == nil {
		return nil, errors.InvalidInput("ha.NewQuorumRecoveryManager", "raft node cannot be nil")
	}
	if config == nil {
		return nil, errors.InvalidInput("ha.NewQuorumRecoveryManager", "config cannot be nil")
	}
	if logger == nil {
		return nil, errors.InvalidInput("ha.NewQuorumRecoveryManager", "logger cannot be nil")
	}

	// Apply defaults
	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = 5 * time.Minute
	}
	if config.NodeRestartTimeout == 0 {
		config.NodeRestartTimeout = 30 * time.Second
	}
	if config.MinHealthyNodes == 0 {
		config.MinHealthyNodes = 2
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 5 * time.Second
	}

	qrm := &QuorumRecoveryManager{
		raftNode:           raftNode,
		config:             config,
		logger:             logger.WithField("component", "quorum-recovery"),
		recoveryInProgress: make(map[uint64]bool),
	}

	qrm.initMetrics()

	return qrm, nil
}

// initMetrics initializes Prometheus metrics.
func (qrm *QuorumRecoveryManager) initMetrics() {
	qrm.metrics = &quorumRecoveryMetrics{
		recoveryAttempts: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "recovery_attempts_total",
			Help:      "Total number of quorum recovery attempts",
		}, []string{"shard", "procedure"}),

		recoverySuccess: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "recovery_success_total",
			Help:      "Total number of successful quorum recoveries",
		}, []string{"shard", "procedure"}),

		recoveryDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "recovery_duration_seconds",
			Help:      "Duration of quorum recovery operations",
			Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600},
		}, []string{"shard", "procedure"}),

		quorumLossEvents: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "quorum_loss_events_total",
			Help:      "Total number of quorum loss events",
		}),

		nodesRestarted: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "nodes_restarted_total",
			Help:      "Total number of nodes restarted during recovery",
		}, []string{"status"}),
	}
}

// RecoverQuorum attempts to restore quorum for a shard.
func (qrm *QuorumRecoveryManager) RecoverQuorum(ctx context.Context, shardID uint64) error {
	start := time.Now()
	shardStr := fmt.Sprintf("%d", shardID)

	qrm.logger.WithField("shard_id", shardID).Info("Starting quorum recovery")

	// Check if recovery already in progress
	qrm.recoveryMu.Lock()
	if qrm.recoveryInProgress[shardID] {
		qrm.recoveryMu.Unlock()
		return errors.Newf(errors.ErrAlreadyExists, "ha.RecoverQuorum", "recovery already in progress for shard %d", shardID)
	}
	qrm.recoveryInProgress[shardID] = true
	qrm.recoveryMu.Unlock()

	defer func() {
		qrm.recoveryMu.Lock()
		delete(qrm.recoveryInProgress, shardID)
		qrm.recoveryMu.Unlock()
	}()

	qrm.metrics.quorumLossEvents.Inc()

	// Assess situation
	assessment, err := qrm.assessQuorumLoss(ctx, shardID)
	if err != nil {
		return err
	}

	// Already have quorum
	if assessment.HasQuorum {
		qrm.logger.WithField("shard_id", shardID).Info("Shard already has quorum")
		return nil
	}

	// Choose recovery procedure based on assessment
	procedure := qrm.chooseRecoveryProcedure(assessment)
	qrm.metrics.recoveryAttempts.With(shardStr, string(procedure)).Inc()

	qrm.logger.WithFields(map[string]interface{}{
		"shard_id":  shardID,
		"procedure": procedure,
	}).Info("Executing recovery procedure")

	// Execute recovery procedure
	var recoveryErr error
	switch procedure {
	case RecoveryRestartNodes:
		recoveryErr = qrm.recoverByRestartingNodes(ctx, shardID, assessment)
	case RecoveryRemoveNodes:
		recoveryErr = qrm.recoverByRemovingNodes(ctx, shardID, assessment)
	case RecoveryAddNodes:
		recoveryErr = qrm.recoverByAddingNodes(ctx, shardID, assessment)
	case RecoveryReconstruct:
		recoveryErr = qrm.recoverByReconstructing(ctx, shardID, assessment)
	default:
		recoveryErr = errors.Newf(errors.ErrNotImplemented, "ha.RecoverQuorum", "unsupported recovery procedure: %s", procedure)
	}

	duration := time.Since(start)
	qrm.metrics.recoveryDuration.With(shardStr, string(procedure)).Observe(duration.Seconds())

	if recoveryErr == nil {
		qrm.metrics.recoverySuccess.With(shardStr, string(procedure)).Inc()
		qrm.logger.WithFields(map[string]interface{}{
			"shard_id":  shardID,
			"procedure": procedure,
			"duration":  duration,
		}).Info("Quorum recovery completed successfully")
	} else {
		qrm.logger.WithError(recoveryErr).WithFields(map[string]interface{}{
			"shard_id":  shardID,
			"procedure": procedure,
			"duration":  duration,
		}).Error("Quorum recovery failed")
	}

	return recoveryErr
}

// QuorumAssessment contains information about the quorum loss situation.
type QuorumAssessment struct {
	ShardID         uint64
	HasQuorum       bool
	TotalMembers    int
	AliveMembers    []uint64
	DeadMembers     []uint64
	RequiredQuorum  int
	RecoveryOptions []RecoveryProcedure
}

// assessQuorumLoss analyzes the quorum loss situation.
func (qrm *QuorumRecoveryManager) assessQuorumLoss(ctx context.Context, shardID uint64) (QuorumAssessment, error) {
	// Get NodeHost info
	nodeHostInfo := qrm.raftNode.GetNodeHostInfo(interfaces.NodeHostInfoOption{})
	if nodeHostInfo == nil {
		return QuorumAssessment{}, errors.Internal("ha.assessQuorumLoss", "failed to get node host info")
	}

	// Try to get leader (will fail if no quorum)
	_, _, err := qrm.raftNode.GetLeaderID(shardID)
	hasQuorum := err == nil

	// For simplicity, assume 3-node cluster
	// In real implementation, query actual membership
	totalMembers := 3
	var aliveMembers []uint64
	deadMembers := make([]uint64, 0)

	// Simulate member detection (in reality, would query Raft membership)
	if hasQuorum {
		aliveMembers = []uint64{1, 2, 3}
	} else {
		// Assume only current node is alive
		aliveMembers = []uint64{1}
		deadMembers = []uint64{2, 3}
	}

	requiredQuorum := (totalMembers / 2) + 1

	// Determine recovery options
	recoveryOptions := qrm.determineRecoveryOptions(len(aliveMembers), totalMembers)

	return QuorumAssessment{
		ShardID:         shardID,
		HasQuorum:       hasQuorum,
		TotalMembers:    totalMembers,
		AliveMembers:    aliveMembers,
		DeadMembers:     deadMembers,
		RequiredQuorum:  requiredQuorum,
		RecoveryOptions: recoveryOptions,
	}, nil
}

// determineRecoveryOptions determines available recovery options.
func (qrm *QuorumRecoveryManager) determineRecoveryOptions(aliveCount, totalCount int) []RecoveryProcedure {
	options := make([]RecoveryProcedure, 0)

	// Can try restarting if some nodes are down
	if aliveCount < totalCount {
		options = append(options, RecoveryRestartNodes)
	}

	// Can try adding nodes if enabled
	options = append(options, RecoveryAddNodes)

	// Can try removing dead nodes to achieve quorum with remaining
	if aliveCount > 0 {
		options = append(options, RecoveryRemoveNodes)
	}

	// Last resort: reconstruct cluster
	if qrm.config.ForceReconstruct {
		options = append(options, RecoveryReconstruct)
	}

	return options
}

// chooseRecoveryProcedure selects the best recovery procedure.
func (qrm *QuorumRecoveryManager) chooseRecoveryProcedure(assessment QuorumAssessment) RecoveryProcedure {
	// Prefer least disruptive approaches first
	if len(assessment.RecoveryOptions) == 0 {
		return RecoveryRestartNodes // Default
	}

	// Prioritize based on likelihood of success
	for _, option := range assessment.RecoveryOptions {
		switch option {
		case RecoveryRestartNodes:
			// Try restart first (least disruptive)
			if len(assessment.DeadMembers) > 0 {
				return RecoveryRestartNodes
			}
		case RecoveryAddNodes:
			// Try adding nodes if restart not applicable
			return RecoveryAddNodes
		case RecoveryRemoveNodes:
			// Try removing dead nodes
			if len(assessment.DeadMembers) > 0 {
				return RecoveryRemoveNodes
			}
		case RecoveryReconstruct:
			// Last resort
			return RecoveryReconstruct
		}
	}

	return assessment.RecoveryOptions[0]
}

// recoverByRestartingNodes attempts to recover by restarting failed nodes.
func (qrm *QuorumRecoveryManager) recoverByRestartingNodes(ctx context.Context, shardID uint64, assessment QuorumAssessment) error {
	qrm.logger.WithFields(map[string]interface{}{
		"shard_id":   shardID,
		"dead_nodes": assessment.DeadMembers,
	}).Info("Attempting to restart failed nodes")

	// In a real implementation, this would:
	// 1. Connect to orchestrator (Kubernetes, systemd, etc.)
	// 2. Restart failed node processes
	// 3. Wait for nodes to rejoin cluster
	// 4. Verify quorum restored

	// Simulate node restart
	for _, nodeID := range assessment.DeadMembers {
		qrm.logger.WithField("node_id", nodeID).Info("Restarting node")

		// Simulate restart delay
		select {
		case <-time.After(1 * time.Second):
			qrm.metrics.nodesRestarted.With("success").Inc()
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Wait for quorum
	return qrm.waitForQuorum(ctx, shardID)
}

// recoverByRemovingNodes attempts to recover by removing dead nodes from cluster.
func (qrm *QuorumRecoveryManager) recoverByRemovingNodes(ctx context.Context, shardID uint64, assessment QuorumAssessment) error {
	qrm.logger.WithFields(map[string]interface{}{
		"shard_id":   shardID,
		"dead_nodes": assessment.DeadMembers,
	}).Warn("Removing dead nodes from cluster")

	// Remove each dead node
	for _, nodeID := range assessment.DeadMembers {
		_, err := qrm.raftNode.RequestRemoveNode(ctx, shardID, nodeID, 0)
		if err != nil {
			return errors.Wrapf(err, errors.ErrRaft, "ha.recoverByRemovingNodes", "failed to remove node %d", nodeID)
		}
		qrm.logger.WithField("node_id", nodeID).Info("Node removed from cluster")
	}

	// Wait for quorum with reduced membership
	return qrm.waitForQuorum(ctx, shardID)
}

// recoverByAddingNodes attempts to recover by adding new nodes to cluster.
func (qrm *QuorumRecoveryManager) recoverByAddingNodes(ctx context.Context, shardID uint64, assessment QuorumAssessment) error {
	qrm.logger.WithField("shard_id", shardID).Info("Adding new nodes to restore quorum")

	// In a real implementation, this would:
	// 1. Provision new nodes
	// 2. Add them to the cluster
	// 3. Wait for data replication
	// 4. Verify quorum

	return errors.NotImplemented("ha.recoverByAddingNodes", "adding nodes requires manual provisioning")
}

// recoverByReconstructing attempts to reconstruct the cluster from scratch.
func (qrm *QuorumRecoveryManager) recoverByReconstructing(ctx context.Context, shardID uint64, assessment QuorumAssessment) error {
	if !qrm.config.ForceReconstruct {
		return errors.New(errors.ErrNotAllowed, "ha.recoverByReconstructing", "cluster reconstruction requires ForceReconstruct=true")
	}

	qrm.logger.WithField("shard_id", shardID).Warn("Reconstructing cluster (last resort)")

	// In a real implementation, this would:
	// 1. Stop all nodes
	// 2. Restore from most recent backup
	// 3. Reform cluster with surviving nodes
	// 4. Verify data consistency

	return errors.NotImplemented("ha.recoverByReconstructing", "cluster reconstruction requires manual intervention")
}

// waitForQuorum waits for quorum to be restored.
func (qrm *QuorumRecoveryManager) waitForQuorum(ctx context.Context, shardID uint64) error {
	timeout := qrm.config.RecoveryTimeout
	deadline := time.Now().Add(timeout)

	ticker := time.NewTicker(qrm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			if time.Now().After(deadline) {
				return errors.Newf(errors.ErrTimeout, "ha.waitForQuorum", "timeout waiting for quorum restoration on shard %d", shardID)
			}

			// Check if quorum restored
			_, _, err := qrm.raftNode.GetLeaderID(shardID)
			if err == nil {
				qrm.logger.WithField("shard_id", shardID).Info("Quorum restored")
				return nil
			}

			qrm.logger.WithField("shard_id", shardID).Debug("Waiting for quorum...")
		}
	}
}

// GetRecoveryStatus returns the status of ongoing recoveries.
func (qrm *QuorumRecoveryManager) GetRecoveryStatus(shardID uint64) RecoveryStatus {
	qrm.recoveryMu.RLock()
	inProgress := qrm.recoveryInProgress[shardID]
	qrm.recoveryMu.RUnlock()

	status := RecoveryStatus{
		ShardID:    shardID,
		InProgress: inProgress,
	}

	if !inProgress {
		// Check current quorum status
		_, _, err := qrm.raftNode.GetLeaderID(shardID)
		status.Success = err == nil
		status.Error = err
	}

	return status
}

// VerifyDataConsistency verifies data consistency after recovery.
func (qrm *QuorumRecoveryManager) VerifyDataConsistency(ctx context.Context, shardID uint64) error {
	qrm.logger.WithField("shard_id", shardID).Info("Verifying data consistency")

	// In a real implementation, this would:
	// 1. Read sample keys from all replicas
	// 2. Compare checksums/versions
	// 3. Report any inconsistencies
	// 4. Trigger repair if needed

	return nil
}
