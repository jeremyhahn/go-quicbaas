//go:build rest && !integration

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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
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
// Mock QuicBaaS Service with Admin
// =============================================================================

type mockQuicBaaSServiceWithAdmin struct {
	service.QuicBaaS
	objectStorageService      service.ObjectStorageService
	objectStorageAdminService service.ObjectStorageAdminService
}

func (m *mockQuicBaaSServiceWithAdmin) ObjectStorageService() service.ObjectStorageService {
	return m.objectStorageService
}

func (m *mockQuicBaaSServiceWithAdmin) ObjectStorageAdminService() service.ObjectStorageAdminService {
	return m.objectStorageAdminService
}

// =============================================================================
// Test Helper Functions
// =============================================================================

func newTestObjectStorageAdminServer(adminService service.ObjectStorageAdminService) *Server {
	mockService := &mockQuicBaaSServiceWithAdmin{
		objectStorageAdminService: adminService,
	}
	return &Server{
		service: mockService,
		config:  DefaultConfig(),
	}
}

// =============================================================================
// HandleCreateObjectBackend Tests
// =============================================================================

func TestHandleCreateObjectBackend_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/admin/object-backends", s.HandleCreateObjectBackend)

	body := `{"name":"test-backend","type":"local","tier":"hot","enabled":true,"config":{"path":"/data"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/object-backends", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp service.CreateBackendResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "test-backend", resp.Name)
}

// =============================================================================
// HandleUpdateObjectBackend Tests
// =============================================================================

func TestHandleUpdateObjectBackend_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Put("/api/v1/admin/object-backends/{name}", s.HandleUpdateObjectBackend)

	body := `{"config":{"new-setting":"value"},"tier":"warm"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/object-backends/test-backend", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.UpdateBackendResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "test-backend", resp.Name)
}

// =============================================================================
// HandleDeleteObjectBackend Tests
// =============================================================================

func TestHandleDeleteObjectBackend_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Delete("/api/v1/admin/object-backends/{name}", s.HandleDeleteObjectBackend)

	body := `{"force":true}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/object-backends/test-backend", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.DeleteBackendResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "test-backend", resp.Name)
	assert.True(t, resp.Deleted)
}

func TestHandleDeleteObjectBackend_WithMigration(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{
		deleteBackendFunc: func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
			return &service.DeleteBackendResponse{
				Name:            req.Name,
				Deleted:         true,
				MigratedObjects: 50,
			}, nil
		},
	}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Delete("/api/v1/admin/object-backends/{name}", s.HandleDeleteObjectBackend)

	body := `{"migrate_to":"target-backend"}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/object-backends/test-backend", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.DeleteBackendResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, int64(50), resp.MigratedObjects)
}

func TestHandleDeleteObjectBackend_WithQueryParams(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Delete("/api/v1/admin/object-backends/{name}", s.HandleDeleteObjectBackend)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/object-backends/test-backend?force=true&migrate_to=target", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// HandleSetObjectBackendStatus Tests
// =============================================================================

func TestHandleSetObjectBackendStatus_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Put("/api/v1/admin/object-backends/{name}/status", s.HandleSetObjectBackendStatus)

	body := `{"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/object-backends/test-backend/status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.SetBackendStatusResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "test-backend", resp.Name)
	assert.True(t, resp.Enabled)
}

func TestHandleSetObjectBackendStatus_Disable(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Put("/api/v1/admin/object-backends/{name}/status", s.HandleSetObjectBackendStatus)

	body := `{"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/object-backends/test-backend/status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.SetBackendStatusResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.False(t, resp.Enabled)
}

// =============================================================================
// HandleGetStorageStats Tests
// =============================================================================

func TestHandleGetStorageStats_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Get("/api/v1/admin/storage/stats", s.HandleGetStorageStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/storage/stats", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.GetStorageStatsResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, int64(1000000), resp.TotalBytes)
	assert.Equal(t, int64(100), resp.TotalObjects)
}

func TestHandleGetStorageStats_WithBackends(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{
		getStorageStatsFunc: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return &service.GetStorageStatsResponse{
				TotalBytes:   1000000,
				TotalObjects: 100,
				BackendStats: []*service.BackendInfo{
					{Name: "backend1", Type: "local"},
					{Name: "backend2", Type: "s3"},
				},
				CalculatedAt: time.Now().UnixNano(),
			}, nil
		},
	}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Get("/api/v1/admin/storage/stats", s.HandleGetStorageStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/storage/stats?backends=true", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.GetStorageStatsResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp.BackendStats, 2)
}

func TestHandleGetStorageStats_WithTenants(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Get("/api/v1/admin/storage/stats", s.HandleGetStorageStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/storage/stats?tenants=true", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// HandleRunGarbageCollection Tests
// =============================================================================

func TestHandleRunGarbageCollection_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/admin/storage/gc", s.HandleRunGarbageCollection)

	body := `{"backends":["backend1","backend2"],"dry_run":false,"aggressive":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/storage/gc", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp service.RunGarbageCollectionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.OperationID)
}

func TestHandleRunGarbageCollection_EmptyBody(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/admin/storage/gc", s.HandleRunGarbageCollection)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/storage/gc", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestHandleRunGarbageCollection_DryRun(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/admin/storage/gc", s.HandleRunGarbageCollection)

	body := `{"dry_run":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/storage/gc", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
}

// =============================================================================
// HandleGetGarbageCollectionProgress Tests
// =============================================================================

func TestHandleGetGarbageCollectionProgress_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Get("/api/v1/admin/storage/gc/progress", s.HandleGetGarbageCollectionProgress)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/storage/gc/progress?operation_id=gc-123", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.GetGarbageCollectionProgressResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "gc-123", resp.OperationID)
}

// =============================================================================
// HandleRepairObject Tests
// =============================================================================

func TestHandleRepairObject_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/admin/objects/repair", s.HandleRepairObject)

	body := `{"key":"test-key","source_location":"primary","timeout_ms":5000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/objects/repair", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.RepairObjectResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key)
	assert.True(t, resp.Success)
}

// =============================================================================
// HandleCompactObjectBackend Tests
// =============================================================================

func TestHandleCompactObjectBackend_Success(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/admin/object-backends/{name}/compact", s.HandleCompactObjectBackend)

	body := `{"aggressive":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/object-backends/test-backend/compact", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp service.CompactBackendResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "test-backend", resp.Name)
	assert.NotEmpty(t, resp.OperationID)
}

func TestHandleCompactObjectBackend_Aggressive(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/admin/object-backends/{name}/compact", s.HandleCompactObjectBackend)

	body := `{"aggressive":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/object-backends/test-backend/compact", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestHandleCompactObjectBackend_EmptyBody(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/admin/object-backends/{name}/compact", s.HandleCompactObjectBackend)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/object-backends/test-backend/compact", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
}

// =============================================================================
// Route Registration Tests
// =============================================================================

// mountObjectStorageAdminTestRoutes mounts the /admin object-storage surface
// on r exactly as the routes.go table (Routes(), GroupAPIUpload) registers
// it in production; the standalone registerObjectStorageAdminRoutes chi
// helper was removed once the table-driven registration became the single
// source of truth.
func mountObjectStorageAdminTestRoutes(s *Server, r chi.Router) {
	r.Route("/admin", func(r chi.Router) {
		r.Post("/object-backends", s.HandleCreateObjectBackend)
		r.Put("/object-backends/{name}", s.HandleUpdateObjectBackend)
		r.Delete("/object-backends/{name}", s.HandleDeleteObjectBackend)
		r.Put("/object-backends/{name}/status", s.HandleSetObjectBackendStatus)
		r.Post("/object-backends/{name}/compact", s.HandleCompactObjectBackend)

		r.Get("/storage/stats", s.HandleGetStorageStats)
		r.Post("/storage/gc", s.HandleRunGarbageCollection)
		r.Get("/storage/gc/progress", s.HandleGetGarbageCollectionProgress)

		r.Post("/objects/repair", s.HandleRepairObject)
	})
}

// serveObjectStorageAdmin sends one request through the mounted admin routes
// of a server backed by adminService and returns the recorded response.
func serveObjectStorageAdmin(adminService service.ObjectStorageAdminService, method, path, body string) *httptest.ResponseRecorder {
	s := newTestObjectStorageAdminServer(adminService)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		mountObjectStorageAdminTestRoutes(s, r)
	})

	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestObjectStorageAdmin_ServiceUnavailable(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create backend", http.MethodPost, "/api/v1/admin/object-backends", `{"name":"test-backend","type":"local","tier":"hot"}`},
		{"update backend", http.MethodPut, "/api/v1/admin/object-backends/test-backend", `{"config":{"key":"value"}}`},
		{"delete backend", http.MethodDelete, "/api/v1/admin/object-backends/test-backend", ""},
		{"set backend status", http.MethodPut, "/api/v1/admin/object-backends/test-backend/status", `{"enabled":true}`},
		{"get storage stats", http.MethodGet, "/api/v1/admin/storage/stats", ""},
		{"run garbage collection", http.MethodPost, "/api/v1/admin/storage/gc", ""},
		{"get gc progress", http.MethodGet, "/api/v1/admin/storage/gc/progress?operation_id=gc-123", ""},
		{"repair object", http.MethodPost, "/api/v1/admin/objects/repair", `{"key":"test-key"}`},
		{"compact backend", http.MethodPost, "/api/v1/admin/object-backends/test-backend/compact", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveObjectStorageAdmin(nil, tt.method, tt.path, tt.body)
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		})
	}
}

func TestObjectStorageAdmin_BadRequest(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create backend invalid json", http.MethodPost, "/api/v1/admin/object-backends", "invalid json"},
		{"create backend missing name", http.MethodPost, "/api/v1/admin/object-backends", `{"type":"local","tier":"hot"}`},
		{"create backend missing type", http.MethodPost, "/api/v1/admin/object-backends", `{"name":"test-backend","tier":"hot"}`},
		{"create backend missing tier", http.MethodPost, "/api/v1/admin/object-backends", `{"name":"test-backend","type":"local"}`},
		{"update backend invalid json", http.MethodPut, "/api/v1/admin/object-backends/test-backend", "invalid"},
		{"set backend status invalid json", http.MethodPut, "/api/v1/admin/object-backends/test-backend/status", "invalid"},
		{"get gc progress missing operation id", http.MethodGet, "/api/v1/admin/storage/gc/progress", ""},
		{"repair object invalid json", http.MethodPost, "/api/v1/admin/objects/repair", "invalid"},
		{"repair object missing key", http.MethodPost, "/api/v1/admin/objects/repair", `{"source_location":"primary"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveObjectStorageAdmin(&mockObjectStorageAdminService{}, tt.method, tt.path, tt.body)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestObjectStorageAdmin_ServiceError(t *testing.T) {
	notFound := &errors.QBError{Code: errors.ErrNotFound}
	tests := []struct {
		name   string
		svc    *mockObjectStorageAdminService
		method string
		path   string
		body   string
		status int
	}{
		{
			name: "create backend already exists",
			svc: &mockObjectStorageAdminService{
				createBackendFunc: func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
					return nil, &errors.QBError{Code: errors.ErrAlreadyExists}
				},
			},
			method: http.MethodPost,
			path:   "/api/v1/admin/object-backends",
			body:   `{"name":"test-backend","type":"local","tier":"hot"}`,
			status: http.StatusConflict,
		},
		{
			name: "update backend not found",
			svc: &mockObjectStorageAdminService{
				updateBackendFunc: func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
					return nil, notFound
				},
			},
			method: http.MethodPut,
			path:   "/api/v1/admin/object-backends/nonexistent",
			body:   `{"config":{"key":"value"}}`,
			status: http.StatusNotFound,
		},
		{
			name: "delete backend not found",
			svc: &mockObjectStorageAdminService{
				deleteBackendFunc: func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
					return nil, notFound
				},
			},
			method: http.MethodDelete,
			path:   "/api/v1/admin/object-backends/nonexistent",
			status: http.StatusNotFound,
		},
		{
			name: "set backend status not found",
			svc: &mockObjectStorageAdminService{
				setBackendStatusFunc: func(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
					return nil, notFound
				},
			},
			method: http.MethodPut,
			path:   "/api/v1/admin/object-backends/nonexistent/status",
			body:   `{"enabled":true}`,
			status: http.StatusNotFound,
		},
		{
			name: "get gc progress not found",
			svc: &mockObjectStorageAdminService{
				getGarbageCollectionProgressFunc: func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
					return nil, notFound
				},
			},
			method: http.MethodGet,
			path:   "/api/v1/admin/storage/gc/progress?operation_id=nonexistent",
			status: http.StatusNotFound,
		},
		{
			name: "repair object not found",
			svc: &mockObjectStorageAdminService{
				repairObjectFunc: func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
					return nil, notFound
				},
			},
			method: http.MethodPost,
			path:   "/api/v1/admin/objects/repair",
			body:   `{"key":"nonexistent-key"}`,
			status: http.StatusNotFound,
		},
		{
			name: "compact backend not found",
			svc: &mockObjectStorageAdminService{
				compactBackendFunc: func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
					return nil, notFound
				},
			},
			method: http.MethodPost,
			path:   "/api/v1/admin/object-backends/nonexistent/compact",
			status: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveObjectStorageAdmin(tt.svc, tt.method, tt.path, tt.body)
			assert.Equal(t, tt.status, w.Code)
		})
	}
}

func TestRegisterObjectStorageAdminRoutes(t *testing.T) {
	mockAdminSvc := &mockObjectStorageAdminService{}
	s := newTestObjectStorageAdminServer(mockAdminSvc)

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		mountObjectStorageAdminTestRoutes(s, r)
	})

	// Test that routes are registered by making requests
	tests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPost, "/api/v1/admin/object-backends", `{"name":"test","type":"local","tier":"hot"}`, http.StatusCreated},
		{http.MethodPut, "/api/v1/admin/object-backends/test", `{}`, http.StatusOK},
		{http.MethodDelete, "/api/v1/admin/object-backends/test", `{"force":true}`, http.StatusOK},
		{http.MethodPut, "/api/v1/admin/object-backends/test/status", `{"enabled":true}`, http.StatusOK},
		{http.MethodPost, "/api/v1/admin/object-backends/test/compact", `{}`, http.StatusAccepted},
		{http.MethodGet, "/api/v1/admin/storage/stats", "", http.StatusOK},
		{http.MethodPost, "/api/v1/admin/storage/gc", `{}`, http.StatusAccepted},
		{http.MethodGet, "/api/v1/admin/storage/gc/progress?operation_id=test", "", http.StatusOK},
		{http.MethodPost, "/api/v1/admin/objects/repair", `{"key":"test"}`, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			var bodyReader io.Reader
			if tt.body != "" {
				bodyReader = bytes.NewReader([]byte(tt.body))
			}
			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			assert.Equal(t, tt.status, w.Code, "Failed for %s %s", tt.method, tt.path)
		})
	}
}
