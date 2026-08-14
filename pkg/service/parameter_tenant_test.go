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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// paramTenantScopedPath tests
// =============================================================================

func TestParamTenantScopedPath_EmptyTenant(t *testing.T) {
	// Single-tenant mode: path is returned unchanged
	path := "app/config/db_host"
	result := paramTenantScopedPath("", path)
	assert.Equal(t, path, result)
}

func TestParamTenantScopedPath_WithTenant(t *testing.T) {
	// Multi-tenant mode: path is prefixed with tenant namespace
	result := paramTenantScopedPath("acme-corp", "app/config/db_host")
	assert.Equal(t, "tenant/acme-corp/app/config/db_host", result)
}

func TestParamTenantScopedPath_PreservesComplexPath(t *testing.T) {
	// Paths with special characters are preserved
	result := paramTenantScopedPath("tenant-123", "production/api-key")
	assert.Equal(t, "tenant/tenant-123/production/api-key", result)
}

func TestParamTenantScopedPath_UnderscoreTenant(t *testing.T) {
	result := paramTenantScopedPath("my_tenant", "app/config/db_host")
	assert.Equal(t, "tenant/my_tenant/app/config/db_host", result)
}

// =============================================================================
// paramTenantStripPath tests
// =============================================================================

func TestParamTenantStripPath_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := paramTenantStripPath("", "app/config/db_host")
	assert.Equal(t, "app/config/db_host", result)
}

func TestParamTenantStripPath_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := paramTenantStripPath("acme-corp", "tenant/acme-corp/app/config/db_host")
	assert.Equal(t, "app/config/db_host", result)
}

func TestParamTenantStripPath_NoMatchingPrefix(t *testing.T) {
	// If the path doesn't have the tenant prefix, it's returned as-is
	result := paramTenantStripPath("acme-corp", "other-path")
	assert.Equal(t, "other-path", result)
}

func TestParamTenantStripPath_WrongTenant(t *testing.T) {
	// If the path belongs to a different tenant, the prefix is not stripped
	result := paramTenantStripPath("acme-corp", "tenant/other-corp/app/config/db_host")
	assert.Equal(t, "tenant/other-corp/app/config/db_host", result)
}

func TestParamTenantStripPath_ComplexPath(t *testing.T) {
	result := paramTenantStripPath("acme-corp", "tenant/acme-corp/production/api-key")
	assert.Equal(t, "production/api-key", result)
}

// =============================================================================
// paramPathBelongsToTenant tests
// =============================================================================

func TestParamPathBelongsToTenant_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all paths belong
	assert.True(t, paramPathBelongsToTenant("", "any-path"))
	assert.True(t, paramPathBelongsToTenant("", "tenant/acme-corp/path"))
}

func TestParamPathBelongsToTenant_MatchingTenant(t *testing.T) {
	assert.True(t, paramPathBelongsToTenant("acme-corp", "tenant/acme-corp/app/config/db_host"))
}

func TestParamPathBelongsToTenant_NonMatchingTenant(t *testing.T) {
	assert.False(t, paramPathBelongsToTenant("acme-corp", "tenant/other-corp/app/config/db_host"))
}

func TestParamPathBelongsToTenant_UnscopedPath(t *testing.T) {
	// A path without tenant prefix does not belong to a multi-tenant request
	assert.False(t, paramPathBelongsToTenant("acme-corp", "app/config/db_host"))
}

func TestParamPathBelongsToTenant_PartialMatch(t *testing.T) {
	// Ensure partial tenant name matches don't succeed
	assert.False(t, paramPathBelongsToTenant("acme", "tenant/acme-corp/path"))
}

// =============================================================================
// paramTenantListPrefix tests
// =============================================================================

func TestParamTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", paramTenantListPrefix(""))
}

func TestParamTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant/acme-corp/", paramTenantListPrefix("acme-corp"))
}

func TestParamTenantListPrefix_TrailingSlash(t *testing.T) {
	// Prefix always ends with slash for proper prefix matching
	prefix := paramTenantListPrefix("my-tenant")
	assert.True(t, strings.HasSuffix(prefix, "/"))
}

// =============================================================================
// paramEnforceTenantAccess tests
// =============================================================================

func TestParamEnforceTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, paramEnforceTenantAccess("", "any-path", "GetParameter"))
}

func TestParamEnforceTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, paramEnforceTenantAccess("acme-corp", "tenant/acme-corp/app/config/db_host", "GetParameter"))
}

func TestParamEnforceTenantAccess_CrossTenantDenied(t *testing.T) {
	err := paramEnforceTenantAccess("acme-corp", "tenant/other-corp/app/config/db_host", "GetParameter")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestParamEnforceTenantAccess_UnscopedPathDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped path
	err := paramEnforceTenantAccess("acme-corp", "unscoped-path", "DeleteParameter")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
}

func TestParamEnforceTenantAccess_OperationInErrorMessage(t *testing.T) {
	err := paramEnforceTenantAccess("acme-corp", "tenant/other-corp/path", "PutParameter")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parameterstore.PutParameter")
}

// =============================================================================
// paramPrepareTenantContext tests
// =============================================================================

func TestParamPrepareTenantContext_NoTenant(t *testing.T) {
	ctx := context.Background()
	tenantID, err := paramPrepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestParamPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	tenantID, err := paramPrepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestParamPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "invalid tenant!@#")
	_, err := paramPrepareTenantContext(ctx)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestParamPrepareTenantContext_TooLongTenant(t *testing.T) {
	longID := strings.Repeat("a", 129)
	ctx := iam.ContextWithTenant(context.Background(), longID)
	_, err := paramPrepareTenantContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestParamTenantPath_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		path     string
	}{
		{"empty-tenant", "", "app/config/db_host"},
		{"with-tenant", "acme-corp", "app/config/db_host"},
		{"complex-path", "tenant-123", "production/api-key"},
		{"underscore-tenant", "my_tenant", "path-name"},
		{"numeric-tenant", "tenant123", "config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := paramTenantScopedPath(tt.tenantID, tt.path)
			stripped := paramTenantStripPath(tt.tenantID, scoped)
			assert.Equal(t, tt.path, stripped, "roundtrip should return original path")
		})
	}
}

func TestParamTenantPath_ScopeAndBelongsTo(t *testing.T) {
	// A scoped path should belong to the tenant that scoped it
	tenantID := "acme-corp"
	path := "app/config/db_host"
	scoped := paramTenantScopedPath(tenantID, path)
	assert.True(t, paramPathBelongsToTenant(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, paramPathBelongsToTenant("other-corp", scoped))
}

func TestParamTenantPath_ListPrefixMatchesScopedPaths(t *testing.T) {
	tenantID := "acme-corp"
	path := "app/config/db_host"
	scoped := paramTenantScopedPath(tenantID, path)
	prefix := paramTenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped path should start with tenant list prefix")
}

func TestParamTenantPath_ScopeAndEnforceAccess(t *testing.T) {
	// A scoped path should pass access enforcement for the same tenant
	tenantID := "acme-corp"
	path := "production/api-key"
	scoped := paramTenantScopedPath(tenantID, path)
	assert.NoError(t, paramEnforceTenantAccess(tenantID, scoped, "GetParameter"))

	// But fail for a different tenant
	err := paramEnforceTenantAccess("other-corp", scoped, "GetParameter")
	require.Error(t, err)
}

// =============================================================================
// Cross-tenant isolation tests
// =============================================================================

func TestParamTenantIsolation_DifferentTenantsGetDifferentPrefixes(t *testing.T) {
	path := "shared-config"
	scopedA := paramTenantScopedPath("tenant-a", path)
	scopedB := paramTenantScopedPath("tenant-b", path)

	// Same path name yields different scoped paths
	assert.NotEqual(t, scopedA, scopedB)

	// Each scoped path belongs only to its own tenant
	assert.True(t, paramPathBelongsToTenant("tenant-a", scopedA))
	assert.False(t, paramPathBelongsToTenant("tenant-b", scopedA))
	assert.True(t, paramPathBelongsToTenant("tenant-b", scopedB))
	assert.False(t, paramPathBelongsToTenant("tenant-a", scopedB))
}

func TestParamTenantIsolation_ListPrefixesAreMutuallyExclusive(t *testing.T) {
	prefixA := paramTenantListPrefix("tenant-a")
	prefixB := paramTenantListPrefix("tenant-b")

	// Tenant A's scoped paths don't start with tenant B's prefix
	scopedA := paramTenantScopedPath("tenant-a", "config")
	assert.True(t, strings.HasPrefix(scopedA, prefixA))
	assert.False(t, strings.HasPrefix(scopedA, prefixB))
}

// =============================================================================
// System-tenant scoping tests
// =============================================================================

// TestParameterTenant_SystemTenantIsPlatformScoped proves a caller carrying
// the literal system tenant is treated identically to single-tenant mode
// rather than being isolated into a tenant namespace named "system".
func TestParameterTenant_SystemTenantIsPlatformScoped(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("tenantID="+tenantID, func(t *testing.T) {
			assert.Equal(t, "/config/db", paramTenantScopedPath(tenantID, "/config/db"))
			assert.True(t, paramPathBelongsToTenant(tenantID, "/any/path"))
			assert.NoError(t, paramEnforceTenantAccess(tenantID, "/any/path", "GetParameter"))
		})
	}
}

// TestParameterTenant_OrdinaryTenantStaysIsolated proves the system-tenant
// carve-out does not widen ordinary tenants' reach: a lookalike ID that
// merely shares the "system" prefix is still fully isolated.
func TestParameterTenant_OrdinaryTenantStaysIsolated(t *testing.T) {
	scoped := paramTenantScopedPath("system-2", "/config/db")
	assert.Equal(t, "tenant/system-2//config/db", scoped)
	assert.False(t, paramPathBelongsToTenant("system-2", "/config/db"))
	err := paramEnforceTenantAccess("system-2", "/config/db", "GetParameter")
	require.Error(t, err)
}
