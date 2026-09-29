// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

// Package main: raw-format (default switch branch) coverage for the agent.go
// rendering helpers. The existing agent_format_test.go covers table / json /
// yaml / invalid but not the raw default branch.
package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestFormatAgentRegistrationResponse_Raw(t *testing.T) {
	resp := &orchestratorSDK.RegisterFleetNodeResponse{
		Node: &orchestratorSDK.FleetNode{
			ID: "ag-1", Name: "alice", Host: "h1", Status: "active",
		},
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetRegistrationResponse(resp, "raw", &buf))
	got := buf.String()
	assert.Contains(t, got, "Fleet node registered: ag-1")
	assert.Contains(t, got, "name: alice")
	assert.Contains(t, got, "host: h1")
}

func TestFormatAgentListResponse_Raw(t *testing.T) {
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: "ag-1", Name: "alice", Host: "h1", Status: "active"},
			{ID: "ag-2", Name: "bob", Host: "h2", Status: "idle"},
		},
		Count: 2,
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetListResponse(resp, "raw", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1: alice (h1) [active]")
	assert.Contains(t, got, "ag-2: bob (h2) [idle]")
}

func TestFormatAgentDetailResponse_Raw(t *testing.T) {
	agent := &orchestratorSDK.FleetNode{
		ID: "ag-1", Name: "alice", NodeType: "worker", Status: "active", Host: "h1",
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetDetailResponse(agent, "raw", &buf))
	assert.Contains(t, buf.String(), "ag-1: alice (worker) [active] on h1")
}

func TestFormatAgentDetailResponse_TableWithMetadata(t *testing.T) {
	agent := &orchestratorSDK.FleetNode{
		ID:           "ag-1",
		Name:         "alice",
		NodeType:     "worker",
		Status:       "active",
		Host:         "h1",
		Backend:      "ollama",
		WorkDir:      "/srv",
		Version:      3,
		RegisteredAt: time.Now().UnixNano(),
		LastSeen:     time.Now().UnixNano(),
		Metadata:     map[string]string{"region": "us-east"},
	}
	var buf bytes.Buffer
	require.NoError(t, formatFleetDetailResponse(agent, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "Backend:")
	assert.Contains(t, got, "ollama")
	assert.Contains(t, got, "Work Dir:")
	assert.Contains(t, got, "Metadata:")
	assert.Contains(t, got, "region")
}

func TestFormatAgentRemoveResponse_Raw(t *testing.T) {
	resp := &orchestratorSDK.RemoveFleetNodeResponse{NodeID: "ag-1", Removed: true}
	var buf bytes.Buffer
	require.NoError(t, formatFleetRemoveResponse(resp, "raw", &buf))
	assert.Contains(t, buf.String(), "Fleet node ag-1 removed")
}

func TestFormatAgentHeartbeatResponse_Raw(t *testing.T) {
	resp := &orchestratorSDK.FleetNodeHeartbeatResponse{NodeID: "ag-1", Heartbeat: true}
	var buf bytes.Buffer
	require.NoError(t, formatFleetHeartbeatResponse(resp, "raw", &buf))
	// The default branch prints the table-style rendering; verify it ran.
	assert.Contains(t, buf.String(), "ag-1")
}
