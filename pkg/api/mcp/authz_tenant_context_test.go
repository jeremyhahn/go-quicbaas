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

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestCheckToolAuthorizationWithContext_OIDC_StampsTenant verifies that a
// validated OIDC bearer token carrying a tenant_id claim results in the tenant
// being placed on the returned context so the tool handler can read it.
func TestCheckToolAuthorizationWithContext_OIDC_StampsTenant(t *testing.T) {
	mockOIDC := new(MockOIDCProvider)
	mockAuthService := new(MockAuthorizationService)

	claims := &MockOIDCClaims{
		Subject:  "user-123",
		Name:     "Test User",
		Roles:    []string{"admin"},
		TenantID: "tenant-alpha",
	}
	mockOIDC.On("ValidateAccessToken", mock.Anything, "oidc-token").Return(claims, nil)
	mockAuthService.On("Authorize", mock.Anything, mock.Anything).
		Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	checker := NewAuthorizationChecker(mockAuthService, nil)
	checker.SetOIDCProvider(mockOIDC)

	ctx, authErr := checker.CheckToolAuthorizationWithContext(context.Background(), "qrdb_get", "oidc-token")

	assert.Nil(t, authErr)
	assert.Equal(t, "tenant-alpha", iam.TenantFromContext(ctx),
		"tenant from validated OIDC claim must be on the authorized tool context")
}

// TestCheckToolAuthorizationWithContext_AuthManager_StampsTenant verifies the
// AuthManager validation branch sources the tenant from the validation result.
func TestCheckToolAuthorizationWithContext_AuthManager_StampsTenant(t *testing.T) {
	mockAuthManager := new(MockAuthManager)
	mockAuthService := new(MockAuthorizationService)

	mockAuthManager.On("ValidateToken", mock.Anything, "am-token").
		Return(&auth.TokenValidationResult{
			Valid:    true,
			UserID:   "user-456",
			Username: "am-user",
			Roles:    []string{"reader"},
			TenantID: "tenant-beta",
		}, nil)
	mockAuthService.On("Authorize", mock.Anything, mock.Anything).
		Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	ctx, authErr := checker.CheckToolAuthorizationWithContext(context.Background(), "qrdb_get", "am-token")

	assert.Nil(t, authErr)
	assert.Equal(t, "tenant-beta", iam.TenantFromContext(ctx),
		"tenant from validated AuthManager result must be on the authorized tool context")
}

// TestCheckToolAuthorizationWithContext_NoTenantClaim_StillAuthorizes verifies
// that a validated token without a tenant claim still authorizes successfully
// and leaves no tenant on the context (a missing tenant must never be rejected
// by this method).
func TestCheckToolAuthorizationWithContext_NoTenantClaim_StillAuthorizes(t *testing.T) {
	mockAuthManager := new(MockAuthManager)
	mockAuthService := new(MockAuthorizationService)

	mockAuthManager.On("ValidateToken", mock.Anything, "am-token").
		Return(&auth.TokenValidationResult{
			Valid:    true,
			UserID:   "user-789",
			Username: "no-tenant-user",
			Roles:    []string{"reader"},
			// TenantID intentionally empty.
		}, nil)
	mockAuthService.On("Authorize", mock.Anything, mock.Anything).
		Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	ctx, authErr := checker.CheckToolAuthorizationWithContext(context.Background(), "qrdb_get", "am-token")

	assert.Nil(t, authErr, "missing tenant claim must not cause authorization failure")
	assert.Equal(t, "", iam.TenantFromContext(ctx),
		"no tenant should be stamped when the token carries no tenant claim")
}
