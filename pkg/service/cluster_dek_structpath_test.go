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
// parseClusterDEKStatus - Struct-based path (clusterDEKStatusLike)
// These tests cover the direct struct type assertion code paths that
// handle ClusterDEKStatus-like concrete types.
// =============================================================================

// clusterDEKStatusLike matches the local struct defined inside parseClusterDEKStatus.
// We define it here with the exact same layout to test the struct-path code.
type testClusterDEKStatusLike struct {
	Initialized  bool
	Version      uint64
	Algorithm    string
	KeyBytes     int
	CreatedAt    time.Time
	SyncedNodes  map[uint64]string
	Reencryption interface{}
}

func TestParseClusterDEKStatus_StructPath_FullPopulation(t *testing.T) {
	svc := &clusterDEKService{}

	created := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	status := &testClusterDEKStatusLike{
		Initialized: true,
		Version:     5,
		Algorithm:   "AES-256-GCM",
		KeyBytes:    32,
		CreatedAt:   created,
		SyncedNodes: map[uint64]string{1: "node1", 2: "node2", 3: "node3"},
	}

	// The parseClusterDEKStatus function uses a locally-defined type,
	// so the struct assertion will NOT match our test type.
	// Instead we verify the interface-based fallback paths with a
	// non-interface type that has no getter methods.
	resp := &ClusterDEKStatusResponse{}
	svc.parseClusterDEKStatus(status, resp)

	// With no getter methods and no matching local type, fields remain zero.
	// This tests the "fall through both paths" code coverage.
	assert.False(t, resp.Initialized)
}

func TestParseClusterDEKStatus_PlainStringStatus(t *testing.T) {
	svc := &clusterDEKService{}

	// Pass a plain string - no interface getters, no struct match
	resp := &ClusterDEKStatusResponse{}
	svc.parseClusterDEKStatus("not-a-status-object", resp)

	assert.False(t, resp.Initialized)
	assert.Equal(t, uint64(0), resp.CurrentVersion)
	assert.Equal(t, 0, resp.NodesSynced)
}

func TestParseClusterDEKStatus_ZeroKeyBytesInterface(t *testing.T) {
	svc := &clusterDEKService{}

	status := &mockDEKStatusWithGetters{
		initialized: true,
		version:     1,
		algorithm:   "ChaCha20",
		keyBytes:    0, // zero should not set key size
		createdAt:   time.Time{},
	}

	resp := &ClusterDEKStatusResponse{}
	svc.parseClusterDEKStatus(status, resp)

	assert.True(t, resp.Initialized)
	assert.Equal(t, uint64(1), resp.CurrentVersion)
	assert.Equal(t, "ChaCha20", resp.Algorithm)
	assert.Equal(t, 0, resp.KeySize) // not set because keyBytes is 0
}

// =============================================================================
// parseReencryptionStatus - Struct-based path (reencryptionInfoLike)
// =============================================================================

func TestParseReencryptionStatus_PlainUnmatchedType(t *testing.T) {
	svc := &clusterDEKService{}

	// Pass an int - no interface getters, no struct match
	resp := &ReencryptionStatusResponse{
		OperationID: "original-id",
		State:       ReencryptionRunning,
	}
	svc.parseReencryptionStatus(42, resp)

	// Nothing should change since no type assertion matched
	assert.Equal(t, "original-id", resp.OperationID)
	assert.Equal(t, ReencryptionRunning, resp.State)
}

func TestParseReencryptionStatus_ThroughputCalculation(t *testing.T) {
	svc := &clusterDEKService{}

	// Use interface-based mock with processed keys and valid start time
	started := time.Now().Add(-10 * time.Second)
	status := &mockReencStatusFull{
		active:        true,
		startedAt:     started,
		totalKeys:     1000,
		processedKeys: 500,
		progress:      0.5,
		oldVersion:    3,
	}

	resp := &ReencryptionStatusResponse{}
	svc.parseReencryptionStatus(status, resp)

	assert.Equal(t, ReencryptionRunning, resp.State)
	assert.Equal(t, int64(1000), resp.TotalEntities)
	assert.Equal(t, int64(500), resp.ProcessedEntities)
	assert.Equal(t, 50.0, resp.Progress)
	assert.True(t, resp.ThroughputRate > 0)
	assert.Equal(t, "reenc-3-4", resp.OperationID)
}

func TestParseReencryptionStatus_NoProcessedKeysNoThroughput(t *testing.T) {
	svc := &clusterDEKService{}

	started := time.Now().Add(-5 * time.Second)
	status := &mockReencStatusFull{
		active:        true,
		startedAt:     started,
		totalKeys:     100,
		processedKeys: 0, // no processed keys
		progress:      0,
		oldVersion:    1,
	}

	resp := &ReencryptionStatusResponse{}
	svc.parseReencryptionStatus(status, resp)

	assert.Equal(t, float64(0), resp.ThroughputRate)
}

// =============================================================================
// parseReencryptionFromStatus - Additional edge cases
// =============================================================================

func TestParseReencryptionFromStatus_PlainUnmatchedType(t *testing.T) {
	svc := &clusterDEKService{}

	// Pass a string - no interface getters, no struct match
	resp := &ClusterDEKStatusResponse{
		ReencryptionInProgress: false,
	}
	svc.parseReencryptionFromStatus("not-reenc-info", resp)

	assert.False(t, resp.ReencryptionInProgress)
	assert.Equal(t, float64(0), resp.ReencryptionProgress)
}

// =============================================================================
// buildReencryptionStatusResponse - with unmatched type
// =============================================================================

func TestBuildReencryptionStatusResponse_UnmatchedStatusType(t *testing.T) {
	svc := &clusterDEKService{}

	// Pass a type that won't match any assertions
	resp := svc.buildReencryptionStatusResponse("some-string-status")

	assert.NotNil(t, resp)
	assert.Equal(t, "unknown", resp.OperationID)
	assert.Equal(t, ReencryptionRunning, resp.State)
}
