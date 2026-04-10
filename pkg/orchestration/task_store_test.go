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
	"errors"
	"testing"
)

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewInMemoryTaskStore(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

// =============================================================================
// CreateTask Tests
// =============================================================================

func TestCreateTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:          "task-1",
		TenantID:    "tenant-a",
		TeamName:    "backend",
		Subject:     "Implement API",
		Description: "Build the REST API endpoint",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if task.Version != 1 {
		t.Errorf("expected version 1, got %d", task.Version)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("expected status pending, got %s", task.Status)
	}
	if task.CreatedAt == 0 {
		t.Error("expected created_at to be set")
	}
	if task.UpdatedAt == 0 {
		t.Error("expected updated_at to be set")
	}

	// Verify retrieval
	got, err := store.GetTask(context.Background(), "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("expected no error on get, got %v", err)
	}
	if got.ID != "task-1" {
		t.Errorf("expected task ID 'task-1', got %q", got.ID)
	}
	if got.TeamName != "backend" {
		t.Errorf("expected team 'backend', got %q", got.TeamName)
	}
}

func TestCreateTask_DuplicateID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-dup",
		TenantID: "tenant-a",
		Subject:  "First task",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("expected no error on first create, got %v", err)
	}

	duplicate := &Task{
		ID:       "task-dup",
		TenantID: "tenant-a",
		Subject:  "Duplicate task",
	}

	err = store.CreateTask(context.Background(), duplicate)
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
	if !errors.Is(err, ErrTaskAlreadyExists) {
		t.Fatalf("expected ErrTaskAlreadyExists, got %v", err)
	}
}

func TestCreateTask_NilTask(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CreateTask(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil task")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestCreateTask_EmptyID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CreateTask(context.Background(), &Task{TenantID: "t"})
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestCreateTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CreateTask(context.Background(), &Task{ID: "t1"})
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestCreateTask_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.CreateTask(ctx, &Task{ID: "t1", TenantID: "t"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// GetTask Tests
// =============================================================================

func TestGetTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:          "task-get",
		TenantID:    "tenant-a",
		Subject:     "Get me",
		Description: "Task to retrieve",
		Metadata:    map[string]string{"key": "value"},
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := store.GetTask(context.Background(), "tenant-a", "task-get")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Subject != "Get me" {
		t.Errorf("expected subject 'Get me', got %q", got.Subject)
	}
	if got.Metadata["key"] != "value" {
		t.Errorf("expected metadata key=value, got %v", got.Metadata)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.GetTask(context.Background(), "tenant-a", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestGetTask_WrongTenant(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-tenant",
		TenantID: "tenant-a",
		Subject:  "Tenant A task",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Try to get with wrong tenant
	_, err = store.GetTask(context.Background(), "tenant-b", "task-tenant")
	if err == nil {
		t.Fatal("expected error for wrong tenant")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound for tenant isolation, got %v", err)
	}
}

func TestGetTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.GetTask(context.Background(), "", "task-1")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestGetTask_EmptyTaskID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.GetTask(context.Background(), "tenant-a", "")
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestGetTask_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// UpdateTask Tests
// =============================================================================

func TestUpdateTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-update",
		TenantID: "tenant-a",
		Subject:  "Original",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Retrieve to get the current version
	current, err := store.GetTask(context.Background(), "tenant-a", "task-update")
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}

	current.Subject = "Updated"
	err = store.UpdateTask(context.Background(), current)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify
	got, err := store.GetTask(context.Background(), "tenant-a", "task-update")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Subject != "Updated" {
		t.Errorf("expected subject 'Updated', got %q", got.Subject)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
}

func TestUpdateTask_VersionConflict(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-conflict",
		TenantID: "tenant-a",
		Subject:  "Original",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Get two copies
	copy1, err := store.GetTask(context.Background(), "tenant-a", "task-conflict")
	if err != nil {
		t.Fatalf("setup get 1: %v", err)
	}
	copy2, err := store.GetTask(context.Background(), "tenant-a", "task-conflict")
	if err != nil {
		t.Fatalf("setup get 2: %v", err)
	}

	// Update with first copy succeeds
	copy1.Subject = "Updated by copy1"
	err = store.UpdateTask(context.Background(), copy1)
	if err != nil {
		t.Fatalf("expected first update to succeed, got %v", err)
	}

	// Update with stale copy2 fails
	copy2.Subject = "Updated by copy2"
	err = store.UpdateTask(context.Background(), copy2)
	if err == nil {
		t.Fatal("expected version conflict error")
	}
	if !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("expected ErrTaskVersionConflict, got %v", err)
	}

	var conflictErr *TaskVersionConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected TaskVersionConflictError, got %T", err)
	}
	if conflictErr.TaskID != "task-conflict" {
		t.Errorf("expected task ID 'task-conflict', got %q", conflictErr.TaskID)
	}
}

func TestUpdateTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "nonexistent",
		TenantID: "tenant-a",
		Version:  1,
	}

	err := store.UpdateTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestUpdateTask_NilTask(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.UpdateTask(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil task")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestUpdateTask_EmptyID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.UpdateTask(context.Background(), &Task{TenantID: "t"})
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// ClaimTask Tests
// =============================================================================

func TestClaimTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-claim",
		TenantID: "tenant-a",
		Subject:  "Claim me",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.ClaimTask(context.Background(), "tenant-a", "task-claim", "agent-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the claim
	got, err := store.GetTask(context.Background(), "tenant-a", "task-claim")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Status != TaskStatusClaimed {
		t.Errorf("expected status claimed, got %s", got.Status)
	}
	if got.AssignedTo != "agent-1" {
		t.Errorf("expected assigned_to 'agent-1', got %q", got.AssignedTo)
	}
	if got.ClaimedAt == 0 {
		t.Error("expected claimed_at to be set")
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
}

func TestClaimTask_AlreadyClaimed(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-double-claim",
		TenantID: "tenant-a",
		Subject:  "Double claim",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// First claim succeeds
	err = store.ClaimTask(context.Background(), "tenant-a", "task-double-claim", "agent-1")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// Second claim fails because status is no longer pending
	err = store.ClaimTask(context.Background(), "tenant-a", "task-double-claim", "agent-2")
	if err == nil {
		t.Fatal("expected error for already claimed task")
	}
	if !errors.Is(err, ErrTaskAlreadyClaimed) {
		t.Fatalf("expected ErrTaskAlreadyClaimed, got %v", err)
	}
}

func TestClaimTask_NotPending(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-not-pending",
		TenantID: "tenant-a",
		Subject:  "Not pending",
		Status:   TaskStatusCompleted,
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Force the status to completed by updating
	current, err := store.GetTask(context.Background(), "tenant-a", "task-not-pending")
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}
	current.Status = TaskStatusCompleted
	err = store.UpdateTask(context.Background(), current)
	if err != nil {
		t.Fatalf("setup update: %v", err)
	}

	err = store.ClaimTask(context.Background(), "tenant-a", "task-not-pending", "agent-1")
	if err == nil {
		t.Fatal("expected error for non-pending task")
	}
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("expected ErrTaskInvalidState, got %v", err)
	}

	var stateErr *TaskInvalidStateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("expected TaskInvalidStateError, got %T", err)
	}
	if stateErr.CurrentState != TaskStatusCompleted {
		t.Errorf("expected current state completed, got %s", stateErr.CurrentState)
	}
}

func TestClaimTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.ClaimTask(context.Background(), "tenant-a", "nonexistent", "agent-1")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestClaimTask_EmptyAgentID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.ClaimTask(context.Background(), "tenant-a", "task-1", "")
	if err == nil {
		t.Fatal("expected error for empty agent ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// CompleteTask Tests
// =============================================================================

func TestCompleteTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-complete",
		TenantID: "tenant-a",
		Subject:  "Complete me",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}

	// Claim first
	err = store.ClaimTask(context.Background(), "tenant-a", "task-complete", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	result := &TaskResult{
		Output: "All done",
		Files:  []string{"main.go", "main_test.go"},
		Metadata: map[string]string{
			"lines_changed": "42",
		},
	}

	err = store.CompleteTask(context.Background(), "tenant-a", "task-complete", result)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := store.GetTask(context.Background(), "tenant-a", "task-complete")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Errorf("expected status completed, got %s", got.Status)
	}
	if got.Result == nil {
		t.Fatal("expected result to be set")
	}
	if got.Result.Output != "All done" {
		t.Errorf("expected result output 'All done', got %q", got.Result.Output)
	}
	if got.CompletedAt == 0 {
		t.Error("expected completed_at to be set")
	}
}

func TestCompleteTask_NotClaimed(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-unclaimed",
		TenantID: "tenant-a",
		Subject:  "Unclaimed task",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.CompleteTask(context.Background(), "tenant-a", "task-unclaimed", &TaskResult{Output: "done"})
	if err == nil {
		t.Fatal("expected error for completing unclaimed task")
	}
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("expected ErrTaskInvalidState, got %v", err)
	}
}

func TestCompleteTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CompleteTask(context.Background(), "tenant-a", "nonexistent", &TaskResult{})
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestCompleteTask_EmptyTaskID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CompleteTask(context.Background(), "tenant-a", "", &TaskResult{})
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// FailTask Tests
// =============================================================================

func TestFailTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-fail",
		TenantID: "tenant-a",
		Subject:  "Fail me",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}

	err = store.ClaimTask(context.Background(), "tenant-a", "task-fail", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	err = store.FailTask(context.Background(), "tenant-a", "task-fail", "compilation error")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := store.GetTask(context.Background(), "tenant-a", "task-fail")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Errorf("expected status failed, got %s", got.Status)
	}
	if got.Result == nil || got.Result.Output != "compilation error" {
		t.Errorf("expected failure reason in result, got %v", got.Result)
	}
}

func TestFailTask_NotClaimed(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-fail-unclaimed",
		TenantID: "tenant-a",
		Subject:  "Unclaimed",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.FailTask(context.Background(), "tenant-a", "task-fail-unclaimed", "reason")
	if err == nil {
		t.Fatal("expected error for failing unclaimed task")
	}
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("expected ErrTaskInvalidState, got %v", err)
	}
}

func TestFailTask_EmptyReason(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.FailTask(context.Background(), "tenant-a", "task-1", "")
	if err == nil {
		t.Fatal("expected error for empty reason")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestFailTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.FailTask(context.Background(), "tenant-a", "nonexistent", "reason")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

// =============================================================================
// DeleteTask Tests
// =============================================================================

func TestDeleteTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-delete",
		TenantID: "tenant-a",
		Subject:  "Delete me",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.DeleteTask(context.Background(), "tenant-a", "task-delete")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify deletion
	_, err = store.GetTask(context.Background(), "tenant-a", "task-delete")
	if err == nil {
		t.Fatal("expected error after deletion")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.DeleteTask(context.Background(), "tenant-a", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestDeleteTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.DeleteTask(context.Background(), "", "task-1")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// ListTasks Tests
// =============================================================================

func TestListTasks_FilterByStatus(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t1", TenantID: "tenant-a", Subject: "Task 1", Status: TaskStatusPending},
		{ID: "t2", TenantID: "tenant-a", Subject: "Task 2", Status: TaskStatusPending},
		{ID: "t3", TenantID: "tenant-a", Subject: "Task 3"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	// Claim t1 to change its status
	err := store.ClaimTask(ctx, "tenant-a", "t1", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	// Filter for pending only
	filter := &TaskFilter{Status: TaskStatusPending}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 pending tasks, got %d", len(results))
	}

	for _, r := range results {
		if r.Status != TaskStatusPending {
			t.Errorf("expected all results to be pending, got %s for task %s", r.Status, r.ID)
		}
	}
}

func TestListTasks_FilterByTeam(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t1", TenantID: "tenant-a", TeamName: "backend", Subject: "Backend task"},
		{ID: "t2", TenantID: "tenant-a", TeamName: "frontend", Subject: "Frontend task"},
		{ID: "t3", TenantID: "tenant-a", TeamName: "backend", Subject: "Another backend"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	filter := &TaskFilter{TeamName: "backend"}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 backend tasks, got %d", len(results))
	}

	for _, r := range results {
		if r.TeamName != "backend" {
			t.Errorf("expected all results to be team 'backend', got %q for task %s", r.TeamName, r.ID)
		}
	}
}

func TestListTasks_FilterByAssignee(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t1", TenantID: "tenant-a", Subject: "Task 1"},
		{ID: "t2", TenantID: "tenant-a", Subject: "Task 2"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	// Claim only t1
	err := store.ClaimTask(ctx, "tenant-a", "t1", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	filter := &TaskFilter{AssignedTo: "agent-1"}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 task assigned to agent-1, got %d", len(results))
	}
	if results[0].ID != "t1" {
		t.Errorf("expected task t1, got %s", results[0].ID)
	}
}

func TestListTasks_TenantIsolation(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "ta-1", TenantID: "tenant-a", Subject: "Tenant A task 1"},
		{ID: "ta-2", TenantID: "tenant-a", Subject: "Tenant A task 2"},
		{ID: "tb-1", TenantID: "tenant-b", Subject: "Tenant B task"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	// List tenant-a tasks
	resultsA, err := store.ListTasks(ctx, "tenant-a", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resultsA) != 2 {
		t.Fatalf("expected 2 tasks for tenant-a, got %d", len(resultsA))
	}

	// List tenant-b tasks
	resultsB, err := store.ListTasks(ctx, "tenant-b", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resultsB) != 1 {
		t.Fatalf("expected 1 task for tenant-b, got %d", len(resultsB))
	}

	// Verify no cross-contamination
	for _, r := range resultsA {
		if r.TenantID != "tenant-a" {
			t.Errorf("tenant-a list contains task from tenant %q", r.TenantID)
		}
	}
	for _, r := range resultsB {
		if r.TenantID != "tenant-b" {
			t.Errorf("tenant-b list contains task from tenant %q", r.TenantID)
		}
	}
}

func TestListTasks_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.ListTasks(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestListTasks_NilFilter(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	err := store.CreateTask(ctx, &Task{ID: "t1", TenantID: "tenant-a", Subject: "Task"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	results, err := store.ListTasks(ctx, "tenant-a", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 task with nil filter, got %d", len(results))
	}
}

func TestListTasks_WithLimit(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := "t" + string(rune('0'+i))
		err := store.CreateTask(ctx, &Task{ID: id, TenantID: "tenant-a", Subject: "Task"})
		if err != nil {
			t.Fatalf("setup create: %v", err)
		}
	}

	filter := &TaskFilter{Limit: 3}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) > 3 {
		t.Fatalf("expected at most 3 tasks with limit, got %d", len(results))
	}
}

// =============================================================================
// DeleteTenantTasks Tests
// =============================================================================

func TestDeleteTenantTasks_CascadeDelete(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	// Create tasks for two tenants
	tasks := []*Task{
		{ID: "ta-1", TenantID: "tenant-a", Subject: "Tenant A task 1"},
		{ID: "ta-2", TenantID: "tenant-a", Subject: "Tenant A task 2"},
		{ID: "ta-3", TenantID: "tenant-a", Subject: "Tenant A task 3"},
		{ID: "tb-1", TenantID: "tenant-b", Subject: "Tenant B task"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	// Delete all tenant-a tasks
	err := store.DeleteTenantTasks(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify tenant-a tasks are gone
	resultsA, err := store.ListTasks(ctx, "tenant-a", nil)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(resultsA) != 0 {
		t.Fatalf("expected 0 tasks for tenant-a after cascade delete, got %d", len(resultsA))
	}

	// Verify tenant-b tasks remain
	resultsB, err := store.ListTasks(ctx, "tenant-b", nil)
	if err != nil {
		t.Fatalf("list tenant-b: %v", err)
	}
	if len(resultsB) != 1 {
		t.Fatalf("expected 1 task for tenant-b after tenant-a delete, got %d", len(resultsB))
	}
}

func TestDeleteTenantTasks_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.DeleteTenantTasks(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestDeleteTenantTasks_NoTasksForTenant(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	// Should succeed even if no tasks exist
	err := store.DeleteTenantTasks(context.Background(), "nonexistent-tenant")
	if err != nil {
		t.Fatalf("expected no error for empty tenant, got %v", err)
	}
}

// =============================================================================
// Task Dependencies Tests
// =============================================================================

func TestTaskDependencies_BlockedUntilResolved(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	// Create dependency task
	depTask := &Task{
		ID:       "dep-task",
		TenantID: "tenant-a",
		Subject:  "Dependency",
	}
	err := store.CreateTask(ctx, depTask)
	if err != nil {
		t.Fatalf("setup create dep: %v", err)
	}

	// Create dependent task
	dependentTask := &Task{
		ID:           "blocked-task",
		TenantID:     "tenant-a",
		Subject:      "Blocked task",
		Dependencies: []string{"dep-task"},
	}
	err = store.CreateTask(ctx, dependentTask)
	if err != nil {
		t.Fatalf("setup create dependent: %v", err)
	}

	// Try to claim the blocked task - should fail because dep is not completed
	err = store.ClaimTask(ctx, "tenant-a", "blocked-task", "agent-1")
	if err == nil {
		t.Fatal("expected error for blocked task")
	}
	if !errors.Is(err, ErrTaskDependencyBlocked) {
		t.Fatalf("expected ErrTaskDependencyBlocked, got %v", err)
	}

	// Complete the dependency
	err = store.ClaimTask(ctx, "tenant-a", "dep-task", "agent-1")
	if err != nil {
		t.Fatalf("claim dep: %v", err)
	}
	err = store.CompleteTask(ctx, "tenant-a", "dep-task", &TaskResult{Output: "done"})
	if err != nil {
		t.Fatalf("complete dep: %v", err)
	}

	// Now claiming the dependent task should succeed
	err = store.ClaimTask(ctx, "tenant-a", "blocked-task", "agent-1")
	if err != nil {
		t.Fatalf("expected claim to succeed after dependency resolved, got %v", err)
	}

	got, err := store.GetTask(ctx, "tenant-a", "blocked-task")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Status != TaskStatusClaimed {
		t.Errorf("expected status claimed, got %s", got.Status)
	}
}

func TestTaskDependencies_NonexistentDependency(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:           "task-bad-dep",
		TenantID:     "tenant-a",
		Subject:      "Bad dependency",
		Dependencies: []string{"nonexistent-dep"},
	}
	err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.ClaimTask(ctx, "tenant-a", "task-bad-dep", "agent-1")
	if err == nil {
		t.Fatal("expected error for nonexistent dependency")
	}
	if !errors.Is(err, ErrTaskDependencyBlocked) {
		t.Fatalf("expected ErrTaskDependencyBlocked, got %v", err)
	}
}

// =============================================================================
// GenerateTaskID Tests
// =============================================================================

func TestGenerateTaskID_Unique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id, err := GenerateTaskID()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if id == "" {
			t.Fatal("expected non-empty ID")
		}
		if len(id) != 32 { // 16 bytes = 32 hex chars
			t.Fatalf("expected 32 hex chars, got %d: %q", len(id), id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestTaskNotFoundError(t *testing.T) {
	t.Parallel()

	err := &TaskNotFoundError{TenantID: "t1", TaskID: "task-1"}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected errors.Is to match ErrTaskNotFound")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestTaskAlreadyExistsError(t *testing.T) {
	t.Parallel()

	err := &TaskAlreadyExistsError{TaskID: "task-1"}
	if !errors.Is(err, ErrTaskAlreadyExists) {
		t.Fatalf("expected errors.Is to match ErrTaskAlreadyExists")
	}
}

func TestTaskAlreadyClaimedError(t *testing.T) {
	t.Parallel()

	err := &TaskAlreadyClaimedError{TaskID: "task-1", ClaimedBy: "a1", RequestingBy: "a2"}
	if !errors.Is(err, ErrTaskAlreadyClaimed) {
		t.Fatalf("expected errors.Is to match ErrTaskAlreadyClaimed")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestTaskVersionConflictError(t *testing.T) {
	t.Parallel()

	err := &TaskVersionConflictError{TaskID: "task-1", ExpectedVersion: 1, ActualVersion: 2}
	if !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("expected errors.Is to match ErrTaskVersionConflict")
	}
}

func TestTaskDependencyBlockedError(t *testing.T) {
	t.Parallel()

	err := &TaskDependencyBlockedError{TaskID: "task-1", UnresolvedDependency: "dep-1"}
	if !errors.Is(err, ErrTaskDependencyBlocked) {
		t.Fatalf("expected errors.Is to match ErrTaskDependencyBlocked")
	}
}

func TestTaskInvalidStateError(t *testing.T) {
	t.Parallel()

	err := &TaskInvalidStateError{TaskID: "task-1", CurrentState: TaskStatusPending, TargetState: TaskStatusCompleted}
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("expected errors.Is to match ErrTaskInvalidState")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestTaskStoreError(t *testing.T) {
	t.Parallel()

	err := &TaskStoreError{Sentinel: ErrTaskStoreInvalidInput, Message: "test message"}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected errors.Is to match ErrTaskStoreInvalidInput")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}

	// Test without message
	errNoMsg := &TaskStoreError{Sentinel: ErrTaskStoreMarshal}
	if errNoMsg.Error() != ErrTaskStoreMarshal.Error() {
		t.Errorf("expected sentinel error string, got %q", errNoMsg.Error())
	}
}

// =============================================================================
// InProgress State Tests
// =============================================================================

func TestCompleteTask_FromInProgress(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:       "task-progress",
		TenantID: "tenant-a",
		Subject:  "In progress task",
	}

	err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}

	// Claim then update to in_progress
	err = store.ClaimTask(ctx, "tenant-a", "task-progress", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	current, err := store.GetTask(ctx, "tenant-a", "task-progress")
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}
	current.Status = TaskStatusInProgress
	err = store.UpdateTask(ctx, current)
	if err != nil {
		t.Fatalf("setup update to in_progress: %v", err)
	}

	// Complete from in_progress
	err = store.CompleteTask(ctx, "tenant-a", "task-progress", &TaskResult{Output: "done"})
	if err != nil {
		t.Fatalf("expected no error completing from in_progress, got %v", err)
	}

	got, err := store.GetTask(ctx, "tenant-a", "task-progress")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
}

func TestFailTask_FromInProgress(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:       "task-fail-progress",
		TenantID: "tenant-a",
		Subject:  "Fail from in progress",
	}

	err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}

	err = store.ClaimTask(ctx, "tenant-a", "task-fail-progress", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	current, err := store.GetTask(ctx, "tenant-a", "task-fail-progress")
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}
	current.Status = TaskStatusInProgress
	err = store.UpdateTask(ctx, current)
	if err != nil {
		t.Fatalf("setup update: %v", err)
	}

	err = store.FailTask(ctx, "tenant-a", "task-fail-progress", "timeout")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := store.GetTask(ctx, "tenant-a", "task-fail-progress")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Errorf("expected failed, got %s", got.Status)
	}
}

// =============================================================================
// ListTasks FilterByCreatedBy Tests
// =============================================================================

func TestListTasks_FilterByCreatedBy(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t1", TenantID: "tenant-a", Subject: "Task 1", CreatedBy: "user-1"},
		{ID: "t2", TenantID: "tenant-a", Subject: "Task 2", CreatedBy: "user-2"},
		{ID: "t3", TenantID: "tenant-a", Subject: "Task 3", CreatedBy: "user-1"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	filter := &TaskFilter{CreatedBy: "user-1"}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tasks created by user-1, got %d", len(results))
	}
	for _, r := range results {
		if r.CreatedBy != "user-1" {
			t.Errorf("expected created_by user-1, got %q", r.CreatedBy)
		}
	}
}
