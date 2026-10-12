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

  # WebAuthn / FIDO2 relying party. This is the ONE WebAuthn block; see
  # "Enrollment assurance" below for the full reference. QuicBaaS is
  # passwordless — there is no password fallback.
  webauthn:
    rp_id: "example.com"
    rp_display_name: "Example Corp"
    rp_origins:
      - "https://example.com"
      - "https://app.example.com"
    user_verification: preferred
    attestation_preference: direct
    attestation_roots_file: "/etc/quicbaasd/fido-attestation-roots.pem"
    assurance: hardware

  # PIV smartcard login. See "PIV smartcard attestation" below.
  piv_login:
    enabled: false
    ca_file: "/etc/quicbaasd/piv-ca.pem"
    attestation_ca_file: "/etc/quicbaasd/yubico-piv-attestation-ca.pem"

  auth:
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
  "groups": ["Developers", "Team Leads"],   // names of the bound roles
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

#### `POST /api/v1/iam/refresh` — renewal (public)

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
clients and use `/api/v1/iam/refresh`.

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
- **Attestation Options**: None, indirect, direct, or enterprise attestation

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
    attestation_preference: "direct"      # none | indirect | direct | enterprise
    attestation_roots_file: "/etc/quicbaasd/fido-attestation-roots.pem"
    assurance: "hardware"                 # hardware (default) | any
    resident_key_requirement: "preferred" # discouraged | preferred | required
    authenticator_attachment: ""          # platform | cross-platform | "" (any)
    debug: false
  aaguid:
    mode: "enforce"                       # enforce (default) | log_only
    allowlist_file: "/etc/quicbaasd/aaguid.json"
```

#### Enrollment assurance — what a credential must prove

`iam.webauthn.assurance` is the minimum attestation level a **non-administrative**
registration must prove.

| Value | Meaning |
|-------|---------|
| `hardware` (default, and the value an empty setting selects) | The attestation statement must chain to a manufacturer batch-attestation key (`basic_full`, `attca` or `anonca`), and the reported AAGUID must be in the `iam.aaguid` allowlist. |
| `any` | Any authenticator is accepted, including one conveying no attestation. The level that was actually proven is still recorded on the stored factor, so the credential is visibly software-backed. |
| `insecure-allow-software-administrators` | As `any`, and additionally drops the administrator bar. **Automated test environments only.** |

**`any` never applies to an administrative account.** An account holding `Admin`
or `SO` is always required to prove hardware, so loosening the tenant policy
cannot loosen the control that guards the platform. A registrant the server
cannot identify — including one whose account does not exist yet, which is the
case during self-service registration — is held to the administrator bar.

`insecure-allow-software-administrators` is the one policy that lets a software
authenticator become an administrator credential. It exists because our own
integration suite drives go-xkms's headless CTAP2 software adapter (which emits
`fmt: "none"`) and the browser E2E probes drive a CDP virtual authenticator, and
both enroll administrators. Under any other policy every such probe is refused —
correctly, because a software authenticator is not hardware. If a probe starts
failing after this change, that is the control working; do not weaken the policy
to make it pass.

It cannot be reached by accident: it is not the zero value, an unrecognised
value is refused rather than defaulted, and there is no shorter alias — the only
spelling that selects it is the one that says what it does. A node running it
logs a `SECURITY:` warning on every boot, and the credentials it admits still
record the level they actually proved, so nothing is ever *represented* as
hardware that is not.

**Conveyance is raised automatically.** Any deployment where a registration
might have to prove hardware — which includes `assurance: any`, because
administrators are held to hardware under it — has `attestation_preference`
raised to `direct` at service-construction time. With the default `none`
conveyance the browser strips the attestation statement before the relying party
sees it, the authenticator reports no AAGUID, and a hardware requirement could
only ever refuse — which is what made the AAGUID allowlist vacuous even when an
operator configured it strictly. An operator who configured `enterprise` keeps
it; nothing is ever lowered.

#### The attestation trust path — why an attestation *type* is not enough

`iam.webauthn.attestation_roots_file` is a PEM bundle of FIDO attestation ROOT
certificates: the manufacturer roots that sign authenticator batch attestation
certificates. **It is required for hardware enrollment.**

go-webauthn reports `Credential.AttestationType` as `basic_full` after running
the packed format's verification procedure — but that procedure checks only that
the statement is internally consistent. The library says so in its own source:

```go
// Step 2.4 If successful, return attestation type Basic and attestation trust path x5c.
// We don't handle trust paths yet but we're done.
```

and `VerifyAttestation` returns before any metadata validation whenever no MDS
provider is supplied, which is every ceremony this platform runs.

So `basic_full` means *somebody signed this with a certificate*, not *a
manufacturer signed this*. An attacker generates their own attestation
certificate, puts an allowlisted AAGUID in both its `id-fido-gen-ce-aaguid`
extension and the authenticator data, signs the statement with it, and satisfies
the attestation type **and** the AAGUID allowlist using material they chose.

Hardware therefore additionally requires an x5c chain that builds a path to a
configured root, with the certificate's AAGUID extension bound to the
authenticator data. The chain lives in the attestation object, which the
credential store never sees, so it is verified at the three surfaces that can
finish a registration — `XKMSWebAuthnManager.FinishRegistration`, the
tenant-scoped route that dispatches into xkms, and the multi-protocol
`FIDO2Service` — and the result travels to the gate on the request context. **A
path that does not verify cannot produce a hardware credential**, because the
gate's default answer is "not proven".

With no roots configured, hardware enrollment is refused with a message naming
the setting, and the node logs a warning at startup.

#### AAGUID enforcement — an absent gate denies

`iam.aaguid.mode` defaults to `enforce`. Only the explicit `log_only` opt-out
yields a permissive gate; every other value, including empty and a typo, is
strict.

An **unconfigured** deployment gets a strict, empty allowlist — not "no
allowlist". A missing gate refuses with a message naming
`iam.aaguid.allowlist_file`, rather than admitting every authenticator on the
planet. Likewise, an `allowlist_file` that cannot be read or parsed is a fatal
startup error: an operator who configured an allowlist and got a node enforcing
nothing is precisely the failure this refuses.

`allowlist_file` accepts two shapes:

```jsonc
// a curated array
[{"aaguid": "cb69481e8ff7403993ec0a2729a154a8", "name": "YubiKey 5 Series"}]

// or a decoded FIDO Metadata Service v3 document
{"legalHeader": "...", "no": 76, "entries": [ { "aaguid": "...", ... } ]}
```

From an MDS3 document exactly three fields are read: `entries[].aaguid`,
`entries[].metadataStatement.description`, and the certification history in
`entries[].statusReports`. Everything else in the blob is ignored, and ignoring
it changes no decision — the allowlist answers only "is this authenticator
*model* admitted?", while attestation trust paths are verified separately
against `attestation_roots_file`.

The status history is read because the blob contains authenticators the
Alliance has withdrawn. Entries whose history includes `REVOKED`,
`ATTESTATION_KEY_COMPROMISE`, `USER_KEY_REMOTE_COMPROMISE`,
`USER_KEY_PHYSICAL_COMPROMISE` or `USER_VERIFICATION_BYPASS` are **skipped**, so
importing a blob wholesale cannot allowlist a compromised model.
`NOT_FIDO_CERTIFIED` is not disqualifying — it means uncertified, not
compromised. Entries with no AAGUID (the U2F and UAF models) are skipped.

The signed JWS is not verified here: this reads a file the operator placed on
the node's disk, so the trust boundary is the filesystem, exactly as it is for
`ca_file` and `attestation_roots_file`.

#### PIV smartcard attestation

```yaml
iam:
  piv_login:
    enabled: true
    ca_file: "/etc/quicbaasd/piv-ca.pem"
    attestation_ca_file: "/etc/quicbaasd/yubico-piv-attestation-ca.pem"
    attestation_intermediates_file: ""   # optional vendor sub-CA bundle
    allow_unattested: false              # explicit downgrade; default false
```

A PIV card proves that a slot's private key was generated on-card by signing a
per-slot attestation certificate with a factory-provisioned attestation key
(slot F9 on a YubiKey), whose own certificate chains to the vendor's PIV
attestation root:

```
slot attestation certificate  ->  device F9 intermediate  ->  vendor root
```

Only the last link is a trust anchor an operator configures; the rest travels
with the card and is supplied as chain material at login. Material presented by
the party being verified is **never** promoted to a trust anchor.

**The login certificate is never its own evidence.** A slot certificate is
normally issued by the *organisation's* CA from a CSR, while the attestation
certificate is a separate artifact (`ykman piv keys attest <slot>`). Accepting
the login certificate when it happens to chain to the attestation anchor looks
like it accommodates the simpler deployment, but the anchor is operator
configuration: point `attestation_ca_file` at the CA that also issues ordinary
client certificates and every software-held key chains to it. Nothing about a
certificate's *path* distinguishes a key generated on a card from one generated
with `openssl`.

Nor is "some other certificate" enough. Under that same reused CA a holder
submits their CSR twice and gets two certificates for one key — distinct,
chaining, about the right key, and still saying nothing about where the key
lives. A negative rule ("not the login certificate") is defeated by producing
another one.

The rule is therefore a **positive** one. A certificate presented alongside the
login credential establishes hardware only when all of the following hold:

1. it is **not** the login certificate;
2. its SubjectPublicKeyInfo is byte-identical to the login certificate's, so it
   speaks about the key that is actually authenticating (without this binding a
   genuine attestation borrowed from any other YubiKey would launder a software
   key);
3. it carries a **PIV slot attestation profile** marker — an extension from
   Yubico's attestation arc `1.3.6.1.4.1.41482.3` (firmware version, device
   serial, PIN/touch policy, form factor), which an ordinary client-auth
   certificate has no reason to carry;

and then it chains to a configured attestation anchor. Only the *presence* of
the profile extension is read, never its values: deciding storage does not
require decoding a firmware version, and decoding it would make this a vendor
attestation parser, which belongs in go-xkms with the rest of the hardware
layer.

A deployment whose cards cannot produce a slot attestation is not silently
downgraded — it is refused, and the operator chooses honestly with
`allow_unattested`.

`trusted_issuers` is a Subject-DN **name** comparison and is not evidence of
anything — any certificate can assert any issuer name. Hardware is established
by `attestation_ca_file` alone.

With no `attestation_ca_file` configured, PIV login is **refused**. Setting
`allow_unattested: true` lets the login proceed, but the issued token records
`attest_level: software` and does **not** carry the hardware-attested ACR, so a
policy requiring hardware still refuses it. The downgrade is explicit and
visible, never silent.

`allow_unattested` covers the deployment that *decided* not to verify. It does
**not** rescue a deployment that meant to and got the configuration wrong: an
`attestation_ca_file` that is named but cannot be read or parsed is refused
either way, because silently downgrading there would turn a typo into a
fleet-wide loss of hardware assurance.

**There is one PIV login path, not two.** go-xkms's composed module serves its
own PIV login at `/api/v1/auth/login/piv`, and it cannot verify a smartcard
attestation — its options carry only `Enabled` and `TrustedIssuers`, and a
`TrustedIssuers` entry is a DN string any certificate can assert. Mounting it
beside the hardened handler would make the attestation requirement bypassable by
choosing the other URL, so it is mounted **only** when `allow_unattested` is
set. The module-side fix is filed as go-xkms#200. A deployment that requires attested PIV gets exactly one PIV path,
`/api/v1/iam/login/piv`; a node that declines to mount the module surface says
so at startup.

#### Attestation claims are derived, never asserted

`attest_level` and `attest_aaguid` are computed from verified evidence inside
`pkg/iam` and cannot be supplied by a handler. `HardwareTokenOptions.Attestation`
carries an `*iam.AttestationResult` whose fields are unexported, so the only way
to obtain one reporting `hardware` is to have run a verifier that checked a
certificate chain. `IssueHardwareTokens` refuses `ACRHardwareAttested` unless
the attestation actually proves hardware.

The same rule applies one layer down, on **every** surface that writes an auth
factor without running a ceremony: the SDK's `Add` and `Rotate`, and the qrdb
auth-factor servicer proxy that backs the gRPC `UserAuthFactorService` and the
MCP `qrdb_user_authfactor_*` tools. Each **refuses** a caller-supplied
`attest_level` or `attest_aaguid` rather than writing it, and stamps every
factor it creates as `attest_level: none`. The rule itself is one function —
`iam.ValidateAuthFactorMetadata` — so the next transport cannot quietly reopen
the hole. A factor row can only claim hardware if the enrollment ceremony put it
there.

#### Known residual exposure: qrdb node-owned auth-factor listeners

The controls above cover every QuicBaaS-owned write path to the auth-factor
store. They do **not** cover go-qrdb's own REST/gRPC/MCP auth-factor handlers,
which QuicBaaS enables on the embedded node when `NetworkServing()` is true
(`pkg/server/qrdb_node.go`). Those handlers belong to go-qrdb, construct the
node's own user-factor service, and pass caller-supplied metadata straight into
`Add`/`Rotate` — so a caller authorized on those listeners can still write
`attest_level=hardware` without any ceremony.

Fixing it belongs upstream: filed as go-qrdb#234 rather than worked around here. Until it lands, treat authorization for `user_authfactors:add` on
the node-owned listeners as equivalent to the ability to mint an administrator
credential, and do not expose those listeners to callers you would not trust
with one.

The trust-path proof travels on the request context, and
`ContextWithAttestationChain` is deliberately **not exported**: an exported
setter would let an embedded, in-process caller stamp a chain of its own
choosing and have the gate treat it as this server's verification. The only way
to place the value is to run the verification. Every entry point also *clears*
any pre-existing proof on each early exit and on failure
(`iam.ClearAttestationChain`) — otherwise a caller could skip verification by
sending an unparseable attestation and keep a planted proof.

The `iam.webauthn.*` keys are validated in two places, because there are two
different values to check.

`config.IAMWebAuthnConfig.Validate` runs in the boot-time configuration gate
(`config.Validate`, called by `cmd/quicbaasd` before the server starts) and
checks the **operator's input for shape only**. Every relying-party identity
field is optional there: `rp_id`, `rp_display_name` and `rp_origins` may all be
omitted, and a node that omits them derives its relying party from the
canonical issuer URL (see the derivation note in
[architecture.md](architecture.md#configuration)). What this gate refuses is a
supplied value that can never work — an `rp_id` the relying party itself would
refuse at construction (`protocol.ValidateRPID`: a bare host with no scheme,
port or path, and a dotted domain rather than a bare single label, with
`localhost` and IP literals explicitly allowed), an entry in `rp_origins` that
is not a usable origin, or a `timeout` that is not a positive Go duration.

The strictness bar is deliberately the embedded relying party's own behaviour,
never a stricter idea of a well-formed value, because a validator stricter than
the thing it guards refuses configurations that work. Concretely: an `rp_id`
that is an IP literal is **accepted** (browsers reject it, but
`protocol.ValidateRPID` does not, and an explicit `rp_id` is the documented
escape hatch for a node whose issuer host is an IP address); and in
`rp_origins`, a URL entry carrying a path, query or fragment is accepted
because the origin comparison ignores them, while any entry that is not an
`http`/`https` URL is accepted because the relying party matches it verbatim —
that is how Android native-application origins work.

`config.IAMWebAuthnConfig.ValidateResolved` runs on the WebAuthn init path
(`webauthnFactoryConfigFromOperator`) against the **resolved** configuration,
after derivation has filled in whatever the operator omitted. That is the value
the relying party is actually built from, so an RP ID, a display name and at
least one origin are genuinely required there. When derivation cannot supply a
missing half — an IP-literal issuer host with only one of `rp_id` /
`rp_origins` configured — startup fails closed rather than mounting a relying
party that rejects every ceremony.

Production deployments should still pin `rp_id`, `rp_display_name` and
`rp_origins` explicitly: derivation follows `server.host` and the REST port, so
a deployment behind a proxy or a different public hostname must state its own
relying-party identity.

**Per-tenant overrides.** A tenant may override `rp_id`, `rp_display_name` and
`rp_origins` through `settings.webauthn` on its tenant record; an empty field
inherits the global value. The override is held to the same RP ID and origin
rules as the global relying party — the rules live in `pkg/webauthnrp`, which
both `pkg/config` and `pkg/iam` call, so the two cannot drift:

- **At write time.** `DefaultTenantManager.CreateTenant` and `UpdateTenant`,
  the one write path every transport reaches, refuse a malformed override
  before storing anything. The error is an `ErrInvalidInput`-coded `QBError`
  wrapping `*iam.TenantWebAuthnOverrideError`, whose `Field` names
  `settings.webauthn.rp_id` or `settings.webauthn.rp_origins`; REST returns
  400 and gRPC `InvalidArgument`.
- **At build time.** `WebAuthnServiceFactory` validates the merged
  global-plus-override value before it builds a tenant's relying party, and
  fails closed with `*iam.WebAuthnTenantRPError` (no service is built or
  cached). This catches an override stored before write-time validation
  existed; correcting the tenant's settings clears it.

Only REST (`PUT /api/v1/tenants/{tenant_id}`) and MCP carry
`settings.webauthn` today; the gRPC `TenantSettings` message and the Go SDK's
tenant request have no WebAuthn fields.

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

```

The enrollment assurance policy and the AAGUID allowlist are handed to the
**factory**, not to the manager:

```go
factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
    Config:          webauthnConfig,
    QRDB:            qrdbClient,
    Assurance:        assurance,       // iam.ParseWebAuthnAssurance(cfg.Assurance)
    AAGUIDAllowlist:  aaguidAllowlist, // never nil: an absent gate denies
    Admins:           iam.NewWebAuthnAdminResolver(userStorage),
    AttestationRoots: attestationRoots, // nil refuses hardware, never trusts blindly
})
```

They are applied inside the per-tenant credential store's write-through
(`webauthn_factor_writethrough.go`), which is the single point every completed
registration converges on — xkms's `Service.FinishRegistration` persists through
`CredentialStore.Save` whichever entry point drove the ceremony.

This placement is load-bearing. The check previously lived on
`XKMSWebAuthnManager.FinishRegistration`, and the **live** registration route
(`POST /api/v1/iam/{tenant_id}/webauthn/registration/finish`, served by
`moduleWebAuthnHandler`) dispatches straight into xkms and never calls that
method — so the allowlist was absent from the primary enrollment path while
appearing, from the manager's code, to be enforced. It also ran *before* the
ceremony verified the attestation statement, where the AAGUID is a value the
browser supplied rather than one the authenticator proved.

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

Two surfaces administer a stored authenticator, for two different questions.

**As a login factor** — list / remove / rotate / relabel the qrdb user auth
factor a passkey resolves through: `Security().Auth().Factors()` and the
`quicbaas auth factor` CLI.

**As a stored credential** — see and remove what the WebAuthn credential
store actually holds for a user, which is what an operator needs when a
credential outlives the account it was registered for and its email becomes
permanently unregistrable (the authenticator refuses the ceremony because
the credential is listed in `excludeCredentials`):

| Verb | Route |
| --- | --- |
| List | `GET /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/` |
| Delete one | `DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/{credential_id}` |
| Delete all | `DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/` |

The same three verbs are served over gRPC, MCP and the Unix socket, and are
exposed by the CLI as `quicbaas iam webauthn list` and
`quicbaas iam webauthn delete --credential <id> | --all`. Every transport
resolves through
`service.WebAuthnCredentialService`, which owns the one part that must not
vary per transport: translating the IAM user id the caller was authorized
against into the `(tenant, email)` pair the credential store is addressed by.

**Addressing is not ownership.** These verbs take an email because the
ceremony does, but xkms's qrdb-backed store files a credential under the qrdb
**user id** — `__xkms:webauthn/users/<id>/credentials`, with the blob under the
credential id and the qrdb fido2 login factor under the same `uint64`. Email
appears in none of those keys: the lookup enters the qrdb user record through
the email index and takes that record's id.

The consequence is that **renaming a user keeps their passkeys**. qrdb moves
the email index with the record, so the new address resolves the same
credentials and the old address resolves to nothing — there is no orphan for
the rename to leave behind. `IdentityStore.UpdateUser` used to revoke them on
every email change on the opposite belief (true of xkms's standalone
email-keyed store, never of the adapter this product wires), which cost a
burn-in user their only passkey and returned `200`; that revocation is gone,
and an email change now logs the credential tally on both sides of the write —
`webauthn credentials carried to the new address` at INFO, or a discrepancy at
ERROR (go-quicbaas#582). A credential still outlives a **deleted** user, which
is what the `DeleteUser` cascade and these operator verbs are for.

`{credential_id}` is the **complete, unpadded base64url** id the list verb
publishes — the same value the browser reports as `rawId`. Every character of
that alphabet is URL-unreserved, so the id is passed verbatim in the path and
is never percent-encoded.

Pass the whole id. A base64url id has no fixed length, so a fragment of one
whose length is a multiple of four decodes cleanly into bytes that match
nothing; the server detects that case and answers `400` naming the full id
(`WebAuthnCredentialTruncatedIDError`) rather than a `404` that would read
like "already deleted". The three id outcomes are:

| Supplied id | REST | gRPC | Meaning |
| --- | --- | --- | --- |
| Not base64url | `400` | `InvalidArgument` | typo — `WebAuthnCredentialIDError` |
| A prefix of a registered id | `400` | `InvalidArgument` | truncated — `WebAuthnCredentialTruncatedIDError`, names the full id |
| Well-formed, owned by nobody | `404` | `NotFound` | `WebAuthnCredentialNotFoundError` |

Deleting one credential verifies it belongs to the named user before removing
anything. That check is load-bearing rather than defensive: the credential
blobs live in a single namespace keyed by credential id alone, so a delete
dispatched straight to the store would let a caller authorized over one user
destroy any passkey in the deployment by naming its id.

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
| `enterprise` | Highest | Managed fleets whose authenticators are permitted to return a uniquely identifying attestation |

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

### Diagnosing a rejected login

Every rejection of `POST /api/v1/auth/login/{begin,finish}` returns the same
`401 unauthorized / "authentication failed"`, whatever went wrong. That is
deliberate: a differentiated error would let an unauthenticated caller probe
which tenants and which accounts exist. The consequence is that **the client
response carries no diagnostic information at all** and the server-side record
is the only account of what happened.

#### The rejection record

`service.LoginService` reports every refused ceremony — both halves, every
transport — to a `LoginCeremonyAuditor`. The server wires one at the same
point it builds the login facade, so a login-capable node can never be in a
state where a ceremony is refused and nothing records why.

Two records are written per rejection:

1. **A structured `WARN` log, always**, at `msg="login ceremony rejected"`:

   ```json
   {
     "level": "WARN", "msg": "login ceremony rejected",
     "stage": "finish", "factor_type": "fido2",
     "session_id": "6892026b-...", "tenant_id": "", "subject": "",
     "factor_id": "",
     "lookup_key_sha256": "54c2a720bc2566df...", "lookup_key_len": 43,
     "method_metadata_keys": ["credential_id", "session_id"],
     "reason": "transport: factor not found: "
   }
   ```

2. **An AU-2/AU-3 authentication event** with `outcome=failure` under action
   `iam.login.begin` / `iam.login.finish` — the same action names the success
   path records, so one audit query returns both outcomes.

Reading the fields:

| Field | What it tells you |
|---|---|
| `reason` | The typed server-side error, verbatim. This is the diagnosis. |
| `factor_id` | Non-empty **only** when the factor resolved and the proof was refused. Empty means the credential resolved to no enrolled factor. |
| `lookup_key_sha256` / `lookup_key_len` | The presented lookup key, reduced. A length of 43 is the base64url credential-id string; 32 would be the raw credential bytes, i.e. a client encoding bug. |
| `method_metadata_keys` | The key names the client sent. A missing required key is a contract mismatch. |
| `subject` | The caller's hint on `begin`; on `finish` it is known only when the proof already passed. |

Never recorded: the signature, the assertion, or any method-metadata **value**.

**Tenant limitation.** qrdb's `FinishLoginRequest` carries no tenant id, so a
`finish` rejection before the principal resolves has no tenant to file an audit
event under. The auditor does not invent one — it skips the audit event and the
`WARN` log, which carries every field, is the record.

#### The FIDO2 proof wire contract

Three fields, and all three producers emit exactly this shape — the TypeScript
SDK (`sdk/typescript/src/rest.ts` `webAuthnFinishLogin`), the Go CLI
(`cmd/quicbaas` `proveCanonicalFIDO2`), and any other client:

| Field | Value |
|---|---|
| `lookup_key` | The **base64url credential-id string**, as bytes (so standard-base64 on the JSON wire). Byte-identical to what enrollment stores as the factor's `Credential`. Not the decoded credential bytes. |
| `signature` | The `navigator.credentials.get()` assertion **document** — the JSON go-webauthn's `ParseCredentialRequestResponseBytes` accepts. The verifier reads the assertion here when `method_metadata["assertion"]` is absent. |
| `method_metadata` | `session_id` = the **relying-party** session id from `begin`'s `challenge_metadata` (not the ceremony `session_id`), and `credential_id` = the same base64url string, unencoded. |

The contract is pinned on both sides:
`sdk/typescript/test/webauthn_login.test.ts` and
`pkg/api/rest/iam_login_ceremony_webauthn_contract_test.go`, the latter parsing
the forwarded proof with the same go-webauthn call the verifier makes.

#### Where the verification key comes from

**The public key an assertion is verified against always comes from the stored
enrollment, never from the request.** The path is
`tenantWebAuthnVerifier.VerifyTenant` → xkms `WebAuthnFactorVerifier.Verify` →
`webauthn.Service.FinishLogin` → go-webauthn `ValidateLogin`, which matches the
assertion's credential id against the credentials the user store returns and
verifies with the key recorded at registration.

A `cose_public_key` entry in `method_metadata` has **no effect in either
direction** — an assertion signed by an unenrolled key is refused with or
without it, and a bogus one does not disturb a legitimate login. This is
enforced by differential tests in
`pkg/iam/webauthn_login_key_provenance_test.go`, tied to the live verifier by
`pkg/server/login_factor_verifier_fido2_provenance_test.go`.

(The `cose_public_key` metadata key belongs to a **different** verifier —
go-xkms `pkg/factor/fido2`, the custodian factor plugin registered through
qrdb's `FactorRegistry`. That plugin is not on the interactive login path;
`WithLoginFactorVerifier` is.)

#### Known cause: a credential enrolled without a login factor

`reason: "transport: factor not found"` on a `finish` whose `begin` returned the
credential in `allowCredentials` means the WebAuthn credential exists but the
qrdb `fido2` auth factor the login resolver indexes does not.

That happens when the credential was registered through the **xkms module's**
SDK ceremony (`POST /api/v1/webauthn/sdk/registration/{begin,finish}`, which
`xkey auth register` drives) rather than a quicbaas surface. The xkms module
builds its own relying-party service over the same qrdb-backed credential store
but without quicbaas's write-through decorator
(`pkg/iam/webauthn_factor_writethrough.go`), so the credential is persisted and
the login factor is not. `xkey auth login` still succeeds, because it validates
against the credential store directly and never consults qrdb's factor index.

Registrations through `POST /api/v1/auth/register/{begin,finish}` and
`/api/v1/iam/{tenant_id}/webauthn/registration/*` go through the decorator and
enroll the factor.

### Best Practices

1. **Always Use HTTPS**: WebAuthn requires secure contexts
2. **Match Origins**: a URL origin is compared by scheme and host only —
   case-insensitively, with the default scheme port normalised, and path, query
   and fragment ignored — so a non-default port MUST be listed (it is part of
   the host), while a trailing slash is harmless. An entry that is not an
   `http`/`https` URL is matched verbatim, which is how native-application
   origins work.
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

#### There Is No Internal Auth Manager

`pkg/auth` holds the request-scoped Subject plumbing and nothing else. It has no
user type, no user store, and no token manager: identity is qrdb's, reached
through `iam.IdentityStore`, and session tokens are minted and validated by
`iam.OIDCProvider`. The `InMemoryAuthManager` / `PersistentAuthManager` pair
that used to live there defined a second `User` struct and `UserStore` — a
second identifier space — was never wired in a running daemon, and was deleted
in go-quicbaas#905 together with the `__quicbaas:users:` / `__quicbaas:roles:`
/ `__quicbaas:tokens:` key schema it owned.

### Direct JWT Authentication

An API client validates a session bearer through the provider that minted it:

```go
claims, err := oidcProvider.ValidateAccessToken(ctx, accessToken)
if err != nil {
    return err
}
log.Printf("User: %s", claims.Subject)   // a qrdb user ID
log.Printf("Roles: %v", claims.Roles)
```

Through the service layer the same three session verbs are
`QuicBaaS.ValidateToken`, `RefreshToken` and `Logout`
(`pkg/service/quicbaas.go`'s `authService`), each routed to the OIDC provider.
Over the SDK they are `Security().Auth()`; over MCP, `quicbaas_logout` and the
`quicbaas_iam_session_*` tools.

Signing is asymmetric only: ES256 by default, key material in the xkms
keystore. HMAC tokens fail validation unconditionally.

**Note:** There is no password login. `authService.Login` always fails. Users
are created in `pending` status and activate by enrolling a hardware
authenticator — see [User Flow](architecture.md#user-flow-invite--activate--login).

### Built-in Roles

| Role | Resources | Actions | Description |
|------|-----------|---------|-------------|
| admin | * | * | Full administrative access |
| readwrite | keys, indexes, transactions | read, write, delete | Application access |
| readonly | keys, indexes, metrics, health | read | Read-only monitoring |

### Usage (Passwordless)

#### Create a User (Pending Activation)

```go
// IdentityStore writes through to the embedded qrdb node; the record it returns
// carries qrdb's Snowflake ID. There is no password parameter.
user, err := identityStore.CreateUser(ctx, tenantID, iam.CreateUserOptions{
    Email: "alice@example.com",
    Roles: []string{"readwrite"},
})
// The user is pending until a WebAuthn credential or PIV certificate is enrolled.
```

Over the SDK this is `Identity().IAM().User().Create`; over MCP,
`quicbaas_iam_user_create`; over REST, `POST /api/v1/iam/users`.

#### Obtain a Token After the User Enrolls an Authenticator

Tokens are issued once the user completes a WebAuthn / PIV / mTLS login
ceremony, or through an OAuth2 grant — both by `iam.OIDCProvider`. The access
token is bound to the caller's mTLS certificate via `cnf.x5t#S256`.

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

#### Resolving a Role by Name

`RoleStorage.GetRoleByName(ctx, tenantID, name)` is the single supported way to
answer "which role is named N in tenant T". It is a LOOKUP, and callers must not
answer the question with a listing:

```go
role, err := identityStore.GetRoleByName(ctx, "acme", "admin")
if iam.IsRoleNotFound(err) {
    // the tenant holds no role of that name
}
```

The reason is that `ListOptions{Limit: 0}` does NOT mean "all rows" on every
store. `QRDBRoleStorage` maps it to qrdb's `PageSize: 0`, which qrdb treats as
its default page of 100 — so a list-and-filter over a tenant holding more than
100 roles could fail to find a role that exists (go-quicbaas#541).

Each store answers it without scanning a page:

| Store | Mechanism |
| --- | --- |
| `QRDBRoleStorage` | delegates to qrdb's own name index (`RoleClient.GetRoleByName`) |
| `BackendStorage` | a tenant-scoped KV index, `__iam:tenant:<id>:idx:rolename:<name>` -> role ID |
| `MemoryStorage` | scans its role map (the double's volume is small; nothing to drift) |

The index segment deliberately sits outside the record prefix
(`:idx:rolename:`, not under `:role:`) so `ListRoles`, which scans
`__iam:tenant:<id>:role:`, never sweeps up an index entry. `BackendStorage`
keeps the index consistent on create, on a rename through `UpdateRole` (new
entry written, old one dropped), on delete, and on the tenant cascade — and it
refuses a create or rename onto a name the tenant already holds, because an
index maps one name to one role and both sibling stores enforce the same
uniqueness.

An absent role is reported as the `ErrRoleNotFound` sentinel; a store failure is
NOT. The distinction is load-bearing at the consumer: qrdb's
`ResolveSystemRoleID` falls back to a DERIVED identifier on not-found, so an
outage reported as an absence would bind a grant to an identifier the tenant may
not use.

#### First-Boot Role Resolution

`qrdbadapter.RoleAdapter` implements qrdb's `role.NameResolver` on top of that
lookup and is wired into first boot as
`FirstBootDependencies.RoleResolver` (`pkg/server/iam_init.go`,
`firstBootDependencies`).

First boot is idempotent, so a second run over an already-seeded system tenant
finds the built-in roles present rather than creating them, and must then bind
the platform admin's grant to the identifier those roles are ACTUALLY persisted
under. A cluster seeded before system role IDs moved off UUIDv5/SHA-1 keeps the
legacy identifiers forever, so a derived identifier would name no role and the
grant would silently confer nothing. With nothing able to resolve them, qrdb
refuses to guess and fails with `UnresolvableSystemRoleError`.

The adapter translates the broker's `ErrRoleNotFound` into qrdb's own
`*role.RoleNotFoundError`, because `ResolveSystemRoleID` matches that exact type
with `errors.As` to take the derived-identifier path and treats every other
error as a failure to resolve.

#### Built-in Role Reconciliation

A tenant's built-in roles are copied from the build's definitions when the
tenant is created, and qrdb refuses every later write to a system role
(`SystemRoleImmutableError`, surfaced here as `iam.ErrSystemRoleImmutable`). So
when a build renames or adds a permission, a pre-existing tenant keeps the old
grant and no privilege level can repair it by hand. Reconciliation is the
exemption qrdb makes for the one caller whose target is not arbitrary — it IS
`BuiltInRoles()`.

```go
// pkg/iam: the capability is a narrow interface, not part of RoleStorage.
// Built-in definitions are qrdb's, so only the qrdb-backed storage can honour
// the contract; anything else is refused with ErrRoleReconcileUnsupported
// rather than reconciled against a set quicbaas invented.
type BuiltInRoleReconciler interface {
    ReconcileBuiltInRoles(ctx context.Context, tenantID string) (*RoleReconcileReport, error)
}

report, err := identityStore.ReconcileBuiltInRoles(ctx, "acme")
// report.Changed() is false on a converged tenant, and nothing was written.
```

Two ordering facts govern the wiring:

- **qrdb resolves the built-in definitions through the process-global
  permission registry at CALL time.** `Server.reconcileTenantBuiltInRoles`
  (`pkg/server/role_reconcile.go`) therefore runs at the tail of IAM
  initialization, after both of this process's registration sites —
  `pkg/permission.Register()` from `NewServer`, and the module's
  `registerRoleGrants()` from `apimodule.NewModule` during embedded-node
  construction. Reconciling earlier would converge every tenant onto a
  definition missing the module's own verbs and report success.
- **Idempotency is a store-level guarantee, not best-effort.** A converged
  tenant performs zero writes, which is what makes a per-tenant sweep on every
  daemon start safe. One log line is emitted per tenant per CHANGED role;
  an unchanged fleet is silent.

The sweep covers every tenant the broker lists plus the system tenant, which
the broker's listing does not carry — the same composition `abac_sync.go`
performs for the policy sweep. The control plane's own built-in roles drift for
exactly the same reason a data-plane tenant's do.

`Role.RetiredPermissions` is the read-side half: qrdb derives it from the live
registry on every role read and never stores it, so an operator sees the
disagreement in an ordinary listing before running the repair.

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
