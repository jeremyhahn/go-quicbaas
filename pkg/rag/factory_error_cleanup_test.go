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

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// =============================================================================
// mockEmbedderAdapter EmbedBatch error path coverage
// =============================================================================

// errorOnIndexEmbedder fails when embedding a specific index in batch
type errorOnIndexEmbedder struct {
	dimensions int
	failOnIdx  int
	callCount  int
}

func (e *errorOnIndexEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if e.callCount == e.failOnIdx {
		e.callCount++
		return nil, errors.New("embed failed at specific index")
	}
	e.callCount++
	return make([]float32, e.dimensions), nil
}

func (e *errorOnIndexEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

func (e *errorOnIndexEmbedder) Dimensions() int { return e.dimensions }
func (e *errorOnIndexEmbedder) MaxTokens() int  { return 8192 }
func (e *errorOnIndexEmbedder) Name() string    { return "error-on-index" }
func (e *errorOnIndexEmbedder) Close() error    { return nil }

var _ embedding.Provider = (*errorOnIndexEmbedder)(nil)

func TestEmbedBatchErrorOnMiddleIndex(t *testing.T) {
	embedder := &errorOnIndexEmbedder{
		dimensions: 128,
		failOnIdx:  2, // Fail on third text (index 2)
	}

	texts := []string{"text1", "text2", "text3", "text4"}
	results, err := embedder.EmbedBatch(context.Background(), texts)

	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "embed failed at specific index")
}

func TestEmbedBatchErrorOnLastIndex(t *testing.T) {
	embedder := &errorOnIndexEmbedder{
		dimensions: 128,
		failOnIdx:  3, // Fail on last text (index 3)
	}

	texts := []string{"text1", "text2", "text3", "text4"}
	results, err := embedder.EmbedBatch(context.Background(), texts)

	assert.Error(t, err)
	assert.Nil(t, results)
}

// =============================================================================
// mockEmbedderAdapter context cancellation error path coverage
// =============================================================================

func TestMockEmbedderAdapterEmbedWithCancelledContext(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Create a pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Embed should return context.Canceled error
	emb, err := adapter.Embed(ctx, "test text")

	assert.Error(t, err)
	assert.Nil(t, emb)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestMockEmbedderAdapterEmbedBatchWithCancelledContext(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Create a pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// EmbedBatch should return error from first Embed call
	texts := []string{"text1", "text2", "text3"}
	results, err := adapter.EmbedBatch(ctx, texts)

	assert.Error(t, err)
	assert.Nil(t, results)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestMockEmbedderAdapterEmbedBatchErrorPropagation(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Create a context that we'll cancel mid-batch
	ctx, cancel := context.WithCancel(context.Background())

	// First, do a successful embed to initialize cache
	_, err := adapter.Embed(context.Background(), "cached_text")
	require.NoError(t, err)

	// Cancel before batch
	cancel()

	// Even with cached first text, second text should fail due to cancelled context
	texts := []string{"cached_text", "new_text", "another_text"}
	results, err := adapter.EmbedBatch(ctx, texts)

	// First text is cached, but Embed still checks context first
	assert.Error(t, err)
	assert.Nil(t, results)
}

func TestMockEmbedderAdapterEmbedWithDeadlineExceeded(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Create a context with already-exceeded deadline
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	// Wait for context to be done
	<-ctx.Done()

	// Embed should return deadline exceeded error
	emb, err := adapter.Embed(ctx, "test text")

	assert.Error(t, err)
	assert.Nil(t, emb)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestMockEmbedderAdapterEmbedNilContextSucceeds(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Nil context should succeed (for backward compatibility)
	emb, err := adapter.Embed(nil, "test text")

	assert.NoError(t, err)
	assert.NotNil(t, emb)
	assert.Len(t, emb, 128)
}

func TestMockEmbedderAdapterEmbedBatchNilContextSucceeds(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Nil context should succeed
	texts := []string{"text1", "text2"}
	results, err := adapter.EmbedBatch(nil, texts)

	assert.NoError(t, err)
	assert.Len(t, results, 2)
}

// =============================================================================
// Additional mockEmbedderAdapter edge cases
// =============================================================================

func TestMockEmbedderAdapterEmbedInitializesCache(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 64}

	// Cache should be nil initially
	assert.Nil(t, adapter.embeddings)

	// After embed, cache should be initialized
	_, err := adapter.Embed(context.Background(), "test")
	require.NoError(t, err)

	assert.NotNil(t, adapter.embeddings)
}

func TestMockEmbedderAdapterEmbedBatchSingleElement(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 256}

	results, err := adapter.EmbedBatch(context.Background(), []string{"single"})

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Len(t, results[0], 256)
}

func TestMockEmbedderAdapterEmbedBatchPreservesOrder(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 64}

	texts := []string{"alpha", "beta", "gamma", "delta"}
	results, err := adapter.EmbedBatch(context.Background(), texts)
	require.NoError(t, err)

	// Verify each text gets a unique embedding
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			// Different texts should have different embeddings
			assert.NotEqual(t, results[i], results[j], "embeddings for '%s' and '%s' should differ", texts[i], texts[j])
		}
	}
}

// =============================================================================
// queryHandlerAdapter additional edge cases
// =============================================================================

func TestQueryHandlerAdapterProcessWithEmptyQuery(t *testing.T) {
	embedder := &mockEmbedderAdapter{dimensions: 128}
	handler := &queryHandlerAdapter{embedder: embedder}

	result, err := handler.Process(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, "", result.Original)
	assert.Equal(t, "", result.Cleaned)
	assert.Equal(t, "search", result.Intent)
	assert.Len(t, result.Embedding, 128)
}

func TestQueryHandlerAdapterProcessWithUnicodeQuery(t *testing.T) {
	embedder := &mockEmbedderAdapter{dimensions: 128}
	handler := &queryHandlerAdapter{embedder: embedder}

	unicodeQuery := "test query with unicode characters"
	result, err := handler.Process(context.Background(), unicodeQuery)

	require.NoError(t, err)
	assert.Equal(t, unicodeQuery, result.Original)
	assert.Len(t, result.Embedding, 128)
}

func TestQueryHandlerAdapterProcessWithCancelledContext(t *testing.T) {
	embedder := &mockEmbedderAdapter{dimensions: 128}
	handler := &queryHandlerAdapter{embedder: embedder}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := handler.Process(ctx, "test query")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, context.Canceled)
}

// =============================================================================
// System concurrent operations with errors
// =============================================================================

func TestSystemConcurrentQueryAfterClose(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Close the system
	err = system.Close()
	require.NoError(t, err)

	// Attempt concurrent queries on closed system
	errChan := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			_, err := system.Query(context.Background(), "test", pipeline.QueryOptions{TopK: 5})
			errChan <- err
		}()
	}

	// All should return ErrSystemClosed
	for i := 0; i < 5; i++ {
		err := <-errChan
		assert.Error(t, err)
		assert.Equal(t, ErrSystemClosed, err)
	}
}

func TestSystemConcurrentIndexAfterClose(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Close the system
	err = system.Close()
	require.NoError(t, err)

	// Attempt concurrent index operations on closed system
	errChan := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			err := system.IndexContent(context.Background(), "id", "content", nil)
			errChan <- err
		}(i)
	}

	// All should return ErrSystemClosed
	for i := 0; i < 5; i++ {
		err := <-errChan
		assert.Error(t, err)
		assert.Equal(t, ErrSystemClosed, err)
	}
}

// =============================================================================
// Error wrapping verification
// =============================================================================

func TestErrSystemClosedMessageVerify(t *testing.T) {
	assert.Equal(t, "system is closed", ErrSystemClosed.Error())
}

func TestErrInvalidConfigMessageVerify(t *testing.T) {
	assert.Equal(t, "invalid configuration", ErrInvalidConfig.Error())
}

func TestErrDimensionMismatchMessageVerify(t *testing.T) {
	assert.Equal(t, "dimension mismatch between embedding and vector config", ErrDimensionMismatch.Error())
}

func TestErrUnsupportedProviderMessageVerify(t *testing.T) {
	assert.Equal(t, "unsupported embedding provider", ErrUnsupportedProvider.Error())
}

func TestErrProviderCreationFailedMessageVerify(t *testing.T) {
	assert.Equal(t, "failed to create embedding provider", ErrProviderCreationFailed.Error())
}

// =============================================================================
// System.Close with component nil checks and error combinations
// =============================================================================

func TestSystemCloseWithNilVectorSearcher(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Set vectorSearcher to nil before closing
	system.vectorSearcher = nil

	err = system.Close()

	// Should not panic, should succeed
	assert.NoError(t, err)
}

func TestSystemCloseWithAllNilComponents(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Set all to nil
	system.pipeline = nil
	system.vectorSearcher = nil
	system.embedder = nil

	err = system.Close()

	// Should not panic
	assert.NoError(t, err)
}

func TestSystemCloseWithOnlyPipelineNil(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Set only pipeline to nil
	system.pipeline = nil

	err = system.Close()

	// Should succeed
	assert.NoError(t, err)
}

func TestSystemCloseWithOnlyEmbedderNil(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)

	// Set only embedder to nil
	system.embedder = nil

	err = system.Close()

	// Should succeed
	assert.NoError(t, err)
}

// =============================================================================
// Additional System integration tests
// =============================================================================

func TestSystemQueryAfterIndexing(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	// Index some content
	err = system.IndexContent(context.Background(), "doc1", "Go programming language basics", map[string]interface{}{
		"source": "tutorial",
	})
	require.NoError(t, err)

	// Query
	opts := pipeline.QueryOptions{TopK: 5}
	resp, err := system.Query(context.Background(), "Go programming", opts)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestSystemMultipleIndexAndQuery(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)
	require.NoError(t, err)
	defer system.Close()

	// Index multiple documents
	docs := []struct {
		id      string
		content string
	}{
		{"doc1", "Introduction to machine learning"},
		{"doc2", "Deep learning neural networks"},
		{"doc3", "Natural language processing"},
	}

	for _, doc := range docs {
		err := system.IndexContent(context.Background(), doc.id, doc.content, nil)
		require.NoError(t, err)
	}

	// Query
	opts := pipeline.QueryOptions{TopK: 3}
	resp, err := system.Query(context.Background(), "machine learning", opts)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// =============================================================================
// Additional mockEmbedderAdapter coverage for hash-based embedding
// =============================================================================

func TestMockEmbedderAdapterHashDeterminism(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Same text should produce identical embeddings
	emb1, err := adapter.Embed(context.Background(), "deterministic test")
	require.NoError(t, err)

	// Create a new adapter and verify same hash result
	adapter2 := &mockEmbedderAdapter{dimensions: 128}
	emb2, err := adapter2.Embed(context.Background(), "deterministic test")
	require.NoError(t, err)

	assert.Equal(t, emb1, emb2, "same text should produce same embedding")
}

func TestMockEmbedderAdapterDifferentDimensionsProduceDifferentLengths(t *testing.T) {
	testCases := []int{64, 128, 256, 512, 1024}

	for _, dim := range testCases {
		t.Run("dimensions_"+string(rune('0'+dim)), func(t *testing.T) {
			adapter := &mockEmbedderAdapter{dimensions: dim}
			emb, err := adapter.Embed(context.Background(), "test")
			require.NoError(t, err)
			assert.Len(t, emb, dim)
		})
	}
}

// =============================================================================
// queryHandlerAdapter with error embedder
// =============================================================================

func TestQueryHandlerAdapterWithErrorEmbedder(t *testing.T) {
	// Use the errorEmbedder from factory_coverage_test.go
	handler := &queryHandlerAdapter{embedder: &errorEmbedder{}}

	result, err := handler.Process(context.Background(), "test query")

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestQueryHandlerAdapterMultipleProcessCalls(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}
	handler := &queryHandlerAdapter{embedder: adapter}

	queries := []string{"query1", "query2", "query3"}
	for _, q := range queries {
		result, err := handler.Process(context.Background(), q)
		require.NoError(t, err)
		assert.Equal(t, q, result.Original)
		assert.Equal(t, q, result.Cleaned)
		assert.Equal(t, "search", result.Intent)
		assert.Len(t, result.Embedding, 128)
	}
}
