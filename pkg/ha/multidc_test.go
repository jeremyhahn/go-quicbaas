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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestNewMultiDCManager tests the constructor with various configurations.
func TestNewMultiDCManager(t *testing.T) {
	tests := []struct {
		name        string
		raftNode    interfaces.RaftNode
		config      *MultiDCConfig
		logger      interfaces.Logger
		expectError bool
		errorCode   errors.ErrorCode
	}{
		{
			name:     "valid configuration",
			raftNode: newMockRaftNode(),
			config: &MultiDCConfig{
				LocalDC: "dc1",
				RemoteDCs: map[string]*RemoteDCConfig{
					"dc2": {
						Name:           "dc2",
						Addresses:      []string{"dc2-node1:8001", "dc2-node2:8001"},
						Priority:       100,
						ReadPreference: ReadPreferLocal,
					},
				},
				ReplicationMode:     ReplicationAsync,
				ConsistencyModel:    ConsistencyEventual,
				LatencyThreshold:    100,
				HealthCheckInterval: 30 * time.Second,
				SyncTimeout:         10 * time.Second,
			},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:     "config with defaults",
			raftNode: newMockRaftNode(),
			config: &MultiDCConfig{
				LocalDC: "dc1",
			},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "nil raft node",
			raftNode:    nil,
			config:      &MultiDCConfig{},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mdm, err := NewMultiDCManager(tt.raftNode, tt.config, tt.logger)

			if tt.expectError {
				require.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				require.True(t, ok, "expected QBError")
				assert.Equal(t, tt.errorCode, qbErr.Code)
				assert.Nil(t, mdm)
			} else {
				require.NoError(t, err)
				require.NotNil(t, mdm)

				// Verify defaults are applied
				assert.Equal(t, ReplicationAsync, mdm.config.ReplicationMode)
				assert.Equal(t, ConsistencyEventual, mdm.config.ConsistencyModel)
				assert.Equal(t, int64(100), mdm.config.LatencyThreshold)
				assert.Equal(t, 30*time.Second, mdm.config.HealthCheckInterval)
				assert.Equal(t, 10*time.Second, mdm.config.SyncTimeout)

				// Verify initialization
				assert.NotNil(t, mdm.datacenters)
				assert.NotNil(t, mdm.metrics)
			}
		})
	}
}

// TestMultiDCManager_StartStop tests lifecycle management.
func TestMultiDCManager_StartStop(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC:             "dc1",
		HealthCheckInterval: 100 * time.Millisecond,
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name:      "dc2",
				Addresses: []string{"dc2-node1:8001"},
				Priority:  100,
			},
		},
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Test Start
	err = mdm.Start()
	require.NoError(t, err)
	assert.True(t, mdm.replicationActive)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Test Stop
	err = mdm.Stop()
	require.NoError(t, err)
	assert.False(t, mdm.replicationActive)

	// Test double stop (should be idempotent)
	err = mdm.Stop()
	require.NoError(t, err)
}

// TestMultiDCManager_RouteRead tests read routing logic.
func TestMultiDCManager_RouteRead(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name:      "dc2",
				Addresses: []string{"dc2-node1:8001"},
				Priority:  100,
			},
			"dc3": {
				Name:      "dc3",
				Addresses: []string{"dc3-node1:8001"},
				Priority:  50,
			},
		},
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	tests := []struct {
		name       string
		preference ReadPreference
		expectDC   string
	}{
		{
			name:       "prefer local",
			preference: ReadPreferLocal,
			expectDC:   "dc1",
		},
		{
			name:       "prefer_nearest",
			preference: ReadPreferNearest,
			expectDC:   "dc1", // Local DC initially (0 latency)
		},
		{
			name:       "prefer primary",
			preference: ReadPreferPrimary,
			expectDC:   "dc2", // dc2 has higher priority
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc, err := mdm.RouteRead(ctx, tt.preference)
			require.NoError(t, err)
			assert.Equal(t, tt.expectDC, dc)
		})
	}
}

// TestMultiDCManager_FindNearestDC tests nearest datacenter selection.
func TestMultiDCManager_FindNearestDC(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name:      "dc2",
				Addresses: []string{"dc2-node1:8001"},
			},
		},
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Set different latencies - dc2 has 50ms latency
	mdm.datacentersMu.Lock()
	mdm.datacenters["dc2"].Latency = 50 * time.Millisecond
	mdm.datacenters["dc2"].Healthy = true
	mdm.datacentersMu.Unlock()

	dc, err := mdm.findNearestDC()
	require.NoError(t, err)
	// Should prefer local DC (0 latency) over dc2 (50ms latency)
	assert.Equal(t, "dc1", dc)
}

// TestMultiDCManager_FindPrimaryDC tests primary datacenter selection.
func TestMultiDCManager_FindPrimaryDC(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name:     "dc2",
				Priority: 100,
			},
			"dc3": {
				Name:     "dc3",
				Priority: 200,
			},
		},
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Mark dc3 as healthy
	mdm.datacentersMu.Lock()
	mdm.datacenters["dc3"].Healthy = true
	mdm.datacentersMu.Unlock()

	dc, err := mdm.findPrimaryDC()
	require.NoError(t, err)
	assert.Equal(t, "dc3", dc) // dc3 has highest priority
}

// TestMultiDCManager_ReplicateToRemoteDC tests cross-DC replication.
func TestMultiDCManager_ReplicateToRemoteDC(t *testing.T) {
	tests := []struct {
		name            string
		targetDC        string
		replicationMode ReplicationMode
		dcHealthy       bool
		expectError     bool
		errorCode       errors.ErrorCode
	}{
		{
			name:            "sync replication to healthy DC",
			targetDC:        "dc2",
			replicationMode: ReplicationSync,
			dcHealthy:       true,
			expectError:     false,
		},
		{
			name:            "async replication to healthy DC",
			targetDC:        "dc2",
			replicationMode: ReplicationAsync,
			dcHealthy:       true,
			expectError:     false,
		},
		{
			name:            "replication to non-existent DC",
			targetDC:        "dc999",
			replicationMode: ReplicationSync,
			dcHealthy:       true,
			expectError:     true,
			errorCode:       errors.ErrNotFound,
		},
		{
			name:            "replication to unhealthy DC",
			targetDC:        "dc2",
			replicationMode: ReplicationSync,
			dcHealthy:       false,
			expectError:     true,
			errorCode:       errors.ErrUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &MultiDCConfig{
				LocalDC: "dc1",
				RemoteDCs: map[string]*RemoteDCConfig{
					"dc2": {
						Name: "dc2",
					},
				},
				ReplicationMode: tt.replicationMode,
				SyncTimeout:     1 * time.Second,
			}

			mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			// Set DC health
			if dc, exists := mdm.datacenters[tt.targetDC]; exists {
				mdm.datacentersMu.Lock()
				dc.Healthy = tt.dcHealthy
				mdm.datacentersMu.Unlock()
			}

			ctx := context.Background()
			err = mdm.ReplicateToRemoteDC(ctx, tt.targetDC, "test-key", []byte("test-value"))

			if tt.expectError {
				require.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, qbErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMultiDCManager_SyncReplicate tests synchronous replication.
func TestMultiDCManager_SyncReplicate(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC:     "dc1",
		SyncTimeout: 1 * time.Second,
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()
	err = mdm.syncReplicate(ctx, "dc2", "test-key", []byte("test-value"))
	require.NoError(t, err)
}

// TestMultiDCManager_AsyncReplicate tests asynchronous replication.
func TestMultiDCManager_AsyncReplicate(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()
	err = mdm.asyncReplicate(ctx, "dc2", "test-key", []byte("test-value"))
	require.NoError(t, err)
}

// TestMultiDCManager_HandleDCFailure tests datacenter failure handling.
func TestMultiDCManager_HandleDCFailure(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name:     "dc2",
				Priority: 100,
			},
			"dc3": {
				Name:     "dc3",
				Priority: 200,
			},
		},
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Mark dc3 as healthy
	mdm.datacentersMu.Lock()
	mdm.datacenters["dc3"].Healthy = true
	mdm.datacentersMu.Unlock()

	ctx := context.Background()
	err = mdm.HandleDCFailure(ctx, "dc2")
	require.NoError(t, err)
}

// TestMultiDCManager_GetStatus tests status reporting.
func TestMultiDCManager_GetStatus(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name: "dc2",
			},
			"dc3": {
				Name: "dc3",
			},
		},
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Mark dc2 as unhealthy
	mdm.datacentersMu.Lock()
	mdm.datacenters["dc2"].Healthy = false
	mdm.datacentersMu.Unlock()

	// Start replication
	err = mdm.Start()
	require.NoError(t, err)

	status := mdm.GetStatus()
	assert.Equal(t, "dc1", status.LocalDC)
	assert.True(t, status.ReplicationActive)
	assert.Contains(t, status.UnhealthyDCs, "dc2")
	assert.NotNil(t, status.ReplicationLag)

	err = mdm.Stop()
	require.NoError(t, err)
}

// TestMultiDCManager_GetDatacenterInfo tests datacenter info retrieval.
func TestMultiDCManager_GetDatacenterInfo(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name:     "dc2",
				Priority: 100,
			},
		},
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("existing datacenter", func(t *testing.T) {
		dc, err := mdm.GetDatacenterInfo("dc2")
		require.NoError(t, err)
		assert.Equal(t, "dc2", dc.Name)
		assert.Equal(t, 100, dc.Priority)
	})

	t.Run("non-existent datacenter", func(t *testing.T) {
		_, err := mdm.GetDatacenterInfo("dc999")
		require.Error(t, err)
		qbErr, ok := err.(*errors.QBError)
		require.True(t, ok)
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})
}

// TestMultiDCManager_CheckDatacenterHealth tests health checking.
func TestMultiDCManager_CheckDatacenterHealth(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name: "dc2",
			},
		},
		LatencyThreshold: 100,
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	mdm.checkDatacenterHealth()

	// Verify health was checked
	mdm.datacentersMu.RLock()
	dc := mdm.datacenters["dc2"]
	mdm.datacentersMu.RUnlock()

	assert.NotZero(t, dc.LastCheck)
}

// TestMultiDCManager_CheckDC tests individual DC health check.
func TestMultiDCManager_CheckDC(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC:          "dc1",
		LatencyThreshold: 100,
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	dc := &Datacenter{
		Name:    "dc2",
		Healthy: true,
	}

	healthy, latency := mdm.checkDC(dc)
	assert.True(t, healthy)
	assert.Greater(t, latency, time.Duration(0))
}

// TestMultiDCManager_ConcurrentReplication tests concurrent replication operations.
func TestMultiDCManager_ConcurrentReplication(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {
				Name: "dc2",
			},
			"dc3": {
				Name: "dc3",
			},
		},
		ReplicationMode: ReplicationAsync,
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Mark DCs as healthy
	mdm.datacentersMu.Lock()
	mdm.datacenters["dc2"].Healthy = true
	mdm.datacenters["dc3"].Healthy = true
	mdm.datacentersMu.Unlock()

	var wg sync.WaitGroup
	numGoroutines := 50

	ctx := context.Background()

	// Concurrent replications
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			targetDC := "dc2"
			if id%2 == 0 {
				targetDC = "dc3"
			}
			_ = mdm.ReplicateToRemoteDC(ctx, targetDC, "key", []byte("value"))
		}(i)
	}
	wg.Wait()

	// No panic means thread safety is working
}

// TestMultiDCManager_ConcurrentStatusChecks tests concurrent status operations.
func TestMultiDCManager_ConcurrentStatusChecks(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &MultiDCConfig{
		LocalDC: "dc1",
		RemoteDCs: map[string]*RemoteDCConfig{
			"dc2": {Name: "dc2"},
		},
	}

	mdm, err := NewMultiDCManager(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mdm.GetStatus()
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mdm.GetDatacenterInfo("dc2")
		}()
	}

	wg.Wait()

	// No panic means thread safety is working
}
