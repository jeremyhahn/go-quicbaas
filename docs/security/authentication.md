# Authentication

QuicBaaS uses **passwordless, hardware-backed authentication exclusively**. Users authenticate via WebAuthn/FIDO2 or PIV smartcard. Service accounts use mTLS with bound OAuth2 tokens. See [Passwordless Authentication](../auth/passwordless.md) for complete details.

> **Breaking Change (v1.1.0):** MCP token parameters (`_token`, `accessToken`) in request body are no longer supported. Use `Authorization: Bearer <token>` header. See [MCP Authorization](../api/mcp-authorization.md#migration-guide).
>
> **Breaking Change (v2.0.0):** Password-based authentication is removed. User passwords are no longer supported. All users must enroll a hardware authenticator (WebAuthn passkey, security key, or PIV smartcard).

## Overview

QuicBaaS supports multiple authentication methods:

| Method | Identity Type | Protocol | Status |
|--------|---|----------|--------|
| WebAuthn / FIDO2 | Human user | CTAP2 / WebAuthn | Production |
| PIV Smartcard | Human user | PIV / PKCS#11 | Production |
| mTLS + OAuth2 | Service account | TLS 1.3 + RFC 8705 | Production |
| OAuth 2.1 + PKCE | Machine client | OIDC / OAuth | Production |
| JWT (bearer token) | API-direct | Bearer token | Production |
| Federation (OIDC) | External IdP | OIDC | Production |

Authentication adapts to your storage backend:
- Memory storage: Ephemeral auth (users lost on restart)
- Persistent storage: Database-backed auth (users survive restarts)

## OAuth 2.1 (MCP Authorization)

MCP clients authenticate using OAuth 2.1 with mandatory PKCE. See [MCP Authorization](../api/mcp-authorization.md) for complete flow.

### PKCE Requirements

PKCE is **mandatory** for all authorization code flows. Only S256 challenge method is supported.

```python
import secrets, hashlib, base64

# Generate code verifier
code_verifier = secrets.token_urlsafe(32)

# Generate code challenge (S256)
digest = hashlib.sha256(code_verifier.encode()).digest()
code_challenge = base64.urlsafe_b64encode(digest).rstrip(b'=').decode()
```

### Resource Indicators (RFC 8707)

Access tokens are bound to specific resource servers:

```bash
# Include resource in authorization request
GET /oauth2/authorize?...&resource=https://quicbaas.example.com/mcp

# Include in token request
POST /oauth2/token
...&resource=https://quicbaas.example.com/mcp
```

The token's `aud` claim must match the MCP server's resource URI.

### Discovery Endpoints

| Endpoint | RFC | Description |
|----------|-----|-------------|
| `/.well-known/oauth-authorization-server` | RFC 8414 | OAuth AS metadata |
| `/.well-known/oauth-protected-resource` | RFC 9728 | MCP resource metadata |
| `/.well-known/openid-configuration` | OIDC | OIDC discovery |
| `/.well-known/jwks.json` | - | Public keys |

### Dynamic Client Registration (RFC 7591 / RFC 7592)

Registering a client mints an authentication principal, so the surface is
**disabled by default** and requires an initial access token when enabled. See
`iam.dynamic_client_registration` in [IAM configuration](../iam/configuration.md).

```bash
POST /oauth2/register
Authorization: Bearer <initial access token>   # a platform token
Content-Type: application/json

{
  "redirect_uris": ["https://client.example.com/callback"],
  "client_name": "My App",
  "grant_types": ["authorization_code"],
  "token_endpoint_auth_method": "client_secret_basic"
}
```

The new client is created in **the tenant the initial access token
authenticated into**. A request header can never select the tenant, and an
unauthenticated registration never lands in the `system` (control-plane)
tenant — it is refused unless the operator explicitly configured a non-system
`anonymous_tenant_id`.

The `201 Created` response carries two credentials that are shown **once** and
are not recoverable:

| Field | Purpose |
|-------|---------|
| `client_secret` | Client authentication at the token endpoint |
| `registration_access_token` | The sole credential for the client configuration endpoint (RFC 7592) |
| `registration_client_uri` | The client configuration endpoint for this client |

#### Client configuration endpoint (RFC 7592)

```bash
GET|PUT|DELETE /oauth2/register/{client_id}
Authorization: Bearer <registration_access_token>
```

These three verbs authenticate **solely** with the registration access token —
the registrant holds no platform token — so they are a deliberate pre-auth
carve-out in the REST authorization chain. The token is a real, fail-closed
gate:

- 256 bits of entropy, issued once at registration;
- stored only as a SHA-256 hash on the client record, never in plaintext;
- verified by constant-time comparison against the hash **for that exact
  `client_id`**, so a token for client A can never address client B, and — since
  a client belongs to exactly one tenant — never crosses a tenant boundary;
- when the request also carries a verified principal, the client must
  additionally live in that principal's tenant (the tenant narrows, never
  widens, the lookup);
- a client created outside DCR owns no registration access token and cannot be
  managed through RFC 7592 at all.

Every failure — absent, malformed, unknown or mismatched token, unknown client,
cross-tenant attempt — answers the same opaque `401 invalid_token`, so the
endpoint cannot be used to enumerate client ids.

The sibling create route (`POST /oauth2/register`, no `client_id` segment) is
**not** part of that carve-out: it stays behind the standard authorization chain
and is PEP-mapped to `tenants:create`.

## External IdP Federation

QuicBaaS can delegate authentication to external identity providers:

```yaml
iam:
  federation:
    providers:
      - id: okta-prod
        name: Okta Production
        type: oidc
        issuer: https://company.okta.com
        client_id: ${OKTA_CLIENT_ID}
        client_secret: ${OKTA_CLIENT_SECRET}
        scopes: ["openid", "profile", "email"]
```

When configured, MCP clients authenticate with the external IdP, and QuicBaaS validates tokens via JWKS.

## JWT Signing

JWT signing is asymmetric only. There is no JWT key material in the configuration file — the former `auth.jwt_secret`, `auth.jwt_signing_key_id`, and `auth.jwt_signing_algorithm` fields were removed, and shared-secret (HS256) tokens are rejected outright to eliminate algorithm-confusion attacks.

### xkms-Backed Signing Key (Automatic)

The IAM/OIDC subsystem signs tokens with an ES256 key held in the configured xkms backend under the common name `oidc-signing`. On first boot the key is generated automatically inside the backend selected by the BackendResolver (TPM 2.0, PKCS#11, or software keystore) — see `pkg/iam/oidc_keymanager.go` (`NewSigningKeyManagerFromXKMS`). The private key never leaves the keystore; signing is delegated to the keystore's `crypto.Signer`, and each token carries a `kid` header matching the published JWKS.

Rotation generates a fresh ES256 key in the xkms backend and demotes the previous key into a JWKS overlap window, so in-flight tokens remain verifiable until they expire.

### Per-Tenant Signing Keys

Dedicated tenants can opt in to their own signing key via tenant OIDC settings. `TenantSigningKeyResolver` (`pkg/iam/oidc_tenant_signing.go`) maps each tenant to its key manager, and the aggregated JWKS publishes the global key plus every tenant key. Per-tenant IAM data (users, policies, sessions, WebAuthn credentials, OIDC settings) is barrier-isolated via the tenant-routed KV store.

### Internal Auth Manager (pkg/auth)

The internal `pkg/auth` manager (embedded and test paths) is asymmetric-only as well: ES256 by default, with the keystore-backed signer supplied via `SetAsymmetricSigning` when `JWTSigningKeyID` is set. HMAC tokens fail validation unconditionally.

## Direct JWT Authentication

For API clients that need to validate or use JWT tokens directly:

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/auth"

// Create storage backend
storage := memory.New(logger)

// Asymmetric signing via KMS (recommended for production)
authConfig := &auth.AuthConfig{
    Enabled:             true,
    JWTSigningKeyID:     "my-jwt-key",
    JWTSigningAlgorithm: "ES256",
    TokenTTL:            1 * time.Hour,
    RefreshTokenTTL:     7 * 24 * time.Hour,
}

authManager, err := auth.NewUnifiedAuthManager(authConfig, storage, logger)

// Validate a token and extract claims
token := "eyJhbGciOiJSUzI1NiIs..." // Token from WebAuthn, OAuth2, etc.
claims, err := authManager.ValidateToken(ctx, token)
if err != nil {
    log.Fatal(err)
}

// Inspect claims
log.Printf("User: %s", claims.Subject)
log.Printf("Auth Method: %v", claims.AMR) // ["webauthn","hwk"] or ["mtls","hwk"]
log.Printf("Attestation: %s", claims.AttestationMethod)
```

**Note:** User creation with passwords is deprecated. Users are created in `pending` status and must activate by enrolling a hardware authenticator. See [User Flow](../auth/passwordless.md#user-flow-invite--activate--login).

## Storage Modes

| Storage Backend | Auth Mode | Persistence | Cluster-Safe |
|----------------|-----------|-------------|--------------|
| memory.MemoryStorage | In-memory | No | No |
| pebble.Storage | Persistent | Yes | Yes |
| Embedded | Persistent | Yes | Yes |
| Cluster | Persistent | Yes | Yes |

## Internal Key Schema

All authentication data uses reserved `__quicbaas:` prefix.

### User Keys
```
__quicbaas:users:{userID}                 -> User JSON
__quicbaas:users:byname:{username}        -> userID lookup
```

### Role Keys
```
__quicbaas:roles:{roleName}               -> Role JSON
```

### Token Keys
```
__quicbaas:tokens:refresh:{userID}:{hash} -> Expiry timestamp
__quicbaas:tokens:revoked:{hash}          -> Revocation timestamp
```

## Built-in Roles

| Role | Resources | Actions | Description |
|------|-----------|---------|-------------|
| admin | * | * | Full administrative access |
| readwrite | keys, indexes, transactions | read, write, delete | Application access |
| readonly | keys, indexes, metrics, health | read | Read-only monitoring |

## Usage (Passwordless)

### Create Users (Pending Activation)

```go
user, err := authManager.CreateUser("alice@example.com", "", []string{"readwrite"})
// User is created in pending status, no password
// User must activate by enrolling a WebAuthn credential
```

### Obtain Token After User Enrolls Authenticator

Tokens are issued after the user completes WebAuthn/smartcard authentication:

```go
// Via OAuth2 client credentials (service account)
token, err := authManager.ClientCredentials(ctx, clientID)
// token.AccessToken (1 hour TTL, bound to mTLS cert via cnf.x5t#S256)
// token.RefreshToken (7 days TTL)

// Or use a token obtained from browser WebAuthn login
```

### Authorize

```go
err := authManager.Authorize(ctx, token.AccessToken, "keys", "write")
```

### Refresh Tokens

```go
newToken, err := authManager.RefreshToken(ctx, token.RefreshToken)
```

### Custom Roles

```go
// Roles are owned by go-qrdb — there is NO local RoleManager / GetRoleManager.
// quicbaas creates and manages roles through QRDBRoleStorage, which delegates to
// the qrdb RoleClient (pkg/iam/qrdb_role_storage.go). Permissions use the colon
// resource:action vocabulary.
err := roleStorage.CreateRole(ctx, &iam.Role{
    Name:        "data_analyst",
    Description: "Read-only data access",
    Permissions: []string{"keys:read", "metrics:read"},
})
```

## Permission Model

### Resources
- keys, indexes, cluster, users, backups, metrics, health, transactions, watch, * (wildcard)

### Actions
- read, write, delete, admin, * (wildcard)

### Matching
Wildcards in roles match all resources/actions. Actions are checked by exact match or wildcard.

## Security Features

1. **Hardware Attestation**: FIDO2/WebAuthn and TPM verify authenticator identity
2. **Phishing Resistant**: Credentials bound to origin; no replay attacks possible
3. **Zero Shared Secrets**: No password databases; each authenticator holds its private key
4. **JWT Signing**: Asymmetric ES256 via an xkms-backed keystore key; no shared secrets
5. **Token Binding**: OAuth2 access tokens bound to mTLS client certificate via `cnf.x5t#S256` (RFC 8705)
6. **Token Uniqueness**: Cryptographic nonce in every token prevents replay
7. **Key Identification**: `kid` header in asymmetric tokens supports key rotation
8. **HMAC Rejection**: HS256 tokens are rejected outright (algorithm-confusion hardening)
9. **Internal Key Protection**: Prevents user data from overwriting auth data
10. **Role Validation**: Built-in roles cannot be modified
11. **Authenticator Tracking**: Sign count and last-used timestamp prevent cloning

## Best Practices

1. **Passwordless-First**: Always use WebAuthn or smartcard for users; no passwords
2. **Use mTLS + Bound Tokens**: Service accounts use TLS client auth + RFC 8705 `cnf` binding
3. **Hardware-Backed Signing**: Configure a hardware xkms backend (TPM 2.0 or PKCS#11) so the auto-generated signing key is hardware-protected
4. **Store Keys Securely**: Signing keys live in the xkms keystore and never appear on disk, in configuration, or in environment variables
5. **Rotate Credentials**: Refresh access tokens before expiry; rotate mTLS certs regularly
6. **Revoke on Logout**: Revoke refresh tokens and close mTLS sessions on logout
7. **Least Privilege**: Assign minimal required roles; avoid admin role when possible
8. **Audit & Monitor**: Log all authentication events; alert on failed attempts or anomalies
9. **Multiple Authenticators**: Recommend users enroll backup authenticators (e.g., passkey + hardware key)
10. **AAGUID Allowlist**: Verify FIDO2 device identities via AAGUID when attestation is enabled
11. See [Passwordless Authentication](../auth/passwordless.md) for detailed architecture

## See Also

- [MCP Authorization](../api/mcp-authorization.md) - OAuth 2.1 for MCP
- [Authorization](authorization.md) - RBAC/ABAC
- [IAM Configuration](../iam/configuration.md) - Identity management
- [Security Overview](README.md)
- [KMS](kms.md)
- [Secrets](secrets.md)
