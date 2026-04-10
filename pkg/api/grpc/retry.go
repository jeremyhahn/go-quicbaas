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

package grpc

import (
	"context"
	stderrors "errors"
	"math"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// retryableGRPCCodes defines which gRPC status codes should trigger a retry.
var retryableGRPCCodes = map[codes.Code]bool{
	codes.Unavailable:       true, // Service unavailable, worth retrying
	codes.DeadlineExceeded:  true, // Request timeout, might succeed next time
	codes.ResourceExhausted: true, // Rate limited, back off and retry
	codes.Aborted:           true, // Transaction conflict, retry might succeed
	codes.Internal:          true, // Internal error, might be transient
}

// isRetryableError determines if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a gRPC status error
	if s, ok := status.FromError(err); ok {
		return retryableGRPCCodes[s.Code()]
	}

	// Check if it's a QuicBaaS error code.
	var dbErr *errors.DragonError
	if stderrors.As(err, &dbErr) {
		return dbErr.Code.IsRetryable()
	}

	// Unknown error type, don't retry by default.
	return false
}

// retryWithBackoff executes a function with exponential backoff retry logic.
func retryWithBackoff(
	ctx context.Context,
	maxRetries int,
	initialBackoff time.Duration,
	maxBackoff time.Duration,
	fn func() error,
) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Execute the function
		err := fn()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			return err // Non-retryable error, fail immediately
		}

		// Last attempt exhausted
		if attempt == maxRetries {
			break
		}

		// Calculate backoff duration with exponential growth
		backoff := calculateBackoff(attempt, initialBackoff, maxBackoff)

		// Wait for backoff duration or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			// Continue to next retry
		}
	}

	// All retries exhausted
	return errors.Wrapf(
		lastErr,
		errors.ErrTimeout,
		"grpc.retryWithBackoff",
		"operation failed after %d retries", maxRetries,
	)
}

// calculateBackoff calculates the backoff duration for a given attempt
// Uses exponential backoff with jitter to prevent thundering herd.
func calculateBackoff(attempt int, initialBackoff, maxBackoff time.Duration) time.Duration {
	// Exponential backoff: initialBackoff * 2^attempt
	backoff := float64(initialBackoff) * math.Pow(2, float64(attempt))

	// Cap at maximum backoff
	if backoff > float64(maxBackoff) {
		backoff = float64(maxBackoff)
	}

	// Add jitter (±10%) to prevent synchronized retries
	jitter := backoff * 0.1 * (2*randFloat() - 1)
	backoff += jitter

	return time.Duration(backoff)
}

// randFloat returns a pseudo-random float64 in [0.0, 1.0)
// Simple linear congruential generator for jitter.
var randState uint64 = uint64(time.Now().UnixNano())

func randFloat() float64 {
	// Linear congruential generator constants
	const (
		a = 1103515245
		c = 12345
		m = 1 << 31
	)

	randState = (a*randState + c) % m
	return float64(randState) / float64(m)
}

// streamRetryWrapper wraps a streaming RPC with retry logic.
type streamRetryWrapper struct {
	ctx            context.Context
	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	createStreamFn func() (interface{}, error)
	currentStream  interface{}
	attemptCount   int
}

// newStreamRetryWrapper creates a new stream retry wrapper.
func newStreamRetryWrapper(
	ctx context.Context,
	maxRetries int,
	initialBackoff, maxBackoff time.Duration,
	createStreamFn func() (interface{}, error),
) *streamRetryWrapper {
	return &streamRetryWrapper{
		ctx:            ctx,
		maxRetries:     maxRetries,
		initialBackoff: initialBackoff,
		maxBackoff:     maxBackoff,
		createStreamFn: createStreamFn,
	}
}

// getStream returns the current stream or creates a new one with retry.
func (w *streamRetryWrapper) getStream() (interface{}, error) {
	if w.currentStream != nil {
		return w.currentStream, nil
	}

	err := retryWithBackoff(
		w.ctx,
		w.maxRetries,
		w.initialBackoff,
		w.maxBackoff,
		func() error {
			stream, err := w.createStreamFn()
			if err != nil {
				return err
			}
			w.currentStream = stream
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	return w.currentStream, nil
}

// reset clears the current stream to force recreation.
func (w *streamRetryWrapper) reset() {
	w.currentStream = nil
	w.attemptCount++
}
