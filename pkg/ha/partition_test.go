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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestNewPartitionDetector tests the constructor with various configurations.
func TestNewPartitionDetector(t *testing.T) {
	tests := []struct {
		name        string
		raftNode    interfaces.RaftNode
		config      *PartitionConfig
		logger      interfaces.Logger
		expectError bool
		errorCode   errors.ErrorCode
	}{
		{
			name:     "valid configuration",
			raftNode: newMockRaftNode(),
			config: &PartitionConfig{
				DetectionInterval:  10 * time.Second,
				PeerTimeout:        30 * time.Second,
				HealthCheckTimeout: 5 * time.Second,
				AutoRecovery:       true,
				ReadOnlyOnMinority: true,
				Peers: map[uint64]string{
					2: "node2:8001",
					3: "node3:8001",
				},
			},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "config with defaults",
			raftNode:    newMockRaftNode(),
			config:      &PartitionConfig{},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "nil raft node",
			raftNode:    nil,
			config:      &PartitionConfig{},
			logger:      newMockLogger(),
			expectError: true,
			errorCode:   errors.ErrInvalidInput,
		},
		{
			name:        "nil config",
			raftNode:    newMockRaftNode(),
			config:      nil,
			logger:      newMockLogger(),
			expectError: true,
			errorCode:   errors.ErrInvalidInput,
		},
		{
			name:        "nil logger",
			raftNode:    newMockRaftNode(),
			config:      &PartitionConfig{},
			logger:      nil,
			expectError: true,
			errorCode:   errors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd, err := NewPartitionDetector(tt.raftNode, tt.config, tt.logger)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*errors.DragonError)
				require.True(t, ok, "expected DragonError")
				assert.Equal(t, tt.errorCode, dragonErr.Code)
				assert.Nil(t, pd)
			} else {
				require.NoError(t, err)
				require.NotNil(t, pd)

				// Verify defaults are applied
				assert.Equal(t, 10*time.Second, pd.config.DetectionInterval)
				assert.Equal(t, 30*time.Second, pd.config.PeerTimeout)
				assert.Equal(t, 5*time.Second, pd.config.HealthCheckTimeout)

				// Verify initialization
				assert.NotNil(t, pd.peers)
				assert.NotNil(t, pd.metrics)
			}
		})
	}
}

// TestPartitionDetector_StartStop tests lifecycle management.
func TestPartitionDetector_StartStop(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		DetectionInterval: 100 * time.Millisecond,
		Peers: map[uint64]string{
			2: "node2:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Test Start
	err = pd.Start()
	require.NoError(t, err)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Test Stop
	err = pd.Stop()
	require.NoError(t, err)

	// Test double stop (should be idempotent)
	err = pd.Stop()
	require.NoError(t, err)
}

// TestPartitionDetector_MonitorLoopTicker tests the automatic monitoring via ticker.
func TestPartitionDetector_MonitorLoopTicker(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		DetectionInterval: 50 * time.Millisecond,
		PeerTimeout:       10 * time.Second,
		Peers: map[uint64]string{
			2: "node2:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Start monitoring
	err = pd.Start()
	require.NoError(t, err)

	// Wait for at least 2 ticks to ensure checkForPartition is called
	time.Sleep(150 * time.Millisecond)

	// Stop monitoring
	err = pd.Stop()
	require.NoError(t, err)

	// Verify no partition was detected (peers are reachable by default)
	status := pd.GetStatus()
	assert.False(t, status.Partitioned)
}

// TestPartitionDetector_MonitorLoopStopChan tests stopping via stopChan.
func TestPartitionDetector_MonitorLoopStopChan(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		DetectionInterval: 10 * time.Second, // Long interval
		Peers: map[uint64]string{
			2: "node2:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	err = pd.Start()
	require.NoError(t, err)

	// Stop immediately via stopChan
	err = pd.Stop()
	require.NoError(t, err)
}

// TestPartitionDetector_MonitorLoopContextCancel tests stopping via context cancel.
func TestPartitionDetector_MonitorLoopContextCancel(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		DetectionInterval: 10 * time.Second,
		Peers: map[uint64]string{
			2: "node2:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	err = pd.Start()
	require.NoError(t, err)

	// Cancel context directly
	pd.cancel()

	// Wait for goroutine to exit
	pd.wg.Wait()

	// Verify it stopped cleanly
	assert.NotNil(t, pd)
}

// TestPartitionDetector_DetectPartition tests partition detection logic.
func TestPartitionDetector_DetectPartition(t *testing.T) {
	tests := []struct {
		name              string
		setupPeers        map[uint64]string
		peerTimeout       time.Duration
		markUnreachable   []uint64
		expectPartitioned bool
	}{
		{
			name: "all peers reachable",
			setupPeers: map[uint64]string{
				2: "node2:8001",
				3: "node3:8001",
			},
			peerTimeout:       10 * time.Second,
			markUnreachable:   []uint64{},
			expectPartitioned: false,
		},
		{
			name: "one peer unreachable",
			setupPeers: map[uint64]string{
				2: "node2:8001",
				3: "node3:8001",
			},
			peerTimeout:       1 * time.Millisecond,
			markUnreachable:   []uint64{2},
			expectPartitioned: true,
		},
		{
			name: "all peers unreachable",
			setupPeers: map[uint64]string{
				2: "node2:8001",
				3: "node3:8001",
			},
			peerTimeout:       1 * time.Millisecond,
			markUnreachable:   []uint64{2, 3},
			expectPartitioned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &PartitionConfig{
				Peers:       tt.setupPeers,
				PeerTimeout: tt.peerTimeout,
			}

			pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			// Mark peers as unreachable by setting LastSeen to past
			for _, nodeID := range tt.markUnreachable {
				pd.peersMu.Lock()
				if peer, exists := pd.peers[nodeID]; exists {
					peer.LastSeen = time.Now().Add(-10 * time.Second)
				}
				pd.peersMu.Unlock()
			}

			ctx := context.Background()
			partition, err := pd.DetectPartition(ctx)

			require.NoError(t, err)
			if tt.expectPartitioned {
				assert.NotNil(t, partition)
				assert.False(t, partition.Healed)
				assert.NotEmpty(t, partition.Minority)
			} else {
				assert.Nil(t, partition)
			}
		})
	}
}

// TestPartitionDetector_DetectPartitionHealing tests partition heal detection.
func TestPartitionDetector_DetectPartitionHealing(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
		},
		PeerTimeout: 1 * time.Millisecond,
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// First, create a partition by marking peer unreachable
	pd.peersMu.Lock()
	pd.peers[2].LastSeen = time.Now().Add(-10 * time.Second)
	pd.peersMu.Unlock()

	partition, err := pd.DetectPartition(ctx)
	require.NoError(t, err)
	require.NotNil(t, partition)
	assert.False(t, partition.Healed)

	// Now mark peer as reachable again
	pd.peersMu.Lock()
	pd.peers[2].LastSeen = time.Now()
	pd.peersMu.Unlock()

	// Detect partition again - should heal
	partition2, err := pd.DetectPartition(ctx)
	require.NoError(t, err)
	assert.Nil(t, partition2)

	// Check that the partition was marked as healed
	pd.partitionMu.RLock()
	assert.True(t, pd.currentPartition.Healed)
	assert.False(t, pd.currentPartition.HealedAt.IsZero())
	pd.partitionMu.RUnlock()
}

// TestPartitionDetector_FindIsolatedNodes tests isolated node identification.
func TestPartitionDetector_FindIsolatedNodes(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
			4: "node4:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	reachability := map[uint64]bool{
		2: true,
		3: false,
		4: false,
	}

	isolated := pd.findIsolatedNodes(reachability)
	assert.Len(t, isolated, 2)
	assert.Contains(t, isolated, uint64(3))
	assert.Contains(t, isolated, uint64(4))
}

// TestPartitionDetector_HandlePartitionWithoutReadOnlyMode tests handling without read-only mode.
func TestPartitionDetector_HandlePartitionWithoutReadOnlyMode(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		ReadOnlyOnMinority: false, // Disabled
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// Create a partition where we're in minority
	partition := &Partition{
		Majority: []uint64{},
		Minority: []uint64{2, 3},
	}

	err = pd.handlePartition(ctx, partition)
	require.NoError(t, err)

	// Should NOT be in read-only mode
	assert.False(t, pd.IsReadOnlyMode())
}

// TestPartitionDetector_HandlePartitionInMajority tests handling when in majority.
func TestPartitionDetector_HandlePartitionInMajority(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		ReadOnlyOnMinority: true,
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// Create a partition where we're in majority (can reach 2 out of 3 peers)
	partition := &Partition{
		Majority: []uint64{2, 3},
		Minority: []uint64{},
	}

	err = pd.handlePartition(ctx, partition)
	require.NoError(t, err)

	// Should NOT be in read-only mode (we're in majority)
	assert.False(t, pd.IsReadOnlyMode())
}

// TestPartitionDetector_RecoverFromPartitionAlreadyReadWrite tests recovery when not in read-only.
func TestPartitionDetector_RecoverFromPartitionAlreadyReadWrite(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		AutoRecovery: true,
		Peers: map[uint64]string{
			2: "node2:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Don't enter read-only mode, just recover
	ctx := context.Background()
	err = pd.recoverFromPartition(ctx)
	require.NoError(t, err)
	assert.False(t, pd.IsReadOnlyMode())
}

// TestPartitionDetector_ReadOnlyMode tests read-only mode handling.
func TestPartitionDetector_ReadOnlyMode(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		ReadOnlyOnMinority: true,
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("initial state - read-write mode", func(t *testing.T) {
		assert.False(t, pd.IsReadOnlyMode())
	})

	t.Run("enter read-only mode", func(t *testing.T) {
		err := pd.enterReadOnlyMode()
		require.NoError(t, err)
		assert.True(t, pd.IsReadOnlyMode())

		// Entering again should be idempotent
		err = pd.enterReadOnlyMode()
		require.NoError(t, err)
		assert.True(t, pd.IsReadOnlyMode())
	})

	t.Run("exit read-only mode", func(t *testing.T) {
		err := pd.exitReadOnlyMode()
		require.NoError(t, err)
		assert.False(t, pd.IsReadOnlyMode())

		// Exiting again should be idempotent
		err = pd.exitReadOnlyMode()
		require.NoError(t, err)
		assert.False(t, pd.IsReadOnlyMode())
	})
}

// TestPartitionDetector_GetStatus tests status reporting.
func TestPartitionDetector_GetStatus(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	status := pd.GetStatus()
	assert.NotNil(t, status)
	assert.False(t, status.ReadOnlyMode)
	assert.Equal(t, 2, status.TotalPeers)
}

// TestPartitionDetector_GetPeerStates tests peer state retrieval.
func TestPartitionDetector_GetPeerStates(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	states := pd.GetPeerStates()
	assert.Len(t, states, 2)

	for nodeID, state := range states {
		assert.Equal(t, nodeID, state.NodeID)
		assert.NotEmpty(t, state.Address)
		assert.True(t, state.Reachable)
	}
}

// TestPartitionDetector_CheckPeerReachability tests peer connectivity checks.
func TestPartitionDetector_CheckPeerReachability(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
		PeerTimeout: 1 * time.Second,
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()
	reachability := pd.checkPeerReachability(ctx)

	assert.NotNil(t, reachability)
	assert.Len(t, reachability, 2)
}

// TestPartitionDetector_PingPeer tests peer ping functionality.
func TestPartitionDetector_PingPeer(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
		},
		PeerTimeout: 1 * time.Second,
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	peer := pd.peers[2]
	ctx := context.Background()

	reachable, rtt := pd.pingPeer(ctx, peer)
	assert.True(t, reachable)
	assert.Greater(t, rtt, time.Duration(0))
}

// TestPartitionDetector_IsPartitioned tests partition detection logic.
func TestPartitionDetector_IsPartitioned(t *testing.T) {
	tests := []struct {
		name              string
		reachability      map[uint64]bool
		expectPartitioned bool
	}{
		{
			name: "all reachable",
			reachability: map[uint64]bool{
				2: true,
				3: true,
			},
			expectPartitioned: false,
		},
		{
			name: "one unreachable",
			reachability: map[uint64]bool{
				2: true,
				3: false,
			},
			expectPartitioned: true,
		},
		{
			name: "all unreachable",
			reachability: map[uint64]bool{
				2: false,
				3: false,
			},
			expectPartitioned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &PartitionConfig{
				Peers: map[uint64]string{
					2: "node2:8001",
					3: "node3:8001",
				},
			}

			pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			partitioned := pd.isPartitioned(tt.reachability)
			assert.Equal(t, tt.expectPartitioned, partitioned)
		})
	}
}

// TestPartitionDetector_FindMajorityPartition tests majority partition identification.
func TestPartitionDetector_FindMajorityPartition(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	reachability := map[uint64]bool{
		2: true,
		3: false,
	}

	majority := pd.findMajorityPartition(reachability)
	assert.Len(t, majority, 1)
	assert.Contains(t, majority, uint64(2))
}

// TestPartitionDetector_FindMinorityPartition tests minority partition identification.
func TestPartitionDetector_FindMinorityPartition(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	reachability := map[uint64]bool{
		2: true,
		3: false,
	}

	minority := pd.findMinorityPartition(reachability)
	assert.Len(t, minority, 1)
	assert.Contains(t, minority, uint64(3))
}

// TestPartitionDetector_IsInMinority tests minority detection.
func TestPartitionDetector_IsInMinority(t *testing.T) {
	tests := []struct {
		name           string
		majorityCount  int
		totalPeers     int
		expectMinority bool
	}{
		{
			name:           "in majority (2/3)",
			majorityCount:  2,
			totalPeers:     3,
			expectMinority: false,
		},
		{
			name:           "in minority (0/3)",
			majorityCount:  0,
			totalPeers:     3,
			expectMinority: true,
		},
		{
			name:           "exactly quorum (1/3)",
			majorityCount:  1,
			totalPeers:     3,
			expectMinority: false,
		},
		{
			name:           "5 node cluster - in majority (3/5)",
			majorityCount:  3,
			totalPeers:     5,
			expectMinority: false,
		},
		{
			name:           "5 node cluster - in minority (1/5)",
			majorityCount:  1,
			totalPeers:     5,
			expectMinority: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			// Build peers map
			peers := make(map[uint64]string)
			for i := 2; i <= tt.totalPeers; i++ {
				peers[uint64(i)] = fmt.Sprintf("node%d:8001", i)
			}

			config := &PartitionConfig{
				Peers: peers,
			}

			pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			// Build majority list
			majority := make([]uint64, 0)
			for i := 2; i < 2+tt.majorityCount; i++ {
				majority = append(majority, uint64(i))
			}

			partition := &Partition{
				Majority: majority,
			}

			inMinority := pd.isInMinority(partition)
			assert.Equal(t, tt.expectMinority, inMinority)
		})
	}
}

// TestPartitionDetector_HandlePartition tests partition handling.
func TestPartitionDetector_HandlePartition(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		ReadOnlyOnMinority: true,
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// Create a partition where we're in minority
	partition := &Partition{
		Majority: []uint64{},
		Minority: []uint64{2, 3},
	}

	err = pd.handlePartition(ctx, partition)
	require.NoError(t, err)
	assert.True(t, pd.IsReadOnlyMode())
}

// TestPartitionDetector_RecoverFromPartition tests partition recovery.
func TestPartitionDetector_RecoverFromPartition(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		AutoRecovery: true,
		Peers: map[uint64]string{
			2: "node2:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Enter read-only mode first
	err = pd.enterReadOnlyMode()
	require.NoError(t, err)
	assert.True(t, pd.IsReadOnlyMode())

	// Recover
	ctx := context.Background()
	err = pd.recoverFromPartition(ctx)
	require.NoError(t, err)
	assert.False(t, pd.IsReadOnlyMode())
}

// TestPartitionDetector_ConcurrentAccess tests thread safety.
func TestPartitionDetector_ConcurrentAccess(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 20

	ctx := context.Background()

	// Concurrent partition detection
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = pd.DetectPartition(ctx)
		}()
	}

	// Concurrent status checks
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pd.GetStatus()
		}()
	}

	// Concurrent read-only mode toggles
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pd.enterReadOnlyMode()
			_ = pd.exitReadOnlyMode()
		}()
	}

	wg.Wait()

	// No panic means thread safety is working
}
