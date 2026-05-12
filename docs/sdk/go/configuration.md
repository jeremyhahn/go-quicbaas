# QuicBaaS Go SDK Configuration

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

**Note:** The `WithMCPCredentials(user, pass)` option is deprecated. QuicBaaS is passwordless-first; obtain tokens via WebAuthn, smartcard, or OAuth2 client credentials instead. See [Passwordless Authentication](../../auth/passwordless.md).

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
