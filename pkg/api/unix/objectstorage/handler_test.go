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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// testLogger implements interfaces.Logger for testing.
type testLogger struct{}

func (l *testLogger) Debug(msg string, args ...any)             {}
func (l *testLogger) Debugf(format string, args ...interface{}) {}
func (l *testLogger) Info(msg string, args ...any)              {}
func (l *testLogger) Infof(format string, args ...interface{})  {}
func (l *testLogger) Warn(msg string, args ...any)              {}
func (l *testLogger) Warnf(format string, args ...interface{})  {}
func (l *testLogger) Error(msg string, args ...any)             {}
func (l *testLogger) Errorf(format string, args ...interface{}) {}
func (l *testLogger) Fatal(msg string)                          {}
func (l *testLogger) Fatalf(format string, args ...interface{}) {}
func (l *testLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithError(err error) interfaces.Logger {
	return l
}

// mockObjectStorageService is a mock implementation for testing.
type mockObjectStorageService struct {
	putObjectFunc          func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error)
	getObjectFunc          func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error)
	deleteObjectFunc       func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error)
	headObjectFunc         func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error)
	listObjectsFunc        func(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error)
	listByTagFunc          func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error)
	listByTenantFunc       func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error)
	copyObjectFunc         func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error)
	replicateObjectFunc    func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error)
	migrateObjectFunc      func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error)
	archiveObjectFunc      func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error)
	restoreObjectFunc      func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error)
	verifyObjectFunc       func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error)
	setObjectPolicyFunc    func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error)
	getObjectPolicyFunc    func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error)
	listBackendsFunc       func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error)
	getBackendHealthFunc   func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error)
	getTenantUsageFunc     func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error)
	getTenantQuotasFunc    func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error)
	setTenantQuotasFunc    func(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error)
	batchPutObjectsFunc    func(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error)
	batchGetObjectsFunc    func(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error)
	batchDeleteObjectsFunc func(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error)
}

func (m *mockObjectStorageService) PutObject(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	if m.putObjectFunc != nil {
		return m.putObjectFunc(ctx, req)
	}
	return &service.PutObjectResponse{Key: req.Key, ObjectID: 1, Size: int64(len(req.Value))}, nil
}

func (m *mockObjectStorageService) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	if m.getObjectFunc != nil {
		return m.getObjectFunc(ctx, req)
	}
	return &service.GetObjectResponse{Key: req.Key, Value: []byte("test-value")}, nil
}

func (m *mockObjectStorageService) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	if m.deleteObjectFunc != nil {
		return m.deleteObjectFunc(ctx, req)
	}
	return &service.DeleteObjectResponse{Key: req.Key, Success: true}, nil
}

func (m *mockObjectStorageService) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	if m.headObjectFunc != nil {
		return m.headObjectFunc(ctx, req)
	}
	return &service.HeadObjectResponse{Key: req.Key, Exists: true, Size: 100}, nil
}

func (m *mockObjectStorageService) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	if m.listObjectsFunc != nil {
		return m.listObjectsFunc(ctx, req)
	}
	return &service.ListObjectsResponse{Objects: []*service.ObjectSummary{}}, nil
}

func (m *mockObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	if m.listByTagFunc != nil {
		return m.listByTagFunc(ctx, req)
	}
	return &service.ListByTagResponse{Objects: []*service.ObjectSummary{}}, nil
}

func (m *mockObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	if m.listByTenantFunc != nil {
		return m.listByTenantFunc(ctx, req)
	}
	return &service.ListByTenantResponse{Objects: []*service.ObjectSummary{}}, nil
}

func (m *mockObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	if m.copyObjectFunc != nil {
		return m.copyObjectFunc(ctx, req)
	}
	return &service.CopyObjectResponse{Key: req.DestinationKey}, nil
}

func (m *mockObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	if m.replicateObjectFunc != nil {
		return m.replicateObjectFunc(ctx, req)
	}
	return &service.ReplicateObjectResponse{Key: req.Key, Status: "completed"}, nil
}

func (m *mockObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	if m.migrateObjectFunc != nil {
		return m.migrateObjectFunc(ctx, req)
	}
	return &service.MigrateObjectResponse{Key: req.Key, Success: true}, nil
}

func (m *mockObjectStorageService) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	if m.archiveObjectFunc != nil {
		return m.archiveObjectFunc(ctx, req)
	}
	return &service.ArchiveObjectResponse{Key: req.Key, Success: true}, nil
}

func (m *mockObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	if m.restoreObjectFunc != nil {
		return m.restoreObjectFunc(ctx, req)
	}
	return &service.RestoreObjectResponse{Key: req.Key, Success: true}, nil
}

func (m *mockObjectStorageService) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	if m.verifyObjectFunc != nil {
		return m.verifyObjectFunc(ctx, req)
	}
	return &service.VerifyObjectResponse{Key: req.Key, Valid: true}, nil
}

func (m *mockObjectStorageService) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	if m.setObjectPolicyFunc != nil {
		return m.setObjectPolicyFunc(ctx, req)
	}
	return &service.SetObjectPolicyResponse{Key: req.Key, Success: true}, nil
}

func (m *mockObjectStorageService) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	if m.getObjectPolicyFunc != nil {
		return m.getObjectPolicyFunc(ctx, req)
	}
	return &service.GetObjectPolicyResponse{Key: req.Key}, nil
}

func (m *mockObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc(ctx, req)
	}
	return &service.ListBackendsResponse{Backends: []*service.BackendInfo{}}, nil
}

func (m *mockObjectStorageService) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	if m.getBackendHealthFunc != nil {
		return m.getBackendHealthFunc(ctx, req)
	}
	return &service.GetBackendHealthResponse{Name: req.Name, Healthy: true}, nil
}

func (m *mockObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	if m.getTenantUsageFunc != nil {
		return m.getTenantUsageFunc(ctx, req)
	}
	return &service.GetTenantUsageResponse{TenantID: req.TenantID, ObjectCount: 10}, nil
}

func (m *mockObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	if m.getTenantQuotasFunc != nil {
		return m.getTenantQuotasFunc(ctx, req)
	}
	return &service.GetTenantQuotasResponse{TenantID: req.TenantID, MaxStorageBytes: 1024 * 1024 * 1024}, nil
}

func (m *mockObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	if m.setTenantQuotasFunc != nil {
		return m.setTenantQuotasFunc(ctx, req)
	}
	return &service.SetTenantQuotasResponse{TenantID: req.TenantID}, nil
}

func (m *mockObjectStorageService) BatchPutObjects(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	if m.batchPutObjectsFunc != nil {
		return m.batchPutObjectsFunc(ctx, req)
	}
	results := make([]*service.BatchPutResult, len(req.Objects))
	for i, obj := range req.Objects {
		results[i] = &service.BatchPutResult{
			Key:      obj.Key,
			Success:  true,
			ObjectID: uint64(i + 1),
		}
	}
	return &service.BatchPutObjectsResponse{
		Results:      results,
		SuccessCount: len(req.Objects),
		FailureCount: 0,
	}, nil
}

func (m *mockObjectStorageService) BatchGetObjects(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	if m.batchGetObjectsFunc != nil {
		return m.batchGetObjectsFunc(ctx, req)
	}
	results := make([]*service.BatchGetResult, len(req.Keys))
	for i, key := range req.Keys {
		results[i] = &service.BatchGetResult{
			Key:   key,
			Found: true,
			Value: []byte("test-value"),
			Size:  10,
		}
	}
	return &service.BatchGetObjectsResponse{
		Results:       results,
		FoundCount:    len(req.Keys),
		NotFoundCount: 0,
	}, nil
}

func (m *mockObjectStorageService) BatchDeleteObjects(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	if m.batchDeleteObjectsFunc != nil {
		return m.batchDeleteObjectsFunc(ctx, req)
	}
	results := make([]*service.BatchDeleteResult, len(req.Keys))
	for i, key := range req.Keys {
		results[i] = &service.BatchDeleteResult{
			Key:     key,
			Success: true,
			Size:    100,
		}
	}
	return &service.BatchDeleteObjectsResponse{
		Results:      results,
		DeletedCount: len(req.Keys),
		FailureCount: 0,
	}, nil
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name    string
		service service.ObjectStorageService
		logger  interfaces.Logger
		wantErr bool
	}{
		{name: "nil service", service: nil, logger: &testLogger{}, wantErr: true},
		{name: "nil logger", service: &mockObjectStorageService{}, logger: nil, wantErr: true},
		{name: "success", service: &mockObjectStorageService{}, logger: &testLogger{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.service, nil, tt.logger)
			if tt.wantErr {
				require.Error(t, err)
				var qbErr *dberrors.QBError
				require.True(t, dberrors.As(err, &qbErr))
				assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, handler)
			assert.NotNil(t, handler.Service())
			assert.Nil(t, handler.AdminService())
		})
	}
}

func TestHandlerUnknownOpcode(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), 0xFF, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestHandlerClosed(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	err = handler.Close()
	require.NoError(t, err)

	// Second close should be idempotent
	err = handler.Close()
	require.NoError(t, err)

	// Handle should fail when closed
	_, err = handler.Handle(context.Background(), OpGetObject, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

// TestHandleOpcodes drives each opcode through Handle with an encoded
// request and decodes the response into the opcode's response type.
func TestHandleOpcodes(t *testing.T) {
	tests := []struct {
		name  string
		op    byte
		svc   *mockObjectStorageService
		req   any
		resp  any
		check func(t *testing.T, resp any)
	}{
		{
			name: "put object",
			op:   OpPutObject,
			svc: &mockObjectStorageService{
				putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
					return &service.PutObjectResponse{Key: req.Key, ObjectID: 123, Size: int64(len(req.Value))}, nil
				},
			},
			req:  &service.PutObjectRequest{Key: "test-key", Value: []byte("test-value")},
			resp: &service.PutObjectResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.PutObjectResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.Equal(t, uint64(123), r.ObjectID)
			},
		},
		{
			name: "get object",
			op:   OpGetObject,
			svc: &mockObjectStorageService{
				getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
					return &service.GetObjectResponse{Key: req.Key, Value: []byte("retrieved-value")}, nil
				},
			},
			req:  &service.GetObjectRequest{Key: "test-key"},
			resp: &service.GetObjectResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.GetObjectResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.Equal(t, []byte("retrieved-value"), r.Value)
			},
		},
		{
			name: "delete object",
			op:   OpDeleteObject,
			svc: &mockObjectStorageService{
				deleteObjectFunc: func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
					return &service.DeleteObjectResponse{Key: req.Key, Success: true, Size: 100}, nil
				},
			},
			req:  &service.DeleteObjectRequest{Key: "test-key"},
			resp: &service.DeleteObjectResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.DeleteObjectResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.True(t, r.Success)
			},
		},
		{
			name: "head object",
			op:   OpHeadObject,
			svc: &mockObjectStorageService{
				headObjectFunc: func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
					return &service.HeadObjectResponse{Key: req.Key, Exists: true, Size: 256}, nil
				},
			},
			req:  &service.HeadObjectRequest{Key: "test-key"},
			resp: &service.HeadObjectResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.HeadObjectResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.True(t, r.Exists)
				assert.Equal(t, int64(256), r.Size)
			},
		},
		{
			name: "list objects",
			op:   OpListObjects,
			req:  &service.ListObjectsRequest{Prefix: "test/", Limit: 10},
			resp: &service.ListObjectsResponse{},
		},
		{
			name: "list by tag",
			op:   OpListByTag,
			req:  &service.ListByTagRequest{TagName: "env", TagValue: "prod"},
			resp: &service.ListByTagResponse{},
		},
		{
			name: "list by tenant",
			op:   OpListByTenant,
			req:  &service.ListByTenantRequest{TenantID: "tenant-1"},
			resp: &service.ListByTenantResponse{},
		},
		{
			name: "copy object",
			op:   OpCopyObject,
			req:  &service.CopyObjectRequest{SourceKey: "source-key", DestinationKey: "dest-key"},
			resp: &service.CopyObjectResponse{},
			check: func(t *testing.T, resp any) {
				assert.Equal(t, "dest-key", resp.(*service.CopyObjectResponse).Key)
			},
		},
		{
			name: "replicate object",
			op:   OpReplicateObject,
			req:  &service.ReplicateObjectRequest{Key: "test-key", Targets: []string{"backend-1", "backend-2"}},
			resp: &service.ReplicateObjectResponse{},
			check: func(t *testing.T, resp any) {
				assert.Equal(t, "test-key", resp.(*service.ReplicateObjectResponse).Key)
			},
		},
		{
			name: "migrate object",
			op:   OpMigrateObject,
			req:  &service.MigrateObjectRequest{Key: "test-key", TargetBackend: "backend-2"},
			resp: &service.MigrateObjectResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.MigrateObjectResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.True(t, r.Success)
			},
		},
		{
			name: "archive object",
			op:   OpArchiveObject,
			req:  &service.ArchiveObjectRequest{Key: "test-key"},
			resp: &service.ArchiveObjectResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.ArchiveObjectResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.True(t, r.Success)
			},
		},
		{
			name: "restore object",
			op:   OpRestoreObject,
			req:  &service.RestoreObjectRequest{Key: "test-key"},
			resp: &service.RestoreObjectResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.RestoreObjectResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.True(t, r.Success)
			},
		},
		{
			name: "verify object",
			op:   OpVerifyObject,
			req:  &service.VerifyObjectRequest{Key: "test-key"},
			resp: &service.VerifyObjectResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.VerifyObjectResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.True(t, r.Valid)
			},
		},
		{
			name: "set object policy",
			op:   OpSetObjectPolicy,
			req:  &service.SetObjectPolicyRequest{Key: "test-key"},
			resp: &service.SetObjectPolicyResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.SetObjectPolicyResponse)
				assert.Equal(t, "test-key", r.Key)
				assert.True(t, r.Success)
			},
		},
		{
			name: "get object policy",
			op:   OpGetObjectPolicy,
			req:  &service.GetObjectPolicyRequest{Key: "test-key"},
			resp: &service.GetObjectPolicyResponse{},
			check: func(t *testing.T, resp any) {
				assert.Equal(t, "test-key", resp.(*service.GetObjectPolicyResponse).Key)
			},
		},
		{
			name: "list backends",
			op:   OpListBackends,
			req:  &service.ListBackendsRequest{IncludeHealth: true},
			resp: &service.ListBackendsResponse{},
		},
		{
			name: "get backend health",
			op:   OpGetBackendHealth,
			req:  &service.GetBackendHealthRequest{Name: "backend-1"},
			resp: &service.GetBackendHealthResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*service.GetBackendHealthResponse)
				assert.Equal(t, "backend-1", r.Name)
				assert.True(t, r.Healthy)
			},
		},
		{
			name: "get tenant usage",
			op:   OpGetTenantUsage,
			req:  &service.GetTenantUsageRequest{TenantID: "tenant-1"},
			resp: &service.GetTenantUsageResponse{},
			check: func(t *testing.T, resp any) {
				assert.Equal(t, "tenant-1", resp.(*service.GetTenantUsageResponse).TenantID)
			},
		},
		{
			name: "get tenant quotas",
			op:   OpGetTenantQuotas,
			req:  &service.GetTenantQuotasRequest{TenantID: "tenant-1"},
			resp: &service.GetTenantQuotasResponse{},
			check: func(t *testing.T, resp any) {
				assert.Equal(t, "tenant-1", resp.(*service.GetTenantQuotasResponse).TenantID)
			},
		},
		{
			name: "set tenant quotas",
			op:   OpSetTenantQuotas,
			req:  &service.SetTenantQuotasRequest{TenantID: "tenant-1", MaxStorageBytes: 1024 * 1024 * 1024},
			resp: &service.SetTenantQuotasResponse{},
			check: func(t *testing.T, resp any) {
				assert.Equal(t, "tenant-1", resp.(*service.SetTenantQuotasResponse).TenantID)
			},
		},
		{
			name: "batch put objects",
			op:   OpBatchPutObjects,
			req: &BatchPutRequest{Objects: []*PutObjectItem{
				{Key: "key1", Value: []byte("value1")},
				{Key: "key2", Value: []byte("value2")},
			}},
			resp: &BatchPutResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*BatchPutResponse)
				assert.Equal(t, 2, r.Succeeded)
				assert.Equal(t, 0, r.Failed)
				assert.Len(t, r.Results, 2)
			},
		},
		{
			name: "batch get objects",
			op:   OpBatchGetObjects,
			req:  &BatchGetRequest{Keys: []string{"key1", "key2"}},
			resp: &BatchGetResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*BatchGetResponse)
				assert.Equal(t, 2, r.Succeeded)
				assert.Equal(t, 0, r.Failed)
				assert.Len(t, r.Results, 2)
			},
		},
		{
			name: "batch delete objects",
			op:   OpBatchDeleteObjects,
			req:  &BatchDeleteRequest{Keys: []string{"key1", "key2"}},
			resp: &BatchDeleteResponse{},
			check: func(t *testing.T, resp any) {
				r := resp.(*BatchDeleteResponse)
				assert.Equal(t, 2, r.Succeeded)
				assert.Equal(t, 0, r.Failed)
				assert.Len(t, r.Results, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.svc
			if svc == nil {
				svc = &mockObjectStorageService{}
			}
			handler, err := NewHandler(svc, nil, &testLogger{})
			require.NoError(t, err)

			reqPayload, err := EncodePayload(tt.req)
			require.NoError(t, err)

			resp, err := handler.Handle(context.Background(), tt.op, reqPayload)
			require.NoError(t, err)
			require.NoError(t, DecodePayload(resp, tt.resp))

			if tt.check != nil {
				tt.check(t, tt.resp)
			}
		})
	}
}

func TestIsAdminOperation(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected bool
	}{
		{OpPutObject, false},
		{OpGetObject, false},
		{OpListObjects, false},
		{OpAdminCreateBackend, true},
		{OpAdminUpdateBackend, true},
		{OpAdminDeleteBackend, true},
		{OpAdminSetBackendStatus, true},
		{OpAdminGetStorageStats, true},
		{OpAdminRunGC, true},
		{OpAdminGetGCProgress, true},
		{OpAdminRepairObject, true},
		{OpAdminCompactBackend, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToString(tt.opcode), func(t *testing.T) {
			assert.Equal(t, tt.expected, IsAdminOperation(tt.opcode))
			assert.Equal(t, tt.expected, RequiresAdminService(tt.opcode))
		})
	}
}

func TestHandleBatchWithErrors(t *testing.T) {
	svc := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			if req.Key == "missing-key" {
				return nil, &dberrors.QBError{
					Code: dberrors.ErrNotFound,
					Op:   "test",
					Err:  assert.AnError,
				}
			}
			return &service.GetObjectResponse{Key: req.Key, Value: []byte("value")}, nil
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&BatchGetRequest{
		Keys: []string{"existing-key", "missing-key"},
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpBatchGetObjects, reqPayload)
	require.NoError(t, err)

	var batchResp BatchGetResponse
	err = DecodePayload(resp, &batchResp)
	require.NoError(t, err)
	assert.Equal(t, 1, batchResp.Succeeded)
	assert.Equal(t, 1, batchResp.Failed)
}

// TestHandleRequest checks that HandleRequest maps a request, and the
// error its opcode handler returns, to the response status.
func TestHandleRequest(t *testing.T) {
	notFound := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test", Err: assert.AnError}
		},
	}

	getPayload, err := EncodePayload(&service.GetObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	getRequest := &Request{Opcode: OpGetObject, Token: "test-token", Payload: getPayload}

	tests := []struct {
		name        string
		svc         *mockObjectStorageService
		req         *Request
		wantStatus  byte
		wantPayload bool
	}{
		{name: "nil request", svc: &mockObjectStorageService{}, req: nil, wantStatus: StatusInvalidRequest},
		{name: "success", svc: &mockObjectStorageService{}, req: getRequest, wantStatus: StatusOK, wantPayload: true},
		{name: "service error", svc: notFound, req: getRequest, wantStatus: StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, nil, &testLogger{})
			require.NoError(t, err)

			resp := handler.HandleRequest(context.Background(), tt.req)
			assert.Equal(t, tt.wantStatus, resp.Status)
			if tt.wantPayload {
				assert.NotEmpty(t, resp.Payload)
			}
		})
	}
}
