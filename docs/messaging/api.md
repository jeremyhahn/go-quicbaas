# Messaging — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/messaging/`.



## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.Messaging` — SDK package `sdk/go/client/messaging`

Dedicated transport handlers: rest=yes, grpc=no, mcp=yes; embedded routes registered: 0; MCP tools registered server-side: 10.

MCP tools: `quicbaas_msg_create_channel`, `quicbaas_msg_dedup_stats`, `quicbaas_msg_delete_channel`, `quicbaas_msg_get_channel`, `quicbaas_msg_list_channels`, `quicbaas_msg_publish`, `quicbaas_msg_publish_delta`, `quicbaas_msg_stats`, `quicbaas_msg_subscribe`, `quicbaas_msg_unsubscribe`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `CreateChannel` | dedicated handler (path not statically recoverable) | ? | MISSING | yes | MISSING | — |
| `DeleteChannel` | `DELETE /api/v1/messaging/channels/{name}` | yes | MISSING | yes | MISSING | `messaging:channel_delete` |
| `GetChannel` | `GET /api/v1/messaging/channels/{name}` | yes | MISSING | yes | MISSING | `messaging:channel_read` |
| `GetChannelStats` | `GET /api/v1/messaging/stats/{channel}` | yes | MISSING | yes | MISSING | `messaging:stats` |
| `GetDedupStats` | `GET /api/v1/messaging/dedup/stats/{channel}` | yes | MISSING | yes | MISSING | `messaging:stats` |
| `GetStats` | dedicated handler (path not statically recoverable) | ? | MISSING | yes | MISSING | — |
| `ListChannels` | dedicated handler (path not statically recoverable) | ? | MISSING | yes | MISSING | — |
| `Publish` | `POST /api/v1/messaging/publish/{channel}` | yes | MISSING | yes | MISSING | `messaging:publish` |
| `PublishDelta` | `POST /api/v1/messaging/publish-delta/{channel}` | yes | MISSING | yes | MISSING | `messaging:publish` |
| `Subscribe` | dedicated handler (path not statically recoverable) | ? | MISSING | yes | MISSING | — |
| `Unsubscribe` | `DELETE /api/v1/messaging/subscribe/{id}` | yes | MISSING | yes | MISSING | `messaging:unsubscribe` |
| `WatchChannel` | dedicated handler (path not statically recoverable) | ? | MISSING | yes | MISSING | — |


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

- [REST API Reference](api.md) - HTTP endpoints and payloads
- [gRPC API Reference](api.md) - Service definitions and streaming
- [QUIC API Reference](api.md) - Low-latency transport usage
- [MCP API Reference](api.md) - AI/LLM tool integration
- [Unix Socket API Reference](api.md) - Local IPC operations

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
- Configuration - Server configuration
- CLI Usage - Command-line operations


---

## Messaging REST API Reference

### Base URL

```
/api/v1/messaging
```

### Authentication

All endpoints require a valid Bearer token in the `Authorization` header:

```
Authorization: Bearer <jwt-token>
```

### Endpoints

#### List Channels

List all messaging channels.

```
GET /channels
```

**Response (200 OK):**

```json
{
  "channels": [
    {
      "name": "orders",
      "codec": "json",
      "created_at": 1737331200,
      "subscribers": 5,
      "persistent": true,
      "max_history": 1000,
      "buffer_size": 256,
      "ttl_seconds": 3600
    }
  ],
  "total": 1,
  "timestamp": 1737331200
}
```

**Example:**

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  https://localhost:8443/api/v1/messaging/channels
```

---

#### Create Channel

Create a new messaging channel.

```
POST /channels
```

**Request Body:**

```json
{
  "name": "orders",
  "codec": "json",
  "persistent": true,
  "max_history": 1000,
  "buffer_size": 256,
  "max_subscribers": 100,
  "ttl_seconds": 3600
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique channel identifier |
| `codec` | string | No | Encoding format (e.g., `json`, `msgpack`) |
| `persistent` | bool | No | Enable message persistence |
| `max_history` | int | No | Maximum retained message count |
| `buffer_size` | int | No | Channel buffer capacity |
| `max_subscribers` | int | No | Maximum concurrent subscribers |
| `ttl_seconds` | int64 | No | Channel time-to-live in seconds |

**Response (201 Created):**

```json
{
  "channel": {
    "name": "orders",
    "codec": "json",
    "created_at": 1737331200,
    "subscribers": 0,
    "persistent": true,
    "max_history": 1000,
    "buffer_size": 256,
    "ttl_seconds": 3600
  },
  "timestamp": 1737331200
}
```

**Example:**

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"orders","persistent":true,"buffer_size":256}' \
  https://localhost:8443/api/v1/messaging/channels
```

---

#### Get Channel

Retrieve a channel by name.

```
GET /channels/{name}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `name` | string | Channel name (URL-encoded if needed) |

**Response (200 OK):**

```json
{
  "channel": {
    "name": "orders",
    "codec": "json",
    "created_at": 1737331200,
    "subscribers": 5,
    "persistent": true,
    "max_history": 1000,
    "buffer_size": 256,
    "ttl_seconds": 3600
  },
  "timestamp": 1737331200
}
```

**Example:**

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  https://localhost:8443/api/v1/messaging/channels/orders
```

---

#### Delete Channel

Delete a channel and disconnect all subscribers.

```
DELETE /channels/{name}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `name` | string | Channel name (URL-encoded if needed) |

**Response (200 OK):**

```json
{
  "name": "orders",
  "deleted": true,
  "timestamp": 1737331200
}
```

**Example:**

```bash
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://localhost:8443/api/v1/messaging/channels/orders
```

---

#### Subscribe

Subscribe to a messaging channel.

```
POST /subscribe
```

**Request Body:**

```json
{
  "channel": "orders",
  "session_id": "sess_abc123",
  "delta_mode": false,
  "include_old_values": false,
  "dedup_metadata": false,
  "dedup_skip_duplicates": false,
  "buffer_size": 128,
  "event_types": ["PUT", "DELETE"],
  "key_prefix": "user/"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `channel` | string | Yes | Channel name to subscribe to |
| `session_id` | string | No | Session identifier for resumable subscriptions |
| `delta_mode` | bool | No | Receive only delta (diff) payloads |
| `include_old_values` | bool | No | Include previous values in change events |
| `dedup_metadata` | bool | No | Attach deduplication metadata to messages |
| `dedup_skip_duplicates` | bool | No | Silently drop duplicate messages |
| `buffer_size` | int | No | Subscription buffer capacity |
| `event_types` | []string | No | Filter by event type: `PUT`, `DELETE` |
| `key_prefix` | string | No | Filter by key prefix |

**Response (201 Created):**

```json
{
  "subscription": {
    "id": "sub_7f3a9c2e",
    "channel": "orders",
    "client_id": "client_456",
    "created_at": 1737331200
  },
  "timestamp": 1737331200
}
```

**Example:**

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"channel":"orders","event_types":["PUT"],"key_prefix":"user/"}' \
  https://localhost:8443/api/v1/messaging/subscribe
```

---

#### Unsubscribe

Remove a subscription by ID.

```
DELETE /subscribe/{id}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | string | Subscription ID (URL-encoded if needed) |

**Response (200 OK):**

```json
{
  "subscription_id": "sub_7f3a9c2e",
  "unsubscribed": true,
  "timestamp": 1737331200
}
```

**Example:**

```bash
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://localhost:8443/api/v1/messaging/subscribe/sub_7f3a9c2e
```

---

#### Publish

Publish a message to a channel.

```
POST /publish/{channel}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `channel` | string | Target channel name (URL-encoded if needed) |

**Request Body:**

```json
{
  "payload": {"order_id": 123, "status": "shipped"},
  "message_id": "msg_unique_001",
  "dedup_key": "order-123-shipped",
  "codec": "json",
  "metadata": {
    "source": "order-service",
    "priority": "high"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `payload` | object/any | Yes | Message payload (arbitrary JSON) |
| `message_id` | string | No | Client-assigned message identifier |
| `dedup_key` | string | No | Deduplication key to prevent duplicates |
| `codec` | string | No | Payload encoding format |
| `metadata` | map[string]string | No | Arbitrary key-value metadata |

**Response (200 OK):**

```json
{
  "channel": "orders",
  "published": true,
  "timestamp": 1737331200
}
```

**Example:**

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"payload":{"order_id":123,"status":"shipped"},"dedup_key":"order-123-shipped"}' \
  https://localhost:8443/api/v1/messaging/publish/orders
```

---

#### Publish Delta

Publish a delta (diff) between two entity states. The server computes the difference and delivers only the changes to subscribers.

```
POST /publish-delta/{channel}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `channel` | string | Target channel name (URL-encoded if needed) |

**Request Body:**

```json
{
  "old": {"order_id": 123, "status": "pending", "items": 3},
  "new": {"order_id": 123, "status": "shipped", "items": 3, "tracking": "TRK-456"},
  "message_id": "delta_001",
  "dedup_key": "order-123-update",
  "codec": "json",
  "metadata": {
    "source": "order-service"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `old` | object/any | Yes | Previous entity state (arbitrary JSON) |
| `new` | object/any | Yes | Current entity state (arbitrary JSON) |
| `message_id` | string | No | Client-assigned message identifier |
| `dedup_key` | string | No | Deduplication key to prevent duplicates |
| `codec` | string | No | Payload encoding format |
| `metadata` | map[string]string | No | Arbitrary key-value metadata |

**Response (200 OK):**

```json
{
  "channel": "orders",
  "published": true,
  "timestamp": 1737331200
}
```

**Example:**

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "old": {"order_id": 123, "status": "pending"},
    "new": {"order_id": 123, "status": "shipped", "tracking": "TRK-456"}
  }' \
  https://localhost:8443/api/v1/messaging/publish-delta/orders
```

---

#### Get Stats

Retrieve messaging service aggregate statistics.

```
GET /stats
```

**Response (200 OK):**

```json
{
  "total_channels": 12,
  "total_subscriptions": 47,
  "messages_published": 150320,
  "messages_delivered": 892100,
  "dedup_hits": 1204,
  "dropped_messages": 3,
  "buffer_utilization": 0.42,
  "timestamp": 1737331200
}
```

| Field | Type | Description |
|-------|------|-------------|
| `total_channels` | int64 | Number of active channels |
| `total_subscriptions` | int64 | Number of active subscriptions |
| `messages_published` | int64 | Total messages published |
| `messages_delivered` | int64 | Total messages delivered to subscribers |
| `dedup_hits` | int64 | Messages suppressed by deduplication |
| `dropped_messages` | int64 | Messages dropped (e.g., buffer full) |
| `buffer_utilization` | float64 | Average buffer utilization (0.0 - 1.0) |

**Example:**

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  https://localhost:8443/api/v1/messaging/stats
```

---

#### Get Dedup Stats

Retrieve deduplication statistics for a specific channel.

```
GET /dedup/stats/{channel}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `channel` | string | Channel name (URL-encoded if needed) |

**Response (200 OK):**

```json
{
  "channel": "orders",
  "total_messages": 15032,
  "total_duplicates": 1204,
  "top_duplicates": [
    {
      "count": 47,
      "first_seen": 1737244800,
      "last_seen": 1737331100,
      "message_id": "msg_unique_001",
      "payload_hash": "a1b2c3d4e5f6"
    }
  ],
  "timestamp": 1737331200
}
```

| Field | Type | Description |
|-------|------|-------------|
| `channel` | string | Channel name |
| `total_messages` | int64 | Total messages processed on channel |
| `total_duplicates` | int64 | Total duplicates detected |
| `top_duplicates` | array | Most frequently duplicated messages |
| `top_duplicates[].count` | int64 | Number of times this message was seen |
| `top_duplicates[].first_seen` | int64 | Unix timestamp of first occurrence |
| `top_duplicates[].last_seen` | int64 | Unix timestamp of most recent occurrence |
| `top_duplicates[].message_id` | string | Message identifier |
| `top_duplicates[].payload_hash` | string | Hash of the message payload |

**Example:**

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  https://localhost:8443/api/v1/messaging/dedup/stats/orders
```

---

### Error Responses

All endpoints return a standard error envelope:

```json
{
  "error": "channel name is required",
  "code": "VALIDATION",
  "details": {},
  "timestamp": "2025-01-20T00:00:00Z",
  "request_id": "req_abc123",
  "retryable": false
}
```

#### HTTP Status Codes

| Status | Description |
|--------|-------------|
| 200 | Success |
| 201 | Created (channel or subscription) |
| 400 | Bad Request -- invalid JSON, missing required fields |
| 401 | Unauthorized -- missing or invalid token |
| 403 | Forbidden -- insufficient permissions |
| 404 | Not Found -- channel or subscription does not exist |
| 409 | Conflict -- channel already exists |
| 500 | Internal Server Error |
| 501 | Not Implemented -- messaging service not configured |
| 503 | Service Unavailable -- leader not available |

#### Common Error Conditions

| Condition | Status | Error Message |
|-----------|--------|---------------|
| Messaging not enabled | 501 | `messaging notification service is not configured` |
| Missing channel name | 400 | `channel name is required` |
| Missing subscription ID | 400 | `subscription ID is required` |
| Missing payload | 400 | `payload is required` |
| Missing old value (delta) | 400 | `old value is required` |
| Missing new value (delta) | 400 | `new value is required` |
| Invalid JSON body | 400 | `invalid JSON body` |
| Unreadable request body | 400 | `failed to read request body` |
| Dedup not enabled | 400 | `deduplication is not enabled` |
| Channel not found | 404 | Resource not found |
| Channel already exists | 409 | Conflict |

### Related Documentation

- Push API Reference -- Protocol-agnostic operations overview
- [Architecture](architecture.md) -- Push system design
- [Delta Publishing](architecture.md) -- Delta computation details
- [CLI Reference](cli.md) -- Command-line operations
- [Go SDK](engineering.md) -- Go client library


---

## gRPC Messaging API Reference

### Overview

The `MessagingService` gRPC API provides high-performance, real-time pub/sub messaging over TCP. It supports channel management, subscriptions with filtering, message publishing with deduplication, delta updates, key-change watching, and server-side streaming for live notifications.

- **Package:** `quicbaas.messaging.v1`
- **Proto:** `api/proto/messaging.proto`
- **Default Port:** `10443`

### Service Methods

#### Channel Management

##### CreateChannel

Create a notification channel.

```protobuf
rpc CreateChannel(MessagingCreateChannelRequest) returns (MessagingCreateChannelResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | **Required.** Unique channel identifier. Alphanumeric, `_-./:\*`, max 256 chars. |
| `codec` | `string` | Default codec for messages (`json`, `msgpack`, `protobuf`). |
| `persistent` | `bool` | Persist messages for replay. |
| `max_history` | `int32` | Max messages retained for replay. |
| `buffer_size` | `int32` | Message buffer size. |
| `max_subscribers` | `int32` | Max subscribers (0 = unlimited). |
| `ttl_seconds` | `int64` | Message time-to-live in seconds. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `channel` | `Channel` | Created channel metadata. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{"name":"events","codec":"json","buffer_size":1024}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/CreateChannel
```

---

##### DeleteChannel

Delete a channel and disconnect all its subscribers.

```protobuf
rpc DeleteChannel(MessagingDeleteChannelRequest) returns (MessagingDeleteChannelResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | **Required.** Channel to delete. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Deleted channel name. |
| `deleted` | `bool` | Whether deletion succeeded. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{"name":"events"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/DeleteChannel
```

---

##### GetChannel

Retrieve channel information.

```protobuf
rpc GetChannel(MessagingGetChannelRequest) returns (MessagingGetChannelResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | **Required.** Channel to retrieve. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `channel` | `Channel` | Channel metadata. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{"name":"events"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/GetChannel
```

---

##### ListChannels

List all notification channels with optional filtering and pagination.

```protobuf
rpc ListChannels(MessagingListChannelsRequest) returns (MessagingListChannelsResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `limit` | `int32` | Max channels to return. |
| `cursor` | `string` | Pagination cursor. |
| `name_prefix` | `string` | Filter by name prefix. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `channels` | `repeated Channel` | List of channels. |
| `next_cursor` | `string` | Next page cursor. |
| `has_more` | `bool` | More results available. |
| `total_count` | `int32` | Total matching channels. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext \
  localhost:10443 quicbaas.messaging.v1.MessagingService/ListChannels
```

---

#### Subscription Management

##### Subscribe

Subscribe to a channel to receive notifications.

```protobuf
rpc Subscribe(MessagingSubscribeRequest) returns (MessagingSubscribeResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `channel` | `string` | **Required.** Channel to subscribe to. |
| `session_id` | `string` | Optional session ID for tracking. |
| `filter` | `SubscriptionFilter` | Event type and key prefix filters. |
| `delta_mode` | `bool` | Enable delta-only updates. |
| `include_old_values` | `bool` | Include previous values in delta payloads. |
| `dedup_metadata` | `bool` | Include deduplication metadata. |
| `dedup_skip_duplicates` | `bool` | Automatically skip duplicate messages. |
| `buffer_size` | `int32` | Subscription buffer size. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `subscription` | `Subscription` | Created subscription with ID. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{"channel":"events","delta_mode":true}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/Subscribe
```

---

##### Unsubscribe

Remove a subscription.

```protobuf
rpc Unsubscribe(MessagingUnsubscribeRequest) returns (MessagingUnsubscribeResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `subscription_id` | `string` | **Required.** Subscription to remove. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `subscription_id` | `string` | Removed subscription ID. |
| `unsubscribed` | `bool` | Whether removal succeeded. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{"subscription_id":"sub-1"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/Unsubscribe
```

---

#### Publishing

##### Publish

Publish a message to a channel.

```protobuf
rpc Publish(MessagingPublishRequest) returns (MessagingPublishResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `channel` | `string` | **Required.** Target channel. |
| `payload` | `bytes` | Message payload. |
| `message_id` | `string` | Custom message ID (auto-generated if empty). |
| `dedup_key` | `string` | Deduplication key. |
| `codec` | `string` | Codec for this message. |
| `metadata` | `map<string,string>` | Key-value metadata. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `message_id` | `string` | Published message ID. |
| `channel` | `string` | Target channel. |
| `is_duplicate` | `bool` | Whether the message was a duplicate. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{
  "channel":"events",
  "payload":"eyJrZXkiOiJ2YWx1ZSJ9",
  "metadata":{"source":"api"}
}' localhost:10443 quicbaas.messaging.v1.MessagingService/Publish
```

---

##### PublishDelta

Publish only the differences between two entity states. The server computes the delta and broadcasts the changes.

```protobuf
rpc PublishDelta(MessagingPublishDeltaRequest) returns (MessagingPublishDeltaResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `channel` | `string` | **Required.** Target channel. |
| `old_value` | `bytes` | Previous state (JSON encoded). |
| `new_value` | `bytes` | New state (JSON encoded). |
| `message_id` | `string` | Custom message ID. |
| `dedup_key` | `string` | Deduplication key. |
| `codec` | `string` | Codec for this message. |
| `metadata` | `map<string,string>` | Key-value metadata. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `message_id` | `string` | Published message ID. |
| `channel` | `string` | Target channel. |
| `delta` | `DeltaPayload` | Computed delta payload. |
| `is_duplicate` | `bool` | Whether the message was a duplicate. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{
  "channel":"users",
  "old_value":"eyJuYW1lIjoiQWxpY2UiLCJhZ2UiOjMwfQ==",
  "new_value":"eyJuYW1lIjoiQWxpY2UiLCJhZ2UiOjMxfQ=="
}' localhost:10443 quicbaas.messaging.v1.MessagingService/PublishDelta
```

---

#### Streaming

##### StreamNotifications

Server-side streaming RPC that delivers real-time notifications for a subscription. The server pushes `MessagingNotification` messages as they arrive on the subscribed channel. The stream remains open until the client cancels or the subscription is closed.

```protobuf
rpc StreamNotifications(MessagingStreamNotificationsRequest) returns (stream MessagingNotification);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `subscription_id` | `string` | **Required.** Subscription to stream. |
| `start_sequence` | `uint64` | Sequence to start from (0 = latest). |
| `include_history` | `bool` | Replay historical messages. |
| `history_limit` | `int32` | Max historical messages to replay. |

**Streamed Response (`MessagingNotification`):**

| Field | Type | Description |
|-------|------|-------------|
| `message` | `Message` | Notification message payload. |
| `subscription_id` | `string` | Owning subscription ID. |
| `sequence_number` | `uint64` | Monotonically increasing sequence. |

```sh
grpcurl -plaintext -d '{"subscription_id":"sub-1"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/StreamNotifications
```

---

#### Watch Integration

##### WatchChannel

Create a subscription that watches for database key changes matching a prefix. Automatically creates a `watch:<prefix>` channel if it does not exist.

```protobuf
rpc WatchChannel(MessagingWatchChannelRequest) returns (MessagingWatchChannelResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `key_prefix` | `string` | **Required.** Key prefix to watch. |
| `session_id` | `string` | Optional session ID. |
| `filter` | `SubscriptionFilter` | Event type filters. |
| `buffer_size` | `int32` | Subscription buffer size. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `subscription` | `Subscription` | Created watch subscription. |
| `channel_name` | `string` | Auto-generated channel name (`watch:<prefix>`). |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{"key_prefix":"users/"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/WatchChannel
```

---

#### Statistics

##### GetStats

Retrieve global messaging service statistics.

```protobuf
rpc GetStats(MessagingGetStatsRequest) returns (MessagingGetStatsResponse);
```

**Request:** Empty.

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `stats` | `Stats` | Service-wide statistics. |
| `is_running` | `bool` | Whether the messaging service is running. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext \
  localhost:10443 quicbaas.messaging.v1.MessagingService/GetStats
```

---

##### GetDedupStats

Retrieve deduplication statistics for a channel.

```protobuf
rpc GetDedupStats(MessagingGetDedupStatsRequest) returns (MessagingGetDedupStatsResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `channel` | `string` | Channel to query. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `stats` | `DedupStats` | Dedup statistics for the channel. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{"channel":"events"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/GetDedupStats
```

---

##### GetChannelStats

Retrieve statistics for a specific channel.

```protobuf
rpc GetChannelStats(MessagingGetChannelStatsRequest) returns (MessagingGetChannelStatsResponse);
```

**Request:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | **Required.** Channel to query. |

**Response:**

| Field | Type | Description |
|-------|------|-------------|
| `stats` | `ChannelStats` | Channel statistics. |
| `timestamp` | `int64` | Response timestamp (Unix). |

```sh
grpcurl -plaintext -d '{"name":"events"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/GetChannelStats
```

---

### Message Types

#### Channel

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Unique channel identifier. |
| `codec` | `string` | Default codec (`json`, `msgpack`, `protobuf`). |
| `created_at` | `int64` | Creation timestamp (Unix). |
| `subscribers` | `int64` | Active subscriber count. |
| `persistent` | `bool` | Messages persisted for replay. |
| `max_history` | `int32` | Max retained messages. |
| `buffer_size` | `int32` | Buffer size. |
| `ttl_seconds` | `int64` | Message TTL in seconds. |

#### Subscription

| Field | Type | Description |
|-------|------|-------------|
| `id` | `string` | Unique subscription ID. |
| `channel` | `string` | Subscribed channel. |
| `client_id` | `string` | Client identifier. |
| `created_at` | `int64` | Creation timestamp (Unix). |
| `filters` | `SubscriptionFilter` | Active filters. |

#### SubscriptionFilter

| Field | Type | Description |
|-------|------|-------------|
| `event_types` | `repeated EventType` | Accepted event types (empty = all). |
| `key_prefix` | `string` | Key prefix filter for watch channels. |

#### Message

| Field | Type | Description |
|-------|------|-------------|
| `id` | `string` | Unique message ID. |
| `type` | `MessageType` | Message type enum. |
| `channel` | `string` | Target channel. |
| `payload` | `bytes` | Message data. |
| `timestamp` | `int64` | Creation timestamp (Unix). |
| `source` | `string` | Message originator. |
| `metadata` | `map<string,string>` | Key-value metadata. |
| `dedup` | `DedupInfo` | Deduplication metadata. |

#### DedupInfo

| Field | Type | Description |
|-------|------|-------------|
| `is_duplicate` | `bool` | Message was a duplicate. |
| `count` | `int64` | Total times received. |
| `first_seen` | `int64` | First receipt timestamp (Unix). |

#### DeltaPayload

| Field | Type | Description |
|-------|------|-------------|
| `entity_id` | `string` | Modified entity ID. |
| `entity_type` | `string` | Entity type descriptor. |
| `version` | `uint64` | New version number. |
| `prev_version` | `uint64` | Previous version number. |
| `timestamp` | `int64` | Change timestamp (Unix). |
| `changes` | `repeated FieldChange` | Field modifications. |

#### FieldChange

| Field | Type | Description |
|-------|------|-------------|
| `path` | `string` | JSON path to field (e.g., `user.address.city`). |
| `operation` | `OpType` | Modification type. |
| `value` | `bytes` | New value (JSON encoded). |
| `old_value` | `bytes` | Previous value (JSON encoded, optional). |

#### Stats

| Field | Type | Description |
|-------|------|-------------|
| `total_channels` | `int64` | Active channel count. |
| `total_subscriptions` | `int64` | Active subscription count. |
| `messages_published` | `int64` | Total published messages. |
| `messages_delivered` | `int64` | Total delivered messages. |
| `dedup_hits` | `int64` | Deduplicated message count. |
| `dropped_messages` | `int64` | Dropped messages (slow consumers). |
| `buffer_utilization` | `double` | Buffer usage percentage (0.0 - 1.0). |

#### ChannelStats

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Channel name. |
| `subscribers` | `int64` | Active subscriber count. |
| `messages_published` | `int64` | Messages published to channel. |
| `messages_in_history` | `int32` | Messages in history buffer. |

#### DedupStats

| Field | Type | Description |
|-------|------|-------------|
| `channel` | `string` | Channel name. |
| `total_messages` | `int64` | Unique messages tracked. |
| `total_duplicates` | `int64` | Total duplicates detected. |
| `top_duplicates` | `repeated DedupEntry` | Most duplicated messages. |

---

### Enums

#### MessageType

| Value | Number | Description |
|-------|--------|-------------|
| `MESSAGE_TYPE_UNSPECIFIED` | 0 | Default/unknown. |
| `MESSAGE_TYPE_SUBSCRIBE` | 1 | Subscribe request. |
| `MESSAGE_TYPE_UNSUBSCRIBE` | 2 | Unsubscribe request. |
| `MESSAGE_TYPE_PUBLISH` | 3 | Publish message. |
| `MESSAGE_TYPE_NOTIFICATION` | 4 | Server push notification. |
| `MESSAGE_TYPE_PING` | 5 | Keep-alive ping. |
| `MESSAGE_TYPE_PONG` | 6 | Keep-alive pong. |
| `MESSAGE_TYPE_ACK` | 7 | Message acknowledgment. |
| `MESSAGE_TYPE_ERROR` | 8 | Error notification. |
| `MESSAGE_TYPE_DATA` | 9 | Data payload. |
| `MESSAGE_TYPE_CONTROL` | 10 | Control message. |
| `MESSAGE_TYPE_HEARTBEAT` | 11 | Heartbeat. |

#### EventType

| Value | Number | Description |
|-------|--------|-------------|
| `EVENT_TYPE_UNSPECIFIED` | 0 | Default/unknown. |
| `EVENT_TYPE_PUT` | 1 | Key created or updated. |
| `EVENT_TYPE_DELETE` | 2 | Key deleted. |

#### OpType

| Value | Number | Description |
|-------|--------|-------------|
| `OP_TYPE_UNSPECIFIED` | 0 | Default/unknown. |
| `OP_TYPE_SET` | 1 | Set field value. |
| `OP_TYPE_DELETE` | 2 | Remove field. |
| `OP_TYPE_APPEND` | 3 | Append to array/string. |
| `OP_TYPE_INCREMENT` | 4 | Increment numeric field. |

#### CodecType

| Value | Number | Description |
|-------|--------|-------------|
| `CODEC_TYPE_UNSPECIFIED` | 0 | Default/unknown. |
| `CODEC_TYPE_JSON` | 1 | JSON encoding. |
| `CODEC_TYPE_MSGPACK` | 2 | MessagePack encoding. |
| `CODEC_TYPE_PROTOBUF` | 3 | Protocol Buffers encoding. |

---

### Error Codes

The gRPC server maps messaging errors to standard gRPC status codes:

| gRPC Code | Condition | Description |
|-----------|-----------|-------------|
| `INVALID_ARGUMENT` | Empty required field, invalid channel name | Missing or malformed request parameters. |
| `NOT_FOUND` | Channel or subscription not found | The requested resource does not exist. |
| `ALREADY_EXISTS` | Channel name already in use | A channel with that name already exists. |
| `RESOURCE_EXHAUSTED` | Max subscribers, buffer full, slow consumer | Resource limits exceeded. |
| `UNAVAILABLE` | Service not running | The messaging service has not started or is shutting down. |
| `ABORTED` | Client disconnected, subscription closed | The operation was interrupted. |
| `UNIMPLEMENTED` | Feature not supported by service type | The service does not support the requested capability. |
| `UNAUTHENTICATED` | Missing credentials | Authentication required. |
| `PERMISSION_DENIED` | Insufficient permissions | Caller lacks required authorization. |
| `DEADLINE_EXCEEDED` | Timeout | Operation exceeded the deadline. |
| `CANCELLED` | Client cancelled | The client cancelled the request. |
| `INTERNAL` | Unexpected error | An internal server error occurred. |

---

### Streaming Usage

#### Full Workflow Example

1. Create a channel.
2. Subscribe to it.
3. Open a notification stream.
4. Publish messages (from another client or the same one).
5. Receive streamed `MessagingNotification` messages.
6. Clean up by unsubscribing and deleting the channel.

```sh
# 1. Create channel
grpcurl -plaintext -d '{"name":"orders","codec":"json"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/CreateChannel

# 2. Subscribe
grpcurl -plaintext -d '{"channel":"orders"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/Subscribe
# Response: {"subscription":{"id":"sub-1","channel":"orders",...},...}

# 3. Stream (runs until cancelled with Ctrl+C)
grpcurl -plaintext -d '{"subscription_id":"sub-1"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/StreamNotifications

# 4. Publish (from another terminal)
grpcurl -plaintext -d '{
  "channel":"orders",
  "payload":"eyJvcmRlcl9pZCI6MTIzfQ=="
}' localhost:10443 quicbaas.messaging.v1.MessagingService/Publish

# 5. Unsubscribe
grpcurl -plaintext -d '{"subscription_id":"sub-1"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/Unsubscribe

# 6. Delete channel
grpcurl -plaintext -d '{"name":"orders"}' \
  localhost:10443 quicbaas.messaging.v1.MessagingService/DeleteChannel
```

### Related Documentation

- Push API Reference - Protocol-agnostic overview
- [Architecture](architecture.md) - Messaging system design
- [Delta Publishing](architecture.md) - Delta update details
- [Go SDK](engineering.md) - Go client library
- [CLI Reference](cli.md) - Command-line usage


---

## QUIC Messaging API Reference

### Overview

The QUIC transport provides low-latency, multiplexed messaging over UDP with 0-RTT connection establishment, connection migration, and stream-level flow control. Messaging operations use a sub-operation dispatch pattern where the primary opcode `OpMsg` (`0x00`) routes to specific handlers via a sub-opcode in the first byte of the payload.

**Default Port:** 9444 (UDP)
**Protocol Version:** `0x01`
**Payload Encoding:** JSON
**Max Payload Size:** 16 MB

### Wire Format

#### Request Format

```
[opcode:1][tokenLen:2][token:N][payloadLen:4][payload:M]
```

| Field | Size | Encoding | Description |
|-------|------|----------|-------------|
| `opcode` | 1 byte | uint8 | Operation code (`0x00` for messaging) |
| `tokenLen` | 2 bytes | big-endian uint16 | Authentication token length (max 8KB) |
| `token` | N bytes | UTF-8 string | JWT authentication token |
| `payloadLen` | 4 bytes | big-endian uint32 | Payload length |
| `payload` | M bytes | JSON | `[subOpcode:1][jsonBody:M-1]` |

For messaging operations, `payload[0]` is the sub-opcode and `payload[1:]` is the JSON-encoded request body.

#### Response Format

```
[status:1][payloadLen:4][payload:N]
```

| Field | Size | Encoding | Description |
|-------|------|----------|-------------|
| `status` | 1 byte | uint8 | Status code |
| `payloadLen` | 4 bytes | big-endian uint32 | Payload length |
| `payload` | N bytes | JSON | Response body |

### Opcodes

#### Dispatch Opcode

All messaging operations use the shared dispatch opcode:

| Opcode | Hex | Description |
|--------|-----|-------------|
| `OpMsg` | `0x00` | Messaging dispatch (sub-op in `payload[0]`) |

#### Messaging Sub-Opcodes

Sub-opcodes occupy range `0x20-0x2F`, non-overlapping with scheduler (`0x01-0x1F`).

| Sub-Opcode | Hex | Operation | Category |
|------------|-----|-----------|----------|
| `SubOpMsgCreateChannel` | `0x20` | Create a channel | Channel |
| `SubOpMsgDeleteChannel` | `0x21` | Delete a channel | Channel |
| `SubOpMsgGetChannel` | `0x22` | Get channel info | Channel |
| `SubOpMsgListChannels` | `0x23` | List all channels | Channel |
| `SubOpMsgSubscribe` | `0x24` | Subscribe to a channel | Subscription |
| `SubOpMsgUnsubscribe` | `0x25` | Unsubscribe | Subscription |
| `SubOpMsgPublish` | `0x26` | Publish a message | Publishing |
| `SubOpMsgPublishDelta` | `0x27` | Publish a delta message | Publishing |
| `SubOpMsgGetStats` | `0x28` | Get service statistics | Statistics |
| `SubOpMsgDedupStats` | `0x29` | Get dedup statistics | Statistics |

### Channel Operations

#### CreateChannel (0x20)

Create a notification channel for publishing messages.

**Request Body:**
```json
{
  "name": "orders",
  "codec": "json",
  "persistent": true,
  "buffer_size": 512,
  "max_history": 1000,
  "ttl_seconds": 3600
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Unique channel identifier |
| `codec` | string | no | Serialization codec (default: JSON) |
| `persistent` | bool | no | Enable message persistence |
| `buffer_size` | int | no | Channel buffer size |
| `max_history` | int | no | Maximum history entries retained |
| `ttl_seconds` | int64 | no | Message time-to-live in seconds |

**Response:** Channel object with configuration and metadata.

#### DeleteChannel (0x21)

Delete a channel and disconnect all subscribers.

**Request Body:**
```json
{
  "name": "orders"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Channel to delete |

**Response:**
```json
{
  "success": true
}
```

#### GetChannel (0x22)

Retrieve detailed information about a channel.

**Request Body:**
```json
{
  "name": "orders"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Channel name to query |

**Response:** Channel object with configuration, subscriber count, and statistics.

#### ListChannels (0x23)

List all notification channels.

**Request Body:** Empty (no fields required).

**Response:** Array of channel objects.

### Subscription Operations

#### Subscribe (0x24)

Subscribe to a channel to receive notifications.

**Request Body:**
```json
{
  "channel": "orders",
  "session_id": "client-session-001",
  "buffer_size": 256,
  "event_types": ["PUT", "DELETE"],
  "key_prefix": "user/123/"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `channel` | string | yes | Channel name |
| `session_id` | string | no | Client session identifier for resumption |
| `buffer_size` | int | no | Subscription buffer size |
| `event_types` | []string | no | Filter: `"PUT"` and/or `"DELETE"` |
| `key_prefix` | string | no | Filter: key prefix match |

**Response:**
```json
{
  "subscription_id": "sub_abc123",
  "channel": "orders",
  "client_id": "client-001"
}
```

#### Unsubscribe (0x25)

Remove a subscription.

**Request Body:**
```json
{
  "subscription_id": "sub_abc123"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `subscription_id` | string | yes | Subscription ID from Subscribe response |

**Response:**
```json
{
  "success": true
}
```

### Publishing Operations

#### Publish (0x26)

Publish a message to a channel.

**Request Body:**
```json
{
  "channel": "orders",
  "payload": {"order_id": "ord_001", "status": "shipped"},
  "message_id": "msg-unique-001",
  "dedup_key": "order-ord_001-shipped",
  "metadata": {
    "source": "order-service",
    "priority": "high"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `channel` | string | yes | Target channel |
| `payload` | any | yes | Message payload (arbitrary JSON) |
| `message_id` | string | no | Client-assigned message ID |
| `dedup_key` | string | no | Deduplication key for idempotent publishing |
| `metadata` | map[string]string | no | Key-value metadata pairs |

**Response:**
```json
{
  "success": true
}
```

#### PublishDelta (0x27)

Publish the differences between two entity states. Subscribers in delta mode receive only the changed fields.

**Request Body:**
```json
{
  "channel": "orders",
  "old": {"order_id": "ord_001", "status": "processing", "items": 3},
  "new": {"order_id": "ord_001", "status": "shipped", "items": 3},
  "metadata": {
    "entity_type": "Order"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `channel` | string | yes | Target channel |
| `old` | any | yes | Previous entity state |
| `new` | any | yes | Current entity state |
| `metadata` | map[string]string | no | Key-value metadata pairs |

**Response:**
```json
{
  "success": true
}
```

### Statistics Operations

#### GetStats (0x28)

Retrieve aggregate messaging service statistics.

**Request Body:** Empty (no fields required).

**Response:**
```json
{
  "total_channels": 12,
  "total_subscriptions": 85,
  "messages_published": 154230,
  "messages_delivered": 1283400
}
```

#### DedupStats (0x29)

Retrieve deduplication statistics for a specific channel.

**Request Body:**
```json
{
  "channel": "orders"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `channel` | string | yes | Channel name |

**Response:**
```json
{
  "channel": "orders",
  "total_messages": 15234,
  "total_duplicates": 343
}
```

### Status Codes

| Code | Hex | Name | Description |
|------|-----|------|-------------|
| 0 | `0x00` | OK | Operation succeeded |
| 1 | `0x01` | InvalidRequest | Malformed request |
| 2 | `0x02` | NotFound | Channel or subscription not found |
| 3 | `0x03` | Unauthorized | Missing or invalid authentication token |
| 4 | `0x04` | InternalError | Server-side failure |
| 5 | `0x05` | Timeout | Operation timed out |
| 6 | `0x06` | AlreadyExists | Channel already exists |
| 7 | `0x07` | PermissionDenied | Insufficient permissions |
| 8 | `0x08` | ResourceExhausted | Rate limit or quota exceeded |
| 9 | `0x09` | InvalidArgument | Invalid field value in request body |
| 10 | `0x0A` | Unavailable | Messaging service not enabled or not ready |

On error, the response payload contains a UTF-8 error message string.

### Streaming Behavior

QUIC streams provide natural multiplexing for concurrent operations. Each request/response pair occupies a single bidirectional QUIC stream. Key behaviors:

- **No head-of-line blocking** -- independent messages do not block each other across streams.
- **Connection migration** -- clients can change network (e.g., Wi-Fi to cellular) without dropping the connection.
- **0-RTT** -- repeat connections skip the TLS handshake round-trip for faster first requests.
- **Subscribe streams** -- subscription notifications are delivered on the response payload. For long-lived subscriptions, the client opens a dedicated QUIC stream and receives messages as sequential response frames.
- **List aggregation** -- `ListChannels` collects all results server-side and returns a single aggregated response.

### CLI Examples

All examples use the `--transport quic` flag to select the QUIC protocol.

#### Create a channel

```bash
quicbaas msg create-channel orders \
  --transport quic \
  --address localhost:9444 \
  --buffer-size 512 \
  --persistent \
  --ttl 1h
```

#### Subscribe to a channel

```bash
quicbaas msg subscribe orders \
  --transport quic \
  --address localhost:9444 \
  --filter "status=shipped"
```

#### Publish a message

```bash
quicbaas msg publish orders \
  --transport quic \
  --address localhost:9444 \
  --payload '{"order_id": "ord_001", "status": "shipped"}' \
  --dedup-id "order-ord_001-shipped"
```

#### Publish a delta

```bash
quicbaas msg publish-delta orders \
  --transport quic \
  --address localhost:9444 \
  --old '{"status": "processing"}' \
  --new '{"status": "shipped", "tracking": "TRK123"}'
```

#### Get service statistics

```bash
quicbaas msg stats --transport quic --address localhost:9444
```

#### Get dedup statistics

```bash
quicbaas msg dedup-stats orders \
  --transport quic \
  --address localhost:9444
```

### Related Documentation

- API Reference -- Messaging API overview
- [CLI Reference](cli.md) -- Full CLI command reference
- [Architecture](architecture.md) -- Messaging system design
- [Delta Publishing](architecture.md) -- Delta computation details


---

## MCP Messaging API Reference

### Overview

The MCP (Model Context Protocol) messaging API exposes QuicBaaS's pub/sub messaging system as MCP tools for AI/LLM integration. All 10 tools follow the MCP specification with JSON schema validation and are available when the server is built with the `mcp` build tag.

Transport: stdio or HTTP (port 9445)

### Tools

#### quicbaas_msg_create_channel

Create a new messaging channel with optional configuration.

**Input Schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Unique channel name (alphanumeric, underscore, hyphen, period, slash, colon) |
| `codec` | string | No | Default message codec: `json`, `msgpack`, or `protobuf` |
| `buffer_size` | string | No | Message buffer size for the channel |
| `max_subscribers` | string | No | Maximum number of subscribers allowed |
| `persistent` | string | No | Set to `"true"` to persist messages for replay |
| `max_history` | string | No | Maximum number of messages to retain for replay |
| `ttl` | string | No | Message time-to-live as duration string (e.g., `"1h"`, `"24h"`) |

**Output:**

```json
{
  "name": "events.orders",
  "codec": "json",
  "created_at": "2025-01-30T12:00:00Z",
  "subscribers": 0,
  "persistent": true,
  "max_history": 1000,
  "buffer_size": 256,
  "ttl": "24h0m0s"
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_create_channel",
  "arguments": {
    "name": "events.orders",
    "codec": "json",
    "persistent": "true",
    "max_history": "1000",
    "ttl": "24h"
  }
}
```

---

#### quicbaas_msg_delete_channel

Delete a messaging channel and all its subscriptions.

**Input Schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Name of the channel to delete |

**Output:**

```json
{
  "deleted": true,
  "name": "events.orders"
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_delete_channel",
  "arguments": {
    "name": "events.orders"
  }
}
```

---

#### quicbaas_msg_get_channel

Get information about a messaging channel.

**Input Schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Name of the channel to retrieve |

**Output:**

```json
{
  "name": "events.orders",
  "codec": "json",
  "created_at": "2025-01-30T12:00:00Z",
  "subscribers": 3,
  "persistent": true,
  "max_history": 1000,
  "buffer_size": 256,
  "ttl": "24h0m0s"
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_get_channel",
  "arguments": {
    "name": "events.orders"
  }
}
```

---

#### quicbaas_msg_list_channels

List all messaging channels. Takes no parameters.

**Input Schema:**

None (empty object).

**Output:**

```json
{
  "channels": [
    {
      "name": "events.orders",
      "codec": "json",
      "created_at": "2025-01-30T12:00:00Z",
      "subscribers": 3,
      "persistent": true,
      "max_history": 1000,
      "buffer_size": 256,
      "ttl": "24h0m0s"
    }
  ],
  "count": 1
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_list_channels",
  "arguments": {}
}
```

---

#### quicbaas_msg_subscribe

Subscribe to a messaging channel with optional filtering.

**Input Schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `channel` | string | Yes | Name of the channel to subscribe to |
| `buffer_size` | string | No | Subscription message buffer size |
| `session_id` | string | No | Session ID for the subscription |
| `filter_key_prefix` | string | No | Filter messages by key prefix |
| `filter_event_types` | string | No | JSON array of event types to filter: `["PUT", "DELETE"]` |

**Output:**

```json
{
  "subscription_id": "sub_abc123",
  "channel": "events.orders",
  "client_id": "client_xyz",
  "created_at": "2025-01-30T12:00:00Z"
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_subscribe",
  "arguments": {
    "channel": "events.orders",
    "filter_key_prefix": "us-east/",
    "filter_event_types": "[\"PUT\"]"
  }
}
```

---

#### quicbaas_msg_unsubscribe

Remove a subscription from a channel.

**Input Schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `subscription_id` | string | Yes | ID of the subscription to remove |

**Output:**

```json
{
  "unsubscribed": true,
  "subscription_id": "sub_abc123"
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_unsubscribe",
  "arguments": {
    "subscription_id": "sub_abc123"
  }
}
```

---

#### quicbaas_msg_publish

Publish a message to a messaging channel.

**Input Schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `channel` | string | Yes | Name of the channel to publish to |
| `payload` | string | Yes | Message payload (JSON string or base64-encoded binary) |
| `message_id` | string | No | Unique message ID for deduplication |
| `dedup_key` | string | No | Deduplication key |
| `metadata` | string | No | JSON object containing message metadata |

If `payload` is valid JSON, it is parsed and published as a structured object. Otherwise, it is published as a raw string.

**Output:**

```json
{
  "published": true,
  "channel": "events.orders"
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_publish",
  "arguments": {
    "channel": "events.orders",
    "payload": "{\"order_id\":\"12345\",\"status\":\"shipped\"}",
    "message_id": "msg_001",
    "metadata": "{\"source\":\"api\",\"region\":\"us-east\"}"
  }
}
```

---

#### quicbaas_msg_publish_delta

Publish a delta update (diff between old and new state) to a channel. The server computes the delta from the two provided states.

**Input Schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `channel` | string | Yes | Name of the channel to publish to |
| `old_state` | string | Yes | JSON representation of the old state |
| `new_state` | string | Yes | JSON representation of the new state |
| `message_id` | string | No | Unique message ID for deduplication |
| `dedup_key` | string | No | Deduplication key |

Both `old_state` and `new_state` must be valid JSON.

**Output:**

```json
{
  "published": true,
  "channel": "events.orders",
  "type": "delta"
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_publish_delta",
  "arguments": {
    "channel": "events.orders",
    "old_state": "{\"order_id\":\"12345\",\"status\":\"pending\"}",
    "new_state": "{\"order_id\":\"12345\",\"status\":\"shipped\"}",
    "dedup_key": "order-12345-update"
  }
}
```

---

#### quicbaas_msg_stats

Get messaging service statistics including channels, subscriptions, and message counts. Takes no parameters.

**Input Schema:**

None (empty object).

**Output:**

```json
{
  "available": true,
  "total_channels": 5,
  "total_subscriptions": 12,
  "messages_published": 48230,
  "messages_delivered": 47915,
  "dedup_hits": 42,
  "dropped_messages": 3,
  "buffer_utilization": 0.34
}
```

When stats are unavailable, returns `{"available": false}`.

**Example:**

```json
{
  "name": "quicbaas_msg_stats",
  "arguments": {}
}
```

---

#### quicbaas_msg_dedup_stats

Get deduplication statistics for a specific channel. Requires the messaging service to have deduplication enabled.

**Input Schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `channel` | string | Yes | Name of the channel to get dedup stats for |

**Output:**

```json
{
  "available": true,
  "channel": "events.orders",
  "total_messages": 48230,
  "total_duplicates": 42,
  "top_duplicates": [
    {
      "message_id": "msg_001",
      "payload_hash": "a1b2c3d4",
      "count": 5,
      "first_seen": "2025-01-30T10:00:00Z",
      "last_seen": "2025-01-30T11:30:00Z"
    }
  ]
}
```

When deduplication is not enabled, returns:

```json
{
  "available": false,
  "channel": "events.orders",
  "message": "deduplication not enabled"
}
```

**Example:**

```json
{
  "name": "quicbaas_msg_dedup_stats",
  "arguments": {
    "channel": "events.orders"
  }
}
```

### Error Handling

All tool invocations return errors via the MCP error response format. Errors are wrapped with context indicating the operation that failed.

| Error | Cause |
|-------|-------|
| `messaging service not configured` | Server started without messaging support |
| `name is required` | Missing required `name` parameter |
| `channel is required` | Missing required `channel` parameter |
| `payload is required` | Missing required `payload` parameter |
| `subscription_id is required` | Missing required `subscription_id` parameter |
| `old_state is required` | Missing required `old_state` parameter |
| `new_state is required` | Missing required `new_state` parameter |
| `invalid ttl format: ...` | TTL string could not be parsed as a Go duration |
| `invalid old_state JSON: ...` | `old_state` is not valid JSON |
| `invalid new_state JSON: ...` | `new_state` is not valid JSON |
| `create channel failed: ...` | Channel creation failed (e.g., duplicate name) |
| `delete channel failed: ...` | Channel deletion failed (e.g., not found) |
| `get channel failed: ...` | Channel lookup failed |
| `list channels failed: ...` | Channel listing failed |
| `subscribe failed: ...` | Subscription creation failed (e.g., max subscribers reached) |
| `unsubscribe failed: ...` | Subscription removal failed (e.g., not found) |
| `publish failed: ...` | Message publish failed |
| `publish delta failed: ...` | Delta publish failed |
| `get dedup stats failed: ...` | Dedup stats retrieval failed |
| `dedup stats not available: ...` | Service type does not support dedup stats |
| `unknown messaging tool: ...` | Unrecognized tool name |

### Related Documentation

- Push API Reference - Core messaging API
- [Architecture](architecture.md) - Push system design
- [Delta Publishing](architecture.md) - Delta compression details
- [CLI Reference](cli.md) - Command-line operations
- [Go SDK](engineering.md) - Go client library


---

## Unix Socket API Reference

### Overview

QuicBaaS exposes its full gRPC messaging API over Unix domain sockets (UDS) for local inter-process communication. The Unix socket transport is the **default** listener and the preferred method for CLI and local service communication. It provides the same `MessagingService` API as the TCP gRPC transport but eliminates network overhead by using the kernel's socket buffer for IPC.

All gRPC services registered on the server (including `MessagingService`, `QuicBaaS`, `KeyManagement`, `Secrets`, etc.) are available over the Unix socket listener. There is no separate protocol -- the transport is standard gRPC over a `unix://` connection.

### Configuration

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

### Connection Setup

#### Go SDK

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

#### grpcurl

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

#### quicbaas CLI

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

### MessagingService RPCs

The full `MessagingService` is available over the Unix socket. The protobuf service definition is at `api/proto/messaging.proto` under package `quicbaas.messaging.v1`.

#### Channel Management

| RPC | Description |
|-----|-------------|
| `CreateChannel` | Create a notification channel |
| `DeleteChannel` | Delete a channel and disconnect subscribers |
| `GetChannel` | Retrieve channel metadata |
| `ListChannels` | List channels with pagination and prefix filter |

#### Subscriptions

| RPC | Description |
|-----|-------------|
| `Subscribe` | Subscribe to a channel with optional filters and delta mode |
| `Unsubscribe` | Remove a subscription by ID |

#### Publishing

| RPC | Description |
|-----|-------------|
| `Publish` | Publish a message with optional dedup key |
| `PublishDelta` | Publish a computed delta between old and new state |

#### Streaming

| RPC | Description |
|-----|-------------|
| `StreamNotifications` | Server-streaming RPC delivering real-time notifications |

#### Watch Integration

| RPC | Description |
|-----|-------------|
| `WatchChannel` | Create a subscription watching key-prefix changes |

#### Statistics

| RPC | Description |
|-----|-------------|
| `GetStats` | Service-wide messaging statistics |
| `GetDedupStats` | Per-channel deduplication statistics |
| `GetChannelStats` | Per-channel message and subscriber stats |

### Security

Unix socket file permissions provide OS-level access control. The socket is created with mode `0660`, restricting access to the file owner and group. No TLS handshake is required for local connections since traffic never leaves the host kernel, though TLS credentials are still accepted by the gRPC server if configured.

For production deployments, ensure the QuicBaaS process runs under a dedicated user and group, and add local client processes to that group.

### Performance

Unix domain sockets bypass the TCP/IP stack entirely, avoiding:
- TCP handshake and teardown overhead
- Nagle algorithm buffering
- IP routing and checksum computation
- Loopback interface processing

This makes UDS the lowest-latency transport for same-host communication, typically achieving sub-100 microsecond round-trip times for small messages. Use this transport for CLI tools, sidecar proxies, and co-located services.

### Related Documentation

- API Reference - Messaging API overview
- [CLI Reference](cli.md) - Command-line messaging operations
- [Go SDK](engineering.md) - Go client library usage
- [Architecture](architecture.md) - Messaging system design
