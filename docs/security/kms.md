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

## Security Features

1. **Key Isolation**: Cryptographically isolated keys
2. **Access Control**: Fine-grained permissions per key
3. **Audit Logging**: All operations logged
4. **Key Rotation**: Regular rotation policies
5. **Secure Storage**: Keys encrypted at rest
6. **No Export**: Keys never leave KMS in plaintext

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
