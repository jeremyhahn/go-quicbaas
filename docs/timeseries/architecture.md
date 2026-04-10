# Time Series Architecture

## Overview

QuicBaaS time series storage combines Raft consensus for strong consistency with FNV-64a hash-based sharding for horizontal scalability.

## Data Flow

```
                                    +------------------+
                                    |   Shard Router   |
                                    | (FNV-64a Hash)   |
                                    +--------+---------+
                                             |
              +------------------------------+------------------------------+
              |                              |                              |
    +---------v---------+          +---------v---------+          +---------v---------+
    |    Shard 1        |          |    Shard 2        |          |    Shard N        |
    |  (Raft Group)     |          |  (Raft Group)     |          |  (Raft Group)     |
    +-------------------+          +-------------------+          +-------------------+
    | Leader  | Follower|          | Leader  | Follower|          | Leader  | Follower|
    +---------+---------+          +---------+---------+          +---------+---------+
              |                              |                              |
    +---------v---------+          +---------v---------+          +---------v---------+
    |     PebbleDB      |          |     PebbleDB      |          |     PebbleDB      |
    +-------------------+          +-------------------+          +-------------------+
```

## Raft Consensus

### State Machine

The `TimeSeriesConsensusFSM` implements QuicRaft's `IStateMachine` interface:

```go
type TimeSeriesConsensusFSM struct {
    shardID   uint64
    replicaID uint64
    storage   *timeseries.Storage
    handlers  map[TimeSeriesConsensusOp]tsHandler  // O(1) dispatch
}
```

### Operations

| Operation | Code | Description |
|-----------|------|-------------|
| TSOpWritePoint | 1 | Write single data point |
| TSOpWriteBatch | 2 | Write multiple points atomically |
| TSOpDeleteSeries | 3 | Delete time series data |
| TSOpSetRetention | 4 | Set retention policy |

### Entry Format

Operations are serialized using MessagePack:

```go
type TimeSeriesEntry struct {
    Op        TimeSeriesConsensusOp      // Operation type
    Point     *timeseries.DataPoint      // Single point (WritePoint)
    Points    []*timeseries.DataPoint    // Batch points (WriteBatch)
    Metric    string                     // Metric name (Delete/Retention)
    Tags      map[string]string          // Tag filter
    Retention *timeseries.RetentionPolicy // Policy (SetRetention)
    StartTime time.Time                  // Delete range start
    EndTime   time.Time                  // Delete range end
}
```

### Snapshot Support

The FSM supports snapshots for efficient recovery:

- Captures `lastIndex`, `opCount`, and `retentionPolicies`
- Uses MessagePack encoding with length prefix
- Restores retention policies to storage on recovery

## Multi-Shard Routing

### FNV-64a Hash Algorithm

The router uses FNV-64a (Fowler-Noll-Vo) hashing for consistent metric-to-shard mapping:

```go
func (r *TimeSeriesRouter) Route(metric string) uint64 {
    hash := fnv.New64a()
    hash.Write([]byte(metric))
    idx := hash.Sum64() & r.shardMask  // Bitwise AND for fast modulo
    return r.shards[idx]
}
```

### Configuration

```go
type TimeSeriesRouterConfig struct {
    NumShards   int     // Must be power of 2 (default: 16)
    ShardPrefix string  // Shard ID prefix (default: "ts")
}
```

### Properties

- **Deterministic**: Same metric always routes to same shard
- **Efficient**: O(1) routing with bitwise operations
- **Power of 2**: Shard count must be power of 2 for fast modulo

### Batch Routing

For batch writes, points are grouped by shard for efficient processing:

```go
func (r *TimeSeriesRouter) RouteBatch(points []*timeseries.DataPoint) map[uint64][]*timeseries.DataPoint
```

## Storage Format

### Key Structure

Time series data uses composite keys:

```
ts:<metric>:<timestamp>:<tag_hash>
```

Components:
- `ts` - Fixed prefix for time series data
- `metric` - Metric name
- `timestamp` - Unix nanosecond timestamp
- `tag_hash` - Hash of sorted tag key-value pairs

### Value Format

Data points are stored as MessagePack-encoded structures containing the value and full tag set.

## Write Path

1. Client sends WritePoint/WriteBatch request
2. Router determines target shard(s) using FNV-64a hash
3. Request forwarded to shard leader
4. Leader proposes entry to Raft log
5. Entry replicated to followers (quorum required)
6. FSM applies entry to PebbleDB storage
7. Response returned to client

## Read Path

1. Client sends Query request
2. Router determines target shard
3. Query executed directly against PebbleDB (no Raft)
4. Results aggregated and returned

## Consistency Model

- **Writes**: Linearizable (Raft consensus)
- **Reads**: Sequential consistency (read from any replica)
- **Retention**: Eventually consistent (background enforcement)

## Multi-Tenant Isolation

The time series service scopes all metric operations to the requesting tenant using `iam.TenantFromContext(ctx)`.

### Key Scoping

In multi-tenant mode, metric names are prefixed at the service layer:

```
ts-tenant/{tenantID}/{metric}
```

This prefix is applied before data reaches the storage or routing layers. The original metric name is preserved in responses by stripping the prefix before returning results.

### Operation Scoping

- **Write (WritePoint/WriteBatch)**: Each data point's metric name is scoped with the tenant prefix before storage
- **Query**: The metric in the query is scoped with the tenant prefix before execution
- **List**: Results are filtered to the requesting tenant's prefix, with the prefix stripped from returned metric names
- **Delete/Retention**: The metric name is scoped before the operation is applied

### Single-Tenant Mode

When the tenant ID from context is empty, the service operates in single-tenant mode. Metrics are stored and queried as-is with no prefix applied and no filtering on list operations. This preserves full backward compatibility with existing deployments.

### Cross-Tenant Access

Attempts to access metrics belonging to a different tenant return a typed `ErrTenantAccessDenied` error. Tenant boundaries are enforced at the service layer before requests reach the storage engine.
