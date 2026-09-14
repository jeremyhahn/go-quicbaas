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

package rbac

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.RBAC"
)

// Client provides RBAC (Role-Based Access Control) operations over roles and
// role bindings.
//
// It has no user verbs. Users belong to go-qrdb and are administered through
// the single IAM surface, Client.Identity().IAM().User(). The
// CreateUser/DeleteUser/ListUsers trio that used to sit here carried a Password
// field for a password login this server does not implement, and dispatched to
// a service-layer auth manager production never wired, so every call failed;
// issue #905 removed it with the rest of the parallel identity model.
type Client interface {
	// CreateRole creates a new RBAC role with permissions.
	CreateRole(ctx context.Context, req *CreateRoleRequest) (*CreateRoleResponse, error)

	// DeleteRole removes an RBAC role from the system.
	DeleteRole(ctx context.Context, req *DeleteRoleRequest) (*DeleteRoleResponse, error)

	// ListRoles returns all RBAC roles with optional pagination.
	ListRoles(ctx context.Context, req *ListRolesRequest) (*ListRolesResponse, error)

	// AssignRole assigns a role to a user.
	AssignRole(ctx context.Context, req *AssignRoleRequest) (*AssignRoleResponse, error)

	// RevokeRole removes a role from a user.
	RevokeRole(ctx context.Context, req *RevokeRoleRequest) (*RevokeRoleResponse, error)
}

// =============================================================================
// Role Types
// =============================================================================

// CreateRoleRequest is a request to create a new RBAC role.
type CreateRoleRequest struct {
	// RoleName is the unique name for the new role.
	RoleName string

	// Permissions is the list of permissions granted by this role.
	Permissions []string
}

// CreateRoleResponse is the response after creating an RBAC role.
type CreateRoleResponse struct {
	// RoleID is the unique identifier for the created role.
	RoleID string

	// Timestamp is the creation timestamp (Unix nanoseconds).
	Timestamp int64
}

// DeleteRoleRequest is a request to delete an RBAC role.
type DeleteRoleRequest struct {
	// RoleName is the name of the role to delete.
	RoleName string
}

// DeleteRoleResponse is the response after deleting an RBAC role.
type DeleteRoleResponse struct {
	// Success indicates whether the deletion was successful.
	Success bool

	// Message contains additional information about the operation.
	Message string
}

// ListRolesRequest is a request to list RBAC roles.
type ListRolesRequest struct {
	// Limit is the maximum number of roles to return.
	Limit uint64

	// Offset is the number of roles to skip.
	Offset uint64
}

// ListRolesResponse is the response containing a list of RBAC roles.
type ListRolesResponse struct {
	// Roles is the list of roles.
	Roles []*RoleInfo

	// TotalCount is the total number of roles available.
	TotalCount uint64
}

// RoleInfo contains information about an RBAC role.
type RoleInfo struct {
	// RoleID is the unique identifier for the role.
	RoleID string

	// RoleName is the role's name.
	RoleName string

	// Permissions is the list of permissions granted by this role.
	Permissions []string

	// CreatedAt is the timestamp when the role was created (Unix nanoseconds).
	CreatedAt int64
}

// =============================================================================
// Assignment Types
// =============================================================================

// AssignRoleRequest is a request to assign a role to a user.
type AssignRoleRequest struct {
	// Username is the username of the user.
	Username string

	// RoleName is the name of the role to assign.
	RoleName string
}

// AssignRoleResponse is the response after assigning a role.
type AssignRoleResponse struct {
	// Success indicates whether the assignment was successful.
	Success bool

	// Message contains additional information about the operation.
	Message string
}

// RevokeRoleRequest is a request to revoke a role from a user.
type RevokeRoleRequest struct {
	// Username is the username of the user.
	Username string

	// RoleName is the name of the role to revoke.
	RoleName string
}

// RevokeRoleResponse is the response after revoking a role.
type RevokeRoleResponse struct {
	// Success indicates whether the revocation was successful.
	Success bool

	// Message contains additional information about the operation.
	Message string
}

// =============================================================================
// Client Implementation
// =============================================================================

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new RBAC client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// CreateRole creates a new RBAC role with permissions.
func (c *client) CreateRole(ctx context.Context, req *CreateRoleRequest) (*CreateRoleResponse, error) {
	if err := validateCreateRoleRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateRole", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("rbac.CreateRole", "nil response from server")
	}

	createResp, ok := resp.(*CreateRoleResponse)
	if !ok {
		return nil, errors.NewInternal("rbac.CreateRole", "invalid response type")
	}

	return createResp, nil
}

// DeleteRole removes an RBAC role from the system.
func (c *client) DeleteRole(ctx context.Context, req *DeleteRoleRequest) (*DeleteRoleResponse, error) {
	if err := validateDeleteRoleRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteRole", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("rbac.DeleteRole", "nil response from server")
	}

	deleteResp, ok := resp.(*DeleteRoleResponse)
	if !ok {
		return nil, errors.NewInternal("rbac.DeleteRole", "invalid response type")
	}

	return deleteResp, nil
}

// ListRoles returns all RBAC roles with optional pagination.
func (c *client) ListRoles(ctx context.Context, req *ListRolesRequest) (*ListRolesResponse, error) {
	if err := validateListRolesRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListRoles", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListRolesResponse{Roles: []*RoleInfo{}}, nil
	}

	listResp, ok := resp.(*ListRolesResponse)
	if !ok {
		return nil, errors.NewInternal("rbac.ListRoles", "invalid response type")
	}

	return listResp, nil
}

// AssignRole assigns a role to a user.
func (c *client) AssignRole(ctx context.Context, req *AssignRoleRequest) (*AssignRoleResponse, error) {
	if err := validateAssignRoleRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "AssignRole", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("rbac.AssignRole", "nil response from server")
	}

	assignResp, ok := resp.(*AssignRoleResponse)
	if !ok {
		return nil, errors.NewInternal("rbac.AssignRole", "invalid response type")
	}

	return assignResp, nil
}

// RevokeRole removes a role from a user.
func (c *client) RevokeRole(ctx context.Context, req *RevokeRoleRequest) (*RevokeRoleResponse, error) {
	if err := validateRevokeRoleRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "RevokeRole", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("rbac.RevokeRole", "nil response from server")
	}

	revokeResp, ok := resp.(*RevokeRoleResponse)
	if !ok {
		return nil, errors.NewInternal("rbac.RevokeRole", "invalid response type")
	}

	return revokeResp, nil
}

// =============================================================================
// Validation Functions
// =============================================================================

func validateCreateRoleRequest(req *CreateRoleRequest) error {
	if req == nil {
		return errors.NewInvalidInput("rbac.CreateRole", "request is required")
	}
	if req.RoleName == "" {
		return errors.NewInvalidInput("rbac.CreateRole", "role name is required")
	}
	return nil
}

func validateDeleteRoleRequest(req *DeleteRoleRequest) error {
	if req == nil {
		return errors.NewInvalidInput("rbac.DeleteRole", "request is required")
	}
	if req.RoleName == "" {
		return errors.NewInvalidInput("rbac.DeleteRole", "role name is required")
	}
	return nil
}

func validateListRolesRequest(req *ListRolesRequest) error {
	if req == nil {
		return errors.NewInvalidInput("rbac.ListRoles", "request is required")
	}
	return nil
}

func validateAssignRoleRequest(req *AssignRoleRequest) error {
	if req == nil {
		return errors.NewInvalidInput("rbac.AssignRole", "request is required")
	}
	if req.Username == "" {
		return errors.NewInvalidInput("rbac.AssignRole", "username is required")
	}
	if req.RoleName == "" {
		return errors.NewInvalidInput("rbac.AssignRole", "role name is required")
	}
	return nil
}

func validateRevokeRoleRequest(req *RevokeRoleRequest) error {
	if req == nil {
		return errors.NewInvalidInput("rbac.RevokeRole", "request is required")
	}
	if req.Username == "" {
		return errors.NewInvalidInput("rbac.RevokeRole", "username is required")
	}
	if req.RoleName == "" {
		return errors.NewInvalidInput("rbac.RevokeRole", "role name is required")
	}
	return nil
}
