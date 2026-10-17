# Object Storage — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/object-storage/`.

Object storage uses a three-layer configuration hierarchy where more specific settings override general defaults.

## Configuration Hierarchy

```
Cluster Defaults (object_storage.defaults)
       ↓ overridden by
Node Config (server.node_object_storage)
       ↓ overridden by
Tenant/Shard Config (object_storage.tenants.{id})
```

## Primary Backend

`object_storage.primary_backend` names the backend that receives objects stored
without an explicit `target_backend` (put, copy, batch put, restore). The server
resolves it once at startup and seeds the cluster defaults with it:

| Configuration | Result |
|---------------|--------|
| `primary_backend` set to a configured backend | That backend is primary |
| `primary_backend` set to a name not under `backends` | Startup refused: `UnknownPrimaryBackendError` |
| `primary_backend` unset, exactly one backend (including the automatic in-memory `default` created when `enabled: true` has no backends) | That backend is primary |
| `primary_backend` unset, several backends | Startup refused: `PrimaryBackendUnresolvedError`, which names the configured backends |

An explicit `target_backend` on a request always wins over the primary.
Tenant and shard `primary_backend` overrides still apply on top of it.

## Complete Configuration Example

```yaml
# quicbaas.yaml

object_storage:
  # Receives objects stored without a target_backend. Required when more
  # than one backend is configured.
  primary_backend: s3-primary

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


---

## Object Storage Encryption

Transparent encryption for objects using QuicBaaS's KMS integration.

### Overview

Object storage encryption provides:
- Transparent encrypt/decrypt during PUT/GET operations
- Hierarchical key management (cluster → node → store → tenant → object)
- Per-object encryption key override
- Automatic key rotation support
- Multiple encryption algorithms

### Encryption Hierarchy

```
┌───────────────────────────────────────────────────────────────────┐
│                     Encryption Configuration                      │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  Level 1: Cluster Defaults                                        │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  object_storage.defaults.encryption                         │  │
│  │    key: dek:cluster/objects                                 │  │
│  │    enforce: true                                            │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Level 2: Node Override                                           │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  server.node_object_storage.encryption                      │  │
│  │    key: dek:node/{node_id}/internal                         │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Level 3: Store-Specific                                          │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  object_storage.backends.{name}.encryption                  │  │
│  │    key: dek:stores/{name}                                   │  │
│  │    enforce: true/false                                      │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Level 4: Tenant-Specific                                         │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  object_storage.tenants.{id}.encryption                     │  │
│  │    key: dek:tenant/{id}/objects                             │  │
│  │    enforce: true/false                                      │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Level 5: Per-Object Override                                     │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  Request Header: X-QuicBaaS-Encryption-Key                  │  │
│  │    key: dek:custom/mykey                                    │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

### Configuration

#### Cluster-Wide Encryption

```yaml
object_storage:
  defaults:
    encryption:
      enabled: true                         # Enable encryption
      enforce: true                         # Use this config when object doesn't specify
      default_key: "dek:cluster/objects"    # Default encryption key
      algorithm: "AES-256-GCM"              # Encryption algorithm
```

#### Per-Backend Encryption

```yaml
object_storage:
  backends:
    s3-primary:
      type: s3
      config:
        bucket: quicbaas-objects
      encryption:
        enabled: true
        enforce: true                       # Encrypt all objects in this backend
        key_id: "dek:stores/s3-primary"
        algorithm: "AES-256-GCM"

    s3-archive:
      type: s3
      config:
        bucket: quicbaas-archive
        storage_class: GLACIER
      encryption:
        enabled: true
        enforce: false                      # Only encrypt if explicitly requested
        key_id: "dek:stores/s3-archive"

    local-cache:
      type: local
      config:
        path: /data/cache
      encryption:
        enabled: false                      # Cache is ephemeral, no encryption
```

#### Per-Tenant Encryption

```yaml
object_storage:
  tenants:
    tenant-acme:
      encryption:
        enabled: true
        enforce: true
        key_id: "dek:tenant/acme/objects"   # Tenant-specific key
        algorithm: "AES-256-GCM"

    tenant-beta:
      encryption:
        enabled: false                      # This tenant opts out of encryption
```

#### Per-Node Encryption

```yaml
server:
  node_object_storage:
    internal_backend: local-cache
    encryption:
      enabled: true
      key_id: "dek:node/${NODE_ID}/internal"  # Node-specific key
```

### Enforce Flag Logic

The `enforce` flag determines whether encryption is applied when an object doesn't specify encryption settings:

| Scenario | enforce=true | enforce=false |
|----------|--------------|---------------|
| Object specifies key | Use object's key | Use object's key |
| Object doesn't specify | Use this level's key | Check next level |
| No level enforces | Use cluster default | No encryption |

#### Resolution Algorithm

```go
func ResolveEncryptionConfig(req *PutRequest, cfg *ResolvedConfig) *EncryptionConfig {
    // 1. Object-level override (always wins if specified)
    if req.EncryptionKeyID != "" {
        return &EncryptionConfig{
            Enabled:   true,
            KeyID:     req.EncryptionKeyID,
            Algorithm: req.EncryptionAlgorithm,
        }
    }

    // 2. Store's enforce setting
    if cfg.StoreEncryption.Enforce {
        return cfg.StoreEncryption
    }

    // 3. Tenant's enforce setting
    if cfg.TenantEncryption.Enforce {
        return cfg.TenantEncryption
    }

    // 4. Cluster default enforce setting
    if cfg.ClusterEncryption.Enforce {
        return cfg.ClusterEncryption
    }

    // 5. No encryption
    return &EncryptionConfig{Enabled: false}
}
```

#### Examples

**Scenario 1: Store enforces, tenant doesn't**
```yaml
backends:
  s3-primary:
    encryption:
      enforce: true
      key_id: "dek:stores/s3-primary"

tenants:
  tenant-acme:
    encryption:
      enforce: false
      key_id: "dek:tenant/acme/objects"
```
Result: Objects use `dek:stores/s3-primary` (store enforces)

**Scenario 2: Tenant enforces, store doesn't**
```yaml
backends:
  s3-primary:
    encryption:
      enforce: false
      key_id: "dek:stores/s3-primary"

tenants:
  tenant-acme:
    encryption:
      enforce: true
      key_id: "dek:tenant/acme/objects"
```
Result: Objects use `dek:tenant/acme/objects` (tenant enforces)

**Scenario 3: Object overrides everything**
The put request sets `encryption_key_id: "dek:custom/mykey"` (the CLI does not
expose this field).
Result: Object uses `dek:custom/mykey` (object override always wins)

### Supported Algorithms

| Algorithm | Description | Key Size | Use Case |
|-----------|-------------|----------|----------|
| `AES-256-GCM` | AES with Galois/Counter Mode | 256-bit | Default, recommended |
| `AES-128-GCM` | AES with Galois/Counter Mode | 128-bit | Faster, lower security |
| `ChaCha20-Poly1305` | ChaCha20 with Poly1305 MAC | 256-bit | Software-optimized |

### Catalog Entry Encryption Metadata

When an object is encrypted, the catalog entry stores encryption metadata:

```json
{
  "key": "tenant-acme:docs/report.pdf",
  "encryption": {
    "enabled": true,
    "key_id": "dek:tenant/acme/objects",
    "key_version": 3,
    "algorithm": "AES-256-GCM",
    "encrypted_at": 1705312200000000000
  },
  "primary_store": {
    "store": "s3-primary",
    "encryption": {
      "key_id": "dek:tenant/acme/objects",
      "key_version": 3,
      "algorithm": "AES-256-GCM"
    }
  }
}
```

### Encryption Flow

#### PUT Operation with Encryption

```
1. Client: Put("tenant-acme:docs/report.pdf", data)
       │
       ▼
2. Resolve Encryption Config
   - Check object request for encryption key
   - Check store enforcement
   - Check tenant enforcement
   - Check cluster default
       │
       ▼
3. If Encryption Enabled:
   │
   ├── Get DEK from KMS
   │   └── kmsService.GetDEK("dek:tenant/acme/objects")
   │
   ├── Encrypt Data
   │   └── ciphertext = kmsManager.Encrypt(keyID, plaintext)
   │
   └── Create Encryption Metadata
       └── ObjectEncryption{KeyID, KeyVersion, Algorithm, EncryptedAt}
       │
       ▼
4. Store Encrypted Data in Backend
       │
       ▼
5. Store Catalog Entry with Encryption Metadata
```

#### GET Operation with Decryption

```
1. Client: Get("tenant-acme:docs/report.pdf")
       │
       ▼
2. Lookup Catalog Entry
   - Get encryption metadata
       │
       ▼
3. Fetch Ciphertext from Backend
       │
       ▼
4. If Encrypted:
   │
   ├── Get DEK from KMS
   │   └── kmsService.GetDEK(entry.Encryption.KeyID)
   │
   └── Decrypt Data
       └── plaintext = kmsManager.Decrypt(keyID, ciphertext)
       │
       ▼
5. Verify Checksum
       │
       ▼
6. Return Plaintext to Client
```

### Key Rotation

#### Automatic Re-encryption

When a DEK is rotated, objects encrypted with the old version need re-encryption:

```go
// Background worker processes re-encryption
type ReencryptionWorker struct {
    catalog    CatalogManager
    backends   BackendManager
    kmsService KMSService
}

func (w *ReencryptionWorker) ReencryptForKey(keyID string, newVersion uint64) {
    // Find objects using old key version
    entries := w.catalog.ListByEncryptionKey(keyID)

    for _, entry := range entries {
        if entry.Encryption.KeyVersion >= newVersion {
            continue  // Already current
        }

        // Read, decrypt with old key
        data := w.fetchAndDecrypt(entry)

        // Encrypt with new key version
        ciphertext := w.encryptWithNewKey(data, keyID, newVersion)

        // Write back
        w.writeBack(entry, ciphertext)

        // Update catalog
        entry.Encryption.KeyVersion = newVersion
        w.catalog.Update(entry)
    }
}
```

#### Manual Re-encryption

```bash
# Rotate the DEK; the re-encryption worker above rewrites old objects
quicbaas kms key rotate dek:tenant/acme/objects
```

The CLI has no per-object re-encrypt verb and no list filter by encryption.

### CLI Usage

```bash
# Store with the encryption resolved from config
quicbaas object put docs/report.pdf --file ./report.pdf

# View an object's metadata
quicbaas object head docs/report.pdf --output json
```

`object put` has no encryption flags; the key is chosen by the configuration
precedence above.

### REST API

#### Encryption Headers

| Header | Description |
|--------|-------------|
| `X-QuicBaaS-Encryption-Key` | Override encryption key ID |
| `X-QuicBaaS-No-Encrypt` | Disable encryption (if allowed) |

#### Example

```bash
# PUT with custom encryption key
curl -X PUT https://quicbaas:8080/api/v1/objects/tenant-acme/secret/data.json \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-QuicBaaS-Encryption-Key: dek:tenant/acme/secrets" \
  --data-binary @data.json

# PUT without encryption (if allowed)
curl -X PUT https://quicbaas:8080/api/v1/objects/tenant-acme/public/readme.txt \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-QuicBaaS-No-Encrypt: true" \
  --data-binary @readme.txt
```

### Security Considerations

#### Key Management

- DEKs are stored securely in KMS with master key encryption
- Keys support versioning for rotation without data loss
- Key access is controlled via RBAC policies

#### At-Rest Encryption

- All encrypted data is stored as ciphertext in backends
- Backend-native encryption can be used in addition (defense in depth)
- Encryption metadata stored separately from ciphertext

#### In-Transit Encryption

- TLS required for all API communication
- Plaintext never transmitted over network

#### Access Control

- Encryption key access requires appropriate RBAC permissions
- Per-tenant key isolation prevents cross-tenant access
- Audit logging tracks all encryption operations

### Troubleshooting

#### Common Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| `encryption key not found` | DEK doesn't exist in KMS | Create DEK or check key path |
| `permission denied` | User lacks key access | Grant appropriate KMS permissions |
| `decryption failed` | Key version mismatch or corruption | Check key version, verify backup |
| `algorithm not supported` | Unknown encryption algorithm | Use supported algorithm |

#### Debugging

```bash
# Check KMS key status
quicbaas kms key get dek:tenant/acme/objects

# View an object's metadata
quicbaas object head docs/report.pdf --output json
```


---

## Encryption Key Storage

Encryption keys from go-xkms can be persisted to any object storage backend using the `ObjStoreAdapter`.

### Configuration

```yaml
object_storage:
  backends:
    key-store:
      type: s3
      config:
        bucket: my-quicbaas-keys
        region: us-east-1
        access_key: ${AWS_ACCESS_KEY_ID}
        secret_key: ${AWS_SECRET_ACCESS_KEY}
```

The named backend is wired to go-xkms programmatically via the
`ObjStoreAdapter` (see below). At-rest encryption of the embedded KV store
itself is provided by the qrdb seal/barrier subsystem, not a key-file
toggle.

### Benefits

- **Persistence** - Keys survive restarts
- **Replication** - Cross-region key availability with cloud storage
- **Integration** - Works with any go-objstore backend (today through a direct go-objstore link; see below)
- **Hardware Security** - Combine with KMS backends for HSM integration

### ObjStoreAdapter

The `ObjStoreAdapter` (`pkg/security`) bridges go-objstore's `common.Storage` interface with go-xkms's `storage.Backend` interface.

It imports go-objstore directly, as intended: go-objstore owns all object
storage, buckets and multipart included, and go-quicbaas links it as its
provider. What remains open is the SDK side: `sdk/go` re-exports go-objstore's
types rather than defining its own (the SPEC's
[Storage clean-up](../SPEC.md#storage-clean-up) row).

```go
// Create an S3 storage backend
s3Storage, _ := factory.NewStorage("s3", map[string]string{
    "bucket":     "my-bucket",
    "region":     "us-east-1",
    "access_key": os.Getenv("AWS_ACCESS_KEY_ID"),
    "secret_key": os.Getenv("AWS_SECRET_ACCESS_KEY"),
})

// Wrap for go-xkms
xkmsBackend := security.NewObjStoreAdapter(s3Storage)

// Use with XKMS
xkmsInstance, _ := xkms.New(xkmsBackend, xkmsConfig)
```

### Security Best Practices

#### 1. Credentials Management

Never hardcode credentials. Use environment variables or secrets managers:

```yaml
config:
  access_key: ${AWS_ACCESS_KEY_ID}
  secret_key: ${AWS_SECRET_ACCESS_KEY}
```

#### 2. Encryption at Rest

Enable server-side encryption on cloud storage:

**AWS S3:**
```yaml
config:
  sse: AES256  # or aws:kms
```

**GCS:**
- Enable default encryption on bucket

**Azure:**
- Enable storage service encryption

#### 3. Access Control

Use IAM policies to restrict access:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::my-quicbaas-keys/*"
    }
  ]
}
```

#### 4. Network Security

- Use TLS for all communication
- Consider VPC endpoints for cloud storage
- Enable bucket policies to restrict IP ranges

#### 5. Key Rotation

Regularly rotate storage access keys:

```bash
# Rotate AWS keys
aws iam create-access-key --user-name quicbaas-service
aws iam delete-access-key --user-name quicbaas-service --access-key-id OLD_KEY
```

### Local Development

For development, use local or memory backends:

```yaml
object_storage:
  backends:
    dev-keys:
      type: local
      config:
        path: /tmp/quicbaas-keys
```

Or in-memory for tests:

```yaml
object_storage:
  backends:
    test-keys:
      type: memory
```


---

## Object Storage Access Control

RBAC, ABAC, and ACL-based access control for object storage.

### Overview

Access control provides:
- Role-Based Access Control (RBAC) with built-in roles
- Attribute-Based Access Control (ABAC) for fine-grained policies
- Per-object ACLs for granular permissions
- Tenant isolation
- Namespace-level permissions

### Access Control Layers

```
┌───────────────────────────────────────────────────────────────────┐
│                       Access Control Stack                        │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  Layer 1: Authentication                                          │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  JWT Token / API Key / mTLS                                 │  │
│  │  → Establishes identity (subject)                           │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Layer 2: RBAC (Role-Based)                                       │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  User has role → Role has permissions                       │  │
│  │  objstore-admin, objstore-readwrite, etc.             │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Layer 3: ABAC (Attribute-Based)                                  │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  Policies based on attributes                               │  │
│  │  tenant, namespace, time, IP, object size, etc.             │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Layer 4: Object ACL                                              │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  Per-object permissions                                     │  │
│  │  Owner, grants, public access                               │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

### Resource Types

| Resource | Description |
|----------|-------------|
| `objstore` | Object operations (read, write, delete) |
| `bucket` | Bucket/namespace management |
| `catalog` | Catalog queries and metadata |

### Actions

| Action | Description |
|--------|-------------|
| `read` | Read objects |
| `write` | Write/update objects |
| `delete` | Delete objects |
| `replicate` | Copy to other backends |
| `migrate` | Move between backends |
| `archive` | Move to archive tier |
| `admin` | Administrative operations |

### RBAC - Built-in Roles

#### objstore-admin

Full access to object storage:

```yaml
name: objstore-admin
description: Full access to object storage
permissions:
  - resource: objstore
    actions: ["*"]
  - resource: bucket
    actions: ["*"]
  - resource: catalog
    actions: ["*"]
```

#### objstore-readwrite

Read and write objects, no admin:

```yaml
name: objstore-readwrite
description: Read/write objects, no admin
permissions:
  - resource: objstore
    actions: [read, write, delete]
  - resource: catalog
    actions: [read]
```

#### objstore-readonly

Read-only access:

```yaml
name: objstore-readonly
description: Read-only access to objects
permissions:
  - resource: objstore
    actions: [read]
  - resource: catalog
    actions: [read]
```

#### Custom Roles

Define custom roles in configuration:

```yaml
auth:
  rbac:
    custom_roles:
      - name: tenant-acme-admin
        description: ACME Corp object storage admin
        permissions:
          - resource: objstore
            actions: [read, write, delete, replicate, archive]
          - resource: catalog
            actions: [read, write]
        conditions:
          tenant_id: tenant-acme

      - name: backup-operator
        description: Backup operations only
        permissions:
          - resource: objstore
            actions: [read, write]
          - resource: catalog
            actions: [read]
        conditions:
          namespace_prefix: backups:
```

### ABAC - Attribute-Based Policies

#### Policy Structure

```go
type ObjectStorePolicy struct {
    Name        string
    Description string
    Rules       []PolicyRule
}

type PolicyRule struct {
    Effect     string           // "allow" or "deny"
    Principal  PrincipalMatch
    Action     []string
    Resource   ResourceMatch
    Condition  map[string]any
}
```

#### Tenant Isolation Policy

```yaml
policies:
  - name: tenant-isolation
    description: Tenants can only access their own namespace
    rules:
      - effect: allow
        principal:
          attribute: tenant_id
          equals: "${request.tenant_id}"
        action:
          - objstore:read
          - objstore:write
          - objstore:delete
        resource:
          attribute: namespace
          starts_with: "${subject.tenant_id}:"
```

#### Time-Based Access

```yaml
policies:
  - name: business-hours-only
    description: Write access only during business hours
    rules:
      - effect: allow
        action: [objstore:write]
        condition:
          time_between: ["09:00", "17:00"]
          day_of_week: [1, 2, 3, 4, 5]  # Mon-Fri
```

#### IP-Based Restriction

```yaml
policies:
  - name: archive-ip-restriction
    description: Archive operations from internal network only
    rules:
      - effect: allow
        action: [objstore:archive]
        condition:
          ip_in_range: "10.0.0.0/8"
```

#### Size-Based Policy

```yaml
policies:
  - name: size-limit
    description: Limit object size for regular users
    rules:
      - effect: deny
        principal:
          role_not: [objstore-admin]
        action: [objstore:write]
        condition:
          object_size_greater_than: 104857600  # 100MB
```

### Object ACLs

#### ACL Structure

```go
type ObjectACL struct {
    Owner       string        // User who created the object
    OwnerAccess []string      // Owner's permissions
    Public      bool          // Publicly readable?
    Grants      []ACLGrant    // Explicit grants
}

type ACLGrant struct {
    Principal     string   // User ID, role, group, or "*"
    PrincipalType string   // "user", "role", "group", "tenant"
    Permissions   []string // "read", "write", "delete", "admin"
}
```

#### Catalog Entry with ACL

```json
{
  "key": "tenant-acme:docs/report.pdf",
  "acl": {
    "owner": "user@example.com",
    "owner_access": ["read", "write", "delete", "admin"],
    "public": false,
    "grants": [
      {
        "principal": "finance-team",
        "principal_type": "group",
        "permissions": ["read", "write"]
      },
      {
        "principal": "auditor@example.com",
        "principal_type": "user",
        "permissions": ["read"]
      }
    ]
  }
}
```

#### ACL Evaluation

```go
func checkObjectACL(entry *CatalogEntry, subject *Subject, action string) bool {
    acl := entry.ACL

    // Owner has full access
    if acl.Owner == subject.UserID {
        return true
    }

    // Check explicit grants
    for _, grant := range acl.Grants {
        if matchesPrincipal(grant, subject) && hasPermission(grant, action) {
            return true
        }
    }

    // Check public access for read
    if action == "read" && acl.Public {
        return true
    }

    return false
}
```

### Authorization Flow

```
Request: PUT tenant-acme:docs/report.pdf
                    │
                    ▼
┌─────────────────────────────────────────┐
│ 1. Extract Subject from Token           │
│    - UserID: user@example.com           │
│    - TenantID: tenant-acme              │
│    - Roles: [objstore-readwrite]     │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ 2. RBAC Check                           │
│    - Role: objstore-readwrite        │
│    - Has permission: objstore:write  │
│    → PASS                               │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ 3. ABAC Check                           │
│    - Policy: tenant-isolation           │
│    - Subject tenant: tenant-acme        │
│    - Resource namespace: tenant-acme    │
│    → PASS                               │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ 4. ACL Check (if object exists)         │
│    - Check owner/grants                 │
│    → PASS or N/A for new objects        │
└─────────────────────────────────────────┘
                    │
                    ▼
            Request Authorized
```

### Configuration

#### Enable Access Control

```yaml
auth:
  enabled: true

  rbac:
    enabled: true
    default_roles:
      - objstore-readonly

  abac:
    enabled: true
    policies_file: /etc/quicbaas/policies.yaml

object_storage:
  access_control:
    enabled: true
    enforce_tenant_isolation: true
    default_acl:
      owner_access: [read, write, delete, admin]
      public: false
```

#### Tenant Configuration

```yaml
object_storage:
  tenants:
    tenant-acme:
      allowed_roles:
        - objstore-readwrite
        - objstore-admin
        - tenant-acme-admin
```

### CLI Commands

The CLI has no object ACL verbs and no permission-check verb; manage ACLs
through the REST API below.

### REST API

#### Get Object ACL

```http
GET /api/v1/objects/{key}/acl
```

**Response:**
```json
{
  "key": "tenant-acme:docs/report.pdf",
  "acl": {
    "owner": "user@example.com",
    "owner_access": ["read", "write", "delete", "admin"],
    "public": false,
    "grants": [
      {
        "principal": "finance-team",
        "principal_type": "group",
        "permissions": ["read", "write"]
      }
    ]
  }
}
```

#### Update Object ACL

```http
PUT /api/v1/objects/{key}/acl
```

**Request:**
```json
{
  "grants": [
    {
      "principal": "finance-team",
      "principal_type": "group",
      "permissions": ["read", "write"]
    },
    {
      "principal": "auditor@example.com",
      "principal_type": "user",
      "permissions": ["read"]
    }
  ],
  "public": false
}
```

#### Add Grant

```http
POST /api/v1/objects/{key}/acl/grants
```

**Request:**
```json
{
  "principal": "new-user@example.com",
  "principal_type": "user",
  "permissions": ["read"]
}
```

#### Remove Grant

```http
DELETE /api/v1/objects/{key}/acl/grants/{principal}
```

### gRPC Method Mapping

```go
func mapObjectStorageMethod(method string) (resource, action string) {
    switch method {
    // Read operations
    case "GetObject", "HeadObject":
        return "objstore", "read"
    case "ListObjects", "SearchObjects":
        return "catalog", "read"
    case "GetCatalogEntry", "GetLocations":
        return "catalog", "read"

    // Write operations
    case "PutObject", "UpdateMetadata":
        return "objstore", "write"
    case "CopyObject":
        return "objstore", "replicate"

    // Delete operations
    case "DeleteObject":
        return "objstore", "delete"

    // Admin operations
    case "CreateBucket", "DeleteBucket", "SetBucketPolicy":
        return "bucket", "admin"
    case "Replicate":
        return "objstore", "replicate"
    case "Migrate":
        return "objstore", "migrate"
    case "Archive":
        return "objstore", "archive"

    default:
        return "objstore", "admin"
    }
}
```

### Audit Logging

All access control decisions are logged:

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "event": "authorization",
  "subject": {
    "user_id": "user@example.com",
    "tenant_id": "tenant-acme",
    "roles": ["objstore-readwrite"]
  },
  "resource": "objstore",
  "action": "write",
  "object_key": "tenant-acme:docs/report.pdf",
  "decision": "allowed",
  "reason": "RBAC role permission",
  "source_ip": "10.0.1.50"
}
```

### Metrics

```
# Authorization decisions
quicbaas_objstore_authz_decisions_total{decision="allowed"}
quicbaas_objstore_authz_decisions_total{decision="denied"}

# By check type
quicbaas_objstore_authz_rbac_checks_total
quicbaas_objstore_authz_abac_checks_total
quicbaas_objstore_authz_acl_checks_total

# Latency
quicbaas_objstore_authz_check_duration_seconds
```

### Troubleshooting

#### Common Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| `permission denied` | No matching permission | Check role assignments |
| `tenant mismatch` | Cross-tenant access | Verify tenant isolation policy |
| `acl check failed` | Object ACL blocks access | Update ACL or use admin |

#### Debugging

The CLI has no permission-listing or authorization-test verb. Check the
caller's role bindings with `quicbaas iam user get` and the
`quicbaas_objstore_authz_*` metrics above.


---

## Backup Storage

QuicBaaS's backup manager (`pkg/backup`) stores database backups to any
supported object storage backend through a direct go-objstore import. That
direct link is a departure, removed by the SPEC's
[Storage clean-up](../SPEC.md#storage-clean-up) row; the backup manager itself
is replaced by go-qrdb's backup service (see [backup-restore](../backup-restore/)).

### Configuration

```yaml
object_storage:
  backends:
    backup-store:
      type: s3
      config:
        bucket: my-quicbaas-backups
        region: us-east-1
        access_key: ${AWS_ACCESS_KEY_ID}
        secret_key: ${AWS_SECRET_ACCESS_KEY}
        prefix: backups/

backup:
  enabled: true
  storage_backend: backup-store  # References named backend
  work_dir: /tmp/quicbaas-backup
  retention_days: 30
  compression: zstd
  schedule: "0 2 * * *"  # Daily at 2 AM
```

### Backup Options

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `enabled` | No | false | Enable backup functionality |
| `storage_backend` | Yes | - | Named backend from `object_storage.backends` |
| `work_dir` | No | /tmp | Temporary directory for backup operations |
| `retention_days` | No | 30 | Days to retain backups |
| `compression` | No | zstd | Compression algorithm (none, gzip, zstd) |
| `schedule` | No | - | Cron expression for scheduled backups |

### CLI Commands

#### Create Backup

```bash
quicbaas backup create nightly --storage-type s3 --bucket my-quicbaas-backups --region us-east-1
```

#### List Backups

```bash
quicbaas backup list
```

#### Restore Backup

```bash
quicbaas backup restore <backup-id>
```

#### Delete Backup

```bash
quicbaas backup delete <backup-id> --yes
```

### Backup Types

#### Full Backup

Complete snapshot of all data:

```bash
quicbaas backup create full-2026-10-12
```

#### Incremental Backup

Only changes since last backup:

```bash
quicbaas backup create incr-2026-10-12 --incremental
```

### Compression

| Algorithm | Speed | Ratio | Use Case |
|-----------|-------|-------|----------|
| `none` | Fastest | 1:1 | When storage is cheap |
| `gzip` | Medium | Good | Compatibility |
| `zstd` | Fast | Best | Recommended default |

### Storage Organization

Backups are stored with the following key structure:

```
{prefix}/backups/{backup-id}/metadata.json
{prefix}/backups/{backup-id}/shard-{n}.data
```

### Multi-Cloud Backup

Store backups across multiple cloud providers for disaster recovery:

```yaml
object_storage:
  backends:
    primary-backup:
      type: s3
      config:
        bucket: quicbaas-backup-primary
        region: us-east-1

    secondary-backup:
      type: gcs
      config:
        bucket: quicbaas-backup-secondary
        project: my-project

backup:
  enabled: true
  storage_backend: primary-backup
  # Mirror to secondary (future feature)
```
