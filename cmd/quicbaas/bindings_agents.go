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

//go:build desktop

package main

import (
	"context"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// AgentStatus is the Wails-facing snapshot of an agent's lifecycle state.
// It mirrors the Svelte type `AgentStatus` (see
// frontend/src/stores/orchestrator.svelte.ts) and the extended status
// fields the UI renders on the agents dashboard.
type AgentStatus struct {
	AgentID   string `json:"agent_id"`
	TenantID  string `json:"tenant_id,omitempty"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Status    string `json:"status"`
	Backend   string `json:"backend,omitempty"`
	Model     string `json:"model,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	LastSeen  int64  `json:"last_seen"`
	Version   uint64 `json:"version"`
}

// AgentBinding exposes agent-registry operations to the Wails frontend.
// It delegates every call to the shared SDK client owned by
// ConnectionBinding — no parallel client is ever constructed.
type AgentBinding struct {
	logger *logger.Logger
	conn   *ConnectionBinding
	ctx    context.Context
}

// NewAgentBinding creates an AgentBinding that reuses the SDK client
// managed by the provided ConnectionBinding.
func NewAgentBinding(log *logger.Logger, conn *ConnectionBinding) *AgentBinding {
	return &AgentBinding{
		logger: log,
		conn:   conn,
	}
}

// startup is called by Wails when the application starts.
func (b *AgentBinding) startup(ctx context.Context) {
	b.ctx = ctx
}

// shutdown is called by Wails when the application is closing.
func (b *AgentBinding) shutdown(ctx context.Context) {
	// No resources owned directly; the SDK client is owned by ConnectionBinding.
}

// List returns all registered agents matching the filter. Supported filter
// keys: tenant_id, host, status, agent_type, backend, model. Unknown keys
// are ignored so the frontend can evolve without breaking the binding.
func (b *AgentBinding) List(filter map[string]interface{}) ([]orchestratorSDK.AgentRegistration, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	req := &orchestratorSDK.ListAgentsRequest{}
	if v, ok := filter["tenant_id"].(string); ok {
		req.TenantID = v
	}
	if v, ok := filter["host"].(string); ok {
		req.Host = v
	}
	if v, ok := filter["status"].(string); ok {
		req.Status = v
	}
	if v, ok := filter["agent_type"].(string); ok {
		req.AgentType = v
	}
	if v, ok := filter["backend"].(string); ok {
		req.Backend = v
	}
	if v, ok := filter["model"].(string); ok {
		req.Model = v
	}

	resp, err := client.Orchestrator().ListAgents(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("AgentBinding.List", err)
	}

	agents := make([]orchestratorSDK.AgentRegistration, 0, len(resp.Agents))
	for _, a := range resp.Agents {
		if a != nil {
			agents = append(agents, *a)
		}
	}
	return agents, nil
}

// Get returns the full registration record for a single agent.
func (b *AgentBinding) Get(agentID string) (*orchestratorSDK.AgentRegistration, error) {
	if agentID == "" {
		return nil, NewGUIAgentIDRequiredError("AgentBinding.Get")
	}
	tenantID, err := b.resolveTenant()
	if err != nil {
		return nil, err
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	agent, err := client.Orchestrator().GetAgent(b.ctx, &orchestratorSDK.GetAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	if err != nil {
		return nil, NewGUITransportError("AgentBinding.Get", err)
	}
	return agent, nil
}

// Status returns a compact lifecycle snapshot for the given agent.
// It is semantically equivalent to Get but projects the full
// AgentRegistration onto the subset of fields the UI's status widgets
// care about — keeping the IPC payload small for frequent polling.
func (b *AgentBinding) Status(agentID string) (*AgentStatus, error) {
	agent, err := b.Get(agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, NewGUIAgentNotFoundError("AgentBinding.Status", agentID)
	}
	return &AgentStatus{
		AgentID:   agent.ID,
		TenantID:  agent.TenantID,
		Name:      agent.Name,
		Host:      agent.Host,
		Status:    agent.Status,
		Backend:   agent.Backend,
		Model:     agent.Model,
		AgentType: agent.AgentType,
		LastSeen:  agent.LastSeen,
		Version:   agent.Version,
	}, nil
}

// Heartbeat refreshes the liveness timestamp for an agent.
func (b *AgentBinding) Heartbeat(agentID string) error {
	if agentID == "" {
		return NewGUIAgentIDRequiredError("AgentBinding.Heartbeat")
	}
	tenantID, err := b.resolveTenant()
	if err != nil {
		return err
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return err
	}

	_, err = client.Orchestrator().AgentHeartbeat(b.ctx, &orchestratorSDK.AgentHeartbeatRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	if err != nil {
		return NewGUITransportError("AgentBinding.Heartbeat", err)
	}
	return nil
}

// resolveTenant looks up a tenant ID from the active connection. Today
// the connection does not carry a tenant selector, so we fall back to
// the default tenant. This is a single point-of-change for when multi-
// tenant GUIs add an explicit tenant switcher.
func (b *AgentBinding) resolveTenant() (string, error) {
	return "default", nil
}
