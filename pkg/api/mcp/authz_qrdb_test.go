// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

func TestGetToolQRDBPermission_Mapped(t *testing.T) {
	cases := []struct {
		tool     string
		resource string
		action   string
	}{
		{"quicbaas_get", "kv", "get"},
		{"quicbaas_put", "kv", "put"},
		{"qrdb_delete", "kv", "delete"},
		{"quicbaas_user_create", "users", "create"},
		{"quicbaas_role_assign", "roles", "assign"},
		{"qrdb_barrier_status", "tenant_barrier", "status"},
		{"qrdb_ceremony_status", "ceremony", "status"},
		{"qrdb_vector_search", "vector", "search_similar"},
		{"qrdb_credential_list", "credentials", "list"},
		{"quicbaas_transaction", "transaction", "execute"},
	}
	for _, c := range cases {
		res, act, ok := getToolQRDBPermission(c.tool)
		require.True(t, ok, c.tool)
		assert.Equal(t, c.resource, res, c.tool)
		assert.Equal(t, c.action, act, c.tool)
	}
}

func TestGetToolQRDBPermission_Unmapped(t *testing.T) {
	// Surfaces with no qrdb permission family must be unmapped (default-deny).
	for _, tool := range []string{
		"quicbaas_kms_generate",
		"quicbaas_crypto_sign",
		"quicbaas_orchestrator_spawn",
		"quicbaas_secrets_store",
		"quicbaas_parameter_set",
		"project_create",
		"agent_enable",
		"quicbaas_unknown_thing",
	} {
		_, _, ok := getToolQRDBPermission(tool)
		assert.False(t, ok, tool)
	}
}

// newQRDBChecker builds a checker wired like the RequireAuthz=true path: OIDC
// validation + qrdb-backed authService + canonical mapping.
func newQRDBChecker(oidc interfaces.OIDCProvider, authz interfaces.AuthorizationService) *AuthorizationChecker {
	ac := NewAuthorizationChecker(authz, nil)
	ac.SetOIDCProvider(oidc)
	ac.EnableQRDBMapping()
	return ac
}

func TestCheckToolAuthorization_QRDBMapping_AllowAndDeny(t *testing.T) {
	oidc := new(MockOIDCProvider)
	claims := &MockOIDCClaims{Subject: "u1", Name: "admin", TenantID: "system", Roles: []string{"admin"}}
	oidc.On("ValidateAccessToken", mock.Anything, "good").Return(claims, nil)

	authz := new(MockAuthorizationService)
	// Allow kv:put, deny kv:delete — assert the canonical (resource, action)
	// and the tenant reach the authz service.
	authz.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return req.Resource == "kv" && req.Action == "put" && req.Subject.TenantID == "system"
	})).Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)
	authz.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return req.Resource == "kv" && req.Action == "delete"
	})).Return(&interfaces.AuthorizationDecision{Allowed: false, Reason: "denied"}, nil)

	ac := newQRDBChecker(oidc, authz)

	// Allow.
	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_put", "good")
	assert.Nil(t, err, "permitted tool must be allowed")

	// Deny.
	derr := ac.CheckToolAuthorization(context.Background(), "quicbaas_delete", "good")
	require.NotNil(t, derr)
	assert.Equal(t, AuthErrorPermissionDenied, derr.Type)

	authz.AssertExpectations(t)
}

func TestCheckToolAuthorization_QRDBMapping_UnmappedDenied(t *testing.T) {
	oidc := new(MockOIDCProvider)
	claims := &MockOIDCClaims{Subject: "u1", Name: "admin", TenantID: "system", Roles: []string{"admin"}}
	oidc.On("ValidateAccessToken", mock.Anything, "good").Return(claims, nil)

	authz := new(MockAuthorizationService) // would allow, but tool is unmapped
	ac := newQRDBChecker(oidc, authz)

	// A tool with no qrdb permission family (KMS) is denied before reaching authz.
	err := ac.CheckToolAuthorization(context.Background(), "quicbaas_kms_generate", "good")
	require.NotNil(t, err)
	assert.Equal(t, AuthErrorPermissionDenied, err.Type)
	authz.AssertNotCalled(t, "Authorize", mock.Anything, mock.Anything)
}

func TestCheckToolAuthorization_QRDBMapping_PublicBypass(t *testing.T) {
	oidc := new(MockOIDCProvider)
	authz := new(MockAuthorizationService)
	ac := newQRDBChecker(oidc, authz)
	// Public tool: allowed without a token or authz call.
	assert.Nil(t, ac.CheckToolAuthorization(context.Background(), "quicbaas_health", ""))
	authz.AssertNotCalled(t, "Authorize", mock.Anything, mock.Anything)
}

func TestCheckToolAuthorization_RequireAuthzOff_NoNewDenial(t *testing.T) {
	// RequireAuthz=false posture: no authService, no oidc, no authManager.
	// CheckToolAuthorization allows every tool (auth disabled) — today's
	// behaviour preserved.
	ac := NewAuthorizationChecker(nil, nil)
	assert.Nil(t, ac.CheckToolAuthorization(context.Background(), "quicbaas_put", ""))
	assert.Nil(t, ac.CheckToolAuthorization(context.Background(), "quicbaas_kms_generate", ""))
}

// TestServer_EnableQRDBAuthzMapping verifies the MCP server delegates the
// canonical-mapping switch to its authorization checker.
func TestServer_EnableQRDBAuthzMapping(t *testing.T) {
	srv := NewServer(nil, &Config{}, nil)
	srv.SetAuthorizationService(new(MockAuthorizationService), nil)
	srv.EnableQRDBAuthzMapping()
	require.NotNil(t, srv.authChecker)
	assert.True(t, srv.authChecker.useQRDBMapping)
}
