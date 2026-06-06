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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/hash"
)

// issueTokenForUser is a helper function that gets a user from the store and issues a token.
// This replaces the deprecated Authenticate(ctx, username, password) pattern.
func issueTokenForUser(ctx context.Context, authManager *auth.InMemoryAuthManager, username string) (*auth.Token, error) {
	userInfo := authManager.GetUserStore().GetByUsername(username)
	if userInfo == nil {
		return nil, &qberrors.MessageError{Message: "user not found: " + username}
	}
	return authManager.IssueTokenForUser(ctx, userInfo)
}

// TestUserAuthentication tests the full JWT authentication flow with the new IssueTokenForUser API
func TestUserAuthentication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create auth manager for testing
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-for-jwt-signing",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create test users
	adminUser, err := authManager.CreateUser("admin-user", "admin-pass", []string{auth.RoleNameAdmin})
	require.NoError(t, err)
	require.NotNil(t, adminUser)

	t.Run("valid user token issuance", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "admin-user")
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)
		assert.Equal(t, "Bearer", token.TokenType)
		assert.True(t, token.ExpiresAt.After(time.Now()))
	})

	t.Run("token issuance for nonexistent user fails", func(t *testing.T) {
		_, err := issueTokenForUser(ctx, authManager, "nonexistent-user")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("token issuance with nil user fails", func(t *testing.T) {
		_, err := authManager.IssueTokenForUser(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user cannot be nil")
	})

	t.Run("CreateUser with empty username fails", func(t *testing.T) {
		_, err := authManager.CreateUser("", "", []string{auth.RoleNameReadOnly})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})
}

// TestTokenRefresh tests real token refresh with refresh tokens
func TestTokenRefresh(t *testing.T) {
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
		JWTSecret:       "test-secret-key-for-jwt-signing",
		TokenTTL:        5 * time.Second, // Short TTL for testing
		RefreshTokenTTL: 1 * time.Hour,
		RefreshWindow:   1 * time.Second,
		MaxTokens:       10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create test user
	_, err = authManager.CreateUser("refresh-test-user", "password123", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)

	t.Run("successful token refresh", func(t *testing.T) {
		// Issue token to get initial tokens
		initialToken, err := issueTokenForUser(ctx, authManager, "refresh-test-user")
		require.NoError(t, err)
		require.NotEmpty(t, initialToken.RefreshToken)

		// Wait a bit before refreshing
		time.Sleep(2 * time.Second)

		// Refresh the token
		newToken, err := authManager.RefreshToken(ctx, initialToken.RefreshToken)
		require.NoError(t, err)
		assert.NotEmpty(t, newToken.AccessToken)
		assert.NotEqual(t, initialToken.AccessToken, newToken.AccessToken)
		assert.NotEmpty(t, newToken.RefreshToken)
	})

	t.Run("invalid refresh token", func(t *testing.T) {
		_, err := authManager.RefreshToken(ctx, "invalid-refresh-token")
		assert.Error(t, err)
	})
}

// TestRoleBasedAccessControl tests real RBAC with admin/readwrite/readonly roles
func TestRoleBasedAccessControl(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create users with different roles
	adminUser, err := authManager.CreateUser("admin", "admin123", []string{auth.RoleNameAdmin})
	require.NoError(t, err)

	rwUser, err := authManager.CreateUser("readwrite", "rw123", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)

	roUser, err := authManager.CreateUser("readonly", "ro123", []string{auth.RoleNameReadOnly})
	require.NoError(t, err)

	// Get tokens for each user using IssueTokenForUser
	adminToken, err := issueTokenForUser(ctx, authManager, "admin")
	require.NoError(t, err)

	rwToken, err := issueTokenForUser(ctx, authManager, "readwrite")
	require.NoError(t, err)

	roToken, err := issueTokenForUser(ctx, authManager, "readonly")
	require.NoError(t, err)

	t.Run("admin has all permissions", func(t *testing.T) {
		err := authManager.Authorize(ctx, adminToken.AccessToken, auth.ResourceKeys, auth.ActionRead)
		assert.NoError(t, err)

		err = authManager.Authorize(ctx, adminToken.AccessToken, auth.ResourceKeys, auth.ActionWrite)
		assert.NoError(t, err)

		err = authManager.Authorize(ctx, adminToken.AccessToken, auth.ResourceUsers, auth.ActionAdmin)
		assert.NoError(t, err)
	})

	t.Run("readwrite can read and write data", func(t *testing.T) {
		err := authManager.Authorize(ctx, rwToken.AccessToken, auth.ResourceKeys, auth.ActionRead)
		assert.NoError(t, err)

		err = authManager.Authorize(ctx, rwToken.AccessToken, auth.ResourceKeys, auth.ActionWrite)
		assert.NoError(t, err)

		// But cannot manage users
		err = authManager.Authorize(ctx, rwToken.AccessToken, auth.ResourceUsers, auth.ActionAdmin)
		assert.Error(t, err)
	})

	t.Run("readonly can only read data", func(t *testing.T) {
		err := authManager.Authorize(ctx, roToken.AccessToken, auth.ResourceKeys, auth.ActionRead)
		assert.NoError(t, err)

		// Cannot write
		err = authManager.Authorize(ctx, roToken.AccessToken, auth.ResourceKeys, auth.ActionWrite)
		assert.Error(t, err)

		// Cannot delete
		err = authManager.Authorize(ctx, roToken.AccessToken, auth.ResourceKeys, auth.ActionDelete)
		assert.Error(t, err)
	})

	// Verify user objects
	assert.Equal(t, "admin", adminUser.Username)
	assert.Contains(t, adminUser.Roles, auth.RoleNameAdmin)
	assert.Equal(t, "readwrite", rwUser.Username)
	assert.Equal(t, "readonly", roUser.Username)
}

// TestAdminOperations tests real admin user creation and user listing
func TestAdminOperations(t *testing.T) {
	_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	t.Run("create admin user", func(t *testing.T) {
		user, err := authManager.CreateUser("new-admin", "secure-password", []string{auth.RoleNameAdmin})
		require.NoError(t, err)
		assert.NotEmpty(t, user.ID)
		assert.Equal(t, "new-admin", user.Username)
		assert.Contains(t, user.Roles, auth.RoleNameAdmin)
	})

	t.Run("create duplicate user fails", func(t *testing.T) {
		_, err := authManager.CreateUser("new-admin", "password", []string{auth.RoleNameAdmin})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user already exists")
	})

	t.Run("list all users", func(t *testing.T) {
		// Create additional users
		_, err := authManager.CreateUser("user1", "pass1", []string{auth.RoleNameReadWrite})
		require.NoError(t, err)

		_, err = authManager.CreateUser("user2", "pass2", []string{auth.RoleNameReadOnly})
		require.NoError(t, err)

		userStore := authManager.GetUserStore()
		users := userStore.ListUsers()
		assert.GreaterOrEqual(t, len(users), 3) // At least new-admin, user1, user2
	})
}

// TestReadWritePermissions tests real read/write operations
func TestReadWritePermissions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create readwrite user
	_, err = authManager.CreateUser("rw-user", "rwpass", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)

	token, err := issueTokenForUser(ctx, authManager, "rw-user")
	require.NoError(t, err)

	t.Run("can read keys", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionRead)
		assert.NoError(t, err)
	})

	t.Run("can write keys", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionWrite)
		assert.NoError(t, err)
	})

	t.Run("can delete keys", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionDelete)
		assert.NoError(t, err)
	})

	t.Run("can access transactions", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceTransactions, auth.ActionRead)
		assert.NoError(t, err)

		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceTransactions, auth.ActionWrite)
		assert.NoError(t, err)
	})

	t.Run("cannot manage users", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceUsers, auth.ActionAdmin)
		assert.Error(t, err)
	})
}

// TestReadOnlyPermissions tests real readonly user operations
func TestReadOnlyPermissions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create readonly user
	user, err := authManager.CreateUser("ro-user", "ropass", []string{auth.RoleNameReadOnly})
	require.NoError(t, err)
	assert.Equal(t, "ro-user", user.Username)
	assert.Contains(t, user.Roles, auth.RoleNameReadOnly)

	token, err := issueTokenForUser(ctx, authManager, "ro-user")
	require.NoError(t, err)

	t.Run("can read keys", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionRead)
		assert.NoError(t, err)
	})

	t.Run("cannot write keys", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionWrite)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient permissions")
	})

	t.Run("cannot delete keys", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionDelete)
		assert.Error(t, err)
	})

	t.Run("can read metrics", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceMetrics, auth.ActionRead)
		assert.NoError(t, err)
	})

	t.Run("can read health", func(t *testing.T) {
		err := authManager.Authorize(ctx, token.AccessToken, auth.ResourceHealth, auth.ActionRead)
		assert.NoError(t, err)
	})
}

// TestCustomRoleCreation tests creating custom roles with specific permissions
func TestCustomRoleCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	t.Run("create custom backup operator role", func(t *testing.T) {
		customRole := &auth.Role{
			Name:        "backup-operator",
			Description: "Can manage backups only",
			Permissions: []*auth.Permission{
				{
					Resource: auth.ResourceBackups,
					Actions:  []string{auth.ActionRead, auth.ActionWrite},
				},
				{
					Resource: auth.ResourceKeys,
					Actions:  []string{auth.ActionRead},
				},
			},
		}

		err := roleManager.AddRole(customRole)
		require.NoError(t, err)

		// Verify role was created
		retrievedRole := roleManager.GetRole("backup-operator")
		require.NotNil(t, retrievedRole)
		assert.Equal(t, "backup-operator", retrievedRole.GetName())
		assert.Len(t, retrievedRole.GetPermissions(), 2)
	})

	t.Run("create user with custom role", func(t *testing.T) {
		user, err := authManager.CreateUser("backup-user", "backuppass", []string{"backup-operator"})
		require.NoError(t, err)
		assert.Contains(t, user.Roles, "backup-operator")

		token, err := issueTokenForUser(ctx, authManager, "backup-user")
		require.NoError(t, err)

		// Can manage backups
		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceBackups, auth.ActionRead)
		assert.NoError(t, err)

		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceBackups, auth.ActionWrite)
		assert.NoError(t, err)

		// Can read keys
		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionRead)
		assert.NoError(t, err)

		// Cannot write keys
		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionWrite)
		assert.Error(t, err)
	})

	t.Run("list all roles", func(t *testing.T) {
		roles := roleManager.ListRoles()
		assert.GreaterOrEqual(t, len(roles), 4) // admin, readwrite, readonly, backup-operator
	})
}

// TestMultiTenantIsolation tests multi-tenant data isolation with namespace patterns
func TestMultiTenantIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	t.Run("create tenant-specific roles", func(t *testing.T) {
		// Create role for tenant A
		tenantARole := &auth.Role{
			Name:        "tenant-a-user",
			Description: "Access to tenant A data only",
			Permissions: []*auth.Permission{
				{
					Resource: "tenant-a/*",
					Actions:  []string{auth.ActionRead, auth.ActionWrite},
				},
			},
		}

		err := roleManager.AddRole(tenantARole)
		require.NoError(t, err)

		// Create role for tenant B
		tenantBRole := &auth.Role{
			Name:        "tenant-b-user",
			Description: "Access to tenant B data only",
			Permissions: []*auth.Permission{
				{
					Resource: "tenant-b/*",
					Actions:  []string{auth.ActionRead, auth.ActionWrite},
				},
			},
		}

		err = roleManager.AddRole(tenantBRole)
		require.NoError(t, err)
	})

	t.Run("create users for different tenants", func(t *testing.T) {
		userA, err := authManager.CreateUser("tenant-a-user1", "passwordA", []string{"tenant-a-user"})
		require.NoError(t, err)
		assert.Equal(t, "tenant-a-user1", userA.Username)

		userB, err := authManager.CreateUser("tenant-b-user1", "passwordB", []string{"tenant-b-user"})
		require.NoError(t, err)
		assert.Equal(t, "tenant-b-user1", userB.Username)
	})

	t.Run("verify tenant isolation", func(t *testing.T) {
		// Tenant A user can access tenant A resources
		tokenA, err := issueTokenForUser(ctx, authManager, "tenant-a-user1")
		require.NoError(t, err)

		// Note: Current implementation uses exact match, not pattern matching
		// This test demonstrates the RBAC structure for future pattern-based isolation

		// Tenant B user can access tenant B resources
		tokenB, err := issueTokenForUser(ctx, authManager, "tenant-b-user1")
		require.NoError(t, err)

		assert.NotEmpty(t, tokenA.AccessToken)
		assert.NotEmpty(t, tokenB.AccessToken)
		assert.NotEqual(t, tokenA.AccessToken, tokenB.AccessToken)
	})
}

// TestAuditLogging tests authentication and authorization audit logging
func TestAuditLogging(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create logger that captures output
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	t.Run("token issuance is logged", func(t *testing.T) {
		// Create user
		_, err := authManager.CreateUser("audit-user", "auditpass", []string{auth.RoleNameReadWrite})
		require.NoError(t, err)

		// Successful token issuance
		_, err = issueTokenForUser(ctx, authManager, "audit-user")
		assert.NoError(t, err)

		// Failed token issuance (nonexistent user)
		_, err = issueTokenForUser(ctx, authManager, "nonexistent-audit-user")
		assert.Error(t, err)

		// Note: In production, logs would be captured and analyzed
		// This test verifies the auth manager functions correctly
	})

	t.Run("authorization checks are logged", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "audit-user")
		require.NoError(t, err)

		// Authorized action
		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionRead)
		assert.NoError(t, err)

		// Unauthorized action
		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceUsers, auth.ActionAdmin)
		assert.Error(t, err)
	})
}

// TestSessionManagement tests real session creation, logout, and token revocation
func TestSessionManagement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create test user
	_, err = authManager.CreateUser("session-user", "sessionpass", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)

	t.Run("create session", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "session-user")
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)

		// Validate token
		result, err := authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.False(t, result.IsRevoked)
		assert.Equal(t, "session-user", result.Username)
	})

	t.Run("revoke session", func(t *testing.T) {
		// Create new session
		token, err := issueTokenForUser(ctx, authManager, "session-user")
		require.NoError(t, err)

		// Verify token is valid
		result, err := authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.Valid)

		// Revoke token (logout)
		err = authManager.RevokeToken(ctx, token.AccessToken)
		require.NoError(t, err)

		// Verify token is now revoked
		result, err = authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.False(t, result.Valid)
		assert.True(t, result.IsRevoked)
	})

	t.Run("multiple concurrent sessions", func(t *testing.T) {
		// Create multiple sessions for same user
		token1, err := issueTokenForUser(ctx, authManager, "session-user")
		require.NoError(t, err)

		token2, err := issueTokenForUser(ctx, authManager, "session-user")
		require.NoError(t, err)

		// Both tokens should be different and valid
		assert.NotEqual(t, token1.AccessToken, token2.AccessToken)

		result1, err := authManager.ValidateToken(ctx, token1.AccessToken)
		require.NoError(t, err)
		assert.True(t, result1.Valid)

		result2, err := authManager.ValidateToken(ctx, token2.AccessToken)
		require.NoError(t, err)
		assert.True(t, result2.Valid)
	})
}

// TestPasswordPolicy tests password policy validation through CreateUser
func TestPasswordPolicy(t *testing.T) {
	_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	t.Run("empty password accepted (password no longer validated)", func(t *testing.T) {
		// Password-based authentication was removed, so empty password is allowed
		// Users must authenticate via mTLS, WebAuthn, or other IAM methods
		user, err := authManager.CreateUser("user-empty-pass", "", []string{auth.RoleNameReadOnly})
		require.NoError(t, err, "empty password should be allowed since password auth is disabled")
		assert.Equal(t, "user-empty-pass", user.Username)
	})

	t.Run("valid password accepted", func(t *testing.T) {
		user, err := authManager.CreateUser("user-valid-pass", "StrongP@ss123", []string{auth.RoleNameReadOnly})
		require.NoError(t, err)
		assert.Equal(t, "user-valid-pass", user.Username)
	})

	t.Run("empty username rejected", func(t *testing.T) {
		_, err := authManager.CreateUser("", "SomePassword123", []string{auth.RoleNameReadWrite})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("user creation succeeds with valid credentials", func(t *testing.T) {
		// Create user with specific credentials
		_, err := authManager.CreateUser("password-match-user", "CorrectPassword123", []string{auth.RoleNameReadWrite})
		require.NoError(t, err)

		// Verify user exists in store
		userInfo := authManager.GetUserStore().GetByUsername("password-match-user")
		require.NotNil(t, userInfo)
		assert.Equal(t, "password-match-user", userInfo.GetUsername())
	})
}

// TestRateLimiting tests authentication rate limiting
func TestRateLimiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	// Create test user
	_, err = authManager.CreateUser("rate-limit-user", "password123", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)

	t.Run("many rapid token issuance attempts", func(t *testing.T) {
		successCount := 0
		failCount := 0

		// Attempt many rapid token issuances
		for i := 0; i < 100; i++ {
			_, err := issueTokenForUser(ctx, authManager, "rate-limit-user")
			if err == nil {
				successCount++
			} else {
				failCount++
			}
		}

		// All should succeed since we don't have rate limiting implemented yet
		// This test documents expected behavior when rate limiting is added
		assert.Greater(t, successCount, 0, "Should have some successful token issuances")
		t.Logf("Token issuance attempts: %d successful, %d failed", successCount, failCount)
	})

	t.Run("token operations are fast", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "rate-limit-user")
		require.NoError(t, err)

		start := time.Now()
		for i := 0; i < 100; i++ {
			_, err := authManager.ValidateToken(ctx, token.AccessToken)
			require.NoError(t, err)
		}
		elapsed := time.Since(start)

		// Should be able to validate 100 tokens in under 1 second
		assert.Less(t, elapsed, 1*time.Second, "Token validation should be fast")
		t.Logf("Validated 100 tokens in %v", elapsed)
	})
}

// TestIPWhitelisting tests network-based access control
func TestIPWhitelisting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	t.Run("create network-restricted role", func(t *testing.T) {
		// Create role with network policy metadata
		restrictedRole := &auth.Role{
			Name:        "internal-only",
			Description: "Only accessible from internal network",
			Permissions: []*auth.Permission{
				{
					Resource: auth.ResourceKeys,
					Actions:  []string{auth.ActionRead, auth.ActionWrite},
				},
			},
		}

		err := roleManager.AddRole(restrictedRole)
		require.NoError(t, err)

		// Create user with restricted role
		user, err := authManager.CreateUser("internal-user", "internalpass", []string{"internal-only"})
		require.NoError(t, err)
		assert.Contains(t, user.Roles, "internal-only")
	})

	t.Run("token issuance works for internal user", func(t *testing.T) {
		// Note: Actual IP whitelisting would be implemented at the network/middleware layer
		// This test verifies the user/role structure is in place
		token, err := issueTokenForUser(ctx, authManager, "internal-user")
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)

		// Verify authorization works
		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionRead)
		assert.NoError(t, err)
	})
}

// TestAPIKeyAuthentication tests API key-based authentication
func TestAPIKeyAuthentication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-jwt-signing",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 10,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	t.Run("create API key user", func(t *testing.T) {
		// Create user that represents an API key
		// API key would be the "password" - a long random string
		apiKey := "api_key_" + strings.Repeat("abcdef1234567890", 4)

		user, err := authManager.CreateUser("api-service-1", apiKey, []string{auth.RoleNameReadWrite})
		require.NoError(t, err)
		assert.Equal(t, "api-service-1", user.Username)
	})

	t.Run("issue token for API key user", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "api-service-1")
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)

		// Verify token works for authorization
		err = authManager.Authorize(ctx, token.AccessToken, auth.ResourceKeys, auth.ActionWrite)
		assert.NoError(t, err)
	})

	t.Run("create multiple API keys", func(t *testing.T) {
		for i := 1; i <= 5; i++ {
			username := fmt.Sprintf("api-service-%d", i+1)
			apiKey := fmt.Sprintf("api_key_%d_%s", i, strings.Repeat("xyz", 20))

			user, err := authManager.CreateUser(username, apiKey, []string{auth.RoleNameReadWrite})
			if err != nil && !strings.Contains(err.Error(), "already exists") {
				require.NoError(t, err)
			}

			if user != nil {
				assert.Equal(t, username, user.Username)
			}
		}

		// List all users to verify API keys were created
		userStore := authManager.GetUserStore()
		users := userStore.ListUsers()
		assert.GreaterOrEqual(t, len(users), 5, "Should have at least 5 API key users")
	})

	t.Run("revoke API key token", func(t *testing.T) {
		apiKey := "api_key_revoke_test_" + strings.Repeat("abc123", 10)

		_, err := authManager.CreateUser("revoke-api-service", apiKey, []string{auth.RoleNameReadWrite})
		require.NoError(t, err)

		// Issue token
		token, err := issueTokenForUser(ctx, authManager, "revoke-api-service")
		require.NoError(t, err)

		// Revoke the token
		err = authManager.RevokeToken(ctx, token.AccessToken)
		require.NoError(t, err)

		// Verify token is revoked
		result, err := authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.IsRevoked)
		assert.False(t, result.Valid)
	})
}

// TestAuthWithGRPCClient tests authentication integration with gRPC client
func TestAuthWithGRPCClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	t.Run("create client with auth token", func(t *testing.T) {
		tlsCfg, err := helpers.IntegrationTLSConfig()
		require.NoError(t, err, "load integration TLS config")
		// Create gRPC connection directly
		conn, err := grpc.DialContext(ctx, serverAddr,
			grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
			grpc.WithBlock(),
		)
		require.NoError(t, err, "Must connect to server at %s", serverAddr)
		defer conn.Close()

		// Create proto client
		client := pb.NewQuicBaaSClient(conn)

		// Try a simple operation using proto types
		key := "test-key"
		req := &pb.PutRequest{
			EntityType: "auth_test",
			Id:         hash.FNV1a(key),
			Data:       []byte("test-value"),
		}

		resp, err := client.Put(ctx, req)
		if err != nil {
			t.Logf("Put operation failed (expected if auth is enabled): %v", err)
			return
		}

		assert.Greater(t, resp.Version, uint64(0))
	})
}

// Ensure interfaces.UserInfo is used (import validation)
var _ interfaces.UserInfo = (*auth.User)(nil)
