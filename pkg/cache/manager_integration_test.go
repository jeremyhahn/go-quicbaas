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
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/poll"
)

// Integration tests for cache manager: TTL expiration, background cleanup
// and concurrency. Expiry is driven by backdating an entry (expireNow), never
// by sleeping past a short TTL, so a slow runner cannot change a verdict.

// expireNow backdates key's L1 entry past its TTL. The replacement goes
// through LRUCache.Add, under the cache's lock, so the cleanup goroutine
// never sees a torn entry.
func expireNow(t *testing.T, cm *CacheManager, key string) {
	t.Helper()
	entry, ok := cm.l1Cache.Peek(key)
	require.True(t, ok, "%s is not in L1", key)
	require.False(t, entry.ExpiresAt.IsZero(), "%s has no TTL to expire", key)
	cm.l1Cache.Add(key, &Entry{
		Value:     entry.Value,
		CachedAt:  entry.CachedAt,
		ExpiresAt: time.Now().Add(-time.Second),
	})
}

func TestCacheManager_TTLExpiration(t *testing.T) {
	config := Config{
		L1Size:          10,
		L1TTL:           time.Hour,
		CleanupInterval: 10 * time.Millisecond,
	}

	cm, err := NewManager(config, testLogger(t))
	require.NoError(t, err)
	defer cm.Close()

	ctx := context.Background()

	key := "expire-key"
	value := []byte("expire-value")

	err = cm.Set(ctx, key, value)
	require.NoError(t, err)

	// Accessible while inside its TTL.
	got, err := cm.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, got)

	// Past its TTL, Get reports it missing.
	expireNow(t, cm, key)
	_, err = cm.Get(ctx, key)
	assert.Error(t, err)
	assert.True(t, errors.IsNotFound(err))

	// An expired entry nobody reads is removed by the background cleanup,
	// which counts it.
	require.NoError(t, cm.Set(ctx, "cleanup-key", []byte("v")))
	expiredBefore := cm.GetMetrics().Get("l1_expired")
	expireNow(t, cm, "cleanup-key")
	// The cleanup removes the entry first and counts it after its sweep, so
	// the wait covers both.
	poll.Until(t, 5*time.Second, func() bool {
		return !cm.l1Cache.Contains("cleanup-key") &&
			cm.GetMetrics().Get("l1_expired") > expiredBefore
	}, "the cleanup goroutine never removed and counted the expired entry")
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

		// No TTL: the entry carries no expiry and no cleanup goroutine runs,
		// so no amount of elapsed time removes it.
		entry, ok := cm.l1Cache.Peek("key")
		require.True(t, ok)
		assert.True(t, entry.ExpiresAt.IsZero(), "an entry without TTL got an expiry")
		select {
		case <-cm.cleanupDone:
		default:
			t.Fatal("a manager without TTL started a cleanup goroutine")
		}

		// Still accessible
		val, err := cm.Get(ctx, "key")
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), val)
	})

	t.Run("multiple entries with different expiration times", func(t *testing.T) {
		config := Config{
			L1Size:          10,
			L1TTL:           time.Hour,
			CleanupInterval: 10 * time.Millisecond,
		}

		cm, err := NewManager(config, testLogger(t))
		require.NoError(t, err)
		defer cm.Close()

		ctx := context.Background()

		require.NoError(t, cm.Set(ctx, "key1", []byte("value1")))
		require.NoError(t, cm.Set(ctx, "key2", []byte("value2")))

		// Each entry carries its own expiry: key1 passing its TTL leaves
		// key2, still inside its own, untouched.
		expireNow(t, cm, "key1")

		_, err = cm.Get(ctx, "key1")
		assert.True(t, errors.IsNotFound(err), "key1 is past its TTL: %v", err)

		val, err := cm.Get(ctx, "key2")
		require.NoError(t, err)
		assert.Equal(t, []byte("value2"), val)
	})
}
