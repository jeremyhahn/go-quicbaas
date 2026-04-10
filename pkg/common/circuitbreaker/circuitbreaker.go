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

// Package circuitbreaker provides a lock-free circuit breaker for protecting
// calls to external services and internal subsystems from cascading failures.
//
// The circuit breaker has three states:
//   - Closed: requests flow normally; consecutive failures are tracked.
//   - Open: requests are rejected immediately; transitions to HalfOpen after a timeout.
//   - HalfOpen: a limited number of probe requests are allowed; successes close the circuit,
//     a single failure reopens it.
//
// All state transitions use atomic operations for lock-free concurrency safety.
package circuitbreaker

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// State represents the circuit breaker state.
type State int32

const (
	// StateClosed is the normal operating state where requests flow through.
	StateClosed State = iota

	// StateOpen is the tripped state where requests are rejected immediately.
	StateOpen

	// StateHalfOpen is the recovery probe state where limited requests are allowed.
	StateHalfOpen
)

// stateNames provides human-readable names for each state.
var stateNames = [...]string{
	StateClosed:   "closed",
	StateOpen:     "open",
	StateHalfOpen: "half_open",
}

// String returns the human-readable name of the state.
func (s State) String() string {
	if int(s) < len(stateNames) {
		return stateNames[s]
	}
	return "unknown"
}

// Config holds the circuit breaker configuration.
type Config struct {
	// Name identifies this circuit breaker instance (used in errors and metrics).
	Name string

	// FailureThreshold is the number of consecutive failures required to open the circuit.
	// Default: 5.
	FailureThreshold int32

	// SuccessThreshold is the number of consecutive successes in HalfOpen required to close.
	// Default: 2.
	SuccessThreshold int32

	// OpenTimeout is the duration the circuit remains open before transitioning to HalfOpen.
	// Default: 30s.
	OpenTimeout time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig(name string) Config {
	return Config{
		Name:             name,
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	}
}

// CircuitBreaker provides lock-free circuit breaker protection for function calls.
// All mutable fields are accessed exclusively through atomic operations.
type CircuitBreaker struct {
	name             string
	failureThreshold int32
	successThreshold int32
	openTimeout      int64 // nanoseconds

	// Mutable state (all accessed atomically)
	state              atomic.Int32
	consecutiveFailure atomic.Int32
	consecutiveSuccess atomic.Int32
	openedAt           atomic.Int64 // UnixNano timestamp when circuit was opened

	// Prometheus metrics (optional, nil-safe)
	metricsStateTransitions *prometheus.CounterVec
	metricsCallTotal        *prometheus.CounterVec

	// Clock function for testing (defaults to time.Now)
	nowFunc func() time.Time
}

// New creates a new CircuitBreaker with the given configuration.
// Returns an error if the configuration is invalid.
func New(cfg Config) (*CircuitBreaker, error) {
	if cfg.Name == "" {
		return nil, errors.New(
			errors.ErrInvalidInput,
			"circuitbreaker.New",
			"name must not be empty",
		)
	}
	if cfg.FailureThreshold <= 0 {
		return nil, errors.Newf(
			errors.ErrInvalidInput,
			"circuitbreaker.New",
			"failure threshold must be positive, got: %d", cfg.FailureThreshold,
		)
	}
	if cfg.SuccessThreshold <= 0 {
		return nil, errors.Newf(
			errors.ErrInvalidInput,
			"circuitbreaker.New",
			"success threshold must be positive, got: %d", cfg.SuccessThreshold,
		)
	}
	if cfg.OpenTimeout <= 0 {
		return nil, errors.Newf(
			errors.ErrInvalidInput,
			"circuitbreaker.New",
			"open timeout must be positive, got: %v", cfg.OpenTimeout,
		)
	}

	cb := &CircuitBreaker{
		name:             cfg.Name,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		openTimeout:      int64(cfg.OpenTimeout),
		nowFunc:          time.Now,
	}
	cb.state.Store(int32(StateClosed))

	return cb, nil
}

// SetMetrics configures optional Prometheus metrics for this circuit breaker.
// Both parameters may be nil to disable metrics.
func (cb *CircuitBreaker) SetMetrics(stateTransitions *prometheus.CounterVec, callTotal *prometheus.CounterVec) {
	cb.metricsStateTransitions = stateTransitions
	cb.metricsCallTotal = callTotal
}

// Execute runs the given function under circuit breaker protection.
// Returns a CircuitOpenError (ErrCircuitOpen) if the circuit is open and the
// timeout has not elapsed.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	// Check if the call is allowed
	if !cb.allowRequest() {
		cb.recordMetric(cb.metricsCallTotal, "rejected")
		return errors.New(
			errors.ErrCircuitOpen,
			"circuitbreaker.Execute",
			"circuit breaker '"+cb.name+"' is open",
		)
	}

	// Execute the protected function
	err := fn()

	// Record the outcome
	if err != nil {
		cb.recordFailure()
		cb.recordMetric(cb.metricsCallTotal, "failure")
		return err
	}

	cb.recordSuccess()
	cb.recordMetric(cb.metricsCallTotal, "success")
	return nil
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() State {
	current := State(cb.state.Load())

	// If the circuit is open, check whether the timeout has elapsed.
	// This is a read-only probe; the actual transition happens in allowRequest.
	if current == StateOpen && cb.openTimeoutElapsed() {
		return StateHalfOpen
	}

	return current
}

// Name returns the circuit breaker name.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// Reset forces the circuit breaker back to the closed state and clears all counters.
func (cb *CircuitBreaker) Reset() {
	oldState := State(cb.state.Swap(int32(StateClosed)))
	cb.consecutiveFailure.Store(0)
	cb.consecutiveSuccess.Store(0)
	cb.openedAt.Store(0)

	if oldState != StateClosed {
		cb.recordTransition(oldState, StateClosed)
	}
}

// Counts returns the current consecutive failure and success counts.
func (cb *CircuitBreaker) Counts() (failures int32, successes int32) {
	return cb.consecutiveFailure.Load(), cb.consecutiveSuccess.Load()
}

// allowRequest determines whether a request should be allowed through.
// Returns true if the request is permitted, false if the circuit is open.
func (cb *CircuitBreaker) allowRequest() bool {
	current := State(cb.state.Load())

	switch current {
	case StateClosed:
		return true

	case StateOpen:
		if !cb.openTimeoutElapsed() {
			return false
		}
		// Attempt CAS transition from Open to HalfOpen.
		// Only one goroutine wins the CAS; others see the circuit as open and are rejected.
		if cb.state.CompareAndSwap(int32(StateOpen), int32(StateHalfOpen)) {
			cb.consecutiveSuccess.Store(0)
			cb.recordTransition(StateOpen, StateHalfOpen)
		}
		return true

	case StateHalfOpen:
		return true

	default:
		return false
	}
}

// recordSuccess processes a successful call outcome.
func (cb *CircuitBreaker) recordSuccess() {
	current := State(cb.state.Load())

	switch current {
	case StateClosed:
		// Reset failure counter on success in closed state.
		cb.consecutiveFailure.Store(0)

	case StateHalfOpen:
		successes := cb.consecutiveSuccess.Add(1)
		if successes >= cb.successThreshold {
			// Enough successes in HalfOpen, attempt to close.
			if cb.state.CompareAndSwap(int32(StateHalfOpen), int32(StateClosed)) {
				cb.consecutiveFailure.Store(0)
				cb.consecutiveSuccess.Store(0)
				cb.recordTransition(StateHalfOpen, StateClosed)
			}
		}
	}
}

// recordFailure processes a failed call outcome.
func (cb *CircuitBreaker) recordFailure() {
	current := State(cb.state.Load())

	switch current {
	case StateClosed:
		failures := cb.consecutiveFailure.Add(1)
		if failures >= cb.failureThreshold {
			// Attempt CAS transition from Closed to Open.
			if cb.state.CompareAndSwap(int32(StateClosed), int32(StateOpen)) {
				cb.openedAt.Store(cb.nowFunc().UnixNano())
				cb.recordTransition(StateClosed, StateOpen)
			}
		}

	case StateHalfOpen:
		// Any failure in HalfOpen immediately reopens the circuit.
		if cb.state.CompareAndSwap(int32(StateHalfOpen), int32(StateOpen)) {
			cb.consecutiveSuccess.Store(0)
			cb.consecutiveFailure.Store(1)
			cb.openedAt.Store(cb.nowFunc().UnixNano())
			cb.recordTransition(StateHalfOpen, StateOpen)
		}
	}
}

// openTimeoutElapsed returns true if the open timeout has passed since the circuit was opened.
func (cb *CircuitBreaker) openTimeoutElapsed() bool {
	openedAt := cb.openedAt.Load()
	if openedAt == 0 {
		return false
	}
	elapsed := cb.nowFunc().UnixNano() - openedAt
	return elapsed >= cb.openTimeout
}

// recordTransition records a state transition metric if metrics are configured.
func (cb *CircuitBreaker) recordTransition(from, to State) {
	if cb.metricsStateTransitions == nil {
		return
	}
	cb.metricsStateTransitions.WithLabelValues(cb.name, from.String(), to.String()).Inc()
}

// recordMetric records a call outcome metric if metrics are configured.
func (cb *CircuitBreaker) recordMetric(counter *prometheus.CounterVec, result string) {
	if counter == nil {
		return
	}
	counter.WithLabelValues(cb.name, result).Inc()
}
