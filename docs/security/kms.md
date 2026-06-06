# Key Management Service

Centralized encryption key lifecycle management with pluggable backend support.

## Overview

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

## CLI Usage

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

## Configuration

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

## Supported Algorithms

### Symmetric
- AES-128-GCM
- AES-192-GCM
- AES-256-GCM (recommended)
- ChaCha20-Poly1305

### Asymmetric
- RSA-2048, RSA-3072, RSA-4096
- ECDSA-P256, ECDSA-P384
- Ed25519

## Key Lifecycle

```
Created → Active → Rotated → Deprecated → Destroyed
```

## Cryptoperiod Enforcement (PCI DSS 3.6)

QuicBaaS enforces cryptoperiod expiry automatically through the **KMS rotation
scheduler**, a background goroutine that scans key metadata at a configurable
interval and rotates any key whose `RotationPolicy.AutoRotate` is `true` and
whose `NextRotation` timestamp has passed.

### How it works

1. On service start (inside the KMS + scheduler block of `newQuicBaaS`), a
   `KMSRotationScheduler` is constructed and started alongside the grace-period
   scheduler engine.
2. Every `KMSRotationScanInterval` (default: 5 minutes) the scheduler calls
   `kmsManager.ListKeys`, filters for expired `AutoRotate` keys, and calls
   `kmsServiceImpl.RotateKey` for each one under `auth.NewSystemContext` so
   that per-key authorization is bypassed for this internal daemon operation.
3. After a successful rotation, `NextRotation` is advanced by `Interval`
   (clamped to the first future boundary if the daemon was offline for more
   than one interval) and persisted via `kmsManager.UpdateStoredMetadata`.
4. Each rotation attempt — success or failure — is recorded via the
   `KMSAutoRotationAuditLogger` extension interface (see below). If the
   installed `KMSAuditLogger` does not implement this extension, the event is
   written to the structured logger instead.
5. The scheduler stops cleanly on `quicBaaS.Close()`, before the scheduler
   engine, ensuring no rotation scan is in flight during shutdown.

### Configuration

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

### Audit logging extension

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

### Setting a rotation policy on a key

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

## Build Tags for Backend Selection

QuicBaaS uses build tags to control which KMS backends are compiled into the binary.
This allows you to build efficient binaries with only the backends you need.

### Available Build Tags

| Tag | Backend | Description | Makefile Variable |
|-----|---------|-------------|-------------------|
| (none) | Software | Always included - in-memory/file-based keys | - |
| `awskms` | AWS KMS | Amazon Web Services Key Management | `WITH_AWS=1` |
| `gcpkms` | GCP KMS | Google Cloud Platform Key Management | `WITH_GCP=1` |
| `azurekv` | Azure Key Vault | Microsoft Azure Key Vault | `WITH_AZURE=1` |
| `vault` | HashiCorp Vault | HashiCorp Vault Transit secrets engine | `WITH_VAULT=1` |
| `pkcs11` | PKCS#11 | Hardware Security Modules (requires CGO) | `WITH_PKCS11=1` |
| `quantum` | Quantum-safe | ML-DSA, ML-KEM post-quantum algorithms | `WITH_QUANTUM=1` |

### Building with Makefile

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

### Building with go build

You can also use go build directly with tags:

```bash
# AWS KMS support
go build -tags awskms ./cmd/quicbaas/

# Multiple backends
go build -tags "awskms gcpkms vault" ./cmd/quicbaas/

# Default (software only)
go build ./cmd/quicbaas/
```

### Multi-Backend Registry

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

### Determining Available Backends

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

## Go SDK

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

## Per-Key Authorization (NIST 800-53 AC-3/AC-6)

Every cryptographic operation — Encrypt, Decrypt, Sign, Verify, GenerateDataKey — is
gated by a per-key policy before the underlying key material is touched. Authorization
is enforced at the **service layer** so that gRPC, REST, QUIC, MCP, and the embedded
path all share exactly the same check.

### How it works

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

### Key policy management

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

### Enable per-key authz

```go
svc := service.NewKMSService(mgr, logger,
    service.WithKMSAuthzEnabled(true),
)
```

In production the flag defaults to `false` for backward compatibility. New deployments
must set it to `true`. The `WithKMSAuthzEnabled` option on `kmsServiceImpl` also
propagates to the `cryptoService` when constructed via the service builder.

## Key-Use Audit (NIST 800-53 AU-2 / PCI 3.6)

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

## Security Features

1. **Key Isolation**: Per-key policy with AllowedPrincipals / DeniedPrincipals
2. **Fail-Closed Authz**: No authenticated subject → deny; policy load failure → deny
3. **Audit Logging**: All key-use operations logged (allow and deny)
4. **Key Rotation**: Regular rotation policies with cryptoperiod tracking
5. **Secure Storage**: Keys encrypted at rest, policies in Raft KVStore
6. **No Export**: Keys never leave KMS in plaintext
7. **System Context Bypass**: Internal daemon operations use `auth.NewSystemContext`;
   this path is inaccessible from any network transport

## Performance

- Software backend: 100K+ ops/sec
- Cloud backends: Provider-dependent rate limits
- Key caching: Configurable metadata TTL
- Batch operations: Multiple encrypt/decrypt in single call

## See Also

- [Crypto](crypto.md) - Encryption as a Service
- [Secrets](secrets.md) - Secret Storage
- [Cluster DEK](cluster-dek.md) - Cluster encryption keys
- [Security Overview](README.md)
