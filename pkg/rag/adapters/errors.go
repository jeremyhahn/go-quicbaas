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

// Package adapters provides adapter implementations that bridge RAG components
// to the pipeline interfaces, enabling clean separation between the ranking/context
// implementations and the pipeline orchestration layer.
package adapters

import "errors"

// Ranker adapter errors.
var (
	// ErrNilHybridRanker indicates the hybrid ranker was nil.
	ErrNilHybridRanker = errors.New("hybrid ranker cannot be nil")

	// ErrEmptyQuery indicates an empty query was provided.
	ErrEmptyQuery = errors.New("query cannot be empty")

	// ErrEmptyResults indicates no results were provided for ranking.
	ErrEmptyResults = errors.New("no results to rank")

	// ErrRankingFailed indicates the ranking operation failed.
	ErrRankingFailed = errors.New("ranking operation failed")
)

// Context builder adapter errors.
var (
	// ErrNilContextBuilder indicates the context builder was nil.
	ErrNilContextBuilder = errors.New("context builder cannot be nil")

	// ErrContextBuildFailed indicates context building failed.
	ErrContextBuildFailed = errors.New("context build operation failed")

	// ErrInvalidMaxTokens indicates invalid max tokens value.
	ErrInvalidMaxTokens = errors.New("max tokens must be positive")

	// ErrNoResultsProvided indicates no results were provided for context building.
	ErrNoResultsProvided = errors.New("no results provided for context building")
)

// Configuration errors.
var (
	// ErrInvalidConfig indicates invalid adapter configuration.
	ErrInvalidConfig = errors.New("invalid adapter configuration")
)
