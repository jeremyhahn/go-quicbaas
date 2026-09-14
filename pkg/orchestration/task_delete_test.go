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

package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"
)

// =============================================================================
// DeleteTask on the service
// =============================================================================

// leaseFor takes a live lease on one task through the service's own store, the
// way a running job does, so the guard is exercised against real lease state
// rather than a stand-in.
func leaseFor(t *testing.T, impl *agentServiceImpl, tenantID, projectID, taskID, ref string) *JobLease {
	t.Helper()
	lease := &JobLease{
		TenantID:   tenantID,
		ProjectID:  projectID,
		Workflow:   "workflow",
		Ref:        ref,
		TaskID:     taskID,
		JobID:      "job-" + taskID,
		Generation: 1,
		Node:       "node-a",
	}
	if err := impl.jobLeases.Acquire(context.Background(), lease); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	return lease
}

func TestAgentService_DeleteTask_CallsStore(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, _ := newTaskServiceWithStore(t, store)

	if err := svc.DeleteTask(context.Background(), "tenant-1", "task-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleteCalls) != 1 {
		t.Fatalf("expected 1 DeleteTask call, got %d", len(store.deleteCalls))
	}
	if call := store.deleteCalls[0]; call.TenantID != "tenant-1" || call.TaskID != "task-1" {
		t.Fatalf("store received %+v", call)
	}
}

func TestAgentService_DeleteTask_RemovesTheRecord(t *testing.T) {
	t.Parallel()

	svc, _ := newTaskServiceWithStore(t, NewInMemoryTaskStore())
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", Subject: "work"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.DeleteTask(ctx, "tenant-1", task.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := svc.GetTask(ctx, "tenant-1", task.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound after delete, got %v", err)
	}
}

func TestAgentService_DeleteTask_RefusedWhileLeased(t *testing.T) {
	t.Parallel()

	svc, impl := newTaskServiceWithStore(t, NewInMemoryTaskStore())
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", ProjectID: testProjectID, Subject: "work"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	leaseFor(t, impl, "tenant-1", testProjectID, task.ID, task.ID)

	err = svc.DeleteTask(ctx, "tenant-1", task.ID)
	var leased *TaskLeasedError
	if !errors.As(err, &leased) {
		t.Fatalf("expected TaskLeasedError, got %v", err)
	}
	if leased.TaskID != task.ID || leased.JobID != "job-"+task.ID || leased.Node != "node-a" {
		t.Fatalf("refusal does not name the holder: %+v", leased)
	}
	if !errors.Is(err, ErrTaskLeased) {
		t.Fatalf("refusal does not unwrap to ErrTaskLeased: %v", err)
	}

	// The record is still there: a refusal deletes nothing.
	if _, getErr := svc.GetTask(ctx, "tenant-1", task.ID); getErr != nil {
		t.Fatalf("refused delete removed the task anyway: %v", getErr)
	}
}

func TestAgentService_DeleteTask_AllowedAfterLeaseReleased(t *testing.T) {
	t.Parallel()

	svc, impl := newTaskServiceWithStore(t, NewInMemoryTaskStore())
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", ProjectID: testProjectID, Subject: "work"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	lease := leaseFor(t, impl, "tenant-1", testProjectID, task.ID, task.ID)

	if err := impl.jobLeases.Release(ctx, lease); err != nil {
		t.Fatalf("release lease: %v", err)
	}

	if err := svc.DeleteTask(ctx, "tenant-1", task.ID); err != nil {
		t.Fatalf("delete after release: %v", err)
	}
}

// TestAgentService_DeleteTask_IgnoresAnotherTasksLease proves the guard is
// keyed on the task, not on the tenant having any lease at all: a live lease
// over a DIFFERENT task must not block this delete.
func TestAgentService_DeleteTask_IgnoresAnotherTasksLease(t *testing.T) {
	t.Parallel()

	svc, impl := newTaskServiceWithStore(t, NewInMemoryTaskStore())
	ctx := context.Background()

	held, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", ProjectID: testProjectID, Subject: "held"})
	if err != nil {
		t.Fatalf("create held: %v", err)
	}
	free, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", ProjectID: testProjectID, Subject: "free"})
	if err != nil {
		t.Fatalf("create free: %v", err)
	}
	leaseFor(t, impl, "tenant-1", testProjectID, held.ID, held.ID)

	if err := svc.DeleteTask(ctx, "tenant-1", free.ID); err != nil {
		t.Fatalf("unleased task refused: %v", err)
	}
}

// TestAgentService_DeleteTask_StaleLeaseDoesNotBlock proves a heartbeat older
// than the TTL is not an obstacle: the holder stopped proving it was alive, so
// the reaper owns that job and the record is free to go.
func TestAgentService_DeleteTask_StaleLeaseDoesNotBlock(t *testing.T) {
	t.Parallel()

	svc, impl := newTaskServiceWithStore(t, NewInMemoryTaskStore())
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", ProjectID: testProjectID, Subject: "work"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	leaseFor(t, impl, "tenant-1", testProjectID, task.ID, task.ID)

	// A one-nanosecond TTL makes the heartbeat just written already older than
	// the bound, which is exactly what "stale" means, with no waiting.
	impl.leaseTTL = time.Nanosecond

	if err := svc.DeleteTask(ctx, "tenant-1", task.ID); err != nil {
		t.Fatalf("stale lease blocked the delete: %v", err)
	}
}

// TestAgentService_DeleteTask_ReleasesSourceForReadoption is the issue's
// motivating case: a task adopted from a forge issue holds that issue's source
// reservation, and while it does no second task can adopt the same issue.
// Deleting the task must give the reservation back.
func TestAgentService_DeleteTask_ReleasesSourceForReadoption(t *testing.T) {
	t.Parallel()

	svc, _ := newTaskServiceWithStore(t, NewInMemoryTaskStore())
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "jeremyhahn/go-truststrap#25"}

	first, err := svc.CreateTask(ctx, &CreateTaskRequest{
		TenantID:  "tenant-1",
		ProjectID: testProjectID,
		Subject:   "adopt the issue",
		Source:    source,
	})
	if err != nil {
		t.Fatalf("first adoption: %v", err)
	}

	// While the first task holds it, the source cannot be adopted again.
	_, err = svc.CreateTask(ctx, &CreateTaskRequest{
		TenantID:  "tenant-1",
		ProjectID: testProjectID,
		Subject:   "adopt it again",
		Source:    source,
	})
	var adopted *TaskSourceAlreadyAdoptedError
	if !errors.As(err, &adopted) {
		t.Fatalf("expected TaskSourceAlreadyAdoptedError before the delete, got %v", err)
	}
	if adopted.TaskID != first.ID {
		t.Fatalf("reservation names %q, want the first task %q", adopted.TaskID, first.ID)
	}

	if err := svc.DeleteTask(ctx, "tenant-1", first.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	second, err := svc.CreateTask(ctx, &CreateTaskRequest{
		TenantID:  "tenant-1",
		ProjectID: testProjectID,
		Subject:   "adopt it again",
		Source:    source,
	})
	if err != nil {
		t.Fatalf("re-adoption after delete: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("re-adoption reused the deleted task's ID")
	}
}

func TestAgentService_DeleteTask_Errors(t *testing.T) {
	t.Parallel()

	t.Run("no task store", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, nil)
		err := svc.DeleteTask(context.Background(), "tenant-1", "task-1")
		if !errors.Is(err, ErrTaskStoreRequired) {
			t.Fatalf("expected ErrTaskStoreRequired, got %v", err)
		}
	})
	t.Run("missing tenant", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, &mockTaskStore{})
		err := svc.DeleteTask(context.Background(), "", "task-1")
		if !errors.Is(err, ErrTenantIDRequired) {
			t.Fatalf("expected ErrTenantIDRequired, got %v", err)
		}
	})
	t.Run("missing task id", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, &mockTaskStore{})
		err := svc.DeleteTask(context.Background(), "tenant-1", "")
		if !errors.Is(err, ErrTaskIDRequired) {
			t.Fatalf("expected ErrTaskIDRequired, got %v", err)
		}
	})
	t.Run("unknown task", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, NewInMemoryTaskStore())
		err := svc.DeleteTask(context.Background(), "tenant-1", "no-such-task")
		if !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("expected ErrTaskNotFound, got %v", err)
		}
	})
	t.Run("store error propagates", func(t *testing.T) {
		t.Parallel()
		store := &mockTaskStore{deleteErr: &TaskNotFoundError{TaskID: "task-1"}}
		svc, _ := newTaskServiceWithStore(t, store)
		err := svc.DeleteTask(context.Background(), "tenant-1", "task-1")
		if !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("expected ErrTaskNotFound, got %v", err)
		}
	})
	t.Run("no lease store fails closed", func(t *testing.T) {
		t.Parallel()
		svc, impl := newTaskServiceWithStore(t, &mockTaskStore{})
		impl.jobLeases = nil
		err := svc.DeleteTask(context.Background(), "tenant-1", "task-1")
		if !errors.Is(err, ErrJobLeaseStoreRequired) {
			t.Fatalf("expected ErrJobLeaseStoreRequired, got %v", err)
		}
	})
}

func TestTaskLeasedError_Message(t *testing.T) {
	t.Parallel()

	withNode := &TaskLeasedError{TenantID: "t", TaskID: "task-1", JobID: "job-1", Node: "node-a"}
	if got := withNode.Error(); got != "agent: task is leased by a running job: task task-1 is held by job job-1 on node node-a" {
		t.Fatalf("unexpected message: %q", got)
	}
	if !errors.Is(withNode, ErrTaskLeased) {
		t.Fatal("TaskLeasedError does not unwrap to ErrTaskLeased")
	}

	noNode := &TaskLeasedError{TenantID: "t", TaskID: "task-1", JobID: "job-1"}
	if got := noNode.Error(); got != "agent: task is leased by a running job: task task-1 is held by job job-1" {
		t.Fatalf("unexpected message without a node: %q", got)
	}
}

// TestAgentService_DeleteTask_RefusedWhenTheRecordWasRescoped is why the lease
// guard lists the tenant's whole subtree and not one project's (#613).
//
// A lease's project segment is pinned when the job acquires it; the task's
// ProjectID is a plain field UpdateTask rewrites. Move the task to another
// project while a job is running it and the two disagree — the live lease sits
// under the OLD project's subtree while the record names the new one. A guard
// that narrowed its listing to the record's project would find no lease and
// delete the record of a task a job is actively running, which is exactly the
// outcome the guard exists to prevent.
func TestAgentService_DeleteTask_RefusedWhenTheRecordWasRescoped(t *testing.T) {
	t.Parallel()

	const otherProjectID = "1f1a1e46-0c1e-4a7e-9a5f-2b0b9c3d4e09"
	svc, impl := newTaskServiceWithStore(t, NewInMemoryTaskStore())
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", ProjectID: testProjectID, Subject: "work"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The job took its lease under the project the task had at spawn time.
	leaseFor(t, impl, "tenant-1", testProjectID, task.ID, task.ID)

	// The task is then moved to another project, as UpdateTask permits.
	task.ProjectID = otherProjectID
	if updateErr := impl.taskStore.UpdateTask(ctx, task); updateErr != nil {
		t.Fatalf("update: %v", updateErr)
	}
	moved, getErr := svc.GetTask(ctx, "tenant-1", task.ID)
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	if moved.ProjectID != otherProjectID {
		t.Fatalf("the fixture did not move the task: project = %q", moved.ProjectID)
	}

	err = svc.DeleteTask(ctx, "tenant-1", task.ID)
	var leased *TaskLeasedError
	if !errors.As(err, &leased) || leased.JobID != "job-"+task.ID {
		t.Fatalf("a live lease under the task's former project must still refuse the delete, got %v", err)
	}
	if _, getErr := svc.GetTask(ctx, "tenant-1", task.ID); getErr != nil {
		t.Fatalf("refused delete removed the task anyway: %v", getErr)
	}
}
