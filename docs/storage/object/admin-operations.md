# Object Storage Admin Operations

Administrative operations for managing backends, garbage collection, object repair, and compaction.

## Backend Management

### Create Backend

Register a new storage backend.

**CLI**

```bash
quicbaas object admin backend create s3-west \
  --type s3 \
  --tier hot \
  --config bucket=my-bucket,region=us-west-2 \
  --enabled
```

**REST API**

```http
POST /api/v1/admin/object-backends
Content-Type: application/json
Authorization: Bearer {token}

{
  "name": "s3-west",
  "type": "s3",
  "tier": "hot",
  "config": {
    "bucket": "my-bucket",
    "region": "us-west-2"
  },
  "enabled": true
}
```

**Response**

```json
{
  "name": "s3-west",
  "created_at": 1704067200000000000
}
```

**gRPC**

```protobuf
rpc CreateBackend(CreateBackendRequest) returns (CreateBackendResponse);
```

**QUIC Opcode**: `0xCF` (OpOSAdminCreateBackend)

**MCP Tool**: `quicbaas_object_admin_create_backend`

### Update Backend

Update backend configuration.

**CLI**

```bash
quicbaas object admin backend update s3-west \
  --tier warm \
  --config prefix=archive/
```

**REST API**

```http
PUT /api/v1/admin/object-backends/{name}
Content-Type: application/json
Authorization: Bearer {token}

{
  "config": {
    "prefix": "archive/"
  },
  "tier": "warm"
}
```

**Response**

```json
{
  "name": "s3-west",
  "updated_at": 1704153600000000000
}
```

**gRPC**

```protobuf
rpc UpdateBackend(UpdateBackendRequest) returns (UpdateBackendResponse);
```

**QUIC Opcode**: `0xFD` (OpOSAdminUpdateBackend)

**MCP Tool**: `quicbaas_object_admin_update_backend`

### Delete Backend

Remove a storage backend. Requires migration or force flag if objects exist.

**CLI**

```bash
# Migrate objects to another backend before deletion
quicbaas object admin backend delete s3-old --migrate-to s3-new

# Force delete (data loss)
quicbaas object admin backend delete s3-old --force
```

**REST API**

```http
DELETE /api/v1/admin/object-backends/{name}
Content-Type: application/json
Authorization: Bearer {token}

{
  "migrate_to": "s3-new",
  "force": false
}
```

**Response**

```json
{
  "name": "s3-old",
  "deleted": true,
  "migrated_objects": 1500
}
```

**gRPC**

```protobuf
rpc DeleteBackend(DeleteBackendRequest) returns (DeleteBackendResponse);
```

**QUIC Opcode**: `0xFE` (OpOSAdminDeleteBackend)

**MCP Tool**: `quicbaas_object_admin_delete_backend`

### Set Backend Status

Enable or disable a backend.

**CLI**

```bash
# Disable backend (stops new writes, reads still work)
quicbaas object admin backend disable s3-maintenance

# Enable backend
quicbaas object admin backend enable s3-maintenance
```

**REST API**

```http
PUT /api/v1/admin/object-backends/{name}/status
Content-Type: application/json
Authorization: Bearer {token}

{
  "enabled": false
}
```

**Response**

```json
{
  "name": "s3-maintenance",
  "enabled": false,
  "updated_at": 1704240000000000000
}
```

**gRPC**

```protobuf
rpc SetBackendStatus(SetBackendStatusRequest) returns (SetBackendStatusResponse);
```

**QUIC Opcode**: `0xFF` (OpOSAdminSetBackendStatus)

**MCP Tool**: `quicbaas_object_admin_set_backend_status`

## Garbage Collection

Remove orphaned objects that exist in backends but not in the catalog.

### Run Garbage Collection

**CLI**

```bash
# GC all backends
quicbaas object admin gc run

# GC specific backends
quicbaas object admin gc run --backends s3-primary,gcs-west

# Dry run (report only)
quicbaas object admin gc run --dry-run

# Aggressive mode
quicbaas object admin gc run --aggressive
```

**REST API**

```http
POST /api/v1/admin/storage/gc
Content-Type: application/json
Authorization: Bearer {token}

{
  "backends": ["s3-primary", "gcs-west"],
  "dry_run": false,
  "aggressive": false
}
```

**Response**

```json
{
  "operation_id": "gc-abc123",
  "started_at": 1704067200000000000
}
```

**gRPC**

```protobuf
rpc RunGarbageCollection(RunGarbageCollectionRequest) returns (RunGarbageCollectionResponse);
```

**QUIC Opcode**: `0xA9` (OpOSAdminRunGC)

**MCP Tool**: `quicbaas_object_admin_run_gc`

### Get GC Progress

**CLI**

```bash
quicbaas object admin gc status gc-abc123
```

**REST API**

```http
GET /api/v1/admin/storage/gc/progress?operation_id=gc-abc123
Authorization: Bearer {token}
```

**Response**

```json
{
  "operation_id": "gc-abc123",
  "status": "running",
  "objects_scanned": 50000,
  "objects_deleted": 150,
  "bytes_reclaimed": 15728640,
  "percent_complete": 75.5,
  "completed_at": null,
  "error": ""
}
```

**Status Values**

| Status | Description |
|--------|-------------|
| `running` | GC operation in progress |
| `completed` | GC completed successfully |
| `failed` | GC failed with error |

**gRPC**

```protobuf
rpc GetGarbageCollectionProgress(GetGarbageCollectionProgressRequest) returns (GetGarbageCollectionProgressResponse);
```

**QUIC Opcode**: `0xAA` (OpOSAdminGetGCProgress)

**MCP Tool**: `quicbaas_object_admin_get_gc_progress`

## Object Repair

Repair objects with inconsistent replicas across backends.

### Repair Object

**CLI**

```bash
# Auto-detect source from checksums
quicbaas object admin repair tenant-acme:docs/report.pdf

# Specify source backend
quicbaas object admin repair tenant-acme:docs/report.pdf \
  --source-location s3-primary
```

**REST API**

```http
POST /api/v1/admin/objects/repair
Content-Type: application/json
Authorization: Bearer {token}

{
  "key": "tenant-acme:docs/report.pdf",
  "source_location": "s3-primary",
  "timeout_ms": 30000
}
```

**Response**

```json
{
  "key": "tenant-acme:docs/report.pdf",
  "success": true,
  "repaired_locations": ["gcs-west", "azure-backup"],
  "error": ""
}
```

**Repair Process**

1. Read object from source location
2. Compute SHA-256 checksum
3. Compare with each replica
4. Re-copy data to locations with mismatched checksums

**gRPC**

```protobuf
rpc RepairObject(RepairObjectRequest) returns (RepairObjectResponse);
```

**QUIC Opcode**: `0xAB` (OpOSAdminRepairObject)

**MCP Tool**: `quicbaas_object_admin_repair_object`

## Compaction

Trigger compaction on storage backends that support it.

### Compact Backend

**CLI**

```bash
# Standard compaction
quicbaas object admin compact local-store

# Aggressive compaction
quicbaas object admin compact local-store --aggressive
```

**REST API**

```http
POST /api/v1/admin/object-backends/{name}/compact
Content-Type: application/json
Authorization: Bearer {token}

{
  "aggressive": false
}
```

**Response**

```json
{
  "name": "local-store",
  "operation_id": "compact-xyz789",
  "started_at": 1704067200000000000
}
```

**Notes**

- Compaction is primarily relevant for local/embedded storage backends
- Cloud backends (S3, GCS, Azure) handle compaction automatically
- Aggressive mode uses more resources but reclaims more space

**gRPC**

```protobuf
rpc CompactBackend(CompactBackendRequest) returns (CompactBackendResponse);
```

**QUIC Opcode**: `0xAC` (OpOSAdminCompactBackend)

**MCP Tool**: `quicbaas_object_admin_compact_backend`

## Storage Statistics

### Get Storage Stats

**CLI**

```bash
# Summary only
quicbaas object admin stats

# Include per-backend breakdown
quicbaas object admin stats --backends

# Include per-tenant breakdown
quicbaas object admin stats --tenants
```

**REST API**

```http
GET /api/v1/admin/storage/stats?backends=true&tenants=true
Authorization: Bearer {token}
```

**Response**

```json
{
  "total_bytes": 107374182400,
  "total_objects": 150000,
  "total_backends": 5,
  "healthy_backends": 5,
  "backend_stats": [
    {
      "name": "s3-primary",
      "type": "s3",
      "enabled": true,
      "tier": "hot",
      "object_count": 100000,
      "total_size": 64424509440,
      "health": {
        "healthy": true,
        "latency": 45,
        "last_check": 1704240000000000000
      }
    }
  ],
  "tenant_stats": [
    {
      "tenant_id": "tenant-acme",
      "total_bytes": 21474836480,
      "object_count": 30000,
      "hot_bytes": 16106127360,
      "warm_bytes": 4294967296,
      "cold_bytes": 1073741824,
      "archive_bytes": 0
    }
  ],
  "calculated_at": 1704240000000000000
}
```

**gRPC**

```protobuf
rpc GetStorageStats(GetStorageStatsRequest) returns (GetStorageStatsResponse);
```

**QUIC Opcode**: `0x8F` (OpOSAdminGetStorageStats)

**MCP Tool**: `quicbaas_object_admin_get_storage_stats`

## Metrics

Admin operations expose Prometheus metrics:

```prometheus
# Backend operations
quicbaas_objectstorage_admin_backends_created_total
quicbaas_objectstorage_admin_backends_deleted_total

# Maintenance operations
quicbaas_objectstorage_admin_gc_operations_total
quicbaas_objectstorage_admin_repairs_total
quicbaas_objectstorage_admin_compactions_total

# Operation timing
quicbaas_objectstorage_admin_operation_duration_seconds{operation="create_backend"}
quicbaas_objectstorage_admin_operation_duration_seconds{operation="run_gc"}
quicbaas_objectstorage_admin_operation_duration_seconds{operation="repair_object"}

# Errors
quicbaas_objectstorage_admin_operation_errors_total{operation="...", error_type="..."}
```

## Error Codes

| Code | HTTP | Description |
|------|------|-------------|
| `BACKEND_NOT_FOUND` | 404 | Backend does not exist |
| `BACKEND_EXISTS` | 409 | Backend name already in use |
| `OBJECT_NOT_FOUND` | 404 | Object key not found |
| `GC_OPERATION_NOT_FOUND` | 404 | GC operation ID not found |
| `OBJECTS_EXIST` | 400 | Backend contains objects (use migrate_to or force) |
| `MIGRATION_TARGET_NOT_FOUND` | 400 | Migration target backend not found |
| `SOURCE_UNAVAILABLE` | 503 | Source backend unavailable for repair |
| `PERMISSION_DENIED` | 403 | Insufficient admin permissions |

## Best Practices

1. **Schedule GC during low-traffic periods** - GC scans all objects and can impact performance
2. **Use dry-run first** - Verify what will be deleted before actual GC
3. **Monitor backend health** - Disable unhealthy backends to prevent data loss
4. **Migrate before delete** - Always migrate objects when removing backends
5. **Regular compaction** - Schedule compaction for local storage backends
6. **Verify repairs** - Check repair results and investigate root cause

## See Also

- [Backends](backends.md) - Backend configuration
- [Replication](replication.md) - Replication strategies
- [Tiering](tiering.md) - Storage tier policies
- [CLI Usage](cli-usage.md) - Complete CLI reference
