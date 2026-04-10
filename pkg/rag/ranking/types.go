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

// Result represents a single search result with metadata and scores.
type Result struct {
	// Vector data
	ID        string    // Vector ID
	Vector    []float32 // Embedding vector (optional)
	Namespace string    // Namespace

	// Content and metadata
	Content  string                 // Original content
	Metadata map[string]interface{} // Full metadata

	// Source tracking
	FilePath  string // Source file
	StartLine int    // Start line number
	EndLine   int    // End line number

	// Scoring components
	VectorScore  float32 // Vector similarity score (0-1)
	KeywordScore float32 // BM25 keyword score (0-1)
	RerankScore  float32 // Reranker score (0-1)
	FinalScore   float32 // Final fused score (0-1)
}

// ScoredResult is an alias for Result for backward compatibility.
type ScoredResult = Result

// Ranker defines the main ranking interface.
type Ranker interface {
	// Rank scores and ranks search results
	Rank(results []Result, query string) ([]ScoredResult, error)

	// RankWithMetadata ranks results with additional metadata context
	RankWithMetadata(results []Result, query string, metadata map[string]interface{}) ([]ScoredResult, error)
}

// BM25Scorer defines the interface for BM25 keyword scoring.
type BM25Scorer interface {
	// Score calculates BM25 score for a query-document pair
	Score(query string, document string, avgDocLen float32) float32

	// ScoreWithMetadata scores with metadata boosting
	ScoreWithMetadata(query string, doc string, metadata map[string]interface{}) float32

	// BatchScore scores multiple documents
	BatchScore(query string, documents []string, avgDocLen float32) []float32
}

// Reranker defines the interface for result reranking.
type Reranker interface {
	// Rerank applies reranking to results and returns top K
	Rerank(query string, results []ScoredResult, topK int) ([]ScoredResult, error)
}

// Fusion defines the interface for result fusion.
type Fusion interface {
	// Fuse combines multiple ranked lists into a single ranked list
	Fuse(vectorResults, keywordResults []Result) []ScoredResult
}
