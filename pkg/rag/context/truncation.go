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
)

// TruncationStrategy defines how to truncate results.
type TruncationStrategy string

const (
	// TruncationSmart truncates at word boundaries.
	TruncationSmart TruncationStrategy = "smart"

	// TruncationHard truncates at exact token limit.
	TruncationHard TruncationStrategy = "hard"

	// TruncationNone disables truncation.
	TruncationNone TruncationStrategy = "none"
)

// SmartTruncate fits results within token limit while preserving quality
// Returns truncated results that fit within maxTokens.
func SmartTruncate(results []Result, maxTokens int, tokenizer Tokenizer) []Result {
	if len(results) == 0 {
		return results
	}

	// Reserve tokens for formatting overhead (~200 tokens)
	const formattingOverhead = 200
	availableTokens := maxTokens - formattingOverhead

	if availableTokens <= 0 {
		// If maxTokens is too low, return at least 1 result
		return results[:1]
	}

	truncated := make([]Result, 0, len(results))
	currentTokens := 0

	for _, result := range results {
		// Estimate tokens for this result
		resultTokens := tokenizer.Count(result.Content)

		// Add overhead for metadata formatting (~50 tokens per result)
		const metadataOverhead = 50
		totalResultTokens := resultTokens + metadataOverhead

		// Check if we can fit this result
		if currentTokens+totalResultTokens > availableTokens {
			// If we have at least one result, stop here
			if len(truncated) > 0 {
				break
			}
			// If this is the first result and it's too large, truncate its content
			result.Content = TruncateContent(result.Content, availableTokens-metadataOverhead, tokenizer)
			truncated = append(truncated, result)
			break
		}

		truncated = append(truncated, result)
		currentTokens += totalResultTokens
	}

	// Ensure at least 1 result is included
	if len(truncated) == 0 && len(results) > 0 {
		result := results[0]
		result.Content = TruncateContent(result.Content, availableTokens/2, tokenizer)
		truncated = append(truncated, result)
	}

	return truncated
}

// TruncateContent truncates content to fit within maxTokens
// Truncates at word boundary and adds "..." if truncated.
func TruncateContent(content string, maxTokens int, tokenizer Tokenizer) string {
	if maxTokens <= 0 {
		return ""
	}

	currentTokens := tokenizer.Count(content)
	if currentTokens <= maxTokens {
		return content
	}

	// Estimate how many words we can keep
	words := strings.Fields(content)
	if len(words) == 0 {
		return content
	}

	// Binary search for the right number of words
	left, right := 0, len(words)
	bestFit := 0

	for left <= right {
		mid := (left + right) / 2
		truncated := strings.Join(words[:mid], " ")
		tokens := tokenizer.Count(truncated)

		if tokens <= maxTokens {
			bestFit = mid
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	if bestFit == 0 {
		return "..."
	}

	truncated := strings.Join(words[:bestFit], " ")
	if bestFit < len(words) {
		truncated += "..."
	}

	return truncated
}

// TruncateHard truncates content at exact character position.
func TruncateHard(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}

	if maxChars < 3 {
		return content[:maxChars]
	}

	return content[:maxChars-3] + "..."
}

// FitResults returns indices of results that fit within token limit.
func FitResults(results []Result, maxTokens int, tokenizer Tokenizer) []int {
	const formattingOverhead = 200
	const metadataOverhead = 50

	availableTokens := maxTokens - formattingOverhead
	if availableTokens <= 0 {
		if len(results) > 0 {
			return []int{0}
		}
		return []int{}
	}

	fitted := make([]int, 0)
	currentTokens := 0

	for i, result := range results {
		resultTokens := tokenizer.Count(result.Content) + metadataOverhead

		if currentTokens+resultTokens > availableTokens {
			if len(fitted) > 0 {
				break
			}
			// Include at least first result
			return []int{0}
		}

		fitted = append(fitted, i)
		currentTokens += resultTokens
	}

	return fitted
}
