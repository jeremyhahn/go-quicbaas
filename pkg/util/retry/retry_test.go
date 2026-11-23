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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestExponentialBackoff_Next(t *testing.T) {
	tests := []struct {
		name    string
		backoff *ExponentialBackoff
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name: "first attempt without jitter",
			backoff: &ExponentialBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     10 * time.Second,
				Multiplier:   2.0,
				Jitter:       0,
			},
			attempt: 0,
			wantMin: 100 * time.Millisecond,
			wantMax: 100 * time.Millisecond,
		},
		{
			name: "second attempt without jitter",
			backoff: &ExponentialBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     10 * time.Second,
				Multiplier:   2.0,
				Jitter:       0,
			},
			attempt: 1,
			wantMin: 200 * time.Millisecond,
			wantMax: 200 * time.Millisecond,
		},
		{
			name: "third attempt without jitter",
			backoff: &ExponentialBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     10 * time.Second,
				Multiplier:   2.0,
				Jitter:       0,
			},
			attempt: 2,
			wantMin: 400 * time.Millisecond,
			wantMax: 400 * time.Millisecond,
		},
		{
			name: "max delay reached",
			backoff: &ExponentialBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     500 * time.Millisecond,
				Multiplier:   2.0,
				Jitter:       0,
			},
			attempt: 10,
			wantMin: 500 * time.Millisecond,
			wantMax: 500 * time.Millisecond,
		},
		{
			name: "with jitter",
			backoff: &ExponentialBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     10 * time.Second,
				Multiplier:   2.0,
				Jitter:       0.5, // 50% jitter
			},
			attempt: 0,
			wantMin: 50 * time.Millisecond,  // 100ms * (1 - 0.5)
			wantMax: 100 * time.Millisecond, // 100ms * 1.0
		},
		{
			name: "negative attempt defaults to 0",
			backoff: &ExponentialBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     10 * time.Second,
				Multiplier:   2.0,
				Jitter:       0,
			},
			attempt: -1,
			wantMin: 100 * time.Millisecond,
			wantMax: 100 * time.Millisecond,
		},
		{
			name: "full jitter stays within zero and the delay",
			backoff: &ExponentialBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     1 * time.Second,
				Multiplier:   2.0,
				Jitter:       1.0,
			},
			attempt: 0,
			wantMin: 0,
			wantMax: 100 * time.Millisecond,
		},
		{
			name: "very high attempt caps at max delay",
			backoff: &ExponentialBackoff{
				InitialDelay: 10 * time.Millisecond,
				MaxDelay:     1 * time.Second,
				Multiplier:   2.0,
				Jitter:       0,
			},
			attempt: 100,
			wantMin: 1 * time.Second,
			wantMax: 1 * time.Second,
		},
		{
			name: "multiplier of 1 keeps the initial delay",
			backoff: &ExponentialBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     1 * time.Second,
				Multiplier:   1.0,
			},
			attempt: 10,
			wantMin: 100 * time.Millisecond,
			wantMax: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run multiple times to account for jitter randomness
			for i := 0; i < 10; i++ {
				got := tt.backoff.Next(tt.attempt)
				assert.GreaterOrEqual(t, got, tt.wantMin, "backoff duration too small")
				assert.LessOrEqual(t, got, tt.wantMax, "backoff duration too large")
			}
		})
	}
}

func TestLinearBackoff_Next(t *testing.T) {
	tests := []struct {
		name    string
		backoff *LinearBackoff
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name: "first attempt without jitter",
			backoff: &LinearBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     5 * time.Second,
				Increment:    50 * time.Millisecond,
				Jitter:       0,
			},
			attempt: 0,
			wantMin: 100 * time.Millisecond,
			wantMax: 100 * time.Millisecond,
		},
		{
			name: "second attempt without jitter",
			backoff: &LinearBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     5 * time.Second,
				Increment:    50 * time.Millisecond,
				Jitter:       0,
			},
			attempt: 1,
			wantMin: 150 * time.Millisecond,
			wantMax: 150 * time.Millisecond,
		},
		{
			name: "max delay reached",
			backoff: &LinearBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     500 * time.Millisecond,
				Increment:    100 * time.Millisecond,
				Jitter:       0,
			},
			attempt: 10,
			wantMin: 500 * time.Millisecond,
			wantMax: 500 * time.Millisecond,
		},
		{
			name: "with jitter",
			backoff: &LinearBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     5 * time.Second,
				Increment:    50 * time.Millisecond,
				Jitter:       0.5,
			},
			attempt: 0,
			wantMin: 50 * time.Millisecond,
			wantMax: 100 * time.Millisecond,
		},
		{
			name: "zero increment keeps the initial delay",
			backoff: &LinearBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     1 * time.Second,
				Increment:    0,
				Jitter:       0,
			},
			attempt: 5,
			wantMin: 100 * time.Millisecond,
			wantMax: 100 * time.Millisecond,
		},
		{
			name: "negative attempt defaults to 0",
			backoff: &LinearBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     1 * time.Second,
				Increment:    50 * time.Millisecond,
				Jitter:       0,
			},
			attempt: -5,
			wantMin: 100 * time.Millisecond,
			wantMax: 100 * time.Millisecond,
		},
		{
			name: "large increment below max delay",
			backoff: &LinearBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     500 * time.Millisecond,
				Increment:    200 * time.Millisecond,
				Jitter:       0,
			},
			attempt: 1,
			wantMin: 300 * time.Millisecond,
			wantMax: 300 * time.Millisecond,
		},
		{
			name: "large increment capped at max delay",
			backoff: &LinearBackoff{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     500 * time.Millisecond,
				Increment:    200 * time.Millisecond,
				Jitter:       0,
			},
			attempt: 2,
			wantMin: 500 * time.Millisecond,
			wantMax: 500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 10; i++ {
				got := tt.backoff.Next(tt.attempt)
				assert.GreaterOrEqual(t, got, tt.wantMin, "backoff duration too small")
				assert.LessOrEqual(t, got, tt.wantMax, "backoff duration too large")
			}
		})
	}
}

func TestConstantBackoff_Next(t *testing.T) {
	tests := []struct {
		name    string
		backoff *ConstantBackoff
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name: "constant delay without jitter",
			backoff: &ConstantBackoff{
				Delay:  200 * time.Millisecond,
				Jitter: 0,
			},
			attempt: 0,
			wantMin: 200 * time.Millisecond,
			wantMax: 200 * time.Millisecond,
		},
		{
			name: "constant delay with jitter",
			backoff: &ConstantBackoff{
				Delay:  200 * time.Millisecond,
				Jitter: 0.5,
			},
			attempt: 5,
			wantMin: 100 * time.Millisecond,
			wantMax: 200 * time.Millisecond,
		},
		{
			name: "large attempt keeps the constant delay",
			backoff: &ConstantBackoff{
				Delay:  100 * time.Millisecond,
				Jitter: 0,
			},
			attempt: 100,
			wantMin: 100 * time.Millisecond,
			wantMax: 100 * time.Millisecond,
		},
		{
			name: "zero delay",
			backoff: &ConstantBackoff{
				Delay:  0,
				Jitter: 0,
			},
			attempt: 5,
			wantMin: 0,
			wantMax: 0,
		},
		{
			name: "jitter with zero delay",
			backoff: &ConstantBackoff{
				Delay:  0,
				Jitter: 0.5,
			},
			attempt: 5,
			wantMin: 0,
			wantMax: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 10; i++ {
				got := tt.backoff.Next(tt.attempt)
				assert.GreaterOrEqual(t, got, tt.wantMin, "backoff duration too small")
				assert.LessOrEqual(t, got, tt.wantMax, "backoff duration too large")
			}
		})
	}
}

func TestDo_SuccessAfterRetries(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{
		MaxAttempts: 3,
		Backoff: &ConstantBackoff{
			Delay:  10 * time.Millisecond,
			Jitter: 0,
		},
	}

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		if callCount < 3 {
			return dberrors.New(dberrors.ErrTimeout, "test.op", "temporary failure")
		}
		return nil
	}

	start := time.Now()
	err := Do(ctx, cfg, op)
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount, "operation should be called three times")
	// Should have waited ~20ms (2 retries * 10ms)
	assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond, "should wait for backoff")
}

func TestDo_NonRetryableError(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultConfig()

	callCount := 0
	expectedErr := dberrors.New(dberrors.ErrNotFound, "test.op", "resource not found")

	op := func(ctx context.Context) error {
		callCount++
		return expectedErr
	}

	err := Do(ctx, cfg, op)
	assert.Error(t, err)
	assert.Equal(t, 1, callCount, "non-retryable error should not retry")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound))
}

func TestDo_MaxAttemptsExceeded(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{
		MaxAttempts: 3,
		Backoff: &ConstantBackoff{
			Delay:  1 * time.Millisecond,
			Jitter: 0,
		},
	}

	callCount := 0
	expectedErr := dberrors.New(dberrors.ErrTimeout, "test.op", "timeout")

	op := func(ctx context.Context) error {
		callCount++
		return expectedErr
	}

	err := Do(ctx, cfg, op)
	assert.Error(t, err)
	assert.Equal(t, 3, callCount, "should retry up to max attempts")
	assert.True(t, dberrors.Is(err, dberrors.ErrTimeout))
}

// assertStoppedOnContext asserts that Do stopped because the context ended
// rather than because the operation ran out of attempts.
//
// THE OBVIOUS ASSERTION DOES NOT WORK, and this exists so nobody writes it and
// believes it. Do wraps its context exits as
// errors.Wrap(lastErr, ErrCanceled, "retry.Do"), but Wrap keeps the code of the
// error it wraps when that error is already a QRDB error — so
// dberrors.Is(err, dberrors.ErrCanceled) is FALSE and the error still reports
// whatever the operation failed with. Measured against the dependency, not
// assumed.
//
// What the wrap does change is the Op: a context exit reports Do's OWN name
// (currently "retry.Do"), while exhausting the attempts returns the operation's
// error untouched and so keeps the operation's Op. Asserting that the Op is no
// longer the operation's is the observable form of "Do reported this stop as
// its own", and it survives an upstream rename of that string.
//
// callCount is required because the bare-context case is only legitimate
// BEFORE the first attempt: Do returns a raw ctx.Err() only when no operation
// error exists yet. Accepting it unconditionally would let a loop that ignored
// cancellation during backoff, and noticed the context only at the next
// attempt boundary, pass for the wrong reason.
func assertStoppedOnContext(t *testing.T, err error, callCount int, opName string) {
	t.Helper()
	require.Error(t, err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		assert.Zero(t, callCount,
			"Do returned a bare context error after running the operation; it only does that before the first attempt")
		return
	}
	var qberr *dberrors.QBError
	require.ErrorAs(t, err, &qberr)
	assert.NotEqual(t, opName, qberr.Op,
		"Do returned the operation's own error, which is what exhausting the attempts looks like; got %v", err)
}

func TestDo_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := &Config{
		MaxAttempts: 10,
		Backoff: &ConstantBackoff{
			Delay:  100 * time.Millisecond,
			Jitter: 0,
		},
	}

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		if callCount == 1 {
			// Cancel context in a goroutine during first backoff
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()
		}
		return dberrors.New(dberrors.ErrTimeout, "test.op", "timeout")
	}

	err := Do(ctx, cfg, op)
	require.Error(t, err)
	// The same contract as TestDo_ContextTimeout, asserted the same way. This
	// one is not known to flake — it cancels 10ms into a 100ms backoff, so it
	// has ten times the slack — but its old `<= 2` was equally silent about
	// WHY the loop stopped, and would have passed a loop that ignored
	// cancellation and merely happened to be slow.
	assert.Less(t, callCount, cfg.MaxAttempts,
		"should stop retrying once the context was cancelled, not run out of attempts")
	assertStoppedOnContext(t, err, callCount, "test.op")
}

// TestDo_ContextTimeout asserts the CONTRACT — the loop stops because the
// context expired — and deliberately does not assert HOW MANY attempts fit
// inside the deadline (#211).
//
// THE OLD BOUND WAS UNFIXABLE BY WIDENING IT. It read `callCount <= 2`, from a
// 30ms deadline against a 20ms backoff, and failed on CI with 3. Reaching a
// third attempt requires ctx.Done() to still be open at ~40ms when the deadline
// was 30ms — that is, the context's deadline timer delivered LATE RELATIVE TO
// the backoff timer. Those are two independent timers, and under starvation Go
// promises nothing about their relative delivery order. So the assertion did
// not have too little margin; it depended on an ordering the runtime does not
// offer, and a bigger number would only have made the failure rarer. It was
// observed passing and failing IN THE SAME CI JOB — the plain pass green, the
// coverage-instrumented pass red at more than twice the duration.
//
// What is asserted instead does not name a precise attempt count: the loop
// stopped SHORT of its ten attempts, and the error says the context is why. The
// remaining timing dependence is only that at least one of the ten attempts is
// skipped, which has the whole backoff budget as slack instead of 10ms. A retry
// loop that ignored cancellation — the defect this test exists to catch — fails
// both, because it would run all ten attempts and return the operation's own
// error.
func TestDo_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	cfg := &Config{
		MaxAttempts: 10,
		Backoff: &ConstantBackoff{
			Delay:  20 * time.Millisecond,
			Jitter: 0,
		},
	}

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		return dberrors.New(dberrors.ErrUnavailable, "test.op", "service unavailable")
	}

	err := Do(ctx, cfg, op)
	require.Error(t, err)
	assert.Less(t, callCount, cfg.MaxAttempts,
		"should stop retrying once the context expired, not run out of attempts")
	assertStoppedOnContext(t, err, callCount, "test.op")
}

func TestDo_OnRetryCallback(t *testing.T) {
	t.Run("called once per retry with the error and backoff", func(t *testing.T) {
		var attempts []int
		var errs []error
		var backoffs []time.Duration

		cfg := &Config{
			MaxAttempts: 4,
			Backoff: &ConstantBackoff{
				Delay:  5 * time.Millisecond,
				Jitter: 0,
			},
			OnRetry: func(attempt int, err error, backoff time.Duration) {
				attempts = append(attempts, attempt)
				errs = append(errs, err)
				backoffs = append(backoffs, backoff)
			},
		}

		callCount := 0
		expectedErr := dberrors.New(dberrors.ErrTimeout, "test.op", "timeout")
		op := func(ctx context.Context) error {
			callCount++
			if callCount < 3 {
				return expectedErr
			}
			return nil
		}

		err := Do(context.Background(), cfg, op)
		assert.NoError(t, err)
		assert.Equal(t, 3, callCount)
		assert.Equal(t, []int{0, 1}, attempts, "callback should be called for each retry")
		assert.Equal(t, []error{expectedErr, expectedErr}, errs)
		assert.Equal(t, []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}, backoffs)
	})

	t.Run("not called on immediate success", func(t *testing.T) {
		onRetryCalled := false
		cfg := &Config{
			MaxAttempts: 3,
			Backoff:     DefaultConstantBackoff(),
			OnRetry: func(attempt int, err error, backoff time.Duration) {
				onRetryCalled = true
			},
		}

		err := Do(context.Background(), cfg, func(ctx context.Context) error { return nil })
		assert.NoError(t, err)
		assert.False(t, onRetryCalled, "OnRetry should not be called on immediate success")
	})
}

func TestDo_CustomRetryableFunc(t *testing.T) {
	customErr := errors.New("custom error")

	tests := []struct {
		name      string
		retryable func(err error) bool
		wantCalls int
	}{
		{
			name: "retries the error the func accepts",
			retryable: func(err error) bool {
				return errors.Is(err, customErr)
			},
			wantCalls: 3,
		},
		{
			name: "stops when the func rejects the error",
			retryable: func(err error) bool {
				return false
			},
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				MaxAttempts: 3,
				Backoff: &ConstantBackoff{
					Delay:  1 * time.Millisecond,
					Jitter: 0,
				},
				RetryableFunc: tt.retryable,
			}

			callCount := 0
			op := func(ctx context.Context) error {
				callCount++
				return customErr
			}

			err := Do(context.Background(), cfg, op)
			assert.ErrorIs(t, err, customErr)
			assert.Equal(t, tt.wantCalls, callCount)
		})
	}
}

func TestDo_ConfigDefaults(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *Config
		failures  int
		wantCalls int
		wantErr   bool
	}{
		{
			name:      "nil config uses the default config",
			cfg:       nil,
			failures:  0,
			wantCalls: 1,
		},
		{
			name:      "zero max attempts defaults to 1",
			cfg:       &Config{MaxAttempts: 0, Backoff: DefaultConstantBackoff()},
			failures:  5,
			wantCalls: 1,
			wantErr:   true,
		},
		{
			name:      "negative max attempts defaults to 1",
			cfg:       &Config{MaxAttempts: -5, Backoff: DefaultConstantBackoff()},
			failures:  5,
			wantCalls: 1,
			wantErr:   true,
		},
		{
			name:      "nil backoff uses the default backoff",
			cfg:       &Config{MaxAttempts: 2, Backoff: nil},
			failures:  1,
			wantCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			op := func(ctx context.Context) error {
				callCount++
				if callCount <= tt.failures {
					return dberrors.New(dberrors.ErrTimeout, "test.op", "timeout")
				}
				return nil
			}

			err := Do(context.Background(), tt.cfg, op)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantCalls, callCount)
		})
	}
}

func TestDoExponential(t *testing.T) {
	ctx := context.Background()

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		if callCount < 2 {
			return dberrors.New(dberrors.ErrTimeout, "test.op", "timeout")
		}
		return nil
	}

	err := DoExponential(ctx, 3, op)
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestDoLinear(t *testing.T) {
	ctx := context.Background()

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		if callCount < 2 {
			return dberrors.New(dberrors.ErrUnavailable, "test.op", "unavailable")
		}
		return nil
	}

	err := DoLinear(ctx, 3, op)
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestDoConstant(t *testing.T) {
	ctx := context.Background()

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		if callCount < 2 {
			return dberrors.New(dberrors.ErrLeaderNotAvailable, "test.op", "no leader")
		}
		return nil
	}

	err := DoConstant(ctx, 3, op)
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestDefaultBackoffs(t *testing.T) {
	t.Run("DefaultExponentialBackoff", func(t *testing.T) {
		b := DefaultExponentialBackoff()
		require.NotNil(t, b)
		assert.Equal(t, 100*time.Millisecond, b.InitialDelay)
		assert.Equal(t, 10*time.Second, b.MaxDelay)
		assert.Equal(t, 2.0, b.Multiplier)
		assert.Equal(t, 0.1, b.Jitter)
	})

	t.Run("DefaultLinearBackoff", func(t *testing.T) {
		b := DefaultLinearBackoff()
		require.NotNil(t, b)
		assert.Equal(t, 100*time.Millisecond, b.InitialDelay)
		assert.Equal(t, 5*time.Second, b.MaxDelay)
		assert.Equal(t, 200*time.Millisecond, b.Increment)
		assert.Equal(t, 0.1, b.Jitter)
	})

	t.Run("DefaultConstantBackoff", func(t *testing.T) {
		b := DefaultConstantBackoff()
		require.NotNil(t, b)
		assert.Equal(t, 500*time.Millisecond, b.Delay)
		assert.Equal(t, 0.1, b.Jitter)
	})

	t.Run("DefaultConfig", func(t *testing.T) {
		cfg := DefaultConfig()
		require.NotNil(t, cfg)
		assert.Equal(t, 3, cfg.MaxAttempts)
		assert.NotNil(t, cfg.Backoff)
	})
}

// Benchmark tests.
func BenchmarkExponentialBackoff_Next(b *testing.B) {
	backoff := DefaultExponentialBackoff()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backoff.Next(i % 10)
	}
}

func BenchmarkLinearBackoff_Next(b *testing.B) {
	backoff := DefaultLinearBackoff()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backoff.Next(i % 10)
	}
}

func BenchmarkConstantBackoff_Next(b *testing.B) {
	backoff := DefaultConstantBackoff()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backoff.Next(i)
	}
}

func BenchmarkDo_NoRetries(b *testing.B) {
	ctx := context.Background()
	cfg := DefaultConfig()
	op := func(ctx context.Context) error {
		return nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Do(ctx, cfg, op)
	}
}

func BenchmarkDo_WithRetries(b *testing.B) {
	ctx := context.Background()
	cfg := &Config{
		MaxAttempts: 3,
		Backoff: &ConstantBackoff{
			Delay:  1 * time.Nanosecond, // Minimal delay for benchmark
			Jitter: 0,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		callCount := 0
		op := func(ctx context.Context) error {
			callCount++
			if callCount < 2 {
				return dberrors.New(dberrors.ErrTimeout, "bench.op", "timeout")
			}
			return nil
		}
		_ = Do(ctx, cfg, op)
	}
}

// Additional edge case tests for 90%+ coverage

func TestDo_ContextCanceledBeforeFirstAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cfg := DefaultConfig()

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		return nil
	}

	err := Do(ctx, cfg, op)
	assert.Error(t, err)
	assert.Equal(t, 0, callCount, "operation should not be called when context is already canceled")
}

func TestDo_ContextCanceledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &Config{
		MaxAttempts: 10,
		Backoff: &ConstantBackoff{
			Delay:  50 * time.Millisecond,
			Jitter: 0,
		},
	}

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		if callCount == 1 {
			// Cancel after first attempt, during backoff
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()
		}
		return dberrors.New(dberrors.ErrTimeout, "test.op", "timeout")
	}

	err := Do(ctx, cfg, op)
	assert.Error(t, err)
	// Should have attempted once before cancellation
	assert.GreaterOrEqual(t, callCount, 1)
	assert.LessOrEqual(t, callCount, 2)
}

func TestShouldRetry(t *testing.T) {
	customCfg := &Config{
		RetryableFunc: func(err error) bool {
			return err.Error() == "retry me"
		},
	}
	defaultCfg := &Config{RetryableFunc: nil} // Use default QuicBaaS error check

	tests := []struct {
		name string
		cfg  *Config
		err  error
		want bool
	}{
		{name: "nil error", cfg: DefaultConfig(), err: nil, want: false},
		{name: "custom func retryable error", cfg: customCfg, err: errors.New("retry me"), want: true},
		{name: "custom func non-retryable error", cfg: customCfg, err: errors.New("don't retry"), want: false},
		{name: "retryable QuicBaaS error", cfg: defaultCfg, err: dberrors.New(dberrors.ErrTimeout, "test", "timeout"), want: true},
		{name: "non-retryable QuicBaaS error", cfg: defaultCfg, err: dberrors.New(dberrors.ErrInvalidInput, "test", "bad input"), want: false},
		{name: "standard error", cfg: defaultCfg, err: errors.New("standard error"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldRetry(tt.cfg, tt.err))
		})
	}
}

func TestDoWithBackoff(t *testing.T) {
	ctx := context.Background()

	callCount := 0
	op := func(ctx context.Context) error {
		callCount++
		if callCount < 2 {
			return dberrors.New(dberrors.ErrTimeout, "test.op", "timeout")
		}
		return nil
	}

	backoff := &ConstantBackoff{
		Delay:  1 * time.Millisecond,
		Jitter: 0,
	}

	err := DoWithBackoff(ctx, 3, backoff, op)
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
}
