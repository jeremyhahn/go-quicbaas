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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// parseClusterDEKStatus Deep Path Tests
// =============================================================================

// mockDEKStatusWithGetters implements the interface-based accessors used
// by parseClusterDEKStatus.
type mockDEKStatusWithGetters struct {
	initialized  bool
	version      uint64
	algorithm    string
	keyBytes     int
	createdAt    time.Time
	reencryption interface{}
	syncedNodes  map[uint64]string
}

func (m *mockDEKStatusWithGetters) GetInitialized() bool              { return m.initialized }
func (m *mockDEKStatusWithGetters) GetVersion() uint64                { return m.version }
func (m *mockDEKStatusWithGetters) GetAlgorithm() string              { return m.algorithm }
func (m *mockDEKStatusWithGetters) GetKeyBytes() int                  { return m.keyBytes }
func (m *mockDEKStatusWithGetters) GetCreatedAt() time.Time           { return m.createdAt }
func (m *mockDEKStatusWithGetters) GetReencryption() interface{}      { return m.reencryption }
func (m *mockDEKStatusWithGetters) GetSyncedNodes() map[uint64]string { return m.syncedNodes }

func TestParseClusterDEKStatus_InterfaceGetters(t *testing.T) {
	svc := &clusterDEKService{}

	created := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	status := &mockDEKStatusWithGetters{
		initialized: true,
		version:     3,
		algorithm:   "AES-256-GCM",
		keyBytes:    32,
		createdAt:   created,
		syncedNodes: map[uint64]string{1: "node1", 2: "node2"},
	}

	resp := &ClusterDEKStatusResponse{}
	svc.parseClusterDEKStatus(status, resp)

	assert.True(t, resp.Initialized)
	assert.Equal(t, uint64(3), resp.CurrentVersion)
	assert.Equal(t, "AES-256-GCM", resp.Algorithm)
	assert.Equal(t, 256, resp.KeySize) // 32 bytes * 8 = 256 bits
	assert.Equal(t, created, resp.CreatedAt)
}

func TestParseClusterDEKStatus_EmptyAlgorithm(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockDEKStatusWithGetters{
		initialized: true,
		version:     1,
		algorithm:   "", // empty algorithm should not overwrite
		keyBytes:    0,  // zero keyBytes should not overwrite
	}

	resp := &ClusterDEKStatusResponse{
		Algorithm: "existing-algo",
		KeySize:   128,
	}
	svc.parseClusterDEKStatus(status, resp)

	assert.True(t, resp.Initialized)
	assert.Equal(t, uint64(1), resp.CurrentVersion)
	assert.Equal(t, "existing-algo", resp.Algorithm) // should not be overwritten
	assert.Equal(t, 128, resp.KeySize)               // should not be overwritten
}

func TestParseClusterDEKStatus_ZeroCreatedAt(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockDEKStatusWithGetters{
		initialized: false,
		createdAt:   time.Time{}, // zero value
	}

	existingTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	resp := &ClusterDEKStatusResponse{
		CreatedAt: existingTime,
	}
	svc.parseClusterDEKStatus(status, resp)

	assert.Equal(t, existingTime, resp.CreatedAt) // should not be overwritten
}

func TestParseClusterDEKStatus_WithReencryption(t *testing.T) {
	svc := &clusterDEKService{}

	reenc := &mockReencryptionWithGetters{
		active:   true,
		progress: 0.75,
	}
	status := &mockDEKStatusWithGetters{
		initialized:  true,
		version:      2,
		reencryption: reenc,
	}

	resp := &ClusterDEKStatusResponse{}
	svc.parseClusterDEKStatus(status, resp)

	assert.True(t, resp.ReencryptionInProgress)
	assert.Equal(t, 75.0, resp.ReencryptionProgress) // 0.75 * 100
}

func TestParseClusterDEKStatus_NilReencryption(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockDEKStatusWithGetters{
		initialized:  true,
		version:      1,
		reencryption: nil,
	}

	resp := &ClusterDEKStatusResponse{}
	svc.parseClusterDEKStatus(status, resp)

	assert.False(t, resp.ReencryptionInProgress)
	assert.Equal(t, 0.0, resp.ReencryptionProgress)
}

// =============================================================================
// parseReencryptionFromStatus Tests
// =============================================================================

type mockReencryptionWithGetters struct {
	active   bool
	progress float64
}

func (m *mockReencryptionWithGetters) GetActive() bool      { return m.active }
func (m *mockReencryptionWithGetters) GetProgress() float64 { return m.progress }

func TestParseReencryptionFromStatus_InterfaceGetters(t *testing.T) {
	svc := &clusterDEKService{}

	reenc := &mockReencryptionWithGetters{
		active:   true,
		progress: 0.5,
	}

	resp := &ClusterDEKStatusResponse{}
	svc.parseReencryptionFromStatus(reenc, resp)

	assert.True(t, resp.ReencryptionInProgress)
	assert.Equal(t, 50.0, resp.ReencryptionProgress) // 0.5 * 100
}

func TestParseReencryptionFromStatus_Nil(t *testing.T) {
	svc := &clusterDEKService{}

	resp := &ClusterDEKStatusResponse{
		ReencryptionInProgress: true, // pre-set
		ReencryptionProgress:   99.0,
	}
	svc.parseReencryptionFromStatus(nil, resp)

	// Should not change resp when nil
	assert.True(t, resp.ReencryptionInProgress)
	assert.Equal(t, 99.0, resp.ReencryptionProgress)
}

func TestParseReencryptionFromStatus_InactiveReencryption(t *testing.T) {
	svc := &clusterDEKService{}

	reenc := &mockReencryptionWithGetters{
		active:   false,
		progress: 1.0,
	}

	resp := &ClusterDEKStatusResponse{}
	svc.parseReencryptionFromStatus(reenc, resp)

	assert.False(t, resp.ReencryptionInProgress)
	assert.Equal(t, 100.0, resp.ReencryptionProgress) // 1.0 * 100
}

// =============================================================================
// parseReencryptionStatus Deep Path Tests
// =============================================================================

type mockReencStatusFull struct {
	active        bool
	startedAt     time.Time
	totalKeys     uint64
	processedKeys uint64
	progress      float64
	oldVersion    uint64
}

func (m *mockReencStatusFull) GetActive() bool          { return m.active }
func (m *mockReencStatusFull) GetStartedAt() time.Time  { return m.startedAt }
func (m *mockReencStatusFull) GetTotalKeys() uint64     { return m.totalKeys }
func (m *mockReencStatusFull) GetProcessedKeys() uint64 { return m.processedKeys }
func (m *mockReencStatusFull) GetProgress() float64     { return m.progress }
func (m *mockReencStatusFull) GetOldVersion() uint64    { return m.oldVersion }

func TestParseReencryptionStatus_AllInterfaceGetters(t *testing.T) {
	svc := &clusterDEKService{}

	started := time.Now().Add(-5 * time.Minute)
	status := &mockReencStatusFull{
		active:        true,
		startedAt:     started,
		totalKeys:     1000,
		processedKeys: 500,
		progress:      0.5,
		oldVersion:    2,
	}

	resp := &ReencryptionStatusResponse{}
	svc.parseReencryptionStatus(status, resp)

	assert.Equal(t, ReencryptionRunning, resp.State)
	assert.Equal(t, started, resp.StartedAt)
	assert.Equal(t, int64(1000), resp.TotalEntities)
	assert.Equal(t, int64(500), resp.ProcessedEntities)
	assert.Equal(t, 50.0, resp.Progress)
	assert.Equal(t, "reenc-2-3", resp.OperationID)
	assert.True(t, resp.ThroughputRate > 0) // calculated from elapsed time
}

func TestParseReencryptionStatus_Completed(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockReencStatusFull{
		active:        false,
		startedAt:     time.Now().Add(-10 * time.Minute),
		totalKeys:     100,
		processedKeys: 100,
		progress:      1.0,
		oldVersion:    1,
	}

	resp := &ReencryptionStatusResponse{}
	svc.parseReencryptionStatus(status, resp)

	assert.Equal(t, ReencryptionCompleted, resp.State)
	assert.Equal(t, 100.0, resp.Progress)
	assert.Equal(t, int64(100), resp.TotalEntities)
	assert.Equal(t, int64(100), resp.ProcessedEntities)
}

func TestParseReencryptionStatus_ZeroStartedAt(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockReencStatusFull{
		active:    true,
		startedAt: time.Time{}, // zero value
	}

	existingStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	resp := &ReencryptionStatusResponse{
		StartedAt: existingStart,
	}
	svc.parseReencryptionStatus(status, resp)

	assert.Equal(t, existingStart, resp.StartedAt) // should not be overwritten
}

func TestParseReencryptionStatus_ZeroOldVersion(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockReencStatusFull{
		active:     true,
		oldVersion: 0, // zero should not generate operation ID
	}

	resp := &ReencryptionStatusResponse{
		OperationID: "existing-id",
	}
	svc.parseReencryptionStatus(status, resp)

	assert.Equal(t, "existing-id", resp.OperationID) // should not be overwritten
}

// =============================================================================
// buildReencryptionStatusResponse Tests
// =============================================================================

func TestBuildReencryptionStatusResponse_NilStatus(t *testing.T) {
	svc := &clusterDEKService{}

	resp := svc.buildReencryptionStatusResponse(nil)

	assert.NotNil(t, resp)
	assert.Equal(t, "unknown", resp.OperationID)
	assert.Equal(t, ReencryptionRunning, resp.State)
	assert.NotZero(t, resp.StartedAt)
	assert.Empty(t, resp.NodeProgress)
	assert.Empty(t, resp.Errors)
}

func TestBuildReencryptionStatusResponse_WithStatus(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockReencStatusFull{
		active:        true,
		startedAt:     time.Now().Add(-1 * time.Minute),
		totalKeys:     50,
		processedKeys: 25,
		progress:      0.5,
		oldVersion:    3,
	}

	resp := svc.buildReencryptionStatusResponse(status)

	assert.NotNil(t, resp)
	assert.Equal(t, ReencryptionRunning, resp.State)
	assert.Equal(t, int64(50), resp.TotalEntities)
	assert.Equal(t, int64(25), resp.ProcessedEntities)
}

// =============================================================================
// countSyncedNodes Tests
// =============================================================================

func TestCountSyncedNodes_NilStatus(t *testing.T) {
	svc := &clusterDEKService{}
	count := svc.countSyncedNodes(nil)
	assert.Equal(t, 0, count)
}

func TestCountSyncedNodes_WithSyncedNodesGetter(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockDEKStatusWithGetters{
		syncedNodes: map[uint64]string{1: "node1", 2: "node2", 3: "node3"},
	}

	count := svc.countSyncedNodes(status)
	assert.Equal(t, 3, count)
}

func TestCountSyncedNodes_EmptySyncedNodes(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockDEKStatusWithGetters{
		syncedNodes: map[uint64]string{},
	}

	count := svc.countSyncedNodes(status)
	assert.Equal(t, 0, count)
}
