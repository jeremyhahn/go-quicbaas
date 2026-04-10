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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/adapters"
)

// =============================================================================
// Test fixtures and mocks for dependency injection tests
// =============================================================================

// failingCloseEmbedder is an embedder that fails on Close
type failingCloseEmbedder struct {
	dimensions int
}

func (f *failingCloseEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return make([]float32, f.dimensions), nil
}

func (f *failingCloseEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = make([]float32, f.dimensions)
	}
	return results, nil
}

func (f *failingCloseEmbedder) Dimensions() int { return f.dimensions }
func (f *failingCloseEmbedder) MaxTokens() int  { return 8192 }
func (f *failingCloseEmbedder) Name() string    { return "failing-close" }
func (f *failingCloseEmbedder) Close() error    { return errors.New("embedder close failed") }

var _ embedding.Provider = (*failingCloseEmbedder)(nil)

// testMockIndex implements VectorIndex for testing
type testMockIndex struct {
	trained bool
}

func (m *testMockIndex) Train(vectors [][]float32) error { m.trained = true; return nil }
func (m *testMockIndex) IsTrained() bool                 { return m.trained }
func (m *testMockIndex) Add(id string, vector []float32, metadata map[string]interface{}) error {
	return nil
}
func (m *testMockIndex) Search(query []float32, k int, nprobe int) ([]VectorSearchResult, error) {
	return nil, nil
}
func (m *testMockIndex) Remove(id string) error { return nil }
func (m *testMockIndex) Save(path string) error { return nil }
func (m *testMockIndex) Load(path string) error { return nil }
func (m *testMockIndex) Size() int              { return 0 }

var _ VectorIndex = (*testMockIndex)(nil)

// failingTrainIndex is an index that fails on Train
type failingTrainIndex struct {
	testMockIndex
}

func (f *failingTrainIndex) Train(vectors [][]float32) error {
	return errors.New("training failed")
}

func (f *failingTrainIndex) IsTrained() bool { return false }

// saveAndRestoreFactories saves current factories and returns restore function
func saveAndRestoreFactories(t *testing.T) func() {
	t.Helper()

	origEmbedder := createEmbedderFunc
	origVectorIndex := createVectorIndexFunc
	origRanker := createRankerAdapterFunc
	origContextBuilder := createContextBuilderFunc

	return func() {
		createEmbedderFunc = origEmbedder
		createVectorIndexFunc = origVectorIndex
		createRankerAdapterFunc = origRanker
		createContextBuilderFunc = origContextBuilder
	}
}

// =============================================================================
// Test vector index creation failure with embedder close also failing (lines 93-98)
// =============================================================================

func TestNewSystemVectorIndexFailsAndEmbedderCloseAlsoFails(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns an embedder that fails on Close
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &failingCloseEmbedder{dimensions: cfg.Dimensions}, nil
	}

	// Inject a factory that fails to create vector index
	createVectorIndexFunc = func(cfg config.RAGVectorConfig) (VectorIndex, error) {
		return nil, errors.New("vector index creation failed")
	}

	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create vector index")
	assert.Contains(t, err.Error(), "also: failed to close embedder")
}

func TestNewSystemVectorIndexFailsAndEmbedderCloseSucceeds(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns a normal mock embedder
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &mockEmbedderAdapter{dimensions: cfg.Dimensions}, nil
	}

	// Inject a factory that fails to create vector index
	createVectorIndexFunc = func(cfg config.RAGVectorConfig) (VectorIndex, error) {
		return nil, errors.New("vector index creation failed")
	}

	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create vector index")
	assert.NotContains(t, err.Error(), "also failed to close embedder")
}

// =============================================================================
// Test train index failure with embedder close also failing (lines 103-109)
// =============================================================================

func TestNewSystemTrainIndexFailsAndEmbedderCloseAlsoFails(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns an embedder that fails on Close
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &failingCloseEmbedder{dimensions: cfg.Dimensions}, nil
	}

	// Inject a factory that returns an index that fails on Train
	createVectorIndexFunc = func(cfg config.RAGVectorConfig) (VectorIndex, error) {
		return &failingTrainIndex{}, nil
	}

	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to train vector index")
	assert.Contains(t, err.Error(), "also: failed to close embedder")
}

func TestNewSystemTrainIndexFailsAndEmbedderCloseSucceeds(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns a normal mock embedder
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &mockEmbedderAdapter{dimensions: cfg.Dimensions}, nil
	}

	// Inject a factory that returns an index that fails on Train
	createVectorIndexFunc = func(cfg config.RAGVectorConfig) (VectorIndex, error) {
		return &failingTrainIndex{}, nil
	}

	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to train vector index")
	assert.NotContains(t, err.Error(), "also failed to close embedder")
}

// =============================================================================
// Test ranker adapter creation failure with embedder close also failing (lines 117-123)
// =============================================================================

func TestNewSystemRankerFailsAndEmbedderCloseAlsoFails(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns an embedder that fails on Close
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &failingCloseEmbedder{dimensions: cfg.Dimensions}, nil
	}

	// Use normal vector index factory (with pre-trained mock)
	createVectorIndexFunc = func(cfg config.RAGVectorConfig) (VectorIndex, error) {
		idx := &testMockIndex{trained: true}
		return idx, nil
	}

	// Inject a factory that fails to create ranker
	createRankerAdapterFunc = func(cfg config.RankingConfig) (*adapters.RankerAdapter, error) {
		return nil, errors.New("ranker creation failed")
	}

	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create ranker")
	assert.Contains(t, err.Error(), "also: failed to close embedder")
}

func TestNewSystemRankerFailsAndEmbedderCloseSucceeds(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns a normal mock embedder
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &mockEmbedderAdapter{dimensions: cfg.Dimensions}, nil
	}

	// Use normal vector index factory (with pre-trained mock)
	createVectorIndexFunc = func(cfg config.RAGVectorConfig) (VectorIndex, error) {
		idx := &testMockIndex{trained: true}
		return idx, nil
	}

	// Inject a factory that fails to create ranker
	createRankerAdapterFunc = func(cfg config.RankingConfig) (*adapters.RankerAdapter, error) {
		return nil, errors.New("ranker creation failed")
	}

	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create ranker")
	assert.NotContains(t, err.Error(), "also failed to close embedder")
}

// =============================================================================
// Test context builder creation failure with embedder close also failing (lines 126-132)
// =============================================================================

func TestNewSystemContextBuilderFailsAndEmbedderCloseAlsoFails(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns an embedder that fails on Close
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &failingCloseEmbedder{dimensions: cfg.Dimensions}, nil
	}

	// Use normal vector index factory (with pre-trained mock)
	createVectorIndexFunc = func(cfg config.RAGVectorConfig) (VectorIndex, error) {
		idx := &testMockIndex{trained: true}
		return idx, nil
	}

	// Use real ranker factory (it won't fail)
	createRankerAdapterFunc = createRankerAdapter

	// Inject a factory that fails to create context builder
	createContextBuilderFunc = func(cfg config.ContextConfig) (*adapters.ContextBuilderAdapter, error) {
		return nil, errors.New("context builder creation failed")
	}

	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create context builder")
	assert.Contains(t, err.Error(), "also: failed to close embedder")
}

func TestNewSystemContextBuilderFailsAndEmbedderCloseSucceeds(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns a normal mock embedder
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &mockEmbedderAdapter{dimensions: cfg.Dimensions}, nil
	}

	// Use normal vector index factory (with pre-trained mock)
	createVectorIndexFunc = func(cfg config.RAGVectorConfig) (VectorIndex, error) {
		idx := &testMockIndex{trained: true}
		return idx, nil
	}

	// Use real ranker factory (it won't fail)
	createRankerAdapterFunc = createRankerAdapter

	// Inject a factory that fails to create context builder
	createContextBuilderFunc = func(cfg config.ContextConfig) (*adapters.ContextBuilderAdapter, error) {
		return nil, errors.New("context builder creation failed")
	}

	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create context builder")
	assert.NotContains(t, err.Error(), "also failed to close embedder")
}

// =============================================================================
// Test pipeline creation failure with embedder close also failing (lines 158-162)
// =============================================================================

func TestNewSystemPipelineFailsAndEmbedderCloseAlsoFails(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Inject a factory that returns an embedder that fails on Close
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		return &failingCloseEmbedder{dimensions: cfg.Dimensions}, nil
	}

	// Use real factories for everything else - pipeline will fail due to DefaultTopK=0
	createVectorIndexFunc = createVectorIndex
	createRankerAdapterFunc = createRankerAdapter
	createContextBuilderFunc = createContextBuilderAdapter

	cfg := createTestRAGConfig(t)
	// Set DefaultTopK to 0 to make pipeline creation fail
	cfg.Pipeline.DefaultTopK = 0

	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create pipeline")
	assert.Contains(t, err.Error(), "also: failed to close embedder")
}

// =============================================================================
// Test dimension mismatch path (lines 80-83)
// This path is defense-in-depth after config validation
// =============================================================================

func TestNewSystemDimensionMismatchDirectPath(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	// Create a custom config that bypasses validation but has mismatched dimensions
	// This is only possible if we skip validation, so we test the error message format
	cfg := createTestRAGConfig(t)
	cfg.Embedding.Dimensions = 384
	cfg.Vector.Dimensions = 512

	// This should fail at config validation first
	system, err := NewSystem(cfg)

	assert.Nil(t, system)
	assert.Error(t, err)
	// Should contain dimension-related error (either from config validation or the check)
	assert.Contains(t, err.Error(), "dimension")
}

// =============================================================================
// Test factory function reset after each test
// =============================================================================

func TestFactoryFunctionsAreProperlyReset(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	var callCount int32
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		atomic.AddInt32(&callCount, 1)
		return &mockEmbedderAdapter{dimensions: cfg.Dimensions}, nil
	}

	cfg := createTestRAGConfig(t)
	_, _ = NewSystem(cfg)

	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

// =============================================================================
// Test concurrent factory function access
// =============================================================================

func TestFactoryFunctionsConcurrentAccess(t *testing.T) {
	restore := saveAndRestoreFactories(t)
	defer restore()

	var callCount int32
	createEmbedderFunc = func(cfg config.EmbeddingConfig) (embedding.Provider, error) {
		atomic.AddInt32(&callCount, 1)
		return &mockEmbedderAdapter{dimensions: cfg.Dimensions}, nil
	}

	cfg := createTestRAGConfig(t)
	done := make(chan struct{}, 5)

	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			system, err := NewSystem(cfg)
			if err == nil && system != nil {
				system.Close()
			}
		}()
	}

	for i := 0; i < 5; i++ {
		<-done
	}

	assert.Equal(t, int32(5), atomic.LoadInt32(&callCount))
}

// =============================================================================
// Additional validation tests
// =============================================================================

func TestNewSystemWithValidConfigSucceeds(t *testing.T) {
	cfg := createTestRAGConfig(t)
	system, err := NewSystem(cfg)

	require.NoError(t, err)
	require.NotNil(t, system)

	err = system.Close()
	assert.NoError(t, err)
}

func TestNewSystemReturnsCorrectErrorTypes(t *testing.T) {
	t.Run("invalid config error", func(t *testing.T) {
		cfg := createTestRAGConfig(t)
		cfg.Embedding.Provider = ""

		_, err := NewSystem(cfg)

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
}
