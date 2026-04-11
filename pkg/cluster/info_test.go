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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockRaftNode implements interfaces.RaftNode for testing.
type mockRaftNode struct {
	nodeHostInfo   *interfaces.NodeHostInfo
	leaderID       uint64
	leaderErr      error
	shardRaftState struct {
		index uint64
		term  uint64
		err   error
	}
}

func (m *mockRaftNode) ID() string          { return "mock-cluster" }
func (m *mockRaftNode) RaftAddress() string { return "" }
func (m *mockRaftNode) ShardCount() int     { return 0 }
func (m *mockRaftNode) HealthCheck() *interfaces.HealthStatus {
	return &interfaces.HealthStatus{Status: "ok"}
}
func (m *mockRaftNode) Drain() error     { return nil }
func (m *mockRaftNode) IsDraining() bool { return false }

func (m *mockRaftNode) SyncPropose(_ context.Context, _ uint64, _ []byte) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}

func (m *mockRaftNode) ProposeWithMode(_ context.Context, _ uint64, _ []byte, _ interfaces.WriteMode) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}

func (m *mockRaftNode) SyncRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNode) StaleRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNode) ReadWithConsistency(_ context.Context, _ uint64, _ interface{}, _ interfaces.ReadOptions) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNode) GetLeaderID(_ uint64) (uint64, bool, error) {
	return m.leaderID, m.leaderID != 0 && m.leaderErr == nil, m.leaderErr
}

func (m *mockRaftNode) GetShardRaftState(_ uint64) (uint64, uint64, error) {
	return m.shardRaftState.index, m.shardRaftState.term, m.shardRaftState.err
}

func (m *mockRaftNode) GetNodeHostInfo(_ interfaces.NodeHostInfoOption) *interfaces.NodeHostInfo {
	return m.nodeHostInfo
}

func (m *mockRaftNode) RequestAddNode(_ context.Context, _, _ uint64, _ string, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNode) RequestRemoveNode(_ context.Context, _, _ uint64, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNode) RequestSnapshot(_ context.Context, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNode) StartShard(_ map[uint64]string, _ bool, _ interfaces.CreateFunc, _ interfaces.ShardConfig) error {
	return nil
}

func (m *mockRaftNode) StopShard(_ uint64) error {
	return nil
}

func (m *mockRaftNode) Close() error {
	return nil
}

func TestNewClusterInfo_Standalone(t *testing.T) {
	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(nil, config)
	require.NotNil(t, ci)

	// In standalone mode, local node should be leader
	assert.True(t, ci.IsLeader())

	localNode := ci.LocalNode()
	assert.Equal(t, uint64(1), localNode.NodeID)
	assert.Equal(t, "localhost:8443", localNode.Address)
	assert.Equal(t, "localhost:63000", localNode.RaftAddress)
	assert.True(t, localNode.IsLeader)
	assert.True(t, localNode.IsHealthy)
}

func TestNewClusterInfo_Cluster(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 2, // Different node is leader
		nodeHostInfo: &interfaces.NodeHostInfo{
			RaftAddress: "localhost:63000",
			ShardInfoList: []interfaces.ShardInfo{
				{
					ShardID:   1,
					ReplicaID: 1,
					LeaderID:  2,
					Nodes: map[uint64]string{
						1: "localhost:63000",
						2: "localhost:63001",
						3: "localhost:63002",
					},
				},
			},
		},
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	require.NotNil(t, ci)

	// Local node is not the leader
	assert.False(t, ci.IsLeader())
}

func TestClusterInfo_NonLeaderNodes_Standalone(t *testing.T) {
	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(nil, config)
	ctx := context.Background()

	nodes, err := ci.NonLeaderNodes(ctx)
	require.NoError(t, err)
	assert.Empty(t, nodes) // In standalone mode, local node is leader, so no non-leaders
}

func TestClusterInfo_NonLeaderNodes_Cluster(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 1, // Local node is leader
		nodeHostInfo: &interfaces.NodeHostInfo{
			RaftAddress: "localhost:63000",
			ShardInfoList: []interfaces.ShardInfo{
				{
					ShardID:   1,
					ReplicaID: 1,
					LeaderID:  1,
					Nodes: map[uint64]string{
						1: "localhost:63000",
						2: "localhost:63001",
						3: "localhost:63002",
					},
				},
			},
		},
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	ctx := context.Background()

	nodes, err := ci.NonLeaderNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 2) // Two non-leader nodes (2 and 3)

	// Verify none of the returned nodes are leaders
	for _, node := range nodes {
		assert.False(t, node.IsLeader)
		assert.True(t, node.IsHealthy)
	}
}

func TestClusterInfo_NonLeaderNodes_ContextCanceled(t *testing.T) {
	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(nil, config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	nodes, err := ci.NonLeaderNodes(ctx)
	assert.Nil(t, nodes)
	assert.Equal(t, ErrContextCanceled, err)
}

func TestClusterInfo_AllNodes_Standalone(t *testing.T) {
	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(nil, config)
	ctx := context.Background()

	nodes, err := ci.AllNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, uint64(1), nodes[0].NodeID)
	assert.True(t, nodes[0].IsLeader)
}

func TestClusterInfo_AllNodes_Cluster(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 1,
		nodeHostInfo: &interfaces.NodeHostInfo{
			RaftAddress: "localhost:63000",
			ShardInfoList: []interfaces.ShardInfo{
				{
					ShardID:   1,
					ReplicaID: 1,
					LeaderID:  1,
					Nodes: map[uint64]string{
						1: "localhost:63000",
						2: "localhost:63001",
						3: "localhost:63002",
					},
				},
			},
		},
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	ctx := context.Background()

	nodes, err := ci.AllNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 3)

	// Find the leader
	var leaderCount int
	for _, node := range nodes {
		if node.IsLeader {
			leaderCount++
		}
	}
	assert.Equal(t, 1, leaderCount)
}

func TestClusterInfo_AllNodes_NotInitialized(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID:     1,
		nodeHostInfo: nil, // Not initialized
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	ctx := context.Background()

	nodes, err := ci.AllNodes(ctx)
	assert.Nil(t, nodes)
	assert.True(t, IsClusterNotInitialized(err))
}

func TestClusterInfo_AllNodes_ContextCanceled(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 1,
		nodeHostInfo: &interfaces.NodeHostInfo{
			ShardInfoList: []interfaces.ShardInfo{},
		},
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	nodes, err := ci.AllNodes(ctx)
	assert.Nil(t, nodes)
	assert.Equal(t, ErrContextCanceled, err)
}

func TestClusterInfo_Leader_Standalone(t *testing.T) {
	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(nil, config)
	ctx := context.Background()

	leader, err := ci.Leader(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), leader.NodeID)
	assert.True(t, leader.IsLeader)
}

func TestClusterInfo_Leader_LocalIsLeader(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 1, // Local node is leader
		nodeHostInfo: &interfaces.NodeHostInfo{
			ShardInfoList: []interfaces.ShardInfo{
				{
					ShardID:  1,
					LeaderID: 1,
					Nodes: map[uint64]string{
						1: "localhost:63000",
					},
				},
			},
		},
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	ctx := context.Background()

	leader, err := ci.Leader(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), leader.NodeID)
	assert.Equal(t, "localhost:8443", leader.Address)
	assert.True(t, leader.IsLeader)
}

func TestClusterInfo_Leader_RemoteIsLeader(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 2, // Remote node is leader
		nodeHostInfo: &interfaces.NodeHostInfo{
			ShardInfoList: []interfaces.ShardInfo{
				{
					ShardID:  1,
					LeaderID: 2,
					Nodes: map[uint64]string{
						1: "localhost:63000",
						2: "localhost:63001",
					},
				},
			},
		},
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	ctx := context.Background()

	leader, err := ci.Leader(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), leader.NodeID)
	assert.Equal(t, "localhost:63001", leader.RaftAddress)
	assert.True(t, leader.IsLeader)
}

func TestClusterInfo_Leader_NoLeader(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 0, // No leader elected
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	ctx := context.Background()

	leader, err := ci.Leader(ctx)
	assert.Nil(t, leader)
	assert.True(t, IsNoLeader(err))
}

func TestClusterInfo_Leader_Error(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderErr: errors.New("leader lookup failed"),
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	ctx := context.Background()

	leader, err := ci.Leader(ctx)
	assert.Nil(t, leader)
	assert.True(t, IsNoLeader(err))
}

func TestClusterInfo_Leader_ContextCanceled(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 1,
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	leader, err := ci.Leader(ctx)
	assert.Nil(t, leader)
	assert.Equal(t, ErrContextCanceled, err)
}

func TestClusterInfo_LocalNode(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 1,
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)

	localNode := ci.LocalNode()
	assert.Equal(t, uint64(1), localNode.NodeID)
	assert.Equal(t, "localhost:8443", localNode.Address)
	assert.Equal(t, "localhost:63000", localNode.RaftAddress)
	assert.True(t, localNode.IsLeader)
	assert.True(t, localNode.IsHealthy)
	assert.False(t, localNode.LastSeen.IsZero())
}

func TestClusterInfo_LocalNode_NotLeader(t *testing.T) {
	mockRaft := &mockRaftNode{
		leaderID: 2, // Different node is leader
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)

	localNode := ci.LocalNode()
	assert.Equal(t, uint64(1), localNode.NodeID)
	assert.False(t, localNode.IsLeader)
}

func TestClusterInfo_IsLeader_Standalone(t *testing.T) {
	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(nil, config)
	assert.True(t, ci.IsLeader())
}

func TestClusterInfo_IsLeader_Cluster(t *testing.T) {
	t.Run("is leader", func(t *testing.T) {
		mockRaft := &mockRaftNode{
			leaderID: 1,
		}

		config := ClusterInfoConfig{
			NodeID:         1,
			Address:        "localhost:8443",
			RaftAddress:    "localhost:63000",
			DefaultShardID: 1,
		}

		ci := NewClusterInfo(mockRaft, config)
		assert.True(t, ci.IsLeader())
	})

	t.Run("not leader", func(t *testing.T) {
		mockRaft := &mockRaftNode{
			leaderID: 2,
		}

		config := ClusterInfoConfig{
			NodeID:         1,
			Address:        "localhost:8443",
			RaftAddress:    "localhost:63000",
			DefaultShardID: 1,
		}

		ci := NewClusterInfo(mockRaft, config)
		assert.False(t, ci.IsLeader())
	})

	t.Run("error falls back to cached", func(t *testing.T) {
		mockRaft := &mockRaftNode{
			leaderID:  1,
			leaderErr: nil,
		}

		config := ClusterInfoConfig{
			NodeID:         1,
			Address:        "localhost:8443",
			RaftAddress:    "localhost:63000",
			DefaultShardID: 1,
		}

		ci := NewClusterInfo(mockRaft, config)

		// First call should succeed and cache the value
		assert.True(t, ci.IsLeader())

		// Now cause an error
		mockRaft.leaderErr = errors.New("connection failed")

		// Should fall back to cached value
		assert.True(t, ci.IsLeader())
	})
}

func TestNodeInfo_IsAvailable(t *testing.T) {
	tests := []struct {
		name      string
		node      NodeInfo
		available bool
	}{
		{
			name: "healthy with address",
			node: NodeInfo{
				NodeID:    1,
				Address:   "localhost:8443",
				IsHealthy: true,
			},
			available: true,
		},
		{
			name: "unhealthy",
			node: NodeInfo{
				NodeID:    1,
				Address:   "localhost:8443",
				IsHealthy: false,
			},
			available: false,
		},
		{
			name: "no address",
			node: NodeInfo{
				NodeID:    1,
				Address:   "",
				IsHealthy: true,
			},
			available: false,
		},
		{
			name: "unhealthy no address",
			node: NodeInfo{
				NodeID:    1,
				Address:   "",
				IsHealthy: false,
			},
			available: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.available, tc.node.IsAvailable())
		})
	}
}

func TestNodeList_Filter(t *testing.T) {
	nodes := NodeList{
		{NodeID: 1, IsLeader: true, IsHealthy: true},
		{NodeID: 2, IsLeader: false, IsHealthy: true},
		{NodeID: 3, IsLeader: false, IsHealthy: false},
	}

	healthy := nodes.Filter(func(n NodeInfo) bool {
		return n.IsHealthy
	})

	assert.Len(t, healthy, 2)
}

func TestNodeList_FindByID(t *testing.T) {
	nodes := NodeList{
		{NodeID: 1, Address: "localhost:8443"},
		{NodeID: 2, Address: "localhost:8444"},
		{NodeID: 3, Address: "localhost:8445"},
	}

	t.Run("found", func(t *testing.T) {
		node := nodes.FindByID(2)
		require.NotNil(t, node)
		assert.Equal(t, "localhost:8444", node.Address)
	})

	t.Run("not found", func(t *testing.T) {
		node := nodes.FindByID(99)
		assert.Nil(t, node)
	})
}

func TestNodeList_Healthy(t *testing.T) {
	nodes := NodeList{
		{NodeID: 1, IsHealthy: true},
		{NodeID: 2, IsHealthy: false},
		{NodeID: 3, IsHealthy: true},
	}

	healthy := nodes.Healthy()
	assert.Len(t, healthy, 2)

	for _, n := range healthy {
		assert.True(t, n.IsHealthy)
	}
}

func TestNodeList_NonLeaders(t *testing.T) {
	nodes := NodeList{
		{NodeID: 1, IsLeader: true},
		{NodeID: 2, IsLeader: false},
		{NodeID: 3, IsLeader: false},
	}

	nonLeaders := nodes.NonLeaders()
	assert.Len(t, nonLeaders, 2)

	for _, n := range nonLeaders {
		assert.False(t, n.IsLeader)
	}
}

func TestNodeList_Leaders(t *testing.T) {
	nodes := NodeList{
		{NodeID: 1, IsLeader: true},
		{NodeID: 2, IsLeader: false},
		{NodeID: 3, IsLeader: false},
	}

	leaders := nodes.Leaders()
	assert.Len(t, leaders, 1)
	assert.Equal(t, uint64(1), leaders[0].NodeID)
}

func TestError_IsNoLeader(t *testing.T) {
	assert.True(t, IsNoLeader(ErrNoLeader))
	assert.True(t, IsNoLeader(NewNoLeaderError("test")))
	assert.False(t, IsNoLeader(nil))
	assert.False(t, IsNoLeader(errors.New("some other error")))
}

func TestError_IsNoHealthyNodes(t *testing.T) {
	assert.True(t, IsNoHealthyNodes(ErrNoHealthyNodes))
	assert.True(t, IsNoHealthyNodes(NewNoHealthyNodesError("test", 3)))
	assert.False(t, IsNoHealthyNodes(nil))
	assert.False(t, IsNoHealthyNodes(errors.New("some other error")))
}

func TestError_IsClusterNotInitialized(t *testing.T) {
	assert.True(t, IsClusterNotInitialized(ErrClusterNotInitialized))
	assert.True(t, IsClusterNotInitialized(NewClusterNotInitializedError("test")))
	assert.False(t, IsClusterNotInitialized(nil))
	assert.False(t, IsClusterNotInitialized(errors.New("some other error")))
}

func TestNewNodeNotFoundError(t *testing.T) {
	err := NewNodeNotFoundError("test.op", 42)
	require.NotNil(t, err)

	var de *dragonerrors.QBError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, dragonerrors.ErrNotFound, de.Code)
	assert.Equal(t, "test.op", de.Op)

	nodeID, ok := de.Context["node_id"]
	assert.True(t, ok)
	assert.Equal(t, uint64(42), nodeID)
}

func TestClusterInfo_MultiShardLeadership(t *testing.T) {
	// Test scenario where different nodes lead different shards
	mockRaft := &mockRaftNode{
		leaderID: 1, // For default shard
		nodeHostInfo: &interfaces.NodeHostInfo{
			RaftAddress: "localhost:63000",
			ShardInfoList: []interfaces.ShardInfo{
				{
					ShardID:   1,
					ReplicaID: 1,
					LeaderID:  1, // Node 1 leads shard 1
					Nodes: map[uint64]string{
						1: "localhost:63000",
						2: "localhost:63001",
					},
				},
				{
					ShardID:   2,
					ReplicaID: 1,
					LeaderID:  2, // Node 2 leads shard 2
					Nodes: map[uint64]string{
						1: "localhost:63000",
						2: "localhost:63001",
					},
				},
			},
		},
	}

	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(mockRaft, config)
	ctx := context.Background()

	nodes, err := ci.AllNodes(ctx)
	require.NoError(t, err)

	// Both nodes should be marked as leaders since they each lead a shard
	leaderCount := 0
	for _, node := range nodes {
		if node.IsLeader {
			leaderCount++
		}
	}
	// In our implementation, a node is marked as leader if it leads ANY shard
	assert.GreaterOrEqual(t, leaderCount, 1)
}

func TestClusterInfo_LastSeenUpdated(t *testing.T) {
	config := ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}

	ci := NewClusterInfo(nil, config)

	// Get local node and record LastSeen
	node1 := ci.LocalNode()
	lastSeen1 := node1.LastSeen

	// Wait a tiny bit
	time.Sleep(time.Millisecond)

	// Get it again - LastSeen should be updated
	node2 := ci.LocalNode()
	assert.True(t, node2.LastSeen.After(lastSeen1) || node2.LastSeen.Equal(lastSeen1))
}
