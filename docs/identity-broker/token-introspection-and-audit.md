# Token Introspection (RFC 7662) + Dual-Identity Audit

Status: **Implemented (Phase 6)** · Targets: **FedRAMP Moderate/High, DoD IL4/IL5**

This document describes the broker's RFC 7662 token introspection endpoint, the
dual-identity audit trail, the platform access-token TTL, and the GovCloud
private-ingress posture (SC-7) for the introspection surface.

## 1. Introspection endpoint (RFC 7662)

The broker exposes `POST /oauth2/introspect`. A resource server submits a
form-encoded body with `token` (required) and an optional `token_type_hint`.
The endpoint is advertised in both OIDC discovery
(`/.well-known/openid-configuration`) and the OAuth 2.0 Authorization Server
Metadata (`/.well-known/oauth-authorization-server`) as `introspection_endpoint`.

### Response

`active=false` (and no other fields) is returned for any token that is:

- not signature-verifiable against the broker JWKS (kid-aware),
- expired (`exp` in the past) or not-yet-valid (`nbf` in the future), or
- **revoked** — looked up in the per-tenant revocation store by `jti`.

For an active token the standard claim set is returned: `active`, `sub`,
`tenant_id`, `scope` + `scp` (array form), `aud`, `iss`, `exp`, `iat`, `nbf`,
`jti`, `client_id`, `token_type` (`Bearer`), `cnf` (DPoP `jkt` / mTLS
`x5t#S256` when sender-constrained), `roles`, and `username` when known. The
endpoint fails closed: it never leaks claims for an invalid token.

### Tenant derivation — no global table

The broker has **no global token table**. The tenant is derived from the
token's own `tenant_id` claim after signature verification, and that tenant is
set on the context **before** the per-tenant revocation lookup. A caller cannot
influence which tenant store is consulted; the verified claim is authoritative.

### Caller authentication (RFC 7662 §2.1)

Introspection is privileged. An unauthenticated caller is rejected with
`401 invalid_client` before any token is inspected. Two caller-authentication
methods are accepted:

1. **mTLS** — a verified client certificate on the TLS chain. When a `client_id`
   is also supplied it is bound to the certificate via the registered mTLS
   authenticator (a stolen `client_id` cannot be asserted over a foreign cert).
2. **Registered client credentials** — `client_id`/`client_secret` via HTTP
   Basic or form body, validated against the OIDC provider's app storage.

The verified client certificate (when present) is also the **service identity**
recorded on the dual-identity audit entry.

### Reused crypto

Introspection reuses the OIDC provider's signature-verification + claim-parsing
path (`parseVerifiedAccessTokenClaims`) and the existing revocation store
(`IsTokenRevoked`). No crypto or revocation logic is duplicated.

## 2. Dual-identity audit (AU-2 / AU-3)

Authenticated requests emit an audit event carrying **both** principals:

- **Service identity** — the verified client-certificate identity: the
  certificate's `spiffe://` URI SAN when present, otherwise its Subject DN
  (`iam.ServiceIdentityFromCert`).
- **User identity** — `sub`, `tenant_id`, `scp`/scopes, `roles`, and `jti`
  carried by the access token.

The helper `iam.LogDualIdentity` builds the `AuditEvent` (actor carries both
identities) and stamps `tenant_id` on every event. It fails closed: a nil
logger or a missing tenant returns an error rather than dropping a security
event. The dual-identity entry satisfies AU-2/AU-3: who (service + user), what
(action), when (timestamp), tenant, and outcome.

Wired call sites:

- **Login** — `CeremonyFinishLogin` (`iam.login.finish`).
- **Token** — `POST /oauth2/token` (`oauth2.token`); the issued access token is
  validated locally to extract the authoritative `sub`/`tenant_id`/`scp`/`jti`
  rather than trusting request inputs.
- **Introspection** — `POST /oauth2/introspect` (`oauth2.introspect`), on the
  active path.

### Audit store layout — FLAG

The audit store (`iam.MemoryAuditStorage`, the default) is a **single process
store partitioned by `tenant_id`** (a `tenantIndex` map). It is **not** a
physically per-tenant store. Each event is stamped with `tenant_id` and queries
are tenant-scoped, so cross-tenant reads are not possible through the API, but
the events for all tenants live in one in-memory structure. For a deployment
that requires physically isolated per-tenant audit partitions (or durable,
tamper-evident storage for FedRAMP AU-9), a tenant-routed `AuditStorage`
backend must be supplied via `iam.WithAuditStorage`. This is called out so the
control-plane operator can make the storage-isolation decision explicitly.

## 3. Platform access-token TTL

The platform access token is short-lived. The broker default is **15 minutes**,
the upper bound of the required 5–15 minute window. Refresh tokens (7 days) and
ID tokens (1 hour) keep their longer lifetimes. Short access-token TTL bounds the
replay window for a compromised bearer; revocation + introspection cover the
in-window case.

These lifetimes are config-wired on `AuthConfig` and default to the values above
when unset (zero):

| Config key (`auth.*`)   | Default | Applies to                 |
| ----------------------- | ------- | -------------------------- |
| `access_token_ttl`      | 15m     | OIDC/OAuth2 access tokens  |
| `refresh_token_ttl`     | 7d      | OIDC refresh tokens        |
| `id_token_ttl`          | 1h      | OIDC ID tokens             |
| `auth_code_ttl`         | 10m     | OAuth2 authorization codes |

`access_token_ttl`, when set, is validated to stay within `[1m, 60m]`
(`DefaultOIDCAccessTokenTTLMin`..`Max`); out-of-range values are rejected by the
config validator. The provider is configured from these values in
`pkg/server/iam_init.go`.

## 4. GovCloud private ingress (SC-7)

The introspection endpoint is a privileged, machine-to-machine surface consumed
only by resource servers — never by end-user browsers. For GovCloud / IL4-IL5
deployments it MUST NOT be reachable from the public internet.

This is a **deployment-posture** requirement (SC-7, boundary protection), not
application code:

- Bind the broker's privileged endpoints to a **private subnet** / internal
  load balancer only; do not attach a public IP or public ALB listener to the
  broker.
- Restrict ingress to the introspection port with a security group / NACL that
  admits only the resource-server subnets (least privilege).
- Terminate mTLS at the broker (not at a public edge) so the verified client
  certificate reaches the introspection caller-authentication path intact.
- Front user-facing OIDC endpoints (authorize/login) separately if they must be
  publicly reachable; keep `/oauth2/introspect`, `/oauth2/revoke`, and the
  admin/audit APIs on the private listener.

The network boundary itself remains **operator-enforced** (private subnet + SG /
NACL); the application does not relocate the boundary into code. Two optional,
backward-compatible config knobs support the posture without changing it when
unset:

- **Per-protocol `bind_address`** (`server.{rest,grpc,quic,mcp,unix}.bind_address`)
  overrides the shared `server.host` for that listener only, so privileged
  protocols can bind a private interface independently of any public-facing one.
  Empty (default) = use `server.host` for every listener (current behavior).
  Resolved via `ServerConfig.ResolveBindHost`.
- **`security.require_mtls`** flips the server TLS client-auth posture from the
  default opportunistic `VerifyClientCertIfGiven` to strict
  `RequireAndVerifyClientCert`, rejecting any connection without a trusted client
  certificate. Default `false` preserves current behavior. Applied via
  `Server.optionalMTLSClientAuth` (`pkg/server/lifecycle.go`).

These are knobs, not a substitute for the network boundary: the operator still
places the private listeners on an internal interface and SG-restricts them.
