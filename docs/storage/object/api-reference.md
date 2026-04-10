# Object Storage API Reference

## REST API

Base URL: `/api/v1`

Authentication: Bearer token in `Authorization` header

### Object Operations

#### PUT Object

Store an object in the specified namespace.

```http
PUT /objects/{namespace}/{path...}
```

**Path Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string | Object namespace |
| `path` | string | Object path (can include `/`) |

**Headers**

| Header | Required | Description |
|--------|----------|-------------|
| `Authorization` | Yes | Bearer token |
| `Content-Type` | No | MIME type (default: `application/octet-stream`) |
| `X-QuicBaaS-Backend` | No | Target backend name |
| `X-QuicBaaS-Tenant` | No | Tenant ID |
| `X-QuicBaaS-Encryption-Key` | No | Override encryption key ID |
| `X-QuicBaaS-Tags` | No | Comma-separated key=value tags |

**Request Body**

Raw object data (binary)

**Response**

```json
{
  "key": "backups:daily/2024-01-15.tar.gz",
  "object_id": "1234567890",
  "size": 1048576,
  "checksum": "sha256:abc123...",
  "primary_store": "s3-primary",
  "created_at": "2024-01-15T10:30:00Z"
}
```

**Status Codes**

| Code | Description |
|------|-------------|
| 201 | Object created |
| 400 | Invalid request |
| 401 | Unauthorized |
| 403 | Forbidden (quota exceeded, backend not allowed) |
| 413 | Object too large |
| 500 | Internal error |

---

#### GET Object

Retrieve an object by key.

```http
GET /objects/{namespace}/{path...}
```

**Path Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string | Object namespace |
| `path` | string | Object path |

**Response Headers**

| Header | Description |
|--------|-------------|
| `Content-Type` | Object MIME type |
| `Content-Length` | Object size in bytes |
| `X-QuicBaaS-Checksum` | SHA-256 checksum |
| `X-QuicBaaS-Backend` | Backend object was read from |
| `ETag` | Object version |

**Response Body**

Raw object data (binary)

**Status Codes**

| Code | Description |
|------|-------------|
| 200 | Success |
| 404 | Object not found |
| 401 | Unauthorized |
| 500 | Internal error |

---

#### HEAD Object

Get object metadata without downloading content.

```http
HEAD /objects/{namespace}/{path...}
```

**Response Headers**

Same as GET, but no body returned.

---

#### DELETE Object

Delete an object.

```http
DELETE /objects/{namespace}/{path...}
```

**Query Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `hard` | boolean | Permanent delete (skip soft delete) |

**Response**

```json
{
  "key": "backups:daily/2024-01-15.tar.gz",
  "deleted": true,
  "deleted_at": "2024-01-15T12:00:00Z"
}
```

---

#### List Objects

List objects matching criteria.

```http
GET /objects
```

**Query Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `prefix` | string | Key prefix filter |
| `tenant` | string | Tenant ID filter |
| `backend` | string | Backend filter |
| `status` | string | Status filter (active, archived, deleted) |
| `tag` | string | Tag filter (key=value) |
| `limit` | int | Max results (default: 100, max: 1000) |
| `cursor` | string | Pagination cursor |

**Response**

```json
{
  "objects": [
    {
      "key": "backups:daily/2024-01-15.tar.gz",
      "size": 1048576,
      "content_type": "application/gzip",
      "created_at": "2024-01-15T10:30:00Z",
      "status": "active"
    }
  ],
  "next_cursor": "eyJrZXkiOiJiYWNrdXBzOmRhaWx5LzIwMjQtMDEtMTYudGFyLmd6In0=",
  "has_more": true
}
```

---

#### Search Objects

Advanced object search.

```http
POST /objects/search
```

**Request Body**

```json
{
  "query": {
    "prefix": "tenant-acme:",
    "tags": {
      "department": "finance"
    },
    "size_min": 1000,
    "size_max": 10000000,
    "created_after": "2024-01-01T00:00:00Z",
    "created_before": "2024-12-31T23:59:59Z"
  },
  "sort": {
    "field": "created_at",
    "order": "desc"
  },
  "limit": 50,
  "cursor": ""
}
```

**Response**

```json
{
  "results": [...],
  "total": 150,
  "next_cursor": "...",
  "has_more": true
}
```

---

### Location Management

#### Replicate Object

Add replicas to an existing object.

```http
POST /objects/{key}/replicate
```

**Request Body**

```json
{
  "target_stores": ["gcs-west", "s3-backup"],
  "mode": "async"
}
```

**Response**

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

---

#### Migrate Object

Change the primary backend for an object.

```http
POST /objects/{key}/migrate
```

**Request Body**

```json
{
  "target_store": "gcs-west",
  "delete_from_source": false
}
```

**Response**

```json
{
  "key": "tenant-acme:docs/report.pdf",
  "old_primary": "s3-primary",
  "new_primary": "gcs-west",
  "status": "completed"
}
```

---

#### Archive Object

Move object to archive storage.

```http
POST /objects/{key}/archive
```

**Request Body**

```json
{
  "target_store": "s3-archive",
  "delete_from_primary": true,
  "compression_level": 6
}
```

**Response**

```json
{
  "key": "tenant-acme:docs/old-report.pdf",
  "archive_store": "s3-archive",
  "archived_at": "2024-01-15T12:00:00Z",
  "status": "archived"
}
```

---

### Catalog Operations

#### Get Catalog Entry

Get full catalog entry for an object.

```http
GET /catalog/{key}
```

**Response**

```json
{
  "key": "tenant-acme:docs/report.pdf",
  "object_id": "1234567890",
  "checksum": "sha256:abc123...",
  "size": 1048576,
  "content_type": "application/pdf",
  "primary_store": {
    "store": "s3-primary",
    "object_ref": "obj/1234567890/v1",
    "status": "active",
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
  "archive_store": null,
  "encryption": {
    "enabled": true,
    "key_id": "dek:tenant/acme/objects",
    "algorithm": "AES-256-GCM"
  },
  "replication_policy": {
    "enabled": true,
    "min_replicas": 2,
    "target_stores": ["s3-primary", "gcs-west"],
    "mode": "async"
  },
  "tenant_id": "tenant-acme",
  "tags": {
    "department": "finance"
  },
  "created_at": "2024-01-15T10:30:00Z",
  "modified_at": "2024-01-15T10:30:00Z",
  "last_access_at": "2024-01-15T11:00:00Z",
  "status": "active",
  "version": 1
}
```

---

#### Get Object Locations

Get all storage locations for an object.

```http
GET /catalog/{key}/locations
```

**Response**

```json
{
  "key": "tenant-acme:docs/report.pdf",
  "locations": [
    {
      "store": "s3-primary",
      "object_ref": "obj/1234567890/v1",
      "status": "active",
      "stored_size": 524288,
      "compressed": true,
      "encryption": {
        "key_id": "dek:tenant/acme/objects",
        "algorithm": "AES-256-GCM"
      }
    },
    {
      "store": "gcs-west",
      "object_ref": "obj/1234567890/v1",
      "status": "active"
    }
  ]
}
```

---

### Backend Operations

#### List Backends

List configured storage backends.

```http
GET /backends
```

**Response**

```json
{
  "backends": [
    {
      "name": "s3-primary",
      "type": "s3",
      "healthy": true,
      "config": {
        "bucket": "quicbaas-objects",
        "region": "us-east-1"
      }
    },
    {
      "name": "gcs-west",
      "type": "gcs",
      "healthy": true
    }
  ]
}
```

---

#### Get Backend Status

Get health status for a specific backend.

```http
GET /backends/{name}/status
```

**Response**

```json
{
  "name": "s3-primary",
  "healthy": true,
  "latency_ms": 45,
  "last_check": "2024-01-15T12:00:00Z",
  "message": "OK"
}
```

---

### Tenant Operations

#### Get Tenant Usage

Get storage usage statistics for a tenant.

```http
GET /tenants/{id}/usage
```

**Response**

```json
{
  "tenant_id": "tenant-acme",
  "storage": {
    "total_bytes": 53687091200,
    "object_count": 15000,
    "by_backend": {
      "s3-primary": 42949672960,
      "gcs-west": 10737418240
    }
  },
  "quotas": {
    "max_storage_bytes": 107374182400,
    "max_object_count": 1000000,
    "storage_used_percent": 50.0,
    "objects_used_percent": 1.5
  }
}
```

---

## gRPC API

### Service Definition

```protobuf
syntax = "proto3";
package quicbaas.objectstorage.v1;

service ObjectStorageService {
  // Object operations
  rpc PutObject(PutObjectRequest) returns (PutObjectResponse);
  rpc GetObject(GetObjectRequest) returns (stream GetObjectResponse);
  rpc DeleteObject(DeleteObjectRequest) returns (DeleteObjectResponse);
  rpc HeadObject(HeadObjectRequest) returns (HeadObjectResponse);

  // Bulk operations
  rpc ListObjects(ListObjectsRequest) returns (ListObjectsResponse);
  rpc SearchObjects(SearchObjectsRequest) returns (SearchObjectsResponse);

  // Location management
  rpc Replicate(ReplicateRequest) returns (ReplicateResponse);
  rpc Migrate(MigrateRequest) returns (MigrateResponse);
  rpc Archive(ArchiveRequest) returns (ArchiveResponse);

  // Catalog
  rpc GetCatalogEntry(GetCatalogEntryRequest) returns (CatalogEntry);
  rpc GetLocations(GetLocationsRequest) returns (GetLocationsResponse);

  // Backends
  rpc ListBackends(ListBackendsRequest) returns (ListBackendsResponse);
  rpc GetBackendStatus(GetBackendStatusRequest) returns (BackendStatus);

  // Tenants
  rpc GetTenantUsage(GetTenantUsageRequest) returns (TenantUsage);
}
```

### Message Types

```protobuf
message PutObjectRequest {
  string key = 1;
  bytes data = 2;
  string content_type = 3;
  string target_backend = 4;
  string tenant_id = 5;
  string encryption_key_id = 6;
  map<string, string> tags = 7;
  ReplicationPolicy replication_policy = 8;
  ArchivePolicy archive_policy = 9;
}

message PutObjectResponse {
  string key = 1;
  uint64 object_id = 2;
  int64 size = 3;
  bytes checksum = 4;
  string primary_store = 5;
  int64 created_at = 6;
}

message GetObjectRequest {
  string key = 1;
}

message GetObjectResponse {
  oneof content {
    ObjectMetadata metadata = 1;
    bytes chunk = 2;
  }
}

message ObjectMetadata {
  string key = 1;
  int64 size = 2;
  string content_type = 3;
  bytes checksum = 4;
  string backend = 5;
}

message CatalogEntry {
  string key = 1;
  uint64 object_id = 2;
  bytes checksum = 3;
  int64 size = 4;
  string content_type = 5;
  StoreLocation primary_store = 6;
  repeated StoreLocation replicas = 7;
  StoreLocation archive_store = 8;
  ObjectEncryption encryption = 9;
  ReplicationPolicy replication_policy = 10;
  ArchivePolicy archive_policy = 11;
  string tenant_id = 12;
  uint64 shard_id = 13;
  map<string, string> tags = 14;
  int64 created_at = 15;
  int64 modified_at = 16;
  int64 last_access_at = 17;
  string status = 18;
  uint64 version = 19;
}

message StoreLocation {
  string store = 1;
  string object_ref = 2;
  string status = 3;
  bytes checksum = 4;
  int64 stored_size = 5;
  bool compressed = 6;
  LocationEncryption encryption = 7;
  int64 synced_at = 8;
}
```

---

## MCP Tools

### quicbaas_object_put

Store an object.

**Input Schema**

```json
{
  "type": "object",
  "properties": {
    "key": {
      "type": "string",
      "description": "Object key (namespace:path)"
    },
    "data": {
      "type": "string",
      "description": "Base64-encoded object data"
    },
    "content_type": {
      "type": "string"
    },
    "backend": {
      "type": "string"
    },
    "tags": {
      "type": "object"
    }
  },
  "required": ["key", "data"]
}
```

### quicbaas_object_get

Retrieve an object.

**Input Schema**

```json
{
  "type": "object",
  "properties": {
    "key": {
      "type": "string",
      "description": "Object key"
    }
  },
  "required": ["key"]
}
```

### quicbaas_object_list

List objects matching criteria.

**Input Schema**

```json
{
  "type": "object",
  "properties": {
    "prefix": {
      "type": "string"
    },
    "tenant": {
      "type": "string"
    },
    "limit": {
      "type": "integer",
      "default": 100
    }
  }
}
```

### quicbaas_catalog_get

Get catalog entry for an object.

**Input Schema**

```json
{
  "type": "object",
  "properties": {
    "key": {
      "type": "string",
      "description": "Object key"
    }
  },
  "required": ["key"]
}
```

---

## Admin REST API

Base URL: `/api/v1/admin`

### Backend Management

#### Create Backend

```http
POST /admin/object-backends
```

**Request Body**

```json
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

#### Update Backend

```http
PUT /admin/object-backends/{name}
```

#### Delete Backend

```http
DELETE /admin/object-backends/{name}
```

**Query Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `migrate_to` | string | Backend to migrate objects to |
| `force` | boolean | Force delete with data loss |

#### Set Backend Status

```http
PUT /admin/object-backends/{name}/status
```

### Storage Statistics

```http
GET /admin/storage/stats
```

**Query Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `backends` | boolean | Include per-backend breakdown |
| `tenants` | boolean | Include per-tenant breakdown |

### Garbage Collection

#### Run GC

```http
POST /admin/storage/gc
```

**Request Body**

```json
{
  "backends": ["s3-primary"],
  "dry_run": false,
  "aggressive": false
}
```

#### Get GC Progress

```http
GET /admin/storage/gc/progress?operation_id={id}
```

### Object Repair

```http
POST /admin/objects/repair
```

**Request Body**

```json
{
  "key": "namespace:path/to/object",
  "source_location": "s3-primary",
  "timeout_ms": 30000
}
```

### Compaction

```http
POST /admin/object-backends/{name}/compact
```

**Request Body**

```json
{
  "aggressive": false
}
```

---

## Admin gRPC API

### Service Definition

```protobuf
service ObjectStorageAdminService {
  // Backend Management
  rpc CreateBackend(CreateBackendRequest) returns (CreateBackendResponse);
  rpc UpdateBackend(UpdateBackendRequest) returns (UpdateBackendResponse);
  rpc DeleteBackend(DeleteBackendRequest) returns (DeleteBackendResponse);
  rpc SetBackendStatus(SetBackendStatusRequest) returns (SetBackendStatusResponse);

  // Statistics
  rpc GetStorageStats(GetStorageStatsRequest) returns (GetStorageStatsResponse);

  // Garbage Collection
  rpc RunGarbageCollection(RunGarbageCollectionRequest) returns (RunGarbageCollectionResponse);
  rpc GetGarbageCollectionProgress(GetGarbageCollectionProgressRequest) returns (GetGarbageCollectionProgressResponse);

  // Repair
  rpc RepairObject(RepairObjectRequest) returns (RepairObjectResponse);

  // Compaction
  rpc CompactBackend(CompactBackendRequest) returns (CompactBackendResponse);
}
```

### Admin Message Types

```protobuf
message CreateBackendRequest {
  string name = 1;
  BackendType type = 2;
  StorageTier tier = 3;
  map<string, string> config = 4;
  bool enabled = 5;
}

message DeleteBackendRequest {
  string name = 1;
  string migrate_to = 2;
  bool force = 3;
}

message RunGarbageCollectionRequest {
  repeated string backends = 1;
  bool dry_run = 2;
  bool aggressive = 3;
}

message GetGarbageCollectionProgressResponse {
  string operation_id = 1;
  string status = 2;
  int64 objects_scanned = 3;
  int64 objects_deleted = 4;
  int64 bytes_reclaimed = 5;
  double percent_complete = 6;
  string error = 7;
  google.protobuf.Timestamp completed_at = 8;
}

message RepairObjectRequest {
  string key = 1;
  string source_location = 2;
  int64 timeout_ms = 3;
}

message RepairObjectResponse {
  string key = 1;
  bool success = 2;
  repeated string repaired_locations = 3;
  string error = 4;
}
```

---

## Admin MCP Tools

### quicbaas_object_admin_create_backend

Create a new storage backend.

**Input Schema**

```json
{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "type": {"type": "string", "enum": ["local", "s3", "gcs", "azure", "memory"]},
    "tier": {"type": "string", "enum": ["hot", "warm", "cold"]},
    "config": {"type": "object"},
    "enabled": {"type": "boolean", "default": true}
  },
  "required": ["name", "type", "tier"]
}
```

### quicbaas_object_admin_run_gc

Initiate garbage collection.

**Input Schema**

```json
{
  "type": "object",
  "properties": {
    "backends": {"type": "array", "items": {"type": "string"}},
    "dry_run": {"type": "boolean", "default": false},
    "aggressive": {"type": "boolean", "default": false}
  }
}
```

### quicbaas_object_admin_repair_object

Repair an object with inconsistent replicas.

**Input Schema**

```json
{
  "type": "object",
  "properties": {
    "key": {"type": "string"},
    "source_location": {"type": "string"},
    "timeout_ms": {"type": "integer", "default": 30000}
  },
  "required": ["key"]
}
```

### quicbaas_object_admin_compact_backend

Trigger compaction on a storage backend.

**Input Schema**

```json
{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "aggressive": {"type": "boolean", "default": false}
  },
  "required": ["name"]
}
```

---

## QUIC Opcodes

### Object Operations

| Opcode | Name | Description |
|--------|------|-------------|
| `0x28` | OpOSPutObject | Store object |
| `0x29` | OpOSGetObject | Retrieve object |
| `0x2A` | OpOSDeleteObject | Delete object |
| `0x2B` | OpOSHeadObject | Get object metadata |
| `0x2C` | OpOSListObjects | List objects |
| `0x2D` | OpOSListByTag | List by tag |
| `0x2E` | OpOSListByTenant | List by tenant |
| `0x2F` | OpOSCopyObject | Copy object |
| `0x32` | OpOSReplicateObject | Replicate object |
| `0x33` | OpOSMigrateObject | Migrate object |
| `0x34` | OpOSArchiveObject | Archive object |
| `0x35` | OpOSRestoreObject | Restore object |
| `0x36` | OpOSVerifyObject | Verify object |
| `0x37` | OpOSGetObjectMetadata | Get metadata |
| `0x38` | OpOSUpdateObjectMetadata | Update metadata |
| `0x39` | OpOSGetStorageStats | Get storage stats |
| `0x3A` | OpOSSetLifecyclePolicy | Set lifecycle |
| `0x3B` | OpOSGetLifecyclePolicy | Get lifecycle |
| `0x5E` | OpOSGetObjectPolicy | Get object policy |
| `0x5F` | OpOSListBackends | List backends |
| `0x93` | OpOSGetBackendHealth | Get backend health |
| `0x94` | OpOSSetTenantQuotas | Set tenant quotas |
| `0xFA` | OpOSBatchPutObjects | Batch put |
| `0xFB` | OpOSBatchGetObjects | Batch get |
| `0xFC` | OpOSBatchDeleteObjects | Batch delete |

### Admin Operations

| Opcode | Name | Description |
|--------|------|-------------|
| `0xCF` | OpOSAdminCreateBackend | Create backend |
| `0xFD` | OpOSAdminUpdateBackend | Update backend |
| `0xFE` | OpOSAdminDeleteBackend | Delete backend |
| `0xFF` | OpOSAdminSetBackendStatus | Set backend status |
| `0x8F` | OpOSAdminGetStorageStats | Get storage stats |
| `0xA9` | OpOSAdminRunGC | Run garbage collection |
| `0xAA` | OpOSAdminGetGCProgress | Get GC progress |

---

## Error Codes

| Code | HTTP | gRPC | Description |
|------|------|------|-------------|
| `OBJECT_NOT_FOUND` | 404 | NOT_FOUND | Object does not exist |
| `OBJECT_EXISTS` | 409 | ALREADY_EXISTS | Object already exists |
| `BACKEND_UNAVAILABLE` | 503 | UNAVAILABLE | Backend is unhealthy |
| `BACKEND_NOT_FOUND` | 404 | NOT_FOUND | Backend does not exist |
| `BACKEND_EXISTS` | 409 | ALREADY_EXISTS | Backend name in use |
| `QUOTA_EXCEEDED` | 403 | RESOURCE_EXHAUSTED | Tenant quota exceeded |
| `OBJECT_TOO_LARGE` | 413 | RESOURCE_EXHAUSTED | Object exceeds size limit |
| `INVALID_KEY` | 400 | INVALID_ARGUMENT | Invalid object key format |
| `BACKEND_NOT_ALLOWED` | 403 | PERMISSION_DENIED | Tenant cannot use backend |
| `ENCRYPTION_FAILED` | 500 | INTERNAL | Encryption operation failed |
| `CHECKSUM_MISMATCH` | 500 | DATA_LOSS | Object integrity check failed |
| `GC_OPERATION_NOT_FOUND` | 404 | NOT_FOUND | GC operation ID not found |
| `OBJECTS_EXIST` | 400 | FAILED_PRECONDITION | Backend contains objects |
