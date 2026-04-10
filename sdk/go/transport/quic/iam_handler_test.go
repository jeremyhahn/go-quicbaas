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

package quic

import (
	"testing"
)

func TestIsIAMService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		// Long format (quicbaas.IAM.*)
		{"IAM Tenant service (long)", "quicbaas.IAM.Tenant", true},
		{"IAM User service (long)", "quicbaas.IAM.User", true},
		{"IAM Group service (long)", "quicbaas.IAM.Group", true},
		{"IAM Role service (long)", "quicbaas.IAM.Role", true},
		{"IAM Policy service (long)", "quicbaas.IAM.Policy", true},
		// Short format (IAM.*)
		{"IAM Tenant service (short)", "IAM.Tenant", true},
		{"IAM User service (short)", "IAM.User", true},
		{"IAM Group service (short)", "IAM.Group", true},
		{"IAM Role service (short)", "IAM.Role", true},
		{"IAM Policy service (short)", "IAM.Policy", true},
		// Non-IAM services
		{"KV service", "quicbaas.KV", false},
		{"Vector service", "quicbaas.Vector", false},
		{"Empty service", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isIAMService(tt.service)
			if result != tt.expected {
				t.Errorf("isIAMService(%q) = %v, expected %v", tt.service, result, tt.expected)
			}
		})
	}
}

func TestExtractTenant(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		checkID  string
		checkKey string
	}{
		{
			name: "nested tenant structure",
			input: map[string]interface{}{
				"tenant": map[string]interface{}{
					"id":   "tenant-123",
					"name": "Test Tenant",
				},
				"timestamp": "2025-01-24T12:00:00Z",
			},
			checkID:  "tenant-123",
			checkKey: "id",
		},
		{
			name: "flat tenant structure",
			input: map[string]interface{}{
				"id":   "tenant-456",
				"name": "Direct Tenant",
			},
			checkID:  "tenant-456",
			checkKey: "id",
		},
		{
			name:     "nil input",
			input:    nil,
			checkID:  "",
			checkKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTenant(tt.input)
			if tt.input == nil {
				if result != nil {
					t.Errorf("extractTenant(nil) should return nil")
				}
				return
			}
			if result == nil {
				t.Errorf("extractTenant returned nil for non-nil input")
				return
			}
			if tt.checkKey != "" {
				if id, ok := result[tt.checkKey].(string); !ok || id != tt.checkID {
					t.Errorf("extractTenant()[%q] = %v, expected %q", tt.checkKey, result[tt.checkKey], tt.checkID)
				}
			}
		})
	}
}

func TestExtractUser(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		checkID  string
		checkKey string
	}{
		{
			name: "nested user structure",
			input: map[string]interface{}{
				"user": map[string]interface{}{
					"id":    "user-123",
					"email": "test@example.com",
					"name":  "Test User",
				},
				"timestamp": "2025-01-24T12:00:00Z",
			},
			checkID:  "user-123",
			checkKey: "id",
		},
		{
			name: "flat user structure",
			input: map[string]interface{}{
				"id":    "user-456",
				"email": "direct@example.com",
			},
			checkID:  "user-456",
			checkKey: "id",
		},
		{
			name:     "nil input",
			input:    nil,
			checkID:  "",
			checkKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractUser(tt.input)
			if tt.input == nil {
				if result != nil {
					t.Errorf("extractUser(nil) should return nil")
				}
				return
			}
			if result == nil {
				t.Errorf("extractUser returned nil for non-nil input")
				return
			}
			if tt.checkKey != "" {
				if id, ok := result[tt.checkKey].(string); !ok || id != tt.checkID {
					t.Errorf("extractUser()[%q] = %v, expected %q", tt.checkKey, result[tt.checkKey], tt.checkID)
				}
			}
		})
	}
}

func TestExtractGroup(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		checkID  string
		checkKey string
	}{
		{
			name: "nested group structure",
			input: map[string]interface{}{
				"group": map[string]interface{}{
					"id":   "group-123",
					"name": "Test Group",
				},
				"timestamp": "2025-01-24T12:00:00Z",
			},
			checkID:  "group-123",
			checkKey: "id",
		},
		{
			name: "flat group structure",
			input: map[string]interface{}{
				"id":   "group-456",
				"name": "Direct Group",
			},
			checkID:  "group-456",
			checkKey: "id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGroup(tt.input)
			if result == nil {
				t.Errorf("extractGroup returned nil for non-nil input")
				return
			}
			if tt.checkKey != "" {
				if id, ok := result[tt.checkKey].(string); !ok || id != tt.checkID {
					t.Errorf("extractGroup()[%q] = %v, expected %q", tt.checkKey, result[tt.checkKey], tt.checkID)
				}
			}
		})
	}
}

func TestExtractRole(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		checkID  string
		checkKey string
	}{
		{
			name: "nested role structure",
			input: map[string]interface{}{
				"role": map[string]interface{}{
					"id":   "role-123",
					"name": "Test Role",
				},
				"timestamp": "2025-01-24T12:00:00Z",
			},
			checkID:  "role-123",
			checkKey: "id",
		},
		{
			name: "flat role structure",
			input: map[string]interface{}{
				"id":   "role-456",
				"name": "Direct Role",
			},
			checkID:  "role-456",
			checkKey: "id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRole(tt.input)
			if result == nil {
				t.Errorf("extractRole returned nil for non-nil input")
				return
			}
			if tt.checkKey != "" {
				if id, ok := result[tt.checkKey].(string); !ok || id != tt.checkID {
					t.Errorf("extractRole()[%q] = %v, expected %q", tt.checkKey, result[tt.checkKey], tt.checkID)
				}
			}
		})
	}
}

func TestTransformTenantList(t *testing.T) {
	input := map[string]interface{}{
		"tenants": []interface{}{
			map[string]interface{}{
				"id":   "tenant-1",
				"name": "Tenant One",
			},
			map[string]interface{}{
				"id":   "tenant-2",
				"name": "Tenant Two",
			},
		},
		"total_count": float64(2),
		"timestamp":   "2025-01-24T12:00:00Z",
	}

	result := transformTenantList(input)

	tenants, ok := result["tenants"].([]interface{})
	if !ok {
		t.Fatalf("tenants should be []interface{}")
	}
	if len(tenants) != 2 {
		t.Errorf("expected 2 tenants, got %d", len(tenants))
	}

	pagination, ok := result["pagination"].(map[string]interface{})
	if !ok {
		t.Fatalf("pagination should be map[string]interface{}")
	}
	if pagination["total_count"] != 2 {
		t.Errorf("pagination.total_count = %v, expected 2", pagination["total_count"])
	}
}

func TestTransformUserList(t *testing.T) {
	input := map[string]interface{}{
		"users": []interface{}{
			map[string]interface{}{
				"id":    "user-1",
				"email": "user1@example.com",
			},
			map[string]interface{}{
				"id":    "user-2",
				"email": "user2@example.com",
			},
		},
		"total_count": int64(2),
	}

	result := transformUserList(input)

	users, ok := result["users"].([]interface{})
	if !ok {
		t.Fatalf("users should be []interface{}")
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestIAMHandlerTransformResponse(t *testing.T) {
	handler := newIAMHandler(nil)

	tests := []struct {
		name     string
		service  string
		method   string
		input    map[string]interface{}
		checkKey string
		checkVal interface{}
	}{
		{
			name:    "Tenant Create (long format)",
			service: "quicbaas.IAM.Tenant",
			method:  "Create",
			input: map[string]interface{}{
				"tenant": map[string]interface{}{
					"id":   "test-tenant",
					"name": "Test",
				},
			},
			checkKey: "id",
			checkVal: "test-tenant",
		},
		{
			name:    "Tenant Create (short format)",
			service: "IAM.Tenant",
			method:  "Create",
			input: map[string]interface{}{
				"tenant": map[string]interface{}{
					"id":   "test-tenant-short",
					"name": "Test Short",
				},
			},
			checkKey: "id",
			checkVal: "test-tenant-short",
		},
		{
			name:    "Tenant Delete",
			service: "quicbaas.IAM.Tenant",
			method:  "Delete",
			input: map[string]interface{}{
				"deleted":   true,
				"timestamp": "2025-01-24T12:00:00Z",
			},
			checkKey: "success",
			checkVal: true,
		},
		{
			name:    "User Create",
			service: "quicbaas.IAM.User",
			method:  "Create",
			input: map[string]interface{}{
				"user": map[string]interface{}{
					"id":    "test-user",
					"email": "test@example.com",
				},
			},
			checkKey: "id",
			checkVal: "test-user",
		},
		{
			name:    "User Create (short format)",
			service: "IAM.User",
			method:  "Create",
			input: map[string]interface{}{
				"user": map[string]interface{}{
					"id":    "test-user-short",
					"email": "test-short@example.com",
				},
			},
			checkKey: "id",
			checkVal: "test-user-short",
		},
		{
			name:    "Group Create",
			service: "quicbaas.IAM.Group",
			method:  "Create",
			input: map[string]interface{}{
				"group": map[string]interface{}{
					"id":   "test-group",
					"name": "Test Group",
				},
			},
			checkKey: "id",
			checkVal: "test-group",
		},
		{
			name:    "Role Create",
			service: "quicbaas.IAM.Role",
			method:  "Create",
			input: map[string]interface{}{
				"role": map[string]interface{}{
					"id":   "test-role",
					"name": "Test Role",
				},
			},
			checkKey: "id",
			checkVal: "test-role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handler.transformResponse(tt.service, tt.method, tt.input)
			if err != nil {
				t.Fatalf("transformResponse returned error: %v", err)
			}
			resultMap, ok := result.(map[string]interface{})
			if !ok {
				// Could be map[string]bool
				if boolMap, ok := result.(map[string]bool); ok {
					if boolMap[tt.checkKey] != tt.checkVal {
						t.Errorf("result[%q] = %v, expected %v", tt.checkKey, boolMap[tt.checkKey], tt.checkVal)
					}
					return
				}
				t.Fatalf("result is not a map: %T", result)
			}
			if resultMap[tt.checkKey] != tt.checkVal {
				t.Errorf("result[%q] = %v, expected %v", tt.checkKey, resultMap[tt.checkKey], tt.checkVal)
			}
		})
	}
}

func TestFlattenUser_FieldMappings(t *testing.T) {
	input := map[string]interface{}{
		"id":        "user-123",
		"tenant_id": "tenant-456",
		"email":     "test@example.com",
		"name":      "Display Name",
		"roles":     []string{"admin", "user"},
		"groups":    []interface{}{"group1", "group2"},
	}

	result := flattenUser(input)

	if result["id"] != "user-123" {
		t.Errorf("id = %v, expected user-123", result["id"])
	}
	if result["tenant_id"] != "tenant-456" {
		t.Errorf("tenant_id = %v, expected tenant-456", result["tenant_id"])
	}
	if result["email"] != "test@example.com" {
		t.Errorf("email = %v, expected test@example.com", result["email"])
	}
	// Server "name" should be mapped to SDK "display_name" and "username"
	if result["display_name"] != "Display Name" {
		t.Errorf("display_name = %v, expected Display Name", result["display_name"])
	}
	if result["username"] != "Display Name" {
		t.Errorf("username = %v, expected Display Name", result["username"])
	}
}

func TestFlattenTenant_FieldMappings(t *testing.T) {
	input := map[string]interface{}{
		"id":      "tenant-123",
		"name":    "Tenant Name",
		"created": "2025-01-24T12:00:00Z",
		"updated": "2025-01-24T13:00:00Z",
	}

	result := flattenTenant(input)

	if result["id"] != "tenant-123" {
		t.Errorf("id = %v, expected tenant-123", result["id"])
	}
	if result["name"] != "Tenant Name" {
		t.Errorf("name = %v, expected Tenant Name", result["name"])
	}
	// Server "created" should be mapped to SDK "created_at"
	if result["created_at"] != "2025-01-24T12:00:00Z" {
		t.Errorf("created_at = %v, expected 2025-01-24T12:00:00Z", result["created_at"])
	}
	// Server "updated" should be mapped to SDK "updated_at"
	if result["updated_at"] != "2025-01-24T13:00:00Z" {
		t.Errorf("updated_at = %v, expected 2025-01-24T13:00:00Z", result["updated_at"])
	}
}

func TestTransformTenantResponse_Delete(t *testing.T) {
	handler := newIAMHandler(nil)

	// Test deleted: true
	result, err := handler.transformTenantResponse("Delete", map[string]interface{}{
		"deleted":   true,
		"timestamp": "2025-01-24T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]bool)
	if !ok {
		t.Fatalf("expected map[string]bool, got %T", result)
	}
	if !resultMap["success"] {
		t.Errorf("success = %v, expected true", resultMap["success"])
	}

	// Test deleted: false
	result2, err := handler.transformTenantResponse("Delete", map[string]interface{}{
		"deleted": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap2, ok := result2.(map[string]bool)
	if !ok {
		t.Fatalf("expected map[string]bool, got %T", result2)
	}
	if resultMap2["success"] {
		t.Errorf("success = %v, expected false", resultMap2["success"])
	}
}

func TestExtractGroup_NilInput(t *testing.T) {
	result := extractGroup(nil)
	if result != nil {
		t.Errorf("extractGroup(nil) should return nil, got %v", result)
	}
}

func TestExtractRole_NilInput(t *testing.T) {
	result := extractRole(nil)
	if result != nil {
		t.Errorf("extractRole(nil) should return nil, got %v", result)
	}
}

func TestFlattenGroup_NilInput(t *testing.T) {
	result := flattenGroup(nil)
	if result != nil {
		t.Errorf("flattenGroup(nil) should return nil, got %v", result)
	}
}

func TestFlattenRole_NilInput(t *testing.T) {
	result := flattenRole(nil)
	if result != nil {
		t.Errorf("flattenRole(nil) should return nil, got %v", result)
	}
}

func TestFlattenTenant_NilInput(t *testing.T) {
	result := flattenTenant(nil)
	if result != nil {
		t.Errorf("flattenTenant(nil) should return nil, got %v", result)
	}
}

func TestFlattenUser_NilInput(t *testing.T) {
	result := flattenUser(nil)
	if result != nil {
		t.Errorf("flattenUser(nil) should return nil, got %v", result)
	}
}

func TestTransformGroupList(t *testing.T) {
	input := map[string]interface{}{
		"groups": []interface{}{
			map[string]interface{}{
				"id":   "group-1",
				"name": "Group One",
			},
		},
		"total_count": int(1),
	}

	result := transformGroupList(input)

	groups, ok := result["groups"].([]interface{})
	if !ok {
		t.Fatalf("groups should be []interface{}")
	}
	if len(groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(groups))
	}
}

func TestTransformRoleList(t *testing.T) {
	input := map[string]interface{}{
		"roles": []interface{}{
			map[string]interface{}{
				"id":   "role-1",
				"name": "Role One",
			},
		},
		"total_count": float64(1),
	}

	result := transformRoleList(input)

	roles, ok := result["roles"].([]interface{})
	if !ok {
		t.Fatalf("roles should be []interface{}")
	}
	if len(roles) != 1 {
		t.Errorf("expected 1 role, got %d", len(roles))
	}
}

func TestIAMHandler_UnknownServiceType(t *testing.T) {
	handler := newIAMHandler(nil)

	// Test unknown IAM service type (returns raw response)
	input := map[string]interface{}{
		"custom_field": "value",
	}
	result, err := handler.transformResponse("quicbaas.IAM.Custom", "Get", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}
	if resultMap["custom_field"] != "value" {
		t.Errorf("expected raw response to be returned unchanged")
	}
}

func TestTransformUserResponse_AssignRole(t *testing.T) {
	handler := newIAMHandler(nil)

	// Test AssignRole returns success
	result, err := handler.transformUserResponse("AssignRole", map[string]interface{}{
		"user": map[string]interface{}{
			"id": "user-123",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]bool)
	if !ok {
		t.Fatalf("expected map[string]bool, got %T", result)
	}
	if !resultMap["success"] {
		t.Errorf("success = %v, expected true", resultMap["success"])
	}
}

func TestTransformGroupResponse_AddMember(t *testing.T) {
	handler := newIAMHandler(nil)

	// Test AddMember returns success when group is present
	result, err := handler.transformGroupResponse("AddMember", map[string]interface{}{
		"group": map[string]interface{}{
			"id": "group-123",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]bool)
	if !ok {
		t.Fatalf("expected map[string]bool, got %T", result)
	}
	if !resultMap["success"] {
		t.Errorf("success = %v, expected true", resultMap["success"])
	}
}
