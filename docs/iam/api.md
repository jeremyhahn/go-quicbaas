# IAM — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/iam/`.



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

### `quicbaas.IAM.Admin` — SDK package `sdk/go/client/iam`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 0; MCP tools registered server-side: 44.

MCP tools: `quicbaas_iam_app_delete`, `quicbaas_iam_app_get`, `quicbaas_iam_app_list`, `quicbaas_iam_app_register`, `quicbaas_iam_app_rotate_secret`, `quicbaas_iam_app_update`, `quicbaas_iam_audit_delete`, `quicbaas_iam_audit_get`, `quicbaas_iam_audit_query`, `quicbaas_iam_policy_create`, `quicbaas_iam_policy_delete`, `quicbaas_iam_policy_get`, `quicbaas_iam_policy_list`, `quicbaas_iam_policy_test`, `quicbaas_iam_policy_update`, `quicbaas_iam_role_create`, `quicbaas_iam_role_delete`, `quicbaas_iam_role_get`, `quicbaas_iam_role_list`, `quicbaas_iam_role_update`, `quicbaas_iam_tenant_create`, `quicbaas_iam_tenant_delete`, `quicbaas_iam_tenant_get`, `quicbaas_iam_tenant_list`, `quicbaas_iam_tenant_update`, `quicbaas_iam_user_assign_role`, `quicbaas_iam_user_create`, `quicbaas_iam_user_delete`, `quicbaas_iam_user_get`, `quicbaas_iam_user_list`, `quicbaas_iam_user_revoke_role`, `quicbaas_iam_user_update`, `quicbaas_mfa_backup_codes_generate`, `quicbaas_mfa_backup_codes_status`, `quicbaas_mfa_backup_codes_verify`, `quicbaas_mfa_challenge_begin`, `quicbaas_mfa_challenge_verify`, `quicbaas_mfa_disable`, `quicbaas_mfa_enroll_begin`, `quicbaas_mfa_enroll_cancel`, `quicbaas_mfa_enroll_complete`, `quicbaas_mfa_status`, `quicbaas_pep_check`, `quicbaas_pep_check_batch`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|

### `quicbaas.Auth` — SDK package `sdk/go/client/auth`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 6; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `BeginFederatedLogin` | `POST /api/v1/auth/beginfederatedlogin` *(default branch)* | **no** | yes | yes | MISSING | — |
| `GetJWKS` | `POST /api/v1/auth/getjwks` *(default branch)* | **no** | yes | yes | MISSING | — |
| `GetOpenIDConfiguration` | `POST /api/v1/auth/getopenidconfiguration` *(default branch)* | **no** | yes | yes | MISSING | — |
| `GetProvider` | `POST /api/v1/auth/getprovider` *(default branch)* | **no** | yes | yes | MISSING | — |
| `GetSession` | `GET /api/v1/auth/session` | **no** | yes | yes | MISSING | — |
| `HandleFederatedCallback` | `POST /api/v1/auth/handlefederatedcallback` *(default branch)* | **no** | yes | yes | MISSING | — |
| `IntrospectToken` | `POST /api/v1/auth/introspecttoken` *(default branch)* | **no** | yes | yes | MISSING | — |
| `ListProviders` | `POST /api/v1/auth/listproviders` *(default branch)* | **no** | yes | yes | MISSING | — |
| `ListSessions` | `GET /api/v1/auth/sessions` | **no** | yes | yes | MISSING | — |
| `Login` | `POST /api/v1/auth/login` | **no** | yes | yes | yes | — |
| `Logout` | `POST /api/v1/iam/logout` | yes | yes | yes | yes | — |
| `RefreshToken` | `POST /api/v1/iam/refresh` | yes | yes | yes | yes | — |
| `RevokeAllSessions` | `POST /api/v1/auth/revokeallsessions` *(default branch)* | **no** | yes | yes | MISSING | — |
| `RevokeSession` | `DELETE /api/v1/auth/sessions/{id}` | **no** | yes | yes | MISSING | — |
| `ValidateToken` | `POST /api/v1/iam/validate` | yes | yes | yes | yes | — |


### IAM is fifteen sub-services, not one

`sdk/go/client/iam` declares no single `serviceName`. It dispatches to fifteen
dotted sub-service names, each carrying its own method set. This is the shape
every transport sees, so it is the shape to reason about:

| IAM sub-service | SDK methods |
|---|---|
| `quicbaas.IAM.Admin` | `CheckPermission`, `CreateAssignment`, `DeleteAssignment`, `EndImpersonation`, `GetAssignment`, `ListActiveImpersonations`, `ListAssignments`, `StartImpersonation`, `UpdateAssignment` |
| `quicbaas.IAM.App` | `Delete`, `Get`, `List`, `Register`, `RotateSecret`, `Update` |
| `quicbaas.IAM.Audit` | `Delete`, `Get`, `Query`, `Record`, `RecordAsync` |
| `quicbaas.IAM.Federation` | `CreateProvider`, `DeleteProvider`, `GetProvider`, `ListProviders`, `UpdateProvider` |
| `quicbaas.IAM.GDPR` | `CancelDeletion`, `GenerateExport`, `GetConsent`, `GetDeletionStatus`, `GetExportStatus`, `GrantConsent`, `ListConsents`, `RequestDeletion`, `RequestExport`, `WithdrawConsent` |
| `quicbaas.IAM.Identity` | `LinkIdentity`, `ListUserIdentities`, `UnlinkIdentity` |
| `quicbaas.IAM.Invite` | `ActivateFIDO2Begin`, `ActivateFIDO2Finish`, `ActivatePIV`, `GetInvite` |
| `quicbaas.IAM.MFA` | `BeginMFAEnrollment`, `CompleteMFAEnrollment`, `DisableMFA`, `GenerateBackupCodes`, `GetBackupCodeStatus`, `GetMFAStatus` |
| `quicbaas.IAM.PEP` | `Check`, `CheckBatch` |
| `quicbaas.IAM.Policy` | `Create`, `Delete`, `Get`, `GetEffectivePermissions`, `List`, `Simulate`, `Test`, `Update`, `Validate` |
| `quicbaas.IAM.Role` | `Create`, `Delete`, `Get`, `List`, `Update` |
| `quicbaas.IAM.Session` | `Get`, `List`, `Revoke`, `RevokeAll`, `RevokeOthers`, `Touch` |
| `quicbaas.IAM.Tenant` | `Create`, `Delete`, `Get`, `List`, `Update` |
| `quicbaas.IAM.User` | `Approve`, `AssignRole`, `Create`, `Delete`, `FindByUsername`, `Get`, `GetByEmail`, `List`, `Reject`, `RevokeRole`, `Suspend`, `Unsuspend`, `Update` |

Every transport resolves these names by table, not by derivation:

- **gRPC and Unix** — `sdk/go/transport/grpc/transport.go` routes anything
  matching `isIAMService` to the typed IAM handler
  (`iam_handler.go` plus the per-subsystem files `iam_admin_handlers.go`,
  `iam_federation_handlers.go`, `iam_gdpr_handlers.go`,
  `iam_identity_handlers.go`, `iam_pep_mfa_handlers.go`,
  `iam_policy_handlers.go`, `iam_session_handlers.go`). Unix is gRPC over
  `AF_UNIX` and inherits all of it.
- **REST and QUIC** — `sdk/go/transport/rest/iam_handler.go`. The server side is
  127 permissioned routes under `/api/v1/iam`, `/api/v1/admin`,
  `/api/v1/tenants` and their siblings; each route's required
  `resource:action` is in `pkg/api/rest/routes_permissions_generated.go`.
- **MCP** — 91 registered tools. IAM tool names are **not** derived from the
  service/method pair: `ToolName` (`sdk/go/transport/mcp/transport.go:882`)
  consults an explicit lookup table first, precisely because the names are
  non-uniform. The registered set is:

  - `quicbaas_iam_admin_assignment_create`
  - `quicbaas_iam_admin_assignment_delete`
  - `quicbaas_iam_admin_assignment_get`
  - `quicbaas_iam_admin_assignment_list`
  - `quicbaas_iam_admin_assignment_update`
  - `quicbaas_iam_admin_check_permission`
  - `quicbaas_iam_app_delete`
  - `quicbaas_iam_app_get`
  - `quicbaas_iam_app_list`
  - `quicbaas_iam_app_register`
  - `quicbaas_iam_app_rotate_secret`
  - `quicbaas_iam_app_update`
  - `quicbaas_iam_audit_delete`
  - `quicbaas_iam_audit_get`
  - `quicbaas_iam_audit_query`
  - `quicbaas_iam_federation_beginlogin`
  - `quicbaas_iam_federation_createprovider`
  - `quicbaas_iam_federation_deleteprovider`
  - `quicbaas_iam_federation_getprovider`
  - `quicbaas_iam_federation_getuserlinks`
  - `quicbaas_iam_federation_handlecallback`
  - `quicbaas_iam_federation_linkaccount`
  - `quicbaas_iam_federation_listproviders`
  - `quicbaas_iam_federation_unlinkaccount`
  - `quicbaas_iam_federation_updateprovider`
  - `quicbaas_iam_gdpr_consent_get`
  - `quicbaas_iam_gdpr_consent_grant`
  - `quicbaas_iam_gdpr_consent_list`
  - `quicbaas_iam_gdpr_consent_withdraw`
  - `quicbaas_iam_gdpr_deletion_cancel`
  - `quicbaas_iam_gdpr_deletion_process`
  - `quicbaas_iam_gdpr_deletion_request`
  - `quicbaas_iam_gdpr_deletion_status`
  - `quicbaas_iam_gdpr_export_generate`
  - `quicbaas_iam_gdpr_export_request`
  - `quicbaas_iam_gdpr_export_status`
  - `quicbaas_iam_identity_linkidentity`
  - `quicbaas_iam_identity_listuseridentities`
  - `quicbaas_iam_identity_unlinkidentity`
  - `quicbaas_iam_impersonation_end`
  - `quicbaas_iam_impersonation_list`
  - `quicbaas_iam_impersonation_start`
  - `quicbaas_iam_policy_create`
  - `quicbaas_iam_policy_delete`
  - `quicbaas_iam_policy_get`
  - `quicbaas_iam_policy_list`
  - `quicbaas_iam_policy_test`
  - `quicbaas_iam_policy_update`
  - `quicbaas_iam_role_create`
  - `quicbaas_iam_role_delete`
  - `quicbaas_iam_role_get`
  - `quicbaas_iam_role_list`
  - `quicbaas_iam_role_update`
  - `quicbaas_iam_session_get`
  - `quicbaas_iam_session_list`
  - `quicbaas_iam_session_revoke`
  - `quicbaas_iam_session_revoke_all`
  - `quicbaas_iam_session_revoke_others`
  - `quicbaas_iam_session_touch`
  - `quicbaas_iam_tenant_create`
  - `quicbaas_iam_tenant_delete`
  - `quicbaas_iam_tenant_get`
  - `quicbaas_iam_tenant_list`
  - `quicbaas_iam_tenant_update`
  - `quicbaas_iam_user_assign_role`
  - `quicbaas_iam_user_create`
  - `quicbaas_iam_user_delete`
  - `quicbaas_iam_user_get`
  - `quicbaas_iam_user_list`
  - `quicbaas_iam_user_revoke_role`
  - `quicbaas_iam_user_update`
  - `quicbaas_mfa_backup_codes_generate`
  - `quicbaas_mfa_backup_codes_status`
  - `quicbaas_mfa_backup_codes_verify`
  - `quicbaas_mfa_challenge_begin`
  - `quicbaas_mfa_challenge_verify`
  - `quicbaas_mfa_disable`
  - `quicbaas_mfa_enroll_begin`
  - `quicbaas_mfa_enroll_cancel`
  - `quicbaas_mfa_enroll_complete`
  - `quicbaas_mfa_status`
  - `quicbaas_pep_check`
  - `quicbaas_pep_check_batch`

  The fallback, used only when the lookup table misses, is
  `quicbaas_iam_<subsystem>_<snake_case_method>`.
- **Embedded** — `sdk/go/transport/embedded/iam_dispatch.go` and
  `handlers_iam.go`.

### `quicbaas.Auth` — SDK package `sdk/go/client/auth`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 6; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `BeginFederatedLogin` | `POST /api/v1/auth/beginfederatedlogin` *(default branch)* | **no** | yes | yes | MISSING | — |
| `GetJWKS` | `POST /api/v1/auth/getjwks` *(default branch)* | **no** | yes | yes | MISSING | — |
| `GetOpenIDConfiguration` | `POST /api/v1/auth/getopenidconfiguration` *(default branch)* | **no** | yes | yes | MISSING | — |
| `GetProvider` | `POST /api/v1/auth/getprovider` *(default branch)* | **no** | yes | yes | MISSING | — |
| `GetSession` | `GET /api/v1/auth/session` | **no** | yes | yes | MISSING | — |
| `HandleFederatedCallback` | `POST /api/v1/auth/handlefederatedcallback` *(default branch)* | **no** | yes | yes | MISSING | — |
| `IntrospectToken` | `POST /api/v1/auth/introspecttoken` *(default branch)* | **no** | yes | yes | MISSING | — |
| `ListProviders` | `POST /api/v1/auth/listproviders` *(default branch)* | **no** | yes | yes | MISSING | — |
| `ListSessions` | `GET /api/v1/auth/sessions` | **no** | yes | yes | MISSING | — |
| `Login` | `POST /api/v1/auth/login` | **no** | yes | yes | yes | — |
| `Logout` | `POST /api/v1/iam/logout` | yes | yes | yes | yes | — |
| `RefreshToken` | `POST /api/v1/iam/refresh` | yes | yes | yes | yes | — |
| `RevokeAllSessions` | `POST /api/v1/auth/revokeallsessions` *(default branch)* | **no** | yes | yes | MISSING | — |
| `RevokeSession` | `DELETE /api/v1/auth/sessions/{id}` | **no** | yes | yes | MISSING | — |
| `ValidateToken` | `POST /api/v1/iam/validate` | yes | yes | yes | yes | — |


## OIDC Endpoints

### Well-Known Configuration

```
GET /.well-known/openid-configuration
```

Returns OpenID Connect discovery document.

### Authorization

```
GET /oauth2/authorize
```

Parameters:
| Parameter | Required | Description |
|-----------|----------|-------------|
| `client_id` | Yes | Application client ID |
| `redirect_uri` | Yes | Callback URL |
| `response_type` | Yes | `code` for authorization code flow |
| `scope` | Yes | Space-separated scopes |
| `state` | Recommended | CSRF protection |
| `code_challenge` | PKCE | Base64url-encoded SHA256 hash |
| `code_challenge_method` | PKCE | `S256` |

On success the server answers `302 Found` to `redirect_uri` with `code`,
`state` (when sent) and `iss` (the issuer) added to any query the registered
URI already has.

Errors follow draft-ietf-oauth-v2-1-16 §4.1.2.1:

- When `client_id` is missing or unknown, or `redirect_uri` is missing or not
  registered for the client, the server never redirects: it answers `400`
  with a JSON error body.
- Every later error (missing or unsupported `response_type`, missing PKCE,
  `plain` PKCE, ...) is a `302 Found` to the validated `redirect_uri` with
  `error`, `error_description`, `state` (when sent) and `iss`.

### Token Exchange

```
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded
```

**Authorization Code Grant:**
```
grant_type=authorization_code
&code=<auth_code>
&redirect_uri=<callback_url>
&client_id=<client_id>
&client_secret=<client_secret>
&code_verifier=<pkce_verifier>
```

**Refresh Token Grant:**
```
grant_type=refresh_token
&refresh_token=<token>
&client_id=<client_id>
&client_secret=<client_secret>
```

**Client Credentials Grant:**
```
grant_type=client_credentials
&client_id=<client_id>
&client_secret=<client_secret>
&scope=<scopes>
```

Response:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "dGhpcyBpcyBhIHJl...",
  "id_token": "eyJhbGciOiJSUzI1NiIs...",
  "scope": "openid profile email"
}
```

### User Info

```
GET /oauth2/userinfo
Authorization: Bearer <access_token>
```

Response:
```json
{
  "sub": "user_abc123",
  "name": "Alice Smith",
  "email": "alice@acme.com",
  "email_verified": true,
  "tenant_id": "acme-corp",
  "roles": ["developer", "team-lead"],
  "groups": ["Developers", "Team Leads"]
}
```

`roles` carries the IDs of the roles the user is bound to; `groups` carries
those roles' names. An account holds no group list of its own.

### JWKS

```
GET /.well-known/jwks.json
```

Returns JSON Web Key Set for token verification.

---

## IAM Management API

### Tenants

**List Tenants**
```
GET /api/v1/tenants
Authorization: Bearer <admin_token>
```

**Create Tenant**
```
POST /api/v1/tenants
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "id": "acme-corp",
  "name": "Acme Corporation",
  "settings": {
    "max_users": 1000,
    "mfa_required": true
  },
  "isolation_type": "barrier",
  "engine_quotas": {
    "max_databases": 20,
    "max_storage_bytes": 107374182400,
    "system_barrier_quota": 0
  },
  "engine_entitlements": {
    "allowed_isolation_modes": ["prefix", "barrier"],
    "allow_dedicated_raft": true
  }
}
```

`isolation_type`, `engine_quotas` and `engine_entitlements` are the engine-side
provisioning knobs. All three are optional and omitting them defers to the
engine's configured defaults, so a client that predates them is unaffected.

- `isolation_type` — `prefix` (default), `barrier`, `volume` or `raft`. The
  engine binds a tenant's isolation at creation, so this is **create-only**:
  there is no update path, because moving an existing tenant between isolation
  strategies would mean moving its data. The reserved system tenant must stay
  `prefix`.
- `engine_quotas` — the engine's numeric limits (`max_databases`, `max_keys`,
  `max_storage_bytes`, `system_barrier_quota`, ...). `-1` means unlimited and
  `0` means "use the system default"; an omitted object means "leave the
  engine's configured defaults alone", which is *not* the same as sending an
  empty object. Applied immediately after the tenant exists, so a provisioning
  caller never races a new tenant running on defaults.
- `engine_entitlements` — the engine's feature grants
  (`allowed_isolation_modes`, `allow_dedicated_raft`, `allow_volume_storage`,
  `allow_system_managed_barrier`, `allow_dedicated_wal`,
  `allowed_barrier_modes`).

A malformed `engine_quotas` or `engine_entitlements` object is rejected with
`400`; it is never silently zeroed, because a zeroed quota set is a real
instruction with different meaning. Both objects are decoded strictly, so an
unrecognized key (`max_databasses`) is a `400` rather than an empty override
that would quietly replace the engine's configured defaults. A literal `null`
means "not supplied" and leaves the defaults in place.

`engine_quotas` accepts the configured **limits only**. The `current_*` usage
counters are engine-owned outputs and are rejected with `400` on every
transport, whatever their value — the gRPC create request models limits only,
so accepting them here would give the same request a different meaning per
transport. Correct a counter through
`PATCH /api/v1/tenants/{tenant_id}/quotas`, never tenant create.

The same three fields travel on every transport: `isolation_type` /
`engine_quotas` / `engine_entitlements` on the gRPC `CreateTenantRequest`
(and therefore Unix), the same JSON keys over REST and QUIC, the same argument
names on the `quicbaas_iam_tenant_create` MCP tool, and the corresponding
fields on the SDK's `iam.CreateTenantRequest` for the embedded transport.

**Get Tenant**
```
GET /api/v1/tenants/{tenant_id}
```

**Update Tenant**
```
PUT /api/v1/tenants/{tenant_id}
```

**Delete Tenant**
```
DELETE /api/v1/tenants/{tenant_id}
```

**Provision Tenant**
```
POST /api/v1/tenants/{tenant_id}/provision
```

Creates the tenant and its first admin in one call. This is the sysadmin
onboarding path for deployments that keep public registration
(`/api/v1/auth/register/*`) off. Requires `tenants:create`, the same permission
as tenant create.

```json
{
  "name": "Acme Corporation",
  "isolation_type": "barrier",
  "admin_username": "admin",
  "admin_enrollment": "invite",
  "invite_expiry_hours": 72
}
```

- `admin_enrollment` — `cert` (default) or `invite`.
  - `cert` signs `admin_csr_pem` (required) and binds the certificate's SPKI
    thumbprint to the admin. The response carries `admin_cert_pem` and
    `admin_cert_thumbprint`. No private key ever crosses the wire.
  - `invite` creates the admin as pending and returns a one-time
    `invite_token` and `invite_expires_at`. Supplying a CSR in this mode is a
    `400`, and so is invite mode for the system tenant. The admin activates
    with the token through [invite activation](#invite-activation).
- `isolation_type` — empty defaults to `barrier`, **not** `prefix` as on tenant
  create. Provisioning does not initialize the tenant barrier.
- `invite_expiry_hours` — zero selects the invite store default. A negative
  value is a `400`.
- `admin_username` — defaults to `admin`.

A `tenant_id` in the body that differs from the path is a `400`, and so is an
unknown field. The response is `201`:

```json
{
  "tenant_id": "acme-corp",
  "admin_user_id": "7",
  "admin_username": "admin",
  "admin_role_id": "...",
  "admin_pending": true,
  "invite_token": "...",
  "invite_expires_at": "2026-10-15T12:00:00Z",
  "timestamp": "2026-10-12T12:00:00Z"
}
```

The same request reaches the server over gRPC as
`IAMService/ProvisionTenant` (and therefore over Unix), over MCP as the
`quicbaas_iam_tenant_provision` tool, and from the SDK as
`iam.TenantClient.Provision` on every transport, embedded included.

### Invite Activation

A user created `pending` (including an `invite`-mode tenant admin) activates
with its one-time invite token. These verbs are **pre-auth**: the token is the
credential, so no bearer token is required, and REST, gRPC, Unix and the
embedded transport all redeem through the same service
(`service.InviteActivationService`).

| SDK method (`quicbaas.IAM.Invite`) | REST route | gRPC (`IAMService/…`) |
|---|---|---|
| `GetInvite(ctx, tenantID, token)` | `GET /api/v1/iam/{tenant_id}/invites/{token}` | `GetInvite` |
| `ActivateFIDO2Begin(ctx, tenantID, token)` | `POST …/invites/{token}/activate/fido2/begin` | `ActivateFIDO2Begin` |
| `ActivateFIDO2Finish(ctx, tenantID, token, credential)` | `POST …/invites/{token}/activate/fido2/finish` | `ActivateFIDO2Finish` |
| `ActivatePIV(ctx, tenantID, token, certPEM)` | `POST …/invites/{token}/activate/piv` | `ActivatePIV` |

The four gRPC methods are in the public method set and carry no permission
option. QUIC carries the REST rows over HTTP/3 and Unix carries the gRPC
column; `sdk/go/transport/{quic,unix}/*invite*_test.go` pin both, including the
refusal of a request without the tenant the routes require. There are no MCP
invite tools yet; they wait on a decision about public (pre-auth) MCP tools.

`GetInvite` returns the invite's public metadata:

```json
{
  "user_id": "7",
  "email": "admin@acme.example",
  "name": "Admin",
  "auth_methods": ["webauthn", "piv"],
  "expires_at": "2026-10-15T12:00:00Z"
}
```

`ActivateFIDO2Begin` returns the WebAuthn `CredentialCreationOptions` for
`navigator.credentials.create()`. `ActivateFIDO2Finish` takes the browser's
`PublicKeyCredential` as `{"credential": {...}}`; `ActivatePIV` takes
`{"cert_pem": "..."}`, whose chain the server's PIV trust pool must verify.
Both return:

```json
{"user_id": "7", "message": "activation successful", "warning": ""}
```

gRPC carries the FIDO2 options and credential as JSON strings
(`options_json`, `credential_json`).

| Condition | REST | gRPC |
|---|---|---|
| Token absent, expired or already used (one opaque answer) | `404` | `NotFound` |
| User is not pending | `409` | `FailedPrecondition` |
| PIV chain verification failed | `422` | `InvalidArgument` |
| Missing `tenant_id`, token, credential or `cert_pem` | `400` | `InvalidArgument` |
| PIV activation not configured | `501` | `Unimplemented` |

### Users

**List Users**
```
GET /api/v1/tenants/{tenant_id}/users
```

Query Parameters:
| Parameter | Description |
|-----------|-------------|
| `limit` | Max results (default 100) |
| `offset` | Pagination offset |
| `email` | Filter by email |
| `role` | Filter by role |

**Create User**
```
POST /api/v1/tenants/{tenant_id}/users
Content-Type: application/json

{
  "email": "alice@acme.com",
  "name": "Alice Smith",
  "roles": ["developer"],
  "attributes": {
    "department": "engineering",
    "team_id": "team-west"
  }
}
```

A user carries no group list, and quicbaas keeps no groups: identity is qrdb's,
and a role binding is the grouping it offers. The OIDC `groups` claim carries
the names of the roles the user is bound to.

**Get User**
```
GET /api/v1/tenants/{tenant_id}/users/{user_id}
```

**Update User**
```
PUT /api/v1/tenants/{tenant_id}/users/{user_id}
```

**Delete User**
```
DELETE /api/v1/tenants/{tenant_id}/users/{user_id}
```

**Assign Role**
```
POST /api/v1/tenants/{tenant_id}/users/{user_id}/roles
Content-Type: application/json

{
  "role": "admin"
}
```

**Remove Role**
```
DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role}
```

### Roles

**List Roles**
```
GET /api/v1/tenants/{tenant_id}/roles
```

**Create Role**
```
POST /api/v1/tenants/{tenant_id}/roles
Content-Type: application/json

{
  "id": "team-lead",
  "name": "Team Lead",
  "description": "Can manage team resources"
}
```

**Reconcile Built-in Roles**
```
POST /api/v1/tenants/{tenant_id}/roles/reconcile
```

Converges the tenant's built-in roles (`admin`, `operator`, `so`, `user`,
`auditor`, `custodian`) onto the definitions the running build carries: verbs
the build now grants are added, verbs it no longer grants are removed, and
custom roles are left untouched. It is the ONLY path that can re-permission a
built-in role — every ordinary update refuses one — and it is idempotent, so a
converged tenant reports no changed roles and nothing is written.

Authorization is the role-CREATE grant, deliberately: a dedicated
`roles:reconcile` verb would be absent from every role seeded before it existed,
so nobody would hold the grant needed to repair that exact absence.

The daemon runs this for every tenant at startup, after the modules have
contributed their permissions, so an operator normally never has to call it.

Response:
```json
{
  "tenant_id": "acme",
  "changed": true,
  "roles": [
    {
      "role_id": "5aee453e-e14b-8818-8b0d-9268c33a0232",
      "name": "admin",
      "added": ["sdlc:bind", "sdlc:unbind"],
      "removed": ["sdlc:register", "sdlc:update", "sdlc:deregister"]
    }
  ]
}
```

**Drift on read.** Every role read (`GET .../roles` and `GET .../roles/{role_id}`)
carries `retired_permissions`: the members of `permissions` the running build no
longer defines. It is derived from the live permission registry on each read and
never stored, so it shows the disagreement BEFORE reconciliation repairs it.

```json
{
  "id": "5aee453e-e14b-8818-8b0d-9268c33a0232",
  "name": "admin",
  "permissions": ["sdlc:read", "sdlc:register"],
  "retired_permissions": ["sdlc:register"]
}
```

### Applications (OAuth Clients)

**List Applications**
```
GET /api/v1/tenants/{tenant_id}/apps
```

**Register Application**
```
POST /api/v1/tenants/{tenant_id}/apps
Content-Type: application/json

{
  "name": "order-service",
  "redirect_uris": [
    "https://orders.acme.com/callback"
  ],
  "resource_prefix": "orders://",
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"]
}
```

Response:
```json
{
  "client_id": "app_ord_abc123xyz",
  "client_secret": "sk_live_xxxxxxxxxxxxx",
  "name": "order-service",
  "redirect_uris": ["https://orders.acme.com/callback"],
  "resource_prefix": "orders://"
}
```

**Rotate Client Secret**
```
POST /api/v1/tenants/{tenant_id}/apps/{client_id}/rotate-secret
```

---

## Policy API

Decisions are go-qrdb's: QuicBaaS stores the tenant's policy documents and
reloads them into the embedded node's ABAC authorizer on every write. See
[ABAC Authorization Decorator](../../../go-qrdb/docs/auth/abac.md) for the
evaluation semantics, and
[Authorization (ABAC) and the PEP](architecture.md#authorization-abac-and-the-pep)
for the QuicBaaS-owned policy store that is still in place.

| Method | Path (`/api/v1/tenants/{tenant_id}` +) | Permission (`resource:action`) |
|--------|----------------------------------------|--------------------------------|
| GET | `/policies/` | `tenants:read` |
| POST | `/policies/` | `tenants:create` |
| GET | `/policies/{policy_name}` | `tenants:read` |
| PUT | `/policies/{policy_name}` | `tenants:create` |
| DELETE | `/policies/{policy_name}` | `tenants:delete` |
| POST | `/policies/validate` | `tenants:create` |
| POST | `/policies/test` | `tenants:create` |
| POST | `/policies/simulate` | `tenants:create` |
| POST | `/policies/effective-permissions` | `tenants:create` |

Over gRPC, the same operations are `IAMService` `CreatePolicy`, `GetPolicy`,
`UpdatePolicy`, `DeletePolicy`, `ListPolicies`, `ValidatePolicy` and
`TestPolicy` (`api/proto/iam/iam.proto`).

Over REST, creating a policy whose name already exists in the tenant answers
`409` with the `conflict` error body.

Write responses are what the store holds after the write, not the request:

- **Create** (REST `POST /policies/`, gRPC `CreatePolicy`, MCP
  `quicbaas_iam_policy_create`): the response is the stored policy, read
  back after the write.
- **Update** (REST `PUT /policies/{policy_name}`, gRPC `UpdatePolicy`, MCP
  `quicbaas_iam_policy_update`): the response is the stored policy, read
  back after the write. The request's policy name selects the target. Over
  gRPC, the stored spec is replaced only when `spec_yaml` is given.

The requested policy name and a document's `metadata.name` must agree
(`iam.CheckPolicyName`; a document that declares no name takes the requested
one). On every transport (REST, gRPC, MCP):

- **Update** refuses a document naming a different policy with an invalid-input
  error (validation class: REST `400`, gRPC `InvalidArgument`) carrying the
  `CheckPolicyName` message (`iam.PolicyNameMismatchError`); nothing is stored.
- **Validate** reports the same case as `valid: false` with that message in
  `errors`.

The embedded transport serves no policy Update or Validate verb.

A policy document (`pkg/iam/types.go` `Policy`):

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: order-access
  tenant: acme-corp
spec:
  subjects:
    authenticated: true
  rules:
    - name: read-own
      effect: allow
      actions: [read]
      resources:
        - orders://orders/${subject.id}/**
```

`/policies/test` returns `allowed`, `matched_policy`, `matched_rule` and
`evaluation_time_ns`.

---

## WebAuthn API

The ceremonies, their request and response bodies
(`PublicKeyCredentialCreationOptions`, the raw `navigator.credentials.*`
JSON) and their verification are go-xkms's:
[WebAuthn Relying Party — API](../../../go-xkms/docs/webauthn/api.md). QuicBaaS
mounts the go-xkms relying party per tenant at
`/api/v1/iam/{tenant_id}/webauthn/*` (`pkg/api/rest/iam_xkms_webauthn_mount.go`):

| Route | Permission |
|---|---|
| `POST /api/v1/iam/{tenant_id}/webauthn/registration/begin` | `user_authfactors:add` |
| `POST /api/v1/iam/{tenant_id}/webauthn/registration/finish` | `user_authfactors:add` |
| `GET /api/v1/iam/{tenant_id}/webauthn/registration/status?user_id=<id>` | `user_authfactors:get` |
| `POST /api/v1/iam/{tenant_id}/webauthn/login/begin` | public (the ceremony is the authentication) |
| `POST /api/v1/iam/{tenant_id}/webauthn/login/finish` | public |

The ceremony session id travels in the `X-Session-Id` header: the begin verbs
return it and the finish verbs echo it. The tenant in the path selects the
relying party (RP id and origins); there is no default-tenant fallback.

Registration write-through enrolls the credential as a go-qrdb `fido2` user
auth factor, so login can resolve it and it appears under
`GET /v1/users/factors/list`.

The mount's login token is not an OIDC bearer. Clients that need the
OIDC access/refresh pair (SPA, TypeScript SDK, CLI) authenticate
through the unified login ceremony instead:
`POST /api/v1/auth/login/begin` (`type: fido2`) then
`POST /api/v1/auth/login/finish`, which verifies the assertion and
mints the bearer plus rotating refresh token.

### Credential Management

Enrolled passkeys are qrdb user auth factors. List, remove, rotate,
and relabel them via the auth-factor surface
(`/v1/users/factors/*` REST wire, `Security().Auth().Factors()` SDK,
`quicbaas auth factor` CLI).

The credentials the WebAuthn store holds for a user are listed and deleted
per user (`pkg/api/rest/routes_iam.go`; also over gRPC, MCP and Unix), under
`/api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials`:

| Route | Permission |
|---|---|
| `GET …/webauthn/credentials/` | `users:read` |
| `DELETE …/webauthn/credentials/` | `users:delete` |
| `DELETE …/webauthn/credentials/{credential_id}` | `users:delete` |

---

## Session Management API

### List Sessions

```
GET /api/v1/iam/sessions
Authorization: Bearer <access_token>
```

Returns all active sessions for the authenticated user.

Response:
```json
{
  "sessions": [
    {
      "id": "sess_abc123",
      "created": "2025-01-15T10:30:00Z",
      "last_active": "2025-01-15T14:22:00Z",
      "expires_at": "2025-01-15T18:30:00Z",
      "mfa_verified": true,
      "ip_address": "203.0.113.100",
      "user_agent": "Mozilla/5.0...",
      "device_info": "Chrome on macOS"
    }
  ]
}
```

### Get Session

```
GET /api/v1/iam/sessions/{session_id}
Authorization: Bearer <access_token>
```

### Revoke Session

```
DELETE /api/v1/iam/sessions/{session_id}
Authorization: Bearer <access_token>
```

### Revoke All Sessions

```
DELETE /api/v1/iam/sessions
Authorization: Bearer <access_token>
```

Query Parameters:
| Parameter | Description |
|-----------|-------------|
| `except` | Session ID to keep active (optional) |

### Revoke Other Sessions

```
POST /api/v1/iam/sessions/revoke-others
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "keep_session_id": "sess_current123"
}
```

---

## Audit Logging API

### Query Audit Events

```
GET /api/v1/iam/audit
Authorization: Bearer <admin_token>
```

**Required Permission**: `audit:query` (the single-event
`GET /api/v1/iam/audit/{id}` requires `audit:read`)

Query Parameters:
| Parameter | Description |
|-----------|-------------|
| `tenant_id` | Target tenant (optional). Defaults to the caller's own authenticated tenant and must equal it — any other value is rejected 403, the system-tenant principal included (go-quicbaas#356) |
| `event_type` | Filter by event type (e.g., "user.login", "policy.created") |
| `category` | Filter by category ("authentication", "authorization", "management") |
| `outcome` | Filter by outcome ("success", "failure") |
| `actor_id` | Filter by actor ID |
| `resource_id` | Filter by resource ID |
| `start_time` | Events after this time (RFC3339) |
| `end_time` | Events before this time (RFC3339) |
| `limit` | Max results (default 100, max 1000) |
| `offset` | Pagination offset |

Response:
```json
{
  "events": [
    {
      "id": "evt_abc123",
      "tenant_id": "acme-corp",
      "event_type": "user.login",
      "category": "authentication",
      "outcome": "success",
      "actor_id": "user_xyz",
      "actor_type": "user",
      "ip_address": "203.0.113.100",
      "user_agent": "Mozilla/5.0...",
      "timestamp": "2025-01-15T10:30:00Z",
      "details": {
        "method": "webauthn",
        "mfa_used": true
      }
    }
  ],
  "total": 150,
  "limit": 100,
  "offset": 0
}
```

### Get Audit Event

```
GET /api/v1/iam/audit/{event_id}?tenant_id={tenant_id}
Authorization: Bearer <admin_token>
```

**Required Permission**: `audit:read`

### Delete Old Audit Events

```
DELETE /api/v1/iam/audit?tenant_id={tenant_id}&before={timestamp}
Authorization: Bearer <admin_token>
```

**Required Permission**: `audit:delete`

**Access Control**: This endpoint is restricted to administrators with the `audit:delete` permission. Only users with the `super_admin` role have this permission by default. Tenant admins and audit admins do NOT have delete permissions to preserve audit trail integrity.

| Admin Role | Has audit:delete |
|------------|------------------|
| `super_admin` | Yes |
| `tenant_admin` | No |
| `audit_admin` | No |
| `user_admin` | No |

Response:
```json
{
  "deleted_count": 1234,
  "before": "2024-01-01T00:00:00Z"
}
```

Error Response (403 Forbidden):
```json
{
  "error": "forbidden",
  "error_description": "The audit:delete permission is required to delete audit events"
}
```

---

## MFA API

TOTP codes are derived with HMAC-SHA1 by default (RFC 6238) and with
HMAC-SHA256 in FIPS mode, where SHA1 is not an approved algorithm. Each
enrollment stores the algorithm it was provisioned with, so changing the
default never invalidates an existing enrollment. See
[Security Hardening: TOTP in FIPS Mode](../security-hardening/engineering.md#totp-in-fips-mode).

TOTP MFA is QuicBaaS's own (`pkg/iam/mfa.go`); go-xkms and go-qrdb document
no TOTP factor. The routes are per user under
`/api/v1/tenants/{tenant_id}/users/{user_id}` (`pkg/api/rest/routes_iam.go`):

| Route | Handler | Body → response | Permission |
|---|---|---|---|
| `GET …/mfa/` | `GetMFAStatus` | → `MFAStatus` | `tenants:read` |
| `DELETE …/mfa/` | `DisableMFA` | — | `tenants:delete` |
| `POST …/mfa/enroll` | `BeginMFAEnrollment` | `MFAEnrollRequest` → `MFAEnrollResponse` (`enrollment_id`, `qr_code_uri`, `secret`, `expires_at`) | `tenants:create` |
| `POST …/mfa/verify` | `CompleteMFAEnrollment` | `MFAVerifyRequest` (`code`, `enrollment_id`) → `MFAVerifyResponse` | `tenants:create` |
| `DELETE …/mfa/enroll/{enrollment_id}` | `CancelMFAEnrollment` | — | `tenants:delete` |
| `POST …/mfa/challenge` | `BeginMFAChallenge` | → `MFAChallenge` | `tenants:create` |
| `POST …/mfa/verify-challenge` | `VerifyMFAChallenge` | `MFAVerifyRequest` (`code`, `challenge_id`, `use_backup_code`) → `MFAVerifyResponse` | `tenants:create` |
| `GET …/mfa/backup-codes` | `GetBackupCodeStatus` | → `BackupCodeStatus` (`total`, `remaining`, `used`, `generated_at`) | `tenants:read` |
| `POST …/mfa/backup-codes` | `GenerateBackupCodes` | → the new codes, shown once | `tenants:create` |

The types are in `pkg/iam/mfa_types.go`.

---

## PEP gRPC API

### Service Definition

`PEPService` (`api/proto/iam/iam.proto`, served by
`pkg/api/grpc/server_pep.go`). `Check` and `CheckBatch` forward to go-qrdb's
authorizer (`node.RBAC().Authorize`). `Subscribe` streams policy documents
from the QuicBaaS policy store. This is a departure; see
[Authorization (ABAC) and the PEP](architecture.md#authorization-abac-and-the-pep).

```protobuf
service PEPService {
  rpc Check(CheckRequest) returns (CheckResponse);
  rpc CheckBatch(CheckBatchRequest) returns (CheckBatchResponse);
  rpc Subscribe(SubscribeRequest) returns (stream PolicyUpdate);
}
```

`CheckBatchRequest` carries one subject and action with a list of
`resources`. `CheckBatchResponse` returns a parallel `results` list of bools.
`PolicyUpdate.type` is one of `SNAPSHOT`, `CREATED`, `UPDATED` or `DELETED`.

### PEP REST API

| Method | Path | Permission |
|--------|------|------------|
| POST | `/api/v1/iam/check` | `permissions:resolve` |
| POST | `/api/v1/iam/check/batch` | `permissions:resolve` |
| POST | `/api/v1/iam/batch-check` | `permissions:resolve` (alias of `check/batch`) |

A request body is `{"subject": {...}, "action": "...", "resource": "..."}`,
and a batch request carries a list of `checks`.

---

## Error Responses

All errors follow this format:

```json
{
  "error": "invalid_request",
  "error_description": "The redirect_uri is not registered",
  "error_uri": "https://docs.quicbaas.io/iam/errors#invalid_request"
}
```

### OAuth2 Errors

| Error | Description |
|-------|-------------|
| `invalid_request` | Missing or invalid parameter |
| `invalid_client` | Client authentication failed |
| `invalid_grant` | Invalid authorization code or refresh token |
| `unauthorized_client` | Client not authorized for this grant type |
| `unsupported_grant_type` | Grant type not supported |
| `invalid_scope` | Requested scope is invalid |

`invalid_client` from the token and introspection endpoints is a `401`. When
the client authenticated with HTTP Basic, the response also carries
`WWW-Authenticate: Basic realm="oauth2"` (draft-ietf-oauth-v2-1-16 §3.2.4).

### API Errors

| Status | Error | Description |
|--------|-------|-------------|
| 400 | `bad_request` | Invalid request body |
| 401 | `unauthorized` | Missing or invalid token |
| 403 | `forbidden` | Insufficient permissions |
| 404 | `not_found` | Resource not found |
| 409 | `conflict` | Resource already exists |
| 429 | `rate_limited` | Too many requests |
| 500 | `internal_error` | Server error |

### WebAuthn Errors

The mount returns go-xkms's WebAuthn errors unchanged; see
[go-xkms WebAuthn API: status and error mapping](../../../go-xkms/docs/webauthn/api.md#status-and-error-mapping).

---

## See Also

- [WebAuthn Guide](engineering.md) - Detailed WebAuthn integration guide
- [Audit Logging](../audit-logging/architecture.md) - Audit logging and permissions
- [ABAC Policy Syntax](../authorization/engineering.md) - Policy language reference
- [Developer Guide](engineering.md) - Integration overview
