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
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock embedding provider
type mockEmbedder struct {
	mu      sync.Mutex
	calls   int
	err     error
	latency time.Duration
	vectors map[string][]float32
}

func newMockEmbedder() *mockEmbedder {
	return &mockEmbedder{
		vectors: make(map[string][]float32),
	}
}
func (m *mockEmbedder) Encode(ctx context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	m.calls++
	latency := m.latency
	err := m.err
	m.mu.Unlock()

	// Check context first
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if latency > 0 {
		select {
		case <-time.After(latency):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, err
	}

	// Return cached vector or generate simple one
	m.mu.Lock()
	vec, ok := m.vectors[text]
	m.mu.Unlock()

	if ok {
		return vec, nil
	}

	// Generate simple vector based on text length
	newVec := make([]float32, 128)
	for i := range newVec {
		newVec[i] = float32(len(text)) / 1000.0
	}
	return newVec, nil
}

func (m *mockEmbedder) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *mockEmbedder) setLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latency = d
}

func (m *mockEmbedder) setCalls(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = n
}

func (m *mockEmbedder) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// Mock vector searcher
type mockSearcher struct {
	mu      sync.Mutex
	calls   int
	results []Result
	err     error
	latency time.Duration
}

func newMockSearcher() *mockSearcher {
	return &mockSearcher{
		results: []Result{
			{
				ID:      "1",
				Score:   0.95,
				Content: "Sample result 1",
				Metadata: map[string]interface{}{
					"source": "test1.go",
				},
			},
			{
				ID:      "2",
				Score:   0.85,
				Content: "Sample result 2",
				Metadata: map[string]interface{}{
					"source": "test2.go",
				},
			},
		},
	}
}

func (m *mockSearcher) Search(ctx context.Context, vector []float32, opts SearchOptions) ([]Result, error) {
	m.mu.Lock()
	m.calls++
	latency := m.latency
	err := m.err
	results := m.results
	m.mu.Unlock()

	// Check context first
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if latency > 0 {
		select {
		case <-time.After(latency):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, err
	}

	// Return up to TopK results
	n := opts.TopK
	if n > len(results) {
		n = len(results)
	}

	return results[:n], nil
}
func (m *mockSearcher) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *mockSearcher) setResults(results []Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results = results
}

func (m *mockSearcher) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestNewHandler_Success(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	assert.NotNil(t, h)

	err = h.Close()
	assert.NoError(t, err)
}

func TestNewHandler_NilEmbedder(t *testing.T) {
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(nil, searcher, config)
	assert.Error(t, err)
	assert.Nil(t, h)
	assert.Contains(t, err.Error(), "embedder")
}

func TestNewHandler_NilSearcher(t *testing.T) {
	embedder := newMockEmbedder()
	config := DefaultConfig()

	h, err := NewHandler(embedder, nil, config)
	assert.Error(t, err)
	assert.Nil(t, h)
	assert.Contains(t, err.Error(), "searcher")
}

func TestNewHandler_InvalidConfig(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()
	config.CacheSize = -1

	h, err := NewHandler(embedder, searcher, config)
	assert.Error(t, err)
	assert.Nil(t, h)
}

func TestHandler_Query_Success(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	response, err := h.Query(ctx, "test query", DefaultOptions())

	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "test query", response.Query)
	assert.Len(t, response.Results, 2)
	assert.NotEmpty(t, response.Context)
	assert.Greater(t, response.ProcessingTime, time.Duration(0))
	assert.False(t, response.CacheHit)
}

func TestHandler_Query_EmptyQuery(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	response, err := h.Query(ctx, "", DefaultOptions())

	assert.Equal(t, ErrEmptyQuery, err)
	assert.Nil(t, response)
}

func TestHandler_Query_EmbeddingError(t *testing.T) {
	embedder := newMockEmbedder()
	embedder.setError(errors.New("embedding failed"))
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	response, err := h.Query(ctx, "test query", DefaultOptions())

	assert.Error(t, err)
	assert.Nil(t, response)
	assert.ErrorIs(t, err, ErrEmbeddingFailed)
}

func TestHandler_Query_SearchError(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	searcher.setError(errors.New("search failed"))
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	response, err := h.Query(ctx, "test query", DefaultOptions())

	assert.Error(t, err)
	assert.Nil(t, response)
	assert.ErrorIs(t, err, ErrSearchFailed)
}

func TestHandler_Query_CacheHit(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	opts := DefaultOptions()

	// First query - cache miss
	response1, err := h.Query(ctx, "test query", opts)
	require.NoError(t, err)
	assert.False(t, response1.CacheHit)

	// Reset call counts
	embedder.setCalls(0)

	// Second query - should hit cache
	response2, err := h.Query(ctx, "test query", opts)
	require.NoError(t, err)
	assert.True(t, response2.CacheHit)

	// Embedder should not be called again
	assert.Equal(t, 0, embedder.getCalls())
}

func TestHandler_Query_CacheDisabled(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig().WithoutCache()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	opts := DefaultOptions()

	// First query
	response1, err := h.Query(ctx, "test query", opts)
	require.NoError(t, err)
	assert.False(t, response1.CacheHit)

	// Second query - should not hit cache
	response2, err := h.Query(ctx, "test query", opts)
	require.NoError(t, err)
	assert.False(t, response2.CacheHit)
}

func TestHandler_Query_DifferentOptions(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()

	// Query with different TopK values
	opts1 := DefaultOptions()
	opts1.TopK = 5

	opts2 := DefaultOptions()
	opts2.TopK = 10

	response1, err := h.Query(ctx, "test query", opts1)
	require.NoError(t, err)

	response2, err := h.Query(ctx, "test query", opts2)
	require.NoError(t, err)

	// Both should be cache misses (different options)
	assert.False(t, response1.CacheHit)
	assert.False(t, response2.CacheHit)
}

func TestHandler_Query_ContextCanceled(t *testing.T) {
	embedder := newMockEmbedder()
	embedder.setLatency(100 * time.Millisecond)
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	response, err := h.Query(ctx, "test query", DefaultOptions())

	assert.Error(t, err)
	assert.Nil(t, response)
}

func TestHandler_Query_Timeout(t *testing.T) {
	embedder := newMockEmbedder()
	embedder.setLatency(200 * time.Millisecond)
	searcher := newMockSearcher()
	config := DefaultConfig()
	config.QueryTimeout = 50 * time.Millisecond

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	response, err := h.Query(ctx, "test query", DefaultOptions())

	assert.Error(t, err)
	assert.Nil(t, response)
}

func TestHandler_Query_ApplyDefaults(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()
	config.DefaultTopK = 15
	config.DefaultMinScore = 0.8
	config.DefaultNProbe = 25

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()

	// Query with empty options - should use defaults
	response, err := h.Query(ctx, "test query", Options{})
	require.NoError(t, err)
	assert.NotNil(t, response)
}

func TestHandler_StreamQuery_Success(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)
	require.NotNil(t, ch)

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// Should have results + done chunk
	assert.GreaterOrEqual(t, len(chunks), 2)

	// Last chunk should be done
	lastChunk := chunks[len(chunks)-1]
	assert.True(t, lastChunk.Done)
}
func TestHandler_StreamQuery_Cancellation(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()

	// Create many results
	manyResults := make([]Result, 100)
	for i := range manyResults {
		manyResults[i] = Result{
			ID:    fmt.Sprintf("id-%d", i),
			Score: 0.9,
		}
	}
	searcher.setResults(manyResults)

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)

	// Read a few chunks then cancel
	chunksRead := 0
	for chunk := range ch {
		if chunk.Done || chunk.Error != nil {
			break
		}
		chunksRead++
		if chunksRead >= 2 {
			cancel()
		}
	}

	// Should have read at least some chunks
	assert.GreaterOrEqual(t, chunksRead, 1)
}

func TestHandler_Close(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)

	err = h.Close()
	assert.NoError(t, err)

	// Second close should be safe
	err = h.Close()
	assert.NoError(t, err)
}

func TestHandler_ClosedHandler(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)

	err = h.Close()
	require.NoError(t, err)

	ctx := context.Background()

	// Query after close
	response, err := h.Query(ctx, "test query", DefaultOptions())
	assert.Equal(t, ErrClosed, err)
	assert.Nil(t, response)

	// StreamQuery after close
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	assert.Equal(t, ErrClosed, err)
	assert.Nil(t, ch)
}

func TestHandler_Concurrent(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	const numGoroutines = 20
	const numQueries = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines*numQueries)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			ctx := context.Background()
			for j := 0; j < numQueries; j++ {
				query := fmt.Sprintf("query-%d-%d", id, j)
				_, err := h.Query(ctx, query, DefaultOptions())
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Should have no errors
	for err := range errors {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandler_IncludeSource(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()

	// With source
	opts1 := DefaultOptions()
	opts1.IncludeSource = true
	response1, err := h.Query(ctx, "test query", opts1)
	require.NoError(t, err)
	assert.Contains(t, response1.Context, "Source:")

	// Without source
	opts2 := DefaultOptions()
	opts2.IncludeSource = false
	response2, err := h.Query(ctx, "test query 2", opts2)
	require.NoError(t, err)
	assert.NotContains(t, response2.Context, "Source:")
}

func BenchmarkHandler_Query(b *testing.B) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(b, err)
	defer h.Close()

	ctx := context.Background()
	opts := DefaultOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.Query(ctx, "test query", opts)
	}
}

func BenchmarkHandler_QueryCached(b *testing.B) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(b, err)
	defer h.Close()

	ctx := context.Background()
	opts := DefaultOptions()

	// Warm up cache
	_, _ = h.Query(ctx, "test query", opts)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.Query(ctx, "test query", opts)
	}
}

func TestHandler_StreamQuery_ContextCancelDuringStream(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()

	// Create several results to stream
	manyResults := make([]Result, 10)
	for i := range manyResults {
		manyResults[i] = Result{
			ID:      string(rune('A' + i)),
			Score:   0.9,
			Content: "content",
		}
	}
	searcher.setResults(manyResults)

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)

	// Read slowly to allow timeout
	receivedResults := 0
	receivedError := false

	for chunk := range ch {
		if chunk.Result != nil {
			receivedResults++
			time.Sleep(10 * time.Millisecond) // Slow read to trigger timeout
		}
		if chunk.Error != nil {
			receivedError = true
		}
	}

	// Should have either completed or received error
	assert.True(t, receivedResults > 0 || receivedError)
}

// TestHandler_StreamQuery_SlowConsumer tests streaming with slow consumer.
func TestHandler_StreamQuery_SlowConsumer(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)

	// Consume slowly
	count := 0
	for chunk := range ch {
		if chunk.Result != nil {
			count++
		}
		time.Sleep(1 * time.Millisecond)
	}

	assert.Equal(t, 2, count)
}

// TestHandler_Query_ContextTimeout tests query with context timeout.
func TestHandler_Query_ContextTimeout(t *testing.T) {
	embedder := newMockEmbedder()
	embedder.setLatency(200 * time.Millisecond)
	searcher := newMockSearcher()

	config := DefaultConfig()
	config.QueryTimeout = 10 * time.Millisecond // Very short timeout

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	_, err = h.Query(ctx, "test query", DefaultOptions())

	// Should timeout
	assert.Error(t, err)
}

// TestHandler_Query_MultipleFilters tests query with multiple filters.
func TestHandler_Query_MultipleFilters(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()

	opts := DefaultOptions()
	opts.Filters = map[string]interface{}{
		"key1": "value1",
		"key2": 123,
		"key3": true,
		"key4": []string{"a", "b", "c"},
	}

	response, err := h.Query(ctx, "test query", opts)
	require.NoError(t, err)
	assert.NotNil(t, response)
}

func TestHandler_Query_AllOptions(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()

	// Specify all options
	opts := Options{
		TopK:          3,
		MinScore:      0.7,
		NProbe:        10,
		IncludeSource: true,
		Filters: map[string]interface{}{
			"type": "code",
		},
	}

	response, err := h.Query(ctx, "test query with all options", opts)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.NotEmpty(t, response.Results)
}

// TestHandler_StreamQuery_AllResultsStreamed tests that all results are streamed.
func TestHandler_StreamQuery_AllResultsStreamed(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)

	resultCount := 0
	doneReceived := false

	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		if chunk.Result != nil {
			resultCount++
		}
		if chunk.Done {
			doneReceived = true
		}
	}

	assert.Equal(t, 2, resultCount, "should receive all results")
	assert.True(t, doneReceived, "should receive done signal")
}

// TestHandler_StreamQuery_NoResults tests streaming with no results.
func TestHandler_StreamQuery_NoResults(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	searcher.setResults([]Result{}) // No results

	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()
	ch, err := h.StreamQuery(ctx, "test query", DefaultOptions())
	require.NoError(t, err)

	chunks := []StreamChunk{}
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// Should only have done chunk (no results to stream)
	assert.Len(t, chunks, 1)
	assert.True(t, chunks[0].Done)
}

// TestHandler_Query_WithFilters tests Query with filters.
func TestHandler_Query_WithFilters(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()
	config := DefaultConfig()

	h, err := NewHandler(embedder, searcher, config)
	require.NoError(t, err)
	defer h.Close()

	ctx := context.Background()

	opts := DefaultOptions()
	opts.Filters = map[string]interface{}{
		"category": "documentation",
		"language": "go",
	}

	response, err := h.Query(ctx, "test query with filters", opts)
	require.NoError(t, err)
	assert.NotNil(t, response)
}
