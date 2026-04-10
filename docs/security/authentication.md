# Authentication

JWT-based authentication with OAuth 2.1 and role-based access control.

> **Breaking Change (v1.1.0):** MCP token parameters (`_token`, `accessToken`) in request body are no longer supported. Use `Authorization: Bearer <token>` header. See [MCP Authorization](../api/mcp-authorization.md#migration-guide).

## Overview

QuicBaaS supports multiple authentication methods:

| Method | Use Case | Protocol |
|--------|----------|----------|
| OAuth 2.1 + PKCE | MCP clients, web apps | OIDC/OAuth |
| Client Credentials | M2M, service accounts | OAuth |
| WebAuthn/FIDO2 | Passwordless users | WebAuthn |
| Federation | External IdPs | OIDC |
| JWT | Direct API access | Bearer tokens |

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

### Dynamic Client Registration (RFC 7591)

```bash
POST /oauth2/register
Content-Type: application/json

{
  "redirect_uris": ["https://client.example.com/callback"],
  "client_name": "My App",
  "grant_types": ["authorization_code"],
  "token_endpoint_auth_method": "client_secret_basic"
}
```

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

QuicBaaS supports two JWT signing modes:

| Mode | Algorithm | Key Material | Use Case |
|------|-----------|-------------|----------|
| Asymmetric (recommended) | ES256/ES384/ES512/RS256/RS384/RS512 | KMS-backed key pair | Production |
| Symmetric (legacy) | HS256 | Shared secret | Development/testing |

### Asymmetric Signing via KMS (Recommended)

Tokens are signed using an ECDSA or RSA private key managed by the KMS. The `kid` (Key ID) header is included in each token for key identification. Verification uses only the public key, enabling verification-only nodes that cannot sign.

```yaml
auth:
  enabled: true
  jwt_signing_key_id: "my-jwt-signing-key"
  jwt_signing_algorithm: "ES256"  # Defaults to ES256 when omitted
```

When `jwt_signing_key_id` is set, the auth manager:
1. Retrieves the `crypto.Signer` from KMS via `xkms.SignerByID()`
2. Extracts the public key for token verification
3. Signs tokens with the configured algorithm (default: ES256)
4. Includes `kid` header in each token for key rotation support

If the KMS signer fails to initialize and a `jwt_secret` is also configured, the manager falls back to HS256 with a warning. If no fallback is available, initialization fails.

### Symmetric Signing (Legacy/Development)

For backward compatibility and development, HS256 signing with a shared secret is supported:

```yaml
auth:
  enabled: true
  jwt_secret: "your-secret-key-32-chars-minimum"
```

### Migration from HS256 to ES256

During migration, both signing methods can be active simultaneously. The token validation layer inspects the token's signing method and routes to the appropriate verification key:

- ECDSA/RSA tokens are verified with the configured public key
- HMAC tokens are verified with the shared secret (if still configured)

To migrate:
1. Configure both `jwt_secret` and `jwt_signing_key_id`
2. New tokens are signed with ES256; existing HS256 tokens remain valid
3. After all HS256 tokens expire, remove `jwt_secret`

## Direct JWT Authentication

For direct API access without OAuth flow:

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

// Or symmetric signing (for development/testing)
authConfig := &auth.AuthConfig{
    Enabled:         true,
    JWTSecret:       "your-secret-key-32-chars",
    TokenTTL:        1 * time.Hour,
    RefreshTokenTTL: 7 * 24 * time.Hour,
}

authManager, err := auth.NewUnifiedAuthManager(authConfig, storage, logger)

// Bootstrap admin user
result, err := auth.MustBootstrap(authManager, &auth.BootstrapConfig{
    CreateAdminUser: true,
    AdminUsername:   "admin",
})

// Authenticate
ctx := context.Background()
token, err := authManager.Authenticate(ctx, "admin", result.AdminPassword)
```

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

## Usage

### Create Users

```go
user, err := authManager.CreateUser("alice", "password", []string{"readwrite"})
```

### Authenticate

```go
token, err := authManager.Authenticate(ctx, "alice", "password")
// token.AccessToken (1 hour TTL)
// token.RefreshToken (7 days TTL)
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
roleManager := authManager.GetRoleManager()

customRole := &auth.Role{
    Name:        "data_analyst",
    Description: "Read-only data access",
    Permissions: []*auth.Permission{
        {Resource: "keys", Actions: []string{"read"}},
        {Resource: "metrics", Actions: []string{"read"}},
    },
}

err := roleManager.AddRole(customRole)
```

## Permission Model

### Resources
- keys, indexes, cluster, users, backups, metrics, health, transactions, watch, * (wildcard)

### Actions
- read, write, delete, admin, * (wildcard)

### Matching
Wildcards in roles match all resources/actions. Actions are checked by exact match or wildcard.

## Security Features

1. **Password Hashing**: bcrypt with configurable cost (legacy SHA-256 transparently upgraded on login)
2. **JWT Signing**: Asymmetric ES256 via KMS (recommended) or HS256 shared secret (legacy)
3. **Token Uniqueness**: Cryptographic nonce in every token prevents replay
4. **Key Identification**: `kid` header in asymmetric tokens supports key rotation
5. **Dual Validation**: Both HS256 and ES256 tokens accepted during migration
6. **Internal Key Protection**: Prevents user data from overwriting auth data
7. **Role Validation**: Built-in roles cannot be modified
8. **Login Throttling**: Configurable lockout after failed attempts

## Best Practices

1. Use asymmetric signing (`jwt_signing_key_id`) with KMS for production
2. Use ES256 (ECDSA P-256) as the default signing algorithm
3. Store signing keys in the KMS, not on disk or in environment variables
4. For development/testing, use a strong JWT secret (32+ characters)
5. Rotate tokens before expiry
6. Revoke tokens on logout
7. Use principle of least privilege
8. Monitor failed authentication attempts
9. Migrate from HS256 to ES256 by configuring both, then removing the secret

## See Also

- [MCP Authorization](../api/mcp-authorization.md) - OAuth 2.1 for MCP
- [Authorization](authorization.md) - RBAC/ABAC
- [IAM Configuration](../iam/configuration.md) - Identity management
- [Security Overview](README.md)
- [KMS](kms.md)
- [Secrets](secrets.md)
