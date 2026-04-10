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
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState represents circuit breaker state.
type CircuitState int32

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	maxFailures  int
	resetTimeout time.Duration
	state        atomic.Int32
	failures     atomic.Int32
	lastFailTime atomic.Int64

	// Metrics
	totalCalls   atomic.Int64
	successCount atomic.Int64
	failureCount atomic.Int64

	mu sync.Mutex
}

// CircuitBreakerStats holds circuit breaker statistics.
type CircuitBreakerStats struct {
	TotalCalls   int64
	SuccessCount int64
	FailureCount int64
	State        CircuitState
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
	}
	cb.state.Store(int32(CircuitClosed))
	return cb
}

// Execute executes the given function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Increment total calls
	cb.totalCalls.Add(1)

	// Check state
	currentState := CircuitState(cb.state.Load())
	if currentState == CircuitOpen {
		lastFail := time.Unix(0, cb.lastFailTime.Load())
		if time.Since(lastFail) > cb.resetTimeout {
			// Transition to half-open
			cb.state.Store(int32(CircuitHalfOpen))
		} else {
			return ErrCircuitBreakerOpen
		}
	}

	// Execute function
	err := fn()

	// Update state based on result
	if err != nil {
		cb.failureCount.Add(1)
		failures := cb.failures.Add(1)
		cb.lastFailTime.Store(time.Now().UnixNano())

		// Check if we should open the circuit
		if cb.maxFailures > 0 && int(failures) >= cb.maxFailures {
			cb.state.Store(int32(CircuitOpen))
		}

		return err
	}

	// Success - reset circuit
	cb.successCount.Add(1)
	cb.failures.Store(0)
	cb.state.Store(int32(CircuitClosed))

	return nil
}

// IsOpen returns whether the circuit breaker is open.
func (cb *CircuitBreaker) IsOpen() bool {
	return CircuitState(cb.state.Load()) == CircuitOpen
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	return CircuitState(cb.state.Load())
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures.Store(0)
	cb.state.Store(int32(CircuitClosed))
}

// Stats returns circuit breaker statistics.
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	return CircuitBreakerStats{
		TotalCalls:   cb.totalCalls.Load(),
		SuccessCount: cb.successCount.Load(),
		FailureCount: cb.failureCount.Load(),
		State:        cb.State(),
	}
}
