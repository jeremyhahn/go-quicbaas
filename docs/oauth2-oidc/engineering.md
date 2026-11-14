# OAuth 2.1 / OIDC — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/oauth2-oidc/`.

Status: **Implemented** · RFCs: 9449 (DPoP), 7800 (cnf), 7638 (JWK thumbprint)

This documents the Phase 2 deliverables: the `cnf.jkt` sender-constraint on the platform
access token, the reusable DPoP verifier, and the reusable SDK validator that downstream
services consume. Phase 1 (KMS/HSM-backed ES256 signing key, `kid` + multi-kid
JWKS, kid-aware validation) is the foundation this builds on.

## 1. Token contract

The platform access token is a single-issuer, ES256-signed JWT. DPoP adds the `cnf` claim:

| Claim | Type | Notes |
|-------|------|-------|
| `iss` | string | Single trust anchor. Must equal the validator's configured issuer. |
| `aud` | array | Audience-scoped per call, e.g. `api://app`. Must contain the service id. |
| `sub` | string | Stable principal id (qrdb user / client). |
| `tenant_id` | string | Downstream row-level-security tenant. |
| `scp` | array | Authoritative scope set for RBAC. Emitted alongside `scope` for OIDC compat. |
| `roles` | array | Optional supplemental RBAC input. |
| `iat`/`exp` | number | TTL 5–15 min. |
| `jti` | string | Unique token id. |
| `cnf` | object | `{"jkt":"<base64url SHA-256(JWK)>"}` — DPoP key binding. Omitted for bearer tokens. |

`cnf.jkt` is the RFC 7638 thumbprint of the DPoP proof key. The forward path (RFC 8705)
reuses the same `cnf` object with `x5t#S256`; the two are mutually exclusive.

## 2. Issuance: binding `cnf.jkt`

A DPoP proof is bound at two points. In both, **no proof ⇒ a plain bearer (no `cnf`)**, and a
**presented-but-invalid proof fails closed** (no silent downgrade).

- **OAuth token endpoint** (`pkg/api/rest/iam_oidc.go` → `pkg/iam/oidc.go`): the REST
  `/oauth2/token` handler reads the `DPoP` request header and reconstructs the htm/htu
  (`requestAbsoluteURI`, honoring `X-Forwarded-Proto`/`-Host` for proxied deployments) into
  `TokenRequest.{DPoPProof,HTTPMethod,HTTPURI}`. `OIDCProvider.bindDPoP` verifies the proof and
  returns `*ConfirmationClaim{Jkt}`, threaded into the `authorization_code`,
  `client_credentials`, and `refresh_token` grants. A bound response is typed
  `token_type: DPoP` (RFC 9449 §5); an unbound one stays `Bearer`.
- **Login / session path** (`pkg/api/rest/iam_login_ceremony.go` → `pkg/server/login_session_issuer.go`
  → `pkg/iam/oidc_session.go`): the REST `/api/v1/auth/login/finish` handler reads the `DPoP`
  header into `iam.LoginDPoP` on the request context. qrdb's login ceremony passes that context
  unchanged into `SessionIssuer.Issue`, which reads it back (`iam.LoginDPoPFromContext`) onto
  `SessionIdentity.{DPoPProof,DPoPMethod,DPoPURI}`; `IssueSessionToken` binds `cnf.jkt` via
  `bindSessionDPoP`. This sender-constrains human session bearers with no change to the qrdb
  ceremony signature. DPoP is HTTP-specific, so this is the REST path; other transports issue
  plain bearers.

`Claims.Confirmation` (`pkg/iam/oidc_types.go`) emits the `cnf` claim;
`GenerateAccessToken` also emits `scp` (from `SCP`, else derived from the space-separated
`Scope`). `tokenTypeForConfirmation` selects `DPoP` vs `Bearer` for the response `token_type`.

The provider holds a `dpop.ReplayCache` so a captured proof cannot be replayed to mint a
second bound token at issuance.

## 3. DPoP verifier — `pkg/iam/dpop`

Untagged, IAM-free package (so downstream services can import it without the `iam` build
tag). The single canonical DPoP implementation in the stack.

- `VerifyProof(proofJWT, VerifyOptions) (*VerifiedProof, error)` — checks: `typ` ==
  `dpop+jwt`, asymmetric `alg` (HS*/none rejected), signature against the embedded `jwk`,
  `htm` == method, `htu` == canonical request URI (scheme+host+path, no query/fragment),
  `iat` within skew (default ±60s, injectable clock), `jti` not replayed, and — when
  `ExpectedJkt` is set — `SHA-256(jwk)` == `cnf.jkt`. Every failure is a typed error.
- `NewProof(key, htm, htu)` / `NewProofWithClock(...)` — client/SDK proof signer (ECDSA,
  RSA, Ed25519).
- `Thumbprint(pub)` — RFC 7638 `jkt`.
- `ReplayCache` interface + `MemoryReplayCache` (TTL, injectable clock; swappable for a
  distributed cache in clustered deployments).

## 4. Reusable SDK validator — `sdk/go/platformtoken`

What every downstream service calls. The JWKS cache supports ECDSA (ES256) as well as RSA.

```go
v, _ := platformtoken.NewValidator(platformtoken.Options{
    Issuer:   "https://identity.<domain>",
    Audience: "api://app",
    JWKSURL:  "https://identity.<domain>/.well-known/jwks.json", // or JWKS for in-memory
})

// Token only.
claims, err := v.ValidatePlatformToken(ctx, bearer)

// Token + DPoP proof, bound via cnf.jkt.
claims, err = v.ValidateWithDPoP(ctx, bearer, dpopProof, r.Method, requestURI, replayCache)
```

- `ValidatePlatformToken` — verifies signature against the issuer JWKS by `kid`, `iss`,
  `aud`, `exp`. Asymmetric algorithms only.
- `ValidateWithDPoP` — runs `ValidatePlatformToken`, then verifies the proof against
  `cnf.jkt`. **A bearer token (no `cnf.jkt`) is rejected on this path** — a stolen token
  replayed with an attacker's DPoP key fails with `ThumbprintMismatchError`.

## 5. scope → RBAC and tenant_id → RLS

`platformtoken.Authorize(claims) *AuthorizedContext` projects validated claims into the
identity a service authorizes on:

- `AuthorizedContext.RequireScope("app.read")` / `RequireAnyScope(...)` — returns
  `ScopeDeniedError` (map to 403) when the scope is absent. This is the scope→RBAC gate.
- `AuthorizedContext.TenantID` — the value a service sets as its RLS tenant
  (e.g. `app.current_tenant`).
- `Subject`, `Roles`, `Confirmation` — for audit and supplemental RBAC.

**Scope catalog convention:** scopes are `<svc>.<verb>`, e.g. `app.read`, `app.admin`. A
token with no `<svc>.*` scope is denied.

## 6. Validation order in a downstream service

1. Terminate/validate mTLS (channel; Layer 1).
2. `ValidatePlatformToken` — signature/iss/aud/exp.
3. `ValidateWithDPoP` — proof + `cnf.jkt` binding.
4. `Authorize` + `RequireScope` for RBAC; `TenantID` for RLS.
5. Serve; audit with service + user identity.

## 7. Tests

- `pkg/iam/dpop` — valid proof; wrong `htm`/`htu`; expired/future `iat`; replayed `jti`;
  `jkt` mismatch; tampered signature; HS256/none rejection; missing `typ`/`jwk`/`iat`/`jti`;
  all ECDSA curves + RSA + Ed25519; replay-cache TTL.
- `sdk/go/platformtoken` — valid token; wrong `iss`/`aud`; expired; unknown `kid`; forged
  signature; remote JWKS fetch + TTL refresh + fetch failure; `ValidateWithDPoP` binds proof
  key; **stolen token + different DPoP key rejected**; bearer rejected on DPoP path; scope
  enforcement.
- `pkg/iam` — token endpoint and session path bind `cnf.jkt` from a presented proof; bearer
  carries no `cnf`; invalid proof fails closed; replay rejected at issuance.
- `pkg/api/rest` — `requestAbsoluteURI` reconstructs the htu (TLS scheme, `X-Forwarded-*`,
  query/fragment dropped).
- `test/integration/iam` (E2E) — `TestDPoPSenderConstrainedToken`: a `DPoP` header on the real
  `/oauth2/token` endpoint yields a `token_type: DPoP` token whose `cnf.jkt` the SDK validator
  accepts with a matching proof; a stolen token + different key is rejected; a no-header request
  yields a `Bearer` token refused on the DPoP path.


---

## Token Introspection (RFC 7662) + Dual-Identity Audit

Status: **Implemented (Phase 6)** · Targets: **FedRAMP Moderate/High, DoD IL4/IL5**

This document describes the broker's RFC 7662 token introspection endpoint, the
dual-identity audit trail, the platform access-token TTL, and the GovCloud
private-ingress posture (SC-7) for the introspection surface.

### 1. Introspection endpoint (RFC 7662)

The broker exposes `POST /oauth2/introspect`. A resource server submits a
form-encoded body with `token` (required) and an optional `token_type_hint`.
The endpoint is advertised in both OIDC discovery
(`/.well-known/openid-configuration`) and the OAuth 2.0 Authorization Server
Metadata (`/.well-known/oauth-authorization-server`) as `introspection_endpoint`.

#### Response

`active=false` (and no other fields) is returned for any token that is:

- not signature-verifiable against the broker JWKS (kid-aware),
- expired (`exp` in the past) or not-yet-valid (`nbf` in the future), or
- **revoked** — looked up in the per-tenant revocation store by `jti`.

For an active token the standard claim set is returned: `active`, `sub`,
`tenant_id`, `scope` + `scp` (array form), `aud`, `iss`, `exp`, `iat`, `nbf`,
`jti`, `client_id`, `token_type` (`Bearer`), `cnf` (DPoP `jkt` / mTLS
`x5t#S256` when sender-constrained), `roles`, and `username` when known. The
endpoint fails closed: it never leaks claims for an invalid token.

An active token that carries attestation also reports it, so an introspecting
resource server can enforce the same hardware-only policy as one that parses
the JWT:

- `attest_level` — the key provenance proven at login: `hardware`, `software`
  or `none`.
- `attest_aaguid` — the hex FIDO2 authenticator model; absent for PIV.

Both are omitted when the token carries no attestation. Absence means "no
evidence", never hardware. `acr` and `amr` are not reported yet (#392), and
the userinfo endpoint never reports attestation: userinfo is profile data.

#### Tenant derivation — no global table

The broker has **no global token table**. The tenant is derived from the
token's own `tenant_id` claim after signature verification, and that tenant is
set on the context **before** the per-tenant revocation lookup. A caller cannot
influence which tenant store is consulted; the verified claim is authoritative.

#### Caller authentication (RFC 7662 §2.1)

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

#### Every transport

Introspection is also `Security().Auth().IntrospectToken` in the SDK, served on
every transport through the service layer (`AuthService.IntrospectToken`):

| Transport | Served by | Caller requirement |
|-----------|-----------|--------------------|
| REST, QUIC (HTTP/3) | `POST /oauth2/introspect` | mTLS or client credentials, as above |
| gRPC, Unix socket | `QuicBaaS/IntrospectToken` | authenticated, `user_authfactors:login` |
| MCP | tool `quicbaas_introspecttoken` | authenticated, `user_authfactors:login` |
| Embedded | in-process handler | the host process |

Every transport returns the same `service.TokenIntrospection`, including
`AttestLevel` and `AttestAAGUID`. An inactive token returns exactly
`Active=false`, and a missing token is an invalid-input error. No transport
lets an unauthenticated caller introspect.

#### Reused crypto

Introspection reuses the OIDC provider's signature-verification + claim-parsing
path (`parseVerifiedAccessTokenClaims`) and the existing revocation store
(`IsTokenRevoked`). No crypto or revocation logic is duplicated.

### 2. Dual-identity audit (AU-2 / AU-3)

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

#### Audit store layout — FLAG

The audit store (`iam.MemoryAuditStorage`, the default) is a **single process
store partitioned by `tenant_id`** (a `tenantIndex` map). It is **not** a
physically per-tenant store. Each event is stamped with `tenant_id` and queries
are tenant-scoped, so cross-tenant reads are not possible through the API, but
the events for all tenants live in one in-memory structure. For a deployment
that requires physically isolated per-tenant audit partitions (or durable,
tamper-evident storage for FedRAMP AU-9), a tenant-routed `AuditStorage`
backend must be supplied via `iam.WithAuditStorage`. This is called out so the
control-plane operator can make the storage-isolation decision explicitly.

### 3. Platform access-token TTL

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

### 4. GovCloud private ingress (SC-7)

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
NACL); the application does not relocate the boundary into code. An optional config
knob supports the posture without changing it when unset:

- Every API listener (REST, gRPC, MCP) binds on the shared `server.host`; the
  embedded node opens them all there. There is no per-protocol bind: a
  `server.<protocol>.bind_address` key is refused at load (`RemovedKeys`), since
  it never reached a listener. Keep a privileged protocol private with the
  network boundary above, or by running it on a separate node.
- **`security.require_mtls`** flips the server TLS client-auth posture from the
  default opportunistic `VerifyClientCertIfGiven` to strict
  `RequireAndVerifyClientCert`, rejecting any connection without a trusted client
  certificate. Default `false` preserves current behavior. Applied via
  `Server.optionalMTLSClientAuth` (`pkg/server/lifecycle.go`).

These are knobs, not a substitute for the network boundary: the operator still
places the private listeners on an internal interface and SG-restricts them.
