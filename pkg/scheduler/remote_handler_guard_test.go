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
	storage := newConcludingStorage()
	node := clusteredNode(t, storage, "node-1")
	now := time.Now()
	a := seedRunningJob(t, storage.mockJobStorage, &Job{
		ID: 7, OwnerType: OwnerTypeTenant, OwnerID: "tenant-a", Status: JobStatusRunning, NextRun: now, RunStartedAt: now,
	})
	a = copyJob(a)
	b := &Job{ID: 7, OwnerType: OwnerTypeTenant, OwnerID: "tenant-b", NextRun: now.Add(time.Hour), RunStartedAt: now.Add(time.Hour)}
	ctxA := node.noteRunStarted(context.Background(), a)
	node.noteRunStarted(context.Background(), b)
	if _, ok := node.startedRuns.Load(runKeyOf(a)); !ok {
		t.Fatal("tenant-b's run overwrote tenant-a's started run")
	}
	if _, won := node.concludeOwnRun(ctxA, a, outcomeOf(a)); !won {
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
	r1Run := RunConclusion{JobID: r1.ID, TenantID: JobTenantID(r1), Firing: r1.NextRun, StartedAt: started}
	r1Outcome := copyJob(r1)
	r1Outcome.Status, r1Outcome.RunCount = JobStatusPending, 1
	if won, err := storage.ConcludeRun(context.Background(), &r1Run,
		outcomeTransition(r1Run, outcomeOf(r1Outcome), "remote@node-2/1")); err != nil || !won {
		t.Fatalf("remote conclusion of R1: won=%v err=%v", won, err)
	}

	// R2 starts and completes locally, while R1's execution still waits.
	r2 := copyJob(r1)
	r2.NextRun = r1.NextRun.Add(time.Hour)
	r2.RunStartedAt = started.Add(30 * time.Second)
	if err := storage.UpdateJob(context.Background(), copyJob(r2)); err != nil {
		t.Fatalf("starting R2: %v", err)
	}
	ctx2 := node.noteRunStarted(context.Background(), r2)
	r2.Status, r2.RunCount = JobStatusPending, 1
	if !node.persistJobOutcome(ctx2, r2, outcomeOf(r2)) {
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

// TestRemoteHandler_TheFiringNodeRefusesARunConcludedElsewhere: a completion
// offered to the firing node after another node concluded the run is
// refused, and the other node's result stands.
func TestRemoteHandler_TheFiringNodeRefusesARunConcludedElsewhere(t *testing.T) {
	storage := newConcludingStorage()
	firing := clusteredNode(t, storage, "node-1")
	other := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, remoteJob(time.Minute))

	done := fire(firing, job)
	ref := openRun(t, firing, job.ID).RemoteRunRef
	if err := other.CompleteRemoteRun(context.Background(), ref, &JobResult{Message: "from node-2"}, nil); err != nil {
		t.Fatalf("CompleteRemoteRun on another node: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := firing.CompleteRemoteRun(ctx, ref, &JobResult{Message: "from node-1"}, nil); !errors.Is(err, ErrRemoteRunNotOpen) {
		t.Fatalf("a completion on the firing node of a run concluded elsewhere: err = %v, want ErrRemoteRunNotOpen", err)
	}
	waitDone(t, done)

	executions := history(t, storage.mockJobStorage, job.ID)
	if len(executions) != 1 || executions[0].Output != "from node-2" {
		t.Fatalf("history is %+v, want the one record node-2 concluded", executions)
	}
}

// TestRemoteHandler_TheFiringNodeReportsAFailedConclusion: a conclusion that
// could not be written is reported to the worker, not acknowledged.
func TestRemoteHandler_TheFiringNodeReportsAFailedConclusion(t *testing.T) {
	storage := newConcludingStorage()
	firing := clusteredNode(t, storage, "node-1")
	job := seedRunningJob(t, storage.mockJobStorage, remoteJob(time.Minute))

	done := fire(firing, job)
	ref := openRun(t, firing, job.ID).RemoteRunRef
	storage.errMu.Lock()
	storage.updateJobErr = errors.New("raft: no quorum")
	storage.errMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := firing.CompleteRemoteRun(ctx, ref, &JobResult{Message: "renewed"}, nil)
	var storageErr ErrStorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("a completion whose conclusion failed to write: err = %v, want an ErrStorageError", err)
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusRunning {
		t.Fatalf("STORED job is %s after a failed conclusion, want running", stored.Status)
	}

	storage.errMu.Lock()
	storage.updateJobErr = nil
	storage.errMu.Unlock()
	waitDone(t, done)
}

// TestRemoteHandler_TheFiringNodeKeepsAFailedHistoryWritePending: on a
// clustered engine a failed history write after the conclusion is kept on
// the job and acknowledged; the record is written later.
func TestRemoteHandler_TheFiringNodeKeepsAFailedHistoryWritePending(t *testing.T) {
	storage := newConcludingStorage()
	firing := clusteredNode(t, storage, "node-1")
	job := seedRunningJob(t, storage.mockJobStorage, remoteJob(time.Minute))

	done := fire(firing, job)
	ref := openRun(t, firing, job.ID).RemoteRunRef
	storage.errMu.Lock()
	storage.saveExecutionErr = errors.New("raft: no quorum")
	storage.errMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := firing.CompleteRemoteRun(ctx, ref, &JobResult{Success: true, Message: "renewed"}, nil); err != nil {
		t.Fatalf("a completion whose history was kept pending: %v", err)
	}
	waitDone(t, done)

	assertHistoryKeptPending(t, storage.mockJobStorage, job.ID)
	storage.mockJobStorage.errMu.Lock()
	storage.mockJobStorage.saveExecutionErr = nil
	storage.mockJobStorage.errMu.Unlock()
	if err := firing.flushPendingHistory(context.Background(), storedJob(t, storage.mockJobStorage, job.ID)); err != nil {
		t.Fatalf("flushPendingHistory: %v", err)
	}
	assertPendingHistoryWritten(t, storage.mockJobStorage, job.ID)
}

// TestRemoteHandler_TheFiringNodeReportsALostHistoryRecord: when neither the
// history write nor keeping it pending succeeds, the worker is told.
func TestRemoteHandler_TheFiringNodeReportsALostHistoryRecord(t *testing.T) {
	storage := newConcludingStorage()
	firing := clusteredNode(t, storage, "node-1")
	job := seedRunningJob(t, storage.mockJobStorage, remoteJob(time.Minute))

	done := fire(firing, job)
	ref := openRun(t, firing, job.ID).RemoteRunRef
	// The conclusion does not read through GetJob; keeping the record
	// pending does, so only the latter fails.
	storage.errMu.Lock()
	storage.saveExecutionErr = errors.New("raft: no quorum")
	storage.getJobErr = errors.New("raft: no quorum")
	storage.errMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := firing.CompleteRemoteRun(ctx, ref, &JobResult{Success: true, Message: "renewed"}, nil)
	var storageErr ErrStorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("a completion whose history was lost: err = %v, want an ErrStorageError", err)
	}

	storage.errMu.Lock()
	storage.saveExecutionErr = nil
	storage.getJobErr = nil
	storage.errMu.Unlock()
	waitDone(t, done)
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.ConcludedBy != "execution@node-1" {
		t.Fatalf("the outcome was not written: ConcludedBy = %q", stored.ConcludedBy)
	}
}

// TestRemoteHandler_AnUpdateDuringTheCompletionIsKept: a completion reads the
// job, releases its stripe lock and then concludes the run. An update that
// commits in between is kept: the outcome is applied to the updated record,
// and the next firing follows the updated schedule.
func TestRemoteHandler_AnUpdateDuringTheCompletionIsKept(t *testing.T) {
	storage := newConcludingStorage()
	node := clusteredNode(t, storage, "node-2")
	job := runningRemoteJob(t, storage, time.Minute, time.Now())
	ref := runRefOf(storedJob(t, storage.mockJobStorage, job.ID))
	storage.beforeConclude = func() {
		updated := storedJob(t, storage.mockJobStorage, job.ID)
		updated.Name = "renew-cert-v2"
		updated.Payload = []byte(`{"node":"node-b"}`)
		updated.Schedule = "30 * * * *"
		if err := storage.mockJobStorage.UpdateJob(context.Background(), updated); err != nil {
			t.Errorf("the interleaved update: %v", err)
		}
	}
	if err := node.CompleteRemoteRun(context.Background(), ref, &JobResult{Success: true, Message: "renewed"}, nil); err != nil {
		t.Fatalf("CompleteRemoteRun: %v", err)
	}

	stored := storedJob(t, storage.mockJobStorage, job.ID)
	if stored.Name != "renew-cert-v2" || string(stored.Payload) != `{"node":"node-b"}` || stored.Schedule != "30 * * * *" {
		t.Fatalf("the completion undid the update: name %q, payload %s, schedule %q", stored.Name, stored.Payload, stored.Schedule)
	}
	if stored.Status != JobStatusPending || stored.RunCount != 1 || stored.ConcludedRunAt.IsZero() {
		t.Fatalf("the run's outcome was not applied: %s, %d runs, concluded at %s", stored.Status, stored.RunCount, stored.ConcludedRunAt)
	}
	if stored.NextRun.Minute() != 30 {
		t.Fatalf("next firing %s does not follow the updated schedule", stored.NextRun)
	}
}

// TestPersistJobOutcome_KeepsHistoryAnotherRunLeftPending: on a single node,
// run A's history write fails and its record is kept pending on the job,
// while run B still holds the snapshot from before. B's outcome must not
// write that snapshot's empty pending history over A's record.
func TestPersistJobOutcome_KeepsHistoryAnotherRunLeftPending(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	started := time.Now()
	job := remoteJob(time.Minute)
	job.Status, job.RunStartedAt = JobStatusRunning, started
	seedRunningJob(t, storage, job)
	runA := copyJob(storedJob(t, storage, job.ID))
	runB := copyJob(storedJob(t, storage, job.ID))

	storage.errMu.Lock()
	storage.saveExecutionErr = errors.New("raft: no quorum")
	storage.errMu.Unlock()
	// A is a remote run: its completion waits on the result, so a failed
	// history write is kept pending on the job and A is acknowledged.
	ctxA := withOwnRun(context.Background(), ownRun{result: &ownRunResult{}})
	engine.handleJobSuccess(ctxA, runA, &JobExecution{JobID: job.ID, StartedAt: started},
		&JobResult{Success: true, Message: "renewed"})
	assertHistoryKeptPending(t, storage, job.ID)

	storage.errMu.Lock()
	storage.saveExecutionErr = nil
	storage.errMu.Unlock()
	engine.handleJobSuccess(context.Background(), runB, &JobExecution{JobID: job.ID, StartedAt: started.Add(time.Second)},
		&JobResult{Success: true, Message: "second"})

	stored := storedJob(t, storage, job.ID)
	kept := false
	for _, rec := range stored.PendingHistory {
		kept = kept || rec.Output == "renewed"
	}
	for _, rec := range history(t, storage, job.ID) {
		kept = kept || rec.Output == "renewed"
	}
	if !kept {
		t.Fatalf("run A's acknowledged history was lost: %d pending, record %+v", len(stored.PendingHistory), stored)
	}
	if stored.RunCount != 2 {
		t.Fatalf("job ran %d times, want 2", stored.RunCount)
	}
}
