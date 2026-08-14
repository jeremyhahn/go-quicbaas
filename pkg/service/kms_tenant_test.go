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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// tenantScopedKeyID tests
// =============================================================================

func TestTenantScopedKeyID_EmptyTenant(t *testing.T) {
	// Single-tenant mode: key ID is returned unchanged
	keyID := "my-key"
	result := tenantScopedKeyID("", keyID)
	assert.Equal(t, keyID, result)
}

func TestTenantScopedKeyID_WithTenant(t *testing.T) {
	// Multi-tenant mode: key ID is prefixed with tenant namespace
	result := tenantScopedKeyID("acme-corp", "my-key")
	assert.Equal(t, "tenant/acme-corp/my-key", result)
}

func TestTenantScopedKeyID_PreservesComplexKeyID(t *testing.T) {
	// Key IDs with special characters are preserved
	result := tenantScopedKeyID("tenant-123", "key/with/slashes")
	assert.Equal(t, "tenant/tenant-123/key/with/slashes", result)
}

// =============================================================================
// tenantStripKeyID tests
// =============================================================================

func TestTenantStripKeyID_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := tenantStripKeyID("", "my-key")
	assert.Equal(t, "my-key", result)
}

func TestTenantStripKeyID_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := tenantStripKeyID("acme-corp", "tenant/acme-corp/my-key")
	assert.Equal(t, "my-key", result)
}

func TestTenantStripKeyID_NoMatchingPrefix(t *testing.T) {
	// If the key doesn't have the tenant prefix, it's returned as-is
	result := tenantStripKeyID("acme-corp", "other-key")
	assert.Equal(t, "other-key", result)
}

func TestTenantStripKeyID_WrongTenant(t *testing.T) {
	// If the key belongs to a different tenant, the prefix is not stripped
	result := tenantStripKeyID("acme-corp", "tenant/other-corp/my-key")
	assert.Equal(t, "tenant/other-corp/my-key", result)
}

// =============================================================================
// tenantKeyBelongsTo tests
// =============================================================================

func TestTenantKeyBelongsTo_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all keys belong
	assert.True(t, tenantKeyBelongsTo("", "any-key"))
	assert.True(t, tenantKeyBelongsTo("", "tenant/acme-corp/key"))
}

func TestTenantKeyBelongsTo_MatchingTenant(t *testing.T) {
	assert.True(t, tenantKeyBelongsTo("acme-corp", "tenant/acme-corp/my-key"))
}

func TestTenantKeyBelongsTo_NonMatchingTenant(t *testing.T) {
	assert.False(t, tenantKeyBelongsTo("acme-corp", "tenant/other-corp/my-key"))
}

func TestTenantKeyBelongsTo_UnscopedKey(t *testing.T) {
	// A key without tenant prefix does not belong to a multi-tenant request
	assert.False(t, tenantKeyBelongsTo("acme-corp", "my-key"))
}

// =============================================================================
// tenantListPrefix tests
// =============================================================================

func TestTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", tenantListPrefix(""))
}

func TestTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant/acme-corp/", tenantListPrefix("acme-corp"))
}

// =============================================================================
// extractTenantID tests
// =============================================================================

func TestExtractTenantID_NoTenant(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", extractTenantID(ctx))
}

func TestExtractTenantID_WithTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "my-tenant")
	assert.Equal(t, "my-tenant", extractTenantID(ctx))
}

func TestExtractTenantID_EmptyTenantValue(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "")
	assert.Equal(t, "", extractTenantID(ctx))
}

// =============================================================================
// validateTenantID tests
// =============================================================================

func TestValidateTenantID_Empty(t *testing.T) {
	// Empty is valid (single-tenant mode)
	assert.NoError(t, validateTenantID(""))
}

func TestValidateTenantID_ValidAlphanumeric(t *testing.T) {
	assert.NoError(t, validateTenantID("acme-corp"))
	assert.NoError(t, validateTenantID("tenant_123"))
	assert.NoError(t, validateTenantID("MyTenant"))
	assert.NoError(t, validateTenantID("a"))
	assert.NoError(t, validateTenantID("UPPERCASE"))
	assert.NoError(t, validateTenantID("mix-of_123-chars"))
}

func TestValidateTenantID_InvalidCharacters(t *testing.T) {
	// Special characters should fail
	err := validateTenantID("tenant/bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")

	err = validateTenantID("tenant@bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")

	err = validateTenantID("tenant bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")

	err = validateTenantID("tenant.bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestValidateTenantID_TooLong(t *testing.T) {
	// Maximum 128 characters
	longID := strings.Repeat("a", 129)
	err := validateTenantID(longID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")

	// Exactly 128 should pass
	maxID := strings.Repeat("a", 128)
	assert.NoError(t, validateTenantID(maxID))
}

// =============================================================================
// isValidTenantChar tests
// =============================================================================

func TestIsValidTenantChar(t *testing.T) {
	tests := []struct {
		char  rune
		valid bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'0', true},
		{'9', true},
		{'-', true},
		{'_', true},
		{' ', false},
		{'/', false},
		{'.', false},
		{'@', false},
		{'!', false},
		{'#', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.char), func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidTenantChar(tt.char))
		})
	}
}

// =============================================================================
// enforceTenantAccess tests
// =============================================================================

func TestEnforceTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, enforceTenantAccess("", "any-key", "GetKey"))
}

func TestEnforceTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, enforceTenantAccess("acme-corp", "tenant/acme-corp/my-key", "GetKey"))
}

func TestEnforceTenantAccess_CrossTenantDenied(t *testing.T) {
	err := enforceTenantAccess("acme-corp", "tenant/other-corp/my-key", "GetKey")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestEnforceTenantAccess_UnscopedKeyDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped key
	err := enforceTenantAccess("acme-corp", "unscoped-key", "DeleteKey")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

// =============================================================================
// prepareTenantContext tests
// =============================================================================

func TestPrepareTenantContext_NoTenant(t *testing.T) {
	ctx := context.Background()
	tenantID, err := prepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	tenantID, err := prepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "invalid/tenant")
	_, err := prepareTenantContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestTenantKeyID_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		keyID    string
	}{
		{"empty-tenant", "", "my-key"},
		{"with-tenant", "acme-corp", "my-key"},
		{"complex-key", "tenant-123", "sub/path/key"},
		{"underscore-tenant", "my_tenant", "key-name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := tenantScopedKeyID(tt.tenantID, tt.keyID)
			stripped := tenantStripKeyID(tt.tenantID, scoped)
			assert.Equal(t, tt.keyID, stripped, "roundtrip should return original key ID")
		})
	}
}

func TestTenantKeyID_ScopeAndBelongsTo(t *testing.T) {
	// A scoped key should belong to the tenant that scoped it
	tenantID := "acme-corp"
	keyID := "my-key"
	scoped := tenantScopedKeyID(tenantID, keyID)
	assert.True(t, tenantKeyBelongsTo(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, tenantKeyBelongsTo("other-corp", scoped))
}

func TestTenantKeyID_ListPrefixMatchesScopedKeys(t *testing.T) {
	tenantID := "acme-corp"
	keyID := "my-key"
	scoped := tenantScopedKeyID(tenantID, keyID)
	prefix := tenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped key should start with tenant list prefix")
}

// =============================================================================
// System-tenant scoping tests
// =============================================================================

// TestKMSTenant_SystemTenantIsPlatformScoped proves a caller carrying the
// literal system tenant is treated identically to single-tenant mode (no key
// prefixing, unrestricted access) rather than being isolated into a tenant
// namespace named "system".
func TestKMSTenant_SystemTenantIsPlatformScoped(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("tenantID="+tenantID, func(t *testing.T) {
			assert.Equal(t, "my-key", tenantScopedKeyID(tenantID, "my-key"))
			assert.True(t, tenantKeyBelongsTo(tenantID, "any-key"))
			assert.NoError(t, enforceTenantAccess(tenantID, "any-key", "GetKey"))
		})
	}
}

// TestKMSTenant_OrdinaryTenantStaysIsolated proves the system-tenant carve-out
// does not widen ordinary tenants' reach: a lookalike ID that merely shares
// the "system" prefix is still fully isolated.
func TestKMSTenant_OrdinaryTenantStaysIsolated(t *testing.T) {
	scoped := tenantScopedKeyID("system-2", "my-key")
	assert.Equal(t, "tenant/system-2/my-key", scoped)
	assert.False(t, tenantKeyBelongsTo("system-2", "my-key"))
	err := enforceTenantAccess("system-2", "my-key", "GetKey")
	require.Error(t, err)
}
