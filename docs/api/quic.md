# QUIC API

High-performance binary protocol over QUIC transport for QuicBaaS.

## Overview

The QUIC API provides a high-performance binary protocol built on QUIC transport (RFC 9000). It offers:

- **Low latency**: 0-RTT connection establishment for returning clients
- **Stream multiplexing**: Concurrent operations without head-of-line blocking
- **Connection migration**: Seamless handling of network changes (ideal for mobile clients)
- **Built-in TLS 1.3**: Mandatory encryption with modern security
- **Efficient binary encoding**: JSON payloads with compact wire format

## Default Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| Listen Address | `0.0.0.0:4433` | UDP port for QUIC |
| Max Streams | 1000 | Maximum concurrent streams per connection |
| Max Idle Timeout | 30s | Connection idle timeout |
| Keep-Alive Period | 15s | Keep-alive ping interval |
| Handshake Timeout | 5s | TLS handshake timeout |
| Max Payload Size | 16MB | Maximum request/response payload |

## Wire Protocol

### Request Format

```
[opcode:1][tokenLen:2][token:N][payloadLen:4][payload:M]
```

| Field | Size | Description |
|-------|------|-------------|
| opcode | 1 byte | Operation code (see Operations) |
| tokenLen | 2 bytes | Authentication token length (big-endian) |
| token | N bytes | JWT or API token (max 8KB) |
| payloadLen | 4 bytes | Payload length (big-endian) |
| payload | M bytes | JSON-encoded request data |

### Response Format

```
[status:1][payloadLen:4][payload:N]
```

| Field | Size | Description |
|-------|------|-------------|
| status | 1 byte | Status code (see Status Codes) |
| payloadLen | 4 bytes | Payload length (big-endian) |
| payload | N bytes | JSON-encoded response data |

## Status Codes

| Code | Value | Description |
|------|-------|-------------|
| OK | 0x00 | Success |
| INVALID_REQUEST | 0x01 | Malformed request |
| NOT_FOUND | 0x02 | Entity not found |
| UNAUTHORIZED | 0x03 | Authentication required |
| INTERNAL_ERROR | 0x04 | Server error |
| TIMEOUT | 0x05 | Request timeout |
| ALREADY_EXISTS | 0x06 | Entity already exists |
| PERMISSION_DENIED | 0x07 | Authorization failed |
| RESOURCE_EXHAUSTED | 0x08 | Rate limit exceeded |
| INVALID_ARGUMENT | 0x09 | Invalid parameters |
| UNAVAILABLE | 0x0A | Service unavailable |

## Authentication

Include a JWT token in the request token field. Tokens are obtained via the Login operation.

```go
req := &quic.Request{
    Opcode:  quic.OpGet,
    Token:   "eyJhbGciOiJIUzI1NiIs...",
    Payload: payload,
}
```

Public operations that do not require authentication:
- `OpLogin` (0x60)
- `OpHealth` (0x90)
- `OpLiveness` (0x91)
- `OpReadiness` (0x92)

## Operations

### KV Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Put | 0x01 | Store key-value pair |
| Get | 0x02 | Retrieve value by key |
| Delete | 0x03 | Remove key-value pair |
| List | 0x04 | List entities by type |

### Batch Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| BatchPut | 0x10 | Store multiple entities |
| BatchGet | 0x11 | Retrieve multiple entities |
| BatchDelete | 0x12 | Delete multiple entities |

### Transaction Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Transaction | 0x20 | Atomic compare-and-swap |

### Query Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Query | 0x30 | Query by index |
| ScanIndex | 0x31 | Scan index range |

### Cluster Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| ClusterStatus | 0x40 | Get cluster status |
| GetLeader | 0x41 | Get shard leader |
| AddNode | 0x42 | Add node to shard |
| RemoveNode | 0x43 | Remove node from shard |
| CreateShard | 0x44 | Create new shard |
| RebalanceCluster | 0x45 | Initiate rebalancing |
| ListNodes | 0x46 | List cluster nodes |

### Health Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Health | 0x90 | Full health check |
| Liveness | 0x91 | Kubernetes liveness probe |
| Readiness | 0x92 | Kubernetes readiness probe |

### Auth Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| Login | 0x60 | Authenticate user |
| Logout | 0x61 | Invalidate session |
| RefreshToken | 0x62 | Refresh JWT token |
| ValidateToken | 0x63 | Validate token |

### KMS Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| KMSGenerateKey | 0xD0 | Generate new key |
| KMSImportKey | 0xD1 | Import external key |
| KMSGetKey | 0xD2 | Get key metadata |
| KMSListKeys | 0xD3 | List all keys |
| KMSRotateKey | 0xD4 | Rotate key version |
| KMSDeleteKey | 0xD5 | Schedule key deletion |
| KMSEncrypt | 0xD6 | Encrypt data |
| KMSDecrypt | 0xD7 | Decrypt data |

### Secrets Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| SecretsStore | 0xE0 | Store secret |
| SecretsGet | 0xE1 | Retrieve secret |
| SecretsList | 0xE2 | List secrets |
| SecretsDelete | 0xE3 | Delete secret |
| SecretsListVersions | 0xE4 | List secret versions |
| SecretsRollback | 0xE5 | Rollback to version |
| SecretsRotate | 0xE6 | Rotate secret value |

### Seal Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| SealStatus | 0xC0 | Get seal status |
| SealInit | 0xC1 | Initialize seal |
| Unseal | 0xC2 | Provide unseal key |
| Seal | 0xC3 | Re-seal database |
| SealRekey | 0xC4 | Regenerate key shares |

### Vector Operations

| Operation | Opcode | Description |
|-----------|--------|-------------|
| VectorCreateNamespace | 0x16 | Create vector namespace |
| VectorDeleteNamespace | 0x17 | Delete namespace |
| VectorListNamespaces | 0x18 | List namespaces |
| VectorUpsert | 0x1A | Insert/update vector |
| VectorGet | 0x1B | Get vector by ID |
| VectorDelete | 0x1C | Delete vector |
| VectorSearchSimilar | 0x1E | Similarity search |
| VectorHybridSearch | 0x21 | Hybrid search |

## Server Configuration

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"

// TLS configuration (required for QUIC)
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    NextProtos:   []string{"quicbaas-quic"},
    MinVersion:   tls.VersionTLS13,
}

// Server configuration
config := &quic.Config{
    ListenAddr:             "0.0.0.0:4433",
    TLSConfig:              tlsConfig,
    MaxStreams:             1000,
    MaxIdleTimeout:         30 * time.Second,
    MaxIncomingStreams:     1000,
    KeepAlivePeriod:        15 * time.Second,
    HandshakeIdleTimeout:   5 * time.Second,
    MaxReceiveBufferSize:   1 * 1024 * 1024,
    InitialStreamReceiveWindow: 512 * 1024,
    MaxStreamReceiveWindow: 6 * 1024 * 1024,
    EnableMetrics:          true,
}

// Create server with authorization
server, err := quic.NewServer(service, config, logger,
    quic.WithAuthManager(authManager),
    quic.WithAuthorizationService(authzService),
)
if err != nil {
    log.Fatal(err)
}

// Start server
if err := server.Start(); err != nil {
    log.Fatal(err)
}
defer server.Stop()
```

## Client Usage

### Direct Client

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"

// Create client with TLS
client, err := quic.NewClient("localhost:4433",
    quic.WithTLSConfig(tlsConfig),
    quic.WithTimeout(10*time.Second),
    quic.WithMaxIdleTimeout(30*time.Second),
)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// Put operation
putResp, err := client.Put(ctx, &service.PutRequest{
    EntityType:  "user",
    Id:          1,
    Data:        []byte(`{"name":"John","email":"john@example.com"}`),
    Consistency: "quorum",
})

// Get operation
getResp, err := client.Get(ctx, &service.GetRequest{
    EntityType: "user",
    Id:         1,
})

// KMS operations
keyResp, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
    Name:      "my-key",
    Algorithm: "AES256",
    Purpose:   "ENCRYPT_DECRYPT",
})

// Secrets operations
secretResp, err := client.StoreSecret(ctx, &service.StoreSecretRequest{
    Path:  "app/database/password",
    Value: []byte("secret-password"),
})
```

### SDK Transport

```go
import (
    "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
    quictransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
)

// Create QUIC transport
cfg := &transport.Config{
    Address:   "localhost:4433",
    TLSConfig: tlsConfig,
    Timeout:   10 * time.Second,
}

t, err := quictransport.New(cfg,
    quictransport.WithHandshakeTimeout(10*time.Second),
    quictransport.WithMaxIdleTimeout(30*time.Second),
    quictransport.WithKeepAlivePeriod(15*time.Second),
    quictransport.WithAuthToken("eyJhbGciOiJIUzI1NiIs..."),
    quictransport.WithEnable0RTT(true),
)
if err != nil {
    log.Fatal(err)
}
defer t.Close()

// Send request
resp, err := t.Request(ctx, "QuicBaaS", "Get", &GetRequest{
    EntityType: "user",
    Id:         1,
})
```

## TLS Configuration

QUIC requires TLS 1.3. Configure certificates for production:

```go
// Load server certificate
cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
if err != nil {
    log.Fatal(err)
}

// Server TLS config
serverTLS := &tls.Config{
    Certificates: []tls.Certificate{cert},
    NextProtos:   []string{"quicbaas-quic"},
    MinVersion:   tls.VersionTLS13,
    ClientAuth:   tls.RequireAndVerifyClientCert, // mTLS
    ClientCAs:    caCertPool,
}

// Client TLS config
clientTLS := &tls.Config{
    RootCAs:      caCertPool,
    Certificates: []tls.Certificate{clientCert}, // mTLS
    NextProtos:   []string{"quicbaas-quic"},
    MinVersion:   tls.VersionTLS13,
    ServerName:   "quicbaas.example.com",
}
```

For development/testing only:

```go
client, err := quic.NewClient("localhost:4433",
    quic.WithInsecure(),
)
```

## Error Handling

```go
resp, err := client.Get(ctx, req)
if err != nil {
    var dragonErr *errors.DragonError
    if errors.As(err, &dragonErr) {
        switch dragonErr.Code {
        case errors.ErrNotFound:
            // Handle not found
        case errors.ErrUnauthorized:
            // Handle auth error
        case errors.ErrTimeout:
            // Handle timeout
        default:
            // Handle other errors
        }
    }
}
```

## Performance Tuning

### Connection Settings

```go
config := &quic.Config{
    // Increase for high-throughput workloads
    MaxStreams:             2000,
    MaxIncomingStreams:     2000,

    // Increase buffer sizes for large payloads
    MaxReceiveBufferSize:   4 * 1024 * 1024,  // 4MB
    InitialStreamReceiveWindow: 1 * 1024 * 1024, // 1MB
    MaxStreamReceiveWindow: 16 * 1024 * 1024,    // 16MB

    // Adjust timeouts for network conditions
    MaxIdleTimeout:         60 * time.Second,
    KeepAlivePeriod:        20 * time.Second,
}
```

### 0-RTT Connection Resumption

Enable 0-RTT for reduced latency on reconnection:

```go
t, err := quictransport.New(cfg,
    quictransport.WithEnable0RTT(true),
)
```

Note: 0-RTT data may be replayed by an attacker. Only use for idempotent operations.

## Metrics

The server exposes metrics via `GetMetrics()`:

```go
metrics := server.GetMetrics()
// {
//   "active_connections": 150,
//   "active_streams": 1200,
//   "total_requests": 5000000,
//   "total_errors": 1234,
//   "running": true
// }
```

## See Also

- [gRPC API](grpc.md) - High-performance binary protocol (alternative)
- [REST API](rest.md) - HTTP/JSON interface
- [Architecture](../architecture/README.md) - System design
