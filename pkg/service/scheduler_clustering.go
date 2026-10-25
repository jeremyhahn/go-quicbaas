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
	"strconv"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
)

// leaderQueryTimeout bounds a single leadership query.
//
// The scheduler asks once per tick per due job, so a query that cannot be
// answered promptly must fail rather than stall dispatch. A failed query reads
// as "not leader", which suppresses the firing — the safe direction.
const leaderQueryTimeout = 5 * time.Second

// ClusterMembership is the go-qrdb surface that answers who leads a shard.
// It is satisfied by qrdbsdk.ClusterClient.
type ClusterMembership interface {
	Members(ctx context.Context, shardID uint64) ([]qrdbsdk.Member, error)
}

// qrdbLeaderChecker answers "is this node the Raft leader for the scheduler's
// shard" by asking go-qrdb, every time.
//
// The query is LIVE and deliberately uncached. Leadership is exactly the fact
// that moves without telling you: a cached bool is how a deposed leader goes on
// believing it leads, which is the docs/18 V4 double-fire in miniature. The
// firing claim exists because even a live answer can be briefly wrong on two
// nodes at once during an election; caching would widen that window from
// milliseconds to however long the cache lives.
type qrdbLeaderChecker struct {
	cluster   ClusterMembership
	shardID   uint64
	replicaID uint64
	timeout   time.Duration
}

// NewQRDBLeaderChecker builds a scheduler.LeaderChecker over go-qrdb's cluster
// membership view for the given shard and replica.
//
// replicaID identifies this node within the shard; it is the same Raft replica
// ID the node was booted with, so the checker asks about itself rather than
// about whichever replica happens to be listed first.
//
// cluster must carry the caller's identity on every read (the server passes
// its service-principal-bound qrdbsdk.ClusterClient): qrdb refuses an
// anonymous membership read, and a refusal reads as "not leader".
func NewQRDBLeaderChecker(cluster ClusterMembership, shardID, replicaID uint64) *qrdbLeaderChecker {
	if cluster == nil {
		return nil
	}
	return &qrdbLeaderChecker{
		cluster:   cluster,
		shardID:   shardID,
		replicaID: replicaID,
		timeout:   leaderQueryTimeout,
	}
}

// IsLeader reports whether this replica currently leads the scheduler's shard.
//
// An error is returned rather than a guess when membership cannot be read or
// this replica is not in it: the scheduler treats an error as "not leader" and
// declines to fire, which is the direction that costs a cycle rather than a
// duplicate.
func (c *qrdbLeaderChecker) IsLeader() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	members, err := c.cluster.Members(ctx, c.shardID)
	if err != nil {
		return false, &LeadershipUnknownError{ShardID: c.shardID, Cause: err}
	}

	for _, m := range members {
		if m.ReplicaID == c.replicaID {
			return m.IsLeader, nil
		}
	}

	// This replica is not in its own shard's membership. That is a real
	// condition — mid-removal, or a misconfigured replica ID — and answering
	// "not leader" silently would let a node sit idle looking healthy.
	return false, &ReplicaNotInShardError{ShardID: c.shardID, ReplicaID: c.replicaID}
}

// LeadershipUnknownError reports that go-qrdb could not answer who leads a
// shard. The scheduler treats it as "not leader".
type LeadershipUnknownError struct {
	ShardID uint64
	Cause   error
}

// Error implements the error interface.
func (e *LeadershipUnknownError) Error() string {
	return "scheduler: leadership for shard " + strconv.FormatUint(e.ShardID, 10) +
		" could not be determined: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *LeadershipUnknownError) Unwrap() error { return e.Cause }

// ReplicaNotInShardError reports that this replica is absent from the
// membership of the shard it schedules on.
type ReplicaNotInShardError struct {
	ShardID   uint64
	ReplicaID uint64
}

// Error implements the error interface.
func (e *ReplicaNotInShardError) Error() string {
	return "scheduler: replica " + strconv.FormatUint(e.ReplicaID, 10) +
		" is not a member of shard " + strconv.FormatUint(e.ShardID, 10)
}

// SchedulerClustering carries everything a clustered scheduler needs, and is
// supplied only on a clustered boot.
//
// It travels as one value rather than four builder options because the parts
// are not independently useful: a leader checker without a claim arbiter gives
// the churn double-fire, and an arbiter without a leader checker gives every
// node firing every job. Requiring them together makes the incomplete
// combination unrepresentable rather than merely discouraged.
type SchedulerClustering struct {
	// NodeID identifies this node for Job.NodeAffinity matching.
	NodeID string

	// Leader answers whether this node currently leads the scheduler's shard.
	Leader scheduler.LeaderChecker

	// Claims decides contested firings; Marks reads the current mark for the
	// advance guard. Both come off the clustered node's SDK client.
	Claims schedulerstorage.ClaimTransactor
	Marks  schedulerstorage.ClaimReader
}

// complete reports whether every part needed to build a clustered scheduler is
// present.
func (c *SchedulerClustering) complete() bool {
	return c != nil && c.Leader != nil && c.Claims != nil && c.Marks != nil
}

// IncompleteSchedulerClusteringError reports a clustered boot that supplied
// only some of what a clustered scheduler needs.
//
// It is a construction error, never a downgrade. The parts are useless
// individually: without the leader checker every node fires every cluster-wide
// job, and without the claim arbiter two nodes fire it during leader churn.
// Starting anyway would present a scheduler that looks correct and is not.
type IncompleteSchedulerClusteringError struct {
	Mode      string
	HasLeader bool
	HasClaims bool
	HasMarks  bool
}

// Error implements the error interface.
func (e *IncompleteSchedulerClusteringError) Error() string {
	return "scheduler: " + e.Mode + " boot is missing part of its clustering wiring" +
		" (leader=" + strconv.FormatBool(e.HasLeader) +
		" claims=" + strconv.FormatBool(e.HasClaims) +
		" marks=" + strconv.FormatBool(e.HasMarks) + ")"
}

// buildSchedulerEngine constructs the scheduler for this boot's topology.
//
// Standalone boots get the single-node engine over the single-node store,
// exactly as before: one scheduler with no peer to race needs no leader gate
// and no claim.
//
// Cluster-like boots get the clustered engine over the clustered store, or an
// error. There is deliberately no third path.
//
// mode is consulted rather than inferred from clustering being nil. Inferring
// it would make the whole fail-closed guarantee depend on every caller
// remembering to pass the wiring: an embedder using the exported builder in
// cluster mode that simply omits WithSchedulerClustering would silently get the
// single-node engine — no gate, no claim, no error, on every node. Absent
// wiring in a cluster-like mode is a refusal, never an inference.
func buildSchedulerEngine(
	cfg *scheduler.SchedulerConfig,
	kv interfaces.KVStore,
	mode string,
	clustering *SchedulerClustering,
) (*scheduler.SchedulerEngine, error) {
	if clustering == nil {
		if config.IsClusterLikeMode(mode) {
			return nil, &IncompleteSchedulerClusteringError{Mode: mode}
		}
		return scheduler.New(cfg, schedulerstorage.NewStorage(kv)), nil
	}
	if !clustering.complete() {
		return nil, &IncompleteSchedulerClusteringError{
			Mode:      mode,
			HasLeader: clustering.Leader != nil,
			HasClaims: clustering.Claims != nil,
			HasMarks:  clustering.Marks != nil,
		}
	}

	store, err := schedulerstorage.NewClusteredStorage(kv, clustering.Claims, clustering.Marks)
	if err != nil {
		return nil, err
	}
	return scheduler.NewWithClustering(cfg, store, clustering.NodeID, clustering.Leader)
}
