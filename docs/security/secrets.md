# Secrets

Versioned storage for sensitive data like passwords, API keys, and certificates.

## Status

Complete - Full implementation with CLI commands, gRPC/REST APIs, and service layer.

## Overview

QuicBaaS Secrets provides:
- Versioned secret storage
- Encryption at rest using KMS
- Fine-grained access control
- Secret rotation support
- Metadata and tagging
- Rollback capabilities

## CLI Usage

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

## Secret Structure

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

## Path Conventions

Use hierarchical structure:

```
<application>/<environment>/<category>/<name>

Examples:
  myapp/production/database/postgres-password
  myapp/staging/api/stripe-key
  shared/certificates/tls-cert
  team-a/dev/aws/access-key
```

## Security Features

1. **Encryption at Rest**: All secrets encrypted using KMS
2. **Versioning**: Full version history maintained
3. **Access Control**: Path-based permissions
4. **Audit Logging**: All access logged
5. **TTL Support**: Auto-expiration of temporary secrets
6. **Soft Delete**: Accidental deletions can be recovered

## Go SDK

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

## Configuration

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

## Best Practices

1. **Least Privilege**: Grant minimal access to secrets
2. **Rotation**: Regularly rotate sensitive secrets
3. **Versioning**: Keep version history for rollback
4. **Audit**: Enable audit logging for compliance
5. **Encryption**: Always use encrypted backend in production
6. **Naming**: Use consistent hierarchical path structure
7. **TTL**: Set expiration for temporary credentials

## See Also

- [KMS](kms.md) - Encryption key management
- [Crypto](crypto.md) - Encryption as a Service
- [Security Overview](README.md)
