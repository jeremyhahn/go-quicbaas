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

// Package embedding provides embedding provider adapters that delegate to the
// canonical implementation in github.com/jeremyhahn/go-qrdb/pkg/embedding.
// This package maintains the go-quicbaas Provider interface shape
// (single-text Embed, EmbedBatch) while the underlying implementation uses
// the go-qrdb batch-first Provider.
package embedding

import "errors"

// ErrVectorNotEnabled is returned when embedding operations are attempted
// without the vector and qrdb_vector build tags. The two travel together: vector
// compiles quicbaas's embedding layer and qrdb_vector the go-qrdb index it
// drives, and either one alone leaves the subsystem unusable.
var ErrVectorNotEnabled = errors.New("vector embedding support not enabled: build with -tags 'vector qrdb_vector'")

// Sentinel errors used throughout the embedding subsystem. These mirror the
// go-quicbaas provider error contract so that callers using errors.Is continue
// to work after the migration to go-qrdb providers.
var (
	ErrProviderNotFound    = errors.New("embedding provider not found")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrRateLimitExceeded   = errors.New("rate limit exceeded")
	ErrInvalidInput        = errors.New("invalid input")
	ErrDimensionsMismatch  = errors.New("dimensions mismatch")
	ErrTokenLimitExceeded  = errors.New("token limit exceeded")
	ErrModelNotFound       = errors.New("model not found")
	ErrProviderUnavailable = errors.New("provider unavailable")
)

// Configuration validation sentinel errors.
var (
	ErrProviderRequired     = errors.New("provider is required")
	ErrProviderInvalid      = errors.New("provider must be one of: voyage, ollama, lmstudio, openai")
	ErrAPIURLRequired       = errors.New("api_url is required")
	ErrAPIKeyRequired       = errors.New("api_key is required for Voyage AI")
	ErrModelRequired        = errors.New("model is required")
	ErrDimensionsPositive   = errors.New("dimensions must be positive")
	ErrMaxBatchSizePositive = errors.New("max_batch_size must be positive")
	ErrTimeoutPositive      = errors.New("timeout must be positive")
)
