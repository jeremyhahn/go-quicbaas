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

// Package query provides query processing for the RAG pipeline.
// It handles parsing, caching, and managing user queries with support
// for both synchronous and streaming operations.
package query

import (
	"context"
	"time"
)

// Handler processes RAG queries end-to-end.
type Handler interface {
	// Query executes a complete RAG query and returns results.
	Query(ctx context.Context, query string, opts Options) (*Response, error)

	// StreamQuery executes a streaming RAG query.
	StreamQuery(ctx context.Context, query string, opts Options) (<-chan StreamChunk, error)

	// Close releases resources.
	Close() error
}

// Options configures query behavior.
type Options struct {
	// TopK is the number of results to retrieve (default: 10).
	TopK int

	// MinScore is the minimum similarity score (default: 0.7).
	MinScore float32

	// Filters are optional metadata filters.
	Filters map[string]interface{}

	// Rerank enables reranking (default: true).
	Rerank bool

	// IncludeSource includes source references (default: true).
	IncludeSource bool

	// Language is an optional language filter.
	Language string

	// NProbe is the IVF nprobe parameter (default: 20).
	NProbe int
}

// DefaultOptions returns Options with sensible defaults.
func DefaultOptions() Options {
	return Options{
		TopK:          10,
		MinScore:      0.7,
		Rerank:        true,
		IncludeSource: true,
		NProbe:        20,
	}
}

// Response represents a complete RAG query response.
type Response struct {
	// Query is the original query string.
	Query string

	// Results are the search results.
	Results []Result

	// Context is the built context for LLM.
	Context string

	// ProcessingTime is the total processing duration.
	ProcessingTime time.Duration

	// CacheHit indicates if the result was from cache.
	CacheHit bool
}

// Result represents a single search result.
type Result struct {
	// ID is the unique identifier.
	ID string

	// Score is the relevance score (0-1).
	Score float32

	// Content is the result content.
	Content string

	// Metadata contains additional information.
	Metadata map[string]interface{}
}

// StreamChunk represents a streaming response chunk.
type StreamChunk struct {
	// Result is the search result (if available).
	Result *Result

	// Done indicates the stream is complete.
	Done bool

	// Error contains any error that occurred.
	Error error
}
