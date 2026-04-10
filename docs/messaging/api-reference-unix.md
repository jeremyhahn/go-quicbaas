# Unix Socket API Reference

## Overview

QuicBaaS exposes its full gRPC messaging API over Unix domain sockets (UDS) for local inter-process communication. The Unix socket transport is the **default** listener and the preferred method for CLI and local service communication. It provides the same `MessagingService` API as the TCP gRPC transport but eliminates network overhead by using the kernel's socket buffer for IPC.

All gRPC services registered on the server (including `MessagingService`, `QuicBaaS`, `KeyManagement`, `Secrets`, etc.) are available over the Unix socket listener. There is no separate protocol -- the transport is standard gRPC over a `unix://` connection.

## Configuration

The Unix socket listener is configured under `server.unix` in the QuicBaaS configuration file.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `server.unix.enabled` | bool | `true` | Enable the Unix socket listener |
| `server.unix.socket` | string | `/var/run/quicbaas.sock` | Absolute path to the socket file |

**Default configuration (`DefaultConfig`):**
```yaml
server:
  unix:
    enabled: true
    socket: /var/run/quicbaas.sock
```

**Development configuration (`DevelopmentConfig`):**
```yaml
server:
  unix:
    enabled: true
    socket: /tmp/quicbaas.sock
```

**Production configuration (`ProductionConfig`):**
```yaml
server:
  unix:
    enabled: true
    socket: /var/run/quicbaas.sock
```

The socket directory is created automatically with `0750` permissions. The socket file is created with `0660` permissions (owner and group read/write). On startup, any stale socket file from a previous unclean shutdown is removed before binding.

## Connection Setup

### Go SDK

The QuicBaaS gRPC client auto-detects Unix socket addresses. Any address ending in `.sock` or prefixed with `unix://` is routed through the Unix transport.

```go
import grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"

// Connect via absolute socket path (auto-detected)
client, err := grpcapi.NewClient("/var/run/quicbaas.sock",
    grpcapi.WithInsecure(),
)

// Connect via explicit gRPC URI format
client, err := grpcapi.NewClient("unix:///var/run/quicbaas.sock",
    grpcapi.WithInsecure(),
)

// Connect via relative path (resolved to absolute)
client, err := grpcapi.NewClient("./quicbaas.sock",
    grpcapi.WithInsecure(),
)
```

Address detection logic (`IsUnixSocketAddress`):
- `unix:///path/to/socket` -- standard gRPC URI format
- `unix-abstract://name` -- Linux abstract sockets
- `/path/to/file.sock` -- absolute path ending in `.sock`
- `./path/to/file.sock` -- relative path ending in `.sock`

All other addresses are treated as TCP `host:port`.

### grpcurl

```bash
# List available services
grpcurl -plaintext -unix /var/run/quicbaas.sock list

# List MessagingService RPCs
grpcurl -plaintext -unix /var/run/quicbaas.sock \
  list quicbaas.messaging.v1.MessagingService

# Create a channel
grpcurl -plaintext -unix /var/run/quicbaas.sock \
  -d '{"name":"orders","buffer_size":256}' \
  quicbaas.messaging.v1.MessagingService/CreateChannel

# Publish a message
grpcurl -plaintext -unix /var/run/quicbaas.sock \
  -d '{"channel":"orders","payload":"eyJvcmRlcl9pZCI6Im9yZF8wMDEifQ=="}' \
  quicbaas.messaging.v1.MessagingService/Publish

# Stream notifications (server-streaming)
grpcurl -plaintext -unix /var/run/quicbaas.sock \
  -d '{"subscription_id":"sub_abc123"}' \
  quicbaas.messaging.v1.MessagingService/StreamNotifications

# Get messaging stats
grpcurl -plaintext -unix /var/run/quicbaas.sock \
  quicbaas.messaging.v1.MessagingService/GetStats
```

### quicbaas CLI

The CLI defaults to the Unix socket transport. The `--address` flag accepts a socket path directly.

```bash
# Uses default socket /var/run/quicbaas.sock
quicbaas msg create-channel orders

# Explicit socket path
quicbaas msg create-channel orders --address /tmp/quicbaas.sock

# Publish via Unix socket
quicbaas msg publish orders --payload '{"order_id":"ord_001"}'

# Watch a channel
quicbaas msg watch orders

# Get service stats
quicbaas msg dedup-stats orders
```

**Environment variable override:**
```bash
export QUICBAAS_SERVER=/tmp/quicbaas.sock
export QUICBAAS_PROTOCOL=unix
quicbaas msg list-channels
```

## MessagingService RPCs

The full `MessagingService` is available over the Unix socket. The protobuf service definition is at `api/proto/messaging.proto` under package `quicbaas.messaging.v1`.

### Channel Management

| RPC | Description |
|-----|-------------|
| `CreateChannel` | Create a notification channel |
| `DeleteChannel` | Delete a channel and disconnect subscribers |
| `GetChannel` | Retrieve channel metadata |
| `ListChannels` | List channels with pagination and prefix filter |

### Subscriptions

| RPC | Description |
|-----|-------------|
| `Subscribe` | Subscribe to a channel with optional filters and delta mode |
| `Unsubscribe` | Remove a subscription by ID |

### Publishing

| RPC | Description |
|-----|-------------|
| `Publish` | Publish a message with optional dedup key |
| `PublishDelta` | Publish a computed delta between old and new state |

### Streaming

| RPC | Description |
|-----|-------------|
| `StreamNotifications` | Server-streaming RPC delivering real-time notifications |

### Watch Integration

| RPC | Description |
|-----|-------------|
| `WatchChannel` | Create a subscription watching key-prefix changes |

### Statistics

| RPC | Description |
|-----|-------------|
| `GetStats` | Service-wide messaging statistics |
| `GetDedupStats` | Per-channel deduplication statistics |
| `GetChannelStats` | Per-channel message and subscriber stats |

## Security

Unix socket file permissions provide OS-level access control. The socket is created with mode `0660`, restricting access to the file owner and group. No TLS handshake is required for local connections since traffic never leaves the host kernel, though TLS credentials are still accepted by the gRPC server if configured.

For production deployments, ensure the QuicBaaS process runs under a dedicated user and group, and add local client processes to that group.

## Performance

Unix domain sockets bypass the TCP/IP stack entirely, avoiding:
- TCP handshake and teardown overhead
- Nagle algorithm buffering
- IP routing and checksum computation
- Loopback interface processing

This makes UDS the lowest-latency transport for same-host communication, typically achieving sub-100 microsecond round-trip times for small messages. Use this transport for CLI tools, sidecar proxies, and co-located services.

## Related Documentation

- [API Reference](api-reference.md) - Messaging API overview
- [CLI Reference](cli-reference.md) - Command-line messaging operations
- [Go SDK](sdk-go.md) - Go client library usage
- [Architecture](architecture.md) - Messaging system design
