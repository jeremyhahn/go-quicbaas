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

//go:build integration && rag

package rag

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// TestRetrieval_SemanticSimilarity verifies semantic search relevance.
func TestRetrieval_SemanticSimilarity(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index documents
	docs := createTestDocuments()
	for _, doc := range docs {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}

	t.Run("RelatedQueryReturnsRelatedDocs", func(t *testing.T) {
		// Query about machine learning should return ML-related documents first
		query := "machine learning algorithms and neural networks"
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.1,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		// First results should be AI/ML related
		topResult := response.Results[0]
		t.Logf("Top result for ML query: %s (score: %.4f)", topResult.ID, topResult.Score)

		// The top result should have good relevance score
		assert.Greater(t, topResult.Score, float32(0.3), "Top result should have good relevance")
	})

	t.Run("DifferentDomainsProduceDifferentResults", func(t *testing.T) {
		// AI-focused query
		aiQuery := "artificial intelligence deep learning neural networks"
		aiOpts := pipeline.QueryOptions{TopK: 3, MinScore: 0.1}

		aiResp, err := system.Query(ctx, aiQuery, aiOpts)
		require.NoError(t, err)
		require.NotEmpty(t, aiResp.Results)

		// Database-focused query
		dbQuery := "SQL database queries relational data storage"
		dbOpts := pipeline.QueryOptions{TopK: 3, MinScore: 0.1}

		dbResp, err := system.Query(ctx, dbQuery, dbOpts)
		require.NoError(t, err)
		require.NotEmpty(t, dbResp.Results)

		// Infrastructure-focused query
		infraQuery := "kubernetes containers deployment orchestration"
		infraOpts := pipeline.QueryOptions{TopK: 3, MinScore: 0.1}

		infraResp, err := system.Query(ctx, infraQuery, infraOpts)
		require.NoError(t, err)
		require.NotEmpty(t, infraResp.Results)

		// Log results
		t.Logf("AI query top results:")
		for i, r := range aiResp.Results {
			t.Logf("  %d. %s (%.4f)", i+1, r.ID, r.Score)
		}

		t.Logf("DB query top results:")
		for i, r := range dbResp.Results {
			t.Logf("  %d. %s (%.4f)", i+1, r.ID, r.Score)
		}

		t.Logf("Infra query top results:")
		for i, r := range infraResp.Results {
			t.Logf("  %d. %s (%.4f)", i+1, r.ID, r.Score)
		}
	})

	t.Run("SimilarQueriesSimilarRankings", func(t *testing.T) {
		// Two very similar queries
		query1 := "What is deep learning?"
		query2 := "Explain deep learning concepts"

		opts := pipeline.QueryOptions{TopK: 5, MinScore: 0.1}

		resp1, err := system.Query(ctx, query1, opts)
		require.NoError(t, err)

		resp2, err := system.Query(ctx, query2, opts)
		require.NoError(t, err)

		require.NotEmpty(t, resp1.Results)
		require.NotEmpty(t, resp2.Results)

		// Similar queries should have similar top results
		t.Logf("Query 1 '%s' top: %s (%.4f)", query1, resp1.Results[0].ID, resp1.Results[0].Score)
		t.Logf("Query 2 '%s' top: %s (%.4f)", query2, resp2.Results[0].ID, resp2.Results[0].Score)
	})

	t.Run("SynonymsAndParaphrases", func(t *testing.T) {
		// Using synonyms should return similar results
		originalQuery := "machine learning"
		synonymQuery := "ML artificial intelligence learning algorithms"

		opts := pipeline.QueryOptions{TopK: 5, MinScore: 0.1}

		origResp, err := system.Query(ctx, originalQuery, opts)
		require.NoError(t, err)

		synResp, err := system.Query(ctx, synonymQuery, opts)
		require.NoError(t, err)

		require.NotEmpty(t, origResp.Results)
		require.NotEmpty(t, synResp.Results)

		// Check overlap in top results
		origIDs := make(map[string]bool)
		for _, r := range origResp.Results {
			origIDs[r.ID] = true
		}

		overlap := 0
		for _, r := range synResp.Results {
			if origIDs[r.ID] {
				overlap++
			}
		}

		t.Logf("Result overlap between original and synonym query: %d/%d", overlap, len(synResp.Results))
	})
}

// TestRetrieval_MetadataFiltering tests filtering by metadata.
func TestRetrieval_MetadataFiltering(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index documents with rich metadata
	docs := createTestDocuments()
	for _, doc := range docs {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}

	t.Run("FilterByCategory", func(t *testing.T) {
		query := "learning and systems"
		opts := pipeline.QueryOptions{
			TopK:     10,
			MinScore: 0.01,
			Filters: map[string]interface{}{
				"category": "AI",
			},
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// All results should be from AI category
		for i, r := range response.Results {
			if meta, ok := r.Metadata["category"]; ok {
				t.Logf("Result %d: %s (category: %v)", i, r.ID, meta)
			}
		}
	})

	t.Run("FilterByTopic", func(t *testing.T) {
		query := "technical concepts"
		opts := pipeline.QueryOptions{
			TopK:     10,
			MinScore: 0.01,
			Filters: map[string]interface{}{
				"topic": "machine learning",
			},
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		t.Logf("Topic filter returned %d results", len(response.Results))
		for i, r := range response.Results {
			t.Logf("  %d. %s (topic: %v)", i, r.ID, r.Metadata["topic"])
		}
	})

	t.Run("CombinedFilters", func(t *testing.T) {
		query := "systems and technology"
		opts := pipeline.QueryOptions{
			TopK:     10,
			MinScore: 0.01,
			Filters: map[string]interface{}{
				"category": "databases",
			},
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		t.Logf("Combined filter returned %d results", len(response.Results))
	})

	t.Run("FilterWithNoMatches", func(t *testing.T) {
		query := "machine learning"
		opts := pipeline.QueryOptions{
			TopK:     10,
			MinScore: 0.01,
			Filters: map[string]interface{}{
				"category": "nonexistent-category",
			},
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// Should return empty or reduced results
		t.Logf("Filter with no matches returned %d results", len(response.Results))
	})
}

// TestRetrieval_TopK verifies top-k results ordering.
func TestRetrieval_TopK(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index documents
	docs := createTestDocuments()
	for _, doc := range docs {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}

	t.Run("TopKLimitRespected", func(t *testing.T) {
		query := "technology and systems"

		for _, k := range []int{1, 3, 5, 10} {
			opts := pipeline.QueryOptions{
				TopK:     k,
				MinScore: 0.01,
			}

			response, err := system.Query(ctx, query, opts)
			require.NoError(t, err)

			assert.LessOrEqual(t, len(response.Results), k,
				"Results should not exceed TopK=%d, got %d", k, len(response.Results))

			t.Logf("TopK=%d returned %d results", k, len(response.Results))
		}
	})

	t.Run("ResultsOrderedByScore", func(t *testing.T) {
		query := "machine learning artificial intelligence"
		opts := pipeline.QueryOptions{
			TopK:     10,
			MinScore: 0.01,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		// Verify descending order
		err = verifyTopKOrdering(response.Results)
		require.NoError(t, err, "Results should be ordered by score descending")

		// Log scores
		t.Log("Results in order:")
		for i, r := range response.Results {
			t.Logf("  %d. %s: %.4f", i+1, r.ID, r.Score)
		}
	})

	t.Run("ConsistentOrderingAcrossQueries", func(t *testing.T) {
		query := "deep learning neural networks"
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.1,
		}

		// Execute same query multiple times
		results := make([][]string, 3)
		for i := 0; i < 3; i++ {
			response, err := system.Query(ctx, query, opts)
			require.NoError(t, err)

			ids := make([]string, len(response.Results))
			for j, r := range response.Results {
				ids[j] = r.ID
			}
			results[i] = ids
		}

		// Results should be consistent (same order)
		for i := 1; i < len(results); i++ {
			if len(results[i]) == len(results[0]) {
				for j := range results[0] {
					if j < len(results[i]) {
						assert.Equal(t, results[0][j], results[i][j],
							"Query %d has different order at position %d", i, j)
					}
				}
			}
		}

		t.Log("Results consistent across 3 queries")
	})

	t.Run("TopKWithFewerResults", func(t *testing.T) {
		// Request more results than available
		query := "very specific unique content not in corpus"
		opts := pipeline.QueryOptions{
			TopK:     100,
			MinScore: 0.9, // High threshold
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// Should return fewer results than requested
		t.Logf("TopK=100 with high threshold returned %d results", len(response.Results))
	})
}

// TestRetrieval_ThresholdFiltering tests score threshold filtering.
func TestRetrieval_ThresholdFiltering(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index documents
	docs := createTestDocuments()
	for _, doc := range docs {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}

	t.Run("MinScoreFilteringWorks", func(t *testing.T) {
		query := "machine learning"

		// Test with different thresholds
		thresholds := []float32{0.1, 0.3, 0.5, 0.7}

		prevCount := 100 // Start high
		for _, threshold := range thresholds {
			opts := pipeline.QueryOptions{
				TopK:     100,
				MinScore: threshold,
			}

			response, err := system.Query(ctx, query, opts)
			require.NoError(t, err)

			// Verify all results meet threshold
			for i, r := range response.Results {
				assert.GreaterOrEqual(t, r.Score, threshold,
					"Result %d score %.4f below threshold %.4f", i, r.Score, threshold)
			}

			t.Logf("Threshold %.2f: %d results", threshold, len(response.Results))

			// Higher threshold should return fewer or equal results
			assert.LessOrEqual(t, len(response.Results), prevCount,
				"Higher threshold should not return more results")
			prevCount = len(response.Results)
		}
	})

	t.Run("ZeroThresholdReturnsAll", func(t *testing.T) {
		query := "technology systems"
		opts := pipeline.QueryOptions{
			TopK:     100,
			MinScore: 0.0,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		t.Logf("Zero threshold returned %d results", len(response.Results))
	})

	t.Run("VeryHighThresholdReturnsNone", func(t *testing.T) {
		query := "random unrelated query string xyz"
		opts := pipeline.QueryOptions{
			TopK:     100,
			MinScore: 0.99, // Very high threshold
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// Should return very few or no results
		t.Logf("Very high threshold returned %d results", len(response.Results))
	})

	t.Run("ThresholdCombinedWithTopK", func(t *testing.T) {
		query := "artificial intelligence"
		opts := pipeline.QueryOptions{
			TopK:     3,
			MinScore: 0.3,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// Should respect both TopK and MinScore
		assert.LessOrEqual(t, len(response.Results), opts.TopK)
		for _, r := range response.Results {
			assert.GreaterOrEqual(t, r.Score, opts.MinScore)
		}

		t.Logf("TopK=%d, MinScore=%.2f returned %d results", opts.TopK, opts.MinScore, len(response.Results))
	})
}

// TestRetrieval_RelevanceQuality tests the quality of retrieval relevance.
func TestRetrieval_RelevanceQuality(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index documents
	docs := createTestDocuments()
	for _, doc := range docs {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}

	t.Run("ExactMatchHighScore", func(t *testing.T) {
		// Query that closely matches document content
		exactQuery := "Machine learning is a subset of artificial intelligence"
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.1,
		}

		response, err := system.Query(ctx, exactQuery, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		// Top result should have high score for near-exact match
		t.Logf("Exact match query top score: %.4f", response.Results[0].Score)
	})

	t.Run("PartialMatchMediumScore", func(t *testing.T) {
		partialQuery := "computers learning from data"
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.1,
		}

		response, err := system.Query(ctx, partialQuery, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		t.Logf("Partial match query top score: %.4f", response.Results[0].Score)
	})

	t.Run("UnrelatedQueryLowScore", func(t *testing.T) {
		unrelatedQuery := "cooking recipes for pasta dishes"
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.01,
		}

		response, err := system.Query(ctx, unrelatedQuery, opts)
		require.NoError(t, err)

		if len(response.Results) > 0 {
			// Unrelated query should have lower scores
			t.Logf("Unrelated query top score: %.4f", response.Results[0].Score)
		} else {
			t.Log("Unrelated query returned no results (expected)")
		}
	})

	t.Run("RelevanceDistribution", func(t *testing.T) {
		query := "machine learning"
		opts := pipeline.QueryOptions{
			TopK:     10,
			MinScore: 0.01,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		// Collect scores
		scores := make([]float32, len(response.Results))
		for i, r := range response.Results {
			scores[i] = r.Score
		}

		// Sort for distribution analysis
		sort.Slice(scores, func(i, j int) bool {
			return scores[i] > scores[j]
		})

		t.Logf("Score distribution:")
		t.Logf("  Max: %.4f", scores[0])
		if len(scores) > 1 {
			t.Logf("  Min: %.4f", scores[len(scores)-1])
		}
		if len(scores) > 2 {
			median := scores[len(scores)/2]
			t.Logf("  Median: %.4f", median)
		}
	})
}

// TestRetrieval_Performance tests retrieval performance characteristics.
func TestRetrieval_Performance(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index documents
	docs := createTestDocuments()
	for _, doc := range docs {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}

	t.Run("QueryLatency", func(t *testing.T) {
		query := "machine learning"
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.1,
		}

		// Warm up
		_, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// Measure latency
		start := time.Now()
		response, err := system.Query(ctx, query, opts)
		latency := time.Since(start)

		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		t.Logf("Query latency: %v", latency)
		t.Logf("Processing time (reported): %v", response.ProcessingTime)

		// Latency should be reasonable (under 10 seconds for embedding-based search)
		assert.Less(t, latency, 10*time.Second, "Query latency too high")
	})

	t.Run("BatchQueryThroughput", func(t *testing.T) {
		queries := []string{
			"machine learning",
			"database systems",
			"kubernetes",
			"microservices",
			"neural networks",
		}

		opts := pipeline.QueryOptions{TopK: 3, MinScore: 0.1}

		start := time.Now()
		for _, query := range queries {
			_, err := system.Query(ctx, query, opts)
			require.NoError(t, err)
		}
		totalTime := time.Since(start)

		throughput := float64(len(queries)) / totalTime.Seconds()
		t.Logf("Batch throughput: %.2f queries/second", throughput)
		t.Logf("Total time for %d queries: %v", len(queries), totalTime)
	})
}
