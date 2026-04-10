# QuicBaaS IAM Configuration

## Complete Configuration Reference

```yaml
# quicbaas.yaml

iam:
  # Enable/disable IAM module
  enabled: true

  # Deployment mode
  # - primary: This node is the IAM authority
  # - remote: Delegate to remote IAM cluster
  # - disabled: IAM module off (alias for enabled: false)
  mode: primary

  # Remote IAM settings (when mode=remote)
  remote:
    endpoint: "iam.internal:9443"
    sync_interval: 30s
    tls:
      enabled: true
      ca_cert: /etc/quicbaas/certs/ca.pem
      client_cert: /etc/quicbaas/certs/client.pem
      client_key: /etc/quicbaas/certs/client-key.pem

  # Multi-tenancy settings
  tenants:
    # Default tenant for requests without explicit tenant
    default: "default"

    # Tenant isolation mode
    # - strict: Complete key prefix isolation
    # - shared: Tenants can share designated resources
    isolation: strict

    # Auto-create tenants on first request
    auto_create: false

  # Authentication settings
  auth:
    # Local identity store
    local:
      enabled: true

      # WebAuthn/FIDO2 passwordless auth
      webauthn:
        enabled: true
        rp_id: "example.com"
        rp_name: "Example Corp"
        rp_origins:
          - "https://example.com"
          - "https://app.example.com"

        # Attestation preference: none, indirect, direct
        attestation: none

        # User verification: required, preferred, discouraged
        user_verification: preferred

      # Password auth (optional fallback)
      password:
        enabled: false
        min_length: 12
        require_uppercase: true
        require_number: true
        require_special: true
        bcrypt_cost: 12

    # External OIDC providers (social login)
    providers:
      google:
        enabled: true
        client_id: "${GOOGLE_CLIENT_ID}"
        client_secret: "${GOOGLE_CLIENT_SECRET}"
        scopes: ["openid", "email", "profile"]

      github:
        enabled: true
        client_id: "${GITHUB_CLIENT_ID}"
        client_secret: "${GITHUB_CLIENT_SECRET}"
        scopes: ["read:user", "user:email"]

      azure:
        enabled: false
        tenant_id: "${AZURE_TENANT_ID}"
        client_id: "${AZURE_CLIENT_ID}"
        client_secret: "${AZURE_CLIENT_SECRET}"
        scopes: ["openid", "email", "profile"]

      # Custom OIDC provider
      custom:
        enabled: false
        name: "Corporate SSO"
        issuer: "https://sso.corp.example.com"
        client_id: "${CUSTOM_CLIENT_ID}"
        client_secret: "${CUSTOM_CLIENT_SECRET}"
        scopes: ["openid", "email", "profile", "groups"]

  # OIDC Provider settings (issue tokens to apps)
  oidc:
    # Issuer URL (must match token iss claim)
    issuer: "https://iam.example.com"

    # Token signing
    signing_algorithm: RS256
    signing_key: "${OIDC_SIGNING_KEY}"
    # Or use file path:
    # signing_key_file: /etc/quicbaas/keys/oidc-signing.pem

    # Token lifetimes
    access_token_ttl: 1h
    refresh_token_ttl: 24h
    id_token_ttl: 1h
    auth_code_ttl: 10m

    # Supported flows
    flows:
      authorization_code: true
      client_credentials: true
      device_code: true
      refresh_token: true

    # PKCE settings
    pkce:
      required: true  # OAuth 2.1 mandates PKCE
      allowed_methods: ["S256"]  # Only S256 allowed per OAuth 2.1

    # Resource Indicators (RFC 8707)
    resource_indicators:
      enabled: true
      validate_audience: true

    # Dynamic Client Registration (RFC 7591)
    dynamic_registration:
      enabled: true
      require_https_redirect: true  # false for development

  # MCP Authorization Configuration
  mcp:
    authorization:
      # Enable OAuth 2.1 for MCP
      enabled: true

      # This MCP server's resource URI (used in token audience)
      resource_uri: "https://quicbaas.example.com/mcp"

      # Protected Resource Metadata (RFC 9728)
      resource_documentation: "https://quicbaas.example.com/docs/api/mcp"

      # Bearer token settings
      bearer:
        # Only accept tokens in Authorization header
        header_only: true

      # Error response settings
      errors:
        # Include WWW-Authenticate header per RFC 6750
        include_www_authenticate: true
        realm: "quicbaas-mcp"

    # Federation settings (inherit from main IAM config)
    # When IAM is configured with external providers (Okta, Auth0),
    # MCP will validate tokens from those providers
    inherit_federation: true

  # ABAC Policy Engine
  abac:
    # Policy source directory
    policy_path: /etc/quicbaas/policies/

    # Watch for policy file changes
    watch_policies: true

    # Policy cache settings
    cache_size: 10000
    cache_ttl: 5m

    # Compiled policy storage
    store_compiled: true

    # Default policy (when no policies match)
    default_effect: deny

    # Evaluation settings
    evaluation:
      # Maximum conditions per rule
      max_conditions: 50

      # Maximum rules per policy
      max_rules: 100

      # Timeout for policy evaluation
      timeout: 10ms

  # PEP (Policy Enforcement Point) settings
  pep:
    # gRPC port for policy sync
    grpc_port: 9444

    # Enable policy sync for external services
    sync_enabled: true

    # Maximum concurrent sync connections
    max_connections: 1000

    # Heartbeat interval
    heartbeat_interval: 30s

    # Policy update batch size
    batch_size: 100

  # Session management
  sessions:
    # Session storage
    storage: local  # local | redis

    # Session TTL
    ttl: 24h

    # Maximum sessions per user
    max_per_user: 10

    # Idle timeout
    idle_timeout: 1h

    # Redis settings (when storage=redis)
    redis:
      address: "localhost:6379"
      password: "${REDIS_PASSWORD}"
      db: 0

  # Audit logging
  audit:
    enabled: true

    # Events to log
    events:
      - auth.login
      - auth.logout
      - auth.failed
      - user.create
      - user.update
      - user.delete
      - policy.create
      - policy.update
      - policy.delete
      - policy.evaluate

    # Output destination
    output: file  # file | stdout | syslog

    # File settings
    file:
      path: /var/log/quicbaas/iam-audit.log
      max_size: 100MB
      max_backups: 10
      compress: true
```

## Deployment Mode Examples

### Integrated Mode (Dev/Small Deployments)

```yaml
# Single node with storage + IAM
iam:
  enabled: true
  mode: primary
```

### Dedicated IAM Cluster (Enterprise)

**IAM Cluster Nodes:**
```yaml
# iam-cluster.yaml
iam:
  enabled: true
  mode: primary

  oidc:
    issuer: "https://iam.prod.example.com"
```

**Data Cluster Nodes:**
```yaml
# data-cluster.yaml
iam:
  enabled: true
  mode: remote

  remote:
    endpoint: "iam-cluster.internal:9443"
    sync_interval: 30s
```

### Storage Only (Bring Your Own Auth)

```yaml
iam:
  enabled: false
```

## Environment Variable Substitution

Configuration values can reference environment variables:

```yaml
iam:
  auth:
    providers:
      google:
        client_id: "${GOOGLE_CLIENT_ID}"
        client_secret: "${GOOGLE_CLIENT_SECRET}"
```

Set in environment:
```bash
export GOOGLE_CLIENT_ID="123456789.apps.googleusercontent.com"
export GOOGLE_CLIENT_SECRET="secret-value"
```

## Network Configuration

### TLS Settings

```yaml
iam:
  tls:
    enabled: true
    cert_file: /etc/quicbaas/certs/server.pem
    key_file: /etc/quicbaas/certs/server-key.pem
    client_ca_file: /etc/quicbaas/certs/ca.pem
    client_auth: require  # none | request | require
```

### CORS Settings

```yaml
iam:
  cors:
    enabled: true
    allowed_origins:
      - "https://app.example.com"
      - "https://admin.example.com"
    allowed_methods: ["GET", "POST", "PUT", "DELETE"]
    allowed_headers: ["Authorization", "Content-Type"]
    max_age: 3600
```

## MCP Authorization Examples

### Built-in OIDC Provider (Default)

MCP clients authenticate via QuicBaaS's internal OIDC:

```yaml
iam:
  enabled: true
  mode: primary

  oidc:
    issuer: "https://quicbaas.example.com"
    pkce:
      required: true
      allowed_methods: ["S256"]

  mcp:
    authorization:
      enabled: true
      resource_uri: "https://quicbaas.example.com/mcp"
```

**Flow:**
1. Client discovers AS at `/.well-known/oauth-authorization-server`
2. Client authenticates with QuicBaaS OIDC (PKCE mandatory)
3. Token issued with `aud: ["https://quicbaas.example.com/mcp"]`
4. Client includes `Authorization: Bearer <token>` in MCP requests

### External IdP (Okta)

Delegate authentication to Okta while using QuicBaaS for data:

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

  mcp:
    authorization:
      enabled: true
      resource_uri: "https://quicbaas.example.com/mcp"
    inherit_federation: true
```

**Flow:**
1. Client discovers external AS via Protected Resource Metadata
2. Client authenticates with Okta (gets Okta token)
3. Client includes Okta token in MCP requests
4. QuicBaaS validates token via Okta's JWKS
5. Claims mapped to QuicBaaS permissions

### External IdP (Auth0)

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
        audience: "https://quicbaas.example.com/mcp"

  mcp:
    authorization:
      enabled: true
      resource_uri: "https://quicbaas.example.com/mcp"
```

### Machine-to-Machine (Client Credentials)

For service accounts and automation:

```yaml
iam:
  oidc:
    flows:
      client_credentials: true

  mcp:
    authorization:
      enabled: true
      resource_uri: "https://quicbaas.example.com/mcp"
```

```bash
# Get M2M token
curl -X POST https://quicbaas.example.com/oauth2/token \
  -H "Authorization: Basic $(echo -n 'client_id:client_secret' | base64)" \
  -d "grant_type=client_credentials&scope=mcp:tools&resource=https://quicbaas.example.com/mcp"
```

## See Also

- [MCP Authorization](../api/mcp-authorization.md) - Complete OAuth 2.1 reference
- [MCP Integration](mcp-integration.md) - IdP federation and role mapping
- [Developer Guide](developer-guide.md) - Integration examples
- [ABAC Policies](abac.md) - Fine-grained access control
