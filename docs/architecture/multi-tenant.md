# Multi-Tenant BaaS Architecture

QuicBaaS implements a 3-tier multi-tenant Backend-as-a-Service (BaaS)
architecture that provides strong isolation between tenants through
dedicated databases, per-database encryption, and platform-level access
control.

## 3-Tier Model

```
+---------------------------------------------------------------+
|                     Platform Tier (Shard 1)                    |
|  - Tenant registry (platform:tenant:{id})                     |
|  - Shard allocation state                                     |
|  - Global configuration                                       |
+---------------------------------------------------------------+
         |                    |                    |
         v                    v                    v
+------------------+  +------------------+  +------------------+
| Tenant Tier      |  | Tenant Tier      |  | Tenant Tier      |
| acme_system      |  | globex_system    |  | initech_system   |
| (shard 101)      |  | (shard 102)      |  | (shard 103)      |
| - IAM            |  | - IAM            |  | - IAM            |
| - KMS            |  | - KMS            |  | - KMS            |
| - Secrets        |  | - Secrets        |  | - Secrets        |
| - DB records     |  | - DB records     |  | - DB records     |
+------------------+  +------------------+  +------------------+
    |         |            |         |            |
    v         v            v         v            v
+------+ +------+    +------+ +------+    +------+
|acme_ | |acme_ |    |globex| |globex|    |init_ |
|orders| |logs  |    |_data | |_logs |    |app   |
|(104) | |(105) |    |(106) | |(107) |    |(108) |
+------+ +------+    +------+ +------+    +------+
  User Databases       User Databases     User DBs
```

### Platform Tier

The platform database runs on **shard 1** (`PlatformShardID`). It stores:

- Tenant registry records (`platform:tenant:{tenantID}`)
- Shard allocation counter (highest allocated shard ID)
- Global platform configuration

Access to the platform shard requires the `db:admin` role. The
`GuardPlatformShard` function in `pkg/shards/access.go` enforces this at
all transport entry points (gRPC interceptor, REST middleware).

### Tenant Tier

Each tenant receives a **system database** named `{tenantID}_system`. This
database hosts the tenant's internal subsystems:

- IAM (users, roles, groups, policies)
- KMS (tenant-scoped encryption keys)
- Secrets engine
- Database records (`sys:db:{dbName}`)

The system database is created automatically when a tenant is provisioned
via the `TenantProvisioner` interface.

### User Database Tier

Tenants create user databases through the database management API. Each
user database:

- Is named `{tenantID}_{dbName}` (e.g., `acme_orders`)
- Receives its own dedicated Raft shard
- Gets a per-database DEK for barrier encryption
- Is tracked by a `DatabaseRecord` in the tenant's system database

## Shard Allocation

Shard IDs are allocated from a central registry in `pkg/shards/`:

| Range | Purpose | Description |
|-------|---------|-------------|
| 1 | Platform | Platform database (`PlatformShardID`) |
| 2-99 | Reserved | Future platform services (`ReservedMax = 100`) |
| 100+ | Tenant | Tenant databases (`TenantShardMin = 100`) |

The `ShardAllocator` in `pkg/database/shard_allocator.go` provides
lock-free, monotonically increasing shard ID allocation using
`atomic.Uint64`. On startup, the allocator is initialized from the highest
shard ID found across existing databases.

```go
allocator, _ := NewShardAllocator(100) // starts at TenantShardMin
shardID := allocator.Allocate()        // returns 101, 102, ...
```

### Shard Classification

```go
IsPlatformShard(id) bool  // id == 1
IsReservedShard(id) bool  // id > 1 && id < 100
IsTenantShard(id)   bool  // id >= 100
```

## Database Naming

All tenant database names follow the pattern `{tenantID}_{dbName}`:

```go
SystemDatabaseName("acme")           // "acme_system"
TenantDatabaseName("acme", "orders") // "acme_orders"
```

The naming convention provides namespace isolation while keeping database
names human-readable and queryable.

## Tenant Provisioning Lifecycle

### Create Tenant

1. Platform validates the tenant ID and stores a tenant record in the
   platform database (`platform:tenant:{tenantID}`)
2. `TenantProvisioner.ProvisionTenant` is called:
   a. Allocates a shard ID for the system database
   b. Creates the system database (`{tenantID}_system`) via qrdb
   c. Provisions a tenant KEK (`EnsureTenantDEK`)
   d. Bootstraps IAM, KMS, and secrets subsystems

### Delete Tenant

1. `TenantProvisioner.DeprovisionTenant` is called:
   a. Lists all databases owned by the tenant
   b. Drops each user database (including DEK cleanup)
   c. Drops the system database
   d. Deletes the tenant KEK (`DeleteTenantDEK`)
2. Platform removes the tenant record from the platform database

## Per-Database Encryption

Each database has its own Data Encryption Key (DEK), forming a 3-tier
key hierarchy:

```
Master Key (KMS / HSM)
  |
  +-- Tenant KEK (dek:tenant:{tenantID})
  |     |
  |     +-- Database DEK (dek:tenant:{tenantID}:db:{dbName})
  |     +-- Database DEK (dek:tenant:{tenantID}:db:{dbName2})
  |
  +-- Tenant KEK (dek:tenant:{tenantID2})
        |
        +-- Database DEK (dek:tenant:{tenantID2}:db:{dbName})
```

See [Per-Database Encryption](../security/per-database-encryption.md) for
full details on DEK management and the barrier seal/unseal lifecycle.

## Platform Shard Access Control

Two enforcement points prevent unauthorized access to the platform shard:

1. **gRPC interceptor** (`pkg/api/grpc/interceptor_shard.go`): Checks
   `GuardPlatformShard` on the shard ID extracted from request metadata.
   Returns `PermissionDenied` for non-admin callers.

2. **REST middleware** (`pkg/api/rest/middleware_shard.go`): Same guard on
   the `X-Shard-ID` header. Returns HTTP 403 for non-admin callers.

Internal operations (tenant provisioning, lifecycle recovery) bypass the
guard by using `shards.PlatformShardID` directly from server-side code.

## Startup Recovery

On server startup, `lifecycle_databases.go` recovers the database state:

1. Opens the platform database (shard 1)
2. Scans for tenant records
3. For each tenant, opens the system database and all user databases
4. Rebuilds the shard allocator counter from the highest observed shard ID

This ensures all tenant databases are available after a restart without
requiring re-provisioning.

## Configuration

```yaml
databases:
  platform_shard_id: 1       # default, rarely changed
  tenant_shard_min: 100      # minimum shard ID for tenant databases
```

## Cross-References

- [Shard ID Allocation](shard-ids.md)
- [Multi-Tenancy Keyspace](multi-tenancy.md)
- [Per-Database Encryption](../security/per-database-encryption.md)
- [Database API Reference](../api/database.md)
- [Database CLI Reference](../cli/database.md)
