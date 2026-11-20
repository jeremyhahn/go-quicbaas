//go:build rest && !integration

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func decodeJSONMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m), w.Body.String())
	return m
}

func TestObjectStorageAdminJSON_CreateBackendSnakeCase(t *testing.T) {
	s := newTestObjectStorageAdminServer(&mockObjectStorageAdminService{})
	r := chi.NewRouter()
	r.Post("/api/v1/admin/object-backends", s.HandleCreateObjectBackend)

	body := `{"name":"test-backend","type":"local","tier":"hot","enabled":true,"config":{"path":"/data"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/object-backends", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	m := decodeJSONMap(t, w)
	require.Equal(t, "test-backend", m["name"])
	require.Contains(t, m, "created_at")
	require.NotContains(t, m, "Name")
	require.NotContains(t, m, "CreatedAt")
}

func TestObjectStorageAdminJSON_StorageStatsSnakeCase(t *testing.T) {
	svc := &mockObjectStorageAdminService{
		getStorageStatsFunc: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return &service.GetStorageStatsResponse{
				TotalBytes: 10,
				BackendStats: []*service.BackendInfo{{
					Name:        "b1",
					ObjectCount: 3,
					Health:      &service.BackendHealthInfo{Healthy: true},
				}},
				TenantStats: []*service.TenantStorageStats{{TenantID: "t1"}},
			}, nil
		},
	}
	s := newTestObjectStorageAdminServer(svc)
	r := chi.NewRouter()
	r.Get("/api/v1/admin/storage/stats", s.HandleGetStorageStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/storage/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	m := decodeJSONMap(t, w)
	require.Contains(t, m, "total_bytes")
	require.NotContains(t, m, "BackendStats")

	backends, ok := m["backend_stats"].([]any)
	require.True(t, ok, "backend_stats: %v", m["backend_stats"])
	require.Len(t, backends, 1)
	b0, ok := backends[0].(map[string]any)
	require.True(t, ok)
	require.Contains(t, b0, "object_count")
	health, ok := b0["health"].(map[string]any)
	require.True(t, ok, "health: %v", b0["health"])
	require.Equal(t, true, health["healthy"])

	tenants, ok := m["tenant_stats"].([]any)
	require.True(t, ok, "tenant_stats: %v", m["tenant_stats"])
	require.Len(t, tenants, 1)
	t0, ok := tenants[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "t1", t0["tenant_id"])
}

func TestObjectStorageAdminJSON_GCProgressSnakeCase(t *testing.T) {
	s := newTestObjectStorageAdminServer(&mockObjectStorageAdminService{})
	r := chi.NewRouter()
	r.Get("/api/v1/admin/storage/gc/progress", s.HandleGetGarbageCollectionProgress)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/storage/gc/progress?operation_id=gc-123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	m := decodeJSONMap(t, w)
	require.Equal(t, "gc-123", m["operation_id"])
	require.Contains(t, m, "percent_complete")
	require.NotContains(t, m, "OperationID")
}
