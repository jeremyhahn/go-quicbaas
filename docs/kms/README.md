# QuicBaaS Key Management Service

Distributed key management with Raft replication, hardware security module support, and AEAD safety tracking.

## Overview

QuicBaaS KMS provides centralized key management for all cryptographic operations. All key operations are replicated via Raft consensus for high availability. Keys are managed through go-xkms, supporting software, hardware (TPM2, PKCS#11), and cloud (AWS KMS, Azure, GCP) backends.

## Key Features

| Feature | Description |
|---------|-------------|
| **Raft Replication** | All key operations replicated via consensus |
| **Multi-Backend** | Software, TPM2, PKCS#11, AWS KMS, Azure, GCP |
| **AEAD Safety** | Per-key and per-DEK usage tracking with NIST SP 800-38D limits |
| **Hierarchical DEKs** | Tenant/namespace/app scoped encryption keys |
| **Key Hierarchy** | Master -> DEK -> User Keys -> Data encryption |
| **Key Rotation** | Zero-downtime key rotation with versioning |
| **Hardware Sealing** | TPM/HSM-bound keys for enhanced security |
| **Key Export/Import** | BYOK support with secure key wrapping |

## Architecture

```
+-----------------------------------------------------------------+
|                         QuicBaaS KMS                            |
+-----------------------------------------------------------------+
|                                                                 |
|   quicbaas.yml defines backends    No runtime init needed       |
|    (software, tpm2, awskms, etc.)  Multiple backends supported  |
|                                                                 |
+-----------------------------------------------------------------+
|                                                                 |
|   Seal/Unseal Flow:                                             |
|   ┌─────────┐     ┌─────────────┐     ┌─────────────────────┐  |
|   │ Sealed  │────>│  Unsealing  │────>│     Unsealed        │  |
|   └─────────┘     └─────────────┘     │  DEKs available     │  |
|                                        │  Keys accessible    │  |
|                                        └─────────────────────┘  |
|                                                                 |
+-----------------------------------------------------------------+
|                                                                 |
|   DEK Hierarchy (path-based):                                   |
|                                                                 |
|   /                          Cluster-wide default DEK           |
|   tenant-123/                Tenant-scoped DEK                  |
|   tenant-123/production/     Namespace within tenant            |
|   tenant-123/production/app/ Application-specific               |
|                                                                 |
+-----------------------------------------------------------------+
|                                                                 |
|   Encryption Modes:                                             |
|                                                                 |
|   Mode 1: Direct Encryption                                     |
|     DEK encrypts secrets directly                               |
|     └── DEK -> encrypt(secret) -> ciphertext                    |
|                                                                 |
|   Mode 2: Envelope Encryption                                   |
|     DEK wraps user keys, user keys encrypt data                 |
|     └── DEK -> wrap(user_key) -> encrypted_key                  |
|     └── user_key -> encrypt(data) -> ciphertext                 |
|                                                                 |
+-----------------------------------------------------------------+
```

## Quick Start

### Create a Key

```bash
# Create AES-256-GCM key (uses cluster DEK for wrapping)
quicbaas kms key generate my-key

# Create key with specific algorithm
quicbaas kms key generate fast-key --algorithm ChaCha20-Poly1305

# Create key in tenant namespace (uses tenant DEK)
quicbaas kms key generate tenant-123/prod/api-key
```

### List Keys

```bash
# List all keys
quicbaas kms key list

# List keys for a tenant
quicbaas kms key list tenant-123/ --recursive
```

### Encrypt/Decrypt

```bash
# Encrypt data
quicbaas kms encrypt my-key --data "secret value"

# Decrypt data
quicbaas kms decrypt my-key --data "<ciphertext>"
```

### Rotate Key

```bash
quicbaas kms key rotate my-key
```

### DEK Management

```bash
# Create tenant DEK
quicbaas kms dek create tenant-123/

# View DEK status
quicbaas kms dek status tenant-123/

# Rotate DEK
quicbaas kms dek rotate tenant-123/
```

## Key Types

| Type | Algorithm | Use Case |
|------|-----------|----------|
| `asymmetric` | Ed25519 | Master keys, signing, key agreement (default) |
| `asymmetric` | ECDSA-P256/P384 | Master keys, signing |
| `asymmetric` | RSA-2048/4096 | Master keys, encryption, signing |
| `symmetric` | AES-128/192/256-GCM | DEKs, data encryption |
| `symmetric` | ChaCha20-Poly1305 | DEKs, data encryption (constant-time) |
| `symmetric` | XChaCha20-Poly1305 | DEKs, data encryption (extended nonce) |
| `sealing` | Backend-specific | Key wrapping, DEK protection |

## Backend Support

| Backend | Description | Hardware |
|---------|-------------|----------|
| `software` | PKCS#8 key files | No |
| `tpm2` | TPM 2.0 sealed keys | Yes |
| `pkcs11` | HSM via PKCS#11 | Yes |
| `awskms` | AWS Key Management Service | Cloud |
| `azurekv` | Azure Key Vault | Cloud |
| `gcpkms` | GCP Cloud KMS | Cloud |

## AEAD Safety

KMS tracks cryptographic usage per NIST SP 800-38D recommendations:

| Metric | Default Limit | Description |
|--------|---------------|-------------|
| Operations | 2^32 (4.29B) | Max encrypt/decrypt operations |
| Bytes | 350 GB | Max bytes encrypted |
| Warning | 80% | Threshold for warnings |
| Auto-rotate | 95% | Threshold for automatic rotation |

Safety limits can be configured at:
1. Server level (defaults for all keys/DEKs)
2. Per-DEK level (overrides server defaults)
3. Per-key level (overrides server and DEK defaults)

## Configuration

```yaml
# quicbaas.yaml
kms:
  enabled: true

  # Multiple backends can be enabled simultaneously
  backends:
    - software      # Always available, PKCS#8 key files
    - tpm2          # Hardware TPM 2.0 sealed keys
    - awskms        # AWS Key Management Service

  # Master key algorithm (asymmetric)
  defaults:
    algorithm: Ed25519    # Ed25519 | ECDSA-P256 | RSA-2048

  # DEK algorithm (symmetric for bulk encryption)
  dek:
    algorithm: AES-256-GCM

  # AEAD safety (NIST SP 800-38D)
  safety:
    enforce: true
    max_operations: 4294967296
    max_bytes: 375809638400
    warning_percent: 80
    auto_rotate_percent: 95
```

## Documentation

- [Architecture](architecture.md) - Key hierarchy and DEK path resolution
- [Configuration](configuration.md) - Complete configuration reference
- [API Reference](api-reference.md) - REST, gRPC, and MCP API
- [CLI Usage](cli-usage.md) - Command-line interface

## See Also

- [Secrets Documentation](../secrets/README.md) - Secrets engine
- [Security Overview](../security/README.md) - Security architecture
