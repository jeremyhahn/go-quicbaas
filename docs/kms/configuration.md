# KMS Configuration

## Configuration File

```yaml
# quicbaas.yaml
kms:
  enabled: true

  # Backend selection - multiple backends can be enabled simultaneously
  # Each backend provides different capabilities (software, hardware, cloud)
  # The system will use the most appropriate backend for each operation
  backends:
    - software      # Always available, PKCS#8 key files
    - tpm2          # Hardware TPM 2.0 sealed keys
    - pkcs11        # HSM via PKCS#11
    - awskms        # AWS Key Management Service
    - azurekv       # Azure Key Vault
    - gcpkms        # GCP Cloud KMS

  # Master key defaults (asymmetric keys for signing/key-agreement)
  # Master keys protect DEKs and are used for signing operations
  defaults:
    algorithm: Ed25519    # Ed25519 | ECDSA-P256 | ECDSA-P384 | RSA-2048 | RSA-4096

  # DEK defaults (symmetric keys for bulk encryption)
  dek:
    algorithm: AES-256-GCM    # AES-256-GCM | AES-128-GCM | ChaCha20-Poly1305

  # AEAD safety configuration (server-level defaults for DEKs)
  # Reference: NIST SP 800-38D - Recommendation for Block Cipher Modes of Operation: GCM
  safety:
    # When true: apply these defaults to ALL keys/DEKs without explicit AEAD config
    # When false: keys/DEKs without explicit AEAD config have no limits
    enforce: true

    # Default limits per NIST SP 800-38D recommendations for AES-GCM
    max_operations: 4294967296    # 2^32 operations (NIST SP 800-38D Section 8.3)
    max_bytes: 375809638400       # 350 GB (NIST SP 800-38D - 2^39 bits = 64GB per key, ~350GB conservative)
    warning_percent: 80           # Warn at 80% of limit
    auto_rotate_percent: 95       # Auto-rotate at 95% of limit
```

## Multi-Backend Routing

When multiple backends are configured, the `BackendRouter` intelligently routes operations to the appropriate backend based on routing rules, algorithm requirements, and backend health.

### RoutingConfig Options

The `RoutingConfig` struct (`pkg/kms/router.go`) controls routing behavior:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `DefaultBackend` | `string` | `""` | Backend name to use when no routing rule applies. If empty, uses highest priority healthy backend. |
| `FailoverEnabled` | `bool` | `true` | Enable automatic failover to backup backends on failure. |
| `MaxFailoverAttempts` | `int` | `3` | Maximum number of backends to try before returning error. |
| `AlgorithmRouting` | `map[string]string` | `{}` | Maps algorithm names to preferred backend names. |
| `BackendTypeRouting` | `map[BackendType]string` | `{}` | Maps backend types to preferred backend names. |

### Routing Configuration

```yaml
kms:
  backends:
    - software
    - hsm
    - awskms

  # Routing configuration (optional)
  routing:
    # Default backend when no routing rule applies
    default_backend: software

    # Enable automatic failover to backup backends
    failover_enabled: true

    # Maximum failover attempts before returning error
    max_failover_attempts: 3

    # Algorithm-to-backend routing
    # Maps algorithm names to backend names
    algorithm_routing:
      CHACHA20-POLY1305: software    # Software backend for ChaCha20
      RSA-4096: hsm                   # HSM for RSA operations
      AES-256-GCM: awskms             # AWS KMS for AES-GCM
      ECDSA-P256: hsm                 # HSM for ECDSA signing

    # Backend type routing (optional)
    # Maps backend types to specific backend instances
    backend_type_routing:
      pkcs11: hsm-primary
      software: local-software
```

### Structured Key IDs

Use structured key IDs for explicit backend routing. Format:

```
backend:keytype:algorithm:keyname
```

| Component | Description | Examples |
|-----------|-------------|----------|
| `backend` | Target backend name | `hsm`, `software`, `vault`, `awskms` |
| `keytype` | Purpose of the key | `signing`, `encryption`, `master`, `dek` |
| `algorithm` | Cryptographic algorithm | `ecdsa-p256`, `aes-256-gcm`, `rsa-4096` |
| `keyname` | User-provided key name | `my-key`, `tenant-123`, `root` |

Examples:
```bash
# Create signing key on HSM backend
quicbaas kms key generate hsm:signing:ecdsa-p256:my-signing-key

# Create encryption key on software backend
quicbaas kms key generate software:encryption:aes-256-gcm:data-key

# Create master key in HashiCorp Vault
quicbaas kms key generate vault:master:rsa-4096:root-key

# Use simple key ID (uses default routing)
quicbaas kms key generate my-simple-key
```

### Routing Priority

The router selects backends in this order:

1. **Key ID prefix** - If key ID is structured (e.g., `hsm:signing:...`), use specified backend
2. **Algorithm routing** - Check `algorithm_routing` config for algorithm-to-backend mapping
3. **Registry algorithm lookup** - Query registry for backends supporting the algorithm
4. **Default backend** - Use `default_backend` if configured
5. **Priority-based** - Select highest priority healthy backend from registry
6. **Fallback** - Use fallback default backend if set via router option

### Backend Priority

Lower priority number = higher preference:

```yaml
kms:
  backends:
    - name: hsm
      priority: 1      # Primary - hardware security
    - name: awskms
      priority: 5      # Secondary - cloud backup
    - name: software
      priority: 100    # Fallback only
```

### Failover Configuration

Automatic failover retries operations on backup backends when the primary fails:

```yaml
kms:
  routing:
    # Enable/disable failover (default: true)
    failover_enabled: true

    # Maximum backends to try (default: 3)
    # Set to 0 to try all available backends
    max_failover_attempts: 3
```

Failover candidates include:
- All enabled backends not marked as `unhealthy`
- Backends supporting the requested algorithm (if specified)
- Default backend added as last resort

### Health Check Configuration

Backend health is tracked by the `BackendRegistry`:

```yaml
kms:
  registry:
    # How often to check backend health (default: 30s)
    health_check_interval: 30s

    # Timeout for each health check (default: 5s)
    health_check_timeout: 5s

    # Failures before marking unhealthy (default: 3)
    unhealthy_threshold: 3

    # Successes before marking healthy after recovery (default: 2)
    recovery_threshold: 2
```

## Backend Configuration

Multiple backends can be configured simultaneously. Each backend is configured independently, and the system can use any enabled backend based on operation requirements.

### Software Backend (Default)

```yaml
kms:
  backends:
    - software
  software:
    # Key storage directory
    key_dir: /var/lib/quicbaas/keys
```

### TPM2 Backend

```yaml
kms:
  backends:
    - software
    - tpm2
  tpm2:
    # TPM device path
    device: /dev/tpmrm0
    # Persistent handle for sealing key
    handle: 0x81000002
    # PCR configuration
    pcr:
      banks:
        - sha256
      registers:
        - 0
        - 7
        - 9
```

### PKCS#11 Backend (HSM)

```yaml
kms:
  backends:
    - software
    - pkcs11
  pkcs11:
    # PKCS#11 library path
    library: /usr/lib/softhsm/libsofthsm2.so
    # Token label
    token_label: quicbaas
    # PIN (use environment variable)
    pin: ${PKCS11_PIN}
```

### AWS KMS Backend

```yaml
kms:
  backends:
    - software
    - awskms
  awskms:
    # AWS region
    region: us-west-2
    # KMS key for sealing
    key_id: arn:aws:kms:us-west-2:123456789:key/abcd1234
```

### Azure Key Vault Backend

```yaml
kms:
  backends:
    - software
    - azurekv
  azurekv:
    # Key Vault URL
    vault_url: https://myquicbaas.vault.azure.net
    # Key name for sealing
    key_name: quicbaas-seal
```

### GCP Cloud KMS Backend

```yaml
kms:
  backends:
    - software
    - gcpkms
  gcpkms:
    # Project ID
    project_id: my-project
    # Location
    location: us-west1
    # Key ring
    key_ring: quicbaas
    # Crypto key for sealing
    crypto_key: seal-key
```

## AEAD Safety Configuration

AEAD safety limits can be configured at three levels with the following inheritance:

```
┌─────────────────────────────────────────────────────────────────────┐
│                        AEAD Config Resolution                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Key/DEK has explicit AEAD config?                                   │
│      ├── YES → Use key/DEK's AEAD config                            │
│      └── NO  → Check kms.safety.enforce                             │
│                   ├── true  → Use kms.safety defaults               │
│                   └── false → No AEAD limits applied                │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Server-Level Defaults

```yaml
kms:
  safety:
    enforce: true                   # Apply defaults to keys without explicit config
    max_operations: 4294967296      # 2^32 operations
    max_bytes: 375809638400         # 350 GB
    warning_percent: 80
    auto_rotate_percent: 95
```

### Per-Key Configuration

```bash
# Set per-key limits
quicbaas kms key set-safety my-key \
  --max-operations 1000000000 \
  --max-bytes 100GB \
  --warning-percent 75

# Clear per-key config (revert to server defaults)
quicbaas kms key set-safety my-key --clear
```

### Per-DEK Configuration

```bash
# Set per-DEK limits
quicbaas kms dek set-safety tenant-123/ \
  --max-operations 10000000000 \
  --max-bytes 1TB \
  --warning-percent 75

# Clear per-DEK config (revert to server defaults)
quicbaas kms dek set-safety tenant-123/ --clear
```

### Use Cases

| Scenario | Configuration |
|----------|---------------|
| Compliance (strict limits) | `enforce: true` with conservative limits |
| Flexible (limits optional) | `enforce: false`, set per-key as needed |
| High-volume key | Per-key config with higher limits |
| Multi-tenant | Per-tenant DEK config with tenant-specific limits |

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_KMS_ENABLED` | Enable KMS | `false` |
| `QUICBAAS_KMS_BACKENDS` | Comma-separated backend list | `software` |
| `QUICBAAS_KMS_ALGORITHM` | Default master key algorithm | `Ed25519` |
| `QUICBAAS_KMS_DEK_ALGORITHM` | Default DEK algorithm | `AES-256-GCM` |
| `PKCS11_PIN` | PKCS#11 token PIN | - |
| `AWS_KMS_KEY_ID` | AWS KMS key ID | - |
| `AZURE_KEYVAULT_URL` | Azure Key Vault URL | - |

## Cluster DEK Configuration

```yaml
kms:
  cluster_dek:
    # Algorithm for DEK (symmetric - used for bulk data encryption)
    algorithm: AES-256-GCM    # AES-256-GCM | AES-128-GCM | ChaCha20-Poly1305
    # Rotation schedule
    rotation_period: 30d
    # Re-encryption batch size
    reencrypt_batch: 1000
    # Concurrent re-encryption workers
    reencrypt_workers: 4
```

## Performance Tuning

### High-Throughput

```yaml
kms:
  # Cache decryption keys in memory
  cache:
    enabled: true
    ttl: 5m
    max_keys: 100

  # Batch usage updates
  batch_updates:
    enabled: true
    interval: 100ms
    max_batch: 100
```

### Low-Latency

```yaml
kms:
  # Use Ed25519 for master keys (fast signing)
  defaults:
    algorithm: Ed25519

  # Use AES-GCM with AES-NI for DEKs
  dek:
    algorithm: AES-128-GCM

  # Disable batching
  batch_updates:
    enabled: false
```

## Complete Example

```yaml
kms:
  enabled: true

  # Multiple backends enabled
  backends:
    - software    # Always available as fallback
    - tpm2        # Hardware security for sealing

  # Software backend config
  software:
    key_dir: /var/lib/quicbaas/keys

  # TPM2 backend config
  tpm2:
    device: /dev/tpmrm0
    handle: 0x81000002
    pcr:
      banks:
        - sha256
      registers:
        - 0
        - 7
        - 9

  # Master key defaults (asymmetric)
  defaults:
    algorithm: Ed25519

  # DEK defaults (symmetric for bulk encryption)
  dek:
    algorithm: AES-256-GCM

  # NIST SP 800-38D compliant safety defaults
  safety:
    enforce: true
    max_operations: 4294967296
    max_bytes: 375809638400
    warning_percent: 80
    auto_rotate_percent: 95

  cluster_dek:
    algorithm: AES-256-GCM
    rotation_period: 30d
    reencrypt_batch: 1000
    reencrypt_workers: 4

  cache:
    enabled: true
    ttl: 5m
    max_keys: 100

  batch_updates:
    enabled: true
    interval: 100ms
    max_batch: 100
```
