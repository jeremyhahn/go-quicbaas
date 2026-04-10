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

import "sort"

// HybridRanker combines BM25, RRF fusion, and reranking into a complete hybrid ranking pipeline.
type HybridRanker struct {
	bm25     *BM25
	rrf      *RRF
	reranker Reranker
	config   Config
}

// NewHybridRanker creates a new hybrid ranker with the given configuration.
func NewHybridRanker(config Config, reranker Reranker) *HybridRanker {
	return &HybridRanker{
		bm25:     NewBM25(config.BM25K1, config.BM25B),
		rrf:      NewRRF(config.RRFK),
		reranker: reranker,
		config:   config,
	}
}

// Rank implements the Ranker interface.
// This is a simplified version that scores results with BM25 and returns them.
func (h *HybridRanker) Rank(results []Result, query string) ([]ScoredResult, error) {
	return h.RankWithMetadata(results, query, nil)
}

// RankWithMetadata ranks results considering metadata.
func (h *HybridRanker) RankWithMetadata(results []Result, query string, metadata map[string]interface{}) ([]ScoredResult, error) {
	if query == "" {
		return nil, ErrEmptyQuery
	}

	if len(results) == 0 {
		return nil, ErrEmptyResults
	}

	// Score all results with BM25
	scoredResults := make([]ScoredResult, len(results))

	for i, result := range results {
		// Score with BM25
		bm25Score := h.bm25.ScoreWithMetadata(query, result.Content, result.Metadata)

		scoredResults[i] = ScoredResult{
			ID:           result.ID,
			Vector:       result.Vector,
			Namespace:    result.Namespace,
			Content:      result.Content,
			Metadata:     result.Metadata,
			FilePath:     result.FilePath,
			StartLine:    result.StartLine,
			EndLine:      result.EndLine,
			VectorScore:  result.VectorScore,
			KeywordScore: bm25Score,
			RerankScore:  0,
			FinalScore:   bm25Score, // Initial final score is BM25 score
		}
	}

	// Sort by final score
	sort.Slice(scoredResults, func(i, j int) bool {
		return scoredResults[i].FinalScore > scoredResults[j].FinalScore
	})

	// Apply reranking if enabled
	if h.config.EnableRerank && h.reranker != nil {
		topK := h.config.RerankTopK
		if topK > len(scoredResults) {
			topK = len(scoredResults)
		}

		reranked, err := h.reranker.Rerank(query, scoredResults, topK)
		if err != nil {
			// If reranking fails, return the BM25-scored results
			return scoredResults, nil
		}
		return reranked, nil
	}

	return scoredResults, nil
}

// RankHybrid performs hybrid ranking combining vector and keyword results.
func (h *HybridRanker) RankHybrid(vectorResults, keywordResults []Result, query string) ([]ScoredResult, error) {
	if query == "" {
		return nil, ErrEmptyQuery
	}

	// If only one result set is provided, fall back to simple ranking
	if len(vectorResults) == 0 && len(keywordResults) == 0 {
		return nil, ErrEmptyResults
	}

	if len(vectorResults) == 0 {
		return h.Rank(keywordResults, query)
	}

	if len(keywordResults) == 0 {
		return h.Rank(vectorResults, query)
	}

	// Score keyword results with BM25 if not already scored
	for i := range keywordResults {
		if keywordResults[i].KeywordScore == 0 {
			keywordResults[i].KeywordScore = h.bm25.ScoreWithMetadata(
				query,
				keywordResults[i].Content,
				keywordResults[i].Metadata,
			)
		}
	}

	// Fuse results using RRF
	fusedResults := h.rrf.Fuse(vectorResults, keywordResults)

	// Apply reranking if enabled
	if h.config.EnableRerank && h.reranker != nil {
		topK := h.config.RerankTopK
		if topK > len(fusedResults) {
			topK = len(fusedResults)
		}

		reranked, err := h.reranker.Rerank(query, fusedResults, topK)
		if err != nil {
			// If reranking fails, return the fused results
			return fusedResults, nil
		}
		return reranked, nil
	}

	return fusedResults, nil
}

// calculateAvgDocLength calculates the average document length for BM25.
func (h *HybridRanker) calculateAvgDocLength(results []Result) float32 {
	if len(results) == 0 {
		return 100.0 // Default average
	}

	var totalLen int
	for _, result := range results {
		totalLen += len(tokenize(result.Content))
	}

	return float32(totalLen) / float32(len(results))
}
