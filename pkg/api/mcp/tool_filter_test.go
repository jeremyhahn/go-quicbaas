//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// buildTestToolRegistry creates a minimal ToolRegistry with known tools for testing.
func buildTestToolRegistry() *ToolRegistry {
	r := &ToolRegistry{
		tools: []ToolDefinition{
			{Name: "quicbaas_health", Description: "Health check"},
			{Name: "quicbaas_kms_get_key", Description: "Get KMS key"},
			{Name: "quicbaas_kms_create_key", Description: "Create KMS key"},
			{Name: "quicbaas_secrets_get", Description: "Get secret"},
			{Name: "quicbaas_secrets_store", Description: "Store secret"},
			{Name: "quicbaas_get", Description: "Get KV"},
			{Name: "quicbaas_put", Description: "Put KV"},
			{Name: "quicbaas_cluster_status", Description: "Cluster status"},
		},
	}
	return r
}

func TestFilterToolsByAuthorization_UnauthenticatedSeesAllTools(t *testing.T) {
	registry := buildTestToolRegistry()
	all := registry.ListTools()
	assert.Equal(t, 8, len(all), "expected all 8 tools from the test registry")
}

func TestFilterToolsByAuthorization_FullPermissions(t *testing.T) {
	registry := buildTestToolRegistry()

	mockAuthSvc := new(MockAuthorizationService)
	mockAuthMgr := new(MockAuthManager)

	// Token is valid
	mockAuthMgr.On("ValidateToken", mock.Anything, "full-access-token").Return(
		&MockTokenValidationResult{
			Valid:    true,
			UserID:   "admin-001",
			Username: "admin",
			Roles:    []string{"admin"},
		}, nil,
	)

	// All authorization checks succeed
	mockAuthSvc.On("Authorize", mock.Anything, mock.AnythingOfType("*interfaces.AuthorizationRequest")).Return(
		&interfaces.AuthorizationDecision{Allowed: true}, nil,
	)

	checker := NewAuthorizationChecker(mockAuthSvc, mockAuthMgr)
	ctx := context.Background()

	filtered := registry.FilterToolsByAuthorization(ctx, checker, "full-access-token")
	assert.Equal(t, 8, len(filtered), "fully authorized user should see all tools")

	// Verify public tools are in the result
	names := toolNames(filtered)
	assert.Contains(t, names, "quicbaas_health")
}

func TestFilterToolsByAuthorization_KMSReadOnly(t *testing.T) {
	registry := buildTestToolRegistry()

	mockAuthSvc := new(MockAuthorizationService)
	mockAuthMgr := new(MockAuthManager)

	// Token is valid
	mockAuthMgr.On("ValidateToken", mock.Anything, "kms-read-token").Return(
		&MockTokenValidationResult{
			Valid:    true,
			UserID:   "agent-001",
			Username: "kms-reader",
			Roles:    []string{"agent"},
		}, nil,
	)

	// Only allow KMS read operations
	mockAuthSvc.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return req.Resource == interfaces.ResourceKMS && req.Action == interfaces.ActionRead
	})).Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	// Deny everything else
	mockAuthSvc.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return !(req.Resource == interfaces.ResourceKMS && req.Action == interfaces.ActionRead)
	})).Return(&interfaces.AuthorizationDecision{Allowed: false, Reason: "insufficient permissions"}, nil)

	checker := NewAuthorizationChecker(mockAuthSvc, mockAuthMgr)
	ctx := context.Background()

	filtered := registry.FilterToolsByAuthorization(ctx, checker, "kms-read-token")
	names := toolNames(filtered)

	// Public tools always visible
	assert.Contains(t, names, "quicbaas_health")

	// KMS read tools visible
	assert.Contains(t, names, "quicbaas_kms_get_key")

	// KMS write tools not visible
	assert.NotContains(t, names, "quicbaas_kms_create_key")

	// Other tools not visible
	assert.NotContains(t, names, "quicbaas_secrets_get")
	assert.NotContains(t, names, "quicbaas_secrets_store")
	assert.NotContains(t, names, "quicbaas_get")
	assert.NotContains(t, names, "quicbaas_put")
	assert.NotContains(t, names, "quicbaas_cluster_status")
}

func TestFilterToolsByAuthorization_NoScopes(t *testing.T) {
	registry := buildTestToolRegistry()

	mockAuthSvc := new(MockAuthorizationService)
	mockAuthMgr := new(MockAuthManager)

	// Token is valid
	mockAuthMgr.On("ValidateToken", mock.Anything, "no-scope-token").Return(
		&MockTokenValidationResult{
			Valid:    true,
			UserID:   "agent-002",
			Username: "no-scope-agent",
			Roles:    []string{},
		}, nil,
	)

	// Deny all authorization checks
	mockAuthSvc.On("Authorize", mock.Anything, mock.AnythingOfType("*interfaces.AuthorizationRequest")).Return(
		&interfaces.AuthorizationDecision{Allowed: false, Reason: "no permissions"}, nil,
	)

	checker := NewAuthorizationChecker(mockAuthSvc, mockAuthMgr)
	ctx := context.Background()

	filtered := registry.FilterToolsByAuthorization(ctx, checker, "no-scope-token")
	names := toolNames(filtered)

	// Only public tools should be visible
	assert.Equal(t, 1, len(filtered), "agent with no scopes should see only public tools")
	assert.Contains(t, names, "quicbaas_health")
}

func TestFilterToolsByAuthorization_InvalidToken(t *testing.T) {
	registry := buildTestToolRegistry()

	mockAuthSvc := new(MockAuthorizationService)
	mockAuthMgr := new(MockAuthManager)

	// Token is invalid
	mockAuthMgr.On("ValidateToken", mock.Anything, "bad-token").Return(
		nil, assert.AnError,
	)

	checker := NewAuthorizationChecker(mockAuthSvc, mockAuthMgr)
	ctx := context.Background()

	filtered := registry.FilterToolsByAuthorization(ctx, checker, "bad-token")
	names := toolNames(filtered)

	// Only public tools should be visible (non-public tools fail token validation)
	assert.Equal(t, 1, len(filtered), "invalid token should see only public tools")
	assert.Contains(t, names, "quicbaas_health")
}

func TestFilterToolsByAuthorization_NoAuthChecker(t *testing.T) {
	registry := buildTestToolRegistry()

	// When auth is completely disabled (nil authService, nil authManager),
	// CheckToolAuthorization allows all requests
	checker := NewAuthorizationChecker(nil, nil)
	ctx := context.Background()

	filtered := registry.FilterToolsByAuthorization(ctx, checker, "any-token")
	assert.Equal(t, 8, len(filtered), "disabled auth should show all tools")
}

func TestFilterToolsByAuthorization_MixedPermissions(t *testing.T) {
	registry := buildTestToolRegistry()

	mockAuthSvc := new(MockAuthorizationService)
	mockAuthMgr := new(MockAuthManager)

	// Token is valid
	mockAuthMgr.On("ValidateToken", mock.Anything, "mixed-token").Return(
		&MockTokenValidationResult{
			Valid:    true,
			UserID:   "agent-003",
			Username: "mixed-agent",
			Roles:    []string{"agent"},
		}, nil,
	)

	// Allow KMS read + secrets read, deny writes and everything else
	mockAuthSvc.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return (req.Resource == interfaces.ResourceKMS || req.Resource == interfaces.ResourceSecrets) &&
			req.Action == interfaces.ActionRead
	})).Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	mockAuthSvc.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return !((req.Resource == interfaces.ResourceKMS || req.Resource == interfaces.ResourceSecrets) &&
			req.Action == interfaces.ActionRead)
	})).Return(&interfaces.AuthorizationDecision{Allowed: false, Reason: "denied"}, nil)

	checker := NewAuthorizationChecker(mockAuthSvc, mockAuthMgr)
	ctx := context.Background()

	filtered := registry.FilterToolsByAuthorization(ctx, checker, "mixed-token")
	names := toolNames(filtered)

	require.Equal(t, 3, len(filtered), "expected 1 public + 2 read tools")
	assert.Contains(t, names, "quicbaas_health")
	assert.Contains(t, names, "quicbaas_kms_get_key")
	assert.Contains(t, names, "quicbaas_secrets_get")

	assert.NotContains(t, names, "quicbaas_kms_create_key")
	assert.NotContains(t, names, "quicbaas_secrets_store")
}

func TestFilterToolsByAuthorization_EmptyRegistry(t *testing.T) {
	registry := &ToolRegistry{
		tools: []ToolDefinition{},
	}

	checker := NewAuthorizationChecker(nil, nil)
	ctx := context.Background()

	filtered := registry.FilterToolsByAuthorization(ctx, checker, "token")
	assert.Equal(t, 0, len(filtered))
}

// toolNames extracts tool names from a slice of ToolDefinition.
func toolNames(tools []ToolDefinition) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
