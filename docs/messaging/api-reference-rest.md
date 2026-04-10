# Messaging REST API Reference

## Base URL

```
/api/v1/messaging
```

## Authentication

All endpoints require a valid Bearer token in the `Authorization` header:

```
Authorization: Bearer <jwt-token>
```

## Endpoints

### List Channels

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

### Create Channel

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

### Get Channel

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

### Delete Channel

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

### Subscribe

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

### Unsubscribe

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

### Publish

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

### Publish Delta

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

### Get Stats

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

### Get Dedup Stats

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

## Error Responses

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

### HTTP Status Codes

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

### Common Error Conditions

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

## Related Documentation

- [Push API Reference](api-reference.md) -- Protocol-agnostic operations overview
- [Architecture](architecture.md) -- Push system design
- [Delta Publishing](delta-publishing.md) -- Delta computation details
- [CLI Reference](cli-reference.md) -- Command-line operations
- [Go SDK](sdk-go.md) -- Go client library
