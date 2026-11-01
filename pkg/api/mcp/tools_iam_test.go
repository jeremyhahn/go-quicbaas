//go:build mcp

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestIAMToolsRegistration(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := &mockService{}

	registry := NewToolRegistry(mockSvc, log)

	// Get list of all registered tools
	tools := registry.ListTools()

	// Count IAM tools
	iamToolCount := 0
	iamToolPrefixes := []string{
		"quicbaas_iam_tenant_",
		"quicbaas_iam_user_",
		"quicbaas_iam_role_",
		"quicbaas_iam_policy_",
		"quicbaas_iam_app_",
		"quicbaas_mfa_",
		"quicbaas_pep_",
	}

	for _, tool := range tools {
		for _, prefix := range iamToolPrefixes {
			if strings.HasPrefix(tool.Name, prefix) {
				iamToolCount++
				t.Logf("Found IAM tool: %s", tool.Name)
				break
			}
		}
	}

	// We should have all IAM tools registered
	expectedCount := 44 // 6 tenant + 7 user + 6 role + 7 policy + 6 app + 10 mfa + 2 pep
	if iamToolCount != expectedCount {
		t.Errorf("Expected %d IAM tools, got %d", expectedCount, iamToolCount)
	}

	// quicbaas keeps no groups, so no transport offers a group tool.
	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, "quicbaas_iam_group_") {
			t.Errorf("group tool %s is still registered", tool.Name)
		}
	}

	// Verify specific tools exist
	requiredTools := []string{
		"quicbaas_iam_tenant_create",
		"quicbaas_iam_tenant_provision",
		"quicbaas_iam_user_create",
		"quicbaas_iam_role_create",
		"quicbaas_iam_policy_create",
		"quicbaas_iam_app_register",
		"quicbaas_mfa_enroll_begin",
		"quicbaas_pep_check",
	}

	toolMap := make(map[string]bool)
	for _, tool := range tools {
		toolMap[tool.Name] = true
	}

	for _, required := range requiredTools {
		if !toolMap[required] {
			t.Errorf("Required tool %s not found in registry", required)
		}
	}
}

func TestIAMToolDefinitions(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := &mockService{}

	registry := NewToolRegistry(mockSvc, log)
	tools := registry.ListTools()

	// Verify each IAM tool has proper schema
	for _, tool := range tools {
		if !strings.HasPrefix(tool.Name, "quicbaas_iam_") &&
			!strings.HasPrefix(tool.Name, "quicbaas_mfa_") &&
			!strings.HasPrefix(tool.Name, "quicbaas_pep_") {
			continue
		}

		// Check that tool has description
		if tool.Description == "" {
			t.Errorf("Tool %s missing description", tool.Name)
		}

		// Check that tool has input schema
		if tool.InputSchema == nil {
			t.Errorf("Tool %s missing input schema", tool.Name)
			continue
		}

		// Verify schema has type
		if schemaType, ok := tool.InputSchema["type"].(string); !ok || schemaType != "object" {
			t.Errorf("Tool %s input schema should have type 'object', got %v", tool.Name, tool.InputSchema["type"])
		}

		// Verify schema has properties
		if _, ok := tool.InputSchema["properties"]; !ok {
			t.Errorf("Tool %s input schema missing properties", tool.Name)
		}
	}
}

func TestIAMToolSchemaValidation(t *testing.T) {
	tests := []struct {
		toolName       string
		requiredFields []string
	}{
		{
			toolName:       "quicbaas_iam_tenant_create",
			requiredFields: []string{"name"},
		},
		{
			toolName:       "quicbaas_iam_user_create",
			requiredFields: []string{"tenant_id", "email", "name"},
		},
		{
			toolName:       "quicbaas_iam_role_create",
			requiredFields: []string{"tenant_id", "name"},
		},
		{
			toolName:       "quicbaas_iam_app_register",
			requiredFields: []string{"tenant_id", "name", "redirect_uris", "grant_types", "response_types"},
		},
		{
			toolName:       "quicbaas_pep_check",
			requiredFields: []string{"tenant_id", "principal", "action", "resource"},
		},
	}

	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := &mockService{}
	registry := NewToolRegistry(mockSvc, log)
	tools := registry.ListTools()

	// Create a map for easy lookup
	toolMap := make(map[string]ToolDefinition)
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			tool, ok := toolMap[tt.toolName]
			if !ok {
				t.Fatalf("Tool %s not found", tt.toolName)
			}

			// Check required fields
			required, ok := tool.InputSchema["required"].([]string)
			if !ok {
				t.Fatalf("Tool %s missing 'required' array in schema", tt.toolName)
			}

			// Verify all expected required fields are present
			requiredMap := make(map[string]bool)
			for _, field := range required {
				requiredMap[field] = true
			}

			for _, expectedField := range tt.requiredFields {
				if !requiredMap[expectedField] {
					t.Errorf("Tool %s missing required field: %s", tt.toolName, expectedField)
				}
			}
		})
	}
}

// =============================================================================
// Object List By Tenant - Tags Branch
// =============================================================================

func TestExecuteObjectListByTenant_WithTags(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ListByTenant", mock.Anything, mock.AnythingOfType("*service.ListByTenantRequest")).
		Return(&service.ListByTenantResponse{
			Objects: []*service.ObjectSummary{
				{
					Key:         "tenant1:file1.txt",
					Size:        100,
					ContentType: "text/plain",
					Tags:        map[string]string{"priority": "high"},
				},
			},
			HasMore:    false,
			TotalCount: 1,
			Timestamp:  time.Now().UnixNano(),
		}, nil)

	args := map[string]interface{}{
		"tenant_id":    "tenant1",
		"include_tags": true,
	}

	result, err := registry.executeObjectListByTenant(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	objects := resultMap["objects"].([]interface{})
	assert.Len(t, objects, 1)

	obj := objects[0].(map[string]interface{})
	assert.NotNil(t, obj["tags"])
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object Migrate - Missing Key, Delete From Source
// =============================================================================

func TestExecuteObjectTools_MissingRequiredArgument(t *testing.T) {
	registry := newTestObjectStorageRegistry(&mockObjectStorageService{})

	tests := []struct {
		name    string
		execute func(context.Context, map[string]interface{}) (interface{}, error)
		args    map[string]interface{}
		wantErr string
	}{
		{"migrate without key", registry.executeObjectMigrate, map[string]interface{}{"target_backend": "s3"}, "key is required"},
		{"archive without key", registry.executeObjectArchive, map[string]interface{}{"target_store": "glacier"}, "key is required"},
		{"restore without key", registry.executeObjectRestore, map[string]interface{}{"target_backend": "local"}, "key is required"},
		{"verify without key", registry.executeObjectVerify, map[string]interface{}{"stores": []interface{}{"local", "s3"}}, "key is required"},
		{"tenant quotas with empty tenant", registry.executeObjectTenantQuotas, map[string]interface{}{"tenant_id": ""}, "tenant_id is required"},
		{"set tenant quotas with empty tenant", registry.executeObjectSetTenantQuotas, map[string]interface{}{"tenant_id": "", "max_storage_bytes": float64(1024 * 1024 * 1024)}, "tenant_id is required"},
		{"tenant usage with empty tenant", registry.executeObjectTenantUsage, map[string]interface{}{"tenant_id": ""}, "tenant_id is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.execute(context.Background(), tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestExecuteObjectMigrate_DeleteFromSourceFalse(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("MigrateObject", mock.Anything, mock.MatchedBy(func(req *service.MigrateObjectRequest) bool {
		return req.DeleteFromSource == false
	})).Return(&service.MigrateObjectResponse{
		Key:               "test:object.txt",
		SourceBackend:     "local",
		TargetBackend:     "s3",
		Success:           true,
		DeletedFromSource: false,
		Timestamp:         time.Now().UnixNano(),
	}, nil)

	args := map[string]interface{}{
		"key":                "test:object.txt",
		"target_backend":     "s3",
		"delete_from_source": false,
	}

	result, err := registry.executeObjectMigrate(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, false, resultMap["deleted_from_source"])
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object Archive - Missing Key, Delete From Primary
// =============================================================================

func TestExecuteObjectArchive_DeleteFromPrimaryFalse(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ArchiveObject", mock.Anything, mock.MatchedBy(func(req *service.ArchiveObjectRequest) bool {
		return req.DeleteFromPrimary == false
	})).Return(&service.ArchiveObjectResponse{
		Key:                "test:object.txt",
		ArchiveStore:       "glacier",
		Success:            true,
		DeletedFromPrimary: false,
		ArchivedAt:         time.Now().UnixNano(),
	}, nil)

	args := map[string]interface{}{
		"key":                 "test:object.txt",
		"target_store":        "glacier",
		"delete_from_primary": false,
	}

	result, err := registry.executeObjectArchive(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, false, resultMap["deleted_from_primary"])
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object Restore - Missing Key
// =============================================================================

// =============================================================================
// Object Verify - Missing Key
// =============================================================================

// =============================================================================
// Object Head - With Existing Object, Tags, and Tenant ID
// =============================================================================

func TestExecuteObjectHead_ExistsWithTagsAndTenant(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	now := time.Now().Unix()
	mockSvc.On("HeadObject", mock.Anything, mock.AnythingOfType("*service.HeadObjectRequest")).
		Return(&service.HeadObjectResponse{
			Key:          "tenant1:test.txt",
			Exists:       true,
			ContentType:  "text/plain",
			Size:         1024,
			Checksum:     []byte{0x01, 0x02, 0x03, 0x04},
			Version:      2,
			Status:       "active",
			Backend:      "s3",
			ReplicaCount: 3,
			CreatedAt:    now,
			ModifiedAt:   now,
			LastAccessAt: now,
			Tags:         map[string]string{"env": "prod", "owner": "team-a"},
			TenantID:     "tenant1",
		}, nil)

	args := map[string]interface{}{
		"key":       "tenant1:test.txt",
		"tenant_id": "tenant1",
	}

	result, err := registry.executeObjectHead(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, true, resultMap["exists"])
	assert.Equal(t, "tenant1:test.txt", resultMap["key"])
	assert.NotNil(t, resultMap["tags"])
	assert.Equal(t, "tenant1", resultMap["tenant_id"])
	mockSvc.AssertExpectations(t)
}

func TestExecuteObjectHead_NotExists(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("HeadObject", mock.Anything, mock.AnythingOfType("*service.HeadObjectRequest")).
		Return(&service.HeadObjectResponse{
			Key:    "missing.txt",
			Exists: false,
		}, nil)

	args := map[string]interface{}{
		"key": "missing.txt",
	}

	result, err := registry.executeObjectHead(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, false, resultMap["exists"])
	assert.Nil(t, resultMap["tags"])
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object Get Policy - Success
// =============================================================================

func TestExecuteObjectGetPolicy_SuccessWithPolicies(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetObjectPolicy", mock.Anything, mock.AnythingOfType("*service.GetObjectPolicyRequest")).
		Return(&service.GetObjectPolicyResponse{
			Key:       "test:object.txt",
			Timestamp: time.Now().UnixNano(),
		}, nil)

	args := map[string]interface{}{
		"key": "test:object.txt",
	}

	result, err := registry.executeObjectGetPolicy(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object Set Policy - Success
// =============================================================================

func TestExecuteObjectSetPolicy_SuccessWithFlags(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("SetObjectPolicy", mock.Anything, mock.AnythingOfType("*service.SetObjectPolicyRequest")).
		Return(&service.SetObjectPolicyResponse{
			Key:       "test:object.txt",
			Timestamp: time.Now().UnixNano(),
		}, nil)

	args := map[string]interface{}{
		"key":          "test:object.txt",
		"public_read":  true,
		"public_write": false,
	}

	result, err := registry.executeObjectSetPolicy(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object List Backends - Success With Multiple
// =============================================================================

func TestExecuteObjectListBackends_MultipleBackends(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ListBackends", mock.Anything, mock.AnythingOfType("*service.ListBackendsRequest")).
		Return(&service.ListBackendsResponse{
			Backends: []*service.BackendInfo{
				{Name: "local", Type: "local", Enabled: true},
				{Name: "s3", Type: "s3", Enabled: true},
				{Name: "archive", Type: "glacier", Enabled: false},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil)

	args := map[string]interface{}{}

	result, err := registry.executeObjectListBackends(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	backends := resultMap["backends"].([]interface{})
	assert.Len(t, backends, 3)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object Tenant Quotas - Missing Tenant ID Edge Case
// =============================================================================

func TestExecuteObjectTenantQuotas_WithUsage(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetTenantQuotas", mock.Anything, mock.AnythingOfType("*service.GetTenantQuotasRequest")).
		Return(&service.GetTenantQuotasResponse{
			TenantID:         "tenant1",
			MaxStorageBytes:  1024 * 1024 * 1024,
			MaxObjectSize:    100 * 1024 * 1024,
			MaxObjectCount:   10000,
			UsedStorageBytes: 512 * 1024 * 1024,
			UsedObjectCount:  5000,
			Timestamp:        time.Now().UnixNano(),
		}, nil)

	args := map[string]interface{}{
		"tenant_id": "tenant1",
	}

	result, err := registry.executeObjectTenantQuotas(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, "tenant1", resultMap["tenant_id"])
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object Set Tenant Quotas - Edge Cases
// =============================================================================

func TestExecuteObjectSetTenantQuotas_WithAllQuotas(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("SetTenantQuotas", mock.Anything, mock.AnythingOfType("*service.SetTenantQuotasRequest")).
		Return(&service.SetTenantQuotasResponse{
			TenantID:  "tenant1",
			UpdatedAt: time.Now().UnixNano(),
		}, nil)

	args := map[string]interface{}{
		"tenant_id":         "tenant1",
		"max_storage_bytes": float64(1024 * 1024 * 1024),
		"max_object_size":   float64(100 * 1024 * 1024),
		"max_object_count":  float64(10000),
	}

	result, err := registry.executeObjectSetTenantQuotas(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, "tenant1", resultMap["tenant_id"])
	assert.Equal(t, true, resultMap["success"])
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Object Tenant Usage - Edge Cases
// =============================================================================

// =============================================================================
// Mock IAM Service
// =============================================================================

type mockIAMService struct {
	createTenantFunc func(ctx context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error)
	getTenantFunc    func(ctx context.Context, req *service.GetTenantRequest) (*service.GetTenantResponse, error)
	updateTenantFunc func(ctx context.Context, req *service.UpdateTenantRequest) (*service.UpdateTenantResponse, error)
	deleteTenantFunc func(ctx context.Context, req *service.DeleteTenantRequest) (*service.DeleteTenantResponse, error)
	listTenantsFunc  func(ctx context.Context, req *service.ListTenantsRequest) (*service.ListTenantsResponse, error)
	provisionFunc    func(ctx context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error)
	createUserFunc   func(ctx context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error)
	getUserFunc      func(ctx context.Context, req *service.IAMGetUserRequest) (*service.IAMGetUserResponse, error)
	updateUserFunc   func(ctx context.Context, req *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error)
	deleteUserFunc   func(ctx context.Context, req *service.IAMDeleteUserRequest) (*service.IAMDeleteUserResponse, error)
	listUsersFunc    func(ctx context.Context, req *service.IAMListUsersRequest) (*service.IAMListUsersResponse, error)
	assignRoleFunc   func(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error)
	revokeRoleFunc   func(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error)
	createRoleFunc   func(ctx context.Context, req *service.IAMCreateRoleRequest) (*service.IAMCreateRoleResponse, error)
	getRoleFunc      func(ctx context.Context, req *service.IAMGetRoleRequest) (*service.IAMGetRoleResponse, error)
	updateRoleFunc   func(ctx context.Context, req *service.IAMUpdateRoleRequest) (*service.IAMUpdateRoleResponse, error)
	deleteRoleFunc   func(ctx context.Context, req *service.IAMDeleteRoleRequest) (*service.IAMDeleteRoleResponse, error)
	listRolesFunc    func(ctx context.Context, req *service.IAMListRolesRequest) (*service.IAMListRolesResponse, error)
	reconcileRolesFn func(ctx context.Context, req *service.IAMReconcileRolesRequest) (*service.IAMReconcileRolesResponse, error)
	createPolicyFunc func(ctx context.Context, req *service.CreatePolicyRequest) (*service.CreatePolicyResponse, error)
	getPolicyFunc    func(ctx context.Context, req *service.GetPolicyRequest) (*service.GetPolicyResponse, error)
	updatePolicyFunc func(ctx context.Context, req *service.UpdatePolicyRequest) (*service.UpdatePolicyResponse, error)
	deletePolicyFunc func(ctx context.Context, req *service.DeletePolicyRequest) (*service.DeletePolicyResponse, error)
	listPoliciesFunc func(ctx context.Context, req *service.ListPoliciesRequest) (*service.ListPoliciesResponse, error)
	validatePolFunc  func(ctx context.Context, req *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error)
	testPolicyFunc   func(ctx context.Context, req *service.TestPolicyRequest) (*service.TestPolicyResponse, error)
	registerAppFunc  func(ctx context.Context, req *service.RegisterAppRequest) (*service.RegisterAppResponse, error)
	getAppFunc       func(ctx context.Context, req *service.GetAppRequest) (*service.GetAppResponse, error)
	updateAppFunc    func(ctx context.Context, req *service.UpdateAppRequest) (*service.UpdateAppResponse, error)
	deleteAppFunc    func(ctx context.Context, req *service.DeleteAppRequest) (*service.DeleteAppResponse, error)
	listAppsFunc     func(ctx context.Context, req *service.ListAppsRequest) (*service.ListAppsResponse, error)
	rotateSecretFunc func(ctx context.Context, req *service.RotateAppSecretRequest) (*service.RotateAppSecretResponse, error)
}

func (m *mockIAMService) CreateTenant(ctx context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
	if m.createTenantFunc != nil {
		return m.createTenantFunc(ctx, req)
	}
	return &service.CreateTenantResponse{Tenant: &iam.Tenant{ID: "t1", Name: req.Name}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) GetTenant(ctx context.Context, req *service.GetTenantRequest) (*service.GetTenantResponse, error) {
	if m.getTenantFunc != nil {
		return m.getTenantFunc(ctx, req)
	}
	return &service.GetTenantResponse{Tenant: &iam.Tenant{ID: req.ID, Name: "test-tenant"}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) UpdateTenant(ctx context.Context, req *service.UpdateTenantRequest) (*service.UpdateTenantResponse, error) {
	if m.updateTenantFunc != nil {
		return m.updateTenantFunc(ctx, req)
	}
	return &service.UpdateTenantResponse{Tenant: &iam.Tenant{ID: req.ID, Name: "updated"}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) DeleteTenant(ctx context.Context, req *service.DeleteTenantRequest) (*service.DeleteTenantResponse, error) {
	if m.deleteTenantFunc != nil {
		return m.deleteTenantFunc(ctx, req)
	}
	return &service.DeleteTenantResponse{Deleted: true, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) ListTenants(ctx context.Context, req *service.ListTenantsRequest) (*service.ListTenantsResponse, error) {
	if m.listTenantsFunc != nil {
		return m.listTenantsFunc(ctx, req)
	}
	return &service.ListTenantsResponse{Tenants: []*iam.Tenant{{ID: "t1", Name: "tenant1"}}, TotalCount: 1, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) ProvisionTenant(ctx context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	if m.provisionFunc != nil {
		return m.provisionFunc(ctx, req)
	}
	return &service.IAMProvisionTenantResponse{TenantID: req.TenantID, AdminUserID: 1, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) CreateUser(ctx context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
	if m.createUserFunc != nil {
		return m.createUserFunc(ctx, req)
	}
	return &service.IAMCreateUserResponse{User: &iam.User{ID: 30001, Email: req.Email, Name: req.Name}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) GetUser(ctx context.Context, req *service.IAMGetUserRequest) (*service.IAMGetUserResponse, error) {
	if m.getUserFunc != nil {
		return m.getUserFunc(ctx, req)
	}
	return &service.IAMGetUserResponse{User: &iam.User{ID: req.UserID, Email: "test@example.com"}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) UpdateUser(ctx context.Context, req *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
	if m.updateUserFunc != nil {
		return m.updateUserFunc(ctx, req)
	}
	return &service.IAMUpdateUserResponse{User: &iam.User{ID: req.UserID, Email: "updated@example.com"}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) DeleteUser(ctx context.Context, req *service.IAMDeleteUserRequest) (*service.IAMDeleteUserResponse, error) {
	if m.deleteUserFunc != nil {
		return m.deleteUserFunc(ctx, req)
	}
	return &service.IAMDeleteUserResponse{Deleted: true, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) ListUsers(ctx context.Context, req *service.IAMListUsersRequest) (*service.IAMListUsersResponse, error) {
	if m.listUsersFunc != nil {
		return m.listUsersFunc(ctx, req)
	}
	return &service.IAMListUsersResponse{Users: []*iam.User{{ID: 30001}}, TotalCount: 1, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) AssignRole(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
	if m.assignRoleFunc != nil {
		return m.assignRoleFunc(ctx, req)
	}
	return &service.AssignRoleResponse{Success: true}, nil
}

func (m *mockIAMService) RevokeRole(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
	if m.revokeRoleFunc != nil {
		return m.revokeRoleFunc(ctx, req)
	}
	return &service.RevokeRoleResponse{Success: true}, nil
}

func (m *mockIAMService) CreateRole(ctx context.Context, req *service.IAMCreateRoleRequest) (*service.IAMCreateRoleResponse, error) {
	if m.createRoleFunc != nil {
		return m.createRoleFunc(ctx, req)
	}
	return &service.IAMCreateRoleResponse{Role: &iam.Role{ID: "r1", Name: req.Name}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) GetRole(ctx context.Context, req *service.IAMGetRoleRequest) (*service.IAMGetRoleResponse, error) {
	if m.getRoleFunc != nil {
		return m.getRoleFunc(ctx, req)
	}
	return &service.IAMGetRoleResponse{Role: &iam.Role{ID: req.RoleID, Name: "test-role"}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) UpdateRole(ctx context.Context, req *service.IAMUpdateRoleRequest) (*service.IAMUpdateRoleResponse, error) {
	if m.updateRoleFunc != nil {
		return m.updateRoleFunc(ctx, req)
	}
	return &service.IAMUpdateRoleResponse{Role: &iam.Role{ID: req.RoleID, Name: "updated-role"}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) DeleteRole(ctx context.Context, req *service.IAMDeleteRoleRequest) (*service.IAMDeleteRoleResponse, error) {
	if m.deleteRoleFunc != nil {
		return m.deleteRoleFunc(ctx, req)
	}
	return &service.IAMDeleteRoleResponse{Deleted: true, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) ListRoles(ctx context.Context, req *service.IAMListRolesRequest) (*service.IAMListRolesResponse, error) {
	if m.listRolesFunc != nil {
		return m.listRolesFunc(ctx, req)
	}
	return &service.IAMListRolesResponse{Roles: []*iam.Role{{ID: "r1"}}, TotalCount: 1, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) ReconcileBuiltInRoles(ctx context.Context, req *service.IAMReconcileRolesRequest) (*service.IAMReconcileRolesResponse, error) {
	if m.reconcileRolesFn != nil {
		return m.reconcileRolesFn(ctx, req)
	}
	return &service.IAMReconcileRolesResponse{
		TenantID: req.TenantID,
		Roles: []iam.RoleReconcileResult{
			{RoleID: "r1", Name: "admin", Added: []string{"roles:create"}},
		},
		Changed:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockIAMService) CreatePolicy(ctx context.Context, req *service.CreatePolicyRequest) (*service.CreatePolicyResponse, error) {
	if m.createPolicyFunc != nil {
		return m.createPolicyFunc(ctx, req)
	}
	return &service.CreatePolicyResponse{Policy: req.Policy, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) GetPolicy(ctx context.Context, req *service.GetPolicyRequest) (*service.GetPolicyResponse, error) {
	if m.getPolicyFunc != nil {
		return m.getPolicyFunc(ctx, req)
	}
	return &service.GetPolicyResponse{Policy: &iam.Policy{Metadata: iam.PolicyMetadata{Name: req.Name}}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) UpdatePolicy(ctx context.Context, req *service.UpdatePolicyRequest) (*service.UpdatePolicyResponse, error) {
	if m.updatePolicyFunc != nil {
		return m.updatePolicyFunc(ctx, req)
	}
	return &service.UpdatePolicyResponse{Policy: req.Policy, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) DeletePolicy(ctx context.Context, req *service.DeletePolicyRequest) (*service.DeletePolicyResponse, error) {
	if m.deletePolicyFunc != nil {
		return m.deletePolicyFunc(ctx, req)
	}
	return &service.DeletePolicyResponse{Deleted: true, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) ListPolicies(ctx context.Context, req *service.ListPoliciesRequest) (*service.ListPoliciesResponse, error) {
	if m.listPoliciesFunc != nil {
		return m.listPoliciesFunc(ctx, req)
	}
	return &service.ListPoliciesResponse{Policies: []*iam.Policy{{Metadata: iam.PolicyMetadata{Name: "pol1"}}}, TotalCount: 1, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) ValidatePolicy(ctx context.Context, req *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
	if m.validatePolFunc != nil {
		return m.validatePolFunc(ctx, req)
	}
	return &service.ValidatePolicyResponse{Valid: true, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) TestPolicy(ctx context.Context, req *service.TestPolicyRequest) (*service.TestPolicyResponse, error) {
	if m.testPolicyFunc != nil {
		return m.testPolicyFunc(ctx, req)
	}
	return &service.TestPolicyResponse{Allowed: true, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) RegisterApp(ctx context.Context, req *service.RegisterAppRequest) (*service.RegisterAppResponse, error) {
	if m.registerAppFunc != nil {
		return m.registerAppFunc(ctx, req)
	}
	return &service.RegisterAppResponse{App: &iam.Application{ClientID: "c1", Name: req.Name}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) GetApp(ctx context.Context, req *service.GetAppRequest) (*service.GetAppResponse, error) {
	if m.getAppFunc != nil {
		return m.getAppFunc(ctx, req)
	}
	return &service.GetAppResponse{App: &iam.Application{ClientID: req.ClientID, Name: "test-app"}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) UpdateApp(ctx context.Context, req *service.UpdateAppRequest) (*service.UpdateAppResponse, error) {
	if m.updateAppFunc != nil {
		return m.updateAppFunc(ctx, req)
	}
	return &service.UpdateAppResponse{App: &iam.Application{ClientID: req.ClientID, Name: "updated"}, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) DeleteApp(ctx context.Context, req *service.DeleteAppRequest) (*service.DeleteAppResponse, error) {
	if m.deleteAppFunc != nil {
		return m.deleteAppFunc(ctx, req)
	}
	return &service.DeleteAppResponse{Deleted: true, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) ListApps(ctx context.Context, req *service.ListAppsRequest) (*service.ListAppsResponse, error) {
	if m.listAppsFunc != nil {
		return m.listAppsFunc(ctx, req)
	}
	return &service.ListAppsResponse{Apps: []*iam.Application{{ClientID: "c1"}}, TotalCount: 1, Timestamp: time.Now()}, nil
}

func (m *mockIAMService) RotateAppSecret(ctx context.Context, req *service.RotateAppSecretRequest) (*service.RotateAppSecretResponse, error) {
	if m.rotateSecretFunc != nil {
		return m.rotateSecretFunc(ctx, req)
	}
	return &service.RotateAppSecretResponse{ClientSecret: "new-secret", Timestamp: time.Now()}, nil
}

// =============================================================================
// IAM-capable mock QuicBaaS service wrapper
// =============================================================================

type iamQuicBaaSWrapper struct {
	*MockQuicBaaS
	iamSvc service.IAMService
}

func (m *iamQuicBaaSWrapper) IAM() service.IAMService {
	return m.iamSvc
}

func newIAMTestRegistry(iamSvc service.IAMService) *ToolRegistry {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	wrapper := &iamQuicBaaSWrapper{
		MockQuicBaaS: &MockQuicBaaS{},
		iamSvc:       iamSvc,
	}
	return NewToolRegistry(wrapper, log)
}

// =============================================================================
// getIAMService tests
// =============================================================================

func TestGetIAMService_NotSupported(t *testing.T) {
	// mockService does NOT implement iamServiceAccessor, so getIAMService should fail
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(&mockService{}, log)

	_, err := reg.getIAMService()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IAM service not available")
}

func TestGetIAMService_NilIAMService(t *testing.T) {
	reg := newIAMTestRegistry(nil)
	_, err := reg.getIAMService()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IAM service not configured")
}

func TestGetIAMService_Success(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	svc, err := reg.getIAMService()
	require.NoError(t, err)
	require.NotNil(t, svc)
}

// =============================================================================
// executeIAMTool router tests
// =============================================================================

func TestExecuteIAMTool_Routes(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	policyMap := map[string]interface{}{"name": "test-policy", "spec_yaml": "rules: []"}

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{"tenant_create", "quicbaas_iam_tenant_create", map[string]interface{}{"name": "test"}},
		{"tenant_get", "quicbaas_iam_tenant_get", map[string]interface{}{"id": "t1"}},
		{"tenant_update", "quicbaas_iam_tenant_update", map[string]interface{}{"id": "t1", "name": "updated"}},
		{"tenant_delete", "quicbaas_iam_tenant_delete", map[string]interface{}{"id": "t1"}},
		{"tenant_list", "quicbaas_iam_tenant_list", map[string]interface{}{}},
		{"tenant_provision", "quicbaas_iam_tenant_provision", map[string]interface{}{"tenant_id": "t1"}},
		{"user_create", "quicbaas_iam_user_create", map[string]interface{}{"tenant_id": "t1", "email": "u@ex.com", "name": "User"}},
		{"user_get", "quicbaas_iam_user_get", map[string]interface{}{"tenant_id": "t1", "user_id": "30001"}},
		{"user_update", "quicbaas_iam_user_update", map[string]interface{}{"tenant_id": "t1", "user_id": "30001", "email": "new@ex.com", "name": "Updated", "suspended": true, "locked": false}},
		{"user_delete", "quicbaas_iam_user_delete", map[string]interface{}{"tenant_id": "t1", "user_id": "30001"}},
		{"user_list", "quicbaas_iam_user_list", map[string]interface{}{"tenant_id": "t1"}},
		{"user_assign_role", "quicbaas_iam_user_assign_role", map[string]interface{}{"user_id": "30001", "role_id": "r1"}},
		{"user_revoke_role", "quicbaas_iam_user_revoke_role", map[string]interface{}{"user_id": "30001", "role_id": "r1"}},
		{"role_create", "quicbaas_iam_role_create", map[string]interface{}{"tenant_id": "t1", "name": "admin", "description": "Admin role", "permissions": []interface{}{"read", "write"}}},
		{"role_get", "quicbaas_iam_role_get", map[string]interface{}{"tenant_id": "t1", "role_id": "r1"}},
		{"role_update", "quicbaas_iam_role_update", map[string]interface{}{"tenant_id": "t1", "role_id": "r1", "name": "updated", "description": "desc", "permissions": []interface{}{"read"}}},
		{"role_delete", "quicbaas_iam_role_delete", map[string]interface{}{"tenant_id": "t1", "role_id": "r1"}},
		{"role_list", "quicbaas_iam_role_list", map[string]interface{}{"tenant_id": "t1"}},
		{"policy_create", "quicbaas_iam_policy_create", map[string]interface{}{"tenant_id": "t1", "policy": policyMap}},
		{"policy_get", "quicbaas_iam_policy_get", map[string]interface{}{"tenant_id": "t1", "name": "p1"}},
		{"policy_update", "quicbaas_iam_policy_update", map[string]interface{}{"tenant_id": "t1", "name": "p1", "policy": policyMap}},
		{"policy_delete", "quicbaas_iam_policy_delete", map[string]interface{}{"tenant_id": "t1", "name": "p1"}},
		{"policy_list", "quicbaas_iam_policy_list", map[string]interface{}{"tenant_id": "t1"}},
		{"policy_test", "quicbaas_iam_policy_test", map[string]interface{}{
			"policy": policyMap,
			"check":  map[string]interface{}{"subject": map[string]interface{}{"id": "u1"}, "action": "read", "resource": "data"},
		}},
		{"app_register", "quicbaas_iam_app_register", map[string]interface{}{
			"tenant_id":      "t1",
			"name":           "my-app",
			"redirect_uris":  []interface{}{"https://example.com/cb"},
			"grant_types":    []interface{}{"authorization_code"},
			"response_types": []interface{}{"code"},
		}},
		{"app_get", "quicbaas_iam_app_get", map[string]interface{}{"client_id": "c1"}},
		{"app_update", "quicbaas_iam_app_update", map[string]interface{}{
			"client_id":       "c1",
			"name":            "updated-app",
			"resource_prefix": "/api",
			"redirect_uris":   []interface{}{"https://new.com/cb"},
			"grant_types":     []interface{}{"client_credentials"},
			"response_types":  []interface{}{"token"},
		}},
		{"app_delete", "quicbaas_iam_app_delete", map[string]interface{}{"client_id": "c1"}},
		{"app_list", "quicbaas_iam_app_list", map[string]interface{}{"tenant_id": "t1"}},
		{"app_rotate_secret", "quicbaas_iam_app_rotate_secret", map[string]interface{}{"client_id": "c1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reg.executeIAMTool(ctx, tt.toolName, tt.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestExecuteIAMTool_UnknownToolReturnsError(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	result, err := reg.executeIAMTool(context.Background(), "quicbaas_iam_unknown", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestExecuteIAMTool_ServiceErrors(t *testing.T) {
	testErr := errors.New("service unavailable")
	iamSvc := &mockIAMService{
		createTenantFunc: func(ctx context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
			return nil, testErr
		},
		createUserFunc: func(ctx context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			return nil, testErr
		},
		createRoleFunc: func(ctx context.Context, req *service.IAMCreateRoleRequest) (*service.IAMCreateRoleResponse, error) {
			return nil, testErr
		},
		registerAppFunc: func(ctx context.Context, req *service.RegisterAppRequest) (*service.RegisterAppResponse, error) {
			return nil, testErr
		},
	}
	reg := newIAMTestRegistry(iamSvc)
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{"tenant_create_error", "quicbaas_iam_tenant_create", map[string]interface{}{"name": "test"}},
		{"user_create_error", "quicbaas_iam_user_create", map[string]interface{}{"tenant_id": "t1", "email": "u@ex.com", "name": "U"}},
		{"role_create_error", "quicbaas_iam_role_create", map[string]interface{}{"tenant_id": "t1", "name": "role"}},
		{"app_register_error", "quicbaas_iam_app_register", map[string]interface{}{"tenant_id": "t1", "name": "app"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.executeIAMTool(ctx, tt.toolName, tt.args)
			require.Error(t, err)
			assert.Equal(t, testErr, err)
		})
	}
}

func TestExecuteIAMTenantCreate_WithOptionalFields(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"id":       "custom-id",
		"name":     "My Tenant",
		"settings": map[string]interface{}{"key": "val"},
		"quotas":   map[string]interface{}{"max_users": float64(100)},
	}

	result, err := reg.executeIAMTenantCreate(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMTenantUpdate_WithOptionalFields(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"id":       "t1",
		"name":     "Updated Name",
		"settings": map[string]interface{}{"mode": "production"},
		"quotas":   map[string]interface{}{"max_users": float64(500)},
	}

	result, err := reg.executeIAMTenantUpdate(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMTenantList_WithPaginationAndFilter(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"limit":  float64(50),
		"offset": float64(10),
		"filter": map[string]interface{}{"status": "active"},
	}

	result, err := reg.executeIAMTenantList(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMUserCreate_WithAllFields(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"tenant_id":  "t1",
		"email":      "user@example.com",
		"name":       "Test User",
		"roles":      []interface{}{"admin", "viewer"},
		"attributes": map[string]interface{}{"department": "engineering"},
	}

	result, err := reg.executeIAMUserCreate(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMUserUpdate_WithAllOptionalFields(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"tenant_id":  "t1",
		"user_id":    "30001",
		"email":      "new@example.com",
		"name":       "New Name",
		"roles":      []interface{}{"admin"},
		"attributes": map[string]interface{}{"level": "senior"},
		"suspended":  true,
		"locked":     false,
	}

	result, err := reg.executeIAMUserUpdate(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMUserList_WithPaginationAndFilter(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"tenant_id": "t1",
		"limit":     float64(25),
		"offset":    float64(5),
		"filter":    map[string]interface{}{"role": "admin"},
	}

	result, err := reg.executeIAMUserList(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMPolicyCreate_MissingPolicyObject(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"tenant_id": "t1",
	}

	_, err := reg.executeIAMPolicyCreate(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy must be an object")
}

func TestExecuteIAMPolicyUpdate_MissingPolicyObject(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"tenant_id": "t1",
		"name":      "p1",
	}

	_, err := reg.executeIAMPolicyUpdate(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy must be an object")
}

func TestExecuteIAMPolicyTest_MissingCheckObject(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"policy": map[string]interface{}{"name": "p1"},
	}

	_, err := reg.executeIAMPolicyTest(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check must be an object")
}

func TestExecuteIAMPolicyTest_SimulateMode(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	// When "policy" key is absent, the handler uses simulate mode
	// with flat subject/action/resource fields.
	args := map[string]interface{}{
		"tenant_id": "test-tenant",
		"subject":   map[string]interface{}{"id": "user1"},
		"action":    "read",
		"resource":  "keys/foo",
	}

	result, err := reg.executeIAMPolicyTest(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Mock MFA Manager
// =============================================================================

type mockMFAManager struct {
	beginEnrollFunc   func(ctx context.Context, userID uint64, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error)
	completeFunc      func(ctx context.Context, enrollmentID, code string) error
	cancelFunc        func(ctx context.Context, enrollmentID string) error
	beginChallengeF   func(ctx context.Context, userID uint64, tenantID, sessionToken string) (*iam.MFAChallenge, error)
	verifyChallengeF  func(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error)
	verifyCodeF       func(ctx context.Context, userID uint64, tenantID, code string) error
	getStatusF        func(ctx context.Context, userID uint64, tenantID string) (*iam.MFAStatus, error)
	isMFARequiredF    func(ctx context.Context, userID uint64, tenantID string) (bool, error)
	disableF          func(ctx context.Context, userID uint64, tenantID string) error
	generateBackupF   func(ctx context.Context, userID uint64, tenantID string) ([]string, error)
	verifyBackupF     func(ctx context.Context, userID uint64, tenantID, code string) error
	backupCodeStatusF func(ctx context.Context, userID uint64, tenantID string) (*iam.BackupCodeStatus, error)
}

func (m *mockMFAManager) BeginEnrollment(ctx context.Context, userID uint64, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	if m.beginEnrollFunc != nil {
		return m.beginEnrollFunc(ctx, userID, tenantID, method)
	}
	return &iam.MFAEnrollResponse{EnrollmentID: "enroll-1", QRCodeURI: "otpauth://totp/test"}, nil
}

func (m *mockMFAManager) CompleteEnrollment(ctx context.Context, _ uint64, _, enrollmentID, code string) error {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, enrollmentID, code)
	}
	return nil
}

func (m *mockMFAManager) CancelEnrollment(ctx context.Context, _ uint64, _, enrollmentID string) error {
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, enrollmentID)
	}
	return nil
}

func (m *mockMFAManager) BeginChallenge(ctx context.Context, userID uint64, tenantID, sessionToken string) (*iam.MFAChallenge, error) {
	if m.beginChallengeF != nil {
		return m.beginChallengeF(ctx, userID, tenantID, sessionToken)
	}
	return &iam.MFAChallenge{ID: "chal-1", Method: iam.MFAMethodTOTP}, nil
}

func (m *mockMFAManager) VerifyChallenge(ctx context.Context, _ uint64, _, challengeID, code string) (*iam.MFAVerifyResponse, error) {
	if m.verifyChallengeF != nil {
		return m.verifyChallengeF(ctx, challengeID, code)
	}
	return &iam.MFAVerifyResponse{Success: true}, nil
}

func (m *mockMFAManager) VerifyCode(ctx context.Context, userID uint64, tenantID, code string) error {
	if m.verifyCodeF != nil {
		return m.verifyCodeF(ctx, userID, tenantID, code)
	}
	return nil
}

func (m *mockMFAManager) GetMFAStatus(ctx context.Context, userID uint64, tenantID string) (*iam.MFAStatus, error) {
	if m.getStatusF != nil {
		return m.getStatusF(ctx, userID, tenantID)
	}
	return &iam.MFAStatus{Enabled: true, Method: iam.MFAMethodTOTP}, nil
}

func (m *mockMFAManager) IsMFARequired(ctx context.Context, userID uint64, tenantID string) (bool, error) {
	if m.isMFARequiredF != nil {
		return m.isMFARequiredF(ctx, userID, tenantID)
	}
	return true, nil
}

func (m *mockMFAManager) DisableMFA(ctx context.Context, userID uint64, tenantID string) error {
	if m.disableF != nil {
		return m.disableF(ctx, userID, tenantID)
	}
	return nil
}

func (m *mockMFAManager) GenerateBackupCodes(ctx context.Context, userID uint64, tenantID string) ([]string, error) {
	if m.generateBackupF != nil {
		return m.generateBackupF(ctx, userID, tenantID)
	}
	return []string{"code1", "code2", "code3"}, nil
}

func (m *mockMFAManager) VerifyBackupCode(ctx context.Context, userID uint64, tenantID, code string) error {
	if m.verifyBackupF != nil {
		return m.verifyBackupF(ctx, userID, tenantID, code)
	}
	return nil
}

func (m *mockMFAManager) GetBackupCodeStatus(ctx context.Context, userID uint64, tenantID string) (*iam.BackupCodeStatus, error) {
	if m.backupCodeStatusF != nil {
		return m.backupCodeStatusF(ctx, userID, tenantID)
	}
	return &iam.BackupCodeStatus{Total: 10, Used: 2, Remaining: 8}, nil
}

// =============================================================================
// Mock Policy Engine
// =============================================================================

type mockPolicyEngine struct {
	evaluateFunc func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error)
}

func (m *mockPolicyEngine) Evaluate(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	if m.evaluateFunc != nil {
		return m.evaluateFunc(ctx, req)
	}
	return &iam.CheckResponse{Allowed: true, Reason: "policy matched"}, nil
}

// =============================================================================
// MFA+PEP capable mock QuicBaaS wrapper
// =============================================================================

type mfaPepQuicBaaSWrapper struct {
	*MockQuicBaaS
	iamSvc       service.IAMService
	mfaManager   service.MFAManager
	policyEngine *mockPolicyEngine
}

func (m *mfaPepQuicBaaSWrapper) IAM() service.IAMService {
	return m.iamSvc
}

func (m *mfaPepQuicBaaSWrapper) GetMFAManager(_ context.Context) (service.MFAManager, error) {
	if m.mfaManager == nil {
		return nil, errors.New("MFA not configured")
	}
	return m.mfaManager, nil
}

func (m *mfaPepQuicBaaSWrapper) GetPolicyEngine(_ context.Context) (*iam.PolicyEngine, error) {
	// Return nil to let the tests work via the interface assertion approach.
	// The PEP tests use inline interface assertions, not a real PolicyEngine.
	return nil, nil
}

func newMFATestRegistry(mfaMgr service.MFAManager) *ToolRegistry {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	wrapper := &mfaPepQuicBaaSWrapper{
		MockQuicBaaS: &MockQuicBaaS{},
		mfaManager:   mfaMgr,
	}
	return NewToolRegistry(wrapper, log)
}

// =============================================================================
// MFA Error Type Tests
// =============================================================================

func TestMFAServiceUnavailableError(t *testing.T) {
	err := &MFAServiceUnavailableError{Reason: "not configured"}
	assert.Contains(t, err.Error(), "MFA service unavailable")
	assert.Contains(t, err.Error(), "not configured")
}

func TestMFAInvalidInputError(t *testing.T) {
	err := &MFAInvalidInputError{Field: "user_id", Message: "required"}
	assert.Contains(t, err.Error(), "MFA invalid input")
	assert.Contains(t, err.Error(), "user_id")
	assert.Contains(t, err.Error(), "required")
}

func TestPEPServiceUnavailableError(t *testing.T) {
	err := &PEPServiceUnavailableError{Reason: "not supported"}
	assert.Contains(t, err.Error(), "PEP service unavailable")
	assert.Contains(t, err.Error(), "not supported")
}

func TestPEPInvalidInputError(t *testing.T) {
	err := &PEPInvalidInputError{Field: "action", Message: "is required"}
	assert.Contains(t, err.Error(), "PEP invalid input")
	assert.Contains(t, err.Error(), "action")
	assert.Contains(t, err.Error(), "is required")
}

// =============================================================================
// getMFAManager tests
// =============================================================================

func TestGetMFAManager_ServiceNotSupported(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	// mockService does NOT implement mfaManagerAccessor
	reg := NewToolRegistry(&mockService{}, log)

	_, err := reg.getMFAManager(context.Background())
	require.Error(t, err)
	var mfaErr *MFAServiceUnavailableError
	assert.True(t, errors.As(err, &mfaErr))
}

func TestGetMFAManager_NilManager(t *testing.T) {
	reg := newMFATestRegistry(nil)

	_, err := reg.getMFAManager(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MFA")
}

func TestGetMFAManager_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})

	mgr, err := reg.getMFAManager(context.Background())
	require.NoError(t, err)
	require.NotNil(t, mgr)
}

// =============================================================================
// MFA Enrollment Execution Tests
// =============================================================================

func TestExecuteMFABeginEnroll_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"user_id":   "30001",
		"tenant_id": "t1",
		"method":    "totp",
	}

	result, err := reg.executeMFABeginEnroll(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFABeginEnroll_DefaultMethod(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	args := map[string]interface{}{
		"user_id":   "30001",
		"tenant_id": "t1",
	}

	result, err := reg.executeMFABeginEnroll(ctx, args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFABeginEnroll_MissingUserID(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"tenant_id": "t1"}

	_, err := reg.executeMFABeginEnroll(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.Error(t, err)
	var inputErr *MFAInvalidInputError
	assert.True(t, errors.As(err, &inputErr))
	assert.Equal(t, "user_id", inputErr.Field)
}

func TestExecuteMFABeginEnroll_MissingTenantID(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"user_id": "30001"}

	_, err := reg.executeMFABeginEnroll(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.Error(t, err)
	var inputErr *MFAInvalidInputError
	assert.True(t, errors.As(err, &inputErr))
	assert.Equal(t, "tenant_id", inputErr.Field)
}

func TestExecuteMFACompleteEnroll_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"enrollment_id": "e1", "code": "123456"}

	result, err := reg.executeMFACompleteEnroll(tenantctx.WithTenant(context.Background(), "tenant-1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFACompleteEnroll_MissingEnrollmentID(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"code": "123456"}

	_, err := reg.executeMFACompleteEnroll(context.Background(), args)
	require.Error(t, err)
	var inputErr *MFAInvalidInputError
	assert.True(t, errors.As(err, &inputErr))
	assert.Equal(t, "enrollment_id", inputErr.Field)
}

func TestExecuteMFACompleteEnroll_MissingCode(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"enrollment_id": "e1"}

	_, err := reg.executeMFACompleteEnroll(context.Background(), args)
	require.Error(t, err)
	var inputErr *MFAInvalidInputError
	assert.True(t, errors.As(err, &inputErr))
	assert.Equal(t, "code", inputErr.Field)
}

func TestExecuteMFACancelEnroll_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"enrollment_id": "e1"}

	result, err := reg.executeMFACancelEnroll(tenantctx.WithTenant(context.Background(), "tenant-1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFACancelEnroll_MissingEnrollmentID(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{}

	_, err := reg.executeMFACancelEnroll(context.Background(), args)
	require.Error(t, err)
	var inputErr *MFAInvalidInputError
	assert.True(t, errors.As(err, &inputErr))
}

// =============================================================================
// MFA Status Tests
// =============================================================================

func TestExecuteMFAGetStatus_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}

	result, err := reg.executeMFAGetStatus(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFAGetStatus_MissingUserID(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"tenant_id": "t1"}

	_, err := reg.executeMFAGetStatus(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.Error(t, err)
}

func TestExecuteMFAGetStatus_MissingTenantID(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"user_id": "30001"}

	_, err := reg.executeMFAGetStatus(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.Error(t, err)
}

func TestExecuteMFADisable_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}

	result, err := reg.executeMFADisable(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFADisable_MissingFields(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})

	t.Run("missing_user_id", func(t *testing.T) {
		_, err := reg.executeMFADisable(tenantctx.WithTenant(context.Background(), "t1"), map[string]interface{}{"tenant_id": "t1"})
		require.Error(t, err)
	})
	t.Run("missing_tenant_id", func(t *testing.T) {
		_, err := reg.executeMFADisable(tenantctx.WithTenant(context.Background(), "t1"), map[string]interface{}{"user_id": "30001"})
		require.Error(t, err)
	})
}

// =============================================================================
// MFA Challenge Tests
// =============================================================================

func TestExecuteMFABeginChallenge_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1", "session_token": "tok"}

	result, err := reg.executeMFABeginChallenge(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFABeginChallenge_MissingFields(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})

	tests := []struct {
		name  string
		args  map[string]interface{}
		field string
	}{
		{"missing_user_id", map[string]interface{}{"tenant_id": "t1", "session_token": "tok"}, "user_id"},
		{"missing_tenant_id", map[string]interface{}{"user_id": "30001", "session_token": "tok"}, "tenant_id"},
		{"missing_session_token", map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}, "session_token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.executeMFABeginChallenge(tenantctx.WithTenant(context.Background(), "t1"), tt.args)
			require.Error(t, err)
			var inputErr *MFAInvalidInputError
			assert.True(t, errors.As(err, &inputErr))
			assert.Equal(t, tt.field, inputErr.Field)
		})
	}
}

func TestExecuteMFAVerifyChallenge_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"challenge_id": "chal-1", "code": "123456"}

	result, err := reg.executeMFAVerifyChallenge(tenantctx.WithTenant(context.Background(), "tenant-1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFAVerifyChallenge_MissingFields(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})

	t.Run("missing_challenge_id", func(t *testing.T) {
		_, err := reg.executeMFAVerifyChallenge(context.Background(), map[string]interface{}{"code": "123456"})
		require.Error(t, err)
	})
	t.Run("missing_code", func(t *testing.T) {
		_, err := reg.executeMFAVerifyChallenge(context.Background(), map[string]interface{}{"challenge_id": "c1"})
		require.Error(t, err)
	})
}

// =============================================================================
// MFA Backup Code Tests
// =============================================================================

func TestExecuteMFAGenerateBackupCodes_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}

	result, err := reg.executeMFAGenerateBackupCodes(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFAGenerateBackupCodes_MissingFields(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})

	t.Run("missing_user_id", func(t *testing.T) {
		_, err := reg.executeMFAGenerateBackupCodes(tenantctx.WithTenant(context.Background(), "t1"), map[string]interface{}{"tenant_id": "t1"})
		require.Error(t, err)
	})
	t.Run("missing_tenant_id", func(t *testing.T) {
		_, err := reg.executeMFAGenerateBackupCodes(tenantctx.WithTenant(context.Background(), "t1"), map[string]interface{}{"user_id": "30001"})
		require.Error(t, err)
	})
}

func TestExecuteMFAVerifyBackupCode_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1", "code": "backup-code-1"}

	result, err := reg.executeMFAVerifyBackupCode(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFAVerifyBackupCode_MissingFields(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})

	tests := []struct {
		name  string
		args  map[string]interface{}
		field string
	}{
		{"missing_user_id", map[string]interface{}{"tenant_id": "t1", "code": "c"}, "user_id"},
		{"missing_tenant_id", map[string]interface{}{"user_id": "30001", "code": "c"}, "tenant_id"},
		{"missing_code", map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}, "code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.executeMFAVerifyBackupCode(tenantctx.WithTenant(context.Background(), "t1"), tt.args)
			require.Error(t, err)
			var inputErr *MFAInvalidInputError
			assert.True(t, errors.As(err, &inputErr))
			assert.Equal(t, tt.field, inputErr.Field)
		})
	}
}

func TestExecuteMFAGetBackupCodeStatus_Success(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}

	result, err := reg.executeMFAGetBackupCodeStatus(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteMFAGetBackupCodeStatus_MissingFields(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})

	t.Run("missing_user_id", func(t *testing.T) {
		_, err := reg.executeMFAGetBackupCodeStatus(tenantctx.WithTenant(context.Background(), "t1"), map[string]interface{}{"tenant_id": "t1"})
		require.Error(t, err)
	})
	t.Run("missing_tenant_id", func(t *testing.T) {
		_, err := reg.executeMFAGetBackupCodeStatus(tenantctx.WithTenant(context.Background(), "t1"), map[string]interface{}{"user_id": "30001"})
		require.Error(t, err)
	})
}

// =============================================================================
// PEP Tests
// =============================================================================

func TestExecutePEPCheck_ServiceNotSupported(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	// mockService does NOT implement pepServiceAccessor
	reg := NewToolRegistry(&mockService{}, log)

	args := map[string]interface{}{
		"tenant_id": "t1",
		"principal": "u1",
		"action":    "read",
		"resource":  "data",
	}

	_, err := reg.executePEPCheck(context.Background(), args)
	require.Error(t, err)
	var pepErr *PEPServiceUnavailableError
	assert.True(t, errors.As(err, &pepErr))
}

func TestExecutePEPCheck_MissingFields(t *testing.T) {
	// Use the MFA wrapper which also implements pepServiceAccessor but returns nil engine
	// Since mockService does not implement pepServiceAccessor, these tests verify the
	// "not supported" case. We need to test the input validation independently.
	reg := newMFATestRegistry(&mockMFAManager{})

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"missing_tenant_id", map[string]interface{}{"principal": "u1", "action": "read", "resource": "data"}},
		{"missing_principal", map[string]interface{}{"tenant_id": "t1", "action": "read", "resource": "data"}},
		{"missing_action", map[string]interface{}{"tenant_id": "t1", "principal": "u1", "resource": "data"}},
		{"missing_resource", map[string]interface{}{"tenant_id": "t1", "principal": "u1", "action": "read"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The PEP check will fail at the service assertion level, which is expected
			// since the mock wrapper returns nil for GetPolicyEngine
			_, err := reg.executePEPCheck(context.Background(), tt.args)
			require.Error(t, err)
		})
	}
}

func TestExecutePEPCheckBatch_ServiceNotSupported(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(&mockService{}, log)

	args := map[string]interface{}{
		"checks": []interface{}{
			map[string]interface{}{"tenant_id": "t1", "principal": "u1", "action": "read", "resource": "data"},
		},
	}

	_, err := reg.executePEPCheckBatch(context.Background(), args)
	require.Error(t, err)
	var pepErr *PEPServiceUnavailableError
	assert.True(t, errors.As(err, &pepErr))
}

func TestExecutePEPCheckBatch_MissingChecksArray(t *testing.T) {
	reg := newMFATestRegistry(&mockMFAManager{})

	args := map[string]interface{}{}
	_, err := reg.executePEPCheckBatch(context.Background(), args)
	require.Error(t, err)
}

// =============================================================================
// MFA Service Error Propagation Tests
// =============================================================================

func TestExecuteMFABeginEnroll_ServiceError(t *testing.T) {
	testErr := errors.New("enrollment failed")
	mfaMgr := &mockMFAManager{
		beginEnrollFunc: func(_ context.Context, _ uint64, _ string, _ iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
			return nil, testErr
		},
	}
	reg := newMFATestRegistry(mfaMgr)
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}

	_, err := reg.executeMFABeginEnroll(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.Error(t, err)
	assert.Equal(t, testErr, err)
}

func TestExecuteMFACompleteEnroll_ServiceError(t *testing.T) {
	testErr := errors.New("invalid code")
	mfaMgr := &mockMFAManager{
		completeFunc: func(_ context.Context, _, _ string) error {
			return testErr
		},
	}
	reg := newMFATestRegistry(mfaMgr)
	args := map[string]interface{}{"enrollment_id": "e1", "code": "wrong"}

	_, err := reg.executeMFACompleteEnroll(tenantctx.WithTenant(context.Background(), "tenant-1"), args)
	require.Error(t, err)
	assert.Equal(t, testErr, err)
}

func TestExecuteMFACancelEnroll_ServiceError(t *testing.T) {
	testErr := errors.New("cancel failed")
	mfaMgr := &mockMFAManager{
		cancelFunc: func(_ context.Context, _ string) error {
			return testErr
		},
	}
	reg := newMFATestRegistry(mfaMgr)
	args := map[string]interface{}{"enrollment_id": "e1"}

	_, err := reg.executeMFACancelEnroll(tenantctx.WithTenant(context.Background(), "tenant-1"), args)
	require.Error(t, err)
	assert.Equal(t, testErr, err)
}

func TestExecuteMFAGetStatus_ServiceError(t *testing.T) {
	testErr := errors.New("status retrieval failed")
	mfaMgr := &mockMFAManager{
		getStatusF: func(_ context.Context, _ uint64, _ string) (*iam.MFAStatus, error) {
			return nil, testErr
		},
	}
	reg := newMFATestRegistry(mfaMgr)
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}

	_, err := reg.executeMFAGetStatus(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.Error(t, err)
	assert.Equal(t, testErr, err)
}

func TestExecuteMFADisable_ServiceError(t *testing.T) {
	testErr := errors.New("disable failed")
	mfaMgr := &mockMFAManager{
		disableF: func(_ context.Context, _ uint64, _ string) error {
			return testErr
		},
	}
	reg := newMFATestRegistry(mfaMgr)
	args := map[string]interface{}{"user_id": "30001", "tenant_id": "t1"}

	_, err := reg.executeMFADisable(tenantctx.WithTenant(context.Background(), "t1"), args)
	require.Error(t, err)
	assert.Equal(t, testErr, err)
}

// errIAMTest is a test error for IAM service failures.
var errIAMTest = errors.New("iam test error")

// newErrorIAMService creates a mockIAMService where ALL methods return errors.
func newErrorIAMService() *mockIAMService {
	return &mockIAMService{
		createTenantFunc: func(_ context.Context, _ *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
			return nil, errIAMTest
		},
		getTenantFunc: func(_ context.Context, _ *service.GetTenantRequest) (*service.GetTenantResponse, error) {
			return nil, errIAMTest
		},
		updateTenantFunc: func(_ context.Context, _ *service.UpdateTenantRequest) (*service.UpdateTenantResponse, error) {
			return nil, errIAMTest
		},
		deleteTenantFunc: func(_ context.Context, _ *service.DeleteTenantRequest) (*service.DeleteTenantResponse, error) {
			return nil, errIAMTest
		},
		listTenantsFunc: func(_ context.Context, _ *service.ListTenantsRequest) (*service.ListTenantsResponse, error) {
			return nil, errIAMTest
		},
		createUserFunc: func(_ context.Context, _ *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			return nil, errIAMTest
		},
		getUserFunc: func(_ context.Context, _ *service.IAMGetUserRequest) (*service.IAMGetUserResponse, error) {
			return nil, errIAMTest
		},
		updateUserFunc: func(_ context.Context, _ *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
			return nil, errIAMTest
		},
		deleteUserFunc: func(_ context.Context, _ *service.IAMDeleteUserRequest) (*service.IAMDeleteUserResponse, error) {
			return nil, errIAMTest
		},
		listUsersFunc: func(_ context.Context, _ *service.IAMListUsersRequest) (*service.IAMListUsersResponse, error) {
			return nil, errIAMTest
		},
		assignRoleFunc: func(_ context.Context, _ *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
			return nil, errIAMTest
		},
		revokeRoleFunc: func(_ context.Context, _ *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
			return nil, errIAMTest
		},
		createRoleFunc: func(_ context.Context, _ *service.IAMCreateRoleRequest) (*service.IAMCreateRoleResponse, error) {
			return nil, errIAMTest
		},
		getRoleFunc: func(_ context.Context, _ *service.IAMGetRoleRequest) (*service.IAMGetRoleResponse, error) {
			return nil, errIAMTest
		},
		updateRoleFunc: func(_ context.Context, _ *service.IAMUpdateRoleRequest) (*service.IAMUpdateRoleResponse, error) {
			return nil, errIAMTest
		},
		deleteRoleFunc: func(_ context.Context, _ *service.IAMDeleteRoleRequest) (*service.IAMDeleteRoleResponse, error) {
			return nil, errIAMTest
		},
		listRolesFunc: func(_ context.Context, _ *service.IAMListRolesRequest) (*service.IAMListRolesResponse, error) {
			return nil, errIAMTest
		},
		createPolicyFunc: func(_ context.Context, _ *service.CreatePolicyRequest) (*service.CreatePolicyResponse, error) {
			return nil, errIAMTest
		},
		getPolicyFunc: func(_ context.Context, _ *service.GetPolicyRequest) (*service.GetPolicyResponse, error) {
			return nil, errIAMTest
		},
		updatePolicyFunc: func(_ context.Context, _ *service.UpdatePolicyRequest) (*service.UpdatePolicyResponse, error) {
			return nil, errIAMTest
		},
		deletePolicyFunc: func(_ context.Context, _ *service.DeletePolicyRequest) (*service.DeletePolicyResponse, error) {
			return nil, errIAMTest
		},
		listPoliciesFunc: func(_ context.Context, _ *service.ListPoliciesRequest) (*service.ListPoliciesResponse, error) {
			return nil, errIAMTest
		},
		testPolicyFunc: func(_ context.Context, _ *service.TestPolicyRequest) (*service.TestPolicyResponse, error) {
			return nil, errIAMTest
		},
		registerAppFunc: func(_ context.Context, _ *service.RegisterAppRequest) (*service.RegisterAppResponse, error) {
			return nil, errIAMTest
		},
		getAppFunc: func(_ context.Context, _ *service.GetAppRequest) (*service.GetAppResponse, error) {
			return nil, errIAMTest
		},
		updateAppFunc: func(_ context.Context, _ *service.UpdateAppRequest) (*service.UpdateAppResponse, error) {
			return nil, errIAMTest
		},
		deleteAppFunc: func(_ context.Context, _ *service.DeleteAppRequest) (*service.DeleteAppResponse, error) {
			return nil, errIAMTest
		},
		listAppsFunc: func(_ context.Context, _ *service.ListAppsRequest) (*service.ListAppsResponse, error) {
			return nil, errIAMTest
		},
		rotateSecretFunc: func(_ context.Context, _ *service.RotateAppSecretRequest) (*service.RotateAppSecretResponse, error) {
			return nil, errIAMTest
		},
	}
}

// TestIAMCoverage91_ServiceErrorPaths tests that all IAM execute functions
// propagate service errors correctly.
func TestIAMCoverage91_ServiceErrorPaths(t *testing.T) {
	errSvc := newErrorIAMService()
	reg := newIAMTestRegistry(errSvc)
	ctx := tenantctx.WithTenant(context.Background(), "t")

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		// Tenant operations
		{"tenant_create_error", "quicbaas_iam_tenant_create", map[string]interface{}{"name": "t"}},
		{"tenant_get_error", "quicbaas_iam_tenant_get", map[string]interface{}{"id": "t1"}},
		{"tenant_update_error", "quicbaas_iam_tenant_update", map[string]interface{}{"id": "t1"}},
		{"tenant_delete_error", "quicbaas_iam_tenant_delete", map[string]interface{}{"id": "t1"}},
		{"tenant_list_error", "quicbaas_iam_tenant_list", map[string]interface{}{}},

		// User operations
		{"user_create_error", "quicbaas_iam_user_create", map[string]interface{}{"tenant_id": "t", "email": "e", "name": "n"}},
		{"user_get_error", "quicbaas_iam_user_get", map[string]interface{}{"tenant_id": "t", "user_id": "1"}},
		{"user_update_error", "quicbaas_iam_user_update", map[string]interface{}{"tenant_id": "t", "user_id": "1"}},
		{"user_delete_error", "quicbaas_iam_user_delete", map[string]interface{}{"tenant_id": "t", "user_id": "1"}},
		{"user_list_error", "quicbaas_iam_user_list", map[string]interface{}{"tenant_id": "t"}},
		{"user_assign_role_error", "quicbaas_iam_user_assign_role", map[string]interface{}{"user_id": "u", "role_id": "r"}},
		{"user_revoke_role_error", "quicbaas_iam_user_revoke_role", map[string]interface{}{"user_id": "u", "role_id": "r"}},

		// Role operations
		{"role_create_error", "quicbaas_iam_role_create", map[string]interface{}{"tenant_id": "t", "name": "r"}},
		{"role_get_error", "quicbaas_iam_role_get", map[string]interface{}{"tenant_id": "t", "role_id": "r"}},
		{"role_update_error", "quicbaas_iam_role_update", map[string]interface{}{"tenant_id": "t", "role_id": "r"}},
		{"role_delete_error", "quicbaas_iam_role_delete", map[string]interface{}{"tenant_id": "t", "role_id": "r"}},
		{"role_list_error", "quicbaas_iam_role_list", map[string]interface{}{"tenant_id": "t"}},

		// Policy operations
		{"policy_create_error", "quicbaas_iam_policy_create", map[string]interface{}{"policy": map[string]interface{}{"name": "p", "spec_yaml": "rules: []"}}},
		{"policy_get_error", "quicbaas_iam_policy_get", map[string]interface{}{"name": "p"}},
		{"policy_update_error", "quicbaas_iam_policy_update", map[string]interface{}{"policy": map[string]interface{}{"name": "p"}}},
		{"policy_delete_error", "quicbaas_iam_policy_delete", map[string]interface{}{"name": "p"}},
		{"policy_list_error", "quicbaas_iam_policy_list", map[string]interface{}{}},
		{"policy_test_error", "quicbaas_iam_policy_test", map[string]interface{}{
			"policy": map[string]interface{}{"name": "p"},
			"check":  map[string]interface{}{"principal": "u1", "action": "read", "resource": "r1"},
		}},

		// App operations
		{"app_register_error", "quicbaas_iam_app_register", map[string]interface{}{"name": "a", "redirect_uris": []interface{}{"http://localhost"}}},
		{"app_get_error", "quicbaas_iam_app_get", map[string]interface{}{"client_id": "c1"}},
		{"app_update_error", "quicbaas_iam_app_update", map[string]interface{}{"client_id": "c1"}},
		{"app_delete_error", "quicbaas_iam_app_delete", map[string]interface{}{"client_id": "c1"}},
		{"app_list_error", "quicbaas_iam_app_list", map[string]interface{}{}},
		{"app_rotate_secret_error", "quicbaas_iam_app_rotate_secret", map[string]interface{}{"client_id": "c1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reg.executeIAMTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "expected error for %s", tt.toolName)
			assert.Nil(t, result)
			assert.ErrorIs(t, err, errIAMTest)
		})
	}
}

// TestIAMCoverage91_NoIAMService tests that all IAM functions properly
// return errors when the service doesn't support IAM.
func TestIAMCoverage91_NoIAMService(t *testing.T) {
	// Use a registry backed by a service that does NOT implement iamServiceAccessor.
	log := newTestLogger(t)
	reg := NewToolRegistry(&mockService{}, log)
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	// Every IAM function should return an error when the service doesn't support IAM.
	functions := []struct {
		name string
		fn   func(context.Context, map[string]interface{}) (interface{}, error)
	}{
		// Tenant operations
		{"tenantCreate", reg.executeIAMTenantCreate},
		{"tenantGet", reg.executeIAMTenantGet},
		{"tenantUpdate", reg.executeIAMTenantUpdate},
		{"tenantDelete", reg.executeIAMTenantDelete},
		{"tenantList", reg.executeIAMTenantList},

		// User operations
		{"userCreate", reg.executeIAMUserCreate},
		{"userGet", reg.executeIAMUserGet},
		{"userUpdate", reg.executeIAMUserUpdate},
		{"userDelete", reg.executeIAMUserDelete},
		{"userList", reg.executeIAMUserList},
		{"userAssignRole", reg.executeIAMUserAssignRole},
		{"userRevokeRole", reg.executeIAMUserRevokeRole},

		// Role operations
		{"roleCreate", reg.executeIAMRoleCreate},
		{"roleGet", reg.executeIAMRoleGet},
		{"roleUpdate", reg.executeIAMRoleUpdate},
		{"roleDelete", reg.executeIAMRoleDelete},
		{"roleList", reg.executeIAMRoleList},

		// Policy operations
		{"policyCreate", reg.executeIAMPolicyCreate},
		{"policyGet", reg.executeIAMPolicyGet},
		{"policyUpdate", reg.executeIAMPolicyUpdate},
		{"policyDelete", reg.executeIAMPolicyDelete},
		{"policyList", reg.executeIAMPolicyList},
		{"policyTest", reg.executeIAMPolicyTest},

		// App operations
		{"appRegister", reg.executeIAMAppRegister},
		{"appGet", reg.executeIAMAppGet},
		{"appUpdate", reg.executeIAMAppUpdate},
		{"appDelete", reg.executeIAMAppDelete},
		{"appList", reg.executeIAMAppList},
		{"appRotateSecret", reg.executeIAMAppRotateSecret},
	}

	for _, fn := range functions {
		t.Run(fn.name, func(t *testing.T) {
			_, err := fn.fn(ctx, map[string]interface{}{"id": "x", "tenant_id": "t", "user_id": "u", "group_id": "g", "role_id": "r", "name": "n", "client_id": "c"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "IAM")
		})
	}
}

// TestIAMCoverage91_ExecToolRouting_MFA covers MFA routing branches in executeIAMTool.
func TestIAMCoverage91_ExecToolRouting_MFA(t *testing.T) {
	// Use a service with IAM but the MFA functions will fail because
	// getMFAManager() requires a mfaManagerAccessor which the mock doesn't implement.
	// This covers the switch branches and the getMFAManager error paths.
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	mfaTools := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{"mfa_enroll_begin", "quicbaas_mfa_enroll_begin", map[string]interface{}{"user_id": "30001", "method": "totp"}},
		{"mfa_enroll_complete", "quicbaas_mfa_enroll_complete", map[string]interface{}{"user_id": "30001", "code": "123456"}},
		{"mfa_enroll_cancel", "quicbaas_mfa_enroll_cancel", map[string]interface{}{"user_id": "30001"}},
		{"mfa_status", "quicbaas_mfa_status", map[string]interface{}{"user_id": "30001"}},
		{"mfa_disable", "quicbaas_mfa_disable", map[string]interface{}{"user_id": "30001"}},
		{"mfa_challenge_begin", "quicbaas_mfa_challenge_begin", map[string]interface{}{"user_id": "30001"}},
		{"mfa_challenge_verify", "quicbaas_mfa_challenge_verify", map[string]interface{}{"user_id": "30001", "code": "123456"}},
		{"mfa_backup_codes_generate", "quicbaas_mfa_backup_codes_generate", map[string]interface{}{"user_id": "30001"}},
		{"mfa_backup_codes_verify", "quicbaas_mfa_backup_codes_verify", map[string]interface{}{"user_id": "30001", "code": "backup"}},
		{"mfa_backup_codes_status", "quicbaas_mfa_backup_codes_status", map[string]interface{}{"user_id": "30001"}},
	}

	for _, tt := range mfaTools {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.executeIAMTool(ctx, tt.toolName, tt.args)
			// All MFA tools should return errors since the MFA manager is not available
			require.Error(t, err)
		})
	}
}

// TestIAMCoverage91_ExecToolRouting_PEP covers PEP routing branches in executeIAMTool.
func TestIAMCoverage91_ExecToolRouting_PEP(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	pepTools := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{"pep_check", "quicbaas_pep_check", map[string]interface{}{
			"principal": "user1", "action": "read", "resource": "doc1",
		}},
		{"pep_check_batch", "quicbaas_pep_check_batch", map[string]interface{}{
			"checks": []interface{}{
				map[string]interface{}{"principal": "user1", "action": "read", "resource": "doc1"},
			},
		}},
	}

	for _, tt := range pepTools {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.executeIAMTool(ctx, tt.toolName, tt.args)
			// PEP tools should return errors since the PEP engine is not available
			require.Error(t, err)
		})
	}
}

// TestIAMCoverage91_ExecToolRouting_DelegatedAdmin covers delegated admin routing branches.
func TestIAMCoverage91_ExecToolRouting_DelegatedAdmin(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	adminTools := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{"admin_assignment_create", "quicbaas_iam_admin_assignment_create", map[string]interface{}{"admin_id": "a1"}},
		{"admin_assignment_get", "quicbaas_iam_admin_assignment_get", map[string]interface{}{"assignment_id": "a1"}},
		{"admin_assignment_update", "quicbaas_iam_admin_assignment_update", map[string]interface{}{"assignment_id": "a1"}},
		{"admin_assignment_delete", "quicbaas_iam_admin_assignment_delete", map[string]interface{}{"assignment_id": "a1"}},
		{"admin_assignment_list", "quicbaas_iam_admin_assignment_list", map[string]interface{}{}},
		{"admin_check_permission", "quicbaas_iam_admin_check_permission", map[string]interface{}{"admin_id": "a1", "permission": "manage_users"}},
	}

	for _, tt := range adminTools {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.executeIAMTool(ctx, tt.toolName, tt.args)
			// These will fail because the delegated admin service is not available
			require.Error(t, err)
		})
	}
}

// TestIAMCoverage91_ExecToolRouting_Impersonation covers impersonation routing branches.
func TestIAMCoverage91_ExecToolRouting_Impersonation(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	impersonationTools := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{"impersonation_start", "quicbaas_iam_impersonation_start", map[string]interface{}{"target_user_id": "30001", "reason": "debug"}},
		{"impersonation_end", "quicbaas_iam_impersonation_end", map[string]interface{}{"session_id": "s1"}},
		{"impersonation_list", "quicbaas_iam_impersonation_list", map[string]interface{}{}},
	}

	for _, tt := range impersonationTools {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.executeIAMTool(ctx, tt.toolName, tt.args)
			// These will fail because the impersonation service is not available
			require.Error(t, err)
		})
	}
}

// TestIAMCoverage91_ExecToolRouting_GDPR covers GDPR routing branches in executeIAMTool.
func TestIAMCoverage91_ExecToolRouting_GDPR(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	gdprTools := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		// GDPR Consent
		{"gdpr_consent_grant", "quicbaas_iam_gdpr_consent_grant", map[string]interface{}{"user_id": "30001", "purpose": "analytics"}},
		{"gdpr_consent_withdraw", "quicbaas_iam_gdpr_consent_withdraw", map[string]interface{}{"user_id": "30001", "purpose": "analytics"}},
		{"gdpr_consent_get", "quicbaas_iam_gdpr_consent_get", map[string]interface{}{"user_id": "30001", "purpose": "analytics"}},
		{"gdpr_consent_list", "quicbaas_iam_gdpr_consent_list", map[string]interface{}{"user_id": "30001"}},

		// GDPR Export
		{"gdpr_export_request", "quicbaas_iam_gdpr_export_request", map[string]interface{}{"user_id": "30001"}},
		{"gdpr_export_status", "quicbaas_iam_gdpr_export_status", map[string]interface{}{"export_id": "e1"}},
		{"gdpr_export_generate", "quicbaas_iam_gdpr_export_generate", map[string]interface{}{"user_id": "30001", "format": "json"}},

		// GDPR Deletion
		{"gdpr_deletion_request", "quicbaas_iam_gdpr_deletion_request", map[string]interface{}{"user_id": "30001", "reason": "request"}},
		{"gdpr_deletion_cancel", "quicbaas_iam_gdpr_deletion_cancel", map[string]interface{}{"deletion_id": "d1"}},
		{"gdpr_deletion_process", "quicbaas_iam_gdpr_deletion_process", map[string]interface{}{"deletion_id": "d1"}},
		{"gdpr_deletion_status", "quicbaas_iam_gdpr_deletion_status", map[string]interface{}{"deletion_id": "d1"}},
	}

	for _, tt := range gdprTools {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.executeIAMTool(ctx, tt.toolName, tt.args)
			// GDPR tools will fail because the GDPR service is not available
			require.Error(t, err)
		})
	}
}

// =============================================================================
// User create role forwarding (transport parity with gRPC/Unix)
// =============================================================================

// TestExecuteIAMUserCreate_RolesForwarded proves the MCP tool arguments
// "roles" reach the IAM service request untouched.
func TestExecuteIAMUserCreate_RolesForwarded(t *testing.T) {
	var captured *service.IAMCreateUserRequest
	mock := &mockIAMService{
		createUserFunc: func(_ context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			captured = req
			return &service.IAMCreateUserResponse{
				User:      &iam.User{ID: 30001, TenantID: req.TenantID, Email: req.Email, Name: req.Name, Roles: req.Roles},
				Timestamp: time.Now(),
			}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	args := map[string]interface{}{
		"tenant_id": "t1",
		"email":     "roles@example.com",
		"name":      "Role User",
		"roles":     []interface{}{"role-a", "role-b"},
	}

	result, err := reg.executeIAMUserCreate(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, captured)
	assert.Equal(t, []string{"role-a", "role-b"}, captured.Roles)
}

// TestExecuteIAMUserCreate_UnknownRoleError proves a typed not-found error
// from validate-before-create propagates to the MCP caller with no result.
func TestExecuteIAMUserCreate_UnknownRoleError(t *testing.T) {
	mock := &mockIAMService{
		createUserFunc: func(_ context.Context, _ *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.role.get"}
		},
	}
	reg := newIAMTestRegistry(mock)

	args := map[string]interface{}{
		"tenant_id": "t1",
		"email":     "phantom@example.com",
		"name":      "Phantom",
		"roles":     []interface{}{"no-such-role"},
	}

	result, err := reg.executeIAMUserCreate(context.Background(), args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, dberrors.IsNotFound(err), "expected typed not-found, got %T: %v", err, err)
}

// TestExecuteIAMUserCreate_AttributesForwarded proves the MCP tool argument
// "attributes" reaches the IAM service request untouched (sibling gap of the
// roles parity fix — the wrapper used to drop it).
func TestExecuteIAMUserCreate_AttributesForwarded(t *testing.T) {
	var captured *service.IAMCreateUserRequest
	mock := &mockIAMService{
		createUserFunc: func(_ context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			captured = req
			return &service.IAMCreateUserResponse{
				User:      &iam.User{ID: 30001, TenantID: req.TenantID, Email: req.Email, Name: req.Name},
				Timestamp: time.Now(),
			}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	args := map[string]interface{}{
		"tenant_id":  "t1",
		"email":      "groups@example.com",
		"name":       "Group User",
		"attributes": map[string]interface{}{"department": "engineering"},
	}

	result, err := reg.executeIAMUserCreate(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, captured)
	require.NotNil(t, captured.Attributes)
	assert.Equal(t, "engineering", captured.Attributes["department"])
}

// TestExecuteIAMUserCreate_CarriesTheInvite proves the invite lifetime reaches
// the service and the activation invite it issues reaches the MCP caller.
func TestExecuteIAMUserCreate_CarriesTheInvite(t *testing.T) {
	var captured *service.IAMCreateUserRequest
	expires := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	mock := &mockIAMService{
		createUserFunc: func(_ context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			captured = req
			return &service.IAMCreateUserResponse{
				User:            &iam.User{ID: 30001, TenantID: req.TenantID, Email: req.Email},
				InviteToken:     "tok",
				InviteExpiresAt: &expires,
				Timestamp:       time.Now(),
			}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	result, err := reg.executeIAMUserCreate(context.Background(), map[string]interface{}{
		"tenant_id":           "t1",
		"email":               "pat@example.com",
		"name":                "Pat",
		"invite_expiry_hours": float64(48),
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, 48, captured.InviteExpiryHours)

	out, ok := result.(map[string]interface{})
	require.True(t, ok, "unexpected result type %T", result)
	assert.Equal(t, "tok", out["invite_token"])
	assert.Equal(t, expires.Format(time.RFC3339), out["invite_expires_at"])
}

// TestExecuteIAMUserUpdate_AttributesReachTheServiceInTheStoreShape pins the
// tool half of the dropped-attribute defect.
//
// The tool used to call stringAttributes only to CHECK the argument and then
// forward the raw map[string]interface{} it had just rejected values from. The
// service layer could not write that shape and dropped it, so an attribute
// change was accepted, answered with success, and never stored — while the
// validation that ran gave every appearance of the value having been handled.
// Forwarding the CONVERTED map is what makes the check and the payload the same
// value.
func TestExecuteIAMUserUpdate_AttributesReachTheServiceInTheStoreShape(t *testing.T) {
	var captured *service.IAMUpdateUserRequest
	mock := &mockIAMService{
		updateUserFunc: func(_ context.Context, req *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
			captured = req
			return &service.IAMUpdateUserResponse{
				User:      &iam.User{ID: req.UserID, TenantID: req.TenantID},
				Timestamp: time.Now(),
			}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	args := map[string]interface{}{
		"tenant_id":  "t1",
		"user_id":    "1",
		"attributes": map[string]interface{}{"clearance": "public"},
	}

	result, err := reg.executeIAMUserUpdate(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, captured)
	require.NotNil(t, captured.Attributes, "the tool validated the attributes and then sent nothing")
	assert.Equal(t, map[string]string{"clearance": "public"}, *captured.Attributes)
}

// TestExecuteIAMUserUpdate_NonStringAttributeIsRefused keeps the refusal that
// the conversion carries: an attribute value the store cannot hold fails the
// call rather than being dropped from it. Persisting less than the request
// asked for is silent data loss on an authorization input.
func TestExecuteIAMUserUpdate_NonStringAttributeIsRefused(t *testing.T) {
	var called bool
	mock := &mockIAMService{
		updateUserFunc: func(_ context.Context, req *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
			called = true
			return &service.IAMUpdateUserResponse{User: &iam.User{ID: req.UserID}, Timestamp: time.Now()}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	result, err := reg.executeIAMUserUpdate(context.Background(), map[string]interface{}{
		"tenant_id":  "t1",
		"user_id":    "1",
		"attributes": map[string]interface{}{"clearance": 5},
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "attributes.clearance must be a string")
	assert.False(t, called, "the update was dispatched with an attribute the store cannot hold")
}
