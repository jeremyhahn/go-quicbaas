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
// runFleetRegister
// =============================================================================

func TestRunAgentRegister_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		registerAgentFn: func(_ context.Context, req *orchestratorSDK.RegisterFleetNodeRequest) (*orchestratorSDK.RegisterFleetNodeResponse, error) {
			return &orchestratorSDK.RegisterFleetNodeResponse{
				Node: &orchestratorSDK.FleetNode{ID: "ag-1", Name: req.Name},
			}, nil
		},
	}
	withMockOrchestrator(t, mc)

	cmd := newFleetRegisterCommand()
	nodeName = "alice"
	nodeHost = "h1"
	nodeType = "worker"
	fleetOutputFmt = "table"
	defer func() {
		nodeName = ""
		nodeHost = ""
		nodeType = ""
		fleetOutputFmt = "table"
	}()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	require.NoError(t, runFleetRegister(cmd, nil))
}

func TestRunAgentRegister_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	withFailingOrchestratorFactory(t, want)

	cmd := newFleetRegisterCommand()
	err := runFleetRegister(cmd, nil)
	require.Error(t, err)
}

func TestRunAgentRegister_TransportError(t *testing.T) {
	want := stderrors.New("register boom")
	mc := &mockOrchestratorClient{
		registerAgentFn: func(_ context.Context, _ *orchestratorSDK.RegisterFleetNodeRequest) (*orchestratorSDK.RegisterFleetNodeResponse, error) {
			return nil, want
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetRegisterCommand()
	err := runFleetRegister(cmd, nil)
	require.Error(t, err)
}

// =============================================================================
// runFleetList
// =============================================================================

func TestRunAgentList_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		listAgentsFn: func(_ context.Context, _ *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error) {
			return &orchestratorSDK.ListFleetNodesResponse{Count: 0}, nil
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetListCommand()
	fleetOutputFmt = "table"
	defer func() { fleetOutputFmt = "table" }()
	require.NoError(t, runFleetList(cmd, nil))
}

func TestRunAgentList_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("boom"))
	cmd := newFleetListCommand()
	require.Error(t, runFleetList(cmd, nil))
}

func TestRunAgentList_TransportError(t *testing.T) {
	mc := &mockOrchestratorClient{
		listAgentsFn: func(_ context.Context, _ *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error) {
			return nil, stderrors.New("boom")
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetListCommand()
	require.Error(t, runFleetList(cmd, nil))
}

// =============================================================================
// runFleetStatus
// =============================================================================

func TestRunAgentStatus_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		getAgentFn: func(_ context.Context, _ *orchestratorSDK.GetFleetNodeRequest) (*orchestratorSDK.FleetNode, error) {
			return &orchestratorSDK.FleetNode{ID: "ag-1"}, nil
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetStatusCommand()
	fleetOutputFmt = "table"
	defer func() { fleetOutputFmt = "table" }()
	require.NoError(t, runFleetStatus(cmd, []string{"ag-1"}))
}

func TestRunAgentStatus_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("boom"))
	cmd := newFleetStatusCommand()
	require.Error(t, runFleetStatus(cmd, []string{"ag-1"}))
}

func TestRunAgentStatus_TransportError(t *testing.T) {
	mc := &mockOrchestratorClient{
		getAgentFn: func(_ context.Context, _ *orchestratorSDK.GetFleetNodeRequest) (*orchestratorSDK.FleetNode, error) {
			return nil, stderrors.New("boom")
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetStatusCommand()
	require.Error(t, runFleetStatus(cmd, []string{"ag-1"}))
}

// =============================================================================
// runFleetRemove
// =============================================================================

func TestRunAgentRemove_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		removeAgentFn: func(_ context.Context, _ *orchestratorSDK.RemoveFleetNodeRequest) (*orchestratorSDK.RemoveFleetNodeResponse, error) {
			return &orchestratorSDK.RemoveFleetNodeResponse{NodeID: "ag-1", Removed: true}, nil
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetRemoveCommand()
	fleetOutputFmt = "table"
	defer func() { fleetOutputFmt = "table" }()
	require.NoError(t, runFleetRemove(cmd, []string{"ag-1"}))
}

func TestRunAgentRemove_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("boom"))
	cmd := newFleetRemoveCommand()
	require.Error(t, runFleetRemove(cmd, []string{"ag-1"}))
}

func TestRunAgentRemove_TransportError(t *testing.T) {
	mc := &mockOrchestratorClient{
		removeAgentFn: func(_ context.Context, _ *orchestratorSDK.RemoveFleetNodeRequest) (*orchestratorSDK.RemoveFleetNodeResponse, error) {
			return nil, stderrors.New("boom")
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetRemoveCommand()
	require.Error(t, runFleetRemove(cmd, []string{"ag-1"}))
}

// =============================================================================
// runFleetHeartbeat
// =============================================================================

func TestRunAgentHeartbeat_Happy(t *testing.T) {
	mc := &mockOrchestratorClient{
		agentHeartFn: func(_ context.Context, _ *orchestratorSDK.FleetNodeHeartbeatRequest) (*orchestratorSDK.FleetNodeHeartbeatResponse, error) {
			return &orchestratorSDK.FleetNodeHeartbeatResponse{NodeID: "ag-1", Heartbeat: true}, nil
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetHeartbeatCommand()
	fleetOutputFmt = "table"
	defer func() { fleetOutputFmt = "table" }()
	require.NoError(t, runFleetHeartbeat(cmd, []string{"ag-1"}))
}

func TestRunAgentHeartbeat_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("boom"))
	cmd := newFleetHeartbeatCommand()
	require.Error(t, runFleetHeartbeat(cmd, []string{"ag-1"}))
}

func TestRunAgentHeartbeat_TransportError(t *testing.T) {
	mc := &mockOrchestratorClient{
		agentHeartFn: func(_ context.Context, _ *orchestratorSDK.FleetNodeHeartbeatRequest) (*orchestratorSDK.FleetNodeHeartbeatResponse, error) {
			return nil, stderrors.New("boom")
		},
	}
	withMockOrchestrator(t, mc)
	cmd := newFleetHeartbeatCommand()
	require.Error(t, runFleetHeartbeat(cmd, []string{"ag-1"}))
}

// silence unused-import lint when assert/orchestratorSDK only appear in
// indirect places.
var _ = assert.Equal
