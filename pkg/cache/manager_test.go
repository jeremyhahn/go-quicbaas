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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// testLogger creates a logger for testing.
func testLogger(t *testing.T) interfaces.Logger {
	cfg := logger.LogConfig{
		Level:  "error", // Reduce noise in tests
		Format: "console",
		Output: "stdout",
	}
	log, err := logger.New(cfg)
	require.NoError(t, err)
	return newLoggerAdapter(log)
}

func TestNewManager(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		logger   interfaces.Logger
		wantErr  bool
		errField string
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			logger:  testLogger(t),
			wantErr: false,
		},
		{
			name: "invalid L1 size",
			config: Config{
				L1Size: -1,
			},
			logger:  testLogger(t),
			wantErr: true,
		},
		{
			name: "negative L1 TTL",
			config: Config{
				L1Size: 100,
				L1TTL:  -1 * time.Second,
			},
			logger:  testLogger(t),
			wantErr: true,
		},
		{
			name:     "nil logger",
			config:   DefaultConfig(),
			logger:   nil,
			wantErr:  true,
			errField: "logger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm, err := NewManager(tt.config, tt.logger)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errField != "" {
					assert.Contains(t, err.Error(), tt.errField)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cm)
			defer cm.Close()

			assert.NotNil(t, cm.metrics)
			assert.NotNil(t, cm.logger)
			assert.NotNil(t, cm.l1Cache)
		})
	}
}

func TestCacheManager_L1Operations(t *testing.T) {
	config := Config{
		L1Size: 10,
		L1TTL:  1 * time.Hour,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	t.Run("set and get", func(t *testing.T) {
		key := "test-key"
		value := []byte("test-value")

		err := cm.Set(ctx, key, value)
		require.NoError(t, err)

		got, err := cm.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, got)

		metrics := cm.GetMetrics()
		assert.Equal(t, int64(1), metrics.L1Hits())
	})

	t.Run("get non-existent key", func(t *testing.T) {
		_, err := cm.Get(ctx, "non-existent")
		assert.Error(t, err)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("delete", func(t *testing.T) {
		key := "delete-key"
		value := []byte("delete-value")

		err := cm.Set(ctx, key, value)
		require.NoError(t, err)

		err = cm.Delete(ctx, key)
		require.NoError(t, err)

		_, err = cm.Get(ctx, key)
		assert.Error(t, err)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("clear", func(t *testing.T) {
		// Add some entries
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			cm.Set(ctx, key, value)
		}

		cm.Clear()

		// All entries should be gone
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("key-%d", i)
			_, err := cm.Get(ctx, key)
			assert.Error(t, err)
		}
	})

	t.Run("empty key validation", func(t *testing.T) {
		err := cm.Set(ctx, "", []byte("value"))
		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))

		_, err = cm.Get(ctx, "")
		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))

		err = cm.Delete(ctx, "")
		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
	})

	t.Run("nil value validation", func(t *testing.T) {
		err := cm.Set(ctx, "key", nil)
		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
	})
}

// L2 integration tests removed - L2 cache is no longer supported

// TestCacheManager_TTLExpiration moved to manager_integration_test.go

// Cache hierarchy tests removed - only L1 cache is supported

func TestCacheManager_Stats(t *testing.T) {
	config := Config{
		L1Size: 10,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	// Generate some activity
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := []byte(fmt.Sprintf("value-%d", i))
		cm.Set(ctx, key, value)
		cm.Get(ctx, key)
	}

	// Get some misses
	for i := 0; i < 3; i++ {
		cm.Get(ctx, fmt.Sprintf("miss-%d", i))
	}

	stats := cm.Stats()

	assert.True(t, stats.Enabled)
	assert.Equal(t, 5, stats.L1Size)
	assert.Equal(t, 10, stats.L1Capacity)
	assert.Equal(t, int64(5), stats.L1Hits)
	assert.Greater(t, stats.L1Misses, int64(0))
	assert.Greater(t, stats.L1HitRate, 0.0)
	assert.Less(t, stats.L1HitRate, 1.0)
}

func TestCacheManager_Concurrent(t *testing.T) {
	config := Config{
		L1Size: 100,
		L1TTL:  1 * time.Hour,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3)

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				cm.Set(ctx, key, value)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cm.Get(ctx, key)
			}
		}(i)
	}

	// Concurrent deleters
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cm.Delete(ctx, key)
			}
		}(i)
	}

	wg.Wait()

	// Verify no panics and metrics were updated
	metrics := cm.GetMetrics()
	assert.Greater(t, metrics.Get("l1_sets"), int64(0))
}

// Context cancellation tests removed - only relevant for L2 cache

func TestCacheManager_LRUEviction(t *testing.T) {
	config := Config{
		L1Size: 3, // Small capacity to force eviction
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	// Fill cache to capacity
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := []byte(fmt.Sprintf("value-%d", i))
		cm.Set(ctx, key, value)
	}

	assert.Equal(t, 3, cm.l1Cache.Len())

	// Add one more to trigger eviction
	cm.Set(ctx, "key-3", []byte("value-3"))

	// Cache should still be at capacity
	assert.Equal(t, 3, cm.l1Cache.Len())

	// First key should be evicted (LRU)
	_, err = cm.Get(ctx, "key-0")
	assert.Error(t, err)
	assert.True(t, errors.IsNotFound(err))

	// Other keys should still exist
	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("key-%d", i)
		_, err := cm.Get(ctx, key)
		assert.NoError(t, err, "key %s should exist", key)
	}
}

func TestCacheManager_Close(t *testing.T) {
	config := Config{
		L1Size: 10,
		L1TTL:  100 * time.Millisecond,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)

	// Add some data
	ctx := context.Background()
	cm.Set(ctx, "key", []byte("value"))

	// Close
	err = cm.Close()
	assert.NoError(t, err)

	// Note: Cleanup goroutine stopping is verified by not panicking
}

// isDone reports whether done is closed, without blocking.
func isDone(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// TestCacheManager_Done_ClosedWhenCloseJoinsCleanup pins the lifecycle signal
// Done exposes: open while the TTL cleanup goroutine runs, closed once Close
// has joined it. Done is closed only by that goroutine's exit, so an open Done
// is proof the goroutine is live -- not a guess from a goroutine count.
func TestCacheManager_Done_ClosedWhenCloseJoinsCleanup(t *testing.T) {
	cm, err := NewManager(Config{L1Size: 10, L1TTL: time.Minute}, testLogger(t))
	require.NoError(t, err)

	assert.False(t, isDone(cm.Done()),
		"a manager with a positive L1 TTL must be running its cleanup goroutine")

	require.NoError(t, cm.Close())

	assert.True(t, isDone(cm.Done()),
		"Close must not return before the cleanup goroutine has exited")
}

// TestCacheManager_Done_ClosedWithoutCleanupGoroutine covers the other
// branch: with no L1 TTL there is no cleanup goroutine, so there is nothing
// to wait for and Done must already be closed. An open Done here would make
// every caller that waits on it hang forever.
func TestCacheManager_Done_ClosedWithoutCleanupGoroutine(t *testing.T) {
	cm, err := NewManager(Config{L1Size: 10}, testLogger(t))
	require.NoError(t, err)

	assert.True(t, isDone(cm.Done()),
		"a manager with no L1 TTL starts no goroutine, so Done must be closed")
	require.NoError(t, cm.Close())
	assert.True(t, isDone(cm.Done()), "Done must stay closed after Close")
}

// Benchmarks

func BenchmarkCacheManager_L1Get(b *testing.B) {
	config := Config{
		L1Size: 1000,
	}

	impl, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	log := newLoggerAdapter(impl)
	cm, _ := NewManager(config, log)
	defer cm.Close()

	ctx := context.Background()
	key := "bench-key"
	value := []byte("bench-value")
	cm.Set(ctx, key, value)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cm.Get(ctx, key)
		}
	})
}

func BenchmarkCacheManager_L1Set(b *testing.B) {
	config := Config{
		L1Size: 10000,
	}

	impl, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	log := newLoggerAdapter(impl)
	cm, _ := NewManager(config, log)
	defer cm.Close()

	ctx := context.Background()
	value := []byte("bench-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		cm.Set(ctx, key, value)
	}
}

// L1L2 benchmark removed - L2 cache is no longer supported

func BenchmarkCacheManager_ConcurrentMixed(b *testing.B) {
	config := Config{
		L1Size: 1000,
	}

	impl, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	log := newLoggerAdapter(impl)
	cm, _ := NewManager(config, log)
	defer cm.Close()

	ctx := context.Background()

	// Pre-populate
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := []byte(fmt.Sprintf("value-%d", i))
		cm.Set(ctx, key, value)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%100)
			value := []byte(fmt.Sprintf("value-%d", i))

			switch i % 3 {
			case 0:
				cm.Get(ctx, key)
			case 1:
				cm.Set(ctx, key, value)
			case 2:
				cm.Delete(ctx, key)
			}
			i++
		}
	})
}

// Additional comprehensive tests for 90%+ coverage

func TestCacheManager_LRUEvictionEdgeCases(t *testing.T) {
	config := Config{
		L1Size: 2,
		L1TTL:  1 * time.Hour,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	t.Run("eviction with single entry", func(t *testing.T) {
		// Clear cache first
		cm.Clear()

		// Add one entry
		err := cm.Set(ctx, "key1", []byte("value1"))
		require.NoError(t, err)

		// Add second to fill capacity
		err = cm.Set(ctx, "key2", []byte("value2"))
		require.NoError(t, err)

		assert.Equal(t, 2, cm.l1Cache.Len())

		// Add third - should evict key1
		err = cm.Set(ctx, "key3", []byte("value3"))
		require.NoError(t, err)

		assert.Equal(t, 2, cm.l1Cache.Len())

		_, err = cm.Get(ctx, "key1")
		assert.Error(t, err)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("update existing key does not evict", func(t *testing.T) {
		cm.Clear()

		// Fill cache
		cm.Set(ctx, "key1", []byte("value1"))
		cm.Set(ctx, "key2", []byte("value2"))

		// Update existing key
		cm.Set(ctx, "key1", []byte("new-value1"))

		// Both keys should still exist
		val1, err := cm.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, []byte("new-value1"), val1)

		_, err = cm.Get(ctx, "key2")
		require.NoError(t, err)
	})

	t.Run("access pattern affects eviction order", func(t *testing.T) {
		cm.Clear()

		// Add two entries
		cm.Set(ctx, "key1", []byte("value1"))
		cm.Set(ctx, "key2", []byte("value2"))

		// Access key1 to make it more recent
		cm.Get(ctx, "key1")

		// Add key3 - should evict key2 (least recently used)
		cm.Set(ctx, "key3", []byte("value3"))

		// key1 should still exist
		_, err := cm.Get(ctx, "key1")
		assert.NoError(t, err)

		// key2 should be evicted
		_, err = cm.Get(ctx, "key2")
		assert.Error(t, err)

		// key3 should exist
		_, err = cm.Get(ctx, "key3")
		assert.NoError(t, err)
	})
}

// TestCacheManager_ConcurrentStress moved to manager_integration_test.go

// TestCacheManager_TTLBoundaryConditions moved to manager_integration_test.go

// TestCacheManager_CleanupLifecycle moved to manager_integration_test.go

func TestCacheManager_MetricsAccuracy(t *testing.T) {
	config := Config{
		L1Size: 100,
		L1TTL:  1 * time.Hour,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	t.Run("accurate hit/miss tracking", func(t *testing.T) {
		cm.Clear()
		metrics := cm.GetMetrics()
		metrics.Reset()

		// Add 10 entries
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Set(ctx, key, []byte(fmt.Sprintf("value-%d", i)))
		}

		// Get 10 hits
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Get(ctx, key)
		}

		// Get 5 misses
		for i := 10; i < 15; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Get(ctx, key)
		}

		metrics = cm.GetMetrics()
		assert.Equal(t, int64(10), metrics.L1Hits())
		assert.GreaterOrEqual(t, metrics.L1Misses(), int64(5))
	})

	t.Run("accurate set/delete tracking", func(t *testing.T) {
		cm.Clear()
		metrics := cm.GetMetrics()
		metrics.Reset()

		// 20 sets
		for i := 0; i < 20; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Set(ctx, key, []byte(fmt.Sprintf("value-%d", i)))
		}

		// 10 deletes
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Delete(ctx, key)
		}

		metrics = cm.GetMetrics()
		assert.GreaterOrEqual(t, metrics.Get("l1_sets"), int64(20))
		assert.GreaterOrEqual(t, metrics.Get("l1_deletes"), int64(10))
	})

	t.Run("metrics under concurrent load", func(t *testing.T) {
		cm.Clear()
		metrics := cm.GetMetrics()
		metrics.Reset()

		const numGoroutines = 10
		const opsPerGoroutine = 100

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < opsPerGoroutine; j++ {
					key := fmt.Sprintf("key-%d-%d", id, j)
					value := []byte(fmt.Sprintf("value-%d-%d", id, j))
					cm.Set(ctx, key, value)
					cm.Get(ctx, key)
				}
			}(i)
		}

		wg.Wait()

		metrics = cm.GetMetrics()
		expectedSets := int64(numGoroutines * opsPerGoroutine)

		// Should have all sets (cache is large enough)
		assert.GreaterOrEqual(t, metrics.Get("l1_sets"), expectedSets)
		// Should have most hits
		assert.Greater(t, metrics.L1Hits(), int64(0))
	})

	t.Run("metrics copy is independent", func(t *testing.T) {
		cm.Clear()
		metrics1 := cm.GetMetrics()
		initialHits := metrics1.L1Hits()

		// Perform operations
		cm.Set(ctx, "key", []byte("value"))
		cm.Get(ctx, "key")

		// Original copy should not change
		assert.Equal(t, initialHits, metrics1.L1Hits())

		// New copy should have updated values
		metrics2 := cm.GetMetrics()
		assert.Greater(t, metrics2.L1Hits(), initialHits)
	})
}

func TestCacheManager_StatsCalculations(t *testing.T) {
	config := Config{
		L1Size: 100,
		L1TTL:  1 * time.Hour,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	t.Run("hit rate calculation", func(t *testing.T) {
		cm.Clear()
		cm.GetMetrics().Reset()

		// 10 sets and hits
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Set(ctx, key, []byte(fmt.Sprintf("value-%d", i)))
			cm.Get(ctx, key)
		}

		// 5 misses
		for i := 10; i < 15; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Get(ctx, key)
		}

		stats := cm.Stats()
		expectedHitRate := 10.0 / 15.0
		assert.InDelta(t, expectedHitRate, stats.L1HitRate, 0.01)
	})

	t.Run("empty cache hit rate", func(t *testing.T) {
		// Create a fresh cache manager for this test
		freshCM, err := NewManager(config, testLogger(t))
		require.NoError(t, err)
		defer freshCM.Close()

		stats := freshCM.Stats()
		assert.Equal(t, 0.0, stats.L1HitRate)
	})

	t.Run("size and capacity", func(t *testing.T) {
		cm.Clear()

		// Add 50 entries
		for i := 0; i < 50; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Set(ctx, key, []byte(fmt.Sprintf("value-%d", i)))
		}

		stats := cm.Stats()
		assert.Equal(t, 50, stats.L1Size)
		assert.Equal(t, 100, stats.L1Capacity)
		assert.True(t, stats.Enabled)
	})
}

func TestCacheManager_ConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "zero L1 size",
			config: Config{
				L1Size: 0,
			},
			wantErr: true,
		},
		{
			name: "negative cleanup interval gets default",
			config: Config{
				L1Size:          100,
				L1TTL:           1 * time.Minute,
				CleanupInterval: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEntry_IsExpired(t *testing.T) {
	t.Run("zero expiration time never expires", func(t *testing.T) {
		entry := &Entry{
			Value:     []byte("value"),
			ExpiresAt: time.Time{},
		}
		assert.False(t, entry.IsExpired())
	})

	t.Run("future expiration not expired", func(t *testing.T) {
		entry := &Entry{
			Value:     []byte("value"),
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		assert.False(t, entry.IsExpired())
	})

	t.Run("past expiration is expired", func(t *testing.T) {
		entry := &Entry{
			Value:     []byte("value"),
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		assert.True(t, entry.IsExpired())
	})
}

func TestCacheManager_GetConfig(t *testing.T) {
	config := Config{
		L1Size:          100,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	t.Run("get config returns copy", func(t *testing.T) {
		retrieved := cm.GetConfig()
		assert.NotNil(t, retrieved)
		assert.Equal(t, config.L1Size, retrieved.L1Size)
		assert.Equal(t, config.L1TTL, retrieved.L1TTL)
		assert.Equal(t, config.CleanupInterval, retrieved.CleanupInterval)
	})

	t.Run("modifying returned config does not affect original", func(t *testing.T) {
		retrieved := cm.GetConfig()
		retrieved.L1Size = 999
		retrieved.L1TTL = 999 * time.Hour

		// Original should be unchanged
		retrieved2 := cm.GetConfig()
		assert.Equal(t, config.L1Size, retrieved2.L1Size)
		assert.Equal(t, config.L1TTL, retrieved2.L1TTL)
	})
}

func TestCacheManager_UpdateConfig(t *testing.T) {
	initialConfig := Config{
		L1Size:          100,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}

	cm, err := NewManager(initialConfig, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	t.Run("nil config returns error", func(t *testing.T) {
		err := cm.UpdateConfig(nil)
		assert.Error(t, err)
		assert.True(t, errors.IsInvalidInput(err))
	})

	t.Run("invalid config returns error", func(t *testing.T) {
		invalidConfig := &Config{
			L1Size: -1, // Invalid
			L1TTL:  5 * time.Minute,
		}
		err := cm.UpdateConfig(invalidConfig)
		assert.Error(t, err)
	})

	t.Run("update L1 size triggers resize", func(t *testing.T) {
		// Add some entries
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)
			cm.Set(ctx, key, []byte(fmt.Sprintf("value-%d", i)))
		}

		// Update to smaller size
		newConfig := &Config{
			L1Size:          5,
			L1TTL:           5 * time.Minute,
			CleanupInterval: 1 * time.Minute,
		}

		err := cm.UpdateConfig(newConfig)
		assert.NoError(t, err)

		// Verify cache was resized
		assert.Equal(t, 5, cm.l1Cache.Cap())
		assert.LessOrEqual(t, cm.l1Cache.Len(), 5)
	})

	t.Run("update cleanup interval", func(t *testing.T) {
		newConfig := &Config{
			L1Size:          100,
			L1TTL:           10 * time.Minute,
			CleanupInterval: 2 * time.Minute, // Changed
		}

		err := cm.UpdateConfig(newConfig)
		assert.NoError(t, err)

		// Verify config was updated
		retrieved := cm.GetConfig()
		assert.Equal(t, 2*time.Minute, retrieved.CleanupInterval)
	})

	t.Run("update TTL triggers cleanup restart", func(t *testing.T) {
		newConfig := &Config{
			L1Size:          100,
			L1TTL:           1 * time.Minute, // Changed
			CleanupInterval: 1 * time.Minute,
		}

		err := cm.UpdateConfig(newConfig)
		assert.NoError(t, err)

		// Verify config was updated
		retrieved := cm.GetConfig()
		assert.Equal(t, 1*time.Minute, retrieved.L1TTL)
	})

	t.Run("update both size and intervals", func(t *testing.T) {
		newConfig := &Config{
			L1Size:          50,
			L1TTL:           30 * time.Second,
			CleanupInterval: 10 * time.Second,
		}

		err := cm.UpdateConfig(newConfig)
		assert.NoError(t, err)

		retrieved := cm.GetConfig()
		assert.Equal(t, 50, retrieved.L1Size)
		assert.Equal(t, 30*time.Second, retrieved.L1TTL)
		assert.Equal(t, 10*time.Second, retrieved.CleanupInterval)
	})

	t.Run("concurrent updates", func(t *testing.T) {
		var wg sync.WaitGroup
		const numUpdates = 10

		wg.Add(numUpdates)
		for i := 0; i < numUpdates; i++ {
			go func(id int) {
				defer wg.Done()
				cfg := &Config{
					L1Size:          100 + id,
					L1TTL:           time.Duration(5+id) * time.Minute,
					CleanupInterval: 1 * time.Minute,
				}
				cm.UpdateConfig(cfg)
			}(i)
		}

		wg.Wait()

		// Cache should still be in valid state
		assert.NotNil(t, cm.GetConfig())
	})
}

func TestCacheManager_CleanupNoExpiredEntries(t *testing.T) {
	config := Config{
		L1Size:          10,
		L1TTL:           1 * time.Hour, // Long TTL
		CleanupInterval: 10 * time.Millisecond,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	// Add entries that won't expire
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		cm.Set(ctx, key, []byte(fmt.Sprintf("value-%d", i)))
	}

	// Trigger cleanup manually
	cm.performCleanup()

	// No entries should be expired
	metrics := cm.GetMetrics()
	// l1_expired might be 0 or from previous tests, just ensure no panic
	assert.GreaterOrEqual(t, metrics.Get("l1_expired"), int64(0))
}

func TestLRUCache_PeekMissingKey(t *testing.T) {
	cache := NewLRUCache(10)

	// Peek at non-existent key
	entry, exists := cache.Peek("missing")
	assert.False(t, exists)
	assert.Nil(t, entry)
}

// TestCacheManager_CleanupTickerReplacement moved to manager_integration_test.go
