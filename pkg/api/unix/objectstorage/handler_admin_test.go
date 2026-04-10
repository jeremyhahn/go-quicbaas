//go:build unix

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

package objectstorage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockAdminService is a mock implementation for admin service testing.
type mockAdminService struct {
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

func (m *mockAdminService) CreateBackend(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
	if m.createBackendFunc != nil {
		return m.createBackendFunc(ctx, req)
	}
	return &service.CreateBackendResponse{Name: req.Name}, nil
}

func (m *mockAdminService) UpdateBackend(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
	if m.updateBackendFunc != nil {
		return m.updateBackendFunc(ctx, req)
	}
	return &service.UpdateBackendResponse{Name: req.Name}, nil
}

func (m *mockAdminService) DeleteBackend(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
	if m.deleteBackendFunc != nil {
		return m.deleteBackendFunc(ctx, req)
	}
	return &service.DeleteBackendResponse{Name: req.Name, Deleted: true}, nil
}

func (m *mockAdminService) SetBackendStatus(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
	if m.setBackendStatusFunc != nil {
		return m.setBackendStatusFunc(ctx, req)
	}
	return &service.SetBackendStatusResponse{Name: req.Name, Enabled: req.Enabled}, nil
}

func (m *mockAdminService) GetStorageStats(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
	if m.getStorageStatsFunc != nil {
		return m.getStorageStatsFunc(ctx, req)
	}
	return &service.GetStorageStatsResponse{TotalBytes: 1024, TotalObjects: 10}, nil
}

func (m *mockAdminService) RunGarbageCollection(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
	if m.runGarbageCollectionFunc != nil {
		return m.runGarbageCollectionFunc(ctx, req)
	}
	return &service.RunGarbageCollectionResponse{OperationID: "gc-123"}, nil
}

func (m *mockAdminService) GetGarbageCollectionProgress(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
	if m.getGarbageCollectionProgressFunc != nil {
		return m.getGarbageCollectionProgressFunc(ctx, req)
	}
	return &service.GetGarbageCollectionProgressResponse{
		OperationID: req.OperationID,
		Status:      service.GCStatusCompleted,
	}, nil
}

func (m *mockAdminService) RepairObject(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
	if m.repairObjectFunc != nil {
		return m.repairObjectFunc(ctx, req)
	}
	return &service.RepairObjectResponse{Key: req.Key, Success: true}, nil
}

func (m *mockAdminService) CompactBackend(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
	if m.compactBackendFunc != nil {
		return m.compactBackendFunc(ctx, req)
	}
	return &service.CompactBackendResponse{Name: req.Name, OperationID: "compact-123"}, nil
}

func TestHandleAdminCreateBackend(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CreateBackendRequest{
		Name:    "new-backend",
		Type:    service.BackendTypeS3,
		Tier:    service.StorageTierHot,
		Enabled: true,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminCreateBackend, reqPayload)
	require.NoError(t, err)

	var createResp service.CreateBackendResponse
	err = DecodePayload(resp, &createResp)
	require.NoError(t, err)
	assert.Equal(t, "new-backend", createResp.Name)
}

func TestHandleAdminCreateBackendNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CreateBackendRequest{
		Name: "new-backend",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminCreateBackend, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleAdminUpdateBackend(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.UpdateBackendRequest{
		Name: "backend-1",
		Tier: service.StorageTierWarm,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminUpdateBackend, reqPayload)
	require.NoError(t, err)

	var updateResp service.UpdateBackendResponse
	err = DecodePayload(resp, &updateResp)
	require.NoError(t, err)
	assert.Equal(t, "backend-1", updateResp.Name)
}

func TestHandleAdminUpdateBackendNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.UpdateBackendRequest{
		Name: "backend-1",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminUpdateBackend, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleAdminDeleteBackend(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.DeleteBackendRequest{
		Name:      "backend-1",
		MigrateTo: "backend-2",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminDeleteBackend, reqPayload)
	require.NoError(t, err)

	var delResp service.DeleteBackendResponse
	err = DecodePayload(resp, &delResp)
	require.NoError(t, err)
	assert.Equal(t, "backend-1", delResp.Name)
	assert.True(t, delResp.Deleted)
}

func TestHandleAdminDeleteBackendNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.DeleteBackendRequest{
		Name: "backend-1",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminDeleteBackend, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleAdminSetBackendStatus(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.SetBackendStatusRequest{
		Name:    "backend-1",
		Enabled: false,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminSetBackendStatus, reqPayload)
	require.NoError(t, err)

	var statusResp service.SetBackendStatusResponse
	err = DecodePayload(resp, &statusResp)
	require.NoError(t, err)
	assert.Equal(t, "backend-1", statusResp.Name)
	assert.False(t, statusResp.Enabled)
}

func TestHandleAdminSetBackendStatusNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.SetBackendStatusRequest{
		Name:    "backend-1",
		Enabled: false,
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminSetBackendStatus, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleAdminGetStorageStats(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetStorageStatsRequest{
		IncludeBackends: true,
		IncludeTenants:  true,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminGetStorageStats, reqPayload)
	require.NoError(t, err)

	var statsResp service.GetStorageStatsResponse
	err = DecodePayload(resp, &statsResp)
	require.NoError(t, err)
	assert.Equal(t, int64(1024), statsResp.TotalBytes)
	assert.Equal(t, int64(10), statsResp.TotalObjects)
}

func TestHandleAdminGetStorageStatsNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetStorageStatsRequest{})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminGetStorageStats, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleAdminRunGC(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.RunGarbageCollectionRequest{
		Backends:   []string{"backend-1"},
		DryRun:     false,
		Aggressive: true,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminRunGC, reqPayload)
	require.NoError(t, err)

	var gcResp service.RunGarbageCollectionResponse
	err = DecodePayload(resp, &gcResp)
	require.NoError(t, err)
	assert.Equal(t, "gc-123", gcResp.OperationID)
}

func TestHandleAdminRunGCNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.RunGarbageCollectionRequest{})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminRunGC, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleAdminGetGCProgress(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetGarbageCollectionProgressRequest{
		OperationID: "gc-123",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminGetGCProgress, reqPayload)
	require.NoError(t, err)

	var progressResp service.GetGarbageCollectionProgressResponse
	err = DecodePayload(resp, &progressResp)
	require.NoError(t, err)
	assert.Equal(t, "gc-123", progressResp.OperationID)
	assert.Equal(t, service.GCStatusCompleted, progressResp.Status)
}

func TestHandleAdminGetGCProgressNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetGarbageCollectionProgressRequest{
		OperationID: "gc-123",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminGetGCProgress, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleAdminRepairObject(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.RepairObjectRequest{
		Key:            "corrupt-object",
		SourceLocation: "backend-1",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminRepairObject, reqPayload)
	require.NoError(t, err)

	var repairResp service.RepairObjectResponse
	err = DecodePayload(resp, &repairResp)
	require.NoError(t, err)
	assert.Equal(t, "corrupt-object", repairResp.Key)
	assert.True(t, repairResp.Success)
}

func TestHandleAdminRepairObjectNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.RepairObjectRequest{
		Key: "corrupt-object",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminRepairObject, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandleAdminCompactBackend(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CompactBackendRequest{
		Name:       "backend-1",
		Aggressive: true,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpAdminCompactBackend, reqPayload)
	require.NoError(t, err)

	var compactResp service.CompactBackendResponse
	err = DecodePayload(resp, &compactResp)
	require.NoError(t, err)
	assert.Equal(t, "backend-1", compactResp.Name)
	assert.Equal(t, "compact-123", compactResp.OperationID)
}

func TestHandleAdminCompactBackendNoAdminService(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CompactBackendRequest{
		Name: "backend-1",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpAdminCompactBackend, reqPayload)
	require.Error(t, err)

	var dragonErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dragonErr))
	assert.Equal(t, dberrors.ErrUnavailable, dragonErr.Code)
}

func TestHandlerWithAdminService(t *testing.T) {
	adminSvc := &mockAdminService{}
	handler, err := NewHandler(&mockObjectStorageService{}, adminSvc, &testLogger{})
	require.NoError(t, err)

	assert.NotNil(t, handler.AdminService())
}
