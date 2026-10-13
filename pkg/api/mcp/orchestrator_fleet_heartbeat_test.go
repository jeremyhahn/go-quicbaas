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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestExecuteFleetHeartbeat_MalformedObservationIsRefused(t *testing.T) {
	heartbeats := 0
	reg := newFleetRegistry(t, &fleetMockAgentService{
		heartbeatFn: func(context.Context, string, string) error {
			heartbeats++
			return nil
		},
	})
	ctx := tenantctx.WithSystem(context.Background())

	_, err := reg.executeAgentTool(ctx, "quicbaas_fleet_heartbeat", map[string]interface{}{
		"tenant_id":   "t1",
		"node_id":     "a1",
		"observation": map[string]interface{}{"backends": "claude"},
	})
	if !errors.Is(err, orchestration.ErrFleetHeartbeatReportInvalid) {
		t.Fatalf("err = %v, want ErrFleetHeartbeatReportInvalid", err)
	}
	if heartbeats != 0 {
		t.Fatal("a heartbeat with an undecodable observation must not reach the service")
	}
}

func TestFleetHeartbeatTool_SchemaDeclaresTheObservation(t *testing.T) {
	reg := newFleetRegistry(t, &fleetMockAgentService{})
	for _, tool := range reg.tools {
		if tool.Name != "quicbaas_fleet_heartbeat" {
			continue
		}
		properties, ok := tool.InputSchema["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("the heartbeat tool has no properties: %#v", tool.InputSchema)
		}
		if _, ok := properties["observation"]; !ok {
			t.Fatal("the heartbeat tool must declare its observation argument")
		}
		return
	}
	t.Fatal("quicbaas_fleet_heartbeat is not registered")
}
