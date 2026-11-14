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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Key helper tests
// =============================================================================

func TestOsTenantScopedKey(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		key      string
		want     string
	}{
		{"empty tenant returns key unchanged", "", "my-bucket:my-object", "my-bucket:my-object"},
		{"tenant prefixes key", "acme-corp", "my-bucket:my-object", "tenant/acme-corp/my-bucket:my-object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, osTenantScopedKey(tt.tenantID, tt.key))
		})
	}
}

func TestOsTenantStripKey(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		key      string
		want     string
	}{
		{"unscoped key returned as-is", "acme-corp", "other-key", "other-key"},
		{"other tenant prefix not stripped", "acme-corp", "tenant/other-corp/my-bucket:my-object", "tenant/other-corp/my-bucket:my-object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, osTenantStripKey(tt.tenantID, tt.key))
		})
	}
}

func TestOsTenantKeyBelongsTo(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		key      string
		want     bool
	}{
		{"empty tenant owns unscoped key", "", "any-key", true},
		{"empty tenant owns scoped key", "", "tenant/acme-corp/key", true},
		{"tenant does not own unscoped key", "acme-corp", "my-object", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, osTenantKeyBelongsTo(tt.tenantID, tt.key))
		})
	}
}

func TestOsTenantListPrefix(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		want     string
	}{
		{"empty tenant has no prefix", "", ""},
		{"tenant prefix", "acme-corp", "tenant/acme-corp/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, osTenantListPrefix(tt.tenantID))
		})
	}
}

// =============================================================================
// Bucket helper tests
// =============================================================================

func TestOsTenantScopedBucket(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		bucket   string
		want     string
	}{
		{"empty tenant returns bucket unchanged", "", "my-bucket", "my-bucket"},
		{"tenant prefixes bucket", "acme-corp", "my-bucket", "tenant/acme-corp/my-bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, osTenantScopedBucket(tt.tenantID, tt.bucket))
		})
	}
}

func TestOsTenantStripBucket(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		bucket   string
		want     string
	}{
		{"unscoped bucket returned as-is", "acme-corp", "other-bucket", "other-bucket"},
		{"other tenant prefix not stripped", "acme-corp", "tenant/other-corp/my-bucket", "tenant/other-corp/my-bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, osTenantStripBucket(tt.tenantID, tt.bucket))
		})
	}
}

func TestOsTenantBucketBelongsTo(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		bucket   string
		want     bool
	}{
		{"empty tenant owns unscoped bucket", "", "any-bucket", true},
		{"empty tenant owns scoped bucket", "", "tenant/acme-corp/bucket", true},
		{"tenant does not own unscoped bucket", "acme-corp", "my-bucket", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, osTenantBucketBelongsTo(tt.tenantID, tt.bucket))
		})
	}
}

// =============================================================================
// Access enforcement tests
// =============================================================================

func TestOsEnforceTenantAccess(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		key       string
		operation string
		wantErr   bool
	}{
		{"empty tenant always allowed", "", "any-key", "PutObject", false},
		{"matching tenant allowed", "acme-corp", "tenant/acme-corp/my-object", "GetObject", false},
		{"cross tenant denied", "acme-corp", "tenant/other-corp/my-object", "GetObject", true},
		{"unscoped key denied", "acme-corp", "unscoped-key", "DeleteObject", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := osEnforceTenantAccess(tt.tenantID, tt.key, tt.operation)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
		})
	}
}

func TestOsEnforceBucketTenantAccess(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		bucket    string
		operation string
		wantErr   bool
	}{
		{"empty tenant always allowed", "", "any-bucket", "CreateBucket", false},
		{"matching tenant allowed", "acme-corp", "tenant/acme-corp/my-bucket", "ListBucket", false},
		{"cross tenant denied", "acme-corp", "tenant/other-corp/my-bucket", "DeleteBucket", true},
		{"unscoped bucket denied", "acme-corp", "unscoped-bucket", "ListBucket", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := osEnforceBucketTenantAccess(tt.tenantID, tt.bucket, tt.operation)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
		})
	}
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
