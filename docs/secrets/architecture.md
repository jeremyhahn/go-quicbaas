# Secrets — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/secrets/`.

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


---

## Dynamic Secrets

Dynamic secrets are short-lived, automatically generated credentials that are created on-demand and expire after a configurable period. Unlike static secrets that are stored and retrieved, dynamic secrets are generated fresh each time they're requested, significantly reducing the risk of credential compromise.

### Overview

QuicBaaS supports several types of dynamic secrets:

| Type | Description | Use Case |
|------|-------------|----------|
| **Database Credentials** | Temporary database users with short-lived passwords | Application database access |
| **OIDC/JWT Tokens** | Short-lived identity tokens for service authentication | Service-to-service auth |
| **X.509 Certificates** | Dynamically generated TLS certificates | mTLS, client authentication |
| **Application Secrets** | Bulk secret retrieval for containers/applications | Kubernetes workloads |

### Database Credentials

Generate temporary database users with automatically rotated passwords. QuicBaaS creates the user in the database, provides credentials to the application, and revokes the user when the lease expires.

#### Supported Databases

- **PostgreSQL** (`pkg/credentials/database/postgres.go`)
- **MySQL** (`pkg/credentials/database/mysql.go`)

#### Configuration

Define a role that specifies how credentials should be generated:

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/credentials"

role := &credentials.Role{
    Name:          "my-app-db-reader",
    GeneratorType: "postgres",
    Config: map[string]string{
        "connection_url": "postgres://admin:password@localhost:5432/mydb?sslmode=disable",
        "creation_statements": `
            CREATE USER "{{username}}" WITH PASSWORD '{{password}}' VALID UNTIL '{{expiration}}';
            GRANT SELECT ON ALL TABLES IN SCHEMA public TO "{{username}}";
        `,
        "revocation_statements": `
            DROP USER IF EXISTS "{{username}}";
        `,
        "username_template": "app_{{random_hex(8)}}",
    },
    DefaultTTL: 1 * time.Hour,
    MaxTTL:     24 * time.Hour,
}
```

#### Usage

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/credentials"
    "github.com/jeremyhahn/go-quicbaas/pkg/credentials/database"
)

// Create the generator
gen := database.NewPostgresGenerator(dbConfig, logger)

// Register with the registry
registry := credentials.NewGeneratorRegistry()
registry.Register(gen)

// Generate credentials
cred, err := gen.Generate(ctx, role)
if err != nil {
    return err
}

// Use the credentials
username := cred.Data["username"]
password := cred.Data["password"]
connectionURL := cred.Data["connection_url"]

// Credentials automatically expire at cred.ExpiresAt
```

#### Template Variables

| Variable | Description |
|----------|-------------|
| `{{username}}` | Generated username |
| `{{password}}` | Generated password |
| `{{expiration}}` | Timestamp when credentials expire |
| `{{random_hex(N)}}` | N random hex characters |

### OIDC/JWT Tokens

Generate short-lived JWT tokens for service authentication. The IAM module provides comprehensive token generation.

#### Token Types

- **Access Tokens**: For API authorization
- **ID Tokens**: For identity claims
- **Refresh Tokens**: For token renewal

#### Usage

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/iam"

tokenService := iam.NewTokenService(config, logger)

// Generate access token
accessToken, err := tokenService.GenerateAccessToken(ctx, &iam.AccessTokenRequest{
    Subject:   "service-account-123",
    Audience:  []string{"api.example.com"},
    Scopes:    []string{"read:data", "write:data"},
    ExpiresIn: 15 * time.Minute,
})

// Generate ID token
idToken, err := tokenService.GenerateIDToken(ctx, &iam.IDTokenRequest{
    Subject: "user@example.com",
    Claims: map[string]interface{}{
        "name":  "John Doe",
        "email": "john@example.com",
    },
})
```

See [IAM Documentation](../iam/README.md) for complete token management.

### X.509 Certificates

Generate short-lived TLS certificates signed by a configured CA. Certificates are created on-demand and automatically expire.

#### Configuration

Enable certificate generation in your configuration:

```yaml
secrets:
  certificates:
    enabled: true
```

#### Role Configuration

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/credentials/certificate"

gen := certificate.NewCertificateGenerator(true, logger)

role := &credentials.Role{
    Name:          "service-mtls",
    GeneratorType: "certificate",
    Config: map[string]string{
        "ca_cert":      string(caCertPEM),      // PEM-encoded CA certificate
        "ca_key":       string(caKeyPEM),       // PEM-encoded CA private key
        "common_name":  "my-service",
        "organization": "My Company",
        "usage":        "both",                  // client, server, or both
        "dns_names":    "localhost,my-service.local",
        "ip_addresses": "127.0.0.1,::1",
    },
    DefaultTTL: 24 * time.Hour,
    MaxTTL:     7 * 24 * time.Hour,
}
```

#### Usage

```go
// Generate certificate
cred, err := gen.Generate(ctx, role)
if err != nil {
    return err
}

// Access certificate and key
certPEM := cred.Data["certificate"]
keyPEM := cred.Data["private_key"]
caCertPEM := cred.Data["ca_cert"]
serialNumber := cred.Data["serial_number"]

// Use for TLS configuration
cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
```

#### Certificate Options

| Option | Description | Values |
|--------|-------------|--------|
| `common_name` | Certificate CN | String |
| `organization` | Organization name | String (default: "QuicBaaS") |
| `usage` | Key usage | `client`, `server`, `both` |
| `dns_names` | Subject Alternative Names | Comma-separated hostnames |
| `ip_addresses` | IP SANs | Comma-separated IPs |

#### Rotation

```go
// Rotate to get a new certificate
newCred, err := gen.Rotate(ctx, oldCred, role)
```

### Application Secrets

Fetch all secrets for an application at once using path-based organization. This is essential for container workloads that need to mount multiple secrets.

#### Path Organization

Organize secrets by path prefix:

```
/apps/
  myapp/
    database/
      username
      password
      host
    api/
      key
      secret
    config/
      settings.json
```

#### Bulk Retrieval

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/service"

// Get all secrets for an application
resp, err := secretsService.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{
    Prefix:      "/apps/myapp/",
    IncludeData: true,
})

// Access secrets by path
for path, data := range resp.Secrets {
    fmt.Printf("Secret: %s\n", path)
    for key, value := range data {
        fmt.Printf("  %s: %s\n", key, string(value))
    }
}
```

#### Watching for Changes

Real-time secret updates for sidecars and agents:

```go
changes, err := secretsService.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{
    Prefix: "/apps/myapp/",
})

for change := range changes {
    switch change.ChangeType {
    case service.SecretChangeCreated:
        fmt.Printf("New secret: %s\n", change.Path)
    case service.SecretChangeUpdated:
        fmt.Printf("Updated secret: %s\n", change.Path)
    case service.SecretChangeDeleted:
        fmt.Printf("Deleted secret: %s\n", change.Path)
    }

    // Reload application configuration
    reloadConfig(change)
}
```

#### REST API

```bash
# List secrets by prefix
curl -X GET "https://quicbaas:8443/api/v1/secrets?prefix=/apps/myapp/"

# Get all secrets with data
curl -X GET "https://quicbaas:8443/api/v1/secrets/prefix?prefix=/apps/myapp/&include_data=true"
```

#### CLI Usage

```bash
# List secrets under a prefix
quicbaas secrets list --prefix /apps/myapp/

# Get all secrets for an app
quicbaas secrets get-prefix /apps/myapp/

# Watch for changes
quicbaas secrets watch /apps/myapp/
```

### Lease Management

All dynamic secrets are associated with leases that control their lifecycle.

#### Lease Operations

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/lease"

// Create lease manager
leaseManager := lease.NewManager(storage, logger)

// Create a lease for a credential
lease, err := leaseManager.Create(ctx, &lease.CreateRequest{
    ResourceID:   cred.CredentialID,
    ResourceType: "database_credential",
    TTL:          1 * time.Hour,
    Renewable:    true,
})

// Renew a lease
err = leaseManager.Renew(ctx, lease.ID, 30*time.Minute)

// Revoke a lease (triggers credential cleanup)
err = leaseManager.Revoke(ctx, lease.ID)
```

#### Automatic Cleanup

When leases expire:
1. The credential is revoked in the backend system
2. Database users are dropped
3. Certificates are added to revocation lists
4. Resources are cleaned up

### Service Layer API

The `DynamicSecretsService` provides a unified interface for all dynamic secret types:

```go
type DynamicSecretsService interface {
    // Database credentials
    GenerateDatabaseCredentials(ctx context.Context, req *GenerateDBCredentialsRequest) (*Credential, error)

    // Certificates
    GenerateCertificate(ctx context.Context, req *GenerateCertRequest) (*Credential, error)

    // OIDC tokens (via IAM service)
    GenerateToken(ctx context.Context, req *GenerateTokenRequest) (*TokenResponse, error)

    // Application secrets
    GetSecretsByPrefix(ctx context.Context, req *GetSecretsByPrefixRequest) (*GetSecretsByPrefixResponse, error)
    WatchSecretsByPrefix(ctx context.Context, req *WatchSecretsRequest) (<-chan *SecretChange, error)

    // Lease management
    RenewLease(ctx context.Context, leaseID string, increment time.Duration) error
    RevokeLease(ctx context.Context, leaseID string) error
}
```

### Security Considerations

1. **Short TTLs**: Use the shortest practical TTL for credentials
2. **Least Privilege**: Generate credentials with minimal required permissions
3. **Audit Logging**: All credential generation/revocation is logged
4. **Encryption at Rest**: Credentials are encrypted in storage
5. **TLS in Transit**: All API communication uses TLS

### Best Practices

1. **Use Dynamic Secrets for Production**: Avoid static credentials in production environments
2. **Implement Rotation**: Use automatic rotation for long-running applications
3. **Monitor Lease Expiration**: Set up alerts for failed renewals
4. **Path Organization**: Use consistent path structures for application secrets
5. **Separate Environments**: Use different path prefixes for dev/staging/production

### Related Documentation

- [Static Secrets](./README.md)
- [IAM & Authentication](../iam/README.md)
- [Kubernetes Integration](../kubernetes/README.md)
- [CLI Reference](./cli-usage.md)
- [API Reference](./api-reference.md)
