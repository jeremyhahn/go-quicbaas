# QuicBaaS IAM Developer Guide

How to integrate your applications with QuicBaaS IAM for SSO and ABAC authorization.

## Overview

QuicBaaS IAM provides:
- **SSO (Single Sign-On)**: One login works across all your applications
- **ABAC (Attribute-Based Access Control)**: Fine-grained authorization
- **PEP (Policy Enforcement Point)**: Local policy evaluation (~2-5μs)

## Integration Flow

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

## Step 1: Register Your Application

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

## Step 2: Configure Your App

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

## Step 3: Backend Integration (Go)

### Initialize IAM Client

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

### Check Permissions in Handlers

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

### Batch Permission Checks

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

## Step 4: Frontend Integration (React)

### Setup Provider

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

### Login Button

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

### Protected Routes

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

### API Calls with JWT

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

## Step 5: Define ABAC Policies

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

## JWT Token Structure

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

## SSO Experience

When a user logs in to one app, they're automatically logged into all apps:

1. User logs into Order App
2. User visits Billing App
3. Billing App redirects to QuicBaaS IAM
4. IAM detects existing session (no login prompt!)
5. IAM issues JWT for Billing App
6. User is instantly authenticated

## Integration Summary

| Step | Time | Description |
|------|------|-------------|
| 1. Register app | ~1 min | CLI command |
| 2. Add config | ~5 min | YAML configuration |
| 3. Add SDK | ~1 min | Package dependency |
| 4. Add middleware | ~10 min | Auth + ABAC middleware |
| 5. Define policies | ~30 min | YAML policies |
| **Total** | **<1 hour** | Basic integration |

## What You DON'T Have to Build

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

## SDK Reference

### Go SDK

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

### JavaScript/TypeScript SDK

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

### Python SDK

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

## MCP Client Integration

AI assistants and MCP clients authenticate using OAuth 2.1 with mandatory PKCE. This section covers how to build MCP clients that integrate with QuicBaaS IAM.

### Discovery Flow

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

### Step 1: Discover Authorization Server

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

### Step 2: Implement PKCE (Mandatory)

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

### Step 3: Authorization Request

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

### Step 4: Token Exchange

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

### Step 5: Make MCP Requests

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

### Machine-to-Machine (Client Credentials)

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

### Dynamic Client Registration

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

### Token Refresh

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

### Error Handling

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

### Complete MCP Client Example

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

### MCP Scopes

| Scope | Description |
|-------|-------------|
| `openid` | OpenID Connect identity |
| `mcp:tools` | Access to MCP tools |
| `mcp:resources` | Access to MCP resources |
| `mcp:admin` | Administrative operations |
| `offline_access` | Refresh token access |

### See Also

- [MCP Authorization](../api/mcp-authorization.md) - Complete OAuth 2.1 reference
- [MCP API](../api/mcp.md) - MCP protocol documentation
- [IAM Configuration](configuration.md) - Server configuration
