# Object Storage Architecture

## Overview

QuicBaaS Object Storage provides a distributed object storage layer built on the Raft-replicated state machine. Objects are stored across multiple backends while the catalog maintains a unified view of all object locations.

## Core Components

```
┌─────────────────────────────────────────────────────────────────────┐
│                       Object Storage System                         │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                    ObjectStorageService                       │  │
│  │  - Request validation and authorization                       │  │
│  │  - Config resolution (cluster → node → tenant)                │  │
│  │  - Encryption coordination with KMS                           │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                │                                    │
│                                ▼                                    │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │              EnhancedObjectStoreStateMachine                  │  │
│  │  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐  │  │
│  │  │ CatalogManager  │ │ BackendManager  │ │ ConfigResolver  │  │  │
│  │  │                 │ │                 │ │                 │  │  │
│  │  │ - Entry CRUD    │ │ - Multi-backend │ │ - 3-layer config│  │  │
│  │  │ - Location mgmt │ │ - Health check  │ │ - Policy merge  │  │  │
│  │  │ - Indexes       │ │ - Selection     │ │ - Quota check   │  │  │
│  │  │ - Work queues   │ │ - Failover      │ │                 │  │  │
│  │  └─────────────────┘ └─────────────────┘ └─────────────────┘  │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                │                                    │
│  ┌─────────────────────────────┴─────────────────────────────────┐  │
│  │                     Background Workers                        │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │  │
│  │  │ Replication  │  │   Archive    │  │    Verify    │         │  │
│  │  │    Worker    │  │    Worker    │  │    Worker    │         │  │
│  │  └──────────────┘  └──────────────┘  └──────────────┘         │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                │                                    │
└────────────────────────────────┼────────────────────────────────────┘
                                 │
            ┌────────────────────┼────────────────────┐
            ▼                    ▼                    ▼
      ┌──────────┐         ┌──────────┐         ┌──────────┐
      │    S3    │         │   GCS    │         │  Local   │
      │ Backend  │         │ Backend  │         │ Backend  │
      └──────────┘         └──────────┘         └──────────┘
```

## Catalog Manager

The Catalog Manager is the source of truth for all object metadata and locations.

### Catalog Entry Schema

```go
type CatalogEntry struct {
    // === IDENTITY (immutable) ===
    Key       string   // Logical key: "namespace:path"
    ObjectID  uint64   // Internal unique ID (snowflake)

    // === CONTENT INFO ===
    Checksum    []byte   // SHA-256 for integrity/dedup
    Size        int64    // Original uncompressed size
    ContentType string   // MIME type

    // === LOCATION TRACKING ===
    PrimaryStore  StoreLocation    // Current primary backend
    Replicas      []StoreLocation  // Additional copies
    ArchiveStore  *StoreLocation   // Cold storage copy

    // === POLICIES ===
    ReplicationPolicy *ReplicationPolicy
    ArchivePolicy     *ArchivePolicy
    RetentionPolicy   *RetentionPolicy

    // === ENCRYPTION ===
    Encryption *ObjectEncryption

    // === MULTI-TENANCY ===
    TenantID  string
    ShardID   uint64
    Owner     string

    // === ACCESS CONTROL ===
    ACL *ObjectACL

    // === METADATA ===
    Tags       map[string]string
    Labels     map[string]string
    CustomMeta map[string]string

    // === TIMESTAMPS ===
    CreatedAt    int64
    ModifiedAt   int64
    LastAccessAt int64
    ArchivedAt   int64

    // === STATUS ===
    Status  EntryStatus  // active, pending, archived, deleted
    Version uint64       // Optimistic concurrency
}

type StoreLocation struct {
    Store       string   // Backend name: "s3-primary"
    ObjectRef   string   // Path within backend
    Status      string   // "active", "syncing", "failed"
    Checksum    []byte   // Per-location checksum
    StoredSize  int64    // May differ if compressed
    Compressed  bool
    Encryption  *LocationEncryption
    SyncedAt    int64
}
```

### Storage Key Prefixes

```
# Primary catalog entries
cat:{namespace}:{path}              → CatalogEntry (msgpack)

# Secondary indexes
idx:store:{store}:{key}             → nil (objects by backend)
idx:tenant:{tenant}:{key}           → nil (objects by tenant)
idx:checksum:{hex}:{key}            → nil (deduplication)
idx:status:{status}:{key}           → nil (objects by status)
idx:tag:{tag}:{value}:{key}         → nil (objects by tag)
idx:encryption:{keyid}:{key}        → nil (objects by encryption key)

# Async work queues
queue:replicate:{priority}:{ts}:{key}  → ReplicationTask
queue:archive:{ts}:{key}               → ArchiveTask
queue:verify:{ts}:{key}                → VerifyTask
queue:delete:{ts}:{key}                → DeleteTask
queue:reencrypt:{ts}:{key}             → ReencryptTask

# Statistics
stats:tenant:{tenant}               → TenantUsageStats
stats:store:{store}                 → StoreUsageStats
```

## Backend Manager

Manages multiple storage backends with health monitoring and intelligent selection.

### Backend Interface

```go
type BackendManager interface {
    // Access
    GetBackend(name string) (common.Storage, error)
    ListBackends() []string
    GetBackendConfig(name string) (*BackendConfig, error)

    // Dynamic registration
    RegisterBackend(name string, cfg *BackendConfig) error
    UnregisterBackend(name string) error

    // Selection
    SelectBackend(criteria *SelectionCriteria) (string, common.Storage, error)

    // Health
    CheckHealth(ctx context.Context, name string) (*BackendHealth, error)
    CheckAllHealth(ctx context.Context) map[string]*BackendHealth

    Close() error
}

type SelectionCriteria struct {
    PreferredBackend string
    TenantID         string
    ObjectSize       int64
    Tier             StorageTier  // hot, warm, cold
    RequireLocal     bool
}
```

### Backend Selection Flow

```
1. Check explicit request (TargetBackend in command)
       │
       ▼
2. Check tenant configuration (tenant.primary_backend)
       │
       ▼
3. Check shard configuration (shard.primary_backend)
       │
       ▼
4. Use cluster default (defaults.primary_backend)
       │
       ▼
5. Filter by enabled backends on this node
       │
       ▼
6. Select based on criteria (size, tier, locality)
```

## Command Protocol

### V1 Commands (Backward Compatible)

```go
const (
    OpPut    byte = 0x01
    OpDelete byte = 0x02
    OpBatch  byte = 0x03
)
```

### V2 Commands (Enhanced)

```go
const (
    OpPutV2     byte = 0x11  // Extended PUT with policies
    OpDeleteV2  byte = 0x12  // Extended DELETE
    OpReplicate byte = 0x13  // Add replica
    OpMigrate   byte = 0x14  // Move primary
    OpArchive   byte = 0x15  // Move to archive
    OpSetPolicy byte = 0x16  // Update policies
    OpVerify    byte = 0x17  // Verify integrity
)

type CommandV2 struct {
    Version    byte     // 0x02
    Op         byte
    Flags      uint16

    Key         string
    Value       []byte
    ContentType string

    ReplicationPolicy *ReplicationPolicy
    ArchivePolicy     *ArchivePolicy

    TenantID      string
    ShardID       uint64
    TargetBackend string

    Tags map[string]string
}
```

### Command Detection

```go
func detectCommandVersion(data []byte) byte {
    if len(data) == 0 {
        return 1
    }
    if data[0] == 0x02 {
        return 2
    }
    return 1
}
```

## Data Flow

### PUT Operation

```
1. Client: Put("tenant-acme:docs/report.pdf", data)
       │
       ▼
2. Service Layer:
   - Authenticate/authorize request
   - Resolve config (cluster → node → tenant)
   - Check quotas
       │
       ▼
3. FSM (via Raft):
   - Select primary backend
   - Compute checksum
   - Compress if configured
   - Encrypt if configured
   - Write to backend
   - Create catalog entry
   - Update indexes
   - Enqueue replication (if async)
       │
       ▼
4. Background Worker (async):
   - Dequeue replication task
   - Copy to replica backends
   - Update catalog with new locations
```

### GET Operation

```
1. Client: Get("tenant-acme:docs/report.pdf")
       │
       ▼
2. Catalog Lookup:
   - Find entry by key
   - Get PrimaryStore and Replicas
       │
       ▼
3. Read Selection:
   - Prefer primary if healthy
   - Fall back to replicas on failure
       │
       ▼
4. Backend Read:
   - Fetch from selected backend
   - Decompress if needed
   - Decrypt if needed
   - Verify checksum
       │
       ▼
5. Update Access Time (async):
   - Update LastAccessAt for tiering
```

### Failover Flow

```
Primary Read Failed
       │
       ▼
Check Replicas Available?
       │
   ┌───┴───┐
   │       │
  Yes      No
   │       │
   ▼       ▼
Try Next   Return
Replica    Error
   │
   ▼
Success?
   │
┌──┴──┐
│     │
Yes   No
│     │
▼     ▼
Return  Mark Location
Data    as "failed",
        Try Next
```

## Background Workers

### Replication Worker

Processes async replication queue to copy objects to replica backends.

```go
type ReplicationWorker struct {
    catalog  CatalogManager
    backends BackendManager
    workers  int
}

// Processing loop
for {
    task := catalog.DequeueReplication()
    entry := catalog.Get(task.Key)

    for _, target := range task.TargetStores {
        if hasLocation(entry, target) {
            continue  // Already replicated
        }

        copyObject(entry, target)
        catalog.AddLocation(task.Key, newLocation)
    }
}
```

### Archive Worker

Moves objects to cold storage based on access patterns.

```go
// Scan for objects to archive
entries := catalog.ListByLastAccess(olderThan: 90days)

for _, entry := range entries {
    if !entry.ArchivePolicy.Enabled {
        continue
    }

    copyToArchive(entry)
    catalog.SetArchiveLocation(entry.Key, archiveLocation)

    if entry.ArchivePolicy.DeleteFromPrimary {
        deleteFromPrimary(entry)
    }
}
```

### Verify Worker

Periodically verifies object integrity across all locations.

```go
for {
    task := catalog.DequeueVerify()
    entry := catalog.Get(task.Key)

    for _, loc := range allLocations(entry) {
        data := readFromBackend(loc)
        if !verifyChecksum(data, entry.Checksum) {
            markLocationFailed(entry.Key, loc.Store)
            enqueueRepair(entry.Key)
        }
    }
}
```

## Multi-Tenancy

### Tenant Isolation

```
┌───────────────────────────────────────────────────────────┐
│                   Tenant: tenant-acme                     │
├───────────────────────────────────────────────────────────┤
│  Namespace: tenant-acme:*                                 │
│  Primary Backend: s3-primary                              │
│  Allowed Backends: [s3-primary, gcs-west]                 │
│  Encryption Key: dek:tenant/acme/objects                  │
│  Quota: 100GB storage, 1M objects                         │
├───────────────────────────────────────────────────────────┤
│  Shards:                                                  │
│  ├── Shard 1001 (hot data)                                │
│  │   └── Primary: local-cache                             │
│  └── Shard 1002 (cold data)                               │
│      └── Primary: s3-archive                              │
└───────────────────────────────────────────────────────────┘
```

### Quota Enforcement

```go
func checkQuotas(tenantID string, objectSize int64) error {
    usage := catalog.GetTenantUsage(tenantID)
    config := configResolver.GetTenantConfig(tenantID)

    if usage.TotalBytes + objectSize > config.Quotas.MaxStorageBytes {
        return ErrQuotaExceeded
    }

    if usage.ObjectCount + 1 > config.Quotas.MaxObjectCount {
        return ErrObjectCountExceeded
    }

    if objectSize > config.Quotas.MaxObjectSize {
        return ErrObjectTooLarge
    }

    return nil
}
```

## State Machine Integration

The Enhanced Object Store FSM extends the base ObjectStoreStateMachine:

```go
type EnhancedObjectStoreStateMachine struct {
    *ObjectStoreStateMachine  // Embed for V1 compatibility

    catalog        CatalogManager
    backends       BackendManager
    configResolver *ConfigResolver
    kmsService     KMSService

    replicator *ReplicationWorker
    archiver   *ArchiveWorker
    verifier   *VerifyWorker

    metrics *EnhancedMetrics
}

func (fsm *EnhancedObjectStoreStateMachine) Update(entries []sm.Entry) ([]sm.Entry, error) {
    results := make([]sm.Entry, len(entries))

    for i, entry := range entries {
        cmd := parseCommand(entry.Cmd)

        switch cmd.Version {
        case 1:
            results[i] = fsm.ObjectStoreStateMachine.handleV1(cmd)
        case 2:
            results[i] = fsm.handleV2(cmd)
        }
    }

    return results, nil
}
```

## Compression

Objects are compressed before storage when configured:

| Algorithm | Use Case | Ratio | Speed |
|-----------|----------|-------|-------|
| `none` | Already compressed data | 1:1 | N/A |
| `zstd` | General purpose (default) | 3:1 | Fast |
| `gzip` | Compatibility | 2.5:1 | Medium |
| `lz4` | Speed priority | 2:1 | Very fast |

## Metrics

```
# Object operations
quicbaas_objstore_puts_total
quicbaas_objstore_gets_total
quicbaas_objstore_deletes_total
quicbaas_objstore_bytes_written_total
quicbaas_objstore_bytes_read_total

# Backend metrics
quicbaas_objstore_backend_latency_seconds
quicbaas_objstore_backend_errors_total
quicbaas_objstore_backend_healthy

# Replication
quicbaas_objstore_replication_queue_size
quicbaas_objstore_replication_lag_seconds
quicbaas_objstore_replicas_synced_total

# Catalog
quicbaas_objstore_catalog_entries_total
quicbaas_objstore_catalog_size_bytes

# Per-tenant
quicbaas_objstore_tenant_storage_bytes
quicbaas_objstore_tenant_object_count
```

## Tenant Isolation

The object storage service validates tenant context on all admin operations using `iam.TenantFromContext(ctx)`.

### Validated Operations

- **CreateBackend**: Tenant ID validated before backend registration
- **DeleteBackend**: Tenant ID validated before backend removal
- **ListBackends**: Tenant ID validated before returning backend list
- **GetBackendHealth/CheckAllHealth**: Tenant ID validated before health checks

Backends are infrastructure-level resources shared across the platform. Tenant validation on these operations ensures the request originates from a well-formed, authenticated context rather than scoping backends per tenant.

### Enforcement Point

Validation occurs at the service layer before requests reach the `BackendManager` or `CatalogManager`. Invalid or malformed tenant IDs are rejected with a typed `ErrTenantAccessDenied` error.

### Single-Tenant Mode

When the tenant ID from context is empty, the service operates in single-tenant mode. No tenant validation is applied to admin operations, preserving backward compatibility with existing deployments.
