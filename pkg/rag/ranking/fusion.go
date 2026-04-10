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

// RRF implements Reciprocal Rank Fusion for combining multiple ranked lists.
type RRF struct {
	k float32 // Constant for reciprocal rank fusion (default: 60)
}

// NewRRF creates a new RRF fusion instance.
func NewRRF(k float32) *RRF {
	return &RRF{k: k}
}

// Fuse combines vector and keyword search results using reciprocal rank fusion.
func (r *RRF) Fuse(vectorResults, keywordResults []Result) []ScoredResult {
	// Build a map of all unique results
	resultMap := make(map[string]*Result)
	scores := make(map[string]float32)

	// Add vector results with their reciprocal ranks
	for rank, result := range vectorResults {
		id := result.ID
		if id == "" {
			id = result.FilePath // Fallback to filepath as ID
		}

		// Calculate RRF score: 1 / (k + rank)
		rrfScore := 1.0 / (r.k + float32(rank))
		scores[id] = rrfScore

		// Store the result
		resultCopy := result
		resultMap[id] = &resultCopy
	}

	// Add keyword results with their reciprocal ranks
	for rank, result := range keywordResults {
		id := result.ID
		if id == "" {
			id = result.FilePath
		}

		// Calculate RRF score
		rrfScore := 1.0 / (r.k + float32(rank))

		// Add to existing score if already present
		if existing, ok := scores[id]; ok {
			scores[id] = existing + rrfScore
		} else {
			scores[id] = rrfScore
			resultCopy := result
			resultMap[id] = &resultCopy
		}
	}

	// Convert to sorted slice
	type scoredID struct {
		id    string
		score float32
	}

	sorted := make([]scoredID, 0, len(scores))
	for id, score := range scores {
		sorted = append(sorted, scoredID{id: id, score: score})
	}

	// Sort by score descending
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	// Build final results
	results := make([]ScoredResult, 0, len(sorted))
	for _, item := range sorted {
		result := *resultMap[item.id]
		result.FinalScore = item.score
		results = append(results, result)
	}

	return results
}

// WeightedFusion combines results using weighted score fusion.
type WeightedFusion struct {
	vectorWeight  float32
	keywordWeight float32
}

// NewWeightedFusion creates a new weighted fusion instance.
func NewWeightedFusion(vectorWeight, keywordWeight float32) *WeightedFusion {
	return &WeightedFusion{
		vectorWeight:  vectorWeight,
		keywordWeight: keywordWeight,
	}
}

// Fuse combines vector and keyword results using weighted score fusion.
func (wf *WeightedFusion) Fuse(vectorResults, keywordResults []Result) []ScoredResult {
	// Build a map of all unique results
	resultMap := make(map[string]*Result)
	scores := make(map[string]float32)

	// Add vector results with weighted scores
	for _, result := range vectorResults {
		id := result.ID
		if id == "" {
			id = result.FilePath
		}

		// Normalize vector score to [0, 1] if needed
		normalizedScore := result.VectorScore
		if normalizedScore > 1.0 {
			normalizedScore = 1.0
		}

		scores[id] = normalizedScore * wf.vectorWeight
		resultCopy := result
		resultMap[id] = &resultCopy
	}

	// Add keyword results with weighted scores
	for _, result := range keywordResults {
		id := result.ID
		if id == "" {
			id = result.FilePath
		}

		// Normalize keyword score
		normalizedScore := result.KeywordScore
		if normalizedScore > 1.0 {
			normalizedScore = normalizedScore / 10.0 // Simple normalization for BM25 scores
			if normalizedScore > 1.0 {
				normalizedScore = 1.0
			}
		}

		weightedScore := normalizedScore * wf.keywordWeight

		if existing, ok := scores[id]; ok {
			scores[id] = existing + weightedScore
		} else {
			scores[id] = weightedScore
			resultCopy := result
			resultMap[id] = &resultCopy
		}
	}

	// Convert to sorted slice
	type scoredID struct {
		id    string
		score float32
	}

	sorted := make([]scoredID, 0, len(scores))
	for id, score := range scores {
		sorted = append(sorted, scoredID{id: id, score: score})
	}

	// Sort by score descending
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	// Build final results
	results := make([]ScoredResult, 0, len(sorted))
	for _, item := range sorted {
		result := *resultMap[item.id]
		result.FinalScore = item.score
		results = append(results, result)
	}

	return results
}
