# Shard Consolidation Plan (2026-03-14)

## Status: APPROVED FOR IMPLEMENTATION

## New Shard ID Scheme (contiguous, 1-9 reserved for system)

| ID | Constant | Purpose | Replaces |
|----|----------|---------|----------|
| 1  | SystemShardID | IAM, scheduler, param store, admin ops, health | old catchall shard-1 |
| 2  | VectorShardID | Vector embeddings | old 2 and conflicting 200 |
| 3  | TimeSeriesShardID | Time series data | old 3 and conflicting 1 in timeseries_raft.go |
| 4  | KMSShardID | KMS/DEK key material | old 100 and conflicting 1000 |
| 5  | SecretsShardID | Secrets engine | old 1001 |
| 6-9 | (reserved) | Future internal subsystems | — |
| 10+ | DefaultDataShardID | User/tenant KV data shards | — |

## Key Decisions
- Central registry: pkg/shards/ (no project imports, bottom of dep graph)
- System shard protected by GuardSystemShard at transport layer (admin-only)
- All `if shardID == 0 { shardID = 1 }` replaced with shards.DefaultDataShardID
- On-disk migration: old shard-1 (KV data) renamed to shard-10, system shard starts fresh
- IAM data re-bootstrapped on first startup (already handled by iam_init.go)
- Multi-tenancy: tenant shard mappings stored in system shard under __sys:tenant-shard:{tenantID}
