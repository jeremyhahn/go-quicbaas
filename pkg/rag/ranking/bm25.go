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
	"math"
	"strings"
	"unicode"
)

// BM25 implements the BM25 ranking algorithm for keyword-based scoring.
type BM25 struct {
	k1 float32 // Term frequency saturation parameter
	b  float32 // Length normalization parameter
}

// NewBM25 creates a new BM25 scorer with the given parameters.
func NewBM25(k1, b float32) *BM25 {
	return &BM25{
		k1: k1,
		b:  b,
	}
}

// Score calculates the BM25 score for a query-document pair.
func (bm *BM25) Score(query string, document string, avgDocLen float32) float32 {
	return bm.ScoreWithMetadata(query, document, nil)
}

// ScoreWithMetadata calculates BM25 score with metadata boosting.
func (bm *BM25) ScoreWithMetadata(query string, doc string, metadata map[string]interface{}) float32 {
	if query == "" || doc == "" {
		return 0.0
	}

	// Tokenize query and document
	queryTerms := tokenize(query)
	docTerms := tokenize(doc)

	if len(queryTerms) == 0 || len(docTerms) == 0 {
		return 0.0
	}

	// Calculate document length
	docLen := float32(len(docTerms))

	// Use actual doc length as average if not provided
	avgDocLen := docLen
	if avgDocLen == 0 {
		avgDocLen = 100 // reasonable default
	}

	// Build term frequency map
	termFreq := make(map[string]int)
	for _, term := range docTerms {
		termFreq[term]++
	}

	// Build document frequency map (simplified: assume 1000 docs, each term appears in 10% of docs)
	// In production, this should be maintained across the corpus
	const totalDocs = 1000
	const defaultDF = 100

	var score float64
	for _, term := range queryTerms {
		tf := float64(termFreq[term])
		if tf == 0 {
			continue
		}

		// Calculate IDF component
		df := float64(defaultDF)
		idf := math.Log((float64(totalDocs)-df+0.5)/(df+0.5) + 1.0)

		// Calculate normalized TF component
		k1 := float64(bm.k1)
		b := float64(bm.b)
		tfNorm := (tf * (k1 + 1)) / (tf + k1*(1-b+b*float64(docLen)/float64(avgDocLen)))

		score += idf * tfNorm
	}

	// Apply metadata boosting
	boost := bm.calculateMetadataBoost(query, metadata)
	score *= boost

	return float32(score)
}

// BatchScore scores multiple documents against a query.
func (bm *BM25) BatchScore(query string, documents []string, avgDocLen float32) []float32 {
	scores := make([]float32, len(documents))
	for i, doc := range documents {
		scores[i] = bm.Score(query, doc, avgDocLen)
	}
	return scores
}

// calculateMetadataBoost applies boosting based on metadata matches.
func (bm *BM25) calculateMetadataBoost(query string, metadata map[string]interface{}) float64 {
	if metadata == nil {
		return 1.0
	}

	boost := 1.0
	queryTerms := tokenize(query)

	// Boost for function name matches (highest priority)
	if funcName, ok := metadata["function_name"].(string); ok && funcName != "" {
		funcTerms := tokenize(funcName)
		if containsAny(queryTerms, funcTerms) {
			boost *= 1.2
		}
	}

	// Boost for file path matches
	if filePath, ok := metadata["file_path"].(string); ok && filePath != "" {
		pathTerms := tokenize(filePath)
		if containsAny(queryTerms, pathTerms) {
			boost *= 1.1
		}
	}

	// Boost for tag/keyword matches
	if tags, ok := metadata["tags"].([]string); ok {
		for _, tag := range tags {
			tagTerms := tokenize(tag)
			if containsAny(queryTerms, tagTerms) {
				boost *= 1.15
				break
			}
		}
	}

	// Boost for language matches
	if lang, ok := metadata["language"].(string); ok && lang != "" {
		langTerms := tokenize(lang)
		if containsAny(queryTerms, langTerms) {
			boost *= 1.05
		}
	}

	return boost
}

// tokenize splits text into lowercase tokens.
func tokenize(text string) []string {
	// Convert to lowercase and split on non-alphanumeric characters
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			current.WriteRune(unicode.ToLower(r))
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// containsAny checks if any term from source appears in target.
func containsAny(source, target []string) bool {
	targetSet := make(map[string]bool)
	for _, t := range target {
		targetSet[t] = true
	}

	for _, s := range source {
		if targetSet[s] {
			return true
		}
	}

	return false
}
