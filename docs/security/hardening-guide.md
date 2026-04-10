# Security Hardening Guide

This guide provides actionable recommendations for hardening QuicBaaS deployments in production environments. Follow these guidelines to minimize attack surface, enforce defense-in-depth, and meet compliance requirements.

## Table of Contents

1. [Network Security](#network-security)
2. [Authentication Hardening](#authentication-hardening)
3. [Authorization Hardening](#authorization-hardening)
4. [Encryption](#encryption)
5. [Audit Logging](#audit-logging)
6. [FIPS 140-3 Compliance](#fips-140-3-compliance)
7. [Operational Security](#operational-security)

---

## Network Security

### Binding to Specific Interfaces

By default, QuicBaaS binds only to Unix sockets for maximum security. Explicitly control network exposure:

```yaml
# .quicbaas.yaml - Minimal network exposure (recommended)
server:
  host: "127.0.0.1"           # Bind to localhost only
  port: 8080                   # REST API port
  grpc_port: 9090              # gRPC port
  unix_socket: "/var/run/quicbaas/quicbaas.sock"  # Default transport

  # Explicitly enable only required transports
  enable_tcp: false            # Disable TCP by default
  enable_rest: false           # Disable REST by default
  enable_quic: false           # Disable QUIC by default
  enable_mcp: false            # Disable MCP by default
```

**Production configuration with controlled TCP exposure:**

```yaml
server:
  host: "10.0.1.100"          # Bind to internal network interface only
  port: 8080
  grpc_port: 9090
  unix_socket: "/var/run/quicbaas/quicbaas.sock"
  enable_tcp: true             # Enable TCP for authorized clients
  enable_rest: true            # Enable REST API
  max_connections: 1000        # Limit concurrent connections
```

**For Kubernetes deployments:**

```yaml
server:
  host: "0.0.0.0"              # Required for pod networking
  port: 8080
  grpc_port: 9090
  enable_tcp: true
  enable_rest: true
```

### Firewall Configuration

**Linux iptables rules:**

```bash
# Allow internal network access to QuicBaaS ports
iptables -A INPUT -p tcp -s 10.0.0.0/8 --dport 8080 -j ACCEPT
iptables -A INPUT -p tcp -s 10.0.0.0/8 --dport 9090 -j ACCEPT

# Allow Raft cluster communication between nodes
iptables -A INPUT -p tcp -s 10.0.1.0/24 --dport 63000 -j ACCEPT

# Drop all other traffic to QuicBaaS ports
iptables -A INPUT -p tcp --dport 8080 -j DROP
iptables -A INPUT -p tcp --dport 9090 -j DROP
iptables -A INPUT -p tcp --dport 63000 -j DROP
```

**UFW (Ubuntu Firewall):**

```bash
# Allow from specific subnets
ufw allow from 10.0.1.0/24 to any port 8080 proto tcp
ufw allow from 10.0.1.0/24 to any port 9090 proto tcp
ufw allow from 10.0.1.0/24 to any port 63000 proto tcp

# Deny from everywhere else
ufw deny 8080/tcp
ufw deny 9090/tcp
ufw deny 63000/tcp
```

**Kubernetes NetworkPolicy:**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: quicbaas-network-policy
  namespace: quicbaas
spec:
  podSelector:
    matchLabels:
      app: quicbaas
  policyTypes:
    - Ingress
    - Egress
  ingress:
    # Allow client connections from app namespace
    - from:
        - namespaceSelector:
            matchLabels:
              name: production
        - podSelector:
            matchLabels:
              access: quicbaas
      ports:
        - protocol: TCP
          port: 8080
        - protocol: TCP
          port: 9090
    # Allow Raft cluster communication
    - from:
        - podSelector:
            matchLabels:
              app: quicbaas
      ports:
        - protocol: TCP
          port: 63000
  egress:
    # Allow Raft cluster communication
    - to:
        - podSelector:
            matchLabels:
              app: quicbaas
      ports:
        - protocol: TCP
          port: 63000
    # Allow DNS
    - to:
        - namespaceSelector: {}
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
```

### Unix Socket vs TCP

Unix sockets provide stronger security than TCP for local communication:

| Feature | Unix Socket | TCP |
|---------|-------------|-----|
| Network exposure | None | Yes |
| File permissions | Yes | No |
| SELinux/AppArmor | Yes | Limited |
| Performance | Higher | Lower |
| Remote access | No | Yes |
| Peer credentials | Yes (SO_PEERCRED) | No |

**Unix socket with restrictive permissions:**

```yaml
server:
  unix_socket: "/var/run/quicbaas/quicbaas.sock"
  enable_tcp: false
```

```bash
# Set socket permissions (owner and group only)
chmod 660 /var/run/quicbaas/quicbaas.sock
chown quicbaas:quicbaas /var/run/quicbaas/quicbaas.sock

# Restrict directory access
chmod 750 /var/run/quicbaas
```

**SELinux policy for Unix socket:**

```bash
# Allow QuicBaaS to create and bind to Unix socket
semanage fcontext -a -t quicbaas_var_run_t "/var/run/quicbaas(/.*)?"
restorecon -Rv /var/run/quicbaas
```

### gRPC Peer Credentials (SO_PEERCRED)

QuicBaaS provides kernel-verified authentication for Unix socket connections using `SO_PEERCRED`. This extracts the UID, GID, and PID of the connecting process directly from the kernel, making it impossible to spoof.

**Benefits:**
- **Zero-trust local auth**: Verify client identity without passwords or tokens
- **Kernel-verified**: Credentials come from the OS, not the application
- **No spoofing**: Client cannot fake UID/GID/PID
- **Audit trail**: Log which processes access the database

**Server configuration with peer credential verification:**

```go
import grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"

// Only allow specific users to connect
verifier := grpcapi.NewUIDVerifier(
    1000, // quicbaas service account
    1001, // app service account
)

// Or use group-based verification
verifier := grpcapi.NewGIDVerifier(
    100, // quicbaas group
)

// Or allow only root
verifier := &grpcapi.RootOnlyVerifier{}

// Chain multiple verifiers (all must pass)
verifier := grpcapi.NewChainVerifier(
    grpcapi.NewUIDVerifier(1000, 1001),
    grpcapi.NewGIDVerifier(100),
)
```

**Credential types:**

| Type | Description |
|------|-------------|
| `PeerCredentials` | Struct containing PID, UID, GID |
| `PeerCredentialsAuthInfo` | gRPC AuthInfo implementation |
| `UnixTransportCredentials` | gRPC TransportCredentials for Unix sockets |

**Verifier types:**

| Verifier | Use Case |
|----------|----------|
| `AllowAllVerifier` | Testing/development |
| `RootOnlyVerifier` | System services only |
| `UIDVerifier` | Allow specific user IDs |
| `GIDVerifier` | Allow specific group IDs |
| `ChainVerifier` | Combine multiple verifiers |

**Accessing credentials in handlers:**

```go
creds := grpcapi.PeerCredentialsFromContext(ctx)
if creds != nil {
    log.Printf("Request from UID=%d, GID=%d, PID=%d",
        creds.UID, creds.GID, creds.PID)
}
```

**Platform notes:**
- Linux: Full SO_PEERCRED support
- macOS/Windows: Stub returns `PeerCredentialsUnavailableError`

---

## Authentication Hardening

### Disabling Anonymous Access

Disable anonymous access for all non-health endpoints:

```yaml
security:
  authorization:
    rbac:
      enabled: true
      allow_anonymous: false    # Disable anonymous access
      default_role: ""          # No default role (deny unauthenticated)
```

**CLI verification:**

```bash
# This should return 401 Unauthorized
curl -X GET http://localhost:8080/api/v1/keys/test
# {"error":"authentication required"}
```

### Token Expiration Settings

Configure short-lived access tokens with longer refresh tokens:

```yaml
security:
  auth:
    enabled: true
    jwt_secret: "${JWT_SECRET}"     # 32+ character secret from environment
    token_ttl: 15m                   # Short-lived access tokens (15 minutes)
    refresh_token_ttl: 24h           # Refresh tokens valid for 24 hours
    max_refresh_count: 48            # Maximum refreshes before re-authentication
```

**Programmatic token configuration:**

```go
authConfig := &auth.AuthConfig{
    Enabled:         true,
    JWTSecret:       os.Getenv("JWT_SECRET"),
    TokenTTL:        15 * time.Minute,      // Access token
    RefreshTokenTTL: 24 * time.Hour,        // Refresh token
}
```

**Token validation best practices:**

```go
// Validate token on every request
token, err := authManager.ValidateToken(ctx, accessToken)
if err != nil {
    // Handle expired or invalid token
    return nil, errors.ErrTokenExpired
}

// Check token claims
if token.ExpiresAt.Before(time.Now()) {
    return nil, errors.ErrTokenExpired
}
```

### WebAuthn/FIDO2 Setup

QuicBaaS supports WebAuthn/FIDO2 for passwordless authentication via the IAM module:

```yaml
# Enable IAM with WebAuthn support
# Build with: go build -tags iam ./cmd/quicbaasd

security:
  iam:
    enabled: true
    webauthn:
      enabled: true
      rp_display_name: "QuicBaaS"
      rp_id: "quicbaas.example.com"
      rp_origins:
        - "https://quicbaas.example.com"
      attestation_preference: "direct"        # Request attestation
      authenticator_selection:
        authenticator_attachment: "platform"  # Platform authenticator preferred
        resident_key: "required"              # Discoverable credentials
        user_verification: "required"         # Require biometric/PIN
```

**WebAuthn registration flow:**

```go
// Begin registration
options, session, err := webauthn.BeginRegistration(user)

// Client performs registration ceremony
// ...

// Complete registration
credential, err := webauthn.FinishRegistration(user, session, response)
```

### MFA Enforcement

Enforce multi-factor authentication for all users or specific roles:

```yaml
security:
  iam:
    mfa:
      enabled: true
      required: true                    # Require MFA for all users
      methods:
        - totp                          # Time-based OTP (Google Authenticator)
        - webauthn                       # Security keys
      totp:
        issuer: "QuicBaaS"
        digits: 6
        period: 30
      grace_period: 7d                  # Allow 7 days to enroll MFA
```

**Per-tenant MFA enforcement:**

```yaml
tenant:
  settings:
    mfa_required: true                   # Enforce MFA for this tenant
    allowed_mfa_methods:
      - totp
      - webauthn
```

**CLI MFA management:**

```bash
# Enroll user in MFA
quicbaas iam mfa enroll --tenant production --user alice --method totp

# Complete enrollment with verification code
quicbaas iam mfa verify --enrollment-id enr-xxx --code 123456

# Check MFA status
quicbaas iam mfa status --tenant production --user alice
```

---

## Authorization Hardening

### Least Privilege Principles

Follow the principle of least privilege when assigning roles:

```yaml
# Create minimal roles for specific tasks
security:
  authorization:
    provider: "rbac"
    rbac:
      enabled: true
      default_role: ""            # No default permissions
      allow_anonymous: false      # No anonymous access
```

**Application-specific role example:**

```go
// Read-only application role
appRole := &auth.Role{
    Name:        "app-readonly",
    Description: "Read-only access for application service",
    Permissions: []*auth.Permission{
        {Resource: "keys", Actions: []string{"read"}},
        {Resource: "health", Actions: []string{"read"}},
    },
}

// Write-only ingest role
ingestRole := &auth.Role{
    Name:        "data-ingest",
    Description: "Write access for data ingestion service",
    Permissions: []*auth.Permission{
        {Resource: "keys", Actions: []string{"write"}},
    },
}
```

### RBAC Role Design

Design roles based on job functions, not individual users:

```go
// Built-in roles
// admin:     Full administrative access (use sparingly)
// readwrite: Application data access
// readonly:  Read-only monitoring access

// Custom roles for specific functions
roles := map[string]*auth.Role{
    "monitoring": {
        Name:        "monitoring",
        Description: "Monitoring and observability access",
        Permissions: []*auth.Permission{
            {Resource: "metrics", Actions: []string{"read"}},
            {Resource: "health", Actions: []string{"read"}},
        },
    },
    "backup-operator": {
        Name:        "backup-operator",
        Description: "Backup and restore operations",
        Permissions: []*auth.Permission{
            {Resource: "backups", Actions: []string{"read", "write"}},
            {Resource: "keys", Actions: []string{"read"}},
        },
    },
    "security-auditor": {
        Name:        "security-auditor",
        Description: "Security audit access",
        Permissions: []*auth.Permission{
            {Resource: "users", Actions: []string{"read"}},
            {Resource: "roles", Actions: []string{"read"}},
            {Resource: "metrics", Actions: []string{"read"}},
        },
    },
    "key-manager": {
        Name:        "key-manager",
        Description: "KMS key management",
        Permissions: []*auth.Permission{
            {Resource: "kms", Actions: []string{"read", "write"}},
            {Resource: "crypto", Actions: []string{"read", "write"}},
        },
    },
}
```

### ABAC Policy Examples

For complex authorization requirements, use Attribute-Based Access Control:

```yaml
# Build with: go build -tags iam ./cmd/quicbaasd

security:
  authorization:
    provider: "abac"
    abac:
      enabled: true
      policy_dir: "/etc/quicbaas/policies"
      cache_ttl: "5m"
```

**Time-based access policy:**

```yaml
# /etc/quicbaas/policies/time-based-access.yaml
apiVersion: quicbaas.io/v1
kind: Policy
metadata:
  name: business-hours-only
  tenant: production
spec:
  description: "Allow write access only during business hours"
  subjects:
    roles:
      - readwrite
  resources:
    - keys
  actions:
    - write
    - delete
  conditions:
    - type: time_range
      start_hour: 9
      end_hour: 17
      timezone: "America/New_York"
      days: [monday, tuesday, wednesday, thursday, friday]
  effect: allow
```

**IP-based access policy:**

```yaml
# /etc/quicbaas/policies/ip-restriction.yaml
apiVersion: quicbaas.io/v1
kind: Policy
metadata:
  name: internal-network-only
  tenant: production
spec:
  description: "Restrict admin access to internal network"
  subjects:
    roles:
      - admin
  resources:
    - "*"
  actions:
    - "*"
  conditions:
    - type: ip_range
      cidr:
        - "10.0.0.0/8"
        - "172.16.0.0/12"
        - "192.168.0.0/16"
  effect: allow
```

**Resource ownership policy:**

```yaml
# /etc/quicbaas/policies/resource-ownership.yaml
apiVersion: quicbaas.io/v1
kind: Policy
metadata:
  name: owner-access
  tenant: production
spec:
  description: "Users can only access their own resources"
  subjects:
    match: all
  resources:
    - keys
    - secrets
  actions:
    - read
    - write
    - delete
  conditions:
    - type: attribute_match
      subject_attr: "id"
      resource_attr: "owner"
  effect: allow
```

**Multi-tenant isolation policy:**

```yaml
# /etc/quicbaas/policies/tenant-isolation.yaml
apiVersion: quicbaas.io/v1
kind: Policy
metadata:
  name: tenant-isolation
spec:
  description: "Enforce tenant data isolation"
  subjects:
    match: all
  resources:
    - keys
    - secrets
    - kms
  actions:
    - "*"
  conditions:
    - type: attribute_match
      subject_attr: "tenant"
      resource_attr: "tenant"
  effect: allow
  priority: 1000  # High priority
```

---

## Encryption

### Data at Rest Encryption (KMS)

Enable encryption at rest using the Key Management Service:

```yaml
security:
  encryption_enabled: true
  encryption_algorithm: "aes256"

kms:
  enabled: true
  backend: software                  # Or: awskms, gcpkms, azurekv, vault
  auto_generate_key: true            # Generate default DEK on startup
  cache:
    enabled: true

  # Software backend configuration
  software:
    key_store_path: /var/lib/quicbaas/keys
```

**Cloud KMS backend (AWS):**

```yaml
kms:
  enabled: true
  backend: awskms

  aws:
    region: us-east-1
    key_id: "alias/quicbaas-master"    # KMS key alias or ARN
    # Credentials from environment or IAM role
```

**Cloud KMS backend (GCP):**

```yaml
kms:
  enabled: true
  backend: gcpkms

  gcp:
    project_id: my-project
    location: global
    key_ring: quicbaas
    key_name: master-key
```

**HashiCorp Vault backend:**

```yaml
kms:
  enabled: true
  backend: vault

  vault:
    address: "https://vault.example.com:8200"
    token: "${VAULT_TOKEN}"             # From environment
    mount_path: transit
    key_name: quicbaas-master
    tls_skip_verify: false
```

### Key Rotation Procedures

**Automatic key rotation:**

```yaml
kms:
  rotation:
    enabled: true
    interval: 90d                       # Rotate every 90 days
    retain_versions: 3                  # Keep 3 old versions
```

**Manual key rotation:**

```bash
# Rotate a specific key
quicbaas kms rotate my-encryption-key

# Verify new key version
quicbaas kms get my-encryption-key
# Output: Version: 2, Status: Active, Created: 2025-01-07T10:00:00Z

# List key versions
quicbaas kms versions my-encryption-key
```

**Re-encryption after rotation:**

```bash
# Re-wrap existing data with new key version
quicbaas crypto rewrap my-encryption-key --all-data

# Or programmatically
rewrapResp, err := cryptoService.Rewrap(ctx, &service.RewrapRequest{
    KeyID:      "my-encryption-key",
    Ciphertext: existingCiphertext,
})
```

### TPM/HSM Integration

For hardware-backed key protection:

```yaml
# PKCS#11 HSM integration (requires pkcs11 build tag)
# Build with: go build -tags pkcs11 ./cmd/quicbaasd

kms:
  enabled: true
  backend: pkcs11

  pkcs11:
    library: "/usr/lib/softhsm/libsofthsm2.so"
    slot: 0
    pin: "${HSM_PIN}"                   # From environment
    key_label: "quicbaas-master"
```

**TPM 2.0 integration:**

QuicBaaS supports TPM 2.0 for PCR-bound hardware sealing through the go-xkms library:

```yaml
kms:
  enabled: true
  backend: tpm2

  tpm2:
    device: "/dev/tpmrm0"
    hierarchy: "owner"
    key_handle: 0x81000001
```

---

## Audit Logging

### Enabling Comprehensive Audit Logs

Enable full audit logging for compliance:

```yaml
security:
  audit:
    enabled: true
    file: "/var/log/quicbaas/audit.log"
    max_size: 100                       # MB before rotation
    max_backups: 10                     # Number of backup files
    buffer_size: 1024                   # Event buffer size
    flush_interval: "5s"                # Auto-flush interval

  authorization:
    audit:
      enabled: true
      log_denied: true                  # Always log denied requests
      log_allowed: true                 # Log allowed requests (verbose)
```

**IAM audit configuration:**

```yaml
# When using IAM module
iam:
  audit:
    enabled: true
    async_buffer_size: 5000
    retention_days: 90
    log_auth_events: true
    log_admin_events: true
    log_policy_checks: true
```

### Log Compression

Enable gzip compression for rotated audit logs to save storage:

```yaml
security:
  audit:
    enabled: true
    file: "/var/log/quicbaas/audit.log"
    compress: true
    compression_level: 6               # 1-9, or -1 for default, -2 for Huffman only
```

Compression levels:

| Level | Description | Use Case |
|-------|-------------|----------|
| -2 | Huffman only | Fastest, minimal compression |
| -1 | Default | Balanced speed/compression |
| 1 | Best speed | Fast compression |
| 6 | Default value | Good balance |
| 9 | Best compression | Maximum compression, slower |

### Native Syslog Integration

QuicBaaS includes native syslog integration supporting local and remote syslog with RFC 5424 compliance:

**Local syslog:**

```yaml
security:
  audit:
    enabled: true
    syslog_enabled: true
    syslog_network: ""                  # Empty for local syslog
    syslog_facility: 16                 # LOCAL0 = 16
    syslog_tag: "quicbaas"
```

**Remote syslog (UDP):**

```yaml
security:
  audit:
    enabled: true
    syslog_enabled: true
    syslog_network: "udp"
    syslog_address: "syslog.example.com:514"
    syslog_facility: 16                 # LOCAL0
    syslog_tag: "quicbaas"
    syslog_hostname: "quicbaas-node-1"  # Optional
```

**Remote syslog (TCP with TLS):**

```yaml
security:
  audit:
    enabled: true
    syslog_enabled: true
    syslog_network: "tcp"
    syslog_address: "syslog.example.com:6514"
    syslog_facility: 16
    syslog_tag: "quicbaas"
```

Syslog facilities:

| Value | Facility | Description |
|-------|----------|-------------|
| 4 | auth | Security/auth messages |
| 10 | authpriv | Security/auth (private) |
| 16 | local0 | Local use 0 (default) |
| 17-23 | local1-7 | Local use 1-7 |

### Native SIEM Integration

QuicBaaS supports direct SIEM integration via HTTP with CEF, LEEF, and JSON formats:

**CEF format (ArcSight):**

```yaml
security:
  audit:
    enabled: true
    siem:
      enabled: true
      endpoint: "https://arcsight.example.com/api/events"
      format: "cef"
      batch_size: 100
      flush_interval: "10s"
      timeout: "30s"
      max_retries: 3
      cef_vendor: "QuicBaaS"
      cef_product: "QuicBaaS"
      cef_version: "1.0"
      headers:
        Authorization: "Bearer ${SIEM_TOKEN}"
```

**LEEF format (QRadar):**

```yaml
security:
  audit:
    enabled: true
    siem:
      enabled: true
      endpoint: "https://qradar.example.com/api/events"
      format: "leef"
      leef_vendor: "QuicBaaS"
      leef_product: "QuicBaaS"
      leef_version: "1.0"
```

**JSON format (Splunk, Elasticsearch):**

```yaml
security:
  audit:
    enabled: true
    siem:
      enabled: true
      endpoint: "https://splunk.example.com:8088/services/collector"
      format: "json"
      batch_size: 100
      flush_interval: "10s"
      headers:
        Authorization: "Splunk ${SPLUNK_HEC_TOKEN}"
```

### External Log Forwarding

**Systemd journal integration:**

```bash
# Configure systemd service for journal logging
[Service]
StandardOutput=journal
StandardError=journal
SyslogIdentifier=quicbaas
```

**Filebeat for Elasticsearch:**

```yaml
# Filebeat configuration for Elasticsearch
filebeat.inputs:
  - type: log
    enabled: true
    paths:
      - /var/log/quicbaas/audit.log
    json.keys_under_root: true
    json.add_error_key: true

output.elasticsearch:
  hosts: ["elasticsearch.example.com:9200"]
  index: "quicbaas-audit-%{+yyyy.MM.dd}"
  username: "elastic"
  password: "${ES_PASSWORD}"
```

**Vector for Splunk:**

```toml
[sources.quicbaas_logs]
type = "file"
include = ["/var/log/quicbaas/*.log"]
read_from = "beginning"

[transforms.parse_json]
type = "remap"
inputs = ["quicbaas_logs"]
source = '''
. = parse_json!(.message)
'''

[sinks.splunk]
type = "splunk_hec"
inputs = ["parse_json"]
endpoint = "https://splunk.example.com:8088"
token = "${SPLUNK_HEC_TOKEN}"
compression = "gzip"
```

### Log Tamper Protection (HMAC Signatures)

Enable cryptographic signatures for audit log integrity:

```yaml
security:
  audit:
    enabled: true
    sign_logs: true
    sign_key: "${AUDIT_SIGN_KEY}"      # HMAC-SHA256 key from environment
```

**Audit log entry format with HMAC:**

```json
{
  "event_id": "evt-1704634800000000000",
  "timestamp": "2025-01-07T10:30:00.000Z",
  "action": "write",
  "resource": "keys",
  "result": "denied",
  "user_id": "user-123",
  "username": "alice",
  "ip_address": "10.0.1.50",
  "error_msg": "insufficient permissions",
  "signature": "a1b2c3d4e5f6..."
}
```

**Verify audit log integrity:**

```bash
# Verify audit log chain
quicbaas audit verify --file /var/log/quicbaas/audit.log

# Output:
# Verifying audit log integrity...
# Entries checked: 10,234
# Chain valid: true
# First entry: 2025-01-01T00:00:00Z
# Last entry: 2025-01-07T10:30:00Z
# Status: VERIFIED
```

---

## FIPS 140-3 Compliance

### Enabling FIPS Mode

QuicBaaS leverages Go 1.24+'s native FIPS 140-3 support. Algorithm validation is handled automatically by the Go runtime.

**Build with FIPS support:**

```bash
# Build FIPS-compliant binary with Go 1.24+
make build-fips

# Or manually:
GOFIPS140=v1.0.0 go build -o bin/quicbaas-fips ./cmd/quicbaas
```

**Runtime configuration:**

```yaml
security:
  fips_mode: true
  encryption_algorithm: "aes256"        # FIPS-approved algorithm

  # TLS configuration for FIPS
  tls_enabled: true
  tls_min_version: "1.2"                # TLS 1.2 minimum for FIPS
  tls_cipher_suites:
    - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
    - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256

kms:
  enabled: true
  # Only FIPS-approved algorithms available in FIPS mode
```

**Enable kernel FIPS mode:**

```bash
# Ubuntu/Debian
sudo apt-get install fips-initramfs
sudo update-initramfs -u
# Add fips=1 to GRUB_CMDLINE_LINUX in /etc/default/grub
sudo update-grub
sudo reboot

# RHEL/CentOS
sudo yum install dracut-fips
sudo dracut -f
sudo grubby --update-kernel=ALL --args="fips=1"
sudo reboot

# Verify FIPS mode
cat /proc/sys/crypto/fips_enabled
# Output: 1
```

### gRPC FIPS Validation

QuicBaaS includes gRPC interceptors that validate FIPS compliance for every request:

- TLS version must be 1.3 or higher (TLS 1.2 allowed with configuration)
- Cipher suite must be FIPS-approved
- Non-compliant connections are rejected with FailedPrecondition error

```go
// Enable FIPS interceptor in gRPC server
unaryInterceptor := grpc.InterceptorChain(
    grpc.UnaryRecoveryInterceptor(logger),
    grpc.UnaryFIPSInterceptor(true, logger),  // FIPS validation enabled
)
```

### Approved Algorithms

When FIPS mode is enabled, the Go runtime automatically enforces FIPS-approved algorithms. Non-approved operations will fail.

| Category | Algorithm | Key Size | Notes |
|----------|-----------|----------|-------|
| Symmetric Encryption | AES-128-GCM | 128-bit | Authenticated encryption |
| Symmetric Encryption | AES-256-GCM | 256-bit | Recommended |
| Hash | SHA-256 | 256-bit | Standard hash |
| Hash | SHA-384 | 384-bit | Extended security |
| Hash | SHA-512 | 512-bit | Maximum security |
| Digital Signature | ECDSA P-256 | 256-bit | secp256r1 |
| Digital Signature | ECDSA P-384 | 384-bit | secp384r1 |
| Digital Signature | RSA-2048 | 2048-bit | Minimum for FIPS |
| Digital Signature | RSA-3072 | 3072-bit | Recommended |
| Key Agreement | ECDH P-256 | 256-bit | Elliptic curve DH |
| Key Agreement | ECDH P-384 | 384-bit | Elliptic curve DH |
| Key Derivation | HKDF | N/A | HMAC-based KDF |
| Key Derivation | PBKDF2 | N/A | Password-based KDF |
| MAC | HMAC-SHA-256 | 256-bit | Message authentication |
| MAC | HMAC-SHA-512 | 512-bit | Extended MAC |

**Check FIPS status programmatically:**

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/fips"

// Check if FIPS mode is enabled
if fips.Enabled() {
    fmt.Println("Running in FIPS 140-3 compliant mode")
}

// Get detailed status
status := fips.GetStatus()
fmt.Printf("FIPS Enabled: %v\n", status.Enabled)
fmt.Printf("Kernel FIPS Mode: %v\n", status.KernelFIPSMode)
fmt.Printf("Kernel Cmdline: %v\n", status.KernelCmdline)
```

### Validation Requirements

**Pre-deployment checklist:**

```bash
# 1. Verify Go version (1.24+)
go version

# 2. Verify kernel FIPS mode
cat /proc/sys/crypto/fips_enabled
# Output: 1

# 3. Check kernel cmdline
cat /proc/cmdline | grep fips
# Should contain fips=1

# 4. Verify TLS configuration
openssl s_client -connect localhost:8080 -tls1_2 < /dev/null
# Should connect successfully with FIPS cipher
```

---

## Operational Security

### Secrets Management

**Never store secrets in configuration files:**

```yaml
# BAD - Secret in config file
security:
  auth:
    jwt_secret: "my-super-secret-key"    # NEVER do this!

# GOOD - Secret from environment
security:
  auth:
    jwt_secret: "${JWT_SECRET}"           # From environment variable
```

**Environment variable management:**

```bash
# Set secrets via environment (not in shell history)
read -s JWT_SECRET && export JWT_SECRET

# Or use a secrets manager
export JWT_SECRET=$(vault kv get -field=jwt_secret secret/quicbaas)
export KMS_MASTER_KEY=$(aws secretsmanager get-secret-value --secret-id quicbaas/kms --query SecretString --output text)
```

**Kubernetes secrets:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: quicbaas-secrets
  namespace: quicbaas
type: Opaque
data:
  jwt-secret: <base64-encoded-secret>
  kms-master-key: <base64-encoded-key>
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: quicbaas
spec:
  template:
    spec:
      containers:
        - name: quicbaas
          env:
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: quicbaas-secrets
                  key: jwt-secret
```

**HashiCorp Vault integration:**

```bash
# Store secrets in Vault
vault kv put secret/quicbaas \
  jwt_secret="$(openssl rand -base64 32)" \
  kms_master_key="$(openssl rand -base64 32)"

# Retrieve at runtime
export JWT_SECRET=$(vault kv get -field=jwt_secret secret/quicbaas)
```

### Backup Encryption

Always encrypt backups:

```yaml
backup:
  enabled: true
  storage_backend: "s3"
  default_compression: "zstd"
  default_encryption: "aes-256-gcm"
  encryption_key: "${BACKUP_ENCRYPTION_KEY}"   # From environment
  work_dir: "/tmp/quicbaas-backup"
  timeout: "30m"

object_storage:
  backends:
    s3:
      type: "s3"
      config:
        bucket: "quicbaas-backups"
        region: "us-east-1"
        server_side_encryption: "aws:kms"
        kms_key_id: "alias/quicbaas-backup"
```

**CLI backup with encryption:**

```bash
# Create encrypted backup
quicbaas backup create \
  --compression zstd \
  --encryption aes-256-gcm \
  --key-id backup-encryption-key \
  --output s3://quicbaas-backups/backup-$(date +%Y%m%d).enc

# Restore encrypted backup
quicbaas backup restore \
  --key-id backup-encryption-key \
  --input s3://quicbaas-backups/backup-20250107.enc
```

### Secure Configuration Storage

**File permissions:**

```bash
# Configuration file permissions
chmod 600 /etc/quicbaas/.quicbaas.yaml
chown quicbaas:quicbaas /etc/quicbaas/.quicbaas.yaml

# Data directory permissions
chmod 700 /var/lib/quicbaas
chown -R quicbaas:quicbaas /var/lib/quicbaas

# Key storage permissions
chmod 700 /var/lib/quicbaas/keys
chmod 600 /var/lib/quicbaas/keys/*

# Log directory permissions
chmod 750 /var/log/quicbaas
chown quicbaas:adm /var/log/quicbaas
```

**AppArmor profile:**

```
#include <tunables/global>

profile quicbaas /usr/bin/quicbaas {
  #include <abstractions/base>
  #include <abstractions/nameservice>

  # Binary
  /usr/bin/quicbaas mr,

  # Configuration
  /etc/quicbaas/** r,
  /etc/quicbaas/.quicbaas.yaml r,

  # Data
  /var/lib/quicbaas/ rw,
  /var/lib/quicbaas/** rwk,

  # Logs
  /var/log/quicbaas/ rw,
  /var/log/quicbaas/** rw,

  # Runtime
  /var/run/quicbaas/ rw,
  /var/run/quicbaas/** rw,

  # Network
  network inet stream,
  network inet6 stream,
  network unix stream,

  # Deny everything else
  deny /home/** rwx,
  deny /root/** rwx,
}
```

**SELinux policy:**

```bash
# Create custom SELinux policy for QuicBaaS
cat > quicbaas.te << 'EOF'
module quicbaas 1.0;

require {
    type quicbaas_t;
    type quicbaas_exec_t;
    type quicbaas_var_lib_t;
    type quicbaas_log_t;
    type quicbaas_var_run_t;
}

# Define domain
type quicbaas_t;
type quicbaas_exec_t;
domain_type(quicbaas_t)
domain_entry_file(quicbaas_t, quicbaas_exec_t)

# Allow network access
allow quicbaas_t self:tcp_socket create_stream_socket_perms;
allow quicbaas_t self:udp_socket create_socket_perms;
corenet_tcp_bind_all_ports(quicbaas_t)

# Allow file access
allow quicbaas_t quicbaas_var_lib_t:dir rw_dir_perms;
allow quicbaas_t quicbaas_var_lib_t:file rw_file_perms;
allow quicbaas_t quicbaas_log_t:dir rw_dir_perms;
allow quicbaas_t quicbaas_log_t:file rw_file_perms;
EOF

# Compile and install
checkmodule -M -m -o quicbaas.mod quicbaas.te
semodule_package -o quicbaas.pp -m quicbaas.mod
semodule -i quicbaas.pp
```

---

## Security Checklist

Use this checklist to verify your hardening implementation:

### Network Security
- [ ] Bind to specific interfaces (not 0.0.0.0 in production)
- [ ] Firewall rules configured
- [ ] NetworkPolicy applied (Kubernetes)
- [ ] Unix socket preferred for local access
- [ ] TCP disabled when not needed
- [ ] mTLS enabled for Raft cluster communication

### Authentication
- [ ] Anonymous access disabled
- [ ] Short access token TTL (15-30 minutes)
- [ ] Refresh token TTL configured
- [ ] MFA required for privileged users
- [ ] WebAuthn/FIDO2 enabled (optional)
- [ ] Strong JWT secret (32+ characters)

### Authorization
- [ ] Least privilege roles assigned
- [ ] Custom roles for specific functions
- [ ] No wildcard permissions (except admin)
- [ ] ABAC policies for complex requirements
- [ ] Regular role audits scheduled

### Encryption
- [ ] Encryption at rest enabled
- [ ] KMS backend configured
- [ ] Key rotation scheduled
- [ ] Backup encryption enabled
- [ ] TLS for all network traffic
- [ ] mTLS for cluster communication

### Audit Logging
- [ ] Comprehensive audit logging enabled
- [ ] Syslog integration configured
- [ ] SIEM integration configured (CEF/LEEF/JSON)
- [ ] Log compression enabled
- [ ] Log tamper protection enabled
- [ ] Retention policy defined
- [ ] Regular log reviews scheduled

### FIPS Compliance (if required)
- [ ] Go 1.24+ with GOFIPS140 build
- [ ] Kernel FIPS mode enabled
- [ ] Kernel cmdline contains fips=1
- [ ] TLS 1.2+ with FIPS ciphers
- [ ] gRPC FIPS interceptor enabled

### Operational Security
- [ ] Secrets in environment variables
- [ ] Restrictive file permissions
- [ ] AppArmor/SELinux configured
- [ ] Backup encryption enabled
- [ ] Regular security audits scheduled

---

## See Also

- [Security Overview](README.md) - Security architecture overview
- [Audit Logging](audit.md) - Detailed audit logging configuration
- [FIPS 140-3](fips.md) - Federal compliance mode
- [REST Security](rest-security.md) - HTTP security headers
- [Authentication](authentication.md) - Authentication configuration
- [Authorization](authorization.md) - Authorization configuration
- [RBAC](rbac.md) - Role-Based Access Control
- [KMS](kms.md) - Key Management Service
- [Raft mTLS](raft-mtls.md) - Cluster communication security
- [Seal/Unseal](seal.md) - Master key protection
- [TLS Setup](tls-setup.md) - TLS configuration guide
