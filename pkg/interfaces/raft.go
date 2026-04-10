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

// Package interfaces provides interface definitions for all major QuicBaaS
// components, enabling comprehensive testing through dependency injection.
package interfaces

import (
	"context"
)

// ---------------------------------------------------------------------------
// Consistency and write mode types
// ---------------------------------------------------------------------------

// ConsistencyLevel defines the consistency guarantee for read operations.
type ConsistencyLevel int

const (
	// ConsistencyStrong requires full Raft consensus for reads.
	ConsistencyStrong ConsistencyLevel = iota
	// ConsistencyLinearizable uses the ReadIndex optimization for
	// linearizable reads without a full log round-trip.
	ConsistencyLinearizable
	// ConsistencyEventual allows reading from the local state machine
	// without Raft coordination, returning potentially stale data.
	ConsistencyEventual
)

// ReadOptions configures the behavior of a consistency-aware read.
type ReadOptions struct {
	// Consistency specifies the desired consistency level.
	Consistency ConsistencyLevel
	// Timeout is the maximum duration in nanoseconds to wait for the read.
	Timeout int64
	// AllowStale permits the read to return stale data when true.
	AllowStale bool
}

// WriteMode defines the durability guarantee for write proposals.
type WriteMode uint8

const (
	// WriteModeSync waits for quorum replication before acknowledging.
	WriteModeSync WriteMode = iota
	// WriteModeWALDurable persists to the local WAL before acknowledging.
	WriteModeWALDurable
	// WriteModeBatched aggregates writes into batches for throughput.
	WriteModeBatched
)

// ---------------------------------------------------------------------------
// State machine types
// ---------------------------------------------------------------------------

// Result holds the output of a state machine operation.
type Result struct {
	// Value is a numeric result returned by the state machine.
	Value uint64
	// Data is an arbitrary byte payload returned by the state machine.
	Data []byte
}

// Entry represents a single Raft log entry to be applied to the state machine.
type Entry struct {
	// Index is the Raft log index of this entry.
	Index uint64
	// Cmd is the command payload to apply.
	Cmd []byte
}

// CreateFunc is a factory function that produces a state machine instance
// for the given shard and replica pair.
type CreateFunc func(shardID, replicaID uint64) interface{}

// ---------------------------------------------------------------------------
// Shard configuration
// ---------------------------------------------------------------------------

// ShardConfig holds per-shard Raft configuration parameters.
type ShardConfig struct {
	// ShardID identifies the Raft shard.
	ShardID uint64
	// ReplicaID identifies this node's replica within the shard.
	ReplicaID uint64
	// IsObserver marks the replica as a non-voting observer.
	IsObserver bool
	// CheckQuorum enables the check-quorum protocol where a leader
	// periodically verifies it still holds a majority.
	CheckQuorum bool
	// ElectionRTT is the minimum number of message round-trip ticks
	// before an election timeout fires.
	ElectionRTT uint64
	// HeartbeatRTT is the number of message round-trip ticks between
	// leader heartbeats.
	HeartbeatRTT uint64
	// SnapshotEntries is the number of applied log entries between
	// automatic snapshots. Set to 0 to disable automatic snapshots.
	SnapshotEntries uint64
	// CompactionOverhead is the number of log entries to keep after
	// compaction for slow followers to catch up.
	CompactionOverhead uint64
	// OrderedConfigChange requires configuration changes to be applied
	// in strict order using the config change index.
	OrderedConfigChange bool
}

// ---------------------------------------------------------------------------
// Shard health and node information
// ---------------------------------------------------------------------------

// ShardHealth represents the health state of a single shard.
type ShardHealth uint8

const (
	// ShardHealthy indicates the shard has a leader and is replicating.
	ShardHealthy ShardHealth = iota
	// ShardNoLeader indicates the shard has no elected leader.
	ShardNoLeader
	// ShardLagging indicates the shard is behind on log replication.
	ShardLagging
	// ShardUnloaded indicates the shard is not loaded on this node.
	ShardUnloaded
	// ShardError indicates the shard is in an error state.
	ShardError
)

// NodeInfo identifies a Raft replica by its shard and replica IDs.
type NodeInfo struct {
	// ShardID is the shard this replica belongs to.
	ShardID uint64
	// ReplicaID is the unique identifier of the replica within its shard.
	ReplicaID uint64
}

// ShardInfo provides a detailed status snapshot for a single shard.
type ShardInfo struct {
	// ShardID is the identifier of the shard.
	ShardID uint64
	// ReplicaID is this node's replica identifier within the shard.
	ReplicaID uint64
	// LeaderID is the current leader's replica identifier.
	LeaderID uint64
	// Term is the current Raft term.
	Term uint64
	// IsLeader is true when this replica is the current leader.
	IsLeader bool
	// IsObserver is true when this replica is a non-voting observer.
	IsObserver bool
	// IsWitness is true when this replica is a voting witness that
	// does not maintain a state machine.
	IsWitness bool
	// StateMachineType identifies the type of state machine running.
	StateMachineType uint64
	// Pending is the number of pending proposals in the shard.
	Pending uint64
	// Nodes maps replica IDs to their Raft network addresses.
	Nodes map[uint64]string
	// Health is the aggregate health status of the shard.
	Health ShardHealth
	// LastApplied is the index of the last applied log entry.
	LastApplied uint64
	// ConfigChangeIndex is the index of the last applied config change.
	ConfigChangeIndex uint64
}

// ---------------------------------------------------------------------------
// Host information and health
// ---------------------------------------------------------------------------

// NodeHostInfoOption configures which information to include when
// querying the host for its current state.
type NodeHostInfoOption struct {
	// SkipLogInfo excludes per-shard log metadata from the result
	// when set to true, reducing the cost of the query.
	SkipLogInfo bool
}

// NodeHostInfo is a snapshot of the host's runtime state including
// address, loaded shards, and optional log metadata.
type NodeHostInfo struct {
	// RaftAddress is the host's Raft network address.
	RaftAddress string
	// ShardInfoList contains status information for every loaded shard.
	ShardInfoList []ShardInfo
	// LogInfo contains per-replica log metadata. It is nil when the
	// query was issued with SkipLogInfo set to true.
	LogInfo []NodeInfo
}

// HealthStatus is an aggregate health report for the host.
type HealthStatus struct {
	// Status is a human-readable summary such as "healthy" or "degraded".
	Status string
	// BarrierSealed indicates whether the host's barrier is sealed,
	// preventing new operations from being accepted.
	BarrierSealed bool
	// DiskOK is true when the underlying storage passes health checks.
	DiskOK bool
	// ShardCount is the number of shards loaded on this host.
	ShardCount int
	// Shards provides per-shard health details.
	Shards []ShardInfo
}

// ---------------------------------------------------------------------------
// Asynchronous request types
// ---------------------------------------------------------------------------

// RequestResult holds the outcome of an asynchronous Raft request.
type RequestResult struct {
	// Value is a numeric result code.
	Value uint64
	// Data is an arbitrary byte payload.
	Data []byte
	// Err is non-nil when the request failed.
	Err error
}

// RequestState represents an in-flight asynchronous Raft request.
// Callers may retrieve the result synchronously via Result or
// asynchronously via ResultC, and must call Release when done.
type RequestState interface {
	// Result blocks until the request completes and returns the result.
	Result() (RequestResult, error)
	// ResultC returns a channel that is closed when the result is ready.
	ResultC() <-chan RequestResult
	// Release returns internal resources to the pool. It must be called
	// exactly once after the result has been consumed.
	Release()
}

// ---------------------------------------------------------------------------
// RaftNode interface
// ---------------------------------------------------------------------------

// RaftNode defines the interface for Raft consensus operations.
// It mirrors the qrdb Host interface, abstracting the underlying
// QuicRaft NodeHost to enable testing and clean separation of concerns.
type RaftNode interface {
	// Identity and status

	// ID returns the unique host identifier string.
	ID() string

	// RaftAddress returns the host's Raft network address.
	RaftAddress() string

	// ShardCount returns the number of shards loaded on this host.
	ShardCount() int

	// GetNodeHostInfo returns a snapshot of the host's state for
	// monitoring and debugging.
	GetNodeHostInfo(opt NodeHostInfoOption) *NodeHostInfo

	// HealthCheck returns the aggregate health status of the host.
	HealthCheck() *HealthStatus

	// Drain transitions the host into drain mode where new proposals
	// and reads are rejected while in-flight operations complete.
	Drain() error

	// IsDraining returns true when the host is in drain mode.
	IsDraining() bool

	// Proposal operations

	// SyncPropose proposes a write operation and waits for it to be committed.
	// Returns the state machine result and an error if the proposal fails or times out.
	SyncPropose(ctx context.Context, shardID uint64, cmd []byte) (Result, error)

	// ProposeWithMode proposes a write with specified durability guarantee.
	// Allows trading consistency for performance: Sync (quorum), WALDurable
	// (local WAL), or Batched (aggregated writes).
	ProposeWithMode(ctx context.Context, shardID uint64, cmd []byte, mode WriteMode) (Result, error)

	// Read operations

	// SyncRead performs a linearizable read against the state machine.
	// The query parameter is passed to the state machine for processing.
	SyncRead(ctx context.Context, shardID uint64, query interface{}) (interface{}, error)

	// StaleRead reads from the local state machine without Raft coordination.
	// Returns potentially stale data but with sub-millisecond latency.
	StaleRead(ctx context.Context, shardID uint64, query interface{}) (interface{}, error)

	// ReadWithConsistency performs a read with the specified consistency level.
	// Strong: full Raft consensus. Linearizable: ReadIndex optimization.
	// Eventual: local stale read.
	ReadWithConsistency(ctx context.Context, shardID uint64, query interface{}, opts ReadOptions) (interface{}, error)

	// Cluster operations

	// GetLeaderID returns the current leader's replica ID for the shard
	// and whether a leader is known. Returns 0, false, error if the shard
	// does not exist.
	GetLeaderID(shardID uint64) (uint64, bool, error)

	// GetShardRaftState returns the Raft state for a shard: the config
	// change index and current term. Used for monitoring and consistency
	// checks.
	GetShardRaftState(shardID uint64) (configChangeIndex uint64, term uint64, err error)

	// RequestAddNode requests adding a new replica to a shard.
	// The configChangeIndex should be obtained from GetShardRaftState
	// to ensure ordered config changes. Pass 0 to skip the ordering check.
	RequestAddNode(ctx context.Context, shardID, replicaID uint64, addr string, configChangeIndex uint64) (RequestState, error)

	// RequestRemoveNode requests removing a replica from a shard.
	// The configChangeIndex should be obtained from GetShardRaftState.
	RequestRemoveNode(ctx context.Context, shardID, replicaID uint64, configChangeIndex uint64) (RequestState, error)

	// RequestSnapshot requests an asynchronous snapshot for a shard.
	RequestSnapshot(ctx context.Context, shardID uint64) (RequestState, error)

	// Lifecycle management

	// StartShard initializes and starts a Raft group for the given shard.
	// members maps replicaID to address for initial cluster members. join
	// indicates whether this node is joining an existing cluster. create
	// is the state machine factory. cfg provides per-shard Raft
	// configuration including ShardID and ReplicaID.
	StartShard(members map[uint64]string, join bool, create CreateFunc, cfg ShardConfig) error

	// StopShard stops the Raft group for the given shard.
	StopShard(shardID uint64) error

	// Close gracefully shuts down the host and all its Raft groups.
	Close() error
}
