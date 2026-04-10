# QuicBaaS Shard ID Allocation

QuicBaaS uses Raft consensus for distributed operations. Each subsystem that requires consensus operates on its own Raft shard. This document defines the shard ID allocation strategy and reserved ranges.

## Shard Ranges

The BaaS multi-tenant architecture uses a simplified shard allocation scheme
managed by the central registry in `pkg/shards/`:

| Range | Constant | Purpose | Description |
|-------|----------|---------|-------------|
| 1 | `PlatformShardID` | Platform | Platform database (tenant registry, global config) |
| 2-99 | `ReservedMax = 100` | Reserved | Future platform services |
| 100+ | `TenantShardMin = 100` | Tenant Databases | Dynamically allocated tenant shards |

## Platform Shard (ID 1)

The platform shard hosts the platform database which stores:

- Tenant registry records (`platform:tenant:{tenantID}`)
- Shard allocation state
- Global platform configuration

Access to the platform shard requires the `db:admin` role, enforced by
`GuardPlatformShard` at gRPC and REST transport layers.

## Tenant Shards (100+)

Each tenant database (system and user) receives its own shard ID, allocated
monotonically by the `ShardAllocator` in `pkg/database/shard_allocator.go`.

Example allocation:

| Shard ID | Database | Purpose |
|----------|----------|---------|
| 1 | platform | Platform database |
| 101 | acme_system | Tenant "acme" system database |
| 102 | acme_orders | Tenant "acme" user database |
| 103 | acme_logs | Tenant "acme" user database |
| 104 | globex_system | Tenant "globex" system database |
| 105 | globex_app | Tenant "globex" user database |

## Shard Classification

```go
const (
    PlatformShardID uint64 = 1
    ReservedMax     uint64 = 100
    TenantShardMin  uint64 = 100
)

IsPlatformShard(id) bool  // id == 1
IsReservedShard(id) bool  // id > 1 && id < 100
IsTenantShard(id)   bool  // id >= 100
```

## Access Control

Two enforcement points prevent unauthorized access to the platform shard:

1. **gRPC interceptor** (`pkg/api/grpc/interceptor_shard.go`): Checks `GuardPlatformShard` on the shard ID from request metadata.
2. **REST middleware** (`pkg/api/rest/middleware_shard.go`): Same guard on the `X-Shard-ID` header.

```go
func GuardPlatformShard(id uint64, hasAdminRole bool) error
```

Returns `PlatformShardAccessDeniedError` when `id == PlatformShardID` and the caller lacks admin role.

## State Machine Types

Each shard uses a specific state machine type optimized for its workload:

| Type | Description | Used By |
|------|-------------|---------|
| `fast` | High-performance KV operations with binary protocol | Platform, tenant system databases |
| `vector` | Vector operations with HNSW index support | Vector engine databases |
| `timeseries` | Time series operations with msgpack encoding | TimeSeries engine databases |
| `rag` | RAG operations with document/chunk management | RAG engine databases |

## Best Practices

1. **Never use shard IDs 2-99 for tenant databases** -- These are reserved for future platform services.

2. **Use the ShardAllocator for tenant shards** -- Do not manually assign shard IDs; let the allocator assign them monotonically.

3. **Use consistent shard IDs across all nodes** -- All nodes in a cluster must use the same shard ID configuration.

4. **Monitor shard health** -- Each shard has its own Raft group with leader election and replication.

## API Reference

Shard ID allocation and cluster management are handled by the qrdb database engine, accessed via the qrdb SDK (`github.com/jeremyhahn/go-qrdb/sdk/go`). QuicBaaS configures shard IDs through the `pkg/shards/` registry, and the qrdb engine manages shard lifecycle, Raft consensus groups, and state machine assignments.

## Cross-References

- [Multi-Tenant BaaS Architecture](multi-tenant.md)
- [Shard Consolidation Design](../design/shard-consolidation.md)
