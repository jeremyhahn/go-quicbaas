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
	"errors"
	"fmt"
)

// Validation errors for context building.
var (
	// ErrNoResults is returned when no results are provided to the builder.
	ErrNoResults = errors.New("no results provided")
)

// EmptyContentError is returned when a result at a specific index has empty content.
type EmptyContentError struct {
	Index int
}

// Error implements the error interface.
func (e *EmptyContentError) Error() string {
	return fmt.Sprintf("result %d has empty content", e.Index)
}

// Builder builds context from search results for LLM consumption.
type Builder interface {
	// Build creates formatted context from results. with default options
	Build(results []Result, maxTokens int) (*BuiltContext, error)

	// BuildWithTemplate creates context using a specific template.
	BuildWithTemplate(results []Result, template Template, maxTokens int) (*BuiltContext, error)

	// EstimateTokens estimates token count for results
	EstimateTokens(results []Result) int
}

// builder implements the Builder interface.
type builder struct {
	tokenizer Tokenizer
	config    Config
}

// NewBuilder creates a new context builder with default config.
func NewBuilder() Builder {
	config := DefaultConfig()
	return NewBuilderWithConfig(config)
}

// NewBuilderWithConfig creates a builder with custom config.
func NewBuilderWithConfig(config Config) Builder {
	if err := config.Validate(); err != nil {
		config = DefaultConfig()
	}

	return &builder{
		tokenizer: NewSimpleTokenizerWithMultiplier(config.TokenMultiplier),
		config:    config,
	}
}

// Build creates formatted context from results.
func (b *builder) Build(results []Result, maxTokens int) (*BuiltContext, error) {
	return b.BuildWithTemplate(results, b.config.DefaultTemplate, maxTokens)
}

// BuildWithTemplate creates context using a specific template.
func (b *builder) BuildWithTemplate(results []Result, template Template, maxTokens int) (*BuiltContext, error) {
	if maxTokens <= 0 {
		maxTokens = b.config.DefaultMaxTokens
	}

	// Limit results to max per chunk
	if len(results) > b.config.MaxResultsPerChunk {
		results = results[:b.config.MaxResultsPerChunk]
	}

	// Apply truncation strategy
	truncated := false
	originalCount := len(results)

	switch b.config.TruncationStrategy {
	case TruncationSmart:
		results = SmartTruncate(results, maxTokens, b.tokenizer)
		truncated = len(results) < originalCount

	case TruncationHard:
		// Calculate how many results fit
		fitted := FitResults(results, maxTokens, b.tokenizer)
		if len(fitted) < len(results) {
			truncated = true
			// Keep only fitted results
			fitResults := make([]Result, len(fitted))
			for i, idx := range fitted {
				fitResults[i] = results[idx]
			}
			results = fitResults
		}

	case TruncationNone:
		// No truncation
	}

	// Build citations if enabled
	var citations []Citation
	if b.config.IncludeCitations {
		citations = buildCitations(results)
	}

	// Get renderer for template
	renderer := GetRenderer(template)

	// Render content
	content := renderer.Render(results, citations, truncated)

	// Count actual tokens
	actualTokens := b.tokenizer.Count(content)

	return &BuiltContext{
		Content:   content,
		Tokens:    actualTokens,
		Results:   results,
		Truncated: truncated,
		Citations: citations,
	}, nil
}

// EstimateTokens estimates token count for results.
func (b *builder) EstimateTokens(results []Result) int {
	const metadataOverhead = 50 // Tokens per result for formatting
	const headerFooter = 200    // Tokens for header and footer

	totalTokens := headerFooter

	for _, result := range results {
		contentTokens := b.tokenizer.Count(result.Content)
		totalTokens += contentTokens + metadataOverhead
	}

	return totalTokens
}

// buildCitations creates citation list from results.
func buildCitations(results []Result) []Citation {
	citations := make([]Citation, len(results))

	for i, result := range results {
		citations[i] = Citation{
			Index:     i + 1,
			FilePath:  result.Metadata.FilePath,
			StartLine: result.Metadata.StartLine,
			EndLine:   result.Metadata.EndLine,
			Score:     result.FusionScore,
		}
	}

	return citations
}

// ValidateResults checks if results are valid.
func ValidateResults(results []Result) error {
	if len(results) == 0 {
		return ErrNoResults
	}

	for i, result := range results {
		if result.Content == "" {
			return &EmptyContentError{Index: i}
		}
	}

	return nil
}

// FilterEmptyResults removes results with empty content.
func FilterEmptyResults(results []Result) []Result {
	filtered := make([]Result, 0, len(results))

	for _, result := range results {
		if result.Content != "" {
			filtered = append(filtered, result)
		}
	}

	return filtered
}
