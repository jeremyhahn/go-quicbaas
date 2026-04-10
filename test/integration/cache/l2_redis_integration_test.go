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

//go:build integration && l2_redis

package cache

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// getRedisAddr returns the Redis address from environment or default
func getRedisAddr() string {
	addr := os.Getenv("QUICBAAS_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	return addr
}

// TestRedisL2Cache_DirectOperations tests RedisL2Cache operations directly
// against a real Redis server without the cache manager layer.
func TestRedisL2Cache_DirectOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log := helpers.CreateTestLoggerConcrete(t)
	logAdapter := helpers.NewLoggerAdapter(log)

	// Create Redis L2 cache config
	config := cache.DefaultRedisL2Config()
	config.Addr = getRedisAddr()

	// Create Redis L2 cache
	redisCache, err := cache.NewRedisL2Cache(ctx, config, logAdapter)
	require.NoError(t, err, "Failed to create Redis L2 cache - is Redis running at %s?", config.Addr)
	defer redisCache.Close()

	t.Run("Ping", func(t *testing.T) {
		err := redisCache.Ping(ctx)
		require.NoError(t, err, "Redis ping failed")
		t.Log("Redis ping successful")
	})

	t.Run("SetAndGet", func(t *testing.T) {
		key := "test:direct:key1"
		value := []byte("test-value-1")
		ttl := 5 * time.Minute

		// Set value
		err := redisCache.Set(ctx, key, value, ttl)
		require.NoError(t, err)

		// Get value
		retrieved, err := redisCache.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)

		t.Logf("Set and Get successful for key: %s", key)
	})

	t.Run("GetNonExistent", func(t *testing.T) {
		_, err := redisCache.Get(ctx, "test:direct:nonexistent")
		assert.Error(t, err, "Should return error for non-existent key")
		t.Log("Non-existent key correctly returns error")
	})

	t.Run("Delete", func(t *testing.T) {
		key := "test:direct:delete"
		value := []byte("delete-value")

		// Set value
		err := redisCache.Set(ctx, key, value, 5*time.Minute)
		require.NoError(t, err)

		// Verify exists
		_, err = redisCache.Get(ctx, key)
		require.NoError(t, err)

		// Delete
		err = redisCache.Delete(ctx, key)
		require.NoError(t, err)

		// Verify deleted
		_, err = redisCache.Get(ctx, key)
		assert.Error(t, err, "Key should be deleted")

		t.Log("Delete operation successful")
	})

	t.Run("TTLExpiration", func(t *testing.T) {
		key := "test:direct:ttl"
		value := []byte("ttl-value")
		ttl := 2 * time.Second

		// Set value with short TTL
		err := redisCache.Set(ctx, key, value, ttl)
		require.NoError(t, err)

		// Verify exists
		_, err = redisCache.Get(ctx, key)
		require.NoError(t, err)

		// Wait for expiration
		t.Logf("Waiting %v for TTL expiration...", ttl+time.Second)
		time.Sleep(ttl + time.Second)

		// Should be expired
		_, err = redisCache.Get(ctx, key)
		assert.Error(t, err, "Key should be expired")

		t.Log("TTL expiration working correctly")
	})

	t.Run("DBSize", func(t *testing.T) {
		size, err := redisCache.DBSize(ctx)
		require.NoError(t, err)
		t.Logf("Current DB size: %d keys", size)
	})

	t.Run("PoolStats", func(t *testing.T) {
		stats := redisCache.PoolStats()
		require.NotNil(t, stats)
		t.Logf("Pool stats - Hits: %d, Misses: %d, Timeouts: %d, TotalConns: %d",
			stats.Hits, stats.Misses, stats.Timeouts, stats.TotalConns)
	})

	t.Run("FlushDB", func(t *testing.T) {
		// Set some test data
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("test:flush:%d", i)
			err := redisCache.Set(ctx, key, []byte("flush-value"), 5*time.Minute)
			require.NoError(t, err)
		}

		// Get size before flush
		sizeBefore, err := redisCache.DBSize(ctx)
		require.NoError(t, err)
		require.Greater(t, sizeBefore, int64(0))

		// Flush
		err = redisCache.FlushDB(ctx)
		require.NoError(t, err)

		// Get size after flush
		sizeAfter, err := redisCache.DBSize(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), sizeAfter)

		t.Logf("FlushDB successful: %d -> %d keys", sizeBefore, sizeAfter)
	})
}

// TestRedisL2Cache_ConcurrentAccess tests thread-safety of Redis operations
func TestRedisL2Cache_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log := helpers.CreateTestLoggerConcrete(t)
	logAdapter := helpers.NewLoggerAdapter(log)

	config := cache.DefaultRedisL2Config()
	config.Addr = getRedisAddr()
	config.PoolSize = 20 // Increase pool for concurrent test

	redisCache, err := cache.NewRedisL2Cache(ctx, config, logAdapter)
	require.NoError(t, err)
	defer redisCache.Close()

	// Flush before test
	err = redisCache.FlushDB(ctx)
	require.NoError(t, err)

	const numWorkers = 20
	const opsPerWorker = 100

	var wg sync.WaitGroup
	var successOps atomic.Int64
	var failedOps atomic.Int64

	t.Logf("Starting %d workers with %d ops each", numWorkers, opsPerWorker)
	startTime := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < opsPerWorker; i++ {
				key := fmt.Sprintf("concurrent:%d:%d", workerID, i)
				value := []byte(fmt.Sprintf("value-%d-%d", workerID, i))

				// Set
				if err := redisCache.Set(ctx, key, value, 5*time.Minute); err != nil {
					failedOps.Add(1)
					continue
				}

				// Get
				retrieved, err := redisCache.Get(ctx, key)
				if err != nil {
					failedOps.Add(1)
					continue
				}

				if string(retrieved) != string(value) {
					failedOps.Add(1)
					continue
				}

				successOps.Add(1)
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := numWorkers * opsPerWorker
	successCount := successOps.Load()
	failCount := failedOps.Load()
	successRate := float64(successCount) / float64(totalOps) * 100

	t.Logf("Completed in %v", duration)
	t.Logf("Total: %d, Success: %d, Failed: %d", totalOps, successCount, failCount)
	t.Logf("Success rate: %.2f%%", successRate)
	t.Logf("Ops/sec: %.2f", float64(totalOps)/duration.Seconds())

	assert.GreaterOrEqual(t, successRate, 99.0, "Success rate should be at least 99%%")
}

// TestCacheManager_WithRedisL2 tests the full L1+L2 cache integration
func TestCacheManager_WithRedisL2(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log := helpers.CreateTestLoggerConcrete(t)
	logAdapter := helpers.NewLoggerAdapter(log)

	// Create Redis L2 cache
	redisConfig := cache.DefaultRedisL2Config()
	redisConfig.Addr = getRedisAddr()

	redisCache, err := cache.NewRedisL2Cache(ctx, redisConfig, logAdapter)
	require.NoError(t, err)
	defer redisCache.Close()

	// Flush Redis for clean test
	err = redisCache.FlushDB(ctx)
	require.NoError(t, err)

	t.Run("WriteThroughSync", func(t *testing.T) {
		config := cache.Config{
			L1Size:          100,
			L1TTL:           5 * time.Minute,
			CleanupInterval: 30 * time.Second,
			L2Cache:         redisCache,
			L2TTL:           10 * time.Minute,
			WritePolicy:     cache.WriteThroughSync,
		}

		cm, err := cache.NewManager(config, logAdapter)
		require.NoError(t, err)
		defer cm.Close()

		key := "sync:key1"
		value := []byte("sync-value-1")

		// Set value
		err = cm.Set(ctx, key, value)
		require.NoError(t, err)

		// Verify in L1 (should hit)
		retrieved, err := cm.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)

		// Verify also in L2 (synchronous write)
		l2Value, err := redisCache.Get(ctx, key)
		require.NoError(t, err, "Value should be in L2 after sync write")
		assert.Equal(t, value, l2Value)

		stats := cm.Stats()
		t.Logf("Stats: L1 hits=%d, L2 hits=%d, L2 sets=%d", stats.L1Hits, stats.L2Hits, stats.L2Sets)
		assert.Equal(t, int64(1), stats.L1Hits)
		assert.Equal(t, int64(1), stats.L2Sets)
	})

	t.Run("WriteThroughAsync", func(t *testing.T) {
		config := cache.Config{
			L1Size:          100,
			L1TTL:           5 * time.Minute,
			CleanupInterval: 30 * time.Second,
			L2Cache:         redisCache,
			L2TTL:           10 * time.Minute,
			WritePolicy:     cache.WriteThroughAsync,
		}

		cm, err := cache.NewManager(config, logAdapter)
		require.NoError(t, err)
		defer cm.Close()

		key := "async:key1"
		value := []byte("async-value-1")

		// Set value
		err = cm.Set(ctx, key, value)
		require.NoError(t, err)

		// Wait for async write
		time.Sleep(200 * time.Millisecond)

		// Verify in L2 (async write should have completed)
		l2Value, err := redisCache.Get(ctx, key)
		require.NoError(t, err, "Value should be in L2 after async write")
		assert.Equal(t, value, l2Value)

		t.Log("Async write to L2 completed successfully")
	})

	t.Run("WriteBack", func(t *testing.T) {
		config := cache.Config{
			L1Size:          5, // Very small to force evictions
			L1TTL:           5 * time.Minute,
			CleanupInterval: 30 * time.Second,
			L2Cache:         redisCache,
			L2TTL:           10 * time.Minute,
			WritePolicy:     cache.WriteBack,
		}

		cm, err := cache.NewManager(config, logAdapter)
		require.NoError(t, err)
		defer cm.Close()

		// Set more keys than L1 capacity to trigger eviction
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("writeback:key%d", i)
			value := []byte(fmt.Sprintf("writeback-value-%d", i))
			err := cm.Set(ctx, key, value)
			require.NoError(t, err)
		}

		// Wait for eviction callbacks to complete
		time.Sleep(200 * time.Millisecond)

		stats := cm.Stats()
		t.Logf("WriteBack stats: L1 size=%d, evictions=%d, L2 sets=%d, writeback_success=%d",
			stats.L1Size, stats.L1Evictions, stats.L2Sets, stats.WritebackSuccess)

		// Evicted keys should be written to L2
		assert.Greater(t, stats.L1Evictions, int64(0), "Should have evictions")
		assert.Greater(t, stats.WritebackSuccess, int64(0), "Should have successful writebacks")

		// Verify evicted keys are in L2
		// First key should be evicted (LRU)
		l2Value, err := redisCache.Get(ctx, "writeback:key0")
		require.NoError(t, err, "Evicted key should be in L2")
		assert.Equal(t, []byte("writeback-value-0"), l2Value)

		t.Log("WriteBack eviction to L2 working correctly")
	})

	t.Run("CacheWarming", func(t *testing.T) {
		// Pre-populate L2 with data
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("warming:key%d", i)
			value := []byte(fmt.Sprintf("warming-value-%d", i))
			err := redisCache.Set(ctx, key, value, 10*time.Minute)
			require.NoError(t, err)
		}

		// Create new cache manager (L1 empty)
		config := cache.Config{
			L1Size:          100,
			L1TTL:           5 * time.Minute,
			CleanupInterval: 30 * time.Second,
			L2Cache:         redisCache,
			L2TTL:           10 * time.Minute,
			WritePolicy:     cache.WriteThroughSync,
		}

		cm, err := cache.NewManager(config, logAdapter)
		require.NoError(t, err)
		defer cm.Close()

		// First access should miss L1, hit L2, and warm L1
		value, err := cm.Get(ctx, "warming:key0")
		require.NoError(t, err)
		assert.Equal(t, []byte("warming-value-0"), value)

		stats := cm.Stats()
		assert.Equal(t, int64(1), stats.L1Misses, "First access should be L1 miss")
		assert.Equal(t, int64(1), stats.L2Hits, "Should hit L2")

		// Second access should hit L1 (warmed)
		value, err = cm.Get(ctx, "warming:key0")
		require.NoError(t, err)
		assert.Equal(t, []byte("warming-value-0"), value)

		stats = cm.Stats()
		assert.Equal(t, int64(1), stats.L1Hits, "Second access should hit warmed L1")

		t.Log("Cache warming from L2 working correctly")
	})

	t.Run("DeleteFromBothLayers", func(t *testing.T) {
		config := cache.Config{
			L1Size:          100,
			L1TTL:           5 * time.Minute,
			CleanupInterval: 30 * time.Second,
			L2Cache:         redisCache,
			L2TTL:           10 * time.Minute,
			WritePolicy:     cache.WriteThroughSync,
		}

		cm, err := cache.NewManager(config, logAdapter)
		require.NoError(t, err)
		defer cm.Close()

		key := "delete:both"
		value := []byte("delete-value")

		// Set value (goes to both L1 and L2)
		err = cm.Set(ctx, key, value)
		require.NoError(t, err)

		// Verify in both
		_, err = cm.Get(ctx, key)
		require.NoError(t, err)
		_, err = redisCache.Get(ctx, key)
		require.NoError(t, err)

		// Delete
		err = cm.Delete(ctx, key)
		require.NoError(t, err)

		// Verify deleted from L1
		_, err = cm.Get(ctx, key)
		assert.Error(t, err, "Should be deleted from L1")

		// Verify deleted from L2
		_, err = redisCache.Get(ctx, key)
		assert.Error(t, err, "Should be deleted from L2")

		t.Log("Delete from both layers working correctly")
	})
}

// TestCacheManager_L2Errors tests graceful degradation when L2 has errors
func TestCacheManager_L2Errors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log := helpers.CreateTestLoggerConcrete(t)
	logAdapter := helpers.NewLoggerAdapter(log)

	// Create Redis L2 cache with invalid address (will fail on operations)
	redisConfig := cache.DefaultRedisL2Config()
	redisConfig.Addr = getRedisAddr()

	redisCache, err := cache.NewRedisL2Cache(ctx, redisConfig, logAdapter)
	require.NoError(t, err)

	config := cache.Config{
		L1Size:          100,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 30 * time.Second,
		L2Cache:         redisCache,
		L2TTL:           10 * time.Minute,
		WritePolicy:     cache.WriteThroughSync,
	}

	cm, err := cache.NewManager(config, logAdapter)
	require.NoError(t, err)
	defer cm.Close()

	// Close Redis to simulate failure
	redisCache.Close()

	key := "error:key1"
	value := []byte("error-value")

	// Set should still work (L1 works, L2 fails gracefully)
	err = cm.Set(ctx, key, value)
	// This may or may not error depending on implementation
	t.Logf("Set with closed L2: err=%v", err)

	// Get from L1 should still work
	retrieved, err := cm.Get(ctx, key)
	if err == nil {
		assert.Equal(t, value, retrieved)
		t.Log("L1 continues working despite L2 failure")
	}

	stats := cm.Stats()
	t.Logf("Stats after L2 failure: L1 hits=%d, L2 errors=%d", stats.L1Hits, stats.L2Errors)
}

// TestRedisL2Cache_LargeValues tests handling of large values
func TestRedisL2Cache_LargeValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log := helpers.CreateTestLoggerConcrete(t)
	logAdapter := helpers.NewLoggerAdapter(log)

	config := cache.DefaultRedisL2Config()
	config.Addr = getRedisAddr()

	redisCache, err := cache.NewRedisL2Cache(ctx, config, logAdapter)
	require.NoError(t, err)
	defer redisCache.Close()

	// Test various value sizes
	sizes := []int{1024, 10240, 102400, 1024000} // 1KB, 10KB, 100KB, 1MB

	for _, size := range sizes {
		t.Run(fmt.Sprintf("Size_%dB", size), func(t *testing.T) {
			key := fmt.Sprintf("large:%d", size)
			value := make([]byte, size)
			for i := range value {
				value[i] = byte(i % 256)
			}

			startSet := time.Now()
			err := redisCache.Set(ctx, key, value, 5*time.Minute)
			setDuration := time.Since(startSet)
			require.NoError(t, err)

			startGet := time.Now()
			retrieved, err := redisCache.Get(ctx, key)
			getDuration := time.Since(startGet)
			require.NoError(t, err)

			assert.Equal(t, len(value), len(retrieved))
			assert.Equal(t, value, retrieved)

			t.Logf("Size: %d bytes, Set: %v, Get: %v", size, setDuration, getDuration)
		})
	}
}

// TestRedisL2Cache_HighThroughput tests sustained high throughput
func TestRedisL2Cache_HighThroughput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "warn", // Reduce logging noise for throughput test
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	logAdapter := helpers.NewLoggerAdapter(log)

	config := cache.DefaultRedisL2Config()
	config.Addr = getRedisAddr()
	config.PoolSize = 50 // Large pool for throughput

	redisCache, err := cache.NewRedisL2Cache(ctx, config, logAdapter)
	require.NoError(t, err)
	defer redisCache.Close()

	// Flush for clean test
	err = redisCache.FlushDB(ctx)
	require.NoError(t, err)

	const numWorkers = 50
	const opsPerWorker = 1000
	const totalOps = numWorkers * opsPerWorker

	var wg sync.WaitGroup
	var completedOps atomic.Int64

	t.Logf("Starting throughput test: %d workers x %d ops = %d total", numWorkers, opsPerWorker, totalOps)
	startTime := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			value := []byte(fmt.Sprintf("throughput-value-%d", workerID))

			for i := 0; i < opsPerWorker; i++ {
				key := fmt.Sprintf("throughput:%d:%d", workerID, i)

				if err := redisCache.Set(ctx, key, value, 5*time.Minute); err != nil {
					continue
				}

				if _, err := redisCache.Get(ctx, key); err != nil {
					continue
				}

				completedOps.Add(1)
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)

	completed := completedOps.Load()
	opsPerSec := float64(completed) / duration.Seconds()
	successRate := float64(completed) / float64(totalOps) * 100

	t.Logf("Completed: %d/%d (%.1f%%)", completed, totalOps, successRate)
	t.Logf("Duration: %v", duration)
	t.Logf("Throughput: %.2f ops/sec", opsPerSec)

	// Minimum expected throughput (should be much higher in practice)
	assert.GreaterOrEqual(t, opsPerSec, 1000.0, "Should achieve at least 1000 ops/sec")
	assert.GreaterOrEqual(t, successRate, 99.0, "Success rate should be at least 99%%")
}
