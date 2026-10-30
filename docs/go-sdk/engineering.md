# Go SDK — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/go-sdk/`.

This document covers all configuration options available for the QuicBaaS Go SDK client.

## Creating a Client

```go
import "github.com/jeremyhahn/go-quicbaas/sdk/go"

client, err := quicbaas.NewClient(address, opts...)
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

## Address Formats

The SDK supports multiple address formats for connecting to QuicBaaS servers:

| Format | Example | Description |
|--------|---------|-------------|
| TCP | `localhost:50051` | Standard TCP connection |
| Unix socket | `/var/run/quicbaas.sock` | Auto-detected file path |
| Unix explicit | `unix:///var/run/quicbaas.sock` | Explicit Unix socket prefix |
| Unix abstract | `unix-abstract://quicbaas` | Linux abstract namespace socket |
| HTTP | `http://localhost:8080` | REST transport (auto-selected) |
| HTTPS | `https://localhost:8443` | REST transport with TLS |

When using `http://` or `https://` URLs, the SDK automatically selects REST transport.

## Transport Options

The SDK supports six transport protocols:

| Transport | Option | Description |
|-----------|--------|-------------|
| gRPC | (default) | High-performance binary protocol |
| REST | `WithREST()` | HTTP-based REST API |
| QUIC | `WithQUIC()` | UDP-based low-latency protocol |
| MCP | `WithMCP()` | Model Context Protocol (JSON-RPC 2.0 over HTTP) |
| Embedded | `WithEmbedded(svc)` | In-process direct service access, no network overhead |
| Unix Socket | (auto-detected) | gRPC over Unix Domain Socket |

```go
// Use REST transport
client, err := quicbaas.NewClient("localhost:8080", quicbaas.WithREST())

// Use QUIC transport
client, err := quicbaas.NewClient("localhost:50051", quicbaas.WithQUIC())

// Use MCP transport
client, err := quicbaas.NewClient("http://localhost:8080", quicbaas.WithMCP())

// Use Embedded transport
client, err := quicbaas.NewClient("", quicbaas.WithEmbedded(svc))
```

## MCP Transport Configuration

MCP (Model Context Protocol) uses JSON-RPC 2.0 over HTTP for communication. It is designed for AI agent and LLM tool integration, mapping QuicBaaS operations to MCP tool calls.

### Basic Usage

```go
client, err := quicbaas.NewClient("http://localhost:8080",
    quicbaas.WithMCP(),
)
```

### Authentication

MCP supports two authentication methods:

**Pre-existing token** -- recommended when a token has already been obtained (e.g., from browser WebAuthn login, CLI session, or OAuth2 client credentials flow):

```go
client, err := quicbaas.NewClient("http://localhost:8080",
    quicbaas.WithMCP(),
    quicbaas.WithMCPAuthToken(token),
)
```

**Auto-authentication via bearer token** (legacy) -- the transport reuses a stored token:

```go
client, err := quicbaas.NewClient("http://localhost:8080",
    quicbaas.WithMCP(),
    quicbaas.WithMCPAuthToken(storedToken),
)
```

**Note:** The `WithMCPCredentials(user, pass)` option is deprecated. QuicBaaS is passwordless-first; obtain tokens via WebAuthn, smartcard, or OAuth2 client credentials instead. See Passwordless Authentication.

### MCP Options

| Option | Description |
|--------|-------------|
| `WithMCP()` | Select MCP transport |
| `WithMCPAuthToken(token)` | Use a pre-existing Bearer token from OAuth2 / WebAuthn |

Public tools (`quicbaas_health`, `quicbaas_liveness`, `quicbaas_readiness`) do not require authentication.

### Limitations

- MCP does not support bidirectional streaming (Watch, Query streams)
- Slightly higher latency than gRPC due to JSON-RPC serialization overhead

## Embedded Transport Configuration

The Embedded transport provides in-process direct service access with zero network overhead. Requests are dispatched directly to the `service.QuicBaaS` implementation using O(1) map-based handler lookup.

### Basic Usage

```go
import (
    "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// svc is a service.QuicBaaS instance running in the same process
client, err := quicbaas.NewClient("", quicbaas.WithEmbedded(svc))
```

### Characteristics

| Property | Detail |
|----------|--------|
| Latency | Zero network overhead, direct function calls |
| Streaming | Supported (Watch, Query, TimeSeries streams) |
| TLS | Not applicable (in-process) |
| Connection pooling | Not applicable (in-process) |
| Use case | Testing, embedded applications, serverless functions |

The address parameter is ignored when using `WithEmbedded()`. Pass an empty string or any placeholder.

### IAM coverage

The embedded dispatch table answers the IAM services an in-process host needs,
including the ones a hosting product depends on most:

| Service | Methods |
|---------|---------|
| `IAM.Tenant` | Create, Get, Update, Delete, List |
| `IAM.User` | Create, Get, Update, Delete, List |
| `IAM.Group` | Create, Get, Update, Delete, List |
| `IAM.Role` | Create, Get, Update, Delete, List |
| `IAM.Policy` | Create, Get, List, Delete |
| `IAM.App` | Register, Get, List, Delete |
| `IAM.Session` | Get, List, Revoke, RevokeAll, RevokeOthers (`RevokeOther` accepted) |
| `IAM.Admin` | CreateAssignment, GetAssignment, UpdateAssignment, DeleteAssignment, ListAssignments, CheckPermission, StartImpersonation, EndImpersonation, ListActiveImpersonations (`GetActiveImpersonations` accepted) |
| `IAM.PEP` | Check |

`IAM.Session` and `IAM.Admin` dispatch against the same
`service.SessionManager` and `*iam.AdminManager` the REST and gRPC handlers
use, so every authorization and audit rule **those managers** enforce applies
identically in-process — an impersonation the admin manager would refuse over
gRPC is refused here too.

Transport-level gating is a different question. A network transport's
middleware authenticates the caller and puts a `Subject` on the context; the
embedded transport has no middleware, because its caller is the host process
itself. Two consequences:

- **`IAM.Session` is self-scoped whenever a `Subject` is present.** The
  handlers apply the REST ownership rule exactly (see
  `pkg/api/rest/iam_sessions.go`): naming another user's session, or a
  user/tenant that disagrees with the subject, returns a typed
  `*embedded.PermissionDeniedError`. So putting the SDK behind an
  authenticated surface — propagate the subject with
  `auth.SubjectIntoContext` — does not turn the embedded transport into a way
  to read or revoke a third party's sessions.
- **`IAM.Admin` evaluates the route-equivalent permission when a `Subject` is
  present.** Delegated administration is permission-gated over REST by the
  frozen route table (`pkg/api/rest/routes_permissions_generated.go`), e.g.
  `roles:assign` for `POST /api/v1/admin/assignments`. A subject-bound embedded
  call evaluates the same `resource:action` against the policy engine before
  touching the manager, and **fails closed** when no engine is wired.
  Those grants are generic — `users:update` says the caller may update users,
  not that a particular record is theirs — so the handlers additionally bind
  the caller-supplied identifiers that decide *whose* records are touched. An
  omitted value is filled from the subject; a mismatched one returns
  `*embedded.PermissionDeniedError`:

  | Operation | Bound to the subject | Bound for a tenant-scoped subject only |
  |-----------|----------------------|----------------------------------------|
  | `CreateAssignment` | `GrantedBy` (the audit actor) | the granted scope: omitted becomes the subject's tenant instead of a global grant, another tenant is refused |
  | `ListAssignments` | — | the tenant filter |
  | `CheckPermission` | — | the tenant the answer is computed in (`AdminID` stays free: naming the admin being inspected is the point) |
  | `StartImpersonation` | `AdminUserID` | `TenantID` |
  | `EndImpersonation` | — | the session must belong to the subject, in its tenant |
  | `ListActiveImpersonations` | — | the admin id, and results are narrowed to the tenant |

  The right-hand column is skipped for a **system-tenant** subject
  (`qrdbsdk.IsSystemTenant`, which covers the empty tenant id): the system
  scope is the platform itself, not a tenant named `system`, so pinning it to
  its own "tenant" would make cross-tenant platform administration impossible.

  `GetAssignment` / `UpdateAssignment` / `DeleteAssignment` are addressed by
  assignment id and are gated on `roles:read` / `roles:assign` /
  `roles:unassign` alone. An assignment's `TenantIDs` are its grant *scope* —
  which tenants the admin may administer — not an ownership marker on the
  record, so no per-record tenant check is inferred from them (the REST
  handlers do not infer one either).
- **A context with no `Subject` is unrestricted**, which is the pre-existing
  contract for every embedded IAM handler (tenant, user, group, role, policy
  all behave this way). The host process is the trust boundary. Do not expose
  the embedded transport directly to an untrusted caller.

A host that has not wired the corresponding manager gets a typed
`*embedded.ServiceNotConfiguredError` naming the service, never a nil-pointer
panic.

`IAM.Session/Touch` is deliberately absent: no transport carries it, because
`service.SessionManager` exposes no activity-update operation. Calling it
yields "unknown method", which matches gRPC's `Unimplemented`.

`IAM.Audit` and `IAM.GDPR` are not yet in the embedded table; use the gRPC,
Unix, or REST transport for those.

## TLS Configuration

### Using Certificate Files

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile:   "/path/to/client.crt",
        KeyFile:    "/path/to/client.key",
        CAFile:     "/path/to/ca.crt",
        ServerName: "quicbaas.example.com", // Optional
    }),
)
```

### TLSConfig Fields

| Field | Type | Description |
|-------|------|-------------|
| `CertFile` | string | Path to client certificate (PEM) |
| `KeyFile` | string | Path to client private key (PEM) |
| `CAFile` | string | Path to CA certificate for server verification |
| `ServerName` | string | Override server name for certificate verification |
| `InsecureSkipVerify` | bool | Disable server certificate verification (not recommended) |

### Using Custom tls.Config

```go
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    RootCAs:      caCertPool,
    MinVersion:   tls.VersionTLS13,
}

client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLSConfig(tlsConfig),
)
```

### Insecure Mode (Development Only)

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithInsecure(),
)
```

## Timeout Configuration

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTimeout(60*time.Second),
)
```

| Option | Default | Description |
|--------|---------|-------------|
| `WithTimeout(d)` | 30s | Request timeout duration |

## Keepalive Configuration

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithKeepalive(10*time.Second, 5*time.Second),
)
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| time | 10s | Interval between keepalive pings |
| timeout | 5s | Timeout waiting for ping response |

## Retry Configuration

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithRetry(5, 200*time.Millisecond),
    quicbaas.WithMaxRetryBackoff(10*time.Second),
)
```

| Option | Default | Description |
|--------|---------|-------------|
| `WithRetry(max, backoff)` | 3 retries, 100ms backoff | Maximum retries and initial backoff |
| `WithMaxRetryBackoff(d)` | 5s | Maximum backoff duration (exponential) |

## Connection Pooling (gRPC Only)

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithConnectionPool(5, 50),
    quicbaas.WithPoolMaxAge(2*time.Hour),
    quicbaas.WithPoolHealthInterval(1*time.Minute),
)
```

| Option | Default | Description |
|--------|---------|-------------|
| `WithConnectionPool(min, max)` | Disabled (5, 50) | Enable pooling with min/max connections |
| `WithPoolMaxAge(d)` | 1h | Maximum age before connection recycling |
| `WithPoolHealthInterval(d)` | 30s | Health check interval for pooled connections |

## Default Values Summary

| Setting | Default Value |
|---------|---------------|
| Transport | gRPC |
| Timeout | 30 seconds |
| Keepalive Time | 10 seconds |
| Keepalive Timeout | 5 seconds |
| Max Retries | 3 |
| Retry Backoff | 100 milliseconds |
| Max Retry Backoff | 5 seconds |
| TLS | Disabled |
| Connection Pool | Disabled |
| Pool Min Size | 5 |
| Pool Max Size | 50 |
| Pool Max Age | 1 hour |
| Pool Health Interval | 30 seconds |

## Complete Example

```go
package main

import (
    "log"
    "time"

    "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    // Production configuration with mTLS and connection pooling
    client, err := quicbaas.NewClient("quicbaas.example.com:50051",
        // TLS configuration
        quicbaas.WithTLS(quicbaas.TLSConfig{
            CertFile:   "/etc/quicbaas/client.crt",
            KeyFile:    "/etc/quicbaas/client.key",
            CAFile:     "/etc/quicbaas/ca.crt",
            ServerName: "quicbaas.example.com",
        }),

        // Timeout and keepalive
        quicbaas.WithTimeout(30*time.Second),
        quicbaas.WithKeepalive(10*time.Second, 5*time.Second),

        // Retry configuration
        quicbaas.WithRetry(3, 100*time.Millisecond),
        quicbaas.WithMaxRetryBackoff(5*time.Second),

        // Connection pooling
        quicbaas.WithConnectionPool(10, 100),
        quicbaas.WithPoolMaxAge(time.Hour),
        quicbaas.WithPoolHealthInterval(30*time.Second),
    )
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()

    // Use the client...
}
```

## Error Handling

All option functions validate their inputs and return typed errors:

```go
// Invalid timeout (negative value)
_, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTimeout(-1*time.Second),
)
// Returns: InvalidInputError

// Invalid pool configuration
_, err = quicbaas.NewClient("localhost:50051",
    quicbaas.WithConnectionPool(100, 50), // maxSize < minSize
)
// Returns: InvalidInputError

// Missing TLS certificate
_, err = quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "/nonexistent/cert.pem",
        KeyFile:  "/nonexistent/key.pem",
    }),
)
// Returns: ConfigurationError
```

### Matching an error from go-qrdb: use `errors.As`, not `errors.Is`

`errors.Is(err, someQrdbSentinel)` does **not** compare against that sentinel.
go-qrdb's error type implements `Is` by comparing **codes**: if the target is
also a qrdb error, the answer is `e.Code == target.Code`. Codes are coarse
(`ErrNotFound`, `ErrInvalidInput`, `ErrConflict`), so every qrdb sentinel
sharing a code matches every other one — a check written for "invite not found"
also fires on "user not found" and "tenant not found".

Nothing about that is visible at the call site. It compiles, it reads correctly,
and it over-matches at runtime, so a handler recovers from a condition it never
meant to handle.

```go
// WRONG — matches every qrdb error carrying the same code as the sentinel,
// not just the condition the sentinel names.
if errors.Is(err, someCodedSentinel) { ... }

// RIGHT — one concrete typed error per condition you branch on.
var target *SomeSpecificError
if errors.As(err, &target) { ... }
```

The rules:

- Branch on a **concrete typed error** with `errors.As`. Where a code really is
  the distinguishing fact, read it *after* the type assertion.
- Never introduce a sentinel of a coded type expecting `errors.Is` to separate
  it from its siblings. A sentinel of a coded type is a code with a name.
- Wrapping with `%w` is unaffected; `errors.As` walks the chain normally.

## Option Reference

| Option | Parameters | Description |
|--------|------------|-------------|
| `WithTimeout` | `time.Duration` | Set request timeout |
| `WithKeepalive` | `time, timeout time.Duration` | Configure keepalive |
| `WithRetry` | `maxRetries int, backoff time.Duration` | Configure retry behavior |
| `WithMaxRetryBackoff` | `time.Duration` | Set max exponential backoff |
| `WithTLS` | `TLSConfig` | Configure mTLS from files |
| `WithTLSConfig` | `*tls.Config` | Use custom tls.Config |
| `WithInsecure` | (none) | Disable TLS |
| `WithConnectionPool` | `minSize, maxSize int` | Enable connection pooling |
| `WithPoolMaxAge` | `time.Duration` | Set pool connection max age |
| `WithPoolHealthInterval` | `time.Duration` | Set pool health check interval |
| `WithREST` | (none) | Use REST transport |
| `WithQUIC` | (none) | Use QUIC transport |
| `WithMCP` | (none) | Use MCP transport |
| `WithMCPCredentials` | `username, password string` | Set MCP auto-authentication credentials |
| `WithMCPAuthToken` | `token string` | Set pre-existing MCP Bearer token |
| `WithEmbedded` | `svc interface{}` | Use embedded in-process transport |


---

## Authentication

QuicBaaS SDK authentication is **passwordless-first**, using hardware authenticators (WebAuthn, smartcards) for users and mTLS with bound access tokens for service accounts. See Passwordless Authentication for the authentication model.

### Overview

QuicBaaS provides comprehensive authentication options for secure client-server communication.

| Method | Identity Type | Status | Description |
|--------|---|--------|-------------|
| WebAuthn (Passkey) | Human user | Production | FIDO2 hardware authenticator (browser-based) |
| PIV Smartcard | Human user | Production | Smartcard with PIN (browser-based) |
| mTLS + Bound Token | Service account | Production | TLS client certificate + access token (RFC 8705) |
| OIDC Federation | Human user | Production | External IdP (Google, Okta, Azure AD) |
| OAuth2 Client Credentials | Service account | Production | Machine-to-machine with client_secret (RFC 6749 §2.3.1) |
| JWT | API-direct | Production | Pre-issued JSON Web Token |
| Session Management | Both | Production | Multi-device session control |
| MFA | Human user | Production | Time-based OTP, smartcard PIN as second factor |

### WebAuthn / FIDO2 Authentication (Browser-Based)

For human users, use WebAuthn to obtain JWT tokens. See WebAuthn Integration for browser examples.

The SDK then uses the JWT token for API calls (see [Using Access Tokens](#using-access-tokens) below).

### mTLS + Bound Access Token (Service Accounts)

Service accounts (non-human identities) authenticate via mTLS with a bound token.

#### 1. Register the Service Account

First, enroll the mTLS certificate:

```bash
quicbaas oauth2 client register \
  --client-name "my-app" \
  --auth-method self_signed_tls_client_auth \
  --cert-pem /path/to/client.crt \
  --key-pem /path/to/client.key
```

Returns `client_id` and a `cnf.x5t#S256` (certificate thumbprint).

#### 2. Obtain Access Token via Client Credentials

```go
package main

import (
    "context"
    "crypto/tls"
    "log"

    "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    // Load client certificate
    cert, err := tls.LoadX509KeyPair("/path/to/client.crt", "/path/to/client.key")
    if err != nil {
        log.Fatal(err)
    }

    // Create client with mTLS
    tlsConfig := &tls.Config{
        Certificates: []tls.Certificate{cert},
        ServerName:   "quicbaas.example.com",
    }

    client, err := quicbaas.NewClientWithTLS("quicbaas.example.com:50051", tlsConfig)
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Request access token (client_credentials flow)
    resp, err := client.Auth().ClientCredentials(context.Background(), &quicbaas.ClientCredentialsRequest{
        ClientID: "svc_abc123xyz",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Token is bound to the mTLS certificate via cnf.x5t#S256
    log.Printf("Access Token: %s", resp.AccessToken)
    log.Printf("Expires At: %s", resp.ExpiresAt)
}
```

All subsequent API calls over this mTLS connection use the `Authorization: Bearer <access_token>` header. The server verifies that the token's `cnf.x5t#S256` matches the client certificate presented on the TLS connection.

#### Token Refresh

```go
// Refresh token before it expires
refreshResp, err := client.Auth().RefreshToken(ctx, resp.RefreshToken)
if err != nil {
    log.Fatal(err)
}
log.Printf("New Access Token: %s", refreshResp.AccessToken)
```

#### Token Validation

```go
// Validate a token and get claims
claims, err := client.Auth().ValidateToken(ctx, accessToken)
if err != nil {
    log.Fatal(err)
}

if claims.Valid {
    log.Printf("User: %s", claims.Username)
    log.Printf("Roles: %v", claims.Roles)
    log.Printf("Permissions: %v", claims.Permissions)
}
```

#### Logout

```go
// Logout and invalidate tokens
err := client.Auth().Logout(ctx, accessToken)
if err != nil {
    log.Fatal(err)
}
```

### Multi-Factor Authentication (MFA)

When MFA is enabled, login may require additional verification.

#### Supported MFA Types

| Type | Description |
|------|-------------|
| TOTP | Time-based One-Time Password (Google Authenticator, Authy) |
| SMS | SMS-based verification codes |
| Email | Email-based verification codes |
| BackupCode | One-time backup recovery codes |

> **Note:** WebAuthn/FIDO2 authentication is handled by browser-based clients. The Go SDK can use JWT tokens obtained from WebAuthn-authenticated browser sessions.

### Using Access Tokens

Once you have an access token (from WebAuthn, smartcard, or client credentials), use it for API calls:

#### Direct Token Usage

```go
// Create client
client, err := quicbaas.NewClient("quicbaas.example.com:50051")
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// Set access token from earlier authentication
ctx := context.Background()
ctx = client.WithToken(ctx, "eyJhbGciOiJSUzI1NiIs...")

// Use the context for API calls
result, err := client.Get(ctx, "mykey")
if err != nil {
    log.Fatal(err)
}
```

#### Token Refresh

```go
// Refresh token before it expires
refreshResp, err := client.Auth().RefreshToken(ctx, refreshToken)
if err != nil {
    log.Fatal(err)
}
log.Printf("New Access Token: %s", refreshResp.AccessToken)
```

#### Token Validation

```go
// Validate a token and get claims
claims, err := client.Auth().ValidateToken(ctx, accessToken)
if err != nil {
    log.Fatal(err)
}

if claims.Valid {
    log.Printf("User: %s", claims.Subject)
    log.Printf("Roles: %v", claims.Roles)
    log.Printf("Auth Method: %v", claims.AMR) // ["webauthn","hwk"] or ["mtls","hwk"]
    log.Printf("Attestation: %s", claims.AttestationMethod)
}
```

#### Logout

```go
// Logout and invalidate tokens
err := client.Auth().Logout(ctx, accessToken)
if err != nil {
    log.Fatal(err)
}
```

### Multi-Factor Authentication (MFA)

WebAuthn devices (FIDO2 / U2F) provide **hardware-backed MFA** built-in. Platform authenticators (Touch ID, Windows Hello) and security keys prevent phishing.

When SmartCard (PIV) is enabled, the PIN acts as the second factor.

For APIs that require additional MFA (e.g., sensitive operations), an access token with `amr: ["webauthn", "hwk", "mfa"]` or `amr: ["smartcard", "mfa"]` is required.

| Type | Implementation |
|------|---|
| TOTP | Time-based One-Time Password (optional, for non-hardware MFA) |
| Smartcard PIN | PIV smartcard PIN as second factor |
| WebAuthn Device | FIDO2 device inherently provides MFA (user verification) |

### OIDC/OAuth2 Federated Authentication

Integrate with external identity providers like Google, Okta, Azure AD, and Keycloak.

#### List Available Providers

```go
providers, err := client.Auth().ListProviders(ctx, tenantID)
if err != nil {
    log.Fatal(err)
}

for _, p := range providers.Providers {
    log.Printf("Provider: %s (%s) - Enabled: %v", p.Name, p.Type, p.Enabled)
}
```

#### Get Provider Details

```go
provider, err := client.Auth().GetProvider(ctx, tenantID, "google")
if err != nil {
    log.Fatal(err)
}

log.Printf("Issuer URL: %s", provider.IssuerURL)
log.Printf("Auth URL: %s", provider.AuthURL)
log.Printf("Scopes: %v", provider.Scopes)
```

#### OIDC Login Flow

```go
// Step 1: Begin federated login
beginResp, err := client.Auth().BeginFederatedLogin(ctx, &quicbaas.BeginFederatedLoginRequest{
    TenantID:    "tenant-123",
    ProviderID:  "google",
    RedirectURI: "https://myapp.com/auth/callback",
    Scopes:      []string{"openid", "profile", "email"},
})
if err != nil {
    log.Fatal(err)
}

// Step 2: Redirect user to identity provider
// In a web app: http.Redirect(w, r, beginResp.AuthorizationURL, http.StatusFound)
log.Printf("Redirect user to: %s", beginResp.AuthorizationURL)
log.Printf("State (for verification): %s", beginResp.State)

// Step 3: Handle callback when user returns
callbackResp, err := client.Auth().HandleFederatedCallback(ctx, &quicbaas.HandleFederatedCallbackRequest{
    TenantID:    "tenant-123",
    ProviderID:  "google",
    Code:        authorizationCode, // From callback query params
    State:       state,             // Verify this matches beginResp.State
    RedirectURI: "https://myapp.com/auth/callback",
})
if err != nil {
    log.Fatal(err)
}

log.Printf("Access Token: %s", callbackResp.AccessToken)
log.Printf("User ID: %s", callbackResp.UserID)
log.Printf("New User: %v", callbackResp.NewUser)
```

### Session Management

Manage user sessions across multiple devices.

#### List Active Sessions

```go
sessions, err := client.Auth().ListSessions(ctx)
if err != nil {
    log.Fatal(err)
}

for _, s := range sessions.Sessions {
    log.Printf("Session %s:", s.ID)
    log.Printf("  IP: %s", s.IPAddress)
    log.Printf("  User Agent: %s", s.UserAgent)
    log.Printf("  Last Active: %s", s.LastActive)
    log.Printf("  MFA Verified: %v", s.MFAVerified)
}
```

#### Get Session Details

```go
session, err := client.Auth().GetSession(ctx, sessionID)
if err != nil {
    log.Fatal(err)
}

log.Printf("Session created: %s", session.CreatedAt)
log.Printf("Expires: %s", session.ExpiresAt)
```

#### Revoke Sessions

```go
// Revoke a specific session (e.g., from another device)
err := client.Auth().RevokeSession(ctx, sessionID)
if err != nil {
    log.Fatal(err)
}

// Revoke ALL sessions (security measure - logs out everywhere)
err = client.Auth().RevokeAllSessions(ctx)
if err != nil {
    log.Fatal(err)
}
```

### Token Introspection

For resource servers to validate tokens (RFC 7662).

```go
introspection, err := client.Auth().IntrospectToken(ctx, accessToken)
if err != nil {
    log.Fatal(err)
}

if introspection.Active {
    log.Printf("Token is valid")
    log.Printf("Username: %s", introspection.Username)
    log.Printf("Subject: %s", introspection.Subject)
    log.Printf("Scope: %s", introspection.Scope)
    log.Printf("Expires: %s", introspection.ExpiresAt)
} else {
    log.Printf("Token is inactive or expired")
}
```

### JWKS (JSON Web Key Set)

Get public keys for local token verification.

```go
jwks, err := client.Auth().GetJWKS(ctx)
if err != nil {
    log.Fatal(err)
}

for _, key := range jwks.Keys {
    log.Printf("Key ID: %s", key.KID)
    log.Printf("Algorithm: %s", key.ALG)
    log.Printf("Type: %s", key.KTY)
    log.Printf("Use: %s", key.Use)
}
```

### OIDC Discovery

Discover authentication server endpoints and capabilities.

```go
config, err := client.Auth().GetOpenIDConfiguration(ctx)
if err != nil {
    log.Fatal(err)
}

log.Printf("Issuer: %s", config.Issuer)
log.Printf("Authorization Endpoint: %s", config.AuthorizationEndpoint)
log.Printf("Token Endpoint: %s", config.TokenEndpoint)
log.Printf("JWKS URI: %s", config.JWKSURI)
log.Printf("Scopes Supported: %v", config.ScopesSupported)
log.Printf("Response Types Supported: %v", config.ResponseTypesSupported)
log.Printf("MFA Supported: %v", config.MFASupported)
```

### mTLS Authentication

Mutual TLS (mTLS) provides certificate-based authentication at the transport level.

#### Certificate Requirements

| File | Purpose |
|------|---------|
| `client.crt` | Client certificate presented to server |
| `client.key` | Client private key (keep secure) |
| `ca.crt` | CA certificate for server verification |

#### Certificate Generation

```bash
# Using QuicBaaS CLI (recommended)
quicbaas crypto generate-ca --out ca.crt --key ca.key
quicbaas crypto generate-cert --ca ca.crt --ca-key ca.key \
    --out client.crt --key client.key --cn "client"

# Using OpenSSL
openssl req -x509 -newkey rsa:4096 -keyout ca.key -out ca.crt -days 365 -nodes
openssl req -newkey rsa:4096 -keyout client.key -out client.csr -nodes
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out client.crt -days 365
```

#### mTLS Connection

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "/etc/quicbaas/certs/client.crt",
        KeyFile:  "/etc/quicbaas/certs/client.key",
        CAFile:   "/etc/quicbaas/certs/ca.crt",
    }),
)
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

#### Custom TLS Configuration

```go
import (
    "crypto/tls"
    "crypto/x509"
    "os"
)

// Load CA certificate
caCert, _ := os.ReadFile("/path/to/ca.crt")
caCertPool := x509.NewCertPool()
caCertPool.AppendCertsFromPEM(caCert)

// Load client certificate
cert, _ := tls.LoadX509KeyPair("/path/to/client.crt", "/path/to/client.key")

// Create custom TLS config
tlsConfig := &tls.Config{
    MinVersion:   tls.VersionTLS13,
    Certificates: []tls.Certificate{cert},
    RootCAs:      caCertPool,
    CipherSuites: []uint16{
        tls.TLS_AES_256_GCM_SHA384,
        tls.TLS_CHACHA20_POLY1305_SHA256,
    },
}

client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLSConfig(tlsConfig),
)
```

### Connection Methods

#### TCP Connection (with mTLS)

```go
client, err := quicbaas.NewClient("quicbaas.example.com:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "/path/to/client.crt",
        KeyFile:  "/path/to/client.key",
        CAFile:   "/path/to/ca.crt",
    }),
)
```

#### Unix Socket

```go
// Auto-detected from path
client, err := quicbaas.NewClient("/var/run/quicbaas.sock")

// Explicit unix scheme
client, err := quicbaas.NewClient("unix:///var/run/quicbaas.sock")
```

### Peer Credentials Authentication (Unix Socket)

When connecting via Unix domain sockets on Linux, QuicBaaS uses `SO_PEERCRED` to extract kernel-verified peer credentials. The server can verify the client's UID, GID, and PID without any additional authentication tokens.

#### How It Works

The Linux kernel provides the `SO_PEERCRED` socket option which returns the UID, GID, and PID of the process on the other end of a Unix domain socket connection. These credentials are:

- **Kernel-verified**: The kernel provides the credentials, not the client
- **Unforgeable**: The client cannot spoof or modify these values
- **Zero-overhead**: No additional handshake or token exchange required

#### Server-Side Configuration

The QuicBaaS server can enforce credential-based access control:

```go
import grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"

// Allow specific UIDs
verifier := grpcapi.NewUIDVerifier(1000, 1001)

// Allow specific GIDs
verifier := grpcapi.NewGIDVerifier(100)

// Root only
verifier := &grpcapi.RootOnlyVerifier{}

// Allow all (testing only)
verifier := &grpcapi.AllowAllVerifier{}
```

#### Accessing Peer Credentials

From service handlers:

```go
creds := grpcapi.PeerCredentialsFromContext(ctx)
if creds != nil {
    fmt.Printf("Client: UID=%d, GID=%d, PID=%d\n",
        creds.UID, creds.GID, creds.PID)

    if creds.IsRoot() {
        // Handle root user
    }
}
```

#### Platform Support

| Platform | Support |
|----------|---------|
| Linux | Full SO_PEERCRED support |
| macOS | Not supported (stub returns error) |
| Windows | Not supported (stub returns error) |

#### Transport Comparison

| Transport | Use Case | Authentication |
|-----------|----------|----------------|
| gRPC + mTLS | Production remote | Certificate + JWT |
| Unix Socket | Local high-performance | Peer credentials (SO_PEERCRED) |
| REST + TLS | Web/HTTP integration | Certificate + JWT |
| QUIC + TLS | Ultra-low latency | Certificate + JWT |

### Development Mode

For local development only:

```go
// WARNING: Never use in production
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithInsecure(),
)
```

### Security Best Practices

#### Token Management

1. **Store tokens securely** - Use secure storage, never localStorage for web
2. **Refresh before expiry** - Implement proactive token refresh
3. **Use short-lived tokens** - Access tokens should expire in minutes/hours
4. **Rotate refresh tokens** - Enable token rotation for enhanced security

#### Certificate Management

1. **Rotate certificates regularly** - Automate rotation before expiry
2. **Use short-lived certificates** - Limit blast radius of compromise
3. **Secure private keys** - Restrict file permissions to 0600
4. **Separate CA from services** - Keep CA offline or in HSM

#### Session Security

1. **Implement session timeouts** - Automatic logout after inactivity
2. **Monitor active sessions** - Alert on suspicious login patterns
3. **Enable MFA** - Require MFA for sensitive operations
4. **Support session revocation** - Allow users to logout all devices

### Troubleshooting

#### Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `invalid credentials` | Wrong username/password | Verify credentials |
| `token expired` | Access token expired | Refresh the token |
| `invalid token` | Malformed or tampered token | Re-authenticate |
| `mfa required` | MFA not provided | Complete MFA challenge |
| `session revoked` | Session was revoked | Re-authenticate |
| `certificate signed by unknown authority` | CA not trusted | Verify CAFile path |

#### Debug Authentication

```go
// Enable debug logging
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithDebug(true),
)

// Validate token and inspect claims
claims, err := client.Auth().ValidateToken(ctx, token)
if err != nil {
    log.Printf("Token validation failed: %v", err)
} else {
    log.Printf("Token valid: %+v", claims)
}
```

### See Also

- SDK README - SDK overview and quick start
- API Reference - Complete AuthClient API
- Configuration Options - All client options
- Security Documentation - Server security setup


---

## QRDB Composition

QuicBaaS delegates key-value, batch, transaction, MVCC, index, time-series and vector operations to go-qrdb through the go-qrdb SDK client. go-qrdb under go-quicbaas is embedded only: each `quicbaasd` process runs one go-qrdb node in-process, and a clustered deployment embeds a node in each instance. The server's qrdb client is the node's own SDK client (`node.Client()`), and every shard ID resolves to it.

go-quicbaas never connects to a separate go-qrdb daemon. The `qrdb:` config block (`qrdb.transport` and its `QUICBAAS_QRDB_*` variables) is removed: a config file that still carries it is refused with `RemovedKeyError` naming the `qrdb` key.

### See Also

- [SDK Architecture](architecture.md) -- Transport interface and client patterns


---

## Local Dependencies

QuicBaaS resolves its sibling repos (go-qrdb, go-quicraft, go-xkms, go-signal,
go-libsignal, go-objstore, go-codec) as **tagged Go modules published to the
company Gitea server**. go-objstore is required directly: it owns all object
storage and go-quicbaas re-exports it (the SPEC's
[Storage clean-up](../SPEC.md#storage-clean-up) row covers the SDK re-export). There is no Go workspace, no `replace` directive, and
no dependence on where a sibling repo happens to be checked out. A clone of
this repo alone builds and tests.

See `software/conventions/dependency-resolution.md` in the automatethethings
repo for the portfolio-wide rules; the short version is below.

### How resolution works

Every sibling module keeps the module path `github.com/jeremyhahn/<repo>`, and
`GOPRIVATE=github.com/jeremyhahn/*` plus a git url-rewrite points fetches at
Gitea. `GOPRIVATE` is the only mechanism in play — it exempts those paths from
both the module proxy and the checksum database, which is what lets the
rewrite fire. Do not set `GOPROXY`, `GOSUMDB`, `GONOSUMDB`, `GOINSECURE` or
`GOFLAGS` to work around a fetch problem: doing so also routes every
*third-party* module around proxy.golang.org and disables sumdb verification
for all of them. That is not hypothetical — it broke this repo on 2026-08-14
when upstream re-pointed a `go-tpm-tools` tag and a direct fetch produced a
zip whose hash no longer matched the correct go.sum entry.

`GOWORK=off` is the default posture. If a stray `go.work` exists anywhere above
this checkout, it is a mistake; delete it rather than accommodate it.

### Local development

`go build`, `go test` and `gopls` work from a plain clone. The one requirement
is credentials for the Gitea host — the same url-rewrite the runners use. No
sibling checkout needs to exist on disk.

To take a new version of a sibling, tag it in its own repo and bump the
`require` here (`go get github.com/jeremyhahn/go-qrdb@v0.3.0-alpha`).

### Deployment pins

The go-qrdb, go-xkms and go-quicraft versions in `go.mod` and `sdk/go/go.mod`
are the exact releases QuicBaaS deploys with. QuicBaaS never takes a library's
unreleased main: `make check-library-pins` (part of `make test`) fails on a
pseudo-version or a `replace` directive for any of the three, and `make
proto-gen` reads go-qrdb's `authz.proto` from the pinned module rather than
from a sibling checkout.

Libraries change their APIs and formats whenever product quality needs it;
nothing is frozen before a release candidate. A pin bump is a normal reviewed
change. QuicBaaS deploys to the 3-node cluster only after go-qrdb's Raft
clustering and storage have passed its 3-node test plan there.

### Coupled changes across repos

When a change spans this repo and a sibling, land it bottom-up: change the
sibling, tag it, then bump the requirement here. A scratch workspace is
permitted while iterating, but it never gets committed and never leaves your
machine — nothing on the CI path may depend on it.

### Docker and CI

The integration and e2e images build from the pinned versions in `go.mod`, the
same as every other build. They receive credentials through a **BuildKit
secret mount**, never a build argument and never a baked layer:

```dockerfile
RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then . /run/secrets/gomodauth; fi; \
    go mod download
```

`make stage-gomodauth` writes that secret file with `umask 077` -- from the
`GIT_CONFIG_*` variables the runners inject, or on a developer or agent host
from a Gitea read token -- and every target whose compose file mounts it stages
it before its build and empties it afterwards. See
[private-modules.md](../testing/private-modules.md). The secret is sourced,
never echoed; verify a token by its shape, never by printing it.

Two rules the harness depends on, both learned the hard way:

- **Tracked files are baked into the image; per-run generated material is
  produced by a service inside the stack into a named volume.** Nothing is
  bind-mounted in from the caller. A job container drives the *host's* Docker
  daemon through a mounted socket, so a `./relative` bind source resolves
  against the host filesystem, and Docker helpfully creates an empty directory
  where the file should have been. The certificates are generated by the
  `certs-init` service into the `quicbaas-certs` volume, and every consumer
  declares `depends_on: certs-init: condition: service_completed_successfully`.
- **A compose `command:` replaces the image's `CMD` entirely.** Fixing
  something in a Dockerfile's `CMD` does nothing if the compose service also
  sets `command:`.

### Pinned Dependencies

| Module | Pinned Version | Reason |
|--------|---------------|--------|
| `github.com/armon/go-metrics` | `v0.4.1` | Prevents upgrade to versions with breaking API changes that affect `hashicorp/memberlist` |

### Build tags

`gms_pure_go` is required for every `CGO_ENABLED=0` build. `dolthub/go-mysql-server`'s
`internal/regex` package ships only `cgo && !gms_pure_go` and `gms_pure_go`
files, so without the tag a static build has no implementation to link and
fails at link time. The Makefile adds it to `BUILD_TAGS` by default; the
workflows and Dockerfiles pass it explicitly where they build directly.
