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
	"errors"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// These tests run against a real go-qrdb node, under the Unit Test Contract's
// consensus-arbitration carve-out: the property under test is that the STORE
// decides concurrent placements, and an in-memory double that agrees with
// this package would prove nothing about that.

func TestMain(m *testing.M) {
	code := m.Run()
	qrdbnode.CloseShared()
	os.Exit(code)
}

// tenants hands each test a tenant namespace no other test uses.
var tenants atomic.Uint64

func realPlacer(t *testing.T) (*Placer, string) {
	t.Helper()
	n := qrdbnode.Shared(t)
	return NewFromClient(n.Node.Client()), "arb" + strconv.FormatUint(tenants.Add(1), 10)
}

// TestRealNode_ConcurrentPlacementsNeverOvercommit races many placements at
// a node with room for only some of them. The store must admit exactly as
// many as fit, and the recorded allocation must equal the admitted total.
func TestRealNode_ConcurrentPlacementsNeverOvercommit(t *testing.T) {
	p, tenant := realPlacer(t)
	ctx := context.Background()
	const capacity = 5
	const contenders = 12
	if _, err := p.RegisterNode(ctx, tenant, facts("only", capacity, nil), time.Minute); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}

	var wg sync.WaitGroup
	var placed, refused atomic.Int64
	errs := make(chan error, contenders)
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := p.Place(ctx, tenant, &PlacementRequest{
				WorkloadID: "w" + strconv.Itoa(i),
				Resources:  map[string]int64{"cpu": 1},
			})
			var none *NoFeasibleNodeError
			switch {
			case err == nil:
				placed.Add(1)
			case errors.As(err, &none):
				refused.Add(1)
			default:
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("unexpected Place error: %v", err)
	}

	if placed.Load() != capacity || refused.Load() != contenders-capacity {
		t.Fatalf("placed %d, refused %d; want %d and %d", placed.Load(), refused.Load(), capacity, contenders-capacity)
	}
	info, err := p.GetNode(ctx, tenant, "only")
	if err != nil {
		t.Fatal(err)
	}
	if info.Allocated["cpu"] != capacity || info.Placements != capacity {
		t.Fatalf("recorded allocation %+v, want %d cpu across %d placements", info, capacity, capacity)
	}
}

// TestRealNode_ConcurrentAntiAffinityHoldsAcrossNodes places a group with
// required self-anti-affinity from many goroutines at once. Each placement
// lands on a different node, which is only true if the group record — not
// the per-node allocation alone — serialises them.
func TestRealNode_ConcurrentAntiAffinityHoldsAcrossNodes(t *testing.T) {
	p, tenant := realPlacer(t)
	ctx := context.Background()
	const nodes = 4
	for i := 0; i < nodes; i++ {
		if _, err := p.RegisterNode(ctx, tenant, facts("n"+strconv.Itoa(i), 100, nil), time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < nodes+3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := p.Place(ctx, tenant, &PlacementRequest{
				WorkloadID:   "replica" + strconv.Itoa(i),
				Group:        "db",
				AntiAffinity: []AffinityTerm{{Group: "db", Required: true}},
			})
			var none *NoFeasibleNodeError
			if err != nil && !errors.As(err, &none) {
				t.Errorf("Place: %v", err)
			}
		}(i)
	}
	wg.Wait()

	placements, err := p.ListPlacements(ctx, tenant, &PlacementFilter{Group: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != nodes {
		t.Fatalf("%d replicas placed, want %d", len(placements), nodes)
	}
	seen := map[string]bool{}
	for _, pl := range placements {
		if seen[pl.NodeID] {
			t.Fatalf("two replicas on %s", pl.NodeID)
		}
		seen[pl.NodeID] = true
	}
}

// TestRealNode_LeaseRevocationRemovesTheNode proves liveness rides the
// lease: deregistering revokes it, the facts vanish, and the placement on
// the node is reported orphaned.
func TestRealNode_LeaseRevocationRemovesTheNode(t *testing.T) {
	p, tenant := realPlacer(t)
	ctx := context.Background()
	if _, err := p.RegisterNode(ctx, tenant, facts("gone", 2, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Place(ctx, tenant, &PlacementRequest{WorkloadID: "w"}); err != nil {
		t.Fatal(err)
	}
	if err := p.DeregisterNode(ctx, tenant, "gone"); err != nil {
		t.Fatalf("DeregisterNode: %v", err)
	}
	nodes, err := p.ListNodes(ctx, tenant)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("nodes after revoke = %+v, %v", nodes, err)
	}
	pl, err := p.GetPlacement(ctx, tenant, "w")
	if err != nil || !pl.Orphaned {
		t.Fatalf("placement = %+v, %v; want orphaned", pl, err)
	}
}
