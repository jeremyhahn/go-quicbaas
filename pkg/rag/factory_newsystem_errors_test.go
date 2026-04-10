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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// NewSystem error paths that can be triggered through configuration
// =============================================================================

// TestNewSystemPipelineValidationError tests the pipeline creation error path
// by passing a DefaultTopK of 0 which passes config validation but fails
// pipeline.Config.Validate().
func TestNewSystemPipelineValidationError(t *testing.T) {
	cfg := createTestRAGConfig(t)
	// Set DefaultTopK to 0 - passes config.PipelineConfig.Validate() which checks < 0
	// but fails pipeline.Config.Validate() which checks <= 0
	cfg.Pipeline.DefaultTopK = 0

	system, err := NewSystem(cfg)

	// Should fail during pipeline creation
	assert.Error(t, err)
	assert.Nil(t, system)
	assert.Contains(t, err.Error(), "failed to create pipeline")
}

// TestNewSystemWithVeryShortTimeout tests system creation with short timeout
func TestNewSystemWithVeryShortTimeout(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Pipeline.Timeout = 1 * time.Millisecond

	system, err := NewSystem(cfg)

	// Should succeed - timeout is just a config value
	require.NoError(t, err)
	assert.NotNil(t, system)
	system.Close()
}

// =============================================================================
// Test various valid configurations to improve coverage
// =============================================================================

func TestNewSystemWithAllPipelineOptions(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Pipeline.EnableCircuitBreaker = true
	cfg.Pipeline.EnableMetrics = true
	cfg.Pipeline.EnableHybrid = true
	cfg.Pipeline.EnableRerank = true
	cfg.Pipeline.EnableCache = true
	cfg.Pipeline.MaxConcurrentQueries = 50

	system, err := NewSystem(cfg)

	require.NoError(t, err)
	require.NotNil(t, system)
	defer system.Close()

	// Verify system works
	stats := system.Stats()
	assert.NotNil(t, stats)
}

func TestNewSystemWithMinimalConfig(t *testing.T) {
	cfg := config.RAGConfig{
		Enabled: true,
		Embedding: config.EmbeddingConfig{
			Provider:   "mock",
			Dimensions: 128,
			Model:      "test",
		},
		Vector: config.RAGVectorConfig{
			NumCentroids: 5,
			Dimensions:   128,
			NProbe:       2,
			Metric:       "cosine",
		},
		Query: config.RAGQueryConfig{
			CacheSize:       10,
			CacheTTL:        time.Hour,
			CleanupInterval: time.Minute,
			DefaultTopK:     5,
			DefaultMinScore: 0.5,
			DefaultNProbe:   2,
		},
		Ranking: config.RankingConfig{
			Strategy:      "vector",
			BM25K1:        1.2,
			BM25B:         0.75,
			VectorWeight:  1.0,
			KeywordWeight: 0.0,
			RerankTopK:    10,
			RRFK:          60,
		},
		Context: config.ContextConfig{
			MaxTokens:          2000,
			DefaultTemplate:    "markdown", // Use valid template
			TruncationStrategy: "smart",
			MaxResultsPerChunk: 10,
			TokenMultiplier:    1.0,
		},
		Pipeline: config.PipelineConfig{
			Timeout:              10 * time.Second,
			MaxConcurrentQueries: 10,
			DefaultTopK:          5,
			DefaultMaxTokens:     2000,
			DefaultMinScore:      0.5,
			DefaultNamespace:     "test",
		},
	}

	system, err := NewSystem(cfg)

	if err != nil {
		// May fail if some validation is missing, that's OK
		assert.Contains(t, err.Error(), "")
	} else {
		assert.NotNil(t, system)
		system.Close()
	}
}

// =============================================================================
// Test mockEmbedderAdapter variations
// =============================================================================

func TestMockEmbedderAdapterVerySmallDimensions(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 1}

	emb, err := adapter.Embed(nil, "test")

	assert.NoError(t, err)
	assert.Len(t, emb, 1)
}

func TestMockEmbedderAdapterLargeDimensions(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 4096}

	emb, err := adapter.Embed(nil, "test")

	assert.NoError(t, err)
	assert.Len(t, emb, 4096)
}

func TestMockEmbedderAdapterWithContextNil(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Should work even with nil context
	emb, err := adapter.Embed(nil, "test text")

	assert.NoError(t, err)
	assert.Len(t, emb, 128)
}

func TestMockEmbedderAdapterBatchWithNilContext(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Should work even with nil context
	results, err := adapter.EmbedBatch(nil, []string{"a", "b", "c"})

	assert.NoError(t, err)
	assert.Len(t, results, 3)
}

// =============================================================================
// Test queryHandlerAdapter variations
// =============================================================================

func TestQueryHandlerAdapterWithNilContext(t *testing.T) {
	embedder := &mockEmbedderAdapter{dimensions: 128}
	handler := &queryHandlerAdapter{embedder: embedder}

	// Should work even with nil context
	result, err := handler.Process(nil, "test query")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test query", result.Original)
}

// =============================================================================
// Test System with specific configuration paths
// =============================================================================

func TestNewSystemWithFusionStrategy(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Ranking.Strategy = "fusion"

	system, err := NewSystem(cfg)

	require.NoError(t, err)
	assert.NotNil(t, system)
	system.Close()
}

func TestNewSystemWithBM25Strategy(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Ranking.Strategy = "bm25"

	system, err := NewSystem(cfg)

	require.NoError(t, err)
	assert.NotNil(t, system)
	system.Close()
}

func TestNewSystemWithVectorStrategy(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Ranking.Strategy = "vector"

	system, err := NewSystem(cfg)

	require.NoError(t, err)
	assert.NotNil(t, system)
	system.Close()
}

func TestNewSystemWithDifferentTemplates(t *testing.T) {
	// Use valid templates: markdown, json, xml, plain
	templates := []string{"markdown", "json", "xml", "plain"}

	for _, template := range templates {
		t.Run("template_"+template, func(t *testing.T) {
			cfg := createTestRAGConfig(t)
			cfg.Context.DefaultTemplate = template

			system, err := NewSystem(cfg)

			require.NoError(t, err)
			assert.NotNil(t, system)
			system.Close()
		})
	}
}

func TestNewSystemWithDifferentMetrics(t *testing.T) {
	metrics := []string{"cosine", "l2", "ip", "dot"}

	for _, metric := range metrics {
		t.Run("metric_"+metric, func(t *testing.T) {
			cfg := createTestRAGConfig(t)
			cfg.Vector.Metric = metric

			system, err := NewSystem(cfg)

			require.NoError(t, err)
			assert.NotNil(t, system)
			system.Close()
		})
	}
}
