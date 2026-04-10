# gRPC Messaging API Reference

## Overview

The `MessagingService` gRPC API provides high-performance, real-time pub/sub messaging over TCP. It supports channel management, subscriptions with filtering, message publishing with deduplication, delta updates, key-change watching, and server-side streaming for live notifications.

- **Package:** `quicbaas.messaging.v1`
- **Proto:** `api/proto/messaging.proto`
- **Default Port:** `10443`

## Service Methods

### Channel Management

#### CreateChannel

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

#### DeleteChannel

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

#### GetChannel

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

#### ListChannels

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

### Subscription Management

#### Subscribe

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

#### Unsubscribe

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

### Publishing

#### Publish

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

#### PublishDelta

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

### Streaming

#### StreamNotifications

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

### Watch Integration

#### WatchChannel

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

### Statistics

#### GetStats

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

#### GetDedupStats

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

#### GetChannelStats

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

## Message Types

### Channel

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

### Subscription

| Field | Type | Description |
|-------|------|-------------|
| `id` | `string` | Unique subscription ID. |
| `channel` | `string` | Subscribed channel. |
| `client_id` | `string` | Client identifier. |
| `created_at` | `int64` | Creation timestamp (Unix). |
| `filters` | `SubscriptionFilter` | Active filters. |

### SubscriptionFilter

| Field | Type | Description |
|-------|------|-------------|
| `event_types` | `repeated EventType` | Accepted event types (empty = all). |
| `key_prefix` | `string` | Key prefix filter for watch channels. |

### Message

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

### DedupInfo

| Field | Type | Description |
|-------|------|-------------|
| `is_duplicate` | `bool` | Message was a duplicate. |
| `count` | `int64` | Total times received. |
| `first_seen` | `int64` | First receipt timestamp (Unix). |

### DeltaPayload

| Field | Type | Description |
|-------|------|-------------|
| `entity_id` | `string` | Modified entity ID. |
| `entity_type` | `string` | Entity type descriptor. |
| `version` | `uint64` | New version number. |
| `prev_version` | `uint64` | Previous version number. |
| `timestamp` | `int64` | Change timestamp (Unix). |
| `changes` | `repeated FieldChange` | Field modifications. |

### FieldChange

| Field | Type | Description |
|-------|------|-------------|
| `path` | `string` | JSON path to field (e.g., `user.address.city`). |
| `operation` | `OpType` | Modification type. |
| `value` | `bytes` | New value (JSON encoded). |
| `old_value` | `bytes` | Previous value (JSON encoded, optional). |

### Stats

| Field | Type | Description |
|-------|------|-------------|
| `total_channels` | `int64` | Active channel count. |
| `total_subscriptions` | `int64` | Active subscription count. |
| `messages_published` | `int64` | Total published messages. |
| `messages_delivered` | `int64` | Total delivered messages. |
| `dedup_hits` | `int64` | Deduplicated message count. |
| `dropped_messages` | `int64` | Dropped messages (slow consumers). |
| `buffer_utilization` | `double` | Buffer usage percentage (0.0 - 1.0). |

### ChannelStats

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Channel name. |
| `subscribers` | `int64` | Active subscriber count. |
| `messages_published` | `int64` | Messages published to channel. |
| `messages_in_history` | `int32` | Messages in history buffer. |

### DedupStats

| Field | Type | Description |
|-------|------|-------------|
| `channel` | `string` | Channel name. |
| `total_messages` | `int64` | Unique messages tracked. |
| `total_duplicates` | `int64` | Total duplicates detected. |
| `top_duplicates` | `repeated DedupEntry` | Most duplicated messages. |

---

## Enums

### MessageType

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

### EventType

| Value | Number | Description |
|-------|--------|-------------|
| `EVENT_TYPE_UNSPECIFIED` | 0 | Default/unknown. |
| `EVENT_TYPE_PUT` | 1 | Key created or updated. |
| `EVENT_TYPE_DELETE` | 2 | Key deleted. |

### OpType

| Value | Number | Description |
|-------|--------|-------------|
| `OP_TYPE_UNSPECIFIED` | 0 | Default/unknown. |
| `OP_TYPE_SET` | 1 | Set field value. |
| `OP_TYPE_DELETE` | 2 | Remove field. |
| `OP_TYPE_APPEND` | 3 | Append to array/string. |
| `OP_TYPE_INCREMENT` | 4 | Increment numeric field. |

### CodecType

| Value | Number | Description |
|-------|--------|-------------|
| `CODEC_TYPE_UNSPECIFIED` | 0 | Default/unknown. |
| `CODEC_TYPE_JSON` | 1 | JSON encoding. |
| `CODEC_TYPE_MSGPACK` | 2 | MessagePack encoding. |
| `CODEC_TYPE_PROTOBUF` | 3 | Protocol Buffers encoding. |

---

## Error Codes

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

## Streaming Usage

### Full Workflow Example

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

## Related Documentation

- [Push API Reference](api-reference.md) - Protocol-agnostic overview
- [Architecture](architecture.md) - Messaging system design
- [Delta Publishing](delta-publishing.md) - Delta update details
- [Go SDK](sdk-go.md) - Go client library
- [CLI Reference](cli-reference.md) - Command-line usage
