# Object Storage — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/object-storage/`.

Distributed object storage with location-agnostic keys, multi-backend support, and automatic replication.

## Key Features

| Feature | Description |
|---------|-------------|
| Location-Agnostic Keys | Object identity (`namespace:path`) separate from storage location |
| Internal Catalog | Source of truth tracking all object locations, replicas, and archives |
| Multi-Backend Support | S3, GCS, Azure Blob, MinIO, and local filesystem |
| Automatic Replication | Sync/async replication across backends with configurable policies |
| Storage Tiering | Automatic archival to cold storage based on access patterns |
| Transparent Encryption | KMS-integrated encryption with hierarchical key management |
| Multi-Tenancy | Per-tenant quotas, isolation, and custom configurations |
| Three-Layer Config | Cluster defaults → Node overrides → Tenant/Shard policies |

## Architecture Overview

```
┌───────────────────────────────────────────────────────────────┐
│                        Client Request                         │
│                 PUT tenant-acme:docs/report.pdf               │
└───────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌───────────────────────────────────────────────────────────────┐
│                    Object Storage Service                     │
│  ┌─────────────┐  ┌─────────────┐  ┌───────────────────────┐  │
│  │   Config    │  │  IAM/RBAC   │  │    KMS Encryption     │  │
│  │  Resolver   │  │    Check    │  │    (if enabled)       │  │
│  └─────────────┘  └─────────────┘  └───────────────────────┘  │
└───────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌───────────────────────────────────────────────────────────────┐
│                  Enhanced Object Store FSM                    │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │                    Catalog Manager                      │  │
│  │  - Tracks ALL object locations (primary, replicas)      │  │
│  │  - Manages policies (replication, archive, retention)   │  │
│  │  - Provides secondary indexes for queries               │  │
│  └─────────────────────────────────────────────────────────┘  │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │                    Backend Manager                      │  │
│  │  - Multi-backend access (S3, GCS, Azure, Local, MinIO)  │  │
│  │  - Health monitoring and failover                       │  │
│  │  - Backend selection based on criteria                  │  │
│  └─────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────┘
                               │
                 ┌─────────────┼─────────────┐
                 ▼             ▼             ▼
           ┌──────────┐  ┌──────────┐  ┌──────────┐
           │    S3    │  │   GCS    │  │  Local   │
           │ Primary  │  │ Replica  │  │  Cache   │
           └──────────┘  └──────────┘  └──────────┘
```

## Quick Start

### Store an Object

```bash
# Store object using CLI
quicbaas object put backups/daily/2024-01-15.tar.gz --file ./backup.tar.gz

# Store with content type and tags
quicbaas object put docs/report.pdf --file ./report.pdf \
  --content-type application/pdf \
  --tag department=finance \
  --tag classification=internal

# Place a copy on a specific backend after writing
quicbaas object replicate images/logo.png --dest s3-primary
```

### Retrieve an Object

```bash
# Get object
quicbaas object get docs/report.pdf -o ./downloaded.pdf

# Get object metadata only
quicbaas object head docs/report.pdf

# Get policies and metadata
quicbaas object metadata get docs/report.pdf
```

The tenant comes from the global `--tenant` flag, not from the key. Every verb
and flag is in [cli.md](cli.md).

### REST API

```bash
# Store object
curl -X PUT https://quicbaas:8080/api/v1/objects/backups/daily/2024-01-15.tar.gz \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @backup.tar.gz

# Get object
curl https://quicbaas:8080/api/v1/objects/backups/daily/2024-01-15.tar.gz \
  -H "Authorization: Bearer $TOKEN" \
  -o backup.tar.gz

# List objects
curl "https://quicbaas:8080/api/v1/objects?prefix=backups:daily/&limit=100" \
  -H "Authorization: Bearer $TOKEN"
```

## Object Key Format

Objects are identified by location-agnostic keys in the format `{namespace}:{path}`:

| Example Key | Description |
|-------------|-------------|
| `backups:daily/2024-01-15.tar.gz` | Daily backup file |
| `tenant-acme:documents/report.pdf` | Tenant-specific document |
| `blobs:user/123/avatar.png` | User avatar image |
| `system:config/cluster.yaml` | System configuration |

The key identifies **what** the object is. The catalog tracks **where** it's stored.

## Supported Backends

| Backend | Type | Use Case |
|---------|------|----------|
| `local` | Local filesystem | Development, edge nodes, caching |
| `s3` | Amazon S3 | Primary cloud storage |
| `gcs` | Google Cloud Storage | GCP deployments |
| `azure` | Azure Blob Storage | Azure deployments |
| `minio` | MinIO | Self-hosted S3-compatible |
| `memory` | In-memory | Testing only |

## Configuration Layers

Object storage uses a three-layer configuration hierarchy:

```
Cluster Defaults (object_storage.defaults)
       ↓
Node Overrides (server.node_object_storage)
       ↓
Tenant/Shard Config (object_storage.tenants.{id})
```

See Configuration Reference for details.

## API Coverage

| Operation | gRPC | REST | QUIC | MCP | CLI |
|-----------|------|------|------|-----|-----|
| PutObject | Y | Y | Y | Y | Y |
| GetObject | Y | Y | Y | Y | Y |
| DeleteObject | Y | Y | Y | Y | Y |
| HeadObject | Y | Y | Y | Y | Y |
| ListObjects | Y | Y | Y | Y | Y |
| SearchObjects | Y | Y | - | - | Y |
| CopyObject | Y | Y | Y | Y | Y |
| ReplicateObject | Y | Y | Y | Y | Y |
| MigrateObject | Y | Y | Y | Y | Y |
| ArchiveObject | Y | Y | Y | Y | Y |
| RestoreObject | Y | Y | Y | Y | Y |
| VerifyObject | Y | Y | Y | Y | Y |
| GetCatalogEntry | Y | Y | - | Y | Y |
| ListBackends | Y | Y | Y | - | Y |
| GetBackendHealth | Y | Y | Y | - | Y |
| BatchPutObjects | Y | Y | Y | - | - |
| BatchGetObjects | Y | Y | Y | - | - |
| BatchDeleteObjects | Y | Y | Y | - | - |

### Admin Operations

| Operation | gRPC | REST | QUIC | MCP | CLI |
|-----------|------|------|------|-----|-----|
| CreateBackend | Y | Y | Y | Y | Y |
| UpdateBackend | Y | Y | Y | Y | Y |
| DeleteBackend | Y | Y | Y | Y | Y |
| SetBackendStatus | Y | Y | Y | Y | Y |
| GetStorageStats | Y | Y | Y | Y | Y |
| RunGarbageCollection | Y | Y | Y | Y | Y |
| GetGCProgress | Y | Y | Y | Y | Y |
| RepairObject | Y | Y | Y | Y | Y |
| CompactBackend | Y | Y | Y | Y | Y |

## Documentation

### Core Documentation

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | System design, catalog schema, data flow |
| Configuration | Three-layer config reference |
| API Reference | REST, gRPC, and MCP APIs |
| CLI Usage | Command-line interface reference |

### Feature Documentation

| Document | Description |
|----------|-------------|
| Encryption | KMS integration and transparent encryption |
| Replication | Multi-backend replication strategies |
| Tiering | Archive and storage tiering policies |
| Catalog | Catalog operations and queries |
| Access Control | RBAC/ABAC and object ACLs |

### Backend Documentation

| Document | Description |
|----------|-------------|
| Backends | Backend-specific configuration (S3, GCS, Azure, etc.) |
| State Machine | ObjectStore Raft state machine |

### Admin Documentation

| Document | Description |
|----------|-------------|
| Admin Operations | Backend management, GC, repair, compaction |

## Design Principles

1. **Identity ≠ Location**: Object keys don't encode storage location
2. **Catalog as Source of Truth**: All location info lives in the catalog
3. **Multi-Location Objects**: Objects can exist in primary + replicas + archive
4. **Async by Default**: Replication and archival happen asynchronously
5. **Automatic Failover**: Reads fall back to replicas if primary fails
6. **Backward Compatible**: V1 commands continue to work alongside V2

## Dependencies

- [go-qrdb](https://github.com/jeremyhahn/go-qrdb) - Storage API; its objstore backend, [go-objstore](https://github.com/jeremyhahn/go-objstore), is selected by configuration. QuicBaaS still links go-objstore directly today; the SPEC's [Storage clean-up](../SPEC.md#storage-clean-up) row removes that link
- [go-xkms](https://github.com/jeremyhahn/go-xkms) - Encryption key management
