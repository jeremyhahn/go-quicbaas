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

package pipeline

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"math/big"
	"time"
)

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
	EnableJitter   bool

	// wait blocks for d, or returns ctx.Err() if the context is cancelled
	// first. A nil wait means real time, which is what every caller gets.
	//
	// It exists so a test can observe the backoff this package COMPUTES
	// without spending it. The schedule -- doubling, and the MaxBackoff cap --
	// is arithmetic, and asserting it by measuring elapsed wall-clock made the
	// tests fail whenever the scheduler was slower than the bound they allowed
	// (go-quicbaas#423). Unexported, so it is not part of the API and no
	// caller can set it.
	wait func(ctx context.Context, d time.Duration) error

	// entropy supplies the jitter. Nil means crypto/rand.Reader. Unexported
	// for the same reason as wait: tests inject a failing reader.
	entropy io.Reader
}

// waitRealTime is the default RetryConfig.wait: sleep for d unless the context
// ends first.
func waitRealTime(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DefaultRetryConfig returns default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     5 * time.Second,
		Multiplier:     2.0,
		EnableJitter:   false,
	}
}

// RetryWithBackoff retries the given function with exponential backoff.
func RetryWithBackoff(ctx context.Context, config RetryConfig, fn func() error) error {
	if config.MaxAttempts <= 0 {
		return ErrInvalidMaxAttempts
	}

	var lastErr error
	backoff := config.InitialBackoff

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// Apply backoff delay (except for first attempt)
		if attempt > 0 {
			delay := backoff
			if config.EnableJitter {
				jittered, err := jitteredDelay(backoff, config.entropy)
				if err != nil {
					return err
				}
				delay = jittered
			}

			wait := config.wait
			if wait == nil {
				wait = waitRealTime
			}
			if err := wait(ctx, delay); err != nil {
				return err
			}

			// Calculate next backoff
			backoff = time.Duration(float64(backoff) * config.Multiplier)
			if backoff > config.MaxBackoff {
				backoff = config.MaxBackoff
			}
		}

		// Execute function
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryable(err) {
			return err
		}
	}

	return lastErr
}

// jitteredDelay returns a uniformly random delay in [0.5, 1.5) times backoff,
// drawn from entropy (crypto/rand.Reader when nil).
func jitteredDelay(backoff time.Duration, entropy io.Reader) (time.Duration, error) {
	if backoff <= 0 {
		return backoff, nil
	}
	if entropy == nil {
		entropy = rand.Reader
	}
	n, err := rand.Int(entropy, big.NewInt(int64(backoff)))
	if err != nil {
		return 0, &RetryJitterError{Cause: err}
	}
	return backoff/2 + time.Duration(n.Int64()), nil
}

// isRetryable determines if an error should be retried.
func isRetryable(err error) bool {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return true
	case errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, ErrResourceExhausted):
		return true
	case errors.Is(err, ErrCircuitBreakerOpen):
		return false
	case errors.Is(err, ErrInvalidConfiguration):
		return false
	case errors.Is(err, ErrMissingDependency):
		return false
	case errors.Is(err, ErrPipelineClosed):
		return false
	default:
		// By default, retry generic errors
		return true
	}
}
