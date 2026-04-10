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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHybridRanker(t *testing.T) {
	config := DefaultConfig()
	reranker := NewSimpleReranker()

	ranker := NewHybridRanker(config, reranker)

	require.NotNil(t, ranker)
	assert.NotNil(t, ranker.bm25)
	assert.NotNil(t, ranker.rrf)
	assert.NotNil(t, ranker.reranker)
	assert.Equal(t, config, ranker.config)
}

func TestHybridRanker_Rank(t *testing.T) {
	config := DefaultConfig()
	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	tests := []struct {
		name    string
		results []Result
		query   string
		wantErr error
		wantLen int
	}{
		{
			name: "rank with content",
			results: []Result{
				{
					ID:      "doc1",
					Content: "authentication handler implementation",
					Metadata: map[string]interface{}{
						"function_name": "authenticate",
					},
				},
				{
					ID:      "doc2",
					Content: "database connection pooling",
					Metadata: map[string]interface{}{
						"function_name": "connect_db",
					},
				},
			},
			query:   "authentication",
			wantErr: nil,
			wantLen: 2,
		},
		{
			name:    "empty query",
			results: []Result{{ID: "doc1", Content: "content"}},
			query:   "",
			wantErr: ErrEmptyQuery,
			wantLen: 0,
		},
		{
			name:    "empty results",
			results: []Result{},
			query:   "test query",
			wantErr: ErrEmptyResults,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := ranker.Rank(tt.results, tt.query)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, results, tt.wantLen)

			// Verify scores are set
			for i, result := range results {
				assert.GreaterOrEqual(t, result.KeywordScore, float32(0.0),
					"result %d should have non-negative keyword score", i)
			}

			// Verify results are sorted by score
			for i := 1; i < len(results); i++ {
				assert.GreaterOrEqual(t, results[i-1].FinalScore, results[i].FinalScore,
					"results should be sorted by final score descending")
			}
		})
	}
}

func TestHybridRanker_RankWithMetadata(t *testing.T) {
	config := DefaultConfig()
	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	results := []Result{
		{
			ID:      "doc1",
			Content: "authentication implementation",
			Metadata: map[string]interface{}{
				"function_name": "authenticate_user",
				"file_path":     "/pkg/auth/handler.go",
			},
		},
		{
			ID:      "doc2",
			Content: "database connection",
			Metadata: map[string]interface{}{
				"function_name": "connect_database",
			},
		},
	}

	metadata := map[string]interface{}{
		"boost_auth": true,
	}

	scored, err := ranker.RankWithMetadata(results, "authentication", metadata)

	require.NoError(t, err)
	assert.Len(t, scored, 2)

	// First result should have higher score due to better match
	assert.Greater(t, scored[0].KeywordScore, scored[1].KeywordScore)
}

func TestHybridRanker_RankHybrid(t *testing.T) {
	config := DefaultConfig()
	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	tests := []struct {
		name           string
		vectorResults  []Result
		keywordResults []Result
		query          string
		wantErr        error
		wantLen        int
	}{
		{
			name: "hybrid ranking with both results",
			vectorResults: []Result{
				{ID: "doc1", Content: "auth handler", VectorScore: 0.9},
				{ID: "doc2", Content: "db connection", VectorScore: 0.8},
			},
			keywordResults: []Result{
				{ID: "doc2", Content: "db connection", KeywordScore: 0.7},
				{ID: "doc3", Content: "api server", KeywordScore: 0.6},
			},
			query:   "database",
			wantErr: nil,
			wantLen: 3,
		},
		{
			name: "vector only",
			vectorResults: []Result{
				{ID: "doc1", Content: "test content matching query", VectorScore: 0.9},
			},
			keywordResults: []Result{},
			query:          "test",
			wantErr:        nil,
			wantLen:        1,
		},
		{
			name:          "keyword only",
			vectorResults: []Result{},
			keywordResults: []Result{
				{ID: "doc1", Content: "test content matching query"},
			},
			query:   "test",
			wantErr: nil,
			wantLen: 1,
		},
		{
			name:           "both empty",
			vectorResults:  []Result{},
			keywordResults: []Result{},
			query:          "test",
			wantErr:        ErrEmptyResults,
			wantLen:        0,
		},
		{
			name: "empty query",
			vectorResults: []Result{
				{ID: "doc1", Content: "content1"},
			},
			keywordResults: []Result{},
			query:          "",
			wantErr:        ErrEmptyQuery,
			wantLen:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := ranker.RankHybrid(tt.vectorResults, tt.keywordResults, tt.query)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, results, tt.wantLen)

			// Verify all results have final scores
			for i, result := range results {
				assert.Greater(t, result.FinalScore, float32(0.0),
					"result %d should have positive final score", i)
			}

			// Verify results are sorted
			for i := 1; i < len(results); i++ {
				assert.GreaterOrEqual(t, results[i-1].FinalScore, results[i].FinalScore,
					"results should be sorted by final score descending")
			}
		})
	}
}

func TestHybridRanker_RankHybridWithReranking(t *testing.T) {
	config := DefaultConfig()
	config.EnableRerank = true
	config.RerankTopK = 2

	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	vectorResults := []Result{
		{ID: "doc1", Content: "auth handler", VectorScore: 0.9},
		{ID: "doc2", Content: "db connection", VectorScore: 0.8},
		{ID: "doc3", Content: "api server", VectorScore: 0.7},
	}

	keywordResults := []Result{
		{ID: "doc2", Content: "db connection", KeywordScore: 0.8},
	}

	results, err := ranker.RankHybrid(vectorResults, keywordResults, "database")

	require.NoError(t, err)
	// Should be limited by RerankTopK
	assert.LessOrEqual(t, len(results), config.RerankTopK)

	// Verify rerank scores are set
	for _, result := range results {
		assert.Greater(t, result.RerankScore, float32(0.0))
	}
}

func TestHybridRanker_RankHybridWithoutReranking(t *testing.T) {
	config := DefaultConfig()
	config.EnableRerank = false

	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	vectorResults := []Result{
		{ID: "doc1", Content: "content1", VectorScore: 0.9},
		{ID: "doc2", Content: "content2", VectorScore: 0.8},
	}

	keywordResults := []Result{
		{ID: "doc2", Content: "content2", KeywordScore: 0.7},
	}

	results, err := ranker.RankHybrid(vectorResults, keywordResults, "test")

	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Verify rerank scores are not set (should be 0)
	for _, result := range results {
		assert.Equal(t, float32(0.0), result.RerankScore)
	}
}

func TestHybridRanker_CalculateAvgDocLength(t *testing.T) {
	config := DefaultConfig()
	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	tests := []struct {
		name     string
		results  []Result
		expected float32
	}{
		{
			name: "normal documents",
			results: []Result{
				{Content: "hello world"},        // 2 tokens
				{Content: "foo bar baz"},        // 3 tokens
				{Content: "one two three four"}, // 4 tokens
			},
			expected: 3.0, // (2 + 3 + 4) / 3 = 3
		},
		{
			name:     "empty results",
			results:  []Result{},
			expected: 100.0, // Default
		},
		{
			name: "single document",
			results: []Result{
				{Content: "hello world"},
			},
			expected: 2.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avgLen := ranker.calculateAvgDocLength(tt.results)
			assert.Equal(t, tt.expected, avgLen)
		})
	}
}

func TestHybridRanker_EndToEnd(t *testing.T) {
	// End-to-end test with realistic data
	config := DefaultConfig()
	config.EnableRerank = true
	config.RerankTopK = 3

	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	vectorResults := []Result{
		{
			ID:          "doc1",
			Content:     "Authentication handler for user login and session management",
			VectorScore: 0.9,
			Metadata: map[string]interface{}{
				"function_name": "authenticate_user",
				"file_path":     "/pkg/auth/handler.go",
			},
		},
		{
			ID:          "doc2",
			Content:     "Database connection pool manager",
			VectorScore: 0.8,
			Metadata: map[string]interface{}{
				"function_name": "create_pool",
				"file_path":     "/pkg/db/pool.go",
			},
		},
		{
			ID:          "doc3",
			Content:     "API server initialization",
			VectorScore: 0.7,
			Metadata: map[string]interface{}{
				"function_name": "init_server",
				"file_path":     "/pkg/api/server.go",
			},
		},
	}

	keywordResults := []Result{
		{
			ID:      "doc1",
			Content: "Authentication handler for user login and session management",
			Metadata: map[string]interface{}{
				"function_name": "authenticate_user",
			},
		},
		{
			ID:      "doc4",
			Content: "User authentication middleware",
			Metadata: map[string]interface{}{
				"function_name": "auth_middleware",
			},
		},
	}

	results, err := ranker.RankHybrid(vectorResults, keywordResults, "authentication handler")

	require.NoError(t, err)
	assert.NotEmpty(t, results)

	// doc1 should be first (appears in both lists and has best match)
	assert.Equal(t, "doc1", results[0].ID)

	// All results should have proper scores
	for _, result := range results {
		assert.Greater(t, result.FinalScore, float32(0.0))
		assert.GreaterOrEqual(t, result.RerankScore, float32(0.0))
	}
}

func BenchmarkHybridRanker_Rank(b *testing.B) {
	config := DefaultConfig()
	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	results := make([]Result, 100)
	for i := range results {
		results[i] = Result{
			ID:      string(rune(i)),
			Content: "some content for benchmarking BM25 scoring performance",
			Metadata: map[string]interface{}{
				"function_name": "test_function",
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ranker.Rank(results, "benchmark test")
	}
}

func BenchmarkHybridRanker_RankHybrid(b *testing.B) {
	config := DefaultConfig()
	config.EnableRerank = false // Disable for pure fusion benchmark
	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	vectorResults := make([]Result, 100)
	for i := range vectorResults {
		vectorResults[i] = Result{
			ID:          string(rune(i)),
			Content:     "content",
			VectorScore: float32(100-i) / 100.0,
		}
	}

	keywordResults := make([]Result, 100)
	for i := range keywordResults {
		keywordResults[i] = Result{
			ID:           string(rune(i + 50)),
			Content:      "content",
			KeywordScore: float32(100-i) / 100.0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ranker.RankHybrid(vectorResults, keywordResults, "test query")
	}
}
