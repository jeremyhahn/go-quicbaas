# QRDB Transport Configuration

QuicBaaS delegates key-value, batch, transaction, MVCC, index, time-series, and vector operations to the qrdb engine via the qrdb SDK client. By default, the SDK uses an embedded (in-process) transport with zero network overhead. For deployments where qrdb runs as a separate service, a network transport can be configured.

## Configuration

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

## Transport Factory

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
                                    +-> networkTransportHandlers[type](opts...)
                                    +-> qrdbsdk.NewClient(transport)
```

## Transport Types

| Transport | Value | Description |
|-----------|-------|-------------|
| Embedded | `embedded` | In-process calls. Zero overhead. Default. |
| gRPC | `grpc` | gRPC to a remote qrdb server |
| REST | `rest` | REST/HTTP to a remote qrdb server |
| QUIC | `quic` | QUIC to a remote qrdb server |
| MCP | `mcp` | Model Context Protocol transport |
| Unix | `unix` | Unix domain socket transport |

## When to Use Each Transport

| Transport | Best For | Latency | Throughput | Notes |
|-----------|----------|---------|------------|-------|
| **Embedded** | Single-process deployments, testing | Lowest (no network) | Highest | Default. Zero serialization overhead. |
| **gRPC** | Production clusters, service meshes | Low | High | Connection pooling, mTLS, streaming support. |
| **REST** | HTTP integrations, browser clients, debugging | Medium | Medium | JSON serialization. Easy to inspect with curl. |
| **QUIC** | Low-latency WAN, mobile clients | Low | High | UDP-based, TLS 1.3 built-in, multiplexed streams. |
| **Unix** | Same-host IPC, sidecar deployments | Very low | High | No TCP overhead. File permission-based security. |
| **MCP** | AI/LLM tool integrations | Medium | Medium | JSON-RPC 2.0. Maps service/method to tool calls. |

## Per-Transport Configuration

### Embedded (Default)

No configuration needed. The embedded transport is used when `qrdb.transport` is unset or `"embedded"`.

```yaml
qrdb:
  transport: embedded
```

No `address` or TLS fields are required. The factory creates per-shard SDK clients that dispatch directly to the service layer via in-process function calls with no serialization.

### gRPC

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

### REST

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

### QUIC

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

### Unix Socket

```yaml
qrdb:
  transport: unix
  unix_socket_path: /var/run/qrdb.sock
```

| Field | Required | Description |
|-------|----------|-------------|
| `unix_socket_path` | Yes | Path to the Unix domain socket |

The `address` field is not used. The socket path is passed to the transport as the address. Security relies on file system permissions.

### MCP (Model Context Protocol)

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

## Environment Variables

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

## Architecture

For the embedded transport, the `ShardedClient` creates a per-shard qrdb SDK client. Each shard gets its own service wrappers that route Raft proposals through the host's `SyncPropose`.

For network transports, a single shared client is created because the remote qrdb server handles shard routing internally. This simplifies the client-side logic and reduces connection overhead.

```
Embedded:  QuicBaaS -> ShardedClient -> [per-shard embedded SDK client] -> service layer -> Raft
Network:   QuicBaaS -> ShardedClient -> [shared network SDK client] -> remote qrdb server
```

## Error Types

| Error | Cause |
|-------|-------|
| `UnixTransportMissingSocketError` | Unix transport configured without `unix_socket_path` |
| `NetworkTransportMissingAddressError` | Network transport configured without `address` |
| `UnsupportedTransportError` | Unknown transport type in config |
| `TransportCreationError` | Transport constructor or SDK client creation failed |

## See Also

- [SDK Architecture](architecture.md) -- Transport interface and client patterns
- [BaaS Migration](../../architecture/baas-migration.md) -- Why all operations route through the SDK
