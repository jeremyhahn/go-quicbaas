# Security

Enterprise-grade security for protecting data at rest and in transit, managing encryption keys, and securing cluster communications.

## Overview

QuicBaaS implements defense-in-depth with hierarchical key management, pluggable KMS backends, Shamir's Secret Sharing for master key protection, and mutual TLS for cluster communication.

## Components

| Component | Purpose | Status |
|-----------|---------|--------|
| [Authentication](authentication.md) | JWT-based authentication | Production |
| [RBAC](rbac.md) | Role-Based Access Control | Production |
| [KMS](kms.md) | Key management with pluggable backends | Production |
| [Cluster DEK](cluster-dek.md) | Cluster-wide data encryption keys | Production |
| [Audit Logging](audit.md) | Comprehensive audit trail with SIEM/Syslog integration | Production |
| [FIPS 140-3](fips.md) | Federal compliance mode | Production |
| [Seal/Unseal](seal.md) | Shamir's Secret Sharing for master key | Development |
| [Secrets](secrets.md) | Versioned secret storage | Development |
| [Crypto](crypto.md) | Encryption as a Service | Development |
| [Raft mTLS](raft-mtls.md) | Mutual TLS for cluster communication | Ready |
| [REST Security](rest-security.md) | HTTP security headers middleware | Production |
| [TLS Setup](tls-setup.md) | TLS configuration guide | Production |
| [Hardening Guide](hardening-guide.md) | Production hardening recommendations | Production |
| [Crypto Backends](../configuration/crypto.md) | `crypto:` backend selection per subsystem (KMS / CA / PIN / barrier AEAD) — operator reference | Production |
| [Crypto Backend Selection](crypto-backend-selection.md) | BackendResolver design, hardware-prefer policy, per-subsystem checklist | Production |

## Key Hierarchy

```
Master Key (per node)
    +-- Derived from Shamir shares during unseal
    +-- Held in memory, never persisted
        |
        +---> Wraps Cluster DEK
        |
        +---> Cluster DEK (cluster-wide)
                +-- Distributed via Raft
                +-- Wraps data keys
                    |
                    +---> Data Keys (per entity)
                            +-- Envelope encryption
```

## Hardware Security Integration

QuicBaaS supports hardware security modules for enhanced key protection:

| Backend | Description | Use Case |
|---------|-------------|----------|
| Software | Software-based key storage | Development, testing |
| TPM 2.0 | Trusted Platform Module integration | PCR-bound hardware sealing |
| PKCS#11 | HSM integration via PKCS#11 | Enterprise HSMs |
| AWS KMS | AWS Key Management Service | Cloud deployments |
| Azure Key Vault | Azure Key Vault integration | Azure deployments |
| HashiCorp Vault | Vault Transit backend | Multi-cloud |

## Quick Start

### Initialize Security

```bash
# Initialize seal
quicbaas seal init --shares 5 --threshold 3

# Unseal node
quicbaas seal unseal --share "share1..."
quicbaas seal unseal --share "share2..."
quicbaas seal unseal --share "share3..."

# Verify status
quicbaas seal status
```

### Manage Keys

```bash
# Generate encryption key
quicbaas kms generate --name mykey --algorithm AES-256-GCM

# Encrypt data
quicbaas kms encrypt mykey --data "sensitive"

# Rotate key
quicbaas kms rotate mykey
```

### Store Secrets

```bash
# Store secret
quicbaas secrets put database/prod --data password=secret123

# Retrieve secret
quicbaas secrets get database/prod
```

### Enable mTLS

```bash
# Start with Raft mTLS
quicbaas server start \
  --raft-mtls-enabled=true \
  --raft-cert=/path/to/raft.crt \
  --raft-key=/path/to/raft.key \
  --raft-ca=/path/to/ca.crt
```

### Configure Audit Logging

```bash
# Enable audit logging with syslog
quicbaas server start \
  --audit-enabled=true \
  --audit-syslog-enabled=true \
  --audit-syslog-address="syslog.example.com:514"
```

## Configuration

```yaml
security:
  # Authentication
  auth:
    enabled: true
    jwt_secret: ${JWT_SECRET}
    token_ttl: 1h

  # Authorization (RBAC)
  authorization:
    provider: "rbac"
    rbac:
      enabled: true
      allow_anonymous: true

  # Audit Logging
  audit:
    enabled: true
    file: "/var/log/quicbaas/audit.log"
    sign_logs: true
    sign_key: ${AUDIT_SIGN_KEY}
    compress: true
    compression_level: 6  # gzip level (1-9)
    max_size: 100         # MB before rotation
    max_backups: 10

    # Syslog Integration
    syslog_enabled: true
    syslog_network: "udp"      # udp, tcp, or unix
    syslog_address: "syslog.example.com:514"
    syslog_facility: 16        # LOCAL0 = 16
    syslog_tag: "quicbaas"

    # SIEM Integration
    siem:
      enabled: true
      endpoint: "https://siem.example.com/api/events"
      format: "cef"            # cef, leef, or json
      batch_size: 100
      flush_interval: "10s"
      headers:
        Authorization: "Bearer ${SIEM_TOKEN}"

  # FIPS Mode
  fips_mode: true

  # KMS backend
  kms:
    backend: software
    software:
      key_store_path: /var/lib/quicbaas/keys

  # Seal configuration
  seal:
    shares: 5
    threshold: 3

  # Raft mTLS
  raft:
    mtls:
      enabled: true
      cert_file: /path/to/raft.crt
      key_file: /path/to/raft.key
      ca_file: /path/to/ca.crt
```

## API Security

### gRPC Interceptors

QuicBaaS includes security interceptors for gRPC:

- **FIPS Validation**: Validates TLS version and cipher suites meet FIPS 140-3 requirements
- **Logging**: Comprehensive request/response logging
- **Metrics**: Prometheus metrics collection
- **Recovery**: Panic recovery with proper error handling
- **Request ID**: Request tracing and correlation

### REST Security Headers

The REST API includes security headers middleware:

| Header | Value | Purpose |
|--------|-------|---------|
| Strict-Transport-Security | max-age=31536000; includeSubDomains | Force HTTPS |
| X-Frame-Options | DENY | Prevent clickjacking |
| X-Content-Type-Options | nosniff | Prevent MIME sniffing |
| X-XSS-Protection | 1; mode=block | XSS filter |
| Referrer-Policy | strict-origin-when-cross-origin | Control referrer |
| Content-Security-Policy | default-src 'self' | Restrict content sources |
| Permissions-Policy | geolocation=(), microphone=(), camera=() | Disable browser features |

## See Also

- [Authentication](authentication.md) - JWT authentication
- [RBAC](rbac.md) - Role-Based Access Control
- [Audit Logging](audit.md) - Audit trail configuration
- [FIPS 140-3](fips.md) - Federal compliance mode
- [Cluster DEK](cluster-dek.md) - Cluster encryption keys
- [Seal/Unseal](seal.md) - Master key protection
- [Crypto](crypto.md) - Encryption as a Service
- [Raft mTLS](raft-mtls.md) - Cluster communication security
- [REST Security](rest-security.md) - HTTP security headers
- [TLS Setup](tls-setup.md) - TLS configuration guide
- [Hardening Guide](hardening-guide.md) - Production security hardening
- [Crypto Backends](../configuration/crypto.md) - Per-subsystem backend selection (auto / hardware-prefer)
- [Crypto Backend Selection](crypto-backend-selection.md) - BackendResolver design of record

## Extended Documentation

- [KMS Guide](../kms/README.md) - Key management service (architecture, API, CLI)
- [Secrets Guide](../secrets/README.md) - Secret storage (architecture, API, CLI)
