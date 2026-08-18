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

## QRDB Transport Configuration

QuicBaaS delegates key-value, batch, transaction, MVCC, index, time-series, and vector operations to the qrdb engine via the qrdb SDK client. By default, the SDK uses an embedded (in-process) transport with zero network overhead. For deployments where qrdb runs as a separate service, a network transport can be configured.

### Configuration

Add a `qrdb` section to your QuicBaaS configuration file:

```yaml
qrdb:
  transport: embedded    # embedded, grpc, rest, quic, mcp, unix
  address: ""            # Required for grpc, rest, quic, mcp
  unix_socket_path: ""   # Required for unix
  tls_enabled: false
  tls_cert_file: ""
  tls_key_file: ""
  tls_ca_file: ""
```

### Transport Factory

Transport selection uses a map-based dispatch pattern for O(1) constant-time lookup. The factory validates configuration, builds transport options, constructs the transport, and wraps it in an SDK client.

```go
// Map-based dispatch -- each transport type maps to its constructor
var networkTransportHandlers = map[TransportType]networkTransportConstructor{
    TransportGRPC: func(opts ...transport.Option) (transport.Transport, error) {
        return grpctransport.New(opts...)
    },
    TransportREST: func(opts ...transport.Option) (transport.Transport, error) {
        return resttransport.New(opts...)
    },
    TransportQUIC: func(opts ...transport.Option) (transport.Transport, error) {
        return quictransport.New(opts...)
    },
    TransportMCP: func(opts ...transport.Option) (transport.Transport, error) {
        return mcptransport.New(opts...)
    },
    TransportUnix: func(opts ...transport.Option) (transport.Transport, error) {
        return unixtransport.New(opts...)
    },
}
```

The embedded transport is excluded from the map because it requires direct service references (KV service, admin service) rather than network options. When the transport type is `embedded` or unset, the factory creates per-shard in-process clients directly.

**Factory flow:**

```
Config -> transportFactory.transportType()
               |
               +-- embedded? -> create per-shard embedded SDK clients
               |
               +-- network?  -> validateNetworkConfig()
                                    |
                                    +-> buildTransportOptions(cfg)
                                    +-> networkTransportHandlerstype
                                    +-> qrdbsdk.NewClient(transport)
```

### Transport Types

| Transport | Value | Description |
|-----------|-------|-------------|
| Embedded | `embedded` | In-process calls. Zero overhead. Default. |
| gRPC | `grpc` | gRPC to a remote qrdb server |
| REST | `rest` | REST/HTTP to a remote qrdb server |
| QUIC | `quic` | QUIC to a remote qrdb server |
| MCP | `mcp` | Model Context Protocol transport |
| Unix | `unix` | Unix domain socket transport |

### When to Use Each Transport

| Transport | Best For | Latency | Throughput | Notes |
|-----------|----------|---------|------------|-------|
| **Embedded** | Single-process deployments, testing | Lowest (no network) | Highest | Default. Zero serialization overhead. |
| **gRPC** | Production clusters, service meshes | Low | High | Connection pooling, mTLS, streaming support. |
| **REST** | HTTP integrations, browser clients, debugging | Medium | Medium | JSON serialization. Easy to inspect with curl. |
| **QUIC** | Low-latency WAN, mobile clients | Low | High | UDP-based, TLS 1.3 built-in, multiplexed streams. |
| **Unix** | Same-host IPC, sidecar deployments | Very low | High | No TCP overhead. File permission-based security. |
| **MCP** | AI/LLM tool integrations | Medium | Medium | JSON-RPC 2.0. Maps service/method to tool calls. |

### Per-Transport Configuration

#### Embedded (Default)

No configuration needed. The embedded transport is used when `qrdb.transport` is unset or `"embedded"`.

```yaml
qrdb:
  transport: embedded
```

No `address` or TLS fields are required. The factory creates per-shard SDK clients that dispatch directly to the service layer via in-process function calls with no serialization.

#### gRPC

```yaml
qrdb:
  transport: grpc
  address: "qrdb-server:50051"
  tls_enabled: true
  tls_cert_file: /etc/quicbaas/tls/client.crt
  tls_key_file: /etc/quicbaas/tls/client.key
  tls_ca_file: /etc/quicbaas/tls/ca.crt
```

| Field | Required | Description |
|-------|----------|-------------|
| `address` | Yes | `host:port` of the QRDB gRPC server |
| `tls_enabled` | No | Enable mTLS (recommended for production) |
| `tls_cert_file` | Conditional | Client certificate for mTLS |
| `tls_key_file` | Conditional | Client private key for mTLS |
| `tls_ca_file` | Conditional | CA certificate for server verification |

#### REST

```yaml
qrdb:
  transport: rest
  address: "https://qrdb-server:8443"
  tls_enabled: true
  tls_ca_file: /etc/quicbaas/tls/ca.crt
```

| Field | Required | Description |
|-------|----------|-------------|
| `address` | Yes | Base URL of the QRDB REST server |
| `tls_enabled` | No | Enable TLS |
| `tls_ca_file` | Conditional | CA certificate for server verification |

#### QUIC

```yaml
qrdb:
  transport: quic
  address: "qrdb-server:4433"
  tls_enabled: true
  tls_cert_file: /etc/quicbaas/tls/client.crt
  tls_key_file: /etc/quicbaas/tls/client.key
  tls_ca_file: /etc/quicbaas/tls/ca.crt
```

| Field | Required | Description |
|-------|----------|-------------|
| `address` | Yes | `host:port` of the QRDB QUIC server |
| `tls_enabled` | No | TLS 1.3 is built into QUIC; this controls mTLS |
| `tls_cert_file` | Conditional | Client certificate for mTLS |
| `tls_key_file` | Conditional | Client private key for mTLS |
| `tls_ca_file` | Conditional | CA certificate for server verification |

#### Unix Socket

```yaml
qrdb:
  transport: unix
  unix_socket_path: /var/run/qrdb.sock
```

| Field | Required | Description |
|-------|----------|-------------|
| `unix_socket_path` | Yes | Path to the Unix domain socket |

The `address` field is not used. The socket path is passed to the transport as the address. Security relies on file system permissions.

#### MCP (Model Context Protocol)

```yaml
qrdb:
  transport: mcp
  address: "http://localhost:3000"
```

| Field | Required | Description |
|-------|----------|-------------|
| `address` | Yes | MCP server endpoint |
| `tls_enabled` | No | Enable TLS for the HTTP connection |

MCP maps `service/method` pairs to JSON-RPC 2.0 tool calls in the format `quicbaas_<service>_<method>`.

### Environment Variables

All fields support Viper environment variable binding:

| Variable | Config Key |
|----------|-----------|
| `QUICBAAS_QRDB_TRANSPORT` | `qrdb.transport` |
| `QUICBAAS_QRDB_ADDRESS` | `qrdb.address` |
| `QUICBAAS_QRDB_UNIX_SOCKET_PATH` | `qrdb.unix_socket_path` |
| `QUICBAAS_QRDB_TLS_ENABLED` | `qrdb.tls_enabled` |
| `QUICBAAS_QRDB_TLS_CERT_FILE` | `qrdb.tls_cert_file` |
| `QUICBAAS_QRDB_TLS_KEY_FILE` | `qrdb.tls_key_file` |
| `QUICBAAS_QRDB_TLS_CA_FILE` | `qrdb.tls_ca_file` |

### Architecture

For the embedded transport, the `ShardedClient` creates a per-shard qrdb SDK client. Each shard gets its own service wrappers that route Raft proposals through the host's `SyncPropose`.

For network transports, a single shared client is created because the remote qrdb server handles shard routing internally. This simplifies the client-side logic and reduces connection overhead.

```
Embedded:  QuicBaaS -> ShardedClient -> [per-shard embedded SDK client] -> service layer -> Raft
Network:   QuicBaaS -> ShardedClient -> [shared network SDK client] -> remote qrdb server
```

### Error Types

| Error | Cause |
|-------|-------|
| `UnixTransportMissingSocketError` | Unix transport configured without `unix_socket_path` |
| `NetworkTransportMissingAddressError` | Network transport configured without `address` |
| `UnsupportedTransportError` | Unknown transport type in config |
| `TransportCreationError` | Transport constructor or SDK client creation failed |

### See Also

- [SDK Architecture](architecture.md) -- Transport interface and client patterns
- BaaS Migration -- Why all operations route through the SDK


---

## Local Dependencies

QuicBaaS resolves its sibling repos (go-qrdb, go-quicraft, go-xkms, go-signal,
go-libsignal, go-objstore, go-codec) as **tagged Go modules published to the
company Gitea server**. There is no Go workspace, no `replace` directive, and
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
`require` here (`go get github.com/jeremyhahn/go-qrdb@v0.3.0-alpha`). Compile
against the latest tagged version of each sibling rather than pinning to an
old one.

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

`make stage-gomodauth` writes that secret file with `umask 077` from the
`GIT_CONFIG_*` variables the runners inject, and `make integration-test`
invokes it. The secret is sourced, never echoed; verify a token by its shape,
never by printing it.

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
