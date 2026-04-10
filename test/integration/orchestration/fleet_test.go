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

// TestE2E_FleetRegisterAgent verifies that registering an agent through the
// AgentService returns a valid registration with expected fields.
func TestE2E_FleetRegisterAgent(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-register-agent"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      "test-agent",
		Host:      "192.168.1.1",
		AgentType: "claude-code",
		Backend:   "acp",
	})
	require.NoError(t, err, "RegisterAgent should succeed")
	require.NotNil(t, resp)
	require.NotNil(t, resp.Agent)

	assert.NotEmpty(t, resp.Agent.ID, "agent ID must not be empty")
	assert.Equal(t, "test-agent", resp.Agent.Name)
	assert.Equal(t, "192.168.1.1", resp.Agent.Host)
	assert.Equal(t, "claude-code", resp.Agent.AgentType)
	assert.Equal(t, tenantID, resp.Agent.TenantID)
	assert.Equal(t, orchestration.AgentStatusRegistered, resp.Agent.Status)
}

// TestE2E_FleetRegisterAgent_MissingName verifies that registering an agent
// without a name returns ErrAgentNameRequired.
func TestE2E_FleetRegisterAgent_MissingName(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-register-missing-name"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Host:      "192.168.1.1",
		AgentType: "claude-code",
		Backend:   "acp",
	})
	require.Error(t, err, "RegisterAgent without name should fail")
	assert.ErrorIs(t, err, orchestration.ErrAgentNameRequired)
}

// TestE2E_FleetRegisterAgent_MissingHost verifies that registering an agent
// without a host returns ErrAgentHostRequired.
func TestE2E_FleetRegisterAgent_MissingHost(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-register-missing-host"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      "test-agent",
		AgentType: "claude-code",
		Backend:   "acp",
	})
	require.Error(t, err, "RegisterAgent without host should fail")
	assert.ErrorIs(t, err, orchestration.ErrAgentHostRequired)
}

// TestE2E_FleetGetAgent verifies that a registered agent can be retrieved
// by ID and all fields match the original registration.
func TestE2E_FleetGetAgent(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-get-agent"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      "retrieval-agent",
		Host:      "10.0.0.1",
		AgentType: "codex",
		Backend:   "acp",
	})
	require.NoError(t, err)

	agent, err := svc.GetAgent(ctx, tenantID, resp.Agent.ID)
	require.NoError(t, err, "GetAgent should succeed")
	require.NotNil(t, agent)

	assert.Equal(t, resp.Agent.ID, agent.ID)
	assert.Equal(t, tenantID, agent.TenantID)
	assert.Equal(t, "retrieval-agent", agent.Name)
	assert.Equal(t, "10.0.0.1", agent.Host)
	assert.Equal(t, "codex", agent.AgentType)
	assert.Equal(t, "acp", agent.Backend)
	assert.Equal(t, orchestration.AgentStatusRegistered, agent.Status)
}

// TestE2E_FleetGetAgent_NotFound verifies that retrieving a non-existent agent
// returns ErrAgentRegistryNotFound.
func TestE2E_FleetGetAgent_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-get-not-found"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.GetAgent(ctx, tenantID, "nonexistent-agent-id")
	require.Error(t, err, "GetAgent for nonexistent agent should fail")
	assert.ErrorIs(t, err, orchestration.ErrAgentRegistryNotFound)
}

// TestE2E_FleetListAgents verifies that listing agents returns all registered
// agents within a tenant.
func TestE2E_FleetListAgents(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-list-agents"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	for i := 0; i < 3; i++ {
		_, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
			TenantID:  tenantID,
			Name:      "agent-" + string(rune('a'+i)),
			Host:      "192.168.1." + string(rune('1'+i)),
			AgentType: "claude-code",
			Backend:   "acp",
		})
		require.NoError(t, err, "RegisterAgent should succeed")
	}

	agents, err := svc.ListAgents(ctx, tenantID, nil)
	require.NoError(t, err, "ListAgents should succeed")
	assert.Len(t, agents, 3, "expected 3 agents")
}

// TestE2E_FleetListAgents_TenantIsolation verifies that agents registered in
// one tenant are not visible to another tenant.
func TestE2E_FleetListAgents_TenantIsolation(t *testing.T) {
	svc := newAgentService(t)
	tenantA := "fleet-isolation-tenant-a"
	tenantB := "fleet-isolation-tenant-b"
	ctxA := orchestration.ContextWithTenant(context.Background(), tenantA)
	ctxB := orchestration.ContextWithTenant(context.Background(), tenantB)

	// Register 2 agents in tenant-A.
	for i := 0; i < 2; i++ {
		_, err := svc.RegisterAgent(ctxA, &orchestration.RegisterAgentRequest{
			TenantID:  tenantA,
			Name:      "agent-a-" + string(rune('0'+i)),
			Host:      "10.0.0." + string(rune('1'+i)),
			AgentType: "claude-code",
			Backend:   "acp",
		})
		require.NoError(t, err)
	}

	// Register 1 agent in tenant-B.
	_, err := svc.RegisterAgent(ctxB, &orchestration.RegisterAgentRequest{
		TenantID:  tenantB,
		Name:      "agent-b-0",
		Host:      "10.0.1.1",
		AgentType: "claude-code",
		Backend:   "acp",
	})
	require.NoError(t, err)

	agentsA, err := svc.ListAgents(ctxA, tenantA, nil)
	require.NoError(t, err)
	assert.Len(t, agentsA, 2, "tenant-A should have 2 agents")

	agentsB, err := svc.ListAgents(ctxB, tenantB, nil)
	require.NoError(t, err)
	assert.Len(t, agentsB, 1, "tenant-B should have 1 agent")
}

// TestE2E_FleetListAgents_FilterByHost verifies that listing agents with a
// host filter returns only agents on the specified host.
func TestE2E_FleetListAgents_FilterByHost(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-filter-host"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	hosts := []string{"host-alpha.local", "host-beta.local", "host-alpha.local"}
	for i, host := range hosts {
		_, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
			TenantID:  tenantID,
			Name:      "agent-host-" + string(rune('0'+i)),
			Host:      host,
			AgentType: "claude-code",
			Backend:   "acp",
		})
		require.NoError(t, err)
	}

	agents, err := svc.ListAgents(ctx, tenantID, &orchestration.AgentFilter{
		Host: "host-alpha.local",
	})
	require.NoError(t, err)
	assert.Len(t, agents, 2, "expected 2 agents on host-alpha.local")

	for _, agent := range agents {
		assert.Equal(t, "host-alpha.local", agent.Host)
	}
}

// TestE2E_FleetListAgents_FilterByAgentType verifies that listing agents with
// an agent type filter returns only agents of the specified type.
func TestE2E_FleetListAgents_FilterByAgentType(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-filter-type"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	types := []string{"claude-code", "codex", "claude-code"}
	for i, agentType := range types {
		_, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
			TenantID:  tenantID,
			Name:      "agent-type-" + string(rune('0'+i)),
			Host:      "192.168.1." + string(rune('1'+i)),
			AgentType: agentType,
			Backend:   "acp",
		})
		require.NoError(t, err)
	}

	agents, err := svc.ListAgents(ctx, tenantID, &orchestration.AgentFilter{
		AgentType: "codex",
	})
	require.NoError(t, err)
	assert.Len(t, agents, 1, "expected 1 codex agent")
	assert.Equal(t, "codex", agents[0].AgentType)
}

// TestE2E_FleetListAgents_Empty verifies that listing agents for a tenant with
// no registrations returns a non-nil empty slice.
func TestE2E_FleetListAgents_Empty(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-list-empty"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	agents, err := svc.ListAgents(ctx, tenantID, nil)
	require.NoError(t, err, "ListAgents for empty tenant should succeed")
	require.NotNil(t, agents, "result slice must not be nil")
	assert.Empty(t, agents, "result slice must be empty")
}

// TestE2E_FleetRemoveAgent verifies that removing a registered agent makes it
// unavailable for subsequent retrieval.
func TestE2E_FleetRemoveAgent(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-remove-agent"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      "removable-agent",
		Host:      "10.0.0.99",
		AgentType: "claude-code",
		Backend:   "acp",
	})
	require.NoError(t, err)

	err = svc.RemoveAgent(ctx, tenantID, resp.Agent.ID)
	require.NoError(t, err, "RemoveAgent should succeed")

	_, err = svc.GetAgent(ctx, tenantID, resp.Agent.ID)
	require.Error(t, err, "GetAgent after removal should fail")
	assert.ErrorIs(t, err, orchestration.ErrAgentRegistryNotFound)
}

// TestE2E_FleetRemoveAgent_NotFound verifies that removing a non-existent agent
// returns an error.
func TestE2E_FleetRemoveAgent_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-remove-not-found"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	err := svc.RemoveAgent(ctx, tenantID, "nonexistent-agent-id")
	require.Error(t, err, "RemoveAgent for nonexistent agent should fail")
}

// TestE2E_FleetHeartbeat verifies that sending a heartbeat updates the agent's
// last seen timestamp and transitions status to running.
func TestE2E_FleetHeartbeat(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-heartbeat"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      "heartbeat-agent",
		Host:      "10.0.0.50",
		AgentType: "claude-code",
		Backend:   "acp",
	})
	require.NoError(t, err)

	// Record the initial last seen value.
	initial, err := svc.GetAgent(ctx, tenantID, resp.Agent.ID)
	require.NoError(t, err)
	initialLastSeen := initial.LastSeen

	err = svc.AgentHeartbeat(ctx, tenantID, resp.Agent.ID)
	require.NoError(t, err, "AgentHeartbeat should succeed")

	updated, err := svc.GetAgent(ctx, tenantID, resp.Agent.ID)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, updated.LastSeen, initialLastSeen,
		"LastSeen should be updated after heartbeat")
	assert.Equal(t, orchestration.AgentStatusRunning, updated.Status,
		"status should transition to running after heartbeat")
}

// TestE2E_FleetHeartbeat_NotFound verifies that sending a heartbeat for a
// non-existent agent returns an error.
func TestE2E_FleetHeartbeat_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-heartbeat-not-found"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	err := svc.AgentHeartbeat(ctx, tenantID, "nonexistent-agent-id")
	require.Error(t, err, "AgentHeartbeat for nonexistent agent should fail")
}

// TestE2E_FleetLifecycle verifies the complete agent lifecycle:
// register -> list (1) -> heartbeat -> get (status=running) -> remove -> list (0).
func TestE2E_FleetLifecycle(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-lifecycle"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Step 1: Register an agent.
	resp, err := svc.RegisterAgent(ctx, &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      "lifecycle-agent",
		Host:      "10.0.0.100",
		AgentType: "claude-code",
		Backend:   "acp",
	})
	require.NoError(t, err, "register should succeed")
	require.NotNil(t, resp.Agent)
	agentID := resp.Agent.ID

	// Step 2: List should return exactly 1 agent.
	agents, err := svc.ListAgents(ctx, tenantID, nil)
	require.NoError(t, err)
	assert.Len(t, agents, 1, "list after register should return 1 agent")

	// Step 3: Send heartbeat.
	err = svc.AgentHeartbeat(ctx, tenantID, agentID)
	require.NoError(t, err, "heartbeat should succeed")

	// Step 4: Get agent and verify status is running.
	agent, err := svc.GetAgent(ctx, tenantID, agentID)
	require.NoError(t, err)
	assert.Equal(t, orchestration.AgentStatusRunning, agent.Status,
		"status should be running after heartbeat")

	// Step 5: Remove the agent.
	err = svc.RemoveAgent(ctx, tenantID, agentID)
	require.NoError(t, err, "remove should succeed")

	// Step 6: List should return 0 agents.
	agents, err = svc.ListAgents(ctx, tenantID, nil)
	require.NoError(t, err)
	assert.Empty(t, agents, "list after remove should return 0 agents")
}
