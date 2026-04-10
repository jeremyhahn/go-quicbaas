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

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// getNodeLastSyncTime - map[string]interface{} path
// =============================================================================

func TestGetNodeLastSyncTime_MapBasedStatus_ValidTime(t *testing.T) {
	syncTime := time.Now().Add(-5 * time.Minute)
	mgr := &mockDEKManagerForGetNodeLastSyncTime{
		getStatusFn: func(ctx context.Context) (interface{}, error) {
			return map[string]interface{}{
				"synced_nodes": map[uint64]string{
					42: syncTime.Format(time.RFC3339),
				},
			}, nil
		},
	}
	s := &clusterDEKService{dekManager: mgr}

	result := s.getNodeLastSyncTime(context.Background(), 42)
	require.False(t, result.IsZero())
	assert.WithinDuration(t, syncTime, result, time.Second)
}

func TestGetNodeLastSyncTime_MapBasedStatus_InvalidTimeFormat(t *testing.T) {
	mgr := &mockDEKManagerForGetNodeLastSyncTime{
		getStatusFn: func(ctx context.Context) (interface{}, error) {
			return map[string]interface{}{
				"synced_nodes": map[uint64]string{
					10: "not-a-valid-time",
				},
			}, nil
		},
		isNodeSyncedFn: func(nodeID uint64) bool {
			return false
		},
	}
	s := &clusterDEKService{dekManager: mgr}

	result := s.getNodeLastSyncTime(context.Background(), 10)
	assert.True(t, result.IsZero(), "expected zero time for invalid time format and unsynced node")
}

func TestGetNodeLastSyncTime_MapBasedStatus_NodeNotInMap(t *testing.T) {
	mgr := &mockDEKManagerForGetNodeLastSyncTime{
		getStatusFn: func(ctx context.Context) (interface{}, error) {
			return map[string]interface{}{
				"synced_nodes": map[uint64]string{
					1: time.Now().Format(time.RFC3339),
				},
			}, nil
		},
		isNodeSyncedFn: func(nodeID uint64) bool {
			return nodeID == 99
		},
	}
	s := &clusterDEKService{dekManager: mgr}

	// Node 99 is not in map but IsNodeSynced returns true
	result := s.getNodeLastSyncTime(context.Background(), 99)
	assert.WithinDuration(t, time.Now(), result, time.Second)
}

func TestGetNodeLastSyncTime_MapBasedStatus_InvalidMapType(t *testing.T) {
	mgr := &mockDEKManagerForGetNodeLastSyncTime{
		getStatusFn: func(ctx context.Context) (interface{}, error) {
			return map[string]interface{}{
				"synced_nodes": "not-a-map", // Wrong type
			}, nil
		},
		isNodeSyncedFn: func(nodeID uint64) bool {
			return false
		},
	}
	s := &clusterDEKService{dekManager: mgr}

	result := s.getNodeLastSyncTime(context.Background(), 1)
	assert.True(t, result.IsZero())
}

// =============================================================================
// getNodeLastSyncTime - SyncedNodes method path - invalid time
// =============================================================================

func TestGetNodeLastSyncTime_SyncedNodesMethod_InvalidTimeFormat(t *testing.T) {
	mgr := &mockDEKManagerForGetNodeLastSyncTime{
		getStatusFn: func(ctx context.Context) (interface{}, error) {
			return &mockStatusWithSyncedNodesMethod{
				syncedNodes: map[uint64]string{
					5: "bad-time-string",
				},
			}, nil
		},
		isNodeSyncedFn: func(nodeID uint64) bool {
			return true
		},
	}
	s := &clusterDEKService{dekManager: mgr}

	// Time parse fails but node is synced, so returns current time
	result := s.getNodeLastSyncTime(context.Background(), 5)
	assert.WithinDuration(t, time.Now(), result, time.Second)
}

func TestGetNodeLastSyncTime_SyncedNodesMethod_NodeNotInMap(t *testing.T) {
	mgr := &mockDEKManagerForGetNodeLastSyncTime{
		getStatusFn: func(ctx context.Context) (interface{}, error) {
			return &mockStatusWithSyncedNodesMethod{
				syncedNodes: map[uint64]string{}, // Empty map
			}, nil
		},
		isNodeSyncedFn: func(nodeID uint64) bool {
			return false
		},
	}
	s := &clusterDEKService{dekManager: mgr}

	result := s.getNodeLastSyncTime(context.Background(), 7)
	assert.True(t, result.IsZero())
}

// =============================================================================
// buildStatusResponse - with non-nil status (interface getters path)
// =============================================================================

func TestBuildStatusResponse_WithInterfaceGetterStatus(t *testing.T) {
	createdAt := time.Now().Add(-time.Hour)
	// Need a manager that returns a version for buildStatusResponse
	s := &clusterDEKService{dekManager: &mockDEKManagerVersioned{version: 3}}

	status := &mockClusterDEKStatusLike{
		initialized: true,
		version:     3,
		algorithm:   "ChaCha20-Poly1305",
		keyBytes:    32,
		createdAt:   createdAt,
		syncedNodes: map[uint64]string{1: "node1", 2: "node2"},
	}

	resp := s.buildStatusResponse(status)
	require.NotNil(t, resp)
	assert.True(t, resp.Initialized)
	assert.Equal(t, uint64(3), resp.CurrentVersion)
	assert.Equal(t, "ChaCha20-Poly1305", resp.Algorithm)
	assert.Equal(t, 256, resp.KeySize)
	assert.Equal(t, 2, resp.NodesSynced)
}

// mockDEKManagerVersioned is a minimal mock that returns a specific version.
type mockDEKManagerVersioned struct {
	version uint64
}

func (m *mockDEKManagerVersioned) Initialize(ctx context.Context, algorithm string, keyBytes int) error {
	return nil
}
func (m *mockDEKManagerVersioned) GetStatus(ctx context.Context) (interface{}, error) {
	return nil, nil
}
func (m *mockDEKManagerVersioned) Rotate(ctx context.Context) error                    { return nil }
func (m *mockDEKManagerVersioned) GetCurrentDEK() ([]byte, error)                      { return nil, nil }
func (m *mockDEKManagerVersioned) GetDEKVersion() uint64                               { return m.version }
func (m *mockDEKManagerVersioned) SyncToNode(ctx context.Context, nodeID uint64) error { return nil }
func (m *mockDEKManagerVersioned) IsNodeSynced(nodeID uint64) bool                     { return false }
func (m *mockDEKManagerVersioned) StartReencryption(ctx context.Context) error         { return nil }
func (m *mockDEKManagerVersioned) GetReencryptionStatus(ctx context.Context) (interface{}, error) {
	return nil, nil
}
func (m *mockDEKManagerVersioned) CancelReencryption(ctx context.Context) error { return nil }
func (m *mockDEKManagerVersioned) RecoverDEK(ctx context.Context, recoveryData interface{}) error {
	return nil
}
func (m *mockDEKManagerVersioned) Close() error { return nil }

// =============================================================================
// buildStatusResponse - with nil status
// =============================================================================

func TestBuildStatusResponse_NilStatus(t *testing.T) {
	s := &clusterDEKService{dekManager: &mockDEKManagerVersioned{version: 0}}

	resp := s.buildStatusResponse(nil)
	require.NotNil(t, resp)
	assert.False(t, resp.Initialized)
	assert.Equal(t, uint64(0), resp.CurrentVersion)
	assert.Equal(t, "AES-256-GCM", resp.Algorithm)
	assert.Equal(t, 256, resp.KeySize)
	assert.Equal(t, 0, resp.NodesSynced)
}

func TestBuildStatusResponse_WithInitializedVersion(t *testing.T) {
	s := &clusterDEKService{dekManager: &mockDEKManagerVersioned{version: 5}}

	resp := s.buildStatusResponse(nil)
	require.NotNil(t, resp)
	assert.True(t, resp.Initialized) // version > 0 means initialized
	assert.Equal(t, uint64(5), resp.CurrentVersion)
}

// =============================================================================
// countSyncedNodes - map path: synced_nodes key missing, SyncedNodes key present
// =============================================================================

func TestCountSyncedNodes_MapStatus_SyncedNodesKey_NotMap(t *testing.T) {
	s := &clusterDEKService{}

	// Map has "synced_nodes" but wrong type
	status := map[string]interface{}{
		"synced_nodes": 42,
	}
	result := s.countSyncedNodes(status)
	assert.Equal(t, 0, result)
}

func TestCountSyncedNodes_MapStatus_BothKeysMissing(t *testing.T) {
	s := &clusterDEKService{}

	status := map[string]interface{}{
		"other_field": "value",
	}
	result := s.countSyncedNodes(status)
	assert.Equal(t, 0, result)
}

func TestCountSyncedNodes_MapStatus_SyncedNodesCapitalKey_NotMap(t *testing.T) {
	s := &clusterDEKService{}

	// Has SyncedNodes key but wrong type
	status := map[string]interface{}{
		"SyncedNodes": "not-a-map",
	}
	result := s.countSyncedNodes(status)
	assert.Equal(t, 0, result)
}

// =============================================================================
// parseClusterDEKStatus - edge: GetReencryption returns non-nil but
// non-matching type (exercises the parseReencryptionFromStatus fallthrough)
// =============================================================================

type mockDEKStatusWithUnmatchedReenc struct {
	initialized  bool
	version      uint64
	algorithm    string
	keyBytes     int
	createdAt    time.Time
	reencryption interface{}
	syncedNodes  map[uint64]string
}

func (m *mockDEKStatusWithUnmatchedReenc) GetInitialized() bool              { return m.initialized }
func (m *mockDEKStatusWithUnmatchedReenc) GetVersion() uint64                { return m.version }
func (m *mockDEKStatusWithUnmatchedReenc) GetAlgorithm() string              { return m.algorithm }
func (m *mockDEKStatusWithUnmatchedReenc) GetKeyBytes() int                  { return m.keyBytes }
func (m *mockDEKStatusWithUnmatchedReenc) GetCreatedAt() time.Time           { return m.createdAt }
func (m *mockDEKStatusWithUnmatchedReenc) GetReencryption() interface{}      { return m.reencryption }
func (m *mockDEKStatusWithUnmatchedReenc) GetSyncedNodes() map[uint64]string { return m.syncedNodes }

func TestParseClusterDEKStatus_ReencryptionWithUnmatchedType(t *testing.T) {
	s := &clusterDEKService{}

	// Reencryption is non-nil but doesn't implement GetActive/GetProgress
	status := &mockDEKStatusWithUnmatchedReenc{
		initialized:  true,
		version:      2,
		reencryption: "just-a-string", // non-nil, but won't match any type assertion
	}

	resp := &ClusterDEKStatusResponse{}
	s.parseClusterDEKStatus(status, resp)

	assert.True(t, resp.Initialized)
	assert.Equal(t, uint64(2), resp.CurrentVersion)
	assert.False(t, resp.ReencryptionInProgress) // No matching reenc type
}

// =============================================================================
// extractReencryptionInfo - map-based status: missing total_keys key
// =============================================================================

func TestExtractReencryptionInfo_MapStatus_MissingTotalKeysKey(t *testing.T) {
	s := &clusterDEKService{}

	status := map[string]interface{}{
		"other_key": "value",
	}
	req := &StartReencryptionRequest{BatchSize: 100, Concurrency: 4}

	total, estimated := s.extractReencryptionInfo(status, req)
	assert.Equal(t, int64(0), total)
	assert.False(t, estimated.IsZero())
}

func TestExtractReencryptionInfo_MapStatus_TotalKeysWrongType(t *testing.T) {
	s := &clusterDEKService{}

	status := map[string]interface{}{
		"total_keys": "not-a-uint64",
	}
	req := &StartReencryptionRequest{BatchSize: 100, Concurrency: 4}

	total, _ := s.extractReencryptionInfo(status, req)
	assert.Equal(t, int64(0), total)
}

// =============================================================================
// extractReencryptionInfo - interface with processedKeys and startedAt
// =============================================================================

func TestExtractReencryptionInfo_InterfaceStatus_WithProcessedKeys(t *testing.T) {
	s := &clusterDEKService{}

	startTime := time.Now().Add(-30 * time.Second)
	status := &mockReencryptionInfoWithGetters{
		totalKeys:     2000,
		processedKeys: 1000,
		startedAt:     startTime,
	}
	req := &StartReencryptionRequest{BatchSize: 100, Concurrency: 4}

	total, estimated := s.extractReencryptionInfo(status, req)
	assert.Equal(t, int64(2000), total)
	// With 1000 processed in 30s, remaining 1000 should take ~30s more
	assert.True(t, estimated.After(time.Now()))
}
