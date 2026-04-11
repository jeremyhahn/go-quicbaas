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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "nil error",
			err:       nil,
			retryable: false,
		},
		{
			name:      "unavailable grpc error",
			err:       status.Error(codes.Unavailable, "service unavailable"),
			retryable: true,
		},
		{
			name:      "deadline exceeded grpc error",
			err:       status.Error(codes.DeadlineExceeded, "deadline exceeded"),
			retryable: true,
		},
		{
			name:      "resource exhausted grpc error",
			err:       status.Error(codes.ResourceExhausted, "too many requests"),
			retryable: true,
		},
		{
			name:      "aborted grpc error",
			err:       status.Error(codes.Aborted, "transaction aborted"),
			retryable: true,
		},
		{
			name:      "internal grpc error",
			err:       status.Error(codes.Internal, "internal error"),
			retryable: true,
		},
		{
			name:      "not found grpc error",
			err:       status.Error(codes.NotFound, "not found"),
			retryable: false,
		},
		{
			name:      "invalid argument grpc error",
			err:       status.Error(codes.InvalidArgument, "invalid"),
			retryable: false,
		},
		{
			name:      "retryable qb error",
			err:       &dberrors.QBError{Code: dberrors.ErrUnavailable},
			retryable: true,
		},
		{
			name:      "non-retryable qb error",
			err:       &dberrors.QBError{Code: dberrors.ErrNotFound},
			retryable: false,
		},
		{
			name:      "generic error",
			err:       errors.New("generic error"),
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.retryable, result)
		})
	}
}

func TestRetryWithBackoff(t *testing.T) {
	t.Run("success on first attempt", func(t *testing.T) {
		callCount := 0
		fn := func() error {
			callCount++
			return nil
		}

		err := retryWithBackoff(
			context.Background(),
			3,
			10*time.Millisecond,
			100*time.Millisecond,
			fn,
		)

		assert.NoError(t, err)
		assert.Equal(t, 1, callCount)
	})

	t.Run("success after retries", func(t *testing.T) {
		callCount := 0
		fn := func() error {
			callCount++
			if callCount < 3 {
				return status.Error(codes.Unavailable, "unavailable")
			}
			return nil
		}

		err := retryWithBackoff(
			context.Background(),
			5,
			10*time.Millisecond,
			100*time.Millisecond,
			fn,
		)

		assert.NoError(t, err)
		assert.Equal(t, 3, callCount)
	})

	t.Run("non-retryable error", func(t *testing.T) {
		callCount := 0
		fn := func() error {
			callCount++
			return status.Error(codes.NotFound, "not found")
		}

		err := retryWithBackoff(
			context.Background(),
			3,
			10*time.Millisecond,
			100*time.Millisecond,
			fn,
		)

		assert.Error(t, err)
		assert.Equal(t, 1, callCount) // Should not retry
	})

	t.Run("all retries exhausted", func(t *testing.T) {
		callCount := 0
		fn := func() error {
			callCount++
			return status.Error(codes.Unavailable, "unavailable")
		}

		err := retryWithBackoff(
			context.Background(),
			3,
			10*time.Millisecond,
			100*time.Millisecond,
			fn,
		)

		assert.Error(t, err)
		assert.Equal(t, 4, callCount) // Initial attempt + 3 retries
	})

	t.Run("context canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		callCount := 0

		fn := func() error {
			callCount++
			if callCount == 2 {
				cancel() // Cancel after second attempt
			}
			return status.Error(codes.Unavailable, "unavailable")
		}

		err := retryWithBackoff(
			ctx,
			5,
			10*time.Millisecond,
			100*time.Millisecond,
			fn,
		)

		assert.Error(t, err)
		assert.True(t, callCount >= 2)
	})
}

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name           string
		attempt        int
		initialBackoff time.Duration
		maxBackoff     time.Duration
		minExpected    time.Duration
		maxExpected    time.Duration
	}{
		{
			name:           "first retry",
			attempt:        0,
			initialBackoff: 100 * time.Millisecond,
			maxBackoff:     5 * time.Second,
			minExpected:    80 * time.Millisecond,  // With jitter: 100ms - 10%
			maxExpected:    120 * time.Millisecond, // With jitter: 100ms + 10%
		},
		{
			name:           "second retry",
			attempt:        1,
			initialBackoff: 100 * time.Millisecond,
			maxBackoff:     5 * time.Second,
			minExpected:    180 * time.Millisecond, // 200ms - 10%
			maxExpected:    220 * time.Millisecond, // 200ms + 10%
		},
		{
			name:           "capped at max",
			attempt:        10,
			initialBackoff: 100 * time.Millisecond,
			maxBackoff:     1 * time.Second,
			minExpected:    900 * time.Millisecond,  // 1s - 10%
			maxExpected:    1100 * time.Millisecond, // 1s + 10%
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backoff := calculateBackoff(tt.attempt, tt.initialBackoff, tt.maxBackoff)
			assert.GreaterOrEqual(t, backoff, tt.minExpected)
			assert.LessOrEqual(t, backoff, tt.maxExpected)
		})
	}
}

func TestRandFloat(t *testing.T) {
	// Test that randFloat returns values between 0 and 1
	for i := 0; i < 100; i++ {
		val := randFloat()
		assert.GreaterOrEqual(t, val, 0.0)
		assert.LessOrEqual(t, val, 1.0)
	}
}

func TestStreamRetryWrapper(t *testing.T) {
	t.Run("get stream success", func(t *testing.T) {
		expectedStream := "test-stream"
		createStreamFn := func() (interface{}, error) {
			return expectedStream, nil
		}

		wrapper := newStreamRetryWrapper(
			context.Background(),
			3,
			10*time.Millisecond,
			100*time.Millisecond,
			createStreamFn,
		)

		stream, err := wrapper.getStream()
		assert.NoError(t, err)
		assert.Equal(t, expectedStream, stream)
	})

	t.Run("get stream with retry", func(t *testing.T) {
		callCount := 0
		createStreamFn := func() (interface{}, error) {
			callCount++
			if callCount < 2 {
				return nil, status.Error(codes.Unavailable, "unavailable")
			}
			return "success-stream", nil
		}

		wrapper := newStreamRetryWrapper(
			context.Background(),
			3,
			10*time.Millisecond,
			100*time.Millisecond,
			createStreamFn,
		)

		stream, err := wrapper.getStream()
		assert.NoError(t, err)
		assert.Equal(t, "success-stream", stream)
		assert.Equal(t, 2, callCount)
	})

	t.Run("get stream failure", func(t *testing.T) {
		createStreamFn := func() (interface{}, error) {
			return nil, status.Error(codes.Unavailable, "unavailable")
		}

		wrapper := newStreamRetryWrapper(
			context.Background(),
			2,
			10*time.Millisecond,
			100*time.Millisecond,
			createStreamFn,
		)

		stream, err := wrapper.getStream()
		assert.Error(t, err)
		assert.Nil(t, stream)
	})

	t.Run("reset stream", func(t *testing.T) {
		callCount := 0
		createStreamFn := func() (interface{}, error) {
			callCount++
			return "stream", nil
		}

		wrapper := newStreamRetryWrapper(
			context.Background(),
			3,
			10*time.Millisecond,
			100*time.Millisecond,
			createStreamFn,
		)

		// First call
		_, err := wrapper.getStream()
		assert.NoError(t, err)
		assert.Equal(t, 1, callCount)

		// Reset
		wrapper.reset()
		assert.Equal(t, 1, wrapper.attemptCount)
		assert.Nil(t, wrapper.currentStream)

		// Second call should create a new stream
		_, err = wrapper.getStream()
		assert.NoError(t, err)
		assert.Equal(t, 2, callCount)
	})
}

// Benchmarks.
func BenchmarkIsRetryableError(b *testing.B) {
	err := status.Error(codes.Unavailable, "unavailable")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = isRetryableError(err)
	}
}

func BenchmarkCalculateBackoff(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calculateBackoff(3, 100*time.Millisecond, 5*time.Second)
	}
}

func BenchmarkRetryWithBackoff_Success(b *testing.B) {
	fn := func() error {
		return nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = retryWithBackoff(
			context.Background(),
			3,
			10*time.Millisecond,
			100*time.Millisecond,
			fn,
		)
	}
}

func BenchmarkRetryWithBackoff_NonRetryable(b *testing.B) {
	fn := func() error {
		return status.Error(codes.NotFound, "not found")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = retryWithBackoff(
			context.Background(),
			3,
			10*time.Millisecond,
			100*time.Millisecond,
			fn,
		)
	}
}
