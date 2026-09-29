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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestMapIAMToolName(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		method   string
		expected string
		found    bool
	}{
		// Admin Assignment Operations
		{
			name:     "Admin.CreateAssignment",
			service:  "quicbaas.IAM.Admin",
			method:   "CreateAssignment",
			expected: "quicbaas_iam_admin_assignment_create",
			found:    true,
		},
		{
			name:     "Admin.GetAssignment",
			service:  "quicbaas.IAM.Admin",
			method:   "GetAssignment",
			expected: "quicbaas_iam_admin_assignment_get",
			found:    true,
		},
		{
			name:     "Admin.UpdateAssignment",
			service:  "quicbaas.IAM.Admin",
			method:   "UpdateAssignment",
			expected: "quicbaas_iam_admin_assignment_update",
			found:    true,
		},
		{
			name:     "Admin.DeleteAssignment",
			service:  "quicbaas.IAM.Admin",
			method:   "DeleteAssignment",
			expected: "quicbaas_iam_admin_assignment_delete",
			found:    true,
		},
		{
			name:     "Admin.ListAssignments",
			service:  "quicbaas.IAM.Admin",
			method:   "ListAssignments",
			expected: "quicbaas_iam_admin_assignment_list",
			found:    true,
		},
		{
			name:     "Admin.CheckPermission",
			service:  "quicbaas.IAM.Admin",
			method:   "CheckPermission",
			expected: "quicbaas_iam_admin_check_permission",
			found:    true,
		},
		{
			name:     "Admin.StartImpersonation",
			service:  "quicbaas.IAM.Admin",
			method:   "StartImpersonation",
			expected: "quicbaas_iam_impersonation_start",
			found:    true,
		},
		{
			name:     "Admin.EndImpersonation",
			service:  "quicbaas.IAM.Admin",
			method:   "EndImpersonation",
			expected: "quicbaas_iam_impersonation_end",
			found:    true,
		},
		{
			name:     "Admin.ListActiveImpersonations",
			service:  "quicbaas.IAM.Admin",
			method:   "ListActiveImpersonations",
			expected: "quicbaas_iam_impersonation_list",
			found:    true,
		},
		// MFA Operations
		{
			name:     "MFA.GetMFAStatus",
			service:  "quicbaas.IAM.MFA",
			method:   "GetMFAStatus",
			expected: "quicbaas_mfa_status",
			found:    true,
		},
		{
			name:     "MFA.BeginMFAEnrollment",
			service:  "quicbaas.IAM.MFA",
			method:   "BeginMFAEnrollment",
			expected: "quicbaas_mfa_enroll_begin",
			found:    true,
		},
		{
			name:     "MFA.GenerateBackupCodes",
			service:  "quicbaas.IAM.MFA",
			method:   "GenerateBackupCodes",
			expected: "quicbaas_mfa_backup_codes_generate",
			found:    true,
		},
		{
			name:     "MFA.GetBackupCodeStatus",
			service:  "quicbaas.IAM.MFA",
			method:   "GetBackupCodeStatus",
			expected: "quicbaas_mfa_backup_codes_status",
			found:    true,
		},
		// PEP Operations
		{
			name:     "PEP.Check",
			service:  "quicbaas.IAM.PEP",
			method:   "Check",
			expected: "quicbaas_pep_check",
			found:    true,
		},
		{
			name:     "PEP.CheckBatch",
			service:  "quicbaas.IAM.PEP",
			method:   "CheckBatch",
			expected: "quicbaas_pep_check_batch",
			found:    true,
		},
		// GDPR Operations
		{
			name:     "GDPR.GrantConsent",
			service:  "quicbaas.IAM.GDPR",
			method:   "GrantConsent",
			expected: "quicbaas_iam_gdpr_consent_grant",
			found:    true,
		},
		{
			name:     "GDPR.RequestExport",
			service:  "quicbaas.IAM.GDPR",
			method:   "RequestExport",
			expected: "quicbaas_iam_gdpr_export_request",
			found:    true,
		},
		{
			name:     "GDPR.RequestDeletion",
			service:  "quicbaas.IAM.GDPR",
			method:   "RequestDeletion",
			expected: "quicbaas_iam_gdpr_deletion_request",
			found:    true,
		},
		// App Operations
		{
			name:     "App.Register",
			service:  "quicbaas.IAM.App",
			method:   "Register",
			expected: "quicbaas_iam_app_register",
			found:    true,
		},
		{
			name:     "App.RotateSecret",
			service:  "quicbaas.IAM.App",
			method:   "RotateSecret",
			expected: "quicbaas_iam_app_rotate_secret",
			found:    true,
		},
		// Unknown method
		{
			name:     "UnknownMethod",
			service:  "quicbaas.IAM.Admin",
			method:   "UnknownMethod",
			expected: "",
			found:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolName, found := mapIAMToolName(tt.service, tt.method)
			assert.Equal(t, tt.found, found)
			if found {
				assert.Equal(t, tt.expected, toolName)
			}
		})
	}
}

func TestFlattenIAMArguments(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "flat arguments pass through unchanged",
			input: map[string]interface{}{
				"tenant_id": "test-tenant",
				"user_id":   "test-user",
			},
			expected: map[string]interface{}{
				"tenant_id": "test-tenant",
				"user_id":   "test-user",
			},
		},
		{
			name: "nested request object is flattened",
			input: map[string]interface{}{
				"tenant_id": "test-tenant",
				"request": map[string]interface{}{
					"name":        "test-role",
					"description": "test description",
				},
			},
			expected: map[string]interface{}{
				"tenant_id":   "test-tenant",
				"name":        "test-role",
				"description": "test description",
			},
		},
		{
			name: "nested app object is flattened",
			input: map[string]interface{}{
				"tenant_id": "test-tenant",
				"app": map[string]interface{}{
					"name":          "my-app",
					"redirect_uris": []string{"http://localhost:8080/callback"},
				},
			},
			expected: map[string]interface{}{
				"tenant_id":     "test-tenant",
				"name":          "my-app",
				"redirect_uris": []string{"http://localhost:8080/callback"},
			},
		},
		{
			name: "top-level keys take precedence over nested keys",
			input: map[string]interface{}{
				"tenant_id": "top-level-tenant",
				"request": map[string]interface{}{
					"tenant_id": "nested-tenant",
					"name":      "test-name",
				},
			},
			expected: map[string]interface{}{
				"tenant_id": "top-level-tenant",
				"name":      "test-name",
			},
		},
		{
			name: "nested update object is flattened",
			input: map[string]interface{}{
				"assignment_id": "adm_123",
				"update": map[string]interface{}{
					"permissions": []string{"user:read", "user:write"},
				},
			},
			expected: map[string]interface{}{
				"assignment_id": "adm_123",
				"permissions":   []string{"user:read", "user:write"},
			},
		},
		{
			name:     "nil input returns empty map",
			input:    map[string]interface{}{},
			expected: map[string]interface{}{},
		},
		{
			name: "non-map nested value is excluded",
			input: map[string]interface{}{
				"tenant_id": "test",
				"request":   "not-a-map",
			},
			expected: map[string]interface{}{
				"tenant_id": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := flattenIAMArguments(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransformIAMToolArguments_TenantRenaming(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name:     "tenant_get renames tenant_id to id",
			toolName: "quicbaas_iam_tenant_get",
			input: map[string]interface{}{
				"tenant_id": "test-tenant",
			},
			expected: map[string]interface{}{
				"id": "test-tenant",
			},
		},
		{
			name:     "tenant_update renames tenant_id to id",
			toolName: "quicbaas_iam_tenant_update",
			input: map[string]interface{}{
				"tenant_id": "test-tenant",
				"name":      "new-name",
			},
			expected: map[string]interface{}{
				"id":   "test-tenant",
				"name": "new-name",
			},
		},
		{
			name:     "tenant_delete renames tenant_id to id",
			toolName: "quicbaas_iam_tenant_delete",
			input: map[string]interface{}{
				"tenant_id": "test-tenant",
			},
			expected: map[string]interface{}{
				"id": "test-tenant",
			},
		},
		{
			name:     "tenant_get does not rename if id already exists",
			toolName: "quicbaas_iam_tenant_get",
			input: map[string]interface{}{
				"id":        "explicit-id",
				"tenant_id": "test-tenant",
			},
			expected: map[string]interface{}{
				"id":        "explicit-id",
				"tenant_id": "test-tenant",
			},
		},
		{
			name:     "user_get does not rename tenant_id",
			toolName: "quicbaas_iam_user_get",
			input: map[string]interface{}{
				"tenant_id": "test-tenant",
				"user_id":   "test-user",
			},
			expected: map[string]interface{}{
				"tenant_id": "test-tenant",
				"user_id":   "test-user",
			},
		},
		{
			name:     "admin_assignment_create copies role_id to admin_role",
			toolName: "quicbaas_iam_admin_assignment_create",
			input: map[string]interface{}{
				"user_id":    "test-user",
				"role_id":    "tenant_admin",
				"granted_by": "admin",
			},
			expected: map[string]interface{}{
				"user_id":    "test-user",
				"role_id":    "tenant_admin",
				"admin_role": "tenant_admin",
				"granted_by": "admin",
			},
		},
		{
			name:     "admin_assignment_create does not overwrite existing admin_role",
			toolName: "quicbaas_iam_admin_assignment_create",
			input: map[string]interface{}{
				"user_id":    "test-user",
				"role_id":    "user_admin",
				"admin_role": "super_admin",
				"granted_by": "admin",
			},
			expected: map[string]interface{}{
				"user_id":    "test-user",
				"role_id":    "user_admin",
				"admin_role": "super_admin",
				"granted_by": "admin",
			},
		},
		{
			name:     "nil args returns nil",
			toolName: "quicbaas_iam_tenant_get",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformIAMToolArguments(tt.toolName, tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsMCPIAMService(t *testing.T) {
	tests := []struct {
		service  string
		expected bool
	}{
		{"quicbaas.IAM.Admin", true},
		{"quicbaas.IAM.Tenant", true},
		{"quicbaas.IAM.User", true},
		{"quicbaas.IAM.MFA", true},
		{"quicbaas.IAM.PEP", true},
		{"quicbaas.IAM.GDPR", true},
		{"quicbaas.KV", false},
		{"quicbaas.Cluster", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			assert.Equal(t, tt.expected, isMCPIAMService(tt.service))
		})
	}
}

func TestIAMHandler_TransformRequest(t *testing.T) {
	transport, err := New(&transport.Config{Address: "http://localhost:8888"})
	require.NoError(t, err)
	handler := newIAMHandler(transport)

	t.Run("flattens nested request for role create", func(t *testing.T) {
		req := map[string]interface{}{
			"tenant_id": "test-tenant",
			"request": map[string]interface{}{
				"name":        "test-role",
				"description": "a test role",
			},
		}
		args, err := handler.transformRequest("quicbaas.IAM.Role", "Create", req)
		require.NoError(t, err)
		assert.Equal(t, "test-tenant", args["tenant_id"])
		assert.Equal(t, "test-role", args["name"])
		assert.Equal(t, "a test role", args["description"])
		assert.Nil(t, args["request"])
	})

	t.Run("renames tenant_id to id for tenant get", func(t *testing.T) {
		req := map[string]interface{}{
			"tenant_id": "test-tenant-id",
		}
		args, err := handler.transformRequest("quicbaas.IAM.Tenant", "Get", req)
		require.NoError(t, err)
		assert.Equal(t, "test-tenant-id", args["id"])
		_, hasTenantID := args["tenant_id"]
		assert.False(t, hasTenantID, "tenant_id should be renamed to id")
	})

	t.Run("nil request returns nil args", func(t *testing.T) {
		args, err := handler.transformRequest("quicbaas.IAM.Tenant", "Get", nil)
		require.NoError(t, err)
		assert.Nil(t, args)
	})

	t.Run("flattens nested app for app register", func(t *testing.T) {
		req := map[string]interface{}{
			"tenant_id": "test-tenant",
			"app": map[string]interface{}{
				"name": "my-app",
			},
		}
		args, err := handler.transformRequest("quicbaas.IAM.App", "Register", req)
		require.NoError(t, err)
		assert.Equal(t, "test-tenant", args["tenant_id"])
		assert.Equal(t, "my-app", args["name"])
		assert.Nil(t, args["app"])
	})
}

func TestIAMHandler_TransformResponse(t *testing.T) {
	transport, err := New(&transport.Config{Address: "http://localhost:8888"})
	require.NoError(t, err)
	handler := newIAMHandler(transport)

	t.Run("passes response through unchanged", func(t *testing.T) {
		resp := map[string]interface{}{
			"id":   "test-id",
			"name": "test-name",
		}
		result, err := handler.transformResponse("quicbaas.IAM.Tenant", "Create", resp)
		require.NoError(t, err)
		assert.Equal(t, resp, result)
	})

	t.Run("nil response passes through", func(t *testing.T) {
		result, err := handler.transformResponse("quicbaas.IAM.Tenant", "Get", nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestIAMToolRequiresSpecialHandling(t *testing.T) {
	err := iamToolRequiresSpecialHandling("quicbaas.IAM.Admin", "UnknownMethod")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown IAM method")
}
