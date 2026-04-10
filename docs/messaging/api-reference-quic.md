# QUIC Messaging API Reference

## Overview

The QUIC transport provides low-latency, multiplexed messaging over UDP with 0-RTT connection establishment, connection migration, and stream-level flow control. Messaging operations use a sub-operation dispatch pattern where the primary opcode `OpMsg` (`0x00`) routes to specific handlers via a sub-opcode in the first byte of the payload.

**Default Port:** 9444 (UDP)
**Protocol Version:** `0x01`
**Payload Encoding:** JSON
**Max Payload Size:** 16 MB

## Wire Format

### Request Format

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

### Response Format

```
[status:1][payloadLen:4][payload:N]
```

| Field | Size | Encoding | Description |
|-------|------|----------|-------------|
| `status` | 1 byte | uint8 | Status code |
| `payloadLen` | 4 bytes | big-endian uint32 | Payload length |
| `payload` | N bytes | JSON | Response body |

## Opcodes

### Dispatch Opcode

All messaging operations use the shared dispatch opcode:

| Opcode | Hex | Description |
|--------|-----|-------------|
| `OpMsg` | `0x00` | Messaging dispatch (sub-op in `payload[0]`) |

### Messaging Sub-Opcodes

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

## Channel Operations

### CreateChannel (0x20)

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

### DeleteChannel (0x21)

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

### GetChannel (0x22)

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

### ListChannels (0x23)

List all notification channels.

**Request Body:** Empty (no fields required).

**Response:** Array of channel objects.

## Subscription Operations

### Subscribe (0x24)

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

### Unsubscribe (0x25)

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

## Publishing Operations

### Publish (0x26)

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

### PublishDelta (0x27)

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

## Statistics Operations

### GetStats (0x28)

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

### DedupStats (0x29)

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

## Status Codes

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

## Streaming Behavior

QUIC streams provide natural multiplexing for concurrent operations. Each request/response pair occupies a single bidirectional QUIC stream. Key behaviors:

- **No head-of-line blocking** -- independent messages do not block each other across streams.
- **Connection migration** -- clients can change network (e.g., Wi-Fi to cellular) without dropping the connection.
- **0-RTT** -- repeat connections skip the TLS handshake round-trip for faster first requests.
- **Subscribe streams** -- subscription notifications are delivered on the response payload. For long-lived subscriptions, the client opens a dedicated QUIC stream and receives messages as sequential response frames.
- **List aggregation** -- `ListChannels` collects all results server-side and returns a single aggregated response.

## CLI Examples

All examples use the `--transport quic` flag to select the QUIC protocol.

### Create a channel

```bash
quicbaas msg create-channel orders \
  --transport quic \
  --address localhost:9444 \
  --buffer-size 512 \
  --persistent \
  --ttl 1h
```

### Subscribe to a channel

```bash
quicbaas msg subscribe orders \
  --transport quic \
  --address localhost:9444 \
  --filter "status=shipped"
```

### Publish a message

```bash
quicbaas msg publish orders \
  --transport quic \
  --address localhost:9444 \
  --payload '{"order_id": "ord_001", "status": "shipped"}' \
  --dedup-id "order-ord_001-shipped"
```

### Publish a delta

```bash
quicbaas msg publish-delta orders \
  --transport quic \
  --address localhost:9444 \
  --old '{"status": "processing"}' \
  --new '{"status": "shipped", "tracking": "TRK123"}'
```

### Get service statistics

```bash
quicbaas msg stats --transport quic --address localhost:9444
```

### Get dedup statistics

```bash
quicbaas msg dedup-stats orders \
  --transport quic \
  --address localhost:9444
```

## Related Documentation

- [API Reference](api-reference.md) -- Messaging API overview
- [CLI Reference](cli-reference.md) -- Full CLI command reference
- [Architecture](architecture.md) -- Messaging system design
- [Delta Publishing](delta-publishing.md) -- Delta computation details
