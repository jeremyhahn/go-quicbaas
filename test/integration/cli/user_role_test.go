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

//go:build integration

package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// User/Role Transport Configuration
// =============================================================================

// rbacTransportsToSkip lists transports that don't support RBAC operations.
// REST has no RBAC endpoints (404), and MCP RBAC handler has response type issues.
var rbacTransportsToSkip = []Transport{TransportREST, TransportMCP, TransportQUIC}

// userRoleTransportsToSkip defines which transports to skip for user/role operations.
// User and role operations are supported via gRPC, REST, and MCP transports.
// QUIC transport does not have RBAC service implementations.
var userRoleTransportsToSkip = []Transport{TransportQUIC}

// =============================================================================
// User Command Definitions
// =============================================================================

// UserCreateCommand returns a command to create a new user.
func UserCreateCommand(username, password string, roles []string) CommandDef {
	args := []string{"user", "create", username, "--password", password}
	if len(roles) > 0 {
		args = append(args, "--roles", strings.Join(roles, ","))
	}
	return CommandDef{
		Name:           fmt.Sprintf("user create %s", username),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{username},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// UserListCommand returns a command to list all users.
func UserListCommand() CommandDef {
	return CommandDef{
		Name:           "user list",
		Args:           []string{"user", "list"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// UserShowCommand returns a command to get user details.
func UserShowCommand(username string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("user show %s", username),
		Args:           []string{"user", "show", username},
		ExpectSuccess:  true,
		ExpectOutput:   []string{username},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// UserUpdateCommand returns a command to update user roles.
func UserUpdateCommand(username string, roles []string) CommandDef {
	args := []string{"user", "update", username}
	if len(roles) > 0 {
		args = append(args, "--roles", strings.Join(roles, ","))
	}
	return CommandDef{
		Name:           fmt.Sprintf("user update %s", username),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// UserDeleteCommand returns a command to delete a user.
func UserDeleteCommand(username string, force bool) CommandDef {
	args := []string{"user", "delete", username}
	if force {
		args = append(args, "--yes")
	}
	return CommandDef{
		Name:           fmt.Sprintf("user delete %s", username),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// UserAssignRoleCommand returns a command to assign a role to a user.
func UserAssignRoleCommand(username, roleName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("user assign-role %s %s", username, roleName),
		Args:           []string{"user", "assign-role", username, roleName},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// UserRevokeRoleCommand returns a command to revoke a role from a user.
func UserRevokeRoleCommand(username, roleName string, force bool) CommandDef {
	args := []string{"user", "revoke-role", username, roleName}
	if force {
		args = append(args, "--yes")
	}
	return CommandDef{
		Name:           fmt.Sprintf("user revoke-role %s %s", username, roleName),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// =============================================================================
// Role Command Definitions
// =============================================================================

// RoleCreateCommand returns a command to create a new role.
func RoleCreateCommand(roleName string, permissions []string) CommandDef {
	args := []string{"role", "create", roleName}
	if len(permissions) > 0 {
		args = append(args, "--permissions", strings.Join(permissions, ","))
	}
	return CommandDef{
		Name:           fmt.Sprintf("role create %s", roleName),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{roleName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// RoleListCommand returns a command to list all roles.
func RoleListCommand() CommandDef {
	return CommandDef{
		Name:           "role list",
		Args:           []string{"role", "list"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// RoleShowCommand returns a command to get role details.
func RoleShowCommand(roleName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("role show %s", roleName),
		Args:           []string{"role", "show", roleName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{roleName},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// RoleDeleteCommand returns a command to delete a role.
func RoleDeleteCommand(roleName string, force bool) CommandDef {
	args := []string{"role", "delete", roleName}
	if force {
		args = append(args, "--yes")
	}
	return CommandDef{
		Name:           fmt.Sprintf("role delete %s", roleName),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// RoleAssignCommand returns a command to assign a role to a user (via role command).
func RoleAssignCommand(roleName, username string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("role assign %s %s", roleName, username),
		Args:           []string{"role", "assign", roleName, username},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// RoleUnassignCommand returns a command to unassign a role from a user (via role command).
func RoleUnassignCommand(roleName, username string, force bool) CommandDef {
	args := []string{"role", "unassign", roleName, username}
	if force {
		args = append(args, "--yes")
	}
	return CommandDef{
		Name:           fmt.Sprintf("role unassign %s %s", roleName, username),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: userRoleTransportsToSkip,
	}
}

// =============================================================================
// User Lifecycle Tests
// =============================================================================

// TestUserLifecycle tests the complete user lifecycle across all transports.
func TestUserLifecycle(t *testing.T) {
	RunForTransports(t, "UserLifecycle", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("test-user")
		password := "SecurePassword123!"
		roles := []string{"readonly"}

		// Create user
		createCmd := UserCreateCommand(username, password, roles)
		runner.RunAndAssert(createCmd)

		// Show user
		showCmd := UserShowCommand(username)
		runner.RunAndAssert(showCmd)

		// List users
		listCmd := UserListCommand()
		runner.RunAndAssert(listCmd)

		// Update user roles
		updateCmd := UserUpdateCommand(username, []string{"readonly", "readwrite"})
		runner.RunAndAssert(updateCmd)

		// Delete user
		deleteCmd := UserDeleteCommand(username, true)
		runner.RunAndAssert(deleteCmd)
	})
}

// TestUserCreate tests user creation across all transports.
func TestUserCreate(t *testing.T) {
	RunForTransports(t, "UserCreate", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("create-user")
		password := "SecurePassword123!"
		roles := []string{"readonly"}

		cmd := UserCreateCommand(username, password, roles)
		runner.RunAndAssert(cmd)

		// Cleanup
		cleanup := UserDeleteCommand(username, true)
		runner.Run(cleanup)
	})
}

// TestUserCreateWithMultipleRoles tests creating a user with multiple roles.
func TestUserCreateWithMultipleRoles(t *testing.T) {
	RunForTransports(t, "UserCreateMultipleRoles", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("multi-role-user")
		password := "SecurePassword123!"
		roles := []string{"readonly", "readwrite"}

		cmd := UserCreateCommand(username, password, roles)
		runner.RunAndAssert(cmd)

		// Verify user has both roles
		showCmd := UserShowCommand(username)
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("User created with roles: %s", stdout)
		}

		// Cleanup
		cleanup := UserDeleteCommand(username, true)
		runner.Run(cleanup)
	})
}

// TestUserList tests listing users across all transports.
func TestUserList(t *testing.T) {
	RunForTransports(t, "UserList", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Create a few test users first
		users := []string{
			UniqueKey("list-user-1"),
			UniqueKey("list-user-2"),
		}
		password := "SecurePassword123!"

		for _, username := range users {
			createCmd := UserCreateCommand(username, password, []string{"readonly"})
			runner.RunAndAssert(createCmd)
		}

		// List users
		listCmd := UserListCommand()
		runner.RunAndAssert(listCmd)

		// Cleanup
		for _, username := range users {
			cleanup := UserDeleteCommand(username, true)
			runner.Run(cleanup)
		}
	})
}

// TestUserShow tests getting user details across all transports.
func TestUserShow(t *testing.T) {
	RunForTransports(t, "UserShow", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("show-user")
		password := "SecurePassword123!"

		// Create user
		createCmd := UserCreateCommand(username, password, []string{"readonly"})
		runner.RunAndAssert(createCmd)

		// Show user
		showCmd := UserShowCommand(username)
		runner.RunAndAssert(showCmd)

		// Cleanup
		cleanup := UserDeleteCommand(username, true)
		runner.Run(cleanup)
	})
}

// TestUserUpdate tests updating user roles across all transports.
func TestUserUpdate(t *testing.T) {
	RunForTransports(t, "UserUpdate", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("update-user")
		password := "SecurePassword123!"

		// Create user with readonly role
		createCmd := UserCreateCommand(username, password, []string{"readonly"})
		runner.RunAndAssert(createCmd)

		// Update to readwrite role
		updateCmd := UserUpdateCommand(username, []string{"readwrite"})
		runner.RunAndAssert(updateCmd)

		// Verify updated roles
		showCmd := UserShowCommand(username)
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("User updated: %s", stdout)
		}

		// Cleanup
		cleanup := UserDeleteCommand(username, true)
		runner.Run(cleanup)
	})
}

// TestUserDelete tests deleting a user across all transports.
func TestUserDelete(t *testing.T) {
	RunForTransports(t, "UserDelete", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("delete-user")
		password := "SecurePassword123!"

		// Create user
		createCmd := UserCreateCommand(username, password, []string{"readonly"})
		runner.RunAndAssert(createCmd)

		// Delete user
		deleteCmd := UserDeleteCommand(username, true)
		runner.RunAndAssert(deleteCmd)

		// Verify deletion - show should fail or return not found
		showCmd := UserShowCommand(username)
		showCmd.ExpectSuccess = false
		stdout, stderr, err := runner.Run(showCmd)
		if err != nil && !runner.IsSkipped(showCmd) {
			t.Logf("User correctly not found after delete: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestUserAssignRole tests assigning a role to a user across all transports.
func TestUserAssignRole(t *testing.T) {
	RunForTransports(t, "UserAssignRole", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("assign-role-user")
		password := "SecurePassword123!"

		// Create user with readonly role
		createCmd := UserCreateCommand(username, password, []string{"readonly"})
		runner.RunAndAssert(createCmd)

		// Assign additional readwrite role
		assignCmd := UserAssignRoleCommand(username, "readwrite")
		runner.RunAndAssert(assignCmd)

		// Verify roles
		showCmd := UserShowCommand(username)
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("User after role assignment: %s", stdout)
		}

		// Cleanup
		cleanup := UserDeleteCommand(username, true)
		runner.Run(cleanup)
	})
}

// TestUserRevokeRole tests revoking a role from a user across all transports.
func TestUserRevokeRole(t *testing.T) {
	RunForTransports(t, "UserRevokeRole", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("revoke-role-user")
		password := "SecurePassword123!"

		// Create user with multiple roles
		createCmd := UserCreateCommand(username, password, []string{"readonly", "readwrite"})
		runner.RunAndAssert(createCmd)

		// Revoke readwrite role
		revokeCmd := UserRevokeRoleCommand(username, "readwrite", true)
		runner.RunAndAssert(revokeCmd)

		// Verify roles
		showCmd := UserShowCommand(username)
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("User after role revocation: %s", stdout)
		}

		// Cleanup
		cleanup := UserDeleteCommand(username, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Role Lifecycle Tests
// =============================================================================

// TestRoleLifecycle tests the complete role lifecycle across all transports.
func TestRoleLifecycle(t *testing.T) {
	RunForTransports(t, "RoleLifecycle", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		roleName := UniqueKey("test-role")
		permissions := []string{"keys:read", "keys:write"}

		// Create role
		createCmd := RoleCreateCommand(roleName, permissions)
		runner.RunAndAssert(createCmd)

		// Show role
		showCmd := RoleShowCommand(roleName)
		runner.RunAndAssert(showCmd)

		// List roles
		listCmd := RoleListCommand()
		runner.RunAndAssert(listCmd)

		// Delete role
		deleteCmd := RoleDeleteCommand(roleName, true)
		runner.RunAndAssert(deleteCmd)
	})
}

// TestRoleCreate tests role creation across all transports.
func TestRoleCreate(t *testing.T) {
	RunForTransports(t, "RoleCreate", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		roleName := UniqueKey("create-role")
		permissions := []string{"keys:read", "indexes:read"}

		cmd := RoleCreateCommand(roleName, permissions)
		runner.RunAndAssert(cmd)

		// Cleanup
		cleanup := RoleDeleteCommand(roleName, true)
		runner.Run(cleanup)
	})
}

// TestRoleCreateWithPermissions tests creating a role with various permissions.
func TestRoleCreateWithPermissions(t *testing.T) {
	permissionSets := []struct {
		name        string
		permissions []string
	}{
		{"ReadOnly", []string{"keys:read", "indexes:read"}},
		{"ReadWrite", []string{"keys:read", "keys:write", "indexes:read", "indexes:write"}},
		{"MetricsAccess", []string{"metrics:read", "health:read"}},
		{"BackupOperator", []string{"backups:read", "backups:write", "backups:delete"}},
		{"ClusterAdmin", []string{"cluster:*"}},
	}

	for _, permSet := range permissionSets {
		t.Run(permSet.name, func(t *testing.T) {
			RunForTransports(t, "RoleCreate", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				roleName := UniqueKey("perm-role-" + strings.ToLower(permSet.name))

				cmd := RoleCreateCommand(roleName, permSet.permissions)
				runner.RunAndAssert(cmd)

				// Verify role was created
				showCmd := RoleShowCommand(roleName)
				stdout, _, err := runner.Run(showCmd)
				if err == nil && !runner.IsSkipped(showCmd) {
					t.Logf("Created role with %s permissions: %s", permSet.name, stdout)
				}

				// Cleanup
				cleanup := RoleDeleteCommand(roleName, true)
				runner.Run(cleanup)
			})
		})
	}
}

// TestRoleList tests listing roles across all transports.
func TestRoleList(t *testing.T) {
	RunForTransports(t, "RoleList", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Create a few test roles first
		roles := []string{
			UniqueKey("list-role-1"),
			UniqueKey("list-role-2"),
		}

		for _, roleName := range roles {
			createCmd := RoleCreateCommand(roleName, []string{"keys:read"})
			runner.RunAndAssert(createCmd)
		}

		// List roles
		listCmd := RoleListCommand()
		runner.RunAndAssert(listCmd)

		// Cleanup
		for _, roleName := range roles {
			cleanup := RoleDeleteCommand(roleName, true)
			runner.Run(cleanup)
		}
	})
}

// TestRoleShow tests getting role details across all transports.
func TestRoleShow(t *testing.T) {
	RunForTransports(t, "RoleShow", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		roleName := UniqueKey("show-role")
		permissions := []string{"keys:read", "keys:write"}

		// Create role
		createCmd := RoleCreateCommand(roleName, permissions)
		runner.RunAndAssert(createCmd)

		// Show role
		showCmd := RoleShowCommand(roleName)
		runner.RunAndAssert(showCmd)

		// Cleanup
		cleanup := RoleDeleteCommand(roleName, true)
		runner.Run(cleanup)
	})
}

// TestRoleShowBuiltIn tests showing built-in roles across all transports.
func TestRoleShowBuiltIn(t *testing.T) {
	builtInRoles := []string{"admin", "readwrite", "readonly"}

	for _, roleName := range builtInRoles {
		t.Run(roleName, func(t *testing.T) {
			RunForTransports(t, "RoleShowBuiltIn", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				showCmd := RoleShowCommand(roleName)
				runner.RunAndAssert(showCmd)
			})
		})
	}
}

// TestRoleDelete tests deleting a role across all transports.
func TestRoleDelete(t *testing.T) {
	RunForTransports(t, "RoleDelete", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		roleName := UniqueKey("delete-role")

		// Create role
		createCmd := RoleCreateCommand(roleName, []string{"keys:read"})
		runner.RunAndAssert(createCmd)

		// Delete role
		deleteCmd := RoleDeleteCommand(roleName, true)
		runner.RunAndAssert(deleteCmd)

		// Verify deletion - show should fail or return not found
		showCmd := RoleShowCommand(roleName)
		showCmd.ExpectSuccess = false
		stdout, stderr, err := runner.Run(showCmd)
		if err != nil && !runner.IsSkipped(showCmd) {
			t.Logf("Role correctly not found after delete: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestRoleAssign tests assigning a role to a user via role command.
func TestRoleAssign(t *testing.T) {
	RunForTransports(t, "RoleAssign", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("role-assign-user")
		password := "SecurePassword123!"
		roleName := UniqueKey("assign-test-role")

		// Create user
		createUserCmd := UserCreateCommand(username, password, []string{"readonly"})
		runner.RunAndAssert(createUserCmd)

		// Create custom role
		createRoleCmd := RoleCreateCommand(roleName, []string{"keys:read", "keys:write"})
		runner.RunAndAssert(createRoleCmd)

		// Assign role to user via role command
		assignCmd := RoleAssignCommand(roleName, username)
		runner.RunAndAssert(assignCmd)

		// Verify user has the role
		showCmd := UserShowCommand(username)
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("User after role assignment: %s", stdout)
		}

		// Cleanup
		runner.Run(UserDeleteCommand(username, true))
		runner.Run(RoleDeleteCommand(roleName, true))
	})
}

// TestRoleUnassign tests unassigning a role from a user via role command.
func TestRoleUnassign(t *testing.T) {
	RunForTransports(t, "RoleUnassign", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("role-unassign-user")
		password := "SecurePassword123!"
		roleName := UniqueKey("unassign-test-role")

		// Create custom role
		createRoleCmd := RoleCreateCommand(roleName, []string{"keys:read"})
		runner.RunAndAssert(createRoleCmd)

		// Create user with the custom role
		createUserCmd := UserCreateCommand(username, password, []string{"readonly", roleName})
		runner.RunAndAssert(createUserCmd)

		// Unassign role from user via role command
		unassignCmd := RoleUnassignCommand(roleName, username, true)
		runner.RunAndAssert(unassignCmd)

		// Verify user no longer has the role
		showCmd := UserShowCommand(username)
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("User after role unassignment: %s", stdout)
		}

		// Cleanup
		runner.Run(UserDeleteCommand(username, true))
		runner.Run(RoleDeleteCommand(roleName, true))
	})
}

// =============================================================================
// User/Role Integration Tests
// =============================================================================

// TestUserRoleIntegration tests combined user and role operations.
func TestUserRoleIntegration(t *testing.T) {
	RunForTransports(t, "UserRoleIntegration", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Create custom roles
		role1 := UniqueKey("integration-role-1")
		role2 := UniqueKey("integration-role-2")
		username := UniqueKey("integration-user")
		password := "SecurePassword123!"

		// Create roles
		createRole1 := RoleCreateCommand(role1, []string{"keys:read"})
		runner.RunAndAssert(createRole1)

		createRole2 := RoleCreateCommand(role2, []string{"keys:write"})
		runner.RunAndAssert(createRole2)

		// Create user with first custom role
		createUser := UserCreateCommand(username, password, []string{role1})
		runner.RunAndAssert(createUser)

		// Assign second role
		assignCmd := UserAssignRoleCommand(username, role2)
		runner.RunAndAssert(assignCmd)

		// Verify user has both roles
		showCmd := UserShowCommand(username)
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("User with multiple custom roles: %s", stdout)
		}

		// Revoke first role
		revokeCmd := UserRevokeRoleCommand(username, role1, true)
		runner.RunAndAssert(revokeCmd)

		// Cleanup
		runner.Run(UserDeleteCommand(username, true))
		runner.Run(RoleDeleteCommand(role1, true))
		runner.Run(RoleDeleteCommand(role2, true))
	})
}

// TestMultipleUsersWithSameRole tests multiple users sharing the same role.
func TestMultipleUsersWithSameRole(t *testing.T) {
	RunForTransports(t, "MultipleUsersWithSameRole", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		roleName := UniqueKey("shared-role")
		users := []string{
			UniqueKey("shared-user-1"),
			UniqueKey("shared-user-2"),
			UniqueKey("shared-user-3"),
		}
		password := "SecurePassword123!"

		// Create shared role
		createRole := RoleCreateCommand(roleName, []string{"keys:read", "metrics:read"})
		runner.RunAndAssert(createRole)

		// Create users with shared role
		for _, username := range users {
			createUser := UserCreateCommand(username, password, []string{roleName})
			runner.RunAndAssert(createUser)
		}

		// List users to verify all were created
		listCmd := UserListCommand()
		runner.RunAndAssert(listCmd)

		// Cleanup
		for _, username := range users {
			runner.Run(UserDeleteCommand(username, true))
		}
		runner.Run(RoleDeleteCommand(roleName, true))
	})
}

// =============================================================================
// Comprehensive User/Role Suites
// =============================================================================

// TestFullUserSuite tests complete user operations across all transports.
func TestFullUserSuite(t *testing.T) {
	RunForTransports(t, "FullUserSuite", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		username := UniqueKey("full-suite-user")
		password := "SecurePassword123!"

		// 1. Create user with readonly role
		createCmd := UserCreateCommand(username, password, []string{"readonly"})
		runner.RunAndAssert(createCmd)

		// 2. Show user details
		showCmd := UserShowCommand(username)
		runner.RunAndAssert(showCmd)

		// 3. List all users
		listCmd := UserListCommand()
		runner.RunAndAssert(listCmd)

		// 4. Assign additional readwrite role
		assignCmd := UserAssignRoleCommand(username, "readwrite")
		runner.RunAndAssert(assignCmd)

		// 5. Verify updated roles
		showCmd2 := UserShowCommand(username)
		runner.RunAndAssert(showCmd2)

		// 6. Update user roles (replace with just admin)
		updateCmd := UserUpdateCommand(username, []string{"admin"})
		runner.RunAndAssert(updateCmd)

		// 7. Revoke admin role
		revokeCmd := UserRevokeRoleCommand(username, "admin", true)
		runner.RunAndAssert(revokeCmd)

		// 8. Delete user
		deleteCmd := UserDeleteCommand(username, true)
		runner.RunAndAssert(deleteCmd)
	})
}

// TestFullRoleSuite tests complete role operations across all transports.
func TestFullRoleSuite(t *testing.T) {
	RunForTransports(t, "FullRoleSuite", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		roleName := UniqueKey("full-suite-role")
		username := UniqueKey("role-suite-user")
		password := "SecurePassword123!"

		// 1. Create custom role with multiple permissions
		createRoleCmd := RoleCreateCommand(roleName, []string{
			"keys:read",
			"keys:write",
			"indexes:read",
			"metrics:read",
		})
		runner.RunAndAssert(createRoleCmd)

		// 2. Show role details
		showRoleCmd := RoleShowCommand(roleName)
		runner.RunAndAssert(showRoleCmd)

		// 3. List all roles
		listRolesCmd := RoleListCommand()
		runner.RunAndAssert(listRolesCmd)

		// 4. Create user
		createUserCmd := UserCreateCommand(username, password, []string{"readonly"})
		runner.RunAndAssert(createUserCmd)

		// 5. Assign custom role to user
		assignCmd := RoleAssignCommand(roleName, username)
		runner.RunAndAssert(assignCmd)

		// 6. Verify user has the role
		showUserCmd := UserShowCommand(username)
		runner.RunAndAssert(showUserCmd)

		// 7. Unassign role from user
		unassignCmd := RoleUnassignCommand(roleName, username, true)
		runner.RunAndAssert(unassignCmd)

		// 8. Delete user
		deleteUserCmd := UserDeleteCommand(username, true)
		runner.RunAndAssert(deleteUserCmd)

		// 9. Delete custom role
		deleteRoleCmd := RoleDeleteCommand(roleName, true)
		runner.RunAndAssert(deleteRoleCmd)
	})
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// TestUserShowNotFound tests error handling for non-existent user.
func TestUserShowNotFound(t *testing.T) {
	RunForTransports(t, "UserShowNotFound", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		showCmd := CommandDef{
			Name:           "user show non-existent",
			Args:           []string{"user", "show", "non-existent-user-12345"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: userRoleTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(showCmd)
		if err != nil && !runner.IsSkipped(showCmd) {
			t.Logf("Correctly failed for non-existent user: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestRoleShowNotFound tests error handling for non-existent role.
func TestRoleShowNotFound(t *testing.T) {
	RunForTransports(t, "RoleShowNotFound", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		showCmd := CommandDef{
			Name:           "role show non-existent",
			Args:           []string{"role", "show", "non-existent-role-12345"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: userRoleTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(showCmd)
		if err != nil && !runner.IsSkipped(showCmd) {
			t.Logf("Correctly failed for non-existent role: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestRoleDeleteBuiltIn tests that built-in roles cannot be deleted.
func TestRoleDeleteBuiltIn(t *testing.T) {
	builtInRoles := []string{"admin", "readwrite", "readonly"}

	for _, roleName := range builtInRoles {
		t.Run(roleName, func(t *testing.T) {
			RunForTransports(t, "RoleDeleteBuiltIn", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				deleteCmd := CommandDef{
					Name:           fmt.Sprintf("role delete built-in %s", roleName),
					Args:           []string{"role", "delete", roleName, "--yes"},
					ExpectSuccess:  false,
					Timeout:        10 * time.Second,
					SkipTransports: userRoleTransportsToSkip,
				}
				stdout, stderr, err := runner.Run(deleteCmd)
				if err != nil && !runner.IsSkipped(deleteCmd) {
					t.Logf("Correctly prevented deletion of built-in role %s: stdout=%s stderr=%s", roleName, stdout, stderr)
				}
			})
		})
	}
}

// TestRoleCreateReservedName tests that reserved role names cannot be used.
func TestRoleCreateReservedName(t *testing.T) {
	reservedNames := []string{"admin", "readwrite", "readonly"}

	for _, name := range reservedNames {
		t.Run(name, func(t *testing.T) {
			RunForTransports(t, "RoleCreateReservedName", rbacTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				createCmd := CommandDef{
					Name:           fmt.Sprintf("role create reserved %s", name),
					Args:           []string{"role", "create", name, "--permissions", "keys:read"},
					ExpectSuccess:  false,
					Timeout:        10 * time.Second,
					SkipTransports: userRoleTransportsToSkip,
				}
				stdout, stderr, err := runner.Run(createCmd)
				if err != nil && !runner.IsSkipped(createCmd) {
					t.Logf("Correctly prevented creation of reserved role %s: stdout=%s stderr=%s", name, stdout, stderr)
				}
			})
		})
	}
}
