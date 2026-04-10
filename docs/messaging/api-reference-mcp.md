# MCP Messaging API Reference

## Overview

The MCP (Model Context Protocol) messaging API exposes QuicBaaS's pub/sub messaging system as MCP tools for AI/LLM integration. All 10 tools follow the MCP specification with JSON schema validation and are available when the server is built with the `mcp` build tag.

Transport: stdio or HTTP (port 9445)

## Tools

### quicbaas_msg_create_channel

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

### quicbaas_msg_delete_channel

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

### quicbaas_msg_get_channel

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

### quicbaas_msg_list_channels

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

### quicbaas_msg_subscribe

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

### quicbaas_msg_unsubscribe

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

### quicbaas_msg_publish

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

### quicbaas_msg_publish_delta

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

### quicbaas_msg_stats

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

### quicbaas_msg_dedup_stats

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

## Error Handling

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

## Related Documentation

- [Push API Reference](api-reference.md) - Core messaging API
- [Architecture](architecture.md) - Push system design
- [Delta Publishing](delta-publishing.md) - Delta compression details
- [CLI Reference](cli-reference.md) - Command-line operations
- [Go SDK](sdk-go.md) - Go client library
