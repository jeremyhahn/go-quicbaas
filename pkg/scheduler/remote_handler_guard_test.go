package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// runningRemoteJob is a remote job already Running since startedAt, with no
// engine executing it: any node's completion is a completion elsewhere.
func runningRemoteJob(t *testing.T, storage *concludingStorage, timeout time.Duration, startedAt time.Time) *Job {
	t.Helper()
	job := remoteJob(timeout)
	job.Status = JobStatusRunning
	job.RunStartedAt = startedAt
	return seedRunningJob(t, storage.mockJobStorage, job)
}

// Two completions of one run on one non-firing node: one is recorded, the
// other is refused, and history holds one record.
func TestRemoteHandler_ConcurrentCompletionsRecordOneOutcome(t *testing.T) {
	storage := newConcludingStorage()
	node := clusteredNode(t, storage, "node-2")
	job := runningRemoteJob(t, storage, time.Minute, time.Now())
	ref := runRefOf(storedJob(t, storage.mockJobStorage, job.ID))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = node.CompleteRemoteRun(context.Background(), ref, &JobResult{Success: true}, nil)
		}(i)
	}
	wg.Wait()

	accepted := 0
	for _, err := range errs {
		switch {
		case err == nil:
			accepted++
		case !errors.Is(err, ErrRemoteRunNotOpen):
			t.Fatalf("CompleteRemoteRun: %v", err)
		}
	}
	if accepted != 1 {
		t.Fatalf("%d completions accepted, want 1", accepted)
	}
	if executions := history(t, storage.mockJobStorage, job.ID); len(executions) != 1 {
		t.Fatalf("history holds %d records for one run, want 1", len(executions))
	}
}

// A run past its deadline is not offered to workers and its completion is
// refused.
func TestRemoteHandler_AnExpiredRunIsNeitherListedNorCompleted(t *testing.T) {
	storage := newConcludingStorage()
	node := clusteredNode(t, storage, "node-2")
	job := runningRemoteJob(t, storage, time.Second, time.Now().Add(-time.Minute))

	runs, err := node.ListRemoteRuns(context.Background(), "tenant-a", []string{remoteRenew})
	if err != nil {
		t.Fatalf("ListRemoteRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("ListRemoteRuns offered %d expired runs, want none", len(runs))
	}
	ref := runRefOf(storedJob(t, storage.mockJobStorage, job.ID))
	if err := node.CompleteRemoteRun(context.Background(), ref, &JobResult{Success: true}, nil); !errors.Is(err, ErrRemoteRunNotOpen) {
		t.Fatalf("completing an expired run: err = %v, want ErrRemoteRunNotOpen", err)
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusRunning || stored.RunCount != 0 {
		t.Fatalf("the expired run's completion was recorded: %s with %d runs", stored.Status, stored.RunCount)
	}
}

// An update of a running job restamps UpdatedAt but keeps the run's
// identity, so the worker's reference still names the run.
func TestRemoteHandler_AnUpdateKeepsTheRunsIdentity(t *testing.T) {
	storage := newConcludingStorage()
	job := runningRemoteJob(t, storage, time.Minute, time.Now().Add(-time.Second))
	ref := runRefOf(storedJob(t, storage.mockJobStorage, job.ID))

	updated := storedJob(t, storage.mockJobStorage, job.ID)
	updated.Payload = []byte(`{"node":"node-b"}`)
	updated.UpdatedAt = time.Now().Add(time.Second)
	if err := storage.UpdateJob(context.Background(), updated); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if !ref.names(storedJob(t, storage.mockJobStorage, job.ID)) {
		t.Fatal("the update changed the identity of the running run")
	}
}

// The waiter closed by executeJob first, with no result, must not block the
// handler: it returns the context's error.
func TestRemoteHandler_ReturnsWhenTheWaitClosesFirst(t *testing.T) {
	node := clusteredNode(t, newConcludingStorage(), "node-1")
	w := &remoteWaiter{outcome: make(chan remoteOutcome, 1)}
	w.close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := node.remoteHandler(w)(ctx, &Job{})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("handler returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the handler blocked after executeJob closed the wait")
	}
}

// Two tenants' jobs sharing an ID keep separate started-run bookkeeping.
func TestRunKey_EqualJobIDsAcrossTenantsDoNotCollide(t *testing.T) {
	node := clusteredNode(t, newConcludingStorage(), "node-1")
	now := time.Now()
	a := &Job{ID: 7, OwnerType: OwnerTypeTenant, OwnerID: "tenant-a", NextRun: now, RunStartedAt: now}
	b := &Job{ID: 7, OwnerType: OwnerTypeTenant, OwnerID: "tenant-b", NextRun: now.Add(time.Hour), RunStartedAt: now.Add(time.Hour)}
	ctxA := node.noteRunStarted(context.Background(), a)
	node.noteRunStarted(context.Background(), b)
	if _, ok := node.startedRuns.Load(runKeyOf(a)); !ok {
		t.Fatal("tenant-b's run overwrote tenant-a's started run")
	}
	if !node.concludeOwnRun(ctxA, a) {
		t.Fatal("tenant-a's run did not conclude")
	}
	if _, ok := node.startedRuns.Load(runKeyOf(b)); !ok {
		t.Fatal("concluding tenant-a's run removed tenant-b's")
	}
}

// An old execution finishing after a newer run started neither consumes the
// newer run's entry nor concludes it.
func TestConcludeOwnRun_ASupersededRunLeavesTheNewerRun(t *testing.T) {
	storage := newConcludingStorage()
	node := clusteredNode(t, storage, "node-1")
	started := time.Now().Add(-time.Minute)
	r1 := runningRemoteJob(t, storage, time.Minute, started)
	ctx1 := node.noteRunStarted(context.Background(), copyJob(r1))

	// Another node completes R1.
	if won, err := storage.ConcludeRun(context.Background(), &RunConclusion{
		JobID: r1.ID, TenantID: JobTenantID(r1), Firing: r1.NextRun, StartedAt: started, Owner: "remote@node-2/1",
	}); err != nil || !won {
		t.Fatalf("remote conclusion of R1: won=%v err=%v", won, err)
	}

	// R2 starts and completes locally, while R1's execution still waits.
	r2 := copyJob(r1)
	r2.NextRun = r1.NextRun.Add(time.Hour)
	r2.RunStartedAt = started.Add(30 * time.Second)
	ctx2 := node.noteRunStarted(context.Background(), r2)
	r2.Status, r2.RunCount = JobStatusPending, 1
	if !node.persistJobOutcome(ctx2, r2) {
		t.Fatal("R2's outcome lost the guard with no contender")
	}

	// R1's execution times out: it holds no entry any more, yet its outcome
	// must lose to R2's conclusion rather than overwrite it.
	node.handleJobTimeout(ctx1, copyJob(r1), &JobExecution{JobID: r1.ID, StartedAt: started}, time.Minute)
	stored := storedJob(t, storage.mockJobStorage, r1.ID)
	if stored.Status != JobStatusPending || stored.RunCount != 1 || stored.FailCount != 0 {
		t.Fatalf("R1's stale timeout overwrote R2: %s, %d runs, %d failures", stored.Status, stored.RunCount, stored.FailCount)
	}
	if executions := history(t, storage.mockJobStorage, r1.ID); len(executions) != 0 {
		t.Fatalf("R1's stale timeout wrote %d history records, want none", len(executions))
	}
}

// A worker's output bytes are kept with the run's history.
func TestRemoteHandler_TheWorkersOutputIsKeptInHistory(t *testing.T) {
	storage := newConcludingStorage()
	node := clusteredNode(t, storage, "node-2")
	job := runningRemoteJob(t, storage, time.Minute, time.Now())
	ref := runRefOf(storedJob(t, storage.mockJobStorage, job.ID))
	result := &JobResult{Success: true, Message: "renewed", Output: []byte("-----BEGIN CERTIFICATE-----")}
	if err := node.CompleteRemoteRun(context.Background(), ref, result, nil); err != nil {
		t.Fatalf("CompleteRemoteRun: %v", err)
	}
	executions := history(t, storage.mockJobStorage, job.ID)
	if len(executions) != 1 || string(executions[0].Result) != string(result.Output) || executions[0].Output != "renewed" {
		t.Fatalf("history %+v does not keep the worker's output", executions)
	}
}
