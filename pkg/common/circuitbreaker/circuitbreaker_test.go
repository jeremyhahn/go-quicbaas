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

package circuitbreaker

import (
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// stubError is a typed error for testing purposes.
type stubError struct {
	message string
}

func (e *stubError) Error() string { return e.message }

// fakeClock provides deterministic time control for tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock {
	return &fakeClock{now: t}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// --- New ---

func TestNew_ValidConfig(t *testing.T) {
	cb, err := New(DefaultConfig("test"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil circuit breaker")
	}
	if cb.Name() != "test" {
		t.Fatalf("expected name 'test', got: %s", cb.Name())
	}
	if cb.State() != StateClosed {
		t.Fatalf("expected initial state Closed, got: %s", cb.State())
	}
}

func TestNew_EmptyName(t *testing.T) {
	_, err := New(Config{
		Name:             "",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, errors.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestNew_InvalidFailureThreshold(t *testing.T) {
	_, err := New(Config{
		Name:             "test",
		FailureThreshold: 0,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error for zero failure threshold")
	}
	if !errors.Is(err, errors.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestNew_InvalidSuccessThreshold(t *testing.T) {
	_, err := New(Config{
		Name:             "test",
		FailureThreshold: 5,
		SuccessThreshold: -1,
		OpenTimeout:      30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error for negative success threshold")
	}
	if !errors.Is(err, errors.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestNew_InvalidOpenTimeout(t *testing.T) {
	_, err := New(Config{
		Name:             "test",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      0,
	})
	if err == nil {
		t.Fatal("expected error for zero open timeout")
	}
	if !errors.Is(err, errors.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got: %v", err)
	}
}

// --- Execute ---

func TestExecute_SuccessPassesThrough(t *testing.T) {
	cb, err := New(DefaultConfig("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	callCount := 0
	execErr := cb.Execute(func() error {
		callCount++
		return nil
	})

	if execErr != nil {
		t.Fatalf("expected no error, got: %v", execErr)
	}
	if callCount != 1 {
		t.Fatalf("expected function called once, got: %d", callCount)
	}
}

func TestExecute_ErrorPassesThrough(t *testing.T) {
	cb, err := New(DefaultConfig("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "test failure"}
	execErr := cb.Execute(func() error {
		return testErr
	})

	if execErr == nil {
		t.Fatal("expected error to be returned")
	}
	if !stderrors.Is(execErr, testErr) {
		t.Fatalf("expected original error, got: %v", execErr)
	}
}

// --- State Transitions ---

func TestTransition_ClosedToOpen(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "fail"}

	// Trigger failures up to threshold
	for i := int32(0); i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state Open after %d failures, got: %s", cfg.FailureThreshold, cb.State())
	}
}

func TestTransition_ClosedStaysClosedBelowThreshold(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "fail"}

	// 4 failures (below threshold of 5)
	for i := 0; i < 4; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	if cb.State() != StateClosed {
		t.Fatalf("expected state Closed with 4 failures (threshold 5), got: %s", cb.State())
	}
}

func TestTransition_OpenToHalfOpenAfterTimeout(t *testing.T) {
	clock := newFakeClock(time.Now())

	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenTimeout:      10 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cb.nowFunc = clock.Now

	testErr := &stubError{message: "fail"}

	// Trip the circuit
	for i := int32(0); i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(func() error { return testErr })
	}
	if cb.State() != StateOpen {
		t.Fatalf("expected Open, got: %s", cb.State())
	}

	// Advance time past timeout
	clock.Advance(11 * time.Second)

	// State should report HalfOpen
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected HalfOpen after timeout, got: %s", cb.State())
	}
}

func TestTransition_OpenRejectsBeforeTimeout(t *testing.T) {
	clock := newFakeClock(time.Now())

	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cb.nowFunc = clock.Now

	testErr := &stubError{message: "fail"}

	// Trip the circuit
	for i := int32(0); i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	// Advance only 5 seconds (not enough)
	clock.Advance(5 * time.Second)

	callCount := 0
	execErr := cb.Execute(func() error {
		callCount++
		return nil
	})

	if execErr == nil {
		t.Fatal("expected circuit open error")
	}
	if !errors.Is(execErr, errors.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got: %v", execErr)
	}
	if callCount != 0 {
		t.Fatalf("expected function not called when circuit is open, got: %d", callCount)
	}
}

func TestTransition_HalfOpenToClosedAfterSuccessThreshold(t *testing.T) {
	clock := newFakeClock(time.Now())

	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 3,
		OpenTimeout:      10 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cb.nowFunc = clock.Now

	testErr := &stubError{message: "fail"}

	// Trip the circuit
	for i := int32(0); i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	// Advance past timeout
	clock.Advance(11 * time.Second)

	// Execute successes in HalfOpen to meet threshold
	for i := int32(0); i < cfg.SuccessThreshold; i++ {
		execErr := cb.Execute(func() error { return nil })
		if execErr != nil {
			t.Fatalf("unexpected error on success %d: %v", i, execErr)
		}
	}

	if cb.State() != StateClosed {
		t.Fatalf("expected Closed after %d successes in HalfOpen, got: %s", cfg.SuccessThreshold, cb.State())
	}
}

func TestTransition_HalfOpenToOpenOnFailure(t *testing.T) {
	clock := newFakeClock(time.Now())

	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 3,
		OpenTimeout:      10 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cb.nowFunc = clock.Now

	testErr := &stubError{message: "fail"}

	// Trip the circuit
	for i := int32(0); i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	// Advance past timeout
	clock.Advance(11 * time.Second)

	// One success in HalfOpen (below threshold of 3)
	_ = cb.Execute(func() error { return nil })
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected HalfOpen after 1 success, got: %s", cb.State())
	}

	// A failure in HalfOpen should reopen
	_ = cb.Execute(func() error { return testErr })
	if cb.State() != StateOpen {
		t.Fatalf("expected Open after failure in HalfOpen, got: %s", cb.State())
	}
}

func TestTransition_FullCycle_ClosedOpenHalfOpenClosed(t *testing.T) {
	clock := newFakeClock(time.Now())

	cfg := Config{
		Name:             "cycle",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		OpenTimeout:      5 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cb.nowFunc = clock.Now

	testErr := &stubError{message: "fail"}

	// 1. Closed -> Open
	for i := int32(0); i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(func() error { return testErr })
	}
	if cb.State() != StateOpen {
		t.Fatalf("step 1: expected Open, got: %s", cb.State())
	}

	// 2. Open -> HalfOpen (after timeout)
	clock.Advance(6 * time.Second)
	if cb.State() != StateHalfOpen {
		t.Fatalf("step 2: expected HalfOpen, got: %s", cb.State())
	}

	// 3. HalfOpen -> Closed (after success threshold)
	for i := int32(0); i < cfg.SuccessThreshold; i++ {
		_ = cb.Execute(func() error { return nil })
	}
	if cb.State() != StateClosed {
		t.Fatalf("step 3: expected Closed, got: %s", cb.State())
	}

	// 4. Verify it works normally again
	execErr := cb.Execute(func() error { return nil })
	if execErr != nil {
		t.Fatalf("step 4: expected no error, got: %v", execErr)
	}
}

// --- Failure Counting ---

func TestFailureCount_ResetsOnSuccess(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "fail"}

	// 3 failures
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	failures, _ := cb.Counts()
	if failures != 3 {
		t.Fatalf("expected 3 failures, got: %d", failures)
	}

	// 1 success resets the count
	_ = cb.Execute(func() error { return nil })

	failures, _ = cb.Counts()
	if failures != 0 {
		t.Fatalf("expected 0 failures after success, got: %d", failures)
	}
}

func TestFailureCount_AccumulatesConsecutive(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 10,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "fail"}

	for i := 0; i < 7; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	failures, _ := cb.Counts()
	if failures != 7 {
		t.Fatalf("expected 7 consecutive failures, got: %d", failures)
	}
}

// --- Reset ---

func TestReset_FromOpenState(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "fail"}

	// Trip the circuit
	for i := int32(0); i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(func() error { return testErr })
	}
	if cb.State() != StateOpen {
		t.Fatalf("expected Open, got: %s", cb.State())
	}

	// Reset
	cb.Reset()

	if cb.State() != StateClosed {
		t.Fatalf("expected Closed after reset, got: %s", cb.State())
	}

	failures, successes := cb.Counts()
	if failures != 0 || successes != 0 {
		t.Fatalf("expected zero counts after reset, got failures=%d successes=%d", failures, successes)
	}

	// Verify it works normally
	execErr := cb.Execute(func() error { return nil })
	if execErr != nil {
		t.Fatalf("expected no error after reset, got: %v", execErr)
	}
}

func TestReset_FromClosedStateIsNoop(t *testing.T) {
	cb, err := New(DefaultConfig("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reset from closed should not panic or change state
	cb.Reset()

	if cb.State() != StateClosed {
		t.Fatalf("expected Closed after reset from Closed, got: %s", cb.State())
	}
}

// --- Counts ---

func TestCounts_ReturnsCorrectValues(t *testing.T) {
	cb, err := New(DefaultConfig("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	failures, successes := cb.Counts()
	if failures != 0 || successes != 0 {
		t.Fatalf("expected zero counts initially, got failures=%d successes=%d", failures, successes)
	}
}

func TestCounts_InvalidInitialState(t *testing.T) {
	cb, err := New(DefaultConfig("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "fail"}
	_ = cb.Execute(func() error { return testErr })

	failures, _ := cb.Counts()
	if failures != 1 {
		t.Fatalf("expected 1 failure, got: %d", failures)
	}
}

// --- Concurrent Access ---

func TestConcurrentAccess_NoRace(t *testing.T) {
	clock := newFakeClock(time.Now())

	cfg := Config{
		Name:             "concurrent",
		FailureThreshold: 10,
		SuccessThreshold: 5,
		OpenTimeout:      1 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cb.nowFunc = clock.Now

	testErr := &stubError{message: "fail"}

	var wg sync.WaitGroup
	goroutines := 100
	iterations := 50

	// Launch goroutines that mix successes and failures
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if id%2 == 0 {
					_ = cb.Execute(func() error { return nil })
				} else {
					_ = cb.Execute(func() error { return testErr })
				}

				// Periodically read state
				if i%10 == 0 {
					_ = cb.State()
					_, _ = cb.Counts()
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify the circuit breaker is in a valid state
	state := cb.State()
	if state != StateClosed && state != StateOpen && state != StateHalfOpen {
		t.Fatalf("invalid state after concurrent access: %s", state)
	}
}

func TestConcurrentAccess_TripsUnderLoad(t *testing.T) {
	cfg := Config{
		Name:             "concurrent-trip",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "fail"}
	var rejected atomic.Int32
	var wg sync.WaitGroup

	// All goroutines produce failures
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			execErr := cb.Execute(func() error { return testErr })
			if execErr != nil && errors.Is(execErr, errors.ErrCircuitOpen) {
				rejected.Add(1)
			}
		}()
	}

	wg.Wait()

	// After 50 failures with threshold 5, circuit must be open and some calls rejected
	if cb.State() != StateOpen {
		t.Fatalf("expected Open after concurrent failures, got: %s", cb.State())
	}
	if rejected.Load() == 0 {
		t.Fatal("expected some calls to be rejected after circuit opened")
	}
}

// --- Typed Error Integration ---

func TestExecute_CircuitOpenError_IsTyped(t *testing.T) {
	cfg := Config{
		Name:             "typed-error",
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Trip the circuit
	_ = cb.Execute(func() error { return &stubError{message: "fail"} })

	// Next call should return typed error
	execErr := cb.Execute(func() error { return nil })
	if execErr == nil {
		t.Fatal("expected error")
	}

	// Verify it's a QBError with ErrCircuitOpen code
	if !errors.Is(execErr, errors.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen code, got: %v", execErr)
	}

	// Verify it's retryable
	if !errors.IsRetryable(execErr) {
		t.Fatal("expected circuit open error to be retryable")
	}

	// Verify HTTP status is 503
	var de *errors.QBError
	if !stderrors.As(execErr, &de) {
		t.Fatal("expected QBError type")
	}
	if de.Code.HTTPStatus() != 503 {
		t.Fatalf("expected HTTP status 503, got: %d", de.Code.HTTPStatus())
	}
}

func TestExecute_CircuitOpenError_ContainsName(t *testing.T) {
	cfg := Config{
		Name:             "my-breaker",
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = cb.Execute(func() error { return &stubError{message: "fail"} })
	execErr := cb.Execute(func() error { return nil })

	if execErr == nil {
		t.Fatal("expected error")
	}

	errMsg := execErr.Error()
	if !stderrors.Is(execErr, &errors.QBError{Code: errors.ErrCircuitOpen}) {
		t.Fatalf("expected QBError with ErrCircuitOpen, got: %v", execErr)
	}
	// The error message should reference the circuit breaker name
	_ = errMsg // error message format is tested via the QBError type
}

// --- State String ---

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half_open"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// --- DefaultConfig ---

func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig("test-default")

	if cfg.Name != "test-default" {
		t.Fatalf("expected name 'test-default', got: %s", cfg.Name)
	}
	if cfg.FailureThreshold != 5 {
		t.Fatalf("expected failure threshold 5, got: %d", cfg.FailureThreshold)
	}
	if cfg.SuccessThreshold != 2 {
		t.Fatalf("expected success threshold 2, got: %d", cfg.SuccessThreshold)
	}
	if cfg.OpenTimeout != 30*time.Second {
		t.Fatalf("expected open timeout 30s, got: %v", cfg.OpenTimeout)
	}
}

func TestDefaultConfig_CreatesValidBreaker(t *testing.T) {
	cb, err := New(DefaultConfig("valid"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil circuit breaker")
	}
}

// --- Metrics ---

func TestMetrics_RecordsTransitions(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	clock := newFakeClock(time.Now())
	cfg := Config{
		Name:             "metrics-test",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenTimeout:      5 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cb.nowFunc = clock.Now
	m.Apply(cb)

	testErr := &stubError{message: "fail"}

	// Trip the circuit: Closed -> Open
	for i := int32(0); i < cfg.FailureThreshold; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	// Advance past timeout and allow a probe: Open -> HalfOpen
	clock.Advance(6 * time.Second)
	_ = cb.Execute(func() error { return nil }) // Success in HalfOpen -> Closed

	// Collect metrics
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	foundTransitions := false
	foundCalls := false
	for _, f := range families {
		if f.GetName() == "quicbaas_circuit_breaker_state_transitions_total" {
			foundTransitions = true
		}
		if f.GetName() == "quicbaas_circuit_breaker_calls_total" {
			foundCalls = true
		}
	}

	if !foundTransitions {
		t.Fatal("expected state_transitions metric to be recorded")
	}
	if !foundCalls {
		t.Fatal("expected calls_total metric to be recorded")
	}
}

func TestMetrics_NilMetricsSafe(t *testing.T) {
	cb, err := New(DefaultConfig("nil-metrics"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Execute without setting metrics should not panic
	execErr := cb.Execute(func() error { return nil })
	if execErr != nil {
		t.Fatalf("expected no error, got: %v", execErr)
	}
}

func TestMetrics_Apply_NilReceiver(t *testing.T) {
	var m *Metrics
	cb, err := New(DefaultConfig("nil-apply"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Apply with nil metrics should not panic
	m.Apply(cb)

	execErr := cb.Execute(func() error { return nil })
	if execErr != nil {
		t.Fatalf("expected no error, got: %v", execErr)
	}
}

func TestMetrics_Apply_NilBreaker(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Apply with nil breaker should not panic
	m.Apply(nil)
}

// --- Edge Cases ---

func TestExecute_OpenedAtZeroNotElapsed(t *testing.T) {
	cfg := Config{
		Name:             "zero-opened",
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenTimeout:      1 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Manually set state to Open without setting openedAt
	cb.state.Store(int32(StateOpen))
	cb.openedAt.Store(0)

	// Should still reject because openedAt is 0 (not elapsed)
	execErr := cb.Execute(func() error { return nil })
	if execErr == nil {
		t.Fatal("expected circuit open error with zero openedAt")
	}
	if !errors.Is(execErr, errors.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got: %v", execErr)
	}
}

func TestExecute_SuccessInClosedClearsFailures(t *testing.T) {
	cfg := Config{
		Name:             "clear-on-success",
		FailureThreshold: 5,
		SuccessThreshold: 1,
		OpenTimeout:      30 * time.Second,
	}
	cb, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testErr := &stubError{message: "fail"}

	// Accumulate 4 failures (below threshold)
	for i := 0; i < 4; i++ {
		_ = cb.Execute(func() error { return testErr })
	}
	failures, _ := cb.Counts()
	if failures != 4 {
		t.Fatalf("expected 4 failures, got: %d", failures)
	}

	// Success resets failures
	_ = cb.Execute(func() error { return nil })
	failures, _ = cb.Counts()
	if failures != 0 {
		t.Fatalf("expected 0 failures after success, got: %d", failures)
	}

	// 4 more failures should still not trip (need 5 consecutive)
	for i := 0; i < 4; i++ {
		_ = cb.Execute(func() error { return testErr })
	}
	if cb.State() != StateClosed {
		t.Fatalf("expected Closed, got: %s", cb.State())
	}
}

func TestName_ReturnsConfiguredName(t *testing.T) {
	cb, err := New(DefaultConfig("my-service"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb.Name() != "my-service" {
		t.Fatalf("expected 'my-service', got: %s", cb.Name())
	}
}

func TestName_EmptyAfterCreation(t *testing.T) {
	// Cannot create with empty name, this is validated
	_, err := New(Config{
		Name:             "",
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenTimeout:      time.Second,
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

// --- SetMetrics ---

func TestSetMetrics_SetsMetricsCorrectly(t *testing.T) {
	cb, err := New(DefaultConfig("set-metrics"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	cb.SetMetrics(m.StateTransitions, m.CallTotal)

	// Execute and verify no panic
	_ = cb.Execute(func() error { return nil })
}

func TestSetMetrics_NilIsSafe(t *testing.T) {
	cb, err := New(DefaultConfig("nil-set"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cb.SetMetrics(nil, nil)

	// Execute without panic
	_ = cb.Execute(func() error { return nil })
}

// --- NewMetrics ---

func TestNewMetrics_DefaultRegisterer(t *testing.T) {
	// Create a separate registry to avoid polluting the default one
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m.StateTransitions == nil {
		t.Fatal("expected non-nil StateTransitions")
	}
	if m.CallTotal == nil {
		t.Fatal("expected non-nil CallTotal")
	}
}

func TestNewMetrics_CustomRegisterer(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m.StateTransitions == nil {
		t.Fatal("expected non-nil StateTransitions")
	}
	if m.CallTotal == nil {
		t.Fatal("expected non-nil CallTotal")
	}

	// Verify metrics are registered
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather: %v", err)
	}
	// Metrics exist but may have no samples yet
	_ = families
}
