# Per-Database Encryption

QuicBaaS provides per-database encryption through a 3-tier key hierarchy.
Each tenant database receives its own Data Encryption Key (DEK), providing
cryptographic isolation between tenants and between databases within the
same tenant.

## Key Hierarchy

```
Master Key (KMS / HSM)
  |
  +-- Global DEK (dek:global)
  |     Used for platform-level encryption
  |
  +-- Tenant KEK (dek:tenant:acme)
  |     |
  |     +-- Database DEK (dek:tenant:acme:db:orders)
  |     +-- Database DEK (dek:tenant:acme:db:logs)
  |     +-- Database DEK (dek:tenant:acme:db:system)
  |
  +-- Tenant KEK (dek:tenant:globex)
        |
        +-- Database DEK (dek:tenant:globex:db:app)
        +-- Database DEK (dek:tenant:globex:db:system)
```

### Key Types

| Key | DEKID Format | Scope | Lifecycle |
|-----|-------------|-------|-----------|
| Master Key | Managed by KMS/HSM | Global | External rotation |
| Global DEK | `dek:global` | Platform | Initialized at startup |
| Tenant KEK | `dek:tenant:{tenantID}` | Per-tenant | Created/deleted with tenant |
| Database DEK | `dek:tenant:{tenantID}:db:{dbName}` | Per-database | Created/deleted with database |

## DEK ID Format

DEK IDs follow a hierarchical format defined in `pkg/secrets/dekid.go`:

```
dek:global                           -- Global DEK
dek:tenant:{tenantID}                -- Tenant KEK
dek:tenant:{tenantID}:db:{dbName}    -- Database DEK
```

### Helper Functions

```go
// Create DEK IDs
secrets.DEKIDGlobal                           // "dek:global"
secrets.NewTenantDEKID("acme")                // "dek:tenant:acme"
secrets.DatabaseDEKID("acme", "orders")       // "dek:tenant:acme:db:orders"

// Parse DEK IDs
secrets.ParseDEKID("dek:tenant:acme:db:orders")
secrets.ParseDatabaseDEKID("dek:tenant:acme:db:orders") // returns ("acme", "orders", nil)

// Classify DEK IDs
secrets.IsGlobal(dekid)    // true for "dek:global"
secrets.IsTenant(dekid)    // true for "dek:tenant:acme" (not database-scoped)
secrets.IsDatabase(dekid)  // true for "dek:tenant:acme:db:orders"

// Extract components
secrets.GetTenantID(dekid)      // ("acme", true)
secrets.GetDatabaseInfo(dekid)  // ("acme", "orders", true)
```

## EnsureDatabaseDEK

`EnsureDatabaseDEK` creates or retrieves the DEK for a tenant's database.
The method is **idempotent** -- calling it multiple times for the same
tenant/database combination returns the existing DEK without error.

```go
dekid, err := secretsDEK.EnsureDatabaseDEK(ctx, "acme", "orders")
// dekid = "dek:tenant:acme:db:orders"
```

### What it does:

1. Validates tenant ID and database name (non-empty after trimming)
2. Computes the storage path: `_internal/secrets/dek/database/{tenantID}/{dbName}`
3. Checks if a DEK already exists at that path
4. If not, generates a new 256-bit AES key via the KMS
5. Wraps the key with the master key and stores the `DEKEntry`
6. Returns the `DEKID`

### Called during:

- Database creation (`DatabaseService.CreateDatabase`)
- Tenant provisioning (for the system database)

## DeleteDatabaseDEK

`DeleteDatabaseDEK` removes the DEK for a tenant's database. Called when
a database is dropped.

```go
err := secretsDEK.DeleteDatabaseDEK(ctx, "acme", "orders")
```

### What it does:

1. Validates tenant ID and database name
2. Deletes the DEK entry from storage
3. Clears the DEK from the in-memory cache
4. Logs the deletion

### Called during:

- Database drop (`DatabaseService.DropDatabase`)
- Tenant deprovisioning (for all tenant databases)

## Barrier Seal/Unseal Lifecycle

Databases with barrier encryption enabled follow this lifecycle:

### 1. Create (Sealed)

```
CreateDatabase("orders", barrier=true)
  |
  +-- EnsureDatabaseDEK("acme", "orders")
  |     Creates DEK: dek:tenant:acme:db:orders
  |     Wraps DEK with master key
  |     Stores at _internal/secrets/dek/database/acme/orders
  |
  +-- Create database with barrier config
  |     Database starts in SEALED state
  |
  +-- Unseal barrier
        Unwraps DEK using master key
        Database transitions to OPEN state
```

### 2. Operational (Unsealed)

While unsealed, the unwrapped DEK is cached in memory. All read/write
operations encrypt/decrypt data transparently using the database DEK.

The DEK cache has a configurable TTL (default 5 minutes) and maximum
size (default 100 entries). Expired entries are cleaned up automatically.

### 3. Seal

```
SealDatabase("orders")
  |
  +-- Wipe unwrapped DEK from memory cache
  +-- Database transitions to SEALED state
  +-- All read/write operations return errors
```

### 4. Unseal

```
UnsealDatabase("orders", masterKey)
  |
  +-- Load wrapped DEK from storage
  +-- Unwrap DEK using master key
  +-- Cache unwrapped DEK in memory
  +-- Database transitions to OPEN state
```

### 5. Drop

```
DropDatabase("orders")
  |
  +-- Close database
  +-- DeleteDatabaseDEK("acme", "orders")
  |     Removes DEK from storage and cache
  |
  +-- Delete all data files
```

## Storage Layout

DEKs are stored in the platform shard (shard 1) under internal key paths:

```
_internal/secrets/dek/
  global                          -- Global DEK entry
  tenant/
    acme                          -- Tenant KEK for "acme"
    globex                        -- Tenant KEK for "globex"
  database/
    acme/
      orders                      -- Database DEK for acme_orders
      logs                        -- Database DEK for acme_logs
      system                      -- Database DEK for acme_system
    globex/
      app                         -- Database DEK for globex_app
  versions/
    dek/global/v1                 -- Global DEK version 1
    dek/tenant/acme/v1            -- Tenant KEK version 1
    dek/tenant/acme/db/orders/v1  -- Database DEK version 1
```

## DEK Entry Format

Each DEK is stored as a JSON-encoded `DEKEntry`:

```json
{
  "dekid": "dek:tenant:acme:db:orders",
  "wrapped_key": "<base64-encoded-wrapped-key>",
  "algorithm": "AES-256-GCM",
  "version": 1,
  "created_at": "2026-03-10T14:30:00Z",
  "last_rotated_at": "2026-03-10T14:30:00Z"
}
```

## Cryptographic Properties

- **Algorithm**: AES-256-GCM (default, 32-byte key)
- **Key wrapping**: DEKs are wrapped (encrypted) with the master key via KMS
- **Memory safety**: Plaintext key material is zeroed after use
- **Cache isolation**: Each DEK is cached independently with expiry
- **Version retention**: Old DEK versions are retained for decrypting
  historical data (default: 3 versions)

## Agent Session Logs and Session State

Agent session logs (stored in `pkg/orchestration/KVSessionEventLog`) and
persisted agent session state (stored by `pkg/agent/Runtime`) are written as
**plaintext JSON into qrdb**. At-rest confidentiality is provided entirely by
**qrdb's `StorageBarrier`**, which transparently encrypts every value written
to the underlying PebbleDB store before it reaches disk.

There is **no application-level envelope DEK** wrapping these payloads:

- A per-tenant application-layer DEK (`security.TenantDEKManager`) was
  previously applied as a second encryption pass before qrdb ingested the data.
  A compliance audit established that this was a redundant envelope: it was
  ineffective against the online-insider threat model (the same process unwraps
  it) and fully superseded by qrdb's at-rest barrier for the disk-level threat.
  It was removed in the Phase 3 security hardening.

- **Tenant-level cryptographic isolation** (crypto-shred on tenant deletion,
  operator key isolation) is achieved by configuring a dedicated per-tenant
  qrdb barrier sealed by any `go-xkms` hardware or cloud strategy. This is an
  operator configuration choice — the data plane barrier routing is not
  hardcoded. See [qrdb barrier documentation](../architecture/barrier.md).

- **Per-key KMS authorization** for finer-grained access control is enforced
  by the KMS authorization layer (Phase 3 control). This is orthogonal to
  at-rest encryption.

## Cross-References

- [Multi-Tenant Architecture](../architecture/multi-tenant.md)
- [Database API Reference](../api/database.md)
- [Cluster DEK Management](cluster-dek.md)
- [KMS Multi-Tenancy](kms-multi-tenancy.md)
