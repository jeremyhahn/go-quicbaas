# Secrets Engine Architecture

## Overview

The QuicBaaS Secrets Engine provides secure storage for sensitive data with encryption at rest, versioning, and fine-grained access control.

## Components

```
┌─────────────────────────────────────────────────────────────┐
│                     Secrets Engine                          │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  REST API   │  │  gRPC API   │  │      MCP API        │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │            │
│         └────────────────┼─────────────────────┘            │
│                          ▼                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │               Encrypted Backend                      │   │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────────┐  │   │
│  │  │ Encryption │  │ Versioning │  │ Soft Delete    │  │   │
│  │  └────────────┘  └────────────┘  └────────────────┘  │   │
│  └──────────────────────────┬───────────────────────────┘   │
│                             ▼                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │               go-xkms Service                    │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌─────────┐  │   │
│  │  │ Software │ │  TPM2    │ │ PKCS#11  │ │Cloud KMS│  │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └─────────┘  │   │
│  └──────────────────────────────────────────────────────┘   │
│                             ▼                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │               Storage Backend                        │   │
│  │  ┌──────────────────┐  ┌─────────────────────────┐   │   │
│  │  │    PebbleDB      │  │   Raft Replication      │   │   │
│  │  └──────────────────┘  └─────────────────────────┘   │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Encryption Layer

### go-xkms Integration

All encryption operations route through go-xkms, providing:

- Unified API for multiple backends
- AEAD safety tracking (nonce uniqueness, bytes encrypted)
- Key lifecycle management
- Hardware security module support

### Encryption Flow

```
Secret Data → Serialize → Encrypt → Store
                           │
                           ▼
                    go-xkms
                    Encrypt(keyRef, plaintext, opts)
                           │
                           ▼
                    ┌──────────────┐
                    │ EncryptedData │
                    │ - Nonce       │
                    │ - Ciphertext  │
                    │ - Tag         │
                    │ - Algorithm   │
                    └──────────────┘
```

### Supported Algorithms

| Algorithm | Nonce Size | Tag Size | Key Size |
|-----------|------------|----------|----------|
| AES-128-GCM | 12 bytes | 16 bytes | 16 bytes |
| AES-192-GCM | 12 bytes | 16 bytes | 24 bytes |
| AES-256-GCM | 12 bytes | 16 bytes | 32 bytes |
| ChaCha20-Poly1305 | 12 bytes | 16 bytes | 32 bytes |
| XChaCha20-Poly1305 | 24 bytes | 16 bytes | 32 bytes |

## Storage Format

### Secret Entry Structure

```json
{
  "key_id": "quicbaas-secrets-key",
  "key_version": 1,
  "algorithm": "AES-128-GCM",
  "nonce": "<base64>",
  "ciphertext": "<base64>",
  "tag": "<base64>",
  "metadata": {"owner": "devops"},
  "version": 3,
  "deleted": false,
  "created_at": "2024-01-15T10:30:00Z"
}
```

### Versioning

Each secret maintains a full version history:

```
secrets/app/config
├── versions[0] → v1 (oldest)
├── versions[1] → v2
├── versions[2] → v3 (current)
└── metadata
    ├── deleted: false
    └── max_version: 3
```

## Security Model

### Defense in Depth

1. **Transport Security** - TLS/mTLS for all API calls
2. **Authentication** - Token-based auth via QuicBaaS IAM
3. **Authorization** - Path-based RBAC policies
4. **Encryption** - AES-GCM or ChaCha20-Poly1305
5. **Key Protection** - HSM or software key storage
6. **Audit Logging** - All operations logged

### Key Hierarchy

```
┌─────────────────────────────────────┐
│         Master Key (HSM/KMS)        │
│  Seals/Unseals the DEK              │
└──────────────────┬──────────────────┘
                   │
                   ▼
┌─────────────────────────────────────┐
│     Data Encryption Key (DEK)       │
│  Encrypts individual secrets        │
└──────────────────┬──────────────────┘
                   │
                   ▼
┌─────────────────────────────────────┐
│         Secret Data                 │
│  AES-GCM or ChaCha20-Poly1305       │
└─────────────────────────────────────┘
```

## Replication

### Raft Consensus

Secrets are replicated via QuicBaaS's Raft consensus layer:

- **Strong Consistency** - Linearizable reads and writes
- **Fault Tolerance** - Survives minority node failures
- **Automatic Recovery** - Nodes rejoin with snapshot + logs

### Cluster Sync

When a node joins or recovers:

1. Leader sends snapshot with all secrets
2. New node applies snapshot
3. Log replication continues normally

## Performance Considerations

### Encryption Overhead

| Operation | CPU Cost | Memory |
|-----------|----------|--------|
| AES-GCM Encrypt | ~1μs/KB (with AES-NI) | Minimal |
| ChaCha20 Encrypt | ~2μs/KB (constant time) | Minimal |
| Key Derivation | ~10μs | Minimal |

### Optimization Tips

- Use AES-GCM on Intel/AMD CPUs (AES-NI acceleration)
- Use ChaCha20 on ARM or when constant-time is critical
- Batch operations for high throughput
- Enable read replicas for read-heavy workloads
