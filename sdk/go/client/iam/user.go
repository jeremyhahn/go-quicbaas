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

const (
	userServiceName = "quicbaas.IAM.User"
)

// UserClient provides user management operations.
type UserClient interface {
	// Create creates a new user in a tenant.
	Create(ctx context.Context, tenantID string, req *CreateUserRequest) (*User, error)

	// Get retrieves a user by ID.
	Get(ctx context.Context, tenantID, userID string) (*User, error)

	// GetByEmail retrieves a user by email address.
	GetByEmail(ctx context.Context, tenantID, email string) (*User, error)

	// Update updates a user.
	Update(ctx context.Context, tenantID, userID string, req *UpdateUserRequest) (*User, error)

	// Delete deletes a user.
	Delete(ctx context.Context, tenantID, userID string) error

	// List lists users in a tenant with optional filtering.
	List(ctx context.Context, tenantID string, req *ListUsersRequest) (*ListUsersResponse, error)

	// AssignRole assigns a role to a user.
	AssignRole(ctx context.Context, tenantID, userID, roleID string) error

	// RevokeRole revokes a role from a user.
	RevokeRole(ctx context.Context, tenantID, userID, roleID string) error

	// AddToGroup adds a user to a group.
	AddToGroup(ctx context.Context, tenantID, userID, groupID string) error

	// RemoveFromGroup removes a user from a group.
	RemoveFromGroup(ctx context.Context, tenantID, userID, groupID string) error

	// Suspend suspends a user account.
	Suspend(ctx context.Context, tenantID, userID string) error

	// Unsuspend reactivates a suspended user account.
	Unsuspend(ctx context.Context, tenantID, userID string) error
}

// CreateUserRequest is a request to create a user.
type CreateUserRequest struct {
	// Username is the unique username within the tenant.
	Username string `json:"username"`

	// Email is the user's email address.
	Email string `json:"email"`

	// Password is the user's password (will be hashed).
	Password string `json:"password"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Roles are initial role IDs to assign.
	Roles []string `json:"roles,omitempty"`

	// Groups are initial group IDs to add the user to.
	Groups []string `json:"groups,omitempty"`

	// Metadata contains additional user metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// RequireEmailVerification requires email verification before activation.
	RequireEmailVerification bool `json:"require_email_verification,omitempty"`
}

// UpdateUserRequest is a request to update a user.
type UpdateUserRequest struct {
	// Username is the unique username within the tenant.
	Username string `json:"username,omitempty"`

	// Email is the user's email address.
	Email string `json:"email,omitempty"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Password is a new password (will be hashed).
	Password string `json:"password,omitempty"`

	// Metadata contains additional user metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ListUsersRequest is a request to list users.
type ListUsersRequest struct {
	// PageSize is the maximum number of users to return.
	PageSize int `json:"page_size,omitempty"`

	// PageToken is the token for the next page.
	PageToken string `json:"page_token,omitempty"`

	// Filter is an optional filter expression.
	Filter string `json:"filter,omitempty"`

	// RoleID filters users by role.
	RoleID string `json:"role_id,omitempty"`

	// GroupID filters users by group.
	GroupID string `json:"group_id,omitempty"`

	// Status filters users by status.
	Status *UserStatus `json:"status,omitempty"`
}

// ListUsersResponse is the response from listing users.
type ListUsersResponse struct {
	// Users is the list of users.
	Users []User `json:"users"`

	// Pagination contains pagination information.
	Pagination PaginationInfo `json:"pagination"`
}

// Internal request/response types for transport.

type createUserRequestInternal struct {
	TenantID string             `json:"tenant_id"`
	Request  *CreateUserRequest `json:"request"`
}

type getUserRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

type getUserByEmailRequest struct {
	TenantID string `json:"tenant_id"`
	Email    string `json:"email"`
}

type updateUserRequestInternal struct {
	TenantID string             `json:"tenant_id"`
	UserID   string             `json:"user_id"`
	Request  *UpdateUserRequest `json:"request"`
}

type deleteUserRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

type deleteUserResponse struct{}

type listUsersRequestInternal struct {
	TenantID string            `json:"tenant_id"`
	Request  *ListUsersRequest `json:"request"`
}

type userRoleRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	RoleID   string `json:"role_id"`
}

type userRoleResponse struct{}

type userGroupRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	GroupID  string `json:"group_id"`
}

type userGroupResponse struct{}

type suspendUserRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

type suspendUserResponse struct{}

type unsuspendUserRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

type unsuspendUserResponse struct{}

// convertToUser converts a response to a User type.
// It handles both *User pointers and map[string]interface{} (from REST/gRPC transports).
func convertToUser(resp interface{}) (*User, bool) {
	if resp == nil {
		return nil, false
	}

	// If already a *User, return it directly.
	if user, ok := resp.(*User); ok {
		return user, true
	}

	// If it's a map, try to convert it.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, false
	}

	// Check for nested user field (server response format: {user: {...}, timestamp: "..."})
	if userData, ok := respMap["user"].(map[string]interface{}); ok {
		respMap = userData
	}

	user := &User{}

	if v, ok := respMap["id"].(string); ok {
		user.ID = v
	}
	if v, ok := respMap["tenant_id"].(string); ok {
		user.TenantID = v
	}
	if v, ok := respMap["username"].(string); ok {
		user.Username = v
	}
	if v, ok := respMap["email"].(string); ok {
		user.Email = v
	}
	if v, ok := respMap["display_name"].(string); ok {
		user.DisplayName = v
	} else if v, ok := respMap["name"].(string); ok {
		// Fall back to "name" for compatibility with REST API
		user.DisplayName = v
	}
	if v, ok := respMap["mfa_enabled"].(bool); ok {
		user.MFAEnabled = v
	}
	if v, ok := respMap["email_verified"].(bool); ok {
		user.EmailVerified = v
	}

	// Handle roles slice.
	if rolesRaw, ok := respMap["roles"]; ok {
		switch roles := rolesRaw.(type) {
		case []string:
			user.Roles = roles
		case []interface{}:
			for _, r := range roles {
				if s, ok := r.(string); ok {
					user.Roles = append(user.Roles, s)
				}
			}
		}
	}

	// Handle groups slice.
	if groupsRaw, ok := respMap["groups"]; ok {
		switch groups := groupsRaw.(type) {
		case []string:
			user.Groups = groups
		case []interface{}:
			for _, g := range groups {
				if s, ok := g.(string); ok {
					user.Groups = append(user.Groups, s)
				}
			}
		}
	}

	// Handle metadata map (arbitrary key-value pairs).
	if metaRaw, ok := respMap["metadata"]; ok {
		switch meta := metaRaw.(type) {
		case map[string]string:
			user.Metadata = meta
		case map[string]interface{}:
			user.Metadata = make(map[string]string, len(meta))
			for k, v := range meta {
				if s, ok := v.(string); ok {
					user.Metadata[k] = s
				}
			}
		}
	}

	// Handle attributes (structured user attributes for ABAC).
	if attrsRaw, ok := respMap["attributes"]; ok {
		if attrsMap, ok := attrsRaw.(map[string]interface{}); ok {
			user.Attributes = &UserAttributes{}
			if v, ok := attrsMap["department"].(string); ok {
				user.Attributes.Department = v
			}
			if v, ok := attrsMap["team_id"].(string); ok {
				user.Attributes.TeamID = v
			}
			if v, ok := attrsMap["location"].(string); ok {
				user.Attributes.Location = v
			}
			if v, ok := attrsMap["cost_center"].(string); ok {
				user.Attributes.CostCenter = v
			}
			if v, ok := attrsMap["employee_id"].(string); ok {
				user.Attributes.EmployeeID = v
			}
			if v, ok := attrsMap["manager"].(string); ok {
				user.Attributes.Manager = v
			}
			if v, ok := attrsMap["classification"].(string); ok {
				user.Attributes.Classification = v
			}
		}
	}

	// Handle timestamps (either string or time.Time).
	if v, ok := respMap["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			user.CreatedAt = t
		}
	} else if v, ok := respMap["created_at"].(time.Time); ok {
		user.CreatedAt = v
	}
	if v, ok := respMap["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			user.UpdatedAt = t
		}
	} else if v, ok := respMap["updated_at"].(time.Time); ok {
		user.UpdatedAt = v
	}
	if v, ok := respMap["last_login_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			user.LastLoginAt = &t
		}
	} else if v, ok := respMap["last_login_at"].(time.Time); ok {
		user.LastLoginAt = &v
	}

	return user, true
}

// isUserSuccessResponse checks if a response indicates success for user operations.
// It handles typed response structs and map responses from REST/gRPC transports.
func isUserSuccessResponse(resp interface{}) bool {
	if resp == nil {
		return true // nil response is considered success for void operations
	}

	// Check for typed response structs.
	switch resp.(type) {
	case *deleteUserResponse, *userRoleResponse, *userGroupResponse, *suspendUserResponse, *unsuspendUserResponse:
		return true
	}

	// Check for map[string]bool response.
	if m, ok := resp.(map[string]bool); ok {
		if success, exists := m["success"]; exists {
			return success
		}
		return true // Map exists but no success key
	}

	// Check for map[string]interface{} response.
	if m, ok := resp.(map[string]interface{}); ok {
		if success, exists := m["success"]; exists {
			if b, ok := success.(bool); ok {
				return b
			}
		}
		return true // Map exists but no success key
	}

	return false
}

// userClient implements the UserClient interface.
type userClient struct {
	transport transport.Transport
}

// NewUserClient creates a new user client.
func NewUserClient(t transport.Transport) UserClient {
	if t == nil {
		return nil
	}
	return &userClient{
		transport: t,
	}
}

// Create creates a new user in a tenant.
func (c *userClient) Create(ctx context.Context, tenantID string, req *CreateUserRequest) (*User, error) {
	if err := validateCreateUserRequest(tenantID, req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.user.Create", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "Create", &createUserRequestInternal{
		TenantID: tenantID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.user.Create", "nil response from server")
	}

	user, ok := convertToUser(resp)
	if !ok {
		return nil, errors.NewInternal("iam.user.Create", "invalid response type")
	}

	return user, nil
}

// Get retrieves a user by ID.
func (c *userClient) Get(ctx context.Context, tenantID, userID string) (*User, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.user.Get", "tenant ID is required")
	}

	if userID == "" {
		return nil, errors.NewInvalidInput("iam.user.Get", "user ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.user.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "Get", &getUserRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.user.Get", "user not found")
	}

	user, ok := convertToUser(resp)
	if !ok {
		return nil, errors.NewInternal("iam.user.Get", "invalid response type")
	}

	return user, nil
}

// GetByEmail retrieves a user by email address.
func (c *userClient) GetByEmail(ctx context.Context, tenantID, email string) (*User, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.user.GetByEmail", "tenant ID is required")
	}

	if email == "" {
		return nil, errors.NewInvalidInput("iam.user.GetByEmail", "email is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.user.GetByEmail", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "GetByEmail", &getUserByEmailRequest{
		TenantID: tenantID,
		Email:    email,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.user.GetByEmail", "user not found")
	}

	user, ok := convertToUser(resp)
	if !ok {
		return nil, errors.NewInternal("iam.user.GetByEmail", "invalid response type")
	}

	return user, nil
}

// Update updates a user.
func (c *userClient) Update(ctx context.Context, tenantID, userID string, req *UpdateUserRequest) (*User, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.user.Update", "tenant ID is required")
	}

	if userID == "" {
		return nil, errors.NewInvalidInput("iam.user.Update", "user ID is required")
	}

	if req == nil {
		return nil, errors.NewInvalidInput("iam.user.Update", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.user.Update", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "Update", &updateUserRequestInternal{
		TenantID: tenantID,
		UserID:   userID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.user.Update", "nil response from server")
	}

	user, ok := convertToUser(resp)
	if !ok {
		return nil, errors.NewInternal("iam.user.Update", "invalid response type")
	}

	return user, nil
}

// Delete deletes a user.
func (c *userClient) Delete(ctx context.Context, tenantID, userID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.user.Delete", "tenant ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.user.Delete", "user ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.user.Delete", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "Delete", &deleteUserRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isUserSuccessResponse(resp) {
		return errors.NewInternal("iam.user.Delete", "delete operation failed")
	}

	return nil
}

// List lists users in a tenant with optional filtering.
func (c *userClient) List(ctx context.Context, tenantID string, req *ListUsersRequest) (*ListUsersResponse, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.user.List", "tenant ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.user.List", "context is required")
	}

	if req == nil {
		req = &ListUsersRequest{}
	}

	resp, err := c.transport.Request(ctx, userServiceName, "List", &listUsersRequestInternal{
		TenantID: tenantID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListUsersResponse{Users: []User{}}, nil
	}

	// If already a *ListUsersResponse, return it directly.
	if listResp, ok := resp.(*ListUsersResponse); ok {
		return listResp, nil
	}

	// Handle map[string]interface{} response from REST/gRPC transports.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.user.List", "invalid response type")
	}

	result := &ListUsersResponse{Users: []User{}}

	// Parse users array.
	if usersRaw, ok := respMap["users"]; ok {
		switch users := usersRaw.(type) {
		case []interface{}:
			for _, u := range users {
				if user, ok := convertToUser(u); ok {
					result.Users = append(result.Users, *user)
				}
			}
		case []*User:
			for _, u := range users {
				if u != nil {
					result.Users = append(result.Users, *u)
				}
			}
		case []User:
			result.Users = users
		}
	}

	// Parse pagination info.
	if paginationRaw, ok := respMap["pagination"]; ok {
		if pagination, ok := paginationRaw.(map[string]interface{}); ok {
			if v, ok := pagination["total_count"].(float64); ok {
				result.Pagination.TotalCount = int(v)
			} else if v, ok := pagination["total_count"].(int32); ok {
				result.Pagination.TotalCount = int(v)
			} else if v, ok := pagination["total_count"].(int64); ok {
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
	} else if v, ok := respMap["total"].(int32); ok {
		result.Pagination.TotalCount = int(v)
	} else if v, ok := respMap["total"].(int64); ok {
		result.Pagination.TotalCount = int(v)
	}

	return result, nil
}

// AssignRole assigns a role to a user.
func (c *userClient) AssignRole(ctx context.Context, tenantID, userID, roleID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.user.AssignRole", "tenant ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.user.AssignRole", "user ID is required")
	}

	if roleID == "" {
		return errors.NewInvalidInput("iam.user.AssignRole", "role ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.user.AssignRole", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "AssignRole", &userRoleRequest{
		TenantID: tenantID,
		UserID:   userID,
		RoleID:   roleID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isUserSuccessResponse(resp) {
		return errors.NewInternal("iam.user.AssignRole", "assign role operation failed")
	}

	return nil
}

// RevokeRole revokes a role from a user.
func (c *userClient) RevokeRole(ctx context.Context, tenantID, userID, roleID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.user.RevokeRole", "tenant ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.user.RevokeRole", "user ID is required")
	}

	if roleID == "" {
		return errors.NewInvalidInput("iam.user.RevokeRole", "role ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.user.RevokeRole", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "RevokeRole", &userRoleRequest{
		TenantID: tenantID,
		UserID:   userID,
		RoleID:   roleID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isUserSuccessResponse(resp) {
		return errors.NewInternal("iam.user.RevokeRole", "revoke role operation failed")
	}

	return nil
}

// AddToGroup adds a user to a group.
func (c *userClient) AddToGroup(ctx context.Context, tenantID, userID, groupID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.user.AddToGroup", "tenant ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.user.AddToGroup", "user ID is required")
	}

	if groupID == "" {
		return errors.NewInvalidInput("iam.user.AddToGroup", "group ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.user.AddToGroup", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "AddToGroup", &userGroupRequest{
		TenantID: tenantID,
		UserID:   userID,
		GroupID:  groupID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isUserSuccessResponse(resp) {
		return errors.NewInternal("iam.user.AddToGroup", "add to group operation failed")
	}

	return nil
}

// RemoveFromGroup removes a user from a group.
func (c *userClient) RemoveFromGroup(ctx context.Context, tenantID, userID, groupID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.user.RemoveFromGroup", "tenant ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.user.RemoveFromGroup", "user ID is required")
	}

	if groupID == "" {
		return errors.NewInvalidInput("iam.user.RemoveFromGroup", "group ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.user.RemoveFromGroup", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "RemoveFromGroup", &userGroupRequest{
		TenantID: tenantID,
		UserID:   userID,
		GroupID:  groupID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isUserSuccessResponse(resp) {
		return errors.NewInternal("iam.user.RemoveFromGroup", "remove from group operation failed")
	}

	return nil
}

// Suspend suspends a user account.
func (c *userClient) Suspend(ctx context.Context, tenantID, userID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.user.Suspend", "tenant ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.user.Suspend", "user ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.user.Suspend", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "Suspend", &suspendUserRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isUserSuccessResponse(resp) {
		return errors.NewInternal("iam.user.Suspend", "suspend operation failed")
	}

	return nil
}

// Unsuspend reactivates a suspended user account.
func (c *userClient) Unsuspend(ctx context.Context, tenantID, userID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.user.Unsuspend", "tenant ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.user.Unsuspend", "user ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.user.Unsuspend", "context is required")
	}

	resp, err := c.transport.Request(ctx, userServiceName, "Unsuspend", &unsuspendUserRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isUserSuccessResponse(resp) {
		return errors.NewInternal("iam.user.Unsuspend", "unsuspend operation failed")
	}

	return nil
}

// Validation functions.

func validateCreateUserRequest(tenantID string, req *CreateUserRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.user.Create", "tenant ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.user.Create", "request is required")
	}
	if req.Username == "" {
		return errors.NewInvalidInput("iam.user.Create", "username is required")
	}
	if req.Email == "" {
		return errors.NewInvalidInput("iam.user.Create", "email is required")
	}
	if req.Password == "" {
		return errors.NewInvalidInput("iam.user.Create", "password is required")
	}
	return nil
}
