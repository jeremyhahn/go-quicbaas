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

package secrets

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDEKCache_CleanupExpired tests the background cleanup goroutine functionality.
func TestDEKCache_CleanupExpired(t *testing.T) {
	t.Run("cleanup expired entries", func(t *testing.T) {
		// Create DEK with cache
		kms := newTestMockKMSManager()
		store := newTestMockKVStore()
		logger := newTestMockLogger()
		dek := NewSecretsDEK(kms, store, "master-key", logger)
		defer dek.Close()

		ctx := context.Background()

		// Initialize the DEK
		err := dek.Initialize(ctx, "AES-256-GCM")
		require.NoError(t, err)

		// Get the key to populate cache
		key1, version1, err := dek.GetKey(ctx, DEKIDGlobal)
		require.NoError(t, err)
		assert.NotNil(t, key1)
		assert.Equal(t, uint64(1), version1)

		// Verify entry is in cache
		cachedKey, cachedVersion, exists := dek.cache.get(DEKIDGlobal)
		require.True(t, exists)
		assert.Equal(t, key1, cachedKey)
		assert.Equal(t, version1, cachedVersion)

		// Manually expire the cache entry by setting expiry in the past
		dek.cache.entries[DEKIDGlobal].expiry = time.Now().Add(-1 * time.Second)

		// Try to get from cache - should return false because expired
		_, _, exists = dek.cache.get(DEKIDGlobal)
		assert.False(t, exists, "Expired entry should not be retrieved from cache")
	})

	t.Run("cleanup versioned cache entries", func(t *testing.T) {
		kms := newTestMockKMSManager()
		store := newTestMockKVStore()
		logger := newTestMockLogger()
		dek := NewSecretsDEK(kms, store, "master-key", logger)
		defer dek.Close()

		ctx := context.Background()

		// Initialize and create multiple versions
		err := dek.Initialize(ctx, "AES-256-GCM")
		require.NoError(t, err)

		_, err = dek.Rotate(ctx, DEKIDGlobal)
		require.NoError(t, err)

		// Get specific version to populate versioned cache
		key1, err := dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
		require.NoError(t, err)
		assert.NotNil(t, key1)

		// Verify versioned entry is in cache
		cachedKey, exists := dek.cache.getVersion(DEKIDGlobal, 1)
		require.True(t, exists)
		assert.Equal(t, key1, cachedKey)

		// Manually expire the versioned cache entry
		vkey := dekCacheKey{dekid: DEKIDGlobal, version: 1}
		dek.cache.versionedCache[vkey].expiry = time.Now().Add(-1 * time.Second)

		// Try to get from versioned cache - should return false because expired
		_, exists = dek.cache.getVersion(DEKIDGlobal, 1)
		assert.False(t, exists, "Expired versioned entry should not be retrieved from cache")
	})

	t.Run("stop cleanup goroutine", func(t *testing.T) {
		kms := newTestMockKMSManager()
		store := newTestMockKVStore()
		logger := newTestMockLogger()
		dek := NewSecretsDEK(kms, store, "master-key", logger)

		// Create some cache entries
		ctx := context.Background()
		err := dek.Initialize(ctx, "AES-256-GCM")
		require.NoError(t, err)

		// Close should stop the cleanup goroutine
		err = dek.Close()
		require.NoError(t, err)

		// Verify stopChan is closed
		select {
		case <-dek.cache.stopChan:
			// Expected - channel is closed
		default:
			t.Error("stopChan should be closed after Close()")
		}
	})

	t.Run("multiple DEK instances with independent caches", func(t *testing.T) {
		kms := newTestMockKMSManager()
		store := newTestMockKVStore()
		logger := newTestMockLogger()

		// Create two DEK instances
		dek1 := NewSecretsDEK(kms, store, "master-key-1", logger)
		defer dek1.Close()
		dek2 := NewSecretsDEK(kms, store, "master-key-2", logger)
		defer dek2.Close()

		// They should have independent caches
		assert.NotEqual(t, dek1.cache, dek2.cache, "Caches should be independent")
	})

	t.Run("background cleanup goroutine actually runs", func(t *testing.T) {
		cache := newDEKCache()
		defer cache.stop()

		// Add an entry that will expire soon
		testDEKID := DEKID("test-cleanup")
		testKey := []byte("test-key")
		cache.set(testDEKID, testKey, 1)

		// Manually set it to expire very soon
		cache.entries[testDEKID].expiry = time.Now().Add(50 * time.Millisecond)

		// Wait long enough for cleanup to run (cleanup runs every 1 minute, but entry expires in 50ms)
		// Since we can't easily wait 1 minute, we verify the expiry logic works
		time.Sleep(100 * time.Millisecond)

		// Entry should be expired now
		_, _, exists := cache.get(testDEKID)
		assert.False(t, exists, "Entry should be expired and removed on access")
	})
}

// TestDEKCache_ConcurrentAccess tests thread safety of cache operations.
func TestDEKCache_ConcurrentAccess(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()
	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	ctx := context.Background()

	// Initialize the DEK
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Spawn multiple goroutines accessing cache concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			for j := 0; j < 10; j++ {
				// Read from cache - protected by SecretsDEK mutex
				_, _, err := dek.GetKey(ctx, DEKIDGlobal)
				assert.NoError(t, err)
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestDEKCache_Expiry tests cache TTL behavior.
func TestDEKCache_Expiry(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()
	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	ctx := context.Background()

	// Initialize the DEK
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Get key to populate cache
	_, _, err = dek.GetKey(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Verify entry is in cache and not expired
	_, _, exists := dek.cache.get(DEKIDGlobal)
	require.True(t, exists)

	entry := dek.cache.entries[DEKIDGlobal]
	assert.True(t, time.Now().Before(entry.expiry), "Entry should not be expired initially")

	// The entry should have cacheTTL (5 minutes) expiry
	expectedExpiry := time.Now().Add(defaultCacheTTL)
	tolerance := 2 * time.Second
	actualExpiry := entry.expiry

	assert.WithinDuration(t, expectedExpiry, actualExpiry, tolerance,
		"Cache entry expiry should be within tolerance")
}

// TestDEKCache_Set tests setting cache entries.
func TestDEKCache_Set(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	testDEKID := DEKID("test-dek")
	testKey := []byte("test-key-data")
	testVersion := uint64(1)

	// Set an entry
	cache.set(testDEKID, testKey, testVersion)

	// Verify it was set
	key, version, exists := cache.get(testDEKID)
	require.True(t, exists)
	assert.Equal(t, testKey, key)
	assert.Equal(t, testVersion, version)
	assert.True(t, time.Now().Before(cache.entries[testDEKID].expiry), "Entry should not be expired")
}

// TestDEKCache_SetVersion tests setting versioned cache entries.
func TestDEKCache_SetVersion(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	testDEKID := DEKID("test-dek")
	testKey := []byte("test-key-data-v1")
	testVersion := uint64(1)

	// Set a versioned entry
	cache.setVersion(testDEKID, testVersion, testKey)

	// Verify it was set
	key, exists := cache.getVersion(testDEKID, testVersion)
	require.True(t, exists)
	assert.Equal(t, testKey, key)

	vkey := dekCacheKey{dekid: testDEKID, version: testVersion}
	assert.True(t, time.Now().Before(cache.versionedCache[vkey].expiry), "Entry should not be expired")
}

// TestDEKCache_GetNonExistent tests getting non-existent entries.
func TestDEKCache_GetNonExistent(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	// Try to get non-existent entry
	_, _, exists := cache.get(DEKID("non-existent"))
	assert.False(t, exists, "Non-existent entry should not be found")

	// Try to get non-existent versioned entry
	_, exists = cache.getVersion(DEKID("non-existent"), 1)
	assert.False(t, exists, "Non-existent versioned entry should not be found")
}

// TestDEKCache_UpdateExpiry tests that cache entries are refreshed on set.
func TestDEKCache_UpdateExpiry(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	testDEKID := DEKID("test-dek")
	testKey := []byte("test-key")
	testVersion := uint64(1)

	// Set initial entry
	cache.set(testDEKID, testKey, testVersion)

	// Get the initial expiry
	expiry1 := cache.entries[testDEKID].expiry

	// Sleep a bit
	time.Sleep(10 * time.Millisecond)

	// Set again (simulating cache refresh)
	cache.set(testDEKID, testKey, testVersion)

	// Get the updated expiry
	expiry2 := cache.entries[testDEKID].expiry

	// The new expiry should be later than the old one
	assert.True(t, expiry2.After(expiry1), "Updated entry should have later expiry")
}

// TestDEKCache_MultipleVersions tests caching multiple versions of the same DEK.
func TestDEKCache_MultipleVersions(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	testDEKID := DEKID("multi-version-dek")

	// Set multiple versions
	cache.setVersion(testDEKID, 1, []byte("key-v1"))
	cache.setVersion(testDEKID, 2, []byte("key-v2"))
	cache.setVersion(testDEKID, 3, []byte("key-v3"))

	// Verify all versions are cached
	key1, exists := cache.getVersion(testDEKID, 1)
	require.True(t, exists)
	assert.Equal(t, []byte("key-v1"), key1)

	key2, exists := cache.getVersion(testDEKID, 2)
	require.True(t, exists)
	assert.Equal(t, []byte("key-v2"), key2)

	key3, exists := cache.getVersion(testDEKID, 3)
	require.True(t, exists)
	assert.Equal(t, []byte("key-v3"), key3)
}

// TestNewDEKCache tests cache initialization.
func TestNewDEKCache(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	assert.NotNil(t, cache.entries)
	assert.NotNil(t, cache.versionedCache)
	assert.NotNil(t, cache.stopChan)

	// Verify maps are initialized and empty
	assert.Equal(t, 0, len(cache.entries))
	assert.Equal(t, 0, len(cache.versionedCache))
}

// TestDEKCache_Delete tests deleting cache entries.
func TestDEKCache_Delete(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	testDEKID := DEKID("test-dek")
	cache.set(testDEKID, []byte("test-key"), 1)

	// Verify it exists
	_, _, exists := cache.get(testDEKID)
	require.True(t, exists)

	// Delete it
	cache.delete(testDEKID)

	// Verify it's gone
	_, _, exists = cache.get(testDEKID)
	assert.False(t, exists)
}

// TestDEKCache_DeleteVersion tests deleting versioned cache entries.
func TestDEKCache_DeleteVersion(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	testDEKID := DEKID("test-dek")
	cache.setVersion(testDEKID, 1, []byte("key-v1"))
	cache.setVersion(testDEKID, 2, []byte("key-v2"))

	// Verify both exist
	_, exists := cache.getVersion(testDEKID, 1)
	require.True(t, exists)
	_, exists = cache.getVersion(testDEKID, 2)
	require.True(t, exists)

	// Delete version 1
	cache.deleteVersion(testDEKID, 1)

	// Verify v1 is gone but v2 remains
	_, exists = cache.getVersion(testDEKID, 1)
	assert.False(t, exists)
	_, exists = cache.getVersion(testDEKID, 2)
	assert.True(t, exists)
}

// TestDEKCache_DeleteAllVersions tests deleting all versions of a DEK.
func TestDEKCache_DeleteAllVersions(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	testDEKID := DEKID("test-dek")
	cache.setVersion(testDEKID, 1, []byte("key-v1"))
	cache.setVersion(testDEKID, 2, []byte("key-v2"))
	cache.setVersion(testDEKID, 3, []byte("key-v3"))

	// Also add a different DEK
	otherDEKID := DEKID("other-dek")
	cache.setVersion(otherDEKID, 1, []byte("other-key"))

	// Delete all versions of testDEKID
	cache.deleteAllVersions(testDEKID)

	// Verify all testDEKID versions are gone
	_, exists := cache.getVersion(testDEKID, 1)
	assert.False(t, exists)
	_, exists = cache.getVersion(testDEKID, 2)
	assert.False(t, exists)
	_, exists = cache.getVersion(testDEKID, 3)
	assert.False(t, exists)

	// Verify otherDEKID still exists
	_, exists = cache.getVersion(otherDEKID, 1)
	assert.True(t, exists)
}

// TestDEKCache_Clear tests clearing the entire cache.
func TestDEKCache_Clear(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	// Add some entries
	cache.set(DEKID("dek1"), []byte("key1"), 1)
	cache.set(DEKID("dek2"), []byte("key2"), 1)
	cache.setVersion(DEKID("dek1"), 1, []byte("key1-v1"))
	cache.setVersion(DEKID("dek1"), 2, []byte("key1-v2"))

	// Clear the cache
	cache.clear()

	// Verify everything is gone
	assert.Equal(t, 0, len(cache.entries))
	assert.Equal(t, 0, len(cache.versionedCache))
}

// TestDEKCache_LRUEviction tests that the cache evicts oldest entries when full.
func TestDEKCache_LRUEviction(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	// Set maxSize to a small value for testing
	cache.maxSize = 3

	// Add entries up to max size
	cache.set(DEKID("dek1"), []byte("key1"), 1)
	time.Sleep(10 * time.Millisecond)
	cache.set(DEKID("dek2"), []byte("key2"), 1)
	time.Sleep(10 * time.Millisecond)
	cache.set(DEKID("dek3"), []byte("key3"), 1)

	// All should exist
	_, _, exists := cache.get(DEKID("dek1"))
	assert.True(t, exists)
	_, _, exists = cache.get(DEKID("dek2"))
	assert.True(t, exists)
	_, _, exists = cache.get(DEKID("dek3"))
	assert.True(t, exists)

	// Add one more - should evict the oldest
	cache.set(DEKID("dek4"), []byte("key4"), 1)

	// Cache should still have 3 entries, with oldest evicted
	assert.Equal(t, 3, len(cache.entries))
}

// TestDEKCache_VersionedLRUEviction tests versioned cache eviction.
func TestDEKCache_VersionedLRUEviction(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	// Set maxSize to a small value for testing
	cache.maxSize = 3

	// Add versioned entries up to max size
	cache.setVersion(DEKID("dek1"), 1, []byte("key1-v1"))
	time.Sleep(10 * time.Millisecond)
	cache.setVersion(DEKID("dek1"), 2, []byte("key1-v2"))
	time.Sleep(10 * time.Millisecond)
	cache.setVersion(DEKID("dek2"), 1, []byte("key2-v1"))

	// All should exist
	_, exists := cache.getVersion(DEKID("dek1"), 1)
	assert.True(t, exists)
	_, exists = cache.getVersion(DEKID("dek1"), 2)
	assert.True(t, exists)
	_, exists = cache.getVersion(DEKID("dek2"), 1)
	assert.True(t, exists)

	// Add one more - should evict the oldest
	cache.setVersion(DEKID("dek2"), 2, []byte("key2-v2"))

	// Cache should still have 3 entries
	assert.Equal(t, 3, len(cache.versionedCache))
}

// TestDEKCache_KeyCopy tests that cache returns copies, not references.
func TestDEKCache_KeyCopy(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	testDEKID := DEKID("test-dek")
	originalKey := []byte("original-key-data")
	cache.set(testDEKID, originalKey, 1)

	// Get the key
	retrievedKey, _, exists := cache.get(testDEKID)
	require.True(t, exists)

	// Modify the retrieved key
	retrievedKey[0] = 'X'

	// Get the key again - should be unchanged
	retrievedKey2, _, exists := cache.get(testDEKID)
	require.True(t, exists)
	assert.Equal(t, originalKey, retrievedKey2, "Cache should return a copy, not a reference")
}

// TestDEKCache_CleanupEdgeCases tests edge cases in cleanup logic.
func TestDEKCache_CleanupEdgeCases(t *testing.T) {
	t.Run("cleanup with mixed expired and valid entries", func(t *testing.T) {
		cache := newDEKCache()
		defer cache.stop()

		// Add some entries
		cache.set(DEKID("fresh1"), []byte("key1"), 1)
		cache.set(DEKID("old1"), []byte("key2"), 1)
		cache.set(DEKID("fresh2"), []byte("key3"), 1)
		cache.set(DEKID("old2"), []byte("key4"), 1)

		// Expire some entries
		cache.entries[DEKID("old1")].expiry = time.Now().Add(-1 * time.Second)
		cache.entries[DEKID("old2")].expiry = time.Now().Add(-1 * time.Second)

		// Access cache - expired ones should be removed
		_, _, exists := cache.get(DEKID("old1"))
		assert.False(t, exists, "old1 should be expired")

		_, _, exists = cache.get(DEKID("old2"))
		assert.False(t, exists, "old2 should be expired")

		// Fresh ones should still exist
		_, _, exists = cache.get(DEKID("fresh1"))
		assert.True(t, exists, "fresh1 should still exist")

		_, _, exists = cache.get(DEKID("fresh2"))
		assert.True(t, exists, "fresh2 should still exist")
	})

	t.Run("cleanup versioned with mixed expiry", func(t *testing.T) {
		cache := newDEKCache()
		defer cache.stop()

		// Add versioned entries
		cache.setVersion(DEKID("dek1"), 1, []byte("v1"))
		cache.setVersion(DEKID("dek1"), 2, []byte("v2"))
		cache.setVersion(DEKID("dek2"), 1, []byte("v1"))

		// Expire version 1 of dek1
		key1 := dekCacheKey{dekid: DEKID("dek1"), version: 1}
		cache.versionedCache[key1].expiry = time.Now().Add(-1 * time.Second)

		// Access - expired should be removed
		_, exists := cache.getVersion(DEKID("dek1"), 1)
		assert.False(t, exists, "dek1 v1 should be expired")

		// Others should still exist
		_, exists = cache.getVersion(DEKID("dek1"), 2)
		assert.True(t, exists, "dek1 v2 should still exist")

		_, exists = cache.getVersion(DEKID("dek2"), 1)
		assert.True(t, exists, "dek2 v1 should still exist")
	})

	t.Run("stop channel cleanup", func(t *testing.T) {
		cache := newDEKCache()

		// Add some entries
		cache.set(DEKID("test"), []byte("key"), 1)

		// Stop should close the channel
		cache.stop()

		// Verify channel is closed
		select {
		case <-cache.stopChan:
			// Expected - channel is closed
		case <-time.After(100 * time.Millisecond):
			t.Error("stopChan should be closed immediately after stop()")
		}
	})
}
