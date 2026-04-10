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

package ranking_test

import (
	"fmt"
	"log"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ranking"
)

// Example_basicBM25 demonstrates basic BM25 scoring
func Example_basicBM25() {
	bm25 := ranking.NewBM25(1.2, 0.75)

	query := "authentication handler"
	doc := "This function handles user authentication and session management"

	score := bm25.Score(query, doc, 100)
	fmt.Printf("Score: %.2f\n", score)
	// Output: Score: 2.30
}

// Example_metadataBoosting demonstrates BM25 with metadata boosting
func Example_metadataBoosting() {
	bm25 := ranking.NewBM25(1.2, 0.75)

	query := "authenticate user"
	doc := "Function implementation"

	metadata := map[string]interface{}{
		"function_name": "authenticate_user",
		"file_path":     "/pkg/auth/handler.go",
		"tags":          []string{"security", "auth"},
	}

	score := bm25.ScoreWithMetadata(query, doc, metadata)
	fmt.Printf("Boosted score: %.2f\n", score)
	// Output: Boosted score: 0.00
}

// Example_hybridRanking demonstrates end-to-end hybrid ranking
func Example_hybridRanking() {
	// Setup
	config := ranking.DefaultConfig()
	reranker := ranking.NewSimpleReranker()
	ranker := ranking.NewHybridRanker(config, reranker)

	// Vector search results
	vectorResults := []ranking.Result{
		{
			ID:          "doc1",
			Content:     "authentication handler implementation for secure login",
			VectorScore: 0.9,
			Metadata: map[string]interface{}{
				"function_name": "authenticate_user",
			},
		},
		{
			ID:          "doc2",
			Content:     "database connection pool manager",
			VectorScore: 0.7,
		},
	}

	// Keyword search results
	keywordResults := []ranking.Result{
		{
			ID:      "doc1",
			Content: "authentication handler implementation for secure login",
		},
		{
			ID:      "doc3",
			Content: "user authentication middleware for API requests",
		},
	}

	// Combine and rank
	results, err := ranker.RankHybrid(vectorResults, keywordResults, "authentication handler")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Top result: %s (score: %.2f)\n", results[0].ID, results[0].FinalScore)
	// Output: Top result: doc1 (score: 0.03)
}

// Example_rrfFusion demonstrates reciprocal rank fusion
func Example_rrfFusion() {
	rrf := ranking.NewRRF(60)

	vectorResults := []ranking.Result{
		{ID: "doc1", VectorScore: 0.9},
		{ID: "doc2", VectorScore: 0.8},
	}

	keywordResults := []ranking.Result{
		{ID: "doc2", KeywordScore: 0.7},
		{ID: "doc3", KeywordScore: 0.6},
	}

	fused := rrf.Fuse(vectorResults, keywordResults)

	fmt.Printf("Top result: %s\n", fused[0].ID)
	fmt.Printf("Fused score: %.4f\n", fused[0].FinalScore)
	// Output: Top result: doc2
	// Fused score: 0.0331
}

// Example_simpleRanking demonstrates ranking a single result set
func Example_simpleRanking() {
	config := ranking.DefaultConfig()
	reranker := ranking.NewSimpleReranker()
	ranker := ranking.NewHybridRanker(config, reranker)

	results := []ranking.Result{
		{
			ID:      "doc1",
			Content: "authentication handler implementation",
			Metadata: map[string]interface{}{
				"function_name": "authenticate_user",
			},
		},
		{
			ID:      "doc2",
			Content: "database connection pool",
			Metadata: map[string]interface{}{
				"function_name": "create_pool",
			},
		},
	}

	scored, err := ranker.Rank(results, "authentication")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Top result: %s\n", scored[0].ID)
	// Output: Top result: doc1
}

// Example_configuration demonstrates custom configuration
func Example_configuration() {
	// Custom config with different weights
	config := ranking.Config{
		BM25K1:        1.5,  // Higher term frequency saturation
		BM25B:         0.8,  // More length normalization
		VectorWeight:  0.6,  // 60% vector search weight
		KeywordWeight: 0.4,  // 40% keyword search weight
		RerankTopK:    100,  // Rerank top 100 candidates
		EnableRerank:  true, // Enable reranking
		RRFK:          100,  // Higher RRF constant
	}

	if err := config.Validate(); err != nil {
		log.Fatal(err)
	}

	reranker := ranking.NewSimpleReranker()
	_ = ranking.NewHybridRanker(config, reranker)

	fmt.Printf("Ranker created with custom config\n")
	fmt.Printf("BM25 K1: %.1f\n", config.BM25K1)
	// Output: Ranker created with custom config
	// BM25 K1: 1.5
}

// Example_batchScoring demonstrates batch scoring for efficiency
func Example_batchScoring() {
	bm25 := ranking.NewBM25(1.2, 0.75)

	query := "database connection"
	documents := []string{
		"Database connection pooling implementation",
		"Connection timeout handling",
		"User interface rendering",
	}

	scores := bm25.BatchScore(query, documents, 100)

	for i, score := range scores {
		fmt.Printf("Doc %d score: %.2f\n", i+1, score)
	}
	// Output: Doc 1 score: 4.60
	// Doc 2 score: 2.30
	// Doc 3 score: 0.00
}
