# Parameter Store Architecture

This document describes the internal architecture and design of QuicBaaS's Parameter Store service.

## Overview

The Parameter Store is implemented as a service layer component that provides hierarchical parameter management with support for encryption, TTL, tagging, and multi-tenancy.

```
┌─────────────────────────────────────────────────────────────────┐
│                        Protocol Layer                           │
│    REST API   │   gRPC   │  Unix IPC  │  QUIC  │    MCP        │
└───────────────┴──────────┴────────────┴────────┴───────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Authorization Layer                          │
│              RBAC Provider  │  ABAC Provider                    │
│                    ParameterContext extraction                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                 ParameterStoreService                           │
│  • Request validation                                           │
│  • Metadata management                                          │
│  • ABAC context injection                                       │
│  • Metrics collection                                           │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                   ParameterManager                              │
│  • Storage operations                                           │
│  • KMS encryption/decryption                                    │
│  • TTL enforcement                                              │
│  • Tag indexing                                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Storage Layer                               │
│                   PebbleDB + Raft                               │
└─────────────────────────────────────────────────────────────────┘
```

## Storage Design

### Key Format

Parameters are stored using a tenant-scoped key format:

```
__t:{tenant_id}:parameters:{normalized_path}
```

**Examples:**
- `__t:acme-corp:parameters:/app/database/host`
- `__t:default:parameters:/shared/config/timeout`

### Value Format

Parameter values are stored as Protocol Buffer encoded messages containing:

```protobuf
message StoredParameter {
  bytes value = 1;              // Raw value or ciphertext
  string type = 2;              // string, number, json, binary
  string description = 3;
  map<string, string> tags = 4;
  string kms_key_id = 5;
  bool is_encrypted = 6;
  int64 ttl_seconds = 7;
  google.protobuf.Timestamp expires_at = 8;
  bool audit_enabled = 9;
  google.protobuf.Timestamp created_at = 10;
  google.protobuf.Timestamp updated_at = 11;
  string created_by = 12;
  string updated_by = 13;
}
```

### Indexes

**Path Index** - For prefix listing:
```
__t:{tenant_id}:param_index:path:{path_segment} → [parameter_paths...]
```

**Tag Index** - For tag-based search:
```
__t:{tenant_id}:param_index:tag:{key}:{value} → [parameter_paths...]
```

## Encryption Flow

When a parameter is stored with encryption enabled:

```
                    ┌─────────────────┐
                    │  Plaintext      │
                    │  Parameter      │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  KMS Manager    │
                    │  Envelope       │
                    │  Encryption     │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
       ┌──────────┐   ┌──────────┐   ┌──────────┐
       │  DEK     │   │Encrypted │   │ Metadata │
       │  (wrapped)│  │  Value   │   │  (clear) │
       └──────────┘   └──────────┘   └──────────┘
              │              │              │
              └──────────────┼──────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Storage        │
                    │  (PebbleDB)     │
                    └─────────────────┘
```

**Encryption process:**
1. Generate Data Encryption Key (DEK) using KMS
2. Encrypt parameter value with DEK (AES-256-GCM)
3. Wrap DEK with KMS Master Key
4. Store wrapped DEK + ciphertext + metadata

**Decryption process:**
1. Retrieve stored data from PebbleDB
2. Unwrap DEK using KMS
3. Decrypt ciphertext with unwrapped DEK
4. Return plaintext value

## TTL Enforcement

TTL (Time-To-Live) is enforced using lazy expiration:

1. **On Write**: Calculate `expires_at` from `ttl_seconds`
2. **On Read**: Check if `expires_at < now()`, return `ErrParameterExpired`
3. **Background Cleanup**: Periodic job removes expired parameters

```go
// Lazy expiration check
if metadata.ExpiresAt != nil && time.Now().After(*metadata.ExpiresAt) {
    return nil, ErrParameterExpired{Path: path}
}
```

## Authorization Integration

### ABAC Context

The service injects `ParameterContext` into the request context for ABAC policy evaluation:

```go
type ParameterContext struct {
    Path         string            // e.g., "/app/database/host"
    Type         string            // string, number, json, binary
    IsEncrypted  bool
    Tags         map[string]string
    Owner        string
    TenantID     string
    Operation    string            // get, set, delete, list, search
    BatchSize    int               // For batch operations
    AuditEnabled bool
}
```

### Policy Evaluation

```
Request → Extract JWT → Build Subject
                            │
                            ▼
         Build AuthorizationRequest:
         - Subject (ID, roles, groups, tenant)
         - Resource ("parameters:/app/database/host")
         - Action ("parameter:read")
         - Context (ParameterContext)
                            │
                            ▼
         AuthorizationService.Authorize()
                            │
              ┌─────────────┴─────────────┐
              ▼                           ▼
         RBACProvider                ABACProvider
         (role check)              (policy evaluation)
              │                           │
              │    ParameterContext used  │
              │    for conditions like:   │
              │    - path prefix matching │
              │    - tag-based access     │
              │    - encryption policies  │
              │                           │
              └─────────────┬─────────────┘
                            │
                            ▼
                  AuthorizationDecision
```

## Batch Operations

Batch operations are handled atomically where possible:

### Batch Set

```go
// All parameters in batch are stored in a single Raft transaction
func (m *Manager) StoreBatch(ctx context.Context, entries []*ParameterEntry) error {
    batch := m.storage.NewBatch()
    for _, entry := range entries {
        key := m.buildKey(entry.Path)
        value := m.encodeParameter(entry)
        batch.Set(key, value)
        m.updateIndexes(batch, entry)
    }
    return m.storage.WriteBatch(batch)
}
```

### Batch Get

```go
// Parallel retrieval with goroutines
func (m *Manager) GetBatch(ctx context.Context, paths []string) (map[string]*Result, error) {
    results := make(map[string]*Result, len(paths))
    var wg sync.WaitGroup
    var mu sync.Mutex

    for _, path := range paths {
        wg.Add(1)
        go func(p string) {
            defer wg.Done()
            result, err := m.Get(ctx, p)
            mu.Lock()
            results[p] = &Result{Value: result, Error: err}
            mu.Unlock()
        }(path)
    }

    wg.Wait()
    return results, nil
}
```

## Metrics

The service exposes Prometheus metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_parameters_set_total` | Counter | Parameters created/updated |
| `quicbaas_parameters_get_total` | Counter | Parameters retrieved |
| `quicbaas_parameters_deleted_total` | Counter | Parameters deleted |
| `quicbaas_parameters_listed_total` | Counter | List operations |
| `quicbaas_parameters_bytes_written_total` | Counter | Bytes written |
| `quicbaas_parameters_bytes_read_total` | Counter | Bytes read |
| `quicbaas_parameters_operation_duration_seconds` | Histogram | Operation latency |
| `quicbaas_parameters_operation_errors_total` | Counter | Errors by operation/type |
| `quicbaas_parameters_expired_total` | Counter | Expired parameters encountered |
| `quicbaas_parameters_encrypted_ops_total` | Counter | Encrypted operations |

## Error Handling

All errors are typed for proper handling:

| Error Type | HTTP Status | Description |
|------------|-------------|-------------|
| `ParameterNotFoundError` | 404 | Parameter does not exist |
| `ParameterExistsError` | 409 | Parameter exists (no overwrite) |
| `InvalidPathError` | 400 | Invalid path format |
| `ParameterExpiredError` | 410 | Parameter TTL expired |
| `ParameterEncryptionError` | 500 | KMS encryption/decryption failure |
| `ParameterValidationError` | 400 | Validation failure |

## Thread Safety

The Parameter Store service is fully thread-safe:

- Uses atomic operations for service state
- Read-write mutex for configuration changes
- Lock-free operations where possible
- All storage operations are handled by PebbleDB's internal synchronization

## See Also

- [README](README.md) - Overview and quick start
- [API Reference](api-reference.md) - Complete API documentation
- [Access Control](access-control.md) - RBAC/ABAC policies

## Service-Layer Tenant Isolation

The parameter store service scopes all parameter paths to the requesting tenant at the service layer using `iam.TenantFromContext(ctx)`.

### Path Scoping

In multi-tenant mode, parameter paths are prefixed before reaching the manager:

```
tenant/{tenantID}/{path}
```

This scoping is applied by the service layer. The underlying `ParameterManager` receives already-scoped paths and operates without tenant awareness.

### Operation Scoping

- **Set/Get/Delete**: The parameter path is prefixed with the tenant namespace before the operation
- **ListParameters**: The tenant prefix is combined with the caller's request prefix for storage lookup
- **SearchByTags**: The tenant prefix is combined with any path-based filters in the tag search

### Response Path Stripping

All responses have the tenant prefix stripped before returning to callers. Clients see only their original paths without any tenant namespace. This keeps the tenant scoping transparent to API consumers.

### ABAC Integration

The ABAC audit layer receives the original unscoped path, not the tenant-prefixed path. This ensures audit logs and policy evaluations reference paths as callers specified them.

### Single-Tenant Mode

When the tenant ID from context is empty, paths are used as-is with no prefix applied and no stripping on responses. Existing single-tenant deployments are unaffected.

### Cross-Tenant Access

Attempts to access parameters outside the requesting tenant's namespace return a typed `ErrTenantAccessDenied` error, enforced at the service layer.
