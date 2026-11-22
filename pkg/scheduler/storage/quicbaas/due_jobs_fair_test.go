package quicbaas

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// TestGetJobsDueForExecution_EarlierTenantCannotStarveLaterTenant runs the
// due scan the way the scheduler's tick does, with its default batch of ten:
// tenant "aaa" owns ten recurring jobs that fire every second and are always
// due, tenant "zzz" owns one job that has been overdue for an hour (a KMS
// grace-period job, say). The store visits keys in sorted order, so a scan
// that stopped at the batch limit would hand every batch to "aaa"; the
// longest-overdue job must be dispatched on the first tick, and "aaa"'s
// jobs must still run.
func TestGetJobsDueForExecution_EarlierTenantCannotStarveLaterTenant(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)
	now := time.Now()
	const batch = 10

	var busy []*scheduler.Job
	for i := 0; i < batch; i++ {
		job := createTestJob(fmt.Sprintf("aaa-every-1s-%d", i), scheduler.OwnerTypeTenant, "aaa")
		job.Type = scheduler.JobTypeRecurring
		job.Schedule = "@every 1s"
		job.NextRun = now.Add(-time.Second)
		if err := storage.CreateJob(ctx, job); err != nil {
			t.Fatalf("CreateJob(aaa %d): %v", i, err)
		}
		busy = append(busy, job)
	}

	overdue := createTestJob("zzz-grace-period", scheduler.OwnerTypeTenant, "zzz")
	overdue.NextRun = now.Add(-time.Hour)
	if err := storage.CreateJob(ctx, overdue); err != nil {
		t.Fatalf("CreateJob(zzz): %v", err)
	}

	tick := now
	dispatched := map[string]int{}
	for round := 0; round < 3; round++ {
		due, err := storage.GetJobsDueForExecution(ctx, tick, batch, nil)
		if err != nil {
			t.Fatalf("round %d: GetJobsDueForExecution: %v", round, err)
		}
		if len(due) > batch {
			t.Fatalf("round %d: %d jobs exceed the batch of %d", round, len(due), batch)
		}
		for i := 1; i < len(due); i++ {
			if due[i].NextRun.Before(due[i-1].NextRun) {
				t.Fatalf("round %d: batch is not ordered by firing time", round)
			}
		}
		if round == 0 && (len(due) == 0 || due[0].Name != overdue.Name) {
			t.Fatalf("round 0: the job overdue for an hour must be dispatched first, got %v", jobNames(due))
		}
		for _, job := range due {
			dispatched[job.Name]++
			// Emulate the dispatch: a recurring job moves to its next
			// firing, a one-shot job completes.
			if job.Type == scheduler.JobTypeRecurring {
				job.NextRun = tick.Add(time.Second)
			} else {
				job.Status = scheduler.JobStatusCompleted
			}
			if err := storage.UpdateJob(ctx, job); err != nil {
				t.Fatalf("round %d: UpdateJob(%s): %v", round, job.Name, err)
			}
		}
		tick = tick.Add(time.Second)
	}

	if dispatched[overdue.Name] != 1 {
		t.Errorf("zzz's overdue job dispatched %d times, want 1", dispatched[overdue.Name])
	}
	for _, job := range busy {
		if dispatched[job.Name] == 0 {
			t.Errorf("aaa's %s never dispatched", job.Name)
		}
	}
}

func jobNames(jobs []*scheduler.Job) []string {
	names := make([]string, 0, len(jobs))
	for _, j := range jobs {
		names = append(names, j.Name)
	}
	return names
}
