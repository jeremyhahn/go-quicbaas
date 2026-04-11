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
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// ========================================
// Vector CRUD Handler Tests
// ========================================

func TestHandleUpsertVector(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful vector upsert",
			namespace: "test-ns",
			body: UpsertVectorRequest{
				ID:       "123",
				Vector:   []float32{0.1, 0.2, 0.3},
				Metadata: map[string]string{"key": "value"},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpsertVector", mock.Anything, mock.MatchedBy(func(req *service.UpsertVectorRequest) bool {
					return req.Namespace == "test-ns" && req.ID == 123 && len(req.Vector) == 3
				})).Return(&service.UpsertVectorResponse{
					ID:        123,
					Version:   1,
					Timestamp: time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp UpsertVectorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "123", resp.ID)
				assert.Equal(t, int64(1), resp.Version)
			},
		},
		{
			name:      "missing vector ID",
			namespace: "test-ns",
			body: UpsertVectorRequest{
				Vector: []float32{0.1, 0.2, 0.3},
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "vector ID is required")
			},
		},
		{
			name:      "empty vector data",
			namespace: "test-ns",
			body: UpsertVectorRequest{
				ID:     "123",
				Vector: []float32{},
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "vector data cannot be empty")
			},
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			body:           "invalid-json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:      "string vector ID accepted",
			namespace: "test-ns",
			body: UpsertVectorRequest{
				ID:     "not-a-number",
				Vector: []float32{0.1, 0.2, 0.3},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpsertVector", mock.Anything, mock.Anything).Return(&service.UpsertVectorResponse{
					ID:        1,
					Version:   1,
					Timestamp: time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp UpsertVectorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotEmpty(t, resp.ID)
				assert.Equal(t, int64(1), resp.Version)
			},
		},
		{
			name:      "service error",
			namespace: "test-ns",
			body: UpsertVectorRequest{
				ID:     "123",
				Vector: []float32{0.1, 0.2, 0.3},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpsertVector", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "upsert", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/"+tt.namespace+"/vectors", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("namespace", tt.namespace)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleUpsertVector(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleGetVector(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		id             string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful get vector",
			namespace: "test-ns",
			id:        "123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetVector", mock.Anything, mock.MatchedBy(func(req *service.GetVectorRequest) bool {
					return req.Namespace == "test-ns" && req.ID == 123
				})).Return(&service.GetVectorResponse{
					Found:    true,
					Vector:   []float32{0.1, 0.2, 0.3},
					Metadata: map[string]interface{}{"key": "value"},
					Version:  1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp GetVectorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "123", resp.ID)
				assert.Equal(t, "test-ns", resp.Namespace)
				assert.Len(t, resp.Vector, 3)
			},
		},
		{
			name:      "vector not found",
			namespace: "test-ns",
			id:        "999",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found: false,
				}, nil).Once()
			},
			expectedStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "vector 999")
			},
		},
		{
			name:        "with query parameters",
			namespace:   "test-ns",
			id:          "123",
			queryParams: "?include_vector=false&include_metadata=false",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetVector", mock.Anything, mock.MatchedBy(func(req *service.GetVectorRequest) bool {
					return req.IncludeVector == false && req.IncludeMetadata == false
				})).Return(&service.GetVectorResponse{
					Found:   true,
					Version: 1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:      "string vector ID accepted",
			namespace: "test-ns",
			id:        "not-a-number",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(&service.GetVectorResponse{
					Found:    true,
					Vector:   []float32{0.1, 0.2, 0.3},
					Metadata: map[string]interface{}{"key": "value"},
					Version:  1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp GetVectorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotEmpty(t, resp.ID)
				assert.Equal(t, "test-ns", resp.Namespace)
			},
		},
		{
			name:      "service error",
			namespace: "test-ns",
			id:        "123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetVector", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "get", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			url := "/api/v1/vector/namespaces/" + tt.namespace + "/vectors/" + tt.id
			if tt.queryParams != "" {
				url += tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("namespace", tt.namespace)
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetVector(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleDeleteVector(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		id             string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful delete",
			namespace: "test-ns",
			id:        "123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteVector", mock.Anything, mock.MatchedBy(func(req *service.DeleteVectorRequest) bool {
					return req.Namespace == "test-ns" && req.ID == 123
				})).Return(&service.DeleteVectorResponse{
					Success:   true,
					Timestamp: time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp DeleteVectorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name:      "string vector ID accepted",
			namespace: "test-ns",
			id:        "not-a-number",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteVector", mock.Anything, mock.Anything).Return(&service.DeleteVectorResponse{
					Success:   true,
					Timestamp: time.Now().UnixNano(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp DeleteVectorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name:      "service error",
			namespace: "test-ns",
			id:        "123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteVector", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrNotFound, Op: "delete", Err: &errors.MessageError{Message: "not found"}}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/"+tt.namespace+"/vectors/"+tt.id, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("namespace", tt.namespace)
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleDeleteVector(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleListVectors(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful list",
			namespace: "test-ns",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Namespace == "test-ns" && req.Limit == 100
				})).Return(&service.SearchByMetadataResponse{
					Results: []*service.VectorData{
						{ID: 1, Metadata: map[string]interface{}{"key": "val1"}},
						{ID: 2, Metadata: map[string]interface{}{"key": "val2"}},
					},
					TotalCount: 2,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListVectorsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Vectors, 2)
				assert.Equal(t, int64(2), resp.Pagination.TotalCount)
			},
		},
		{
			name:        "with pagination",
			namespace:   "test-ns",
			queryParams: "?limit=50&offset=10",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
					return req.Limit == 50 && req.Offset == 10
				})).Return(&service.SearchByMetadataResponse{
					Results:    []*service.VectorData{},
					TotalCount: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:      "service error",
			namespace: "test-ns",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SearchByMetadata", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "list", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			url := "/api/v1/vector/namespaces/" + tt.namespace + "/vectors"
			if tt.queryParams != "" {
				url += tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("namespace", tt.namespace)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleListVectors(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleSearchSimilar(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:      "successful search",
			namespace: "test-ns",
			body: SearchSimilarRequest{
				QueryVector: []float32{0.1, 0.2, 0.3},
				K:           5,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SearchSimilar", mock.Anything, mock.MatchedBy(func(req *service.SearchSimilarRequest) bool {
					return req.Namespace == "test-ns" && req.K == 5 && len(req.QueryVector) == 3
				})).Return(&service.SearchSimilarResponse{
					Results: []*service.VectorSearchResult{
						{ID: 1, Score: 0.95, Rank: 1, Vector: []float32{0.11, 0.21, 0.31}},
						{ID: 2, Score: 0.85, Rank: 2, Vector: []float32{0.12, 0.22, 0.32}},
					},
					QueryDuration: 5000,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp SearchSimilarResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Results, 2)
				assert.Equal(t, int64(5), resp.SearchTimeMs)
			},
		},
		{
			name:      "empty query vector",
			namespace: "test-ns",
			body: SearchSimilarRequest{
				QueryVector: []float32{},
				K:           5,
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "query_vector is required")
			},
		},
		{
			name:      "invalid k value",
			namespace: "test-ns",
			body: SearchSimilarRequest{
				QueryVector: []float32{0.1, 0.2, 0.3},
				K:           0,
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "k must be positive")
			},
		},
		{
			name:           "invalid JSON body",
			namespace:      "test-ns",
			body:           "invalid-json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:      "service error",
			namespace: "test-ns",
			body: SearchSimilarRequest{
				QueryVector: []float32{0.1, 0.2, 0.3},
				K:           5,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SearchSimilar", mock.Anything, mock.Anything).Return(nil,
					&errors.QBError{Code: errors.ErrInternal, Op: "search", Err: &errors.MessageError{Message: "internal error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/"+tt.namespace+"/search/similar", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("namespace", tt.namespace)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleSearchSimilar(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}
