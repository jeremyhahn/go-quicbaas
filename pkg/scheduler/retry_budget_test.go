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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The retry budget is PER FIRING: a successful run, or the start of a new
// firing, resets FailCount. These tests drive the engine's real outcome paths
// (handleJobError / handleJobSuccess) against the in-memory store and assert
// on the durable record, which is what every later tick acts on.

// errHandlerRun is the failure every failing run in these tests returns.
var errHandlerRun = errors.New("handler run failed")

// budgetJob stores a running recurring job with the default retry budget.
func budgetJob(t *testing.T, storage *mockJobStorage) *Job {
	t.Helper()
	job := &Job{
		Name:     "recurring",
		Type:     JobTypeRecurring,
		Schedule: "@every 1h",
		Handler:  "work",
		Status:   JobStatusRunning,
	}
	require.NoError(t, storage.CreateJob(context.Background(), job))
	return job
}

// failRun records one failed run of the stored job and returns the record.
func failRun(t *testing.T, engine *SchedulerEngine, storage *mockJobStorage, id uint64) *Job {
	t.Helper()
	job := storedJob(t, storage, id)
	job.Status = JobStatusRunning
	engine.handleJobError(context.Background(), job,
		&JobExecution{JobID: id, StartedAt: time.Now()}, errHandlerRun)
	return storedJob(t, storage, id)
}

// succeedRun records one successful run of the stored job and returns the
// record.
func succeedRun(t *testing.T, engine *SchedulerEngine, storage *mockJobStorage, id uint64) *Job {
	t.Helper()
	job := storedJob(t, storage, id)
	job.Status = JobStatusRunning
	engine.handleJobSuccess(context.Background(), job,
		&JobExecution{JobID: id, StartedAt: time.Now()}, &JobResult{Success: true})
	return storedJob(t, storage, id)
}

// TestRetryBudget_RecurringJobFailsRecoversAndFailsAgain is the regression:
// under a lifetime count, two failures, a success, and two more failures
// reached DefaultRetries (3) and retired the job for good.
func TestRetryBudget_RecurringJobFailsRecoversAndFailsAgain(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := budgetJob(t, storage)

	for want := uint64(1); want <= 2; want++ {
		stored := failRun(t, engine, storage, job.ID)
		require.Equal(t, JobStatusPending, stored.Status)
		require.Equal(t, want, stored.FailCount)
	}

	stored := succeedRun(t, engine, storage, job.ID)
	require.Equal(t, JobStatusPending, stored.Status)
	assert.Zero(t, stored.FailCount, "a successful run resets the retry budget")

	for want := uint64(1); want <= 2; want++ {
		stored = failRun(t, engine, storage, job.ID)
		assert.Equal(t, JobStatusPending, stored.Status,
			"failure %d of a later firing must not retire the job", want)
		assert.Equal(t, want, stored.FailCount, "the later firing counts only its own failures")
	}
	assert.Equal(t, uint64(5), stored.RunCount, "every run is still counted")
}

// TestRetryBudget_SpentFiringMovesOnToTheNextFiring pins the other reset: a
// recurring firing whose budget is spent is abandoned, not the job. The job
// is rescheduled on its cron schedule — not on retry backoff — with a fresh
// budget, and keeps the error that spent the last one.
func TestRetryBudget_SpentFiringMovesOnToTheNextFiring(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := budgetJob(t, storage)

	var stored *Job
	for range DefaultConfig().DefaultRetries {
		stored = failRun(t, engine, storage, job.ID)
	}

	assert.Equal(t, JobStatusPending, stored.Status, "a spent firing must not retire a recurring job")
	assert.Zero(t, stored.FailCount, "the next firing starts with a fresh budget")
	assert.Equal(t, errHandlerRun.Error(), stored.LastError, "the failure that spent the firing stays visible")
	assert.Equal(t, stored.LastRun.Add(time.Hour).Truncate(time.Second), stored.NextRun.Truncate(time.Second),
		"the job waits for its next scheduled firing, not a retry backoff")

	stored = failRun(t, engine, storage, job.ID)
	assert.Equal(t, JobStatusPending, stored.Status)
	assert.Equal(t, uint64(1), stored.FailCount)
}

// TestRetryBudget_NonRecurringJobFailsWhenTheBudgetIsSpent pins that a job
// with no next firing still fails once its only firing's budget is spent.
func TestRetryBudget_NonRecurringJobFailsWhenTheBudgetIsSpent(t *testing.T) {
	for _, jobType := range []JobType{JobTypeOneShot, JobTypeDelayed} {
		t.Run(string(jobType), func(t *testing.T) {
			storage := newMockJobStorage()
			engine := New(DefaultConfig(), storage)
			job := &Job{
				Name:       "once",
				Type:       jobType,
				ExecuteAt:  time.Now(),
				Delay:      time.Minute,
				Handler:    "work",
				Status:     JobStatusRunning,
				MaxRetries: 2,
			}
			require.NoError(t, storage.CreateJob(context.Background(), job))

			stored := failRun(t, engine, storage, job.ID)
			require.Equal(t, JobStatusPending, stored.Status)

			stored = failRun(t, engine, storage, job.ID)
			assert.Equal(t, JobStatusFailed, stored.Status)
			assert.Equal(t, uint64(2), stored.FailCount)
		})
	}
}

// TestRetryBudget_UnschedulableRecurringJobFailsWhenTheBudgetIsSpent covers
// the error path of moving on: a recurring job whose schedule no longer
// parses has no next firing, so a spent budget fails it and says why.
func TestRetryBudget_UnschedulableRecurringJobFailsWhenTheBudgetIsSpent(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	job := budgetJob(t, storage)
	job.Schedule = "not a schedule"
	job.MaxRetries = 1
	require.NoError(t, storage.UpdateJob(context.Background(), job))

	stored := failRun(t, engine, storage, job.ID)

	assert.Equal(t, JobStatusFailed, stored.Status)
	assert.Contains(t, stored.LastError, "not a schedule")
}

// TestRetryBudget_ResumePastAMissedFiringStartsAFreshBudget pins the reset on
// resume: a recurring job paused mid-retry whose firing passed resumes on a
// new firing.
func TestRetryBudget_ResumePastAMissedFiringStartsAFreshBudget(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() {
		require.NoError(t, engine.Stop(context.Background()))
	})

	job := budgetJob(t, storage)
	job.Status = JobStatusPaused
	job.FailCount = 2
	job.LastRun = time.Now().Add(-time.Minute)
	job.NextRun = time.Now().Add(-time.Second)
	require.NoError(t, storage.UpdateJob(context.Background(), job))

	require.NoError(t, engine.ResumeJob(context.Background(), job.ID, ""))

	stored := storedJob(t, storage, job.ID)
	assert.Equal(t, JobStatusPending, stored.Status)
	assert.Zero(t, stored.FailCount)
	assert.True(t, stored.NextRun.After(time.Now()))
}

// TestRetryBudget_ResumeOnTheSameFiringKeepsItsBudget is the negative: a
// resume that does not skip a firing is still in that firing's budget.
func TestRetryBudget_ResumeOnTheSameFiringKeepsItsBudget(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() {
		require.NoError(t, engine.Stop(context.Background()))
	})

	job := budgetJob(t, storage)
	job.Status = JobStatusPaused
	job.FailCount = 2
	job.NextRun = time.Now().Add(time.Hour)
	require.NoError(t, storage.UpdateJob(context.Background(), job))

	require.NoError(t, engine.ResumeJob(context.Background(), job.ID, ""))

	assert.Equal(t, uint64(2), storedJob(t, storage, job.ID).FailCount)
}

// TestInstallSystemJobs_RearmStartsAFreshBudget pins the reset at install: a
// system job re-armed from Failed must not inherit the failures that failed
// it, or its first failure after the re-arm would fail it again.
func TestInstallSystemJobs_RearmStartsAFreshBudget(t *testing.T) {
	storage := newMockJobStorage()
	stored := systemJob()
	stored.Status = JobStatusFailed
	stored.FailCount = 3
	stored.LastRun = time.Now().Add(-time.Hour)
	storage.jobs[stored.ID] = copyJob(stored)

	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterSystemJob(systemJob()))
	engine.installSystemJobs(context.Background())

	job := storage.getJobDirect(stored.ID)
	assert.Equal(t, JobStatusPending, job.Status)
	assert.Zero(t, job.FailCount)
}
