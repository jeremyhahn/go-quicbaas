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
API Layer (REST/gRPC/MCP/Unix)
    │  go-qrdb's AuthMiddleware (or the legacy listener's interceptor)
    │  binds the caller's tenant under go-qrdb's central key
    ▼
Service Layer (pkg/service/)
    │  tenantctx.FromContext / prepareTenantContext(ctx) → refuses an
    │  unbound context, validates the tenant ID
    │  tenantScopedKey(tenantID, id) → prefixes storage key
    ▼
Storage Layer (KVStore / QRDB SDK)
    │  BarrierBackend encrypts/decrypts transparently
    ▼
Physical Storage (PebbleDB via Raft)
```

Every service follows the same pattern:

1. Read the tenant from go-qrdb's central context key through
   `pkg/tenantctx` (see "Request Tenant Resolution" below) — the one helper
   every service uses; there is no go-quicbaas-private tenant key
2. Validate tenant ID (alphanumeric, hyphens, underscores; max 128 chars)
3. Scope storage key: `prefix/{tenantID}/{originalKey}`
4. Perform storage operation with scoped key
5. Strip tenant prefix from response keys

## Tenant ID Rules

| Rule | Value |
|------|-------|
| Max length | 128 characters |
| Allowed chars | `[a-zA-Z0-9_-]` |
| Empty value | Refused — an absent tenant is never the system tenant (see below) |
| Path separators | Rejected |

Validation is shared across all services via `validateTenantID()` and `isValidTenantChar()` in `pkg/service/kms_tenant.go`.

## Request Tenant Resolution

`pkg/tenantctx` (go-quicbaas#1150) is the single read and bind point for the
tenant a request acts for. It owns no context key: the tenant lives under
go-qrdb's central key (`qrdbsdk.ContextWithTenant` / `TenantFromContext`),
which go-qrdb's `AuthMiddleware` binds from the authenticated identity on
every transport the unified server runs, and which go-quicbaas's own
authenticating paths (`injectPrincipal`, the legacy gRPC/Unix authorization
interceptor, the MCP upstream projection) bind through `tenantctx.WithTenant`.

| Function | Use |
|----------|-----|
| `FromContext(ctx, op)` | The bound tenant, or a `*MissingTenantError` (coded `ErrForbidden`, so 403 / `PermissionDenied` on every transport). Every service that scopes records reads this. |
| `Target(ctx, op, requested)` | Resolve a caller-supplied tenant (a `tenant_id` field, an `X-Tenant-ID` header). Empty means the caller's own tenant; the caller's own tenant is accepted; anything else is a `*CrossTenantError` (coded `ErrForbidden`) for **every** caller, the system tenant included — a request field is not an authorization. |
| `WithTenant` / `WithSystem` | Bind a tenant. In-process callers that act for the platform bind the system tenant explicitly: the embedded transport does so at `Request`/`RequestStream` when the caller bound none, and `auth.NewSystemContext` does so for daemon-internal jobs. |
| `Lookup(ctx)` | The login and token plane only, which runs before a tenant is known; never for scoping records. |

**Fail closed.** A context with no tenant bound is refused. Before #1150 the
services read a go-quicbaas-private key that nothing on the unified gRPC path
set, treated the empty result as "single-tenant / system", and then honoured
whatever tenant the request named — so a tenant-A caller authorized by
go-qrdb reached tenant B and system data. Inside the services the explicit
system binding still maps to the empty, unprefixed platform scope
(`prepareTenantContext` returns `""` for it), so storage layouts are unchanged;
what changed is that `""` can only be reached by binding the system tenant.

**Platform tokens carry a tenant.** go-qrdb folds an identity with an empty
tenant onto the system tenant, so a validated platform token whose
`tenant_id` is empty is refused before that can happen: on REST by
`Server.AuthenticateHTTP` (`EmptyTenantTokenError`), and on gRPC, MCP, QUIC
and Unix by the platform-bearer authenticator registered with
`qrdbnode.WithAuthenticators` (`PlatformBearerTenantRequiredError`).

## The System Tenant

The empty tenant ID and the literal `"system"` (`iam.SystemTenantID` /
`qrdbsdk.SystemTenantID`, an alias of qrdb's `tenant.SystemTenantID`) denote
the **same** structurally privileged scope: the platform / control plane, not
a tenant namespace named "system". `qrdbsdk.IsSystemTenant(tenantID)`
(`go-qrdb/sdk/go`, aliasing `tenant.IsSystemTenant`) is the single canonical
predicate for this question — every tenant-scoping call site in quicbaas uses
it directly instead of re-deriving `tenantID == ""` (single-tenant mode) or
`tenantID == "system"` locally; quicbaas keeps no local copy of the
comparison for storage scoping. A storage scope carrying either value is
exempt from per-tenant key scoping; an authenticated caller is a different
question (see below). An ordinary tenant ID
(including one that merely resembles "system", e.g. `"System"` or
`"system-2"`) is still fully isolated.

### An empty caller tenant is not the system tenant

`IsSystemTenant("")` is true because an unbound *storage* scope resolves to
the system namespace. That does not make an *authenticated caller* with an
empty tenant the system tenant. Only a principal whose tenant is the system
tenant id itself acts as system; `auth.IsSystemSubject(subject)` is the
predicate for that question. An authenticated principal with an empty tenant
is bound to no tenant and is refused:

| Site | Empty-tenant caller gets |
|------|--------------------------|
| Per-tenant barrier read and mutate gates (`pkg/service`) | `ErrForbidden` |
| Embedded IAM admin and account-reservation scoping | `PermissionDeniedError` |
| REST `requireTenantSubject` | `400 tenant ID is required` |

On the embedded transport, an **unbound** context (the host process, no
subject at all) is the grant and keeps its access.

### Asking the question from outside quicbaas

An embedding host may be forbidden from importing go-qrdb directly (the
go-trusted-platform layer guard is machine-enforced), so the SDK re-exports
the same symbols under `github.com/jeremyhahn/go-quicbaas/sdk/go`:

| SDK symbol | Meaning |
|------------|---------|
| `quicbaas.SystemTenantID` | The reserved system tenant id |
| `quicbaas.IsSystemTenant(tenantID string) bool` | The canonical predicate — true for the system id **and** the empty string |
| `quicbaas.AuthIdentity` | The authenticated principal carried on a request context |
| `quicbaas.IdentityIsSystemTenant(*AuthIdentity) bool` | Same question for an identity; a nil identity is system-scoped (internal boot pipeline) |

These are pure aliases of the engine's own symbols, so the host gets exactly
the answer the engine gives itself. Use the predicates rather than comparing
against `SystemTenantID`: a bare string comparison misses the empty-tenant
case and silently under-privileges internal callers.

`AuthIdentity` is named apart from the SDK's existing `Identity` — that one is
the entity resolved by a login ceremony's `FinishLoginResponse`, a different
type; collapsing the two would be a silent widening.

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
| Object Storage | `objectstorage_tenant.go` | Catalog entry tenant | Every operation resolves its tenant with `objectStorageTenant` (tenantctx.Target): the request's `X-Tenant-ID` / `tenant_id` may only repeat the caller's own tenant |
| Dynamic Secrets | `dynamic_secrets_tenant.go` | Deferred | Interface-only, not yet implemented |
| Account, Agent Catalog, Cost Settings, Spend Policy | `tenant_target.go` | Tenant namespace | `targetTenantContext` (tenantctx.Target): the request's `tenant_id` may only repeat the caller's own tenant |
| IAM | N/A | N/A | Platform-scoped, not tenant-scoped; the transports confine `tenant_id` (below) |

### Caller-Named Tenants

Every request field that names a tenant goes through `tenantctx.Target`
(above): the system tenant gets no lift, so to act in tenant `T` a caller
authenticates as a principal of `T`. An embedded SDK caller names its tenant
with `transport.ContextWithTenant`; one that names none is bound as the
system tenant and is refused on the account, agent catalog, cost and spend
APIs for any other tenant.

On gRPC, the IAM (users, roles, policies, apps, consent, impersonation,
WebAuthn credentials), federation, project and session servers check
`tenant_id` with `requireCallerTenant` before the service is reached. So do
the orchestrator's `SubscribeSessionEvents` and `GetSessionConversation`. An
omitted `tenant_id` on `GetSessionConversation` reads the caller's own
conversation, not the system tenant's. `PushEvents`, the fleet daemon's
multi-tenant ingest stream, is not checked this way.

On REST, the IAM handlers that take `{tenant_id}` from the path resolve it
with `Server.urlTenant` (`pkg/api/rest/tenant_target.go`). These cover apps,
federation provider administration and account links, GDPR consent, export
and deletion, MFA, policies, roles, sessions, users and WebAuthn credentials.
A path naming any tenant other than the caller's own is `403`. The object
storage routes are confined in the service by `objectStorageTenant`.

On both transports, these are outside the check:

- the pre-auth flows: federated login begin and callback, invite
  activation, self-registration;
- tenant provisioning and administration (`/api/v1/tenants/...` and the
  tenant barrier).

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

Storage layouts are unchanged: the system tenant keeps the unprefixed
layout. What changed in go-quicbaas#1150 is how the system scope is reached.
A single-tenant or embedded deployment reaches it because its caller is bound
to the system tenant — the embedded transport and `auth.NewSystemContext`
bind it — never because no tenant was bound. A request with no tenant bound is
refused, and a request naming a tenant other than its caller's is refused.

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

# Tenant scope on the go-qrdb wire mount — what go-qrdb's SDK and
# `quicbaas barrier enable-encryption --tenant acme-corp` send
curl -X POST http://localhost:3000/v1/barrier/enable-encryption \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "strategy": "passphrase",
    "tenant_id": "acme-corp",
    "passphrase": "dGVuYW50LXdyYXBwaW5nLXBhc3NwaHJhc2U="
  }'
# => 200 {"status":"migration_started","scope":"tenant"}
```

The `/api/v1` routes take `target_strategy`, derive the scope from the path
and answer `204`; the system route rejects a body `tenant_id` with `400`
rather than migrate the system barrier in its place. The `/v1/barrier/enable-encryption` mount speaks go-qrdb's
wire verbatim (go-quicbaas#990): `strategy`, a body `tenant_id` selecting the
tenant barrier (empty = system), and `200 {status, scope}`. It authorizes the
scope it was given: `tenant_barrier:enable_encryption` against the named
tenant, or `seal:enable_encryption` against the system tenant when `tenant_id`
is empty. A tenant admin can therefore migrate its own barrier on this mount,
but not the system barrier or another tenant's. On every route the service
binds a tenant-scoped migration to the caller's own tenant.

Enable-encryption requires:

1. Tenant entitlement `AllowedBarrierModes` includes the target strategy
2. Barrier currently in `none` state (only none → keyed transitions supported)
3. For cloud KMS / HSM: corresponding provider is reachable and provisioned
4. Sufficient quorum for multi-shard coordination (IsolationRaft mode)

See [Multi-Tenancy Architecture: Tenant Entitlements](architecture.md#tenant-entitlements) for how the entitlement is enforced, and the [Barrier API](../barrier/api.md) for the endpoint.


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
