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

//go:build integration

package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestCacheHitMiss tests cache hit and miss behavior
func TestCacheHitMiss(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Initialize cache
	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          10, // Small cache to test eviction
		L1TTL:           5 * time.Second,
		CleanupInterval: 1 * time.Second,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	// Test cache hits
	t.Run("CacheHits", func(t *testing.T) {
		key := "test-key-1"
		value := []byte("test-value-1")

		// Set value
		err := cacheManager.Set(ctx, key, value)
		require.NoError(t, err)

		// Get value (should hit cache)
		cachedValue, err := cacheManager.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, cachedValue)

		// Check stats
		stats := cacheManager.Stats()
		assert.Greater(t, stats.L1Hits, int64(0))
		assert.Greater(t, stats.L1HitRate, 0.0)

		t.Logf("Cache hit rate: %.2f%%, L1 size: %d/%d",
			stats.L1HitRate*100, stats.L1Size, stats.L1Capacity)
	})

	// Test cache misses
	t.Run("CacheMisses", func(t *testing.T) {
		// Request non-existent key
		_, err := cacheManager.Get(ctx, "non-existent-key")
		assert.Error(t, err)

		// Check stats
		stats := cacheManager.Stats()
		assert.Greater(t, stats.L1Misses, int64(0))

		t.Logf("Cache misses: %d", stats.L1Misses)
	})

	// Test cache eviction
	t.Run("CacheEviction", func(t *testing.T) {
		// Fill cache beyond capacity
		for i := 0; i < 20; i++ {
			key := fmt.Sprintf("eviction-key-%d", i)
			value := []byte(fmt.Sprintf("eviction-value-%d", i))

			err := cacheManager.Set(ctx, key, value)
			require.NoError(t, err)
		}

		// Check that cache size is limited
		stats := cacheManager.Stats()
		assert.LessOrEqual(t, stats.L1Size, stats.L1Capacity)

		t.Logf("Cache size after eviction: %d (capacity: %d)",
			stats.L1Size, stats.L1Capacity)
	})

	// Test TTL expiration
	t.Run("CacheTTLExpiration", func(t *testing.T) {
		key := "ttl-test-key"
		value := []byte("ttl-test-value")

		// Set value
		err := cacheManager.Set(ctx, key, value)
		require.NoError(t, err)

		// Verify it exists
		_, err = cacheManager.Get(ctx, key)
		require.NoError(t, err)

		// Wait for TTL to expire
		time.Sleep(6 * time.Second)

		// Should be expired now
		_, err = cacheManager.Get(ctx, key)
		assert.Error(t, err, "Value should be expired")

		t.Logf("TTL expiration working correctly")
	})
}
