//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock DynamicSecretsService
// =============================================================================

// MockDynamicSecretsService is a mock implementation of service.DynamicSecretsService.
type MockDynamicSecretsService struct {
	mock.Mock
}

func NewMockDynamicSecretsService() *MockDynamicSecretsService {
	return &MockDynamicSecretsService{}
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

// =============================================================================
// Test QuicBaaS wrapper with DynamicSecrets
// =============================================================================

// testQuicBaaSWithDynamicSecrets wraps mockService and provides a DynamicSecretsService.
type testQuicBaaSWithDynamicSecrets struct {
	*mockService
	dynamicSecretsSvc service.DynamicSecretsService
}

func (m *testQuicBaaSWithDynamicSecrets) DynamicSecrets() service.DynamicSecretsService {
	return m.dynamicSecretsSvc
}

func newTestQuicBaaSWithDynamicSecrets(dsSvc service.DynamicSecretsService) *testQuicBaaSWithDynamicSecrets {
	return &testQuicBaaSWithDynamicSecrets{
		mockService:       &mockService{},
		dynamicSecretsSvc: dsSvc,
	}
}

// =============================================================================
// Helper functions
// =============================================================================

func createTestRole(name, generatorType string) *credentials.Role {
	return &credentials.Role{
		Name:          name,
		GeneratorType: generatorType,
		Config: map[string]string{
			"connection_url": "postgres://localhost:5432/testdb",
		},
		DefaultTTL: 1 * time.Hour,
		MaxTTL:     24 * time.Hour,
	}
}

func createTestCredential(credID, credType, leaseID string) *credentials.Credential {
	now := time.Now()
	return &credentials.Credential{
		CredentialID: credID,
		Type:         credType,
		Data: map[string]string{
			"username": "dynamic_user_123",
			"password": "generated_password",
		},
		LeaseID:   leaseID,
		CreatedAt: now,
		ExpiresAt: now.Add(1 * time.Hour),
	}
}

func createTestLease(leaseID, secretPath string) *lease.Lease {
	now := time.Now()
	return &lease.Lease{
		LeaseID:     leaseID,
		SecretPath:  secretPath,
		IssuedAt:    now,
		ExpiresAt:   now.Add(1 * time.Hour),
		TTL:         1 * time.Hour,
		RenewCount:  0,
		MaxRenewals: 10,
		Renewable:   true,
		Metadata: map[string]string{
			"role": "test-role",
		},
	}
}

func createDynamicSecretsTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-dynamic-secrets-test",
	})
	require.NoError(t, err)
	return log
}

// =============================================================================
// Tool Definition Tests
// =============================================================================

func TestDynamicSecretsGetAllTools(t *testing.T) {
	tools := getAllDynamicSecretsTools()
	assert.Equal(t, 8, len(tools), "expected 8 dynamic secrets tools")

	expectedTools := map[string]bool{
		"quicbaas_dynamic_secrets_create_role":  false,
		"quicbaas_dynamic_secrets_get_role":     false,
		"quicbaas_dynamic_secrets_list_roles":   false,
		"quicbaas_dynamic_secrets_delete_role":  false,
		"quicbaas_dynamic_secrets_generate":     false,
		"quicbaas_dynamic_secrets_renew_lease":  false,
		"quicbaas_dynamic_secrets_revoke_lease": false,
		"quicbaas_dynamic_secrets_list_leases":  false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool name: %s", tool.Name)
		}
		expectedTools[tool.Name] = true
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestDynamicSecretsIsDynamicSecretsTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"create role tool", "quicbaas_dynamic_secrets_create_role", true},
		{"get role tool", "quicbaas_dynamic_secrets_get_role", true},
		{"list roles tool", "quicbaas_dynamic_secrets_list_roles", true},
		{"delete role tool", "quicbaas_dynamic_secrets_delete_role", true},
		{"generate tool", "quicbaas_dynamic_secrets_generate", true},
		{"renew lease tool", "quicbaas_dynamic_secrets_renew_lease", true},
		{"revoke lease tool", "quicbaas_dynamic_secrets_revoke_lease", true},
		{"list leases tool", "quicbaas_dynamic_secrets_list_leases", true},
		{"unknown tool", "quicbaas_unknown", false},
		{"kms tool", "quicbaas_kms_generate_key", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDynamicSecretsTool(tt.toolName)
			assert.Equal(t, tt.expected, result, "isDynamicSecretsTool(%s) = %v, want %v", tt.toolName, result, tt.expected)
		})
	}
}

func TestDynamicSecretsToolSchemas(t *testing.T) {
	tests := []struct {
		name     string
		tool     Tool
		toolName string
		required []string
		props    []string
	}{
		{
			name:     "create role",
			tool:     DynamicSecretsCreateRoleTool,
			toolName: "quicbaas_dynamic_secrets_create_role",
			required: []string{"name", "generator_type", "default_ttl", "max_ttl"},
			props:    []string{"name", "generator_type", "config", "default_ttl", "max_ttl"},
		},
		{
			name:     "get role",
			tool:     DynamicSecretsGetRoleTool,
			toolName: "quicbaas_dynamic_secrets_get_role",
			required: []string{"name"},
			props:    []string{"name"},
		},
		{
			name:     "list roles",
			tool:     DynamicSecretsListRolesTool,
			toolName: "quicbaas_dynamic_secrets_list_roles",
			props:    []string{"prefix"},
		},
		{
			name:     "delete role",
			tool:     DynamicSecretsDeleteRoleTool,
			toolName: "quicbaas_dynamic_secrets_delete_role",
			required: []string{"name"},
			props:    []string{"name"},
		},
		{
			name:     "generate",
			tool:     DynamicSecretsGenerateTool,
			toolName: "quicbaas_dynamic_secrets_generate",
			required: []string{"role_name"},
			props:    []string{"role_name", "ttl"},
		},
		{
			name:     "renew lease",
			tool:     DynamicSecretsRenewLeaseTool,
			toolName: "quicbaas_dynamic_secrets_renew_lease",
			required: []string{"lease_id"},
			props:    []string{"lease_id", "increment"},
		},
		{
			name:     "revoke lease",
			tool:     DynamicSecretsRevokeLeaseTool,
			toolName: "quicbaas_dynamic_secrets_revoke_lease",
			required: []string{"lease_id"},
			props:    []string{"lease_id"},
		},
		{
			name:     "list leases",
			tool:     DynamicSecretsListLeasesTool,
			toolName: "quicbaas_dynamic_secrets_list_leases",
			props:    []string{"prefix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.toolName, tt.tool.Name)
			assert.Equal(t, "object", tt.tool.InputSchema.Type)
			if len(tt.required) == 0 {
				assert.Empty(t, tt.tool.InputSchema.Required)
			}
			for _, field := range tt.required {
				assert.Contains(t, tt.tool.InputSchema.Required, field)
			}
			for _, prop := range tt.props {
				assert.Contains(t, tt.tool.InputSchema.Properties, prop)
			}
		})
	}
}

// =============================================================================
// Service Not Available Tests
// =============================================================================

func TestDynamicSecretsServiceNotAvailable(t *testing.T) {
	log := createDynamicSecretsTestLogger(t)

	// Create mock without DynamicSecretsService
	mockSvc := &mockService{}
	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "create role - service not available",
			toolName: "quicbaas_dynamic_secrets_create_role",
			args: map[string]interface{}{
				"name":           "test-role",
				"generator_type": "postgres",
				"default_ttl":    "1h",
				"max_ttl":        "24h",
			},
		},
		{
			name:     "get role - service not available",
			toolName: "quicbaas_dynamic_secrets_get_role",
			args: map[string]interface{}{
				"name": "test-role",
			},
		},
		{
			name:     "list roles - service not available",
			toolName: "quicbaas_dynamic_secrets_list_roles",
			args:     map[string]interface{}{},
		},
		{
			name:     "delete role - service not available",
			toolName: "quicbaas_dynamic_secrets_delete_role",
			args: map[string]interface{}{
				"name": "test-role",
			},
		},
		{
			name:     "generate credential - service not available",
			toolName: "quicbaas_dynamic_secrets_generate",
			args: map[string]interface{}{
				"role_name": "test-role",
			},
		},
		{
			name:     "renew lease - service not available",
			toolName: "quicbaas_dynamic_secrets_renew_lease",
			args: map[string]interface{}{
				"lease_id": "lease-123",
			},
		},
		{
			name:     "revoke lease - service not available",
			toolName: "quicbaas_dynamic_secrets_revoke_lease",
			args: map[string]interface{}{
				"lease_id": "lease-123",
			},
		},
		{
			name:     "list leases - service not available",
			toolName: "quicbaas_dynamic_secrets_list_leases",
			args:     map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not available")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not configured")
		})
	}
}

// =============================================================================
// Create Role Tests
// =============================================================================

func TestDynamicSecretsCreateRoleSuccess(t *testing.T) {
	log := createDynamicSecretsTestLogger(t)
	mockDS := NewMockDynamicSecretsService()

	testRole := createTestRole("db-role", "postgres")
	now := time.Now()

	mockDS.On("CreateRole", mock.Anything, mock.MatchedBy(func(req *service.CreateRoleRequest) bool {
		return req.Name == "db-role" && req.GeneratorType == "postgres"
	})).Return(&service.CreateRoleResponse{
		Role:      testRole,
		CreatedAt: now,
	}, nil)

	mockSvc := newTestQuicBaaSWithDynamicSecrets(mockDS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"name":           "db-role",
		"generator_type": "postgres",
		"config":         `{"connection_url":"postgres://localhost:5432/testdb"}`,
		"default_ttl":    "1h",
		"max_ttl":        "24h",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dynamic_secrets_create_role", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.NotEmpty(t, resultMap["created_at"])

	roleData, ok := resultMap["role"].(map[string]interface{})
	require.True(t, ok, "role should be a map")
	assert.Equal(t, "db-role", roleData["name"])
	assert.Equal(t, "postgres", roleData["generator_type"])

	mockDS.AssertExpectations(t)
}

// =============================================================================
// Get Role Tests
// =============================================================================

// dynamicSecretsToolCase is one row of a per-tool table: a row with
// errorContains expects the tool to fail with that message, any other row
// expects a result map, which check inspects when set.
type dynamicSecretsToolCase struct {
	name          string
	args          map[string]interface{}
	setupMock     func(*MockDynamicSecretsService)
	errorContains string
	check         func(*testing.T, map[string]interface{})
}

func runDynamicSecretsToolCases(t *testing.T, toolName string, tests []dynamicSecretsToolCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := createDynamicSecretsTestLogger(t)
			mockDS := NewMockDynamicSecretsService()
			if tt.setupMock != nil {
				tt.setupMock(mockDS)
			}
			registry := NewToolRegistry(newTestQuicBaaSWithDynamicSecrets(mockDS), log)

			result, err := registry.ExecuteTool(context.Background(), toolName, tt.args)

			if tt.errorContains != "" {
				require.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
				resultMap, ok := result.(map[string]interface{})
				require.True(t, ok, "result should be a map")
				if tt.check != nil {
					tt.check(t, resultMap)
				}
			}

			mockDS.AssertExpectations(t)
		})
	}
}

func TestDynamicSecretsGetRole(t *testing.T) {
	runDynamicSecretsToolCases(t, "quicbaas_dynamic_secrets_get_role", []dynamicSecretsToolCase{
		{
			name: "success",
			args: map[string]interface{}{"name": "db-role"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GetRole", mock.Anything, mock.MatchedBy(func(req *service.GetRoleRequest) bool {
					return req.Name == "db-role"
				})).Return(&service.GetRoleResponse{
					Role: createTestRole("db-role", "postgres"),
				}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				roleData, ok := resultMap["role"].(map[string]interface{})
				require.True(t, ok, "role should be a map")
				assert.Equal(t, "db-role", roleData["name"])
				assert.Equal(t, "postgres", roleData["generator_type"])
			},
		},
		{
			name:          "missing name",
			args:          map[string]interface{}{},
			errorContains: "name is required",
		},
		{
			name: "role not found",
			args: map[string]interface{}{"name": "nonexistent-role"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GetRole", mock.Anything, mock.Anything).Return(nil, errors.New("role not found"))
			},
			errorContains: "get role failed",
		},
	})
}

// =============================================================================
// List Roles Tests
// =============================================================================

func TestDynamicSecretsListRoles(t *testing.T) {
	runDynamicSecretsToolCases(t, "quicbaas_dynamic_secrets_list_roles", []dynamicSecretsToolCase{
		{
			name: "success",
			args: map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListRoles", mock.Anything, mock.Anything).Return(&service.ListRolesResponse{
					Roles: []*credentials.Role{
						createTestRole("postgres-role", "postgres"),
						createTestRole("mysql-role", "mysql"),
					},
					Count: 2,
				}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				rolesData, ok := resultMap["roles"].([]map[string]interface{})
				require.True(t, ok, "roles should be a slice")
				assert.Len(t, rolesData, 2)
				assert.Equal(t, 2, resultMap["count"])
			},
		},
		{
			name: "prefix is passed to the service",
			args: map[string]interface{}{"prefix": "db-"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListRoles", mock.Anything, mock.MatchedBy(func(req *service.ListRolesRequest) bool {
					return req.Prefix == "db-"
				})).Return(&service.ListRolesResponse{
					Roles: []*credentials.Role{createTestRole("db-postgres", "postgres")},
					Count: 1,
				}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				assert.Equal(t, 1, resultMap["count"])
			},
		},
		{
			name: "service error",
			args: map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListRoles", mock.Anything, mock.Anything).Return(nil, errors.New("internal error"))
			},
			errorContains: "list roles failed",
		},
	})
}

// =============================================================================
// Delete Role Tests
// =============================================================================

func TestDynamicSecretsDeleteRole(t *testing.T) {
	runDynamicSecretsToolCases(t, "quicbaas_dynamic_secrets_delete_role", []dynamicSecretsToolCase{
		{
			name: "success",
			args: map[string]interface{}{"name": "db-role"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("DeleteRole", mock.Anything, mock.MatchedBy(func(req *service.DeleteRoleRequest) bool {
					return req.Name == "db-role"
				})).Return(&service.DeleteRoleResponse{DeletedAt: time.Now()}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				assert.NotEmpty(t, resultMap["deleted_at"])
			},
		},
		{
			name:          "missing name",
			args:          map[string]interface{}{},
			errorContains: "name is required",
		},
		{
			name: "role not found",
			args: map[string]interface{}{"name": "nonexistent-role"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("DeleteRole", mock.Anything, mock.Anything).Return(nil, errors.New("role not found"))
			},
			errorContains: "delete role failed",
		},
	})
}

// =============================================================================
// Generate Credential Tests
// =============================================================================

func TestDynamicSecretsGenerateSuccess(t *testing.T) {
	log := createDynamicSecretsTestLogger(t)
	mockDS := NewMockDynamicSecretsService()

	testCred := createTestCredential("cred-123", "database", "lease-456")
	testLease := createTestLease("lease-456", "credentials/db-role/cred-123")

	mockDS.On("GenerateCredential", mock.Anything, mock.MatchedBy(func(req *service.GenerateCredentialRequest) bool {
		return req.RoleName == "db-role"
	})).Return(&service.GenerateCredentialResponse{
		Credential: testCred,
		Lease:      testLease,
		LeaseID:    "lease-456",
	}, nil)

	mockSvc := newTestQuicBaaSWithDynamicSecrets(mockDS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"role_name": "db-role",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dynamic_secrets_generate", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "lease-456", resultMap["lease_id"])

	credData, ok := resultMap["credential"].(map[string]interface{})
	require.True(t, ok, "credential should be a map")
	assert.Equal(t, "cred-123", credData["credential_id"])

	leaseData, ok := resultMap["lease"].(map[string]interface{})
	require.True(t, ok, "lease should be a map")
	assert.Equal(t, "lease-456", leaseData["lease_id"])

	mockDS.AssertExpectations(t)
}

// =============================================================================
// Renew Lease Tests
// =============================================================================

func TestDynamicSecretsRenewLease(t *testing.T) {
	runDynamicSecretsToolCases(t, "quicbaas_dynamic_secrets_renew_lease", []dynamicSecretsToolCase{
		{
			name: "success",
			args: map[string]interface{}{"lease_id": "lease-456"},
			setupMock: func(m *MockDynamicSecretsService) {
				renewedLease := createTestLease("lease-456", "credentials/db-role/cred-123")
				renewedLease.RenewCount = 1
				m.On("RenewLease", mock.Anything, mock.MatchedBy(func(req *service.RenewLeaseRequest) bool {
					return req.LeaseID == "lease-456"
				})).Return(&service.RenewLeaseResponse{
					Lease:     renewedLease,
					RenewedAt: time.Now(),
				}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				assert.NotEmpty(t, resultMap["renewed_at"])
				leaseData, ok := resultMap["lease"].(map[string]interface{})
				require.True(t, ok, "lease should be a map")
				assert.Equal(t, "lease-456", leaseData["lease_id"])
				assert.Equal(t, 1, leaseData["renew_count"])
			},
		},
		{
			name: "increment is passed to the service",
			args: map[string]interface{}{"lease_id": "lease-456", "increment": "30m"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RenewLease", mock.Anything, mock.MatchedBy(func(req *service.RenewLeaseRequest) bool {
					return req.LeaseID == "lease-456" && req.Increment == 30*time.Minute
				})).Return(&service.RenewLeaseResponse{
					Lease:     createTestLease("lease-456", "credentials/db-role/cred-123"),
					RenewedAt: time.Now(),
				}, nil)
			},
		},
		{
			name:          "missing lease_id",
			args:          map[string]interface{}{},
			errorContains: "lease_id is required",
		},
		{
			name:          "invalid increment",
			args:          map[string]interface{}{"lease_id": "lease-456", "increment": "invalid"},
			errorContains: "invalid increment format",
		},
		{
			name: "lease not found",
			args: map[string]interface{}{"lease_id": "nonexistent-lease"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RenewLease", mock.Anything, mock.Anything).Return(nil, errors.New("lease not found"))
			},
			errorContains: "renew lease failed",
		},
	})
}

// =============================================================================
// Revoke Lease Tests
// =============================================================================

func TestDynamicSecretsRevokeLease(t *testing.T) {
	runDynamicSecretsToolCases(t, "quicbaas_dynamic_secrets_revoke_lease", []dynamicSecretsToolCase{
		{
			name: "success",
			args: map[string]interface{}{"lease_id": "lease-456"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RevokeLease", mock.Anything, mock.MatchedBy(func(req *service.RevokeLeaseRequest) bool {
					return req.LeaseID == "lease-456"
				})).Return(&service.RevokeLeaseResponse{RevokedAt: time.Now()}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				assert.NotEmpty(t, resultMap["revoked_at"])
			},
		},
		{
			name:          "missing lease_id",
			args:          map[string]interface{}{},
			errorContains: "lease_id is required",
		},
		{
			name: "lease not found",
			args: map[string]interface{}{"lease_id": "nonexistent-lease"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("RevokeLease", mock.Anything, mock.Anything).Return(nil, errors.New("lease not found"))
			},
			errorContains: "revoke lease failed",
		},
	})
}

// =============================================================================
// List Leases Tests
// =============================================================================

func TestDynamicSecretsListLeases(t *testing.T) {
	runDynamicSecretsToolCases(t, "quicbaas_dynamic_secrets_list_leases", []dynamicSecretsToolCase{
		{
			name: "success",
			args: map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListLeases", mock.Anything, mock.Anything).Return(&service.ListLeasesResponse{
					Leases: []*lease.Lease{
						createTestLease("lease-1", "credentials/role-1/cred-1"),
						createTestLease("lease-2", "credentials/role-2/cred-2"),
					},
					Count: 2,
				}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				leasesData, ok := resultMap["leases"].([]map[string]interface{})
				require.True(t, ok, "leases should be a slice")
				assert.Len(t, leasesData, 2)
				assert.Equal(t, 2, resultMap["count"])
			},
		},
		{
			name: "prefix is passed to the service",
			args: map[string]interface{}{"prefix": "credentials/postgres"},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListLeases", mock.Anything, mock.MatchedBy(func(req *service.ListLeasesRequest) bool {
					return req.Prefix == "credentials/postgres"
				})).Return(&service.ListLeasesResponse{
					Leases: []*lease.Lease{createTestLease("lease-1", "credentials/postgres/cred-1")},
					Count:  1,
				}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				assert.Equal(t, 1, resultMap["count"])
			},
		},
		{
			name: "lease metadata is included",
			args: map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				leaseWithMeta := createTestLease("lease-1", "credentials/role-1/cred-1")
				leaseWithMeta.Metadata = map[string]string{
					"role":          "test-role",
					"credential_id": "cred-1",
				}
				m.On("ListLeases", mock.Anything, mock.Anything).Return(&service.ListLeasesResponse{
					Leases: []*lease.Lease{leaseWithMeta},
					Count:  1,
				}, nil)
			},
			check: func(t *testing.T, resultMap map[string]interface{}) {
				leasesData, ok := resultMap["leases"].([]map[string]interface{})
				require.True(t, ok, "leases should be a slice")
				require.Len(t, leasesData, 1)
				metadata, ok := leasesData[0]["metadata"].(map[string]string)
				require.True(t, ok, "metadata should be a map")
				assert.Equal(t, "test-role", metadata["role"])
			},
		},
		{
			name: "service error",
			args: map[string]interface{}{},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("ListLeases", mock.Anything, mock.Anything).Return(nil, errors.New("internal error"))
			},
			errorContains: "list leases failed",
		},
	})
}

// =============================================================================
// Tool Registration Tests
// =============================================================================

func TestDynamicSecretsToolRegistration(t *testing.T) {
	log := createDynamicSecretsTestLogger(t)
	mockDS := NewMockDynamicSecretsService()
	mockSvc := newTestQuicBaaSWithDynamicSecrets(mockDS)
	registry := NewToolRegistry(mockSvc, log)

	tools := registry.ListTools()
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{
		"quicbaas_dynamic_secrets_create_role",
		"quicbaas_dynamic_secrets_get_role",
		"quicbaas_dynamic_secrets_list_roles",
		"quicbaas_dynamic_secrets_delete_role",
		"quicbaas_dynamic_secrets_generate",
		"quicbaas_dynamic_secrets_renew_lease",
		"quicbaas_dynamic_secrets_revoke_lease",
		"quicbaas_dynamic_secrets_list_leases",
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}
}

// =============================================================================
// Unknown Tool Tests
// =============================================================================

func TestDynamicSecretsUnknownTool(t *testing.T) {
	log := createDynamicSecretsTestLogger(t)
	mockDS := NewMockDynamicSecretsService()
	mockSvc := newTestQuicBaaSWithDynamicSecrets(mockDS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{}

	result, err := registry.executeDynamicSecretsTool(context.Background(), "quicbaas_dynamic_secrets_unknown", args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool: quicbaas_dynamic_secrets_unknown")
}

// =============================================================================
// Table Driven Tests
// =============================================================================

func TestDynamicSecretsCreateRoleTableDriven(t *testing.T) {
	tests := []struct {
		name          string
		args          map[string]interface{}
		setupMock     func(*MockDynamicSecretsService)
		expectError   bool
		errorContains string
	}{
		{
			name: "success without optional config",
			args: map[string]interface{}{
				"name":           "minimal-role",
				"generator_type": "mysql",
				"default_ttl":    "30m",
				"max_ttl":        "2h",
			},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("CreateRole", mock.Anything, mock.Anything).Return(&service.CreateRoleResponse{
					Role:      createTestRole("minimal-role", "mysql"),
					CreatedAt: time.Now(),
				}, nil)
			},
			expectError: false,
		},
		{
			name: "missing name",
			args: map[string]interface{}{
				"generator_type": "postgres",
				"default_ttl":    "1h",
				"max_ttl":        "24h",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "name is required",
		},
		{
			name: "missing generator_type",
			args: map[string]interface{}{
				"name":        "test-role",
				"default_ttl": "1h",
				"max_ttl":     "24h",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "generator_type is required",
		},
		{
			name: "invalid default_ttl",
			args: map[string]interface{}{
				"name":           "test-role",
				"generator_type": "postgres",
				"default_ttl":    "not-a-duration",
				"max_ttl":        "24h",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "invalid default_ttl format",
		},
		{
			name: "missing default_ttl",
			args: map[string]interface{}{
				"name":           "test-role",
				"generator_type": "postgres",
				"max_ttl":        "24h",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "default_ttl is required",
		},
		{
			name: "missing max_ttl",
			args: map[string]interface{}{
				"name":           "test-role",
				"generator_type": "postgres",
				"default_ttl":    "1h",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "max_ttl is required",
		},
		{
			name: "invalid max_ttl",
			args: map[string]interface{}{
				"name":           "test-role",
				"generator_type": "postgres",
				"default_ttl":    "1h",
				"max_ttl":        "not-a-duration",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "invalid max_ttl format",
		},
		{
			name: "invalid config JSON",
			args: map[string]interface{}{
				"name":           "test-role",
				"generator_type": "postgres",
				"config":         "{invalid-json",
				"default_ttl":    "1h",
				"max_ttl":        "24h",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "invalid config JSON",
		},
		{
			name: "service error",
			args: map[string]interface{}{
				"name":           "error-role",
				"generator_type": "postgres",
				"default_ttl":    "1h",
				"max_ttl":        "24h",
			},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("CreateRole", mock.Anything, mock.Anything).Return(nil, errors.New("database error"))
			},
			expectError:   true,
			errorContains: "create role failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := createDynamicSecretsTestLogger(t)
			mockDS := NewMockDynamicSecretsService()
			tt.setupMock(mockDS)

			mockSvc := newTestQuicBaaSWithDynamicSecrets(mockDS)
			registry := NewToolRegistry(mockSvc, log)

			result, err := registry.ExecuteTool(context.Background(), "quicbaas_dynamic_secrets_create_role", tt.args)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, result)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}

			mockDS.AssertExpectations(t)
		})
	}
}

func TestDynamicSecretsGenerateTableDriven(t *testing.T) {
	tests := []struct {
		name          string
		args          map[string]interface{}
		setupMock     func(*MockDynamicSecretsService)
		expectError   bool
		errorContains string
	}{
		{
			name: "success with default TTL",
			args: map[string]interface{}{
				"role_name": "db-role",
			},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GenerateCredential", mock.Anything, mock.MatchedBy(func(req *service.GenerateCredentialRequest) bool {
					return req.RoleName == "db-role" && req.TTL == 0
				})).Return(&service.GenerateCredentialResponse{
					Credential: createTestCredential("cred-1", "database", "lease-1"),
					Lease:      createTestLease("lease-1", "credentials/db-role/cred-1"),
					LeaseID:    "lease-1",
				}, nil)
			},
			expectError: false,
		},
		{
			name: "success with custom TTL",
			args: map[string]interface{}{
				"role_name": "db-role",
				"ttl":       "2h",
			},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GenerateCredential", mock.Anything, mock.MatchedBy(func(req *service.GenerateCredentialRequest) bool {
					return req.RoleName == "db-role" && req.TTL == 2*time.Hour
				})).Return(&service.GenerateCredentialResponse{
					Credential: createTestCredential("cred-1", "database", "lease-1"),
					Lease:      createTestLease("lease-1", "credentials/db-role/cred-1"),
					LeaseID:    "lease-1",
				}, nil)
			},
			expectError: false,
		},
		{
			name: "missing role_name",
			args: map[string]interface{}{
				"ttl": "1h",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "role_name is required",
		},
		{
			name: "invalid TTL format",
			args: map[string]interface{}{
				"role_name": "db-role",
				"ttl":       "invalid",
			},
			setupMock:     func(m *MockDynamicSecretsService) {},
			expectError:   true,
			errorContains: "invalid ttl format",
		},
		{
			name: "role not found",
			args: map[string]interface{}{
				"role_name": "nonexistent",
			},
			setupMock: func(m *MockDynamicSecretsService) {
				m.On("GenerateCredential", mock.Anything, mock.Anything).Return(nil, errors.New("role not found"))
			},
			expectError:   true,
			errorContains: "generate credential failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := createDynamicSecretsTestLogger(t)
			mockDS := NewMockDynamicSecretsService()
			tt.setupMock(mockDS)

			mockSvc := newTestQuicBaaSWithDynamicSecrets(mockDS)
			registry := NewToolRegistry(mockSvc, log)

			result, err := registry.ExecuteTool(context.Background(), "quicbaas_dynamic_secrets_generate", tt.args)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, result)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}

			mockDS.AssertExpectations(t)
		})
	}
}
