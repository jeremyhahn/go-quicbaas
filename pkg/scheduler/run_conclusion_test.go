package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// concludingStorage is the mock store with the guarded run transition: the
// same forward-only mark the quicbaas ClusteredStorage keeps, in memory. Two
// engines over one concludingStorage are two nodes over one cluster.
type concludingStorage struct {
	*mockJobStorage

	mu    sync.Mutex
	marks map[uint64]RunConclusion
	calls int
}

func newConcludingStorage() *concludingStorage {
	return &concludingStorage{mockJobStorage: newMockJobStorage(), marks: map[uint64]RunConclusion{}}
}

func runBefore(a, b RunConclusion) bool {
	if !a.Firing.Equal(b.Firing) {
		return a.Firing.Before(b.Firing)
	}
	return a.StartedAt.Before(b.StartedAt)
}

func (c *concludingStorage) ConcludeRun(_ context.Context, run *RunConclusion) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	held, ok := c.marks[run.JobID]
	switch {
	case !ok, runBefore(held, *run):
	case runBefore(*run, held), held.Owner != run.Owner:
		return false, nil
	}
	c.marks[run.JobID] = *run
	return true, nil
}

func clusteredNode(t *testing.T, storage *concludingStorage, nodeID string) *SchedulerEngine {
	t.Helper()
	engine, err := NewWithClustering(DefaultConfig(), storage, nodeID, nil)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	return engine
}

// TestRecoverAbandonedRun_ClusteredEngineRecoversThroughTheGuard is the
// lifted gate: a clustered engine whose store provides the guarded
// transition concludes an abandoned run, and does so through the guard.
//
// Before #105 the clustered engine refused every recovery by name.
func TestRecoverAbandonedRun_ClusteredEngineRecoversThroughTheGuard(t *testing.T) {
	storage := newConcludingStorage()
	engine := clusteredNode(t, storage, "node-1")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())

	status, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	if err != nil {
		t.Fatalf("RecoverAbandonedRun on a guarded clustered engine: %v", err)
	}
	if status != JobStatusPending {
		t.Fatalf("returned status is %s, want pending", status)
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusPending || stored.FailCount != 1 {
		t.Fatalf("STORED job is %s with %d failures, want pending with 1", stored.Status, stored.FailCount)
	}
	if storage.calls != 1 {
		t.Fatalf("the guard was consulted %d times, want once: a clustered recovery that skips it races", storage.calls)
	}
}

// TestRecoverAbandonedRun_RacingRecoverersLeaveOneWinner is two nodes
// recovering the same abandoned run at once. Each holds only its own stripe
// lock, so without the guard both pass the Running check and both conclude
// the run: two history records, two failures spent. With it exactly one
// wins, and the other is refused having written nothing.
//
// This is a stress check, not the proof: the window is narrow and an
// unguarded engine often passes it by luck. The deterministic proofs are the
// two ordered tests below, which fail without the guard every time.
func TestRecoverAbandonedRun_RacingRecoverersLeaveOneWinner(t *testing.T) {
	for i := 0; i < 50; i++ {
		storage := newConcludingStorage()
		nodes := []*SchedulerEngine{
			clusteredNode(t, storage, "node-1"),
			clusteredNode(t, storage, "node-2"),
		}
		job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())

		start := make(chan struct{})
		errs := make([]error, len(nodes))
		var wg sync.WaitGroup
		for n, engine := range nodes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, errs[n] = engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
			}()
		}
		close(start)
		wg.Wait()

		winners := 0
		for _, err := range errs {
			var refused RecoveryRefusedError
			switch {
			case err == nil:
				winners++
			case !errors.As(err, &refused):
				t.Fatalf("a losing recoverer failed with %v, want a typed refusal", err)
			}
		}
		if winners != 1 {
			t.Fatalf("%d recoverers concluded one run, want exactly 1", winners)
		}
		executions, err := storage.GetExecutions(context.Background(), job.ID, "", 10)
		if err != nil {
			t.Fatalf("GetExecutions: %v", err)
		}
		if len(executions) != 1 {
			t.Fatalf("history holds %d records for one abandoned run, want 1", len(executions))
		}
		if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.FailCount != 1 {
			t.Fatalf("STORED fail count is %d, want 1: the run was concluded twice", stored.FailCount)
		}
	}
}

// TestClusteredOutcome_LosesToARecoveryThatConcludedTheRun is the race the
// gate named: the executing node was judged gone, another node recovered the
// run, and then the executing node came back with its outcome. Unguarded,
// that outcome overwrote the recovery's record. Guarded, it is dropped.
func TestClusteredOutcome_LosesToARecoveryThatConcludedTheRun(t *testing.T) {
	storage := newConcludingStorage()
	executor := clusteredNode(t, storage, "node-1")
	recoverer := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())
	runCtx := executor.noteRunStarted(context.Background(), job)

	if _, err := recoverer.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
		t.Fatalf("RecoverAbandonedRun: %v", err)
	}
	recovered := storedJob(t, storage.mockJobStorage, job.ID)

	late := copyJob(job)
	late.Status = JobStatusCompleted
	executor.persistJobOutcome(runCtx, late)

	stored := storedJob(t, storage.mockJobStorage, job.ID)
	if stored.Status != recovered.Status || stored.FailCount != recovered.FailCount {
		t.Fatalf("the late outcome overwrote the recovery: STORED %s/%d failures, recovery left %s/%d",
			stored.Status, stored.FailCount, recovered.Status, recovered.FailCount)
	}
	if executor.ConcludedElsewhere() != 1 {
		t.Errorf("ConcludedElsewhere = %d, want 1", executor.ConcludedElsewhere())
	}
}

// TestClusteredOutcome_ALosingOutcomeWritesNoHistory: an outcome that lost
// the guard leaves the history alone too. Before, the job record was spared
// but the late run still appended its own execution record beside the
// recovery's, so one run read as two.
func TestClusteredOutcome_ALosingOutcomeWritesNoHistory(t *testing.T) {
	storage := newConcludingStorage()
	executor := clusteredNode(t, storage, "node-1")
	recoverer := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())
	runCtx := executor.noteRunStarted(context.Background(), job)

	if _, err := recoverer.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
		t.Fatalf("RecoverAbandonedRun: %v", err)
	}
	exec := &JobExecution{JobID: job.ID, Status: JobStatusRunning, StartedAt: time.Now()}
	executor.handleJobSuccess(runCtx, copyJob(job), exec, &JobResult{Success: true})

	executions, err := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if err != nil {
		t.Fatalf("GetExecutions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("history holds %d records for one run, want the recovery's 1", len(executions))
	}
}

// TestRecoverAbandonedRun_RefusedWhenTheExecutorConcludedFirst is the other
// order: the executing node won the guard for its run, so a recovery that
// still reads the record as Running is refused and writes nothing.
func TestRecoverAbandonedRun_RefusedWhenTheExecutorConcludedFirst(t *testing.T) {
	storage := newConcludingStorage()
	executor := clusteredNode(t, storage, "node-1")
	recoverer := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())
	runCtx := executor.noteRunStarted(context.Background(), job)
	if !executor.concludeOwnRun(runCtx, job) {
		t.Fatal("the executor lost the guard for its own run with no contender")
	}

	_, err := recoverer.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	var refused RecoveryRefusedError
	if !errors.As(err, &refused) || refused.Refusal != RefusedConcluded {
		t.Fatalf("RecoverAbandonedRun = %v, want a concluded_elsewhere refusal", err)
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusRunning || stored.FailCount != 0 {
		t.Fatalf("a refused recovery wrote: STORED %s with %d failures", stored.Status, stored.FailCount)
	}
	executions, _ := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if len(executions) != 0 {
		t.Fatalf("a refused recovery wrote %d history records", len(executions))
	}
}

// TestRecoverAbandonedRun_RetryOnTheSameNodeWinsAgain keeps the retry
// contract on a clustered engine: a recovery whose transition failed after it
// took the guard completes on retry rather than refusing itself.
func TestRecoverAbandonedRun_RetryOnTheSameNodeWinsAgain(t *testing.T) {
	storage := newConcludingStorage()
	engine := clusteredNode(t, storage, "node-1")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())

	storage.updateJobErr = errors.New("job store unreachable")
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err == nil {
		t.Fatal("the transition failure was not reported")
	}
	storage.updateJobErr = nil
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
		t.Fatalf("the retry on the node that holds the guard: %v", err)
	}
	executions, _ := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if len(executions) != 1 {
		t.Fatalf("history holds %d records, want 1", len(executions))
	}
}
