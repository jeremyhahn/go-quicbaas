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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// MockParameterStoreService is a mock implementation of ParameterStoreService.
type MockParameterStoreService struct {
	mock.Mock
}

func (m *MockParameterStoreService) GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.GetParameterResponse), args.Error(1)
}

func (m *MockParameterStoreService) SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.SetParameterResponse), args.Error(1)
}

func (m *MockParameterStoreService) DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.DeleteParameterResponse), args.Error(1)
}

func (m *MockParameterStoreService) GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.GetParametersResponse), args.Error(1)
}

func (m *MockParameterStoreService) SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.SetParametersResponse), args.Error(1)
}

func (m *MockParameterStoreService) DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.DeleteParametersResponse), args.Error(1)
}

func (m *MockParameterStoreService) ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.ListParametersResponse), args.Error(1)
}

func (m *MockParameterStoreService) SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.SearchByTagsResponse), args.Error(1)
}

func setupMockServerWithParameterStore(t *testing.T) (*Server, *MockParameterStoreService, func()) {
	t.Helper()

	server, mockService, cleanup := setupMockServer(t)

	mockParamService := &MockParameterStoreService{}
	mockService.ParameterStoreSvc = mockParamService

	return server, mockParamService, cleanup
}

func TestHandleGetParameter(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		queryParams    string
		setupMock      func(*MockParameterStoreService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get parameter successfully",
			path: "app/database/host",
			setupMock: func(m *MockParameterStoreService) {
				m.On("GetParameter", mock.Anything, mock.MatchedBy(func(req *interfaces.GetParameterRequest) bool {
					return req.Path == "/app/database/host" && req.WithDecryption
				})).Return(&interfaces.GetParameterResponse{
					Parameter: &interfaces.ParameterValue{
						Path:        "/app/database/host",
						Value:       []byte("localhost"),
						Type:        "string",
						Description: "Database host",
						CreatedAt:   time.Now(),
						UpdatedAt:   time.Now(),
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.GetParameterResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "/app/database/host", resp.Parameter.Path)
				assert.Equal(t, []byte("localhost"), resp.Parameter.Value)
			},
		},
		{
			name:        "get parameter without decryption",
			path:        "app/secrets/key",
			queryParams: "with_decryption=false",
			setupMock: func(m *MockParameterStoreService) {
				m.On("GetParameter", mock.Anything, mock.MatchedBy(func(req *interfaces.GetParameterRequest) bool {
					return req.Path == "/app/secrets/key" && !req.WithDecryption
				})).Return(&interfaces.GetParameterResponse{
					Parameter: &interfaces.ParameterValue{
						Path:        "/app/secrets/key",
						Value:       []byte("encrypted-value"),
						IsEncrypted: true,
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing parameter path",
			path:           "",
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "parameter not found",
			path: "nonexistent/path",
			setupMock: func(m *MockParameterStoreService) {
				m.On("GetParameter", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "GetParameter",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			path: "app/error",
			setupMock: func(m *MockParameterStoreService) {
				m.On("GetParameter", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "GetParameter",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
			defer cleanup()

			tt.setupMock(mockParamService)

			path := "/api/v1/parameters/" + tt.path
			if tt.queryParams != "" {
				path += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetParameter(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleSetParameter(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           interface{}
		setupMock      func(*MockParameterStoreService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "set parameter successfully - create new",
			path: "app/database/host",
			body: map[string]interface{}{
				"value":       []byte("localhost:5432"),
				"type":        "string",
				"description": "Database connection string",
				"tags": map[string]string{
					"env":     "production",
					"service": "database",
				},
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("SetParameter", mock.Anything, mock.MatchedBy(func(req *interfaces.SetParameterRequest) bool {
					return req.Path == "/app/database/host" &&
						req.Type == "string" &&
						req.Description == "Database connection string"
				})).Return(&interfaces.SetParameterResponse{
					Path:      "/app/database/host",
					Created:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.SetParameterResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "/app/database/host", resp.Path)
				assert.True(t, resp.Created)
			},
		},
		{
			name: "set parameter successfully - update existing",
			path: "app/database/host",
			body: map[string]interface{}{
				"value":     []byte("newhost:5432"),
				"overwrite": true,
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("SetParameter", mock.Anything, mock.Anything).Return(&interfaces.SetParameterResponse{
					Path:      "/app/database/host",
					Created:   false,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.SetParameterResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.False(t, resp.Created)
			},
		},
		{
			name: "set parameter with encryption",
			path: "app/secrets/api-key",
			body: map[string]interface{}{
				"value":      []byte("super-secret-key"),
				"kms_key_id": "default",
				"type":       "string",
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("SetParameter", mock.Anything, mock.MatchedBy(func(req *interfaces.SetParameterRequest) bool {
					return req.Path == "/app/secrets/api-key" && req.KMSKeyID == "default"
				})).Return(&interfaces.SetParameterResponse{
					Path:      "/app/secrets/api-key",
					Created:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "missing parameter path",
			path:           "",
			body:           map[string]interface{}{"value": []byte("test")},
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			path:           "app/test",
			body:           "invalid-json",
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing value",
			path:           "app/test",
			body:           map[string]interface{}{"description": "test"},
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			path: "app/error",
			body: map[string]interface{}{
				"value": []byte("test"),
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("SetParameter", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "SetParameter",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
			defer cleanup()

			tt.setupMock(mockParamService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			path := "/api/v1/parameters/" + tt.path
			req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleSetParameter(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleDeleteParameter(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*MockParameterStoreService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "delete parameter successfully",
			path: "app/database/host",
			setupMock: func(m *MockParameterStoreService) {
				m.On("DeleteParameter", mock.Anything, mock.MatchedBy(func(req *interfaces.DeleteParameterRequest) bool {
					return req.Path == "/app/database/host"
				})).Return(&interfaces.DeleteParameterResponse{
					Path:      "/app/database/host",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.DeleteParameterResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "/app/database/host", resp.Path)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name:           "missing parameter path",
			path:           "",
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "parameter not found",
			path: "nonexistent/path",
			setupMock: func(m *MockParameterStoreService) {
				m.On("DeleteParameter", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "DeleteParameter",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
			defer cleanup()

			tt.setupMock(mockParamService)

			path := "/api/v1/parameters/" + tt.path
			req := httptest.NewRequest(http.MethodDelete, path, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleDeleteParameter(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleListParameters(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		setupMock      func(*MockParameterStoreService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:        "list parameters with prefix",
			queryParams: "prefix=/app/&recursive=true",
			setupMock: func(m *MockParameterStoreService) {
				m.On("ListParameters", mock.Anything, mock.MatchedBy(func(req *interfaces.ListParametersRequest) bool {
					return req.Prefix == "/app/" && req.Recursive
				})).Return(&interfaces.ListParametersResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/database/host", Type: "string"},
						{Path: "/app/database/port", Type: "number"},
					},
					HasMore:    false,
					TotalCount: 2,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.ListParametersResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Parameters, 2)
				assert.Equal(t, int64(2), resp.TotalCount)
			},
		},
		{
			name:        "list parameters with pagination",
			queryParams: "limit=10&cursor=abc123",
			setupMock: func(m *MockParameterStoreService) {
				m.On("ListParameters", mock.Anything, mock.MatchedBy(func(req *interfaces.ListParametersRequest) bool {
					return req.Limit == 10 && req.Cursor == "abc123"
				})).Return(&interfaces.ListParametersResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/config1", Type: "string"},
					},
					NextCursor: "next-cursor",
					HasMore:    true,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.ListParametersResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "next-cursor", resp.NextCursor)
				assert.True(t, resp.HasMore)
			},
		},
		{
			name:        "list parameters with values",
			queryParams: "with_values=true&with_decryption=true",
			setupMock: func(m *MockParameterStoreService) {
				m.On("ListParameters", mock.Anything, mock.MatchedBy(func(req *interfaces.ListParametersRequest) bool {
					return req.WithValues && req.WithDecryption
				})).Return(&interfaces.ListParametersResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/test", Value: []byte("test-value")},
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "service error",
			queryParams: "",
			setupMock: func(m *MockParameterStoreService) {
				m.On("ListParameters", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "ListParameters",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
			defer cleanup()

			tt.setupMock(mockParamService)

			path := "/api/v1/parameters"
			if tt.queryParams != "" {
				path += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()

			server.HandleListParameters(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleGetParametersBatch(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockParameterStoreService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "batch get parameters successfully",
			body: map[string]interface{}{
				"paths":           []string{"/app/db/host", "/app/db/port"},
				"with_decryption": true,
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("GetParameters", mock.Anything, mock.MatchedBy(func(req *interfaces.GetParametersRequest) bool {
					return len(req.Paths) == 2 && req.WithDecryption
				})).Return(&interfaces.GetParametersResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/db/host", Value: []byte("localhost")},
						{Path: "/app/db/port", Value: []byte("5432")},
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.GetParametersResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Parameters, 2)
			},
		},
		{
			name: "batch get with partial errors",
			body: map[string]interface{}{
				"paths": []string{"/app/exists", "/app/missing"},
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("GetParameters", mock.Anything, mock.Anything).Return(&interfaces.GetParametersResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/exists", Value: []byte("value")},
					},
					Errors: map[string]string{
						"/app/missing": "parameter not found",
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.GetParametersResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Parameters, 1)
				assert.Contains(t, resp.Errors, "/app/missing")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "empty paths array",
			body: map[string]interface{}{
				"paths": []string{},
			},
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
			defer cleanup()

			tt.setupMock(mockParamService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/get", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleGetParametersBatch(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleSetParametersBatch(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockParameterStoreService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "batch set parameters successfully",
			body: map[string]interface{}{
				"parameters": []map[string]interface{}{
					{"path": "/app/db/host", "value": []byte("localhost"), "type": "string"},
					{"path": "/app/db/port", "value": []byte("5432"), "type": "number"},
				},
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("SetParameters", mock.Anything, mock.MatchedBy(func(req *interfaces.SetParametersRequest) bool {
					return len(req.Parameters) == 2
				})).Return(&interfaces.SetParametersResponse{
					Results: []*interfaces.SetParameterResult{
						{Path: "/app/db/host", Created: true, Success: true},
						{Path: "/app/db/port", Created: true, Success: true},
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.SetParametersResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Results, 2)
				assert.True(t, resp.Results[0].Success)
			},
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "empty parameters array",
			body: map[string]interface{}{
				"parameters": []map[string]interface{}{},
			},
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing path in parameter",
			body: map[string]interface{}{
				"parameters": []map[string]interface{}{
					{"value": []byte("test")},
				},
			},
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing value in parameter",
			body: map[string]interface{}{
				"parameters": []map[string]interface{}{
					{"path": "/app/test"},
				},
			},
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
			defer cleanup()

			tt.setupMock(mockParamService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/set", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleSetParametersBatch(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleDeleteParametersBatch(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockParameterStoreService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "batch delete parameters successfully",
			body: map[string]interface{}{
				"paths": []string{"/app/db/host", "/app/db/port"},
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("DeleteParameters", mock.Anything, mock.MatchedBy(func(req *interfaces.DeleteParametersRequest) bool {
					return len(req.Paths) == 2
				})).Return(&interfaces.DeleteParametersResponse{
					Results: []*interfaces.DeleteParameterResult{
						{Path: "/app/db/host", Deleted: true},
						{Path: "/app/db/port", Deleted: true},
					},
					DeletedCount: 2,
					Timestamp:    time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.DeleteParametersResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 2, resp.DeletedCount)
			},
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "empty paths array",
			body: map[string]interface{}{
				"paths": []string{},
			},
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
			defer cleanup()

			tt.setupMock(mockParamService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/delete", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleDeleteParametersBatch(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleSearchParametersByTags(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockParameterStoreService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "search by tags successfully",
			body: map[string]interface{}{
				"tags": map[string]string{
					"env":     "production",
					"service": "database",
				},
				"prefix":      "/app/",
				"with_values": true,
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("SearchByTags", mock.Anything, mock.MatchedBy(func(req *interfaces.SearchByTagsRequest) bool {
					return req.Tags["env"] == "production" && req.Prefix == "/app/"
				})).Return(&interfaces.SearchByTagsResponse{
					Parameters: []*interfaces.ParameterValue{
						{Path: "/app/db/host", Tags: map[string]string{"env": "production", "service": "database"}},
					},
					HasMore:   false,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp interfaces.SearchByTagsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.Parameters, 1)
			},
		},
		{
			name: "search with pagination",
			body: map[string]interface{}{
				"tags":   map[string]string{"env": "prod"},
				"limit":  50,
				"cursor": "next-cursor",
			},
			setupMock: func(m *MockParameterStoreService) {
				m.On("SearchByTags", mock.Anything, mock.MatchedBy(func(req *interfaces.SearchByTagsRequest) bool {
					return req.Limit == 50 && req.Cursor == "next-cursor"
				})).Return(&interfaces.SearchByTagsResponse{
					Parameters: []*interfaces.ParameterValue{},
					HasMore:    false,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "empty tags",
			body: map[string]interface{}{
				"tags": map[string]string{},
			},
			setupMock:      func(m *MockParameterStoreService) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
			defer cleanup()

			tt.setupMock(mockParamService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/search", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleSearchParametersByTags(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestSetParameterRequestUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name                 string
		jsonData             string
		expectedOverwrite    bool
		expectedOverwriteSet bool
	}{
		{
			name:                 "overwrite explicitly set to true",
			jsonData:             `{"value":"dGVzdA==","overwrite":true}`,
			expectedOverwrite:    true,
			expectedOverwriteSet: true,
		},
		{
			name:                 "overwrite explicitly set to false",
			jsonData:             `{"value":"dGVzdA==","overwrite":false}`,
			expectedOverwrite:    false,
			expectedOverwriteSet: true,
		},
		{
			name:                 "overwrite not set",
			jsonData:             `{"value":"dGVzdA=="}`,
			expectedOverwrite:    false,
			expectedOverwriteSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req setParameterRequest
			err := json.Unmarshal([]byte(tt.jsonData), &req)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedOverwrite, req.Overwrite)
			assert.Equal(t, tt.expectedOverwriteSet, req.OverwriteSet)
		})
	}
}
