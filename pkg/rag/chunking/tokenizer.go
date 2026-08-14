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

package chunking

import (
	"strings"
	"unicode"
)

// Tokenizer provides token counting capabilities.
type Tokenizer interface {
	Count(text string) int
	Estimate(text string) int
}

// SimpleTokenizer implements basic whitespace-based token counting.
type SimpleTokenizer struct{}

// NewSimpleTokenizer creates a new simple tokenizer.
func NewSimpleTokenizer() *SimpleTokenizer {
	return &SimpleTokenizer{}
}

// Count returns the estimated token count for the given text.
// Uses word count + punctuation estimation as a simple approximation.
func (t *SimpleTokenizer) Count(text string) int {
	if text == "" {
		return 0
	}

	// Count words
	words := 0
	inWord := false

	for _, r := range text {
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			words++
			inWord = true
		}
	}

	// Add ~15% for punctuation and special tokens
	// This is a reasonable approximation for most English text
	return int(float64(words) * 1.15)
}

// Estimate is an alias for Count in the simple tokenizer.
func (t *SimpleTokenizer) Estimate(text string) int {
	return t.Count(text)
}

// CountWords returns just the word count without token estimation.
func (t *SimpleTokenizer) CountWords(text string) int {
	return len(strings.Fields(text))
}
