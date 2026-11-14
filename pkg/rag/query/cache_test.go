//go:build rag

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

package query

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"context"
	"errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCache(t *testing.T) {
	cache := NewCache(100)
	assert.NotNil(t, cache)

	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, 0.0, stats.HitRate)
}

func TestCache_SetGet(t *testing.T) {
	tests := []struct {
		name       string
		capacity   int
		sets       []string // queries stored under "key1", in order
		wantOK     bool
		wantQuery  string
		wantHits   int64
		wantMisses int64
		wantSize   int
	}{
		{name: "hit after set", capacity: 10, sets: []string{"test"}, wantOK: true, wantQuery: "test", wantHits: 1, wantSize: 1},
		{name: "miss on absent key", capacity: 10, wantMisses: 1},
		{name: "set on existing key replaces value", capacity: 10, sets: []string{"original", "updated"}, wantOK: true, wantQuery: "updated", wantHits: 1, wantSize: 1},
		{name: "zero capacity evicts at once", capacity: 0, sets: []string{"test"}, wantMisses: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(tt.capacity)
			for _, query := range tt.sets {
				cache.Set("key1", &Response{Query: query}, time.Minute)
			}

			got, ok := cache.Get("key1")
			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantQuery, got.Query)
			} else {
				assert.Nil(t, got)
			}

			stats := cache.Stats()
			assert.Equal(t, tt.wantHits, stats.Hits)
			assert.Equal(t, tt.wantMisses, stats.Misses)
			assert.Equal(t, tt.wantSize, stats.Size)
		})
	}
}

func TestCache_LRUEviction(t *testing.T) {
	cache := NewCache(3)

	// Fill cache to capacity
	for i := 1; i <= 3; i++ {
		cache.Set(fmt.Sprintf("key%d", i), &Response{Query: fmt.Sprintf("query%d", i)}, 1*time.Minute)
	}

	stats := cache.Stats()
	assert.Equal(t, 3, stats.Size)

	// Access key1 to make it most recent
	cache.Get("key1")

	// Add new item, should evict least recently used (key2)
	cache.Set("key4", &Response{Query: "query4"}, 1*time.Minute)

	// key2 should be evicted
	_, ok := cache.Get("key2")
	assert.False(t, ok)

	// key1, key3, key4 should still be present
	_, ok = cache.Get("key1")
	assert.True(t, ok)
	_, ok = cache.Get("key3")
	assert.True(t, ok)
	_, ok = cache.Get("key4")
	assert.True(t, ok)
}

func TestCache_Clear(t *testing.T) {
	cache := NewCache(10)

	// Add some entries
	for i := 1; i <= 5; i++ {
		cache.Set(fmt.Sprintf("key%d", i), &Response{Query: fmt.Sprintf("query%d", i)}, 1*time.Minute)
	}

	stats := cache.Stats()
	assert.Equal(t, 5, stats.Size)

	// Clear cache
	cache.Clear()

	// Verify cache is empty
	stats = cache.Stats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)

	// Values should not be accessible
	_, ok := cache.Get("key1")
	assert.False(t, ok)
}

func TestCache_HitRate(t *testing.T) {
	cache := NewCache(10)

	cache.Set("key1", &Response{Query: "test"}, 1*time.Minute)

	// 2 hits
	cache.Get("key1")
	cache.Get("key1")

	// 3 misses
	cache.Get("key2")
	cache.Get("key3")
	cache.Get("key4")

	stats := cache.Stats()
	assert.Equal(t, int64(2), stats.Hits)
	assert.Equal(t, int64(3), stats.Misses)
	assert.InDelta(t, 0.4, stats.HitRate, 0.01) // 2/5 = 0.4
}

// TestCache_Expiry stores expired entries with a negative TTL, so no test
// waits for a deadline to pass.
func TestCache_Expiry(t *testing.T) {
	tests := []struct {
		name    string
		expired []string
		live    []string
		clean   bool
	}{
		{name: "Get drops an expired entry", expired: []string{"key1"}},
		{name: "CleanExpired removes only expired entries", expired: []string{"key1", "key2"}, live: []string{"key-long"}, clean: true},
		{name: "CleanExpired with nothing expired", live: []string{"key1"}, clean: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache(10)
			for _, key := range tt.expired {
				cache.Set(key, &Response{Query: key}, -time.Minute)
			}
			for _, key := range tt.live {
				cache.Set(key, &Response{Query: key}, time.Minute)
			}

			if tt.clean {
				removed := cache.(*lruCache).CleanExpired()
				assert.Equal(t, len(tt.expired), removed)
				assert.Equal(t, len(tt.live), cache.Stats().Size)
			}

			for _, key := range tt.expired {
				got, ok := cache.Get(key)
				assert.False(t, ok, key)
				assert.Nil(t, got)
			}
			for _, key := range tt.live {
				_, ok := cache.Get(key)
				assert.True(t, ok, key)
			}
			assert.Equal(t, len(tt.live), cache.Stats().Size)
		})
	}
}

func TestCache_Concurrency(t *testing.T) {
	cache := NewCache(100)
	const numGoroutines = 10
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Set(key, &Response{Query: key}, 1*time.Minute)
			}
		}(i)
	}

	wg.Wait()

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Get(key)
			}
		}(i)
	}

	wg.Wait()

	// Should not panic and have reasonable stats
	stats := cache.Stats()
	assert.Greater(t, stats.Size, 0)
}

func TestCache_ConcurrentUpdateSameKey(t *testing.T) {
	cache := NewCache(10)
	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent updates to same key
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			cache.Set("shared-key", &Response{Query: fmt.Sprintf("query-%d", id)}, 1*time.Minute)
		}(i)
	}

	wg.Wait()

	// Should have exactly one entry
	stats := cache.Stats()
	assert.Equal(t, 1, stats.Size)

	// Should be accessible
	_, ok := cache.Get("shared-key")
	assert.True(t, ok)
}

func BenchmarkCache_Set(b *testing.B) {
	cache := NewCache(10000)
	response := &Response{Query: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(fmt.Sprintf("key%d", i), response, 1*time.Minute)
	}
}

func BenchmarkCache_Get(b *testing.B) {
	cache := NewCache(10000)
	response := &Response{Query: "test"}

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("key%d", i), response, 1*time.Minute)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(fmt.Sprintf("key%d", i%1000))
	}
}

func BenchmarkCache_Concurrent(b *testing.B) {
	cache := NewCache(10000)
	response := &Response{Query: "test"}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key%d", i%1000)
			if i%2 == 0 {
				cache.Set(key, response, 1*time.Minute)
			} else {
				cache.Get(key)
			}
			i++
		}
	})
}

func TestHandler_StreamQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		setup   func(e *mockEmbedder, s *mockSearcher)
		wantErr error
	}{
		{name: "ends with a done chunk", query: "test query"},
		{name: "empty query", query: "", wantErr: ErrEmptyQuery},
		{
			name:    "embedding error",
			query:   "test query",
			setup:   func(e *mockEmbedder, _ *mockSearcher) { e.setError(errors.New("embedding failed")) },
			wantErr: ErrEmbeddingFailed,
		},
		{
			name:    "search error",
			query:   "test query",
			setup:   func(_ *mockEmbedder, s *mockSearcher) { s.setError(errors.New("search failed")) },
			wantErr: ErrSearchFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder := newMockEmbedder()
			searcher := newMockSearcher()
			if tt.setup != nil {
				tt.setup(embedder, searcher)
			}

			h, err := NewHandler(embedder, searcher, DefaultConfig())
			require.NoError(t, err)
			defer h.Close()

			ch, err := h.StreamQuery(context.Background(), tt.query, DefaultOptions())
			require.NoError(t, err)
			require.NotNil(t, ch)

			if tt.wantErr != nil {
				chunk := <-ch
				assert.ErrorIs(t, chunk.Error, tt.wantErr)
				_, ok := <-ch
				assert.False(t, ok, "channel must close after the error chunk")
				return
			}

			var lastChunk StreamChunk
			for chunk := range ch {
				lastChunk = chunk
			}
			assert.True(t, lastChunk.Done)
			assert.Nil(t, lastChunk.Error)
			assert.Nil(t, lastChunk.Result)
		})
	}
}

func TestHandler_buildContext(t *testing.T) {
	tests := []struct {
		name        string
		results     []Result
		want        []string
		wantMissing []string
	}{
		{
			name:    "empty results",
			results: []Result{},
			want:    []string{"No relevant results found."},
		},
		{
			name: "no source metadata",
			results: []Result{
				{ID: "1", Score: 0.95, Content: "Test content", Metadata: map[string]interface{}{"other": "value"}},
			},
			want:        []string{"Test content"},
			wantMissing: []string{"Source:"},
		},
		{
			name: "non-string source",
			results: []Result{
				{ID: "1", Score: 0.95, Content: "Test content", Metadata: map[string]interface{}{"source": 123}},
			},
			want:        []string{"Test content"},
			wantMissing: []string{"Source:"},
		},
		{
			name: "multiple results with sources",
			results: []Result{
				{ID: "1", Score: 0.95, Content: "First result", Metadata: map[string]interface{}{"source": "file1.go"}},
				{ID: "2", Score: 0.85, Content: "Second result", Metadata: map[string]interface{}{"source": "file2.go"}},
				{ID: "3", Score: 0.75, Content: "Third result", Metadata: map[string]interface{}{"source": "file3.go"}},
			},
			want: []string{
				"First result", "Second result", "Third result",
				"0.95", "0.85", "0.75",
				"Source: file1.go", "Source: file2.go", "Source: file3.go",
				"\n\n",
			},
		},
	}

	opts := DefaultOptions()
	opts.IncludeSource = true

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&handler{}).buildContext(tt.results, opts)
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
			for _, missing := range tt.wantMissing {
				assert.NotContains(t, got, missing)
			}
		})
	}
}

// TestHandler_cleanupLoop checks that the cleanup goroutine removes expired
// entries on its own and stops when the handler closes. Stats does not touch
// expired entries, so the size reaching zero proves the loop ran.
func TestHandler_cleanupLoop(t *testing.T) {
	config := DefaultConfig()
	config.CleanupInterval = time.Millisecond

	h, err := NewHandler(newMockEmbedder(), newMockSearcher(), config)
	require.NoError(t, err)

	cache := h.(*handler).cache
	cache.Set("expired", &Response{Query: "expired"}, -time.Minute)
	require.Equal(t, 1, cache.Stats().Size)

	require.Eventually(t, func() bool { return cache.Stats().Size == 0 },
		5*time.Second, time.Millisecond, "cleanup loop never removed the expired entry")

	// Close waits for the loop to return.
	assert.NoError(t, h.Close())
}

// TestHandler_cleanupLoop_WithoutCache tests cleanup loop when cache is disabled.
func TestHandler_cleanupLoop_WithoutCache(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig().WithoutCache()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)

	// Close should not panic even though cleanup wasn't started
	err = h.Close()
	assert.NoError(t, err)
}

func TestParser_detectLanguage(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{name: "c as a word", query: "show me c code", want: "c"},
		{name: "c at end", query: "how to use c", want: "c"},
		{name: "c inside code does not match", query: "show me code example", want: ""},
		{name: "c inside authentication does not match", query: "authentication function", want: ""},
		{name: "cpp variant", query: "show me cpp code", want: "c++"},
		{name: "javascript", query: "javascript function", want: "javascript"},
		{name: "java not javascript", query: "show me java class", want: "java"},
		{name: "javascript wins over java", query: "javascript java interop", want: "javascript"},
		{name: "js shorthand", query: "show me js code", want: "javascript"},
		{name: "node means javascript", query: "show me node server code", want: "javascript"},
		{name: "python", query: "python implementation", want: "python"},
		{name: "py shorthand", query: "show me py code", want: "python"},
		{name: "py inside copy does not match", query: "copy the file", want: ""},
		{name: "rust", query: "show me rust implementation", want: "rust"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, detectLanguage(tt.query))
		})
	}
}

func TestHandler_Query(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		results     []Result
		wantErr     error
		wantContext string
	}{
		{name: "empty query", query: "", wantErr: ErrEmptyQuery},
		{name: "no results", query: "test query", results: []Result{}, wantContext: "No relevant results found."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searcher := newMockSearcher()
			if tt.results != nil {
				searcher.setResults(tt.results)
			}

			h, err := NewHandler(newMockEmbedder(), searcher, DefaultConfig())
			require.NoError(t, err)
			defer h.Close()

			response, err := h.Query(context.Background(), tt.query, DefaultOptions())
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, response)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, response.Results)
			assert.Equal(t, tt.wantContext, response.Context)
		})
	}
}
