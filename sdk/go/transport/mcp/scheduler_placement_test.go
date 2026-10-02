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
	"encoding/json"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// asToolArgs renders a value the way it reaches a tool: through JSON.
func asToolArgs(t *testing.T, v interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTransformPlacementRequest_RegisterNodeIsFlat(t *testing.T) {
	args, err := transformPlacementRequest("RegisterNode", &scheduler.RegisterNodeRequest{
		Facts:      scheduler.NodeFacts{NodeID: "n1", Capacity: map[string]int64{"cpu": 4}, Labels: map[string]string{"zone": "a"}},
		TTLSeconds: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	wire := asToolArgs(t, args)
	if wire["node_id"] != "n1" || wire["ttl_seconds"] != float64(9) ||
		wire["capacity"].(map[string]interface{})["cpu"] != float64(4) ||
		wire["labels"].(map[string]interface{})["zone"] != "a" {
		t.Fatalf("register_node arguments = %v", wire)
	}
	if _, err := transformPlacementRequest("RegisterNode", (*scheduler.RegisterNodeRequest)(nil)); err == nil {
		t.Fatal("nil RegisterNode request accepted")
	}
}

func TestTransformPlacementRequest_PlaceCarriesNestedFields(t *testing.T) {
	args, err := transformPlacementRequest("Place", &scheduler.PlacementRequest{
		WorkloadID:  "w",
		Group:       "web",
		Resources:   map[string]int64{"cpu": 2},
		Constraints: []scheduler.Constraint{{Key: "arch", Operator: scheduler.OperatorIn, Values: []string{"arm64"}}},
		Spread:      &scheduler.SpreadConstraint{TopologyKey: "zone", MaxSkew: 1, Required: true},
		Strategy:    scheduler.StrategySpread,
	})
	if err != nil {
		t.Fatal(err)
	}
	wire := asToolArgs(t, args)
	if wire["workload_id"] != "w" || wire["strategy"] != "spread" {
		t.Fatalf("place arguments = %v", wire)
	}
	constraint := wire["constraints"].([]interface{})[0].(map[string]interface{})
	if constraint["operator"] != "In" || constraint["values"].([]interface{})[0] != "arm64" {
		t.Fatalf("constraint = %v", constraint)
	}
	if wire["spread"].(map[string]interface{})["max_skew"] != float64(1) {
		t.Fatalf("spread = %v", wire["spread"])
	}

	nodeArgs, err := transformPlacementRequest("GetNode", &scheduler.NodeRequest{NodeID: "n1"})
	if err != nil || nodeArgs["node_id"] != "n1" {
		t.Fatalf("get_node arguments = %v, %v", nodeArgs, err)
	}
	if _, err := transformPlacementRequest("Place", make(chan int)); err == nil {
		t.Fatal("an unencodable request was accepted")
	}
}

func TestDecodePlacementResult_EveryMethodDecodesItsToolResult(t *testing.T) {
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	placementJSON := map[string]interface{}{"workload_id": "w", "node_id": "n1", "created_at": created.Format(time.RFC3339), "resources": map[string]interface{}{"cpu": 2}}
	nodeJSON := map[string]interface{}{"facts": map[string]interface{}{"node_id": "n1"}, "status": map[string]interface{}{"draining": true}, "lease_id": 7}
	results := map[string]interface{}{
		"RegisterNode":   nodeJSON,
		"GetNode":        nodeJSON,
		"CordonNode":     nodeJSON,
		"UncordonNode":   nodeJSON,
		"RenewNode":      map[string]interface{}{"node_id": "n1", "renewed": true},
		"DeregisterNode": map[string]interface{}{"node_id": "n1", "deregistered": true},
		"ListNodes":      map[string]interface{}{"nodes": []interface{}{nodeJSON}},
		"DrainNode":      map[string]interface{}{"node_id": "n1", "placements": []interface{}{placementJSON}},
		"Place":          map[string]interface{}{"placement": placementJSON, "existing": true},
		"Release":        map[string]interface{}{"workload_id": "w", "released": true},
		"GetPlacement":   placementJSON,
		"ListPlacements": map[string]interface{}{"placements": []interface{}{placementJSON}},
	}
	h := newSchedulerHandler()
	for method, result := range results {
		got, err := h.transformResponse(method, result)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		switch v := got.(type) {
		case *scheduler.NodeInfo:
			if v.Facts.NodeID != "n1" || !v.Status.Draining || v.LeaseID != 7 {
				t.Errorf("%s decoded %+v", method, v)
			}
		case *scheduler.PlaceResult:
			if !v.Existing || v.Placement.Resources["cpu"] != 2 || !v.Placement.CreatedAt.Equal(created) {
				t.Errorf("%s decoded %+v", method, v)
			}
		case *scheduler.ListPlacementsResponse:
			if len(v.Placements) != 1 {
				t.Errorf("%s decoded %+v", method, v)
			}
		case *scheduler.DrainNodeResponse:
			if len(v.Placements) != 1 || v.Placements[0].NodeID != "n1" {
				t.Errorf("%s decoded %+v", method, v)
			}
		}
	}
	if _, err := decodePlacementResult(&scheduler.NodeInfo{}, map[string]interface{}{"lease_id": "not-a-number"}); err == nil {
		t.Fatal("a mismatched tool result decoded")
	}
	if _, err := decodePlacementResult(&scheduler.NodeInfo{}, make(chan int)); err == nil {
		t.Fatal("an unencodable tool result decoded")
	}
}
