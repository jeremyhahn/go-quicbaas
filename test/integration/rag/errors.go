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

//go:build integration && rag

package rag

import "errors"

// Test errors for typed error handling per CLAUDE.md conventions.
var (
	// ErrTestSetupFailed indicates test setup failed.
	ErrTestSetupFailed = errors.New("test setup failed")

	// ErrEmbeddingFailed indicates embedding generation failed.
	ErrEmbeddingFailed = errors.New("embedding generation failed")

	// ErrSearchFailed indicates vector search failed.
	ErrSearchFailed = errors.New("vector search failed")

	// ErrChunkingFailed indicates document chunking failed.
	ErrChunkingFailed = errors.New("document chunking failed")

	// ErrContextBuildFailed indicates context building failed.
	ErrContextBuildFailed = errors.New("context building failed")

	// ErrInvalidResult indicates an invalid test result.
	ErrInvalidResult = errors.New("invalid result")

	// ErrTimeout indicates a test timeout.
	ErrTimeout = errors.New("test timeout")
)
