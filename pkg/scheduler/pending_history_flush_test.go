package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pendingMarker names the pending history record these tests flush.
const pendingMarker = "abandoned: node lost"

// flushGateStorage holds the pending-history flush between its read of the
// job and its write: once the pending record is saved, the next GetJob (the
// flush's read) returns and then waits for release.
type flushGateStorage struct {
	*mockJobStorage
	armed   atomic.Bool
	once    sync.Once
	held    chan struct{}
	release chan struct{}
}

func newFlushGateStorage() *flushGateStorage {
	return &flushGateStorage{
		mockJobStorage: newMockJobStorage(),
		held:           make(chan struct{}),
		release:        make(chan struct{}),
	}
}

func (g *flushGateStorage) SaveExecution(ctx context.Context, exec *JobExecution) error {
	err := g.mockJobStorage.SaveExecution(ctx, exec)
	if err == nil && exec.Error == pendingMarker {
		g.armed.Store(true)
	}
	return err
}

func (g *flushGateStorage) GetJob(ctx context.Context, id uint64, tenantID string) (*Job, error) {
	job, err := g.mockJobStorage.GetJob(ctx, id, tenantID)
	if g.armed.CompareAndSwap(true, false) {
		g.once.Do(func() { close(g.held) })
		<-g.release
	}
	return job, err
}

// jobWithPendingHistory is a one-shot job that carries one record a recovery
// could not write. It is due in an hour, so only the test fires it.
func jobWithPendingHistory(handler string) *Job {
	now := time.Now()
	due := now.Add(time.Hour)
	return &Job{
		Name:      "flush-me",
		OwnerType: OwnerTypeTenant,
		OwnerID:   "tenant-a",
		Type:      JobTypeOneShot,
		Handler:   handler,
		Timeout:   time.Minute,
		Status:    JobStatusPending,
		ExecuteAt: due,
		NextRun:   due,
		PendingHistory: []*JobExecution{{
			TenantID:  "tenant-a",
			Status:    JobStatusFailed,
			StartedAt: now.Add(-2 * time.Minute),
			EndedAt:   now.Add(-time.Minute),
			Error:     pendingMarker,
		}},
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never happened", what)
	}
}

// TestFlushPendingHistory_UnderStripeLock holds executeJob's pending-history
// flush between its read and its write while a force delete or a rename of
// the job runs. Outside the stripe lock the flush wrote back the record it
// had read, recreating the deleted job or undoing the rename.
func TestFlushPendingHistory_UnderStripeLock(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(ctx context.Context, engine *SchedulerEngine, job *Job) error
		check  func(t *testing.T, storage *mockJobStorage, job *Job)
	}{
		{
			name: "force delete",
			mutate: func(ctx context.Context, engine *SchedulerEngine, job *Job) error {
				return engine.DeleteJob(ctx, job.ID, "tenant-a", true)
			},
			check: func(t *testing.T, storage *mockJobStorage, job *Job) {
				t.Helper()
				if held, err := storage.GetJob(context.Background(), job.ID, "tenant-a"); err == nil && held != nil {
					t.Fatalf("the force-deleted job is back: %s", held.Status)
				}
			},
		},
		{
			name: "rename",
			mutate: func(ctx context.Context, engine *SchedulerEngine, job *Job) error {
				_, err := engine.UpdateJob(ctx, job.ID, "tenant-a", &JobUpdateParams{Name: "renamed"})
				return err
			},
			check: func(t *testing.T, storage *mockJobStorage, job *Job) {
				t.Helper()
				if held := storedJob(t, storage, job.ID); held.Name != "renamed" {
					t.Fatalf("job is named %q after the flush, want the rename kept", held.Name)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			storage := newFlushGateStorage()
			engine := New(DefaultConfig(), storage)
			started, finish := make(chan struct{}), make(chan struct{})
			if err := engine.RegisterHandler("flush-test", func(context.Context, *Job) (*JobResult, error) {
				close(started)
				<-finish
				return &JobResult{Success: true}, nil
			}); err != nil {
				t.Fatalf("RegisterHandler: %v", err)
			}
			if err := engine.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer func() { _ = engine.Stop(context.Background()) }()
			job := seedRunningJob(t, storage.mockJobStorage, jobWithPendingHistory("flush-test"))

			done := fire(engine, job)
			waitClosed(t, storage.held, "the pending-history flush")

			lock := engine.jobLock(job.ID)
			if lock.TryLock() {
				// The flush runs outside the stripe lock: the mutation
				// completes between the flush's read and its write.
				lock.Unlock()
				if err := tc.mutate(ctx, engine, job); err != nil {
					t.Fatalf("mutate: %v", err)
				}
				close(storage.release)
			} else {
				mutated := make(chan error, 1)
				go func() { mutated <- tc.mutate(ctx, engine, job) }()
				close(storage.release)
				if err := <-mutated; err != nil {
					t.Fatalf("mutate: %v", err)
				}
			}

			waitClosed(t, started, "the handler")
			tc.check(t, storage.mockJobStorage, job)
			close(finish)
			waitDone(t, done)
			if tc.name == "force delete" {
				tc.check(t, storage.mockJobStorage, job)
			}
		})
	}
}
