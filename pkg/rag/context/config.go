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

// Config configures the context builder.
type Config struct {
	// DefaultMaxTokens is the default maximum token limit
	DefaultMaxTokens int

	// DefaultTemplate is the default output template
	DefaultTemplate Template

	// IncludeMetadata controls whether to include metadata in output
	IncludeMetadata bool

	// IncludeCitations controls whether to include source citations
	IncludeCitations bool

	// MaxResultsPerChunk limits results per context
	MaxResultsPerChunk int

	// TruncationStrategy defines how to handle truncation
	TruncationStrategy TruncationStrategy

	// TokenMultiplier for word-to-token estimation (default 1.3)
	TokenMultiplier float32
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		DefaultMaxTokens:   8000,
		DefaultTemplate:    TemplateMarkdown,
		IncludeMetadata:    true,
		IncludeCitations:   true,
		MaxResultsPerChunk: 10,
		TruncationStrategy: TruncationSmart,
		TokenMultiplier:    1.3,
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.DefaultMaxTokens <= 0 {
		c.DefaultMaxTokens = 8000
	}

	if c.MaxResultsPerChunk <= 0 {
		c.MaxResultsPerChunk = 10
	}

	if c.TokenMultiplier <= 0 {
		c.TokenMultiplier = 1.3
	}

	if c.DefaultTemplate == "" {
		c.DefaultTemplate = TemplateMarkdown
	}

	if c.TruncationStrategy == "" {
		c.TruncationStrategy = TruncationSmart
	}

	return nil
}

// WithMaxTokens sets the max tokens.
func (c Config) WithMaxTokens(tokens int) Config {
	c.DefaultMaxTokens = tokens
	return c
}

// WithTemplate sets the template.
func (c Config) WithTemplate(template Template) Config {
	c.DefaultTemplate = template
	return c
}

// WithMetadata sets whether to include metadata.
func (c Config) WithMetadata(include bool) Config {
	c.IncludeMetadata = include
	return c
}

// WithCitations sets whether to include citations.
func (c Config) WithCitations(include bool) Config {
	c.IncludeCitations = include
	return c
}

// WithTruncationStrategy sets the truncation strategy.
func (c Config) WithTruncationStrategy(strategy TruncationStrategy) Config {
	c.TruncationStrategy = strategy
	return c
}
