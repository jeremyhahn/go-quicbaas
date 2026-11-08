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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// ========================================
// Health and Monitoring Handler Tests
// ========================================

func TestHandleVectorHealth(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful health check",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
					Status:        service.HealthStatusHealthy,
					UptimeSeconds: 3600,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp VectorHealthResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "healthy", resp.Status)
				assert.Equal(t, int64(3600), resp.UptimeSeconds)
			},
		},
		{
			name: "service error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Health", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "health", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockSvc)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/health", nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Namespace Management Handler Tests
// ========================================

func TestHandleCreateNamespace(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful namespace creation",
			body: CreateNamespaceRequest{
				Namespace:      "test-ns",
				Dimensions:     128,
				DistanceMetric: "cosine",
			},
			setupMock: func(m *MockVectorService) {
				m.On("CreateNamespace", mock.Anything, mock.MatchedBy(func(req *service.CreateNamespaceRequest) bool {
					return req.Namespace == "test-ns" && req.Dimension == 128
				})).Return(&service.CreateNamespaceResponse{
					Namespace: "test-ns",
					Dimension: 128,
					Timestamp: time.Now().UnixNano(),
					Success:   true,
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp CreateNamespaceResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "test-ns", resp.Namespace)
			},
		},
		{
			name:           "missing namespace name",
			body:           CreateNamespaceRequest{Dimensions: 128, DistanceMetric: "cosine"},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "namespace is required")
			},
		},
		{
			name:           "invalid dimensions",
			body:           CreateNamespaceRequest{Namespace: "test", Dimensions: 0, DistanceMetric: "cosine"},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "dimensions must be positive")
			},
		},
		{
			name:           "missing distance metric",
			body:           CreateNamespaceRequest{Namespace: "test", Dimensions: 128},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "distance_metric is required")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleUpdateNamespace(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful namespace update",
			namespace: "test-ns",
			body: UpdateNamespaceRequest{
				IndexParams: map[string]string{"m": "16"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetNamespaceInfo", mock.Anything, mock.Anything).Return(&service.GetNamespaceInfoResponse{
					Namespace: "test-ns",
					Dimension: 128,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp UpdateNamespaceResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "test-ns", resp.Namespace)
				assert.True(t, resp.RequiresRebuild)
			},
		},
		{
			name:      "namespace not found",
			namespace: "missing-ns",
			body:      UpdateNamespaceRequest{},
			setupMock: func(m *MockVectorService) {
				m.On("GetNamespaceInfo", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrNotFound, Op: "get", Err: &errors.MessageError{Message: "not found"}}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPut, "/api/v1/vector/namespaces/"+tt.namespace, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleDeleteNamespace(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful namespace deletion",
			namespace: "test-ns",
			setupMock: func(m *MockVectorService) {
				m.On("DeleteNamespace", mock.Anything, mock.Anything).Return(&service.DeleteNamespaceResponse{
					Success:        true,
					VectorsDeleted: 100,
					Timestamp:      time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp DeleteNamespaceResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Deleted)
				assert.Equal(t, int64(100), resp.VectorsDeleted)
			},
		},
		{
			name:      "namespace not found",
			namespace: "missing-ns",
			setupMock: func(m *MockVectorService) {
				m.On("DeleteNamespace", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrNotFound, Op: "delete", Err: &errors.MessageError{Message: "not found"}}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/"+tt.namespace, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleListNamespaces(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful list namespaces",
			setupMock: func(m *MockVectorService) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{
						{Namespace: "ns1", Dimension: 128, VectorCount: 100},
						{Namespace: "ns2", Dimension: 256, VectorCount: 200},
					},
					TotalCount: 2,
					Timestamp:  time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListNamespacesResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Namespaces, 2)
				assert.Equal(t, 2, resp.TotalCount)
			},
		},
		{
			name:        "with pagination",
			queryParams: "?page_size=50",
			setupMock: func(m *MockVectorService) {
				m.On("ListNamespaces", mock.Anything, mock.MatchedBy(func(req *service.ListNamespacesRequest) bool {
					return req.Limit == 50
				})).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "service error",
			setupMock: func(m *MockVectorService) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "list", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			url := "/api/v1/vector/namespaces"
			if tt.queryParams != "" {
				url += tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleGetNamespaceInfo(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful get namespace info",
			namespace: "test-ns",
			setupMock: func(m *MockVectorService) {
				m.On("GetNamespaceInfo", mock.Anything, mock.Anything).Return(&service.GetNamespaceInfoResponse{
					Namespace:   "test-ns",
					Dimension:   128,
					VectorCount: 1000,
					IndexType:   "HNSW",
					IndexConfig: map[string]string{"m": "16", "ef_construction": "200"},
					CreatedAt:   time.Now().UnixNano(),
					UpdatedAt:   time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp NamespaceInfo
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "test-ns", resp.Namespace)
				assert.Equal(t, 128, resp.Dimensions)
				assert.Equal(t, int64(1000), resp.VectorCount)
			},
		},
		{
			name:      "namespace not found",
			namespace: "missing-ns",
			setupMock: func(m *MockVectorService) {
				m.On("GetNamespaceInfo", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrNotFound, Op: "get", Err: &errors.MessageError{Message: "not found"}}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/"+tt.namespace, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleGetNamespaceStats(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful get namespace stats",
			namespace: "test-ns",
			setupMock: func(m *MockVectorService) {
				m.On("GetIndexStats", mock.Anything, mock.Anything).Return(&service.GetIndexStatsResponse{
					Namespace:      "test-ns",
					VectorCount:    5000,
					IndexSizeBytes: 1024000,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp NamespaceStatsItem
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "test-ns", resp.Namespace)
				assert.Equal(t, int64(5000), resp.VectorCount)
			},
		},
		{
			name:      "namespace not found",
			namespace: "missing-ns",
			setupMock: func(m *MockVectorService) {
				m.On("GetIndexStats", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrNotFound, Op: "stats", Err: &errors.MessageError{Message: "not found"}}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/"+tt.namespace+"/stats", nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Vector CRUD Handler Tests
// ========================================

func TestHandleBatchUpsertVectors(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful batch upsert",
			namespace: "test-ns",
			body: BatchUpsertVectorsRequest{
				Vectors: []VectorEntity{
					{ID: "1", Vector: []float32{0.1, 0.2}, Metadata: map[string]string{"key": "val1"}},
					{ID: "2", Vector: []float32{0.3, 0.4}, Metadata: map[string]string{"key": "val2"}},
				},
			},
			setupMock: func(m *MockVectorService) {
				m.On("BatchUpsert", mock.Anything, mock.MatchedBy(func(req *service.BatchUpsertRequest) bool {
					return req.Namespace == "test-ns" && len(req.Vectors) == 2
				})).Return(&service.BatchUpsertResponse{
					InsertedCount: 2,
					Timestamp:     time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp BatchUpsertVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(2), resp.VectorsProcessed)
				assert.Equal(t, int64(2), resp.VectorsSucceeded)
			},
		},
		{
			name:      "string vector ID accepted",
			namespace: "test-ns",
			body: BatchUpsertVectorsRequest{
				Vectors: []VectorEntity{
					{ID: "invalid-id", Vector: []float32{0.1, 0.2}},
				},
			},
			setupMock: func(m *MockVectorService) {
				m.On("BatchUpsert", mock.Anything, mock.MatchedBy(func(req *service.BatchUpsertRequest) bool {
					return req.Namespace == "test-ns" && len(req.Vectors) == 1
				})).Return(&service.BatchUpsertResponse{
					InsertedCount: 1,
					Timestamp:     time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp BatchUpsertVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(1), resp.VectorsProcessed)
				assert.Equal(t, int64(1), resp.VectorsSucceeded)
			},
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:      "service error",
			namespace: "test-ns",
			body: BatchUpsertVectorsRequest{
				Vectors: []VectorEntity{
					{ID: "1", Vector: []float32{0.1, 0.2}},
				},
			},
			setupMock: func(m *MockVectorService) {
				m.On("BatchUpsert", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "batch_upsert", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/"+tt.namespace+"/vectors/batch", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleBatchDeleteVectors(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful batch delete",
			namespace: "test-ns",
			body: BatchDeleteVectorsRequest{
				IDs: []string{"1", "2", "3"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("DeleteVector", mock.Anything, mock.Anything).Return(&service.DeleteVectorResponse{
					Success:   true,
					Timestamp: time.Now().UnixNano(),
				}, nil).Times(3)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp BatchDeleteVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(3), resp.DeletedCount)
			},
		},
		{
			name:      "empty IDs array",
			namespace: "test-ns",
			body: BatchDeleteVectorsRequest{
				IDs: []string{},
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "ids array is required")
			},
		},
		{
			name:      "string vector IDs accepted",
			namespace: "test-ns",
			body: BatchDeleteVectorsRequest{
				IDs: []string{"invalid-id", "2"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("DeleteVector", mock.Anything, mock.Anything).Return(&service.DeleteVectorResponse{
					Success:   true,
					Timestamp: time.Now().UnixNano(),
				}, nil).Times(2)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp BatchDeleteVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(2), resp.DeletedCount)
			},
		},
		{
			name:      "partial failure",
			namespace: "test-ns",
			body: BatchDeleteVectorsRequest{
				IDs: []string{"1", "2"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("DeleteVector", mock.Anything, mock.Anything).Return(&service.DeleteVectorResponse{
					Success:   true,
					Timestamp: time.Now().UnixNano(),
				}, nil).Once()
				m.On("DeleteVector", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrNotFound, Op: "delete", Err: &errors.MessageError{Message: "not found"}}).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp BatchDeleteVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(1), resp.DeletedCount)
			},
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/"+tt.namespace+"/vectors/batch", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleUpdateMetadata(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		id             string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful metadata update",
			namespace: "test-ns",
			id:        "123",
			body: map[string]interface{}{
				"metadata": map[string]string{"key": "updated-value"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found:   true,
					ID:      123,
					Vector:  []float32{0.1, 0.2, 0.3},
					Version: 1,
				}, nil).Once()
				m.On("UpsertVector", mock.Anything, mock.Anything).Return(&service.UpsertVectorResponse{
					ID:        123,
					Version:   2,
					Timestamp: time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp UpsertVectorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "123", resp.ID)
				assert.Equal(t, int64(2), resp.Version)
			},
		},
		{
			name:      "vector not found",
			namespace: "test-ns",
			id:        "999",
			body: map[string]interface{}{
				"metadata": map[string]string{"key": "value"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found: false,
				}, nil).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:      "string vector ID accepted",
			namespace: "test-ns",
			id:        "invalid-id",
			body: map[string]interface{}{
				"metadata": map[string]string{"key": "value"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found:   true,
					ID:      123,
					Vector:  []float32{0.1, 0.2, 0.3},
					Version: 1,
				}, nil).Once()
				m.On("UpsertVector", mock.Anything, mock.Anything).Return(&service.UpsertVectorResponse{
					ID:        123,
					Version:   2,
					Timestamp: time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp UpsertVectorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "invalid-id", resp.ID)
				assert.Equal(t, int64(2), resp.Version)
			},
		},
		{
			name:      "missing metadata",
			namespace: "test-ns",
			id:        "123",
			body: map[string]interface{}{
				"other": "field",
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "metadata is required")
			},
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			id:             "123",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:      "get vector service error",
			namespace: "test-ns",
			id:        "123",
			body: map[string]interface{}{
				"metadata": map[string]string{"key": "value"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "get", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:      "upsert service error",
			namespace: "test-ns",
			id:        "123",
			body: map[string]interface{}{
				"metadata": map[string]string{"key": "value"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found:  true,
					ID:     123,
					Vector: []float32{0.1, 0.2},
				}, nil).Once()
				m.On("UpsertVector", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "upsert", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPut, "/api/v1/vector/namespaces/"+tt.namespace+"/vectors/"+tt.id+"/metadata", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Search Handler Tests
// ========================================

func TestHandleSearchByID(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		id             string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful search by ID",
			namespace: "test-ns",
			id:        "123",
			body: SearchByIDRequest{
				K:               5,
				IncludeVectors:  true,
				IncludeMetadata: true,
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found:  true,
					ID:     123,
					Vector: []float32{0.1, 0.2, 0.3},
				}, nil).Once()
				m.On("SearchSimilar", mock.Anything, mock.MatchedBy(func(req *service.SearchSimilarRequest) bool {
					return req.K == 6 // K+1
				})).Return(&service.SearchSimilarResponse{
					Results: []*service.VectorSearchResult{
						{ID: 123, Score: 1.0, Rank: 1},
						{ID: 124, Score: 0.95, Rank: 2},
						{ID: 125, Score: 0.90, Rank: 3},
					},
					QueryDuration: 1000,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp SearchByIDResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Results, 2) // Excludes self by default
			},
		},
		{
			name:      "include self in results",
			namespace: "test-ns",
			id:        "123",
			body: SearchByIDRequest{
				K:           5,
				IncludeSelf: true,
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found:  true,
					ID:     123,
					Vector: []float32{0.1, 0.2, 0.3},
				}, nil).Once()
				m.On("SearchSimilar", mock.Anything, mock.Anything).Return(&service.SearchSimilarResponse{
					Results: []*service.VectorSearchResult{
						{ID: 123, Score: 1.0, Rank: 1},
						{ID: 124, Score: 0.95, Rank: 2},
					},
					QueryDuration: 1000,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp SearchByIDResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Results, 2)
			},
		},
		{
			name:      "vector not found",
			namespace: "test-ns",
			id:        "999",
			body: SearchByIDRequest{
				K: 5,
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found: false,
				}, nil).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:      "string vector ID accepted",
			namespace: "test-ns",
			id:        "invalid-id",
			body: SearchByIDRequest{
				K: 5,
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found:  true,
					ID:     123,
					Vector: []float32{0.1, 0.2},
				}, nil).Once()
				m.On("SearchSimilar", mock.Anything, mock.Anything).Return(&service.SearchSimilarResponse{
					Results: []*service.VectorSearchResult{},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:      "invalid k value",
			namespace: "test-ns",
			id:        "123",
			body: SearchByIDRequest{
				K: 0,
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "k must be positive")
			},
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			id:             "123",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:      "get vector service error",
			namespace: "test-ns",
			id:        "123",
			body: SearchByIDRequest{
				K: 5,
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "get", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:      "search service error",
			namespace: "test-ns",
			id:        "123",
			body: SearchByIDRequest{
				K: 5,
			},
			setupMock: func(m *MockVectorService) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found:  true,
					ID:     123,
					Vector: []float32{0.1, 0.2},
				}, nil).Once()
				m.On("SearchSimilar", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "search", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/"+tt.namespace+"/search/by-id/"+tt.id, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Hybrid Search Handler Tests
// ========================================

func TestHandleHybridSearch(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful hybrid search",
			body: HybridSearchRequest{
				Namespace:       "test-ns",
				QueryVector:     []float32{0.1, 0.2, 0.3},
				K:               10,
				MetadataFilters: map[string]string{"category": "electronics"},
				IncludeMetadata: true,
				IncludeVectors:  true,
			},
			setupMock: func(m *MockVectorService) {
				m.On("HybridSearch", mock.Anything, mock.MatchedBy(func(req *service.HybridSearchRequest) bool {
					return req.Namespace == "test-ns" && req.K == 10 && len(req.QueryVector) == 3
				})).Return(&service.HybridSearchResponse{
					Results: []*service.VectorSearchResult{
						{ID: 1, Score: 0.95, Rank: 1, Vector: []float32{0.1, 0.2, 0.3}, Metadata: map[string]interface{}{"category": "electronics"}},
						{ID: 2, Score: 0.90, Rank: 2, Vector: []float32{0.2, 0.3, 0.4}, Metadata: map[string]interface{}{"category": "electronics"}},
					},
					TotalCandidates: 100,
					TotalFiltered:   50,
					QueryDuration:   2000,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp HybridSearchResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Results, 2)
				assert.Equal(t, int64(100), resp.TotalCandidates)
				assert.Equal(t, 50, resp.TotalFiltered)
				assert.Equal(t, int64(2), resp.SearchTimeMs)
				assert.Equal(t, "1", resp.Results[0].ID)
				assert.Equal(t, float32(0.95), resp.Results[0].Score)
			},
		},
		{
			name: "hybrid search with filter operator",
			body: HybridSearchRequest{
				Namespace:          "test-ns",
				QueryVector:        []float32{0.5, 0.5},
				K:                  5,
				MetadataFilters:    map[string]string{"type": "doc", "status": "active"},
				FilterOperator:     "AND",
				RerankingAlgorithm: "mmr",
				EfSearch:           200,
				MinScore:           0.5,
			},
			setupMock: func(m *MockVectorService) {
				m.On("HybridSearch", mock.Anything, mock.MatchedBy(func(req *service.HybridSearchRequest) bool {
					return req.FilterOperator == "AND" && req.RerankingAlgorithm == "mmr" &&
						req.EfSearch == 200 && req.MinScore == 0.5
				})).Return(&service.HybridSearchResponse{
					Results:         []*service.VectorSearchResult{},
					TotalCandidates: 0,
					TotalFiltered:   0,
					QueryDuration:   500,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp HybridSearchResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Results, 0)
			},
		},
		{
			name: "missing namespace",
			body: HybridSearchRequest{
				QueryVector: []float32{0.1, 0.2},
				K:           5,
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "namespace is required")
			},
		},
		{
			name: "missing query vector",
			body: HybridSearchRequest{
				Namespace: "test-ns",
				K:         5,
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "query_vector is required")
			},
		},
		{
			name: "invalid k value zero",
			body: HybridSearchRequest{
				Namespace:   "test-ns",
				QueryVector: []float32{0.1, 0.2},
				K:           0,
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "k must be positive")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "invalid JSON body")
			},
		},
		{
			name: "service internal error",
			body: HybridSearchRequest{
				Namespace:   "test-ns",
				QueryVector: []float32{0.1, 0.2},
				K:           5,
			},
			setupMock: func(m *MockVectorService) {
				m.On("HybridSearch", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "hybrid_search", Err: &errors.MessageError{Message: "database error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "namespace not found error",
			body: HybridSearchRequest{
				Namespace:   "nonexistent-ns",
				QueryVector: []float32{0.1, 0.2},
				K:           5,
			},
			setupMock: func(m *MockVectorService) {
				m.On("HybridSearch", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrNotFound, Op: "hybrid_search", Err: &errors.MessageError{Message: "namespace not found"}}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/hybrid-search", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Search By Metadata Handler Tests
// ========================================

func TestHandleSearchByMetadata(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful metadata search",
			body: SearchByMetadataRequest{
				Namespace:       "test-ns",
				Filters:         map[string]string{"category": "books", "status": "available"},
				IncludeMetadata: true,
				IncludeVectors:  true,
				Limit:           50,
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Namespace == "test-ns" && req.Limit == 50 && req.IncludeMetadata && req.IncludeVectors
				})).Return(&service.SearchByMetadataResponse{
					Results: []*service.VectorData{
						{ID: 1, Vector: []float32{0.1, 0.2}, Metadata: map[string]interface{}{"category": "books"}, Version: 1, CreatedAt: time.Now().UnixNano()},
						{ID: 2, Vector: []float32{0.3, 0.4}, Metadata: map[string]interface{}{"category": "books"}, Version: 2, CreatedAt: time.Now().UnixNano()},
					},
					TotalCount: 2,
					NextCursor: "",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp SearchByMetadataResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Results, 2)
				assert.Equal(t, int64(2), resp.TotalCount)
				assert.Equal(t, "1", resp.Results[0].ID)
				assert.Equal(t, "2", resp.Results[1].ID)
			},
		},
		{
			name: "metadata search with pagination",
			body: SearchByMetadataRequest{
				Namespace: "test-ns",
				Filters:   map[string]string{"type": "document"},
				Limit:     10,
				Offset:    20,
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Limit == 10 && req.Offset == 20
				})).Return(&service.SearchByMetadataResponse{
					Results: []*service.VectorData{
						{ID: 21, Metadata: map[string]interface{}{"type": "document"}},
					},
					TotalCount: 100,
					NextCursor: "cursor_30",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp SearchByMetadataResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Results, 1)
				assert.Equal(t, int64(100), resp.TotalCount)
				assert.Equal(t, "cursor_30", resp.NextCursor)
			},
		},
		{
			name: "metadata search with filter operator",
			body: SearchByMetadataRequest{
				Namespace:      "test-ns",
				Filters:        map[string]string{"color": "red", "size": "large"},
				FilterOperator: "OR",
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.FilterOperator == "OR"
				})).Return(&service.SearchByMetadataResponse{
					Results:    []*service.VectorData{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "metadata search with default limit",
			body: SearchByMetadataRequest{
				Namespace: "test-ns",
				Filters:   map[string]string{"key": "value"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Limit == 100 // default limit
				})).Return(&service.SearchByMetadataResponse{
					Results:    []*service.VectorData{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "metadata search with limit capped at 1000",
			body: SearchByMetadataRequest{
				Namespace: "test-ns",
				Filters:   map[string]string{"key": "value"},
				Limit:     5000,
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Limit == 1000 // capped at 1000
				})).Return(&service.SearchByMetadataResponse{
					Results:    []*service.VectorData{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "metadata search with negative offset corrected",
			body: SearchByMetadataRequest{
				Namespace: "test-ns",
				Filters:   map[string]string{"key": "value"},
				Offset:    -10,
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Offset == 0 // corrected to 0
				})).Return(&service.SearchByMetadataResponse{
					Results:    []*service.VectorData{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "missing namespace",
			body: SearchByMetadataRequest{
				Filters: map[string]string{"key": "value"},
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "namespace is required")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "invalid JSON body")
			},
		},
		{
			name: "service internal error",
			body: SearchByMetadataRequest{
				Namespace: "test-ns",
				Filters:   map[string]string{"key": "value"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "search_metadata", Err: &errors.MessageError{Message: "database error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "namespace not found",
			body: SearchByMetadataRequest{
				Namespace: "nonexistent-ns",
				Filters:   map[string]string{"key": "value"},
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrNotFound, Op: "search_metadata", Err: &errors.MessageError{Message: "namespace not found"}}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/search-by-metadata", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Global Stats Handler Tests (Cluster Stats Alias)
// ========================================

func TestHandleGetGlobalStats(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful cluster stats",
			setupMock: func(m *MockVectorService) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{
						{Namespace: "ns1", VectorCount: 1000},
						{Namespace: "ns2", VectorCount: 2000},
						{Namespace: "ns3", VectorCount: 3000},
					},
					TotalCount: 3,
				}, nil).Once()
				m.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
					return req.Namespace == "ns1"
				})).Return(&service.GetIndexStatsResponse{
					Namespace:      "ns1",
					VectorCount:    1000,
					IndexSizeBytes: 10240,
				}, nil).Once()
				m.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
					return req.Namespace == "ns2"
				})).Return(&service.GetIndexStatsResponse{
					Namespace:      "ns2",
					VectorCount:    2000,
					IndexSizeBytes: 20480,
				}, nil).Once()
				m.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
					return req.Namespace == "ns3"
				})).Return(&service.GetIndexStatsResponse{
					Namespace:      "ns3",
					VectorCount:    3000,
					IndexSizeBytes: 30720,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterStatsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "quicbaas-cluster", resp.ClusterID)
				assert.Equal(t, 1, resp.TotalNodes)
				assert.Equal(t, int64(6000), resp.TotalVectors)
				assert.Equal(t, 3, resp.TotalNamespaces)
				assert.Equal(t, int64(61440), resp.TotalStorageBytes)
				assert.Equal(t, int64(61440), resp.TotalIndexBytes)
				assert.Len(t, resp.NamespaceStats, 3)
				assert.False(t, resp.Timestamp.IsZero())
				assert.Empty(t, resp.NodeStats) // Empty slice returned, omitempty causes nil after JSON round-trip
			},
		},
		{
			name: "cluster stats with empty cluster",
			setupMock: func(m *MockVectorService) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterStatsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(0), resp.TotalVectors)
				assert.Equal(t, 0, resp.TotalNamespaces)
				assert.Len(t, resp.NamespaceStats, 0)
			},
		},
		{
			name: "cluster stats with partial namespace stats failure",
			setupMock: func(m *MockVectorService) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{
						{Namespace: "ns1", VectorCount: 100},
						{Namespace: "ns2", VectorCount: 200},
					},
					TotalCount: 2,
				}, nil).Once()
				m.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
					return req.Namespace == "ns1"
				})).Return(&service.GetIndexStatsResponse{
					Namespace:      "ns1",
					VectorCount:    100,
					IndexSizeBytes: 1024,
				}, nil).Once()
				m.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
					return req.Namespace == "ns2"
				})).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "stats", Err: &errors.MessageError{Message: "error"}}).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterStatsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(100), resp.TotalVectors)
				assert.Len(t, resp.NamespaceStats, 1)
			},
		},
		{
			name: "list namespaces error",
			setupMock: func(m *MockVectorService) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "list", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "global stats route serves the same handler",
			path: "/api/v1/vector/admin/stats/global",
			setupMock: func(m *MockVectorService) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{
						{Namespace: "ns1", VectorCount: 100},
					},
					TotalCount: 1,
				}, nil).Once()
				m.On("GetIndexStats", mock.Anything, mock.Anything).Return(&service.GetIndexStatsResponse{
					Namespace:      "ns1",
					VectorCount:    100,
					IndexSizeBytes: 1024,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterStatsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(100), resp.TotalVectors)
				assert.Equal(t, 1, resp.TotalNamespaces)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			path := tt.path
			if path == "" {
				path = "/api/v1/vector/cluster/stats"
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Index Management Handler Tests
// ========================================

func TestHandleRebuildIndex(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful index rebuild",
			namespace: "test-ns",
			body: RebuildIndexRequest{
				IndexParams: map[string]string{"m": "16"},
				Async:       true,
			},
			setupMock: func(m *MockVectorService) {
				m.On("RebuildIndex", mock.Anything, mock.MatchedBy(func(req *service.RebuildIndexRequest) bool {
					return req.Namespace == "test-ns" && req.Async
				})).Return(&service.RebuildIndexResponse{
					JobID:                  "job-123",
					Timestamp:              time.Now().UnixNano(),
					EstimatedTimeRemaining: 30,
				}, nil).Once()
			},
			expectedStatus: http.StatusAccepted,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RebuildIndexResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "job-123", resp.OperationID)
			},
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:      "service error",
			namespace: "test-ns",
			body:      RebuildIndexRequest{},
			setupMock: func(m *MockVectorService) {
				m.On("RebuildIndex", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "rebuild", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/"+tt.namespace+"/index/rebuild", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleGetIndexProgress(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		queryParams    string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "get index progress with operation ID",
			namespace:      "test-ns",
			queryParams:    "?operation_id=job-123",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp IndexProgressResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "job-123", resp.OperationID)
				assert.Equal(t, "in_progress", resp.Status)
				assert.Empty(t, resp.ErrorMessage)
				assert.Equal(t, "test-ns", resp.Namespace)
			},
		},
		{
			name:           "missing operation ID",
			namespace:      "test-ns",
			queryParams:    "",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "operation_id is required")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			url := "/api/v1/vector/namespaces/" + tt.namespace + "/index/progress" + tt.queryParams
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Admin Handler Tests
// ========================================

func TestHandleCompactNamespace(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful compaction request",
			body: map[string]interface{}{
				"namespace": "test-ns",
			},
			expectedStatus: http.StatusAccepted,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp CompactNamespaceResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotEmpty(t, resp.OperationID)
			},
		},
		{
			name: "missing namespace",
			body: map[string]interface{}{
				"other": "field",
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "namespace is required")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/admin/compact", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleGetCompactionProgress(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "get compaction progress with operation ID",
			queryParams:    "?operation_id=op-123",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp CompactionProgressResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "op-123", resp.OperationID)
			},
		},
		{
			name:           "missing operation ID",
			queryParams:    "",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "operation_id is required")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			url := "/api/v1/vector/admin/compact/progress" + tt.queryParams
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleExportVectors(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		body           interface{}
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful export",
			namespace: "test-ns",
			body: ExportVectorsRequest{
				Format:    "json",
				BatchSize: 100,
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.Anything).Return(&service.SearchByMetadataResponse{
					Results: []*service.VectorData{
						{ID: 1, Vector: []float32{0.1, 0.2}, Metadata: map[string]interface{}{"key": "val1"}},
						{ID: 2, Vector: []float32{0.3, 0.4}, Metadata: map[string]interface{}{"key": "val2"}},
					},
					TotalCount: 2,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
				assert.Contains(t, w.Header().Get("Content-Disposition"), "test-ns-export.json")
			},
		},
		{
			name:      "export with defaults",
			namespace: "test-ns",
			body:      ExportVectorsRequest{},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Limit == 1000
				})).Return(&service.SearchByMetadataResponse{
					Results:    []*service.VectorData{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:      "export multiple batches",
			namespace: "test-ns",
			body: ExportVectorsRequest{
				Format:    "json",
				BatchSize: 2,
			},
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Offset == 0
				})).Return(&service.SearchByMetadataResponse{
					Results: []*service.VectorData{
						{ID: 1, Vector: []float32{0.1}},
						{ID: 2, Vector: []float32{0.2}},
					},
					TotalCount: 3,
				}, nil).Once()
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Offset == 2
				})).Return(&service.SearchByMetadataResponse{
					Results: []*service.VectorData{
						{ID: 3, Vector: []float32{0.3}},
					},
					TotalCount: 3,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/admin/export/"+tt.namespace, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleImportVectors(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		body           string
		setupMock      func(*MockVectorService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful import",
			namespace: "test-ns",
			body: `{"id":"1","vector":[0.1,0.2],"metadata":{"key":"val1"}}
{"id":"2","vector":[0.3,0.4],"metadata":{"key":"val2"}}`,
			setupMock: func(m *MockVectorService) {
				m.On("BatchUpsert", mock.Anything, mock.Anything).Return(&service.BatchUpsertResponse{
					InsertedCount: 2,
					Timestamp:     time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ImportVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(2), resp.VectorsImported)
				assert.Equal(t, int64(0), resp.VectorsSkipped)
			},
		},
		{
			name:      "import with batch processing",
			namespace: "test-ns",
			body:      strings.Repeat(`{"id":"1","vector":[0.1,0.2]}`+"\n", 150),
			setupMock: func(m *MockVectorService) {
				m.On("BatchUpsert", mock.Anything, mock.Anything).Return(&service.BatchUpsertResponse{
					InsertedCount: 100,
					Timestamp:     time.Now().UnixNano(),
				}, nil).Once()
				m.On("BatchUpsert", mock.Anything, mock.Anything).Return(&service.BatchUpsertResponse{
					InsertedCount: 50,
					Timestamp:     time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ImportVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(150), resp.VectorsImported)
			},
		},
		{
			name:      "skip invalid vectors",
			namespace: "test-ns",
			body: `{"id":"","vector":[0.1,0.2]}
{"id":"2","vector":[]}
{"id":"3","vector":[0.3,0.4]}`,
			setupMock: func(m *MockVectorService) {
				m.On("BatchUpsert", mock.Anything, mock.Anything).Return(&service.BatchUpsertResponse{
					InsertedCount: 1,
					Timestamp:     time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ImportVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(1), resp.VectorsImported)
				assert.Equal(t, int64(2), resp.VectorsSkipped)
			},
		},
		{
			name:      "string vector ID accepted",
			namespace: "test-ns",
			body:      `{"id":"invalid-id","vector":[0.1,0.2]}`,
			setupMock: func(m *MockVectorService) {
				m.On("BatchUpsert", mock.Anything, mock.Anything).Return(&service.BatchUpsertResponse{
					IDs: []uint64{1},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ImportVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(1), resp.VectorsImported)
			},
		},
		{
			name:      "batch upsert error",
			namespace: "test-ns",
			body:      `{"id":"1","vector":[0.1,0.2]}`,
			setupMock: func(m *MockVectorService) {
				m.On("BatchUpsert", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "batch_upsert", Err: &errors.MessageError{Message: "error"}}).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ImportVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, int64(1), resp.VectorsFailed)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/admin/import/"+tt.namespace, bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// ========================================
// Test Helper: MockVectorService
// ========================================

// MockVectorService is a mock implementation of VectorService
type MockVectorService struct {
	mock.Mock
}

func (m *MockVectorService) CreateNamespace(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CreateNamespaceResponse), args.Error(1)
}

func (m *MockVectorService) DeleteNamespace(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteNamespaceResponse), args.Error(1)
}

func (m *MockVectorService) ListNamespaces(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListNamespacesResponse), args.Error(1)
}

func (m *MockVectorService) GetNamespaceInfo(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetNamespaceInfoResponse), args.Error(1)
}

func (m *MockVectorService) UpsertVector(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.UpsertVectorResponse), args.Error(1)
}

func (m *MockVectorService) GetVector(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetVectorResponse), args.Error(1)
}

func (m *MockVectorService) DeleteVector(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteVectorResponse), args.Error(1)
}

func (m *MockVectorService) BatchUpsert(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchUpsertResponse), args.Error(1)
}

func (m *MockVectorService) SearchSimilar(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SearchSimilarResponse), args.Error(1)
}

func (m *MockVectorService) SearchByMetadata(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SearchByMetadataResponse), args.Error(1)
}

func (m *MockVectorService) HybridSearch(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.HybridSearchResponse), args.Error(1)
}

func (m *MockVectorService) RebuildIndex(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RebuildIndexResponse), args.Error(1)
}

func (m *MockVectorService) GetIndexStats(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetIndexStatsResponse), args.Error(1)
}

func (m *MockVectorService) BatchDelete(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchDeleteResponse), args.Error(1)
}

func (m *MockVectorService) CompactNamespace(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CompactNamespaceResponse), args.Error(1)
}

func (m *MockVectorService) GetCompactionProgress(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetCompactionProgressResponse), args.Error(1)
}

func (m *MockVectorService) GetRebuildProgress(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetRebuildProgressResponse), args.Error(1)
}

func (m *MockVectorService) BatchDeleteVectors(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchDeleteVectorsResponse), args.Error(1)
}

func (m *MockVectorService) ListVectors(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListVectorsResponse), args.Error(1)
}

func (m *MockVectorService) UpdateMetadata(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.UpdateVectorMetadataResponse), args.Error(1)
}

func (m *MockVectorService) SearchByID(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SearchByIDResponse), args.Error(1)
}

// ========================================
// Additional Edge Case Tests
// ========================================

func TestHandleListVectorsErrors(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		setupMock      func(*MockVectorService)
		expectedStatus int
	}{
		{
			name: "service error",
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "search", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:        "with high limit capped at 1000",
			queryParams: "?limit=5000&offset=100",
			setupMock: func(m *MockVectorService) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Limit == 1000 && req.Offset == 100
				})).Return(&service.SearchByMetadataResponse{
					Results:    []*service.VectorData{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			mockVectorSvc := &MockVectorService{}
			mockSvc.VectorService = mockVectorSvc

			if tt.setupMock != nil {
				tt.setupMock(mockVectorSvc)
			}

			url := "/api/v1/vector/namespaces/test-ns/vectors"
			if tt.queryParams != "" {
				url += tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}
