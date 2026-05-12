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

// claimAuthzSetup creates a pre-wired AgentService for ClaimTask authz tests.
// The project store is seeded with a single project whose AgentRoles are
// provided via the roles map (agentID → role). The returned mockTaskStore
// has getTask pre-set to a project-scoped task.
func claimAuthzSetup(t *testing.T, roles map[string]ProjectRole) (AgentService, *mockTaskStore, string, string) {
	t.Helper()

	const (
		tenantID  = "tenant-1"
		projectID = "proj-1"
	)

	// Build and seed the project store.
	projectStore := NewInMemoryProjectStore()
	proj := &Project{
		ID:       projectID,
		TenantID: tenantID,
		Name:     "test-project",
		Permissions: ProjectPermissions{
			AgentRoles: roles,
		},
	}
	if err := projectStore.Create(context.Background(), proj); err != nil {
		t.Fatalf("failed to seed project store: %v", err)
	}

	// Build the service.
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create AgentService: %v", err)
	}

	// Pre-seed the mock task store with a project-scoped task.
	taskStore := &mockTaskStore{
		getTask: &Task{
			ID:        "task-1",
			TenantID:  tenantID,
			ProjectID: projectID,
			Subject:   "test task",
		},
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(taskStore)
	impl.SetProjectAuthz(NewProjectAuthz(projectStore))

	return svc, taskStore, tenantID, projectID
}

// TestClaimTask_WorkerRole_Allowed verifies that an agent with the worker role
// can claim a project-scoped task.
func TestClaimTask_WorkerRole_Allowed(t *testing.T) {
	t.Parallel()

	const agentID = "agent-worker"
	svc, _, tenantID, _ := claimAuthzSetup(t, map[string]ProjectRole{
		agentID: ProjectRoleWorker,
	})

	if err := svc.ClaimTask(context.Background(), tenantID, "task-1", agentID); err != nil {
		t.Errorf("expected worker role to be allowed, got: %v", err)
	}
}

// TestClaimTask_LeadRole_Allowed verifies that a lead agent (higher privilege
// than worker) can also claim a project-scoped task.
func TestClaimTask_LeadRole_Allowed(t *testing.T) {
	t.Parallel()

	const agentID = "agent-lead"
	svc, _, tenantID, _ := claimAuthzSetup(t, map[string]ProjectRole{
		agentID: ProjectRoleLead,
	})

	if err := svc.ClaimTask(context.Background(), tenantID, "task-1", agentID); err != nil {
		t.Errorf("expected lead role to be allowed, got: %v", err)
	}
}

// TestClaimTask_ObserverRole_Rejected verifies that an observer agent cannot
// claim a project-scoped task (observer < worker).
func TestClaimTask_ObserverRole_Rejected(t *testing.T) {
	t.Parallel()

	const agentID = "agent-observer"
	svc, _, tenantID, _ := claimAuthzSetup(t, map[string]ProjectRole{
		agentID: ProjectRoleObserver,
	})

	err := svc.ClaimTask(context.Background(), tenantID, "task-1", agentID)
	if err == nil {
		t.Fatal("expected access denied for observer role, got nil")
	}
	if !errors.Is(err, ErrProjectAccessDenied) {
		t.Errorf("expected ErrProjectAccessDenied, got %T: %v", err, err)
	}
}

// TestClaimTask_DeniedRole_Rejected verifies that an explicitly denied agent
// cannot claim a project-scoped task.
func TestClaimTask_DeniedRole_Rejected(t *testing.T) {
	t.Parallel()

	const agentID = "agent-denied"
	svc, _, tenantID, _ := claimAuthzSetup(t, map[string]ProjectRole{
		agentID: ProjectRoleDenied,
	})

	err := svc.ClaimTask(context.Background(), tenantID, "task-1", agentID)
	if err == nil {
		t.Fatal("expected access denied for denied role, got nil")
	}
	if !errors.Is(err, ErrProjectAccessDenied) {
		t.Errorf("expected ErrProjectAccessDenied, got %T: %v", err, err)
	}
}

// TestClaimTask_NoProjectID_LegacyAllowAll verifies that when a task has no
// ProjectID the authz check is skipped (backward-compatible allow-all behavior).
func TestClaimTask_NoProjectID_LegacyAllowAll(t *testing.T) {
	t.Parallel()

	const agentID = "some-agent"
	svc, store, tenantID, _ := claimAuthzSetup(t, map[string]ProjectRole{})

	// Override the pre-seeded task to have no ProjectID (unscoped).
	store.mu.Lock()
	store.getTask = &Task{
		ID:       "task-unscoped",
		TenantID: tenantID,
		Subject:  "unscoped task",
		// ProjectID intentionally empty.
	}
	store.mu.Unlock()

	// Agent has no explicit role but the task is not project-scoped, so
	// ClaimTask must not perform an authz check.
	if err := svc.ClaimTask(context.Background(), tenantID, "task-unscoped", agentID); err != nil {
		t.Errorf("expected legacy allow-all for unscoped task, got: %v", err)
	}
}

// TestClaimTask_NoProjectAuthz_AllowAll verifies that when no ProjectAuthz is
// configured (SetProjectAuthz never called) all claims are allowed.
func TestClaimTask_NoProjectAuthz_AllowAll(t *testing.T) {
	t.Parallel()

	const (
		tenantID = "tenant-x"
		agentID  = "any-agent"
	)

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create AgentService: %v", err)
	}

	store := &mockTaskStore{
		getTask: &Task{
			ID:        "task-no-authz",
			TenantID:  tenantID,
			ProjectID: "some-project",
			Subject:   "project task with no authz",
		},
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	// No SetProjectAuthz call — projectAuthz is nil.

	if err := svc.ClaimTask(context.Background(), tenantID, "task-no-authz", agentID); err != nil {
		t.Errorf("expected allow-all when no authz configured, got: %v", err)
	}
}
