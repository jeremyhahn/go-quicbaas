# Object Storage — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/object-storage/`.

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


---

## Storage Backends

Configuration reference for each supported object storage backend.

### Local Filesystem

File-based storage for local or single-node deployments.

```yaml
object_storage:
  backends:
    local-store:
      type: local
      config:
        path: /var/lib/quicbaas/objects
```

| Field | Required | Description |
|-------|----------|-------------|
| `path` | Yes | Directory path for object storage |

### Amazon S3

AWS S3 or S3-compatible storage.

```yaml
object_storage:
  backends:
    s3-store:
      type: s3
      config:
        bucket: my-bucket
        region: us-east-1
        access_key: ${AWS_ACCESS_KEY_ID}
        secret_key: ${AWS_SECRET_ACCESS_KEY}
        prefix: quicbaas/
        endpoint: ""  # Optional, for custom endpoints
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `bucket` | Yes | - | S3 bucket name |
| `region` | Yes | - | AWS region |
| `access_key` | Yes | - | AWS access key ID |
| `secret_key` | Yes | - | AWS secret access key |
| `prefix` | No | "" | Key prefix for all objects |
| `endpoint` | No | AWS default | Custom S3 endpoint URL |

### MinIO

Self-hosted S3-compatible storage.

```yaml
object_storage:
  backends:
    minio-store:
      type: minio
      config:
        endpoint: minio.internal:9000
        bucket: my-bucket
        access_key: ${MINIO_ACCESS_KEY}
        secret_key: ${MINIO_SECRET_KEY}
        use_ssl: false
        prefix: data/
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `endpoint` | Yes | - | MinIO server endpoint |
| `bucket` | Yes | - | Bucket name |
| `access_key` | Yes | - | MinIO access key |
| `secret_key` | Yes | - | MinIO secret key |
| `use_ssl` | No | false | Enable TLS |
| `prefix` | No | "" | Key prefix |

### Google Cloud Storage

GCP object storage.

```yaml
object_storage:
  backends:
    gcs-store:
      type: gcs
      config:
        bucket: my-bucket
        project: my-project-id
        credentials_file: /path/to/credentials.json
        prefix: quicbaas/
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `bucket` | Yes | - | GCS bucket name |
| `project` | No | - | GCP project ID |
| `credentials_file` | No | - | Path to service account JSON |
| `prefix` | No | "" | Key prefix |

### Azure Blob Storage

Microsoft Azure blob storage.

```yaml
object_storage:
  backends:
    azure-store:
      type: azure
      config:
        account_name: mystorageaccount
        account_key: ${AZURE_STORAGE_KEY}
        container: my-container
        prefix: quicbaas/
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `account_name` | Yes | - | Azure storage account name |
| `account_key` | Yes | - | Azure storage account key |
| `container` | Yes | - | Blob container name |
| `prefix` | No | "" | Key prefix |

### Memory (Testing)

In-memory storage for development and testing.

```yaml
object_storage:
  backends:
    test-store:
      type: memory
```

No configuration required. Data is not persisted.

**Use cases:**
- Unit tests
- Integration tests
- Local development
- Ephemeral environments

### API Reference

#### Storage Interface

All backends implement `common.Storage`:

```go
type Storage interface {
    Put(key string, data io.Reader) error
    Get(key string) (io.ReadCloser, error)
    Delete(key string) error
    List(prefix string) ([]string, error)
    Exists(ctx context.Context, key string) (bool, error)
}
```

#### Factory Functions

```go
// Create backend by type
storage, err := config.CreateStorageBackendByType("s3", configMap)

// Get named backend from config
storage, err := config.GetStorageBackend("s3-primary", cfg)
```

### Security Best Practices

1. **Credentials** - Use environment variables or secrets management
2. **Encryption at Rest** - Enable server-side encryption on cloud backends
3. **Access Control** - Use IAM policies to restrict bucket access
4. **Network** - Use TLS for all communication
5. **Key Rotation** - Regularly rotate access keys


---

## Object Storage Tiering

Automatic storage tiering and archival based on access patterns and policies.

### Overview

Storage tiering provides:
- Automatic movement of objects between storage tiers
- Access-pattern based archival decisions
- Cost optimization through cold storage
- Configurable retention policies
- Transparent retrieval from any tier

### Tiering Architecture

```
┌───────────────────────────────────────────────────────────────────┐
│                          Storage Tiers                            │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  HOT TIER (Frequent Access)                                       │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  local-cache              │  s3-primary (STANDARD)          │  │
│  │  - Fast SSD               │  - Low latency                  │  │
│  │  - Expensive              │  - Higher cost                  │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│                   (after N days of no access)                     │
│                              ↓                                    │
│  WARM TIER (Infrequent Access)                                    │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  s3-primary (STANDARD_IA) │  gcs-west (NEARLINE)            │  │
│  │  - Medium latency         │  - Lower cost                   │  │
│  │  - Retrieval fees         │  - 30-day minimum               │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│                   (after N days of no access)                     │
│                              ↓                                    │
│  COLD TIER (Archive)                                              │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  s3-archive (GLACIER)     │  gcs-archive (COLDLINE)         │  │
│  │  - High latency (hours)   │  - Very low cost                │  │
│  │  - Retrieval fees         │  - 90-day minimum               │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

### Configuration

#### Archive Policy

```yaml
object_storage:
  defaults:
    archive:
      enabled: true
      archive_after_days: 90     # Move to archive after 90 days of no access
      target_store: s3-archive   # Destination backend
      delete_from_primary: false # Keep on primary after archive
      compression_level: 6       # 0-9 compression level

  backends:
    s3-archive:
      type: s3
      config:
        bucket: quicbaas-archive
        region: us-east-1
        storage_class: GLACIER
```

#### Per-Tenant Tiering

```yaml
object_storage:
  tenants:
    tenant-acme:
      archive:
        enabled: true
        archive_after_days: 30    # Archive sooner
        target_store: s3-archive
        delete_from_primary: true # Remove from hot storage
        compression_level: 9      # Maximum compression

    tenant-beta:
      archive:
        enabled: false            # No automatic archival
```

#### Retention Policy

```yaml
object_storage:
  defaults:
    retention:
      enabled: true
      retention_days: 365        # Delete after 1 year
      max_versions: 10           # Keep last 10 versions

  tenants:
    tenant-acme:
      retention:
        enabled: true
        retention_days: 2555     # 7 years for compliance
        max_versions: 0          # Keep all versions
```

### Archive Policy

#### Policy Structure

```go
type ArchivePolicy struct {
    Enabled          bool    // Enable automatic archival
    ArchiveAfterDays int     // Days since last access
    TargetStore      string  // Archive backend name
    DeleteFromPrimary bool   // Remove from primary after archive
    CompressionLevel int     // 0=none, 1-9=level
}
```

#### Catalog Entry

```json
{
  "key": "tenant-acme:docs/old-report.pdf",
  "primary_store": null,
  "archive_store": {
    "store": "s3-archive",
    "object_ref": "archive/tenant-acme/docs/old-report.pdf.zst",
    "status": "active",
    "compressed": true,
    "synced_at": "2024-01-15T10:30:00Z"
  },
  "archive_policy": {
    "enabled": true,
    "archive_after_days": 90,
    "target_store": "s3-archive",
    "delete_from_primary": true,
    "compression_level": 6
  },
  "archived_at": "2024-01-15T10:30:00Z",
  "status": "archived"
}
```

### Archive Worker

#### Worker Architecture

```go
type ArchiveWorker struct {
    catalog  CatalogManager
    backends BackendManager
    interval time.Duration
}

func (w *ArchiveWorker) Run(ctx context.Context) {
    ticker := time.NewTicker(w.interval)
    for {
        select {
        case <-ticker.C:
            w.processArchiveCandidates(ctx)
        case <-ctx.Done():
            return
        }
    }
}

func (w *ArchiveWorker) processArchiveCandidates(ctx context.Context) {
    // Find objects eligible for archival
    candidates := w.catalog.ListArchiveCandidates(ctx)

    for _, entry := range candidates {
        if !entry.ArchivePolicy.Enabled {
            continue
        }

        daysSinceAccess := time.Since(entry.LastAccessAt).Hours() / 24
        if daysSinceAccess < float64(entry.ArchivePolicy.ArchiveAfterDays) {
            continue
        }

        w.archiveObject(ctx, entry)
    }
}

func (w *ArchiveWorker) archiveObject(ctx context.Context, entry *CatalogEntry) {
    // Read from primary
    data := w.readFromPrimary(entry)

    // Compress if configured
    if entry.ArchivePolicy.CompressionLevel > 0 {
        data = compress(data, entry.ArchivePolicy.CompressionLevel)
    }

    // Write to archive backend
    archiveBackend := w.backends.GetBackend(entry.ArchivePolicy.TargetStore)
    archiveRef := generateArchiveRef(entry.Key)
    archiveBackend.Put(archiveRef, data)

    // Update catalog
    entry.ArchiveStore = &StoreLocation{
        Store:      entry.ArchivePolicy.TargetStore,
        ObjectRef:  archiveRef,
        Status:     "active",
        Compressed: entry.ArchivePolicy.CompressionLevel > 0,
        SyncedAt:   time.Now(),
    }
    entry.ArchivedAt = time.Now()
    entry.Status = StatusArchived

    // Delete from primary if configured
    if entry.ArchivePolicy.DeleteFromPrimary {
        w.deleteFromPrimary(entry)
        entry.PrimaryStore = nil
    }

    w.catalog.Update(ctx, entry)
}
```

### Retention Policy

#### Policy Structure

```go
type RetentionPolicy struct {
    Enabled       bool   // Enable retention enforcement
    RetentionDays int    // Days to retain (0 = infinite)
    MaxVersions   int    // Maximum versions to keep (0 = unlimited)
}
```

#### Retention Enforcement

```go
type RetentionWorker struct {
    catalog  CatalogManager
    backends BackendManager
    interval time.Duration
}

func (w *RetentionWorker) processRetentionCandidates(ctx context.Context) {
    candidates := w.catalog.ListRetentionCandidates(ctx)

    for _, entry := range candidates {
        if !entry.RetentionPolicy.Enabled {
            continue
        }

        // Check age-based retention
        if entry.RetentionPolicy.RetentionDays > 0 {
            daysSinceCreation := time.Since(entry.CreatedAt).Hours() / 24
            if daysSinceCreation > float64(entry.RetentionPolicy.RetentionDays) {
                w.deleteObject(ctx, entry)
                continue
            }
        }

        // Check version-based retention
        if entry.RetentionPolicy.MaxVersions > 0 {
            w.pruneOldVersions(ctx, entry)
        }
    }
}
```

### Tiering Operations

#### Archive Object

Move object to archive tier:

```
1. Read object from primary backend
       │
       ▼
2. Compress (if configured)
       │
       ▼
3. Write to archive backend
       │
       ▼
4. Update catalog:
   - Set archive_store
   - Set archived_at
   - Set status = "archived"
       │
       ▼
5. Delete from primary (if delete_from_primary=true)
       │
       ▼
6. Update catalog:
   - Set primary_store = null
```

#### Restore Object

Retrieve object from archive:

```
1. Client: Get("tenant-acme:docs/old-report.pdf")
       │
       ▼
2. Catalog lookup: status = "archived"
       │
       ▼
3. Read from archive backend (may take time)
       │
       ▼
4. Decompress (if compressed)
       │
       ▼
5. Return to client
       │
       ▼
6. Optionally restore to hot tier:
   - Write to primary backend
   - Update catalog
   - Update last_access_at
```

#### Lifecycle Transitions

```
                ┌──────────────────────────────────────────┐
                │                                          │
                ▼                                          │
┌─────────┐   PUT   ┌─────────┐  archive_after_days  ┌─────────┐
│ pending │ ──────► │  active │ ──────────────────► │archived │
└─────────┘         └─────────┘                      └─────────┘
                         │                                │
                         │ retention_days                 │
                         ▼                                │
                    ┌─────────┐                           │
                    │ deleted │ ◄─────────────────────────┘
                    └─────────┘       retention_days
```

### CLI Commands

#### Archive Object

```bash
# Archive immediately
quicbaas object archive tenant-acme:docs/old-report.pdf

# Archive and delete from primary
quicbaas object archive tenant-acme:docs/old-report.pdf --delete-primary

# Archive to specific backend
quicbaas object archive tenant-acme:docs/old-report.pdf \
  --target s3-glacier \
  --compression 9

# Archive all objects in namespace older than N days
quicbaas object archive --prefix tenant-acme: --older-than 90d
```

#### Restore Object

```bash
# Restore to hot tier
quicbaas object restore tenant-acme:docs/old-report.pdf

# Restore to specific backend
quicbaas object restore tenant-acme:docs/old-report.pdf --target s3-primary

# Check restore status (for slow backends like Glacier)
quicbaas object restore-status tenant-acme:docs/old-report.pdf
```

#### View Tier Status

```bash
# View object tier
quicbaas catalog get tenant-acme:docs/report.pdf

# List archived objects
quicbaas object list --status archived

# List objects eligible for archive
quicbaas object list --archive-eligible

# List objects exceeding retention
quicbaas object list --retention-exceeded
```

### REST API

#### Archive Object

```http
POST /api/v1/objects/{key}/archive
```

**Request:**
```json
{
  "target_store": "s3-archive",
  "delete_from_primary": true,
  "compression_level": 6
}
```

**Response:**
```json
{
  "key": "tenant-acme:docs/old-report.pdf",
  "archive_store": "s3-archive",
  "archived_at": "2024-01-15T10:30:00Z",
  "status": "archived"
}
```

#### Restore Object

```http
POST /api/v1/objects/{key}/restore
```

**Request:**
```json
{
  "target_store": "s3-primary",
  "expedited": false
}
```

**Response:**
```json
{
  "key": "tenant-acme:docs/old-report.pdf",
  "restore_status": "in_progress",
  "estimated_time": "3-5 hours",
  "task_id": "restore-123456"
}
```

#### Check Restore Status

```http
GET /api/v1/objects/{key}/restore-status
```

**Response:**
```json
{
  "key": "tenant-acme:docs/old-report.pdf",
  "status": "completed",
  "restored_at": "2024-01-15T14:30:00Z",
  "primary_store": "s3-primary"
}
```

### Storage Classes

#### S3 Storage Classes

| Class | Use Case | Retrieval Time | Min Duration |
|-------|----------|----------------|--------------|
| STANDARD | Frequently accessed | Immediate | None |
| STANDARD_IA | Infrequently accessed | Immediate | 30 days |
| GLACIER | Archive | 3-5 hours | 90 days |
| GLACIER_DEEP_ARCHIVE | Long-term archive | 12 hours | 180 days |

#### GCS Storage Classes

| Class | Use Case | Retrieval Time | Min Duration |
|-------|----------|----------------|--------------|
| STANDARD | Frequently accessed | Immediate | None |
| NEARLINE | Monthly access | Immediate | 30 days |
| COLDLINE | Quarterly access | Immediate | 90 days |
| ARCHIVE | Yearly access | Immediate | 365 days |

#### Azure Blob Tiers

| Tier | Use Case | Retrieval Time | Min Duration |
|------|----------|----------------|--------------|
| Hot | Frequently accessed | Immediate | None |
| Cool | Infrequently accessed | Immediate | 30 days |
| Archive | Rarely accessed | Hours | 180 days |

### Compression

Objects can be compressed before archival:

| Level | Ratio | Speed | Use Case |
|-------|-------|-------|----------|
| 0 | 1:1 | N/A | No compression |
| 1-3 | 2:1 | Fast | Quick archival |
| 4-6 | 3:1 | Medium | Balanced (default) |
| 7-9 | 4:1 | Slow | Maximum savings |

Configuration:
```yaml
archive:
  compression_level: 6  # Balanced compression
```

### Metrics

```
# Tiering operations
quicbaas_objstore_archives_total
quicbaas_objstore_restores_total
quicbaas_objstore_retention_deletes_total

# Storage by tier
quicbaas_objstore_hot_tier_bytes
quicbaas_objstore_warm_tier_bytes
quicbaas_objstore_cold_tier_bytes

# Tiering queue
quicbaas_objstore_archive_queue_size
quicbaas_objstore_archive_candidates

# Latency
quicbaas_objstore_archive_duration_seconds
quicbaas_objstore_restore_duration_seconds
```

### Troubleshooting

#### Common Issues

| Issue | Cause | Resolution |
|-------|-------|------------|
| Slow restore | Glacier retrieval | Use expedited retrieval |
| Archive failed | Backend unreachable | Check backend health |
| Retention not enforced | Worker not running | Check retention worker |
| Unexpected deletion | Retention policy | Review retention settings |

#### Debugging

```bash
# Check archive worker status
quicbaas status --component archive-worker

# View archive queue
quicbaas object list --archive-pending

# Check retention candidates
quicbaas object list --retention-exceeded

# Force archive run
quicbaas admin archive-run --dry-run
```


---

## Object Storage Replication

Multi-backend replication strategies for durability and disaster recovery.

### Overview

Object replication provides:
- Automatic copying of objects across multiple backends
- Configurable replication modes (sync, async, quorum)
- Priority-based replication scheduling
- Automatic failover for reads
- Replication status tracking

### Replication Architecture

```
┌───────────────────────────────────────────────────────────────────┐
│                       Object PUT Request                          │
└───────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌───────────────────────────────────────────────────────────────────┐
│                     Write to Primary Backend                      │
│                          (s3-primary)                             │
└───────────────────────────────────────────────────────────────────┘
                                   │
                     ┌─────────────┴─────────────┐
                     ▼                           ▼
             ┌───────────────┐           ┌───────────────┐
             │   Sync Mode   │           │  Async Mode   │
             │               │           │               │
             │ Wait for all  │           │    Return     │
             │   replicas    │           │  immediately  │
             └───────┬───────┘           └───────┬───────┘
                     │                           │
                     │                           ▼
                     │               ┌───────────────────────┐
                     │               │   Replication Queue   │
                     │               │   queue:replicate:... │
                     │               └───────────┬───────────┘
                     │                           │
                     │                           ▼
                     │               ┌───────────────────────┐
                     │               │  Replication Worker   │
                     │               │      (background)     │
                     │               └───────────┬───────────┘
                     │                           │
                     └─────────────┬─────────────┘
                                   ▼
               ┌───────────────────┴───────────────────┐
               ▼                                       ▼
         ┌──────────┐                           ┌──────────┐
         │ gcs-west │                           │s3-backup │
         │ (replica)│                           │ (replica)│
         └──────────┘                           └──────────┘
```

### Configuration

#### Cluster-Wide Defaults

```yaml
object_storage:
  defaults:
    replication:
      enabled: true
      min_replicas: 2              # Minimum copies (including primary)
      target_stores:               # Where to replicate
        - s3-primary
        - gcs-west
      mode: async                  # sync, async, quorum
      priority: 5                  # 1-10, higher = replicate sooner
```

#### Per-Tenant Configuration

```yaml
object_storage:
  tenants:
    tenant-acme:
      replication:
        min_replicas: 3            # Override: need 3 copies
        target_stores:
          - s3-primary
          - gcs-west
          - s3-backup
        mode: quorum               # Wait for majority
        priority: 8                # High priority

    tenant-beta:
      replication:
        min_replicas: 2
        target_stores:
          - s3-primary
          - gcs-west
        mode: async                # Background replication
        priority: 3                # Lower priority
```

#### Per-Shard Configuration

```yaml
object_storage:
  tenants:
    tenant-acme:
      shard_configs:
        1001:                      # Hot data shard
          replication:
            min_replicas: 3
            target_stores:
              - local-cache
              - s3-primary
              - gcs-west
            mode: sync             # Sync for critical data
```

### Replication Modes

#### Sync Mode

Wait for all replicas before returning success.

```yaml
replication:
  mode: sync
  target_stores: [s3-primary, gcs-west, s3-backup]
```

**Flow:**
```
PUT Request
     │
     ▼
Write to s3-primary ─────┐
     │                   │
     ├───► Wait         │
     │                   │
Write to gcs-west ───────┤
     │                   │
     ├───► Wait         │
     │                   │
Write to s3-backup ──────┤
     │                   │
     ▼                   │
All complete ◄───────────┘
     │
     ▼
Return Success
```

**Use cases:**
- Critical data requiring guaranteed durability
- Compliance requirements
- Small objects where latency is acceptable

#### Async Mode

Return immediately after primary write, replicate in background.

```yaml
replication:
  mode: async
  target_stores: [s3-primary, gcs-west, s3-backup]
```

**Flow:**
```
PUT Request
     │
     ▼
Write to s3-primary
     │
     ├───► Return Success (immediately)
     │
     ▼
Enqueue Replication Task
     │
     ▼ (background)
Replication Worker
     │
     ├───► Copy to gcs-west
     │
     └───► Copy to s3-backup
```

**Use cases:**
- Large objects
- High-throughput scenarios
- Non-critical data
- Latency-sensitive applications

#### Quorum Mode

Wait for majority of replicas before returning success.

```yaml
replication:
  mode: quorum
  min_replicas: 3
  target_stores: [s3-primary, gcs-west, s3-backup]
```

**Flow:**
```
PUT Request
     │
     ▼
Write to s3-primary ─────┐
     │                   │ 1/3
     ├───► Wait         │
     │                   │
Write to gcs-west ───────┤
     │                   │ 2/3 ── Quorum reached!
     ▼                   │
Return Success ◄─────────┘
     │
     ▼ (background)
Complete remaining replica (s3-backup)
```

**Use cases:**
- Balance between durability and latency
- Large-scale deployments
- Geographic distribution

### Priority Scheduling

Replication tasks are scheduled based on priority (1-10, higher = sooner):

```yaml
# High-priority tenant
tenants:
  tenant-critical:
    replication:
      priority: 10

# Normal priority
tenants:
  tenant-normal:
    replication:
      priority: 5

# Low priority (best effort)
tenants:
  tenant-archive:
    replication:
      priority: 1
```

Queue ordering:
```
queue:replicate:{priority}:{timestamp}:{key}

Example:
queue:replicate:10:1705312200:critical:data.json  ← Processed first
queue:replicate:05:1705312100:normal:file.txt     ← Processed second
queue:replicate:01:1705312000:archive:old.tar.gz  ← Processed last
```

### Replication Policy

#### Policy Structure

```go
type ReplicationPolicy struct {
    Enabled      bool       // Enable replication
    MinReplicas  int        // Minimum copies required
    TargetStores []string   // Backend names
    Mode         string     // sync, async, quorum
    Priority     int        // 1-10
}
```

#### Catalog Entry

```json
{
  "key": "tenant-acme:docs/report.pdf",
  "primary_store": {
    "store": "s3-primary",
    "status": "active",
    "synced_at": "2024-01-15T10:30:00Z"
  },
  "replicas": [
    {
      "store": "gcs-west",
      "status": "active",
      "synced_at": "2024-01-15T10:35:00Z"
    },
    {
      "store": "s3-backup",
      "status": "syncing",
      "synced_at": null
    }
  ],
  "replication_policy": {
    "enabled": true,
    "min_replicas": 3,
    "target_stores": ["s3-primary", "gcs-west", "s3-backup"],
    "mode": "async",
    "priority": 8
  }
}
```

### Replication Worker

#### Worker Architecture

```go
type ReplicationWorker struct {
    catalog     CatalogManager
    backends    BackendManager
    workers     int           // Number of parallel workers
    batchSize   int           // Tasks per batch
    retryPolicy *RetryPolicy
}

func (w *ReplicationWorker) Run(ctx context.Context) {
    for i := 0; i < w.workers; i++ {
        go w.processQueue(ctx)
    }
}

func (w *ReplicationWorker) processQueue(ctx context.Context) {
    for {
        // Dequeue highest priority task
        task := w.catalog.DequeueReplication(ctx)

        // Get catalog entry
        entry := w.catalog.Get(ctx, task.Key)

        // Replicate to each target
        for _, target := range task.TargetStores {
            if hasLocation(entry, target) {
                continue  // Already replicated
            }

            err := w.copyObject(ctx, entry, target)
            if err != nil {
                w.handleError(task, target, err)
                continue
            }

            // Update catalog
            w.catalog.AddLocation(ctx, task.Key, &StoreLocation{
                Store:    target,
                Status:   "active",
                SyncedAt: time.Now(),
            })
        }
    }
}
```

#### Retry Policy

```go
type RetryPolicy struct {
    MaxRetries     int           // Maximum retry attempts
    InitialBackoff time.Duration // Initial wait
    MaxBackoff     time.Duration // Maximum wait
    BackoffFactor  float64       // Multiplier
}

// Default policy
var DefaultRetryPolicy = &RetryPolicy{
    MaxRetries:     5,
    InitialBackoff: 1 * time.Second,
    MaxBackoff:     5 * time.Minute,
    BackoffFactor:  2.0,
}
```

### Read Failover

When primary is unavailable, reads automatically fall back to replicas:

```
GET Request for "tenant-acme:docs/report.pdf"
     │
     ▼
Catalog Lookup
  - PrimaryStore: s3-primary
  - Replicas: [gcs-west, s3-backup]
     │
     ▼
Try Primary (s3-primary)
     │
     ├─── Success ───► Return data
     │
     └─── Failure
            │
            ▼
     Try Replica 1 (gcs-west)
            │
            ├─── Success ───► Return data
            │
            └─── Failure
                   │
                   ▼
            Try Replica 2 (s3-backup)
                   │
                   ├─── Success ───► Return data
                   │
                   └─── Failure ───► Return Error
```

### CLI Commands

#### Add Replicas

```bash
# Add replicas asynchronously
quicbaas object replicate tenant-acme:docs/report.pdf \
  --target gcs-west \
  --target s3-backup

# Synchronous replication
quicbaas object replicate tenant-acme:docs/report.pdf \
  --target gcs-west \
  --mode sync

# Wait for async replication
quicbaas object replicate tenant-acme:docs/report.pdf \
  --target gcs-west \
  --wait
```

#### Check Replication Status

```bash
# View locations
quicbaas catalog locations tenant-acme:docs/report.pdf

# List under-replicated objects
quicbaas object list --under-replicated

# List objects pending replication
quicbaas object list --replication-status syncing
```

#### Force Replication

```bash
# Force immediate replication
quicbaas object replicate tenant-acme:docs/report.pdf \
  --target gcs-west \
  --force

# Replicate all objects in namespace
quicbaas object replicate --prefix tenant-acme: \
  --target s3-backup
```

### REST API

#### Replicate Object

```http
POST /api/v1/objects/{key}/replicate
```

**Request:**
```json
{
  "target_stores": ["gcs-west", "s3-backup"],
  "mode": "async"
}
```

**Response:**
```json
{
  "key": "tenant-acme:docs/report.pdf",
  "replicas": [
    {"store": "gcs-west", "status": "syncing"},
    {"store": "s3-backup", "status": "syncing"}
  ],
  "task_id": "repl-123456"
}
```

#### Get Replication Status

```http
GET /api/v1/objects/{key}/replication-status
```

**Response:**
```json
{
  "key": "tenant-acme:docs/report.pdf",
  "policy": {
    "enabled": true,
    "min_replicas": 3,
    "target_stores": ["s3-primary", "gcs-west", "s3-backup"],
    "mode": "async"
  },
  "status": {
    "current_replicas": 2,
    "target_replicas": 3,
    "locations": [
      {"store": "s3-primary", "status": "active"},
      {"store": "gcs-west", "status": "active"},
      {"store": "s3-backup", "status": "syncing"}
    ]
  }
}
```

### Metrics

```
# Replication queue
quicbaas_objstore_replication_queue_size
quicbaas_objstore_replication_queue_age_seconds

# Replication operations
quicbaas_objstore_replications_total
quicbaas_objstore_replication_failures_total
quicbaas_objstore_replication_duration_seconds

# Replication lag
quicbaas_objstore_replication_lag_seconds
quicbaas_objstore_under_replicated_objects

# Per-backend
quicbaas_objstore_backend_replicas_synced
quicbaas_objstore_backend_replication_bytes
```

### Troubleshooting

#### Common Issues

| Issue | Cause | Resolution |
|-------|-------|------------|
| Replication lag | High queue, slow backends | Increase workers, check backend health |
| Sync timeout | Backend latency | Use async mode, increase timeout |
| Under-replicated objects | Backend failures | Check backend health, trigger re-replication |
| Inconsistent replicas | Partial failures | Run verification, re-replicate |

#### Debugging

```bash
# Check replication queue size
quicbaas status --component replication

# View failed replications
quicbaas object list --replication-status failed

# Retry failed replications
quicbaas object replicate --retry-failed --prefix tenant-acme:

# Verify replica consistency
quicbaas object verify tenant-acme:docs/report.pdf
```


---

## ObjectStore State Machine

A hybrid Raft state machine that automatically offloads large values to object storage while keeping metadata in PebbleDB.

### Overview

The ObjectStore state machine optimizes storage by:

- **Small values** (< threshold): Stored inline in PebbleDB for fast access
- **Large values** (>= threshold): Offloaded to object storage (S3/GCS/Azure)
- **Metadata**: Always in PebbleDB, replicated via Raft consensus

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                ObjectStoreStateMachine                          │
│            (implements IOnDiskStateMachine)                     │
└─────────────────────────────────────────────────────────────────┘
                 │                           │
      value < threshold              value >= threshold
                 │                           │
                 ▼                           ▼
    ┌─────────────────────┐      ┌─────────────────────┐
    │   PebbleDB          │      │  Object Storage     │
    │   (Inline Data)     │      │  (S3/GCS/Azure)     │
    │   + Metadata        │      │                     │
    └─────────────────────┘      └─────────────────────┘
```

### Multi-Cloud Replication

Each node writes large values to its own object storage backend. Example 3-node cluster:

```
   Node A               Node B               Node C
   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
   │ PebbleDB     │    │ PebbleDB     │    │ PebbleDB     │
   │ (metadata)   │    │ (metadata)   │    │ (metadata)   │
   └──────────────┘    └──────────────┘    └──────────────┘
          │                   │                   │
          ▼                   ▼                   ▼
   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
   │     S3       │    │     GCS      │    │    Azure     │
   │   (AWS)      │    │   (GCP)      │    │   (Azure)    │
   └──────────────┘    └──────────────┘    └──────────────┘
```

**Benefits:**
- Multi-cloud redundancy (no single cloud dependency)
- Each node reads from its own store (low latency)
- Independent failure domains

### Configuration

```yaml
raft:
  statemachines:
    - shard_id: 1
      type: objstore
      config:
        value_threshold: 1048576  # 1 MB
        metadata_path: /var/lib/quicbaas/raft/shard-1
        async_writes: true
        max_pending_writes: 100
        compression: zstd
        objstore:
          type: s3
          config:
            bucket: my-quicbaas-bucket
            region: us-east-1
```

#### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `value_threshold` | int64 | 1048576 | Size in bytes to trigger offload |
| `metadata_path` | string | required | PebbleDB path for metadata |
| `async_writes` | bool | true | Async object storage writes |
| `max_pending_writes` | int | 100 | Max concurrent async operations |
| `compression` | string | "none" | Compression: none, zstd, gzip |

#### Object Storage Configuration

See [backends.md](backends.md) for backend-specific options.

### Value Metadata

Stored in PebbleDB for every key:

```go
type ValueMetadata struct {
    Key             []byte            // Original key
    Size            int64             // Original value size
    IsOffloaded     bool              // True if in object storage
    ObjectRef       string            // Object storage key (local)
    Checksum        []byte            // SHA-256 for integrity
    Data            []byte            // Inline data (if not offloaded)
    Version         uint64            // MVCC version
    CreatedAt       int64
    UpdatedAt       int64
    FetchStatus     ObjectFetchStatus // Available, Pending, or Fetching
    SourceObjectRef string            // Source peer's object ref (for fetching)
}

type ObjectFetchStatus uint8

const (
    ObjectStatusAvailable ObjectFetchStatus = 0 // Locally available
    ObjectStatusPending   ObjectFetchStatus = 1 // Needs fetch from peer
    ObjectStatusFetching  ObjectFetchStatus = 2 // Fetch in progress
)
```

**FetchStatus Transitions:**
- `Available` → Normal state, object is locally accessible
- `Pending` → After snapshot recovery, object needs fetching from peer
- `Fetching` → Fetch worker is actively retrieving the object

### Operations

#### Update Flow

```
1. Decode command (binary protocol)
2. If PUT with value >= threshold:
   - Compute SHA-256 checksum
   - Compress if configured
   - Upload to object storage (async or sync)
   - Store metadata with ObjectRef in PebbleDB
3. If PUT with value < threshold:
   - Store metadata with inline Data in PebbleDB
4. If DELETE:
   - Delete from PebbleDB
   - Delete from object storage if offloaded
```

#### Lookup Flow

```
1. Load metadata from PebbleDB
2. If inline data, return directly
3. If offloaded and FetchStatus is Pending or Fetching:
   - Schedule high-priority fetch from peers (if Pending)
   - Return ObjectPendingError (client should retry)
4. If offloaded and FetchStatus is Available:
   - Fetch from local object storage
   - Decompress if needed
   - Verify checksum
   - Return value
```

**Pending Object Behavior:** When a read encounters a pending object, the state machine returns `ObjectPendingError` and triggers an async fetch. The client should retry after a brief delay.

### Snapshots

Snapshots contain **metadata only** - large object data is excluded for optimal performance. Recovering nodes fetch objects lazily from peers using the async ObjectFetcher.

**Format:**
```
Header: [Version:1B][Revision:8B][Timestamp:8B][NumKeys:8B]
For each key:
  [MetaLen:4B][ValueMetadata (msgpack)]
```

**Recovery:**
1. Restore metadata to PebbleDB
2. For each offloaded value:
   - Mark `FetchStatus = Pending`
   - Store `SourceObjectRef` (original peer's object ref)
   - Clear local `ObjectRef` (no local copy yet)
3. Schedule bulk fetch for all pending objects

**Benefits:**
- Dramatically smaller snapshot sizes
- Faster snapshot transfer between nodes
- Non-blocking recovery (reads can proceed, pending objects return error)
- Parallel object fetching from multiple peers

### Lazy Object Fetching

The `ObjectFetcher` asynchronously retrieves pending objects from peers without blocking Raft or state machine operations.

#### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     ObjectFetcher                           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │ High Priority│  │Normal Priority│  │  Fetch Workers  │   │
│  │    Queue     │  │    Queue      │  │  (N concurrent) │   │
│  └──────────────┘  └──────────────┘  └──────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│               ObjectTransferService (gRPC)                  │
│                                                             │
│   Peer A ◄────────► Peer B ◄────────► Peer C                │
└─────────────────────────────────────────────────────────────┘
```

#### Priority Queues

- **High Priority**: On-demand fetches triggered by read operations
- **Normal Priority**: Background fetches scheduled after recovery

#### Fetch Flow

```
1. Worker selects request (high priority first)
2. Get peer list from discovery
3. For each peer until success:
   - Connect via gRPC
   - Call FetchObject(shardID, objectRef, checksum)
   - Verify checksum if provided
4. Write to local object store
5. Update metadata: FetchStatus=Available, ObjectRef=localRef
6. Clear SourceObjectRef
```

#### Configuration

```yaml
raft:
  statemachines:
    - shard_id: 1
      type: objstore
      config:
        fetcher:
          max_concurrent: 4      # Parallel fetch workers
          max_retries: 5         # Retries per object
          retry_base_delay: 1s   # Exponential backoff base
          fetch_timeout: 30s     # Per-fetch timeout
          queue_size: 10000      # Max pending requests
```

#### Deduplication

The fetcher deduplicates requests using an `inFlight` map. Multiple reads for the same pending object result in a single fetch operation.

#### Retry Logic

Failed fetches retry with exponential backoff:
- Attempt 1: immediate
- Attempt 2: 1s delay
- Attempt 3: 2s delay
- Attempt 4: 4s delay
- Attempt 5: 8s delay (max)

After max retries, the object is marked permanently failed.

### Object Transfer Service

gRPC service for peer-to-peer object transfer:

```protobuf
service ObjectTransferService {
    rpc FetchObject(FetchObjectRequest) returns (FetchObjectResponse);
}

message FetchObjectRequest {
    uint64 shard_id = 1;
    string object_ref = 2;
    bytes checksum = 3;
}

message FetchObjectResponse {
    bytes data = 1;
    bytes checksum = 2;
    bool found = 3;
    string error_message = 4;
}
```

### Threshold Strategies

#### Small Threshold (100 KB)

```yaml
value_threshold: 102400
```

- Most values offloaded
- Smaller PebbleDB
- More object storage operations
- Best for: Media-heavy workloads

#### Default (1 MB)

```yaml
value_threshold: 1048576
```

- Balanced approach
- Common KV data stays local
- Large blobs offloaded
- Best for: Mixed workloads

#### Large Threshold (10 MB)

```yaml
value_threshold: 10485760
```

- Only very large values offloaded
- Larger PebbleDB
- Fewer object storage operations
- Best for: Predominantly small values

### Compression

| Algorithm | Speed | Ratio | Use Case |
|-----------|-------|-------|----------|
| `none` | Fastest | 1:1 | Pre-compressed data (images, videos) |
| `zstd` | Fast | Best | General purpose (recommended) |
| `gzip` | Medium | Good | Maximum compatibility |

### Async vs Sync Writes

#### Async Writes (Recommended)

```yaml
async_writes: true
```

- Better write latency
- Raft log provides durability
- Object storage writes happen after Raft commit
- May have brief window where value is in Raft log but not object storage

#### Sync Writes

```yaml
async_writes: false
```

- Stricter consistency
- Higher write latency
- Value confirmed in object storage before Raft commit returns

### Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `objstore_put_ops_total` | Counter | Total PUT operations |
| `objstore_get_ops_total` | Counter | Total GET operations |
| `objstore_offloaded_writes_total` | Counter | Values written to object storage |
| `objstore_inline_writes_total` | Counter | Values stored inline |
| `objstore_offloaded_bytes_total` | Counter | Bytes written to object storage |
| `objstore_put_latency_seconds` | Histogram | PUT operation duration |
| `objstore_offload_latency_seconds` | Histogram | Object storage upload duration |
| `objstore_pending_writes` | Gauge | Current pending async writes |
| `objstore_fetcher_fetched_total` | Counter | Objects fetched from peers |
| `objstore_fetcher_failed_total` | Counter | Failed fetch attempts |
| `objstore_fetcher_pending` | Gauge | Objects pending fetch |

### Error Handling

| Error | Description | Recovery |
|-------|-------------|----------|
| `ErrObjectNotFound` | Object missing from storage | Re-upload from snapshot |
| `ErrChecksumMismatch` | Data integrity failure | Fetch from another node |
| `ErrAsyncWriteFailed` | Background write failed | Retry with backoff |
| `ErrObjectPending` | Object being fetched from peer | Retry read after delay |
| `ErrFetchFailed` | Peer fetch failed | Auto-retry with backoff |


---

## Object Catalog

The internal catalog is the source of truth for all object metadata and locations.

### Overview

The catalog provides:
- Unified view of all object locations across backends
- Secondary indexes for efficient querying
- Policy storage (replication, archive, retention)
- Multi-tenant isolation
- Versioning and optimistic concurrency

### Catalog Architecture

```
┌───────────────────────────────────────────────────────────────────┐
│                         Catalog Manager                           │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │                      Primary Storage                        │  │
│  │            cat:{namespace}:{path} → CatalogEntry            │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │                     Secondary Indexes                       │  │
│  │  idx:store:{store}:{key}      → (objects by backend)        │  │
│  │  idx:tenant:{tenant}:{key}    → (objects by tenant)         │  │
│  │  idx:checksum:{hex}:{key}     → (deduplication)             │  │
│  │  idx:status:{status}:{key}    → (objects by status)         │  │
│  │  idx:tag:{tag}:{value}:{key}  → (objects by tag)            │  │
│  │  idx:encryption:{keyid}:{key} → (objects by enc key)        │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │                     Async Work Queues                       │  │
│  │  queue:replicate:{priority}:{ts}:{key}                      │  │
│  │  queue:archive:{ts}:{key}                                   │  │
│  │  queue:verify:{ts}:{key}                                    │  │
│  │  queue:delete:{ts}:{key}                                    │  │
│  │  queue:reencrypt:{ts}:{key}                                 │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │                        Statistics                           │  │
│  │  stats:tenant:{tenant} → TenantUsageStats                   │  │
│  │  stats:store:{store}   → StoreUsageStats                    │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

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
    Owner     string   // User/service that created it

    // === ACCESS CONTROL ===
    ACL *ObjectACL

    // === METADATA ===
    Tags       map[string]string  // User-defined tags
    Labels     map[string]string  // System labels
    CustomMeta map[string]string  // Custom metadata

    // === TIMESTAMPS ===
    CreatedAt    int64   // Creation time
    ModifiedAt   int64   // Last modification
    LastAccessAt int64   // Last read access
    ArchivedAt   int64   // When archived (0 if not)

    // === STATUS ===
    Status  EntryStatus  // active, pending, archived, deleted
    Version uint64       // For optimistic concurrency
}

type StoreLocation struct {
    Store       string   // Backend name: "s3-primary"
    ObjectRef   string   // Path/key within backend
    Status      string   // "active", "syncing", "failed", "stale"
    Checksum    []byte   // Per-location checksum
    StoredSize  int64    // May differ if compressed
    Compressed  bool
    Encryption  *LocationEncryption
    SyncedAt    int64    // Last successful sync
}

type EntryStatus string
const (
    StatusActive   EntryStatus = "active"
    StatusPending  EntryStatus = "pending"
    StatusArchived EntryStatus = "archived"
    StatusDeleted  EntryStatus = "deleted"
)
```

### Catalog Manager Interface

```go
type CatalogManager interface {
    // CRUD operations
    Put(ctx context.Context, entry *CatalogEntry) error
    Get(ctx context.Context, key string) (*CatalogEntry, error)
    Delete(ctx context.Context, key string) error
    Update(ctx context.Context, key string, updater func(*CatalogEntry) error) error

    // Location management
    AddLocation(ctx context.Context, key string, loc *StoreLocation) error
    RemoveLocation(ctx context.Context, key string, store string) error
    SetPrimaryStore(ctx context.Context, key string, store string) error
    GetLocations(ctx context.Context, key string) ([]StoreLocation, error)

    // Queries
    ListByStore(ctx context.Context, store string, opts *ListOptions) ([]*CatalogEntry, error)
    ListByTenant(ctx context.Context, tenant string, opts *ListOptions) ([]*CatalogEntry, error)
    ListByPrefix(ctx context.Context, prefix string, opts *ListOptions) ([]*CatalogEntry, error)
    ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) ([]*CatalogEntry, error)
    ListByTag(ctx context.Context, tag, value string, opts *ListOptions) ([]*CatalogEntry, error)
    ListByEncryptionKey(ctx context.Context, keyID string, opts *ListOptions) ([]*CatalogEntry, error)
    Search(ctx context.Context, query *SearchQuery) (*SearchResult, error)

    // Async operations
    EnqueueReplication(ctx context.Context, key string, targets []string) error
    EnqueueArchive(ctx context.Context, key string) error
    EnqueueVerify(ctx context.Context, key string) error
    DequeueReplication(ctx context.Context) (*ReplicationTask, error)
    DequeueArchive(ctx context.Context) (*ArchiveTask, error)

    // Stats
    GetTenantUsage(ctx context.Context, tenant string) (*TenantUsage, error)
    GetStoreUsage(ctx context.Context, store string) (*StoreUsage, error)
    UpdateAccessTime(ctx context.Context, key string) error
}
```

### Key Format

#### Object Keys

Location-agnostic format: `{namespace}:{path}`

```
backups:daily/2024-01-15.tar.gz
tenant-acme:documents/report.pdf
blobs:user/123/avatar.png
system:config/cluster.yaml
```

#### Storage Keys

```
# Primary catalog entry
cat:{key}                              → CatalogEntry (msgpack)

Example:
cat:tenant-acme:documents/report.pdf   → {...}

# Secondary indexes (value is empty, key existence is sufficient)
idx:store:{store}:{key}                → nil
idx:tenant:{tenant}:{key}              → nil
idx:checksum:{hex}:{key}               → nil
idx:status:{status}:{key}              → nil
idx:tag:{tag}:{value}:{key}            → nil
idx:encryption:{keyid}:{key}           → nil

Examples:
idx:store:s3-primary:tenant-acme:documents/report.pdf
idx:tenant:tenant-acme:tenant-acme:documents/report.pdf
idx:status:active:tenant-acme:documents/report.pdf
idx:tag:department:finance:tenant-acme:documents/report.pdf
```

### Queries

#### List by Prefix

```go
// List all objects with prefix
entries, err := catalog.ListByPrefix(ctx, "backups:daily/", &ListOptions{
    Limit:  100,
    Cursor: "",
})
```

#### List by Store

```go
// Find all objects in a specific backend
entries, err := catalog.ListByStore(ctx, "s3-primary", &ListOptions{
    Limit: 100,
})
```

#### List by Tenant

```go
// Find all objects for a tenant
entries, err := catalog.ListByTenant(ctx, "tenant-acme", &ListOptions{
    Limit: 100,
})
```

#### Search

```go
// Complex search
result, err := catalog.Search(ctx, &SearchQuery{
    Prefix:        "tenant-acme:",
    Status:        StatusActive,
    Tags:          map[string]string{"department": "finance"},
    SizeMin:       1000,
    SizeMax:       10000000,
    CreatedAfter:  time.Now().AddDate(0, -1, 0),
    CreatedBefore: time.Now(),
    Sort:          "created_at",
    Order:         "desc",
    Limit:         50,
})
```

### CLI Commands

#### Get Catalog Entry

```bash
# Full catalog entry
quicbaas catalog get tenant-acme:docs/report.pdf

# JSON output
quicbaas catalog get tenant-acme:docs/report.pdf --format json

# Specific fields
quicbaas catalog get tenant-acme:docs/report.pdf --fields key,size,status
```

**Output:**
```
CATALOG ENTRY: tenant-acme:docs/report.pdf
─────────────────────────────────────────────
Object ID:      1234567890
Checksum:       sha256:abc123...
Size:           1.2 MB (1258291 bytes)
Content-Type:   application/pdf
Status:         active
Version:        1

LOCATIONS:
  Primary:      s3-primary
    ObjectRef:  obj/1234567890/v1
    Status:     active
    Stored:     650 KB (compressed)
    Synced:     2024-01-15 10:30:00

  Replica:      gcs-west
    ObjectRef:  obj/1234567890/v1
    Status:     active
    Synced:     2024-01-15 10:35:00

ENCRYPTION:
  Enabled:      Yes
  Key ID:       dek:tenant/acme/objects
  Algorithm:    AES-256-GCM

POLICIES:
  Replication:  2 replicas, async mode
  Archive:      after 90 days to s3-archive
  Retention:    disabled

METADATA:
  Tenant:       tenant-acme
  Owner:        user@example.com
  Tags:         department=finance, classification=internal
  Created:      2024-01-15 10:30:00
  Modified:     2024-01-15 10:30:00
  Last Access:  2024-01-15 12:00:00
```

#### Get Locations

```bash
# All storage locations
quicbaas catalog locations tenant-acme:docs/report.pdf
```

**Output:**
```
STORE         STATUS    SIZE      COMPRESSED  SYNCED
s3-primary    active    650 KB    Yes         2024-01-15 10:30:00
gcs-west      active    650 KB    Yes         2024-01-15 10:35:00
```

#### Search Catalog

```bash
# Search by prefix
quicbaas catalog search --prefix tenant-acme:

# Search by tag
quicbaas catalog search --tag department=finance

# Search by size
quicbaas catalog search --size-min 1MB --size-max 100MB

# Complex search
quicbaas catalog search \
  --prefix tenant-acme: \
  --status active \
  --tag department=finance \
  --created-after 2024-01-01 \
  --sort size \
  --order desc \
  --limit 50
```

#### Update Metadata

```bash
# Add/update tags
quicbaas catalog tag tenant-acme:docs/report.pdf \
  department=finance \
  classification=confidential

# Remove tags
quicbaas catalog untag tenant-acme:docs/report.pdf department

# Set custom metadata
quicbaas catalog set-meta tenant-acme:docs/report.pdf \
  author="John Doe" \
  version="1.2"
```

### REST API

#### Get Catalog Entry

```http
GET /api/v1/catalog/{key}
```

**Response:**
```json
{
  "key": "tenant-acme:docs/report.pdf",
  "object_id": "1234567890",
  "checksum": "sha256:abc123...",
  "size": 1258291,
  "content_type": "application/pdf",
  "primary_store": {
    "store": "s3-primary",
    "object_ref": "obj/1234567890/v1",
    "status": "active",
    "stored_size": 665600,
    "compressed": true,
    "synced_at": "2024-01-15T10:30:00Z"
  },
  "replicas": [
    {
      "store": "gcs-west",
      "object_ref": "obj/1234567890/v1",
      "status": "active",
      "synced_at": "2024-01-15T10:35:00Z"
    }
  ],
  "encryption": {
    "enabled": true,
    "key_id": "dek:tenant/acme/objects",
    "algorithm": "AES-256-GCM"
  },
  "tenant_id": "tenant-acme",
  "tags": {
    "department": "finance"
  },
  "created_at": "2024-01-15T10:30:00Z",
  "modified_at": "2024-01-15T10:30:00Z",
  "last_access_at": "2024-01-15T12:00:00Z",
  "status": "active",
  "version": 1
}
```

#### Get Locations

```http
GET /api/v1/catalog/{key}/locations
```

**Response:**
```json
{
  "key": "tenant-acme:docs/report.pdf",
  "locations": [
    {
      "store": "s3-primary",
      "object_ref": "obj/1234567890/v1",
      "status": "active",
      "stored_size": 665600,
      "compressed": true
    },
    {
      "store": "gcs-west",
      "object_ref": "obj/1234567890/v1",
      "status": "active"
    }
  ]
}
```

#### Search Catalog

```http
POST /api/v1/catalog/search
```

**Request:**
```json
{
  "prefix": "tenant-acme:",
  "status": "active",
  "tags": {
    "department": "finance"
  },
  "size_min": 1000,
  "size_max": 10000000,
  "created_after": "2024-01-01T00:00:00Z",
  "sort": "created_at",
  "order": "desc",
  "limit": 50
}
```

**Response:**
```json
{
  "results": [...],
  "total": 150,
  "next_cursor": "...",
  "has_more": true
}
```

#### Update Tags

```http
PATCH /api/v1/catalog/{key}/tags
```

**Request:**
```json
{
  "add": {
    "department": "finance",
    "classification": "confidential"
  },
  "remove": ["old-tag"]
}
```

### Indexes

#### Available Indexes

| Index | Purpose | Query Pattern |
|-------|---------|---------------|
| `idx:store:{store}:{key}` | Objects by backend | Find all in s3-primary |
| `idx:tenant:{tenant}:{key}` | Objects by tenant | Tenant isolation |
| `idx:checksum:{hex}:{key}` | Deduplication | Find duplicates |
| `idx:status:{status}:{key}` | Objects by status | List archived objects |
| `idx:tag:{tag}:{value}:{key}` | Objects by tag | Search by metadata |
| `idx:encryption:{keyid}:{key}` | Objects by enc key | Key rotation |

#### Index Maintenance

Indexes are updated atomically with catalog entries:

```go
func (c *catalogManager) Put(ctx context.Context, entry *CatalogEntry) error {
    batch := c.db.NewBatch()

    // Write primary entry
    batch.Set(catalogKey(entry.Key), serialize(entry))

    // Write indexes
    batch.Set(storeIndex(entry.PrimaryStore.Store, entry.Key), nil)
    batch.Set(tenantIndex(entry.TenantID, entry.Key), nil)
    batch.Set(statusIndex(entry.Status, entry.Key), nil)
    batch.Set(checksumIndex(entry.Checksum, entry.Key), nil)

    for tag, value := range entry.Tags {
        batch.Set(tagIndex(tag, value, entry.Key), nil)
    }

    if entry.Encryption != nil && entry.Encryption.Enabled {
        batch.Set(encryptionIndex(entry.Encryption.KeyID, entry.Key), nil)
    }

    return batch.Commit()
}
```

### Statistics

#### Tenant Usage

```go
type TenantUsage struct {
    TenantID     string
    TotalBytes   int64
    ObjectCount  int64
    ByBackend    map[string]int64
    ByStatus     map[EntryStatus]int64
}
```

#### Store Usage

```go
type StoreUsage struct {
    StoreName    string
    TotalBytes   int64
    ObjectCount  int64
    ByTenant     map[string]int64
}
```

#### CLI

```bash
# Tenant usage
quicbaas tenant usage tenant-acme

# Store usage
quicbaas backend usage s3-primary
```

### Metrics

```
# Catalog operations
quicbaas_objstore_catalog_puts_total
quicbaas_objstore_catalog_gets_total
quicbaas_objstore_catalog_deletes_total
quicbaas_objstore_catalog_updates_total

# Catalog size
quicbaas_objstore_catalog_entries_total
quicbaas_objstore_catalog_size_bytes

# Query performance
quicbaas_objstore_catalog_query_duration_seconds
quicbaas_objstore_catalog_scan_size

# Index operations
quicbaas_objstore_catalog_index_writes_total
quicbaas_objstore_catalog_index_lookups_total
```
