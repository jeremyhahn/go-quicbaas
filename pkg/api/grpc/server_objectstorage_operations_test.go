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
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// testMockObjectStorageService is a mock implementation for testing
type testMockObjectStorageService struct {
	listByTagFunc        func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error)
	listByTenantFunc     func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error)
	copyObjectFunc       func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error)
	replicateObjectFunc  func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error)
	migrateObjectFunc    func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error)
	archiveObjectFunc    func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error)
	restoreObjectFunc    func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error)
	verifyObjectFunc     func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error)
	setObjectPolicyFunc  func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error)
	getObjectPolicyFunc  func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error)
	listBackendsFunc     func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error)
	getBackendHealthFunc func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error)
	getTenantUsageFunc   func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error)
	getTenantQuotasFunc  func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error)
	setTenantQuotasFunc  func(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error)
}

func (m *testMockObjectStorageService) PutObject(_ context.Context, _ *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	return &service.PutObjectResponse{}, nil
}

func (m *testMockObjectStorageService) GetObject(_ context.Context, _ *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	return &service.GetObjectResponse{}, nil
}

func (m *testMockObjectStorageService) DeleteObject(_ context.Context, _ *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	return &service.DeleteObjectResponse{}, nil
}

func (m *testMockObjectStorageService) HeadObject(_ context.Context, _ *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	return &service.HeadObjectResponse{}, nil
}

func (m *testMockObjectStorageService) ListObjects(_ context.Context, _ *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	return &service.ListObjectsResponse{}, nil
}

func (m *testMockObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	if m.listByTagFunc != nil {
		return m.listByTagFunc(ctx, req)
	}
	return &service.ListByTagResponse{
		Objects: []*service.ObjectSummary{
			{Key: "tagged/obj1", Size: 100, Status: "active"},
			{Key: "tagged/obj2", Size: 200, Status: "active"},
		},
		NextCursor: "cursor456",
		HasMore:    false,
		TotalCount: 2,
	}, nil
}

func (m *testMockObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	if m.listByTenantFunc != nil {
		return m.listByTenantFunc(ctx, req)
	}
	return &service.ListByTenantResponse{
		Objects: []*service.ObjectSummary{
			{Key: "tenant/obj1", Size: 100, Status: "active"},
			{Key: "tenant/obj2", Size: 200, Status: "active"},
		},
		NextCursor: "cursor789",
		HasMore:    false,
		TotalCount: 2,
	}, nil
}

func (m *testMockObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	if m.copyObjectFunc != nil {
		return m.copyObjectFunc(ctx, req)
	}
	return &service.CopyObjectResponse{
		Key:       req.DestinationKey,
		Version:   1,
		Size:      100,
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *testMockObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	if m.replicateObjectFunc != nil {
		return m.replicateObjectFunc(ctx, req)
	}
	results := make(map[string]*service.ReplicationResult)
	for _, target := range req.Targets {
		results[target] = &service.ReplicationResult{Success: true}
	}
	return &service.ReplicateObjectResponse{
		Key:     req.Key,
		Status:  "success",
		Results: results,
	}, nil
}

func (m *testMockObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	if m.migrateObjectFunc != nil {
		return m.migrateObjectFunc(ctx, req)
	}
	return &service.MigrateObjectResponse{
		Key:               req.Key,
		SourceBackend:     "local",
		TargetBackend:     req.TargetBackend,
		Success:           true,
		DeletedFromSource: req.DeleteFromSource,
	}, nil
}

func (m *testMockObjectStorageService) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	if m.archiveObjectFunc != nil {
		return m.archiveObjectFunc(ctx, req)
	}
	return &service.ArchiveObjectResponse{
		Key:          req.Key,
		ArchiveStore: req.TargetStore,
		Success:      true,
		ArchivedAt:   time.Now().UnixNano(),
	}, nil
}

func (m *testMockObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	if m.restoreObjectFunc != nil {
		return m.restoreObjectFunc(ctx, req)
	}
	return &service.RestoreObjectResponse{
		Key:     req.Key,
		Backend: req.TargetBackend,
		Success: true,
	}, nil
}

func (m *testMockObjectStorageService) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	if m.verifyObjectFunc != nil {
		return m.verifyObjectFunc(ctx, req)
	}
	return &service.VerifyObjectResponse{
		Key:   req.Key,
		Valid: true,
		Results: map[string]*service.VerificationResult{
			"local": {Store: "local", Valid: true},
			"s3":    {Store: "s3", Valid: true},
		},
		Repaired: false,
	}, nil
}

func (m *testMockObjectStorageService) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	if m.setObjectPolicyFunc != nil {
		return m.setObjectPolicyFunc(ctx, req)
	}
	return &service.SetObjectPolicyResponse{
		Key:       req.Key,
		Version:   2,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *testMockObjectStorageService) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	if m.getObjectPolicyFunc != nil {
		return m.getObjectPolicyFunc(ctx, req)
	}
	return &service.GetObjectPolicyResponse{
		Key: req.Key,
		ReplicationPolicy: &catalog.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 3,
		},
		ArchivePolicy: &catalog.ArchivePolicy{
			Enabled:          true,
			ArchiveAfterDays: 30,
		},
		RetentionPolicy: &catalog.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 90,
		},
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *testMockObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc(ctx, req)
	}
	return &service.ListBackendsResponse{
		Backends: []*service.BackendInfo{
			{Name: "local", Type: "local", Enabled: true, Tier: "hot"},
			{Name: "s3-primary", Type: "s3", Enabled: true, Tier: "warm"},
		},
	}, nil
}

func (m *testMockObjectStorageService) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	if m.getBackendHealthFunc != nil {
		return m.getBackendHealthFunc(ctx, req)
	}
	return &service.GetBackendHealthResponse{
		Name:      req.Name,
		Healthy:   true,
		Latency:   10,
		LastCheck: time.Now().UnixNano(),
		Status:    "healthy",
	}, nil
}

func (m *testMockObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	if m.getTenantUsageFunc != nil {
		return m.getTenantUsageFunc(ctx, req)
	}
	return &service.GetTenantUsageResponse{
		TenantID:    req.TenantID,
		ObjectCount: 100,
		TotalSize:   1024 * 1024,
	}, nil
}

func (m *testMockObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	if m.getTenantQuotasFunc != nil {
		return m.getTenantQuotasFunc(ctx, req)
	}
	return &service.GetTenantQuotasResponse{
		TenantID:        req.TenantID,
		MaxObjectCount:  10000,
		MaxStorageBytes: 1024 * 1024 * 1024,
	}, nil
}

func (m *testMockObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	if m.setTenantQuotasFunc != nil {
		return m.setTenantQuotasFunc(ctx, req)
	}
	return &service.SetTenantQuotasResponse{
		TenantID:  req.TenantID,
		UpdatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *testMockObjectStorageService) BatchPutObjects(_ context.Context, _ *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	return &service.BatchPutObjectsResponse{}, nil
}

func (m *testMockObjectStorageService) BatchGetObjects(_ context.Context, _ *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	return &service.BatchGetObjectsResponse{}, nil
}

func (m *testMockObjectStorageService) BatchDeleteObjects(_ context.Context, _ *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	return &service.BatchDeleteObjectsResponse{}, nil
}

// ========================================
// ListByTag Tests
// ========================================

func TestObjectStorageServer_ListByTag(t *testing.T) {
	failingSvc := &testMockObjectStorageService{
		listByTagFunc: func(_ context.Context, _ *service.ListByTagRequest) (*service.ListByTagResponse, error) {
			return nil, &dberrors.QBError{
				Code: dberrors.ErrInternal,
				Op:   "ListByTag",
				Err:  &dberrors.MessageError{Message: "database error"},
			}
		},
	}
	tests := []struct {
		name     string
		svc      service.ObjectStorageService
		req      *pb.ListByTagRequest
		wantCode codes.Code
		wantKeys int
	}{
		{
			name:     "success",
			svc:      &testMockObjectStorageService{},
			req:      &pb.ListByTagRequest{Tag: "env", Value: "production", TenantId: "tenant-1", Limit: 100},
			wantCode: codes.OK,
			wantKeys: 2,
		},
		{name: "nil request", svc: &testMockObjectStorageService{}, req: nil, wantCode: codes.InvalidArgument},
		{name: "empty tag", svc: &testMockObjectStorageService{}, req: &pb.ListByTagRequest{Value: "test"}, wantCode: codes.InvalidArgument},
		{name: "nil service", svc: nil, req: &pb.ListByTagRequest{Tag: "env", Value: "test"}, wantCode: codes.Unavailable},
		{name: "service error", svc: failingSvc, req: &pb.ListByTagRequest{Tag: "env", Value: "test"}, wantCode: codes.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &objectStorageServer{service: tt.svc}

			resp, err := srv.ListByTag(context.Background(), tt.req)

			if tt.wantCode != codes.OK {
				assert.Nil(t, resp)
				assert.Equal(t, tt.wantCode, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.Len(t, resp.Keys, tt.wantKeys)
		})
	}
}

// ========================================
// ListByTenant Tests
// ========================================

func TestObjectStorageServer_ListByTenant(t *testing.T) {
	tests := []struct {
		name     string
		svc      service.ObjectStorageService
		req      *pb.ListByTenantRequest
		wantCode codes.Code
	}{
		{
			name:     "success",
			svc:      &testMockObjectStorageService{},
			req:      &pb.ListByTenantRequest{TenantId: "tenant-1", Limit: 100, IncludeMetadata: true},
			wantCode: codes.OK,
		},
		{name: "nil request", svc: &testMockObjectStorageService{}, req: nil, wantCode: codes.InvalidArgument},
		{name: "empty tenant id", svc: &testMockObjectStorageService{}, req: &pb.ListByTenantRequest{}, wantCode: codes.InvalidArgument},
		{name: "nil service", svc: nil, req: &pb.ListByTenantRequest{TenantId: "tenant-1"}, wantCode: codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &objectStorageServer{service: tt.svc}

			resp, err := srv.ListByTenant(context.Background(), tt.req)

			if tt.wantCode != codes.OK {
				assert.Nil(t, resp)
				assert.Equal(t, tt.wantCode, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.Len(t, resp.Keys, 2)
		})
	}
}

// ========================================
// CopyObject Tests
// ========================================

func TestObjectStorageServer_CopyObject(t *testing.T) {
	tests := []struct {
		name     string
		svc      service.ObjectStorageService
		req      *pb.CopyObjectRequest
		wantCode codes.Code
	}{
		{
			name:     "success",
			svc:      &testMockObjectStorageService{},
			req:      &pb.CopyObjectRequest{SourceKey: "source/obj.txt", DestKey: "dest/obj.txt", TenantId: "tenant-1"},
			wantCode: codes.OK,
		},
		{name: "nil request", svc: &testMockObjectStorageService{}, req: nil, wantCode: codes.InvalidArgument},
		{name: "empty source key", svc: &testMockObjectStorageService{}, req: &pb.CopyObjectRequest{DestKey: "dest/obj.txt"}, wantCode: codes.InvalidArgument},
		{name: "empty dest key", svc: &testMockObjectStorageService{}, req: &pb.CopyObjectRequest{SourceKey: "source/obj.txt"}, wantCode: codes.InvalidArgument},
		{name: "nil service", svc: nil, req: &pb.CopyObjectRequest{SourceKey: "source/obj.txt", DestKey: "dest/obj.txt"}, wantCode: codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &objectStorageServer{service: tt.svc}

			resp, err := srv.CopyObject(context.Background(), tt.req)

			if tt.wantCode != codes.OK {
				assert.Nil(t, resp)
				assert.Equal(t, tt.wantCode, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "dest/obj.txt", resp.Key)
		})
	}
}

// ========================================
// ReplicateObject Tests
// ========================================

func TestObjectStorageServer_ReplicateObject(t *testing.T) {
	partialSvc := &testMockObjectStorageService{
		replicateObjectFunc: func(_ context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
			return &service.ReplicateObjectResponse{
				Key:    req.Key,
				Status: "partial",
				Results: map[string]*service.ReplicationResult{
					"s3":  {Success: true},
					"gcs": {Success: false, Error: "connection timeout"},
				},
			}, nil
		},
	}
	tests := []struct {
		name        string
		svc         service.ObjectStorageService
		req         *pb.ReplicateObjectRequest
		wantCode    codes.Code
		wantSuccess bool
		wantFailed  []string
	}{
		{
			name:        "success",
			svc:         &testMockObjectStorageService{},
			req:         &pb.ReplicateObjectRequest{Key: "test/obj.txt", TargetStores: []string{"s3", "gcs"}, Sync: true},
			wantCode:    codes.OK,
			wantSuccess: true,
		},
		{
			name:       "partial failure",
			svc:        partialSvc,
			req:        &pb.ReplicateObjectRequest{Key: "test/obj.txt", TargetStores: []string{"s3", "gcs"}},
			wantCode:   codes.OK,
			wantFailed: []string{"gcs"},
		},
		{name: "nil request", svc: &testMockObjectStorageService{}, req: nil, wantCode: codes.InvalidArgument},
		{name: "empty key", svc: &testMockObjectStorageService{}, req: &pb.ReplicateObjectRequest{TargetStores: []string{"s3"}}, wantCode: codes.InvalidArgument},
		{name: "no targets", svc: &testMockObjectStorageService{}, req: &pb.ReplicateObjectRequest{Key: "test/obj.txt", TargetStores: []string{}}, wantCode: codes.InvalidArgument},
		{name: "nil service", svc: nil, req: &pb.ReplicateObjectRequest{Key: "test/obj.txt", TargetStores: []string{"s3"}}, wantCode: codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &objectStorageServer{service: tt.svc}

			resp, err := srv.ReplicateObject(context.Background(), tt.req)

			if tt.wantCode != codes.OK {
				assert.Nil(t, resp)
				assert.Equal(t, tt.wantCode, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "test/obj.txt", resp.Key)
			assert.Equal(t, tt.wantSuccess, resp.Success)
			for _, store := range tt.wantFailed {
				assert.Contains(t, resp.FailedStores, store)
			}
		})
	}
}

// ========================================
// MigrateObject Tests
// ========================================

func TestObjectStorageServer_MigrateObject(t *testing.T) {
	tests := []struct {
		name     string
		svc      service.ObjectStorageService
		req      *pb.MigrateObjectRequest
		wantCode codes.Code
	}{
		{
			name:     "success",
			svc:      &testMockObjectStorageService{},
			req:      &pb.MigrateObjectRequest{Key: "test/obj.txt", TargetBackend: "s3", DeleteSource: true},
			wantCode: codes.OK,
		},
		{name: "nil request", svc: &testMockObjectStorageService{}, req: nil, wantCode: codes.InvalidArgument},
		{name: "empty key", svc: &testMockObjectStorageService{}, req: &pb.MigrateObjectRequest{TargetBackend: "s3"}, wantCode: codes.InvalidArgument},
		{name: "empty target backend", svc: &testMockObjectStorageService{}, req: &pb.MigrateObjectRequest{Key: "test/obj.txt"}, wantCode: codes.InvalidArgument},
		{name: "nil service", svc: nil, req: &pb.MigrateObjectRequest{Key: "test/obj.txt", TargetBackend: "s3"}, wantCode: codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &objectStorageServer{service: tt.svc}

			resp, err := srv.MigrateObject(context.Background(), tt.req)

			if tt.wantCode != codes.OK {
				assert.Nil(t, resp)
				assert.Equal(t, tt.wantCode, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.True(t, resp.Success)
			assert.Equal(t, "s3", resp.NewBackend)
			assert.True(t, resp.SourceDeleted)
		})
	}
}

// ========================================
// ArchiveObject Tests
// ========================================

func TestObjectStorageServer_ArchiveObject(t *testing.T) {
	tests := []struct {
		name     string
		svc      service.ObjectStorageService
		req      *pb.ArchiveObjectRequest
		wantCode codes.Code
	}{
		{
			name:     "success",
			svc:      &testMockObjectStorageService{},
			req:      &pb.ArchiveObjectRequest{Key: "test/obj.txt", TargetStore: "glacier", DeleteFromPrimary: true},
			wantCode: codes.OK,
		},
		{name: "nil request", svc: &testMockObjectStorageService{}, req: nil, wantCode: codes.InvalidArgument},
		{name: "empty key", svc: &testMockObjectStorageService{}, req: &pb.ArchiveObjectRequest{TargetStore: "glacier"}, wantCode: codes.InvalidArgument},
		{name: "nil service", svc: nil, req: &pb.ArchiveObjectRequest{Key: "test/obj.txt", TargetStore: "glacier"}, wantCode: codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &objectStorageServer{service: tt.svc}

			resp, err := srv.ArchiveObject(context.Background(), tt.req)

			if tt.wantCode != codes.OK {
				assert.Nil(t, resp)
				assert.Equal(t, tt.wantCode, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.True(t, resp.Success)
			assert.Equal(t, "glacier", resp.ArchiveStore)
		})
	}
}

// ========================================
// RestoreObject Tests
// ========================================

func TestObjectStorageServer_RestoreObject(t *testing.T) {
	tests := []struct {
		name     string
		svc      service.ObjectStorageService
		req      *pb.RestoreObjectRequest
		wantCode codes.Code
	}{
		{
			name:     "success",
			svc:      &testMockObjectStorageService{},
			req:      &pb.RestoreObjectRequest{Key: "test/obj.txt", TargetBackend: "local"},
			wantCode: codes.OK,
		},
		{name: "nil request", svc: &testMockObjectStorageService{}, req: nil, wantCode: codes.InvalidArgument},
		{name: "empty key", svc: &testMockObjectStorageService{}, req: &pb.RestoreObjectRequest{TargetBackend: "local"}, wantCode: codes.InvalidArgument},
		{name: "nil service", svc: nil, req: &pb.RestoreObjectRequest{Key: "test/obj.txt", TargetBackend: "local"}, wantCode: codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &objectStorageServer{service: tt.svc}

			resp, err := srv.RestoreObject(context.Background(), tt.req)

			if tt.wantCode != codes.OK {
				assert.Nil(t, resp)
				assert.Equal(t, tt.wantCode, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.True(t, resp.Success)
			assert.Equal(t, "local", resp.RestoredTo)
		})
	}
}

// ========================================
// VerifyObject Tests
// ========================================

func TestObjectStorageServer_VerifyObject_Success(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.VerifyObject(context.Background(), &pb.VerifyObjectRequest{
		Key:    "test/obj.txt",
		Repair: false,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Valid)
	assert.Len(t, resp.Locations, 2)
}

func TestObjectStorageServer_VerifyObject_WithRepair(t *testing.T) {
	mockSvc := &testMockObjectStorageService{
		verifyObjectFunc: func(_ context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
			return &service.VerifyObjectResponse{
				Key:   req.Key,
				Valid: false,
				Results: map[string]*service.VerificationResult{
					"local": {Store: "local", Valid: true},
					"s3":    {Store: "s3", Valid: false, Error: "checksum mismatch"},
				},
				Repaired: req.RepairMismatches,
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.VerifyObject(context.Background(), &pb.VerifyObjectRequest{
		Key:    "test/obj.txt",
		Repair: true,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Valid)
	assert.True(t, resp.Repaired)
}

func TestObjectStorageServer_VerifyObject_NilRequest(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.VerifyObject(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_VerifyObject_EmptyKey(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.VerifyObject(context.Background(), &pb.VerifyObjectRequest{Key: ""})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_VerifyObject_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}

	resp, err := srv.VerifyObject(context.Background(), &pb.VerifyObjectRequest{Key: "test/obj.txt"})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// ========================================
// SetObjectPolicy Tests
// ========================================

func TestObjectStorageServer_SetObjectPolicy_Success(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.SetObjectPolicy(context.Background(), &pb.SetObjectPolicyRequest{
		Key: "test/obj.txt",
		ReplicationPolicy: &pb.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 3,
		},
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test/obj.txt", resp.Key)
}

func TestObjectStorageServer_SetObjectPolicy_NilRequest(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.SetObjectPolicy(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_SetObjectPolicy_EmptyKey(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.SetObjectPolicy(context.Background(), &pb.SetObjectPolicyRequest{Key: ""})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_SetObjectPolicy_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}

	resp, err := srv.SetObjectPolicy(context.Background(), &pb.SetObjectPolicyRequest{Key: "test/obj.txt"})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// ========================================
// GetObjectPolicy Tests
// ========================================

func TestObjectStorageServer_GetObjectPolicy_Success(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectPolicy(context.Background(), &pb.GetObjectPolicyRequest{
		Key: "test/obj.txt",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test/obj.txt", resp.Key)
	assert.NotNil(t, resp.Policy)
	assert.NotNil(t, resp.Policy.ReplicationPolicy)
}

func TestObjectStorageServer_GetObjectPolicy_NilRequest(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectPolicy(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_GetObjectPolicy_EmptyKey(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectPolicy(context.Background(), &pb.GetObjectPolicyRequest{Key: ""})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_GetObjectPolicy_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}

	resp, err := srv.GetObjectPolicy(context.Background(), &pb.GetObjectPolicyRequest{Key: "test/obj.txt"})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// ========================================
// ListBackends Tests
// ========================================

func TestObjectStorageServer_ListBackends_Success(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.ListBackends(context.Background(), &pb.ListBackendsRequest{
		IncludeHealth: true,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Backends, 2)
}

func TestObjectStorageServer_ListBackends_NilRequest(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.ListBackends(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_ListBackends_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}

	resp, err := srv.ListBackends(context.Background(), &pb.ListBackendsRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// ========================================
// GetBackendHealth Tests
// ========================================

func TestObjectStorageServer_GetBackendHealth_Success(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetBackendHealth(context.Background(), &pb.GetBackendHealthRequest{
		Name: "local",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Healthy)
	assert.Equal(t, "local", resp.Name)
}

func TestObjectStorageServer_GetBackendHealth_NilRequest(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetBackendHealth(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_GetBackendHealth_EmptyName(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetBackendHealth(context.Background(), &pb.GetBackendHealthRequest{Name: ""})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_GetBackendHealth_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}

	resp, err := srv.GetBackendHealth(context.Background(), &pb.GetBackendHealthRequest{Name: "local"})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// ========================================
// GetObjectStorageTenantUsage Tests
// ========================================

func TestObjectStorageServer_GetTenantUsage_NilRequest(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectStorageTenantUsage(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_GetTenantUsage_EmptyTenantID(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectStorageTenantUsage(context.Background(), &pb.GetObjectStorageTenantUsageRequest{TenantId: ""})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_GetTenantUsage_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}

	resp, err := srv.GetObjectStorageTenantUsage(context.Background(), &pb.GetObjectStorageTenantUsageRequest{TenantId: "tenant-1"})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// ========================================
// GetObjectStorageTenantQuotas Tests
// ========================================

func TestObjectStorageServer_GetTenantQuotas_NilRequest(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectStorageTenantQuotas(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_GetTenantQuotas_EmptyTenantID(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectStorageTenantQuotas(context.Background(), &pb.GetObjectStorageTenantQuotasRequest{TenantId: ""})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_GetTenantQuotas_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}

	resp, err := srv.GetObjectStorageTenantQuotas(context.Background(), &pb.GetObjectStorageTenantQuotasRequest{TenantId: "tenant-1"})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// ========================================
// SetObjectStorageTenantQuotas Tests
// ========================================

func TestObjectStorageServer_SetTenantQuotas_NilRequest(t *testing.T) {
	mockSvc := &testMockObjectStorageService{}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.SetObjectStorageTenantQuotas(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestObjectStorageServer_SetTenantQuotas_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}

	resp, err := srv.SetObjectStorageTenantQuotas(context.Background(), &pb.SetObjectStorageTenantQuotasRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}
