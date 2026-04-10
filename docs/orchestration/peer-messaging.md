# Peer Messaging

KV-backed agent-to-agent message routing with backpressure, idempotent delivery, and real-time subscriber notifications.

Source: `pkg/orchestration/peer_messaging.go`, `pkg/orchestration/peer_messaging_store.go`

## PeerMessage Format

```go
type PeerMessage struct {
    ID        string            `json:"id"`         // Unique message ID (dedup key)
    From      string            `json:"from"`       // Sender agent ID
    To        string            `json:"to"`         // Recipient agent ID
    Action    string            `json:"action"`     // Message type (advisory, progress, assign, etc.)
    Channel   string            `json:"channel"`    // Optional routing channel
    Content   string            `json:"content"`    // Message payload
    Metadata  map[string]string `json:"metadata"`   // Arbitrary key-value metadata
    Timestamp int64             `json:"timestamp"`  // Unix nanos
    TTL       int64             `json:"ttl"`        // Optional time-to-live (nanos)
}
```

Required fields: `ID`, `From`, `To`, `Action`. Validation returns `PeerMessageValidationError` for missing fields.

## Message Lifecycle

```
Sender                    KV Store                    Recipient
  |                          |                           |
  |-- Send(msg) ------------>|                           |
  |   [validate]             |                           |
  |   [size check]           |                           |
  |   [dedup check]          |                           |
  |   [inbox capacity]       |                           |
  |   [write inbox key]----->|                           |
  |   [write dedup key]----->|                           |
  |                          |-- notify subscriber ----->|
  |                          |                           |
  |                          |<---- Poll(since) ---------|
  |                          |----> [messages] --------->|
  |                          |                           |
  |                          |<---- Acknowledge(id) -----|
  |                          |   [delete inbox key]      |
  |                          |   [retain dedup key]      |
```

### Send

1. Validate required fields
2. Marshal and check against `MaxMessageSize`
3. Check dedup key -- return `ErrDuplicateMessage` if already delivered
4. Check inbox capacity -- apply overflow action if full
5. Write message to inbox under key `inbox:{tenant}:{agent}:{timestamp}:{msgID}`
6. Write dedup key under `dedup:{tenant}:{agent}:{msgID}`
7. Non-blocking notify to subscriber channel

### Poll

Returns all messages with `timestamp >= since`, sorted chronologically (oldest first). Ties broken by message ID.

### Subscribe

Returns a buffered channel (capacity 100) that receives new messages on delivery. One subscription per agent; subsequent calls replace the previous channel.

### Acknowledge

Removes the message from the inbox. The dedup key is retained permanently to prevent redelivery. Idempotent: returns `nil` if the message does not exist.

## Backpressure

Configured via `InboxPolicy`:

| Field | Type | Description |
|-------|------|-------------|
| `MaxMessages` | `int` | Maximum messages in an inbox |
| `MaxMessageSize` | `int` | Maximum serialized bytes per message |
| `OverflowAction` | `string` | `"reject"` or `"drop_oldest"` |

**reject** (default): Returns `ErrInboxFull` when the inbox is at capacity. The sender must retry or discard.

**drop_oldest**: Evicts the oldest message (lowest timestamp) to make room. The evicted message is lost.

## Idempotent Delivery

Every message is keyed by its `ID` field. Before writing to the inbox, the service checks whether `dedup:{tenant}:{agent}:{msgID}` exists. If so, `ErrDuplicateMessage` is returned.

Dedup keys are never deleted, even after acknowledgment. This guarantees exactly-once delivery semantics within the lifetime of the KV store.

## Auto-Wake Notification

When a message is delivered, the service sends a non-blocking notification to the subscriber channel:

```go
select {
case ch <- msg:
default:
    // Buffer full, drop notification (message still in inbox for polling)
}
```

The recipient can use `Subscribe()` to avoid polling. If the buffer is full, the notification is dropped but the message remains in the inbox for the next `Poll()`.

## Configuration

```yaml
settings:
  peer_messaging:
    max_message_size: 10240   # bytes (default: 10KB)
    max_inbox_size: 1000      # messages (default: 1000)
    overflow_action: reject   # reject | drop_oldest
    inbox_ttl: 1h             # message retention
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `quicbaas_orch_peer_send` | Send a message to another orchestrated agent's inbox |
| `quicbaas_orch_peer_inbox` | Poll the calling agent's inbox for new messages |

## Error Types

| Error | Sentinel | Condition |
|-------|----------|-----------|
| `InboxFullError` | `ErrInboxFull` | Inbox at capacity, overflow=reject |
| `MessageTooLargeError` | `ErrMessageTooLarge` | Serialized size > MaxMessageSize |
| `DuplicateMessageError` | `ErrDuplicateMessage` | Message ID already delivered |
| `PeerMessageValidationError` | `ErrPeerMessageValidation` | Missing required field |
| `PeerInboxError` | `ErrPeerInbox` | KV store operation failure |
| `PeerSubscriptionError` | `ErrPeerSubscription` | Subscription setup failure |
