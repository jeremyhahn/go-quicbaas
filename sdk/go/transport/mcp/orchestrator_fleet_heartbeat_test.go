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

package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	// pkg/api/mcp imports this package for the MCP wire error codes, so
	// only an external test package may import it back.
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/fleetparity"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// heartbeatOverMCP encodes req as the SDK MCP transport does, runs the tool
// the SDK names on the real server tool registry, decodes the result as the
// SDK does, and returns what reached the agent service.
func heartbeatOverMCP(t *testing.T, req *orchestrator.FleetNodeHeartbeatRequest) *fleetparity.Recorder {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	recorder := fleetparity.NewRecorder()
	registry := mcpapi.NewToolRegistry(recorder, log)

	tool := mcp.ToolName("quicbaas.Orchestrator", "FleetNodeHeartbeat")
	result, err := registry.ExecuteTool(context.Background(), tool, mcp.ConvertToArguments(req))
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal %s result: %v", tool, err)
	}
	resp, err := mcp.DecodeMCPFleetNodeHeartbeat(raw)
	if err != nil {
		t.Fatalf("decode %s result: %v", tool, err)
	}
	ack, ok := resp.(*orchestrator.FleetNodeHeartbeatResponse)
	if !ok || !ack.Heartbeat || ack.NodeID != req.NodeID {
		t.Fatalf("%s answered %#v, want an acknowledgement for %s", tool, resp, req.NodeID)
	}
	return recorder
}

func TestFleetHeartbeat_ObservationSurvivesMCP(t *testing.T) {
	recorder := heartbeatOverMCP(t, &orchestrator.FleetNodeHeartbeatRequest{
		TenantID:    "tenant-1",
		NodeID:      "node-1",
		Observation: fleetparity.Observation(),
	})
	fleetparity.RequireParity(t, recorder.Observed(t))
}

func TestFleetHeartbeat_BareHeartbeatOverMCPObservesNothing(t *testing.T) {
	recorder := heartbeatOverMCP(t, &orchestrator.FleetNodeHeartbeatRequest{TenantID: "tenant-1", NodeID: "node-1"})
	if observed := recorder.Observed(t); observed != nil {
		t.Fatalf("a bare heartbeat observed %#v, want nothing", observed)
	}
}

func TestDecodeMCPFleetNodeHeartbeat_RejectsMalformedResult(t *testing.T) {
	if _, err := mcp.DecodeMCPFleetNodeHeartbeat([]byte(`{"acknowledged":"yes"}`)); err == nil {
		t.Fatal("a malformed acknowledgement must fail to decode")
	}
}
