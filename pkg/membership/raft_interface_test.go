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

package membership

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRaftNodeInterface verifies that mockRaftNode implements RaftNode interface
func TestRaftNodeInterface(t *testing.T) {
	var _ RaftNode = (*mockRaftNode)(nil)
}

func TestMockRaftNode_RequestAddNode(t *testing.T) {
	tests := []struct {
		name        string
		shardID     uint64
		nodeID      uint64
		address     string
		setError    error
		expectError bool
	}{
		{
			name:        "Success",
			shardID:     100,
			nodeID:      1,
			address:     "localhost:9001",
			expectError: false,
		},
		{
			name:        "WithError",
			shardID:     100,
			nodeID:      1,
			address:     "localhost:9001",
			setError:    errors.New("add node failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockRaftNode(1)

			if tt.setError != nil {
				mock.SetAddNodeError(tt.setError)
			}

			ctx := context.Background()
			err := mock.RequestAddNode(ctx, tt.shardID, tt.nodeID, tt.address, 0)

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, tt.setError, err)
			} else {
				require.NoError(t, err)
			}

			calls := mock.GetRequestAddNodeCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, tt.shardID, calls[0].shardID)
			assert.Equal(t, tt.nodeID, calls[0].nodeID)
			assert.Equal(t, tt.address, calls[0].address)
		})
	}
}

func TestMockRaftNode_RequestRemoveNode(t *testing.T) {
	tests := []struct {
		name        string
		shardID     uint64
		nodeID      uint64
		setError    error
		expectError bool
	}{
		{
			name:        "Success",
			shardID:     100,
			nodeID:      1,
			expectError: false,
		},
		{
			name:        "WithError",
			shardID:     100,
			nodeID:      1,
			setError:    errors.New("remove node failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockRaftNode(1)

			if tt.setError != nil {
				mock.SetRemoveNodeError(tt.setError)
			}

			ctx := context.Background()
			err := mock.RequestRemoveNode(ctx, tt.shardID, tt.nodeID, 0)

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, tt.setError, err)
			} else {
				require.NoError(t, err)
			}

			calls := mock.GetRequestRemoveNodeCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, tt.shardID, calls[0].shardID)
			assert.Equal(t, tt.nodeID, calls[0].nodeID)
		})
	}
}

func TestMockRaftNode_GetLeaderID(t *testing.T) {
	tests := []struct {
		name        string
		leaderID    uint64
		setError    error
		expectError bool
		expectedID  uint64
		expectValid bool
	}{
		{
			name:        "Success",
			leaderID:    5,
			expectError: false,
			expectedID:  5,
			expectValid: true,
		},
		{
			name:        "WithError",
			leaderID:    0,
			setError:    errors.New("get leader failed"),
			expectError: true,
			expectedID:  0,
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockRaftNode(tt.leaderID)

			if tt.setError != nil {
				mock.SetGetLeaderError(tt.setError)
			}

			id, valid, err := mock.GetLeaderID(100)

			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, tt.setError, err)
				assert.False(t, valid)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectValid, valid)
			}

			assert.Equal(t, tt.expectedID, id)
		})
	}
}

func TestMockRaftNode_SetLeaderID(t *testing.T) {
	mock := newMockRaftNode(1)

	id, valid, err := mock.GetLeaderID(100)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), id)
	assert.True(t, valid)

	mock.SetLeaderID(5)

	id, valid, err = mock.GetLeaderID(100)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), id)
	assert.True(t, valid)
}

func TestMockRaftNode_ResetCalls(t *testing.T) {
	mock := newMockRaftNode(1)
	ctx := context.Background()

	// Make some calls
	err := mock.RequestAddNode(ctx, 100, 1, "localhost:9001", 0)
	require.NoError(t, err)

	err = mock.RequestRemoveNode(ctx, 100, 2, 0)
	require.NoError(t, err)

	// Verify calls recorded
	assert.Len(t, mock.GetRequestAddNodeCalls(), 1)
	assert.Len(t, mock.GetRequestRemoveNodeCalls(), 1)

	// Reset
	mock.ResetCalls()

	// Verify calls cleared
	assert.Empty(t, mock.GetRequestAddNodeCalls())
	assert.Empty(t, mock.GetRequestRemoveNodeCalls())
}

func TestMockRaftNode_MultipleCalls(t *testing.T) {
	mock := newMockRaftNode(1)
	ctx := context.Background()

	// Add multiple nodes
	err := mock.RequestAddNode(ctx, 100, 1, "localhost:9001", 0)
	require.NoError(t, err)

	err = mock.RequestAddNode(ctx, 100, 2, "localhost:9002", 0)
	require.NoError(t, err)

	err = mock.RequestAddNode(ctx, 200, 3, "localhost:9003", 0)
	require.NoError(t, err)

	calls := mock.GetRequestAddNodeCalls()
	require.Len(t, calls, 3)

	assert.Equal(t, uint64(100), calls[0].shardID)
	assert.Equal(t, uint64(1), calls[0].nodeID)
	assert.Equal(t, "localhost:9001", calls[0].address)

	assert.Equal(t, uint64(100), calls[1].shardID)
	assert.Equal(t, uint64(2), calls[1].nodeID)
	assert.Equal(t, "localhost:9002", calls[1].address)

	assert.Equal(t, uint64(200), calls[2].shardID)
	assert.Equal(t, uint64(3), calls[2].nodeID)
	assert.Equal(t, "localhost:9003", calls[2].address)
}

func TestMockRaftNode_ConcurrentAccess(t *testing.T) {
	mock := newMockRaftNode(1)
	ctx := context.Background()

	// Test concurrent SetLeaderID and GetLeaderID
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			mock.SetLeaderID(uint64(i))
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_, _, _ = mock.GetLeaderID(100)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = mock.RequestAddNode(ctx, 100, uint64(i), "localhost:9001", 0)
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done

	// Should not panic, and should have all calls recorded
	calls := mock.GetRequestAddNodeCalls()
	assert.Equal(t, 100, len(calls))
}

func TestMockRaftNode_ErrorHandling(t *testing.T) {
	mock := newMockRaftNode(1)
	ctx := context.Background()

	// Set errors
	addErr := errors.New("add error")
	removeErr := errors.New("remove error")
	leaderErr := errors.New("leader error")

	mock.SetAddNodeError(addErr)
	mock.SetRemoveNodeError(removeErr)
	mock.SetGetLeaderError(leaderErr)

	// Verify errors are returned
	err := mock.RequestAddNode(ctx, 100, 1, "localhost:9001", 0)
	assert.ErrorIs(t, err, addErr)

	err = mock.RequestRemoveNode(ctx, 100, 1, 0)
	assert.ErrorIs(t, err, removeErr)

	_, _, err = mock.GetLeaderID(100)
	assert.ErrorIs(t, err, leaderErr)

	// Clear errors
	mock.SetAddNodeError(nil)
	mock.SetRemoveNodeError(nil)
	mock.SetGetLeaderError(nil)

	// Verify no errors
	err = mock.RequestAddNode(ctx, 100, 2, "localhost:9002", 0)
	assert.NoError(t, err)

	err = mock.RequestRemoveNode(ctx, 100, 2, 0)
	assert.NoError(t, err)

	_, _, err = mock.GetLeaderID(100)
	assert.NoError(t, err)
}
