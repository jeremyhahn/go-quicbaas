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

package ranking

import "errors"

var (
	// ErrInvalidConfig indicates invalid configuration.
	ErrInvalidConfig = errors.New("invalid ranking configuration")

	// ErrEmptyQuery indicates an empty query string.
	ErrEmptyQuery = errors.New("query string cannot be empty")

	// ErrEmptyResults indicates no results to rank.
	ErrEmptyResults = errors.New("no results to rank")

	// ErrInvalidTopK indicates invalid top-k parameter.
	ErrInvalidTopK = errors.New("topK must be greater than 0")

	// ErrRerankFailed indicates reranking operation failed.
	ErrRerankFailed = errors.New("reranking failed")

	// ErrFusionFailed indicates fusion operation failed.
	ErrFusionFailed = errors.New("fusion failed")
)
