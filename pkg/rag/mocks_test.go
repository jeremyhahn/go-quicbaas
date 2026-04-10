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
	"hash/fnv"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// mockEmbedderImpl provides a test implementation of the embedding Provider interface.
type mockEmbedderImpl struct {
	dimensions int
	embeddings map[string][]float32
	mu         sync.Mutex
}

// Embed generates a deterministic embedding for text.
func (m *mockEmbedderImpl) Embed(ctx context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.embeddings == nil {
		m.embeddings = make(map[string][]float32)
	}

	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}

	// Generate deterministic embedding based on text hash
	emb := make([]float32, m.dimensions)
	h := fnv.New64a()
	h.Write([]byte(text))
	seed := h.Sum64()

	for i := range emb {
		// Simple deterministic value generation
		emb[i] = float32((seed^uint64(i))%1000) / 1000.0
	}

	m.embeddings[text] = emb
	return emb, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (m *mockEmbedderImpl) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
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

// Dimensions returns the embedding dimensions.
func (m *mockEmbedderImpl) Dimensions() int {
	return m.dimensions
}

// MaxTokens returns the maximum tokens.
func (m *mockEmbedderImpl) MaxTokens() int {
	return 8192
}

// Name returns the provider name.
func (m *mockEmbedderImpl) Name() string {
	return "mock"
}

// Close is a no-op for the mock.
func (m *mockEmbedderImpl) Close() error {
	return nil
}

// Ensure mockEmbedderImpl implements embedding.Provider
var _ embedding.Provider = (*mockEmbedderImpl)(nil)

// mockQueryHandler provides a test implementation of the QueryHandler interface.
type mockQueryHandler struct {
	embedder embedding.Provider
}

// Process processes a query into a ProcessedQuery.
func (m *mockQueryHandler) Process(ctx context.Context, query string) (*pipeline.ProcessedQuery, error) {
	emb, err := m.embedder.Embed(ctx, query)
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

// Close closes the handler.
func (m *mockQueryHandler) Close() error {
	return nil
}

// Ensure mockQueryHandler implements pipeline.QueryHandler
var _ pipeline.QueryHandler = (*mockQueryHandler)(nil)
