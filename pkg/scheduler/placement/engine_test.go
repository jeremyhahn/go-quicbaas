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
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// node builds a candidate with a single "cpu" capacity and the given labels.
func node(id string, cpu, allocated int64, labels map[string]string) candidate {
	return candidate{
		facts:     NodeFacts{NodeID: id, Capacity: map[string]int64{"cpu": cpu}, Labels: labels},
		allocated: map[string]int64{"cpu": allocated},
	}
}

func TestEvaluate_BinPackPrefersTheFullestNodeThatFits(t *testing.T) {
	req := &PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"cpu": 2}}
	d := evaluate(req, []candidate{
		node("a", 10, 1, nil),
		node("b", 10, 7, nil),
		node("c", 10, 9, nil), // does not fit: 1 free
	}, groupCounts{})
	if got := d.ranked[0].node.facts.NodeID; got != "b" {
		t.Fatalf("bin-pack chose %s, want b", got)
	}
	if !strings.Contains(d.rejections["c"], "insufficient cpu") {
		t.Fatalf("c rejection = %q, want insufficient cpu", d.rejections["c"])
	}
}

func TestEvaluate_SpreadStrategyPrefersTheEmptiestNode(t *testing.T) {
	req := &PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"cpu": 1}, Strategy: StrategySpread}
	d := evaluate(req, []candidate{node("a", 10, 5, nil), node("b", 10, 1, nil)}, groupCounts{})
	if got := d.ranked[0].node.facts.NodeID; got != "b" {
		t.Fatalf("spread chose %s, want b", got)
	}
}

func TestEvaluate_TiesBreakByNodeID(t *testing.T) {
	req := &PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"cpu": 1}}
	d := evaluate(req, []candidate{node("z", 10, 0, nil), node("m", 10, 0, nil), node("a", 10, 0, nil)}, groupCounts{})
	if got := d.ranked[0].node.facts.NodeID; got != "a" {
		t.Fatalf("tie chose %s, want a", got)
	}
}

func TestEvaluate_RejectsCordonedDrainingAndMissingResources(t *testing.T) {
	cordoned := node("cordoned", 10, 0, nil)
	cordoned.status.Unschedulable = true
	draining := node("draining", 10, 0, nil)
	draining.status = NodeStatus{Unschedulable: true, Draining: true}
	req := &PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"gpu": 1}}
	d := evaluate(req, []candidate{cordoned, draining, node("nogpu", 10, 0, nil)}, groupCounts{})

	if len(d.ranked) != 0 {
		t.Fatalf("expected no feasible node, got %d", len(d.ranked))
	}
	want := map[string]string{
		"cordoned": "node is cordoned",
		"draining": "node is draining",
		"nogpu":    "node does not offer resource gpu",
	}
	for id, reason := range want {
		if d.rejections[id] != reason {
			t.Errorf("rejection[%s] = %q, want %q", id, d.rejections[id], reason)
		}
	}
}

func TestEvaluate_LabelConstraintOperators(t *testing.T) {
	nodes := []candidate{
		node("arm", 10, 0, map[string]string{"arch": "arm64", "tier": "measured"}),
		node("x86", 10, 0, map[string]string{"arch": "amd64"}),
	}
	cases := []struct {
		name string
		c    Constraint
		want string
	}{
		{"In", Constraint{Key: "arch", Operator: OperatorIn, Values: []string{"arm64"}}, "arm"},
		{"NotIn", Constraint{Key: "arch", Operator: OperatorNotIn, Values: []string{"arm64"}}, "x86"},
		{"Exists", Constraint{Key: "tier", Operator: OperatorExists}, "arm"},
		{"DoesNotExist", Constraint{Key: "tier", Operator: OperatorDoesNotExist}, "x86"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := evaluate(&PlacementRequest{WorkloadID: "w", Constraints: []Constraint{tc.c}}, nodes, groupCounts{})
			if len(d.ranked) != 1 || d.ranked[0].node.facts.NodeID != tc.want {
				t.Fatalf("ranked = %+v, want only %s", d.ranked, tc.want)
			}
		})
	}
}

func TestEvaluate_RequiredAffinityAndAntiAffinity(t *testing.T) {
	nodes := []candidate{node("a", 10, 0, nil), node("b", 10, 0, nil)}
	groups := groupCounts{"db": {"a": 1}}

	d := evaluate(&PlacementRequest{WorkloadID: "w", Affinity: []AffinityTerm{{Group: "db", Required: true}}}, nodes, groups)
	if len(d.ranked) != 1 || d.ranked[0].node.facts.NodeID != "a" {
		t.Fatalf("required affinity ranked %+v, want only a", d.ranked)
	}

	d = evaluate(&PlacementRequest{WorkloadID: "w", AntiAffinity: []AffinityTerm{{Group: "db", Required: true}}}, nodes, groups)
	if len(d.ranked) != 1 || d.ranked[0].node.facts.NodeID != "b" {
		t.Fatalf("required anti-affinity ranked %+v, want only b", d.ranked)
	}
	if !strings.Contains(d.rejections["a"], "anti-affinity") {
		t.Fatalf("a rejection = %q", d.rejections["a"])
	}
}

func TestEvaluate_PreferredAffinityOutweighsStrategy(t *testing.T) {
	nodes := []candidate{node("busy", 10, 9, nil), node("idle", 10, 0, nil)}
	groups := groupCounts{"cache": {"idle": 1}}
	req := &PlacementRequest{
		WorkloadID: "w",
		Resources:  map[string]int64{"cpu": 1},
		Affinity:   []AffinityTerm{{Group: "cache", Weight: 100}},
	}
	d := evaluate(req, nodes, groups)
	if got := d.ranked[0].node.facts.NodeID; got != "idle" {
		t.Fatalf("preferred affinity chose %s, want idle", got)
	}

	req.Affinity = nil
	req.AntiAffinity = []AffinityTerm{{Group: "cache", Weight: 100}}
	d = evaluate(req, []candidate{node("busy", 10, 5, nil), node("idle", 10, 0, nil)}, groups)
	if got := d.ranked[0].node.facts.NodeID; got != "busy" {
		t.Fatalf("preferred anti-affinity chose %s, want busy", got)
	}
}

func TestEvaluate_RequiredSpreadBoundsSkew(t *testing.T) {
	nodes := []candidate{
		node("a1", 10, 0, map[string]string{"zone": "a"}),
		node("b1", 10, 0, map[string]string{"zone": "b"}),
		node("nolabel", 10, 0, nil),
	}
	groups := groupCounts{"web": {"a1": 2, "b1": 1}}
	req := &PlacementRequest{
		WorkloadID: "w",
		Group:      "web",
		Spread:     &SpreadConstraint{TopologyKey: "zone", MaxSkew: 1, Required: true},
	}
	d := evaluate(req, nodes, groups)
	if len(d.ranked) != 1 || d.ranked[0].node.facts.NodeID != "b1" {
		t.Fatalf("required spread ranked %+v, want only b1", d.ranked)
	}
	if !strings.Contains(d.rejections["a1"], "skew 2") {
		t.Fatalf("a1 rejection = %q, want a skew of 2", d.rejections["a1"])
	}
	if !strings.Contains(d.rejections["nolabel"], "no zone label") {
		t.Fatalf("nolabel rejection = %q", d.rejections["nolabel"])
	}
}

func TestEvaluate_PreferredSpreadOnlyLowersTheScore(t *testing.T) {
	nodes := []candidate{
		node("a1", 10, 0, map[string]string{"zone": "a"}),
		node("b1", 10, 0, map[string]string{"zone": "b"}),
	}
	groups := groupCounts{"web": {"a1": 3}}
	req := &PlacementRequest{WorkloadID: "w", Group: "web", Spread: &SpreadConstraint{TopologyKey: "zone", MaxSkew: 1}}
	d := evaluate(req, nodes, groups)
	if len(d.ranked) != 2 {
		t.Fatalf("preferred spread filtered nodes: %+v", d.rejections)
	}
	if got := d.ranked[0].node.facts.NodeID; got != "b1" {
		t.Fatalf("preferred spread chose %s, want b1", got)
	}
}

func TestSkewAfter_EmptyDomainsCountFromZero(t *testing.T) {
	if got := skewAfter(map[string]int64{}, "a"); got != 1 {
		t.Fatalf("skewAfter on no domains = %d, want 1", got)
	}
}

func TestUtilisation_NoResourcesAnywhereIsZero(t *testing.T) {
	c := candidate{facts: NodeFacts{NodeID: "n"}}
	if got := utilisation(&PlacementRequest{WorkloadID: "w"}, c); got != 0 {
		t.Fatalf("utilisation = %v, want 0", got)
	}
}

// TestEvaluate_Properties runs the engine over random clusters and requests
// and checks the invariants a placement must never break, whatever the
// scores: the chosen node has room for every requested resource, required
// anti-affinity is honoured, and a required spread's skew stays bounded.
func TestEvaluate_Properties(t *testing.T) {
	rng := rand.New(rand.NewSource(1143))
	zones := []string{"a", "b", "c"}

	for i := 0; i < 2000; i++ {
		var nodes []candidate
		groups := groupCounts{"web": {}}
		for n := 0; n < 1+rng.Intn(8); n++ {
			id := "n" + strconv.Itoa(n)
			capacity := int64(1 + rng.Intn(16))
			c := candidate{
				facts: NodeFacts{
					NodeID:   id,
					Capacity: map[string]int64{"cpu": capacity, "mem": int64(1 + rng.Intn(64))},
					Labels:   map[string]string{"zone": zones[rng.Intn(len(zones))]},
				},
				allocated: map[string]int64{"cpu": rng.Int63n(capacity + 1)},
				status:    NodeStatus{Unschedulable: rng.Intn(10) == 0},
			}
			nodes = append(nodes, c)
			if rng.Intn(3) == 0 {
				groups["web"][id] = int64(1 + rng.Intn(3))
			}
		}
		req := &PlacementRequest{
			WorkloadID: "w",
			Group:      "web",
			Resources:  map[string]int64{"cpu": int64(1 + rng.Intn(6))},
			Strategy:   []Strategy{StrategyBinPack, StrategySpread}[rng.Intn(2)],
		}
		if rng.Intn(2) == 0 {
			req.AntiAffinity = []AffinityTerm{{Group: "web", Required: true}}
		}
		if rng.Intn(2) == 0 {
			req.Spread = &SpreadConstraint{TopologyKey: "zone", MaxSkew: 1 + rng.Intn(2), Required: true}
		}

		d := evaluate(req, nodes, groups)
		if len(d.ranked)+len(d.rejections) != len(nodes) {
			t.Fatalf("iteration %d: %d ranked + %d rejected != %d nodes", i, len(d.ranked), len(d.rejections), len(nodes))
		}
		if len(d.ranked) == 0 {
			continue
		}
		chosen := d.ranked[0].node
		if chosen.status.Unschedulable {
			t.Fatalf("iteration %d: chose a cordoned node", i)
		}
		if chosen.allocated["cpu"]+req.Resources["cpu"] > chosen.facts.Capacity["cpu"] {
			t.Fatalf("iteration %d: overcommitted %s", i, chosen.facts.NodeID)
		}
		if len(req.AntiAffinity) > 0 && groups.hosts("web", chosen.facts.NodeID) {
			t.Fatalf("iteration %d: anti-affinity violated on %s", i, chosen.facts.NodeID)
		}
		if req.Spread != nil {
			var eligible []candidate
			for _, n := range nodes {
				if firstRejection(req, n, groups) == "" {
					eligible = append(eligible, n)
				}
			}
			domains := spreadDomains(req.Spread, "web", eligible, groups)
			if skew := skewAfter(domains, chosen.facts.Labels["zone"]); skew > int64(req.Spread.MaxSkew) {
				t.Fatalf("iteration %d: skew %d above %d", i, skew, req.Spread.MaxSkew)
			}
		}
	}
}

func TestValidatePlacementRequest(t *testing.T) {
	valid := func() *PlacementRequest {
		return &PlacementRequest{WorkloadID: "w", Group: "g", Resources: map[string]int64{"cpu": 1}}
	}
	if err := ValidatePlacementRequest(valid()); err != nil {
		t.Fatalf("valid request refused: %v", err)
	}

	cases := map[string]func(*PlacementRequest){
		"nil-like empty workload": func(r *PlacementRequest) { r.WorkloadID = "" },
		"slash in workload":       func(r *PlacementRequest) { r.WorkloadID = "a/b" },
		"long workload":           func(r *PlacementRequest) { r.WorkloadID = strings.Repeat("x", maxIDLen+1) },
		"slash in group":          func(r *PlacementRequest) { r.Group = "a/b" },
		"negative resource":       func(r *PlacementRequest) { r.Resources["cpu"] = -1 },
		"empty resource name":     func(r *PlacementRequest) { r.Resources[""] = 1 },
		"unknown operator": func(r *PlacementRequest) {
			r.Constraints = []Constraint{{Key: "k", Operator: "Near"}}
		},
		"In without values": func(r *PlacementRequest) {
			r.Constraints = []Constraint{{Key: "k", Operator: OperatorIn}}
		},
		"Exists with values": func(r *PlacementRequest) {
			r.Constraints = []Constraint{{Key: "k", Operator: OperatorExists, Values: []string{"v"}}}
		},
		"constraint without key": func(r *PlacementRequest) {
			r.Constraints = []Constraint{{Operator: OperatorExists}}
		},
		"preferred term weight 0": func(r *PlacementRequest) { r.Affinity = []AffinityTerm{{Group: "x"}} },
		"anti term without group": func(r *PlacementRequest) { r.AntiAffinity = []AffinityTerm{{Required: true}} },
		"spread without group": func(r *PlacementRequest) {
			r.Group = ""
			r.Spread = &SpreadConstraint{TopologyKey: "zone", MaxSkew: 1}
		},
		"spread without key":  func(r *PlacementRequest) { r.Spread = &SpreadConstraint{MaxSkew: 1} },
		"spread skew 0":       func(r *PlacementRequest) { r.Spread = &SpreadConstraint{TopologyKey: "zone"} },
		"unknown strategy":    func(r *PlacementRequest) { r.Strategy = "random" },
		"weight above bounds": func(r *PlacementRequest) { r.Affinity = []AffinityTerm{{Group: "x", Weight: 101}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := valid()
			mutate(r)
			err := ValidatePlacementRequest(r)
			if _, ok := err.(*InvalidRequestError); !ok {
				t.Fatalf("err = %v, want *InvalidRequestError", err)
			}
		})
	}
	if _, ok := ValidatePlacementRequest(nil).(*InvalidRequestError); !ok {
		t.Fatal("nil request accepted")
	}
}

func TestValidateNodeFacts(t *testing.T) {
	if err := ValidateNodeFacts(&NodeFacts{NodeID: "n", Capacity: map[string]int64{"cpu": 4}, Labels: map[string]string{"a": "b"}}); err != nil {
		t.Fatalf("valid facts refused: %v", err)
	}
	bad := []*NodeFacts{
		nil,
		{},
		{NodeID: "a/b"},
		{NodeID: "n", Capacity: map[string]int64{"cpu": -1}},
		{NodeID: "n", Labels: map[string]string{"": "v"}},
	}
	for i, f := range bad {
		if _, ok := ValidateNodeFacts(f).(*InvalidRequestError); !ok {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestErrorMessages(t *testing.T) {
	cases := map[string]error{
		"invalid request: x":                 &InvalidRequestError{Reason: "x"},
		"node not found: n":                  &NodeNotFoundError{NodeID: "n"},
		"no placement for workload: w":       &PlacementNotFoundError{WorkloadID: "w"},
		"release it before placing it again": &PlacementConflictError{WorkloadID: "w", NodeID: "n", Reason: "r"},
		"no live nodes are registered":       &NoFeasibleNodeError{WorkloadID: "w"},
		"a: x; b: y":                         &NoFeasibleNodeError{WorkloadID: "w", Rejections: map[string]string{"b": "y", "a": "x"}},
		"place abandoned after 16 contended": &ContendedError{Op: "place", Attempts: 16},
		"placement: scan: boom":              &StoreError{Op: "scan", Err: errBoom},
		"corrupt record k: boom":             &CorruptRecordError{Key: "k", Err: errBoom},
	}
	for want, err := range cases {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%T message %q does not contain %q", err, err.Error(), want)
		}
	}
}
