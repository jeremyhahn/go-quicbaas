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

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// dsTenantScopedRoleName tests
// =============================================================================

func TestDsTenantScopedRoleName_EmptyTenant(t *testing.T) {
	// Single-tenant mode: role name is returned unchanged
	roleName := "db-reader"
	result := dsTenantScopedRoleName("", roleName)
	assert.Equal(t, roleName, result)
}

func TestDsTenantScopedRoleName_WithTenant(t *testing.T) {
	// Multi-tenant mode: role name is prefixed with tenant namespace
	result := dsTenantScopedRoleName("acme-corp", "db-reader")
	assert.Equal(t, "tenant/acme-corp/db-reader", result)
}

func TestDsTenantScopedRoleName_PreservesComplexName(t *testing.T) {
	// Role names with special characters are preserved
	result := dsTenantScopedRoleName("tenant-123", "role/with/slashes")
	assert.Equal(t, "tenant/tenant-123/role/with/slashes", result)
}

// =============================================================================
// dsTenantStripRoleName tests
// =============================================================================

func TestDsTenantStripRoleName_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := dsTenantStripRoleName("", "db-reader")
	assert.Equal(t, "db-reader", result)
}

func TestDsTenantStripRoleName_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := dsTenantStripRoleName("acme-corp", "tenant/acme-corp/db-reader")
	assert.Equal(t, "db-reader", result)
}

func TestDsTenantStripRoleName_NoMatchingPrefix(t *testing.T) {
	// If the role doesn't have the tenant prefix, it's returned as-is
	result := dsTenantStripRoleName("acme-corp", "other-role")
	assert.Equal(t, "other-role", result)
}

func TestDsTenantStripRoleName_WrongTenant(t *testing.T) {
	// If the role belongs to a different tenant, the prefix is not stripped
	result := dsTenantStripRoleName("acme-corp", "tenant/other-corp/db-reader")
	assert.Equal(t, "tenant/other-corp/db-reader", result)
}

// =============================================================================
// dsTenantRoleBelongsTo tests
// =============================================================================

func TestDsTenantRoleBelongsTo_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all roles belong
	assert.True(t, dsTenantRoleBelongsTo("", "any-role"))
	assert.True(t, dsTenantRoleBelongsTo("", "tenant/acme-corp/role"))
}

func TestDsTenantRoleBelongsTo_MatchingTenant(t *testing.T) {
	assert.True(t, dsTenantRoleBelongsTo("acme-corp", "tenant/acme-corp/db-reader"))
}

func TestDsTenantRoleBelongsTo_NonMatchingTenant(t *testing.T) {
	assert.False(t, dsTenantRoleBelongsTo("acme-corp", "tenant/other-corp/db-reader"))
}

func TestDsTenantRoleBelongsTo_UnscopedRole(t *testing.T) {
	// A role without tenant prefix does not belong to a multi-tenant request
	assert.False(t, dsTenantRoleBelongsTo("acme-corp", "db-reader"))
}

// =============================================================================
// dsTenantListPrefix tests
// =============================================================================

func TestDsTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", dsTenantListPrefix(""))
}

func TestDsTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant/acme-corp/", dsTenantListPrefix("acme-corp"))
}

// =============================================================================
// dsTenantScopedLeasePrefix tests
// =============================================================================

func TestDsTenantScopedLeasePrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "credentials/", dsTenantScopedLeasePrefix(""))
}

func TestDsTenantScopedLeasePrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "credentials/tenant/acme-corp/", dsTenantScopedLeasePrefix("acme-corp"))
}

// =============================================================================
// dsEnforceTenantAccess tests
// =============================================================================

func TestDsEnforceTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, dsEnforceTenantAccess("", "any-role", "GetRole"))
}

func TestDsEnforceTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, dsEnforceTenantAccess("acme-corp", "tenant/acme-corp/db-reader", "GetRole"))
}

func TestDsEnforceTenantAccess_CrossTenantDenied(t *testing.T) {
	err := dsEnforceTenantAccess("acme-corp", "tenant/other-corp/db-reader", "GetRole")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestDsEnforceTenantAccess_UnscopedRoleDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped role
	err := dsEnforceTenantAccess("acme-corp", "unscoped-role", "DeleteRole")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

// =============================================================================
// dsPrepareTenantContext tests
// =============================================================================

func TestDsPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	tenantID, err := prepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestDsPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "invalid/tenant")
	_, err := prepareTenantContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestDsTenantRoleName_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		roleName string
	}{
		{"empty-tenant", "", "db-reader"},
		{"with-tenant", "acme-corp", "db-writer"},
		{"complex-role", "tenant-123", "role/with/slashes"},
		{"underscore-tenant", "my_tenant", "cache-admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := dsTenantScopedRoleName(tt.tenantID, tt.roleName)
			stripped := dsTenantStripRoleName(tt.tenantID, scoped)
			assert.Equal(t, tt.roleName, stripped, "roundtrip should return original role name")
		})
	}
}

func TestDsTenantRoleName_ScopeAndBelongsTo(t *testing.T) {
	// A scoped role should belong to the tenant that scoped it
	tenantID := "acme-corp"
	roleName := "db-reader"
	scoped := dsTenantScopedRoleName(tenantID, roleName)
	assert.True(t, dsTenantRoleBelongsTo(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, dsTenantRoleBelongsTo("other-corp", scoped))
}

func TestDsTenantRoleName_ListPrefixMatchesScopedRoles(t *testing.T) {
	tenantID := "acme-corp"
	roleName := "db-reader"
	scoped := dsTenantScopedRoleName(tenantID, roleName)
	prefix := dsTenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped role should start with tenant list prefix")
}
