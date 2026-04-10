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
	"errors"
	"testing"
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

func TestRetryExponentialBackoff(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
		Multiplier:     2.0,
	}
	ctx := context.Background()

	callTimes := []time.Time{}
	err := RetryWithBackoff(ctx, config, func() error {
		callTimes = append(callTimes, time.Now())
		return errors.New("error")
	})

	require.Error(t, err)
	require.Len(t, callTimes, 3)

	// Check backoff intervals
	// First retry: ~10ms
	// Second retry: ~20ms
	interval1 := callTimes[1].Sub(callTimes[0])
	interval2 := callTimes[2].Sub(callTimes[1])

	assert.GreaterOrEqual(t, interval1, 10*time.Millisecond)
	assert.GreaterOrEqual(t, interval2, 20*time.Millisecond)
	assert.Less(t, interval1, 50*time.Millisecond) // Allow some variance
	assert.Less(t, interval2, 100*time.Millisecond)
}

func TestRetryMaxBackoffCap(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:    5,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     150 * time.Millisecond,
		Multiplier:     10.0, // Large multiplier
	}
	ctx := context.Background()

	callTimes := []time.Time{}
	err := RetryWithBackoff(ctx, config, func() error {
		callTimes = append(callTimes, time.Now())
		return errors.New("error")
	})

	require.Error(t, err)
	require.Len(t, callTimes, 5)

	// All intervals should be capped at MaxBackoff
	for i := 1; i < len(callTimes); i++ {
		interval := callTimes[i].Sub(callTimes[i-1])
		assert.LessOrEqual(t, interval, 200*time.Millisecond) // Allow some variance
	}
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
