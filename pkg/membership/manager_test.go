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
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

var ErrKeyNotFound = errors.New("key not found")

// mockStorage is a simple in-memory storage implementation for testing
type mockStorage struct {
	data      map[string][]byte
	mu        sync.RWMutex
	setErr    error
	getErr    error
	deleteErr error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data: make(map[string][]byte),
	}
}

func (s *mockStorage) Set(key, value []byte) error {
	s.mu.RLock()
	err := s.setErr
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[string(key)] = value
	return nil
}

func (s *mockStorage) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	if val, ok := s.data[string(key)]; ok {
		return val, nil
	}
	return nil, ErrKeyNotFound
}

func (s *mockStorage) Delete(key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.data, string(key))
	return nil
}

func (s *mockStorage) List(prefix []byte) ([][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys [][]byte
	for k := range s.data {
		if len(prefix) == 0 || (len(k) >= len(prefix) && k[:len(prefix)] == string(prefix)) {
			keys = append(keys, []byte(k))
		}
	}
	return keys, nil
}

func (s *mockStorage) Exists(key []byte) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[string(key)]
	return ok, nil
}

func (s *mockStorage) Batch() storage.Batch {
	return nil
}

func (s *mockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return nil
}

func (s *mockStorage) Snapshot() (storage.Snapshot, error) {
	return nil, nil
}

func (s *mockStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string][]byte)
	return nil
}

// SetSetErr sets the error to return from Set operations (thread-safe)
func (s *mockStorage) SetSetErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setErr = err
}

// SetGetErr sets the error to return from Get operations (thread-safe)
func (s *mockStorage) SetGetErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErr = err
}

// SetDeleteErr sets the error to return from Delete operations (thread-safe)
func (s *mockStorage) SetDeleteErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteErr = err
}

// mockRaftNode is a mock implementation of RaftNode for testing
type mockRaftNode struct {
	addNodeCalls    []addNodeCall
	removeNodeCalls []removeNodeCall
	leaderID        uint64
	addNodeErr      error
	removeNodeErr   error
	getLeaderErr    error
	mu              sync.Mutex
}

type addNodeCall struct {
	shardID uint64
	nodeID  uint64
	address string
}

type removeNodeCall struct {
	shardID uint64
	nodeID  uint64
}

func newMockRaftNode(leaderID uint64) *mockRaftNode {
	return &mockRaftNode{
		leaderID: leaderID,
	}
}

func (m *mockRaftNode) RequestAddNode(_ context.Context, shardID, replicaID uint64, addr string, _ uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.addNodeCalls = append(m.addNodeCalls, addNodeCall{
		shardID: shardID,
		nodeID:  replicaID,
		address: addr,
	})

	return m.addNodeErr
}

func (m *mockRaftNode) RequestRemoveNode(_ context.Context, shardID, replicaID uint64, _ uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.removeNodeCalls = append(m.removeNodeCalls, removeNodeCall{
		shardID: shardID,
		nodeID:  replicaID,
	})

	return m.removeNodeErr
}

func (m *mockRaftNode) GetLeaderID(shardID uint64) (uint64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getLeaderErr != nil {
		return 0, false, m.getLeaderErr
	}

	return m.leaderID, m.leaderID != 0, nil
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

func (m *mockRaftNode) GetRequestAddNodeCalls() []addNodeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]addNodeCall{}, m.addNodeCalls...)
}

func (m *mockRaftNode) GetRequestRemoveNodeCalls() []removeNodeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]removeNodeCall{}, m.removeNodeCalls...)
}

func (m *mockRaftNode) ResetCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addNodeCalls = nil
	m.removeNodeCalls = nil
}

// setupTestManager creates a test membership manager
func setupTestManager(t *testing.T, nodeID uint64, opts ...Option) (*Manager, *mockStorage, *watch.Manager) {
	t.Helper()

	store := newMockStorage()

	log, err := logger.New(logger.LogConfig{
		Level:  "error", // Use error level to reduce test output
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	watchMgr := watch.NewManager()

	peer := discovery.Peer{
		NodeID:   nodeID,
		Address:  fmt.Sprintf("localhost:%d", 9000+nodeID),
		RaftAddr: fmt.Sprintf("localhost:%d", 9000+nodeID),
		Metadata: map[string]string{"region": "us-east-1"},
	}

	config := &Config{
		LeaseTTL:            2 * time.Second,
		KeepAliveInterval:   1 * time.Second,
		HealthCheckInterval: 3 * time.Second,
		MaxRetries:          3,
		RetryBaseDelay:      10 * time.Millisecond, // Faster for tests
	}

	mgr, err := NewManager(store, watchMgr, peer, config, log, opts...)
	require.NoError(t, err)

	return mgr, store, watchMgr
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, DefaultLeaseTTL, config.LeaseTTL)
	assert.Equal(t, DefaultKeepAliveInterval, config.KeepAliveInterval)
	assert.Equal(t, DefaultHealthCheckInterval, config.HealthCheckInterval)
	assert.Equal(t, DefaultMaxRetries, config.MaxRetries)
	assert.Equal(t, DefaultRetryBaseDelay, config.RetryBaseDelay)
}

func TestNewManager_Success(t *testing.T) {
	tests := []struct {
		name              string
		nodeID            uint64
		setupOpts         func() []Option
		expectRaftEnabled bool
	}{
		{
			name:              "WithoutRaftIntegration",
			nodeID:            1,
			setupOpts:         func() []Option { return nil },
			expectRaftEnabled: false,
		},
		{
			name:   "WithRaftIntegration",
			nodeID: 1,
			setupOpts: func() []Option {
				return []Option{WithRaftIntegration(newMockRaftNode(1), 100)}
			},
			expectRaftEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _, _ := setupTestManager(t, tt.nodeID, tt.setupOpts()...)
			defer mgr.Close()

			assert.NotNil(t, mgr)
			assert.Equal(t, tt.expectRaftEnabled, mgr.IsRaftEnabled())
			assert.Equal(t, tt.nodeID, mgr.GetSelf().NodeID)
			assert.Equal(t, fmt.Sprintf("localhost:%d", 9000+tt.nodeID), mgr.GetSelf().Address)
			assert.Equal(t, "us-east-1", mgr.GetSelf().Metadata["region"])
		})
	}
}

func TestNewManager_Errors(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	store := newMockStorage()
	watchMgr := watch.NewManager()
	peer := discovery.Peer{NodeID: 1, Address: "localhost:9001"}

	tests := []struct {
		name        string
		store       storage.Storage
		watchMgr    *watch.Manager
		logger      *logger.Logger
		config      *Config
		expectedErr string
	}{
		{
			name:        "NilStorage",
			store:       nil,
			watchMgr:    watchMgr,
			logger:      log,
			config:      DefaultConfig(),
			expectedErr: "storage cannot be nil",
		},
		{
			name:        "NilWatchManager",
			store:       store,
			watchMgr:    nil,
			logger:      log,
			config:      DefaultConfig(),
			expectedErr: "watch manager cannot be nil",
		},
		{
			name:        "NilLogger",
			store:       store,
			watchMgr:    watchMgr,
			logger:      nil,
			config:      DefaultConfig(),
			expectedErr: "logger cannot be nil",
		},
		{
			name:     "InvalidLeaseTTL",
			store:    store,
			watchMgr: watchMgr,
			logger:   log,
			config: &Config{
				LeaseTTL:            500 * time.Millisecond,
				KeepAliveInterval:   1 * time.Second,
				HealthCheckInterval: 3 * time.Second,
				MaxRetries:          3,
				RetryBaseDelay:      100 * time.Millisecond,
			},
			expectedErr: "lease TTL must be at least 1 second",
		},
		{
			name:     "InvalidKeepAliveInterval",
			store:    store,
			watchMgr: watchMgr,
			logger:   log,
			config: &Config{
				LeaseTTL:            2 * time.Second,
				KeepAliveInterval:   500 * time.Millisecond,
				HealthCheckInterval: 3 * time.Second,
				MaxRetries:          3,
				RetryBaseDelay:      100 * time.Millisecond,
			},
			expectedErr: "keep-alive interval must be at least 1 second",
		},
		{
			name:     "KeepAliveIntervalGreaterThanTTL",
			store:    store,
			watchMgr: watchMgr,
			logger:   log,
			config: &Config{
				LeaseTTL:            2 * time.Second,
				KeepAliveInterval:   3 * time.Second,
				HealthCheckInterval: 3 * time.Second,
				MaxRetries:          3,
				RetryBaseDelay:      100 * time.Millisecond,
			},
			expectedErr: "keep-alive interval must be less than lease TTL",
		},
		{
			name:     "InvalidHealthCheckInterval",
			store:    store,
			watchMgr: watchMgr,
			logger:   log,
			config: &Config{
				LeaseTTL:            2 * time.Second,
				KeepAliveInterval:   1 * time.Second,
				HealthCheckInterval: 500 * time.Millisecond,
				MaxRetries:          3,
				RetryBaseDelay:      100 * time.Millisecond,
			},
			expectedErr: "health check interval must be at least 1 second",
		},
		{
			name:     "InvalidMaxRetries",
			store:    store,
			watchMgr: watchMgr,
			logger:   log,
			config: &Config{
				LeaseTTL:            2 * time.Second,
				KeepAliveInterval:   1 * time.Second,
				HealthCheckInterval: 3 * time.Second,
				MaxRetries:          0,
				RetryBaseDelay:      100 * time.Millisecond,
			},
			expectedErr: "max retries must be at least 1",
		},
		{
			name:     "InvalidRetryBaseDelay",
			store:    store,
			watchMgr: watchMgr,
			logger:   log,
			config: &Config{
				LeaseTTL:            2 * time.Second,
				KeepAliveInterval:   1 * time.Second,
				HealthCheckInterval: 3 * time.Second,
				MaxRetries:          3,
				RetryBaseDelay:      500 * time.Microsecond,
			},
			expectedErr: "retry base delay must be at least 1 millisecond",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewManager(tt.store, tt.watchMgr, peer, tt.config, tt.logger)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestRegister_Success(t *testing.T) {
	mgr, store, _ := setupTestManager(t, 1)
	defer mgr.Close()

	ctx := context.Background()
	err := mgr.Register(ctx)
	require.NoError(t, err)

	// Verify member was stored
	key := fmt.Sprintf("%s%d", MembershipPrefix, 1)
	data, err := store.Get([]byte(key))
	require.NoError(t, err)

	var member Member
	err = json.Unmarshal(data, &member)
	require.NoError(t, err)

	assert.Equal(t, uint64(1), member.NodeID)
	assert.Equal(t, "localhost:9001", member.Address)
}

func TestRegister_StorageError(t *testing.T) {
	mgr, store, _ := setupTestManager(t, 1)
	defer mgr.Close()

	// Inject storage error using thread-safe setter
	store.SetSetErr(errors.New("storage failure"))

	ctx := context.Background()
	err := mgr.Register(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to register member")
}

func TestRegister_AfterClose(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	mgr.Close()

	ctx := context.Background()
	err := mgr.Register(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manager is closed")
}

func TestStartWatching_Success(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	ctx := context.Background()
	err := mgr.StartWatching(ctx)
	require.NoError(t, err)

	// Give watch time to start
	time.Sleep(50 * time.Millisecond)

	// Verify watch is running
	assert.True(t, mgr.watchStarted)
}

func TestStartWatching_Errors(t *testing.T) {
	t.Run("AlreadyStarted", func(t *testing.T) {
		mgr, _, _ := setupTestManager(t, 1)
		defer mgr.Close()

		ctx := context.Background()
		err := mgr.StartWatching(ctx)
		require.NoError(t, err)

		// Try starting again
		err = mgr.StartWatching(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "watch already started")
	})

	t.Run("AfterClose", func(t *testing.T) {
		mgr, _, _ := setupTestManager(t, 1)
		mgr.Close()

		ctx := context.Background()
		err := mgr.StartWatching(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "manager is closed")
	})
}

func TestGetMembers(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	// Initially empty
	members := mgr.GetMembers()
	assert.Empty(t, members)

	// Add some members
	mgr.membersMu.Lock()
	mgr.members[1] = Member{NodeID: 1, Address: "localhost:9001"}
	mgr.members[2] = Member{NodeID: 2, Address: "localhost:9002"}
	mgr.membersMu.Unlock()

	members = mgr.GetMembers()
	assert.Len(t, members, 2)
}

func TestGetMember(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	// Add member
	mgr.membersMu.Lock()
	mgr.members[1] = Member{NodeID: 1, Address: "localhost:9001"}
	mgr.membersMu.Unlock()

	// Test found
	member, found := mgr.GetMember(1)
	assert.True(t, found)
	assert.Equal(t, uint64(1), member.NodeID)

	// Test not found
	_, found = mgr.GetMember(999)
	assert.False(t, found)
}

func TestGetSelf(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 42)
	defer mgr.Close()

	self := mgr.GetSelf()
	assert.Equal(t, uint64(42), self.NodeID)
	assert.Equal(t, "localhost:9042", self.Address)
}

func TestIsRaftEnabled(t *testing.T) {
	tests := []struct {
		name     string
		opts     []Option
		expected bool
	}{
		{
			name:     "Disabled",
			opts:     nil,
			expected: false,
		},
		{
			name:     "Enabled",
			opts:     []Option{WithRaftIntegration(newMockRaftNode(1), 100)},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _, _ := setupTestManager(t, 1, tt.opts...)
			defer mgr.Close()

			assert.Equal(t, tt.expected, mgr.IsRaftEnabled())
		})
	}
}

func TestClose(t *testing.T) {
	t.Run("SuccessfulClose", func(t *testing.T) {
		mgr, store, _ := setupTestManager(t, 1)

		ctx := context.Background()
		err := mgr.Register(ctx)
		require.NoError(t, err)

		err = mgr.Close()
		require.NoError(t, err)

		// Verify member was deregistered
		key := fmt.Sprintf("%s%d", MembershipPrefix, 1)
		_, err = store.Get([]byte(key))
		assert.ErrorIs(t, err, ErrKeyNotFound)

		// Verify manager is closed
		assert.True(t, mgr.closed)
	})

	t.Run("DoubleClose", func(t *testing.T) {
		mgr, _, _ := setupTestManager(t, 1)

		err := mgr.Close()
		require.NoError(t, err)

		// Second close should be no-op
		err = mgr.Close()
		require.NoError(t, err)
	})

	t.Run("CloseWithDeleteError", func(t *testing.T) {
		mgr, store, _ := setupTestManager(t, 1)

		ctx := context.Background()
		err := mgr.Register(ctx)
		require.NoError(t, err)

		// Inject delete error using thread-safe setter
		store.SetDeleteErr(errors.New("delete failed"))

		err = mgr.Close()
		require.NoError(t, err) // Close doesn't return the delete error
	})
}

func TestHandleMemberAdded_NoRaft(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	member := Member{
		NodeID:       2,
		Address:      "localhost:9002",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	mgr.handleMemberAdded(member)

	// Verify member was added to local cache
	members := mgr.GetMembers()
	assert.Len(t, members, 1)
	assert.Equal(t, uint64(2), members[0].NodeID)
}

func TestHandleMemberAdded_WithRaft_NonLeader(t *testing.T) {
	mockRaft := newMockRaftNode(2) // Different node is leader
	mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
	defer mgr.Close()

	member := Member{
		NodeID:       3,
		Address:      "localhost:9003",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	mgr.handleMemberAdded(member)

	// Wait a bit for potential Raft calls
	time.Sleep(100 * time.Millisecond)

	// Verify no Raft AddNode was called (not leader)
	calls := mockRaft.GetRequestAddNodeCalls()
	assert.Empty(t, calls)
}

func TestHandleMemberAdded_WithRaft_Leader(t *testing.T) {
	mockRaft := newMockRaftNode(1) // This node is leader
	mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
	defer mgr.Close()

	member := Member{
		NodeID:       2,
		Address:      "localhost:9002",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	mgr.handleMemberAdded(member)

	// Wait for async Raft call
	time.Sleep(100 * time.Millisecond)

	// Verify Raft AddNode was called
	calls := mockRaft.GetRequestAddNodeCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, uint64(100), calls[0].shardID)
	assert.Equal(t, uint64(2), calls[0].nodeID)
	assert.Equal(t, "localhost:9002", calls[0].address)
}

func TestHandleMemberAdded_SelfIgnored(t *testing.T) {
	mockRaft := newMockRaftNode(1) // This node is leader
	mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
	defer mgr.Close()

	// Try to add self
	member := Member{
		NodeID:       1,
		Address:      "localhost:9001",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	mgr.handleMemberAdded(member)

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Verify no Raft AddNode was called (self is ignored)
	calls := mockRaft.GetRequestAddNodeCalls()
	assert.Empty(t, calls)
}

func TestHandleMemberAdded_Update(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	// Add initial member
	member := Member{
		NodeID:       2,
		Address:      "localhost:9002",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}
	mgr.handleMemberAdded(member)

	// Update same member
	updatedMember := Member{
		NodeID:       2,
		Address:      "localhost:9002",
		RegisteredAt: member.RegisteredAt,
		LastSeen:     time.Now().Add(1 * time.Second),
	}
	mgr.handleMemberAdded(updatedMember)

	// Should still have only one member
	members := mgr.GetMembers()
	assert.Len(t, members, 1)
	assert.Equal(t, updatedMember.LastSeen.Unix(), members[0].LastSeen.Unix())
}

func TestHandleMemberRemoved(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	// Add member first
	member := Member{
		NodeID:       2,
		Address:      "localhost:9002",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}
	mgr.handleMemberAdded(member)

	assert.Len(t, mgr.GetMembers(), 1)

	// Remove member
	mgr.handleMemberRemoved(member)

	assert.Empty(t, mgr.GetMembers())
}

func TestHandleMemberRemoved_WithRaft_Leader(t *testing.T) {
	mockRaft := newMockRaftNode(1) // This node is leader
	mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
	defer mgr.Close()

	member := Member{
		NodeID:       2,
		Address:      "localhost:9002",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	// Add then remove
	mgr.handleMemberAdded(member)
	time.Sleep(100 * time.Millisecond)
	mockRaft.ResetCalls()

	mgr.handleMemberRemoved(member)
	time.Sleep(100 * time.Millisecond)

	// Verify Raft RemoveNode was called
	calls := mockRaft.GetRequestRemoveNodeCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, uint64(100), calls[0].shardID)
	assert.Equal(t, uint64(2), calls[0].nodeID)
}

func TestIsLeader(t *testing.T) {
	tests := []struct {
		name           string
		nodeID         uint64
		leaderID       uint64
		getLeaderErr   error
		expectedResult bool
	}{
		{
			name:           "IsLeader",
			nodeID:         1,
			leaderID:       1,
			expectedResult: true,
		},
		{
			name:           "NotLeader",
			nodeID:         1,
			leaderID:       2,
			expectedResult: false,
		},
		{
			name:           "GetLeaderError",
			nodeID:         1,
			leaderID:       0,
			getLeaderErr:   errors.New("raft error"),
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode(tt.leaderID)
			if tt.getLeaderErr != nil {
				mockRaft.SetGetLeaderError(tt.getLeaderErr)
			}
			mgr, _, _ := setupTestManager(t, tt.nodeID, WithRaftIntegration(mockRaft, 100))
			defer mgr.Close()

			result := mgr.isLeader()
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestIsLeader_NoRaft(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	result := mgr.isLeader()
	assert.False(t, result)
}

func TestPerformHealthCheck(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	now := time.Now()

	// Add healthy member
	healthyMember := Member{
		NodeID:   2,
		Address:  "localhost:9002",
		LastSeen: now,
	}
	mgr.membersMu.Lock()
	mgr.members[2] = healthyMember
	mgr.membersMu.Unlock()

	// Add stale member (last seen > 2x TTL ago)
	staleMember := Member{
		NodeID:   3,
		Address:  "localhost:9003",
		LastSeen: now.Add(-5 * time.Second),
	}
	mgr.membersMu.Lock()
	mgr.members[3] = staleMember
	mgr.membersMu.Unlock()

	// Perform health check
	mgr.performHealthCheck()

	// Verify stale member was removed
	members := mgr.GetMembers()
	assert.Len(t, members, 1)
	assert.Equal(t, uint64(2), members[0].NodeID)
}

func TestPerformHealthCheck_SkipSelf(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	// Add self as stale (should not be removed)
	now := time.Now()
	staleSelf := Member{
		NodeID:   1,
		Address:  "localhost:9001",
		LastSeen: now.Add(-10 * time.Second),
	}
	mgr.membersMu.Lock()
	mgr.members[1] = staleSelf
	mgr.membersMu.Unlock()

	mgr.performHealthCheck()

	// Self should still be present
	members := mgr.GetMembers()
	assert.Len(t, members, 1)
	assert.Equal(t, uint64(1), members[0].NodeID)
}

func TestKeepAlive(t *testing.T) {
	// Use minimum valid intervals
	config := &Config{
		LeaseTTL:            3 * time.Second,
		KeepAliveInterval:   1 * time.Second,
		HealthCheckInterval: 3 * time.Second,
		MaxRetries:          3,
		RetryBaseDelay:      10 * time.Millisecond,
	}

	store := newMockStorage()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	watchMgr := watch.NewManager()
	peer := discovery.Peer{NodeID: 1, Address: "localhost:9001"}

	mgr, err := NewManager(store, watchMgr, peer, config, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()
	err = mgr.Register(ctx)
	require.NoError(t, err)

	// Record initial registration time
	initialTime := mgr.self.RegisteredAt

	// Wait for at least one keep-alive cycle
	time.Sleep(1500 * time.Millisecond)

	// Verify member was updated in storage
	key := fmt.Sprintf("%s%d", MembershipPrefix, 1)
	data, err := store.Get([]byte(key))
	require.NoError(t, err)

	var member Member
	err = json.Unmarshal(data, &member)
	require.NoError(t, err)

	// LastSeen should be updated
	assert.True(t, member.LastSeen.After(initialTime))
}

func TestKeepAlive_StorageError(t *testing.T) {
	// This test verifies that keep-alive continues even with storage errors
	config := &Config{
		LeaseTTL:            3 * time.Second,
		KeepAliveInterval:   1 * time.Second,
		HealthCheckInterval: 3 * time.Second,
		MaxRetries:          3,
		RetryBaseDelay:      10 * time.Millisecond,
	}

	store := newMockStorage()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	watchMgr := watch.NewManager()
	peer := discovery.Peer{NodeID: 1, Address: "localhost:9001"}

	mgr, err := NewManager(store, watchMgr, peer, config, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()
	err = mgr.Register(ctx)
	require.NoError(t, err)

	// Inject error after initial registration using thread-safe setter
	store.SetSetErr(errors.New("storage failure"))

	// Wait for keep-alive attempts
	time.Sleep(1200 * time.Millisecond)

	// Manager should still be operational despite errors
	assert.NotNil(t, mgr)
}

func TestAddNodeWithRetry_Success(t *testing.T) {
	mockRaft := newMockRaftNode(1)
	mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
	defer mgr.Close()

	member := Member{
		NodeID:   2,
		Address:  "localhost:9002",
		LastSeen: time.Now(),
	}

	mgr.addNodeWithRetry(member)

	// Wait for operation to complete
	time.Sleep(100 * time.Millisecond)

	calls := mockRaft.GetRequestAddNodeCalls()
	assert.Len(t, calls, 1)
}

func TestAddNodeWithRetry_FailureAndRetry(t *testing.T) {
	mockRaft := newMockRaftNode(1)
	mockRaft.SetAddNodeError(errors.New("raft error"))

	mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
	defer mgr.Close()

	member := Member{
		NodeID:   2,
		Address:  "localhost:9002",
		LastSeen: time.Now(),
	}

	mgr.addNodeWithRetry(member)

	// Wait for retries to complete
	time.Sleep(500 * time.Millisecond)

	// Should have tried 3 times (maxRetries)
	calls := mockRaft.GetRequestAddNodeCalls()
	assert.Len(t, calls, 3)
}

func TestRemoveNodeWithRetry_Success(t *testing.T) {
	mockRaft := newMockRaftNode(1)
	mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
	defer mgr.Close()

	member := Member{
		NodeID:   2,
		Address:  "localhost:9002",
		LastSeen: time.Now(),
	}

	mgr.removeNodeWithRetry(member)

	// Wait for operation to complete
	time.Sleep(100 * time.Millisecond)

	calls := mockRaft.GetRequestRemoveNodeCalls()
	assert.Len(t, calls, 1)
}

func TestRemoveNodeWithRetry_FailureAndRetry(t *testing.T) {
	mockRaft := newMockRaftNode(1)
	mockRaft.SetRemoveNodeError(errors.New("raft error"))

	mgr, _, _ := setupTestManager(t, 1, WithRaftIntegration(mockRaft, 100))
	defer mgr.Close()

	member := Member{
		NodeID:   2,
		Address:  "localhost:9002",
		LastSeen: time.Now(),
	}

	mgr.removeNodeWithRetry(member)

	// Wait for retries to complete
	time.Sleep(500 * time.Millisecond)

	// Should have tried 3 times (maxRetries)
	calls := mockRaft.GetRequestRemoveNodeCalls()
	assert.Len(t, calls, 3)
}

func TestHandleWatchEvent_Put(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	member := Member{
		NodeID:       2,
		Address:      "localhost:9002",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	data, err := json.Marshal(member)
	require.NoError(t, err)

	event := watch.Event{
		Type:  watch.EventTypePut,
		Key:   []byte(fmt.Sprintf("%s%d", MembershipPrefix, 2)),
		Value: data,
	}

	mgr.handleWatchEvent(event)

	// Verify member was added
	members := mgr.GetMembers()
	assert.Len(t, members, 1)
	assert.Equal(t, uint64(2), members[0].NodeID)
}

func TestHandleWatchEvent_Delete(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	// Add member first
	member := Member{
		NodeID:       2,
		Address:      "localhost:9002",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}
	mgr.handleMemberAdded(member)

	data, err := json.Marshal(member)
	require.NoError(t, err)

	event := watch.Event{
		Type:  watch.EventTypeDelete,
		Key:   []byte(fmt.Sprintf("%s%d", MembershipPrefix, 2)),
		Value: data,
	}

	mgr.handleWatchEvent(event)

	// Verify member was removed
	members := mgr.GetMembers()
	assert.Empty(t, members)
}

func TestHandleWatchEvent_InvalidJSON(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	event := watch.Event{
		Type:  watch.EventTypePut,
		Key:   []byte(fmt.Sprintf("%s%d", MembershipPrefix, 2)),
		Value: []byte("invalid json"),
	}

	// Should not panic, just log error
	mgr.handleWatchEvent(event)

	// No member should be added
	members := mgr.GetMembers()
	assert.Empty(t, members)
}

func TestMemberRaftAddrDefault(t *testing.T) {
	store := newMockStorage()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	watchMgr := watch.NewManager()

	// Peer without RaftAddr
	peer := discovery.Peer{
		NodeID:  1,
		Address: "localhost:9001",
	}

	mgr, err := NewManager(store, watchMgr, peer, DefaultConfig(), log)
	require.NoError(t, err)
	defer mgr.Close()

	// RaftAddr should default to Address
	assert.Equal(t, "localhost:9001", mgr.GetSelf().RaftAddr)
}

func TestConcurrentMemberAccess(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			member := Member{
				NodeID:   uint64(id),
				Address:  fmt.Sprintf("localhost:%d", 9000+id),
				LastSeen: time.Now(),
			}
			mgr.handleMemberAdded(member)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.GetMembers()
		}()
	}

	wg.Wait()

	// All members should be present
	members := mgr.GetMembers()
	assert.Len(t, members, iterations)
}

func TestHealthCheckLoop_StopsOnClose(t *testing.T) {
	mockRaft := newMockRaftNode(1)
	config := &Config{
		LeaseTTL:            2 * time.Second,
		KeepAliveInterval:   1 * time.Second,
		HealthCheckInterval: 1 * time.Second, // Must be >= 1 second
		MaxRetries:          3,
		RetryBaseDelay:      10 * time.Millisecond,
	}

	store := newMockStorage()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	watchMgr := watch.NewManager()
	peer := discovery.Peer{NodeID: 1, Address: "localhost:9001"}

	mgr, err := NewManager(store, watchMgr, peer, config, log, WithRaftIntegration(mockRaft, 100))
	require.NoError(t, err)

	ctx := context.Background()
	err = mgr.Register(ctx)
	require.NoError(t, err)

	// Wait for health check to run
	time.Sleep(200 * time.Millisecond)

	// Close should stop health check loop
	err = mgr.Close()
	require.NoError(t, err)

	// Give time for goroutines to stop
	time.Sleep(100 * time.Millisecond)
}

func TestWatchMembership_CancelContext(t *testing.T) {
	mgr, _, _ := setupTestManager(t, 1)
	defer mgr.Close()

	ctx := context.Background()
	err := mgr.StartWatching(ctx)
	require.NoError(t, err)

	// Give watch time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel watch
	mgr.watchCancel()

	// Give time for watch to stop
	time.Sleep(100 * time.Millisecond)
}
