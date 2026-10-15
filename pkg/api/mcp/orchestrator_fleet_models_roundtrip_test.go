//go:build mcp

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

package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// fleetModelsService answers GetFleetNode from a fixed registry and
// ListBackendModels from a fixed backend catalog, failing with the service's
// own not-found sentinels for anything else.
type fleetModelsService struct {
	fleetMockAgentService
	nodes  map[string]*orchestration.FleetNode
	models map[string][]string

	gotTenant   string
	gotAffinity string
}

func (s *fleetModelsService) GetFleetNode(_ context.Context, tenantID, nodeID string) (*orchestration.FleetNode, error) {
	s.gotTenant = tenantID
	node, ok := s.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("get %s: %w", nodeID, orchestration.ErrFleetNodeNotFound)
	}
	return node, nil
}

func (s *fleetModelsService) ListBackendModels(ctx context.Context, backendName string) ([]string, error) {
	s.gotTenant = orchestration.TenantFromContext(ctx)
	s.gotAffinity = orchestration.HostAffinityFromContext(ctx)
	models, ok := s.models[backendName]
	if !ok {
		return nil, fmt.Errorf("backend %s: %w", backendName, orchestration.ErrBackendNotFound)
	}
	return models, nil
}

func newFleetModelsService() *fleetModelsService {
	return &fleetModelsService{
		nodes: map[string]*orchestration.FleetNode{
			"node-1": {
				ID:           "node-1",
				TenantID:     "t1",
				Name:         "builder",
				Host:         "10.0.0.5:9443",
				Backend:      "claude",
				Model:        "opus",
				NodeType:     "agent",
				Status:       orchestration.FleetNodeStatus("online"),
				Capabilities: []string{"codex"},
				WorkDir:      "/srv/work",
				LastSeen:     1700000002,
				RegisteredAt: 1700000001,
				Version:      7,
				Metadata:     map[string]string{"zone": "a"},
			},
		},
		models: map[string][]string{
			"ollama": {"llama3", "qwen2"},
			"codex":  nil,
		},
	}
}

func TestFleetModels_GetFleetNode(t *testing.T) {
	svc := newFleetModelsService()
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_fleet_status")

	node, err := client.GetFleetNode(context.Background(), &orchestrator.GetFleetNodeRequest{
		TenantID: "t1", NodeID: "node-1",
	})
	require.NoError(t, err)
	require.Equal(t, "t1", svc.gotTenant)
	require.Equal(t, &orchestrator.FleetNode{
		ID:           "node-1",
		TenantID:     "t1",
		Name:         "builder",
		Host:         "10.0.0.5:9443",
		Backend:      "claude",
		Model:        "opus",
		NodeType:     "agent",
		Status:       "online",
		WorkDir:      "/srv/work",
		LastSeen:     1700000002,
		RegisteredAt: 1700000001,
		Version:      7,
		Metadata:     map[string]string{"zone": "a"},
		Backends:     []string{"claude", "codex"},
	}, node)
}

func TestFleetModels_GetFleetNode_NotFound(t *testing.T) {
	client := serveRegistryOverMCP(t, newFleetRegistry(t, newFleetModelsService()), "quicbaas_fleet_status")

	_, err := client.GetFleetNode(context.Background(), &orchestrator.GetFleetNodeRequest{
		TenantID: "t1", NodeID: "missing",
	})
	requireNotFound(t, err)
}

func TestFleetModels_ListBackendModels(t *testing.T) {
	svc := newFleetModelsService()
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_list_backend_models")

	resp, err := client.ListBackendModels(context.Background(), &orchestrator.ListBackendModelsRequest{
		NodeID: "node-1", BackendName: "ollama", TenantID: "t1",
	})
	require.NoError(t, err)
	require.Equal(t, "t1", svc.gotTenant)
	require.Equal(t, "node-1", svc.gotAffinity)
	require.Equal(t, &orchestrator.ListBackendModelsResponse{
		Backend: "ollama", Models: []string{"llama3", "qwen2"}, Count: 2,
	}, resp)
}

func TestFleetModels_ListBackendModels_AnyNodeAndNoCatalog(t *testing.T) {
	svc := newFleetModelsService()
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_list_backend_models")

	resp, err := client.ListBackendModels(context.Background(), &orchestrator.ListBackendModelsRequest{
		NodeID: "any", BackendName: "codex",
	})
	require.NoError(t, err)
	require.Equal(t, "t1", svc.gotTenant, "the caller's own tenant is used when none is named")
	require.Empty(t, svc.gotAffinity, "\"any\" must not pin a host")
	require.Equal(t, &orchestrator.ListBackendModelsResponse{
		Backend: "codex", Models: []string{}, Count: 0,
	}, resp)
}

func TestFleetModels_ListBackendModels_NotFound(t *testing.T) {
	client := serveRegistryOverMCP(t, newFleetRegistry(t, newFleetModelsService()), "quicbaas_orchestrator_list_backend_models")

	_, err := client.ListBackendModels(context.Background(), &orchestrator.ListBackendModelsRequest{
		BackendName: "nonesuch",
	})
	requireNotFound(t, err)
}

func TestFleetModels_ListBackendModels_RequiresBackendName(t *testing.T) {
	reg := newFleetRegistry(t, newFleetModelsService())

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_list_backend_models", map[string]interface{}{
		"node_id": "node-1",
	})
	require.ErrorContains(t, err, "backend_name is required")
}

func TestFleetModels_ListBackendModels_Permission(t *testing.T) {
	got, ok := qrdbToolExactMapping["quicbaas_orchestrator_list_backend_models"]
	require.True(t, ok, "quicbaas_orchestrator_list_backend_models has no permission")
	require.Equal(t, qrdbToolResource{"orchestrator", "backend_list"}, got,
		"the tool must need what GET /api/v1/fleet/{nodeID}/backends/{backendName}/models needs")
}
