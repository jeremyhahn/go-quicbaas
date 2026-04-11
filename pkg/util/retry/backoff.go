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

// Package retry provides retry logic with configurable backoff strategies.
// This package is a facade over github.com/jeremyhahn/go-qrdb/pkg/retry.
package retry

import (
	sdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Backoff defines the interface for backoff strategies.
type Backoff = sdk.Backoff

// ExponentialBackoff implements exponential backoff with jitter.
type ExponentialBackoff = sdk.ExponentialBackoff

// LinearBackoff implements linear backoff with optional jitter.
type LinearBackoff = sdk.LinearBackoff

// ConstantBackoff implements constant backoff with optional jitter.
type ConstantBackoff = sdk.ConstantBackoff

// DefaultExponentialBackoff returns a preconfigured exponential backoff
// with sensible defaults: 100ms initial, 10s max, 2x multiplier, 0.1 jitter.
func DefaultExponentialBackoff() *ExponentialBackoff {
	return sdk.DefaultExponentialBackoff()
}

// DefaultLinearBackoff returns a preconfigured linear backoff
// with sensible defaults: 100ms initial, 5s max, 200ms increment, 0.1 jitter.
func DefaultLinearBackoff() *LinearBackoff {
	return sdk.DefaultLinearBackoff()
}

// DefaultConstantBackoff returns a preconfigured constant backoff
// with sensible defaults: 500ms delay, 0.1 jitter.
func DefaultConstantBackoff() *ConstantBackoff {
	return sdk.DefaultConstantBackoff()
}
