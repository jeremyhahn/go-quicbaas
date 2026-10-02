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

package placement

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
)

// BenchmarkEvaluate measures the pure filter/score decision over a pool of
// 500 nodes with constraints, anti-affinity and a required spread.
func BenchmarkEvaluate(b *testing.B) {
	zones := []string{"a", "b", "c", "d"}
	nodes := make([]candidate, 0, 500)
	groups := groupCounts{"web": {}}
	for i := 0; i < 500; i++ {
		id := "n" + strconv.Itoa(i)
		nodes = append(nodes, candidate{
			facts:     NodeFacts{NodeID: id, Capacity: map[string]int64{"cpu": 64, "mem": 256}, Labels: map[string]string{"zone": zones[i%4], "arch": "arm64"}},
			allocated: map[string]int64{"cpu": int64(i % 60), "mem": int64(i % 200)},
		})
		if i%3 == 0 {
			groups["web"][id] = 1
		}
	}
	req := &PlacementRequest{
		WorkloadID:   "w",
		Group:        "web",
		Resources:    map[string]int64{"cpu": 2, "mem": 4},
		Constraints:  []Constraint{{Key: "arch", Operator: OperatorIn, Values: []string{"arm64"}}},
		AntiAffinity: []AffinityTerm{{Group: "web", Required: true}},
		Spread:       &SpreadConstraint{TopologyKey: "zone", MaxSkew: 2, Required: true},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = evaluate(req, nodes, groups)
	}
}

// BenchmarkPlaceAndRelease measures a full guarded placement and release
// against the in-memory store with 50 nodes.
func BenchmarkPlaceAndRelease(b *testing.B) {
	ctx := context.Background()
	p := New(placementtest.New())
	for i := 0; i < 50; i++ {
		if _, err := p.RegisterNode(ctx, "", &NodeFacts{NodeID: "n" + strconv.Itoa(i), Capacity: map[string]int64{"cpu": 1 << 30}}, time.Hour); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := "w" + strconv.Itoa(i)
		if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: id, Group: "g", Resources: map[string]int64{"cpu": 1}}); err != nil {
			b.Fatal(err)
		}
		if err := p.Release(ctx, "", id); err != nil {
			b.Fatal(err)
		}
	}
}
