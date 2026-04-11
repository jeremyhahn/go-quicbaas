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

package retry

import (
	"context"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-qrdb/pkg/retry"
)

// Operation is a function that can be retried.
// It should return an error if the operation fails.
type Operation = retry.Operation

// Config configures retry behavior.
type Config = retry.Config

// DefaultConfig returns a retry config with sensible defaults.
func DefaultConfig() *Config {
	return retry.DefaultConfig()
}

// Do executes the operation with retry logic according to the config.
// The operation is retried only if:
// - The error is retryable (according to RetryableFunc or QBError.IsRetryable())
// - MaxAttempts has not been exceeded
// - The context has not been canceled.
func Do(ctx context.Context, cfg *Config, op Operation) error {
	return retry.Do(ctx, cfg, op)
}

// DoWithBackoff is a convenience function that retries an operation with the specified
// max attempts and backoff strategy.
func DoWithBackoff(ctx context.Context, maxAttempts int, backoff Backoff, op Operation) error {
	return retry.DoWithBackoff(ctx, maxAttempts, backoff, op)
}

// DoExponential is a convenience function that retries an operation with exponential backoff.
func DoExponential(ctx context.Context, maxAttempts int, op Operation) error {
	return retry.DoExponential(ctx, maxAttempts, op)
}

// DoLinear is a convenience function that retries an operation with linear backoff.
func DoLinear(ctx context.Context, maxAttempts int, op Operation) error {
	return retry.DoLinear(ctx, maxAttempts, op)
}

// DoConstant is a convenience function that retries an operation with constant backoff.
func DoConstant(ctx context.Context, maxAttempts int, op Operation) error {
	return retry.DoConstant(ctx, maxAttempts, op)
}

// shouldRetry determines if an error is retryable based on the config.
// This unexported helper exists to support white-box tests.
func shouldRetry(cfg *Config, err error) bool {
	if err == nil {
		return false
	}
	if cfg.RetryableFunc != nil {
		return cfg.RetryableFunc(err)
	}
	return qberrors.IsRetryable(err)
}
