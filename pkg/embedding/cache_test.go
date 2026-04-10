//go:build vector

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

package embedding

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCache(t *testing.T) {
	t.Run("valid cache", func(t *testing.T) {
		cache, err := NewCache(100, 3600, true)
		require.NoError(t, err)
		assert.NotNil(t, cache)
		assert.Equal(t, 0, cache.Size())
	})

	t.Run("invalid capacity", func(t *testing.T) {
		_, err := NewCache(0, 3600, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "capacity must be positive")
	})

	t.Run("negative capacity", func(t *testing.T) {
		_, err := NewCache(-1, 3600, true)
		assert.Error(t, err)
	})
}

func TestCachePutAndGet(t *testing.T) {
	cache, err := NewCache(10, 3600, false)
	require.NoError(t, err)

	embedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}

	t.Run("put and get", func(t *testing.T) {
		cache.Put("key1", embedding)

		result, found := cache.Get("key1")
		assert.True(t, found)
		assert.Equal(t, embedding, result)
		assert.Equal(t, 1, cache.Size())
		assert.Equal(t, uint64(1), cache.Hits())
	})

	t.Run("get non-existent key", func(t *testing.T) {
		result, found := cache.Get("nonexistent")
		assert.False(t, found)
		assert.Nil(t, result)
		assert.Equal(t, uint64(1), cache.Misses())
	})

	t.Run("update existing key", func(t *testing.T) {
		newEmbedding := []float32{0.9, 0.8, 0.7}
		cache.Put("key1", newEmbedding)

		result, found := cache.Get("key1")
		assert.True(t, found)
		assert.Equal(t, newEmbedding, result)
		assert.Equal(t, 1, cache.Size()) // Size shouldn't increase
	})
}

func TestCacheEviction(t *testing.T) {
	cache, err := NewCache(3, 3600, false)
	require.NoError(t, err)

	// Add 4 items to a cache with capacity 3
	cache.Put("key1", []float32{0.1})
	cache.Put("key2", []float32{0.2})
	cache.Put("key3", []float32{0.3})
	cache.Put("key4", []float32{0.4}) // Should evict key1

	assert.Equal(t, 3, cache.Size())
	assert.Equal(t, uint64(1), cache.Evictions())

	// key1 should be evicted
	_, found := cache.Get("key1")
	assert.False(t, found)

	// Other keys should still exist
	_, found = cache.Get("key2")
	assert.True(t, found)
	_, found = cache.Get("key3")
	assert.True(t, found)
	_, found = cache.Get("key4")
	assert.True(t, found)
}

func TestCacheLRUOrder(t *testing.T) {
	cache, err := NewCache(3, 3600, false)
	require.NoError(t, err)

	cache.Put("key1", []float32{0.1})
	cache.Put("key2", []float32{0.2})
	cache.Put("key3", []float32{0.3})

	// Access key1 to make it recently used
	cache.Get("key1")

	// Add key4, should evict key2 (least recently used)
	cache.Put("key4", []float32{0.4})

	_, found := cache.Get("key2")
	assert.False(t, found, "key2 should be evicted")

	_, found = cache.Get("key1")
	assert.True(t, found, "key1 should still exist (was recently accessed)")
}

func TestCacheTTL(t *testing.T) {
	cache, err := NewCache(10, 1, false) // 1 second TTL
	require.NoError(t, err)

	cache.Put("key1", []float32{0.1, 0.2})

	// Immediately should be found
	_, found := cache.Get("key1")
	assert.True(t, found)

	// Wait for TTL to expire
	time.Sleep(1100 * time.Millisecond)

	// Should be expired
	_, found = cache.Get("key1")
	assert.False(t, found)
	assert.Equal(t, 0, cache.Size()) // Should be removed
}

func TestCacheCompression(t *testing.T) {
	t.Run("with compression", func(t *testing.T) {
		cache, err := NewCache(10, 3600, true)
		require.NoError(t, err)

		// Large embedding that should benefit from compression
		embedding := make([]float32, 1000)
		for i := range embedding {
			embedding[i] = 0.1
		}

		cache.Put("key1", embedding)

		result, found := cache.Get("key1")
		assert.True(t, found)
		assert.Equal(t, len(embedding), len(result))
		assert.Equal(t, embedding, result)
	})

	t.Run("without compression", func(t *testing.T) {
		cache, err := NewCache(10, 3600, false)
		require.NoError(t, err)

		embedding := []float32{0.1, 0.2, 0.3}
		cache.Put("key1", embedding)

		result, found := cache.Get("key1")
		assert.True(t, found)
		assert.Equal(t, embedding, result)
	})
}

func TestCacheClear(t *testing.T) {
	cache, err := NewCache(10, 3600, false)
	require.NoError(t, err)

	cache.Put("key1", []float32{0.1})
	cache.Put("key2", []float32{0.2})
	cache.Put("key3", []float32{0.3})

	assert.Equal(t, 3, cache.Size())

	cache.Clear()

	assert.Equal(t, 0, cache.Size())
	_, found := cache.Get("key1")
	assert.False(t, found)
}

func TestCacheStatistics(t *testing.T) {
	cache, err := NewCache(10, 3600, false)
	require.NoError(t, err)

	// Initial stats
	assert.Equal(t, uint64(0), cache.Hits())
	assert.Equal(t, uint64(0), cache.Misses())
	assert.Equal(t, uint64(0), cache.Evictions())

	// Add items
	cache.Put("key1", []float32{0.1})
	cache.Put("key2", []float32{0.2})

	// Hit
	cache.Get("key1")
	assert.Equal(t, uint64(1), cache.Hits())

	// Miss
	cache.Get("key3")
	assert.Equal(t, uint64(1), cache.Misses())

	// Fill cache to cause eviction (capacity is 10, already have 2 items)
	for i := 0; i < 10; i++ {
		cache.Put(fmt.Sprintf("evict-%d", i), []float32{float32(i)})
	}
	// Should have evicted at least 2 items (2 existing + 10 new = 12, capacity is 10)
	assert.Equal(t, uint64(2), cache.Evictions())
}

func TestCacheKeyHashing(t *testing.T) {
	cache, err := NewCache(10, 3600, false)
	require.NoError(t, err)

	// Same content, different keys should be treated as different
	cache.Put("hello world", []float32{0.1})
	cache.Put("hello world", []float32{0.2}) // Same key, should update

	result, found := cache.Get("hello world")
	assert.True(t, found)
	assert.Equal(t, []float32{0.2}, result)
	assert.Equal(t, 1, cache.Size())
}

func TestCacheConcurrentAccess(t *testing.T) {
	cache, err := NewCache(100, 3600, false)
	require.NoError(t, err)

	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			cache.Put("key", []float32{float32(i)})
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			cache.Get("key")
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Should not panic and cache should be in valid state
	assert.Equal(t, 1, cache.Size())
}

func TestCacheEncodeDecodeErrors(t *testing.T) {
	cache, err := NewCache(10, 3600, true)
	require.NoError(t, err)

	// Test with valid data
	embedding := []float32{0.1, 0.2, 0.3}
	cache.Put("key1", embedding)

	result, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, embedding, result)
}

func TestCacheZeroTTL(t *testing.T) {
	cache, err := NewCache(10, 0, false) // Zero TTL = no expiration
	require.NoError(t, err)

	cache.Put("key1", []float32{0.1})

	time.Sleep(100 * time.Millisecond)

	// Should still be found (no TTL)
	_, found := cache.Get("key1")
	assert.True(t, found)
}
