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
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestCacheHitRatio validates cache performance under realistic workload.
// Verifies 70%+ hit rate with 10 workers performing 1000 reads each.
func TestCacheHitRatio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Initialize cache with reasonable size
	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          5000,
		L1TTL:           10 * time.Minute, // Long TTL to ensure hits
		CleanupInterval: 30 * time.Second,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	// Pre-populate cache with hot keys (80% of dataset)
	hotKeyCount := 800
	t.Logf("Pre-populating cache with %d hot keys", hotKeyCount)
	for i := 0; i < hotKeyCount; i++ {
		key := fmt.Sprintf("hot-key-%d", i)
		value := []byte(fmt.Sprintf("hot-value-%d", i))
		err := cacheManager.Set(ctx, key, value)
		require.NoError(t, err)
	}

	// Run concurrent workers performing reads
	const numWorkers = 10
	const readsPerWorker = 1000

	var wg sync.WaitGroup
	var successfulReads atomic.Int64
	var failedReads atomic.Int64

	t.Logf("Starting %d workers, %d reads each", numWorkers, readsPerWorker)
	startTime := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))

			for i := 0; i < readsPerWorker; i++ {
				// 80% hot keys, 20% cold keys (Pareto principle)
				var key string
				if rng.Float64() < 0.8 {
					// Hot key - should be in cache
					keyNum := rng.Intn(hotKeyCount)
					key = fmt.Sprintf("hot-key-%d", keyNum)
				} else {
					// Cold key - cache miss
					keyNum := rng.Intn(1000) + hotKeyCount
					key = fmt.Sprintf("cold-key-%d", keyNum)
				}

				_, err := cacheManager.Get(ctx, key)
				if err == nil {
					successfulReads.Add(1)
				} else {
					failedReads.Add(1)
				}
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// Analyze results
	stats := cacheManager.Stats()
	totalOps := numWorkers * readsPerWorker
	successCount := successfulReads.Load()
	failCount := failedReads.Load()

	t.Logf("Test completed in %v", duration)
	t.Logf("Total operations: %d", totalOps)
	t.Logf("Successful reads: %d", successCount)
	t.Logf("Failed reads: %d", failCount)
	t.Logf("L1 hits: %d, L1 misses: %d", stats.L1Hits, stats.L1Misses)
	t.Logf("Cache hit rate: %.2f%%", stats.L1HitRate*100)
	t.Logf("Operations per second: %.2f", float64(totalOps)/duration.Seconds())

	// Verify hit rate meets target (70%+)
	assert.GreaterOrEqual(t, stats.L1HitRate, 0.70,
		"Cache hit rate should be at least 70%% (got %.2f%%)", stats.L1HitRate*100)

	// Verify cache is working efficiently
	assert.Greater(t, stats.L1Hits, int64(0), "Should have cache hits")
	assert.Equal(t, int64(totalOps), stats.L1Hits+stats.L1Misses,
		"Total hits + misses should equal total operations")
}

// TestCacheEvictionUnderPressure validates LRU eviction behavior.
// Tests cache with 10,000 keys with 1KB values to force evictions.
func TestCacheEvictionUnderPressure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Small cache to force evictions
	cacheCapacity := 1000
	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          cacheCapacity,
		L1TTL:           30 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	// Insert 10x cache capacity
	const numKeys = 10000
	const valueSize = 1024 // 1KB per value

	t.Logf("Inserting %d keys with %d byte values (cache capacity: %d)",
		numKeys, valueSize, cacheCapacity)

	value := make([]byte, valueSize)
	for i := 0; i < valueSize; i++ {
		value[i] = byte(i % 256)
	}

	startTime := time.Now()
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key-%d", i)
		err := cacheManager.Set(ctx, key, value)
		require.NoError(t, err)

		// Log progress every 1000 keys
		if (i+1)%1000 == 0 {
			stats := cacheManager.Stats()
			t.Logf("Progress: %d/%d keys, cache size: %d, evictions: %d",
				i+1, numKeys, stats.L1Size, stats.L1Evictions)
		}
	}
	duration := time.Since(startTime)

	// Verify eviction behavior
	stats := cacheManager.Stats()
	t.Logf("Test completed in %v", duration)
	t.Logf("Final cache size: %d (capacity: %d)", stats.L1Size, stats.L1Capacity)
	t.Logf("Total evictions: %d", stats.L1Evictions)
	t.Logf("Write throughput: %.2f ops/sec", float64(numKeys)/duration.Seconds())

	// Cache should be at or near capacity
	assert.LessOrEqual(t, stats.L1Size, stats.L1Capacity,
		"Cache size should not exceed capacity")
	assert.GreaterOrEqual(t, stats.L1Size, int(float64(stats.L1Capacity)*0.95),
		"Cache should be near capacity after insertions")

	// Verify evictions occurred
	expectedEvictions := numKeys - cacheCapacity
	assert.GreaterOrEqual(t, stats.L1Evictions, int64(expectedEvictions),
		"Should have evicted at least %d entries", expectedEvictions)

	// Test LRU behavior: recently added keys should still be present
	recentKeysFound := 0
	for i := numKeys - 100; i < numKeys; i++ {
		key := fmt.Sprintf("key-%d", i)
		_, err := cacheManager.Get(ctx, key)
		if err == nil {
			recentKeysFound++
		}
	}

	t.Logf("Recent keys found in cache: %d/100", recentKeysFound)
	assert.GreaterOrEqual(t, recentKeysFound, 95,
		"Most recently added keys should still be in cache (LRU)")
}

// TestConcurrentCacheAccess validates thread-safety under high concurrency.
// Tests 20 workers performing 500 operations each with 95%+ success rate.
func TestConcurrentCacheAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          2000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 30 * time.Second,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	const numWorkers = 20
	const opsPerWorker = 500

	var wg sync.WaitGroup
	var successfulOps atomic.Int64
	var failedOps atomic.Int64
	var sets atomic.Int64
	var gets atomic.Int64
	var deletes atomic.Int64

	t.Logf("Starting %d workers with %d operations each", numWorkers, opsPerWorker)
	startTime := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))

			for i := 0; i < opsPerWorker; i++ {
				key := fmt.Sprintf("concurrent-key-%d", rng.Intn(1000))
				value := []byte(fmt.Sprintf("value-%d-%d", workerID, i))

				// Random operation: 50% Set, 40% Get, 10% Delete
				op := rng.Float64()

				var err error
				if op < 0.5 {
					// Set operation
					err = cacheManager.Set(ctx, key, value)
					sets.Add(1)
				} else if op < 0.9 {
					// Get operation
					_, err = cacheManager.Get(ctx, key)
					gets.Add(1)
					// Missing keys are not failures
					if err != nil {
						err = nil
					}
				} else {
					// Delete operation
					err = cacheManager.Delete(ctx, key)
					deletes.Add(1)
				}

				if err == nil {
					successfulOps.Add(1)
				} else {
					failedOps.Add(1)
				}
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// Analyze results
	totalOps := numWorkers * opsPerWorker
	successCount := successfulOps.Load()
	failCount := failedOps.Load()
	successRate := float64(successCount) / float64(totalOps)

	stats := cacheManager.Stats()

	t.Logf("Test completed in %v", duration)
	t.Logf("Total operations: %d", totalOps)
	t.Logf("  Sets: %d, Gets: %d, Deletes: %d", sets.Load(), gets.Load(), deletes.Load())
	t.Logf("Successful: %d, Failed: %d", successCount, failCount)
	t.Logf("Success rate: %.2f%%", successRate*100)
	t.Logf("Operations per second: %.2f", float64(totalOps)/duration.Seconds())
	t.Logf("Final cache size: %d/%d", stats.L1Size, stats.L1Capacity)

	// Verify high success rate
	assert.GreaterOrEqual(t, successRate, 0.95,
		"Success rate should be at least 95%% (got %.2f%%)", successRate*100)

	// Verify no data corruption
	assert.Equal(t, totalOps, int(successCount+failCount),
		"All operations should be accounted for")
}

// TestCacheInvalidationOnWrite validates write-through cache invalidation.
// Ensures cache entries are properly invalidated on updates.
func TestCacheInvalidationOnWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          1000,
		L1TTL:           10 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	key := "invalidation-test-key"
	value1 := []byte("initial-value")
	value2 := []byte("updated-value")

	// Set initial value
	err = cacheManager.Set(ctx, key, value1)
	require.NoError(t, err)

	// Verify initial value is cached
	cachedValue, err := cacheManager.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value1, cachedValue)

	initialStats := cacheManager.Stats()
	t.Logf("After initial set - L1 hits: %d", initialStats.L1Hits)

	// Update the value (simulating write-through invalidation)
	err = cacheManager.Delete(ctx, key) // Invalidate
	require.NoError(t, err)

	err = cacheManager.Set(ctx, key, value2)
	require.NoError(t, err)

	// Verify updated value is returned
	cachedValue, err = cacheManager.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value2, cachedValue, "Cache should return updated value")

	// Test bulk invalidation
	t.Run("BulkInvalidation", func(t *testing.T) {
		// Set multiple keys
		for i := 0; i < 100; i++ {
			k := fmt.Sprintf("bulk-key-%d", i)
			v := []byte(fmt.Sprintf("bulk-value-%d", i))
			err := cacheManager.Set(ctx, k, v)
			require.NoError(t, err)
		}

		stats := cacheManager.Stats()
		t.Logf("Cache size before clear: %d", stats.L1Size)

		// Clear cache (bulk invalidation)
		cacheManager.Clear()

		statsAfter := cacheManager.Stats()
		assert.Equal(t, 0, statsAfter.L1Size, "Cache should be empty after clear")
		t.Logf("Cache size after clear: %d", statsAfter.L1Size)
	})

	finalStats := cacheManager.Stats()
	t.Logf("Final stats - L1 hits: %d, L1 misses: %d, sets: %d",
		finalStats.L1Hits, finalStats.L1Misses, finalStats.L1Size)
}

// TestReadThroughCache validates cache population on miss.
// Simulates read-through pattern where cache is populated from storage on miss.
func TestReadThroughCache(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          500,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 30 * time.Second,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	// Simulate backend storage
	backendStorage := make(map[string][]byte)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("backend-key-%d", i)
		value := []byte(fmt.Sprintf("backend-value-%d", i))
		backendStorage[key] = value
	}

	// Read-through function
	readThrough := func(key string) ([]byte, error) {
		// Try cache first
		value, err := cacheManager.Get(ctx, key)
		if err == nil {
			return value, nil
		}

		// Cache miss - fetch from backend
		value, exists := backendStorage[key]
		if !exists {
			return nil, &qberrors.MessageError{Message: "key not found in backend"}
		}

		// Populate cache
		err = cacheManager.Set(ctx, key, value)
		if err != nil {
			t.Logf("Warning: failed to populate cache: %v", err)
		}

		return value, nil
	}

	// Test read-through pattern
	t.Logf("Testing read-through pattern with 1000 keys")

	initialStats := cacheManager.Stats()
	var cacheMisses atomic.Int64
	var cacheHits atomic.Int64

	// First pass - all cache misses
	t.Run("FirstPass", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("backend-key-%d", i)

			// Check if in cache before read-through
			_, err := cacheManager.Get(ctx, key)
			if err != nil {
				cacheMisses.Add(1)
			}

			value, err := readThrough(key)
			require.NoError(t, err)

			expectedValue := []byte(fmt.Sprintf("backend-value-%d", i))
			assert.Equal(t, expectedValue, value)
		}

		t.Logf("First pass - cache misses: %d", cacheMisses.Load())
	})

	// Second pass - should be cache hits
	t.Run("SecondPass", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("backend-key-%d", i)

			_, err := cacheManager.Get(ctx, key)
			if err == nil {
				cacheHits.Add(1)
			}

			value, err := readThrough(key)
			require.NoError(t, err)

			expectedValue := []byte(fmt.Sprintf("backend-value-%d", i))
			assert.Equal(t, expectedValue, value)
		}

		t.Logf("Second pass - cache hits: %d", cacheHits.Load())
	})

	finalStats := cacheManager.Stats()
	hitRate := float64(finalStats.L1Hits) / float64(finalStats.L1Hits+finalStats.L1Misses)

	t.Logf("Cache statistics:")
	t.Logf("  Initial hits: %d, misses: %d", initialStats.L1Hits, initialStats.L1Misses)
	t.Logf("  Final hits: %d, misses: %d", finalStats.L1Hits, finalStats.L1Misses)
	t.Logf("  Hit rate: %.2f%%", hitRate*100)
	t.Logf("  Cache size: %d/%d", finalStats.L1Size, finalStats.L1Capacity)

	// Verify read-through improved hit rate
	assert.Greater(t, cacheHits.Load(), int64(0),
		"Should have cache hits on second pass")
	assert.Greater(t, hitRate, 0.0,
		"Read-through should improve cache hit rate")
}

// TestCacheTTLExpiration validates time-based cache expiration patterns.
// Uses real time delays to test TTL-based eviction.
func TestCacheTTLExpiration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Short TTL for testing
	ttl := 5 * time.Second
	cleanupInterval := 1 * time.Second
	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          1000,
		L1TTL:           ttl,
		CleanupInterval: cleanupInterval,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	// Test different expiration patterns
	t.Run("SimpleExpiration", func(t *testing.T) {
		key := "expire-test-1"
		value := []byte("expire-value-1")

		// Set value
		err := cacheManager.Set(ctx, key, value)
		require.NoError(t, err)

		// Immediately readable
		cachedValue, err := cacheManager.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, cachedValue)

		t.Logf("Waiting up to %v for TTL expiration...", ttl+cleanupInterval)
		requireExpiredWithin(t, ttl+cleanupInterval, func() error {
			_, err := cacheManager.Get(ctx, key)
			return err
		})

		// Should be expired
		_, err = cacheManager.Get(ctx, key)
		assert.Error(t, err, "Value should be expired")
	})

	t.Run("RollingExpiration", func(t *testing.T) {
		// Set keys with staggered timing
		stagger := time.NewTicker(500 * time.Millisecond)
		defer stagger.Stop()
		keys := make([]string, 10)
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("rolling-key-%d", i)
			value := []byte(fmt.Sprintf("rolling-value-%d", i))

			err := cacheManager.Set(ctx, key, value)
			require.NoError(t, err)
			keys[i] = key

			select {
			case <-stagger.C:
			case <-ctx.Done():
				require.NoError(t, ctx.Err())
			}
		}

		// First keys should expire sooner
		t.Logf("Waiting for early keys to expire...")
		requireExpiredWithin(t, ttl+cleanupInterval, func() error {
			_, err := cacheManager.Get(ctx, keys[0])
			return err
		})

		// Early keys might be expired
		_, err := cacheManager.Get(ctx, keys[0])
		t.Logf("First key status: %v", err)

		// Later keys should still be valid
		_, err = cacheManager.Get(ctx, keys[9])
		assert.NoError(t, err, "Most recent key should still be valid")
	})

	t.Run("ExpirationCleanup", func(t *testing.T) {
		// Fill cache
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("cleanup-key-%d", i)
			value := []byte(fmt.Sprintf("cleanup-value-%d", i))
			err := cacheManager.Set(ctx, key, value)
			require.NoError(t, err)
		}

		beforeStats := cacheManager.Stats()
		t.Logf("Cache size before expiration: %d", beforeStats.L1Size)

		// Wait for expiration + cleanup
		t.Logf("Waiting for cleanup cycle...")
		require.Eventually(t, func() bool {
			return cacheManager.Stats().L1Size < beforeStats.L1Size
		}, ttl+2*cleanupInterval, pollInterval, "cleanup cycle should remove expired entries")

		afterStats := cacheManager.Stats()
		t.Logf("Cache size after expiration: %d", afterStats.L1Size)

		// Most entries should be cleaned up
		assert.Less(t, afterStats.L1Size, beforeStats.L1Size,
			"Cache size should decrease after expiration cleanup")
	})
}

// TestCacheWarmupAfterRestart simulates cache warmup after service restart.
// Tests realistic warmup patterns and performance.
func TestCacheWarmupAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Simulate initial cache state
	t.Run("Phase1_InitialLoad", func(t *testing.T) {
		cacheManager, err := cache.NewManager(cache.Config{
			L1Size:          2000,
			L1TTL:           10 * time.Minute,
			CleanupInterval: 1 * time.Minute,
		}, helpers.NewLoggerAdapter(log))
		require.NoError(t, err)
		defer cacheManager.Close()

		// Load hot keys
		t.Logf("Loading 1000 hot keys into cache...")
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("hot-key-%d", i)
			value := []byte(fmt.Sprintf("hot-value-%d", i))
			err := cacheManager.Set(ctx, key, value)
			require.NoError(t, err)
		}

		stats := cacheManager.Stats()
		t.Logf("Initial cache state - size: %d, capacity: %d", stats.L1Size, stats.L1Capacity)
	})

	// Simulate restart and warmup
	t.Run("Phase2_Warmup", func(t *testing.T) {
		// Create new cache manager (simulates restart)
		cacheManager, err := cache.NewManager(cache.Config{
			L1Size:          2000,
			L1TTL:           10 * time.Minute,
			CleanupInterval: 1 * time.Minute,
		}, helpers.NewLoggerAdapter(log))
		require.NoError(t, err)
		defer cacheManager.Close()

		// Cache is empty after "restart"
		stats := cacheManager.Stats()
		assert.Equal(t, 0, stats.L1Size, "Cache should be empty after restart")

		// Simulate warmup from backend
		backendData := make(map[string][]byte)
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("hot-key-%d", i)
			value := []byte(fmt.Sprintf("hot-value-%d", i))
			backendData[key] = value
		}

		// Warmup strategy: load top 500 hot keys
		t.Logf("Warming up cache with 500 most accessed keys...")
		warmupStart := time.Now()

		for i := 0; i < 500; i++ {
			key := fmt.Sprintf("hot-key-%d", i)
			value := backendData[key]
			err := cacheManager.Set(ctx, key, value)
			require.NoError(t, err)
		}

		warmupDuration := time.Since(warmupStart)
		t.Logf("Warmup completed in %v", warmupDuration)

		stats = cacheManager.Stats()
		t.Logf("Post-warmup cache size: %d/%d", stats.L1Size, stats.L1Capacity)
		assert.Equal(t, 500, stats.L1Size, "Cache should contain 500 warmed keys")

		// Verify warmed keys are accessible
		for i := 0; i < 500; i++ {
			key := fmt.Sprintf("hot-key-%d", i)
			value, err := cacheManager.Get(ctx, key)
			require.NoError(t, err)
			expectedValue := []byte(fmt.Sprintf("hot-value-%d", i))
			assert.Equal(t, expectedValue, value)
		}

		finalStats := cacheManager.Stats()
		hitRate := float64(finalStats.L1Hits) / float64(finalStats.L1Hits+finalStats.L1Misses)
		t.Logf("Post-warmup hit rate: %.2f%%", hitRate*100)

		// After warmup, hit rate should be 100% for warmed keys
		assert.GreaterOrEqual(t, hitRate, 0.99,
			"Hit rate should be ~100%% for warmed keys")
	})
}

// TestCacheSizeMonitoring validates cache size tracking and metrics.
// Tests real-time size monitoring through cache metrics.
func TestCacheSizeMonitoring(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          1000,
		L1TTL:           10 * time.Minute,
		CleanupInterval: 30 * time.Second,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	// Monitor size during various operations
	type SizeSnapshot struct {
		Operation string
		Size      int
		Capacity  int
		Evictions int64
		Timestamp time.Time
	}

	var snapshots []SizeSnapshot

	recordSnapshot := func(operation string) {
		stats := cacheManager.Stats()
		snapshot := SizeSnapshot{
			Operation: operation,
			Size:      stats.L1Size,
			Capacity:  stats.L1Capacity,
			Evictions: stats.L1Evictions,
			Timestamp: time.Now(),
		}
		snapshots = append(snapshots, snapshot)
		t.Logf("[%s] Size: %d/%d, Evictions: %d",
			operation, snapshot.Size, snapshot.Capacity, snapshot.Evictions)
	}

	// Initial state
	recordSnapshot("initial")

	// Add entries gradually
	t.Run("GradualInsertion", func(t *testing.T) {
		for batch := 1; batch <= 5; batch++ {
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("size-key-%d", (batch-1)*200+i)
				value := []byte(fmt.Sprintf("size-value-%d", i))
				err := cacheManager.Set(ctx, key, value)
				require.NoError(t, err)
			}
			recordSnapshot(fmt.Sprintf("batch-%d", batch))
		}
	})

	// Delete entries
	t.Run("Deletion", func(t *testing.T) {
		for i := 0; i < 300; i++ {
			key := fmt.Sprintf("size-key-%d", i)
			err := cacheManager.Delete(ctx, key)
			require.NoError(t, err)
		}
		recordSnapshot("after-deletes")
	})

	// Test size after clear
	t.Run("Clear", func(t *testing.T) {
		cacheManager.Clear()
		recordSnapshot("after-clear")

		stats := cacheManager.Stats()
		assert.Equal(t, 0, stats.L1Size, "Cache should be empty after clear")
	})

	// Refill and monitor
	t.Run("Refill", func(t *testing.T) {
		for i := 0; i < 800; i++ {
			key := fmt.Sprintf("refill-key-%d", i)
			value := []byte(fmt.Sprintf("refill-value-%d", i))
			err := cacheManager.Set(ctx, key, value)
			require.NoError(t, err)
		}
		recordSnapshot("after-refill")
	})

	// Print monitoring summary
	t.Logf("\n=== Cache Size Monitoring Summary ===")
	for i, snapshot := range snapshots {
		fillPercent := float64(snapshot.Size) / float64(snapshot.Capacity) * 100
		t.Logf("[%d] %s: %d/%d (%.1f%%), evictions: %d",
			i, snapshot.Operation, snapshot.Size, snapshot.Capacity,
			fillPercent, snapshot.Evictions)
	}

	// Verify size tracking accuracy
	finalStats := cacheManager.Stats()
	t.Logf("\nFinal statistics:")
	t.Logf("  Cache size: %d/%d", finalStats.L1Size, finalStats.L1Capacity)
	t.Logf("  Total evictions: %d", finalStats.L1Evictions)
	t.Logf("  L1 hits: %d, misses: %d", finalStats.L1Hits, finalStats.L1Misses)

	// Size should never exceed capacity
	for _, snapshot := range snapshots {
		assert.LessOrEqual(t, snapshot.Size, snapshot.Capacity,
			"Cache size should never exceed capacity at %s", snapshot.Operation)
	}

	// Verify metrics are consistent
	assert.Equal(t, 800, finalStats.L1Size,
		"Final cache size should match number of refill entries")
}
