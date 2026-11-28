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

package adapters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ranking"
)

func TestNewRankerAdapter(t *testing.T) {
	t.Run("success with valid ranker", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()

		adapter, err := NewRankerAdapter(hybridRanker)

		require.NoError(t, err)
		assert.NotNil(t, adapter)
		assert.NotNil(t, adapter.config)
		assert.Equal(t, true, adapter.config.EnableHybrid)
	})

	t.Run("error with nil ranker", func(t *testing.T) {
		adapter, err := NewRankerAdapter(nil)

		assert.Error(t, err)
		assert.Nil(t, adapter)
		assert.ErrorIs(t, err, ErrNilHybridRanker)
	})

	t.Run("success with options", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()
		log := createTestLogger(t)
		config := &RankerConfig{
			EnableHybrid:  false,
			VectorWeight:  0.8,
			KeywordWeight: 0.2,
		}

		adapter, err := NewRankerAdapter(
			hybridRanker,
			WithRankerConfig(config),
			WithRankerLogger(log),
		)

		require.NoError(t, err)
		assert.NotNil(t, adapter)
		assert.Equal(t, false, adapter.config.EnableHybrid)
		assert.Equal(t, float32(0.8), adapter.config.VectorWeight)
		assert.NotNil(t, adapter.logger)
	})

	t.Run("error with invalid config", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()
		config := &RankerConfig{
			VectorWeight:  -0.5, // Invalid
			KeywordWeight: 0.3,
		}

		adapter, err := NewRankerAdapter(hybridRanker, WithRankerConfig(config))

		assert.Error(t, err)
		assert.Nil(t, adapter)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("nil config option is ignored", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()

		adapter, err := NewRankerAdapter(hybridRanker, WithRankerConfig(nil))

		require.NoError(t, err)
		assert.NotNil(t, adapter)
		assert.NotNil(t, adapter.config)
	})

	t.Run("nil logger option is ignored", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()

		adapter, err := NewRankerAdapter(hybridRanker, WithRankerLogger(nil))

		require.NoError(t, err)
		assert.NotNil(t, adapter)
		assert.Nil(t, adapter.logger)
	})
}

func TestRankerAdapter_Rank(t *testing.T) {
	t.Run("success with vector results only", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)
		vectorResults := createTestPipelineResults(3)

		results, err := adapter.Rank(context.Background(), "test query", vectorResults, nil)

		require.NoError(t, err)
		assert.NotNil(t, results)
		assert.Len(t, results, 3)
	})

	t.Run("success with keyword results only", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)
		keywordResults := createTestPipelineResults(2)

		results, err := adapter.Rank(context.Background(), "test query", nil, keywordResults)

		require.NoError(t, err)
		assert.NotNil(t, results)
		assert.Len(t, results, 2)
	})

	t.Run("success with hybrid results", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)
		vectorResults := createTestPipelineResults(3)
		keywordResults := createTestPipelineResults(2)

		results, err := adapter.Rank(context.Background(), "test query", vectorResults, keywordResults)

		require.NoError(t, err)
		assert.NotNil(t, results)
		// Hybrid ranking deduplicates and fuses results
		assert.Greater(t, len(results), 0)
	})

	t.Run("success with logger enabled", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()
		log := createTestLogger(t)
		adapter, err := NewRankerAdapter(hybridRanker, WithRankerLogger(log))
		require.NoError(t, err)

		vectorResults := createTestPipelineResults(3)

		results, err := adapter.Rank(context.Background(), "test query", vectorResults, nil)

		require.NoError(t, err)
		assert.NotNil(t, results)
		assert.Len(t, results, 3)
	})

	t.Run("error with empty query", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)
		vectorResults := createTestPipelineResults(3)

		results, err := adapter.Rank(context.Background(), "", vectorResults, nil)

		assert.Error(t, err)
		assert.Nil(t, results)
		assert.ErrorIs(t, err, ErrEmptyQuery)
	})

	t.Run("error with no results", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)

		results, err := adapter.Rank(context.Background(), "test query", nil, nil)

		assert.Error(t, err)
		assert.Nil(t, results)
		assert.ErrorIs(t, err, ErrEmptyResults)
	})

	t.Run("error with empty result slices", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)

		results, err := adapter.Rank(context.Background(), "test query", []pipeline.Result{}, []pipeline.Result{})

		assert.Error(t, err)
		assert.Nil(t, results)
		assert.ErrorIs(t, err, ErrEmptyResults)
	})

	t.Run("context cancellation before ranking", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)
		vectorResults := createTestPipelineResults(3)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		results, err := adapter.Rank(ctx, "test query", vectorResults, nil)

		assert.Error(t, err)
		assert.Nil(t, results)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("hybrid disabled falls back to simple ranking", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()
		adapter, err := NewRankerAdapter(hybridRanker, WithHybridEnabled(false))
		require.NoError(t, err)

		vectorResults := createTestPipelineResults(3)
		keywordResults := createTestPipelineResults(2)

		results, err := adapter.Rank(context.Background(), "test query", vectorResults, keywordResults)

		require.NoError(t, err)
		assert.NotNil(t, results)
		// When hybrid is disabled, should use vector results only
		assert.Len(t, results, 3)
	})

	t.Run("results preserve metadata", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)
		vectorResults := []pipeline.Result{
			{
				ID:       "doc1",
				Content:  "Test content",
				FilePath: "/test/file.go",
				Metadata: map[string]interface{}{
					"language": "go",
				},
				StartLine: 10,
				EndLine:   20,
				Score:     0.9,
			},
		}

		results, err := adapter.Rank(context.Background(), "test", vectorResults, nil)

		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "doc1", results[0].ID)
		assert.Equal(t, "Test content", results[0].Content)
		assert.Equal(t, "/test/file.go", results[0].FilePath)
		assert.Equal(t, 10, results[0].StartLine)
		assert.Equal(t, 20, results[0].EndLine)
	})

	t.Run("hybrid enabled with only vector results fuses them with their keyword scores", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()
		adapter, err := NewRankerAdapter(hybridRanker, WithHybridEnabled(true))
		require.NoError(t, err)

		vectorResults := createTestPipelineResults(3)

		results, err := adapter.Rank(context.Background(), "test query", vectorResults, nil)

		require.NoError(t, err)
		assert.NotNil(t, results)
		assert.Len(t, results, 3)
	})

	t.Run("hybrid enabled with only keyword results falls back", func(t *testing.T) {
		hybridRanker := createTestHybridRanker()
		adapter, err := NewRankerAdapter(hybridRanker, WithHybridEnabled(true))
		require.NoError(t, err)

		keywordResults := createTestPipelineResults(2)

		results, err := adapter.Rank(context.Background(), "test query", nil, keywordResults)

		require.NoError(t, err)
		assert.NotNil(t, results)
		assert.Len(t, results, 2)
	})
}

type weightQueryHandler struct{}

func (weightQueryHandler) Process(_ context.Context, q string) (*pipeline.ProcessedQuery, error) {
	return &pipeline.ProcessedQuery{Original: q, Cleaned: q, Embedding: []float32{0.1}}, nil
}
func (weightQueryHandler) Close() error { return nil }

type weightVectorSearcher struct{ results []pipeline.Result }

func (s weightVectorSearcher) Search(context.Context, []float32, pipeline.SearchOptions) ([]pipeline.Result, error) {
	out := make([]pipeline.Result, len(s.results))
	copy(out, s.results)
	return out, nil
}
func (weightVectorSearcher) Close() error { return nil }

type weightContextBuilder struct{}

func (weightContextBuilder) Build(_ context.Context, results []pipeline.Result, _ pipeline.ContextOptions) (*pipeline.BuiltContext, error) {
	return &pipeline.BuiltContext{Content: "ctx", Tokens: len(results)}, nil
}
func (weightContextBuilder) Close() error { return nil }

// The pipeline passes the ranker no keyword list, so the hybrid ranker's
// VectorWeight and KeywordWeight only matter if the adapter BM25-scores the
// vector results itself. Two weight settings must order the same documents
// differently.
func TestRankerAdapter_PipelineQueryHonoursHybridWeights(t *testing.T) {
	docs := []pipeline.Result{
		{ID: "vector-best", Content: "alpha beta gamma delta", Score: 0.9},
		{ID: "keyword-best", Content: "connection pool pool settings", Score: 0.8},
	}
	order := func(vectorWeight, keywordWeight float32) []string {
		cfg := ranking.DefaultConfig()
		cfg.EnableRerank = false
		cfg.VectorWeight = vectorWeight
		cfg.KeywordWeight = keywordWeight
		adapter, err := NewRankerAdapter(ranking.NewHybridRanker(cfg, nil), WithRankerConfig(&RankerConfig{
			EnableHybrid: true, VectorWeight: vectorWeight, KeywordWeight: keywordWeight,
		}))
		require.NoError(t, err)

		pcfg := pipeline.DefaultConfig()
		pcfg.EnableCircuitBreaker = false
		pcfg.EnableMetrics = false
		p, err := pipeline.NewPipeline(pcfg, weightQueryHandler{}, weightVectorSearcher{results: docs}, adapter, weightContextBuilder{})
		require.NoError(t, err)

		// The default MinScore (0.7) applies to the vector scores, so both
		// documents survive and the fused ranking decides the order.
		resp, err := p.Query(context.Background(), "pool", pipeline.QueryOptions{TopK: 10})
		require.NoError(t, err)
		ids := make([]string, 0, len(resp.Results))
		for _, r := range resp.Results {
			ids = append(ids, r.ID)
		}
		return ids
	}

	assert.Equal(t, []string{"vector-best", "keyword-best"}, order(0.75, 0.25))
	assert.Equal(t, []string{"keyword-best", "vector-best"}, order(0.25, 0.75))
}

// TestRankerAdapter_PipelineScoresAreSimilarities proves a hybrid query's
// results report the vector similarity, not the fused RRF score (about
// 1/60): every score is at least MinScore and never rises down the list,
// while the fused ranking still decides the order.
func TestRankerAdapter_PipelineScoresAreSimilarities(t *testing.T) {
	docs := []pipeline.Result{
		{ID: "vector-best", Content: "alpha beta gamma delta", Score: 0.9},
		{ID: "keyword-best", Content: "connection pool pool settings", Score: 0.8},
	}
	query := func(vectorWeight, keywordWeight float32) []pipeline.Result {
		cfg := ranking.DefaultConfig()
		cfg.EnableRerank = false
		cfg.VectorWeight = vectorWeight
		cfg.KeywordWeight = keywordWeight
		adapter, err := NewRankerAdapter(ranking.NewHybridRanker(cfg, nil), WithRankerConfig(&RankerConfig{
			EnableHybrid: true, VectorWeight: vectorWeight, KeywordWeight: keywordWeight,
		}))
		require.NoError(t, err)

		pcfg := pipeline.DefaultConfig()
		pcfg.EnableCircuitBreaker = false
		pcfg.EnableMetrics = false
		p, err := pipeline.NewPipeline(pcfg, weightQueryHandler{}, weightVectorSearcher{results: docs}, adapter, weightContextBuilder{})
		require.NoError(t, err)

		resp, err := p.Query(context.Background(), "pool", pipeline.QueryOptions{TopK: 10, MinScore: 0.75})
		require.NoError(t, err)
		require.Len(t, resp.Results, 2)
		for i, r := range resp.Results {
			assert.GreaterOrEqual(t, r.Score, float32(0.75), "result %d", i)
			if i > 0 {
				assert.LessOrEqual(t, r.Score, resp.Results[i-1].Score, "result %d", i)
			}
		}
		return resp.Results
	}

	vectorLed := query(0.75, 0.25)
	assert.Equal(t, "vector-best", vectorLed[0].ID)
	assert.InDelta(t, 0.9, vectorLed[0].Score, 1e-6)
	assert.InDelta(t, 0.8, vectorLed[1].Score, 1e-6)

	keywordLed := query(0.25, 0.75)
	assert.Equal(t, "keyword-best", keywordLed[0].ID)
	assert.InDelta(t, 0.8, keywordLed[0].Score, 1e-6)
	assert.InDelta(t, 0.8, keywordLed[1].Score, 1e-6) // capped at the result above
}

// TestRankerAdapter_PipelineDefaultMinScoreFiltersVectorScores proves the
// pipeline's default threshold (0.7) applies to the vector similarity scores
// before ranking, not to the small fused RRF scores: with the default hybrid
// adapter, a 0.9 match is returned with context and a 0.5 match is dropped.
func TestRankerAdapter_PipelineDefaultMinScoreFiltersVectorScores(t *testing.T) {
	docs := []pipeline.Result{
		{ID: "close", Content: "connection pool settings", Score: 0.9},
		{ID: "far", Content: "unrelated text about weather", Score: 0.5},
	}
	pcfg := pipeline.DefaultConfig()
	pcfg.EnableCircuitBreaker = false
	pcfg.EnableMetrics = false
	require.InDelta(t, 0.7, pcfg.DefaultMinScore, 1e-6)

	p, err := pipeline.NewPipeline(pcfg, weightQueryHandler{}, weightVectorSearcher{results: docs},
		createTestRankerAdapter(t), weightContextBuilder{})
	require.NoError(t, err)

	resp, err := p.Query(context.Background(), "pool", pipeline.QueryOptions{TopK: 10})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "close", resp.Results[0].ID)
	assert.NotEmpty(t, resp.Context)
}

func TestRankerAdapter_Close(t *testing.T) {
	t.Run("close returns nil", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)

		err := adapter.Close()

		assert.NoError(t, err)
	})

	t.Run("close can be called multiple times", func(t *testing.T) {
		adapter := createTestRankerAdapter(t)

		err1 := adapter.Close()
		err2 := adapter.Close()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})
}

func TestRankerConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := DefaultRankerConfig()

		err := config.Validate()

		assert.NoError(t, err)
	})

	t.Run("invalid negative vector weight", func(t *testing.T) {
		config := &RankerConfig{
			VectorWeight:  -0.1,
			KeywordWeight: 0.3,
		}

		err := config.Validate()

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("invalid vector weight over 1", func(t *testing.T) {
		config := &RankerConfig{
			VectorWeight:  1.5,
			KeywordWeight: 0.3,
		}

		err := config.Validate()

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("invalid negative keyword weight", func(t *testing.T) {
		config := &RankerConfig{
			VectorWeight:  0.7,
			KeywordWeight: -0.1,
		}

		err := config.Validate()

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("invalid keyword weight over 1", func(t *testing.T) {
		config := &RankerConfig{
			VectorWeight:  0.7,
			KeywordWeight: 1.5,
		}

		err := config.Validate()

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
}

func TestDefaultRankerConfig(t *testing.T) {
	config := DefaultRankerConfig()

	assert.True(t, config.EnableHybrid)
	assert.Equal(t, float32(0.7), config.VectorWeight)
	assert.Equal(t, float32(0.3), config.KeywordWeight)
}

func TestPipelineToRankingResults(t *testing.T) {
	t.Run("converts non-empty results", func(t *testing.T) {
		pipelineResults := []pipeline.Result{
			{
				ID:        "doc1",
				Content:   "content1",
				FilePath:  "/path1",
				StartLine: 1,
				EndLine:   10,
				Score:     0.9,
			},
			{
				ID:        "doc2",
				Content:   "content2",
				FilePath:  "/path2",
				StartLine: 5,
				EndLine:   15,
				Score:     0.8,
			},
		}

		rankingResults := pipelineToRankingResults(pipelineResults)

		require.Len(t, rankingResults, 2)
		assert.Equal(t, "doc1", rankingResults[0].ID)
		assert.Equal(t, "content1", rankingResults[0].Content)
		assert.Equal(t, float32(0.9), rankingResults[0].VectorScore)
		assert.Equal(t, "doc2", rankingResults[1].ID)
	})

	t.Run("returns nil for empty results", func(t *testing.T) {
		rankingResults := pipelineToRankingResults([]pipeline.Result{})

		assert.Nil(t, rankingResults)
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		rankingResults := pipelineToRankingResults(nil)

		assert.Nil(t, rankingResults)
	})
}

func TestScoredResultsToPipelineResults(t *testing.T) {
	t.Run("converts non-empty results", func(t *testing.T) {
		scoredResults := []ranking.ScoredResult{
			{
				ID:         "doc1",
				Content:    "content1",
				FilePath:   "/path1",
				StartLine:  1,
				EndLine:    10,
				FinalScore: 0.95,
			},
		}

		pipelineResults := scoredResultsToPipelineResults(scoredResults)

		require.Len(t, pipelineResults, 1)
		assert.Equal(t, "doc1", pipelineResults[0].ID)
		assert.Equal(t, "content1", pipelineResults[0].Content)
		assert.Equal(t, float32(0.95), pipelineResults[0].Score)
	})

	t.Run("returns nil for empty results", func(t *testing.T) {
		pipelineResults := scoredResultsToPipelineResults([]ranking.ScoredResult{})

		assert.Nil(t, pipelineResults)
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		pipelineResults := scoredResultsToPipelineResults(nil)

		assert.Nil(t, pipelineResults)
	})
}

// Helper functions

func createTestHybridRanker() *ranking.HybridRanker {
	config := ranking.Config{
		BM25K1:        1.2,
		BM25B:         0.75,
		VectorWeight:  0.7,
		KeywordWeight: 0.3,
		RerankTopK:    50,
		EnableRerank:  false,
		RRFK:          60,
	}
	return ranking.NewHybridRanker(config, nil)
}

func createTestRankerAdapter(t *testing.T) *RankerAdapter {
	t.Helper()
	hybridRanker := createTestHybridRanker()
	adapter, err := NewRankerAdapter(hybridRanker)
	require.NoError(t, err)
	return adapter
}

func createTestPipelineResults(count int) []pipeline.Result {
	results := make([]pipeline.Result, count)
	for i := 0; i < count; i++ {
		results[i] = pipeline.Result{
			ID:        "doc" + string(rune('0'+i)),
			Content:   "Test content for document " + string(rune('0'+i)),
			FilePath:  "/test/file" + string(rune('0'+i)) + ".go",
			StartLine: i * 10,
			EndLine:   (i + 1) * 10,
			Score:     float32(0.9 - float64(i)*0.1),
		}
	}
	return results
}

func createTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	return log
}
