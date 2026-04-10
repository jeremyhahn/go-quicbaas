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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestHandleCreateDEKHandler tests the POST /api/v1/kms/deks endpoint
func TestHandleCreateDEKHandler(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful DEK creation",
			body: map[string]interface{}{
				"path":        "tenants/acme/data",
				"algorithm":   "AES-256-GCM",
				"description": "ACME tenant data key",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("CreateDEK", mock.Anything, mock.MatchedBy(func(req *service.CreateDEKRequest) bool {
					return req.Path == "tenants/acme/data"
				})).Return(&service.CreateDEKResponse{
					DEK: &service.DEKMetadata{
						Path:      "tenants/acme/data",
						Algorithm: service.KeyAlgAES256GCM,
						Version:   1,
						Status:    "active",
						CreatedAt: time.Now(),
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.CreateDEKResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.DEK)
				assert.Equal(t, "tenants/acme/data", resp.DEK.Path)
			},
		},
		{
			name:           "missing path",
			body:           map[string]interface{}{"algorithm": "AES-256-GCM"},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "path is required")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "not-valid-json",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "invalid JSON")
			},
		},
		{
			name: "service error",
			body: map[string]interface{}{
				"path":      "tenants/test",
				"algorithm": "AES-256-GCM",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("CreateDEK", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrInternal, "CreateDEK", "database error"),
				).Once()
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks", bytes.NewReader(bodyBytes))
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

// TestHandleGetDEKHandler tests the GET /api/v1/kms/deks/{path} endpoint
func TestHandleGetDEKHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful DEK retrieval",
			path: "/api/v1/kms/deks/tenants/acme/data",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetDEK", mock.Anything, mock.MatchedBy(func(req *service.GetDEKRequest) bool {
					return req.Path == "tenants/acme/data"
				})).Return(&service.GetDEKResponse{
					DEK: &service.DEKMetadata{
						Path:      "tenants/acme/data",
						Algorithm: service.KeyAlgAES256GCM,
						Version:   1,
						Status:    "active",
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.GetDEKResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.DEK)
				assert.Equal(t, "tenants/acme/data", resp.DEK.Path)
			},
		},
		{
			name: "DEK retrieval with version",
			path: "/api/v1/kms/deks/tenants/acme/data?version=2",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetDEK", mock.Anything, mock.MatchedBy(func(req *service.GetDEKRequest) bool {
					return req.Path == "tenants/acme/data" && req.Version == 2
				})).Return(&service.GetDEKResponse{
					DEK: &service.DEKMetadata{
						Path:    "tenants/acme/data",
						Version: 2,
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid version format",
			path:           "/api/v1/kms/deks/tenants/acme/data?version=invalid",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "invalid version format")
			},
		},
		{
			name: "DEK not found",
			path: "/api/v1/kms/deks/tenants/nonexistent",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetDEK", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrNotFound, "GetDEK", "DEK not found"),
				).Once()
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

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestHandleListDEKsHandler tests the GET /api/v1/kms/deks endpoint
func TestHandleListDEKsHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful DEK list",
			path: "/api/v1/kms/deks",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListDEKs", mock.Anything, mock.Anything).Return(&service.ListDEKsResponse{
					DEKs: []*service.DEKMetadata{
						{Path: "tenants/acme/data", Version: 1, Status: "active"},
						{Path: "tenants/beta/data", Version: 1, Status: "active"},
					},
					HasMore:    false,
					TotalCount: 2,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.ListDEKsResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Len(t, resp.DEKs, 2)
			},
		},
		{
			name: "list with prefix filter",
			path: "/api/v1/kms/deks?prefix=tenants/acme",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListDEKs", mock.Anything, mock.MatchedBy(func(req *service.ListDEKsRequest) bool {
					return req.Prefix == "tenants/acme"
				})).Return(&service.ListDEKsResponse{
					DEKs:      []*service.DEKMetadata{{Path: "tenants/acme/data"}},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "list with recursive flag",
			path: "/api/v1/kms/deks?recursive=true",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListDEKs", mock.Anything, mock.MatchedBy(func(req *service.ListDEKsRequest) bool {
					return req.Recursive == true
				})).Return(&service.ListDEKsResponse{
					DEKs:      []*service.DEKMetadata{},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "list with pagination",
			path: "/api/v1/kms/deks?limit=10&cursor=abc123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListDEKs", mock.Anything, mock.MatchedBy(func(req *service.ListDEKsRequest) bool {
					return req.Limit == 10 && req.Cursor == "abc123"
				})).Return(&service.ListDEKsResponse{
					DEKs:       []*service.DEKMetadata{},
					NextCursor: "def456",
					HasMore:    true,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "service error",
			path: "/api/v1/kms/deks",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListDEKs", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrInternal, "ListDEKs", "database error"),
				).Once()
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

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestHandleRotateDEKHandler tests the POST /api/v1/kms/deks/{path}/rotate endpoint
func TestHandleRotateDEKHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful DEK rotation",
			path: "/api/v1/kms/deks/tenants/acme/data/rotate",
			body: map[string]interface{}{"retain_old_version": true},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RotateDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateDEKRequest) bool {
					return req.Path == "tenants/acme/data" && req.RetainOldVersion == true
				})).Return(&service.RotateDEKResponse{
					Path:       "tenants/acme/data",
					OldVersion: 1,
					NewVersion: 2,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.RotateDEKResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, uint64(2), resp.NewVersion)
			},
		},
		{
			name: "rotation with empty body",
			path: "/api/v1/kms/deks/tenants/acme/data/rotate",
			body: nil,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RotateDEK", mock.Anything, mock.Anything).Return(&service.RotateDEKResponse{
					Path:       "tenants/acme/data",
					OldVersion: 1,
					NewVersion: 2,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON body",
			path:           "/api/v1/kms/deks/tenants/acme/data/rotate",
			body:           "{invalid-json}",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "rotation service error",
			path: "/api/v1/kms/deks/tenants/acme/data/rotate",
			body: map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RotateDEK", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrNotFound, "RotateDEK", "DEK not found"),
				).Once()
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

			var bodyReader *bytes.Reader
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					bodyReader = bytes.NewReader([]byte(str))
				} else {
					bodyBytes, _ := json.Marshal(tt.body)
					bodyReader = bytes.NewReader(bodyBytes)
				}
			} else {
				bodyReader = bytes.NewReader(nil)
			}

			req := httptest.NewRequest(http.MethodPost, tt.path, bodyReader)
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

// TestHandleDeleteDEKHandler tests the DELETE /api/v1/kms/deks/{path} endpoint
func TestHandleDeleteDEKHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful DEK deletion",
			path: "/api/v1/kms/deks/tenants/acme/data",
			body: map[string]interface{}{"force": true},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteDEK", mock.Anything, mock.MatchedBy(func(req *service.DeleteDEKRequest) bool {
					return req.Path == "tenants/acme/data" && req.Force == true
				})).Return(&service.DeleteDEKResponse{
					Path:      "tenants/acme/data",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.DeleteDEKResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name: "deletion with migration",
			path: "/api/v1/kms/deks/tenants/old",
			body: map[string]interface{}{"migrate_to": "tenants/new"},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteDEK", mock.Anything, mock.MatchedBy(func(req *service.DeleteDEKRequest) bool {
					return req.MigrateTo == "tenants/new"
				})).Return(&service.DeleteDEKResponse{
					Path:          "tenants/old",
					Deleted:       true,
					MigratedCount: 100,
					Timestamp:     time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "deletion without body",
			path: "/api/v1/kms/deks/tenants/acme/data",
			body: nil,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteDEK", mock.Anything, mock.Anything).Return(&service.DeleteDEKResponse{
					Path:      "tenants/acme/data",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON body",
			path:           "/api/v1/kms/deks/tenants/acme/data",
			body:           "{bad-json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "deletion service error forbidden",
			path: "/api/v1/kms/deks/tenants/protected",
			body: map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteDEK", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrForbidden, "DeleteDEK", "cannot delete protected DEK"),
				).Once()
			},
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			var bodyReader *bytes.Reader
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					bodyReader = bytes.NewReader([]byte(str))
				} else {
					bodyBytes, _ := json.Marshal(tt.body)
					bodyReader = bytes.NewReader(bodyBytes)
				}
			} else {
				bodyReader = bytes.NewReader(nil)
			}

			req := httptest.NewRequest(http.MethodDelete, tt.path, bodyReader)
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

// TestHandleGetDEKStatusHandler tests the GET /api/v1/kms/deks/{path}/status endpoint
func TestHandleGetDEKStatusHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful status retrieval",
			path: "/api/v1/kms/deks/tenants/acme/data/status",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetDEKStatus", mock.Anything, mock.MatchedBy(func(req *service.GetDEKStatusRequest) bool {
					return req.Path == "tenants/acme/data"
				})).Return(&service.GetDEKStatusResponse{
					DEK: &service.DEKMetadata{
						Path:       "tenants/acme/data",
						Status:     "active",
						UsageCount: 1000,
						BytesUsed:  1024 * 1024,
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.GetDEKStatusResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.DEK)
				assert.Equal(t, "active", resp.DEK.Status)
			},
		},
		{
			name: "status with reencryption progress",
			path: "/api/v1/kms/deks/tenants/acme/data/status",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetDEKStatus", mock.Anything, mock.Anything).Return(&service.GetDEKStatusResponse{
					DEK: &service.DEKMetadata{Path: "tenants/acme/data", Status: "rotating"},
					ReencryptionProgress: &service.ReencryptionProgress{
						Status:          "running",
						InProgress:      true,
						TotalKeys:       100,
						ProcessedKeys:   50,
						PercentComplete: 50.0,
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.GetDEKStatusResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.ReencryptionProgress)
				assert.True(t, resp.ReencryptionProgress.InProgress)
			},
		},
		{
			name: "status not found",
			path: "/api/v1/kms/deks/tenants/nonexistent/status",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetDEKStatus", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrNotFound, "GetDEKStatus", "DEK not found"),
				).Once()
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

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestHandleSetDefaultDEKHandler tests the POST /api/v1/kms/deks/{path}/set-default endpoint
func TestHandleSetDefaultDEKHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful set default",
			path: "/api/v1/kms/deks/tenants/acme/data/set-default",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SetDefaultDEK", mock.Anything, mock.MatchedBy(func(req *service.SetDefaultDEKRequest) bool {
					return req.Path == "tenants/acme/data"
				})).Return(&service.SetDefaultDEKResponse{
					Path:            "tenants/acme/data",
					PreviousDefault: "tenants/old/data",
					Timestamp:       time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.SetDefaultDEKResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "tenants/acme/data", resp.Path)
			},
		},
		{
			name: "set default service error",
			path: "/api/v1/kms/deks/tenants/nonexistent/set-default",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SetDefaultDEK", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrNotFound, "SetDefaultDEK", "DEK not found"),
				).Once()
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

			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestHandleSetDefaultDEKFromBodyHandler tests the PUT /api/v1/kms/deks/default endpoint
func TestHandleSetDefaultDEKFromBodyHandler(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful set default from body",
			body: map[string]interface{}{"path": "tenants/acme/data"},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SetDefaultDEK", mock.Anything, mock.MatchedBy(func(req *service.SetDefaultDEKRequest) bool {
					return req.Path == "tenants/acme/data"
				})).Return(&service.SetDefaultDEKResponse{
					Path:      "tenants/acme/data",
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing path in body",
			body:           map[string]interface{}{},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "path is required")
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

			req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/default", bytes.NewReader(bodyBytes))
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

// TestHandleSetDEKSafetyHandler tests the PUT /api/v1/kms/deks/{path}/safety endpoint
func TestHandleSetDEKSafetyHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful safety configuration",
			path: "/api/v1/kms/deks/tenants/acme/data/safety",
			body: map[string]interface{}{
				"max_operations":  1000000,
				"max_bytes":       1073741824,
				"warning_percent": 80,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SetDEKSafety", mock.Anything, mock.MatchedBy(func(req *service.SetDEKSafetyRequest) bool {
					return req.Path == "tenants/acme/data" && req.MaxOperations == 1000000
				})).Return(&service.SetDEKSafetyResponse{
					Path: "tenants/acme/data",
					Config: &service.DEKSafetyConfig{
						MaxOperations:  1000000,
						MaxBytes:       1073741824,
						WarningPercent: 80,
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.SetDEKSafetyResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.Config)
				assert.Equal(t, uint64(1000000), resp.Config.MaxOperations)
			},
		},
		{
			name:           "invalid JSON body",
			path:           "/api/v1/kms/deks/tenants/acme/data/safety",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "safety set service error",
			path: "/api/v1/kms/deks/tenants/acme/data/safety",
			body: map[string]interface{}{"max_operations": 100},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SetDEKSafety", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrNotFound, "SetDEKSafety", "DEK not found"),
				).Once()
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

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPut, tt.path, bytes.NewReader(bodyBytes))
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

// TestHandleClearDEKSafetyHandler tests the DELETE /api/v1/kms/deks/{path}/safety endpoint
func TestHandleClearDEKSafetyHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful safety clear",
			path: "/api/v1/kms/deks/tenants/acme/data/safety",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClearDEKSafety", mock.Anything, mock.MatchedBy(func(req *service.ClearDEKSafetyRequest) bool {
					return req.Path == "tenants/acme/data"
				})).Return(&service.ClearDEKSafetyResponse{
					Path:      "tenants/acme/data",
					Cleared:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.ClearDEKSafetyResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Cleared)
			},
		},
		{
			name: "clear safety not found",
			path: "/api/v1/kms/deks/tenants/nonexistent/safety",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClearDEKSafety", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrNotFound, "ClearDEKSafety", "DEK not found"),
				).Once()
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

			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestHandleStartReencryptionHandler tests the POST /api/v1/kms/deks/{path}/reencrypt endpoint
func TestHandleStartReencryptionHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful reencryption start",
			path: "/api/v1/kms/deks/tenants/acme/data/reencrypt",
			body: map[string]interface{}{"from_version": 1, "to_version": 2},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
					return req.Path == "tenants/acme/data"
				})).Return(&service.StartReencryptionResponse{
					OperationID:    "op-123",
					Path:           "tenants/acme/data",
					Started:        true,
					EstimatedItems: 1000,
					Timestamp:      time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusAccepted,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.StartReencryptionResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Started)
				assert.NotEmpty(t, resp.OperationID)
			},
		},
		{
			name: "reencryption with empty body",
			path: "/api/v1/kms/deks/tenants/acme/data/reencrypt",
			body: nil,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StartReencryption", mock.Anything, mock.Anything).Return(&service.StartReencryptionResponse{
					OperationID: "op-456",
					Started:     true,
					Timestamp:   time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "invalid JSON body",
			path:           "/api/v1/kms/deks/tenants/acme/data/reencrypt",
			body:           "{bad-json}",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "reencryption service error",
			path: "/api/v1/kms/deks/tenants/acme/data/reencrypt",
			body: map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StartReencryption", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrAlreadyExists, "StartReencryption", "reencryption already in progress"),
				).Once()
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			var bodyReader *bytes.Reader
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					bodyReader = bytes.NewReader([]byte(str))
				} else {
					bodyBytes, _ := json.Marshal(tt.body)
					bodyReader = bytes.NewReader(bodyBytes)
				}
			} else {
				bodyReader = bytes.NewReader(nil)
			}

			req := httptest.NewRequest(http.MethodPost, tt.path, bodyReader)
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

// TestHandleGetReencryptionProgressHandler tests the GET /api/v1/kms/deks/{path}/reencrypt endpoint
func TestHandleGetReencryptionProgressHandler(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful progress retrieval",
			path: "/api/v1/kms/deks/tenants/acme/data/reencrypt",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetReencryptionProgress", mock.Anything, mock.MatchedBy(func(req *service.GetReencryptionProgressRequest) bool {
					return req.Path == "tenants/acme/data"
				})).Return(&service.GetReencryptionProgressResponse{
					Path:        "tenants/acme/data",
					OperationID: "op-123",
					Progress: &service.ReencryptionProgress{
						Status:          "running",
						InProgress:      true,
						TotalKeys:       1000,
						ProcessedKeys:   500,
						PercentComplete: 50.0,
					},
					Status:    "running",
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp service.GetReencryptionProgressResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.Progress)
				assert.Equal(t, 50.0, resp.Progress.PercentComplete)
			},
		},
		{
			name: "progress with operation_id query param",
			path: "/api/v1/kms/deks/tenants/acme/data/reencrypt?operation_id=op-789",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetReencryptionProgress", mock.Anything, mock.MatchedBy(func(req *service.GetReencryptionProgressRequest) bool {
					return req.OperationID == "op-789"
				})).Return(&service.GetReencryptionProgressResponse{
					OperationID: "op-789",
					Status:      "completed",
					Progress: &service.ReencryptionProgress{
						Status:          "completed",
						PercentComplete: 100.0,
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "progress with verbose flag",
			path: "/api/v1/kms/deks/tenants/acme/data/reencrypt?verbose=true",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetReencryptionProgress", mock.Anything, mock.MatchedBy(func(req *service.GetReencryptionProgressRequest) bool {
					return req.Verbose == true
				})).Return(&service.GetReencryptionProgressResponse{
					Status:    "not_started",
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "progress not found",
			path: "/api/v1/kms/deks/tenants/nonexistent/reencrypt",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
					nil, errors.Newf(errors.ErrNotFound, "GetReencryptionProgress", "no reencryption in progress"),
				).Once()
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

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}
