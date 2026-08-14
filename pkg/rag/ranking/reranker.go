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
	"math"
	"sort"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

// SimilarityReranker implements reranking based on semantic similarity.
// Uses an embedding provider to calculate cosine similarity between query and results.
type SimilarityReranker struct {
	embedder embedding.Provider
}

// NewSimilarityReranker creates a new similarity-based reranker.
func NewSimilarityReranker(embedder embedding.Provider) *SimilarityReranker {
	return &SimilarityReranker{
		embedder: embedder,
	}
}

// Rerank reranks results based on semantic similarity with the query.
// Returns the top K results after reranking.
func (r *SimilarityReranker) Rerank(query string, results []ScoredResult, topK int) ([]ScoredResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	if topK <= 0 {
		return nil, ErrInvalidTopK
	}

	// Generate query embedding
	ctx := context.Background()
	queryEmb, err := r.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	// Calculate cosine similarity for each result
	for i := range results {
		// If result already has an embedding, use it
		var resultEmb []float32
		switch {
		case len(results[i].Vector) > 0:
			resultEmb = results[i].Vector
		case results[i].Content != "":
			// Generate embedding for content
			resultEmb, err = r.embedder.Embed(ctx, results[i].Content)
			if err != nil {
				// If embedding fails, keep existing score
				results[i].RerankScore = results[i].FinalScore
				continue
			}
		default:
			// No content to embed, keep existing score
			results[i].RerankScore = results[i].FinalScore
			continue
		}

		// Calculate cosine similarity
		similarity := cosineSimilarity(queryEmb, resultEmb)

		// Combine with existing score (weighted average)
		// 70% similarity, 30% existing score
		results[i].RerankScore = 0.7*similarity + 0.3*results[i].FinalScore
	}

	// Sort by rerank score
	sort.Slice(results, func(i, j int) bool {
		return results[i].RerankScore > results[j].RerankScore
	})

	// Return top K
	if len(results) > topK {
		results = results[:topK]
	}

	// Update final scores to rerank scores
	for i := range results {
		results[i].FinalScore = results[i].RerankScore
	}

	return results, nil
}

// cosineSimilarity calculates the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct float64
	var normA float64
	var normB float64

	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// SimpleReranker implements a simple reranker that just re-sorts by existing scores.
// Useful for testing or when no embedding provider is available.
type SimpleReranker struct{}

// NewSimpleReranker creates a new simple reranker.
func NewSimpleReranker() *SimpleReranker {
	return &SimpleReranker{}
}

// Rerank simply returns the top K results sorted by their current final score.
func (r *SimpleReranker) Rerank(query string, results []ScoredResult, topK int) ([]ScoredResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	if topK <= 0 {
		return nil, ErrInvalidTopK
	}

	// Sort by final score
	sort.Slice(results, func(i, j int) bool {
		return results[i].FinalScore > results[j].FinalScore
	})

	// Set rerank score to final score
	for i := range results {
		results[i].RerankScore = results[i].FinalScore
	}

	// Return top K
	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}
