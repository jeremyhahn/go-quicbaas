# MCP IAM Integration

How MCP authorization integrates with QuicBaaS's Identity and Access Management system.

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

- [MCP Authorization](../api/mcp-authorization.md) - OAuth 2.1 protocol reference
- [Authentication](../security/authentication.md) - General authentication guide
- [IAM Configuration](configuration.md) - Complete IAM config reference
- [ABAC Policies](abac.md) - Fine-grained access control
