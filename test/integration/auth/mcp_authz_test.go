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

	"github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/test/integration/authzfake"
)

// authManagerAdapter wraps auth.InMemoryAuthManager to satisfy interfaces.AuthManager.
type authManagerAdapter struct {
	manager *auth.InMemoryAuthManager
}

// IssueTokenForUser issues JWT tokens for a user who has been authenticated
// via mTLS certificate, FIDO2/WebAuthn, or other IAM provider.
func (a *authManagerAdapter) IssueTokenForUser(ctx context.Context, user interfaces.UserInfo) (interface{}, error) {
	return a.manager.IssueTokenForUser(ctx, user)
}

// Authorize wraps the concrete Authorize method.
func (a *authManagerAdapter) Authorize(ctx context.Context, token, resource, action string) error {
	return a.manager.Authorize(ctx, token, resource, action)
}

// RefreshToken wraps the concrete RefreshToken to return interface{}.
func (a *authManagerAdapter) RefreshToken(ctx context.Context, refreshToken string) (interface{}, error) {
	return a.manager.RefreshToken(ctx, refreshToken)
}

// ValidateToken wraps the concrete ValidateToken.
func (a *authManagerAdapter) ValidateToken(ctx context.Context, token string) (interface{}, error) {
	return a.manager.ValidateToken(ctx, token)
}

// RevokeToken wraps the concrete RevokeToken method.
func (a *authManagerAdapter) RevokeToken(ctx context.Context, token string) error {
	return a.manager.RevokeToken(ctx, token)
}

// CreateUser wraps the concrete CreateUser to return interface{}.
func (a *authManagerAdapter) CreateUser(username string, roles []string) (interface{}, error) {
	return a.manager.CreateUser(username, roles)
}

// GetUserStore returns the user store.
func (a *authManagerAdapter) GetUserStore() interfaces.UserStore {
	return a.manager.GetUserStore()
}

// testMCPContext provides infrastructure for MCP authorization tests.
type testMCPContext struct {
	t            *testing.T
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *logger.Logger
	authManager  *auth.InMemoryAuthManager
	authzService interfaces.AuthorizationService
	authzChecker *mcp.AuthorizationChecker
}

// newTestMCPContext creates a new test context for MCP authorization tests.
func newTestMCPContext(t *testing.T, allowed ...string) *testMCPContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-mcp-secret-key-for-integration-testing",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       100,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	authzService := authzfake.New(allowed...)

	// Create adapter to satisfy interfaces.AuthManager
	authAdapter := &authManagerAdapter{manager: authManager}

	// Create MCP authorization checker
	authzChecker := mcp.NewAuthorizationChecker(authzService, authAdapter)

	return &testMCPContext{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		logger:       log,
		authManager:  authManager,
		authzService: authzService,
		authzChecker: authzChecker,
	}
}

// Close releases test resources.
func (tc *testMCPContext) Close() {
	tc.cancel()
	if tc.authzService != nil {
		tc.authzService.Close()
	}
}

// createTestUser creates a user and returns the token.
// Note: password parameter removed as password-based authentication is disabled.
// Users authenticate via mTLS certificates, WebAuthn, or OIDC.
func (tc *testMCPContext) createTestUser(username string, roles []string) (string, error) {
	_, err := tc.authManager.CreateUser(username, roles)
	if err != nil {
		return "", err
	}

	userInfo := tc.authManager.GetUserStore().GetByUsername(username)
	token, err := tc.authManager.IssueTokenForUser(tc.ctx, userInfo)
	if err != nil {
		return "", err
	}

	return token.AccessToken, nil
}

// TestMCPPublicToolsNoAuth tests that public tools bypass authorization.
func TestMCPPublicToolsNoAuth(t *testing.T) {
	tc := newTestMCPContext(t, "*:*")
	defer tc.Close()

	publicTools := []string{
		"qrdb_health",
		"quicbaas_version",
		"quicbaas_liveness",
	}

	for _, tool := range publicTools {
		t.Run(tool+"_is_public", func(t *testing.T) {
			// Public tools should pass without token
			err := tc.authzChecker.CheckToolAuthorization(tc.ctx, tool, "")
			assert.Nil(t, err, "Public tool %s should not require auth", tool)
		})
	}
}

// TestMCPProtectedToolsRequireToken tests that protected tools require a token.
func TestMCPProtectedToolsRequireToken(t *testing.T) {
	tc := newTestMCPContext(t, "*:*")
	defer tc.Close()

	protectedTools := []string{
		"qrdb_get",
		"qrdb_put",
		"qrdb_delete",
		"qrdb_list",
		"quicbaas_query",
		"quicbaas_secrets_get",
		"quicbaas_kms_get_key",
		"quicbaas_cluster_status",
	}

	for _, tool := range protectedTools {
		t.Run(tool+"_requires_token", func(t *testing.T) {
			authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, tool, "")

			require.NotNil(t, authErr, "Protected tool %s should require auth", tool)
			assert.Equal(t, mcp.AuthErrorMissingToken, authErr.Type)
			assert.Contains(t, authErr.Message, "authentication required")
		})
	}
}

// TestMCPAuthErrorMissingToken tests AuthErrorMissingToken for missing token.
func TestMCPAuthErrorMissingToken(t *testing.T) {
	tc := newTestMCPContext(t, "*:*")
	defer tc.Close()

	authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_get", "")

	require.NotNil(t, authErr)
	assert.Equal(t, mcp.AuthErrorMissingToken, authErr.Type)
	assert.Contains(t, authErr.Message, "authentication required")
}

// TestMCPAuthErrorInvalidToken tests AuthErrorInvalidToken for invalid token.
func TestMCPAuthErrorInvalidToken(t *testing.T) {
	tc := newTestMCPContext(t, "*:*")
	defer tc.Close()

	authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_get", "invalid-token-12345")

	require.NotNil(t, authErr)
	assert.Equal(t, mcp.AuthErrorInvalidToken, authErr.Type)
	assert.Contains(t, authErr.Message, "invalid token")
}

// TestMCPAuthErrorPermissionDenied tests AuthErrorPermissionDenied for denied access.
func TestMCPAuthErrorPermissionDenied(t *testing.T) {
	tc := newTestMCPContext(t, "keys:read")
	defer tc.Close()

	// Create readonly user
	token, err := tc.createTestUser("readonly-mcp", []string{"readonly"})
	require.NoError(t, err)

	// Readonly user trying to use write tool
	authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_put", token)

	require.NotNil(t, authErr)
	assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	assert.Contains(t, authErr.Message, "permission denied")
}

// TestMCPAdminRoleAccess tests that admin role has full access to all tools.
func TestMCPAdminRoleAccess(t *testing.T) {
	tc := newTestMCPContext(t, "*:*")
	defer tc.Close()

	token, err := tc.createTestUser("admin-mcp", []string{"admin"})
	require.NoError(t, err)

	adminTools := []string{
		"qrdb_get",
		"qrdb_put",
		"qrdb_delete",
		"qrdb_list",
		"quicbaas_query",
		"quicbaas_secrets_get",
		"quicbaas_secrets_store",
		"quicbaas_secrets_delete",
		"quicbaas_kms_get_key",
		"quicbaas_kms_create_key",
		"quicbaas_kms_delete_key",
		"quicbaas_crypto_encrypt",
		"quicbaas_crypto_decrypt",
		"quicbaas_seal_status",
		"quicbaas_unseal",
		"quicbaas_cluster_status",
		"quicbaas_add_node",
		"qrdb_user_create",
		"qrdb_role_create",
		"quicbaas_backup_create",
		"quicbaas_backup_delete",
	}

	for _, tool := range adminTools {
		t.Run("admin_can_"+tool, func(t *testing.T) {
			authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, tool, token)
			assert.Nil(t, authErr, "Admin should have access to %s", tool)
		})
	}
}

// TestMCPReadWriteRoleAccess tests readwrite role permissions.
func TestMCPReadWriteRoleAccess(t *testing.T) {
	tc := newTestMCPContext(t, "keys:read", "keys:write", "keys:delete")
	defer tc.Close()

	token, err := tc.createTestUser("readwrite-mcp", []string{"readwrite"})
	require.NoError(t, err)

	t.Run("can_read_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_get", token)
		assert.Nil(t, authErr)
	})

	t.Run("can_write_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_put", token)
		assert.Nil(t, authErr)
	})

	t.Run("can_delete_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_delete", token)
		assert.Nil(t, authErr)
	})

	t.Run("cannot_admin_cluster", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_add_node", token)
		require.NotNil(t, authErr)
		assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	})

	t.Run("cannot_manage_users", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_user_create", token)
		require.NotNil(t, authErr)
		assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	})
}

// TestMCPReadOnlyRoleAccess tests readonly role permissions.
func TestMCPReadOnlyRoleAccess(t *testing.T) {
	tc := newTestMCPContext(t, "keys:read")
	defer tc.Close()

	token, err := tc.createTestUser("readonly-mcp-2", []string{"readonly"})
	require.NoError(t, err)

	t.Run("can_read_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_get", token)
		assert.Nil(t, authErr)
	})

	t.Run("can_list_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_list", token)
		assert.Nil(t, authErr)
	})

	t.Run("can_query_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_query", token)
		assert.Nil(t, authErr)
	})

	t.Run("cannot_write_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_put", token)
		require.NotNil(t, authErr)
		assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	})

	t.Run("cannot_delete_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_delete", token)
		require.NotNil(t, authErr)
		assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	})
}

// TestMCPToolToResourceMapping tests tool name to resource/action mapping.
func TestMCPToolToResourceMapping(t *testing.T) {
	testCases := []struct {
		toolName         string
		expectedResource string
		expectedAction   string
		description      string
	}{
		// KV Operations
		{"qrdb_get", interfaces.ResourceKeys, interfaces.ActionRead, "get -> keys:read"},
		{"qrdb_put", interfaces.ResourceKeys, interfaces.ActionWrite, "put -> keys:write"},
		{"qrdb_delete", interfaces.ResourceKeys, interfaces.ActionDelete, "delete -> keys:delete"},
		{"qrdb_list", interfaces.ResourceKeys, interfaces.ActionRead, "list -> keys:read"},
		{"quicbaas_query", interfaces.ResourceKeys, interfaces.ActionRead, "query -> keys:read"},
		{"quicbaas_batch_get", interfaces.ResourceKeys, interfaces.ActionRead, "batch_get -> keys:read"},
		{"quicbaas_batch_put", interfaces.ResourceKeys, interfaces.ActionWrite, "batch_put -> keys:write"},
		{"quicbaas_batch_delete", interfaces.ResourceKeys, interfaces.ActionDelete, "batch_delete -> keys:delete"},

		// Transaction Operations
		{"quicbaas_transaction", interfaces.ResourceTransactions, interfaces.ActionWrite, "transaction -> transactions:write"},

		// Cluster Operations
		{"quicbaas_cluster_status", interfaces.ResourceCluster, interfaces.ActionRead, "cluster_status -> cluster:read"},
		{"quicbaas_get_leader", interfaces.ResourceCluster, interfaces.ActionRead, "get_leader -> cluster:read"},
		{"quicbaas_add_node", interfaces.ResourceCluster, interfaces.ActionAdmin, "add_node -> cluster:admin"},
		{"quicbaas_remove_node", interfaces.ResourceCluster, interfaces.ActionAdmin, "remove_node -> cluster:admin"},

		// Backup Operations
		{"quicbaas_backup_create", interfaces.ResourceBackups, interfaces.ActionWrite, "backup_create -> backups:write"},
		{"quicbaas_backup_list", interfaces.ResourceBackups, interfaces.ActionRead, "backup_list -> backups:read"},
		{"quicbaas_backup_delete", interfaces.ResourceBackups, interfaces.ActionDelete, "backup_delete -> backups:delete"},

		// User Management
		{"qrdb_user_create", interfaces.ResourceUsers, interfaces.ActionWrite, "user_create -> users:write"},
		{"qrdb_user_delete", interfaces.ResourceUsers, interfaces.ActionDelete, "user_delete -> users:delete"},
		{"qrdb_user_list", interfaces.ResourceUsers, interfaces.ActionRead, "user_list -> users:read"},

		// Role Management
		{"qrdb_role_create", interfaces.ResourceRoles, interfaces.ActionWrite, "role_create -> roles:write"},
		{"qrdb_role_delete", interfaces.ResourceRoles, interfaces.ActionDelete, "role_delete -> roles:delete"},
		{"qrdb_role_list", interfaces.ResourceRoles, interfaces.ActionRead, "role_list -> roles:read"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			resource, action := mcp.GetToolResourceActionForTest(tc.toolName)
			assert.Equal(t, tc.expectedResource, resource, "Resource mismatch for tool %s", tc.toolName)
			assert.Equal(t, tc.expectedAction, action, "Action mismatch for tool %s", tc.toolName)
		})
	}
}

// TestMCPRevokedTokenRejected tests that revoked tokens are rejected.
func TestMCPRevokedTokenRejected(t *testing.T) {
	tc := newTestMCPContext(t, "keys:read")
	defer tc.Close()

	token, err := tc.createTestUser("revoke-mcp", []string{"admin"})
	require.NoError(t, err)

	// Verify token works initially
	authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_get", token)
	assert.Nil(t, authErr)

	// Revoke the token
	err = tc.authManager.RevokeToken(tc.ctx, token)
	require.NoError(t, err)

	// Verify token is now rejected
	authErr = tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_get", token)
	require.NotNil(t, authErr)
	assert.Equal(t, mcp.AuthErrorInvalidToken, authErr.Type)
}

// TestMCPCustomRoleAuthorization tests authorization with custom roles.
func TestMCPCustomRoleAuthorization(t *testing.T) {
	tc := newTestMCPContext(t, "secrets:read", "secrets:write", "secrets:delete")
	defer tc.Close()

	// The server's authzfake allow-list grants secrets read/write/delete only,
	// modelling a narrow custom "secrets-manager" role.
	token, err := tc.createTestUser("secrets-only-mcp", []string{"secrets-manager"})
	require.NoError(t, err)

	t.Run("can_read_secrets", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_secrets_get", token)
		assert.Nil(t, authErr)
	})

	t.Run("can_write_secrets", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_secrets_store", token)
		assert.Nil(t, authErr)
	})

	t.Run("can_delete_secrets", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_secrets_delete", token)
		assert.Nil(t, authErr)
	})

	t.Run("cannot_read_keys", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_get", token)
		require.NotNil(t, authErr)
		assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	})

	t.Run("cannot_access_kms", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_kms_get_key", token)
		require.NotNil(t, authErr)
		assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	})
}

// TestMCPMultipleRolesAuthorization tests authorization with multiple roles.
func TestMCPMultipleRolesAuthorization(t *testing.T) {
	tc := newTestMCPContext(t, "keys:read", "keys:write")
	defer tc.Close()

	token, err := tc.createTestUser("multi-role-mcp", []string{"readonly", "readwrite"})
	require.NoError(t, err)

	t.Run("can_read_from_readonly", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_get", token)
		assert.Nil(t, authErr)
	})

	t.Run("can_write_from_readwrite", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "qrdb_put", token)
		assert.Nil(t, authErr)
	})

	t.Run("cannot_admin_cluster", func(t *testing.T) {
		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_add_node", token)
		require.NotNil(t, authErr)
		assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	})
}

// TestMCPIsPublicTool tests the IsPublicTool function.
func TestMCPIsPublicTool(t *testing.T) {
	publicTools := []string{
		"qrdb_health",
		"quicbaas_version",
		"quicbaas_liveness",
	}

	for _, tool := range publicTools {
		t.Run(tool+"_is_public", func(t *testing.T) {
			assert.True(t, mcp.IsPublicTool(tool), "%s should be public", tool)
		})
	}

	protectedTools := []string{
		"qrdb_get",
		"qrdb_put",
		"qrdb_delete",
		"quicbaas_secrets_get",
		"quicbaas_kms_get_key",
	}

	for _, tool := range protectedTools {
		t.Run(tool+"_is_protected", func(t *testing.T) {
			assert.False(t, mcp.IsPublicTool(tool), "%s should be protected", tool)
		})
	}
}

// TestMCPUnknownToolDefaultsToAdmin tests that unknown tools require admin access.
func TestMCPUnknownToolDefaultsToAdmin(t *testing.T) {
	// Unknown tools default to the cluster:admin permission. A non-admin server
	// (allow-list without cluster:admin) denies them; an admin server (full
	// wildcard) allows them.
	t.Run("unknown_tool_denied_for_non_admin", func(t *testing.T) {
		tc := newTestMCPContext(t, "keys:read", "keys:write")
		defer tc.Close()

		nonAdminToken, err := tc.createTestUser("non-admin-mcp", []string{"readwrite"})
		require.NoError(t, err)

		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_unknown_tool", nonAdminToken)
		require.NotNil(t, authErr)
		assert.Equal(t, mcp.AuthErrorPermissionDenied, authErr.Type)
	})

	t.Run("unknown_tool_allowed_for_admin", func(t *testing.T) {
		tc := newTestMCPContext(t, "*:*")
		defer tc.Close()

		adminToken, err := tc.createTestUser("admin-mcp-2", []string{"admin"})
		require.NoError(t, err)

		authErr := tc.authzChecker.CheckToolAuthorization(tc.ctx, "quicbaas_unknown_tool", adminToken)
		assert.Nil(t, authErr)
	})
}

// TestMCPNilAuthServiceDisablesAuth tests that nil auth service disables authorization.
func TestMCPNilAuthServiceDisablesAuth(t *testing.T) {
	// Create checker with nil services
	checker := mcp.NewAuthorizationChecker(nil, nil)

	// All tools should pass when auth is disabled
	protectedTools := []string{
		"qrdb_get",
		"qrdb_put",
		"qrdb_delete",
		"quicbaas_admin_operation",
	}

	for _, tool := range protectedTools {
		t.Run(tool+"_allowed_when_auth_disabled", func(t *testing.T) {
			authErr := checker.CheckToolAuthorization(context.Background(), tool, "")
			assert.Nil(t, authErr, "Tool %s should be allowed when auth is disabled", tool)
		})
	}
}
