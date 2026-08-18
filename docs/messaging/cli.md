# Messaging — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/messaging/`.

## Overview

The `quicbaas msg` commands manage messaging notifications, channels, and subscriptions via the CLI. All commands communicate with the QuicBaaS server through the configured protocol (default: Unix socket).

**Status:** Implemented in Phase 7.

## Global Flags

```
--transport  Transport protocol (grpc, rest, quic, unix, mcp, embedded) [default: grpc]
--address    Server address (Unix socket path or host:port) [default: /tmp/quicbaas.sock]
--timeout    Request timeout [default: 30s]
--output     Output format (table, json, yaml) [default: table]
```

## Channel Management

### msg create-channel

Create a notification channel for publishing messages.

```bash
quicbaas msg create-channel <name> [flags]
```

**Flags:**
```
--buffer-size       Channel buffer size [default: 256]
--max-subscribers   Maximum subscribers allowed [default: 1000]
--persistent        Enable message persistence
--ttl               Message time-to-live [default: 0 (no expiry)]
--description       Channel description
```

**Examples:**

```bash
# Create a basic channel
quicbaas msg create-channel orders

# Create a channel with custom buffer and TTL
quicbaas msg create-channel events \
  --buffer-size 512 \
  --ttl 1h \
  --description "Application events channel"

# Create a persistent channel with subscriber limit
quicbaas msg create-channel notifications \
  --persistent \
  --max-subscribers 5000

# Create channel and output as JSON
quicbaas msg create-channel audit --format json
```

**Output:**
```
Channel created successfully.

Name:            orders
Buffer Size:     256
Max Subscribers: 1000
Persistent:      false
TTL:             0s
Created:         2026-01-21T10:30:00Z
```

### msg delete-channel

Delete a channel and disconnect all subscribers.

```bash
quicbaas msg delete-channel <name> [flags]
```

**Flags:**
```
--force    Delete without confirmation
```

**Examples:**

```bash
# Delete with confirmation prompt
quicbaas msg delete-channel orders

# Force delete without confirmation
quicbaas msg delete-channel orders --force
```

### msg list-channels

List all channels with optional filtering.

```bash
quicbaas msg list-channels [flags]
```

**Flags:**
```
--limit      Maximum number of results [default: 100]
--offset     Pagination offset
--filter     Filter by name pattern (glob)
```

**Examples:**

```bash
# List all channels
quicbaas msg list-channels

# List channels with JSON output
quicbaas msg list-channels --format json

# Filter channels by pattern
quicbaas msg list-channels --filter "order*"

# Paginated listing
quicbaas msg list-channels --limit 10 --offset 20
```

**Output:**
```
NAME            SUBSCRIBERS  MESSAGES  PERSISTENT  CREATED
orders          12           1542      false       2026-01-20T08:00:00Z
events          45           8921      true        2026-01-15T12:30:00Z
notifications   128          25430     true        2026-01-10T09:15:00Z
```

### msg channel-info

Get detailed information about a channel.

```bash
quicbaas msg channel-info <name> [flags]
```

**Examples:**

```bash
# Get channel details
quicbaas msg channel-info orders

# Get channel details as YAML
quicbaas msg channel-info orders --format yaml
```

**Output:**
```
Channel: orders
=================
Name:            orders
Description:     Order processing events
Buffer Size:     256
Max Subscribers: 1000
Persistent:      false
TTL:             0s
Created:         2026-01-20T08:00:00Z

Statistics
----------
Active Subscribers: 12
Messages Published: 1542
Messages Delivered: 18504
Bytes Transmitted:  2.4 MB

Deduplication
-------------
Enabled:           true
Window:            1m
Duplicates Blocked: 23
```

## Subscription Commands

### msg subscribe

Subscribe to a channel to receive notifications.

```bash
quicbaas msg subscribe <channel> [flags]
```

**Flags:**
```
--delta          Enable delta mode (receive only changes)
--dedup          Enable deduplication
--dedup-skip     Skip duplicate messages silently
--filter         Message filter expression
--replay         Replay messages from offset
--buffer-size    Client buffer size [default: 256]
```

**Examples:**

```bash
# Basic subscription
quicbaas msg subscribe orders

# Subscribe with delta mode
quicbaas msg subscribe orders --delta

# Subscribe with deduplication enabled
quicbaas msg subscribe orders --dedup

# Subscribe with dedup skip mode (silently drop duplicates)
quicbaas msg subscribe orders --dedup-skip

# Subscribe with message filter
quicbaas msg subscribe orders --filter "status=completed"

# Replay from a specific offset
quicbaas msg subscribe orders --replay 1000
```

**Output:**
```
Subscribed to channel: orders
Subscription ID: sub_abc123

Waiting for messages (Ctrl+C to exit)...

[2026-01-21T10:30:01Z] {"order_id": "ord_001", "status": "completed"}
[2026-01-21T10:30:05Z] {"order_id": "ord_002", "status": "processing"}
[2026-01-21T10:30:08Z] {"order_id": "ord_003", "status": "completed"}
```

### msg unsubscribe

Remove a subscription.

```bash
quicbaas msg unsubscribe <subscription-id> [flags]
```

**Flags:**
```
--force    Unsubscribe without confirmation
```

**Examples:**

```bash
# Unsubscribe from a channel
quicbaas msg unsubscribe sub_abc123

# Force unsubscribe
quicbaas msg unsubscribe sub_abc123 --force
```

### msg list-subscriptions

List active subscriptions.

```bash
quicbaas msg list-subscriptions [flags]
```

**Flags:**
```
--channel    Filter by channel name
--limit      Maximum number of results [default: 100]
--offset     Pagination offset
```

**Examples:**

```bash
# List all subscriptions
quicbaas msg list-subscriptions

# Filter by channel
quicbaas msg list-subscriptions --channel orders

# Output as JSON
quicbaas msg list-subscriptions --format json
```

**Output:**
```
SUBSCRIPTION ID  CHANNEL        MODE     DEDUP   CREATED
sub_abc123       orders         normal   false   2026-01-21T10:00:00Z
sub_def456       orders         delta    true    2026-01-21T09:30:00Z
sub_ghi789       events         normal   false   2026-01-21T08:15:00Z
sub_jkl012       notifications  delta    true    2026-01-20T16:00:00Z
```

## Publishing Commands

### msg publish

Publish a message to a channel.

```bash
quicbaas msg publish <channel> [flags]
```

**Flags:**
```
--payload      Message payload as JSON string (required unless --file)
--file         Read payload from file
--dedup-id     Deduplication ID for idempotent publishing
--priority     Message priority (low, normal, high) [default: normal]
--ttl          Message time-to-live
--ack          Wait for delivery acknowledgment
```

**Examples:**

```bash
# Publish a message with inline payload
quicbaas msg publish orders --payload '{"order_id": "ord_001", "status": "created"}'

# Publish with deduplication ID
quicbaas msg publish orders \
  --dedup-id "order-ord_001-created" \
  --payload '{"order_id": "ord_001", "status": "created"}'

# Publish from file
quicbaas msg publish orders --file /path/to/message.json

# Publish with priority and TTL
quicbaas msg publish alerts \
  --payload '{"level": "critical", "message": "System overload"}' \
  --priority high \
  --ttl 5m

# Publish and wait for acknowledgment
quicbaas msg publish orders \
  --payload '{"order_id": "ord_002", "status": "shipped"}' \
  --ack
```

**Output:**
```
Message published successfully.

Channel:     orders
Message ID:  msg_xyz789
Timestamp:   2026-01-21T10:30:00Z
Delivered:   12 subscribers
```

### msg publish-delta

Publish only the differences between two entity states.

```bash
quicbaas msg publish-delta <channel> [flags]
```

**Flags:**
```
--old           Previous state as JSON string
--new           Current state as JSON string
--old-file      Read previous state from file
--new-file      Read current state from file
--entity-id     Entity identifier
--entity-type   Entity type name
--dedup-id      Deduplication ID
```

**Examples:**

```bash
# Publish delta with inline JSON
quicbaas msg publish-delta orders \
  --old '{"order_id": "ord_001", "status": "created", "items": 3}' \
  --new '{"order_id": "ord_001", "status": "shipped", "items": 3}'

# Publish delta from files
quicbaas msg publish-delta orders \
  --old-file /path/to/old-state.json \
  --new-file /path/to/new-state.json

# Publish delta with entity metadata
quicbaas msg publish-delta orders \
  --entity-id "ord_001" \
  --entity-type "Order" \
  --old '{"status": "processing"}' \
  --new '{"status": "shipped", "tracking": "TRK123"}'
```

**Output:**
```
Delta published successfully.

Channel:     orders
Entity ID:   ord_001
Entity Type: Order
Version:     42
Changes:     2

Field Changes:
  - status: "processing" -> "shipped"
  - tracking: (none) -> "TRK123"

Delivered:   8 delta subscribers
```

## Monitoring Commands

### msg watch

Watch a channel in real-time and display messages as they arrive.

```bash
quicbaas msg watch <channel> [flags]
```

**Flags:**
```
--delta          Enable delta mode
--filter         Message filter expression
--output         Output file (append mode)
--timestamps     Show timestamps [default: true]
--raw            Output raw JSON without formatting
```

**Examples:**

```bash
# Watch channel in real-time
quicbaas msg watch orders

# Watch with delta mode
quicbaas msg watch orders --delta

# Watch with filter
quicbaas msg watch orders --filter "status=shipped"

# Watch and save to file
quicbaas msg watch orders --output /var/log/orders.jsonl

# Raw JSON output
quicbaas msg watch orders --raw
```

**Output (formatted):**
```
Watching channel: orders (Ctrl+C to exit)

[2026-01-21T10:30:01Z] Message received
  order_id: ord_001
  status:   completed
  amount:   $125.00

[2026-01-21T10:30:05Z] Message received
  order_id: ord_002
  status:   processing
  amount:   $89.50
```

**Output (delta mode):**
```
Watching channel: orders (delta mode) (Ctrl+C to exit)

[2026-01-21T10:30:01Z] Delta received for Order:ord_001 (v41 -> v42)
  - status: "processing" -> "shipped"
  - updated_at: "2026-01-21T10:25:00Z" -> "2026-01-21T10:30:01Z"

[2026-01-21T10:30:08Z] Delta received for Order:ord_002 (v15 -> v16)
  - tracking_number: (none) -> "TRK456789"
```

### msg dedup-stats

Show deduplication statistics for a channel.

```bash
quicbaas msg dedup-stats <channel> [flags]
```

**Flags:**
```
--reset    Reset statistics after display
--window   Show stats for specific time window
```

**Examples:**

```bash
# Show dedup statistics
quicbaas msg dedup-stats orders

# Show stats as JSON
quicbaas msg dedup-stats orders --format json

# Show stats for last hour
quicbaas msg dedup-stats orders --window 1h

# Show and reset statistics
quicbaas msg dedup-stats orders --reset
```

**Output:**
```
Deduplication Statistics: orders
================================

Configuration
-------------
Enabled:           true
Window:            1m
Algorithm:         SHA256 (content-based)

Statistics (last 24h)
---------------------
Total Messages:    15,234
Unique Messages:   14,891
Duplicates:        343 (2.3%)
Bytes Saved:       1.2 MB

Top Duplicate Keys
------------------
KEY                             COUNT   LAST SEEN
order-ord_001-status-update     12      2026-01-21T10:28:00Z
order-ord_002-status-update     8       2026-01-21T10:25:30Z
heartbeat-check                 323     2026-01-21T10:30:00Z
```

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |
| 2 | Channel not found |
| 3 | Subscription not found |
| 4 | Invalid arguments |
| 5 | Permission denied |
| 6 | Connection error |
| 7 | Timeout |
| 8 | Rate limited |

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_SERVER` | Server address | `/run/quicbaas.sock` |
| `QUICBAAS_PROTOCOL` | Default protocol | `unix` |
| `QUICBAAS_TENANT` | Default tenant ID | (none) |
| `QUICBAAS_FORMAT` | Default output format | `table` |
| `QUICBAAS_TIMEOUT` | Request timeout | `30s` |

## Configuration File

CLI settings can be stored in `~/.quicbaas/config.yaml`:

```yaml
server: /run/quicbaas.sock
protocol: unix
tenant: default
format: table
timeout: 30s

msg:
  default_buffer_size: 256
  default_dedup: true
  watch_timestamps: true
```

## Related Documentation

- API Reference - Messaging API overview
- [Architecture](architecture.md) - Messaging system design
- [Delta Publishing](architecture.md) - Delta computation details
