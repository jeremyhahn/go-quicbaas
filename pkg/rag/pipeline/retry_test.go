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
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	assert.Equal(t, 3, config.MaxAttempts)
	assert.Equal(t, 100*time.Millisecond, config.InitialBackoff)
	assert.Equal(t, 5*time.Second, config.MaxBackoff)
	assert.Equal(t, 2.0, config.Multiplier)
}

func TestRetrySuccess(t *testing.T) {
	config := DefaultRetryConfig()
	ctx := context.Background()

	callCount := 0
	err := RetryWithBackoff(ctx, config, func() error {
		callCount++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestRetrySuccessAfterFailures(t *testing.T) {
	config := DefaultRetryConfig()
	config.InitialBackoff = 10 * time.Millisecond
	ctx := context.Background()

	callCount := 0
	testErr := errors.New("temporary error")

	err := RetryWithBackoff(ctx, config, func() error {
		callCount++
		if callCount < 3 {
			return testErr
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestRetryMaxAttemptsExceeded(t *testing.T) {
	config := DefaultRetryConfig()
	config.MaxAttempts = 3
	config.InitialBackoff = 10 * time.Millisecond
	ctx := context.Background()

	callCount := 0
	testErr := errors.New("persistent error")

	err := RetryWithBackoff(ctx, config, func() error {
		callCount++
		return testErr
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, testErr))
	assert.Equal(t, 3, callCount)
}

func TestRetryContextCancellation(t *testing.T) {
	config := DefaultRetryConfig()
	config.MaxAttempts = 10
	config.InitialBackoff = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	callCount := 0
	err := RetryWithBackoff(ctx, config, func() error {
		callCount++
		return errors.New("error")
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	// Should have tried at least once before timeout
	assert.GreaterOrEqual(t, callCount, 1)
	// Should not have tried all attempts
	assert.Less(t, callCount, config.MaxAttempts)
}

// recordWaits returns a RetryConfig.wait that captures the backoff it is asked
// for and returns immediately, so a schedule can be asserted exactly instead of
// measured.
func recordWaits(delays *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*delays = append(*delays, d)
		return nil
	}
}

// TestRetryExponentialBackoff asserts the schedule RetryWithBackoff computes:
// each wait is the previous one times Multiplier.
//
// It used to measure elapsed wall-clock between calls and require the first gap
// to be under 50ms and the second under 100ms. Those upper bounds are the
// flake: a delay can only ever come back LONGER than asked for, so any
// scheduling hiccup above the allowance failed a correct implementation. Seen
// in CI on PR #410, whose diff does not touch this package (go-quicbaas#423).
//
// Lower bounds would have been safe -- overshooting cannot violate them -- but
// on their own they cannot tell 20ms from 20 minutes, so the schedule is read
// from the seam instead and pinned exactly. Real waiting is covered by
// TestRetryWithBackoffWaitsForReal.
func TestRetryExponentialBackoff(t *testing.T) {
	var delays []time.Duration
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
		Multiplier:     2.0,
		wait:           recordWaits(&delays),
	}

	callCount := 0
	err := RetryWithBackoff(context.Background(), config, func() error {
		callCount++
		return errors.New("error")
	})

	require.Error(t, err)
	require.Equal(t, 3, callCount)

	// Three attempts means two waits, and the second is the first doubled.
	assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, delays)
}

// TestRetryWithBackoffWaitsForReal covers what the seam cannot: that the
// computed delay is actually spent when no wait is injected. Without this, a
// regression that stopped calling wait at all would leave every schedule
// assertion above still passing.
//
// It asserts a LOWER bound only, which load cannot break -- a slow machine
// makes the elapsed time larger, never smaller.
func TestRetryWithBackoffWaitsForReal(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:    2,
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     time.Second,
		Multiplier:     2.0,
	}

	start := time.Now()
	err := RetryWithBackoff(context.Background(), config, func() error {
		return errors.New("error")
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond,
		"the single backoff between two attempts must actually be waited out")
}

// TestRetryMaxBackoffCap asserts MaxBackoff caps the schedule even when the
// multiplier would blow past it.
//
// The version this replaces measured elapsed time and allowed each gap up to
// 200ms against a 150ms cap, so it failed whenever the scheduler added 50ms --
// and it could not have detected the cap being applied as 190ms either, since
// that was inside the allowance. Reading the computed waits pins both.
func TestRetryMaxBackoffCap(t *testing.T) {
	var delays []time.Duration
	config := RetryConfig{
		MaxAttempts:    5,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     150 * time.Millisecond,
		Multiplier:     10.0, // Large multiplier
		wait:           recordWaits(&delays),
	}

	callCount := 0
	err := RetryWithBackoff(context.Background(), config, func() error {
		callCount++
		return errors.New("error")
	})

	require.Error(t, err)
	require.Equal(t, 5, callCount)

	// The first wait is InitialBackoff; every later one is pinned to the cap,
	// because 100ms * 10 already exceeds it.
	assert.Equal(t, []time.Duration{
		100 * time.Millisecond,
		150 * time.Millisecond,
		150 * time.Millisecond,
		150 * time.Millisecond,
	}, delays)
}

func TestRetryNonRetryableError(t *testing.T) {
	config := DefaultRetryConfig()
	config.InitialBackoff = 10 * time.Millisecond
	ctx := context.Background()

	callCount := 0
	nonRetryableErr := ErrInvalidConfiguration

	err := RetryWithBackoff(ctx, config, func() error {
		callCount++
		return nonRetryableErr
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidConfiguration))
	assert.Equal(t, 1, callCount) // Should not retry
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "deadline exceeded",
			err:       context.DeadlineExceeded,
			retryable: true,
		},
		{
			name:      "context canceled",
			err:       context.Canceled,
			retryable: false,
		},
		{
			name:      "resource exhausted",
			err:       ErrResourceExhausted,
			retryable: true,
		},
		{
			name:      "circuit breaker open",
			err:       ErrCircuitBreakerOpen,
			retryable: false,
		},
		{
			name:      "invalid configuration",
			err:       ErrInvalidConfiguration,
			retryable: false,
		},
		{
			name:      "generic error",
			err:       errors.New("generic error"),
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryable(tt.err)
			assert.Equal(t, tt.retryable, result)
		})
	}
}

func TestRetryWithJitter(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:    5,
		InitialBackoff: 50 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
		Multiplier:     2.0,
		EnableJitter:   true,
	}
	ctx := context.Background()

	callTimes := []time.Time{}
	err := RetryWithBackoff(ctx, config, func() error {
		callTimes = append(callTimes, time.Now())
		return errors.New("error")
	})

	require.Error(t, err)
	require.Len(t, callTimes, 5)

	// With jitter, intervals should vary
	intervals := make([]time.Duration, len(callTimes)-1)
	for i := 1; i < len(callTimes); i++ {
		intervals[i-1] = callTimes[i].Sub(callTimes[i-1])
	}

	// Just verify all intervals are reasonable (not exact due to jitter)
	for _, interval := range intervals {
		assert.Greater(t, interval, 10*time.Millisecond)
		assert.Less(t, interval, 5*time.Second)
	}
}

func TestJitteredDelay_StaysWithinHalfToOneAndAHalfBackoff(t *testing.T) {
	backoff := 100 * time.Millisecond

	lowest, err := jitteredDelay(backoff, bytes.NewReader(make([]byte, 16)))
	require.NoError(t, err)
	assert.Equal(t, backoff/2, lowest, "zero entropy must give the lower bound")

	for range 100 {
		delay, err := jitteredDelay(backoff, nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, delay, backoff/2)
		assert.Less(t, delay, backoff+backoff/2)
	}
}

func TestJitteredDelay_NonPositiveBackoffIsReturnedUnchanged(t *testing.T) {
	delay, err := jitteredDelay(0, iotest.ErrReader(errors.New("unused")))
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), delay)
}

func TestRetryWithBackoff_JitterSourceFailureStopsRetrying(t *testing.T) {
	entropyErr := errors.New("entropy exhausted")
	calls := 0
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     time.Second,
		Multiplier:     2.0,
		EnableJitter:   true,
		wait:           func(context.Context, time.Duration) error { return nil },
		entropy:        iotest.ErrReader(entropyErr),
	}

	err := RetryWithBackoff(context.Background(), config, func() error {
		calls++
		return errors.New("transient")
	})

	var jitterErr *RetryJitterError
	require.ErrorAs(t, err, &jitterErr)
	require.ErrorIs(t, err, entropyErr)
	assert.Equal(t, 1, calls, "no retry may run without a chosen delay")
}

func TestRetryZeroAttempts(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:    0,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
		Multiplier:     2.0,
	}
	ctx := context.Background()

	callCount := 0
	err := RetryWithBackoff(ctx, config, func() error {
		callCount++
		return errors.New("error")
	})

	require.Error(t, err)
	assert.Equal(t, 0, callCount) // Should not execute at all
}
