# Session Renewal

How a QuicBaaS session survives past its 15-minute access token.

## Why renewal exists

Access tokens are deliberately short-lived:

| Setting | Value | Source |
| --- | --- | --- |
| `DefaultOIDCAccessTokenTTL` | 15m | `pkg/config/config.go` |
| `DefaultOIDCAccessTokenTTLMin` | 1m | `pkg/config/config.go` |
| `DefaultOIDCAccessTokenTTLMax` | 60m | `pkg/config/config.go` |
| `DefaultOIDCRefreshTokenTTL` | 7d | `pkg/config/config.go` |

`validateAuth` in `pkg/config/validator.go` rejects an `access_token_ttl`
outside `[1m, 60m]`, so a long-lived bearer is impossible by configuration.
That bounds the replay window of a stolen token — and it is only a usable
posture if the session can be renewed without re-running the factor ceremony.

## The credential

The renewal credential is **opaque**, not a JWT:

- 256 bits of CSPRNG entropy, base64url encoded.
- Stored **hashed by value** (`Token.ValueHash` = SHA-256 hex). The presented
  secret is the lookup handle; the secret itself is never a storage key.
- **Rotated on every redemption.** The presented credential is revoked and a
  replacement is returned. Replaying a rotated credential is refused — the
  OAuth 2.1 §4.13.2 detection for a stolen refresh token.
- Rotation **inherits the original absolute expiry**. A session cannot be
  extended indefinitely by refreshing; it lives at most `RefreshTokenTTL` from
  the login that created it.
- Revocable server-side, so ending a session is immediate.

## What a renewal reproduces

A refresh **renews** a session. It does not re-authenticate and it does not
re-authorize. The authentication context established at login is frozen into
the stored record (`Token.Session`, a `TokenSessionContext`) and replayed onto
every renewed access token: roles, groups, audience, profile claims,
`auth_method`, `mfa_verified`, `amr`, `acr`, and the DPoP `cnf.jkt` binding.

Authority changes take effect through the two mechanisms that already bound the
session:

1. the 15-minute access-token lifetime, after which the request-time RBAC gate
   re-evaluates the subject, and
2. refresh-token revocation, which ends the session immediately.

A DPoP-bound session (RFC 9449) may only be renewed by presenting a proof over
the bound key, and the renewed token stays bound to it. A plain-bearer session
is never silently upgraded from a presented proof.

## Where each piece lives

| Concern | Location |
| --- | --- |
| Issue / redeem / rotate | `pkg/iam/oidc_session_refresh.go` |
| Typed errors | `pkg/iam/oidc_session_refresh_errors.go` |
| Value index (hashed) | `pkg/iam/storage_backend.go`, `pkg/iam/storage_memory.go` |
| Service verbs (`RefreshToken` / `Logout` / `ValidateToken`) | `pkg/service/quicbaas.go` (`authService`) |
| Ceremony attaches the credential | `pkg/service/login_service.go` |
| Boot-time wiring | `pkg/server/login_session_issuer.go` |
| SPA renewal + 401 recovery | `cmd/quicbaas/frontend/src/stores/auth.svelte.ts`, `authFetch.ts` |
| CLI renewal + write-back | `pkg/cli/token_refresh.go`, `cmd/quicbaas/client.go` |

### Why the ceremony layers it on in the service facade

qrdb owns `BeginLogin` / `FinishLogin`, and its `FinishLoginResponse` carries
only `(bearer, identity, expiry, factor)`. Its `SessionIssuer` contract returns
`(token, expiry, error)` — there is no room for a second credential. Refresh is
an OAuth2 concept that belongs to QuicBaaS, not to qrdb's factor ceremony, so
`LoginService.FinishLogin` mints it **after** the ceremony returns, in the one
facade every transport already funnels through.

`OIDCProvider.IssueRefreshForSessionBearer` re-reads the claims of the bearer
it just minted, so all token knowledge stays inside the OIDC layer and the
renewal reproduces exactly the session that was issued.

If refresh issuance fails, **the login fails**. Handing back a bearer with no
way to renew it is the exact failure this design removes: the client looks
signed in and then dies at the 15-minute boundary.

## Endpoints

### `POST /api/v1/auth/login/{begin,finish}` — unified ceremony

`finish` returns:

```json
{
  "bearer": "<jwt>",
  "identity": { "subject": "...", "tenant_id": "...", "roles": ["..."] },
  "expires_at": "2026-08-08T12:00:00Z",
  "factor_id": "...",
  "refresh_token": "<opaque>",
  "refresh_expires_at": "2026-08-15T11:45:00Z"
}
```

`refresh_token` / `refresh_expires_at` are omitted when no refresh issuer is
bound (early boot). The same two fields are carried by the gRPC
`FinishLoginResponse` (fields 5 and 6), and by the QUIC and MCP responses
(flattened as `refreshToken` / `refreshExpiresAt`).

### `POST /api/v1/auth/refresh` — renewal (public)

The credential is itself the authentication, so the route is public. An
optional `DPoP` header carries the RFC 9449 proof.

```json
{ "refresh_token": "<opaque>" }
```

```json
{
  "access_token": "<jwt>",
  "refresh_token": "<rotated opaque>",
  "token_type": "Bearer",
  "expires_in": 900,
  "expires_at": "2026-08-08T12:15:00Z"
}
```

| Status | Meaning |
| --- | --- |
| 200 | Renewed. **Persist `refresh_token`** — the presented one is revoked. |
| 400 | `refresh_token` missing. |
| 401 | Unknown, expired, revoked, or already-rotated credential; or a DPoP-bound session refreshed without a matching proof. |
| 503 | The token authority is not configured. |

### `POST /oauth2/token` with `grant_type=refresh_token`

The OAuth2 grant is for registered clients (it authenticates `client_id` /
`client_secret`). It rotates too. SPA and CLI sessions are **not** OAuth
clients and use `/api/v1/auth/refresh`.

## Client behaviour

### SPA

- `setAuthenticated()` persists both tokens (`quicbaas_token`,
  `quicbaas_refresh_token`).
- `getValidAccessToken()` is the SDK token factory. It decodes `exp` and
  renews **pre-emptively** inside a 60-second skew window.
- `refreshAccessToken()` is single-flight; concurrent callers share one request.
  It persists the rotated credential.
- `installAuthFetch()` (`main.ts`) wraps `window.fetch` once: a 401 from a
  same-origin `/api/` request triggers one renewal and replays the request with
  the new bearer. It replays at most once and never intercepts the refresh
  endpoint.
- A definitive rejection clears the session; a *network* failure does not
  (transient blips must not log the operator out).
- `validateSession()` is guarded by a session generation counter, so a check
  that was in flight during a logout cannot resurrect the session.

### CLI / MCP

- `quicbaas auth login --save-token` writes `access_token`, `refresh_token`,
  `expires_at`, and `token_type` into the `cli.TokenData` envelope at
  `~/.quicbaas/token`.
- `loadStoredAuthToken()` resolves through `cli.TokenManager`: a token inside
  the skew window is renewed first and the rotated pair is written back.
- Renewal is single-flight (mutex plus a re-read under the lock), so parallel
  invocations do not each redeem a single-use credential.
- The renewal client is deliberately token-less: the endpoint is public, and an
  authenticated client would recurse back into `loadStoredAuthToken()`.

This is what lets `quicbaas mcp serve --stdio`, which an MCP host keeps alive
for hours, survive well past the first 15-minute boundary.

## Testing

| Layer | Tests |
| --- | --- |
| Issue / rotate / replay / DPoP / expiry | `pkg/iam/oidc_session_refresh_test.go` |
| Service verbs against a real provider | `pkg/service/auth_session_test.go` |
| Ceremony attaches the credential | `pkg/service/login_service_refresh_test.go` |
| REST wiring + DPoP passthrough | `pkg/api/rest/iam_session_refresh_test.go` |
| CLI renewal, rotation write-back, single-flight | `pkg/cli/token_refresh_test.go` |
| SPA pre-emptive + 401 recovery | `cmd/quicbaas/frontend/test/authRefresh.test.ts` |
| End to end against a live server | `test/integration/iam/session_refresh_test.go` |
