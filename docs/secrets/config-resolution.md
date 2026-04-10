# Configuration Secret Resolution

QuicBaaS provides a framework for resolving secret references in configuration files, enabling secure storage and retrieval of sensitive configuration values.

## Overview

Configuration secrets can be stored in various backends and referenced using URI schemes. This framework provides the interfaces and implementations for resolving these references to their actual values at runtime.

## Supported URI Schemes

| Scheme | Format | Description |
|--------|--------|-------------|
| `kms://` | `kms://path/to/secret` | Local QuicBaaS KMS secrets store |
| `remote-kms://` | `remote-kms://host:port/path` | Remote QuicBaaS instance |
| Auto-generation | Literal placeholder values | Auto-generate cryptographic secrets |

### Auto-Generation Placeholders

The following placeholder values trigger automatic secret generation:

| Placeholder | Case Sensitive | Description |
|-------------|----------------|-------------|
| `auto` | No | Explicit auto-generation trigger |
| `123456` | N/A | Common weak password placeholder |
| `changeme` | No | Common placeholder indicating value needs replacement |
| `CHANGEME` | No | Upper case variant |
| `placeholder` | No | Explicit placeholder indicator |

Placeholder matching is case-insensitive and whitespace is trimmed before comparison.

## Basic Usage

### In Configuration Files

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
      pin: auto  # Will be auto-generated on first bootstrap
```

### Programmatic Usage

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
registry.Register(secrets.NewAutoGenResolver())

// Resolve individual values
value, err := registry.Resolve(ctx, "kms://config/database/password")
if err != nil {
    // handle error
}
fmt.Println(value.Value)     // resolved plaintext
fmt.Println(value.Strategy)  // "local_kms"
```

### Processing Configuration Structs

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

## Auto-Generation

The `AutoGenResolver` generates cryptographically secure secrets when configuration values match certain placeholders:

| Placeholder | Description |
|-------------|-------------|
| `auto` | Explicit auto-generation trigger |
| `123456` | Common weak password placeholder |
| `changeme` | Common placeholder |
| `CHANGEME` | Upper case variant |
| `placeholder` | Explicit placeholder |

Generated secrets are:
- 32 bytes (256 bits) of cryptographically random data
- URL-safe base64 encoded (no padding)
- Unique per generation (not cached)

Example:

```yaml
kms:
  backends:
    tpm2:
      srk-auth: auto  # Will generate: "k7Hj9mQx2Bv5Nf8Wd3Ry6Tp0Ls4Uc1Xz"
```

## Resolver Types

### LocalKMSResolver

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

### RemoteKMSResolver

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

### AutoGenResolver

Generates high-entropy secrets for placeholder values:

```go
resolver := secrets.NewAutoGenResolver()
resolver = resolver.WithSecretSize(64)  // 64 bytes instead of default 32
registry.Register(resolver)
```

## Error Handling

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

### Error Types

| Error Type | Description |
|------------|-------------|
| `SecretNotFoundError` | Secret path doesn't exist in backend |
| `ResolutionFailedError` | Resolution failed for other reasons |
| `RemoteConnectionFailedError` | Connection to remote KMS failed |
| `InvalidSecretURIError` | URI format is malformed |
| `EntropyGenerationFailedError` | Random generation failed |
| `UnsupportedSchemeError` | No resolver handles the scheme |

## Implementing Custom Resolvers

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

## Thread Safety

The `ResolverRegistry` is thread-safe and can be used concurrently from multiple goroutines. Individual resolver implementations are also designed to be thread-safe.

## Performance Considerations

- Secrets are resolved at configuration load time, not on every access
- The `ConfigProcessor` walks structs using reflection, which has some overhead
- For high-performance scenarios, cache resolved configuration structs
- Remote KMS calls include network latency; consider connection pooling

## Security Considerations

1. **Memory Safety**: Resolved secret values are held in memory; consider zeroing after use
2. **Logging**: Never log resolved secret values; log paths/URIs only
3. **Transport Security**: `remote-kms://` should only be used over TLS
4. **Access Control**: KMS backends should enforce proper authorization
5. **Auto-Generation**: Auto-generated secrets should be persisted to KMS after generation

## Integration with go-trusted-platform

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

    // Register auto-generation resolver (handles placeholder values)
    autoResolver := secrets.NewAutoGenResolver()
    registry.Register(autoResolver)

    // Register local KMS resolver (handles kms:// URIs)
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

### Complete Configuration Example

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

## Multi-Protocol Testing

The secrets resolution framework is tested across all supported protocols to ensure consistent behavior. Integration tests are located in `test/integration/secrets/` and follow the multi-protocol testing pattern.

### Test Structure

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

### Running Multi-Protocol Tests

Execute integration tests using the provided Makefile targets:

```bash
# Run all secrets integration tests
make integration-test-secrets

# Run with verbose output
make integration-test-secrets VERBOSE=1
```

### Protocol Adapter Pattern

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

### Test Coverage

The multi-protocol tests verify:

- **Store operations**: All flag combinations (tenant-id, key-id, algorithm, metadata)
- **Get operations**: Latest version, specific versions, error handling
- **List operations**: Pagination, filtering, prefix matching
- **Delete operations**: Soft delete and permanent destroy
- **Version management**: List versions, rollback, rotation
- **Status management**: Enable/disable secrets
- **Data integrity**: Complex data structures, timestamps
- **Tenant isolation**: Secret separation between tenants

## See Also

- [Secrets Engine README](README.md) - Main secrets documentation
- [API Reference](api-reference.md) - REST and gRPC API documentation
- [CLI Usage](cli-usage.md) - Command-line interface guide
- [KMS Documentation](../kms/README.md) - Key Management Service
