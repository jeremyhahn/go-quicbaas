// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// systemJob returns a valid system job definition under the reserved range.
func systemJob() *Job {
	return &Job{
		ID:        SystemJobIDFloor + 42,
		Name:      "sweep",
		OwnerType: OwnerTypeSystem,
		Type:      JobTypeRecurring,
		Schedule:  "@every 1m",
		Handler:   "sweep",
		Timeout:   time.Minute,
	}
}

// pendingSystemJobs reports how many registered system jobs await install.
func pendingSystemJobs(s *SchedulerEngine) int {
	s.systemJobsMu.Lock()
	defer s.systemJobsMu.Unlock()
	return len(s.systemJobs)
}

func TestRegisterSystemJob_RejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Job) *Job
		field  string
	}{
		{"nil job", func(*Job) *Job { return nil }, "job"},
		{"tenant owned", func(j *Job) *Job { j.OwnerType = OwnerTypeTenant; return j }, "owner_type"},
		{"id below the reserved floor", func(j *Job) *Job { j.ID = 7; return j }, "id"},
		{"no handler", func(j *Job) *Job { j.Handler = ""; return j }, "handler"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := New(DefaultConfig(), newMockJobStorage())

			err := engine.RegisterSystemJob(tc.mutate(systemJob()))

			var invalid InvalidJobError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tc.field, invalid.Field)
			assert.Zero(t, pendingSystemJobs(engine))
		})
	}
}

func TestRegisterSystemJob_RejectsInvalidSchedule(t *testing.T) {
	engine := New(DefaultConfig(), newMockJobStorage())
	job := systemJob()
	job.Schedule = "not a schedule"

	var invalid InvalidScheduleError
	require.ErrorAs(t, engine.RegisterSystemJob(job), &invalid)
}

func TestRegisterSystemJob_RejectsDuplicateID(t *testing.T) {
	engine := New(DefaultConfig(), newMockJobStorage())
	require.NoError(t, engine.RegisterSystemJob(systemJob()))

	var exists JobAlreadyExistsError
	require.ErrorAs(t, engine.RegisterSystemJob(systemJob()), &exists)
	assert.Equal(t, systemJob().ID, exists.JobID)
}

func TestInstallSystemJobs_CreatesTheRecordOnce(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterSystemJob(systemJob()))

	engine.installSystemJobs(context.Background())

	job := storage.getJobDirect(systemJob().ID)
	require.NotNil(t, job, "the system job is stored under its reserved ID")
	assert.Equal(t, JobStatusPending, job.Status)
	assert.True(t, job.NextRun.After(time.Now()), "the first firing is one interval out")
	assert.Zero(t, pendingSystemJobs(engine), "an installed job is not installed again")

	engine.installSystemJobs(context.Background())
	assert.Equal(t, int32(1), storage.createJobCount.Load())
}

// TestInstallSystemJobs_EveryNodeNamesOneRecord is why system jobs carry a
// reserved ID: every node declares the job, and the cluster must still hold
// one record for the firing claim to arbitrate.
func TestInstallSystemJobs_EveryNodeNamesOneRecord(t *testing.T) {
	storage := newMockJobStorage()
	nodes := make([]*SchedulerEngine, 0, 3)
	for _, id := range []string{"node-A", "node-B", "node-C"} {
		engine, err := NewWithClustering(DefaultConfig(), storage, id, &fakeLeader{isLeader: id == "node-A"})
		require.NoError(t, err)
		require.NoError(t, engine.RegisterSystemJob(systemJob()))
		nodes = append(nodes, engine)
	}

	var wg sync.WaitGroup
	for _, engine := range nodes {
		wg.Add(1)
		go func(e *SchedulerEngine) { defer wg.Done(); e.installSystemJobs(context.Background()) }(engine)
	}
	wg.Wait()

	assert.Equal(t, 1, storage.jobCount())
}

func TestInstallSystemJobs_RetriesUntilTheStoreAccepts(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterSystemJob(systemJob()))

	storage.errMu.Lock()
	storage.getJobErr = errors.New("no leader yet")
	storage.errMu.Unlock()
	engine.installSystemJobs(context.Background())

	assert.Equal(t, 1, pendingSystemJobs(engine), "a failed install stays pending")
	assert.Equal(t, uint64(1), engine.StorageErrorCount())
	assert.Nil(t, storage.getJobDirect(systemJob().ID))

	storage.errMu.Lock()
	storage.getJobErr = nil
	storage.errMu.Unlock()
	engine.installSystemJobs(context.Background())

	assert.Zero(t, pendingSystemJobs(engine))
	assert.NotNil(t, storage.getJobDirect(systemJob().ID))
}

// TestInstallSystemJobs_StoreErrorNeverReadsAsAbsent pins that install
// treats a failed read of an EXISTING record as "retry next tick", never as
// "absent": creating from the definition would overwrite the record — here
// one mid-run on another node — with a fresh pending copy.
func TestInstallSystemJobs_StoreErrorNeverReadsAsAbsent(t *testing.T) {
	storage := newMockJobStorage()
	stored := systemJob()
	stored.Status = JobStatusRunning
	stored.RunCount = 7
	storage.jobs[stored.ID] = copyJob(stored)
	storage.getJobErr = ErrStorageError{Op: "GetJob.get", Cause: errors.New("no leader")}

	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterSystemJob(systemJob()))
	engine.installSystemJobs(context.Background())

	assert.Zero(t, storage.createJobCount.Load(), "a store error must never be read as an absent record")
	assert.Zero(t, storage.updateJobCount.Load())
	assert.Equal(t, 1, pendingSystemJobs(engine), "install is retried next tick")
	assert.Equal(t, uint64(1), engine.StorageErrorCount())
	job := storage.getJobDirect(stored.ID)
	assert.Equal(t, JobStatusRunning, job.Status)
	assert.Equal(t, uint64(7), job.RunCount)
}

// TestInstallSystemJobs_TypedNotFoundCreatesTheRecord is the other half: the
// store's typed not-found — what pkg/scheduler/storage/quicbaas returns for
// an absent key — is what install creates from.
func TestInstallSystemJobs_TypedNotFoundCreatesTheRecord(t *testing.T) {
	storage := newMockJobStorage()
	storage.getJobErr = &JobNotFoundError{JobID: systemJob().ID}

	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterSystemJob(systemJob()))
	engine.installSystemJobs(context.Background())

	assert.Equal(t, int32(1), storage.createJobCount.Load())
	assert.Zero(t, pendingSystemJobs(engine))
	assert.Zero(t, engine.StorageErrorCount())
}

func TestInstallSystemJobs_CreateFailureStaysPending(t *testing.T) {
	storage := newMockJobStorage()
	storage.createJobErr = errors.New("disk full")
	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterSystemJob(systemJob()))

	engine.installSystemJobs(context.Background())

	assert.Equal(t, 1, pendingSystemJobs(engine))
	assert.Equal(t, uint64(1), engine.StorageErrorCount())
}

// TestInstallSystemJobs_ReconcilesTheStoredRecord covers a record left by an
// earlier boot: failed runs are re-armed, a changed definition is applied, and
// an operator's pause is kept.
func TestInstallSystemJobs_ReconcilesTheStoredRecord(t *testing.T) {
	lastRun := time.Now().Add(-time.Hour)
	tests := []struct {
		name        string
		stored      func(*Job)
		wantStatus  JobStatus
		wantSched   string
		rescheduled bool
	}{
		{"failed run is re-armed", func(j *Job) { j.Status = JobStatusFailed }, JobStatusPending, "@every 1m", true},
		{"changed schedule is applied", func(j *Job) { j.Schedule = "@every 5m" }, JobStatusPending, "@every 1m", true},
		{"operator pause is kept", func(j *Job) { j.Status = JobStatusPaused }, JobStatusPaused, "@every 1m", false},
		{"matching record is left alone", func(*Job) {}, JobStatusPending, "@every 1m", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			storage := newMockJobStorage()
			stored := systemJob()
			stored.Status = JobStatusPending
			stored.LastRun = lastRun
			stored.NextRun = lastRun.Add(24 * time.Hour)
			tc.stored(stored)
			storage.jobs[stored.ID] = copyJob(stored)

			engine := New(DefaultConfig(), storage)
			require.NoError(t, engine.RegisterSystemJob(systemJob()))
			engine.installSystemJobs(context.Background())

			job := storage.getJobDirect(stored.ID)
			assert.Equal(t, tc.wantStatus, job.Status)
			assert.Equal(t, tc.wantSched, job.Schedule)
			if tc.rescheduled {
				assert.Equal(t, lastRun.Add(time.Minute).Truncate(time.Second), job.NextRun.Truncate(time.Second),
					"a reconciled record is rescheduled from its last run")
			} else {
				assert.Equal(t, stored.NextRun, job.NextRun)
			}
			assert.Zero(t, pendingSystemJobs(engine))
		})
	}
}

// TestInstallSystemJobs_WaitsOutARunningJob pins that install never rewrites a
// running record: the execution's outcome write would overwrite it.
func TestInstallSystemJobs_WaitsOutARunningJob(t *testing.T) {
	storage := newMockJobStorage()
	stored := systemJob()
	stored.Status = JobStatusRunning
	stored.Schedule = "@every 5m"
	storage.jobs[stored.ID] = copyJob(stored)

	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterSystemJob(systemJob()))
	engine.installSystemJobs(context.Background())

	assert.Equal(t, 1, pendingSystemJobs(engine), "install is retried after the run")
	assert.Equal(t, "@every 5m", storage.getJobDirect(stored.ID).Schedule)
	assert.Zero(t, storage.updateJobCount.Load())
}

func TestInstallSystemJobs_UpdateFailureStaysPending(t *testing.T) {
	storage := newMockJobStorage()
	stored := systemJob()
	stored.Status = JobStatusFailed
	storage.jobs[stored.ID] = copyJob(stored)
	storage.updateJobErr = errors.New("disk full")

	engine := New(DefaultConfig(), storage)
	require.NoError(t, engine.RegisterSystemJob(systemJob()))
	engine.installSystemJobs(context.Background())

	assert.Equal(t, 1, pendingSystemJobs(engine))
	assert.Equal(t, uint64(1), engine.StorageErrorCount())
}

// TestSystemJob_LeaderChurn_FiresOnce is the property system jobs are on the
// scheduler for: two nodes that both declared the job and both believe they
// lead run each firing exactly once.
func TestSystemJob_LeaderChurn_FiresOnce(t *testing.T) {
	storage := newMockJobStorage()
	storage.claimBarrier = newClaimBarrier(2)
	storage.dueBarrier = newClaimBarrier(2)

	runs := map[string]*atomicCounter{"node-A": newCounter(), "node-B": newCounter()}
	nodes := make([]*SchedulerEngine, 0, 2)
	for _, id := range []string{"node-A", "node-B"} {
		engine, err := NewWithClustering(DefaultConfig(), storage, id, &fakeLeader{isLeader: true})
		require.NoError(t, err)
		counter := runs[id]
		require.NoError(t, engine.RegisterHandler("sweep", func(context.Context, *Job) (*JobResult, error) {
			counter.inc()
			return &JobResult{Success: true}, nil
		}))
		require.NoError(t, engine.RegisterSystemJob(systemJob()))
		engine.installSystemJobs(context.Background())
		nodes = append(nodes, engine)
	}

	// Make the installed firing due now.
	storage.mu.Lock()
	storage.jobs[systemJob().ID].NextRun = time.Now().Add(-time.Second)
	storage.mu.Unlock()

	var ticking sync.WaitGroup
	for _, engine := range nodes {
		ticking.Add(1)
		go func(e *SchedulerEngine) { defer ticking.Done(); e.processDueJobs(context.Background()) }(engine)
	}
	ticking.Wait()
	for _, engine := range nodes {
		engine.wg.Wait()
	}

	assert.Equal(t, 1, runs["node-A"].get()+runs["node-B"].get(),
		"node-A ran %d, node-B ran %d", runs["node-A"].get(), runs["node-B"].get())
	assert.Len(t, storage.claims(), 1, "the store granted exactly one claim")
}
