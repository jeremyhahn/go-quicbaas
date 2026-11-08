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

package rag

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/adapters"
	ragcontext "github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ranking"
)

func TestNewSystem_ValidConfig(t *testing.T) {
	cfg := createTestRAGConfig(t)

	system, err := NewSystem(cfg)

	require.NoError(t, err)
	require.NotNil(t, system)
	assert.NotNil(t, system.pipeline)
	assert.NotNil(t, system.vectorIndex)
	assert.NotNil(t, system.vectorSearcher)
	assert.NotNil(t, system.embedder)

	// Cleanup
	err = system.Close()
	assert.NoError(t, err)
}

func TestNewSystem_InvalidConfig(t *testing.T) {
	t.Run("empty provider", func(t *testing.T) {
		cfg := createTestRAGConfig(t)
		cfg.Embedding.Provider = ""

		_, err := NewSystem(cfg)

		assert.ErrorIs(t, err, ErrInvalidConfig)
		assert.Contains(t, err.Error(), "embedding provider")
	})

	t.Run("zero dimensions", func(t *testing.T) {
		cfg := createTestRAGConfig(t)
		cfg.Embedding.Dimensions = 0

		_, err := NewSystem(cfg)

		assert.Error(t, err)
	})

	t.Run("dimension mismatch", func(t *testing.T) {
		cfg := createTestRAGConfig(t)
		cfg.Embedding.Dimensions = 384
		cfg.Vector.Dimensions = 512

		_, err := NewSystem(cfg)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dimension mismatch")
	})
}

func TestSystem_Query(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	// Index some test documents first
	indexTestDocuments(t, system)

	t.Run("successful query", func(t *testing.T) {
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.0,
		}

		response, err := system.Query(context.Background(), "test query", opts)

		require.NoError(t, err)
		assert.NotNil(t, response)
		assert.Equal(t, "test query", response.Query)
		// Results may be empty if nothing matches
	})

	t.Run("query with filters", func(t *testing.T) {
		opts := pipeline.QueryOptions{
			TopK: 5,
			Filters: map[string]interface{}{
				"language": "go",
			},
		}

		response, err := system.Query(context.Background(), "function", opts)

		// May return error if no results match the filter, which is valid behavior
		if err != nil {
			assert.Contains(t, err.Error(), "no results")
		} else {
			assert.NotNil(t, response)
		}
	})
}

func TestSystem_IndexDocument(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	t.Run("index simple content", func(t *testing.T) {
		content := "This is test content for indexing"

		err := system.IndexContent(context.Background(), "test-doc-1", content, map[string]interface{}{
			"source": "test",
		})

		require.NoError(t, err)

		// Verify it's searchable
		response, err := system.Query(context.Background(), "test content", pipeline.QueryOptions{TopK: 1})
		require.NoError(t, err)

		if len(response.Results) > 0 {
			assert.Equal(t, "test-doc-1", response.Results[0].ID)
		}
	})
}

func TestSystem_Stats(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	stats := system.Stats()

	assert.NotNil(t, stats)
	// Stats should be valid even with no queries
	assert.GreaterOrEqual(t, stats.TotalQueries, int64(0))
}

func TestSystem_Close(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	err = system.Close()

	assert.NoError(t, err)

	// Calling Close() again should be safe
	err = system.Close()
	assert.NoError(t, err)
}

func TestSystem_ConcurrentQueries(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	// Index test data
	indexTestDocuments(t, system)

	// Run concurrent queries
	concurrency := 10
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(id int) {
			opts := pipeline.QueryOptions{TopK: 3}
			_, err := system.Query(context.Background(), "test query", opts)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all queries
	for i := 0; i < concurrency; i++ {
		<-done
	}
}

// Helper functions

func createTestRAGConfig(t *testing.T) config.RAGConfig {
	t.Helper()

	return config.RAGConfig{
		Enabled:   true,
		IndexPath: t.TempDir() + "/test_index",
		Embedding: config.EmbeddingConfig{
			Provider:   "mock",
			Model:      "test-model",
			APIURL:     "http://localhost:11434",
			Dimensions: 384,
			Timeout:    30,
		},
		Vector: config.RAGVectorConfig{
			NumCentroids: 10,
			Dimensions:   384,
			NProbe:       3,
			Metric:       "cosine",
		},
		Query: config.RAGQueryConfig{
			CacheSize:       100,
			CacheTTL:        time.Hour,
			CleanupInterval: 5 * time.Minute,
			DefaultTopK:     10,
			DefaultMinScore: 0.7,
			DefaultNProbe:   3,
		},
		Ranking: config.RankingConfig{
			Strategy:      "hybrid",
			BM25K1:        1.2,
			BM25B:         0.75,
			RRFK:          60,
			EnableRerank:  false,
			RerankTopK:    50,
			VectorWeight:  0.7,
			KeywordWeight: 0.3,
		},
		Context: config.ContextConfig{
			MaxTokens:          4000,
			DefaultTemplate:    "markdown",
			IncludeCitations:   true,
			TruncationStrategy: "smart",
			MaxResultsPerChunk: 20,
			TokenMultiplier:    1.3,
		},
		Pipeline: config.PipelineConfig{
			Timeout:                 30 * time.Second,
			MaxConcurrentQueries:    100,
			EnableCircuitBreaker:    false, // Disable for tests
			CircuitBreakerThreshold: 5,
			CircuitBreakerTimeout:   60 * time.Second,
			EnableMetrics:           false, // Disable for tests
			EnableTracing:           false,
			MetricsNamespace:        "test_rag",
			DefaultTopK:             10,
			DefaultMaxTokens:        8000,
			DefaultMinScore:         0.7,
			DefaultNamespace:        "default",
			EnableCache:             true,
			EnableHybrid:            false, // Simplified for tests
			EnableRerank:            false,
		},
	}
}

func indexTestDocuments(t *testing.T, system *System) {
	t.Helper()

	testDocs := []struct {
		id       string
		content  string
		metadata map[string]interface{}
	}{
		{
			id:      "doc1",
			content: "package main func Start() error { return nil }",
			metadata: map[string]interface{}{
				"language":  "go",
				"file_path": "/test/server.go",
			},
		},
		{
			id:      "doc2",
			content: "type Server struct { config Config }",
			metadata: map[string]interface{}{
				"language":  "go",
				"file_path": "/test/types.go",
			},
		},
		{
			id:      "doc3",
			content: "This is markdown documentation about the API",
			metadata: map[string]interface{}{
				"language":  "markdown",
				"file_path": "/test/README.md",
			},
		},
	}

	for _, doc := range testDocs {
		err := system.IndexContent(context.Background(), doc.id, doc.content, doc.metadata)
		if err != nil {
			t.Logf("Failed to index %s: %v", doc.id, err)
		}
	}
}

// Additional test coverage

// =============================================================================
// trainIndex coverage
// =============================================================================

func TestTrainIndex_VariousDimensions(t *testing.T) {
	testCases := []struct {
		name       string
		dimensions int
	}{
		{"tiny dimensions", 8},
		{"small dimensions", 64},
		{"medium dimensions", 256},
		{"standard dimensions", 384},
		{"large dimensions", 768},
		{"xlarge dimensions", 1024},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.RAGVectorConfig{
				NumCentroids: 10,
				Dimensions:   tc.dimensions,
				NProbe:       3,
				Metric:       "cosine",
			}

			idx, err := createVectorIndex(cfg)
			require.NoError(t, err)

			err = trainIndex(idx, tc.dimensions)
			assert.NoError(t, err)
			assert.True(t, idx.IsTrained())
		})
	}
}

// =============================================================================
// EmbedBatch with context cancellation
// =============================================================================

func TestMockEmbedderAdapter_EmbedBatchContextCancelled(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// The mock checks for context cancellation, so this should return an error
	results, err := adapter.EmbedBatch(ctx, []string{"test"})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, results)
}

// =============================================================================
// createContextBuilderAdapter with all field variations
// =============================================================================

func TestCreateContextBuilderAdapter_FullConfig(t *testing.T) {
	cfg := config.ContextConfig{
		MaxTokens:          8000,
		DefaultTemplate:    "json",
		IncludeCitations:   true,
		TruncationStrategy: "hard",
		MaxResultsPerChunk: 50,
		TokenMultiplier:    2.0,
	}

	adapter, err := createContextBuilderAdapter(cfg)

	assert.NoError(t, err)
	assert.NotNil(t, adapter)
}

// =============================================================================
// RankerAdapter with actual ranking
// =============================================================================

func TestRankerAdapter_RankResults(t *testing.T) {
	rankingCfg := ranking.Config{
		BM25K1:        1.2,
		BM25B:         0.75,
		VectorWeight:  0.7,
		KeywordWeight: 0.3,
		RerankTopK:    5,
		EnableRerank:  false,
		RRFK:          60,
	}

	hybridRanker := ranking.NewHybridRanker(rankingCfg, nil)
	adapterCfg := &adapters.RankerConfig{
		EnableHybrid:  true,
		VectorWeight:  0.7,
		KeywordWeight: 0.3,
	}

	adapter, err := adapters.NewRankerAdapter(hybridRanker, adapters.WithRankerConfig(adapterCfg))
	require.NoError(t, err)

	// Create test results using pipeline.Result
	vectorResults := []pipeline.Result{
		{ID: "1", Score: 0.9, Content: "test content 1"},
		{ID: "2", Score: 0.7, Content: "test content 2"},
	}
	keywordResults := []pipeline.Result{
		{ID: "1", Score: 0.8, Content: "test content 1"},
		{ID: "3", Score: 0.5, Content: "test content 3"},
	}

	ranked, err := adapter.Rank(context.Background(), "test query", vectorResults, keywordResults)

	assert.NoError(t, err)
	assert.NotNil(t, ranked)
	assert.True(t, len(ranked) > 0)
}

// =============================================================================
// ContextBuilderAdapter with actual context building
// =============================================================================

func TestContextBuilderAdapter_Build(t *testing.T) {
	contextCfg := ragcontext.Config{
		DefaultMaxTokens:   4000,
		DefaultTemplate:    ragcontext.TemplateMarkdown,
		IncludeCitations:   true,
		MaxResultsPerChunk: 20,
		TokenMultiplier:    1.3,
		TruncationStrategy: ragcontext.TruncationSmart,
	}

	builder := ragcontext.NewBuilderWithConfig(contextCfg)
	adapterCfg := &adapters.ContextBuilderConfig{
		DefaultMaxTokens: 4000,
		DefaultTemplate:  ragcontext.TemplateMarkdown,
		IncludeCitations: true,
	}

	adapter, err := adapters.NewContextBuilderAdapter(builder, adapters.WithContextBuilderConfig(adapterCfg))
	require.NoError(t, err)

	results := []pipeline.Result{
		{ID: "1", Score: 0.9, Content: "This is test content for context building."},
	}

	opts := pipeline.ContextOptions{
		MaxTokens: 4000,
		Template:  "markdown",
	}

	ctx, err := adapter.Build(context.Background(), results, opts)

	assert.NoError(t, err)
	assert.NotNil(t, ctx)
}

// =============================================================================
// mockEmbedderAdapter edge cases
// =============================================================================

func TestMockEmbedderAdapter_EmptyText(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	emb, err := adapter.Embed(context.Background(), "")

	require.NoError(t, err)
	assert.Len(t, emb, 128)
}

func TestMockEmbedderAdapter_SpecialCharacters(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	testTexts := []string{
		"Hello\nWorld",
		"Tab\there",
		"Unicode: \u0041\u0042\u0043",
		"Emoji: test",
		"Quotes: 'single' and \"double\"",
		"backslash: \\path\\to\\file",
		"null: \x00",
	}

	for _, text := range testTexts {
		emb, err := adapter.Embed(context.Background(), text)
		require.NoError(t, err, "failed for text: %s", text)
		assert.Len(t, emb, 128)
	}
}

// =============================================================================
// queryHandlerAdapter edge cases
// =============================================================================

func TestQueryHandlerAdapter_EmptyQuery(t *testing.T) {
	embedder := &mockEmbedderAdapter{dimensions: 128}
	handler := &queryHandlerAdapter{embedder: embedder}

	result, err := handler.Process(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, "", result.Original)
	assert.Equal(t, "", result.Cleaned)
	assert.Len(t, result.Embedding, 128)
}

// =============================================================================
// Concurrent access tests
// =============================================================================

func TestMockEmbedderAdapter_ConcurrentEmbed(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	var wg sync.WaitGroup
	numGoroutines := 20
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := "concurrent test " + string(rune('A'+i))
			_, err := adapter.Embed(context.Background(), text)
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent embed failed: %v", err)
	}
}

func TestSystem_ConcurrentIndexAndQuery(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	var wg sync.WaitGroup
	numOps := 10

	// Concurrent indexing
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "doc-" + string(rune('A'+i))
			content := "Content for document " + id
			_ = system.IndexContent(context.Background(), id, content, nil)
		}(i)
	}

	// Concurrent queries
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			query := "query " + string(rune('A'+i))
			opts := pipeline.QueryOptions{TopK: 5}
			_, _ = system.Query(context.Background(), query, opts)
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// Additional NewSystem paths
// =============================================================================

func TestNewSystem_Metrics(t *testing.T) {
	for _, metric := range []string{"l2", "dot", "ip"} {
		t.Run(metric, func(t *testing.T) {
			cfg := createTestRAGConfig(t)
			cfg.Vector.Metric = metric

			system, err := NewSystem(cfg)

			require.NoError(t, err)
			require.NotNil(t, system)
			defer system.Close()
		})
	}
}

// =============================================================================
// Additional config variations
// =============================================================================

func TestNewSystem_AllTruncationStrategies(t *testing.T) {
	strategies := []string{"smart", "hard", "none", "unknown"}

	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			cfg := createTestRAGConfig(t)
			cfg.Context.TruncationStrategy = strategy

			system, err := NewSystem(cfg)

			require.NoError(t, err)
			require.NotNil(t, system)
			system.Close()
		})
	}
}

func TestNewSystem_RankingStrategies(t *testing.T) {
	strategies := []string{"hybrid", "fusion", "vector", "bm25"}

	for _, strategy := range strategies {
		t.Run("strategy_"+strategy, func(t *testing.T) {
			cfg := createTestRAGConfig(t)
			cfg.Ranking.Strategy = strategy

			system, err := NewSystem(cfg)

			require.NoError(t, err)
			require.NotNil(t, system)
			system.Close()
		})
	}
}

func TestNewSystem_WithReranking(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Ranking.EnableRerank = true
	cfg.Ranking.RerankTopK = 20

	system, err := NewSystem(cfg)

	require.NoError(t, err)
	require.NotNil(t, system)
	defer system.Close()
}

func TestInMemoryVectorIndex_Remove(t *testing.T) {
	t.Run("removes existing entry", func(t *testing.T) {
		idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 3})
		require.NoError(t, err)

		err = idx.Add("v1", []float32{1, 0, 0}, nil)
		require.NoError(t, err)
		err = idx.Add("v2", []float32{0, 1, 0}, nil)
		require.NoError(t, err)
		err = idx.Add("v3", []float32{0, 0, 1}, nil)
		require.NoError(t, err)
		assert.Equal(t, 3, idx.Size())

		err = idx.Remove("v2")
		require.NoError(t, err)
		assert.Equal(t, 2, idx.Size())

		// Verify removed entry is gone from search results.
		results, err := idx.Search([]float32{0, 1, 0}, 10, 0)
		require.NoError(t, err)
		for _, r := range results {
			assert.NotEqual(t, "v2", r.ID, "removed vector should not appear in search results")
		}
	})

	t.Run("remove non-existent id is a no-op", func(t *testing.T) {
		idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 3})
		require.NoError(t, err)

		err = idx.Add("v1", []float32{1, 0, 0}, nil)
		require.NoError(t, err)

		err = idx.Remove("non-existent")
		require.NoError(t, err)
		assert.Equal(t, 1, idx.Size())
	})

	t.Run("remove last entry leaves empty index", func(t *testing.T) {
		idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 2})
		require.NoError(t, err)

		err = idx.Add("only", []float32{1, 1}, nil)
		require.NoError(t, err)

		err = idx.Remove("only")
		require.NoError(t, err)
		assert.Equal(t, 0, idx.Size())
	})

	t.Run("remove first entry with swap", func(t *testing.T) {
		idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 2})
		require.NoError(t, err)

		err = idx.Add("a", []float32{1, 0}, nil)
		require.NoError(t, err)
		err = idx.Add("b", []float32{0, 1}, nil)
		require.NoError(t, err)

		// Remove first entry; "b" should be swapped into position 0.
		err = idx.Remove("a")
		require.NoError(t, err)
		assert.Equal(t, 1, idx.Size())

		// "b" should still be searchable.
		results, err := idx.Search([]float32{0, 1}, 1, 0)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "b", results[0].ID)
	})
}

func TestInMemoryVectorIndex_SaveAndLoad(t *testing.T) {
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 4})
	require.NoError(t, err)

	// Save and Load are no-ops; they should return nil.
	err = idx.Save("/tmp/test-vector-index")
	assert.NoError(t, err)

	err = idx.Load("/tmp/test-vector-index")
	assert.NoError(t, err)
}

// =============================================================================
// VectorIndex: NewVectorIndex invalid dimensions (error path)
// =============================================================================

func TestNewVectorIndex_InvalidDimensions(t *testing.T) {
	t.Run("zero dimensions returns error", func(t *testing.T) {
		idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 0})
		assert.Error(t, err)
		assert.Nil(t, idx)

		var configErr *VectorIndexConfigError
		assert.True(t, errors.As(err, &configErr))
		assert.Equal(t, "Dimensions", configErr.Field)
		assert.Contains(t, configErr.Reason, "positive")
	})

	t.Run("negative dimensions returns error", func(t *testing.T) {
		idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: -5})
		assert.Error(t, err)
		assert.Nil(t, idx)

		var configErr *VectorIndexConfigError
		assert.True(t, errors.As(err, &configErr))
	})
}

// =============================================================================
// VectorIndex: Search with DotProduct and L2 distance metrics
// =============================================================================

func TestInMemoryVectorIndex_SearchDotProduct(t *testing.T) {
	idx, err := NewVectorIndex(&VectorIndexConfig{
		Dimensions:     3,
		DistanceMetric: DotProductDistance,
	})
	require.NoError(t, err)

	err = idx.Add("v1", []float32{1, 0, 0}, nil)
	require.NoError(t, err)
	err = idx.Add("v2", []float32{0, 1, 0}, nil)
	require.NoError(t, err)
	err = idx.Add("v3", []float32{1, 1, 0}, nil)
	require.NoError(t, err)

	results, err := idx.Search([]float32{1, 0, 0}, 3, 0)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// v1 and v3 both have dot product 1 with query, v2 has 0.
	assert.True(t, results[0].Score >= results[1].Score, "results should be sorted by score descending")
	assert.True(t, results[1].Score >= results[2].Score, "results should be sorted by score descending")

	// The last result should be v2 with score 0.
	assert.Equal(t, "v2", results[2].ID)
	assert.InDelta(t, float32(0), results[2].Score, 0.001)
}

func TestInMemoryVectorIndex_SearchL2Distance(t *testing.T) {
	idx, err := NewVectorIndex(&VectorIndexConfig{
		Dimensions:     2,
		DistanceMetric: L2Distance,
	})
	require.NoError(t, err)

	err = idx.Add("close", []float32{1, 0}, nil)
	require.NoError(t, err)
	err = idx.Add("far", []float32{10, 10}, nil)
	require.NoError(t, err)

	results, err := idx.Search([]float32{1, 0}, 2, 0)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// "close" should have a higher score (lower distance) than "far".
	assert.Equal(t, "close", results[0].ID)
	assert.True(t, results[0].Score > results[1].Score, "closer vector should have higher score")
}

func TestInMemoryVectorIndex_SearchEmptyIndex(t *testing.T) {
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 3})
	require.NoError(t, err)

	results, err := idx.Search([]float32{1, 0, 0}, 5, 0)
	assert.NoError(t, err)
	assert.Nil(t, results)
}

func TestInMemoryVectorIndex_SearchDimensionMismatch(t *testing.T) {
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 3})
	require.NoError(t, err)

	err = idx.Add("v1", []float32{1, 0, 0}, nil)
	require.NoError(t, err)

	results, err := idx.Search([]float32{1, 0}, 5, 0)
	assert.Error(t, err)
	assert.Nil(t, results)

	var dimErr *VectorDimensionError
	assert.True(t, errors.As(err, &dimErr))
	assert.Equal(t, 3, dimErr.Expected)
	assert.Equal(t, 2, dimErr.Got)
}

func TestInMemoryVectorIndex_SearchKGreaterThanSize(t *testing.T) {
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 2})
	require.NoError(t, err)

	err = idx.Add("v1", []float32{1, 0}, nil)
	require.NoError(t, err)

	// Request more results than available.
	results, err := idx.Search([]float32{1, 0}, 100, 0)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// =============================================================================
// VectorIndex: Add with replacement (existing ID)
// =============================================================================

func TestInMemoryVectorIndex_AddReplacesExisting(t *testing.T) {
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 2})
	require.NoError(t, err)

	err = idx.Add("v1", []float32{1, 0}, map[string]interface{}{"version": 1})
	require.NoError(t, err)

	// Replace v1 with a different vector.
	err = idx.Add("v1", []float32{0, 1}, map[string]interface{}{"version": 2})
	require.NoError(t, err)

	// Size should still be 1 (replaced, not appended).
	assert.Equal(t, 1, idx.Size())

	// Search should find the updated vector.
	results, err := idx.Search([]float32{0, 1}, 1, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "v1", results[0].ID)
	assert.InDelta(t, float32(1.0), results[0].Score, 0.001)
}

// =============================================================================
// VectorIndex: cosineSimilarity zero-denominator path
// =============================================================================

func TestCosineSimilarity_ZeroVectors(t *testing.T) {
	// When both vectors are zero, the denominator is zero,
	// cosineSimilarity should return 0.
	result := cosineSimilarity([]float32{0, 0, 0}, []float32{0, 0, 0})
	assert.InDelta(t, float32(0), result, 0.001)
}

// =============================================================================
// Distance functions: dotProduct, l2Distance
// =============================================================================

func TestDotProduct(t *testing.T) {
	t.Run("orthogonal vectors", func(t *testing.T) {
		result := dotProduct([]float32{1, 0, 0}, []float32{0, 1, 0})
		assert.InDelta(t, float32(0), result, 0.001)
	})

	t.Run("parallel vectors", func(t *testing.T) {
		result := dotProduct([]float32{1, 2, 3}, []float32{1, 2, 3})
		// 1*1 + 2*2 + 3*3 = 14
		assert.InDelta(t, float32(14), result, 0.001)
	})

	t.Run("negative components", func(t *testing.T) {
		result := dotProduct([]float32{1, -1}, []float32{-1, 1})
		// 1*(-1) + (-1)*1 = -2
		assert.InDelta(t, float32(-2), result, 0.001)
	})

	t.Run("single dimension", func(t *testing.T) {
		result := dotProduct([]float32{5}, []float32{3})
		assert.InDelta(t, float32(15), result, 0.001)
	})
}

func TestL2Distance(t *testing.T) {
	t.Run("same vectors", func(t *testing.T) {
		result := l2Distance([]float32{1, 2, 3}, []float32{1, 2, 3})
		assert.InDelta(t, float32(0), result, 0.001)
	})

	t.Run("known distance", func(t *testing.T) {
		// sqrt((3-0)^2 + (4-0)^2) = sqrt(9+16) = sqrt(25) = 5
		result := l2Distance([]float32{0, 0}, []float32{3, 4})
		assert.InDelta(t, float32(5), result, 0.001)
	})

	t.Run("single dimension", func(t *testing.T) {
		result := l2Distance([]float32{10}, []float32{3})
		assert.InDelta(t, float32(7), result, 0.001)
	})

	t.Run("negative components", func(t *testing.T) {
		result := l2Distance([]float32{-1, -2}, []float32{1, 2})
		// sqrt(4 + 16) = sqrt(20) ~= 4.472
		assert.InDelta(t, float32(4.472), result, 0.01)
	})
}

// =============================================================================
// Error types: VectorIndexConfigError, VectorDimensionError
// =============================================================================

func TestErrorTypes(t *testing.T) {
	cause := errors.New("root cause")
	cleanup := errors.New("cleanup error")

	testCases := []struct {
		name    string
		err     error
		wantMsg string
		wantIs  []error
	}{
		{
			name:    "VectorIndexConfigError",
			err:     &VectorIndexConfigError{Field: "Dimensions", Reason: "must be positive"},
			wantMsg: "invalid vector index config: Dimensions must be positive",
		},
		{
			name:    "VectorDimensionError",
			err:     &VectorDimensionError{Expected: 128, Got: 256},
			wantMsg: "vector dimension mismatch",
		},
		{
			name:    "DimensionMismatchError",
			err:     &DimensionMismatchError{EmbeddingDim: 384, VectorDim: 768},
			wantMsg: "dimension mismatch between embedding and vector config",
		},
		{
			name:    "UnsupportedProviderError",
			err:     &UnsupportedProviderError{Provider: "bogus"},
			wantMsg: "unsupported embedding provider: 'bogus' - supported providers: mock, ollama, openai, voyage, lmstudio",
		},
		{
			name:    "ComponentError",
			err:     &ComponentError{Component: "embedder", Operation: "create", Cause: cause},
			wantMsg: "failed to create embedder: root cause",
			wantIs:  []error{cause},
		},
		{
			name:    "MultiComponentError",
			err:     &MultiComponentError{Primary: cause, Cleanup: cleanup},
			wantMsg: "root cause (also: cleanup error)",
			wantIs:  []error{cause, cleanup},
		},
		{
			name:    "SentinelCauseError",
			err:     &SentinelCauseError{Sentinel: ErrInvalidConfig, Cause: cause},
			wantMsg: "invalid configuration: root cause",
			wantIs:  []error{ErrInvalidConfig, cause},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantMsg, tc.err.Error())
			for _, target := range tc.wantIs {
				assert.ErrorIs(t, tc.err, target)
			}
		})
	}
}

// =============================================================================
// RAGSearcher: error paths
// =============================================================================

func TestRAGSearcher_AddEmbedError(t *testing.T) {
	failEmb := &failingEmbedder{failAfter: 0}
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 384})
	require.NoError(t, err)

	searcher := NewRAGSearcher(idx, failEmb)

	err = searcher.Add(context.Background(), "doc1", "test content", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embed failed")
}

func TestRAGSearcher_SearchIndexError(t *testing.T) {
	embedder := &mockEmbedderAdapter{dimensions: 128}
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 128})
	require.NoError(t, err)

	searcher := NewRAGSearcher(idx, embedder)

	// Add a document.
	err = searcher.Add(context.Background(), "doc1", "content", nil)
	require.NoError(t, err)

	// Search with wrong dimension embedding to trigger error.
	wrongDimEmbedding := make([]float32, 64)
	results, err := searcher.Search(context.Background(), wrongDimEmbedding, pipeline.SearchOptions{TopK: 5})
	assert.Error(t, err)
	assert.Nil(t, results)
}

func TestRAGSearcher_SearchMinScoreFiltering(t *testing.T) {
	embedder := &mockEmbedderAdapter{dimensions: 3}
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 3})
	require.NoError(t, err)

	searcher := NewRAGSearcher(idx, embedder)

	// Manually add vectors to the index (bypassing embedder).
	err = idx.Add("high", []float32{1, 0, 0}, nil)
	require.NoError(t, err)
	err = idx.Add("low", []float32{-1, 0, 0}, nil)
	require.NoError(t, err)

	// Store content for retrieval.
	searcher.mu.Lock()
	searcher.contents["high"] = "high relevance"
	searcher.contents["low"] = "low relevance"
	searcher.mu.Unlock()

	// Search with high min score. The "low" entry should have negative cosine
	// similarity to query [1,0,0] and should be filtered out.
	results, err := searcher.Search(context.Background(), []float32{1, 0, 0}, pipeline.SearchOptions{
		TopK:     10,
		MinScore: 0.5,
	})
	require.NoError(t, err)

	for _, r := range results {
		assert.True(t, r.Score >= 0.5, "result %s score %f should be >= 0.5", r.ID, r.Score)
	}
}

func TestRAGSearcher_SearchDefaultTopK(t *testing.T) {
	embedder := &mockEmbedderAdapter{dimensions: 2}
	idx, err := NewVectorIndex(&VectorIndexConfig{Dimensions: 2})
	require.NoError(t, err)

	searcher := NewRAGSearcher(idx, embedder)
	err = idx.Add("v1", []float32{1, 0}, nil)
	require.NoError(t, err)

	searcher.mu.Lock()
	searcher.contents["v1"] = "content"
	searcher.mu.Unlock()

	// TopK 0 should default to 10.
	results, err := searcher.Search(context.Background(), []float32{1, 0}, pipeline.SearchOptions{TopK: 0})
	require.NoError(t, err)
	assert.Len(t, results, 1) // Only 1 vector in index.
}

// =============================================================================
// createEmbedder with safe providers only
// =============================================================================

// =============================================================================
// createContextBuilderAdapter with different strategies
// =============================================================================

// =============================================================================
// System with nil metadata
// =============================================================================

func TestSystem_IndexContentWithNilMetadata(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	// Index with nil metadata - should work and create empty map
	err = system.IndexContent(context.Background(), "test-nil-meta", "test content", nil)

	assert.NoError(t, err)
}

// =============================================================================
// queryHandlerAdapter tests
// =============================================================================

// =============================================================================
// NewSystem with pipeline config variations
// =============================================================================

func TestNewSystem_PipelineConfigVariations(t *testing.T) {
	t.Run("with circuit breaker enabled", func(t *testing.T) {
		cfg := createTestRAGConfig(t)
		cfg.Pipeline.EnableCircuitBreaker = true
		cfg.Pipeline.CircuitBreakerThreshold = 5
		cfg.Pipeline.CircuitBreakerTimeout = 30000000000 // 30s

		system, err := NewSystem(cfg)

		require.NoError(t, err)
		require.NotNil(t, system)
		system.Close()
	})

	t.Run("with metrics enabled", func(t *testing.T) {
		cfg := createTestRAGConfig(t)
		cfg.Pipeline.EnableMetrics = true

		system, err := NewSystem(cfg)

		require.NoError(t, err)
		require.NotNil(t, system)
		system.Close()
	})

	t.Run("with hybrid enabled", func(t *testing.T) {
		cfg := createTestRAGConfig(t)
		cfg.Pipeline.EnableHybrid = true

		system, err := NewSystem(cfg)

		require.NoError(t, err)
		require.NotNil(t, system)
		system.Close()
	})

	t.Run("with rerank enabled", func(t *testing.T) {
		cfg := createTestRAGConfig(t)
		cfg.Pipeline.EnableRerank = true

		system, err := NewSystem(cfg)

		require.NoError(t, err)
		require.NotNil(t, system)
		system.Close()
	})
}

// =============================================================================
// Error type tests
// =============================================================================

func TestRAGErrors(t *testing.T) {
	assert.Equal(t, "system is closed", ErrSystemClosed.Error())
	assert.Equal(t, "invalid configuration", ErrInvalidConfig.Error())
	assert.Equal(t, "dimension mismatch between embedding and vector config", ErrDimensionMismatch.Error())
	assert.Equal(t, "unsupported embedding provider", ErrUnsupportedProvider.Error())
	assert.Equal(t, "failed to create embedding provider", ErrProviderCreationFailed.Error())
}

// =============================================================================
// Additional System tests for edge cases
// =============================================================================

func TestNewSystem_EmbedderCreationError(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Embedding.Provider = "unsupported-provider"

	system, err := NewSystem(cfg)

	assert.Error(t, err)
	assert.Nil(t, system)
	assert.Contains(t, err.Error(), "failed to create embedder")
	assert.Contains(t, err.Error(), "unsupported embedding provider")
}

func TestNewSystem_VectorIndexCreationError(t *testing.T) {
	// Test with zero centroids which should fail
	cfg := createTestRAGConfig(t)
	cfg.Vector.NumCentroids = 0

	system, err := NewSystem(cfg)

	// Should error during validation or creation
	assert.Error(t, err)
	assert.Nil(t, system)
}

// Test System closed error paths

func TestSystem_QueryAfterClose(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Close the system
	err = system.Close()
	require.NoError(t, err)

	// Try to query after closing
	opts := pipeline.QueryOptions{TopK: 5}
	response, err := system.Query(context.Background(), "test query", opts)

	assert.Error(t, err)
	assert.Nil(t, response)
	assert.ErrorIs(t, err, ErrSystemClosed)
}

func TestSystem_IndexContentAfterClose(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Close the system
	err = system.Close()
	require.NoError(t, err)

	// Try to index content after closing
	err = system.IndexContent(context.Background(), "test-id", "test content", nil)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrSystemClosed)
}

func TestSystem_StatsAfterClose(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Close the system
	err = system.Close()
	require.NoError(t, err)

	// Try to get stats after closing
	stats := system.Stats()

	// Should return empty stats
	assert.Equal(t, pipeline.Stats{}, stats)
}

// Test Close error paths with error-returning components

func TestSystem_CloseWithPipelineError(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Replace pipeline with error-returning mock
	system.pipeline = &errorPipeline{}

	err = system.Close()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline close")
}

func TestSystem_CloseWithEmbedderError(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Replace embedder with error-returning mock
	system.embedder = &errorEmbedder{}

	err = system.Close()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embedder close")
}

// Test IndexContent with nil metadata

// Test vector index with different metrics
// Mock implementations that return errors

type errorPipeline struct{}

func (e *errorPipeline) Query(ctx context.Context, query string, opts pipeline.QueryOptions) (*pipeline.QueryResponse, error) {
	return nil, errors.New("pipeline error")
}

func (e *errorPipeline) Stats() pipeline.Stats {
	return pipeline.Stats{}
}

func (e *errorPipeline) Health() pipeline.HealthStatus {
	return pipeline.HealthStatus{
		Healthy: false,
		Status:  "unhealthy",
	}
}

func (e *errorPipeline) Close() error {
	return errors.New("pipeline close error")
}

type errorEmbedder struct{}

func (e *errorEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("embed error")
}

func (e *errorEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, errors.New("embed batch error")
}

func (e *errorEmbedder) Dimensions() int {
	return 384
}

func (e *errorEmbedder) MaxTokens() int {
	return 8192
}

func (e *errorEmbedder) Name() string {
	return "error"
}

func (e *errorEmbedder) Close() error {
	return errors.New("embedder close error")
}

type failingEmbedder struct {
	failAfter int
	count     int
}

func (f *failingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if f.count >= f.failAfter {
		return nil, errors.New("embed failed")
	}
	f.count++
	return make([]float32, 384), nil
}

func (f *failingEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := f.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

func (f *failingEmbedder) Dimensions() int {
	return 384
}

func (f *failingEmbedder) MaxTokens() int {
	return 8192
}

func (f *failingEmbedder) Name() string {
	return "failing"
}

func (f *failingEmbedder) Close() error {
	return nil
}

var _ embedding.Provider = (*failingEmbedder)(nil)
var _ embedding.Provider = (*errorEmbedder)(nil)

// TestCreateVectorIndex_DotMetric specifically tests the "dot" metric path
// =============================================================================
// Test combined error scenarios - Close with multiple error components
// =============================================================================

func TestSystemCloseWithAllComponentErrors(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Replace all components with error-returning mocks
	system.pipeline = &errorPipeline{}
	system.embedder = &errorEmbedder{}
	// vectorSearcher can't be easily replaced but we test the scenario

	err = system.Close()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close pipeline")
	// Should contain both pipeline and embedder errors
	assert.Contains(t, err.Error(), "pipeline close")
	assert.Contains(t, err.Error(), "embedder close")
}

// =============================================================================
// Test createEmbedder mock provider path
// We only test the mock provider since others may panic or require network
// =============================================================================

// =============================================================================
// Test createRankerAdapter with different strategies
// =============================================================================

func TestCreateRankerAdapterStrategies(t *testing.T) {
	strategies := []string{"hybrid", "fusion", "vector", "bm25", "unknown"}

	for _, strategy := range strategies {
		t.Run("strategy_"+strategy, func(t *testing.T) {
			cfg := config.RankingConfig{
				Strategy:      strategy,
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    10,
				RRFK:          60,
			}

			adapter, err := createRankerAdapter(cfg)

			require.NoError(t, err)
			assert.NotNil(t, adapter)
		})
	}
}

// =============================================================================
// Test trainIndex with edge cases
// =============================================================================

// =============================================================================
// Test pipeline error path at lines 141-147
// We trigger this by passing invalid config that passes initial validation
// but fails pipeline.NewPipeline validation
// =============================================================================

// =============================================================================
// Test queryHandlerAdapter edge cases
// =============================================================================

// =============================================================================
// Test System operations in various states
// =============================================================================

// =============================================================================
// Test createContextBuilderAdapter with all truncation strategies
// =============================================================================

// =============================================================================
// Test createVectorIndex with all metrics
// =============================================================================

func TestCreateVectorIndexAllMetrics(t *testing.T) {
	metrics := []string{"cosine", "l2", "ip", "dot", "unknown", ""}

	for _, metric := range metrics {
		t.Run("metric_"+metric, func(t *testing.T) {
			cfg := config.RAGVectorConfig{
				NumCentroids: 5,
				Dimensions:   64,
				NProbe:       2,
				Metric:       metric,
			}

			idx, err := createVectorIndex(cfg)

			require.NoError(t, err)
			assert.NotNil(t, idx)
		})
	}
}

// =============================================================================
// Test System with various query options to increase coverage
// =============================================================================

// =============================================================================
// Test System concurrent operations
// =============================================================================

// TestMockEmbedderAdapter tests all methods of mockEmbedderAdapter
func TestMockEmbedderAdapter(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	t.Run("Embed generates deterministic embedding", func(t *testing.T) {
		ctx := context.Background()
		emb1, err := adapter.Embed(ctx, "hello world")
		require.NoError(t, err)
		assert.Len(t, emb1, 128)

		// Same text should return same embedding
		emb2, err := adapter.Embed(ctx, "hello world")
		require.NoError(t, err)
		assert.Equal(t, emb1, emb2)

		// Different text should return different embedding
		emb3, err := adapter.Embed(ctx, "goodbye world")
		require.NoError(t, err)
		assert.NotEqual(t, emb1, emb3)
	})

	t.Run("EmbedBatch processes multiple texts", func(t *testing.T) {
		ctx := context.Background()
		texts := []string{"text one", "text two", "text three"}

		results, err := adapter.EmbedBatch(ctx, texts)

		require.NoError(t, err)
		assert.Len(t, results, 3)
		for i, emb := range results {
			assert.Len(t, emb, 128, "embedding %d should have correct dimension", i)
		}
	})

	t.Run("EmbedBatch with empty slice", func(t *testing.T) {
		ctx := context.Background()

		results, err := adapter.EmbedBatch(ctx, []string{})

		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("EmbedBatch with single text", func(t *testing.T) {
		ctx := context.Background()

		results, err := adapter.EmbedBatch(ctx, []string{"single"})

		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Len(t, results[0], 128)
	})

	t.Run("Dimensions returns correct value", func(t *testing.T) {
		assert.Equal(t, 128, adapter.Dimensions())
	})

	t.Run("MaxTokens returns correct value", func(t *testing.T) {
		assert.Equal(t, 8192, adapter.MaxTokens())
	})

	t.Run("Name returns mock", func(t *testing.T) {
		assert.Equal(t, "mock", adapter.Name())
	})

	t.Run("Close returns nil", func(t *testing.T) {
		err := adapter.Close()
		assert.NoError(t, err)
	})
}

// TestMockEmbedderAdapterVariousDimensions tests adapter with various dimensions
func TestMockEmbedderAdapterVariousDimensions(t *testing.T) {
	testCases := []struct {
		name       string
		dimensions int
	}{
		{"small", 64},
		{"medium", 384},
		{"large", 768},
		{"xlarge", 1536},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &mockEmbedderAdapter{dimensions: tc.dimensions}

			emb, err := adapter.Embed(context.Background(), "test")
			require.NoError(t, err)
			assert.Len(t, emb, tc.dimensions)
			assert.Equal(t, tc.dimensions, adapter.Dimensions())
		})
	}
}

// TestQueryHandlerAdapter tests all paths in queryHandlerAdapter
func TestQueryHandlerAdapter(t *testing.T) {
	t.Run("Process succeeds with valid embedder", func(t *testing.T) {
		embedder := &mockEmbedderAdapter{dimensions: 128}
		handler := &queryHandlerAdapter{embedder: embedder}

		result, err := handler.Process(context.Background(), "test query")

		require.NoError(t, err)
		assert.Equal(t, "test query", result.Original)
		assert.Equal(t, "test query", result.Cleaned)
		assert.Equal(t, "search", result.Intent)
		assert.Len(t, result.Embedding, 128)
	})

	t.Run("Process fails when embedder fails", func(t *testing.T) {
		embedder := &embeddingErrorProvider{}
		handler := &queryHandlerAdapter{embedder: embedder}

		result, err := handler.Process(context.Background(), "test query")

		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("Close returns nil", func(t *testing.T) {
		embedder := &mockEmbedderAdapter{dimensions: 128}
		handler := &queryHandlerAdapter{embedder: embedder}

		err := handler.Close()
		assert.NoError(t, err)
	})
}

// TestCreateContextBuilderAdapter tests all truncation strategy paths
func TestCreateContextBuilderAdapter(t *testing.T) {
	testCases := []struct {
		name     string
		strategy string
	}{
		{"smart truncation", "smart"},
		{"hard truncation", "hard"},
		{"no truncation", "none"},
		{"default truncation", "unknown"},
		{"empty truncation", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.ContextConfig{
				MaxTokens:          4000,
				DefaultTemplate:    "markdown",
				IncludeCitations:   true,
				TruncationStrategy: tc.strategy,
				MaxResultsPerChunk: 20,
				TokenMultiplier:    1.3,
			}

			adapter, err := createContextBuilderAdapter(cfg)

			assert.NoError(t, err)
			assert.NotNil(t, adapter)
		})
	}
}

// TestCreateEmbedder tests all embedding provider paths
func TestCreateEmbedder(t *testing.T) {
	t.Run("mock provider", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "mock",
			Dimensions: 384,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 384, embedder.Dimensions())
	})

	// With the "vector" build tag enabled (always on when rag is enabled),
	// real embedding providers should create embedders successfully.

	t.Run("ollama provider creates embedder", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "ollama",
			Model:      "nomic-embed-text",
			Dimensions: 384,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 384, embedder.Dimensions())
	})

	t.Run("ollama provider with custom URL creates embedder", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "ollama",
			APIURL:     "http://custom-host:11434",
			Model:      "nomic-embed-text",
			Dimensions: 384,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 384, embedder.Dimensions())
	})

	t.Run("openai provider creates embedder", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "openai",
			APIKey:     "test-key",
			Model:      "text-embedding-3-small",
			Dimensions: 1536,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 1536, embedder.Dimensions())
	})

	t.Run("openai provider with custom URL creates embedder", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "openai",
			APIURL:     "https://custom-api.openai.com",
			APIKey:     "test-key",
			Model:      "text-embedding-3-small",
			Dimensions: 1536,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 1536, embedder.Dimensions())
	})

	t.Run("voyage provider creates embedder", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "voyage",
			APIKey:     "test-key",
			Model:      "voyage-code-2",
			Dimensions: 1536,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 1536, embedder.Dimensions())
	})

	t.Run("voyage provider with custom URL creates embedder", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "voyage",
			APIURL:     "https://custom-voyage.api.com/v1/embeddings",
			APIKey:     "test-key",
			Model:      "voyage-2",
			Dimensions: 1024,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 1024, embedder.Dimensions())
	})

	t.Run("lmstudio provider creates embedder", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "lmstudio",
			Model:      "local-model",
			Dimensions: 768,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 768, embedder.Dimensions())
	})

	t.Run("lmstudio provider with custom URL creates embedder", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "lmstudio",
			APIURL:     "http://custom-lmstudio:1234",
			Model:      "local-model",
			Dimensions: 768,
		}

		embedder, err := createEmbedder(cfg)

		require.NoError(t, err)
		assert.NotNil(t, embedder)
		assert.Equal(t, 768, embedder.Dimensions())
	})

	t.Run("unsupported provider returns error", func(t *testing.T) {
		cfg := config.EmbeddingConfig{
			Provider:   "invalid-provider",
			Dimensions: 384,
		}

		embedder, err := createEmbedder(cfg)

		assert.Error(t, err)
		assert.Nil(t, embedder)
		var providerErr *UnsupportedProviderError
		require.ErrorAs(t, err, &providerErr)
		assert.Equal(t, "invalid-provider", providerErr.Provider)
	})
}

// embeddingErrorProvider implements embedding.Provider that always returns errors
type embeddingErrorProvider struct{}

func (e *embeddingErrorProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("embedding error")
}

func (e *embeddingErrorProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, errors.New("batch embedding error")
}

func (e *embeddingErrorProvider) Dimensions() int { return 384 }
func (e *embeddingErrorProvider) MaxTokens() int  { return 8192 }
func (e *embeddingErrorProvider) Name() string    { return "error" }
func (e *embeddingErrorProvider) Close() error    { return nil }
