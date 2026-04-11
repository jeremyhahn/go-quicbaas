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
	"github.com/jeremyhahn/go-qrdb/pkg/retry"
)

// Backoff defines the interface for backoff strategies.
type Backoff = retry.Backoff

// ExponentialBackoff implements exponential backoff with jitter.
type ExponentialBackoff = retry.ExponentialBackoff

// LinearBackoff implements linear backoff with optional jitter.
type LinearBackoff = retry.LinearBackoff

// ConstantBackoff implements constant backoff with optional jitter.
type ConstantBackoff = retry.ConstantBackoff

// DefaultExponentialBackoff returns a preconfigured exponential backoff
// with sensible defaults: 100ms initial, 10s max, 2x multiplier, 0.1 jitter.
func DefaultExponentialBackoff() *ExponentialBackoff {
	return retry.DefaultExponentialBackoff()
}

// DefaultLinearBackoff returns a preconfigured linear backoff
// with sensible defaults: 100ms initial, 5s max, 200ms increment, 0.1 jitter.
func DefaultLinearBackoff() *LinearBackoff {
	return retry.DefaultLinearBackoff()
}

// DefaultConstantBackoff returns a preconfigured constant backoff
// with sensible defaults: 500ms delay, 0.1 jitter.
func DefaultConstantBackoff() *ConstantBackoff {
	return retry.DefaultConstantBackoff()
}
