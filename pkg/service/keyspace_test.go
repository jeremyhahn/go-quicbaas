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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestKeyspaceResolver_BuildKey_MultiTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()

	// Set tenant context
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")

	key, err := resolver.BuildKey(ctx, "User", 12345)
	require.NoError(t, err)
	assert.Equal(t, "__t:acme-corp:User:12345", string(key))
}

func TestKeyspaceResolver_BuildKey_SingleTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()

	// No tenant context (single-tenant mode)
	ctx := tenantctx.WithSystem(context.Background())

	key, err := resolver.BuildKey(ctx, "User", 12345)
	require.NoError(t, err)
	assert.Equal(t, "User:12345", string(key))
}

func TestKeyspaceResolver_BuildKey_InvalidEntityType(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name       string
		entityType string
		wantErr    bool
	}{
		{"empty", "", true},
		{"reserved prefix", "__internal", true},
		{"starts with colon", ":invalid", true},
		{"valid", "User", false},
		{"valid with numbers", "User123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolver.BuildKey(ctx, tt.entityType, 1)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKeyspaceResolver_BuildPrefix_MultiTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithTenant(context.Background(), "beta-inc")

	prefix, err := resolver.BuildPrefix(ctx, "Organization")
	require.NoError(t, err)
	assert.Equal(t, "__t:beta-inc:Organization:", string(prefix))
}

func TestKeyspaceResolver_BuildPrefix_SingleTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithSystem(context.Background())

	prefix, err := resolver.BuildPrefix(ctx, "Organization")
	require.NoError(t, err)
	assert.Equal(t, "Organization:", string(prefix))
}

func TestKeyspaceResolver_BuildPrefix_InvalidEntityType(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithSystem(context.Background())

	_, err := resolver.BuildPrefix(ctx, "")
	require.Error(t, err)

	_, err = resolver.BuildPrefix(ctx, "__reserved")
	require.Error(t, err)
}

func TestKeyspaceResolver_BuildTenantPrefix(t *testing.T) {
	resolver := NewKeyspaceResolver()

	t.Run("multi-tenant", func(t *testing.T) {
		ctx := tenantctx.WithTenant(context.Background(), "tenant-xyz")
		prefix, err := resolver.BuildTenantPrefix(ctx)
		require.NoError(t, err)
		assert.Equal(t, "__t:tenant-xyz:", string(prefix))
	})

	t.Run("single-tenant", func(t *testing.T) {
		ctx := tenantctx.WithSystem(context.Background())
		prefix, err := resolver.BuildTenantPrefix(ctx)
		require.NoError(t, err)
		assert.Nil(t, prefix) // Empty for single-tenant mode
	})
}

func TestKeyspaceResolver_ParseKey_TenantKey(t *testing.T) {
	resolver := NewKeyspaceResolver()

	tenantID, entityType, id, err := resolver.ParseKey([]byte("__t:acme-corp:User:12345"))
	require.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
	assert.Equal(t, "User", entityType)
	assert.Equal(t, uint64(12345), id)
}

func TestKeyspaceResolver_ParseKey_LegacyKey(t *testing.T) {
	resolver := NewKeyspaceResolver()

	tenantID, entityType, id, err := resolver.ParseKey([]byte("User:12345"))
	require.NoError(t, err)
	assert.Equal(t, "", tenantID)
	assert.Equal(t, "User", entityType)
	assert.Equal(t, uint64(12345), id)
}

func TestKeyspaceResolver_ParseKey_SystemKey(t *testing.T) {
	resolver := NewKeyspaceResolver()

	_, _, _, err := resolver.ParseKey([]byte("__sys:cluster/config"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot parse system key")
}

func TestKeyspaceResolver_ParseKey_InvalidFormat(t *testing.T) {
	resolver := NewKeyspaceResolver()

	tests := []struct {
		name string
		key  string
	}{
		{"no separator", "invalid"},
		{"tenant key missing parts", "__t:tenant"},
		{"tenant key missing id", "__t:tenant:User"},
		{"invalid id", "User:notanumber"},
		{"tenant key invalid id", "__t:tenant:User:notanumber"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := resolver.ParseKey([]byte(tt.key))
			assert.Error(t, err)
		})
	}
}

func TestKeyspaceResolver_ParseKey_CompositeKeys(t *testing.T) {
	resolver := NewKeyspaceResolver()

	// Test tenant key with composite ID (has additional colons after the numeric ID)
	tenantID, entityType, id, err := resolver.ParseKey([]byte("__t:acme:User:123:extra:data"))
	require.NoError(t, err)
	assert.Equal(t, "acme", tenantID)
	assert.Equal(t, "User", entityType)
	assert.Equal(t, uint64(123), id)

	// Test legacy key with composite data
	_, entityType, id, err = resolver.ParseKey([]byte("User:456:extra"))
	require.NoError(t, err)
	assert.Equal(t, "User", entityType)
	assert.Equal(t, uint64(456), id)
}

func TestKeyspaceResolver_ValidateAccess_SystemKeyspace(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	err := resolver.ValidateAccess(ctx, []byte("__sys:cluster/config"))
	require.Error(t, err)
	assert.True(t, errors.IsSystemKeyspaceViolation(err))
}

func TestKeyspaceResolver_ValidateAccess_CrossTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	// Trying to access tenant-b's data
	err := resolver.ValidateAccess(ctx, []byte("__t:tenant-b:User:123"))
	require.Error(t, err)
	assert.True(t, errors.IsCrossTenantAccess(err))
}

func TestKeyspaceResolver_ValidateAccess_SameTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	// Accessing own data
	err := resolver.ValidateAccess(ctx, []byte("__t:tenant-a:User:123"))
	require.NoError(t, err)
}

func TestKeyspaceResolver_ValidateAccess_LegacyKeySingleTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithSystem(context.Background()) // No tenant

	// Single-tenant mode can access legacy keys
	err := resolver.ValidateAccess(ctx, []byte("User:123"))
	require.NoError(t, err)
}

func TestKeyspaceResolver_ValidateAccess_TenantKeyFromSingleTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithSystem(context.Background()) // No tenant

	// Single-tenant mode cannot access tenant-prefixed keys
	err := resolver.ValidateAccess(ctx, []byte("__t:some-tenant:User:123"))
	require.Error(t, err)
	assert.True(t, errors.IsCrossTenantAccess(err))
}

func TestKeyspaceResolver_ValidateAccess_LegacyKeyMultiTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	// Multi-tenant mode can still access legacy keys for backward compatibility
	err := resolver.ValidateAccess(ctx, []byte("User:123"))
	require.NoError(t, err)
}

func TestKeyspaceResolver_ValidateAccess_SystemKeyspaceNoTenant(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithSystem(context.Background()) // No tenant

	// Even without tenant context, system keyspace should be blocked
	err := resolver.ValidateAccess(ctx, []byte("__sys:cluster/config"))
	require.Error(t, err)
	assert.True(t, errors.IsSystemKeyspaceViolation(err))
}

func TestKeyspaceResolver_ValidateAccess_InvalidTenantKey(t *testing.T) {
	resolver := NewKeyspaceResolver()
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	// Invalid tenant key format should return error
	err := resolver.ValidateAccess(ctx, []byte("__t:invalid"))
	require.Error(t, err)
}

func TestKeyspaceResolver_IsSystemKey(t *testing.T) {
	resolver := NewKeyspaceResolver()

	assert.True(t, resolver.IsSystemKey([]byte("__sys:cluster/config")))
	assert.True(t, resolver.IsSystemKey([]byte("__sys:dek/tenant/abc")))
	assert.False(t, resolver.IsSystemKey([]byte("__t:tenant:User:1")))
	assert.False(t, resolver.IsSystemKey([]byte("User:1")))
}

func TestKeyspaceResolver_IsTenantKey(t *testing.T) {
	resolver := NewKeyspaceResolver()

	assert.True(t, resolver.IsTenantKey([]byte("__t:tenant:User:1")))
	assert.True(t, resolver.IsTenantKey([]byte("__t:acme-corp:Zone:999")))
	assert.False(t, resolver.IsTenantKey([]byte("__sys:cluster/config")))
	assert.False(t, resolver.IsTenantKey([]byte("User:1")))
}

func TestKeyspaceResolver_IsLegacyKey(t *testing.T) {
	resolver := NewKeyspaceResolver()

	assert.True(t, resolver.IsLegacyKey([]byte("User:1")))
	assert.True(t, resolver.IsLegacyKey([]byte("Organization:12345")))
	assert.False(t, resolver.IsLegacyKey([]byte("__sys:cluster/config")))
	assert.False(t, resolver.IsLegacyKey([]byte("__t:tenant:User:1")))
}

func TestKeyspaceResolver_BuildSystemKey(t *testing.T) {
	resolver := NewKeyspaceResolver()

	key := resolver.BuildSystemKey("dek/tenant/abc")
	assert.Equal(t, "__sys:dek/tenant/abc", string(key))

	key = resolver.BuildSystemKey("cluster/config")
	assert.Equal(t, "__sys:cluster/config", string(key))
}

func TestKeyspaceResolver_ValidateEntityType(t *testing.T) {
	resolver := NewKeyspaceResolver()

	tests := []struct {
		name       string
		entityType string
		wantErr    bool
	}{
		{"valid simple", "User", false},
		{"valid with numbers", "User123", false},
		{"valid with hyphen", "User-Profile", false},
		{"valid with underscore", "User_Profile", false},
		{"empty", "", true},
		{"reserved double underscore", "__System", true},
		{"reserved __internal", "__internal", true},
		{"starts with colon", ":User", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := resolver.ValidateEntityType(tt.entityType)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExtractEntityTypeFromKey(t *testing.T) {
	resolver := NewKeyspaceResolver()

	t.Run("tenant key", func(t *testing.T) {
		entityType, ok := ExtractEntityTypeFromKey(resolver, []byte("__t:tenant:User:123"))
		assert.True(t, ok)
		assert.Equal(t, "User", entityType)
	})

	t.Run("legacy key", func(t *testing.T) {
		entityType, ok := ExtractEntityTypeFromKey(resolver, []byte("Organization:456"))
		assert.True(t, ok)
		assert.Equal(t, "Organization", entityType)
	})

	t.Run("system key", func(t *testing.T) {
		_, ok := ExtractEntityTypeFromKey(resolver, []byte("__sys:config"))
		assert.False(t, ok)
	})

	t.Run("invalid key", func(t *testing.T) {
		_, ok := ExtractEntityTypeFromKey(resolver, []byte("invalid"))
		assert.False(t, ok)
	})
}

func TestKeyspaceResolver_MultiTenantIsolation(t *testing.T) {
	resolver := NewKeyspaceResolver()

	// Create keys for different tenants
	ctxA := tenantctx.WithTenant(context.Background(), "tenant-a")
	ctxB := tenantctx.WithTenant(context.Background(), "tenant-b")

	keyA, err := resolver.BuildKey(ctxA, "User", 1)
	require.NoError(t, err)

	keyB, err := resolver.BuildKey(ctxB, "User", 1)
	require.NoError(t, err)

	// Keys should be different even for same entity type and ID
	assert.NotEqual(t, string(keyA), string(keyB))
	assert.Equal(t, "__t:tenant-a:User:1", string(keyA))
	assert.Equal(t, "__t:tenant-b:User:1", string(keyB))

	// Tenant A should not be able to access Tenant B's data
	err = resolver.ValidateAccess(ctxA, keyB)
	assert.Error(t, err)
	assert.True(t, errors.IsCrossTenantAccess(err))

	// Tenant B should not be able to access Tenant A's data
	err = resolver.ValidateAccess(ctxB, keyA)
	assert.Error(t, err)
	assert.True(t, errors.IsCrossTenantAccess(err))

	// Each tenant can access their own data
	err = resolver.ValidateAccess(ctxA, keyA)
	assert.NoError(t, err)

	err = resolver.ValidateAccess(ctxB, keyB)
	assert.NoError(t, err)
}
