# Secrets — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/secrets/`.

Secure secrets management with versioning, encryption, and fine-grained access control.

## Overview

QuicBaaS Secrets Engine provides enterprise grade secrets management integrated directly into QuicBaaS. All secrets are encrypted at rest using go-xkms, with support for multiple encryption algorithms and hardware security modules.

## Key Features

| Feature | Description |
|---------|-------------|
| **Encryption at Rest** | All secrets encrypted via go-xkms (AES-GCM, ChaCha20-Poly1305) |
| **Versioning** | Full version history with rollback capability |
| **Soft Delete** | Recoverable deletion with permanent destroy option |
| **Key Rotation** | Re-encrypt secrets with new keys without downtime |
| **Multi-Algorithm** | AES-128/192/256-GCM, ChaCha20-Poly1305, XChaCha20-Poly1305 |
| **HSM Support** | Hardware security via TPM2, PKCS#11, or Cloud KMS |
| **RBAC** | Path-based access control integrated with QuicBaaS IAM |
| **Bulk Operations** | Retrieve multiple secrets by path prefix |
| **Real-time Monitoring** | Watch for secret changes via streaming API |

## Quick Start

### Store a Secret

```bash
# CLI
quicbaas secrets put app/config \
  --data '{"api_key":"sk-123","db_password":"secret123"}'

# REST API
curl -X POST http://localhost:8080/api/v1/secrets/app/config \
  -H 'Content-Type: application/json' \
  -d '{
    "data": {"api_key":"sk-123","db_password":"secret123"},
    "metadata": {"owner":"devops"}
  }'
```

### Retrieve a Secret

```bash
# CLI
quicbaas secrets get app/config

# REST API
curl http://localhost:8080/api/v1/secrets/app/config
```

### List Secrets

```bash
# CLI
quicbaas secrets list app/

# REST API
curl http://localhost:8080/api/v1/secrets?prefix=app/
```

### Delete a Secret

```bash
# Soft delete (recoverable)
quicbaas secrets delete app/config

# Permanent destroy
quicbaas secrets destroy app/config
```

### Get Secrets by Prefix

```bash
# CLI - Bulk retrieve all secrets under a path
quicbaas secrets get-prefix app/

# REST API
curl http://localhost:8080/api/v1/secrets/prefix/app/
```

### Watch for Changes

```bash
# CLI - Real-time monitoring (Ctrl+C to stop)
quicbaas secrets watch app/

# With timeout (e.g., 60 seconds)
quicbaas secrets watch app/ --timeout 60s
```

## Encryption Algorithms

| Algorithm | Key Size | Description |
|-----------|----------|-------------|
| AES-128-GCM | 128-bit | Default, fast, NIST approved |
| AES-192-GCM | 192-bit | Higher security margin |
| AES-256-GCM | 256-bit | Maximum security |
| ChaCha20-Poly1305 | 256-bit | Constant-time, no AES-NI required |
| XChaCha20-Poly1305 | 256-bit | Extended nonce (192-bit) |

### Specify Algorithm

```bash
# Use ChaCha20-Poly1305
quicbaas secrets put app/high-security \
  --algorithm chacha20-poly1305 \
  --data '{"secret":"value"}'
```

## Versioning

### Get Specific Version

```bash
quicbaas secrets get app/config --version 2
```

### List Versions

```bash
quicbaas secrets versions app/config
```

### Rollback to Previous Version

```bash
quicbaas secrets rollback app/config --version 2
```

## Key Rotation

### Rotate Encryption Key

```bash
# Re-encrypt with new key
quicbaas secrets rotate app/config --key-id new-key-2024
```

### Get Current Key Info

```bash
quicbaas secrets key-info app/config
```

## Configuration

```yaml
# quicbaas.yaml
secrets:
  enabled: true

  encryption:
    default_algorithm: AES-128-GCM
    default_key_id: quicbaas-secrets-key

  versioning:
    enabled: true
    max_versions: 10  # 0 = unlimited

  soft_delete:
    enabled: true
    retention_days: 30
```

## Security

### go-xkms Integration

All cryptographic operations are performed through go-xkms, supporting:

- **Software Backend** - PKCS#8 based key storage
- **TPM2** - Hardware-bound keys via TPM 2.0
- **PKCS#11** - HSM integration (YubiHSM, SafeNet, etc.)
- **Cloud KMS** - AWS KMS, Azure Key Vault, GCP Cloud KMS

### AEAD Safety Tracking

go-xkms automatically tracks nonce uniqueness and bytes encrypted per key to prevent cryptographic limits from being exceeded.

## Performance

| Operation | P50 | P99 | Notes |
|-----------|-----|-----|-------|
| Put | < 2ms | < 10ms | With encryption |
| Get | < 1ms | < 5ms | With decryption |
| List | < 3ms | < 15ms | 100 secrets |
| Get-Prefix | < 5ms | < 20ms | Bulk retrieval |
| Watch | N/A | N/A | Streaming, event-driven |
| Delete | < 1ms | < 5ms | Soft delete |

## Documentation

- [Architecture](architecture.md) - Encryption and storage design
- [Configuration](configuration.md) - Complete configuration reference
- [Config Resolution](config-resolution.md) - Secret resolution in config files
- [API Reference](api-reference.md) - REST and gRPC API documentation
- [CLI Usage](cli-usage.md) - Command-line interface guide

## See Also

- [KMS Documentation](../kms/README.md) - Key Management Service
- [Security Overview](../security/README.md) - Security architecture
