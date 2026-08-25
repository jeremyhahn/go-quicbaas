# IAM — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/iam/`.

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

    # Token signing is not configurable: an ES256 key (common name
    # "oidc-signing") is generated in the configured xkms backend on first
    # boot and never leaves the keystore. Dedicated tenants may opt in to
    # their own signing key via tenant OIDC settings.

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

    # Dynamic Client Registration (RFC 7591) — see also
    # docs/security/authentication.md. Registering a client mints an
    # authentication principal, so this surface is off by default.
    dynamic_client_registration:
      # false (default): POST /oauth2/register answers 403 access_denied.
      enabled: false

      # Empty (default): an initial access token is REQUIRED; a token-less
      # registration is refused with 401 and the new client is created in the
      # tenant that token authenticated into.
      #
      # Setting this enables UNAUTHENTICATED registration and names the tenant
      # such clients land in. It must never be "system": anonymous callers may
      # not create control-plane clients (rejected by config validation and
      # again at request time).
      anonymous_tenant_id: ""

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

- [MCP Authorization](../oauth2-oidc/api.md) - Complete OAuth 2.1 reference
- MCP Integration - IdP federation and role mapping
- [Developer Guide](engineering.md) - Integration examples
- [ABAC Policies](../authorization/architecture.md) - Fine-grained access control


---

## QuicBaaS IAM Developer Guide

How to integrate your applications with QuicBaaS IAM for SSO and ABAC authorization.

### Overview

QuicBaaS IAM provides:
- **SSO (Single Sign-On)**: One login works across all your applications
- **ABAC (Attribute-Based Access Control)**: Fine-grained authorization
- **PEP (Policy Enforcement Point)**: Local policy evaluation (~2-5μs)

### Integration Flow

```
┌──────┐      ┌──────────────┐      ┌──────────────┐      ┌─────────────┐
│ User │─────▶│ App Frontend │─────▶│ QuicBaaS IAM │─────▶│ Identity    │
│      │      │ (React/Vue)  │      │ /auth/login  │      │ Provider    │
└──────┘      └──────────────┘      └──────────────┘      │ (any OIDC)  │
   │                                       │               └─────────────┘
   │                                       ▼
   │                              ┌──────────────┐
   │                              │ QuicBaaS IAM │
   │                              │ Issues JWT   │
   │◀─────────────────────────────┤ with claims  │
   │         JWT token            └──────────────┘
   │
   │         SAME JWT WORKS ACROSS ALL APPS (SSO)
   │
   │    ┌────────────┐    ┌────────────┐    ┌────────────┐
   └───▶│   App A    │    │   App B    │    │   App C    │
        │ (Orders)   │    │ (Billing)  │    │ (Reports)  │
        │  Same JWT  │    │  Same JWT  │    │  Same JWT  │
        └────────────┘    └────────────┘    └────────────┘
```

### Step 1: Register Your Application

Use the CLI to register your app with QuicBaaS IAM:

```bash
quicbaas iam app register \
    --name "order-service" \
    --tenant "acme-corp" \
    --redirect-uris "https://orders.acme.com/callback" \
    --resource-prefix "orders://"
```

Output:
```
App registered successfully!

Client ID:     app_ord_abc123xyz
Client Secret: sk_live_xxxxxxxxxxxxx

OIDC Endpoints:
- Issuer:    https://iam.quicbaas.acme.com
- Authorize: https://iam.quicbaas.acme.com/oauth2/authorize
- Token:     https://iam.quicbaas.acme.com/oauth2/token
- UserInfo:  https://iam.quicbaas.acme.com/oauth2/userinfo
- JWKS:      https://iam.quicbaas.acme.com/.well-known/jwks.json

Resource namespace registered: orders://
```

### Step 2: Configure Your App

Create a configuration file:

```yaml
# config/quicbaas-iam.yaml

quicbaas_iam:
  endpoint: "iam.quicbaas.acme.com:443"
  tenant: "acme-corp"

  oauth:
    client_id: "app_ord_abc123xyz"
    client_secret: "${QUICBAAS_CLIENT_SECRET}"
    redirect_uri: "https://orders.acme.com/callback"
    scopes: ["openid", "profile", "email", "groups"]

  pep:
    resource_prefix: "orders://"
    cache_ttl: 5m
    sync_interval: 30s
```

### Step 3: Backend Integration (Go)

#### Initialize IAM Client

```go
package main

import (
    "github.com/your-org/quicbaas-go/iam"
)

func main() {
    // Load configuration
    config := iam.LoadConfig("config/quicbaas-iam.yaml")

    // Initialize IAM client
    iamClient, err := iam.NewClient(config)
    if err != nil {
        log.Fatal(err)
    }

    // Create router with IAM middleware
    router := chi.NewRouter()

    // Validate JWT on every request
    router.Use(iamClient.AuthMiddleware())

    // Optional: Check ABAC policies automatically
    router.Use(iamClient.ABACMiddleware())

    // Register routes
    router.Get("/api/orders/{id}", GetOrder)
    router.Post("/api/orders", CreateOrder)

    http.ListenAndServe(":8080", router)
}
```

#### Check Permissions in Handlers

```go
func GetOrder(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    orderID := chi.URLParam(r, "id")

    // Get user from context (set by AuthMiddleware)
    user := iam.UserFromContext(ctx)

    // Check permission with PEP (local evaluation, ~2-5μs)
    allowed, err := iam.Check(ctx, iam.CheckRequest{
        Subject:  user,
        Action:   "read",
        Resource: fmt.Sprintf("orders://orders/%s", orderID),
    })

    if err != nil || !allowed {
        http.Error(w, "Forbidden", http.StatusForbidden)
        return
    }

    // Fetch and return order...
    order := db.GetOrder(orderID)
    json.NewEncoder(w).Encode(order)
}
```

#### Batch Permission Checks

```go
func ListOrders(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    user := iam.UserFromContext(ctx)

    // Get all orders
    orders := db.GetAllOrders()

    // Build resources to check
    resources := make([]string, len(orders))
    for i, o := range orders {
        resources[i] = fmt.Sprintf("orders://orders/%s", o.ID)
    }

    // Batch check (more efficient)
    results, _ := iam.CheckBatch(ctx, iam.BatchRequest{
        Subject:   user,
        Action:    "read",
        Resources: resources,
    })

    // Filter to allowed orders
    var allowed []Order
    for i, order := range orders {
        if results[i] {
            allowed = append(allowed, order)
        }
    }

    json.NewEncoder(w).Encode(allowed)
}
```

### Step 4: Frontend Integration (React)

#### Setup Provider

```jsx
// App.jsx
import { QuicBaaSIAMProvider } from '@quicbaas/iam-react';

const iamConfig = {
  issuer: 'https://iam.quicbaas.acme.com',
  clientId: 'app_ord_abc123xyz',
  redirectUri: 'https://orders.acme.com/callback',
  scopes: ['openid', 'profile', 'email', 'groups'],
};

function App() {
  return (
    <QuicBaaSIAMProvider config={iamConfig}>
      <Router>
        <Routes>
          <Route path="/callback" element={<AuthCallback />} />
          <Route path="/login" element={<LoginPage />} />
          <Route path="/orders" element={<ProtectedRoute><OrdersPage /></ProtectedRoute>} />
        </Routes>
      </Router>
    </QuicBaaSIAMProvider>
  );
}
```

#### Login Button

```jsx
// LoginButton.jsx
import { useQuicBaaSIAM } from '@quicbaas/iam-react';

function LoginButton() {
  const { login, isAuthenticated, user } = useQuicBaaSIAM();

  if (isAuthenticated) {
    return <span>Welcome, {user.name}</span>;
  }

  return <button onClick={login}>Sign In</button>;
}
```

#### Protected Routes

```jsx
// ProtectedRoute.jsx
import { useQuicBaaSIAM } from '@quicbaas/iam-react';
import { Navigate } from 'react-router-dom';

function ProtectedRoute({ children }) {
  const { isAuthenticated, isLoading } = useQuicBaaSIAM();

  if (isLoading) {
    return <div>Loading...</div>;
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" />;
  }

  return children;
}
```

#### API Calls with JWT

```jsx
// api.js
import { useQuicBaaSIAM } from '@quicbaas/iam-react';

function useAPI() {
  const { getAccessToken } = useQuicBaaSIAM();

  const fetchWithAuth = async (url, options = {}) => {
    const token = await getAccessToken();

    return fetch(url, {
      ...options,
      headers: {
        ...options.headers,
        'Authorization': `Bearer ${token}`,
      },
    });
  };

  return { fetchWithAuth };
}

// Usage
function OrdersPage() {
  const { fetchWithAuth } = useAPI();
  const [orders, setOrders] = useState([]);

  useEffect(() => {
    fetchWithAuth('/api/orders')
      .then(res => res.json())
      .then(setOrders);
  }, []);

  return (
    <ul>
      {orders.map(o => <li key={o.id}>{o.name}</li>)}
    </ul>
  );
}
```

### Step 5: Define ABAC Policies

Create policies for your application's resources:

```yaml
# policies/order-service.yaml

apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: order-service-access
  tenant: acme-corp

spec:
  variables:
    user_orders: "orders://orders/user/${subject.id}/**"
    team_orders: "orders://orders/team/${subject.team_id}/**"

  rules:
    # Users can read their own orders
    - name: read-own-orders
      effect: allow
      actions: [read, list]
      resources:
        - ${user_orders}
      subjects:
        authenticated: true

    # Users can create orders for themselves
    - name: create-own-orders
      effect: allow
      actions: [create]
      resources:
        - orders://orders/user/${subject.id}/*
      subjects:
        authenticated: true

    # Team leads can view all team orders
    - name: team-lead-read
      effect: allow
      actions: [read, list]
      resources:
        - ${team_orders}
      subjects:
        roles: [team-lead]

    # Support staff can read any order (business hours only)
    - name: support-read-all
      effect: allow
      actions: [read]
      resources:
        - orders://orders/**
      subjects:
        groups: [customer-support]
      when:
        - time.hour between 8..18
        - time.weekday in [mon, tue, wed, thu, fri]

    # Admins have full access with MFA
    - name: admin-full-access
      effect: allow
      actions: ["*"]
      resources:
        - orders://**
      subjects:
        roles: [admin]
      when:
        - subject.mfa_verified exists
```

Apply the policy:

```bash
quicbaas iam policy apply -f policies/order-service.yaml
```

### JWT Token Structure

The JWT issued by QuicBaaS IAM contains:

```json
{
  // Standard OIDC claims
  "iss": "https://iam.quicbaas.acme.com",
  "sub": "user_abc123",
  "aud": ["app_ord_abc123xyz", "app_billing_def456"],
  "exp": 1735689600,
  "iat": 1735686000,

  // User identity
  "email": "alice@acme.com",
  "name": "Alice Smith",
  "email_verified": true,

  // QuicBaaS IAM claims (used by ABAC)
  "tenant_id": "acme-corp",
  "roles": ["developer", "team-lead"],
  "groups": ["engineering", "project-alpha"],
  "team_id": "team-eng-west",
  "department": "engineering",

  // Authentication context
  "auth_method": "webauthn",
  "mfa_verified": true,
  "mfa_timestamp": 1735686000
}
```

### SSO Experience

When a user logs in to one app, they're automatically logged into all apps:

1. User logs into Order App
2. User visits Billing App
3. Billing App redirects to QuicBaaS IAM
4. IAM detects existing session (no login prompt!)
5. IAM issues JWT for Billing App
6. User is instantly authenticated

### Integration Summary

| Step | Time | Description |
|------|------|-------------|
| 1. Register app | ~1 min | CLI command |
| 2. Add config | ~5 min | YAML configuration |
| 3. Add SDK | ~1 min | Package dependency |
| 4. Add middleware | ~10 min | Auth + ABAC middleware |
| 5. Define policies | ~30 min | YAML policies |
| **Total** | **<1 hour** | Basic integration |

### What You DON'T Have to Build

- Login/signup pages
- Password hashing/storage
- Session management
- JWT issuance/validation
- OAuth2 flows
- Social login integration
- MFA/WebAuthn implementation
- User management UI
- Role/permission management
- Policy evaluation engine

All provided by QuicBaaS IAM!

### SDK Reference

#### Go SDK

```go
// Initialize
client, _ := iam.NewClient(config)

// Middleware
router.Use(client.AuthMiddleware())
router.Use(client.ABACMiddleware())

// Get user from context
user := iam.UserFromContext(ctx)

// Check permission
allowed, _ := iam.Check(ctx, iam.CheckRequest{...})

// Batch check
results, _ := iam.CheckBatch(ctx, iam.BatchRequest{...})
```

#### JavaScript/TypeScript SDK

```typescript
// Initialize
const iam = new QuicBaaSIAM(config);

// React hooks
const { login, logout, user, isAuthenticated } = useQuicBaaSIAM();

// Get access token
const token = await iam.getAccessToken();

// Check permission (client-side, for UI)
const allowed = await iam.check({ action: 'read', resource: 'orders://...' });
```

#### Python SDK

```python
from quicbaas_iam import IAMClient, auth_required, check_access

# Initialize
client = IAMClient(config)

# Decorator for Flask/FastAPI
@auth_required
def get_order(order_id: str):
    ...

# Check permission
if check_access(user, 'read', f'orders://orders/{order_id}'):
    ...
```

### MCP Client Integration

AI assistants and MCP clients authenticate using OAuth 2.1 with mandatory PKCE. This section covers how to build MCP clients that integrate with QuicBaaS IAM.

#### Discovery Flow

MCP clients discover authorization requirements through standard endpoints:

```
┌─────────────────┐                    ┌─────────────────────────┐
│   MCP Client    │                    │      QuicBaaS           │
│ (Claude, etc.)  │                    │                         │
└────────┬────────┘                    └────────────┬────────────┘
         │                                          │
         │ 1. GET /.well-known/oauth-protected-resource
         │─────────────────────────────────────────▶│
         │                                          │
         │     { "resource": "https://db.example.com/mcp",
         │◀──────"authorization_servers": [...] }──│
         │                                          │
         │ 2. GET /.well-known/oauth-authorization-server
         │─────────────────────────────────────────▶│
         │                                          │
         │     { "issuer": "...", "authorization_endpoint": "...",
         │◀──────"token_endpoint": "...", ... }────│
         │                                          │
         │ 3. OAuth 2.1 + PKCE Flow                 │
         │◀────────────────────────────────────────▶│
         │                                          │
         │ 4. MCP Request with Bearer Token         │
         │─────────────────────────────────────────▶│
         │                                          │
```

#### Step 1: Discover Authorization Server

```python
import requests

MCP_SERVER = "https://quicbaas.example.com"

# Get protected resource metadata (RFC 9728)
resource_meta = requests.get(
    f"{MCP_SERVER}/.well-known/oauth-protected-resource"
).json()

# Get authorization server metadata (RFC 8414)
as_uri = resource_meta["authorization_servers"][0]
as_meta = requests.get(
    f"{as_uri}/.well-known/oauth-authorization-server"
).json()

print(f"Resource URI: {resource_meta['resource']}")
print(f"Token Endpoint: {as_meta['token_endpoint']}")
```

#### Step 2: Implement PKCE (Mandatory)

PKCE with S256 is **required** for all authorization code flows:

```python
import secrets
import hashlib
import base64

def generate_pkce():
    """Generate PKCE code verifier and challenge."""
    # Generate code verifier (43-128 characters)
    code_verifier = secrets.token_urlsafe(32)

    # Generate code challenge (S256)
    digest = hashlib.sha256(code_verifier.encode()).digest()
    code_challenge = base64.urlsafe_b64encode(digest).rstrip(b'=').decode()

    return code_verifier, code_challenge

code_verifier, code_challenge = generate_pkce()
```

#### Step 3: Authorization Request

```python
import urllib.parse

def build_auth_url(as_meta, client_id, redirect_uri, code_challenge, resource_uri):
    """Build OAuth 2.1 authorization URL."""
    params = {
        "client_id": client_id,
        "response_type": "code",
        "redirect_uri": redirect_uri,
        "scope": "openid mcp:tools mcp:resources",
        "state": secrets.token_urlsafe(16),
        "code_challenge": code_challenge,
        "code_challenge_method": "S256",
        "resource": resource_uri,  # RFC 8707
    }

    return f"{as_meta['authorization_endpoint']}?{urllib.parse.urlencode(params)}"

auth_url = build_auth_url(
    as_meta,
    client_id="my-mcp-client",
    redirect_uri="http://localhost:8080/callback",
    code_challenge=code_challenge,
    resource_uri=resource_meta["resource"]
)

print(f"Open in browser: {auth_url}")
```

#### Step 4: Token Exchange

```python
def exchange_code(as_meta, code, code_verifier, client_id, client_secret, redirect_uri, resource_uri):
    """Exchange authorization code for tokens."""
    response = requests.post(
        as_meta["token_endpoint"],
        data={
            "grant_type": "authorization_code",
            "code": code,
            "redirect_uri": redirect_uri,
            "client_id": client_id,
            "client_secret": client_secret,
            "code_verifier": code_verifier,
            "resource": resource_uri,
        }
    )

    return response.json()

tokens = exchange_code(
    as_meta,
    code="received_auth_code",
    code_verifier=code_verifier,
    client_id="my-mcp-client",
    client_secret="my-secret",
    redirect_uri="http://localhost:8080/callback",
    resource_uri=resource_meta["resource"]
)

access_token = tokens["access_token"]
refresh_token = tokens.get("refresh_token")
```

#### Step 5: Make MCP Requests

```python
def mcp_call(access_token, method, params=None):
    """Make authenticated MCP JSON-RPC call."""
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": method,
    }
    if params:
        payload["params"] = params

    response = requests.post(
        f"{MCP_SERVER}/mcp",
        json=payload,
        headers={
            "Authorization": f"Bearer {access_token}",
            "Content-Type": "application/json",
            "MCP-Protocol-Version": "2024-11-05",
        }
    )

    return response.json()

# List available tools
tools = mcp_call(access_token, "tools/list")

# Call a tool
result = mcp_call(access_token, "tools/call", {
    "name": "quicbaas_get",
    "arguments": {
        "entityType": "user",
        "id": "123"
    }
})
```

#### Machine-to-Machine (Client Credentials)

For service accounts without user interaction:

```python
def get_m2m_token(as_meta, client_id, client_secret, resource_uri):
    """Get token using client credentials flow."""
    response = requests.post(
        as_meta["token_endpoint"],
        auth=(client_id, client_secret),
        data={
            "grant_type": "client_credentials",
            "scope": "mcp:tools mcp:resources",
            "resource": resource_uri,
        }
    )

    return response.json()["access_token"]

token = get_m2m_token(
    as_meta,
    client_id="service-account",
    client_secret="service-secret",
    resource_uri=resource_meta["resource"]
)
```

#### Dynamic Client Registration

Register new MCP clients programmatically (RFC 7591):

```python
def register_client(as_meta, client_name, redirect_uris):
    """Register a new OAuth client dynamically."""
    response = requests.post(
        as_meta["registration_endpoint"],
        json={
            "client_name": client_name,
            "redirect_uris": redirect_uris,
            "grant_types": ["authorization_code", "refresh_token"],
            "response_types": ["code"],
            "token_endpoint_auth_method": "client_secret_basic",
            "scope": "openid mcp:tools mcp:resources",
        }
    )

    return response.json()

client = register_client(
    as_meta,
    client_name="My AI Assistant",
    redirect_uris=["https://my-app.example.com/callback"]
)

print(f"Client ID: {client['client_id']}")
print(f"Client Secret: {client['client_secret']}")
```

#### Token Refresh

```python
def refresh_access_token(as_meta, refresh_token, client_id, client_secret):
    """Refresh an expired access token."""
    response = requests.post(
        as_meta["token_endpoint"],
        auth=(client_id, client_secret),
        data={
            "grant_type": "refresh_token",
            "refresh_token": refresh_token,
        }
    )

    return response.json()
```

#### Error Handling

Handle OAuth and MCP authorization errors:

```python
def handle_mcp_response(response):
    """Handle MCP response with proper error handling."""
    if response.status_code == 401:
        # Token invalid or expired
        www_auth = response.headers.get("WWW-Authenticate", "")
        if "invalid_token" in www_auth:
            raise AuthError("Token expired, refresh required")
        raise AuthError("Authentication required")

    if response.status_code == 403:
        # Insufficient scope/permissions
        www_auth = response.headers.get("WWW-Authenticate", "")
        raise AuthError(f"Permission denied: {www_auth}")

    data = response.json()
    if "error" in data:
        code = data["error"]["code"]
        msg = data["error"]["message"]
        if code == -32001:
            raise AuthError("Authentication required")
        elif code == -32002:
            raise AuthError("Invalid token")
        elif code == -32003:
            raise AuthError(f"Permission denied: {msg}")
        raise MCPError(f"MCP error {code}: {msg}")

    return data["result"]
```

#### Complete MCP Client Example

```python
"""Complete MCP client with OAuth 2.1 authentication."""

import secrets
import hashlib
import base64
import requests
from urllib.parse import urlencode

class MCPClient:
    def __init__(self, mcp_server: str, client_id: str, client_secret: str):
        self.mcp_server = mcp_server
        self.client_id = client_id
        self.client_secret = client_secret
        self.access_token = None
        self.refresh_token = None

        # Discover endpoints
        self._discover()

    def _discover(self):
        """Discover OAuth and MCP endpoints."""
        # Protected Resource Metadata
        res = requests.get(f"{self.mcp_server}/.well-known/oauth-protected-resource")
        self.resource_meta = res.json()
        self.resource_uri = self.resource_meta["resource"]

        # Authorization Server Metadata
        as_uri = self.resource_meta["authorization_servers"][0]
        res = requests.get(f"{as_uri}/.well-known/oauth-authorization-server")
        self.as_meta = res.json()

    def get_auth_url(self, redirect_uri: str) -> tuple[str, str, str]:
        """Get authorization URL with PKCE."""
        verifier = secrets.token_urlsafe(32)
        challenge = base64.urlsafe_b64encode(
            hashlib.sha256(verifier.encode()).digest()
        ).rstrip(b'=').decode()
        state = secrets.token_urlsafe(16)

        params = {
            "client_id": self.client_id,
            "response_type": "code",
            "redirect_uri": redirect_uri,
            "scope": "openid mcp:tools mcp:resources offline_access",
            "state": state,
            "code_challenge": challenge,
            "code_challenge_method": "S256",
            "resource": self.resource_uri,
        }

        url = f"{self.as_meta['authorization_endpoint']}?{urlencode(params)}"
        return url, verifier, state

    def exchange_code(self, code: str, verifier: str, redirect_uri: str):
        """Exchange authorization code for tokens."""
        res = requests.post(
            self.as_meta["token_endpoint"],
            data={
                "grant_type": "authorization_code",
                "code": code,
                "redirect_uri": redirect_uri,
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "code_verifier": verifier,
                "resource": self.resource_uri,
            }
        )
        tokens = res.json()
        self.access_token = tokens["access_token"]
        self.refresh_token = tokens.get("refresh_token")

    def authenticate_m2m(self):
        """Authenticate using client credentials (M2M)."""
        res = requests.post(
            self.as_meta["token_endpoint"],
            auth=(self.client_id, self.client_secret),
            data={
                "grant_type": "client_credentials",
                "scope": "mcp:tools mcp:resources",
                "resource": self.resource_uri,
            }
        )
        self.access_token = res.json()["access_token"]

    def call(self, method: str, params: dict = None) -> dict:
        """Make authenticated MCP call."""
        if not self.access_token:
            raise RuntimeError("Not authenticated")

        payload = {"jsonrpc": "2.0", "id": 1, "method": method}
        if params:
            payload["params"] = params

        res = requests.post(
            f"{self.mcp_server}/mcp",
            json=payload,
            headers={
                "Authorization": f"Bearer {self.access_token}",
                "Content-Type": "application/json",
                "MCP-Protocol-Version": "2024-11-05",
            }
        )

        if res.status_code == 401:
            # Try refresh
            if self.refresh_token:
                self._refresh()
                return self.call(method, params)
            raise RuntimeError("Authentication expired")

        return res.json()

    def _refresh(self):
        """Refresh access token."""
        res = requests.post(
            self.as_meta["token_endpoint"],
            auth=(self.client_id, self.client_secret),
            data={
                "grant_type": "refresh_token",
                "refresh_token": self.refresh_token,
            }
        )
        tokens = res.json()
        self.access_token = tokens["access_token"]
        if "refresh_token" in tokens:
            self.refresh_token = tokens["refresh_token"]

# Usage
client = MCPClient(
    mcp_server="https://quicbaas.example.com",
    client_id="my-client",
    client_secret="my-secret"
)

# For M2M (service accounts)
client.authenticate_m2m()

# List tools
tools = client.call("tools/list")

# Call a tool
result = client.call("tools/call", {
    "name": "quicbaas_put",
    "arguments": {
        "entityType": "user",
        "id": 123,
        "data": base64.b64encode(b'{"name": "Alice"}').decode()
    }
})
```

#### MCP Scopes

| Scope | Description |
|-------|-------------|
| `openid` | OpenID Connect identity |
| `mcp:tools` | Access to MCP tools |
| `mcp:resources` | Access to MCP resources |
| `mcp:admin` | Administrative operations |
| `offline_access` | Refresh token access |

#### See Also

- [MCP Authorization](../oauth2-oidc/api.md) - Complete OAuth 2.1 reference
- [MCP API](../mcp-server/api.md) - MCP protocol documentation
- IAM Configuration - Server configuration


---

## Session Renewal

How a QuicBaaS session survives past its 15-minute access token.

### Why renewal exists

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

### The credential

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

### What a renewal reproduces

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

### Where each piece lives

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

#### Why the ceremony layers it on in the service facade

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

### Endpoints

#### `POST /api/v1/auth/login/{begin,finish}` — unified ceremony

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

#### `POST /api/v1/auth/refresh` — renewal (public)

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

#### `POST /oauth2/token` with `grant_type=refresh_token`

The OAuth2 grant is for registered clients (it authenticates `client_id` /
`client_secret`). It rotates too. SPA and CLI sessions are **not** OAuth
clients and use `/api/v1/auth/refresh`.

### Client behaviour

#### SPA

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

#### CLI / MCP

- `quicbaas auth login` writes `access_token`, `refresh_token`, `expires_at`,
  and `token_type` into the `cli.TokenData` envelope at `~/.quicbaas/token` on
  every successful login, and prints no token to stdout unless `--show-token`
  is passed (owner ruling on #58). `--save-token` is deprecated and ignored:
  storing the credential is unconditional.
- `loadStoredAuthToken()` resolves through `cli.TokenManager`: a token inside
  the skew window is renewed first and the rotated pair is written back.
- Renewal is single-flight (mutex plus a re-read under the lock), so parallel
  invocations do not each redeem a single-use credential.
- The renewal client is deliberately token-less: the endpoint is public, and an
  authenticated client would recurse back into `loadStoredAuthToken()`.

This is what lets `quicbaas mcp serve --stdio`, which an MCP host keeps alive
for hours, survive well past the first 15-minute boundary.

### Testing

| Layer | Tests |
| --- | --- |
| Issue / rotate / replay / DPoP / expiry | `pkg/iam/oidc_session_refresh_test.go` |
| Service verbs against a real provider | `pkg/service/auth_session_test.go` |
| Ceremony attaches the credential | `pkg/service/login_service_refresh_test.go` |
| REST wiring + DPoP passthrough | `pkg/api/rest/iam_session_refresh_test.go` |
| CLI renewal, rotation write-back, single-flight | `pkg/cli/token_refresh_test.go` |
| SPA pre-emptive + 401 recovery | `cmd/quicbaas/frontend/test/authRefresh.test.ts` |
| End to end against a live server | `test/integration/iam/session_refresh_test.go` |


---

## WebAuthn / FIDO2 Authentication

### Overview

QuicBaaS IAM provides passwordless authentication via WebAuthn/FIDO2,
supporting hardware security keys (YubiKey, etc.) and platform
authenticators (Touch ID, Windows Hello, Android biometrics).

**Phase 5 update (2026-05-07).** The WebAuthn relying-party logic is no
longer reimplemented inside `pkg/iam`. The canonical implementation
lives in `go-xkms/pkg/webauthn`; quicbaas wraps it with a thin
multi-tenant manager (`iam.XKMSWebAuthnManager`) and per-tenant
service factory (`iam.WebAuthnServiceFactory`). User and credential
persistence flows through xkms's qrdb adapter
(`xkms.NewQRDBUserStoreAdapter`) so each tenant has an isolated user
namespace inside the platform tenant's qrdb keyspace.

REST surface is mounted at one canonical path:

- `/api/v1/iam/{tenant_id}/webauthn/{registration,login}/{begin,finish}`
  (plus `registration/status`), served by xkms's native WebAuthn HTTP
  handler via the per-tenant factory.

The legacy `/api/v1/tenants/{tenant_id}/webauthn/*` mount (including
its credential-management endpoints) was deleted. Browser and
TypeScript-SDK login now uses the unified login ceremony at
`/api/v1/auth/login/{begin,finish}` (`type: fido2`), which mints the
OIDC bearer + refresh token; the xkms mount remains the ceremony
surface the Go SDK's `Security().Auth().FIDO2()` REST transport
drives.

### Features

- **Passkey Support**: Platform authenticators (Touch ID, Face ID, Windows Hello)
- **Hardware Keys**: USB, NFC, and BLE security keys
- **Multi-Credential**: Users can register multiple authenticators
- **Credential Management**: Rename, list, and delete credentials
- **Sign Count Tracking**: Clone detection for hardware keys
- **Attestation Options**: None, indirect, or direct attestation

### Quick Start

#### Configuration

Configure the relying-party in your QuicBaaS configuration:

```yaml
iam:
  webauthn:
    rp_id: "example.com"
    rp_display_name: "Example Application"
    rp_origins:
      - "https://example.com"
      - "https://app.example.com"
    timeout: "60s"
    user_verification: "preferred"        # required | preferred | discouraged
    attestation_preference: "none"        # none | indirect | direct
    resident_key_requirement: "preferred" # discouraged | preferred | required
    authenticator_attachment: ""          # platform | cross-platform | "" (any)
    debug: false
  aaguid:
    mode: "log_only"                      # log_only | enforce
    allowlist_file: "/etc/quicbaasd/aaguid.json"
```

The `iam.webauthn.*` keys are validated at daemon startup
(`config.IAMWebAuthnConfig.Validate`). Production deployments must
provide an explicit `rp_id`, `rp_display_name`, and at least one
`rp_origin`; the dev defaults (`localhost`, `QuicBaaS`,
`https://localhost`) are applied only when the operator omits the
field entirely.

#### Go SDK Usage

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/iam"
    qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Build the per-tenant WebAuthn factory. The qrdb client is required:
// each tenant's user + credential records are persisted via xkms's
// qrdb adapter pair under the platform tenant's KV namespace.
factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
    Config: iam.WebAuthnFactoryConfig{
        RPID:          "example.com",
        RPDisplayName: "Example Application",
        RPOrigins:     []string{"https://example.com"},
        Timeout:       60 * time.Second,
    },
    QRDB: qrdbClient,
})
if err != nil {
    return err
}

// Wrap the factory in the multi-tenant manager. The manager
// implements service.WebAuthnManager and is the surface the REST
// handlers consume.
manager, err := iam.NewXKMSWebAuthnManager(factory)
if err != nil {
    return err
}

// Optionally apply an AAGUID allowlist. The manager intercepts
// FinishRegistration and rejects credentials whose AAGUID is not
// permitted (in strict mode).
manager.SetAAGUIDAllowlist(allowlist)
```

### REST API Flow

#### Registration Flow

The registration flow creates a new WebAuthn credential for a user.

```
Client                    Server                    Authenticator
  |                          |                           |
  |  1. POST /register/begin |                           |
  |------------------------->|                           |
  |                          |                           |
  |  2. CredentialCreation   |                           |
  |<-------------------------|                           |
  |                          |                           |
  |  3. Create Credential    |                           |
  |-------------------------------------------------->   |
  |                          |                           |
  |  4. Attestation Response |                           |
  |<--------------------------------------------------|  |
  |                          |                           |
  |  5. POST /register/finish|                           |
  |------------------------->|                           |
  |                          |                           |
  |  6. Credential Stored    |                           |
  |<-------------------------|                           |
```

##### Step 1: Begin Registration

Initiate credential registration for an authenticated user. The
opaque ceremony session id is returned in the `X-Session-Id` response
header and must be echoed on finish.

```http
POST /api/v1/iam/{tenant_id}/webauthn/registration/begin
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "email": "alice@example.com",
  "display_name": "Alice Smith"
}
```

Response (`X-Session-Id: <session>` header + options body):
```json
{
  "publicKey": {
    "challenge": "dGhpcyBpcyBhIHJhbmRvbSBjaGFsbGVuZ2U",
    "rp": {
      "id": "example.com",
      "name": "Example Application"
    },
    "user": {
      "id": "dXNlcl9hYmMxMjM",
      "name": "alice@example.com",
      "displayName": "Alice Smith"
    },
    "pubKeyCredParams": [
      {"type": "public-key", "alg": -7},
      {"type": "public-key", "alg": -257}
    ],
    "timeout": 60000,
    "attestation": "none",
    "excludeCredentials": [
      {
        "type": "public-key",
        "id": "existing-credential-id"
      }
    ]
  }
}
```

##### Step 2: Complete Registration

Submit the authenticator's attestation response — the raw
`navigator.credentials.create()` PublicKeyCredential JSON is the
request body, correlated by the `X-Session-Id` header from begin.

```http
POST /api/v1/iam/{tenant_id}/webauthn/registration/finish
Authorization: Bearer <access_token>
X-Session-Id: <session>
Content-Type: application/json

{
  "id": "credential-id-base64url",
  "rawId": "credential-id-base64url",
  "type": "public-key",
  "response": {
    "clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIi...",
    "attestationObject": "o2NmbXRkbm9uZWdhdHRTdG10oGhh..."
  },
  "authenticatorAttachment": "platform"
}
```

**Credential Parsing**: The server parses the attestation response using the `go-webauthn/webauthn` library's `protocol.ParseCredentialCreationResponse` function, which handles:
- Base64url decoding of `id` and `rawId` fields
- Parsing of `clientDataJSON` to extract challenge, origin, and type
- CBOR decoding of `attestationObject` to extract authenticator data
- Public key extraction and COSE key parsing
- Attestation verification (when attestation is not "none")

Response (xkms auth response):
```json
{
  "token": "<post-registration token>",
  "user_id": "dXNlcl9hYmMxMjM"
}
```

#### Authentication Flow

The login flow authenticates a user with their WebAuthn credential.

```
Client                    Server                    Authenticator
  |                          |                           |
  |  1. POST /login/begin    |                           |
  |------------------------->|                           |
  |                          |                           |
  |  2. CredentialAssertion  |                           |
  |<-------------------------|                           |
  |                          |                           |
  |  3. Get Assertion        |                           |
  |-------------------------------------------------->   |
  |                          |                           |
  |  4. Signed Assertion     |                           |
  |<--------------------------------------------------|  |
  |                          |                           |
  |  5. POST /login/finish   |                           |
  |------------------------->|                           |
  |                          |                           |
  |  6. Session Token        |                           |
  |<-------------------------|                           |
```

##### Step 1: Begin Login

Initiate WebAuthn authentication for a user. This route is public —
WebAuthn IS the authentication mechanism. The ceremony session id is
returned in the `X-Session-Id` response header.

```http
POST /api/v1/iam/{tenant_id}/webauthn/login/begin
Content-Type: application/json

{
  "email": "alice@example.com"
}
```

Response (`X-Session-Id: <session>` header + options body):
```json
{
  "publicKey": {
    "challenge": "YW5vdGhlciByYW5kb20gY2hhbGxlbmdl",
    "timeout": 60000,
    "rpId": "example.com",
    "allowCredentials": [
      {
        "type": "public-key",
        "id": "credential-id-1",
        "transports": ["internal"]
      },
      {
        "type": "public-key",
        "id": "credential-id-2",
        "transports": ["usb", "nfc"]
      }
    ],
    "userVerification": "preferred"
  }
}
```

##### Step 2: Complete Login

Submit the authenticator's assertion response — the raw
`navigator.credentials.get()` PublicKeyCredential JSON is the request
body, correlated by the `X-Session-Id` header from begin.

```http
POST /api/v1/iam/{tenant_id}/webauthn/login/finish
X-Session-Id: <session>
Content-Type: application/json

{
  "id": "credential-id-base64url",
  "rawId": "credential-id-base64url",
  "type": "public-key",
  "response": {
    "clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0Ii...",
    "authenticatorData": "SZYN5YgOjGh0NBcPZHZgW4...",
    "signature": "MEUCIQCYrNzWB..."
  }
}
```

**Credential Parsing**: The server parses the assertion response using the `go-webauthn/webauthn` library's `protocol.ParseCredentialRequestResponse` function, which handles:
- Base64url decoding of credential IDs
- Challenge verification against stored session
- Signature verification using the stored public key
- Sign count validation for clone detection
- User verification flag checking

Response (xkms auth response):
```json
{
  "token": "<post-login token>",
  "user_id": "dXNlcl9hYmMxMjM"
}
```

The xkms mount's login token is not an OIDC bearer. Applications that
need the full OIDC access/refresh token pair (the SPA, the TypeScript
SDK, the CLI) log in through the unified ceremony instead:
`POST /api/v1/auth/login/begin` (`type: fido2`) issues the WebAuthn
challenge and `POST /api/v1/auth/login/finish` verifies the assertion
server-side and mints the bearer + rotating refresh token.

#### Credential Management

The legacy per-user credential CRUD endpoints
(`/api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials`)
were deleted with the legacy mount. Enrolled WebAuthn credentials are
managed as qrdb user auth factors via
`Security().Auth().Factors()` (list / remove / rotate / relabel) and
the `quicbaas auth factor` CLI.

### Frontend Integration

#### JavaScript/TypeScript

Using the Web Authentication API:

```javascript
// Registration (canonical xkms mount; session travels in X-Session-Id)
async function registerWebAuthn(email, displayName) {
  // 1. Get options from server
  const beginResponse = await fetch('/api/v1/iam/my-tenant/webauthn/registration/begin', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ email, display_name: displayName })
  });
  const sessionId = beginResponse.headers.get('X-Session-Id');
  const options = await beginResponse.json();

  // 2. Decode base64url fields for the browser API
  options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge);
  options.publicKey.user.id = base64urlToBuffer(options.publicKey.user.id);
  if (options.publicKey.excludeCredentials) {
    options.publicKey.excludeCredentials = options.publicKey.excludeCredentials.map(c => ({
      ...c,
      id: base64urlToBuffer(c.id)
    }));
  }

  // 3. Create credential with authenticator
  const credential = await navigator.credentials.create(options);

  // 4. Encode response for server
  const body = {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64url(credential.response.clientDataJSON),
      attestationObject: bufferToBase64url(credential.response.attestationObject)
    }
  };

  // 5. Send to server (raw PublicKeyCredential JSON, session header)
  const finishResponse = await fetch('/api/v1/iam/my-tenant/webauthn/registration/finish', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json',
      'X-Session-Id': sessionId
    },
    body: JSON.stringify(body)
  });

  return finishResponse.json();
}

// Authentication (ceremony-only; for an OIDC bearer use
// POST /api/v1/auth/login/{begin,finish} with type "fido2" instead)
async function loginWebAuthn(email) {
  // 1. Get options from server
  const beginResponse = await fetch('/api/v1/iam/my-tenant/webauthn/login/begin', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email })
  });
  const sessionId = beginResponse.headers.get('X-Session-Id');
  const options = await beginResponse.json();

  // 2. Decode base64url fields
  options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge);
  options.publicKey.allowCredentials = options.publicKey.allowCredentials.map(c => ({
    ...c,
    id: base64urlToBuffer(c.id)
  }));

  // 3. Get assertion from authenticator
  const assertion = await navigator.credentials.get(options);

  // 4. Encode response for server
  const body = {
    id: assertion.id,
    rawId: bufferToBase64url(assertion.rawId),
    type: assertion.type,
    response: {
      clientDataJSON: bufferToBase64url(assertion.response.clientDataJSON),
      authenticatorData: bufferToBase64url(assertion.response.authenticatorData),
      signature: bufferToBase64url(assertion.response.signature)
    }
  };

  // 5. Send to server (raw PublicKeyCredential JSON, session header)
  const finishResponse = await fetch('/api/v1/iam/my-tenant/webauthn/login/finish', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Session-Id': sessionId
    },
    body: JSON.stringify(body)
  });

  return finishResponse.json();
}

// Helper functions
function base64urlToBuffer(base64url) {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/');
  const padding = '='.repeat((4 - base64.length % 4) % 4);
  const binary = atob(base64 + padding);
  return Uint8Array.from(binary, c => c.charCodeAt(0));
}

function bufferToBase64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
```

#### React Integration

```jsx
import { useState } from 'react';

function WebAuthnManager() {
  const [credentials, setCredentials] = useState([]);
  const [loading, setLoading] = useState(false);

  const registerPasskey = async () => {
    setLoading(true);
    try {
      await registerWebAuthn();
      await loadCredentials();
    } catch (err) {
      console.error('Registration failed:', err);
    } finally {
      setLoading(false);
    }
  };

  const deleteCredential = async (factorId) => {
    // Enrolled passkeys are qrdb user auth factors; remove via the
    // auth-factor surface (see Security().Auth().Factors()).
    await fetch(`/v1/users/factors/${factorId}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${token}` }
    });
    await loadCredentials();
  };

  return (
    <div>
      <h2>Passkeys</h2>
      <button onClick={registerPasskey} disabled={loading}>
        {loading ? 'Registering...' : 'Add Passkey'}
      </button>

      <ul>
        {credentials.map(cred => (
          <li key={cred.id}>
            {cred.name}
            <span>Last used: {cred.last_used_at || 'Never'}</span>
            <button onClick={() => deleteCredential(cred.id)}>Remove</button>
          </li>
        ))}
      </ul>
    </div>
  );
}
```

### Credential Storage

Phase 5 moves credential persistence to xkms. quicbaas no longer
defines a `CredentialStorage` interface; the per-tenant xkms
WebAuthn service uses `xkms.NewQRDBUserStoreAdapter` to read and
write credentials through qrdb's KV client under the
`__xkms:webauthn/` keyspace prefix. Operators do not need to
configure a separate credential store — the platform's qrdb
deployment is the single source of truth.

The legacy storage interface documented in earlier revisions
(`iam.CredentialStorage`) has been removed; this section is kept
for historical reference only.

#### Storage Interface (legacy / removed)

The WebAuthn manager uses a credential storage interface:

```go
type CredentialStorage interface {
    // Credential operations
    CreateCredential(ctx context.Context, credential *WebAuthnCredential) error
    GetCredential(ctx context.Context, tenantID, credentialID string) (*WebAuthnCredential, error)
    GetCredentialsByUser(ctx context.Context, tenantID, userID string) ([]*WebAuthnCredential, error)
    UpdateCredential(ctx context.Context, credential *WebAuthnCredential) error
    DeleteCredential(ctx context.Context, tenantID, credentialID string) error

    // Challenge operations
    StoreChallenge(ctx context.Context, challenge *WebAuthnChallenge) error
    GetChallenge(ctx context.Context, challengeID string) (*WebAuthnChallenge, error)
    DeleteChallenge(ctx context.Context, challengeID string) error
}
```

#### Credential Structure

```go
type WebAuthnCredential struct {
    ID              string    // Base64url-encoded credential ID
    UserID          string    // Owner user ID
    TenantID        string    // Tenant scope
    Name            string    // User-friendly name
    PublicKey       []byte    // COSE-encoded public key
    AttestationType string    // none, indirect, direct
    AAGUID          []byte    // Authenticator GUID
    SignCount       uint32    // Signature counter
    Transports      []string  // usb, nfc, ble, internal
    CreatedAt       time.Time
    LastUsedAt      *time.Time
}
```

### Security Considerations

#### Challenge Handling

- Challenges are cryptographically random (32 bytes)
- Challenges have a configurable TTL (default 60 seconds)
- Challenges are deleted after use (success or failure)
- Challenge replay is prevented by server-side validation

#### Sign Count Verification

The sign count is tracked and verified to detect credential cloning:

```go
// During login, sign count is verified
if response.Authenticator.SignCount <= storedCredential.SignCount {
    // Potential clone detected
    // Log warning but may still allow if sign count is 0
}
```

#### Attestation

| Mode | Security | Use Case |
|------|----------|----------|
| `none` | Basic | Consumer applications, passkeys |
| `indirect` | Moderate | Enterprise with some verification |
| `direct` | High | High-security environments |

#### User Verification

| Mode | Description |
|------|-------------|
| `required` | Biometric/PIN always required |
| `preferred` | Use if available, fallback allowed |
| `discouraged` | Skip verification (not recommended) |

### Error Handling

#### Common Errors

| Error | HTTP Status | Description |
|-------|-------------|-------------|
| `challenge_expired` | 400 | Challenge TTL exceeded |
| `challenge_not_found` | 400 | Challenge not found or already used |
| `credential_not_found` | 404 | Credential does not exist |
| `credential_invalid` | 400 | Credential validation failed |
| `user_not_found` | 404 | User not found in tenant |
| `registration_failed` | 400 | Registration verification failed |
| `authentication_failed` | 401 | Authentication verification failed |

#### Error Response Format

```json
{
  "error": "challenge_expired",
  "error_description": "The WebAuthn challenge has expired. Please try again."
}
```

### Best Practices

1. **Always Use HTTPS**: WebAuthn requires secure contexts
2. **Match Origins**: RP origins must match exactly (including port for development)
3. **Handle Errors Gracefully**: Show user-friendly messages
4. **Provide Fallback**: Offer alternative authentication methods
5. **Encourage Multiple Credentials**: Users should register backup authenticators
6. **Clear Credential Names**: Help users identify their authenticators
7. **Monitor Sign Counts**: Alert on potential clone detection
8. **Regular Cleanup**: Remove expired challenges periodically

### See Also

- [API Reference](api.md) - Complete API documentation
- Authentication - General authentication guide
- [MFA](api.md#mfa-api) - TOTP multi-factor authentication


---

## GDPR Compliance

QuicBaaS IAM provides comprehensive GDPR compliance features for managing user consent, data export (Right to Access), and data deletion (Right to Erasure).

### Overview

The GDPR module (`pkg/iam/gdpr.go`) implements:

- **Consent Management**: Track and manage user consent for various data processing purposes
- **Data Export**: Allow users to request and download their personal data
- **Data Deletion**: Process deletion requests with configurable grace periods

### Consent Management

#### Consent Purposes

QuicBaaS supports the following consent purposes:

| Purpose | Description |
|---------|-------------|
| `marketing` | Marketing communications |
| `analytics` | Analytics and usage tracking |
| `personalization` | Personalized experiences |
| `third_party` | Sharing with third parties |
| `data_processing` | General data processing |
| `data_retention` | Extended data retention |
| `functional` | Functional features and cookies |

#### Consent Status

| Status | Description |
|--------|-------------|
| `granted` | Consent was given |
| `denied` | Consent was explicitly denied |
| `withdrawn` | Consent was previously granted but withdrawn |
| `pending` | Consent has not been given or denied |

#### API Operations

##### Grant Consent

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/consent \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "user_id": "user_123",
    "purpose": "marketing",
    "version": "1.0",
    "expires_at": "2026-01-01T00:00:00Z"
  }'

# CLI
quicbaas iam gdpr consent grant --tenant acme-corp \
  --user-id user_123 --purpose marketing --version 1.0
```

##### Withdraw Consent

```bash
# REST API
curl -X DELETE https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/consent \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"user_id": "user_123", "purpose": "marketing"}'

# CLI
quicbaas iam gdpr consent withdraw --tenant acme-corp \
  --user-id user_123 --purpose marketing
```

##### List User Consents

```bash
# REST API
curl https://quicbaas:8080/api/v1/tenants/{tenant_id}/users/{user_id}/consents \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr consent list --tenant acme-corp --user-id user_123
```

### Data Export (Right to Access)

Users can request an export of all their personal data stored in QuicBaaS.

#### Export Formats

| Format | Description |
|--------|-------------|
| `json` | JSON format (default) |
| `csv` | CSV format |
| `xml` | XML format |

#### Data Categories

Exports can include or exclude specific data categories:

| Category | Description |
|----------|-------------|
| `profile` | User profile data (email, name, attributes) |
| `sessions` | Session history |
| `consents` | Consent records |
| `groups` | Group memberships |
| `roles` | Role assignments |
| `audit_logs` | User's audit log entries |
| `credentials` | Credential metadata (not secrets) |
| `mfa` | MFA enrollment info |

#### Export Flow

1. **Request Export**: User initiates export request
2. **Processing**: System collects user data
3. **Completion**: Download link generated (valid for 7 days by default)
4. **Download**: User downloads their data

#### API Operations

##### Request Export

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/export \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "user_id": "user_123",
    "format": "json",
    "include_categories": ["profile", "sessions", "consents"]
  }'

# CLI
quicbaas iam gdpr export request --tenant acme-corp \
  --user-id user_123 --format json
```

##### Check Export Status

```bash
# REST API
curl https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/export/{export_id} \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr export status --tenant acme-corp --export-id exp_abc123
```

##### Generate Export

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/export/{export_id}/generate \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr export generate --tenant acme-corp --export-id exp_abc123
```

### Data Deletion (Right to Erasure)

Users can request deletion of their personal data with a configurable grace period.

#### Deletion Flow

1. **Request Deletion**: User initiates deletion request
2. **Grace Period**: Configurable waiting period (default: 30 days)
3. **Cancellation Window**: User can cancel during grace period
4. **Processing**: After grace period, deletion is executed
5. **Completion**: Summary of deleted data provided

#### Deletion Categories

Users can selectively delete or preserve specific data categories:

- **Delete Categories**: Specify which categories to delete (empty = all)
- **Preserve Categories**: Specify which categories to keep (for legal holds)

#### API Operations

##### Request Deletion

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/deletion \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "user_id": "user_123",
    "reason": "User requested account deletion",
    "grace_period_days": 30,
    "preserve_categories": ["audit_logs"]
  }'

# CLI
quicbaas iam gdpr deletion request --tenant acme-corp \
  --user-id user_123 --reason "Account closure" --grace-period 30
```

##### Cancel Deletion

```bash
# REST API
curl -X DELETE https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/deletion/{deletion_id} \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr deletion cancel --tenant acme-corp --deletion-id del_abc123
```

##### Check Deletion Status

```bash
# REST API
curl https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/deletion/{deletion_id} \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr deletion status --tenant acme-corp --deletion-id del_abc123
```

##### Process Deletion (Admin)

After grace period expires:

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/deletion/{deletion_id}/process \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr deletion process --tenant acme-corp --deletion-id del_abc123
```

#### Deletion Summary

After processing, a summary is provided:

```json
{
  "profile_deleted": true,
  "sessions_deleted": 5,
  "audit_logs_anonymized": 150,
  "consents_deleted": 3,
  "credentials_deleted": 2,
  "group_memberships_removed": 4,
  "role_assignments_removed": 2,
  "tokens_revoked": 3
}
```

### Configuration

#### Default Settings

| Setting | Default | Description |
|---------|---------|-------------|
| Grace Period | 30 days | Time before deletion is executed |
| Export Download TTL | 7 days | How long export download links are valid |
| Export Schema Version | 1.0 | Current export data schema version |

### Audit Integration

All GDPR operations are logged to the audit system:

| Event | Description |
|-------|-------------|
| `consent.granted` | Consent was granted |
| `consent.updated` | Consent was updated |
| `consent.withdrawn` | Consent was withdrawn |
| `data_export.requested` | Export request created |
| `data_export.completed` | Export generation completed |
| `data_deletion.requested` | Deletion request created |
| `data_deletion.cancelled` | Deletion request cancelled |
| `data_deletion.completed` | Deletion executed |

### Protocol Support

GDPR operations are available via all protocols:

| Protocol | Implementation |
|----------|---------------|
| REST API | `pkg/api/rest/iam_gdpr.go` |
| gRPC | `pkg/api/grpc/server_iam.go` |
| QUIC | `pkg/api/quic/handler_iam_gdpr.go` |
| MCP | `pkg/api/mcp/tools_iam_gdpr.go` |
| CLI | `cmd/quicbaas/iam_gdpr.go` |


---

## Authentication

QuicBaaS uses **passwordless, hardware-backed authentication exclusively**. Users authenticate via WebAuthn/FIDO2 or PIV smartcard. Service accounts use mTLS with bound OAuth2 tokens. See [Passwordless Authentication](architecture.md) for complete details.

> **Breaking Change (v1.1.0):** MCP token parameters (`_token`, `accessToken`) in request body are no longer supported. Use `Authorization: Bearer <token>` header. See [MCP Authorization](../oauth2-oidc/api.md#migration-guide).
>
> **Breaking Change (v2.0.0):** Password-based authentication is removed. User passwords are no longer supported. All users must enroll a hardware authenticator (WebAuthn passkey, security key, or PIV smartcard).

### Overview

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

### OAuth 2.1 (MCP Authorization)

MCP clients authenticate using OAuth 2.1 with mandatory PKCE. See [MCP Authorization](../oauth2-oidc/api.md) for complete flow.

#### PKCE Requirements

PKCE is **mandatory** for all authorization code flows. Only S256 challenge method is supported.

```python
import secrets, hashlib, base64

# Generate code verifier
code_verifier = secrets.token_urlsafe(32)

# Generate code challenge (S256)
digest = hashlib.sha256(code_verifier.encode()).digest()
code_challenge = base64.urlsafe_b64encode(digest).rstrip(b'=').decode()
```

#### Resource Indicators (RFC 8707)

Access tokens are bound to specific resource servers:

```bash
# Include resource in authorization request
GET /oauth2/authorize?...&resource=https://quicbaas.example.com/mcp

# Include in token request
POST /oauth2/token
...&resource=https://quicbaas.example.com/mcp
```

The token's `aud` claim must match the MCP server's resource URI.

#### Discovery Endpoints

| Endpoint | RFC | Description |
|----------|-----|-------------|
| `/.well-known/oauth-authorization-server` | RFC 8414 | OAuth AS metadata |
| `/.well-known/oauth-protected-resource` | RFC 9728 | MCP resource metadata |
| `/.well-known/openid-configuration` | OIDC | OIDC discovery |
| `/.well-known/jwks.json` | - | Public keys |

#### Dynamic Client Registration (RFC 7591 / RFC 7592)

Registering a client mints an authentication principal, so the surface is
**disabled by default** and requires an initial access token when enabled. See
`iam.dynamic_client_registration` in IAM configuration.

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

##### Client configuration endpoint (RFC 7592)

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

### External IdP Federation

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

### JWT Signing

JWT signing is asymmetric only. There is no JWT key material in the configuration file — the former `auth.jwt_secret`, `auth.jwt_signing_key_id`, and `auth.jwt_signing_algorithm` fields were removed, and shared-secret (HS256) tokens are rejected outright to eliminate algorithm-confusion attacks.

#### xkms-Backed Signing Key (Automatic)

The IAM/OIDC subsystem signs tokens with an ES256 key held in the configured xkms backend under the common name `oidc-signing`. On first boot the key is generated automatically inside the backend selected by the BackendResolver (TPM 2.0, PKCS#11, or software keystore) — see `pkg/iam/oidc_keymanager.go` (`NewSigningKeyManagerFromXKMS`). The private key never leaves the keystore; signing is delegated to the keystore's `crypto.Signer`, and each token carries a `kid` header matching the published JWKS.

Rotation generates a fresh ES256 key in the xkms backend and demotes the previous key into a JWKS overlap window, so in-flight tokens remain verifiable until they expire.

#### Per-Tenant Signing Keys

Dedicated tenants can opt in to their own signing key via tenant OIDC settings. `TenantSigningKeyResolver` (`pkg/iam/oidc_tenant_signing.go`) maps each tenant to its key manager, and the aggregated JWKS publishes the global key plus every tenant key. Per-tenant IAM data (users, policies, sessions, WebAuthn credentials, OIDC settings) is barrier-isolated via the tenant-routed KV store.

#### Internal Auth Manager (pkg/auth)

The internal `pkg/auth` manager (embedded and test paths) is asymmetric-only as well: ES256 by default, with the keystore-backed signer supplied via `SetAsymmetricSigning` when `JWTSigningKeyID` is set. HMAC tokens fail validation unconditionally.

### Direct JWT Authentication

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

**Note:** User creation with passwords is deprecated. Users are created in `pending` status and must activate by enrolling a hardware authenticator. See [User Flow](architecture.md#user-flow-invite--activate--login).

### Storage Modes

| Storage Backend | Auth Mode | Persistence | Cluster-Safe |
|----------------|-----------|-------------|--------------|
| memory.MemoryStorage | In-memory | No | No |
| pebble.Storage | Persistent | Yes | Yes |
| Embedded | Persistent | Yes | Yes |
| Cluster | Persistent | Yes | Yes |

### Internal Key Schema

All authentication data uses reserved `__quicbaas:` prefix.

#### User Keys
```
__quicbaas:users:{userID}                 -> User JSON
__quicbaas:users:byname:{username}        -> userID lookup
```

#### Role Keys
```
__quicbaas:roles:{roleName}               -> Role JSON
```

#### Token Keys
```
__quicbaas:tokens:refresh:{userID}:{hash} -> Expiry timestamp
__quicbaas:tokens:revoked:{hash}          -> Revocation timestamp
```

### Built-in Roles

| Role | Resources | Actions | Description |
|------|-----------|---------|-------------|
| admin | * | * | Full administrative access |
| readwrite | keys, indexes, transactions | read, write, delete | Application access |
| readonly | keys, indexes, metrics, health | read | Read-only monitoring |

### Usage (Passwordless)

#### Create Users (Pending Activation)

```go
user, err := authManager.CreateUser("alice@example.com", "", []string{"readwrite"})
// User is created in pending status, no password
// User must activate by enrolling a WebAuthn credential
```

#### Obtain Token After User Enrolls Authenticator

Tokens are issued after the user completes WebAuthn/smartcard authentication:

```go
// Via OAuth2 client credentials (service account)
token, err := authManager.ClientCredentials(ctx, clientID)
// token.AccessToken (1 hour TTL, bound to mTLS cert via cnf.x5t#S256)
// token.RefreshToken (7 days TTL)

// Or use a token obtained from browser WebAuthn login
```

#### Authorize

```go
err := authManager.Authorize(ctx, token.AccessToken, "keys", "write")
```

#### Refresh Tokens

```go
newToken, err := authManager.RefreshToken(ctx, token.RefreshToken)
```

#### Custom Roles

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

### Permission Model

#### Resources
- keys, indexes, cluster, users, backups, metrics, health, transactions, watch, * (wildcard)

#### Actions
- read, write, delete, admin, * (wildcard)

#### Matching
Wildcards in roles match all resources/actions. Actions are checked by exact match or wildcard.

### Security Features

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

### Best Practices

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
11. See [Passwordless Authentication](architecture.md) for detailed architecture

### See Also

- [MCP Authorization](../oauth2-oidc/api.md) - OAuth 2.1 for MCP
- Authorization - RBAC/ABAC
- IAM Configuration - Identity management
- Security Overview
- KMS
- Secrets
