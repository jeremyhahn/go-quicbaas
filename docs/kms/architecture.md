# KMS — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/kms/`.

## Overview

QuicBaaS KMS provides distributed key management through Raft consensus. All key operations are replicated across the cluster, ensuring consistency and high availability.

## Components

```
┌─────────────────────────────────────────────────────────────┐
│                     KMS Service                             │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  REST API   │  │  gRPC API   │  │      MCP API        │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │            │
│         └────────────────┼─────────────────────┘            │
│                          ▼                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │               KMS State Machine                       │   │
│  │         (Raft Replicated Operations)                  │   │
│  └──────────────────────────┬───────────────────────────┘   │
│                             ▼                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │               go-xkms Facade                      │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌─────────┐  │   │
│  │  │ Software │ │  TPM2    │ │ PKCS#11  │ │Cloud KMS│  │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └─────────┘  │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Key Hierarchy

```
┌─────────────────────────────────────┐
│      Root Key (HSM/TPM/Cloud)       │
│   Never leaves hardware/cloud       │
└──────────────────┬──────────────────┘
                   │ seals
                   ▼
┌─────────────────────────────────────┐
│     Master Key (Per Backend)        │
│   Seals/unseals DEKs                │
└──────────────────┬──────────────────┘
                   │ seals
                   ▼
┌─────────────────────────────────────┐
│    Data Encryption Key (DEK)        │
│   Encrypts application data         │
│   Replicated via Raft               │
└──────────────────┬──────────────────┘
                   │ encrypts
                   ▼
┌─────────────────────────────────────┐
│         Application Data            │
│   Secrets, user data, etc.          │
└─────────────────────────────────────┘
```

## KMS State Machine

The KMS uses a dedicated Raft state machine for key replication.

### State Machine Operations

| Op Code | Operation | Description |
|---------|-----------|-------------|
| 0x01 | CreateKey | Create new key entry |
| 0x02 | DeleteKey | Remove key (soft delete) |
| 0x03 | RotateKey | Create new key version |
| 0x04 | SetDEK | Set cluster DEK |
| 0x05 | RotateDEK | Rotate cluster DEK |
| 0x06 | ExportKey | Export key material |
| 0x07 | ImportKey | Import key material |
| 0x08 | UpdateUsage | Update AEAD usage counters |

### Key Entry Structure

```go
type KMSKeyEntry struct {
    ID           string            // Unique key identifier
    Type         string            // "symmetric", "sealing"
    Algorithm    string            // "AES-256-GCM", etc.
    SealedData   []byte            // Key material (sealed)
    Version      uint64            // Key version
    UsageCount   uint64            // Operations performed
    BytesUsed    uint64            // Bytes encrypted
    CreatedAt    int64
    RotatedAt    int64
    Metadata     map[string]string
}
```

### Binary Command Format

```
CREATE:     [0x01][IDLen:2B][ID][Algorithm:1B][Metadata...]
DELETE:     [0x02][IDLen:2B][ID]
ROTATE:     [0x03][IDLen:2B][ID]
SET_DEK:    [0x04][SealedLen:4B][SealedData]
UPDATE:     [0x08][IDLen:2B][ID][UsageCount:8B][BytesUsed:8B]
```

## AEAD Safety Tracking

go-xkms tracks cryptographic limits per key:

| Algorithm | Nonce Limit | Bytes Limit |
|-----------|-------------|-------------|
| AES-128-GCM | 2^32 operations | 64 GB |
| AES-256-GCM | 2^32 operations | 64 GB |
| ChaCha20-Poly1305 | 2^64 operations | 256 GB |
| XChaCha20-Poly1305 | 2^96 operations | 256 GB |

### Safety Flow

```
1. Encrypt request arrives
2. Check usage counters in state machine
3. If approaching limits, trigger rotation
4. Perform encryption via go-xkms
5. Update usage counters (Raft replicated)
```

## Cluster DEK Management

### Initialization

```
1. Leader generates DEK via go-xkms
2. DEK sealed with backend-specific key
3. Sealed DEK replicated via Raft
4. Each node unseals locally for use
```

### Rotation

```
1. Leader generates new DEK
2. Old DEK marked for phase-out
3. New sealed DEK replicated
4. Background re-encryption of data
5. Old DEK deleted after migration
```

## Multi-Backend Routing

The KMS Manager supports intelligent routing to multiple backends through the `BackendRouter` component (`pkg/kms/router.go`).

### Routing Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                       KMS Manager                                │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                   BackendRouter                          │    │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │    │
│  │  │ Key ID      │  │ Algorithm   │  │ Health-Based    │  │    │
│  │  │ Prefix      │  │ Routing     │  │ Failover        │  │    │
│  │  │ Routing     │  │             │  │                 │  │    │
│  │  └──────┬──────┘  └──────┬──────┘  └────────┬────────┘  │    │
│  │         └────────────────┼──────────────────┘           │    │
│  └──────────────────────────┼──────────────────────────────┘    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                   BackendRegistry                         │   │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────────┐  │   │
│  │  │software │  │  hsm    │  │  aws    │  │   vault     │  │   │
│  │  │priority:1│  │priority:10│ │priority:5│  │ priority:5 │  │   │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### Routing Flow

The `BackendRouter.Route()` method selects backends in this priority order:

```
┌──────────────────────────────────────────────────────────────┐
│                    Route(keyID, algorithm)                    │
├──────────────────────────────────────────────────────────────┤
│  1. Parse Key ID                                              │
│     ├── Structured? (backend:keytype:algorithm:keyname)       │
│     │   └── YES → Try backend from key ID prefix              │
│     │             ├── Available? → RETURN backend             │
│     │             └── Not available → Continue to step 2      │
│     └── NO → Continue to step 2                               │
│                                                               │
│  2. Algorithm Routing                                         │
│     ├── Check AlgorithmRouting config map                     │
│     │   └── Match? → Try configured backend                   │
│     └── Query registry.GetByAlgorithm()                       │
│         └── Found? → RETURN backend                           │
│                                                               │
│  3. Default Backend                                           │
│     └── DefaultBackend configured?                            │
│         └── YES → Try default backend                         │
│             └── Available? → RETURN backend                   │
│                                                               │
│  4. Priority-Based Selection                                  │
│     └── registry.GetHealthy()                                 │
│         └── Returns highest priority healthy backend          │
│                                                               │
│  5. Fallback Default                                          │
│     └── Use fallback defaultBackend if set via option         │
│                                                               │
│  6. Error: No available backend                               │
└──────────────────────────────────────────────────────────────┘
```

### Structured Key ID Format

Keys can use a structured ID format for explicit backend routing:

```
backend:keytype:algorithm:keyname
```

The `ParseKeyID()` function parses this format into a `ParsedKeyID` struct:

| Field | Description | Example |
|-------|-------------|---------|
| Backend | Target backend name | `hsm`, `software`, `vault` |
| KeyType | Purpose of the key | `signing`, `encryption`, `master` |
| Algorithm | Cryptographic algorithm | `ecdsa-p256`, `aes-256-gcm` |
| KeyName | User-provided key name | `my-signing-key` |

Examples:
- `hsm:signing:ecdsa-p256:my-signing-key` - Routes to HSM backend
- `software:encryption:aes-256-gcm:data-key` - Routes to Software backend
- `vault:master:rsa-4096:root-key` - Routes to HashiCorp Vault backend
- `my-simple-key` - Unstructured, uses default routing

### Failover Behavior

The `RouteWithFailover()` method provides automatic failover:

```go
err := router.RouteWithFailover(ctx, keyID, algorithm, func(backend Backend) error {
    return backend.Encrypt(ctx, keyID, plaintext)
})
```

Failover behavior is controlled by `RoutingConfig`:

| Option | Default | Description |
|--------|---------|-------------|
| `FailoverEnabled` | `true` | Enable automatic failover on failure |
| `MaxFailoverAttempts` | `3` | Maximum backends to try before returning error |

Failover candidates are selected from:
1. All enabled backends with `Health.Status != Unhealthy`
2. Backends supporting the requested algorithm (if specified)
3. Sorted by priority (lower number = higher preference)
4. Default backend added as last resort if not already included

### Backend Registry

The `BackendRegistry` (`pkg/kms/registry.go`) manages backend lifecycle:

| Feature | Description |
|---------|-------------|
| Health Tracking | Automatic health checks with configurable intervals (default: 30s) |
| Priority Selection | Lower priority value = higher preference |
| Algorithm Support | Route by supported cryptographic algorithms |
| Health Status | `healthy`, `degraded`, `unhealthy`, `unknown` |
| Auto-Recovery | Backends transition through `degraded` state during recovery |

Registry health check configuration:

| Option | Default | Description |
|--------|---------|-------------|
| `HealthCheckInterval` | 30s | Time between health checks |
| `HealthCheckTimeout` | 5s | Timeout for each health check |
| `UnhealthyThreshold` | 3 | Consecutive failures before marking unhealthy |
| `RecoveryThreshold` | 2 | Consecutive successes before marking healthy |

### Supported Backend Types

Using go-xkms's `BackendType` constants:

| Type | Description |
|------|-------------|
| `BackendTypeSoftware` | In-memory software implementation |
| `BackendTypePKCS11` | Hardware Security Module via PKCS#11 |
| `BackendTypeSmartCardHSM` | SmartCard-HSM devices |
| `BackendTypeTPM2` | Trusted Platform Module 2.0 |
| `BackendTypeAWSKMS` | AWS Key Management Service |
| `BackendTypeGCPKMS` | Google Cloud KMS |
| `BackendTypeAzureKV` | Azure Key Vault |
| `BackendTypeVault` | HashiCorp Vault |
| `BackendTypeQuantum` | Post-quantum algorithms |
| `BackendTypeThreshold` | Threshold signatures |
| `BackendTypeSymmetric` | Symmetric key operations |

## Multi-Backend Support

Each node can use a different backend:

```
Node A (AWS)           Node B (GCP)           Node C (Azure)
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│ KMS State    │      │ KMS State    │      │ KMS State    │
│ Machine      │ ←──→ │ Machine      │ ←──→ │ Machine      │
└──────┬───────┘      └──────┬───────┘      └──────┬───────┘
       │                     │                     │
       ▼                     ▼                     ▼
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│   AWS KMS    │      │   GCP KMS    │      │  Azure KV    │
└──────────────┘      └──────────────┘      └──────────────┘
```

- Sealed data format is backend-agnostic
- Each node unseals using its local backend
- Provides multi-cloud redundancy

## Security Considerations

### Key Protection

- Keys never stored in plaintext on disk
- Sealed via go-xkms backend
- Hardware backends (TPM/HSM) bind to device

### Access Control

- RBAC policies for key operations
- Audit logging for all operations
- Principle of least privilege

### Recovery

- Quorum required for key operations
- Snapshot includes sealed key material
- Node recovery re-seals with local backend


---

## Key Management Service

Centralized encryption key lifecycle management with pluggable backend support.

### Overview

QuicBaaS KMS supports multiple backends:

| Backend | Description | Status |
|---------|-------------|--------|
| Software | In-memory/file-based key storage | Production |
| AWS KMS | Amazon Web Services | Build tag: awskms |
| GCP KMS | Google Cloud Platform | Build tag: gcpkms |
| Azure | Microsoft Azure Key Vault | Build tag: azurekms |
| Vault | HashiCorp Vault | Build tag: vault |
| PKCS#11 | Hardware Security Modules | Build tag: pkcs11 |
| TPM 2.0 | Trusted Platform Module | Build tag: raft |

### CLI Usage

```bash
# Generate key
quicbaas kms generate --name mykey --algorithm AES-256-GCM

# List keys
quicbaas kms list
quicbaas kms list --prefix prod-

# Get key metadata
quicbaas kms get mykey
quicbaas kms get mykey --version 2

# Rotate key
quicbaas kms rotate mykey

# Encrypt/decrypt
quicbaas kms encrypt mykey --data "sensitive"
quicbaas kms decrypt mykey --data "ciphertext..."

# Delete key (30-day pending)
quicbaas kms delete mykey
quicbaas kms delete mykey --force

# Import key
quicbaas kms import --name imported-key \
  --material /path/to/keyfile \
  --algorithm AES-256-GCM
```

### Configuration

```yaml
kms:
  backend: software

  # Software backend (default)
  software:
    key_store_path: /var/lib/quicbaas/keys
    cert_store_path: /var/lib/quicbaas/certs

  # AWS KMS
  aws:
    region: us-east-1
    key_id: alias/quicbaas-master

  # GCP KMS
  gcp:
    project_id: my-project
    location: global
    key_ring: quicbaas
    key_name: master-key

  # Azure Key Vault
  azure:
    vault_url: https://myvault.vault.azure.net
    key_name: quicbaas-master

  # HashiCorp Vault
  vault:
    address: https://vault.example.com:8200
    token: ${VAULT_TOKEN}
    mount_path: crypto
    key_name: quicbaas
```

### Supported Algorithms

#### Symmetric
- AES-128-GCM
- AES-192-GCM
- AES-256-GCM (recommended)
- ChaCha20-Poly1305

#### Asymmetric
- RSA-2048, RSA-3072, RSA-4096
- ECDSA-P256, ECDSA-P384
- Ed25519

### Key Lifecycle

```
Created → Active → Rotated → Deprecated → Destroyed
```

### Cryptoperiod Enforcement (PCI DSS 3.6)

QuicBaaS enforces cryptoperiod expiry automatically through the **KMS rotation
system job**, a recurring scheduler job that scans key metadata at a
configurable interval and rotates any key whose `RotationPolicy` is enabled,
set to `AutoRotate`, and past its `NextRotation` timestamp.

#### How it works

1. On service start (inside the KMS + scheduler block of `newQuicBaaS`),
   `registerKMSRotation` registers the `kms.rotation` handler and declares the
   system job (`handlers.KMSRotationJob`) under the reserved ID
   `handlers.KMSRotationJobID`. The scheduler installs it on its next tick
   and retries every tick until the store accepts it (see
   [System jobs](../scheduler/architecture.md#system-jobs)).
2. Every `KMSRotationScanInterval` (default: 5 minutes) the job runs
   `kmsServiceImpl.RotateDueKeys`: it calls `kmsManager.ListKeys`, filters for
   due keys, and calls `RotateKey` for each under `auth.NewSystemContext` so
   that per-key authorization is bypassed for this internal operation.
3. After a successful rotation, `NextRotation` is advanced by `Interval`
   (clamped to the first future boundary if the daemon was offline for more
   than one interval) and persisted via `kmsManager.UpdateStoredMetadata`.
   A key whose rotation fails keeps its deadline and is retried by the next
   scan; one key's failure does not abort the scan.
4. Each rotation attempt — success or failure — is recorded via the
   `KMSAutoRotationAuditLogger` extension interface (see below). If the
   installed `KMSAuditLogger` does not implement this extension, the event is
   written to the structured logger instead.
5. A scan that cannot list keys fails the run with `KMSRotationScanError`; the
   scheduler records it and retries with backoff within that scan's retry
   budget; once the budget is spent the job moves on to its next scan. The
   budget is per firing, so failed scans never retire the job.

**Exactly once on a cluster.** The job is cluster-wide (no node affinity), so
only the Raft leader dispatches it, and every firing is arbitrated by the
scheduler's firing claim, so exactly one node runs each scan even during
leader churn. A standalone node runs every scan itself. The interval must be a
whole number of seconds (the scheduler's `@every` schedules resolve to
seconds); anything else fails service construction.

#### Configuration

```go
svc, err := service.NewService(
    service.WithConfig(&service.ServiceConfig{
        EnableScheduler:         true,
        KMSRotationScanInterval: 5 * time.Minute, // default; safe to omit
    }),
    service.WithKMSManager(kmsMgr),
    // ...
)
```

```yaml
# ServiceConfig fields (mapped from your YAML loader)
scheduler:
  enabled: true
kms:
  rotation_scan_interval: 5m   # how often the scheduler checks for expired keys
```

#### Audit logging extension

Implement `KMSAutoRotationAuditLogger` (in addition to `KMSAuditLogger`) to
receive structured rotation events in your audit store:

```go
type KMSAutoRotationAuditLogger interface {
    // LogKeyAutoRotation logs a cryptoperiod-driven auto-rotation attempt.
    // success is false when RotateKey returned an error.
    LogKeyAutoRotation(ctx context.Context, keyID, algorithm string,
                       success bool, errMsg string) error
}
```

Wire via `service.WithKMSAuditLogger(myLogger)` — when the logger also
implements `KMSAutoRotationAuditLogger`, rotation events are dispatched to it
automatically.

#### Setting a rotation policy on a key

The `RotationPolicy` lives on the `kms.KeyMetadata` stored in the KMS backend.
Update it via `kmsManager.UpdateStoredMetadata` or any KMS management API that
exposes the policy fields:

```go
meta.RotationPolicy = &kms.RotationPolicy{
    Enabled:      true,
    AutoRotate:   true,             // required for scheduler-driven rotation
    Interval:     90 * 24 * time.Hour, // PCI DSS 3.6: rotate every 90 days
    NextRotation: time.Now().Add(90 * 24 * time.Hour),
}
_ = kmsManager.UpdateStoredMetadata(meta)
```

### Build Tags for Backend Selection

QuicBaaS uses build tags to control which KMS backends are compiled into the binary.
This allows you to build efficient binaries with only the backends you need.

#### Available Build Tags

| Tag | Backend | Description | Makefile Variable |
|-----|---------|-------------|-------------------|
| (none) | Software | Always included - in-memory/file-based keys | - |
| `awskms` | AWS KMS | Amazon Web Services Key Management | `WITH_AWS=1` |
| `gcpkms` | GCP KMS | Google Cloud Platform Key Management | `WITH_GCP=1` |
| `azurekv` | Azure Key Vault | Microsoft Azure Key Vault | `WITH_AZURE=1` |
| `vault` | HashiCorp Vault | HashiCorp Vault Transit secrets engine | `WITH_VAULT=1` |
| `pkcs11` | PKCS#11 | Hardware Security Modules (requires CGO) | `WITH_PKCS11=1` |
| `quantum` | Quantum-safe | ML-DSA, ML-KEM post-quantum algorithms | `WITH_QUANTUM=1` |

#### Building with Makefile

The easiest way to build with specific backends is using the Makefile variables:

```bash
# Default build (software backend only)
make build

# Build with AWS support (AWS KMS + S3 object storage)
make build WITH_AWS=1

# Build with all cloud providers
make build WITH_AWS=1 WITH_GCP=1 WITH_AZURE=1

# Build with HSM support (requires CGO)
make build WITH_PKCS11=1

# Build with quantum-safe cryptography
make build WITH_QUANTUM=1

# Build with multiple backends
make build WITH_AWS=1 WITH_VAULT=1 WITH_QUANTUM=1

# FIPS build with AWS support
make build-fips WITH_AWS=1
```

#### Building with go build

You can also use go build directly with tags:

```bash
# AWS KMS support
go build -tags awskms ./cmd/quicbaas/

# Multiple backends
go build -tags "awskms gcpkms vault" ./cmd/quicbaas/

# Default (software only)
go build ./cmd/quicbaas/
```

#### Multi-Backend Registry

QuicBaaS uses a backend registry that automatically includes all backends available
based on build tags. At runtime, you can optionally filter to a subset using configuration:

```yaml
kms:
  enabled: true
  backend: software  # Default/preferred backend

  # Optional: Explicitly list which backends to use
  # If empty, uses all backends from go-xkms based on build tags
  enabled_backends:
    - software
    - awskms
```

Or via environment variables:
```bash
QUICBAAS_KMS_ENABLED=true
QUICBAAS_KMS_BACKEND=software
QUICBAAS_KMS_ENABLED_BACKENDS=software,awskms
```

#### Determining Available Backends

At runtime, the KMS service queries go-xkms for available backends:

```go
// List available backends (populated from build tags)
backends := kmsSvc.ListBackends()
for _, b := range backends {
    fmt.Printf("Backend: %s, Type: %s\n", b.Name, b.Type)
}

// Check backend count
fmt.Printf("Registered backends: %d\n", kmsSvc.BackendCount())
```

### Go SDK

```go
import (
    "context"
    "crypto/elliptic"
    "crypto/x509"

    "github.com/jeremyhahn/go-quicbaas/pkg/kms"
    "github.com/jeremyhahn/go-quicbaas/pkg/config"
    xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// Initialize go-xkms first (typically done at server startup)
err := xkmssdk.ServiceInitialize(xkmsConfig)

// Create backend factory to populate registry from go-xkms
factory, err := kms.NewBackendFactory(&kms.BackendFactoryConfig{
    Logger:              logger,
    EnabledBackends:     nil, // Use all available backends
    HealthCheckInterval: 30,
})

// Initialize registry from go-xkms's available backends
ctx := context.Background()
err = factory.InitializeFromXKMS(ctx, nil)

// Create KMS service with the registry
svc, err := kms.NewKMSService(&kms.KMSServiceOptions{
    Registry:         factory.Registry(),
    Logger:           logger,
    CacheKeyMaterial: false,
})

// Generate symmetric key (AES-256-GCM)
symAttrs := &xkmssdk.KeyAttributes{
    CN:                 "my-encryption-key",
    SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
    KeyType:            xkmssdk.KeyTypeEncryption,
}
key, err := svc.GenerateKey(ctx, symAttrs)

// Generate asymmetric key (ECDSA P-256 for signing)
signAttrs := &xkmssdk.KeyAttributes{
    CN:           "my-signing-key",
    KeyAlgorithm: x509.ECDSA,
    ECCAttributes: &xkmssdk.ECCAttributes{
        Curve: elliptic.P256(),
    },
    KeyType: xkmssdk.KeyTypeSigning,
}
signingKey, err := svc.GenerateKey(ctx, signAttrs)

// Generate RSA key
rsaAttrs := &xkmssdk.KeyAttributes{
    CN:           "my-rsa-key",
    KeyAlgorithm: x509.RSA,
    RSAAttributes: &xkmssdk.RSAAttributes{
        KeySize: 4096,
    },
    KeyType: xkmssdk.KeyTypeSigning,
}
rsaKey, err := svc.GenerateKey(ctx, rsaAttrs)

// Generate data encryption key (DEK) wrapped by a key encryption key (KEK)
kekAttrs := &xkmssdk.KeyAttributes{CN: "my-kek"}
plaintextDEK, wrappedDEK, err := svc.GenerateDataKey(ctx, kekAttrs, 32)

// Encrypt/decrypt
ciphertext, err := svc.Encrypt(ctx, "my-encryption-key", []byte("sensitive"), nil)
plaintext, err := svc.Decrypt(ctx, "my-encryption-key", ciphertext, nil)

// Sign/verify
digest := sha256.Sum256([]byte("message to sign"))
signature, err := svc.Sign(ctx, "my-signing-key", digest[:], "SHA256")
valid, err := svc.Verify(ctx, "my-signing-key", digest[:], signature, "SHA256")
```

### Per-Key Authorization (NIST 800-53 AC-3/AC-6)

Every cryptographic operation — Encrypt, Decrypt, Sign, Verify, GenerateDataKey — is
gated by a per-key policy before the underlying key material is touched. Authorization
is enforced at the **service layer** so that gRPC, REST, QUIC, MCP, and the embedded
path all share exactly the same check.

#### How it works

1. The authentication middleware (gRPC interceptor / REST AuthzMiddleware / QUIC handler /
   MCP `CheckToolAuthorizationWithContext`) validates the caller's token and injects an
   `interfaces.Subject` into the request `context.Context` using `auth.SubjectIntoContext`.

2. `cryptoService` and `kmsServiceImpl` call `checkKeyAuthz(ctx, keyID, operation)` which:
   - Skips the check for system contexts (`auth.NewSystemContext`) used by internal
     daemon operations (scheduler, re-encryption) — these must never originate from
     network requests.
   - Denies immediately when `authz_enabled` is `false` (for test/dev deployments).
   - Denies when no authenticated `Subject` is found in the context (fail-closed).
   - Loads the stored `KeyPolicy` from the Raft-replicated KVStore under
     `kms:policy:<keyID>`.
   - Default-allows when no policy is stored (any authenticated principal may use the
     key until a policy is set).
   - Evaluates `AllowedPrincipals`, `DeniedPrincipals`, `AllowedOperations`, and
     `ExpiresAt`. Explicit deny entries always take precedence.

#### Key policy management

```bash
# Set a policy: only alice and service-account-1 may encrypt with this key
quicbaas kms policy set prod-key \
  --allow-principal alice \
  --allow-principal service-account-1 \
  --allow-op encrypt \
  --allow-op decrypt

# Retrieve the policy
quicbaas kms policy get prod-key

# Remove the policy (restore default-allow-all)
quicbaas kms policy delete prod-key
```

Policies are stored in the Raft-replicated KVStore so every cluster node evaluates
the same rules in real time with no TTL drift.

#### Enable per-key authz

```go
svc := service.NewKMSService(mgr, logger,
    service.WithKMSAuthzEnabled(true),
)
```

In production the flag defaults to `false` for backward compatibility. New deployments
must set it to `true`. The `WithKMSAuthzEnabled` option on `kmsServiceImpl` also
propagates to the `cryptoService` when constructed via the service builder.

### Key-Use Audit (NIST 800-53 AU-2 / PCI 3.6)

The `KMSKeyUseAuditLogger` interface extends `KMSAuditLogger` with a `LogKeyUse` method
that records every cryptographic operation (both allow **and** deny outcomes) with:

- Authenticated principal ID
- Key ID
- Operation (encrypt / decrypt / sign / verify / generate_data_key)
- Outcome (allow / deny)
- Timestamp

```go
type KMSKeyUseAuditLogger interface {
    KMSAuditLogger
    LogKeyUse(ctx context.Context, principal, keyID, operation string,
              outcome keyUseOutcome, timestamp time.Time) error
}
```

Wire in via `service.WithKMSAuditLogger(myLogger)`.

### Security Features

1. **Key Isolation**: Per-key policy with AllowedPrincipals / DeniedPrincipals
2. **Fail-Closed Authz**: No authenticated subject → deny; policy load failure → deny
3. **Audit Logging**: All key-use operations logged (allow and deny)
4. **Key Rotation**: Regular rotation policies with cryptoperiod tracking
5. **Secure Storage**: Keys encrypted at rest, policies in Raft KVStore
6. **No Export**: Keys never leave KMS in plaintext
7. **System Context Bypass**: Internal daemon operations use `auth.NewSystemContext`;
   this path is inaccessible from any network transport

### Performance

- Software backend: 100K+ ops/sec
- Cloud backends: Provider-dependent rate limits
- Key caching: Configurable metadata TTL
- Batch operations: Multiple encrypt/decrypt in single call

### See Also

- Crypto - Encryption as a Service
- Secrets - Secret Storage
- Cluster DEK - Cluster encryption keys
- [Security Overview](user.md)


---

## Crypto Service

Encryption as a Service - encrypt, decrypt, sign, and verify data without exposing keys.

### Status

Complete - Full implementation with CLI commands, gRPC/REST APIs, and service layer.

### Overview

Crypto provides:
- Encrypt/decrypt data without key exposure
- Sign and verify data
- Generate HMACs
- Generate secure random data
- Generate data encryption keys
- Key derivation

### CLI Usage

#### Encryption/Decryption

```bash
# Encrypt data
quicbaas crypto encrypt mykey --data "sensitive"

# Encrypt with context
quicbaas crypto encrypt mykey --data "sensitive" --context "user-123"

# Decrypt data
quicbaas crypto decrypt mykey --data "ciphertext..."

# Re-wrap with latest key version
quicbaas crypto rewrap mykey --data "old-ciphertext..."
```

#### Signing/Verification

```bash
# Sign data
quicbaas crypto sign signing-key --data "message"

# Sign with specific hash
quicbaas crypto sign signing-key --data "message" --algorithm SHA512

# Verify signature
quicbaas crypto verify signing-key \
  --data "message" \
  --signature "base64-signature"
```

#### Random Data

```bash
# Generate 32 random bytes (default)
quicbaas crypto random

# Generate specific size
quicbaas crypto random --bytes 64 --format hex
```

#### Data Encryption Keys

```bash
# Generate 256-bit AES key (default)
quicbaas crypto datakey mykey

# Generate 128-bit AES key
quicbaas crypto datakey mykey --bits 128
```

### Supported Algorithms

#### Encryption
- AES-128-GCM
- AES-192-GCM
- AES-256-GCM (recommended)
- ChaCha20-Poly1305

#### Signing
- RSA-2048, RSA-4096
- ECDSA-P256, ECDSA-P384
- Ed25519

#### Hashing
- SHA-256, SHA-384, SHA-512

### Go SDK

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/service"

// Encrypt data
encResp, err := cryptoService.Encrypt(ctx, &service.EncryptRequest{
    KeyID:     "mykey",
    Plaintext: []byte("sensitive"),
})

// Decrypt data
decResp, err := cryptoService.Decrypt(ctx, &service.DecryptRequest{
    KeyID:      "mykey",
    Ciphertext: encResp.Ciphertext,
})

// Sign data
signResp, err := cryptoService.Sign(ctx, &service.SignRequest{
    KeyID:   "signing-key",
    Message: []byte("data"),
})

// Verify signature
verifyResp, err := cryptoService.Verify(ctx, &service.VerifyRequest{
    KeyID:     "signing-key",
    Message:   []byte("data"),
    Signature: signResp.Signature,
})

// Generate random bytes
randomResp, err := cryptoService.GenerateRandom(ctx, &service.GenerateRandomRequest{
    NumBytes: 32,
})

// Generate data key
dekResp, err := cryptoService.GenerateDataKey(ctx, &service.GenerateDataKeyRequest{
    KeyID:   "mykey",
    KeySpec: service.KeySpec{Algorithm: service.KeyAlgAES256GCM},
})
```

### Security Features

1. **Key Isolation**: Crypto keys never leave QuicBaaS
2. **No Key Exposure**: Applications never see encryption keys
3. **Versioning**: All operations versioned
4. **Audit Trail**: Every operation can be logged
5. **Rate Limiting**: Protection against abuse
6. **Access Control**: Fine-grained permissions per key

### Use Cases

#### Application-Level Encryption

```bash
# Encrypt before storing
ENCRYPTED=$(quicbaas crypto encrypt mykey --data "SSN: 123-45-6789")

# Decrypt when retrieved
quicbaas crypto decrypt mykey --data "$ENCRYPTED"
```

#### Envelope Encryption

```bash
# Generate wrapped data key
quicbaas crypto datakey master-key --bits 256
# Use plaintext key to encrypt data
# Store ciphertext blob with encrypted data
```

#### Digital Signatures

```bash
# Sign message
SIG=$(quicbaas crypto sign signing-key --data "contract text")

# Verify later
quicbaas crypto verify signing-key --data "contract text" --signature "$SIG"
```

### See Also

- KMS - Key management
- Secrets - Secret storage
- Seal/Unseal - Master key protection
- [Security Overview](user.md)


---

## Per-Database Encryption

QuicBaaS provides per-database encryption through a 3-tier key hierarchy.
Each tenant database receives its own Data Encryption Key (DEK), providing
cryptographic isolation between tenants and between databases within the
same tenant.

### Key Hierarchy

```
Master Key (KMS / HSM)
  |
  +-- Global DEK (dek:global)
  |     Used for platform-level encryption
  |
  +-- Tenant KEK (dek:tenant:acme)
  |     |
  |     +-- Database DEK (dek:tenant:acme:db:orders)
  |     +-- Database DEK (dek:tenant:acme:db:logs)
  |     +-- Database DEK (dek:tenant:acme:db:system)
  |
  +-- Tenant KEK (dek:tenant:globex)
        |
        +-- Database DEK (dek:tenant:globex:db:app)
        +-- Database DEK (dek:tenant:globex:db:system)
```

#### Key Types

| Key | DEKID Format | Scope | Lifecycle |
|-----|-------------|-------|-----------|
| Master Key | Managed by KMS/HSM | Global | External rotation |
| Global DEK | `dek:global` | Platform | Initialized at startup |
| Tenant KEK | `dek:tenant:{tenantID}` | Per-tenant | Created/deleted with tenant |
| Database DEK | `dek:tenant:{tenantID}:db:{dbName}` | Per-database | Created/deleted with database |

### DEK ID Format

DEK IDs follow a hierarchical format defined in `pkg/secrets/dekid.go`:

```
dek:global                           -- Global DEK
dek:tenant:{tenantID}                -- Tenant KEK
dek:tenant:{tenantID}:db:{dbName}    -- Database DEK
```

#### Helper Functions

```go
// Create DEK IDs
secrets.DEKIDGlobal                           // "dek:global"
secrets.NewTenantDEKID("acme")                // "dek:tenant:acme"
secrets.DatabaseDEKID("acme", "orders")       // "dek:tenant:acme:db:orders"

// Parse DEK IDs
secrets.ParseDEKID("dek:tenant:acme:db:orders")
secrets.ParseDatabaseDEKID("dek:tenant:acme:db:orders") // returns ("acme", "orders", nil)

// Classify DEK IDs
secrets.IsGlobal(dekid)    // true for "dek:global"
secrets.IsTenant(dekid)    // true for "dek:tenant:acme" (not database-scoped)
secrets.IsDatabase(dekid)  // true for "dek:tenant:acme:db:orders"

// Extract components
secrets.GetTenantID(dekid)      // ("acme", true)
secrets.GetDatabaseInfo(dekid)  // ("acme", "orders", true)
```

### EnsureDatabaseDEK

`EnsureDatabaseDEK` creates or retrieves the DEK for a tenant's database.
The method is **idempotent** -- calling it multiple times for the same
tenant/database combination returns the existing DEK without error.

```go
dekid, err := secretsDEK.EnsureDatabaseDEK(ctx, "acme", "orders")
// dekid = "dek:tenant:acme:db:orders"
```

#### What it does:

1. Validates tenant ID and database name (non-empty after trimming)
2. Computes the storage path: `_internal/secrets/dek/database/{tenantID}/{dbName}`
3. Checks if a DEK already exists at that path
4. If not, generates a new 256-bit AES key via the KMS
5. Wraps the key with the master key and stores the `DEKEntry`
6. Returns the `DEKID`

#### Called during:

- Database creation (`DatabaseService.CreateDatabase`)
- Tenant provisioning (for the system database)

### DeleteDatabaseDEK

`DeleteDatabaseDEK` removes the DEK for a tenant's database. Called when
a database is dropped.

```go
err := secretsDEK.DeleteDatabaseDEK(ctx, "acme", "orders")
```

#### What it does:

1. Validates tenant ID and database name
2. Deletes the DEK entry from storage
3. Clears the DEK from the in-memory cache
4. Logs the deletion

#### Called during:

- Database drop (`DatabaseService.DropDatabase`)
- Tenant deprovisioning (for all tenant databases)

### Barrier Seal/Unseal Lifecycle

Databases with barrier encryption enabled follow this lifecycle:

#### 1. Create (Sealed)

```
CreateDatabase("orders", barrier=true)
  |
  +-- EnsureDatabaseDEK("acme", "orders")
  |     Creates DEK: dek:tenant:acme:db:orders
  |     Wraps DEK with master key
  |     Stores at _internal/secrets/dek/database/acme/orders
  |
  +-- Create database with barrier config
  |     Database starts in SEALED state
  |
  +-- Unseal barrier
        Unwraps DEK using master key
        Database transitions to OPEN state
```

#### 2. Operational (Unsealed)

While unsealed, the unwrapped DEK is cached in memory. All read/write
operations encrypt/decrypt data transparently using the database DEK.

The DEK cache has a configurable TTL (default 5 minutes) and maximum
size (default 100 entries). Expired entries are cleaned up automatically.

#### 3. Seal

```
SealDatabase("orders")
  |
  +-- Wipe unwrapped DEK from memory cache
  +-- Database transitions to SEALED state
  +-- All read/write operations return errors
```

#### 4. Unseal

```
UnsealDatabase("orders", masterKey)
  |
  +-- Load wrapped DEK from storage
  +-- Unwrap DEK using master key
  +-- Cache unwrapped DEK in memory
  +-- Database transitions to OPEN state
```

#### 5. Drop

```
DropDatabase("orders")
  |
  +-- Close database
  +-- DeleteDatabaseDEK("acme", "orders")
  |     Removes DEK from storage and cache
  |
  +-- Delete all data files
```

### Storage Layout

DEKs are stored in the platform shard (shard 1) under internal key paths:

```
_internal/secrets/dek/
  global                          -- Global DEK entry
  tenant/
    acme                          -- Tenant KEK for "acme"
    globex                        -- Tenant KEK for "globex"
  database/
    acme/
      orders                      -- Database DEK for acme_orders
      logs                        -- Database DEK for acme_logs
      system                      -- Database DEK for acme_system
    globex/
      app                         -- Database DEK for globex_app
  versions/
    dek/global/v1                 -- Global DEK version 1
    dek/tenant/acme/v1            -- Tenant KEK version 1
    dek/tenant/acme/db/orders/v1  -- Database DEK version 1
```

### DEK Entry Format

Each DEK is stored as a JSON-encoded `DEKEntry`:

```json
{
  "dekid": "dek:tenant:acme:db:orders",
  "wrapped_key": "<base64-encoded-wrapped-key>",
  "algorithm": "AES-256-GCM",
  "version": 1,
  "created_at": "2026-03-10T14:30:00Z",
  "last_rotated_at": "2026-03-10T14:30:00Z"
}
```

### Cryptographic Properties

- **Algorithm**: AES-256-GCM (default, 32-byte key)
- **Key wrapping**: DEKs are wrapped (encrypted) with the master key via KMS
- **Memory safety**: Plaintext key material is zeroed after use
- **Cache isolation**: Each DEK is cached independently with expiry
- **Version retention**: Old DEK versions are retained for decrypting
  historical data (default: 3 versions)

### Agent Session Logs and Session State

Agent session logs (stored in `pkg/orchestration/KVSessionEventLog`) and
persisted agent session state (stored by `pkg/fleet/Runtime`) are written as
**plaintext JSON into qrdb**. At-rest confidentiality is provided entirely by
**qrdb's `StorageBarrier`**, which transparently encrypts every value written
to the underlying PebbleDB store before it reaches disk.

There is **no application-level envelope DEK** wrapping these payloads:

- A per-tenant application-layer DEK (`security.TenantDEKManager`) was
  previously applied as a second encryption pass before qrdb ingested the data.
  A compliance audit established that this was a redundant envelope: it was
  ineffective against the online-insider threat model (the same process unwraps
  it) and fully superseded by qrdb's at-rest barrier for the disk-level threat.
  It was removed in the Phase 3 security hardening.

- **Tenant-level cryptographic isolation** (crypto-shred on tenant deletion,
  operator key isolation) is achieved by configuring a dedicated per-tenant
  qrdb barrier sealed by any `go-xkms` hardware or cloud strategy. This is an
  operator configuration choice — the data plane barrier routing is not
  hardcoded. See [qrdb barrier documentation](../barrier/architecture.md).

- **Per-key KMS authorization** for finer-grained access control is enforced
  by the KMS authorization layer (Phase 3 control). This is orthogonal to
  at-rest encryption.

### Cross-References

- [Multi-Tenant Architecture](../multi-tenancy/architecture.md)
- [Database API Reference](../data-services/api.md)
- Cluster DEK Management
- KMS Multi-Tenancy
