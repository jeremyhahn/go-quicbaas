# Secrets — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/secrets/`.

## Configuration File

```yaml
# quicbaas.yaml
secrets:
  enabled: true

  # Encryption settings
  encryption:
    # Default encryption algorithm
    # Options: AES-128-GCM, AES-192-GCM, AES-256-GCM,
    #          ChaCha20-Poly1305, XChaCha20-Poly1305
    default_algorithm: AES-128-GCM

    # Default encryption key ID (go-xkms key reference)
    default_key_id: quicbaas-secrets-key

    # Backend for encryption keys
    # Options: software, tpm2, pkcs11, awskms, azurekv, gcpkms
    backend: software

  # Versioning settings
  versioning:
    enabled: true
    # Maximum versions to retain (0 = unlimited)
    max_versions: 10

  # Soft delete settings
  soft_delete:
    enabled: true
    # Days to retain deleted secrets before permanent removal
    retention_days: 30

  # Access control
  access:
    # Require authentication for all operations
    require_auth: true
    # Default policy for unauthenticated access
    default_deny: true
```

## Backend Configuration

### Software Backend (Default)

```yaml
secrets:
  encryption:
    backend: software
    software:
      # Directory for key storage
      key_dir: /var/lib/quicbaas/keys
      # Key file permissions (octal)
      key_perms: 0600
```

### TPM2 Backend

```yaml
secrets:
  encryption:
    backend: tpm2
    tpm2:
      # TPM device path
      device: /dev/tpmrm0
      # PCR banks to use for sealing
      pcr_banks: [0, 7, 9]
      # Owner auth password (optional)
      owner_auth: ""
```

### PKCS#11 Backend (HSM)

```yaml
secrets:
  encryption:
    backend: pkcs11
    pkcs11:
      # PKCS#11 library path
      library: /usr/lib/softhsm/libsofthsm2.so
      # Token label
      token_label: quicbaas
      # Slot ID (alternative to label)
      slot_id: 0
      # PIN for token access
      pin: "${PKCS11_PIN}"
```

### AWS KMS Backend

```yaml
secrets:
  encryption:
    backend: awskms
    aws_kms:
      # AWS region
      region: us-west-2
      # KMS key ID or ARN
      key_id: arn:aws:kms:us-west-2:123456789:key/abcd1234
      # Optional: assume role ARN
      role_arn: ""
```

### Azure Key Vault Backend

```yaml
secrets:
  encryption:
    backend: azurekv
    azure_kv:
      # Key Vault URL
      vault_url: https://myquicbaas.vault.azure.net
      # Key name
      key_name: quicbaas-secrets
      # Key version (optional, uses latest if empty)
      key_version: ""
```

### GCP Cloud KMS Backend

```yaml
secrets:
  encryption:
    backend: gcpkms
    gcp_kms:
      # Project ID
      project_id: my-project
      # Location
      location: us-west1
      # Key ring name
      key_ring: quicbaas
      # Crypto key name
      crypto_key: secrets-key
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_SECRETS_ENABLED` | Enable secrets engine | `false` |
| `QUICBAAS_SECRETS_KEY_ID` | Default encryption key ID | `quicbaas-secrets-key` |
| `QUICBAAS_SECRETS_ALGORITHM` | Default algorithm | `AES-128-GCM` |
| `PKCS11_PIN` | PKCS#11 token PIN | - |
| `AWS_KMS_KEY_ID` | AWS KMS key ID | - |

## CLI Configuration

### Set Default Key

```bash
quicbaas config set secrets.encryption.default_key_id my-key
```

### Set Default Algorithm

```bash
quicbaas config set secrets.encryption.default_algorithm ChaCha20-Poly1305
```

### View Configuration

```bash
quicbaas config get secrets
```

## Runtime Configuration

### Update via API

```bash
# Update default algorithm
curl -X PUT http://localhost:8080/api/v1/config/secrets \
  -H 'Content-Type: application/json' \
  -d '{
    "encryption": {
      "default_algorithm": "AES-256-GCM"
    }
  }'
```

## Performance Tuning

### High-Throughput Settings

```yaml
secrets:
  # Batch encryption for multiple secrets
  batch:
    enabled: true
    max_batch_size: 100
    flush_interval: 10ms

  # Caching for decrypted secrets (use with caution)
  cache:
    enabled: false  # Default: disabled for security
    ttl: 60s
    max_size: 1000
```

### Low-Latency Settings

```yaml
secrets:
  encryption:
    # Use AES-GCM on systems with AES-NI
    default_algorithm: AES-128-GCM

  # Disable versioning for simple use cases
  versioning:
    enabled: false
```


---

## Configuration Secret Resolution

QuicBaaS provides a framework for resolving secret references in configuration files, enabling secure storage and retrieval of sensitive configuration values.

### Overview

Configuration secrets can be stored in various backends and referenced using URI schemes. This framework provides the interfaces and implementations for resolving these references to their actual values at runtime.

### Supported URI Schemes

| Scheme | Format | Description |
|--------|--------|-------------|
| `kms://` | `kms://path/to/secret` | Local QuicBaaS KMS secrets store |
| `kms://` + `?ensure=true` | `kms://path?ensure=true&len=32&enc=base64` | Local KMS, generated and persisted on first use |
| `remote-kms://` | `remote-kms://host:port/path` | Remote QuicBaaS instance |
| `literal://` | `literal://value` | Force-quote a value that would otherwise look like a reference (owned by go-qrdb) |

#### Dispatch Is By Scheme Only

A resolver claims a value by its URI scheme, never by its content. A value that
carries no registered scheme is **not** a secret reference: it is used verbatim
under `StrategyLiteral`, and `ConfigProcessor` leaves the field untouched.

This means placeholder-looking values such as `123456`, `changeme`, or `auto`
are ordinary literals. Substituting generated entropy for them would silently
corrupt length-constrained fields — a 43-character base64 value written into a
TPM `TPM2B_AUTH` field (32-byte maximum) fails with `TPM_RC_SIZE`.

### Basic Usage

#### In Configuration Files

Reference secrets in your YAML configuration using URI schemes:

```yaml
# quicbaas.yaml
kms:
  backends:
    tpm2:
      srk-auth: kms://config/tpm/srk-auth
    awskms:
      secret-access-key: remote-kms://provisioning.example.com:9443/aws/secret
    yubikey:
      pin: kms://config/yubikey/pin?ensure=true&len=8&enc=numeric  # Minted on first boot, stable thereafter
```

#### Programmatic Usage

```go
import (
    "context"
    "github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
)

// Create a resolver registry
registry := secrets.NewResolverRegistry()

// Register resolvers for each scheme you need
registry.Register(secrets.NewLocalKMSResolver(kmsClient))
registry.Register(secrets.NewRemoteKMSResolver(remoteClient))

// Resolve individual values
value, err := registry.Resolve(ctx, "kms://config/database/password")
if err != nil {
    // handle error
}
fmt.Println(value.Value)     // resolved plaintext
fmt.Println(value.Strategy)  // "local_kms"
```

#### Processing Configuration Structs

Use `ConfigProcessor` to walk a configuration struct and resolve all secret references:

```go
type DatabaseConfig struct {
    Host     string `yaml:"host"`
    Port     int    `yaml:"port"`
    Password string `yaml:"password"`  // kms://secrets/db/password
}

type Config struct {
    Database DatabaseConfig `yaml:"database"`
}

processor := secrets.NewConfigProcessor(registry)
result, err := processor.Process(ctx, &config)
if err != nil {
    // handle error
}

// config.Database.Password now contains the resolved plaintext
for _, resolved := range result.Resolved {
    fmt.Printf("Resolved %s using %s\n", resolved.FieldPath, resolved.Strategy)
}
```

### Generate-On-First-Use

A secret that has not been provisioned yet is expressed with the `ensure` form
of the `kms://` scheme. The first resolution mints the value and persists it via
`SecretsService.EnsureSecret`; every later resolution returns the **same** bytes,
so the secret can be bound to a config field across reboots.

| Parameter | Values | Default | Description |
|-----------|--------|---------|-------------|
| `ensure` | boolean | `false` | Generate and store the secret when it is absent |
| `len` | positive integer | `32` | Entropy length: bytes for `base64`/`hex`/`raw`, digits for `numeric` |
| `enc` | `base64`, `hex`, `numeric`, `raw` | `base64` | Output encoding |

The query string is not part of the secret identity; only the path is.

Example:

```yaml
kms:
  backends:
    tpm2:
      srk-auth: kms://config/tpm/srk-auth?ensure=true&len=24&enc=base64
```

A `SecretsClient` that does not implement `SecretsEnsurer` reports a typed
`ResolutionFailedError` for an `ensure=true` URI rather than degrading to a
plain read that is guaranteed to fail on first boot.

### Resolver Types

#### LocalKMSResolver

Resolves `kms://` URIs from the local QuicBaaS secrets store:

```go
resolver := secrets.NewLocalKMSResolver(kmsClient)
registry.Register(resolver)

// kms://path/to/secret -> calls kmsClient.GetSecret(ctx, "path/to/secret")
```

The `SecretsClient` interface must be implemented by your KMS client:

```go
type SecretsClient interface {
    // GetSecret retrieves a secret by its ID/path from the local KMS.
    // Returns the secret data as bytes, or an error if the secret cannot be retrieved.
    GetSecret(ctx context.Context, secretID string) ([]byte, error)
}
```

#### RemoteKMSResolver

Resolves `remote-kms://` URIs from another QuicBaaS instance:

```go
// Create resolver with functional options
resolver := secrets.NewRemoteKMSResolver(
    remoteClient,
    secrets.WithTimeout(30*time.Second),
)
registry.Register(resolver)

// remote-kms://provisioning.example.com:9443/aws/credentials
// -> connects to provisioning.example.com:9443
// -> calls GetSecret("aws/credentials")
```

The `RemoteSecretsClient` interface must be implemented by your remote client:

```go
type RemoteSecretsClient interface {
    // GetSecret retrieves a secret from the remote QuicBaaS instance.
    // The host parameter is the host:port of the remote instance.
    // The secretID parameter is the path to the secret.
    GetSecret(ctx context.Context, host string, secretID string) ([]byte, error)
}
```

Default timeout is 30 seconds. Use the `WithTimeout` option to customize.

The `SecretsEnsurer` interface adds generate-on-first-use support to a
`SecretsClient`, backing the `?ensure=true` form:

```go
type SecretsEnsurer interface {
    // EnsureSecret returns the secret at secretID, generating and persisting it
    // with the requested length/encoding when it does not yet exist.
    EnsureSecret(ctx context.Context, secretID string, length int, encoding string) ([]byte, error)
}
```

### Error Handling

All errors in this package use typed error structures. Use helper functions to check error types:

```go
value, err := registry.Resolve(ctx, uri)
if err != nil {
    if secrets.IsSecretNotFound(err) {
        // Secret doesn't exist in the backend
        log.Printf("Secret not found: %s", uri)
    }
    if secrets.IsRemoteConnectionFailed(err) {
        // Couldn't connect to remote KMS
        log.Printf("Connection failed: %v", err)
    }
    if secrets.IsInvalidSecretURI(err) {
        // URI format is invalid
        log.Printf("Invalid URI: %s", uri)
    }
    return err
}
```

#### Error Types

| Error Type | Description |
|------------|-------------|
| `SecretNotFoundError` | Secret path doesn't exist in backend |
| `ResolutionFailedError` | Resolution failed for other reasons |
| `RemoteConnectionFailedError` | Connection to remote KMS failed |
| `InvalidSecretURIError` | URI format is malformed |
| `EntropyGenerationFailedError` | Random generation failed |
| `UnsupportedSchemeError` | No resolver handles the scheme |

### Implementing Custom Resolvers

Implement the `SecretResolver` interface to add support for additional backends:

```go
type VaultResolver struct {
    client *vault.Client
}

func (r *VaultResolver) Scheme() string {
    return "vault"
}

func (r *VaultResolver) CanResolve(raw string) bool {
    return strings.HasPrefix(raw, "vault://")
}

func (r *VaultResolver) Resolve(ctx context.Context, raw string) (*secrets.SecretValue, error) {
    // Parse URI: vault://path/to/secret
    path := strings.TrimPrefix(raw, "vault://")

    secret, err := r.client.Logical().Read(path)
    if err != nil {
        return nil, secrets.NewResolutionFailedError("VaultResolver.Resolve", raw, "read failed", err)
    }

    return &secrets.SecretValue{
        Raw:      raw,
        Value:    secret.Data["value"].(string),
        Strategy: secrets.StrategyLiteral, // Or define your own
    }, nil
}
```

### Thread Safety

The `ResolverRegistry` is thread-safe and can be used concurrently from multiple goroutines. Individual resolver implementations are also designed to be thread-safe.

### Performance Considerations

- Secrets are resolved at configuration load time, not on every access
- The `ConfigProcessor` walks structs using reflection, which has some overhead
- For high-performance scenarios, cache resolved configuration structs
- Remote KMS calls include network latency; consider connection pooling

### Security Considerations

1. **Memory Safety**: Resolved secret values are held in memory; consider zeroing after use
2. **Logging**: Never log resolved secret values; log paths/URIs only
3. **Transport Security**: `remote-kms://` should only be used over TLS
4. **Access Control**: KMS backends should enforce proper authorization
5. **Generation**: Generated secrets must be persisted (`kms://...?ensure=true`); a value regenerated per boot cannot be bound to a config field
6. **No Content Matching**: Resolvers dispatch by scheme only; a scheme-less value is a literal and is never replaced

### Integration with go-trusted-platform

The secrets resolution framework is designed for seamless integration with go-trusted-platform and other projects. Here is a complete example of setting up the resolver registry with all available resolvers:

```go
import (
    "context"
    "log"
    "time"

    "github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
)

// SetupSecretsResolution creates a fully configured resolver registry
// with all available resolvers for use in go-trusted-platform.
func SetupSecretsResolution(kmsClient secrets.SecretsClient, remoteClient secrets.RemoteSecretsClient) (*secrets.ResolverRegistry, *secrets.ConfigProcessor) {
    // Create the resolver registry
    registry := secrets.NewResolverRegistry()

    // Register local KMS resolver (handles kms:// URIs, including ?ensure=true)
    if kmsClient != nil {
        localResolver := secrets.NewLocalKMSResolver(kmsClient)
        registry.Register(localResolver)
    }

    // Register remote KMS resolver (handles remote-kms:// URIs)
    if remoteClient != nil {
        remoteResolver := secrets.NewRemoteKMSResolver(
            remoteClient,
            secrets.WithTimeout(30*time.Second),
        )
        registry.Register(remoteResolver)
    }

    // Create the config processor
    processor := secrets.NewConfigProcessor(registry)

    return registry, processor
}

// ProcessConfiguration resolves all secrets in a configuration struct.
func ProcessConfiguration(ctx context.Context, processor *secrets.ConfigProcessor, config interface{}) error {
    result, err := processor.Process(ctx, config)
    if err != nil {
        return err
    }

    // Log resolution summary (never log actual secret values)
    for _, resolved := range result.Resolved {
        log.Printf("Resolved secret at %s using strategy %s", resolved.FieldPath, resolved.Strategy)
    }

    // Handle any unresolved secrets
    if len(result.Unresolved) > 0 {
        log.Printf("Warning: %d secrets could not be resolved", len(result.Unresolved))
        for _, path := range result.Unresolved {
            log.Printf("  - %s", path)
        }
    }

    return nil
}
```

#### Complete Configuration Example

```go
// Example configuration struct with secrets
type PlatformConfig struct {
    KMS KMSConfig `yaml:"kms"`
    Database DatabaseConfig `yaml:"database"`
}

type KMSConfig struct {
    Backends BackendsConfig `yaml:"backends"`
}

type BackendsConfig struct {
    TPM2 TPM2Config `yaml:"tpm2"`
    AWS AWSConfig `yaml:"awskms"`
    YubiKey YubiKeyConfig `yaml:"yubikey"`
}

type TPM2Config struct {
    SRKAuth string `yaml:"srk-auth"` // kms://config/tpm/srk-auth
}

type AWSConfig struct {
    SecretAccessKey string `yaml:"secret-access-key"` // remote-kms://provisioning.example.com:9443/aws/secret
}

type YubiKeyConfig struct {
    PIN string `yaml:"pin"` // auto (will be generated)
}

type DatabaseConfig struct {
    Host string `yaml:"host"`
    Port int `yaml:"port"`
    Password string `yaml:"password"` // kms://secrets/db/password
}

func main() {
    ctx := context.Background()

    // Load YAML config (secrets are still URI references at this point)
    config := &PlatformConfig{
        KMS: KMSConfig{
            Backends: BackendsConfig{
                TPM2: TPM2Config{
                    SRKAuth: "kms://config/tpm/srk-auth",
                },
                AWS: AWSConfig{
                    SecretAccessKey: "remote-kms://provisioning.example.com:9443/aws/secret",
                },
                YubiKey: YubiKeyConfig{
                    PIN: "auto", // Will generate a secure PIN
                },
            },
        },
        Database: DatabaseConfig{
            Host: "localhost",
            Port: 5432,
            Password: "kms://secrets/db/password",
        },
    }

    // Setup resolvers (kmsClient and remoteClient would be real implementations)
    registry, processor := SetupSecretsResolution(kmsClient, remoteClient)

    // Process the config - all secrets are now resolved to plaintext
    if err := ProcessConfiguration(ctx, processor, config); err != nil {
        log.Fatalf("Failed to process configuration: %v", err)
    }

    // config.KMS.Backends.TPM2.SRKAuth now contains the actual secret
    // config.KMS.Backends.YubiKey.PIN now contains a generated secure PIN
    // config.Database.Password now contains the database password
}
```

### Multi-Protocol Testing

The secrets resolution framework is tested across all supported protocols to ensure consistent behavior. Integration tests are located in `test/integration/secrets/` and follow the multi-protocol testing pattern.

#### Test Structure

```
test/integration/secrets/
    main_test.go              # Test setup and fixtures
    secrets_integration_test.go # Core integration tests
    multiprotocol_test.go     # Multi-protocol test suite
    mcp_test.go               # MCP-specific tests
    mcp_comprehensive_test.go # Comprehensive MCP coverage
    isolation_test.go         # Tenant isolation tests
    helpers.go                # Test utilities
```

#### Running Multi-Protocol Tests

Execute integration tests using the provided Makefile targets:

```bash
# Run all secrets integration tests
make integration-test-secrets

# Run with verbose output
make integration-test-secrets VERBOSE=1
```

#### Protocol Adapter Pattern

The integration tests use a protocol adapter pattern to ensure consistent behavior across REST, gRPC, QUIC, Unix Socket, and MCP protocols:

```go
//go:build integration

func TestSecretsMultiProtocol(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    t.Run("REST", func(t *testing.T) {
        testSecretsProtocol(t, ctx, &restProtocolClient{})
    })

    t.Run("gRPC", func(t *testing.T) {
        testSecretsProtocol(t, ctx, &grpcProtocolClient{})
    })

    t.Run("QUIC", func(t *testing.T) {
        testSecretsProtocol(t, ctx, &quicProtocolClient{})
    })

    t.Run("UnixSocket", func(t *testing.T) {
        testSecretsProtocol(t, ctx, &unixSocketProtocolClient{})
    })
}
```

Each protocol adapter implements a common interface:

```go
type secretsProtocol interface {
    Setup(t *testing.T, ctx context.Context) error
    Teardown(t *testing.T) error
    StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
    GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
    ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
    DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
    ListVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
    Rollback(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
    Rotate(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
    UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error)
    Name() string
}
```

#### Test Coverage

The multi-protocol tests verify:

- **Store operations**: All flag combinations (tenant-id, key-id, algorithm, metadata)
- **Get operations**: Latest version, specific versions, error handling
- **List operations**: Pagination, filtering, prefix matching
- **Delete operations**: Soft delete and permanent destroy
- **Version management**: List versions, rollback, rotation
- **Status management**: Enable/disable secrets
- **Data integrity**: Complex data structures, timestamps
- **Tenant isolation**: Secret separation between tenants

### See Also

- [Secrets Engine README](user.md) - Main secrets documentation
- [API Reference](api.md) - REST and gRPC API documentation
- [CLI Usage](cli.md) - Command-line interface guide
- [KMS Documentation](../kms/user.md) - Key Management Service


---

## Secrets

Versioned storage for sensitive data like passwords, API keys, and certificates.

### Status

Complete - Full implementation with CLI commands, gRPC/REST APIs, and service layer.

### Overview

QuicBaaS Secrets provides:
- Versioned secret storage
- Encryption at rest using KMS
- Fine-grained access control
- Secret rotation support
- Metadata and tagging
- Rollback capabilities

### CLI Usage

```bash
# Store secret
quicbaas secrets put database/prod --data password=secret123

# Store multiple values
quicbaas secrets put app/config \
  --data username=admin \
  --data password=secret \
  --data host=localhost

# Store with TTL
quicbaas secrets put temp/token --data token=abc123 --ttl 1h

# Get secret
quicbaas secrets get database/prod
quicbaas secrets get database/prod --version 2

# List secrets
quicbaas secrets list
quicbaas secrets list --prefix database/

# Delete secret (soft delete)
quicbaas secrets delete database/prod

# Permanently delete
quicbaas secrets delete database/prod --permanent

# List versions
quicbaas secrets versions database/prod

# Rollback to previous version
quicbaas secrets rollback database/prod --version 2
```

### Secret Structure

```json
{
  "secret_id": "database/prod",
  "version": 3,
  "data": {
    "password": "secret123",
    "username": "admin"
  },
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-20T14:45:00Z",
  "expires_at": null
}
```

### Path Conventions

Use hierarchical structure:

```
<application>/<environment>/<category>/<name>

Examples:
  myapp/production/database/postgres-password
  myapp/staging/api/stripe-key
  shared/certificates/tls-cert
  team-a/dev/aws/access-key
```

### Security Features

1. **Encryption at Rest**: All secrets encrypted using KMS
2. **Versioning**: Full version history maintained
3. **Access Control**: Path-based permissions
4. **Audit Logging**: All access logged
5. **TTL Support**: Auto-expiration of temporary secrets
6. **Soft Delete**: Accidental deletions can be recovered

### Go SDK

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/service"

// Store secret
resp, err := secretsService.Put(ctx, &service.StoreSecretRequest{
    SecretID: "database/prod",
    Data: map[string]interface{}{
        "password": "secret123",
        "username": "admin",
    },
})

// Get secret
getResp, err := secretsService.Get(ctx, &service.GetSecretRequest{
    SecretID: "database/prod",
})

// List secrets
listResp, err := secretsService.List(ctx, &service.ListSecretsRequest{
    Prefix: "database/",
})

// Delete secret
err = secretsService.Delete(ctx, &service.DeleteSecretRequest{
    SecretID: "database/prod",
})
```

### Configuration

```yaml
secrets:
  backend: encrypted  # memory, encrypted

  encryption:
    key_id: secrets-encryption-key
    algorithm: AES-256-GCM

  versioning:
    max_versions: 10
    auto_delete_old: false

  access:
    default_policy: deny
    audit_enabled: true
```

### Best Practices

1. **Least Privilege**: Grant minimal access to secrets
2. **Rotation**: Regularly rotate sensitive secrets
3. **Versioning**: Keep version history for rollback
4. **Audit**: Enable audit logging for compliance
5. **Encryption**: Always use encrypted backend in production
6. **Naming**: Use consistent hierarchical path structure
7. **TTL**: Set expiration for temporary credentials

### See Also

- KMS - Encryption key management
- Crypto - Encryption as a Service
- [Security Overview](user.md)


---

## QuicBaaS Kubernetes Integration

QuicBaaS provides a simple, cross-platform secrets solution that works with Kubernetes and any other container runtime.

### Overview

| Component | Description | Status |
|-----------|-------------|--------|
| **Service Discovery** | Kubernetes-native node discovery | ✅ Implemented |
| **Secrets Fetch CLI** | Init container/sidecar for secret injection | ✅ Implemented |
| **Helm Chart** | Kubernetes deployment templates | ✅ Implemented |
| **Path Encryption** | Transparent encryption at rest | ✅ Implemented |

### Secrets Integration

QuicBaaS uses a simple CLI-based approach that works identically across Docker, Podman, and Kubernetes:

```yaml
initContainers:
  - name: fetch-secrets
    image: quicbaas/quicbaas:latest
    command:
      - quicbaas
      - secrets
      - fetch
      - apps/myapp/
      - --output-dir
      - /secrets
    env:
      - name: QUICBAAS_TOKEN
        valueFrom:
          secretKeyRef:
            name: quicbaas-credentials
            key: token
    volumeMounts:
      - name: secrets
        mountPath: /secrets

containers:
  - name: myapp
    volumeMounts:
      - name: secrets
        mountPath: /secrets
        readOnly: true

volumes:
  - name: secrets
    emptyDir:
      medium: Memory  # RAM-backed for security
```

See Container Secrets Integration for complete documentation.

### Kubernetes Service Discovery

QuicBaaS can discover cluster nodes using Kubernetes APIs:

```yaml
# quicbaas.yaml
discovery:
  type: kubernetes
  kubernetes:
    namespace: quicbaas
    service_name: quicbaas-headless
    port_name: raft
    label_selector: "app=quicbaas"
```

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/discovery"

disc := discovery.NewKubernetesDiscovery(config, logger)
nodes, err := disc.Discover(ctx)
```

See `pkg/discovery/kubernetes.go` for the complete implementation.

### Deployment

#### Helm Chart (Recommended)

```bash
# Install QuicBaaS secrets integration
helm install quicbaas-secrets ./deployments/helm/quicbaas-k8s-secrets \
  --set quicbaas.endpoint=quicbaas.default.svc:9090 \
  --set secrets.prefix=apps/myapp/
```

#### StatefulSet

QuicBaaS should be deployed as a StatefulSet for stable network identities:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: quicbaas
spec:
  serviceName: quicbaas-headless
  replicas: 3
  selector:
    matchLabels:
      app: quicbaas
  template:
    spec:
      containers:
      - name: quicbaas
        image: quicbaas/quicbaas:latest
        ports:
        - containerPort: 2379  # Client
        - containerPort: 2380  # Peer
        - containerPort: 8080  # REST API
        - containerPort: 9090  # gRPC
```

### Configuration

#### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_TOKEN` | Authentication token | Required |
| `QUICBAAS_K8S_NAMESPACE` | Kubernetes namespace | `default` |
| `QUICBAAS_K8S_SERVICE_ACCOUNT` | Service account for auth | `quicbaas` |
| `QUICBAAS_K8S_DISCOVERY_ENABLED` | Enable K8s discovery | `true` |

#### RBAC Requirements

QuicBaaS needs minimal RBAC permissions for discovery:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: quicbaas-discovery
rules:
- apiGroups: [""]
  resources: ["endpoints", "pods"]
  verbs: ["get", "list", "watch"]
```

### Security Considerations

1. **Memory-backed volumes**: Use `emptyDir` with `medium: Memory` for secrets
2. **Service Account Tokens**: Use projected volumes for short-lived tokens
3. **Network Policies**: Restrict cluster communication to QuicBaaS namespace
4. **Pod Security**: Run QuicBaaS with non-root user
5. **Path Encryption**: Enable path-based encryption for secrets at rest

### Related Documentation

- Container Secrets Integration
- [Dynamic Secrets](architecture.md)
- [Security Best Practices](../security-hardening/user.md)
