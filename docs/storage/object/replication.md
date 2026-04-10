# Object Storage Replication

Multi-backend replication strategies for durability and disaster recovery.

## Overview

Object replication provides:
- Automatic copying of objects across multiple backends
- Configurable replication modes (sync, async, quorum)
- Priority-based replication scheduling
- Automatic failover for reads
- Replication status tracking

## Replication Architecture

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

## Configuration

### Cluster-Wide Defaults

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

### Per-Tenant Configuration

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

### Per-Shard Configuration

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

## Replication Modes

### Sync Mode

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

### Async Mode

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

### Quorum Mode

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

## Priority Scheduling

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

## Replication Policy

### Policy Structure

```go
type ReplicationPolicy struct {
    Enabled      bool       // Enable replication
    MinReplicas  int        // Minimum copies required
    TargetStores []string   // Backend names
    Mode         string     // sync, async, quorum
    Priority     int        // 1-10
}
```

### Catalog Entry

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

## Replication Worker

### Worker Architecture

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

### Retry Policy

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

## Read Failover

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

## CLI Commands

### Add Replicas

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

### Check Replication Status

```bash
# View locations
quicbaas catalog locations tenant-acme:docs/report.pdf

# List under-replicated objects
quicbaas object list --under-replicated

# List objects pending replication
quicbaas object list --replication-status syncing
```

### Force Replication

```bash
# Force immediate replication
quicbaas object replicate tenant-acme:docs/report.pdf \
  --target gcs-west \
  --force

# Replicate all objects in namespace
quicbaas object replicate --prefix tenant-acme: \
  --target s3-backup
```

## REST API

### Replicate Object

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

### Get Replication Status

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

## Metrics

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

## Troubleshooting

### Common Issues

| Issue | Cause | Resolution |
|-------|-------|------------|
| Replication lag | High queue, slow backends | Increase workers, check backend health |
| Sync timeout | Backend latency | Use async mode, increase timeout |
| Under-replicated objects | Backend failures | Check backend health, trigger re-replication |
| Inconsistent replicas | Partial failures | Run verification, re-replicate |

### Debugging

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
