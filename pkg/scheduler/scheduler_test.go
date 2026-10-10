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
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// copyJob creates a deep copy of a job to prevent race conditions.
func copyJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	cp := *job
	// Deep copy payload
	if job.Payload != nil {
		cp.Payload = make(json.RawMessage, len(job.Payload))
		copy(cp.Payload, job.Payload)
	}
	// Deep copy labels
	if job.Labels != nil {
		cp.Labels = make(map[string]string, len(job.Labels))
		for k, v := range job.Labels {
			cp.Labels[k] = v
		}
	}
	return &cp
}

// mockJobStorage implements JobStorage interface for testing.
type mockJobStorage struct {
	mu    sync.RWMutex
	errMu sync.RWMutex // protects error injection fields

	jobs       map[uint64]*Job
	executions []*JobExecution
	nextID     uint64

	// firingClaims is the store-side claim ledger: one record per
	// (job, scheduled firing time), written only when absent.
	firingClaims map[firingClaimKey]*FiringClaim

	// claimBarrier, when set, holds arriving claims until the configured
	// number of nodes have arrived so the leader-churn tests can force a
	// genuine race without a sleep.
	claimBarrier *claimBarrier

	// dueBarrier, when set, holds each node inside GetJobsDueForExecution
	// until every node has read the due list. Without it the churn tests are
	// decided by which goroutine happens to run first: a node that ticks
	// after the winner already completed a recurring job sees NextRun pushed
	// into the future and finds nothing due, so the test would pass even with
	// the claim removed. Holding both nodes at the read makes the claim the
	// only thing that separates them.
	dueBarrier *claimBarrier

	// afterUpdateJob observes committed updates in tests that need to
	// synchronize with the stored outcome rather than handler entry.
	afterUpdateJob func(*Job)

	// Error injection for testing error paths
	createJobErr              error
	claimErr                  error
	getJobErr                 error
	updateJobErr              error
	deleteJobErr              error
	listJobsErr               error
	getJobsDueForExecutionErr error
	saveExecutionErr          error

	getExecutionsErr error
	initIDsErr       error

	// honorCancel makes job and execution reads and writes refuse a done
	// context, the way a Raft-backed store does. Off by default because most
	// tests drive outcome paths with contexts they never cancel.
	honorCancel atomic.Bool

	// Track call counts for verification
	createJobCount              atomic.Int32
	getJobCount                 atomic.Int32
	updateJobCount              atomic.Int32
	deleteJobCount              atomic.Int32
	listJobsCount               atomic.Int32
	getJobsDueForExecutionCount atomic.Int32
	saveExecutionCount          atomic.Int32
	getExecutionsCount          atomic.Int32
	claimCount                  atomic.Int32
}

func newMockJobStorage() *mockJobStorage {
	return &mockJobStorage{
		jobs:         make(map[uint64]*Job),
		firingClaims: make(map[firingClaimKey]*FiringClaim),
		nextID:       1,
	}
}

func (m *mockJobStorage) CreateJob(ctx context.Context, job *Job) error {
	m.createJobCount.Add(1)
	m.errMu.RLock()
	createJobErr := m.createJobErr
	m.errMu.RUnlock()
	if createJobErr != nil {
		return createJobErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Honor a caller-chosen ID the way the real store does; system jobs
	// carry a reserved one.
	if job.ID == 0 {
		job.ID = m.nextID
		m.nextID++
	}
	// Store a copy to prevent race conditions
	m.jobs[job.ID] = copyJob(job)
	return nil
}

// InitializeJobIDCounter reports the injected initialisation error, if any.
// The mock's in-memory counter needs no loading.
func (m *mockJobStorage) InitializeJobIDCounter(context.Context) error {
	m.errMu.RLock()
	defer m.errMu.RUnlock()
	return m.initIDsErr
}

func (m *mockJobStorage) GetJob(ctx context.Context, id uint64, tenantID string) (*Job, error) {
	if m.honorCancel.Load() && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.getJobCount.Add(1)
	m.errMu.RLock()
	getJobErr := m.getJobErr
	m.errMu.RUnlock()
	if getJobErr != nil {
		return nil, getJobErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	job, ok := m.jobs[id]
	if !ok {
		return nil, nil
	}
	// Return a copy to prevent race conditions
	return copyJob(job), nil
}

func (m *mockJobStorage) UpdateJob(ctx context.Context, job *Job) error {
	if m.honorCancel.Load() && ctx.Err() != nil {
		return ctx.Err()
	}
	m.updateJobCount.Add(1)
	m.errMu.RLock()
	updateJobErr := m.updateJobErr
	m.errMu.RUnlock()
	if updateJobErr != nil {
		return updateJobErr
	}

	stored := copyJob(job)
	m.mu.Lock()
	m.jobs[job.ID] = stored
	m.mu.Unlock()

	if m.afterUpdateJob != nil {
		m.afterUpdateJob(copyJob(stored))
	}
	return nil
}

func (m *mockJobStorage) DeleteJob(ctx context.Context, id uint64, tenantID string) error {
	m.deleteJobCount.Add(1)
	m.errMu.RLock()
	deleteJobErr := m.deleteJobErr
	m.errMu.RUnlock()
	if deleteJobErr != nil {
		return deleteJobErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.jobs, id)
	return nil
}

func (m *mockJobStorage) ListJobs(ctx context.Context, query *JobQuery) ([]*Job, string, error) {
	m.listJobsCount.Add(1)
	m.errMu.RLock()
	listJobsErr := m.listJobsErr
	m.errMu.RUnlock()
	if listJobsErr != nil {
		return nil, "", listJobsErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Job
	for _, job := range m.jobs {
		// Apply filters if query provided
		if query != nil {
			if query.Status != "" && job.Status != query.Status {
				continue
			}
			if query.Handler != "" && job.Handler != query.Handler {
				continue
			}
			if query.OwnerType != "" && job.OwnerType != query.OwnerType {
				continue
			}
		}
		// Return copies to prevent race conditions
		result = append(result, copyJob(job))
	}

	// Apply limit
	if query != nil && query.Limit > 0 && len(result) > query.Limit {
		result = result[:query.Limit]
	}

	return result, "", nil
}

func (m *mockJobStorage) GetJobsDueForExecution(ctx context.Context, before time.Time, limit int) ([]*Job, error) {
	m.getJobsDueForExecutionCount.Add(1)
	m.errMu.RLock()
	getJobsDueForExecutionErr := m.getJobsDueForExecutionErr
	m.errMu.RUnlock()
	if getJobsDueForExecutionErr != nil {
		return nil, getJobsDueForExecutionErr
	}

	if m.dueBarrier != nil {
		m.dueBarrier.wait()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Job
	for _, job := range m.jobs {
		if job.Status == JobStatusPending && !job.NextRun.After(before) {
			// Return copies to prevent race conditions
			result = append(result, copyJob(job))
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockJobStorage) SaveExecution(ctx context.Context, exec *JobExecution) error {
	if m.honorCancel.Load() && ctx.Err() != nil {
		return ctx.Err()
	}
	m.saveExecutionCount.Add(1)
	m.errMu.RLock()
	saveExecutionErr := m.saveExecutionErr
	m.errMu.RUnlock()
	if saveExecutionErr != nil {
		return saveExecutionErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy
	cp := *exec
	m.executions = append(m.executions, &cp)
	return nil
}

func (m *mockJobStorage) GetExecutions(ctx context.Context, jobID uint64, tenantID string, limit int) ([]*JobExecution, error) {
	m.getExecutionsCount.Add(1)
	m.errMu.RLock()
	getExecutionsErr := m.getExecutionsErr
	m.errMu.RUnlock()
	if getExecutionsErr != nil {
		return nil, getExecutionsErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*JobExecution
	for _, exec := range m.executions {
		if exec.JobID == jobID {
			cp := *exec
			result = append(result, &cp)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// Helper to get a job directly from storage for assertions
func (m *mockJobStorage) getJobDirect(id uint64) *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil
	}
	return copyJob(job)
}

// Helper to count jobs in storage
func (m *mockJobStorage) jobCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.jobs)
}

// TestNew tests the New constructor function.
func TestNew(t *testing.T) {
	t.Run("creates scheduler with provided config", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      500 * time.Millisecond,
			MaxConcurrentJobs: 5,
			DefaultTimeout:    time.Minute,
			DefaultRetries:    2,
			RetryBackoff:      time.Second,
		}

		scheduler := New(config, storage)

		require.NotNil(t, scheduler)
		assert.Equal(t, config, scheduler.config)
		assert.Equal(t, storage, scheduler.storage)
		assert.NotNil(t, scheduler.handlers)
		assert.NotNil(t, scheduler.triggerCh)
		assert.NotNil(t, scheduler.semaphore)
		assert.Equal(t, 5, cap(scheduler.semaphore))
	})

	t.Run("creates scheduler with default config when nil", func(t *testing.T) {
		storage := newMockJobStorage()

		scheduler := New(nil, storage)

		require.NotNil(t, scheduler)
		assert.NotNil(t, scheduler.config)
		assert.Equal(t, DefaultConfig().TickInterval, scheduler.config.TickInterval)
		assert.Equal(t, DefaultConfig().MaxConcurrentJobs, scheduler.config.MaxConcurrentJobs)
		assert.Equal(t, DefaultConfig().DefaultTimeout, scheduler.config.DefaultTimeout)
		assert.Equal(t, DefaultConfig().DefaultRetries, scheduler.config.DefaultRetries)
	})
}

// TestRegisterHandler tests handler registration.
func TestRegisterHandler(t *testing.T) {
	t.Run("registers handler successfully", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		handler := func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		}

		err := scheduler.RegisterHandler("test-handler", handler)

		require.NoError(t, err)
		assert.Equal(t, 1, scheduler.HandlerCount())
	})

	t.Run("returns error for empty handler name", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		handler := func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		}

		err := scheduler.RegisterHandler("", handler)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "handler", invalidErr.Field)
	})

	t.Run("returns error for nil handler", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		err := scheduler.RegisterHandler("test-handler", nil)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "handler", invalidErr.Field)
	})

	t.Run("returns error for duplicate handler registration", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		handler := func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		}

		err := scheduler.RegisterHandler("test-handler", handler)
		require.NoError(t, err)

		err = scheduler.RegisterHandler("test-handler", handler)

		require.Error(t, err)
		var existsErr JobAlreadyExistsError
		assert.True(t, errors.As(err, &existsErr))
	})

	t.Run("allows multiple unique handlers", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		handler := func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		}

		err := scheduler.RegisterHandler("handler1", handler)
		require.NoError(t, err)

		err = scheduler.RegisterHandler("handler2", handler)
		require.NoError(t, err)

		assert.Equal(t, 2, scheduler.HandlerCount())
	})
}

// TestUnregisterHandler tests handler unregistration.
func TestUnregisterHandler(t *testing.T) {
	t.Run("unregisters existing handler", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		handler := func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		}

		err := scheduler.RegisterHandler("test-handler", handler)
		require.NoError(t, err)
		assert.Equal(t, 1, scheduler.HandlerCount())

		err = scheduler.UnregisterHandler("test-handler")

		require.NoError(t, err)
		assert.Equal(t, 0, scheduler.HandlerCount())
	})

	t.Run("unregistering non-existent handler is safe", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		err := scheduler.UnregisterHandler("non-existent")

		require.NoError(t, err)
	})
}

// TestStartAndStop tests scheduler lifecycle.
func TestStartAndStop(t *testing.T) {
	t.Run("starts scheduler successfully", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.Start(ctx)

		require.NoError(t, err)
		assert.True(t, scheduler.IsRunning())

		// Clean up
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err = scheduler.Stop(stopCtx)
		require.NoError(t, err)
	})

	t.Run("returns error when starting already running scheduler", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)

		err = scheduler.Start(ctx)

		require.Error(t, err)
		var schedErr *SchedulerError
		assert.True(t, errors.As(err, &schedErr))
		assert.Equal(t, ErrCodeSchedulerStopped, schedErr.Code)

		// Clean up
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = scheduler.Stop(stopCtx)
	})

	t.Run("stops running scheduler successfully", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)

		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err = scheduler.Stop(stopCtx)

		require.NoError(t, err)
		assert.False(t, scheduler.IsRunning())
	})

	t.Run("returns error when stopping non-running scheduler", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err := scheduler.Stop(stopCtx)

		require.Error(t, err)
		var schedErr *SchedulerError
		assert.True(t, errors.As(err, &schedErr))
		assert.Equal(t, ErrCodeSchedulerStopped, schedErr.Code)
	})

	t.Run("cannot restart stopped scheduler", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)

		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err = scheduler.Stop(stopCtx)
		require.NoError(t, err)

		err = scheduler.Start(ctx)

		require.Error(t, err)
		var schedErr *SchedulerError
		assert.True(t, errors.As(err, &schedErr))
		assert.Contains(t, schedErr.Message, "cannot be restarted")
	})

	// Regression for go-quicbaas#62. A Stop that arrives BEFORE the matching
	// Start must still be terminal, or the ordering decides whether a poll
	// loop leaks: an owner that tears down between constructing an engine and
	// starting it -- which is exactly what Shutdown after a failed Start does
	// -- had its Stop rejected with "not running", and the Start that landed
	// afterwards brought the loop up against storage the owner had already
	// closed.
	//
	// Deterministic by construction: the two calls are made in this
	// goroutine, in the order the defect needs, so there is no race to lose.
	t.Run("stop before start is terminal", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()

		// Stop first. It still reports that there was nothing to stop...
		err := scheduler.Stop(stopCtx)
		require.Error(t, err, "stopping an engine that never started still reports nothing to stop")
		var stopErr *SchedulerError
		require.True(t, errors.As(err, &stopErr))
		assert.Equal(t, ErrCodeSchedulerStopped, stopErr.Code)

		// ...but the intent sticks: the late Start must refuse.
		err = scheduler.Start(ctx)

		require.Error(t, err, "a Start after a Stop must refuse, whatever order they arrived in")
		var startErr *SchedulerError
		require.True(t, errors.As(err, &startErr))
		assert.Contains(t, startErr.Message, "cannot be restarted")
		assert.False(t, scheduler.IsRunning(),
			"the refused Start must leave no poll loop running")
	})
}

// TestScheduleJob tests job scheduling.
func TestScheduleJob(t *testing.T) {
	t.Run("schedules one-shot job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "one-shot-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.NoError(t, err)
		assert.Equal(t, uint64(1), job.ID)
		assert.Equal(t, JobStatusPending, job.Status)
		assert.NotZero(t, job.CreatedAt)
		assert.NotZero(t, job.UpdatedAt)
		assert.Equal(t, int32(1), storage.createJobCount.Load())
	})

	t.Run("schedules recurring job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "recurring-job",
			Type:      JobTypeRecurring,
			Handler:   "test-handler",
			Schedule:  "*/5 * * * *", // Every 5 minutes
			OwnerType: OwnerTypeSystem,
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.NoError(t, err)
		assert.NotZero(t, job.NextRun)
	})

	t.Run("schedules delayed job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "delayed-job",
			Type:      JobTypeDelayed,
			Handler:   "test-handler",
			Delay:     time.Hour,
			OwnerType: OwnerTypeSystem,
		}

		beforeSchedule := time.Now()
		err = scheduler.ScheduleJob(ctx, job)

		require.NoError(t, err)
		assert.True(t, job.NextRun.After(beforeSchedule))
		assert.True(t, job.NextRun.Before(beforeSchedule.Add(time.Hour+time.Second)))
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
		}

		err := scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for nil job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.ScheduleJob(ctx, nil)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "job", invalidErr.Field)
	})

	t.Run("returns error for empty job name", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "name", invalidErr.Field)
	})

	t.Run("returns error for empty handler", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "",
			ExecuteAt: time.Now().Add(time.Hour),
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "handler", invalidErr.Field)
	})

	t.Run("allows scheduling with unregistered handler for late registration", func(t *testing.T) {
		// Handler existence is validated at execution time, not creation time.
		// This allows jobs to be created for handlers that may be registered later.
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "late-registered-handler",
			ExecuteAt: time.Now().Add(time.Hour),
		}

		// Should succeed - handler validation is deferred to execution time
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)
		assert.NotZero(t, job.ID)
	})

	t.Run("returns error for one-shot job without execute_at", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:    "test-job",
			Type:    JobTypeOneShot,
			Handler: "test-handler",
			// Missing ExecuteAt
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "execute_at", invalidErr.Field)
	})

	t.Run("returns error for recurring job without schedule", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:    "test-job",
			Type:    JobTypeRecurring,
			Handler: "test-handler",
			// Missing Schedule
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "schedule", invalidErr.Field)
	})

	t.Run("returns error for recurring job with invalid cron expression", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:     "test-job",
			Type:     JobTypeRecurring,
			Handler:  "test-handler",
			Schedule: "invalid cron expression",
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var invalidScheduleErr InvalidScheduleError
		assert.True(t, errors.As(err, &invalidScheduleErr))
	})

	t.Run("returns error for delayed job with zero delay", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:    "test-job",
			Type:    JobTypeDelayed,
			Handler: "test-handler",
			Delay:   0,
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "delay", invalidErr.Field)
	})

	t.Run("returns error for invalid job type", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:    "test-job",
			Type:    "invalid-type",
			Handler: "test-handler",
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Equal(t, "type", invalidErr.Field)
	})

	t.Run("returns error on storage failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.createJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeDelayed,
			Handler:   "test-handler",
			Delay:     time.Hour,
			OwnerType: OwnerTypeSystem,
		}

		err = scheduler.ScheduleJob(ctx, job)

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestCancelJob tests job cancellation.
func TestCancelJob(t *testing.T) {
	t.Run("cancels pending job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		err = scheduler.CancelJob(ctx, job.ID)

		require.NoError(t, err)
		canceledJob := storage.getJobDirect(job.ID)
		assert.Equal(t, JobStatusCanceled, canceledJob.Status)
	})

	t.Run("cancels paused job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		err = scheduler.PauseJob(ctx, job.ID, "")
		require.NoError(t, err)

		err = scheduler.CancelJob(ctx, job.ID)

		require.NoError(t, err)
		canceledJob := storage.getJobDirect(job.ID)
		assert.Equal(t, JobStatusCanceled, canceledJob.Status)
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.CancelJob(ctx, 1)

		require.Error(t, err)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.CancelJob(ctx, 999)

		require.Error(t, err)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error when canceling running job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Manually insert a running job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "running-job",
			Status: JobStatusRunning,
		}
		storage.mu.Unlock()

		err = scheduler.CancelJob(ctx, 1)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Contains(t, invalidErr.Message, "running job")
	})

	t.Run("returns error when canceling already canceled job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Manually insert a canceled job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "canceled-job",
			Status: JobStatusCanceled,
		}
		storage.mu.Unlock()

		err = scheduler.CancelJob(ctx, 1)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Contains(t, invalidErr.Message, "terminal state")
	})

	t.Run("returns error when canceling completed job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Manually insert a completed job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "completed-job",
			Status: JobStatusCompleted,
		}
		storage.mu.Unlock()

		err = scheduler.CancelJob(ctx, 1)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Contains(t, invalidErr.Message, "terminal state")
	})

	t.Run("returns error on storage get failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.CancelJob(ctx, 1)

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestTriggerJob tests immediate job triggering.
func TestTriggerJob(t *testing.T) {
	t.Run("triggers pending job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		err = scheduler.TriggerJob(ctx, job.ID, "")

		require.NoError(t, err)
	})

	t.Run("triggers paused job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		err = scheduler.PauseJob(ctx, job.ID, "")
		require.NoError(t, err)

		err = scheduler.TriggerJob(ctx, job.ID, "")

		require.NoError(t, err)
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.TriggerJob(ctx, 1, "")

		require.Error(t, err)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.TriggerJob(ctx, 999, "")

		require.Error(t, err)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error for running job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Manually insert a running job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "running-job",
			Status: JobStatusRunning,
		}
		storage.mu.Unlock()

		err = scheduler.TriggerJob(ctx, 1, "")

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
	})

	t.Run("returns error on storage failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.TriggerJob(ctx, 1, "")

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestPauseJob tests job pausing.
func TestPauseJob(t *testing.T) {
	t.Run("pauses pending job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		err = scheduler.PauseJob(ctx, job.ID, "")

		require.NoError(t, err)
		pausedJob := storage.getJobDirect(job.ID)
		assert.Equal(t, JobStatusPaused, pausedJob.Status)
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.PauseJob(ctx, 1, "")

		require.Error(t, err)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.PauseJob(ctx, 999, "")

		require.Error(t, err)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error for non-pending job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Manually insert a running job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "running-job",
			Status: JobStatusRunning,
		}
		storage.mu.Unlock()

		err = scheduler.PauseJob(ctx, 1, "")

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
	})

	t.Run("returns error on storage failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.PauseJob(ctx, 1, "")

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestResumeJob tests job resumption.
func TestResumeJob(t *testing.T) {
	t.Run("resumes paused job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		err = scheduler.PauseJob(ctx, job.ID, "")
		require.NoError(t, err)

		err = scheduler.ResumeJob(ctx, job.ID, "")

		require.NoError(t, err)
		resumedJob := storage.getJobDirect(job.ID)
		assert.Equal(t, JobStatusPending, resumedJob.Status)
	})

	t.Run("resumes job and recalculates next run if past", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Insert a paused job with next run in the past
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "paused-job",
			Type:      JobTypeDelayed,
			Handler:   "test-handler",
			Status:    JobStatusPaused,
			NextRun:   time.Now().Add(-time.Hour),
			Delay:     time.Minute,
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		beforeResume := time.Now()
		err = scheduler.ResumeJob(ctx, 1, "")

		require.NoError(t, err)
		resumedJob := storage.getJobDirect(1)
		assert.Equal(t, JobStatusPending, resumedJob.Status)
		assert.True(t, resumedJob.NextRun.After(beforeResume))
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.ResumeJob(ctx, 1, "")

		require.Error(t, err)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.ResumeJob(ctx, 999, "")

		require.Error(t, err)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error for non-paused job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Manually insert a pending job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "pending-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		err = scheduler.ResumeJob(ctx, 1, "")

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
	})

	t.Run("returns error on storage failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.ResumeJob(ctx, 1, "")

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestGetJob tests job retrieval.
func TestGetJob(t *testing.T) {
	t.Run("gets existing job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		// Manually insert a job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "test-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		job, err := scheduler.GetJob(ctx, 1)

		require.NoError(t, err)
		require.NotNil(t, job)
		assert.Equal(t, uint64(1), job.ID)
		assert.Equal(t, "test-job", job.Name)
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		job, err := scheduler.GetJob(ctx, 999)

		require.Error(t, err)
		assert.Nil(t, job)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error on storage failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		job, err := scheduler.GetJob(ctx, 1)

		require.Error(t, err)
		assert.Nil(t, job)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestListJobs tests job listing.
func TestListJobs(t *testing.T) {
	t.Run("lists all jobs successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		// Insert some jobs
		storage.mu.Lock()
		storage.jobs[1] = &Job{ID: 1, Name: "job1", Status: JobStatusPending}
		storage.jobs[2] = &Job{ID: 2, Name: "job2", Status: JobStatusRunning}
		storage.jobs[3] = &Job{ID: 3, Name: "job3", Status: JobStatusCompleted}
		storage.mu.Unlock()

		jobs, err := scheduler.ListJobs(ctx, &JobQuery{})

		require.NoError(t, err)
		assert.Len(t, jobs, 3)
	})

	t.Run("filters jobs by status", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		// Insert some jobs
		storage.mu.Lock()
		storage.jobs[1] = &Job{ID: 1, Name: "job1", Status: JobStatusPending}
		storage.jobs[2] = &Job{ID: 2, Name: "job2", Status: JobStatusRunning}
		storage.jobs[3] = &Job{ID: 3, Name: "job3", Status: JobStatusPending}
		storage.mu.Unlock()

		jobs, err := scheduler.ListJobs(ctx, &JobQuery{Status: JobStatusPending})

		require.NoError(t, err)
		assert.Len(t, jobs, 2)
		for _, job := range jobs {
			assert.Equal(t, JobStatusPending, job.Status)
		}
	})

	t.Run("returns error on storage failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.listJobsErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		jobs, err := scheduler.ListJobs(ctx, &JobQuery{})

		require.Error(t, err)
		assert.Nil(t, jobs)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestListJobsWithCursor tests paginated job listing.
func TestListJobsWithCursor(t *testing.T) {
	t.Run("lists jobs with cursor successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		// Insert some jobs
		storage.mu.Lock()
		storage.jobs[1] = &Job{ID: 1, Name: "job1", Status: JobStatusPending}
		storage.jobs[2] = &Job{ID: 2, Name: "job2", Status: JobStatusPending}
		storage.mu.Unlock()

		jobs, cursor, err := scheduler.ListJobsWithCursor(ctx, &JobQuery{Limit: 10})

		require.NoError(t, err)
		assert.Len(t, jobs, 2)
		assert.Empty(t, cursor) // Mock doesn't return cursor
	})

	t.Run("returns error on storage failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.listJobsErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		jobs, cursor, err := scheduler.ListJobsWithCursor(ctx, &JobQuery{})

		require.Error(t, err)
		assert.Nil(t, jobs)
		assert.Empty(t, cursor)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestStats tests scheduler statistics.
func TestStats(t *testing.T) {
	t.Run("returns stats for new scheduler", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		stats := scheduler.Stats()

		require.NotNil(t, stats)
		assert.Equal(t, uint64(0), stats.JobCount)
		assert.Equal(t, uint64(0), stats.RunningJobs)
		assert.Equal(t, uint64(0), stats.SuccessCount)
		assert.Equal(t, uint64(0), stats.FailureCount)
	})

	t.Run("returns stats after job scheduling", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeDelayed,
			Handler:   "test-handler",
			Delay:     time.Hour,
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		stats := scheduler.Stats()

		assert.Equal(t, uint64(1), stats.JobCount)
		assert.False(t, stats.StartedAt.IsZero())
	})
}

// TestIsRunning tests running state checking.
func TestIsRunning(t *testing.T) {
	t.Run("returns false for new scheduler", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		assert.False(t, scheduler.IsRunning())
	})

	t.Run("returns true after start", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		assert.True(t, scheduler.IsRunning())
	})

	t.Run("returns false after stop", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)

		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err = scheduler.Stop(stopCtx)
		require.NoError(t, err)

		assert.False(t, scheduler.IsRunning())
	})
}

// TestJobExecution tests actual job execution with the scheduler loop.
func TestJobExecution(t *testing.T) {
	t.Run("executes job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    3,
			RetryBackoff:      100 * time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		// Use channel to signal job completion
		jobExecuted := make(chan struct{})
		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			close(jobExecuted)
			return &JobResult{Success: true, Message: "executed"}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "exec-job",
			Type:      JobTypeDelayed,
			Handler:   "test-handler",
			Delay:     time.Millisecond, // Execute almost immediately
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		// Wait for job to execute
		select {
		case <-jobExecuted:
			// Success
		case <-time.After(2 * time.Second):
			t.Fatal("job did not execute within timeout")
		}

		// The stored record and the success counter are written separately
		// (persistJobOutcome, then successCount.Add), so wait until every
		// asserted value has landed. Poll on a ticker with a deadline rather
		// than spinning.
		settled := func() bool {
			updatedJob := storage.getJobDirect(job.ID)
			return updatedJob != nil &&
				updatedJob.RunCount > 0 &&
				updatedJob.Status == JobStatusCompleted &&
				scheduler.Stats().SuccessCount > 0
		}
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		for !settled() {
			select {
			case <-ticker.C:
			case <-deadline.C:
				t.Fatal("job outcome did not settle within timeout")
			}
		}

		updatedJob := storage.getJobDirect(job.ID)
		assert.Equal(t, JobStatusCompleted, updatedJob.Status)
		assert.Equal(t, uint64(1), updatedJob.RunCount)
		assert.Equal(t, uint64(1), scheduler.Stats().SuccessCount)
	})

	t.Run("handles job failure and retries", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    3,
			RetryBackoff:      10 * time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		attemptCount := atomic.Int32{}
		maxFailures := make(chan struct{})
		var closeOnce sync.Once

		err := scheduler.RegisterHandler("failing-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			count := attemptCount.Add(1)
			if count >= 3 {
				closeOnce.Do(func() { close(maxFailures) })
			}
			return nil, errors.New("intentional failure")
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:       "failing-job",
			Type:       JobTypeDelayed,
			Handler:    "failing-handler",
			Delay:      time.Millisecond,
			MaxRetries: 3,
			OwnerType:  OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		// Wait for retries to exhaust
		select {
		case <-maxFailures:
			// Success
		case <-time.After(5 * time.Second):
			t.Fatal("retries did not exhaust within timeout")
		}

		// Allow time for final status update
		waitDone := make(chan struct{})
		go func() {
			for {
				updatedJob := storage.getJobDirect(job.ID)
				if updatedJob != nil && updatedJob.Status == JobStatusFailed {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			updatedJob := storage.getJobDirect(job.ID)
			assert.Equal(t, JobStatusFailed, updatedJob.Status)
			assert.True(t, updatedJob.FailCount >= 3)
			assert.NotEmpty(t, updatedJob.LastError)
		case <-time.After(2 * time.Second):
			t.Fatal("job status did not update to failed within timeout")
		}
	})

	t.Run("handler not found fails job", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Insert a job with unregistered handler directly
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "orphan-job",
			Type:      JobTypeDelayed,
			Handler:   "non-existent-handler",
			Status:    JobStatusPending,
			NextRun:   time.Now().Add(-time.Second),
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Wait for scheduler to pick it up and fail it
		waitDone := make(chan struct{})
		go func() {
			for {
				updatedJob := storage.getJobDirect(1)
				if updatedJob != nil && updatedJob.Status == JobStatusFailed {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			updatedJob := storage.getJobDirect(1)
			assert.Equal(t, JobStatusFailed, updatedJob.Status)
			assert.Contains(t, updatedJob.LastError, "handler not found")
		case <-time.After(2 * time.Second):
			t.Fatal("job with missing handler did not fail within timeout")
		}
	})
}

// TestConcurrentJobExecution tests concurrent job handling.
func TestConcurrentJobExecution(t *testing.T) {
	t.Run("respects max concurrent jobs limit", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 2, // Only allow 2 concurrent
			DefaultTimeout:    5 * time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		currentRunning := atomic.Int32{}
		maxRunning := atomic.Int32{}
		allStarted := make(chan struct{})
		releaseJobs := make(chan struct{})
		jobsStarted := atomic.Int32{}
		var closeOnce sync.Once

		err := scheduler.RegisterHandler("concurrent-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			running := currentRunning.Add(1)
			defer currentRunning.Add(-1)

			// Track max concurrent
			for {
				current := maxRunning.Load()
				if running <= current || maxRunning.CompareAndSwap(current, running) {
					break
				}
			}

			if jobsStarted.Add(1) == 2 { // Signal when 2 jobs have started
				closeOnce.Do(func() { close(allStarted) })
			}

			<-releaseJobs
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			close(releaseJobs)
			stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Schedule 4 jobs
		for i := 0; i < 4; i++ {
			job := &Job{
				Name:      "concurrent-job",
				Type:      JobTypeDelayed,
				Handler:   "concurrent-handler",
				Delay:     time.Millisecond,
				OwnerType: OwnerTypeSystem,
			}
			err = scheduler.ScheduleJob(ctx, job)
			require.NoError(t, err)
		}

		// Wait for 2 jobs to start (max concurrent)
		select {
		case <-allStarted:
			// Check that max concurrent is 2
			assert.LessOrEqual(t, maxRunning.Load(), int32(2))
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent jobs did not start within timeout")
		}
	})
}

// TestStorageErrorHandling tests storage error handling.
func TestStorageErrorHandling(t *testing.T) {
	t.Run("tracks storage errors during due job fetch", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobsDueForExecutionErr = errors.New("storage error")
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Wait for a tick to occur
		waitDone := make(chan struct{})
		go func() {
			for {
				if scheduler.StorageErrorCount() > 0 {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			assert.Greater(t, scheduler.StorageErrorCount(), uint64(0))
		case <-time.After(time.Second):
			t.Fatal("storage error was not tracked")
		}
	})
}

// TestHandlerCount tests handler counting.
func TestHandlerCount(t *testing.T) {
	t.Run("returns correct handler count", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		assert.Equal(t, 0, scheduler.HandlerCount())

		err := scheduler.RegisterHandler("handler1", func(ctx context.Context, job *Job) (*JobResult, error) {
			return nil, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, scheduler.HandlerCount())

		err = scheduler.RegisterHandler("handler2", func(ctx context.Context, job *Job) (*JobResult, error) {
			return nil, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 2, scheduler.HandlerCount())

		err = scheduler.UnregisterHandler("handler1")
		require.NoError(t, err)
		assert.Equal(t, 1, scheduler.HandlerCount())
	})
}

// TestConfig tests configuration access.
func TestConfig(t *testing.T) {
	t.Run("returns provided config", func(t *testing.T) {
		config := &SchedulerConfig{
			TickInterval:      500 * time.Millisecond,
			MaxConcurrentJobs: 5,
			DefaultTimeout:    time.Minute,
			DefaultRetries:    2,
			RetryBackoff:      time.Second,
		}
		scheduler := New(config, newMockJobStorage())

		assert.Equal(t, config, scheduler.Config())
	})

	t.Run("returns default config when nil provided", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		config := scheduler.Config()
		require.NotNil(t, config)
		assert.Equal(t, DefaultConfig().TickInterval, config.TickInterval)
	})
}

// TestJobStateTransitions tests complete job lifecycle.
func TestJobStateTransitions(t *testing.T) {
	t.Run("pending to paused to resumed to canceled", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("state-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Schedule job far in future so it doesn't execute
		job := &Job{
			Name:      "state-job",
			Type:      JobTypeOneShot,
			Handler:   "state-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)
		assert.Equal(t, JobStatusPending, job.Status)

		// Pause the job
		err = scheduler.PauseJob(ctx, job.ID, "")
		require.NoError(t, err)
		pausedJob := storage.getJobDirect(job.ID)
		assert.Equal(t, JobStatusPaused, pausedJob.Status)

		// Resume the job
		err = scheduler.ResumeJob(ctx, job.ID, "")
		require.NoError(t, err)
		resumedJob := storage.getJobDirect(job.ID)
		assert.Equal(t, JobStatusPending, resumedJob.Status)

		// Cancel the job
		err = scheduler.CancelJob(ctx, job.ID)
		require.NoError(t, err)
		canceledJob := storage.getJobDirect(job.ID)
		assert.Equal(t, JobStatusCanceled, canceledJob.Status)
	})
}

// TestTriggerJobExecution tests the triggerJobByID internal method through TriggerJob API.
func TestTriggerJobExecution(t *testing.T) {
	t.Run("triggered job executes via trigger channel", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      time.Hour, // Long interval so tick doesn't pick it up
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		jobExecuted := make(chan struct{})
		err := scheduler.RegisterHandler("trigger-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			close(jobExecuted)
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Schedule job far in future
		job := &Job{
			Name:      "trigger-job",
			Type:      JobTypeOneShot,
			Handler:   "trigger-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		// Trigger immediate execution
		err = scheduler.TriggerJob(ctx, job.ID, "")
		require.NoError(t, err)

		// Wait for job to execute
		select {
		case <-jobExecuted:
			// Success - job was triggered
		case <-time.After(2 * time.Second):
			t.Fatal("triggered job did not execute")
		}
	})
}

// TestJobResultMessage tests that job result message is stored.
func TestJobResultMessage(t *testing.T) {
	t.Run("stores job result message in execution", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		jobDone := make(chan struct{})
		err := scheduler.RegisterHandler("msg-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			close(jobDone)
			return &JobResult{Success: true, Message: "job completed successfully"}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "msg-job",
			Type:      JobTypeDelayed,
			Handler:   "msg-handler",
			Delay:     time.Millisecond,
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		select {
		case <-jobDone:
		case <-time.After(2 * time.Second):
			t.Fatal("job did not execute")
		}

		// Wait for execution to be saved
		waitDone := make(chan struct{})
		go func() {
			for {
				storage.mu.RLock()
				hasExec := len(storage.executions) > 0
				storage.mu.RUnlock()
				if hasExec {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			storage.mu.RLock()
			require.Len(t, storage.executions, 1)
			assert.Equal(t, "job completed successfully", storage.executions[0].Output)
			storage.mu.RUnlock()
		case <-time.After(2 * time.Second):
			t.Fatal("execution was not saved")
		}
	})
}

// TestSchedulerDefaultConfig tests the DefaultConfig function.
func TestSchedulerDefaultConfig(t *testing.T) {
	t.Run("returns valid default config", func(t *testing.T) {
		config := DefaultConfig()

		require.NotNil(t, config)
		assert.Equal(t, time.Second, config.TickInterval)
		assert.Equal(t, 10, config.MaxConcurrentJobs)
		assert.Equal(t, 5*time.Minute, config.DefaultTimeout)
		assert.Equal(t, 3, config.DefaultRetries)
		assert.Equal(t, 5*time.Second, config.RetryBackoff)
	})
}

// TestCancelJobUpdateStorageError tests storage error handling during cancel.
func TestCancelJobUpdateStorageError(t *testing.T) {
	t.Run("returns error on update failure during cancel", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		// Set update error
		storage.updateJobErr = errors.New("update failed")

		err = scheduler.CancelJob(ctx, job.ID)

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestPauseJobUpdateStorageError tests storage error handling during pause.
func TestPauseJobUpdateStorageError(t *testing.T) {
	t.Run("returns error on update failure during pause", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		// Set update error
		storage.updateJobErr = errors.New("update failed")

		err = scheduler.PauseJob(ctx, job.ID, "")

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestResumeJobUpdateStorageError tests storage error handling during resume.
func TestResumeJobUpdateStorageError(t *testing.T) {
	t.Run("returns error on update failure during resume", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Insert a paused job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "paused-job",
			Type:      JobTypeDelayed,
			Handler:   "test-handler",
			Status:    JobStatusPaused,
			NextRun:   time.Now().Add(time.Hour),
			Delay:     time.Minute,
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Set update error
		storage.updateJobErr = errors.New("update failed")

		err = scheduler.ResumeJob(ctx, 1, "")

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestStorageErrorCount tests the StorageErrorCount method.
func TestStorageErrorCount(t *testing.T) {
	t.Run("returns zero for new scheduler", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		assert.Equal(t, uint64(0), scheduler.StorageErrorCount())
	})
}

// TestRecurringJobRescheduling tests that recurring jobs reschedule correctly.
func TestRecurringJobRescheduling(t *testing.T) {
	t.Run("recurring job reschedules after execution", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		jobExecuted := make(chan struct{})
		executionCount := atomic.Int32{}
		var closeOnce sync.Once

		err := scheduler.RegisterHandler("recurring-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			if executionCount.Add(1) >= 1 {
				closeOnce.Do(func() { close(jobExecuted) })
			}
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job := &Job{
			Name:      "recurring-job",
			Type:      JobTypeRecurring,
			Handler:   "recurring-handler",
			Schedule:  "* * * * *", // Every minute
			OwnerType: OwnerTypeSystem,
		}
		err = scheduler.ScheduleJob(ctx, job)
		require.NoError(t, err)

		// Modify the next run to trigger immediate execution
		storage.mu.Lock()
		storage.jobs[job.ID].NextRun = time.Now().Add(-time.Second)
		storage.mu.Unlock()

		// Wait for execution
		select {
		case <-jobExecuted:
			// Success
		case <-time.After(2 * time.Second):
			t.Fatal("recurring job did not execute")
		}

		// Wait for status update to pending (rescheduled)
		waitDone := make(chan struct{})
		go func() {
			for {
				updatedJob := storage.getJobDirect(job.ID)
				if updatedJob != nil && updatedJob.RunCount > 0 && updatedJob.Status == JobStatusPending {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			updatedJob := storage.getJobDirect(job.ID)
			assert.Equal(t, JobStatusPending, updatedJob.Status)
			assert.True(t, updatedJob.NextRun.After(updatedJob.LastRun))
		case <-time.After(2 * time.Second):
			t.Fatal("recurring job did not reschedule within timeout")
		}
	})
}

// TestCancelJobWithTenant tests job cancellation with tenant context.
func TestCancelJobWithTenant(t *testing.T) {
	t.Run("cancels pending job with tenant successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "tenant-job",
			Status:  JobStatusPending,
			OwnerID: "tenant-123",
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.CancelJobWithTenant(ctx, 1, "tenant-123", "user requested cancellation")

		require.NoError(t, err)
		assert.Equal(t, JobStatusPending, oldStatus)
		canceledJob := storage.getJobDirect(1)
		assert.Equal(t, JobStatusCanceled, canceledJob.Status)
		assert.Equal(t, "user requested cancellation", canceledJob.LastError)
	})

	t.Run("cancels paused job with tenant successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "paused-tenant-job",
			Status:  JobStatusPaused,
			OwnerID: "tenant-123",
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.CancelJobWithTenant(ctx, 1, "tenant-123", "")

		require.NoError(t, err)
		assert.Equal(t, JobStatusPaused, oldStatus)
		canceledJob := storage.getJobDirect(1)
		assert.Equal(t, JobStatusCanceled, canceledJob.Status)
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		status, err := scheduler.CancelJobWithTenant(ctx, 1, "tenant-123", "")

		require.Error(t, err)
		assert.Empty(t, status)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		status, err := scheduler.CancelJobWithTenant(ctx, 999, "tenant-123", "")

		require.Error(t, err)
		assert.Empty(t, status)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error when canceling running job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "running-job",
			Status: JobStatusRunning,
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.CancelJobWithTenant(ctx, 1, "", "")

		require.Error(t, err)
		assert.Equal(t, JobStatusRunning, oldStatus)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
	})

	t.Run("returns error when canceling already canceled job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "canceled-job",
			Status: JobStatusCanceled,
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.CancelJobWithTenant(ctx, 1, "", "")

		require.Error(t, err)
		assert.Equal(t, JobStatusCanceled, oldStatus)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
	})

	t.Run("returns error on storage get failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		status, err := scheduler.CancelJobWithTenant(ctx, 1, "", "")

		require.Error(t, err)
		assert.Empty(t, status)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})

	t.Run("returns error on storage update failure", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "pending-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		storage.updateJobErr = errors.New("update failed")

		oldStatus, err := scheduler.CancelJobWithTenant(ctx, 1, "", "")

		require.Error(t, err)
		assert.Equal(t, JobStatusPending, oldStatus)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestPauseJobWithStatus tests job pausing with status return.
func TestPauseJobWithStatus(t *testing.T) {
	t.Run("pauses pending job and returns old status", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "pending-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.PauseJobWithStatus(ctx, 1, "")

		require.NoError(t, err)
		assert.Equal(t, JobStatusPending, oldStatus)
		pausedJob := storage.getJobDirect(1)
		assert.Equal(t, JobStatusPaused, pausedJob.Status)
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		status, err := scheduler.PauseJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Empty(t, status)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		status, err := scheduler.PauseJobWithStatus(ctx, 999, "")

		require.Error(t, err)
		assert.Empty(t, status)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error for non-pending job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "running-job",
			Status: JobStatusRunning,
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.PauseJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Equal(t, JobStatusRunning, oldStatus)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
	})

	t.Run("returns error on storage get failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		status, err := scheduler.PauseJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Empty(t, status)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})

	t.Run("returns error on storage update failure", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "pending-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		storage.updateJobErr = errors.New("update failed")

		oldStatus, err := scheduler.PauseJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Equal(t, JobStatusPending, oldStatus)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestResumeJobWithStatus tests job resumption with status return.
func TestResumeJobWithStatus(t *testing.T) {
	t.Run("resumes paused job and returns old status", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "paused-job",
			Type:    JobTypeDelayed,
			Handler: "test-handler",
			Status:  JobStatusPaused,
			NextRun: time.Now().Add(time.Hour),
			Delay:   time.Minute,
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.ResumeJobWithStatus(ctx, 1, "")

		require.NoError(t, err)
		assert.Equal(t, JobStatusPaused, oldStatus)
		resumedJob := storage.getJobDirect(1)
		assert.Equal(t, JobStatusPending, resumedJob.Status)
	})

	t.Run("resumes paused job with past next run and recalculates", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "paused-job",
			Type:    JobTypeDelayed,
			Handler: "test-handler",
			Status:  JobStatusPaused,
			NextRun: time.Now().Add(-time.Hour),
			Delay:   time.Minute,
		}
		storage.mu.Unlock()

		beforeResume := time.Now()
		oldStatus, err := scheduler.ResumeJobWithStatus(ctx, 1, "")

		require.NoError(t, err)
		assert.Equal(t, JobStatusPaused, oldStatus)
		resumedJob := storage.getJobDirect(1)
		assert.Equal(t, JobStatusPending, resumedJob.Status)
		assert.True(t, resumedJob.NextRun.After(beforeResume))
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		status, err := scheduler.ResumeJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Empty(t, status)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		status, err := scheduler.ResumeJobWithStatus(ctx, 999, "")

		require.Error(t, err)
		assert.Empty(t, status)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error for non-paused job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "pending-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.ResumeJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Equal(t, JobStatusPending, oldStatus)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
	})

	t.Run("returns error on storage get failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		status, err := scheduler.ResumeJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Empty(t, status)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})

	t.Run("returns error on storage update failure", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "paused-job",
			Type:    JobTypeDelayed,
			Handler: "test-handler",
			Status:  JobStatusPaused,
			NextRun: time.Now().Add(time.Hour),
			Delay:   time.Minute,
		}
		storage.mu.Unlock()

		storage.updateJobErr = errors.New("update failed")

		oldStatus, err := scheduler.ResumeJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Equal(t, JobStatusPaused, oldStatus)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})

	t.Run("returns error when recalculating next run fails for recurring job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:       1,
			Name:     "paused-recurring-job",
			Type:     JobTypeRecurring,
			Handler:  "test-handler",
			Status:   JobStatusPaused,
			NextRun:  time.Now().Add(-time.Hour),
			Schedule: "invalid cron",
		}
		storage.mu.Unlock()

		oldStatus, err := scheduler.ResumeJobWithStatus(ctx, 1, "")

		require.Error(t, err)
		assert.Equal(t, JobStatusPaused, oldStatus)
		var invalidScheduleErr InvalidScheduleError
		assert.True(t, errors.As(err, &invalidScheduleErr))
	})
}

// TestGetJobWithTenant tests job retrieval with tenant context.
func TestGetJobWithTenant(t *testing.T) {
	t.Run("gets existing job with tenant successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "tenant-job",
			Status:  JobStatusPending,
			OwnerID: "tenant-123",
		}
		storage.mu.Unlock()

		job, err := scheduler.GetJobWithTenant(ctx, 1, "tenant-123")

		require.NoError(t, err)
		require.NotNil(t, job)
		assert.Equal(t, uint64(1), job.ID)
		assert.Equal(t, "tenant-job", job.Name)
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		job, err := scheduler.GetJobWithTenant(ctx, 999, "tenant-123")

		require.Error(t, err)
		assert.Nil(t, job)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error on storage failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		job, err := scheduler.GetJobWithTenant(ctx, 1, "tenant-123")

		require.Error(t, err)
		assert.Nil(t, job)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestUpdateJob tests job update functionality.
func TestUpdateJob(t *testing.T) {
	t.Run("updates job name successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "original-name",
			Type:    JobTypeDelayed,
			Handler: "test-handler",
			Status:  JobStatusPending,
		}
		storage.mu.Unlock()

		updatedJob, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			Name: "updated-name",
		})

		require.NoError(t, err)
		require.NotNil(t, updatedJob)
		assert.Equal(t, "updated-name", updatedJob.Name)
	})

	t.Run("updates job description successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:          1,
			Name:        "test-job",
			Description: "old description",
			Type:        JobTypeDelayed,
			Handler:     "test-handler",
			Status:      JobStatusPending,
		}
		storage.mu.Unlock()

		updatedJob, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			Description: "new description",
		})

		require.NoError(t, err)
		require.NotNil(t, updatedJob)
		assert.Equal(t, "new description", updatedJob.Description)
	})

	t.Run("updates recurring job schedule and recalculates next run", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:       1,
			Name:     "recurring-job",
			Type:     JobTypeRecurring,
			Handler:  "test-handler",
			Schedule: "*/5 * * * *",
			Status:   JobStatusPending,
		}
		storage.mu.Unlock()

		updatedJob, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			Schedule: "*/10 * * * *",
		})

		require.NoError(t, err)
		require.NotNil(t, updatedJob)
		assert.Equal(t, "*/10 * * * *", updatedJob.Schedule)
		assert.False(t, updatedJob.NextRun.IsZero())
	})

	t.Run("updates one-shot job execute_at and next_run", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		newExecuteAt := time.Now().Add(2 * time.Hour)
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "one-shot-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			ExecuteAt: time.Now().Add(time.Hour),
			Status:    JobStatusPending,
		}
		storage.mu.Unlock()

		updatedJob, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			ExecuteAt: newExecuteAt,
		})

		require.NoError(t, err)
		require.NotNil(t, updatedJob)
		assert.Equal(t, newExecuteAt, updatedJob.ExecuteAt)
		assert.Equal(t, newExecuteAt, updatedJob.NextRun)
	})

	t.Run("updates job handler", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "test-job",
			Type:    JobTypeDelayed,
			Handler: "old-handler",
			Status:  JobStatusPending,
		}
		storage.mu.Unlock()

		updatedJob, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			Handler: "new-handler",
		})

		require.NoError(t, err)
		require.NotNil(t, updatedJob)
		assert.Equal(t, "new-handler", updatedJob.Handler)
	})

	t.Run("updates job payload", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "test-job",
			Type:    JobTypeDelayed,
			Handler: "test-handler",
			Payload: json.RawMessage(`{"old": "data"}`),
			Status:  JobStatusPending,
		}
		storage.mu.Unlock()

		newPayload := []byte(`{"new": "data"}`)
		updatedJob, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			Payload: newPayload,
		})

		require.NoError(t, err)
		require.NotNil(t, updatedJob)
		assert.Equal(t, json.RawMessage(newPayload), updatedJob.Payload)
	})

	t.Run("updates job priority timeout and max_retries", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:         1,
			Name:       "test-job",
			Type:       JobTypeDelayed,
			Handler:    "test-handler",
			Priority:   1,
			Timeout:    time.Minute,
			MaxRetries: 3,
			Status:     JobStatusPending,
		}
		storage.mu.Unlock()

		updatedJob, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			Priority:   5,
			Timeout:    2 * time.Minute,
			MaxRetries: 5,
		})

		require.NoError(t, err)
		require.NotNil(t, updatedJob)
		assert.Equal(t, int32(5), updatedJob.Priority)
		assert.Equal(t, 2*time.Minute, updatedJob.Timeout)
		assert.Equal(t, int32(5), updatedJob.MaxRetries)
	})

	t.Run("updates job labels", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "test-job",
			Type:    JobTypeDelayed,
			Handler: "test-handler",
			Labels:  map[string]string{"env": "dev"},
			Status:  JobStatusPending,
		}
		storage.mu.Unlock()

		newLabels := map[string]string{"env": "prod", "tier": "premium"}
		updatedJob, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			Labels: newLabels,
		})

		require.NoError(t, err)
		require.NotNil(t, updatedJob)
		assert.Equal(t, newLabels, updatedJob.Labels)
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		job, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{Name: "new-name"})

		require.Error(t, err)
		assert.Nil(t, job)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job, err := scheduler.UpdateJob(ctx, 999, "", &JobUpdateParams{Name: "new-name"})

		require.Error(t, err)
		assert.Nil(t, job)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error for invalid schedule", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:       1,
			Name:     "recurring-job",
			Type:     JobTypeRecurring,
			Handler:  "test-handler",
			Schedule: "*/5 * * * *",
			Status:   JobStatusPending,
		}
		storage.mu.Unlock()

		job, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{
			Schedule: "invalid cron",
		})

		require.Error(t, err)
		assert.Nil(t, job)
		var invalidScheduleErr InvalidScheduleError
		assert.True(t, errors.As(err, &invalidScheduleErr))
	})

	t.Run("returns error on storage get failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		job, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{Name: "new-name"})

		require.Error(t, err)
		assert.Nil(t, job)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})

	t.Run("returns error on storage update failure", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:      1,
			Name:    "test-job",
			Type:    JobTypeDelayed,
			Handler: "test-handler",
			Status:  JobStatusPending,
		}
		storage.mu.Unlock()

		storage.updateJobErr = errors.New("update failed")

		job, err := scheduler.UpdateJob(ctx, 1, "", &JobUpdateParams{Name: "new-name"})

		require.Error(t, err)
		assert.Nil(t, job)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// TestDeleteJob tests job deletion functionality.
func TestDeleteJob(t *testing.T) {
	t.Run("deletes pending job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "pending-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		scheduler.jobCount.Store(1)

		err = scheduler.DeleteJob(ctx, 1, "", false)

		require.NoError(t, err)
		assert.Nil(t, storage.getJobDirect(1))
		assert.Equal(t, uint64(0), scheduler.jobCount.Load())
	})

	t.Run("deletes completed job successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "completed-job",
			Status: JobStatusCompleted,
		}
		storage.mu.Unlock()

		err = scheduler.DeleteJob(ctx, 1, "", false)

		require.NoError(t, err)
		assert.Nil(t, storage.getJobDirect(1))
	})

	t.Run("returns error when deleting running job without force", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "running-job",
			Status: JobStatusRunning,
		}
		storage.mu.Unlock()

		err = scheduler.DeleteJob(ctx, 1, "", false)

		require.Error(t, err)
		var invalidErr InvalidJobError
		assert.True(t, errors.As(err, &invalidErr))
		assert.Contains(t, invalidErr.Message, "force flag")
	})

	t.Run("deletes running job with force flag", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "running-job",
			Status: JobStatusRunning,
		}
		storage.mu.Unlock()

		err = scheduler.DeleteJob(ctx, 1, "", true)

		require.NoError(t, err)
		assert.Nil(t, storage.getJobDirect(1))
	})

	t.Run("returns error when scheduler not running", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())
		ctx := context.Background()

		err := scheduler.DeleteJob(ctx, 1, "", false)

		require.Error(t, err)
		var stoppedErr SchedulerStoppedError
		assert.True(t, errors.As(err, &stoppedErr))
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.DeleteJob(ctx, 999, "", false)

		require.Error(t, err)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error on storage get failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		err = scheduler.DeleteJob(ctx, 1, "", false)

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})

	t.Run("returns error on storage delete failure", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "pending-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		storage.deleteJobErr = errors.New("delete failed")

		err = scheduler.DeleteJob(ctx, 1, "", false)

		require.Error(t, err)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})

	t.Run("handles job count already at zero", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "pending-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		// Job count is already 0
		scheduler.jobCount.Store(0)

		err = scheduler.DeleteJob(ctx, 1, "", false)

		require.NoError(t, err)
		assert.Equal(t, uint64(0), scheduler.jobCount.Load())
	})
}

// TestGetJobHistory tests job history retrieval.
func TestGetJobHistory(t *testing.T) {
	t.Run("gets job history successfully", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "test-job",
			Status: JobStatusCompleted,
		}
		storage.executions = append(storage.executions, &JobExecution{
			ID:     1,
			JobID:  1,
			Status: JobStatusCompleted,
		})
		storage.executions = append(storage.executions, &JobExecution{
			ID:     2,
			JobID:  1,
			Status: JobStatusCompleted,
		})
		storage.mu.Unlock()

		executions, err := scheduler.GetJobHistory(ctx, 1, "", 10)

		require.NoError(t, err)
		assert.Len(t, executions, 2)
	})

	t.Run("returns empty list when no executions", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "test-job",
			Status: JobStatusPending,
		}
		storage.mu.Unlock()

		executions, err := scheduler.GetJobHistory(ctx, 1, "", 10)

		require.NoError(t, err)
		assert.Empty(t, executions)
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		executions, err := scheduler.GetJobHistory(ctx, 999, "", 10)

		require.Error(t, err)
		assert.Nil(t, executions)
		var notFoundError JobNotFoundError
		assert.True(t, errors.As(err, &notFoundError))
	})

	t.Run("returns error on storage get job failure", func(t *testing.T) {
		storage := newMockJobStorage()
		storage.getJobErr = errors.New("storage failure")
		scheduler := New(nil, storage)
		ctx := context.Background()

		executions, err := scheduler.GetJobHistory(ctx, 1, "", 10)

		require.Error(t, err)
		assert.Nil(t, executions)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})

	t.Run("returns error on storage get executions failure", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "test-job",
			Status: JobStatusCompleted,
		}
		storage.mu.Unlock()

		storage.getExecutionsErr = errors.New("executions failure")

		executions, err := scheduler.GetJobHistory(ctx, 1, "", 10)

		require.Error(t, err)
		assert.Nil(t, executions)
		var storageErr ErrStorageError
		assert.True(t, errors.As(err, &storageErr))
	})
}

// mockLogger implements Logger interface for testing.
type mockLogger struct {
	debugMessages []string
	warnMessages  []string
	errorMessages []string
	mu            sync.Mutex
}

func newMockLogger() *mockLogger {
	return &mockLogger{
		debugMessages: make([]string, 0),
		warnMessages:  make([]string, 0),
		errorMessages: make([]string, 0),
	}
}

func (l *mockLogger) Debugf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debugMessages = append(l.debugMessages, format)
}

func (l *mockLogger) Warnf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnMessages = append(l.warnMessages, format)
}

func (l *mockLogger) Errorf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errorMessages = append(l.errorMessages, format)
}

// TestLogging tests the logging functions.
func TestLogging(t *testing.T) {
	t.Run("logDebug logs when logger configured", func(t *testing.T) {
		logger := newMockLogger()
		config := &SchedulerConfig{
			TickInterval:      time.Second,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Minute,
			DefaultRetries:    3,
			RetryBackoff:      time.Second,
			Logger:            logger,
		}
		scheduler := New(config, newMockJobStorage())

		scheduler.logDebug("test debug message")

		logger.mu.Lock()
		defer logger.mu.Unlock()
		assert.Len(t, logger.debugMessages, 1)
		assert.Equal(t, "test debug message", logger.debugMessages[0])
	})

	t.Run("logDebug does nothing when logger nil", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		// Should not panic
		scheduler.logDebug("test debug message")
	})

	t.Run("logWarn logs when logger configured", func(t *testing.T) {
		logger := newMockLogger()
		config := &SchedulerConfig{
			TickInterval:      time.Second,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Minute,
			DefaultRetries:    3,
			RetryBackoff:      time.Second,
			Logger:            logger,
		}
		scheduler := New(config, newMockJobStorage())

		scheduler.logWarn("test warning message")

		logger.mu.Lock()
		defer logger.mu.Unlock()
		assert.Len(t, logger.warnMessages, 1)
		assert.Equal(t, "test warning message", logger.warnMessages[0])
	})

	t.Run("logWarn does nothing when logger nil", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		// Should not panic
		scheduler.logWarn("test warning message")
	})

	t.Run("logError logs when logger configured", func(t *testing.T) {
		logger := newMockLogger()
		config := &SchedulerConfig{
			TickInterval:      time.Second,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Minute,
			DefaultRetries:    3,
			RetryBackoff:      time.Second,
			Logger:            logger,
		}
		scheduler := New(config, newMockJobStorage())

		scheduler.logError("test error message")

		logger.mu.Lock()
		defer logger.mu.Unlock()
		assert.Len(t, logger.errorMessages, 1)
		assert.Equal(t, "test error message", logger.errorMessages[0])
	})

	t.Run("logError does nothing when logger nil", func(t *testing.T) {
		scheduler := New(nil, newMockJobStorage())

		// Should not panic
		scheduler.logError("test error message")
	})
}

// TestJobTimeout tests job timeout handling.
func TestJobTimeout(t *testing.T) {
	timeoutCase := func(t *testing.T, handler JobHandler) *Job {
		t.Helper()

		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		require.NoError(t, scheduler.RegisterHandler("timeout-handler", handler))

		job := &Job{
			ID:        1,
			Name:      "timeout-job",
			Type:      JobTypeDelayed,
			Handler:   "timeout-handler",
			Status:    JobStatusPending,
			NextRun:   time.Now().Add(-time.Second),
			Timeout:   10 * time.Millisecond,
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Lock()
		storage.jobs[job.ID] = copyJob(job)
		storage.mu.Unlock()

		scheduler.executeJob(context.Background(), job, time.Time{})

		stored := storedJob(t, storage, job.ID)
		assert.Equal(t, JobStatusFailed, stored.Status)
		assert.Contains(t, stored.LastError, "timed out")
		return stored
	}

	t.Run("job timeout triggers failure", func(t *testing.T) {
		timeoutCase(t, func(ctx context.Context, job *Job) (*JobResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
	})

	t.Run("handler ctx error at timeout preserves timeout semantics", func(t *testing.T) {
		stored := timeoutCase(t, func(ctx context.Context, job *Job) (*JobResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
		assert.NotContains(t, stored.LastError, context.DeadlineExceeded.Error())
	})

	t.Run("handler nil at deadline still times out", func(t *testing.T) {
		timeoutCase(t, func(ctx context.Context, job *Job) (*JobResult, error) {
			<-ctx.Done()
			return &JobResult{Success: true}, nil
		})
	})
}

// TestStopWithContextTimeout tests stop with context timeout.
func TestStopWithContextTimeout(t *testing.T) {
	t.Run("stop returns context error when jobs take too long", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    10 * time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		handlerStarted := make(chan struct{})
		handlerRelease := make(chan struct{})
		err := scheduler.RegisterHandler("blocking-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			close(handlerStarted)
			<-handlerRelease
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)

		// Schedule a job that will block
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "blocking-job",
			Type:      JobTypeDelayed,
			Handler:   "blocking-handler",
			Status:    JobStatusPending,
			NextRun:   time.Now().Add(-time.Second),
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Wait for handler to start
		select {
		case <-handlerStarted:
		case <-time.After(2 * time.Second):
			close(handlerRelease)
			t.Fatal("handler did not start")
		}

		// Stop with very short timeout
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		err = scheduler.Stop(stopCtx)

		// Stop may or may not return an error depending on timing
		// Either nil (graceful) or DeadlineExceeded is acceptable
		if err != nil {
			assert.Equal(t, context.DeadlineExceeded, err)
		}

		// Release the handler so it can complete
		close(handlerRelease)
	})
}

// TestTriggerJobContextCancellation tests trigger channel behavior with context cancellation.
func TestTriggerJobContextCancellation(t *testing.T) {
	t.Run("trigger job returns context error when cancelled", func(t *testing.T) {
		storage := newMockJobStorage()
		scheduler := New(nil, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:     1,
			Name:   "test-job",
			Status: JobStatusPending,
			// NOT due, so the scheduler's own tick leaves it alone.
			//
			// The mock's GetJobsDueForExecution selects on
			// `Status == Pending && !NextRun.After(before)`, and a zero NextRun
			// is not after now — so a job written with no NextRun is DUE
			// IMMEDIATELY. New(nil, storage) ticks every second (types.go
			// DefaultConfig), so a runner that stalls this goroutine past one
			// tick lets the scheduler execute job 1 on its own, move it off
			// JobStatusPending, and TriggerJob's status gate then returns
			// InvalidJobError — the same failure the filler ids below caused by
			// a faster route. Only TriggerJob is under test here and it does
			// not consult NextRun, so putting the job beyond the tick's reach
			// costs the test nothing.
			NextRun: time.Now().Add(time.Hour),
		}
		storage.mu.Unlock()

		// Create a context that's already cancelled
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()

		// Pressure the trigger channel with ids that CANNOT be the job under
		// test.
		//
		// This loop used to start at 0, so it enqueued a trigger for job 1 —
		// the job this test then triggers. The scheduler is running and drains
		// the channel concurrently, so it would execute job 1 and move it off
		// JobStatusPending; TriggerJob's status gate then refused with
		// InvalidJobError, which is neither outcome asserted below. On an idle
		// machine the test's own TriggerJob usually won that race, so it passed;
		// on a loaded runner the drain won and the job reddened lanes whose
		// diffs never touched the scheduler (#308). Measured under load before
		// the fix, InvalidJobError appeared in roughly 1 run in 60.
		const fillerBase = 1000 // above any id this test registers
		for i := 0; i < 100; i++ {
			select {
			case scheduler.triggerCh <- triggerRequest{jobID: uint64(fillerBase + i)}:
			default:
			}
		}

		err = scheduler.TriggerJob(cancelledCtx, 1, "")

		// Both outcomes are legitimate, and NOT because the buffer may or may
		// not be full: TriggerJob selects over the send, ctx.Done() and a
		// default. With an already-cancelled context ctx.Done() is always
		// ready, so the default arm is unreachable here and the queue-full
		// refusal cannot occur. When the buffer also has room, two arms are
		// ready at once and Go picks between them UNIFORMLY AT RANDOM — that
		// randomness, not timing, is why this accepts either answer.
		if err != nil {
			assert.Equal(t, context.Canceled, err)
		}
	})
}

// TestTriggerJobByIDStorageError tests storage error handling in triggerJobByID.
func TestTriggerJobByIDStorageError(t *testing.T) {
	t.Run("triggerJobByID handles storage error", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      time.Hour,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		err := scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Set storage error
		storage.getJobErr = errors.New("storage failure")

		// Schedule a pending job and trigger it - may return error or not depending on implementation
		err = scheduler.TriggerJob(ctx, 1, "")
		// Either we get an error here, or the error is tracked asynchronously
		if err != nil {
			assert.Contains(t, err.Error(), "storage")
		} else {
			// Wait for error to be tracked
			waitDone := make(chan struct{})
			go func() {
				for {
					if scheduler.StorageErrorCount() > 0 {
						close(waitDone)
						return
					}
				}
			}()

			select {
			case <-waitDone:
				assert.Greater(t, scheduler.StorageErrorCount(), uint64(0))
			case <-time.After(2 * time.Second):
				// Storage error might not be tracked if TriggerJob returned error
			}
		}
	})
}

// TestExecuteJobStorageErrors tests storage error handling during job execution.
func TestExecuteJobStorageErrors(t *testing.T) {
	t.Run("execute job handles update storage error", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Insert a job
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "test-job",
			Type:      JobTypeDelayed,
			Handler:   "test-handler",
			Status:    JobStatusPending,
			NextRun:   time.Now().Add(-time.Second),
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Set update error after initial insert
		storage.errMu.Lock()
		storage.updateJobErr = errors.New("update failed")
		storage.errMu.Unlock()

		// Wait for storage error to be tracked
		waitDone := make(chan struct{})
		go func() {
			for {
				if scheduler.StorageErrorCount() > 0 {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			assert.Greater(t, scheduler.StorageErrorCount(), uint64(0))
		case <-time.After(2 * time.Second):
			t.Fatal("storage error was not tracked")
		}
	})
}

// TestHandleJobSuccessStorageErrors tests storage error handling in handleJobSuccess.
func TestHandleJobSuccessStorageErrors(t *testing.T) {
	t.Run("handles save execution storage error", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		jobDone := make(chan struct{})
		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			close(jobDone)
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "test-job",
			Type:      JobTypeOneShot,
			Handler:   "test-handler",
			Status:    JobStatusPending,
			NextRun:   time.Now().Add(-time.Second),
			ExecuteAt: time.Now().Add(-time.Second),
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Set save execution error
		storage.errMu.Lock()
		storage.saveExecutionErr = errors.New("save failed")
		storage.errMu.Unlock()

		// Wait for job to complete
		select {
		case <-jobDone:
		case <-time.After(2 * time.Second):
			t.Fatal("job did not complete")
		}

		// Wait for storage error to be tracked
		waitDone := make(chan struct{})
		go func() {
			for {
				if scheduler.StorageErrorCount() > 0 {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			assert.Greater(t, scheduler.StorageErrorCount(), uint64(0))
		case <-time.After(2 * time.Second):
			t.Fatal("storage error was not tracked")
		}
	})
}

// TestHandleJobErrorStorageErrors tests storage error handling in handleJobError.
func TestHandleJobErrorStorageErrors(t *testing.T) {
	t.Run("handles save execution storage error on failure", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		jobDone := make(chan struct{})
		err := scheduler.RegisterHandler("failing-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			close(jobDone)
			return nil, errors.New("handler failed")
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "failing-job",
			Type:      JobTypeOneShot,
			Handler:   "failing-handler",
			Status:    JobStatusPending,
			NextRun:   time.Now().Add(-time.Second),
			ExecuteAt: time.Now().Add(-time.Second),
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Set save execution error
		storage.errMu.Lock()
		storage.saveExecutionErr = errors.New("save failed")
		storage.errMu.Unlock()

		// Wait for job to fail
		select {
		case <-jobDone:
		case <-time.After(2 * time.Second):
			t.Fatal("job did not complete")
		}

		// Wait for storage error to be tracked
		waitDone := make(chan struct{})
		go func() {
			for {
				if scheduler.StorageErrorCount() > 0 {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			assert.Greater(t, scheduler.StorageErrorCount(), uint64(0))
		case <-time.After(2 * time.Second):
			t.Fatal("storage error was not tracked")
		}
	})
}

// TestRecurringJobNextRunCalculationFailure tests recurring job failure on invalid schedule after run.
func TestRecurringJobNextRunCalculationFailure(t *testing.T) {
	t.Run("recurring job fails when next run calculation fails", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		jobDone := make(chan struct{})
		err := scheduler.RegisterHandler("test-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			close(jobDone)
			return &JobResult{Success: true}, nil
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Insert a recurring job with valid schedule initially
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "recurring-job",
			Type:      JobTypeRecurring,
			Handler:   "test-handler",
			Schedule:  "* * * * *",
			Status:    JobStatusPending,
			NextRun:   time.Now().Add(-time.Second),
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Wait for job to execute
		select {
		case <-jobDone:
		case <-time.After(2 * time.Second):
			t.Fatal("job did not execute")
		}

		// Corrupt the schedule after execution but before next run calculation
		// This tests the error path in handleJobSuccess for recurring jobs
		// The job should now have an invalid schedule stored
		storage.mu.Lock()
		if job, ok := storage.jobs[1]; ok {
			job.Schedule = "invalid cron"
		}
		storage.mu.Unlock()
	})
}

// TestJobRetryWithDefaultRetries tests retry using default config retries.
func TestJobRetryWithDefaultRetries(t *testing.T) {
	t.Run("uses default retries when job max_retries is zero", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    2,
			RetryBackoff:      10 * time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		attemptCount := atomic.Int32{}
		allDone := make(chan struct{})
		var closeOnce sync.Once

		err := scheduler.RegisterHandler("failing-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			count := attemptCount.Add(1)
			if count >= 2 {
				closeOnce.Do(func() { close(allDone) })
			}
			return nil, errors.New("intentional failure")
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		// Job with MaxRetries = 0 should use DefaultRetries
		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:         1,
			Name:       "retry-job",
			Type:       JobTypeDelayed,
			Handler:    "failing-handler",
			Status:     JobStatusPending,
			NextRun:    time.Now().Add(-time.Second),
			MaxRetries: 0, // Should use default of 2
			OwnerType:  OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Wait for retries
		select {
		case <-allDone:
			assert.GreaterOrEqual(t, attemptCount.Load(), int32(2))
		case <-time.After(5 * time.Second):
			t.Fatal("retries did not occur within timeout")
		}
	})
}

// TestNilResultInJobSuccess tests handling nil result in handleJobSuccess.
func TestNilResultInJobSuccess(t *testing.T) {
	t.Run("handles nil result in job success", func(t *testing.T) {
		storage := newMockJobStorage()
		config := &SchedulerConfig{
			TickInterval:      50 * time.Millisecond,
			MaxConcurrentJobs: 10,
			DefaultTimeout:    time.Second,
			DefaultRetries:    0,
			RetryBackoff:      time.Millisecond,
		}
		scheduler := New(config, storage)
		ctx := context.Background()

		jobDone := make(chan struct{})
		err := scheduler.RegisterHandler("nil-result-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
			close(jobDone)
			return nil, nil // Return nil result
		})
		require.NoError(t, err)

		err = scheduler.Start(ctx)
		require.NoError(t, err)
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = scheduler.Stop(stopCtx)
		}()

		storage.mu.Lock()
		storage.jobs[1] = &Job{
			ID:        1,
			Name:      "nil-result-job",
			Type:      JobTypeOneShot,
			Handler:   "nil-result-handler",
			Status:    JobStatusPending,
			NextRun:   time.Now().Add(-time.Second),
			ExecuteAt: time.Now().Add(-time.Second),
			OwnerType: OwnerTypeSystem,
		}
		storage.mu.Unlock()

		// Wait for job to complete
		select {
		case <-jobDone:
		case <-time.After(2 * time.Second):
			t.Fatal("job did not complete")
		}

		// Wait for status update
		waitDone := make(chan struct{})
		go func() {
			for {
				job := storage.getJobDirect(1)
				if job != nil && job.Status == JobStatusCompleted {
					close(waitDone)
					return
				}
			}
		}()

		select {
		case <-waitDone:
			job := storage.getJobDirect(1)
			assert.Equal(t, JobStatusCompleted, job.Status)
		case <-time.After(2 * time.Second):
			t.Fatal("job did not complete within timeout")
		}
	})
}

// newRaceTestConfig returns a small, fast config for the status
// transition race tests below.
func newRaceTestConfig() *SchedulerConfig {
	return &SchedulerConfig{
		TickInterval:      50 * time.Millisecond,
		MaxConcurrentJobs: 10,
		DefaultTimeout:    time.Second,
		DefaultRetries:    0,
		RetryBackoff:      time.Millisecond,
	}
}

// TestCancelJobConcurrentWithCompletionWrite verifies the per-job
// stripe lock invariant: once CancelJob succeeds, an in-flight
// execution-completion write can never overwrite the canceled status.
// This is a regression test for the integration failure where a
// triggered execution's failure write clobbered a concurrent cancel,
// leaving the job "failed" after the cancel API returned "canceled".
func TestCancelJobConcurrentWithCompletionWrite(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(newRaceTestConfig(), storage)
	ctx := context.Background()

	require.NoError(t, engine.Start(ctx))
	defer func() {
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = engine.Stop(stopCtx)
	}()

	for i := 0; i < 50; i++ {
		jobID := uint64(1000 + i)
		storage.mu.Lock()
		storage.jobs[jobID] = &Job{
			ID:        jobID,
			Name:      "race-job",
			Type:      JobTypeOneShot,
			Handler:   "missing-handler",
			Status:    JobStatusPending,
			OwnerType: OwnerTypeSystem,
			NextRun:   time.Now().Add(time.Hour),
		}
		storage.mu.Unlock()

		// Stale copy simulating an in-flight execution that fails
		// (e.g. unregistered handler) concurrently with the cancel.
		stale := &Job{
			ID:        jobID,
			Name:      "race-job",
			Type:      JobTypeOneShot,
			Handler:   "missing-handler",
			Status:    JobStatusPending,
			OwnerType: OwnerTypeSystem,
		}

		var wg sync.WaitGroup
		var cancelErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			engine.handleJobFailure(ctx, stale, HandlerNotFoundError{Handler: "missing-handler"})
		}()
		go func() {
			defer wg.Done()
			_, cancelErr = engine.CancelJobWithTenant(ctx, jobID, "", "test cancellation")
		}()
		wg.Wait()

		// Cancel always succeeds here: the job is either pending or
		// failed at cancel time, and both states are cancelable.
		require.NoError(t, cancelErr, "iteration %d: cancel failed", i)

		storage.mu.RLock()
		finalStatus := storage.jobs[jobID].Status
		storage.mu.RUnlock()
		assert.Equal(t, JobStatusCanceled, finalStatus,
			"iteration %d: cancel was clobbered by a concurrent completion write", i)
	}
}

// TestExecuteJobSkipsTerminalOrPausedJob verifies that executeJob
// re-checks the persisted job status under the stripe lock and skips
// execution when the job was concurrently canceled, completed, or
// paused between dispatch and execution.
func TestExecuteJobSkipsTerminalOrPausedJob(t *testing.T) {
	statuses := []JobStatus{JobStatusCanceled, JobStatusCompleted, JobStatusPaused}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			storage := newMockJobStorage()
			engine := New(newRaceTestConfig(), storage)
			ctx := context.Background()

			handlerCalled := make(chan struct{}, 1)
			require.NoError(t, engine.RegisterHandler("skip-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
				handlerCalled <- struct{}{}
				return &JobResult{Success: true}, nil
			}))

			storage.mu.Lock()
			storage.jobs[7] = &Job{
				ID:        7,
				Name:      "skip-job",
				Type:      JobTypeOneShot,
				Handler:   "skip-handler",
				Status:    status,
				OwnerType: OwnerTypeSystem,
			}
			storage.mu.Unlock()

			// Stale dispatch copy still shows pending.
			stale := &Job{
				ID:        7,
				Name:      "skip-job",
				Type:      JobTypeOneShot,
				Handler:   "skip-handler",
				Status:    JobStatusPending,
				OwnerType: OwnerTypeSystem,
			}
			engine.executeJob(ctx, stale, time.Time{})

			select {
			case <-handlerCalled:
				t.Fatalf("handler executed for %s job", status)
			default:
			}

			storage.mu.RLock()
			finalStatus := storage.jobs[7].Status
			storage.mu.RUnlock()
			assert.Equal(t, status, finalStatus, "status must be preserved")
		})
	}
}

// TestExecuteJobSkipsDeletedJob verifies that executeJob does not run
// (or resurrect) a job whose record was deleted between dispatch and
// execution.
func TestExecuteJobSkipsDeletedJob(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(newRaceTestConfig(), storage)
	ctx := context.Background()

	handlerCalled := make(chan struct{}, 1)
	require.NoError(t, engine.RegisterHandler("deleted-handler", func(ctx context.Context, job *Job) (*JobResult, error) {
		handlerCalled <- struct{}{}
		return &JobResult{Success: true}, nil
	}))

	// Job is NOT in storage: simulates a concurrent delete.
	stale := &Job{
		ID:        42,
		Name:      "deleted-job",
		Type:      JobTypeOneShot,
		Handler:   "deleted-handler",
		Status:    JobStatusPending,
		OwnerType: OwnerTypeSystem,
	}
	engine.executeJob(ctx, stale, time.Time{})

	select {
	case <-handlerCalled:
		t.Fatal("handler executed for deleted job")
	default:
	}

	storage.mu.RLock()
	_, exists := storage.jobs[42]
	storage.mu.RUnlock()
	assert.False(t, exists, "deleted job must not be resurrected")
}

// TestHandleJobSuccessPreservesConcurrentCancel verifies that a
// success-completion write preserves a concurrently persisted
// canceled status instead of overwriting it.
func TestHandleJobSuccessPreservesConcurrentCancel(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(newRaceTestConfig(), storage)
	ctx := context.Background()

	storage.mu.Lock()
	storage.jobs[9] = &Job{
		ID:        9,
		Name:      "canceled-job",
		Type:      JobTypeOneShot,
		Handler:   "any",
		Status:    JobStatusCanceled,
		OwnerType: OwnerTypeSystem,
	}
	storage.mu.Unlock()

	stale := &Job{
		ID:        9,
		Name:      "canceled-job",
		Type:      JobTypeOneShot,
		Handler:   "any",
		Status:    JobStatusRunning,
		OwnerType: OwnerTypeSystem,
	}
	exec := &JobExecution{JobID: 9, Status: JobStatusRunning, StartedAt: time.Now()}
	engine.handleJobSuccess(ctx, stale, exec, &JobResult{Success: true})

	storage.mu.RLock()
	finalStatus := storage.jobs[9].Status
	storage.mu.RUnlock()
	assert.Equal(t, JobStatusCanceled, finalStatus)
}

// TestCompletionWritesDoNotResurrectDeletedJob verifies that all
// three completion paths skip the job write when the record was
// deleted concurrently, instead of re-creating (resurrecting) it.
func TestCompletionWritesDoNotResurrectDeletedJob(t *testing.T) {
	newDeletedJob := func() *Job {
		return &Job{
			ID:        77,
			Name:      "ghost-job",
			Type:      JobTypeOneShot,
			Handler:   "any",
			Status:    JobStatusRunning,
			OwnerType: OwnerTypeSystem,
		}
	}

	t.Run("handleJobSuccess", func(t *testing.T) {
		storage := newMockJobStorage()
		engine := New(newRaceTestConfig(), storage)
		exec := &JobExecution{JobID: 77, Status: JobStatusRunning, StartedAt: time.Now()}

		engine.handleJobSuccess(context.Background(), newDeletedJob(), exec, &JobResult{Success: true})

		storage.mu.RLock()
		_, exists := storage.jobs[77]
		storage.mu.RUnlock()
		assert.False(t, exists, "success write must not resurrect a deleted job")
		assert.Equal(t, int32(1), storage.saveExecutionCount.Load(), "execution record must still be saved")
	})

	t.Run("handleJobError", func(t *testing.T) {
		storage := newMockJobStorage()
		engine := New(newRaceTestConfig(), storage)
		exec := &JobExecution{JobID: 77, Status: JobStatusRunning, StartedAt: time.Now()}

		engine.handleJobError(context.Background(), newDeletedJob(), exec, errors.New("handler failed"))

		storage.mu.RLock()
		_, exists := storage.jobs[77]
		storage.mu.RUnlock()
		assert.False(t, exists, "error write must not resurrect a deleted job")
		assert.Equal(t, int32(1), storage.saveExecutionCount.Load(), "execution record must still be saved")
	})

	t.Run("handleJobFailure", func(t *testing.T) {
		storage := newMockJobStorage()
		engine := New(newRaceTestConfig(), storage)

		engine.handleJobFailure(context.Background(), newDeletedJob(), errors.New("dispatch failed"))

		storage.mu.RLock()
		_, exists := storage.jobs[77]
		storage.mu.RUnlock()
		assert.False(t, exists, "failure write must not resurrect a deleted job")
	})
}

// TestJobTenantID verifies the tenant scoping used for storage
// lookups of a job by ownership type.
func TestJobTenantID(t *testing.T) {
	tests := []struct {
		name string
		job  *Job
		want string
	}{
		{"tenant job uses owner id", &Job{OwnerType: OwnerTypeTenant, OwnerID: "acme"}, "acme"},
		{"system job uses system namespace", &Job{OwnerType: OwnerTypeSystem, OwnerID: "system"}, ""},
		{"delegated job uses system namespace", &Job{OwnerType: OwnerTypeDelegated, OwnerID: "acme"}, ""},
		{"unset owner type uses system namespace", &Job{OwnerID: "acme"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, JobTenantID(tc.job))
		})
	}
}

// TestIsJobNotFound verifies matching of value and pointer forms of
// JobNotFoundError plus rejection of other errors.
func TestIsJobNotFound(t *testing.T) {
	assert.True(t, isJobNotFound(JobNotFoundError{JobID: 1}))
	assert.True(t, isJobNotFound(&JobNotFoundError{JobID: 1}))
	assert.True(t, isJobNotFound(ErrStorageError{Op: "GetJob", Cause: JobNotFoundError{JobID: 1}}))
	assert.False(t, isJobNotFound(nil))
	assert.False(t, isJobNotFound(errors.New("boom")))
}

// TestTriggerQueueFull_IsAnErrorNotASilentDrop (Codex 879 #2 on
// go-quicbaas#83): a full trigger queue used to return nil — "the next tick
// will pick it up", which is true only of a DUE job. A paused job, or a
// stranded firing at or below its claim mark, is never picked up by the tick,
// so the dropped trigger was a repair reported done that never happened.
// Driven internally: the queue is filled to capacity so the refusal branch is
// deterministic rather than a race against the drain.
//
// Mutation evidence: restoring `return nil` on the default branch fails all
// three assertions.
func TestTriggerQueueFull_IsAnErrorNotASilentDrop(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	engine.running.Store(true) // queue semantics only; nothing drains
	job := seedRunningJob(t, storage, runningRecoveryJob())
	job.Status = JobStatusPending
	if err := storage.UpdateJob(context.Background(), job); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}

	for i := 0; i < cap(engine.triggerCh); i++ {
		engine.triggerCh <- triggerRequest{jobID: 999, scope: ""}
	}

	var full TriggerQueueFullError
	if err := engine.TriggerJob(context.Background(), job.ID, JobTenantID(job)); !errors.As(err, &full) {
		t.Fatalf("TriggerJob on a full queue = %v, want TriggerQueueFullError", err)
	}
	if full.JobID != job.ID {
		t.Fatalf("the refusal names job %d, want %d", full.JobID, job.ID)
	}
	if err := engine.RetriggerStrandedFiring(context.Background(), job.ID, JobTenantID(job), job.NextRun); !errors.As(err, &full) {
		t.Fatalf("RetriggerStrandedFiring on a full queue = %v, want TriggerQueueFullError", err)
	}
}

// TestExecuteGate_BindsDispatchToFiringAndRefusesRunning drives the DRAIN
// directly (triggerJobByID), past every queue-time precheck, because the
// prechecks race the execution window and the executeJob gate is the
// authoritative one (Codex 879 #1): a dispatch accepted while the job was
// Pending must still decline if the job began running — or moved to another
// firing — before the drain reached it.
//
// Mutation evidence: passing a zero firing through the drain (unbinding the
// repair) runs the handler in the moved-firing case; removing executeJob's
// Running refusal runs it beside the live execution. Both fail here.
func TestExecuteGate_BindsDispatchToFiringAndRefusesRunning(t *testing.T) {
	firing := time.Now().Add(24 * time.Hour)
	newFixture := func(t *testing.T, status JobStatus, nextRun time.Time) (*SchedulerEngine, *mockJobStorage, *Job, *atomic.Int32) {
		t.Helper()
		storage := newMockJobStorage()
		engine := New(DefaultConfig(), storage)
		var runs atomic.Int32
		if err := engine.RegisterHandler("gate-handler", func(context.Context, *Job) (*JobResult, error) {
			runs.Add(1)
			return &JobResult{}, nil
		}); err != nil {
			t.Fatalf("RegisterHandler: %v", err)
		}
		job := seedRunningJob(t, storage, &Job{
			Name: "gated", Type: JobTypeRecurring, Schedule: "*/5 * * * *", Handler: "gate-handler",
			Status: status, NextRun: nextRun, UpdatedAt: nextRun, OwnerType: OwnerTypeSystem,
		})
		return engine, storage, job, &runs
	}

	t.Run("a bound dispatch declines a job that began running", func(t *testing.T) {
		engine, _, job, runs := newFixture(t, JobStatusRunning, firing)
		engine.triggerJobByID(context.Background(), triggerRequest{jobID: job.ID, firing: firing})
		engine.wg.Wait()
		if runs.Load() != 0 {
			t.Fatal("the repair ran the handler beside the live execution")
		}
	})
	t.Run("a MANUAL dispatch declines a job that began running", func(t *testing.T) {
		engine, _, job, runs := newFixture(t, JobStatusRunning, firing)
		engine.triggerJobByID(context.Background(), triggerRequest{jobID: job.ID})
		engine.wg.Wait()
		if runs.Load() != 0 {
			t.Fatal("a manual trigger ran the handler beside the live execution")
		}
	})
	t.Run("a bound dispatch declines a firing that moved on", func(t *testing.T) {
		engine, _, job, runs := newFixture(t, JobStatusPending, firing.Add(time.Hour))
		engine.triggerJobByID(context.Background(), triggerRequest{jobID: job.ID, firing: firing})
		engine.wg.Wait()
		if runs.Load() != 0 {
			t.Fatal("the repair ran a firing the job had already moved past")
		}
	})
	t.Run("a bound dispatch on the observed firing runs once", func(t *testing.T) {
		engine, _, job, runs := newFixture(t, JobStatusPending, firing)
		engine.triggerJobByID(context.Background(), triggerRequest{jobID: job.ID, firing: firing})
		engine.wg.Wait()
		if got := runs.Load(); got != 1 {
			t.Fatalf("the honest repair ran %d time(s), want 1", got)
		}
	})
}
