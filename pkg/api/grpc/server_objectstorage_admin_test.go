//go:build !integration

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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Object Storage Admin Service
// =============================================================================

type mockObjectStorageAdminService struct {
	createBackendFunc                func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error)
	updateBackendFunc                func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error)
	deleteBackendFunc                func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error)
	setBackendStatusFunc             func(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error)
	getStorageStatsFunc              func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error)
	runGarbageCollectionFunc         func(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error)
	getGarbageCollectionProgressFunc func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error)
	repairObjectFunc                 func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error)
	compactBackendFunc               func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error)
}

func (m *mockObjectStorageAdminService) CreateBackend(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
	if m.createBackendFunc != nil {
		return m.createBackendFunc(ctx, req)
	}
	return &service.CreateBackendResponse{
		Name:      req.Name,
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageAdminService) UpdateBackend(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
	if m.updateBackendFunc != nil {
		return m.updateBackendFunc(ctx, req)
	}
	return &service.UpdateBackendResponse{
		Name:      req.Name,
		UpdatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageAdminService) DeleteBackend(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
	if m.deleteBackendFunc != nil {
		return m.deleteBackendFunc(ctx, req)
	}
	return &service.DeleteBackendResponse{
		Name:            req.Name,
		Deleted:         true,
		MigratedObjects: 0,
	}, nil
}

func (m *mockObjectStorageAdminService) SetBackendStatus(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
	if m.setBackendStatusFunc != nil {
		return m.setBackendStatusFunc(ctx, req)
	}
	return &service.SetBackendStatusResponse{
		Name:      req.Name,
		Enabled:   req.Enabled,
		UpdatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageAdminService) GetStorageStats(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
	if m.getStorageStatsFunc != nil {
		return m.getStorageStatsFunc(ctx, req)
	}
	return &service.GetStorageStatsResponse{
		TotalBytes:      1000000,
		TotalObjects:    100,
		TotalBackends:   2,
		HealthyBackends: 2,
		CalculatedAt:    time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageAdminService) RunGarbageCollection(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
	if m.runGarbageCollectionFunc != nil {
		return m.runGarbageCollectionFunc(ctx, req)
	}
	return &service.RunGarbageCollectionResponse{
		OperationID: "gc-operation-123",
		StartedAt:   time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageAdminService) GetGarbageCollectionProgress(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
	if m.getGarbageCollectionProgressFunc != nil {
		return m.getGarbageCollectionProgressFunc(ctx, req)
	}
	return &service.GetGarbageCollectionProgressResponse{
		OperationID:     req.OperationID,
		Status:          service.GCStatusRunning,
		ObjectsScanned:  50,
		ObjectsDeleted:  5,
		BytesReclaimed:  50000,
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
		RepairedLocations: []string{"replica1", "replica2"},
	}, nil
}

func (m *mockObjectStorageAdminService) CompactBackend(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
	if m.compactBackendFunc != nil {
		return m.compactBackendFunc(ctx, req)
	}
	return &service.CompactBackendResponse{
		Name:        req.Name,
		OperationID: "compact-operation-123",
		StartedAt:   time.Now().UnixNano(),
	}, nil
}

// =============================================================================
// Test Helper Functions
// =============================================================================

func newTestObjectStorageAdminServer(svc service.ObjectStorageAdminService) *objectStorageAdminServer {
	return &objectStorageAdminServer{service: svc}
}

// =============================================================================
// NewObjectStorageAdminServer Tests
// =============================================================================

func TestNewObjectStorageAdminServer_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := NewObjectStorageAdminServer(mockSvc)
	assert.NotNil(t, server)
}

func TestNewObjectStorageAdminServer_NilService(t *testing.T) {
	server := NewObjectStorageAdminServer(nil)
	assert.NotNil(t, server)
}

// =============================================================================
// CreateBackend Tests
// =============================================================================

func TestCreateBackend_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.CreateBackendRequest{
		Name:    "test-backend",
		Type:    pb.BackendType_BACKEND_TYPE_LOCAL,
		Tier:    pb.StorageTier_STORAGE_TIER_HOT,
		Config:  map[string]string{"path": "/data"},
		Enabled: true,
	}

	resp, err := server.CreateBackend(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.NotNil(t, resp.CreatedAt)
}

func TestCreateBackend_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.CreateBackend(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateBackend_gRPC_EmptyName(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.CreateBackendRequest{
		Type: pb.BackendType_BACKEND_TYPE_LOCAL,
		Tier: pb.StorageTier_STORAGE_TIER_HOT,
	}

	resp, err := server.CreateBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateBackend_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.CreateBackendRequest{
		Name: "test-backend",
		Type: pb.BackendType_BACKEND_TYPE_LOCAL,
		Tier: pb.StorageTier_STORAGE_TIER_HOT,
	}

	resp, err := server.CreateBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestCreateBackend_gRPC_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		createBackendFunc: func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrAlreadyExists,
				Op:   "test",
				Err:  nil,
			}
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.CreateBackendRequest{
		Name: "test-backend",
		Type: pb.BackendType_BACKEND_TYPE_LOCAL,
		Tier: pb.StorageTier_STORAGE_TIER_HOT,
	}

	resp, err := server.CreateBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
}

// =============================================================================
// UpdateBackend Tests
// =============================================================================

func TestUpdateBackend_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.UpdateBackendRequest{
		Name:   "test-backend",
		Config: map[string]string{"new-setting": "value"},
		Tier:   pb.StorageTier_STORAGE_TIER_WARM,
	}

	resp, err := server.UpdateBackend(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.NotNil(t, resp.UpdatedAt)
}

func TestUpdateBackend_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.UpdateBackend(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestUpdateBackend_gRPC_EmptyName(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.UpdateBackendRequest{
		Config: map[string]string{"key": "value"},
	}

	resp, err := server.UpdateBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestUpdateBackend_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.UpdateBackendRequest{
		Name:   "test-backend",
		Config: map[string]string{"key": "value"},
	}

	resp, err := server.UpdateBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestUpdateBackend_gRPC_NotFound(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		updateBackendFunc: func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  nil,
			}
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.UpdateBackendRequest{
		Name:   "nonexistent",
		Config: map[string]string{"key": "value"},
	}

	resp, err := server.UpdateBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
}

// =============================================================================
// DeleteBackend Tests
// =============================================================================

func TestDeleteBackend_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.DeleteBackendRequest{
		Name:  "test-backend",
		Force: true,
	}

	resp, err := server.DeleteBackend(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.True(t, resp.Deleted)
}

func TestDeleteBackend_gRPC_WithMigration(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		deleteBackendFunc: func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
			return &service.DeleteBackendResponse{
				Name:            req.Name,
				Deleted:         true,
				MigratedObjects: 50,
			}, nil
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.DeleteBackendRequest{
		Name:      "test-backend",
		MigrateTo: "target-backend",
	}

	resp, err := server.DeleteBackend(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, int64(50), resp.MigratedObjects)
}

func TestDeleteBackend_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.DeleteBackend(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteBackend_gRPC_EmptyName(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.DeleteBackendRequest{
		Force: true,
	}

	resp, err := server.DeleteBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDeleteBackend_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.DeleteBackendRequest{
		Name:  "test-backend",
		Force: true,
	}

	resp, err := server.DeleteBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// =============================================================================
// SetBackendStatus Tests
// =============================================================================

func TestSetBackendStatus_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.SetBackendStatusRequest{
		Name:    "test-backend",
		Enabled: true,
	}

	resp, err := server.SetBackendStatus(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.True(t, resp.Enabled)
	assert.NotNil(t, resp.UpdatedAt)
}

func TestSetBackendStatus_gRPC_Disable(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.SetBackendStatusRequest{
		Name:    "test-backend",
		Enabled: false,
	}

	resp, err := server.SetBackendStatus(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Enabled)
}

func TestSetBackendStatus_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.SetBackendStatus(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSetBackendStatus_gRPC_EmptyName(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.SetBackendStatusRequest{
		Enabled: true,
	}

	resp, err := server.SetBackendStatus(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSetBackendStatus_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.SetBackendStatusRequest{
		Name:    "test-backend",
		Enabled: true,
	}

	resp, err := server.SetBackendStatus(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// =============================================================================
// GetStorageStats Tests
// =============================================================================

func TestGetStorageStats_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.GetStorageStatsRequest{
		IncludeBackends: true,
		IncludeTenants:  true,
	}

	resp, err := server.GetStorageStats(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, int64(1000000), resp.TotalBytes)
	assert.Equal(t, int64(100), resp.TotalObjects)
	assert.Equal(t, int32(2), resp.TotalBackends)
	assert.Equal(t, int32(2), resp.HealthyBackends)
}

func TestGetStorageStats_gRPC_WithBackendStats(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		getStorageStatsFunc: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return &service.GetStorageStatsResponse{
				TotalBytes:      1000000,
				TotalObjects:    100,
				TotalBackends:   2,
				HealthyBackends: 2,
				BackendStats: []*service.BackendInfo{
					{Name: "backend1", Type: "local", Enabled: true, ObjectCount: 50},
					{Name: "backend2", Type: "s3", Enabled: true, ObjectCount: 50},
				},
				CalculatedAt: time.Now().UnixNano(),
			}, nil
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.GetStorageStatsRequest{
		IncludeBackends: true,
	}

	resp, err := server.GetStorageStats(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.BackendStats, 2)
}

func TestGetStorageStats_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.GetStorageStats(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetStorageStats_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.GetStorageStatsRequest{}

	resp, err := server.GetStorageStats(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// =============================================================================
// RunGarbageCollection Tests
// =============================================================================

func TestRunGarbageCollection_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.RunGarbageCollectionRequest{
		Backends:   []string{"backend1", "backend2"},
		DryRun:     false,
		Aggressive: true,
	}

	resp, err := server.RunGarbageCollection(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.OperationId)
	assert.NotNil(t, resp.StartedAt)
}

func TestRunGarbageCollection_gRPC_DryRun(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.RunGarbageCollectionRequest{
		DryRun: true,
	}

	resp, err := server.RunGarbageCollection(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestRunGarbageCollection_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.RunGarbageCollection(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRunGarbageCollection_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.RunGarbageCollectionRequest{}

	resp, err := server.RunGarbageCollection(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// =============================================================================
// GetGarbageCollectionProgress Tests
// =============================================================================

func TestGetGarbageCollectionProgress_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.GetGarbageCollectionProgressRequest{
		OperationId: "gc-operation-123",
	}

	resp, err := server.GetGarbageCollectionProgress(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "gc-operation-123", resp.OperationId)
	assert.Equal(t, "running", resp.Status)
	assert.Equal(t, int64(50), resp.ObjectsScanned)
}

func TestGetGarbageCollectionProgress_gRPC_Completed(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		getGarbageCollectionProgressFunc: func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
			return &service.GetGarbageCollectionProgressResponse{
				OperationID:     req.OperationID,
				Status:          service.GCStatusCompleted,
				ObjectsScanned:  100,
				ObjectsDeleted:  10,
				BytesReclaimed:  100000,
				PercentComplete: 100.0,
				CompletedAt:     time.Now().UnixNano(),
			}, nil
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.GetGarbageCollectionProgressRequest{
		OperationId: "gc-operation-123",
	}

	resp, err := server.GetGarbageCollectionProgress(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "completed", resp.Status)
	assert.NotNil(t, resp.CompletedAt)
}

func TestGetGarbageCollectionProgress_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.GetGarbageCollectionProgress(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetGarbageCollectionProgress_gRPC_EmptyOperationID(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.GetGarbageCollectionProgressRequest{
		OperationId: "",
	}

	resp, err := server.GetGarbageCollectionProgress(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetGarbageCollectionProgress_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.GetGarbageCollectionProgressRequest{
		OperationId: "gc-operation-123",
	}

	resp, err := server.GetGarbageCollectionProgress(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestGetGarbageCollectionProgress_gRPC_NotFound(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		getGarbageCollectionProgressFunc: func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  nil,
			}
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.GetGarbageCollectionProgressRequest{
		OperationId: "nonexistent",
	}

	resp, err := server.GetGarbageCollectionProgress(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
}

// =============================================================================
// RepairObject Tests
// =============================================================================

func TestRepairObject_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.RepairObjectRequest{
		Key:            "test-key",
		SourceLocation: "primary",
		TimeoutMs:      5000,
	}

	resp, err := server.RepairObject(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.Key)
	assert.True(t, resp.Success)
	assert.NotEmpty(t, resp.RepairedLocations)
}

func TestRepairObject_gRPC_NoRepairNeeded(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		repairObjectFunc: func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
			return &service.RepairObjectResponse{
				Key:               req.Key,
				Success:           true,
				RepairedLocations: []string{},
			}, nil
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.RepairObjectRequest{
		Key: "test-key",
	}

	resp, err := server.RepairObject(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.RepairedLocations)
}

func TestRepairObject_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.RepairObject(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRepairObject_gRPC_EmptyKey(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.RepairObjectRequest{
		Key: "",
	}

	resp, err := server.RepairObject(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRepairObject_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.RepairObjectRequest{
		Key: "test-key",
	}

	resp, err := server.RepairObject(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestRepairObject_gRPC_ObjectNotFound(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		repairObjectFunc: func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  nil,
			}
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.RepairObjectRequest{
		Key: "nonexistent-key",
	}

	resp, err := server.RepairObject(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
}

// =============================================================================
// CompactBackend Tests
// =============================================================================

func TestCompactBackend_gRPC_Success(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.CompactBackendRequest{
		Name:       "test-backend",
		Aggressive: false,
	}

	resp, err := server.CompactBackend(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.NotEmpty(t, resp.OperationId)
	assert.NotNil(t, resp.StartedAt)
}

func TestCompactBackend_gRPC_Aggressive(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.CompactBackendRequest{
		Name:       "test-backend",
		Aggressive: true,
	}

	resp, err := server.CompactBackend(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestCompactBackend_gRPC_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	resp, err := server.CompactBackend(context.Background(), nil)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCompactBackend_gRPC_EmptyName(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.CompactBackendRequest{
		Name: "",
	}

	resp, err := server.CompactBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCompactBackend_gRPC_ServiceUnavailable(t *testing.T) {
	server := newTestObjectStorageAdminServer(nil)

	req := &pb.CompactBackendRequest{
		Name: "test-backend",
	}

	resp, err := server.CompactBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestCompactBackend_gRPC_BackendNotFound(t *testing.T) {
	mockSvc := &mockObjectStorageAdminService{
		compactBackendFunc: func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  nil,
			}
		},
	}
	server := newTestObjectStorageAdminServer(mockSvc)

	req := &pb.CompactBackendRequest{
		Name: "nonexistent",
	}

	resp, err := server.CompactBackend(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)
}

// =============================================================================
// Proto Conversion Tests
// =============================================================================

func TestProtoBackendTypeToService(t *testing.T) {
	tests := []struct {
		input    pb.BackendType
		expected service.BackendType
	}{
		{pb.BackendType_BACKEND_TYPE_LOCAL, service.BackendTypeLocal},
		{pb.BackendType_BACKEND_TYPE_S3, service.BackendTypeS3},
		{pb.BackendType_BACKEND_TYPE_GCS, service.BackendTypeGCS},
		{pb.BackendType_BACKEND_TYPE_AZURE, service.BackendTypeAzure},
		{pb.BackendType_BACKEND_TYPE_MINIO, service.BackendTypeS3},
		{pb.BackendType_BACKEND_TYPE_PEBBLE, service.BackendTypeLocal},
		{pb.BackendType_BACKEND_TYPE_RAFT, service.BackendTypeLocal},
		{pb.BackendType_BACKEND_TYPE_UNSPECIFIED, service.BackendTypeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			result := protoBackendTypeToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtoStorageTierToService(t *testing.T) {
	tests := []struct {
		input    pb.StorageTier
		expected service.StorageTier
	}{
		{pb.StorageTier_STORAGE_TIER_HOT, service.StorageTierHot},
		{pb.StorageTier_STORAGE_TIER_WARM, service.StorageTierWarm},
		{pb.StorageTier_STORAGE_TIER_COLD, service.StorageTierCold},
		{pb.StorageTier_STORAGE_TIER_ARCHIVE, service.StorageTierCold},
		{pb.StorageTier_STORAGE_TIER_UNSPECIFIED, service.StorageTierUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			result := protoStorageTierToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServiceBackendInfoToProto(t *testing.T) {
	// Test with nil input
	result := serviceBackendInfoToProto(nil)
	assert.Nil(t, result)

	// Test with valid input
	info := &service.BackendInfo{
		Name:        "test-backend",
		Type:        "local",
		Enabled:     true,
		Tier:        "hot",
		Region:      "us-west-2",
		ObjectCount: 100,
		TotalSize:   10000,
		Health: &service.BackendHealthInfo{
			Healthy:   true,
			Latency:   10,
			LastCheck: time.Now().UnixNano(),
		},
	}

	result = serviceBackendInfoToProto(info)
	assert.NotNil(t, result)
	assert.Equal(t, "test-backend", result.Name)
	assert.True(t, result.Enabled)
	assert.True(t, result.Healthy)
	assert.Equal(t, "us-west-2", result.Region)
	assert.Equal(t, int64(100), result.ObjectCount)
}

func TestServiceTenantStatsToProto(t *testing.T) {
	// Test with nil input
	result := serviceTenantStatsToProto(nil)
	assert.Nil(t, result)

	// Test with valid input
	stats := &service.TenantStorageStats{
		TenantID:     "tenant1",
		TotalBytes:   100000,
		ObjectCount:  100,
		HotBytes:     50000,
		WarmBytes:    30000,
		ColdBytes:    15000,
		ArchiveBytes: 5000,
	}

	result = serviceTenantStatsToProto(stats)
	assert.NotNil(t, result)
	assert.Equal(t, "tenant1", result.TenantId)
	assert.Equal(t, int64(100000), result.TotalBytes)
	assert.Equal(t, int64(100), result.ObjectCount)
	assert.Equal(t, int64(50000), result.HotBytes)
}
