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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreakerCreation(t *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Second)
	assert.NotNil(t, cb)
	assert.Equal(t, CircuitClosed, cb.State())
	assert.False(t, cb.IsOpen())
}

func TestCircuitBreakerOpenClose(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)

	// Initial state should be closed
	assert.Equal(t, CircuitClosed, cb.State())
	assert.False(t, cb.IsOpen())

	// Successful execution should keep circuit closed
	err := cb.Execute(func() error {
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, CircuitClosed, cb.State())

	// Failed executions should open circuit after threshold
	testErr := errors.New("test error")
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error {
			return testErr
		})
		assert.Error(t, err)
	}

	// Circuit should now be open
	assert.Equal(t, CircuitOpen, cb.State())
	assert.True(t, cb.IsOpen())

	// Attempts to execute should fail immediately
	err = cb.Execute(func() error {
		t.Fatal("should not execute when circuit is open")
		return nil
	})
	assert.ErrorIs(t, err, ErrCircuitBreakerOpen)
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)

	// Trigger circuit open
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, CircuitOpen, cb.State())

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Next execution should cryptoion to half-open
	executed := false
	err := cb.Execute(func() error {
		executed = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, executed)
	assert.Equal(t, CircuitClosed, cb.State()) // Successful execution closes circuit
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)

	// Trigger circuit open
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, CircuitOpen, cb.State())

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Failed execution should re-open circuit
	err := cb.Execute(func() error {
		return testErr
	})
	assert.Error(t, err)
	assert.Equal(t, CircuitOpen, cb.State())
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreaker(2, 1*time.Second)

	// Trigger circuit open
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, CircuitOpen, cb.State())

	// Manual reset
	cb.Reset()
	assert.Equal(t, CircuitClosed, cb.State())
	assert.False(t, cb.IsOpen())

	// Should execute normally
	err := cb.Execute(func() error {
		return nil
	})
	assert.NoError(t, err)
}

func TestCircuitBreakerConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(10, 100*time.Millisecond)

	// Execute concurrently
	const goroutines = 50
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			if id%5 == 0 {
				// Some failures
				cb.Execute(func() error {
					return errors.New("error")
				})
			} else {
				// Mostly successes
				cb.Execute(func() error {
					return nil
				})
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// Circuit should still be operational
	assert.NotNil(t, cb)
}

func TestCircuitBreakerMetrics(t *testing.T) {
	cb := NewCircuitBreaker(5, 100*time.Millisecond)

	// Track successes and failures
	stats := cb.Stats()
	assert.Equal(t, int64(0), stats.TotalCalls)
	assert.Equal(t, int64(0), stats.SuccessCount)
	assert.Equal(t, int64(0), stats.FailureCount)

	// Execute some operations
	cb.Execute(func() error { return nil })
	cb.Execute(func() error { return nil })
	cb.Execute(func() error { return errors.New("error") })

	stats = cb.Stats()
	assert.Equal(t, int64(3), stats.TotalCalls)
	assert.Equal(t, int64(2), stats.SuccessCount)
	assert.Equal(t, int64(1), stats.FailureCount)
}

func TestCircuitBreakerZeroThreshold(t *testing.T) {
	// Zero threshold should never open
	cb := NewCircuitBreaker(0, 100*time.Millisecond)

	for i := 0; i < 10; i++ {
		cb.Execute(func() error {
			return errors.New("error")
		})
	}

	// Circuit should remain closed with zero threshold
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreakerErrorPropagation(t *testing.T) {
	cb := NewCircuitBreaker(5, 100*time.Millisecond)

	testErr := errors.New("specific error")
	err := cb.Execute(func() error {
		return testErr
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, testErr))
}
