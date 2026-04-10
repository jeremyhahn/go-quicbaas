# Raft Consensus

QuicBaaS uses [QuicRaft](https://github.com/jeremyhahn/go-quicraft) for distributed consensus and replication.

## Overview

Raft ensures multiple nodes agree on a sequence of log entries despite failures. QuicBaaS uses a multi-raft architecture where multiple independent Raft groups run on the same cluster.

QuicRaft provides QUIC transport with mandatory mTLS, LeaseRead support, internal session management, and a cleaner API. Sessions are managed internally, eliminating manual session lifecycle management.

## Multi-Raft Architecture

### Why Multi-Raft?

Single Raft groups have limitations: all writes through one leader, single bottleneck, and failures affect the entire database.

Multi-raft provides: horizontal scaling across multiple leaders, independent failure domains, and different configurations per shard.

### Shard Assignment

```go
// Route entity to shard using consistent hashing
func (r *Router) Route(entityType string, id uint64) uint64 {
    key := fmt.Sprintf("%s:%d", entityType, id)
    hash := xxhash.Sum64([]byte(key))
    return (hash % r.numShards) + 1
}
```

## QuicRaft Integration

### Node Host Setup

```go
import "github.com/jeremyhahn/go-quicraft"

// Create NodeHost
nhc := quicraft.NodeHostConfig{
    NodeHostDir:    "/var/lib/quicbaas/raft",
    RTTMillisecond: 200,
    RaftAddress:    "localhost:63001",
    MutualTLS:      true,
    NotifyCommit:   true,
}

nh, err := quicraft.NewNodeHost(nhc)
if err != nil {
    return err
}
```

### Starting a Raft Group

```go
// Raft configuration
rc := quicraft.Config{
    NodeID:             1,
    ShardID:            1,
    ElectionRTT:        10,
    HeartbeatRTT:       1,
    CheckQuorum:        true,
    SnapshotEntries:    10000,
    CompactionOverhead: 5000,
}

// Initial members
members := map[uint64]string{
    1: "localhost:63001",
    2: "localhost:63002",
    3: "localhost:63003",
}

// Start Raft cluster
err := nh.StartCluster(members, false, NewStateMachine, rc)
if err != nil {
    return err
}
```

## Consistency Levels

### Quorum Reads (Linearizable via SyncRead)

Quorum reads provide linearizability - clients always see the most recent write:

```go
// SyncRead performs a quorum read
result, err := nh.SyncRead(ctx, shardID, query)
```

Process: leader verifies leadership, reads from state machine, returns result.

Guarantees: always returns latest committed value, no stale reads, requires majority of nodes available.

Use cases: critical reads, financial transactions, inventory management.

### LeaseRead (Linearizable, Lower Latency)

LeaseRead provides linearizability using leader lease for reduced latency:

```go
// LeaseRead uses leader lease for faster linearizable reads
result, err := nh.LeaseRead(ctx, shardID, query)
```

Process: leader checks lease validity, reads from state machine, returns result.

Guarantees: linearizable with clock-based lease, lower latency than SyncRead (~1-3ms), requires synchronized clocks.

Use cases: high-throughput linearizable reads where clock sync is available.

### Local Reads (Stale)

Local reads are much faster but may return stale data:

```go
// StaleRead performs a local read
result, err := nh.StaleRead(shardID, query)
```

Process: read directly from local state machine without leadership verification.

Guarantees: very low latency (<1ms), may return stale data, works during network partitions.

Use cases: read-heavy workloads, analytics and reporting, non-critical data access.

## Write Path

All writes go through Raft consensus. QuicRaft manages sessions internally:

```go
// Propose a write (no manual session management needed)
result, err := nh.SyncPropose(ctx, shardID, data)
```

Process: client proposes to leader, leader replicates to followers, majority acknowledgment triggers commit, state machine applies write, result returned to client.

With NotifyCommit enabled, WAL-durable early notification (~1-2ms) is available via dual-channel RequestState before the state machine applies the entry.

**Timeline:**
```
Client  Leader  Follower1  Follower2  StateMachine
  |       |         |          |          |
  |------>|         |          |          |  1. Propose
  |       |-------->|          |          |  2. Replicate
  |       |---------|--------->|          |  2. Replicate
  |       |<--------|          |          |  3. Ack
  |       |<--------|----------|          |  3. Ack
  |       |                    |          |  4. Majority reached
  |       |--------------------------->   |  5. Apply
  |       |<---------------------------|   |  6. Result
  |<------|                                |  7. Response
```

## Leader Election

Raft automatically elects a leader when:
- Cluster starts
- Current leader fails
- Network partition heals

**Election Process:**
1. Follower times out waiting for leader heartbeat
2. Follower becomes candidate and requests votes
3. Other nodes vote for candidate
4. Candidate with majority votes becomes leader
5. New leader sends heartbeats to establish authority

**Configuration:**
```go
rc := quicraft.Config{
    ElectionRTT:  10,  // 10 RTT intervals before election
    HeartbeatRTT: 1,   // Heartbeat every 1 RTT interval
}
```

**RTT (Round-Trip Time):**
- Base time unit for Raft timeouts
- Configured in NodeHostConfig
- Default: 200ms
- Election timeout: ElectionRTT x RTTMillisecond = 2 seconds
- Heartbeat interval: HeartbeatRTT x RTTMillisecond = 200ms

## Log Replication

### Log Structure

Each Raft log entry contains:
```go
type Entry struct {
    Index uint64  // Position in log
    Term  uint64  // Election term when created
    Cmd   []byte  // Serialized command
}
```

### Log Compaction (Snapshots)

Raft logs grow unbounded without compaction. Snapshots provide:
- **Space savings:** Delete old log entries
- **Faster recovery:** Restore from snapshot vs replaying log
- **Bounded memory:** Limit in-memory log size

**Configuration:**
```go
rc := quicraft.Config{
    SnapshotEntries:    10000,  // Snapshot after 10K entries
    CompactionOverhead: 5000,   // Keep 5K entries after snapshot
}
```

**Snapshot Process:**
1. State machine reaches entry threshold
2. State machine serializes current state
3. Raft saves snapshot to disk
4. Old log entries deleted
5. Snapshot replicated to lagging followers

## Membership Changes

Add or remove nodes from Raft cluster:

```go
// Add node
err := nh.SyncRequestAddNode(ctx, shardID, nodeID, address, 0)

// Remove node
err := nh.SyncRequestDeleteNode(ctx, shardID, nodeID, 0)
```

**Safety:**
- Changes processed through Raft consensus
- No split-brain scenarios
- Automatic configuration updates

**Process:**
1. Configuration change proposed to leader
2. Replicated to majority
3. New configuration activated
4. New node catches up (if adding)
5. Old node stops (if removing)

## Failure Scenarios

### Node Failure

**Impact:**
- Cluster remains available if quorum exists
- Reads/writes continue normally
- Failed node marked as unavailable

**Recovery:**
1. Failed node restarts
2. Rejoins cluster
3. Catches up from log or snapshot
4. Resumes normal operation

**Timeline:**
- Detection: ~2 seconds (election timeout)
- Recovery: <30 seconds (typical)

### Network Partition

**Scenario:** Cluster split into two groups

**Behavior:**
- Partition with majority continues operating
- Minority partition becomes read-only
- Writes to minority fail (no quorum)

**Recovery:**
1. Network partition heals
2. Minority nodes catch up from leader
3. Conflicting entries rolled back
4. Normal operation resumes

### Leader Failure

**Process:**
1. Followers detect missing heartbeats
2. Election triggered (2 seconds)
3. New leader elected
4. New leader establishes authority
5. Normal operation resumes

**Downtime:**
- Typical: 2-3 seconds
- Worst case: 4-6 seconds (multiple rounds)

## Performance Tuning

### RTT Configuration

```go
nhc := quicraft.NodeHostConfig{
    RTTMillisecond: 200,  // Adjust based on network
}
```

**Low-latency network (same datacenter):**
```go
RTTMillisecond: 50   // 50ms base RTT
ElectionRTT: 20      // 1 second election timeout
HeartbeatRTT: 2      // 100ms heartbeat
```

**High-latency network (cross-region):**
```go
RTTMillisecond: 500  // 500ms base RTT
ElectionRTT: 10      // 5 second election timeout
HeartbeatRTT: 1      // 500ms heartbeat
```

### Snapshot Configuration

```go
rc := quicraft.Config{
    SnapshotEntries:    10000,   // Larger = less frequent snapshots
    CompactionOverhead: 5000,    // Larger = more log entries kept
}
```

**High write throughput:**
```go
SnapshotEntries: 50000     // Less frequent snapshots
CompactionOverhead: 10000  // More log history
```

**Low memory:**
```go
SnapshotEntries: 5000      // More frequent snapshots
CompactionOverhead: 1000   // Less log history
```

### Batch Operations

```go
// Use batch API for higher throughput
entries := make([][]byte, 100)
for i := 0; i < 100; i++ {
    entries[i] = createEntry(i)
}

results := nh.SyncProposeBatch(ctx, shardID, entries)
```

**Benefits:**
- Amortize network overhead
- Better CPU utilization
- Higher throughput (10x improvement)

## Monitoring

### Raft Metrics

```go
type RaftMetrics struct {
    // Leadership
    IsLeader     bool
    LeaderID     uint64
    Term         uint64

    // Log
    CommittedIndex uint64
    AppliedIndex   uint64
    SnapshotIndex  uint64

    // Performance
    ProposalsMade  uint64
    ProposalsDropped uint64
    ProposalsDuration time.Duration
}
```

**Key Metrics to Monitor:**
- **Leader changes:** Frequent changes indicate instability
- **Proposal drop rate:** High rate indicates overload
- **Log gap:** Large gap between committed and applied indicates slow state machine
- **Snapshot frequency:** Too frequent indicates undersized configuration

### Health Checks

QuicRaft provides built-in health checks and graceful drain support:

```go
// Check if cluster is healthy
func (n *Node) IsHealthy(shardID uint64) bool {
    // Has leader
    leader := n.nh.GetLeaderID(shardID)
    if leader == 0 {
        return false
    }

    // Can propose (has quorum)
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    _, err := n.nh.SyncPropose(ctx, shardID, []byte("healthcheck"))
    return err == nil
}
```

## Best Practices

### Cluster Sizing

**3-node cluster:**
- Tolerates 1 failure
- Good for development/small deployments
- Lower overhead

**5-node cluster:**
- Tolerates 2 failures
- Better for production
- Higher availability

**7+ nodes:**
- Rarely needed
- Higher overhead
- Consider multi-raft instead

### Write Patterns

**DO:**
- Batch related writes
- Use async propose when possible
- Set appropriate timeouts

**DON'T:**
- Make writes from multiple clients to same key
- Propose very large entries (>1MB)
- Ignore proposal failures

### Read Patterns

**DO:**
- Use local reads for non-critical data
- Use LeaseRead for high-throughput linearizable reads
- Use SyncRead for critical data requiring strictest consistency
- Cache frequently accessed data

**DON'T:**
- Use SyncRead for everything
- Make reads without timeouts
- Ignore read failures

## Troubleshooting

### Cluster Won't Start

**Check:**
- All nodes have same ShardID
- All nodes have unique NodeIDs
- All nodes have correct member addresses
- Data directories are writable
- mTLS certificates are valid and trusted

### Slow Writes

**Possible causes:**
- Network latency too high
- Disk I/O bottleneck
- State machine too slow
- RTT configuration too large

**Solutions:**
- Reduce RTT settings
- Use faster disks (SSD)
- Optimize state machine
- Enable batching

### Frequent Leader Changes

**Possible causes:**
- Network instability
- Node overload
- RTT settings too aggressive

**Solutions:**
- Increase ElectionRTT
- Reduce load
- Check network stability
- Add more resources

## References

- [Raft Paper](https://raft.github.io/raft.pdf) - Original Raft specification
- [QuicRaft Documentation](https://github.com/jeremyhahn/go-quicraft) - Library documentation
- [Raft Visualization](http://thesecretlivesofdata.com/raft/) - Interactive Raft demo

---

**Last Updated:** 2026-02-23
