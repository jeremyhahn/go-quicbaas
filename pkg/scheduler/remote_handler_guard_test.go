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
	node.noteRunStarted(a)
	node.noteRunStarted(b)
	if _, ok := node.startedRuns.Load(runKeyOf(a)); !ok {
		t.Fatal("tenant-b's run overwrote tenant-a's started run")
	}
	if !node.concludeOwnRun(context.Background(), a) {
		t.Fatal("tenant-a's run did not conclude")
	}
	if _, ok := node.startedRuns.Load(runKeyOf(b)); !ok {
		t.Fatal("concluding tenant-a's run removed tenant-b's")
	}
}

// An old execution finishing after a newer run started neither consumes the
// newer run's entry nor concludes it.
func TestConcludeOwnRun_ASupersededRunLeavesTheNewerRun(t *testing.T) {
	node := clusteredNode(t, newConcludingStorage(), "node-1")
	now := time.Now()
	old := &Job{ID: 9, NextRun: now, RunStartedAt: now}
	newer := &Job{ID: 9, NextRun: now.Add(time.Hour), RunStartedAt: now.Add(time.Hour)}
	node.noteRunStarted(newer)
	if node.concludeOwnRun(context.Background(), old) {
		t.Fatal("the superseded run's outcome was accepted")
	}
	if _, ok := node.startedRuns.Load(runKeyOf(newer)); !ok {
		t.Fatal("the superseded run consumed the newer run's entry")
	}
}
