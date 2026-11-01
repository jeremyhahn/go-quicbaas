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

func TestHandler_StreamQuery_Error(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()

	// Set embedder to fail
	embedder.setError(errors.New("embedding failed"))

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Should receive error chunk
	chunk := <-ch
	assert.Error(t, chunk.Error)
	assert.ErrorIs(t, chunk.Error, ErrEmbeddingFailed)

	// Channel should be closed
	_, ok := <-ch
	assert.False(t, ok)
}

// TestHandler_buildContext_EmptyResults tests buildContext with no results.
func TestHandler_buildContext_EmptyResults(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	// Cast to concrete type to access buildContext
	handler := h.(*handler)

	// Test with empty results
	context := handler.buildContext([]Result{}, DefaultOptions())
	assert.Equal(t, "No relevant results found.", context)
}

// TestHandler_buildContext_NoSourceMetadata tests buildContext with results missing source metadata.
func TestHandler_buildContext_NoSourceMetadata(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	handler := h.(*handler)

	// Create results without source metadata
	results := []Result{
		{
			ID:      "1",
			Score:   0.95,
			Content: "Test content",
			Metadata: map[string]interface{}{
				"other": "value",
			},
		},
	}

	opts := DefaultOptions()
	opts.IncludeSource = true

	context := handler.buildContext(results, opts)
	assert.Contains(t, context, "Test content")
	assert.NotContains(t, context, "Source:")
}

// TestHandler_buildContext_NonStringSource tests buildContext with non-string source metadata.
func TestHandler_buildContext_NonStringSource(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	handler := h.(*handler)

	// Create results with non-string source
	results := []Result{
		{
			ID:      "1",
			Score:   0.95,
			Content: "Test content",
			Metadata: map[string]interface{}{
				"source": 123, // Non-string source
			},
		},
	}

	opts := DefaultOptions()
	opts.IncludeSource = true

	context := handler.buildContext(results, opts)
	assert.Contains(t, context, "Test content")
	assert.NotContains(t, context, "Source:")
}

// TestHandler_cleanupLoop tests the cleanup loop goroutine.
func TestHandler_cleanupLoop(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()
	config.CleanupInterval = 10 * time.Millisecond // Short interval for testing

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)

	// Let cleanup run a few times
	time.Sleep(50 * time.Millisecond)

	// Close handler - should stop cleanup loop
	err = h.Close()
	assert.NoError(t, err)

	// Give time for cleanup to stop
	time.Sleep(20 * time.Millisecond)
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

// TestParser_detectLanguage_CSharp tests C language detection.
func TestParser_detectLanguage_CSharp(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "c language standalone",
			query: "show me c code",
			want:  "c",
		},
		{
			name:  "c in middle",
			query: "implement in c language",
			want:  "c",
		},
		{
			name:  "c at end",
			query: "how to use c",
			want:  "c",
		},
		{
			name:  "c in code - should not match",
			query: "show me code example",
			want:  "",
		},
		{
			name:  "authentication - should not match c",
			query: "authentication function",
			want:  "",
		},
		{
			name:  "cpp variant",
			query: "show me cpp code",
			want:  "c++",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLanguage(tt.query)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParser_detectLanguage_JavaScriptEdgeCase tests JavaScript vs Java detection.
func TestParser_detectLanguage_JavaScriptEdgeCase(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "javascript",
			query: "javascript function",
			want:  "javascript",
		},
		{
			name:  "java not javascript",
			query: "show me java class",
			want:  "java",
		},
		{
			name:  "java in javascript - should detect javascript",
			query: "javascript java interop",
			want:  "javascript",
		},
		{
			name:  "js shorthand",
			query: "show me js code",
			want:  "javascript",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLanguage(tt.query)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParser_detectLanguage_Python tests Python detection with py shorthand.
func TestParser_detectLanguage_Python(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "python full",
			query: "python implementation",
			want:  "python",
		},
		{
			name:  "py shorthand",
			query: "show me py code",
			want:  "python",
		},
		{
			name:  "py in word should not match",
			query: "copy the file",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLanguage(tt.query)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestHandler_Query_ParseError tests query parsing error handling.
func TestHandler_Query_ParseError(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()

	// Empty query should fail parsing
	response, err := h.Query(ctx, "", DefaultOptions())
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Equal(t, ErrEmptyQuery, err)
}

// TestHandler_buildContext_MultipleResults tests buildContext with multiple results.
func TestHandler_buildContext_MultipleResults(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	handler := h.(*handler)

	results := []Result{
		{
			ID:      "1",
			Score:   0.95,
			Content: "First result",
			Metadata: map[string]interface{}{
				"source": "file1.go",
			},
		},
		{
			ID:      "2",
			Score:   0.85,
			Content: "Second result",
			Metadata: map[string]interface{}{
				"source": "file2.go",
			},
		},
		{
			ID:      "3",
			Score:   0.75,
			Content: "Third result",
			Metadata: map[string]interface{}{
				"source": "file3.go",
			},
		},
	}

	opts := DefaultOptions()
	opts.IncludeSource = true

	context := handler.buildContext(results, opts)

	// Should contain all results
	assert.Contains(t, context, "First result")
	assert.Contains(t, context, "Second result")
	assert.Contains(t, context, "Third result")

	// Should contain scores
	assert.Contains(t, context, "0.95")
	assert.Contains(t, context, "0.85")
	assert.Contains(t, context, "0.75")

	// Should contain sources
	assert.Contains(t, context, "file1.go")
	assert.Contains(t, context, "file2.go")
	assert.Contains(t, context, "file3.go")

	// Should have proper separation
	assert.Contains(t, context, "\n\n")
}

// TestHandler_cleanupLoop_CacheExpiry tests that cleanup loop actually cleans cache.
func TestHandler_cleanupLoop_CacheExpiry(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()
	config.CacheTTL = 50 * time.Millisecond        // Short TTL
	config.CleanupInterval = 25 * time.Millisecond // Frequent cleanup

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()

	// Add item to cache
	_, err = h.Query(ctx, "test query", DefaultOptions())
	require.NoError(t, err)

	// Verify it's cached
	response2, err := h.Query(ctx, "test query", DefaultOptions())
	require.NoError(t, err)
	assert.True(t, response2.CacheHit)

	// Wait for TTL to expire and cleanup to run
	time.Sleep(100 * time.Millisecond)

	// Query again - should be cache miss now
	response3, err := h.Query(ctx, "test query", DefaultOptions())
	require.NoError(t, err)
	assert.False(t, response3.CacheHit)
}

// TestHandler_StreamQuery_EmptyQuery tests streaming with empty query.
func TestHandler_StreamQuery_EmptyQuery(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	ch, err := h.StreamQuery(ctx, "", DefaultOptions())
	require.NoError(t, err)

	// Should get error chunk
	chunk := <-ch
	assert.Error(t, chunk.Error)
	assert.Equal(t, ErrEmptyQuery, chunk.Error)
}

// TestHandler_StreamQuery_SearchError tests streaming with search error.
func TestHandler_StreamQuery_SearchError(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	searcher.setError(errors.New("search failed"))

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)

	// Should get error chunk
	chunk := <-ch
	assert.Error(t, chunk.Error)
	assert.ErrorIs(t, chunk.Error, ErrSearchFailed)
}

// TestHandler_StreamQuery_DoneChunk tests that stream sends done chunk.
func TestHandler_StreamQuery_DoneChunk(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)

	// Drain all chunks
	var lastChunk StreamChunk
	for chunk := range ch {
		lastChunk = chunk
	}

	// Last chunk should be done
	assert.True(t, lastChunk.Done)
	assert.Nil(t, lastChunk.Error)
	assert.Nil(t, lastChunk.Result)
}

// TestParser_detectLanguage_Node tests Node.js detection.
func TestParser_detectLanguage_Node(t *testing.T) {
	query := "show me node server code"
	got := detectLanguage(query)
	assert.Equal(t, "javascript", got)
}

// TestParser_detectLanguage_Rust tests Rust detection.
func TestParser_detectLanguage_Rust(t *testing.T) {
	query := "show me rust implementation"
	got := detectLanguage(query)
	assert.Equal(t, "rust", got)
}

// TestHandler_Query_NoResults tests query with no search results.
func TestHandler_Query_NoResults(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	searcher.setResults([]Result{}) // No results

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	response, err := h.Query(ctx, "test query", DefaultOptions())

	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Len(t, response.Results, 0)
	assert.Equal(t, "No relevant results found.", response.Context)
}
