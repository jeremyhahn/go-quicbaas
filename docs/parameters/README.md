# Parameter Store

Hierarchical, multi-tenant parameter management with optional encryption, TTL, and tag-based organization.

## Overview

QuicBaaS's Parameter Store provides a centralized configuration management system similar to AWS Systems Manager Parameter Store. It enables applications to store and retrieve configuration values, feature flags, connection strings, and other application parameters using hierarchical paths.

Parameters are organized in a tree-like namespace (e.g., `/app/database/host`) and support multi-tenancy, optional KMS encryption, time-to-live (TTL) expiration, and tag-based organization for flexible access patterns.

The Parameter Store integrates with QuicBaaS's IAM system, providing fine-grained RBAC and ABAC access control for secure parameter management across teams and environments.

## Key Features

| Feature | Description |
|---------|-------------|
| **Hierarchical Paths** | Organize parameters in tree structures like `/app/prod/database/host` |
| **Multi-Tenancy** | Tenant-scoped isolation for secure multi-tenant deployments |
| **Optional Encryption** | KMS envelope encryption for sensitive configuration values |
| **TTL Support** | Automatic expiration for time-sensitive parameters |
| **Tags & Labels** | Key-value tags for organization and tag-based search |
| **Batch Operations** | Efficient batch get, set, and delete operations |
| **RBAC/ABAC** | Fine-grained access control with path-based and tag-based policies |
| **Audit Logging** | Optional per-parameter audit logging |
| **Multiple Protocols** | REST, gRPC, Unix IPC, QUIC, and MCP access |
| **Type Support** | String, number, JSON, and binary parameter types |

## Quick Start

### Store a Parameter (CLI)

```bash
# Store a simple string parameter
quicbaas parameter set /app/database/host "db.example.com"

# Store with encryption
quicbaas parameter set /app/database/password "secret123" \
  --kms-key default \
  --type string

# Store with tags and TTL
quicbaas parameter set /app/cache/url "redis://localhost:6379" \
  --tags env=prod,service=cache \
  --ttl 3600

# Store JSON configuration
quicbaas parameter set /app/config/settings '{"debug": false, "timeout": 30}' \
  --type json
```

### Store a Parameter (REST API)

```bash
# Store a parameter
curl -X PUT "http://localhost:8080/api/v1/parameters/app/database/host" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "value": "ZGIuZXhhbXBsZS5jb20=",
    "type": "string",
    "description": "Production database host",
    "tags": {"env": "prod", "service": "database"}
  }'

# Store encrypted parameter
curl -X PUT "http://localhost:8080/api/v1/parameters/app/database/password" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "value": "c2VjcmV0MTIz",
    "type": "string",
    "kms_key_id": "default",
    "description": "Database password",
    "audit_enabled": true
  }'
```

### Retrieve Parameters

```bash
# Get a single parameter (CLI)
quicbaas parameter get /app/database/host

# Get a parameter (REST)
curl "http://localhost:8080/api/v1/parameters/app/database/host" \
  -H "Authorization: Bearer $TOKEN"

# List parameters by prefix
curl "http://localhost:8080/api/v1/parameters?prefix=/app/database/&recursive=true" \
  -H "Authorization: Bearer $TOKEN"

# Search by tags
curl -X POST "http://localhost:8080/api/v1/parameters/search" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tags": {"env": "prod", "service": "database"}}'
```

### Delete Parameters

```bash
# Delete single parameter
quicbaas parameter delete /app/database/host

# REST API
curl -X DELETE "http://localhost:8080/api/v1/parameters/app/database/host" \
  -H "Authorization: Bearer $TOKEN"
```

## Batch Operations

Batch operations provide efficient handling of multiple parameters in a single request.

### Batch Get

```bash
curl -X POST "http://localhost:8080/api/v1/parameters/batch/get" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "paths": [
      "/app/database/host",
      "/app/database/port",
      "/app/database/name"
    ],
    "with_decryption": true
  }'
```

### Batch Set

```bash
curl -X POST "http://localhost:8080/api/v1/parameters/batch/set" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "parameters": [
      {"path": "/app/database/host", "value": "ZGIuZXhhbXBsZS5jb20=", "type": "string"},
      {"path": "/app/database/port", "value": "NTQzMg==", "type": "number"},
      {"path": "/app/database/name", "value": "bXlhcHA=", "type": "string"}
    ]
  }'
```

### Batch Delete

```bash
curl -X POST "http://localhost:8080/api/v1/parameters/batch/delete" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "paths": [
      "/app/database/host",
      "/app/database/port"
    ]
  }'
```

## Path Conventions

Parameters use hierarchical paths similar to filesystem paths:

- Paths must start with `/`
- Paths cannot end with `/`
- Paths cannot contain `//`
- Use meaningful hierarchies: `/{app}/{environment}/{component}/{key}`

**Recommended patterns:**

```
/app/prod/database/host           # Application-specific config
/shared/certificates/tls-cert     # Shared resources
/tenant/{tenant-id}/config/...    # Tenant-specific config
/feature-flags/new-ui-enabled     # Feature flags
```

## Access Control

### RBAC Roles

| Role | Permissions |
|------|-------------|
| `parameters-reader` | Read parameters |
| `parameters-writer` | Read and write parameters |
| `parameters-admin` | Full access including delete and admin operations |

### ABAC Policies

Fine-grained access control based on parameter attributes:

```yaml
# Path-based access - restrict production writes
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: restrict-production-writes
spec:
  subjects:
    authenticated: true
  rules:
    - name: deny-prod-writes
      effect: deny
      actions: [parameter:write, parameter:delete]
      resources:
        - parameters:/*/prod/**
      unless:
        - subject.roles contains prod-admin

# Tag-based access - environment restriction
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: environment-access
spec:
  subjects:
    authenticated: true
  rules:
    - name: match-environment
      effect: allow
      actions: [parameter:read]
      resources:
        - parameters:**
      when:
        - context.parameter_tags.env == subject.attrs.allowed_env
```

## Encryption

Parameters can be encrypted using KMS envelope encryption:

```bash
# Store encrypted parameter
quicbaas parameter set /app/secrets/api-key "my-secret-key" \
  --kms-key default \
  --audit-enabled

# Retrieve decrypted (default)
quicbaas parameter get /app/secrets/api-key

# Retrieve without decryption
quicbaas parameter get /app/secrets/api-key --no-decrypt
```

Encrypted parameters store the ciphertext with metadata about the encryption key, enabling automatic decryption on retrieval when `with_decryption=true` (default).

## Configuration

```yaml
# quicbaas.yaml
parameter_store:
  enabled: true

  # Default TTL for parameters without explicit TTL (0 = no expiration)
  default_ttl_seconds: 0

  # Maximum parameter value size in bytes
  max_value_size: 65536

  # Enable audit logging by default for new parameters
  default_audit_enabled: false

  # KMS key to use for encrypted parameters
  default_kms_key: "default"
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_PARAMETER_STORE_ENABLED` | Enable parameter store service | `true` |
| `QUICBAAS_PARAMETER_STORE_DEFAULT_TTL` | Default TTL in seconds | `0` |
| `QUICBAAS_PARAMETER_STORE_MAX_VALUE_SIZE` | Maximum value size | `65536` |
| `QUICBAAS_PARAMETER_STORE_DEFAULT_AUDIT` | Default audit logging | `false` |

## Performance

| Operation | P50 | P99 | Notes |
|-----------|-----|-----|-------|
| Get | < 0.5ms | < 1ms | Single parameter retrieval |
| Set | < 2ms | < 5ms | Without encryption |
| Set (encrypted) | < 5ms | < 10ms | With KMS encryption |
| List (100 items) | < 5ms | < 10ms | Prefix listing |
| Batch Get (100) | < 10ms | < 25ms | Parallel retrieval |
| Batch Set (100) | < 50ms | < 100ms | Atomic batch write |

## Related Documentation

- [Architecture](architecture.md) - Storage design and data flow
- [API Reference](api-reference.md) - Complete REST and gRPC API documentation
- [CLI Usage](cli-usage.md) - Command-line interface reference
- [Access Control](access-control.md) - RBAC roles and ABAC policy examples

## See Also

- [KMS Documentation](../kms/README.md) - Key management for encryption
- [IAM Documentation](../iam/README.md) - Identity and access management
- [Secrets Documentation](../secrets/README.md) - Secrets management (for sensitive values)
