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

//go:build integration && rag

package orchestration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestE2E_TaskStore_CreateAndGet verifies that creating a task through the
// AgentService and retrieving it by ID returns the correct data.
func TestE2E_TaskStore_CreateAndGet(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-create-get"

	task := createTestTask(t, svc, tenantID, "Implement REST endpoint", "backend-team")

	// Retrieve the task by ID.
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	retrieved, err := svc.GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err, "GetTask should succeed")
	require.NotNil(t, retrieved)

	assert.Equal(t, task.ID, retrieved.ID)
	assert.Equal(t, tenantID, retrieved.TenantID)
	assert.Equal(t, "Implement REST endpoint", retrieved.Subject)
	assert.Equal(t, "backend-team", retrieved.TeamName)
	assert.Equal(t, orchestration.TaskStatusPending, retrieved.Status)
	assert.NotZero(t, retrieved.CreatedAt, "created_at should be set")
	assert.Equal(t, uint64(1), retrieved.Version, "initial version should be 1")
}

// TestE2E_TaskStore_CreateAndGet_NotFound verifies that retrieving a
// nonexistent task returns a TaskNotFound error.
func TestE2E_TaskStore_CreateAndGet_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-not-found"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	_, err := svc.GetTask(ctx, tenantID, "nonexistent-task-id")
	require.Error(t, err, "GetTask for nonexistent task should fail")
	assert.ErrorIs(t, err, orchestration.ErrTaskNotFound)
}

// TestE2E_TaskStore_ListWithFilters verifies that listing tasks with status,
// team, and assignee filters returns the correct subset.
func TestE2E_TaskStore_ListWithFilters(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-list-filters"

	// Create tasks with different teams and statuses.
	task1 := createTestTask(t, svc, tenantID, "Backend task 1", "backend-team")
	createTestTask(t, svc, tenantID, "Frontend task 1", "frontend-team")
	createTestTask(t, svc, tenantID, "Backend task 2", "backend-team")

	// Claim task1 to change its status.
	claimTestTask(t, svc, tenantID, task1.ID, "agent-filter-01")

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Filter by team name.
	t.Run("FilterByTeam", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			TeamName: "backend-team",
		})
		require.NoError(t, err)
		assert.Len(t, tasks, 2, "should find 2 backend-team tasks")
		for _, task := range tasks {
			assert.Equal(t, "backend-team", task.TeamName)
		}
	})

	// Filter by status.
	t.Run("FilterByStatus", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			Status: orchestration.TaskStatusPending,
		})
		require.NoError(t, err)
		assert.Len(t, tasks, 2, "should find 2 pending tasks")
		for _, task := range tasks {
			assert.Equal(t, orchestration.TaskStatusPending, task.Status)
		}
	})

	// Filter by assignee.
	t.Run("FilterByAssignee", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			AssignedTo: "agent-filter-01",
		})
		require.NoError(t, err)
		assert.Len(t, tasks, 1, "should find 1 task assigned to agent-filter-01")
		assert.Equal(t, task1.ID, tasks[0].ID)
	})

	// Filter by team + status.
	t.Run("FilterByTeamAndStatus", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			TeamName: "backend-team",
			Status:   orchestration.TaskStatusClaimed,
		})
		require.NoError(t, err)
		assert.Len(t, tasks, 1, "should find 1 claimed backend-team task")
	})
}

// TestE2E_TaskStore_ClaimTask verifies that claiming a pending task succeeds
// and transitions the task to claimed status with the correct agent ID.
func TestE2E_TaskStore_ClaimTask(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-claim"

	task := createTestTask(t, svc, tenantID, "Task to claim", "ops-team")
	claimTestTask(t, svc, tenantID, task.ID, "agent-claim-01")

	// Verify the task is now claimed.
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	claimed, err := svc.GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, orchestration.TaskStatusClaimed, claimed.Status)
	assert.Equal(t, "agent-claim-01", claimed.AssignedTo)
	assert.NotZero(t, claimed.ClaimedAt, "claimed_at should be set")
	assert.True(t, claimed.Version > 1, "version should be incremented after claim")
}

// TestE2E_TaskStore_ClaimAlreadyClaimed verifies that attempting to claim a
// task that is already claimed by another agent returns a conflict error.
func TestE2E_TaskStore_ClaimAlreadyClaimed(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-double-claim"

	task := createTestTask(t, svc, tenantID, "Task for double claim", "ops-team")
	claimTestTask(t, svc, tenantID, task.ID, "agent-first")

	// Second claim by a different agent should fail.
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.ClaimTask(ctx, tenantID, task.ID, "agent-second")
	require.Error(t, err, "double-claim should fail")
	assert.ErrorIs(t, err, orchestration.ErrTaskAlreadyClaimed)
}

// TestE2E_TaskStore_CompleteTask verifies that completing a claimed task
// transitions it to completed status with the result stored.
func TestE2E_TaskStore_CompleteTask(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-complete"

	task := createTestTask(t, svc, tenantID, "Task to complete", "dev-team")
	claimTestTask(t, svc, tenantID, task.ID, "agent-complete-01")

	// Complete the task.
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.CompleteTask(ctx, tenantID, task.ID, &orchestration.TaskResult{
		Output:   "feature implemented",
		Files:    []string{"main.go", "main_test.go"},
		Metadata: map[string]string{"lines_changed": "42"},
	})
	require.NoError(t, err, "CompleteTask should succeed")

	// Verify the task is completed with result.
	completed, err := svc.GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, orchestration.TaskStatusCompleted, completed.Status)
	assert.NotZero(t, completed.CompletedAt, "completed_at should be set")
	require.NotNil(t, completed.Result)
	assert.Equal(t, "feature implemented", completed.Result.Output)
	assert.Equal(t, []string{"main.go", "main_test.go"}, completed.Result.Files)
	assert.Equal(t, "42", completed.Result.Metadata["lines_changed"])
}

// TestE2E_TaskStore_CompleteTask_NotClaimed verifies that completing a task
// that has not been claimed returns an invalid state error.
func TestE2E_TaskStore_CompleteTask_NotClaimed(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-complete-unclaimed"

	task := createTestTask(t, svc, tenantID, "Unclaimed task", "dev-team")

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.CompleteTask(ctx, tenantID, task.ID, &orchestration.TaskResult{
		Output: "should fail",
	})
	require.Error(t, err, "completing an unclaimed task should fail")
	assert.ErrorIs(t, err, orchestration.ErrTaskInvalidState)
}

// TestE2E_TaskStore_TaskLifecycle exercises the full task lifecycle:
// create -> claim -> complete, verifying state at each transition.
func TestE2E_TaskStore_TaskLifecycle(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-lifecycle"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Step 1: Create.
	task := createTestTask(t, svc, tenantID, "Lifecycle test task", "platform-team")
	assert.Equal(t, orchestration.TaskStatusPending, task.Status)
	assert.Empty(t, task.AssignedTo)

	// Step 2: List - should be pending.
	pendingTasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
		Status: orchestration.TaskStatusPending,
	})
	require.NoError(t, err)
	found := false
	for _, pt := range pendingTasks {
		if pt.ID == task.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "newly created task should appear in pending list")

	// Step 3: Claim.
	claimTestTask(t, svc, tenantID, task.ID, "lifecycle-agent")
	claimed, err := svc.GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, orchestration.TaskStatusClaimed, claimed.Status)
	assert.Equal(t, "lifecycle-agent", claimed.AssignedTo)

	// Step 4: Complete.
	completeTestTask(t, svc, tenantID, task.ID)
	completed, err := svc.GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, orchestration.TaskStatusCompleted, completed.Status)
	assert.NotZero(t, completed.CompletedAt)

	// Step 5: Completed tasks should no longer appear in pending filter.
	pendingAfter, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
		Status: orchestration.TaskStatusPending,
	})
	require.NoError(t, err)
	for _, pt := range pendingAfter {
		assert.NotEqual(t, task.ID, pt.ID, "completed task should not appear in pending filter")
	}
}

// TestE2E_TaskStore_CreateTask_MissingSubject verifies that creating a task
// with an empty subject returns ErrTaskSubjectRequired.
func TestE2E_TaskStore_CreateTask_MissingSubject(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-missing-subject"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	_, err := svc.CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID: tenantID,
		Subject:  "",
		TeamName: "backend-team",
	})
	require.Error(t, err, "CreateTask with empty subject should fail")
	assert.ErrorIs(t, err, orchestration.ErrTaskSubjectRequired)
}

// TestE2E_TaskStore_CreateTask_MissingTenantID verifies that creating a task
// with an empty TenantID returns ErrTenantIDRequired.
func TestE2E_TaskStore_CreateTask_MissingTenantID(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "")
	_, err := svc.CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID: "",
		Subject:  "Some task",
		TeamName: "backend-team",
	})
	require.Error(t, err, "CreateTask with empty TenantID should fail")

	var tenantRequired *orchestration.TenantRequiredError
	assert.ErrorAs(t, err, &tenantRequired, "error should be TenantRequiredError")
}

// TestE2E_TaskStore_GetTask_NotFound verifies that retrieving a task with
// a non-existent ID returns ErrTaskNotFound.
func TestE2E_TaskStore_GetTask_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-get-notfound"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	_, err := svc.GetTask(ctx, tenantID, "does-not-exist-12345")
	require.Error(t, err, "GetTask with nonexistent ID should fail")
	assert.ErrorIs(t, err, orchestration.ErrTaskNotFound)
}

// TestE2E_TaskStore_ClaimTask_NotFound verifies that claiming a task with
// a non-existent ID returns ErrTaskNotFound.
func TestE2E_TaskStore_ClaimTask_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-claim-notfound"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.ClaimTask(ctx, tenantID, "does-not-exist-12345", "agent-01")
	require.Error(t, err, "ClaimTask with nonexistent ID should fail")
	assert.ErrorIs(t, err, orchestration.ErrTaskNotFound)
}

// TestE2E_TaskStore_CompleteTask_NotFound verifies that completing a task with
// a non-existent ID returns ErrTaskNotFound.
func TestE2E_TaskStore_CompleteTask_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-complete-notfound"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.CompleteTask(ctx, tenantID, "does-not-exist-12345", &orchestration.TaskResult{
		Output: "should fail",
	})
	require.Error(t, err, "CompleteTask with nonexistent ID should fail")
	assert.ErrorIs(t, err, orchestration.ErrTaskNotFound)
}

// TestE2E_TaskStore_ListTasks_FilterByStatus verifies that ListTasks correctly
// filters tasks by status after state transitions.
func TestE2E_TaskStore_ListTasks_FilterByStatus(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-filter-status"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Create 3 tasks: one stays pending, one gets claimed, one gets completed.
	pendingTask := createTestTask(t, svc, tenantID, "Pending task", "team-a")
	claimedTask := createTestTask(t, svc, tenantID, "Claimed task", "team-a")
	completedTask := createTestTask(t, svc, tenantID, "Completed task", "team-a")

	claimTestTask(t, svc, tenantID, claimedTask.ID, "agent-status-01")
	claimTestTask(t, svc, tenantID, completedTask.ID, "agent-status-02")
	completeTestTask(t, svc, tenantID, completedTask.ID)

	// Filter by pending - should only return the unclaimed task.
	t.Run("FilterPending", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			Status: orchestration.TaskStatusPending,
		})
		require.NoError(t, err)
		require.Len(t, tasks, 1, "should find exactly 1 pending task")
		assert.Equal(t, pendingTask.ID, tasks[0].ID)
		assert.Equal(t, orchestration.TaskStatusPending, tasks[0].Status)
	})

	// Filter by claimed - should only return the claimed task.
	t.Run("FilterClaimed", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			Status: orchestration.TaskStatusClaimed,
		})
		require.NoError(t, err)
		require.Len(t, tasks, 1, "should find exactly 1 claimed task")
		assert.Equal(t, claimedTask.ID, tasks[0].ID)
		assert.Equal(t, orchestration.TaskStatusClaimed, tasks[0].Status)
	})

	// Filter by completed - should only return the completed task.
	t.Run("FilterCompleted", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			Status: orchestration.TaskStatusCompleted,
		})
		require.NoError(t, err)
		require.Len(t, tasks, 1, "should find exactly 1 completed task")
		assert.Equal(t, completedTask.ID, tasks[0].ID)
		assert.Equal(t, orchestration.TaskStatusCompleted, tasks[0].Status)
	})
}

// TestE2E_TaskStore_ListTasks_FilterByTeam verifies that ListTasks correctly
// filters tasks by team name, returning only the matching subset.
func TestE2E_TaskStore_ListTasks_FilterByTeam(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-filter-team"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Create tasks in different teams.
	createTestTask(t, svc, tenantID, "Alpha task 1", "alpha-team")
	createTestTask(t, svc, tenantID, "Alpha task 2", "alpha-team")
	createTestTask(t, svc, tenantID, "Beta task 1", "beta-team")

	// Filter by alpha-team.
	t.Run("FilterAlphaTeam", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			TeamName: "alpha-team",
		})
		require.NoError(t, err)
		assert.Len(t, tasks, 2, "should find 2 alpha-team tasks")
		for _, task := range tasks {
			assert.Equal(t, "alpha-team", task.TeamName)
		}
	})

	// Filter by beta-team.
	t.Run("FilterBetaTeam", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			TeamName: "beta-team",
		})
		require.NoError(t, err)
		assert.Len(t, tasks, 1, "should find 1 beta-team task")
		assert.Equal(t, "beta-team", tasks[0].TeamName)
	})

	// Filter by non-existent team.
	t.Run("FilterNonexistentTeam", func(t *testing.T) {
		tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
			TeamName: "nonexistent-team",
		})
		require.NoError(t, err)
		assert.Empty(t, tasks, "should find no tasks for nonexistent team")
	})
}

// TestE2E_TaskStore_ListTasks_Empty verifies that ListTasks returns an empty
// slice (not nil) when no tasks exist for a tenant.
func TestE2E_TaskStore_ListTasks_Empty(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-list-empty-no-tasks"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	tasks, err := svc.ListTasks(ctx, tenantID, &orchestration.TaskFilter{})
	require.NoError(t, err, "ListTasks should succeed even with no tasks")
	require.NotNil(t, tasks, "ListTasks should return empty slice, not nil")
	assert.Empty(t, tasks, "should return zero tasks for a fresh tenant")
}

// TestE2E_TaskStore_TaskWithMetadata verifies that task metadata is preserved
// through the Create->Get roundtrip.
func TestE2E_TaskStore_TaskWithMetadata(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-metadata"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	metadata := map[string]string{
		"priority": "high",
		"source":   "ci-pipeline",
		"region":   "us-east-1",
	}

	task, err := svc.CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID: tenantID,
		Subject:  "Task with metadata",
		TeamName: "infra-team",
		Metadata: metadata,
	})
	require.NoError(t, err)
	require.NotNil(t, task)

	// Retrieve and verify metadata roundtrip.
	retrieved, err := svc.GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved.Metadata, "metadata should not be nil")
	assert.Equal(t, "high", retrieved.Metadata["priority"])
	assert.Equal(t, "ci-pipeline", retrieved.Metadata["source"])
	assert.Equal(t, "us-east-1", retrieved.Metadata["region"])
	assert.Len(t, retrieved.Metadata, 3, "metadata should have exactly 3 entries")
}

// TestE2E_TaskStore_TaskWithDependencies verifies that task dependencies are
// preserved through the Create->Get roundtrip.
func TestE2E_TaskStore_TaskWithDependencies(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-dependencies"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Create prerequisite tasks first.
	dep1 := createTestTask(t, svc, tenantID, "Dependency 1", "build-team")
	dep2 := createTestTask(t, svc, tenantID, "Dependency 2", "build-team")

	// Create a task that depends on both prerequisites.
	task, err := svc.CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:     tenantID,
		Subject:      "Task with dependencies",
		TeamName:     "build-team",
		Dependencies: []string{dep1.ID, dep2.ID},
	})
	require.NoError(t, err)
	require.NotNil(t, task)

	// Retrieve and verify dependencies roundtrip.
	retrieved, err := svc.GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved.Dependencies, "dependencies should not be nil")
	assert.Len(t, retrieved.Dependencies, 2, "should have exactly 2 dependencies")
	assert.Contains(t, retrieved.Dependencies, dep1.ID)
	assert.Contains(t, retrieved.Dependencies, dep2.ID)
}

// TestE2E_TaskStore_CompleteTask_WithResult verifies that completing a task
// with a TaskResult that has Output persists through a subsequent GetTask.
func TestE2E_TaskStore_CompleteTask_WithResult(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-complete-result"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	task := createTestTask(t, svc, tenantID, "Task with detailed result", "qa-team")
	claimTestTask(t, svc, tenantID, task.ID, "agent-result-01")

	// Complete with a detailed result.
	result := &orchestration.TaskResult{
		Output:   "All 47 tests passed with 92% coverage",
		Files:    []string{"report.html", "coverage.out"},
		Metadata: map[string]string{"duration_ms": "1234", "test_count": "47"},
	}
	err := svc.CompleteTask(ctx, tenantID, task.ID, result)
	require.NoError(t, err, "CompleteTask with result should succeed")

	// Verify result roundtrip.
	completed, err := svc.GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, orchestration.TaskStatusCompleted, completed.Status)
	require.NotNil(t, completed.Result, "result should not be nil after completion")
	assert.Equal(t, "All 47 tests passed with 92% coverage", completed.Result.Output)
	assert.Equal(t, []string{"report.html", "coverage.out"}, completed.Result.Files)
	assert.Equal(t, "1234", completed.Result.Metadata["duration_ms"])
	assert.Equal(t, "47", completed.Result.Metadata["test_count"])
}

// TestE2E_TaskStore_ClaimTask_MissingAgentID verifies that claiming a task
// with an empty agentID returns ErrAgentIDRequired.
func TestE2E_TaskStore_ClaimTask_MissingAgentID(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-claim-no-agent"

	task := createTestTask(t, svc, tenantID, "Task for missing agent", "ops-team")

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.ClaimTask(ctx, tenantID, task.ID, "")
	require.Error(t, err, "ClaimTask with empty agentID should fail")
	assert.ErrorIs(t, err, orchestration.ErrAgentIDRequired)
}

// TestE2E_TaskStore_ClaimTask_MissingTaskID verifies that claiming a task
// with an empty taskID returns ErrTaskIDRequired.
func TestE2E_TaskStore_ClaimTask_MissingTaskID(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "task-tenant-claim-no-taskid"

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.ClaimTask(ctx, tenantID, "", "agent-01")
	require.Error(t, err, "ClaimTask with empty taskID should fail")
	assert.ErrorIs(t, err, orchestration.ErrTaskIDRequired)
}
