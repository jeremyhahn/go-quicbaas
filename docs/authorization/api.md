# Authorization — API

> **Status: NORMATIVE** · Last verified: 2026-09-13
> The capability on every transport, with permissions. Feature folder: `docs/authorization/`.

> **Overview**: QuicBaaS provides a unified authorization system that protects all API protocols (REST, gRPC, QUIC, MCP) with configurable access control.



## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.RBAC` — SDK package `sdk/go/client/rbac`

Dedicated transport handlers: rest=no, grpc=yes, mcp=no; embedded routes registered: 5; MCP tools registered server-side: 5 (`qrdb_role_create`, `qrdb_role_delete`, `qrdb_role_list`, `qrdb_role_assign`, `quicbaas_role_revoke`).

**Roles only.** This surface has no user verbs. Users belong to go-qrdb and are
administered over the single IAM surface — `Identity().IAM().User()` on the SDK,
`/api/v1/iam/users` on REST, `quicbaas.iam.v1.IAMService` on gRPC,
`quicbaas_iam_user_*` on MCP (see [../iam/api.md](../iam/api.md)). The
`CreateUser` / `DeleteUser` / `ListUsers` rows that used to sit in this table
dispatched to an auth manager production never wired, so every call failed; they
were deleted in go-quicbaas#905 together with the parallel identity model they
belonged to.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `AssignRole` | `POST /api/v1/rbac/users/{username}/roles` | **no** | yes | `qrdb_role_assign` | yes | — |
| `CreateRole` | `POST /api/v1/rbac/roles` | **no** | yes | `qrdb_role_create` | yes | — |
| `DeleteRole` | `DELETE /api/v1/rbac/roles/{name}` | **no** | yes | `qrdb_role_delete` | yes | — |
| `ListRoles` | `GET /api/v1/rbac/roles` | **no** | yes | `qrdb_role_list` | yes | — |
| `RevokeRole` | `DELETE /api/v1/rbac/users/{username}/roles/{role}` | **no** | yes | `quicbaas_role_revoke` | yes | — |


## Authorization Providers

QuicBaaS supports three authorization providers:

| Provider | Description | Use Case |
|----------|-------------|----------|
| **RBAC** | Role-Based Access Control | Simple deployments with predefined roles |
| **ABAC** | Attribute-Based Access Control | Complex policies based on user/resource attributes |
| **Hybrid** | ABAC-first with RBAC fallback | Migration from RBAC to ABAC |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Client Request                             │
│              (REST / gRPC / QUIC / MCP)                         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Protocol Handler                             │
│  ┌─────────────┐  ┌─────────────┐  ┌──────┐  ┌──────┐           │
│  │ REST        │  │ gRPC        │  │ QUIC │  │ MCP  │           │
│  │ Middleware  │  │ Interceptor │  │ Auth │  │ Auth │           │
│  └─────────────┘  └─────────────┘  └──────┘  └──────┘           │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Extract: Subject, Resource, Action
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                  AuthorizationService                           │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  • Audit logging (all decisions)                           │ │
│  │  • Prometheus metrics collection                           │ │
│  │  • Decision ID generation                                  │ │
│  │  • Evaluation timing                                       │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                              │
            ┌─────────────────┼─────────────────┐
            ▼                 ▼                 ▼
     ┌───────────┐     ┌─────────────┐     ┌───────────┐
     │   RBAC    │     │     ABAC    │     │  Hybrid   │
     │ Provider  │     │   Provider  │     │ Provider  │
     │ (default) │     │(iam)│     │  (both)   │
     └───────────┘     └─────────────┘     └───────────┘
            │                 │                 │
            ▼                 ▼                 ▼
      go-qrdb PDP       PolicyEngine      ABAC → RBAC
                                          fallback
```

## Quick Start

### Basic RBAC Configuration (Default)

```yaml
# .quicbaas.yaml
security:
  authorization:
    provider: "rbac"
    rbac:
      enabled: true
      default_role: ""  # Deny unauthenticated requests
      allow_anonymous: true  # Allow health/metrics without auth
    audit:
      enabled: true
      log_denied: true
```

### Enabling ABAC

```yaml
security:
  authorization:
    provider: "abac"
    abac:
      enabled: true
      cache_ttl: "5m"
      policy_dir: "/etc/quicbaas/policies"
```

> **Note**: ABAC is always compiled into every build; no build tag is required.

### Hybrid Mode (Migration)

```yaml
security:
  authorization:
    provider: "hybrid"
    rbac:
      enabled: true
    abac:
      enabled: true
```

## Core Concepts

### Subject

The entity making the request:

```go
type Subject struct {
    ID            string                 // Unique identifier
    Username      string                 // Human-readable name
    TenantID      string                 // Multi-tenant context
    Roles         []string               // RBAC roles
    Groups        []string               // The `groups` claim: names of the bound roles
    Attributes    map[string]interface{} // ABAC attributes
    Authenticated bool                   // Auth status
}
```

### Resources

Protected resources in QuicBaaS:

| Resource | Description |
|----------|-------------|
| `keys` | Key-value data operations |
| `transactions` | Transaction operations |
| `secrets` | Secrets management |
| `kms` | Key Management Service |
| `crypto` | Encryption/decryption |
| `seal` | Seal/unseal operations |
| `cluster` | Cluster management |
| `users` | User management |
| `roles` | Role management |
| `timeseries` | Time-series data |
| `vector` | Vector database |
| `watch` | Watch/subscription |
| `backups` | Backup operations |

### Actions

| Action | HTTP Methods | Description |
|--------|--------------|-------------|
| `read` | GET, HEAD, OPTIONS | Read operations |
| `write` | POST, PUT, PATCH | Write operations |
| `delete` | DELETE | Delete operations |
| `admin` | - | Administrative operations |

### Decision

Authorization result:

```go
type AuthorizationDecision struct {
    Allowed          bool   // Allow or deny
    Reason           string // Explanation
    Provider         string // rbac, abac, or hybrid
    MatchedPolicy    string // Which policy matched
    DecisionID       string // Unique ID for audit
    EvaluationTimeNs int64  // Performance metric
}
```

## Public Endpoints

These endpoints bypass authorization:

- `/api/v1/auth/login` - Authentication
- `/api/v1/health` - Health checks
- `/api/v1/health/live` - Kubernetes liveness probe
- `/api/v1/health/ready` - Kubernetes readiness probe
- `/metrics` - Prometheus metrics

The probe paths are `live` / `ready` (see `setupRouter` in
`pkg/api/rest/server.go`). The allowlists previously named `liveness` /
`readiness`, which matched no mounted route; both `publicEndpoints`
(`authz_middleware.go`) and `defaultDenyPublicPrefixes`
(`authz_default_deny.go`) now name the real paths.

### Pre-auth (tenant-scoped) routes

The WebAuthn **login** ceremony is the authentication mechanism itself, so it
must be reachable before the caller holds any token. Because the path carries a
variable tenant segment it cannot be expressed as a literal prefix; the
templates live in `pkg/api/rest/authz_public_routes.go` and are honoured by
every enforcement posture (`isAuthzPublicEndpoint` and `isDefaultDenyPublic`):

| Route | Mount |
| --- | --- |
| `POST /api/v1/iam/{tenant_id}/webauthn/login/begin` | canonical xkms mount (Go SDK REST transport) |
| `POST /api/v1/iam/{tenant_id}/webauthn/login/finish` | canonical xkms mount (Go SDK REST transport) |

The legacy `/api/v1/tenants/{tenant_id}/webauthn/*` mount was
deleted; the SPA and TypeScript SDK now log in through the unified
ceremony at `/api/v1/auth/login/{begin,finish}`, which is public via
`publicEndpoints` rather than these templates.

Matching is whole-path and segment-exact: the tenant segment is the only
wildcard, so no sibling route is exempted. The **enrollment** ceremonies
(`.../webauthn/register/{begin,finish}` and
`.../webauthn/registration/{begin,finish,status}`) are deliberately NOT public —
they bind a new authenticator to a caller-supplied user id. The pre-auth
enrollment path is invite activation
(`/api/v1/iam/invites/{token}/activate/fido2/{begin,finish}`), authorized by the
one-time invite token in the URL.

### Root-mounted route groups

The IAM surface registers on the **root** chi router (the OIDC discovery
endpoints must live at `/.well-known/*`), so those groups do NOT inherit the
`/api/v1` group's authorization chain. Every protected root mount installs the
same shared selector itself — `Server.useAPIAuthzMiddleware` — inside its
`chi.Route` closure. Under the default-deny posture the selector is a no-op
because `EnforceDefaultDeny` on the top router already covers root mounts, so
each surface is gated exactly once.

| Root mount | Gate |
| --- | --- |
| `/api/v1/tenants`, `/api/v1/iam` | `useAPIAuthzMiddleware` (iam.go) |
| `/api/v1/iam/{tenant_id}/webauthn` | `useAPIAuthzMiddleware` (iam_xkms_webauthn_mount.go); login routes stay pre-auth |
| `/api/v1/admin` | `useAPIAuthzMiddleware` (iam_admin.go) |
| `/v1/barrier`, `/v1/credentials`, `/v1/ceremony`, `/v1/init`, `/users`, `/v1/users/factors` | `useAPIAuthzMiddleware` (server.go) |
| `/api/v1/auth`, `/api/v1/auth/providers/{tenant_id}/{provider_name}`, `/api/v1/iam/invites`, `/acme`, `/.well-known/*`, `/oauth2/*` | intentionally pre-auth (the request carries its own credential) |

A new root mount that omits the selector is reachable unauthenticated under
validator (`RequireAuthz=true`) enforcement — the routing-hole class that has
recurred on `/v1/barrier`, `/v1/credentials`, the canonical WebAuthn mount and
`/api/v1/admin`. Guard every new one with a real-router test.

### The REST PEP mapping is mandatory for every mounted route

`SecurityConfig.RequireAuthz` defaults to **true**, so a production install runs
`AuthorizeViaValidator`. That gate is **default-deny**: it resolves the request
to a qrdb-canonical `resource:action` through `mapRequestToPermission`
(`pkg/api/rest/authz_pep_mapping.go`) and, when no mapping exists, answers
`403 Authorization denied` **without ever consulting the validator**.

The consequence is blunt: *a surface with no PEP entry is not "unprotected", it
is unreachable*. Mounting routes, wiring the SDK and writing handler tests is
not enough — a handler test calls the handler directly and never traverses the
gate, so the surface looks healthy while every real request 403s. Surfaces that
were shipped in exactly this state include Trust, the ACME control plane, the
go-xkms compatibility API, the secret side-mounts, the legacy seal API,
`/api/v1/batch`, `/api/v1/query` and the Claude Code hook callbacks.

Two watch-outs when adding an entry:

- **Prefix matching requires `prefix + "/"`.** `/api/v1/secrets` does *not*
  cover `/api/v1/secrets-versions`; sibling mounts each need their own entry.
- **Only registered permissions work.** The join `resource:action` must exist
  in the permission registry — either a family `go-qrdb/pkg/permission`
  registers itself, or one QuicBaaS contributes through `pkg/permission` (see
  [Contributing permissions to the qrdb registry](#contributing-permissions-to-the-qrdb-registry)).
  An unregistered permission is denied by the PDP, which looks identical to a
  missing mapping.

`TestPEPMappingCoversEveryMountedRoute`
(`pkg/api/rest/authz_route_coverage_test.go`) enforces this structurally: it
walks the real chi route tree of a constructed server and fails when any mounted
route neither resolves through `mapRequestToPermission` nor appears in the
test's explicit pre-auth allowlist. Exempting a route is therefore a deliberate,
reviewed edit rather than an omission.

### Two tables, one covered surface

`EnforceDefaultDeny` (`authz_default_deny.go`) carries a second scope table for
an alternative posture that is reachable only through
`SetDefaultDenyEnforcement` — which has no production caller today. The two
tables drifting apart is what hid the bug class above: a surface listed in the
default-deny table looked gated while the live PEP had no entry for it.
`TestAuthzTablesAgreeOnCoverage` now fails if either table names a surface the
other does not resolve, and the barrier / legacy-seal granular ops are shared
source-of-truth slices (`barrierGranularOps`, `legacySealOps`) read by both
gates. Scope *granularity* may still differ where a literal prefix cannot
express a variable segment; the coarse rule is never weaker than the granular
one it stands in for.

### Contributing permissions to the qrdb registry

go-qrdb owns the single RBAC policy decision point and the canonical
`resource:action` vocabulary, but it deliberately does not know about
broker-layer domain concepts. It exposes three extension points for them:
`qrdbsdk.RegisterPermissions` (the vocabulary),
`qrdbsdk.RegisterPermissionRoleGrant` (the permission's default built-in role
placement) and `qrdbsdk.RegisterPermissionDuty` (the FIPS 140-3 AS10.03
separated duty, which is what decides Security-Officer grantability).

QuicBaaS uses all three in exactly one place — **`pkg/permission`** holds the
single list of broker contributions, where each entry pairs a permission with
the built-in roles that hold it and the duty it belongs to, and
**`server.NewServer`** (the sole composition root, shared by `quicbaasd serve`,
`quicbaasd init`, the embedded server and the Wails bindings) calls
`permission.Register()` before any component starts.

Why it matters: a permission that is not in the registry cannot be held by any
role. go-qrdb's role store validates every permission on role create/update
(`pkg/role.validatePermissions`) and rejects an unregistered one with
`InvalidPermissionError`. An authz table that maps an endpoint onto an
unregistered permission therefore ships a **dead endpoint that looks wired**:
the PEP resolves a permission no token can ever carry, so the endpoint is
permanently denied under `RequireAuthz`. This is the failure mode that hid the
attestation surface on all four transports at once.

Currently contributed:

| Permission | Strength | Built-in roles | Meaning |
|---|---|---|---|
| `attestation:verify` | read | `admin`, `operator` | Verify an attestation statement or certificate chain. A pure computation over caller-supplied material: mutates nothing, touches no hardware. |
| `attestation:attest` | write | `admin` | Drive the registered attestor — extend a PCR and ask the host TPM for a signed quote. A host-level mutation. |
| `prompts:read` | read | `admin`, `operator` | Read a tenant's or project's prompt records and the effective body a job would resolve. Tenant-scoped, outside the Security Officer's duties ([prompt-sets](../software-factory/prompt-sets.md#permissions)). |
| `prompts:write` | write | `admin`, `operator` | Edit, roll back or reset a tenant's or project's prompt. The store refuses a body that drops a marker the engine parses; no verb writes the system set. |
| `prompts:history` | read | `admin`, `operator`, `auditor` | Read a prompt's revision log — who changed it, when, at which version. Oversight evidence, hence the `auditor`. |

Both take qrdb's default `ScopeSelf` (the permission acts on the identity's own
tenant; the system-tenant rule lifts it across tenants for a system-tenant
identity). `go-qrdb/sdk/go` does re-export `permission.RegisterScope` as
`qrdbsdk.RegisterPermissionScope`, so `ScopeCross` and `ScopeTenantOnly` are
reachable from this layer — neither is right here. `ScopeTenantOnly` would deny
the system tenant the host's own attestation surface, and `ScopeCross` would
deny it to every tenant-local identity; `ScopeSelf` plus the system-tenant rule
already yields both. A permission absent from the scope registry resolves to
`ScopeSelf`, so the default is taken by omission rather than a no-op call.

**Duty (FIPS 140-3 AS10.03).** Each contribution also states which of the
separated duties it belongs to, in the same literal as the permission and its
roles, so the classification is authored rather than inferred.
`attestation:attest` is declared `DutyCryptoUse`. qrdb refuses to place it on
`so` either way: its guard treats the unambiguous crypto-primitive verbs
(`sign`, `encrypt`, `decrypt`, `wrap`, `unwrap`, `derive`, `kdf`, `mac`,
`hmac`, `attest`, `quote`) as cryptographic use in **every** resource family —
including one a higher layer contributed — and no declaration can override that
backstop. `attestation:verify` is deliberately left undeclared in qrdb:
verifying caller-supplied material touches no module-held key, so
`DutyCryptoUse` would be wrong, and it is no part of the Security Officer's
governance duties, so `DutyGovernance` would both overstate it and make it
`so`-grantable. Undeclared is the fail-safe state — qrdb refuses an `so` grant
of an unclassified permission with `UndeclaredDutyError` — so the omission
denies rather than permits.

**Where the grants land.** Role placement is metadata carried by the permission:
`pkg/role.BuiltInRoles` is a live projection of the grant registry evaluated at
role-seed time, so a grant declared in `pkg/permission` reaches the seeded
`admin` / `operator` roles because `NewServer` registers strictly upstream of
first-boot / bootstrap seeding. qrdb's five built-in roles are **flat sets** —
`admin` does not inherit `operator`'s grants — so a permission both should hold
names both explicitly. Grants are always concrete permissions, never an
`attestation:*` whole-segment wildcard, which would collapse the read/write
split the four transport authz tables encode.

The placement rationale:

- **`attestation:verify` → `admin` + `operator`.** Same strength tier as
  `keys:verify`, which `operator` already holds; it is the verb a workload or CI
  identity uses day to day to check a peer's statement. `admin` holds it too
  because it holds `attestation:attest`, and `admin` carries the read verb of
  every other family it can write (`certs:read` beside `certs:generate`,
  `trust:read` beside `trust:write`).
- **`attestation:attest` → `admin` only.** A host-level mutation that
  fingerprints platform boot state, sitting beside `certs:generate` and
  `trust:write`. Not `operator` or `user` (routine data-plane identities must
  not reshape host PCR state), not `auditor` (read-only *state* visibility under
  FIPS 140-3 AS10.10 — a quote request mutates the TPM), and not `so` (a quote
  is a signing operation, and the Security Officer is barred from cryptographic
  operations under FIPS 140-3 AS10.03; qrdb enforces that structurally for
  *this* permission, not only for the `keys:*` family —
  `RegisterPermissionRoleGrant` panics with `SoDViolationError` for a permission
  declared `DutyCryptoUse` and for any permission whose action names an
  unambiguous cryptographic primitive in any resource family, `attest` among
  them — so an `so` entry added to the grant list could not be registered, and
  the runtime RBAC deny-list rejects the operation again at authorization
  time).

Registration also makes a permission *grantable* in the ordinary way: it can be
attached to any custom role through the roles API (`roles:create` /
`roles:update`) and it appears in `permissions:list` and the UI permission
picker. On a fresh standard-posture deployment the `owner` convenience role
picks it up too, because `role.OwnerRole` snapshots `permission.All()` when
first-boot seeds roles.

Guard tests, one per transport, make an unregistered mapping a build failure:

| Transport | Test |
|---|---|
| gRPC | `TestGRPCPermissions_EveryMappingIsRegistered` (`pkg/api/grpc/authz_permission_registration_test.go`) |
| REST | `TestRESTPermissions_EveryMappingIsRegistered` (`pkg/api/rest/authz_permission_registration_test.go`) |
| QUIC | `TestQUICPermissions_EveryMappingIsRegistered` (`pkg/api/quic/authz_permission_registration_test.go`) |
| MCP | `TestMCPPermissions_EveryMappingIsRegistered` (`pkg/api/mcp/authz_qrdb_coverage_test.go`) |

`TestNewServer_RegistersQuicBaaSPermissions` (`pkg/server`) is the other end of
the arrangement: the transport guards call `permission.Register()` themselves
(importing `pkg/server` from a transport package would be an import cycle), so
this test proves production performs the registration too.

The role grants have their own end-to-end assertions in `pkg/permission`, all
driven through `Register()` and read back from qrdb's live registry rather than
re-declared: `TestRegister_PlacesPermissionsInBuiltInRoles`,
`TestRegister_WithholdsAttestFromReadOnlyAndSORoles`,
`TestRegister_GrantsOnlyConcretePermissions` and
`TestContributions_DeclareARolePlacementAndADuty` (which fails any contributed
permission left with no built-in role, or with no stated duty, at all).

The duty declarations have the same treatment:
`TestRegister_DeclaresTheDutyOfEveryContribution` reads each classification
back out of qrdb's registry, `TestRegister_RefusesAttestGrantToTheSecurityOfficer`
drives qrdb's guard and asserts the `SoDViolationError` panic, and
`TestRegister_KeepsVerifyOutsideTheSecurityOfficerDuties` proves the
deliberately undeclared read verb is refused to `so` with
`UndeclaredDutyError` rather than silently admitted.

## Detailed Documentation

For in-depth configuration and usage:

- **RBAC Documentation** - Role-Based Access Control
  - Built-in roles and permissions
  - Custom role creation
  - Permission matrix
  - Performance tuning

- **[ABAC Documentation](architecture.md)** - Attribute-Based Access Control
  - Policy syntax and structure
  - Attribute evaluation
  - Context-based access
  - Multi-tenant scenarios

## Protocol-Specific Integration

### REST API

Authorization is enforced via `AuthzMiddleware`:

```go
// Configure server with authorization
server.SetAuthorizationService(authzService, tokenValidator)
```

### gRPC

Uses interceptors:

```go
// Unary RPC
grpc.UnaryInterceptor(auth.UnaryAuthInterceptorWithService(am, authzService, log))

// Streaming RPC
grpc.StreamInterceptor(auth.StreamAuthInterceptorWithService(am, authzService, log))
```

### QUIC

Server option:

```go
server := quic.NewServer(
    quic.WithAuthorizationService(authzService),
)
```

### MCP

MCP authorization includes OAuth 2.1 token validation and audience binding:

```go
server.SetAuthorizationService(authzService, authManager)
```

**MCP-Specific Requirements:**

1. **Bearer Token**: All requests must include `Authorization: Bearer <token>` header
2. **Audience Validation**: Token `aud` claim must include MCP server's resource URI
3. **Scopes**: Token must include required scopes (`mcp:tools`, `mcp:resources`)

**HTTP Error Responses:**

| Code | Error | WWW-Authenticate |
|------|-------|------------------|
| 401 | Invalid/missing token | `Bearer realm="quicbaas-mcp", error="invalid_token"` |
| 403 | Insufficient scope | `Bearer realm="quicbaas-mcp", error="insufficient_scope", scope="mcp:admin"` |

**MCP Tool Authorization:**

| Tool Pattern | Resource | Action |
|-------------|----------|--------|
| `quicbaas_get`, `quicbaas_list` | keys | read |
| `quicbaas_put`, `quicbaas_delete` | keys | write/delete |
| `quicbaas_kms_*` | kms | varies |
| `quicbaas_secrets_*` | secrets | varies |
| `quicbaas_cluster_*` | cluster | read/admin |

**Public Tools (no auth):**
- `quicbaas_health`
- `quicbaas_login`
- `quicbaas_version`
- `quicbaas_liveness`

See [MCP Authorization](../oauth2-oidc/api.md) for complete OAuth 2.1 flow.

## Metrics

Authorization metrics are exposed via Prometheus:

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_authz_requests_total` | Counter | Total authorization requests |
| `quicbaas_authz_latency_seconds` | Histogram | Request latency |

Labels: `provider`, `resource`, `action`, `allowed`

## Audit Logging

All authorization decisions are logged:

```json
{
  "level": "info",
  "msg": "Authorization denied",
  "decision_id": "dec-abc123",
  "subject_id": "user-456",
  "resource": "secrets",
  "action": "write",
  "allowed": false,
  "reason": "roles [readonly] do not have write permission on secrets",
  "provider": "rbac"
}
```

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| 401 Unauthorized | Missing/invalid token | Check JWT token validity |
| 403 Forbidden | Insufficient permissions | Verify role has required permission |

### Debug Mode

Enable verbose logging:

```yaml
security:
  authorization:
    audit:
      log_allowed: true  # Log successful authorizations
```

## See Also

- [MCP Authorization](../oauth2-oidc/api.md) - OAuth 2.1 for MCP
- Authentication - JWT token management
- IAM Overview - Identity and Access Management
- IAM Configuration - IAM settings
- Security Overview - Security features
