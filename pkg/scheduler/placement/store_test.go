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

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
)

var errBoom = errors.New("boom")

func facts(id string, cpu int64, labels map[string]string) *NodeFacts {
	return &NodeFacts{NodeID: id, Capacity: map[string]int64{"cpu": cpu}, Labels: labels}
}

func TestRegisterNode_ReturnsInfoAndReusesALiveLease(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)

	info, err := p.RegisterNode(ctx, "t1", facts("n1", 8, map[string]string{"arch": "arm64"}), 30*time.Second)
	if err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	if info.LeaseID == 0 || info.TTLSeconds != 30 || info.Facts.Labels["arch"] != "arm64" {
		t.Fatalf("info = %+v", info)
	}
	again, err := p.RegisterNode(ctx, "t1", facts("n1", 16, nil), 30*time.Second)
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if again.LeaseID != info.LeaseID {
		t.Fatalf("re-register with the same TTL replaced the lease (%d -> %d)", info.LeaseID, again.LeaseID)
	}
	if again.Facts.Capacity["cpu"] != 16 {
		t.Fatalf("facts not refreshed: %+v", again.Facts)
	}

	// A live node cannot change its TTL: go-qrdb cannot move the record to
	// a new lease without the old one's expiry deleting it.
	_, err = p.RegisterNode(ctx, "t1", facts("n1", 16, nil), 60*time.Second)
	var invalid *InvalidRequestError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Reason, "ttl_seconds 30") {
		t.Fatalf("TTL change on a live node = %v, want an InvalidRequestError", err)
	}
	if !b.HasLease(info.LeaseID) {
		t.Fatal("the refused TTL change touched the live lease")
	}
}

func TestRegisterNode_TTLBelowOneSecondIsClamped(t *testing.T) {
	p := New(placementtest.New())
	info, err := p.RegisterNode(context.Background(), "", facts("n", 1, nil), time.Millisecond)
	if err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	if info.TTLSeconds != 1 {
		t.Fatalf("TTL = %d, want 1", info.TTLSeconds)
	}
}

func TestRegisterNode_Errors(t *testing.T) {
	ctx := context.Background()
	if _, err := New(placementtest.New()).RegisterNode(ctx, "", &NodeFacts{}, time.Second); err == nil {
		t.Fatal("invalid facts accepted")
	}
	for _, op := range []string{"get", "grant", "putlease"} {
		b := placementtest.New()
		b.FailOp = op
		_, err := New(b).RegisterNode(ctx, "", facts("n", 1, nil), time.Second)
		var storeErr *StoreError
		if !errors.As(err, &storeErr) {
			t.Errorf("%s failure: err = %v, want *StoreError", op, err)
		}
	}
}

func TestRegisterNode_ExpiredLeaseIsReplaced(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	info, err := p.RegisterNode(ctx, "", facts("n", 1, nil), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// The lease expires after RegisterNode read the record: the record is
	// still there, the lease is not.
	raw, err := b.Get(ctx, key("", nodesDir, "n"))
	if err != nil || raw == nil {
		t.Fatalf("read node record: %v", err)
	}
	b.Expire(info.LeaseID)
	b.Set(key("", nodesDir, "n"), raw)
	again, err := p.RegisterNode(ctx, "", facts("n", 1, nil), time.Second)
	if err != nil {
		t.Fatalf("re-register after expiry: %v", err)
	}
	if again.LeaseID == info.LeaseID {
		t.Fatal("expired lease reused")
	}
}

// A keep-alive the store could not answer says nothing about the lease, so
// a refresh must not start a new incarnation on it: that would orphan
// workloads that are still running.
func TestRegisterNode_KeepAliveStoreFailureFailsClosed(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	info, err := p.RegisterNode(ctx, "", facts("n", 1, nil), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b.FailOp = "keepalive"
	_, err = p.RegisterNode(ctx, "", facts("n", 1, nil), time.Second)
	var storeErr *StoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("refresh during a store failure = %v, want a StoreError", err)
	}
	if err := p.RenewNode(ctx, "", "n"); !errors.As(err, &storeErr) {
		t.Fatalf("renew during a store failure = %v, want a StoreError", err)
	}
	b.FailOp = ""
	got, err := p.GetNode(ctx, "", "n")
	if err != nil || got.LeaseID != info.LeaseID {
		t.Fatalf("the failed refresh replaced the lease: %+v, %v", got, err)
	}
}

func TestRenewNode_GoneLeaseIsNodeNotFound(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	info, err := p.RegisterNode(ctx, "", facts("n", 1, nil), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := b.Get(ctx, key("", nodesDir, "n"))
	if err != nil {
		t.Fatal(err)
	}
	b.Expire(info.LeaseID)
	b.Set(key("", nodesDir, "n"), raw)
	var notFound *NodeNotFoundError
	if err := p.RenewNode(ctx, "", "n"); !errors.As(err, &notFound) {
		t.Fatalf("renew of a gone lease = %v, want NodeNotFoundError", err)
	}
}

func TestNodeLifecycle_RenewCordonDrainUncordonDeregister(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	if _, err := p.RegisterNode(ctx, "t", facts("n1", 8, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := p.RenewNode(ctx, "t", "n1"); err != nil {
		t.Fatalf("RenewNode: %v", err)
	}

	info, err := p.CordonNode(ctx, "t", "n1")
	if err != nil || !info.Status.Unschedulable || info.Status.Draining {
		t.Fatalf("CordonNode = %+v, %v", info, err)
	}
	// A re-registration must not clear an operator's cordon.
	if info, err = p.RegisterNode(ctx, "t", facts("n1", 8, nil), time.Minute); err != nil || !info.Status.Unschedulable {
		t.Fatalf("re-register cleared the cordon: %+v, %v", info, err)
	}

	if _, err := p.UncordonNode(ctx, "t", "n1"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Place(ctx, "t", &PlacementRequest{WorkloadID: "w1", Resources: map[string]int64{"cpu": 1}}); err != nil {
		t.Fatalf("Place: %v", err)
	}
	moved, err := p.DrainNode(ctx, "t", "n1")
	if err != nil {
		t.Fatalf("DrainNode: %v", err)
	}
	if len(moved) != 1 || moved[0].WorkloadID != "w1" {
		t.Fatalf("DrainNode returned %+v, want w1", moved)
	}
	info, err = p.GetNode(ctx, "t", "n1")
	if err != nil || !info.Status.Draining || !info.Status.Unschedulable {
		t.Fatalf("drained node = %+v, %v", info, err)
	}
	if info, err = p.UncordonNode(ctx, "t", "n1"); err != nil || info.Status != (NodeStatus{}) {
		t.Fatalf("uncordon left %+v, %v", info, err)
	}

	if err := p.DeregisterNode(ctx, "t", "n1"); err != nil {
		t.Fatalf("DeregisterNode: %v", err)
	}
	var notFound *NodeNotFoundError
	if _, err := p.GetNode(ctx, "t", "n1"); !errors.As(err, &notFound) {
		t.Fatalf("GetNode after deregister = %v", err)
	}
	got, err := p.GetPlacement(ctx, "t", "w1")
	if err != nil || !got.Orphaned {
		t.Fatalf("placement on deregistered node = %+v, %v; want orphaned", got, err)
	}
}

func TestNodeOperations_UnknownNode(t *testing.T) {
	ctx := context.Background()
	p := New(placementtest.New())
	var notFound *NodeNotFoundError
	checks := map[string]error{
		"renew":      p.RenewNode(ctx, "", "x"),
		"deregister": p.DeregisterNode(ctx, "", "x"),
	}
	_, checks["get"] = p.GetNode(ctx, "", "x")
	_, checks["cordon"] = p.CordonNode(ctx, "", "x")
	_, checks["uncordon"] = p.UncordonNode(ctx, "", "x")
	_, checks["drain"] = p.DrainNode(ctx, "", "x")
	for name, err := range checks {
		if !errors.As(err, &notFound) {
			t.Errorf("%s: err = %v, want *NodeNotFoundError", name, err)
		}
	}
	var invalid *InvalidRequestError
	if err := p.RenewNode(ctx, "", ""); !errors.As(err, &invalid) {
		t.Errorf("empty node id accepted: %v", err)
	}
}

func TestNodeOperations_StoreFailures(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	if _, err := p.RegisterNode(ctx, "", facts("n", 4, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	var storeErr *StoreError

	b.FailOp = "keepalive"
	if err := p.RenewNode(ctx, "", "n"); !errors.As(err, &storeErr) {
		t.Errorf("renew: %v", err)
	}
	b.FailOp = "put"
	if _, err := p.CordonNode(ctx, "", "n"); !errors.As(err, &storeErr) {
		t.Errorf("cordon: %v", err)
	}
	b.FailOp = "scan"
	if _, err := p.ListNodes(ctx, ""); !errors.As(err, &storeErr) {
		t.Errorf("list nodes: %v", err)
	}
	if _, err := p.ListPlacements(ctx, "", nil); !errors.As(err, &storeErr) {
		t.Errorf("list placements: %v", err)
	}
	b.FailOp = "delete"
	if err := p.DeregisterNode(ctx, "", "n"); !errors.As(err, &storeErr) {
		t.Errorf("deregister: %v", err)
	}
}

func TestDeregisterNode_RevokeFailureOnALiveLeaseIsReported(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	if _, err := p.RegisterNode(ctx, "", facts("n", 4, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	b.FailOp = "revoke"
	var storeErr *StoreError
	if err := p.DeregisterNode(ctx, "", "n"); !errors.As(err, &storeErr) {
		t.Fatalf("err = %v, want *StoreError", err)
	}
}

func TestListNodes_SortedAndTenantIsolated(t *testing.T) {
	ctx := context.Background()
	p := New(placementtest.New())
	for _, id := range []string{"c", "a", "b"} {
		if _, err := p.RegisterNode(ctx, "t1", facts(id, 1, nil), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.RegisterNode(ctx, "t2", facts("other", 1, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	nodes, err := p.ListNodes(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range nodes {
		ids = append(ids, n.Facts.NodeID)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("ListNodes(t1) = %v", ids)
	}
}

func TestPlace_RecordsAllocationAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	p := New(placementtest.New())
	if _, err := p.RegisterNode(ctx, "", facts("n1", 4, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	req := &PlacementRequest{WorkloadID: "w1", Group: "web", Resources: map[string]int64{"cpu": 3}}
	res, err := p.Place(ctx, "", req)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if res.Existing || res.Placement.NodeID != "n1" {
		t.Fatalf("result = %+v", res)
	}
	again, err := p.Place(ctx, "", req)
	if err != nil || !again.Existing || again.Placement.NodeID != "n1" {
		t.Fatalf("idempotent Place = %+v, %v", again, err)
	}
	info, err := p.GetNode(ctx, "", "n1")
	if err != nil || info.Allocated["cpu"] != 3 || info.Placements != 1 {
		t.Fatalf("allocation not recorded once: %+v, %v", info, err)
	}

	var none *NoFeasibleNodeError
	_, err = p.Place(ctx, "", &PlacementRequest{WorkloadID: "w2", Resources: map[string]int64{"cpu": 2}})
	if !errors.As(err, &none) || !strings.Contains(none.Rejections["n1"], "insufficient cpu") {
		t.Fatalf("overcommit not refused: %v", err)
	}
}

func TestPlace_NoNodesAndInvalidRequest(t *testing.T) {
	ctx := context.Background()
	p := New(placementtest.New())
	var none *NoFeasibleNodeError
	if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: "w"}); !errors.As(err, &none) {
		t.Fatalf("err = %v, want *NoFeasibleNodeError", err)
	}
	var invalid *InvalidRequestError
	if _, err := p.Place(ctx, "", &PlacementRequest{}); !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *InvalidRequestError", err)
	}
}

func TestPlace_ExistingPlacementOnADeadOrDrainingNodeConflicts(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	info, err := p.RegisterNode(ctx, "", facts("n1", 4, nil), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := &PlacementRequest{WorkloadID: "w1", Resources: map[string]int64{"cpu": 1}}
	if _, err := p.Place(ctx, "", req); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DrainNode(ctx, "", "n1"); err != nil {
		t.Fatal(err)
	}
	var conflict *PlacementConflictError
	if _, err := p.Place(ctx, "", req); !errors.As(err, &conflict) || conflict.Reason != "the node is draining" {
		t.Fatalf("draining: err = %v", err)
	}
	b.Expire(info.LeaseID)
	if _, err := p.Place(ctx, "", req); !errors.As(err, &conflict) || conflict.Reason != "the node's lease has expired" {
		t.Fatalf("expired: err = %v", err)
	}
	if err := p.Release(ctx, "", "w1"); err != nil {
		t.Fatalf("Release orphan: %v", err)
	}
}

func TestPlace_ReEvaluatesAfterLosingTheGuardedWrite(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	for _, id := range []string{"n1", "n2"} {
		if _, err := p.RegisterNode(ctx, "", facts(id, 4, nil), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	// Bin-pack prefers n1 (first by ID). Just before the commit, a rival
	// fills it, so the guard fails and the retry must land on n2.
	b.CommitHook = func() {
		if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: "rival", Resources: map[string]int64{"cpu": 4}}); err != nil {
			t.Errorf("rival Place: %v", err)
		}
	}
	res, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"cpu": 2}})
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if res.Placement.NodeID != "n2" {
		t.Fatalf("placed on %s after losing n1, want n2", res.Placement.NodeID)
	}
}

// contendingBackend fails every guarded write, so Place and Release must
// give up with ContendedError rather than loop.
type contendingBackend struct{ *placementtest.Backend }

func (c contendingBackend) Commit(context.Context, *transport.TransactionRequest) (bool, error) {
	return false, nil
}

func TestPlaceAndRelease_GiveUpWhenAlwaysContended(t *testing.T) {
	ctx := context.Background()
	mem := placementtest.New()
	p := New(mem)
	if _, err := p.RegisterNode(ctx, "", facts("n", 4, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: "w"}); err != nil {
		t.Fatal(err)
	}
	contended := New(contendingBackend{mem})
	var gaveUp *ContendedError
	if _, err := contended.Place(ctx, "", &PlacementRequest{WorkloadID: "w2"}); !errors.As(err, &gaveUp) {
		t.Fatalf("Place: err = %v", err)
	}
	if err := contended.Release(ctx, "", "w"); !errors.As(err, &gaveUp) {
		t.Fatalf("Release: err = %v", err)
	}
}

func TestRelease_ReturnsResourcesAndGroupMembership(t *testing.T) {
	ctx := context.Background()
	p := New(placementtest.New())
	if _, err := p.RegisterNode(ctx, "", facts("n1", 4, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"w1", "w2"} {
		if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: w, Group: "web", Resources: map[string]int64{"cpu": 2}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Release(ctx, "", "w1"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	info, err := p.GetNode(ctx, "", "n1")
	if err != nil || info.Allocated["cpu"] != 2 || info.Placements != 1 {
		t.Fatalf("after one release: %+v, %v", info, err)
	}
	if err := p.Release(ctx, "", "w2"); err != nil {
		t.Fatal(err)
	}
	info, err = p.GetNode(ctx, "", "n1")
	if err != nil || len(info.Allocated) != 0 || info.Placements != 0 {
		t.Fatalf("after both releases: %+v, %v", info, err)
	}
	// Group membership is gone too: a required affinity to it now fails.
	var none *NoFeasibleNodeError
	_, err = p.Place(ctx, "", &PlacementRequest{WorkloadID: "w3", Affinity: []AffinityTerm{{Group: "web", Required: true}}})
	if !errors.As(err, &none) {
		t.Fatalf("group survived its last release: %v", err)
	}
	var notFound *PlacementNotFoundError
	if err := p.Release(ctx, "", "w1"); !errors.As(err, &notFound) {
		t.Fatalf("double release: %v", err)
	}
}

func TestRelease_StoreFailures(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	if _, err := p.RegisterNode(ctx, "", facts("n", 4, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: "w", Group: "g"}); err != nil {
		t.Fatal(err)
	}
	var storeErr *StoreError
	b.FailOp = "commit"
	if err := p.Release(ctx, "", "w"); !errors.As(err, &storeErr) {
		t.Fatalf("commit failure: %v", err)
	}
	if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: "w9"}); !errors.As(err, &storeErr) {
		t.Fatalf("place commit failure: %v", err)
	}
	b.FailOp = "get"
	if err := p.Release(ctx, "", "w"); !errors.As(err, &storeErr) {
		t.Fatalf("get failure: %v", err)
	}
	if _, err := p.GetPlacement(ctx, "", "w"); !errors.As(err, &storeErr) {
		t.Fatalf("get placement failure: %v", err)
	}
	var invalid *InvalidRequestError
	if err := p.Release(ctx, "", ""); !errors.As(err, &invalid) {
		t.Fatalf("empty workload: %v", err)
	}
}

func TestCorruptRecordsFailClosed(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	if _, err := p.RegisterNode(ctx, "", facts("n", 4, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	b.Set(keyPrefix+allocDir+"n", []byte("{not json"))
	var corrupt *CorruptRecordError
	if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: "w"}); !errors.As(err, &corrupt) {
		t.Fatalf("corrupt allocation: err = %v", err)
	}
	if _, err := p.GetNode(ctx, "", "n"); !errors.As(err, &corrupt) {
		t.Fatalf("GetNode: err = %v", err)
	}
	b.Set(keyPrefix+workloadsDir+"bad", []byte("x"))
	if _, err := p.ListPlacements(ctx, "", nil); !errors.As(err, &corrupt) {
		t.Fatalf("ListPlacements: err = %v", err)
	}
}

func TestListPlacements_FiltersAndOrphans(t *testing.T) {
	ctx := context.Background()
	b := placementtest.New()
	p := New(b)
	a, err := p.RegisterNode(ctx, "", facts("a", 10, nil), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.RegisterNode(ctx, "", facts("b", 10, nil), time.Minute); err != nil {
		t.Fatal(err)
	}
	place := func(w, g string, c Constraint) {
		t.Helper()
		if _, err := p.Place(ctx, "", &PlacementRequest{WorkloadID: w, Group: g, Constraints: []Constraint{c}}); err != nil {
			t.Fatal(err)
		}
	}
	onA := Constraint{Key: "x", Operator: OperatorDoesNotExist}
	place("w1", "web", onA)
	if _, err := p.CordonNode(ctx, "", "a"); err != nil {
		t.Fatal(err)
	}
	place("w2", "db", onA)
	place("w3", "web", onA)

	all, err := p.ListPlacements(ctx, "", nil)
	if err != nil || len(all) != 3 || all[0].WorkloadID != "w1" {
		t.Fatalf("all = %+v, %v", all, err)
	}
	web, err := p.ListPlacements(ctx, "", &PlacementFilter{Group: "web"})
	if err != nil || len(web) != 2 {
		t.Fatalf("web = %+v, %v", web, err)
	}
	onB, err := p.ListPlacements(ctx, "", &PlacementFilter{NodeID: "b"})
	if err != nil || len(onB) != 2 {
		t.Fatalf("on b = %+v, %v", onB, err)
	}
	b.Expire(a.LeaseID)
	all, err = p.ListPlacements(ctx, "", &PlacementFilter{NodeID: "a"})
	if err != nil || len(all) != 1 || !all[0].Orphaned {
		t.Fatalf("orphan = %+v, %v", all, err)
	}
	var notFound *PlacementNotFoundError
	if _, err := p.GetPlacement(ctx, "", "nope"); !errors.As(err, &notFound) {
		t.Fatalf("GetPlacement missing = %v", err)
	}
}
