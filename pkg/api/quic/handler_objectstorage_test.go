//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock ObjectStorage Service
// =============================================================================

// mockObjectStorageService provides a configurable mock for testing ObjectStorage handlers.
type mockObjectStorageService struct {
	putObjectFn        func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error)
	getObjectFn        func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error)
	deleteObjectFn     func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error)
	headObjectFn       func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error)
	listObjectsFn      func(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error)
	listByTagFn        func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error)
	listByTenantFn     func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error)
	copyObjectFn       func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error)
	replicateObjectFn  func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error)
	migrateObjectFn    func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error)
	archiveObjectFn    func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error)
	restoreObjectFn    func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error)
	verifyObjectFn     func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error)
	setObjectPolicyFn  func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error)
	getObjectPolicyFn  func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error)
	listBackendsFn     func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error)
	getBackendHealthFn func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error)
	getTenantUsageFn   func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error)
	getTenantQuotasFn  func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error)
}

func (m *mockObjectStorageService) PutObject(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	if m.putObjectFn != nil {
		return m.putObjectFn(ctx, req)
	}
	return &service.PutObjectResponse{
		Key:       req.Key,
		ObjectID:  12345,
		Size:      int64(len(req.Value)),
		Checksum:  []byte("checksum"),
		Version:   1,
		Backend:   "default",
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	if m.getObjectFn != nil {
		return m.getObjectFn(ctx, req)
	}
	return &service.GetObjectResponse{
		Key:         req.Key,
		Value:       []byte("test data"),
		ContentType: "application/octet-stream",
		Size:        9,
		Checksum:    []byte("checksum"),
		Version:     1,
		CreatedAt:   time.Now().UnixNano(),
		Backend:     "default",
	}, nil
}

func (m *mockObjectStorageService) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	if m.deleteObjectFn != nil {
		return m.deleteObjectFn(ctx, req)
	}
	return &service.DeleteObjectResponse{
		Key:       req.Key,
		Success:   true,
		Size:      100,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	if m.headObjectFn != nil {
		return m.headObjectFn(ctx, req)
	}
	return &service.HeadObjectResponse{
		Key:          req.Key,
		Exists:       true,
		ContentType:  "application/octet-stream",
		Size:         100,
		Version:      1,
		Status:       "active",
		CreatedAt:    time.Now().UnixNano(),
		Backend:      "default",
		ReplicaCount: 2,
	}, nil
}

func (m *mockObjectStorageService) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	if m.listObjectsFn != nil {
		return m.listObjectsFn(ctx, req)
	}
	return &service.ListObjectsResponse{
		Objects:    []*service.ObjectSummary{},
		NextCursor: "",
		HasMore:    false,
		TotalCount: 0,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	if m.listByTagFn != nil {
		return m.listByTagFn(ctx, req)
	}
	return &service.ListByTagResponse{
		Objects:    []*service.ObjectSummary{},
		NextCursor: "",
		HasMore:    false,
		TotalCount: 0,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	if m.listByTenantFn != nil {
		return m.listByTenantFn(ctx, req)
	}
	return &service.ListByTenantResponse{
		Objects:    []*service.ObjectSummary{},
		NextCursor: "",
		HasMore:    false,
		TotalCount: 0,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	if m.copyObjectFn != nil {
		return m.copyObjectFn(ctx, req)
	}
	return &service.CopyObjectResponse{
		Key:       req.DestinationKey,
		ObjectID:  12346,
		Size:      100,
		Checksum:  []byte("checksum"),
		Version:   1,
		Backend:   "default",
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	if m.replicateObjectFn != nil {
		return m.replicateObjectFn(ctx, req)
	}
	results := make(map[string]*service.ReplicationResult)
	for _, target := range req.Targets {
		results[target] = &service.ReplicationResult{
			Target:   target,
			Success:  true,
			Duration: 100,
		}
	}
	return &service.ReplicateObjectResponse{
		Key:       req.Key,
		Status:    "completed",
		Results:   results,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	if m.migrateObjectFn != nil {
		return m.migrateObjectFn(ctx, req)
	}
	return &service.MigrateObjectResponse{
		Key:               req.Key,
		SourceBackend:     "old-backend",
		TargetBackend:     req.TargetBackend,
		Success:           true,
		DeletedFromSource: req.DeleteFromSource,
		Timestamp:         time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	if m.archiveObjectFn != nil {
		return m.archiveObjectFn(ctx, req)
	}
	return &service.ArchiveObjectResponse{
		Key:                req.Key,
		ArchiveStore:       req.TargetStore,
		Success:            true,
		DeletedFromPrimary: req.DeleteFromPrimary,
		ArchivedAt:         time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	if m.restoreObjectFn != nil {
		return m.restoreObjectFn(ctx, req)
	}
	return &service.RestoreObjectResponse{
		Key:        req.Key,
		Backend:    req.TargetBackend,
		Success:    true,
		RestoredAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	if m.verifyObjectFn != nil {
		return m.verifyObjectFn(ctx, req)
	}
	results := make(map[string]*service.VerificationResult)
	for _, store := range req.Stores {
		results[store] = &service.VerificationResult{
			Store:         store,
			Valid:         true,
			ChecksumMatch: true,
			SizeMatch:     true,
		}
	}
	return &service.VerifyObjectResponse{
		Key:       req.Key,
		Valid:     true,
		Results:   results,
		Repaired:  false,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	if m.setObjectPolicyFn != nil {
		return m.setObjectPolicyFn(ctx, req)
	}
	return &service.SetObjectPolicyResponse{
		Key:       req.Key,
		Success:   true,
		Version:   2,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	if m.getObjectPolicyFn != nil {
		return m.getObjectPolicyFn(ctx, req)
	}
	return &service.GetObjectPolicyResponse{
		Key: req.Key,
		ReplicationPolicy: &catalog.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 2,
		},
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	if m.listBackendsFn != nil {
		return m.listBackendsFn(ctx, req)
	}
	return &service.ListBackendsResponse{
		Backends: []*service.BackendInfo{
			{Name: "default", Type: "local", Enabled: true, Default: true},
		},
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	if m.getBackendHealthFn != nil {
		return m.getBackendHealthFn(ctx, req)
	}
	return &service.GetBackendHealthResponse{
		Name:      req.Name,
		Healthy:   true,
		Latency:   5,
		LastCheck: time.Now().UnixNano(),
		Status:    "healthy",
	}, nil
}

func (m *mockObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	if m.getTenantUsageFn != nil {
		return m.getTenantUsageFn(ctx, req)
	}
	return &service.GetTenantUsageResponse{
		TenantID:      req.TenantID,
		ObjectCount:   100,
		TotalSize:     1024000,
		ActiveCount:   90,
		ArchivedCount: 10,
		Timestamp:     time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	if m.getTenantQuotasFn != nil {
		return m.getTenantQuotasFn(ctx, req)
	}
	return &service.GetTenantQuotasResponse{
		TenantID:         req.TenantID,
		MaxStorageBytes:  10737418240,
		MaxObjectSize:    104857600,
		MaxObjectCount:   10000,
		UsedStorageBytes: 1024000,
		UsedObjectCount:  100,
		Timestamp:        time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	return &service.SetTenantQuotasResponse{
		TenantID:  req.TenantID,
		UpdatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) BatchPutObjects(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	return &service.BatchPutObjectsResponse{
		SuccessCount: len(req.Objects),
		FailureCount: 0,
		Timestamp:    time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) BatchGetObjects(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	return &service.BatchGetObjectsResponse{
		FoundCount:    len(req.Keys),
		NotFoundCount: 0,
		Timestamp:     time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) BatchDeleteObjects(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	return &service.BatchDeleteObjectsResponse{
		DeletedCount: len(req.Keys),
		FailureCount: 0,
		Timestamp:    time.Now().UnixNano(),
	}, nil
}

// mockOSEnabledService wraps mockService to provide ObjectStorageService.
type mockOSEnabledService struct {
	mockService
	osSvc service.ObjectStorageService
}

func (m *mockOSEnabledService) ObjectStorageService() service.ObjectStorageService {
	return m.osSvc
}

// =============================================================================
// Error Mock Service
// =============================================================================

// errorObjectStorageService returns errors for all operations.
type errorObjectStorageService struct{}

func (e *errorObjectStorageService) PutObject(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	return nil, errors.New("put object failed")
}

func (e *errorObjectStorageService) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	return nil, errors.New("get object failed")
}

func (e *errorObjectStorageService) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	return nil, errors.New("delete object failed")
}

func (e *errorObjectStorageService) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	return nil, errors.New("head object failed")
}

func (e *errorObjectStorageService) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	return nil, errors.New("list objects failed")
}

func (e *errorObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	return nil, errors.New("list by tag failed")
}

func (e *errorObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	return nil, errors.New("list by tenant failed")
}

func (e *errorObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	return nil, errors.New("copy object failed")
}

func (e *errorObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	return nil, errors.New("replicate object failed")
}

func (e *errorObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	return nil, errors.New("migrate object failed")
}

func (e *errorObjectStorageService) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	return nil, errors.New("archive object failed")
}

func (e *errorObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	return nil, errors.New("restore object failed")
}

func (e *errorObjectStorageService) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	return nil, errors.New("verify object failed")
}

func (e *errorObjectStorageService) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	return nil, errors.New("set object policy failed")
}

func (e *errorObjectStorageService) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	return nil, errors.New("get object policy failed")
}

func (e *errorObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	return nil, errors.New("list backends failed")
}

func (e *errorObjectStorageService) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	return nil, errors.New("get backend health failed")
}

func (e *errorObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	return nil, errors.New("get tenant usage failed")
}

func (e *errorObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	return nil, errors.New("get tenant quotas failed")
}

func (e *errorObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	return nil, errors.New("set tenant quotas failed")
}

func (e *errorObjectStorageService) BatchPutObjects(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	return nil, errors.New("batch put objects failed")
}

func (e *errorObjectStorageService) BatchGetObjects(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	return nil, errors.New("batch get objects failed")
}

func (e *errorObjectStorageService) BatchDeleteObjects(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	return nil, errors.New("batch delete objects failed")
}

// =============================================================================
// Test Helpers
// =============================================================================

func createOSTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-os-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createOSEnabledHandler(osSvc service.ObjectStorageService) *Handler {
	mockSvc := &mockOSEnabledService{
		osSvc: osSvc,
	}
	return NewHandler(mockSvc, nil, nil, nil, nil, createOSTestLogger())
}

// =============================================================================
// handleOSPutObject Tests
// =============================================================================

func TestHandleOSPutObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.PutObjectRequest{
		Key:   "test/object",
		Value: []byte("test data"),
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSPutObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSPutObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.PutObjectRequest{
		Key:   "test/object",
		Value: []byte("test data"),
	})
	req := &Request{
		Opcode:  OpOSPutObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSPutObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSPutObject,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSPutObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.PutObjectRequest{
		Key:   "test/object",
		Value: []byte("test data"),
	})
	req := &Request{
		Opcode:  OpOSPutObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSGetObject Tests
// =============================================================================

func TestHandleOSGetObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.GetObjectRequest{
		Key: "test/object",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSGetObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSGetObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.GetObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSGetObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSGetObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSGetObject,
		Payload: []byte("not valid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSGetObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.GetObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSGetObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSDeleteObject Tests
// =============================================================================

func TestHandleOSDeleteObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.DeleteObjectRequest{
		Key: "test/object",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSDeleteObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSDeleteObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.DeleteObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSDeleteObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSDeleteObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSDeleteObject,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSDeleteObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.DeleteObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSDeleteObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSHeadObject Tests
// =============================================================================

func TestHandleOSHeadObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.HeadObjectRequest{
		Key: "test/object",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSHeadObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSHeadObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.HeadObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSHeadObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSHeadObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSHeadObject,
		Payload: []byte("bad payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSHeadObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.HeadObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSHeadObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSListObjects Tests
// =============================================================================

func TestHandleOSListObjects_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.ListObjectsRequest{
		Prefix: "test/",
		Limit:  100,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSListObjects,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSListObjects_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.ListObjectsRequest{})
	req := &Request{
		Opcode:  OpOSListObjects,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSListObjects_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSListObjects,
		Payload: []byte("invalid json data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSListObjects_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.ListObjectsRequest{})
	req := &Request{
		Opcode:  OpOSListObjects,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSListByTag Tests
// =============================================================================

func TestHandleOSListByTag_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.ListByTagRequest{
		TagName:  "environment",
		TagValue: "production",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSListByTag,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSListByTag_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.ListByTagRequest{TagName: "env"})
	req := &Request{
		Opcode:  OpOSListByTag,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSListByTag_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSListByTag,
		Payload: []byte("garbage"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSListByTag_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.ListByTagRequest{TagName: "env"})
	req := &Request{
		Opcode:  OpOSListByTag,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSListByTenant Tests
// =============================================================================

func TestHandleOSListByTenant_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.ListByTenantRequest{
		TenantID: "tenant-123",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSListByTenant,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSListByTenant_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.ListByTenantRequest{TenantID: "tenant-123"})
	req := &Request{
		Opcode:  OpOSListByTenant,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSListByTenant_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSListByTenant,
		Payload: []byte("not json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSListByTenant_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.ListByTenantRequest{TenantID: "tenant-123"})
	req := &Request{
		Opcode:  OpOSListByTenant,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSCopyObject Tests
// =============================================================================

func TestHandleOSCopyObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.CopyObjectRequest{
		SourceKey:      "test/source",
		DestinationKey: "test/destination",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSCopyObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSCopyObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.CopyObjectRequest{
		SourceKey:      "test/source",
		DestinationKey: "test/destination",
	})
	req := &Request{
		Opcode:  OpOSCopyObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSCopyObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSCopyObject,
		Payload: []byte("bad data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSCopyObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.CopyObjectRequest{
		SourceKey:      "test/source",
		DestinationKey: "test/destination",
	})
	req := &Request{
		Opcode:  OpOSCopyObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSReplicateObject Tests
// =============================================================================

func TestHandleOSReplicateObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.ReplicateObjectRequest{
		Key:     "test/object",
		Targets: []string{"backend-1", "backend-2"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSReplicateObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSReplicateObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.ReplicateObjectRequest{
		Key:     "test/object",
		Targets: []string{"backend-1"},
	})
	req := &Request{
		Opcode:  OpOSReplicateObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSReplicateObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSReplicateObject,
		Payload: []byte("invalid payload data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSReplicateObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.ReplicateObjectRequest{
		Key:     "test/object",
		Targets: []string{"backend-1"},
	})
	req := &Request{
		Opcode:  OpOSReplicateObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSMigrateObject Tests
// =============================================================================

func TestHandleOSMigrateObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.MigrateObjectRequest{
		Key:              "test/object",
		TargetBackend:    "new-backend",
		DeleteFromSource: true,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSMigrateObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSMigrateObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.MigrateObjectRequest{
		Key:           "test/object",
		TargetBackend: "new-backend",
	})
	req := &Request{
		Opcode:  OpOSMigrateObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSMigrateObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSMigrateObject,
		Payload: []byte("corrupt data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSMigrateObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.MigrateObjectRequest{
		Key:           "test/object",
		TargetBackend: "new-backend",
	})
	req := &Request{
		Opcode:  OpOSMigrateObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSArchiveObject Tests
// =============================================================================

func TestHandleOSArchiveObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.ArchiveObjectRequest{
		Key:         "test/object",
		TargetStore: "archive-store",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSArchiveObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSArchiveObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.ArchiveObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSArchiveObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSArchiveObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSArchiveObject,
		Payload: []byte("bad json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSArchiveObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.ArchiveObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSArchiveObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSRestoreObject Tests
// =============================================================================

func TestHandleOSRestoreObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.RestoreObjectRequest{
		Key:           "test/object",
		TargetBackend: "primary-backend",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSRestoreObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSRestoreObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.RestoreObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSRestoreObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSRestoreObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSRestoreObject,
		Payload: []byte("not json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSRestoreObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.RestoreObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSRestoreObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSVerifyObject Tests
// =============================================================================

func TestHandleOSVerifyObject_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.VerifyObjectRequest{
		Key:    "test/object",
		Stores: []string{"backend-1", "backend-2"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSVerifyObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSVerifyObject_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.VerifyObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSVerifyObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSVerifyObject_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSVerifyObject,
		Payload: []byte("invalid json payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSVerifyObject_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.VerifyObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSVerifyObject,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSGetObjectMetadata Tests
// =============================================================================

func TestHandleOSGetObjectMetadata_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.HeadObjectRequest{
		Key: "test/object",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSGetObjectMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSGetObjectMetadata_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.HeadObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSGetObjectMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSGetObjectMetadata_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSGetObjectMetadata,
		Payload: []byte("corrupt json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSGetObjectMetadata_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.HeadObjectRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSGetObjectMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSUpdateObjectMetadata Tests
// =============================================================================

func TestHandleOSUpdateObjectMetadata_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.SetObjectPolicyRequest{
		Key: "test/object",
		ReplicationPolicy: &catalog.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 3,
		},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSUpdateObjectMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSUpdateObjectMetadata_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.SetObjectPolicyRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSUpdateObjectMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSUpdateObjectMetadata_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSUpdateObjectMetadata,
		Payload: []byte("bad payload data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSUpdateObjectMetadata_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.SetObjectPolicyRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSUpdateObjectMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSGetStorageStats Tests
// =============================================================================

func TestHandleOSGetStorageStats_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.GetTenantUsageRequest{
		TenantID: "tenant-123",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSGetStorageStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSGetStorageStats_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.GetTenantUsageRequest{TenantID: "tenant-123"})
	req := &Request{
		Opcode:  OpOSGetStorageStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSGetStorageStats_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSGetStorageStats,
		Payload: []byte("invalid payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSGetStorageStats_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.GetTenantUsageRequest{TenantID: "tenant-123"})
	req := &Request{
		Opcode:  OpOSGetStorageStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSSetLifecyclePolicy Tests
// =============================================================================

func TestHandleOSSetLifecyclePolicy_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.SetObjectPolicyRequest{
		Key: "test/object",
		RetentionPolicy: &catalog.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
			MaxVersions:   10,
		},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSSetLifecyclePolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSSetLifecyclePolicy_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.SetObjectPolicyRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSSetLifecyclePolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSSetLifecyclePolicy_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSSetLifecyclePolicy,
		Payload: []byte("garbage data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSSetLifecyclePolicy_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.SetObjectPolicyRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSSetLifecyclePolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOSGetLifecyclePolicy Tests
// =============================================================================

func TestHandleOSGetLifecyclePolicy_Success(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, err := EncodePayload(&service.GetObjectPolicyRequest{
		Key: "test/object",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOSGetLifecyclePolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleOSGetLifecyclePolicy_NilService(t *testing.T) {
	handler := createOSEnabledHandler(nil)

	payload, _ := EncodePayload(&service.GetObjectPolicyRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSGetLifecyclePolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil ObjectStorage service")
	}
}

func TestHandleOSGetLifecyclePolicy_DecodeError(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	req := &Request{
		Opcode:  OpOSGetLifecyclePolicy,
		Payload: []byte("not valid json data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOSGetLifecyclePolicy_ServiceError(t *testing.T) {
	osSvc := &errorObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.GetObjectPolicyRequest{Key: "test/object"})
	req := &Request{
		Opcode:  OpOSGetLifecyclePolicy,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Table-Driven Tests for All ObjectStorage Operations
// =============================================================================

func TestHandleObjectStorageOperations_AllOpcodes(t *testing.T) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"OSPutObject", OpOSPutObject, &service.PutObjectRequest{Key: "test", Value: []byte("data")}},
		{"OSGetObject", OpOSGetObject, &service.GetObjectRequest{Key: "test"}},
		{"OSDeleteObject", OpOSDeleteObject, &service.DeleteObjectRequest{Key: "test"}},
		{"OSHeadObject", OpOSHeadObject, &service.HeadObjectRequest{Key: "test"}},
		{"OSListObjects", OpOSListObjects, &service.ListObjectsRequest{}},
		{"OSListByTag", OpOSListByTag, &service.ListByTagRequest{TagName: "env"}},
		{"OSListByTenant", OpOSListByTenant, &service.ListByTenantRequest{TenantID: "t1"}},
		{"OSCopyObject", OpOSCopyObject, &service.CopyObjectRequest{SourceKey: "src", DestinationKey: "dst"}},
		{"OSReplicateObject", OpOSReplicateObject, &service.ReplicateObjectRequest{Key: "test", Targets: []string{"b1"}}},
		{"OSMigrateObject", OpOSMigrateObject, &service.MigrateObjectRequest{Key: "test", TargetBackend: "b2"}},
		{"OSArchiveObject", OpOSArchiveObject, &service.ArchiveObjectRequest{Key: "test"}},
		{"OSRestoreObject", OpOSRestoreObject, &service.RestoreObjectRequest{Key: "test"}},
		{"OSVerifyObject", OpOSVerifyObject, &service.VerifyObjectRequest{Key: "test"}},
		{"OSGetObjectMetadata", OpOSGetObjectMetadata, &service.HeadObjectRequest{Key: "test"}},
		{"OSUpdateObjectMetadata", OpOSUpdateObjectMetadata, &service.SetObjectPolicyRequest{Key: "test"}},
		{"OSGetStorageStats", OpOSGetStorageStats, &service.GetTenantUsageRequest{TenantID: "t1"}},
		{"OSSetLifecyclePolicy", OpOSSetLifecyclePolicy, &service.SetObjectPolicyRequest{Key: "test"}},
		{"OSGetLifecyclePolicy", OpOSGetLifecyclePolicy, &service.GetObjectPolicyRequest{Key: "test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := EncodePayload(tt.payload)
			if err != nil {
				t.Fatalf("failed to encode payload: %v", err)
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkHandleOSPutObject(b *testing.B) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.PutObjectRequest{
		Key:   "bench/object",
		Value: []byte("benchmark data for testing"),
	})
	req := &Request{
		Opcode:  OpOSPutObject,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandleOSGetObject(b *testing.B) {
	osSvc := &mockObjectStorageService{}
	handler := createOSEnabledHandler(osSvc)

	payload, _ := EncodePayload(&service.GetObjectRequest{
		Key: "bench/object",
	})
	req := &Request{
		Opcode:  OpOSGetObject,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}
