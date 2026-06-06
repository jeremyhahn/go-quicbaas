# Broker Phase 2 — DPoP sender-constrained platform token

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
