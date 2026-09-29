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

package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A run cut short by the engine's own shutdown is not a failed run. These
// tests drive executeJob — synchronous, so the outcome is in the store when it
// returns — with an engine context the test cancels, against a store that
// refuses a done context the way a Raft-backed one does.

// interruptWait bounds every wait in these tests. Reaching it is a failure,
// never a pass.
const interruptWait = 10 * time.Second

// interruptedJob stores a due job with a partly spent retry budget and some
// history, so a test can see that the interruption touched none of it.
func interruptedJob(t *testing.T, storage *mockJobStorage, jobType JobType, firing time.Time) *Job {
	t.Helper()
	job := &Job{
		ID:         1,
		Name:       "interrupted",
		Type:       jobType,
		Handler:    "work",
		MaxRetries: 3,
		Status:     JobStatusPending,
		ExecuteAt:  firing,
		NextRun:    firing,
		LastRun:    firing.Add(-time.Hour),
		RunCount:   4,
		FailCount:  1,
	}
	if jobType == JobTypeRecurring {
		job.Schedule = "@every 1h"
	}
	storage.mu.Lock()
	storage.jobs[job.ID] = copyJob(job)
	storage.mu.Unlock()
	return copyJob(job)
}

// blockingHandler signals started once its run begins, then waits: for its
// context when it honours cancellation, or for release when it does not.
func blockingHandler(started chan<- struct{}, release <-chan struct{}, honours bool) JobHandler {
	var once sync.Once
	return func(ctx context.Context, _ *Job) (*JobResult, error) {
		once.Do(func() { close(started) })
		if honours {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		<-release
		return &JobResult{Success: true}, nil
	}
}

// runAndInterrupt executes job on engine and cancels the engine context once
// the handler is running, as Stop does.
func runAndInterrupt(t *testing.T, engine *SchedulerEngine, job *Job, claimedFiring time.Time, started <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-started:
			cancel()
		case <-time.After(interruptWait):
		}
	}()
	engine.executeJob(ctx, job, claimedFiring)
}

// TestInterruptedRun_IsNotAFailure is the defect: an interrupted run was
// recorded through handleJobFailure, which charged a retry and marked the job
// Failed — permanent for a user recurring job, since only system jobs are
// re-armed at boot. The job must come back Pending with its budget and history
// untouched, and due again past the firing that was claimed.
func TestInterruptedRun_IsNotAFailure(t *testing.T) {
	for _, jobType := range []JobType{JobTypeRecurring, JobTypeOneShot} {
		for _, honours := range []bool{true, false} {
			name := string(jobType) + "/handler ignores cancellation"
			if honours {
				name = string(jobType) + "/handler honours cancellation"
			}
			t.Run(name, func(t *testing.T) {
				storage := newMockJobStorage()
				storage.honorCancel.Store(true)
				engine := New(DefaultConfig(), storage)

				started := make(chan struct{})
				release := make(chan struct{})
				t.Cleanup(func() { close(release) })
				require.NoError(t, engine.RegisterHandler("work", blockingHandler(started, release, honours)))

				firing := time.Now().Add(-time.Minute)
				job := interruptedJob(t, storage, jobType, firing)

				runAndInterrupt(t, engine, job, firing, started)
				interruptedAt := time.Now()

				stored := storedJob(t, storage, job.ID)
				assert.Equal(t, JobStatusPending, stored.Status, "an interrupted run is not a failure")
				assert.Equal(t, uint64(1), stored.FailCount, "an interrupted run is not charged a retry")
				assert.Equal(t, uint64(4), stored.RunCount, "an interrupted run did not run")
				assert.True(t, stored.LastRun.Equal(job.LastRun), "an interrupted run did not run")
				assert.True(t, stored.NextRun.After(firing),
					"the firing must be re-armed past the claimed one (%s), got %s", firing, stored.NextRun)
				assert.False(t, stored.NextRun.After(interruptedAt),
					"the interrupted firing must be due again at once, got %s", stored.NextRun)
				assert.Zero(t, engine.Stats().FailureCount)

				executions, err := storage.GetExecutions(context.Background(), job.ID, "", 10)
				require.NoError(t, err)
				require.Len(t, executions, 1, "the partial run stays in the history")
				assert.Equal(t, JobStatusCanceled, executions[0].Status)
				assert.Equal(t, RunInterruptedError{JobID: job.ID}.Error(), executions[0].Error)
			})
		}
	}
}

// TestInterruptedRun_AFutureFiringKeepsItsTime covers a manual trigger cut
// short: the job's scheduled firing had not come yet, so it is kept rather
// than pulled forward.
func TestInterruptedRun_AFutureFiringKeepsItsTime(t *testing.T) {
	storage := newMockJobStorage()
	storage.honorCancel.Store(true)
	engine := New(DefaultConfig(), storage)

	started := make(chan struct{})
	require.NoError(t, engine.RegisterHandler("work", blockingHandler(started, nil, true)))

	firing := time.Now().Add(time.Hour)
	job := interruptedJob(t, storage, JobTypeRecurring, firing)

	runAndInterrupt(t, engine, job, time.Time{}, started)

	stored := storedJob(t, storage, job.ID)
	assert.Equal(t, JobStatusPending, stored.Status)
	assert.True(t, stored.NextRun.Equal(firing), "a firing still to come keeps its time")
}

// TestInterruptedRun_AHandlerErrorWhileRunningIsStillCharged pins the other
// side: the shutdown check must not swallow real failures. A handler that
// fails while the engine is running spends a retry as before.
func TestInterruptedRun_AHandlerErrorWhileRunningIsStillCharged(t *testing.T) {
	storage := newMockJobStorage()
	storage.honorCancel.Store(true)
	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterHandler("work", func(context.Context, *Job) (*JobResult, error) {
		return nil, errHandlerRun
	}))

	firing := time.Now().Add(-time.Minute)
	job := interruptedJob(t, storage, JobTypeRecurring, firing)
	engine.executeJob(context.Background(), job, firing)

	stored := storedJob(t, storage, job.ID)
	assert.Equal(t, uint64(2), stored.FailCount, "a real failure is charged")
	assert.Equal(t, errHandlerRun.Error(), stored.LastError)
	assert.Equal(t, uint64(1), engine.Stats().FailureCount)
}

// TestInterruptedRun_AFailedWriteIsAStorageErrorNotAFailure covers the error
// path: when the re-arm cannot be written, the failure is counted as a storage
// error, and the run is still not counted as a job failure.
func TestInterruptedRun_AFailedWriteIsAStorageErrorNotAFailure(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)

	started := make(chan struct{})
	var once sync.Once
	require.NoError(t, engine.RegisterHandler("work", func(ctx context.Context, _ *Job) (*JobResult, error) {
		once.Do(func() {
			// The running transition has been written; every write from
			// here on fails.
			storage.errMu.Lock()
			storage.updateJobErr = errors.New("no leader")
			storage.saveExecutionErr = errors.New("no leader")
			storage.errMu.Unlock()
			close(started)
		})
		<-ctx.Done()
		return nil, ctx.Err()
	}))

	firing := time.Now().Add(-time.Minute)
	job := interruptedJob(t, storage, JobTypeRecurring, firing)
	before := engine.StorageErrorCount()

	runAndInterrupt(t, engine, job, firing, started)

	assert.Greater(t, engine.StorageErrorCount(), before, "a lost outcome write is visible")
	assert.Zero(t, engine.Stats().FailureCount, "an interrupted run is never a job failure")
}

// TestInterruptedRun_AnotherNodeRunsTheInterruptedFiring is the cluster path,
// end to end through Start and Stop: the firing node A claimed is cut short by
// A's shutdown, and node B must be able to claim and run it. Re-arming at the
// original instant would leave the firing at its claim — held by A — and it
// would never run anywhere.
func TestInterruptedRun_AnotherNodeRunsTheInterruptedFiring(t *testing.T) {
	storage := newMockJobStorage()
	storage.honorCancel.Store(true)
	cfg := DefaultConfig()
	cfg.TickInterval = 5 * time.Millisecond

	firing := time.Now().Add(-time.Minute)
	job := interruptedJob(t, storage, JobTypeOneShot, firing)

	nodeA, err := NewWithClustering(cfg, storage, "node-a", nil)
	require.NoError(t, err)
	started := make(chan struct{})
	require.NoError(t, nodeA.RegisterHandler("work", blockingHandler(started, nil, true)))
	require.NoError(t, nodeA.Start(context.Background()))

	select {
	case <-started:
	case <-time.After(interruptWait):
		t.Fatal("node A never ran the job")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), interruptWait)
	defer cancel()
	require.NoError(t, nodeA.Stop(stopCtx))
	require.Equal(t, JobStatusPending, storedJob(t, storage, job.ID).Status)

	nodeB, err := NewWithClustering(cfg, storage, "node-b", nil)
	require.NoError(t, err)
	ran := make(chan struct{})
	var once sync.Once
	require.NoError(t, nodeB.RegisterHandler("work", func(context.Context, *Job) (*JobResult, error) {
		once.Do(func() { close(ran) })
		return &JobResult{Success: true}, nil
	}))
	require.NoError(t, nodeB.Start(context.Background()))

	select {
	case <-ran:
	case <-time.After(interruptWait):
		t.Fatal("the interrupted firing never ran on node B")
	}
	require.NoError(t, nodeB.Stop(stopCtx))
	assert.Equal(t, JobStatusCompleted, storedJob(t, storage, job.ID).Status)
}

// TestOutcomeWrites_SurviveTheEngineStopping pins outcomeContext: a run that
// finished just as the engine stopped still has its outcome written, even
// against a store that refuses a done context. Derived from the engine's
// context, the write was refused and the job stayed Running — a status no
// tick picks up again.
func TestOutcomeWrites_SurviveTheEngineStopping(t *testing.T) {
	outcomes := map[string]struct {
		record     func(e *SchedulerEngine, ctx context.Context, job *Job)
		wantStatus JobStatus
	}{
		"success": {
			record: func(e *SchedulerEngine, ctx context.Context, job *Job) {
				e.handleJobSuccess(ctx, job, &JobExecution{JobID: job.ID, StartedAt: time.Now()}, &JobResult{Success: true})
			},
			wantStatus: JobStatusCompleted,
		},
		"handler error": {
			record: func(e *SchedulerEngine, ctx context.Context, job *Job) {
				e.handleJobError(ctx, job, &JobExecution{JobID: job.ID, StartedAt: time.Now()}, errHandlerRun)
			},
			wantStatus: JobStatusPending,
		},
		"handler missing": {
			record: func(e *SchedulerEngine, ctx context.Context, job *Job) {
				e.handleJobFailure(ctx, job, HandlerNotFoundError{Handler: job.Handler})
			},
			wantStatus: JobStatusFailed,
		},
	}
	for name, tc := range outcomes {
		t.Run(name, func(t *testing.T) {
			storage := newMockJobStorage()
			storage.honorCancel.Store(true)
			engine := New(DefaultConfig(), storage)
			job := interruptedJob(t, storage, JobTypeOneShot, time.Now().Add(-time.Minute))
			job.Status = JobStatusRunning

			stopped, cancel := context.WithCancel(context.Background())
			cancel()
			tc.record(engine, stopped, job)

			assert.Equal(t, tc.wantStatus, storedJob(t, storage, job.ID).Status)
			assert.Zero(t, engine.StorageErrorCount(), "the outcome write must not be refused")
		})
	}
}
