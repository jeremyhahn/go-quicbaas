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
	"encoding/json"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// iamHandler handles IAM request/response transformations for the QUIC transport.
// The SDK sends nested structures (e.g., {"tenant_id": "...", "request": {...}})
// but the server expects flat structures with fields directly at the top level.
// The server returns responses with nested structures (e.g., {"tenant": {...}, "timestamp": "..."})
// but the SDK IAM clients expect flat structures with fields directly at the top level.
type iamHandler struct {
	transport *Transport
}

// newIAMHandler creates a new IAM handler for the QUIC transport.
func newIAMHandler(t *Transport) *iamHandler {
	return &iamHandler{transport: t}
}

// isIAMService checks if the service is an IAM service.
// Supports both long format (quicbaas.IAM.Tenant) and short format (IAM.Tenant).
func isIAMService(service string) bool {
	return strings.HasPrefix(service, "quicbaas.IAM.") || strings.HasPrefix(service, "IAM.")
}

// structToMap converts a struct to a map[string]interface{} via JSON round-trip.
// This allows the transformation functions to work with both struct and map inputs.
func structToMap(v interface{}) (map[string]interface{}, error) {
	// If it's already a map, return it directly
	if m, ok := v.(map[string]interface{}); ok {
		return m, nil
	}

	// Convert struct to JSON and back to map
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// transformRequest transforms SDK IAM requests to server-expected format.
// The SDK sends nested structures, the server expects flat structures.
func (h *iamHandler) transformRequest(service, method string, req interface{}) (interface{}, error) {
	switch {
	case strings.Contains(service, "MFA"):
		return h.transformMFARequest(method, req)
	case strings.Contains(service, "Tenant"):
		return h.transformTenantRequest(method, req)
	case strings.Contains(service, "User"):
		return h.transformUserRequest(method, req)
	case strings.Contains(service, "Group"):
		return h.transformGroupRequest(method, req)
	case strings.Contains(service, "Role"):
		return h.transformRoleRequest(method, req)
	case strings.Contains(service, "Policy"):
		return h.transformPolicyRequest(method, req)
	case strings.Contains(service, "App"):
		return h.transformAppRequest(method, req)
	default:
		// Return raw request for other IAM services
		return req, nil
	}
}

// transformMFARequest wraps MFA requests in an IAMSubRequest envelope.
// The server expects: {"subop": <byte>, "payload": <json_bytes>}
// where subop identifies the MFA operation and payload contains the inner request.
func (h *iamHandler) transformMFARequest(method string, req interface{}) (interface{}, error) {
	subOp, ok := MFAMethodToSubOp(method)
	if !ok {
		// Unknown MFA method - pass through the request as-is wrapped in a
		// sub-request with subop 0xFF so the server returns a clear error.
		subOp = 0xFF
	}

	// Encode the inner request payload to JSON bytes
	var innerPayload json.RawMessage
	if req != nil {
		data, err := json.Marshal(req)
		if err != nil {
			return nil, err
		}
		innerPayload = data
	}

	// Return the IAMSubRequest envelope that will be serialized by EncodePayload
	return &IAMSubRequest{
		SubOp:   subOp,
		Payload: innerPayload,
	}, nil
}

// transformTenantRequest transforms tenant requests.
func (h *iamHandler) transformTenantRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "Create":
		// SDK sends: *iam.CreateTenantRequest
		// Server expects: {id, name, settings, quotas} flat
		if sdkReq, ok := req.(*iam.CreateTenantRequest); ok {
			return map[string]interface{}{
				"id":       sdkReq.ID,
				"name":     sdkReq.Name,
				"settings": sdkReq.Settings,
				"metadata": sdkReq.Metadata,
			}, nil
		}
		return req, nil
	case "Get":
		// SDK sends: {tenant_id: "..."}
		// Server expects: {id: "..."}
		if m, ok := req.(map[string]interface{}); ok {
			if tenantID, exists := m["tenant_id"]; exists {
				return map[string]interface{}{"id": tenantID}, nil
			}
		}
		// Handle getTenantRequest type
		return transformGetRequest(req, "tenant_id", "id")
	case "Update":
		// SDK sends: {tenant_id: "...", request: {...}}
		// Server expects: {id: "...", name: "...", ...} flat
		return transformUpdateRequest(req, "tenant_id", "id")
	case "Delete":
		// SDK sends: {tenant_id: "..."}
		// Server expects: {id: "..."}
		return transformGetRequest(req, "tenant_id", "id")
	case "List":
		// SDK sends: ListTenantsRequest directly
		// Server expects same format
		return req, nil
	default:
		return req, nil
	}
}

// transformUserRequest transforms user requests.
func (h *iamHandler) transformUserRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "Create":
		// SDK sends: {tenant_id: "...", request: {username, email, auth_methods, ...}}
		// Server expects: {tenant_id: "...", email, username, name, roles, groups, attributes} flat
		return transformUserCreateRequest(req)
	case "Get":
		// SDK sends: {tenant_id: "...", user_id: "..."}
		// Server expects: same format
		return req, nil
	case "GetByEmail":
		// SDK sends: {tenant_id: "...", email: "..."}
		// Server expects: same format
		return req, nil
	case "FindByUsername":
		// SDK sends: {tenant_id: "...", username: "..."}
		// Server expects: same format
		return req, nil
	case "Update":
		// SDK sends: {tenant_id: "...", user_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", user_id: "...", email, username, name, ...} flat
		return transformUserUpdateRequest(req)
	case "Delete":
		// SDK sends: {tenant_id: "...", user_id: "..."}
		// Server expects: same format
		return req, nil
	case "List":
		// SDK sends: {tenant_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", limit, offset, filter} flat
		return transformUserListRequest(req)
	case "AssignRole":
		// SDK sends: {tenant_id, user_id, role_id}
		// Server expects: same format
		return req, nil
	case "RevokeRole":
		// SDK sends: {tenant_id, user_id, role_id}
		// Server expects: same format
		return req, nil
	case "AddToGroup":
		// SDK sends: {tenant_id, user_id, group_id}
		// Server expects: {tenant_id, user_id, group_id} as AddUserToGroupRequest
		return req, nil
	case "RemoveFromGroup":
		// SDK sends: {tenant_id, user_id, group_id}
		// Server expects: {tenant_id, user_id, group_id} as RemoveUserFromGroupRequest
		return req, nil
	case "Suspend", "Unsuspend":
		// SDK sends: {tenant_id, user_id}
		// Server expects: same format
		return req, nil
	default:
		return req, nil
	}
}

// transformGroupRequest transforms group requests.
func (h *iamHandler) transformGroupRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "Create":
		// SDK sends: {tenant_id: "...", request: {name, description, ...}}
		// Server expects: {tenant_id: "...", name, description} flat
		return transformGroupCreateRequest(req)
	case "Get":
		// SDK sends: {tenant_id: "...", group_id: "..."}
		// Server expects: same format
		return req, nil
	case "Update":
		// SDK sends: {tenant_id: "...", group_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", group_id: "...", name, description} flat
		return transformGroupUpdateRequest(req)
	case "Delete":
		// SDK sends: {tenant_id: "...", group_id: "..."}
		// Server expects: same format
		return req, nil
	case "List":
		// SDK sends: {tenant_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", limit, offset} flat
		return transformGroupListRequest(req)
	case "AddMember":
		// SDK sends: {tenant_id, user_id, group_id}
		// Server expects: same format
		return req, nil
	case "RemoveMember":
		// SDK sends: {tenant_id, user_id, group_id}
		// Server expects: same format
		return req, nil
	default:
		return req, nil
	}
}

// transformRoleRequest transforms role requests.
func (h *iamHandler) transformRoleRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "Create":
		// SDK sends: {tenant_id: "...", request: {name, description, permissions}}
		// Server expects: {tenant_id: "...", name, description, permissions} flat
		return transformRoleCreateRequest(req)
	case "Get":
		// SDK sends: {tenant_id: "...", role_id: "..."}
		// Server expects: same format
		return req, nil
	case "Update":
		// SDK sends: {tenant_id: "...", role_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", role_id: "...", name, description, permissions} flat
		return transformRoleUpdateRequest(req)
	case "Delete":
		// SDK sends: {tenant_id: "...", role_id: "..."}
		// Server expects: same format
		return req, nil
	case "List":
		// SDK sends: {tenant_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", limit, offset} flat
		return transformRoleListRequest(req)
	default:
		return req, nil
	}
}

// transformPolicyRequest transforms policy requests.
func (h *iamHandler) transformPolicyRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "Create":
		// SDK sends: {tenant_id: "...", request: {name, description, rules, ...}}
		// Server expects: {tenant_id: "...", policy: {...}}
		return transformPolicyCreateRequest(req)
	case "Get":
		// SDK sends: {tenant_id: "...", policy_name: "..."}
		// Server expects: {tenant_id: "...", name: "..."}
		return transformGetRequest(req, "policy_name", "name")
	case "Update":
		// SDK sends: {tenant_id: "...", policy_name: "...", request: {...}}
		// Server expects: {tenant_id: "...", name: "...", policy: {...}}
		return transformPolicyUpdateRequest(req)
	case "Delete":
		// SDK sends: {tenant_id: "...", policy_name: "..."}
		// Server expects: {tenant_id: "...", name: "..."}
		return transformGetRequest(req, "policy_name", "name")
	case "List":
		// SDK sends: {tenant_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", limit, offset} flat
		return transformPolicyListRequest(req)
	case "Validate":
		// SDK sends: {tenant_id: "...", policy: {...}}
		// Server expects: same format
		return req, nil
	case "Test":
		// SDK sends: {tenant_id: "...", request: {...}}
		// Server expects: {tenant_id: "..., policy_name, test_cases}
		return transformPolicyTestRequest(req)
	default:
		return req, nil
	}
}

// transformAppRequest transforms application requests.
func (h *iamHandler) transformAppRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "Register":
		// SDK sends: {tenant_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", ...} flat
		return transformAppRegisterRequest(req)
	case "Get":
		// SDK sends: {tenant_id: "...", app_id: "..."}
		// Server expects: {tenant_id: "...", id: "..."}
		return transformGetRequest(req, "app_id", "id")
	case "Update":
		// SDK sends: {tenant_id: "...", app_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", id: "...", ...} flat
		return transformAppUpdateRequest(req)
	case "Delete":
		// SDK sends: {tenant_id: "...", app_id: "..."}
		// Server expects: {tenant_id: "...", id: "..."}
		return transformGetRequest(req, "app_id", "id")
	case "List":
		// SDK sends: {tenant_id: "...", request: {...}}
		// Server expects: {tenant_id: "...", limit, offset} flat
		return transformAppListRequest(req)
	case "RotateSecret":
		// SDK sends: {tenant_id: "...", app_id: "..."}
		// Server expects: {tenant_id: "...", id: "..."}
		return transformGetRequest(req, "app_id", "id")
	default:
		return req, nil
	}
}

// transformGetRequest transforms a Get/Delete request by renaming the ID field.
func transformGetRequest(req interface{}, fromField, toField string) (interface{}, error) {
	// Convert to map if it's a struct
	var m map[string]interface{}
	switch r := req.(type) {
	case map[string]interface{}:
		m = r
	default:
		// For struct types, serialize and re-parse as map
		converted, err := structToMap(req)
		if err != nil {
			return req, nil
		}
		m = converted
	}

	// Check if the fromField exists and needs renaming
	if val, exists := m[fromField]; exists {
		result := make(map[string]interface{})
		for k, v := range m {
			if k == fromField {
				result[toField] = v
			} else {
				result[k] = v
			}
		}
		// Ensure ID is set if value is valid
		if result[toField] != nil && result[toField] != "" {
			return result, nil
		}
		// If value was empty, try the original request in case field names differ
		if val != nil && val != "" {
			result[toField] = val
			return result, nil
		}
	}

	// If fromField doesn't exist, check if toField already exists
	if val, exists := m[toField]; exists && val != nil && val != "" {
		return m, nil
	}

	return m, nil
}

// transformUpdateRequest transforms an Update request by flattening nested request.
func transformUpdateRequest(req interface{}, idField, targetIDField string) (interface{}, error) {
	// Convert to map if it's a struct
	var m map[string]interface{}
	switch r := req.(type) {
	case map[string]interface{}:
		m = r
	default:
		// For struct types, serialize and re-parse as map
		converted, err := structToMap(req)
		if err != nil {
			return req, nil
		}
		m = converted
	}

	result := make(map[string]interface{})

	// Copy the ID field with potential rename
	if id, exists := m[idField]; exists {
		result[targetIDField] = id
	}

	// Flatten the nested request object
	if nestedReq, exists := m["request"].(map[string]interface{}); exists {
		for k, v := range nestedReq {
			result[k] = v
		}
	}

	// Copy other top-level fields that aren't "request"
	for k, v := range m {
		if k != idField && k != "request" {
			result[k] = v
		}
	}

	return result, nil
}

// getStringField extracts a string field from a map, checking both lowercase and uppercase keys.
// This handles the case where SDK structs without json tags marshal with uppercase field names.
func getStringField(m map[string]interface{}, lowercase, uppercase string) (string, bool) {
	if v, ok := m[lowercase].(string); ok {
		return v, true
	}
	if v, ok := m[uppercase].(string); ok {
		return v, true
	}
	return "", false
}

// getInterfaceField extracts an interface field from a map, checking both lowercase and uppercase keys.
func getInterfaceField(m map[string]interface{}, lowercase, uppercase string) (interface{}, bool) {
	if v, ok := m[lowercase]; ok {
		return v, true
	}
	if v, ok := m[uppercase]; ok {
		return v, true
	}
	return nil, false
}

// transformUserCreateRequest transforms user create request.
// Handles both nested format (SDK client) and flat format (integration tests).
func transformUserCreateRequest(req interface{}) (interface{}, error) {
	// Handle *iam.CreateUserRequest wrapped in createUserRequestInternal
	// The internal type is: {TenantID: "...", Request: *CreateUserRequest}
	// OR a flat map: {tenant_id: "...", email: "...", name: "...", username: "..."}

	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		// If conversion fails, return original request
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of user fields: nested request or top level
	// If we have a nested request, use it; otherwise use the top-level map
	fieldSource := nestedReq
	if fieldSource == nil {
		// No nested request - fields are at the top level (flat format)
		fieldSource = m
	}

	// Map SDK field names to server field names
	// Map username directly (SDK username -> server username)
	if username, ok := getStringField(fieldSource, "username", "Username"); ok {
		result["username"] = username
	}

	// Map display_name to name (SDK display_name -> server name)
	if displayName, ok := getStringField(fieldSource, "display_name", "DisplayName"); ok {
		result["name"] = displayName
	} else if name, ok := getStringField(fieldSource, "name", "Name"); ok {
		result["name"] = name
	}

	// Check for email with both lowercase and uppercase keys
	if email, ok := getStringField(fieldSource, "email", "Email"); ok {
		result["email"] = email
	}

	// Password field is intentionally not mapped; authentication is passwordless (FIDO2/PIV).

	// Check for roles with both lowercase and uppercase keys
	if roles, ok := getInterfaceField(fieldSource, "roles", "Roles"); ok {
		result["roles"] = roles
	}

	// Check for groups with both lowercase and uppercase keys
	if groups, ok := getInterfaceField(fieldSource, "groups", "Groups"); ok {
		result["groups"] = groups
	}

	// Check for metadata with both lowercase and uppercase keys
	if metadata, ok := getInterfaceField(fieldSource, "metadata", "Metadata"); ok {
		result["attributes"] = metadata
	}

	return result, nil
}

// transformUserUpdateRequest transforms user update request.
func transformUserUpdateRequest(req interface{}) (interface{}, error) {
	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy direct fields (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}
	if userID, ok := getStringField(m, "user_id", "UserID"); ok {
		result["user_id"] = userID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of user fields: nested request or top level
	fieldSource := nestedReq
	if fieldSource == nil {
		// No nested request - fields are at the top level (flat format)
		fieldSource = m
	}

	// Map username directly (SDK username -> server username)
	if username, ok := getStringField(fieldSource, "username", "Username"); ok && username != "" {
		u := username
		result["username"] = &u
	}

	// Map display_name to name (SDK display_name -> server name)
	if displayName, ok := getStringField(fieldSource, "display_name", "DisplayName"); ok && displayName != "" {
		n := displayName
		result["name"] = &n
	} else if name, ok := getStringField(fieldSource, "name", "Name"); ok && name != "" {
		n := name
		result["name"] = &n
	}

	// Check for email with both lowercase and uppercase keys
	if email, ok := getStringField(fieldSource, "email", "Email"); ok && email != "" {
		e := email
		result["email"] = &e
	}

	// Check for metadata with both lowercase and uppercase keys
	if metadata, ok := getInterfaceField(fieldSource, "metadata", "Metadata"); ok {
		result["attributes"] = metadata
	}

	return result, nil
}

// transformUserListRequest transforms user list request.
func transformUserListRequest(req interface{}) (interface{}, error) {
	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of list fields: nested request or top level
	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Check for page_size with both lowercase and uppercase keys
	if pageSize, ok := fieldSource["page_size"].(float64); ok {
		result["limit"] = int(pageSize)
	} else if pageSize, ok := fieldSource["PageSize"].(float64); ok {
		result["limit"] = int(pageSize)
	} else if pageSize, ok := fieldSource["limit"].(float64); ok {
		result["limit"] = int(pageSize)
	}

	// Check for filter with both lowercase and uppercase keys
	if filter, ok := getInterfaceField(fieldSource, "filter", "Filter"); ok {
		result["filter"] = filter
	}

	// Check for role_id with both lowercase and uppercase keys
	if roleID, ok := getStringField(fieldSource, "role_id", "RoleID"); ok && roleID != "" {
		if result["filter"] == nil {
			result["filter"] = make(map[string]interface{})
		}
		if f, ok := result["filter"].(map[string]interface{}); ok {
			f["role_id"] = roleID
		}
	}

	// Check for group_id with both lowercase and uppercase keys
	if groupID, ok := getStringField(fieldSource, "group_id", "GroupID"); ok && groupID != "" {
		if result["filter"] == nil {
			result["filter"] = make(map[string]interface{})
		}
		if f, ok := result["filter"].(map[string]interface{}); ok {
			f["group_id"] = groupID
		}
	}

	return result, nil
}

// transformGroupCreateRequest transforms group create request.
func transformGroupCreateRequest(req interface{}) (interface{}, error) {
	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of group fields: nested request or top level
	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Check for id with both lowercase and uppercase keys
	// This allows clients to provide a specific ID for the group
	if id, ok := getStringField(fieldSource, "id", "ID"); ok {
		result["id"] = id
	} else if id, ok := getStringField(m, "id", "ID"); ok {
		// Also check top-level in case id is not in nested request
		result["id"] = id
	}

	// Check for name with both lowercase and uppercase keys
	if name, ok := getStringField(fieldSource, "name", "Name"); ok {
		result["name"] = name
	}

	// Check for description with both lowercase and uppercase keys
	if description, ok := getStringField(fieldSource, "description", "Description"); ok {
		result["description"] = description
	}

	// Check for metadata with both lowercase and uppercase keys
	if metadata, ok := getInterfaceField(fieldSource, "metadata", "Metadata"); ok {
		result["metadata"] = metadata
	}

	return result, nil
}

// transformGroupUpdateRequest transforms group update request.
func transformGroupUpdateRequest(req interface{}) (interface{}, error) {
	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy direct fields (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}
	if groupID, ok := getStringField(m, "group_id", "GroupID"); ok {
		result["group_id"] = groupID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of group fields: nested request or top level
	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Check for name with both lowercase and uppercase keys
	if name, ok := getStringField(fieldSource, "name", "Name"); ok && name != "" {
		n := name
		result["name"] = &n
	}

	// Check for description with both lowercase and uppercase keys
	if description, ok := getStringField(fieldSource, "description", "Description"); ok && description != "" {
		d := description
		result["description"] = &d
	}

	return result, nil
}

// transformGroupListRequest transforms group list request.
func transformGroupListRequest(req interface{}) (interface{}, error) {
	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of list fields: nested request or top level
	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Check for page_size with both lowercase and uppercase keys
	if pageSize, ok := fieldSource["page_size"].(float64); ok {
		result["limit"] = int(pageSize)
	} else if pageSize, ok := fieldSource["PageSize"].(float64); ok {
		result["limit"] = int(pageSize)
	} else if pageSize, ok := fieldSource["limit"].(float64); ok {
		result["limit"] = int(pageSize)
	}

	// Check for filter with both lowercase and uppercase keys
	if filter, ok := getInterfaceField(fieldSource, "filter", "Filter"); ok {
		result["filter"] = filter
	}

	return result, nil
}

// transformRoleCreateRequest transforms role create request.
func transformRoleCreateRequest(req interface{}) (interface{}, error) {
	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of role fields: nested request or top level
	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Check for id with both lowercase and uppercase keys
	// This allows clients to provide a specific ID for the role
	if id, ok := getStringField(fieldSource, "id", "ID"); ok {
		result["id"] = id
	} else if id, ok := getStringField(m, "id", "ID"); ok {
		// Also check top-level in case id is not in nested request
		result["id"] = id
	}

	// Check for name with both lowercase and uppercase keys
	if name, ok := getStringField(fieldSource, "name", "Name"); ok {
		result["name"] = name
	}

	// Check for description with both lowercase and uppercase keys
	if description, ok := getStringField(fieldSource, "description", "Description"); ok {
		result["description"] = description
	}

	// Check for permissions with both lowercase and uppercase keys
	if permissions, ok := getInterfaceField(fieldSource, "permissions", "Permissions"); ok {
		result["permissions"] = permissions
	}

	return result, nil
}

// transformRoleUpdateRequest transforms role update request.
func transformRoleUpdateRequest(req interface{}) (interface{}, error) {
	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy direct fields (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}
	if roleID, ok := getStringField(m, "role_id", "RoleID"); ok {
		result["role_id"] = roleID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of role fields: nested request or top level
	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Check for name with both lowercase and uppercase keys
	if name, ok := getStringField(fieldSource, "name", "Name"); ok && name != "" {
		n := name
		result["name"] = &n
	}

	// Check for description with both lowercase and uppercase keys
	if description, ok := getStringField(fieldSource, "description", "Description"); ok && description != "" {
		d := description
		result["description"] = &d
	}

	// Check for permissions with both lowercase and uppercase keys
	if permissions, ok := getInterfaceField(fieldSource, "permissions", "Permissions"); ok {
		result["permissions"] = permissions
	}

	return result, nil
}

// transformRoleListRequest transforms role list request.
func transformRoleListRequest(req interface{}) (interface{}, error) {
	// First, convert to map if it's a struct
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id (check both lowercase and uppercase)
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object (check both lowercase and uppercase)
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Determine the source of list fields: nested request or top level
	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Check for page_size with both lowercase and uppercase keys
	if pageSize, ok := fieldSource["page_size"].(float64); ok {
		result["limit"] = int(pageSize)
	} else if pageSize, ok := fieldSource["PageSize"].(float64); ok {
		result["limit"] = int(pageSize)
	} else if pageSize, ok := fieldSource["limit"].(float64); ok {
		result["limit"] = int(pageSize)
	}

	// Check for filter with both lowercase and uppercase keys
	if filter, ok := getInterfaceField(fieldSource, "filter", "Filter"); ok {
		result["filter"] = filter
	}

	return result, nil
}

// transformResponse transforms server IAM responses to SDK-expected format.
func (h *iamHandler) transformResponse(service, method string, resp map[string]interface{}) (interface{}, error) {
	switch {
	case strings.Contains(service, "MFA"):
		// MFA responses are returned directly from the server without
		// additional nesting, so pass them through as-is.
		return resp, nil
	case strings.Contains(service, "Tenant"):
		return h.transformTenantResponse(method, resp)
	case strings.Contains(service, "User"):
		return h.transformUserResponse(method, resp)
	case strings.Contains(service, "Group"):
		return h.transformGroupResponse(method, resp)
	case strings.Contains(service, "Role"):
		return h.transformRoleResponse(method, resp)
	default:
		// Return raw response for other IAM services
		return resp, nil
	}
}

// transformTenantResponse transforms tenant responses.
func (h *iamHandler) transformTenantResponse(method string, resp map[string]interface{}) (interface{}, error) {
	switch method {
	case "Create", "Get", "Update":
		// Server returns: {"tenant": {...}, "timestamp": "..."}
		// SDK expects: {"id": "...", "name": "...", ...}
		return extractTenant(resp), nil
	case "Delete":
		// Server returns: {"deleted": bool, "timestamp": "..."}
		// SDK expects: {"success": bool} or empty
		if deleted, ok := resp["deleted"].(bool); ok {
			return map[string]bool{"success": deleted}, nil
		}
		return map[string]bool{"success": true}, nil
	case "List":
		// Server returns: {"tenants": [...], "total_count": N, "timestamp": "..."}
		// SDK expects: {"tenants": [...], "pagination": {...}}
		return transformTenantList(resp), nil
	default:
		return resp, nil
	}
}

// transformUserResponse transforms user responses.
func (h *iamHandler) transformUserResponse(method string, resp map[string]interface{}) (interface{}, error) {
	switch method {
	case "Create", "Get", "Update", "GetByEmail", "FindByUsername":
		// Server returns: {"user": {...}, "timestamp": "..."}
		// SDK expects: {"id": "...", "username": "...", ...}
		return extractUser(resp), nil
	case "Delete", "AssignRole", "RevokeRole", "AddToGroup", "RemoveFromGroup", "Suspend", "Unsuspend":
		// Server returns: {"deleted": bool, "timestamp": "..."} or similar
		// SDK expects: {"success": bool}
		if deleted, ok := resp["deleted"].(bool); ok {
			return map[string]bool{"success": deleted}, nil
		}
		if user, ok := resp["user"]; ok && user != nil {
			return map[string]bool{"success": true}, nil
		}
		return map[string]bool{"success": true}, nil
	case "List":
		// Server returns: {"users": [...], "total_count": N, "timestamp": "..."}
		// SDK expects: {"users": [...], "pagination": {...}}
		return transformUserList(resp), nil
	default:
		return resp, nil
	}
}

// transformGroupResponse transforms group responses.
func (h *iamHandler) transformGroupResponse(method string, resp map[string]interface{}) (interface{}, error) {
	switch method {
	case "Create", "Get", "Update":
		// Server returns: {"group": {...}, "timestamp": "..."}
		// SDK expects: {"id": "...", "name": "...", ...}
		return extractGroup(resp), nil
	case "Delete", "AddMember", "RemoveMember":
		// Server returns: {"deleted": bool, "timestamp": "..."} or {"group": {...}}
		// SDK expects: {"success": bool}
		if deleted, ok := resp["deleted"].(bool); ok {
			return map[string]bool{"success": deleted}, nil
		}
		if group, ok := resp["group"]; ok && group != nil {
			return map[string]bool{"success": true}, nil
		}
		return map[string]bool{"success": true}, nil
	case "List":
		// Server returns: {"groups": [...], "total_count": N, "timestamp": "..."}
		// SDK expects: {"groups": [...], "pagination": {...}}
		return transformGroupList(resp), nil
	default:
		return resp, nil
	}
}

// transformRoleResponse transforms role responses.
func (h *iamHandler) transformRoleResponse(method string, resp map[string]interface{}) (interface{}, error) {
	switch method {
	case "Create", "Get", "Update":
		// Server returns: {"role": {...}, "timestamp": "..."}
		// SDK expects: {"id": "...", "name": "...", ...}
		return extractRole(resp), nil
	case "Delete":
		// Server returns: {"deleted": bool, "timestamp": "..."}
		// SDK expects: {"success": bool}
		if deleted, ok := resp["deleted"].(bool); ok {
			return map[string]bool{"success": deleted}, nil
		}
		return map[string]bool{"success": true}, nil
	case "List":
		// Server returns: {"roles": [...], "total_count": N, "timestamp": "..."}
		// SDK expects: {"roles": [...], "pagination": {...}}
		return transformRoleList(resp), nil
	default:
		return resp, nil
	}
}

// extractTenant extracts and flattens the tenant from a server response.
func extractTenant(resp map[string]interface{}) map[string]interface{} {
	if resp == nil {
		return nil
	}

	// If response has a "tenant" key, extract it
	if tenant, ok := resp["tenant"].(map[string]interface{}); ok {
		return flattenTenant(tenant)
	}

	// Response might already be flat
	if _, hasID := resp["id"]; hasID {
		return flattenTenant(resp)
	}

	return resp
}

// flattenTenant converts server tenant format to SDK format.
func flattenTenant(tenant map[string]interface{}) map[string]interface{} {
	if tenant == nil {
		return nil
	}

	result := make(map[string]interface{})

	// Copy basic fields
	if v, ok := tenant["id"].(string); ok {
		result["id"] = v
	}
	if v, ok := tenant["name"].(string); ok {
		result["name"] = v
	}

	// Handle display_name - server uses Name, SDK expects display_name
	if v, ok := tenant["name"].(string); ok && result["display_name"] == nil {
		result["display_name"] = v
	}

	// Handle settings
	if settings, ok := tenant["settings"].(map[string]interface{}); ok {
		result["settings"] = settings
	}

	// Handle timestamps - server uses "created"/"updated", SDK expects "created_at"/"updated_at"
	if v, ok := tenant["created"].(string); ok {
		result["created_at"] = v
	} else if v, ok := tenant["created_at"].(string); ok {
		result["created_at"] = v
	}
	if v, ok := tenant["updated"].(string); ok {
		result["updated_at"] = v
	} else if v, ok := tenant["updated_at"].(string); ok {
		result["updated_at"] = v
	}

	// Handle timestamps as time.Time
	if v, ok := tenant["created"].(time.Time); ok {
		result["created_at"] = v.Format(time.RFC3339)
	}
	if v, ok := tenant["updated"].(time.Time); ok {
		result["updated_at"] = v.Format(time.RFC3339)
	}

	// Handle quotas
	if quotas, ok := tenant["quotas"].(map[string]interface{}); ok {
		result["quotas"] = quotas
	}

	// Handle status
	if v, ok := tenant["status"].(string); ok {
		result["status"] = v
	}

	return result
}

// extractUser extracts and flattens the user from a server response.
func extractUser(resp map[string]interface{}) map[string]interface{} {
	if resp == nil {
		return nil
	}

	// If response has a "user" key, extract it
	if user, ok := resp["user"].(map[string]interface{}); ok {
		return flattenUser(user)
	}

	// Response might already be flat
	if _, hasID := resp["id"]; hasID {
		return flattenUser(resp)
	}

	return resp
}

// flattenUser converts server user format to SDK format.
func flattenUser(user map[string]interface{}) map[string]interface{} {
	if user == nil {
		return nil
	}

	result := make(map[string]interface{})

	// Copy basic fields
	if v, ok := user["id"].(string); ok {
		result["id"] = v
	}
	if v, ok := user["tenant_id"].(string); ok {
		result["tenant_id"] = v
	}

	// Handle username/name mapping
	// Server uses "name" for display name, SDK expects "username" for username
	if v, ok := user["email"].(string); ok {
		result["email"] = v
	}
	if v, ok := user["name"].(string); ok {
		// Server "name" is display name
		result["display_name"] = v
		// If no username set, use name as username (for SDK compatibility)
		if _, hasUsername := user["username"]; !hasUsername {
			result["username"] = v
		}
	}
	if v, ok := user["username"].(string); ok {
		result["username"] = v
	}
	if v, ok := user["display_name"].(string); ok {
		result["display_name"] = v
	}

	// Handle MFA
	if v, ok := user["mfa_enabled"].(bool); ok {
		result["mfa_enabled"] = v
	}
	if v, ok := user["email_verified"].(bool); ok {
		result["email_verified"] = v
	}

	// Handle roles and groups
	if roles, ok := user["roles"].([]interface{}); ok {
		result["roles"] = roles
	} else if roles, ok := user["roles"].([]string); ok {
		result["roles"] = roles
	}
	if groups, ok := user["groups"].([]interface{}); ok {
		result["groups"] = groups
	} else if groups, ok := user["groups"].([]string); ok {
		result["groups"] = groups
	}

	// Handle metadata/attributes
	if metadata, ok := user["metadata"].(map[string]interface{}); ok {
		result["metadata"] = metadata
	}
	if attributes, ok := user["attributes"].(map[string]interface{}); ok {
		result["metadata"] = attributes
	}

	// Handle timestamps
	if v, ok := user["created"].(string); ok {
		result["created_at"] = v
	} else if v, ok := user["created_at"].(string); ok {
		result["created_at"] = v
	}
	if v, ok := user["updated"].(string); ok {
		result["updated_at"] = v
	} else if v, ok := user["updated_at"].(string); ok {
		result["updated_at"] = v
	}
	if v, ok := user["last_login_at"].(string); ok {
		result["last_login_at"] = v
	}

	// Handle timestamps as time.Time
	if v, ok := user["created"].(time.Time); ok {
		result["created_at"] = v.Format(time.RFC3339)
	}
	if v, ok := user["updated"].(time.Time); ok {
		result["updated_at"] = v.Format(time.RFC3339)
	}

	return result
}

// extractGroup extracts and flattens the group from a server response.
func extractGroup(resp map[string]interface{}) map[string]interface{} {
	if resp == nil {
		return nil
	}

	// If response has a "group" key, extract it
	if group, ok := resp["group"].(map[string]interface{}); ok {
		return flattenGroup(group)
	}

	// Response might already be flat
	if _, hasID := resp["id"]; hasID {
		return flattenGroup(resp)
	}

	return resp
}

// flattenGroup converts server group format to SDK format.
func flattenGroup(group map[string]interface{}) map[string]interface{} {
	if group == nil {
		return nil
	}

	result := make(map[string]interface{})

	// Copy basic fields
	if v, ok := group["id"].(string); ok {
		result["id"] = v
	}
	if v, ok := group["tenant_id"].(string); ok {
		result["tenant_id"] = v
	}
	if v, ok := group["name"].(string); ok {
		result["name"] = v
	}
	if v, ok := group["display_name"].(string); ok {
		result["display_name"] = v
	}
	if v, ok := group["description"].(string); ok {
		result["description"] = v
	}

	// Handle members
	if members, ok := group["members"].([]interface{}); ok {
		result["members"] = members
	} else if members, ok := group["members"].([]string); ok {
		result["members"] = members
	}

	// Handle timestamps
	if v, ok := group["created"].(string); ok {
		result["created_at"] = v
	} else if v, ok := group["created_at"].(string); ok {
		result["created_at"] = v
	}
	if v, ok := group["updated"].(string); ok {
		result["updated_at"] = v
	} else if v, ok := group["updated_at"].(string); ok {
		result["updated_at"] = v
	}

	// Handle timestamps as time.Time
	if v, ok := group["created"].(time.Time); ok {
		result["created_at"] = v.Format(time.RFC3339)
	}
	if v, ok := group["updated"].(time.Time); ok {
		result["updated_at"] = v.Format(time.RFC3339)
	}

	return result
}

// extractRole extracts and flattens the role from a server response.
func extractRole(resp map[string]interface{}) map[string]interface{} {
	if resp == nil {
		return nil
	}

	// If response has a "role" key, extract it
	if role, ok := resp["role"].(map[string]interface{}); ok {
		return flattenRole(role)
	}

	// Response might already be flat
	if _, hasID := resp["id"]; hasID {
		return flattenRole(resp)
	}

	return resp
}

// flattenRole converts server role format to SDK format.
func flattenRole(role map[string]interface{}) map[string]interface{} {
	if role == nil {
		return nil
	}

	result := make(map[string]interface{})

	// Copy basic fields
	if v, ok := role["id"].(string); ok {
		result["id"] = v
	}
	if v, ok := role["tenant_id"].(string); ok {
		result["tenant_id"] = v
	}
	if v, ok := role["name"].(string); ok {
		result["name"] = v
	}
	if v, ok := role["display_name"].(string); ok {
		result["display_name"] = v
	}
	if v, ok := role["description"].(string); ok {
		result["description"] = v
	}

	// Handle permissions
	if permissions, ok := role["permissions"].([]interface{}); ok {
		result["permissions"] = permissions
	} else if permissions, ok := role["permissions"].([]string); ok {
		result["permissions"] = permissions
	}

	// Handle timestamps
	if v, ok := role["created"].(string); ok {
		result["created_at"] = v
	} else if v, ok := role["created_at"].(string); ok {
		result["created_at"] = v
	}
	if v, ok := role["updated"].(string); ok {
		result["updated_at"] = v
	} else if v, ok := role["updated_at"].(string); ok {
		result["updated_at"] = v
	}

	// Handle timestamps as time.Time
	if v, ok := role["created"].(time.Time); ok {
		result["created_at"] = v.Format(time.RFC3339)
	}
	if v, ok := role["updated"].(time.Time); ok {
		result["updated_at"] = v.Format(time.RFC3339)
	}

	return result
}

// transformTenantList transforms the tenant list response.
func transformTenantList(resp map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Extract and transform tenants
	if tenants, ok := resp["tenants"].([]interface{}); ok {
		transformedTenants := make([]interface{}, 0, len(tenants))
		for _, t := range tenants {
			if tenant, ok := t.(map[string]interface{}); ok {
				transformedTenants = append(transformedTenants, flattenTenant(tenant))
			}
		}
		result["tenants"] = transformedTenants
	} else {
		result["tenants"] = []interface{}{}
	}

	// Build pagination info
	pagination := make(map[string]interface{})
	if total, ok := resp["total_count"].(float64); ok {
		pagination["total_count"] = int(total)
	} else if total, ok := resp["total_count"].(int64); ok {
		pagination["total_count"] = int(total)
	} else if total, ok := resp["total_count"].(int); ok {
		pagination["total_count"] = total
	}
	result["pagination"] = pagination

	return result
}

// transformUserList transforms the user list response.
func transformUserList(resp map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Extract and transform users
	if users, ok := resp["users"].([]interface{}); ok {
		transformedUsers := make([]interface{}, 0, len(users))
		for _, u := range users {
			if user, ok := u.(map[string]interface{}); ok {
				transformedUsers = append(transformedUsers, flattenUser(user))
			}
		}
		result["users"] = transformedUsers
	} else {
		result["users"] = []interface{}{}
	}

	// Build pagination info
	pagination := make(map[string]interface{})
	if total, ok := resp["total_count"].(float64); ok {
		pagination["total_count"] = int(total)
	} else if total, ok := resp["total_count"].(int64); ok {
		pagination["total_count"] = int(total)
	} else if total, ok := resp["total_count"].(int); ok {
		pagination["total_count"] = total
	}
	result["pagination"] = pagination

	return result
}

// transformGroupList transforms the group list response.
func transformGroupList(resp map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Extract and transform groups
	if groups, ok := resp["groups"].([]interface{}); ok {
		transformedGroups := make([]interface{}, 0, len(groups))
		for _, g := range groups {
			if group, ok := g.(map[string]interface{}); ok {
				transformedGroups = append(transformedGroups, flattenGroup(group))
			}
		}
		result["groups"] = transformedGroups
	} else {
		result["groups"] = []interface{}{}
	}

	// Build pagination info
	pagination := make(map[string]interface{})
	if total, ok := resp["total_count"].(float64); ok {
		pagination["total_count"] = int(total)
	} else if total, ok := resp["total_count"].(int64); ok {
		pagination["total_count"] = int(total)
	} else if total, ok := resp["total_count"].(int); ok {
		pagination["total_count"] = total
	}
	result["pagination"] = pagination

	return result
}

// transformRoleList transforms the role list response.
func transformRoleList(resp map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Extract and transform roles
	if roles, ok := resp["roles"].([]interface{}); ok {
		transformedRoles := make([]interface{}, 0, len(roles))
		for _, r := range roles {
			if role, ok := r.(map[string]interface{}); ok {
				transformedRoles = append(transformedRoles, flattenRole(role))
			}
		}
		result["roles"] = transformedRoles
	} else {
		result["roles"] = []interface{}{}
	}

	// Build pagination info
	pagination := make(map[string]interface{})
	if total, ok := resp["total_count"].(float64); ok {
		pagination["total_count"] = int(total)
	} else if total, ok := resp["total_count"].(int64); ok {
		pagination["total_count"] = int(total)
	} else if total, ok := resp["total_count"].(int); ok {
		pagination["total_count"] = total
	}
	result["pagination"] = pagination

	return result
}

// =============================================================================
// Policy Request Transformations
// =============================================================================

// transformPolicyCreateRequest transforms SDK's Policy Create request.
// SDK sends: {tenant_id: "...", request: {name, description, rules, ...}}
// Test client sends: {tenant_id: "...", policy: {...}} (complete policy object)
// Server expects: {tenant_id: "...", policy: {...}}
func transformPolicyCreateRequest(req interface{}) (interface{}, error) {
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Check if a complete policy object is already provided (test client format)
	if policy, ok := m["policy"].(map[string]interface{}); ok {
		result["policy"] = policy
		return result, nil
	}
	if policy, ok := m["Policy"].(map[string]interface{}); ok {
		result["policy"] = policy
		return result, nil
	}

	// Get nested request object and convert to policy
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	// Build policy object
	policy := make(map[string]interface{})

	// Determine source for policy fields - use nested request if available, otherwise use top-level
	fieldSource := nestedReq
	if fieldSource == nil {
		// Flat format: fields are at top level (e.g., test client format)
		fieldSource = m
	}

	// Copy standard fields
	if name, ok := getStringField(fieldSource, "name", "Name"); ok {
		policy["name"] = name
	}
	if desc, ok := getStringField(fieldSource, "description", "Description"); ok {
		policy["description"] = desc
	}
	if enabled, ok := fieldSource["enabled"].(bool); ok {
		policy["enabled"] = enabled
	} else if enabled, ok := fieldSource["Enabled"].(bool); ok {
		policy["enabled"] = enabled
	}
	if priority, ok := fieldSource["priority"].(float64); ok {
		policy["priority"] = int(priority)
	} else if priority, ok := fieldSource["Priority"].(float64); ok {
		policy["priority"] = int(priority)
	}

	// Copy rules
	if rules, ok := fieldSource["rules"]; ok {
		policy["rules"] = rules
	} else if rules, ok := fieldSource["Rules"]; ok {
		policy["rules"] = rules
	}

	// Copy conditions
	if conditions, ok := fieldSource["conditions"]; ok {
		policy["conditions"] = conditions
	} else if conditions, ok := fieldSource["Conditions"]; ok {
		policy["conditions"] = conditions
	}

	// Only set policy if we have at least a name
	if _, hasName := policy["name"]; hasName {
		result["policy"] = policy
	}

	return result, nil
}

// transformPolicyUpdateRequest transforms SDK's Policy Update request.
// SDK sends: {tenant_id: "...", policy_name: "...", request: {...}}
// Server expects: {tenant_id: "...", name: "...", policy: {...}}
func transformPolicyUpdateRequest(req interface{}) (interface{}, error) {
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Rename policy_name to name
	if policyName, ok := getStringField(m, "policy_name", "PolicyName"); ok {
		result["name"] = policyName
	}

	// Get nested request object and convert to policy updates
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	if nestedReq != nil {
		// Build policy object from nested request
		policy := make(map[string]interface{})
		for k, v := range nestedReq {
			policy[k] = v
		}
		result["policy"] = policy
	}

	return result, nil
}

// transformPolicyListRequest transforms SDK's Policy List request.
func transformPolicyListRequest(req interface{}) (interface{}, error) {
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Copy pagination fields
	if pageSize, ok := fieldSource["page_size"].(float64); ok {
		result["limit"] = int(pageSize)
	} else if pageSize, ok := fieldSource["PageSize"].(float64); ok {
		result["limit"] = int(pageSize)
	}
	if pageToken, ok := getStringField(fieldSource, "page_token", "PageToken"); ok {
		result["offset"] = pageToken
	}
	if filter, ok := getStringField(fieldSource, "filter", "Filter"); ok {
		result["filter"] = filter
	}

	return result, nil
}

// transformPolicyTestRequest transforms SDK's Policy Test request.
func transformPolicyTestRequest(req interface{}) (interface{}, error) {
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	if nestedReq != nil {
		// Copy policy_name
		if policyName, ok := getStringField(nestedReq, "policy_name", "PolicyName"); ok {
			result["policy_name"] = policyName
		}
		// Copy test_cases
		if testCases, ok := nestedReq["test_cases"]; ok {
			result["test_cases"] = testCases
		} else if testCases, ok := nestedReq["TestCases"]; ok {
			result["test_cases"] = testCases
		}
	}

	return result, nil
}

// =============================================================================
// App Request Transformations
// =============================================================================

// transformAppRegisterRequest transforms SDK's App Register request.
func transformAppRegisterRequest(req interface{}) (interface{}, error) {
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Copy app fields
	if name, ok := getStringField(fieldSource, "name", "Name"); ok {
		result["name"] = name
	}
	if desc, ok := getStringField(fieldSource, "description", "Description"); ok {
		result["description"] = desc
	}
	if appType, ok := getStringField(fieldSource, "type", "Type"); ok {
		result["type"] = appType
	}
	if redirectURIs, ok := fieldSource["redirect_uris"]; ok {
		result["redirect_uris"] = redirectURIs
	} else if redirectURIs, ok := fieldSource["RedirectURIs"]; ok {
		result["redirect_uris"] = redirectURIs
	}
	if scopes, ok := fieldSource["scopes"]; ok {
		result["scopes"] = scopes
	} else if scopes, ok := fieldSource["Scopes"]; ok {
		result["scopes"] = scopes
	}

	return result, nil
}

// transformAppUpdateRequest transforms SDK's App Update request.
func transformAppUpdateRequest(req interface{}) (interface{}, error) {
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Rename app_id to id
	if appID, ok := getStringField(m, "app_id", "AppID"); ok {
		result["id"] = appID
	}

	// Get nested request object
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	if nestedReq != nil {
		// Copy update fields
		for k, v := range nestedReq {
			result[k] = v
		}
	}

	return result, nil
}

// transformAppListRequest transforms SDK's App List request.
func transformAppListRequest(req interface{}) (interface{}, error) {
	m, err := structToMap(req)
	if err != nil {
		return req, nil
	}

	result := make(map[string]interface{})

	// Copy tenant_id
	if tenantID, ok := getStringField(m, "tenant_id", "TenantID"); ok {
		result["tenant_id"] = tenantID
	}

	// Get nested request object
	var nestedReq map[string]interface{}
	if nested, ok := m["request"].(map[string]interface{}); ok {
		nestedReq = nested
	} else if nested, ok := m["Request"].(map[string]interface{}); ok {
		nestedReq = nested
	}

	fieldSource := nestedReq
	if fieldSource == nil {
		fieldSource = m
	}

	// Copy pagination fields
	if pageSize, ok := fieldSource["page_size"].(float64); ok {
		result["limit"] = int(pageSize)
	} else if pageSize, ok := fieldSource["PageSize"].(float64); ok {
		result["limit"] = int(pageSize)
	}
	if pageToken, ok := getStringField(fieldSource, "page_token", "PageToken"); ok {
		result["offset"] = pageToken
	}
	if filter, ok := getStringField(fieldSource, "filter", "Filter"); ok {
		result["filter"] = filter
	}

	return result, nil
}
