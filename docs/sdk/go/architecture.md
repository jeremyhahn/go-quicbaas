# SDK Architecture

## Overview

The QuicBaaS Go SDK uses a layered architecture with transport abstraction, allowing applications to switch between gRPC, REST, QUIC, MCP, and Embedded protocols without changing business logic. The SDK provides both a unified client interface and standalone domain-specific clients.

## Components

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Application Code                             │
└───────────────────────────────┬─────────────────────────────────────┘
                                │
┌───────────────────────────────▼─────────────────────────────────────┐
│                        quicbaas.Client                               │
│                      (Unified Client)                                │
├─────────────────────────────────────────────────────────────────────┤
│  ┌─────┐ ┌───────┐ ┌─────┐ ┌─────────┐ ┌────────┐ ┌──────────────┐ │
│  │ KV  │ │Secrets│ │ KMS │ │ Crypto  │ │ Vector │ │  TimeSeries  │ │
│  └──┬──┘ └───┬───┘ └──┬──┘ └────┬────┘ └───┬────┘ └──────┬───────┘ │
│     │        │        │         │          │             │         │
│  ┌──┴────────┴────────┴─────────┴──────────┴─────────────┴──┐      │
│  │                    Domain Clients                         │      │
│  │   (Batch, Transaction, Query, Cluster, Health, Seal,     │      │
│  │    Auth, Backup)                                          │      │
│  └──────────────────────────┬────────────────────────────────┘      │
└─────────────────────────────┼───────────────────────────────────────┘
                              │
┌─────────────────────────────▼───────────────────────────────────────┐
│                      Transport Interface                             │
│                                                                      │
│   Request(ctx, service, method, req) (resp, error)                  │
│   RequestStream(ctx, service, method, req) (Stream, error)          │
│   Conn() interface{}                                                 │
│   Close() error                                                      │
│   Healthy() bool                                                     │
└─────────────────────────────┬───────────────────────────────────────┘
                              │
    ┌───────────┬─────────────┼─────────────┬───────────┐
    │           │             │             │           │
┌───▼───┐  ┌───▼───┐    ┌────▼────┐   ┌────▼───┐  ┌───▼──────┐
│ gRPC  │  │ REST  │    │  QUIC   │   │  MCP   │  │ Embedded │
│       │  │       │    │         │   │        │  │          │
├───────┤  ├───────┤    ├─────────┤   ├────────┤  ├──────────┤
│-Pool  │  │-HTTP  │    │-Streams │   │-JSON-  │  │-Direct   │
│-Retry │  │-JSON  │    │-Binary  │   │ RPC2.0 │  │ dispatch │
│-Unix  │  │-TLS   │    │-TLS1.3 │   │-HTTP   │  │-Zero     │
│-mTLS  │  │       │    │         │   │-Auth   │  │ overhead │
└───┬───┘  └───┬───┘    └────┬────┘   └────┬───┘  └───┬──────┘
    │          │             │             │           │
    └──────────┴─────────────┼─────────────┘           │
                             │                         │
┌────────────────────────────▼──────────────────┐  ┌───▼──────────────┐
│                QuicBaaS Server                 │  │ service.QuicBaaS │
│              (network access)                  │  │   (in-process)   │
└────────────────────────────────────────────────┘  └──────────────────┘
```

## Design Principles

### Transport Abstraction

The transport layer abstracts protocol-specific details, allowing the SDK to support multiple protocols through a common interface:

```go
type Transport interface {
    // Request sends a unary request and returns the response
    Request(ctx context.Context, service, method string, req interface{}) (interface{}, error)

    // RequestStream initiates a streaming request
    RequestStream(ctx context.Context, service, method string, req interface{}) (Stream, error)

    // Conn returns the underlying connection for advanced usage
    Conn() interface{}

    // Close closes the transport connection
    Close() error

    // Healthy returns true if the transport is healthy
    Healthy() bool
}
```

All six transports implement this interface. The MCP transport maps service/method pairs to JSON-RPC tool calls (`quicbaas_<service>_<method>`). The Embedded transport dispatches directly to `service.QuicBaaS` handler functions via O(1) map lookup with no serialization.

### Hybrid Client Pattern

The SDK supports two usage patterns:

**1. Unified Client** - Single entry point with all services:
```go
client, _ := quicbaas.NewClient("localhost:50051")
client.KV().Put(ctx, req)
client.KMS().GenerateKey(ctx, req)
client.Secrets().Store(ctx, req)
```

**2. Standalone Clients** - Individual domain clients:
```go
transport, _ := grpc.NewTransport("localhost:50051")
kvClient := kv.NewClient(transport)
kvClient.Put(ctx, req)
```

### Functional Options

Configuration uses the functional options pattern for flexibility:

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTimeout(30*time.Second),
    quicbaas.WithTLS(tlsConfig),
    quicbaas.WithConnectionPool(10, 50),
    quicbaas.WithRetry(3, 100*time.Millisecond),
)
```

## Connection Management

### Address Parsing

The SDK automatically detects address types:

| Input | Detected Type | Notes |
|-------|---------------|-------|
| `localhost:50051` | TCP | Standard network address |
| `/var/run/quicbaas.sock` | Unix | Path starting with `/` |
| `unix:///path/to/sock` | Unix | Explicit Unix prefix |
| `unix-abstract://name` | Unix Abstract | Linux abstract namespace |
| `http://localhost:8080` | REST | HTTP/HTTPS triggers REST transport |

For MCP transport, use `WithMCP()` with an HTTP address. For Embedded transport, the address is ignored.

### Connection Pooling (gRPC)

```
┌─────────────────────────────────────────────────────────────────┐
│                      Connection Pool                             │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────┐  ┌──────────┐  ┌──────────┐       ┌──────────┐   │
│  │  Conn 1  │  │  Conn 2  │  │  Conn 3  │  ...  │  Conn N  │   │
│  │ (active) │  │  (idle)  │  │ (active) │       │  (idle)  │   │
│  └──────────┘  └──────────┘  └──────────┘       └──────────┘   │
├─────────────────────────────────────────────────────────────────┤
│  Pool Config:                                                    │
│  - MinSize: 5 (minimum connections)                             │
│  - MaxSize: 50 (maximum connections)                            │
│  - MaxAge: 1h (connection lifetime)                             │
│  - HealthCheck: 30s (check interval)                            │
└─────────────────────────────────────────────────────────────────┘
```

### Retry with Backoff

```
Request Failed
     │
     ▼
┌─────────────┐
│ Retryable?  │──No──▶ Return Error
└──────┬──────┘
       │ Yes
       ▼
┌─────────────┐
│ Max Retries │──Yes──▶ Return Error
│  Exceeded?  │
└──────┬──────┘
       │ No
       ▼
┌─────────────┐
│    Wait     │  Backoff: min(baseDelay * 2^attempt, maxDelay)
│  (backoff)  │
└──────┬──────┘
       │
       ▼
    Retry Request
```

## Error Handling

### Typed Errors

All errors are typed for programmatic handling:

```go
type QRDBError struct {
    Code      ErrorCode
    Op        string                 // Operation that failed
    Err       error                  // Underlying error
    Context   map[string]interface{} // Additional context
    Retryable bool                   // Can be retried
}
```

### Error Codes

| Code | Description | Retryable |
|------|-------------|-----------|
| `NotFound` | Resource not found | No |
| `AlreadyExists` | Resource already exists | No |
| `InvalidInput` | Invalid request parameters | No |
| `Timeout` | Operation timed out | Yes |
| `Unavailable` | Service unavailable | Yes |
| `Unauthorized` | Authentication failed | No |
| `Forbidden` | Permission denied | No |
| `Internal` | Internal server error | Sometimes |

### Error Helpers

```go
if errors.IsNotFound(err) {
    // Handle not found
}

if errors.IsRetryable(err) {
    // Safe to retry
}

code := errors.Code(err)  // Get error code
```

## Watch Architecture

The watch system provides real-time change notifications:

```
┌─────────────────────────────────────────────────────────────────┐
│                         Client                                   │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                      Watcher                                │ │
│  │                                                             │ │
│  │  Events() <-chan *WatchEvent                               │ │
│  │  Err() error                                                │ │
│  │  Close() error                                              │ │
│  └─────────────────────────┬──────────────────────────────────┘ │
└────────────────────────────┼────────────────────────────────────┘
                             │ gRPC Stream
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                         Server                                   │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                   Watch Manager                             │ │
│  │                                                             │ │
│  │  - Tracks active watchers                                  │ │
│  │  - Filters events by key/prefix                            │ │
│  │  - Manages revisions                                        │ │
│  └─────────────────────────┬──────────────────────────────────┘ │
│                             │                                    │
│  ┌─────────────────────────▼──────────────────────────────────┐ │
│  │                   State Machine                             │ │
│  │                                                             │ │
│  │  - Emits change events on Put/Delete                       │ │
│  │  - Tracks MVCC revisions                                   │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### Watch Options

| Option | Description |
|--------|-------------|
| `WithStartRevision(rev)` | Start from specific MVCC revision |
| `WithPrevKV()` | Include previous value in events |
| `WithProgressNotify()` | Periodic progress updates |
| `WithFilterPut()` | Only receive DELETE events |
| `WithFilterDelete()` | Only receive PUT events |

## Security

### mTLS Authentication

The SDK uses mutual TLS for authentication. API keys are not supported.

```
┌─────────────┐                           ┌─────────────┐
│   Client    │                           │   Server    │
├─────────────┤                           ├─────────────┤
│ Client Cert │◀────── Verify ───────────▶│ Server Cert │
│ Client Key  │                           │ Server Key  │
│ CA Cert     │                           │ CA Cert     │
└─────────────┘                           └─────────────┘
```

### Certificate Setup

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "/path/to/client.crt",
        KeyFile:  "/path/to/client.key",
        CAFile:   "/path/to/ca.crt",
    }),
)
```

## Package Structure

```
sdk/go/
├── quicbaas.go           # Unified client entry point
├── options.go            # Client options
├── doc.go                # Package documentation
│
├── transport/            # Transport abstraction
│   ├── transport.go      # Transport interface
│   ├── address.go        # Address parsing
│   ├── tls.go            # TLS helpers
│   ├── grpc/             # gRPC implementation
│   ├── rest/             # REST implementation
│   ├── quic/             # QUIC implementation
│   ├── unix/             # Unix socket implementation
│   ├── mcp/              # MCP (Model Context Protocol) implementation
│   └── embedded/         # Embedded in-process implementation
│
├── client/               # Domain clients
│   ├── kv/               # Key-value with Watch
│   ├── batch/            # Batch operations
│   ├── transaction/      # Transactions
│   ├── query/            # Queries
│   ├── cluster/          # Cluster management
│   ├── health/           # Health checks
│   ├── auth/             # Authentication
│   ├── seal/             # Seal/unseal
│   ├── kms/              # Key management
│   ├── secrets/          # Secrets
│   ├── crypto/           # Crypto operations
│   ├── vector/           # Vector search
│   ├── timeseries/       # Time-series
│   └── backup/           # Backup/restore
│
├── errors/               # Error types
│   ├── errors.go
│   └── codes.go
│
└── internal/             # Internal packages
    └── proto/            # Proto converters
```
