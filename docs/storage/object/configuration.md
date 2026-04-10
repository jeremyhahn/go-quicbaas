# Object Storage Configuration

Object storage uses a three-layer configuration hierarchy where more specific settings override general defaults.

## Configuration Hierarchy

```
Cluster Defaults (object_storage.defaults)
       ↓ overridden by
Node Config (server.node_object_storage)
       ↓ overridden by
Tenant/Shard Config (object_storage.tenants.{id})
```

## Complete Configuration Example

```yaml
# quicbaas.yaml

object_storage:
  # Backend definitions (cluster-wide)
  backends:
    s3-primary:
      type: s3
      config:
        bucket: quicbaas-objects
        region: us-east-1
        access_key: ${AWS_ACCESS_KEY_ID}
        secret_key: ${AWS_SECRET_ACCESS_KEY}
      encryption:
        enabled: true
        enforce: true
        key_id: "dek:stores/s3-primary"
        algorithm: "AES-256-GCM"

    s3-archive:
      type: s3
      config:
        bucket: quicbaas-archive
        region: us-east-1
        storage_class: GLACIER
      encryption:
        enabled: true
        enforce: false
        key_id: "dek:stores/s3-archive"

    gcs-west:
      type: gcs
      config:
        bucket: quicbaas-gcs-west
        project: my-project
        credentials_file: /etc/quicbaas/gcs-credentials.json

    local-cache:
      type: local
      config:
        path: /data/object-cache
        max_size: 10737418240  # 10GB

    minio-dev:
      type: minio
      config:
        endpoint: minio.internal:9000
        bucket: quicbaas
        access_key: ${MINIO_ACCESS_KEY}
        secret_key: ${MINIO_SECRET_KEY}
        use_ssl: false

  # Cluster-wide defaults
  defaults:
    primary_backend: s3-primary
    required_backends: [s3-primary]
    value_threshold: 1048576      # 1MB - offload threshold
    compression: zstd

    encryption:
      enabled: true
      enforce: true
      default_key: "dek:cluster/objects"
      algorithm: "AES-256-GCM"

    replication:
      enabled: true
      min_replicas: 2
      target_stores: [s3-primary, gcs-west]
      mode: async                 # sync, async, quorum
      priority: 5

    archive:
      enabled: true
      archive_after_days: 90
      target_store: s3-archive
      delete_from_primary: false
      compression_level: 6

    retention:
      enabled: false
      retention_days: 0           # 0 = infinite
      max_versions: 0             # 0 = unlimited

  # Per-tenant configurations
  tenants:
    tenant-acme:
      primary_backend: s3-primary
      allowed_backends: [s3-primary, gcs-west]

      encryption:
        enabled: true
        enforce: true
        key_id: "dek:tenant/acme/objects"

      replication:
        min_replicas: 3
        target_stores: [s3-primary, gcs-west, s3-archive]
        mode: quorum

      quotas:
        max_storage_bytes: 107374182400   # 100GB
        max_object_size: 5368709120       # 5GB
        max_object_count: 1000000

      # Per-shard overrides
      shard_configs:
        1001:                              # Hot data shard
          primary_backend: local-cache
          replication:
            min_replicas: 2
            target_stores: [local-cache, s3-primary]

    tenant-beta:
      primary_backend: gcs-west
      allowed_backends: [gcs-west]

      encryption:
        enabled: false
        enforce: false

      quotas:
        max_storage_bytes: 10737418240    # 10GB

# Per-node configuration
server:
  node_object_storage:
    internal_backend: local-cache
    cache_backend: local-cache
    cache_max_size: 10737418240           # 10GB

    encryption:
      enabled: true
      key_id: "dek:node/${NODE_ID}/internal"

    backend_overrides:
      local-cache:
        config:
          path: /nvme/object-cache        # Node-specific path

    enabled_backends: [s3-primary, gcs-west, local-cache]
```

## Backend Configuration

### S3 Backend

```yaml
backends:
  s3-primary:
    type: s3
    config:
      bucket: my-bucket                   # Required
      region: us-east-1                   # Required
      access_key: ${AWS_ACCESS_KEY_ID}    # Optional (uses IAM if not set)
      secret_key: ${AWS_SECRET_ACCESS_KEY}
      endpoint: ""                        # Optional custom endpoint
      storage_class: STANDARD             # STANDARD, REDUCED_REDUNDANCY, etc.
      force_path_style: false             # Use path-style addressing
      disable_ssl: false
      upload_part_size: 5242880           # 5MB multipart upload size
      max_retries: 3
      timeout: 30s
```

### GCS Backend

```yaml
backends:
  gcs-west:
    type: gcs
    config:
      bucket: my-gcs-bucket               # Required
      project: my-project                 # Required
      credentials_file: /path/to/creds.json
      storage_class: STANDARD             # STANDARD, NEARLINE, COLDLINE, ARCHIVE
      timeout: 30s
```

### Azure Blob Backend

```yaml
backends:
  azure-primary:
    type: azure
    config:
      account_name: mystorageaccount      # Required
      account_key: ${AZURE_STORAGE_KEY}   # Or use connection_string
      container: quicbaas-objects         # Required
      endpoint_suffix: core.windows.net
      timeout: 30s
```

### Local Backend

```yaml
backends:
  local-cache:
    type: local
    config:
      path: /data/object-cache            # Required
      max_size: 10737418240               # Optional size limit
      dir_mode: 0755
      file_mode: 0644
```

### MinIO Backend

```yaml
backends:
  minio-dev:
    type: minio
    config:
      endpoint: minio.internal:9000       # Required
      bucket: quicbaas                    # Required
      access_key: ${MINIO_ACCESS_KEY}     # Required
      secret_key: ${MINIO_SECRET_KEY}     # Required
      use_ssl: false
      region: us-east-1
```

## Defaults Configuration

### Compression Settings

| Value | Description |
|-------|-------------|
| `none` | No compression |
| `zstd` | Zstandard (default, best ratio/speed) |
| `gzip` | Gzip (wide compatibility) |
| `lz4` | LZ4 (fastest) |

```yaml
defaults:
  compression: zstd
  value_threshold: 1048576    # Only compress objects > 1MB
```

### Replication Modes

| Mode | Description |
|------|-------------|
| `sync` | Wait for all replicas before returning |
| `async` | Return immediately, replicate in background |
| `quorum` | Wait for majority of replicas |

```yaml
defaults:
  replication:
    enabled: true
    min_replicas: 2
    target_stores: [s3-primary, gcs-west]
    mode: async
    priority: 5    # Higher = replicate sooner (1-10)
```

### Archive Policy

```yaml
defaults:
  archive:
    enabled: true
    archive_after_days: 90      # Days since last access
    target_store: s3-archive
    delete_from_primary: false  # Keep on primary after archive
    compression_level: 6        # 0-9 (0=none, 9=max)
```

### Retention Policy

```yaml
defaults:
  retention:
    enabled: true
    retention_days: 365         # Auto-delete after N days (0=never)
    max_versions: 10            # Keep last N versions (0=unlimited)
```

## Encryption Configuration

### Hierarchy

```
Cluster Default → Store Config → Tenant Config → Object Override
```

### Enforcement Logic

```yaml
encryption:
  enabled: true       # Encryption is available
  enforce: true       # Use this encryption if object doesn't specify
  key_id: "dek:..."   # KMS key path
  algorithm: "AES-256-GCM"
```

When `enforce: true`:
- If object specifies encryption → use object's key
- If object doesn't specify → use this config's key

When `enforce: false`:
- If object specifies encryption → use object's key
- If object doesn't specify → no encryption (even if enabled)

### Resolution Order

1. Object-level override (always wins if specified)
2. Store's encryption config (if enforce=true)
3. Tenant's encryption config (if enforce=true)
4. Cluster default (if enforce=true)
5. No encryption

## Tenant Configuration

### Quotas

```yaml
tenants:
  tenant-acme:
    quotas:
      max_storage_bytes: 107374182400   # 100GB total
      max_object_size: 5368709120       # 5GB per object
      max_object_count: 1000000         # 1M objects max
```

### Backend Restrictions

```yaml
tenants:
  tenant-acme:
    primary_backend: s3-primary
    allowed_backends: [s3-primary, gcs-west]  # Only these backends allowed
```

### Shard-Specific Config

```yaml
tenants:
  tenant-acme:
    shard_configs:
      1001:
        primary_backend: local-cache
        replication:
          target_stores: [local-cache, s3-primary]
      1002:
        primary_backend: s3-archive
```

## Node Configuration

### Internal Backend

Used for Raft snapshots and internal data:

```yaml
server:
  node_object_storage:
    internal_backend: local-cache
    encryption:
      enabled: true
      key_id: "dek:node/${NODE_ID}/internal"
```

### Cache Configuration

```yaml
server:
  node_object_storage:
    cache_backend: local-cache
    cache_max_size: 10737418240   # 10GB cache
```

### Backend Overrides

Node-specific backend configuration:

```yaml
server:
  node_object_storage:
    backend_overrides:
      local-cache:
        config:
          path: /nvme/cache       # Different path on this node
      s3-primary:
        config:
          endpoint: s3-proxy.local:8080  # Local proxy
```

### Enabled Backends

Limit which backends this node can access:

```yaml
server:
  node_object_storage:
    enabled_backends: [s3-primary, local-cache]
    # This node won't use gcs-west even if configured cluster-wide
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_OBJECT_PRIMARY_BACKEND` | Override primary backend | - |
| `QUICBAAS_OBJECT_COMPRESSION` | Override compression | `zstd` |
| `QUICBAAS_OBJECT_ENCRYPTION_ENABLED` | Enable/disable encryption | `true` |
| `QUICBAAS_OBJECT_CACHE_SIZE` | Cache size in bytes | `10737418240` |
| `AWS_ACCESS_KEY_ID` | S3 access key | - |
| `AWS_SECRET_ACCESS_KEY` | S3 secret key | - |
| `GOOGLE_APPLICATION_CREDENTIALS` | GCS credentials path | - |
| `AZURE_STORAGE_KEY` | Azure storage key | - |

## Configuration Resolution

The effective configuration for an operation is resolved as:

```go
func ResolveConfig(tenantID, shardID string) *ResolvedConfig {
    // Start with cluster defaults
    cfg := clusterDefaults.Clone()

    // Apply node overrides
    cfg.Merge(nodeConfig)

    // Apply tenant config
    if tenantCfg := getTenantConfig(tenantID); tenantCfg != nil {
        cfg.Merge(tenantCfg)

        // Apply shard config
        if shardCfg := tenantCfg.ShardConfigs[shardID]; shardCfg != nil {
            cfg.Merge(shardCfg)
        }
    }

    return cfg
}
```

## Validation

Configuration is validated at startup:

1. All referenced backends must be defined
2. Primary backend must be in allowed backends
3. Quota values must be positive
4. Encryption key IDs must be valid KMS paths
5. Compression algorithm must be supported
6. Replication targets must be defined backends
