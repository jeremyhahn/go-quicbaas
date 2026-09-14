// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package scheduler

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// mustCluster builds a clustered engine for the dispatch-gate tests, which
// exercise shouldExecute and never touch storage. NewWithClustering refuses a
// store that cannot claim, so these tests supply one that can.
func mustCluster(t *testing.T, config *SchedulerConfig, nodeID string, leader LeaderChecker) *SchedulerEngine {
	t.Helper()
	s, err := NewWithClustering(config, newMockJobStorage(), nodeID, leader)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	return s
}

// fakeLeader is a programmable LeaderChecker for unit tests.
type fakeLeader struct {
	isLeader bool
	err      error
	calls    int
}

func (f *fakeLeader) IsLeader() (bool, error) {
	f.calls++
	return f.isLeader, f.err
}

// --- NewWithClustering / New ---

func TestNew_DefaultsToSingleNode(t *testing.T) {
	s := New(nil, nil)
	if s.nodeID != "" {
		t.Errorf("nodeID = %q, want empty", s.nodeID)
	}
	if s.leader != nil {
		t.Error("leader should be nil for single-node New")
	}
}

func TestNewWithClustering_StoresFields(t *testing.T) {
	leader := &fakeLeader{}
	s := mustCluster(t, nil, "node-A", leader)
	if s.nodeID != "node-A" {
		t.Errorf("nodeID = %q, want node-A", s.nodeID)
	}
	if s.leader != leader {
		t.Error("leader pointer not stored")
	}
}

// --- shouldExecute matrix ---

func TestShouldExecute_SingleNodeMode_AlwaysTrue(t *testing.T) {
	s := New(nil, nil) // nodeID="" + leader=nil
	cases := []*Job{
		{ID: 1, NodeAffinity: ""},          // cluster-wide
		{ID: 2, NodeAffinity: "anything"},  // affine to a different node
		{ID: 3, NodeAffinity: "node-self"}, // affine to a name
	}
	for _, j := range cases {
		if !s.shouldExecute(j) {
			t.Errorf("single-node mode: job %d (affinity=%q) should execute", j.ID, j.NodeAffinity)
		}
	}
}

func TestShouldExecute_NodeIDOnly_AffineMatch(t *testing.T) {
	s := mustCluster(t, nil, "node-A", nil)
	if !s.shouldExecute(&Job{NodeAffinity: "node-A"}) {
		t.Error("matching affinity should execute when leader is nil")
	}
}

func TestShouldExecute_NodeIDOnly_AffineMismatch(t *testing.T) {
	s := mustCluster(t, nil, "node-A", nil)
	if s.shouldExecute(&Job{NodeAffinity: "node-B"}) {
		t.Error("mismatched affinity should not execute")
	}
}

func TestShouldExecute_NodeIDOnly_ClusterWideAlwaysFires(t *testing.T) {
	// Without a LeaderChecker, cluster-wide jobs always fire even
	// when nodeID is set. This preserves "no leader gate => single
	// node owns all cluster-wide work" semantics.
	s := mustCluster(t, nil, "node-A", nil)
	if !s.shouldExecute(&Job{NodeAffinity: ""}) {
		t.Error("cluster-wide job with nil leader should execute")
	}
}

func TestShouldExecute_LeaderOnly_ClusterWideOnLeader(t *testing.T) {
	leader := &fakeLeader{isLeader: true}
	s := mustCluster(t, nil, "", leader)
	if !s.shouldExecute(&Job{NodeAffinity: ""}) {
		t.Error("cluster-wide job on leader should execute")
	}
	if leader.calls != 1 {
		t.Errorf("leader.calls = %d, want 1", leader.calls)
	}
}

func TestShouldExecute_LeaderOnly_ClusterWideSkipsFollower(t *testing.T) {
	leader := &fakeLeader{isLeader: false}
	s := mustCluster(t, nil, "", leader)
	if s.shouldExecute(&Job{NodeAffinity: ""}) {
		t.Error("cluster-wide job on follower should not execute")
	}
}

func TestShouldExecute_LeaderOnly_AffineIgnoresLeadership(t *testing.T) {
	// Without a nodeID, affinity matching is disabled and the gate
	// degrades to leader-only. So an affine job on a non-leader node
	// with no nodeID is treated like a cluster-wide job that needs
	// leadership.
	leader := &fakeLeader{isLeader: false}
	s := mustCluster(t, nil, "", leader)
	if s.shouldExecute(&Job{NodeAffinity: "node-A"}) {
		t.Error("affine job without nodeID should fall through to leader gate")
	}
}

func TestShouldExecute_FullClustering_AffineMatchOnFollower(t *testing.T) {
	// Affine job on its owner node — even if the node is a follower,
	// it must execute because only this node can do the work.
	leader := &fakeLeader{isLeader: false}
	s := mustCluster(t, nil, "node-A", leader)
	if !s.shouldExecute(&Job{NodeAffinity: "node-A"}) {
		t.Error("affine job on owner (follower) must execute")
	}
	// Leader checker should NOT be called — affinity short-circuits.
	if leader.calls != 0 {
		t.Errorf("leader.calls = %d, want 0 (affine bypass)", leader.calls)
	}
}

func TestShouldExecute_FullClustering_AffineMismatchOnLeader(t *testing.T) {
	// Affine job for node-B, currently running on node-A which is
	// the leader. node-A skips because it's not the owner.
	leader := &fakeLeader{isLeader: true}
	s := mustCluster(t, nil, "node-A", leader)
	if s.shouldExecute(&Job{NodeAffinity: "node-B"}) {
		t.Error("non-owner leader should not execute affine job")
	}
}

func TestShouldExecute_FullClustering_ClusterWideOnLeader(t *testing.T) {
	leader := &fakeLeader{isLeader: true}
	s := mustCluster(t, nil, "node-A", leader)
	if !s.shouldExecute(&Job{NodeAffinity: ""}) {
		t.Error("cluster-wide job on leader should execute")
	}
}

func TestShouldExecute_FullClustering_ClusterWideOnFollower(t *testing.T) {
	leader := &fakeLeader{isLeader: false}
	s := mustCluster(t, nil, "node-A", leader)
	if s.shouldExecute(&Job{NodeAffinity: ""}) {
		t.Error("cluster-wide job on follower should not execute")
	}
}

func TestShouldExecute_LeaderCheckErrorTreatedAsFollower(t *testing.T) {
	leader := &fakeLeader{err: errors.New("raft transport flap")}
	s := mustCluster(t, nil, "node-A", leader)
	if s.shouldExecute(&Job{NodeAffinity: ""}) {
		t.Error("leader-check error should suppress execution")
	}
}

// --- Job.NodeAffinity persists through JSON round-trip ---

func TestJob_NodeAffinity_JSONRoundTrip(t *testing.T) {
	j := &Job{ID: 7, Name: "x", NodeAffinity: "node-A"}
	b, err := jsonMarshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var got Job
	if err := jsonUnmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.NodeAffinity != "node-A" {
		t.Errorf("NodeAffinity = %q, want node-A", got.NodeAffinity)
	}
}

func TestJob_NodeAffinity_OmittedWhenEmpty(t *testing.T) {
	j := &Job{ID: 7, Name: "x"} // no affinity
	b, err := jsonMarshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if containsBytes(b, "node_affinity") {
		t.Errorf("empty NodeAffinity should be omitted; got %s", string(b))
	}
}

// --- helpers ---

func jsonMarshal(v any) ([]byte, error)     { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error   { return json.Unmarshal(b, v) }
func containsBytes(h []byte, n string) bool { return bytes.Contains(h, []byte(n)) }
