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

package iam

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// convertToRole converts a response to a Role type.
// It handles both *Role pointers and map[string]interface{} (from REST/gRPC transports).
func convertToRole(resp interface{}) (*Role, bool) {
	if resp == nil {
		return nil, false
	}

	// If already a *Role, return it directly.
	if role, ok := resp.(*Role); ok {
		return role, true
	}

	// If it's a map, try to convert it.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, false
	}

	// Check for nested role field (server response format: {role: {...}, timestamp: "..."})
	if roleData, ok := respMap["role"].(map[string]interface{}); ok {
		respMap = roleData
	}

	role := &Role{}

	if v, ok := respMap["id"].(string); ok {
		role.ID = v
	}
	if v, ok := respMap["tenant_id"].(string); ok {
		role.TenantID = v
	}
	if v, ok := respMap["name"].(string); ok {
		role.Name = v
	}
	if v, ok := respMap["display_name"].(string); ok {
		role.DisplayName = v
	}
	if v, ok := respMap["description"].(string); ok {
		role.Description = v
	}
	if v, ok := respMap["is_system"].(bool); ok {
		role.IsSystem = v
	}

	// Handle permissions slice.
	if permsRaw, ok := respMap["permissions"]; ok {
		switch perms := permsRaw.(type) {
		case []string:
			role.Permissions = perms
		case []interface{}:
			for _, p := range perms {
				if s, ok := p.(string); ok {
					role.Permissions = append(role.Permissions, s)
				}
			}
		}
	}

	// Handle metadata map.
	if metaRaw, ok := respMap["metadata"]; ok {
		switch meta := metaRaw.(type) {
		case map[string]string:
			role.Metadata = meta
		case map[string]interface{}:
			role.Metadata = make(map[string]string, len(meta))
			for k, v := range meta {
				if s, ok := v.(string); ok {
					role.Metadata[k] = s
				}
			}
		}
	}

	// Handle timestamps.
	if v, ok := respMap["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			role.CreatedAt = t
		}
	} else if v, ok := respMap["created"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			role.CreatedAt = t
		}
	}
	if v, ok := respMap["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			role.UpdatedAt = t
		}
	} else if v, ok := respMap["updated"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			role.UpdatedAt = t
		}
	}

	return role, true
}

const (
	roleServiceName = "quicbaas.IAM.Role"
)

// RoleClient provides role management operations.
type RoleClient interface {
	// Create creates a new role in a tenant.
	Create(ctx context.Context, tenantID string, req *CreateRoleRequest) (*Role, error)

	// Get retrieves a role by ID.
	Get(ctx context.Context, tenantID, roleID string) (*Role, error)

	// Update updates a role.
	Update(ctx context.Context, tenantID, roleID string, req *UpdateRoleRequest) (*Role, error)

	// Delete deletes a role.
	Delete(ctx context.Context, tenantID, roleID string) error

	// List lists roles in a tenant with optional filtering.
	List(ctx context.Context, tenantID string, req *ListRolesRequest) (*ListRolesResponse, error)
}

// CreateRoleRequest is a request to create a role.
type CreateRoleRequest struct {
	// ID is the client-provided role ID.
	// If empty, the server will generate a UUID.
	ID string `json:"id,omitempty"`

	// Name is the unique name within the tenant.
	Name string `json:"name"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Permissions are the permissions granted by this role.
	Permissions []string `json:"permissions"`

	// Metadata contains additional role metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdateRoleRequest is a request to update a role.
type UpdateRoleRequest struct {
	// Name is the unique name within the tenant.
	Name string `json:"name,omitempty"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Permissions are the permissions granted by this role (replaces existing).
	Permissions []string `json:"permissions,omitempty"`

	// Metadata contains additional role metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ListRolesRequest is a request to list roles.
type ListRolesRequest struct {
	// PageSize is the maximum number of roles to return.
	PageSize int `json:"page_size,omitempty"`

	// PageToken is the token for the next page.
	PageToken string `json:"page_token,omitempty"`

	// Filter is an optional filter expression.
	Filter string `json:"filter,omitempty"`

	// IncludeSystem includes system-defined roles.
	IncludeSystem bool `json:"include_system,omitempty"`
}

// ListRolesResponse is the response from listing roles.
type ListRolesResponse struct {
	// Roles is the list of roles.
	Roles []Role `json:"roles"`

	// Pagination contains pagination information.
	Pagination PaginationInfo `json:"pagination"`
}

// Internal request/response types for transport.

type createRoleRequestInternal struct {
	TenantID string             `json:"tenant_id"`
	Request  *CreateRoleRequest `json:"request"`
}

type getRoleRequest struct {
	TenantID string `json:"tenant_id"`
	RoleID   string `json:"role_id"`
}

type updateRoleRequestInternal struct {
	TenantID string             `json:"tenant_id"`
	RoleID   string             `json:"role_id"`
	Request  *UpdateRoleRequest `json:"request"`
}

type deleteRoleRequest struct {
	TenantID string `json:"tenant_id"`
	RoleID   string `json:"role_id"`
}

type deleteRoleResponse struct{}

type listRolesRequestInternal struct {
	TenantID string            `json:"tenant_id"`
	Request  *ListRolesRequest `json:"request"`
}

// roleClient implements the RoleClient interface.
type roleClient struct {
	transport transport.Transport
}

// NewRoleClient creates a new role client.
func NewRoleClient(t transport.Transport) RoleClient {
	if t == nil {
		return nil
	}
	return &roleClient{
		transport: t,
	}
}

// Create creates a new role in a tenant.
func (c *roleClient) Create(ctx context.Context, tenantID string, req *CreateRoleRequest) (*Role, error) {
	if err := validateCreateRoleRequest(tenantID, req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.role.Create", "context is required")
	}

	resp, err := c.transport.Request(ctx, roleServiceName, "Create", &createRoleRequestInternal{
		TenantID: tenantID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.role.Create", "nil response from server")
	}

	role, ok := convertToRole(resp)
	if !ok {
		return nil, errors.NewInternal("iam.role.Create", "invalid response type")
	}

	return role, nil
}

// Get retrieves a role by ID.
func (c *roleClient) Get(ctx context.Context, tenantID, roleID string) (*Role, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.role.Get", "tenant ID is required")
	}

	if roleID == "" {
		return nil, errors.NewInvalidInput("iam.role.Get", "role ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.role.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, roleServiceName, "Get", &getRoleRequest{
		TenantID: tenantID,
		RoleID:   roleID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.role.Get", "role not found")
	}

	role, ok := convertToRole(resp)
	if !ok {
		return nil, errors.NewInternal("iam.role.Get", "invalid response type")
	}

	return role, nil
}

// Update updates a role.
func (c *roleClient) Update(ctx context.Context, tenantID, roleID string, req *UpdateRoleRequest) (*Role, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.role.Update", "tenant ID is required")
	}

	if roleID == "" {
		return nil, errors.NewInvalidInput("iam.role.Update", "role ID is required")
	}

	if req == nil {
		return nil, errors.NewInvalidInput("iam.role.Update", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.role.Update", "context is required")
	}

	resp, err := c.transport.Request(ctx, roleServiceName, "Update", &updateRoleRequestInternal{
		TenantID: tenantID,
		RoleID:   roleID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.role.Update", "nil response from server")
	}

	role, ok := convertToRole(resp)
	if !ok {
		return nil, errors.NewInternal("iam.role.Update", "invalid response type")
	}

	return role, nil
}

// Delete deletes a role.
func (c *roleClient) Delete(ctx context.Context, tenantID, roleID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.role.Delete", "tenant ID is required")
	}

	if roleID == "" {
		return errors.NewInvalidInput("iam.role.Delete", "role ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.role.Delete", "context is required")
	}

	resp, err := c.transport.Request(ctx, roleServiceName, "Delete", &deleteRoleRequest{
		TenantID: tenantID,
		RoleID:   roleID,
	})
	if err != nil {
		return err
	}

	// Accept nil response, *deleteRoleResponse, or map response.
	if resp == nil {
		return nil
	}

	// If already the expected type, return.
	if _, ok := resp.(*deleteRoleResponse); ok {
		return nil
	}

	// Accept map responses from REST/gRPC transports.
	if _, ok := resp.(map[string]interface{}); ok {
		return nil
	}

	return errors.NewInternal("iam.role.Delete", "invalid response type")
}

// List lists roles in a tenant with optional filtering.
func (c *roleClient) List(ctx context.Context, tenantID string, req *ListRolesRequest) (*ListRolesResponse, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.role.List", "tenant ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.role.List", "context is required")
	}

	if req == nil {
		req = &ListRolesRequest{}
	}

	resp, err := c.transport.Request(ctx, roleServiceName, "List", &listRolesRequestInternal{
		TenantID: tenantID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListRolesResponse{Roles: []Role{}}, nil
	}

	// If already a *ListRolesResponse, return it directly.
	if listResp, ok := resp.(*ListRolesResponse); ok {
		return listResp, nil
	}

	// Handle map[string]interface{} response from REST/gRPC transports.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.role.List", "invalid response type")
	}

	result := &ListRolesResponse{Roles: []Role{}}

	// Parse roles array.
	if rolesRaw, ok := respMap["roles"]; ok {
		switch roles := rolesRaw.(type) {
		case []interface{}:
			for _, r := range roles {
				if role, ok := convertToRole(r); ok {
					result.Roles = append(result.Roles, *role)
				}
			}
		case []*Role:
			for _, r := range roles {
				if r != nil {
					result.Roles = append(result.Roles, *r)
				}
			}
		case []Role:
			result.Roles = roles
		}
	}

	// Parse pagination info.
	if paginationRaw, ok := respMap["pagination"]; ok {
		if pagination, ok := paginationRaw.(map[string]interface{}); ok {
			if v, ok := pagination["total_count"].(float64); ok {
				result.Pagination.TotalCount = int(v)
			}
			if v, ok := pagination["page_size"].(float64); ok {
				result.Pagination.PageSize = int(v)
			}
			if v, ok := pagination["next_page_token"].(string); ok {
				result.Pagination.NextPageToken = v
			}
		}
	}

	// Also handle direct limit/offset from REST API.
	if v, ok := respMap["limit"].(float64); ok {
		result.Pagination.PageSize = int(v)
	}
	if v, ok := respMap["total"].(float64); ok {
		result.Pagination.TotalCount = int(v)
	}

	return result, nil
}

// Validation functions.

func validateCreateRoleRequest(tenantID string, req *CreateRoleRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.role.Create", "tenant ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.role.Create", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("iam.role.Create", "name is required")
	}
	// Permissions are optional - roles can be created without permissions
	// and permissions can be added later via update.
	return nil
}
