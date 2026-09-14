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

package service

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// =============================================================================
// Leader checker
// =============================================================================

// fakeMembership stands in for go-qrdb's cluster view.
type fakeMembership struct {
	members []qrdbsdk.Member
	err     error
	calls   int
}

func (f *fakeMembership) Members(context.Context, uint64) ([]qrdbsdk.Member, error) {
	f.calls++
	return f.members, f.err
}

func checkerOver(m *fakeMembership, replicaID uint64) *qrdbLeaderChecker {
	return &qrdbLeaderChecker{cluster: m, shardID: 1, replicaID: replicaID, timeout: leaderQueryTimeout}
}

// TestQRDBLeaderChecker_AnswersForThisReplica proves the checker reports on
// ITSELF, not on whichever replica is listed first. Reading another replica's
// flag would make every follower believe it leads.
func TestQRDBLeaderChecker_AnswersForThisReplica(t *testing.T) {
	membership := &fakeMembership{members: []qrdbsdk.Member{
		{ReplicaID: 1, IsLeader: true},
		{ReplicaID: 2, IsLeader: false},
	}}

	leader, err := checkerOver(membership, 2).IsLeader()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if leader {
		t.Fatal("replica 2 is a follower; the checker must answer for its own replica ID")
	}

	leader, err = checkerOver(membership, 1).IsLeader()
	if err != nil || !leader {
		t.Fatalf("replica 1 leads: got leader=%v err=%v", leader, err)
	}
}

// TestQRDBLeaderChecker_QueriesEveryTime pins the no-cache property.
//
// Leadership is the fact that moves without announcing itself. A cached answer
// is precisely how a deposed leader goes on believing it leads, which is the
// double-fire this whole work package exists to close.
func TestQRDBLeaderChecker_QueriesEveryTime(t *testing.T) {
	membership := &fakeMembership{members: []qrdbsdk.Member{{ReplicaID: 1, IsLeader: true}}}
	checker := checkerOver(membership, 1)

	if leader, _ := checker.IsLeader(); !leader {
		t.Fatal("expected leadership on the first call")
	}

	// Leadership moves away.
	membership.members = []qrdbsdk.Member{{ReplicaID: 1, IsLeader: false}, {ReplicaID: 2, IsLeader: true}}

	if leader, _ := checker.IsLeader(); leader {
		t.Fatal("the checker must observe leadership moving away; a cached answer " +
			"would keep a deposed leader firing cluster-wide jobs")
	}
	if membership.calls != 2 {
		t.Errorf("membership queried %d times, want 2 — one live query per call", membership.calls)
	}
}

// TestQRDBLeaderChecker_UnreadableMembershipIsNotLeadership covers the
// fail-closed direction: an unanswerable query must not read as leadership.
func TestQRDBLeaderChecker_UnreadableMembershipIsNotLeadership(t *testing.T) {
	membership := &fakeMembership{err: errors.New("cluster unreachable")}

	leader, err := checkerOver(membership, 1).IsLeader()
	if leader {
		t.Fatal("an unanswerable leadership query must never read as leadership")
	}
	var unknown *LeadershipUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected LeadershipUnknownError, got %T: %v", err, err)
	}
}

// TestQRDBLeaderChecker_AbsentReplicaIsReported covers a replica missing from
// its own shard's membership — a misconfigured replica ID or a mid-removal
// node. Answering a silent "not leader" would let it idle while looking
// healthy.
func TestQRDBLeaderChecker_AbsentReplicaIsReported(t *testing.T) {
	membership := &fakeMembership{members: []qrdbsdk.Member{{ReplicaID: 1, IsLeader: true}}}

	leader, err := checkerOver(membership, 99).IsLeader()
	if leader {
		t.Fatal("a replica absent from its shard must not report leadership")
	}
	var absent *ReplicaNotInShardError
	if !errors.As(err, &absent) {
		t.Fatalf("expected ReplicaNotInShardError, got %T: %v", err, err)
	}
}

// =============================================================================
// Fail-closed construction
// =============================================================================

// stubTransactor satisfies the claim arbiter interface for construction tests.
type stubTransactor struct{}

func (stubTransactor) ExecuteTransaction(
	context.Context, *transport.TransactionRequest, ...transport.WriteOption,
) (*transport.TransactionResponse, error) {
	return &transport.TransactionResponse{Succeeded: true}, nil
}

type stubLeader struct{}

func (stubLeader) IsLeader() (bool, error) { return true, nil }

// TestBuildSchedulerEngine_StandaloneIsUnchanged proves a standalone boot still
// gets the plain scheduler with no clustering wiring at all.
func TestBuildSchedulerEngine_StandaloneIsUnchanged(t *testing.T) {
	engine, err := buildSchedulerEngine(scheduler.DefaultConfig(), kvstore.NewMemory(), "standalone", nil)
	if err != nil {
		t.Fatalf("a standalone boot must build a scheduler: %v", err)
	}
	if engine == nil {
		t.Fatal("expected an engine")
	}
}

// TestBuildSchedulerEngine_IncompleteClusteringRefuses is the fail-closed rule.
//
// Each partial combination is useless in its own way — no leader checker means
// every node fires every cluster-wide job, no arbiter means two nodes fire it
// during churn — so a clustered boot missing any part must stop the daemon
// rather than present a scheduler that looks correct.
func TestBuildSchedulerEngine_IncompleteClusteringRefuses(t *testing.T) {
	cases := []struct {
		name       string
		clustering *SchedulerClustering
	}{
		{"no leader checker", &SchedulerClustering{Claims: stubTransactor{}, Marks: stubReader{}}},
		{"no claim arbiter", &SchedulerClustering{Leader: stubLeader{}, Marks: stubReader{}}},
		{"no mark reader", &SchedulerClustering{Leader: stubLeader{}, Claims: stubTransactor{}}},
		{"nothing but a node id", &SchedulerClustering{NodeID: "node-A"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, err := buildSchedulerEngine(scheduler.DefaultConfig(), kvstore.NewMemory(), "cluster", tc.clustering)
			if err == nil {
				t.Fatal("a clustered boot missing part of its wiring must refuse to build")
			}
			if engine != nil {
				t.Error("no engine may be returned alongside the refusal")
			}
			var incomplete *IncompleteSchedulerClusteringError
			if !errors.As(err, &incomplete) {
				t.Fatalf("expected IncompleteSchedulerClusteringError, got %T: %v", err, err)
			}
		})
	}
}

// TestBuildSchedulerEngine_CompleteClusteringBuilds proves the whole set
// produces a clustered engine.
func TestBuildSchedulerEngine_CompleteClusteringBuilds(t *testing.T) {
	engine, err := buildSchedulerEngine(scheduler.DefaultConfig(), kvstore.NewMemory(), "cluster", &SchedulerClustering{
		NodeID: "node-A",
		Leader: stubLeader{},
		Claims: stubTransactor{},
		Marks:  stubReader{},
	})
	if err != nil {
		t.Fatalf("complete clustering must build: %v", err)
	}
	if engine == nil {
		t.Fatal("expected a clustered engine")
	}
}

// stubReader satisfies the mark-reader interface for construction tests.
type stubReader struct{}

func (stubReader) Put(context.Context, string, []byte, ...transport.WriteOption) error {
	return nil
}
func (stubReader) Get(context.Context, string, ...transport.ReadOption) ([]byte, error) {
	return nil, nil
}
func (stubReader) Delete(context.Context, string, ...transport.WriteOption) error { return nil }
func (stubReader) List(context.Context, string, ...transport.ReadOption) ([]string, error) {
	return nil, nil
}
func (stubReader) Scan(context.Context, string, ...transport.ReadOption) (map[string][]byte, error) {
	return nil, nil
}
func (stubReader) Exists(context.Context, string, ...transport.ReadOption) (bool, error) {
	return false, nil
}

// TestBuildSchedulerEngine_ClusterLikeModeWithoutWiringRefuses is the regression
// for inferring topology from the absence of wiring.
//
// The fail-closed guarantee must not depend on every caller remembering to pass
// the clustering. An embedder using the exported builder in a multi-node mode
// that simply omits WithSchedulerClustering would otherwise get the single-node
// engine — no leader gate, no claim, no error, on every node. Both multi-node
// modes are covered: devcluster runs several local nodes and is the mode most
// likely to be used for multi-node testing.
func TestBuildSchedulerEngine_ClusterLikeModeWithoutWiringRefuses(t *testing.T) {
	for _, mode := range []string{"cluster", "devcluster"} {
		t.Run(mode, func(t *testing.T) {
			engine, err := buildSchedulerEngine(
				scheduler.DefaultConfig(), kvstore.NewMemory(), mode, nil)
			if err == nil {
				t.Fatal("a multi-node mode with no clustering wiring must refuse to build, " +
					"not silently fall back to the single-node scheduler")
			}
			if engine != nil {
				t.Error("no engine may be returned alongside the refusal")
			}
			var incomplete *IncompleteSchedulerClusteringError
			if !errors.As(err, &incomplete) {
				t.Fatalf("expected IncompleteSchedulerClusteringError, got %T: %v", err, err)
			}
			if incomplete.Mode != mode {
				t.Errorf("Mode = %q, want %q — the error must name the mode that "+
					"required the wiring", incomplete.Mode, mode)
			}
		})
	}
}

// TestBuildSchedulerEngine_UnknownModeIsTreatedAsSingleNode documents the
// deliberate boundary: only the modes go-quicbaas validates as multi-node
// require the clustered scheduler. An unrecognised mode is not silently
// promoted into a cluster.
func TestBuildSchedulerEngine_UnknownModeIsTreatedAsSingleNode(t *testing.T) {
	engine, err := buildSchedulerEngine(
		scheduler.DefaultConfig(), kvstore.NewMemory(), "wat-is-this-mode", nil)
	if err != nil || engine == nil {
		t.Fatalf("an unrecognised mode must build a single-node scheduler rather than "+
			"being promoted into a cluster: engine=%v err=%v", engine, err)
	}
}
