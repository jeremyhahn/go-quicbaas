# Multi-Tenant BaaS Architecture

QuicBaaS implements a multi-tenant Backend-as-a-Service (BaaS) architecture
where **tenant identity, isolation, barriers, DEKs, and engine-level quotas
are owned exclusively by qrdb's TenantServicer**. QuicBaaS adds a thin
BaaS extension layer: `TenantSettings` (MFA policy, session/token TTLs,
branding, named-networks, sandbox policy), IAM-subsystem quotas
(`MaxGroups`, `MaxPolicies`, `MaxApps`), and `pkg/orchestration` Projects.

## Ownership Boundary

| Capability | Owner | Notes |
|---|---|---|
| Tenant identity, name, timestamps | qrdb TenantServicer | authoritative |
| IsolationType, Entitlements, per-tenant barrier, DEK | qrdb TenantServicer | authoritative |
| Engine quotas (users, roles, storage, DBs, keys, certs, etc.) | qrdb TenantServicer | authoritative |
| Quota enforcement + usage tracking | qrdb TenantServicer | moved from BaaS in Phase D |
| Enable-encryption migrations | qrdb embedded node | via EncryptionMigrator |
| TenantSettings (MFA/session/branding/sandbox) | QuicBaaS IAM | BaaS product concern |
| IAM group/policy/app quotas | QuicBaaS IAM | BaaS concept, not an engine primitive |
| Federation/OIDC/MFA/groups/policies/apps | QuicBaaS IAM | BaaS-only, not in qrdb |
| Projects (workspace/git/agent) | QuicBaaS orchestration | BaaS-only |

QuicBaaS `CreateTenant` calls qrdb's `TenantServicer.CreateTenant` (which
handles identity + barrier + DEK + engine quotas) then runs provisioner hooks
and stores BaaS-only metadata. No duplicate barrier or isolation management
exists in QuicBaaS after Phase D.

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

Shard allocation is owned by qrdb's `DatabaseServicer`. The BaaS-local
`ShardAllocator` was removed in Phase D; shard IDs for new tenant databases
are assigned by qrdb's database creation API.

### Shard Classification

```go
IsPlatformShard(id) bool  // id == 1
IsReservedShard(id) bool  // id > 1 && id < 100
IsTenantShard(id)   bool  // id >= 100
```

## Tenant Isolation Modes

Each tenant has an `IsolationType` that determines the scope of isolation:

```go
type IsolationType int

const (
    IsolationKeyspace = iota  // Shared barrier, shared storage: namespace prefix only
    IsolationBarrier          // Per-tenant barrier, shared storage: DEK + namespace
    IsolationVolume           // Per-tenant barrier, volume directory: DEK + directory + namespace
    IsolationRaft             // Per-tenant barrier, dedicated Raft group: all three + quorum (clustered mode only)
)
```

Configuration at tenant creation:

```yaml
# Provisioner hooks qrdb's CreateTenant API
tenant = TenantServicer.CreateTenant({
    ID: "acme-corp",
    IsolationType: IsolationVolume,  // or Barrier/Keyspace/Raft
    Entitlements: {
        AllowedIsolationModes: ["barrier", "volume"],
        AllowedBarrierModes: ["none", "passphrase", "shamir"],
        AllowVolumeStorage: true,
        AllowDedicatedRaft: false,
        AllowSystemManagedBarrier: true,
    },
    Quotas: {
        MaxDatabases: 100,
        MaxKeys: 10000000,
        MaxStorageBytes: 10737418240,  # 10 GB
        // ... other quotas
    },
})
```

QuicBaaS provisioner invokes qrdb's `CreateTenant` (which materializes the barrier, DEK, and
storage according to `IsolationType`), then runs BaaS-layer hooks (IAM setup, TenantSettings, Projects).

## Tenant Entitlements

Every tenant has an `Entitlements` struct that gates which features and barrier modes are allowed:

| Entitlement | Type | Controlled feature |
|---|---|---|
| `AllowedIsolationModes` | []string | Which IsolationType values the tenant may use (e.g., `["barrier", "volume"]`) |
| `AllowedBarrierModes` | []string | Which barrier strategies the tenant may enable via enable-encryption (e.g., `["none", "passphrase", "shamir"]`) |
| `AllowDedicatedRaft` | bool | Permission to use `IsolationRaft` (requires clustered mode) |
| `AllowVolumeStorage` | bool | Permission to use `IsolationVolume` (separate storage directory per tenant) |
| `AllowSystemManagedBarrier` | bool | Permission to enable encryption without external HSM/KMS (uses system's managed envelope) |
| `AllowDedicatedWAL` | bool | Permission to allocate dedicated WAL groups (REST/gRPC only) |

Enable-encryption enforcement: When a tenant calls `POST /tenants/{id}/barrier/enable-encryption` with strategy `S`,
the service layer checks:

1. `EntitlementError` if `S not in Entitlements.AllowedBarrierModes`
2. `ErrSharedBarrierImmutable` if barrier is shared and `IsolationType != IsolationBarrier/Volume/Raft`
3. `QuotaViolation` if barrier encryption would exceed `SystemBarrierQuota`

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

## Vector Namespaces and Tenant Isolation

go-qrdb provides persistent distributed vector storage via tenant-partitioned
vector namespaces. When using vector operations through the qrdb SDK:

- **Namespace creation** must include tenant context to ensure vectors for one
  tenant cannot collide with or be accessed by another tenant.
- **Tenant isolation** of vectors is enforced by tenant-partitioned namespace
  keys in qrdb — tenant A cannot address, collide with, or read tenant B's
  namespaces.
- **Barrier encryption at rest**: qrdb encrypts the vector store under the
  cluster `StorageBarrier` (the same seal that protects KV/MVCC), not a
  per-tenant vector DEK. Confidentiality at rest requires the cluster barrier
  unsealed; cross-tenant isolation is the namespace partitioning above.
- **Multi-tenancy is not optional** — the vector namespace interface enforces
  tenant context, and quicbaas must pass the current tenant ID when creating or
  querying namespaces.

This is a locked architecture boundary: quicbaas does **not** maintain a
separate in-memory per-process vector index. Vector data is persisted and
encrypted within qrdb shards, leveraging the same multi-tenant isolation as
other KV operations.

See [Vector Database Architecture](vector.md) for the vector operations API
and [Embedding Provider Integration](../embedding/README.md) for how the SDK
embedding providers are consumed.

## Agent Session Log and State Encryption

Agent session logs (`pkg/orchestration/KVSessionEventLog`) and persisted agent
session records (`pkg/agent/Runtime`) are stored as **plaintext JSON inside
qrdb**. All at-rest confidentiality is delegated to **qrdb's `StorageBarrier`**,
which encrypts every byte written to the underlying store before it touches disk.

No application-level envelope DEK is applied on top of qrdb's barrier:

- The former `security.TenantDEKManager` KMS-wrapped per-tenant DEK (used to
  re-encrypt session events and session state before they were handed to qrdb)
  was removed in Phase 3. It was redundant for the disk-level threat
  (superseded by qrdb's barrier) and ineffective for the online-insider threat
  (the same process that served the data also unwrapped it).

- **Tenant-level crypto-shred and operator key isolation** — the controls that
  actually matter for multi-tenant confidentiality — are provided by configuring
  a **dedicated per-tenant qrdb barrier** sealed by any `go-xkms` hardware or
  cloud KMS strategy. This is an operator configuration choice, not hardcoded.

- **Per-key KMS authorization** (Phase 3) enforces fine-grained access control
  at the service layer, orthogonal to at-rest encryption.

## Intra-Tenant KMS Key Isolation

Within a single tenant, multiple KMS keys may exist for different purposes (e.g.,
a separate KEK per database, per service, or per classification level). Each key
is cryptographically isolated — only principals explicitly authorised for a given key
may perform cryptographic operations with it. This satisfies the compliance requirement:
*"intra-tenant multiple keys, each cryptographically isolated so only principals
authorized for a given key may perform operations with it."*

Authorization is enforced at the **service layer** (`cryptoService`/`kmsServiceImpl`),
not inside a transport-specific interceptor, so all transports — gRPC, REST, QUIC, MCP,
and the embedded SDK path — share the same gate uniformly.

### Key Model

```
Tenant
 └─ Barrier (tenant DEK, qrdb-owned)
     └─ Key A: KeyPolicy { AllowedPrincipals: ["alice"], AllowedOps: ["encrypt","decrypt"] }
     └─ Key B: KeyPolicy { AllowedPrincipals: ["service-account-payments"], AllowedOps: ["sign"] }
     └─ Key C: (no policy = default-allow any authenticated principal)
```

Policies are stored in the Raft-replicated KVStore under `kms:policy:<keyID>` so every
node enforces the same rules without TTL drift. An empty policy entry is treated as
"allow all" so existing keys are unaffected until an operator explicitly sets a policy.

### Subject Propagation

Authentication middleware at each transport boundary:
- validates the bearer token
- constructs an `interfaces.Subject` (ID, Username, Roles, TenantID)
- injects it into the request context via `auth.SubjectIntoContext`

The service layer extracts the Subject with `auth.SubjectFromContext` and evaluates it
against the key policy. Requests that arrive at the service layer without an authenticated
Subject are denied fail-closed unless `auth.IsSystemContext` returns true (for internal
daemon operations only; this mark cannot be forged by external callers).

### Key-Use Audit (AU-2)

Every cryptographic call records a key-use audit event (principal, keyID, operation,
allow/deny, timestamp) via `KMSKeyUseAuditLogger.LogKeyUse`. Both allow and deny
outcomes are captured, regardless of which transport delivered the request.

## Cross-References

- [KMS Security](../security/kms.md)
- [Shard ID Allocation](shard-ids.md)
- [Multi-Tenancy Keyspace](multi-tenancy.md)
- [Per-Database Encryption](../security/per-database-encryption.md)
- [Vector Database Architecture](vector.md)
- [Embedding Provider Integration](../embedding/README.md)
- [Database API Reference](../api/database.md)
- [Database CLI Reference](../cli/database.md)
