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

package orchestration

import (
	"context"
	"runtime"
	"sync"
	"testing"
)

// waitFor polls cond, yielding the scheduler between checks, until cond returns
// true or the bounded iteration budget is exhausted. It avoids time.Sleep while
// still letting a cooperating goroutine make progress.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200000; i++ {
		if cond() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("waitFor: condition not met within budget")
}

// seedClaimedTask creates and claims a task so it is eligible for completion.
func seedClaimedTask(t *testing.T, svc *agentServiceImpl, store TaskStore, tenantID string) *Task {
	t.Helper()
	ctx := context.Background()
	task, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: tenantID, Subject: "mirror-subject"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.ClaimTask(ctx, tenantID, task.ID, "agent-a"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	return task
}

func TestMirrorJobStatusToTask_Completed(t *testing.T) {
	svc := newTestService(t)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)

	const tenantID = "tenant-1"
	const jobID = "job-1"
	// SubscribeJobEvents verifies the job exists in memory.
	svc.jobs.Store(tenantID+"/"+jobID, &runningJob{})

	task := seedClaimedTask(t, svc, store, tenantID)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		svc.mirrorJobStatusToTask(tenantID, task.ID, jobID)
	}()

	// Publish the terminal job event repeatedly until the mirror picks it up
	// and marks the task completed. Extra publishes are harmless once the
	// mirror has returned.
	ctx := context.Background()
	waitFor(t, func() bool {
		svc.publishEvent(ctx, tenantID, "job_completed", map[string]string{
			"job_id":  jobID,
			"state":   string(JobStateCompleted),
			"details": "all done",
		})
		got, err := store.GetTask(ctx, tenantID, task.ID)
		return err == nil && got.Status == TaskStatusCompleted
	})
	wg.Wait()

	got, err := store.GetTask(ctx, tenantID, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Fatalf("expected task completed, got %q", got.Status)
	}
	if got.Result == nil || got.Result.Output != "all done" {
		t.Fatalf("expected mirrored output, got %+v", got.Result)
	}
}

func TestMirrorJobStatusToTask_Failed(t *testing.T) {
	svc := newTestService(t)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)

	const tenantID = "tenant-1"
	const jobID = "job-2"
	svc.jobs.Store(tenantID+"/"+jobID, &runningJob{})

	task := seedClaimedTask(t, svc, store, tenantID)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		svc.mirrorJobStatusToTask(tenantID, task.ID, jobID)
	}()

	ctx := context.Background()
	waitFor(t, func() bool {
		svc.publishEvent(ctx, tenantID, "job_failed", map[string]string{
			"job_id":  jobID,
			"state":   string(JobStateFailed),
			"details": "boom",
		})
		got, err := store.GetTask(ctx, tenantID, task.ID)
		return err == nil && got.Status == TaskStatusFailed
	})
	wg.Wait()

	got, err := store.GetTask(ctx, tenantID, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Fatalf("expected task failed, got %q", got.Status)
	}
}

func TestMirrorJobStatusToTask_SubscribeFails(t *testing.T) {
	svc := newTestService(t)
	svc.SetTaskStore(NewInMemoryTaskStore())

	// No job registered → SubscribeJobEvents returns JobNotFoundError and the
	// mirror logs and returns without touching the task store. This must not
	// block or panic.
	svc.mirrorJobStatusToTask("tenant-1", "task-x", "absent-job")
}
