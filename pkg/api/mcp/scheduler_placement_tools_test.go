//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// placementMockQuicBaaS exposes a real placement engine on an in-memory store.
type placementMockQuicBaaS struct {
	mockService
	placementSvc service.PlacementService
}

func (m *placementMockQuicBaaS) Placement() service.PlacementService { return m.placementSvc }

func newPlacementRegistry(t *testing.T, svc service.PlacementService) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	return NewToolRegistry(&placementMockQuicBaaS{placementSvc: svc}, log)
}

// asJSONMap renders a tool result as the decoded JSON a client receives.
func asJSONMap(t *testing.T, v interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestPlacementTools_EndToEnd(t *testing.T) {
	// The tenant the transport's authentication binds for an authenticated caller.
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	r := newPlacementRegistry(t, service.NewPlacementService(placement.New(placementtest.New())))

	// Arguments arrive as decoded JSON, numbers as float64, exactly as an
	// MCP client sends them.
	var args map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(`{
		"node_id": "n1",
		"capacity": {"cpu": 8},
		"labels": {"zone": "a"},
		"ttl_seconds": 15
	}`), &args))
	res, err := r.ExecuteTool(ctx, "quicbaas_scheduler_register_node", args)
	require.NoError(t, err)
	assert.Equal(t, float64(15), asJSONMap(t, res)["ttl_seconds"])

	_, err = r.ExecuteTool(ctx, "quicbaas_scheduler_renew_node", map[string]interface{}{"node_id": "n1"})
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal([]byte(`{
		"workload_id": "w1",
		"group": "web",
		"resources": {"cpu": 2},
		"constraints": [{"key": "zone", "operator": "In", "values": ["a"]}],
		"anti_affinity": [{"group": "web", "required": true}],
		"spread": {"topology_key": "zone", "max_skew": 1},
		"strategy": "spread"
	}`), &args))
	res, err = r.ExecuteTool(ctx, "quicbaas_scheduler_place", args)
	require.NoError(t, err)
	placed := asJSONMap(t, res)
	assert.Equal(t, "n1", placed["placement"].(map[string]interface{})["node_id"])

	// The nested resources reached the engine: the node carries them.
	res, err = r.ExecuteTool(ctx, "quicbaas_scheduler_get_node", map[string]interface{}{"node_id": "n1"})
	require.NoError(t, err)
	assert.Equal(t, float64(2), asJSONMap(t, res)["allocated"].(map[string]interface{})["cpu"])

	// The required anti-affinity reached the engine: a second replica fails.
	args["workload_id"] = "w2"
	_, err = r.ExecuteTool(ctx, "quicbaas_scheduler_place", args)
	var none *placement.NoFeasibleNodeError
	require.True(t, stderrors.As(err, &none), "err = %v", err)

	for _, tool := range []string{"quicbaas_scheduler_list_nodes", "quicbaas_scheduler_list_placements"} {
		_, err := r.ExecuteTool(ctx, tool, map[string]interface{}{})
		require.NoError(t, err, tool)
	}
	res, err = r.ExecuteTool(ctx, "quicbaas_scheduler_list_placements", map[string]interface{}{"group": "web"})
	require.NoError(t, err)
	assert.Len(t, asJSONMap(t, res)["placements"], 1)

	_, err = r.ExecuteTool(ctx, "quicbaas_scheduler_get_placement", map[string]interface{}{"workload_id": "w1"})
	require.NoError(t, err)
	for _, tool := range []string{"quicbaas_scheduler_cordon_node", "quicbaas_scheduler_uncordon_node"} {
		_, err := r.ExecuteTool(ctx, tool, map[string]interface{}{"node_id": "n1"})
		require.NoError(t, err, tool)
	}
	res, err = r.ExecuteTool(ctx, "quicbaas_scheduler_drain_node", map[string]interface{}{"node_id": "n1"})
	require.NoError(t, err)
	assert.Len(t, asJSONMap(t, res)["placements"], 1)

	res, err = r.ExecuteTool(ctx, "quicbaas_scheduler_release", map[string]interface{}{"workload_id": "w1"})
	require.NoError(t, err)
	assert.Equal(t, true, asJSONMap(t, res)["released"])
	res, err = r.ExecuteTool(ctx, "quicbaas_scheduler_deregister_node", map[string]interface{}{"node_id": "n1"})
	require.NoError(t, err)
	assert.Equal(t, true, asJSONMap(t, res)["deregistered"])
}

func TestPlacementTools_Errors(t *testing.T) {
	// The tenant the transport's authentication binds for an authenticated caller.
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	r := newPlacementRegistry(t, service.NewPlacementService(placement.New(placementtest.New())))

	_, err := r.ExecuteTool(ctx, "quicbaas_scheduler_place", map[string]interface{}{"workload_id": "w", "resources": "not-a-map"})
	var argsErr *placementArgsError
	require.True(t, stderrors.As(err, &argsErr), "err = %v", err)
	assert.Contains(t, argsErr.Error(), "quicbaas_scheduler_place")

	_, err = r.ExecuteTool(ctx, "quicbaas_scheduler_register_node", map[string]interface{}{"node_id": "n", "capacity": []int{1}})
	require.True(t, stderrors.As(err, &argsErr))

	for _, tool := range []string{"quicbaas_scheduler_renew_node", "quicbaas_scheduler_deregister_node", "quicbaas_scheduler_drain_node"} {
		_, err := r.ExecuteTool(ctx, tool, map[string]interface{}{"node_id": "missing"})
		var notFound *placement.NodeNotFoundError
		assert.Truef(t, stderrors.As(err, &notFound), "%s: %v", tool, err)
	}
	_, err = r.ExecuteTool(ctx, "quicbaas_scheduler_release", map[string]interface{}{"workload_id": "missing"})
	var noPlacement *placement.PlacementNotFoundError
	assert.True(t, stderrors.As(err, &noPlacement))

	_, err = r.executePlacementTool(ctx, "quicbaas_scheduler_unknown", nil)
	assert.Error(t, err)

	offline := newPlacementRegistry(t, nil)
	_, err = offline.ExecuteTool(ctx, "quicbaas_scheduler_list_nodes", map[string]interface{}{})
	assert.Error(t, err)
}

func TestPlacementTools_AreRegisteredAndAuthorized(t *testing.T) {
	r := newPlacementRegistry(t, nil)
	registered := map[string]bool{}
	for _, tool := range r.ListTools() {
		registered[tool.Name] = true
	}
	for name := range placementExecutors {
		assert.Truef(t, registered[name], "%s has an executor but is not registered", name)
		assert.Truef(t, isPlacementTool(name), "%s", name)
	}
	assert.False(t, isPlacementTool("quicbaas_scheduler_create_job"))
}
