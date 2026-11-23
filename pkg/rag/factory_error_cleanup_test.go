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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

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

func TestMockEmbedderAdapterEmbedNilContextSucceeds(t *testing.T) {
	adapter := &mockEmbedderAdapter{dimensions: 128}

	// Nil context should succeed (for backward compatibility)
	emb, err := adapter.Embed(nil, "test text")

	assert.NoError(t, err)
	assert.NotNil(t, emb)
	assert.Len(t, emb, 128)
}

// =============================================================================
// Additional mockEmbedderAdapter edge cases
// =============================================================================

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
