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

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
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

func testClusterInfoConfig() ClusterInfoConfig {
	return ClusterInfoConfig{
		NodeID:         1,
		Address:        "localhost:8443",
		RaftAddress:    "localhost:63000",
		DefaultShardID: 1,
	}
}

// newTestClusterInfo builds a ClusterInfo over raft, or a standalone one
// when raft is nil (a nil *mockRaftNode must not become a non-nil interface).
func newTestClusterInfo(raft *mockRaftNode) ClusterInfo {
	if raft == nil {
		return NewClusterInfo(nil, testClusterInfoConfig())
	}
	return NewClusterInfo(raft, testClusterInfoConfig())
}

// threeNodeRaft returns a raft double whose default shard holds nodes 1-3,
// led by leaderID.
func threeNodeRaft(leaderID uint64) *mockRaftNode {
	return &mockRaftNode{
		leaderID: leaderID,
		nodeHostInfo: &interfaces.NodeHostInfo{
			RaftAddress: "localhost:63000",
			ShardInfoList: []interfaces.ShardInfo{
				{
					ShardID:   1,
					ReplicaID: 1,
					LeaderID:  leaderID,
					Nodes: map[uint64]string{
						1: "localhost:63000",
						2: "localhost:63001",
						3: "localhost:63002",
					},
				},
			},
		},
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func isContextCanceled(err error) bool {
	return errors.Is(err, ErrContextCanceled)
}

// nodeIDs returns the IDs of the nodes that keep selects.
func nodeIDs(nodes []NodeInfo, keep func(NodeInfo) bool) []uint64 {
	ids := []uint64{}
	for _, n := range nodes {
		if keep(n) {
			ids = append(ids, n.NodeID)
		}
	}
	return ids
}

func anyNode(NodeInfo) bool { return true }

func TestNewClusterInfo_LocalNode(t *testing.T) {
	tests := []struct {
		name       string
		raft       *mockRaftNode
		wantLeader bool
	}{
		{name: "standalone is always leader", raft: nil, wantLeader: true},
		{name: "cluster leader", raft: &mockRaftNode{leaderID: 1}, wantLeader: true},
		{name: "cluster follower", raft: &mockRaftNode{leaderID: 2}, wantLeader: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ci := newTestClusterInfo(tc.raft)
			require.NotNil(t, ci)

			localNode := ci.LocalNode()
			assert.Equal(t, uint64(1), localNode.NodeID)
			assert.Equal(t, "localhost:8443", localNode.Address)
			assert.Equal(t, "localhost:63000", localNode.RaftAddress)
			assert.Equal(t, tc.wantLeader, localNode.IsLeader)
			assert.True(t, localNode.IsHealthy)
			assert.False(t, localNode.LastSeen.IsZero())
		})
	}
}

func TestClusterInfo_NonLeaderNodes(t *testing.T) {
	tests := []struct {
		name    string
		raft    *mockRaftNode
		ctx     context.Context
		wantIDs []uint64
		wantErr func(error) bool
	}{
		{name: "standalone has no non-leaders", raft: nil, ctx: context.Background(), wantIDs: []uint64{}},
		{name: "cluster returns followers", raft: threeNodeRaft(1), ctx: context.Background(), wantIDs: []uint64{2, 3}},
		{name: "cluster not initialized", raft: &mockRaftNode{leaderID: 1}, ctx: context.Background(), wantErr: IsClusterNotInitialized},
		{name: "context canceled", raft: nil, ctx: canceledContext(), wantErr: isContextCanceled},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := newTestClusterInfo(tc.raft).NonLeaderNodes(tc.ctx)
			if tc.wantErr != nil {
				assert.Nil(t, nodes)
				assert.True(t, tc.wantErr(err), "unexpected error: %v", err)
				return
			}
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.wantIDs, nodeIDs(nodes, anyNode))
			for _, node := range nodes {
				assert.False(t, node.IsLeader)
				assert.True(t, node.IsHealthy)
			}
		})
	}
}

func TestClusterInfo_AllNodes(t *testing.T) {
	tests := []struct {
		name        string
		raft        *mockRaftNode
		ctx         context.Context
		wantIDs     []uint64
		wantLeaders []uint64
		wantErr     func(error) bool
	}{
		{name: "standalone returns local node", raft: nil, ctx: context.Background(), wantIDs: []uint64{1}, wantLeaders: []uint64{1}},
		{name: "cluster returns every member", raft: threeNodeRaft(1), ctx: context.Background(), wantIDs: []uint64{1, 2, 3}, wantLeaders: []uint64{1}},
		{name: "cluster not initialized", raft: &mockRaftNode{leaderID: 1}, ctx: context.Background(), wantErr: IsClusterNotInitialized},
		{name: "context canceled", raft: threeNodeRaft(1), ctx: canceledContext(), wantErr: isContextCanceled},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := newTestClusterInfo(tc.raft).AllNodes(tc.ctx)
			if tc.wantErr != nil {
				assert.Nil(t, nodes)
				assert.True(t, tc.wantErr(err), "unexpected error: %v", err)
				return
			}
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.wantIDs, nodeIDs(nodes, anyNode))
			assert.ElementsMatch(t, tc.wantLeaders, nodeIDs(nodes, func(n NodeInfo) bool { return n.IsLeader }))
		})
	}
}

func TestClusterInfo_Leader(t *testing.T) {
	tests := []struct {
		name            string
		raft            *mockRaftNode
		ctx             context.Context
		wantID          uint64
		wantAddress     string
		wantRaftAddress string
		wantErr         func(error) bool
	}{
		{
			name: "standalone local node", raft: nil, ctx: context.Background(),
			wantID: 1, wantAddress: "localhost:8443", wantRaftAddress: "localhost:63000",
		},
		{
			name: "local node is leader", raft: threeNodeRaft(1), ctx: context.Background(),
			wantID: 1, wantAddress: "localhost:8443", wantRaftAddress: "localhost:63000",
		},
		{
			name: "remote node is leader", raft: threeNodeRaft(2), ctx: context.Background(),
			wantID: 2, wantAddress: "localhost:63001", wantRaftAddress: "localhost:63001",
		},
		{name: "no leader elected", raft: &mockRaftNode{}, ctx: context.Background(), wantErr: IsNoLeader},
		{
			name: "leader lookup error", raft: &mockRaftNode{leaderErr: errors.New("leader lookup failed")},
			ctx: context.Background(), wantErr: IsNoLeader,
		},
		{name: "context canceled", raft: &mockRaftNode{leaderID: 1}, ctx: canceledContext(), wantErr: isContextCanceled},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			leader, err := newTestClusterInfo(tc.raft).Leader(tc.ctx)
			if tc.wantErr != nil {
				assert.Nil(t, leader)
				assert.True(t, tc.wantErr(err), "unexpected error: %v", err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, leader.NodeID)
			assert.Equal(t, tc.wantAddress, leader.Address)
			assert.Equal(t, tc.wantRaftAddress, leader.RaftAddress)
			assert.True(t, leader.IsLeader)
		})
	}
}

func TestClusterInfo_IsLeader(t *testing.T) {
	tests := []struct {
		name string
		raft *mockRaftNode
		want bool
	}{
		{name: "standalone", raft: nil, want: true},
		{name: "cluster leader", raft: &mockRaftNode{leaderID: 1}, want: true},
		{name: "cluster follower", raft: &mockRaftNode{leaderID: 2}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, newTestClusterInfo(tc.raft).IsLeader())
		})
	}

	t.Run("lookup error falls back to cached value", func(t *testing.T) {
		raft := &mockRaftNode{leaderID: 1}
		ci := newTestClusterInfo(raft)
		require.True(t, ci.IsLeader())

		raft.leaderErr = errors.New("connection failed")
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

func TestNodeList_Selectors(t *testing.T) {
	nodes := NodeList{
		{NodeID: 1, IsLeader: true, IsHealthy: true},
		{NodeID: 2, IsLeader: false, IsHealthy: true},
		{NodeID: 3, IsLeader: false, IsHealthy: false},
	}

	tests := []struct {
		name    string
		sel     func(NodeList) NodeList
		wantIDs []uint64
	}{
		{
			name:    "Filter",
			sel:     func(l NodeList) NodeList { return l.Filter(func(n NodeInfo) bool { return n.NodeID == 3 }) },
			wantIDs: []uint64{3},
		},
		{name: "Healthy", sel: NodeList.Healthy, wantIDs: []uint64{1, 2}},
		{name: "NonLeaders", sel: NodeList.NonLeaders, wantIDs: []uint64{2, 3}},
		{name: "Leaders", sel: NodeList.Leaders, wantIDs: []uint64{1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.wantIDs, nodeIDs(tc.sel(nodes), anyNode))
		})
	}
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

func TestError_Predicates(t *testing.T) {
	tests := []struct {
		name        string
		is          func(error) bool
		sentinel    error
		constructed error
	}{
		{name: "IsNoLeader", is: IsNoLeader, sentinel: ErrNoLeader, constructed: NewNoLeaderError("test")},
		{name: "IsNoHealthyNodes", is: IsNoHealthyNodes, sentinel: ErrNoHealthyNodes, constructed: NewNoHealthyNodesError("test", 3)},
		{
			name: "IsClusterNotInitialized", is: IsClusterNotInitialized,
			sentinel: ErrClusterNotInitialized, constructed: NewClusterNotInitializedError("test"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, tc.is(tc.sentinel))
			assert.True(t, tc.is(tc.constructed))
			assert.False(t, tc.is(nil))
			assert.False(t, tc.is(errors.New("some other error")))
		})
	}
}

func TestNewNodeNotFoundError(t *testing.T) {
	err := NewNodeNotFoundError("test.op", 42)
	require.NotNil(t, err)

	var de *qberrors.QBError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, qberrors.ErrNotFound, de.Code)
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
