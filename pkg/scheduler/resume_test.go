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

// resumeFunc is one of the two resume entry points, reduced to its error.
type resumeFunc func(s *SchedulerEngine, ctx context.Context, id uint64) error

// resumeEntryPoints covers ResumeJob and ResumeJobWithStatus, which share the
// rescheduling rule and must not drift apart.
var resumeEntryPoints = map[string]resumeFunc{
	"ResumeJob": func(s *SchedulerEngine, ctx context.Context, id uint64) error {
		return s.ResumeJob(ctx, id, "")
	},
	"ResumeJobWithStatus": func(s *SchedulerEngine, ctx context.Context, id uint64) error {
		_, err := s.ResumeJobWithStatus(ctx, id, "")
		return err
	},
}

// startedIdleEngine returns a running engine whose tick never fires during
// the test, so the only writes to the store are the test's own.
func startedIdleEngine(t *testing.T, storage *mockJobStorage) *SchedulerEngine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.TickInterval = time.Hour
	engine := New(cfg, storage)
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, engine.Stop(ctx))
	})
	return engine
}

// pausedRecurringJob stores a paused "@every 1h" job with the given last and
// next runs and a partly spent retry budget.
func pausedRecurringJob(storage *mockJobStorage, schedule string, lastRun, nextRun time.Time) *Job {
	job := &Job{
		ID:        1,
		Name:      "paused-recurring",
		Type:      JobTypeRecurring,
		Schedule:  schedule,
		Handler:   "work",
		Status:    JobStatusPaused,
		LastRun:   lastRun,
		NextRun:   nextRun,
		FailCount: 2,
	}
	storage.mu.Lock()
	storage.jobs[job.ID] = copyJob(job)
	storage.mu.Unlock()
	return job
}

// TestResume_RecurringJobResumesOnItsSchedule is the defect: the next run was
// computed from LastRun, so a job paused for longer than its interval resumed
// with a NextRun already in the past and fired on the next tick. It must
// resume on the schedule's next occurrence strictly after the resume.
func TestResume_RecurringJobResumesOnItsSchedule(t *testing.T) {
	for name, resume := range resumeEntryPoints {
		t.Run(name, func(t *testing.T) {
			storage := newMockJobStorage()
			engine := startedIdleEngine(t, storage)
			lastRun := time.Now().Add(-5 * time.Hour)
			job := pausedRecurringJob(storage, "@every 1h", lastRun, lastRun.Add(time.Hour))

			resumedAt := time.Now()
			require.NoError(t, resume(engine, context.Background(), job.ID))

			stored := storage.getJobDirect(job.ID)
			assert.Equal(t, JobStatusPending, stored.Status)
			assert.True(t, stored.NextRun.After(resumedAt),
				"a resumed job's next run %s must be after the resume %s", stored.NextRun, resumedAt)
			assert.False(t, stored.NextRun.After(time.Now().Add(time.Hour)),
				"the next run %s must be the schedule's next occurrence, one interval out", stored.NextRun)
			assert.Zero(t, stored.FailCount, "a resume past a missed firing starts a new firing")
		})
	}
}

// TestResume_LastRunAheadOfThisClockStillWins pins the clock-skew guard: a
// LastRun stamped by a node whose clock leads this one is the later base, so
// the new firing lands after the run that already happened rather than on it.
func TestResume_LastRunAheadOfThisClockStillWins(t *testing.T) {
	storage := newMockJobStorage()
	engine := startedIdleEngine(t, storage)
	lastRun := time.Now().Add(2 * time.Hour)
	job := pausedRecurringJob(storage, "@every 1h", lastRun, time.Now().Add(-time.Minute))

	require.NoError(t, engine.ResumeJob(context.Background(), job.ID, ""))

	stored := storage.getJobDirect(job.ID)
	assert.True(t, stored.NextRun.After(lastRun),
		"the next run %s must follow the last run %s", stored.NextRun, lastRun)
}

// TestResume_AFutureFiringIsKept pins that a resume which missed nothing
// changes nothing about the firing: it keeps its time and its retry budget.
func TestResume_AFutureFiringIsKept(t *testing.T) {
	storage := newMockJobStorage()
	engine := startedIdleEngine(t, storage)
	nextRun := time.Now().Add(30 * time.Minute)
	job := pausedRecurringJob(storage, "@every 1h", time.Now().Add(-30*time.Minute), nextRun)

	require.NoError(t, engine.ResumeJob(context.Background(), job.ID, ""))

	stored := storage.getJobDirect(job.ID)
	assert.True(t, nextRun.Equal(stored.NextRun), "an unmissed firing keeps its time")
	assert.Equal(t, uint64(2), stored.FailCount, "an unmissed firing keeps its retry budget")
}

// TestResume_AnUnparseableScheduleRefusesAndLeavesTheJobPaused covers the
// error path: a schedule that cannot produce a next run refuses the resume
// with a typed error, and nothing is written.
func TestResume_AnUnparseableScheduleRefusesAndLeavesTheJobPaused(t *testing.T) {
	for name, resume := range resumeEntryPoints {
		t.Run(name, func(t *testing.T) {
			storage := newMockJobStorage()
			engine := startedIdleEngine(t, storage)
			lastRun := time.Now().Add(-5 * time.Hour)
			job := pausedRecurringJob(storage, "not a schedule", lastRun, lastRun.Add(time.Hour))

			err := resume(engine, context.Background(), job.ID)

			var invalid InvalidScheduleError
			require.True(t, errors.As(err, &invalid), "expected InvalidScheduleError, got %T: %v", err, err)
			assert.Equal(t, JobStatusPaused, storage.getJobDirect(job.ID).Status)
			assert.Zero(t, storage.updateJobCount.Load())
		})
	}
}
