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

The backend assigns the version number of every write, and the secrets
service reports that number. The service never computes one from the size
of the history: a number is never reused, so once a version is removed the
history can have gaps and the next write is still past every version the
backend ever assigned. Concurrent writers to one secret each get their own
version. A manager that cannot report the version of a write, or a read
that carries none, is an internal error.

### Concurrent Writers

The internal KV backend (`pkg/secrets/kv_backend.go`) keeps each version
under `_qb/secrets/<path>/v/<N>` and names the current one in a pointer at
`_qb/secrets/<path>/current`. When the injected store arbitrates guarded
writes (`PutIfAbsent`, `CompareAndSwap`, `CompareAndDelete`, as the server's
store does), every pointer write is conditional on the exact pointer bytes
the writer read:

- the first Store of a path is `PutIfAbsent`;
- Store, soft Delete and Rollback are `CompareAndSwap`;
- Destroy is `CompareAndDelete`.

A writer whose read went stale fails with `ErrWriteConflict`, removes the
version entry it reserved, and leaves the winner's write in place; retrying
re-reads the pointer. On a store without these verbs the pointer write is
unconditional, so run exactly one writer process per shared store.

## Security Model

### Defense in Depth

1. **Transport Security** - TLS/mTLS for all API calls
2. **Authentication** - Token-based auth via QuicBaaS IAM
3. **Authorization** - Path-based RBAC policies
4. **Encryption** - AES-GCM or ChaCha20-Poly1305
5. **Key Protection** - HSM or software key storage
6. **Audit Logging** - All operations logged

### Tenant Scoping and Authorization

Every secret is stored under the tenant that owns it. `secretsTenantScopedID`
(`pkg/service/secrets_tenant.go`) maps a secret path to
`tenant/<tenantID>/<path>` for a tenant caller and leaves it unprefixed for
the system tenant, whose scope is the platform itself rather than a namespace
literally named `system`.

**One resolver decides the namespace.** `secretsScopedTenant` reconciles the
tenant bound to the request context with the tenant a request names in its
`tenant_id` field. `StoreSecret` and `EnsureSecret` — the only two requests
that carry a tenant — and `GetSecret`, the read they must agree with, all
resolve their namespace through it, so a write and a read can never land in
different namespaces for the same request. The rule is that a request may name
only its own tenant:

| caller | `tenant_id` | result |
|---|---|---|
| tenant T | omitted | stored under T |
| tenant T | `T` | stored under T, encrypted with T's DEK |
| tenant T | `U` | refused, `403 cross_tenant_access` |
| system | omitted | stored in the platform namespace |
| system | any tenant | refused, `403 cross_tenant_access` |

`tenant_id` selects the tenant-keyed DEK **and** the storage namespace; the two
are the same tenant by construction. It previously selected only the DEK, so a
value could be encrypted for one tenant and stored in another's namespace, and
`EnsureSecret` applied it to the path with no check at all — which let any
caller steer a generate-and-store into another tenant's namespace.

**Permissions.** Every transport is authorized with go-qrdb's
`secrets:read` / `secrets:write` / `secrets:delete` / `secrets:list`
(`pkg/permission/broker_scope.go`), which qrdb registers `ScopeTenantOnly` and
already grants to the built-in Admin role. `ScopeTenantOnly` admits a request
only when the identity is not in the system tenant and the identity's tenant
equals the request's target tenant — so the policy decision point establishes
the same invariant the service enforces, and the platform operator is
deliberately not able to read or write a tenant's secrets through the API.

These routes must **not** carry `credentials:*`. That family guards qrdb's
named-credential store, which has no tenant namespacing and is therefore
registered `ScopeCross` — system tenant only. Authorizing the tenant vault
with it denied every tenant identity, making the tenant-facing secrets API
unreachable (go-quicbaas#534).

**One grant per operation, whichever transport carries it.** The vault is
reachable on every transport and each has its own gate, so the same operation
is mapped onto the same permission in each of the three tables:

| operation | permission | REST | gRPC / Unix | MCP |
|---|---|---|---|---|
| enumerate names | `secrets:list` | `GET /api/v1/secrets/` | `ListSecrets` | `quicbaas_secrets_list` |
| read a value, a version, a prefix or a watch | `secrets:read` | `GET /api/v1/secrets/*`, `secrets-prefix/*`, `secrets-versions/*`, `secrets-watch/*` | `GetSecret`, `GetSecretVersion`, `ListSecretVersions`, `GetSecretsByPrefix`, `WatchSecretsByPrefix`, `GetRotationPolicy` | `quicbaas_secrets_get`, `_get_prefix`, `_versions`, `_watch`, `_get_rotation_policy` |
| any mutation | `secrets:write` | `POST /api/v1/secrets/*`, `PUT /api/v1/secrets/*` | `StoreSecret`, `EnsureSecret`, `RotateSecret`, `RollbackSecret`, `UpdateSecretStatus`, `SetRotationPolicy` | `quicbaas_secrets_store`, `_ensure`, `_rotate`, `_rollback`, `_update_status`, `_set_rotation_policy` |
| removal | `secrets:delete` | `DELETE /api/v1/secrets/*` | `DeleteSecret` | `quicbaas_secrets_delete` |

`list` is the names-only enumeration; anything that returns secret material or
its metadata is `read`. The two rotation-policy verbs have no REST route of
their own and take read / write of the policy sub-resource on the same family.

The tables live in `pkg/api/rest/routes_permissions_generated.go`,
`pkg/api/grpc/authz_mapping.go` (plus the `(qrdb.v1.permission)` annotations in
`api/proto/kms.proto`, which qrdb's unified-server interceptor enforces) and
`pkg/api/mcp/authz_qrdb.go`. The QUIC transport carries the REST routes over
HTTP/3 and the Unix socket carries gRPC, so neither has a table of its own.
None of the three is exported to the others, so each pins itself to the same
literals in its own package — `TestRoutes_SecretsVaultCarriesTenantScopedPermissions`,
`TestSecretsVaultGRPCPermissions` and `TestSecretsVaultMCPPermissions` — and the
gRPC and MCP suites additionally drive qrdb's real RBAC authorizer to prove the
three outcomes the family exists to produce: a tenant is admitted in its own
vault, refused when it names another tenant, and the system tenant is denied
(go-quicbaas#682).

**The system tenant has no vault over the API today.** `ScopeTenantOnly`
denies it by design — "a platform (system) operator must not exercise them on a
tenant's behalf". Restoring a private system vault needs a new upstream scope
and is tracked in go-quicbaas#689; a `credentials:*` fallback for system
callers is not an option, because it would reopen the cross-tenant reach
go-quicbaas#534 closed.

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

See IAM Documentation for complete token management.

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

- [Static Secrets](user.md)
- IAM & Authentication
- [Kubernetes Integration](engineering.md)
- [CLI Reference](cli.md)
- [API Reference](api.md)
