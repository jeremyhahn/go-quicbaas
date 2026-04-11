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

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// MockDynamicSecretsService is a mock implementation of service.DynamicSecretsService.
type MockDynamicSecretsService struct {
	mock.Mock
}

func (m *MockDynamicSecretsService) CreateRole(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CreateRoleResponse), args.Error(1)
}

func (m *MockDynamicSecretsService) GetRole(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetRoleResponse), args.Error(1)
}

func (m *MockDynamicSecretsService) ListRoles(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListRolesResponse), args.Error(1)
}

func (m *MockDynamicSecretsService) DeleteRole(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteRoleResponse), args.Error(1)
}

func (m *MockDynamicSecretsService) GenerateCredential(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GenerateCredentialResponse), args.Error(1)
}

func (m *MockDynamicSecretsService) RenewLease(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RenewLeaseResponse), args.Error(1)
}

func (m *MockDynamicSecretsService) RevokeLease(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RevokeLeaseResponse), args.Error(1)
}

func (m *MockDynamicSecretsService) ListLeases(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListLeasesResponse), args.Error(1)
}

// MockQuicBaaSWithDynamicSecrets wraps testutil.MockQuicBaaS to add DynamicSecrets support.
type MockQuicBaaSWithDynamicSecrets struct {
	*testutil.MockQuicBaaS
	DynamicSecretsService *MockDynamicSecretsService
}

// DynamicSecrets returns the mock dynamic secrets service.
func (m *MockQuicBaaSWithDynamicSecrets) DynamicSecrets() service.DynamicSecretsService {
	return m.DynamicSecretsService
}

// setupDynamicSecretsMockServer creates a test server with dynamic secrets mock support.
func setupDynamicSecretsMockServer(t *testing.T, mockDynamicSecrets *MockDynamicSecretsService) (*Server, *MockQuicBaaSWithDynamicSecrets, func()) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	baseMock := testutil.NewMockQuicBaaS()
	mockService := &MockQuicBaaSWithDynamicSecrets{
		MockQuicBaaS:          baseMock,
		DynamicSecretsService: mockDynamicSecrets,
	}

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService, serverConfig, log)
	require.NoError(t, err)

	cleanup := func() {
		server.Close()
	}

	return server, mockService, cleanup
}

func TestHandleCreateDynamicSecretRole(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockDynamicSecretsService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "create role successfully",
			body: map[string]interface{}{
				"name":           "postgres-readonly",
				"generator_type": "postgres",
				"config": map[string]string{
					"connection_url": "postgresql://localhost:5432/mydb",
				},
				"default_ttl": "1h",
				"max_ttl":     "24h",
			},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("CreateRole", mock.Anything, mock.MatchedBy(func(req *service.CreateRoleRequest) bool {
					return req.Name == "postgres-readonly" &&
						req.GeneratorType == "postgres" &&
						req.DefaultTTL == time.Hour &&
						req.MaxTTL == 24*time.Hour
				})).Return(&service.CreateRoleResponse{
					Role: &credentials.Role{
						Name:          "postgres-readonly",
						GeneratorType: "postgres",
						Config:        map[string]string{"connection_url": "postgresql://localhost:5432/mydb"},
						DefaultTTL:    time.Hour,
						MaxTTL:        24 * time.Hour,
					},
					CreatedAt: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp DynamicSecretRoleRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "postgres-readonly", resp.Name)
				assert.Equal(t, "postgres", resp.GeneratorType)
				assert.NotZero(t, resp.CreatedAt)
			},
		},
		{
			name: "missing name returns bad request",
			body: map[string]interface{}{
				"generator_type": "postgres",
				"default_ttl":    "1h",
				"max_ttl":        "24h",
			},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing generator_type returns bad request",
			body: map[string]interface{}{
				"name":        "my-role",
				"default_ttl": "1h",
				"max_ttl":     "24h",
			},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing default_ttl returns bad request",
			body: map[string]interface{}{
				"name":           "my-role",
				"generator_type": "postgres",
				"max_ttl":        "24h",
			},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing max_ttl returns bad request",
			body: map[string]interface{}{
				"name":           "my-role",
				"generator_type": "postgres",
				"default_ttl":    "1h",
			},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "invalid default_ttl format returns bad request",
			body: map[string]interface{}{
				"name":           "my-role",
				"generator_type": "postgres",
				"default_ttl":    "invalid",
				"max_ttl":        "24h",
			},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "invalid max_ttl format returns bad request",
			body: map[string]interface{}{
				"name":           "my-role",
				"generator_type": "postgres",
				"default_ttl":    "1h",
				"max_ttl":        "invalid",
			},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body returns bad request",
			body:           "invalid-json",
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error returns internal server error",
			body: map[string]interface{}{
				"name":           "my-role",
				"generator_type": "postgres",
				"default_ttl":    "1h",
				"max_ttl":        "24h",
			},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("CreateRole", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "CreateRole",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "role already exists returns conflict",
			body: map[string]interface{}{
				"name":           "existing-role",
				"generator_type": "postgres",
				"default_ttl":    "1h",
				"max_ttl":        "24h",
			},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("CreateRole", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrAlreadyExists,
					Op:   "CreateRole",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDynamicSecrets := &MockDynamicSecretsService{}
			tt.setupMock(mockDynamicSecrets)

			server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
			defer cleanup()

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/dynamic/roles", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleCreateDynamicSecretRole(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockDynamicSecrets.AssertExpectations(t)
		})
	}
}

func TestHandleCreateDynamicSecretRole_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := map[string]interface{}{
		"name":           "test-role",
		"generator_type": "postgres",
		"default_ttl":    "1h",
		"max_ttl":        "24h",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/dynamic/roles", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.HandleCreateDynamicSecretRole(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleGetDynamicSecretRole(t *testing.T) {
	tests := []struct {
		name           string
		roleName       string
		setupMock      func(*MockDynamicSecretsService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:     "get role successfully",
			roleName: "postgres-readonly",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GetRole", mock.Anything, mock.MatchedBy(func(req *service.GetRoleRequest) bool {
					return req.Name == "postgres-readonly"
				})).Return(&service.GetRoleResponse{
					Role: &credentials.Role{
						Name:          "postgres-readonly",
						GeneratorType: "postgres",
						Config:        map[string]string{"connection_url": "postgresql://localhost:5432/mydb"},
						DefaultTTL:    time.Hour,
						MaxTTL:        24 * time.Hour,
					},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp DynamicSecretRoleRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "postgres-readonly", resp.Name)
				assert.Equal(t, "postgres", resp.GeneratorType)
			},
		},
		{
			name:           "missing role name returns bad request",
			roleName:       "",
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:     "role not found returns not found",
			roleName: "nonexistent",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GetRole", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "GetRole",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:     "service error returns internal server error",
			roleName: "test-role",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GetRole", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "GetRole",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDynamicSecrets := &MockDynamicSecretsService{}
			tt.setupMock(mockDynamicSecrets)

			server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
			defer cleanup()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/dynamic/roles/"+tt.roleName, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.roleName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetDynamicSecretRole(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockDynamicSecrets.AssertExpectations(t)
		})
	}
}

func TestHandleGetDynamicSecretRole_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/dynamic/roles/test-role", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-role")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetDynamicSecretRole(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleListDynamicSecretRoles(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		setupMock      func(*MockDynamicSecretsService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "list roles successfully",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListRoles", mock.Anything, mock.MatchedBy(func(req *service.ListRolesRequest) bool {
					return req.Prefix == ""
				})).Return(&service.ListRolesResponse{
					Roles: []*credentials.Role{
						{Name: "role1", GeneratorType: "postgres", DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour},
						{Name: "role2", GeneratorType: "mysql", DefaultTTL: 2 * time.Hour, MaxTTL: 48 * time.Hour},
					},
					Count: 2,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListDynamicSecretRolesRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 2, resp.Count)
				assert.Len(t, resp.Roles, 2)
			},
		},
		{
			name:        "list roles with prefix filter",
			queryParams: "prefix=postgres-",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListRoles", mock.Anything, mock.MatchedBy(func(req *service.ListRolesRequest) bool {
					return req.Prefix == "postgres-"
				})).Return(&service.ListRolesResponse{
					Roles: []*credentials.Role{
						{Name: "postgres-readonly", GeneratorType: "postgres", DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour},
					},
					Count: 1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListDynamicSecretRolesRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 1, resp.Count)
			},
		},
		{
			name: "list roles returns empty list",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListRoles", mock.Anything, mock.Anything).Return(&service.ListRolesResponse{
					Roles: []*credentials.Role{},
					Count: 0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListDynamicSecretRolesRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 0, resp.Count)
				assert.Len(t, resp.Roles, 0)
			},
		},
		{
			name: "service error returns internal server error",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListRoles", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "ListRoles",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDynamicSecrets := &MockDynamicSecretsService{}
			tt.setupMock(mockDynamicSecrets)

			server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
			defer cleanup()

			url := "/api/v1/secrets/dynamic/roles"
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.HandleListDynamicSecretRoles(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockDynamicSecrets.AssertExpectations(t)
		})
	}
}

func TestHandleListDynamicSecretRoles_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/dynamic/roles", nil)
	w := httptest.NewRecorder()

	server.HandleListDynamicSecretRoles(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleDeleteDynamicSecretRole(t *testing.T) {
	tests := []struct {
		name           string
		roleName       string
		setupMock      func(*MockDynamicSecretsService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:     "delete role successfully",
			roleName: "postgres-readonly",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("DeleteRole", mock.Anything, mock.MatchedBy(func(req *service.DeleteRoleRequest) bool {
					return req.Name == "postgres-readonly"
				})).Return(&service.DeleteRoleResponse{
					DeletedAt: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp DeleteDynamicSecretRoleRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.NotZero(t, resp.DeletedAt)
			},
		},
		{
			name:           "missing role name returns bad request",
			roleName:       "",
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:     "role not found returns not found",
			roleName: "nonexistent",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("DeleteRole", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "DeleteRole",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:     "service error returns internal server error",
			roleName: "test-role",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("DeleteRole", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "DeleteRole",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDynamicSecrets := &MockDynamicSecretsService{}
			tt.setupMock(mockDynamicSecrets)

			server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
			defer cleanup()

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/dynamic/roles/"+tt.roleName, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.roleName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleDeleteDynamicSecretRole(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockDynamicSecrets.AssertExpectations(t)
		})
	}
}

func TestHandleDeleteDynamicSecretRole_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/dynamic/roles/test-role", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-role")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDeleteDynamicSecretRole(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleGenerateDynamicCredential(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name           string
		roleName       string
		body           interface{}
		setupMock      func(*MockDynamicSecretsService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:     "generate credential successfully without custom TTL",
			roleName: "postgres-readonly",
			body:     map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GenerateCredential", mock.Anything, mock.MatchedBy(func(req *service.GenerateCredentialRequest) bool {
					return req.RoleName == "postgres-readonly" && req.TTL == 0
				})).Return(&service.GenerateCredentialResponse{
					Credential: &credentials.Credential{
						CredentialID: "cred-12345",
						Type:         "database",
						Data: map[string]string{
							"username": "user_12345",
							"password": "securepass",
						},
						LeaseID:   "lease-12345",
						CreatedAt: now,
						ExpiresAt: now.Add(time.Hour),
					},
					Lease: &lease.Lease{
						LeaseID:    "lease-12345",
						SecretPath: "credentials/postgres-readonly/cred-12345",
						IssuedAt:   now,
						ExpiresAt:  now.Add(time.Hour),
						TTL:        time.Hour,
						RenewCount: 0,
						Renewable:  true,
					},
					LeaseID: "lease-12345",
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp GenerateDynamicCredentialRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "cred-12345", resp.CredentialID)
				assert.Equal(t, "user_12345", resp.Username)
				assert.Equal(t, "securepass", resp.Password)
				assert.Equal(t, "lease-12345", resp.LeaseID)
				assert.True(t, resp.Renewable)
			},
		},
		{
			name:     "generate credential with custom TTL",
			roleName: "postgres-readonly",
			body:     map[string]interface{}{"ttl": "2h"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GenerateCredential", mock.Anything, mock.MatchedBy(func(req *service.GenerateCredentialRequest) bool {
					return req.RoleName == "postgres-readonly" && req.TTL == 2*time.Hour
				})).Return(&service.GenerateCredentialResponse{
					Credential: &credentials.Credential{
						CredentialID: "cred-67890",
						Type:         "database",
						Data: map[string]string{
							"username": "user_67890",
							"password": "anotherpass",
						},
						LeaseID:   "lease-67890",
						CreatedAt: now,
						ExpiresAt: now.Add(2 * time.Hour),
					},
					Lease: &lease.Lease{
						LeaseID:    "lease-67890",
						SecretPath: "credentials/postgres-readonly/cred-67890",
						IssuedAt:   now,
						ExpiresAt:  now.Add(2 * time.Hour),
						TTL:        2 * time.Hour,
						RenewCount: 0,
						Renewable:  true,
					},
					LeaseID: "lease-67890",
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "missing role name returns bad request",
			roleName:       "",
			body:           map[string]interface{}{},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid ttl format returns bad request",
			roleName:       "test-role",
			body:           map[string]interface{}{"ttl": "invalid"},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body returns bad request",
			roleName:       "test-role",
			body:           "invalid-json",
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:     "role not found returns not found",
			roleName: "nonexistent",
			body:     map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GenerateCredential", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "GenerateCredential",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:     "service error returns internal server error",
			roleName: "test-role",
			body:     map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GenerateCredential", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "GenerateCredential",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDynamicSecrets := &MockDynamicSecretsService{}
			tt.setupMock(mockDynamicSecrets)

			server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
			defer cleanup()

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/dynamic/credentials/"+tt.roleName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("role", tt.roleName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGenerateDynamicCredential(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockDynamicSecrets.AssertExpectations(t)
		})
	}
}

func TestHandleGenerateDynamicCredential_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/dynamic/credentials/test-role", bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("role", "test-role")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGenerateDynamicCredential(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleRenewDynamicSecretLease(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name           string
		leaseID        string
		body           interface{}
		setupMock      func(*MockDynamicSecretsService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:    "renew lease successfully without increment",
			leaseID: "lease-12345",
			body:    map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RenewLease", mock.Anything, mock.MatchedBy(func(req *service.RenewLeaseRequest) bool {
					return req.LeaseID == "lease-12345" && req.Increment == 0
				})).Return(&service.RenewLeaseResponse{
					Lease: &lease.Lease{
						LeaseID:    "lease-12345",
						SecretPath: "credentials/test/cred-12345",
						IssuedAt:   now,
						ExpiresAt:  now.Add(2 * time.Hour),
						TTL:        time.Hour,
						RenewCount: 1,
						Renewable:  true,
					},
					RenewedAt: now,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RenewDynamicSecretLeaseRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "lease-12345", resp.LeaseID)
				assert.Equal(t, 1, resp.RenewCount)
				assert.NotZero(t, resp.RenewedAt)
			},
		},
		{
			name:    "renew lease with custom increment",
			leaseID: "lease-12345",
			body:    map[string]interface{}{"increment": "30m"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RenewLease", mock.Anything, mock.MatchedBy(func(req *service.RenewLeaseRequest) bool {
					return req.LeaseID == "lease-12345" && req.Increment == 30*time.Minute
				})).Return(&service.RenewLeaseResponse{
					Lease: &lease.Lease{
						LeaseID:    "lease-12345",
						SecretPath: "credentials/test/cred-12345",
						IssuedAt:   now,
						ExpiresAt:  now.Add(30 * time.Minute),
						TTL:        30 * time.Minute,
						RenewCount: 2,
						Renewable:  true,
					},
					RenewedAt: now,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing lease ID returns bad request",
			leaseID:        "",
			body:           map[string]interface{}{},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid increment format returns bad request",
			leaseID:        "lease-12345",
			body:           map[string]interface{}{"increment": "invalid"},
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body returns bad request",
			leaseID:        "lease-12345",
			body:           "invalid-json",
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "lease not found returns not found",
			leaseID: "nonexistent",
			body:    map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RenewLease", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "RenewLease",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:    "service error returns internal server error",
			leaseID: "lease-12345",
			body:    map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RenewLease", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "RenewLease",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDynamicSecrets := &MockDynamicSecretsService{}
			tt.setupMock(mockDynamicSecrets)

			server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
			defer cleanup()

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/dynamic/leases/"+tt.leaseID+"/renew", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.leaseID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleRenewDynamicSecretLease(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockDynamicSecrets.AssertExpectations(t)
		})
	}
}

func TestHandleRenewDynamicSecretLease_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/dynamic/leases/lease-12345/renew", bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "lease-12345")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRenewDynamicSecretLease(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleRevokeDynamicSecretLease(t *testing.T) {
	tests := []struct {
		name           string
		leaseID        string
		setupMock      func(*MockDynamicSecretsService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:    "revoke lease successfully",
			leaseID: "lease-12345",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RevokeLease", mock.Anything, mock.MatchedBy(func(req *service.RevokeLeaseRequest) bool {
					return req.LeaseID == "lease-12345"
				})).Return(&service.RevokeLeaseResponse{
					RevokedAt: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RevokeDynamicSecretLeaseRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.NotZero(t, resp.RevokedAt)
			},
		},
		{
			name:           "missing lease ID returns bad request",
			leaseID:        "",
			setupMock:      func(m *MockDynamicSecretsService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "lease not found returns not found",
			leaseID: "nonexistent",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RevokeLease", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "RevokeLease",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:    "service error returns internal server error",
			leaseID: "lease-12345",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RevokeLease", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "RevokeLease",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDynamicSecrets := &MockDynamicSecretsService{}
			tt.setupMock(mockDynamicSecrets)

			server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
			defer cleanup()

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/dynamic/leases/"+tt.leaseID, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.leaseID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleRevokeDynamicSecretLease(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockDynamicSecrets.AssertExpectations(t)
		})
	}
}

func TestHandleRevokeDynamicSecretLease_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/dynamic/leases/lease-12345", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "lease-12345")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRevokeDynamicSecretLease(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleListDynamicSecretLeases(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name           string
		queryParams    string
		setupMock      func(*MockDynamicSecretsService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "list leases successfully",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListLeases", mock.Anything, mock.MatchedBy(func(req *service.ListLeasesRequest) bool {
					return req.Prefix == ""
				})).Return(&service.ListLeasesResponse{
					Leases: []*lease.Lease{
						{
							LeaseID:    "lease-1",
							SecretPath: "credentials/postgres-readonly/cred-1",
							IssuedAt:   now,
							ExpiresAt:  now.Add(time.Hour),
							TTL:        time.Hour,
							RenewCount: 0,
							Renewable:  true,
							Metadata:   map[string]string{"role": "postgres-readonly"},
						},
						{
							LeaseID:    "lease-2",
							SecretPath: "credentials/mysql-admin/cred-2",
							IssuedAt:   now,
							ExpiresAt:  now.Add(2 * time.Hour),
							TTL:        2 * time.Hour,
							RenewCount: 1,
							Renewable:  true,
							Metadata:   map[string]string{"role": "mysql-admin"},
						},
					},
					Count: 2,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListDynamicSecretLeasesRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 2, resp.Count)
				assert.Len(t, resp.Leases, 2)
				assert.Equal(t, "lease-1", resp.Leases[0].LeaseID)
				assert.Equal(t, "credentials/postgres-readonly/cred-1", resp.Leases[0].Path)
			},
		},
		{
			name:        "list leases with prefix filter",
			queryParams: "prefix=credentials/postgres-",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListLeases", mock.Anything, mock.MatchedBy(func(req *service.ListLeasesRequest) bool {
					return req.Prefix == "credentials/postgres-"
				})).Return(&service.ListLeasesResponse{
					Leases: []*lease.Lease{
						{
							LeaseID:    "lease-1",
							SecretPath: "credentials/postgres-readonly/cred-1",
							IssuedAt:   now,
							ExpiresAt:  now.Add(time.Hour),
							TTL:        time.Hour,
							RenewCount: 0,
							Renewable:  true,
						},
					},
					Count: 1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListDynamicSecretLeasesRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 1, resp.Count)
			},
		},
		{
			name: "list leases returns empty list",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListLeases", mock.Anything, mock.Anything).Return(&service.ListLeasesResponse{
					Leases: []*lease.Lease{},
					Count:  0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListDynamicSecretLeasesRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 0, resp.Count)
				assert.Len(t, resp.Leases, 0)
			},
		},
		{
			name: "service error returns internal server error",
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListLeases", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "ListLeases",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDynamicSecrets := &MockDynamicSecretsService{}
			tt.setupMock(mockDynamicSecrets)

			server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
			defer cleanup()

			url := "/api/v1/secrets/dynamic/leases"
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.HandleListDynamicSecretLeases(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockDynamicSecrets.AssertExpectations(t)
		})
	}
}

func TestHandleListDynamicSecretLeases_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/dynamic/leases", nil)
	w := httptest.NewRecorder()

	server.HandleListDynamicSecretLeases(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestDynamicSecretsRouteRegistration(t *testing.T) {
	mockDynamicSecrets := &MockDynamicSecretsService{}
	server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
	defer cleanup()

	router := server.GetRouter()
	assert.NotNil(t, router)
}

func TestHandleGenerateDynamicCredential_EmptyBody(t *testing.T) {
	now := time.Now()
	mockDynamicSecrets := &MockDynamicSecretsService{}
	mockDynamicSecrets.On("GenerateCredential", mock.Anything, mock.MatchedBy(func(req *service.GenerateCredentialRequest) bool {
		return req.RoleName == "test-role" && req.TTL == 0
	})).Return(&service.GenerateCredentialResponse{
		Credential: &credentials.Credential{
			CredentialID: "cred-empty-body",
			Type:         "database",
			Data:         map[string]string{"username": "user", "password": "pass"},
			LeaseID:      "lease-empty",
			CreatedAt:    now,
			ExpiresAt:    now.Add(time.Hour),
		},
		Lease: &lease.Lease{
			LeaseID:    "lease-empty",
			SecretPath: "credentials/test-role/cred-empty-body",
			IssuedAt:   now,
			ExpiresAt:  now.Add(time.Hour),
			TTL:        time.Hour,
			Renewable:  true,
		},
		LeaseID: "lease-empty",
	}, nil).Once()

	server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/dynamic/credentials/test-role", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("role", "test-role")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGenerateDynamicCredential(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	mockDynamicSecrets.AssertExpectations(t)
}

func TestHandleRenewDynamicSecretLease_EmptyBody(t *testing.T) {
	now := time.Now()
	mockDynamicSecrets := &MockDynamicSecretsService{}
	mockDynamicSecrets.On("RenewLease", mock.Anything, mock.MatchedBy(func(req *service.RenewLeaseRequest) bool {
		return req.LeaseID == "lease-empty-body" && req.Increment == 0
	})).Return(&service.RenewLeaseResponse{
		Lease: &lease.Lease{
			LeaseID:    "lease-empty-body",
			SecretPath: "credentials/test/cred",
			IssuedAt:   now,
			ExpiresAt:  now.Add(time.Hour),
			TTL:        time.Hour,
			RenewCount: 1,
			Renewable:  true,
		},
		RenewedAt: now,
	}, nil).Once()

	server, _, cleanup := setupDynamicSecretsMockServer(t, mockDynamicSecrets)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/dynamic/leases/lease-empty-body/renew", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "lease-empty-body")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRenewDynamicSecretLease(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockDynamicSecrets.AssertExpectations(t)
}
