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
// osTenantScopedKey tests
// =============================================================================

func TestOsTenantScopedKey_EmptyTenant(t *testing.T) {
	// Single-tenant mode: key is returned unchanged
	key := "my-bucket:my-object"
	result := osTenantScopedKey("", key)
	assert.Equal(t, key, result)
}

func TestOsTenantScopedKey_WithTenant(t *testing.T) {
	// Multi-tenant mode: key is prefixed with tenant namespace
	result := osTenantScopedKey("acme-corp", "my-bucket:my-object")
	assert.Equal(t, "tenant/acme-corp/my-bucket:my-object", result)
}

func TestOsTenantScopedKey_PreservesComplexKey(t *testing.T) {
	// Keys with special characters are preserved
	result := osTenantScopedKey("tenant-123", "path/to/object.bin")
	assert.Equal(t, "tenant/tenant-123/path/to/object.bin", result)
}

// =============================================================================
// osTenantStripKey tests
// =============================================================================

func TestOsTenantStripKey_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := osTenantStripKey("", "my-bucket:my-object")
	assert.Equal(t, "my-bucket:my-object", result)
}

func TestOsTenantStripKey_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := osTenantStripKey("acme-corp", "tenant/acme-corp/my-bucket:my-object")
	assert.Equal(t, "my-bucket:my-object", result)
}

func TestOsTenantStripKey_NoMatchingPrefix(t *testing.T) {
	// If the key doesn't have the tenant prefix, it's returned as-is
	result := osTenantStripKey("acme-corp", "other-key")
	assert.Equal(t, "other-key", result)
}

func TestOsTenantStripKey_WrongTenant(t *testing.T) {
	// If the key belongs to a different tenant, the prefix is not stripped
	result := osTenantStripKey("acme-corp", "tenant/other-corp/my-bucket:my-object")
	assert.Equal(t, "tenant/other-corp/my-bucket:my-object", result)
}

// =============================================================================
// osTenantKeyBelongsTo tests
// =============================================================================

func TestOsTenantKeyBelongsTo_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all keys belong
	assert.True(t, osTenantKeyBelongsTo("", "any-key"))
	assert.True(t, osTenantKeyBelongsTo("", "tenant/acme-corp/key"))
}

func TestOsTenantKeyBelongsTo_MatchingTenant(t *testing.T) {
	assert.True(t, osTenantKeyBelongsTo("acme-corp", "tenant/acme-corp/my-object"))
}

func TestOsTenantKeyBelongsTo_NonMatchingTenant(t *testing.T) {
	assert.False(t, osTenantKeyBelongsTo("acme-corp", "tenant/other-corp/my-object"))
}

func TestOsTenantKeyBelongsTo_UnscopedKey(t *testing.T) {
	// A key without tenant prefix does not belong to a multi-tenant request
	assert.False(t, osTenantKeyBelongsTo("acme-corp", "my-object"))
}

// =============================================================================
// osTenantListPrefix tests
// =============================================================================

func TestOsTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", osTenantListPrefix(""))
}

func TestOsTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant/acme-corp/", osTenantListPrefix("acme-corp"))
}

// =============================================================================
// osTenantScopedBucket tests
// =============================================================================

func TestOsTenantScopedBucket_EmptyTenant(t *testing.T) {
	// Single-tenant mode: bucket name is returned unchanged
	result := osTenantScopedBucket("", "my-bucket")
	assert.Equal(t, "my-bucket", result)
}

func TestOsTenantScopedBucket_WithTenant(t *testing.T) {
	// Multi-tenant mode: bucket is prefixed with tenant namespace
	result := osTenantScopedBucket("acme-corp", "my-bucket")
	assert.Equal(t, "tenant/acme-corp/my-bucket", result)
}

func TestOsTenantScopedBucket_PreservesComplexName(t *testing.T) {
	result := osTenantScopedBucket("tenant-123", "nested/bucket/name")
	assert.Equal(t, "tenant/tenant-123/nested/bucket/name", result)
}

// =============================================================================
// osTenantStripBucket tests
// =============================================================================

func TestOsTenantStripBucket_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := osTenantStripBucket("", "my-bucket")
	assert.Equal(t, "my-bucket", result)
}

func TestOsTenantStripBucket_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := osTenantStripBucket("acme-corp", "tenant/acme-corp/my-bucket")
	assert.Equal(t, "my-bucket", result)
}

func TestOsTenantStripBucket_NoMatchingPrefix(t *testing.T) {
	result := osTenantStripBucket("acme-corp", "other-bucket")
	assert.Equal(t, "other-bucket", result)
}

func TestOsTenantStripBucket_WrongTenant(t *testing.T) {
	result := osTenantStripBucket("acme-corp", "tenant/other-corp/my-bucket")
	assert.Equal(t, "tenant/other-corp/my-bucket", result)
}

// =============================================================================
// osTenantBucketBelongsTo tests
// =============================================================================

func TestOsTenantBucketBelongsTo_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all buckets belong
	assert.True(t, osTenantBucketBelongsTo("", "any-bucket"))
	assert.True(t, osTenantBucketBelongsTo("", "tenant/acme-corp/bucket"))
}

func TestOsTenantBucketBelongsTo_MatchingTenant(t *testing.T) {
	assert.True(t, osTenantBucketBelongsTo("acme-corp", "tenant/acme-corp/my-bucket"))
}

func TestOsTenantBucketBelongsTo_NonMatchingTenant(t *testing.T) {
	assert.False(t, osTenantBucketBelongsTo("acme-corp", "tenant/other-corp/my-bucket"))
}

func TestOsTenantBucketBelongsTo_UnscopedBucket(t *testing.T) {
	assert.False(t, osTenantBucketBelongsTo("acme-corp", "my-bucket"))
}

// =============================================================================
// osEnforceTenantAccess tests
// =============================================================================

func TestOsEnforceTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, osEnforceTenantAccess("", "any-key", "PutObject"))
}

func TestOsEnforceTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, osEnforceTenantAccess("acme-corp", "tenant/acme-corp/my-object", "GetObject"))
}

func TestOsEnforceTenantAccess_CrossTenantDenied(t *testing.T) {
	err := osEnforceTenantAccess("acme-corp", "tenant/other-corp/my-object", "GetObject")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestOsEnforceTenantAccess_UnscopedKeyDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped key
	err := osEnforceTenantAccess("acme-corp", "unscoped-key", "DeleteObject")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

// =============================================================================
// osEnforceBucketTenantAccess tests
// =============================================================================

func TestOsEnforceBucketTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, osEnforceBucketTenantAccess("", "any-bucket", "CreateBucket"))
}

func TestOsEnforceBucketTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, osEnforceBucketTenantAccess("acme-corp", "tenant/acme-corp/my-bucket", "ListBucket"))
}

func TestOsEnforceBucketTenantAccess_CrossTenantDenied(t *testing.T) {
	err := osEnforceBucketTenantAccess("acme-corp", "tenant/other-corp/my-bucket", "DeleteBucket")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestOsEnforceBucketTenantAccess_UnscopedBucketDenied(t *testing.T) {
	err := osEnforceBucketTenantAccess("acme-corp", "unscoped-bucket", "ListBucket")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

// =============================================================================
// osPrepareTenantContext tests
// =============================================================================

func TestOsPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	tenantID, err := prepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestOsPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "invalid/tenant")
	_, err := prepareTenantContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestOsTenantKey_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		key      string
	}{
		{"empty-tenant", "", "bucket:object"},
		{"with-tenant", "acme-corp", "bucket:object"},
		{"complex-key", "tenant-123", "path/to/deep/object.bin"},
		{"underscore-tenant", "my_tenant", "data:record-001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := osTenantScopedKey(tt.tenantID, tt.key)
			stripped := osTenantStripKey(tt.tenantID, scoped)
			assert.Equal(t, tt.key, stripped, "roundtrip should return original key")
		})
	}
}

func TestOsTenantBucket_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		bucket   string
	}{
		{"empty-tenant", "", "my-bucket"},
		{"with-tenant", "acme-corp", "data-bucket"},
		{"complex-bucket", "tenant-123", "nested/bucket"},
		{"underscore-tenant", "my_tenant", "archive-bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := osTenantScopedBucket(tt.tenantID, tt.bucket)
			stripped := osTenantStripBucket(tt.tenantID, scoped)
			assert.Equal(t, tt.bucket, stripped, "roundtrip should return original bucket")
		})
	}
}

func TestOsTenantKey_ScopeAndBelongsTo(t *testing.T) {
	// A scoped key should belong to the tenant that scoped it
	tenantID := "acme-corp"
	key := "bucket:object"
	scoped := osTenantScopedKey(tenantID, key)
	assert.True(t, osTenantKeyBelongsTo(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, osTenantKeyBelongsTo("other-corp", scoped))
}

func TestOsTenantBucket_ScopeAndBelongsTo(t *testing.T) {
	// A scoped bucket should belong to the tenant that scoped it
	tenantID := "acme-corp"
	bucket := "my-bucket"
	scoped := osTenantScopedBucket(tenantID, bucket)
	assert.True(t, osTenantBucketBelongsTo(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, osTenantBucketBelongsTo("other-corp", scoped))
}

func TestOsTenantKey_ListPrefixMatchesScopedKeys(t *testing.T) {
	tenantID := "acme-corp"
	key := "bucket:object"
	scoped := osTenantScopedKey(tenantID, key)
	prefix := osTenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped key should start with tenant list prefix")
}
