# Multi-Tenancy — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/multi-tenancy/`.

QuicBaaS provides data isolation across all services via tenant-scoped storage keys and per-tenant barriers.
When a tenant ID is present in the request context, all operations are automatically scoped to that tenant's namespace.
Tenants can be isolated at four levels: keyspace (namespace prefix), barrier (per-tenant encryption), volume (separate
physical storage directory), or raft (dedicated consensus group). Single-tenant deployments require zero configuration changes.

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
| Empty value | Valid — resolves to the system tenant (see below) |
| Path separators | Rejected |

Validation is shared across all services via `validateTenantID()` and `isValidTenantChar()` in `pkg/service/kms_tenant.go`.

## The System Tenant

The empty tenant ID and the literal `"system"` (`iam.SystemTenantID` /
`qrdbsdk.SystemTenantID`, an alias of qrdb's `tenant.SystemTenantID`) denote
the **same** structurally privileged scope: the platform / control plane, not
a tenant namespace named "system". `qrdbsdk.IsSystemTenant(tenantID)`
(`go-qrdb/sdk/go`, aliasing `tenant.IsSystemTenant`) is the single canonical
predicate for this question — every tenant-scoping call site in quicbaas uses
it directly instead of re-deriving `tenantID == ""` (single-tenant mode) or
`tenantID == "system"` locally; quicbaas keeps no local copy of the
comparison. A caller whose identity carries either value is exempt from
per-tenant key scoping and cross-tenant access checks; an ordinary tenant ID
(including one that merely resembles "system", e.g. `"System"` or
`"system-2"`) is still fully isolated.

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

## Tenant Isolation Modes

Each tenant is assigned an `IsolationType` that controls the scope of isolation:

| Mode | Scope | Barrier | Storage | Use case |
|------|-------|---------|---------|----------|
| `keyspace` | Namespace prefix | Shared system barrier | Shared PebbleDB | Cost-optimized multi-tenant SaaS, low-trust environments |
| `barrier` | Per-tenant encryption | Dedicated per-tenant barrier | Shared PebbleDB | Multi-tenant SaaS with per-tenant encryption |
| `volume` | Separate directory | Dedicated per-tenant barrier | Separate filesystem directory | Hybrid: per-tenant isolation + shared cluster |
| `raft` | Dedicated consensus group | Dedicated per-tenant barrier | Dedicated Raft group replicas | High-security, strong isolation (clustered mode only) |

The provisioner wires the isolation mode at tenant creation time:

```go
tenant, err := provisioner.CreateTenant(ctx, &qrdb.CreateTenantRequest{
    ID: "acme-corp",
    IsolationType: qrdb.IsolationVolume,  // or Barrier, Keyspace, Raft
    // ... other fields
})
```

## Tenant Entitlements and Quotas

Each tenant has an `Entitlements` set that governs what features and barrier modes are allowed:

| Entitlement | Type | Values | Notes |
|-------------|------|--------|-------|
| `AllowedIsolationModes` | []string | keyspace, barrier, volume, raft | Which isolation modes the tenant may use |
| `AllowedBarrierModes` | []string | none, passphrase, shamir, tpm2, pkcs11, awskms, gcpkms, azurekv, vault | Which barrier strategies the tenant may enable |
| `AllowDedicatedRaft` | bool | true/false | Whether this tenant may create dedicated Raft groups (requires `IsolationType: raft`) |
| `AllowVolumeStorage` | bool | true/false | Whether this tenant may use volume-scoped storage (requires `IsolationType: volume`) |
| `AllowSystemManagedBarrier` | bool | true/false | Whether this tenant's barrier may use system-managed DEK envelope (enables enable-encryption without manual HSM/KMS) |
| `AllowDedicatedWAL` | bool | true/false | Whether this tenant may allocate dedicated WAL groups (REST/gRPC only; not available on QUIC transport) |

Additionally, each tenant has resource `Quotas`:

- `MaxDatabases`, `MaxRaftGroups`, `MaxReplicasPerGroup`, `MaxTotalReplicas`
- `MaxCollections`, `MaxKeys`, `MaxStorageBytes`, `MaxDatabaseSizeBytes`
- `MaxUsers`, `MaxRoles`, `MaxCertificates`, `MaxBackups`
- `MaxVectorDims`, `MaxTSMetrics`, `MaxDedicatedWALGroups` (where supported)
- `SystemBarrierQuota` — tenant's share of the system barrier's storage

Quotas and entitlements are set at tenant creation and updated via:

- `PUT /v1/tenants/{tenant_id}/quotas` (REST)
- `PUT /v1/tenants/{tenant_id}/entitlements` (REST)
- `TenantServicer.UpdateQuotas()` / `UpdateEntitlements()` (gRPC/SDK)

## Tenant Enable-Encryption

Tenants can transition their isolation barrier from cleartext (`none`) to encrypted via:

```bash
# System scope: enable shared cluster barrier
curl -X POST http://localhost:3000/api/v1/barrier/enable-encryption \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "target_strategy": "shamir",
    "passphrase": "my-wrapping-passphrase",
    "shamir_threshold": 2,
    "shamir_total": 3
  }'

# Tenant scope: enable per-tenant barrier
curl -X POST http://localhost:3000/api/v1/tenants/acme-corp/barrier/enable-encryption \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "target_strategy": "passphrase",
    "passphrase": "tenant-wrapping-passphrase"
  }'
```

Enable-encryption requires:

1. Tenant entitlement `AllowedBarrierModes` includes the target strategy
2. Barrier currently in `none` state (only none → keyed transitions supported)
3. For cloud KMS / HSM: corresponding provider is reachable and provisioned
4. Sufficient quorum for multi-shard coordination (IsolationRaft mode)

See [Barrier Architecture: Enable-Encryption](../architecture/barrier.md#enable-encryption-barrier-migration) for details.


---

## KMS Multi-Tenancy

QuicBaaS's KMS layer supports multi-tenant key isolation. Each tenant's keys are stored in a separate namespace, preventing cross-tenant access.

### How It Works

When a tenant ID is present in the request context, key operations are automatically scoped to that tenant's namespace. When no tenant ID is present, the system operates in single-tenant mode with no behavioral changes.

#### Key Namespace Format

- **Single-tenant**: keys stored as `{keyID}`
- **Multi-tenant**: keys stored as `tenant/{tenantID}/{keyID}`

At the storage layer, the full path includes the KMS prefix:

- **Single-tenant**: `kms/keys/{keyID}`
- **Multi-tenant**: `kms/tenant/{tenantID}/keys/{keyID}`

#### Scoped Operations

All KMS CRUD operations are tenant-aware:

| Operation    | Behavior                                                   |
|-------------|------------------------------------------------------------|
| GenerateKey | Key stored under tenant namespace                          |
| ImportKey   | Key stored under tenant namespace                          |
| GetKey      | Scoped to tenant; returns not-found for other tenants' keys |
| ListKeys    | Returns only the requesting tenant's keys                  |
| RotateKey   | Only rotates keys owned by the requesting tenant           |
| DeleteKey   | Only deletes keys owned by the requesting tenant           |

#### Tenant ID Validation

Tenant IDs must:
- Be 1-128 characters
- Contain only alphanumeric characters, hyphens, and underscores
- Not contain path separators (/) or special characters

#### API Layer

All API protocols (REST, gRPC, MCP) route KMS operations through the service layer when available. The service layer extracts the tenant ID from the request context via `iam.TenantFromContext(ctx)` and applies namespace scoping transparently.

If the service layer is unavailable, handlers fall back to direct KMS manager calls (no tenant isolation).

### Backward Compatibility

Single-tenant deployments require zero changes. When tenant ID is empty:
- Key IDs are used as-is (no prefix)
- All keys are visible in list operations
- No access control is applied
- Storage paths use the original `kms/keys/` prefix
