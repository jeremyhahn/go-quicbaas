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

package ranking

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock embedding provider for testing
type mockEmbedder struct {
	embeddings map[string][]float32
	err        error
}

func newMockEmbedder() *mockEmbedder {
	return &mockEmbedder{
		embeddings: make(map[string][]float32),
	}
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}

	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}

	// Generate a simple mock embedding based on text length
	emb := make([]float32, 128)
	for i := range emb {
		emb[i] = float32(len(text)%10) / 10.0
	}
	return emb, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := m.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = emb
	}
	return result, nil
}

func (m *mockEmbedder) Dimensions() int {
	return 128
}

func (m *mockEmbedder) MaxTokens() int {
	return 8192
}

func (m *mockEmbedder) Name() string {
	return "mock"
}

func (m *mockEmbedder) Close() error {
	return nil
}

func TestNewSimilarityReranker(t *testing.T) {
	embedder := newMockEmbedder()
	reranker := NewSimilarityReranker(embedder)

	require.NotNil(t, reranker)
	assert.NotNil(t, reranker.embedder)
}

func TestSimilarityReranker_Rerank(t *testing.T) {
	embedder := newMockEmbedder()

	// Set up mock embeddings with known similarities
	queryEmb := []float32{1.0, 0.0, 0.0, 0.0}
	doc1Emb := []float32{0.9, 0.1, 0.0, 0.0} // High similarity
	doc2Emb := []float32{0.5, 0.5, 0.0, 0.0} // Medium similarity
	doc3Emb := []float32{0.0, 0.0, 1.0, 0.0} // Low similarity

	// Pad to 128 dimensions
	for len(queryEmb) < 128 {
		queryEmb = append(queryEmb, 0)
	}
	for len(doc1Emb) < 128 {
		doc1Emb = append(doc1Emb, 0)
	}
	for len(doc2Emb) < 128 {
		doc2Emb = append(doc2Emb, 0)
	}
	for len(doc3Emb) < 128 {
		doc3Emb = append(doc3Emb, 0)
	}

	embedder.embeddings["test query"] = queryEmb

	reranker := NewSimilarityReranker(embedder)

	tests := []struct {
		name      string
		query     string
		results   []ScoredResult
		topK      int
		wantLen   int
		wantErr   bool
		setupMock func()
	}{
		{
			name:  "rerank with embeddings",
			query: "test query",
			results: []ScoredResult{
				{ID: "doc1", Vector: doc1Emb, Content: "content1", FinalScore: 0.5},
				{ID: "doc2", Vector: doc2Emb, Content: "content2", FinalScore: 0.6},
				{ID: "doc3", Vector: doc3Emb, Content: "content3", FinalScore: 0.7},
			},
			topK:    3,
			wantLen: 3,
			wantErr: false,
		},
		{
			name:  "rerank fewer than topK",
			query: "test query",
			results: []ScoredResult{
				{ID: "doc1", Vector: doc1Emb, Content: "content1", FinalScore: 0.5},
				{ID: "doc2", Vector: doc2Emb, Content: "content2", FinalScore: 0.6},
			},
			topK:    5,
			wantLen: 2,
			wantErr: false,
		},
		{
			name:    "empty results",
			query:   "test query",
			results: []ScoredResult{},
			topK:    5,
			wantLen: 0,
			wantErr: false,
		},
		{
			name:  "invalid topK",
			query: "test query",
			results: []ScoredResult{
				{ID: "doc1", Vector: doc1Emb, FinalScore: 0.5},
			},
			topK:    0,
			wantLen: 0,
			wantErr: true,
		},
		{
			name:  "rerank with content (no vector)",
			query: "test query",
			results: []ScoredResult{
				{ID: "doc1", Content: "content1", FinalScore: 0.5},
				{ID: "doc2", Content: "content2", FinalScore: 0.6},
			},
			topK:    2,
			wantLen: 2,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock()
			}

			results, err := reranker.Rerank(tt.query, tt.results, tt.topK)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, results, tt.wantLen)

			// Verify results are sorted by rerank score
			for i := 1; i < len(results); i++ {
				assert.GreaterOrEqual(t, results[i-1].RerankScore, results[i].RerankScore,
					"results should be sorted by rerank score descending")
			}

			// Verify final scores are updated
			for i, result := range results {
				assert.Equal(t, result.RerankScore, result.FinalScore,
					"result %d final score should equal rerank score", i)
			}
		})
	}
}

func TestSimilarityReranker_RerankWithEmbeddingError(t *testing.T) {
	embedder := newMockEmbedder()
	embedder.err = errors.New("embedding failed")

	reranker := NewSimilarityReranker(embedder)

	results := []ScoredResult{
		{ID: "doc1", Content: "content1", FinalScore: 0.5},
	}

	_, err := reranker.Rerank("test query", results, 1)
	require.Error(t, err)
}

func TestSimilarityReranker_RerankNoContent(t *testing.T) {
	embedder := newMockEmbedder()
	embedder.embeddings["test query"] = make([]float32, 128)

	reranker := NewSimilarityReranker(embedder)

	// Result with no vector and no content
	results := []ScoredResult{
		{ID: "doc1", FinalScore: 0.5},
	}

	reranked, err := reranker.Rerank("test query", results, 1)
	require.NoError(t, err)
	require.Len(t, reranked, 1)

	// Should keep the original final score as rerank score
	assert.Equal(t, float32(0.5), reranked[0].RerankScore)
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		delta    float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 1.0,
			delta:    0.0001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{0.0, 1.0, 0.0},
			expected: 0.0,
			delta:    0.0001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{-1.0, 0.0, 0.0},
			expected: -1.0,
			delta:    0.0001,
		},
		{
			name:     "similar vectors",
			a:        []float32{1.0, 1.0, 0.0},
			b:        []float32{1.0, 0.9, 0.0},
			expected: 0.995, // approximately
			delta:    0.01,
		},
		{
			name:     "different lengths",
			a:        []float32{1.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 0.0,
			delta:    0.0001,
		},
		{
			name:     "zero vector",
			a:        []float32{0.0, 0.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 0.0,
			delta:    0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, float64(tt.delta))
		})
	}
}

func TestNewSimpleReranker(t *testing.T) {
	reranker := NewSimpleReranker()
	require.NotNil(t, reranker)
}

func TestSimpleReranker_Rerank(t *testing.T) {
	reranker := NewSimpleReranker()

	tests := []struct {
		name    string
		query   string
		results []ScoredResult
		topK    int
		wantLen int
		wantErr bool
	}{
		{
			name:  "simple rerank",
			query: "test query",
			results: []ScoredResult{
				{ID: "doc1", FinalScore: 0.5},
				{ID: "doc2", FinalScore: 0.8},
				{ID: "doc3", FinalScore: 0.6},
			},
			topK:    3,
			wantLen: 3,
			wantErr: false,
		},
		{
			name:  "topK smaller than results",
			query: "test query",
			results: []ScoredResult{
				{ID: "doc1", FinalScore: 0.5},
				{ID: "doc2", FinalScore: 0.8},
				{ID: "doc3", FinalScore: 0.6},
			},
			topK:    2,
			wantLen: 2,
			wantErr: false,
		},
		{
			name:    "empty results",
			query:   "test query",
			results: []ScoredResult{},
			topK:    5,
			wantLen: 0,
			wantErr: false,
		},
		{
			name:  "invalid topK",
			query: "test query",
			results: []ScoredResult{
				{ID: "doc1", FinalScore: 0.5},
			},
			topK:    0,
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := reranker.Rerank(tt.query, tt.results, tt.topK)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, results, tt.wantLen)

			// Verify results are sorted by final score descending
			for i := 1; i < len(results); i++ {
				assert.GreaterOrEqual(t, results[i-1].FinalScore, results[i].FinalScore,
					"results should be sorted by final score descending")
			}

			// Verify rerank score equals final score
			for i, result := range results {
				assert.Equal(t, result.FinalScore, result.RerankScore,
					"result %d rerank score should equal final score", i)
			}
		})
	}
}

func BenchmarkSimilarityReranker_Rerank(b *testing.B) {
	embedder := newMockEmbedder()
	reranker := NewSimilarityReranker(embedder)

	// Create test embeddings
	queryEmb := make([]float32, 128)
	for i := range queryEmb {
		queryEmb[i] = 0.5
	}
	embedder.embeddings["test query"] = queryEmb

	results := make([]ScoredResult, 50)
	for i := range results {
		emb := make([]float32, 128)
		for j := range emb {
			emb[j] = float32(i) / 50.0
		}
		results[i] = ScoredResult{
			ID:         string(rune(i)),
			Vector:     emb,
			FinalScore: float32(i) / 50.0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reranker.Rerank("test query", results, 10)
	}
}

func BenchmarkSimpleReranker_Rerank(b *testing.B) {
	reranker := NewSimpleReranker()

	results := make([]ScoredResult, 50)
	for i := range results {
		results[i] = ScoredResult{
			ID:         string(rune(i)),
			FinalScore: float32(i) / 50.0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reranker.Rerank("test query", results, 10)
	}
}
