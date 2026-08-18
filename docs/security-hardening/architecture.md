# Security Hardening — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/security-hardening/`.

QuicBaaS implements defense-in-depth security with encryption, authentication, and compliance features.

## Overview

Security is built into every layer of QuicBaaS, from network encryption to field-level data protection.

**Security Features:**
- TLS 1.3 for data in crypto
- Field-level encryption for data at rest
- Integrated Key Management System (KMS)
- FIPS 140-3 compliance mode
- Authentication and authorization
- Audit logging
- Secrets management

## Encryption

### Data in Crypto

All network communication is encrypted using TLS 1.3.

**Configuration:**
```yaml
server:
  tls:
    enabled: true
    cert_file: /path/to/cert.pem
    key_file: /path/to/key.pem
    ca_file: /path/to/ca.pem  # For mTLS
    min_version: "1.3"
```

**Features:**
- Perfect Forward Secrecy (PFS)
- Strong cipher suites only
- Certificate validation
- mTLS for mutual authentication

**Supported Protocols:**
- gRPC over TLS
- HTTPS for REST API
- QUIC with built-in TLS 1.3

### Data at Rest

Field-level encryption protects sensitive data in storage.

**Architecture:**
```
┌──────────────────────────────────────┐
│         Application Data              │
│  ┌────────────────────────────────┐  │
│  │  Public Fields                 │  │
│  │  • ID: 12345                   │  │
│  │  • Type: "user"                │  │
│  └────────────────────────────────┘  │
│                                       │
│  ┌────────────────────────────────┐  │
│  │  Encrypted Fields              │  │
│  │  • SSN: [encrypted]            │  │
│  │  • CreditCard: [encrypted]     │  │
│  │  • Email: [encrypted]          │  │
│  └────────────────────────────────┘  │
└──────────────────────────────────────┘
           ↓
┌──────────────────────────────────────┐
│         Encryption Layer              │
│  • AES-128-GCM or AES-256-GCM        │
│  • Per-field encryption keys         │
│  • Automatic key rotation            │
└──────────────────────────────────────┘
           ↓
┌──────────────────────────────────────┐
│         PebbleDB Storage              │
│  • Binary encoding (MessagePack)     │
│  • Compressed with Snappy/Zstd       │
└──────────────────────────────────────┘
```

**Entity-Level Configuration:**
```go
type User struct {
    ID        uint64 `json:"id"`
    Email     string `json:"email" encrypt:"true"`
    SSN       string `json:"ssn" encrypt:"true"`
    CreditCard string `json:"credit_card" encrypt:"true"`
    Name      string `json:"name"` // Not encrypted
}
```

**Encryption Algorithms:**
- AES-128-GCM (default)
- AES-256-GCM (high security mode)
- ChaCha20-Poly1305 (alternative)

## Key Management System (KMS)

Integrated KMS manages encryption keys with automatic rotation and auditing.

### Architecture

```
┌─────────────────────────────────────────────┐
│          KMS Components                      │
│  ┌────────────────────────────────────────┐ │
│  │  Key Manager                            │ │
│  │  • DEK (Data Encryption Keys)          │ │
│  │  • KEK (Key Encryption Keys)           │ │
│  │  • Master Key                          │ │
│  └────────────────────────────────────────┘ │
│                                              │
│  ┌────────────────────────────────────────┐ │
│  │  Key Rotation Engine                   │ │
│  │  • Automatic rotation schedule         │ │
│  │  • Background re-encryption            │ │
│  │  • Version tracking                    │ │
│  └────────────────────────────────────────┘ │
│                                              │
│  ┌────────────────────────────────────────┐ │
│  │  Storage Backends                      │ │
│  │  • Raft-based distributed storage     │ │
│  │  • HashiCorp Vault integration        │ │
│  │  • AWS/GCP/Azure KMS integration      │ │
│  └────────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
```

### Key Hierarchy

```
Master Key (HSM or secure storage)
    ↓
Key Encryption Keys (KEK)
    ↓
Data Encryption Keys (DEK)
    ↓
Encrypted Application Data
```

### Configuration

```yaml
kms:
  enabled: true
  backend: "raft"  # or "vault"
  master_key_path: /secure/master.key
  rotation:
    enabled: true
    interval: 720h  # 30 days
  backends:
    vault:
      address: https://vault.example.com
      token: ${VAULT_TOKEN}
      mount: quicbaas/kms
```

### Operations

**Create Encryption Key:**
```go
key, err := kmsClient.CreateKey(ctx, &pb.CreateKeyRequest{
    Name: "user-data-key",
    Algorithm: pb.Algorithm_AES_256_GCM,
    RotationPeriod: 30 * 24 * time.Hour,
})
```

**Encrypt Data:**
```go
ciphertext, err := kmsClient.Encrypt(ctx, &pb.EncryptRequest{
    KeyName: "user-data-key",
    Plaintext: []byte("sensitive data"),
})
```

**Rotate Key:**
```go
err := kmsClient.RotateKey(ctx, &pb.RotateKeyRequest{
    KeyName: "user-data-key",
})
```

## FIPS 140-3 Compliance

QuicBaaS supports FIPS mode for government and regulated environments.

### FIPS Mode

When compiled with FIPS support:
- Uses BoringSSL (FIPS 140-3 validated)
- Enforces FIPS-approved algorithms
- Validates kernel FIPS mode
- Rejects non-compliant operations

**Build with FIPS:**
```bash
make build-fips
```

**Runtime Check:**
```go
if security.IsFIPSMode() {
    // Running in FIPS mode
    log.Info("FIPS 140-3 mode enabled")
}
```

**FIPS-Approved Algorithms:**
- AES-128-GCM, AES-256-GCM
- SHA-256, SHA-384, SHA-512
- RSA 2048/3072/4096
- ECDSA P-256/P-384
- TLS 1.3

## Authentication

### API Keys

Token-based authentication for programmatic access.

**Key Format:**
```
sk_live_<random>     # Production keys
sk_test_<random>     # Test environment keys
```

**Configuration:**
```yaml
security:
  api_keys:
    enabled: true
    key_file: /etc/quicbaas/api_keys.yaml
    rotation_days: 90
```

**API Keys File:**
```yaml
keys:
  - key: sk_live_abc123xyz
    name: production-app
    permissions:
      - kv:read
      - kv:write
    expires: 2025-12-31

  - key: sk_test_def456uvw
    name: test-environment
    permissions:
      - kv:*
    expires: never
```

**Usage:**
```bash
# REST API
curl -H "Authorization: Bearer sk_live_abc123xyz" \
  http://localhost:8080/api/v1/kv/user:123

# gRPC
md := metadata.New(map[string]string{
    "authorization": "Bearer sk_live_abc123xyz",
})
ctx := metadata.NewOutgoingContext(context.Background(), md)
```

### mTLS (Mutual TLS)

Certificate-based authentication for service-to-service communication.

**Server Configuration:**
```yaml
server:
  tls:
    enabled: true
    cert_file: /path/to/server.crt
    key_file: /path/to/server.key
    client_ca_file: /path/to/client-ca.crt
    require_client_cert: true
```

**Client Configuration:**
```go
// Load client certificate
cert, _ := tls.LoadX509KeyPair("client.crt", "client.key")

// Load CA cert
caCert, _ := os.ReadFile("ca.crt")
caCertPool := x509.NewCertPool()
caCertPool.AppendCertsFromPEM(caCert)

// Configure TLS
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    RootCAs:      caCertPool,
}

// Connect with mTLS
creds := credentials.NewTLS(tlsConfig)
conn, _ := grpc.Dial("localhost:50051",
    grpc.WithTransportCredentials(creds))
```

## Authorization

### Role-Based Access Control (RBAC)

RBAC provides fine-grained permissions for QuicBaaS operations. The IAM service (`pkg/service/iam_service.go`) implements user, role, and group management with the authorization framework defined in `pkg/interfaces/authz.go`.

**Roles:**
```yaml
roles:
  - name: admin
    permissions:
      - kv:*
      - cluster:*
      - vector:*

  - name: developer
    permissions:
      - kv:read
      - kv:write
      - vector:read
      - vector:write

  - name: read-only
    permissions:
      - kv:read
      - vector:read
```

**User Assignment:**
```yaml
users:
  - email: alice@example.com
    roles: [admin]

  - email: bob@example.com
    roles: [developer]
```

## Secrets Management

Secure storage and management of application secrets.

### Crypto Encryption

Encrypt/decrypt data without storing it.

**Use Cases:**
- Payment processing
- PII encryption
- Temporary data encryption

**API:**
```bash
# Encrypt data
quicbaas kms encrypt --key payment-key --plaintext "4111-1111-1111-1111"

# Decrypt data
quicbaas kms decrypt --key payment-key --ciphertext "..."
```

### Static Secrets

Store secrets with versioning and audit trail.

**API:**
```bash
# Store secret
quicbaas secrets put database/password --value "super-secret"

# Retrieve secret
quicbaas secrets get database/password

# List secret versions
quicbaas secrets versions database/password

# Rotate secret
quicbaas secrets rotate database/password --value "new-secret"
```

**Configuration:**
```yaml
secrets:
  enabled: true
  encryption_key: /secure/secrets-key
  versioning: true
  max_versions: 10
```

## Audit Logging

Track all security-relevant operations.

### Logged Events

```go
type AuditEvent struct {
    Timestamp   time.Time
    User        string
    Operation   string
    Resource    string
    Result      string  // success, failure
    IPAddress   string
    Details     map[string]interface{}
}
```

**Logged Operations:**
- Key creation/rotation
- Secret access
- Encryption/decryption
- Authentication attempts
- Permission changes
- Cluster configuration changes

### Configuration

```yaml
audit:
  enabled: true
  log_file: /var/log/quicbaas/audit.log
  log_level: info
  format: json
  retention_days: 365
```

### Example Log Entry

```json
{
  "timestamp": "2025-12-19T10:30:45Z",
  "user": "alice@example.com",
  "operation": "kms.encrypt",
  "resource": "user-data-key",
  "result": "success",
  "ip_address": "203.0.113.100",
  "details": {
    "key_version": 3,
    "algorithm": "AES-256-GCM"
  }
}
```

## Security Best Practices

### Network Security

1. **Always use TLS** - Enable TLS for all deployments
2. **Use mTLS for services** - Mutual authentication between services
3. **Firewall rules** - Restrict access to QuicBaaS ports
4. **Network segmentation** - Isolate database network

### Key Management

1. **Rotate keys regularly** - Enable automatic key rotation
2. **Secure master key** - Store master key in HSM or secure vault
3. **Separate DEKs** - Use different keys for different data types
4. **Backup keys securely** - Encrypted backups with offline storage

### Access Control

1. **Principle of least privilege** - Grant minimum required permissions
2. **Strong API keys** - Use cryptographically random keys
3. **Key expiration** - Set expiration dates for all keys
4. **Regular audits** - Review access logs periodically

### Compliance

1. **Enable audit logging** - Track all security events
2. **Data retention policies** - Define and enforce retention
3. **Encryption at rest** - Encrypt sensitive fields
4. **FIPS mode for regulated environments**

## Troubleshooting

### TLS Issues

**Problem:** Certificate validation failures

**Solution:**
```bash
# Check certificate
openssl x509 -in cert.pem -text -noout

# Verify certificate chain
openssl verify -CAfile ca.pem cert.pem

# Test TLS connection
openssl s_client -connect localhost:50051 -showcerts
```

### KMS Issues

**Problem:** Key not found

**Solution:**
```bash
# List all keys
quicbaas kms list-keys

# Check key status
quicbaas kms describe-key --name user-data-key
```

### FIPS Mode

**Problem:** Algorithm not FIPS-compliant

**Solution:**
- Check `/proc/sys/crypto/fips_enabled` (should be 1)
- Review algorithm configuration
- Rebuild with FIPS-compliant BoringSSL

## References

### Implementation
- **Security Package** - `pkg/security/`
- **KMS** - `pkg/kms/`
- **TLS Configuration** - `pkg/config/tls.go`
- **Audit Logging** - `pkg/audit/`

### Documentation
- **KMS Design** - `../features/KMS.md`
- **Secrets Management** - `../features/SECRETS.md`
- **Security Features** - `../features/SECURITY_FEATURES.md`

### Standards
- **FIPS 140-3** - [NIST Standard](https://csrc.nist.gov/publications/detail/fips/140/3/final)
- **TLS 1.3** - [RFC 8446](https://datatracker.ietf.org/doc/html/rfc8446)

---

**Last Updated:** 2025-12-19
**Version:** 0.3.0
