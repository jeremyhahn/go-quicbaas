# Security Hardening — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/security-hardening/`.

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
  sealed_bind_host: "127.0.0.1"  # Sealed-window (pre-unseal) listeners; loopback default
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
  # Sealed-window (pre-unseal ceremony/unseal) listeners stay on loopback by
  # default; set sealed_bind_host: "0.0.0.0" only if remote Shamir officers
  # must reach the sealed surface from outside the pod.
  # sealed_bind_host: "127.0.0.1"
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
    token_ttl: 15m                   # Short-lived access tokens (15 minutes)
    refresh_token_ttl: 24h           # Refresh tokens valid for 24 hours
    max_refresh_count: 48            # Maximum refreshes before re-authentication
```

Tokens are signed with the ES256 key auto-generated in the xkms keystore at first boot; there is no JWT secret to configure.

**Programmatic token configuration:**

```go
authConfig := &auth.AuthConfig{
    Enabled:             true,
    JWTSigningKeyID:     "quicbaas-jwt-signing", // xkms-backed ES256 signing key
    TokenTTL:            15 * time.Minute,       // Access token
    RefreshTokenTTL:     24 * time.Hour,         // Refresh token
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
# Enable IAM with WebAuthn support (IAM is always compiled)

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

### TOTP in FIPS Mode

FIPS mode is a constrained variant of the same product: the code is identical,
and only the algorithms the process is permitted to use change.

RFC 6238 baselines TOTP on HMAC-SHA1, and that remains the default outside FIPS
mode — every authenticator app implements it and existing enrollments are
unaffected. HMAC-SHA1 is not FIPS 140-3 approved, so in FIPS mode QuicBaaS
derives TOTP codes with HMAC-SHA256 instead.

| | Normal build | FIPS mode |
|---|---|---|
| Default algorithm for new enrollments | SHA1 | SHA256 |
| Enrollment with `mfa.algorithm: SHA256` / `SHA512` | supported | supported |
| Enrollment with `mfa.algorithm: SHA1` | supported | refused — `TOTP algorithm SHA1 is not FIPS 140-3 approved` |
| Verifying a secret enrolled as SHA1 | supported, using the algorithm the secret was provisioned with | refused — the user must re-enroll |

Each secret records the algorithm it was provisioned with (`MFASecret.Algorithm`)
and verification uses that stored value, never the current default. A pending
enrollment likewise records the parameters baked into the QR code it issued, so
an enrollment started before a switch into FIPS mode still completes against the
code the authenticator app was provisioned to produce. Changing the configured
default therefore never invalidates an existing enrollment; a deployment can move
to SHA256 and keep verifying every SHA1 secret already in the store.

**Re-enrollment is required for SHA1 secrets in FIPS mode.** The shared secret is
bound to the algorithm the authenticator app was provisioned with, so an
HMAC-SHA1 code cannot be computed without HMAC-SHA1. There is no migration that
preserves such a secret — the operator must have those users re-enroll. QuicBaaS
never silently downgrades to SHA1 to make an existing secret work, and the
refusal is a typed error (`*iam.MFAAlgorithmNotApprovedError`) on the normal
error path, not a panic.

To find the affected enrollments before switching a deployment into FIPS mode,
list the stored secrets whose `algorithm` is `SHA1` or empty (an empty value
predates the field and means SHA1).

Newly issued secrets are 160 bits (RFC 4226 recommends at least 128); FIPS
140-only mode additionally rejects HMAC keys shorter than 112 bits.

### Where the TOTP Shared Secret Lives in Memory

The shared secret is a long-lived key. go-auth's `pkg/otp` accepts it only as
`otp.Config.Seed`, a go-enclave `*enclave.MemoryEnclave` in locked memory that
each HMAC borrows for the moment a code is derived; it has no plaintext string
path. `otp.DecodeSeed` decodes a stored base32 secret straight into the
enclave, `otp.NewAuthenticator` takes ownership of it, and the manager closes
the authenticator — wiping the seed — as soon as the check or the provisioning
URI is done. The submitted code is copied into its own enclave for the
constant-time compare.

This closes the derivation, not the whole path. The secret's route from
persistence to code derivation is:

| Segment | Protected? |
|---|---|
| At rest in the tenant barrier (encrypted with the tenant DEK) | yes, encrypted at rest |
| `KVStore.Get` result — plaintext JSON bytes on the Go heap | no |
| `MFASecret.Secret` / `MFAEnrollment.Secret` — a Go string from `json.Unmarshal` | no; a string cannot be wiped |
| Decoded key in `otp.Config.Seed` | yes — locked enclave, destroyed after each check |

The two unprotected segments are properties of the storage interface handing
the secret back as a string, not of the MFA manager, and closing them requires
the store — and the KV read below it — to return protected memory. Enrollment
additionally renders the base32 form deliberately: the user has to scan or type
it, so that copy is inherent to enrolling a shared secret.

**Accepted known limitation (go-quicbaas#402).** The two unprotected segments
are accepted as the boundary for now; QuicBaaS does not work around them. The
KV read belongs to go-qrdb: `interfaces.KVStore.Get` returns `[]byte` from the
qrdb read beneath it, so only a qrdb read that returns protected memory can
close the first segment. A local shim would copy the plaintext into an enclave
after it had already reached the heap, which removes nothing. Once go-qrdb
adopts go-enclave, a protected-bytes KV read is to be filed on go-qrdb.
QuicBaaS then moves the MFA store and `MFASecret.Secret` /
`MFAEnrollment.Secret` onto that read, which closes both segments.

Each enclave is destroyed when the authenticator built from it is done, so the
protection is released rather than held for the process lifetime.

#### Accepted boundary: the KV read and MFA storage

The two unprotected segments above are an **accepted boundary**
(go-quicbaas#402). Today a TOTP secret is protected at rest by
the tenant barrier and during code derivation by the go-enclave seed; between
the two it is ordinary heap memory — the `KVStore.Get` bytes and the
`MFASecret.Secret` string. QuicBaaS does not work around this in the MFA
manager: closing it needs a KV read that returns protected bytes, which is
go-qrdb's to provide, and the MFA store moves to it once it exists.

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
# IAM/ABAC is always compiled; no build tag is required.

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
# Allow write access only during business hours
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: business-hours-only
  tenant: production
spec:
  subjects:
    roles: [readwrite]
  rules:
    - name: business-hours-writes
      effect: allow
      actions: [write, delete]
      resources:
        - keys/**
      when:
        - expression: 'time.hour >= 9 && time.hour < 17'
        - expression: 'time.weekday in ["monday", "tuesday", "wednesday", "thursday", "friday"]'
```

**IP-based access policy:**

```yaml
# /etc/quicbaas/policies/ip-restriction.yaml
# Restrict admin access to internal network
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: internal-network-only
  tenant: production
spec:
  subjects:
    roles: [admin]
  rules:
    - name: internal-admin
      effect: allow
      actions: ["*"]
      resources:
        - "**"
      when:
        # Flat conditions only: nested and/or/not is refused at write time.
        - expression: 'context.source_ip in_cidr "10.0.0.0/8"'
```

**Resource ownership policy:**

```yaml
# /etc/quicbaas/policies/resource-ownership.yaml
# Users can only access their own resources
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: owner-access
  tenant: production
spec:
  subjects:
    authenticated: true
  rules:
    - name: own-resources
      effect: allow
      actions: [read, write, delete]
      resources:
        - keys/users/${subject.id}/**
        - secrets/users/${subject.id}/**
```

**Multi-tenant isolation policy:**

```yaml
# /etc/quicbaas/policies/tenant-isolation.yaml
# Enforce tenant data isolation
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: tenant-isolation
spec:
  subjects:
    authenticated: true
  rules:
    - name: cross-tenant-deny
      effect: deny
      priority: 1000  # High priority
      actions: ["*"]
      resources:
        - "**"
      unless:
        - expression: 'resource.tenant == subject.tenant'
```

---

## Encryption

### Data at Rest Encryption (KMS)

At-rest encryption is provided by the embedded qrdb seal/barrier subsystem;
there is no standalone key-file toggle. Configure the Key Management Service
for application-level key operations:

```yaml
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

QuicBaaS reaches TPM 2.0 through go-xkms; the configuration key below selects
it. TPM behaviour, PCR binding and TPM policies are documented in go-xkms:
[TPM 2.0](../../../go-xkms/docs/tpm2/user.md) and
[TPM Policies](../../../go-xkms/docs/tpm-policies/user.md).

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

The audit HMAC key is never held in plain Go memory: `AuditConfig.SignKey`
is an mlocked `secure.Buffer` that the audit logger owns and destroys on
`Close` (see `docs/audit-logging/engineering.md`).

The signing key must be at least 14 bytes (112 bits) per FIPS 140-3
(SP 800-107); the audit logger refuses to start with a shorter key. Generate
one with `openssl rand -base64 32`.

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

**Verify audit log integrity:** the CLI has no audit verifier. Check each
event's `signature` in Go with `AuditLogger.VerifySignature(event)` from
`pkg/security`; with `sign_logs` on, an event with no signature reports
unverified. To read the IAM audit trail, use
`quicbaas iam audit query --start-time 2025-01-07T00:00:00Z --outcome failure`.

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

### Data Encryption Keys Under FIPS

ChaCha20-Poly1305 remains a supported DEK algorithm; FIPS activation is what
takes it away. The artifact is the same either way — a FIPS-capable build that
becomes FIPS-activated at runtime — so the refusal has to name the mode, not
the algorithm, or it reads as an unknown-algorithm bug.

| | Normal build | FIPS-activated |
|---|---|---|
| `kms.defaults.algorithm: AES-256-GCM` | accepted | accepted |
| `kms.defaults.algorithm: CHACHA20-POLY1305` | accepted | refused at startup |
| `CreateDEK` with AES-128/192/256-GCM | accepted | accepted |
| `CreateDEK` with (X)ChaCha20-Poly1305 | accepted | refused |
| `RotateDEK` of an existing ChaCha20 DEK | accepted | refused |

Both refusals carry a typed `*fips.AEADNotApprovedError`, reachable with
`errors.As`, naming the operation, the algorithm and the approved set:

```
kms.CreateDEK: CHACHA20-POLY1305 is not a FIPS 140-3 approved AEAD and this
process is FIPS-activated; approved: AES-128-GCM, AES-192-GCM, AES-256-GCM
```

A rotation is refused as well as a creation, because re-keying carries the
stored algorithm forward — it would hand a FIPS-activated process a fresh
unapproved key. Existing ChaCha20 DEK records are left intact and readable by a
process that is not FIPS-activated; switch the deployment's algorithm and
create a new DEK to move the data.

Nothing is silently substituted. A deployment that asked for ChaCha20 and got
AES without being told would be holding a key that is not the one its
configuration names.

### Placement Hashing Under FIPS

The consistent-hash ring (`pkg/util/hash`) keeps **blake2b** as its default.
Placing a key on the ring decides which node owns it; it is not a
cryptographic security function, and nothing about the placement is a secret, a
signature or a key. Blake2b is also not served by the Go Cryptographic Module,
so `GODEBUG=fips140=only` does not refuse it.

**SHA-256 is selectable** for deployments that would rather every hash come
from an approved module:

```go
hashFunc, err := hash.Select("sha256")   // "blake2b" (default), "sha256", "fnv1a"
if err != nil {
    return err
}
ring, err := hash.New(hash.Config{VirtualNodes: 150, HashFunc: hashFunc})
```

An unknown name yields a typed `*hash.UnknownAlgorithmError` listing what would
have been accepted, so a ring is never built with a hash other than the one
that was asked for. Changing the algorithm re-places every key: pick it when
the ring is created, not while it is serving.

### Conformance Pass

`GOFIPS140=latest` activates the module; `GODEBUG=fips140=only` makes it
**refuse** unapproved algorithms rather than merely deprioritize them. The
second is what proves conformance, and it is what this target runs:

```bash
make test-fips-conformance
```

It covers client-secret login (PBKDF2-HMAC-SHA256), envelope field encryption,
the DEK algorithm gate, placement hashing and configuration validation. The
TOTP surface is deliberately outside this pass: it carries protocol-mandated
HMAC-SHA1, which has its own FIPS treatment documented under *TOTP in FIPS
Mode* above.

**Known gap — `pkg/security` keyring encryption is not yet conformant.** Its
AEAD is built by go-xkms `pkg/crypto/aead.NewGCM`, which calls
`cipher.NewGCM` and supplies the IV itself; `fips140=only` refuses it, exactly
as the field path did before this change. The fix belongs upstream in go-xkms
(the same `cipher.NewGCMWithRandomNonce` substitution that package already
applies elsewhere, e.g. `pkg/seal/secret_sealer.go`), so the package is named
here rather than quietly left out of the pass.

**The AES-128 field path carries one `#nosec G407`.**
`cipher.NewGCMWithRandomNonce`'s `Seal` takes a nil nonce by contract — it
panics on a non-empty one and draws the nonce from the module DRBG itself —
but gosec matches the literal argument, not the constructor, and reads it as a
hardcoded IV. The annotation sits on that single line with the reason written
out, which is what `GOSEC_EXCLUDE` in the Makefile prescribes: G407 is
deliberately absent from the repo-wide exclude list, so the one site that can
raise it is answered where a reviewer reads the code rather than silenced for
every future site as well.

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

**JWT signing keys are not managed secrets.** There is no `jwt_secret` — no JWT key material can be configured at all. The ES256 token-signing key is generated inside the configured xkms backend at first boot and never leaves the keystore. The hardening decision is backend choice: select a hardware-backed xkms backend (TPM 2.0 or PKCS#11) so the signing key is hardware-protected.

**Never store the remaining secrets (backup keys, provider credentials) in configuration files:**

```yaml
# BAD - Secret in config file
backup:
  encryption_key: "my-super-secret-key"        # NEVER do this!

# GOOD - Secret from environment
backup:
  encryption_key: "${BACKUP_ENCRYPTION_KEY}"   # From environment variable
```

**Environment variable management:**

```bash
# Set secrets via environment (not in shell history)
read -s BACKUP_ENCRYPTION_KEY && export BACKUP_ENCRYPTION_KEY

# Or use a secrets manager
export BACKUP_ENCRYPTION_KEY=$(vault kv get -field=backup_encryption_key secret/quicbaas)
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
  backup-encryption-key: <base64-encoded-secret>
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
            - name: BACKUP_ENCRYPTION_KEY
              valueFrom:
                secretKeyRef:
                  name: quicbaas-secrets
                  key: backup-encryption-key
```

**HashiCorp Vault integration:**

```bash
# Store secrets in Vault
vault kv put secret/quicbaas \
  backup_encryption_key="$(openssl rand -base64 32)" \
  kms_master_key="$(openssl rand -base64 32)"

# Retrieve at runtime
export BACKUP_ENCRYPTION_KEY=$(vault kv get -field=backup_encryption_key secret/quicbaas)
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

- Security Overview - Security architecture overview
- Audit Logging - Detailed audit logging configuration
- FIPS 140-3 - Federal compliance mode
- REST Security - HTTP security headers
- Authentication - Authentication configuration
- Authorization - Authorization configuration
- RBAC - Role-Based Access Control
- KMS - Key Management Service
- Raft mTLS - Cluster communication security
- Seal/Unseal - Master key protection
- TLS Setup - TLS configuration guide


---

## FIPS 140-3 Compliance

QuicBaaS supports FIPS 140-3 compliance mode for deployments requiring federal cryptographic standards. This document covers configuration, approved algorithms, and validation requirements.

### Overview

QuicBaaS leverages Go 1.24+'s native FIPS 140-3 support via the `crypto/fips140` package. When FIPS mode is enabled:

- The Go crypto library automatically restricts to FIPS 140-3 approved algorithms
- Non-approved cryptographic operations will fail at runtime
- TLS automatically uses only approved cipher suites
- Key sizes are validated automatically

**Note:** Algorithm validation is handled automatically by the Go runtime - no manual validation is required in application code.

### Configuration

#### Enable FIPS Mode

```yaml
security:
  fips_mode: true
  encryption_algorithm: "aes256"    # FIPS-approved

  # TLS configuration for FIPS
  tls_enabled: true
  tls_min_version: "1.2"            # TLS 1.2 minimum
  tls_cipher_suites:
    - TLS_AES_128_GCM_SHA256        # TLS 1.3
    - TLS_AES_256_GCM_SHA384        # TLS 1.3
    - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
    - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
```

#### Build with FIPS Support

```bash
# Build FIPS-compliant binary with Go 1.24+
make build-fips

# Manual build:
GOFIPS140=v1.0.0 go build -o bin/quicbaas-fips ./cmd/quicbaas
```

#### Check FIPS Status

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/fips"

// Check if FIPS mode is enabled
if fips.Enabled() {
    fmt.Println("Running in FIPS 140-3 compliant mode")
}

// Get detailed status for diagnostics
status := fips.GetStatus()
fmt.Printf("FIPS Enabled: %v\n", status.Enabled)
fmt.Printf("Kernel FIPS Mode: %v\n", status.KernelFIPSMode)
fmt.Printf("Kernel Cmdline: %v\n", status.KernelCmdline)
fmt.Printf("Crypto Provider: %s\n", status.CryptoProvider)

// Log status for audit purposes
fips.LogStatus()
```

### Approved Algorithms

When Go FIPS mode is enabled, only these algorithms are available. Non-approved algorithms will fail automatically.

#### Symmetric Encryption

| Algorithm | Key Size | Description |
|-----------|----------|-------------|
| AES-128-GCM | 128 bits | AES-128-GCM authenticated encryption |
| AES-256-GCM | 256 bits | AES-256-GCM authenticated encryption |

#### Hash Functions

| Algorithm | Output Size | Description |
|-----------|-------------|-------------|
| SHA-256 | 256 bits | SHA-256 |
| SHA-384 | 384 bits | SHA-384 |
| SHA-512 | 512 bits | SHA-512 |

#### Digital Signatures

| Algorithm | Key Size | Description |
|-----------|----------|-------------|
| ECDSA P-256 | 256 bits | ECDSA with P-256 curve |
| ECDSA P-384 | 384 bits | ECDSA with P-384 curve |
| ECDSA P-521 | 521 bits | ECDSA with P-521 curve |
| RSA-2048 | 2048 bits | RSA (minimum for FIPS) |
| RSA-3072 | 3072 bits | RSA |
| RSA-4096 | 4096 bits | RSA |

#### Key Agreement

| Algorithm | Key Size | Description |
|-----------|----------|-------------|
| ECDH P-256 | 256 bits | ECDH with P-256 curve |
| ECDH P-384 | 384 bits | ECDH with P-384 curve |
| ECDH P-521 | 521 bits | ECDH with P-521 curve |

#### Key Derivation

| Algorithm | Description |
|-----------|-------------|
| HKDF | HMAC-based Key Derivation Function |
| PBKDF2 | Password-Based Key Derivation Function 2 |

#### Message Authentication

| Algorithm | Description |
|-----------|-------------|
| HMAC-SHA-256 | HMAC with SHA-256 |
| HMAC-SHA-384 | HMAC with SHA-384 |
| HMAC-SHA-512 | HMAC with SHA-512 |

### TLS Requirements

#### Minimum TLS Version

FIPS 140-3 requires TLS 1.2 or higher. QuicBaaS enforces TLS 1.3 by default in FIPS mode.

#### Approved Cipher Suites

TLS 1.3 cipher suites:

| Cipher Suite | Description |
|--------------|-------------|
| TLS_AES_128_GCM_SHA256 | AES-128-GCM with SHA-256 |
| TLS_AES_256_GCM_SHA384 | AES-256-GCM with SHA-384 |

TLS 1.2 cipher suites:

| Cipher Suite | Description |
|--------------|-------------|
| TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 | ECDHE-RSA with AES-128-GCM |
| TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384 | ECDHE-RSA with AES-256-GCM |
| TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 | ECDHE-ECDSA with AES-128-GCM |
| TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384 | ECDHE-ECDSA with AES-256-GCM |

#### Server Certificate SANs

When no operator cert is configured, the server issues its own TLS
certificate from its CA (`pkg/server/server_cert_sans.go`). Its SANs are:

- `localhost`, `127.0.0.1` and `::1`;
- `server.host`, unless it is empty or `0.0.0.0`;
- the machine hostname;
- `server.additional_san_dns` and `server.additional_san_ips`.

Each SAN appears once. DNS names are compared without regard to case. IPs
are compared by value, so `::1` and `0:0:0:0:0:0:0:1` are one entry. A host
or DNS entry that parses as an IP becomes an IP SAN. The config validator
rejects an entry in `server.additional_san_ips` that is not an IP.

On each boot the server compares the stored certificate with this set. If a
configured SAN is missing, the server revokes the stored certificate
(reason: superseded) and issues a new one over the same key. It logs one
line naming the added SANs. Adding a SAN to the configuration therefore
takes effect at the next restart. A certificate that already covers the set
is kept as it is.

Before revoking, the server asks the CA whether the stored serial is already
revoked. A boot that revoked the certificate but stopped before deleting it
leaves a revoked certificate stored; the next boot skips the revocation,
which the CA would refuse, and replaces the certificate. If the CA cannot
answer, the server refuses to start.

`security.tls_cert_file` and `security.tls_key_file` decide where the server
certificate comes from. There are four outcomes:

- **Neither path configured.** The server issues its own certificate and
  writes it to the default certs directory. On later boots the SAN check
  above runs on it.
- **Both files exist and hold this server's certificate.** The server treats
  the certificate as self-issued when its leaf is byte-identical to the
  certificate the CA stores for the server CN and chains to this CA. The
  server keeps managing it: the SAN check runs on it, and a re-issued
  certificate is written back to the configured paths.
- **Both files exist and hold any other certificate.** This covers a
  certificate from another CA, or one this CA has since superseded. The
  server treats it as the operator's, serves it as given and never re-issues
  it or writes over it.
- **A configured path is absent, or only one of the two is configured.** The
  boot fails with `ServerCertPathError` (matching `ErrServerCertPath`). The
  error names both configured paths and says which key is absent or unset.
  The server writes no certificate or key file.

### gRPC FIPS Validation

QuicBaaS includes gRPC interceptors that validate FIPS compliance for every request.

#### Validation Checks

The FIPS interceptor validates:

1. **TLS Enabled**: Connection must use TLS encryption
2. **TLS Version**: Must be TLS 1.3 or higher (TLS 1.2 allowed with warning)
3. **Cipher Suite**: Must use a FIPS-approved cipher suite

Non-compliant connections receive:

```
rpc error: code = FailedPrecondition desc = FIPS compliance validation failed: insufficient TLS version
```

### Linux Kernel FIPS Mode

QuicBaaS can detect and integrate with Linux kernel FIPS mode.

#### Check Kernel FIPS Mode

```bash
# Check if kernel FIPS mode is enabled
cat /proc/sys/crypto/fips_enabled
# 1 = enabled, 0 = disabled

# Check kernel command line
cat /proc/cmdline | grep fips
```

#### Enable Kernel FIPS Mode

```bash
# Add to kernel boot parameters
fips=1

# On RHEL/CentOS
sudo fips-mode-setup --enable
sudo reboot

# On Ubuntu/Debian
sudo apt-get install fips-initramfs
sudo update-initramfs -u
# Add fips=1 to GRUB_CMDLINE_LINUX in /etc/default/grub
sudo update-grub
sudo reboot
```

#### Automatic Detection

QuicBaaS reports kernel FIPS status in the status output:

```
=== FIPS 140-3 Status ===
Go FIPS Mode: true
Kernel FIPS Mode: true
Kernel Cmdline fips=1: true
Crypto Provider: Go FIPS 140-3
========================
```

### Go 1.24+ FIPS Support

Go 1.24+ includes native FIPS 140-3 support. Build with GOFIPS140 to enable.

#### Build Requirements

```bash
# Requires Go 1.24+
export GOFIPS140=v1.0.0
go build ./cmd/quicbaas
```

#### Verify FIPS Mode

Check if FIPS mode is active:

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/fips"

status := fips.GetStatus()
if status.Enabled {
    fmt.Println("Go FIPS 140-3 Module active")
} else {
    fmt.Println("Using Go Standard Crypto")
}
```

### Compliance Checklist

#### Build Requirements

- [ ] Go 1.24+
- [ ] Built with GOFIPS140 environment variable

#### Configuration

- [ ] fips_mode: true in configuration
- [ ] TLS 1.2+ minimum version
- [ ] Only FIPS-approved cipher suites configured

#### Runtime

- [ ] FIPS mode enabled at startup
- [ ] Kernel FIPS mode enabled (if applicable)
- [ ] gRPC FIPS interceptor enabled
- [ ] Non-approved algorithms rejected automatically

#### Monitoring

- [ ] FIPS status logged at startup
- [ ] Non-compliant connections rejected

### Error Messages

| Error | Cause | Resolution |
|-------|-------|------------|
| crypto operation failed | Using non-approved algorithm | Use only FIPS-approved algorithms |
| TLS version below minimum | TLS 1.1 or lower | Configure TLS 1.2+ |
| cipher suite not FIPS-approved | Non-approved cipher | Configure approved cipher suite |

### See Also

- Security README - Security overview
- TLS Setup - TLS configuration guide
- Hardening Guide - Production hardening
- [Go FIPS Documentation](https://go.dev/doc/fips) - Official Go FIPS docs
