//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock ClusterDEKService Implementation
// =============================================================================

// clusterDEKMockSvc is a mock implementation of service.ClusterDEKService.
type clusterDEKMockSvc struct {
	mock.Mock
}

func (m *clusterDEKMockSvc) InitializeClusterDEK(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.InitClusterDEKResponse), args.Error(1)
}

func (m *clusterDEKMockSvc) GetClusterDEKStatus(ctx context.Context) (*service.ClusterDEKStatusResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ClusterDEKStatusResponse), args.Error(1)
}

func (m *clusterDEKMockSvc) RotateClusterDEK(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RotateClusterDEKResponse), args.Error(1)
}

func (m *clusterDEKMockSvc) SyncNodeDEK(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SyncNodeDEKResponse), args.Error(1)
}

func (m *clusterDEKMockSvc) GetNodeDEKStatus(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetNodeDEKStatusResponse), args.Error(1)
}

func (m *clusterDEKMockSvc) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.StartReencryptionResponse), args.Error(1)
}

func (m *clusterDEKMockSvc) GetReencryptionStatus(ctx context.Context) (*service.ReencryptionStatusResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ReencryptionStatusResponse), args.Error(1)
}

func (m *clusterDEKMockSvc) CancelReencryption(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *clusterDEKMockSvc) RecoverDEK(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RecoverDEKResponse), args.Error(1)
}

// =============================================================================
// testQuicBaaSWithClusterDEK wraps mockService and provides ClusterDEKService
// =============================================================================

type testQuicBaaSWithClusterDEK struct {
	*mockService
	clusterDEKSvc service.ClusterDEKService
}

func (m *testQuicBaaSWithClusterDEK) ClusterDEK() service.ClusterDEKService {
	return m.clusterDEKSvc
}

func newTestQuicBaaSWithClusterDEK(clusterDEKSvc service.ClusterDEKService) *testQuicBaaSWithClusterDEK {
	return &testQuicBaaSWithClusterDEK{
		mockService:   &mockService{},
		clusterDEKSvc: clusterDEKSvc,
	}
}

// =============================================================================
// Test Helper Functions
// =============================================================================

func createClusterDEKTestLogger(t *testing.T) *logger.Logger {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-cluster-dek-test",
	})
	require.NoError(t, err)
	return log
}

// =============================================================================
// Tool Definition Tests
// =============================================================================

func TestGetAllClusterDEKTools(t *testing.T) {
	tools := getAllClusterDEKTools()

	assert.Len(t, tools, 9, "expected 9 cluster DEK tools")

	expectedTools := map[string]bool{
		"quicbaas_cluster_dek_init":                false,
		"quicbaas_cluster_dek_status":              false,
		"quicbaas_cluster_dek_rotate":              false,
		"quicbaas_cluster_dek_sync_node":           false,
		"quicbaas_cluster_dek_node_status":         false,
		"quicbaas_cluster_dek_start_reencryption":  false,
		"quicbaas_cluster_dek_reencryption_status": false,
		"quicbaas_cluster_dek_cancel_reencryption": false,
		"quicbaas_cluster_dek_recover":             false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool name: %s", tool.Name)
		}
		expectedTools[tool.Name] = true
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestIsClusterDEKTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"init tool", "quicbaas_cluster_dek_init", true},
		{"status tool", "quicbaas_cluster_dek_status", true},
		{"rotate tool", "quicbaas_cluster_dek_rotate", true},
		{"sync node tool", "quicbaas_cluster_dek_sync_node", true},
		{"node status tool", "quicbaas_cluster_dek_node_status", true},
		{"start reencryption tool", "quicbaas_cluster_dek_start_reencryption", true},
		{"reencryption status tool", "quicbaas_cluster_dek_reencryption_status", true},
		{"cancel reencryption tool", "quicbaas_cluster_dek_cancel_reencryption", true},
		{"recover tool", "quicbaas_cluster_dek_recover", true},
		{"unknown tool", "quicbaas_unknown", false},
		{"kms tool", "quicbaas_kms_generate_key", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isClusterDEKTool(tt.toolName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Tool Schema Validation Tests
// =============================================================================

func TestClusterDEKInitTool_Schema(t *testing.T) {
	tool := ClusterDEKInitTool

	assert.Equal(t, "quicbaas_cluster_dek_init", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Contains(t, tool.InputSchema.Required, "algorithm")
	assert.Contains(t, tool.InputSchema.Required, "key_size")

	props := tool.InputSchema.Properties
	assert.Contains(t, props, "algorithm")
	assert.Contains(t, props, "key_size")
	assert.Contains(t, props, "rotation_enabled")
	assert.Contains(t, props, "rotation_interval")
	assert.Contains(t, props, "auto_reencrypt")
}

func TestClusterDEKStatusTool_Schema(t *testing.T) {
	tool := ClusterDEKStatusTool

	assert.Equal(t, "quicbaas_cluster_dek_status", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Empty(t, tool.InputSchema.Required)
}

func TestClusterDEKRotateTool_Schema(t *testing.T) {
	tool := ClusterDEKRotateTool

	assert.Equal(t, "quicbaas_cluster_dek_rotate", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Contains(t, tool.InputSchema.Required, "reason")

	props := tool.InputSchema.Properties
	assert.Contains(t, props, "reason")
	assert.Contains(t, props, "scheduled_time")
	assert.Contains(t, props, "force_rotation")
	assert.Contains(t, props, "auto_start_reencryption")
}

func TestClusterDEKSyncNodeTool_Schema(t *testing.T) {
	tool := ClusterDEKSyncNodeTool

	assert.Equal(t, "quicbaas_cluster_dek_sync_node", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Contains(t, tool.InputSchema.Required, "node_id")

	props := tool.InputSchema.Properties
	assert.Contains(t, props, "node_id")
	assert.Contains(t, props, "current_version")
	assert.Contains(t, props, "force_resync")
}

func TestClusterDEKNodeStatusTool_Schema(t *testing.T) {
	tool := ClusterDEKNodeStatusTool

	assert.Equal(t, "quicbaas_cluster_dek_node_status", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Contains(t, tool.InputSchema.Required, "node_id")
}

func TestClusterDEKStartReencryptionTool_Schema(t *testing.T) {
	tool := ClusterDEKStartReencryptionTool

	assert.Equal(t, "quicbaas_cluster_dek_start_reencryption", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Contains(t, tool.InputSchema.Required, "from_version")
	assert.Contains(t, tool.InputSchema.Required, "to_version")

	props := tool.InputSchema.Properties
	assert.Contains(t, props, "path")
	assert.Contains(t, props, "from_version")
	assert.Contains(t, props, "to_version")
	assert.Contains(t, props, "batch_size")
	assert.Contains(t, props, "concurrency")
	assert.Contains(t, props, "throttle_rate")
	assert.Contains(t, props, "force")
}

func TestClusterDEKReencryptionStatusTool_Schema(t *testing.T) {
	tool := ClusterDEKReencryptionStatusTool

	assert.Equal(t, "quicbaas_cluster_dek_reencryption_status", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Empty(t, tool.InputSchema.Required)
}

func TestClusterDEKCancelReencryptionTool_Schema(t *testing.T) {
	tool := ClusterDEKCancelReencryptionTool

	assert.Equal(t, "quicbaas_cluster_dek_cancel_reencryption", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Empty(t, tool.InputSchema.Required)
}

func TestClusterDEKRecoverTool_Schema(t *testing.T) {
	tool := ClusterDEKRecoverTool

	assert.Equal(t, "quicbaas_cluster_dek_recover", tool.Name)
	assert.Equal(t, "object", tool.InputSchema.Type)
	assert.Contains(t, tool.InputSchema.Required, "recovery_method")
	assert.Contains(t, tool.InputSchema.Required, "reason")

	props := tool.InputSchema.Properties
	assert.Contains(t, props, "recovery_method")
	assert.Contains(t, props, "recovery_keys")
	assert.Contains(t, props, "backup_location")
	assert.Contains(t, props, "manual_key")
	assert.Contains(t, props, "reason")

	// Check enum values for recovery_method
	assert.Contains(t, props["recovery_method"].Enum, "recovery_keys")
	assert.Contains(t, props["recovery_method"].Enum, "backup")
	assert.Contains(t, props["recovery_method"].Enum, "manual")
}

// =============================================================================
// Service Not Configured Tests
// =============================================================================

func TestClusterDEKTools_ServiceNotConfigured(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockSvc := &mockService{}
	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "init - service not configured",
			toolName: "quicbaas_cluster_dek_init",
			args: map[string]interface{}{
				"algorithm": "AES-256-GCM",
				"key_size":  "256",
			},
		},
		{
			name:     "status - service not configured",
			toolName: "quicbaas_cluster_dek_status",
			args:     map[string]interface{}{},
		},
		{
			name:     "rotate - service not configured",
			toolName: "quicbaas_cluster_dek_rotate",
			args: map[string]interface{}{
				"reason": "scheduled rotation",
			},
		},
		{
			name:     "sync node - service not configured",
			toolName: "quicbaas_cluster_dek_sync_node",
			args: map[string]interface{}{
				"node_id": "1",
			},
		},
		{
			name:     "node status - service not configured",
			toolName: "quicbaas_cluster_dek_node_status",
			args: map[string]interface{}{
				"node_id": "1",
			},
		},
		{
			name:     "start reencryption - service not configured",
			toolName: "quicbaas_cluster_dek_start_reencryption",
			args: map[string]interface{}{
				"from_version": "1",
				"to_version":   "2",
			},
		},
		{
			name:     "reencryption status - service not configured",
			toolName: "quicbaas_cluster_dek_reencryption_status",
			args:     map[string]interface{}{},
		},
		{
			name:     "cancel reencryption - service not configured",
			toolName: "quicbaas_cluster_dek_cancel_reencryption",
			args:     map[string]interface{}{},
		},
		{
			name:     "recover - service not configured",
			toolName: "quicbaas_cluster_dek_recover",
			args: map[string]interface{}{
				"recovery_method": "backup",
				"reason":          "disaster recovery",
				"backup_location": "/backups/dek.enc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "cluster DEK service not configured")
		})
	}
}

// =============================================================================
// Success Case Tests
// =============================================================================

func TestClusterDEKInit_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("InitializeClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.InitClusterDEKRequest) bool {
		return req.Algorithm == "AES-256-GCM" && req.KeySize == 256
	})).Return(&service.InitClusterDEKResponse{
		Version:     1,
		CreatedAt:   now,
		NodesSynced: 3,
		Algorithm:   "AES-256-GCM",
		KeySize:     256,
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"algorithm": "AES-256-GCM",
		"key_size":  "256",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_init", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, uint64(1), resultMap["version"])
	assert.Equal(t, 3, resultMap["nodes_synced"])
	assert.Equal(t, "AES-256-GCM", resultMap["algorithm"])
	assert.Equal(t, 256, resultMap["key_size"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKInit_WithRotationPolicy(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("InitializeClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.InitClusterDEKRequest) bool {
		return req.RotationPolicy != nil &&
			req.RotationPolicy.Enabled &&
			req.RotationPolicy.RotationInterval == 720*time.Hour &&
			req.RotationPolicy.AutoReencrypt
	})).Return(&service.InitClusterDEKResponse{
		Version:     1,
		CreatedAt:   now,
		NodesSynced: 3,
		Algorithm:   "AES-256-GCM",
		KeySize:     256,
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"algorithm":         "AES-256-GCM",
		"key_size":          "256",
		"rotation_enabled":  "true",
		"rotation_interval": "720h",
		"auto_reencrypt":    "true",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_init", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKStatus_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	rotatedAt := now.Add(-24 * time.Hour)
	nextRotation := now.Add(720 * time.Hour)

	mockDEKSvc.On("GetClusterDEKStatus", mock.Anything).Return(&service.ClusterDEKStatusResponse{
		Initialized:            true,
		CurrentVersion:         2,
		CreatedAt:              now.Add(-30 * 24 * time.Hour),
		RotatedAt:              &rotatedAt,
		NextRotation:           &nextRotation,
		NodesSynced:            3,
		TotalNodes:             3,
		Algorithm:              "AES-256-GCM",
		KeySize:                256,
		ReencryptionInProgress: false,
		ReencryptionProgress:   0,
		NodesOutOfSync:         nil,
		PreviousVersions: []service.DEKVersionInfo{
			{
				Version:           1,
				CreatedAt:         now.Add(-60 * 24 * time.Hour),
				DeprecatedAt:      &rotatedAt,
				EntitiesEncrypted: 0,
				CanDelete:         true,
			},
		},
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_status", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["initialized"])
	assert.Equal(t, uint64(2), resultMap["current_version"])
	assert.Equal(t, 3, resultMap["nodes_synced"])
	assert.Equal(t, 3, resultMap["total_nodes"])
	assert.Equal(t, "AES-256-GCM", resultMap["algorithm"])
	assert.Equal(t, 256, resultMap["key_size"])
	assert.NotNil(t, resultMap["rotated_at"])
	assert.NotNil(t, resultMap["next_rotation"])
	assert.NotNil(t, resultMap["previous_versions"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKRotate_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("RotateClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateClusterDEKRequest) bool {
		return req.Reason == "scheduled rotation" && req.AutoStartReencryption
	})).Return(&service.RotateClusterDEKResponse{
		NewVersion:                2,
		OldVersion:                1,
		RotatedAt:                 now,
		RotationID:                "rot-1-2",
		NodesSynced:               3,
		ReencryptionStarted:       true,
		EstimatedReencryptionTime: time.Hour,
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"reason":                  "scheduled rotation",
		"auto_start_reencryption": "true",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_rotate", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, uint64(2), resultMap["new_version"])
	assert.Equal(t, uint64(1), resultMap["old_version"])
	assert.Equal(t, "rot-1-2", resultMap["rotation_id"])
	assert.Equal(t, 3, resultMap["nodes_synced"])
	assert.Equal(t, true, resultMap["reencryption_started"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKSyncNode_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("SyncNodeDEK", mock.Anything, mock.MatchedBy(func(req *service.SyncNodeDEKRequest) bool {
		return req.NodeID == 2
	})).Return(&service.SyncNodeDEKResponse{
		NodeID:         2,
		VersionsSynced: []uint64{1, 2},
		CurrentVersion: 2,
		SyncedAt:       now,
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"node_id": "2",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_sync_node", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, uint64(2), resultMap["node_id"])
	assert.Equal(t, uint64(2), resultMap["current_version"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKNodeStatus_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("GetNodeDEKStatus", mock.Anything, mock.MatchedBy(func(req *service.GetNodeDEKStatusRequest) bool {
		return req.NodeID == 1
	})).Return(&service.GetNodeDEKStatusResponse{
		NodeID:            1,
		NodeName:          "node-1",
		CurrentVersion:    2,
		LastSyncedAt:      now,
		Status:            service.NodeDEKSynced,
		VersionsAvailable: []uint64{1, 2},
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"node_id": "1",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_node_status", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, uint64(1), resultMap["node_id"])
	assert.Equal(t, "node-1", resultMap["node_name"])
	assert.Equal(t, uint64(2), resultMap["current_version"])
	assert.Equal(t, string(service.NodeDEKSynced), resultMap["status"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKStartReencryption_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
		return req.FromVersion == 1 && req.ToVersion == 2
	})).Return(&service.StartReencryptionResponse{
		OperationID:         "reenc-1-2-123",
		Started:             true,
		StartedAt:           now,
		TotalEntities:       10000,
		EstimatedCompletion: now.Add(time.Hour),
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"from_version": "1",
		"to_version":   "2",
		"batch_size":   "100",
		"concurrency":  "4",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_start_reencryption", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "reenc-1-2-123", resultMap["operation_id"])
	assert.Equal(t, true, resultMap["started"])
	assert.Equal(t, int64(10000), resultMap["total_entities"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKReencryptionStatus_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("GetReencryptionStatus", mock.Anything).Return(&service.ReencryptionStatusResponse{
		OperationID:       "reenc-1-2-123",
		State:             service.ReencryptionRunning,
		StartedAt:         now.Add(-30 * time.Minute),
		Progress:          50.0,
		TotalEntities:     10000,
		ProcessedEntities: 5000,
		FailedEntities:    5,
		ThroughputRate:    166.67,
		NodeProgress: []service.NodeReencryptionProgress{
			{
				NodeID:            1,
				NodeName:          "node-1",
				Progress:          55.0,
				ProcessedEntities: 1833,
				TotalEntities:     3333,
				ThroughputRate:    60.0,
				LastUpdatedAt:     now,
			},
		},
		Errors: []service.ReencryptionError{
			{
				Timestamp: now.Add(-10 * time.Minute),
				NodeID:    2,
				EntityID:  "entity-123",
				Error:     "temporary network error",
				Retried:   true,
			},
		},
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_reencryption_status", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "reenc-1-2-123", resultMap["operation_id"])
	assert.Equal(t, string(service.ReencryptionRunning), resultMap["state"])
	assert.Equal(t, 50.0, resultMap["progress"])
	assert.Equal(t, int64(10000), resultMap["total_entities"])
	assert.Equal(t, int64(5000), resultMap["processed_entities"])
	assert.Equal(t, int64(5), resultMap["failed_entities"])
	assert.NotNil(t, resultMap["node_progress"])
	assert.NotNil(t, resultMap["errors"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKCancelReencryption_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("CancelReencryption", mock.Anything).Return(nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_cancel_reencryption", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["cancelled"])
	assert.NotNil(t, resultMap["cancelled_at"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKRecover_Success(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
		return req.RecoveryMethod == service.RecoveryMethodBackup &&
			req.BackupLocation == "/backups/dek.enc" &&
			req.Reason == "disaster recovery"
	})).Return(&service.RecoverDEKResponse{
		Success:          true,
		RecoveredVersion: 2,
		RecoveredAt:      now,
		NodesSynced:      3,
		AuditID:          "recovery-2-123",
		Warnings:         nil,
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"recovery_method": "backup",
		"backup_location": "/backups/dek.enc",
		"reason":          "disaster recovery",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["success"])
	assert.Equal(t, uint64(2), resultMap["recovered_version"])
	assert.Equal(t, 3, resultMap["nodes_synced"])
	assert.Equal(t, "recovery-2-123", resultMap["audit_id"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKRecover_WithRecoveryKeys(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
		return req.RecoveryMethod == service.RecoveryMethodKeys &&
			len(req.RecoveryKeys) == 3
	})).Return(&service.RecoverDEKResponse{
		Success:          true,
		RecoveredVersion: 2,
		RecoveredAt:      now,
		NodesSynced:      3,
		AuditID:          "recovery-2-456",
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	// Base64 encoded recovery keys
	args := map[string]interface{}{
		"recovery_method": "recovery_keys",
		"recovery_keys":   `["a2V5MQ==", "a2V5Mg==", "a2V5Mw=="]`,
		"reason":          "scheduled recovery test",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["success"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKRecover_WithManualKey(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
		return req.RecoveryMethod == service.RecoveryMethodManual &&
			len(req.ManualKey) > 0
	})).Return(&service.RecoverDEKResponse{
		Success:          true,
		RecoveredVersion: 2,
		RecoveredAt:      now,
		NodesSynced:      3,
		AuditID:          "recovery-2-789",
		Warnings:         []string{"Manual key injection used - audit required"},
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	// Base64 encoded manual key (32 bytes for AES-256)
	args := map[string]interface{}{
		"recovery_method": "manual",
		"manual_key":      "MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI=",
		"reason":          "emergency break-glass recovery",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["success"])
	assert.NotNil(t, resultMap["warnings"])

	mockDEKSvc.AssertExpectations(t)
}

// =============================================================================
// Error Case Tests
// =============================================================================

func TestClusterDEKInit_MissingAlgorithm(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_size": "256",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_init", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "algorithm is required")
}

func TestClusterDEKInit_MissingKeySize(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"algorithm": "AES-256-GCM",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_init", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "key_size is required")
}

func TestClusterDEKInit_InvalidRotationInterval(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"algorithm":         "AES-256-GCM",
		"key_size":          "256",
		"rotation_enabled":  "true",
		"rotation_interval": "invalid-duration",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_init", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid rotation_interval format")
}

func TestClusterDEKInit_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("InitializeClusterDEK", mock.Anything, mock.Anything).
		Return(nil, errors.New("cluster DEK already initialized"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"algorithm": "AES-256-GCM",
		"key_size":  "256",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_init", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "init cluster DEK failed")

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKRotate_MissingReason(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_rotate", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "reason is required")
}

func TestClusterDEKRotate_InvalidScheduledTime(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"reason":         "scheduled rotation",
		"scheduled_time": "not-a-valid-time",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_rotate", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid scheduled_time format")
}

func TestClusterDEKRotate_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("RotateClusterDEK", mock.Anything, mock.Anything).
		Return(nil, errors.New("rotation already in progress"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"reason": "scheduled rotation",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_rotate", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "rotate cluster DEK failed")

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKSyncNode_MissingNodeID(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_sync_node", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "node_id is required")
}

func TestClusterDEKSyncNode_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("SyncNodeDEK", mock.Anything, mock.Anything).
		Return(nil, errors.New("node not in cluster"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"node_id": "999",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_sync_node", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "sync node DEK failed")

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKNodeStatus_MissingNodeID(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_node_status", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "node_id is required")
}

func TestClusterDEKNodeStatus_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("GetNodeDEKStatus", mock.Anything, mock.Anything).
		Return(nil, errors.New("node not found"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"node_id": "999",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_node_status", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "get node DEK status failed")

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKStartReencryption_MissingFromVersion(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"to_version": "2",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_start_reencryption", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "from_version is required")
}

func TestClusterDEKStartReencryption_MissingToVersion(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"from_version": "1",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_start_reencryption", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "to_version is required")
}

func TestClusterDEKStartReencryption_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("StartReencryption", mock.Anything, mock.Anything).
		Return(nil, errors.New("reencryption already in progress"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"from_version": "1",
		"to_version":   "2",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_start_reencryption", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "start reencryption failed")

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKStatus_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("GetClusterDEKStatus", mock.Anything).
		Return(nil, errors.New("cluster not initialized"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_status", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "get cluster DEK status failed")

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKReencryptionStatus_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("GetReencryptionStatus", mock.Anything).
		Return(nil, errors.New("no active reencryption"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_reencryption_status", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "get reencryption status failed")

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKCancelReencryption_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("CancelReencryption", mock.Anything).
		Return(errors.New("no active reencryption to cancel"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_cancel_reencryption", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "cancel reencryption failed")

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKRecover_MissingRecoveryMethod(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"reason": "disaster recovery",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "recovery_method is required")
}

func TestClusterDEKRecover_MissingReason(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"recovery_method": "backup",
		"backup_location": "/backups/dek.enc",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "reason is required")
}

func TestClusterDEKRecover_InvalidRecoveryKeysJSON(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"recovery_method": "recovery_keys",
		"recovery_keys":   "not-valid-json",
		"reason":          "recovery test",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid recovery_keys JSON")
}

func TestClusterDEKRecover_InvalidBase64RecoveryKey(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"recovery_method": "recovery_keys",
		"recovery_keys":   `["not-valid-base64!!!"]`,
		"reason":          "recovery test",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid base64 recovery key")
}

func TestClusterDEKRecover_InvalidBase64ManualKey(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"recovery_method": "manual",
		"manual_key":      "not-valid-base64!!!",
		"reason":          "emergency recovery",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid base64 manual key")
}

func TestClusterDEKRecover_ServiceError(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	mockDEKSvc.On("RecoverDEK", mock.Anything, mock.Anything).
		Return(nil, errors.New("invalid recovery keys"))

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"recovery_method": "backup",
		"backup_location": "/backups/dek.enc",
		"reason":          "disaster recovery",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_recover", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "recover DEK failed")

	mockDEKSvc.AssertExpectations(t)
}

// =============================================================================
// Unknown Tool Test
// =============================================================================

func TestClusterDEKExecuteTool_UnknownTool(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	// This should not be dispatched to cluster DEK handler since it's not a cluster DEK tool
	// But let's test the executeClusterDEKTool directly
	result, err := registry.executeClusterDEKTool(context.Background(), "quicbaas_cluster_dek_unknown", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool: quicbaas_cluster_dek_unknown")
}

// =============================================================================
// Tool Registration Tests
// =============================================================================

func TestClusterDEKTools_ToolRegistration(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)
	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	tools := registry.ListTools()
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{
		"quicbaas_cluster_dek_init",
		"quicbaas_cluster_dek_status",
		"quicbaas_cluster_dek_rotate",
		"quicbaas_cluster_dek_sync_node",
		"quicbaas_cluster_dek_node_status",
		"quicbaas_cluster_dek_start_reencryption",
		"quicbaas_cluster_dek_reencryption_status",
		"quicbaas_cluster_dek_cancel_reencryption",
		"quicbaas_cluster_dek_recover",
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestClusterDEKNodeStatus_WithErrorMessage(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("GetNodeDEKStatus", mock.Anything, mock.Anything).Return(&service.GetNodeDEKStatusResponse{
		NodeID:            2,
		NodeName:          "node-2",
		CurrentVersion:    1,
		LastSyncedAt:      now.Add(-time.Hour),
		Status:            service.NodeDEKSyncFailed,
		Error:             "network timeout during sync",
		VersionsAvailable: []uint64{1},
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"node_id": "2",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_node_status", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, string(service.NodeDEKSyncFailed), resultMap["status"])
	assert.Equal(t, "network timeout during sync", resultMap["error"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKStatus_WithNodesOutOfSync(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("GetClusterDEKStatus", mock.Anything).Return(&service.ClusterDEKStatusResponse{
		Initialized:    true,
		CurrentVersion: 2,
		CreatedAt:      now.Add(-30 * 24 * time.Hour),
		NodesSynced:    2,
		TotalNodes:     3,
		Algorithm:      "AES-256-GCM",
		KeySize:        256,
		NodesOutOfSync: []service.NodeDEKInfo{
			{
				NodeID:         3,
				NodeName:       "node-3",
				CurrentVersion: 1,
				LastSyncedAt:   now.Add(-24 * time.Hour),
			},
		},
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_status", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 2, resultMap["nodes_synced"])
	assert.Equal(t, 3, resultMap["total_nodes"])
	assert.NotNil(t, resultMap["nodes_out_of_sync"])

	nodesOutOfSync, ok := resultMap["nodes_out_of_sync"].([]map[string]interface{})
	require.True(t, ok)
	assert.Len(t, nodesOutOfSync, 1)
	assert.Equal(t, uint64(3), nodesOutOfSync[0]["node_id"])
	assert.Equal(t, "node-3", nodesOutOfSync[0]["node_name"])

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKRotate_WithScheduledTime(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	scheduledTime := now.Add(24 * time.Hour)

	mockDEKSvc.On("RotateClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateClusterDEKRequest) bool {
		return req.ScheduledTime != nil && !req.ScheduledTime.IsZero()
	})).Return(&service.RotateClusterDEKResponse{
		NewVersion:          2,
		OldVersion:          1,
		RotatedAt:           scheduledTime,
		RotationID:          "rot-1-2-scheduled",
		NodesSynced:         0,
		ReencryptionStarted: false,
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"reason":         "scheduled monthly rotation",
		"scheduled_time": scheduledTime.Format(time.RFC3339),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_rotate", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKSyncNode_WithForceResync(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("SyncNodeDEK", mock.Anything, mock.MatchedBy(func(req *service.SyncNodeDEKRequest) bool {
		return req.NodeID == 2 && req.ForceResync && req.CurrentVersion == 1
	})).Return(&service.SyncNodeDEKResponse{
		NodeID:         2,
		VersionsSynced: []uint64{1, 2},
		CurrentVersion: 2,
		SyncedAt:       now,
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"node_id":         "2",
		"current_version": "1",
		"force_resync":    "true",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_sync_node", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	mockDEKSvc.AssertExpectations(t)
}

func TestClusterDEKStartReencryption_WithAllOptions(t *testing.T) {
	log := createClusterDEKTestLogger(t)
	mockDEKSvc := new(clusterDEKMockSvc)

	now := time.Now()
	mockDEKSvc.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
		return req.FromVersion == 1 &&
			req.ToVersion == 2 &&
			req.Path == "/secrets/" &&
			req.BatchSize == 500 &&
			req.Concurrency == 8 &&
			req.ThrottleRate == 1000 &&
			req.Force
	})).Return(&service.StartReencryptionResponse{
		OperationID:         "reenc-1-2-full",
		Path:                "/secrets/",
		Started:             true,
		StartedAt:           now,
		TotalEntities:       50000,
		EstimatedCompletion: now.Add(2 * time.Hour),
	}, nil)

	mockSvc := newTestQuicBaaSWithClusterDEK(mockDEKSvc)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"from_version":  "1",
		"to_version":    "2",
		"path":          "/secrets/",
		"batch_size":    "500",
		"concurrency":   "8",
		"throttle_rate": "1000",
		"force":         "true",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_cluster_dek_start_reencryption", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "/secrets/", resultMap["path"])
	assert.Equal(t, int64(50000), resultMap["total_entities"])

	mockDEKSvc.AssertExpectations(t)
}
