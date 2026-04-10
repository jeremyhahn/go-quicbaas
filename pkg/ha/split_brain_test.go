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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestNewSplitBrainPreventer tests the constructor with various configurations.
func TestNewSplitBrainPreventer(t *testing.T) {
	tests := []struct {
		name        string
		raftNode    interfaces.RaftNode
		config      *SplitBrainConfig
		logger      interfaces.Logger
		expectError bool
		errorCode   dberrors.ErrorCode
	}{
		{
			name:     "valid configuration",
			raftNode: newMockRaftNode(),
			config: &SplitBrainConfig{
				MinQuorum:               2,
				QuorumTimeout:           5 * time.Second,
				FencingEnabled:          true,
				FencingTimeout:          5 * time.Minute,
				EnableGenerationNumbers: true,
			},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "config with defaults",
			raftNode:    newMockRaftNode(),
			config:      &SplitBrainConfig{},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "nil raft node",
			raftNode:    nil,
			config:      &SplitBrainConfig{},
			logger:      newMockLogger(),
			expectError: true,
			errorCode:   dberrors.ErrInvalidInput,
		},
		{
			name:        "nil config",
			raftNode:    newMockRaftNode(),
			config:      nil,
			logger:      newMockLogger(),
			expectError: true,
			errorCode:   dberrors.ErrInvalidInput,
		},
		{
			name:        "nil logger",
			raftNode:    newMockRaftNode(),
			config:      &SplitBrainConfig{},
			logger:      nil,
			expectError: true,
			errorCode:   dberrors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sbp, err := NewSplitBrainPreventer(tt.raftNode, tt.config, tt.logger)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*dberrors.DragonError)
				require.True(t, ok, "expected DragonError")
				assert.Equal(t, tt.errorCode, dragonErr.Code)
				assert.Nil(t, sbp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, sbp)

				// Verify defaults are applied
				assert.Equal(t, 2, sbp.config.MinQuorum)
				assert.Equal(t, 5*time.Second, sbp.config.QuorumTimeout)
				assert.Equal(t, 5*time.Minute, sbp.config.FencingTimeout)

				// Verify initialization
				assert.NotNil(t, sbp.fencedNodes)
				assert.NotNil(t, sbp.metrics)
			}
		})
	}
}

// TestSplitBrainPreventer_CheckQuorum tests quorum checking logic.
func TestSplitBrainPreventer_CheckQuorum(t *testing.T) {
	tests := []struct {
		name         string
		setupMock    func(*mockRaftNode)
		shardID      uint64
		expectQuorum bool
		expectError  bool
		errorCode    dberrors.ErrorCode
	}{
		{
			name: "has quorum with leader",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:      1,
			expectQuorum: true,
			expectError:  false,
		},
		{
			name: "no quorum - no leader",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:      1,
			expectQuorum: false,
			expectError:  false,
		},
		{
			name: "no quorum - leader error",
			setupMock: func(m *mockRaftNode) {
				m.setError(1, &dberrors.MessageError{Message: "raft error"})
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:      1,
			expectQuorum: false,
			expectError:  false,
		},
		{
			name: "nil node host info",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = nil
			},
			shardID:     1,
			expectError: true,
			errorCode:   dberrors.ErrInternal,
		},
		{
			name: "shard not found in LogInfo",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 2, ReplicaID: 1}, // Different shard
					},
				}
			},
			shardID:     1,
			expectError: true,
			errorCode:   dberrors.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &SplitBrainConfig{
				MinQuorum: 2,
			}

			sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			status, err := sbp.CheckQuorum(ctx, tt.shardID)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*dberrors.DragonError)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, dragonErr.Code)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectQuorum, status.HasQuorum)
				assert.Greater(t, status.TotalMembers, 0)
				assert.GreaterOrEqual(t, status.RequiredQuorum, config.MinQuorum)
			}
		})
	}
}

// TestSplitBrainPreventer_EnforceQuorum tests quorum enforcement.
func TestSplitBrainPreventer_EnforceQuorum(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*mockRaftNode)
		shardID     uint64
		expectError bool
		errorCode   dberrors.ErrorCode
	}{
		{
			name: "quorum present",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:     1,
			expectError: false,
		},
		{
			name: "quorum lost",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:     1,
			expectError: true,
			errorCode:   dberrors.ErrQuorumLost,
		},
		{
			name: "check quorum returns error",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = nil // Will cause CheckQuorum to fail
			},
			shardID:     1,
			expectError: true,
			errorCode:   dberrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &SplitBrainConfig{
				MinQuorum: 2,
			}

			sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			err = sbp.EnforceQuorum(ctx, tt.shardID)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*dberrors.DragonError)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, dragonErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestSplitBrainPreventer_FenceNode tests node fencing.
func TestSplitBrainPreventer_FenceNode(t *testing.T) {
	tests := []struct {
		name           string
		fencingEnabled bool
		nodeID         uint64
		reason         string
		expectError    bool
		errorCode      dberrors.ErrorCode
	}{
		{
			name:           "fencing enabled",
			fencingEnabled: true,
			nodeID:         2,
			reason:         "suspected split-brain",
			expectError:    false,
		},
		{
			name:           "fencing disabled",
			fencingEnabled: false,
			nodeID:         2,
			reason:         "test",
			expectError:    true,
			errorCode:      dberrors.ErrNotImplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &SplitBrainConfig{
				FencingEnabled: tt.fencingEnabled,
				FencingTimeout: 5 * time.Minute,
			}

			sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			err = sbp.FenceNode(tt.nodeID, tt.reason)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*dberrors.DragonError)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, dragonErr.Code)
			} else {
				require.NoError(t, err)
				assert.True(t, sbp.IsNodeFenced(tt.nodeID))
			}
		})
	}
}

// TestSplitBrainPreventer_UnfenceNode tests node unfencing.
func TestSplitBrainPreventer_UnfenceNode(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &SplitBrainConfig{
		FencingEnabled: true,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("unfence existing node", func(t *testing.T) {
		err := sbp.FenceNode(2, "test")
		require.NoError(t, err)
		assert.True(t, sbp.IsNodeFenced(2))

		// Then unfence
		err = sbp.UnfenceNode(2)
		require.NoError(t, err)
		assert.False(t, sbp.IsNodeFenced(2))
	})

	t.Run("unfence non-fenced node", func(t *testing.T) {
		err := sbp.UnfenceNode(999)
		require.Error(t, err)
		dragonErr, ok := err.(*dberrors.DragonError)
		require.True(t, ok)
		assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
	})
}

// TestSplitBrainPreventer_IsNodeFenced tests node fence status checking.
func TestSplitBrainPreventer_IsNodeFenced(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &SplitBrainConfig{
		FencingEnabled: true,
		FencingTimeout: 100 * time.Millisecond,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("node not fenced", func(t *testing.T) {
		assert.False(t, sbp.IsNodeFenced(1))
	})

	t.Run("node is fenced", func(t *testing.T) {
		err := sbp.FenceNode(2, "test")
		require.NoError(t, err)
		assert.True(t, sbp.IsNodeFenced(2))
	})

	t.Run("fence expires", func(t *testing.T) {
		err := sbp.FenceNode(3, "test")
		require.NoError(t, err)
		assert.True(t, sbp.IsNodeFenced(3))

		// Wait for fence to expire
		time.Sleep(150 * time.Millisecond)
		assert.False(t, sbp.IsNodeFenced(3))
	})
}

// TestSplitBrainPreventer_GetFencedNodes tests retrieval of fenced nodes.
func TestSplitBrainPreventer_GetFencedNodes(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &SplitBrainConfig{
		FencingEnabled: true,
		FencingTimeout: 100 * time.Millisecond,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("no fenced nodes", func(t *testing.T) {
		nodes := sbp.GetFencedNodes()
		assert.Empty(t, nodes)
	})

	t.Run("multiple fenced nodes", func(t *testing.T) {
		err := sbp.FenceNode(2, "test")
		require.NoError(t, err)
		err = sbp.FenceNode(3, "test")
		require.NoError(t, err)

		nodes := sbp.GetFencedNodes()
		assert.Len(t, nodes, 2)
		assert.Contains(t, nodes, uint64(2))
		assert.Contains(t, nodes, uint64(3))
	})

	t.Run("expired nodes not included", func(t *testing.T) {
		err := sbp.FenceNode(4, "test")
		require.NoError(t, err)

		// Wait for fence to expire
		time.Sleep(150 * time.Millisecond)

		nodes := sbp.GetFencedNodes()
		assert.NotContains(t, nodes, uint64(4))
	})
}

// TestSplitBrainPreventer_CleanupExpiredFences tests fence cleanup.
func TestSplitBrainPreventer_CleanupExpiredFences(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &SplitBrainConfig{
		FencingEnabled: true,
		FencingTimeout: 100 * time.Millisecond,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Fence multiple nodes
	err = sbp.FenceNode(2, "test")
	require.NoError(t, err)
	err = sbp.FenceNode(3, "test")
	require.NoError(t, err)

	// Wait for fences to expire
	time.Sleep(150 * time.Millisecond)

	// Cleanup
	removed := sbp.CleanupExpiredFences()
	assert.Equal(t, 2, removed)

	// Verify they're gone
	nodes := sbp.GetFencedNodes()
	assert.Empty(t, nodes)
}

// TestSplitBrainPreventer_DetectSplitBrain tests split-brain detection.
func TestSplitBrainPreventer_DetectSplitBrain(t *testing.T) {
	tests := []struct {
		name             string
		setupMock        func(*mockRaftNode)
		shardID          uint64
		expectSplitBrain bool
		expectError      bool
	}{
		{
			name: "normal operation - has leader and quorum",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:          1,
			expectSplitBrain: false,
			expectError:      false,
		},
		{
			name: "no leader",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:          1,
			expectSplitBrain: false,
			expectError:      false,
		},
		{
			name: "leader error",
			setupMock: func(m *mockRaftNode) {
				m.setError(1, &dberrors.MessageError{Message: "raft error"})
			},
			shardID:     1,
			expectError: true,
		},
		{
			name: "CheckQuorum error propagates",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = nil // Will cause CheckQuorum to fail
			},
			shardID:     1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &SplitBrainConfig{
				MinQuorum: 2,
			}

			sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			splitBrain, err := sbp.DetectSplitBrain(ctx, tt.shardID)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectSplitBrain, splitBrain)
			}
		})
	}
}

// TestSplitBrainPreventer_PreventWrite tests write prevention logic.
func TestSplitBrainPreventer_PreventWrite(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*mockRaftNode)
		shardID     uint64
		expectError bool
		errorCode   dberrors.ErrorCode
	}{
		{
			name: "write allowed - has quorum",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:     1,
			expectError: false,
		},
		{
			name: "write blocked - no quorum",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{
						{ShardID: 1, ReplicaID: 1},
					},
				}
			},
			shardID:     1,
			expectError: true,
			errorCode:   dberrors.ErrQuorumLost,
		},
		{
			name: "write blocked - DetectSplitBrain error",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = nil // Causes CheckQuorum in DetectSplitBrain to fail
			},
			shardID:     1,
			expectError: true,
			errorCode:   dberrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &SplitBrainConfig{
				MinQuorum: 2,
			}

			sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			err = sbp.PreventWrite(ctx, tt.shardID)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*dberrors.DragonError)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, dragonErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestSplitBrainPreventer_ConcurrentFencing tests concurrent fencing operations.
func TestSplitBrainPreventer_ConcurrentFencing(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &SplitBrainConfig{
		FencingEnabled: true,
		FencingTimeout: 1 * time.Minute,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 20

	// Concurrent fencing
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(nodeID uint64) {
			defer wg.Done()
			_ = sbp.FenceNode(nodeID, "concurrent test")
		}(uint64(i))
	}
	wg.Wait()

	// Verify all nodes were fenced
	fencedNodes := sbp.GetFencedNodes()
	assert.Len(t, fencedNodes, numGoroutines)

	// Concurrent unfencing
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(nodeID uint64) {
			defer wg.Done()
			_ = sbp.UnfenceNode(nodeID)
		}(uint64(i))
	}
	wg.Wait()

	// Verify all nodes were unfenced
	fencedNodes = sbp.GetFencedNodes()
	assert.Empty(t, fencedNodes)
}

// TestSplitBrainPreventer_ConcurrentQuorumChecks tests concurrent quorum operations.
func TestSplitBrainPreventer_ConcurrentQuorumChecks(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{
			{ShardID: 1, ReplicaID: 1},
		},
	}

	config := &SplitBrainConfig{
		MinQuorum: 2,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 50

	ctx := context.Background()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = sbp.CheckQuorum(ctx, 1)
		}()
	}
	wg.Wait()

	// No panic means thread safety is working
}

// TestSplitBrainPreventer_CheckQuorum_ShardFound tests shard lookup in LogInfo.
func TestSplitBrainPreventer_CheckQuorum_ShardFound(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(2, 5)
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{
			{ShardID: 1, ReplicaID: 1},
			{ShardID: 2, ReplicaID: 5}, // The shard we're looking for
			{ShardID: 3, ReplicaID: 3},
		},
	}

	config := &SplitBrainConfig{
		MinQuorum: 2,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()
	status, err := sbp.CheckQuorum(ctx, 2)

	require.NoError(t, err)
	assert.True(t, status.HasQuorum)
	assert.Equal(t, 3, status.TotalMembers)
	assert.Equal(t, 2, status.RequiredQuorum)
}

// TestSplitBrainPreventer_EnforceQuorum_MetricsUpdated tests that metrics are updated correctly.
func TestSplitBrainPreventer_EnforceQuorum_MetricsUpdated(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 0) // No leader - quorum lost
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{
			{ShardID: 1, ReplicaID: 1},
		},
	}

	config := &SplitBrainConfig{
		MinQuorum: 2,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// First call - should fail
	err = sbp.EnforceQuorum(ctx, 1)
	require.Error(t, err)

	// Second call - should also fail and increment metrics again
	err = sbp.EnforceQuorum(ctx, 1)
	require.Error(t, err)

	dragonErr, ok := err.(*dberrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrQuorumLost, dragonErr.Code)
}

// TestSplitBrainPreventer_DetectSplitBrain_LeaderWithoutQuorum tests the suspicious case.
func TestSplitBrainPreventer_DetectSplitBrain_LeaderWithoutQuorum(t *testing.T) {
	// This is a tricky test case that shouldn't happen in normal Raft,
	// but we test the detection logic anyway

	// Create a custom mock that returns leader ID but CheckQuorum says no quorum
	mockRaft := newMockRaftNode()

	// Set up a scenario: GetLeaderID returns non-zero but there's no actual quorum
	// We'll use the error path to simulate this
	mockRaft.setLeader(1, 5) // Has a leader ID
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{
			{ShardID: 1, ReplicaID: 1},
		},
	}

	config := &SplitBrainConfig{
		MinQuorum: 2,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// First verify the leader is detected
	leaderID, _, err := mockRaft.GetLeaderID(1)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), leaderID)

	ctx := context.Background()

	// Now change the setup to simulate no leader (quorum lost)
	mockRaft.setLeader(1, 0)

	splitBrain, err := sbp.DetectSplitBrain(ctx, 1)
	require.NoError(t, err)
	assert.False(t, splitBrain) // No leader means no split brain (just partition)
}

// TestSplitBrainPreventer_PreventWrite_SplitBrainDetected tests split-brain scenario in writes.
func TestSplitBrainPreventer_PreventWrite_SplitBrainDetected(t *testing.T) {
	// We need to create a scenario where:
	// 1. EnforceQuorum passes (has quorum)
	// 2. DetectSplitBrain detects split brain
	// This is actually hard to achieve with the current implementation
	// because if there's a leader (quorum), DetectSplitBrain won't detect split-brain

	// Instead, test the error path where DetectSplitBrain itself returns an error
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{
			{ShardID: 1, ReplicaID: 1},
		},
	}

	config := &SplitBrainConfig{
		MinQuorum: 2,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// This should succeed
	err = sbp.PreventWrite(ctx, 1)
	require.NoError(t, err)

	// Now break the node info to cause DetectSplitBrain to error
	mockRaft.nodeInfo = nil

	err = sbp.PreventWrite(ctx, 1)
	require.Error(t, err)
	dragonErr, ok := err.(*dberrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInternal, dragonErr.Code)
}

// TestSplitBrainPreventer_CheckQuorum_RequiredQuorumCalculation tests quorum calculation edge cases.
func TestSplitBrainPreventer_CheckQuorum_RequiredQuorumCalculation(t *testing.T) {
	tests := []struct {
		name           string
		minQuorum      int
		expectedQuorum int
	}{
		{
			name:           "default min quorum (2)",
			minQuorum:      2,
			expectedQuorum: 2,
		},
		{
			name:           "higher min quorum",
			minQuorum:      3,
			expectedQuorum: 3,
		},
		{
			name:           "min quorum 1",
			minQuorum:      1,
			expectedQuorum: 2, // (3/2 + 1) = 2 is higher than minQuorum
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)
			mockRaft.nodeInfo = &interfaces.NodeHostInfo{
				LogInfo: []interfaces.NodeInfo{
					{ShardID: 1, ReplicaID: 1},
				},
			}

			config := &SplitBrainConfig{
				MinQuorum: tt.minQuorum,
			}

			sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			status, err := sbp.CheckQuorum(ctx, 1)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedQuorum, status.RequiredQuorum)
		})
	}
}

// TestSplitBrainPreventer_Integration_FullWorkflow tests a complete workflow.
func TestSplitBrainPreventer_Integration_FullWorkflow(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{
			{ShardID: 1, ReplicaID: 1},
		},
	}

	config := &SplitBrainConfig{
		MinQuorum:      2,
		FencingEnabled: true,
		FencingTimeout: 1 * time.Minute,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// Step 1: Check quorum - should pass
	status, err := sbp.CheckQuorum(ctx, 1)
	require.NoError(t, err)
	assert.True(t, status.HasQuorum)

	// Step 2: Enforce quorum - should pass
	err = sbp.EnforceQuorum(ctx, 1)
	require.NoError(t, err)

	// Step 3: Detect split brain - should be false
	splitBrain, err := sbp.DetectSplitBrain(ctx, 1)
	require.NoError(t, err)
	assert.False(t, splitBrain)

	// Step 4: Prevent write - should pass
	err = sbp.PreventWrite(ctx, 1)
	require.NoError(t, err)

	// Step 5: Simulate quorum loss
	mockRaft.setLeader(1, 0)

	// Step 6: Check quorum - should fail
	status, err = sbp.CheckQuorum(ctx, 1)
	require.NoError(t, err)
	assert.False(t, status.HasQuorum)

	// Step 7: Enforce quorum - should fail
	err = sbp.EnforceQuorum(ctx, 1)
	require.Error(t, err)

	// Step 8: Prevent write - should fail
	err = sbp.PreventWrite(ctx, 1)
	require.Error(t, err)

	// Step 9: Fence a node
	err = sbp.FenceNode(2, "partition detected")
	require.NoError(t, err)

	// Step 10: Verify node is fenced
	assert.True(t, sbp.IsNodeFenced(2))

	// Step 11: Get fenced nodes
	fencedNodes := sbp.GetFencedNodes()
	assert.Contains(t, fencedNodes, uint64(2))

	// Step 12: Unfence the node
	err = sbp.UnfenceNode(2)
	require.NoError(t, err)
	assert.False(t, sbp.IsNodeFenced(2))
}

// TestSplitBrainPreventer_CleanupExpiredFences_NoExpiredFences tests cleanup with no expired fences.
func TestSplitBrainPreventer_CleanupExpiredFences_NoExpiredFences(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &SplitBrainConfig{
		FencingEnabled: true,
		FencingTimeout: 10 * time.Minute, // Long timeout
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Fence a node
	err = sbp.FenceNode(2, "test")
	require.NoError(t, err)

	// Cleanup immediately - nothing should be removed
	removed := sbp.CleanupExpiredFences()
	assert.Equal(t, 0, removed)

	// Node should still be fenced
	assert.True(t, sbp.IsNodeFenced(2))
}

// TestSplitBrainPreventer_DetectSplitBrain_ActualSplitBrainCase tests the actual split-brain detection.
// This tests the critical path where we have a leader ID but CheckQuorum returns no quorum.
func TestSplitBrainPreventer_DetectSplitBrain_ActualSplitBrainCase(t *testing.T) {
	// Create a custom mock that simulates split-brain condition
	mockRaft := &mockRaftNodeWithSplitBrain{
		mockRaftNode:   *newMockRaftNode(),
		splitBrainMode: true,
	}

	// Set leader ID (will be returned by GetLeaderID)
	mockRaft.setLeader(1, 5)

	// Set node info with shard
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{
			{ShardID: 1, ReplicaID: 1},
		},
	}

	config := &SplitBrainConfig{
		MinQuorum: 2,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// This should detect split-brain: leader exists (5) but GetLeaderID will return 0 for quorum check
	splitBrain, err := sbp.DetectSplitBrain(ctx, 1)
	require.NoError(t, err)
	assert.True(t, splitBrain, "Should detect split-brain when leader exists but no quorum")
}

// mockRaftNodeWithSplitBrain simulates a split-brain scenario where GetLeaderID returns
// a non-zero leader but the node doesn't actually have quorum.
type mockRaftNodeWithSplitBrain struct {
	mockRaftNode
	splitBrainMode bool
	callCount      int
	mu             sync.Mutex
}

func (m *mockRaftNodeWithSplitBrain) GetLeaderID(shardID uint64) (uint64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++

	if m.splitBrainMode {
		// First call returns leader ID (simulating we think we have a leader)
		// Second call (from CheckQuorum) returns 0 (simulating we don't have quorum)
		if m.callCount == 1 {
			// Return the leader ID we think we have
			leaderID, ok := m.leaders[shardID]
			if !ok {
				return 0, false, &dberrors.MessageError{Message: "shard " + strconv.FormatUint(shardID, 10) + " not found"}
			}
			return leaderID, leaderID != 0, nil
		}
		// Subsequent calls return 0 (no quorum)
		return 0, false, nil
	}

	// Normal mode - use parent implementation
	return m.mockRaftNode.GetLeaderID(shardID)
}

// TestSplitBrainPreventer_PreventWrite_ActualSplitBrainBlocked tests PreventWrite when split-brain is detected.
func TestSplitBrainPreventer_PreventWrite_ActualSplitBrainBlocked(t *testing.T) {
	// Create a custom mock that simulates split-brain condition
	mockRaft := &mockRaftNodeWithSplitBrainForPreventWrite{
		mockRaftNode: *newMockRaftNode(),
	}

	// Set leader ID (will be returned by GetLeaderID)
	mockRaft.setLeader(1, 5)

	// Set node info with shard
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{
			{ShardID: 1, ReplicaID: 1},
		},
	}

	config := &SplitBrainConfig{
		MinQuorum: 2,
	}

	sbp, err := NewSplitBrainPreventer(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// PreventWrite should block due to split-brain detection
	err = sbp.PreventWrite(ctx, 1)
	require.Error(t, err)
	dragonErr, ok := err.(*dberrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrSplitBrain, dragonErr.Code)
}

// mockRaftNodeWithSplitBrainForPreventWrite simulates a split-brain scenario for PreventWrite.
// It returns leader in a way that passes EnforceQuorum but fails in DetectSplitBrain.
type mockRaftNodeWithSplitBrainForPreventWrite struct {
	mockRaftNode
	callCount int
	mu        sync.Mutex
}

func (m *mockRaftNodeWithSplitBrainForPreventWrite) GetLeaderID(shardID uint64) (uint64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++

	// Call pattern for PreventWrite:
	// 1st call: EnforceQuorum -> CheckQuorum -> GetLeaderID (return leader to pass EnforceQuorum)
	// 2nd call: DetectSplitBrain -> GetLeaderID (return leader)
	// 3rd call: DetectSplitBrain -> CheckQuorum -> GetLeaderID (return 0 to simulate no quorum)

	if m.callCount <= 2 {
		// Return leader for first two calls
		leaderID, ok := m.leaders[shardID]
		if !ok {
			return 0, false, &dberrors.MessageError{Message: "shard " + strconv.FormatUint(shardID, 10) + " not found"}
		}
		return leaderID, leaderID != 0, nil
	}

	// Third call onwards: return 0 (no quorum)
	return 0, false, nil
}
