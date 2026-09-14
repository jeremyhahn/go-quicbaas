# OAuth 2.1 / OIDC — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/oauth2-oidc/`.


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
| `Logout` | `POST /api/v1/auth/logout` | **no** | yes | yes | yes | — |
| `RefreshToken` | `POST /api/v1/auth/refresh` | **no** | yes | yes | yes | — |
| `RevokeAllSessions` | `POST /api/v1/auth/revokeallsessions` *(default branch)* | **no** | yes | yes | MISSING | — |
| `RevokeSession` | `DELETE /api/v1/auth/sessions/{id}` | **no** | yes | yes | MISSING | — |
| `ValidateToken` | `POST /api/v1/auth/validate` | **no** | yes | yes | yes | — |

> **Composed-node caveat for the three token-lifecycle verbs.** `Served?` above
> is read against the FULL route table, which includes the legacy in-process
> listener's hand-mount. On a **composed** node — the only kind the unified API
> server binds a listener for — the `/api/v1/auth/*` session plane belongs to
> the xkms module, and quicbaas serves its own verbs from the IAM namespace
> instead: `POST /api/v1/iam/{refresh,validate,logout}`
> (`pkg/api/rest/routes_iam.go`). `sdk/go/transport/rest` still sends all three
> to the `/api/v1/auth` paths in the rows above, so against a composed node
> `RefreshToken` and `ValidateToken` fail and `Logout` silently revokes nothing
> — it reaches xkms's handler, which redeems against a different token store.
> That is the live SDK-to-server mismatch tracked in #259; the SPA and the
> TypeScript SDK were moved onto the IAM-namespaced verbs in #221 and #256.


## Overview

MCP authorization inherits from QuicBaaS's IAM configuration. When IAM is enabled, MCP automatically uses the configured authentication providers, RBAC policies, and token validation.

**Integration Points:**
- Built-in OIDC provider for token issuance
- External IdP federation (Okta, Auth0, Azure AD)
- RBAC/ABAC policy enforcement
- Token audience validation (RFC 8707)

## Architecture

```
MCP Client              QuicBaaS
    |                      |
    |  1. Discovery        |
    |--------------------->|  /.well-known/oauth-protected-resource
    |                      |
    |  2. Get AS Metadata  |
    |--------------------->|  /.well-known/oauth-authorization-server
    |                      |
    |  3. Authenticate     |
    |--------------------->|  Built-in OIDC -or- External IdP
    |                      |
    |  4. MCP Request      |
    |--------------------->|  Authorization: Bearer <token>
    |                      |
    |                      |  5. Validate token (JWKS)
    |                      |  6. Check audience (RFC 8707)
    |                      |  7. Enforce RBAC/ABAC
    |                      |
    |  8. Response         |
    |<---------------------|
```

## Using Built-in OIDC Provider

QuicBaaS includes a complete OAuth 2.1/OIDC provider. MCP clients authenticate directly with QuicBaaS.

### Configuration

```yaml
iam:
  enabled: true
  mode: primary

  oidc:
    issuer: "https://quicbaas.example.com"
    signing_algorithm: RS256
    signing_key_file: /etc/quicbaas/keys/oidc-signing.pem

    access_token_ttl: 1h
    refresh_token_ttl: 24h

    pkce:
      required: true
      allowed_methods: ["S256"]  # OAuth 2.1 mandates S256 only

    resource_indicators:
      enabled: true
      validate_audience: true

    dynamic_registration:
      enabled: true
      require_https_redirect: true

  mcp:
    authorization:
      enabled: true
      resource_uri: "https://quicbaas.example.com/mcp"
      resource_documentation: "https://quicbaas.example.com/docs/api/mcp"

      bearer:
        header_only: true  # Required per MCP spec

      errors:
        include_www_authenticate: true
        realm: "quicbaas-mcp"
```

### Client Flow

```bash
# 1. Discover endpoints
curl -s https://quicbaas.example.com/.well-known/oauth-authorization-server

# 2. Register client (if dynamic registration enabled)
curl -X POST https://quicbaas.example.com/oauth2/register \
  -H "Content-Type: application/json" \
  -d '{
    "redirect_uris": ["http://localhost:8080/callback"],
    "client_name": "MCP Client",
    "grant_types": ["authorization_code"]
  }'

# 3. Authorization flow with PKCE (S256 required)
# ... see docs/api/mcp-authorization.md for complete flow

# 4. Use token for MCP requests
curl -X POST https://quicbaas.example.com/mcp \
  -H "Authorization: Bearer <access_token>" \
  -H "MCP-Protocol-Version: 2024-11-05" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

## Federated IdP Support

QuicBaaS can delegate authentication to external identity providers. MCP clients authenticate with the external IdP, and QuicBaaS validates tokens using the IdP's JWKS.

### Okta Integration

```yaml
iam:
  enabled: true
  mode: primary

  auth:
    providers:
      okta:
        enabled: true
        name: "Okta Production"
        type: oidc
        issuer: "https://company.okta.com"
        client_id: "${OKTA_CLIENT_ID}"
        client_secret: "${OKTA_CLIENT_SECRET}"
        scopes: ["openid", "profile", "email", "groups"]

        # Map Okta groups to QuicBaaS roles
        claim_mapping:
          roles: "groups"
          username: "preferred_username"

  mcp:
    authorization:
      enabled: true
      resource_uri: "https://quicbaas.example.com/mcp"
    inherit_federation: true  # MCP uses federated providers
```

**Okta Application Setup:**

1. Create a new OIDC application in Okta Admin Console
2. Set redirect URI: `https://quicbaas.example.com/oauth2/callback/okta`
3. Enable Authorization Code grant with PKCE
4. Add "groups" scope for role mapping
5. Configure audience to include your MCP resource URI

### Auth0 Integration

```yaml
iam:
  auth:
    providers:
      auth0:
        enabled: true
        name: "Auth0 Production"
        type: oidc
        issuer: "https://company.auth0.com"
        client_id: "${AUTH0_CLIENT_ID}"
        client_secret: "${AUTH0_CLIENT_SECRET}"
        scopes: ["openid", "profile", "email"]

        # Auth0 API audience for MCP
        audience: "https://quicbaas.example.com/mcp"

  mcp:
    authorization:
      enabled: true
      resource_uri: "https://quicbaas.example.com/mcp"
```

**Auth0 Setup:**

1. Create a Regular Web Application
2. Enable Authorization Code flow with PKCE
3. Create an API with identifier matching your MCP resource URI
4. Add permissions that map to MCP scopes

### Azure AD Integration

```yaml
iam:
  auth:
    providers:
      azure:
        enabled: true
        name: "Azure AD"
        type: oidc
        tenant_id: "${AZURE_TENANT_ID}"
        issuer: "https://login.microsoftonline.com/${AZURE_TENANT_ID}/v2.0"
        client_id: "${AZURE_CLIENT_ID}"
        client_secret: "${AZURE_CLIENT_SECRET}"
        scopes: ["openid", "profile", "email"]

        # Map Azure AD groups to roles
        claim_mapping:
          roles: "groups"

  mcp:
    authorization:
      enabled: true
      resource_uri: "api://${AZURE_CLIENT_ID}"  # Azure AD format
```

### Generic OIDC Provider

```yaml
iam:
  auth:
    providers:
      custom:
        enabled: true
        name: "Corporate SSO"
        type: oidc
        issuer: "https://sso.corp.example.com"
        client_id: "${SSO_CLIENT_ID}"
        client_secret: "${SSO_CLIENT_SECRET}"
        scopes: ["openid", "profile", "email", "groups"]

        # JWKS endpoint (auto-discovered from issuer if not specified)
        jwks_uri: "https://sso.corp.example.com/.well-known/jwks.json"

        # Token validation
        audience: "quicbaas-mcp"
        algorithms: ["RS256", "ES256"]
```

## Token Validation

MCP validates tokens in this order:

1. **Format Check:** Valid JWT structure
2. **Signature Verification:** Using JWKS from issuer
3. **Expiration Check:** Token not expired
4. **Issuer Validation:** Matches configured issuer(s)
5. **Audience Validation:** Contains MCP resource URI (RFC 8707)
6. **Scope Check:** Has required MCP scopes

### Audience Validation (RFC 8707)

Resource Indicators ensure tokens are bound to specific resource servers:

```yaml
mcp:
  authorization:
    resource_uri: "https://quicbaas.example.com/mcp"
```

Token must contain this URI in the `aud` claim:

```json
{
  "iss": "https://auth.example.com",
  "sub": "user-123",
  "aud": ["https://quicbaas.example.com/mcp"],
  "scope": "mcp:tools mcp:resources",
  "exp": 1704070800
}
```

## Role and Permission Mapping

### From Built-in OIDC

Users created in QuicBaaS have roles assigned directly:

```go
authManager.CreateUser("alice", "password", []string{"readwrite"})
```

### From Federated IdP

External claims are mapped to QuicBaaS roles:

```yaml
iam:
  auth:
    providers:
      okta:
        claim_mapping:
          roles: "groups"  # Okta groups become QuicBaaS roles
```

**Example Okta Token:**
```json
{
  "sub": "00u1234567",
  "groups": ["mcp-users", "data-analysts"],
  "preferred_username": "alice@example.com"
}
```

QuicBaaS maps `groups` to roles and enforces permissions accordingly.

### MCP Tool Permissions

Each MCP tool requires specific resource/action permissions:

| Tool Pattern | Resource | Action |
|-------------|----------|--------|
| `quicbaas_get`, `quicbaas_list` | keys | read |
| `quicbaas_put`, `quicbaas_batch_put` | keys | write |
| `quicbaas_delete` | keys | delete |
| `quicbaas_cluster_*` | cluster | read/admin |
| `quicbaas_backup_*` | backups | read/write/delete |
| `quicbaas_user_*`, `quicbaas_role_*` | users/roles | read/write/delete |
| `quicbaas_kms_*` | kms | read/write/delete |
| `quicbaas_secrets_*` | secrets | read/write/delete |

### Built-in Roles

| Role | Resources | Actions |
|------|-----------|---------|
| admin | * | * |
| readwrite | keys, indexes, transactions | read, write, delete |
| readonly | keys, indexes, metrics, health | read |

## Machine-to-Machine Authentication

For services and automation, use client credentials flow:

```yaml
iam:
  oidc:
    flows:
      client_credentials: true
```

```bash
# Get M2M token
curl -X POST https://quicbaas.example.com/oauth2/token \
  -u "service-account:secret" \
  -d "grant_type=client_credentials" \
  -d "scope=mcp:tools mcp:resources" \
  -d "resource=https://quicbaas.example.com/mcp"
```

## Configuration Reference

### MCP Authorization Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | false | Enable MCP authorization |
| `resource_uri` | string | - | MCP server resource URI (required) |
| `resource_documentation` | string | - | Link to API docs |
| `bearer.header_only` | bool | true | Only accept header tokens |
| `errors.include_www_authenticate` | bool | true | RFC 6750 headers |
| `errors.realm` | string | "quicbaas-mcp" | WWW-Authenticate realm |
| `inherit_federation` | bool | true | Use IAM federation config |

### Environment Variables

```bash
# Built-in OIDC
export OIDC_SIGNING_KEY="-----BEGIN RSA PRIVATE KEY-----..."

# Okta
export OKTA_CLIENT_ID="0oa..."
export OKTA_CLIENT_SECRET="..."

# Auth0
export AUTH0_CLIENT_ID="..."
export AUTH0_CLIENT_SECRET="..."

# Azure AD
export AZURE_TENANT_ID="..."
export AZURE_CLIENT_ID="..."
export AZURE_CLIENT_SECRET="..."
```

## Troubleshooting

### Token Validation Errors

**401 Unauthorized - Missing Token:**
```json
{"error": {"code": -32001, "message": "authentication required"}}
```
Solution: Include `Authorization: Bearer <token>` header.

**401 Unauthorized - Invalid Token:**
```json
{"error": {"code": -32002, "message": "invalid token"}}
```
Causes:
- Token expired
- Invalid signature (JWKS mismatch)
- Token from wrong issuer

**401 Unauthorized - Wrong Audience:**
```json
{"error": {"code": -32002, "message": "token audience does not include MCP resource URI"}}
```
Solution: Include `resource` parameter when requesting token.

**403 Forbidden - Insufficient Scope:**
```json
{"error": {"code": -32003, "message": "permission denied: insufficient scope"}}
```
Solution: Request token with required scopes (`mcp:tools`, `mcp:resources`).

### Discovery Issues

**404 on /.well-known endpoints:**
- Check IAM is enabled: `iam.enabled: true`
- Check MCP auth is enabled: `iam.mcp.authorization.enabled: true`
- Verify issuer URL is configured

### Federation Issues

**JWKS fetch failed:**
- Verify IdP issuer URL is accessible from QuicBaaS
- Check network/firewall rules
- Validate TLS certificates

**Claim mapping not working:**
- Verify claim names in IdP token
- Check `claim_mapping` configuration matches IdP claims
- Enable debug logging to see incoming claims

## See Also

- [MCP Authorization](api.md) - OAuth 2.1 protocol reference
- Authentication - General authentication guide
- IAM Configuration - Complete IAM config reference
- ABAC Policies - Fine-grained access control


---

## MCP Authorization

OAuth 2.1 authorization for the Model Context Protocol (MCP) API.

> **Breaking Change (v1.1.0):** Token parameters in request body (`_token`, `accessToken`) are no longer supported. All authenticated requests must use the `Authorization: Bearer <token>` header. See [Migration Guide](#migration-guide) for details.

### Overview

QuicBaaS's MCP server implements the [MCP Authorization Specification (2025)](https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization), providing secure OAuth 2.1 authentication for AI assistants and MCP clients.

**Key Features:**
- OAuth 2.1 with mandatory PKCE (S256)
- Bearer token authentication
- Resource Indicators (RFC 8707)
- Dynamic Client Registration (RFC 7591)
- Federation with external IdPs (Okta, Auth0, etc.)

### Architecture

```
┌─────────────────┐     ┌──────────────────────┐     ┌─────────────────┐
│   MCP Client    │────▶│   Authorization      │────▶│   QuicBaaS      │
│ (Claude, etc.)  │     │   Server (IAM)       │     │   MCP Server    │
└─────────────────┘     └──────────────────────┘     └─────────────────┘
        │                         │                          │
        │  1. Authorization       │                          │
        │     Request (PKCE)      │                          │
        │────────────────────────▶│                          │
        │                         │                          │
        │  2. Authorization Code  │                          │
        │◀────────────────────────│                          │
        │                         │                          │
        │  3. Token Exchange      │                          │
        │     (with resource)     │                          │
        │────────────────────────▶│                          │
        │                         │                          │
        │  4. Access Token        │                          │
        │     (audience-bound)    │                          │
        │◀────────────────────────│                          │
        │                         │                          │
        │  5. MCP Request         │                          │
        │     (Bearer token)      │                          │
        │─────────────────────────────────────────────────────▶
        │                         │                          │
        │  6. Validate token,     │                          │
        │     check audience      │                          │
        │◀─────────────────────────────────────────────────────
```

### Discovery Endpoints

#### OAuth Authorization Server Metadata (RFC 8414)

```bash
GET /.well-known/oauth-authorization-server
```

Returns authorization server metadata:

```json
{
  "issuer": "https://quicbaas.example.com",
  "authorization_endpoint": "https://quicbaas.example.com/oauth2/authorize",
  "token_endpoint": "https://quicbaas.example.com/oauth2/token",
  "registration_endpoint": "https://quicbaas.example.com/oauth2/register",
  "revocation_endpoint": "https://quicbaas.example.com/oauth2/revoke",
  "jwks_uri": "https://quicbaas.example.com/.well-known/jwks.json",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code", "client_credentials", "refresh_token"],
  "code_challenge_methods_supported": ["S256"],
  "token_endpoint_auth_methods_supported": ["client_secret_basic", "client_secret_post"],
  "scopes_supported": ["openid", "profile", "email", "offline_access", "mcp:tools", "mcp:resources"],
  "resource_indicators_supported": true
}
```

#### Protected Resource Metadata (RFC 9728)

```bash
GET /.well-known/oauth-protected-resource
```

Returns MCP server resource metadata:

```json
{
  "resource": "https://quicbaas.example.com/mcp",
  "authorization_servers": ["https://quicbaas.example.com"],
  "bearer_methods_supported": ["header"],
  "resource_documentation": "https://quicbaas.example.com/docs/api/mcp"
}
```

#### OpenID Connect Discovery

```bash
GET /.well-known/openid-configuration
```

Returns OIDC discovery document (compatible with OAuth AS Metadata).

### Authorization Flows

#### Authorization Code Flow with PKCE

PKCE is **mandatory** for all clients. Only the S256 challenge method is supported.

##### Step 1: Generate PKCE Values

```python
import secrets
import hashlib
import base64

# Generate code verifier (43-128 characters)
code_verifier = secrets.token_urlsafe(32)

# Generate code challenge (S256)
digest = hashlib.sha256(code_verifier.encode()).digest()
code_challenge = base64.urlsafe_b64encode(digest).rstrip(b'=').decode()
```

##### Step 2: Authorization Request

```bash
GET /oauth2/authorize
  ?client_id=mcp-client-123
  &response_type=code
  &redirect_uri=http://localhost:8080/callback
  &scope=openid%20mcp:tools%20mcp:resources
  &state=random-state-value
  &code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM
  &code_challenge_method=S256
  &resource=https://quicbaas.example.com/mcp
```

**Required Parameters:**
| Parameter | Description |
|-----------|-------------|
| `client_id` | Registered client identifier |
| `response_type` | Must be `code` |
| `redirect_uri` | Registered callback URL |
| `code_challenge` | Base64url-encoded SHA256 hash of verifier |
| `code_challenge_method` | Must be `S256` |

**Recommended Parameters:**
| Parameter | Description |
|-----------|-------------|
| `resource` | Target MCP server URI (RFC 8707) |
| `scope` | Space-separated scopes |
| `state` | CSRF protection value |

##### Step 3: Token Exchange

```bash
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code
&code=SplxlOBeZQQYbYS6WxSbIA
&redirect_uri=http://localhost:8080/callback
&client_id=mcp-client-123
&client_secret=secret123
&code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk
&resource=https://quicbaas.example.com/mcp
```

**Response:**

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "8xLOxBtZp8",
  "scope": "openid mcp:tools mcp:resources"
}
```

#### Client Credentials Flow (M2M)

For machine-to-machine authentication without user interaction:

```bash
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded
Authorization: Basic base64(client_id:client_secret)

grant_type=client_credentials
&scope=mcp:tools%20mcp:resources
&resource=https://quicbaas.example.com/mcp
```

#### Refresh Token Flow

```bash
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded

grant_type=refresh_token
&refresh_token=8xLOxBtZp8
&client_id=mcp-client-123
&client_secret=secret123
```

### MCP Request Authentication

All MCP requests must include the Bearer token in the Authorization header:

```bash
POST /mcp HTTP/1.1
Host: quicbaas.example.com
Content-Type: application/json
Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...
MCP-Protocol-Version: 2024-11-05

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "quicbaas_get",
    "arguments": {
      "entityType": "user",
      "id": "123"
    }
  }
}
```

### Error Responses

#### HTTP 401 Unauthorized

Returned when authentication fails:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="quicbaas-mcp", error="invalid_token", error_description="Token has expired"
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32001,
    "message": "authentication required"
  }
}
```

#### HTTP 403 Forbidden

Returned when the token lacks required scope:

```http
HTTP/1.1 403 Forbidden
WWW-Authenticate: Bearer realm="quicbaas-mcp", error="insufficient_scope", scope="mcp:admin"
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32003,
    "message": "permission denied: insufficient scope"
  }
}
```

#### MCP Error Codes

| Code | Name | Description |
|------|------|-------------|
| -32001 | AuthenticationRequired | No valid token provided |
| -32002 | InvalidToken | Token is malformed or expired |
| -32003 | PermissionDenied | Insufficient permissions |

### Dynamic Client Registration (RFC 7591)

Register new OAuth clients programmatically:

```bash
POST /oauth2/register
Content-Type: application/json

{
  "redirect_uris": ["https://mcp-client.example.com/callback"],
  "token_endpoint_auth_method": "client_secret_basic",
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"],
  "client_name": "My MCP Client",
  "client_uri": "https://mcp-client.example.com",
  "scope": "openid mcp:tools mcp:resources"
}
```

**Response:**

```json
{
  "client_id": "mcp-client-abc123",
  "client_secret": "generated-secret-xyz",
  "client_id_issued_at": 1704067200,
  "client_secret_expires_at": 0,
  "redirect_uris": ["https://mcp-client.example.com/callback"],
  "token_endpoint_auth_method": "client_secret_basic",
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"],
  "client_name": "My MCP Client"
}
```

**Redirect URI Requirements:**
- HTTPS required for production (except `localhost` for development)
- Exact match validation (no wildcards)

### Scopes

| Scope | Description |
|-------|-------------|
| `openid` | OpenID Connect identity |
| `profile` | User profile information |
| `email` | User email address |
| `offline_access` | Refresh token access |
| `mcp:tools` | Access to MCP tools |
| `mcp:resources` | Access to MCP resources |
| `mcp:admin` | Administrative operations |

### Resource Indicators (RFC 8707)

Resource indicators bind access tokens to specific resource servers:

```bash
# Authorization request with resource indicator
GET /oauth2/authorize?...&resource=https://quicbaas.example.com/mcp

# Token request with resource indicator
POST /oauth2/token
...
&resource=https://quicbaas.example.com/mcp
```

The resulting token will include the resource in its audience (`aud`) claim:

```json
{
  "iss": "https://quicbaas.example.com",
  "sub": "user-123",
  "aud": ["https://quicbaas.example.com/mcp"],
  "exp": 1704070800,
  "scope": "mcp:tools mcp:resources"
}
```

The MCP server validates that its resource URI is in the token's audience.

### Federation with External IdPs

QuicBaaS can delegate authentication to external identity providers:

#### Configuration

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

#### Flow

1. MCP client discovers external authorization server via Protected Resource Metadata
2. Client authenticates with external IdP (Okta, Auth0, etc.)
3. External IdP issues token with appropriate audience
4. MCP server validates token using external IdP's JWKS
5. QuicBaaS maps external claims to internal permissions

### Configuration

```yaml
mcp:
  transport: http
  address: ":8888"

  authorization:
    enabled: true
    resource_uri: "https://quicbaas.example.com/mcp"

  dynamic_registration:
    enabled: true
    require_https_redirect: true  # false for development
```

### Curl Examples

#### Discover Authorization Server

```bash
# Get OAuth 2.0 Authorization Server Metadata
curl -s https://quicbaas.example.com/.well-known/oauth-authorization-server | jq

# Get Protected Resource Metadata (MCP server info)
curl -s https://quicbaas.example.com/.well-known/oauth-protected-resource | jq
```

#### Register Client (Dynamic Registration)

```bash
curl -X POST https://quicbaas.example.com/oauth2/register \
  -H "Content-Type: application/json" \
  -d '{
    "redirect_uris": ["http://localhost:8080/callback"],
    "client_name": "My MCP Client",
    "grant_types": ["authorization_code", "refresh_token"],
    "response_types": ["code"],
    "token_endpoint_auth_method": "client_secret_basic"
  }'
```

#### Complete OAuth 2.1 Flow with PKCE

```bash
# Step 1: Generate PKCE values
CODE_VERIFIER=$(openssl rand -base64 32 | tr -d '=' | tr '/+' '_-')
CODE_CHALLENGE=$(echo -n "$CODE_VERIFIER" | openssl dgst -sha256 -binary | openssl base64 | tr -d '=' | tr '/+' '_-')

# Step 2: Build authorization URL (open in browser)
AUTH_URL="https://quicbaas.example.com/oauth2/authorize?\
client_id=mcp-client-123&\
response_type=code&\
redirect_uri=http://localhost:8080/callback&\
scope=openid%20mcp:tools%20mcp:resources&\
state=$(openssl rand -hex 16)&\
code_challenge=$CODE_CHALLENGE&\
code_challenge_method=S256&\
resource=https://quicbaas.example.com/mcp"

echo "Open in browser: $AUTH_URL"

# Step 3: Exchange code for tokens (after receiving code at callback)
curl -X POST https://quicbaas.example.com/oauth2/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=authorization_code" \
  -d "code=RECEIVED_AUTH_CODE" \
  -d "redirect_uri=http://localhost:8080/callback" \
  -d "client_id=mcp-client-123" \
  -d "client_secret=your-client-secret" \
  -d "code_verifier=$CODE_VERIFIER" \
  -d "resource=https://quicbaas.example.com/mcp"
```

#### Call MCP Endpoint (Authenticated)

```bash
# List available tools
curl -X POST https://quicbaas.example.com/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer eyJhbGciOiJSUzI1NiI..." \
  -H "MCP-Protocol-Version: 2024-11-05" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# Execute a tool
curl -X POST https://quicbaas.example.com/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer eyJhbGciOiJSUzI1NiI..." \
  -H "MCP-Protocol-Version: 2024-11-05" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "quicbaas_get",
      "arguments": {
        "entityType": "user",
        "id": "123"
      }
    }
  }'
```

#### Refresh Token

```bash
curl -X POST https://quicbaas.example.com/oauth2/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=refresh_token" \
  -d "refresh_token=8xLOxBtZp8..." \
  -d "client_id=mcp-client-123" \
  -d "client_secret=your-client-secret"
```

#### Client Credentials (M2M)

```bash
curl -X POST https://quicbaas.example.com/oauth2/token \
  -u "client_id:client_secret" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "scope=mcp:tools mcp:resources" \
  -d "resource=https://quicbaas.example.com/mcp"
```

### Security Best Practices

1. **Always use HTTPS** in production
2. **Use S256 PKCE** for all authorization code flows
3. **Validate redirect URIs** against registered values
4. **Use resource indicators** to bind tokens to specific servers
5. **Short token lifetimes** with refresh tokens for longer sessions
6. **Validate token audience** before processing requests
7. **Store tokens securely** (encrypted at rest)

### RFC References

| RFC | Title | Usage |
|-----|-------|-------|
| [RFC 6749](https://datatracker.ietf.org/doc/html/rfc6749) | OAuth 2.0 Authorization Framework | Base protocol |
| [RFC 6750](https://datatracker.ietf.org/doc/html/rfc6750) | Bearer Token Usage | Token format |
| [RFC 7636](https://datatracker.ietf.org/doc/html/rfc7636) | PKCE | Code exchange protection |
| [RFC 7591](https://datatracker.ietf.org/doc/html/rfc7591) | Dynamic Client Registration | Client registration |
| [RFC 8414](https://datatracker.ietf.org/doc/html/rfc8414) | Authorization Server Metadata | Discovery |
| [RFC 8707](https://datatracker.ietf.org/doc/html/rfc8707) | Resource Indicators | Audience binding |
| [RFC 9728](https://datatracker.ietf.org/doc/html/rfc9728) | Protected Resource Metadata | Resource discovery |
| [OAuth 2.1](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-v2-1) | OAuth 2.1 Draft | Security updates |

### Migration Guide

#### Breaking Changes in v1.1.0

**Removed Token Parameters:**

The following token delivery methods are **no longer supported**:

| Removed Parameter | Location | Replacement |
|------------------|----------|-------------|
| `_token` | Request arguments | `Authorization: Bearer` header |
| `accessToken` | Request params | `Authorization: Bearer` header |
| `token` query param | URL | `Authorization: Bearer` header |

**Why This Change:**
- Aligns with OAuth 2.1 and RFC 6750 Bearer Token Usage
- Prevents token leakage in logs and error messages
- Follows MCP Authorization Specification (2025-03-26)

#### From Token-in-Arguments to Bearer Header

**Before (removed in v1.1.0):**
```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "quicbaas_get",
    "arguments": {
      "_token": "eyJhbGciOiJSUzI1NiI...",
      "entityType": "user",
      "id": "123"
    }
  }
}
```

**After (required):**
```bash
curl -X POST https://quicbaas.example.com/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer eyJhbGciOiJSUzI1NiI..." \
  -H "MCP-Protocol-Version: 2024-11-05" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "quicbaas_get",
      "arguments": {
        "entityType": "user",
        "id": "123"
      }
    }
  }'
```

#### PKCE Requirements

All clients must implement PKCE with S256 (plain method is not supported):

1. Generate a random `code_verifier` (43-128 characters)
2. Compute `code_challenge = BASE64URL(SHA256(code_verifier))`
3. Include `code_challenge` and `code_challenge_method=S256` in authorization request
4. Include original `code_verifier` in token exchange

**Validation:**
- Server rejects `code_challenge_method=plain`
- Server rejects authorization requests without PKCE parameters
- Token exchange fails if `code_verifier` doesn't match stored challenge
