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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestFleetPlacementToolNames pins the SDK methods to the tools the server
// registers (go-quicbaas#1681).
func TestFleetPlacementToolNames(t *testing.T) {
	tr := &Transport{}
	for method, want := range map[string]string{
		"DeclareFleetPlacementSpec":  "quicbaas_fleet_placement_declare",
		"GetFleetPlacementSpec":      "quicbaas_fleet_placement_get",
		"ListFleetPlacementSpecs":    "quicbaas_fleet_placement_list",
		"WithdrawFleetPlacementSpec": "quicbaas_fleet_placement_withdraw",
		"GetFleetPlacementDrift":     "quicbaas_fleet_placement_drift",
	} {
		if got := tr.mapToToolName("quicbaas.Orchestrator", method); got != want {
			t.Errorf("mapToToolName(%s) = %q, want %q", method, got, want)
		}
		if orchestratorMCPTransforms[method] == nil {
			t.Errorf("%s has no result decoder", method)
		}
	}
}

// TestDecodeMCPFleetPlacement decodes the JSON the server's tools marshal
// (orchestration.PlacementSpec and DriftReport, and the list and withdraw
// envelopes).
func TestDecodeMCPFleetPlacement(t *testing.T) {
	decode := func(method, raw string) interface{} {
		t.Helper()
		got, err := orchestratorMCPTransforms[method]([]byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return got
	}
	spec := decode("GetFleetPlacementSpec", `{"tenant_id":"acme","name":"agents","role":"agent","hosts":["h1"],
		"profile":{"cpu_cores":1.5,"memory_bytes":3,"measured":true},"actuate":false,"generation":2}`).(*orchestrator.FleetPlacementSpec)
	if spec.Name != "agents" || spec.Generation != 2 || spec.Hosts[0] != "h1" || spec.Profile.CPUCores != 1.5 {
		t.Fatalf("spec = %+v", spec)
	}
	list := decode("ListFleetPlacementSpecs", `{"specs":[{"name":"agents"}],"count":1}`).(*orchestrator.ListFleetPlacementSpecsResponse)
	if list.Count != 1 || list.Specs[0].Name != "agents" {
		t.Fatalf("list = %+v", list)
	}
	withdrawn := decode("WithdrawFleetPlacementSpec", `{"name":"agents","withdrawn":true}`).(*orchestrator.WithdrawFleetPlacementSpecResponse)
	if !withdrawn.Withdrawn {
		t.Fatalf("withdraw = %+v", withdrawn)
	}
	report := decode("GetFleetPlacementDrift", `{"spec":"agents","observed_work":1,"admissible_hosts":["h1"],
		"findings":[{"kind":"host_not_admitted","host":"h2","work_id":"s1","remedy":{"action":"move","from":"h2","destinations":["h1"]}}]}`).(*orchestrator.FleetPlacementDrift)
	if report.ObservedWork != 1 || len(report.Findings) != 1 || report.Findings[0].Remedy.Destinations[0] != "h1" {
		t.Fatalf("drift = %+v", report)
	}
	if _, err := orchestratorMCPTransforms["GetFleetPlacementDrift"]([]byte(`{`)); err == nil {
		t.Fatal("an undecodable result must be a failure, not an empty report")
	}
}
