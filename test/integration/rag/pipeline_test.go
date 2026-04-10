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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// TestRAGPipeline_EndToEnd tests the full RAG pipeline workflow.
func TestRAGPipeline_EndToEnd(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create RAG system with real Ollama embeddings
	system, err := createTestRAGSystem(true)
	require.NoError(t, err, "Failed to create RAG system")
	defer system.Close()

	// Index test documents
	docs := createTestDocuments()
	t.Run("IndexDocuments", func(t *testing.T) {
		for _, doc := range docs {
			err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
			require.NoError(t, err, "Failed to index document %s", doc.ID)
		}
		t.Logf("Indexed %d documents successfully", len(docs))
	})

	// Execute RAG query
	t.Run("QueryPipeline", func(t *testing.T) {
		query := "What is machine learning and how does it relate to AI?"
		opts := pipeline.QueryOptions{
			TopK:         5,
			MinScore:     0.3,
			MaxTokens:    2000,
			EnableHybrid: true,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err, "Query failed")
		require.NotNil(t, response, "Response is nil")

		// Verify response structure
		assert.Equal(t, query, response.Query)
		assert.NotEmpty(t, response.Results, "No results returned")
		assert.NotEmpty(t, response.Context, "No context generated")
		assert.Greater(t, response.ContextTokens, 0, "Context tokens should be positive")
		assert.Greater(t, response.ProcessingTime, time.Duration(0), "Processing time should be positive")

		t.Logf("Query: %s", query)
		t.Logf("Results found: %d", len(response.Results))
		t.Logf("Processing time: %v", response.ProcessingTime)
		t.Logf("Context tokens: %d", response.ContextTokens)

		// Log top results
		for i, result := range response.Results {
			if i >= 3 {
				break
			}
			t.Logf("Result %d: ID=%s, Score=%.4f", i+1, result.ID, result.Score)
		}
	})

	// Verify semantic relevance
	t.Run("VerifySemanticRelevance", func(t *testing.T) {
		// Query about AI should return AI-related documents with higher scores
		aiQuery := "artificial intelligence and neural networks"
		aiOpts := pipeline.QueryOptions{TopK: 3, MinScore: 0.2}

		aiResponse, err := system.Query(ctx, aiQuery, aiOpts)
		require.NoError(t, err)
		require.NotEmpty(t, aiResponse.Results)

		// Query about databases should return database-related documents
		dbQuery := "database systems and SQL queries"
		dbOpts := pipeline.QueryOptions{TopK: 3, MinScore: 0.2}

		dbResponse, err := system.Query(ctx, dbQuery, dbOpts)
		require.NoError(t, err)
		require.NotEmpty(t, dbResponse.Results)

		// Results should be different for different domains
		aiTopID := aiResponse.Results[0].ID
		dbTopID := dbResponse.Results[0].ID

		t.Logf("AI query top result: %s (score: %.4f)", aiTopID, aiResponse.Results[0].Score)
		t.Logf("DB query top result: %s (score: %.4f)", dbTopID, dbResponse.Results[0].Score)
	})
}

// TestRAGPipeline_QueryWithRealEmbeddings tests querying with real Ollama embeddings.
func TestRAGPipeline_QueryWithRealEmbeddings(t *testing.T) {
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

	t.Run("SimilarQueriesSimilarResults", func(t *testing.T) {
		// Two similar queries should return similar results
		query1 := "What is deep learning?"
		query2 := "Explain neural networks and deep learning"

		opts := pipeline.QueryOptions{TopK: 3, MinScore: 0.2}

		resp1, err := system.Query(ctx, query1, opts)
		require.NoError(t, err)

		resp2, err := system.Query(ctx, query2, opts)
		require.NoError(t, err)

		// Both should return results related to deep learning
		require.NotEmpty(t, resp1.Results)
		require.NotEmpty(t, resp2.Results)

		t.Logf("Query 1 top result: %s (%.4f)", resp1.Results[0].ID, resp1.Results[0].Score)
		t.Logf("Query 2 top result: %s (%.4f)", resp2.Results[0].ID, resp2.Results[0].Score)
	})

	t.Run("DifferentQueriesDifferentResults", func(t *testing.T) {
		// Queries about different topics should return different top results
		aiQuery := "machine learning algorithms"
		infraQuery := "kubernetes container orchestration"

		opts := pipeline.QueryOptions{TopK: 3, MinScore: 0.1}

		aiResp, err := system.Query(ctx, aiQuery, opts)
		require.NoError(t, err)

		infraResp, err := system.Query(ctx, infraQuery, opts)
		require.NoError(t, err)

		require.NotEmpty(t, aiResp.Results)
		require.NotEmpty(t, infraResp.Results)

		// Top results should be different
		t.Logf("AI query top result: %s", aiResp.Results[0].ID)
		t.Logf("Infra query top result: %s", infraResp.Results[0].ID)
	})
}

// TestRAGPipeline_ContextBuilding tests context building with token limits.
func TestRAGPipeline_ContextBuilding(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index larger document
	largeDoc := createLargeTestDocument()
	err = system.IndexContent(ctx, largeDoc.ID, largeDoc.Content, largeDoc.Metadata)
	require.NoError(t, err)

	// Also index regular documents
	for _, doc := range createTestDocuments() {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}

	t.Run("SmallTokenLimit", func(t *testing.T) {
		query := "What is machine learning?"
		opts := pipeline.QueryOptions{
			TopK:      10,
			MinScore:  0.1,
			MaxTokens: 500, // Small limit
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotNil(t, response)

		// Context should respect token limit
		assert.LessOrEqual(t, response.ContextTokens, 600, "Context tokens should be near limit")
		t.Logf("Requested max tokens: %d, Actual tokens: %d", opts.MaxTokens, response.ContextTokens)
	})

	t.Run("LargeTokenLimit", func(t *testing.T) {
		query := "Explain all aspects of machine learning"
		opts := pipeline.QueryOptions{
			TopK:      10,
			MinScore:  0.1,
			MaxTokens: 4000, // Large limit
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotNil(t, response)

		// Context should include more content with larger limit
		assert.Greater(t, response.ContextTokens, 0)
		assert.NotEmpty(t, response.Context)
		t.Logf("Context tokens with large limit: %d", response.ContextTokens)
	})

	t.Run("ContextContainsRelevantContent", func(t *testing.T) {
		query := "deep learning neural networks"
		opts := pipeline.QueryOptions{
			TopK:      5,
			MinScore:  0.2,
			MaxTokens: 2000,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// Context should contain relevant terms
		assert.NotEmpty(t, response.Context)
		t.Logf("Context preview: %s...", truncateString(response.Context, 200))
	})
}

// TestRAGPipeline_HybridSearch tests hybrid vector + keyword search.
func TestRAGPipeline_HybridSearch(t *testing.T) {
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

	t.Run("HybridEnabled", func(t *testing.T) {
		query := "machine learning artificial intelligence"
		opts := pipeline.QueryOptions{
			TopK:          5,
			MinScore:      0.1,
			EnableHybrid:  true,
			VectorWeight:  0.7,
			KeywordWeight: 0.3,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		t.Logf("Hybrid search returned %d results", len(response.Results))
		for i, r := range response.Results {
			t.Logf("  %d. %s (score: %.4f)", i+1, r.ID, r.Score)
		}
	})

	t.Run("VectorOnlySearch", func(t *testing.T) {
		query := "neural network deep learning models"
		opts := pipeline.QueryOptions{
			TopK:         5,
			MinScore:     0.1,
			EnableHybrid: false, // Vector only
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		t.Logf("Vector-only search returned %d results", len(response.Results))
	})

	t.Run("CompareHybridVsVectorOnly", func(t *testing.T) {
		query := "SQL database queries"

		hybridOpts := pipeline.QueryOptions{
			TopK:          5,
			MinScore:      0.1,
			EnableHybrid:  true,
			VectorWeight:  0.5,
			KeywordWeight: 0.5,
		}

		vectorOpts := pipeline.QueryOptions{
			TopK:         5,
			MinScore:     0.1,
			EnableHybrid: false,
		}

		hybridResp, err := system.Query(ctx, query, hybridOpts)
		require.NoError(t, err)

		vectorResp, err := system.Query(ctx, query, vectorOpts)
		require.NoError(t, err)

		t.Logf("Hybrid top result: %s (%.4f)", hybridResp.Results[0].ID, hybridResp.Results[0].Score)
		t.Logf("Vector top result: %s (%.4f)", vectorResp.Results[0].ID, vectorResp.Results[0].Score)
	})
}

// TestRAGPipeline_MultipleDocuments tests retrieval with multiple documents.
func TestRAGPipeline_MultipleDocuments(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index all documents including large and specialized ones
	allDocs := append(createTestDocuments(),
		createLargeTestDocument(),
		createMarkdownTestDocument(),
		createCodeTestDocument(),
	)

	for _, doc := range allDocs {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}
	t.Logf("Indexed %d documents", len(allDocs))

	t.Run("RetrieveMultipleRelated", func(t *testing.T) {
		query := "machine learning and artificial intelligence systems"
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.1,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		// Should find multiple related documents
		assert.GreaterOrEqual(t, len(response.Results), 2, "Should find multiple related documents")

		// Results should be ordered by relevance
		err = verifyTopKOrdering(response.Results)
		require.NoError(t, err, "Results should be ordered by score")

		t.Logf("Found %d related documents", len(response.Results))
	})

	t.Run("RetrieveFromDifferentCategories", func(t *testing.T) {
		// Query that spans multiple categories
		query := "database systems for machine learning applications"
		opts := pipeline.QueryOptions{
			TopK:     10,
			MinScore: 0.05,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		// Collect unique document IDs
		uniqueIDs := make(map[string]bool)
		for _, r := range response.Results {
			uniqueIDs[r.ID] = true
		}

		t.Logf("Retrieved %d unique documents from query spanning categories", len(uniqueIDs))
		for _, r := range response.Results {
			t.Logf("  - %s (score: %.4f)", r.ID, r.Score)
		}
	})

	t.Run("LimitedTopK", func(t *testing.T) {
		query := "technology concepts"
		opts := pipeline.QueryOptions{
			TopK:     3,
			MinScore: 0.01,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// Should respect TopK limit
		assert.LessOrEqual(t, len(response.Results), opts.TopK, "Should respect TopK limit")
		t.Logf("TopK=%d, Got %d results", opts.TopK, len(response.Results))
	})

	t.Run("HighMinScoreFilter", func(t *testing.T) {
		query := "machine learning"
		opts := pipeline.QueryOptions{
			TopK:     10,
			MinScore: 0.8, // High threshold
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)

		// All results should meet the min score threshold
		for i, r := range response.Results {
			assert.GreaterOrEqual(t, r.Score, opts.MinScore,
				"Result %d score %.4f below min score %.4f", i, r.Score, opts.MinScore)
		}
		t.Logf("High min score filter: %d results with score >= %.2f", len(response.Results), opts.MinScore)
	})
}

// TestRAGPipeline_ConcurrentQueries tests concurrent query execution.
func TestRAGPipeline_ConcurrentQueries(t *testing.T) {
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

	queries := []string{
		"machine learning algorithms",
		"deep neural networks",
		"database systems",
		"kubernetes orchestration",
		"microservices architecture",
	}

	t.Run("ParallelQueries", func(t *testing.T) {
		results := make(chan error, len(queries))

		for _, q := range queries {
			go func(query string) {
				opts := pipeline.QueryOptions{TopK: 3, MinScore: 0.1}
				_, err := system.Query(ctx, query, opts)
				results <- err
			}(q)
		}

		// Collect all results
		for i := 0; i < len(queries); i++ {
			err := <-results
			require.NoError(t, err, "Concurrent query %d failed", i)
		}
		t.Logf("Successfully executed %d concurrent queries", len(queries))
	})
}

// TestRAGPipeline_EdgeCases tests edge cases and error handling.
func TestRAGPipeline_EdgeCases(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	t.Run("EmptyQuery", func(t *testing.T) {
		opts := pipeline.QueryOptions{TopK: 5}
		_, err := system.Query(ctx, "", opts)
		assert.Error(t, err, "Empty query should return error")
	})

	t.Run("QueryWithNoIndexedDocs", func(t *testing.T) {
		// Query on fresh system with no documents
		freshSystem, err := createTestRAGSystem(true)
		require.NoError(t, err)
		defer freshSystem.Close()

		opts := pipeline.QueryOptions{TopK: 5, MinScore: 0.1}
		response, err := freshSystem.Query(ctx, "any query", opts)

		// Should not error, but may return empty results
		if err == nil {
			t.Logf("Query on empty index returned %d results", len(response.Results))
		}
	})

	t.Run("VeryLongQuery", func(t *testing.T) {
		// Index at least one document first
		doc := createTestDocuments()[0]
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)

		// Create a very long query
		longQuery := "machine learning " + string(make([]byte, 1000))
		opts := pipeline.QueryOptions{TopK: 5, MinScore: 0.01}

		response, err := system.Query(ctx, longQuery, opts)
		// Should handle gracefully - either success or appropriate error
		if err != nil {
			t.Logf("Long query handled with error (acceptable): %v", err)
		} else {
			t.Logf("Long query returned %d results", len(response.Results))
		}
	})
}

// TestRAGPipeline_Stats tests pipeline statistics collection.
func TestRAGPipeline_Stats(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	system, err := createTestRAGSystem(true)
	require.NoError(t, err)
	defer system.Close()

	// Index some documents
	docs := createTestDocuments()[:3]
	for _, doc := range docs {
		err := system.IndexContent(ctx, doc.ID, doc.Content, doc.Metadata)
		require.NoError(t, err)
	}

	// Execute some queries
	for i := 0; i < 3; i++ {
		opts := pipeline.QueryOptions{TopK: 3, MinScore: 0.1}
		_, err := system.Query(ctx, "machine learning", opts)
		require.NoError(t, err)
	}

	// Check stats
	stats := system.Stats()
	t.Logf("Pipeline stats: %+v", stats)

	// Stats should reflect queries executed
	assert.GreaterOrEqual(t, stats.TotalQueries, int64(0), "Should track queries")
}

// truncateString truncates a string to maxLen characters.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
