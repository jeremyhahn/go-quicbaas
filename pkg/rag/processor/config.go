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

package processor

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/language"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/metadata"
)

// Config configures the document processor behavior.
type Config struct {
	// Strategy selection
	DefaultStrategy   string                       // "auto", "code", "text", "markdown"
	StrategyOverrides map[language.Language]string // Override strategy per language

	// Chunking configurations
	TextConfig     chunking.TextChunkerConfig
	CodeConfig     chunking.GoCodeChunkerConfig
	MarkdownConfig MarkdownChunkerConfig // Will be implemented in parallel

	// Metadata configuration
	MetadataConfig metadata.ExtractorConfig

	// Performance settings
	MaxConcurrency int // For batch processing (0 = NumCPU)

	// Processing options
	EnableDeduplication bool // Enable hash-based deduplication
	SkipBinaryFiles     bool // Skip binary file detection

	// Security settings
	AllowedDirs []string // Allowed directory prefixes for file access; empty allows all (with traversal check)
}

// MarkdownChunkerConfig defines configuration for the Markdown chunker.
type MarkdownChunkerConfig struct {
	ChunkSize         int  // Target size in tokens
	ChunkOverlap      int  // Overlap in tokens
	MinChunkSize      int  // Minimum chunk size
	RespectHeaders    bool // Break on headers
	RespectCodeBlocks bool // Keep code blocks intact
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultStrategy:     "auto",
		StrategyOverrides:   make(map[language.Language]string),
		TextConfig:          chunking.DefaultTextChunkerConfig(),
		CodeConfig:          chunking.DefaultGoCodeChunkerConfig(),
		MarkdownConfig:      DefaultMarkdownChunkerConfig(),
		MetadataConfig:      metadata.DefaultConfig(),
		MaxConcurrency:      0, // Use runtime.NumCPU()
		EnableDeduplication: true,
		SkipBinaryFiles:     true,
	}
}

// DefaultMarkdownChunkerConfig returns default markdown chunker configuration.
func DefaultMarkdownChunkerConfig() MarkdownChunkerConfig {
	return MarkdownChunkerConfig{
		ChunkSize:         512,
		ChunkOverlap:      51,
		MinChunkSize:      50,
		RespectHeaders:    true,
		RespectCodeBlocks: true,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	// Validate strategy
	validStrategies := map[string]bool{
		"auto": true, "code": true, "text": true, "markdown": true,
	}
	if !validStrategies[c.DefaultStrategy] {
		c.DefaultStrategy = "auto"
	}

	// Validate chunking configs
	if c.TextConfig.ChunkSize <= 0 {
		c.TextConfig = chunking.DefaultTextChunkerConfig()
	}
	if c.CodeConfig.MaxFunctionTokens <= 0 {
		c.CodeConfig = chunking.DefaultGoCodeChunkerConfig()
	}
	if c.MarkdownConfig.ChunkSize <= 0 {
		c.MarkdownConfig = DefaultMarkdownChunkerConfig()
	}

	// Validate concurrency
	if c.MaxConcurrency < 0 {
		c.MaxConcurrency = 0
	}

	return nil
}
