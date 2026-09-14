// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeTurnPusher is a TurnPusher whose PushTurn behaviour and call count are
// controlled by the test, so JobRun.PushTurn's dispatch and error-wrapping
// can be asserted independently of any real runner.
type fakeTurnPusher struct {
	calls atomic.Int32
	err   error
}

func (f *fakeTurnPusher) PushTurn(_ context.Context) error {
	f.calls.Add(1)
	return f.err
}

// TestJobRun_PushTurn_NoPusherIsANoOp asserts that a run with nothing
// installed — every lead-driven job today, and every job on a persistent
// workspace — returns nil without touching the audit trail or the logger.
func TestJobRun_PushTurn_NoPusherIsANoOp(t *testing.T) {
	run := &JobRun{JobID: "job-noop"}

	if err := run.PushTurn(context.Background()); err != nil {
		t.Fatalf("PushTurn with no pusher installed = %v, want nil", err)
	}
	if got := len(run.auditEventsCopy()); got != 0 {
		t.Errorf("audit trail has %d events, want 0 for a run with no pusher", got)
	}
}

// TestJobRun_PushTurn_CallsInstalledPusher asserts the success path: a
// pusher installed via SetTurnPusher is invoked exactly once and its nil
// return is passed straight through.
func TestJobRun_PushTurn_CallsInstalledPusher(t *testing.T) {
	run := &JobRun{JobID: "job-push"}
	pusher := &fakeTurnPusher{}
	run.SetTurnPusher(pusher)

	if err := run.PushTurn(context.Background()); err != nil {
		t.Fatalf("PushTurn = %v, want nil", err)
	}
	if got := pusher.calls.Load(); got != 1 {
		t.Errorf("pusher.PushTurn called %d times, want exactly 1", got)
	}
}

// TestJobRun_PushTurn_FailurePropagatesTypedErrorAndAudits asserts the
// failure path: a pusher's error is wrapped in a *TurnPushError naming the
// job, filed on the job's own audit trail, and logged — because on the
// ephemeral tier a swallowed publication failure is silent data loss.
func TestJobRun_PushTurn_FailurePropagatesTypedErrorAndAudits(t *testing.T) {
	logger := &controlLoopMockLogger{}
	run := &JobRun{JobID: "job-fail", Logger: logger}
	cause := errors.New("push: non-fast-forward")
	pusher := &fakeTurnPusher{err: cause}
	run.SetTurnPusher(pusher)

	err := run.PushTurn(context.Background())
	if err == nil {
		t.Fatal("PushTurn = nil, want a *TurnPushError")
	}
	var pushErr *TurnPushError
	if !errors.As(err, &pushErr) {
		t.Fatalf("PushTurn error = %T, want *TurnPushError", err)
	}
	if pushErr.JobID != "job-fail" {
		t.Errorf("TurnPushError.JobID = %q, want %q", pushErr.JobID, "job-fail")
	}
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false, want true — Unwrap must expose the pusher's own error")
	}

	events := run.auditEventsCopy()
	if len(events) != 1 || events[0].Type != auditTurnPushFailed {
		t.Errorf("audit events = %+v, want exactly one %q event", events, auditTurnPushFailed)
	}
	if got := len(logger.errorLines()); got != 1 {
		t.Errorf("logger.Errorf called %d times, want exactly 1", got)
	}
}

// TestTurnPushError_ErrorAndUnwrap asserts the typed error's own contract:
// Error() names both the job and the cause, and Unwrap exposes the cause to
// errors.Is/As rather than hiding it behind a formatted string.
func TestTurnPushError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	err := &TurnPushError{JobID: "job-1", Cause: cause}

	if got := err.Error(); got == "" {
		t.Fatal("Error() returned an empty string")
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true")
	}
	if unwrapped := err.Unwrap(); unwrapped != cause {
		t.Errorf("Unwrap() = %v, want the exact cause %v", unwrapped, cause)
	}
}

// TestTurnPushError_UnwrapNilCause asserts that a malformed TurnPushError
// carrying no cause degrades to a nil Unwrap rather than panicking — the bad
// input case for a type whose only field access is a method call.
func TestTurnPushError_UnwrapNilCause(t *testing.T) {
	err := &TurnPushError{JobID: "job-1"}

	if unwrapped := err.Unwrap(); unwrapped != nil {
		t.Errorf("Unwrap() = %v, want nil for a TurnPushError with no cause", unwrapped)
	}
}

// TestJobRun_PushTurn_TheLastInstalledPusherWins covers re-installation, which
// is what a resumed run does when it re-spawns its worker: the publication must
// go to the LIVE container's publisher, never to the dead container's, whose
// exec seam would refuse or — worse — answer about a stale checkout.
func TestJobRun_PushTurn_TheLastInstalledPusherWins(t *testing.T) {
	run := &JobRun{JobID: "job-respawn"}
	stale := &fakeTurnPusher{err: errors.New("that container is gone")}
	live := &fakeTurnPusher{}
	run.SetTurnPusher(stale)
	run.SetTurnPusher(live)

	if err := run.PushTurn(context.Background()); err != nil {
		t.Fatalf("PushTurn = %v, want the live pusher's nil", err)
	}
	if got := stale.calls.Load(); got != 0 {
		t.Errorf("the replaced pusher was called %d time(s), want 0", got)
	}
	if got := live.calls.Load(); got != 1 {
		t.Errorf("the installed pusher was called %d time(s), want 1", got)
	}
}

// TestTurnPushError_ErrorWithNoCause is the bad-input twin of Error(): a
// malformed error must still name the job rather than panicking, because the
// one thing a reader needs from it is which job's work is at risk.
func TestTurnPushError_ErrorWithNoCause(t *testing.T) {
	err := &TurnPushError{JobID: "job-1"}
	if got := err.Error(); !strings.Contains(got, "job-1") {
		t.Fatalf("Error() = %q, want it to name the job", got)
	}
}
