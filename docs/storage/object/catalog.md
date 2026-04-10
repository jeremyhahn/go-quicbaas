# Object Catalog

The internal catalog is the source of truth for all object metadata and locations.

## Overview

The catalog provides:
- Unified view of all object locations across backends
- Secondary indexes for efficient querying
- Policy storage (replication, archive, retention)
- Multi-tenant isolation
- Versioning and optimistic concurrency

## Catalog Architecture

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

## Catalog Entry Schema

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

## Catalog Manager Interface

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

## Key Format

### Object Keys

Location-agnostic format: `{namespace}:{path}`

```
backups:daily/2024-01-15.tar.gz
tenant-acme:documents/report.pdf
blobs:user/123/avatar.png
system:config/cluster.yaml
```

### Storage Keys

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

## Queries

### List by Prefix

```go
// List all objects with prefix
entries, err := catalog.ListByPrefix(ctx, "backups:daily/", &ListOptions{
    Limit:  100,
    Cursor: "",
})
```

### List by Store

```go
// Find all objects in a specific backend
entries, err := catalog.ListByStore(ctx, "s3-primary", &ListOptions{
    Limit: 100,
})
```

### List by Tenant

```go
// Find all objects for a tenant
entries, err := catalog.ListByTenant(ctx, "tenant-acme", &ListOptions{
    Limit: 100,
})
```

### Search

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

## CLI Commands

### Get Catalog Entry

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

### Get Locations

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

### Search Catalog

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

### Update Metadata

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

## REST API

### Get Catalog Entry

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

### Get Locations

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

### Search Catalog

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

### Update Tags

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

## Indexes

### Available Indexes

| Index | Purpose | Query Pattern |
|-------|---------|---------------|
| `idx:store:{store}:{key}` | Objects by backend | Find all in s3-primary |
| `idx:tenant:{tenant}:{key}` | Objects by tenant | Tenant isolation |
| `idx:checksum:{hex}:{key}` | Deduplication | Find duplicates |
| `idx:status:{status}:{key}` | Objects by status | List archived objects |
| `idx:tag:{tag}:{value}:{key}` | Objects by tag | Search by metadata |
| `idx:encryption:{keyid}:{key}` | Objects by enc key | Key rotation |

### Index Maintenance

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

## Statistics

### Tenant Usage

```go
type TenantUsage struct {
    TenantID     string
    TotalBytes   int64
    ObjectCount  int64
    ByBackend    map[string]int64
    ByStatus     map[EntryStatus]int64
}
```

### Store Usage

```go
type StoreUsage struct {
    StoreName    string
    TotalBytes   int64
    ObjectCount  int64
    ByTenant     map[string]int64
}
```

### CLI

```bash
# Tenant usage
quicbaas tenant usage tenant-acme

# Store usage
quicbaas backend usage s3-primary
```

## Metrics

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
