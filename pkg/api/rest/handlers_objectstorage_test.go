//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Object Storage Service
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
		Key:      req.Key,
		ObjectID: 12345,
		Checksum: []byte{0x01, 0x02, 0x03},
		Size:     int64(len(req.Value)),
		Backend:  "primary",
		Version:  1,
	}, nil
}

func (m *mockObjectStorageService) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	if m.getObjectFunc != nil {
		return m.getObjectFunc(ctx, req)
	}
	return &service.GetObjectResponse{
		Key:         req.Key,
		Value:       []byte("test data"),
		ContentType: "text/plain",
		Size:        9,
		Checksum:    []byte{0x01, 0x02, 0x03},
		Version:     1,
	}, nil
}

func (m *mockObjectStorageService) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	if m.deleteObjectFunc != nil {
		return m.deleteObjectFunc(ctx, req)
	}
	return &service.DeleteObjectResponse{
		Key:     req.Key,
		Success: true,
		Size:    100,
	}, nil
}

func (m *mockObjectStorageService) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	if m.headObjectFunc != nil {
		return m.headObjectFunc(ctx, req)
	}
	return &service.HeadObjectResponse{
		Key:          req.Key,
		Exists:       true,
		ContentType:  "text/plain",
		Size:         100,
		Checksum:     []byte{0x01, 0x02, 0x03},
		Version:      1,
		Status:       "active",
		ReplicaCount: 2,
	}, nil
}

func (m *mockObjectStorageService) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	if m.listObjectsFunc != nil {
		return m.listObjectsFunc(ctx, req)
	}
	return &service.ListObjectsResponse{
		Objects: []*service.ObjectSummary{
			{Key: "obj1", Size: 100},
			{Key: "obj2", Size: 200},
		},
		HasMore:    false,
		TotalCount: 2,
	}, nil
}

func (m *mockObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	if m.listByTagFunc != nil {
		return m.listByTagFunc(ctx, req)
	}
	return &service.ListByTagResponse{
		Objects:    []*service.ObjectSummary{{Key: "tagged-obj", Size: 100}},
		HasMore:    false,
		TotalCount: 1,
	}, nil
}

func (m *mockObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	if m.listByTenantFunc != nil {
		return m.listByTenantFunc(ctx, req)
	}
	return &service.ListByTenantResponse{
		Objects:    []*service.ObjectSummary{{Key: "tenant-obj", Size: 100}},
		HasMore:    false,
		TotalCount: 1,
	}, nil
}

func (m *mockObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	if m.copyObjectFunc != nil {
		return m.copyObjectFunc(ctx, req)
	}
	return &service.CopyObjectResponse{
		Key:      req.DestinationKey,
		ObjectID: 12346,
		Size:     100,
		Checksum: []byte{0x01, 0x02, 0x03},
		Version:  1,
	}, nil
}

func (m *mockObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	if m.replicateObjectFunc != nil {
		return m.replicateObjectFunc(ctx, req)
	}
	results := make(map[string]*service.ReplicationResult)
	for _, target := range req.Targets {
		results[target] = &service.ReplicationResult{Target: target, Success: true}
	}
	return &service.ReplicateObjectResponse{
		Key:     req.Key,
		Status:  "completed",
		Results: results,
	}, nil
}

func (m *mockObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	if m.migrateObjectFunc != nil {
		return m.migrateObjectFunc(ctx, req)
	}
	return &service.MigrateObjectResponse{
		Key:               req.Key,
		SourceBackend:     "primary",
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
		ArchiveStore: "archive",
		Success:      true,
	}, nil
}

func (m *mockObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	if m.restoreObjectFunc != nil {
		return m.restoreObjectFunc(ctx, req)
	}
	return &service.RestoreObjectResponse{
		Key:     req.Key,
		Backend: "primary",
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
			"primary": {Store: "primary", Valid: true, ChecksumMatch: true, SizeMatch: true},
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
		Key:               req.Key,
		ReplicationPolicy: &catalog.ReplicationPolicy{Enabled: true, MinReplicas: 2},
	}, nil
}

func (m *mockObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc(ctx, req)
	}
	return &service.ListBackendsResponse{
		Backends: []*service.BackendInfo{
			{Name: "primary", Type: "local", Enabled: true},
			{Name: "archive", Type: "s3", Enabled: true},
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
		Latency: 5,
		Status:  "ok",
	}, nil
}

func (m *mockObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	if m.getTenantUsageFunc != nil {
		return m.getTenantUsageFunc(ctx, req)
	}
	return &service.GetTenantUsageResponse{
		TenantID:      req.TenantID,
		ObjectCount:   100,
		TotalSize:     1000000,
		ActiveCount:   90,
		ArchivedCount: 10,
	}, nil
}

func (m *mockObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	if m.getTenantQuotasFunc != nil {
		return m.getTenantQuotasFunc(ctx, req)
	}
	return &service.GetTenantQuotasResponse{
		TenantID:         req.TenantID,
		MaxStorageBytes:  10000000000,
		MaxObjectSize:    1000000000,
		MaxObjectCount:   10000,
		UsedStorageBytes: 1000000,
		UsedObjectCount:  100,
	}, nil
}

func (m *mockObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	if m.setTenantQuotasFunc != nil {
		return m.setTenantQuotasFunc(ctx, req)
	}
	return &service.SetTenantQuotasResponse{
		TenantID:  req.TenantID,
		UpdatedAt: 1234567890,
	}, nil
}

func (m *mockObjectStorageService) BatchPutObjects(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	return &service.BatchPutObjectsResponse{
		Results:      make([]*service.BatchPutResult, 0),
		SuccessCount: 0,
		FailureCount: 0,
	}, nil
}

func (m *mockObjectStorageService) BatchGetObjects(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	return &service.BatchGetObjectsResponse{
		Results:       make([]*service.BatchGetResult, 0),
		FoundCount:    0,
		NotFoundCount: 0,
	}, nil
}

func (m *mockObjectStorageService) BatchDeleteObjects(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	return &service.BatchDeleteObjectsResponse{
		Results:      make([]*service.BatchDeleteResult, 0),
		DeletedCount: 0,
		FailureCount: 0,
	}, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

func newTestObjectStorageServer(objService service.ObjectStorageService) *Server {
	mockService := &mockQuicBaaSService{
		objectStorageService: objService,
	}
	return &Server{
		service: mockService,
		config:  DefaultConfig(),
	}
}

func setupObjectStorageRoute(s *Server, method, path string, handler http.HandlerFunc) *chi.Mux {
	r := chi.NewRouter()
	r.HandleFunc(path, handler)
	return r
}

// =============================================================================
// PUT Object Tests
// =============================================================================

func TestHandlePutObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test/object.txt", bytes.NewReader([]byte("test content")))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Tenant-ID", "tenant1")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	if w.Header().Get("ETag") == "" {
		t.Error("expected ETag header to be set")
	}

	if w.Header().Get("X-Object-Version") == "" {
		t.Error("expected X-Object-Version header to be set")
	}
}

func TestHandlePutObject_EmptyKey(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/", bytes.NewReader([]byte("test content")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandlePutObject_EmptyBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandlePutObject_WithTags(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			if len(req.Tags) == 0 {
				t.Error("expected tags to be parsed")
			}
			if req.Tags["env"] != "test" {
				t.Errorf("expected tag env=test, got %s", req.Tags["env"])
			}
			return &service.PutObjectResponse{
				Key:      req.Key,
				ObjectID: 123,
				Checksum: []byte{0x01},
				Size:     int64(len(req.Value)),
				Version:  1,
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	tags := `{"env":"test","app":"demo"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt?tags="+tags, bytes.NewReader([]byte("content")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
}

func TestHandlePutObject_WithChecksum(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			if len(req.Checksum) == 0 {
				t.Error("expected checksum to be parsed")
			}
			return &service.PutObjectResponse{
				Key:      req.Key,
				ObjectID: 123,
				Checksum: req.Checksum,
				Size:     int64(len(req.Value)),
				Version:  1,
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	checksum := hex.EncodeToString([]byte{0x01, 0x02, 0x03, 0x04})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt", bytes.NewReader([]byte("content")))
	req.Header.Set("X-Checksum-SHA256", checksum)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}
}

func TestHandlePutObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt", bytes.NewReader([]byte("content")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

// =============================================================================
// GET Object Tests
// =============================================================================

func TestHandleGetObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test/object.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("expected Content-Type text/plain, got %s", w.Header().Get("Content-Type"))
	}

	if w.Header().Get("ETag") == "" {
		t.Error("expected ETag header")
	}
}

func TestHandleGetObject_EmptyKey(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleGetObject_NotFound(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrNotFound, Err: &errors.MessageError{Message: "object not found"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/nonexistent.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// =============================================================================
// DELETE Object Tests
// =============================================================================

func TestHandleDeleteObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Delete("/api/v1/objects/*", s.HandleDeleteObject)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleDeleteObject_EmptyKey(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Delete("/api/v1/objects/*", s.HandleDeleteObject)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/objects/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// =============================================================================
// HEAD Object Tests
// =============================================================================

func TestHandleHeadObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Head("/api/v1/objects/*", s.HandleHeadObject)

	req := httptest.NewRequest(http.MethodHead, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Header().Get("Content-Length") == "" {
		t.Error("expected Content-Length header")
	}

	if w.Header().Get("X-Object-Status") == "" {
		t.Error("expected X-Object-Status header")
	}
}

func TestHandleHeadObject_NotFound(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		headObjectFunc: func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
			return &service.HeadObjectResponse{Key: req.Key, Exists: false}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Head("/api/v1/objects/*", s.HandleHeadObject)

	req := httptest.NewRequest(http.MethodHead, "/api/v1/objects/nonexistent.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// =============================================================================
// List Objects Tests
// =============================================================================

func TestHandleListObjects_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects", s.HandleListObjects)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects?prefix=test/&limit=50", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp service.ListObjectsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp.Objects) != 2 {
		t.Errorf("expected 2 objects, got %d", len(resp.Objects))
	}
}

func TestHandleListByTag_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tag", s.HandleListByTag)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tag?name=env&value=prod", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleListByTag_MissingTagName(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tag", s.HandleListByTag)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tag", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleListByTenant_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tenant/{tenant}", s.HandleListByTenant)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tenant/tenant1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleListByTenant_MissingTenant(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tenant/{tenant}", s.HandleListByTenant)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tenant/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Router won't match without tenant, so 404
	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// =============================================================================
// Copy Object Tests
// =============================================================================

func TestHandleCopyObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleCopyObject)

	body := `{"destination_key":"backup/test.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/copy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
}

func TestHandleCopyObject_MissingDestination(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleCopyObject)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/copy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// =============================================================================
// Replicate Object Tests
// =============================================================================

func TestHandleReplicateObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleReplicateObject)

	body := `{"targets":["replica1","replica2"],"priority":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/replicate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestHandleReplicateObject_NoTargets(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleReplicateObject)

	body := `{"targets":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/replicate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// =============================================================================
// Migrate Object Tests
// =============================================================================

func TestHandleMigrateObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleMigrateObject)

	body := `{"target_backend":"archive","delete_from_source":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/migrate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestHandleMigrateObject_NoTarget(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleMigrateObject)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/migrate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// =============================================================================
// Archive Object Tests
// =============================================================================

func TestHandleArchiveObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleArchiveObject)

	body := `{"target_store":"cold-storage"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/archive", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestHandleArchiveObject_EmptyBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleArchiveObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/archive", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Empty body should be acceptable (uses defaults)
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// =============================================================================
// Restore Object Tests
// =============================================================================

func TestHandleRestoreObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleRestoreObject)

	body := `{"target_backend":"primary"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/archived.txt/restore", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// =============================================================================
// Verify Object Tests
// =============================================================================

func TestHandleVerifyObject_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleVerifyObject)

	body := `{"stores":["primary","replica"],"repair_mismatches":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/verify", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// =============================================================================
// Policy Tests
// =============================================================================

func TestHandleGetObjectPolicy_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObjectPolicy)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt/policy", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleSetObjectPolicy_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandleSetObjectPolicy)

	body := `{"replication_policy":{"enabled":true,"min_replicas":3}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt/policy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// =============================================================================
// Backend Tests
// =============================================================================

func TestHandleListObjectBackends_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/backends", s.HandleListObjectBackends)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends?health=true", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp service.ListBackendsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp.Backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(resp.Backends))
	}
}

func TestHandleGetObjectBackendHealth_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/backends/{name}/health", s.HandleGetObjectBackendHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/primary/health", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleGetObjectBackendHealth_MissingName(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/backends/{name}/health", s.HandleGetObjectBackendHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends//health", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Empty name returns 400 Bad Request
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// =============================================================================
// Tenant Tests
// =============================================================================

func TestHandleGetTenantUsage_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{tenant}/usage", s.HandleGetTenantUsage)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/usage", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp service.GetTenantUsageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.TenantID != "tenant1" {
		t.Errorf("expected tenant ID tenant1, got %s", resp.TenantID)
	}
}

func TestHandleGetTenantQuotas_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{tenant}/quotas", s.HandleGetTenantQuotas)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/quotas", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp service.GetTenantQuotasResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.MaxStorageBytes == 0 {
		t.Error("expected non-zero max storage bytes")
	}
}

// =============================================================================
// Error Handling Tests
// =============================================================================

func TestHandleObjectStorageError_NotFound(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrNotFound}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/missing.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleObjectStorageError_Forbidden(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrForbidden}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/protected.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestHandleObjectStorageError_QuotaExceeded(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrResourceExhausted}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/large.bin", bytes.NewReader([]byte("data")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected status %d, got %d", http.StatusTooManyRequests, w.Code)
	}
}

func TestHandleObjectStorageError_AlreadyExists(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		copyObjectFunc: func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrAlreadyExists}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleCopyObject)

	body := `{"destination_key":"existing.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/copy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, w.Code)
	}
}

// =============================================================================
// Mock QuicBaaS Service Extension
// =============================================================================

type mockQuicBaaSService struct {
	service.QuicBaaS
	objectStorageService service.ObjectStorageService
}

func (m *mockQuicBaaSService) ObjectStorageService() service.ObjectStorageService {
	return m.objectStorageService
}

// =============================================================================
// Additional Coverage Tests
// =============================================================================

func TestHandleObjectAction_Get(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleObjectAction_GetPolicy(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt/policy", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleObjectAction_Put(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt", bytes.NewReader([]byte("data")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}
}

func TestHandleObjectAction_PutPolicy(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	body := `{"replication_policy":{"enabled":true}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt/policy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleObjectAction_Delete(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleObjectAction_Head(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	req := httptest.NewRequest(http.MethodHead, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleObjectAction_PostCopy(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	body := `{"destination_key":"backup/test.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/copy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
}

func TestHandleObjectAction_PostReplicate(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	body := `{"targets":["replica1"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/replicate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestHandleObjectAction_PostMigrate(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	body := `{"target_backend":"archive"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/migrate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestHandleObjectAction_PostArchive(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/archive", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestHandleObjectAction_PostRestore(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/restore", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestHandleObjectAction_PostVerify(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/verify", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestHandleObjectAction_PostUnknown(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/unknown", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleObjectAction_MethodNotAllowed(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.HandleFunc("/api/v1/objects/*", s.handleObjectAction)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestHandleObjectStorageError_InvalidInput(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrInvalidInput, Err: &errors.MessageError{Message: "invalid input"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt", bytes.NewReader([]byte("data")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleObjectStorageError_Timeout(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrTimeout, Err: &errors.MessageError{Message: "timeout"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status %d, got %d", http.StatusGatewayTimeout, w.Code)
	}
}

func TestHandleObjectStorageError_Storage(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "storage error"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleObjectStorageError_NonDragonError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, &errors.MessageError{Message: "generic error"}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandlePutObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "storage error"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt", bytes.NewReader([]byte("data")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleDeleteObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		deleteObjectFunc: func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrNotFound, Err: &errors.MessageError{Message: "not found"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Delete("/api/v1/objects/*", s.HandleDeleteObject)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/objects/nonexistent.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleListObjects_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		listObjectsFunc: func(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "storage error"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects", s.HandleListObjects)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleListByTag_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		listByTagFunc: func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "storage error"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tag", s.HandleListByTag)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tag?name=env", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleListByTenant_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		listByTenantFunc: func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrForbidden, Err: &errors.MessageError{Message: "forbidden"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tenant/{tenant}", s.HandleListByTenant)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tenant/tenant1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestHandleHeadObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		headObjectFunc: func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "storage error"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Head("/api/v1/objects/*", s.HandleHeadObject)

	req := httptest.NewRequest(http.MethodHead, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleCopyObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		copyObjectFunc: func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrNotFound, Err: &errors.MessageError{Message: "source not found"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleCopyObject)

	body := `{"destination_key":"backup/test.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/nonexistent.txt/copy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleReplicateObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		replicateObjectFunc: func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "replication failed"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleReplicateObject)

	body := `{"targets":["replica1"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/replicate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleMigrateObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		migrateObjectFunc: func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "migration failed"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleMigrateObject)

	body := `{"target_backend":"archive"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/migrate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleArchiveObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		archiveObjectFunc: func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "archive failed"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleArchiveObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/archive", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleRestoreObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		restoreObjectFunc: func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrNotFound, Err: &errors.MessageError{Message: "not archived"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleRestoreObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/restore", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleVerifyObject_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		verifyObjectFunc: func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "verify failed"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleVerifyObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/verify", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleGetObjectPolicy_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectPolicyFunc: func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrNotFound, Err: &errors.MessageError{Message: "not found"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObjectPolicy)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/nonexistent.txt/policy", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleSetObjectPolicy_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		setObjectPolicyFunc: func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrNotFound, Err: &errors.MessageError{Message: "not found"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandleSetObjectPolicy)

	body := `{"replication_policy":{"enabled":true}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/nonexistent.txt/policy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleListObjectBackends_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		listBackendsFunc: func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrStorage, Err: &errors.MessageError{Message: "storage error"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/backends", s.HandleListObjectBackends)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandleGetObjectBackendHealth_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getBackendHealthFunc: func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrNotFound, Err: &errors.MessageError{Message: "backend not found"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/backends/{name}/health", s.HandleGetObjectBackendHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/nonexistent/health", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleGetTenantUsage_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getTenantUsageFunc: func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrForbidden, Err: &errors.MessageError{Message: "forbidden"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{tenant}/usage", s.HandleGetTenantUsage)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/usage", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestHandleGetTenantQuotas_ServiceError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getTenantQuotasFunc: func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
			return nil, &errors.DragonError{Code: errors.ErrForbidden, Err: &errors.MessageError{Message: "forbidden"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{tenant}/quotas", s.HandleGetTenantQuotas)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/quotas", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

func TestHandlePutObject_InvalidTagsJSON(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt?tags=invalid-json", bytes.NewReader([]byte("data")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleCopyObject_InvalidBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleCopyObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/copy", strings.NewReader("invalid-json"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleReplicateObject_InvalidBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleReplicateObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/replicate", strings.NewReader("invalid-json"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleMigrateObject_InvalidBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleMigrateObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/migrate", strings.NewReader("invalid-json"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleArchiveObject_InvalidBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleArchiveObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/archive", strings.NewReader("invalid-json"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleRestoreObject_InvalidBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleRestoreObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/restore", strings.NewReader("invalid-json"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleVerifyObject_InvalidBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleVerifyObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/verify", strings.NewReader("invalid-json"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleSetObjectPolicy_InvalidBody(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandleSetObjectPolicy)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt/policy", strings.NewReader("invalid-json"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// Additional tests for improved coverage

func TestHandleGetObject_DefaultContentType(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return &service.GetObjectResponse{
				Key:         req.Key,
				Value:       []byte("binary data"),
				ContentType: "", // empty content type should default to octet-stream
				Size:        11,
				Checksum:    []byte{0x01},
				Version:     1,
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/binary.dat", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %s", w.Header().Get("Content-Type"))
	}
}

func TestHandleHeadObject_DefaultContentType(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		headObjectFunc: func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
			return &service.HeadObjectResponse{
				Key:         req.Key,
				Exists:      true,
				ContentType: "", // empty content type
				Size:        100,
				Checksum:    []byte{0x01},
				Version:     1,
				Status:      "active",
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Head("/api/v1/objects/*", s.HandleHeadObject)

	req := httptest.NewRequest(http.MethodHead, "/api/v1/objects/binary.dat", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %s", w.Header().Get("Content-Type"))
	}
}

func TestHandleHeadObject_WithTenantID(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		headObjectFunc: func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
			return &service.HeadObjectResponse{
				Key:         req.Key,
				Exists:      true,
				ContentType: "text/plain",
				Size:        100,
				Checksum:    []byte{0x01},
				Version:     1,
				Status:      "active",
				TenantID:    "tenant123",
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Head("/api/v1/objects/*", s.HandleHeadObject)

	req := httptest.NewRequest(http.MethodHead, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Header().Get("X-Tenant-ID") != "tenant123" {
		t.Errorf("expected X-Tenant-ID tenant123, got %s", w.Header().Get("X-Tenant-ID"))
	}
}

func TestHandleHeadObject_EmptyKey(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Head("/api/v1/objects/*", s.HandleHeadObject)

	req := httptest.NewRequest(http.MethodHead, "/api/v1/objects/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleHeadObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Head("/api/v1/objects/*", s.HandleHeadObject)

	req := httptest.NewRequest(http.MethodHead, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleListObjects_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/objects", s.HandleListObjects)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleListByTag_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tag", s.HandleListByTag)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tag?name=env", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleListByTenant_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tenant/{tenant}", s.HandleListByTenant)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tenant/tenant1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleCopyObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleCopyObject)

	body := `{"destination_key":"backup.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/copy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleReplicateObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleReplicateObject)

	body := `{"targets":["replica1"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/replicate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleMigrateObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleMigrateObject)

	body := `{"target_backend":"archive"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/migrate", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleArchiveObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleArchiveObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/archive", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleRestoreObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleRestoreObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/restore", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleVerifyObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/*", s.HandleVerifyObject)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/test.txt/verify", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleGetObjectPolicy_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObjectPolicy)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt/policy", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleSetObjectPolicy_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandleSetObjectPolicy)

	body := `{"replication_policy":{"enabled":true}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt/policy", strings.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleGetTenantUsage_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{tenant}/usage", s.HandleGetTenantUsage)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/usage", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleGetTenantQuotas_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{tenant}/quotas", s.HandleGetTenantQuotas)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/quotas", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleGetTenantUsage_MissingTenant(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{tenant}/usage", s.HandleGetTenantUsage)

	// Empty tenant parameter
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//usage", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Empty path param returns bad request
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleGetTenantQuotas_MissingTenant(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{tenant}/quotas", s.HandleGetTenantQuotas)

	// Empty tenant parameter
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//quotas", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Empty path param returns bad request
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// Additional tests to improve coverage

func TestHandleListByTenant_WithPagination(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		listByTenantFunc: func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
			if req.Limit != 50 {
				t.Errorf("expected limit 50, got %d", req.Limit)
			}
			if req.Cursor != "abc123" {
				t.Errorf("expected cursor abc123, got %s", req.Cursor)
			}
			if !req.IncludeTags {
				t.Error("expected IncludeTags to be true")
			}
			return &service.ListByTenantResponse{
				Objects:    []*service.ObjectSummary{{Key: "obj1", Size: 100}},
				HasMore:    true,
				NextCursor: "def456",
				TotalCount: 100,
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tenant/{tenant}", s.HandleListByTenant)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tenant/tenant1?limit=50&cursor=abc123&include_tags=true", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleListObjectBackends_WithFilters(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		listBackendsFunc: func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
			if !req.IncludeHealth {
				t.Error("expected IncludeHealth to be true")
			}
			if req.FilterByType != "s3" {
				t.Errorf("expected FilterByType s3, got %s", req.FilterByType)
			}
			return &service.ListBackendsResponse{
				Backends: []*service.BackendInfo{
					{Name: "s3-primary", Type: "s3", Enabled: true},
				},
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/backends", s.HandleListObjectBackends)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends?health=true&type=s3", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleListObjectBackends_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/backends", s.HandleListObjectBackends)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleGetObjectBackendHealth_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/backends/{name}/health", s.HandleGetObjectBackendHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backends/primary/health", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleDeleteObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Delete("/api/v1/objects/*", s.HandleDeleteObject)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleGetObject_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandlePutObject_WithShardID(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			if req.ShardID != 42 {
				t.Errorf("expected ShardID 42, got %d", req.ShardID)
			}
			return &service.PutObjectResponse{
				Key:      req.Key,
				ObjectID: 123,
				Checksum: []byte{0x01},
				Size:     int64(len(req.Value)),
				Version:  1,
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/objects/*", s.HandlePutObject)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/objects/test.txt", bytes.NewReader([]byte("data")))
	req.Header.Set("X-Shard-ID", "42")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}
}

func TestHandleListByTag_WithCustomLimit(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		listByTagFunc: func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
			if req.Limit != 25 {
				t.Errorf("expected limit 25, got %d", req.Limit)
			}
			return &service.ListByTagResponse{
				Objects:    []*service.ObjectSummary{{Key: "tagged-obj", Size: 100}},
				HasMore:    false,
				TotalCount: 1,
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tag", s.HandleListByTag)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tag?name=env&limit=25", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleGetObjectPolicy_EmptyKeyAfterStrip(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObjectPolicy)

	// Test with /policy suffix - key becomes empty after strip
	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/policy", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// With key="policy" and no /policy suffix it's a valid key
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// Final tests to achieve 90%+ coverage

func TestHandleListByTenant_WithLimit(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		listByTenantFunc: func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
			if req.Limit != 75 {
				t.Errorf("expected limit 75, got %d", req.Limit)
			}
			return &service.ListByTenantResponse{
				Objects: []*service.ObjectSummary{{Key: "obj1", Size: 100}},
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/by-tenant/{tenant}", s.HandleListByTenant)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/by-tenant/tenant1?limit=75", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleGetObjectPolicy_WithPolicy(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectPolicyFunc: func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
			return &service.GetObjectPolicyResponse{
				Key: req.Key,
				ReplicationPolicy: &catalog.ReplicationPolicy{
					Enabled:     true,
					MinReplicas: 3,
				},
				ArchivePolicy: &catalog.ArchivePolicy{
					Enabled:          true,
					ArchiveAfterDays: 90,
					TargetStore:      "cold",
				},
				RetentionPolicy: &catalog.RetentionPolicy{
					Enabled:       true,
					RetentionDays: 365,
				},
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObjectPolicy)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt/policy", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleObjectStorageError_DefaultCase(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			// Use an error code that doesn't have specific handling
			return nil, &errors.DragonError{Code: errors.ErrRaft, Err: &errors.MessageError{Message: "raft error"}}
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Get("/api/v1/objects/*", s.HandleGetObject)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/test.txt", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// =============================================================================
// Batch Put Objects Tests
// =============================================================================

func TestHandleBatchPutObjects_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil) // nil service

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch", s.HandleBatchPutObjects)

	body := `{"objects":[{"key":"test.txt","value":"test"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleBatchPutObjects_InvalidJSON(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch", s.HandleBatchPutObjects)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBatchPutObjects_EmptyArray(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch", s.HandleBatchPutObjects)

	body := `{"objects":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBatchPutObjects_MissingKey(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch", s.HandleBatchPutObjects)

	body := `{"objects":[{"key":"","value":"test"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp BatchPutObjectsRESTResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", resp.Failed)
	}
}

func TestHandleBatchPutObjects_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch", s.HandleBatchPutObjects)

	body := `{"objects":[{"key":"test1.txt","value":"test1"},{"key":"test2.txt","value":"test2"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant1")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleBatchPutObjects_PartialFailure(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			if req.Key == "fail.txt" {
				return nil, &errors.MessageError{Message: "put failed"}
			}
			return &service.PutObjectResponse{
				Key:      req.Key,
				ObjectID: 123,
				Version:  1,
				Size:     int64(len(req.Value)),
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch", s.HandleBatchPutObjects)

	body := `{"objects":[{"key":"success.txt","value":"data"},{"key":"fail.txt","value":"data"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp BatchPutObjectsRESTResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Success != 1 || resp.Failed != 1 {
		t.Errorf("expected 1 success and 1 failed, got %d success and %d failed", resp.Success, resp.Failed)
	}
}

// =============================================================================
// Batch Get Objects Tests
// =============================================================================

func TestHandleBatchGetObjects_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/get", s.HandleBatchGetObjects)

	body := `{"keys":["test.txt"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/get", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleBatchGetObjects_InvalidJSON(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/get", s.HandleBatchGetObjects)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/get", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBatchGetObjects_EmptyKeys(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/get", s.HandleBatchGetObjects)

	body := `{"keys":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/get", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBatchGetObjects_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/get", s.HandleBatchGetObjects)

	body := `{"keys":["test1.txt","test2.txt"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/get", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant1")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

// =============================================================================
// Batch Delete Objects Tests
// =============================================================================

func TestHandleBatchDeleteObjects_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/delete", s.HandleBatchDeleteObjects)

	body := `{"keys":["test.txt"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleBatchDeleteObjects_InvalidJSON(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/delete", s.HandleBatchDeleteObjects)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/delete", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBatchDeleteObjects_EmptyKeys(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/delete", s.HandleBatchDeleteObjects)

	body := `{"keys":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleBatchDeleteObjects_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/delete", s.HandleBatchDeleteObjects)

	body := `{"keys":["test1.txt","test2.txt"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant1")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

// =============================================================================
// Set Tenant Quotas Tests
// =============================================================================

func TestHandleSetTenantQuotas_ServiceUnavailable(t *testing.T) {
	s := newTestObjectStorageServer(nil)

	r := chi.NewRouter()
	r.Put("/api/v1/tenants/{tenant}/quotas", s.HandleSetTenantQuotas)

	body := `{"max_storage_bytes":1073741824,"max_objects":1000}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant1/quotas", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleSetTenantQuotas_MissingTenantID(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/tenants/{tenant}/quotas", s.HandleSetTenantQuotas)

	body := `{"max_storage_bytes":1073741824}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants//quotas", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleSetTenantQuotas_InvalidJSON(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant1")

	r := chi.NewRouter()
	r.Put("/api/v1/tenants/{tenant}/quotas", s.HandleSetTenantQuotas)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant1/quotas", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleSetTenantQuotas_Success(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Put("/api/v1/tenants/{tenant}/quotas", s.HandleSetTenantQuotas)

	body := `{"max_storage_bytes":1073741824,"max_objects":1000,"max_object_size":104857600}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant1/quotas", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}
