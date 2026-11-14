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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// vectorTenantScopedNamespace tests
// =============================================================================

func TestVectorTenantScopedNamespace(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		ns       string
		want     string
	}{
		{"empty tenant returns namespace unchanged", "", "embeddings", "embeddings"},
		{"tenant prefixes namespace", "acme-corp", "embeddings", "tenant/acme-corp/embeddings"},
		{"hyphenated namespace preserved", "tenant-123", "product-vectors", "tenant/tenant-123/product-vectors"},
		{"underscore tenant", "my_tenant", "search-index", "tenant/my_tenant/search-index"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, vectorTenantScopedNamespace(tt.tenantID, tt.ns))
		})
	}
}

// =============================================================================
// vectorTenantStripNamespace tests
// =============================================================================

func TestVectorTenantStripNamespace(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		ns       string
		want     string
	}{
		{"empty tenant does not strip", "", "embeddings", "embeddings"},
		{"own tenant prefix removed", "acme-corp", "tenant/acme-corp/embeddings", "embeddings"},
		{"unprefixed namespace returned as-is", "acme-corp", "other-namespace", "other-namespace"},
		{"other tenant prefix not stripped", "acme-corp", "tenant/other-corp/embeddings", "tenant/other-corp/embeddings"},
		{"hyphenated namespace", "acme-corp", "tenant/acme-corp/product-vectors", "product-vectors"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, vectorTenantStripNamespace(tt.tenantID, tt.ns))
		})
	}
}

// =============================================================================
// vectorNamespaceBelongsToTenant tests
// =============================================================================

func TestVectorNamespaceBelongsToTenant(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		ns       string
		want     bool
	}{
		{"empty tenant owns unscoped namespace", "", "any-namespace", true},
		{"empty tenant owns scoped namespace", "", "tenant/acme-corp/ns", true},
		{"matching tenant", "acme-corp", "tenant/acme-corp/embeddings", true},
		{"other tenant", "acme-corp", "tenant/other-corp/embeddings", false},
		{"unscoped namespace in multi-tenant request", "acme-corp", "embeddings", false},
		{"partial tenant name match", "acme", "tenant/acme-corp/ns", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, vectorNamespaceBelongsToTenant(tt.tenantID, tt.ns))
		})
	}
}

// =============================================================================
// vectorTenantListPrefix tests
// =============================================================================

func TestVectorTenantListPrefix(t *testing.T) {
	assert.Equal(t, "", vectorTenantListPrefix(""))
	// The prefix ends with a slash so prefix matching cannot hit a longer tenant ID.
	assert.Equal(t, "tenant/acme-corp/", vectorTenantListPrefix("acme-corp"))
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
	assert.Contains(t, err.Error(), "vector.Search")
}

func TestVectorEnforceTenantAccess_UnscopedNamespaceDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped namespace
	err := vectorEnforceTenantAccess("acme-corp", "unscoped-namespace", "DeleteNamespace")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
}

// =============================================================================
// vectorPrepareTenantContext tests
// =============================================================================

func TestVectorPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	tenantID, err := prepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestVectorPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "invalid tenant!@#")
	_, err := prepareTenantContext(ctx)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestVectorPrepareTenantContext_TooLongTenant(t *testing.T) {
	longID := strings.Repeat("a", 129)
	ctx := tenantctx.WithTenant(context.Background(), longID)
	_, err := prepareTenantContext(ctx)
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

// =============================================================================
// System-tenant scoping tests
// =============================================================================

// TestVectorTenant_SystemTenantIsPlatformScoped proves a caller carrying the
// literal system tenant is treated identically to single-tenant mode rather
// than being isolated into a tenant namespace named "system".
func TestVectorTenant_SystemTenantIsPlatformScoped(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("tenantID="+tenantID, func(t *testing.T) {
			assert.Equal(t, "embeddings", vectorTenantScopedNamespace(tenantID, "embeddings"))
			assert.True(t, vectorNamespaceBelongsToTenant(tenantID, "any-namespace"))
			assert.NoError(t, vectorEnforceTenantAccess(tenantID, "any-namespace", "Search"))
		})
	}
}

// TestVectorTenant_OrdinaryTenantStaysIsolated proves the system-tenant
// carve-out does not widen ordinary tenants' reach: a lookalike ID that
// merely shares the "system" prefix is still fully isolated.
func TestVectorTenant_OrdinaryTenantStaysIsolated(t *testing.T) {
	scoped := vectorTenantScopedNamespace("system-2", "embeddings")
	assert.Equal(t, "tenant/system-2/embeddings", scoped)
	assert.False(t, vectorNamespaceBelongsToTenant("system-2", "embeddings"))
	err := vectorEnforceTenantAccess("system-2", "embeddings", "Search")
	require.Error(t, err)
}
