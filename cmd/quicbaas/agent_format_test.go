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
// (formatAgentRegistrationResponse, formatAgentListResponse, ...) are
// pure rendering and easy to test without an actual SDK connection.
package main

import (
	"bytes"
	"testing"
	"time"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// formatAgentRegistrationResponse
// =============================================================================

func TestFormatAgentRegistrationResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.RegisterAgentResponse{
		Agent: &orchestratorSDK.AgentRegistration{
			ID:           "ag-1",
			Name:         "alice",
			Host:         "h1",
			AgentType:    "worker",
			Status:       "active",
			Backend:      "ollama",
			WorkDir:      "/tmp",
			RegisteredAt: time.Now().UnixNano(),
		},
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentRegistrationResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1")
	assert.Contains(t, got, "alice")
	assert.Contains(t, got, "ollama")
}

func TestFormatAgentRegistrationResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.RegisterAgentResponse{
		Agent: &orchestratorSDK.AgentRegistration{ID: "ag-1", Name: "alice"},
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentRegistrationResponse(resp, "json", &buf))
	got := buf.String()
	assert.Contains(t, got, `"id"`)
	assert.Contains(t, got, "ag-1")
}

func TestFormatAgentRegistrationResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.RegisterAgentResponse{
		Agent: &orchestratorSDK.AgentRegistration{ID: "ag-1"},
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentRegistrationResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentRegistrationResponse_InvalidFormat(t *testing.T) {
	resp := &orchestratorSDK.RegisterAgentResponse{Agent: &orchestratorSDK.AgentRegistration{}}
	var buf bytes.Buffer
	err := formatAgentRegistrationResponse(resp, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatAgentListResponse
// =============================================================================

func TestFormatAgentListResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.ListAgentsResponse{
		Agents: []*orchestratorSDK.AgentRegistration{
			{ID: "ag-1", Name: "alice", Host: "h1", AgentType: "worker", Status: "active", LastSeen: time.Now().UnixNano()},
			{ID: "ag-2", Name: "bob", Host: "h2", AgentType: "worker", Status: "idle"},
		},
		Count: 2,
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentListResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1")
	assert.Contains(t, got, "ag-2")
	assert.Contains(t, got, "Total: 2 agents")
}

func TestFormatAgentListResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.ListAgentsResponse{
		Agents: []*orchestratorSDK.AgentRegistration{{ID: "a"}},
		Count:  1,
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentListResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"id"`)
}

func TestFormatAgentListResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.ListAgentsResponse{
		Agents: []*orchestratorSDK.AgentRegistration{{ID: "a"}},
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentListResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "a")
}

func TestFormatAgentListResponse_InvalidFormat(t *testing.T) {
	resp := &orchestratorSDK.ListAgentsResponse{}
	var buf bytes.Buffer
	err := formatAgentListResponse(resp, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatAgentDetailResponse
// =============================================================================

func TestFormatAgentDetailResponse_Table(t *testing.T) {
	agent := &orchestratorSDK.AgentRegistration{
		ID: "ag-1", TenantID: "t1", Name: "alice", Host: "h1",
		AgentType: "worker", Status: "active", Backend: "ollama",
		WorkDir: "/tmp", Version: 3,
		RegisteredAt: time.Now().UnixNano(),
		LastSeen:     time.Now().UnixNano(),
		Metadata:     map[string]string{"key": "value"},
	}
	var buf bytes.Buffer
	require.NoError(t, formatAgentDetailResponse(agent, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1")
	assert.Contains(t, got, "alice")
	assert.Contains(t, got, "ollama")
	assert.Contains(t, got, "Metadata")
	assert.Contains(t, got, "key")
}

func TestFormatAgentDetailResponse_JSON(t *testing.T) {
	agent := &orchestratorSDK.AgentRegistration{ID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatAgentDetailResponse(agent, "json", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentDetailResponse_YAML(t *testing.T) {
	agent := &orchestratorSDK.AgentRegistration{ID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatAgentDetailResponse(agent, "yaml", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentDetailResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatAgentDetailResponse(&orchestratorSDK.AgentRegistration{}, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatAgentRemoveResponse
// =============================================================================

func TestFormatAgentRemoveResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.RemoveAgentResponse{AgentID: "ag-1", Removed: true}
	var buf bytes.Buffer
	require.NoError(t, formatAgentRemoveResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "ag-1")
	assert.Contains(t, got, "true")
}

func TestFormatAgentRemoveResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.RemoveAgentResponse{AgentID: "ag-1", Removed: true}
	var buf bytes.Buffer
	require.NoError(t, formatAgentRemoveResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"agent_id"`)
}

func TestFormatAgentRemoveResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.RemoveAgentResponse{AgentID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatAgentRemoveResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentRemoveResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatAgentRemoveResponse(&orchestratorSDK.RemoveAgentResponse{}, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatAgentHeartbeatResponse
// =============================================================================

func TestFormatAgentHeartbeatResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.AgentHeartbeatResponse{AgentID: "ag-1", Heartbeat: true}
	var buf bytes.Buffer
	require.NoError(t, formatAgentHeartbeatResponse(resp, "table", &buf))
	assert.Contains(t, buf.String(), "ag-1")
	assert.Contains(t, buf.String(), "true")
}

func TestFormatAgentHeartbeatResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.AgentHeartbeatResponse{AgentID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatAgentHeartbeatResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"agent_id"`)
}

func TestFormatAgentHeartbeatResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.AgentHeartbeatResponse{AgentID: "ag-1"}
	var buf bytes.Buffer
	require.NoError(t, formatAgentHeartbeatResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "ag-1")
}

func TestFormatAgentHeartbeatResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatAgentHeartbeatResponse(&orchestratorSDK.AgentHeartbeatResponse{}, "bogus", &buf)
	require.Error(t, err)
}
