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

// Package auth provides integration tests for the unified authorization system.
// These tests verify that authorization works correctly across all protocols
// (REST, gRPC, QUIC, MCP) using the unified AuthorizationService.
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth/providers"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// testAuthzContext provides shared test infrastructure for authorization tests.
type testAuthzContext struct {
	t            *testing.T
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *logger.Logger
	authManager  *auth.InMemoryAuthManager
	roleManager  interfaces.RoleManager
	authzService interfaces.AuthorizationService
	provider     interfaces.AuthorizationProvider
}

// newTestAuthzContext creates a new test context with all authorization components.
func newTestAuthzContext(t *testing.T) *testAuthzContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-for-integration-testing",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       100,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	roleManager := authManager.GetRoleManager()

	// Create RBAC provider
	rbacProvider, err := providers.NewRBACProvider(roleManager, log)
	require.NoError(t, err)

	// Create authorization service
	authzService, err := auth.NewAuthorizationService(rbacProvider, log)
	require.NoError(t, err)

	return &testAuthzContext{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		logger:       log,
		authManager:  authManager,
		roleManager:  roleManager,
		authzService: authzService,
		provider:     rbacProvider,
	}
}

// Close releases all test resources.
func (tc *testAuthzContext) Close() {
	tc.cancel()
	if tc.authzService != nil {
		tc.authzService.Close()
	}
}

// createTestUser creates a user with the given roles and returns the user and token.
// Note: password parameter is ignored as password-based authentication is disabled.
// Users authenticate via mTLS certificates, WebAuthn, or OIDC.
func (tc *testAuthzContext) createTestUser(username string, roles []string) (*auth.User, *auth.Token, error) {
	user, err := tc.authManager.CreateUser(username, "", roles)
	if err != nil {
		return nil, nil, err
	}

	// Get user from store and issue token (replaces deprecated password-based authentication)
	userInfo := tc.authManager.GetUserStore().GetByUsername(username)
	if userInfo == nil {
		return nil, nil, &qberrors.MessageError{Message: "user not found after creation: " + username}
	}

	token, err := tc.authManager.IssueTokenForUser(tc.ctx, userInfo)
	if err != nil {
		return nil, nil, err
	}

	return user, token, nil
}

// buildSubject creates a Subject from a token validation result.
func (tc *testAuthzContext) buildSubject(token string) (*interfaces.Subject, error) {
	result, err := tc.authManager.ValidateToken(tc.ctx, token)
	if err != nil {
		return nil, err
	}

	return &interfaces.Subject{
		ID:            result.UserID,
		Username:      result.Username,
		Roles:         result.Roles,
		Authenticated: result.Valid,
		Groups:        []string{},
		Attributes:    make(map[string]interface{}),
	}, nil
}

// TestAuthorizationServiceIntegration tests the AuthorizationService across different scenarios.
func TestAuthorizationServiceIntegration(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	t.Run("authorize with valid admin token", func(t *testing.T) {
		_, token, err := tc.createTestUser("admin-user", []string{auth.RoleNameAdmin})
		require.NoError(t, err)

		subject, err := tc.buildSubject(token.AccessToken)
		require.NoError(t, err)

		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
		assert.NotEmpty(t, decision.DecisionID)
		assert.Equal(t, "rbac", decision.Provider)
	})

	t.Run("authorize readonly user for read action", func(t *testing.T) {
		_, token, err := tc.createTestUser("readonly-user", []string{auth.RoleNameReadOnly})
		require.NoError(t, err)

		subject, err := tc.buildSubject(token.AccessToken)
		require.NoError(t, err)

		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("deny readonly user for write action", func(t *testing.T) {
		_, token, err := tc.createTestUser("readonly-user2", []string{auth.RoleNameReadOnly})
		require.NoError(t, err)

		subject, err := tc.buildSubject(token.AccessToken)
		require.NoError(t, err)

		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.Reason, "do not have")
	})

	t.Run("deny unauthenticated user for protected resource", func(t *testing.T) {
		subject := interfaces.Subject{
			ID:            "anonymous",
			Authenticated: false,
		}

		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.Reason, "authentication required")
	})

	t.Run("allow anonymous access to health endpoint", func(t *testing.T) {
		subject := interfaces.Subject{
			ID:            "anonymous",
			Authenticated: false,
		}

		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceHealth,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
		assert.Contains(t, decision.Reason, "anonymous access allowed")
	})
}

// TestBatchAuthorization tests batch authorization operations.
func TestBatchAuthorization(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	_, token, err := tc.createTestUser("batch-user", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)

	subject, err := tc.buildSubject(token.AccessToken)
	require.NoError(t, err)

	requests := []*interfaces.AuthorizationRequest{
		{Subject: *subject, Resource: interfaces.ResourceKeys, Action: interfaces.ActionRead},
		{Subject: *subject, Resource: interfaces.ResourceKeys, Action: interfaces.ActionWrite},
		{Subject: *subject, Resource: interfaces.ResourceKeys, Action: interfaces.ActionDelete},
		{Subject: *subject, Resource: interfaces.ResourceUsers, Action: interfaces.ActionAdmin},
	}

	decisions, err := tc.authzService.BatchAuthorize(tc.ctx, requests)
	require.NoError(t, err)
	require.Len(t, decisions, 4)

	// ReadWrite can read, write, delete keys
	assert.True(t, decisions[0].Allowed, "Should allow read")
	assert.True(t, decisions[1].Allowed, "Should allow write")
	assert.True(t, decisions[2].Allowed, "Should allow delete")

	// ReadWrite cannot admin users
	assert.False(t, decisions[3].Allowed, "Should deny admin on users")
}

// TestAuthorizationWithDifferentResources tests authorization across all resource types.
func TestAuthorizationWithDifferentResources(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	// Create users with different roles
	_, adminToken, err := tc.createTestUser("res-admin", []string{auth.RoleNameAdmin})
	require.NoError(t, err)
	adminSubject, err := tc.buildSubject(adminToken.AccessToken)
	require.NoError(t, err)

	_, rwToken, err := tc.createTestUser("res-rw", []string{auth.RoleNameReadWrite})
	require.NoError(t, err)
	rwSubject, err := tc.buildSubject(rwToken.AccessToken)
	require.NoError(t, err)

	_, roToken, err := tc.createTestUser("res-ro", []string{auth.RoleNameReadOnly})
	require.NoError(t, err)
	roSubject, err := tc.buildSubject(roToken.AccessToken)
	require.NoError(t, err)

	resources := []string{
		interfaces.ResourceKeys,
		interfaces.ResourceSecrets,
		interfaces.ResourceKMS,
		interfaces.ResourceCrypto,
		interfaces.ResourceSeal,
		interfaces.ResourceCluster,
		interfaces.ResourceBackups,
		interfaces.ResourceTimeseries,
		interfaces.ResourceVector,
	}

	for _, resource := range resources {
		t.Run("admin_can_access_"+resource, func(t *testing.T) {
			decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
				Subject:  *adminSubject,
				Resource: resource,
				Action:   interfaces.ActionAdmin,
			})
			require.NoError(t, err)
			assert.True(t, decision.Allowed, "Admin should have admin access to %s", resource)
		})
	}

	readWriteResources := []string{
		interfaces.ResourceKeys,
		interfaces.ResourceTimeseries,
		interfaces.ResourceVector,
	}

	for _, resource := range readWriteResources {
		t.Run("readwrite_can_write_"+resource, func(t *testing.T) {
			decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
				Subject:  *rwSubject,
				Resource: resource,
				Action:   interfaces.ActionWrite,
			})
			require.NoError(t, err)
			assert.True(t, decision.Allowed, "ReadWrite should have write access to %s", resource)
		})
	}

	t.Run("readonly_cannot_write_keys", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *roSubject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})

	t.Run("readonly_can_read_keys", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *roSubject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})
}

// TestAuthorizationMetrics verifies that authorization decisions include proper metrics.
func TestAuthorizationMetrics(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	_, token, err := tc.createTestUser("metrics-user", []string{auth.RoleNameAdmin})
	require.NoError(t, err)

	subject, err := tc.buildSubject(token.AccessToken)
	require.NoError(t, err)

	decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
		Subject:  *subject,
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	})
	require.NoError(t, err)

	// Verify decision has all expected fields
	assert.True(t, decision.Allowed)
	assert.NotEmpty(t, decision.DecisionID, "Should have decision ID")
	assert.NotEmpty(t, decision.Provider, "Should have provider")
	assert.NotEmpty(t, decision.MatchedPolicy, "Should have matched policy")
	assert.Greater(t, decision.EvaluationTimeNs, int64(0), "Should have evaluation time")
}

// TestAuthorizationContextCancellation tests that context cancellation is respected.
func TestAuthorizationContextCancellation(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	_, token, err := tc.createTestUser("cancel-user", []string{auth.RoleNameAdmin})
	require.NoError(t, err)

	subject, err := tc.buildSubject(token.AccessToken)
	require.NoError(t, err)

	// Create a canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tc.authzService.Authorize(canceledCtx, &interfaces.AuthorizationRequest{
		Subject:  *subject,
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	})
	assert.Error(t, err, "Should return error for canceled context")
}

// TestAuthorizationServiceClose tests that the service properly handles close.
func TestAuthorizationServiceClose(t *testing.T) {
	tc := newTestAuthzContext(t)

	// Create a separate authz service to close
	provider, err := providers.NewRBACProvider(tc.roleManager, tc.logger)
	require.NoError(t, err)

	service, err := auth.NewAuthorizationService(provider, tc.logger)
	require.NoError(t, err)

	// Close the service
	err = service.Close()
	require.NoError(t, err)

	// Operations should fail after close
	_, err = service.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "test",
			Authenticated: true,
			Roles:         []string{auth.RoleNameAdmin},
		},
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	})
	assert.Error(t, err, "Should return error after close")

	tc.Close()
}

// TestCustomRoleAuthorization tests authorization with custom roles.
func TestCustomRoleAuthorization(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	// Create a custom backup operator role
	customRole := &auth.Role{
		Name:        "backup-operator",
		Description: "Can manage backups only",
		Permissions: []*auth.Permission{
			{
				Resource: interfaces.ResourceBackups,
				Actions:  []string{interfaces.ActionRead, interfaces.ActionWrite},
			},
		},
	}

	err := tc.roleManager.AddRole(customRole)
	require.NoError(t, err)

	_, token, err := tc.createTestUser("backup-op", []string{"backup-operator"})
	require.NoError(t, err)

	subject, err := tc.buildSubject(token.AccessToken)
	require.NoError(t, err)

	t.Run("can_read_backups", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceBackups,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("can_write_backups", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceBackups,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("cannot_delete_backups", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceBackups,
			Action:   interfaces.ActionDelete,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})

	t.Run("cannot_access_keys", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})
}

// TestMultipleRolesAuthorization tests authorization with users having multiple roles.
func TestMultipleRolesAuthorization(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	// Create user with multiple roles
	_, token, err := tc.createTestUser("multi-role", []string{auth.RoleNameReadOnly, auth.RoleNameReadWrite})
	require.NoError(t, err)

	subject, err := tc.buildSubject(token.AccessToken)
	require.NoError(t, err)

	// Should have combined permissions from both roles
	t.Run("can_read_from_readonly", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("can_write_from_readwrite", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("cannot_admin_users", func(t *testing.T) {
		decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  *subject,
			Resource: interfaces.ResourceUsers,
			Action:   interfaces.ActionAdmin,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})
}

// TestAuthorizationDecisionAudit verifies that decisions contain proper audit information.
func TestAuthorizationDecisionAudit(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	_, token, err := tc.createTestUser("audit-user", []string{auth.RoleNameAdmin})
	require.NoError(t, err)

	subject, err := tc.buildSubject(token.AccessToken)
	require.NoError(t, err)

	decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
		Subject:  *subject,
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionWrite,
		Context: map[string]interface{}{
			"ip_address": "192.168.1.100",
			"user_agent": "test-client/1.0",
		},
	})
	require.NoError(t, err)

	// Verify audit-relevant fields
	assert.True(t, decision.Allowed)
	assert.NotEmpty(t, decision.DecisionID, "Decision should have unique ID for audit")
	assert.Equal(t, "rbac", decision.Provider, "Provider should be set")
	assert.NotEmpty(t, decision.MatchedPolicy, "Matched policy should be set")
	assert.Greater(t, decision.EvaluationTimeNs, int64(0), "Evaluation time should be recorded")
}

// TestAuthorizationWithEmptyRoles tests behavior when user has no roles.
func TestAuthorizationWithEmptyRoles(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	subject := interfaces.Subject{
		ID:            "user-no-roles",
		Username:      "noroles",
		Authenticated: true,
		Roles:         []string{},
	}

	decision, err := tc.authzService.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
		Subject:  subject,
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed, "User with no roles should be denied")
	assert.Contains(t, decision.Reason, "no roles")
}

// TestAuthorizationErrorHandling tests error handling in authorization.
func TestAuthorizationErrorHandling(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	t.Run("nil request", func(t *testing.T) {
		_, err := tc.authzService.Authorize(tc.ctx, nil)
		assert.Error(t, err, "Should return error for nil request")
	})

	t.Run("empty batch", func(t *testing.T) {
		_, err := tc.authzService.BatchAuthorize(tc.ctx, []*interfaces.AuthorizationRequest{})
		assert.Error(t, err, "Should return error for empty batch")
	})

	t.Run("nil request in batch", func(t *testing.T) {
		requests := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "test",
					Authenticated: true,
					Roles:         []string{auth.RoleNameAdmin},
				},
				Resource: interfaces.ResourceKeys,
				Action:   interfaces.ActionRead,
			},
			nil,
		}
		_, err := tc.authzService.BatchAuthorize(tc.ctx, requests)
		assert.Error(t, err, "Should return error for nil request in batch")
	})
}
