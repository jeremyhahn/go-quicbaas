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

// Package main provides mock implementations for testing.
package main

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
)

// MockRBACClient is a mock implementation of the RBAC client for testing.
type MockRBACClient struct {
	CreateUserFunc func(ctx context.Context, req *rbac.CreateUserRequest) (*rbac.CreateUserResponse, error)
	DeleteUserFunc func(ctx context.Context, req *rbac.DeleteUserRequest) (*rbac.DeleteUserResponse, error)
	ListUsersFunc  func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error)
	CreateRoleFunc func(ctx context.Context, req *rbac.CreateRoleRequest) (*rbac.CreateRoleResponse, error)
	DeleteRoleFunc func(ctx context.Context, req *rbac.DeleteRoleRequest) (*rbac.DeleteRoleResponse, error)
	ListRolesFunc  func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error)
	AssignRoleFunc func(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error)
	RevokeRoleFunc func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error)
}

// CreateUser implements rbac.Client.
func (m *MockRBACClient) CreateUser(ctx context.Context, req *rbac.CreateUserRequest) (*rbac.CreateUserResponse, error) {
	if m.CreateUserFunc != nil {
		return m.CreateUserFunc(ctx, req)
	}
	return &rbac.CreateUserResponse{
		UserID:    "mock-user-id",
		Timestamp: 1234567890000000000,
	}, nil
}

// DeleteUser implements rbac.Client.
func (m *MockRBACClient) DeleteUser(ctx context.Context, req *rbac.DeleteUserRequest) (*rbac.DeleteUserResponse, error) {
	if m.DeleteUserFunc != nil {
		return m.DeleteUserFunc(ctx, req)
	}
	return &rbac.DeleteUserResponse{
		Success: true,
		Message: "User deleted successfully",
	}, nil
}

// ListUsers implements rbac.Client.
func (m *MockRBACClient) ListUsers(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
	if m.ListUsersFunc != nil {
		return m.ListUsersFunc(ctx, req)
	}
	return &rbac.ListUsersResponse{
		Users: []*rbac.UserInfo{
			{
				UserID:      "user-1",
				Username:    "admin",
				Roles:       []string{"admin"},
				CreatedAt:   1234567890000000000,
				LastLoginAt: 1234567890000000000,
			},
		},
		TotalCount: 1,
	}, nil
}

// CreateRole implements rbac.Client.
func (m *MockRBACClient) CreateRole(ctx context.Context, req *rbac.CreateRoleRequest) (*rbac.CreateRoleResponse, error) {
	if m.CreateRoleFunc != nil {
		return m.CreateRoleFunc(ctx, req)
	}
	return &rbac.CreateRoleResponse{
		RoleID:    "mock-role-id",
		Timestamp: 1234567890000000000,
	}, nil
}

// DeleteRole implements rbac.Client.
func (m *MockRBACClient) DeleteRole(ctx context.Context, req *rbac.DeleteRoleRequest) (*rbac.DeleteRoleResponse, error) {
	if m.DeleteRoleFunc != nil {
		return m.DeleteRoleFunc(ctx, req)
	}
	return &rbac.DeleteRoleResponse{
		Success: true,
		Message: "Role deleted successfully",
	}, nil
}

// ListRoles implements rbac.Client.
func (m *MockRBACClient) ListRoles(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
	if m.ListRolesFunc != nil {
		return m.ListRolesFunc(ctx, req)
	}
	return &rbac.ListRolesResponse{
		Roles: []*rbac.RoleInfo{
			{
				RoleID:      "role-1",
				RoleName:    "admin",
				Permissions: []string{"*:*"},
				CreatedAt:   1234567890000000000,
			},
		},
		TotalCount: 1,
	}, nil
}

// AssignRole implements rbac.Client.
func (m *MockRBACClient) AssignRole(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
	if m.AssignRoleFunc != nil {
		return m.AssignRoleFunc(ctx, req)
	}
	return &rbac.AssignRoleResponse{
		Success: true,
		Message: "Role assigned successfully",
	}, nil
}

// RevokeRole implements rbac.Client.
func (m *MockRBACClient) RevokeRole(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
	if m.RevokeRoleFunc != nil {
		return m.RevokeRoleFunc(ctx, req)
	}
	return &rbac.RevokeRoleResponse{
		Success: true,
		Message: "Role revoked successfully",
	}, nil
}

// Ensure MockRBACClient implements rbac.Client at compile time.
var _ rbac.Client = (*MockRBACClient)(nil)
