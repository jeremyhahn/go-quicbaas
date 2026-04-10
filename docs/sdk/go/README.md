# QuicBaaS Go SDK

Official Go SDK for QuicBaaS with support for gRPC, REST, QUIC, MCP, and Embedded protocols.

## Overview

The QuicBaaS Go SDK provides a type-safe, high-performance client library for interacting with QuicBaaS servers. It supports multiple transport protocols, connection pooling, automatic retries, and all QuicBaaS services including KV storage, KMS, secrets management, vector search, and time-series data.

## Key Features

| Feature | Description |
|---------|-------------|
| **Unified Client** | Single client with access to all services via `client.KV()`, `client.KMS()`, etc. |
| **Standalone Clients** | Use individual clients like `kv.NewClient(conn)` for specific needs |
| **Multi-Protocol** | gRPC (default), REST, QUIC, MCP, and Embedded transport support |
| **Connection Pooling** | High-performance connection management for gRPC |
| **Automatic Retry** | Configurable retry with exponential backoff |
| **mTLS Authentication** | Secure mutual TLS authentication (no API keys) |
| **Unix Socket** | High-performance local connections |
| **Watch Support** | Real-time key change notifications |
| **Type Safety** | Fully typed requests and responses |

## Installation

```bash
go get github.com/jeremyhahn/go-quicbaas/sdk/go
```

## Quick Start

### Unified Client

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/jeremyhahn/go-quicbaas/sdk/go"
    "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
)

func main() {
    ctx := context.Background()

    // Connect via Unix socket (default for local)
    client, err := quicbaas.NewClient("/var/run/quicbaas.sock")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Put a value
    _, err = client.KV().Put(ctx, &kv.PutRequest{
        EntityType: "users",
        Key:        "user-123",
        Value:      []byte(`{"name": "Alice", "email": "alice@example.com"}`),
    })
    if err != nil {
        log.Fatal(err)
    }

    // Get the value
    resp, err := client.KV().Get(ctx, &kv.GetRequest{
        EntityType: "users",
        Key:        "user-123",
    })
    if err != nil {
        log.Fatal(err)
    }

    if resp.Found {
        log.Printf("User: %s", string(resp.Value))
    }
}
```

### With mTLS

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "/path/to/client.crt",
        KeyFile:  "/path/to/client.key",
        CAFile:   "/path/to/ca.crt",
    }),
)
```

### Watch for Changes

```go
// Watch a key prefix for changes
watcher, err := client.KV().WatchPrefix(ctx, "users/",
    kv.WithPrevKV(),  // Include previous value in events
)
if err != nil {
    log.Fatal(err)
}
defer watcher.Close()

for event := range watcher.Events() {
    switch event.Type {
    case kv.EventPut:
        log.Printf("PUT %s: %s", event.Key, string(event.Value))
    case kv.EventDelete:
        log.Printf("DELETE %s", event.Key)
    }
}
```

## Transports

| Transport | Use Case | Address Format |
|-----------|----------|----------------|
| gRPC | Default, high throughput | `localhost:50051` |
| Unix Socket | Local, lowest latency | `/var/run/quicbaas.sock` |
| REST | Web integration | `http://localhost:8080` |
| QUIC | Ultra-low latency | `localhost:50052` |
| MCP | AI/LLM agent tool integration | `http://localhost:8080` |
| Embedded | In-process, zero network overhead | N/A (direct service reference) |

### Connection Examples

```go
// gRPC (default)
client, _ := quicbaas.NewClient("localhost:50051")

// Unix socket (auto-detected)
client, _ := quicbaas.NewClient("/var/run/quicbaas.sock")

// Unix socket (explicit)
client, _ := quicbaas.NewClient("unix:///var/run/quicbaas.sock")

// REST
client, _ := quicbaas.NewClient("http://localhost:8080", quicbaas.WithREST())

// QUIC
client, _ := quicbaas.NewClient("localhost:50052", quicbaas.WithQUIC())

// MCP (Model Context Protocol)
client, _ := quicbaas.NewClient("http://localhost:8080", quicbaas.WithMCP())

// MCP with auto-authentication
client, _ := quicbaas.NewClient("http://localhost:8080",
    quicbaas.WithMCP(),
    quicbaas.WithMCPCredentials("admin", "admin"),
)

// MCP with pre-existing token
client, _ := quicbaas.NewClient("http://localhost:8080",
    quicbaas.WithMCP(),
    quicbaas.WithMCPAuthToken(token),
)

// Embedded (in-process, no network)
client, _ := quicbaas.NewClient("", quicbaas.WithEmbedded(svc))
```

## Services

| Service | Description |
|---------|-------------|
| `KV()` | Key-value storage with CRUD and Watch |
| `Batch()` | Atomic batch operations |
| `Transaction()` | Compare-and-swap transactions |
| `Query()` | Secondary index queries |
| `Cluster()` | Cluster status and management |
| `Health()` | Health checks and probes |
| `Seal()` | Seal/unseal operations |
| `KMS()` | Key management service |
| `Secrets()` | Secret storage with versioning |
| `Crypto()` | Encryption/decryption as a service |
| `Vector()` | Vector similarity search |
| `TimeSeries()` | Time-series data storage |
| `Backup()` | Backup and restore |

## Configuration Options

| Option | Description |
|--------|-------------|
| `WithTimeout(d)` | Request timeout |
| `WithTLS(cfg)` | mTLS configuration |
| `WithTLSConfig(cfg)` | Custom `*tls.Config` |
| `WithInsecure()` | Disable TLS (development only) |
| `WithConnectionPool(min, max)` | Connection pool size |
| `WithRetry(max, backoff)` | Retry configuration |
| `WithREST()` | Use REST transport |
| `WithQUIC()` | Use QUIC transport |
| `WithMCP()` | Use MCP transport (JSON-RPC 2.0 over HTTP) |
| `WithMCPCredentials(user, pass)` | Auto-authenticate MCP transport |
| `WithMCPAuthToken(token)` | Pre-existing token for MCP transport |
| `WithEmbedded(svc)` | Use embedded in-process transport |

## Examples

The SDK includes runnable examples demonstrating common use cases:

| Example | Description | Location |
|---------|-------------|----------|
| **Vector Search** | Semantic search, hybrid queries, k-nearest neighbors | `examples/vector/main.go` |
| **Time-Series** | Metrics storage, aggregations, downsampling | `examples/timeseries/main.go` |
| **KMS** | Key generation, rotation, encryption | `examples/client/kms/main.go` |
| **Secrets** | Secret storage with versioning | `examples/client/secrets/main.go` |
| **IAM** | User, role, and policy management | `examples/client/iam/main.go` |
| **ABAC** | Attribute-based access control | `examples/client/abac/main.go` |
| **Watch** | Real-time key change notifications | `examples/watch/simple/main.go` |
| **MVCC** | Multi-version concurrency control | `examples/mvcc/basic/main.go` |

### Running Examples

```bash
# Vector search example
go run examples/vector/main.go

# With different transport
go run examples/vector/main.go -transport rest -address http://localhost:8080

# Time-series example
go run examples/timeseries/main.go

# With QUIC transport
go run examples/timeseries/main.go -transport quic -address localhost:50052
```

See [examples.md](examples.md) for complete code examples and detailed documentation.

## Documentation

- [Architecture](architecture.md) - Transport abstraction and client design
- [Authentication](authentication.md) - mTLS setup and configuration
- [API Reference](api-reference.md) - Complete API documentation
- [Configuration](configuration.md) - All configuration options
- [Examples](examples.md) - Code examples for common use cases
- [Roadmap](ROADMAP.md) - Development progress and status

## See Also

- [CLI Documentation](../../cli/README.md) - Command-line interface
- [KMS Documentation](../../kms/README.md) - Key Management Service
- [Secrets Documentation](../../secrets/README.md) - Secrets Engine
