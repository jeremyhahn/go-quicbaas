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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func startedPlatformTestEngine(t *testing.T) (*SchedulerEngine, *mockJobStorage) {
	t.Helper()
	storage := newMockJobStorage()
	engine := New(newRaceTestConfig(), storage)
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	return engine, storage
}

// storeDueJob writes job straight into storage, due now, bypassing every
// create-time check, the way a record written by an older node or another
// path would sit in the store.
func storeDueJob(t *testing.T, storage *mockJobStorage, job *Job) *Job {
	t.Helper()
	now := time.Now()
	job.Type = JobTypeOneShot
	job.ExecuteAt = now.Add(-time.Second)
	job.NextRun = now.Add(-time.Second)
	job.Status = JobStatusPending
	job.CreatedAt = now
	job.UpdatedAt = now
	require.NoError(t, storage.CreateJob(context.Background(), job))
	return job
}

// runDueScan runs one due scan and waits for the executions it dispatched.
func runDueScan(engine *SchedulerEngine) {
	engine.processDueJobs(context.Background())
	engine.wg.Wait()
}

func TestPlatformHandlers_ListIsSortedAndMatchesIsPlatformHandler(t *testing.T) {
	names := PlatformHandlers()
	require.NotEmpty(t, names)
	for i, name := range names {
		assert.True(t, IsPlatformHandler(name), name)
		if i > 0 {
			assert.Less(t, names[i-1], name, "sorted")
		}
	}
	assert.False(t, IsPlatformHandler("noop"))
	assert.False(t, IsPlatformHandler(""))
}

// TestScheduleJob_RefusesTenantJobOfPlatformHandler: the engine refuses a
// tenant-owned job of any platform handler, whatever path reached it.
func TestScheduleJob_RefusesTenantJobOfPlatformHandler(t *testing.T) {
	for _, handler := range PlatformHandlers() {
		t.Run(handler, func(t *testing.T) {
			engine, storage := startedPlatformTestEngine(t)
			err := engine.ScheduleJob(context.Background(), &Job{
				Name:      "forged",
				Type:      JobTypeOneShot,
				ExecuteAt: time.Now().Add(time.Hour),
				Handler:   handler,
				OwnerType: OwnerTypeTenant,
				OwnerID:   "tenant-a",
			})
			var platformErr PlatformHandlerError
			require.ErrorAs(t, err, &platformErr)
			assert.Equal(t, handler, platformErr.Handler)
			assert.Equal(t, "tenant-a", platformErr.TenantID)
			assert.Equal(t, ErrCodePlatformHandler, platformErr.Code())
			assert.Zero(t, storage.jobCount(), "no job is stored")
		})
	}
}

// TestUpdateJob_RefusesTenantJobToPlatformHandler: a tenant-owned job cannot
// be turned into a platform handler's job through the engine either.
func TestUpdateJob_RefusesTenantJobToPlatformHandler(t *testing.T) {
	engine, storage := startedPlatformTestEngine(t)
	job := &Job{
		Name:      "tenant-job",
		Type:      JobTypeOneShot,
		ExecuteAt: time.Now().Add(time.Hour),
		Handler:   "noop",
		OwnerType: OwnerTypeTenant,
		OwnerID:   "tenant-a",
	}
	require.NoError(t, engine.ScheduleJob(context.Background(), job))

	_, err := engine.UpdateJob(context.Background(), job.ID, "tenant-a", &JobUpdateParams{Handler: PlatformHandlerKMSRotation})
	var platformErr PlatformHandlerError
	require.ErrorAs(t, err, &platformErr)
	assert.Equal(t, "noop", storage.getJobDirect(job.ID).Handler, "the stored handler is unchanged")
}

// TestExecuteJob_RefusesTenantJobOfPlatformHandler: a tenant-owned
// kms.rotation record already in the store comes up in the due scan, but its
// handler never runs; the job fails with PlatformHandlerError and is not
// retried.
func TestExecuteJob_RefusesTenantJobOfPlatformHandler(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(newRaceTestConfig(), storage)

	var calls atomic.Int32
	require.NoError(t, engine.RegisterHandler(PlatformHandlerKMSRotation, func(context.Context, *Job) (*JobResult, error) {
		calls.Add(1)
		return &JobResult{Success: true}, nil
	}))
	job := storeDueJob(t, storage, &Job{
		Name:       "tenant-rotation",
		Handler:    PlatformHandlerKMSRotation,
		OwnerType:  OwnerTypeTenant,
		OwnerID:    "tenant-a",
		MaxRetries: 3,
	})

	runDueScan(engine)

	assert.Zero(t, calls.Load(), "the platform handler never runs for a tenant job")
	stored := storage.getJobDirect(job.ID)
	assert.Equal(t, JobStatusFailed, stored.Status, "the job fails and is not retried")
	assert.Equal(t, PlatformHandlerError{Handler: PlatformHandlerKMSRotation, TenantID: "tenant-a"}.Error(), stored.LastError)
}

// TestExecuteJob_SystemJobOfPlatformHandlerRuns: a system-owned job of a
// platform handler still runs.
func TestExecuteJob_SystemJobOfPlatformHandlerRuns(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(newRaceTestConfig(), storage)

	var calls atomic.Int32
	require.NoError(t, engine.RegisterHandler(PlatformHandlerKMSRotation, func(context.Context, *Job) (*JobResult, error) {
		calls.Add(1)
		return &JobResult{Success: true}, nil
	}))
	job := storeDueJob(t, storage, &Job{
		Name:      "system-rotation",
		Handler:   PlatformHandlerKMSRotation,
		OwnerType: OwnerTypeSystem,
	})

	runDueScan(engine)

	assert.Equal(t, int32(1), calls.Load(), "the system job runs")
	assert.Equal(t, JobStatusCompleted, storage.getJobDirect(job.ID).Status)
}

// TestExecuteJob_TenantJobRunsWithItsTenantBound: a tenant-owned job's
// handler sees that tenant in its context, and not the system tenant.
func TestExecuteJob_TenantJobRunsWithItsTenantBound(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(newRaceTestConfig(), storage)

	type seen struct {
		tenantID string
		err      error
	}
	seenCh := make(chan seen, 1)
	require.NoError(t, engine.RegisterHandler("noop", func(ctx context.Context, _ *Job) (*JobResult, error) {
		tenantID, err := tenantctx.FromContext(ctx, "test")
		seenCh <- seen{tenantID: tenantID, err: err}
		return &JobResult{Success: true}, nil
	}))
	storeDueJob(t, storage, &Job{
		Name:      "tenant-noop",
		Handler:   "noop",
		OwnerType: OwnerTypeTenant,
		OwnerID:   "tenant-a",
	})

	runDueScan(engine)

	select {
	case got := <-seenCh:
		require.NoError(t, got.err)
		assert.Equal(t, "tenant-a", got.tenantID)
		assert.False(t, tenantctx.IsSystem(got.tenantID))
	default:
		t.Fatal("the tenant job's handler did not run")
	}
}

// TestExecuteJob_SystemJobContextHasNoTenantBound: a system-owned job's
// context is left as the engine's own; no tenant is bound for it.
func TestExecuteJob_SystemJobContextHasNoTenantBound(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(newRaceTestConfig(), storage)

	errCh := make(chan error, 1)
	require.NoError(t, engine.RegisterHandler("noop", func(ctx context.Context, _ *Job) (*JobResult, error) {
		_, err := tenantctx.FromContext(ctx, "test")
		errCh <- err
		return &JobResult{Success: true}, nil
	}))
	storeDueJob(t, storage, &Job{Name: "system-noop", Handler: "noop", OwnerType: OwnerTypeSystem})

	runDueScan(engine)

	select {
	case err := <-errCh:
		var missing *tenantctx.MissingTenantError
		assert.True(t, errors.As(err, &missing), "no tenant is bound for a system job")
	default:
		t.Fatal("the system job's handler did not run")
	}
}
