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

package cluster

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Ensure clusterInfo implements ClusterInfo interface.
var _ ClusterInfo = (*clusterInfo)(nil)

// ClusterInfoConfig contains configuration for the ClusterInfo implementation.
type ClusterInfoConfig struct {
	// NodeID is the unique identifier for the local node.
	NodeID uint64

	// Address is the client-facing address for this node.
	Address string

	// RaftAddress is the internal Raft address for this node.
	RaftAddress string

	// DefaultShardID is the shard ID to use for leader determination.
	// In multi-shard setups, leadership is per-shard, but for simple
	// single-shard deployments this determines the "cluster leader".
	DefaultShardID uint64
}

// clusterInfo implements ClusterInfo using the RaftNode interface.
type clusterInfo struct {
	raftNode       interfaces.RaftNode
	config         ClusterInfoConfig
	localNode      *NodeInfo
	isLeader       atomic.Bool
	defaultShardID uint64
}

// NewClusterInfo creates a new ClusterInfo implementation.
// If raftNode is nil, the cluster operates in standalone mode where only
// the local node is known and it is always considered the leader.
func NewClusterInfo(raftNode interfaces.RaftNode, config ClusterInfoConfig) ClusterInfo {
	localNode := &NodeInfo{
		NodeID:      config.NodeID,
		Address:     config.Address,
		RaftAddress: config.RaftAddress,
		IsLeader:    raftNode == nil, // Standalone mode is always leader
		IsHealthy:   true,
		LastSeen:    time.Now(),
	}

	ci := &clusterInfo{
		raftNode:       raftNode,
		config:         config,
		localNode:      localNode,
		defaultShardID: config.DefaultShardID,
	}

	// In standalone mode, we're always the leader
	if raftNode == nil {
		ci.isLeader.Store(true)
	}

	return ci
}

// NonLeaderNodes returns all healthy non-leader nodes for load balancer routing.
func (c *clusterInfo) NonLeaderNodes(ctx context.Context) ([]NodeInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ErrContextCanceled
	default:
	}

	// In standalone mode, return empty slice since local node is the only node and is leader
	if c.raftNode == nil {
		return []NodeInfo{}, nil
	}

	allNodes, err := c.AllNodes(ctx)
	if err != nil {
		return nil, err
	}

	// Filter to healthy non-leader nodes
	result := make([]NodeInfo, 0, len(allNodes))
	for _, node := range allNodes {
		if !node.IsLeader && node.IsHealthy {
			result = append(result, node)
		}
	}

	return result, nil
}

// AllNodes returns all nodes in the cluster.
func (c *clusterInfo) AllNodes(ctx context.Context) ([]NodeInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ErrContextCanceled
	default:
	}

	// In standalone mode, return only the local node
	if c.raftNode == nil {
		return []NodeInfo{*c.localNode}, nil
	}

	nodeHostInfo := c.raftNode.GetNodeHostInfo(interfaces.NodeHostInfoOption{})
	if nodeHostInfo == nil {
		return nil, NewClusterNotInitializedError("cluster.AllNodes")
	}

	return c.convertNodeHostInfo(nodeHostInfo), nil
}

// Leader returns the current cluster leader.
func (c *clusterInfo) Leader(ctx context.Context) (*NodeInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ErrContextCanceled
	default:
	}

	// In standalone mode, local node is always the leader
	if c.raftNode == nil {
		return c.localNode, nil
	}

	// Get leader ID from the default shard
	leaderID, _, err := c.raftNode.GetLeaderID(c.defaultShardID)
	if err != nil {
		return nil, NewNoLeaderError("cluster.Leader")
	}

	if leaderID == 0 {
		return nil, NewNoLeaderError("cluster.Leader")
	}

	// If local node is the leader, return it directly
	if leaderID == c.config.NodeID {
		c.isLeader.Store(true)
		c.localNode.IsLeader = true
		c.localNode.LastSeen = time.Now()
		return c.localNode, nil
	}

	c.isLeader.Store(false)
	c.localNode.IsLeader = false

	// Find the leader in the cluster
	nodeHostInfo := c.raftNode.GetNodeHostInfo(interfaces.NodeHostInfoOption{})
	if nodeHostInfo == nil {
		return nil, NewClusterNotInitializedError("cluster.Leader")
	}

	// Search through shards to find the leader's address
	for _, shardInfo := range nodeHostInfo.ShardInfoList {
		if shardInfo.ShardID == c.defaultShardID {
			if addr, ok := shardInfo.Nodes[leaderID]; ok {
				return &NodeInfo{
					NodeID:      leaderID,
					Address:     addr, // Note: This is Raft address, caller may need to map to client address
					RaftAddress: addr,
					IsLeader:    true,
					IsHealthy:   true,
					LastSeen:    time.Now(),
				}, nil
			}
		}
	}

	return nil, NewNoLeaderError("cluster.Leader")
}

// LocalNode returns information about the local node.
func (c *clusterInfo) LocalNode() *NodeInfo {
	// Update leader status before returning
	if c.raftNode != nil {
		leaderID, _, err := c.raftNode.GetLeaderID(c.defaultShardID)
		if err == nil && leaderID == c.config.NodeID {
			c.isLeader.Store(true)
			c.localNode.IsLeader = true
		} else {
			c.isLeader.Store(false)
			c.localNode.IsLeader = false
		}
	}
	c.localNode.LastSeen = time.Now()
	return c.localNode
}

// IsLeader returns true if the local node is the current Raft leader.
func (c *clusterInfo) IsLeader() bool {
	// In standalone mode, always return true
	if c.raftNode == nil {
		return true
	}

	// Check leader status for the default shard
	leaderID, _, err := c.raftNode.GetLeaderID(c.defaultShardID)
	if err != nil {
		// If we can't determine leadership, use cached value
		return c.isLeader.Load()
	}

	isLeader := leaderID == c.config.NodeID
	c.isLeader.Store(isLeader)
	c.localNode.IsLeader = isLeader
	return isLeader
}

// convertNodeHostInfo converts QuicRaft NodeHostInfo to a slice of NodeInfo.
func (c *clusterInfo) convertNodeHostInfo(info *interfaces.NodeHostInfo) []NodeInfo {
	if info == nil {
		return nil
	}

	// Build a map of all known nodes from all shards
	nodeMap := make(map[uint64]*NodeInfo)

	// Add local node first
	nodeMap[c.config.NodeID] = &NodeInfo{
		NodeID:      c.config.NodeID,
		Address:     c.config.Address,
		RaftAddress: c.config.RaftAddress,
		IsLeader:    false, // Will be updated below
		IsHealthy:   true,
		LastSeen:    time.Now(),
	}

	// Collect nodes from all shard infos
	for _, shardInfo := range info.ShardInfoList {
		// Determine leader for this shard
		leaderID := shardInfo.LeaderID

		for replicaID, raftAddr := range shardInfo.Nodes {
			if existing, ok := nodeMap[replicaID]; ok {
				// Update existing node - it's the leader if it leads any shard
				if replicaID == leaderID {
					existing.IsLeader = true
				}
			} else {
				// Add new node
				nodeMap[replicaID] = &NodeInfo{
					NodeID:      replicaID,
					Address:     raftAddr, // Use raft address as default, caller may need to map
					RaftAddress: raftAddr,
					IsLeader:    replicaID == leaderID,
					IsHealthy:   true, // Assume healthy if in membership
					LastSeen:    time.Now(),
				}
			}
		}
	}

	// Convert map to slice
	result := make([]NodeInfo, 0, len(nodeMap))
	for _, node := range nodeMap {
		result = append(result, *node)
	}

	return result
}
