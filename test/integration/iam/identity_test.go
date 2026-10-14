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
// +build integration

package iam_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// setupTenantForTests creates a test tenant and returns its ID.
func setupTenantForTests(t *testing.T, cluster *testCluster) string {
	tenantID := generateUniqueTenantID()
	createReq := map[string]interface{}{
		"id":   tenantID,
		"name": "Test Tenant for Identity",
	}
	var tenant iam.Tenant
	require.NoError(t, cluster.postJSON("/api/v1/tenants", createReq, &tenant))
	return tenantID
}

// TestUserCRUD tests complete user lifecycle.
func TestUserCRUD(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	email := generateUniqueEmail()

	// Create user
	t.Run("Create", func(t *testing.T) {
		createReq := map[string]interface{}{
			"email":    email,
			"username": "testuser",
			"name":     "Test User",
			"attributes": map[string]interface{}{
				"department": "engineering",
				"level":      "senior",
			},
		}

		var user iam.User
		err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createReq, &user)
		require.NoError(t, err, "Failed to create user")

		assert.Equal(t, email, user.Email)
		assert.Equal(t, "Test User", user.Name)
		assert.Equal(t, tenantID, user.TenantID)
		assert.NotEmpty(t, user.ID)
		assert.NotZero(t, user.Created)
		assert.Equal(t, "engineering", user.Attributes["department"])
	})

	// Get user
	t.Run("Get", func(t *testing.T) {
		var resp struct {
			Users  []iam.User `json:"users"`
			Cursor string     `json:"cursor"`
		}
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), &resp)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Users)

		user := resp.Users[0]
		assert.Equal(t, email, user.Email)
		assert.Equal(t, tenantID, user.TenantID)
	})

	// Update user
	t.Run("Update", func(t *testing.T) {
		var resp struct {
			Users []iam.User `json:"users"`
		}
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), &resp)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Users)

		userID := resp.Users[0].ID
		updateReq := map[string]interface{}{
			"name": "Updated User Name",
			"attributes": map[string]interface{}{
				"department": "platform",
				"level":      "staff",
			},
		}

		var updated iam.User
		err = cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%d", tenantID, userID), updateReq, &updated)
		require.NoError(t, err)

		assert.Equal(t, "Updated User Name", updated.Name)
		assert.Equal(t, "platform", updated.Attributes["department"])
	})

	// Delete user
	t.Run("Delete", func(t *testing.T) {
		var resp struct {
			Users []iam.User `json:"users"`
		}
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), &resp)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Users)

		userID := resp.Users[0].ID
		err = cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%d", tenantID, userID))
		require.NoError(t, err)

		// Verify deletion - use expectNotFound which handles eventual consistency
		err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s/users/%d", tenantID, userID))
		assert.NoError(t, err, "Deleted user should not be found")
	})
}

// TestUserEmailUniqueness verifies email uniqueness within a tenant.
func TestUserEmailUniqueness(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	email := generateUniqueEmail()

	// Create first user
	createReq := map[string]interface{}{
		"email":    email,
		"username": generateUniqueUsername(),
		"name":     "User 1",
	}
	var user1 iam.User
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createReq, &user1)
	require.NoError(t, err)

	// Attempt to create second user with same email
	createReq2 := map[string]interface{}{
		"email":    email,
		"username": generateUniqueUsername(),
		"name":     "User 2",
	}
	var user2 iam.User
	err = cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createReq2, &user2)
	assert.Error(t, err, "Duplicate email should be rejected")
}

// TestUserRoleAssignment tests assigning and revoking roles.
func TestUserRoleAssignment(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Create user
	userReq := map[string]interface{}{
		"email":    generateUniqueEmail(),
		"username": generateUniqueUsername(),
		"name":     "Test User",
	}
	var user iam.User
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), userReq, &user))

	// Create role
	roleReq := map[string]interface{}{
		"id":          "developer",
		"name":        "Developer",
		"permissions": []string{"kv:get", "kv:put"},
	}
	var role iam.Role
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/roles", tenantID), roleReq, &role))

	// Assign role to user
	t.Run("AssignRole", func(t *testing.T) {
		assignReq := map[string]interface{}{
			"roles": []string{"developer"},
		}
		var updated iam.User
		err := cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%d", tenantID, user.ID), assignReq, &updated)
		require.NoError(t, err)

		assert.Contains(t, updated.Roles, "developer")
	})

	// Revoke role
	t.Run("RevokeRole", func(t *testing.T) {
		assignReq := map[string]interface{}{
			"roles": []string{},
		}
		var updated iam.User
		err := cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%d", tenantID, user.ID), assignReq, &updated)
		require.NoError(t, err)

		assert.Empty(t, updated.Roles)
	})
}

// TestRoleCRUD tests complete role lifecycle.
func TestRoleCRUD(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	roleID := "test-role"

	// Create role
	t.Run("Create", func(t *testing.T) {
		createReq := map[string]interface{}{
			"id":          roleID,
			"name":        "Test Role",
			"description": "A test role",
			"permissions": []string{"kv:get", "kv:put", "kv:delete"},
		}

		var role iam.Role
		err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/roles", tenantID), createReq, &role)
		require.NoError(t, err)

		assert.Equal(t, roleID, role.ID)
		assert.Equal(t, "Test Role", role.Name)
		assert.Equal(t, tenantID, role.TenantID)
		assert.Len(t, role.Permissions, 3)
		assert.Contains(t, role.Permissions, "kv:get")
	})

	// Get role
	t.Run("Get", func(t *testing.T) {
		var role iam.Role
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/roles/%s", tenantID, roleID), &role)
		require.NoError(t, err)

		assert.Equal(t, roleID, role.ID)
		assert.Equal(t, "Test Role", role.Name)
	})

	// Update role
	t.Run("Update", func(t *testing.T) {
		updateReq := map[string]interface{}{
			"name":        "Updated Role Name",
			"permissions": []string{"kv:get", "kv:put", "kv:delete", "kv:*"},
		}

		var updated iam.Role
		err := cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/roles/%s", tenantID, roleID), updateReq, &updated)
		require.NoError(t, err)

		assert.Equal(t, "Updated Role Name", updated.Name)
		assert.Len(t, updated.Permissions, 4)
		assert.Contains(t, updated.Permissions, "kv:*")
	})

	// Delete role
	t.Run("Delete", func(t *testing.T) {
		err := cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/roles/%s", tenantID, roleID))
		require.NoError(t, err)

		// Verify deletion - use expectNotFound which handles eventual consistency
		err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s/roles/%s", tenantID, roleID))
		assert.NoError(t, err, "Deleted role should not be found")
	})
}

// TestRoleAssignmentRevocation tests role assignment to users and revocation.
func TestRoleAssignmentRevocation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Create user
	userReq := map[string]interface{}{
		"email":    generateUniqueEmail(),
		"username": generateUniqueUsername(),
		"name":     "Test User",
	}
	var user iam.User
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), userReq, &user))

	// Create multiple roles
	roles := []string{"viewer", "editor", "admin"}
	// Each broker role ID maps to a qrdb-registered colon permission — the
	// qrdb PDP rejects unregistered permission strings at role creation.
	rolePerms := map[string]string{
		"viewer": "kv:get",
		"editor": "kv:put",
		"admin":  "kv:*",
	}
	for _, roleID := range roles {
		roleReq := map[string]interface{}{
			"id":          roleID,
			"name":        roleID,
			"permissions": []string{rolePerms[roleID]},
		}
		var role iam.Role
		require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/roles", tenantID), roleReq, &role))
	}

	// Assign all roles
	t.Run("AssignMultipleRoles", func(t *testing.T) {
		updateReq := map[string]interface{}{
			"roles": roles,
		}
		var updated iam.User
		err := cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%d", tenantID, user.ID), updateReq, &updated)
		require.NoError(t, err)

		assert.Len(t, updated.Roles, 3)
		for _, roleID := range roles {
			assert.Contains(t, updated.Roles, roleID)
		}
	})

	// Revoke one role
	t.Run("RevokeOneRole", func(t *testing.T) {
		updateReq := map[string]interface{}{
			"roles": []string{"viewer", "editor"},
		}
		var updated iam.User
		err := cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%d", tenantID, user.ID), updateReq, &updated)
		require.NoError(t, err)

		assert.Len(t, updated.Roles, 2)
		assert.NotContains(t, updated.Roles, "admin")
	})

	// Revoke all roles
	t.Run("RevokeAllRoles", func(t *testing.T) {
		updateReq := map[string]interface{}{
			"roles": []string{},
		}
		var updated iam.User
		err := cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%d", tenantID, user.ID), updateReq, &updated)
		require.NoError(t, err)

		assert.Empty(t, updated.Roles)
	})
}
