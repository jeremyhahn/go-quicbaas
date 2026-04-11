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
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// vectorTenantScopedNamespace tests
// =============================================================================

func TestVectorTenantScopedNamespace_EmptyTenant(t *testing.T) {
	// Single-tenant mode: namespace is returned unchanged
	ns := "embeddings"
	result := vectorTenantScopedNamespace("", ns)
	assert.Equal(t, ns, result)
}

func TestVectorTenantScopedNamespace_WithTenant(t *testing.T) {
	// Multi-tenant mode: namespace is prefixed with tenant namespace
	result := vectorTenantScopedNamespace("acme-corp", "embeddings")
	assert.Equal(t, "tenant/acme-corp/embeddings", result)
}

func TestVectorTenantScopedNamespace_PreservesComplexNamespace(t *testing.T) {
	// Namespaces with special characters are preserved
	result := vectorTenantScopedNamespace("tenant-123", "product-vectors")
	assert.Equal(t, "tenant/tenant-123/product-vectors", result)
}

func TestVectorTenantScopedNamespace_UnderscoreTenant(t *testing.T) {
	result := vectorTenantScopedNamespace("my_tenant", "search-index")
	assert.Equal(t, "tenant/my_tenant/search-index", result)
}

// =============================================================================
// vectorTenantStripNamespace tests
// =============================================================================

func TestVectorTenantStripNamespace_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := vectorTenantStripNamespace("", "embeddings")
	assert.Equal(t, "embeddings", result)
}

func TestVectorTenantStripNamespace_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := vectorTenantStripNamespace("acme-corp", "tenant/acme-corp/embeddings")
	assert.Equal(t, "embeddings", result)
}

func TestVectorTenantStripNamespace_NoMatchingPrefix(t *testing.T) {
	// If the namespace doesn't have the tenant prefix, it's returned as-is
	result := vectorTenantStripNamespace("acme-corp", "other-namespace")
	assert.Equal(t, "other-namespace", result)
}

func TestVectorTenantStripNamespace_WrongTenant(t *testing.T) {
	// If the namespace belongs to a different tenant, the prefix is not stripped
	result := vectorTenantStripNamespace("acme-corp", "tenant/other-corp/embeddings")
	assert.Equal(t, "tenant/other-corp/embeddings", result)
}

func TestVectorTenantStripNamespace_ComplexNamespace(t *testing.T) {
	result := vectorTenantStripNamespace("acme-corp", "tenant/acme-corp/product-vectors")
	assert.Equal(t, "product-vectors", result)
}

// =============================================================================
// vectorNamespaceBelongsToTenant tests
// =============================================================================

func TestVectorNamespaceBelongsToTenant_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all namespaces belong
	assert.True(t, vectorNamespaceBelongsToTenant("", "any-namespace"))
	assert.True(t, vectorNamespaceBelongsToTenant("", "tenant/acme-corp/ns"))
}

func TestVectorNamespaceBelongsToTenant_MatchingTenant(t *testing.T) {
	assert.True(t, vectorNamespaceBelongsToTenant("acme-corp", "tenant/acme-corp/embeddings"))
}

func TestVectorNamespaceBelongsToTenant_NonMatchingTenant(t *testing.T) {
	assert.False(t, vectorNamespaceBelongsToTenant("acme-corp", "tenant/other-corp/embeddings"))
}

func TestVectorNamespaceBelongsToTenant_UnscopedNamespace(t *testing.T) {
	// A namespace without tenant prefix does not belong to a multi-tenant request
	assert.False(t, vectorNamespaceBelongsToTenant("acme-corp", "embeddings"))
}

func TestVectorNamespaceBelongsToTenant_PartialMatch(t *testing.T) {
	// Ensure partial tenant name matches don't succeed
	assert.False(t, vectorNamespaceBelongsToTenant("acme", "tenant/acme-corp/ns"))
}

// =============================================================================
// vectorTenantListPrefix tests
// =============================================================================

func TestVectorTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", vectorTenantListPrefix(""))
}

func TestVectorTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant/acme-corp/", vectorTenantListPrefix("acme-corp"))
}

func TestVectorTenantListPrefix_TrailingSlash(t *testing.T) {
	// Prefix always ends with slash for proper prefix matching
	prefix := vectorTenantListPrefix("my-tenant")
	assert.True(t, strings.HasSuffix(prefix, "/"))
}

// =============================================================================
// vectorEnforceTenantAccess tests
// =============================================================================

func TestVectorEnforceTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, vectorEnforceTenantAccess("", "any-namespace", "Search"))
}

func TestVectorEnforceTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, vectorEnforceTenantAccess("acme-corp", "tenant/acme-corp/embeddings", "Search"))
}

func TestVectorEnforceTenantAccess_CrossTenantDenied(t *testing.T) {
	err := vectorEnforceTenantAccess("acme-corp", "tenant/other-corp/embeddings", "Search")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestVectorEnforceTenantAccess_UnscopedNamespaceDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped namespace
	err := vectorEnforceTenantAccess("acme-corp", "unscoped-namespace", "DeleteNamespace")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
}

func TestVectorEnforceTenantAccess_OperationInErrorMessage(t *testing.T) {
	err := vectorEnforceTenantAccess("acme-corp", "tenant/other-corp/ns", "Upsert")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector.Upsert")
}

// =============================================================================
// vectorPrepareTenantContext tests
// =============================================================================

func TestVectorPrepareTenantContext_NoTenant(t *testing.T) {
	ctx := context.Background()
	tenantID, err := vectorPrepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestVectorPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	tenantID, err := vectorPrepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestVectorPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "invalid tenant!@#")
	_, err := vectorPrepareTenantContext(ctx)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestVectorPrepareTenantContext_TooLongTenant(t *testing.T) {
	longID := strings.Repeat("a", 129)
	ctx := iam.ContextWithTenant(context.Background(), longID)
	_, err := vectorPrepareTenantContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestVectorTenantNamespace_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		ns       string
	}{
		{"empty-tenant", "", "embeddings"},
		{"with-tenant", "acme-corp", "embeddings"},
		{"complex-namespace", "tenant-123", "product-vectors"},
		{"underscore-tenant", "my_tenant", "search-index"},
		{"numeric-tenant", "tenant123", "ns"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := vectorTenantScopedNamespace(tt.tenantID, tt.ns)
			stripped := vectorTenantStripNamespace(tt.tenantID, scoped)
			assert.Equal(t, tt.ns, stripped, "roundtrip should return original namespace")
		})
	}
}

func TestVectorTenantNamespace_ScopeAndBelongsTo(t *testing.T) {
	// A scoped namespace should belong to the tenant that scoped it
	tenantID := "acme-corp"
	ns := "embeddings"
	scoped := vectorTenantScopedNamespace(tenantID, ns)
	assert.True(t, vectorNamespaceBelongsToTenant(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, vectorNamespaceBelongsToTenant("other-corp", scoped))
}

func TestVectorTenantNamespace_ListPrefixMatchesScopedNamespaces(t *testing.T) {
	tenantID := "acme-corp"
	ns := "embeddings"
	scoped := vectorTenantScopedNamespace(tenantID, ns)
	prefix := vectorTenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped namespace should start with tenant list prefix")
}

func TestVectorTenantNamespace_ScopeAndEnforceAccess(t *testing.T) {
	// A scoped namespace should pass access enforcement for the same tenant
	tenantID := "acme-corp"
	ns := "product-vectors"
	scoped := vectorTenantScopedNamespace(tenantID, ns)
	assert.NoError(t, vectorEnforceTenantAccess(tenantID, scoped, "Search"))

	// But fail for a different tenant
	err := vectorEnforceTenantAccess("other-corp", scoped, "Search")
	require.Error(t, err)
}

// =============================================================================
// Cross-tenant isolation tests
// =============================================================================

func TestVectorTenantIsolation_DifferentTenantsGetDifferentPrefixes(t *testing.T) {
	ns := "shared-namespace"
	scopedA := vectorTenantScopedNamespace("tenant-a", ns)
	scopedB := vectorTenantScopedNamespace("tenant-b", ns)

	// Same namespace name yields different scoped namespaces
	assert.NotEqual(t, scopedA, scopedB)

	// Each scoped namespace belongs only to its own tenant
	assert.True(t, vectorNamespaceBelongsToTenant("tenant-a", scopedA))
	assert.False(t, vectorNamespaceBelongsToTenant("tenant-b", scopedA))
	assert.True(t, vectorNamespaceBelongsToTenant("tenant-b", scopedB))
	assert.False(t, vectorNamespaceBelongsToTenant("tenant-a", scopedB))
}

func TestVectorTenantIsolation_ListPrefixesAreMutuallyExclusive(t *testing.T) {
	prefixA := vectorTenantListPrefix("tenant-a")
	prefixB := vectorTenantListPrefix("tenant-b")

	// Tenant A's scoped namespaces don't start with tenant B's prefix
	scopedA := vectorTenantScopedNamespace("tenant-a", "embeddings")
	assert.True(t, strings.HasPrefix(scopedA, prefixA))
	assert.False(t, strings.HasPrefix(scopedA, prefixB))
}
