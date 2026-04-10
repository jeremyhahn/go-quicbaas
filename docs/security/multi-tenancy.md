# Multi-Tenant Service Isolation

QuicBaaS provides data isolation across all services via tenant-scoped storage keys. When a tenant ID is present in the request context, all operations are automatically scoped to that tenant's namespace. Single-tenant deployments require zero configuration changes.

## Architecture

```
Client Request
    │
    ▼
API Layer (REST/gRPC/MCP)
    │  sets tenant in context via IAM middleware
    ▼
Service Layer (pkg/service/)
    │  prepareTenantContext(ctx) → validates tenant ID
    │  tenantScopedKey(tenantID, id) → prefixes storage key
    ▼
Storage Layer (KVStore / QRDB SDK)
    │  BarrierBackend encrypts/decrypts transparently
    ▼
Physical Storage (PebbleDB via Raft)
```

Every service follows the same pattern:

1. Extract tenant ID from context: `iam.TenantFromContext(ctx)`
2. Validate tenant ID (alphanumeric, hyphens, underscores; max 128 chars)
3. Scope storage key: `prefix/{tenantID}/{originalKey}`
4. Perform storage operation with scoped key
5. Strip tenant prefix from response keys

## Tenant ID Rules

| Rule | Value |
|------|-------|
| Max length | 128 characters |
| Allowed chars | `[a-zA-Z0-9_-]` |
| Empty value | Valid (single-tenant mode) |
| Path separators | Rejected |

Validation is shared across all services via `validateTenantID()` and `isValidTenantChar()` in `pkg/service/kms_tenant.go`.

## Service Coverage

| Service | Tenant Helper File | Storage Prefix | Scoped Entity |
|---------|-------------------|----------------|---------------|
| KMS | `kms_tenant.go` | `tenant/{tenantID}/` | Key IDs |
| Secrets | `secrets_tenant.go` | `tenant/{tenantID}/` | Secret paths |
| TimeSeries | `timeseries_tenant.go` | `ts-tenant/{tenantID}/` | Metric names |
| Scheduler | `scheduler_tenant.go` | `tenant/{tenantID}/` | Job names (via TargetTenantID) |
| Parameter Store | `parameter_tenant.go` | `tenant/{tenantID}/` | Parameter paths |
| Vector | `vector_tenant.go` | `tenant/{tenantID}/` | Namespace names |
| Certificate | `certificate_tenant.go` | N/A (external CA) | Context validation only |
| Embedding | `embedding_tenant.go` | N/A (external provider) | Context validation only |
| Object Storage Admin | `objectstorage_admin_tenant.go` | N/A (infra-level) | Context validation only |
| Object Storage | `objectstorage_tenant.go` | Deferred | Interface-only, not yet implemented |
| Dynamic Secrets | `dynamic_secrets_tenant.go` | Deferred | Interface-only, not yet implemented |
| IAM | N/A | N/A | Platform-scoped, not tenant-scoped |

## Isolation Patterns by Service

### Full Key Scoping (KMS, Secrets, Parameter Store, Vector, TimeSeries)

These services store data in QuicBaaS's KVStore. All storage keys are prefixed with the tenant namespace:

```
# Single-tenant (tenantID = "")
kms/keys/my-key

# Multi-tenant (tenantID = "acme-corp")
kms/tenant/acme-corp/keys/my-key
```

Cross-tenant access returns `ErrCrossTenantAccess`. List operations return only the requesting tenant's data with prefixes stripped from the response.

### Job-Level Scoping (Scheduler)

The scheduler uses `TargetTenantID` on the job struct rather than prefix-based key scoping. The service layer:

- Sets `TargetTenantID` from the request context on job creation
- Enforces that `TargetTenantID` matches the requesting tenant on read/write
- Filters list results to only jobs belonging to the requesting tenant

### Context Validation Only (Certificate, Embedding, Object Storage Admin)

These services delegate to external systems (xkms CA, embedding providers, infrastructure backends). Tenant isolation validates the context is well-formed but does not scope external identifiers. The external systems are responsible for their own isolation.

## Error Handling

All tenant violations produce typed errors:

| Error Code | Scenario |
|------------|----------|
| `ErrInvalidInput` | Tenant ID fails validation |
| `ErrCrossTenantAccess` | Operation targets another tenant's data |

Error types include diagnostic fields (`RequestingTenant`, `OwnerTenant`, `Metric`/`Operation`) for audit logging.

## Barrier Encryption

Barrier encryption is handled transparently by `qrdb.BarrierBackend`. When `Barrier.Enabled = true`, all values are encrypted on `Put` and decrypted on `Get`/`Range`/`Batch`. Services do not implement encryption — they only scope keys.

## Backward Compatibility

Single-tenant deployments (empty tenant ID) require zero changes:
- Storage keys are unmodified
- All data is visible in list operations
- No access control is applied
- All helper functions pass through without modification

## Testing

Each tenant helper file has a corresponding `*_tenant_test.go` with tests covering:
- Single-tenant passthrough
- Multi-tenant scoping and stripping
- Cross-tenant access denial
- Roundtrip consistency (scope then strip)
- Input validation (invalid chars, too long)

Run all tenant tests:

```bash
make test-service-tenant
```
