# Security Hardening — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/security-hardening/`.

Enterprise-grade security for protecting data at rest and in transit, managing encryption keys, and securing cluster communications.

## Overview

QuicBaaS implements defense-in-depth with hierarchical key management, pluggable KMS backends, Shamir's Secret Sharing for master key protection, and mutual TLS for cluster communication.

## Components

| Component | Purpose | Status |
|-----------|---------|--------|
| Authentication | JWT-based authentication | Production |
| RBAC | Role-Based Access Control | Production |
| KMS | Key management with pluggable backends | Production |
| Cluster DEK | Cluster-wide data encryption keys | Production |
| Audit Logging | Comprehensive audit trail with SIEM/Syslog integration | Production |
| FIPS 140-3 | Federal compliance mode | Production |
| Seal/Unseal | Shamir's Secret Sharing for master key | Development |
| Secrets | Versioned secret storage | Development |
| Crypto | Encryption as a Service | Development |
| Raft mTLS | Mutual TLS for cluster communication | Ready |
| REST Security | HTTP security headers middleware | Production |
| TLS Setup | TLS configuration guide | Production |
| Hardening Guide | Production hardening recommendations | Production |
| [Crypto Backends](../composition/engineering.md) | `crypto:` backend selection per subsystem (KMS / CA / PIN / barrier AEAD) — operator reference | Production |
| Crypto Backend Selection | BackendResolver design, hardware-prefer policy, per-subsystem checklist | Production |

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
# Initialize the barrier with 3-of-5 Shamir shares
quicbaas barrier init-shamir --total 5 --threshold 3

# Unseal by submitting shares (repeat --share or use --shares-file)
quicbaas barrier unseal-shares --share "share1..." --share "share2..." --share "share3..."

# Verify status
quicbaas barrier status
```

### Manage Keys

```bash
# Generate encryption key
quicbaas kms key generate mykey --algorithm AES-256-GCM

# Encrypt data
quicbaas kms encrypt mykey --data "sensitive"

# Rotate key
quicbaas kms key rotate mykey
```

### Store Secrets

```bash
# Store secret
quicbaas secrets put database/prod --data password=secret123

# Retrieve secret
quicbaas secrets get database/prod
```

### Enable mTLS

There are no TLS flags on `quicbaasd`; set Raft mTLS in the configuration file:

```yaml
raft:
  mutual_tls: true
  tls_cert_file: /path/to/raft.crt
  tls_key_file: /path/to/raft.key
  tls_ca_file: /path/to/ca.crt
```

```bash
quicbaasd node --config /etc/quicbaas/config.yaml
```

### Configure Audit Logging

Authorization decisions are audited through the configuration file. The
configuration has no syslog setting.

```yaml
security:
  authorization:
    audit:
      enabled: true
      log_denied: true
      log_allowed: false
```

## Configuration

```yaml
security:
  # Authentication (tokens are signed with the ES256 key auto-generated
  # in the xkms keystore at first boot; no JWT secret is configurable)
  auth:
    enabled: true
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
    sign_key: "kms://config/audit/sign-key"  # reference; >= 14 bytes (112 bits), FIPS 140-3 minimum
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

- Authentication - JWT authentication
- RBAC - Role-Based Access Control
- Audit Logging - Audit trail configuration
- FIPS 140-3 - Federal compliance mode
- Cluster DEK - Cluster encryption keys
- Seal/Unseal - Master key protection
- Crypto - Encryption as a Service
- Raft mTLS - Cluster communication security
- REST Security - HTTP security headers
- TLS Setup - TLS configuration guide
- Hardening Guide - Production security hardening
- [Crypto Backends](../composition/engineering.md) - Per-subsystem backend selection (auto / hardware-prefer)
- Crypto Backend Selection - BackendResolver design of record

## Extended Documentation

- [KMS Guide](../kms/user.md) - Key management service (architecture, API, CLI)
- [Secrets Guide](../secrets/user.md) - Secret storage (architecture, API, CLI)
