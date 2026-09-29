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

// Package main: format-function coverage for agent.go. The Format helpers
// (formatFleetRegistrationResponse, formatFleetListResponse, ...) are
// pure rendering and easy to test without an actual SDK connection.
package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// =============================================================================
// formatFleetRegistrationResponse
// =============================================================================

func TestFormatAgentRegistrationResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.RegisterFleetNodeResponse{
		Node: &orchestratorSDK.FleetNode{
			ID:           "ag-1",
			Name:         "alice",
			Host:         "h1",
			NodeType:     "worker",
			Status:       "active",
			Backend:      "ollama",
			WorkDir:      "/tmp",
			RegisteredAt: time.Now().UnixNano(),
		},
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetRegistrationResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1")
	assert.Contains(t, got, "alice")
	assert.Contains(t, got, "ollama")
}

func TestFormatAgentRegistrationResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.RegisterFleetNodeResponse{
		Node: &orchestratorSDK.FleetNode{ID: "ag-1", Name: "alice"},
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetRegistrationResponse(resp, "json", &buf))
	got := buf.String()
	assert.Contains(t, got, `"id"`)
	assert.Contains(t, got, "ag-1")
}

func TestFormatAgentRegistrationResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.RegisterFleetNodeResponse{
		Node: &orchestratorSDK.FleetNode{ID: "ag-1"},
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetRegistrationResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentRegistrationResponse_InvalidFormat(t *testing.T) {
	resp := &orchestratorSDK.RegisterFleetNodeResponse{Node: &orchestratorSDK.FleetNode{}}
	var buf bytes.Buffer
	err := formatFleetRegistrationResponse(resp, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatFleetListResponse
// =============================================================================

func TestFormatAgentListResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: "ag-1", Name: "alice", Host: "h1", NodeType: "worker", Status: "active", LastSeen: time.Now().UnixNano()},
			{ID: "ag-2", Name: "bob", Host: "h2", NodeType: "worker", Status: "idle"},
		},
		Count:    2,
		Envelope: page.Envelope{Total: 2, Limit: 25},
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetListResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1")
	assert.Contains(t, got, "ag-2")
	assert.Contains(t, got, "Showing 1–2 of 2.")
}

func TestFormatAgentListResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{{ID: "a"}},
		Count: 1,
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetListResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"id"`)
}

func TestFormatAgentListResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{{ID: "a"}},
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetListResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "a")
}

func TestFormatAgentListResponse_InvalidFormat(t *testing.T) {
	resp := &orchestratorSDK.ListFleetNodesResponse{}
	var buf bytes.Buffer
	err := formatFleetListResponse(resp, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatFleetDetailResponse
// =============================================================================

func TestFormatAgentDetailResponse_Table(t *testing.T) {
	agent := &orchestratorSDK.FleetNode{
		ID: "ag-1", TenantID: "t1", Name: "alice", Host: "h1",
		NodeType: "worker", Status: "active", Backend: "ollama",
		WorkDir: "/tmp", Version: 3,
		RegisteredAt: time.Now().UnixNano(),
		LastSeen:     time.Now().UnixNano(),
		Metadata:     map[string]string{"key": "value"},
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetDetailResponse(agent, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1")
	assert.Contains(t, got, "alice")
	assert.Contains(t, got, "ollama")
	assert.Contains(t, got, "Metadata")
	assert.Contains(t, got, "key")
}

func TestFormatAgentDetailResponse_JSON(t *testing.T) {
	agent := &orchestratorSDK.FleetNode{ID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatFleetDetailResponse(agent, "json", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentDetailResponse_YAML(t *testing.T) {
	agent := &orchestratorSDK.FleetNode{ID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatFleetDetailResponse(agent, "yaml", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentDetailResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatFleetDetailResponse(&orchestratorSDK.FleetNode{}, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatFleetRemoveResponse
// =============================================================================

func TestFormatAgentRemoveResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.RemoveFleetNodeResponse{NodeID: "ag-1", Removed: true}
	var buf bytes.Buffer
	require.NoError(t, formatFleetRemoveResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1")
	assert.Contains(t, got, "true")
}

func TestFormatAgentRemoveResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.RemoveFleetNodeResponse{NodeID: "ag-1", Removed: true}
	var buf bytes.Buffer
	require.NoError(t, formatFleetRemoveResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"node_id"`)
}

func TestFormatAgentRemoveResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.RemoveFleetNodeResponse{NodeID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatFleetRemoveResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentRemoveResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatFleetRemoveResponse(&orchestratorSDK.RemoveFleetNodeResponse{}, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatFleetHeartbeatResponse
// =============================================================================

func TestFormatAgentHeartbeatResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.FleetNodeHeartbeatResponse{NodeID: "ag-1", Heartbeat: true}
	var buf bytes.Buffer
	require.NoError(t, formatFleetHeartbeatResponse(resp, "table", &buf))
	assert.Contains(t, buf.String(), "ag-1")
	assert.Contains(t, buf.String(), "true")
}

func TestFormatAgentHeartbeatResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.FleetNodeHeartbeatResponse{NodeID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatFleetHeartbeatResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"node_id"`)
}

func TestFormatAgentHeartbeatResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.FleetNodeHeartbeatResponse{NodeID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatFleetHeartbeatResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentHeartbeatResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatFleetHeartbeatResponse(&orchestratorSDK.FleetNodeHeartbeatResponse{}, "bogus", &buf)
	require.Error(t, err)
}
