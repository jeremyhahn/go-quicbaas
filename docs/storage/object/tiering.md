# Object Storage Tiering

Automatic storage tiering and archival based on access patterns and policies.

## Overview

Storage tiering provides:
- Automatic movement of objects between storage tiers
- Access-pattern based archival decisions
- Cost optimization through cold storage
- Configurable retention policies
- Transparent retrieval from any tier

## Tiering Architecture

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

## Configuration

### Archive Policy

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

### Per-Tenant Tiering

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

### Retention Policy

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

## Archive Policy

### Policy Structure

```go
type ArchivePolicy struct {
    Enabled          bool    // Enable automatic archival
    ArchiveAfterDays int     // Days since last access
    TargetStore      string  // Archive backend name
    DeleteFromPrimary bool   // Remove from primary after archive
    CompressionLevel int     // 0=none, 1-9=level
}
```

### Catalog Entry

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

## Archive Worker

### Worker Architecture

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

## Retention Policy

### Policy Structure

```go
type RetentionPolicy struct {
    Enabled       bool   // Enable retention enforcement
    RetentionDays int    // Days to retain (0 = infinite)
    MaxVersions   int    // Maximum versions to keep (0 = unlimited)
}
```

### Retention Enforcement

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

## Tiering Operations

### Archive Object

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

### Restore Object

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

### Lifecycle Transitions

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

## CLI Commands

### Archive Object

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

### Restore Object

```bash
# Restore to hot tier
quicbaas object restore tenant-acme:docs/old-report.pdf

# Restore to specific backend
quicbaas object restore tenant-acme:docs/old-report.pdf --target s3-primary

# Check restore status (for slow backends like Glacier)
quicbaas object restore-status tenant-acme:docs/old-report.pdf
```

### View Tier Status

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

## REST API

### Archive Object

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

### Restore Object

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

### Check Restore Status

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

## Storage Classes

### S3 Storage Classes

| Class | Use Case | Retrieval Time | Min Duration |
|-------|----------|----------------|--------------|
| STANDARD | Frequently accessed | Immediate | None |
| STANDARD_IA | Infrequently accessed | Immediate | 30 days |
| GLACIER | Archive | 3-5 hours | 90 days |
| GLACIER_DEEP_ARCHIVE | Long-term archive | 12 hours | 180 days |

### GCS Storage Classes

| Class | Use Case | Retrieval Time | Min Duration |
|-------|----------|----------------|--------------|
| STANDARD | Frequently accessed | Immediate | None |
| NEARLINE | Monthly access | Immediate | 30 days |
| COLDLINE | Quarterly access | Immediate | 90 days |
| ARCHIVE | Yearly access | Immediate | 365 days |

### Azure Blob Tiers

| Tier | Use Case | Retrieval Time | Min Duration |
|------|----------|----------------|--------------|
| Hot | Frequently accessed | Immediate | None |
| Cool | Infrequently accessed | Immediate | 30 days |
| Archive | Rarely accessed | Hours | 180 days |

## Compression

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

## Metrics

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

## Troubleshooting

### Common Issues

| Issue | Cause | Resolution |
|-------|-------|------------|
| Slow restore | Glacier retrieval | Use expedited retrieval |
| Archive failed | Backend unreachable | Check backend health |
| Retention not enforced | Worker not running | Check retention worker |
| Unexpected deletion | Retention policy | Review retention settings |

### Debugging

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
