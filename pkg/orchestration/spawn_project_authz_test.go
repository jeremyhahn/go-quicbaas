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
)

// =============================================================================
// The project boundary on the dispatch path
// =============================================================================
//
// A DISPATCH HANDS THE WORK OVER; NOTHING CLAIMS IT AFTERWARDS. The claim path
// asks the project's role matrix whether the agent may take the task, so the
// dispatch path must ask the same question before it assigns one — otherwise
// the matrix governs the door an agent knocks on and not the one work is
// carried through (go-quicbaas#634).

// spawnAuthzService builds an agent service wired the way a dispatch needs:
// a task store, an agent registry holding the given agents, and the project
// authorizer over a store seeded with project (nil for "no such project").
func spawnAuthzService(t *testing.T, project *Project, agents ...*FleetNode) (*agentServiceImpl, *mockTaskStore) {
	t.Helper()

	projectStore := NewInMemoryProjectStore()
	if project != nil {
		if err := projectStore.Create(context.Background(), project); err != nil {
			t.Fatalf("seed project store: %v", err)
		}
	}

	registry := NewInMemoryFleetRegistry()
	for _, agent := range agents {
		if err := registry.RegisterFleetNode(context.Background(), agent); err != nil {
			t.Fatalf("register agent %s: %v", agent.ID, err)
		}
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("create agent service: %v", err)
	}
	impl := svc.(*agentServiceImpl)

	taskStore := &mockTaskStore{}
	impl.SetTaskStore(taskStore)
	impl.SetFleetRegistry(registry)
	impl.SetProjectAuthz(NewProjectAuthz(projectStore))

	return impl, taskStore
}

// scopedProject returns a project whose role matrix is exactly roles.
func scopedProject(tenantID, projectID string, roles map[string]ProjectRole) *Project {
	return &Project{
		ID:          projectID,
		TenantID:    tenantID,
		Name:        "proj-" + projectID,
		Permissions: ProjectPermissions{FleetNodeRoles: roles},
	}
}

// fleetNode returns an online agent on the given backend.
func fleetNode(tenantID, agentID string) *FleetNode {
	return &FleetNode{
		ID:       agentID,
		TenantID: tenantID,
		Name:     agentID,
		Host:     "host-" + agentID,
		Backend:  "ollama",
		NodeType: "worker",
		Status:   FleetNodeStatusIdle,
	}
}

// scopedTask returns a project-scoped task routed at the ollama backend.
func scopedTask(tenantID, taskID, projectID string) *Task {
	return &Task{
		ID:        taskID,
		TenantID:  tenantID,
		ProjectID: projectID,
		Subject:   "scoped work",
		Routing:   TaskRouting{Backend: "ollama"},
	}
}

// TestSpawnTask_DeniedAgentIsNotDispatched proves the denied agent is never
// handed the project's work: the only online agent holds the denied role, so
// the dispatch is refused naming the role matrix as the constraint.
func TestSpawnTask_DeniedAgentIsNotDispatched(t *testing.T) {
	t.Parallel()

	const (
		tenantID  = "tenant-spawn-denied"
		projectID = "proj-denied"
		agentID   = "agent-denied"
	)

	svc, _ := spawnAuthzService(t,
		scopedProject(tenantID, projectID, map[string]ProjectRole{agentID: ProjectRoleDenied}),
		fleetNode(tenantID, agentID),
	)

	_, err := svc.SpawnTask(context.Background(), scopedTask(tenantID, "task-denied", projectID))
	if err == nil {
		t.Fatal("expected the dispatch to be refused for a denied agent, got nil")
	}
	var noAgent *NoMatchingFleetNodeError
	if !errors.As(err, &noAgent) {
		t.Fatalf("expected *NoMatchingFleetNodeError, got %T: %v", err, err)
	}
	if noAgent.Constraint != "project_role" {
		t.Errorf("Constraint = %q, want %q", noAgent.Constraint, "project_role")
	}
}

// TestSpawnTask_ObserverAgentIsNotDispatched proves an observer — who cannot
// claim — is equally not handed the work by the dispatcher.
func TestSpawnTask_ObserverAgentIsNotDispatched(t *testing.T) {
	t.Parallel()

	const (
		tenantID  = "tenant-spawn-observer"
		projectID = "proj-observer"
		agentID   = "agent-observer"
	)

	svc, _ := spawnAuthzService(t,
		scopedProject(tenantID, projectID, map[string]ProjectRole{agentID: ProjectRoleObserver}),
		fleetNode(tenantID, agentID),
	)

	_, err := svc.SpawnTask(context.Background(), scopedTask(tenantID, "task-observer", projectID))
	var noAgent *NoMatchingFleetNodeError
	if !errors.As(err, &noAgent) {
		t.Fatalf("expected *NoMatchingFleetNodeError, got %T: %v", err, err)
	}
	if noAgent.Constraint != "project_role" {
		t.Errorf("Constraint = %q, want %q", noAgent.Constraint, "project_role")
	}
}

// TestSpawnTask_AgentAbsentFromScopedProjectIsNotDispatched proves the fleet
// agent a scoped project never listed is not an eligible dispatch target.
func TestSpawnTask_AgentAbsentFromScopedProjectIsNotDispatched(t *testing.T) {
	t.Parallel()

	const (
		tenantID  = "tenant-spawn-absent"
		projectID = "proj-absent"
	)

	svc, _ := spawnAuthzService(t,
		scopedProject(tenantID, projectID, map[string]ProjectRole{"agent-elsewhere": ProjectRoleWorker}),
		fleetNode(tenantID, "agent-stranger"),
	)

	_, err := svc.SpawnTask(context.Background(), scopedTask(tenantID, "task-absent", projectID))
	var noAgent *NoMatchingFleetNodeError
	if !errors.As(err, &noAgent) {
		t.Fatalf("expected *NoMatchingFleetNodeError, got %T: %v", err, err)
	}
	if noAgent.Constraint != "project_role" {
		t.Errorf("Constraint = %q, want %q", noAgent.Constraint, "project_role")
	}
}

// TestSpawnTask_EligibleAgentSurvivesADeniedPeer proves the boundary narrows
// the candidate set rather than refusing outright: a project that grants one
// online agent the worker role still dispatches, even while another online
// agent is denied.
func TestSpawnTask_EligibleAgentSurvivesADeniedPeer(t *testing.T) {
	t.Parallel()

	const (
		tenantID  = "tenant-spawn-mixed"
		projectID = "proj-mixed"
	)

	svc, _ := spawnAuthzService(t,
		scopedProject(tenantID, projectID, map[string]ProjectRole{
			"agent-denied": ProjectRoleDenied,
			"agent-worker": ProjectRoleWorker,
		}),
		fleetNode(tenantID, "agent-denied"),
		fleetNode(tenantID, "agent-worker"),
	)

	// The job itself cannot run (no backend is registered), so the spawn
	// still fails — but never on the project boundary, which is what this
	// pins.
	_, err := svc.SpawnTask(context.Background(), scopedTask(tenantID, "task-mixed", projectID))
	var noAgent *NoMatchingFleetNodeError
	if errors.As(err, &noAgent) && noAgent.Constraint == "project_role" {
		t.Fatalf("an eligible worker was online; dispatch must not be refused on the role matrix: %v", err)
	}
	if errors.Is(err, ErrProjectAccessDenied) || errors.Is(err, ErrProjectUnresolved) {
		t.Fatalf("an eligible worker was online; dispatch must not be refused on the project: %v", err)
	}
}

// TestSpawnTask_UnresolvableProjectRefused proves the dispatcher refuses when
// it cannot read the rule at all. This is the shape the tenant-routed store
// produced for every scoped task before #634: a project the authorizer cannot
// produce must never be dispatched as if it were unscoped.
func TestSpawnTask_UnresolvableProjectRefused(t *testing.T) {
	t.Parallel()

	const tenantID = "tenant-spawn-unresolved"

	svc, _ := spawnAuthzService(t, nil, fleetNode(tenantID, "agent-any"))

	_, err := svc.SpawnTask(context.Background(), scopedTask(tenantID, "task-unresolved", "no-such-project"))
	if !errors.Is(err, ErrProjectUnresolved) {
		t.Fatalf("expected ErrProjectUnresolved, got %T: %v", err, err)
	}
	var unresolved *ProjectUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("expected *ProjectUnresolvedError, got %T", err)
	}
	if unresolved.ProjectID != "no-such-project" {
		t.Errorf("ProjectID = %q, want %q", unresolved.ProjectID, "no-such-project")
	}
}

// TestSpawnTask_UnscopedTaskUnaffected proves a task that names no project is
// dispatched exactly as before: there is no matrix to consult.
func TestSpawnTask_UnscopedTaskUnaffected(t *testing.T) {
	t.Parallel()

	const tenantID = "tenant-spawn-unscoped"

	svc, _ := spawnAuthzService(t, nil, fleetNode(tenantID, "agent-any"))

	task := scopedTask(tenantID, "task-unscoped", "")
	_, err := svc.SpawnTask(context.Background(), task)
	if errors.Is(err, ErrProjectAccessDenied) || errors.Is(err, ErrProjectUnresolved) {
		t.Fatalf("an unscoped task must not meet the project boundary: %v", err)
	}
	var noAgent *NoMatchingFleetNodeError
	if errors.As(err, &noAgent) && noAgent.Constraint == "project_role" {
		t.Fatalf("an unscoped task must not be refused on the role matrix: %v", err)
	}
}

// TestSpawnTask_NoProjectAuthzUnaffected proves a node with no project
// authorizer wired dispatches as it always did.
func TestSpawnTask_NoProjectAuthzUnaffected(t *testing.T) {
	t.Parallel()

	const tenantID = "tenant-spawn-noauthz"

	registry := NewInMemoryFleetRegistry()
	if err := registry.RegisterFleetNode(context.Background(), fleetNode(tenantID, "agent-any")); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("create agent service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(&mockTaskStore{})
	impl.SetFleetRegistry(registry)
	// No SetProjectAuthz: projectAuthz stays nil.

	_, spawnErr := impl.SpawnTask(context.Background(), scopedTask(tenantID, "task-noauthz", "proj-any"))
	var noAgent *NoMatchingFleetNodeError
	if errors.As(spawnErr, &noAgent) && noAgent.Constraint == "project_role" {
		t.Fatalf("without a project authorizer there is no matrix to refuse on: %v", spawnErr)
	}
	if errors.Is(spawnErr, ErrProjectUnresolved) {
		t.Fatalf("without a project authorizer there is no project to resolve: %v", spawnErr)
	}
}
