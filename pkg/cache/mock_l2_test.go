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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestNewMockL2Cache(t *testing.T) {
	t.Run("creates new mock cache", func(t *testing.T) {
		cache := NewMockL2Cache()
		require.NotNil(t, cache)
		assert.NotNil(t, cache.data)
		assert.False(t, cache.closed)
		assert.Nil(t, cache.pingError)
	})
}

func TestMockL2Cache_SetAndGet(t *testing.T) {
	ctx := context.Background()

	t.Run("set and get value", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Set(ctx, "key1", []byte("value1"), 0)
		require.NoError(t, err)

		value, err := cache.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, []byte("value1"), value)
	})

	t.Run("set with TTL", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Set(ctx, "key1", []byte("value1"), 100*time.Millisecond)
		require.NoError(t, err)

		// Should exist immediately
		value, err := cache.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, []byte("value1"), value)
	})

	t.Run("expired entry returns not found", func(t *testing.T) {
		cache := NewMockL2Cache()

		// Set with very short TTL
		err := cache.Set(ctx, "key1", []byte("value1"), 1*time.Millisecond)
		require.NoError(t, err)

		// Wait for expiration
		time.Sleep(10 * time.Millisecond)

		_, err = cache.Get(ctx, "key1")
		require.Error(t, err)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("get non-existent key returns not found", func(t *testing.T) {
		cache := NewMockL2Cache()

		_, err := cache.Get(ctx, "nonexistent")
		require.Error(t, err)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("set with canceled context", func(t *testing.T) {
		cache := NewMockL2Cache()
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		err := cache.Set(ctx, "key1", []byte("value1"), 0)
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("get with canceled context", func(t *testing.T) {
		cache := NewMockL2Cache()
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := cache.Get(ctx, "key1")
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("values are copied to prevent external modification", func(t *testing.T) {
		cache := NewMockL2Cache()

		original := []byte("original")
		err := cache.Set(ctx, "key1", original, 0)
		require.NoError(t, err)

		// Modify original
		original[0] = 'X'

		// Get should return unmodified value
		value, err := cache.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, []byte("original"), value)

		// Modify returned value
		value[0] = 'Y'

		// Get again should return unmodified value
		value2, err := cache.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, []byte("original"), value2)
	})
}

func TestMockL2Cache_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("delete existing key", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Set(ctx, "key1", []byte("value1"), 0)
		require.NoError(t, err)

		err = cache.Delete(ctx, "key1")
		require.NoError(t, err)

		_, err = cache.Get(ctx, "key1")
		require.Error(t, err)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("delete non-existent key succeeds", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Delete(ctx, "nonexistent")
		require.NoError(t, err)
	})

	t.Run("delete with canceled context", func(t *testing.T) {
		cache := NewMockL2Cache()
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		err := cache.Delete(ctx, "key1")
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestMockL2Cache_Ping(t *testing.T) {
	ctx := context.Background()

	t.Run("ping succeeds by default", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Ping(ctx)
		require.NoError(t, err)
	})

	t.Run("ping returns error when set", func(t *testing.T) {
		cache := NewMockL2Cache()
		expectedErr := errors.Internal("test", "ping error")

		cache.SetPingError(expectedErr)

		err := cache.Ping(ctx)
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("ping with canceled context", func(t *testing.T) {
		cache := NewMockL2Cache()
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		err := cache.Ping(ctx)
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestMockL2Cache_Close(t *testing.T) {
	ctx := context.Background()

	t.Run("close succeeds", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Set(ctx, "key1", []byte("value1"), 0)
		require.NoError(t, err)

		err = cache.Close()
		require.NoError(t, err)
		assert.True(t, cache.IsClosed())
	})

	t.Run("operations fail after close", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Close()
		require.NoError(t, err)

		// Get should fail
		_, err = cache.Get(ctx, "key1")
		require.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "cache closed")

		// Set should fail
		err = cache.Set(ctx, "key1", []byte("value1"), 0)
		require.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "cache closed")

		// Delete should fail
		err = cache.Delete(ctx, "key1")
		require.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "cache closed")

		// Ping should fail
		err = cache.Ping(ctx)
		require.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "cache closed")
	})

	t.Run("close clears data", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Set(ctx, "key1", []byte("value1"), 0)
		require.NoError(t, err)
		assert.Equal(t, 1, cache.Len())

		err = cache.Close()
		require.NoError(t, err)

		// Data should be nil, not just empty
		assert.Nil(t, cache.data)
	})
}

func TestMockL2Cache_Clear(t *testing.T) {
	ctx := context.Background()

	t.Run("clear removes all entries", func(t *testing.T) {
		cache := NewMockL2Cache()

		// Add multiple entries
		for i := 0; i < 5; i++ {
			key := string(rune('a' + i))
			err := cache.Set(ctx, key, []byte("value"), 0)
			require.NoError(t, err)
		}

		assert.Equal(t, 5, cache.Len())

		cache.Clear()

		assert.Equal(t, 0, cache.Len())

		// Verify entries are gone
		_, err := cache.Get(ctx, "a")
		require.Error(t, err)
	})

	t.Run("clear on empty cache", func(t *testing.T) {
		cache := NewMockL2Cache()

		cache.Clear()

		assert.Equal(t, 0, cache.Len())
	})
}

func TestMockL2Cache_Len(t *testing.T) {
	ctx := context.Background()

	t.Run("len returns correct count", func(t *testing.T) {
		cache := NewMockL2Cache()

		assert.Equal(t, 0, cache.Len())

		err := cache.Set(ctx, "key1", []byte("value1"), 0)
		require.NoError(t, err)
		assert.Equal(t, 1, cache.Len())

		err = cache.Set(ctx, "key2", []byte("value2"), 0)
		require.NoError(t, err)
		assert.Equal(t, 2, cache.Len())

		err = cache.Delete(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, 1, cache.Len())
	})

	t.Run("len updates on overwrite", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Set(ctx, "key1", []byte("value1"), 0)
		require.NoError(t, err)
		assert.Equal(t, 1, cache.Len())

		// Overwrite same key
		err = cache.Set(ctx, "key1", []byte("value2"), 0)
		require.NoError(t, err)
		assert.Equal(t, 1, cache.Len())
	})
}

func TestMockL2Cache_Keys(t *testing.T) {
	ctx := context.Background()

	t.Run("keys returns all keys", func(t *testing.T) {
		cache := NewMockL2Cache()

		expected := []string{"key1", "key2", "key3"}
		for _, key := range expected {
			err := cache.Set(ctx, key, []byte("value"), 0)
			require.NoError(t, err)
		}

		keys := cache.Keys()
		assert.Len(t, keys, 3)

		// Check all expected keys are present (order doesn't matter)
		for _, key := range expected {
			assert.Contains(t, keys, key)
		}
	})

	t.Run("keys returns empty slice for empty cache", func(t *testing.T) {
		cache := NewMockL2Cache()

		keys := cache.Keys()
		assert.NotNil(t, keys)
		assert.Len(t, keys, 0)
	})
}

func TestMockL2Cache_IsClosed(t *testing.T) {
	t.Run("returns false initially", func(t *testing.T) {
		cache := NewMockL2Cache()
		assert.False(t, cache.IsClosed())
	})

	t.Run("returns true after close", func(t *testing.T) {
		cache := NewMockL2Cache()

		err := cache.Close()
		require.NoError(t, err)
		assert.True(t, cache.IsClosed())
	})
}

func TestMockL2Cache_SetPingError(t *testing.T) {
	ctx := context.Background()

	t.Run("sets and clears ping error", func(t *testing.T) {
		cache := NewMockL2Cache()

		// Initially no error
		err := cache.Ping(ctx)
		require.NoError(t, err)

		// Set error
		expectedErr := errors.Internal("test", "custom error")
		cache.SetPingError(expectedErr)

		err = cache.Ping(ctx)
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)

		// Clear error
		cache.SetPingError(nil)

		err = cache.Ping(ctx)
		require.NoError(t, err)
	})
}

func TestMockL2Cache_Concurrent(t *testing.T) {
	ctx := context.Background()

	t.Run("concurrent operations are safe", func(t *testing.T) {
		cache := NewMockL2Cache()

		done := make(chan bool)

		// Writer goroutines
		for i := 0; i < 10; i++ {
			go func(id int) {
				key := string(rune('a' + id))
				for j := 0; j < 100; j++ {
					_ = cache.Set(ctx, key, []byte("value"), 0)
				}
				done <- true
			}(i)
		}

		// Reader goroutines
		for i := 0; i < 10; i++ {
			go func(id int) {
				key := string(rune('a' + id))
				for j := 0; j < 100; j++ {
					_, _ = cache.Get(ctx, key)
				}
				done <- true
			}(i)
		}

		// Delete goroutines
		for i := 0; i < 5; i++ {
			go func(id int) {
				key := string(rune('a' + id))
				for j := 0; j < 50; j++ {
					_ = cache.Delete(ctx, key)
				}
				done <- true
			}(i)
		}

		// Utility goroutines
		go func() {
			for i := 0; i < 100; i++ {
				_ = cache.Len()
				_ = cache.Keys()
				_ = cache.Ping(ctx)
			}
			done <- true
		}()

		// Wait for all goroutines
		for i := 0; i < 26; i++ {
			<-done
		}

		// Cache should still be functional
		err := cache.Set(ctx, "final", []byte("value"), 0)
		require.NoError(t, err)
	})
}
