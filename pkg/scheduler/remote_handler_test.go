package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

const remoteRenew = RemoteHandlerPrefix + "renew-cert/node-a"

func remoteJob(timeout time.Duration) *Job {
	now := time.Now().Truncate(time.Second)
	return &Job{
		Name:      "renew-cert",
		OwnerType: OwnerTypeTenant,
		OwnerID:   "tenant-a",
		Type:      JobTypeRecurring,
		Schedule:  "0 * * * *",
		Handler:   remoteRenew,
		Payload:   []byte(`{"node":"node-a"}`),
		Timeout:   timeout,
		Status:    JobStatusPending,
		NextRun:   now,
		UpdatedAt: now.Add(-time.Minute),
	}
}

// fire runs one execution of job on engine and returns a channel closed when
// the execution has recorded its outcome.
func fire(engine *SchedulerEngine, job *Job) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.executeJob(context.Background(), copyJob(job), time.Time{})
	}()
	return done
}

// openRun polls the engine for the job's open remote run, as a worker does.
func openRun(t *testing.T, engine *SchedulerEngine, jobID uint64) *RemoteRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := engine.ListRemoteRuns(context.Background(), "tenant-a", []string{remoteRenew})
		if err != nil {
			t.Fatalf("ListRemoteRuns: %v", err)
		}
		for _, run := range runs {
			if run.JobID == jobID {
				return run
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %d never opened a remote run", jobID)
	return nil
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the execution never recorded an outcome")
	}
}

func history(t *testing.T, storage *mockJobStorage, jobID uint64) []*JobExecution {
	t.Helper()
	executions, err := storage.GetExecutions(context.Background(), jobID, "", 10)
	if err != nil {
		t.Fatalf("GetExecutions: %v", err)
	}
	return executions
}

// TestRemoteHandler_TheWorkerCompletesTheRun is go-quicbaas#1955: a job whose
// handler no process of the server registers is run by a worker that pulls
// it. Before, the firing failed with HandlerNotFoundError, every time.
func TestRemoteHandler_TheWorkerCompletesTheRun(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, remoteJob(time.Minute))

	done := fire(engine, job)
	run := openRun(t, engine, job.ID)
	if run.Handler != remoteRenew || string(run.Payload) != `{"node":"node-a"}` || run.TenantID != "tenant-a" {
		t.Fatalf("the worker was handed %+v", run)
	}
	if err := engine.CompleteRemoteRun(context.Background(), run.RemoteRunRef, &JobResult{Success: true, Message: "renewed"}, nil); err != nil {
		t.Fatalf("CompleteRemoteRun: %v", err)
	}
	waitDone(t, done)

	stored := storedJob(t, storage, job.ID)
	if stored.Status != JobStatusPending || stored.RunCount != 1 || stored.FailCount != 0 {
		t.Fatalf("STORED job is %s, %d runs, %d failures: want pending, 1 run, none failed",
			stored.Status, stored.RunCount, stored.FailCount)
	}
	if !stored.NextRun.After(job.NextRun) {
		t.Fatalf("the recurring job did not move on to its next firing: %s", stored.NextRun)
	}
	executions := history(t, storage, job.ID)
	if len(executions) != 1 || executions[0].Status != JobStatusCompleted || executions[0].Output != "renewed" {
		t.Fatalf("history is %+v, want one completed run with the worker's message", executions)
	}
}

func TestRemoteHandler_TheWorkersErrorFailsTheRun(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, remoteJob(time.Minute))

	done := fire(engine, job)
	run := openRun(t, engine, job.ID)
	if err := engine.CompleteRemoteRun(context.Background(), run.RemoteRunRef, nil, errors.New("acme order refused")); err != nil {
		t.Fatalf("CompleteRemoteRun: %v", err)
	}
	waitDone(t, done)

	stored := storedJob(t, storage, job.ID)
	if stored.FailCount != 1 || stored.LastError != "acme order refused" {
		t.Fatalf("STORED job has %d failures, last error %q", stored.FailCount, stored.LastError)
	}
}

// TestRemoteHandler_AnUnreportedRunTimesOut: a worker that never reports
// costs the run its timeout, as a hung local handler does, and its late
// result is refused rather than recorded over the timeout.
func TestRemoteHandler_AnUnreportedRunTimesOut(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, remoteJob(50*time.Millisecond))

	done := fire(engine, job)
	run := openRun(t, engine, job.ID)
	waitDone(t, done)

	if stored := storedJob(t, storage, job.ID); stored.FailCount != 1 {
		t.Fatalf("STORED job has %d failures after a timed-out run, want 1", stored.FailCount)
	}
	err := engine.CompleteRemoteRun(context.Background(), run.RemoteRunRef, &JobResult{Success: true}, nil)
	if !errors.Is(err, ErrRemoteRunNotOpen) {
		t.Fatalf("a late result: err = %v, want ErrRemoteRunNotOpen", err)
	}
	if executions := history(t, storage, job.ID); len(executions) != 1 {
		t.Fatalf("history holds %d records for one run", len(executions))
	}
}

// TestRemoteHandler_AResultForAnotherRunIsRefused: the run reference names a
// run, not a job, so a result for any other run of the job (or another
// tenant's) never lands on the open one.
func TestRemoteHandler_AResultForAnotherRunIsRefused(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := seedRunningJob(t, storage, remoteJob(time.Minute))

	done := fire(engine, job)
	run := openRun(t, engine, job.ID)

	earlier := run.RemoteRunRef
	earlier.StartedAt = earlier.StartedAt.Add(-time.Hour)
	otherTenant := run.RemoteRunRef
	otherTenant.TenantID = "tenant-b"
	for name, ref := range map[string]RemoteRunRef{"an earlier run": earlier, "another tenant": otherTenant} {
		err := engine.CompleteRemoteRun(context.Background(), ref, &JobResult{Success: true}, nil)
		if !errors.Is(err, ErrRemoteRunNotOpen) {
			t.Fatalf("%s: err = %v, want ErrRemoteRunNotOpen", name, err)
		}
	}
	if err := engine.CompleteRemoteRun(context.Background(), run.RemoteRunRef, &JobResult{Success: true}, nil); err != nil {
		t.Fatalf("the open run, after the refusals: %v", err)
	}
	waitDone(t, done)
	if stored := storedJob(t, storage, job.ID); stored.RunCount != 1 {
		t.Fatalf("STORED run count is %d, want 1", stored.RunCount)
	}
}

func TestListRemoteRuns_ListsOnlyTheTenantsOpenRunsOfRemoteHandlers(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	running := remoteJob(time.Minute)
	running.Status = JobStatusRunning
	seedRunningJob(t, storage, running)
	pending := remoteJob(time.Minute)
	seedRunningJob(t, storage, pending)
	foreign := remoteJob(time.Minute)
	foreign.Status = JobStatusRunning
	foreign.OwnerID = "tenant-b"
	seedRunningJob(t, storage, foreign)

	runs, err := engine.ListRemoteRuns(context.Background(), "tenant-a", []string{remoteRenew})
	if err != nil {
		t.Fatalf("ListRemoteRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].JobID != running.ID {
		t.Fatalf("listed %d runs, want only job %d's", len(runs), running.ID)
	}
	if want := running.UpdatedAt.Add(time.Minute); !runs[0].Deadline.Equal(want) {
		t.Fatalf("deadline %s, want %s", runs[0].Deadline, want)
	}

	if _, err := engine.ListRemoteRuns(context.Background(), "tenant-a", []string{"sdlc.reconcile"}); !errors.Is(err, ErrNotRemoteHandler) {
		t.Fatalf("a local handler name: err = %v, want ErrNotRemoteHandler", err)
	}
}

// TestRemoteHandler_AStandaloneEngineRefusesARunItIsNotWaitingOn: with no
// run guard there is no other node that could have fired the run, so a
// result for a run no execution here waits on is refused, not written.
func TestRemoteHandler_AStandaloneEngineRefusesARunItIsNotWaitingOn(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := remoteJob(time.Minute)
	job.Status = JobStatusRunning
	seedRunningJob(t, storage, job)

	err := engine.CompleteRemoteRun(context.Background(), runRefOf(job), &JobResult{Success: true}, nil)
	if !errors.Is(err, ErrRemoteRunNotOpen) {
		t.Fatalf("err = %v, want ErrRemoteRunNotOpen", err)
	}
	if stored := storedJob(t, storage, job.ID); stored.Status != JobStatusRunning || stored.RunCount != 0 {
		t.Fatalf("a refused result wrote: STORED %s with %d runs", stored.Status, stored.RunCount)
	}
}

// TestRemoteHandler_ClusteredCompletionOnAnotherNode: the worker reports to a
// node other than the one that fired the run. That node concludes it through
// the run guard and records it; the firing node's wait then times out, loses
// the guard and writes nothing — one run, one outcome.
func TestRemoteHandler_ClusteredCompletionOnAnotherNode(t *testing.T) {
	storage := newConcludingStorage()
	firing := clusteredNode(t, storage, "node-1")
	other := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, remoteJob(200*time.Millisecond))

	done := fire(firing, job)
	run := openRun(t, other, job.ID)
	if err := other.CompleteRemoteRun(context.Background(), run.RemoteRunRef, &JobResult{Success: true}, nil); err != nil {
		t.Fatalf("CompleteRemoteRun on another node: %v", err)
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.RunCount != 1 || stored.Status != JobStatusPending {
		t.Fatalf("STORED job is %s with %d runs after the completion, want pending with 1", stored.Status, stored.RunCount)
	}
	waitDone(t, done)

	stored := storedJob(t, storage.mockJobStorage, job.ID)
	if stored.RunCount != 1 || stored.FailCount != 0 {
		t.Fatalf("the firing node's timeout overwrote the completion: %d runs, %d failures", stored.RunCount, stored.FailCount)
	}
	if executions := history(t, storage.mockJobStorage, job.ID); len(executions) != 1 {
		t.Fatalf("history holds %d records for one run, want 1", len(executions))
	}
	if again := other.CompleteRemoteRun(context.Background(), run.RemoteRunRef, &JobResult{Success: true}, nil); !errors.Is(again, ErrRemoteRunNotOpen) {
		t.Fatalf("a second result for the concluded run: err = %v, want ErrRemoteRunNotOpen", again)
	}
}
