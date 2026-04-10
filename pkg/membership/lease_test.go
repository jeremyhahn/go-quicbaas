//go:build raft && integration

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
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

var ErrKeyNotFound = errors.New("key not found")

// mockStorage is a simple in-memory storage implementation for testing
type mockStorage struct {
	data map[string][]byte
	mu   sync.RWMutex
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data: make(map[string][]byte),
	}
}

func (s *mockStorage) Set(key, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[string(key)] = value
	return nil
}

func (s *mockStorage) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if val, ok := s.data[string(key)]; ok {
		return val, nil
	}
	return nil, ErrKeyNotFound
}

func (s *mockStorage) Delete(key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, string(key))
	return nil
}

func (s *mockStorage) Batch() storage.Batch {
	return nil // Not needed for tests
}

func (s *mockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return nil // Not needed for tests
}

func (s *mockStorage) Snapshot() (storage.Snapshot, error) {
	return nil, nil // Not needed for tests
}

func (s *mockStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string][]byte)
	return nil
}

// mockRaftNode is a mock implementation of RaftNode for testing
type mockRaftNode struct {
	requestAddNodeCalls    []requestAddNodeCall
	requestRemoveNodeCalls []requestRemoveNodeCall
	leaderID               uint64
	addNodeErr             error
	addNodeCallback        func(ctx context.Context, shardID, nodeID uint64, address string, configChangeIndex uint64) error
	removeNodeErr          error
	getLeaderErr           error
	mu                     sync.Mutex
}

type requestAddNodeCall struct {
	shardID           uint64
	nodeID            uint64
	address           string
	configChangeIndex uint64
}

type requestRemoveNodeCall struct {
	shardID           uint64
	nodeID            uint64
	configChangeIndex uint64
}

func newMockRaftNode(leaderID uint64) *mockRaftNode {
	return &mockRaftNode{
		leaderID: leaderID,
	}
}

func (m *mockRaftNode) RequestAddNode(ctx context.Context, shardID, nodeID uint64, address string, configChangeIndex uint64) error {
	m.mu.Lock()

	// Check for callback first (for testing retry scenarios)
	// IMPORTANT: Execute callback without holding lock to avoid deadlock
	if m.addNodeCallback != nil {
		callback := m.addNodeCallback
		m.mu.Unlock()
		err := callback(ctx, shardID, nodeID, address, configChangeIndex)
		if err != nil {
			return err
		}
		// Re-acquire lock to record the call
		m.mu.Lock()
	}

	m.requestAddNodeCalls = append(m.requestAddNodeCalls, requestAddNodeCall{
		shardID:           shardID,
		nodeID:            nodeID,
		address:           address,
		configChangeIndex: configChangeIndex,
	})

	err := m.addNodeErr
	m.mu.Unlock()
	return err
}

func (m *mockRaftNode) RequestRemoveNode(ctx context.Context, shardID, nodeID uint64, configChangeIndex uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestRemoveNodeCalls = append(m.requestRemoveNodeCalls, requestRemoveNodeCall{
		shardID:           shardID,
		nodeID:            nodeID,
		configChangeIndex: configChangeIndex,
	})

	return m.removeNodeErr
}

func (m *mockRaftNode) GetLeaderID(shardID uint64) (uint64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getLeaderErr != nil {
		return 0, false, m.getLeaderErr
	}

	return m.leaderID, m.leaderID > 0, nil
}

func (m *mockRaftNode) SetLeaderID(leaderID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaderID = leaderID
}

func (m *mockRaftNode) SetAddNodeError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addNodeErr = err
}

func (m *mockRaftNode) SetRemoveNodeError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeNodeErr = err
}

func (m *mockRaftNode) SetGetLeaderError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getLeaderErr = err
}

func (m *mockRaftNode) GetRequestAddNodeCalls() []requestAddNodeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]requestAddNodeCall{}, m.requestAddNodeCalls...)
}

func (m *mockRaftNode) GetRequestRemoveNodeCalls() []requestRemoveNodeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]requestRemoveNodeCall{}, m.requestRemoveNodeCalls...)
}

func (m *mockRaftNode) ResetCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestAddNodeCalls = nil
	m.requestRemoveNodeCalls = nil
}

// setupTestManager creates a test membership manager
func setupTestManager(t *testing.T, nodeID uint64, opts ...Option) (*Manager, *mockStorage, *watch.Manager) {
	store := newMockStorage()

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	watchMgr := watch.NewManager()

	peer := discovery.Peer{
		NodeID:   nodeID,
		Address:  fmt.Sprintf("localhost:900%d", nodeID),
		RaftAddr: fmt.Sprintf("localhost:900%d", nodeID),
	}

	config := &Config{
		LeaseTTL:            2 * time.Second,
		KeepAliveInterval:   1 * time.Second,
		HealthCheckInterval: 3 * time.Second,
		MaxRetries:          3,
		RetryBaseDelay:      100 * time.Millisecond,
	}

	mgr, err := NewManager(store, watchMgr, peer, config, log, opts...)
	require.NoError(t, err)

	return mgr, store, watchMgr
}

func TestNewManager(t *testing.T) {
	t.Run("WithoutRaftIntegration", func(t *testing.T) {
		mgr, _, _ := setupTestManager(t, 1)
		defer mgr.Close()

		assert.NotNil(t, mgr)
		assert.False(t, mgr.IsRaftEnabled())
		assert.Equal(t, uint64(1), mgr.GetSelf().NodeID)
	})

	t.Run("WithRaftIntegration", func(t *testing.T) {
		mockRaft := newMockRaftNode(1)
		mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
		defer mgr.Close()

		assert.NotNil(t, mgr)
		assert.True(t, mgr.IsRaftEnabled())
		assert.Equal(t, uint64(100), mgr.shardID)
	})

	t.Run("InvalidConfig", func(t *testing.T) {
		store := newMockStorage()
		log, err := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
		require.NoError(t, err)
		watchMgr := watch.NewManager()
		peer := discovery.Peer{NodeID: 1, Address: "localhost:9001"}

		// Invalid lease TTL
		config := &Config{
			LeaseTTL:            500 * time.Millisecond, // Too short
			KeepAliveInterval:   1 * time.Second,
			HealthCheckInterval: 3 * time.Second,
			MaxRetries:          3,
			RetryBaseDelay:      100 * time.Millisecond,
		}

		_, err = NewManager(store, watchMgr, peer, config, log)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "lease TTL must be at least 1 second")
	})
}

func TestMembershipManager_RaftIntegration(t *testing.T) {
	t.Run("LeaderAddsNode", func(t *testing.T) {
		mockRaft := newMockRaftNode(1) // Node 1 is leader
		mgr, store, watchMgr := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
		defer mgr.Close()

		ctx := context.Background()

		// Register and start watching
		err := mgr.Register(ctx)
		require.NoError(t, err)

		err = mgr.StartWatching(ctx)
		require.NoError(t, err)

		// Give watch time to start
		time.Sleep(100 * time.Millisecond)

		// Add a new member
		member2 := Member{
			NodeID:       2,
			Address:      "localhost:9002",
			RaftAddr:     "localhost:9002",
			RegisteredAt: time.Now(),
			LastSeen:     time.Now(),
		}

		data, err := json.Marshal(member2)
		require.NoError(t, err)

		key := fmt.Sprintf("%s%d", MembershipPrefix, 2)

		// Store the member
		err = store.Set([]byte(key), data)
		require.NoError(t, err)

		// IMPORTANT: Manually trigger watch notification since mockStorage
		// doesn't automatically notify watches like a real storage backend would
		watchMgr.NotifyPut([]byte(key), nil, data, 1, 1)

		// Wait for watch event to be processed and Raft RequestAddNode to be called
		time.Sleep(500 * time.Millisecond)

		// Verify RequestAddNode was called
		calls := mockRaft.GetRequestAddNodeCalls()
		assert.Len(t, calls, 1, "Expected exactly 1 RequestAddNode call")
		if len(calls) > 0 {
			assert.Equal(t, uint64(100), calls[0].shardID)
			assert.Equal(t, uint64(2), calls[0].nodeID)
			assert.Equal(t, "localhost:9002", calls[0].address)
			assert.Equal(t, uint64(0), calls[0].configChangeIndex)
		}
	})
}

func TestMembershipManager_FailureHandling(t *testing.T) {
	t.Run("RetryOnAddNodeFailure", func(t *testing.T) {
		mockRaft := newMockRaftNode(1)
		mockRaft.SetAddNodeError(&dberrors.MessageError{Message: "temporary error"})

		mgr, store, watchMgr := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
		defer mgr.Close()

		ctx := context.Background()

		// Register and start watching
		err := mgr.Register(ctx)
		require.NoError(t, err)

		err = mgr.StartWatching(ctx)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		// Add a new member (will fail)
		member2 := Member{
			NodeID:       2,
			Address:      "localhost:9002",
			RaftAddr:     "localhost:9002",
			RegisteredAt: time.Now(),
			LastSeen:     time.Now(),
		}

		data, err := json.Marshal(member2)
		require.NoError(t, err)

		key := fmt.Sprintf("%s%d", MembershipPrefix, 2)
		err = store.Set([]byte(key), data)
		require.NoError(t, err)

		// Trigger watch notification
		watchMgr.NotifyPut([]byte(key), nil, data, 1, 1)

		// Wait for retries to complete (3 attempts with exponential backoff)
		time.Sleep(1 * time.Second)

		// Verify RequestAddNode was called multiple times (retries)
		calls := mockRaft.GetRequestAddNodeCalls()
		assert.GreaterOrEqual(t, len(calls), 3, "Expected at least 3 retry attempts")
	})

	t.Run("SuccessAfterRetry", func(t *testing.T) {
		mockRaft := newMockRaftNode(1)

		// Use atomic counter to avoid race conditions
		var callCount atomic.Int32

		// Fail first attempt, succeed on second using callback
		mockRaft.addNodeCallback = func(ctx context.Context, shardID, nodeID uint64, address string, configChangeIndex uint64) error {
			count := callCount.Add(1)
			if count == 1 {
				return &dberrors.MessageError{Message: "temporary error"}
			}
			return nil
		}

		mgr, store, watchMgr := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
		defer mgr.Close()

		ctx := context.Background()

		// Register and start watching
		err := mgr.Register(ctx)
		require.NoError(t, err)

		err = mgr.StartWatching(ctx)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		// Add a new member
		member2 := Member{
			NodeID:       2,
			Address:      "localhost:9002",
			RaftAddr:     "localhost:9002",
			RegisteredAt: time.Now(),
			LastSeen:     time.Now(),
		}

		data, err := json.Marshal(member2)
		require.NoError(t, err)

		key := fmt.Sprintf("%s%d", MembershipPrefix, 2)
		err = store.Set([]byte(key), data)
		require.NoError(t, err)

		// Trigger watch notification
		watchMgr.NotifyPut([]byte(key), nil, data, 1, 1)

		// Wait for retry to succeed
		time.Sleep(500 * time.Millisecond)

		// Verify RequestAddNode was called twice and succeeded on second attempt
		calls := mockRaft.GetRequestAddNodeCalls()
		assert.GreaterOrEqual(t, len(calls), 1, "Expected at least 1 successful RequestAddNode call")
		assert.Equal(t, int32(2), callCount.Load(), "Expected callback to be called exactly twice")
	})
}
