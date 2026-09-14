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
	"sync"
	"testing"
	"time"
)

// drainTestGrace is how long a drain test waits for a step that must happen
// promptly. It bounds a wait on a channel, never a sleep between steps.
const drainTestGrace = 5 * time.Second

// drainTestBound is the drain window a test gives a job that will not finish.
const drainTestBound = 100 * time.Millisecond

// startBlockedJob launches a job whose runner blocks until release is
// called, so a test can hold a control loop open across a shutdown. The
// runner is released and its goroutine waited for at the end of the test,
// so no job outlives the test that started it.
func startBlockedJob(t *testing.T, svc *agentServiceImpl, name string) (jobID string, release func()) {
	t.Helper()
	held := make(chan struct{})
	running := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(held) }) }

	runner := &fakeRunner{name: name, run: func(ctx context.Context, _ *JobRun) error {
		close(running)
		select {
		case <-held:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	registerJobRunnerForTest(t, runner)

	jobID, err := svc.launchJob(context.Background(), &JobConfig{
		TenantID: "acme", Description: "held open across shutdown", Runner: name,
	}, "", nil)
	if err != nil {
		t.Fatalf("launchJob: %v", err)
	}
	select {
	case <-running:
	case <-time.After(drainTestGrace):
		t.Fatal("the job's runner never started")
	}

	t.Cleanup(func() {
		release()
		select {
		case <-waitGroupDone(&svc.jobsInFlight):
		case <-time.After(drainTestGrace):
			t.Error("the job goroutine did not return after the runner was released")
		}
	})
	return jobID, release
}

// TestShutdown_DrainsARunningJobBeforeItReturns proves the drain waits: the
// daemon does not reach the store close while a control loop is still
// writing, which is the whole point of having one.
func TestShutdown_DrainsARunningJobBeforeItReturns(t *testing.T) {
	svc := newTestService(t)
	_, release := startBlockedJob(t, svc, "drain-waits-for-me")

	drained := make(chan error, 1)
	go func() { drained <- svc.Shutdown(context.Background()) }()

	// The drain must still be waiting while the loop runs.
	holding := time.NewTimer(50 * time.Millisecond)
	defer holding.Stop()
	select {
	case err := <-drained:
		t.Fatalf("Shutdown returned %v while a control loop was still running", err)
	case <-holding.C:
	}

	release()
	select {
	case err := <-drained:
		if err != nil {
			t.Fatalf("Shutdown() = %v, want nil once the loop finished", err)
		}
	case <-time.After(drainTestGrace):
		t.Fatal("Shutdown did not return after the loop finished")
	}
}

// TestShutdown_LeavesAJobItCouldNotDrainForRecovery proves the OWNER DECISION
// on #571: a loop still mid-turn when the bound expires is NOT cancelled and
// failed. Its record stays non-terminal, which is what RecoverJobs resumes
// from on the next start — the same disposition as being killed today.
func TestShutdown_LeavesAJobItCouldNotDrainForRecovery(t *testing.T) {
	svc := newTestService(t)
	jobID, _ := startBlockedJob(t, svc, "drain-cannot-finish-me")

	ctx, cancel := context.WithTimeout(context.Background(), drainTestBound)
	defer cancel()

	err := svc.Shutdown(ctx)

	var timedOut *DrainTimeoutError
	if !errors.As(err, &timedOut) {
		t.Fatalf("Shutdown() = %v, want a DrainTimeoutError", err)
	}
	if !errors.Is(err, ErrDrainTimeout) {
		t.Fatalf("Shutdown() = %v, want it to match ErrDrainTimeout", err)
	}
	if len(timedOut.Running) == 0 {
		t.Fatal("a drain that timed out must name the loops it left running")
	}
	found := false
	for _, id := range timedOut.Running {
		if id == "acme/"+jobID {
			found = true
		}
	}
	if !found {
		t.Fatalf("DrainTimeoutError running = %v, want it to name %q", timedOut.Running, "acme/"+jobID)
	}

	status, statusErr := svc.GetJobStatus(ContextWithTenant(context.Background(), "acme"), "acme", jobID)
	if statusErr != nil {
		t.Fatalf("GetJobStatus after a timed-out drain: %v", statusErr)
	}
	if IsTerminalJobState(status.State) {
		t.Fatalf("job state = %s: a loop the drain could not finish is left for RecoverJobs, not failed", status.State)
	}
}

// TestShutdown_RefusesToStartNewWork proves the drain is a one-way door: a
// job launched after it began would be work the daemon has no time left to
// do, and the transports are still accepting requests at that point.
func TestShutdown_RefusesToStartNewWork(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown with nothing running: %v", err)
	}

	runner := &fakeRunner{name: "refused-after-shutdown"}
	registerJobRunnerForTest(t, runner)
	_, err := svc.launchJob(context.Background(), &JobConfig{
		TenantID: "acme", Description: "too late", Runner: runner.Name(),
	}, "", nil)

	var shuttingDown *ServiceShuttingDownError
	if !errors.As(err, &shuttingDown) {
		t.Fatalf("launchJob after Shutdown = %v, want a ServiceShuttingDownError", err)
	}
	if !errors.Is(err, ErrServiceShuttingDown) {
		t.Fatalf("launchJob after Shutdown = %v, want it to match ErrServiceShuttingDown", err)
	}
	count := 0
	svc.jobs.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Fatalf("a refused job must not be registered, found %d", count)
	}
}

// TestShutdown_WithNothingRunningIsCleanAndIdempotent proves the ordinary
// case — a daemon stopping with no work in flight — and that a second call
// is harmless, because Server.Shutdown may be reached more than once.
func TestShutdown_WithNothingRunningIsCleanAndIdempotent(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown() = %v, want nil", err)
	}
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() = %v, want nil", err)
	}
}

// TestShutdown_OnAnAlreadyExpiredContextStillReportsWhatItLeft proves the
// invalid-input path: a caller that hands the drain no time at all gets the
// typed timeout rather than a hang or a nil error.
func TestShutdown_OnAnAlreadyExpiredContextStillReportsWhatItLeft(t *testing.T) {
	svc := newTestService(t)
	_, _ = startBlockedJob(t, svc, "drain-no-budget")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var timedOut *DrainTimeoutError
	if err := svc.Shutdown(ctx); !errors.As(err, &timedOut) {
		t.Fatalf("Shutdown on a cancelled context = %v, want a DrainTimeoutError", err)
	}
}

// TestDrainTimeoutError_MessageNamesThePhaseAndWhatWasLeft proves the error
// says what an operator needs: which drain phase gave up, after how long,
// and what is still running.
func TestDrainTimeoutError_MessageNamesThePhaseAndWhatWasLeft(t *testing.T) {
	t.Parallel()
	err := &DrainTimeoutError{Phase: "jobs", Waited: 10 * time.Second, Running: []string{"acme/job-1", "acme/job-2"}}
	const want = "orchestration: drain timed out: phase=jobs waited=10s running=acme/job-1,acme/job-2"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrDrainTimeout) {
		t.Fatal("a DrainTimeoutError matches ErrDrainTimeout")
	}

	bare := &DrainTimeoutError{Phase: "turns", Waited: time.Second}
	if bare.Error() != "orchestration: drain timed out: phase=turns waited=1s" {
		t.Fatalf("Error() = %q, want the running list omitted when empty", bare.Error())
	}
}

// TestServiceShuttingDownError_NamesTheOperationRefused proves the refusal
// says which operation was refused, and matches its sentinel.
func TestServiceShuttingDownError_NamesTheOperationRefused(t *testing.T) {
	t.Parallel()
	err := &ServiceShuttingDownError{Operation: "launch job"}
	const want = "orchestration: service is shutting down: launch job"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrServiceShuttingDown) {
		t.Fatal("a ServiceShuttingDownError matches ErrServiceShuttingDown")
	}

	bare := &ServiceShuttingDownError{}
	if bare.Error() != "orchestration: service is shutting down" {
		t.Fatalf("Error() = %q, want the operation omitted when unknown", bare.Error())
	}
}
