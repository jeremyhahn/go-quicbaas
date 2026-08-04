// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Retry re-exports. QuicBaaS is the single SDK frontage for the stack:
// downstream consumers (e.g. go-trusted-platform) must reach the shared qrdb
// retry utility through this SDK rather than importing go-qrdb directly.
// These are pure aliases — no transport state, no behavior.

// ---------------------------------------------------------------------------
// Backoff interface and strategy types
// ---------------------------------------------------------------------------

// Backoff defines the interface for retry backoff strategies.
type Backoff = qrdbsdk.Backoff

// ExponentialBackoff implements exponential backoff with optional jitter.
type ExponentialBackoff = qrdbsdk.ExponentialBackoff

// LinearBackoff implements linear backoff with optional jitter.
type LinearBackoff = qrdbsdk.LinearBackoff

// ConstantBackoff implements constant backoff with optional jitter.
type ConstantBackoff = qrdbsdk.ConstantBackoff

// ---------------------------------------------------------------------------
// Retry config and operation types
// ---------------------------------------------------------------------------

// RetryConfig configures retry behavior including max attempts, backoff
// strategy, retry predicate, and an optional per-attempt callback.
type RetryConfig = qrdbsdk.RetryConfig

// RetryOperation is a function that can be retried.
type RetryOperation = qrdbsdk.RetryOperation

// ---------------------------------------------------------------------------
// Default backoff constructors
// ---------------------------------------------------------------------------

// DefaultExponentialBackoff returns a preconfigured exponential backoff
// with sensible defaults: 100ms initial, 10s max, 2x multiplier, 0.1 jitter.
var DefaultExponentialBackoff = qrdbsdk.DefaultExponentialBackoff

// DefaultLinearBackoff returns a preconfigured linear backoff
// with sensible defaults: 100ms initial, 5s max, 200ms increment, 0.1 jitter.
var DefaultLinearBackoff = qrdbsdk.DefaultLinearBackoff

// DefaultConstantBackoff returns a preconfigured constant backoff
// with sensible defaults: 500ms delay, 0.1 jitter.
var DefaultConstantBackoff = qrdbsdk.DefaultConstantBackoff

// DefaultRetryConfig returns a retry config with sensible defaults.
var DefaultRetryConfig = qrdbsdk.DefaultRetryConfig

// ---------------------------------------------------------------------------
// Retry execution functions
// ---------------------------------------------------------------------------

// RetryDo executes the operation with retry logic according to the config.
var RetryDo = qrdbsdk.RetryDo

// RetryDoWithBackoff retries an operation with the specified max attempts
// and backoff strategy.
var RetryDoWithBackoff = qrdbsdk.RetryDoWithBackoff

// RetryDoExponential retries an operation with exponential backoff.
var RetryDoExponential = qrdbsdk.RetryDoExponential

// RetryDoLinear retries an operation with linear backoff.
var RetryDoLinear = qrdbsdk.RetryDoLinear

// RetryDoConstant retries an operation with constant backoff.
var RetryDoConstant = qrdbsdk.RetryDoConstant
