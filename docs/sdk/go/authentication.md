# Authentication

QuicBaaS SDK authentication supports multiple methods including mTLS, JWT tokens, and OIDC federation.

## Overview

QuicBaaS provides comprehensive authentication options for secure client-server communication.

| Method | Status | Description |
|--------|--------|-------------|
| mTLS | Production | Mutual TLS with X.509 certificates |
| JWT | Production | JSON Web Token authentication |
| OIDC | Production | OpenID Connect federation |
| Session Management | Production | Multi-device session control |
| MFA | Production | Multi-factor authentication support |

## JWT Token Authentication

The SDK provides full JWT authentication with access and refresh tokens.

### Login with Credentials

```go
package main

import (
    "context"
    "log"

    "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    client, err := quicbaas.NewClient("localhost:50051")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Login with username and password
    resp, err := client.Auth().Login(ctx, &quicbaas.LoginRequest{
        Username: "user@example.com",
        Password: "secret",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Use the access token for API calls
    log.Printf("Access Token: %s", resp.AccessToken)
    log.Printf("Expires At: %s", resp.ExpiresAt)
}
```

### Token Refresh

```go
// Refresh token before it expires
refreshResp, err := client.Auth().RefreshToken(ctx, resp.RefreshToken)
if err != nil {
    log.Fatal(err)
}
log.Printf("New Access Token: %s", refreshResp.AccessToken)
```

### Token Validation

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

### Logout

```go
// Logout and invalidate tokens
err := client.Auth().Logout(ctx, accessToken)
if err != nil {
    log.Fatal(err)
}
```

## Multi-Factor Authentication (MFA)

When MFA is enabled, login may require additional verification.

### Supported MFA Types

| Type | Description |
|------|-------------|
| TOTP | Time-based One-Time Password (Google Authenticator, Authy) |
| SMS | SMS-based verification codes |
| Email | Email-based verification codes |
| BackupCode | One-time backup recovery codes |

> **Note:** WebAuthn/FIDO2 authentication is handled by browser-based clients. The Go SDK can use JWT tokens obtained from WebAuthn-authenticated browser sessions.

### MFA Login Flow

```go
// Initial login attempt
resp, err := client.Auth().Login(ctx, &quicbaas.LoginRequest{
    Username: "user@example.com",
    Password: "secret",
})

if resp.MFARequired {
    // MFA verification needed
    log.Printf("MFA Type: %s", resp.MFAChallenge.Type)
    log.Printf("Challenge ID: %s", resp.MFAChallenge.ChallengeID)

    // Prompt user for MFA code and retry
    resp, err = client.Auth().Login(ctx, &quicbaas.LoginRequest{
        Username: "user@example.com",
        Password: "secret",
        MFACode:  totpCode, // User-provided TOTP code
    })
    if err != nil {
        log.Fatal(err)
    }
}

// Now authenticated with MFA
log.Printf("Access Token: %s", resp.AccessToken)
```

## OIDC/OAuth2 Federated Authentication

Integrate with external identity providers like Google, Okta, Azure AD, and Keycloak.

### List Available Providers

```go
providers, err := client.Auth().ListProviders(ctx, tenantID)
if err != nil {
    log.Fatal(err)
}

for _, p := range providers.Providers {
    log.Printf("Provider: %s (%s) - Enabled: %v", p.Name, p.Type, p.Enabled)
}
```

### Get Provider Details

```go
provider, err := client.Auth().GetProvider(ctx, tenantID, "google")
if err != nil {
    log.Fatal(err)
}

log.Printf("Issuer URL: %s", provider.IssuerURL)
log.Printf("Auth URL: %s", provider.AuthURL)
log.Printf("Scopes: %v", provider.Scopes)
```

### OIDC Login Flow

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

## Session Management

Manage user sessions across multiple devices.

### List Active Sessions

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

### Get Session Details

```go
session, err := client.Auth().GetSession(ctx, sessionID)
if err != nil {
    log.Fatal(err)
}

log.Printf("Session created: %s", session.CreatedAt)
log.Printf("Expires: %s", session.ExpiresAt)
```

### Revoke Sessions

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

## Token Introspection

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

## JWKS (JSON Web Key Set)

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

## OIDC Discovery

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

## mTLS Authentication

Mutual TLS (mTLS) provides certificate-based authentication at the transport level.

### Certificate Requirements

| File | Purpose |
|------|---------|
| `client.crt` | Client certificate presented to server |
| `client.key` | Client private key (keep secure) |
| `ca.crt` | CA certificate for server verification |

### Certificate Generation

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

### mTLS Connection

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

### Custom TLS Configuration

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

## Connection Methods

### TCP Connection (with mTLS)

```go
client, err := quicbaas.NewClient("quicbaas.example.com:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "/path/to/client.crt",
        KeyFile:  "/path/to/client.key",
        CAFile:   "/path/to/ca.crt",
    }),
)
```

### Unix Socket

```go
// Auto-detected from path
client, err := quicbaas.NewClient("/var/run/quicbaas.sock")

// Explicit unix scheme
client, err := quicbaas.NewClient("unix:///var/run/quicbaas.sock")
```

## Peer Credentials Authentication (Unix Socket)

When connecting via Unix domain sockets on Linux, QuicBaaS uses `SO_PEERCRED` to extract kernel-verified peer credentials. The server can verify the client's UID, GID, and PID without any additional authentication tokens.

### How It Works

The Linux kernel provides the `SO_PEERCRED` socket option which returns the UID, GID, and PID of the process on the other end of a Unix domain socket connection. These credentials are:

- **Kernel-verified**: The kernel provides the credentials, not the client
- **Unforgeable**: The client cannot spoof or modify these values
- **Zero-overhead**: No additional handshake or token exchange required

### Server-Side Configuration

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

### Accessing Peer Credentials

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

### Platform Support

| Platform | Support |
|----------|---------|
| Linux | Full SO_PEERCRED support |
| macOS | Not supported (stub returns error) |
| Windows | Not supported (stub returns error) |

### Transport Comparison

| Transport | Use Case | Authentication |
|-----------|----------|----------------|
| gRPC + mTLS | Production remote | Certificate + JWT |
| Unix Socket | Local high-performance | Peer credentials (SO_PEERCRED) |
| REST + TLS | Web/HTTP integration | Certificate + JWT |
| QUIC + TLS | Ultra-low latency | Certificate + JWT |

## Development Mode

For local development only:

```go
// WARNING: Never use in production
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithInsecure(),
)
```

## Security Best Practices

### Token Management

1. **Store tokens securely** - Use secure storage, never localStorage for web
2. **Refresh before expiry** - Implement proactive token refresh
3. **Use short-lived tokens** - Access tokens should expire in minutes/hours
4. **Rotate refresh tokens** - Enable token rotation for enhanced security

### Certificate Management

1. **Rotate certificates regularly** - Automate rotation before expiry
2. **Use short-lived certificates** - Limit blast radius of compromise
3. **Secure private keys** - Restrict file permissions to 0600
4. **Separate CA from services** - Keep CA offline or in HSM

### Session Security

1. **Implement session timeouts** - Automatic logout after inactivity
2. **Monitor active sessions** - Alert on suspicious login patterns
3. **Enable MFA** - Require MFA for sensitive operations
4. **Support session revocation** - Allow users to logout all devices

## Troubleshooting

### Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `invalid credentials` | Wrong username/password | Verify credentials |
| `token expired` | Access token expired | Refresh the token |
| `invalid token` | Malformed or tampered token | Re-authenticate |
| `mfa required` | MFA not provided | Complete MFA challenge |
| `session revoked` | Session was revoked | Re-authenticate |
| `certificate signed by unknown authority` | CA not trusted | Verify CAFile path |

### Debug Authentication

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

## See Also

- [SDK README](README.md) - SDK overview and quick start
- [API Reference](api-reference.md) - Complete AuthClient API
- [Configuration Options](configuration.md) - All client options
- [Security Documentation](../../security/README.md) - Server security setup
