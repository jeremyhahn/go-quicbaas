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

package context

import (
	"strings"
	"unicode"
)

// Tokenizer estimates token counts for text.
type Tokenizer interface {
	// Count estimates token count for text
	Count(text string) int

	// Estimate is an alias for Count
	Estimate(text string) int
}

// SimpleTokenizer implements word-based token estimation.
// Uses 1.3x multiplier for GPT-style tokenization approximation.
type SimpleTokenizer struct {
	multiplier float32
}

// NewSimpleTokenizer creates a tokenizer with default 1.3x multiplier.
func NewSimpleTokenizer() *SimpleTokenizer {
	return &SimpleTokenizer{
		multiplier: 1.3,
	}
}

// NewSimpleTokenizerWithMultiplier creates a tokenizer with custom multiplier.
func NewSimpleTokenizerWithMultiplier(multiplier float32) *SimpleTokenizer {
	return &SimpleTokenizer{
		multiplier: multiplier,
	}
}

// Count estimates token count based on word count
// Research shows GPT tokenizers average ~1.3 tokens per word.
func (t *SimpleTokenizer) Count(text string) int {
	if text == "" {
		return 0
	}

	// Count words (split on whitespace)
	words := countWords(text)

	// Apply multiplier for subword tokenization
	return int(float32(words) * t.multiplier)
}

// Estimate is an alias for Count.
func (t *SimpleTokenizer) Estimate(text string) int {
	return t.Count(text)
}

// countWords counts words in text more accurately than strings.Fields.
// Handles code, punctuation, and special characters better.
func countWords(text string) int {
	words := 0
	inWord := false

	for _, r := range text {
		if unicode.IsSpace(r) {
			if inWord {
				words++
				inWord = false
			}
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			inWord = true
		} else {
			// Punctuation counts as a separator
			if inWord {
				words++
				inWord = false
			}
			// Most punctuation counts as a token
			if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
				words++
			}
		}
	}

	// Add final word if text doesn't end with separator
	if inWord {
		words++
	}

	return words
}

// EstimateTokensFromWords estimates tokens from word count.
func EstimateTokensFromWords(wordCount int) int {
	return int(float32(wordCount) * 1.3)
}

// CountWordsSimple provides a simple word count (for testing).
func CountWordsSimple(text string) int {
	return len(strings.Fields(text))
}
