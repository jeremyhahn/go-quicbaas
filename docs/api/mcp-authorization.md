# MCP Authorization

OAuth 2.1 authorization for the Model Context Protocol (MCP) API.

> **Breaking Change (v1.1.0):** Token parameters in request body (`_token`, `accessToken`) are no longer supported. All authenticated requests must use the `Authorization: Bearer <token>` header. See [Migration Guide](#migration-guide) for details.

## Overview

QuicBaaS's MCP server implements the [MCP Authorization Specification (2025)](https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization), providing secure OAuth 2.1 authentication for AI assistants and MCP clients.

**Key Features:**
- OAuth 2.1 with mandatory PKCE (S256)
- Bearer token authentication
- Resource Indicators (RFC 8707)
- Dynamic Client Registration (RFC 7591)
- Federation with external IdPs (Okta, Auth0, etc.)

## Architecture

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

## Discovery Endpoints

### OAuth Authorization Server Metadata (RFC 8414)

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

### Protected Resource Metadata (RFC 9728)

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

### OpenID Connect Discovery

```bash
GET /.well-known/openid-configuration
```

Returns OIDC discovery document (compatible with OAuth AS Metadata).

## Authorization Flows

### Authorization Code Flow with PKCE

PKCE is **mandatory** for all clients. Only the S256 challenge method is supported.

#### Step 1: Generate PKCE Values

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

#### Step 2: Authorization Request

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

#### Step 3: Token Exchange

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

### Client Credentials Flow (M2M)

For machine-to-machine authentication without user interaction:

```bash
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded
Authorization: Basic base64(client_id:client_secret)

grant_type=client_credentials
&scope=mcp:tools%20mcp:resources
&resource=https://quicbaas.example.com/mcp
```

### Refresh Token Flow

```bash
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded

grant_type=refresh_token
&refresh_token=8xLOxBtZp8
&client_id=mcp-client-123
&client_secret=secret123
```

## MCP Request Authentication

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

## Error Responses

### HTTP 401 Unauthorized

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

### HTTP 403 Forbidden

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

### MCP Error Codes

| Code | Name | Description |
|------|------|-------------|
| -32001 | AuthenticationRequired | No valid token provided |
| -32002 | InvalidToken | Token is malformed or expired |
| -32003 | PermissionDenied | Insufficient permissions |

## Dynamic Client Registration (RFC 7591)

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

## Scopes

| Scope | Description |
|-------|-------------|
| `openid` | OpenID Connect identity |
| `profile` | User profile information |
| `email` | User email address |
| `offline_access` | Refresh token access |
| `mcp:tools` | Access to MCP tools |
| `mcp:resources` | Access to MCP resources |
| `mcp:admin` | Administrative operations |

## Resource Indicators (RFC 8707)

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

## Federation with External IdPs

QuicBaaS can delegate authentication to external identity providers:

### Configuration

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

### Flow

1. MCP client discovers external authorization server via Protected Resource Metadata
2. Client authenticates with external IdP (Okta, Auth0, etc.)
3. External IdP issues token with appropriate audience
4. MCP server validates token using external IdP's JWKS
5. QuicBaaS maps external claims to internal permissions

## Configuration

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

## Curl Examples

### Discover Authorization Server

```bash
# Get OAuth 2.0 Authorization Server Metadata
curl -s https://quicbaas.example.com/.well-known/oauth-authorization-server | jq

# Get Protected Resource Metadata (MCP server info)
curl -s https://quicbaas.example.com/.well-known/oauth-protected-resource | jq
```

### Register Client (Dynamic Registration)

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

### Complete OAuth 2.1 Flow with PKCE

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

### Call MCP Endpoint (Authenticated)

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

### Refresh Token

```bash
curl -X POST https://quicbaas.example.com/oauth2/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=refresh_token" \
  -d "refresh_token=8xLOxBtZp8..." \
  -d "client_id=mcp-client-123" \
  -d "client_secret=your-client-secret"
```

### Client Credentials (M2M)

```bash
curl -X POST https://quicbaas.example.com/oauth2/token \
  -u "client_id:client_secret" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "scope=mcp:tools mcp:resources" \
  -d "resource=https://quicbaas.example.com/mcp"
```

## Security Best Practices

1. **Always use HTTPS** in production
2. **Use S256 PKCE** for all authorization code flows
3. **Validate redirect URIs** against registered values
4. **Use resource indicators** to bind tokens to specific servers
5. **Short token lifetimes** with refresh tokens for longer sessions
6. **Validate token audience** before processing requests
7. **Store tokens securely** (encrypted at rest)

## RFC References

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

## Migration Guide

### Breaking Changes in v1.1.0

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

### From Token-in-Arguments to Bearer Header

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

### PKCE Requirements

All clients must implement PKCE with S256 (plain method is not supported):

1. Generate a random `code_verifier` (43-128 characters)
2. Compute `code_challenge = BASE64URL(SHA256(code_verifier))`
3. Include `code_challenge` and `code_challenge_method=S256` in authorization request
4. Include original `code_verifier` in token exchange

**Validation:**
- Server rejects `code_challenge_method=plain`
- Server rejects authorization requests without PKCE parameters
- Token exchange fails if `code_verifier` doesn't match stored challenge
