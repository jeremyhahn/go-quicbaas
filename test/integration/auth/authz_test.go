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
// These tests verify that the AuthorizationService seam used by every protocol
// handler (REST, gRPC, QUIC, MCP) behaves correctly: allowed requests pass,
// denied requests are refused with a populated decision, batch evaluation works,
// and the service fails after close. The allow/deny decision itself is driven by
// the self-contained authzfake double (no fork role manager); the RBAC decision
// logic is qrdb's responsibility and is covered by qrdb's pkg/auth/rbac suite.
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/test/integration/authzfake"
)

// testAuthzContext provides shared test infrastructure for authorization tests.
type testAuthzContext struct {
	t      *testing.T
	ctx    context.Context
	cancel context.CancelFunc
	logger *logger.Logger
}

// newTestAuthzContext creates a new test context. The authorization decision is
// supplied per scenario via authzfake; nothing here mints a token, because the
// bearer is minted by iam.OIDCProvider in a running server and the in-process
// token manager this used to build was deleted in issue #905.
func newTestAuthzContext(t *testing.T) *testAuthzContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	return &testAuthzContext{
		t:      t,
		ctx:    ctx,
		cancel: cancel,
		logger: log,
	}
}

// Close releases all test resources.
func (tc *testAuthzContext) Close() {
	tc.cancel()
}

// subjectFor builds an authenticated Subject carrying the given roles.
func subjectFor(id, username string, roles []string) interfaces.Subject {
	return interfaces.Subject{
		ID:            id,
		Username:      username,
		Roles:         roles,
		Authenticated: true,
		Groups:        []string{},
		Attributes:    make(map[string]interface{}),
	}
}

// TestAuthorizationServiceIntegration tests the AuthorizationService across
// allow, deny, anonymous-allow and anonymous-deny scenarios.
func TestAuthorizationServiceIntegration(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	t.Run("authorize allowed request returns populated decision", func(t *testing.T) {
		svc := authzfake.New("keys:write")
		defer svc.Close()

		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subjectFor("admin-user", "admin-user", []string{"admin"}),
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
		assert.NotEmpty(t, decision.DecisionID)
		assert.Equal(t, "rbac", decision.Provider)
	})

	t.Run("authorize read action allowed", func(t *testing.T) {
		svc := authzfake.New("keys:read")
		defer svc.Close()

		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subjectFor("readonly-user", "readonly-user", []string{"readonly"}),
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("deny request outside allow-list", func(t *testing.T) {
		svc := authzfake.New("keys:read")
		defer svc.Close()

		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subjectFor("readonly-user2", "readonly-user2", []string{"readonly"}),
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.Reason, "do not have")
	})

	t.Run("deny unauthenticated user for protected resource", func(t *testing.T) {
		svc := authzfake.New("keys:write")
		defer svc.Close()

		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  interfaces.Subject{ID: "anonymous", Authenticated: false},
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.Reason, "authentication required")
	})

	t.Run("allow anonymous access to health endpoint", func(t *testing.T) {
		svc := authzfake.New("health:read")
		defer svc.Close()

		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  interfaces.Subject{ID: "anonymous", Authenticated: false},
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

	// ReadWrite can read, write, delete keys but cannot admin users.
	svc := authzfake.New("keys:read", "keys:write", "keys:delete")
	defer svc.Close()

	subject := subjectFor("batch-user", "batch-user", []string{"readwrite"})

	requests := []*interfaces.AuthorizationRequest{
		{Subject: subject, Resource: interfaces.ResourceKeys, Action: interfaces.ActionRead},
		{Subject: subject, Resource: interfaces.ResourceKeys, Action: interfaces.ActionWrite},
		{Subject: subject, Resource: interfaces.ResourceKeys, Action: interfaces.ActionDelete},
		{Subject: subject, Resource: interfaces.ResourceUsers, Action: interfaces.ActionAdmin},
	}

	decisions, err := svc.BatchAuthorize(tc.ctx, requests)
	require.NoError(t, err)
	require.Len(t, decisions, 4)

	assert.True(t, decisions[0].Allowed, "Should allow read")
	assert.True(t, decisions[1].Allowed, "Should allow write")
	assert.True(t, decisions[2].Allowed, "Should allow delete")
	assert.False(t, decisions[3].Allowed, "Should deny admin on users")
}

// TestAuthorizationWithDifferentResources tests authorization across resource types.
func TestAuthorizationWithDifferentResources(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	adminSubject := subjectFor("res-admin", "res-admin", []string{"admin"})
	rwSubject := subjectFor("res-rw", "res-rw", []string{"readwrite"})
	roSubject := subjectFor("res-ro", "res-ro", []string{"readonly"})

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

	// Admin is granted everything via the "*" wildcard.
	adminSvc := authzfake.New("*:*")
	defer adminSvc.Close()
	for _, resource := range resources {
		t.Run("admin_can_access_"+resource, func(t *testing.T) {
			decision, err := adminSvc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
				Subject:  adminSubject,
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

	rwSvc := authzfake.New(
		"keys:write", "timeseries:write", "vector:write",
		"keys:read", "timeseries:read", "vector:read",
	)
	defer rwSvc.Close()
	for _, resource := range readWriteResources {
		t.Run("readwrite_can_write_"+resource, func(t *testing.T) {
			decision, err := rwSvc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
				Subject:  rwSubject,
				Resource: resource,
				Action:   interfaces.ActionWrite,
			})
			require.NoError(t, err)
			assert.True(t, decision.Allowed, "ReadWrite should have write access to %s", resource)
		})
	}

	roSvc := authzfake.New("keys:read")
	defer roSvc.Close()

	t.Run("readonly_cannot_write_keys", func(t *testing.T) {
		decision, err := roSvc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  roSubject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})

	t.Run("readonly_can_read_keys", func(t *testing.T) {
		decision, err := roSvc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  roSubject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})
}

// TestAuthorizationMetrics verifies that decisions include audit/metric fields.
func TestAuthorizationMetrics(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	svc := authzfake.New("keys:read")
	defer svc.Close()

	decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
		Subject:  subjectFor("metrics-user", "metrics-user", []string{"admin"}),
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	})
	require.NoError(t, err)

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

	svc := authzfake.New("keys:read")
	defer svc.Close()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Authorize(canceledCtx, &interfaces.AuthorizationRequest{
		Subject:  subjectFor("cancel-user", "cancel-user", []string{"admin"}),
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	})
	assert.Error(t, err, "Should return error for canceled context")
}

// TestAuthorizationServiceClose tests that the service fails after close.
func TestAuthorizationServiceClose(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	svc := authzfake.New("keys:read")

	require.NoError(t, svc.Close())

	_, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
		Subject:  subjectFor("test", "test", []string{"admin"}),
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	})
	assert.Error(t, err, "Should return error after close")
}

// TestCustomRoleAuthorization tests a scenario modelled on a narrow custom role.
func TestCustomRoleAuthorization(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	// A "backup-operator" can read/write backups only.
	svc := authzfake.New("backups:read", "backups:write")
	defer svc.Close()

	subject := subjectFor("backup-op", "backup-op", []string{"backup-operator"})

	t.Run("can_read_backups", func(t *testing.T) {
		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceBackups,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("can_write_backups", func(t *testing.T) {
		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceBackups,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("cannot_delete_backups", func(t *testing.T) {
		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceBackups,
			Action:   interfaces.ActionDelete,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})

	t.Run("cannot_access_keys", func(t *testing.T) {
		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})
}

// TestMultipleRolesAuthorization tests a subject holding multiple roles whose
// combined grants permit both read and write but not user administration.
func TestMultipleRolesAuthorization(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	svc := authzfake.New("keys:read", "keys:write", "keys:delete")
	defer svc.Close()

	subject := subjectFor("multi-role", "multi-role", []string{"readonly", "readwrite"})

	t.Run("can_read_from_readonly", func(t *testing.T) {
		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("can_write_from_readwrite", func(t *testing.T) {
		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionWrite,
		})
		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("cannot_admin_users", func(t *testing.T) {
		decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: interfaces.ResourceUsers,
			Action:   interfaces.ActionAdmin,
		})
		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})
}

// TestAuthorizationDecisionAudit verifies decisions contain audit information.
func TestAuthorizationDecisionAudit(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	svc := authzfake.New("keys:write")
	defer svc.Close()

	decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
		Subject:  subjectFor("audit-user", "audit-user", []string{"admin"}),
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionWrite,
		Context: map[string]interface{}{
			"ip_address": "192.168.1.100",
			"user_agent": "test-client/1.0",
		},
	})
	require.NoError(t, err)

	assert.True(t, decision.Allowed)
	assert.NotEmpty(t, decision.DecisionID, "Decision should have unique ID for audit")
	assert.Equal(t, "rbac", decision.Provider, "Provider should be set")
	assert.NotEmpty(t, decision.MatchedPolicy, "Matched policy should be set")
	assert.Greater(t, decision.EvaluationTimeNs, int64(0), "Evaluation time should be recorded")
}

// TestAuthorizationWithEmptyRoles tests behavior when an authenticated subject
// has no roles.
func TestAuthorizationWithEmptyRoles(t *testing.T) {
	tc := newTestAuthzContext(t)
	defer tc.Close()

	svc := authzfake.New("keys:write")
	defer svc.Close()

	subject := interfaces.Subject{
		ID:            "user-no-roles",
		Username:      "noroles",
		Authenticated: true,
		Roles:         []string{},
	}

	decision, err := svc.Authorize(tc.ctx, &interfaces.AuthorizationRequest{
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

	svc := authzfake.New("keys:read")
	defer svc.Close()

	t.Run("nil request", func(t *testing.T) {
		_, err := svc.Authorize(tc.ctx, nil)
		assert.Error(t, err, "Should return error for nil request")
	})

	t.Run("empty batch", func(t *testing.T) {
		_, err := svc.BatchAuthorize(tc.ctx, []*interfaces.AuthorizationRequest{})
		assert.Error(t, err, "Should return error for empty batch")
	})

	t.Run("nil request in batch", func(t *testing.T) {
		requests := []*interfaces.AuthorizationRequest{
			{
				Subject:  subjectFor("test", "test", []string{"admin"}),
				Resource: interfaces.ResourceKeys,
				Action:   interfaces.ActionRead,
			},
			nil,
		}
		_, err := svc.BatchAuthorize(tc.ctx, requests)
		assert.Error(t, err, "Should return error for nil request in batch")
	})
}
