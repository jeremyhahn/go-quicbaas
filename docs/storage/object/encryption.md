# Object Storage Encryption

Transparent encryption for objects using QuicBaaS's KMS integration.

## Overview

Object storage encryption provides:
- Transparent encrypt/decrypt during PUT/GET operations
- Hierarchical key management (cluster → node → store → tenant → object)
- Per-object encryption key override
- Automatic key rotation support
- Multiple encryption algorithms

## Encryption Hierarchy

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

## Configuration

### Cluster-Wide Encryption

```yaml
object_storage:
  defaults:
    encryption:
      enabled: true                         # Enable encryption
      enforce: true                         # Use this config when object doesn't specify
      default_key: "dek:cluster/objects"    # Default encryption key
      algorithm: "AES-256-GCM"              # Encryption algorithm
```

### Per-Backend Encryption

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

### Per-Tenant Encryption

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

### Per-Node Encryption

```yaml
server:
  node_object_storage:
    internal_backend: local-cache
    encryption:
      enabled: true
      key_id: "dek:node/${NODE_ID}/internal"  # Node-specific key
```

## Enforce Flag Logic

The `enforce` flag determines whether encryption is applied when an object doesn't specify encryption settings:

| Scenario | enforce=true | enforce=false |
|----------|--------------|---------------|
| Object specifies key | Use object's key | Use object's key |
| Object doesn't specify | Use this level's key | Check next level |
| No level enforces | Use cluster default | No encryption |

### Resolution Algorithm

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

### Examples

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
```bash
quicbaas object put tenant-acme:secret/data.json ./data.json \
  --encryption-key "dek:custom/mykey"
```
Result: Object uses `dek:custom/mykey` (object override always wins)

## Supported Algorithms

| Algorithm | Description | Key Size | Use Case |
|-----------|-------------|----------|----------|
| `AES-256-GCM` | AES with Galois/Counter Mode | 256-bit | Default, recommended |
| `AES-128-GCM` | AES with Galois/Counter Mode | 128-bit | Faster, lower security |
| `ChaCha20-Poly1305` | ChaCha20 with Poly1305 MAC | 256-bit | Software-optimized |

## Catalog Entry Encryption Metadata

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

## Encryption Flow

### PUT Operation with Encryption

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

### GET Operation with Decryption

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

## Key Rotation

### Automatic Re-encryption

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

### Manual Re-encryption

```bash
# Re-encrypt all objects for a key
quicbaas kms rotate dek:tenant/acme/objects --reencrypt

# Re-encrypt specific object
quicbaas object reencrypt tenant-acme:docs/report.pdf

# Check re-encryption status
quicbaas object list --prefix tenant-acme: --encryption-version-below 3
```

## CLI Usage

### Store Encrypted Object

```bash
# Use default encryption from config
quicbaas object put tenant-acme:docs/report.pdf ./report.pdf

# Specify encryption key
quicbaas object put tenant-acme:secret/data.json ./data.json \
  --encryption-key "dek:tenant/acme/secrets"

# Disable encryption (if allowed)
quicbaas object put tenant-acme:public/readme.txt ./readme.txt \
  --no-encrypt
```

### Check Encryption Status

```bash
# View encryption in catalog entry
quicbaas catalog get tenant-acme:docs/report.pdf

# List encrypted objects
quicbaas object list --prefix tenant-acme: --encrypted

# List objects with specific key
quicbaas object list --encryption-key "dek:tenant/acme/objects"
```

## REST API

### Encryption Headers

| Header | Description |
|--------|-------------|
| `X-QuicBaaS-Encryption-Key` | Override encryption key ID |
| `X-QuicBaaS-No-Encrypt` | Disable encryption (if allowed) |

### Example

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

## Security Considerations

### Key Management

- DEKs are stored securely in KMS with master key encryption
- Keys support versioning for rotation without data loss
- Key access is controlled via RBAC policies

### At-Rest Encryption

- All encrypted data is stored as ciphertext in backends
- Backend-native encryption can be used in addition (defense in depth)
- Encryption metadata stored separately from ciphertext

### In-Transit Encryption

- TLS required for all API communication
- Plaintext never transmitted over network

### Access Control

- Encryption key access requires appropriate RBAC permissions
- Per-tenant key isolation prevents cross-tenant access
- Audit logging tracks all encryption operations

## Troubleshooting

### Common Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| `encryption key not found` | DEK doesn't exist in KMS | Create DEK or check key path |
| `permission denied` | User lacks key access | Grant appropriate KMS permissions |
| `decryption failed` | Key version mismatch or corruption | Check key version, verify backup |
| `algorithm not supported` | Unknown encryption algorithm | Use supported algorithm |

### Debugging

```bash
# Check KMS key status
quicbaas kms get dek:tenant/acme/objects

# Verify encryption config resolution
quicbaas object put --dry-run tenant-acme:test.txt ./test.txt

# View catalog encryption metadata
quicbaas catalog get tenant-acme:docs/report.pdf --format json | jq .encryption
```
