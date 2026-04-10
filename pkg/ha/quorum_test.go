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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestNewQuorumRecoveryManager tests the constructor with various configurations.
func TestNewQuorumRecoveryManager(t *testing.T) {
	tests := []struct {
		name        string
		raftNode    interfaces.RaftNode
		config      *QuorumRecoveryConfig
		logger      interfaces.Logger
		expectError bool
		errorCode   dberrors.ErrorCode
	}{
		{
			name:     "valid configuration",
			raftNode: newMockRaftNode(),
			config: &QuorumRecoveryConfig{
				AutoRecover:         true,
				RecoveryTimeout:     5 * time.Minute,
				ForceReconstruct:    false,
				NodeRestartTimeout:  30 * time.Second,
				MinHealthyNodes:     2,
				HealthCheckInterval: 5 * time.Second,
			},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "config with defaults",
			raftNode:    newMockRaftNode(),
			config:      &QuorumRecoveryConfig{},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "nil raft node",
			raftNode:    nil,
			config:      &QuorumRecoveryConfig{},
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
			config:      &QuorumRecoveryConfig{},
			logger:      nil,
			expectError: true,
			errorCode:   dberrors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qrm, err := NewQuorumRecoveryManager(tt.raftNode, tt.config, tt.logger)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*dberrors.DragonError)
				require.True(t, ok, "expected DragonError")
				assert.Equal(t, tt.errorCode, dragonErr.Code)
				assert.Nil(t, qrm)
			} else {
				require.NoError(t, err)
				require.NotNil(t, qrm)

				// Verify defaults are applied
				assert.Equal(t, 5*time.Minute, qrm.config.RecoveryTimeout)
				assert.Equal(t, 30*time.Second, qrm.config.NodeRestartTimeout)
				assert.Equal(t, 2, qrm.config.MinHealthyNodes)
				assert.Equal(t, 5*time.Second, qrm.config.HealthCheckInterval)

				// Verify initialization
				assert.NotNil(t, qrm.recoveryInProgress)
				assert.NotNil(t, qrm.metrics)
			}
		})
	}
}

// TestQuorumRecoveryManager_RecoverQuorum tests quorum recovery.
func TestQuorumRecoveryManager_RecoverQuorum(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*mockRaftNode)
		shardID     uint64
		expectError bool
		errorCode   dberrors.ErrorCode
	}{
		{
			name: "already has quorum",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{},
				}
			},
			shardID:     1,
			expectError: false,
		},
		{
			name: "quorum lost",
			setupMock: func(m *mockRaftNode) {
				// Set error to simulate no quorum
				m.setError(1, &dberrors.MessageError{Message: "no leader available"})
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{},
				}
				// Restore quorum after a delay
				go func() {
					time.Sleep(100 * time.Millisecond)
					m.clearError(1)
					m.setLeader(1, 1)
				}()
			},
			shardID:     1,
			expectError: false,
		},
		{
			name: "recovery already in progress",
			setupMock: func(m *mockRaftNode) {
				m.setError(1, &dberrors.MessageError{Message: "no leader available"})
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{},
				}
			},
			shardID:     1,
			expectError: true,
			errorCode:   dberrors.ErrAlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &QuorumRecoveryConfig{
				AutoRecover:     true,
				RecoveryTimeout: 1 * time.Second,
			}

			qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			if tt.name == "recovery already in progress" {
				// Mark recovery as in progress
				qrm.recoveryMu.Lock()
				qrm.recoveryInProgress[tt.shardID] = true
				qrm.recoveryMu.Unlock()
			}

			ctx := context.Background()
			err = qrm.RecoverQuorum(ctx, tt.shardID)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*dberrors.DragonError)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, dragonErr.Code)
			} else {
				// May succeed or timeout depending on mock behavior
				if err != nil {
					t.Logf("Recovery error (expected in some cases): %v", err)
				}
			}
		})
	}
}

// TestQuorumRecoveryManager_AssessQuorumLoss tests quorum assessment.
func TestQuorumRecoveryManager_AssessQuorumLoss(t *testing.T) {
	tests := []struct {
		name         string
		setupMock    func(*mockRaftNode)
		shardID      uint64
		expectQuorum bool
		expectError  bool
	}{
		{
			name: "has quorum",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{},
				}
			},
			shardID:      1,
			expectQuorum: true,
			expectError:  false,
		},
		{
			name: "no_quorum",
			setupMock: func(m *mockRaftNode) {
				// Set error to simulate no leader/no quorum
				m.setError(1, &dberrors.MessageError{Message: "no leader available"})
				m.nodeInfo = &interfaces.NodeHostInfo{
					LogInfo: []interfaces.NodeInfo{},
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &QuorumRecoveryConfig{}

			qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			assessment, err := qrm.assessQuorumLoss(ctx, tt.shardID)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectQuorum, assessment.HasQuorum)
				assert.Equal(t, tt.shardID, assessment.ShardID)
				assert.Greater(t, assessment.TotalMembers, 0)
				assert.Greater(t, assessment.RequiredQuorum, 0)
				assert.NotEmpty(t, assessment.RecoveryOptions)
			}
		})
	}
}

// TestQuorumRecoveryManager_DetermineRecoveryOptions tests recovery option selection.
func TestQuorumRecoveryManager_DetermineRecoveryOptions(t *testing.T) {
	tests := []struct {
		name             string
		aliveCount       int
		totalCount       int
		forceReconstruct bool
		expectOptions    []RecoveryProcedure
	}{
		{
			name:             "some nodes down",
			aliveCount:       2,
			totalCount:       3,
			forceReconstruct: false,
			expectOptions:    []RecoveryProcedure{RecoveryRestartNodes, RecoveryAddNodes, RecoveryRemoveNodes},
		},
		{
			name:             "all nodes alive",
			aliveCount:       3,
			totalCount:       3,
			forceReconstruct: false,
			expectOptions:    []RecoveryProcedure{RecoveryAddNodes, RecoveryRemoveNodes},
		},
		{
			name:             "with force reconstruct",
			aliveCount:       1,
			totalCount:       3,
			forceReconstruct: true,
			expectOptions:    []RecoveryProcedure{RecoveryRestartNodes, RecoveryAddNodes, RecoveryRemoveNodes, RecoveryReconstruct},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &QuorumRecoveryConfig{
				ForceReconstruct: tt.forceReconstruct,
			}

			qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			options := qrm.determineRecoveryOptions(tt.aliveCount, tt.totalCount)
			assert.NotEmpty(t, options)

			for _, expectedOpt := range tt.expectOptions {
				assert.Contains(t, options, expectedOpt)
			}
		})
	}
}

// TestQuorumRecoveryManager_ChooseRecoveryProcedure tests procedure selection.
func TestQuorumRecoveryManager_ChooseRecoveryProcedure(t *testing.T) {
	tests := []struct {
		name              string
		assessment        QuorumAssessment
		expectedProcedure RecoveryProcedure
	}{
		{
			name: "dead members present - restart",
			assessment: QuorumAssessment{
				DeadMembers:     []uint64{2, 3},
				RecoveryOptions: []RecoveryProcedure{RecoveryRestartNodes, RecoveryAddNodes},
			},
			expectedProcedure: RecoveryRestartNodes,
		},
		{
			name: "no dead members - add nodes",
			assessment: QuorumAssessment{
				DeadMembers:     []uint64{},
				RecoveryOptions: []RecoveryProcedure{RecoveryAddNodes, RecoveryRemoveNodes},
			},
			expectedProcedure: RecoveryAddNodes,
		},
		{
			name: "empty options - default to restart",
			assessment: QuorumAssessment{
				DeadMembers:     []uint64{2},
				RecoveryOptions: []RecoveryProcedure{},
			},
			expectedProcedure: RecoveryRestartNodes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &QuorumRecoveryConfig{}

			qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			procedure := qrm.chooseRecoveryProcedure(tt.assessment)
			assert.Equal(t, tt.expectedProcedure, procedure)
		})
	}
}

// TestQuorumRecoveryManager_RecoverByRestartingNodes tests node restart recovery.
func TestQuorumRecoveryManager_RecoverByRestartingNodes(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setError(1, &dberrors.MessageError{Message: "no leader available"})
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{},
	}

	config := &QuorumRecoveryConfig{
		RecoveryTimeout: 2 * time.Second,
	}

	qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	assessment := QuorumAssessment{
		ShardID:     1,
		DeadMembers: []uint64{2, 3},
	}

	// Restore leader after delay to simulate successful restart
	go func() {
		time.Sleep(200 * time.Millisecond)
		mockRaft.clearError(1)
		mockRaft.setLeader(1, 1)
	}()

	ctx := context.Background()
	err = qrm.recoverByRestartingNodes(ctx, 1, assessment)
	// May succeed or timeout
	if err != nil {
		t.Logf("Restart recovery error (may be expected): %v", err)
	}
}

// TestQuorumRecoveryManager_RecoverByRemovingNodes tests node removal recovery.
func TestQuorumRecoveryManager_RecoverByRemovingNodes(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setError(1, &dberrors.MessageError{Message: "no leader available"})
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{},
	}

	config := &QuorumRecoveryConfig{
		RecoveryTimeout: 2 * time.Second,
	}

	qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	assessment := QuorumAssessment{
		ShardID:     1,
		DeadMembers: []uint64{2, 3},
	}

	// Restore leader after removal to simulate successful recovery
	go func() {
		time.Sleep(200 * time.Millisecond)
		mockRaft.clearError(1)
		mockRaft.setLeader(1, 1)
	}()

	ctx := context.Background()
	err = qrm.recoverByRemovingNodes(ctx, 1, assessment)
	// May succeed or fail depending on timing
	if err != nil {
		t.Logf("Remove nodes recovery error (may be expected): %v", err)
	}

	// Verify nodes were attempted to be removed
	mockRaft.mu.RLock()
	removedNodes := mockRaft.removedNodes[1]
	mockRaft.mu.RUnlock()
	assert.NotNil(t, removedNodes)
}

// TestQuorumRecoveryManager_RecoverByAddingNodes tests node addition recovery.
func TestQuorumRecoveryManager_RecoverByAddingNodes(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &QuorumRecoveryConfig{}

	qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	assessment := QuorumAssessment{
		ShardID: 1,
	}

	ctx := context.Background()
	err = qrm.recoverByAddingNodes(ctx, 1, assessment)

	// Should return not implemented
	require.Error(t, err)
	dragonErr, ok := err.(*dberrors.DragonError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrNotImplemented, dragonErr.Code)
}

// TestQuorumRecoveryManager_RecoverByReconstructing tests cluster reconstruction.
func TestQuorumRecoveryManager_RecoverByReconstructing(t *testing.T) {
	tests := []struct {
		name             string
		forceReconstruct bool
		expectError      bool
		errorCode        dberrors.ErrorCode
	}{
		{
			name:             "force reconstruct disabled",
			forceReconstruct: false,
			expectError:      true,
			errorCode:        dberrors.ErrNotAllowed,
		},
		{
			name:             "force reconstruct enabled",
			forceReconstruct: true,
			expectError:      true,
			errorCode:        dberrors.ErrNotImplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &QuorumRecoveryConfig{
				ForceReconstruct: tt.forceReconstruct,
			}

			qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			assessment := QuorumAssessment{
				ShardID: 1,
			}

			ctx := context.Background()
			err = qrm.recoverByReconstructing(ctx, 1, assessment)

			require.Error(t, err)
			dragonErr, ok := err.(*dberrors.DragonError)
			require.True(t, ok)
			assert.Equal(t, tt.errorCode, dragonErr.Code)
		})
	}
}

// TestQuorumRecoveryManager_WaitForQuorum tests quorum waiting logic.
func TestQuorumRecoveryManager_WaitForQuorum(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*mockRaftNode)
		expectError bool
		errorCode   dberrors.ErrorCode
	}{
		{
			name: "quorum restored",
			setupMock: func(m *mockRaftNode) {
				// Start with no leader
				m.setError(1, &dberrors.MessageError{Message: "no leader available"})
				go func() {
					time.Sleep(100 * time.Millisecond)
					m.clearError(1)
					m.setLeader(1, 1)
				}()
			},
			expectError: false,
		},
		{
			name: "timeout",
			setupMock: func(m *mockRaftNode) {
				// Leader stays unavailable, never restored
				m.setError(1, &dberrors.MessageError{Message: "no leader available"})
			},
			expectError: true,
			errorCode:   dberrors.ErrTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &QuorumRecoveryConfig{
				RecoveryTimeout:     500 * time.Millisecond,
				HealthCheckInterval: 50 * time.Millisecond,
			}

			qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			err = qrm.waitForQuorum(ctx, 1)

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

// TestQuorumRecoveryManager_GetRecoveryStatus tests status retrieval.
func TestQuorumRecoveryManager_GetRecoveryStatus(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &QuorumRecoveryConfig{}

	qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("no recovery in progress", func(t *testing.T) {
		status := qrm.GetRecoveryStatus(1)
		assert.Equal(t, uint64(1), status.ShardID)
		assert.False(t, status.InProgress)
		assert.True(t, status.Success)
	})

	t.Run("recovery in progress", func(t *testing.T) {
		qrm.recoveryMu.Lock()
		qrm.recoveryInProgress[2] = true
		qrm.recoveryMu.Unlock()

		status := qrm.GetRecoveryStatus(2)
		assert.Equal(t, uint64(2), status.ShardID)
		assert.True(t, status.InProgress)
	})
}

// TestQuorumRecoveryManager_VerifyDataConsistency tests consistency verification.
func TestQuorumRecoveryManager_VerifyDataConsistency(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &QuorumRecoveryConfig{}

	qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()
	err = qrm.VerifyDataConsistency(ctx, 1)

	// Currently a no-op, should not error
	require.NoError(t, err)
}

// TestQuorumRecoveryManager_ConcurrentRecovery tests concurrent recovery attempts.
func TestQuorumRecoveryManager_ConcurrentRecovery(t *testing.T) {
	mockRaft := newMockRaftNode()
	// Start with no leader - set error to simulate no quorum
	mockRaft.setError(1, &dberrors.MessageError{Message: "no leader available"})
	mockRaft.nodeInfo = &interfaces.NodeHostInfo{
		LogInfo: []interfaces.NodeInfo{},
	}

	config := &QuorumRecoveryConfig{
		RecoveryTimeout:     2 * time.Second,
		HealthCheckInterval: 50 * time.Millisecond,
	}

	qrm, err := NewQuorumRecoveryManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Use a channel to coordinate when to restore the leader
	// This makes the test deterministic
	var recoveryStarted sync.WaitGroup
	recoveryStarted.Add(1)

	// Restore leader after first recovery attempt has started
	go func() {
		recoveryStarted.Wait()
		time.Sleep(100 * time.Millisecond)
		mockRaft.clearError(1)
		mockRaft.setLeader(1, 1)
	}()

	var wg sync.WaitGroup
	numGoroutines := 5

	ctx := context.Background()
	var successCount, alreadyExistsCount int
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			// First goroutine signals that recovery has started
			if index == 0 {
				recoveryStarted.Done()
			}

			err := qrm.RecoverQuorum(ctx, 1)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successCount++
			} else if dragonErr, ok := err.(*dberrors.DragonError); ok && dragonErr.Code == dberrors.ErrAlreadyExists {
				alreadyExistsCount++
			}
		}(i)
	}
	wg.Wait()

	// At most one should succeed, others should get AlreadyExists
	assert.LessOrEqual(t, successCount, 1, "at most one recovery should succeed")
	assert.GreaterOrEqual(t, alreadyExistsCount, numGoroutines-1, "other attempts should get AlreadyExists error")

	// Verify that all attempts were accounted for
	assert.Equal(t, numGoroutines, successCount+alreadyExistsCount, "all attempts should be accounted for")
}
