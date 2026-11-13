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

//go:build integration

package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// tenantCtx returns a background context that calls as a caller bound to
// tenantID: the orchestration key the TenantGuard wired into the embedded
// AgentService reads, and go-qrdb's central key every transport binds from the
// authenticated identity, without which a tenant lookup (the registration's
// tenant_id check) refuses the call as tenant-less.
func tenantCtx(tenantID string) context.Context {
	ctx := tenantctx.WithTenant(context.Background(), tenantID)
	return orchestration.ContextWithTenant(ctx, tenantID)
}

// TestProjectAccessEnforcement_OnClaimTask validates that ClaimTask honours
// the per-project FleetNodeRoles matrix when a task is project-scoped. The flow:
//
//  1. Create a project with a non-empty FleetNodeRoles map (worker for our agent).
//  2. Register the agent in the orchestration registry.
//  3. Create a task scoped to the project.
//  4. Claim succeeds while role is "worker".
//  5. Flip the role to "denied" — claim returns ProjectAccessDeniedError.
//  6. Flip the role to "observer" — claim still rejected.
//  7. Flip the role to "lead" — claim succeeds.
//
// The SDK CreateTask request does not currently expose ProjectID, so this
// test reaches the orchestration AgentService directly via testService.Agent()
// to construct the project-scoped task. Project authz is wired into the
// AgentService during Server.startOrchestration().
func TestProjectAccessEnforcement_OnClaimTask(t *testing.T) {
	tenantID := newTenant(t, "tenant-pae-")
	ctx := tenantCtx(tenantID)
	plainCtx := context.Background() // ProjectService does not require tenant ctx

	projectName := "proj-pae-" + uniqueSuffix()
	projectAgentName := "agent-pae-" + uniqueSuffix()

	// Step 1: create project with a placeholder role for the agent (re-granted
	// after registration when we know the real agent ID).
	createResp, err := testService.Project().CreateProject(plainCtx, &service.CreateProjectRequest{
		TenantID: tenantID,
		Name:     projectName,
		FleetNodeRoles: map[string]orchestration.ProjectRole{
			"placeholder-keeps-map-non-empty": orchestration.ProjectRoleObserver,
		},
	})
	require.NoError(t, err, "create project")
	require.NotNil(t, createResp.Project)
	projectID := createResp.Project.ID

	// Step 2: register an agent so subsequent task ops are valid.
	regResp, err := testService.Agent().RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     projectAgentName,
		Host:     "host-pae",
		NodeType: "worker",
		Backend:  "ollama",
	})
	require.NoError(t, err, "register agent")
	require.NotNil(t, regResp.Node)
	agentID := regResp.Node.ID

	// Grant the worker role against the registered agent's actual ID.
	_, err = testService.Project().GrantFleetNodeRole(plainCtx, &service.GrantFleetNodeRoleRequest{
		TenantID:    tenantID,
		ProjectID:   projectID,
		FleetNodeID: agentID,
		Role:        orchestration.ProjectRoleWorker,
	})
	require.NoError(t, err, "grant worker role to registered agent")

	createTask := func(t *testing.T) string {
		t.Helper()
		task, terr := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
			TenantID:  tenantID,
			Subject:   "scoped-task-" + uniqueSuffix(),
			ProjectID: projectID,
		})
		require.NoError(t, terr, "create task")
		require.NotNil(t, task)
		return task.ID
	}

	// Step 4: as "worker" claim should succeed.
	t.Run("WorkerCanClaim", func(t *testing.T) {
		taskID := createTask(t)
		claimErr := testService.Agent().ClaimTask(ctx, tenantID, taskID, agentID)
		require.NoError(t, claimErr, "worker should be allowed to claim")
	})

	// Step 5: flip to denied — claim should be rejected with the typed error.
	t.Run("DeniedCannotClaim", func(t *testing.T) {
		_, gerr := testService.Project().GrantFleetNodeRole(plainCtx, &service.GrantFleetNodeRoleRequest{
			TenantID:    tenantID,
			ProjectID:   projectID,
			FleetNodeID: agentID,
			Role:        orchestration.ProjectRoleDenied,
		})
		require.NoError(t, gerr)

		taskID := createTask(t)
		claimErr := testService.Agent().ClaimTask(ctx, tenantID, taskID, agentID)
		require.Error(t, claimErr)

		var deniedErr *orchestration.ProjectAccessDeniedError
		require.True(t, errors.As(claimErr, &deniedErr),
			"expected *orchestration.ProjectAccessDeniedError, got %T: %v", claimErr, claimErr)
		assert.Equal(t, projectID, deniedErr.ProjectID)
		assert.Equal(t, agentID, deniedErr.FleetNodeID)
		assert.Equal(t, orchestration.ProjectRoleWorker, deniedErr.RequiredRole)
		assert.Equal(t, orchestration.ProjectRoleDenied, deniedErr.ActualRole)
	})

	// Step 6: flip to observer — observer can't claim worker tasks.
	t.Run("ObserverCannotClaim", func(t *testing.T) {
		_, gerr := testService.Project().GrantFleetNodeRole(plainCtx, &service.GrantFleetNodeRoleRequest{
			TenantID:    tenantID,
			ProjectID:   projectID,
			FleetNodeID: agentID,
			Role:        orchestration.ProjectRoleObserver,
		})
		require.NoError(t, gerr)

		taskID := createTask(t)
		claimErr := testService.Agent().ClaimTask(ctx, tenantID, taskID, agentID)
		require.Error(t, claimErr)
		var deniedErr *orchestration.ProjectAccessDeniedError
		require.True(t, errors.As(claimErr, &deniedErr),
			"expected ProjectAccessDeniedError, got %T", claimErr)
		assert.Equal(t, orchestration.ProjectRoleObserver, deniedErr.ActualRole)
	})

	// Step 7: flip to lead — lead is more permissive than worker, claim allowed.
	t.Run("LeadCanClaim", func(t *testing.T) {
		_, gerr := testService.Project().GrantFleetNodeRole(plainCtx, &service.GrantFleetNodeRoleRequest{
			TenantID:    tenantID,
			ProjectID:   projectID,
			FleetNodeID: agentID,
			Role:        orchestration.ProjectRoleLead,
		})
		require.NoError(t, gerr)

		taskID := createTask(t)
		claimErr := testService.Agent().ClaimTask(ctx, tenantID, taskID, agentID)
		require.NoError(t, claimErr, "lead should be allowed to claim")
	})
}

// TestProjectAccessEnforcement_LegacyAllowAll verifies that a project with an
// empty FleetNodeRoles map preserves legacy unscoped behaviour: any agent can
// claim regardless of role membership. This guards against accidental
// regressions that would tighten enforcement on legacy projects.
func TestProjectAccessEnforcement_LegacyAllowAll(t *testing.T) {
	tenantID := newTenant(t, "tenant-legacy-")
	ctx := tenantCtx(tenantID)
	plainCtx := context.Background()

	// Create a project with NO FleetNodeRoles map.
	createResp, err := testService.Project().CreateProject(plainCtx, &service.CreateProjectRequest{
		TenantID: tenantID,
		Name:     "proj-legacy-" + uniqueSuffix(),
	})
	require.NoError(t, err)
	projectID := createResp.Project.ID

	// Register an agent that is *not* in the role matrix.
	regResp, err := testService.Agent().RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     "agent-legacy-" + uniqueSuffix(),
		Host:     "host-legacy",
		NodeType: "worker",
		Backend:  "ollama",
	})
	require.NoError(t, err)
	agentID := regResp.Node.ID

	// Create project-scoped task.
	task, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:  tenantID,
		Subject:   "legacy-task",
		ProjectID: projectID,
	})
	require.NoError(t, err)

	// Empty FleetNodeRoles ⇒ legacy unscoped allow-all.
	err = testService.Agent().ClaimTask(ctx, tenantID, task.ID, agentID)
	require.NoError(t, err, "legacy project (empty role map) should allow any agent")
}

// TestProjectAccessEnforcement_OnDispatch pins the same boundary on the write
// that hands an agent a task without any claim at all.
//
// A CLAIM IS THE AGENT ASKING; A DISPATCH IS THE ENGINE TELLING. Auto-spawn at
// creation time and the dispatcher's later sweep share one dispatch path
// (SpawnTask → autoSpawnTask), and that path assigns the task to a fleet node
// it picked itself. If only the claim consulted the project's role matrix, a
// denied agent would simply be handed the work instead of being refused when
// it asked for it — so both entry points are asserted here.
func TestProjectAccessEnforcement_OnDispatch(t *testing.T) {
	tenantID := newTenant(t, "tenant-pad-")
	ctx := tenantCtx(tenantID)
	plainCtx := context.Background()

	createResp, err := testService.Project().CreateProject(plainCtx, &service.CreateProjectRequest{
		TenantID: tenantID,
		Name:     "proj-pad-" + uniqueSuffix(),
		FleetNodeRoles: map[string]orchestration.ProjectRole{
			"placeholder-keeps-map-non-empty": orchestration.ProjectRoleObserver,
		},
	})
	require.NoError(t, err, "create project")
	projectID := createResp.Project.ID

	agent := registerOnlineAgent(t, ctx, tenantID, "agent-pad-"+uniqueSuffix(), "ollama", nil)

	grant := func(t *testing.T, role orchestration.ProjectRole) {
		t.Helper()
		_, gerr := testService.Project().GrantFleetNodeRole(plainCtx, &service.GrantFleetNodeRoleRequest{
			TenantID:    tenantID,
			ProjectID:   projectID,
			FleetNodeID: agent.ID,
			Role:        role,
		})
		require.NoError(t, gerr, "grant %s", role)
	}

	// The dispatcher reaches the same service through TaskSpawner; asserting
	// it here is what pins the sweep path rather than only creation time.
	spawner, ok := testService.Agent().(orchestration.TaskSpawner)
	require.True(t, ok, "the orchestration AgentService must satisfy TaskSpawner")

	newScopedTask := func(t *testing.T) *orchestration.Task {
		t.Helper()
		task, terr := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
			TenantID:  tenantID,
			Subject:   "dispatch-task-" + uniqueSuffix(),
			ProjectID: projectID,
			Backend:   "ollama",
		})
		require.NoError(t, terr, "create task")
		return task
	}

	t.Run("WorkerIsDispatched", func(t *testing.T) {
		grant(t, orchestration.ProjectRoleWorker)

		task, terr := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
			TenantID:  tenantID,
			Subject:   "dispatch-worker-" + uniqueSuffix(),
			ProjectID: projectID,
			Backend:   "ollama",
			AutoSpawn: true,
		})
		require.NoError(t, terr, "a worker the project granted must be dispatched")
		assert.NotEmpty(t, task.SpawnedJobID, "a dispatched task carries its job")
	})

	t.Run("DeniedIsNotDispatchedOnCreate", func(t *testing.T) {
		grant(t, orchestration.ProjectRoleDenied)

		task, terr := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
			TenantID:  tenantID,
			Subject:   "dispatch-denied-" + uniqueSuffix(),
			ProjectID: projectID,
			Backend:   "ollama",
			AutoSpawn: true,
		})
		require.Error(t, terr, "a denied agent must not be handed the project's work")
		require.NotNil(t, task, "the task is persisted even when the dispatch is refused")
		assert.Empty(t, task.SpawnedJobID, "a refused dispatch starts no job")

		var noMatch *orchestration.NoMatchingFleetNodeError
		require.True(t, errors.As(terr, &noMatch),
			"expected *orchestration.NoMatchingFleetNodeError, got %T: %v", terr, terr)
		assert.Equal(t, "project_role", noMatch.Constraint,
			"the refusal must name the role matrix, not routing")
	})

	t.Run("DeniedIsNotDispatchedByTheDispatcher", func(t *testing.T) {
		grant(t, orchestration.ProjectRoleDenied)

		task := newScopedTask(t)
		jobID, serr := spawner.SpawnTask(ctx, task)
		require.Error(t, serr, "the dispatcher must refuse a denied agent too")
		assert.Empty(t, jobID)

		var noMatch *orchestration.NoMatchingFleetNodeError
		require.True(t, errors.As(serr, &noMatch),
			"expected *orchestration.NoMatchingFleetNodeError, got %T: %v", serr, serr)
		assert.Equal(t, "project_role", noMatch.Constraint)
	})

	t.Run("ObserverIsNotDispatched", func(t *testing.T) {
		grant(t, orchestration.ProjectRoleObserver)

		task := newScopedTask(t)
		_, serr := spawner.SpawnTask(ctx, task)
		require.Error(t, serr, "an observer cannot claim, so it must not be dispatched either")

		var noMatch *orchestration.NoMatchingFleetNodeError
		require.True(t, errors.As(serr, &noMatch),
			"expected *orchestration.NoMatchingFleetNodeError, got %T: %v", serr, serr)
		assert.Equal(t, "project_role", noMatch.Constraint)
	})

	t.Run("UnresolvableProjectIsNotDispatched", func(t *testing.T) {
		grant(t, orchestration.ProjectRoleWorker)

		task, terr := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
			TenantID: tenantID,
			Subject:  "dispatch-unresolved-" + uniqueSuffix(),
			// Well-formed, so CreateTask accepts it, but never created, so
			// the project store cannot produce it at dispatch.
			ProjectID: uuid.NewString(),
			Backend:   "ollama",
		})
		require.NoError(t, terr, "create task")

		_, serr := spawner.SpawnTask(ctx, task)
		require.Error(t, serr, "a project the store cannot produce must not be dispatched")
		assert.True(t, errors.Is(serr, orchestration.ErrProjectUnresolved),
			"expected ErrProjectUnresolved, got %T: %v", serr, serr)
	})
}
