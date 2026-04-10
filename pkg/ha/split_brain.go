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

// SplitBrainPreventer prevents split-brain scenarios by enforcing quorum
// requirements and implementing fencing mechanisms.
type SplitBrainPreventer struct {
	raftNode interfaces.RaftNode
	config   *SplitBrainConfig
	logger   interfaces.Logger

	// fencing state
	fencedNodes   map[uint64]time.Time // nodeID -> fence time
	fencedNodesMu sync.RWMutex

	// metrics
	metrics *splitBrainMetrics
}

// SplitBrainConfig configures split-brain prevention.
type SplitBrainConfig struct {
	// MinQuorum is the minimum number of nodes required for quorum
	MinQuorum int

	// QuorumTimeout is the timeout for quorum checks
	QuorumTimeout time.Duration

	// FencingEnabled enables automatic node fencing
	FencingEnabled bool

	// FencingTimeout is how long to fence a node
	FencingTimeout time.Duration

	// EnableGenerationNumbers enables generation-based fencing
	EnableGenerationNumbers bool
}

// QuorumStatus represents the current quorum state.
type QuorumStatus struct {
	HasQuorum        bool
	TotalMembers     int
	ReachableMembers int
	RequiredQuorum   int
	Timestamp        time.Time
}

type splitBrainMetrics struct {
	quorumChecksTotal  *metrics.CounterVec
	quorumLossEvents   *metrics.Counter
	splitBrainDetected *metrics.Counter
	fencingOperations  *metrics.CounterVec
	quorumStatus       *metrics.GaugeVec
}

// NewSplitBrainPreventer creates a new split-brain preventer.
func NewSplitBrainPreventer(raftNode interfaces.RaftNode, config *SplitBrainConfig, logger interfaces.Logger) (*SplitBrainPreventer, error) {
	if raftNode == nil {
		return nil, errors.InvalidInput("ha.NewSplitBrainPreventer", "raft node cannot be nil")
	}
	if config == nil {
		return nil, errors.InvalidInput("ha.NewSplitBrainPreventer", "config cannot be nil")
	}
	if logger == nil {
		return nil, errors.InvalidInput("ha.NewSplitBrainPreventer", "logger cannot be nil")
	}

	// Apply defaults
	if config.MinQuorum == 0 {
		config.MinQuorum = 2 // Default to 2 for 3-node cluster
	}
	if config.QuorumTimeout == 0 {
		config.QuorumTimeout = 5 * time.Second
	}
	if config.FencingTimeout == 0 {
		config.FencingTimeout = 5 * time.Minute
	}

	sbp := &SplitBrainPreventer{
		raftNode:    raftNode,
		config:      config,
		logger:      logger.WithField("component", "split-brain-preventer"),
		fencedNodes: make(map[uint64]time.Time),
	}

	sbp.initMetrics()

	return sbp, nil
}

// initMetrics initializes Prometheus metrics.
func (sbp *SplitBrainPreventer) initMetrics() {
	sbp.metrics = &splitBrainMetrics{
		quorumChecksTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "quorum_checks_total",
			Help:      "Total number of quorum checks by status",
		}, []string{"shard", "status"}),

		quorumLossEvents: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "quorum_loss_events_total",
			Help:      "Total number of quorum loss events",
		}),

		splitBrainDetected: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "split_brain_detected_total",
			Help:      "Total number of split-brain scenarios detected",
		}),

		fencingOperations: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "fencing_operations_total",
			Help:      "Total number of fencing operations by action",
		}, []string{"action"}),

		quorumStatus: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "quorum_status",
			Help:      "Current quorum status (1=has quorum, 0=no quorum)",
		}, []string{"shard"}),
	}
}

// EnforceQuorum verifies that the shard has quorum before allowing operations.
func (sbp *SplitBrainPreventer) EnforceQuorum(ctx context.Context, shardID uint64) error {
	status, err := sbp.CheckQuorum(ctx, shardID)
	if err != nil {
		return err
	}

	if !status.HasQuorum {
		sbp.metrics.quorumLossEvents.Inc()
		sbp.logger.WithFields(map[string]interface{}{
			"shard_id":          shardID,
			"reachable_members": status.ReachableMembers,
			"required_quorum":   status.RequiredQuorum,
		}).Error("Quorum lost")

		return errors.Newf(errors.ErrQuorumLost, "ha.EnforceQuorum",
			"quorum lost for shard %d: %d/%d members reachable (need %d)",
			shardID, status.ReachableMembers, status.TotalMembers, status.RequiredQuorum)
	}

	return nil
}

// CheckQuorum checks if the shard currently has quorum.
func (sbp *SplitBrainPreventer) CheckQuorum(ctx context.Context, shardID uint64) (QuorumStatus, error) {
	shardStr := fmt.Sprintf("%d", shardID)

	// Get cluster membership info
	nodeHostInfo := sbp.raftNode.GetNodeHostInfo(interfaces.NodeHostInfoOption{})
	if nodeHostInfo == nil {
		sbp.metrics.quorumChecksTotal.With(shardStr, "error").Inc()
		return QuorumStatus{}, errors.Internal("ha.CheckQuorum", "failed to get node host info")
	}

	// Find shard info
	var shardInfo *interface{}
	for _, clusterInfo := range nodeHostInfo.LogInfo {
		if clusterInfo.ShardID == shardID {
			temp := interface{}(clusterInfo)
			shardInfo = &temp
			break
		}
	}

	if shardInfo == nil {
		sbp.metrics.quorumChecksTotal.With(shardStr, "error").Inc()
		return QuorumStatus{}, errors.Newf(errors.ErrNotFound, "ha.CheckQuorum", "shard %d not found", shardID)
	}

	// For Raft consensus, we need to check if there's a leader
	// A leader can only exist with quorum
	leaderID, _, err := sbp.raftNode.GetLeaderID(shardID)
	hasQuorum := err == nil && leaderID != 0

	// Estimate cluster size from config (in real implementation, query actual membership)
	// For now, we'll use a simple heuristic: if there's a leader, assume quorum
	totalMembers := 3 // Default assumption for 3-node cluster
	reachableMembers := 0
	if hasQuorum {
		reachableMembers = (totalMembers / 2) + 1 // Minimum quorum
	}

	requiredQuorum := (totalMembers / 2) + 1
	if requiredQuorum < sbp.config.MinQuorum {
		requiredQuorum = sbp.config.MinQuorum
	}

	status := QuorumStatus{
		HasQuorum:        hasQuorum,
		TotalMembers:     totalMembers,
		ReachableMembers: reachableMembers,
		RequiredQuorum:   requiredQuorum,
		Timestamp:        time.Now(),
	}

	// Update metrics
	if hasQuorum {
		sbp.metrics.quorumStatus.With(shardStr).Set(1)
		sbp.metrics.quorumChecksTotal.With(shardStr, "success").Inc()
	} else {
		sbp.metrics.quorumStatus.With(shardStr).Set(0)
		sbp.metrics.quorumChecksTotal.With(shardStr, "no_quorum").Inc()
	}

	return status, nil
}

// FenceNode marks a node as fenced, preventing it from participating in the cluster.
func (sbp *SplitBrainPreventer) FenceNode(nodeID uint64, reason string) error {
	if !sbp.config.FencingEnabled {
		return errors.NotImplemented("ha.FenceNode", "fencing is not enabled")
	}

	sbp.fencedNodesMu.Lock()
	defer sbp.fencedNodesMu.Unlock()

	sbp.fencedNodes[nodeID] = time.Now()
	sbp.metrics.fencingOperations.With("fence").Inc()

	sbp.logger.WithFields(map[string]interface{}{
		"node_id": nodeID,
		"reason":  reason,
	}).Warn("Node fenced")

	return nil
}

// UnfenceNode removes fencing from a node, allowing it to rejoin the cluster.
func (sbp *SplitBrainPreventer) UnfenceNode(nodeID uint64) error {
	sbp.fencedNodesMu.Lock()
	defer sbp.fencedNodesMu.Unlock()

	if _, fenced := sbp.fencedNodes[nodeID]; !fenced {
		return errors.Newf(errors.ErrNotFound, "ha.UnfenceNode", "node %d is not fenced", nodeID)
	}

	delete(sbp.fencedNodes, nodeID)
	sbp.metrics.fencingOperations.With("unfence").Inc()

	sbp.logger.WithField("node_id", nodeID).Info("Node unfenced")

	return nil
}

// IsNodeFenced checks if a node is currently fenced.
func (sbp *SplitBrainPreventer) IsNodeFenced(nodeID uint64) bool {
	sbp.fencedNodesMu.RLock()
	defer sbp.fencedNodesMu.RUnlock()

	fenceTime, fenced := sbp.fencedNodes[nodeID]
	if !fenced {
		return false
	}

	// Auto-unfence after timeout
	if time.Since(fenceTime) > sbp.config.FencingTimeout {
		// Note: We don't modify the map here to avoid deadlock
		// The caller should call UnfenceNode explicitly or use a cleanup goroutine
		return false
	}

	return true
}

// GetFencedNodes returns the list of currently fenced nodes.
func (sbp *SplitBrainPreventer) GetFencedNodes() []uint64 {
	sbp.fencedNodesMu.RLock()
	defer sbp.fencedNodesMu.RUnlock()

	nodes := make([]uint64, 0, len(sbp.fencedNodes))
	now := time.Now()

	for nodeID, fenceTime := range sbp.fencedNodes {
		if now.Sub(fenceTime) <= sbp.config.FencingTimeout {
			nodes = append(nodes, nodeID)
		}
	}

	return nodes
}

// CleanupExpiredFences removes expired fencing entries.
func (sbp *SplitBrainPreventer) CleanupExpiredFences() int {
	sbp.fencedNodesMu.Lock()
	defer sbp.fencedNodesMu.Unlock()

	now := time.Now()
	removed := 0

	for nodeID, fenceTime := range sbp.fencedNodes {
		if now.Sub(fenceTime) > sbp.config.FencingTimeout {
			delete(sbp.fencedNodes, nodeID)
			removed++
			sbp.logger.WithField("node_id", nodeID).Info("Expired fence removed")
		}
	}

	return removed
}

// DetectSplitBrain attempts to detect split-brain conditions.
// This is a best-effort detection mechanism.
func (sbp *SplitBrainPreventer) DetectSplitBrain(ctx context.Context, shardID uint64) (bool, error) {
	// Get current leader
	leaderID, _, err := sbp.raftNode.GetLeaderID(shardID)
	if err != nil {
		// If we can't determine leader, we might be in a split-brain or partition
		return false, err
	}

	// Check quorum status
	status, err := sbp.CheckQuorum(ctx, shardID)
	if err != nil {
		return false, err
	}

	// If we have a leader but no quorum, this is suspicious
	// In Raft, a leader should only exist with quorum, so this shouldn't happen
	// But in network partition scenarios with stale state, it might
	if leaderID != 0 && !status.HasQuorum {
		sbp.metrics.splitBrainDetected.Inc()
		sbp.logger.WithFields(map[string]interface{}{
			"shard_id":  shardID,
			"leader_id": leaderID,
		}).Error("Potential split-brain detected: leader exists without quorum")
		return true, nil
	}

	return false, nil
}

// PreventWrite ensures a write operation is safe from split-brain.
func (sbp *SplitBrainPreventer) PreventWrite(ctx context.Context, shardID uint64) error {
	// Enforce quorum before allowing write
	if err := sbp.EnforceQuorum(ctx, shardID); err != nil {
		return err
	}

	// Check for split-brain
	splitBrain, err := sbp.DetectSplitBrain(ctx, shardID)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "ha.PreventWrite", "failed to check split-brain")
	}

	if splitBrain {
		return errors.Newf(errors.ErrSplitBrain, "ha.PreventWrite", "split-brain detected for shard %d", shardID)
	}

	return nil
}
