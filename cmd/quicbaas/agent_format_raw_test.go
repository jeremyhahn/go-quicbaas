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
	resp := &orchestratorSDK.RegisterAgentResponse{
		Agent: &orchestratorSDK.AgentRegistration{
			ID: "ag-1", Name: "alice", Host: "h1", Status: "active",
		},
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentRegistrationResponse(resp, "raw", &buf))
	got := buf.String()
	assert.Contains(t, got, "Agent registered: ag-1")
	assert.Contains(t, got, "name: alice")
	assert.Contains(t, got, "host: h1")
}

func TestFormatAgentListResponse_Raw(t *testing.T) {
	resp := &orchestratorSDK.ListAgentsResponse{
		Agents: []*orchestratorSDK.AgentRegistration{
			{ID: "ag-1", Name: "alice", Host: "h1", Status: "active"},
			{ID: "ag-2", Name: "bob", Host: "h2", Status: "idle"},
		},
		Count: 2,
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentListResponse(resp, "raw", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1: alice (h1) [active]")
	assert.Contains(t, got, "ag-2: bob (h2) [idle]")
}

func TestFormatAgentDetailResponse_Raw(t *testing.T) {
	agent := &orchestratorSDK.AgentRegistration{
		ID: "ag-1", Name: "alice", AgentType: "worker", Status: "active", Host: "h1",
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentDetailResponse(agent, "raw", &buf))
	assert.Contains(t, buf.String(), "ag-1: alice (worker) [active] on h1")
}

func TestFormatAgentDetailResponse_TableWithMetadata(t *testing.T) {
	agent := &orchestratorSDK.AgentRegistration{
		ID:           "ag-1",
		Name:         "alice",
		AgentType:    "worker",
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
	require.NoError(t, formatAgentDetailResponse(agent, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "Backend:")
	assert.Contains(t, got, "ollama")
	assert.Contains(t, got, "Work Dir:")
	assert.Contains(t, got, "Metadata:")
	assert.Contains(t, got, "region")
}

func TestFormatAgentRemoveResponse_Raw(t *testing.T) {
	resp := &orchestratorSDK.RemoveAgentResponse{AgentID: "ag-1", Removed: true}
	var buf bytes.Buffer
	require.NoError(t, formatAgentRemoveResponse(resp, "raw", &buf))
	assert.Contains(t, buf.String(), "Agent ag-1 removed")
}

func TestFormatAgentHeartbeatResponse_Raw(t *testing.T) {
	resp := &orchestratorSDK.AgentHeartbeatResponse{AgentID: "ag-1", Heartbeat: true}
	var buf bytes.Buffer
	require.NoError(t, formatAgentHeartbeatResponse(resp, "raw", &buf))
	// The default branch prints the table-style rendering; verify it ran.
	assert.Contains(t, buf.String(), "ag-1")
}
