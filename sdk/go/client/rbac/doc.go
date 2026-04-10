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

// Package rbac provides the RBAC (Role-Based Access Control) client for the QuicBaaS SDK.
//
// The RBAC client supports user management, role management, and role assignment
// operations for QuicBaaS's internal authentication system.
//
// # Basic Usage
//
//	client := rbac.NewClient(transport)
//
//	// Create a user
//	resp, err := client.CreateUser(ctx, &rbac.CreateUserRequest{
//	    Username: "alice",
//	    Password: "secret123",
//	    Roles:    []string{"admin"},
//	})
//
//	// List all users
//	listResp, err := client.ListUsers(ctx, &rbac.ListUsersRequest{
//	    Limit: 100,
//	})
//
// # Role Management
//
// The RBAC client supports creating and managing roles with permissions:
//
//	// Create a role
//	roleResp, err := client.CreateRole(ctx, &rbac.CreateRoleRequest{
//	    RoleName:    "readonly",
//	    Permissions: []string{"kv:read", "cluster:status"},
//	})
//
//	// Assign a role to a user
//	assignResp, err := client.AssignRole(ctx, &rbac.AssignRoleRequest{
//	    Username: "alice",
//	    RoleName: "readonly",
//	})
//
//	// Revoke a role from a user
//	revokeResp, err := client.RevokeRole(ctx, &rbac.RevokeRoleRequest{
//	    Username: "alice",
//	    RoleName: "readonly",
//	})
//
// # Available Operations
//
// User management:
//   - CreateUser: Create a new user with initial roles
//   - DeleteUser: Remove a user from the system
//   - ListUsers: List all users with pagination
//
// Role management:
//   - CreateRole: Create a new role with permissions
//   - DeleteRole: Remove a role from the system
//   - ListRoles: List all roles with pagination
//
// Role assignment:
//   - AssignRole: Assign a role to a user
//   - RevokeRole: Revoke a role from a user
package rbac
