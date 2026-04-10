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
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleStoreSecret(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "store secret successfully",
			path: "app/database/password",
			body: map[string]interface{}{
				"data":        []byte("super-secret-password"),
				"description": "Database password for production",
				"metadata": map[string]string{
					"env":     "production",
					"service": "database",
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StoreSecret", mock.Anything, mock.MatchedBy(func(req *service.StoreSecretRequest) bool {
					return req.Name == "app/database/password" &&
						len(req.Data) > 0 &&
						req.Description == "Database password for production"
				})).Return(&service.StoreSecretResponse{
					SecretID:  "app/database/password",
					Version:   1,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "store secret without metadata",
			path: "simple/secret",
			body: map[string]interface{}{
				"data": []byte("secret-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StoreSecret", mock.Anything, mock.Anything).Return(&service.StoreSecretResponse{
					SecretID:  "simple/secret",
					Version:   1,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "missing secret path",
			path:           "",
			body:           map[string]interface{}{"data": []byte("test")},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			path:           "test/secret",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing secret data",
			path:           "test/secret",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty secret data",
			path:           "test/secret",
			body:           map[string]interface{}{"data": []byte{}},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			path: "test/secret",
			body: map[string]interface{}{
				"data": []byte("test-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StoreSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "StoreSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "invalid algorithm rejected",
			path: "test/secret",
			body: map[string]interface{}{
				"data":      []byte("test-data"),
				"algorithm": "INVALID-ALGO",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "valid AES-256-GCM algorithm accepted",
			path: "test/secret-with-algo",
			body: map[string]interface{}{
				"data":      []byte("test-data"),
				"algorithm": "AES-256-GCM",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StoreSecret", mock.Anything, mock.MatchedBy(func(req *service.StoreSecretRequest) bool {
					return req.Algorithm == "AES-256-GCM"
				})).Return(&service.StoreSecretResponse{
					SecretID:  "test/secret-with-algo",
					Version:   1,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "valid AES-128-GCM algorithm accepted",
			path: "test/secret-aes128",
			body: map[string]interface{}{
				"data":      []byte("test-data"),
				"algorithm": "AES-128-GCM",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StoreSecret", mock.Anything, mock.MatchedBy(func(req *service.StoreSecretRequest) bool {
					return req.Algorithm == "AES-128-GCM"
				})).Return(&service.StoreSecretResponse{
					SecretID:  "test/secret-aes128",
					Version:   1,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "valid ChaCha20-Poly1305 algorithm accepted (case insensitive)",
			path: "test/secret-chacha",
			body: map[string]interface{}{
				"data":      []byte("test-data"),
				"algorithm": "ChaCha20-Poly1305",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StoreSecret", mock.Anything, mock.MatchedBy(func(req *service.StoreSecretRequest) bool {
					return req.Algorithm == "ChaCha20-Poly1305"
				})).Return(&service.StoreSecretResponse{
					SecretID:  "test/secret-chacha",
					Version:   1,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+tt.path, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleStoreSecret(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleGetSecret(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "get latest secret version",
			path: "app/api/key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecret", mock.Anything, mock.MatchedBy(func(req *service.GetSecretRequest) bool {
					return req.SecretID == "app/api/key" && req.Version == 0
				})).Return(&service.GetSecretResponse{
					SecretID:  "app/api/key",
					Data:      []byte("api-key-value"),
					Version:   3,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "get specific secret version",
			path:        "app/api/key",
			queryParams: "version=2",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecret", mock.Anything, mock.MatchedBy(func(req *service.GetSecretRequest) bool {
					return req.SecretID == "app/api/key" && req.Version == 2
				})).Return(&service.GetSecretResponse{
					SecretID:  "app/api/key",
					Data:      []byte("old-api-key-value"),
					Version:   2,
					Timestamp: time.Now().Add(-24 * time.Hour),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path",
			path:           "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid version parameter",
			path:           "test/secret",
			queryParams:    "version=invalid",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "secret not found",
			path: "nonexistent/secret",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "GetSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			path: "test/secret",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "GetSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			url := "/api/v1/secrets/" + tt.path
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetSecret(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleListSecrets(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "list secrets with default limit",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecrets", mock.Anything, mock.MatchedBy(func(req *service.ListSecretsRequest) bool {
					return req.Limit == 100 && req.Cursor == ""
				})).Return(&service.ListSecretsResponse{
					Secrets: []*service.Secret{
						{SecretID: "app/secret1", Name: "secret1", CurrentVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
						{SecretID: "app/secret2", Name: "secret2", CurrentVersion: 2, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					},
					NextCursor: "",
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "list secrets with custom limit",
			queryParams: "limit=50",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecrets", mock.Anything, mock.MatchedBy(func(req *service.ListSecretsRequest) bool {
					return req.Limit == 50
				})).Return(&service.ListSecretsResponse{
					Secrets:   []*service.Secret{},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "list secrets with pagination cursor",
			queryParams: "limit=25&cursor=abc123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecrets", mock.Anything, mock.MatchedBy(func(req *service.ListSecretsRequest) bool {
					return req.Limit == 25 && req.Cursor == "abc123"
				})).Return(&service.ListSecretsResponse{
					Secrets:    []*service.Secret{},
					NextCursor: "def456",
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "list secrets with prefix filter",
			queryParams: "prefix=app/prod",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecrets", mock.Anything, mock.MatchedBy(func(req *service.ListSecretsRequest) bool {
					return req.Prefix == "app/prod" && req.Limit == 100
				})).Return(&service.ListSecretsResponse{
					Secrets: []*service.Secret{
						{SecretID: "app/prod/secret1", Name: "secret1", CurrentVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "list secrets with invalid limit - uses default",
			queryParams: "limit=2000",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecrets", mock.Anything, mock.MatchedBy(func(req *service.ListSecretsRequest) bool {
					return req.Limit == 100 // Should use default because 2000 > 1000
				})).Return(&service.ListSecretsResponse{
					Secrets:   []*service.Secret{},
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "service error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecrets", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "ListSecrets",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			url := "/api/v1/secrets"
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.HandleListSecrets(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleDeleteSecret(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "soft delete secret",
			path: "app/temp/secret",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.MatchedBy(func(req *service.DeleteSecretRequest) bool {
					return req.SecretID == "app/temp/secret" && !req.PermanentlyDelete
				})).Return(&service.DeleteSecretResponse{
					SecretID:  "app/temp/secret",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "permanent delete secret",
			path:        "app/old/secret",
			queryParams: "permanent=true",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.MatchedBy(func(req *service.DeleteSecretRequest) bool {
					return req.SecretID == "app/old/secret" && req.PermanentlyDelete
				})).Return(&service.DeleteSecretResponse{
					SecretID:  "app/old/secret",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path",
			path:           "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "secret not found",
			path: "nonexistent/secret",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "DeleteSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			path: "test/secret",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "DeleteSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			url := "/api/v1/secrets/" + tt.path
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodDelete, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleDeleteSecret(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleEnableSecret(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "enable secret successfully",
			path: "app/database/password/enable",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.MatchedBy(func(req *service.UpdateSecretStatusRequest) bool {
					return req.SecretID == "app/database/password" && req.Status == service.SecretStatusEnabled
				})).Return(&service.UpdateSecretStatusResponse{
					SecretID:  "app/database/password",
					OldStatus: service.SecretStatusDisabled,
					NewStatus: service.SecretStatusEnabled,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path",
			path:           "/enable",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "secret not found",
			path: "nonexistent/secret/enable",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "UpdateSecretStatus",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			path: "test/secret/enable",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "UpdateSecretStatus",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+tt.path, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleEnableSecret(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleDisableSecret(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "disable secret successfully",
			path: "app/database/password/disable",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.MatchedBy(func(req *service.UpdateSecretStatusRequest) bool {
					return req.SecretID == "app/database/password" && req.Status == service.SecretStatusDisabled
				})).Return(&service.UpdateSecretStatusResponse{
					SecretID:  "app/database/password",
					OldStatus: service.SecretStatusEnabled,
					NewStatus: service.SecretStatusDisabled,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path",
			path:           "/disable",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "secret not found",
			path: "nonexistent/secret/disable",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "UpdateSecretStatus",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			path: "test/secret/disable",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "UpdateSecretStatus",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+tt.path, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleDisableSecret(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleGetSecretsByPrefix(t *testing.T) {
	tests := []struct {
		name           string
		prefix         string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:   "get secrets by prefix successfully",
			prefix: "apps/myapp/",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecretsByPrefix", mock.Anything, mock.MatchedBy(func(req *service.GetSecretsByPrefixRequest) bool {
					return req.Prefix == "apps/myapp/" && req.IncludeData && req.Limit == 100
				})).Return(&service.GetSecretsByPrefixResponse{
					Secrets: map[string]map[string][]byte{
						"apps/myapp/db":    {"password": []byte("dbpass")},
						"apps/myapp/cache": {"password": []byte("cachepass")},
					},
					TotalCount: 2,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "get secrets by prefix with limit",
			prefix:      "apps/",
			queryParams: "limit=50&include_data=false",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecretsByPrefix", mock.Anything, mock.MatchedBy(func(req *service.GetSecretsByPrefixRequest) bool {
					return req.Prefix == "apps/" && !req.IncludeData && req.Limit == 50
				})).Return(&service.GetSecretsByPrefixResponse{
					Secrets:    map[string]map[string][]byte{},
					TotalCount: 0,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing prefix",
			prefix:         "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "service error",
			prefix: "test/",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecretsByPrefix", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "GetSecretsByPrefix",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			url := "/api/v1/secrets-prefix/" + tt.prefix
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.prefix)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetSecretsByPrefix(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleWatchSecrets(t *testing.T) {
	tests := []struct {
		name           string
		prefix         string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkBody      bool
	}{
		{
			name:   "watch secrets successfully",
			prefix: "apps/myapp/",
			setupMock: func(m *testutil.MockQuicBaaS) {
				ch := make(chan *service.SecretChange, 1)
				ch <- &service.SecretChange{
					Path:       "apps/myapp/db",
					ChangeType: service.SecretChangeUpdated,
					Version:    1,
					Timestamp:  time.Now(),
				}
				close(ch)
				m.On("WatchSecretsByPrefix", mock.Anything, mock.MatchedBy(func(req *service.WatchSecretsRequest) bool {
					return req.Prefix == "apps/myapp/"
				})).Return((<-chan *service.SecretChange)(ch), nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkBody:      true,
		},
		{
			name:        "watch secrets with start version",
			prefix:      "apps/",
			queryParams: "start_version=100",
			setupMock: func(m *testutil.MockQuicBaaS) {
				ch := make(chan *service.SecretChange)
				close(ch)
				m.On("WatchSecretsByPrefix", mock.Anything, mock.MatchedBy(func(req *service.WatchSecretsRequest) bool {
					return req.Prefix == "apps/" && req.StartVersion == 100
				})).Return((<-chan *service.SecretChange)(ch), nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing prefix",
			prefix:         "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "service error",
			prefix: "test/",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("WatchSecretsByPrefix", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "WatchSecretsByPrefix",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			url := "/api/v1/secrets-watch/" + tt.prefix
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.prefix)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleWatchSecrets(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkBody {
				assert.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
			}
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleRotateSecretMissingData(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Test missing data field
	body := map[string]interface{}{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret?action=rotate", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "new secret data is required")
}

func TestHandleRotateSecretJSONObjectData(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("RotateSecret", mock.Anything, mock.MatchedBy(func(req *service.RotateSecretRequest) bool {
		// Data should be stored as-is for JSON objects
		return req.SecretID == "test/secret" && len(req.NewData) > 0
	})).Return(&service.RotateSecretResponse{
		SecretID:   "test/secret",
		OldVersion: 1,
		NewVersion: 2,
		Timestamp:  time.Now(),
	}, nil).Once()

	// Test JSON object data - note the data field contains a JSON object
	body := `{"data": {"username": "admin", "password": "secret123"}}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret?action=rotate", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateSecret(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleRotateSecretRawDataFallback(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("RotateSecret", mock.Anything, mock.MatchedBy(func(req *service.RotateSecretRequest) bool {
		return req.SecretID == "test/secret"
	})).Return(&service.RotateSecretResponse{
		SecretID:   "test/secret",
		OldVersion: 1,
		NewVersion: 2,
		Timestamp:  time.Now(),
	}, nil).Once()

	// Test array data (not a string, not an object)
	body := `{"data": [1, 2, 3, 4, 5]}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret?action=rotate", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateSecret(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleStoreSecretJSONObjectData(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("StoreSecret", mock.Anything, mock.MatchedBy(func(req *service.StoreSecretRequest) bool {
		return req.Name == "test/secret"
	})).Return(&service.StoreSecretResponse{
		SecretID:  "test/secret",
		Version:   1,
		Timestamp: time.Now(),
	}, nil).Once()

	// Test JSON object data
	body := `{"data": {"db_host": "localhost", "db_port": 5432}}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleStoreSecretArrayData(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("StoreSecret", mock.Anything, mock.MatchedBy(func(req *service.StoreSecretRequest) bool {
		return req.Name == "test/secret"
	})).Return(&service.StoreSecretResponse{
		SecretID:  "test/secret",
		Version:   1,
		Timestamp: time.Now(),
	}, nil).Once()

	// Test array data (uses fallback path)
	body := `{"data": [100, 200, 300]}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleStoreSecretEmptyDataAfterProcessing(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Test empty base64 string - decoded results in empty data
	body := `{"data": ""}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	// The actual error message - check it contains something about empty data
	assert.Contains(t, w.Body.String(), "empty")
}

func TestHandleStoreSecretInvalidBase64(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Invalid base64 string (has invalid characters)
	body := `{"data": "not-valid-base64!!!"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid base64 data")
}

func TestHandleStoreSecretInvalidAlgorithm(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Invalid algorithm
	body := `{"data": "dGVzdA==", "algorithm": "INVALID-ALGO"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid algorithm")
}

func TestHandleGetDEKURLDecoding(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetDEK", mock.Anything, mock.MatchedBy(func(req *service.GetDEKRequest) bool {
		// Should decode "tenants%2Facme%2Fdata" to "tenants/acme/data"
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

	// Use URL-encoded path
	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/tenants%2Facme%2Fdata", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleRotateDEKEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Empty path should fail
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks//rotate", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleDeleteDEKEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/deks/", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDeleteDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleGetDEKStatusEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/status", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetDEKStatus(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleSetDefaultDEKEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/set-default", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetDefaultDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleSetDEKSafetyEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"max_operations": 1000000}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/safety", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetDEKSafety(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleClearDEKSafetyEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/deks/safety", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleClearDEKSafety(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleStartReencryptionEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/reencrypt", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStartReencryption(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleGetReencryptionProgressEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/reencrypt", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetReencryptionProgress(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleGetReencryptionProgressWithQueryParams(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetReencryptionProgress", mock.Anything, mock.MatchedBy(func(req *service.GetReencryptionProgressRequest) bool {
		return req.Path == "tenants/acme/data" && req.OperationID == "op-123" && req.Verbose == true
	})).Return(&service.GetReencryptionProgressResponse{
		OperationID: "op-123",
		Path:        "tenants/acme/data",
		Status:      "running",
		Progress: &service.ReencryptionProgress{
			Status:          "running",
			InProgress:      true,
			TotalKeys:       1000,
			ProcessedKeys:   500,
			PercentComplete: 50.0,
		},
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/tenants/acme/data/reencrypt?operation_id=op-123&verbose=true", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleListDEKsLimitClamping(t *testing.T) {
	tests := []struct {
		name          string
		limitParam    string
		expectedLimit int
	}{
		{"limit too high", "2000", 100}, // Should clamp to default 100
		{"limit negative", "-1", 100},   // Should use default 100
		{"limit zero", "0", 100},        // Should use default 100
		{"limit invalid", "abc", 100},   // Should use default 100
		{"limit valid", "50", 50},       // Should use 50
		{"limit max", "1000", 1000},     // Should use 1000 (max)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			mockService.On("ListDEKs", mock.Anything, mock.MatchedBy(func(req *service.ListDEKsRequest) bool {
				return req.Limit == tt.expectedLimit
			})).Return(&service.ListDEKsResponse{
				DEKs:      []*service.DEKMetadata{},
				Timestamp: time.Now(),
			}, nil).Once()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks?limit="+tt.limitParam, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestHandleCreateDEKReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Create a request with an error-producing body
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks", &testErrorReader{})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCreateDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "failed to read request body")
}

type testErrorReader struct{}

func (e *testErrorReader) Read(p []byte) (n int, err error) {
	return 0, assert.AnError
}

func TestHandleWatchSecretsMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/watch", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleWatchSecrets(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "prefix is required")
}

func TestHandleRollbackSecretSuccessCoverage(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("RollbackSecret", mock.Anything, mock.MatchedBy(func(req *service.RollbackSecretRequest) bool {
		return req.SecretID == "test/secret" && req.TargetVersion == 2
	})).Return(&service.RollbackSecretResponse{
		SecretID:   "test/secret",
		NewVersion: 2,
		Timestamp:  time.Now(),
	}, nil).Once()

	body := `{"version": 2}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret?action=rollback", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRollbackSecret(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.RollbackSecretResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestHandleRollbackSecretMissingVersionCoverage(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret?action=rollback", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRollbackSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "version is required")
}

func TestHandleRollbackSecretInvalidJSONCoverage(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret?action=rollback", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRollbackSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON body")
}

func TestHandleListVersionsSuccessCoverage(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ListSecretVersions", mock.Anything, mock.MatchedBy(func(req *service.ListSecretVersionsRequest) bool {
		return req.SecretID == "test/secret"
	})).Return(&service.ListSecretVersionsResponse{
		Versions: []*service.SecretVersion{
			{SecretID: "test/secret", Version: 1, CreatedAt: time.Now()},
			{SecretID: "test/secret", Version: 2, CreatedAt: time.Now()},
		},
		HasMore: false,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/test/secret?versions=true", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "test/secret")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleListVersions(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp service.ListSecretVersionsResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 2)
}

func TestHandleListVersionsMissingPathCoverage(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/?versions=true", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleListVersions(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "secret path is required")
}

func TestHandleGetDEKEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleDestroySecret(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "destroy secret successfully",
			path: "app/temp/secret/destroy",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.MatchedBy(func(req *service.DeleteSecretRequest) bool {
					return req.SecretID == "app/temp/secret" && req.PermanentlyDelete == true
				})).Return(&service.DeleteSecretResponse{
					SecretID:  "app/temp/secret",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path (empty string becomes /destroy trimmed to empty)",
			path:           "/destroy", // This will be trimmed to empty
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "secret not found",
			path: "nonexistent/secret/destroy",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "DeleteSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			path: "test/secret/destroy",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "DeleteSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+tt.path, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleDestroySecret(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleListSecretVersions(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "list versions successfully",
			path: "app/database/password/versions",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecretVersions", mock.Anything, mock.MatchedBy(func(req *service.ListSecretVersionsRequest) bool {
					return req.SecretID == "app/database/password"
				})).Return(&service.ListSecretVersionsResponse{
					Versions: []*service.SecretVersion{
						{SecretID: "app/database/password", Version: 1, CreatedAt: time.Now()},
						{SecretID: "app/database/password", Version: 2, CreatedAt: time.Now()},
					},
					HasMore: false,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "list versions with pagination",
			path:        "app/secret/versions",
			queryParams: "limit=10&cursor=abc123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecretVersions", mock.Anything, mock.MatchedBy(func(req *service.ListSecretVersionsRequest) bool {
					return req.SecretID == "app/secret" && req.Limit == 10 && req.Cursor == "abc123"
				})).Return(&service.ListSecretVersionsResponse{
					Versions: []*service.SecretVersion{},
					HasMore:  false,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path",
			path:           "/versions", // This will be trimmed to empty
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			path: "test/secret/versions",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecretVersions", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "ListSecretVersions",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			url := "/api/v1/secrets/" + tt.path
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleListSecretVersions(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleRollbackSecret(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "rollback secret successfully",
			path: "app/database/password",
			body: map[string]interface{}{
				"version": 2,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RollbackSecret", mock.Anything, mock.MatchedBy(func(req *service.RollbackSecretRequest) bool {
					return req.SecretID == "app/database/password" && req.TargetVersion == 2
				})).Return(&service.RollbackSecretResponse{
					SecretID:   "app/database/password",
					NewVersion: 2,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path",
			path:           "",
			body:           map[string]interface{}{"version": 1},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			path:           "test/secret",
			body:           "not-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			path: "test/secret",
			body: map[string]interface{}{"version": 1},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RollbackSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "RollbackSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var body []byte
			if s, ok := tt.body.(string); ok {
				body = []byte(s)
			} else {
				body, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+tt.path+"?action=rollback", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleRollbackSecret(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleRotateSecret(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "rotate secret successfully",
			path: "app/database/password",
			body: map[string]interface{}{
				"data": "bmV3LXNlY3JldC1kYXRh", // base64 encoded "new-secret-data"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RotateSecret", mock.Anything, mock.MatchedBy(func(req *service.RotateSecretRequest) bool {
					return req.SecretID == "app/database/password"
				})).Return(&service.RotateSecretResponse{
					SecretID:   "app/database/password",
					OldVersion: 1,
					NewVersion: 2,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path",
			path:           "",
			body:           map[string]interface{}{"data": "dGVzdA=="},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			path:           "test/secret",
			body:           "not-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			path: "test/secret",
			body: map[string]interface{}{"data": "dGVzdA=="},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RotateSecret", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "RotateSecret",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var body []byte
			if s, ok := tt.body.(string); ok {
				body = []byte(s)
			} else {
				body, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+tt.path+"?action=rotate", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleRotateSecret(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleGetSecretOrVersions(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		versions       bool
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:     "get secret without versions",
			path:     "app/database/password",
			versions: false,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecret", mock.Anything, mock.MatchedBy(func(req *service.GetSecretRequest) bool {
					return req.SecretID == "app/database/password"
				})).Return(&service.GetSecretResponse{
					SecretID:  "app/database/password",
					Version:   1,
					Data:      []byte("secret-data"),
					CreatedAt: time.Now(),
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:     "get secret with versions flag",
			path:     "app/database/password",
			versions: true,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecretVersions", mock.Anything, mock.MatchedBy(func(req *service.ListSecretVersionsRequest) bool {
					return req.SecretID == "app/database/password"
				})).Return(&service.ListSecretVersionsResponse{
					Versions: []*service.SecretVersion{
						{SecretID: "app/database/password", Version: 1, CreatedAt: time.Now()},
					},
					HasMore: false,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			url := "/api/v1/secrets/" + tt.path
			if tt.versions {
				url += "?versions=true"
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetSecretOrVersions(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleSecretsAction(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		action         string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:   "store action (default)",
			path:   "test/secret",
			action: "",
			body: map[string]interface{}{
				"data": "dGVzdC1kYXRh",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StoreSecret", mock.Anything, mock.Anything).Return(&service.StoreSecretResponse{
					SecretID:  "test/secret",
					Version:   1,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:   "rollback action",
			path:   "test/secret",
			action: "rollback",
			body: map[string]interface{}{
				"version": 1,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RollbackSecret", mock.Anything, mock.Anything).Return(&service.RollbackSecretResponse{
					SecretID:   "test/secret",
					NewVersion: 1,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "rotate action",
			path:   "test/secret",
			action: "rotate",
			body: map[string]interface{}{
				"data": "bmV3LWRhdGE=",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RotateSecret", mock.Anything, mock.Anything).Return(&service.RotateSecretResponse{
					SecretID:   "test/secret",
					OldVersion: 1,
					NewVersion: 2,
					Timestamp:  time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "destroy action",
			path:   "test/secret",
			action: "destroy",
			body:   nil,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.MatchedBy(func(req *service.DeleteSecretRequest) bool {
					return req.SecretID == "test/secret" && req.PermanentlyDelete == true
				})).Return(&service.DeleteSecretResponse{
					SecretID:  "test/secret",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "destroy action with path ending in /destroy",
			path:   "test/secret/destroy",
			action: "destroy",
			body:   nil,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.MatchedBy(func(req *service.DeleteSecretRequest) bool {
					return req.SecretID == "test/secret" && req.PermanentlyDelete == true
				})).Return(&service.DeleteSecretResponse{
					SecretID:  "test/secret",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			body, _ := json.Marshal(tt.body)

			url := "/api/v1/secrets/" + tt.path
			if tt.action != "" {
				url += "?action=" + tt.action
			}

			req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleSecretsAction(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleListVersions(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "list versions successfully",
			path: "app/secret",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecretVersions", mock.Anything, mock.MatchedBy(func(req *service.ListSecretVersionsRequest) bool {
					return req.SecretID == "app/secret"
				})).Return(&service.ListSecretVersionsResponse{
					Versions: []*service.SecretVersion{
						{SecretID: "app/secret", Version: 1, CreatedAt: time.Now()},
						{SecretID: "app/secret", Version: 2, CreatedAt: time.Now()},
					},
					HasMore: false,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "empty path returns bad request",
			path:           "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/"+tt.path+"?versions=true", nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleListVersions(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}
