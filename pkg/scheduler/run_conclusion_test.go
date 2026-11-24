package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// concludingStorage is the mock store with the guarded run transition: the
// same compare-and-swap of the job record the quicbaas ClusteredStorage
// performs, in memory. Two engines over one concludingStorage are two nodes
// over one cluster.
type concludingStorage struct {
	*mockJobStorage

	// guard serializes conclusions, the way the store's swap does.
	guard sync.Mutex
	calls int

	// beforeConclude, when set, runs as a conclusion arrives at the store:
	// whatever it does happened between the caller's read and the swap.
	beforeConclude func()

	// interleave, when set, runs once, between a mutation's read and its
	// write: whatever it does happened while the writer held a stale read.
	hookMu     sync.Mutex
	interleave func()
}

func newConcludingStorage() *concludingStorage {
	return &concludingStorage{mockJobStorage: newMockJobStorage()}
}

// takeInterleave returns the interleave hook and clears it, so it runs once.
func (c *concludingStorage) takeInterleave() func() {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()
	hook := c.interleave
	c.interleave = nil
	return hook
}

// GetJob reads as the mock does, then runs the interleave hook, so a
// read-then-write of the job can be interleaved like a guarded mutation.
func (c *concludingStorage) GetJob(ctx context.Context, id uint64, tenantID string) (*Job, error) {
	job, err := c.mockJobStorage.GetJob(ctx, id, tenantID)
	if hook := c.takeInterleave(); hook != nil {
		hook()
	}
	return job, err
}

// MutateJob is the guarded job write the quicbaas ClusteredStorage
// performs, in memory: the result is stored only while the job record is
// the one the mutation decided against, and re-decided otherwise.
func (c *concludingStorage) MutateJob(ctx context.Context, jobID uint64, _ string, mutate JobMutation) (*Job, error) {
	for attempt := 0; attempt < 8; attempt++ {
		if c.honorCancel.Load() && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		c.errMu.RLock()
		getErr, writeErr := c.getJobErr, c.updateJobErr
		c.errMu.RUnlock()
		if getErr != nil {
			return nil, getErr
		}
		if writeErr != nil {
			return nil, writeErr
		}
		c.mu.RLock()
		stored, ok := c.jobs[jobID]
		c.mu.RUnlock()
		if !ok {
			return nil, nil
		}
		next, err := mutate(copyJob(stored))
		if err != nil {
			return nil, err
		}
		if hook := c.takeInterleave(); hook != nil {
			hook()
		}
		c.guard.Lock()
		c.mu.Lock()
		if c.jobs[jobID] != stored {
			c.mu.Unlock()
			c.guard.Unlock()
			continue
		}
		written := copyJob(next)
		written.UpdatedAt = time.Now()
		c.jobs[jobID] = written
		c.mu.Unlock()
		c.guard.Unlock()
		if c.afterUpdateJob != nil {
			c.afterUpdateJob(copyJob(written))
		}
		return copyJob(written), nil
	}
	return nil, errors.New("mock job mutation contended")
}

func (c *concludingStorage) ConcludeRun(_ context.Context, run *RunConclusion, transition RunTransition) (bool, error) {
	if hook := c.beforeConclude; hook != nil {
		hook()
	}
	c.guard.Lock()
	defer c.guard.Unlock()
	c.calls++
	c.errMu.RLock()
	writeErr := c.updateJobErr
	c.errMu.RUnlock()
	if writeErr != nil {
		return false, writeErr
	}
	c.mu.RLock()
	stored, ok := c.jobs[run.JobID]
	var held *Job
	if ok {
		held = copyJob(stored)
	}
	c.mu.RUnlock()
	if !ok {
		return false, nil
	}
	next, take := transition(held)
	if !take {
		return false, nil
	}
	written := copyJob(next)
	written.UpdatedAt = time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.jobs[run.JobID]; !ok {
		return false, nil
	}
	c.jobs[run.JobID] = written
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

// outcomeOf is the outcome a test spells as a whole record: applied to the
// record current at the write, it sets want's outcome fields.
func outcomeOf(want *Job) runOutcome {
	return func(j *Job) {
		j.Status, j.LastRun, j.RunCount, j.FailCount = want.Status, want.LastRun, want.RunCount, want.FailCount
		j.LastError, j.NextRun, j.UpdatedAt = want.LastError, want.NextRun, want.UpdatedAt
	}
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
	executor.persistJobOutcome(runCtx, late, outcomeOf(late))

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
// order: the executing node concluded its run, so a recovery that still read
// the record as Running is refused and writes nothing.
func TestRecoverAbandonedRun_RefusedWhenTheExecutorConcludedFirst(t *testing.T) {
	storage := newConcludingStorage()
	executor := clusteredNode(t, storage, "node-1")
	recoverer := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())
	runCtx := executor.noteRunStarted(context.Background(), job)
	done := copyJob(job)
	done.Status = JobStatusPending
	if guarded, won := executor.concludeOwnRun(runCtx, done, outcomeOf(done)); !guarded || !won {
		t.Fatalf("the executor's own conclusion: guarded=%v won=%v, want both", guarded, won)
	}

	_, err := recoverer.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	var refused RecoveryRefusedError
	if !errors.As(err, &refused) || refused.Refusal != RefusedConcluded {
		t.Fatalf("RecoverAbandonedRun = %v, want a concluded_elsewhere refusal", err)
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusPending || stored.FailCount != 0 {
		t.Fatalf("STORED %s with %d failures, want the executor's pending with 0", stored.Status, stored.FailCount)
	}
	executions, _ := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if len(executions) != 0 {
		t.Fatalf("a refused recovery wrote %d history records", len(executions))
	}
}

// TestRecoverAbandonedRun_AFailedTransitionIsRetried: a recovery whose
// guarded write failed concluded nothing, so the job stays Running and its
// retry concludes the run once.
func TestRecoverAbandonedRun_AFailedTransitionIsRetried(t *testing.T) {
	storage := newConcludingStorage()
	engine := clusteredNode(t, storage, "node-1")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())

	storage.updateJobErr = errors.New("job store unreachable")
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err == nil {
		t.Fatal("the transition failure was not reported")
	}
	storage.updateJobErr = nil
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusRunning {
		t.Fatalf("a failed transition left the job %s, want running", stored.Status)
	}
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	executions, _ := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if len(executions) != 1 {
		t.Fatalf("history holds %d records, want 1", len(executions))
	}
}

// TestRecoverAbandonedRun_SameNodeRetryFinishesTheHistory: the transition
// was written, then the history record failed. The job names this node's
// recovery, so a retry writes the record once and spends no second failure;
// another node is refused.
func TestRecoverAbandonedRun_SameNodeRetryFinishesTheHistory(t *testing.T) {
	storage := newConcludingStorage()
	engine := clusteredNode(t, storage, "node-1")
	other := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())

	storage.saveExecutionErr = errors.New("history unreachable")
	if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err == nil {
		t.Fatal("the history failure was not reported")
	}
	storage.saveExecutionErr = nil
	var refused RecoveryRefusedError
	if _, err := other.RecoverAbandonedRun(context.Background(), job, goodAttestation()); !errors.As(err, &refused) || refused.Refusal != RefusedConcluded {
		t.Fatalf("another node's recovery = %v, want a concluded refusal", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
			t.Fatalf("retry %d on the concluding node: %v", i, err)
		}
	}
	executions, _ := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if len(executions) != 1 {
		t.Fatalf("history holds %d records, want 1", len(executions))
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.FailCount != 1 {
		t.Fatalf("STORED fail count %d, want 1", stored.FailCount)
	}
}

// TestRecoverAbandonedRun_DoesNotRecreateAForceDeletedJob: the job is
// deleted by another node after this recovery read it Running. The guarded
// transition finds no job and writes nothing.
func TestRecoverAbandonedRun_DoesNotRecreateAForceDeletedJob(t *testing.T) {
	storage := newConcludingStorage()
	engine := clusteredNode(t, storage, "node-1")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())
	storage.beforeConclude = func() {
		if err := storage.DeleteJob(context.Background(), job.ID, ""); err != nil {
			t.Errorf("DeleteJob: %v", err)
		}
	}

	_, err := engine.RecoverAbandonedRun(context.Background(), job, goodAttestation())
	var refused RecoveryRefusedError
	if !errors.As(err, &refused) || refused.Refusal != RefusedConcluded {
		t.Fatalf("RecoverAbandonedRun = %v, want a concluded refusal", err)
	}
	if got, _ := storage.GetJob(context.Background(), job.ID, ""); got != nil {
		t.Fatalf("the deleted job was recreated as %s", got.Status)
	}
	executions, _ := storage.GetExecutions(context.Background(), job.ID, "", 10)
	if len(executions) != 0 {
		t.Fatalf("a recovery of a deleted job wrote %d history records", len(executions))
	}
}

// TestClusteredOutcome_DoesNotRecreateADeletedJob is the same race on the
// executing node's outcome.
func TestClusteredOutcome_DoesNotRecreateADeletedJob(t *testing.T) {
	storage := newConcludingStorage()
	executor := clusteredNode(t, storage, "node-1")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())
	runCtx := executor.noteRunStarted(context.Background(), job)
	if err := storage.DeleteJob(context.Background(), job.ID, ""); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	done := copyJob(job)
	done.Status = JobStatusPending
	if executor.persistJobOutcome(runCtx, done, outcomeOf(done)) {
		t.Fatal("the outcome of a deleted job won")
	}
	if got, _ := storage.GetJob(context.Background(), job.ID, ""); got != nil {
		t.Fatalf("the deleted job was recreated as %s", got.Status)
	}
}

// TestClusteredOutcome_AFailedWriteLeavesTheRunRecoverable is the winner
// that fails before its write: no conclusion is recorded without the
// outcome, so the job stays Running and another node's recovery concludes
// it.
func TestClusteredOutcome_AFailedWriteLeavesTheRunRecoverable(t *testing.T) {
	storage := newConcludingStorage()
	executor := clusteredNode(t, storage, "node-1")
	recoverer := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())
	runCtx := executor.noteRunStarted(context.Background(), job)

	storage.updateJobErr = errors.New("job store unreachable")
	done := copyJob(job)
	done.Status = JobStatusPending
	if executor.persistJobOutcome(runCtx, done, outcomeOf(done)) {
		t.Fatal("an outcome whose write failed won")
	}
	storage.updateJobErr = nil
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusRunning || runConcluded(stored) {
		t.Fatalf("STORED %s (concluded=%v), want an open running run", stored.Status, runConcluded(stored))
	}
	if _, err := recoverer.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
		t.Fatalf("recovery after the executor's failed write: %v", err)
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusPending || stored.FailCount != 1 {
		t.Fatalf("STORED %s with %d failures, want pending with 1", stored.Status, stored.FailCount)
	}
}

// TestClusteredOutcome_AnEarlyTriggersRetryConcludes: a job due an hour
// ahead is triggered now and fails, and its retry is due at now plus the
// backoff, BEFORE the firing the first run carried. The retry is a newer
// run all the same, and its outcome concludes it.
func TestClusteredOutcome_AnEarlyTriggersRetryConcludes(t *testing.T) {
	storage := newConcludingStorage()
	executor := clusteredNode(t, storage, "node-1")
	now := time.Now().Truncate(time.Second)
	first := runningRecoveryJob()
	first.NextRun = now.Add(time.Hour)
	first.RunStartedAt = now
	job := seedRunningJob(t, storage.mockJobStorage, first)

	runCtx := executor.noteRunStarted(context.Background(), job)
	failed := copyJob(job)
	failed.Status = JobStatusPending
	failed.FailCount = 1
	failed.NextRun = now.Add(time.Minute)
	if !executor.persistJobOutcome(runCtx, failed, outcomeOf(failed)) {
		t.Fatal("the triggered run's outcome lost")
	}

	retry := storedJob(t, storage.mockJobStorage, job.ID)
	retry.Status = JobStatusRunning
	retry.RunStartedAt = now.Add(time.Minute)
	if err := storage.UpdateJob(context.Background(), retry); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	retryCtx := executor.noteRunStarted(context.Background(), retry)
	done := copyJob(retry)
	done.Status = JobStatusPending
	done.FailCount = 0
	if !executor.persistJobOutcome(retryCtx, done, outcomeOf(done)) {
		t.Fatal("the retry's outcome lost to the earlier run's later firing")
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusPending || stored.FailCount != 0 {
		t.Fatalf("STORED %s with %d failures, want the retry's pending with 0", stored.Status, stored.FailCount)
	}
}

// TestClusteredUpdate_AStaleUpdateKeepsARecoverysConclusion: a rename that
// read the job Running before another node recovered the run cannot write
// its stale snapshot over the recovery's conclusion.
func TestClusteredUpdate_AStaleUpdateKeepsARecoverysConclusion(t *testing.T) {
	storage := newConcludingStorage()
	updater := clusteredNode(t, storage, "node-1")
	updater.running.Store(true)
	recoverer := clusteredNode(t, storage, "node-2")
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())

	recovered := false
	storage.interleave = func() {
		if _, err := recoverer.RecoverAbandonedRun(context.Background(), job, goodAttestation()); err != nil {
			t.Errorf("recovery between the rename's read and its write: %v", err)
			return
		}
		recovered = true
	}

	if _, err := updater.UpdateJob(context.Background(), job.ID, "", &JobUpdateParams{Name: "renamed"}); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if !recovered {
		t.Fatal("the recovery did not run between the rename's read and its write")
	}
	stored := storedJob(t, storage.mockJobStorage, job.ID)
	if stored.Status != JobStatusPending || stored.ConcludedBy != "recovery@node-2" || !runConcluded(stored) {
		t.Fatalf("STORED job is %s concluded by %q (concluded=%v), want the recovery's pending conclusion",
			stored.Status, stored.ConcludedBy, runConcluded(stored))
	}
	if stored.FailCount != 1 || stored.Name != "renamed" {
		t.Fatalf("STORED job has %d failures and name %q, want 1 and \"renamed\"", stored.FailCount, stored.Name)
	}
}

// TestRecoverAbandonedRun_AFailedHistoryWriteSurvivesTheNextRun: a recovery
// whose history write failed keeps the record on the job; a new run of the
// job does not lose it, and a later attempt writes it once.
func TestRecoverAbandonedRun_AFailedHistoryWriteSurvivesTheNextRun(t *testing.T) {
	ctx := context.Background()
	storage := newConcludingStorage()
	recoverer := clusteredNode(t, storage, "node-1")
	executor := clusteredNode(t, storage, "node-2")
	if err := executor.RegisterHandler("sdlc.reconcile", func(context.Context, *Job) (*JobResult, error) {
		return &JobResult{Message: "next run"}, nil
	}); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	job := seedRunningJob(t, storage.mockJobStorage, runningRecoveryJob())

	storage.errMu.Lock()
	storage.saveExecutionErr = errors.New("raft: no quorum")
	storage.errMu.Unlock()
	if _, err := recoverer.RecoverAbandonedRun(ctx, job, goodAttestation()); err == nil {
		t.Fatal("RecoverAbandonedRun with a failing history write reported success")
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.Status != JobStatusPending || len(stored.PendingHistory) != 1 {
		t.Fatalf("STORED job is %s with %d pending records, want pending with 1", stored.Status, len(stored.PendingHistory))
	}

	// The next run starts and concludes before the recovery is retried,
	// with the history still failing.
	executor.executeJob(context.Background(), storedJob(t, storage.mockJobStorage, job.ID), time.Time{})
	if stored := storedJob(t, storage.mockJobStorage, job.ID); stored.ConcludedBy != "execution@node-2" || len(stored.PendingHistory) != 1 {
		t.Fatalf("STORED job concluded by %q with %d pending records, want the next run's conclusion and 1",
			stored.ConcludedBy, len(stored.PendingHistory))
	}

	storage.errMu.Lock()
	storage.saveExecutionErr = nil
	storage.errMu.Unlock()
	_, err := recoverer.RecoverAbandonedRun(ctx, job, goodAttestation())
	var refused RecoveryRefusedError
	if !errors.As(err, &refused) || refused.Refusal != RefusedConcluded {
		t.Fatalf("the retried recovery after the next run: err = %v, want refused as concluded", err)
	}

	executions, err := storage.GetExecutions(ctx, job.ID, "", 10)
	if err != nil {
		t.Fatalf("GetExecutions: %v", err)
	}
	evidence := renderAttestation(goodAttestation())
	found := 0
	for _, exec := range executions {
		if exec.Status == JobStatusFailed && exec.StartedAt.Equal(recoveryFiring) && exec.Error == evidence {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("history holds %d records of the abandoned run, want 1: %+v", found, executions)
	}
	if stored := storedJob(t, storage.mockJobStorage, job.ID); len(stored.PendingHistory) != 0 {
		t.Fatalf("STORED job still holds %d pending records", len(stored.PendingHistory))
	}
}
