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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// newTestAgentBinding constructs an AgentBinding whose ConnectionBinding
// holds a fake client returning the supplied mockOrchestratorClient.
func newTestAgentBinding(t *testing.T, mock *mockOrchestratorClient) *AgentBinding {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)

	conn := NewConnectionBinding(log)
	var client quicbaas.Client = &sessionBindingClient{orch: mock}
	conn.client.Store(&client)

	binding := NewAgentBinding(log, conn)
	binding.ctx = context.Background()
	return binding
}

// -----------------------------------------------------------------------------
// List
// -----------------------------------------------------------------------------

func TestAgentBinding_List_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		listAgentsFn: func(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
			assert.Equal(t, "acme", req.TenantID)
			assert.Equal(t, "running", req.Status)
			assert.Equal(t, "claude", req.Backend)
			return &orchestratorSDK.ListAgentsResponse{
				Agents: []*orchestratorSDK.AgentRegistration{
					{ID: "a1", Name: "alpha", Status: "running"},
					{ID: "a2", Name: "beta", Status: "idle"},
				},
				Count: 2,
			}, nil
		},
	}
	binding := newTestAgentBinding(t, mock)

	agents, err := binding.List(map[string]interface{}{
		"tenant_id": "acme",
		"status":    "running",
		"backend":   "claude",
	})
	require.NoError(t, err)
	require.Len(t, agents, 2)
	assert.Equal(t, "a1", agents[0].ID)
	assert.Equal(t, "a2", agents[1].ID)
}

func TestAgentBinding_List_TransportError(t *testing.T) {
	sentinel := errors.New("list failed")
	mock := &mockOrchestratorClient{
		listAgentsFn: func(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
			return nil, sentinel
		},
	}
	binding := newTestAgentBinding(t, mock)

	agents, err := binding.List(nil)
	require.Error(t, err)
	assert.Nil(t, agents)
	assert.ErrorIs(t, err, sentinel)

	var qberr *qberrors.QBError
	require.ErrorAs(t, err, &qberr)
	assert.Equal(t, "AgentBinding.List", qberr.Op)
}

// -----------------------------------------------------------------------------
// Get
// -----------------------------------------------------------------------------

func TestAgentBinding_Get_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		getAgentFn: func(ctx context.Context, req *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error) {
			assert.Equal(t, "default", req.TenantID)
			assert.Equal(t, "agent-7", req.AgentID)
			return &orchestratorSDK.AgentRegistration{ID: "agent-7", Name: "g7", Status: "running"}, nil
		},
	}
	binding := newTestAgentBinding(t, mock)

	agent, err := binding.Get("agent-7")
	require.NoError(t, err)
	require.NotNil(t, agent)
	assert.Equal(t, "agent-7", agent.ID)
}

func TestAgentBinding_Get_EmptyID(t *testing.T) {
	binding := newTestAgentBinding(t, &mockOrchestratorClient{})

	agent, err := binding.Get("")
	require.Error(t, err)
	assert.Nil(t, agent)

	var typed *GUIAgentIDRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestAgentBinding_Get_TransportError(t *testing.T) {
	sentinel := errors.New("agent get failed")
	mock := &mockOrchestratorClient{
		getAgentFn: func(ctx context.Context, req *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error) {
			return nil, sentinel
		},
	}
	binding := newTestAgentBinding(t, mock)

	agent, err := binding.Get("agent-x")
	require.Error(t, err)
	assert.Nil(t, agent)
	assert.ErrorIs(t, err, sentinel)
}

// -----------------------------------------------------------------------------
// Status
// -----------------------------------------------------------------------------

func TestAgentBinding_Status_ProjectsFields(t *testing.T) {
	mock := &mockOrchestratorClient{
		getAgentFn: func(ctx context.Context, req *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error) {
			return &orchestratorSDK.AgentRegistration{
				ID:        "a1",
				TenantID:  "t1",
				Name:      "agent-1",
				Host:      "host.local",
				Status:    "running",
				Backend:   "claude",
				Model:     "sonnet",
				AgentType: "dev",
				LastSeen:  42,
				Version:   9,
			}, nil
		},
	}
	binding := newTestAgentBinding(t, mock)

	status, err := binding.Status("a1")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "a1", status.AgentID)
	assert.Equal(t, "host.local", status.Host)
	assert.Equal(t, "running", status.Status)
	assert.Equal(t, int64(42), status.LastSeen)
	assert.Equal(t, uint64(9), status.Version)
}

func TestAgentBinding_Status_EmptyID(t *testing.T) {
	binding := newTestAgentBinding(t, &mockOrchestratorClient{})

	status, err := binding.Status("")
	require.Error(t, err)
	assert.Nil(t, status)

	var typed *GUIAgentIDRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// Heartbeat
// -----------------------------------------------------------------------------

func TestAgentBinding_Heartbeat_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		agentHeartFn: func(ctx context.Context, req *orchestratorSDK.AgentHeartbeatRequest) (*orchestratorSDK.AgentHeartbeatResponse, error) {
			assert.Equal(t, "default", req.TenantID)
			assert.Equal(t, "agent-heart", req.AgentID)
			return &orchestratorSDK.AgentHeartbeatResponse{AgentID: req.AgentID, Heartbeat: true}, nil
		},
	}
	binding := newTestAgentBinding(t, mock)

	require.NoError(t, binding.Heartbeat("agent-heart"))
}

func TestAgentBinding_Heartbeat_EmptyID(t *testing.T) {
	binding := newTestAgentBinding(t, &mockOrchestratorClient{})

	err := binding.Heartbeat("")
	require.Error(t, err)

	var typed *GUIAgentIDRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestAgentBinding_Heartbeat_TransportError(t *testing.T) {
	sentinel := errors.New("heartbeat failed")
	mock := &mockOrchestratorClient{
		agentHeartFn: func(ctx context.Context, req *orchestratorSDK.AgentHeartbeatRequest) (*orchestratorSDK.AgentHeartbeatResponse, error) {
			return nil, sentinel
		},
	}
	binding := newTestAgentBinding(t, mock)

	err := binding.Heartbeat("agent-x")
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

// -----------------------------------------------------------------------------
// Lifecycle
// -----------------------------------------------------------------------------

func TestAgentBinding_Startup_StoresContext(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)
	conn := NewConnectionBinding(log)
	binding := NewAgentBinding(log, conn)

	ctx := context.Background()
	binding.startup(ctx)
	assert.Equal(t, ctx, binding.ctx)
}

func TestAgentBinding_Shutdown_Noop(t *testing.T) {
	binding := newTestAgentBinding(t, &mockOrchestratorClient{})
	// Should not panic or error.
	binding.shutdown(context.Background())
}
