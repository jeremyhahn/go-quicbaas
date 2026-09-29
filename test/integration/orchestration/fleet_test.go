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

	resp, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     "test-agent",
		Host:     "192.168.1.1",
		NodeType: "claude-code",
		Backend:  "acp",
	})
	require.NoError(t, err, "RegisterFleetNode should succeed")
	require.NotNil(t, resp)
	require.NotNil(t, resp.Node)

	assert.NotEmpty(t, resp.Node.ID, "agent ID must not be empty")
	assert.Equal(t, "test-agent", resp.Node.Name)
	assert.Equal(t, "192.168.1.1", resp.Node.Host)
	assert.Equal(t, "claude-code", resp.Node.NodeType)
	assert.Equal(t, tenantID, resp.Node.TenantID)
	assert.Equal(t, orchestration.FleetNodeStatusRegistered, resp.Node.Status)
}

// TestE2E_FleetRegisterAgent_MissingName verifies that registering an agent
// without a name returns ErrFleetNodeNameRequired.
func TestE2E_FleetRegisterAgent_MissingName(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-register-missing-name"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Host:     "192.168.1.1",
		NodeType: "claude-code",
		Backend:  "acp",
	})
	require.Error(t, err, "RegisterFleetNode without name should fail")
	assert.ErrorIs(t, err, orchestration.ErrFleetNodeNameRequired)
}

// TestE2E_FleetRegisterAgent_MissingHost verifies that registering an agent
// without a host returns ErrFleetNodeHostRequired.
func TestE2E_FleetRegisterAgent_MissingHost(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-register-missing-host"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     "test-agent",
		NodeType: "claude-code",
		Backend:  "acp",
	})
	require.Error(t, err, "RegisterFleetNode without host should fail")
	assert.ErrorIs(t, err, orchestration.ErrFleetNodeHostRequired)
}

// TestE2E_FleetGetAgent verifies that a registered agent can be retrieved
// by ID and all fields match the original registration.
func TestE2E_FleetGetAgent(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-get-agent"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     "retrieval-agent",
		Host:     "10.0.0.1",
		NodeType: "codex",
		Backend:  "acp",
	})
	require.NoError(t, err)

	agent, err := svc.GetFleetNode(ctx, tenantID, resp.Node.ID)
	require.NoError(t, err, "GetFleetNode should succeed")
	require.NotNil(t, agent)

	assert.Equal(t, resp.Node.ID, agent.ID)
	assert.Equal(t, tenantID, agent.TenantID)
	assert.Equal(t, "retrieval-agent", agent.Name)
	assert.Equal(t, "10.0.0.1", agent.Host)
	assert.Equal(t, "codex", agent.NodeType)
	assert.Equal(t, "acp", agent.Backend)
	assert.Equal(t, orchestration.FleetNodeStatusRegistered, agent.Status)
}

// TestE2E_FleetGetAgent_NotFound verifies that retrieving a non-existent agent
// returns ErrFleetNodeNotFound.
func TestE2E_FleetGetAgent_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-get-not-found"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.GetFleetNode(ctx, tenantID, "nonexistent-agent-id")
	require.Error(t, err, "GetFleetNode for nonexistent agent should fail")
	assert.ErrorIs(t, err, orchestration.ErrFleetNodeNotFound)
}

// TestE2E_FleetListAgents verifies that listing agents returns all registered
// agents within a tenant.
func TestE2E_FleetListAgents(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-list-agents"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	for i := 0; i < 3; i++ {
		_, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
			TenantID: tenantID,
			Name:     "agent-" + string(rune('a'+i)),
			Host:     "192.168.1." + string(rune('1'+i)),
			NodeType: "claude-code",
			Backend:  "acp",
		})
		require.NoError(t, err, "RegisterFleetNode should succeed")
	}

	agents, err := svc.ListFleetNodes(ctx, tenantID, nil)
	require.NoError(t, err, "ListFleetNodes should succeed")
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
		_, err := svc.RegisterFleetNode(ctxA, &orchestration.RegisterFleetNodeRequest{
			TenantID: tenantA,
			Name:     "agent-a-" + string(rune('0'+i)),
			Host:     "10.0.0." + string(rune('1'+i)),
			NodeType: "claude-code",
			Backend:  "acp",
		})
		require.NoError(t, err)
	}

	// Register 1 agent in tenant-B.
	_, err := svc.RegisterFleetNode(ctxB, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantB,
		Name:     "agent-b-0",
		Host:     "10.0.1.1",
		NodeType: "claude-code",
		Backend:  "acp",
	})
	require.NoError(t, err)

	agentsA, err := svc.ListFleetNodes(ctxA, tenantA, nil)
	require.NoError(t, err)
	assert.Len(t, agentsA, 2, "tenant-A should have 2 agents")

	agentsB, err := svc.ListFleetNodes(ctxB, tenantB, nil)
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
		_, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
			TenantID: tenantID,
			Name:     "agent-host-" + string(rune('0'+i)),
			Host:     host,
			NodeType: "claude-code",
			Backend:  "acp",
		})
		require.NoError(t, err)
	}

	agents, err := svc.ListFleetNodes(ctx, tenantID, &orchestration.FleetNodeFilter{
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
		_, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
			TenantID: tenantID,
			Name:     "agent-type-" + string(rune('0'+i)),
			Host:     "192.168.1." + string(rune('1'+i)),
			NodeType: agentType,
			Backend:  "acp",
		})
		require.NoError(t, err)
	}

	agents, err := svc.ListFleetNodes(ctx, tenantID, &orchestration.FleetNodeFilter{
		NodeType: "codex",
	})
	require.NoError(t, err)
	assert.Len(t, agents, 1, "expected 1 codex agent")
	assert.Equal(t, "codex", agents[0].NodeType)
}

// TestE2E_FleetListAgents_Empty verifies that listing agents for a tenant with
// no registrations returns a non-nil empty slice.
func TestE2E_FleetListAgents_Empty(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-list-empty"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	agents, err := svc.ListFleetNodes(ctx, tenantID, nil)
	require.NoError(t, err, "ListFleetNodes for empty tenant should succeed")
	require.NotNil(t, agents, "result slice must not be nil")
	assert.Empty(t, agents, "result slice must be empty")
}

// TestE2E_FleetRemoveAgent verifies that removing a registered agent makes it
// unavailable for subsequent retrieval.
func TestE2E_FleetRemoveAgent(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-remove-agent"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     "removable-agent",
		Host:     "10.0.0.99",
		NodeType: "claude-code",
		Backend:  "acp",
	})
	require.NoError(t, err)

	err = svc.RemoveFleetNode(ctx, tenantID, resp.Node.ID)
	require.NoError(t, err, "RemoveFleetNode should succeed")

	_, err = svc.GetFleetNode(ctx, tenantID, resp.Node.ID)
	require.Error(t, err, "GetFleetNode after removal should fail")
	assert.ErrorIs(t, err, orchestration.ErrFleetNodeNotFound)
}

// TestE2E_FleetRemoveAgent_NotFound verifies that removing a non-existent agent
// returns an error.
func TestE2E_FleetRemoveAgent_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-remove-not-found"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	err := svc.RemoveFleetNode(ctx, tenantID, "nonexistent-agent-id")
	require.Error(t, err, "RemoveFleetNode for nonexistent agent should fail")
}

// TestE2E_FleetHeartbeat verifies that sending a heartbeat updates the agent's
// last seen timestamp and transitions status to running.
func TestE2E_FleetHeartbeat(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-heartbeat"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     "heartbeat-agent",
		Host:     "10.0.0.50",
		NodeType: "claude-code",
		Backend:  "acp",
	})
	require.NoError(t, err)

	// Record the initial last seen value.
	initial, err := svc.GetFleetNode(ctx, tenantID, resp.Node.ID)
	require.NoError(t, err)
	initialLastSeen := initial.LastSeen

	err = svc.FleetNodeHeartbeat(ctx, tenantID, resp.Node.ID, nil)
	require.NoError(t, err, "FleetNodeHeartbeat should succeed")

	updated, err := svc.GetFleetNode(ctx, tenantID, resp.Node.ID)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, updated.LastSeen, initialLastSeen,
		"LastSeen should be updated after heartbeat")
	assert.Equal(t, orchestration.FleetNodeStatusRunning, updated.Status,
		"status should transition to running after heartbeat")
}

// TestE2E_FleetHeartbeat_NotFound verifies that sending a heartbeat for a
// non-existent agent returns an error.
func TestE2E_FleetHeartbeat_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-heartbeat-not-found"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	err := svc.FleetNodeHeartbeat(ctx, tenantID, "nonexistent-agent-id", nil)
	require.Error(t, err, "FleetNodeHeartbeat for nonexistent agent should fail")
}

// TestE2E_FleetLifecycle verifies the complete agent lifecycle:
// register -> list (1) -> heartbeat -> get (status=running) -> remove -> list (0).
func TestE2E_FleetLifecycle(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "fleet-lifecycle"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Step 1: Register an agent.
	resp, err := svc.RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     "lifecycle-agent",
		Host:     "10.0.0.100",
		NodeType: "claude-code",
		Backend:  "acp",
	})
	require.NoError(t, err, "register should succeed")
	require.NotNil(t, resp.Node)
	agentID := resp.Node.ID

	// Step 2: List should return exactly 1 agent.
	agents, err := svc.ListFleetNodes(ctx, tenantID, nil)
	require.NoError(t, err)
	assert.Len(t, agents, 1, "list after register should return 1 agent")

	// Step 3: Send heartbeat.
	err = svc.FleetNodeHeartbeat(ctx, tenantID, agentID, nil)
	require.NoError(t, err, "heartbeat should succeed")

	// Step 4: Get agent and verify status is running.
	agent, err := svc.GetFleetNode(ctx, tenantID, agentID)
	require.NoError(t, err)
	assert.Equal(t, orchestration.FleetNodeStatusRunning, agent.Status,
		"status should be running after heartbeat")

	// Step 5: Remove the agent.
	err = svc.RemoveFleetNode(ctx, tenantID, agentID)
	require.NoError(t, err, "remove should succeed")

	// Step 6: List should return 0 agents.
	agents, err = svc.ListFleetNodes(ctx, tenantID, nil)
	require.NoError(t, err)
	assert.Empty(t, agents, "list after remove should return 0 agents")
}
