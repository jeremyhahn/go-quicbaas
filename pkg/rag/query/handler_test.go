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
	"runtime"
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

func TestNewHandler(t *testing.T) {
	invalidConfig := DefaultConfig()
	invalidConfig.CacheSize = -1

	tests := []struct {
		name     string
		embedder EmbeddingProvider
		searcher VectorSearcher
		config   Config
		wantErr  error
	}{
		{name: "valid", embedder: newMockEmbedder(), searcher: newMockSearcher(), config: DefaultConfig()},
		{name: "nil embedder", searcher: newMockSearcher(), config: DefaultConfig(), wantErr: ErrNilEmbedder},
		{name: "nil searcher", embedder: newMockEmbedder(), config: DefaultConfig(), wantErr: ErrNilSearcher},
		{name: "invalid config", embedder: newMockEmbedder(), searcher: newMockSearcher(), config: invalidConfig, wantErr: ErrInvalidOptions},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := NewHandler(tt.embedder, tt.searcher, tt.config)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, h)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, h)
			assert.NoError(t, h.Close())
		})
	}
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

func TestHandler_Query_Errors(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name         string
		query        string
		ctx          context.Context
		embedErr     error
		searchErr    error
		embedLatency time.Duration
		queryTimeout time.Duration
		wantErr      error
	}{
		{name: "empty query", query: "", ctx: context.Background(), wantErr: ErrEmptyQuery},
		{name: "embedding error", query: "test query", ctx: context.Background(), embedErr: errors.New("embedding failed"), wantErr: ErrEmbeddingFailed},
		{name: "search error", query: "test query", ctx: context.Background(), searchErr: errors.New("search failed"), wantErr: ErrSearchFailed},
		{name: "context canceled", query: "test query", ctx: canceled, embedLatency: 100 * time.Millisecond},
		{name: "query timeout", query: "test query", ctx: context.Background(), embedLatency: 200 * time.Millisecond, queryTimeout: 50 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder := newMockEmbedder()
			embedder.setError(tt.embedErr)
			embedder.setLatency(tt.embedLatency)
			searcher := newMockSearcher()
			searcher.setError(tt.searchErr)
			config := DefaultConfig()
			if tt.queryTimeout > 0 {
				config.QueryTimeout = tt.queryTimeout
			}

			h, err := NewHandler(embedder, searcher, config)
			require.NoError(t, err)
			defer h.Close()

			response, err := h.Query(tt.ctx, tt.query, DefaultOptions())

			require.Error(t, err)
			assert.Nil(t, response)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
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

	topK5 := DefaultOptions()
	topK5.TopK = 5

	topK10 := DefaultOptions()
	topK10.TopK = 10

	filters := DefaultOptions()
	filters.Filters = map[string]interface{}{
		"key1": "value1",
		"key2": 123,
		"key3": true,
		"key4": []string{"a", "b", "c"},
	}

	allOptions := Options{
		TopK:          3,
		MinScore:      0.7,
		NProbe:        10,
		IncludeSource: true,
		Filters: map[string]interface{}{
			"type": "code",
		},
	}

	// The same query with each set of options in turn: every one is a
	// cache miss, because the options are part of the cache key.
	tests := []struct {
		name string
		opts Options
	}{
		{name: "top k 5", opts: topK5},
		{name: "top k 10", opts: topK10},
		{name: "mixed-type filters", opts: filters},
		{name: "all options", opts: allOptions},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := h.Query(ctx, "test query", tt.opts)
			require.NoError(t, err)
			assert.False(t, response.CacheHit)
			assert.NotEmpty(t, response.Results)
		})
	}
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
	tests := []struct {
		name        string
		results     []Result
		wantResults int
	}{
		{name: "default results", wantResults: 2},
		{name: "no results", results: []Result{}, wantResults: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder := newMockEmbedder()
			searcher := newMockSearcher()
			if tt.results != nil {
				searcher.setResults(tt.results)
			}

			h, err := NewHandler(embedder, searcher, DefaultConfig())
			require.NoError(t, err)
			defer h.Close()

			ch, err := h.StreamQuery(context.Background(), "test query", DefaultOptions())
			require.NoError(t, err)
			require.NotNil(t, ch)

			var chunks []StreamChunk
			for chunk := range ch {
				require.NoError(t, chunk.Error)
				chunks = append(chunks, chunk)
			}

			// One chunk per result, then exactly one done chunk.
			require.Len(t, chunks, tt.wantResults+1)
			for _, chunk := range chunks[:tt.wantResults] {
				assert.NotNil(t, chunk.Result)
				assert.False(t, chunk.Done)
			}
			assert.True(t, chunks[tt.wantResults].Done)
		})
	}
}

func TestHandler_StreamQuery_Cancellation(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()

	// Many more results than the stream buffer holds, so the stream is
	// still sending when the context is canceled.
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
	opts := DefaultOptions()
	opts.TopK = len(manyResults)
	ch, err := h.StreamQuery(ctx, "test query", opts)
	require.NoError(t, err)

	// Read two results, cancel, then drain the stream.
	chunksRead := 0
	var last StreamChunk
	for chunk := range ch {
		last = chunk
		if chunk.Result != nil {
			chunksRead++
		}
		if chunksRead == 2 {
			cancel()
		}
	}

	assert.Less(t, chunksRead, len(manyResults))
	assert.False(t, last.Done)
	assert.ErrorIs(t, last.Error, ErrContextCanceled)
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

// TestHandler_StreamQuery_SlowConsumer tests that a consumer that does not
// read until the stream buffer is full still receives every result.
func TestHandler_StreamQuery_SlowConsumer(t *testing.T) {
	embedder := newMockEmbedder()
	searcher := newMockSearcher()

	manyResults := make([]Result, 25)
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

	opts := DefaultOptions()
	opts.TopK = len(manyResults)
	ch, err := h.StreamQuery(context.Background(), "test query", opts)
	require.NoError(t, err)

	// Hold off reading until the producer has filled the buffer and blocks.
	for len(ch) < cap(ch) {
		runtime.Gosched()
	}

	count := 0
	done := false
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		if chunk.Result != nil {
			count++
		}
		if chunk.Done {
			done = true
		}
	}

	assert.Equal(t, len(manyResults), count)
	assert.True(t, done)
}
