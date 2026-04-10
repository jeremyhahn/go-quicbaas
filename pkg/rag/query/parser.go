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

package query

import (
	"strings"
	"unicode"
)

// ParsedQuery represents a parsed user query.
type ParsedQuery struct {
	// Original is the original query string.
	Original string

	// Keywords are extracted keywords.
	Keywords []string

	// IsCode indicates if the query is about code.
	IsCode bool

	// Language is the detected programming language (if IsCode is true).
	Language string
}

// ParseQuery extracts intent and keywords from user query.
// Target latency: <5ms.
func ParseQuery(query string) (*ParsedQuery, error) {
	if query == "" {
		return nil, ErrEmptyQuery
	}

	parsed := &ParsedQuery{
		Original: query,
		Keywords: extractKeywords(query),
		IsCode:   detectCode(query),
	}

	if parsed.IsCode {
		parsed.Language = detectLanguage(query)
	}

	return parsed, nil
}

// extractKeywords extracts meaningful keywords from query.
func extractKeywords(query string) []string {
	// Convert to lowercase
	query = strings.ToLower(query)

	// Split on whitespace and punctuation
	words := splitWords(query)

	// Filter stopwords and short words
	keywords := make([]string, 0, len(words))
	for _, word := range words {
		if !isStopword(word) && len(word) > 2 {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// splitWords splits text into words on whitespace and punctuation.
func splitWords(text string) []string {
	var words []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}

// detectCode checks if the query is about code.
func detectCode(query string) bool {
	query = strings.ToLower(query)

	// Check for code-related terms
	codeTerms := []string{
		"function", "method", "class", "interface",
		"implementation", "code", "example",
		"struct", "type", "package",
	}

	for _, term := range codeTerms {
		if strings.Contains(query, term) {
			return true
		}
	}

	// Check for code patterns (camelCase, snake_case, etc.)
	words := strings.Fields(query)
	for _, word := range words {
		if hasCamelCase(word) || hasSnakeCase(word) {
			return true
		}
	}

	return false
}

// detectLanguage detects programming language from query.
// Uses ordered checking to avoid false positives (e.g., "c" matching in "code").
func detectLanguage(query string) string {
	query = strings.ToLower(query)

	// Check in order from most specific to least specific
	// This avoids "c" matching in "code" or "authentication"

	// Multi-word languages first
	if strings.Contains(query, "c++") || strings.Contains(query, "cpp") {
		return "c++"
	}

	// Word boundary checks for single-letter languages
	words := strings.Fields(query)
	for _, word := range words {
		// Exact match for "c" as a standalone word
		if word == "c" {
			return "c"
		}
	}

	// Check other languages with unique keywords
	if strings.Contains(query, "golang") || containsWord(query, "go") {
		return "go"
	}
	if strings.Contains(query, "python") || containsWord(query, "py") {
		return "python"
	}
	if strings.Contains(query, "javascript") || containsWord(query, "js") || containsWord(query, "node") {
		return "javascript"
	}
	if strings.Contains(query, "java") {
		// Avoid matching "javascript"
		if !strings.Contains(query, "javascript") {
			return "java"
		}
	}
	if strings.Contains(query, "rust") {
		return "rust"
	}

	return ""
}

// containsWord checks if query contains word as a whole word (not substring).
func containsWord(query, word string) bool {
	words := strings.Fields(query)
	for _, w := range words {
		if w == word {
			return true
		}
	}
	return false
}

// hasCamelCase checks if word has camelCase pattern.
func hasCamelCase(word string) bool {
	hasLower := false
	hasUpper := false

	for _, r := range word {
		if unicode.IsLower(r) {
			hasLower = true
		}
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if hasLower && hasUpper {
			return true
		}
	}

	return false
}

// hasSnakeCase checks if word has snake_case pattern.
func hasSnakeCase(word string) bool {
	return strings.Contains(word, "_")
}

// isStopword checks if word is a common stopword.
func isStopword(word string) bool {
	stopwords := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true,
		"at": true, "be": true, "by": true, "for": true, "from": true,
		"has": true, "he": true, "in": true, "is": true, "it": true,
		"its": true, "of": true, "on": true, "that": true, "the": true,
		"to": true, "was": true, "will": true, "with": true,
		"how": true, "what": true, "where": true, "when": true, "why": true,
		"do": true, "does": true, "i": true, "can": true, "should": true,
	}

	return stopwords[word]
}
