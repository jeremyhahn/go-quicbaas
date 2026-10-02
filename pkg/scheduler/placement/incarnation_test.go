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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
)

// A node that lost its lease and registers again under the same ID is a new
// incarnation: its earlier placements stay orphaned, their resources and
// group membership do not burden the new incarnation, and releasing them
// does not touch the new incarnation's allocation.
func TestReRegisteredNode_IsANewIncarnation(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)

	first, err := p.RegisterNode(ctx, "t", facts("n1", 4, nil), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	old := &PlacementRequest{WorkloadID: "w-old", Group: "web", Resources: map[string]int64{"cpu": 3}}
	if _, err := p.Place(ctx, "t", old); err != nil {
		t.Fatalf("Place: %v", err)
	}

	b.Expire(first.LeaseID)
	second, err := p.RegisterNode(ctx, "t", facts("n1", 4, nil), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	got, err := p.GetPlacement(ctx, "t", "w-old")
	if err != nil || !got.Orphaned {
		t.Fatalf("GetPlacement after re-registration = %+v, %v; want orphaned", got, err)
	}
	listed, err := p.ListPlacements(ctx, "t", nil)
	if err != nil || len(listed) != 1 || !listed[0].Orphaned {
		t.Fatalf("ListPlacements = %+v, %v; want the old placement orphaned", listed, err)
	}
	_, err = p.Place(ctx, "t", old)
	var conflict *PlacementConflictError
	if !errors.As(err, &conflict) || !strings.Contains(conflict.Reason, "re-registered") {
		t.Fatalf("Place of the orphaned workload = %v; want a re-registration conflict", err)
	}
	if second.Allocated["cpu"] != 0 || second.Placements != 0 {
		t.Fatalf("the new incarnation inherited the old allocation: %+v", second)
	}

	// The full capacity is free again, and the old group member does not
	// count: a required anti-affinity against "web" still admits n1.
	fresh := &PlacementRequest{
		WorkloadID: "w-new", Group: "api", Resources: map[string]int64{"cpu": 4},
		AntiAffinity: []AffinityTerm{{Group: "web", Required: true}},
	}
	res, err := p.Place(ctx, "t", fresh)
	if err != nil || res.Placement.NodeID != "n1" {
		t.Fatalf("Place on the new incarnation = %+v, %v", res, err)
	}

	if err := p.Release(ctx, "t", "w-old"); err != nil {
		t.Fatalf("Release of the orphan: %v", err)
	}
	info, err := p.GetNode(ctx, "t", "n1")
	if err != nil || info.Allocated["cpu"] != 4 || info.Placements != 1 {
		t.Fatalf("releasing the orphan changed the new incarnation's allocation: %+v, %v", info, err)
	}
	if got, err := p.GetPlacement(ctx, "t", "w-new"); err != nil || got.Orphaned {
		t.Fatalf("the new placement = %+v, %v; want live", got, err)
	}
	if err := p.Release(ctx, "t", "w-new"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if info, err := p.GetNode(ctx, "t", "n1"); err != nil || info.Placements != 0 || len(info.Allocated) != 0 {
		t.Fatalf("after releasing everything: %+v, %v", info, err)
	}
}

// Refreshing a live node continues its incarnation: its placements stay
// live and its allocation stays counted.
func TestLiveReRegistration_KeepsTheIncarnation(t *testing.T) {
	ctx := context.Background()
	p := New(placementtest.New())

	if _, err := p.RegisterNode(ctx, "t", facts("n1", 4, nil), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Place(ctx, "t", &PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"cpu": 3}}); err != nil {
		t.Fatal(err)
	}
	for refresh := 1; refresh <= 2; refresh++ {
		info, err := p.RegisterNode(ctx, "t", facts("n1", 4, nil), 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if info.Allocated["cpu"] != 3 || info.Placements != 1 {
			t.Fatalf("refresh %d lost the allocation: %+v", refresh, info)
		}
		got, err := p.GetPlacement(ctx, "t", "w")
		if err != nil || got.Orphaned {
			t.Fatalf("refresh %d orphaned the placement: %+v, %v", refresh, got, err)
		}
		res, err := p.Place(ctx, "t", &PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"cpu": 3}})
		if err != nil || !res.Existing {
			t.Fatalf("Place after refresh %d = %+v, %v; want the existing placement", refresh, res, err)
		}
	}
	// Capacity left is 1: the counted allocation still constrains.
	if _, err := p.Place(ctx, "t", &PlacementRequest{WorkloadID: "big", Resources: map[string]int64{"cpu": 2}}); err == nil {
		t.Fatal("a refreshed node forgot its allocation and overcommitted")
	}
}

func TestGroupRecord_LiveCountsSkipsForeignAndMalformedMembers(t *testing.T) {
	snap := &snapshotState{nodes: map[string]*liveNode{
		"n1": {record: nodeRecord{Incarnation: 7}},
	}}
	got := groupRecord{
		groupMember("n1", 7): 2,
		groupMember("n1", 3): 5, // an earlier incarnation
		groupMember("n2", 1): 1, // a node that is gone
		"malformed":          9,
	}.liveCounts(snap)
	if len(got) != 1 || got["n1"] != 2 {
		t.Fatalf("liveCounts = %v, want only n1's current incarnation", got)
	}
}
