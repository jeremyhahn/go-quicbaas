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

package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test failsafes.
//
// Several tests here assert that an operation completes rather than blocks:
// Start returns in embedded mode, a signal closes the shutdown channel, a
// listener comes up. A fixed wall-clock budget ("returns within 10s") is only
// a proxy for that assertion — it proves nothing extra about the code and
// fails whenever the machine is busy, which on a shared CI runner it routinely
// is. The tests below therefore synchronise on the operation's own completion
// channel and keep a timeout purely as a failsafe against a genuine hang.
//
// The failsafe is derived from the test binary's own deadline
// (`go test -timeout`, 10m by default) rather than from a constant, so it
// scales with whatever budget the caller gave the suite.

// failsafeReportMargin is the slice of the remaining test-binary budget left
// to the failing test so it can report its own message before `go test`
// panics the whole process with a stack dump.
const failsafeReportMargin = 5 * time.Second

// failsafeDelay returns how long a failsafe timer started at now should run
// for a test binary whose deadline is deadline. The result is the remaining
// budget less failsafeReportMargin, and never more than the budget itself; a
// budget already spent yields a non-positive delay, i.e. an immediate fire.
func failsafeDelay(now, deadline time.Time) time.Duration {
	budget := deadline.Sub(now)
	margin := failsafeReportMargin
	if budget < 2*margin {
		// Short budget: still reserve a tenth of it for reporting.
		margin = budget / 10
	}
	return budget - margin
}

// testFailsafe returns a channel that fires shortly before the test binary's
// deadline. A nil channel — one that blocks forever — is returned when the
// binary runs without a deadline (`go test -timeout 0`): running until
// interrupted is exactly what that flag asks for, and the resulting goroutine
// dump is better evidence of a hang than an arbitrary cutoff would be.
func testFailsafe(t *testing.T) <-chan time.Time {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		return nil
	}
	return time.After(failsafeDelay(time.Now(), deadline))
}

// awaitResult waits for the result of an operation running in its own
// goroutine — typically Server.Start — and returns it. what names the
// operation in the failure message.
func awaitResult(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-testFailsafe(t):
		t.Fatalf("%s did not return before the test deadline", what)
		return nil // unreachable: Fatalf ends the goroutine.
	}
}

// awaitNoError waits for an operation to finish and requires it to succeed.
func awaitNoError(t *testing.T, done <-chan error, what string) {
	t.Helper()
	require.NoError(t, awaitResult(t, done, what), "%s must succeed", what)
}

// awaitSignal waits for ch to be closed or to yield a value. what names the
// event in the failure message.
func awaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-testFailsafe(t):
		t.Fatalf("%s did not happen before the test deadline", what)
	}
}

// awaitPollInterval is how often awaitCond re-evaluates its condition. It
// bounds the latency of noticing a state change, not the test's patience.
const awaitPollInterval = 100 * time.Millisecond

// awaitCond polls cond until it reports true. It exists for state that is not
// published on a channel — a listener address becoming non-nil, a node
// finishing its boot — and, like the helpers above, is bounded only by the
// test binary's deadline.
func awaitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ticker := time.NewTicker(awaitPollInterval)
	defer ticker.Stop()
	failsafe := testFailsafe(t)
	for {
		select {
		case <-ticker.C:
			if cond() {
				return
			}
		case <-failsafe:
			t.Fatalf("%s did not happen before the test deadline", what)
		}
	}
}

// =============================================================================
// tests for the helpers above
// =============================================================================

func TestFailsafeDelay_LeavesReportingMarginWithinBudget(t *testing.T) {
	now := time.Unix(0, 0)

	// A full 10m `go test -timeout` budget: the failsafe fires one margin
	// before the harness would kill the process.
	assert.Equal(t, 10*time.Minute-failsafeReportMargin,
		failsafeDelay(now, now.Add(10*time.Minute)))

	// A budget shorter than two margins keeps a proportional slice instead
	// of going negative and firing immediately.
	short := failsafeDelay(now, now.Add(4*time.Second))
	assert.Equal(t, 4*time.Second-400*time.Millisecond, short)
	assert.Greater(t, short, time.Duration(0), "a live budget must yield a live timer")
}

func TestFailsafeDelay_SpentBudgetFiresImmediately(t *testing.T) {
	now := time.Unix(0, 0)

	// Deadline already passed: no time left to wait for anything.
	assert.LessOrEqual(t, failsafeDelay(now, now.Add(-time.Minute)), time.Duration(0))
	assert.LessOrEqual(t, failsafeDelay(now, now), time.Duration(0))
}

func TestTestFailsafe_TracksTheTestBinaryDeadline(t *testing.T) {
	deadline, ok := t.Deadline()
	failsafe := testFailsafe(t)
	if !ok {
		assert.Nil(t, failsafe, "without a deadline the failsafe must block forever")
		return
	}

	assert.NotNil(t, failsafe)
	select {
	case <-failsafe:
		t.Fatal("failsafe fired immediately despite a live test deadline")
	default:
	}
	assert.Greater(t, time.Until(deadline), time.Duration(0))
}

func TestAwaitHelpers_ReturnOnceTheOperationCompletes(t *testing.T) {
	done := make(chan error, 1)
	done <- nil
	awaitNoError(t, done, "an operation that succeeded")

	failed := make(chan error, 1)
	failed <- assert.AnError
	assert.ErrorIs(t, awaitResult(t, failed, "an operation that failed"), assert.AnError,
		"awaitResult must surface the operation's error rather than assert on it")

	closed := make(chan struct{})
	close(closed)
	awaitSignal(t, closed, "a closed channel")

	calls := 0
	awaitCond(t, "a condition that becomes true", func() bool {
		calls++
		return calls >= 2
	})
	assert.Equal(t, 2, calls, "awaitCond must poll until the condition holds")
}
