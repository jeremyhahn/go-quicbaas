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

// Package main: command-handler coverage for agent.go using the
// mockOrchestratorClient defined in orchestrator_test.go.
package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// withMockOrchestrator installs a sdk-style orchestratorClientFactory
// returning the supplied mock and restores the previous factory on
// cleanup. Also ensures globalTimeout is set so the context.WithTimeout
// in the runner doesn't panic with a zero duration.
func withMockOrchestrator(t *testing.T, mc *mockOrchestratorClient) {
	t.Helper()
	orig := orchestratorClientFactory
	origTO := globalTimeout
	orchestratorClientFactory = func() (OrchestratorClient, error) { return mc, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	t.Cleanup(func() {
		orchestratorClientFactory = orig
		globalTimeout = origTO
	})
}

// withFailingOrchestratorFactory installs an orchestratorClientFactory
// that returns err.
func withFailingOrchestratorFactory(t *testing.T, err error) {
	t.Helper()
	orig := orchestratorClientFactory
	orchestratorClientFactory = func() (OrchestratorClient, error) { return nil, err }
	t.Cleanup(func() { orchestratorClientFactory = orig })
}

// =============================================================================
// runAgentRegister
// =============================================================================

func TestRunAgentRegister_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		registerAgentFn: func(_ context.Context, req *orchestratorSDK.RegisterAgentRequest) (*orchestratorSDK.RegisterAgentResponse, error) {
			return &orchestratorSDK.RegisterAgentResponse{
				Agent: &orchestratorSDK.AgentRegistration{ID: "ag-1", Name: req.Name},
			}, nil
		},
	}
	withMockOrchestrator(t, mc)

	cmd := newAgentRegisterCommand()
	agentName = "alice"
	agentHost = "h1"
	agentType = "worker"
	agentOutputFmt = "table"
	defer func() {
		agentName = ""
		agentHost = ""
		agentType = ""
		agentOutputFmt = "table"
	}()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	require.NoError(t, runAgentRegister(cmd, nil))
}

func TestRunAgentRegister_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	withFailingOrchestratorFactory(t, want)

	cmd := newAgentRegisterCommand()
	err := runAgentRegister(cmd, nil)
	require.Error(t, err)
}

func TestRunAgentRegister_TransportError(t *testing.T) {
	want := stderrors.New("register boom")
	mc := &mockOrchestratorClient{
		registerAgentFn: func(_ context.Context, _ *orchestratorSDK.RegisterAgentRequest) (*orchestratorSDK.RegisterAgentResponse, error) {
			return nil, want
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentRegisterCommand()
	err := runAgentRegister(cmd, nil)
	require.Error(t, err)
}

// =============================================================================
// runAgentList
// =============================================================================

func TestRunAgentList_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		listAgentsFn: func(_ context.Context, _ *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
			return &orchestratorSDK.ListAgentsResponse{Count: 0}, nil
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentListCommand()
	agentOutputFmt = "table"
	defer func() { agentOutputFmt = "table" }()
	require.NoError(t, runAgentList(cmd, nil))
}

func TestRunAgentList_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("boom"))
	cmd := newAgentListCommand()
	require.Error(t, runAgentList(cmd, nil))
}

func TestRunAgentList_TransportError(t *testing.T) {
	mc := &mockOrchestratorClient{
		listAgentsFn: func(_ context.Context, _ *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
			return nil, stderrors.New("boom")
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentListCommand()
	require.Error(t, runAgentList(cmd, nil))
}

// =============================================================================
// runAgentStatus
// =============================================================================

func TestRunAgentStatus_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		getAgentFn: func(_ context.Context, _ *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error) {
			return &orchestratorSDK.AgentRegistration{ID: "ag-1"}, nil
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentStatusCommand()
	agentOutputFmt = "table"
	defer func() { agentOutputFmt = "table" }()
	require.NoError(t, runAgentStatus(cmd, []string{"ag-1"}))
}

func TestRunAgentStatus_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("boom"))
	cmd := newAgentStatusCommand()
	require.Error(t, runAgentStatus(cmd, []string{"ag-1"}))
}

func TestRunAgentStatus_TransportError(t *testing.T) {
	mc := &mockOrchestratorClient{
		getAgentFn: func(_ context.Context, _ *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error) {
			return nil, stderrors.New("boom")
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentStatusCommand()
	require.Error(t, runAgentStatus(cmd, []string{"ag-1"}))
}

// =============================================================================
// runAgentRemove
// =============================================================================

func TestRunAgentRemove_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		removeAgentFn: func(_ context.Context, _ *orchestratorSDK.RemoveAgentRequest) (*orchestratorSDK.RemoveAgentResponse, error) {
			return &orchestratorSDK.RemoveAgentResponse{AgentID: "ag-1", Removed: true}, nil
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentRemoveCommand()
	agentOutputFmt = "table"
	defer func() { agentOutputFmt = "table" }()
	require.NoError(t, runAgentRemove(cmd, []string{"ag-1"}))
}

func TestRunAgentRemove_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("boom"))
	cmd := newAgentRemoveCommand()
	require.Error(t, runAgentRemove(cmd, []string{"ag-1"}))
}

func TestRunAgentRemove_TransportError(t *testing.T) {
	mc := &mockOrchestratorClient{
		removeAgentFn: func(_ context.Context, _ *orchestratorSDK.RemoveAgentRequest) (*orchestratorSDK.RemoveAgentResponse, error) {
			return nil, stderrors.New("boom")
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentRemoveCommand()
	require.Error(t, runAgentRemove(cmd, []string{"ag-1"}))
}

// =============================================================================
// runAgentHeartbeat
// =============================================================================

func TestRunAgentHeartbeat_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		agentHeartFn: func(_ context.Context, _ *orchestratorSDK.AgentHeartbeatRequest) (*orchestratorSDK.AgentHeartbeatResponse, error) {
			return &orchestratorSDK.AgentHeartbeatResponse{AgentID: "ag-1", Heartbeat: true}, nil
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentHeartbeatCommand()
	agentOutputFmt = "table"
	defer func() { agentOutputFmt = "table" }()
	require.NoError(t, runAgentHeartbeat(cmd, []string{"ag-1"}))
}

func TestRunAgentHeartbeat_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("boom"))
	cmd := newAgentHeartbeatCommand()
	require.Error(t, runAgentHeartbeat(cmd, []string{"ag-1"}))
}

func TestRunAgentHeartbeat_TransportError(t *testing.T) {
	mc := &mockOrchestratorClient{
		agentHeartFn: func(_ context.Context, _ *orchestratorSDK.AgentHeartbeatRequest) (*orchestratorSDK.AgentHeartbeatResponse, error) {
			return nil, stderrors.New("boom")
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newAgentHeartbeatCommand()
	require.Error(t, runAgentHeartbeat(cmd, []string{"ag-1"}))
}

// silence unused-import lint when assert/orchestratorSDK only appear in
// indirect places.
var _ = assert.Equal
