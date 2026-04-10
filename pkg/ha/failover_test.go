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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockRaftNode implements the RaftNode interface for testing.
type mockRaftNode struct {
	mu            sync.RWMutex
	leaders       map[uint64]uint64 // shardID -> leaderID
	errors        map[uint64]error  // shardID -> error to return
	nodeInfo      *interfaces.NodeHostInfo
	closed        bool
	addedNodes    map[uint64]map[uint64]string // shardID -> (nodeID -> address)
	removedNodes  map[uint64]map[uint64]bool   // shardID -> (nodeID -> removed)
	startedGroups map[uint64]bool
	stoppedGroups map[uint64]bool
	proposals     [][]byte
	reads         []interface{}
	// For controlled leader changes using atomic operations
	atomicLeaders   map[uint64]*atomic.Uint64
	getLeaderIDFunc func(shardID uint64) (uint64, error)
}

func newMockRaftNode() *mockRaftNode {
	return &mockRaftNode{
		leaders:       make(map[uint64]uint64),
		errors:        make(map[uint64]error),
		addedNodes:    make(map[uint64]map[uint64]string),
		removedNodes:  make(map[uint64]map[uint64]bool),
		startedGroups: make(map[uint64]bool),
		stoppedGroups: make(map[uint64]bool),
		proposals:     make([][]byte, 0),
		reads:         make([]interface{}, 0),
		atomicLeaders: make(map[uint64]*atomic.Uint64),
	}
}

func (m *mockRaftNode) GetLeaderID(shardID uint64) (uint64, bool, error) {
	// Use custom function if set
	if m.getLeaderIDFunc != nil {
		id, err := m.getLeaderIDFunc(shardID)
		return id, id != 0 && err == nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, ok := m.errors[shardID]; ok {
		return 0, false, err
	}

	// Check if atomic leader is set for this shard
	if atomicLeader, ok := m.atomicLeaders[shardID]; ok {
		val := atomicLeader.Load()
		return val, val != 0, nil
	}

	leaderID, ok := m.leaders[shardID]
	if !ok {
		return 0, false, errors.Newf(errors.ErrNotFound, "mockRaftNode.GetLeaderID", "shard %d not found", shardID)
	}

	return leaderID, leaderID != 0, nil
}

func (m *mockRaftNode) GetShardRaftState(shardID uint64) (uint64, uint64, error) {
	return 0, 0, nil
}

func (m *mockRaftNode) setLeader(shardID, leaderID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaders[shardID] = leaderID
}

func (m *mockRaftNode) setError(shardID uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[shardID] = err
}

func (m *mockRaftNode) clearError(shardID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.errors, shardID)
}

// enableAtomicLeader enables atomic leader mode for a specific shard.
func (m *mockRaftNode) enableAtomicLeader(shardID, initialLeaderID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	atomicLeader := &atomic.Uint64{}
	atomicLeader.Store(initialLeaderID)
	m.atomicLeaders[shardID] = atomicLeader
}

// setAtomicLeader changes the leader in atomic mode for a specific shard.
func (m *mockRaftNode) setAtomicLeader(shardID, leaderID uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if atomicLeader, ok := m.atomicLeaders[shardID]; ok {
		atomicLeader.Store(leaderID)
	}
}

func (m *mockRaftNode) SyncPropose(ctx context.Context, shardID uint64, data []byte) (interfaces.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return interfaces.Result{}, errors.Internal("mockRaftNode.SyncPropose", "node is closed")
	}

	m.proposals = append(m.proposals, data)
	return interfaces.Result{}, nil
}

func (m *mockRaftNode) SyncRead(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, errors.Internal("mockRaftNode.SyncRead", "node is closed")
	}

	m.reads = append(m.reads, query)
	return query, nil
}

func (m *mockRaftNode) GetNodeHostInfo(_ interfaces.NodeHostInfoOption) *interfaces.NodeHostInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodeInfo
}

func (m *mockRaftNode) RequestAddNode(_ context.Context, shardID, nodeID uint64, address string, _ uint64) (interfaces.RequestState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.addedNodes[shardID] == nil {
		m.addedNodes[shardID] = make(map[uint64]string)
	}
	m.addedNodes[shardID][nodeID] = address
	return nil, nil
}

func (m *mockRaftNode) RequestRemoveNode(_ context.Context, shardID, nodeID uint64, _ uint64) (interfaces.RequestState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.removedNodes[shardID] == nil {
		m.removedNodes[shardID] = make(map[uint64]bool)
	}
	m.removedNodes[shardID][nodeID] = true
	return nil, nil
}

func (m *mockRaftNode) StartShard(_ map[uint64]string, _ bool, _ interfaces.CreateFunc, cfg interfaces.ShardConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startedGroups[cfg.ShardID] = true
	return nil
}

func (m *mockRaftNode) StopShard(shardID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stoppedGroups[shardID] = true
	return nil
}

func (m *mockRaftNode) ReadWithConsistency(ctx context.Context, shardID uint64, query interface{}, opts interfaces.ReadOptions) (interface{}, error) {
	return m.SyncRead(ctx, shardID, query)
}

func (m *mockRaftNode) ID() string          { return "mock-ha" }
func (m *mockRaftNode) RaftAddress() string { return "" }
func (m *mockRaftNode) ShardCount() int     { return 0 }
func (m *mockRaftNode) HealthCheck() *interfaces.HealthStatus {
	return &interfaces.HealthStatus{Status: "ok"}
}
func (m *mockRaftNode) Drain() error     { return nil }
func (m *mockRaftNode) IsDraining() bool { return false }

func (m *mockRaftNode) ProposeWithMode(ctx context.Context, shardID uint64, data []byte, _ interfaces.WriteMode) (interfaces.Result, error) {
	return m.SyncPropose(ctx, shardID, data)
}

func (m *mockRaftNode) StaleRead(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
	return m.SyncRead(ctx, shardID, query)
}

func (m *mockRaftNode) RequestSnapshot(_ context.Context, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNode) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

// mockLogger implements the Logger interface for testing.
// The mutex is a pointer so it can be shared across derived loggers
// created by WithField, WithFields, and WithError to prevent data races.
type mockLogger struct {
	mu     *sync.Mutex
	logs   []string
	fields map[string]interface{}
	errors []error
}

func newMockLogger() *mockLogger {
	return &mockLogger{
		mu:     &sync.Mutex{},
		logs:   make([]string, 0),
		fields: make(map[string]interface{}),
		errors: make([]error, 0),
	}
}

func (m *mockLogger) Debug(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf("DEBUG: %s", msg))
}

func (m *mockLogger) Debugf(format string, args ...interface{}) {
	m.Debug(fmt.Sprintf(format, args...))
}

func (m *mockLogger) Info(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf("INFO: %s", msg))
}

func (m *mockLogger) Infof(format string, args ...interface{}) {
	m.Info(fmt.Sprintf(format, args...))
}

func (m *mockLogger) Warn(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf("WARN: %s", msg))
}

func (m *mockLogger) Warnf(format string, args ...interface{}) {
	m.Warn(fmt.Sprintf(format, args...))
}

func (m *mockLogger) Error(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf("ERROR: %s", msg))
}

func (m *mockLogger) Errorf(format string, args ...interface{}) {
	m.Error(fmt.Sprintf(format, args...))
}

func (m *mockLogger) Fatal(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf("FATAL: %s", msg))
}

func (m *mockLogger) Fatalf(format string, args ...interface{}) {
	m.Fatal(fmt.Sprintf(format, args...))
}

func (m *mockLogger) WithField(key string, value interface{}) interfaces.Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	newLogger := &mockLogger{
		mu:     m.mu,
		logs:   m.logs,
		fields: make(map[string]interface{}),
		errors: m.errors,
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

func (m *mockLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	newLogger := &mockLogger{
		mu:     m.mu,
		logs:   m.logs,
		fields: make(map[string]interface{}),
		errors: m.errors,
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

func (m *mockLogger) WithError(err error) interfaces.Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	newLogger := &mockLogger{
		mu:     m.mu,
		logs:   m.logs,
		fields: make(map[string]interface{}),
		errors: append([]error{}, m.errors...),
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	newLogger.errors = append(newLogger.errors, err)
	return newLogger
}

func (m *mockLogger) hasLog(substr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, log := range m.logs {
		if contains(log, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestNewFailoverManager tests the constructor with various configurations.
func TestNewFailoverManager(t *testing.T) {
	tests := []struct {
		name        string
		raftNode    interfaces.RaftNode
		config      *FailoverConfig
		logger      interfaces.Logger
		expectError bool
		errorCode   errors.ErrorCode
	}{
		{
			name:     "valid configuration",
			raftNode: newMockRaftNode(),
			config: &FailoverConfig{
				HealthCheckInterval: 5 * time.Second,
				FailoverTimeout:     30 * time.Second,
				MaxFailoversPerHour: 10,
				Shards:              []uint64{1, 2, 3},
				LeaderTimeout:       10 * time.Second,
				HealthCheckTimeout:  5 * time.Second,
			},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:     "config with defaults",
			raftNode: newMockRaftNode(),
			config: &FailoverConfig{
				Shards: []uint64{1},
			},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "nil raft node",
			raftNode:    nil,
			config:      &FailoverConfig{},
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
			config:      &FailoverConfig{},
			logger:      nil,
			expectError: true,
			errorCode:   errors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := NewFailoverManager(tt.raftNode, tt.config, tt.logger)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != errors.ErrUnknown {
					dragonErr, ok := err.(*errors.DragonError)
					require.True(t, ok, "expected DragonError")
					assert.Equal(t, tt.errorCode, dragonErr.Code)
				}
				assert.Nil(t, fm)
			} else {
				require.NoError(t, err)
				require.NotNil(t, fm)

				// Verify defaults are applied
				assert.Equal(t, 5*time.Second, fm.config.HealthCheckInterval)
				assert.Equal(t, 30*time.Second, fm.config.FailoverTimeout)
				assert.Equal(t, 10, fm.config.MaxFailoversPerHour)
				assert.Equal(t, 10*time.Second, fm.config.LeaderTimeout)
				assert.Equal(t, 5*time.Second, fm.config.HealthCheckTimeout)

				// Verify initialization
				assert.NotNil(t, fm.ctx)
				assert.NotNil(t, fm.cancel)
				assert.NotNil(t, fm.stopChan)
				assert.NotNil(t, fm.failoverHistory)
				assert.NotNil(t, fm.metrics)
			}
		})
	}
}

// TestFailoverManager_StartStop tests the lifecycle management.
func TestFailoverManager_StartStop(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &FailoverConfig{
		HealthCheckInterval: 100 * time.Millisecond,
		Shards:              []uint64{1},
	}

	fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Test Start
	err = fm.Start()
	require.NoError(t, err)

	// Give it a moment to start the monitor loop
	time.Sleep(50 * time.Millisecond)

	// Test Stop
	err = fm.Stop()
	require.NoError(t, err)

	// Test double stop (should be idempotent)
	err = fm.Stop()
	require.NoError(t, err)
}

// TestFailoverManager_GetStatus tests status reporting.
func TestFailoverManager_GetStatus(t *testing.T) {
	tests := []struct {
		name                  string
		setupMock             func(*mockRaftNode)
		addRecentEvents       int
		expectHealthy         bool
		expectUnhealthyShards int
	}{
		{
			name: "all healthy",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.setLeader(2, 2)
			},
			addRecentEvents:       0,
			expectHealthy:         true,
			expectUnhealthyShards: 0,
		},
		{
			name: "one unhealthy",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.setError(2, errors.Internal("test", "error"))
			},
			addRecentEvents:       3,
			expectHealthy:         false,
			expectUnhealthyShards: 1,
		},
		{
			name: "all unhealthy",
			setupMock: func(m *mockRaftNode) {
				m.setError(1, errors.Internal("test", "no leader"))
				m.setError(2, errors.Internal("test", "no leader"))
			},
			addRecentEvents:       5,
			expectHealthy:         false,
			expectUnhealthyShards: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &FailoverConfig{
				Shards: []uint64{1, 2},
			}

			fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			// Add recent events
			now := time.Now()
			for i := 0; i < tt.addRecentEvents; i++ {
				fm.recordFailoverEvent(FailoverEvent{
					Timestamp: now.Add(-time.Duration(i) * time.Minute),
					ShardID:   1,
				})
			}

			status := fm.GetStatus()

			assert.Equal(t, tt.expectHealthy, status.Healthy)
			assert.Len(t, status.UnhealthyShards, tt.expectUnhealthyShards)
			assert.Equal(t, tt.addRecentEvents, status.FailoversLastHour)

			// Recent events should be at most 10
			if tt.addRecentEvents > 10 {
				assert.Len(t, status.RecentEvents, 10)
			} else {
				assert.Len(t, status.RecentEvents, tt.addRecentEvents)
			}
		})
	}
}

// TestFailoverManager_PerformHealthCheck tests health check execution.
func TestFailoverManager_PerformHealthCheck(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(*mockRaftNode)
		shards        []uint64
		expectError   bool
		expectHealthy int
	}{
		{
			name: "all shards healthy",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.setLeader(2, 2)
				m.setLeader(3, 3)
			},
			shards:        []uint64{1, 2, 3},
			expectError:   false,
			expectHealthy: 3,
		},
		{
			name: "one shard unhealthy",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
				m.setError(2, errors.Internal("test", "no leader"))
				m.setLeader(3, 3)
			},
			shards:        []uint64{1, 2, 3},
			expectError:   false,
			expectHealthy: 2,
		},
		{
			name: "all shards unhealthy",
			setupMock: func(m *mockRaftNode) {
				m.setError(1, errors.Internal("test", "no leader"))
				m.setError(2, errors.Internal("test", "no leader"))
			},
			shards:        []uint64{1, 2},
			expectError:   false,
			expectHealthy: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &FailoverConfig{
				Shards:             tt.shards,
				HealthCheckTimeout: 1 * time.Second,
			}

			fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			err = fm.performHealthCheck()

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestFailoverManager_CheckShardHealth tests individual shard health checks.
func TestFailoverManager_CheckShardHealth(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*mockRaftNode)
		shardID     uint64
		expectError bool
		errorCode   errors.ErrorCode
	}{
		{
			name: "leader available",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 1)
			},
			shardID:     1,
			expectError: false,
		},
		{
			name: "no leader (ID is 0)",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
			},
			shardID:     1,
			expectError: true,
			errorCode:   errors.ErrLeaderNotAvailable,
		},
		{
			name: "GetLeaderID returns error",
			setupMock: func(m *mockRaftNode) {
				m.setError(1, errors.New(errors.ErrRaft, "test", "raft error"))
			},
			shardID:     1,
			expectError: true,
			errorCode:   errors.ErrLeaderNotAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &FailoverConfig{
				Shards: []uint64{1},
			}

			fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			err = fm.checkShardHealth(ctx, tt.shardID)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*errors.DragonError)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, dragonErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestFailoverManager_TriggerFailover tests failover triggering logic.
func TestFailoverManager_TriggerFailover(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*mockRaftNode)
		shardID        uint64
		reason         string
		addPriorEvents int
		expectError    bool
		errorCode      errors.ErrorCode
	}{
		{
			name: "successful_failover",
			setupMock: func(m *mockRaftNode) {
				// Return 1 first time (old leader), then 2 (new leader)
				callCount := 0
				m.getLeaderIDFunc = func(shardID uint64) (uint64, error) {
					callCount++
					if callCount == 1 {
						return 1, nil // First call gets old leader
					}
					return 2, nil // Subsequent calls get new leader
				}
			},
			shardID:        1,
			reason:         "leader unavailable",
			addPriorEvents: 0,
			expectError:    false,
		},
		{
			name: "rate limited",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
			},
			shardID:        1,
			reason:         "test",
			addPriorEvents: 15, // Exceeds limit of 10
			expectError:    true,
			errorCode:      errors.ErrRateLimited,
		},
		{
			name: "timeout waiting for new leader",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
			},
			shardID:        1,
			reason:         "no leader",
			addPriorEvents: 0,
			expectError:    true,
			errorCode:      errors.ErrTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &FailoverConfig{
				Shards:              []uint64{1},
				MaxFailoversPerHour: 10,
				FailoverTimeout:     1 * time.Second,
			}

			fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			// Add prior events if needed
			now := time.Now()
			for i := 0; i < tt.addPriorEvents; i++ {
				fm.recordFailoverEvent(FailoverEvent{
					Timestamp: now.Add(-time.Duration(i) * time.Minute),
					ShardID:   tt.shardID,
				})
			}

			ctx := context.Background()
			err = fm.triggerFailover(ctx, tt.shardID, tt.reason)

			if tt.expectError {
				require.Error(t, err)
				dragonErr, ok := err.(*errors.DragonError)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, dragonErr.Code)
			} else {
				require.NoError(t, err)
				// Verify event was recorded
				history := fm.GetFailoverHistory()
				assert.Greater(t, len(history), 0)
			}
		})
	}
}

// TestFailoverManager_WaitForNewLeader tests leader election waiting.
func TestFailoverManager_WaitForNewLeader(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(*mockRaftNode)
		oldLeaderID   uint64
		expectError   bool
		expectTimeout bool
		errorCode     errors.ErrorCode
	}{
		{
			name: "successful_leader_election",
			setupMock: func(m *mockRaftNode) {
				// Always return new leader
				m.setLeader(1, 2)
			},
			oldLeaderID: 1,
			expectError: false,
		},
		{
			name: "timeout",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
			},
			oldLeaderID:   0,
			expectError:   true,
			expectTimeout: true,
			errorCode:     errors.ErrTimeout,
		},
		{
			name: "context cancelled",
			setupMock: func(m *mockRaftNode) {
				m.setLeader(1, 0)
			},
			oldLeaderID: 0,
			expectError: true,
		},
		{
			name: "old_leader_is_0,_new_leader_elected",
			setupMock: func(m *mockRaftNode) {
				// Return new leader
				m.setLeader(1, 2)
			},
			oldLeaderID: 0,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			tt.setupMock(mockRaft)

			config := &FailoverConfig{
				Shards:          []uint64{1},
				FailoverTimeout: 1 * time.Second,
			}

			fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			var ctx context.Context
			var cancel context.CancelFunc

			if tt.name == "context cancelled" {
				ctx, cancel = context.WithCancel(context.Background())
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()
			} else {
				ctx = context.Background()
			}

			newLeaderID, err := fm.waitForNewLeader(ctx, 1, tt.oldLeaderID)

			if tt.expectError {
				require.Error(t, err)
				if tt.expectTimeout {
					dragonErr, ok := err.(*errors.DragonError)
					require.True(t, ok)
					assert.Equal(t, tt.errorCode, dragonErr.Code)
				}
			} else {
				require.NoError(t, err)
				assert.NotEqual(t, uint64(0), newLeaderID)
				assert.NotEqual(t, tt.oldLeaderID, newLeaderID)
			}
		})
	}
}

// TestFailoverManager_ExceedsFailoverLimit tests rate limiting logic.
func TestFailoverManager_ExceedsFailoverLimit(t *testing.T) {
	tests := []struct {
		name          string
		eventCount    int
		eventAges     []time.Duration
		limit         int
		expectExceeds bool
	}{
		{
			name:          "empty history",
			eventCount:    0,
			eventAges:     []time.Duration{},
			limit:         10,
			expectExceeds: false,
		},
		{
			name:       "under limit",
			eventCount: 5,
			eventAges: []time.Duration{
				5 * time.Minute,
				10 * time.Minute,
				15 * time.Minute,
				20 * time.Minute,
				25 * time.Minute,
			},
			limit:         10,
			expectExceeds: false,
		},
		{
			name:       "at limit",
			eventCount: 10,
			eventAges: []time.Duration{
				5 * time.Minute,
				10 * time.Minute,
				15 * time.Minute,
				20 * time.Minute,
				25 * time.Minute,
				30 * time.Minute,
				35 * time.Minute,
				40 * time.Minute,
				45 * time.Minute,
				50 * time.Minute,
			},
			limit:         10,
			expectExceeds: true,
		},
		{
			name:       "over limit",
			eventCount: 15,
			eventAges: []time.Duration{
				1 * time.Minute,
				2 * time.Minute,
				3 * time.Minute,
				4 * time.Minute,
				5 * time.Minute,
				10 * time.Minute,
				15 * time.Minute,
				20 * time.Minute,
				25 * time.Minute,
				30 * time.Minute,
				35 * time.Minute,
				40 * time.Minute,
				45 * time.Minute,
				50 * time.Minute,
				55 * time.Minute,
			},
			limit:         10,
			expectExceeds: true,
		},
		{
			name:       "old events don't count",
			eventCount: 5,
			eventAges: []time.Duration{
				70 * time.Minute,
				75 * time.Minute,
				80 * time.Minute,
				90 * time.Minute,
				120 * time.Minute,
			},
			limit:         10,
			expectExceeds: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &FailoverConfig{
				Shards:              []uint64{1},
				MaxFailoversPerHour: tt.limit,
			}

			fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			// Add events with specified ages
			now := time.Now()
			for i, age := range tt.eventAges {
				fm.recordFailoverEvent(FailoverEvent{
					Timestamp: now.Add(-age),
					ShardID:   1,
					Success:   i%2 == 0,
				})
			}

			exceeds := fm.exceedsFailoverLimit()
			assert.Equal(t, tt.expectExceeds, exceeds)
		})
	}
}

// TestFailoverManager_RecordFailoverEvent tests event recording.
func TestFailoverManager_RecordFailoverEvent(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &FailoverConfig{
		Shards: []uint64{1},
	}

	fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("single event", func(t *testing.T) {
		event := FailoverEvent{
			Timestamp:   time.Now(),
			ShardID:     1,
			OldLeaderID: 1,
			NewLeaderID: 2,
			Success:     true,
		}

		fm.recordFailoverEvent(event)
		history := fm.GetFailoverHistory()
		assert.Len(t, history, 1)
		assert.Equal(t, event.ShardID, history[0].ShardID)
	})

	t.Run("multiple events", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			event := FailoverEvent{
				Timestamp:   time.Now(),
				ShardID:     uint64(i),
				OldLeaderID: 1,
				NewLeaderID: 2,
				Success:     true,
			}
			fm.recordFailoverEvent(event)
		}

		history := fm.GetFailoverHistory()
		assert.Len(t, history, 11) // 1 from previous test + 10
	})

	t.Run("truncation at 100", func(t *testing.T) {
		// Add more events to exceed 100
		for i := 0; i < 100; i++ {
			event := FailoverEvent{
				Timestamp:   time.Now(),
				ShardID:     1,
				OldLeaderID: 1,
				NewLeaderID: 2,
				Success:     true,
			}
			fm.recordFailoverEvent(event)
		}

		history := fm.GetFailoverHistory()
		assert.Len(t, history, 100)
	})

	t.Run("thread safety", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 10
		eventsPerGoroutine := 5

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < eventsPerGoroutine; j++ {
					event := FailoverEvent{
						Timestamp:   time.Now(),
						ShardID:     uint64(id),
						OldLeaderID: 1,
						NewLeaderID: 2,
						Success:     true,
					}
					fm.recordFailoverEvent(event)
				}
			}(i)
		}

		wg.Wait()

		// Should have at most 100 events due to truncation
		history := fm.GetFailoverHistory()
		assert.LessOrEqual(t, len(history), 100)
	})
}

// TestFailoverManager_GetFailoverHistory tests history retrieval.
func TestFailoverManager_GetFailoverHistory(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &FailoverConfig{
		Shards: []uint64{1},
	}

	fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("empty history", func(t *testing.T) {
		history := fm.GetFailoverHistory()
		assert.NotNil(t, history)
		assert.Len(t, history, 0)
	})

	t.Run("with events", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			event := FailoverEvent{
				Timestamp:   time.Now(),
				ShardID:     1,
				OldLeaderID: 1,
				NewLeaderID: 2,
				Success:     true,
			}
			fm.recordFailoverEvent(event)
		}

		history := fm.GetFailoverHistory()
		assert.Len(t, history, 5)
	})

	t.Run("immutability", func(t *testing.T) {
		history1 := fm.GetFailoverHistory()
		originalLen := len(history1)

		// Modify returned slice
		history1 = append(history1, FailoverEvent{
			Timestamp:   time.Now(),
			ShardID:     999,
			OldLeaderID: 1,
			NewLeaderID: 2,
			Success:     true,
		})

		// Get history again - should not be affected
		history2 := fm.GetFailoverHistory()
		assert.Len(t, history2, originalLen)
	})
}

// TestFailoverManager_ConcurrentAccess tests thread safety.
func TestFailoverManager_ConcurrentAccess(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &FailoverConfig{
		Shards: []uint64{1, 2, 3},
	}

	fm, err := NewFailoverManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 20

	t.Run("concurrent GetStatus", func(t *testing.T) {
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				status := fm.GetStatus()
				assert.NotNil(t, status)
			}()
		}
		wg.Wait()
	})

	t.Run("concurrent RecordFailoverEvent", func(t *testing.T) {
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				event := FailoverEvent{
					Timestamp:   time.Now(),
					ShardID:     uint64(id % 3),
					OldLeaderID: 1,
					NewLeaderID: 2,
					Success:     true,
				}
				fm.recordFailoverEvent(event)
			}(i)
		}
		wg.Wait()
	})

	t.Run("concurrent health checks", func(t *testing.T) {
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = fm.performHealthCheck()
			}()
		}
		wg.Wait()
	})
}
