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
// osAdminTenantScopedBackend tests
// =============================================================================

func TestOsAdminTenantScopedBackend_EmptyTenant(t *testing.T) {
	// Single-tenant mode: backend name is returned unchanged
	backend := "s3-primary"
	result := osAdminTenantScopedBackend("", backend)
	assert.Equal(t, backend, result)
}

func TestOsAdminTenantScopedBackend_WithTenant(t *testing.T) {
	// Multi-tenant mode: backend name is prefixed with tenant namespace
	result := osAdminTenantScopedBackend("acme-corp", "s3-primary")
	assert.Equal(t, "tenant/acme-corp/s3-primary", result)
}

func TestOsAdminTenantScopedBackend_PreservesComplexBackend(t *testing.T) {
	// Backend names with special characters are preserved
	result := osAdminTenantScopedBackend("tenant-123", "gcs-backup")
	assert.Equal(t, "tenant/tenant-123/gcs-backup", result)
}

func TestOsAdminTenantScopedBackend_UnderscoreTenant(t *testing.T) {
	result := osAdminTenantScopedBackend("my_tenant", "local-storage")
	assert.Equal(t, "tenant/my_tenant/local-storage", result)
}

// =============================================================================
// osAdminTenantStripBackend tests
// =============================================================================

func TestOsAdminTenantStripBackend_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := osAdminTenantStripBackend("", "s3-primary")
	assert.Equal(t, "s3-primary", result)
}

func TestOsAdminTenantStripBackend_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := osAdminTenantStripBackend("acme-corp", "tenant/acme-corp/s3-primary")
	assert.Equal(t, "s3-primary", result)
}

func TestOsAdminTenantStripBackend_NoMatchingPrefix(t *testing.T) {
	// If the backend doesn't have the tenant prefix, it's returned as-is
	result := osAdminTenantStripBackend("acme-corp", "other-backend")
	assert.Equal(t, "other-backend", result)
}

func TestOsAdminTenantStripBackend_WrongTenant(t *testing.T) {
	// If the backend belongs to a different tenant, the prefix is not stripped
	result := osAdminTenantStripBackend("acme-corp", "tenant/other-corp/s3-primary")
	assert.Equal(t, "tenant/other-corp/s3-primary", result)
}

func TestOsAdminTenantStripBackend_ComplexBackend(t *testing.T) {
	result := osAdminTenantStripBackend("acme-corp", "tenant/acme-corp/gcs-backup")
	assert.Equal(t, "gcs-backup", result)
}

// =============================================================================
// osAdminBackendBelongsToTenant tests
// =============================================================================

func TestOsAdminBackendBelongsToTenant_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all backends belong
	assert.True(t, osAdminBackendBelongsToTenant("", "any-backend"))
	assert.True(t, osAdminBackendBelongsToTenant("", "tenant/acme-corp/backend"))
}

func TestOsAdminBackendBelongsToTenant_MatchingTenant(t *testing.T) {
	assert.True(t, osAdminBackendBelongsToTenant("acme-corp", "tenant/acme-corp/s3-primary"))
}

func TestOsAdminBackendBelongsToTenant_NonMatchingTenant(t *testing.T) {
	assert.False(t, osAdminBackendBelongsToTenant("acme-corp", "tenant/other-corp/s3-primary"))
}

func TestOsAdminBackendBelongsToTenant_UnscopedBackend(t *testing.T) {
	// A backend without tenant prefix does not belong to a multi-tenant request
	assert.False(t, osAdminBackendBelongsToTenant("acme-corp", "s3-primary"))
}

func TestOsAdminBackendBelongsToTenant_PartialMatch(t *testing.T) {
	// Ensure partial tenant name matches don't succeed
	assert.False(t, osAdminBackendBelongsToTenant("acme", "tenant/acme-corp/backend"))
}

// =============================================================================
// osAdminTenantListPrefix tests
// =============================================================================

func TestOsAdminTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", osAdminTenantListPrefix(""))
}

func TestOsAdminTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant/acme-corp/", osAdminTenantListPrefix("acme-corp"))
}

func TestOsAdminTenantListPrefix_TrailingSlash(t *testing.T) {
	// Prefix always ends with slash for proper prefix matching
	prefix := osAdminTenantListPrefix("my-tenant")
	assert.True(t, strings.HasSuffix(prefix, "/"))
}

// =============================================================================
// osAdminEnforceTenantAccess tests
// =============================================================================

func TestOsAdminEnforceTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, osAdminEnforceTenantAccess("", "any-backend", "GetBackend"))
}

func TestOsAdminEnforceTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, osAdminEnforceTenantAccess("acme-corp", "tenant/acme-corp/s3-primary", "GetBackend"))
}

func TestOsAdminEnforceTenantAccess_CrossTenantDenied(t *testing.T) {
	err := osAdminEnforceTenantAccess("acme-corp", "tenant/other-corp/s3-primary", "GetBackend")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestOsAdminEnforceTenantAccess_UnscopedBackendDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped backend
	err := osAdminEnforceTenantAccess("acme-corp", "unscoped-backend", "DeleteBackend")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
}

func TestOsAdminEnforceTenantAccess_OperationInErrorMessage(t *testing.T) {
	err := osAdminEnforceTenantAccess("acme-corp", "tenant/other-corp/backend", "RegisterBackend")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "objectstorage_admin.RegisterBackend")
}

// =============================================================================
// osAdminPrepareTenantContext tests
// =============================================================================

func TestOsAdminPrepareTenantContext_NoTenant(t *testing.T) {
	ctx := context.Background()
	tenantID, err := osAdminPrepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestOsAdminPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	tenantID, err := osAdminPrepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestOsAdminPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "invalid tenant!@#")
	_, err := osAdminPrepareTenantContext(ctx)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestOsAdminPrepareTenantContext_TooLongTenant(t *testing.T) {
	longID := strings.Repeat("a", 129)
	ctx := iam.ContextWithTenant(context.Background(), longID)
	_, err := osAdminPrepareTenantContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestOsAdminTenantBackend_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		backend  string
	}{
		{"empty-tenant", "", "s3-primary"},
		{"with-tenant", "acme-corp", "s3-primary"},
		{"complex-backend", "tenant-123", "gcs-backup"},
		{"underscore-tenant", "my_tenant", "local-storage"},
		{"numeric-tenant", "tenant123", "backend"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := osAdminTenantScopedBackend(tt.tenantID, tt.backend)
			stripped := osAdminTenantStripBackend(tt.tenantID, scoped)
			assert.Equal(t, tt.backend, stripped, "roundtrip should return original backend name")
		})
	}
}

func TestOsAdminTenantBackend_ScopeAndBelongsTo(t *testing.T) {
	// A scoped backend should belong to the tenant that scoped it
	tenantID := "acme-corp"
	backend := "s3-primary"
	scoped := osAdminTenantScopedBackend(tenantID, backend)
	assert.True(t, osAdminBackendBelongsToTenant(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, osAdminBackendBelongsToTenant("other-corp", scoped))
}

func TestOsAdminTenantBackend_ListPrefixMatchesScopedBackends(t *testing.T) {
	tenantID := "acme-corp"
	backend := "s3-primary"
	scoped := osAdminTenantScopedBackend(tenantID, backend)
	prefix := osAdminTenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped backend should start with tenant list prefix")
}

func TestOsAdminTenantBackend_ScopeAndEnforceAccess(t *testing.T) {
	// A scoped backend should pass access enforcement for the same tenant
	tenantID := "acme-corp"
	backend := "gcs-backup"
	scoped := osAdminTenantScopedBackend(tenantID, backend)
	assert.NoError(t, osAdminEnforceTenantAccess(tenantID, scoped, "GetBackend"))

	// But fail for a different tenant
	err := osAdminEnforceTenantAccess("other-corp", scoped, "GetBackend")
	require.Error(t, err)
}

// =============================================================================
// Cross-tenant isolation tests
// =============================================================================

func TestOsAdminTenantIsolation_DifferentTenantsGetDifferentPrefixes(t *testing.T) {
	backend := "shared-backend"
	scopedA := osAdminTenantScopedBackend("tenant-a", backend)
	scopedB := osAdminTenantScopedBackend("tenant-b", backend)

	// Same backend name yields different scoped backends
	assert.NotEqual(t, scopedA, scopedB)

	// Each scoped backend belongs only to its own tenant
	assert.True(t, osAdminBackendBelongsToTenant("tenant-a", scopedA))
	assert.False(t, osAdminBackendBelongsToTenant("tenant-b", scopedA))
	assert.True(t, osAdminBackendBelongsToTenant("tenant-b", scopedB))
	assert.False(t, osAdminBackendBelongsToTenant("tenant-a", scopedB))
}

func TestOsAdminTenantIsolation_ListPrefixesAreMutuallyExclusive(t *testing.T) {
	prefixA := osAdminTenantListPrefix("tenant-a")
	prefixB := osAdminTenantListPrefix("tenant-b")

	// Tenant A's scoped backends don't start with tenant B's prefix
	scopedA := osAdminTenantScopedBackend("tenant-a", "s3-primary")
	assert.True(t, strings.HasPrefix(scopedA, prefixA))
	assert.False(t, strings.HasPrefix(scopedA, prefixB))
}
