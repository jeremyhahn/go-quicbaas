# QuicBaaS Go SDK

Official Go client SDK for QuicBaaS - a distributed, encrypted key-value database with built-in KMS, secrets management, and AI capabilities.

## Installation

```bash
go get github.com/jeremyhahn/go-quicbaas/sdk/go
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    // Connect to QuicBaaS via Unix socket
    client, err := quicbaas.NewClient("/var/run/quicbaas.sock")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Put a key-value pair
    ctx := context.Background()
    _, err = client.KV().Put(ctx, &quicbaas.PutRequest{
        EntityType: "users",
        Key:        "user-123",
        Value:      []byte(`{"name": "Alice", "email": "alice@example.com"}`),
    })
    if err != nil {
        log.Fatal(err)
    }

    // Get the value back
    resp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
        EntityType: "users",
        Key:        "user-123",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Retrieved: %s", string(resp.Value))
}
```

## Available Services

The SDK provides unified access to all QuicBaaS services:

### Core Data Operations
- **KV**: Key-value storage with Watch support
- **Batch**: Atomic batch operations
- **Transaction**: Compare-and-swap transactions
- **Query**: Secondary index queries

### Cluster Management
- **Cluster**: Cluster status and node management
- **Health**: Health checks and probes
- **Operations**: Cluster operations (migration, rebalancing, upgrades)
- **Cache**: Cache management and statistics

### Security Services
- **Auth**: Authentication management
- **Seal**: Seal/unseal operations
- **KMS**: Key management service
- **Secrets**: Secret storage with versioning
- **Crypto**: Encryption/decryption as a service
- **RBAC**: Role-based access control
- **DynamicSecrets**: Dynamic credential generation
- **Certificate**: Certificate management
- **ClusterDEK**: Cluster data encryption key management

### Identity & Access
- **IAM**: Identity and access management with multi-tenancy

### Specialized Storage
- **Vector**: Vector similarity search
- **TimeSeries**: Time-series data storage
- **ObjectStorage**: S3-compatible object storage
- **ObjectStorageAdmin**: Object storage administration
- **ObjectTransfer**: Optimized object transfer operations
- **ParameterStore**: Hierarchical parameter management

### Async & Scheduling
- **Scheduler**: Job scheduling and execution
- **Messaging**: Pub/sub messaging and push notifications

### AI/ML Integration
- **Embedding**: Embedding generation (Voyage, OpenAI, Ollama, LMStudio)
- **Ollama**: Direct Ollama integration
- **RAG**: Retrieval-augmented generation

### Backup & Recovery
- **Backup**: Backup and restore operations

## Transport Options

The SDK supports multiple transport protocols:

### gRPC (Default)
```go
client, err := quicbaas.NewClient("localhost:50051")
```

### REST/HTTP
```go
client, err := quicbaas.NewClient("http://localhost:8080", quicbaas.WithREST())
```

### QUIC
```go
client, err := quicbaas.NewClient("localhost:50052", quicbaas.WithQUIC())
```

### Unix Socket
```go
client, err := quicbaas.NewClient("/var/run/quicbaas.sock")
// Or explicitly:
client, err := quicbaas.NewClient("localhost", quicbaas.WithUnix("/var/run/quicbaas.sock"))
```

### Embedded (In-Process)
```go
cfg := quicbaas.EmbeddedServerConfig()
srv, err := quicbaas.NewServer(cfg)
if err != nil {
    log.Fatal(err)
}
if err := srv.Start(); err != nil {
    log.Fatal(err)
}
defer srv.Shutdown(context.Background())

client, err := quicbaas.NewClient("", quicbaas.WithEmbedded(srv.Service()))
```

### MCP (Model Context Protocol)
```go
client, err := quicbaas.NewClient("http://localhost:8080",
    quicbaas.WithMCP(),
    quicbaas.WithMCPCredentials("username", "password"),
)
```

## TLS Configuration

QuicBaaS uses mTLS for authentication:

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "client.crt",
        KeyFile:  "client.key",
        CAFile:   "ca.crt",
    }),
)
```

## Advanced Features

### Connection Pooling
```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithConnectionPool(5, 50), // min, max connections
)
```

### Retry Configuration
```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithRetry(3, 100*time.Millisecond),
)
```

### Watch for Changes
```go
watcher, err := client.KV().WatchPrefix(ctx, "users/")
if err != nil {
    log.Fatal(err)
}
defer watcher.Close()

for event := range watcher.Events() {
    log.Printf("Event: %v %s = %s", event.Type, event.Key, string(event.Value))
}
```

### Mounting your own ServiceModule

A host that embeds a QuicBaaS node can mount its own capability plane on that
node — served over gRPC, unix, REST and MCP under the node's single authorizer,
alongside the `quicbaas` and `xkms` modules:

```go
type deviceModule struct{ quicbaas.BaseHostModule }

func (m *deviceModule) Name() string { return "trusted-platform" }

func (m *deviceModule) RegisterHTTP(r *quicbaas.ModuleHTTPRegistrar) {
    r.Handle(http.MethodGet, "/api/v1/devices", quicbaas.Permission("device:read"), m.list)
}

srv, err := quicbaas.NewServer(cfg, quicbaas.WithModule(&deviceModule{}))
```

`quicbaas.WithModule` is the only supported shape — the SDK exposes no qrdb
server constructor, because composing the node is go-quicbaas's job. Note that
modules do **not** reach QUIC or the embedded in-process transport. See
[docs/go-sdk/api.md](../../docs/go-sdk/api.md) § *Mounting a host
ServiceModule*.

## More Information

For detailed documentation, examples, and the server implementation, visit the main repository:

https://github.com/jeremyhahn/go-quicbaas

## License

Dual-licensed under AGPL-3.0 and commercial license. See LICENSE file for details.
