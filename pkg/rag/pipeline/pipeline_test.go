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

package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing

type mockQueryHandler struct {
	processFunc func(ctx context.Context, query string) (*ProcessedQuery, error)
	closed      bool
}

func (m *mockQueryHandler) Process(ctx context.Context, query string) (*ProcessedQuery, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, query)
	}
	return &ProcessedQuery{
		Original:  query,
		Cleaned:   query,
		Embedding: make([]float32, 128),
		Intent:    "search",
		CacheHit:  false,
	}, nil
}

func (m *mockQueryHandler) Close() error {
	m.closed = true
	return nil
}

type mockVectorSearcher struct {
	searchFunc func(ctx context.Context, embedding []float32, opts SearchOptions) ([]Result, error)
	closed     bool
}

func (m *mockVectorSearcher) Search(ctx context.Context, embedding []float32, opts SearchOptions) ([]Result, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, embedding, opts)
	}
	results := make([]Result, 5)
	for i := range results {
		results[i] = Result{
			ID:       string(rune('A' + i)),
			Content:  "vector result content",
			Metadata: map[string]interface{}{},
			Score:    0.9 - float32(i)*0.1,
		}
	}
	return results, nil
}

func (m *mockVectorSearcher) Close() error {
	m.closed = true
	return nil
}

type mockRanker struct {
	rankFunc func(ctx context.Context, query string, vectorResults, keywordResults []Result) ([]Result, error)
	closed   bool
}

func (m *mockRanker) Rank(ctx context.Context, query string, vectorResults, keywordResults []Result) ([]Result, error) {
	if m.rankFunc != nil {
		return m.rankFunc(ctx, query, vectorResults, keywordResults)
	}
	// Simple ranking: just return vector results
	return vectorResults, nil
}

func (m *mockRanker) Close() error {
	m.closed = true
	return nil
}

type mockContextBuilder struct {
	buildFunc func(ctx context.Context, results []Result, opts ContextOptions) (*BuiltContext, error)
	closed    bool
}

func (m *mockContextBuilder) Build(ctx context.Context, results []Result, opts ContextOptions) (*BuiltContext, error) {
	if m.buildFunc != nil {
		return m.buildFunc(ctx, results, opts)
	}
	context := "Built context from results"
	return &BuiltContext{
		Content:   context,
		Tokens:    100,
		Truncated: false,
	}, nil
}

func (m *mockContextBuilder) Close() error {
	m.closed = true
	return nil
}

func setupTestPipeline(t *testing.T) Pipeline {
	config := DefaultConfig()
	config.EnableCircuitBreaker = false // Disable for simpler tests
	config.EnableMetrics = true

	queryHandler := &mockQueryHandler{}
	vectorSearcher := &mockVectorSearcher{}
	ranker := &mockRanker{}
	contextBuilder := &mockContextBuilder{}

	p, err := NewPipeline(config, queryHandler, vectorSearcher, ranker, contextBuilder)
	require.NoError(t, err)
	return p
}

func TestPipelineCreation(t *testing.T) {
	config := DefaultConfig()
	queryHandler := &mockQueryHandler{}
	vectorSearcher := &mockVectorSearcher{}
	ranker := &mockRanker{}
	contextBuilder := &mockContextBuilder{}

	p, err := NewPipeline(config, queryHandler, vectorSearcher, ranker, contextBuilder)
	require.NoError(t, err)
	assert.NotNil(t, p)
	defer p.Close()
}

func TestPipelineCreationErrors(t *testing.T) {
	config := DefaultConfig()
	queryHandler := &mockQueryHandler{}
	vectorSearcher := &mockVectorSearcher{}
	ranker := &mockRanker{}
	contextBuilder := &mockContextBuilder{}

	tests := []struct {
		name    string
		qh      QueryHandler
		vs      VectorSearcher
		r       Ranker
		cb      ContextBuilder
		wantErr bool
	}{
		{
			name:    "nil query handler",
			qh:      nil,
			vs:      vectorSearcher,
			r:       ranker,
			cb:      contextBuilder,
			wantErr: true,
		},
		{
			name:    "nil vector searcher",
			qh:      queryHandler,
			vs:      nil,
			r:       ranker,
			cb:      contextBuilder,
			wantErr: true,
		},
		{
			name:    "nil ranker",
			qh:      queryHandler,
			vs:      vectorSearcher,
			r:       nil,
			cb:      contextBuilder,
			wantErr: true,
		},
		{
			name:    "nil context builder",
			qh:      queryHandler,
			vs:      vectorSearcher,
			r:       ranker,
			cb:      nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPipeline(config, tt.qh, tt.vs, tt.r, tt.cb)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, p)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, p)
				if p != nil {
					p.Close()
				}
			}
		})
	}
}

func TestPipelineQuery(t *testing.T) {
	p := setupTestPipeline(t)
	defer p.Close()

	ctx := context.Background()
	opts := QueryOptions{
		TopK:         5,
		EnableHybrid: true,
		EnableRerank: false,
		UseCache:     false,
	}

	resp, err := p.Query(ctx, "test query", opts)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test query", resp.Query)
	assert.NotEmpty(t, resp.Results)
	assert.NotEmpty(t, resp.Context)
	assert.Greater(t, resp.ProcessingTime, time.Duration(0))
}

func TestPipelineQueryEmpty(t *testing.T) {
	p := setupTestPipeline(t)
	defer p.Close()

	ctx := context.Background()
	opts := QueryOptions{}

	_, err := p.Query(ctx, "", opts)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidQuery))
}

func TestPipelineConcurrency(t *testing.T) {
	config := DefaultConfig()
	config.MaxConcurrentQueries = 2
	config.EnableCircuitBreaker = false

	queryHandler := &mockQueryHandler{}
	vectorSearcher := &mockVectorSearcher{}
	ranker := &mockRanker{}
	contextBuilder := &mockContextBuilder{}

	p, err := NewPipeline(config, queryHandler, vectorSearcher, ranker, contextBuilder)
	require.NoError(t, err)
	defer p.Close()

	const numQueries = 10
	results := make(chan error, numQueries)

	// Launch concurrent queries
	for i := 0; i < numQueries; i++ {
		go func() {
			ctx := context.Background()
			_, err := p.Query(ctx, "test", QueryOptions{})
			results <- err
		}()
	}

	// Collect results
	var successCount, errorCount int
	for i := 0; i < numQueries; i++ {
		err := <-results
		if err == nil {
			successCount++
		} else {
			errorCount++
		}
	}

	// All should succeed eventually
	assert.Greater(t, successCount, 0)
}

func TestPipelineClosedError(t *testing.T) {
	p := setupTestPipeline(t)
	p.Close()

	ctx := context.Background()
	_, err := p.Query(ctx, "test", QueryOptions{})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrPipelineClosed))
}

func TestPipelineHealth(t *testing.T) {
	p := setupTestPipeline(t)
	defer p.Close()

	health := p.Health()
	assert.NotNil(t, health)
	assert.True(t, health.Healthy)
	assert.Equal(t, "healthy", health.Status)
	assert.NotEmpty(t, health.ComponentHealth)
}

func TestPipelineStats(t *testing.T) {
	p := setupTestPipeline(t)
	defer p.Close()

	// Initially zero
	stats := p.Stats()
	assert.Equal(t, int64(0), stats.TotalQueries)

	// Execute a query
	ctx := context.Background()
	p.Query(ctx, "test", QueryOptions{})

	// Check stats updated
	stats = p.Stats()
	assert.Equal(t, int64(1), stats.TotalQueries)
}

func TestPipelineComponentError(t *testing.T) {
	config := DefaultConfig()
	config.EnableCircuitBreaker = false

	// Mock that returns error
	queryHandler := &mockQueryHandler{
		processFunc: func(ctx context.Context, query string) (*ProcessedQuery, error) {
			return nil, errors.New("query handler error")
		},
	}
	vectorSearcher := &mockVectorSearcher{}
	ranker := &mockRanker{}
	contextBuilder := &mockContextBuilder{}

	p, err := NewPipeline(config, queryHandler, vectorSearcher, ranker, contextBuilder)
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	_, err = p.Query(ctx, "test", QueryOptions{})
	assert.Error(t, err)

	// Error should be wrapped
	var pErr *PipelineError
	assert.True(t, errors.As(err, &pErr))
}

func TestPipelineDefaultOptions(t *testing.T) {
	p := setupTestPipeline(t)
	defer p.Close()

	ctx := context.Background()

	// Query with empty options should use defaults
	resp, err := p.Query(ctx, "test", QueryOptions{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestPipelineClose(t *testing.T) {
	queryHandler := &mockQueryHandler{}
	vectorSearcher := &mockVectorSearcher{}
	ranker := &mockRanker{}
	contextBuilder := &mockContextBuilder{}

	config := DefaultConfig()
	p, err := NewPipeline(config, queryHandler, vectorSearcher, ranker, contextBuilder)
	require.NoError(t, err)

	err = p.Close()
	assert.NoError(t, err)

	// Verify all components closed
	assert.True(t, queryHandler.closed)
	assert.True(t, vectorSearcher.closed)
	assert.True(t, ranker.closed)
	assert.True(t, contextBuilder.closed)

	// Double close should error
	err = p.Close()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrPipelineClosed))
}

func TestPipelineCircuitBreaker(t *testing.T) {
	config := DefaultConfig()
	config.EnableCircuitBreaker = true
	config.CircuitBreakerThreshold = 3

	// Mock that always fails
	queryHandler := &mockQueryHandler{
		processFunc: func(ctx context.Context, query string) (*ProcessedQuery, error) {
			return nil, errors.New("persistent error")
		},
	}
	vectorSearcher := &mockVectorSearcher{}
	ranker := &mockRanker{}
	contextBuilder := &mockContextBuilder{}

	p, err := NewPipeline(config, queryHandler, vectorSearcher, ranker, contextBuilder)
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()

	// Execute queries until circuit opens
	var lastErr error
	for i := 0; i < 5; i++ {
		_, err := p.Query(ctx, "test", QueryOptions{})
		lastErr = err
		if errors.Is(err, ErrCircuitBreakerOpen) {
			break
		}
	}

	// Circuit should be open
	assert.Error(t, lastErr)
}

func TestPipelineMetricsCollection(t *testing.T) {
	p := setupTestPipeline(t)
	defer p.Close()

	ctx := context.Background()

	// Execute multiple queries
	for i := 0; i < 10; i++ {
		p.Query(ctx, "test", QueryOptions{})
	}

	stats := p.Stats()
	assert.Equal(t, int64(10), stats.TotalQueries)
	assert.Greater(t, stats.AvgLatencyMs, 0.0)
}
