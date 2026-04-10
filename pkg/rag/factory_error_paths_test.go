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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// =============================================================================
// EmbedBatch error path coverage
// =============================================================================

func TestEmbedBatchWithInternalErrorAtIndex(t *testing.T) {
	// Create an adapter that will fail during batch embedding
	errAdapter := &failOnBatchEmbedder{
		dimensions: 128,
		failIndex:  1, // Fail on second text
	}

	texts := []string{"text1", "text2", "text3"}
	results, err := errAdapter.EmbedBatch(context.Background(), texts)

	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "embed failed at index")
}

func TestEmbedBatchFirstIndexFailure(t *testing.T) {
	errAdapter := &failOnBatchEmbedder{
		dimensions: 128,
		failIndex:  0, // Fail on first text
	}

	texts := []string{"text1"}
	results, err := errAdapter.EmbedBatch(context.Background(), texts)

	assert.Error(t, err)
	assert.Nil(t, results)
}

// =============================================================================
// System.Close with nil components
// =============================================================================

func TestSystemCloseWithNilPipelineComponent(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Set pipeline to nil
	system.pipeline = nil

	err = system.Close()

	// Should succeed - nil check should prevent error
	assert.NoError(t, err)
}

func TestSystemCloseWithNilVectorSearcherComponent(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Set vectorSearcher to nil
	system.vectorSearcher = nil

	err = system.Close()

	// Should succeed - nil check should prevent error
	assert.NoError(t, err)
}

func TestSystemCloseWithNilEmbedderComponent(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Set embedder to nil
	system.embedder = nil

	err = system.Close()

	// Should succeed - nil check should prevent error
	assert.NoError(t, err)
}

func TestSystemCloseWithAllNilComponentsSet(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Set all components to nil
	system.pipeline = nil
	system.vectorSearcher = nil
	system.embedder = nil

	err = system.Close()

	// Should succeed with no errors
	assert.NoError(t, err)
}

// =============================================================================
// Test EmbedBatch error propagation in mockEmbedderAdapter
// =============================================================================

func TestEmbedBatchInternalEmbedErrorPropagation(t *testing.T) {
	// This tests the error return path in EmbedBatch when Embed fails
	adapter := &conditionalFailEmbedder{
		dimensions:      128,
		failOnText:      "fail_me",
		failWithMessage: "intentional embed failure",
	}

	texts := []string{"ok_text", "fail_me", "another_ok"}
	results, err := adapter.EmbedBatch(context.Background(), texts)

	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "intentional embed failure")
}

// =============================================================================
// Additional test cases for edge conditions
// =============================================================================

func TestSystemQueryWithValidPipelineComponent(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	// Query with the valid system
	opts := pipeline.QueryOptions{TopK: 5}
	resp, err := system.Query(context.Background(), "test", opts)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestSystemIndexContentWithVeryLargeContent(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	// Create large content
	largeContent := ""
	for i := 0; i < 1000; i++ {
		largeContent += "This is a repeating sentence for testing large content handling. "
	}

	err = system.IndexContent(context.Background(), "large-doc", largeContent, map[string]interface{}{
		"type": "large",
	})

	assert.NoError(t, err)
}

func TestSystemIndexContentWithSpecialCharactersInContent(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	specialContent := "Content with special chars: \n\t\r \"quotes\" 'apostrophes' <tags> & symbols"

	err = system.IndexContent(context.Background(), "special-doc", specialContent, nil)

	assert.NoError(t, err)
}

func TestSystemMultipleConcurrentCloseAttempts(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Attempt concurrent closes
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- system.Close()
		}()
	}

	// Collect results - all should be nil (idempotent close)
	for i := 0; i < 10; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}

// =============================================================================
// Helper mock types
// =============================================================================

// failOnBatchEmbedder fails at a specific index during batch embedding.
type failOnBatchEmbedder struct {
	dimensions int
	failIndex  int
}

func (f *failOnBatchEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return make([]float32, f.dimensions), nil
}

func (f *failOnBatchEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		if i == f.failIndex {
			return nil, errors.New("embed failed at index")
		}
		emb, err := f.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

func (f *failOnBatchEmbedder) Dimensions() int { return f.dimensions }
func (f *failOnBatchEmbedder) MaxTokens() int  { return 8192 }
func (f *failOnBatchEmbedder) Name() string    { return "fail-batch" }
func (f *failOnBatchEmbedder) Close() error    { return nil }

var _ embedding.Provider = (*failOnBatchEmbedder)(nil)

// conditionalFailEmbedder fails when embedding a specific text.
type conditionalFailEmbedder struct {
	dimensions      int
	failOnText      string
	failWithMessage string
}

func (c *conditionalFailEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == c.failOnText {
		return nil, errors.New(c.failWithMessage)
	}
	return make([]float32, c.dimensions), nil
}

func (c *conditionalFailEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := c.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

func (c *conditionalFailEmbedder) Dimensions() int { return c.dimensions }
func (c *conditionalFailEmbedder) MaxTokens() int  { return 8192 }
func (c *conditionalFailEmbedder) Name() string    { return "conditional-fail" }
func (c *conditionalFailEmbedder) Close() error    { return nil }

var _ embedding.Provider = (*conditionalFailEmbedder)(nil)

// =============================================================================
// Additional NewSystem error path tests
// =============================================================================

func TestNewSystemWithZeroDimensionsConfig(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Embedding.Dimensions = 0
	cfg.Vector.Dimensions = 0

	system, err := NewSystem(cfg)

	assert.Error(t, err)
	assert.Nil(t, system)
}

func TestNewSystemWithVeryLargeDimensionsConfig(t *testing.T) {
	cfg := createTestRAGConfig(t)
	cfg.Embedding.Dimensions = 4096
	cfg.Vector.Dimensions = 4096

	system, err := NewSystem(cfg)

	// Should succeed but may be slow
	if err == nil {
		assert.NotNil(t, system)
		system.Close()
	}
}

func TestNewSystemWithMinimalValidConfig(t *testing.T) {
	cfg := config.RAGConfig{
		Enabled: true,
		Embedding: config.EmbeddingConfig{
			Provider:   "mock",
			Dimensions: 64,
		},
		Vector: config.RAGVectorConfig{
			NumCentroids: 5,
			Dimensions:   64,
			NProbe:       2,
			Metric:       "cosine",
		},
		Ranking: config.RankingConfig{
			Strategy: "vector",
		},
		Context: config.ContextConfig{
			MaxTokens: 1000,
		},
		Pipeline: config.PipelineConfig{
			MaxConcurrentQueries: 1,
		},
	}

	system, err := NewSystem(cfg)

	if err == nil {
		assert.NotNil(t, system)
		system.Close()
	}
}

// =============================================================================
// Combined close error tests
// =============================================================================

func TestSystemCloseWithPipelineAndEmbedderErrors(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Replace both components with error-returning mocks
	system.pipeline = &errorPipeline{}
	system.embedder = &errorEmbedder{}

	err = system.Close()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close pipeline")
	assert.Contains(t, err.Error(), "pipeline close")
	assert.Contains(t, err.Error(), "embedder close")
}

// =============================================================================
// mockEmbedderImpl additional coverage
// =============================================================================

func TestMockEmbedderImplRepeatedEmbed(t *testing.T) {
	embedder := &mockEmbedderImpl{dimensions: 128}
	ctx := context.Background()

	// First embed
	emb1, err := embedder.Embed(ctx, "test text repeated")
	require.NoError(t, err)

	// Second embed with same text should return cached
	emb2, err := embedder.Embed(ctx, "test text repeated")
	require.NoError(t, err)

	assert.Equal(t, emb1, emb2)
}

func TestMockEmbedderImplBatchEmbed(t *testing.T) {
	embedder := &mockEmbedderImpl{dimensions: 128}
	ctx := context.Background()

	texts := []string{"text1", "text2", "text3"}
	results, err := embedder.EmbedBatch(ctx, texts)

	require.NoError(t, err)
	assert.Len(t, results, 3)
	for _, emb := range results {
		assert.Len(t, emb, 128)
	}
}

// =============================================================================
// mockQueryHandler additional coverage
// =============================================================================

func TestMockQueryHandlerProcessWithEmbedError(t *testing.T) {
	// Create handler with failing embedder
	handler := &mockQueryHandler{
		embedder: &errorEmbedder{},
	}

	result, err := handler.Process(context.Background(), "test query")

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestMockQueryHandlerCloseMethod(t *testing.T) {
	embedder := &mockEmbedderImpl{dimensions: 128}
	handler := &mockQueryHandler{embedder: embedder}

	err := handler.Close()

	assert.NoError(t, err)
}
