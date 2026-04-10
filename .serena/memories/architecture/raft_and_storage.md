# Raft and Storage Architecture

## Multi-Raft Design
QuicBaaS runs multiple Raft groups (shards) on each node via go-quicraft (QuicRaft):
- **Shard 1-3**: Core KV shards
- **Shard 100 (KMS)**: Key Management Service
- **Shard 200 (Vector)**: Vector database  
- **Shard 300 (TimeSeries)**: Time series data
- **Shard 1000 (KMS raft)**: KMS Raft shard
- **Shard 1001 (Secrets)**: Secrets engine

## Request Flow
```
Client → Transport (gRPC/REST/QUIC/MCP) → Service Layer → Raft Node → QuicRaft Host → State Machine → PebbleDB
```

## State Machine: FastStateMachine (`pkg/raft/statemachine/fast.go`)
- 256 sharded cache buckets with RWMutex (hash-based via fnv32a)
- Sync cache update + async PebbleDB write on Put
- Tombstone-based delete (nil value in cache, async PebbleDB delete)
- Cache-first reads with PebbleDB fallback

## Write Modes (`pkg/raft/writemode/`)
- **Sync**: Full quorum commit (~10-30ms)
- **WALDurable**: Local WAL write only (~1-2ms)
- **Batched**: Batch aggregator queues and flushes

## Batch Aggregator (`pkg/raft/batch/aggregator.go`)
- Queue: 10,000 items max
- Batch size: 1,000 ops max
- Flush interval: 50ms
- Encodes as: [len1][data1][len2][data2]...

## Configuration Defaults
- RTTMillisecond: 200ms
- ElectionRTT: 10 (= 2s election timeout)
- HeartbeatRTT: 1 (= 200ms heartbeat)
- SnapshotEntries: 10,000

## Critical Rule (from CLAUDE.md memory)
**NEVER** pass `context.Background()` to any KVStore operation that may be Raft-backed. Always use `context.WithTimeout`.

## QuicRaft Library
- Located at: `vendor/github.com/jeremyhahn/go-quicraft/`
- Key type: `quicraft.Host` — manages Raft node host
- Key methods: `SyncPropose()`, `SyncRead()`, `StaleRead()`, `StartShard()`, `ReadIndex()`
- NotifyCommit enabled for WAL-durable write mode
