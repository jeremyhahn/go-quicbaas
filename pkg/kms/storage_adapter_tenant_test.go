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

package kms

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// NewKVStoreStorageAdapterWithTenant Constructor Tests
// =============================================================================

func TestNewKVStoreStorageAdapterWithTenant_NilKVStore(t *testing.T) {
	adapter, err := NewKVStoreStorageAdapterWithTenant(nil, "tenant-1")
	assert.Nil(t, adapter)
	assert.ErrorIs(t, err, ErrNilStorage)
}

func TestNewKVStoreStorageAdapterWithTenant_ValidKVStore(t *testing.T) {
	mockStore := NewMockKVStore()

	t.Run("with tenant ID", func(t *testing.T) {
		adapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-1")
		require.NoError(t, err)
		assert.NotNil(t, adapter)
	})

	t.Run("with empty tenant ID (single-tenant mode)", func(t *testing.T) {
		adapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "")
		require.NoError(t, err)
		assert.NotNil(t, adapter)
	})
}

// =============================================================================
// buildKey Tests
// =============================================================================

func TestBuildKey_SingleTenant(t *testing.T) {
	adapter := &KVStoreStorageAdapter{tenantID: ""}

	t.Run("standard key", func(t *testing.T) {
		result := adapter.buildKey("my-key")
		assert.Equal(t, "kms/keys/my-key", result)
	})

	t.Run("empty key", func(t *testing.T) {
		result := adapter.buildKey("")
		assert.Equal(t, "kms/keys/", result)
	})

	t.Run("nested key path", func(t *testing.T) {
		result := adapter.buildKey("app/db/encryption-key")
		assert.Equal(t, "kms/keys/app/db/encryption-key", result)
	})
}

func TestBuildKey_MultiTenant(t *testing.T) {
	adapter := &KVStoreStorageAdapter{tenantID: "acme-corp"}

	t.Run("standard key", func(t *testing.T) {
		result := adapter.buildKey("my-key")
		assert.Equal(t, "kms/tenant/acme-corp/keys/my-key", result)
	})

	t.Run("empty key", func(t *testing.T) {
		result := adapter.buildKey("")
		assert.Equal(t, "kms/tenant/acme-corp/keys/", result)
	})

	t.Run("nested key path", func(t *testing.T) {
		result := adapter.buildKey("app/db/encryption-key")
		assert.Equal(t, "kms/tenant/acme-corp/keys/app/db/encryption-key", result)
	})
}

// =============================================================================
// Backward Compatibility: Empty tenantID uses "kms/keys/" prefix
// =============================================================================

func TestTenantAdapter_BackwardCompatible_EmptyTenantID(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "")
	require.NoError(t, err)

	// Put through tenant adapter with empty tenantID
	err = adapter.Put(ctx, "test-key", []byte("test-value"))
	require.NoError(t, err)

	// Verify it uses the standard "kms/keys/" prefix
	fullKey := KeyPrefix + "test-key"
	storedValue, exists := mockStore.data[fullKey]
	assert.True(t, exists, "key should be stored with standard prefix")
	assert.Equal(t, []byte("test-value"), storedValue)

	// Get should also work
	value, err := adapter.Get(ctx, "test-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("test-value"), value)
}

func TestTenantAdapter_BackwardCompatible_OriginalConstructor(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()

	// Create via original constructor
	originalAdapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Create via tenant constructor with empty tenant
	tenantAdapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "")
	require.NoError(t, err)

	// Write with original adapter
	err = originalAdapter.Put(ctx, "shared-key", []byte("original-value"))
	require.NoError(t, err)

	// Read with tenant adapter (empty tenant) should find the same key
	value, err := tenantAdapter.Get(ctx, "shared-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("original-value"), value)

	// Write with tenant adapter (empty tenant)
	err = tenantAdapter.Put(ctx, "tenant-key", []byte("tenant-value"))
	require.NoError(t, err)

	// Read with original adapter should find the same key
	value, err = originalAdapter.Get(ctx, "tenant-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("tenant-value"), value)
}

// =============================================================================
// Multi-Tenant: tenantID uses "kms/tenant/{tenantID}/keys/" prefix
// =============================================================================

func TestTenantAdapter_TenantScoped_PutGet(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "acme-corp")
	require.NoError(t, err)

	err = adapter.Put(ctx, "encryption-key", []byte("key-material"))
	require.NoError(t, err)

	// Verify the storage key uses tenant-scoped prefix
	expectedKey := "kms/tenant/acme-corp/keys/encryption-key"
	storedValue, exists := mockStore.data[expectedKey]
	assert.True(t, exists, "key should be stored under tenant prefix")
	assert.Equal(t, []byte("key-material"), storedValue)

	// Get should retrieve correctly
	value, err := adapter.Get(ctx, "encryption-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("key-material"), value)
}

func TestTenantAdapter_TenantScoped_Delete(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "acme-corp")
	require.NoError(t, err)

	// Put a key
	err = adapter.Put(ctx, "to-delete", []byte("value"))
	require.NoError(t, err)

	// Delete it
	err = adapter.Delete(ctx, "to-delete")
	require.NoError(t, err)

	// Verify it is deleted from storage
	expectedKey := "kms/tenant/acme-corp/keys/to-delete"
	_, exists := mockStore.data[expectedKey]
	assert.False(t, exists)

	// Get should return not found
	_, err = adapter.Get(ctx, "to-delete")
	assert.Error(t, err)
}

func TestTenantAdapter_TenantScoped_Exists(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "acme-corp")
	require.NoError(t, err)

	// Key doesn't exist
	exists, err := adapter.Exists(ctx, "missing-key")
	require.NoError(t, err)
	assert.False(t, exists)

	// Put a key
	err = adapter.Put(ctx, "present-key", []byte("value"))
	require.NoError(t, err)

	// Now it exists
	exists, err = adapter.Exists(ctx, "present-key")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestTenantAdapter_TenantScoped_List(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "acme-corp")
	require.NoError(t, err)

	// Put some keys
	err = adapter.Put(ctx, "app/key1", []byte("v1"))
	require.NoError(t, err)
	err = adapter.Put(ctx, "app/key2", []byte("v2"))
	require.NoError(t, err)
	err = adapter.Put(ctx, "db/key3", []byte("v3"))
	require.NoError(t, err)

	t.Run("list all", func(t *testing.T) {
		keys, listErr := adapter.List(ctx, "")
		require.NoError(t, listErr)
		assert.Len(t, keys, 3)
	})

	t.Run("list with prefix", func(t *testing.T) {
		keys, listErr := adapter.List(ctx, "app/")
		require.NoError(t, listErr)
		assert.Len(t, keys, 2)
		for _, key := range keys {
			assert.True(t, strings.HasPrefix(key, "app/"))
		}
	})

	t.Run("list with no matches", func(t *testing.T) {
		keys, listErr := adapter.List(ctx, "nonexistent/")
		require.NoError(t, listErr)
		assert.Empty(t, keys)
	})
}

// =============================================================================
// Tenant Isolation Tests
// =============================================================================

func TestTenantAdapter_Isolation_DifferentTenantsCantSeeEachOther(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()

	// Create two tenant-scoped adapters
	tenantA, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-a")
	require.NoError(t, err)
	tenantB, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-b")
	require.NoError(t, err)

	// Tenant A stores a key
	err = tenantA.Put(ctx, "secret-key", []byte("tenant-a-secret"))
	require.NoError(t, err)

	// Tenant B stores a key with the same name
	err = tenantB.Put(ctx, "secret-key", []byte("tenant-b-secret"))
	require.NoError(t, err)

	// Tenant A should see their own key
	valueA, err := tenantA.Get(ctx, "secret-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("tenant-a-secret"), valueA)

	// Tenant B should see their own key
	valueB, err := tenantB.Get(ctx, "secret-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("tenant-b-secret"), valueB)

	// Verify they are stored at different paths in the underlying store
	_, existsA := mockStore.data["kms/tenant/tenant-a/keys/secret-key"]
	_, existsB := mockStore.data["kms/tenant/tenant-b/keys/secret-key"]
	assert.True(t, existsA, "tenant-a key should exist at tenant-scoped path")
	assert.True(t, existsB, "tenant-b key should exist at tenant-scoped path")
}

func TestTenantAdapter_Isolation_ListOnlyShowsOwnKeys(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()

	tenantA, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-a")
	require.NoError(t, err)
	tenantB, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-b")
	require.NoError(t, err)

	// Tenant A stores 3 keys
	err = tenantA.Put(ctx, "key1", []byte("a1"))
	require.NoError(t, err)
	err = tenantA.Put(ctx, "key2", []byte("a2"))
	require.NoError(t, err)
	err = tenantA.Put(ctx, "key3", []byte("a3"))
	require.NoError(t, err)

	// Tenant B stores 2 keys
	err = tenantB.Put(ctx, "key1", []byte("b1"))
	require.NoError(t, err)
	err = tenantB.Put(ctx, "key4", []byte("b4"))
	require.NoError(t, err)

	// Tenant A should only see 3 keys
	keysA, err := tenantA.List(ctx, "")
	require.NoError(t, err)
	assert.Len(t, keysA, 3)

	// Tenant B should only see 2 keys
	keysB, err := tenantB.List(ctx, "")
	require.NoError(t, err)
	assert.Len(t, keysB, 2)
}

func TestTenantAdapter_Isolation_DeleteOnlyAffectsOwnKeys(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()

	tenantA, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-a")
	require.NoError(t, err)
	tenantB, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-b")
	require.NoError(t, err)

	// Both tenants store a key with the same name
	err = tenantA.Put(ctx, "shared-name", []byte("a-value"))
	require.NoError(t, err)
	err = tenantB.Put(ctx, "shared-name", []byte("b-value"))
	require.NoError(t, err)

	// Delete tenant A's key
	err = tenantA.Delete(ctx, "shared-name")
	require.NoError(t, err)

	// Tenant A can no longer see it
	_, err = tenantA.Get(ctx, "shared-name")
	assert.Error(t, err)

	// Tenant B's key is unaffected
	valueB, err := tenantB.Get(ctx, "shared-name")
	require.NoError(t, err)
	assert.Equal(t, []byte("b-value"), valueB)
}

func TestTenantAdapter_Isolation_ExistsOnlyChecksOwnKeys(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()

	tenantA, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-a")
	require.NoError(t, err)
	tenantB, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-b")
	require.NoError(t, err)

	// Only tenant A has the key
	err = tenantA.Put(ctx, "only-mine", []byte("value"))
	require.NoError(t, err)

	// Tenant A sees it
	exists, err := tenantA.Exists(ctx, "only-mine")
	require.NoError(t, err)
	assert.True(t, exists)

	// Tenant B does not see it
	exists, err = tenantB.Exists(ctx, "only-mine")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestTenantAdapter_Isolation_TenantCannotSeeSingleTenantKeys(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()

	// Single-tenant adapter
	singleAdapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Multi-tenant adapter
	tenantAdapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-1")
	require.NoError(t, err)

	// Single-tenant stores a key
	err = singleAdapter.Put(ctx, "global-key", []byte("global-value"))
	require.NoError(t, err)

	// Tenant adapter cannot see it
	_, err = tenantAdapter.Get(ctx, "global-key")
	assert.Error(t, err, "tenant adapter should not see single-tenant keys")

	// Tenant stores a key
	err = tenantAdapter.Put(ctx, "tenant-key", []byte("tenant-value"))
	require.NoError(t, err)

	// Single-tenant adapter cannot see tenant key
	_, err = singleAdapter.Get(ctx, "tenant-key")
	assert.Error(t, err, "single-tenant adapter should not see tenant-scoped keys")
}

// =============================================================================
// storagePrefix / buildListPrefix Tests
// =============================================================================

func TestStoragePrefix_SingleTenant(t *testing.T) {
	adapter := &KVStoreStorageAdapter{tenantID: ""}
	assert.Equal(t, "kms/keys/", adapter.storagePrefix())
}

func TestStoragePrefix_MultiTenant(t *testing.T) {
	adapter := &KVStoreStorageAdapter{tenantID: "my-tenant"}
	assert.Equal(t, "kms/tenant/my-tenant/keys/", adapter.storagePrefix())
}

func TestBuildListPrefix_SingleTenant(t *testing.T) {
	adapter := &KVStoreStorageAdapter{tenantID: ""}
	assert.Equal(t, "kms/keys/app/", adapter.buildListPrefix("app/"))
	assert.Equal(t, "kms/keys/", adapter.buildListPrefix(""))
}

func TestBuildListPrefix_MultiTenant(t *testing.T) {
	adapter := &KVStoreStorageAdapter{tenantID: "my-tenant"}
	assert.Equal(t, "kms/tenant/my-tenant/keys/app/", adapter.buildListPrefix("app/"))
	assert.Equal(t, "kms/tenant/my-tenant/keys/", adapter.buildListPrefix(""))
}

// =============================================================================
// TenantKeyPrefix Constant Test
// =============================================================================

func TestTenantKeyPrefix(t *testing.T) {
	assert.Equal(t, "kms/tenant/", TenantKeyPrefix)
}

// =============================================================================
// Close Test for Tenant Adapter
// =============================================================================

func TestTenantAdapter_Close(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapterWithTenant(mockStore, "tenant-1")
	require.NoError(t, err)

	// Close should be a no-op and return nil
	err = adapter.Close()
	require.NoError(t, err)

	// Should still be able to use the adapter after close
	err = adapter.Put(ctx, "after-close", []byte("value"))
	assert.NoError(t, err)
}
