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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// tenantCtx returns a background context tagged with the tenant ID expected
// by the TenantGuard wired into the embedded orchestration AgentService.
func tenantCtx(tenantID string) context.Context {
	return orchestration.ContextWithTenant(context.Background(), tenantID)
}

// TestProjectAccessEnforcement_OnClaimTask validates that ClaimTask honours
// the per-project AgentRoles matrix when a task is project-scoped. The flow:
//
//  1. Create a project with a non-empty AgentRoles map (worker for our agent).
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
	tenantID := "tenant-pae-" + uniqueSuffix()
	ctx := tenantCtx(tenantID)
	plainCtx := context.Background() // ProjectService does not require tenant ctx

	projectName := "proj-pae-" + uniqueSuffix()
	projectAgentName := "agent-pae-" + uniqueSuffix()

	// Step 1: create project with a placeholder role for the agent (re-granted
	// after registration when we know the real agent ID).
	createResp, err := testService.Project().CreateProject(plainCtx, &service.CreateProjectRequest{
		TenantID: tenantID,
		Name:     projectName,
		AgentRoles: map[string]orchestration.ProjectRole{
			"placeholder-keeps-map-non-empty": orchestration.ProjectRoleObserver,
		},
	})
	require.NoError(t, err, "create project")
	require.NotNil(t, createResp.Project)
	projectID := createResp.Project.ID

	// Step 2: register an agent so subsequent task ops are valid.
	regResp, err := testService.Agent().RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      projectAgentName,
		Host:      "host-pae",
		AgentType: "worker",
		Backend:   "ollama",
	})
	require.NoError(t, err, "register agent")
	require.NotNil(t, regResp.Agent)
	agentID := regResp.Agent.ID

	// Grant the worker role against the registered agent's actual ID.
	_, err = testService.Project().GrantAgentRole(plainCtx, &service.GrantAgentRoleRequest{
		TenantID:  tenantID,
		ProjectID: projectID,
		AgentID:   agentID,
		Role:      orchestration.ProjectRoleWorker,
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
		_, gerr := testService.Project().GrantAgentRole(plainCtx, &service.GrantAgentRoleRequest{
			TenantID:  tenantID,
			ProjectID: projectID,
			AgentID:   agentID,
			Role:      orchestration.ProjectRoleDenied,
		})
		require.NoError(t, gerr)

		taskID := createTask(t)
		claimErr := testService.Agent().ClaimTask(ctx, tenantID, taskID, agentID)
		require.Error(t, claimErr)

		var deniedErr *orchestration.ProjectAccessDeniedError
		require.True(t, errors.As(claimErr, &deniedErr),
			"expected *orchestration.ProjectAccessDeniedError, got %T: %v", claimErr, claimErr)
		assert.Equal(t, projectID, deniedErr.ProjectID)
		assert.Equal(t, agentID, deniedErr.AgentID)
		assert.Equal(t, orchestration.ProjectRoleWorker, deniedErr.RequiredRole)
		assert.Equal(t, orchestration.ProjectRoleDenied, deniedErr.ActualRole)
	})

	// Step 6: flip to observer — observer can't claim worker tasks.
	t.Run("ObserverCannotClaim", func(t *testing.T) {
		_, gerr := testService.Project().GrantAgentRole(plainCtx, &service.GrantAgentRoleRequest{
			TenantID:  tenantID,
			ProjectID: projectID,
			AgentID:   agentID,
			Role:      orchestration.ProjectRoleObserver,
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
		_, gerr := testService.Project().GrantAgentRole(plainCtx, &service.GrantAgentRoleRequest{
			TenantID:  tenantID,
			ProjectID: projectID,
			AgentID:   agentID,
			Role:      orchestration.ProjectRoleLead,
		})
		require.NoError(t, gerr)

		taskID := createTask(t)
		claimErr := testService.Agent().ClaimTask(ctx, tenantID, taskID, agentID)
		require.NoError(t, claimErr, "lead should be allowed to claim")
	})
}

// TestProjectAccessEnforcement_LegacyAllowAll verifies that a project with an
// empty AgentRoles map preserves legacy unscoped behaviour: any agent can
// claim regardless of role membership. This guards against accidental
// regressions that would tighten enforcement on legacy projects.
func TestProjectAccessEnforcement_LegacyAllowAll(t *testing.T) {
	tenantID := "tenant-legacy-" + uniqueSuffix()
	ctx := tenantCtx(tenantID)
	plainCtx := context.Background()

	// Create a project with NO AgentRoles map.
	createResp, err := testService.Project().CreateProject(plainCtx, &service.CreateProjectRequest{
		TenantID: tenantID,
		Name:     "proj-legacy-" + uniqueSuffix(),
	})
	require.NoError(t, err)
	projectID := createResp.Project.ID

	// Register an agent that is *not* in the role matrix.
	regResp, err := testService.Agent().RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      "agent-legacy-" + uniqueSuffix(),
		Host:      "host-legacy",
		AgentType: "worker",
		Backend:   "ollama",
	})
	require.NoError(t, err)
	agentID := regResp.Agent.ID

	// Create project-scoped task.
	task, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:  tenantID,
		Subject:   "legacy-task",
		ProjectID: projectID,
	})
	require.NoError(t, err)

	// Empty AgentRoles ⇒ legacy unscoped allow-all.
	err = testService.Agent().ClaimTask(ctx, tenantID, task.ID, agentID)
	require.NoError(t, err, "legacy project (empty role map) should allow any agent")
}
