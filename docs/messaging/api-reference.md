# Push API Reference

## Overview

The Push system provides real-time notification capabilities for QuicBaaS with pub/sub messaging, delta publishing, and database change watching. Clients can subscribe to channels, receive notifications on data changes, and efficiently synchronize state using delta-only updates.

## Supported Protocols

| Protocol | Transport | Port | Use Case | Status |
|----------|-----------|------|----------|--------|
| REST | HTTP/HTTPS | 8443 | Web clients, external systems | Planned |
| gRPC | TCP | 10443 | High-performance service-to-service | Planned |
| QUIC | UDP | 9444 | Low-latency, mobile clients | Planned |
| MCP | stdio/HTTP | 9445 | AI/LLM integration | Planned |
| Unix | Unix Socket | /run/dragond.sock | Local IPC, CLI | Planned |
| WebSocket | HTTP Upgrade | 8443 | Real-time bidirectional | Phase 2 |

## Core Operations

### CreateChannel

Create a notification channel for publishing messages.

```
CreateChannel(name string, opts ChannelOptions) (*Channel, error)
```

**Parameters:**
- `name` - Unique channel identifier
- `opts` - Channel configuration (TTL, max subscribers, persistence)

### DeleteChannel

Delete a channel and disconnect all subscribers.

```
DeleteChannel(name string) error
```

**Parameters:**
- `name` - Channel identifier to delete

### Subscribe

Subscribe to a channel to receive notifications.

```
Subscribe(channel string, opts SubscribeOptions) (*Subscription, error)
```

**Parameters:**
- `channel` - Channel name to subscribe to
- `opts` - Subscription options (filters, replay from offset)

### Unsubscribe

Remove a subscription.

```
Unsubscribe(subscriptionID string) error
```

**Parameters:**
- `subscriptionID` - Subscription identifier returned from Subscribe

### Publish

Publish a message to a channel.

```
Publish(channel string, payload []byte, opts PublishOptions) error
```

**Parameters:**
- `channel` - Target channel name
- `payload` - Message payload
- `opts` - Publish options (priority, TTL, acknowledgment)

### PublishDelta

Publish only the differences between two entity states.

```
PublishDelta(channel string, old, new interface{}, opts DeltaOptions) error
```

**Parameters:**
- `channel` - Target channel name
- `old` - Previous entity state
- `new` - Current entity state
- `opts` - Delta options (format, compression)

### WatchChannel

Watch database key changes and receive notifications.

```
WatchChannel(keyPrefix string, opts WatchOptions) (*Watcher, error)
```

**Parameters:**
- `keyPrefix` - Key prefix to watch for changes
- `opts` - Watch options (include values, revision start)

## Message Types

### Client Messages

| Type | Direction | Description |
|------|-----------|-------------|
| `subscribe` | Client -> Server | Subscribe to a channel |
| `unsubscribe` | Client -> Server | Unsubscribe from a channel |
| `publish` | Client -> Server | Publish a message |
| `ping` | Client -> Server | Keep-alive ping |
| `ack` | Client -> Server | Acknowledge message receipt |

### Server Messages

| Type | Direction | Description |
|------|-----------|-------------|
| `notification` | Server -> Client | Push notification with payload |
| `pong` | Server -> Client | Response to ping |
| `error` | Server -> Client | Error response |

### Payload Categories

| Category | Description |
|----------|-------------|
| `data` | Application data payload |
| `control` | System control messages |
| `heartbeat` | Connection health check |

## Protocol-Specific Documentation

Detailed API documentation for each protocol:

- [REST API Reference](api-reference-rest.md) - HTTP endpoints and payloads
- [gRPC API Reference](api-reference-grpc.md) - Service definitions and streaming
- [QUIC API Reference](api-reference-quic.md) - Low-latency transport usage
- [MCP API Reference](api-reference-mcp.md) - AI/LLM tool integration
- [Unix Socket API Reference](api-reference-unix.md) - Local IPC operations

## Error Codes

| Code | Description |
|------|-------------|
| `CHANNEL_NOT_FOUND` | Channel does not exist |
| `CHANNEL_EXISTS` | Channel already exists |
| `SUBSCRIPTION_NOT_FOUND` | Subscription ID invalid |
| `MAX_SUBSCRIBERS` | Channel subscriber limit reached |
| `INVALID_PAYLOAD` | Message payload invalid |
| `UNAUTHORIZED` | Insufficient permissions |
| `RATE_LIMITED` | Too many requests |

## Related Documentation

- [Architecture](architecture.md) - Push system design
- [Configuration](configuration.md) - Server configuration
- [CLI Usage](cli-usage.md) - Command-line operations
