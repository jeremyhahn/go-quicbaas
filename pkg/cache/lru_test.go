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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLRUCache(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		expected int
	}{
		{
			name:     "positive capacity",
			capacity: 100,
			expected: 100,
		},
		{
			name:     "zero capacity defaults to minimum",
			capacity: 0,
			expected: 100,
		},
		{
			name:     "negative capacity defaults to minimum",
			capacity: -1,
			expected: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewLRUCache(tt.capacity)
			require.NotNil(t, cache)
			assert.Equal(t, tt.expected, cache.Cap())
			assert.Equal(t, 0, cache.Len())
		})
	}
}

func TestLRUCache_AddAndGet(t *testing.T) {
	cache := NewLRUCache(10)

	t.Run("add and get single entry", func(t *testing.T) {
		entry := &Entry{
			Value:    []byte("test-value"),
			CachedAt: time.Now(),
		}

		cache.Add("test-key", entry)

		got, exists := cache.Get("test-key")
		assert.True(t, exists)
		assert.Equal(t, entry.Value, got.Value)
	})

	t.Run("get non-existent key", func(t *testing.T) {
		_, exists := cache.Get("non-existent")
		assert.False(t, exists)
	})

	t.Run("update existing key", func(t *testing.T) {
		entry1 := &Entry{Value: []byte("value1")}
		entry2 := &Entry{Value: []byte("value2")}

		cache.Add("update-key", entry1)
		cache.Add("update-key", entry2)

		got, exists := cache.Get("update-key")
		assert.True(t, exists)
		assert.Equal(t, entry2.Value, got.Value)
		assert.Equal(t, 2, cache.Len()) // Should not increase size
	})
}

func TestLRUCache_Remove(t *testing.T) {
	cache := NewLRUCache(10)

	entry := &Entry{Value: []byte("test-value")}
	cache.Add("test-key", entry)

	t.Run("remove existing key", func(t *testing.T) {
		removed := cache.Remove("test-key")
		assert.True(t, removed)

		_, exists := cache.Get("test-key")
		assert.False(t, exists)
	})

	t.Run("remove non-existent key", func(t *testing.T) {
		removed := cache.Remove("non-existent")
		assert.False(t, removed)
	})
}

func TestLRUCache_LRUEviction(t *testing.T) {
	cache := NewLRUCache(3)

	// Add entries to fill cache
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key-%d", i)
		entry := &Entry{Value: []byte(fmt.Sprintf("value-%d", i))}
		cache.Add(key, entry)
	}

	assert.Equal(t, 3, cache.Len())

	// Add one more to trigger eviction
	entry := &Entry{Value: []byte("value-3")}
	cache.Add("key-3", entry)

	// Cache should still be at capacity
	assert.Equal(t, 3, cache.Len())

	// First key (oldest) should be evicted
	_, exists := cache.Get("key-0")
	assert.False(t, exists)

	// Other keys should exist
	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("key-%d", i)
		_, exists := cache.Get(key)
		assert.True(t, exists, "key %s should exist", key)
	}
}

func TestLRUCache_LRUOrdering(t *testing.T) {
	cache := NewLRUCache(3)

	// Add three entries
	cache.Add("key-1", &Entry{Value: []byte("value-1")})
	cache.Add("key-2", &Entry{Value: []byte("value-2")})
	cache.Add("key-3", &Entry{Value: []byte("value-3")})

	// Access key-1 to move it to front
	cache.Get("key-1")

	// Add new entry - should evict key-2 (oldest after key-1 was accessed)
	cache.Add("key-4", &Entry{Value: []byte("value-4")})

	// key-2 should be evicted
	_, exists := cache.Get("key-2")
	assert.False(t, exists)

	// key-1 should still exist (was recently used)
	_, exists = cache.Get("key-1")
	assert.True(t, exists)

	// key-3 and key-4 should exist
	_, exists = cache.Get("key-3")
	assert.True(t, exists)
	_, exists = cache.Get("key-4")
	assert.True(t, exists)
}

func TestLRUCache_Clear(t *testing.T) {
	cache := NewLRUCache(10)

	// Add some entries
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		entry := &Entry{Value: []byte(fmt.Sprintf("value-%d", i))}
		cache.Add(key, entry)
	}

	assert.Equal(t, 5, cache.Len())

	cache.Clear()

	assert.Equal(t, 0, cache.Len())

	// All keys should be gone
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		_, exists := cache.Get(key)
		assert.False(t, exists)
	}
}

func TestLRUCache_Peek(t *testing.T) {
	cache := NewLRUCache(3)

	// Add entries
	cache.Add("key-1", &Entry{Value: []byte("value-1")})
	cache.Add("key-2", &Entry{Value: []byte("value-2")})
	cache.Add("key-3", &Entry{Value: []byte("value-3")})

	// Peek at key-1 (doesn't update recency)
	entry, exists := cache.Peek("key-1")
	assert.True(t, exists)
	assert.Equal(t, []byte("value-1"), entry.Value)

	// Add new entry - should evict key-1 (peeking didn't update recency)
	cache.Add("key-4", &Entry{Value: []byte("value-4")})

	// key-1 should be evicted
	_, exists = cache.Get("key-1")
	assert.False(t, exists)

	// Peek at a missing key
	entry, exists = cache.Peek("missing")
	assert.False(t, exists)
	assert.Nil(t, entry)
}

func TestLRUCache_Contains(t *testing.T) {
	cache := NewLRUCache(10)

	cache.Add("exists", &Entry{Value: []byte("value")})

	assert.True(t, cache.Contains("exists"))
	assert.False(t, cache.Contains("not-exists"))
}

func TestLRUCache_Keys(t *testing.T) {
	cache := NewLRUCache(10)

	// Add some entries
	expectedKeys := make(map[string]bool)
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		expectedKeys[key] = true
		cache.Add(key, &Entry{Value: []byte(fmt.Sprintf("value-%d", i))})
	}

	keys := cache.Keys()
	assert.Equal(t, len(expectedKeys), len(keys))

	// Verify all keys are present
	for _, key := range keys {
		assert.True(t, expectedKeys[key], "unexpected key: %s", key)
	}
}

func TestLRUCache_GetOldest(t *testing.T) {
	cache := NewLRUCache(10)

	t.Run("empty cache", func(t *testing.T) {
		key, entry, exists := cache.GetOldest()
		assert.False(t, exists)
		assert.Empty(t, key)
		assert.Nil(t, entry)
	})

	t.Run("with entries", func(t *testing.T) {
		cache.Add("key-1", &Entry{Value: []byte("value-1")})
		cache.Add("key-2", &Entry{Value: []byte("value-2")})
		cache.Add("key-3", &Entry{Value: []byte("value-3")})

		key, entry, exists := cache.GetOldest()
		assert.True(t, exists)
		assert.Equal(t, "key-1", key)
		assert.Equal(t, []byte("value-1"), entry.Value)

		// Access key-1 to move it to front
		cache.Get("key-1")

		// Now key-2 should be oldest
		key, entry, exists = cache.GetOldest()
		assert.True(t, exists)
		assert.Equal(t, "key-2", key)
		assert.Equal(t, []byte("value-2"), entry.Value)
	})
}

func TestLRUCache_Resize(t *testing.T) {
	cache := NewLRUCache(5)

	// Fill cache
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Add(key, &Entry{Value: []byte(fmt.Sprintf("value-%d", i))})
	}

	assert.Equal(t, 5, cache.Len())
	assert.Equal(t, 5, cache.Cap())

	t.Run("resize to larger capacity", func(t *testing.T) {
		cache.Resize(10)
		assert.Equal(t, 10, cache.Cap())
		assert.Equal(t, 5, cache.Len())

		// All entries should still exist
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("key-%d", i)
			_, exists := cache.Get(key)
			assert.True(t, exists)
		}
	})

	t.Run("resize to smaller capacity", func(t *testing.T) {
		cache.Resize(3)
		assert.Equal(t, 3, cache.Cap())
		assert.Equal(t, 3, cache.Len())

		// Oldest entries should be evicted
		_, exists := cache.Get("key-0")
		assert.False(t, exists)
		_, exists = cache.Get("key-1")
		assert.False(t, exists)

		// Newer entries should exist
		for i := 2; i < 5; i++ {
			key := fmt.Sprintf("key-%d", i)
			_, exists := cache.Get(key)
			assert.True(t, exists)
		}
	})

	t.Run("resize to zero or negative", func(t *testing.T) {
		oldCap := cache.Cap()
		cache.Resize(0)
		assert.Equal(t, oldCap, cache.Cap()) // Should not change

		cache.Resize(-1)
		assert.Equal(t, oldCap, cache.Cap()) // Should not change
	})
}

func TestLRUCache_Concurrent(t *testing.T) {
	cache := NewLRUCache(100)

	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3)

	// Concurrent adds
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				entry := &Entry{Value: []byte(fmt.Sprintf("value-%d-%d", id, j))}
				cache.Add(key, entry)
			}
		}(i)
	}

	// Concurrent gets
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Get(key)
			}
		}(i)
	}

	// Concurrent removes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Remove(key)
			}
		}(i)
	}

	wg.Wait()

	// Should not panic and cache should be in valid state
	assert.LessOrEqual(t, cache.Len(), cache.Cap())
}

func TestLRUCache_ConcurrentSameKey(t *testing.T) {
	cache := NewLRUCache(100)

	const numGoroutines = 100
	const sharedKey = "shared-key"

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Multiple goroutines updating the same key
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			entry := &Entry{Value: []byte(fmt.Sprintf("value-%d", id))}
			cache.Add(sharedKey, entry)
			cache.Get(sharedKey)
		}(i)
	}

	wg.Wait()

	// Key should exist
	_, exists := cache.Get(sharedKey)
	assert.True(t, exists)
	assert.Equal(t, 1, cache.Len())
}

// Benchmarks

func BenchmarkLRUCache_Add(b *testing.B) {
	cache := NewLRUCache(10000)
	entry := &Entry{Value: []byte("bench-value")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		cache.Add(key, entry)
	}
}

func BenchmarkLRUCache_Get(b *testing.B) {
	cache := NewLRUCache(1000)

	// Pre-populate
	entry := &Entry{Value: []byte("bench-value")}
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Add(key, entry)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%1000)
			cache.Get(key)
			i++
		}
	})
}

func BenchmarkLRUCache_Remove(b *testing.B) {
	cache := NewLRUCache(b.N)

	// Pre-populate
	entry := &Entry{Value: []byte("bench-value")}
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Add(key, entry)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Remove(key)
	}
}

func BenchmarkLRUCache_ConcurrentMixed(b *testing.B) {
	cache := NewLRUCache(1000)

	// Pre-populate
	entry := &Entry{Value: []byte("bench-value")}
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Add(key, entry)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%1000)
			entry := &Entry{Value: []byte(fmt.Sprintf("value-%d", i))}

			switch i % 3 {
			case 0:
				cache.Get(key)
			case 1:
				cache.Add(key, entry)
			case 2:
				cache.Peek(key)
			}
			i++
		}
	})
}

func BenchmarkLRUCache_Eviction(b *testing.B) {
	cache := NewLRUCache(100)

	entry := &Entry{Value: []byte("bench-value")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Add(key, entry)
	}
}
