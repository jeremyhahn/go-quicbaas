# Shard Consolidation Design

> **Status: REFERENCE** · Last verified: 2026-04-10
> Shard consolidation design.

## Problem Statement

Shard IDs are scattered across 7+ files with no central registry, three direct conflicts,
zero multi-tenancy at the shard layer, and non-contiguous numbering (1, 2, 3, 100, 200, 1000, 1001).
Nine occurrences of `if shardID == 0 { shardID = 1 }` dump all unrouted data into a single
catchall shard that also hosts IAM, scheduler, parameter store, and admin operations.

### Current Shard Map (Before)

| ID   | Purpose                    | Defined In                         | Conflict           |
|------|----------------------------|------------------------------------|---------------------|
| 1    | KV catchall + IAM + scheduler + params + admin | 20+ hardcoded sites   | Overloaded          |
| 2    | Vector storage             | `pkg/service/vector.go:37`         | Conflicts with 200  |
| 3    | Time series                | `pkg/config/timeseries_config.go`  | Conflicts with 1 in `timeseries_raft.go` |
| 100  | KMS (lifecycle)            | `pkg/server/lifecycle.go:70`       | Conflicts with 1000 |
| 200  | Vector (config default)    | `pkg/config/vector_config.go:51`   | Conflicts with 2    |
| 1000 | KMS/DEK cluster            | `pkg/secrets/cluster.go:54`        | Conflicts with 100  |
| 1001 | Secrets storage            | `pkg/secrets/cluster.go:57`        | —                   |

## New Shard ID Scheme

### Design Principles

- **Reserved band 1-9**: System-managed shards. Fixed, immutable, not user-configurable.
- **User data shards start at 10**: All KV data shards created by operators or for tenants.
- **No gaps in system band**: 1 through 5 assigned; 6-9 reserved for future subsystems.
- **Contiguous numbering**: No skipping from 3 to 100.

### System Shard Allocations

| ID | Constant            | Purpose                          | Replaces                                    |
|----|---------------------|----------------------------------|---------------------------------------------|
| 1  | `SystemShardID`     | IAM, scheduler, param store, admin ops, health | Old catchall shard-1               |
| 2  | `VectorShardID`     | Vector embedding storage         | Old 2 (service) and 200 (config)            |
| 3  | `TimeSeriesShardID` | Time series data                 | Old 3 (config) and 1 (timeseries_raft.go)   |
| 4  | `KMSShardID`        | KMS / DEK key material           | Old 100 (lifecycle) and 1000 (secrets)      |
| 5  | `SecretsShardID`    | Secrets engine storage           | Old 1001 (secrets/cluster.go)               |
| 6-9| _(reserved)_        | Future internal subsystems       | —                                           |

### User Data Shard Range

| Range     | Purpose                                                              |
|-----------|----------------------------------------------------------------------|
| 10-10000  | Operator-configured KV data shards. Default first data shard is 10.  |
| 10001+    | Dynamically allocated tenant shards (future).                        |

### Conflict Resolution

- **Vector 2 vs 200**: Keep 2. The service constant `VectorShardID = 2` is canonical (30+ call sites).
  Config default 200 was wrong.
- **KMS 100 vs 1000**: Both wrong. New value 4 is clean and contiguous. Both had limited call sites.
- **TimeSeries 1 vs 3**: Keep 3. Config value is correct. `timeseries_raft.go` hardcode of 1 was a bug
  routing timeseries writes to the general KV shard.

## Central Shard Registry Package

### Location: `pkg/shards/`

```
pkg/shards/
  shards.go       # Constants, ShardMeta, Registry
  access.go       # AccessTier, GuardSystemShard
  errors.go       # Typed errors
  shards_test.go  # Tests
```

The package sits at the bottom of the dependency graph. It imports nothing from the project.

### Constants

```go
const (
    SystemShardID     uint64 = 1
    VectorShardID     uint64 = 2
    TimeSeriesShardID uint64 = 3
    KMSShardID        uint64 = 4
    SecretsShardID    uint64 = 5

    SystemShardMax     uint64 = 10  // Exclusive upper bound of reserved band
    DefaultDataShardID uint64 = 10  // First user-defined KV data shard
)
```

### Registry API

```go
type AccessTier uint8

const (
    TierSystem AccessTier = iota // Internal system components only
    TierAdmin                    // Database admin role
    TierTenant                   // Any authenticated tenant
)

type ShardMeta struct {
    ID          uint64
    Name        string       // "_system", "vector", "timeseries", "kms", "secrets"
    Description string
    Tier        AccessTier
    System      bool         // True for ID < SystemShardMax
}

type Registry struct { entries map[uint64]ShardMeta }

var Global = buildRegistry()

func (r *Registry) Lookup(id uint64) (ShardMeta, bool)
func (r *Registry) IsSystem(id uint64) bool
func (r *Registry) ValidateUserShard(id uint64) error
func (r *Registry) All() []ShardMeta
```

### Access Guard

```go
func GuardSystemShard(id uint64, hasAdminRole bool) error
```

Returns `SystemShardAccessError` if `id < SystemShardMax` and `hasAdminRole` is false.

## System Shard Design

### What Lives in System Shard (ID 1)

All keys prefixed with `__sys:` via the existing `KeyspaceResolver`.

| Subsystem              | Key Prefix Pattern                          |
|------------------------|---------------------------------------------|
| IAM (per-tenant)       | `__sys:iam:{tenantID}:{entity}:{id}`        |
| Scheduler jobs         | `__sys:scheduler:{jobID}`                   |
| Parameter store        | `__sys:params:{name}`                       |
| Admin/cluster config   | `__sys:cluster:{key}`                       |
| Health sentinel        | `__sys:health:sentinel`                     |
| Tenant-shard mappings  | `__sys:tenant-shard:{tenantID}`             |

### Access Control

Two enforcement points:

1. **gRPC interceptors** (`pkg/api/grpc/server.go`): Check `GuardSystemShard` before forwarding
   any client-supplied shard ID in the reserved band.
2. **REST middleware** (`pkg/api/rest/middleware.go`): Same guard on `X-Shard-ID` header.

Internal calls (IAM init, scheduler, param store) bypass the guard because they reference
`shards.SystemShardID` directly, never from external input.

## Multi-Tenancy Model

### Key Insight

The existing `KeyspaceResolver` already handles key-prefix-based tenant isolation at the data level.
The shard layer needs to become tenant-aware for shard routing.

### Single-Tenant (embedded/standalone)

A single data shard at `DefaultDataShardID = 10` holds all user data. Keyspace resolver uses
legacy key format (no `__t:` prefix).

### Multi-Tenant (clustered)

Each tenant is assigned one or more data shards. Mapping stored in system shard:

```
__sys:tenant-shard:{tenantID} = [10, 11, ...]
```

Tenant data keys are prefixed: `__t:{tenantID}:{entityType}:{id}`

The `if shardID == 0` fallback becomes:

```go
if shardID == 0 {
    shardID = shards.DefaultDataShardID
}
```

## Migration Strategy

### Phase 1: Create Registry (no behavior change)

- Create `pkg/shards/` package with constants, registry, guard, errors, tests.
- Add `LegacyToNew` compatibility map for old-to-new shard ID remapping.
- All existing constants remain in original files. No call sites change.

### Phase 2: Replace Constants (compile-time safety)

| File                                          | Remove                          | Replace With                 |
|-----------------------------------------------|---------------------------------|------------------------------|
| `pkg/server/lifecycle.go:64`                  | `defaultVectorShardID = 2`      | `shards.VectorShardID`       |
| `pkg/server/lifecycle.go:67`                  | `defaultTimeSeriesShardID = 3`  | `shards.TimeSeriesShardID`   |
| `pkg/server/lifecycle.go:70`                  | `defaultKMSShardID = 100`       | `shards.KMSShardID`          |
| `pkg/server/iam_init.go:41`                   | `iamShardID = 1`                | `shards.SystemShardID`       |
| `pkg/service/vector.go:37`                    | `VectorShardID = 2`             | `shards.VectorShardID`       |
| `pkg/service/timeseries_raft.go:57`           | `defaultTimeSeriesShardID = 1`  | `shards.TimeSeriesShardID`   |
| `pkg/config/timeseries_config.go:25`          | `DefaultTimeSeriesShardID = 3`  | `shards.TimeSeriesShardID`   |
| `pkg/config/vector_config.go:51`              | `ShardID: 200`                  | `shards.VectorShardID`       |
| `pkg/config/kms.go` (default 1000)            | `ShardID: 1000`                 | `shards.KMSShardID`          |
| `pkg/secrets/cluster.go:54`                   | `DefaultKMSShardID = 1000`      | `shards.KMSShardID`          |
| `pkg/secrets/cluster.go:57`                   | `DefaultSecretsShardID = 1001`  | `shards.SecretsShardID`      |
| `pkg/routing/router.go:47,55,72,90`           | `[]uint64{1}`                   | `[]uint64{shards.DefaultDataShardID}` |
| `pkg/api/grpc/server_batch_stream.go:264`     | `header.ShardId = 1`            | `shards.DefaultDataShardID`  |
| `cmd/quicbaasd/main.go:212`                   | `viper.SetDefault("vector.shard_id", 200)` | `shards.VectorShardID` |
| `cmd/quicbaasd/main.go:217`                   | `viper.SetDefault("timeseries.shard_id", ...)` | `shards.TimeSeriesShardID` |

### Phase 3: Replace `if shardID == 0` Fallbacks

In `pkg/service/quicbaas.go`, 9 occurrences at lines 686-687, 806-807, 927-928, 1017-1018,
1308-1309, 1476-1477, 1650-1651, 1732-1733, 2016-2017:

```go
// Before:
if shardID == 0 { shardID = 1 }

// After:
if shardID == 0 { shardID = shards.DefaultDataShardID }
```

Hardcoded system shard references:

| Location                    | Old           | New                       |
|-----------------------------|---------------|---------------------------|
| `quicbaas.go:429` (scheduler) | `kvStoreProvider(1)` | `kvStoreProvider(shards.SystemShardID)` |
| `quicbaas.go:554` (params)    | `newKVParameterManager(..., 1, ...)` | `..., shards.SystemShardID, ...` |
| `quicbaas.go:2134` (admin)    | `qrdbForShard(1)` | `qrdbForShard(shards.SystemShardID)` |
| `quicbaas.go:3470` (health)   | `[]uint64{1}` | `[]uint64{shards.DefaultDataShardID}` |
| `quicbaas.go:3518` (entity)   | `shardID := uint64(1)` | `shardID := shards.DefaultDataShardID` |
| `quicbaas.go:3680` (entity)   | `shardID := uint64(1)` | `shardID := shards.DefaultDataShardID` |

### Phase 4: On-Disk Directory Migration

Executed once at server startup, guarded by marker file `{dataDir}/.shard-migration-v1-complete`.

| Old Directory | New Directory | Rationale |
|---------------|---------------|-----------|
| `shard-1/`    | `shard-10/`   | Old catchall KV data becomes first data shard |
| `shard-100/`  | `shard-4/`    | Old KMS lifecycle shard |
| `shard-200/`  | `shard-2/`    | Old vector config default |
| `shard-1000/` | `shard-4/`    | Old KMS secrets shard (merge with 100 → 4) |
| `shard-1001/` | `shard-5/`    | Old secrets shard |

System shard `shard-1/` starts fresh (empty). IAM data is re-bootstrapped on startup
(already handled by `iam_init.go`). Scheduler/parameter data reconstructed from application state.

### Phase 5: Add System Shard Guard

- `pkg/api/grpc/server.go` — unary interceptor chain
- `pkg/api/rest/middleware.go` — before routing KV requests
- `pkg/api/quic/handler.go` — before dispatching KV commands

### Phase 6: Validation and Cleanup

- Remove `LegacyToNew` map after one release cycle.
- Update `kvShards` to filter using `shards.IsSystem()`.
- Enrich metrics/logs: `shard_name` label from `shards.Global.Lookup(id).Name`.

## Observability

Every `ShardMeta` carries a `Name` field. Metrics labels, log fields, and trace spans that
currently emit `shard_id=1` should be enriched to `shard_name=_system`. This gives operators
human-readable dashboard labels instead of opaque integers.

The migration function emits structured INFO-level logs for each directory rename and writes
a JSON manifest to `{dataDir}/.shard-migration-v1-manifest.json`.

## Security

### Threat Vectors

1. **Tenant accessing system shard directly** — Mitigated by `GuardSystemShard` at all transport entry points.
2. **Tenant constructing `__sys:` prefixed key** — Mitigated by `KeyspaceResolver.ValidateEntityType` (blocks `__` prefix).
3. **KMS shard direct key exfiltration** — Same system shard guard; KMS operations only via KMS service layer.
4. **Shard ID overflow** — `ValidateUserShard` rejects IDs 1-9 from external configuration.

### What db:admin Can Do With System Shard Access

- Read/modify IAM records across tenants (account recovery, audit)
- Inspect scheduler state
- Read/write parameter store entries
- Access cluster topology

All access subject to audit logging via `security.AuditLogger`.
