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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Integration tests for cache manager - these tests use time.Sleep
// and test timing-dependent behavior like TTL expiration and cleanup.

func TestCacheManager_TTLExpiration(t *testing.T) {
	config := Config{
		L1Size:          10,
		L1TTL:           100 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	key := "expire-key"
	value := []byte("expire-value")

	// Set value
	err = cm.Set(ctx, key, value)
	require.NoError(t, err)

	// Should be accessible immediately
	got, err := cm.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, got)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, err = cm.Get(ctx, key)
	assert.Error(t, err)
	assert.True(t, errors.IsNotFound(err))

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify cleanup ran
	metrics := cm.GetMetrics()
	assert.Greater(t, metrics.Get("l1_expired"), int64(0))
}

func TestCacheManager_ConcurrentStress(t *testing.T) {
	config := Config{
		L1Size: 50,
		L1TTL:  5 * time.Minute,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*3)

	// Writers
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d", j%100) // Reuse keys to create contention
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				if err := cm.Set(ctx, key, value); err != nil {
					errChan <- err
					return
				}
			}
		}(i)
	}

	// Readers
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d", j%100)
				cm.Get(ctx, key) // Ignore errors - may not exist
			}
		}(i)
	}

	// Deleters
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d", j%100)
				if err := cm.Delete(ctx, key); err != nil {
					errChan <- err
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("Concurrent operation error: %v", err)
	}

	// Verify cache is in valid state
	stats := cm.Stats()
	assert.LessOrEqual(t, stats.L1Size, stats.L1Capacity)
	assert.GreaterOrEqual(t, stats.L1Size, 0)

	// Verify metrics were updated
	metrics := cm.GetMetrics()
	assert.Greater(t, metrics.Get("l1_sets"), int64(0))
}

func TestCacheManager_TTLBoundaryConditions(t *testing.T) {
	t.Run("zero TTL means no expiration", func(t *testing.T) {
		config := Config{
			L1Size: 10,
			L1TTL:  0, // No expiration
		}

		cm, err := NewManager(config, testLogger(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()

		// Set value
		err = cm.Set(ctx, "key", []byte("value"))
		require.NoError(t, err)

		// Wait a bit
		time.Sleep(100 * time.Millisecond)

		// Should still be accessible
		val, err := cm.Get(ctx, "key")
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), val)
	})

	t.Run("multiple entries with different expiration times", func(t *testing.T) {
		config := Config{
			L1Size:          10,
			L1TTL:           100 * time.Millisecond,
			CleanupInterval: 20 * time.Millisecond,
		}

		cm, err := NewManager(config, testLogger(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()

		// Set first entry
		cm.Set(ctx, "key1", []byte("value1"))

		// Wait a bit
		time.Sleep(60 * time.Millisecond)

		// Set second entry (will expire later)
		cm.Set(ctx, "key2", []byte("value2"))

		// Wait for first to expire but not second
		time.Sleep(60 * time.Millisecond)

		// key1 should be expired
		_, err = cm.Get(ctx, "key1")
		assert.Error(t, err)

		// key2 should still exist
		_, err = cm.Get(ctx, "key2")
		assert.NoError(t, err)
	})
}
