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

package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestAuthRoleAssignmentAndVerify tests that role assignment grants correct permissions
func TestAuthRoleAssignmentAndVerify(t *testing.T) {
	_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-verify",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create user with readonly role
	user, err := authManager.CreateUser("verify-user", "password123", []string{auth.RoleNameReadOnly})
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	t.Run("user starts with readonly permissions", func(t *testing.T) {
		// Verify readonly can read keys
		hasRead := roleManager.HasPermission(user.GetRoles(), auth.ResourceKeys, auth.ActionRead)
		assert.True(t, hasRead, "Readonly user should have read permission")

		// Verify readonly cannot write keys
		hasWrite := roleManager.HasPermission(user.GetRoles(), auth.ResourceKeys, auth.ActionWrite)
		assert.False(t, hasWrite, "Readonly user should not have write permission")
	})

	t.Run("assign readwrite role grants write permission", func(t *testing.T) {
		userStore := authManager.GetUserStore()

		// Assign readwrite role
		err := userStore.AssignRole(user.GetID(), auth.RoleNameReadWrite)
		require.NoError(t, err)

		// Refresh user data
		updatedUser := userStore.GetByID(user.GetID())
		require.NotNil(t, updatedUser)

		// Verify now has write permission
		hasWrite := roleManager.HasPermission(updatedUser.GetRoles(), auth.ResourceKeys, auth.ActionWrite)
		assert.True(t, hasWrite, "User with readwrite role should have write permission")
	})
}

// TestAuthRoleRevocationAndVerify tests that role revocation removes permissions
func TestAuthRoleRevocationAndVerify(t *testing.T) {
	_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-revoke",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create user with both readonly and readwrite roles
	user, err := authManager.CreateUser("revoke-user", "password123", []string{auth.RoleNameReadOnly, auth.RoleNameReadWrite})
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()
	userStore := authManager.GetUserStore()

	t.Run("user has write permission before revocation", func(t *testing.T) {
		hasWrite := roleManager.HasPermission(user.GetRoles(), auth.ResourceKeys, auth.ActionWrite)
		assert.True(t, hasWrite, "User should have write permission initially")
	})

	t.Run("revoking readwrite removes write permission", func(t *testing.T) {
		// Revoke readwrite role
		err := userStore.RevokeRole(user.GetID(), auth.RoleNameReadWrite)
		require.NoError(t, err)

		// Refresh user data
		updatedUser := userStore.GetByID(user.GetID())
		require.NotNil(t, updatedUser)

		// Verify write permission is gone
		hasWrite := roleManager.HasPermission(updatedUser.GetRoles(), auth.ResourceKeys, auth.ActionWrite)
		assert.False(t, hasWrite, "User should not have write permission after revocation")

		// But still has read permission
		hasRead := roleManager.HasPermission(updatedUser.GetRoles(), auth.ResourceKeys, auth.ActionRead)
		assert.True(t, hasRead, "User should still have read permission")
	})
}

// TestAuthTokenRevocationAndVerify tests that revoked tokens are rejected
func TestAuthTokenRevocationAndVerify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-token-revoke",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create user and issue token
	_, err = authManager.CreateUser("token-revoke-user", "password123", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)

	userInfo := authManager.GetUserStore().GetByUsername("token-revoke-user")
	token, err := authManager.IssueTokenForUser(ctx, userInfo)
	require.NoError(t, err)
	require.NotEmpty(t, token.AccessToken)

	t.Run("token is valid before revocation", func(t *testing.T) {
		result, err := authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.Valid, "Token should be valid before revocation")
		assert.False(t, result.IsRevoked, "Token should not be marked as revoked")
	})

	t.Run("token is invalid after revocation", func(t *testing.T) {
		// Revoke the token
		err := authManager.RevokeToken(ctx, token.AccessToken)
		require.NoError(t, err)

		// Validate should now show token as revoked
		result, err := authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.False(t, result.Valid, "Token should be invalid after revocation")
		assert.True(t, result.IsRevoked, "Token should be marked as revoked")
	})
}

// TestAuthMultipleRolesVerify tests that users with multiple roles have combined permissions
func TestAuthMultipleRolesVerify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-multi",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	// Test single role vs multiple roles
	singleRoleUser, err := authManager.CreateUser("single-role", "password", []string{auth.RoleNameReadOnly})
	require.NoError(t, err)

	multiRoleUser, err := authManager.CreateUser("multi-role", "password", []string{auth.RoleNameReadOnly, auth.RoleNameReadWrite})
	require.NoError(t, err)

	t.Run("single role user has limited permissions", func(t *testing.T) {
		hasRead := roleManager.HasPermission(singleRoleUser.GetRoles(), auth.ResourceKeys, auth.ActionRead)
		hasWrite := roleManager.HasPermission(singleRoleUser.GetRoles(), auth.ResourceKeys, auth.ActionWrite)

		assert.True(t, hasRead, "Single role user should have read")
		assert.False(t, hasWrite, "Single role user should not have write")
	})

	t.Run("multi role user has combined permissions", func(t *testing.T) {
		hasRead := roleManager.HasPermission(multiRoleUser.GetRoles(), auth.ResourceKeys, auth.ActionRead)
		hasWrite := roleManager.HasPermission(multiRoleUser.GetRoles(), auth.ResourceKeys, auth.ActionWrite)
		hasDelete := roleManager.HasPermission(multiRoleUser.GetRoles(), auth.ResourceKeys, auth.ActionDelete)

		assert.True(t, hasRead, "Multi role user should have read")
		assert.True(t, hasWrite, "Multi role user should have write")
		assert.True(t, hasDelete, "Multi role user should have delete from readwrite")
	})

	_ = ctx // Used for context
}

// TestAuthAdminRoleVerify tests that admin role has all permissions
func TestAuthAdminRoleVerify(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-admin",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	adminUser, err := authManager.CreateUser("admin-verify", "password", []string{auth.RoleNameAdmin})
	require.NoError(t, err)

	// Admin should have all permissions on all resources
	testCases := []struct {
		resource string
		action   string
	}{
		{auth.ResourceKeys, auth.ActionRead},
		{auth.ResourceKeys, auth.ActionWrite},
		{auth.ResourceKeys, auth.ActionDelete},
		{auth.ResourceUsers, auth.ActionAdmin},
		{auth.ResourceCluster, auth.ActionAdmin},
		{auth.ResourceMetrics, auth.ActionRead},
		{auth.ResourceHealth, auth.ActionRead},
	}

	for _, tc := range testCases {
		t.Run(tc.resource+"_"+tc.action, func(t *testing.T) {
			hasPerm := roleManager.HasPermission(adminUser.GetRoles(), tc.resource, tc.action)
			assert.True(t, hasPerm, "Admin should have %s permission on %s", tc.action, tc.resource)
		})
	}
}

// TestAuthUserStoreOperations tests user store CRUD operations
func TestAuthUserStoreOperations(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-store",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	userStore := authManager.GetUserStore()

	t.Run("create and retrieve user", func(t *testing.T) {
		user, err := authManager.CreateUser("store-test", "password", []string{auth.RoleNameReadOnly})
		require.NoError(t, err)

		retrieved := userStore.GetByID(user.GetID())
		require.NotNil(t, retrieved)
		assert.Equal(t, user.GetUsername(), retrieved.GetUsername())
	})

	t.Run("get by username", func(t *testing.T) {
		user, err := authManager.CreateUser("get-by-name", "password", []string{auth.RoleNameReadOnly})
		require.NoError(t, err)

		retrieved := userStore.GetByUsername("get-by-name")
		require.NotNil(t, retrieved)
		assert.Equal(t, user.GetID(), retrieved.GetID())
	})

	t.Run("delete user", func(t *testing.T) {
		user, err := authManager.CreateUser("to-delete", "password", []string{auth.RoleNameReadOnly})
		require.NoError(t, err)

		userStore.DeleteUser(user.GetID())

		retrieved := userStore.GetByID(user.GetID())
		assert.Nil(t, retrieved, "Deleted user should not be retrievable")
	})

	t.Run("list users", func(t *testing.T) {
		// Create a few users
		_, err := authManager.CreateUser("list-test-1", "password", []string{auth.RoleNameReadOnly})
		require.NoError(t, err)
		_, err = authManager.CreateUser("list-test-2", "password", []string{auth.RoleNameReadWrite})
		require.NoError(t, err)

		users := userStore.ListUsers()
		assert.GreaterOrEqual(t, len(users), 2, "Should have at least 2 users")
	})
}

// TestAuthAnonymousAccess tests that anonymous access is properly controlled
func TestAuthAnonymousAccess(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-anon",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	t.Run("anonymous can access health", func(t *testing.T) {
		allowed := roleManager.AllowsAnonymous(auth.ResourceHealth, auth.ActionRead)
		assert.True(t, allowed, "Anonymous should be able to read health")
	})

	t.Run("anonymous can access metrics", func(t *testing.T) {
		allowed := roleManager.AllowsAnonymous(auth.ResourceMetrics, auth.ActionRead)
		assert.True(t, allowed, "Anonymous should be able to read metrics")
	})

	t.Run("anonymous cannot access keys", func(t *testing.T) {
		allowed := roleManager.AllowsAnonymous(auth.ResourceKeys, auth.ActionRead)
		assert.False(t, allowed, "Anonymous should not be able to read keys")
	})

	t.Run("anonymous cannot write health", func(t *testing.T) {
		allowed := roleManager.AllowsAnonymous(auth.ResourceHealth, auth.ActionWrite)
		assert.False(t, allowed, "Anonymous should not be able to write health")
	})
}
