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

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// testLoggerWriteBack creates a logger for testing.
func testLoggerWriteBack(t *testing.T) interfaces.Logger {
	cfg := logger.LogConfig{
		Level:  "error", // Reduce noise in tests
		Format: "console",
		Output: "stdout",
	}
	log, err := logger.New(cfg)
	require.NoError(t, err)
	return newLoggerAdapter(log)
}

func TestLRUCache_EvictionCallback(t *testing.T) {
	t.Run("callback invoked on eviction", func(t *testing.T) {
		cache := NewLRUCache(2)

		var evictedKeys []string
		var evictedValues [][]byte

		callback := func(key string, entry *Entry) {
			evictedKeys = append(evictedKeys, key)
			evictedValues = append(evictedValues, entry.Value)
		}

		cache.SetEvictCallback(callback)

		// Add entries that will fill the cache
		cache.Add("key1", &Entry{Value: []byte("value1")})
		cache.Add("key2", &Entry{Value: []byte("value2")})

		// This should trigger eviction of key1
		cache.Add("key3", &Entry{Value: []byte("value3")})

		assert.Len(t, evictedKeys, 1)
		assert.Equal(t, "key1", evictedKeys[0])
		assert.Equal(t, []byte("value1"), evictedValues[0])
	})

	t.Run("callback invoked on manual removal", func(t *testing.T) {
		cache := NewLRUCache(5)

		var evictedKey string
		callback := func(key string, entry *Entry) {
			evictedKey = key
		}

		cache.SetEvictCallback(callback)
		cache.Add("test", &Entry{Value: []byte("value")})
		cache.Remove("test")

		assert.Equal(t, "test", evictedKey)
	})

	t.Run("callback invoked on resize", func(t *testing.T) {
		cache := NewLRUCache(5)

		evictedCount := 0
		callback := func(key string, entry *Entry) {
			evictedCount++
		}

		cache.SetEvictCallback(callback)

		// Fill cache
		for i := 0; i < 5; i++ {
			cache.Add(string(rune('a'+i)), &Entry{Value: []byte{byte(i)}})
		}

		// Resize to smaller capacity
		cache.Resize(2)

		assert.Equal(t, 3, evictedCount)
		assert.Equal(t, 2, cache.Len())
	})

	t.Run("callback can be cleared", func(t *testing.T) {
		cache := NewLRUCache(1)

		invoked := false
		callback := func(key string, entry *Entry) {
			invoked = true
		}

		cache.SetEvictCallback(callback)
		cache.Add("key1", &Entry{Value: []byte("value1")})
		cache.Add("key2", &Entry{Value: []byte("value2")})
		assert.True(t, invoked)

		// Clear callback
		invoked = false
		cache.SetEvictCallback(nil)
		cache.Add("key3", &Entry{Value: []byte("value3")})
		assert.False(t, invoked)
	})
}

func TestCacheManager_WriteBackPolicy(t *testing.T) {
	t.Run("WriteBack policy is valid", func(t *testing.T) {
		assert.True(t, WriteBack.IsValid())
		assert.False(t, WriteBack.IsWriteThrough())
		assert.False(t, WriteBack.IsAsync())
	})

	t.Run("WriteBack does not write to L2 on Set", func(t *testing.T) {
		mockL2 := NewMockL2Cache()

		config := Config{
			L1Size:      10,
			L1TTL:       5 * time.Minute,
			L2Cache:     mockL2,
			L2TTL:       10 * time.Minute,
			WritePolicy: WriteBack,
		}

		cm, err := NewManager(config, testLoggerWriteBack(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()
		err = cm.Set(ctx, "key1", []byte("value1"))
		require.NoError(t, err)

		// L2 should not have the key yet
		assert.Equal(t, 0, mockL2.Len())

		// L1 should have the key
		value, err := cm.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, []byte("value1"), value)
	})

	t.Run("WriteBack writes to L2 on eviction", func(t *testing.T) {
		mockL2 := NewMockL2Cache()

		config := Config{
			L1Size:      2, // Small cache to trigger evictions
			L1TTL:       1 * time.Hour,
			L2Cache:     mockL2,
			L2TTL:       10 * time.Minute,
			WritePolicy: WriteBack,
		}

		cm, err := NewManager(config, testLoggerWriteBack(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()

		// Fill L1 cache
		err = cm.Set(ctx, "key1", []byte("value1"))
		require.NoError(t, err)
		err = cm.Set(ctx, "key2", []byte("value2"))
		require.NoError(t, err)

		// Verify L2 is still empty
		assert.Equal(t, 0, mockL2.Len())

		// Add third entry to trigger eviction
		err = cm.Set(ctx, "key3", []byte("value3"))
		require.NoError(t, err)

		// The writeback runs in its own goroutine; wait for its metric.
		require.Eventually(t, func() bool {
			return cm.GetMetrics().Get("writeback_success") == 1
		}, 5*time.Second, time.Millisecond)

		// L2 should now have the evicted entry
		assert.Equal(t, 1, mockL2.Len())

		// Verify the evicted entry is in L2
		value, err := mockL2.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, []byte("value1"), value)

		// Verify metrics
		metrics := cm.GetMetrics()
		assert.Equal(t, int64(1), metrics.Get("writeback_success"))
		assert.Equal(t, int64(1), metrics.Get("l2_sets"))
	})

	t.Run("WriteBack cache warming from L2", func(t *testing.T) {
		mockL2 := NewMockL2Cache()

		config := Config{
			L1Size:      2,
			L1TTL:       1 * time.Hour,
			L2Cache:     mockL2,
			L2TTL:       10 * time.Minute,
			WritePolicy: WriteBack,
		}

		cm, err := NewManager(config, testLoggerWriteBack(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()

		// Manually add to L2 to simulate evicted data
		err = mockL2.Set(ctx, "cached-key", []byte("cached-value"), 10*time.Minute)
		require.NoError(t, err)

		// First Get should miss L1, hit L2, and warm L1
		value, err := cm.Get(ctx, "cached-key")
		require.NoError(t, err)
		assert.Equal(t, []byte("cached-value"), value)

		// Verify L1 is warmed
		entry, ok := cm.l1Cache.Get("cached-key")
		assert.True(t, ok)
		assert.Equal(t, []byte("cached-value"), entry.Value)

		// Second Get should hit L1 (warmed from previous call)
		value2, err := cm.Get(ctx, "cached-key")
		require.NoError(t, err)
		assert.Equal(t, []byte("cached-value"), value2)

		// Verify metrics
		metrics := cm.GetMetrics()
		assert.Equal(t, int64(1), metrics.Get("l1_misses")) // First Get missed L1
		assert.Equal(t, int64(1), metrics.Get("l2_hits"))   // First Get hit L2
		assert.Equal(t, int64(1), metrics.Get("l1_hits"))   // Second Get hit L1
	})

	t.Run("WriteBack graceful degradation on L2 failure", func(t *testing.T) {
		mockL2 := NewMockL2Cache()

		config := Config{
			L1Size:      1,
			L1TTL:       1 * time.Hour,
			L2Cache:     mockL2,
			L2TTL:       10 * time.Minute,
			WritePolicy: WriteBack,
		}

		cm, err := NewManager(config, testLoggerWriteBack(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()

		// Fill L1
		err = cm.Set(ctx, "key1", []byte("value1"))
		require.NoError(t, err)

		// Close L2 to simulate failure
		mockL2.Close()

		// Trigger eviction (should not panic)
		err = cm.Set(ctx, "key2", []byte("value2"))
		require.NoError(t, err)

		// The writeback runs in its own goroutine; wait for its metric.
		require.Eventually(t, func() bool {
			return cm.GetMetrics().Get("writeback_failures") == 1
		}, 5*time.Second, time.Millisecond)

		// Verify metrics track the failure
		metrics := cm.GetMetrics()
		assert.Equal(t, int64(1), metrics.Get("writeback_failures"))
		assert.Equal(t, int64(1), metrics.Get("l2_errors"))
	})

	t.Run("WriteBack policy update enables callback", func(t *testing.T) {
		mockL2 := NewMockL2Cache()

		// Start with WriteThrough
		config := Config{
			L1Size:      2,
			L1TTL:       1 * time.Hour,
			L2Cache:     mockL2,
			L2TTL:       10 * time.Minute,
			WritePolicy: WriteThroughSync,
		}

		cm, err := NewManager(config, testLoggerWriteBack(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()

		// Add entries with WriteThrough
		err = cm.Set(ctx, "key1", []byte("value1"))
		require.NoError(t, err)

		// L2 should have the entry
		assert.Equal(t, 1, mockL2.Len())

		mockL2.Clear()

		// Update to WriteBack policy
		newConfig := &Config{
			L1Size:      2,
			L1TTL:       1 * time.Hour,
			L2Cache:     mockL2,
			L2TTL:       10 * time.Minute,
			WritePolicy: WriteBack,
		}

		err = cm.UpdateConfig(newConfig)
		require.NoError(t, err)

		// Now Set should not write to L2
		err = cm.Set(ctx, "key2", []byte("value2"))
		require.NoError(t, err)

		assert.Equal(t, 0, mockL2.Len())
	})
}

func TestCacheManager_ConcurrentWriteBack(t *testing.T) {
	t.Run("concurrent evictions with WriteBack", func(t *testing.T) {
		mockL2 := NewMockL2Cache()

		config := Config{
			L1Size:      5,
			L1TTL:       1 * time.Hour,
			L2Cache:     mockL2,
			L2TTL:       10 * time.Minute,
			WritePolicy: WriteBack,
		}

		cm, err := NewManager(config, testLoggerWriteBack(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()

		// Add many entries concurrently to trigger evictions
		done := make(chan bool)

		for i := 0; i < 3; i++ {
			go func(start int) {
				for j := 0; j < 20; j++ {
					key := string(rune('a' + (start*20 + j)))
					value := []byte{byte(start*20 + j)}
					cm.Set(ctx, key, value)
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 3; i++ {
			<-done
		}

		// Each eviction starts one writeback goroutine; wait for all of them.
		evictions := cm.l1Cache.EvictionCount()
		require.Eventually(t, func() bool {
			m := cm.GetMetrics()
			return m.Get("writeback_success")+m.Get("writeback_failures") == evictions
		}, 5*time.Second, time.Millisecond)

		// Verify L1 is at capacity
		assert.Equal(t, 5, cm.l1Cache.Len())

		// Verify L2 has evicted entries
		assert.Greater(t, mockL2.Len(), 0)

		// Verify no errors occurred
		metrics := cm.GetMetrics()
		assert.Equal(t, int64(0), metrics.Get("writeback_failures"))
	})
}
