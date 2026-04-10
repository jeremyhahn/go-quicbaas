//go:build mcp

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Object Storage Admin Service
// =============================================================================

type mockObjectStorageAdminService struct {
	createBackendFunc    func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error)
	updateBackendFunc    func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error)
	deleteBackendFunc    func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error)
	setBackendStatusFunc func(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error)
	getStorageStatsFunc  func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error)
	runGCFunc            func(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error)
	getGCProgressFunc    func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error)
	repairObjectFunc     func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error)
	compactBackendFunc   func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error)
}

func (m *mockObjectStorageAdminService) CreateBackend(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
	if m.createBackendFunc != nil {
		return m.createBackendFunc(ctx, req)
	}
	return &service.CreateBackendResponse{
		Name:      req.Name,
		CreatedAt: time.Now().Unix(),
	}, nil
}

func (m *mockObjectStorageAdminService) UpdateBackend(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
	if m.updateBackendFunc != nil {
		return m.updateBackendFunc(ctx, req)
	}
	return &service.UpdateBackendResponse{
		Name:      req.Name,
		UpdatedAt: time.Now().Unix(),
	}, nil
}

func (m *mockObjectStorageAdminService) DeleteBackend(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
	if m.deleteBackendFunc != nil {
		return m.deleteBackendFunc(ctx, req)
	}
	return &service.DeleteBackendResponse{
		Name:    req.Name,
		Deleted: true,
	}, nil
}

func (m *mockObjectStorageAdminService) SetBackendStatus(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
	if m.setBackendStatusFunc != nil {
		return m.setBackendStatusFunc(ctx, req)
	}
	return &service.SetBackendStatusResponse{
		Name:      req.Name,
		Enabled:   req.Enabled,
		UpdatedAt: time.Now().Unix(),
	}, nil
}

func (m *mockObjectStorageAdminService) GetStorageStats(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
	if m.getStorageStatsFunc != nil {
		return m.getStorageStatsFunc(ctx, req)
	}
	return &service.GetStorageStatsResponse{
		TotalObjects:    1000,
		TotalBytes:      1024 * 1024 * 1024,
		TotalBackends:   3,
		HealthyBackends: 3,
		CalculatedAt:    time.Now().Unix(),
	}, nil
}

func (m *mockObjectStorageAdminService) RunGarbageCollection(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
	if m.runGCFunc != nil {
		return m.runGCFunc(ctx, req)
	}
	return &service.RunGarbageCollectionResponse{
		OperationID: "gc-123",
		StartedAt:   time.Now().Unix(),
	}, nil
}

func (m *mockObjectStorageAdminService) GetGarbageCollectionProgress(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
	if m.getGCProgressFunc != nil {
		return m.getGCProgressFunc(ctx, req)
	}
	return &service.GetGarbageCollectionProgressResponse{
		OperationID:     req.OperationID,
		Status:          service.GCStatusRunning,
		ObjectsScanned:  500,
		ObjectsDeleted:  50,
		BytesReclaimed:  1024 * 1024,
		PercentComplete: 50.0,
	}, nil
}

func (m *mockObjectStorageAdminService) RepairObject(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
	if m.repairObjectFunc != nil {
		return m.repairObjectFunc(ctx, req)
	}
	return &service.RepairObjectResponse{
		Key:               req.Key,
		Success:           true,
		RepairedLocations: []string{"loc1"},
	}, nil
}

func (m *mockObjectStorageAdminService) CompactBackend(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
	if m.compactBackendFunc != nil {
		return m.compactBackendFunc(ctx, req)
	}
	return &service.CompactBackendResponse{
		Name:        req.Name,
		OperationID: "compact-123",
		StartedAt:   time.Now().Unix(),
	}, nil
}

// =============================================================================
// Mock Service with Object Storage Admin Support
// =============================================================================

type objStorageAdminMockService struct {
	mockService
	objAdminSvc *mockObjectStorageAdminService
}

func (m *objStorageAdminMockService) ObjectStorageAdminService() service.ObjectStorageAdminService {
	return m.objAdminSvc
}

// =============================================================================
// Test Helpers
// =============================================================================

func newObjAdminRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &objStorageAdminMockService{
		objAdminSvc: &mockObjectStorageAdminService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func newObjAdminRegistryWithMock(t *testing.T, adminSvc *mockObjectStorageAdminService) *ToolRegistry {
	t.Helper()
	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

// =============================================================================
// CreateBackend Tests
// =============================================================================

func TestObjectAdminCreateBackend_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "minimal",
			args: map[string]interface{}{
				"name": "backend-1",
				"type": "local",
				"tier": "hot",
			},
		},
		{
			name: "with enabled",
			args: map[string]interface{}{
				"name":    "backend-2",
				"type":    "s3",
				"tier":    "warm",
				"enabled": true,
			},
		},
		{
			name: "with config",
			args: map[string]interface{}{
				"name": "backend-3",
				"type": "gcs",
				"tier": "cold",
				"config": map[string]interface{}{
					"bucket":   "my-bucket",
					"endpoint": "https://storage.googleapis.com",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_create_backend", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "name")
			assert.Contains(t, resultMap, "success")
			assert.Equal(t, true, resultMap["success"])
		})
	}
}

func TestObjectAdminCreateBackend_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing name",
			args:        map[string]interface{}{"type": "local", "tier": "hot"},
			errContains: "name",
		},
		{
			name:        "missing type",
			args:        map[string]interface{}{"name": "test", "tier": "hot"},
			errContains: "type",
		},
		{
			name:        "missing tier",
			args:        map[string]interface{}{"name": "test", "type": "local"},
			errContains: "tier",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newObjAdminRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_create_backend", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestObjectAdminCreateBackend_ServiceError(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		createBackendFunc: func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
			return nil, &dberrors.MessageError{Message: "backend already exists"}
		},
	}
	registry := newObjAdminRegistryWithMock(t, adminSvc)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_create_backend", map[string]interface{}{
		"name": "existing",
		"type": "local",
		"tier": "hot",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "already exists")
}

// =============================================================================
// UpdateBackend Tests
// =============================================================================

func TestObjectAdminUpdateBackend_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "with tier",
			args: map[string]interface{}{
				"name": "backend-1",
				"tier": "warm",
			},
		},
		{
			name: "with config",
			args: map[string]interface{}{
				"name": "backend-2",
				"config": map[string]interface{}{
					"max_size": "10GB",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_update_backend", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "name")
			assert.Contains(t, resultMap, "success")
		})
	}
}

func TestObjectAdminUpdateBackend_MissingName(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_update_backend", map[string]interface{}{
		"tier": "warm",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "name")
}

// =============================================================================
// DeleteBackend Tests
// =============================================================================

func TestObjectAdminDeleteBackend_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "simple delete",
			args: map[string]interface{}{"name": "backend-1"},
		},
		{
			name: "with migrate_to",
			args: map[string]interface{}{
				"name":       "backend-2",
				"migrate_to": "backend-3",
			},
		},
		{
			name: "force delete",
			args: map[string]interface{}{
				"name":  "backend-4",
				"force": true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_delete_backend", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "name")
			assert.Contains(t, resultMap, "deleted")
		})
	}
}

func TestObjectAdminDeleteBackend_MissingName(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_delete_backend", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "name")
}

// =============================================================================
// SetBackendStatus Tests
// =============================================================================

func TestObjectAdminSetBackendStatus_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "enable backend",
			args: map[string]interface{}{
				"name":    "backend-1",
				"enabled": true,
			},
		},
		{
			name: "disable backend",
			args: map[string]interface{}{
				"name":    "backend-2",
				"enabled": false,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_set_backend_status", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "name")
			assert.Contains(t, resultMap, "enabled")
		})
	}
}

func TestObjectAdminSetBackendStatus_MissingName(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_set_backend_status", map[string]interface{}{
		"enabled": true,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "name")
}

// =============================================================================
// GetStorageStats Tests
// =============================================================================

func TestObjectAdminGetStorageStats_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "default",
			args: map[string]interface{}{},
		},
		{
			name: "with breakdowns",
			args: map[string]interface{}{
				"include_backends": true,
				"include_tenants":  true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_storage_stats", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "total_objects")
			assert.Contains(t, resultMap, "total_bytes")
		})
	}
}

// =============================================================================
// RunGC Tests
// =============================================================================

func TestObjectAdminRunGC_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "default",
			args: map[string]interface{}{},
		},
		{
			name: "with backends",
			args: map[string]interface{}{
				"backends": []interface{}{"backend-1", "backend-2"},
			},
		},
		{
			name: "dry run",
			args: map[string]interface{}{
				"dry_run": true,
			},
		},
		{
			name: "aggressive",
			args: map[string]interface{}{
				"aggressive": true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_run_gc", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "operation_id")
		})
	}
}

// =============================================================================
// GetGCProgress Tests
// =============================================================================

func TestObjectAdminGetGCProgress_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_gc_progress", map[string]interface{}{
		"operation_id": "gc-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "operation_id")
	assert.Contains(t, resultMap, "percent_complete")
}

func TestObjectAdminGetGCProgress_MissingOperationID(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_gc_progress", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "operation_id")
}

// =============================================================================
// RepairObject Tests
// =============================================================================

func TestObjectAdminRepairObject_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_repair_object", map[string]interface{}{
		"key": "my-bucket/my-object",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "key")
	assert.Contains(t, resultMap, "success")
}

func TestObjectAdminRepairObject_MissingKey(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_repair_object", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "key")
}

// =============================================================================
// CompactBackend Tests
// =============================================================================

func TestObjectAdminCompactBackend_Success(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "simple compact",
			args: map[string]interface{}{"name": "backend-1"},
		},
		{
			name: "aggressive",
			args: map[string]interface{}{
				"name":       "backend-2",
				"aggressive": true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_compact_backend", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "name")
			assert.Contains(t, resultMap, "operation_id")
		})
	}
}

func TestObjectAdminCompactBackend_MissingName(t *testing.T) {
	registry := newObjAdminRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_compact_backend", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "name")
}

// =============================================================================
// Service Unavailable Tests
// =============================================================================

func TestObjectAdminTools_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	adminTools := []string{
		"quicbaas_object_admin_create_backend",
		"quicbaas_object_admin_update_backend",
		"quicbaas_object_admin_delete_backend",
		"quicbaas_object_admin_set_backend_status",
		"quicbaas_object_admin_get_storage_stats",
		"quicbaas_object_admin_run_gc",
		"quicbaas_object_admin_get_gc_progress",
		"quicbaas_object_admin_repair_object",
		"quicbaas_object_admin_compact_backend",
	}

	for _, toolName := range adminTools {
		t.Run(toolName, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, toolName, map[string]interface{}{
				"name":         "test",
				"type":         "local",
				"tier":         "hot",
				"key":          "key",
				"operation_id": "op-123",
			})
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// =============================================================================
// Object Admin Delete Backend - Service Error
// =============================================================================

func TestExecuteObjectAdminDeleteBackend_ServiceError(t *testing.T) {
	expectedErr := errors.New("delete backend failed")
	adminSvc := &mockObjectStorageAdminService{
		deleteBackendFunc: func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_delete_backend", map[string]interface{}{
		"name": "test-backend",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "delete backend failed")
}

// =============================================================================
// Object Admin Run GC - Service Error
// =============================================================================

func TestExecuteObjectAdminRunGC_ServiceError(t *testing.T) {
	expectedErr := errors.New("gc failed")
	adminSvc := &mockObjectStorageAdminService{
		runGCFunc: func(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_run_gc", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "gc failed")
}

// =============================================================================
// Object Admin Get GC Progress - Service Error
// =============================================================================

func TestExecuteObjectAdminGetGCProgress_ServiceError(t *testing.T) {
	expectedErr := errors.New("get gc progress failed")
	adminSvc := &mockObjectStorageAdminService{
		getGCProgressFunc: func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_gc_progress", map[string]interface{}{
		"operation_id": "gc-123",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "get gc progress failed")
}

// =============================================================================
// Object Admin Repair Object - Service Error
// =============================================================================

func TestExecuteObjectAdminRepairObject_ServiceError(t *testing.T) {
	expectedErr := errors.New("repair object failed")
	adminSvc := &mockObjectStorageAdminService{
		repairObjectFunc: func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_repair_object", map[string]interface{}{
		"key": "test:object.txt",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "repair object failed")
}

// =============================================================================
// Object Admin Compact Backend - Service Error
// =============================================================================

func TestExecuteObjectAdminCompactBackend_ServiceError(t *testing.T) {
	expectedErr := errors.New("compact backend failed")
	adminSvc := &mockObjectStorageAdminService{
		compactBackendFunc: func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_compact_backend", map[string]interface{}{
		"name": "local",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "compact backend failed")
}

// =============================================================================
// Object Admin Update Backend - Service Error
// =============================================================================

func TestExecuteObjectAdminUpdateBackend_ServiceError(t *testing.T) {
	expectedErr := errors.New("update backend failed")
	adminSvc := &mockObjectStorageAdminService{
		updateBackendFunc: func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_update_backend", map[string]interface{}{
		"name": "local",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "update backend failed")
}

// =============================================================================
// Object Admin Get Storage Stats - Service Error
// =============================================================================

func TestExecuteObjectAdminGetStorageStats_ServiceError(t *testing.T) {
	expectedErr := errors.New("storage stats failed")
	adminSvc := &mockObjectStorageAdminService{
		getStorageStatsFunc: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_storage_stats", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "storage stats failed")
}

// =============================================================================
// Object Admin Create Backend - Service Error
// =============================================================================

func TestExecuteObjectAdminCreateBackend_ServiceError(t *testing.T) {
	expectedErr := errors.New("create backend failed")
	adminSvc := &mockObjectStorageAdminService{
		createBackendFunc: func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_create_backend", map[string]interface{}{
		"name": "new-backend",
		"type": "local",
		"tier": "hot",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "create backend failed")
}

// =============================================================================
// Object Admin Tools - Missing Required Parameters
// =============================================================================

func TestExecuteObjectAdminCompactBackend_MissingName(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{}
	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_compact_backend", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "name is required")
}

func TestExecuteObjectAdminRepairObject_MissingKey(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{}
	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_repair_object", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "key is required")
}

func TestExecuteObjectAdminDeleteBackend_MissingName(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{}
	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_delete_backend", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "name is required")
}

func TestExecuteObjectAdminUpdateBackend_MissingName(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{}
	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_update_backend", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "name is required")
}

func TestExecuteObjectAdminGetGCProgress_MissingOperationID(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{}
	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_gc_progress", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "operation_id is required")
}

// =============================================================================
// GetStorageStats - Backend and Tenant Stats Branches
// =============================================================================

func TestObjectAdminGetStorageStats_WithBackendStats(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		getStorageStatsFunc: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return &service.GetStorageStatsResponse{
				TotalObjects:    2000,
				TotalBytes:      2 * 1024 * 1024 * 1024,
				TotalBackends:   2,
				HealthyBackends: 2,
				CalculatedAt:    time.Now().Unix(),
				BackendStats: []*service.BackendInfo{
					{
						Name:        "local",
						Type:        "filesystem",
						Enabled:     true,
						Tier:        "hot",
						ObjectCount: 1500,
						TotalSize:   1024 * 1024 * 1024,
					},
					{
						Name:        "s3",
						Type:        "s3",
						Enabled:     true,
						Tier:        "warm",
						ObjectCount: 500,
						TotalSize:   1024 * 1024 * 1024,
					},
				},
			}, nil
		},
	}
	registry := newObjAdminRegistryWithMock(t, adminSvc)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_storage_stats", map[string]interface{}{
		"include_backends": true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "backend_stats")

	// After marshalResult JSON round-trip, slices become []interface{}
	backends, ok := resultMap["backend_stats"].([]interface{})
	require.True(t, ok)
	assert.Len(t, backends, 2)

	first, ok := backends[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "local", first["name"])
}

func TestObjectAdminGetStorageStats_WithTenantStats(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		getStorageStatsFunc: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return &service.GetStorageStatsResponse{
				TotalObjects:    1000,
				TotalBytes:      1024 * 1024,
				TotalBackends:   1,
				HealthyBackends: 1,
				CalculatedAt:    time.Now().Unix(),
				TenantStats: []*service.TenantStorageStats{
					{
						TenantID:     "tenant-1",
						TotalBytes:   512 * 1024,
						ObjectCount:  600,
						HotBytes:     256 * 1024,
						WarmBytes:    128 * 1024,
						ColdBytes:    64 * 1024,
						ArchiveBytes: 64 * 1024,
					},
				},
			}, nil
		},
	}
	registry := newObjAdminRegistryWithMock(t, adminSvc)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_storage_stats", map[string]interface{}{
		"include_tenants": true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "tenant_stats")

	// After marshalResult JSON round-trip, slices become []interface{}
	tenants, ok := resultMap["tenant_stats"].([]interface{})
	require.True(t, ok)
	assert.Len(t, tenants, 1)

	first, ok := tenants[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "tenant-1", first["tenant_id"])
}

func TestObjectAdminGetStorageStats_ServiceError(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		getStorageStatsFunc: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return nil, &dberrors.MessageError{Message: "stats unavailable"}
		},
	}
	registry := newObjAdminRegistryWithMock(t, adminSvc)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_storage_stats", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stats unavailable")
}

// =============================================================================
// GetGCProgress - Error and CompletedAt Branches
// =============================================================================

func TestObjectAdminGetGCProgress_WithErrorAndCompletedAt(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		getGCProgressFunc: func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
			return &service.GetGarbageCollectionProgressResponse{
				OperationID:     "gc-456",
				Status:          service.GCStatusCompleted,
				ObjectsScanned:  1000,
				ObjectsDeleted:  100,
				BytesReclaimed:  2 * 1024 * 1024,
				PercentComplete: 100.0,
				Error:           "partial error: 3 objects skipped",
				CompletedAt:     time.Now().Unix(),
			}, nil
		},
	}
	registry := newObjAdminRegistryWithMock(t, adminSvc)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_gc_progress", map[string]interface{}{
		"operation_id": "gc-456",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "error")
	assert.Contains(t, resultMap, "completed_at")
	assert.Equal(t, "partial error: 3 objects skipped", resultMap["error"])
}

func TestObjectAdminGetGCProgress_ServiceError(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		getGCProgressFunc: func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
			return nil, &dberrors.MessageError{Message: "gc operation not found"}
		},
	}
	registry := newObjAdminRegistryWithMock(t, adminSvc)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_gc_progress", map[string]interface{}{
		"operation_id": "gc-unknown",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gc operation not found")
}

// =============================================================================
// RepairObject - Error Field Branch
// =============================================================================

func TestObjectAdminRepairObject_WithError(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		repairObjectFunc: func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
			return &service.RepairObjectResponse{
				Key:               req.Key,
				Success:           false,
				RepairedLocations: nil,
				Error:             "checksum mismatch at location-2",
			}, nil
		},
	}
	registry := newObjAdminRegistryWithMock(t, adminSvc)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_repair_object", map[string]interface{}{
		"key": "test:broken-object",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "error")
	assert.Equal(t, "checksum mismatch at location-2", resultMap["error"])
}

func TestObjectAdminRepairObject_ServiceError(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		repairObjectFunc: func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
			return nil, &dberrors.MessageError{Message: "repair timeout"}
		},
	}
	registry := newObjAdminRegistryWithMock(t, adminSvc)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_repair_object", map[string]interface{}{
		"key": "test:object",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repair timeout")
}
