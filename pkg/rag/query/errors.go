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

import "errors"

// Common errors.
var (
	// ErrEmptyQuery is returned when the query is empty.
	ErrEmptyQuery = errors.New("query cannot be empty")

	// ErrInvalidOptions is returned when options are invalid.
	ErrInvalidOptions = errors.New("invalid query options")

	// ErrEmbeddingFailed is returned when embedding generation fails.
	ErrEmbeddingFailed = errors.New("embedding generation failed")

	// ErrSearchFailed is returned when search fails.
	ErrSearchFailed = errors.New("search failed")

	// ErrNoResults is returned when no results are found.
	ErrNoResults = errors.New("no results found")

	// ErrContextCanceled is returned when the context is canceled.
	ErrContextCanceled = errors.New("context canceled")

	// ErrTimeout is returned when the query times out.
	ErrTimeout = errors.New("query timeout")

	// ErrClosed is returned when the handler is closed.
	ErrClosed = errors.New("handler closed")

	// ErrNilEmbedder is returned when a nil embedder is provided.
	ErrNilEmbedder = errors.New("embedder cannot be nil")

	// ErrNilSearcher is returned when a nil searcher is provided.
	ErrNilSearcher = errors.New("searcher cannot be nil")

	// ErrQueryParseFailed is returned when query parsing fails.
	ErrQueryParseFailed = errors.New("query parse failed")

	// Config validation errors.

	// ErrInvalidCacheSize is returned when cache size is negative.
	ErrInvalidCacheSize = errors.New("cache size must be non-negative")

	// ErrInvalidCacheTTL is returned when cache TTL is not positive.
	ErrInvalidCacheTTL = errors.New("cache TTL must be positive")

	// ErrInvalidDefaultTopK is returned when default TopK is not positive.
	ErrInvalidDefaultTopK = errors.New("default TopK must be positive")

	// ErrInvalidDefaultMinScore is returned when default min score is out of range.
	ErrInvalidDefaultMinScore = errors.New("default min score must be between 0 and 1")

	// ErrInvalidDefaultNProbe is returned when default NProbe is not positive.
	ErrInvalidDefaultNProbe = errors.New("default NProbe must be positive")

	// ErrInvalidQueryTimeout is returned when query timeout is not positive.
	ErrInvalidQueryTimeout = errors.New("query timeout must be positive")

	// ErrInvalidCleanupInterval is returned when cleanup interval is not positive.
	ErrInvalidCleanupInterval = errors.New("cleanup interval must be positive")
)

// SentinelCauseError wraps a sentinel error with a cause using multi-unwrap.
type SentinelCauseError struct {
	Sentinel error
	Cause    error
}

// Error implements the error interface.
func (e *SentinelCauseError) Error() string {
	return e.Sentinel.Error() + ": " + e.Cause.Error()
}

// Unwrap returns both errors for multi-unwrap support (Go 1.20+).
func (e *SentinelCauseError) Unwrap() []error {
	return []error{e.Sentinel, e.Cause}
}
