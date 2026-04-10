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

func TestNewRRF(t *testing.T) {
	rrf := NewRRF(60)
	require.NotNil(t, rrf)
	assert.Equal(t, float32(60), rrf.k)
}

func TestRRF_Fuse(t *testing.T) {
	rrf := NewRRF(60)

	tests := []struct {
		name           string
		vectorResults  []Result
		keywordResults []Result
		expectedLen    int
		checkFirst     bool
		firstID        string
	}{
		{
			name: "simple fusion",
			vectorResults: []Result{
				{ID: "doc1", Content: "content1", VectorScore: 0.9},
				{ID: "doc2", Content: "content2", VectorScore: 0.8},
			},
			keywordResults: []Result{
				{ID: "doc2", Content: "content2", KeywordScore: 0.7},
				{ID: "doc3", Content: "content3", KeywordScore: 0.6},
			},
			expectedLen: 3,
			checkFirst:  true,
			firstID:     "doc2", // doc2 appears in both lists
		},
		{
			name: "vector only",
			vectorResults: []Result{
				{ID: "doc1", VectorScore: 0.9},
				{ID: "doc2", VectorScore: 0.8},
			},
			keywordResults: []Result{},
			expectedLen:    2,
			checkFirst:     false,
		},
		{
			name:          "keyword only",
			vectorResults: []Result{},
			keywordResults: []Result{
				{ID: "doc1", KeywordScore: 0.9},
				{ID: "doc2", KeywordScore: 0.8},
			},
			expectedLen: 2,
			checkFirst:  false,
		},
		{
			name:           "both empty",
			vectorResults:  []Result{},
			keywordResults: []Result{},
			expectedLen:    0,
			checkFirst:     false,
		},
		{
			name: "no overlap",
			vectorResults: []Result{
				{ID: "doc1", VectorScore: 0.9},
				{ID: "doc2", VectorScore: 0.8},
			},
			keywordResults: []Result{
				{ID: "doc3", KeywordScore: 0.7},
				{ID: "doc4", KeywordScore: 0.6},
			},
			expectedLen: 4,
			checkFirst:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := rrf.Fuse(tt.vectorResults, tt.keywordResults)

			assert.Len(t, results, tt.expectedLen)

			if tt.checkFirst && len(results) > 0 {
				assert.Equal(t, tt.firstID, results[0].ID)
			}

			// Verify scores are set
			for i, result := range results {
				assert.Greater(t, result.FinalScore, float32(0.0), "result %d should have positive score", i)
			}

			// Verify results are sorted by score descending
			for i := 1; i < len(results); i++ {
				assert.GreaterOrEqual(t, results[i-1].FinalScore, results[i].FinalScore,
					"results should be sorted by score descending")
			}
		})
	}
}

func TestRRF_FuseWithFilePath(t *testing.T) {
	// Test fusion when ID is empty and FilePath is used as fallback
	rrf := NewRRF(60)

	vectorResults := []Result{
		{FilePath: "/pkg/auth/handler.go", Content: "content1", VectorScore: 0.9},
		{FilePath: "/pkg/db/connection.go", Content: "content2", VectorScore: 0.8},
	}

	keywordResults := []Result{
		{FilePath: "/pkg/auth/handler.go", Content: "content1", KeywordScore: 0.7},
		{FilePath: "/pkg/api/server.go", Content: "content3", KeywordScore: 0.6},
	}

	results := rrf.Fuse(vectorResults, keywordResults)

	require.Len(t, results, 3)

	// First result should be the one that appears in both lists
	assert.Equal(t, "/pkg/auth/handler.go", results[0].FilePath)
	assert.Greater(t, results[0].FinalScore, results[1].FinalScore)
}

func TestRRF_ScoreCalculation(t *testing.T) {
	// Test that RRF score is calculated correctly
	rrf := NewRRF(60)

	vectorResults := []Result{
		{ID: "doc1", VectorScore: 0.9}, // rank 0
	}

	keywordResults := []Result{
		{ID: "doc1", KeywordScore: 0.8}, // rank 0
	}

	results := rrf.Fuse(vectorResults, keywordResults)

	require.Len(t, results, 1)

	// Expected RRF score: 1/(60+0) + 1/(60+0) = 2/60 = 0.0333...
	expectedScore := float32(2.0 / 60.0)
	assert.InDelta(t, expectedScore, results[0].FinalScore, 0.0001)
}

func TestNewWeightedFusion(t *testing.T) {
	wf := NewWeightedFusion(0.7, 0.3)
	require.NotNil(t, wf)
	assert.Equal(t, float32(0.7), wf.vectorWeight)
	assert.Equal(t, float32(0.3), wf.keywordWeight)
}

func TestWeightedFusion_Fuse(t *testing.T) {
	wf := NewWeightedFusion(0.7, 0.3)

	tests := []struct {
		name           string
		vectorResults  []Result
		keywordResults []Result
		expectedLen    int
	}{
		{
			name: "simple weighted fusion",
			vectorResults: []Result{
				{ID: "doc1", Content: "content1", VectorScore: 0.9},
				{ID: "doc2", Content: "content2", VectorScore: 0.8},
			},
			keywordResults: []Result{
				{ID: "doc2", Content: "content2", KeywordScore: 0.7},
				{ID: "doc3", Content: "content3", KeywordScore: 0.6},
			},
			expectedLen: 3,
		},
		{
			name: "vector only",
			vectorResults: []Result{
				{ID: "doc1", VectorScore: 0.9},
				{ID: "doc2", VectorScore: 0.8},
			},
			keywordResults: []Result{},
			expectedLen:    2,
		},
		{
			name:          "keyword only",
			vectorResults: []Result{},
			keywordResults: []Result{
				{ID: "doc1", KeywordScore: 0.9},
				{ID: "doc2", KeywordScore: 0.8},
			},
			expectedLen: 2,
		},
		{
			name:           "both empty",
			vectorResults:  []Result{},
			keywordResults: []Result{},
			expectedLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := wf.Fuse(tt.vectorResults, tt.keywordResults)

			assert.Len(t, results, tt.expectedLen)

			// Verify scores are set and within valid range
			for i, result := range results {
				assert.GreaterOrEqual(t, result.FinalScore, float32(0.0), "result %d score should be non-negative", i)
				assert.LessOrEqual(t, result.FinalScore, float32(1.0), "result %d score should be <= 1.0", i)
			}

			// Verify results are sorted by score descending
			for i := 1; i < len(results); i++ {
				assert.GreaterOrEqual(t, results[i-1].FinalScore, results[i].FinalScore,
					"results should be sorted by score descending")
			}
		})
	}
}

func TestWeightedFusion_ScoreCalculation(t *testing.T) {
	wf := NewWeightedFusion(0.7, 0.3)

	vectorResults := []Result{
		{ID: "doc1", VectorScore: 0.9}, // weighted: 0.9 * 0.7 = 0.63
	}

	keywordResults := []Result{
		{ID: "doc1", KeywordScore: 0.6}, // weighted: 0.6 * 0.3 = 0.18
	}

	results := wf.Fuse(vectorResults, keywordResults)

	require.Len(t, results, 1)

	// Expected score: 0.63 + 0.18 = 0.81
	expectedScore := float32(0.81)
	assert.InDelta(t, expectedScore, results[0].FinalScore, 0.01)
}

func TestWeightedFusion_ScoreNormalization(t *testing.T) {
	// Test that high BM25 scores are normalized
	wf := NewWeightedFusion(0.7, 0.3)

	vectorResults := []Result{
		{ID: "doc1", VectorScore: 0.9},
	}

	keywordResults := []Result{
		{ID: "doc1", KeywordScore: 15.0}, // High BM25 score that needs normalization
	}

	results := wf.Fuse(vectorResults, keywordResults)

	require.Len(t, results, 1)

	// Score should be normalized and within [0, 1]
	assert.GreaterOrEqual(t, results[0].FinalScore, float32(0.0))
	assert.LessOrEqual(t, results[0].FinalScore, float32(1.0))
}

func BenchmarkRRF_Fuse(b *testing.B) {
	rrf := NewRRF(60)

	vectorResults := make([]Result, 100)
	for i := range vectorResults {
		vectorResults[i] = Result{
			ID:          string(rune(i)),
			VectorScore: float32(100-i) / 100.0,
		}
	}

	keywordResults := make([]Result, 100)
	for i := range keywordResults {
		keywordResults[i] = Result{
			ID:           string(rune(i + 50)),
			KeywordScore: float32(100-i) / 100.0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rrf.Fuse(vectorResults, keywordResults)
	}
}

func BenchmarkWeightedFusion_Fuse(b *testing.B) {
	wf := NewWeightedFusion(0.7, 0.3)

	vectorResults := make([]Result, 100)
	for i := range vectorResults {
		vectorResults[i] = Result{
			ID:          string(rune(i)),
			VectorScore: float32(100-i) / 100.0,
		}
	}

	keywordResults := make([]Result, 100)
	for i := range keywordResults {
		keywordResults[i] = Result{
			ID:           string(rune(i + 50)),
			KeywordScore: float32(100-i) / 100.0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wf.Fuse(vectorResults, keywordResults)
	}
}
