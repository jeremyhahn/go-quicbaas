# KMS Multi-Tenancy

QuicBaaS's KMS layer supports multi-tenant key isolation. Each tenant's keys are stored in a separate namespace, preventing cross-tenant access.

## How It Works

When a tenant ID is present in the request context, key operations are automatically scoped to that tenant's namespace. When no tenant ID is present, the system operates in single-tenant mode with no behavioral changes.

### Key Namespace Format

- **Single-tenant**: keys stored as `{keyID}`
- **Multi-tenant**: keys stored as `tenant/{tenantID}/{keyID}`

At the storage layer, the full path includes the KMS prefix:

- **Single-tenant**: `kms/keys/{keyID}`
- **Multi-tenant**: `kms/tenant/{tenantID}/keys/{keyID}`

### Scoped Operations

All KMS CRUD operations are tenant-aware:

| Operation    | Behavior                                                   |
|-------------|------------------------------------------------------------|
| GenerateKey | Key stored under tenant namespace                          |
| ImportKey   | Key stored under tenant namespace                          |
| GetKey      | Scoped to tenant; returns not-found for other tenants' keys |
| ListKeys    | Returns only the requesting tenant's keys                  |
| RotateKey   | Only rotates keys owned by the requesting tenant           |
| DeleteKey   | Only deletes keys owned by the requesting tenant           |

### Tenant ID Validation

Tenant IDs must:
- Be 1-128 characters
- Contain only alphanumeric characters, hyphens, and underscores
- Not contain path separators (/) or special characters

### API Layer

All API protocols (REST, gRPC, MCP) route KMS operations through the service layer when available. The service layer extracts the tenant ID from the request context via `iam.TenantFromContext(ctx)` and applies namespace scoping transparently.

If the service layer is unavailable, handlers fall back to direct KMS manager calls (no tenant isolation).

## Backward Compatibility

Single-tenant deployments require zero changes. When tenant ID is empty:
- Key IDs are used as-is (no prefix)
- All keys are visible in list operations
- No access control is applied
- Storage paths use the original `kms/keys/` prefix
