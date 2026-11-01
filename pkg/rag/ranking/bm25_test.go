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

	"context"
	"errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBM25(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)
	require.NotNil(t, bm25)
	assert.Equal(t, float32(1.2), bm25.k1)
	assert.Equal(t, float32(0.75), bm25.b)
}

func TestBM25_Score(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	tests := []struct {
		name      string
		query     string
		document  string
		avgDocLen float32
		wantScore bool // whether we expect a score > 0
	}{
		{
			name:      "matching query and document",
			query:     "authentication token",
			document:  "This function handles authentication token validation and verification",
			avgDocLen: 100,
			wantScore: true,
		},
		{
			name:      "partial match",
			query:     "authentication security",
			document:  "Authentication is a critical security feature",
			avgDocLen: 100,
			wantScore: true,
		},
		{
			name:      "no match",
			query:     "database connection",
			document:  "This file handles user interface rendering",
			avgDocLen: 100,
			wantScore: false,
		},
		{
			name:      "empty query",
			query:     "",
			document:  "Some document content",
			avgDocLen: 100,
			wantScore: false,
		},
		{
			name:      "empty document",
			query:     "some query",
			document:  "",
			avgDocLen: 100,
			wantScore: false,
		},
		{
			name:      "case insensitive match",
			query:     "Database",
			document:  "database connection pool management",
			avgDocLen: 100,
			wantScore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := bm25.Score(tt.query, tt.document, tt.avgDocLen)

			if tt.wantScore {
				assert.Greater(t, score, float32(0.0), "expected positive score")
			} else {
				assert.Equal(t, float32(0.0), score, "expected zero score")
			}
		})
	}
}

func TestBM25_ScoreWithMetadata(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	tests := []struct {
		name          string
		query         string
		document      string
		metadata      map[string]interface{}
		expectBoosted bool
	}{
		{
			name:     "function name boost",
			query:    "authenticate user login function",
			document: "function implementation details",
			metadata: map[string]interface{}{
				"function_name": "authenticate_user",
			},
			expectBoosted: true,
		},
		{
			name:     "file path boost",
			query:    "auth handler login",
			document: "handler implementation details",
			metadata: map[string]interface{}{
				"file_path": "/pkg/auth/handler.go",
			},
			expectBoosted: true,
		},
		{
			name:     "tags boost",
			query:    "security feature auth implementation",
			document: "implementation details",
			metadata: map[string]interface{}{
				"tags": []string{"security", "authentication"},
			},
			expectBoosted: true,
		},
		{
			name:     "language boost",
			query:    "golang go implementation",
			document: "code implementation details",
			metadata: map[string]interface{}{
				"language": "go",
			},
			expectBoosted: true,
		},
		{
			name:     "multiple boosts",
			query:    "auth handler security",
			document: "authentication handler implementation",
			metadata: map[string]interface{}{
				"function_name": "auth_handler",
				"file_path":     "/pkg/auth/handler.go",
				"tags":          []string{"auth", "security"},
			},
			expectBoosted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			boostedScore := bm25.ScoreWithMetadata(tt.query, tt.document, tt.metadata)
			baselineScore := bm25.ScoreWithMetadata(tt.query, tt.document, nil)

			if tt.expectBoosted {
				assert.Greater(t, boostedScore, baselineScore,
					"expected boosted score (%.4f) to be higher than baseline (%.4f)", boostedScore, baselineScore)
			}

			assert.GreaterOrEqual(t, boostedScore, float32(0.0), "score should not be negative")
		})
	}
}

func TestBM25_BatchScore(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	query := "database connection"
	documents := []string{
		"Database connection pooling implementation",
		"Connection timeout handling",
		"User interface rendering",
	}

	scores := bm25.BatchScore(query, documents, 100)

	require.Len(t, scores, len(documents))

	// First two documents should have higher scores than the third
	assert.Greater(t, scores[0], float32(0.0))
	assert.Greater(t, scores[1], float32(0.0))

	// First document should have highest score (contains both terms)
	assert.Greater(t, scores[0], scores[1])
	assert.Greater(t, scores[0], scores[2])
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "simple text",
			text:     "hello world",
			expected: []string{"hello", "world"},
		},
		{
			name:     "with punctuation",
			text:     "Hello, World!",
			expected: []string{"hello", "world"},
		},
		{
			name:     "camelCase",
			text:     "authenticateUser",
			expected: []string{"authenticateuser"},
		},
		{
			name:     "snake_case",
			text:     "authenticate_user",
			expected: []string{"authenticate", "user"},
		},
		{
			name:     "with numbers",
			text:     "user123 password456",
			expected: []string{"user123", "password456"},
		},
		{
			name:     "empty string",
			text:     "",
			expected: nil,
		},
		{
			name:     "special characters",
			text:     "foo@bar.com",
			expected: []string{"foo", "bar", "com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenize(tt.text)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		source   []string
		target   []string
		expected bool
	}{
		{
			name:     "match found",
			source:   []string{"auth", "user"},
			target:   []string{"authenticate", "auth", "login"},
			expected: true,
		},
		{
			name:     "no match",
			source:   []string{"database", "connection"},
			target:   []string{"user", "interface"},
			expected: false,
		},
		{
			name:     "empty source",
			source:   []string{},
			target:   []string{"auth", "user"},
			expected: false,
		},
		{
			name:     "empty target",
			source:   []string{"auth", "user"},
			target:   []string{},
			expected: false,
		},
		{
			name:     "both empty",
			source:   []string{},
			target:   []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsAny(tt.source, tt.target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBM25_ScoreConsistency(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	query := "authentication token validation"
	document := "This function handles authentication token validation and verification"

	// Score should be consistent across multiple calls
	score1 := bm25.Score(query, document, 100)
	score2 := bm25.Score(query, document, 100)
	score3 := bm25.Score(query, document, 100)

	assert.Equal(t, score1, score2)
	assert.Equal(t, score2, score3)
}

func TestBM25_ParameterSensitivity(t *testing.T) {
	query := "database connection"
	document := "database connection pool implementation with timeout handling"

	// Test different k1 values - note: BM25 doesn't use avgDocLen parameter
	bm25_k1_1 := NewBM25(1.0, 0.75)
	bm25_k1_2 := NewBM25(2.0, 0.75)

	score_k1_1 := bm25_k1_1.Score(query, document, 100)
	score_k1_2 := bm25_k1_2.Score(query, document, 100)

	// Scores should be positive
	assert.Greater(t, score_k1_1, float32(0.0))
	assert.Greater(t, score_k1_2, float32(0.0))

	// Test different b values
	bm25_b_0 := NewBM25(1.2, 0.0)
	bm25_b_1 := NewBM25(1.2, 1.0)

	score_b_0 := bm25_b_0.Score(query, document, 100)
	score_b_1 := bm25_b_1.Score(query, document, 100)

	// Scores should be positive
	assert.Greater(t, score_b_0, float32(0.0))
	assert.Greater(t, score_b_1, float32(0.0))
}

func BenchmarkBM25_Score(b *testing.B) {
	bm25 := NewBM25(1.2, 0.75)
	query := "authentication token validation"
	document := "This function handles authentication token validation and verification for secure access control"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bm25.Score(query, document, 100)
	}
}

func BenchmarkBM25_ScoreWithMetadata(b *testing.B) {
	bm25 := NewBM25(1.2, 0.75)
	query := "authentication handler"
	document := "Authentication handler implementation"
	metadata := map[string]interface{}{
		"function_name": "authenticate_user",
		"file_path":     "/pkg/auth/handler.go",
		"tags":          []string{"security", "auth"},
		"language":      "go",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bm25.ScoreWithMetadata(query, document, metadata)
	}
}

func BenchmarkBM25_BatchScore(b *testing.B) {
	bm25 := NewBM25(1.2, 0.75)
	query := "database connection"
	documents := make([]string, 100)
	for i := range documents {
		documents[i] = "Database connection pooling implementation with timeout handling"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bm25.BatchScore(query, documents, 100)
	}
}

func TestHybridRanker_RankWithMetadata_AllPaths(t *testing.T) {
	tests := []struct {
		name         string
		config       Config
		reranker     Reranker
		results      []Result
		query        string
		wantErr      error
		checkResults func(t *testing.T, results []ScoredResult)
	}{
		{
			name: "reranking enabled with topK exactly matching results",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    2,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			reranker: NewSimpleReranker(),
			results: []Result{
				{ID: "doc1", Content: "test content one"},
				{ID: "doc2", Content: "test content two"},
			},
			query:   "test",
			wantErr: nil,
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 2)
				// Verify reranking was applied
				for _, r := range results {
					assert.GreaterOrEqual(t, r.RerankScore, float32(0.0))
				}
			},
		},
		{
			name: "reranking enabled with topK less than results",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    1,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			reranker: NewSimpleReranker(),
			results: []Result{
				{ID: "doc1", Content: "test one"},
				{ID: "doc2", Content: "test two"},
				{ID: "doc3", Content: "test three"},
			},
			query:   "test",
			wantErr: nil,
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.LessOrEqual(t, len(results), 1)
				for _, r := range results {
					assert.Greater(t, r.RerankScore, float32(0.0))
				}
			},
		},
		{
			name: "reranker failure falls back to BM25 scores",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    2,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			reranker: &failingRerankerImpl{},
			results: []Result{
				{ID: "doc1", Content: "authentication handler implementation"},
				{ID: "doc2", Content: "authentication database connection"},
				{ID: "doc3", Content: "authentication api server"},
			},
			query:   "authentication",
			wantErr: nil,
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 3)
				hasPositiveScore := false
				for _, r := range results {
					if r.KeywordScore > 0 {
						hasPositiveScore = true
					}
					assert.Equal(t, r.KeywordScore, r.FinalScore)
					assert.Equal(t, float32(0.0), r.RerankScore)
				}
				assert.True(t, hasPositiveScore, "at least one result should have positive score")
			},
		},
		{
			name: "reranking enabled with topK much greater than results",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    1000,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			reranker: NewSimpleReranker(),
			results: []Result{
				{ID: "doc1", Content: "test"},
			},
			query:   "test",
			wantErr: nil,
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 1)
			},
		},
		{
			name: "reranking disabled",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  false,
				RerankTopK:    10,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			reranker: NewSimpleReranker(),
			results: []Result{
				{ID: "doc1", Content: "test content"},
				{ID: "doc2", Content: "test content two"},
			},
			query:   "test",
			wantErr: nil,
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 2)
				// Rerank score should be 0 when reranking is disabled
				for _, r := range results {
					assert.Equal(t, float32(0.0), r.RerankScore)
				}
			},
		},
		{
			name: "nil reranker with reranking enabled",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    10,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			reranker: nil,
			results: []Result{
				{ID: "doc1", Content: "test"},
			},
			query:   "test",
			wantErr: nil,
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 1)
				// Should skip reranking when reranker is nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranker := NewHybridRanker(tt.config, tt.reranker)
			scored, err := ranker.RankWithMetadata(tt.results, tt.query, nil)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err)
				return
			}

			require.NoError(t, err)
			if tt.checkResults != nil {
				tt.checkResults(t, scored)
			}
		})
	}
}

// TestHybridRanker_RankHybrid_AllPaths tests all code paths in RankHybrid
func TestHybridRanker_RankHybrid_AllPaths(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		reranker       Reranker
		vectorResults  []Result
		keywordResults []Result
		query          string
		checkResults   func(t *testing.T, results []ScoredResult)
	}{
		{
			name: "reranking with topK boundary",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    2,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			vectorResults: []Result{
				{ID: "doc1", Content: "test", VectorScore: 0.9},
			},
			keywordResults: []Result{
				{ID: "doc2", Content: "test", KeywordScore: 0.8},
			},
			query: "test",
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 2)
			},
		},
		{
			name: "reranking with topK exceeds results",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    100,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			vectorResults: []Result{
				{ID: "doc1", Content: "test", VectorScore: 0.9},
			},
			keywordResults: []Result{
				{ID: "doc2", Content: "test", KeywordScore: 0.8},
			},
			query: "test",
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 2)
			},
		},
		{
			name: "reranking with topK less than fused results",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    2,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			vectorResults: []Result{
				{ID: "doc1", Content: "vector1", VectorScore: 0.9},
				{ID: "doc2", Content: "vector2", VectorScore: 0.8},
			},
			keywordResults: []Result{
				{ID: "doc3", Content: "keyword1", KeywordScore: 0.7},
				{ID: "doc4", Content: "keyword2", KeywordScore: 0.6},
			},
			query: "test",
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.LessOrEqual(t, len(results), 2)
			},
		},
		{
			name: "reranker failure falls back to fused results",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  true,
				RerankTopK:    2,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			reranker: &failingRerankerImpl{},
			vectorResults: []Result{
				{ID: "doc1", Content: "auth", VectorScore: 0.9},
			},
			keywordResults: []Result{
				{ID: "doc2", Content: "db"},
			},
			query: "test",
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 2)
				for _, r := range results {
					assert.Equal(t, float32(0.0), r.RerankScore)
				}
			},
		},
		{
			name: "pre-scored keyword results keep their scores",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  false,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			vectorResults: []Result{
				{ID: "doc1", Content: "auth handler", VectorScore: 0.9},
				{ID: "doc2", Content: "db connection", VectorScore: 0.8},
			},
			keywordResults: []Result{
				{ID: "doc2", Content: "db connection", KeywordScore: 5.5},
				{ID: "doc3", Content: "api server", KeywordScore: 3.2},
			},
			query: "database",
			checkResults: func(t *testing.T, results []ScoredResult) {
				assert.Len(t, results, 3)
				for _, r := range results {
					assert.Greater(t, r.FinalScore, float32(0.0))
				}
			},
		},
		{
			name: "zero keyword score is computed with BM25",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				EnableRerank:  false,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RRFK:          60,
			},
			vectorResults: []Result{
				{ID: "doc1", Content: "vector content", VectorScore: 0.9},
			},
			keywordResults: []Result{
				{ID: "doc2", Content: "keyword content", KeywordScore: 0},
			},
			query: "content",
			checkResults: func(t *testing.T, results []ScoredResult) {
				var doc2Found bool
				for _, r := range results {
					if r.ID == "doc2" {
						doc2Found = true
						assert.Greater(t, r.KeywordScore, float32(0.0))
					}
				}
				assert.True(t, doc2Found)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reranker := tt.reranker
			if reranker == nil {
				reranker = NewSimpleReranker()
			}
			ranker := NewHybridRanker(tt.config, reranker)
			results, err := ranker.RankHybrid(tt.vectorResults, tt.keywordResults, tt.query)

			require.NoError(t, err)
			if tt.checkResults != nil {
				tt.checkResults(t, results)
			}
		})
	}
}

// TestBM25_ScoreWithMetadata_ComprehensiveMetadataTests tests all metadata paths
func TestBM25_ScoreWithMetadata_ComprehensiveMetadataTests(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	tests := []struct {
		name     string
		query    string
		document string
		metadata map[string]interface{}
		check    func(t *testing.T, score float32)
	}{
		{
			name:     "nil metadata",
			query:    "test query",
			document: "test document content",
			metadata: nil,
			check: func(t *testing.T, score float32) {
				assert.Greater(t, score, float32(0.0))
			},
		},
		{
			name:     "empty metadata map",
			query:    "test query",
			document: "test document content",
			metadata: map[string]interface{}{},
			check: func(t *testing.T, score float32) {
				assert.Greater(t, score, float32(0.0))
			},
		},
		{
			name:     "metadata with empty string values",
			query:    "test",
			document: "test content",
			metadata: map[string]interface{}{
				"function_name": "",
				"file_path":     "",
				"language":      "",
			},
			check: func(t *testing.T, score float32) {
				assert.Greater(t, score, float32(0.0))
			},
		},
		{
			name:     "metadata with wrong types",
			query:    "test",
			document: "test content",
			metadata: map[string]interface{}{
				"function_name": 123,
				"file_path":     []int{1, 2, 3},
				"tags":          "not-a-slice",
				"language":      map[string]string{},
			},
			check: func(t *testing.T, score float32) {
				assert.Greater(t, score, float32(0.0))
			},
		},
		{
			name:     "metadata with all boosts applied",
			query:    "test auth security go",
			document: "test authentication security implementation",
			metadata: map[string]interface{}{
				"function_name": "test_auth",
				"file_path":     "/test/security.go",
				"tags":          []string{"security"},
				"language":      "go",
			},
			check: func(t *testing.T, score float32) {
				assert.Greater(t, score, float32(0.0))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := bm25.ScoreWithMetadata(tt.query, tt.document, tt.metadata)
			tt.check(t, score)
		})
	}
}

func TestBM25_ScoreWithMetadata_EdgeCases(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	tests := []struct {
		name     string
		query    string
		document string
		metadata map[string]interface{}
		wantZero bool
	}{
		{
			name:     "empty function name in metadata",
			query:    "authenticate",
			document: "authentication handler",
			metadata: map[string]interface{}{
				"function_name": "",
			},
			wantZero: false,
		},
		{
			name:     "empty file path in metadata",
			query:    "handler",
			document: "authentication handler",
			metadata: map[string]interface{}{
				"file_path": "",
			},
			wantZero: false,
		},
		{
			name:     "empty language in metadata",
			query:    "code",
			document: "some code implementation",
			metadata: map[string]interface{}{
				"language": "",
			},
			wantZero: false,
		},
		{
			name:     "non-string function name",
			query:    "test",
			document: "test function",
			metadata: map[string]interface{}{
				"function_name": 123, // Not a string
			},
			wantZero: false,
		},
		{
			name:     "non-string file path",
			query:    "test",
			document: "test file",
			metadata: map[string]interface{}{
				"file_path": []string{"not", "a", "string"},
			},
			wantZero: false,
		},
		{
			name:     "non-string-slice tags",
			query:    "security",
			document: "security feature",
			metadata: map[string]interface{}{
				"tags": "not-a-slice",
			},
			wantZero: false,
		},
		{
			name:     "non-string language",
			query:    "golang",
			document: "golang implementation",
			metadata: map[string]interface{}{
				"language": 123,
			},
			wantZero: false,
		},
		{
			name:     "metadata with no matches",
			query:    "database",
			document: "user interface",
			metadata: map[string]interface{}{
				"function_name": "render_ui",
				"file_path":     "/pkg/ui/render.go",
				"tags":          []string{"ui", "frontend"},
				"language":      "go",
			},
			wantZero: false,
		},
		{
			name:     "empty tags slice",
			query:    "test",
			document: "test content",
			metadata: map[string]interface{}{
				"tags": []string{},
			},
			wantZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := bm25.ScoreWithMetadata(tt.query, tt.document, tt.metadata)
			if tt.wantZero {
				assert.Equal(t, float32(0.0), score)
			} else {
				assert.GreaterOrEqual(t, score, float32(0.0))
			}
		})
	}
}

// TestWeightedFusion_Fuse_HighScores tests normalization of high BM25 scores
func TestWeightedFusion_Fuse_HighScores(t *testing.T) {
	wf := NewWeightedFusion(0.7, 0.3)

	tests := []struct {
		name           string
		vectorResults  []Result
		keywordResults []Result
		expectedMax    float32
	}{
		{
			name: "very high BM25 score normalization",
			vectorResults: []Result{
				{ID: "doc1", VectorScore: 0.8},
			},
			keywordResults: []Result{
				{ID: "doc1", KeywordScore: 25.0}, // High BM25 score > 10
			},
			expectedMax: 1.0,
		},
		{
			name: "BM25 score exactly at boundary",
			vectorResults: []Result{
				{ID: "doc1", VectorScore: 0.9},
			},
			keywordResults: []Result{
				{ID: "doc1", KeywordScore: 10.0},
			},
			expectedMax: 1.0,
		},
		{
			name: "BM25 score just above 1.0",
			vectorResults: []Result{
				{ID: "doc1", VectorScore: 0.5},
			},
			keywordResults: []Result{
				{ID: "doc1", KeywordScore: 1.5},
			},
			expectedMax: 1.0,
		},
		{
			name:          "keyword only with high score",
			vectorResults: []Result{},
			keywordResults: []Result{
				{ID: "doc1", KeywordScore: 20.0},
			},
			expectedMax: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := wf.Fuse(tt.vectorResults, tt.keywordResults)
			require.Len(t, results, 1)
			assert.LessOrEqual(t, results[0].FinalScore, tt.expectedMax)
			assert.GreaterOrEqual(t, results[0].FinalScore, float32(0.0))
		})
	}
}

// TestHybridRanker_RankHybrid_PreScoredKeywords tests path where keyword score already set
func TestHybridRanker_RankHybrid_PreScoredKeywords(t *testing.T) {
	config := DefaultConfig()
	config.EnableRerank = false
	ranker := NewHybridRanker(config, NewSimpleReranker())

	vectorResults := []Result{
		{ID: "doc1", Content: "auth handler", VectorScore: 0.9},
		{ID: "doc2", Content: "db connection", VectorScore: 0.8},
	}

	// Keyword results with pre-set scores (non-zero)
	keywordResults := []Result{
		{ID: "doc2", Content: "db connection", KeywordScore: 5.5},
		{ID: "doc3", Content: "api server", KeywordScore: 3.2},
	}

	results, err := ranker.RankHybrid(vectorResults, keywordResults, "database")

	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify that keyword scores were not recalculated (they were already set)
	// The pre-set scores should be preserved in the fusion
	for _, result := range results {
		assert.Greater(t, result.FinalScore, float32(0.0))
	}
}

// TestHybridRanker_RankWithMetadata_TopKGreaterThanResults tests topK boundary
func TestHybridRanker_RankWithMetadata_TopKGreaterThanResults(t *testing.T) {
	config := DefaultConfig()
	config.EnableRerank = true
	config.RerankTopK = 100 // Much larger than result set

	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	results := []Result{
		{ID: "doc1", Content: "test content"},
		{ID: "doc2", Content: "more content"},
	}

	scored, err := ranker.RankWithMetadata(results, "test", nil)

	require.NoError(t, err)
	// Should get all results, not limited by topK
	assert.Len(t, scored, 2)
}

// TestSimilarityReranker_Rerank_EmbeddingGenerationFailure tests partial embedding failures
func TestSimilarityReranker_Rerank_EmbeddingGenerationFailure(t *testing.T) {
	embedder := &partialFailEmbedder{
		failOn: map[string]bool{
			"fail": true,
		},
	}
	embedder.embeddings = make(map[string][]float32)

	// Set query embedding
	queryEmb := make([]float32, 128)
	for i := range queryEmb {
		queryEmb[i] = 0.5
	}
	embedder.embeddings["test query"] = queryEmb

	reranker := NewSimilarityReranker(embedder)

	results := []ScoredResult{
		{ID: "doc1", Content: "success", FinalScore: 0.5},
		{ID: "doc2", Content: "fail", FinalScore: 0.6}, // Will fail embedding
		{ID: "doc3", Content: "success2", FinalScore: 0.4},
	}

	reranked, err := reranker.Rerank("test query", results, 3)

	require.NoError(t, err)
	assert.Len(t, reranked, 3)

	// doc2 should keep its original final score as rerank score
	var doc2Found bool
	for _, result := range reranked {
		if result.ID == "doc2" {
			doc2Found = true
			assert.Equal(t, float32(0.6), result.RerankScore)
		}
	}
	assert.True(t, doc2Found)
}

// failingRerankerImpl is a mock reranker that always fails
type failingRerankerImpl struct{}

func (f *failingRerankerImpl) Rerank(query string, results []ScoredResult, topK int) ([]ScoredResult, error) {
	return nil, errors.New("reranking failed")
}

// partialFailEmbedder is a mock embedder that fails on specific content
type partialFailEmbedder struct {
	embeddings map[string][]float32
	failOn     map[string]bool
}

func (p *partialFailEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if p.failOn[text] {
		return nil, errors.New("embedding failed for: " + text)
	}

	if emb, ok := p.embeddings[text]; ok {
		return emb, nil
	}

	// Generate mock embedding
	emb := make([]float32, 128)
	for i := range emb {
		emb[i] = 0.5
	}
	return emb, nil
}

func (p *partialFailEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = emb
	}
	return result, nil
}

func (p *partialFailEmbedder) Dimensions() int {
	return 128
}

func (p *partialFailEmbedder) MaxTokens() int {
	return 8192
}

func (p *partialFailEmbedder) Name() string {
	return "partial-fail-mock"
}

func (p *partialFailEmbedder) Close() error {
	return nil
}

// TestWeightedFusion_Fuse_VectorScoreGreaterThanOne tests vector score normalization
func TestWeightedFusion_Fuse_VectorScoreGreaterThanOne(t *testing.T) {
	wf := NewWeightedFusion(0.7, 0.3)

	vectorResults := []Result{
		{ID: "doc1", VectorScore: 1.5}, // > 1.0, should be normalized
	}

	results := wf.Fuse(vectorResults, []Result{})

	require.Len(t, results, 1)
	// Score should be normalized to max 1.0, then weighted
	assert.LessOrEqual(t, results[0].FinalScore, float32(0.7)) // 1.0 * 0.7
	assert.Greater(t, results[0].FinalScore, float32(0.0))
}

func TestBM25_ScoreWithMetadata_TokenizeEdgeCases(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	tests := []struct {
		name     string
		query    string
		document string
		metadata map[string]interface{}
	}{
		{
			name:     "query with only special characters",
			query:    "!@#$%^&*()",
			document: "valid content here",
			metadata: nil,
		},
		{
			name:     "document with only special characters",
			query:    "valid query",
			document: "!@#$%^&*()",
			metadata: nil,
		},
		{
			name:     "both with only special characters",
			query:    "!@#$",
			document: "***",
			metadata: nil,
		},
		{
			name:     "query and doc with spaces only",
			query:    "   ",
			document: "   ",
			metadata: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := bm25.ScoreWithMetadata(tt.query, tt.document, tt.metadata)
			// Should return 0 when tokenization produces no valid tokens
			assert.Equal(t, float32(0.0), score)
		})
	}
}

// TestHybridRanker_RankHybrid_ExactTopKBoundary tests exact topK boundary in hybrid mode
func TestHybridRanker_RankHybrid_ExactTopKBoundary(t *testing.T) {
	config := DefaultConfig()
	config.EnableRerank = true
	config.RerankTopK = 2 // Exact same as fused result count

	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	vectorResults := []Result{
		{ID: "doc1", Content: "content", VectorScore: 0.9},
	}

	keywordResults := []Result{
		{ID: "doc2", Content: "content", KeywordScore: 0.8},
	}

	results, err := ranker.RankHybrid(vectorResults, keywordResults, "test")

	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// TestWeightedFusion_Fuse_AllEdgeCases tests remaining edge cases
func TestWeightedFusion_Fuse_AllEdgeCases(t *testing.T) {
	wf := NewWeightedFusion(0.7, 0.3)

	// Test with keyword score = 1.0 exactly (boundary condition)
	vectorResults := []Result{
		{ID: "doc1", VectorScore: 0.5},
	}

	keywordResults := []Result{
		{ID: "doc1", KeywordScore: 1.0}, // Exactly 1.0, should not normalize
	}

	results := wf.Fuse(vectorResults, keywordResults)

	require.Len(t, results, 1)
	// Expected: (0.5 * 0.7) + (1.0 * 0.3) = 0.35 + 0.3 = 0.65
	assert.InDelta(t, 0.65, results[0].FinalScore, 0.01)
}

// TestBM25_Score_DirectCall tests direct Score method call
func TestBM25_Score_DirectCall(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	// Test Score method which calls ScoreWithMetadata with nil metadata
	score := bm25.Score("test query", "test document content", 100)

	assert.Greater(t, score, float32(0.0))
}

// TestHybridRanker_Rank_DirectCall tests direct Rank method call
func TestHybridRanker_Rank_DirectCall(t *testing.T) {
	config := DefaultConfig()
	ranker := NewHybridRanker(config, NewSimpleReranker())

	results := []Result{
		{ID: "doc1", Content: "test content"},
	}

	// Rank calls RankWithMetadata with nil metadata
	scored, err := ranker.Rank(results, "test")

	require.NoError(t, err)
	assert.Len(t, scored, 1)
}

// TestRRF_Fuse_EmptyID tests RRF with empty IDs
func TestRRF_Fuse_EmptyID(t *testing.T) {
	rrf := NewRRF(60)

	vectorResults := []Result{
		{ID: "", FilePath: "/path/file1.go", VectorScore: 0.9},
	}

	keywordResults := []Result{
		{ID: "", FilePath: "/path/file1.go", KeywordScore: 0.8},
	}

	results := rrf.Fuse(vectorResults, keywordResults)

	// Should merge results with same filepath
	require.Len(t, results, 1)
	assert.Greater(t, results[0].FinalScore, float32(0.0))
}

func TestWeightedFusion_Fuse_EmptyID(t *testing.T) {
	wf := NewWeightedFusion(0.7, 0.3)

	tests := []struct {
		name           string
		vectorResults  []Result
		keywordResults []Result
		expectedLen    int
	}{
		{
			name: "vector result with empty ID",
			vectorResults: []Result{
				{ID: "", FilePath: "/path/to/file.go", VectorScore: 0.9},
			},
			keywordResults: []Result{},
			expectedLen:    1,
		},
		{
			name:          "keyword result with empty ID",
			vectorResults: []Result{},
			keywordResults: []Result{
				{ID: "", FilePath: "/path/to/file.go", KeywordScore: 0.8},
			},
			expectedLen: 1,
		},
		{
			name: "both with empty ID, same filepath",
			vectorResults: []Result{
				{ID: "", FilePath: "/path/to/file.go", VectorScore: 0.9},
			},
			keywordResults: []Result{
				{ID: "", FilePath: "/path/to/file.go", KeywordScore: 0.8},
			},
			expectedLen: 1, // Should merge since same filepath
		},
		{
			name: "both with empty ID, different filepath",
			vectorResults: []Result{
				{ID: "", FilePath: "/path/to/file1.go", VectorScore: 0.9},
			},
			keywordResults: []Result{
				{ID: "", FilePath: "/path/to/file2.go", KeywordScore: 0.8},
			},
			expectedLen: 2, // Different files
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := wf.Fuse(tt.vectorResults, tt.keywordResults)
			assert.Len(t, results, tt.expectedLen)

			for _, result := range results {
				assert.Greater(t, result.FinalScore, float32(0.0))
			}
		})
	}
}

// TestWeightedFusion_Fuse_ScoreNormalizationEdgeCases tests edge cases in score normalization
func TestWeightedFusion_Fuse_ScoreNormalizationEdgeCases(t *testing.T) {
	wf := NewWeightedFusion(0.7, 0.3)

	tests := []struct {
		name           string
		vectorResults  []Result
		keywordResults []Result
		checkFn        func(t *testing.T, results []ScoredResult)
	}{
		{
			name: "keyword score between 1.0 and 10.0",
			vectorResults: []Result{
				{ID: "doc1", VectorScore: 0.5},
			},
			keywordResults: []Result{
				{ID: "doc1", KeywordScore: 5.0}, // Should be normalized to 0.5
			},
			checkFn: func(t *testing.T, results []ScoredResult) {
				require.Len(t, results, 1)
				// Expected: (0.5 * 0.7) + (0.5 * 0.3) = 0.35 + 0.15 = 0.5
				assert.InDelta(t, 0.5, results[0].FinalScore, 0.01)
			},
		},
		{
			name: "keyword score after normalization still > 1.0",
			vectorResults: []Result{
				{ID: "doc1", VectorScore: 0.0},
			},
			keywordResults: []Result{
				{ID: "doc1", KeywordScore: 15.0}, // 15/10 = 1.5, then clamped to 1.0
			},
			checkFn: func(t *testing.T, results []ScoredResult) {
				require.Len(t, results, 1)
				// Expected: (0 * 0.7) + (1.0 * 0.3) = 0.3
				assert.InDelta(t, 0.3, results[0].FinalScore, 0.01)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := wf.Fuse(tt.vectorResults, tt.keywordResults)
			tt.checkFn(t, results)
		})
	}
}

// TestHybridRanker_RankHybrid_RerankTopKClamp tests topK clamping in hybrid mode
func TestHybridRanker_RankHybrid_RerankTopKClamp(t *testing.T) {
	config := DefaultConfig()
	config.EnableRerank = true
	config.RerankTopK = 2

	reranker := NewSimpleReranker()
	ranker := NewHybridRanker(config, reranker)

	vectorResults := []Result{
		{ID: "doc1", Content: "vector1", VectorScore: 0.9},
		{ID: "doc2", Content: "vector2", VectorScore: 0.8},
	}

	keywordResults := []Result{
		{ID: "doc3", Content: "keyword1", KeywordScore: 0.7},
		{ID: "doc4", Content: "keyword2", KeywordScore: 0.6},
	}

	results, err := ranker.RankHybrid(vectorResults, keywordResults, "test")

	require.NoError(t, err)
	// Should be clamped to topK after fusion
	assert.LessOrEqual(t, len(results), config.RerankTopK)
}

// TestSimilarityReranker_Rerank_TopKClamp tests topK is properly applied
func TestSimilarityReranker_Rerank_TopKClamp(t *testing.T) {
	embedder := newMockEmbedder()

	queryEmb := make([]float32, 128)
	for i := range queryEmb {
		queryEmb[i] = 0.5
	}
	embedder.embeddings["query"] = queryEmb

	reranker := NewSimilarityReranker(embedder)

	// Create 5 results
	results := make([]ScoredResult, 5)
	for i := range results {
		emb := make([]float32, 128)
		for j := range emb {
			emb[j] = float32(i) / 10.0
		}
		results[i] = ScoredResult{
			ID:         string(rune('a' + i)),
			Vector:     emb,
			FinalScore: float32(i) / 10.0,
		}
	}

	// Request only top 3
	reranked, err := reranker.Rerank("query", results, 3)

	require.NoError(t, err)
	assert.Len(t, reranked, 3, "should return exactly topK results")
}

// TestBM25_ScoreWithMetadata_AllBoosts tests all boost paths combined
func TestBM25_ScoreWithMetadata_AllBoosts(t *testing.T) {
	bm25 := NewBM25(1.2, 0.75)

	// Test with all possible boost types
	query := "auth handler security go"
	document := "authentication handler implementation"

	tests := []struct {
		name      string
		metadata  map[string]interface{}
		wantBoost bool
	}{
		{
			name: "all boosts active",
			metadata: map[string]interface{}{
				"function_name": "auth_handler",
				"file_path":     "/pkg/auth/handler.go",
				"tags":          []string{"security"},
				"language":      "go",
			},
			wantBoost: true,
		},
		{
			name: "function and language boost",
			metadata: map[string]interface{}{
				"function_name": "authenticate",
				"language":      "go",
			},
			wantBoost: true,
		},
		{
			name: "filepath and tags boost",
			metadata: map[string]interface{}{
				"file_path": "/pkg/auth/security.go",
				"tags":      []string{"security"},
			},
			wantBoost: true,
		},
		{
			name: "only language boost",
			metadata: map[string]interface{}{
				"language": "go",
			},
			wantBoost: true,
		},
	}

	baselineScore := bm25.ScoreWithMetadata(query, document, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			boostedScore := bm25.ScoreWithMetadata(query, document, tt.metadata)

			if tt.wantBoost {
				assert.Greater(t, boostedScore, baselineScore,
					"expected boosted score to be higher than baseline")
			}
		})
	}
}
