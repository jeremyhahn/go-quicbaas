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

// FleetNodeStatus is the Wails-facing snapshot of a fleet node's lifecycle state.
// It mirrors the Svelte type `FleetNodeStatus` (see
// frontend/src/stores/orchestrator.svelte.ts) and the extended status
// fields the UI renders on the fleet nodes dashboard.
type FleetNodeStatus struct {
	AgentID  string `json:"agent_id"`
	TenantID string `json:"tenant_id,omitempty"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Status   string `json:"status"`
	Backend  string `json:"backend,omitempty"`
	Model    string `json:"model,omitempty"`
	NodeType string `json:"node_type,omitempty"`
	LastSeen int64  `json:"last_seen"`
	Version  uint64 `json:"version"`
}

// FleetBinding exposes agent-registry operations to the Wails frontend.
// It delegates every call to the shared SDK client owned by
// ConnectionBinding — no parallel client is ever constructed.
type FleetBinding struct {
	logger *logger.Logger
	conn   *ConnectionBinding
	ctx    context.Context
}

// NewFleetBinding creates a FleetBinding that reuses the SDK client
// managed by the provided ConnectionBinding.
func NewFleetBinding(log *logger.Logger, conn *ConnectionBinding) *FleetBinding {
	return &FleetBinding{
		logger: log,
		conn:   conn,
	}
}

// startup is called by Wails when the application starts.
func (b *FleetBinding) startup(ctx context.Context) {
	b.ctx = ctx
}

// shutdown is called by Wails when the application is closing.
func (b *FleetBinding) shutdown(ctx context.Context) {
	// No resources owned directly; the SDK client is owned by ConnectionBinding.
}

// List returns all registered fleet nodes matching the filter. Supported filter
// keys: tenant_id, host, status, node_type, backend, model. Unknown keys
// are ignored so the frontend can evolve without breaking the binding.
func (b *FleetBinding) List(filter map[string]interface{}) ([]orchestratorSDK.FleetNode, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	req := &orchestratorSDK.ListFleetNodesRequest{}
	if v, ok := filter["tenant_id"].(string); ok {
		req.TenantID = v
	}
	if v, ok := filter["host"].(string); ok {
		req.Host = v
	}
	if v, ok := filter["status"].(string); ok {
		req.Status = v
	}
	if v, ok := filter["node_type"].(string); ok {
		req.NodeType = v
	}
	if v, ok := filter["backend"].(string); ok {
		req.Backend = v
	}
	if v, ok := filter["model"].(string); ok {
		req.Model = v
	}

	resp, err := client.Orchestrator().ListFleetNodes(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("FleetBinding.List", err)
	}

	agents := make([]orchestratorSDK.FleetNode, 0, len(resp.Nodes))
	for _, a := range resp.Nodes {
		if a != nil {
			agents = append(agents, *a)
		}
	}
	return agents, nil
}

// Get returns the full registration record for a single fleet node.
func (b *FleetBinding) Get(nodeID string) (*orchestratorSDK.FleetNode, error) {
	if nodeID == "" {
		return nil, NewGUIFleetNodeIDRequiredError("FleetBinding.Get")
	}
	tenantID, err := b.resolveTenant()
	if err != nil {
		return nil, err
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	agent, err := client.Orchestrator().GetFleetNode(b.ctx, &orchestratorSDK.GetFleetNodeRequest{
		TenantID: tenantID,
		NodeID:   nodeID,
	})
	if err != nil {
		return nil, NewGUITransportError("FleetBinding.Get", err)
	}
	return agent, nil
}

// Status returns a compact lifecycle snapshot for the given fleet node.
// It is semantically equivalent to Get but projects the full
// FleetNode onto the subset of fields the UI's status widgets
// care about — keeping the IPC payload small for frequent polling.
func (b *FleetBinding) Status(nodeID string) (*FleetNodeStatus, error) {
	agent, err := b.Get(nodeID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, NewGUIFleetNodeNotFoundError("FleetBinding.Status", nodeID)
	}
	return &FleetNodeStatus{
		AgentID:  agent.ID,
		TenantID: agent.TenantID,
		Name:     agent.Name,
		Host:     agent.Host,
		Status:   agent.Status,
		Backend:  agent.Backend,
		Model:    agent.Model,
		NodeType: agent.NodeType,
		LastSeen: agent.LastSeen,
		Version:  agent.Version,
	}, nil
}

// Heartbeat refreshes the liveness timestamp for a fleet node.
func (b *FleetBinding) Heartbeat(nodeID string) error {
	if nodeID == "" {
		return NewGUIFleetNodeIDRequiredError("FleetBinding.Heartbeat")
	}
	tenantID, err := b.resolveTenant()
	if err != nil {
		return err
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return err
	}

	_, err = client.Orchestrator().FleetNodeHeartbeat(b.ctx, &orchestratorSDK.FleetNodeHeartbeatRequest{
		TenantID: tenantID,
		NodeID:   nodeID,
	})
	if err != nil {
		return NewGUITransportError("FleetBinding.Heartbeat", err)
	}
	return nil
}

// resolveTenant looks up a tenant ID from the active connection. Today
// the connection does not carry a tenant selector, so we fall back to
// the default tenant. This is a single point-of-change for when multi-
// tenant GUIs add an explicit tenant switcher.
func (b *FleetBinding) resolveTenant() (string, error) {
	return "default", nil
}
