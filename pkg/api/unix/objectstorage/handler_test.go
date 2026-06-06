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

func TestNewHandlerNilService(t *testing.T) {
	_, err := NewHandler(nil, nil, &testLogger{})
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestNewHandlerNilLogger(t *testing.T) {
	_, err := NewHandler(&mockObjectStorageService{}, nil, nil)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestNewHandlerSuccess(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)
	require.NotNil(t, handler)

	assert.NotNil(t, handler.Service())
	assert.Nil(t, handler.AdminService())
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

func TestHandleRequestNil(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	resp := handler.HandleRequest(context.Background(), nil)
	assert.Equal(t, StatusInvalidRequest, resp.Status)
}

func TestHandlePutObject(t *testing.T) {
	svc := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			return &service.PutObjectResponse{
				Key:      req.Key,
				ObjectID: 123,
				Size:     int64(len(req.Value)),
			}, nil
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.PutObjectRequest{
		Key:   "test-key",
		Value: []byte("test-value"),
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpPutObject, reqPayload)
	require.NoError(t, err)

	var putResp service.PutObjectResponse
	err = DecodePayload(resp, &putResp)
	require.NoError(t, err)

	assert.Equal(t, "test-key", putResp.Key)
	assert.Equal(t, uint64(123), putResp.ObjectID)
}

func TestHandleGetObject(t *testing.T) {
	svc := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return &service.GetObjectResponse{
				Key:   req.Key,
				Value: []byte("retrieved-value"),
			}, nil
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetObjectRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpGetObject, reqPayload)
	require.NoError(t, err)

	var getResp service.GetObjectResponse
	err = DecodePayload(resp, &getResp)
	require.NoError(t, err)

	assert.Equal(t, "test-key", getResp.Key)
	assert.Equal(t, []byte("retrieved-value"), getResp.Value)
}

func TestHandleDeleteObject(t *testing.T) {
	svc := &mockObjectStorageService{
		deleteObjectFunc: func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
			return &service.DeleteObjectResponse{
				Key:     req.Key,
				Success: true,
				Size:    100,
			}, nil
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.DeleteObjectRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpDeleteObject, reqPayload)
	require.NoError(t, err)

	var delResp service.DeleteObjectResponse
	err = DecodePayload(resp, &delResp)
	require.NoError(t, err)

	assert.Equal(t, "test-key", delResp.Key)
	assert.True(t, delResp.Success)
}

func TestHandleHeadObject(t *testing.T) {
	svc := &mockObjectStorageService{
		headObjectFunc: func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
			return &service.HeadObjectResponse{
				Key:    req.Key,
				Exists: true,
				Size:   256,
			}, nil
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.HeadObjectRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpHeadObject, reqPayload)
	require.NoError(t, err)

	var headResp service.HeadObjectResponse
	err = DecodePayload(resp, &headResp)
	require.NoError(t, err)

	assert.Equal(t, "test-key", headResp.Key)
	assert.True(t, headResp.Exists)
	assert.Equal(t, int64(256), headResp.Size)
}

func TestHandleListObjects(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ListObjectsRequest{
		Prefix: "test/",
		Limit:  10,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpListObjects, reqPayload)
	require.NoError(t, err)

	var listResp service.ListObjectsResponse
	err = DecodePayload(resp, &listResp)
	require.NoError(t, err)
}

func TestHandleListByTag(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ListByTagRequest{
		TagName:  "env",
		TagValue: "prod",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpListByTag, reqPayload)
	require.NoError(t, err)

	var listResp service.ListByTagResponse
	err = DecodePayload(resp, &listResp)
	require.NoError(t, err)
}

func TestHandleListByTenant(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ListByTenantRequest{
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpListByTenant, reqPayload)
	require.NoError(t, err)

	var listResp service.ListByTenantResponse
	err = DecodePayload(resp, &listResp)
	require.NoError(t, err)
}

func TestHandleCopyObject(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.CopyObjectRequest{
		SourceKey:      "source-key",
		DestinationKey: "dest-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpCopyObject, reqPayload)
	require.NoError(t, err)

	var copyResp service.CopyObjectResponse
	err = DecodePayload(resp, &copyResp)
	require.NoError(t, err)
	assert.Equal(t, "dest-key", copyResp.Key)
}

func TestHandleReplicateObject(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ReplicateObjectRequest{
		Key:     "test-key",
		Targets: []string{"backend-1", "backend-2"},
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpReplicateObject, reqPayload)
	require.NoError(t, err)

	var replResp service.ReplicateObjectResponse
	err = DecodePayload(resp, &replResp)
	require.NoError(t, err)
	assert.Equal(t, "test-key", replResp.Key)
}

func TestHandleMigrateObject(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.MigrateObjectRequest{
		Key:           "test-key",
		TargetBackend: "backend-2",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpMigrateObject, reqPayload)
	require.NoError(t, err)

	var migrResp service.MigrateObjectResponse
	err = DecodePayload(resp, &migrResp)
	require.NoError(t, err)
	assert.Equal(t, "test-key", migrResp.Key)
	assert.True(t, migrResp.Success)
}

func TestHandleArchiveObject(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ArchiveObjectRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpArchiveObject, reqPayload)
	require.NoError(t, err)

	var archResp service.ArchiveObjectResponse
	err = DecodePayload(resp, &archResp)
	require.NoError(t, err)
	assert.Equal(t, "test-key", archResp.Key)
	assert.True(t, archResp.Success)
}

func TestHandleRestoreObject(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.RestoreObjectRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpRestoreObject, reqPayload)
	require.NoError(t, err)

	var restResp service.RestoreObjectResponse
	err = DecodePayload(resp, &restResp)
	require.NoError(t, err)
	assert.Equal(t, "test-key", restResp.Key)
	assert.True(t, restResp.Success)
}

func TestHandleVerifyObject(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.VerifyObjectRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpVerifyObject, reqPayload)
	require.NoError(t, err)

	var verResp service.VerifyObjectResponse
	err = DecodePayload(resp, &verResp)
	require.NoError(t, err)
	assert.Equal(t, "test-key", verResp.Key)
	assert.True(t, verResp.Valid)
}

func TestHandleSetObjectPolicy(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.SetObjectPolicyRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSetObjectPolicy, reqPayload)
	require.NoError(t, err)

	var polResp service.SetObjectPolicyResponse
	err = DecodePayload(resp, &polResp)
	require.NoError(t, err)
	assert.Equal(t, "test-key", polResp.Key)
	assert.True(t, polResp.Success)
}

func TestHandleGetObjectPolicy(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetObjectPolicyRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpGetObjectPolicy, reqPayload)
	require.NoError(t, err)

	var polResp service.GetObjectPolicyResponse
	err = DecodePayload(resp, &polResp)
	require.NoError(t, err)
	assert.Equal(t, "test-key", polResp.Key)
}

func TestHandleListBackends(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.ListBackendsRequest{
		IncludeHealth: true,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpListBackends, reqPayload)
	require.NoError(t, err)

	var listResp service.ListBackendsResponse
	err = DecodePayload(resp, &listResp)
	require.NoError(t, err)
}

func TestHandleGetBackendHealth(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetBackendHealthRequest{
		Name: "backend-1",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpGetBackendHealth, reqPayload)
	require.NoError(t, err)

	var healthResp service.GetBackendHealthResponse
	err = DecodePayload(resp, &healthResp)
	require.NoError(t, err)
	assert.Equal(t, "backend-1", healthResp.Name)
	assert.True(t, healthResp.Healthy)
}

func TestHandleGetTenantUsage(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetTenantUsageRequest{
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpGetTenantUsage, reqPayload)
	require.NoError(t, err)

	var usageResp service.GetTenantUsageResponse
	err = DecodePayload(resp, &usageResp)
	require.NoError(t, err)
	assert.Equal(t, "tenant-1", usageResp.TenantID)
}

func TestHandleGetTenantQuotas(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetTenantQuotasRequest{
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpGetTenantQuotas, reqPayload)
	require.NoError(t, err)

	var quotaResp service.GetTenantQuotasResponse
	err = DecodePayload(resp, &quotaResp)
	require.NoError(t, err)
	assert.Equal(t, "tenant-1", quotaResp.TenantID)
}

func TestHandleSetTenantQuotas(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.SetTenantQuotasRequest{
		TenantID:        "tenant-1",
		MaxStorageBytes: 1024 * 1024 * 1024,
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpSetTenantQuotas, reqPayload)
	require.NoError(t, err)

	var quotaResp service.SetTenantQuotasResponse
	err = DecodePayload(resp, &quotaResp)
	require.NoError(t, err)
	assert.Equal(t, "tenant-1", quotaResp.TenantID)
}

func TestHandleServiceError(t *testing.T) {
	svc := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &dberrors.QBError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetObjectRequest{
		Key: "missing-key",
	})
	require.NoError(t, err)

	_, err = handler.Handle(context.Background(), OpGetObject, reqPayload)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrNotFound, qbErr.Code)
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
			result := IsAdminOperation(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRequiresAdminService(t *testing.T) {
	assert.False(t, RequiresAdminService(OpPutObject))
	assert.False(t, RequiresAdminService(OpGetObject))
	assert.True(t, RequiresAdminService(OpAdminCreateBackend))
	assert.True(t, RequiresAdminService(OpAdminRunGC))
}

func TestHandleBatchPutObjects(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&BatchPutRequest{
		Objects: []*PutObjectItem{
			{Key: "key1", Value: []byte("value1")},
			{Key: "key2", Value: []byte("value2")},
		},
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpBatchPutObjects, reqPayload)
	require.NoError(t, err)

	var batchResp BatchPutResponse
	err = DecodePayload(resp, &batchResp)
	require.NoError(t, err)
	assert.Equal(t, 2, batchResp.Succeeded)
	assert.Equal(t, 0, batchResp.Failed)
	assert.Len(t, batchResp.Results, 2)
}

func TestHandleBatchGetObjects(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&BatchGetRequest{
		Keys: []string{"key1", "key2"},
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpBatchGetObjects, reqPayload)
	require.NoError(t, err)

	var batchResp BatchGetResponse
	err = DecodePayload(resp, &batchResp)
	require.NoError(t, err)
	assert.Equal(t, 2, batchResp.Succeeded)
	assert.Equal(t, 0, batchResp.Failed)
	assert.Len(t, batchResp.Results, 2)
}

func TestHandleBatchDeleteObjects(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&BatchDeleteRequest{
		Keys: []string{"key1", "key2"},
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpBatchDeleteObjects, reqPayload)
	require.NoError(t, err)

	var batchResp BatchDeleteResponse
	err = DecodePayload(resp, &batchResp)
	require.NoError(t, err)
	assert.Equal(t, 2, batchResp.Succeeded)
	assert.Equal(t, 0, batchResp.Failed)
	assert.Len(t, batchResp.Results, 2)
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

func TestHandleRequestSuccess(t *testing.T) {
	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetObjectRequest{
		Key: "test-key",
	})
	require.NoError(t, err)

	req := &Request{
		Opcode:  OpGetObject,
		Token:   "test-token",
		Payload: reqPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	assert.Equal(t, StatusOK, resp.Status)
	assert.NotEmpty(t, resp.Payload)
}

func TestHandleRequestWithError(t *testing.T) {
	svc := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &dberrors.QBError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  assert.AnError,
			}
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&service.GetObjectRequest{
		Key: "missing-key",
	})
	require.NoError(t, err)

	req := &Request{
		Opcode:  OpGetObject,
		Token:   "test-token",
		Payload: reqPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	assert.Equal(t, StatusNotFound, resp.Status)
}
