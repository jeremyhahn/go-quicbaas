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

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/adapters"
	ragcontext "github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ranking"
)

// Common errors.
var (
	ErrSystemClosed           = errors.New("system is closed")
	ErrInvalidConfig          = errors.New("invalid configuration")
	ErrDimensionMismatch      = errors.New("dimension mismatch between embedding and vector config")
	ErrUnsupportedProvider    = errors.New("unsupported embedding provider")
	ErrProviderCreationFailed = errors.New("failed to create embedding provider")
)

// SentinelCauseError wraps a sentinel error with a cause using multi-unwrap.
type SentinelCauseError struct {
	Sentinel error
	Cause    error
}

// Error implements the error interface.
func (e *SentinelCauseError) Error() string {
	return e.Sentinel.Error() + ": " + e.Cause.Error()
}

// Unwrap returns both errors for multi-unwrap support (Go 1.20+).
func (e *SentinelCauseError) Unwrap() []error {
	return []error{e.Sentinel, e.Cause}
}

// DimensionMismatchError indicates embedding and vector config dimensions don't match.
type DimensionMismatchError struct {
	EmbeddingDim int
	VectorDim    int
}

// Error implements the error interface.
func (e *DimensionMismatchError) Error() string {
	return "dimension mismatch between embedding and vector config"
}

// ComponentError wraps an error during component creation or cleanup.
type ComponentError struct {
	Component string
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *ComponentError) Error() string {
	return "failed to " + e.Operation + " " + e.Component + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ComponentError) Unwrap() error {
	return e.Cause
}

// MultiComponentError wraps a primary error with an additional cleanup error.
type MultiComponentError struct {
	Primary error
	Cleanup error
}

// Error implements the error interface.
func (e *MultiComponentError) Error() string {
	return e.Primary.Error() + " (also: " + e.Cleanup.Error() + ")"
}

// Unwrap returns both errors for multi-unwrap support (Go 1.20+).
func (e *MultiComponentError) Unwrap() []error {
	return []error{e.Primary, e.Cleanup}
}

// UnsupportedProviderError indicates the specified embedding provider is not supported.
type UnsupportedProviderError struct {
	Provider string
}

// Error implements the error interface.
func (e *UnsupportedProviderError) Error() string {
	return "unsupported embedding provider: '" + e.Provider + "' - supported providers: mock, ollama, openai, voyage, lmstudio"
}

// Factory function types for dependency injection in tests.
type (
	embedderFactory    func(cfg config.EmbeddingConfig) (embedding.Provider, error)
	vectorIndexFactory func(cfg config.RAGVectorConfig) (VectorIndex, error)
	rankerFactory      func(cfg config.RankingConfig) (*adapters.RankerAdapter, error)
	contextBuilderFac  func(cfg config.ContextConfig) (*adapters.ContextBuilderAdapter, error)
)

// Package-level factory functions - can be replaced in tests.
var (
	createEmbedderFunc       embedderFactory    = createEmbedder
	createVectorIndexFunc    vectorIndexFactory = createVectorIndex
	createRankerAdapterFunc  rankerFactory      = createRankerAdapter
	createContextBuilderFunc contextBuilderFac  = createContextBuilderAdapter
)

// System represents a complete RAG system.
type System struct {
	pipeline       pipeline.Pipeline
	vectorIndex    VectorIndex
	vectorSearcher *RAGSearcher
	embedder       embedding.Provider
	config         config.RAGConfig

	mu     sync.RWMutex
	closed bool
}

// NewSystem creates a complete RAG system from configuration.
// This is the main entry point for creating a RAG system with real providers.
func NewSystem(cfg config.RAGConfig) (*System, error) {
	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, &SentinelCauseError{Sentinel: ErrInvalidConfig, Cause: err}
	}

	// Validate dimension consistency (defense in depth - config validation also checks this)
	if cfg.Embedding.Dimensions != cfg.Vector.Dimensions {
		return nil, &DimensionMismatchError{
			EmbeddingDim: cfg.Embedding.Dimensions,
			VectorDim:    cfg.Vector.Dimensions,
		}
	}

	// 1. Create embedding provider
	embedder, err := createEmbedderFunc(cfg.Embedding)
	if err != nil {
		return nil, &ComponentError{Component: "embedder", Operation: "create", Cause: err}
	}

	// 2. Create and train vector index
	vectorIndex, err := createVectorIndexFunc(cfg.Vector)
	if err != nil {
		closeErr := embedder.Close()
		if closeErr != nil {
			return nil, &MultiComponentError{
				Primary: &ComponentError{Component: "vector index", Operation: "create", Cause: err},
				Cleanup: &ComponentError{Component: "embedder", Operation: "close", Cause: closeErr},
			}
		}
		return nil, &ComponentError{Component: "vector index", Operation: "create", Cause: err}
	}

	// Train index with initial vectors if needed
	if !vectorIndex.IsTrained() {
		if err := trainIndex(vectorIndex, cfg.Vector.Dimensions); err != nil {
			closeErr := embedder.Close()
			if closeErr != nil {
				return nil, &MultiComponentError{
					Primary: &ComponentError{Component: "vector index", Operation: "train", Cause: err},
					Cleanup: &ComponentError{Component: "embedder", Operation: "close", Cause: closeErr},
				}
			}
			return nil, &ComponentError{Component: "vector index", Operation: "train", Cause: err}
		}
	}

	// 3. Create vector searcher (adapter connecting index + embedder)
	vectorSearcher := NewRAGSearcher(vectorIndex, embedder)

	// 4. Create real ranker and context builder adapters
	rankerAdapter, err := createRankerAdapterFunc(cfg.Ranking)
	if err != nil {
		closeErr := embedder.Close()
		if closeErr != nil {
			return nil, &MultiComponentError{
				Primary: &ComponentError{Component: "ranker", Operation: "create", Cause: err},
				Cleanup: &ComponentError{Component: "embedder", Operation: "close", Cause: closeErr},
			}
		}
		return nil, &ComponentError{Component: "ranker", Operation: "create", Cause: err}
	}

	contextBuilderAdapter, err := createContextBuilderFunc(cfg.Context)
	if err != nil {
		closeErr := embedder.Close()
		if closeErr != nil {
			return nil, &MultiComponentError{
				Primary: &ComponentError{Component: "context builder", Operation: "create", Cause: err},
				Cleanup: &ComponentError{Component: "embedder", Operation: "close", Cause: closeErr},
			}
		}
		return nil, &ComponentError{Component: "context builder", Operation: "create", Cause: err}
	}

	// 5. Create pipeline with real components
	pipelineCfg := pipeline.DefaultConfig()
	pipelineCfg.EnableCircuitBreaker = cfg.Pipeline.EnableCircuitBreaker
	pipelineCfg.EnableMetrics = cfg.Pipeline.EnableMetrics
	pipelineCfg.MaxConcurrentQueries = cfg.Pipeline.MaxConcurrentQueries
	pipelineCfg.QueryTimeout = cfg.Pipeline.Timeout
	pipelineCfg.DefaultTopK = cfg.Pipeline.DefaultTopK
	pipelineCfg.DefaultMinScore = cfg.Pipeline.DefaultMinScore
	pipelineCfg.DefaultMaxTokens = cfg.Pipeline.DefaultMaxTokens
	pipelineCfg.DefaultNamespace = cfg.Pipeline.DefaultNamespace
	pipelineCfg.EnableHybrid = cfg.Pipeline.EnableHybrid
	pipelineCfg.EnableRerank = cfg.Pipeline.EnableRerank

	// Create query handler with real embedder
	queryHandler := &queryHandlerAdapter{embedder: embedder}

	pipe, err := pipeline.NewPipeline(
		pipelineCfg,
		queryHandler,
		vectorSearcher,
		rankerAdapter,
		contextBuilderAdapter,
	)
	if err != nil {
		closeErr := embedder.Close()
		if closeErr != nil {
			return nil, &MultiComponentError{
				Primary: &ComponentError{Component: "pipeline", Operation: "create", Cause: err},
				Cleanup: &ComponentError{Component: "embedder", Operation: "close", Cause: closeErr},
			}
		}
		return nil, &ComponentError{Component: "pipeline", Operation: "create", Cause: err}
	}

	return &System{
		pipeline:       pipe,
		vectorIndex:    vectorIndex,
		vectorSearcher: vectorSearcher,
		embedder:       embedder,
		config:         cfg,
		closed:         false,
	}, nil
}

// Query executes a RAG query.
func (s *System) Query(ctx context.Context, query string, opts pipeline.QueryOptions) (*pipeline.QueryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrSystemClosed
	}

	return s.pipeline.Query(ctx, query, opts)
}

// IndexContent indexes content directly into the RAG system.
func (s *System) IndexContent(ctx context.Context, id string, content string, metadata map[string]interface{}) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrSystemClosed
	}

	// Ensure metadata exists
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	// Add via vector searcher which handles embedding generation
	return s.vectorSearcher.Add(ctx, id, content, metadata)
}

// Stats returns system statistics.
func (s *System) Stats() pipeline.Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return pipeline.Stats{}
	}

	return s.pipeline.Stats()
}

// Close gracefully shuts down the system.
func (s *System) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil // Already closed
	}

	s.closed = true

	var errs []error

	// Close pipeline
	if s.pipeline != nil {
		if err := s.pipeline.Close(); err != nil {
			errs = append(errs, &ComponentError{Component: "pipeline", Operation: "close", Cause: err})
		}
	}

	// Close vector searcher
	if s.vectorSearcher != nil {
		if err := s.vectorSearcher.Close(); err != nil {
			errs = append(errs, &ComponentError{Component: "vector searcher", Operation: "close", Cause: err})
		}
	}

	// Close embedder
	if s.embedder != nil {
		if err := s.embedder.Close(); err != nil {
			errs = append(errs, &ComponentError{Component: "embedder", Operation: "close", Cause: err})
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Helper functions

// createEmbedder creates an embedding provider based on configuration.
func createEmbedder(cfg config.EmbeddingConfig) (embedding.Provider, error) {
	// Convert config to embedding.Config
	embeddingCfg := embedding.Config{
		Provider:     cfg.Provider,
		APIURL:       cfg.APIURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		Dimensions:   cfg.Dimensions,
		Timeout:      cfg.Timeout,
		MaxRetries:   3,
		MaxBatchSize: 128,
	}

	switch cfg.Provider {
	case "mock":
		// For tests and mock provider
		return &mockEmbedderAdapter{dimensions: cfg.Dimensions}, nil

	case "ollama":
		// Ollama uses OpenAI-compatible API
		if embeddingCfg.APIURL == "" {
			embeddingCfg.APIURL = "http://localhost:11434"
		}
		return embedding.NewOpenAIProvider(embeddingCfg)

	case "openai":
		// OpenAI API
		if embeddingCfg.APIURL == "" {
			embeddingCfg.APIURL = "https://api.openai.com"
		}
		return embedding.NewOpenAIProvider(embeddingCfg)

	case "voyage":
		// Voyage AI
		if embeddingCfg.APIURL == "" {
			embeddingCfg.APIURL = "https://api.voyageai.com/v1/embeddings"
		}
		return embedding.NewVoyageProvider(embeddingCfg)

	case "lmstudio":
		// LMStudio uses OpenAI-compatible API
		if embeddingCfg.APIURL == "" {
			embeddingCfg.APIURL = "http://localhost:1234"
		}
		return embedding.NewOpenAIProvider(embeddingCfg)

	default:
		return nil, &UnsupportedProviderError{Provider: cfg.Provider}
	}
}

// createVectorIndex creates and initializes the vector index.
func createVectorIndex(cfg config.RAGVectorConfig) (VectorIndex, error) {
	return createVectorIndexFromConfig(cfg)
}

// trainIndex trains the vector index with synthetic data.
func trainIndex(idx VectorIndex, dimensions int) error {
	// Create training vectors (minimum required for clustering)
	numTrainingVectors := 100
	trainingVectors := make([][]float32, numTrainingVectors)

	for i := 0; i < numTrainingVectors; i++ {
		vec := make([]float32, dimensions)
		for j := 0; j < dimensions; j++ {
			// Create somewhat diverse training vectors
			vec[j] = float32(i+j) / float32(numTrainingVectors)
		}
		trainingVectors[i] = vec
	}

	return idx.Train(trainingVectors)
}

// createRankerAdapter creates a ranker adapter from configuration.
func createRankerAdapter(cfg config.RankingConfig) (*adapters.RankerAdapter, error) {
	// Create ranking config
	rankingCfg := ranking.Config{
		BM25K1:        cfg.BM25K1,
		BM25B:         cfg.BM25B,
		VectorWeight:  cfg.VectorWeight,
		KeywordWeight: cfg.KeywordWeight,
		RerankTopK:    cfg.RerankTopK,
		EnableRerank:  cfg.EnableRerank,
		RRFK:          float32(cfg.RRFK),
	}

	// Create hybrid ranker
	hybridRanker := ranking.NewHybridRanker(rankingCfg, nil)

	// Create adapter config
	adapterCfg := &adapters.RankerConfig{
		EnableHybrid:  cfg.Strategy == "hybrid" || cfg.Strategy == "fusion",
		VectorWeight:  cfg.VectorWeight,
		KeywordWeight: cfg.KeywordWeight,
	}

	return adapters.NewRankerAdapter(hybridRanker, adapters.WithRankerConfig(adapterCfg))
}

// createContextBuilderAdapter creates a context builder adapter from configuration.
func createContextBuilderAdapter(cfg config.ContextConfig) (*adapters.ContextBuilderAdapter, error) {
	// Create context builder config
	contextCfg := ragcontext.Config{
		DefaultMaxTokens:   cfg.MaxTokens,
		DefaultTemplate:    ragcontext.Template(cfg.DefaultTemplate),
		IncludeCitations:   cfg.IncludeCitations,
		MaxResultsPerChunk: cfg.MaxResultsPerChunk,
		TokenMultiplier:    cfg.TokenMultiplier,
	}

	// Set truncation strategy
	switch cfg.TruncationStrategy {
	case "smart":
		contextCfg.TruncationStrategy = ragcontext.TruncationSmart
	case "hard":
		contextCfg.TruncationStrategy = ragcontext.TruncationHard
	case "none":
		contextCfg.TruncationStrategy = ragcontext.TruncationNone
	default:
		contextCfg.TruncationStrategy = ragcontext.TruncationSmart
	}

	// Create builder with config
	builder := ragcontext.NewBuilderWithConfig(contextCfg)

	// Create adapter config
	adapterCfg := &adapters.ContextBuilderConfig{
		DefaultMaxTokens: cfg.MaxTokens,
		DefaultTemplate:  ragcontext.Template(cfg.DefaultTemplate),
		IncludeCitations: cfg.IncludeCitations,
	}

	return adapters.NewContextBuilderAdapter(builder, adapters.WithContextBuilderConfig(adapterCfg))
}

// Mock implementations for testing

// mockEmbedderAdapter implements embedding.Provider for testing.
type mockEmbedderAdapter struct {
	dimensions int
	mu         sync.Mutex
	embeddings map[string][]float32
}

func (m *mockEmbedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	// Check for context cancellation - enables testing of error paths
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.embeddings == nil {
		m.embeddings = make(map[string][]float32)
	}

	// Return cached if exists
	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}

	// Generate deterministic embedding
	emb := make([]float32, m.dimensions)
	hash := 0
	for _, c := range text {
		hash = (hash*31 + int(c)) % 256
	}

	for i := 0; i < m.dimensions; i++ {
		emb[i] = float32((hash+i)%256) / 255.0
	}

	m.embeddings[text] = emb
	return emb, nil
}

func (m *mockEmbedderAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := m.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

func (m *mockEmbedderAdapter) Dimensions() int {
	return m.dimensions
}

func (m *mockEmbedderAdapter) MaxTokens() int {
	return 8192
}

func (m *mockEmbedderAdapter) Name() string {
	return "mock"
}

func (m *mockEmbedderAdapter) Close() error {
	return nil
}

// queryHandlerAdapter implements pipeline.QueryHandler using an embedder.
type queryHandlerAdapter struct {
	embedder embedding.Provider
}

func (q *queryHandlerAdapter) Process(ctx context.Context, query string) (*pipeline.ProcessedQuery, error) {
	emb, err := q.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	return &pipeline.ProcessedQuery{
		Original:  query,
		Cleaned:   query,
		Embedding: emb,
		Intent:    "search",
	}, nil
}

func (q *queryHandlerAdapter) Close() error {
	return nil
}
