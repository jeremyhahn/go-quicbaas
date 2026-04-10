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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestIntegration_WorkflowLoader_LoadFromDirectory verifies that the workflow
// loader can discover and parse all bundled workflow definitions from the
// workflows directory.
func TestIntegration_WorkflowLoader_LoadFromDirectory(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	err := loader.LoadFromDir(workflowDir)
	require.NoError(t, err, "loading workflows from directory should succeed")

	names := loader.List()
	require.GreaterOrEqual(t, len(names), 3,
		"directory should contain at least software-dev, code-review, and autonomous")

	assert.Contains(t, names, "software-dev")
	assert.Contains(t, names, "code-review")
	assert.Contains(t, names, "autonomous")
}

// TestIntegration_WorkflowLoader_SoftwareDevAgents verifies that the
// software-dev workflow has exactly 3 agents with the expected roles.
func TestIntegration_WorkflowLoader_SoftwareDevAgents(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	err := loader.LoadFromDir(workflowDir)
	require.NoError(t, err)

	def, err := loader.Get("software-dev")
	require.NoError(t, err, "software-dev workflow should exist")
	require.NotNil(t, def)

	require.Len(t, def.Agents, 3, "software-dev should have exactly 3 agents")

	roleSet := make(map[string]bool)
	for _, agent := range def.Agents {
		roleSet[agent.Role] = true
		assert.NotEmpty(t, agent.Backend, "agent %s should have a backend", agent.Role)
		assert.NotEmpty(t, agent.Model, "agent %s should have a model", agent.Role)
	}

	assert.True(t, roleSet["lead"], "software-dev should have a lead agent")
	assert.True(t, roleSet["coder"], "software-dev should have a coder agent")
	assert.True(t, roleSet["reviewer"], "software-dev should have a reviewer agent")
}

// TestIntegration_WorkflowLoader_SoftwareDevSettings verifies that the
// software-dev workflow has valid operational settings.
func TestIntegration_WorkflowLoader_SoftwareDevSettings(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	err := loader.LoadFromDir(workflowDir)
	require.NoError(t, err)

	def, err := loader.Get("software-dev")
	require.NoError(t, err)

	assert.Equal(t, orchestration.WorkflowModeConstrained, def.Mode,
		"software-dev should use constrained mode")
	assert.Greater(t, def.Settings.MaxIterations, 0,
		"max_iterations should be positive")
	assert.Greater(t, def.Settings.MaxParallelism, 0,
		"max_parallelism should be positive")
	assert.NotEmpty(t, def.Settings.TotalTimeout,
		"total_timeout should be set")
	assert.NotEmpty(t, def.Settings.PollInterval,
		"poll_interval should be set")
}

// TestIntegration_WorkflowLoader_AutonomousSingleAgent verifies that the
// autonomous workflow has exactly 1 agent (lead only) with the correct mode.
func TestIntegration_WorkflowLoader_AutonomousSingleAgent(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	err := loader.LoadFromDir(workflowDir)
	require.NoError(t, err)

	def, err := loader.Get("autonomous")
	require.NoError(t, err, "autonomous workflow should exist")
	require.NotNil(t, def)

	assert.Equal(t, orchestration.WorkflowModeAutonomous, def.Mode,
		"autonomous should use autonomous mode")
	require.Len(t, def.Agents, 1, "autonomous should have exactly 1 agent")
	assert.Equal(t, "lead", def.Agents[0].Role, "the single agent should be the lead")
	assert.NotEmpty(t, def.Agents[0].MCPScopes, "lead agent should have MCP scopes")
}

// TestIntegration_WorkflowLoader_CodeReviewAgents verifies that the code-review
// workflow has the expected agent roles.
func TestIntegration_WorkflowLoader_CodeReviewAgents(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	err := loader.LoadFromDir(workflowDir)
	require.NoError(t, err)

	def, err := loader.Get("code-review")
	require.NoError(t, err, "code-review workflow should exist")
	require.NotNil(t, def)

	assert.Equal(t, orchestration.WorkflowModeConstrained, def.Mode,
		"code-review should use constrained mode")
	require.Len(t, def.Agents, 2, "code-review should have exactly 2 agents")

	roleSet := make(map[string]bool)
	for _, agent := range def.Agents {
		roleSet[agent.Role] = true
	}

	assert.True(t, roleSet["lead"], "code-review should have a lead agent")
	assert.True(t, roleSet["reviewer"], "code-review should have a reviewer agent")
}

// TestIntegration_WorkflowLoader_Register verifies that programmatically
// registered workflows can be retrieved and that duplicate registration fails.
func TestIntegration_WorkflowLoader_Register(t *testing.T) {
	loader := orchestration.NewWorkflowLoader()

	def := &orchestration.WorkflowDefinition{
		Name:        "test-workflow",
		Description: "Programmatic test workflow",
		Mode:        orchestration.WorkflowModeAutonomous,
		Agents: []orchestration.AgentRoleSpec{
			{Role: "lead", Backend: "ollama", Model: "tinyllama"},
		},
	}

	t.Run("RegisterSucceeds", func(t *testing.T) {
		err := loader.Register(def)
		require.NoError(t, err, "Register should succeed for new workflow")

		retrieved, err := loader.Get("test-workflow")
		require.NoError(t, err)
		assert.Equal(t, "test-workflow", retrieved.Name)
	})

	t.Run("DuplicateRegistrationFails", func(t *testing.T) {
		err := loader.Register(def)
		require.Error(t, err, "Register should fail for duplicate workflow name")
	})
}

// TestIntegration_WorkflowLoader_Register_InvalidDefinition verifies that
// registering an invalid workflow definition returns a validation error.
func TestIntegration_WorkflowLoader_Register_InvalidDefinition(t *testing.T) {
	loader := orchestration.NewWorkflowLoader()

	t.Run("EmptyName", func(t *testing.T) {
		def := &orchestration.WorkflowDefinition{
			Name: "",
			Mode: orchestration.WorkflowModeAutonomous,
			Agents: []orchestration.AgentRoleSpec{
				{Role: "lead", Backend: "ollama", Model: "tinyllama"},
			},
		}
		err := loader.Register(def)
		require.Error(t, err, "Register with empty name should fail")
	})

	t.Run("InvalidMode", func(t *testing.T) {
		def := &orchestration.WorkflowDefinition{
			Name: "bad-mode",
			Mode: "invalid",
			Agents: []orchestration.AgentRoleSpec{
				{Role: "lead", Backend: "ollama", Model: "tinyllama"},
			},
		}
		err := loader.Register(def)
		require.Error(t, err, "Register with invalid mode should fail")
	})

	t.Run("NoAgents", func(t *testing.T) {
		def := &orchestration.WorkflowDefinition{
			Name:   "no-agents",
			Mode:   orchestration.WorkflowModeAutonomous,
			Agents: nil,
		}
		err := loader.Register(def)
		require.Error(t, err, "Register with no agents should fail")
	})

	t.Run("ConstrainedWithoutLead", func(t *testing.T) {
		def := &orchestration.WorkflowDefinition{
			Name: "no-lead",
			Mode: orchestration.WorkflowModeConstrained,
			Agents: []orchestration.AgentRoleSpec{
				{Role: "worker", Backend: "ollama", Model: "tinyllama"},
			},
		}
		err := loader.Register(def)
		require.Error(t, err, "Register constrained without lead should fail")
	})
}

// TestIntegration_WorkflowLoader_ListDefinitions verifies that ListDefinitions
// returns all loaded definitions sorted by name.
func TestIntegration_WorkflowLoader_ListDefinitions(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	err := loader.LoadFromDir(workflowDir)
	require.NoError(t, err)

	defs := loader.ListDefinitions()
	require.GreaterOrEqual(t, len(defs), 3)

	// Verify sorted order.
	for i := 1; i < len(defs); i++ {
		assert.Less(t, defs[i-1].Name, defs[i].Name,
			"definitions should be sorted by name")
	}

	// Verify each definition is valid.
	for _, def := range defs {
		assert.NotEmpty(t, def.Name)
		assert.NotEmpty(t, def.Mode)
		assert.NotEmpty(t, def.Agents)
	}
}
