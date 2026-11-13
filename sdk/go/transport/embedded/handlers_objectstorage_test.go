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

package embedded

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	objclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
)

// =============================================================================
// Mock ObjectStorage Service
// =============================================================================

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
	return &service.PutObjectResponse{
		Key:       req.Key,
		ObjectID:  1,
		Checksum:  []byte("checksum"),
		Size:      int64(len(req.Value)),
		Backend:   "default",
		Version:   1,
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	if m.getObjectFunc != nil {
		return m.getObjectFunc(ctx, req)
	}
	return &service.GetObjectResponse{
		Key:         req.Key,
		Value:       []byte("test-data"),
		ContentType: "application/octet-stream",
		Size:        9,
		Version:     1,
		Backend:     "default",
	}, nil
}

func (m *mockObjectStorageService) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	if m.deleteObjectFunc != nil {
		return m.deleteObjectFunc(ctx, req)
	}
	return &service.DeleteObjectResponse{
		Key:       req.Key,
		Success:   true,
		Size:      100,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	if m.headObjectFunc != nil {
		return m.headObjectFunc(ctx, req)
	}
	return &service.HeadObjectResponse{
		Key:     req.Key,
		Exists:  true,
		Size:    100,
		Version: 1,
		Backend: "default",
	}, nil
}

func (m *mockObjectStorageService) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	if m.listObjectsFunc != nil {
		return m.listObjectsFunc(ctx, req)
	}
	return &service.ListObjectsResponse{
		Objects: []*service.ObjectSummary{
			{Key: "obj/1", Size: 100},
			{Key: "obj/2", Size: 200},
		},
		TotalCount: 2,
	}, nil
}

func (m *mockObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	if m.listByTagFunc != nil {
		return m.listByTagFunc(ctx, req)
	}
	return &service.ListByTagResponse{
		Objects:    []*service.ObjectSummary{{Key: "tagged/1", Size: 50}},
		TotalCount: 1,
	}, nil
}

func (m *mockObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	if m.listByTenantFunc != nil {
		return m.listByTenantFunc(ctx, req)
	}
	return &service.ListByTenantResponse{
		Objects:    []*service.ObjectSummary{{Key: "tenant/1", Size: 75}},
		TotalCount: 1,
	}, nil
}

func (m *mockObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	if m.copyObjectFunc != nil {
		return m.copyObjectFunc(ctx, req)
	}
	return &service.CopyObjectResponse{
		Key:      req.DestinationKey,
		ObjectID: 2,
		Size:     100,
		Version:  1,
		Backend:  "default",
	}, nil
}

func (m *mockObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	if m.replicateObjectFunc != nil {
		return m.replicateObjectFunc(ctx, req)
	}
	return &service.ReplicateObjectResponse{
		Key:    req.Key,
		Status: "completed",
		Results: map[string]*service.ReplicationResult{
			"backup": {Target: "backup", Success: true, Duration: 50},
		},
	}, nil
}

func (m *mockObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	if m.migrateObjectFunc != nil {
		return m.migrateObjectFunc(ctx, req)
	}
	return &service.MigrateObjectResponse{
		Key:               req.Key,
		SourceBackend:     "default",
		TargetBackend:     req.TargetBackend,
		Success:           true,
		DeletedFromSource: req.DeleteFromSource,
	}, nil
}

func (m *mockObjectStorageService) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	if m.archiveObjectFunc != nil {
		return m.archiveObjectFunc(ctx, req)
	}
	return &service.ArchiveObjectResponse{
		Key:          req.Key,
		ArchiveStore: req.TargetStore,
		Success:      true,
	}, nil
}

func (m *mockObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	if m.restoreObjectFunc != nil {
		return m.restoreObjectFunc(ctx, req)
	}
	return &service.RestoreObjectResponse{
		Key:     req.Key,
		Backend: req.TargetBackend,
		Success: true,
	}, nil
}

func (m *mockObjectStorageService) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	if m.verifyObjectFunc != nil {
		return m.verifyObjectFunc(ctx, req)
	}
	return &service.VerifyObjectResponse{
		Key:   req.Key,
		Valid: true,
		Results: map[string]*service.VerificationResult{
			"default": {Store: "default", Valid: true, ChecksumMatch: true, SizeMatch: true},
		},
	}, nil
}

func (m *mockObjectStorageService) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	if m.setObjectPolicyFunc != nil {
		return m.setObjectPolicyFunc(ctx, req)
	}
	return &service.SetObjectPolicyResponse{
		Key:     req.Key,
		Success: true,
		Version: 2,
	}, nil
}

func (m *mockObjectStorageService) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	if m.getObjectPolicyFunc != nil {
		return m.getObjectPolicyFunc(ctx, req)
	}
	return &service.GetObjectPolicyResponse{
		Key: req.Key,
		ReplicationPolicy: &catalog.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 2,
			Mode:        catalog.ReplicationModeAsync,
		},
	}, nil
}

func (m *mockObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc(ctx, req)
	}
	return &service.ListBackendsResponse{
		Backends: []*service.BackendInfo{
			{Name: "default", Type: "local", Enabled: true, Default: true},
		},
	}, nil
}

func (m *mockObjectStorageService) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	if m.getBackendHealthFunc != nil {
		return m.getBackendHealthFunc(ctx, req)
	}
	return &service.GetBackendHealthResponse{
		Name:    req.Name,
		Healthy: true,
		Status:  "ok",
	}, nil
}

func (m *mockObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	if m.getTenantUsageFunc != nil {
		return m.getTenantUsageFunc(ctx, req)
	}
	return &service.GetTenantUsageResponse{
		TenantID:    req.TenantID,
		ObjectCount: 42,
		TotalSize:   1024,
	}, nil
}

func (m *mockObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	if m.getTenantQuotasFunc != nil {
		return m.getTenantQuotasFunc(ctx, req)
	}
	return &service.GetTenantQuotasResponse{
		TenantID:        req.TenantID,
		MaxStorageBytes: 1073741824,
		MaxObjectCount:  10000,
	}, nil
}

func (m *mockObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	if m.setTenantQuotasFunc != nil {
		return m.setTenantQuotasFunc(ctx, req)
	}
	return &service.SetTenantQuotasResponse{
		TenantID:  req.TenantID,
		UpdatedAt: time.Now().UnixNano(),
	}, nil
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
			Value: []byte("data-" + key),
			Size:  int64(5 + len(key)),
		}
	}
	return &service.BatchGetObjectsResponse{
		Results:    results,
		FoundCount: len(req.Keys),
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
		}
	}
	return &service.BatchDeleteObjectsResponse{
		Results:      results,
		DeletedCount: len(req.Keys),
	}, nil
}

// =============================================================================
// Dispatch Registration Tests
// =============================================================================

func TestUnaryHandlers_ObjectStorageRegistered(t *testing.T) {
	methods := []string{
		"quicbaas.ObjectStorage/PutObject",
		"quicbaas.ObjectStorage/GetObject",
		"quicbaas.ObjectStorage/DeleteObject",
		"quicbaas.ObjectStorage/HeadObject",
		"quicbaas.ObjectStorage/ListObjects",
		"quicbaas.ObjectStorage/ListByTag",
		"quicbaas.ObjectStorage/ListByTenant",
		"quicbaas.ObjectStorage/CopyObject",
		"quicbaas.ObjectStorage/ReplicateObject",
		"quicbaas.ObjectStorage/MigrateObject",
		"quicbaas.ObjectStorage/ArchiveObject",
		"quicbaas.ObjectStorage/RestoreObject",
		"quicbaas.ObjectStorage/VerifyObject",
		"quicbaas.ObjectStorage/SetObjectPolicy",
		"quicbaas.ObjectStorage/GetObjectPolicy",
		"quicbaas.ObjectStorage/ListBackends",
		"quicbaas.ObjectStorage/GetBackendHealth",
		"quicbaas.ObjectStorage/GetTenantUsage",
		"quicbaas.ObjectStorage/GetTenantQuotas",
		"quicbaas.ObjectStorage/SetTenantQuotas",
		"quicbaas.ObjectStorage/BatchPutObjects",
		"quicbaas.ObjectStorage/BatchGetObjects",
		"quicbaas.ObjectStorage/BatchDeleteObjects",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			_, ok := unaryHandlers[method]
			assert.True(t, ok, "handler not registered for %s", method)
		})
	}
}

func TestStreamHandlers_StreamGetObjectRegistered(t *testing.T) {
	_, ok := streamHandlers["quicbaas.ObjectStorage/StreamGetObject"]
	assert.True(t, ok, "handler not registered for quicbaas.ObjectStorage/StreamGetObject")
}

// readAllWithin runs io.ReadAll on r and fails the test if the stream does
// not end before ctx does.
func readAllWithin(ctx context.Context, t *testing.T, r io.Reader) ([]byte, error) {
	t.Helper()
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(r)
		done <- result{data: data, err: err}
	}()
	select {
	case res := <-done:
		return res.data, res.err
	case <-ctx.Done():
		t.Fatal("stream did not end before the context ended")
		return nil, ctx.Err()
	}
}

func TestStreamGetObject_ReadAllRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "above one chunk", payload: bytes.Repeat([]byte("quicbaas"), 64*1024/8+4321)},
		{name: "empty object", payload: []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objSvc := &mockObjectStorageService{
				getObjectFunc: func(_ context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
					return &service.GetObjectResponse{Key: req.Key, Value: tt.payload, Size: int64(len(tt.payload))}, nil
				},
			}
			client := newObjectStorageStreamClient(t, objSvc)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			reader, err := client.StreamGetObject(ctx, &objclient.GetObjectRequest{Key: "docs/round-trip"})
			require.NoError(t, err)
			defer func() { require.NoError(t, reader.Close()) }()

			got, err := readAllWithin(ctx, t, reader)
			require.NoError(t, err)
			require.True(t, bytes.Equal(tt.payload, got), "payload mismatch: want %d bytes, got %d", len(tt.payload), len(got))
		})
	}
}

// =============================================================================
// PutObject Handler Tests
// =============================================================================

func TestHandleObjPutObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.PutObjectRequest{
		Key:         "ns:test/object",
		Value:       []byte("hello"),
		ContentType: "text/plain",
		Tags:        map[string]string{"env": "test"},
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "PutObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	putResp, ok := resp.(*objclient.PutObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:test/object", putResp.Key)
	assert.Equal(t, uint64(1), putResp.ObjectID)
	assert.Equal(t, int64(5), putResp.Size)
	assert.Equal(t, "default", putResp.Backend)
}

func TestHandleObjPutObject_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.PutObjectRequest{
		Key:   "ns:test/object",
		Value: []byte("hello"),
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "PutObject", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
	assert.Equal(t, "ObjectStorage", svcErr.Service)
}

func TestHandleObjPutObject_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "PutObject", "not-a-request")
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "quicbaas.ObjectStorage", typeErr.Service)
	assert.Equal(t, "PutObject", typeErr.Method)
}

func TestHandleObjPutObject_WithPolicies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	var capturedReq *service.PutObjectRequest
	objMock := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			capturedReq = req
			return &service.PutObjectResponse{Key: req.Key, Version: 1}, nil
		},
	}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.PutObjectRequest{
		Key:   "ns:policy/obj",
		Value: []byte("data"),
		ReplicationPolicy: &objclient.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 3,
			Mode:        objclient.ReplicationModeSync,
		},
		ArchivePolicy: &objclient.ArchivePolicy{
			Enabled:          true,
			ArchiveAfterDays: 30,
			TargetStore:      "cold",
		},
		RetentionPolicy: &objclient.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 365,
		},
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "PutObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, capturedReq)

	assert.Equal(t, true, capturedReq.ReplicationPolicy.Enabled)
	assert.Equal(t, 3, capturedReq.ReplicationPolicy.MinReplicas)
	assert.Equal(t, catalog.ReplicationModeSync, capturedReq.ReplicationPolicy.Mode)
	assert.Equal(t, true, capturedReq.ArchivePolicy.Enabled)
	assert.Equal(t, 30, capturedReq.ArchivePolicy.ArchiveAfterDays)
	assert.Equal(t, "cold", capturedReq.ArchivePolicy.TargetStore)
	assert.Equal(t, true, capturedReq.RetentionPolicy.Enabled)
	assert.Equal(t, 365, capturedReq.RetentionPolicy.RetentionDays)
}

// =============================================================================
// GetObject Handler Tests
// =============================================================================

func TestHandleObjGetObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.GetObjectRequest{Key: "ns:test/object"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	getResp, ok := resp.(*objclient.GetObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:test/object", getResp.Key)
	assert.Equal(t, []byte("test-data"), getResp.Value)
}

func TestHandleObjGetObject_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.GetObjectRequest{Key: "ns:test/object"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetObject", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

func TestHandleObjGetObject_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetObject", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// DeleteObject Handler Tests
// =============================================================================

func TestHandleObjDeleteObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.DeleteObjectRequest{Key: "ns:test/object"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "DeleteObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	delResp, ok := resp.(*objclient.DeleteObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:test/object", delResp.Key)
	assert.True(t, delResp.Success)
}

func TestHandleObjDeleteObject_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.DeleteObjectRequest{Key: "ns:test/object"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "DeleteObject", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// HeadObject Handler Tests
// =============================================================================

func TestHandleObjHeadObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.HeadObjectRequest{Key: "ns:test/object"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "HeadObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	headResp, ok := resp.(*objclient.HeadObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:test/object", headResp.Key)
	assert.True(t, headResp.Exists)
}

func TestHandleObjHeadObject_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "HeadObject", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// ListObjects Handler Tests
// =============================================================================

func TestHandleObjListObjects_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ListObjectsRequest{Prefix: "obj/"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ListObjects", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	listResp, ok := resp.(*objclient.ListObjectsResponse)
	require.True(t, ok)
	assert.Len(t, listResp.Objects, 2)
	assert.Equal(t, int64(2), listResp.TotalCount)
}

func TestHandleObjListObjects_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ListObjectsRequest{Prefix: "obj/"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ListObjects", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// CopyObject Handler Tests
// =============================================================================

func TestHandleObjCopyObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.CopyObjectRequest{
		SourceKey:      "ns:src/obj",
		DestinationKey: "ns:dst/obj",
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "CopyObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	copyResp, ok := resp.(*objclient.CopyObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:dst/obj", copyResp.Key)
}

func TestHandleObjCopyObject_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "CopyObject", "wrong")
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// ReplicateObject Handler Tests
// =============================================================================

func TestHandleObjReplicateObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ReplicateObjectRequest{
		Key:     "ns:rep/obj",
		Targets: []string{"backup"},
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ReplicateObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	repResp, ok := resp.(*objclient.ReplicateObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:rep/obj", repResp.Key)
	assert.Equal(t, "completed", repResp.Status)
	assert.Contains(t, repResp.Results, "backup")
	assert.True(t, repResp.Results["backup"].Success)
}

func TestHandleObjReplicateObject_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ReplicateObjectRequest{Key: "ns:rep/obj", Targets: []string{"backup"}}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ReplicateObject", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// VerifyObject Handler Tests
// =============================================================================

func TestHandleObjVerifyObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.VerifyObjectRequest{Key: "ns:verify/obj"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "VerifyObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	verResp, ok := resp.(*objclient.VerifyObjectResponse)
	require.True(t, ok)
	assert.True(t, verResp.Valid)
	assert.Contains(t, verResp.Results, "default")
}

func TestHandleObjVerifyObject_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "VerifyObject", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// GetObjectPolicy Handler Tests
// =============================================================================

func TestHandleObjGetObjectPolicy_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.GetObjectPolicyRequest{Key: "ns:policy/obj"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetObjectPolicy", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	policyResp, ok := resp.(*objclient.GetObjectPolicyResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:policy/obj", policyResp.Key)
	require.NotNil(t, policyResp.ReplicationPolicy)
	assert.True(t, policyResp.ReplicationPolicy.Enabled)
	assert.Equal(t, 2, policyResp.ReplicationPolicy.MinReplicas)
	assert.Equal(t, objclient.ReplicationModeAsync, policyResp.ReplicationPolicy.Mode)
}

func TestHandleObjGetObjectPolicy_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.GetObjectPolicyRequest{Key: "ns:policy/obj"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetObjectPolicy", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// SetObjectPolicy Handler Tests
// =============================================================================

func TestHandleObjSetObjectPolicy_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.SetObjectPolicyRequest{
		Key: "ns:policy/obj",
		ReplicationPolicy: &objclient.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 3,
		},
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "SetObjectPolicy", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	setResp, ok := resp.(*objclient.SetObjectPolicyResponse)
	require.True(t, ok)
	assert.True(t, setResp.Success)
}

func TestHandleObjSetObjectPolicy_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "SetObjectPolicy", "bad")
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// Tenant Usage/Quotas Handler Tests
// =============================================================================

func TestHandleObjGetTenantUsage_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.GetTenantUsageRequest{TenantID: "tenant-1"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetTenantUsage", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	usageResp, ok := resp.(*objclient.GetTenantUsageResponse)
	require.True(t, ok)
	assert.Equal(t, "tenant-1", usageResp.TenantID)
	assert.Equal(t, int64(42), usageResp.ObjectCount)
}

func TestHandleObjGetTenantUsage_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetTenantUsage", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

func TestHandleObjSetTenantQuotas_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.SetTenantQuotasRequest{
		TenantID:        "tenant-1",
		MaxStorageBytes: 1073741824,
		MaxObjectCount:  10000,
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "SetTenantQuotas", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	quotaResp, ok := resp.(*objclient.SetTenantQuotasResponse)
	require.True(t, ok)
	assert.Equal(t, "tenant-1", quotaResp.TenantID)
}

func TestHandleObjSetTenantQuotas_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.SetTenantQuotasRequest{TenantID: "tenant-1"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "SetTenantQuotas", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// BatchPutObjects Handler Tests
// =============================================================================

func TestHandleObjBatchPutObjects_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.BatchPutObjectsRequest{
		Objects: []*objclient.PutObjectRequest{
			{Key: "ns:batch/1", Value: []byte("a")},
			{Key: "ns:batch/2", Value: []byte("b")},
		},
		Atomic: true,
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "BatchPutObjects", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	batchResp, ok := resp.(*objclient.BatchPutObjectsResponse)
	require.True(t, ok)
	assert.Equal(t, int32(2), batchResp.Succeeded)
	assert.Equal(t, int32(0), batchResp.Failed)
	assert.Len(t, batchResp.Results, 2)
}

func TestHandleObjBatchPutObjects_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "BatchPutObjects", "bad")
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// BatchGetObjects Handler Tests
// =============================================================================

func TestHandleObjBatchGetObjects_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.BatchGetObjectsRequest{
		Keys: []string{"ns:batch/1", "ns:batch/2"},
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "BatchGetObjects", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	batchResp, ok := resp.(*objclient.BatchGetObjectsResponse)
	require.True(t, ok)
	assert.Len(t, batchResp.Objects, 2)
	assert.Empty(t, batchResp.NotFound)
}

func TestHandleObjBatchGetObjects_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.BatchGetObjectsRequest{Keys: []string{"ns:batch/1"}}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "BatchGetObjects", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// BatchDeleteObjects Handler Tests
// =============================================================================

func TestHandleObjBatchDeleteObjects_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.BatchDeleteObjectsRequest{
		Keys: []string{"ns:batch/1", "ns:batch/2"},
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "BatchDeleteObjects", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	batchResp, ok := resp.(*objclient.BatchDeleteObjectsResponse)
	require.True(t, ok)
	assert.Len(t, batchResp.DeletedKeys, 2)
	assert.Empty(t, batchResp.FailedKeys)
}

func TestHandleObjBatchDeleteObjects_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "BatchDeleteObjects", "bad")
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// MigrateObject Handler Tests
// =============================================================================

func TestHandleObjMigrateObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.MigrateObjectRequest{
		Key:              "ns:migrate/obj",
		TargetBackend:    "cold-storage",
		DeleteFromSource: true,
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "MigrateObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	migResp, ok := resp.(*objclient.MigrateObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:migrate/obj", migResp.Key)
	assert.True(t, migResp.Success)
	assert.Equal(t, "cold-storage", migResp.TargetBackend)
	assert.True(t, migResp.DeletedFromSource)
}

func TestHandleObjMigrateObject_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "MigrateObject", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// ArchiveObject Handler Tests
// =============================================================================

func TestHandleObjArchiveObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ArchiveObjectRequest{
		Key:         "ns:archive/obj",
		TargetStore: "cold",
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ArchiveObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	archResp, ok := resp.(*objclient.ArchiveObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:archive/obj", archResp.Key)
	assert.True(t, archResp.Success)
	assert.Equal(t, "cold", archResp.ArchiveStore)
}

func TestHandleObjArchiveObject_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ArchiveObjectRequest{Key: "ns:archive/obj"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ArchiveObject", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// RestoreObject Handler Tests
// =============================================================================

func TestHandleObjRestoreObject_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.RestoreObjectRequest{
		Key:           "ns:restore/obj",
		TargetBackend: "hot",
	}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "RestoreObject", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	restoreResp, ok := resp.(*objclient.RestoreObjectResponse)
	require.True(t, ok)
	assert.Equal(t, "ns:restore/obj", restoreResp.Key)
	assert.True(t, restoreResp.Success)
}

func TestHandleObjRestoreObject_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "RestoreObject", "bad")
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// ListBackends Handler Tests
// =============================================================================

func TestHandleObjListBackends_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ListBackendsRequest{IncludeHealth: true}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ListBackends", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	listResp, ok := resp.(*objclient.ListBackendsResponse)
	require.True(t, ok)
	assert.Len(t, listResp.Backends, 1)
	assert.Equal(t, "default", listResp.Backends[0].Name)
}

func TestHandleObjListBackends_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ListBackends", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// GetBackendHealth Handler Tests
// =============================================================================

func TestHandleObjGetBackendHealth_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.GetBackendHealthRequest{Name: "default"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetBackendHealth", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	healthResp, ok := resp.(*objclient.GetBackendHealthResponse)
	require.True(t, ok)
	assert.Equal(t, "default", healthResp.Name)
	assert.True(t, healthResp.Healthy)
}

func TestHandleObjGetBackendHealth_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.GetBackendHealthRequest{Name: "default"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetBackendHealth", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// ListByTag Handler Tests
// =============================================================================

func TestHandleObjListByTag_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ListByTagRequest{TagName: "env", TagValue: "prod"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ListByTag", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	tagResp, ok := resp.(*objclient.ListByTagResponse)
	require.True(t, ok)
	assert.Len(t, tagResp.Objects, 1)
}

func TestHandleObjListByTag_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ListByTag", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// ListByTenant Handler Tests
// =============================================================================

func TestHandleObjListByTenant_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ListByTenantRequest{TenantID: "tenant-1"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ListByTenant", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	tenantResp, ok := resp.(*objclient.ListByTenantResponse)
	require.True(t, ok)
	assert.Len(t, tenantResp.Objects, 1)
}

func TestHandleObjListByTenant_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.ListByTenantRequest{TenantID: "tenant-1"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "ListByTenant", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

// =============================================================================
// GetTenantQuotas Handler Tests
// =============================================================================

func TestHandleObjGetTenantQuotas_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	req := &objclient.GetTenantQuotasRequest{TenantID: "tenant-1"}

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetTenantQuotas", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	quotaResp, ok := resp.(*objclient.GetTenantQuotasResponse)
	require.True(t, ok)
	assert.Equal(t, "tenant-1", quotaResp.TenantID)
	assert.Equal(t, int64(1073741824), quotaResp.MaxStorageBytes)
}

func TestHandleObjGetTenantQuotas_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	objMock := &mockObjectStorageService{}
	mock := &mockQuicBaaS{objectStorageService: objMock}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", "GetTenantQuotas", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}
