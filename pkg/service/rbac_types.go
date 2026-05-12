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

// Package service provides RBAC (Role-Based Access Control) types for QuicBaaS's
// internal authentication system. These are separate from the multi-tenant IAM types.
package service

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// RBAC User Types
// =============================================================================

// AuthCreateUserRequest is a request to create a new RBAC user.
type AuthCreateUserRequest struct {
	Username string
	Roles    []string
}

// AuthCreateUserResponse is the response after creating an RBAC user.
type AuthCreateUserResponse struct {
	UserID    string
	Timestamp int64
}

// AuthDeleteUserRequest is a request to delete an RBAC user.
type AuthDeleteUserRequest struct {
	Username string
}

// AuthDeleteUserResponse is the response after deleting an RBAC user.
type AuthDeleteUserResponse struct {
	Success bool
	Message string
}

// AuthListUsersRequest is a request to list RBAC users.
type AuthListUsersRequest struct {
	Limit  uint64
	Offset uint64
}

// AuthListUsersResponse is the response containing a list of RBAC users.
type AuthListUsersResponse struct {
	Users      []*UserInfo
	TotalCount uint64
}

// UserInfo contains information about an RBAC user.
type UserInfo struct {
	UserID      string
	Username    string
	Roles       []string
	CreatedAt   int64
	LastLoginAt int64
}

// =============================================================================
// RBAC Role Types
// =============================================================================

// AuthCreateRoleRequest is a request to create a new RBAC role.
type AuthCreateRoleRequest struct {
	RoleName    string
	Permissions []string
}

// AuthCreateRoleResponse is the response after creating an RBAC role.
type AuthCreateRoleResponse struct {
	RoleID    string
	Timestamp int64
}

// AuthDeleteRoleRequest is a request to delete an RBAC role.
type AuthDeleteRoleRequest struct {
	RoleName string
}

// AuthDeleteRoleResponse is the response after deleting an RBAC role.
type AuthDeleteRoleResponse struct {
	Success bool
	Message string
}

// AuthListRolesRequest is a request to list RBAC roles.
type AuthListRolesRequest struct {
	Limit  uint64
	Offset uint64
}

// AuthListRolesResponse is the response containing a list of RBAC roles.
type AuthListRolesResponse struct {
	Roles      []*RoleInfo
	TotalCount uint64
}

// RoleInfo contains information about an RBAC role.
type RoleInfo struct {
	RoleID      string
	RoleName    string
	Permissions []string
	CreatedAt   int64
}

// =============================================================================
// RBAC Assignment Types
// =============================================================================

// AuthAssignRoleRequest is a request to assign a role to a user.
type AuthAssignRoleRequest struct {
	Username string
	RoleName string
}

// AuthAssignRoleResponse is the response after assigning a role.
type AuthAssignRoleResponse struct {
	Success bool
	Message string
}

// AuthRevokeRoleRequest is a request to revoke a role from a user.
type AuthRevokeRoleRequest struct {
	Username string
	RoleName string
}

// AuthRevokeRoleResponse is the response after revoking a role.
type AuthRevokeRoleResponse struct {
	Success bool
	Message string
}

// =============================================================================
// Service Infrastructure Types
// =============================================================================

// DAOFactory provides data access object creation with KVStore access.
type DAOFactory interface {
	// GetKVStore returns a KVStore for the given shard.
	GetKVStore(shardID uint64) interfaces.KVStore
}

// iamServiceProvider wraps IAM service functionality.
type iamServiceProvider struct {
	// Placeholder for IAM service integration
}

// daoFactory is the concrete implementation of DAOFactory.
type daoFactory struct {
	kvStoreProvider func(shardID uint64) interfaces.KVStore
}

// GetKVStore returns a KVStore for the given shard.
func (f *daoFactory) GetKVStore(shardID uint64) interfaces.KVStore {
	if f.kvStoreProvider == nil {
		return nil
	}
	return f.kvStoreProvider(shardID)
}

// =============================================================================
// Type Aliases for Backwards Compatibility
// =============================================================================

// User request/response type aliases.
type CreateUserRequest = AuthCreateUserRequest
type CreateUserResponse = AuthCreateUserResponse
type DeleteUserRequest = AuthDeleteUserRequest
type DeleteUserResponse = AuthDeleteUserResponse
type ListUsersRequest = AuthListUsersRequest
type ListUsersResponse = AuthListUsersResponse

// Role request/response type aliases.
type AssignRoleRequest = AuthAssignRoleRequest
type AssignRoleResponse = AuthAssignRoleResponse
type RevokeRoleRequest = AuthRevokeRoleRequest
type RevokeRoleResponse = AuthRevokeRoleResponse
