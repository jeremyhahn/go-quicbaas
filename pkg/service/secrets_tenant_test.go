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

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// secretsTenantScopedID tests
// =============================================================================

func TestSecretsTenantScopedID_EmptyTenant(t *testing.T) {
	// Single-tenant mode: secret ID is returned unchanged
	secretID := "my-secret"
	result := secretsTenantScopedID("", secretID)
	assert.Equal(t, secretID, result)
}

func TestSecretsTenantScopedID_WithTenant(t *testing.T) {
	// Multi-tenant mode: secret ID is prefixed with tenant namespace
	result := secretsTenantScopedID("acme-corp", "my-secret")
	assert.Equal(t, "tenant/acme-corp/my-secret", result)
}

func TestSecretsTenantScopedID_PreservesComplexSecretID(t *testing.T) {
	// Secret IDs with path separators are preserved
	result := secretsTenantScopedID("tenant-123", "apps/myapp/db-password")
	assert.Equal(t, "tenant/tenant-123/apps/myapp/db-password", result)
}

func TestSecretsTenantScopedID_UnderscoreTenant(t *testing.T) {
	result := secretsTenantScopedID("my_tenant", "secret-name")
	assert.Equal(t, "tenant/my_tenant/secret-name", result)
}

// =============================================================================
// secretsTenantStripID tests
// =============================================================================

func TestSecretsTenantStripID_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := secretsTenantStripID("", "my-secret")
	assert.Equal(t, "my-secret", result)
}

func TestSecretsTenantStripID_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := secretsTenantStripID("acme-corp", "tenant/acme-corp/my-secret")
	assert.Equal(t, "my-secret", result)
}

func TestSecretsTenantStripID_NoMatchingPrefix(t *testing.T) {
	// If the secret doesn't have the tenant prefix, it's returned as-is
	result := secretsTenantStripID("acme-corp", "other-secret")
	assert.Equal(t, "other-secret", result)
}

func TestSecretsTenantStripID_WrongTenant(t *testing.T) {
	// If the secret belongs to a different tenant, the prefix is not stripped
	result := secretsTenantStripID("acme-corp", "tenant/other-corp/my-secret")
	assert.Equal(t, "tenant/other-corp/my-secret", result)
}

func TestSecretsTenantStripID_ComplexPath(t *testing.T) {
	result := secretsTenantStripID("acme-corp", "tenant/acme-corp/apps/myapp/db-password")
	assert.Equal(t, "apps/myapp/db-password", result)
}

// =============================================================================
// secretsTenantBelongsTo tests
// =============================================================================

func TestSecretsTenantBelongsTo_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all secrets belong
	assert.True(t, secretsTenantBelongsTo("", "any-secret"))
	assert.True(t, secretsTenantBelongsTo("", "tenant/acme-corp/secret"))
}

func TestSecretsTenantBelongsTo_MatchingTenant(t *testing.T) {
	assert.True(t, secretsTenantBelongsTo("acme-corp", "tenant/acme-corp/my-secret"))
}

func TestSecretsTenantBelongsTo_NonMatchingTenant(t *testing.T) {
	assert.False(t, secretsTenantBelongsTo("acme-corp", "tenant/other-corp/my-secret"))
}

func TestSecretsTenantBelongsTo_UnscopedSecret(t *testing.T) {
	// A secret without tenant prefix does not belong to a multi-tenant request
	assert.False(t, secretsTenantBelongsTo("acme-corp", "my-secret"))
}

func TestSecretsTenantBelongsTo_PartialMatch(t *testing.T) {
	// Ensure partial tenant name matches don't succeed
	assert.False(t, secretsTenantBelongsTo("acme", "tenant/acme-corp/secret"))
}

// =============================================================================
// secretsTenantListPrefix tests
// =============================================================================

func TestSecretsTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", secretsTenantListPrefix(""))
}

func TestSecretsTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant/acme-corp/", secretsTenantListPrefix("acme-corp"))
}

func TestSecretsTenantListPrefix_TrailingSlash(t *testing.T) {
	// Prefix always ends with slash for proper prefix matching
	prefix := secretsTenantListPrefix("my-tenant")
	assert.True(t, strings.HasSuffix(prefix, "/"))
}

// =============================================================================
// enforceSecretsTenantAccess tests
// =============================================================================

func TestEnforceSecretsTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, enforceSecretsTenantAccess("", "any-secret", "GetSecret"))
}

func TestEnforceSecretsTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, enforceSecretsTenantAccess("acme-corp", "tenant/acme-corp/my-secret", "GetSecret"))
}

func TestEnforceSecretsTenantAccess_CrossTenantDenied(t *testing.T) {
	err := enforceSecretsTenantAccess("acme-corp", "tenant/other-corp/my-secret", "GetSecret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestEnforceSecretsTenantAccess_UnscopedSecretDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped secret
	err := enforceSecretsTenantAccess("acme-corp", "unscoped-secret", "DeleteSecret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestEnforceSecretsTenantAccess_OperationInErrorMessage(t *testing.T) {
	err := enforceSecretsTenantAccess("acme-corp", "tenant/other-corp/secret", "StoreSecret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secrets.StoreSecret")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestSecretsTenantID_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		secretID string
	}{
		{"empty-tenant", "", "my-secret"},
		{"with-tenant", "acme-corp", "my-secret"},
		{"complex-path", "tenant-123", "apps/myapp/db-password"},
		{"underscore-tenant", "my_tenant", "secret-name"},
		{"numeric-tenant", "tenant123", "key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := secretsTenantScopedID(tt.tenantID, tt.secretID)
			stripped := secretsTenantStripID(tt.tenantID, scoped)
			assert.Equal(t, tt.secretID, stripped, "roundtrip should return original secret ID")
		})
	}
}

func TestSecretsTenantID_ScopeAndBelongsTo(t *testing.T) {
	// A scoped secret should belong to the tenant that scoped it
	tenantID := "acme-corp"
	secretID := "my-secret"
	scoped := secretsTenantScopedID(tenantID, secretID)
	assert.True(t, secretsTenantBelongsTo(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, secretsTenantBelongsTo("other-corp", scoped))
}

func TestSecretsTenantID_ListPrefixMatchesScopedSecrets(t *testing.T) {
	tenantID := "acme-corp"
	secretID := "my-secret"
	scoped := secretsTenantScopedID(tenantID, secretID)
	prefix := secretsTenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped secret should start with tenant list prefix")
}

func TestSecretsTenantID_ScopeAndEnforceAccess(t *testing.T) {
	// A scoped secret should pass access enforcement for the same tenant
	tenantID := "acme-corp"
	secretID := "my-secret"
	scoped := secretsTenantScopedID(tenantID, secretID)
	assert.NoError(t, enforceSecretsTenantAccess(tenantID, scoped, "GetSecret"))

	// But fail for a different tenant
	err := enforceSecretsTenantAccess("other-corp", scoped, "GetSecret")
	require.Error(t, err)
}

// =============================================================================
// Cross-tenant isolation tests
// =============================================================================

func TestSecretsTenantIsolation_DifferentTenantsGetDifferentPrefixes(t *testing.T) {
	secretID := "shared-name"
	scopedA := secretsTenantScopedID("tenant-a", secretID)
	scopedB := secretsTenantScopedID("tenant-b", secretID)

	// Same secret name yields different scoped paths
	assert.NotEqual(t, scopedA, scopedB)

	// Each scoped path belongs only to its own tenant
	assert.True(t, secretsTenantBelongsTo("tenant-a", scopedA))
	assert.False(t, secretsTenantBelongsTo("tenant-b", scopedA))
	assert.True(t, secretsTenantBelongsTo("tenant-b", scopedB))
	assert.False(t, secretsTenantBelongsTo("tenant-a", scopedB))
}

func TestSecretsTenantIsolation_ListPrefixesAreMutuallyExclusive(t *testing.T) {
	prefixA := secretsTenantListPrefix("tenant-a")
	prefixB := secretsTenantListPrefix("tenant-b")

	// Tenant A's scoped secrets don't start with tenant B's prefix
	scopedA := secretsTenantScopedID("tenant-a", "secret")
	assert.True(t, strings.HasPrefix(scopedA, prefixA))
	assert.False(t, strings.HasPrefix(scopedA, prefixB))
}
