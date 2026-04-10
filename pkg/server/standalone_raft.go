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

package server

import (
	"context"
	"fmt"
	"sync/atomic"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// StandaloneModeError indicates an operation is unavailable in standalone mode.
type StandaloneModeError struct {
	Operation string
}

// Error implements the error interface.
func (e *StandaloneModeError) Error() string {
	return e.Operation + " unavailable in standalone mode"
}

// newStandaloneModeError creates a DragonError for standalone mode operations.
func newStandaloneModeError(op, operation string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrUnavailable,
		Op:   op,
		Err:  &StandaloneModeError{Operation: operation},
	}
}

// standaloneRaftNode implements interfaces.RaftNode for standalone (non-clustered) mode.
// All consensus, proposal, and cluster mutation operations return errors indicating
// standalone mode. Identity and health methods return configured values.
type standaloneRaftNode struct {
	id      string
	address string
	closed  atomic.Bool
}

// newStandaloneRaftNode creates a new standalone raft node with the given identity.
func newStandaloneRaftNode(nodeID uint64, address string) *standaloneRaftNode {
	return &standaloneRaftNode{
		id:      fmt.Sprintf("standalone-%d", nodeID),
		address: address,
	}
}

// ID returns the unique host identifier string.
func (n *standaloneRaftNode) ID() string {
	return n.id
}

// RaftAddress returns the host's Raft network address.
func (n *standaloneRaftNode) RaftAddress() string {
	return n.address
}

// ShardCount returns zero since standalone mode has no Raft shards.
func (n *standaloneRaftNode) ShardCount() int {
	return 0
}

// GetNodeHostInfo returns an empty node host info snapshot.
func (n *standaloneRaftNode) GetNodeHostInfo(_ interfaces.NodeHostInfoOption) *interfaces.NodeHostInfo {
	return &interfaces.NodeHostInfo{
		RaftAddress: n.address,
	}
}

// HealthCheck returns a healthy status for standalone mode.
func (n *standaloneRaftNode) HealthCheck() *interfaces.HealthStatus {
	status := "healthy"
	if n.closed.Load() {
		status = "closed"
	}
	return &interfaces.HealthStatus{
		Status:     status,
		DiskOK:     true,
		ShardCount: 0,
	}
}

// Drain is a no-op in standalone mode.
func (n *standaloneRaftNode) Drain() error {
	return nil
}

// IsDraining returns false since standalone mode does not support draining.
func (n *standaloneRaftNode) IsDraining() bool {
	return false
}

// SyncPropose returns an error since proposals require Raft consensus.
func (n *standaloneRaftNode) SyncPropose(_ context.Context, _ uint64, _ []byte) (interfaces.Result, error) {
	return interfaces.Result{}, newStandaloneModeError("standaloneRaftNode.SyncPropose", "proposal")
}

// ProposeWithMode returns an error since proposals require Raft consensus.
func (n *standaloneRaftNode) ProposeWithMode(_ context.Context, _ uint64, _ []byte, _ interfaces.WriteMode) (interfaces.Result, error) {
	return interfaces.Result{}, newStandaloneModeError("standaloneRaftNode.ProposeWithMode", "proposal")
}

// SyncRead returns an error since linearizable reads require Raft consensus.
func (n *standaloneRaftNode) SyncRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, newStandaloneModeError("standaloneRaftNode.SyncRead", "linearizable read")
}

// StaleRead returns an error since stale reads require a local state machine.
func (n *standaloneRaftNode) StaleRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, newStandaloneModeError("standaloneRaftNode.StaleRead", "stale read")
}

// ReadWithConsistency returns an error since reads require a state machine.
func (n *standaloneRaftNode) ReadWithConsistency(_ context.Context, _ uint64, _ interface{}, _ interfaces.ReadOptions) (interface{}, error) {
	return nil, newStandaloneModeError("standaloneRaftNode.ReadWithConsistency", "consistency read")
}

// GetLeaderID returns zero and false since there is no leader in standalone mode.
func (n *standaloneRaftNode) GetLeaderID(_ uint64) (uint64, bool, error) {
	return 0, false, newStandaloneModeError("standaloneRaftNode.GetLeaderID", "leader lookup")
}

// GetShardRaftState returns an error since there are no shards in standalone mode.
func (n *standaloneRaftNode) GetShardRaftState(_ uint64) (uint64, uint64, error) {
	return 0, 0, newStandaloneModeError("standaloneRaftNode.GetShardRaftState", "shard state lookup")
}

// RequestAddNode returns an error since cluster operations require Raft consensus.
func (n *standaloneRaftNode) RequestAddNode(_ context.Context, _, _ uint64, _ string, _ uint64) (interfaces.RequestState, error) {
	return nil, newStandaloneModeError("standaloneRaftNode.RequestAddNode", "cluster membership change")
}

// RequestRemoveNode returns an error since cluster operations require Raft consensus.
func (n *standaloneRaftNode) RequestRemoveNode(_ context.Context, _, _ uint64, _ uint64) (interfaces.RequestState, error) {
	return nil, newStandaloneModeError("standaloneRaftNode.RequestRemoveNode", "cluster membership change")
}

// RequestSnapshot returns an error since snapshots require Raft consensus.
func (n *standaloneRaftNode) RequestSnapshot(_ context.Context, _ uint64) (interfaces.RequestState, error) {
	return nil, newStandaloneModeError("standaloneRaftNode.RequestSnapshot", "snapshot")
}

// StartShard returns an error since shard management requires Raft consensus.
func (n *standaloneRaftNode) StartShard(_ map[uint64]string, _ bool, _ interfaces.CreateFunc, _ interfaces.ShardConfig) error {
	return newStandaloneModeError("standaloneRaftNode.StartShard", "shard management")
}

// StopShard returns an error since shard management requires Raft consensus.
func (n *standaloneRaftNode) StopShard(_ uint64) error {
	return newStandaloneModeError("standaloneRaftNode.StopShard", "shard management")
}

// Close marks the standalone raft node as closed.
func (n *standaloneRaftNode) Close() error {
	n.closed.Store(true)
	return nil
}
